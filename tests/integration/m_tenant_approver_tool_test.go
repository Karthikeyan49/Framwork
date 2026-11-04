//go:build integration

package integration

import (
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

// TestIntegrationTenantRoleApproverGatesAToolSend is the combination of the two lines that no test of either line
// could contain: a workflow of authoring schema 1.5.0 (tool activities, the milestone line) whose approval state names a
// role that the tenant defined itself (finance_manager, the remediation line's tenant roles, authoring 1.3.0 and DSL 2.1.0). The
// merged compiler pins both in one plan; the database and the API must then keep the two promises together: nobody who does
// not hold the role at the moment of the decision can release the send, and nothing reaches the provider before a holder
// has approved. Run through the real routes, the real worker and a provider that records every request.
func TestIntegrationTenantRoleApproverGatesAToolSend(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true, approvers: []string{"finance_manager", "owner"}, customRoles: true})
	root := "/v1/tenants/" + m.f.tenantA
	r5Publish(t, m.handler, root+"/roles", m.token, 0, r5eDefinition(m.app, policy.DataScope{Kind: "ALL"}), true, uuid(t))

	sent := func() int { return m.provider.requests("", "/notify") }
	x := m.startFlow(t, "native")
	x = m.drive(t, x.ID, "WAITING_APPROVAL")

	// The pinned plan carries both contracts: the tenant role of the approval and the pinned tool of the activity.
	var dsl, irVersion, schema string
	var roles, contracts, hashes int
	if e := m.f.operator.QueryRow(`SELECT convert_from(plan_body,'UTF8')::jsonb->>'dsl_version',
 convert_from(ir_body,'UTF8')::jsonb->>'ir_version', convert_from(ir_body,'UTF8')::jsonb->'application'->>'schema_version',
 (SELECT count(*) FROM jsonb_array_elements(convert_from(plan_body,'UTF8')::jsonb->'states') s WHERE s->'approval_roles' ? 'finance_manager'),
 (SELECT count(*) FROM jsonb_array_elements(convert_from(plan_body,'UTF8')::jsonb->'states') s WHERE s ? 'tool_contract'),
 (SELECT count(*) FROM jsonb_array_elements(convert_from(plan_body,'UTF8')::jsonb->'states') s WHERE s->>'tool_hash' ~ '^[0-9a-f]{64}$')
 FROM runtime.workflow_bindings WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID).Scan(&dsl, &irVersion, &schema, &roles, &contracts, &hashes); e != nil {
		t.Fatal("pinned plan", e)
	}
	if dsl != workflow.ToolDSLVersion || irVersion != "1.5.0" || schema != "1.5.0" || roles != 1 || contracts != 1 || hashes != 1 {
		t.Fatalf("pinned plan: dsl %s, ir %s, schema %s, %d tenant-role approval states, %d tool contracts, %d tool hashes", dsl, irVersion, schema, roles, contracts, hashes)
	}

	signal := workflow.Signal{Operation: "approve", ExpectedVersion: x.Version, StateID: x.StateID, Key: "finance-approval", OperatorRef: "test-review"}
	noSend := func(why, token string) {
		t.Helper()
		r5eNoSignal(t, m.m9Fixture, x, token, signal, 403)
		if sent() != 0 || m.ledgerRows(t, x.ID) != 0 {
			t.Fatalf("%s: the provider was reached (%d requests, %d ledger rows)", why, sent(), m.ledgerRows(t, x.ID))
		}
	}
	noSend("an admin who does not hold the tenant role", m.approver)
	noSend("the requesting owner (self approval)", m.token)
	forged := m.f.token(t, m.f.viewerSubject, map[string]any{"roles": []string{"finance_manager", "owner"}, "tenant_id": m.f.tenantA})
	noSend("a signed claim of the role", forged)
	if again := m.drive(t, x.ID, "WAITING_APPROVAL"); again.Version != x.Version {
		t.Fatal("a refused approval moved the instance", again.Version, x.Version)
	}

	// A holder of the role approves; the worker then sends exactly once.
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	accepted := m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal))
	if accepted.Version != x.Version+1 || accepted.Pin.PlanHash != x.Pin.PlanHash || accepted.Pin.IRHash != x.Pin.IRHash {
		t.Fatal("approval rewrote the pin or did not advance", accepted.Version, x.Version)
	}
	x = m.drive(t, x.ID, "SUCCEEDED")
	if l := m.ledger(t, x.ID); l.State != "SUCCEEDED" || l.Attempts != 1 || sent() != 1 || m.provider.effectCount() != 1 {
		t.Fatalf("one approved send expected: ledger %+v, %d requests, %d effects", l, sent(), m.provider.effectCount())
	}

	// The same role, revoked before the next decision, closes the gate again: the pending instance of the second start
	// cannot be released by a user who lost the role, and the provider sees nothing until a holder acts.
	y := m.drive(t, m.startFlow(t, "native").ID, "WAITING_APPROVAL")
	r5Assign(t, m.handler, root, m.token, m.f.bob, "admin", 200)
	later := workflow.Signal{Operation: "approve", ExpectedVersion: y.Version, StateID: y.StateID, Key: "finance-approval-2", OperatorRef: "test-review"}
	r5eNoSignal(t, m.m9Fixture, y, m.approver, later, 403)
	if sent() != 1 || m.ledgerRows(t, y.ID) != 0 || m.drive(t, y.ID, "WAITING_APPROVAL").Version != y.Version {
		t.Fatal("a revoked approver released a send", sent(), m.ledgerRows(t, y.ID))
	}
	r5Assign(t, m.handler, root, m.token, m.f.bob, "finance_manager", 200)
	m9Decode(t, m5Call(m.handler, m.base+"/instances/"+y.ID+"/signals", m.approver, later))
	y = m.drive(t, y.ID, "SUCCEEDED")
	if sent() != 2 || m.provider.effectCount() != 2 || m.ledger(t, y.ID).Attempts != 1 {
		t.Fatalf("second approved send: %d requests, %d effects", sent(), m.provider.effectCount())
	}
}
