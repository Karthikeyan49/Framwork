//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type m9Fixture struct {
	f                                  *iamFixture
	handler                            http.Handler
	worker                             workflow.Worker
	db                                 *sql.DB
	token, approver, base, entity, app string
	record                             data.Record
	raw                                []byte
}

func m9Setup(t *testing.T, change func(*metadata.Workflow)) *m9Fixture {
	t.Helper()
	m8Database(t)
	f := newIAM(t)
	raw, e := os.ReadFile("../../contracts/examples/workflow-draft-v1.2.json")
	if e != nil {
		t.Fatal(e)
	}
	var doc metadata.Document
	if e = json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	doc.AppID = "app.workflows_" + strings.ReplaceAll(uuid(t), "-", "")
	for i := range doc.Definitions {
		if change != nil && doc.Definitions[i].Workflow != nil {
			change(doc.Definitions[i].Workflow)
		}
	}
	raw, _ = json.Marshal(doc)
	api := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	token := f.token(t, f.aliceSubject, nil)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + doc.AppID + "/versions/1"
	if r := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+doc.AppID+"/versions", token, 0, raw); r.Code != 201 {
		t.Fatal("schema1.2 registry", r.Code, r.Body.String())
	}
	rec := m5Create(t, h, root+"/entities/entity.contact", token, "Workflow snapshot")
	_, db := m8Roles(t)
	store := access.WorkflowStore(policy.Native{}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var job string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&job); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, job)
	})
	tx, e := f.operator.BeginTx(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = (tenant.Store{}).Provision(context.Background(), tx, f.tenantA, "Test workspace", f.bob, "admin", true); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if _, e = f.operator.Exec(`CREATE TABLE runtime.m9_test_effects(tenant_id uuid NOT NULL,instance_id uuid NOT NULL,step_key text NOT NULL,PRIMARY KEY(tenant_id,instance_id,step_key)); SELECT runtime.install_tenant_rls('runtime.m9_test_effects'); REVOKE ALL ON runtime.m9_test_effects FROM framework_app; CREATE POLICY test_worker ON runtime.m9_test_effects TO framework_worker USING(iam.worker_tenant_scope(tenant_id) AND instance_id::text=current_setting('app.job_id',true)) WITH CHECK(iam.worker_tenant_scope(tenant_id) AND instance_id::text=current_setting('app.job_id',true)); GRANT SELECT,INSERT ON runtime.m9_test_effects TO framework_worker`); e != nil {
		t.Fatal(e)
	}
	store.Activity = func(ctx context.Context, q postgres.Executor, a tenant.Access, r workflow.ActivityRequest) (json.RawMessage, error) {
		if _, e := q.ExecContext(ctx, `INSERT INTO runtime.m9_test_effects(tenant_id,instance_id,step_key) VALUES($1,$2,$3)`, a.TenantID(), r.InstanceID, r.StepKey); e != nil {
			return nil, e
		}
		return workflow.Noop(ctx, q, a, r)
	}
	return &m9Fixture{f: f, handler: h, worker: workflow.Worker{DB: db, Store: store}, db: db, token: token, approver: f.token(t, f.bobSubject, nil), base: root + "/workflows/workflow.contact.review", entity: root + "/entities/entity.contact", record: rec, app: doc.AppID, raw: raw}
}
func (m *m9Fixture) start(t *testing.T, key string) workflow.Instance {
	t.Helper()
	r := m5Call(m.handler, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": key})
	return m9Decode(t, r)
}
func m9Decode(t *testing.T, r *httptest.ResponseRecorder) workflow.Instance {
	t.Helper()
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	var x workflow.Instance
	if json.Unmarshal(r.Body.Bytes(), &x) != nil || x.ID == "" || x.Pin.PlanHash == "" {
		t.Fatal("instance contract")
	}
	return x
}
func (m *m9Fixture) get(t *testing.T, id string) workflow.Instance {
	t.Helper()
	r := httptest.NewRequest("GET", m.base+"/instances/"+id, nil)
	r.Header.Set("Authorization", "Bearer "+m.token)
	w := httptest.NewRecorder()
	m.handler.ServeHTTP(w, r)
	return m9Decode(t, w)
}
func (m *m9Fixture) signal(t *testing.T, x workflow.Instance, op, token string) workflow.Instance {
	t.Helper()
	r := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", token, workflow.Signal{Operation: op, ExpectedVersion: x.Version, StateID: x.StateID, Key: op + "_" + uuid(t), OperatorRef: "test-review"})
	return m9Decode(t, r)
}
func (m *m9Fixture) step(t *testing.T) jobs.WorkflowLease {
	t.Helper()
	l, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal("workflow claim", e)
	}
	if e = m.worker.Step(context.Background(), l); e != nil {
		t.Fatal("step", e)
	}
	return l
}
func (m *m9Fixture) until(t *testing.T, id, status string) workflow.Instance {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		x := m.get(t, id)
		if x.Status == status {
			return x
		}
		found, e := m.worker.Once(context.Background())
		if e != nil {
			t.Fatal(e)
		}
		if !found {
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("workflow status timeout", status)
	return workflow.Instance{}
}
func (m *m9Fixture) counts(t *testing.T, x workflow.Instance, effects int) {
	t.Helper()
	var history, outbox, n int
	if e := m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2),(SELECT count(*) FROM runtime.workflow_outbox WHERE tenant_id=$1 AND instance_id=$2),(SELECT count(*) FROM runtime.m9_test_effects WHERE tenant_id=$1 AND instance_id=$2)`, m.f.tenantA, x.ID).Scan(&history, &outbox, &n); e != nil {
		t.Fatal(e)
	}
	if int64(history) != x.Version || history != outbox || n != effects {
		t.Fatalf("atomic history/outbox/effect counts %d/%d/%d version=%d effects=%d", history, outbox, n, x.Version, effects)
	}
}

func TestM9DurableSnapshotApprovalAndImmutablePins(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "review-one")
	same := m.start(t, "review-one")
	if x.ID != same.ID || x.Version != same.Version {
		t.Fatal("duplicate start")
	}
	// A later record change must not replace the captured condition values.
	values := m5Values("Changed after start")
	values["field.contact.status"] = "inactive"
	if r := m5Call(m.handler, m.entity+"/commands", m.token, data.Command{Operation: "update", ID: m.record.ID, ExpectedVersion: 1, Values: values}); r.Code != 200 {
		t.Fatal(r.Code)
	}
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	if x.Pin.RecordVersion != 1 || x.StateID != "state.approval" {
		t.Fatal("snapshot changed")
	}
	m.counts(t, x, 1)
	denied := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.token, workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "self-approval"})
	if denied.Code != 403 {
		t.Fatal("self approval", denied.Code)
	}
	stale := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, workflow.Signal{Operation: "approve", ExpectedVersion: x.Version - 1, StateID: x.StateID, Key: "stale-approval"})
	if stale.Code != 409 {
		t.Fatal("stale approval", stale.Code)
	}
	signal := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "approved-once"}
	x = m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	again := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	if again.Version != x.Version {
		t.Fatal("duplicate approval advanced")
	}
	signal.Operation = "reject"
	if r := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal); r.Code != 409 {
		t.Fatal("signal key collision")
	}
	x = m.until(t, x.ID, "SUCCEEDED")
	m.counts(t, x, 1)
	if r := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.token, workflow.Signal{Operation: "cancel", ExpectedVersion: x.Version, StateID: x.StateID, Key: "terminal-cancel"}); r.Code != 409 {
		t.Fatal("terminal mutation")
	}
	if _, e := m.f.operator.Exec(`UPDATE runtime.workflow_bindings SET workflow_version='2.0.0' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e == nil {
		t.Fatal("operator mutated immutable pin")
	}
	if _, e := m.f.operator.Exec(`DELETE FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, x.ID); e == nil {
		t.Fatal("history deleted")
	}
}
func TestM9CancellationCompensationTimerAndTimeout(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "cancel-review")
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	x = m.signal(t, x, "cancel", m.token)
	x = m.until(t, x.ID, "CANCELLED")
	m.counts(t, x, 2)
	m2 := m.start(t, "timeout-review")
	m2 = m.until(t, m2.ID, "WAITING_APPROVAL")
	if _, e := m.f.operator.Exec(`UPDATE runtime.workflow_instances SET due_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2;`, m.f.tenantA, m2.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := m.f.operator.Exec(`UPDATE control.workflow_jobs SET due_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, m2.ID); e != nil {
		t.Fatal(e)
	}
	m2 = m.until(t, m2.ID, "TIMED_OUT")
	m.counts(t, m2, 2)
}
func TestM9RetryExhaustionRecoveryAndUnknownReconciliation(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "retry-review")
	m.step(t)
	successful := m.worker.Store.Activity
	m.worker.Store.Activity = func(ctx context.Context, q postgres.Executor, a tenant.Access, r workflow.ActivityRequest) (json.RawMessage, error) {
		if _, e := successful(ctx, q, a, r); e != nil {
			return nil, e
		}
		return nil, workflow.Retryable()
	}
	x = m.until(t, x.ID, "DEAD")
	if x.Attempt != 3 {
		t.Fatal("retry bound", x.Attempt)
	}
	m.counts(t, x, 0)
	x = m.signal(t, x, "recover", m.approver)
	m.worker.Store.Activity = successful
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	m.counts(t, x, 1)
	x = m.signal(t, x, "cancel", m.token)
	m.worker.Store.Activity = func(context.Context, postgres.Executor, tenant.Access, workflow.ActivityRequest) (json.RawMessage, error) {
		return nil, workflow.Unknown()
	}
	x = m.until(t, x.ID, "RECONCILE")
	found, e := m.worker.Once(context.Background())
	if e != nil || found {
		t.Fatal("unknown outcome retried", found, e)
	}
	r := workflow.Signal{Operation: "reconcile", ExpectedVersion: x.Version, StateID: x.StateID, Key: "reconcile-unknown", OperatorRef: "checked-local-ledger", Outcome: "NOT_APPLIED"}
	x = m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, r))
	m.worker.Store.Activity = successful
	x = m.until(t, x.ID, "CANCELLED")
	m.counts(t, x, 2)
}
func TestM9WorkerIsolationRolesAndRevocation(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "isolated-review")
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	if e = workflow.Ready(context.Background(), m.f.db, "framework_app"); e != nil {
		t.Fatal("API readiness", e)
	}
	if e = workflow.Ready(context.Background(), m.db, "framework_worker"); e != nil {
		t.Fatal("worker readiness", e)
	}
	if _, _, e = jobs.ClaimWorkflow(context.Background(), m.f.db, uuid(t)); e == nil {
		t.Fatal("API claimed worker job")
	}
	dispatcher, _ := m8Roles(t)
	if _, e = dispatcher.Exec(`SELECT ir_body FROM runtime.workflow_bindings`); e == nil {
		t.Fatal("dispatcher read workflow payload")
	}
	if _, e = m.db.Exec(`SELECT inputs FROM runtime.workflow_bindings`); e == nil {
		var count int
		if e = m.db.QueryRow(`SELECT count(*) FROM runtime.workflow_bindings`).Scan(&count); e != nil || count != 0 {
			t.Fatal("unbound worker saw workflow")
		}
	}
	if e = jobs.WithJob(context.Background(), m.db, lease.Ticket, func(ctx context.Context, q postgres.Executor, a tenant.Access) error {
		var count int
		if e := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.workflow_bindings WHERE tenant_id=$1`, m.f.tenantB).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return errors.New("foreign payload exposed")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = m.f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, m.f.tenantA, m.f.alice); e != nil {
		t.Fatal(e)
	}
	if e = m.worker.Step(context.Background(), lease); e == nil {
		t.Fatal("revoked actor executed")
	}
	m.counts(t, x, 0)
}
