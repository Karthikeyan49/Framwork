# M12 deterministic logic — independent adversarial review

## Progress

- [x] Read ADR038, M12 design appendix, V3 architecture 12/13/19, AGENTS.md, two-agent protocol.
- [x] Code read: intent (requirements, acceptance, oracle), verification, risk, compiler diff/classify,
      promotion bundle2/facts/dependencies/staleness, runtime workflow/rules semantics the verifier relies on.
- [x] Coordinated forgery against `promotion.Restore` — F1 CONFIRMED (HIGH).
- [~] Byte-level leaf sweep of three real BundleV2 candidates (first, second, tool release): run complete,
      accepted mutations being classified.
- [ ] Randomised mutation testing of `verification.Verify` against an independent oracle.
- [ ] Risk monotonicity property test, diff/storage-class gaps, staleness reachability.
- [ ] Patch/rebase, requirement floors, oracle/probe edge cases.
- [ ] Contract honesty table (ADR038 §3, §4, §7, §8, Amendments 1–3, 5).

## Scope and commit

Reviewed commit `fb52ad3` ("docs(m12): record full integration result and decider wording"),
branch `review/m12-verifier-final`. Pure packages only; no database was used.

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

M12 verifier review STATUS: IN PROGRESS
