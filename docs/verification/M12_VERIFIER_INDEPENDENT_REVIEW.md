# M12 deterministic logic — independent adversarial review

## Progress

- [x] Read ADR038, M12 design appendix, V3 architecture 12/13/19, AGENTS.md, two-agent protocol.
- [x] Code read: intent (requirements, acceptance, oracle), verification, risk, compiler diff/classify,
      promotion bundle2/facts/dependencies/staleness, runtime workflow/rules semantics the verifier relies on.
- [x] Coordinated forgery against `promotion.Restore` — F1 CONFIRMED (HIGH).
- [x] Byte-level leaf sweep of three real BundleV2 candidates — classified (F6 LOW, rest inert).
- [x] Randomised mutation testing of `verification.Verify` (508 mutants, 3 currencies) — F2, F3, F4, F5.
- [x] Risk monotonicity property test (held), diff/storage-class gaps (F7, F8), staleness reachability (held).
- [x] Patch/rebase (code review, held), requirement floors and oracle/probe edge cases (held).
- [x] Contract honesty table (ADR038 §3, §4, §7, §8, Amendments 1–3, 5).
- Done. Final status at the end of this file.

## Scope and commit

Reviewed commit `fb52ad3` ("docs(m12): record full integration result and decider wording"),
branch `review/m12-verifier-final`. Pure packages only; no database was used. In scope: `internal/intent`
(v2 requirements, acceptance, oracle, probes), `internal/verification`, `internal/risk`, `internal/compiler`
(`diff.go`, `classify.go`), `internal/metadata/patch.go`, `internal/promotion` (bundle 2.0.0, facts,
dependencies, staleness), `internal/erp` Money, and the runtime workflow/rules semantics the verifier
replays.

## Method

1. Read the contracts (ADR038 with amendments, design appendix, V3 12/13/19, M12 implementation report,
   promotion README) and every in-scope source file, and wrote down the claims to attack.
2. **Restore tamper.** A byte-level JSON scanner found every scalar of three real candidates built with the
   package's own helpers (first release, second release with a base, tool release). It replaced each scalar
   with up to six variants (append, empty, hex flip, case change, `\u` escape, ±1, zero, number form,
   boolean flip) and kept every other byte. It recomputed the outer hash and called `promotion.Restore`:
   4125 mutations. Separately, coordinated forgeries edit the diff or the storage delta and then recompute
   everything a forger can (facts, assessment, `HighRisk`, verifier report). Encoding attacks cover
   duplicate keys, whitespace, trailing data, escaped/cased keys, NUL, 200k-deep nesting and oversize.
3. **Verifier mutation testing.** A generator builds purchase-order applications from nine knobs (route
   operator, threshold literal including ±1 minor unit, amount-field currency, approval role set, an extra
   conjunct/disjunct on a second field, a vip bypass branch, a swapped route, the `approved` and `rejected`
   targets). It produces every one-step mutant of the correct app plus 150 random multi-knob mutants, for
   requirements `GT 500` in USD (2 minor digits), JPY (0) and KWD (3): 508 distinct compiled applications.
   An independent oracle written from the requirement text (approval by exactly `admin` iff
   amount > 500, and an approval must gate completion) judges each mutant over an 11-amount × 4-note grid
   of records. That verdict is compared with `verification.Verify` using the derived probes plus a
   realistic independent spec (750 → approval, 100 → none).
4. **Risk.** A property test generated 20,000 random `Facts`, each grown by one more or more severe change:
   level and BLOCKED never decrease by rank, separation never weakens, output is independent of order,
   the assessment verifies, and unknown policy versions and vocabularies fail closed.
5. **Staleness, diff, rebase, floors, oracle.** A table test over each `CurrentState`/`BoundState`
   input; `ReduceFacts` unit probes; a compiler probe for kind/payload mismatch; parser and probe tables.

## Commands

| Command | Result | Duration |
|---|---|---|
| `./scripts/go.sh test -count=1 -skip TestZZReview ./internal/{intent,verification,risk,compiler,metadata,promotion,erp}/` | PASS (all 7 packages) | 13.9 s |
| `go test -run TestZZReviewLeafSweep -v ./internal/promotion/` | PASS (harness; 152/4125 accepted, classified in F6) | 204 s |
| `go test -run 'TestZZReviewCoordinatedDiffForgery\|TestZZReviewStorageClassForgery' ./internal/promotion/` | forgeries ACCEPTED by `Restore` (F1) | 1.1 s |
| `go test -run TestZZReviewEncodingTamper ./internal/promotion/` | PASS (every encoding tamper rejected) | 0.5 s |
| `go test -run TestZZReviewRandomisedMutants -v ./internal/verification/` | 481 wrong→FAIL, 24 wrong→PASS (F2–F5), 3 correct→PASS, 0 correct rejected | 89 s |
| `go test -run TestZZReviewGateRejectedBranch ./internal/verification/` | gate with `rejected→complete` PASSes (F2) | 0.3 s |
| `go test -run TestZZReviewKindPayloadMismatch ./internal/verification/` | PASS (all six kind/payload mismatches refused by the compiler) | 0.3 s |
| `go test -run TestZZReviewMonotoneAndDeterministic ./internal/risk/` | PASS (16,392 pairs, 0 rank violations) | 3.9 s |
| `go test -run TestZZReviewStalenessReachability ./internal/promotion/` | PASS (21 cases, each exact reason) | 0.4 s |
| `go test -run TestZZReviewPlanOnlyWorkflowDoesNotInheritTool ./internal/promotion/` | MEDIUM without tool fact (F7) | 0.1 s |
| `go test -run TestZZReviewOracleAndParserEdges ./internal/intent/` | PASS | 0.0 s |
| Database / integration tests | NOT RUN (out of scope, no database needed) | — |

Committed harnesses: `internal/risk/zz_review_harness_test.go` and `internal/intent/zz_review_harness_test.go`
(deterministic, < 4 s, PASS on `fb52ad3`). Not committed: the promotion harness (leaf sweep 204 s; forgery
tests document a defect) and the verifier harness (89 s; it documents accepted wrong applications). Their
repro snippets are in the findings below.

## Findings

### F1 — HIGH — `Restore` accepts a forged diff and storage class, so forged risk restores and the requester can self-approve

- Where: `internal/promotion/bundle2.go:528-550` (diff and storage are only checked structurally:
  `ParseDiff` canonical form, `TargetIRHash`, first-release flag, `BaseIRHash` presence; storage class only
  as `EQUAL iff BaseSchemaHash == TargetSchemaHash`; the evolution plan's `Class` and the delta's
  `Reasons` are never compared) and `bundle2.go:612-625` (facts and risk are then recomputed *from the
  forged diff/delta*, so they agree with the forgery). The full recompute exists only in
  `Candidate.VerifyAgainstBase` (`bundle2.go:644`), which no production code calls
  (`grep -rn VerifyAgainstBase` finds only tests).
- Contract claims falsified: ADR038 Amendment 2 ("2.0.0 bodies ... are fully recomputed (diff, storage
  class, facts, risk, ...)"), `internal/promotion/README.md:10` ("`Restore` recomputes the diff, storage
  class, risk facts ..."), ADR038 §6 ("`Restore` recomputation at approval is the backstop") for a
  compromised API process staging a self-consistent forged body; `control.stage_candidate`
  (`db/migrations/00048_m12_candidate_builder.sql:123-127`) only checks `HighRisk` against `Risk.Level`.
- Reproduction (harness `TestZZReviewCoordinatedDiffForgery`, `TestZZReviewStorageClassForgery`): build the
  second-release candidate of `bundle2_test.go` (threshold 500→600, honest level HIGH,
  `ApprovalGateChanged`), then either replace `Diff` by `ComputeDiff(target, target)` or delete the
  `APPROVAL_GATE` aspect, recompute `ReduceFacts`/`risk.Evaluate`/`HighRisk` and the (unchanged) verifier
  report, re-encode, re-hash:

  | Forgery | Restore | Level | HighRisk | `Approve(..., requester, ...)` | `VerifyAgainstBase` |
  |---|---|---|---|---|---|
  | diff of target against itself | accepted | HIGH → LOW | true → false | allowed | rejects |
  | drop `APPROVAL_GATE` aspect | accepted | HIGH → MEDIUM | true → false | allowed | rejects |
  | TRANSFORM (required field + backfill) relabelled ADDITIVE | accepted | CRITICAL → HIGH, separation 2 approvals (freezer excluded) → 1 | stays true | — | rejects |

- Impact: the bundle hash, the stage port and `Restore` all accept a candidate whose risk level,
  `HighRisk`, separation requirement and requirement-completeness (`ChangedFor` is derived from the same
  forged diff, so `OMITTED_REQUIREMENT` can be hidden too) were lowered. `promotion.Approve`
  (`bundle.go:173`) and migration 11's `approval_insert` read `HighRisk`; with the forgery the requester
  approves their own HIGH change.
- Recommended fix: make the base part of the restore contract — either `Restore` takes the base program
  (resolved from `BaseRelease`) and runs `VerifyAgainstBase`, or the approval/activation ports must call
  `VerifyAgainstBase` and the ADR/README must say `Restore` alone does not recompute diff/storage. In any
  case compare the evolution artifact's `Class`/operations with `Storage.Class`/`Reasons` inside
  `restoreV2`, and bind `Diff.BaseIRHash` to the base release's IR hash in the stage port (the DB knows the
  active base bundle).
- Status note: found on `fb52ad3`. The coordinator reports it fixed on `claude/m12-candidate-builder`
  (`Store.LoadCandidate` runs `Candidate.VerifyWithBase`; regression tests
  `internal/promotion/verify_base_test.go`, `TestM12CoordinatedForgeryIsRefusedWhenLoaded`). This review
  did not re-verify that fix.

### F2 — HIGH — an approval whose `rejected` edge also completes verifies as an approval (threshold and gate)

- Where: `internal/verification/verifier.go:330-332` (the threshold walk returns `APPROVAL_REQUIRED(roles)`
  as soon as it *enters* an approval state and never follows its `approved`/`rejected` edges) and
  `verifier.go:380-381` (the gate walk treats a matching approval state as gated, `answer = false`,
  without looking at where `rejected` goes).
- Minimal failing input: the purchase-order workflow of `verifier_test.go` with
  `state.approval: approved -> state.complete, rejected -> state.complete` (plus any branch that keeps
  `state.failed` reachable, which `validatePlan` requires). Requirement `GT 500.00 USD -> [admin]`:
  `Verify` = PASS for USD, JPY and KWD (`TestZZReviewRandomisedMutants`, 5 mutants). Requirement
  `approval_gate {workflow.order.approve, [admin]}` on an unconditional gate with
  `rejected -> state.complete`: PASS (`TestZZReviewGateRejectedBranch`).
- Impact: the verifier certifies "requires approval by admin" for an application in which an admin
  *rejection* ends in the same `SUCCEEDED` completion as an approval, so the approval decides nothing. The
  runtime's tool ledger still refuses a WRITE/`Approval: REQUIRED` tool without an `approve` signal by
  another actor (`00046_m11_sandbox.sql` `tool_begin`, `TL001`), so external writes on the rejected path
  are contained; workflow outcome, local activities and READ tools are not. (The mirror mutant
  `approved -> state.failed` also PASSes: 4 mutants; fail-closed, LOW on its own.)
- Recommended fix: a matching approval gates only through its `approved` edges. In the threshold walk
  continue past the approval along `rejected` and FAIL if `complete` (or any activity) is reachable; in
  the gate walk require every `rejected` path from a gating approval to end in `failed`.

### F3 — HIGH — derived probes never leave the threshold neighbourhood: an upper cap passes

- Where: `internal/intent/oracle.go:154-161` (probes: t−1u, t, t+1u, 0, −1u, wrong currency only) and
  `verifier.go:249-260`.
- Minimal failing input: route condition `amount > 500 AND amount <= 1000000` (same currency, roles
  `[admin]`, `rejected -> failed`) with requirement `GT 500 -> [admin]` and a realistic independent spec
  (cases 750 → approval, 100 → none). `Verify` = PASS in USD, JPY and KWD; the oracle requires approval at
  `1000000.01` USD / `1000001` JPY / `1000000.001` KWD and the application routes those to completion.
- Impact: the largest orders bypass approval while the candidate carries a PASS report. The probes are
  the mechanical floor ADR038 §4 relies on ("human cases may add but never remove probes"); nothing
  mechanical tests far from the boundary. Recommended fix: add probes at the maximum `erp.Money` amount
  (10^18) and at several decades above (and, for LT/LTE, below) the threshold, or decide the route
  symbolically over the money field.

### F4 — MEDIUM — probes run in the all-null world of unconstrained optional fields

- Where: `verifier.go:299-342` (values = context + threshold field only; a condition that reads any other
  non-required field sees null, and `eq`/`neq`/`is_null`/`coalesce` accept null without error,
  `internal/runtime/rules/eval.go:77-84,140-158`).
- Minimal failing input: requirement without context, route `amount > 500 AND note != "vip"` or
  `amount > 500 AND is_null(note)`: PASS in all three currencies (`TestZZReviewRandomisedMutants`,
  7 mutants). At run time a 500.01 USD order with `note = "vip"` (or any non-null note for `is_null`) skips
  approval. With the context `note: "regular"` the `!= "vip"` exemption still passes.
- Impact and documentation: the authors know the class (`verifier_test.go:286-296`
  `TestExamplesCannotSeeBypassesTheyDoNotTry`), but ADR038, the design appendix and the M12 report state
  no such limit, and the verifier's own BLOCKED message (`verifier.go:338`) tells the author to add the
  fields the route reads to the context, which turns "all records" into one sampled record. Recommended:
  BLOCK (`UNSUPPORTED_BY_VERIFIER`) when a condition on a path to the approval decision reads a field
  that is neither the threshold field nor fixed by the context, or probe each such field with null and
  representative non-null values; document the limit in ADR038 §4.

### F5 — LOW — over-approval passes

`amount > 500 OR note == "x"` PASSes (3 mutants): a −0.01 USD order with `note = "x"` needs approval
although the requirement says none. Same cause as F4. Fail-safe for money, wrong for the requirement.

### F6 — LOW — candidate bytes are malleable inside `Source.JSON`

Leaf sweep (`TestZZReviewLeafSweep`; 4125 single-leaf mutations over a first release, a second release and
a tool release, outer hash recomputed): 152 accepted. 146 are re-encodings inside `Source.JSON` (`\u00XX`
escapes of a letter, `30000` → `30000.0`). `v.Parse` normalizes the source and `Source.Hash` covers only the
normalized form (`bundle2.go:503-505`), while the outer canonical check keeps RawMessage bytes. Every
decision reads decoded content, so the accepted bundles are semantically identical. However, one
candidate has many valid hashes, and approval binds "the exact hash". Fix: require `Source.JSON` to be
byte-equal to the normalized source.

The other 6 accepted mutations are DB/staleness-bound fields that `Restore` cannot check by design:
`BaseRelease` (hex flip), `BaseGeneration` (+1) and `Requirements.Version` (±1). The stage port binds
them (`00048…sql:136-157`, rows looked up by version and hash) and `Staleness` reports
`BASE_CHANGED`/`BASE_GENERATION_ADVANCED`. They are inert for `Restore`. They also show that `Diff.BaseIRHash`
is not tied to `BaseRelease` anywhere (see F1).

### F7 — MEDIUM — a PLAN_ONLY change of a workflow that contains a tool does not inherit the tool's level (S9b)

- Where: `internal/promotion/facts.go:96-99` (`break` before the workflow's `c.Tools` are collected into
  `changedWorkflowTools`).
- Reproduction (`TestZZReviewPlanOnlyWorkflowDoesNotInheritTool`): a diff with one `workflow` change whose
  aspects are `[PLAN_ONLY]` and whose `Tools` list a pinned WRITE tool → facts have no tool, level MEDIUM
  (`S11`). The same change with `[WORKFLOW_GRAPH]` → inherited tool fact (`S9b.*`).
- Impact: appendix D S9b says "any change to a workflow containing a tool inherits the tool's level". A
  compiler/catalog upgrade that recompiles the conditions in front of a WRITE tool (identical authoring,
  different plan) rates MEDIUM (no separation of duties) instead of HIGH. Fix: collect `c.Tools` before the
  PLAN_ONLY shortcut.

### F8 — LOW — documentation and dead-code gaps

- `UNSUPPORTED_BY_RUNTIME` (`report.go:39`) is never emitted and the verifier never reads `Plan.Trigger`.
  ADR038 §4 lists "unsupported trigger" as BLOCKED. That is vacuous today, because `validatePlan` admits
  only built-in roles and three triggers. However, an approval workflow with trigger `manual` verifies
  although nothing forces it to run for a record.
- `verification.ChangedSurfaces.ToolRefs` is documented as including "inherited" activities
  (`verifier.go:37`). `promotion.ChangedFor` (`facts.go:229-231`) adds only added/removed/re-pinned
  activities, so a changed workflow around an unchanged WRITE tool needs no `tool_effect` requirement.
- `risk.Facts.UnknownCapability` and rule `BLOCK.unknown_capability` are never set by `ReduceFacts`
  (the compiler already refuses unknown capabilities, so this is vacuous).
- When the storage cell is BLOCKED (BREAKING), `Assessment.Level` can be lower than for the same facts with
  TRANSFORM, e.g. CRITICAL → HIGH. The policy is monotone by rank (BLOCKED above CRITICAL), and a blocked
  assessment is never staged or restored, but the reported level and separation are not.
- `RuntimeContract.Components` is only self-hashed (`bundle2.go:507`); nothing compares it with the running
  binary's `RuntimeComponents()`. `BoundState.RiskPolicyVersion` is unused by `Staleness`. Both are inert
  because `Restore` re-checks versions.

## Verified (attacks that held)

- **Restore, single-field tamper**: of 4125 single-leaf mutations across three candidates, everything except
  F6's inert set is refused. That includes every hash, level, reason, separation, rollback, facts flag,
  report byte, requirement/acceptance byte, tool pin/definition, schema hash, identity and version field.
  Duplicate keys, inner whitespace, leading/trailing bytes, `\u`-escaped or cased member names, NUL in an
  id, 200k-deep nesting and > 8 MiB are all refused (`TestZZReviewEncodingTamper`).
- **Delimiter injection**: `DefinitionKey` (`ref|version|hash`) cannot collide with a valid key because
  validated refs/versions/hashes contain no `|`, so a forged component would add a third separator.
  `ToolsHash`, `PolicyBundleHash` and the components hash are JSON encodings of structs. The diff's
  `fmt.Sprint` transition list cannot alias, because every element is a whole JSON object. Read-checked
  only, not fuzzed.
- **Verifier, money path**: every wrong operator (GT/GTE/LT/LTE/EQ/NEQ), threshold off by one minor unit
  either way (USD, JPY, KWD), ×10 and constant thresholds, wrong field currency, role subset/superset/other
  role, swapped route and vip bypass FAIL with `APPROVAL_MISMATCH` in all three currencies. That is 481 of
  the 505 wrong mutants. No correct application was rejected.
- **Approval timeouts** cannot bypass: the runtime turns an approval timeout into `TIMED_OUT`
  compensation, not a transition (`execution.go:101-107`). An unreachable approval or state is refused by
  `validatePlan`. Multiple start transitions are first-match in both the verifier and the runtime (same
  `Program.Next`).
- **Oracle and probes**: probes straddle t−1u/t/t+1u for 0, 2 and 3 minor digits, including negative and
  maximum (10^18) thresholds. Exponent, leading-zero, locale, `+`, trailing-dot and full-width amounts are
  rejected. Human case ids cannot contain `/`, so they cannot shadow `…/probe/…` ids. Oracle-contradicting
  cases are re-checked at verify time.
- **Requirement parsing and floors**: duplicate, cased and unknown keys, cased categories and
  requiredness, unsorted or duplicate roles, a Cyrillic look-alike id and trailing data are rejected. A floor
  category marked OPTIONAL or RECOMMENDED, or POLICY_FLOOR claimed for an alias category, is a
  `FLOOR_VIOLATION`. OPEN/REJECTED mandatory requirements BLOCK (existing tests). Completeness matches
  exact entity, workflow and tool ids.
- **Risk**: monotone by rank over 16,392 random growth pairs; order-independent; self-verifying; unknown
  policy → `ErrUnknownPolicy`; unknown class, volume or effect → error, never a default level. The compiler
  rejects a workflow payload under any other kind, so `ReduceFacts`' switch on `Kind` cannot be steered to
  `page`. Every `metadata.Field` attribute is part of `StorageField`, so field edits always surface as
  non-EQUAL storage.
- **Staleness**: each of the 10 reasons is reachable alone and exact. A nil `ToolStatus`, an unknown
  status, a zero `CurrentState`, a zero bound level, an empty volume, a case-differing hash, a lower
  generation and a nil IR list all read STALE (`TestZZReviewStalenessReachability`).
- **Rebase** (code review against the existing `patch_test.go`): the class is derived from every side's
  `Kind` (non-page means `SENSITIVE_CHANGE`), a declared class must equal the derived one, a convergent
  add/update/delete is a no-op, a second application is idempotent, output is sorted, and the merged
  document is re-validated. No property test was written for it.

## Contract honesty (ADR038)

| Clause | Verdict | Evidence |
|---|---|---|
| §3 server-derived, monotone, hashed risk; `Restore` re-runs the named policy | VERIFIED for the policy (property test, registry fail-closed); FALSIFIED for the backstop, because facts come from an unrecomputed diff/storage delta (F1); S9b inheritance FALSIFIED for PLAN_ONLY (F7) | F1, F7 |
| §4 requirements, floors, oracle never reads IR, probes, deterministic verifier | VERIFIED for floors, parsing, oracle purity, probe placement and determinism; FALSIFIED as completeness evidence for approval semantics (F2) and far-from-threshold behaviour (F3); limit undocumented (F4); "unsupported trigger → BLOCKED" vacuous (F8) | F2–F5, F8 |
| §7 staleness derived, sorted, never stored | VERIFIED | `TestZZReviewStalenessReachability` |
| §8 rebase derives class, three-way, typed conflicts, never auto-applies non-page | VERIFIED by code review and existing tests; no new property test | `patch.go:116-176` |
| Amendment 1 default volume | VERIFIED (`DefaultVolume`; later releases UNVERIFIED in the second-release fixture) | `bundle2.go:271` |
| Amendment 2 one strict restore path, "fully recomputed (diff, storage class, …)" | FALSIFIED for diff and storage class (F1); VERIFIED for unknown fields, canonical form, report and risk recomputation from stored facts | F1, F6 |
| Amendment 3 approver roles by set equality | VERIFIED (role subset, superset and other-role mutants FAIL) | mutation harness |
| Amendment 5 first release excluded from `NonEqualStorage` | VERIFIED (`ChangedFor`: `!s.FirstRelease`) | `facts.go:220` |

## Not covered

- Database paths (`stage_candidate`, RLS, the integration flow) and the API layer. They were out of scope
  for this pure-logic review.
- The coordinator-reported F1 fix on `claude/m12-candidate-builder` (`VerifyWithBase` in
  `Store.LoadCandidate`) was not re-verified.
- No fuzzing of `ParseDiff`, `ClassifyStorage` or `PlanEvolution` beyond the fixtures. Relationship
  cardinality, rename vs drop+add and default changes were read-checked only (all reach non-EQUAL storage
  or UNCLASSIFIED/MEDIUM through `contrast`).
- Tool-effect requirement mutants beyond the existing tests; multi-workflow interactions (a second workflow
  on the same entity performing effects outside the verified workflow); tiered approvals with two
  sequential approval states; deep expression trees.
- No property test for rebase merge symmetry; the leaf sweep did not mutate array lengths or object
  membership. Those are covered only by the strict-decode and canonical-form checks and the existing tamper
  tests.

M12 verifier review STATUS: FAIL
