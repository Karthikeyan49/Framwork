# Independent re-verification of the M10 review fixes and the M12 verifier fixes

Reviewer: "reviewer" agent, wave 3 (did not write the fixes; product code is not modified). Branch `review/m13-final`,
subject tree `e6b7877` (M0 to M13 + M10 fixes `46db427` + verifier fixes `a5e6292`). Date 2026-10-19.

STATUS: COMPLETE for the listed items. Verdict: the five M10 fixes and the five verifier fixes HOLD against their
regression tests and against 34 variants of my own; two residuals are recorded (FR-01 LOW, FR-02 LOW) plus five
informational notes. Nothing HIGH or MEDIUM was found in the fixes.

## What was run (all alone, `nice -n 10`; integration tests through the MAIN stack queue wrapper on disposable databases)

| Command | Result | Duration |
|---|---|---|
| `... -tags=integration -run 'TestM10R' ./tests/integration` | 11 PASS (9 `TestM10R01..05*` + `TestM10ReadTool...`, `TestM10ReconcileNotApplied...`) | 84 s |
| `./scripts/go.sh test -count=1 ./internal/promotion ./internal/verification ./internal/risk ./internal/intent ./internal/runtime/workflow ./internal/tools ./internal/policy ./internal/audit ./internal/platform/access ./internal/platform/postgres ./internal/metadata ./internal/compiler` | 12 packages PASS | 17 s |
| own variants (below), integration | PASS, results quoted | 35 s + 12 s |
| own variants, unit (`internal/verification`, `internal/promotion`) | PASS, results quoted | 1 s |
| the two reproductions of Appendix A | both FAIL on this tree (the residuals exist) | 2 s each |

The variant sources lived in untracked `zz_review_*_test.go` files outside the repository while committing; only the two
reproductions of the residuals are kept (Appendix A).

## M10 review fixes (M10R-01 to M10R-05)

| Finding | Fix (where) | Regression tests (PASS) | Own variants and result |
|---|---|---|---|
| M10R-01 blind resend after a failed lookup | durable `unknown_outstanding` (`00050:34-39`), admission in `tool_begin` (`00050:91-109`), `tool_finish` keeps UNKNOWN after any non-success while outstanding (`00050:143-158`), `decideTool` (`tool_step.go:115-150`) | `TestM10R01TransientLookupFailureNeverResendsUnknownWrite`, `TestM10R01DatabaseRefusesSendWhileUnknownOutstanding` | V1 DEDUPE_WINDOW tool with an expired window and a NONE/RECONCILE_FIRST tool, first POST dropped: both end `RECONCILE`, ledger UNKNOWN, exactly 1 POST. V3 LOOKUP_BY_REFERENCE write whose first POST is dropped and whose lookup answers 500 twice: `RECONCILE`, 1 POST, 2 lookups, attempts `[SEND UNKNOWN, LOOKUP UNKNOWN server_error, LOOKUP UNKNOWN server_error]`. V7 a row as the pre-fix binary left it (state FAILED, retryable, `unknown_outstanding` NULL, an earlier UNKNOWN attempt): raw `tool_begin('SEND')` is refused with TL003, i.e. the NULL derivation (`00050:36-39`) keeps old rows safe. DEDUPE_WINDOW inside its window resends once with the same idempotency key (2 POSTs, 1 effect): by design. HELD |
| M10R-02 worker skips the approval gate | gate derived from the pinned plan (`00050:12-22,67`; DSL 2.2.0 pins `tool_contract`, `plan.go:23-27,65-83`, `compiler/workflow_tools.go:105`), request witness (`00050:25-27,80`), `guard_worker_transition` (`00050:205-216,233`), `guard_tool_receipt` (`00050:219-231,234`) | `TestM10R02WorkerCannotSkipApprovalInsideOwnLease` (jump to the tool state refused, `needs_approval=false` ignored -> TL001, forged receipt refused, forged witness 22023) | V5 below. The worker still sends the flag (`tool_step.go:278`); the database ignores it (`00050:50`). Effect-bearing steps HELD; FR-01 is the residual for the instance row itself |
| M10R-03 retry budget bypassed | `decideTool` returns a retryable failure for every fresh non-success so `recordFailure` counts it; ledger cap `MaxLedgerAttempts`; TL004 on exhaustion (`00050:112`) | `TestM10R03RetryWaitHonoursMaxAttempts`, `TestM10R03UnknownKeyedResendsAreBoundedThenReconcile` | not extended: the provider fixture can inject 429/400/reject500 on GET and drop/500/slow on POST only, so a dropped READ is not expressible (my V4 ran a READ with no fault and proves nothing; recorded as NOT RUN) |
| M10R-04 rejected lookup strands UNKNOWN in DEAD | `tool_finish` keeps UNKNOWN (`00050:156`); catalog read order | `TestM10R04RejectedLookupKeepsUnknownWriteReconcilable`, `TestM10R04CatalogChangeWhileUnknownParksInReconcile` | V3 (above) is the same property with 500s. HELD |
| M10R-05 404 trusted while the original may be in flight | absence is proven only when the lookup started after the last send's start + timeout + declared `settle_ms` (`00050:146-155`; `tools/catalog.go:61-70`) | `TestM10R05LookupWaitsForDeclaredSettleInterval`, `TestM10R05EarlyNotFoundCannotProveAbsence` | by reading: `settle_ms` 0 or absent never proves absence (always RECONCILE); a provider that commits later than timeout + settle still yields a duplicate (a declaration, not a proof). HELD within that stated contract |

### FR-01 LOW - the worker can still finish, fail or cancel its leased instance without a transition

`runtime.guard_worker_transition` (`00050:205-216`) returns immediately when `state_id` does not change
(`NEW.state_id IS NOT DISTINCT FROM OLD.state_id`), and for every `session_user` other than `framework_worker`
(`00050:208`). The worker's column grant (`00020:38`) also covers `status`, `termination`, `failure`, `attempt`,
`entered_at`, `due_at`, `version`.
Executed (Appendix A `TestZZReproFR01...`): inside the exact lease of an instance that sits at `state.start` (its
approval state never reached), raw SQL as `framework_worker` `UPDATE ... SET status='SUCCEEDED'` and `status='FAILED',
failure='permanent'` and `termination='CANCELLED'`: each `rows=1`, accepted. (`deadline` is not granted: 42501.)
Reading only (not executed, no timer fixture): the trigger never reads `due_at`, so for a `timer` state an `always`
transition is not tied to the timer being due.
Impact: LOW. No external effect can follow (`tool_begin` demands `state_id = step` and the plan-derived approval) and
ADR024 accepts fabrication inside a lease, but ADR036 section 7 and the fix's own comment ("bound to the pinned
plan") read as if the instance row were confined; the recorded workflow outcome of an approval-gated flow can still be
forged by a compromised worker, and the API role keeps its unguarded `UPDATE` grant (`00020:38`; N-03 of the
foundation analysis stays PARTIAL). Fix direction: reject `status` in the terminal set and `termination` changes from the
worker unless the transition is plan-derived, and tie timer exits to `due_at`.

### Informational (M10 fixes)

- FR-I1 the approval requirement is derived from `workflow_bindings.plan_body`, and `framework_app` holds INSERT on that
  table (`00020:24`) with only a sha256 equality check (foundation gap V1): the derivation defeats a compromised
  WORKER (the stated threat), not a compromised API process that stores its own binding.
- FR-I2 `tool_approval_gated` over-approximates for legacy 2.1.0 plans (a tool behind any approval edge needs approval
  evidence); honest legacy executions are unaffected because the compiler already forced the gate.
- FR-I3 `control.tool_begin` keeps the `needs_approval` parameter for signature compatibility; a future caller may
  assume it is honoured. The comment at `00050:50` says it is ignored; the Go call site (`tool_step.go:278`) still computes it.

## Verifier fixes (F2, F3, F4, F5, F7 of the M12 verifier review; commit `a5e6292`)

Regression tests (PASS): `TestRejectedApprovalThatCompletesFails`, `TestUpperCapOnTheApprovalRouteFails`,
`TestUnpinnedFieldsAreMinedFromTheRoute`, `TestPlanOnlyWorkflowChangeInheritsTheToolsLevel` and the rest of
`internal/verification` and `internal/promotion` (unit run above).

| Finding | Variants of my own (unit level, `Verify` on compiled programs) | Result |
|---|---|---|
| F2 rejected edge completes | threshold requirement with `rejected -> complete` in USD, JPY (0 decimals) and KWD (3 decimals); `rejected` into a SECOND approval that completes | all FAIL (`APPROVAL_MISMATCH`): HELD. The mirror (`approved -> failed` and `rejected -> failed`) still PASSes: fail-closed, unchanged from the review |
| F3 upper cap | cap `<= 1,000,000` USD, `<= 5,000,000` JPY, `<= 1,000,000` KWD | all FAIL: HELD |
| F3 band between probes | exemption band `[600,900]`, `[2000,3000]`, `[20000,30000]`, `[1500000,2500000]` USD | all PASS: FR-02 |
| F4 optional fields | boolean exemption (`rush == true` skips approval); integer band exemption (`qty` in [10,20]); integer exemption (`qty > 1000`); `coalesce(note,"x") != "x"` skips unset notes | FAIL, BLOCKED, BLOCKED, FAIL: HELD |
| F4 two fields | skip only when `rush == true` AND `note == "vip"` | PASS: the documented limit ("two-field interactions beyond one extra value per run are not explored", ADR038 amendment 9) |
| F5 over-approval | `amount > 500 OR rush == true`; `amount > 500 OR qty >= 5` | FAIL, BLOCKED: HELD |
| F7 plan-only around a tool | WRITE tool, PLAN_ONLY -> level HIGH; READ tool -> MEDIUM; WRITE tool whose definition is missing -> CRITICAL and blocked; `[PLAN_ONLY, PLAN_ONLY]` -> HIGH; WORKFLOW_GRAPH around WRITE + READ -> HIGH with both inherited | HELD |

### FR-02 LOW - the threshold field's own constants are not probed: an exemption band still passes

The F3 fix adds decade probes (1e3 to 1e18, both signs) and 10x/1000x multiples of the threshold. A route that exempts a
band that contains no probe (here orders from 600 to 900 USD, just above the threshold) verifies as PASS with a human
case at 501.00, which is exactly the hole F3 described moved to a narrower place. ADR038 amendment 9 states the floor is
"bounded, not a proof" and that money fields other than the threshold are not mined, but the constants compared against
the THRESHOLD field itself (here 600 and 900) are the cheapest facts to mine and are not. Executed: Appendix A
`TestZZReproFR02...` FAILs (PASS reported). Impact LOW: the independent acceptance author is expected to cover special
regions and a band close to the threshold is the first thing a careful author would try, but a PASS stamp is what M13 later
treats as evidence. Fix direction: add probes at every money literal the route compares the threshold field against, at
+/- one minor unit and at each midpoint between sorted literals.

### Informational (verifier fixes)

- FR-I4 integer-field exemptions end as BLOCKED rather than FAIL in two variants (outcome is still non-PASS, so nothing
  stages); the finding codes are `APPROVAL_MISMATCH`.
- FR-I5 F6 (candidate bytes malleable inside `Source.JSON`, LOW) and F8 (documentation gaps) were not part of the fix
  commit and were not re-verified.

## Not covered

- A dropped READ (not expressible with the provider fixture), timers on the worker path (no timer flow exists in the
  M10 fixtures), plans stored by the pre-fix binary with DSL 2.1.0 (executed only through a hand-built ledger row), a
  provider that commits after `timeout + settle_ms`, and the combination of M10 fixes with M11 sandbox ceilings.
- F6 and F8 of the verifier review; the independent review of the M11 and M12 API fixes that other agents are writing.

## Appendix A - reproductions of the residuals

Copy the first block to `tests/integration/zz_review_fr01_test.go` and the second to
`internal/verification/zz_review_fr02_test.go`, run, delete. Both FAIL while the residual exists (ASCII only).

    /home/user/bin/dbrun nice -n 10 node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -run 'TestZZReproFR01' ./tests/integration
    nice -n 10 ./scripts/go.sh test -count=1 -run 'TestZZReproFR02' ./internal/verification

```go
//go:build integration

package integration

import "testing"

// FR-01: inside its exact lease a worker can still finish, fail or cancel an instance without any
// transition; guard_worker_transition only looks at state_id changes.
func TestZZReproFR01WorkerStatusOnlyUpdate(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "native") // state.start -> state.approval -> state.tool -> state.complete
	lease := m10rLease(t, m, x.ID)
	for _, set := range []string{"status='SUCCEEDED'", "status='FAILED',failure='permanent'", "termination='CANCELLED'"} {
		out, err := m10rInLease(m, lease, `UPDATE runtime.workflow_instances SET `+set+` WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID)
		if err == nil {
			t.Errorf("FR-01: the worker applied %q to its leased instance without a transition and without approval (%s)", set, out)
		}
		_, _ = m.f.operator.Exec(`UPDATE runtime.workflow_instances SET status='READY',termination='',failure='' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID)
	}
}
```

```go
package verification

import (
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// FR-02: an exemption band for mid-size orders lies between the derived probes
// (1e3..1e18 and 10x/1000x of the threshold) and passes verification.
func TestZZReproFR02ExemptionBandPasses(t *testing.T) {
	money := func(a string) metadata.Expression {
		return metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + a + `","currency":"USD"}`)}
	}
	amount := metadata.Expression{Op: "field", FieldRef: "field.order.amount"}
	op := func(name string, args ...metadata.Expression) metadata.Expression { return metadata.Expression{Op: name, Args: args} }
	doc := orderDocument(orderOptions{})
	for i := range doc.Definitions {
		if wf := doc.Definitions[i].Workflow; wf != nil {
			for j := range wf.States {
				if wf.States[j].ID == "state.start" {
					// approval when amount > 500 AND NOT (600 <= amount <= 900)
					cond := op("and", op("gt", amount, money("500")), op("not", op("and", op("gte", amount, money("600")), op("lte", amount, money("900")))))
					wf.States[j].Transitions[0].Condition = &cond
				}
			}
		}
	}
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{
		{ID: "above-threshold", RequirementID: "po-approval", Given: amountGiven("501.00", nil), Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}}}}
	r := mustVerify(t, input(compileDocument(t, doc, compiler.PlatformCatalog()), set, spec))
	if r.Outcome == Pass {
		t.Errorf("FR-02: orders between 600 and 900 USD skip the admin approval and the verifier says PASS")
	}
}
```
