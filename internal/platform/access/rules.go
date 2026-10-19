package access

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"net/http"
)

func (a API) evaluateRule(ctx context.Context, q postgres.Executor, r *http.Request, u identity.User, access tenant.Access, evidence *audit.Entry, program compiler.Program, scope data.Scope, guard data.Guard, resolve data.ScopeResolver) (reply, error) {
	id := r.PathValue("rule")
	if len(id) > 160 {
		return denied(400, "invalid_rule", "Invalid rule selection"), nil
	}
	executable, e := program.Rule(id)
	if e != nil {
		return denied(422, "rule_unavailable", "A compiled Rule V2 definition is required"), nil
	}
	plan := executable.Plan()
	evidence.Action = "rules.evaluate"
	evidence.Resource = "rule"
	evidence.State.RuleID = id
	evidence.State.RuleHash = executable.Hash()
	evidence.State.RuleVersion = rules.Version
	evidence.State.EntityID = plan.EntityID
	decision := enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, "rules.evaluate", policy.Resource{TenantID: access.TenantID(), Type: "rule", AppID: scope.AppID(), EntityID: plan.EntityID, RuleID: id}, policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}))
	evidence.Allow = decision.Allow
	evidence.DecisionID = decision.ID
	evidence.PolicyVersion = decision.Version
	if !decision.Allow {
		return denied(403, "forbidden", "Rule evaluation is denied"), nil
	}
	var request struct {
		RecordID string `json:"record_id"`
	}
	if readDataCommand(r, &request) != nil || !tenant.ValidID(request.RecordID) {
		return denied(400, "invalid_rule_input", "A valid record selection is required"), nil
	}
	projection := executable.FieldIDs()
	if len(projection) == 0 {
		entity, ok := program.Entity(plan.EntityID)
		if !ok || len(entity.Fields) == 0 {
			return reply{}, rules.ErrInvalid
		}
		projection = []string{entity.Fields[0].ID}
		for _, f := range entity.Fields {
			if f.CoreID {
				projection = []string{f.ID}
				break
			}
		}
	}
	page, e := (data.Store{}).Query(ctx, q, scope, access, plan.EntityID, data.Query{RecordID: request.RecordID, Projection: projection, Limit: 1}, guard, resolve)
	if errors.Is(e, data.ErrDenied) {
		return denied(403, "forbidden", "Rule inputs are denied"), nil
	}
	if errors.Is(e, data.ErrInvalid) || errors.Is(e, data.ErrBudget) {
		return denied(400, "invalid_rule_input", "Invalid rule input selection"), nil
	}
	if e != nil {
		return reply{}, e
	}
	if len(page.Records) != 1 {
		return denied(404, "not_found", "Rule input record not found"), nil
	}
	record := page.Records[0]
	evidence.State.RecordID = record.ID
	evidence.State.RecordVersion = record.Version
	decision = enforce(a.Policy.Decide(ctx, policy.Subject{User: u, Access: access}, "rules.evaluate", policy.Resource{TenantID: access.TenantID(), Type: "rule", AppID: scope.AppID(), EntityID: plan.EntityID, RuleID: id, RecordID: record.ID, OwnerID: record.OwnerID}, policy.Context{RequestID: evidence.RequestID, TraceID: evidence.TraceID}))
	evidence.Allow = decision.Allow
	evidence.DecisionID = decision.ID
	evidence.PolicyVersion = decision.Version
	if !decision.Allow {
		return denied(403, "forbidden", "Record rule evaluation is denied"), nil
	}
	values := map[string]json.RawMessage{}
	for _, id := range executable.FieldIDs() {
		raw, err := json.Marshal(record.Values[id])
		if err != nil {
			return reply{}, err
		}
		values[id] = raw
	}
	evidence.State.RecordID = record.ID
	evidence.State.RecordVersion = record.Version
	ctx, span := trace.SpanFromContext(ctx).TracerProvider().Tracer("framework/rules").Start(ctx, "rules.evaluate")
	defer span.End()
	span.SetAttributes(attribute.String("rule_id", plan.ID), attribute.String("rule_hash", executable.Hash()), attribute.String("rule_version", rules.Version))
	result, e := executable.Evaluate(ctx, values)
	if e != nil {
		if ctx.Err() != nil {
			return reply{}, ctx.Err()
		}
		return denied(422, "rule_evaluation_failed", "Rule inputs violate evaluation constraints"), nil
	}
	// Outer serve commits mandatory audit before disclosing the decision. No writes/effects.
	return reply{status: 200, body: struct {
		Result        rules.Value `json:"result"`
		RuleHash      string      `json:"rule_hash"`
		IRHash        string      `json:"ir_hash"`
		RecordVersion int64       `json:"record_version"`
	}{result, executable.Hash(), program.Hash(), record.Version}}, nil
}
