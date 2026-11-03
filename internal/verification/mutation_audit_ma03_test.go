package verification

import (
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/intent"
)

func TestGateWithFewerRolesThanRequiredFails(t *testing.T) {
	gate := requirement("po-gate", intent.CategoryApprovalGate, json.RawMessage(`{"workflow_id":"workflow.order.approve","approver_roles":["admin","owner"]}`))
	set := requirementSet(gate)
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{{ID: "gate", RequirementID: "po-gate", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}}}}
	r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true, roles: []string{"admin"}}), set, spec))
	if r.Outcome != Fail || !hasFinding(r, CodeGateRolesMismatch, "") {
		t.Fatalf("a gate with only the admin role passed for admin+owner: %s %+v", r.Outcome, r.Findings)
	}
}
