package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"time"
)

func (s Store) Signal(ctx context.Context, q postgres.Executor, a tenant.Access, id string, r Signal) (Instance, error) {
	if !keyPattern.MatchString(r.Key) || r.ExpectedVersion < 1 || r.ExpectedVersion > 1000 || !validID(r.StateID, "state") || !includes([]string{"approve", "reject", "cancel", "recover", "reconcile"}, r.Operation) {
		return Instance{}, ErrInvalid
	}
	if (r.Operation == "recover" || r.Operation == "reconcile") && !keyPattern.MatchString(r.OperatorRef) {
		return Instance{}, ErrInvalid
	}
	if r.Operation != "reconcile" && (r.Outcome != "" || len(r.Output) > 0) {
		return Instance{}, ErrInvalid
	}
	x, e := s.load(ctx, q, a, id, true)
	if e != nil {
		return Instance{}, e
	}
	entry, e := s.Authorize(ctx, q, a, "workflows."+r.Operation, x.Pin, x.inputs.Fields)
	if e != nil {
		return Instance{}, e
	}
	raw, _ := json.Marshal(struct {
		Signal Signal
		Actor  string
	}{r, a.ActorID()})
	requestHash := Hash(raw)
	var oldHash string
	e = q.QueryRowContext(ctx, `SELECT request_hash FROM runtime.workflow_signals WHERE tenant_id=$1 AND instance_id=$2 AND idempotency_key=$3`, a.TenantID(), id, r.Key).Scan(&oldHash)
	if e == nil {
		if oldHash != requestHash {
			return Instance{}, ErrConflict
		}
		if e = s.Audit.Append(ctx, q, workflowEvidence(entry, x.Instance, "signal_duplicate", "")); e != nil {
			return Instance{}, e
		}
		return x.Instance, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Instance{}, e
	}
	if x.Version != r.ExpectedVersion || x.StateID != r.StateID {
		return Instance{}, ErrConflict
	}
	if x.Version >= 1000 {
		return Instance{}, ErrInvalid
	}
	var now time.Time
	if e = q.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return Instance{}, e
	}
	state, ok := x.program.State(x.StateID)
	if !ok {
		return Instance{}, ErrInvalid
	}
	step := ""
	result := ""
	switch r.Operation {
	case "approve", "reject":
		if x.Status != "WAITING_APPROVAL" || state.Type != "approval" || !now.Before(x.DueAt) || !now.Before(x.Deadline) {
			return Instance{}, ErrConflict
		}
		if a.ActorID() == x.ActorID {
			return Instance{}, ErrDenied
		}
		allowed := false
		for _, role := range a.Roles() {
			if includes(state.ApprovalRoles, role) {
				allowed = true
			}
		}
		if !allowed {
			return Instance{}, ErrDenied
		}
		on := "approved"
		if r.Operation == "reject" {
			on = "rejected"
		}
		next, e := x.program.Next(ctx, x.StateID, on, x.inputs.Values)
		if e != nil {
			return Instance{}, e
		}
		enter(&x, next, now)
	case "cancel":
		if (terminal(x.Status) && x.Status != "DEAD") || x.Status == "COMPENSATING" {
			return Instance{}, ErrConflict
		}
		x.Termination = "CANCELLED"
		x.Status = "COMPENSATING"
		x.DueAt = now
		x.Attempt = 0
		x.Failure = "cancelled"
	case "recover":
		if !now.Before(x.Deadline) && x.Termination == "" {
			return Instance{}, ErrConflict
		}
		if x.Status == "COMPENSATION_FAILED" {
			x.Status = "COMPENSATING"
		} else if x.Status == "DEAD" || x.RoutingState == "DEAD" || x.RoutingState == "BLOCKED" {
			if x.Status == "DEAD" {
				x.Status = "READY"
			}
		} else {
			return Instance{}, ErrConflict
		}
		x.Attempt = 0
		x.DueAt = now
		x.Failure = ""
	case "reconcile":
		if x.Status != "RECONCILE" || !includes([]string{"APPLIED", "NOT_APPLIED"}, r.Outcome) {
			return Instance{}, ErrConflict
		}
		call, e := s.pendingActivity(ctx, q, a, x)
		if e != nil {
			return Instance{}, e
		}
		step = call.StepKey
		if r.Outcome == "APPLIED" {
			out, e := canonicalOutput(r.Output)
			if e != nil {
				return Instance{}, e
			}
			if e = s.receipt(ctx, q, a, x, call, out); e != nil {
				return Instance{}, e
			}
			result = Hash(out)
			if call.Compensation {
				x.Status = "COMPENSATING"
				x.DueAt = now
				x.Attempt = 0
			} else {
				next, e := x.program.Next(ctx, x.StateID, "always", x.inputs.Values)
				if e != nil {
					x.Status = "COMPENSATING"
					x.Termination = "FAILED"
					x.DueAt = now
					x.Attempt = 0
					x.Failure = "condition"
				} else {
					enter(&x, next, now)
				}
			}
		} else {
			if len(r.Output) > 0 {
				return Instance{}, ErrInvalid
			}
			if call.Compensation {
				x.Status = "COMPENSATING"
			} else {
				x.Status = "READY"
			}
			x.DueAt = now
			x.Attempt = 0
		}
		if x.Failure != "condition" {
			x.Failure = ""
		}
	}
	entry.State.OperatorRef = r.OperatorRef
	if e = s.save(ctx, q, a, &x, entry, r.Operation, step, result); e != nil {
		return Instance{}, e
	}
	_, e = q.ExecContext(ctx, `INSERT INTO runtime.workflow_signals(tenant_id,instance_id,idempotency_key,request_hash,sequence) VALUES($1,$2,$3,$4,$5)`, a.TenantID(), id, r.Key, requestHash, x.Version)
	if e != nil {
		return Instance{}, e
	}
	return x.Instance, nil
}
