//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func r5eSetup(t *testing.T) (*m9Fixture, string, policy.Definition) {
	t.Helper()
	m := m9SetupDocument(t, func(doc *metadata.Document) {
		doc.SchemaVersion = metadata.ApprovalSchemaVersion
		for i := range doc.Definitions {
			d := &doc.Definitions[i]
			d.SchemaVersion = doc.SchemaVersion
			if w := d.Workflow; w != nil {
				w.DSLVersion = workflow.TenantRoleVersion
				w.TimeoutMS = 300000
				for j := range w.States {
					if s := &w.States[j]; s.Type == "approval" {
						s.ApprovalRoles = []string{"finance_manager", "owner"}
						s.ApprovalTimeoutMS = 120000
					}
				}
			}
		}
	})
	m.handler = r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	d := r5eDefinition(m.app, policy.DataScope{Kind: "ALL"})
	r5Publish(t, m.handler, root+"/roles", m.token, 0, d, true, uuid(t))
	return m, root, d
}
func r5eDefinition(app string, scope policy.DataScope) policy.Definition {
	d := r5Definition("finance_manager", app, scope)
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.read", "workflows.approve", "workflows.reject"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	return d
}
func r5eNoSignal(t *testing.T, m *m9Fixture, x workflow.Instance, token string, r workflow.Signal, want int) {
	t.Helper()
	w := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", token, r)
	if w.Code != want {
		t.Fatal("signal", w.Code, w.Body.String(), "want", want)
	}
	var version int64
	var receipts int
	if e := m.f.operator.QueryRow(`SELECT version,(SELECT count(*) FROM runtime.workflow_signals WHERE tenant_id=$1 AND instance_id=$2) FROM runtime.workflow_instances WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID).Scan(&version, &receipts); e != nil || version != x.Version || receipts != 0 {
		t.Fatal("rejected signal changed workflow", version, receipts, e)
	}
}
func TestM9RWorkflowApprovalCustomRolesAndImmutablePins(t *testing.T) {
	m, root, _ := r5eSetup(t)
	x := m.until(t, m.start(t, "custom-role-review").ID, "WAITING_APPROVAL")
	signal := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "finance-approval"}
	// Populated tenant B also has this role and a real owner. Neither that role nor
	// signed-but-untrusted tenant/role claims grant membership in tenant A.
	other := r5Handler(t, m.f, audit.Postgres{})
	r5Publish(t, other, "/v1/tenants/"+m.f.tenantB+"/roles", m.approver, 0, r5eDefinition(m.app, policy.DataScope{Kind: "ALL"}), true, uuid(t))
	r5eNoSignal(t, m, x, m.approver, signal, 403) // live admin is not in the pinned list
	forged := m.f.token(t, m.f.viewerSubject, map[string]any{"roles": []string{"finance_manager", "owner"}, "tenant_id": m.f.tenantB})
	r5eNoSignal(t, m, x, forged, signal, 403)
	r5eNoSignal(t, m, x, m.token, signal, 403) // owner is allowed by the plan, but self approval is forbidden
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	// Token was minted before assignment and claims an unrelated tenant/role.
	approver := m.f.token(t, m.f.bobSubject, map[string]any{"roles": []string{"viewer"}, "tenant_id": m.f.tenantB})
	stale := signal
	stale.ExpectedVersion--
	r5eNoSignal(t, m, x, approver, stale, 409)
	failing := r5Handler(t, m.f, failedAudit{})
	if w := m5Call(failing, m.base+"/instances/"+x.ID+"/signals", approver, signal); w.Code != 503 {
		t.Fatal("audit failure", w.Code)
	}
	r5eNoSignal(t, m, x, forged, signal, 403) // also verifies the fault rolled back all workflow writes
	// Register a newer source revision with a different allowed role. The pending
	// instance must still use its original IR/plan, not this current authoring graph.
	var doc metadata.Document
	_ = json.Unmarshal(m.raw, &doc)
	for i := range doc.Definitions {
		if w := doc.Definitions[i].Workflow; w != nil {
			for j := range w.States {
				if s := &w.States[j]; s.Type == "approval" {
					s.ApprovalRoles = []string{"admin"}
				}
			}
		}
	}
	raw, _ := json.Marshal(doc)
	if w := m3Call(m.handler, "POST", root+"/metadata/"+m.app+"/versions", m.token, 1, raw); w.Code != 201 {
		t.Fatal("new source", w.Code, w.Body.String())
	}
	accepted := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", approver, signal))
	duplicate := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", approver, signal))
	if accepted.Version != x.Version+1 || duplicate.Version != accepted.Version || accepted.Pin.IRHash != x.Pin.IRHash || accepted.Pin.PlanHash != x.Pin.PlanHash || accepted.Pin.Revision != 1 {
		t.Fatal("approval rewrote pin or duplicated transition")
	}
	// A previously successful key is no replacement for today's role requirement.
	r5Assign(t, m.handler, root, m.token, m.f.bob, "admin", 200)
	if w := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal); w.Code != 403 {
		t.Fatal("old receipt bypassed live role", w.Code)
	}
	var n int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='workflows.rejected' AND NOT allowed AND state->>'role'='admin'`, m.f.tenantA).Scan(&n); e != nil || n < 1 {
		t.Fatal("role denial evidence", n, e)
	}
	m.counts(t, accepted, 1)
}
func TestM9RWorkflowApprovalLiveCapabilitiesAndMembership(t *testing.T) {
	m, root, d := r5eSetup(t)
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	x := m.until(t, m.start(t, "live-role-review").ID, "WAITING_APPROVAL")
	signal := workflow.Signal{Operation: "reject", ExpectedVersion: x.Version, StateID: x.StateID, Key: "live-reject"}
	// Same permitted role name, wrong record scope.
	restricted := r5eDefinition(m.app, policy.DataScope{Kind: "OWNER"})
	r5Publish(t, m.handler, root+"/roles", m.token, 1, restricted, true, uuid(t))
	r5eNoSignal(t, m, x, m.approver, signal, 403)
	// Same name and record scope, but an explicit capability DENY wins.
	denied := r5eDefinition(m.app, policy.DataScope{Kind: "ALL"})
	denied.Grants = append(denied.Grants, policy.Grant{Effect: "DENY", Actions: []string{"workflows.reject"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, m.handler, root+"/roles", m.token, 2, denied, true, uuid(t))
	r5eNoSignal(t, m, x, m.approver, signal, 403)
	r5Publish(t, m.handler, root+"/roles", m.token, 3, d, false, uuid(t))
	r5eNoSignal(t, m, x, m.approver, signal, 403)
	r5Publish(t, m.handler, root+"/roles", m.token, 4, d, true, uuid(t))
	if _, e := m.f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, m.f.tenantA, m.f.bob); e != nil {
		t.Fatal(e)
	}
	r5eNoSignal(t, m, x, m.approver, signal, 403)
	if _, e := m.f.operator.Exec(`UPDATE iam.memberships SET active=true WHERE tenant_id=$1 AND user_id=$2`, m.f.tenantA, m.f.bob); e != nil {
		t.Fatal(e)
	}
	foreignPath := strings.Replace(m.base, m.f.tenantA, m.f.tenantB, 1) + "/instances/" + x.ID + "/signals"
	if w := m5Call(m.handler, foreignPath, m.approver, signal); w.Code != 404 {
		t.Fatal("foreign workflow access", w.Code, w.Body.String())
	}
	rejected := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	if rejected.StateID != "state.failed" || rejected.Pin.IRHash != x.Pin.IRHash {
		t.Fatal("custom rejection", rejected.StateID)
	}
	rejected = m.until(t, rejected.ID, "FAILED")
	// Rejection runs the already registered local compensator as a second effect.
	m.counts(t, rejected, 2)
}

func TestM9RWorkflowApprovalConcurrentRoleRevocation(t *testing.T) {
	m, root, d := r5eSetup(t)
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	x := m.until(t, m.start(t, "revocation-fence").ID, "WAITING_APPROVAL")
	principal, err := m.f.verifier.Verify(m.approver)
	if err != nil {
		t.Fatal(err)
	}
	newStore := func(user identity.User) workflow.Store {
		return access.WorkflowStore(policy.Runtime{}, audit.Postgres{}, events.Postgres{}, func(context.Context, postgres.Executor, tenant.Access) (identity.User, error) {
			return user, nil
		})
	}
	signal := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "fenced-approval"}
	ready, resume := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
			user, e := (identity.Store{}).Resolve(ctx, q, principal)
			if e != nil {
				close(ready)
				<-resume
				return e
			}
			a, e := m3Scope(m.f, ctx, q, principal, m.f.tenantA)
			close(ready)
			<-resume
			if e != nil {
				return e
			}
			_, e = newStore(user).Signal(ctx, q, a, x.ID, signal)
			return e
		})
	}()
	<-ready
	r5Publish(t, m.handler, root+"/roles", m.token, 1, d, false, uuid(t))
	close(resume)
	if err = <-result; !postgres.KnownAbort(err) {
		t.Fatal("stale REPEATABLE READ approval was not aborted", err)
	}
	r5eNoSignal(t, m, x, m.approver, signal, 403)
	r5Publish(t, m.handler, root+"/roles", m.token, 2, d, true, uuid(t))
	// Conversely, an admitted signal holds role authority until commit. A role
	// update must conflict instead of revoking behind an already checked signal.
	ready, resume = make(chan struct{}), make(chan struct{})
	go func() {
		result <- postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
			user, e := (identity.Store{}).Resolve(ctx, q, principal)
			if e != nil {
				close(ready)
				<-resume
				return e
			}
			a, e := m3Scope(m.f, ctx, q, principal, m.f.tenantA)
			if e == nil {
				_, e = newStore(user).Signal(ctx, q, a, x.ID, signal)
			}
			close(ready)
			<-resume
			return e
		})
	}()
	<-ready
	w := m5Call(m.handler, root+"/roles", m.token, map[string]any{"operation_id": uuid(t), "expected_version": 3, "active": false, "definition": d})
	close(resume)
	if err = <-result; err != nil {
		t.Fatal("admitted signal", err)
	}
	if w.Code != 409 {
		t.Fatal("role change crossed approval authority fence", w.Code, w.Body.String())
	}
	r5Publish(t, m.handler, root+"/roles", m.token, 3, d, false, uuid(t))
	var count int
	if err = m.f.operator.QueryRow(`SELECT count(*) FROM runtime.workflow_signals WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, x.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("fenced signal receipt", count, err)
	}
}

func TestM9RWorkflowApprovalSQLBoundaryAndStaleAccess(t *testing.T) {
	m, root, _ := r5eSetup(t)
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	principal, e := m.f.verifier.Verify(m.approver)
	if e != nil {
		t.Fatal(e)
	}
	var retained tenant.Access
	err := postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
		a, e := m3Scope(m.f, ctx, q, principal, m.f.tenantA)
		if e != nil {
			return e
		}
		retained = a
		var yes bool
		if e = q.QueryRowContext(ctx, `SELECT control.lock_workflow_approver($1,$2,$3)`, a.TenantID(), a.ActorID(), "finance_manager").Scan(&yes); e != nil || !yes {
			return errors.New("live role rejected")
		}
		for _, args := range [][]any{{m.f.tenantB, m.f.bob, "owner"}, {m.f.tenantA, m.f.alice, "finance_manager"}, {m.f.tenantA, m.f.bob, "owner"}, {m.f.tenantA, m.f.bob, "missing_role"}} {
			if e = q.QueryRowContext(ctx, `SELECT control.lock_workflow_approver($1,$2,$3)`, args...).Scan(&yes); e != nil || yes {
				return errors.New("approval context mismatch accepted")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var yes bool
	if e = m.f.db.QueryRow(`SELECT control.lock_workflow_approver($1,$2,'finance_manager')`, m.f.tenantA, m.f.bob).Scan(&yes); e != nil || yes {
		t.Fatal("contextless approval", e)
	}
	for _, db := range []*sql.DB{m.db, m.worker.dispatcher} {
		if e = db.QueryRow(`SELECT control.lock_workflow_approver($1,$2,'finance_manager')`, m.f.tenantA, m.f.bob).Scan(&yes); e == nil {
			t.Fatal("worker/dispatcher reached approval port")
		}
	}
	r5Assign(t, m.handler, root, m.token, m.f.bob, "admin", 200)
	err = postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(m.f, ctx, q, principal, m.f.tenantA); e != nil {
			return e
		}
		yes, e := (policy.RoleStore{}).LockWorkflowApprover(ctx, q, retained, []string{"finance_manager"})
		if e != nil {
			return e
		}
		if yes {
			return errors.New("retained access bypassed current membership")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if e = workflow.Ready(context.Background(), m.f.db, "framework_app"); e != nil {
		t.Fatal("app readiness", e)
	}
	if e = workflow.Ready(context.Background(), m.db, "framework_worker"); e != nil {
		t.Fatal("worker readiness", e)
	}
}
