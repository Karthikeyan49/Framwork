package access

import (
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/policy"
)

// The stage gate used to answer true unconditionally (gap N-06). It now opens only for the request's own allowed
// decision, for the same tenant and actor, and for the stage action.
func TestReleaseGateOpensOnlyForTheRequestsOwnDecision(t *testing.T) {
	allowed := policy.Decision{Allow: true, ID: "decision-1", Version: policy.Version}
	denied := policy.Decision{Allow: false, ID: "decision-2", Version: policy.Version}
	open := releaseGate{decision: allowed, tenantID: "tenant-a", actorID: "actor-a"}
	if !open.AuthorizeRelease("tenant-a", "actor-a", "stage", "hash") {
		t.Fatal("the request's own allowed decision did not open the gate")
	}
	for name, tc := range map[string]struct {
		gate                      releaseGate
		tenantID, actorID, action string
	}{
		"zero gate":         {releaseGate{}, "tenant-a", "actor-a", "stage"},
		"zero gate, no ids": {releaseGate{}, "", "", "stage"},
		"denied decision":   {releaseGate{decision: denied, tenantID: "tenant-a", actorID: "actor-a"}, "tenant-a", "actor-a", "stage"},
		"another tenant":    {open, "tenant-b", "actor-a", "stage"},
		"another actor":     {open, "tenant-a", "actor-b", "stage"},
		"another action":    {open, "tenant-a", "actor-a", "activate"},
		"empty action":      {open, "tenant-a", "actor-a", ""},
		"gate without ids":  {releaseGate{decision: allowed}, "", "", "stage"},
	} {
		if tc.gate.AuthorizeRelease(tc.tenantID, tc.actorID, tc.action, "hash") {
			t.Errorf("%s: the gate opened", name)
		}
	}
}
