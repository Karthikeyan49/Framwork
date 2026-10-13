package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

var workflowsPOST = regexp.MustCompile(`^/v1/tenants/[0-9a-f-]{36}/preview/app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/versions/[0-9]+/workflows/workflow\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*/(start|instances/[0-9a-f-]{36}/signals)$`)

type WorkflowUser func(context.Context, postgres.Executor, tenant.Access) (identity.User, error)

// WorkflowStore is platform composition: workflow depends on authorization and
// snapshot ports, not the data runtime/compiler's implementation or private tables.
func WorkflowStore(engine policy.Engine, writer audit.Writer, lifecycle events.LifecycleWriter, user WorkflowUser) workflow.Store {
	return workflowStore(engine, writer, lifecycle, user, false)
}

// WorkerWorkflowStore uses the database's confined source port, without generic
// record privileges. API capture continues through WorkflowStore.
func WorkerWorkflowStore(engine policy.Engine, writer audit.Writer, lifecycle events.LifecycleWriter, user WorkflowUser) workflow.Store {
	return workflowStore(engine, writer, lifecycle, user, true)
}

func workflowStore(engine policy.Engine, writer audit.Writer, lifecycle events.LifecycleWriter, user WorkflowUser, worker bool) workflow.Store {
	restore := func(raw []byte, hash, id string) (workflow.Program, error) {
		p, e := compiler.Restore(raw, hash, compiler.SupportedIRVersions())
		if e != nil {
			return workflow.Program{}, e
		}
		return p.Workflow(id)
	}
	record := func(ctx context.Context, q postgres.Executor, a tenant.Access, pin workflow.Pin, fields []string) (data.Record, error) {
		if engine == nil || writer == nil || user == nil {
			return data.Record{}, workflow.ErrDenied
		}
		u, e := user(ctx, q, a)
		if e != nil || u.ID() != a.ActorID() {
			return data.Record{}, workflow.ErrDenied
		}
		p, e := compiler.Restore(pin.IR, pin.IRHash, compiler.SupportedIRVersions())
		if e != nil || p.IR().Application.AppID != pin.AppID {
			return data.Record{}, workflow.ErrInvalid
		}
		w, e := p.Workflow(pin.WorkflowID)
		if e != nil || w.Hash() != pin.PlanHash || w.Plan().Version != pin.WorkflowVersion {
			return data.Record{}, workflow.ErrInvalid
		}
		scope, e := data.NewScope(p, pin.Revision)
		if e != nil {
			return data.Record{}, e
		}
		entity := w.Plan().EntityID
		if len(fields) == 0 {
			model, ok := p.Entity(entity)
			if !ok || len(model.Fields) == 0 {
				return data.Record{}, workflow.ErrInvalid
			}
			fields = []string{model.Fields[0].ID}
			for _, f := range model.Fields {
				if f.CoreID {
					fields = []string{f.ID}
					break
				}
			}
		}
		evidence := audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), RequestID: pin.RequestID, TraceID: trace.SpanContextFromContext(ctx).TraceID().String(), State: audit.State{MetadataApp: pin.AppID, MetadataRevision: pin.Revision, IRHash: pin.IRHash, WorkflowID: pin.WorkflowID, WorkflowVersion: pin.WorkflowVersion, WorkflowPlanHash: pin.PlanHash}}
		guard := func(ctx context.Context, action string, r data.Resource) error {
			d := enforce(engine.Decide(ctx, policy.Subject{User: u, Access: a}, action, policy.Resource{TenantID: a.TenantID(), Type: strings.Split(action, ".")[1], OwnerID: r.OwnerID, AppID: pin.AppID, EntityID: r.EntityID, FieldID: r.FieldID, RecordID: r.RecordID}, policy.Context{RequestID: pin.RequestID, TraceID: evidence.TraceID}))
			ev := evidence
			ev.Action = action
			ev.Resource = strings.Split(action, ".")[1]
			ev.Allow = d.Allow
			ev.DecisionID = d.ID
			ev.PolicyVersion = d.Version
			ev.State.EntityID = r.EntityID
			ev.State.FieldID = r.FieldID
			ev.State.RecordID = r.RecordID
			if e := writer.Append(ctx, q, ev); e != nil {
				return e
			}
			if !d.Allow {
				return workflow.ErrDenied
			}
			return nil
		}
		teams, e := (tenant.Store{}).Teams(ctx, q, a)
		if e != nil {
			return data.Record{}, e
		}
		resolve := func(ctx context.Context, action string, r data.Resource) (policy.DataScope, error) {
			scoped, ok := engine.(policy.ScopeEngine)
			if !ok {
				return policy.DataScope{}, workflow.ErrDenied
			}
			kind := "entity"
			if r.FieldID != "" {
				kind = "field"
			}
			scope, e := scoped.DataScope(ctx, policy.Subject{User: u, Access: a}, action, policy.Resource{TenantID: a.TenantID(), Type: kind, AppID: pin.AppID, EntityID: r.EntityID, FieldID: r.FieldID}, policy.ScopeContext{Teams: teams, RequestID: pin.RequestID, TraceID: evidence.TraceID})
			entry := evidence
			entry.Action = "workflows.data_scope"
			entry.Resource = kind
			entry.Allow = e == nil
			entry.State.EntityID = r.EntityID
			entry.State.FieldID = r.FieldID
			raw, _ := json.Marshal(scope)
			hash := sha256.Sum256(raw)
			entry.State.ScopeHash = hex.EncodeToString(hash[:])
			if er := writer.Append(ctx, q, entry); er != nil {
				return policy.DataScope{}, er
			}
			if e != nil {
				return policy.DataScope{}, workflow.ErrDenied
			}
			return scope, nil
		}
		query := data.Query{RecordID: pin.RecordID, Projection: fields, Limit: 1}
		var page data.Page
		if worker {
			page, e = (data.WorkerSourceAdapter{}).Query(ctx, q, scope, a, entity, query, guard, resolve)
		} else {
			page, e = (data.Store{}).Query(ctx, q, scope, a, entity, query, guard, resolve)
		}
		if errors.Is(e, data.ErrDenied) {
			return data.Record{}, workflow.ErrDenied
		}
		if e != nil {
			return data.Record{}, e
		}
		if len(page.Records) != 1 {
			return data.Record{}, workflow.ErrNotFound
		}
		return page.Records[0], nil
	}
	capture := func(ctx context.Context, q postgres.Executor, a tenant.Access, pin workflow.Pin, fields []string) (workflow.Snapshot, error) {
		r, e := record(ctx, q, a, pin, fields)
		if e != nil {
			return workflow.Snapshot{}, e
		}
		values := map[string]json.RawMessage{}
		ids := []string{}
		for id, v := range r.Values {
			b, e := json.Marshal(v)
			if e != nil {
				return workflow.Snapshot{}, e
			}
			values[id] = b
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return workflow.Snapshot{Fields: ids, Values: values, RecordVersion: r.Version}, nil
	}
	authorize := func(ctx context.Context, q postgres.Executor, a tenant.Access, action string, pin workflow.Pin, fields []string) (audit.Entry, error) {
		if user == nil || engine == nil {
			return audit.Entry{}, workflow.ErrDenied
		}
		u, e := user(ctx, q, a)
		if e != nil || u.ID() != a.ActorID() {
			return audit.Entry{}, workflow.ErrDenied
		}
		p, e := restore(pin.IR, pin.IRHash, pin.WorkflowID)
		if e != nil {
			return audit.Entry{}, e
		}
		source, e := record(ctx, q, a, pin, fields)
		if e != nil {
			return audit.Entry{}, e
		}
		d := enforce(engine.Decide(ctx, policy.Subject{User: u, Access: a}, action, policy.Resource{TenantID: a.TenantID(), Type: "workflow", OwnerID: source.OwnerID, AppID: pin.AppID, EntityID: p.Plan().EntityID, RecordID: pin.RecordID, WorkflowID: pin.WorkflowID}, policy.Context{RequestID: pin.RequestID, TraceID: trace.SpanContextFromContext(ctx).TraceID().String()}))
		entry := audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), Action: action, Resource: "workflow", Allow: d.Allow, DecisionID: d.ID, PolicyVersion: d.Version, RequestID: pin.RequestID, TraceID: trace.SpanContextFromContext(ctx).TraceID().String(), State: audit.State{EntityID: p.Plan().EntityID}}
		if !d.Allow {
			if writer != nil {
				if e := writer.Append(ctx, q, entry); e != nil {
					return entry, e
				}
			}
			return entry, &workflow.AuthorizationDenied{Evidence: entry}
		}
		return entry, nil
	}
	return workflow.Store{Restore: restore, Capture: capture, Authorize: authorize, Audit: writer, Events: lifecycle, Activity: workflow.Noop}
}

func (a API) previewWorkflow(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry, p compiler.Program, revision int64) (reply, error) {
	id := r.PathValue("workflow")
	if len(id) > 160 {
		return denied(400, "invalid_workflow", "Invalid workflow selection"), nil
	}
	if r.PathValue("instance") == "" {
		if executable, e := p.Workflow(id); e != nil || executable.Plan().Trigger != "manual" {
			return denied(422, "workflow_unavailable", "A compiled Workflow V2 definition is required"), nil
		}
	}
	lifecycle := a.WorkflowEvents
	if lifecycle == nil {
		lifecycle = events.Postgres{}
	}
	store := WorkflowStore(a.Policy, a.Audit, lifecycle, func(context.Context, postgres.Executor, tenant.Access) (identity.User, error) { return u, nil })
	var x workflow.Instance
	var err error
	instance := r.PathValue("instance")
	if instance == "" {
		var body struct {
			RecordID string `json:"record_id"`
			Key      string `json:"idempotency_key"`
		}
		if readDataCommand(r, &body) != nil {
			return denied(400, "invalid_workflow_input", "A record and idempotency key are required"), nil
		}
		x, err = store.Start(ctx, q, access, workflow.Start{IR: p.JSON(), IRHash: p.Hash(), Revision: revision, WorkflowID: id, RecordID: body.RecordID, Key: body.Key, RequestID: httpserver.RequestID(ctx)})
	} else {
		x, err = store.Get(ctx, q, access, instance)
		if err == nil && (x.Pin.AppID != r.PathValue("app") || x.Pin.Revision != revision || x.Pin.WorkflowID != id) {
			return denied(404, "not_found", "Workflow not found in this selection"), nil
		}
		if err == nil && r.Method != http.MethodGet {
			var body workflow.Signal
			if readDataCommand(r, &body) != nil {
				return denied(400, "invalid_workflow_signal", "Invalid workflow operation"), nil
			}
			x, err = store.Signal(ctx, q, access, instance, body)
		}
	}
	switch {
	case err == nil:
		return reply{status: 200, body: x}, nil
	case errors.Is(err, workflow.ErrInvalid):
		return reply{}, &requestRejection{response: denied(400, "invalid_workflow_input", "Workflow request violates its typed contract")}
	case errors.Is(err, workflow.ErrDenied):
		rejected := &requestRejection{response: denied(403, "forbidden", "Workflow operation is denied")}
		var denied *workflow.AuthorizationDenied
		if errors.As(err, &denied) {
			entry := denied.Evidence
			rejected.evidence = &entry
		}
		return reply{}, rejected
	case errors.Is(err, workflow.ErrNotFound):
		return reply{}, &requestRejection{response: denied(404, "not_found", "Workflow or input record not found")}
	case errors.Is(err, workflow.ErrConflict):
		return reply{}, &requestRejection{response: denied(409, "workflow_conflict", "Reload workflow state before retrying")}
	default:
		return reply{}, err
	}
}
