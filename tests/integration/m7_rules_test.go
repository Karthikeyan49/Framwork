//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"net/http"
	"os"
	"strings"
	"testing"
)

func m7Fixture(t *testing.T) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f := newIAM(t)
	h := m5Handler(t, f, policy.Native{}, audit.Postgres{}, true)
	raw, e := os.ReadFile("../../contracts/examples/contacts-rules-v2.json")
	if e != nil {
		t.Fatal(e)
	}
	app := "app.rules_" + strings.ReplaceAll(uuid(t), "-", "")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	token := f.token(t, f.aliceSubject, nil)
	w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 0, raw)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var version metadata.Version
	_ = json.Unmarshal(w.Body.Bytes(), &version)
	if version.SchemaVersion != metadata.RuleSchemaVersion {
		t.Fatal("schema column not derived", version.SchemaVersion)
	}
	return f, h, "/v1/tenants/" + f.tenantA + "/preview/" + app + "/versions/1", token, app
}
func m7Record(t *testing.T, h http.Handler, base, token, amount string) data.Record {
	t.Helper()
	values := m5Values("Rule fixture")
	values["field.contact.balance"] = map[string]any{"amount": amount, "currency": "USD"}
	values["field.contact.follow_up"] = "2026-10-13"
	w := m5Call(h, base+"/entities/entity.contact/commands", token, data.Command{Operation: "create", Values: values})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r data.Record
	if json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal("record")
	}
	return r
}
func TestM7RealScopedRuleMoneyCalendarEvidenceAndNoMutation(t *testing.T) {
	f, h, base, token, app := m7Fixture(t)
	var countBefore int
	if e := f.operator.QueryRow(`SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&countBefore); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		amount   string
		expected bool
	}{{"499.999999999999999999", false}, {"500", false}, {"500.000000000000000001", true}} {
		record := m7Record(t, h, base, token, tc.amount)
		path := base + "/rules/rule.contact.approval_required/evaluate"
		w := m5Call(h, path, f.token(t, f.viewerSubject, nil), map[string]string{"record_id": record.ID})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		var result struct {
			Result struct {
				Value bool `json:"value"`
			} `json:"result"`
			RuleHash      string `json:"rule_hash"`
			IRHash        string `json:"ir_hash"`
			RecordVersion int64  `json:"record_version"`
		}
		if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Result.Value != tc.expected || len(result.RuleHash) != 64 || len(result.IRHash) != 64 || result.RecordVersion != 1 {
			t.Fatal("wrong exact threshold", w.Body.String())
		}
		again := m5Call(h, path, f.token(t, f.viewerSubject, nil), map[string]string{"record_id": record.ID})
		if again.Code != 200 || again.Body.String() != w.Body.String() {
			t.Fatal("evaluation not deterministic")
		}
		var audited int
		e := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=$1 AND action='rules.evaluate' AND allowed AND state->>'rule_hash'=$2 AND state->>'rule_id'='rule.contact.approval_required' AND NOT (state ? 'result')`, w.Header().Get("X-Request-ID"), result.RuleHash).Scan(&audited)
		if e != nil || audited != 1 {
			t.Fatal("rule evidence", e, audited)
		}
		date := m5Call(h, base+"/rules/rule.contact.next_business_date/evaluate", token, map[string]string{"record_id": record.ID})
		if date.Code != 200 || !strings.Contains(date.Body.String(), `"value":"2026-10-16"`) {
			t.Fatal("calendar execution", date.Code, date.Body.String())
		}
		var version int64
		if e := f.operator.QueryRow(`SELECT row_version FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID).Scan(&version); e != nil || version != 1 {
			t.Fatal("rule mutated record", e)
		}
	}
	var countAfter int
	if e := f.operator.QueryRow(`SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&countAfter); e != nil || countAfter-countBefore != 3 {
		t.Fatal("rule emitted events", e, countAfter-countBefore)
	}
}

type m7ScopePolicy struct{ hideField bool }

func (m7ScopePolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	return (policy.Native{}).Decide(ctx, s, a, r, c)
}
func (p m7ScopePolicy) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	if p.hideField && r.FieldID == "field.contact.balance" {
		return policy.DataScope{Kind: "NONE"}, nil
	}
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}

type failedRuleAudit struct{}

type m7RecordRulePolicy struct{ DeniedRecord, AllowedOwner string }

func (p m7RecordRulePolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	d := (policy.Native{}).Decide(ctx, s, a, r, c)
	if a == "rules.evaluate" && r.RecordID != "" && (r.RecordID == p.DeniedRecord || p.AllowedOwner != "" && r.OwnerID != p.AllowedOwner) {
		d.Allow = false
	}
	return d
}
func (m7RecordRulePolicy) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}

func TestM7RecordRulePermissionUsesActualRecordAndOwner(t *testing.T) {
	f, h, base, token, app := m7Fixture(t)
	record := m7Record(t, h, base, token, "501")
	path := base + "/rules/rule.contact.approval_required/evaluate"
	request := map[string]string{"record_id": record.ID}
	blocked := m5Handler(t, f, m7RecordRulePolicy{DeniedRecord: record.ID}, audit.Postgres{}, true)
	if w := m5Call(blocked, path, token, request); w.Code != 403 {
		t.Fatal("record rule decision skipped", w.Code)
	}
	if e := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Rule owner fixture", f.bob, "member", true); e != nil {
		t.Fatal(e)
	}
	tx, e := f.operator.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(`UPDATE runtime.records SET owner_user_id=$1 WHERE tenant_id=$2 AND app_id=$3 AND id=$4`, f.bob, f.tenantA, app, record.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	ownerGate := m5Handler(t, f, m7RecordRulePolicy{AllowedOwner: f.alice}, audit.Postgres{}, true)
	if w := m5Call(ownerGate, path, token, request); w.Code != 403 {
		t.Fatal("creator used in place of actual owner", w.Code)
	}
	ownerGate = m5Handler(t, f, m7RecordRulePolicy{AllowedOwner: f.bob}, audit.Postgres{}, true)
	if w := m5Call(ownerGate, path, token, request); w.Code != 200 {
		t.Fatal("actual owner absent", w.Code, w.Body.String())
	}
}

func (failedRuleAudit) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if e.Action == "rules.evaluate" {
		return errors.New("fixture rule evidence unavailable")
	}
	return (audit.Postgres{}).Append(ctx, q, e)
}
func TestM7PreviewRuleAuthorizationIsolationAndAuditFailClosed(t *testing.T) {
	f, h, base, token, _ := m7Fixture(t)
	record := m7Record(t, h, base, token, "501")
	path := base + "/rules/rule.contact.approval_required/evaluate"
	request := map[string]string{"record_id": record.ID}
	for _, tc := range []struct {
		policy policy.Engine
		writer audit.Writer
		code   int
	}{
		{denyDataPolicy{action: "rules.evaluate"}, audit.Postgres{}, 403}, {denyDataPolicy{action: "data.field.read", field: "field.contact.balance"}, audit.Postgres{}, 403},
		{m7ScopePolicy{hideField: true}, audit.Postgres{}, 404}, {policy.Native{}, failedRuleAudit{}, 503},
	} {
		w := m5Call(m5Handler(t, f, tc.policy, tc.writer, true), path, token, request)
		if w.Code != tc.code || strings.Contains(w.Body.String(), `"result"`) {
			t.Fatal("rule authorization/audit bypass", w.Code, w.Body.String())
		}
	}
	if e := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Rule fixture", f.bob, "member", true); e != nil {
		t.Fatal(e)
	}
	ownOnly := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	if w := m5Call(ownOnly, path, f.token(t, f.bobSubject, nil), request); w.Code != 404 {
		t.Fatal("ownership input leak", w.Code)
	}
	if w := m5Call(h, strings.Replace(path, f.tenantA, f.tenantB, 1), f.token(t, f.bobSubject, nil), request); w.Code != 404 {
		t.Fatal("foreign tenant metadata leak", w.Code)
	}
	if w := m5Call(h, path, "", request); w.Code != 401 {
		t.Fatal("unauthenticated", w.Code)
	}
	if w := m5Call(h, path, token, map[string]any{"record_id": record.ID, "expression": map[string]string{"op": "http"}}); w.Code != 400 {
		t.Fatal("inline execution accepted", w.Code)
	}
	if w := m5Call(h, path, token, map[string]string{"record_id": uuid(t)}); w.Code != 404 {
		t.Fatal("unknown record", w.Code)
	}
	if w := m5Call(m5Handler(t, f, policy.Native{}, audit.Postgres{}, false), path, token, request); (w.Code != 404 && w.Code != 413) || strings.Contains(w.Body.String(), `"result"`) {
		t.Fatal("production rule preview exposed", w.Code)
	}
}
