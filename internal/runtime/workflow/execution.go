package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"log/slog"
	"strings"
	"time"
)

type Worker struct {
	DB    *sql.DB
	Store Store
}

func (w Worker) save(ctx context.Context, q postgres.Executor, a tenant.Access, x *stored, lease jobs.WorkflowLease, entry audit.Entry, event, step, result string) error {
	if e := checkLease(ctx, q, a, *x, lease); e != nil {
		return e
	}
	return w.Store.save(ctx, q, a, x, entry, event, step, result)
}

type activityFailure struct {
	err             error
	instanceVersion int64
	step            string
}

func (f activityFailure) Error() string { return "workflow local activity aborted" }
func (f activityFailure) Unwrap() error { return f.err }

func (w Worker) Claim(ctx context.Context) (jobs.WorkflowLease, bool, error) {
	token, e := events.NewID()
	if e != nil {
		return jobs.WorkflowLease{}, false, e
	}
	return jobs.ClaimWorkflow(ctx, w.DB, token)
}
func (w Worker) Step(ctx context.Context, lease jobs.WorkflowLease) error {
	if !w.Store.configured() || w.Store.Activity == nil || lease.Generation < 1 || !tenant.ValidID(lease.Token) {
		return ErrInvalid
	}
	err := jobs.WithJob(ctx, w.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		x, e := w.Store.load(ctx, q, a, lease.Ticket.JobID(), true)
		if e != nil {
			return e
		}
		if terminal(x.Status) {
			return nil
		}
		if e = checkLease(ctx, q, a, x, lease); e != nil {
			return e
		}
		ctx = (propagation.TraceContext{}).Extract(ctx, propagation.MapCarrier{"traceparent": x.Pin.TraceParent})
		ctx, span := otel.Tracer("framework/workflow").Start(ctx, "workflow.step", trace.WithAttributes(attribute.String("tenant_id", a.TenantID()), attribute.String("actor_id", a.ActorID()), attribute.String("workflow_instance_id", x.ID), attribute.String("workflow_id", x.Pin.WorkflowID), attribute.String("workflow_version", x.Pin.WorkflowVersion), attribute.String("metadata_version", x.Pin.IRHash), attribute.String("request_id", x.Pin.RequestID)))
		defer span.End()
		entry, e := w.Store.Authorize(ctx, q, a, "workflows.execute", x.Pin, x.inputs.Fields)
		if e != nil {
			return e
		}
		var now time.Time
		if e = q.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return e
		}
		state, ok := x.program.State(x.StateID)
		if !ok {
			return ErrInvalid
		}
		if x.Status != "COMPENSATING" && (!now.Before(x.Deadline) || (x.Status == "WAITING_APPROVAL" && !now.Before(x.DueAt))) {
			x.Termination = "TIMED_OUT"
			x.Status = "COMPENSATING"
			x.DueAt = now
			x.Attempt = 0
			x.Failure = "timeout"
			return w.save(ctx, q, a, &x, lease, entry, "timeout", "", "")
		}
		if x.Status == "COMPENSATING" {
			call, e := w.Store.pendingActivity(ctx, q, a, x)
			if errors.Is(e, sql.ErrNoRows) {
				x.Status = x.Termination
				x.DueAt = now
				return w.save(ctx, q, a, &x, lease, entry, "compensated", "", "")
			}
			if e != nil {
				return e
			}
			if e = w.Store.call(ctx, q, a, &x, call, state, lease); e != nil {
				return e
			}
			x.DueAt = now
			x.Attempt = 0
			return w.save(ctx, q, a, &x, lease, entry, "compensation_completed", call.StepKey, callResultHash(ctx, q, a, x, call.StepKey))
		}
		if x.Status == "RETRY_WAIT" && now.Before(x.DueAt) {
			return ErrLease
		}
		switch state.Type {
		case "complete":
			x.Status = "SUCCEEDED"
			x.DueAt = now
			return w.save(ctx, q, a, &x, lease, entry, "completed", "", "")
		case "failed":
			x.Termination = "FAILED"
			x.Status = "COMPENSATING"
			x.DueAt = now
			return w.save(ctx, q, a, &x, lease, entry, "failed", "", "")
		case "approval":
			if x.Status == "WAITING_APPROVAL" {
				return ErrLease
			}
			x.Status = "WAITING_APPROVAL"
			x.DueAt = now.Add(time.Duration(state.ApprovalTimeoutMS) * time.Millisecond)
			return w.save(ctx, q, a, &x, lease, entry, "approval_requested", "", "")
		case "timer":
			if x.Status != "WAITING_TIMER" {
				x.Status = "WAITING_TIMER"
				x.DueAt = now.Add(time.Duration(state.TimerMS) * time.Millisecond)
				return w.save(ctx, q, a, &x, lease, entry, "timer_started", "", "")
			}
			if now.Before(x.DueAt) {
				return ErrLease
			}
		case "activity":
			call, e := w.Store.pendingActivity(ctx, q, a, x)
			if e != nil {
				return e
			}
			if e = w.Store.call(ctx, q, a, &x, call, state, lease); e != nil {
				return e
			}
			entry.State.WorkflowStep = call.StepKey
		case "start":
		default:
			return ErrInvalid
		}
		oldState := x.StateID
		next, e := x.program.Next(ctx, x.StateID, "always", x.inputs.Values)
		if e != nil {
			x.Status = "COMPENSATING"
			x.Termination = "FAILED"
			x.DueAt = now
			x.Attempt = 0
			x.Failure = "condition"
			return w.save(ctx, q, a, &x, lease, entry, "condition_failed", "", "")
		}
		step := ""
		result := ""
		if state.Type == "activity" {
			step = oldState
			result = callResultHash(ctx, q, a, x, step)
		}
		enter(&x, next, now)
		if e = checkLease(ctx, q, a, x, lease); e != nil {
			return e
		}
		return w.save(ctx, q, a, &x, lease, entry, "advanced", step, result)
	})
	var failed activityFailure
	if errors.As(err, &failed) {
		return w.recordFailure(ctx, lease, failed)
	}
	return err
}
func checkLease(ctx context.Context, q postgres.Executor, a tenant.Access, x stored, lease jobs.WorkflowLease) error {
	var ok bool
	e := q.QueryRowContext(ctx, `SELECT control.check_workflow_lease($1::uuid,$2::uuid,$3,$4,$5::uuid)`, a.TenantID(), x.ID, x.Pin.IRHash, lease.Generation, lease.Token).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return ErrLease
	}
	return nil
}
func (s Store) pendingActivity(ctx context.Context, q postgres.Executor, a tenant.Access, x stored) (ActivityRequest, error) {
	call := ActivityRequest{InstanceID: x.ID, Pin: x.Pin, Inputs: x.inputs, Version: ActivityVersion}
	if x.Termination != "" {
		var original string
		var output []byte
		e := q.QueryRowContext(ctx, `SELECT r.step_key,r.compensation_ref,r.output FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`, a.TenantID(), x.ID).Scan(&original, &call.ActivityRef, &output)
		if e != nil {
			return call, e
		}
		call.StepKey = original + ":compensate"
		call.OriginalOutput = output
		call.Compensation = true
	} else {
		state, ok := x.program.State(x.StateID)
		if !ok || state.Activity == nil {
			return call, ErrInvalid
		}
		call.StepKey = x.StateID
		call.ActivityRef = state.Activity.LocalRef
		call.Version = state.Activity.Version
	}
	body, _ := json.Marshal(struct{ Instance, Plan, Step, Activity, Input, Output string }{x.ID, x.Pin.PlanHash, call.StepKey, call.ActivityRef, Hash(mustJSON(x.inputs)), Hash(call.OriginalOutput)})
	call.RequestHash = Hash(body)
	return call, nil
}
func mustJSON(x any) []byte { b, _ := json.Marshal(x); return b }
func (s Store) receipt(ctx context.Context, q postgres.Executor, a tenant.Access, x stored, call ActivityRequest, out []byte) error {
	comp := ""
	if !call.Compensation {
		state, _ := x.program.State(x.StateID)
		if state.Activity != nil {
			comp = state.Activity.CompensationLocalRef
		}
	}
	_, e := q.ExecContext(ctx, `INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, a.TenantID(), x.ID, call.StepKey, call.ActivityRef, call.RequestHash, out, Hash(out), x.Version+1, comp)
	return e
}
func (s Store) call(ctx context.Context, q postgres.Executor, a tenant.Access, x *stored, call ActivityRequest, state State, lease jobs.WorkflowLease) error {
	var requestHash string
	err := q.QueryRowContext(ctx, `SELECT request_hash FROM runtime.workflow_receipts WHERE tenant_id=$1 AND instance_id=$2 AND step_key=$3`, a.TenantID(), x.ID, call.StepKey).Scan(&requestHash)
	if err == nil {
		if requestHash != call.RequestHash {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	activityEntry, e := s.Authorize(ctx, q, a, "workflows.activity", x.Pin, x.inputs.Fields)
	if e != nil {
		return e
	}
	if e = s.Audit.Append(ctx, q, workflowEvidence(activityEntry, x.Instance, "activity", call.StepKey)); e != nil {
		return e
	}
	if call.Compensation {
		if original, ok := x.program.State(strings.TrimSuffix(call.StepKey, ":compensate")); ok {
			state = original
		}
	}
	timeout := 1000
	if state.Activity != nil {
		timeout = state.Activity.TimeoutMS
	}
	activityCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	out, e := invoke(s.Activity, activityCtx, q, a, call)
	if e == nil {
		e = activityCtx.Err()
	}
	cancel()
	if e != nil {
		return activityFailure{e, x.Version, call.StepKey}
	}
	canonical, e := canonicalOutput(out)
	if e != nil {
		return activityFailure{Permanent(), x.Version, call.StepKey}
	}
	if e = checkLease(ctx, q, a, *x, lease); e != nil {
		return e
	}
	return s.receipt(ctx, q, a, *x, call, canonical)
}
func invoke(f LocalActivity, ctx context.Context, q postgres.Executor, a tenant.Access, r ActivityRequest) (out json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			out = nil
			err = Permanent()
		}
	}()
	return f(ctx, q, a, r)
}
func callResultHash(ctx context.Context, q postgres.Executor, a tenant.Access, x stored, step string) string {
	var hash string
	_ = q.QueryRowContext(ctx, `SELECT output_hash FROM runtime.workflow_receipts WHERE tenant_id=$1 AND instance_id=$2 AND step_key=$3`, a.TenantID(), x.ID, step).Scan(&hash)
	return hash
}
func (w Worker) recordFailure(ctx context.Context, lease jobs.WorkflowLease, f activityFailure) error {
	return jobs.WithJob(ctx, w.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		x, e := w.Store.load(ctx, q, a, lease.Ticket.JobID(), true)
		if e != nil {
			return e
		}
		if x.Version != f.instanceVersion {
			return ErrLease
		}
		if e = checkLease(ctx, q, a, x, lease); e != nil {
			return e
		}
		entry, e := w.Store.Authorize(ctx, q, a, "workflows.execute", x.Pin, x.inputs.Fields)
		if e != nil {
			return e
		}
		var now time.Time
		if e = q.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return e
		}
		x.Attempt++
		x.Failure = failureKind(f.err)
		state, _ := x.program.State(x.StateID)
		if x.Termination != "" {
			if original, ok := x.program.State(strings.TrimSuffix(f.step, ":compensate")); ok {
				state = original
			}
		}
		max := 3
		initial := 100
		backoffMax := 1000
		if state.Activity != nil {
			max = state.Activity.Retry.MaxAttempts
			initial = state.Activity.Retry.InitialBackoffMS
			backoffMax = state.Activity.Retry.MaxBackoffMS
		}
		if x.Failure == "unknown" {
			x.Status = "RECONCILE"
			x.DueAt = now
		} else if x.Failure == "permanent" || x.Attempt >= max {
			if x.Termination != "" {
				x.Status = "COMPENSATION_FAILED"
			} else {
				x.Status = "DEAD"
			}
			x.DueAt = now
		} else {
			if x.Termination != "" {
				x.Status = "COMPENSATING"
			} else {
				x.Status = "RETRY_WAIT"
			}
			delay := initial * (1 << (x.Attempt - 1))
			if delay > backoffMax {
				delay = backoffMax
			}
			x.DueAt = now.Add(time.Duration(delay) * time.Millisecond)
		}
		return w.save(ctx, q, a, &x, lease, entry, "activity_failed", f.step, "")
	})
}
func (w Worker) Once(ctx context.Context) (bool, error) {
	l, found, e := w.Claim(ctx)
	if e != nil || !found {
		return false, e
	}
	return true, w.Step(ctx, l)
}
func (w Worker) Run(ctx context.Context, logger *slog.Logger) {
	for ctx.Err() == nil {
		worked, e := w.Once(ctx)
		if e != nil && ctx.Err() == nil {
			logger.Warn("workflow execution deferred")
		}
		if !worked || e != nil {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
}
