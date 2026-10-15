package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"regexp"
	"sort"
	"time"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,159}$`)

type stored struct {
	Instance
	program Program
	inputs  Snapshot
}

const selectInstance = `SELECT i.id::text,b.actor_id::text,b.app_id,b.metadata_revision,b.ir_hash,b.ir_body,b.workflow_id,b.workflow_version,b.plan_hash,b.plan_body,b.record_id::text,b.record_version,b.inputs,b.input_hash,b.request_id,b.traceparent,i.state_id,i.status,i.version,i.attempt,i.entered_at,i.due_at,i.deadline,i.termination,i.failure,coalesce(b.dataset_id::text,''),coalesce(b.binding_version,0),coalesce(b.data_generation,0),coalesce(b.data_revision,0),coalesce(b.data_hash,'') FROM runtime.workflow_instances i JOIN runtime.workflow_bindings b USING(tenant_id,id) WHERE i.tenant_id=$1::uuid AND i.id=$2::uuid`

func (s Store) load(ctx context.Context, q postgres.Executor, a tenant.Access, id string, lock bool) (stored, error) {
	var x stored
	x.Mode = "preview"
	var plan, inputs []byte
	var inputHash string
	if !s.configured() || q == nil || !tenant.ValidID(a.ActorID()) || !tenant.ValidID(a.TenantID()) || !tenant.ValidID(id) {
		return x, ErrInvalid
	}
	query := selectInstance
	if lock {
		query += " FOR UPDATE OF i"
	}
	err := q.QueryRowContext(ctx, query, a.TenantID(), id).Scan(&x.ID, &x.ActorID, &x.Pin.AppID, &x.Pin.Revision, &x.Pin.IRHash, &x.Pin.IR, &x.Pin.WorkflowID, &x.Pin.WorkflowVersion, &x.Pin.PlanHash, &plan, &x.Pin.RecordID, &x.Pin.RecordVersion, &inputs, &inputHash, &x.Pin.RequestID, &x.Pin.TraceParent, &x.StateID, &x.Status, &x.Version, &x.Attempt, &x.EnteredAt, &x.DueAt, &x.Deadline, &x.Termination, &x.Failure, &x.Pin.DatasetID, &x.Pin.BindingVersion, &x.Pin.DataGeneration, &x.Pin.DataRevision, &x.Pin.DataHash)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	if err != nil {
		return x, err
	}
	x.program, err = s.Restore(x.Pin.IR, x.Pin.IRHash, x.Pin.WorkflowID)
	if err != nil || x.program.Hash() != x.Pin.PlanHash || x.program.Plan().Version != x.Pin.WorkflowVersion || !equalBytes(x.program.JSON(), plan) || Hash(inputs) != inputHash || json.Unmarshal(inputs, &x.inputs) != nil {
		return stored{}, ErrInvalid
	}
	raw, _ := json.Marshal(x.inputs)
	if !equalBytes(raw, inputs) || len(inputs) > 65536 || validateSnapshot(x.program, x.inputs) != nil || x.inputs.RecordVersion != x.Pin.RecordVersion {
		return stored{}, ErrInvalid
	}
	if err = q.QueryRowContext(ctx, `SELECT control.workflow_route_state($1::uuid,$2::uuid)`, a.TenantID(), id).Scan(&x.RoutingState); err != nil {
		return x, err
	}
	return x, nil
}
func equalBytes(a, b []byte) bool { return string(a) == string(b) }
func (s Store) Get(ctx context.Context, q postgres.Executor, a tenant.Access, id string) (Instance, error) {
	x, e := s.load(ctx, q, a, id, false)
	if e != nil {
		return Instance{}, e
	}
	entry, e := s.Authorize(ctx, q, a, "workflows.read", x.Pin, x.inputs.Fields)
	if e != nil {
		return Instance{}, e
	}
	if e = s.Audit.Append(ctx, q, workflowEvidence(entry, x.Instance, "read", "")); e != nil {
		return Instance{}, e
	}
	rows, e := q.QueryContext(ctx, `SELECT sequence,state_id,status,event,actor_id::text,occurred_at,step_key,result_hash FROM runtime.workflow_history WHERE tenant_id=$1::uuid AND instance_id=$2::uuid ORDER BY sequence LIMIT 1000`, a.TenantID(), id)
	if e != nil {
		return Instance{}, e
	}
	defer rows.Close()
	x.History = []History{}
	for rows.Next() {
		var h History
		if e = rows.Scan(&h.Sequence, &h.StateID, &h.Status, &h.Event, &h.ActorID, &h.At, &h.StepKey, &h.ResultHash); e != nil {
			return Instance{}, e
		}
		x.History = append(x.History, h)
	}
	if e = rows.Err(); e != nil {
		return Instance{}, e
	}
	// Bounded reconciliation evidence: no request bodies, keys or responses.
	trows, e := q.QueryContext(ctx, `SELECT step_key,tool_ref,tool_version,mode,state,attempts,outcome_class,http_status,simulated FROM runtime.tool_invocations WHERE tenant_id=$1::uuid AND instance_id=$2::uuid ORDER BY step_key LIMIT 100`, a.TenantID(), id)
	if e != nil {
		return Instance{}, e
	}
	defer trows.Close()
	for trows.Next() {
		var t ToolStatus
		if e = trows.Scan(&t.StepKey, &t.ToolRef, &t.ToolVersion, &t.Mode, &t.State, &t.Attempts, &t.Class, &t.HTTPStatus, &t.Simulated); e != nil {
			return Instance{}, e
		}
		x.Tools = append(x.Tools, t)
	}
	return x.Instance, trows.Err()
}
func (s Store) Start(ctx context.Context, q postgres.Executor, a tenant.Access, r Start) (Instance, error) {
	if r.DatasetID == "" {
		if r.BindingVersion != 0 || r.DataGeneration != 0 || r.DataRevision != 0 || r.DataHash != "" {
			return Instance{}, ErrInvalid
		}
	} else if !tenant.ValidID(r.DatasetID) || r.BindingVersion < 1 || r.DataGeneration < 1 || r.DataRevision < 1 || len(r.DataHash) != 64 {
		return Instance{}, ErrInvalid
	}
	if !s.configured() || !tenant.ValidID(a.ActorID()) || !tenant.ValidID(a.TenantID()) || !tenant.ValidID(r.RecordID) || !keyPattern.MatchString(r.Key) || r.Revision < 1 || r.Revision > 1e9 || len(r.RequestID) > 64 || len(r.TraceParent) > 55 {
		return Instance{}, ErrInvalid
	}
	program, e := s.Restore(r.IR, r.IRHash, r.WorkflowID)
	if e != nil {
		return Instance{}, ErrInvalid
	}
	plan := program.Plan()
	if r.TraceParent == "" {
		carrier := propagation.MapCarrier{}
		(propagation.TraceContext{}).Inject(ctx, carrier)
		r.TraceParent = carrier.Get("traceparent")
	}
	trigger := r.Trigger
	if trigger == "" {
		trigger = "manual"
	}
	if plan.Trigger != trigger {
		return Instance{}, ErrInvalid
	}
	var app struct {
		Application struct {
			AppID string `json:"app_id"`
		} `json:"application"`
	}
	if json.Unmarshal(r.IR, &app) != nil || app.Application.AppID == "" {
		return Instance{}, ErrInvalid
	}
	pin := Pin{DataPin: r.DataPin, AppID: app.Application.AppID, Revision: r.Revision, IRHash: r.IRHash, WorkflowID: r.WorkflowID, WorkflowVersion: plan.Version, PlanHash: program.Hash(), RecordID: r.RecordID, IR: append([]byte(nil), r.IR...), RequestID: r.RequestID, TraceParent: r.TraceParent}
	request, _ := json.Marshal(struct {
		App, IR, Workflow, Record, Actor, Trigger string
		Revision                                  int64
	}{pin.AppID, pin.IRHash, pin.WorkflowID, pin.RecordID, a.ActorID(), trigger, pin.Revision})
	if pin.DatasetID != "" {
		request, _ = json.Marshal(struct {
			Request json.RawMessage
			Data    DataPin
		}{request, pin.DataPin})
	}
	requestHash := Hash(request)
	var existing, oldHash string
	e = q.QueryRowContext(ctx, `SELECT id::text,request_hash FROM runtime.workflow_bindings WHERE tenant_id=$1::uuid AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND workflow_id=$5 AND start_key=$6`, a.TenantID(), pin.AppID, pin.Revision, pin.IRHash, pin.WorkflowID, r.Key).Scan(&existing, &oldHash)
	if e == nil {
		if requestHash != oldHash {
			return Instance{}, ErrConflict
		}
		return s.Get(ctx, q, a, existing)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return Instance{}, e
	}
	snapshot, e := s.Capture(ctx, q, a, pin, program.FieldIDs())
	if e != nil {
		return Instance{}, e
	}
	if validateSnapshot(program, snapshot) != nil {
		return Instance{}, ErrInvalid
	}
	pin.RecordVersion = snapshot.RecordVersion
	entry, e := s.Authorize(ctx, q, a, "workflows.start", pin, snapshot.Fields)
	if e != nil {
		return Instance{}, e
	}
	inputs, e := json.Marshal(snapshot)
	if e != nil || len(inputs) > 65536 {
		return Instance{}, ErrInvalid
	}
	id, e := events.NewID()
	if e != nil {
		return Instance{}, e
	}
	// Conflict is returned, not retried automatically. A repeated request finds the
	// stored binding after commit even when the original COMMIT response was lost.
	result, e := q.ExecContext(ctx, `INSERT INTO runtime.workflow_bindings(tenant_id,id,app_id,metadata_revision,ir_hash,ir_body,workflow_id,workflow_version,plan_hash,plan_body,record_id,record_version,inputs,input_hash,start_key,request_hash,request_id,traceparent,dataset_id,binding_version,data_generation,data_revision,data_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23) ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,workflow_id,start_key) DO NOTHING`, a.TenantID(), id, pin.AppID, pin.Revision, pin.IRHash, []byte(pin.IR), pin.WorkflowID, pin.WorkflowVersion, pin.PlanHash, program.JSON(), pin.RecordID, pin.RecordVersion, inputs, Hash(inputs), r.Key, requestHash, r.RequestID, r.TraceParent, nullData(pin.DatasetID), nullNumber(pin.BindingVersion), nullNumber(pin.DataGeneration), nullNumber(pin.DataRevision), nullData(pin.DataHash))
	if e != nil {
		return Instance{}, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return Instance{}, e
	}
	if n != 1 {
		return Instance{}, ErrConflict
	}
	x := stored{Instance: Instance{Mode: "preview", ID: id, ActorID: a.ActorID(), Pin: pin, StateID: plan.InitialState, Status: "READY", Version: 1, RoutingState: "RUNNABLE"}, program: program, inputs: snapshot}
	e = q.QueryRowContext(ctx, `INSERT INTO runtime.workflow_instances(tenant_id,id,state_id,status,version,deadline) VALUES($1,$2,$3,'READY',1,clock_timestamp()+$4*interval '1 millisecond') RETURNING entered_at,due_at,deadline`, a.TenantID(), id, plan.InitialState, plan.TimeoutMS).Scan(&x.EnteredAt, &x.DueAt, &x.Deadline)
	if e != nil {
		return Instance{}, e
	}
	if e = s.emit(ctx, q, a, x, entry, "started", "", ""); e != nil {
		return Instance{}, e
	}
	return x.Instance, nil
}
func workflowEvidence(e audit.Entry, x Instance, event, step string) audit.Entry {
	e.Action = "workflows." + event
	e.Resource = "workflow"
	e.Allow = true
	e.State.WorkflowInstanceID = x.ID
	e.State.WorkflowID = x.Pin.WorkflowID
	e.State.WorkflowVersion = x.Pin.WorkflowVersion
	e.State.WorkflowPlanHash = x.Pin.PlanHash
	e.State.WorkflowSequence = x.Version
	e.State.WorkflowStep = step
	e.State.MetadataApp = x.Pin.AppID
	e.State.MetadataRevision = x.Pin.Revision
	e.State.IRHash = x.Pin.IRHash
	e.State.RecordID = x.Pin.RecordID
	e.State.RecordVersion = x.Pin.RecordVersion
	e.State.DatasetID = x.Pin.DatasetID
	e.State.DatasetBinding = x.Pin.BindingVersion
	e.State.DataSchemaGeneration = x.Pin.DataGeneration
	return e
}
func (s Store) emit(ctx context.Context, q postgres.Executor, a tenant.Access, x stored, entry audit.Entry, event, step, resultHash string) error {
	if x.Version > 1000 {
		return ErrInvalid
	}
	var at time.Time
	e := q.QueryRowContext(ctx, `INSERT INTO runtime.workflow_history(tenant_id,instance_id,sequence,state_id,status,event,step_key,result_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING occurred_at`, a.TenantID(), x.ID, x.Version, x.StateID, x.Status, event, step, resultHash).Scan(&at)
	if e != nil {
		return e
	}
	evidence := workflowEvidence(entry, x.Instance, event, step)
	if e = s.Audit.Append(ctx, q, evidence); e != nil {
		return e
	}
	if e = s.Events.AppendWorkflow(ctx, q, events.WorkflowEvent{SchemaVersion: "1.0.0", TenantID: a.TenantID(), InstanceID: x.ID, Sequence: x.Version, ActorID: a.ActorID(), IRHash: x.Pin.IRHash, WorkflowID: x.Pin.WorkflowID, WorkflowVersion: x.Pin.WorkflowVersion, PlanHash: x.Pin.PlanHash, StateID: x.StateID, Status: x.Status, Event: event, OccurredAt: at, RequestID: entry.RequestID, TraceID: trace.SpanContextFromContext(ctx).TraceID().String()}); e != nil {
		return e
	}
	_, e = q.ExecContext(ctx, `SELECT control.schedule_workflow($1::uuid,$2::uuid)`, a.TenantID(), x.ID)
	return e
}
func (s Store) save(ctx context.Context, q postgres.Executor, a tenant.Access, x *stored, entry audit.Entry, event, step, result string) error {
	if x.Version >= 1000 {
		return ErrInvalid
	}
	previous := x.Version
	x.Version++
	res, e := q.ExecContext(ctx, `UPDATE runtime.workflow_instances SET state_id=$3,status=$4,version=$5,attempt=$6,entered_at=$7,due_at=$8,termination=$9,failure=$10 WHERE tenant_id=$1 AND id=$2 AND version=$11`, a.TenantID(), x.ID, x.StateID, x.Status, x.Version, x.Attempt, x.EnteredAt, x.DueAt, x.Termination, x.Failure, previous)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrConflict
	}
	return s.emit(ctx, q, a, *x, entry, event, step, result)
}
func canonicalOutput(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 16384 {
		return nil, ErrInvalid
	}
	v, e := metadata.DecodeJSON(raw)
	if e != nil {
		return nil, ErrInvalid
	}
	b, e := json.Marshal(v)
	return b, e
}
func enter(x *stored, state string, now time.Time) {
	x.StateID = state
	x.Status = "READY"
	x.Attempt = 0
	x.EnteredAt = now
	x.DueAt = now
	x.Failure = ""
}
func validateSnapshot(p Program, s Snapshot) error {
	if len(s.Fields) < 1 || len(s.Fields) > 32 || len(s.Values) != len(s.Fields) || s.RecordVersion < 1 || !sort.StringsAreSorted(s.Fields) {
		return ErrInvalid
	}
	allowed := map[string]bool{}
	for _, f := range p.Plan().Fields {
		allowed[f.ID] = true
	}
	for i, id := range s.Fields {
		if !allowed[id] || (i > 0 && s.Fields[i-1] == id) || s.Values[id] == nil {
			return ErrInvalid
		}
		if _, e := metadata.DecodeJSON(s.Values[id]); e != nil {
			return ErrInvalid
		}
	}
	for _, id := range p.FieldIDs() {
		if !includes(s.Fields, id) {
			return ErrInvalid
		}
	}
	return nil
}

func nullData(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullNumber(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
