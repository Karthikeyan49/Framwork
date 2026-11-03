# Mutation audit of the M12/M13/M14 lane

Auditor: independent mutation auditor (brief 65). Target pin: `11a4ee40a12e0301472b275203ac10d8347eaad9` (branch `integration/m9r-m14`, code head `585499d`).
Branch: `review/mutation-audit`. Product code is changed only as one temporary mutation at a time (reverted with `git checkout -- <path>` after every run); this file is the only thing committed.

Method. For every mutant: one exact text replacement in one product file, then the narrowest tests that should fail (`/home/user/bin/gt test -count=1 -run '<regex>' ./<pkg>`), widened to the whole package when the narrow run passed; SQL mutants through `/home/user/bin/it-run fix` with the M13 test group. Results: KILLED (name of the first failing test), SURVIVED (everything listed passed), EQUIVALENT (argued below). A mutant that did not compile or whose edit did not apply is discarded and listed.
Unit mutants are measured against the unit tests of their package only unless the row says otherwise: a unit survivor in a function that the integration suite covers is reported as SURVIVED with the note "unit-only" and is NOT claimed to be unprotected. Every survivor that concerns an authority, tenant, integrity or exactly-once invariant is analysed in Findings with a killing test that was run on the mutant (fails) and on the original (passes).
Stack lock caveat found during the run: `it-run` releases the stack lock between its reset phase and its test phase, so another agent using the same stack can rebuild it in between. Every SQL mutant is therefore run with a canary test (`TestZZCanary`, in the same `go test` invocation, reading `pg_get_functiondef`) that fails when the stack does not run the mutant. A first S01 run without the canary reported SURVIVED against a stack that had been rebuilt from the original migrations; it was discarded and redone.

## Progress

Status at this commit: PARTIAL, in progress. The run was cut off once by an account limit and resumed; everything below is regenerated from the run log.

- Target 1 (M13): unit mutants of `internal/policy` (U), `internal/promotion` (D, T, R, B) done; SQL mutant S01 done (MA-01); further M13 SQL mutants pending.
- Target 2 (M12 verifier and oracle): done (V, O); findings MA-02 (promotion, runtime components) and MA-03 (verifier, sameRoles) have verified killing tests.
- Target 3 (M14): gateway/repair/patch/schema/service unit mutants (A, G, P, K, SV, SC) in progress; SQL mutants of migration 00089 (X) pending.
- Target 4 (front door, traffic): body budget (F) and traffic gate (TR) in progress.
- Target 5: not reached.

## Mutation score per target

KILLED / SURVIVED / EQUIVALENT after the reclassification below; the score is killed / (killed + survived). "Survived" means: survived every test listed in the "tests run" column of its row, and nothing else was run.

| key | target | killed | survived | equivalent | score |
|---|---|---|---|---|---|
| 1a | Target 1 - M13 policy (Holds / NativeHolds / Decide / permits / RoleStore) | 12 | 23 | 1 | 34% (12/35) |
| 1b | Target 1 - M13 promotion package (counting, floors, staleness, runtime components, restore gate) | 36 | 19 | 3 | 65% (36/55) |
| 1c | Target 1 - M13 SQL ports (migrations 00084-00092), through it-run fix | 0 | 1 | 0 | 0% (0/1) |
| 2 | Target 2 - M12 verifier and oracle | 36 | 10 | 5 | 78% (36/46) |
| 3a | Target 3 - M14 gateway, repair loop, patch, kinds, schema, service helpers (internal/ai) | 17 | 10 | 3 | 63% (17/27) |
| 3b | Target 3 - M14 route and SQL (internal/platform/access/ai.go, migration 00089) | 0 | 0 | 0 | not reached |
| 4 | Target 4 - platform front door and traffic | 18 | 9 | 1 | 67% (18/27) |
| 5 | Target 5 - M10 tools gateway and M11 sandbox | 7 | 5 | 1 | 58% (7/12) |
| all | all targets reached | 126 | 77 | 14 | 62% (126/203) |

Discarded (did not compile or the edit did not apply, redone or dropped): B09, A19, G02, G03, AC17, AC18, AC21, AC25.

## Findings

### MA-01 (MEDIUM) - the membership lock of `activate_release` is not tested

Mutant S01 (`db/migrations/00088_m13_review_fixes.sql:79`, the live definition of `control.activate_release`): delete ` FOR SHARE OF m` from the statement that "holds the approvers' memberships until commit, so a concurrent demotion either waits or fails this transaction" (M13R-03). Result: all 60 tests of `TestM13*|TestF05*` pass (`/home/user/logs/ma-s01b.log`, with the canary proving the mutant was live). What is unprotected: an approver demoted (or removed, or disabled) between the quorum recount and the pointer move of the activating transaction still counts, so a candidate can become active with approvals that no longer satisfy the quorum or the policy. `tests/integration/m13_api_test.go:741` states that "the concurrent demotion tests cover" this lock; no such test exists (`grep` for memberships in `tests/integration/m13*_test.go` finds only sequential `UPDATE`s).
Killing test, verified: it fails on the mutant (`demotion of an approver while the activation is open: accepted, want SQLSTATE [55P03]`, `/home/user/logs/ma-s01b.log:147-150`) and passes on the original (`/home/user/logs/ma-s01-kill-on-mutant3.log`, `demotion outcome: ERROR: canceling statement due to lock timeout (SQLSTATE 55P03)`). It opens the activation transaction through the port, leaves it open, and tries the operator demotion with `lock_timeout='300ms'`:

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

func TestActivationHoldsTheApproversMemberships(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()
	m13Code(t, "approve", e.httpApprove(e.dave.token, tmpl.hash), 201)
	err := postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, e.erin.subject, e.f.tenantA); err != nil {
			return err
		}
		var generation int64
		if err := q.QueryRowContext(ctx, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, tmpl.hash, "", 0).Scan(&generation); err != nil || generation != 1 {
			t.Fatal("activation", generation, err)
		}
		// The activation transaction is still open: dave's membership row must be locked against the demotion.
		demotion, err := e.f.operator.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer demotion.Rollback()
		if _, err = demotion.ExecContext(ctx, `SET LOCAL lock_timeout='300ms'`); err != nil {
			return err
		}
		_, err = demotion.ExecContext(ctx, `UPDATE iam.memberships SET role='viewer' WHERE tenant_id=$1 AND user_id=$2`, e.f.tenantA, e.dave.id)
		m13Refused(t, "demotion of an approver while the activation is open", err, "55P03")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
```


### MA-02 (MEDIUM) - nothing tests that the recorded runtime components carry the live versions (12 surviving mutants)

`promotion.runtimeComponents` (`internal/promotion/dependencies.go:130-136`) builds the document whose hash makes older candidates stale (RUNTIME_CHANGED, ADR102). Mutants R01, R02, R04 to R13 replace the version of one component by the empty string (the version constant stays referenced, `[:0]`): StorageContract, Evolution, the tool DSL version, RuleDSL, ToolCatalog, Diff, RiskPolicies, **Verifier** (`verification.VerifierVersion`, line 134), AcceptanceVocab, RequirementPolicy, RequirementSetSpec, AcceptanceSpec. All of them pass the whole `internal/promotion` package: `TestRuntimeComponentsHashCoversEveryComponent` (`staleness_test.go:152-170`) edits the document AFTER `runtimeComponents` built it, so it cannot see a constructor that stopped reading a version; only `SupportedIR` (R03) is caught. What is unprotected: bumping `VerifierVersion` (or any of the twelve versions) would no longer make a candidate verified by the older procedure stale, and the candidate would be activated on evidence from a verifier the binary no longer runs.
Killing test, verified (passes on the original; fails on all 13 mutants R01 to R13, run in an untracked copy of the tree, `kill_results.tsv`: every `K-R*` row KILLED by this test):

```go
package promotion

import (
	"reflect"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

func TestRuntimeComponentsRecordTheLiveVersions(t *testing.T) {
	want := RuntimeComponentsDoc{
		StorageContract: compiler.StorageContractVersion, Evolution: compiler.EvolutionVersion, SupportedIR: compiler.SupportedIRVersions(),
		WorkflowDSL: []string{workflow.Version, workflow.TenantRoleVersion, workflow.ToolDSLVersion}, RuleDSL: rules.Version, ToolCatalog: tools.CatalogVersion,
		Diff: compiler.DiffVersion, RiskPolicies: risk.Versions(), Verifier: verification.VerifierVersion, AcceptanceVocab: intent.VocabularyVersion,
		RequirementPolicy: intent.RequirementPolicyVersion, RequirementSetSpec: intent.RequirementSetVersion, AcceptanceSpec: intent.AcceptanceSpecVersion,
	}
	got, hash := RuntimeComponents()
	if !reflect.DeepEqual(got, want) || hash != hashComponents(want) {
		t.Fatalf("the recorded components are not the live versions:\n got %+v\nwant %+v", got, want)
	}
}
```

### MA-03 (MEDIUM) - a gate whose approval states name only some of the required roles passes (`sameRoles`, mutant V23)

`internal/verification/verifier.go:631-643`: `sameRoles` compares the sorted role lists. Deleting `len(x) == len(y) &&` (V23) leaves every `internal/verification` test green, yet the comparison then accepts a prefix of the required list (and indexes past the end of `y` when the plan names more roles). With `approver_roles: ["admin","owner"]` required, a plan whose approval state lists only `["admin"]` passes the approval-gate requirement: the weaker gate is verified as the required one. The existing gate tests use a wrong role (`member`) and an equal two-role list, never a subset.
Killing test, verified (passes on the original, fails on V23: `a gate with only the admin role passed for admin+owner: PASS`):

```go
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
```

### MA-04 (LOW) - the loop starts a provider call whose worst case the budget does not cover (mutant A12)

`internal/ai/repair.go:199`: `if remaining < estimateTokens(len(reqJSON))+int64(outTokens)` refuses the next attempt before any provider call when the tokens left cannot cover the request plus the output the call may produce. Weakening it to `remaining < estimateTokens(len(reqJSON))` passes the whole `internal/ai` package: the only boundary test (`TestRunPatchBudgets`, "a call larger than the remaining budget stops the loop", `gateway_test.go:303-317`) leaves 100 tokens of slack, so it cannot tell the two apart. What is unprotected: one attempt can overrun the reservation that admission made, by up to the output ceiling (charged afterwards, capped at `2*outTokens+1024`), so the daily budget accounting is exceeded.
Killing test, verified (passes on the original; fails on A12: `status ... 1 provider calls with a budget one token short of the reservation`):

```go
package ai

import (
	"encoding/json"
	"testing"
)

func TestNoCallWithoutTheOutputReservation(t *testing.T) {
	r := newRig(t, Options{})
	r.prov.Push(ScriptStep{Payload: r.broken("G0"), Usage: usage(1200, 300)})
	b := okBudget()
	probe := r.patchInput(b)
	req, _ := r.g.(*gateway).request(runSpec{task: TaskApplicationPatch, context: probe.Context, schema: mustSchema(t, r.doc)}, DefaultTemplates(), 1, b.MaxOutputTokens, nil)
	raw, _ := json.Marshal(req)
	b.TokensRemaining = estimateTokens(len(raw)) + int64(b.MaxOutputTokens) - 1 // one token short of what one attempt may cost
	run := r.run(b)
	if run.Status != RunBudgetExceeded || r.prov.Calls() != 0 {
		t.Fatalf("status %s, %d provider calls with a budget one token short of the reservation", run.Status, r.prov.Calls())
	}
}
```

### MA-05 (LOW) - a nearly full admission bucket can refill past its burst (mutant TR06)

`internal/platform/traffic/gate.go:58`: `b.credit = min(ceiling, b.credit+int64(elapsed)*int64(rate))`. Without the `min` ceiling every traffic test still passes, and a bucket that had 19 of 20 tokens and refills 15 holds 34: the burst that `Limits.Validate` bounds to twice the rate is exceeded by up to 2x between two requests. Killing test, verified (passes on the original; fails on TR06: more than 20 requests admitted (34 by the arithmetic)):

```go
package traffic

import (
	"context"
	"testing"
	"time"
)

func TestRefillNeverExceedsTheBurst(t *testing.T) {
	now := time.Unix(1000, 0)
	g, err := New(Limits{PeerRate: 10, PeerBurst: 20, GlobalRate: 100, GlobalBurst: 200}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx, remote := context.Background(), "192.0.2.1:4000"
	if !g.Peer(ctx, remote).Allowed() { // 19 tokens left
		t.Fatal("the first request was refused")
	}
	now = now.Add(1500 * time.Millisecond) // +15 tokens: 34 without the ceiling, 20 with it
	allowed := 0
	for i := 0; i < 40; i++ {
		if g.Peer(ctx, remote).Allowed() {
			allowed++
		}
	}
	if allowed != 20 {
		t.Fatalf("%d requests were admitted in one burst, the burst is 20", allowed)
	}
}
```

## Equivalent mutants

- D25 (`internal/promotion/decisions.go:551`): the dedupe key `lower(actor)+actor` is injective in the actor id, so `CountEligible` counts the same set.
- U002 (`internal/policy/holds.go:16`): `permits` and `permitsSelf` both return false for an empty actor, so the `member != ""` guard of `NativeHolds` is redundant.

## NOT RUN

- Integration cover of the unit-surviving mutants of `internal/policy` (U001-U003, U009-U022, U027, U028, U031, U033-U036) was NOT RUN end to end; they are reported as unit-only survivors.
- Targets 2 to 5 are listed as not reached in the score table until their rows appear.

## Mutant table

| id | file:line | mutation | tests run | result | first killing test / note |
|---|---|---|---|---|---|
| U001 | internal/policy/holds.go:16 | NativeHolds: drop the tenantID guard | go test ./internal/policy (whole package) | SURVIVED | LOW: differs only for identity.session.read, which promote.go:261-263 never asks; unit-only |
| U002 | internal/policy/holds.go:16 | NativeHolds: drop the member guard | go test ./internal/policy (whole package) | EQUIVALENT | permits and permitsSelf both require a non-empty actor, so the member guard is redundant |
| U003 | internal/policy/holds.go:16 | NativeHolds: drop the permitsSelf branch (no longer equal to Native.Decide) | go test ./internal/policy (whole package) | SURVIVED | LOW: differs only for identity.session.read, which promote.go:261-263 never asks; unit-only |
| U004 | internal/policy/holds.go:48 | Defined.holds: bootstrap (legacy) roles hold nothing | go test ./internal/policy (whole package) | KILLED | TestDefinedHoldsFollowsTheGrants |
| U005 | internal/policy/holds.go:50 | Defined.holds: drop validResource | go test ./internal/policy (whole package) | KILLED | TestDefinedHoldsFollowsTheGrants |
| U006 | internal/policy/holds.go:50 | Defined.holds: drop the resource tenant equality | go test ./internal/policy (whole package) | KILLED | TestDefinedHoldsFollowsTheGrants |
| U007 | internal/policy/holds.go:58 | Defined.holds: a DENY after an ALLOW no longer wins (order dependent) | go test ./internal/policy (whole package) | KILLED | TestDefinedHoldsFollowsTheGrants |
| U008 | internal/policy/holds.go:55 | Defined.holds: drop the action membership test | go test ./internal/policy (whole package) | KILLED | TestDefinedHoldsFollowsTheGrants |
| U009 | internal/policy/holds.go:26 | RoleStore.Holds: drop the resource tenant equality (needs DB to observe) | go test ./internal/policy (whole package) | SURVIVED | needs a database to observe; TestM13R03HoldsMatchesDecide uses the same tenant only; the caller always passes the access tenant |
| U010 | internal/policy/definition.go:239 | selectorMatches: entity selector ignored | go test ./internal/policy (whole package) | SURVIVED |  |
| U011 | internal/policy/definition.go:239 | selectorMatches: field selector ignored | go test ./internal/policy (whole package) | SURVIVED |  |
| U012 | internal/policy/definition.go:242 | validSubject: the subject need not hold the role the definition names | go test ./internal/policy (whole package) | SURVIVED |  |
| U013 | internal/policy/definition.go:242 | validSubject: user need not be the access actor | go test ./internal/policy (whole package) | SURVIVED |  |
| U014 | internal/policy/definition.go:242 | validSubject: resource tenant may differ from the access tenant | go test ./internal/policy (whole package) | SURVIVED |  |
| U015 | internal/policy/definition.go:282 | Defined.Decide: drop validSubject | go test ./internal/policy (whole package) | SURVIVED |  |
| U016 | internal/policy/definition.go:301 | Defined.Decide: DENY after ALLOW does not win | go test ./internal/policy (whole package) | SURVIVED |  |
| U017 | internal/policy/definition.go:290 | Defined.Decide: drop validResource | go test ./internal/policy (whole package) | SURVIVED |  |
| U018 | internal/policy/definition.go:360 | Defined.DataScope: no scope at all no longer denies | go test ./internal/policy (whole package) | SURVIVED |  |
| U019 | internal/policy/definition.go:353 | Defined.DataScope: DENY only wins if it comes before every ALLOW | go test ./internal/policy (whole package) | SURVIVED |  |
| U020 | internal/policy/definition.go:334 | Defined.DataScope: drop validSubject | go test ./internal/policy (whole package) | SURVIVED |  |
| U021 | internal/policy/policy.go:52 | Native.Decide: a cancelled context may still allow | go test ./internal/policy (whole package) | SURVIVED |  |
| U022 | internal/policy/policy.go:51 | Native.Decide: user id need not equal the access actor | go test ./internal/policy (whole package) | SURVIVED |  |
| U023 | internal/policy/policy.go:83 | permits: members (not viewers) hold the release authorities | go test ./internal/policy (whole package) | KILLED | TestNativeHolds |
| U024 | internal/policy/policy.go:63 | permits: drop tenant equality | go test ./internal/policy (whole package) | KILLED | TestNativeHolds |
| U025 | internal/policy/policy.go:67 | permits: unknown roles are treated as built-in | go test ./internal/policy (whole package) | KILLED | TestPolicyMatrix |
| U026 | internal/policy/policy.go:97 | permits: viewers may invoke tools | go test ./internal/policy (whole package) | KILLED | TestToolInvokeCapability |
| U027 | internal/policy/policy.go:105 | permits: viewers may reconcile commands | go test ./internal/policy (whole package) | SURVIVED |  |
| U028 | internal/policy/policy.go:114 | permits: members may reconcile workflows | go test ./internal/policy (whole package) | SURVIVED |  |
| U029 | internal/policy/policy.go:154 | permits: members share/assign records | go test ./internal/policy (whole package) | KILLED | TestRecordAccessAdministrationCapabilities |
| U030 | internal/policy/policy.go:160 | permits: members write metadata | go test ./internal/policy (whole package) | KILLED | TestPolicyMatrix |
| U031 | internal/policy/policy.go:75 | permits: admins write and assign roles | go test ./internal/policy (whole package) | SURVIVED |  |
| U032 | internal/policy/policy.go:165 | permits: profile.read of another user | go test ./internal/policy (whole package) | KILLED | TestPolicyMatrix |
| U033 | internal/policy/store.go:133 | compileVersion: an inactive role head still compiles | go test ./internal/policy (whole package) | SURVIVED |  |
| U034 | internal/policy/store.go:137 | compileVersion: a bootstrap version may carry grants | go test ./internal/policy (whole package) | SURVIVED |  |
| U035 | internal/policy/store.go:148 | RoleStore.Load: several roles use the first one | go test ./internal/policy (whole package) | SURVIVED |  |
| U036 | internal/policy/store.go:172 | LoadWorker: worker actor binding not compared | go test ./internal/policy (whole package) | SURVIVED |  |
| D01 | internal/promotion/decisions.go:472 | RequiredApprovals: an unknown level fails open to quorum 1 | go test ./internal/promotion (whole package) | KILLED | TestDatabaseFloorsMirrorAndNeverExceedThePolicy |
| D02 | internal/promotion/decisions.go:485 | FloorRoles HIGH: the acceptance author may approve | go test ./internal/promotion (whole package) | KILLED | TestHeaderAndDatabaseFloorsOfABuiltCandidate |
| D03 | internal/promotion/decisions.go:488 | FloorRoles CRITICAL/unknown: the sandbox freezer may approve | go test ./internal/promotion (whole package) | KILLED | TestDatabaseFloorsMirrorAndNeverExceedThePolicy |
| D04 | internal/promotion/decisions.go:537 | FloorExclusion: party ids compared case-sensitively | go test ./internal/promotion (whole package) | KILLED | TestDatabaseFloorsMirrorAndNeverExceedThePolicy |
| D05 | internal/promotion/decisions.go:537 | FloorExclusion: an unknown party no longer excludes everyone | go test ./internal/promotion (whole package) | KILLED | TestDatabaseFloorsMirrorAndNeverExceedThePolicy |
| D06 | internal/promotion/decisions.go:531 | FloorExclusion: only the empty actor is invalid | go test ./internal/promotion (whole package) | KILLED | TestDatabaseFloorsMirrorAndNeverExceedThePolicy |
| D07 | internal/promotion/decisions.go:459 | DeriveStatus: required 0 is APPROVED | go test ./internal/promotion (whole package) | KILLED | TestDeriveStatusPrecedence |
| D08 | internal/promotion/decisions.go:455 | DeriveStatus: SUPERSEDED outranks ACTIVE | go test ./internal/promotion (whole package) | KILLED | TestDeriveStatusPrecedence |
| D09 | internal/promotion/decisions.go:186 | ValidNote: boundary >= 280 | go test ./internal/promotion (whole package) | KILLED | TestRejectReasonsAndNotesAreClosedAndBounded |
| D10 | internal/promotion/decisions.go:190 | ValidNote: invisible marks accepted | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | invisible marks: the SQL port (00088:45) repeats the check and the HTTP route answers 400 either way; NOT RUN end to end |
| D11 | internal/promotion/decisions.go:190 | ValidNote: line/paragraph separators accepted | go test ./internal/promotion (whole package) | KILLED | TestRejectReasonsAndNotesAreClosedAndBounded |
| D12 | internal/promotion/decisions.go:190 | ValidNote: bidi embeddings/overrides accepted | go test ./internal/promotion (whole package) | KILLED | TestRejectReasonsAndNotesAreClosedAndBounded |
| D13 | internal/promotion/decisions.go:205 | activationArgs: first release at any generation | go test ./internal/promotion (whole package) | KILLED | TestArgumentShapesAreCheckedBeforeSQL |
| D14 | internal/promotion/decisions.go:207 | activationArgs: base with generation 0 | go test ./internal/promotion (whole package) | KILLED | TestArgumentShapesAreCheckedBeforeSQL |
| D15 | internal/promotion/decisions.go:207 | activationArgs: generation == MaxGeneration | go test ./internal/promotion (whole package) | KILLED | TestArgumentShapesAreCheckedBeforeSQL |
| D16 | internal/promotion/decisions.go:213 | rollbackArgs: target == current | go test ./internal/promotion (whole package) | KILLED | TestArgumentShapesAreCheckedBeforeSQL |
| D17 | internal/promotion/decisions.go:216 | rollbackArgs: a recovery plan is a pointer move | go test ./internal/promotion (whole package) | KILLED | TestArgumentShapesAreCheckedBeforeSQL |
| D18 | internal/promotion/decisions.go:274 | Acknowledged: recovery_required ignored | go test ./internal/promotion (whole package) | KILLED | TestArgumentShapesAreCheckedBeforeSQL |
| D19 | internal/promotion/decisions.go:153 | classify: 42501 mapped to conflict | go test ./internal/promotion (whole package) | KILLED | TestPortErrorsAreTypedWithoutHidingTheSQLState |
| D20 | internal/promotion/decisions.go:414 | WorseRollbackClass: returns the better class | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED |  |
| D21 | internal/promotion/decisions.go:424 | rollbackRank: external effects rank below irreversible data | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED |  |
| D22 | internal/promotion/decisions.go:102 | MaxRollbackSpan 32 -> 33 | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | MaxRollbackSpan boundary; only the integration test TestM13R02* could see it; NOT RUN |
| D23 | internal/promotion/decisions.go:92 | MaxNoteRunes 280 -> 281 (database says 280) | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | Go bound 281 against the SQL bound 280: the unit test uses the constant itself |
| D24 | internal/promotion/decisions.go:561 | RollbackClassNeedsPlan: unknown or empty class needs no plan | go test ./internal/promotion (whole package) | KILLED | TestRollbackClassGate |
| D25 | internal/promotion/decisions.go:551 | CountEligible: key includes the case-sensitive id (neutral) | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | EQUIVALENT | the key seen[lower(actor)+actor] is injective in the actor id, so the distinct count is unchanged |
| T01 | internal/promotion/staleness.go:82 | Staleness: ABA generation advance ignored | go test ./internal/promotion (whole package) | KILLED | TestSecondReleaseBindsAndVerifiesAgainstItsBase |
| T02 | internal/promotion/staleness.go:111 | Staleness: evidence hash not compared | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T03 | internal/promotion/staleness.go:111 | Staleness: unfrozen evidence accepted | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T04 | internal/promotion/staleness.go:108 | Staleness: unchanged risk level counts as escalated | go test ./internal/promotion (whole package) | KILLED | TestSecondReleaseBindsAndVerifiesAgainstItsBase |
| T05 | internal/promotion/staleness.go:108 | Staleness: a blocked assessment is not an escalation | go test ./internal/promotion (whole package) | EQUIVALENT | assessment.Blocked cannot become true in Staleness: the blocking rules read facts a built candidate cannot have (release_risk_v1.go:58-60) and only the BREAKING storage row blocks by volume (release_risk_v1.go:84), which Restore refuses (bundle2.go:541) |
| T06 | internal/promotion/staleness.go:126 | Staleness: empty components hashes match | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T07 | internal/promotion/staleness.go:92 | Staleness: nil tool resolver treated as OK | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T08 | internal/promotion/staleness.go:102 | Staleness: unknown tool status treated as OK | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T09 | internal/promotion/staleness.go:88 | Staleness: policy change ignored | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T10 | internal/promotion/staleness.go:85 | Staleness: schema change ignored | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T11 | internal/promotion/staleness.go:133 | Staleness: reasons not sorted ascending | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T12 | internal/promotion/staleness.go:80 | Staleness: base release change ignored | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| R01 | internal/promotion/dependencies.go:132 | runtimeComponents: StorageContract not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R02 | internal/promotion/dependencies.go:132 | runtimeComponents: Evolution not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R03 | internal/promotion/dependencies.go:132 | runtimeComponents: SupportedIR not recorded | go test ./internal/promotion (whole package) | KILLED | TestRuntimeComponentsHashCoversEveryComponent |
| R04 | internal/promotion/dependencies.go:133 | runtimeComponents: tool DSL version not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R05 | internal/promotion/dependencies.go:133 | runtimeComponents: RuleDSL not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R06 | internal/promotion/dependencies.go:133 | runtimeComponents: ToolCatalog not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R07 | internal/promotion/dependencies.go:134 | runtimeComponents: Diff not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R08 | internal/promotion/dependencies.go:134 | runtimeComponents: RiskPolicies not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R09 | internal/promotion/dependencies.go:134 | runtimeComponents: Verifier not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 (the verifier version the brief names) |
| R10 | internal/promotion/dependencies.go:134 | runtimeComponents: AcceptanceVocab not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R11 | internal/promotion/dependencies.go:135 | runtimeComponents: RequirementPolicy not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R12 | internal/promotion/dependencies.go:135 | runtimeComponents: RequirementSetSpec not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| R13 | internal/promotion/dependencies.go:135 | runtimeComponents: AcceptanceSpec not recorded | go test ./internal/promotion (whole package) | SURVIVED | MA-02 |
| B01 | internal/promotion/bundle2.go:599 | restore: requirement set of another app | go test ./internal/promotion (whole package) | SURVIVED | LOW: a requirement set of another application is refused by the staging port only; a coordinated forgery would have to recompute the report; no killing test written |
| B02 | internal/promotion/bundle2.go:599 | restore: requirement version < 1 | go test ./internal/promotion (whole package) | SURVIVED | LOW: version < 1 of the requirement set; no killing test written |
| B03 | internal/promotion/bundle2.go:599 | restore: requirement policy version not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B04 | internal/promotion/bundle2.go:606 | restore: acceptance binding recorded in the body not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B05 | internal/promotion/bundle2.go:630 | restore: HighRisk flag not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B06 | internal/promotion/bundle2.go:635 | restore: a non-PASS verification claim is accepted | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B07 | internal/promotion/bundle2.go:635 | restore: verifier version not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B08 | internal/promotion/bundle2.go:642 | restore: bundle hash not compared | go test ./internal/promotion (whole package) | EQUIVALENT | the same condition Hash(raw) != hash is checked first at bundle.go:107 and tested by bundle2_test.go:294 |
| S01 | db/migrations/00088_m13_review_fixes.sql:79 | activate_release: drop FOR SHARE OF m (approvers memberships not locked until commit) | it-run fix 'TestM13\|TestF05' (60 tests, canary proved the mutant live) | SURVIVED |  |
| V01 | internal/verification/literals.go:34 | literalAmounts: a condition reading any field counts as reading the threshold field | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V02 | internal/verification/literals.go:37 | literalAmounts: money literals of another currency are probed | go test ./internal/verification (whole package) | SURVIVED | LOW: extra probes only (an amount of another currency is probed against the threshold currency) |
| V03 | internal/verification/literals.go:52 | literalAmounts: amounts of conditions that do not read the field are probed | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V04 | internal/verification/literals.go:17 | MaxLiteralAmounts 32 -> 33 | go test ./internal/verification (whole package) | SURVIVED | LOW: the tests derive the bound from the constant itself |
| V05 | internal/verification/literals.go:90 | literalProbes: "below" probes above | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V06 | internal/verification/literals.go:90 | literalProbes: "above" probes below | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V07 | internal/verification/literals.go:92 | literalProbes: midpoints ignore the threshold | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V08 | internal/verification/literals.go:102 | literalProbes: amounts already probed are probed again | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V09 | internal/verification/literals.go:96 | literalProbes: midpoint replaced by the lower neighbour | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V10 | internal/verification/literals.go:94 | literalProbes: the last midpoint is dropped | go test ./internal/verification (whole package) | KILLED | TestLiteralProbesFollowTheAmountsOfThePlan |
| V11 | internal/verification/verifier.go:243 | threshold: the amount bound is exclusive | go test ./internal/verification (whole package) | KILLED | TestAPlanWithTooManyAmountsIsBlocked |
| V12 | internal/verification/verifier.go:230 | threshold: non-money field accepted | go test ./internal/verification (whole package) | KILLED | TestBlockedStates |
| V13 | internal/verification/verifier.go:230 | threshold: workflow of another entity accepted | go test ./internal/verification (whole package) | SURVIVED | LOW/MEDIUM: a threshold requirement whose workflow belongs to another entity is no longer BLOCKED up front; the probes then fail or block on missing fields; no killing test written |
| V14 | internal/verification/verifier.go:250 | threshold: amounts of the wrong-currency probe count as probed | go test ./internal/verification (whole package) | EQUIVALENT | the wrong-currency probe has the same amount text as the "at" probe (oracle.go:163,167), which is already taken |
| V15 | internal/verification/verifier.go:322 | observe: an unevaluable mined variant blocks | go test ./internal/verification (whole package) | SURVIVED | LOW: an unevaluable mined variant blocks instead of being skipped |
| V16 | internal/verification/verifier.go:327 | threshold: a wrong observation never fails | go test ./internal/verification (whole package) | KILLED | TestMutantsFailOrBlockWithSpecificCodes |
| V17 | internal/verification/verifier.go:349 | observe: currency mismatch not rejected | go test ./internal/verification (whole package) | KILLED | TestControlFixturePasses |
| V18 | internal/verification/verifier.go:372 | observe: a rejection that completes goes unnoticed | go test ./internal/verification (whole package) | KILLED | TestRejectedApprovalThatCompletesFails |
| V19 | internal/verification/verifier.go:379 | observe: a failed state is never an expectation | go test ./internal/verification (whole package) | SURVIVED | LOW: a failed state on a fixed probe is BLOCKED instead of FAIL, still not PASS |
| V20 | internal/verification/verifier.go:429 | rejectionCompletes: approved edges count as rejection edges | go test ./internal/verification (whole package) | KILLED | TestControlFixturePasses |
| V21 | internal/verification/verifier.go:597 | gate: the approved edge of a gating state counts as a bypass | go test ./internal/verification (whole package) | KILLED | TestRejectedApprovalThatCompletesFails |
| V22 | internal/verification/verifier.go:573 | gate: wrong roles never recorded | go test ./internal/verification (whole package) | KILLED | TestApprovalGateRequirements |
| V23 | internal/verification/verifier.go:635 | sameRoles: a role subset equals the required set | go test ./internal/verification (whole package) | SURVIVED | MA-03 |
| V24 | internal/verification/verifier.go:661 | toolEffect: a tool at the maximum effect fails | go test ./internal/verification (whole package) | KILLED | TestToolEffectRequirements |
| V25 | internal/verification/verifier.go:664 | toolEffect: approval NEVER/OPTIONAL accepted when approval is required | go test ./internal/verification (whole package) | KILLED | TestToolEffectRequirements |
| V26 | internal/verification/verifier.go:667 | toolEffect: unknown sandbox mode accepted | go test ./internal/verification (whole package) | EQUIVALENT | tools.NewCatalog rejects an unknown sandbox mode (catalog.go:355-381, default branch), so rank -1 is unreachable |
| V27 | internal/verification/verifier.go:658 | toolEffect: tool not allowed for the tenant accepted | go test ./internal/verification (whole package) | KILLED | TestToolEffectRequirements |
| V28 | internal/verification/verifier.go:687 | migration: TRANSFORM allowed under an ADDITIVE maximum | go test ./internal/verification (whole package) | EQUIVALENT | intent.classBounds is COMPATIBLE and TRANSFORM (requirements.go:112), so the added ADDITIVE alternative is unreachable; the real variant is V43 (killed) |
| V29 | internal/verification/verifier.go:685 | migration: BREAKING allowed | go test ./internal/verification (whole package) | KILLED | TestMigrationRequirements |
| V30 | internal/verification/verifier.go:745 | completeness: storage evolution needs no requirement | go test ./internal/verification (whole package) | KILLED | TestCompletenessRequiresResolvedMatchingRequirements |
| V31 | internal/verification/verifier.go:711 | completeness: an unresolved requirement covers a changed surface | go test ./internal/verification (whole package) | KILLED | TestCompletenessRequiresResolvedMatchingRequirements |
| V32 | internal/verification/verifier.go:68 | validate: acceptance bound to another requirement set | go test ./internal/verification (whole package) | EQUIVALENT | intent.ValidateAcceptance repeats the binding (acceptance.go:163), called two statements later |
| V33 | internal/verification/verifier.go:124 | Verify: optional requirements are verified like mandatory ones | go test ./internal/verification (whole package) | KILLED | TestNonMandatoryRequirementsAreSkippedAndReported |
| V34 | internal/verification/verifier.go:126 | Verify: an open mandatory requirement is verified | go test ./internal/verification (whole package) | KILLED | TestBlockedStates |
| V35 | internal/verification/verifier.go:129 | Verify: an unknown category is verified | go test ./internal/verification (whole package) | KILLED | TestBlockedStates |
| V36 | internal/verification/verifier.go:113 | Verify: floor violations are not blocked | go test ./internal/verification (whole package) | KILLED | TestBlockedStates |
| V37 | internal/verification/verifier.go:31 | MaxWork 50000 -> 500000 | go test ./internal/verification (whole package) | SURVIVED | LOW: the work bound constant |
| V38 | internal/verification/verifier.go:443 | maxMinedFields 8 -> 9 | go test ./internal/verification (whole package) | SURVIVED | LOW: the mining bound constant |
| V39 | internal/verification/verifier.go:444 | maxMinedPerField 6 -> 7 | go test ./internal/verification (whole package) | SURVIVED | LOW: the mining bound constant |
| V40 | internal/verification/verifier.go:492 | minedValues: pinned fields are mined as well | go test ./internal/verification (whole package) | KILLED | TestPinnedContextScopesTheRequirement |
| V41 | internal/verification/verifier.go:489 | minedValues: the threshold field is mined as well | go test ./internal/verification (whole package) | EQUIVALENT | the threshold field is money-typed (verifier.go:230) and the mining switch skips every kind but string, enum, bool, integer |
| V42 | internal/verification/verifier.go:287 | threshold: every probe is repeated for every mined value | go test ./internal/verification (whole package) | KILLED | TestProbeSetIsPinnedToTheVerifierVersion |
| O01 | internal/intent/oracle.go:108 | oracle: GT includes equality | go test ./internal/intent (whole package) | KILLED | TestOracleTruthTables |
| O02 | internal/intent/oracle.go:112 | oracle: LT includes equality | go test ./internal/intent (whole package) | KILLED | TestOracleTruthTables |
| O03 | internal/intent/oracle.go:95 | oracle: another currency compared as the threshold currency | go test ./internal/intent (whole package) | KILLED | TestOracleTruthTables |
| O04 | internal/intent/oracle.go:126 | oracle: approver roles not expected | go test ./internal/intent (whole package) | KILLED | TestOracleTruthTables |
| O05 | internal/intent/oracle.go:98 | oracle: invalid money amounts not rejected | go test ./internal/intent (whole package) | KILLED | TestOracleTruthTables |
| A01 | internal/ai/repair.go:77 | chargeUsage: input cap 2x+1024 -> 2x+1025 | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestChargeUsageIsCappedNearTheSizeOfTheRequest |
| A02 | internal/ai/repair.go:77 | chargeUsage: input cap 4x (review F-08) | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestChargeUsageIsCappedNearTheSizeOfTheRequest |
| A03 | internal/ai/repair.go:78 | chargeUsage: the byte estimate of a large answer no longer wins over the cap | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestChargeUsageIsCappedNearTheSizeOfTheRequest |
| A04 | internal/ai/repair.go:78 | chargeUsage: output not raised to the byte estimate | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestChargeUsageIsCappedNearTheSizeOfTheRequest |
| A05 | internal/ai/repair.go:221 | cancel charge: the reserved output is not charged (M14R-02) | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestACanceledCallIsChargedTheReservedOutput |
| A06 | internal/ai/repair.go:233 | timeout charge: the input estimate is not charged | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestAnAdapterThatIgnoresItsContextCannotHoldTheRun |
| A07 | internal/ai/repair.go:235 | provider error: the run budget is not reduced | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: after a provider error the input estimate is not taken off the run budget; no test; cost control only |
| A08 | internal/ai/repair.go:251 | answer: the output is not taken off the run budget | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | KILLED | TestPropertiesOfEveryRun (widened) |
| A09 | internal/ai/repair.go:115 | abandoned bound: off by one | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestAbandonedProviderCallsAreBounded |
| A10 | internal/ai/repair.go:152 | abandoned bound 16 -> 17 | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: the abandoned-call bound test loops to the constant itself |
| V43 | internal/verification/verifier.go:688 | migration: TRANSFORM allowed under a COMPATIBLE maximum | go test ./internal/verification (whole package) | KILLED | TestMigrationRequirements |
| V44 | internal/verification/verifier.go:602 | gate: reaching completion is not a bypass | go test ./internal/verification (whole package) | KILLED | TestRejectedApprovalThatCompletesFails |
| V45 | internal/verification/verifier.go:604 | gate: a failed end counts as a bypass | go test ./internal/verification (whole package) | KILLED | TestRejectedApprovalThatCompletesFails |
| V46 | internal/verification/verifier.go:301 | threshold: human cases of other requirements are evaluated | go test ./internal/verification (whole package) | SURVIVED |  |
| F01 | internal/platform/access/bodybudget.go:68 | BodyHold.Add: the share of one identity is exclusive | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F02 | internal/platform/access/bodybudget.go:71 | BodyHold.Add: the total is exclusive | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' ; widened: ./internal/platform/access | SURVIVED | LOW: the total bound is exclusive at exactly total bytes; no test books exactly the total |
| F03 | internal/platform/access/bodybudget.go:68 | BodyHold.Add: no per-identity share | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F04 | internal/platform/access/bodybudget.go:71 | BodyHold.Add: no total bound | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F05 | internal/platform/access/bodybudget.go:89 | Release: the total is never given back | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F06 | internal/platform/access/bodybudget.go:90 | Release: an identity with nothing left stays listed | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldReleaseIsIdempotentAndForgetsIdentities |
| F07 | internal/platform/access/bodybudget.go:62 | Add: a released hold books again | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldReleaseIsIdempotentAndForgetsIdentities |
| F08 | internal/platform/access/bodybudget.go:76 | Add: the hold forgets what it booked | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F09 | internal/platform/access/bodybudget.go:81 | Release: a second release gives the bytes back twice | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' ; widened: ./internal/platform/access | EQUIVALENT | Release zeroes h.n, so a second release subtracts 0 whatever the guard says; the released flag is still tested through F07 |
| F10 | internal/platform/access/bodybudget.go:130 | bookedBody: the bytes that did not fit are reported to the caller | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBookedBodyStopsTheReadThatDoesNotFit |
| F11 | internal/platform/access/bodybudget.go:33 | default identity share below the largest admitted body | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' ; widened: ./internal/platform/access | SURVIVED | LOW: a default identity share below 1 MiB would refuse honest uploads; no test pins the default |
| TR01 | internal/platform/traffic/gate.go:29 | Validate: a peer rate above the global rate is valid | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW: configuration validation relation not tested |
| TR02 | internal/platform/traffic/gate.go:29 | Validate: a peer burst above the global burst is valid | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW: configuration validation relation not tested |
| TR03 | internal/platform/traffic/gate.go:29 | Validate: peer rate ceiling 10000 -> 100000 | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW: configuration validation bound not tested |
| TR04 | internal/platform/traffic/gate.go:29 | Validate: burst may be three times the rate | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW: configuration validation bound not tested |
| TR05 | internal/platform/traffic/gate.go:62 | bucket: a credit of exactly one token is not enough | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW: a credit of exactly one token is refused; no test lands on it |
| TR06 | internal/platform/traffic/gate.go:58 | bucket: refill without the burst ceiling | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW (MA-05): the refill can exceed the burst by up to 2x |
| TR07 | internal/platform/traffic/gate.go:182 | Peer: the global bucket is never consumed | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| TR08 | internal/platform/traffic/gate.go:171 | Peer: the global bucket is never checked | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| TR09 | internal/platform/traffic/gate.go:100 | pool: one key above the bound | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| TR10 | internal/platform/traffic/gate.go:197 | Invalid: no global bound on invalid credentials | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| TR11 | internal/platform/traffic/gate.go:149 | decide: a clock that goes backwards is used as is | go test ./internal/platform/traffic (whole package) | KILLED | TestRefillFractionalCreditRetryAndClockRegression |
| TR12 | internal/platform/traffic/gate.go:210 | Subject: no length prefix (issuer, subject) pairs can collide | go test ./internal/platform/traffic (whole package) | KILLED | TestSubjectKeyIsSignedIdentityTupleAndNotPeerOrConcatenation |
| TR13 | internal/platform/traffic/gate.go:142 | peerKey: IPv4-mapped IPv6 addresses get their own bucket | go test ./internal/platform/traffic (whole package) | KILLED | TestRefillFractionalCreditRetryAndClockRegression |
| TR14 | internal/platform/traffic/gate.go:142 | peerKey: IPv6 zones multiply the buckets of one host | go test ./internal/platform/traffic (whole package) ; widened: ./internal/platform/traffic | SURVIVED | LOW: IPv6 zones come from the server-side connection, not from the client |
| TR15 | internal/platform/traffic/gate.go:42 | RetrySeconds: rounds down to zero | go test ./internal/platform/traffic (whole package) | KILLED | TestRefillFractionalCreditRetryAndClockRegression |
| TR16 | internal/platform/traffic/gate.go:194 | Invalid: no per-peer bound on invalid credentials | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| TR17 | internal/platform/traffic/gate.go:214 | Subject: the key keeps one byte of the digest | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| A11 | internal/ai/repair.go:131 | abandoned calls are never uncounted | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestAnAdapterThatIgnoresItsContextCannotHoldTheRun |
| A12 | internal/ai/repair.go:199 | reservation check ignores the output | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW (MA-04): the loop calls the provider with a budget that does not cover the reserved output |
| A13 | internal/ai/repair.go:185 | an exhausted budget (0) may still call the provider | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | EQUIVALENT | remaining == 0 is refused by the next check (remaining < estimate + output), which is positive |
| TR08b | internal/platform/traffic/gate.go:182 | Peer: the peer bucket is never consumed | go test ./internal/platform/traffic (whole package) | KILLED | TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy |
| A14 | internal/ai/repair.go:180 | attempt bound off by one | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestRunPatchNeedsHumanAtTheAttemptLimit |
| A15 | internal/ai/repair.go:159 | configured attempts may exceed the ceiling | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestRunPatchBudgets |
| A16 | internal/ai/repair.go:167 | output token ceiling dropped | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: the output-token ceiling is enforced a second time by ServiceLimits.Effective |
| A17 | internal/ai/repair.go:171 | wall clock ceiling dropped | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: the wall-clock ceiling is enforced a second time by ServiceLimits.Effective (TestEffectiveWallClock) |
| A18 | internal/ai/repair.go:258 | output size bound is exclusive | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: the output size bound is exclusive at exactly MaxOutputBytes; no boundary test |
| A20 | internal/ai/repair.go:53 | estimateTokens rounds down | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: estimateTokens rounds down; every test uses multiples of four bytes |
| A21 | internal/ai/repair.go:215 | caller cancellation not charged | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestACanceledCallIsChargedTheReservedOutput |
| A22 | internal/ai/repair.go:282 | a terminal verdict still gets another model call | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestRunPatchInfrastructureAndCallerErrors |
| A23 | internal/ai/repair.go:301 | diagnostics bound off by one | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | KILLED | TestAdversarialCorpus (widened) |
| TL01 | internal/tools/catalog.go:276 | Lookup: the pinned hash is not compared (a changed tool replaces a pinned one) | go test ./internal/tools (whole package) | KILLED | TestLoadCatalogAcceptsValidDefinitions |
| TL03 | internal/tools/catalog.go:236 | NewCatalog: a duplicate ref@version silently replaces the first | go test ./internal/tools (whole package) | KILLED | TestNewCatalogRejectsUnsafeDefinitions |
| TL04 | internal/tools/catalog.go:225 | NewCatalog: no size bound | go test ./internal/tools (whole package) | SURVIVED | LOW: the catalog size bound has no test at the bound |
| TL05 | internal/tools/catalog.go:255 | NewCatalog: a sandbox may use another tool's production credential | go test ./internal/tools (whole package) | KILLED | TestSandboxMayNotUseAnotherToolsProductionHostOrCredential |
| TL06 | internal/tools/catalog.go:184 | AllowsTenant: a tenant id prefix matches | go test ./internal/tools (whole package) | EQUIVALENT | definition tenants must match tenantPattern (a whole tenant id) or be "*", so a prefix match equals equality |
| SB01 | internal/sandbox/context.go:12 | Validate: a sandbox with production credentials mounted is valid | go test ./internal/sandbox (whole package) | KILLED | TestIsolationAdmission |
| SB02 | internal/sandbox/context.go:12 | Validate: a sandbox without an id is valid | go test ./internal/sandbox (whole package) | SURVIVED | LOW: legacy 1.0.0 bundle admission only; an empty sandbox id is valid |
| SB03 | internal/sandbox/context.go:16 | Validate: production data is a sandbox data mode | go test ./internal/sandbox (whole package) | SURVIVED | LOW: legacy 1.0.0 bundle admission only; production data mode accepted |
| SB04 | internal/sandbox/context.go:21 | Validate: a live tool mode is a sandbox tool mode | go test ./internal/sandbox (whole package) | SURVIVED | LOW: legacy 1.0.0 bundle admission only; the test names one arbitrary bad tool mode |
| TL07 | internal/tools/catalog.go:582 | asciiKey: upper-case and punctuation keys are admitted | go test ./internal/tools (whole package) | KILLED | TestLoadCatalogRejectsMalformedDocuments |
| TL08 | internal/tools/catalog.go:611 | strictKeys: duplicate keys are admitted | go test ./internal/tools (whole package) | KILLED | TestLoadCatalogRejectsMalformedDocuments |
| A24 | internal/ai/repair.go:59 | clampTokens: no upper clamp | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | EQUIVALENT | chargeUsage takes min(..., 2*in+1024) after the clamp, which is far below maxChargeTokens |
| TL09 | internal/tools/catalog.go:261 | NewCatalog: the catalog hash does not cover the catalog version | go test ./internal/tools (whole package) | SURVIVED | LOW: the catalog hash omits CatalogVersion; the version is covered by the runtime components hash |
| TL10 | internal/tools/catalog.go:259 | NewCatalog: the catalog hash depends on the order of the definitions | go test ./internal/tools (whole package) | KILLED | TestCatalogHashStableAndOrderIndependent |
| N01 | internal/tools/netpolicy.go:91 | classifyAddress: IPv4-mapped IPv6 is not unmapped | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| N02 | internal/tools/netpolicy.go:95 | classifyAddress: the metadata address list is not consulted | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| N03 | internal/tools/netpolicy.go:52 | neverV4: the link-local block is only internal | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointResolvingToDeniedAddressIsBlockedWithoutConnecting |
| N04 | internal/tools/netpolicy.go:52 | neverV4: the Azure WireServer is public | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| N05 | internal/tools/netpolicy.go:109 | classifyAddress: ISATAP embedded IPv4 is not examined | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| N06 | internal/tools/netpolicy.go:121 | classifyAddress: NAT64 is public whatever it embeds | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointResolvingToDeniedAddressIsBlockedWithoutConnecting |
| A25 | internal/ai/repair.go:57 | clampTokens: negative figures refill the budget | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | EQUIVALENT | chargeUsage takes max(clamp(v), estimate) with estimate >= 0, so a negative figure and 0 give the same charge |
| N07 | internal/tools/netpolicy.go:149 | embeddedClass: an internal IPv4 inside a translation form is public | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointResolvingToDeniedAddressIsBlockedWithoutConnecting |
| G01 | internal/ai/gateway.go:95 | RunPatch: apply conflicts ignored | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' | KILLED | TestRunPatchHostileAnswersNeverProduceADocument |
| N08 | internal/tools/netpolicy.go:165 | DeniedAddress: internal addresses are always allowed | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointResolvingToDeniedAddressIsBlockedWithoutConnecting |
| N09 | internal/tools/netpolicy.go:200 | dialContext: any address may be dialed | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointRejectsAddressesRevalidatedAtConnect |
| N10 | internal/tools/netpolicy.go:217 | dialContext: only the first resolved answer is validated | go test ./internal/tools (whole package) | KILLED | TestOneDeniedAnswerRefusesTheWholeDestination |
| N11 | internal/tools/netpolicy.go:210 | dialContext: no bound on the resolved answers | go test ./internal/tools (whole package) | SURVIVED | LOW: the bound on resolved answers is untested |
| AC01 | internal/platform/access/metadata.go:28 | AcceptsPOST: the AI POST routes are not admitted | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: no unit test pins that the AI POST routes are admitted by AcceptsPOST (the comment at ai.go:36 cites an ai_test.go that does not exist); the integration suite would see every AI POST refused |
| N12 | internal/tools/netpolicy.go:271 | controlContext: the port is not re-validated | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointRejectsAddressesRevalidatedAtConnect |
| N13 | internal/tools/netpolicy.go:271 | controlContext: the literal address is not re-validated | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointRejectsAddressesRevalidatedAtConnect |
| G04 | internal/ai/gateway.go:201 | RunRequirements: NEEDS_HUMAN for any last outcome | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED |  |
| N14 | internal/tools/netpolicy.go:296 | AddressGuard: the denied predicate is not consulted | go test ./internal/tools (whole package) | SURVIVED | see N14b |
| AC02 | internal/platform/access/ai.go:67 | aiPOST: an unregistered route is admitted | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: no unit test pins aiPOST against the registered routes |
| N15 | internal/tools/netpolicy.go:296 | AddressGuard: any port | go test ./internal/tools (whole package) | SURVIVED | see N15b |
| N16 | internal/tools/netpolicy.go:90 | classifyAddress: a zoned IPv6 address bypasses the prefix checks | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| AC03 | internal/platform/access/ai.go:49 | aiPatterns: reject needs only ai.read | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: no unit test pins the policy action of the apply, confirm, reject and GET routes (aiPatterns, ai.go:42-50); release routes have such a pin (promote_test.go:67-73) |
| N17 | internal/tools/netpolicy.go:54 | neverV6: only a /64 of the AWS block | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| AC04 | internal/platform/access/ai.go:43 | aiPatterns: creating a patch proposal needs only ai.read | go test ./internal/platform/access (whole package) | KILLED | TestAICreateRoutesAreExactlyTheProposeRoutes |
| AC05 | internal/platform/access/ai.go:47 | aiPatterns: apply needs ai.propose instead of ai.apply | go test ./internal/platform/access (whole package) | KILLED | TestAICreateRoutesAreExactlyTheProposeRoutes |
| AC06 | internal/platform/access/ai.go:57 | aiCreatePOST: requirement proposals are not repeated after a known abort | go test ./internal/platform/access (whole package) | KILLED | TestAICreateRoutesAreExactlyTheProposeRoutes |
| G05 | internal/ai/gateway.go:110 | RunPatch: a program the compiler returned invalid is accepted | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED |  |
| AC07 | internal/platform/access/ai.go:84 | aiCreateBody 32 KiB -> 64 KiB | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the body caps are tested by the integration suite only |
| AC08 | internal/platform/access/ai.go:87 | aiRejectBody 4 KiB -> 64 KiB | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the body caps are tested by the integration suite only |
| AC09 | internal/platform/access/ai.go:107 | aiBody: a body of exactly the limit is too large | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: boundary of the body cap |
| AC10 | internal/platform/access/ai.go:119 | aiBody: unknown keys are admitted | go test ./internal/platform/access (whole package) | SURVIVED |  |
| AC11 | internal/platform/access/ai.go:124 | aiBody: required keys may be absent | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: required keys of the AI bodies |
| AC12 | internal/platform/access/ai.go:100 | aiBody: any content type | go test ./internal/platform/access (whole package) | SURVIVED |  |
| P01 | internal/ai/patch.go:109 | apply: replacing an existing definition of a forbidden kind | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalRefusesToReplaceAnExistingToolUnderAnAllowedId |
| AC13 | internal/platform/access/ai.go:450 | aiTokenOutlivesTheRun: a token that ends exactly with the run is refused | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: token-expiry boundary is tested by the integration suite (m14r_ai_route_test.go) |
| AC14 | internal/platform/access/ai.go:450 | aiTokenOutlivesTheRun: any unexpired token is accepted | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: token-expiry check is tested by the integration suite (m14r_ai_route_test.go) |
| AC15 | internal/platform/access/ai.go:407 | aiPropose: expected_revision 0 is valid | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: expected_revision lower bound |
| AC16 | internal/platform/access/ai.go:518 | aiRun: the result exchange need not be the requester | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the requester check of the result exchange is tested by the integration suite |
| AC19 | internal/platform/access/promote.go:614 | activateCandidate: the expected generation need not match the candidate base | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: Go-side expected generation check (the port repeats it) |
| P02 | internal/ai/patch.go:115 | apply: a definition carrying a tool payload | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalRefusesEveryNonAllowlistedKind |
| AC20 | internal/platform/access/promote.go:598 | activateCandidate: generation bound dropped | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: Go-side generation bound (the port repeats it) |
| AC22 | internal/platform/access/promote.go:691 | rollbackRelease: a never-activated target is not refused in Go | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: Go-side never-activated check (the port repeats it) |
| AC23 | internal/platform/access/promote.go:706 | rollbackRelease: no plan is required in Go | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: Go-side plan requirement (the port repeats it) |
| AC24 | internal/platform/access/promote.go:698 | rollbackRelease: a rejected target is not refused in Go | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: Go-side rejected-target check (the port repeats it) |
| P03 | internal/ai/patch.go:118 | apply: definition kind need not match the target | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' ; widened: ./internal/ai | SURVIVED |  |
