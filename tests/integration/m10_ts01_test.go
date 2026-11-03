//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

// TS-01 (independent tools and sandbox red team; OPEN: it needs a design decision and a migration, see docs/migration/M9R_FINDING_STATUS.md and
// ADR100 amendment I2). A tool invocation whose first send ended UNKNOWN waits, with its workflow instance, in RETRY_WAIT. A member who holds
// workflows.cancel cancels the instance: it becomes terminal while the invocation stays UNKNOWN, no reconcile signal is accepted on a terminal
// instance, and the SQL function of the operator refuses the row, so it is stranded for good and the freeze of the sandbox, which waits for every
// outstanding invocation, cannot finish.
//
// This test states the invariant that the fix must give: nothing is left outstanding that nobody can settle. It FAILS today, so it runs only when it
// is asked for (independent review TF-11: the reproduction was a throwaway, and the next owner had nothing to run):
//
//	FRAMEWORK_REPRODUCE_TS01=1 /home/user/bin/it-run m14 <worktree> ts01 TestTS01   (or the same variable with scripts/with-env.mjs)
//
// When TS-01 is fixed the gate is deleted and the test stays.
func TestTS01ACancelledInstanceLeavesNoInvocationThatCannotBeSettled(t *testing.T) {
	if os.Getenv("FRAMEWORK_REPRODUCE_TS01") == "" {
		t.Skip("TS-01 is open and this test fails until it is fixed; set FRAMEWORK_REPRODUCE_TS01=1 to run it")
	}
	m := m10Setup(t, m10Options{allowInternal: true})
	m.provider.setBehavior(func(c m10Call, _ int) string {
		if c.Method == "POST" {
			return "500" // the keyed write ends UNKNOWN: the platform cannot tell whether the provider applied it
		}
		return ""
	})
	x := m.startFlow(t, "native")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x, errs := m10rRun(t, m, x.ID, []string{"RETRY_WAIT", "RECONCILE", "DEAD", "SUCCEEDED"}, 15*time.Second)
	if x.Status != "RETRY_WAIT" || len(errs) != 0 {
		t.Fatalf("premise not reached: status %s, errors %v", x.Status, errs)
	}
	if l := m.ledger(t, x.ID); l.State != "UNKNOWN" {
		t.Fatalf("premise not reached: the ledger is %+v", l)
	}
	after := m.signal(t, x, "cancel", m.token) // the route needs workflows.cancel, which every role but the viewer holds
	if after.Termination != "CANCELLED" {
		t.Fatalf("the cancel did not take: %s %s", after.Status, after.Termination)
	}
	final, errs := m10rRun(t, m, x.ID, []string{"CANCELLED", "TIMED_OUT", "DEAD", "FAILED", "SUCCEEDED", "RECONCILE"}, 15*time.Second)
	if len(errs) != 0 {
		t.Fatalf("driving the cancelled instance: %v", errs)
	}

	var outstanding int
	if err := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.tool_invocations WHERE instance_id=$1 AND state IN ('SENDING','UNKNOWN')`, x.ID).Scan(&outstanding); err != nil {
		t.Fatal(err)
	}
	if outstanding == 0 {
		return // the fix may settle the row at the cancel, or refuse the cancel: both leave nothing stranded
	}
	// Something is outstanding: a person must be able to settle it. The operator signal of the red team's repro:
	signal := workflow.Signal{Operation: "reconcile", ExpectedVersion: final.Version, StateID: final.StateID, Key: "reconcile-" + uuid(t), OperatorRef: "ops-1", Outcome: "NOT_APPLIED"}
	if r := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.approver, signal); r.Code != 200 {
		t.Fatalf("TS-01: %d invocation(s) of a %s instance stay SENDING or UNKNOWN and the reconcile is answered %d %s", outstanding, final.Status, r.Code, strings.TrimSpace(r.Body.String()))
	}
}
