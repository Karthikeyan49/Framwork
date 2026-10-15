//go:build integration

package integration

import (
	"context"
	"errors"
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

// Independent review witnesses include receipts and all durable effects. Denial
// audit entries may be appended, but none of these business facts may change.
type r5eReviewWitness struct{ version, history, outbox, signals, effects int64 }

func r5eReviewFacts(t *testing.T, m *m9Fixture, id string) r5eReviewWitness {
	t.Helper()
	var w r5eReviewWitness
	if err := m.f.operator.QueryRow(`SELECT version,
 (SELECT count(*) FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2),
 (SELECT count(*) FROM runtime.workflow_outbox WHERE tenant_id=$1 AND instance_id=$2),
 (SELECT count(*) FROM runtime.workflow_signals WHERE tenant_id=$1 AND instance_id=$2),
 (SELECT count(*) FROM runtime.m9_test_effects WHERE tenant_id=$1 AND instance_id=$2)
 FROM runtime.workflow_instances WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, id).Scan(&w.version, &w.history, &w.outbox, &w.signals, &w.effects); err != nil {
		t.Fatal("cannot load disposable workflow witnesses")
	}
	return w
}

func r5eReviewDeny(t *testing.T, m *m9Fixture, x workflow.Instance, token string, signal workflow.Signal, status int) {
	t.Helper()
	before := r5eReviewFacts(t, m, x.ID)
	if w := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", token, signal); w.Code != status {
		t.Fatalf("signal status %d, want %d", w.Code, status)
	}
	if after := r5eReviewFacts(t, m, x.ID); after != before {
		t.Fatal("rejected signal changed durable workflow witnesses")
	}
}

func r5eReviewDenialEvidence(t *testing.T, m *m9Fixture, x workflow.Instance, actor, role string) {
	t.Helper()
	var n int
	if err := m.f.operator.QueryRow(`SELECT count(*) FROM audit.entries
 WHERE tenant_id=$1 AND actor_id=$2 AND action='workflows.rejected' AND NOT allowed
 AND state->>'role'=$3 AND state->>'workflow_instance_id'=$4
 AND state->>'workflow_plan_hash'=$5 AND state->>'ir_hash'=$6
 AND (state->>'workflow_sequence')::bigint=$7 AND state->>'record_id'=$8
 AND (state->>'record_version')::bigint=$9 AND state->>'workflow_id'=$10
 AND state->>'workflow_version'=$11 AND state->>'metadata_app'=$12
 AND (state->>'metadata_revision')::bigint=$13`, m.f.tenantA, actor, role, x.ID, x.Pin.PlanHash, x.Pin.IRHash, x.Version, x.Pin.RecordID, x.Pin.RecordVersion, x.Pin.WorkflowID, x.Pin.WorkflowVersion, x.Pin.AppID, x.Pin.Revision).Scan(&n); err != nil || n != 1 {
		t.Fatal("denied approval did not retain exact live actor and immutable workflow witnesses")
	}
}

func r5eReviewDeniedDefinition(app, action, field string) policy.Definition {
	d := r5eDefinition(app, policy.DataScope{Kind: "ALL"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{action}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: field}})
	return d
}

func TestM9RWorkflowApprovalReviewFieldCapabilityAndReceiptRevocation(t *testing.T) {
	m, root, d := r5eSetup(t)
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	x := m.until(t, m.start(t, "review-field-revocation").ID, "WAITING_APPROVAL")
	signal := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "review-field-signal"}
	r5eReviewDeny(t, m, x, m.token, signal, 403)
	r5eReviewDenialEvidence(t, m, x, m.f.alice, "owner")
	fieldDenied := r5eReviewDeniedDefinition(m.app, "data.field.read", "field.contact.status")
	r5Publish(t, m.handler, root+"/roles", m.token, 1, fieldDenied, true, uuid(t))
	r5eReviewDeny(t, m, x, m.approver, signal, 403)
	r5Publish(t, m.handler, root+"/roles", m.token, 2, d, true, uuid(t))
	accepted := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	if accepted.Version != x.Version+1 {
		t.Fatal("approval did not advance exactly once")
	}
	// An old successful key must still require the captured field and action.
	r5Publish(t, m.handler, root+"/roles", m.token, 3, fieldDenied, true, uuid(t))
	r5eReviewDeny(t, m, accepted, m.approver, signal, 403)
	actionDenied := r5eReviewDeniedDefinition(m.app, "workflows.approve", "*")
	r5Publish(t, m.handler, root+"/roles", m.token, 4, actionDenied, true, uuid(t))
	r5eReviewDeny(t, m, accepted, m.approver, signal, 403)
	r5Publish(t, m.handler, root+"/roles", m.token, 5, d, false, uuid(t))
	r5eReviewDeny(t, m, accepted, m.approver, signal, 403)
	r5Publish(t, m.handler, root+"/roles", m.token, 6, d, true, uuid(t))
	before := r5eReviewFacts(t, m, x.ID)
	replay := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	if replay.Version != accepted.Version || r5eReviewFacts(t, m, x.ID) != before || before.signals != 1 {
		t.Fatal("restored authority replayed a transition or lost its receipt")
	}
	m.counts(t, accepted, 1)
}

func TestM9RWorkflowApprovalReviewDistinctStatesAndActors(t *testing.T) {
	m := m9SetupDocument(t, func(doc *metadata.Document) {
		doc.SchemaVersion = metadata.ApprovalSchemaVersion
		for i := range doc.Definitions {
			d := &doc.Definitions[i]
			d.SchemaVersion = doc.SchemaVersion
			if w := d.Workflow; w != nil {
				w.DSLVersion, w.TimeoutMS = workflow.TenantRoleVersion, 300000
				for j := range w.States {
					if s := &w.States[j]; s.Type == "approval" {
						s.ApprovalRoles, s.ApprovalTimeoutMS = []string{"finance_manager"}, 120000
						second := *s
						second.ID, second.ApprovalRoles = "state.second", []string{"quality_manager"}
						second.Transitions = append([]metadata.Transition(nil), s.Transitions...)
						for k := range s.Transitions {
							if s.Transitions[k].On == "approved" {
								s.Transitions[k].To = second.ID
							}
						}
						w.States = append(w.States, second)
						break
					}
				}
			}
		}
	})
	m.handler = r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	for _, name := range []string{"finance_manager", "quality_manager"} {
		d := r5eDefinition(m.app, policy.DataScope{Kind: "ALL"})
		d.Name = name
		r5Publish(t, m.handler, root+"/roles", m.token, 0, d, true, uuid(t))
	}
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	r5Assign(t, m.handler, root, m.token, m.f.viewer, "quality_manager", 200)
	quality := m.f.token(t, m.f.viewerSubject, map[string]any{"roles": []string{"owner", "finance_manager"}, "tenant_id": m.f.tenantB})
	x := m.until(t, m.start(t, "review-two-roles").ID, "WAITING_APPROVAL")
	first := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "review-state-key"}
	r5eReviewDeny(t, m, x, quality, first, 403)
	r5eReviewDenialEvidence(t, m, x, m.f.viewer, "quality_manager")
	x = m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, first))
	if x.StateID != "state.second" || x.Status != "READY" {
		t.Fatal("did not enter distinct second approval")
	}
	x = m.until(t, x.ID, "WAITING_APPROVAL")
	before := r5eReviewFacts(t, m, x.ID)
	if replay := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, first)); replay.Version != x.Version || r5eReviewFacts(t, m, x.ID) != before {
		t.Fatal("first-state receipt advanced the second state")
	}
	oldState := first
	oldState.Key = "review-fresh-old-state"
	r5eReviewDeny(t, m, x, m.approver, oldState, 409)
	second := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: first.Key}
	r5eReviewDeny(t, m, x, m.approver, second, 403)
	r5eReviewDenialEvidence(t, m, x, m.f.bob, "finance_manager")
	r5eReviewDeny(t, m, x, quality, second, 409) // existing key also binds actor/state/version
	second.Key = "review-second-key"
	x = m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", quality, second))
	if x.StateID != "state.complete" || r5eReviewFacts(t, m, x.ID).signals != 2 {
		t.Fatal("distinct approver did not advance exactly once")
	}
	m.counts(t, x, 1)
}

func TestM9RWorkflowApprovalReviewStaleMembershipUserAndRoleBody(t *testing.T) {
	m, root, d := r5eSetup(t)
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	x := m.until(t, m.start(t, "review-stale-authority").ID, "WAITING_APPROVAL")
	principal, err := m.f.verifier.Verify(m.approver)
	if err != nil {
		t.Fatal("fixture identity verification failed")
	}
	signal := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "review-stale-signal"}
	version := int64(1)
	for _, kind := range []string{"membership", "user", "assignment", "field", "capability"} {
		t.Run(kind, func(t *testing.T) {
			ready, resume := make(chan error, 1), make(chan struct{})
			result := make(chan error, 1)
			before := r5eReviewFacts(t, m, x.ID)
			go func() {
				result <- postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
					u, e := (identity.Store{}).Resolve(ctx, q, principal)
					var a tenant.Access
					if e == nil {
						e = (tenant.Store{}).BindActor(ctx, q, u)
					}
					if e == nil {
						a, e = (tenant.Store{}).Resolve(ctx, q, u, m.f.tenantA)
					}
					ready <- e
					<-resume
					if e != nil {
						return e
					}
					store := access.WorkflowStore(policy.Runtime{}, audit.Postgres{}, events.Postgres{}, func(context.Context, postgres.Executor, tenant.Access) (identity.User, error) { return u, nil })
					_, e = store.Signal(ctx, q, a, x.ID, signal)
					return e
				})
			}()
			defer close(resume)
			if <-ready != nil {
				t.Fatal("could not establish stale fixture snapshot")
			}
			switch kind {
			case "membership":
				_, err = m.f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, m.f.tenantA, m.f.bob)
			case "user":
				_, err = m.f.operator.Exec(`UPDATE iam.users SET active=false WHERE id=$1`, m.f.bob)
			case "assignment":
				r5Assign(t, m.handler, root, m.token, m.f.bob, "admin", 200)
			case "field", "capability":
				action, field := "data.field.read", "field.contact.status"
				if kind == "capability" {
					action, field = "workflows.approve", "*"
				}
				r5Publish(t, m.handler, root+"/roles", m.token, version, r5eReviewDeniedDefinition(m.app, action, field), true, uuid(t))
				version++
			}
			if err != nil {
				t.Fatal("disposable revocation failed")
			}
			resume <- struct{}{}
			if e := <-result; !postgres.KnownAbort(e) {
				t.Fatal("stale approval did not fail with a known transaction abort")
			}
			if r5eReviewFacts(t, m, x.ID) != before {
				t.Fatal("stale approval persisted effects")
			}
			switch kind {
			case "membership":
				_, err = m.f.operator.Exec(`UPDATE iam.memberships SET active=true WHERE tenant_id=$1 AND user_id=$2`, m.f.tenantA, m.f.bob)
			case "user":
				_, err = m.f.operator.Exec(`UPDATE iam.users SET active=true WHERE id=$1`, m.f.bob)
			case "assignment":
				r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
			case "field", "capability":
				r5Publish(t, m.handler, root+"/roles", m.token, version, d, true, uuid(t))
				version++
			}
			if err != nil {
				t.Fatal("disposable authority restoration failed")
			}
		})
	}
	x = m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	m.counts(t, x, 1)
}

func TestM9RWorkflowApprovalReviewSQLBindingAndReadiness(t *testing.T) {
	m, root, _ := r5eSetup(t)
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	principal, err := m.f.verifier.Verify(m.approver)
	if err != nil {
		t.Fatal("fixture identity verification failed")
	}
	err = postgres.WithRuntime(context.Background(), m.f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(m.f, ctx, q, principal, m.f.tenantA); e != nil {
			return e
		}
		var yes bool
		if e := q.QueryRowContext(ctx, `SELECT control.lock_workflow_approver($1,$2,'finance_manager')`, m.f.tenantA, m.f.bob).Scan(&yes); e != nil || !yes {
			return errors.New("matching live approver rejected")
		}
		// actor_id alone is insufficient: issuer/subject still identify Bob.
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.actor_id',$1,true)`, m.f.alice); e != nil {
			return e
		}
		if e := q.QueryRowContext(ctx, `SELECT control.lock_workflow_approver($1,$2,'owner')`, m.f.tenantA, m.f.alice).Scan(&yes); e != nil || yes {
			return errors.New("actor GUC bypassed issuer/subject binding")
		}
		return nil
	})
	if err != nil {
		t.Fatal("SQL approver binding probe failed")
	}
	const port = "control.lock_workflow_approver(uuid,uuid,text)"
	var safe bool
	if err = m.f.operator.QueryRow(`SELECT p.prosecdef AND NOT r.rolcanlogin AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
 AND has_function_privilege('framework_app',p.oid,'EXECUTE')
 AND NOT has_function_privilege('framework_worker',p.oid,'EXECUTE')
 AND NOT has_function_privilege('framework_dispatcher',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(p.proacl) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
 FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=$1::regprocedure AND r.rolname='framework_schema_owner'`, port).Scan(&safe); err != nil || !safe {
		t.Fatal("SQL port owner/path/grants contract failed")
	}
	if _, err = m.f.operator.Exec(`GRANT EXECUTE ON FUNCTION control.lock_workflow_approver(uuid,uuid,text) TO framework_worker`); err != nil {
		t.Fatal("cannot inject disposable grant drift")
	}
	if workflow.Ready(context.Background(), m.db, "framework_worker") == nil {
		t.Fatal("worker readiness accepted approval EXECUTE drift")
	}
	var yes bool
	if err = m.db.QueryRow(`SELECT control.lock_workflow_approver($1,$2,'finance_manager')`, m.f.tenantA, m.f.bob).Scan(&yes); err != nil || yes {
		t.Fatal("helper lost its app-only session fence after grant drift")
	}
	if _, err = m.f.operator.Exec(`REVOKE EXECUTE ON FUNCTION control.lock_workflow_approver(uuid,uuid,text) FROM framework_worker; REVOKE EXECUTE ON FUNCTION control.lock_workflow_approver(uuid,uuid,text) FROM framework_app`); err != nil {
		t.Fatal("cannot revoke disposable approval grant")
	}
	if workflow.Ready(context.Background(), m.f.db, "framework_app") == nil {
		t.Fatal("app readiness accepted missing approval port")
	}
	if _, err = m.f.operator.Exec(`GRANT EXECUTE ON FUNCTION control.lock_workflow_approver(uuid,uuid,text) TO framework_app`); err != nil {
		t.Fatal("cannot restore disposable approval grant")
	}
	for _, role := range []string{"framework_app", "framework_worker"} {
		db := m.f.db
		if role == "framework_worker" {
			db = m.db
		}
		if workflow.Ready(context.Background(), db, role) != nil {
			t.Fatal("readiness failed after restoring reviewed grants")
		}
	}
}
