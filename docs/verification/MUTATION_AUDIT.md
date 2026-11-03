# Mutation audit of the M12/M13/M14 lane

Auditor: independent mutation auditor (brief 65). Target pin: `11a4ee40a12e0301472b275203ac10d8347eaad9` (branch `integration/m9r-m14`, code head `585499d`).
Branch: `review/mutation-audit`. Product code is changed only as one temporary mutation at a time (reverted with `git checkout -- <path>` after every run); this file is the only thing committed.

Method. For every mutant: one exact text replacement in one product file, then the narrowest tests that should fail (`/home/user/bin/gt test -count=1 -run '<regex>' ./<pkg>`), widened to the whole package when the narrow run passed; SQL mutants through `/home/user/bin/it-run fix` with the M13 test group. Results: KILLED (name of the first failing test), SURVIVED (everything listed passed), EQUIVALENT (argued below). A mutant that did not compile or whose edit did not apply is discarded and listed.
Unit mutants are measured against the unit tests of their package only unless the row says otherwise: a unit survivor in a function that the integration suite covers is reported as SURVIVED with the note "unit-only" and is NOT claimed to be unprotected. Every survivor that concerns an authority, tenant, integrity or exactly-once invariant is analysed in Findings with a killing test that was run on the mutant (fails) and on the original (passes).
Stack lock caveat found during the run: `it-run` releases the stack lock between its reset phase and its test phase, so another agent using the same stack can rebuild it in between. Every SQL mutant is therefore run with a canary test (`TestZZCanary`, in the same `go test` invocation, reading `pg_get_functiondef`) that fails when the stack does not run the mutant. A first S01 run without the canary reported SURVIVED against a stack that had been rebuilt from the original migrations; it was discarded and redone.
Reading the scores. A unit score below the integration reality is expected for code that only the integration suite can call (`policy.Decide`, `validSubject`, `Defined.DataScope`, `RoleStore.*`, the handlers of `internal/platform/access`, all SQL ports): their survivors say "no unit test", not "no test". The scores of targets 1a, 1d and 3c therefore measure the unit tests only; the integration suite was run against two SQL mutants (S01, X06) only. The scores of the pure packages (promotion, verifier, oracle, ai, traffic, tools) are the ones that measure the tests of the logic itself.

## Progress

Status at this commit: PARTIAL. The run was cut off once by an account limit and resumed; everything below is regenerated from the run log (`results.tsv` of the auditor, rows below).

- Target 1 (M13): policy (U), promotion (D, T, R, B), handler authority helpers (AU, 9), SQL mutant S01 (finding MA-01). NOT REACHED: the end-to-end mutants H01 (`holdingApprovers` narrowing), U010i (selector entity) and the SQL mutants S02 to S08: the run of H01 was queued on `it-run fix` for 15 minutes behind seven other agents for the global heavy lock (`/tmp/framework-heavy.lock`) and never started; the other ones were never submitted.
- Target 2 (M12 verifier and oracle): done (V, O); findings MA-03 (verifier) and, through the runtime components, MA-02.
- Target 3 (M14): gateway, repair loop, patch, kinds, schema (A, G, P, K, SC) done; the service helpers SV were reached up to the id shown in the table; SQL mutant X06 done (killed); X01 to X05 NOT RUN (same lock queue).
- Target 4 (front door, traffic): body budget (F), traffic gate (TR), `AcceptsPOST`, `enforce`, route to action mapping (AC) at unit level; the route table needs the integration suite (NOT RUN).
- Target 5 (tools gateway, network policy, sandbox context): done (TL, N, SB); the freeze/evidence manifest is SQL (`runtime.freeze_sandbox`) and was NOT RUN.

## Mutation score per target

KILLED / SURVIVED / EQUIVALENT after the reclassification below; the score is killed / (killed + survived). "Survived" means: survived every test listed in the "tests run" column of its row, and nothing else was run.

| key | target | killed | survived | equivalent | score |
|---|---|---|---|---|---|
| 1a | Target 1 - M13 policy (Holds / NativeHolds / Decide / permits / RoleStore), unit and end to end | 12 | 23 | 1 | 34% (12/35) |
| 1b | Target 1 - M13 promotion package (counting, floors, staleness, runtime components, restore gate) | 36 | 19 | 3 | 65% (36/55) |
| 1c | Target 1 - M13 SQL ports (migrations 00084-00092), through it-run fix | 0 | 1 | 0 | 0% (0/1) |
| 1d | Target 1 - M13 handlers (holdingApprovers end to end; authority helpers, unit; internal/platform/access/promote.go) | 0 | 8 | 1 | 0% (0/8) |
| 2 | Target 2 - M12 verifier and oracle | 36 | 10 | 5 | 78% (36/46) |
| 3a | Target 3 - M14 gateway, repair loop, patch, kinds, schema, service helpers (internal/ai) | 42 | 24 | 4 | 64% (42/66) |
| 3b | Target 3 - M14 SQL (migration 00089), through it-run fix | 1 | 0 | 0 | 100% (1/1) |
| 3c | Targets 3 and 4 - front door and handlers (internal/platform/access: ai.go, promote.go, metadata.go), unit | 4 | 25 | 0 | 14% (4/29) |
| 4 | Target 4 - body budget and traffic gate | 18 | 9 | 1 | 67% (18/27) |
| 5 | Target 5 - M10 tools gateway, network policy and M11 sandbox context | 21 | 8 | 1 | 72% (21/29) |
| all | all targets reached | 170 | 127 | 16 | 57% (170/297) |

Discarded (did not compile or the edit did not apply, redone or dropped): B09, AC17, AC18, AC21, AC25, AC32.

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
Killing test, verified (passes on the original; fails on all 13 mutants R01 to R13, run in a scratch copy of the tree: see the section "Killing tests, run on the original and on the mutants"):

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

`internal/verification/verifier.go:631-643`: `sameRoles` compares the sorted role lists. Deleting `len(x) == len(y) &&` (V23) leaves every `internal/verification` test green, yet the comparison then accepts a prefix of the required list. With `approver_roles: ["admin","owner"]` required, a plan whose approval state lists only `["admin"]` passes the approval-gate requirement: the weaker gate is verified as the required one. The existing gate tests use a wrong role (`member`) and an equal two-role list, never a subset.
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

### MA-06 (LOW, unit-only) - the built-in role matrix does not pin roles.write/assign, commands.reconcile and workflows.reconcile/recover (mutants U027, U028, U031)

`permits` (`internal/policy/policy.go:62-171`): making viewers hold `commands.reconcile` (U027, line 105), members hold `workflows.reconcile` (U028, line 114) or admins hold `roles.write` and `roles.assign` (U031, line 75) leaves every test of `internal/policy` green: `TestPolicyMatrix` (`policy_test.go:12-56`) names none of these actions. The integration suite (role administration, workflow tests) is expected to see them; that was NOT RUN. Killing test, verified (passes on the original; fails on U027, U028 and U031, see the section "Killing tests, run on the original and on the mutants"):

```go
package policy

import "testing"

func TestBuiltInRolesAndTheLessCommonActions(t *testing.T) {
	role := Resource{TenantID: "tenant-a", Type: "role"}
	preview := Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}
	workflow := Resource{TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"}
	for _, tc := range []struct {
		role, action string
		resource     Resource
		want         bool
	}{
		{"owner", "roles.write", role, true}, {"admin", "roles.write", role, false}, {"member", "roles.write", role, false},
		{"owner", "roles.assign", role, true}, {"admin", "roles.assign", role, false},
		{"member", "commands.reconcile", preview, true}, {"viewer", "commands.reconcile", preview, false},
		{"owner", "workflows.reconcile", workflow, true}, {"admin", "workflows.reconcile", workflow, true}, {"member", "workflows.reconcile", workflow, false}, {"viewer", "workflows.reconcile", workflow, false},
		{"admin", "workflows.recover", workflow, true}, {"member", "workflows.recover", workflow, false},
		{"member", "workflows.cancel", workflow, true}, {"viewer", "workflows.cancel", workflow, false}, {"viewer", "workflows.read", workflow, true},
	} {
		if got := permits("alice", "tenant-a", []string{tc.role}, tc.action, tc.resource); got != tc.want {
			t.Errorf("%s/%s: allow=%v, want %v", tc.role, tc.action, got, tc.want)
		}
	}
}
```

### Structural gap (LOW) - the engine's request path cannot be unit tested

`Native.Decide`'s `tenantScoped` path, `Defined.Decide`, `validSubject` and `Defined.DataScope` take a `policy.Subject` whose `identity.User` and `tenant.Access` have unexported fields and no test constructor, so no test in `internal/policy` can call them with a real subject. The 13 surviving policy mutants U010 to U022 and the 4 of `policy/store.go` (U033 to U036: inactive role head, bootstrap grants, several roles, worker actor binding), which need a database as well (selector widening, subject binding, DENY ordering, empty-scope and cancelled-context behaviour) are therefore judged only by the integration suite; of those, `TestM9RStoredRoleEngineBindsTenantAndActor` (`m9r_capabilities_test.go:487`) covers the `d.actorID` and `d.tenantID` conjuncts of `validSubject` and `TestM13R03HoldsMatchesDecide` covers the DENY ordering through the routes, and the rest (U010-U014 individually, U017-U022) were NOT RUN end to end except where a row of the table says otherwise.

### MA-07 (LOW, unit gaps with verified killing tests) - helpers and bounds that no unit test pins

Each of these mutants passes every unit test of its package; each killing test below passes on the original and fails on the mutants named (see the section "Killing tests, run on the original and on the mutants").
- `promotion.WorseRollbackClass` / `rollbackRank` (`decisions.go:410-430`, mutants D20, D21) and the bounds `MaxNoteRunes` (`decisions.go:92`, D23: the SQL bound is `char_length <= 280`) and `MaxRollbackSpan` (D22): the worst class of a rollback span decides whether an acknowledged plan is required (M13R-02); only the integration test `TestM13R02*` can see the ranking (NOT RUN against D20/D21).
- `ai.estimateTokens` (`repair.go:53`, A20): rounding down under-charges every call by up to 3 bytes' worth of tokens; every test uses multiples of four bytes.
- `access.BodyHold.Add` (`bodybudget.go:71`, F02: the total reached exactly) and `DefaultBodyBudgetPerUser` (`bodybudget.go:33`, F11: a default share below the 1 MiB body the front door admits would refuse honest uploads).

```go
package promotion // zz: WorseRollbackClass and the bounds (kills D20, D21, D22, D23)

import (
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/risk"
)

func TestWorseRollbackClassAndTheBoundsThatMirrorTheDatabase(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{
		{risk.RollbackPointerSafe, risk.RollbackDataNotReversible, risk.RollbackDataNotReversible},
		{risk.RollbackDataNotReversible, risk.RollbackPointerSafe, risk.RollbackDataNotReversible},
		{risk.RollbackDataNotReversible, risk.RollbackExternalEffects, risk.RollbackExternalEffects},
		{risk.RollbackExternalEffects, risk.RollbackDataNotReversible, risk.RollbackExternalEffects},
		{risk.RollbackPointerSafe, "", ""},
		{"", risk.RollbackPointerSafe, ""},
		{risk.RollbackDataNotReversible, "", risk.RollbackDataNotReversible},
		{risk.RollbackPointerSafe, "weird", "weird"},
	} {
		if got := WorseRollbackClass(tc.a, tc.b); got != tc.want {
			t.Errorf("WorseRollbackClass(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
	if MaxNoteRunes != 280 || MaxRollbackSpan != 32 { // the note bound is the database's char_length <= 280
		t.Errorf("MaxNoteRunes %d MaxRollbackSpan %d", MaxNoteRunes, MaxRollbackSpan)
	}
}
```

```go
package ai // zz: kills A20

import "testing"

func TestEstimateTokensRoundsUp(t *testing.T) {
	for bytes, want := range map[int]int64{0: 0, 1: 1, 3: 1, 4: 1, 5: 2, 8: 2, 9: 3} {
		if got := estimateTokens(bytes); got != want {
			t.Errorf("estimateTokens(%d) = %d, want %d", bytes, got, want)
		}
	}
}
```

```go
package access // zz: kills F02, F11

import "testing"

func TestBodyBudgetReachesItsTotalExactlyAndTheDefaultShareFitsAMegabyte(t *testing.T) {
	b := NewBodyBudget(100, 60)
	if r := b.Hold("a").Add(60); r != "" {
		t.Fatalf("a share of exactly 60 refused: %q", r)
	}
	if r := b.Hold("b").Add(40); r != "" { // 60 + 40 == the total of 100
		t.Fatalf("the last 40 bytes of a total of 100 refused: %q", r)
	}
	if r := b.Hold("c").Add(1); r != bodyRefusedTotal {
		t.Fatalf("a byte over the total: %q", r)
	}
	if DefaultBodyBudgetPerUser <= 1<<20 || DefaultBodyBudgetBytes < DefaultBodyBudgetPerUser {
		t.Fatalf("defaults %d total, %d per identity", DefaultBodyBudgetBytes, DefaultBodyBudgetPerUser)
	}
}
```

### MA-08 (LOW, defense in depth) - `tools.AddressGuard`, the last check before connect(2) of the AI provider adapter, has no test (mutants N14, N15; N14b, N15b)

`internal/tools/netpolicy.go:293-301`, used at `internal/ai/provider_http.go:158`. Dropping the `denied(target.Addr())` predicate (N14) or the port comparison (N15) leaves `internal/tools` and `internal/ai` (the caller, run as N14b and N15b) green; nothing references `AddressGuard` in a test. The first layer still holds (every resolved address is checked with `p.denied` before the dial, `provider_http.go:150-154`, and the dial goes to the validated literal), so this is the second layer of a rebinding defence going untested; the tool gateway's own `controlContext` is covered (N12, N13 killed). Killing test, verified (passes on the original; fails on N14 and N15):

```go
package tools

import (
	"context"
	"net/netip"
	"testing"
)

func TestAddressGuardRefusesDeniedAddressesOtherPortsAndGarbage(t *testing.T) {
	denied := netip.MustParseAddr("10.0.0.1")
	guard := AddressGuard(443, func(a netip.Addr) bool { return a == denied })
	for address, refused := range map[string]bool{
		"203.0.113.7:443": false, "10.0.0.1:443": true, "203.0.113.7:80": true, "[2001:db8::1]:443": false, "not an address": true, "203.0.113.7": true,
	} {
		if err := guard(context.Background(), "tcp", address, nil); (err != nil) != refused {
			t.Errorf("%s: err %v, refused should be %v", address, err, refused)
		}
	}
}
```

### MA-09 (MEDIUM for AU01 and AU02, LOW for the rest) - the separation-of-duties helpers of the approval handlers have no unit test, and "the stricter of the bundle and the database floor" is exercised by nothing

`internal/platform/access/promote.go:150-232` (`authority.required`, `excluded`, `party`, `bars`, `eligible`) decide, in Go and before the port is called, the quorum, the excluded roles, the barred actor and the distinct eligible approvers; the comment at line 150 says "the stricter of the bundle's separation and the database floor applies". Mutants AU01 (the bundle can no longer raise the quorum above the floor), AU02 (the roles a bundle excludes beyond the floor are ignored), AU04 (an unknown party no longer bars everyone), AU05 (a malformed actor id is not barred), AU06/AU07 (the acceptance author or freezer is read from another party), AU09 (barred approvers become eligible) and AU08 (a repeated approver counts twice) all pass every unit test of `internal/platform/access` (package has no test of these helpers) and the shipped risk policy equals the database floor at every level (`release_risk_v1.go:62-67`), so no integration test can distinguish AU01 or AU02 either: the stricter-than-floor behaviour is enforced by Go alone (the database floor is `RequiredApprovals`/`FloorRoles`) and is unprotected. AU03 is EQUIVALENT (`tenant.ValidID` already refuses an upper-case actor, party ids are canonical).
Killing test, verified (passes on the original; fails on AU01, AU02, AU04, AU05, AU06, AU07, AU08, AU09 and passes on the equivalent AU03):

```go
package access

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/risk"
)

func TestAuthorityQuorumExcludedRolesBarredActorsAndEligibleApprovers(t *testing.T) {
	const requester, author, freezer, other, second = "aaaaaaaa-0000-4000-8000-000000000001", "aaaaaaaa-0000-4000-8000-000000000002", "aaaaaaaa-0000-4000-8000-000000000003", "aaaaaaaa-0000-4000-8000-000000000004", "aaaaaaaa-0000-4000-8000-000000000005"
	parties := risk.Parties{Requester: requester, AcceptanceAuthor: author, SandboxFreezer: freezer}
	high := authority{level: "HIGH", separation: risk.Separation{Approvals: 1, Excluded: []string{risk.RoleRequester, risk.RoleAcceptanceAuthor}}, parties: parties}
	critical := authority{level: "CRITICAL", separation: risk.Separation{Approvals: 2, Excluded: []string{risk.RoleRequester, risk.RoleAcceptanceAuthor, risk.RoleSandboxFreezer}}, parties: parties}
	stricter := authority{level: "LOW", separation: risk.Separation{Approvals: 3, Excluded: []string{risk.RoleSandboxFreezer}}, parties: parties}
	if high.required() != 1 || critical.required() != 2 || stricter.required() != 3 || (authority{level: "LOW", separation: risk.Separation{Approvals: 1}}).required() != 1 {
		t.Fatal("required quorum", high.required(), critical.required(), stricter.required())
	}
	if got := stricter.excluded(); !reflect.DeepEqual(got, []string{risk.RoleSandboxFreezer}) {
		t.Fatalf("a bundle that excludes the freezer at LOW: %v", got)
	}
	if got := high.excluded(); !reflect.DeepEqual(got, []string{risk.RoleRequester, risk.RoleAcceptanceAuthor}) {
		t.Fatalf("HIGH excludes %v", got)
	}
	for _, tc := range []struct {
		au     authority
		actor  string
		role   string
		barred bool
	}{
		{high, requester, risk.RoleRequester, true}, {high, author, risk.RoleAcceptanceAuthor, true}, {high, freezer, "", false}, {high, other, "", false},
		{critical, freezer, risk.RoleSandboxFreezer, true}, {critical, other, "", false}, {high, strings.ToUpper(requester), "", true}, {high, "not-an-id", "", true},
		{authority{level: "HIGH", separation: high.separation, parties: risk.Parties{Requester: requester}}, other, risk.RoleAcceptanceAuthor, true}, // an unknown party bars everyone
	} {
		if role, barred := tc.au.bars(tc.actor); role != tc.role || barred != tc.barred {
			t.Errorf("bars(%s) = %q, %v; want %q, %v", tc.actor, role, barred, tc.role, tc.barred)
		}
	}
	approvals := []promotion.Decision{{ActorID: other, At: time.Unix(1, 0)}, {ActorID: requester}, {ActorID: strings.ToUpper(other)}, {ActorID: second}, {ActorID: other}, {ActorID: author}}
	if got := high.eligible(approvals); !reflect.DeepEqual(got, []string{other, second}) {
		t.Fatalf("eligible approvers %v", got)
	}
}
```

### MA-10 (LOW) - `cleanText` is only partly pinned (mutants SC02, SC03, SC05, SC06)

`internal/ai/schema.go:304-319` keeps invisible and direction-changing characters out of the text of a model proposal that a human reviews and approves (the "invisible-instruction smuggling channel"). Removing the word joiner U+2060 (SC02), the Mongolian vowel separator U+180E (SC03), the bidi isolates U+2066 to U+2069 (SC05) or the C1 controls U+0080 to U+009F (SC06) passes the whole `internal/ai` package; the tests name the embeddings U+202A to U+202E, the tag characters and a few others. Killing test, verified (passes on the original; fails on SC02, SC03, SC04, SC05 and SC06):

```go
package ai

import "testing"

func TestCleanTextRefusesEveryInvisibleOrReorderingCharacter(t *testing.T) {
	for _, ok := range []string{"plain text", "line one\nline two\ttabbed", "é ü 日本語 🚀"} {
		if !cleanText(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	bad := map[string]rune{
		"NUL": 0x00, "BEL": 0x07, "ESC": 0x1b, "DEL": 0x7f, "C1 start": 0x80, "NEL": 0x85, "C1 end": 0x9f,
		"LRE": 0x202a, "RLO": 0x202e, "LRI": 0x2066, "FSI": 0x2068, "PDI": 0x2069,
		"line separator": 0x2028, "paragraph separator": 0x2029, "BOM": 0xfeff, "zero width space": 0x200b, "word joiner": 0x2060, "mongolian vowel separator": 0x180e,
		"tag start": 0xe0000, "tag end": 0xe007f,
	}
	for name, r := range bad {
		if cleanText("a" + string(r) + "b") {
			t.Errorf("%s (U+%04X) accepted", name, r)
		}
	}
}
```

### MA-11 (LOW) - only the wall-clock ceiling of `ServiceLimits.Effective` is tested (mutants SV01 to SV04)

`internal/ai/service.go:56-82`: "configuration may lower a ceiling and never raise it". `service_limits_test.go` pins the wall clock only; dropping the ceiling of the attempts (SV01), the output tokens (SV02), the in-flight requests per tenant (SV03) or the daily tokens (SV04) passes the whole package. The values come from trusted configuration (`config.go` validates them as well), so this is a hardening gap. Killing test, verified (passes on the original; fails on SV01, SV02, SV03 and SV04):

```go
package ai

import "testing"

func TestEffectiveLimitsNeverRaiseACeiling(t *testing.T) {
	got := (ServiceLimits{MaxAttempts: MaxAttempts + 5, MaxOutputTokens: MaxOutputTokensCeiling + 1, InFlightPerTenant: MaxInFlightPerTenant + 1, DailyTokens: maxDBTokens + 1}).Effective()
	if got.MaxAttempts != MaxAttempts || got.MaxOutputTokens != MaxOutputTokensCeiling || got.InFlightPerTenant != MaxInFlightPerTenant || got.DailyTokens != maxDBTokens {
		t.Fatalf("a ceiling was raised: %+v", got)
	}
	low := (ServiceLimits{MaxAttempts: 1, MaxOutputTokens: 7, InFlightPerTenant: 1, DailyTokens: 9}).Effective()
	if low.MaxAttempts != 1 || low.MaxOutputTokens != 7 || low.InFlightPerTenant != 1 || low.DailyTokens != 9 {
		t.Fatalf("a lower value was not kept: %+v", low)
	}
	zero := (ServiceLimits{}).Effective()
	if zero.MaxAttempts != MaxAttempts || zero.MaxOutputTokens != DefaultMaxOutputTokens || zero.InFlightPerTenant != DefaultInFlightPerTenant || zero.DailyTokens != DefaultDailyToken {
		t.Fatalf("defaults: %+v", zero)
	}
}
```

## Killing tests, run on the original and on the mutants

Every killing test was written as a temporary `zz_review_*_test.go` in a scratch copy of the tree (never in the audited worktree), run on the original code (passed, "ok") and then against each mutant it targets. The test names below carry the temporary prefix `TestZZReview`; the texts in Findings drop it. MA-01 is an integration test (its runs are the logs named there); all others are unit tests.

| failing test on the mutant | package | passes on the original | mutants it kills |
|---|---|---|---|
| TestZZReviewRuntimeComponentsRecordTheLiveVersions | `./internal/promotion` | yes | R01, R02, R03, R04, R05, R06, R07, R08, R09, R10, R11, R12, R13 |
| TestZZReviewGateWithFewerRolesThanRequiredFails | `./internal/verification` | yes | V23 |
| TestZZReviewRefillNeverExceedsTheBurst | `./internal/platform/traffic` | yes | TR06 |
| TestZZReviewNoCallWithoutTheOutputReservation | `./internal/ai` | yes | A12 |
| TestZZReviewBuiltInRolesAndTheLessCommonActions | `./internal/policy` | yes | U027, U028, U031 |
| TestZZReviewWorseRollbackClassAndTheBoundsThatMirrorTheDatabase | `./internal/promotion` | yes | D20, D21, D22, D23 |
| TestZZReviewEstimateTokensRoundsUp | `./internal/ai` | yes | A20 |
| TestZZReviewBodyBudgetReachesItsTotalExactlyAndTheDefaultShareFitsAMegabyte | `./internal/platform/access` | yes | F02, F11 |
| TestZZReviewAddressGuardRefusesDeniedAddressesOtherPortsAndGarbage | `./internal/tools` | yes | N14, N15 |
| TestZZReviewAuthorityQuorumExcludedRolesBarredActorsAndEligibleApprovers | `./internal/platform/access` | yes | AU01, AU02, AU04, AU05, AU06, AU07, AU09, AU08 |
| (no failing test) | `./internal/platform/access` | yes | AU03 (survived), AU08 (survived) |
| TestZZReviewCleanTextRefusesEveryInvisibleOrReorderingCharacter | `./internal/ai` | yes | SC02, SC03, SC04, SC05, SC06 |
| TestZZReviewEffectiveLimitsNeverRaiseACeiling | `./internal/ai` | yes | SV01, SV02, SV03, SV04 |

AU03 is EQUIVALENT (see Equivalent mutants); AU08 survived the first version of the test (no exact duplicate approver) and is killed by the final one (`K2-AU08`).

## Equivalent mutants

- D25 (`internal/promotion/decisions.go:551`): the dedupe key `lower(actor)+actor` is injective in the actor id, so `CountEligible` counts the same set.
- U002 (`internal/policy/holds.go:16`): `permits` and `permitsSelf` both return false for an empty actor, so the `member != ""` guard of `NativeHolds` is redundant.
- T05 (`internal/promotion/staleness.go:108`): `assessment.Blocked` cannot become true in `Staleness`: the blocking rules read facts that a built candidate cannot have (`release_risk_v1.go:58-60`) and only the BREAKING storage row blocks by volume (`release_risk_v1.go:84`), which Restore refuses (`bundle2.go:541`).
- B08 (`internal/promotion/bundle2.go:642`): the same condition `Hash(raw) != hash` is checked first at `bundle.go:107` and tested at `bundle2_test.go:294`.
- V14, V26, V28, V32, V41 (`internal/verification/verifier.go`): the wrong-currency probe has the same amount text as the "at" probe (oracle.go:163,167); `tools.NewCatalog` rejects an unknown sandbox mode (`catalog.go:355-381`); `classBounds` has no ADDITIVE (`requirements.go:112`); `intent.ValidateAcceptance` repeats the binding check (`acceptance.go:163`); the threshold field is money-typed and the mining switch skips every kind but string, enum, bool and integer.
- F09 (`internal/platform/access/bodybudget.go:81`): `Release` zeroes `h.n`, so a second release subtracts nothing whatever the guard says.
- A13, A24, A25 (`internal/ai/repair.go`): `remaining == 0` is refused by the next check; `chargeUsage` takes `min(..., 2*in+1024)` after the clamp and `max(clamp, estimate)` with a non-negative estimate.
- TL06 (`internal/tools/catalog.go`): definition tenants must match a whole tenant id (`tenantPattern`, line 37) or be `*`, so a prefix match equals equality.
- AU03 (`internal/platform/access/promote.go:207`): `tenant.ValidID` refuses an upper-case actor before the comparison and party ids are canonical, so `==` and `EqualFold` agree.
- SC11 (`internal/ai/schema.go:326`): `checkText` already applies `cleanText` to every string value of the tree (`schema.go:109`).

## NOT RUN

- Integration or SQL mutants that were planned and never ran: H01 (`holdingApprovers` narrowing, `promote.go:270`), U010i (selector entity ignored, end to end), S02 to S08 (M13 ports: CRITICAL quorum, approve pointer lock and fence, approval-count exclusions, rollback fence and span), X01 to X05 (M14 ports: pending reservations in the budget, decide ownership, finish charge and requester, in-flight limit). Reason: after S01 and X06 (about 20 minutes each under the load of the host) the `it-run fix` run of H01 waited more than 15 minutes for the global heavy lock behind seven other agents and did not start; it could not be cancelled from this session, so a log `/home/user/logs/ma-H01.log` may appear later: it is NOT a valid result (the mutation is reverted).
- Integration cover of the unit-surviving mutants of `internal/policy` (U001 to U003, U009 to U022, U033 to U036), of the route table (AC rows) and of the handlers' Go-side pre-checks (AC19 to AC25) was NOT RUN end to end; the rows say "unit-only".
- Mutants whose edit did not compile or apply and were not redone: B09, AC17, AC18, AC21, AC25, AC32 (handler details covered only by integration tests anyway).
- M11 freeze and evidence manifest (SQL), M10 `Execute`/gateway outcome paths beyond the network policy and the catalog, redaction (excluded by the brief).

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
| U027 | internal/policy/policy.go:105 | permits: viewers may reconcile commands | go test ./internal/policy (whole package) | SURVIVED | MA-06 (unit); integration NOT RUN |
| U028 | internal/policy/policy.go:114 | permits: members may reconcile workflows | go test ./internal/policy (whole package) | SURVIVED | MA-06 (unit); integration NOT RUN |
| U029 | internal/policy/policy.go:154 | permits: members share/assign records | go test ./internal/policy (whole package) | KILLED | TestRecordAccessAdministrationCapabilities |
| U030 | internal/policy/policy.go:160 | permits: members write metadata | go test ./internal/policy (whole package) | KILLED | TestPolicyMatrix |
| U031 | internal/policy/policy.go:75 | permits: admins write and assign roles | go test ./internal/policy (whole package) | SURVIVED | MA-06 (unit); integration NOT RUN |
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
| D20 | internal/promotion/decisions.go:414 | WorseRollbackClass: returns the better class | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | MA-07 (unit); the integration test TestM13R02* is expected to see it, NOT RUN |
| D21 | internal/promotion/decisions.go:424 | rollbackRank: external effects rank below irreversible data | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | MA-07 (unit) |
| D22 | internal/promotion/decisions.go:102 | MaxRollbackSpan 32 -> 33 | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | MA-07 (unit) |
| D23 | internal/promotion/decisions.go:92 | MaxNoteRunes 280 -> 281 (database says 280) | go test ./internal/promotion (whole package) ; widened: ./internal/promotion | SURVIVED | MA-07 (unit) |
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
| F02 | internal/platform/access/bodybudget.go:71 | BodyHold.Add: the total is exclusive | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' ; widened: ./internal/platform/access | SURVIVED | MA-07 (unit) |
| F03 | internal/platform/access/bodybudget.go:68 | BodyHold.Add: no per-identity share | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F04 | internal/platform/access/bodybudget.go:71 | BodyHold.Add: no total bound | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F05 | internal/platform/access/bodybudget.go:89 | Release: the total is never given back | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F06 | internal/platform/access/bodybudget.go:90 | Release: an identity with nothing left stays listed | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldReleaseIsIdempotentAndForgetsIdentities |
| F07 | internal/platform/access/bodybudget.go:62 | Add: a released hold books again | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldReleaseIsIdempotentAndForgetsIdentities |
| F08 | internal/platform/access/bodybudget.go:76 | Add: the hold forgets what it booked | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBodyHoldBooksPerIdentityAndInAll |
| F09 | internal/platform/access/bodybudget.go:81 | Release: a second release gives the bytes back twice | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' ; widened: ./internal/platform/access | EQUIVALENT | Release zeroes h.n, so a second release subtracts 0 whatever the guard says; the released flag is still tested through F07 |
| F10 | internal/platform/access/bodybudget.go:130 | bookedBody: the bytes that did not fit are reported to the caller | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' | KILLED | TestBookedBodyStopsTheReadThatDoesNotFit |
| F11 | internal/platform/access/bodybudget.go:33 | default identity share below the largest admitted body | go test ./internal/platform/access -run 'Budget\|Body\|Replay\|Deadline' ; widened: ./internal/platform/access | SURVIVED | MA-07 (unit) |
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
| A19 | internal/ai/repair.go:239 | transient failure retried without the one-retry limit | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget\|Hostile\|Adversarial' | KILLED | TestRunPatchProviderFailures |
| A20 | internal/ai/repair.go:53 | estimateTokens rounds down | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | MA-07 (unit) |
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
| G02 | internal/ai/gateway.go:98 | RunPatch: forbidden kind reported as a plain conflict | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget\|Hostile\|Adversarial' ; widened: ./internal/ai | SURVIVED | LOW: a forbidden-kind conflict is reported with the outcome CONFLICT instead of FORBIDDEN_TARGET; the run is still refused |
| G03 | internal/ai/gateway.go:143 | RunPatch: an infrastructure failure becomes NEEDS_HUMAN | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget\|Hostile\|Adversarial' | KILLED | TestAdversarialCorpus |
| N08 | internal/tools/netpolicy.go:165 | DeniedAddress: internal addresses are always allowed | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointResolvingToDeniedAddressIsBlockedWithoutConnecting |
| N09 | internal/tools/netpolicy.go:200 | dialContext: any address may be dialed | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointRejectsAddressesRevalidatedAtConnect |
| N10 | internal/tools/netpolicy.go:217 | dialContext: only the first resolved answer is validated | go test ./internal/tools (whole package) | KILLED | TestOneDeniedAnswerRefusesTheWholeDestination |
| N11 | internal/tools/netpolicy.go:210 | dialContext: no bound on the resolved answers | go test ./internal/tools (whole package) | SURVIVED | LOW: the bound on resolved answers is untested |
| AC01 | internal/platform/access/metadata.go:28 | AcceptsPOST: the AI POST routes are not admitted | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: no unit test pins that the AI POST routes are admitted by AcceptsPOST (the comment at ai.go:36 cites an ai_test.go that does not exist); the integration suite would see every AI POST refused |
| N12 | internal/tools/netpolicy.go:271 | controlContext: the port is not re-validated | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointRejectsAddressesRevalidatedAtConnect |
| N13 | internal/tools/netpolicy.go:271 | controlContext: the literal address is not re-validated | go test ./internal/tools (whole package) | KILLED | TestPublicEndpointRejectsAddressesRevalidatedAtConnect |
| G04 | internal/ai/gateway.go:201 | RunRequirements: NEEDS_HUMAN for any last outcome | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: NEEDS_HUMAN for a requirements run whose last outcome is not a schema error |
| N14 | internal/tools/netpolicy.go:296 | AddressGuard: the denied predicate is not consulted | go test ./internal/tools (whole package) | SURVIVED | MA-08 |
| AC02 | internal/platform/access/ai.go:67 | aiPOST: an unregistered route is admitted | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: no unit test pins aiPOST against the registered routes |
| N15 | internal/tools/netpolicy.go:296 | AddressGuard: any port | go test ./internal/tools (whole package) | SURVIVED | MA-08 |
| N16 | internal/tools/netpolicy.go:90 | classifyAddress: a zoned IPv6 address bypasses the prefix checks | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| AC03 | internal/platform/access/ai.go:49 | aiPatterns: reject needs only ai.read | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: no unit test pins the policy action of the apply, confirm, reject and GET routes (aiPatterns, ai.go:42-50); release routes have such a pin (promote_test.go:67-73) |
| N17 | internal/tools/netpolicy.go:54 | neverV6: only a /64 of the AWS block | go test ./internal/tools (whole package) | KILLED | TestDeniedAddressCorpus |
| AC04 | internal/platform/access/ai.go:43 | aiPatterns: creating a patch proposal needs only ai.read | go test ./internal/platform/access (whole package) | KILLED | TestAICreateRoutesAreExactlyTheProposeRoutes |
| AC05 | internal/platform/access/ai.go:47 | aiPatterns: apply needs ai.propose instead of ai.apply | go test ./internal/platform/access (whole package) | KILLED | TestAICreateRoutesAreExactlyTheProposeRoutes |
| AC06 | internal/platform/access/ai.go:57 | aiCreatePOST: requirement proposals are not repeated after a known abort | go test ./internal/platform/access (whole package) | KILLED | TestAICreateRoutesAreExactlyTheProposeRoutes |
| G05 | internal/ai/gateway.go:110 | RunPatch: a program the compiler returned invalid is accepted | go test ./internal/ai -run 'RunPatch\|RunRequirements\|Charge\|Cancel\|Adapter\|Abandoned\|Label\|Budget' ; widened: ./internal/ai | SURVIVED | LOW: an invalid program is still refused later, by DeriveSensitiveAspects (patch.go:293), with the diagnostic diff_unsupported instead of invalid_program |
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
| P03 | internal/ai/patch.go:118 | apply: definition kind need not match the target | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' ; widened: ./internal/ai | SURVIVED | LOW: redundant with validateOperation (schema.go:482) on every production path (Apply and RunPatch parse the proposal first); a direct ApplyProposal call is unprotected |
| N11b | internal/tools/netpolicy.go:210 | dialContext: no bound on the resolved answers (tests of internal/ai, the AI provider adapter dials through AddressGuard) | go test ./internal/ai -run 'Provider\|HTTP\|Dial\|Guard\|Address\|Rebind\|Config' ; widened: ./internal/ai | SURVIVED | LOW: the bound on resolved answers of the AI adapter is untested |
| P04 | internal/ai/patch.go:121 | apply: the kind of a definition may change | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' ; widened: ./internal/ai | SURVIVED |  |
| X06 | db/migrations/00089_m14_ai_proposals.sql:160 | ai_begin: an expired pending request is not charged its reservation | it-run fix 'TestM14' -v (32 top-level tests passed, canary ok) | KILLED | TestM14APIAdmissionBudgetsAndLimits/a_stale_PENDING_row_expires_and_frees_its_slot |
| P05 | internal/ai/patch.go:124 | apply: redaction placeholders may be written back | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalRefusesRedactionPlaceholders |
| N14b | internal/tools/netpolicy.go:296 | AddressGuard: the denied predicate is not consulted (tests of internal/ai, the AI provider adapter dials through AddressGuard) | go test ./internal/ai -run 'Provider\|HTTP\|Dial\|Guard\|Address\|Rebind\|Config' ; widened: ./internal/ai | SURVIVED | MA-08 |
| P06 | internal/ai/patch.go:127 | apply: schema version mismatch accepted | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalValidatesDefinitionsWithTheMetadataSchema |
| P07 | internal/ai/patch.go:130 | apply: namespace mismatch accepted | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| P08 | internal/ai/patch.go:150 | apply: an add may carry an expected hash | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' ; widened: ./internal/ai | SURVIVED | LOW: redundant with validateOperation (schema.go:462) on every production path |
| P09 | internal/ai/patch.go:152 | apply: an add over an existing id | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| P10 | internal/ai/patch.go:157 | apply: update/delete of a missing definition | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| N15b | internal/tools/netpolicy.go:296 | AddressGuard: any port (tests of internal/ai, the AI provider adapter dials through AddressGuard) | go test ./internal/ai -run 'Provider\|HTTP\|Dial\|Guard\|Address\|Rebind\|Config' ; widened: ./internal/ai | SURVIVED | MA-08 |
| P11 | internal/ai/patch.go:159 | apply: the per-target expected-hash compare-and-set is skipped | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| P12 | internal/ai/patch.go:99 | apply: two operations on one target | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalDuplicateTargets |
| P13 | internal/ai/patch.go:60 | apply: operation bound is exclusive | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| P14 | internal/ai/patch.go:104 | apply: any target kind | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalRefusesEveryNonAllowlistedKind |
| P15 | internal/ai/patch.go:280 | sensitive aspects: financial rule changes need no acknowledgement | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| P16 | internal/ai/patch.go:280 | sensitive aspects: unclassified changes need no acknowledgement | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestSensitiveAspectVocabulary |
| P17 | internal/ai/patch.go:94 | apply: a delete carrying a definition | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestApplyProposalMalformedProposalsAreErrors |
| K01 | internal/ai/kinds.go:11 | allowlist: workflow dropped (neutral, tightening) | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| K02 | internal/ai/kinds.go:11 | allowlist: tool added | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAdversarialCorpus |
| AC26 | internal/platform/access/access.go:184 | extendWriteDeadline: the extension is a hundredth of the asked time | go test ./internal/platform/access (whole package) | KILLED | TestExtendWriteDeadlineLetsASlowResponseFinish |
| AC27 | internal/platform/access/access.go:427 | serve: the write deadline of an AI creation is never extended | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the integration deadline tests (m14_api_deadline_test.go) are expected to see it; NOT RUN |
| AC28 | internal/platform/access/access.go:276 | serve: an AI creation is never repeated after a known abort | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the retry of an AI creation after a known abort is seen by the integration suite only; NOT RUN |
| K03 | internal/ai/kinds.go:11 | allowlist: role added | go test ./internal/ai -run 'Apply\|Patch\|Allowlist\|Sensitive\|Kind\|Hostile\|Adversarial\|Corpus\|Swap' | KILLED | TestAllowlistCoversEveryDefinitionKind |
| AC29 | internal/platform/access/access.go:723 | enforce: an unknown obligation does not deny | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: enforce() fail-closed on unknown obligations is covered by TestM1AuditAndObligationsFailClosed (integration); NOT RUN |
| AC30 | internal/platform/access/access.go:727 | enforce: a decision without id or version still allows | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: enforce() fail-closed on a decision without id/version is covered by TestM1AuditAndObligationsFailClosed (integration); NOT RUN |
| AC31 | internal/platform/access/access.go:548 | perform: a metadata POST is authorized as a read | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the metadata route to action mapping is covered by the integration suite; NOT RUN |
| SC01 | internal/ai/schema.go:64 | serverField: no key normalisation | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestParsePatchProposalNamesServerDerivedFields |
| AC33 | internal/platform/access/access.go:582 | perform: the AI routes are authorized as preview reads | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the AI route to action mapping; NOT RUN end to end |
| AC34 | internal/platform/access/access.go:579 | perform: the release routes are authorized as preview reads | go test ./internal/platform/access (whole package) | SURVIVED | unit-only: the release route to action mapping; NOT RUN end to end |
| SC02 | internal/ai/schema.go:312 | cleanText: word joiner accepted | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' ; widened: ./internal/ai | SURVIVED | MA-10 |
| SC03 | internal/ai/schema.go:312 | cleanText: mongolian vowel separator accepted | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' ; widened: ./internal/ai | SURVIVED | MA-10 |
| AU01 | internal/platform/access/promote.go:162 | authority.required: the bundle may not raise the quorum above the database floor | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 (MEDIUM): the stricter-than-floor quorum is enforced by Go alone and untested |
| SC04 | internal/ai/schema.go:314 | cleanText: tag characters accepted | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestParsePatchProposalRejects |
| AU02 | internal/platform/access/promote.go:170 | authority.excluded: the roles the bundle excludes are ignored (floor roles only) | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 (MEDIUM): the roles a bundle excludes beyond the floor are enforced by Go alone and untested |
| AU03 | internal/platform/access/promote.go:207 | authority.bars: party ids compared case-sensitively | go test ./internal/platform/access (whole package) | EQUIVALENT | tenant.ValidID refuses an upper-case actor before the comparison and party ids are canonical, so == and EqualFold agree |
| AU04 | internal/platform/access/promote.go:207 | authority.bars: an unknown party no longer bars everyone | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 |
| AU05 | internal/platform/access/promote.go:202 | authority.bars: a malformed actor id is not barred | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 |
| AU06 | internal/platform/access/promote.go:191 | authority.party: the acceptance author is the requester | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 |
| AU07 | internal/platform/access/promote.go:193 | authority.party: the sandbox freezer is the acceptance author | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 |
| AU08 | internal/platform/access/promote.go:222 | authority.eligible: a repeated approver counts twice | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 (LOW): the approvals primary key already forbids a repeated approver |
| AU09 | internal/platform/access/promote.go:226 | authority.eligible: barred approvers are eligible | go test ./internal/platform/access (whole package) | SURVIVED | MA-09 |
| SC05 | internal/ai/schema.go:310 | cleanText: bidi isolates accepted | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' ; widened: ./internal/ai | SURVIVED | MA-10 |
| SC06 | internal/ai/schema.go:308 | cleanText: C1 controls accepted | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' ; widened: ./internal/ai | SURVIVED | MA-10 |
| SC07 | internal/ai/schema.go:289 | checkText: keys of free-form maps are not checked | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' ; widened: ./internal/ai | SURVIVED | LOW: the keys of free-form maps; no killing test written |
| SC08 | internal/ai/schema.go:476 | validateOperation: a tool payload under another kind | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestParsePatchProposalRejects |
| SC09 | internal/ai/schema.go:464 | validateOperation: update/delete without an expected hash | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestParsePatchProposalRejects |
| SC10 | internal/ai/schema.go:395 | validatePatchProposal: an empty proposal | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestAdversarialCorpus |
| SC11 | internal/ai/schema.go:326 | textOK: text hygiene dropped | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' ; widened: ./internal/ai | EQUIVALENT | checkText applies cleanText to every string value of the tree before textOK runs (schema.go:109, 277-279), so the second application changes nothing |
| SC12 | internal/ai/schema.go:326 | textOK: bound off by one | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestAdversarialCorpus |
| SC13 | internal/ai/schema.go:109 | decodeStrict: text hygiene not applied to the tree | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestParsePatchProposalRejects |
| SC14 | internal/ai/schema.go:106 | decodeStrict: required keys not required | go test ./internal/ai -run 'Schema\|Parse\|Strict\|Hostile\|Adversarial\|Corpus\|Text\|Proposal' | KILLED | TestParsePatchProposalRejects |
| SV01 | internal/ai/service.go:57 | Effective: attempts ceiling dropped | go test ./internal/ai -run 'Limits\|Effective\|Service\|Config' ; widened: ./internal/ai | SURVIVED | MA-11 |
| SV02 | internal/ai/service.go:63 | Effective: output token ceiling dropped | go test ./internal/ai -run 'Limits\|Effective\|Service\|Config' ; widened: ./internal/ai | SURVIVED | MA-11 |
| SV03 | internal/ai/service.go:69 | Effective: in-flight ceiling dropped | go test ./internal/ai -run 'Limits\|Effective\|Service\|Config' ; widened: ./internal/ai | SURVIVED | MA-11 |
| SV04 | internal/ai/service.go:75 | Effective: daily token ceiling dropped | go test ./internal/ai -run 'Limits\|Effective\|Service\|Config' ; widened: ./internal/ai | SURVIVED | MA-11 |
| SV05 | internal/ai/service.go:231 | reserveFor: the output is not reserved | go test ./internal/ai -run 'Limits\|Effective\|Service\|Reserve\|Config' ; widened: ./internal/ai | SURVIVED |  |
| SV06 | internal/ai/service.go:232 | reserveFor: one attempt reserved for several | go test ./internal/ai -run 'Limits\|Effective\|Service\|Reserve\|Config' ; widened: ./internal/ai | SURVIVED |  |
