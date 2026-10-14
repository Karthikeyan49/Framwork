//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/app"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type m9FailedEvents struct{}
type m9ScopePolicy struct{ hideField bool }

func (m9ScopePolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	return (policy.Native{}).Decide(ctx, s, a, r, c)
}
func (p m9ScopePolicy) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	if !p.hideField || r.FieldID == "field.contact.status" {
		return policy.DataScope{Kind: "NONE"}, nil
	}
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}

func (m9FailedEvents) AppendWorkflow(context.Context, postgres.Executor, events.WorkflowEvent) error {
	return errors.New("injected lifecycle write failure")
}
func TestM9AtomicActivityRollbackDuplicateFencingAndAuditFailure(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "atomic-review")
	m.step(t)
	x = m.get(t, x.ID)
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	successful := m.worker.Store.Events
	m.worker.Store.Events = m9FailedEvents{}
	if e = m.worker.Step(context.Background(), lease); e == nil {
		t.Fatal("outbox failure hidden")
	}
	m.counts(t, x, 0)
	m.worker.Store.Events = successful
	m.worker.Store.Audit = failedAudit{}
	if e = m.worker.Step(context.Background(), lease); e == nil {
		t.Fatal("audit failure hidden")
	}
	m.counts(t, x, 0)
	m.worker.Store.Audit = audit.Postgres{}
	if e = m.worker.Step(context.Background(), lease); e != nil {
		t.Fatal(e)
	}
	x = m.get(t, x.ID)
	m.counts(t, x, 1)
	if e = m.worker.Step(context.Background(), lease); !errors.Is(e, workflow.ErrLease) {
		t.Fatal("duplicate fenced ticket", e)
	}
	m.counts(t, x, 1)
}
func TestM9ExpiredLeaseOnEarlySaveAndConcurrentSignals(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "fenced-start")
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	authorize := m.worker.Store.Authorize
	m.worker.Store.Authorize = func(ctx context.Context, q postgres.Executor, a tenant.Access, action string, pin workflow.Pin, fields []string) (audit.Entry, error) {
		entry, e := authorize(ctx, q, a, action, pin, fields)
		if e != nil {
			return entry, e
		}
		_, e = q.ExecContext(ctx, `SELECT pg_sleep(0.3)`)
		if e != nil {
			return entry, e
		}
		_, e = q.ExecContext(ctx, `SELECT set_config('app.m9_test','lease-delay',true)`)
		return entry, e
	}
	// Expiry is set before execution; delays stay below WithRuntime's3s budget.
	if _, e = m.f.operator.Exec(`UPDATE control.workflow_jobs SET lease_until=clock_timestamp()+interval '200 milliseconds' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	if e = m.worker.Step(context.Background(), lease); !errors.Is(e, workflow.ErrLease) {
		t.Fatal("expired early save", e)
	}
	m.counts(t, x, 0)
	m.worker.Store.Authorize = authorize
	if _, e = m.f.operator.Exec(`UPDATE control.workflow_jobs SET due_at=clock_timestamp()-interval '1 second',lease_until=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, op := range []string{"approve", "reject"} {
		wg.Go(func() {
			r := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, workflow.Signal{Operation: op, ExpectedVersion: x.Version, StateID: x.StateID, Key: op + "-race"})
			codes <- r.Code
		})
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 409 && code != 503 {
			t.Fatal("signal race response", code)
		}
	}
	if success != 1 {
		t.Fatal("concurrent signals both applied", success)
	}
}
func TestM9ConditionFailureAfterEffectCompensates(t *testing.T) {
	m := m9Setup(t, func(w *metadata.Workflow) {
		for i := range w.States {
			if w.States[i].ID == "state.activity" {
				w.States[i].Transitions = []metadata.Transition{{To: "state.timer", Condition: &metadata.Expression{Op: "gt", Args: []metadata.Expression{{Op: "div", Args: []metadata.Expression{{Op: "literal", Value: json.RawMessage(`1`)}, {Op: "literal", Value: json.RawMessage(`0`)}}}, {Op: "literal", Value: json.RawMessage(`0`)}}}}, {To: "state.failed"}}
			}
		}
	})
	x := m.start(t, "failed-condition")
	x = m.until(t, x.ID, "FAILED")
	m.counts(t, x, 2)
}
func TestM9RecordTriggersAreAtomicAndNeverRehydratedByM8(t *testing.T) {
	m := m9Setup(t, func(w *metadata.Workflow) { w.Trigger = "record.created" })
	var id string
	if e := m.f.operator.QueryRow(`SELECT id::text FROM runtime.workflow_bindings WHERE tenant_id=$1 AND app_id=$2`, m.f.tenantA, m.app).Scan(&id); e != nil {
		t.Fatal("trigger instance", e)
	}
	x := m.until(t, id, "WAITING_APPROVAL")
	m.counts(t, x, 1)
	if r := m5Call(m.handler, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "manual-for-event"}); r.Code != 422 {
		t.Fatal("manual trigger bypass", r.Code)
	}
	d, _ := m8Roles(t)
	dispatch := events.Dispatcher{DB: d}
	p := m8Claim(t, dispatch)
	if e := dispatch.Finish(context.Background(), p, true); e != nil {
		t.Fatal(e)
	}
	consumer := events.Consumer{DB: m.db, Policy: policy.Native{}, Audit: audit.Postgres{}}
	if e := consumer.Consume(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	if e := consumer.Consume(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	var n int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.workflow_bindings WHERE tenant_id=$1`, m.f.tenantA).Scan(&n); e != nil || n != 1 {
		t.Fatal("M8 replay started new workflow", n, e)
	}
}
func TestM9DataScopeAndFieldDenialApplyToSnapshotsAndResume(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "scoped-review")
	m.step(t)
	x = m.get(t, x.ID)
	originalActivity := m.worker.Store.Activity
	m.worker.Store = access.WorkerWorkflowStore(m9ScopePolicy{}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var id string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&id); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, id)
	})
	m.worker.Store.Activity = originalActivity
	l, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	if e = m.worker.Step(context.Background(), l); !errors.Is(e, workflow.ErrNotFound) && !errors.Is(e, workflow.ErrDenied) {
		t.Fatal("source DataScope ignored", e)
	}
	m.counts(t, x, 0)
}
func TestM9ReadinessMissingColumnAndTriggerFailClosed(t *testing.T) {
	m := m9Setup(t, nil)
	for _, change := range []struct {
		sql, undo string
		db        *sql.DB
		role      string
	}{
		{`REVOKE INSERT(result_hash) ON runtime.workflow_history FROM framework_worker`, `GRANT INSERT(result_hash) ON runtime.workflow_history TO framework_worker`, m.db, "framework_worker"},
		{`REVOKE INSERT(request_hash) ON runtime.workflow_signals FROM framework_app`, `GRANT INSERT(request_hash) ON runtime.workflow_signals TO framework_app`, m.f.db, "framework_app"},
		{`ALTER TABLE runtime.workflow_bindings DISABLE TRIGGER route_workflow`, `ALTER TABLE runtime.workflow_bindings ENABLE TRIGGER route_workflow`, m.f.db, "framework_app"},
	} {
		if _, e := m.f.operator.Exec(change.sql); e != nil {
			t.Fatal(e)
		}
		if e := workflow.Ready(context.Background(), change.db, change.role); e == nil {
			t.Fatal("missing contract reported ready")
		}
		if _, e := m.f.operator.Exec(change.undo); e != nil {
			t.Fatal(e)
		}
		if e := workflow.Ready(context.Background(), change.db, change.role); e != nil {
			t.Fatal("restored contract", e)
		}
	}
}
func TestM9TimerTimeoutAndFailurePinCannotSwitch(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "pinned-review")
	oldLease := m.step(t)
	// Store a new metadata version; old instance must retain the stored IR/plan.
	var doc metadata.Document
	_ = json.Unmarshal(m.raw, &doc)
	for i := range doc.Definitions {
		if w := doc.Definitions[i].Workflow; w != nil {
			w.Version = "2.0.0"
			w.States[0].Transitions[0].To = "state.failed"
		}
	}
	raw, _ := json.Marshal(doc)
	if r := m3Call(m.handler, "POST", "/v1/tenants/"+m.f.tenantA+"/metadata/"+m.app+"/versions", m.token, 1, raw); r.Code != 201 {
		t.Fatal(r.Code)
	}
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	if x.Pin.WorkflowVersion != "1.0.0" || x.Pin.Revision != 1 {
		t.Fatal("instance repinned")
	}
	if _, e := m.f.operator.Exec(`UPDATE control.job_routes SET release_hash=$3 WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID, strings.Repeat("b", 64)); e != nil {
		t.Fatal(e)
	}
	// An old ticket after a route pin changes must fail WithJob, not silently adopt it.
	if e := m.worker.Step(context.Background(), oldLease); !errors.Is(e, jobs.ErrJob) && !errors.Is(e, workflow.ErrLease) {
		t.Fatal("old genuinely minted pin accepted", e)
	}
}

type m9KnownRejectedEvents struct{}
type m9DenyActionPolicy struct{ policy.Native }

func (m9DenyActionPolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	if a == "workflows.start" {
		return policy.Decision{Allow: false, ID: "actual-workflow-denial", Version: "m9-review-policy", Obligations: []string{"audit"}}
	}
	return (policy.Native{}).Decide(ctx, s, a, r, c)
}

func (m9KnownRejectedEvents) AppendWorkflow(context.Context, postgres.Executor, events.WorkflowEvent) error {
	return workflow.ErrInvalid
}
func TestM9RejectedMutationsRollbackBeforeHTTPResponse(t *testing.T) {
	m := m9Setup(t, nil)
	api := access.API{DB: m.f.db, Auth: m.f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true, WorkflowEvents: m9KnownRejectedEvents{}}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	r := m5Call(h, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "failed-lifecycle"})
	if r.Code != 400 {
		t.Fatal("known rejection response", r.Code, r.Body.String())
	}
	var bindings, history, outbox, routes, rejected int
	if e := m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.workflow_bindings WHERE tenant_id=$1),(SELECT count(*) FROM runtime.workflow_history WHERE tenant_id=$1),(SELECT count(*) FROM runtime.workflow_outbox WHERE tenant_id=$1),(SELECT count(*) FROM control.workflow_jobs WHERE tenant_id=$1),(SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='workflows.rejected')`, m.f.tenantA).Scan(&bindings, &history, &outbox, &routes, &rejected); e != nil {
		t.Fatal(e)
	}
	if bindings+history+outbox+routes != 0 || rejected != 1 {
		t.Fatal("partial business commit or missing denial audit", bindings, history, outbox, routes, rejected)
	}
	x := m.start(t, "budget-boundary")
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	// Synthetic owned fixture puts a valid waiting state at the history budget;
	// every existing business artifact must remain unchanged on rejection.
	if _, e := m.f.operator.Exec(`UPDATE runtime.workflow_instances SET version=1000 WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	before := m.get(t, x.ID)
	r = m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, workflow.Signal{Operation: "approve", ExpectedVersion: 1000, StateID: x.StateID, Key: "budget-approval"})
	if r.Code != 400 {
		t.Fatal("budget rejection", r.Code, r.Body.String())
	}
	after := m.get(t, x.ID)
	if before.Version != after.Version || before.StateID != after.StateID || before.Status != after.Status || len(before.History) != len(after.History) {
		t.Fatal("budget rejection mutated instance")
	}
}

func TestM9RWorkerSecurityErrorClassification(t *testing.T) {
	t.Run("cancelled recheck retains actual permission error", func(t *testing.T) {
		m := m9Setup(t, nil)
		x := m.start(t, "cancelled-recheck")
		lease, found, e := m.worker.Claim(context.Background())
		if e != nil || !found {
			t.Fatal(e)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m.worker.Store.Authorize = func(ctx context.Context, q postgres.Executor, a tenant.Access, action string, pin workflow.Pin, fields []string) (audit.Entry, error) {
			_, e := q.ExecContext(ctx, `SELECT custom_data FROM runtime.records LIMIT 1`)
			cancel()
			return audit.Entry{}, e
		}
		e = m.worker.Step(ctx, lease)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "42501" || errors.Is(e, workflow.ErrLease) {
			t.Fatal("cancelled validation masked real error", e)
		}
		m.counts(t, x, 0)
	})
	t.Run("valid ticket retains real permission error", func(t *testing.T) {
		m := m9Setup(t, nil)
		x := m.start(t, "permission-classification")
		lease, found, e := m.worker.Claim(context.Background())
		if e != nil || !found {
			t.Fatal(e)
		}
		m.worker.Store.Authorize = func(ctx context.Context, q postgres.Executor, a tenant.Access, action string, pin workflow.Pin, fields []string) (audit.Entry, error) {
			_, e := q.ExecContext(ctx, `SELECT custom_data FROM runtime.records LIMIT 1`)
			return audit.Entry{}, e
		}
		e = m.worker.Step(context.Background(), lease)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "42501" || errors.Is(e, workflow.ErrLease) {
			t.Fatal("valid ticket masked permission failure", e)
		}
		m.counts(t, x, 0)
	})
	t.Run("valid ticket retains database exception", func(t *testing.T) {
		m := m9Setup(t, nil)
		x := m.start(t, "exception-classification")
		lease, found, e := m.worker.Claim(context.Background())
		if e != nil || !found {
			t.Fatal(e)
		}
		m.worker.Store.Authorize = func(ctx context.Context, q postgres.Executor, a tenant.Access, action string, pin workflow.Pin, fields []string) (audit.Entry, error) {
			_, e := q.ExecContext(ctx, `DO $$ BEGIN RAISE EXCEPTION 'owned regression exception'; END $$`)
			return audit.Entry{}, e
		}
		e = m.worker.Step(context.Background(), lease)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || pg.Code != "P0001" || errors.Is(e, workflow.ErrLease) {
			t.Fatal("valid exception masked as expiry", e)
		}
		m.counts(t, x, 0)
	})
	t.Run("expiry during audit rolls back and becomes lease error", func(t *testing.T) {
		m := m9Setup(t, nil)
		x := m.start(t, "expired-audit-classification")
		lease, found, e := m.worker.Claim(context.Background())
		if e != nil || !found {
			t.Fatal(e)
		}
		if _, e = m.f.operator.Exec(`UPDATE control.workflow_jobs SET lease_until=clock_timestamp()+interval '500 milliseconds' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
			t.Fatal(e)
		}
		attempted := false
		m.worker.Store.Authorize = func(ctx context.Context, q postgres.Executor, a tenant.Access, action string, pin workflow.Pin, fields []string) (audit.Entry, error) {
			var valid bool
			if e := q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM control.worker_authority())`).Scan(&valid); e != nil {
				return audit.Entry{}, e
			}
			if !valid {
				return audit.Entry{}, fmt.Errorf("fixture missing initial authority")
			}
			if _, e := q.ExecContext(ctx, `SELECT pg_sleep(0.6)`); e != nil {
				return audit.Entry{}, e
			}
			attempted = true
			entry := audit.Entry{ActorID: a.ActorID(), TenantID: a.TenantID(), Action: "workflows.execute", Resource: "workflow", Allow: true, DecisionID: "owned-expired-audit", PolicyVersion: policy.Version, RequestID: pin.RequestID}
			return entry, (audit.Postgres{}).Append(ctx, q, entry)
		}
		e = m.worker.Step(context.Background(), lease)
		if !attempted || !errors.Is(e, workflow.ErrLease) {
			t.Fatal("expired audit did not retain lease contract", attempted, e)
		}
		m.counts(t, x, 0)
	})
}
func TestM9FieldScopeDeniesStartSnapshotAndWorkerResume(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "field-scope-native")
	m.step(t)
	x = m.get(t, x.ID)
	api := access.API{DB: m.f.db, Auth: m.f.verifier, Policy: m9ScopePolicy{hideField: true}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	r := m5Call(h, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "denied-field-snapshot"})
	if r.Code != 404 && r.Code != 403 {
		t.Fatal("field snapshot denial", r.Code, r.Body.String())
	}
	var n int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.workflow_bindings WHERE tenant_id=$1`, m.f.tenantA).Scan(&n); e != nil || n != 1 {
		t.Fatal("denied snapshot persisted", n, e)
	}
	activity := m.worker.Store.Activity
	m.worker.Store = access.WorkerWorkflowStore(m9ScopePolicy{hideField: true}, audit.Postgres{}, events.Postgres{}, func(ctx context.Context, q postgres.Executor, a tenant.Access) (identity.User, error) {
		var id string
		if e := q.QueryRowContext(ctx, `SELECT current_setting('app.job_id',true)`).Scan(&id); e != nil {
			return identity.User{}, e
		}
		return (identity.Store{}).ResolveWorker(ctx, q, id)
	})
	m.worker.Store.Activity = activity
	l, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	if e = m.worker.Step(context.Background(), l); !errors.Is(e, workflow.ErrNotFound) && !errors.Is(e, workflow.ErrDenied) {
		t.Fatal("field scope ignored", e)
	}
	m.counts(t, x, 0)
}
func TestM9AppliedReconciliationPreservesFactAndCompensatesConditionFailure(t *testing.T) {
	m := m9Setup(t, func(w *metadata.Workflow) {
		for i := range w.States {
			if w.States[i].ID == "state.activity" {
				w.States[i].Transitions = []metadata.Transition{{To: "state.timer", Condition: &metadata.Expression{Op: "gt", Args: []metadata.Expression{{Op: "div", Args: []metadata.Expression{{Op: "literal", Value: json.RawMessage(`1`)}, {Op: "literal", Value: json.RawMessage(`0`)}}}, {Op: "literal", Value: json.RawMessage(`0`)}}}}, {To: "state.failed"}}
			}
		}
	})
	x := m.start(t, "applied-reconciliation")
	m.step(t)
	successful := m.worker.Store.Activity
	m.worker.Store.Activity = func(context.Context, postgres.Executor, tenant.Access, workflow.ActivityRequest) (json.RawMessage, error) {
		return nil, workflow.Unknown()
	}
	x = m.until(t, x.ID, "RECONCILE")
	r := workflow.Signal{Operation: "reconcile", ExpectedVersion: x.Version, StateID: x.StateID, Key: "verified-applied", OperatorRef: "external-ledger-review", Outcome: "APPLIED", Output: json.RawMessage(`{"completed":true}`)}
	x = m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, r))
	if x.Status != "COMPENSATING" || x.Termination != "FAILED" {
		t.Fatal("applied fact stranded", x.Status)
	}
	m.worker.Store.Activity = successful
	x = m.until(t, x.ID, "FAILED")
	m.counts(t, x, 1)
	var n int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.workflow_receipts WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, x.ID).Scan(&n); e != nil || n != 2 {
		t.Fatal("lost applied/compensation receipt", n, e)
	}
}
func TestM9WorkflowOnlyStartupRequiresAuditAndRunsWithoutEventBroker(t *testing.T) {
	m := m9Setup(t, nil)
	c := config.Config{Environment: "test", DatabaseURL: os.Getenv("DATABASE_URL"), DispatcherDatabaseURL: os.Getenv("DISPATCHER_DATABASE_URL"), WorkerDatabaseURL: os.Getenv("WORKER_DATABASE_URL"), WorkflowsEnabled: true, DependencyTimeout: 2 * time.Second}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if _, e := m.f.operator.Exec(`REVOKE INSERT(state) ON audit.entries FROM framework_worker`); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	e := app.Run(ctx, "framework-worker", c, logger)
	cancel()
	if e == nil {
		t.Fatal("workflow worker started without mandatory audit")
	}
	if _, e := m.f.operator.Exec(`GRANT INSERT(state) ON audit.entries TO framework_worker`); e != nil {
		t.Fatal(e)
	}
	x := m.start(t, "workflow-only-service")
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	dispatchDone := make(chan error, 1)
	go func() { dispatchDone <- app.Run(ctx, "framework-dispatcher", c, logger) }()
	go func() { done <- app.Run(ctx, "framework-worker", c, logger) }()
	deadline := time.Now().Add(12 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		x = m.get(t, x.ID)
		if x.Status == "WAITING_APPROVAL" {
			ready = true
			break
		}
		select {
		case e := <-done:
			t.Fatal("workflow service exited", e)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		var state string
		var generation, failures int64
		var begun bool
		_ = m.f.operator.QueryRow(`SELECT state,generation,lease_failures,begun FROM control.workflow_jobs WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID).Scan(&state, &generation, &failures, &begun)
		t.Log("routing state", state, "generation", generation, "failures", failures, "picked up", begun, "instance", x.Status)
		select {
		case e := <-dispatchDone:
			t.Log("dispatcher exited", e)
		default:
		}
		t.Fatal("workflow-only process did not execute")
	}
	cancel()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal("graceful workflow shutdown", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("workflow shutdown stalled")
	}
	select {
	case e := <-dispatchDone:
		if e != nil {
			t.Fatal("dispatcher shutdown", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("dispatcher shutdown stalled")
	}
	m.counts(t, x, 0)
}
func TestM9PolicyDenialEvidenceSurvivesBusinessRollback(t *testing.T) {
	m := m9Setup(t, nil)
	api := access.API{DB: m.f.db, Auth: m.f.verifier, Policy: m9DenyActionPolicy{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	h := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	r := m5Call(h, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "policy-denied"})
	if r.Code != 403 {
		t.Fatal(r.Code, r.Body.String())
	}
	var id, version string
	var n int
	if e := m.f.operator.QueryRow(`SELECT decision_id,policy_version,(SELECT count(*) FROM runtime.workflow_bindings WHERE tenant_id=$1) FROM audit.entries WHERE tenant_id=$1 AND action='workflows.rejected' ORDER BY id DESC LIMIT 1`, m.f.tenantA).Scan(&id, &version, &n); e != nil || id != "actual-workflow-denial" || version != "m9-review-policy" || n != 0 {
		t.Fatal("incorrect denial provenance", id, version, n, e)
	}
}
