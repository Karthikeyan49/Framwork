//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"testing"
)

// The action engine and SQL scope engine are deliberately distinct: action
// permission does not mean every record/field is visible.
type scopedPolicy struct{ mode string }

func (p scopedPolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	return (policy.Native{}).Decide(ctx, s, a, r, c)
}
func (p scopedPolicy) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	if _, e := (policy.Native{}).DataScope(ctx, s, a, r, c); e != nil {
		return policy.DataScope{}, e
	}
	switch p.mode {
	case "owner":
		return policy.DataScope{Kind: "OWNER"}, nil
	case "team":
		return policy.DataScope{Kind: "TEAM", Teams: c.Teams}, nil
	case "share":
		return policy.DataScope{Kind: "SHARE", WriteShare: a == "data.write"}, nil
	case "condition":
		return policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.name", Op: "contains", Value: json.RawMessage(`"Owned"`)}}, nil
	case "write_owner":
		if a == "data.write" {
			return policy.DataScope{Kind: "OWNER"}, nil
		}
	case "field_none":
		if r.FieldID == "field.contact.name" {
			return policy.DataScope{Kind: "NONE"}, nil
		}
	}
	return policy.DataScope{Kind: "ALL"}, nil
}
func TestM6RSQLDataScopeOwnershipTeamsHierarchySharesAndConditions(t *testing.T) {
	f, h, base, alice, _ := m5Fixture(t)
	owned := m5Create(t, h, base, alice, "Owned Alpha")
	m5Create(t, h, base, alice, "Owned Beta")
	if e := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Scope fixture", f.bob, "member", true); e != nil {
		t.Fatal(e)
	}
	bob := f.token(t, f.bobSubject, nil)
	teamRecord := m5Create(t, h, base, bob, "Team record")
	shared := m5Create(t, h, base, bob, "Shared record")
	secret := m5Create(t, h, base, bob, "Other record")
	root, child := uuid(t), uuid(t)
	tx, e := f.operator.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, []any{f.aliceSubject, f.alice, f.tenantA}},
		{`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Parent')`, []any{f.tenantA, root}},
		{`INSERT INTO iam.teams(tenant_id,id,parent_id,name) VALUES($1,$2,$3,'Child')`, []any{f.tenantA, child, root}},
		{`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id) VALUES($1,$2,$3)`, []any{f.tenantA, root, f.alice}},
		{`UPDATE runtime.records SET owner_team_id=$1 WHERE tenant_id=$2 AND id=$3`, []any{child, f.tenantA, teamRecord.ID}},
		{`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$1 FROM runtime.records WHERE tenant_id=$2 AND id=$3`, []any{f.alice, f.tenantA, shared.ID}},
	} {
		if _, e = tx.Exec(stmt.sql, stmt.args...); e != nil {
			t.Fatal("scope fixture provisioning failed")
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		mode  string
		count int
		ids   []string
	}{{"owner", 2, []string{owned.ID}}, {"team", 1, []string{teamRecord.ID}}, {"share", 1, []string{shared.ID}}, {"condition", 2, []string{owned.ID}}} {
		t.Run(tc.mode, func(t *testing.T) {
			restricted := m5Handler(t, f, scopedPolicy{tc.mode}, audit.Postgres{}, true)
			q := m5Query()
			q.Limit = 100
			p := m5Page(t, m5Call(restricted, base+"/query", alice, q))
			if len(p.Records) != tc.count {
				t.Fatal("scope visibility", len(p.Records))
			}
			q.Filter = &data.Filter{Op: "eq", Field: "field.contact.id", Value: json.RawMessage(`"` + secret.ID + `"`)}
			if p := m5Page(t, m5Call(restricted, base+"/query", alice, q)); len(p.Records) != 0 {
				t.Fatal("get/filter bypass")
			}
			q.Filter = &data.Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"Other"`)}
			if p := m5Page(t, m5Call(restricted, base+"/query", alice, q)); len(p.Records) != 0 {
				t.Fatal("search bypass")
			}
			q.Filter = nil
			q.Aggregate = &data.Aggregate{Op: "count"}
			if p := m5Page(t, m5Call(restricted, base+"/query", alice, q)); p.Aggregate == nil || p.Aggregate.Count != tc.count {
				t.Fatal("count bypass")
			}
			q.Aggregate = &data.Aggregate{Op: "sum", Field: "field.contact.balance"}
			p = m5Page(t, m5Call(restricted, base+"/query", alice, q))
			want := "12.3"
			if tc.count == 2 {
				want = "24.6"
			}
			if p.Aggregate == nil || p.Aggregate.Sum != want {
				t.Fatal("aggregate bypass")
			}
			// Export through bounded paginated projection preserves the same SQL scope.
			q.Aggregate = nil
			q.Limit = 1
			total := 0
			for page := 0; page < 10; page++ {
				p = m5Page(t, m5Call(restricted, base+"/query", alice, q))
				total += len(p.Records)
				if p.Next == "" {
					break
				}
				q.After = p.Next
			}
			if total != tc.count {
				t.Fatal("paged export bypass")
			}
		})
	}
	restricted := m5Handler(t, f, scopedPolicy{"write_owner"}, audit.Postgres{}, true)
	if w := m5Call(restricted, base+"/commands", alice, data.Command{Operation: "update", ID: secret.ID, ExpectedVersion: 1, Values: m5Values("Stolen")}); w.Code != 403 {
		t.Fatal("owner mutation bypass", w.Code)
	}
	if w := m5Call(m5Handler(t, f, scopedPolicy{"share"}, audit.Postgres{}, true), base+"/commands", alice, data.Command{Operation: "update", ID: shared.ID, ExpectedVersion: 1, Values: m5Values("Changed")}); w.Code != 403 {
		t.Fatal("read share became write share", w.Code)
	}
	restricted = m5Handler(t, f, scopedPolicy{"field_none"}, audit.Postgres{}, true)
	if p := m5Page(t, m5Call(restricted, base+"/query", alice, m5Query())); len(p.Records) != 2 || p.Records[0].FieldAccess == nil || len(p.Records[0].FieldAccess.Masked) != 1 {
		t.Fatal("field scope must mask without filtering entity-visible rows")
	}
	if w := m5Call(restricted, base+"/commands", alice, data.Command{Operation: "create", Values: m5Values("Forbidden")}); w.Code != 403 {
		t.Fatal("create field scope bypass", w.Code)
	}
}
