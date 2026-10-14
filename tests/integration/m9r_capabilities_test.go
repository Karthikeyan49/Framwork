//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func r5Handler(t *testing.T, f *iamFixture, writer audit.Writer) http.Handler {
	t.Helper()
	api := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, PolicyLoader: (policy.RoleStore{}).Load, Audit: writer, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	return httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
}
func r5Definition(name, app string, scope policy.DataScope) policy.Definition {
	return policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: name, Grants: []policy.Grant{
		{Effect: "ALLOW", Actions: []string{"preview.read"}, Resource: policy.Selector{AppID: app, EntityID: "*", FieldID: "*"}},
		{Effect: "ALLOW", Actions: []string{"data.entity.read", "data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}, Scope: &scope},
		{Effect: "ALLOW", Actions: []string{"data.row.read", "data.aggregate.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}},
	}}
}
func r5Publish(t *testing.T, h http.Handler, path, token string, base int64, d policy.Definition, enabled bool, op string) policy.RoleVersion {
	t.Helper()
	w := m5Call(h, path, token, map[string]any{"operation_id": op, "expected_version": base, "active": enabled, "definition": d})
	if w.Code != 200 {
		t.Fatal("publish", w.Code, w.Body.String())
	}
	var v policy.RoleVersion
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Name != d.Name || v.Version != base+1 || v.OperationID != op || len(v.Hash) != 64 || v.Bootstrap {
		t.Fatal("role witness")
	}
	return v
}
func r5Assign(t *testing.T, h http.Handler, root, token, user, name string, want int) {
	t.Helper()
	expected := "viewer"
	if w := m3Call(h, "GET", root+"/members", token, 0, nil); w.Code == 200 {
		var body struct {
			Members []tenant.Member `json:"members"`
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal("membership read")
		}
		for _, member := range body.Members {
			if member.UserID == user {
				expected = member.Role
			}
		}
	}
	if w := m5Call(h, root+"/members/"+user+"/role", token, map[string]string{"role": name, "expected_role": expected}); w.Code != want {
		t.Fatal("assign", w.Code, w.Body.String())
	}
}

func TestM9RCapabilitiesPublicAPISQLScopesAndRevocation(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	if err := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Roles", f.bob, "member", true); err != nil {
		t.Fatal(err)
	}
	bob := f.token(t, f.bobSubject, map[string]any{"roles": []string{"owner"}, "tenant_id": f.tenantB})
	own := m5Create(t, legacy, base, bob, "Allowed own")
	teamRecord := m5Create(t, legacy, base, alice, "Allowed team")
	shared := m5Create(t, legacy, base, alice, "Shared")
	hidden := m5Create(t, legacy, base, alice, "Hidden")
	root, child := uuid(t), uuid(t)
	tx, err := f.operator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, []any{f.aliceSubject, f.alice, f.tenantA}},
		{`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Parent')`, []any{f.tenantA, root}},
		{`INSERT INTO iam.teams(tenant_id,id,parent_id,name) VALUES($1,$2,$3,'Child')`, []any{f.tenantA, child, root}},
		{`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id) VALUES($1,$2,$3)`, []any{f.tenantA, root, f.bob}},
		{`UPDATE runtime.records SET owner_team_id=$1 WHERE tenant_id=$2 AND id=$3`, []any{child, f.tenantA, teamRecord.ID}},
		{`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$1 FROM runtime.records WHERE tenant_id=$2 AND id=$3`, []any{f.bob, f.tenantA, shared.ID}},
	} {
		if _, err = tx.Exec(s.sql, s.args...); err != nil {
			t.Fatal("scope fixture", err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h := r5Handler(t, f, audit.Postgres{})
	roleRoot := "/v1/tenants/" + f.tenantA
	definition := r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"})
	version := r5Publish(t, h, roleRoot+"/roles", alice, 0, definition, true, uuid(t))
	r5Assign(t, h, roleRoot, alice, f.bob, definition.Name, 200)
	if w := m5Call(h, roleRoot+"/members/"+f.bob+"/role", alice, map[string]string{"role": "admin", "expected_role": "member"}); w.Code != 409 {
		t.Fatal("stale assignment overwrote current role", w.Code)
	}
	if w := m3Call(h, "GET", roleRoot+"/access", bob, 0, nil); w.Code != 403 {
		t.Fatal("custom role inherited tenant capability", w.Code)
	}
	query := m5Query()
	query.Limit = 100
	cases := []struct {
		name  string
		scope policy.DataScope
		ids   []string
	}{
		{"owner", policy.DataScope{Kind: "OWNER"}, []string{own.ID}},
		{"team hierarchy", policy.DataScope{Kind: "TEAM"}, []string{teamRecord.ID}},
		{"share", policy.DataScope{Kind: "SHARE"}, []string{shared.ID}},
		{"condition", policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.name", Op: "contains", Value: json.RawMessage(`"Allowed"`)}}, []string{own.ID, teamRecord.ID}},
		{"or", policy.DataScope{Kind: "OR", Children: []policy.DataScope{{Kind: "OWNER"}, {Kind: "SHARE"}}}, []string{own.ID, shared.ID}},
		{"and", policy.DataScope{Kind: "AND", Children: []policy.DataScope{{Kind: "TEAM"}, {Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.name", Op: "contains", Value: json.RawMessage(`"Allowed"`)}}}}, []string{teamRecord.ID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			definition = r5Definition("finance_manager", app, tc.scope)
			version = r5Publish(t, h, roleRoot+"/roles", alice, version.Version, definition, true, uuid(t))
			page := m5Page(t, m5Call(h, base+"/query", bob, query))
			if len(page.Records) != len(tc.ids) {
				t.Fatal("scope leaked rows", len(page.Records))
			}
			for _, record := range page.Records {
				found := false
				for _, id := range tc.ids {
					found = found || record.ID == id
				}
				if !found {
					t.Fatal("foreign scope record")
				}
			}
			deniedQuery := query
			deniedQuery.Filter = &data.Filter{Op: "eq", Field: "field.contact.id", Value: json.RawMessage(`"` + hidden.ID + `"`)}
			if p := m5Page(t, m5Call(h, base+"/query", bob, deniedQuery)); len(p.Records) != 0 {
				t.Fatal("filter bypass")
			}
			aggregate := query
			aggregate.Aggregate = &data.Aggregate{Op: "count"}
			if p := m5Page(t, m5Call(h, base+"/query", bob, aggregate)); p.Aggregate == nil || p.Aggregate.Count != len(tc.ids) {
				t.Fatal("aggregate bypass")
			}
			if w := m5Call(h, base+"/commands", bob, data.Command{Operation: "update", ID: hidden.ID, ExpectedVersion: 1, Values: m5Values("Stolen")}); w.Code != 403 {
				t.Fatal("custom write inherited", w.Code)
			}
		})
	}
	// Explicit field DENY must also cover inference through filter/sort/aggregate.
	definition = r5Definition("finance_manager", app, policy.DataScope{Kind: "ALL"})
	definition.Grants = append(definition.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.name"}}, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.balance"}})
	version = r5Publish(t, h, roleRoot+"/roles", alice, version.Version, definition, true, uuid(t))
	for _, q := range []data.Query{query, {Projection: []string{"field.contact.id"}, Limit: 100, Filter: &data.Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"Hidden"`)}}, {Projection: []string{"field.contact.id"}, Limit: 100, Sort: &data.Sort{Field: "field.contact.name"}}, {Projection: []string{"field.contact.id", "field.contact.balance"}, Limit: 100, Aggregate: &data.Aggregate{Op: "sum", Field: "field.contact.balance"}}} {
		if w := m5Call(h, base+"/query", alice, q); w.Code != 200 {
			t.Fatal("deny fixture invalid", w.Code, w.Body.String())
		}
		if w := m5Call(h, base+"/query", bob, q); w.Code != 403 {
			t.Fatal("field deny bypass", w.Code, w.Body.String())
		}
	}
	// Built-in bootstrap behavior remains explicit; custom roles have no fallback.
	if p := m5Page(t, m5Call(h, base+"/query", alice, query)); len(p.Records) != 4 {
		t.Fatal("bootstrap compatibility")
	}
	r5Assign(t, h, roleRoot, bob, f.viewer, "owner", 403)
	if w := m5Call(h, "/v1/tenants/"+f.tenantB+"/roles", alice, map[string]any{"operation_id": uuid(t), "expected_version": 0, "active": true, "definition": definition}); w.Code != 403 {
		t.Fatal("cross tenant management", w.Code)
	}
	r5Assign(t, h, roleRoot, alice, f.alice, "finance_manager", 409)
	version = r5Publish(t, h, roleRoot+"/roles", alice, version.Version, definition, false, uuid(t))
	if w := m5Call(h, base+"/query", bob, query); w.Code != 403 {
		t.Fatal("inactive role authorized", w.Code)
	}
	var denialCount int
	if err = f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND actor_id=$2 AND action='policy.unavailable' AND NOT allowed AND policy_version=$3 AND (state->>'policy_revision')::bigint=$4`, f.tenantA, f.bob, "tenant-role-v1:"+version.Hash, version.Version).Scan(&denialCount); err != nil || denialCount != 1 {
		t.Fatal("inactive denial provenance", err, denialCount)
	}
	if err := (policy.RoleStore{}).Ready(context.Background(), f.db, false); err != nil {
		t.Fatal("capability readiness", err)
	}
}

func TestM9REventConsumerUsesLiveTenantCapabilities(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	m5Create(t, legacy, base, alice, "Allowed event")
	m5Create(t, legacy, base, alice, "Revoked event")
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	d := r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"events.consume"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, h, root+"/roles", alice, 0, d, true, uuid(t))
	r5Assign(t, h, root, alice, f.viewer, "owner", 200)
	r5Assign(t, h, root, f.token(t, f.viewerSubject, nil), f.alice, d.Name, 200)
	dispatcher, db := m8Roles(t)
	dispatch := events.Dispatcher{DB: dispatcher}
	first := m8Claim(t, dispatch)
	if err := dispatch.Finish(context.Background(), first, true); err != nil {
		t.Fatal(err)
	}
	c := m8Consumer(db)
	c.Policy = policy.Runtime{Worker: true}
	if err := c.Consume(context.Background(), first); err != nil {
		t.Fatal("allowed delegated event", err)
	}
	next := m8Claim(t, dispatch)
	if err := dispatch.Finish(context.Background(), next, true); err != nil {
		t.Fatal(err)
	}
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"events.consume"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, h, root+"/roles", f.token(t, f.viewerSubject, nil), 1, d, true, uuid(t))
	if err := c.Consume(context.Background(), next); !errors.Is(err, events.ErrDelivery) {
		t.Fatal("revoked event consumed", err)
	}
	var effects, receipts int
	if err := f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.m8_test_effects WHERE tenant_id=$1),(SELECT count(*) FROM runtime.event_inbox WHERE tenant_id=$1)`, f.tenantA).Scan(&effects, &receipts); err != nil || effects != 1 || receipts != 1 {
		t.Fatal("revoked event committed effects", err, effects, receipts)
	}
}

func TestM9RCapabilityWitnessCASAuditRollbackAndUnknownCommit(t *testing.T) {
	m8Database(t)
	f, _, _, alice, app := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	path := "/v1/tenants/" + f.tenantA + "/roles"
	d := r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"})
	op := uuid(t)
	v := r5Publish(t, h, path, alice, 0, d, true, op)
	d2 := r5Definition("finance_manager", app, policy.DataScope{Kind: "TEAM"})
	r5Publish(t, h, path, alice, 1, d2, true, uuid(t))
	replay := m5Call(h, path, alice, map[string]any{"operation_id": op, "expected_version": 0, "active": true, "definition": d})
	var original policy.RoleVersion
	if replay.Code != 200 || json.Unmarshal(replay.Body.Bytes(), &original) != nil || original.Version != 1 || original.Hash != v.Hash {
		t.Fatal("retry returned newer head")
	}
	for _, body := range []map[string]any{
		{"operation_id": op, "expected_version": 0, "active": true, "definition": d2},
		{"operation_id": uuid(t), "expected_version": 1, "active": true, "definition": d},
	} {
		if w := m5Call(h, path, alice, body); w.Code != 409 {
			t.Fatal("stale or conflicting role operation", w.Code)
		}
	}
	for _, bad := range []policy.Definition{
		{SchemaVersion: policy.DefinitionVersion, Name: "owner", Grants: []policy.Grant{}},
		{SchemaVersion: policy.DefinitionVersion, Name: "unknown", Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{"data.row.read"}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}, Scope: &policy.DataScope{Kind: "OWNER"}}}},
		{SchemaVersion: policy.DefinitionVersion, Name: "unknown", Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{"workflows.execute"}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}, Scope: &policy.DataScope{Kind: "OWNER"}}}},
	} {
		if w := m5Call(h, path, alice, map[string]any{"operation_id": uuid(t), "expected_version": 0, "active": true, "definition": bad}); w.Code != 400 {
			t.Fatal("unsafe public role definition accepted", w.Code)
		}
	}
	fail := r5Handler(t, f, failedAudit{})
	if w := m5Call(fail, path, alice, map[string]any{"operation_id": uuid(t), "expected_version": 2, "active": true, "definition": d}); w.Code != 503 {
		t.Fatal("audit failure acknowledged", w.Code)
	}
	var n int
	if err := f.operator.QueryRow(`SELECT count(*) FROM control.role_versions WHERE tenant_id=$1 AND name='finance_manager'`, f.tenantA).Scan(&n); err != nil || n != 2 {
		t.Fatal("failed audit committed authority")
	}
	proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
	db, err := postgres.Open(proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }); err != nil {
		t.Fatal(err)
	}
	proxied := *f
	proxied.db = db
	lostOp := uuid(t)
	if w := m5Call(r5Handler(t, &proxied, audit.Postgres{}), path, alice, map[string]any{"operation_id": lostOp, "expected_version": 2, "active": true, "definition": d}); w.Code != 503 || !observed.Load() {
		t.Fatal("commit loss not reproduced", w.Code, observed.Load())
	}
	w := m3Call(h, "GET", path+"/operations/"+lostOp, alice, 0, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Version != 3 || v.OperationID != lostOp {
		t.Fatal("unknown commit not reconciled", w.Code)
	}
	if w = m3Call(h, "GET", "/v1/tenants/"+f.tenantB+"/roles/operations/"+lostOp, f.token(t, f.bobSubject, nil), 0, nil); w.Code != 404 {
		t.Fatal("foreign operation disclosed", w.Code)
	}
	if _, err = f.operator.Exec(`UPDATE control.role_versions SET active=false WHERE tenant_id=$1 AND name='finance_manager'`, f.tenantA); err == nil {
		t.Fatal("immutable role history changed")
	}
	if _, err = f.operator.Exec(`UPDATE control.role_heads SET active=false WHERE tenant_id=$1 AND name='finance_manager'`, f.tenantA); err == nil {
		t.Fatal("head witness changed")
	}
	if err = f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='roles.write' AND allowed AND state->>'policy_operation'=$2`, f.tenantA, lostOp).Scan(&n); err != nil || n != 1 {
		t.Fatal("committed role audit missing")
	}
}

func TestM9RConcurrentOwnerDemotionsCannotRemoveLastOwner(t *testing.T) {
	m8Database(t)
	f, _, _, alice, app := m5Fixture(t)
	if err := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Roles", f.bob, "owner", true); err != nil {
		t.Fatal(err)
	}
	h := r5Handler(t, f, audit.Postgres{})
	r5Publish(t, h, "/v1/tenants/"+f.tenantA+"/roles", alice, 0, r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"}), true, uuid(t))
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	out := make(chan error, 2)
	for _, pair := range []struct{ token, user string }{{alice, f.alice}, {f.token(t, f.bobSubject, nil), f.bob}} {
		principal, err := f.verifier.Verify(pair.token)
		if err != nil {
			t.Fatal(err)
		}
		go func(user string) {
			out <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
				a, err := m3Scope(f, ctx, q, principal, f.tenantA)
				ready <- struct{}{}
				<-start
				if err != nil {
					return err
				}
				return (policy.RoleStore{}).Assign(ctx, q, a, user, "finance_manager", "owner")
			})
		}(pair.user)
	}
	<-ready
	<-ready
	close(start)
	first, second := <-out, <-out
	if (first == nil) == (second == nil) || first != nil && !postgres.KnownAbort(first) || second != nil && !postgres.KnownAbort(second) {
		t.Fatal("concurrent owner fence", first, second)
	}
	var n int
	if err := f.operator.QueryRow(`SELECT count(*) FROM iam.memberships WHERE tenant_id=$1 AND role='owner' AND active`, f.tenantA).Scan(&n); err != nil || n != 1 {
		t.Fatal("last owner lost")
	}
}

func TestM9RWorkerCapabilityDefinitionConfinementAndLiveRevocation(t *testing.T) {
	m := m9Setup(t, nil)
	instance := m.start(t, "capability-revocation")
	h := r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	d := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "ALL"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, h, root+"/roles", m.token, 0, d, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, "finance_manager", 200)
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("lease", err)
	}
	var count int
	if err = m.db.QueryRow(`SELECT count(*) FROM control.worker_role_definition()`).Scan(&count); err != nil || count != 0 {
		t.Fatal("unbound definition disclosure", err)
	}
	for _, relation := range []string{"iam.roles", "control.role_versions", "control.role_heads"} {
		if err = m.db.QueryRow(`SELECT count(*) FROM ` + relation).Scan(&count); err == nil {
			t.Fatal("worker generic catalog read", relation)
		}
	}
	if err = (policy.RoleStore{}).Ready(context.Background(), m.db, true); err != nil {
		t.Fatal("worker capability readiness", err)
	}
	var firstHash string
	if err = jobs.WithJob(context.Background(), m.db, lease.Ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		engine, e := (policy.RoleStore{}).LoadWorker(ctx, q, a)
		if e != nil {
			return e
		}
		user, e := (identity.Store{}).ResolveWorker(ctx, q, lease.Ticket.JobID())
		if e != nil {
			return e
		}
		decision := engine.Decide(ctx, policy.Subject{User: user, Access: a}, "workflows.execute", policy.Resource{TenantID: a.TenantID(), Type: "workflow", AppID: m.app, EntityID: "entity.contact", RecordID: m.record.ID, WorkflowID: instance.Pin.WorkflowID}, policy.Context{})
		if !decision.Allow {
			return policy.ErrScope
		}
		firstHash = decision.Version
		return nil
	}); err != nil {
		t.Fatal("exact delegated definition", err)
	}
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"workflows.execute"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	revised := r5Publish(t, h, root+"/roles", m.approver, 1, d, true, uuid(t))
	if firstHash == "tenant-role-v1:"+revised.Hash {
		t.Fatal("policy version did not change")
	}
	m.worker.Store = access.WorkerWorkflowStore(policy.Runtime{Worker: true}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var job string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&job); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, job)
	})
	if err = m.worker.Step(context.Background(), lease); !errors.Is(err, workflow.ErrDenied) {
		t.Fatal("revoked workflow capability executed", err)
	}
	var version int64
	if err = m.f.operator.QueryRow(`SELECT version FROM runtime.workflow_instances WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, instance.ID).Scan(&version); err != nil || version != instance.Version {
		t.Fatal("denied worker mutated workflow", err)
	}
	if err = m.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='workflows.execute' AND NOT allowed AND policy_version=$2`, m.f.tenantA, "tenant-role-v1:"+revised.Hash).Scan(&count); err != nil || count != 1 {
		t.Fatal("revocation audit not version bound", err, count)
	}
	allowed := d
	allowed.Grants = append([]policy.Grant{}, d.Grants[:len(d.Grants)-1]...)
	fieldDeny := allowed
	fieldDeny.Grants = append(append([]policy.Grant{}, allowed.Grants...), policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "field.contact.status"}})
	none := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "NONE"})
	none.Grants = append(none.Grants, allowed.Grants[len(allowed.Grants)-1])
	team := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "TEAM"})
	team.Grants = append(team.Grants, allowed.Grants[len(allowed.Grants)-1])
	for _, tc := range []struct {
		name, action string
		definition   policy.Definition
		active       bool
	}{
		{"field denial", "data.field.read", fieldDeny, true},
		{"scope denial", "workflows.data_scope", none, true},
		{"SQL scope excludes row", "workflows.data_scope", team, true},
		{"inactive definition", "policy.unavailable", allowed, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			revised = r5Publish(t, h, root+"/roles", m.approver, revised.Version, tc.definition, tc.active, uuid(t))
			if e := m.worker.Step(context.Background(), lease); !errors.Is(e, workflow.ErrDenied) {
				t.Fatal("revocation did not deny", e)
			}
			if e := m.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action=$2 AND NOT allowed AND policy_version=$3 AND state->>'workflow_instance_id'=$4`, m.f.tenantA, tc.action, "tenant-role-v1:"+revised.Hash, instance.ID).Scan(&count); e != nil || count != 1 {
				t.Fatal("durable denial provenance", e, count)
			}
		})
	}
}

func TestM9RRoleNameGrantDriftRejectsAPIAndWorkerTransactions(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	_, worker := m8Roles(t)
	for _, pair := range []struct {
		role string
		db   *sql.DB
	}{{"framework_app", f.db}, {"framework_worker", worker}} {
		t.Run(pair.role, func(t *testing.T) {
			if _, err := f.operator.Exec(`GRANT SELECT(name) ON iam.roles TO ` + pair.role); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := f.operator.Exec(`REVOKE SELECT(name) ON iam.roles FROM ` + pair.role); err != nil {
					t.Error(err)
				}
			}()
			called := false
			if err := postgres.WithRuntime(context.Background(), pair.db, func(context.Context, postgres.Executor) error { called = true; return nil }); err == nil || called {
				t.Fatal("grant drift admitted transaction")
			}
			if pair.role == "framework_worker" {
				var safe bool
				if err := pair.db.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe); err != nil || safe {
					t.Fatal("worker drift guard missed column grant", err)
				}
			}
		})
	}
}

func TestM9RStoredRoleEngineBindsTenantAndActor(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	h := r5Handler(t, f, audit.Postgres{})
	selector := policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}
	allowed := policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "finance_manager", Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{"memberships.read"}, Resource: selector}}}
	denied := policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "finance_manager", Grants: []policy.Grant{}}
	alice, bob, viewer := f.token(t, f.aliceSubject, nil), f.token(t, f.bobSubject, nil), f.token(t, f.viewerSubject, nil)
	r5Publish(t, h, "/v1/tenants/"+f.tenantA+"/roles", alice, 0, allowed, true, uuid(t))
	r5Publish(t, h, "/v1/tenants/"+f.tenantB+"/roles", bob, 0, denied, true, uuid(t))
	r5Assign(t, h, "/v1/tenants/"+f.tenantA, alice, f.viewer, "finance_manager", 200)
	if e := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantB, "Engine binding", f.viewer, "finance_manager", true); e != nil {
		t.Fatal(e)
	}
	if e := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Engine binding", f.bob, "finance_manager", true); e != nil {
		t.Fatal(e)
	}
	if w := m3Call(h, "GET", "/v1/tenants/"+f.tenantA+"/members", viewer, 0, nil); w.Code != 200 {
		t.Fatal("A allow control", w.Code)
	}
	if w := m3Call(h, "GET", "/v1/tenants/"+f.tenantB+"/members", viewer, 0, nil); w.Code != 403 {
		t.Fatal("B deny control", w.Code)
	}
	p, e := f.verifier.Verify(viewer)
	if e != nil {
		t.Fatal(e)
	}
	var engine policy.Engine
	if e = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		a, e := m3Scope(f, ctx, q, p, f.tenantA)
		if e != nil {
			return e
		}
		engine, e = (policy.RoleStore{}).Load(ctx, q, a)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ token, tenantID string }{{viewer, f.tenantB}, {bob, f.tenantA}} {
		principal, e := f.verifier.Verify(tc.token)
		if e != nil {
			t.Fatal(e)
		}
		if e = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			a, e := m3Scope(f, ctx, q, principal, tc.tenantID)
			if e != nil {
				return e
			}
			user, e := (identity.Store{}).Resolve(ctx, q, principal)
			if e != nil {
				return e
			}
			resource := policy.Resource{TenantID: tc.tenantID, Type: "membership"}
			if d := engine.Decide(ctx, policy.Subject{User: user, Access: a}, "memberships.read", resource, policy.Context{}); d.Allow {
				t.Fatal("stored engine reused across tenant/actor")
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
	}
}

func TestM9RReservedCapabilitiesRejectUnexecutableGrants(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	h := r5Handler(t, f, audit.Postgres{})
	owner := f.token(t, f.aliceSubject, nil)
	root := "/v1/tenants/" + f.tenantA + "/roles"
	for _, tc := range []struct {
		role, action string
		base         int64
	}{{"viewer", "workflows.start", 1}, {"viewer", "events.consume", 1}, {"finance_manager", "indexes.manage", 0}, {"finance_manager", "datasets.evolve", 0}, {"finance_manager", "roles.assign", 0}} {
		d := policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: tc.role, Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{tc.action}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}}
		if w := m5Call(h, root, owner, map[string]any{"operation_id": uuid(t), "expected_version": tc.base, "active": true, "definition": d}); w.Code != 400 {
			t.Fatal("unsupported executor capability accepted", tc.role, tc.action, w.Code)
		}
	}
}
