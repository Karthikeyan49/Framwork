package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/jackc/pgx/v5/pgconn"
)

// minimumSendMS refuses to start a send that could not complete, and have its
// outcome recorded, inside the remaining lease (ADR036 #5).
const minimumSendMS = 250

// toolDispatch is the committed intent of one bounded attempt. It is executed
// outside any database transaction and its outcome is fenced by the attempt.
type toolDispatch struct {
	step, key, mode, kind string
	attempt               int
	timeout               time.Duration
	definition            tools.Definition
	inputs                map[string]any
	evidence              audit.Entry
}

type toolLedger struct {
	state, class, lastKind string
	retryable              bool
	attempts               int
	firstSent              time.Time
	response               []byte
}

func toolActivityRef(a toolActivityState) string {
	return a.ref + "@" + a.version + "#" + a.hash
}

type toolActivityState struct{ ref, version, hash string }

func toolState(s State) (toolActivityState, bool) {
	if s.Activity == nil || s.Activity.ToolRef == "" || s.ToolHash == "" {
		return toolActivityState{}, false
	}
	return toolActivityState{s.Activity.ToolRef, s.Activity.Version, s.ToolHash}, true
}

// toolMode derives the effective non-production mode from server-owned facts:
// the tool's declared behavior and the leased job's dataset environment/ceiling
// (control.worker_environment). Requests, metadata and AI never supply it.
func (s Store) toolMode(ctx context.Context, q postgres.Executor, d tools.Definition) (string, error) {
	var environment string
	var ceiling sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT environment,tool_ceiling FROM control.worker_environment()`).Scan(&environment, &ceiling); err != nil {
		return "", err
	}
	if environment == "sandbox" && !ceiling.Valid {
		return tools.ModeBlock, nil
	}
	return tools.EffectiveMode(d, ceiling.String), nil
}

func loadToolLedger(ctx context.Context, q postgres.Executor, a tenant.Access, instance, step string) (toolLedger, bool, error) {
	var l toolLedger
	err := q.QueryRowContext(ctx, `SELECT v.state,v.outcome_class,v.retryable,v.attempts,v.first_sent_at,v.response,coalesce((SELECT t.kind FROM runtime.tool_attempts t WHERE t.tenant_id=v.tenant_id AND t.instance_id=v.instance_id AND t.step_key=v.step_key ORDER BY t.attempt DESC LIMIT 1),'') FROM runtime.tool_invocations v WHERE v.tenant_id=$1 AND v.instance_id=$2 AND v.step_key=$3`, a.TenantID(), instance, step).Scan(&l.state, &l.class, &l.retryable, &l.attempts, &l.firstSent, &l.response, &l.lastKind)
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	return l, err == nil, err
}

// decideTool applies the declared delivery capability to the durable ledger.
// It never resends an unknown outcome unless the provider declared a replay
// window or a lookup proved absence; fresh failures back off first.
func decideTool(d tools.Definition, l toolLedger, found bool, x stored, maxAttempts int, now time.Time) (string, error) {
	if !found || l.state == "NOT_APPLIED" {
		return tools.KindSend, nil
	}
	due := x.Status == "RETRY_WAIT"
	exhausted := x.Attempt+1 >= maxAttempts
	switch l.state {
	case "FAILED":
		if !l.retryable || d.Delivery.RetryMode == "NEVER" {
			return "", Permanent()
		}
		if !due {
			return "", Retryable()
		}
		return tools.KindSend, nil
	case "UNKNOWN":
		backoff := func(kind string) (string, error) {
			if due {
				return kind, nil
			}
			if exhausted {
				return "", Unknown()
			}
			return "", Retryable()
		}
		switch d.Delivery.IdempotencyMode {
		case "LOOKUP_BY_REFERENCE":
			if l.lastKind == tools.KindSend {
				return tools.KindLookup, nil
			}
			return backoff(tools.KindLookup)
		case "NATIVE_KEY", "DEDUPE_WINDOW":
			if d.Delivery.RetryMode != "SAFE_RETRY" || now.Sub(l.firstSent) >= time.Duration(d.Delivery.ReplayWindowMS)*time.Millisecond {
				return "", Unknown()
			}
			return backoff(tools.KindSend)
		default:
			if (d.SideEffect == "NONE" || d.SideEffect == "READ") && d.Delivery.RetryMode == "SAFE_RETRY" {
				return backoff(tools.KindSend)
			}
			return "", Unknown()
		}
	}
	return "", ErrInvalid
}

func toolInputs(d tools.Definition, a map[string]string, snapshot Snapshot) (map[string]any, error) {
	out := map[string]any{}
	for name, field := range a {
		f, ok := d.InputField(name)
		raw, present := snapshot.Values[field]
		if !ok {
			return nil, ErrInvalid
		}
		if !present || string(raw) == "null" {
			if f.Required {
				return nil, ErrInvalid
			}
			continue
		}
		v, err := tools.DecodeInput(f, raw)
		if err != nil {
			return nil, ErrInvalid
		}
		out[name] = v
	}
	for _, f := range d.Input {
		if _, ok := out[f.Name]; f.Required && !ok {
			return nil, ErrInvalid
		}
	}
	return out, nil
}

func toolEvidence(e audit.Entry, x stored, step string, t toolActivityState, mode string, attempt int, outcome string) audit.Entry {
	e = workflowEvidence(e, x.Instance, "tool", step)
	e.State.ToolRef, e.State.ToolVersion, e.State.ArtifactHash = t.ref, t.version, t.hash
	e.State.ToolMode, e.State.ToolAttempt, e.State.ToolOutcome = mode, int64(attempt), outcome
	return e
}

// toolActivity runs inside the leased step transaction. It either confirms an
// existing receipt (done), records a receipt from a settled ledger (done), or
// commits one attempt intent and returns it for execution after commit.
func (s Store) toolActivity(ctx context.Context, q postgres.Executor, a tenant.Access, x *stored, state State, call ActivityRequest, now time.Time) (*toolDispatch, error) {
	t, _ := toolState(state)
	var requestHash string
	err := q.QueryRowContext(ctx, `SELECT request_hash FROM runtime.workflow_receipts WHERE tenant_id=$1 AND instance_id=$2 AND step_key=$3`, a.TenantID(), x.ID, call.StepKey).Scan(&requestHash)
	if err == nil {
		if requestHash != call.RequestHash {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	fail := func(e error) error { return activityFailure{e, x.Version, call.StepKey} }
	def, ok := s.ToolCatalog.Lookup(t.ref, t.version, t.hash)
	if !ok || !def.AllowsTenant(a.TenantID()) || s.Tools == nil {
		// A removed/changed/unlicensed tool fails closed before any I/O.
		return nil, fail(Permanent())
	}
	l, found, err := loadToolLedger(ctx, q, a, x.ID, call.StepKey)
	if err != nil {
		return nil, err
	}
	if found && l.state == "SENDING" {
		var abandoned bool
		if err = q.QueryRowContext(ctx, `SELECT control.tool_abandon($1)`, call.StepKey).Scan(&abandoned); err != nil {
			var pe *pgconn.PgError
			if errors.As(err, &pe) && pe.Code == "55000" {
				return nil, ErrLease
			}
			return nil, err
		}
		if l, found, err = loadToolLedger(ctx, q, a, x.ID, call.StepKey); err != nil {
			return nil, err
		}
	}
	if found && l.state == "SUCCEEDED" {
		out, e := canonicalOutput(l.response)
		if e != nil {
			return nil, fail(Unknown())
		}
		return nil, s.receipt(ctx, q, a, *x, call, out)
	}
	if found && l.state == "APPLIED" {
		// Manual reconciliation writes its receipt in the same transaction.
		return nil, ErrInvalid
	}
	maxAttempts := 3
	if state.Activity != nil && state.Activity.Retry.MaxAttempts > 0 {
		maxAttempts = state.Activity.Retry.MaxAttempts
	}
	kind, decision := decideTool(def, l, found, *x, maxAttempts, now)
	if decision != nil {
		return nil, fail(decision)
	}
	inputs, err := toolInputs(def, state.Activity.Inputs, x.inputs)
	if err != nil {
		return nil, fail(Permanent())
	}
	activityEntry, err := s.Authorize(ctx, q, a, "workflows.activity", x.Pin, x.inputs.Fields)
	if err != nil {
		return nil, err
	}
	invokeEntry, err := s.Authorize(ctx, q, a, "tools.invoke", x.Pin, x.inputs.Fields)
	if err != nil {
		return nil, err
	}
	mode, err := s.toolMode(ctx, q, def)
	if err != nil {
		return nil, err
	}
	var attempt, remaining int
	var key string
	err = q.QueryRowContext(ctx, `SELECT attempt,idempotency_key,remaining_ms FROM control.tool_begin($1,$2,$3,$4,$5,$6,$7,$8)`, call.StepKey, t.ref, t.version, t.hash, call.RequestHash, mode, kind, def.Approval == "REQUIRED").Scan(&attempt, &key, &remaining)
	var pe *pgconn.PgError
	if errors.As(err, &pe) && (pe.Code == "TL001" || pe.Code == "TL002") {
		// Missing approval evidence or a changed approved record: no send.
		return nil, fail(Permanent())
	}
	if err != nil {
		return nil, err
	}
	if remaining < minimumSendMS {
		return nil, ErrLease
	}
	timeout := def.Connector.TimeoutMS
	if remaining < timeout {
		timeout = remaining
	}
	if err = s.Audit.Append(ctx, q, toolEvidence(activityEntry, *x, call.StepKey, t, mode, attempt, "")); err != nil {
		return nil, err
	}
	invoke := toolEvidence(invokeEntry, *x, call.StepKey, t, mode, attempt, "SENDING")
	if err = s.Audit.Append(ctx, q, invoke); err != nil {
		return nil, err
	}
	return &toolDispatch{step: call.StepKey, key: key, mode: mode, kind: kind, attempt: attempt, timeout: time.Duration(timeout) * time.Millisecond, definition: def, inputs: inputs, evidence: invoke}, nil
}

// sendTool executes a committed intent outside any transaction, then records
// its outcome under the same exact lease. A lost lease leaves the attempt
// SENDING, which the next lease treats as UNKNOWN.
func (w Worker) sendTool(ctx context.Context, lease jobs.WorkflowLease, d toolDispatch) error {
	sendCtx, cancel := context.WithTimeout(ctx, d.timeout)
	r := w.Store.Tools.Execute(sendCtx, tools.Request{Definition: d.definition, Mode: d.mode, Kind: d.kind, Key: d.key, Inputs: d.inputs, Timeout: d.timeout})
	cancel()
	var body []byte
	switch r.Outcome {
	case tools.Success:
		if r.Class != tools.ClassOK {
			r = tools.Result{Outcome: tools.Unknown, Class: tools.ClassInvalidResponse}
			break
		}
		raw, e := canonicalToolOutput(r.Output)
		if e != nil {
			r = tools.Result{Outcome: tools.Unknown, Class: tools.ClassInvalidResponse}
		} else {
			body = raw
		}
	case tools.Failed, tools.Unknown:
		if r.Class == "" || r.Class == tools.ClassOK {
			r.Class = tools.ClassInvalidResponse
			r.Outcome = tools.Unknown
		}
	default:
		r = tools.Result{Outcome: tools.Unknown, Class: tools.ClassInvalidResponse}
	}
	err := jobs.WithJob(ctx, w.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var next string
		if e := q.QueryRowContext(ctx, `SELECT control.tool_finish($1,$2,$3,$4,$5,$6,$7,$8)`, d.step, d.attempt, string(r.Outcome), r.Class, r.Retryable, r.Status, r.Simulated, body).Scan(&next); e != nil {
			return e
		}
		entry := d.evidence
		entry.Action = "tools.outcome"
		entry.State.ToolOutcome = string(r.Outcome) + "/" + r.Class
		entry.Allow = r.Outcome == tools.Success
		return w.Store.Audit.Append(ctx, q, entry)
	})
	if errors.Is(err, jobs.ErrJob) {
		return ErrLease
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "55000" {
		// A newer lease already settled or abandoned this attempt.
		return ErrLease
	}
	return err
}

// canonicalToolOutput serializes validated typed scalars deterministically.
func canonicalToolOutput(out map[string]any) ([]byte, error) {
	names := make([]string, 0, len(out))
	for k := range out {
		names = append(names, k)
	}
	sort.Strings(names)
	ordered := make(map[string]any, len(out))
	for _, k := range names {
		ordered[k] = out[k]
	}
	raw, err := json.Marshal(ordered)
	if err != nil {
		return nil, err
	}
	return canonicalOutput(raw)
}
