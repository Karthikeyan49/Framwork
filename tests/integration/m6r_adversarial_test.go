//go:build integration

package integration

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"os"
	"strings"
	"testing"
)

type independentM6RFieldScope struct{}

func (independentM6RFieldScope) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	return (policy.Native{}).Decide(ctx, s, a, r, c)
}
func (independentM6RFieldScope) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	if r.FieldID == "field.contact.name" {
		return policy.DataScope{Kind: "NONE"}, nil
	}
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}
func TestIndependentM6RFieldScopeBypass(t *testing.T) {
	f, h, base, token, _ := m5Fixture(t)
	target := m5Create(t, h, base, token, "synthetic protected target")
	v := m5Values("synthetic source")
	v["field.contact.parent"] = target.ID
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}); w.Code != 200 {
		t.Fatal("source fixture", w.Code)
	}
	restricted := m5Handler(t, f, independentM6RFieldScope{}, audit.Postgres{}, true)
	query := data.Query{Projection: []string{"field.contact.name"}, Limit: 100}
	list := m5Page(t, m5Call(restricted, base+"/query", token, query))
	if len(list.Records) != 0 {
		t.Fatal("list field scope not enforced")
	}
	join := data.Query{Projection: []string{"field.contact.id"}, Limit: 100, Join: &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.name"}}}
	jw := m5Call(restricted, base+"/query", token, join)
	t.Logf("join status=%d protected target disclosed=%v", jw.Code, strings.Contains(jw.Body.String(), "synthetic protected target"))
	if jw.Code != 200 || strings.Contains(jw.Body.String(), "synthetic protected target") {
		t.Fatal("join scope bypass remains")
	}
	cw := m5Call(restricted, base+"/commands", token, data.Command{Operation: "create", Values: m5Values("synthetic denied created")})
	t.Logf("create status=%d", cw.Code)
	if cw.Code != 403 {
		t.Fatal("command scope bypass remains", cw.Code)
	}
	uw := m5Call(restricted, base+"/commands", token, data.Command{Operation: "update", ID: target.ID, ExpectedVersion: 1, Values: m5Values("synthetic denied updated")})
	t.Logf("update status=%d", uw.Code)
	if uw.Code != 403 && uw.Code != 404 {
		t.Fatal("update scope bypass remains", uw.Code)
	}
}
func TestIndependentM6RWorkerAuditProvenance(t *testing.T) {
	f := newIAM(t)
	ctx := context.Background()
	if e := (tenant.Store{}).Provision(ctx, f.operator, f.tenantB, "Worker isolation fixture", f.alice, "member", true); e != nil {
		t.Fatal("secondary membership fixture")
	}
	job := uuid(t)
	if _, e := f.operator.ExecContext(ctx, "INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES($1,$2,$3,$4)", f.tenantA, job, f.alice, strings.Repeat("a", 64)); e != nil {
		t.Fatal("job fixture unavailable")
	}
	t.Cleanup(func() {
		_, _ = f.operator.ExecContext(ctx, "DELETE FROM control.job_routes WHERE tenant_id=$1 AND id=$2", f.tenantA, job)
	})
	worker, e := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if e != nil {
		t.Fatal("worker unavailable")
	}
	defer worker.Close()
	tx, e := worker.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal("worker transaction unavailable")
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "SELECT set_config('app.auth_issuer',$1,true),set_config('app.auth_subject',$2,true),set_config('app.actor_id',$3,true),set_config('app.tenant_id',$4,true),set_config('app.job_id',$5,true)", "https://test-issuer.example", f.aliceSubject, f.alice, f.tenantA, job); e != nil {
		t.Fatal("worker context unavailable")
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO audit.entries(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,writer_role,occurred_at) VALUES($1,$2,'m6r.review.provenance','probe',false,'review','review',$3,'','framework_migrator','1900-01-01'::timestamptz)", f.alice, f.tenantA, uuid(t))
	t.Logf("worker forged provenance rejected=%v", e != nil)
	if e == nil {
		t.Fatal("audit provenance bypass remains")
	}
}

type independentM6RRowAction struct{}

func (independentM6RRowAction) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	d := (policy.Native{}).Decide(ctx, s, a, r, c)
	if a == "data.row.read" {
		d.Allow = false
	}
	return d
}
func (independentM6RRowAction) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}
func TestIndependentM6RRowActionIgnored(t *testing.T) {
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "synthetic row action denied")
	restricted := m5Handler(t, f, independentM6RRowAction{}, audit.Postgres{}, true)
	w := m5Call(restricted, base+"/query", token, m5Query())
	t.Logf("universally denied row.read status=%d", w.Code)
	if w.Code != 403 || strings.Contains(w.Body.String(), "synthetic row action denied") {
		t.Fatal("row action bypass remains")
	}
}

func TestIndependentM6RWorkerRoutingAndPool(t *testing.T) {
	f, h, base, token, app := m5Fixture(t)
	if e := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantB, "Worker isolation fixture", f.alice, "member", true); e != nil {
		t.Fatal("secondary worker actor membership fixture")
	}
	m5Create(t, h, base, token, "tenant A synthetic")
	bob := f.token(t, f.bobSubject, nil)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); w.Code != 201 {
		t.Fatal("tenant B metadata fixture")
	}
	baseB := strings.Replace(base, f.tenantA, f.tenantB, 1)
	m5Create(t, h, baseB, bob, "tenant B synthetic")
	ctx := context.Background()
	job := uuid(t)
	release := strings.Repeat("a", 64)
	if _, e := f.operator.ExecContext(ctx, "INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES($1,$2,$3,$4)", f.tenantA, job, f.alice, release); e != nil {
		t.Fatal("job fixture unavailable")
	}
	t.Cleanup(func() {
		_, _ = f.operator.ExecContext(ctx, "DELETE FROM control.job_routes WHERE tenant_id=$1 AND id=$2", f.tenantA, job)
	})
	dispatcher, e := postgres.Open(os.Getenv("DISPATCHER_DATABASE_URL"))
	if e != nil {
		t.Fatal("dispatcher unavailable")
	}
	defer dispatcher.Close()
	ticket, e := jobs.FindRoute(ctx, dispatcher, f.tenantA, job)
	if e != nil || ticket.JobID() != job {
		t.Fatal("exact dispatcher route unavailable")
	}
	if e = postgres.WithRuntime(ctx, dispatcher, func(ctx context.Context, q postgres.Executor) error {
		var n int
		return q.QueryRowContext(ctx, "SELECT count(*) FROM runtime.records").Scan(&n)
	}); e == nil {
		t.Fatal("dispatcher could read business records")
	}
	worker, e := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if e != nil {
		t.Fatal("worker unavailable")
	}
	defer worker.Close()
	worker.SetMaxOpenConns(1)
	e = jobs.WithJob(ctx, worker, ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		if a.TenantID() != f.tenantA || a.ActorID() != f.alice {
			t.Fatal("delegation mismatch")
		}
		var na, nb int
		if er := q.QueryRowContext(ctx, "SELECT count(*) FROM runtime.records WHERE tenant_id=$1", f.tenantA).Scan(&na); er != nil {
			return er
		}
		if er := q.QueryRowContext(ctx, "SELECT count(*) FROM runtime.records WHERE tenant_id=$1", f.tenantB).Scan(&nb); er != nil {
			return er
		}
		var foreignIAM int
		if er := q.QueryRowContext(ctx, "SELECT count(*) FROM iam.memberships WHERE tenant_id=$1", f.tenantB).Scan(&foreignIAM); er != nil {
			return er
		}
		if foreignIAM != 0 {
			t.Fatal("worker IAM cross-tenant read")
		}
		if na < 1 || nb != 0 {
			t.Fatal("worker tenant boundary")
		}
		if _, er := q.ExecContext(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.tenantB); er != nil {
			return er
		}
		if er := q.QueryRowContext(ctx, "SELECT count(*) FROM runtime.records WHERE tenant_id=$1", f.tenantB).Scan(&nb); er != nil {
			return er
		}
		if er := q.QueryRowContext(ctx, "SELECT count(*) FROM iam.memberships WHERE tenant_id=$1", f.tenantB).Scan(&foreignIAM); er != nil {
			return er
		}
		if foreignIAM != 0 {
			t.Fatal("worker IAM tenant switch bypass")
		}
		if nb != 0 {
			t.Fatal("job scope tenant switch bypass")
		}
		_, er := q.ExecContext(ctx, "SELECT set_config('app.job_id','synthetic-persistent-marker',false)")
		return er
	})
	if e != nil {
		t.Fatal("valid scoped worker execution failed")
	}
	var leftover string
	if e = worker.QueryRowContext(ctx, "SELECT current_setting('app.job_id',true)").Scan(&leftover); e != nil || leftover != "" {
		t.Fatal("worker context leaked through pool")
	}
	if _, e = f.operator.ExecContext(ctx, "UPDATE control.job_routes SET release_hash=$3 WHERE tenant_id=$1 AND id=$2", f.tenantA, job, strings.Repeat("b", 64)); e != nil {
		t.Fatal("own test route update failed")
	}
	ran := false
	if e = jobs.WithJob(ctx, worker, ticket, func(context.Context, postgres.Executor, tenant.Access) error { ran = true; return nil }); e == nil || ran {
		t.Fatal("stale ticket executed")
	}
	t.Log("worker A visibility, tenant switch fail-closed, persistent job cleanup, stale release ticket, dispatcher payload denial all verified")
}

type independentM6RNullableScope struct{}

func (independentM6RNullableScope) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	return (policy.Native{}).Decide(ctx, s, a, r, c)
}
func (independentM6RNullableScope) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	if a == "data.write" && r.FieldID == "" {
		return policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.balance", Op: "gt", Value: []byte{123, 34, 97, 109, 111, 117, 110, 116, 34, 58, 34, 48, 34, 44, 34, 99, 117, 114, 114, 101, 110, 99, 121, 34, 58, 34, 85, 83, 68, 34, 125}}}, nil
	}
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}
func TestIndependentM6RNullableScopeDenial(t *testing.T) {
	f, _, base, token, _ := m5Fixture(t)
	h := m5Handler(t, f, independentM6RNullableScope{}, audit.Postgres{}, true)
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: map[string]any{"field.contact.name": "synthetic absent optional balance"}})
	t.Logf("nullable gt scope missing optional field status=%d", w.Code)
	if w.Code != 403 {
		t.Fatalf("unknown predicate should deny403, actual status%d", w.Code)
	}
}
