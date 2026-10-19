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
	state, class, lastKind, requestHash string
	// outstanding: some send's outcome is still unknown (M10R-01). Only a
	// success, a lookup that proved absence or an operator decision clears it.
	retryable, outstanding bool
	attempts               int
	firstSent              time.Time
	// lastFinished is when the latest attempt finished (zero while unfinished);
	// lastSendStarted is when the latest SEND attempt began.
	lastFinished, lastSendStarted time.Time
	response                      []byte
}

// MaxLedgerAttempts is the database attempt budget of one tool step
// (runtime.tool_invocations.attempts); reaching it settles the step.
const MaxLedgerAttempts = 20

// ToolContractOf derives the database-enforced contract pinned beside a
// definition hash (ADR036 Amendment A1).
func ToolContractOf(d tools.Definition) ToolContract {
	c := ToolContract{SideEffect: d.SideEffect, Approval: d.Approval, IdempotencyMode: d.Delivery.IdempotencyMode, RetryMode: d.Delivery.RetryMode, ReplayWindowMS: d.Delivery.ReplayWindowMS, TimeoutMS: d.Connector.TimeoutMS}
	if d.Connector.Lookup != nil {
		c.SettleMS = d.Connector.Lookup.SettleMS
	}
	return c
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
	var finished, sendStarted sql.NullTime
	err := q.QueryRowContext(ctx, `SELECT v.state,v.outcome_class,v.retryable,v.attempts,v.first_sent_at,v.response,v.request_hash,
 coalesce(v.unknown_outstanding,v.state IN('UNKNOWN','SENDING') OR v.state NOT IN('SUCCEEDED','APPLIED','NOT_APPLIED') AND EXISTS(SELECT 1 FROM runtime.tool_attempts u WHERE u.tenant_id=v.tenant_id AND u.instance_id=v.instance_id AND u.step_key=v.step_key AND (u.outcome='UNKNOWN_REQUIRES_RECONCILIATION' OR u.finished_at IS NULL))),
 coalesce(t.kind,''),t.finished_at,(SELECT max(s.started_at) FROM runtime.tool_attempts s WHERE s.tenant_id=v.tenant_id AND s.instance_id=v.instance_id AND s.step_key=v.step_key AND s.kind='SEND')
 FROM runtime.tool_invocations v LEFT JOIN LATERAL (SELECT x.kind,x.finished_at FROM runtime.tool_attempts x WHERE x.tenant_id=v.tenant_id AND x.instance_id=v.instance_id AND x.step_key=v.step_key ORDER BY x.attempt DESC LIMIT 1) t ON true
 WHERE v.tenant_id=$1 AND v.instance_id=$2 AND v.step_key=$3`, a.TenantID(), instance, step).Scan(&l.state, &l.class, &l.retryable, &l.attempts, &l.firstSent, &l.response, &l.requestHash, &l.outstanding, &l.lastKind, &finished, &sendStarted)
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	l.lastFinished, l.lastSendStarted = finished.Time, sendStarted.Time
	return l, err == nil, err
}

// decideTool applies the pinned delivery capability to the durable ledger.
// While an unknown outcome is outstanding it never resends unless the
// capability permits it: a lookup (LOOKUP_BY_REFERENCE), an in-window keyed
// resend whose deadline also ends inside the replay window (NATIVE_KEY,
// DEDUPE_WINDOW) or a safe read retry; a failed lookup is retried as a lookup
// (M10R-01). The database enforces the same admission (control.tool_begin).
//
// A fresh outcome (the latest attempt finished after the instance's current
// due time, i.e. it was not yet routed through the failure path) is always
// returned as a failure, so recordFailure counts it, backs off and applies
// max_attempts (UNKNOWN at the limit parks in RECONCILE) (M10R-03).
func decideTool(c ToolContract, l toolLedger, found bool, x stored, now time.Time) (string, error) {
	if !found || l.state == "NOT_APPLIED" && !l.outstanding {
		return tools.KindSend, nil
	}
	if l.state != "UNKNOWN" && l.state != "FAILED" {
		return "", ErrInvalid
	}
	fresh := !l.lastFinished.Before(x.DueAt)
	if !l.outstanding {
		if l.state != "FAILED" {
			return "", ErrInvalid
		}
		if !l.retryable || c.RetryMode == "NEVER" || l.attempts >= MaxLedgerAttempts {
			return "", Permanent()
		}
		if fresh {
			return "", Retryable()
		}
		return tools.KindSend, nil
	}
	next := ""
	switch c.IdempotencyMode {
	case "LOOKUP_BY_REFERENCE":
		next = tools.KindLookup
	case "NATIVE_KEY", "DEDUPE_WINDOW":
		if inReplayWindow(c, l, now) {
			next = tools.KindSend
		}
	default:
		if (c.SideEffect == "NONE" || c.SideEffect == "READ") && c.RetryMode == "SAFE_RETRY" {
			next = tools.KindSend
		}
	}
	if next == "" || l.attempts >= MaxLedgerAttempts {
		return "", Unknown()
	}
	if fresh {
		return "", Retryable()
	}
	return next, nil
}

// inReplayWindow admits a keyed resend only when its whole send deadline ends
// inside the provider's replay window measured from the first send (C6).
func inReplayWindow(c ToolContract, l toolLedger, now time.Time) bool {
	window := time.Duration(c.ReplayWindowMS) * time.Millisecond
	return c.RetryMode == "SAFE_RETRY" && window > 0 && now.Add(time.Duration(c.TimeoutMS)*time.Millisecond).Before(l.firstSent.Add(window))
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
func (s Store) toolActivity(ctx context.Context, q postgres.Executor, a tenant.Access, x *stored, state State, call ActivityRequest, now time.Time, pre *[2]audit.Entry) (*toolDispatch, error) {
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
	fail := func(e error) error { return activityFailure{err: e, instanceVersion: x.Version, step: call.StepKey} }
	// The ledger is read before the catalog fail-closed check (M10R-04): an
	// unsettled or settled send is recorded whatever the catalog says now.
	l, found, err := loadToolLedger(ctx, q, a, x.ID, call.StepKey)
	if err != nil {
		return nil, err
	}
	if found && l.state == "SENDING" {
		// A send whose outcome was never recorded is UNKNOWN. The abandonment must
		// commit on its own (this transaction ends in a recorded failure), so the
		// failure path settles it before any resend is considered.
		return nil, activityFailure{err: Retryable(), instanceVersion: x.Version, step: call.StepKey, abandon: true}
	}
	if found && l.state == "SUCCEEDED" {
		// The settled ledger must belong to this exact step request (M10R-02).
		if l.requestHash != call.RequestHash {
			return nil, fail(Permanent())
		}
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
	def, ok := s.ToolCatalog.Lookup(t.ref, t.version, t.hash)
	if ok && state.ToolContract != nil && ToolContractOf(def) != *state.ToolContract {
		// The pinned contract is derived from the pinned definition; any
		// disagreement is an integrity failure, never a reason to send.
		ok = false
	}
	if !ok || !def.AllowsTenant(a.TenantID()) || s.Tools == nil {
		// A removed/changed/unlicensed tool fails closed before any I/O; with an
		// unknown outcome outstanding the failure path parks it in RECONCILE.
		return nil, fail(Permanent())
	}
	contract := ToolContractOf(def)
	if state.ToolContract != nil {
		contract = *state.ToolContract
	}
	kind, decision := decideTool(contract, l, found, *x, now)
	if decision != nil {
		return nil, fail(decision)
	}
	inputs, err := toolInputs(def, state.Activity.Inputs, x.inputs)
	if err != nil {
		return nil, fail(Permanent())
	}
	var activityEntry, invokeEntry audit.Entry
	if pre != nil {
		activityEntry, invokeEntry = pre[0], pre[1]
	} else if activityEntry, invokeEntry, err = s.authorizeTool(ctx, q, a, *x); err != nil {
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
	if errors.As(err, &pe) && pe.Code == "TL003" {
		// The database refused a resend while an unknown outcome is outstanding.
		return nil, fail(Unknown())
	}
	if errors.As(err, &pe) && pe.Code == "TL004" {
		// The ledger attempt budget is a settled failure, never a livelock.
		if l.outstanding {
			return nil, fail(Unknown())
		}
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

// authorizeTool requires both workflows.activity and tools.invoke. One read of
// the leased record serves both decisions, keeping the dispatch transaction
// inside the 3 s runtime budget; each decision keeps its own evidence.
func (s Store) authorizeTool(ctx context.Context, q postgres.Executor, a tenant.Access, x stored) (activity, invoke audit.Entry, err error) {
	if s.AuthorizeActions != nil {
		entries, e := s.AuthorizeActions(ctx, q, a, []string{"workflows.activity", "tools.invoke"}, x.Pin, x.inputs.Fields)
		if e != nil || len(entries) != 2 {
			if e == nil {
				e = ErrDenied
			}
			return audit.Entry{}, audit.Entry{}, e
		}
		return entries[0], entries[1], nil
	}
	if activity, err = s.Authorize(ctx, q, a, "workflows.activity", x.Pin, x.inputs.Fields); err != nil {
		return audit.Entry{}, audit.Entry{}, err
	}
	if invoke, err = s.Authorize(ctx, q, a, "tools.invoke", x.Pin, x.inputs.Fields); err != nil {
		return audit.Entry{}, audit.Entry{}, err
	}
	return activity, invoke, nil
}

// authorizeStep decides the step's own action. For a tool activity it also
// decides workflows.activity and tools.invoke over the same single read of the
// leased record, so the dispatch transaction stays inside the 3 s runtime
// budget. Each decision keeps its own audit evidence; a denial of any of them
// stops the step exactly as a sequential denial would.
func (s Store) authorizeStep(ctx context.Context, q postgres.Executor, a tenant.Access, x stored) (audit.Entry, *[2]audit.Entry, error) {
	if state, ok := x.program.State(x.StateID); ok && s.AuthorizeActions != nil && x.Termination == "" && x.Status != "COMPENSATING" && state.Type == "activity" && state.IsTool() {
		entries, err := s.AuthorizeActions(ctx, q, a, []string{"workflows.execute", "workflows.activity", "tools.invoke"}, x.Pin, x.inputs.Fields)
		if err != nil || len(entries) != 3 {
			if err == nil {
				err = ErrDenied
			}
			if len(entries) > 0 {
				return entries[len(entries)-1], nil, err
			}
			return audit.Entry{}, nil, err
		}
		return entries[0], &[2]audit.Entry{entries[1], entries[2]}, nil
	}
	entry, err := s.Authorize(ctx, q, a, "workflows.execute", x.Pin, x.inputs.Fields)
	return entry, nil, err
}
