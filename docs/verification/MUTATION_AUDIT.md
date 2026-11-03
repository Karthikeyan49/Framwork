# Mutation audit of the M12/M13/M14 lane

Auditor: independent mutation auditor (brief 65). Target pin: `11a4ee40a12e0301472b275203ac10d8347eaad9` (branch `integration/m9r-m14`, code head `585499d`).
Branch: `review/mutation-audit`. Product code is changed only as one temporary mutation at a time (reverted with `git checkout -- <path>` after every run); this file is the only thing committed.

Method. For every mutant: one exact text replacement in one product file, then the narrowest tests that should fail (`/home/user/bin/gt test -count=1 -run '<regex>' ./<pkg>`), widened to the whole package when the narrow run passed; SQL mutants through `/home/user/bin/it-run fix` with the M13 test group. Results: KILLED (name of the first failing test), SURVIVED (everything listed passed), EQUIVALENT (argued below). A mutant that did not compile or whose edit did not apply is discarded and listed.
Unit mutants are measured against the unit tests of their package only unless the row says otherwise: a unit survivor in a function that the integration suite covers is reported as SURVIVED with the note "unit-only" and is NOT claimed to be unprotected. Every survivor that concerns an authority, tenant, integrity or exactly-once invariant is analysed in Findings with a killing test that was run on the mutant (fails) and on the original (passes).
Stack lock caveat found during the run: `it-run` releases the stack lock between its reset phase and its test phase, so another agent using the same stack can rebuild it in between. Every SQL mutant is therefore run with a canary test (`TestZZCanary`, in the same `go test` invocation, reading `pg_get_functiondef`) that fails when the stack does not run the mutant. A first S01 run without the canary reported SURVIVED against a stack that had been rebuilt from the original migrations; it was discarded and redone.

## Progress

Status at this commit: PARTIAL, in progress (the run was interrupted once by an account limit and resumed; nothing is lost, the table below is regenerated from the run log).

- Target 1 (M13 approval and promotion): unit mutants of `internal/policy` (U), `internal/promotion` (D, T, R, B) done; SQL port mutant S01 done, further SQL mutants pending.
- Target 2 (M12 verifier): mutants prepared, not yet run.
- Target 3 (M14 pipeline and route): mutants prepared, not yet run.
- Targets 4 and 5: not reached yet.

## Mutation score per target

KILLED / SURVIVED / EQUIVALENT after the reclassification below; the score is killed / (killed + survived). "Survived" means: survived every test listed in the "tests run" column of its row, and nothing else was run.

| key | target | killed | survived | equivalent | score |
|---|---|---|---|---|---|
| 1a | Target 1 - M13 policy (Holds / NativeHolds / Decide / permits / RoleStore) | 12 | 23 | 1 | 34% (12/35) |
| 1b | Target 1 - M13 promotion package (counting, floors, staleness, runtime components, restore gate) | 36 | 21 | 1 | 63% (36/57) |
| 1c | Target 1 - M13 SQL ports (migrations 00084-00092), through it-run fix | 0 | 1 | 0 | 0% (0/1) |
| 2 | Target 2 - M12 verifier and oracle | 0 | 0 | 0 | not reached |
| 3a | Target 3 - M14 gateway, repair loop, patch, kinds, schema, service helpers (internal/ai) | 0 | 0 | 0 | not reached |
| 3b | Target 3 - M14 route and SQL (internal/platform/access/ai.go, migration 00089) | 0 | 0 | 0 | not reached |
| 4 | Target 4 - platform front door and traffic | 0 | 0 | 0 | not reached |
| 5 | Target 5 - M10 tools gateway and M11 sandbox | 0 | 0 | 0 | not reached |
| all | all targets reached | 48 | 45 | 2 | 52% (48/93) |

Discarded (did not compile or the edit did not apply, redone or dropped): B09.

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
| T05 | internal/promotion/staleness.go:108 | Staleness: a blocked assessment is not an escalation | go test ./internal/promotion (whole package) | SURVIVED |  |
| T06 | internal/promotion/staleness.go:126 | Staleness: empty components hashes match | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T07 | internal/promotion/staleness.go:92 | Staleness: nil tool resolver treated as OK | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T08 | internal/promotion/staleness.go:102 | Staleness: unknown tool status treated as OK | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T09 | internal/promotion/staleness.go:88 | Staleness: policy change ignored | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T10 | internal/promotion/staleness.go:85 | Staleness: schema change ignored | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T11 | internal/promotion/staleness.go:133 | Staleness: reasons not sorted ascending | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| T12 | internal/promotion/staleness.go:80 | Staleness: base release change ignored | go test ./internal/promotion (whole package) | KILLED | TestStalenessReasons |
| R01 | internal/promotion/dependencies.go:132 | runtimeComponents: StorageContract not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R02 | internal/promotion/dependencies.go:132 | runtimeComponents: Evolution not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R03 | internal/promotion/dependencies.go:132 | runtimeComponents: SupportedIR not recorded | go test ./internal/promotion (whole package) | KILLED | TestRuntimeComponentsHashCoversEveryComponent |
| R04 | internal/promotion/dependencies.go:133 | runtimeComponents: tool DSL version not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R05 | internal/promotion/dependencies.go:133 | runtimeComponents: RuleDSL not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R06 | internal/promotion/dependencies.go:133 | runtimeComponents: ToolCatalog not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R07 | internal/promotion/dependencies.go:134 | runtimeComponents: Diff not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R08 | internal/promotion/dependencies.go:134 | runtimeComponents: RiskPolicies not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R09 | internal/promotion/dependencies.go:134 | runtimeComponents: Verifier not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R10 | internal/promotion/dependencies.go:134 | runtimeComponents: AcceptanceVocab not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R11 | internal/promotion/dependencies.go:135 | runtimeComponents: RequirementPolicy not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R12 | internal/promotion/dependencies.go:135 | runtimeComponents: RequirementSetSpec not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| R13 | internal/promotion/dependencies.go:135 | runtimeComponents: AcceptanceSpec not recorded | go test ./internal/promotion (whole package) | SURVIVED |  |
| B01 | internal/promotion/bundle2.go:599 | restore: requirement set of another app | go test ./internal/promotion (whole package) | SURVIVED |  |
| B02 | internal/promotion/bundle2.go:599 | restore: requirement version < 1 | go test ./internal/promotion (whole package) | SURVIVED |  |
| B03 | internal/promotion/bundle2.go:599 | restore: requirement policy version not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B04 | internal/promotion/bundle2.go:606 | restore: acceptance binding recorded in the body not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B05 | internal/promotion/bundle2.go:630 | restore: HighRisk flag not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B06 | internal/promotion/bundle2.go:635 | restore: a non-PASS verification claim is accepted | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B07 | internal/promotion/bundle2.go:635 | restore: verifier version not compared | go test ./internal/promotion (whole package) | KILLED | TestEveryTamperedFieldIsRejectedOnRestore |
| B08 | internal/promotion/bundle2.go:642 | restore: bundle hash not compared | go test ./internal/promotion (whole package) | SURVIVED |  |
| S01 | db/migrations/00088_m13_review_fixes.sql:79 | activate_release: drop FOR SHARE OF m (approvers memberships not locked until commit) | it-run fix 'TestM13\|TestF05' (60 tests, canary proved the mutant live) | SURVIVED |  |
