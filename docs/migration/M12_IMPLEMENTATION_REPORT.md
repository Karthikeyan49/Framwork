# M12 Candidate Release Builder — implementation report

Date: 15 October 2026 (UTC). Source: `claude/m12-candidate-builder` (based on
`claude/m10-m11-tools-sandbox` at `633d44b`). Decisions, amendments and
recovery are in [ADR038](../adr/038-m12-candidate-release-builder.md) and the
[design appendix](M12_DESIGN_APPENDIX.md). M13 (approval, activation, production
volume) and M14 (AI capture) are not started. This is a development/test preview
capability with no production exposure; it is not production ready.

## What changed

Before M12, nothing built a release candidate: `Verification.Outcome`,
`HighRisk`, acceptance provenance, dependency hashes and the sandbox context were
caller-declared, and `framework_app` could insert any hash-consistent body into
`control.release_bundles`. A candidate is now **bundle 2.0.0, assembled only on the
server**, and the database refuses every other writer.

- `internal/compiler`: `ComputeDiff` (typed definition/tool/gate/money/trigger
  changes with stable hashes) and `ClassifyStorage` (EQUAL, ADDITIVE, TRANSFORM,
  BREAKING with reasons and evolution-plan hash).
- `internal/risk`: `release-risk-v1`, a data-driven, hashed, monotone policy over
  server-derived facts, with BLOCKED verdicts, separation and rollback classes;
  a registry that re-evaluates a stored bundle under the policy version it names.
- `internal/intent`: `requirements-v2` and `acceptance-v2` (typed, canonical,
  strictly parsed), requiredness floors (`requirement-policy-v1`), validation with
  hash binding and coverage, an exact money oracle that never reads the IR, and
  mechanically derived boundary probes.
- `internal/verification`: deterministic `verifier-v2` over the stored IR bytes
  (compiled rules and workflow `Next`; no clock, network, database or compile).
  PASS, FAIL (behaviour differs) or BLOCKED (cannot be verified, including an
  omitted requirement, an open mandatory requirement or an unsupported category).
- `internal/metadata`: three-way `Rebase` that derives the change class itself,
  merges convergent changes, reports typed conflicts and never auto-applies
  non-page definitions.
- `internal/promotion`: bundle 2.0.0 build/restore with full recomputation; derived
  tool pins, whole-tenant policy bundle hash and runtime components hash;
  derived staleness; release store; version dispatch in `Restore` (1.0.0 stays
  readable but can no longer be staged).
- Migration 48: `control.requirement_sets`, `acceptance_specs`, `release_candidates`
  (forced RLS, immutability and TRUNCATE guards); definer ports
  `append_requirements`, `author_acceptance` and `stage_candidate`; `INSERT` on
  `release_bundles` revoked from `framework_app`; worker drift guard extended.
  `stage_candidate` re-derives what the database knows: tenant/app, requester =
  authenticated actor, risk/HighRisk consistency, verification PASS, source
  revision and hash, requirement/acceptance rows and DB authors, frozen sandbox
  evidence and freezer, acceptance separation of duties, and the current active
  base.
- API (`internal/platform/access/releases.go`): preview-gated
  `POST|GET .../requirements`, `.../acceptance-specs`, `.../candidates`. Requests
  naming authority fields fail with 400; verifier/risk rejections return 422 with
  the report and are audited. Native policy `m12-native-v9`.

No new Go or npm dependency. No UI change.

## Commands executed and results

| Command | Result |
|---|---|
| Unit packages per slice (`risk`, `compiler`, `metadata`, `intent`, `verification`, `promotion`, `policy`) | PASS (golden risk pins, monotonicity property test, ~50 bundle tamper mutations, verifier mutation fixtures, oracle currency tables) |
| `go vet -tags=integration ./tests/integration` | PASS after porting the M6R release test |
| `-run TestM12` (5 tests), early runs | FAIL: release routes were registered but not dispatched; replay and rejection audits were miscounted by the test; `stage_candidate` did not bind the sandbox freezer (real defect, fixed in migration 48); the worker-guard check ran as the wrong role. Each fixed, then 5/5 PASS (25.4 s) |
| `make check` (docs checksums, format, vet, `mod verify`) | PASS for docs/format/vet/mod verify; web lint needs `node_modules` in this worktree (symlinked afterwards) |
| Go unit tests `./cmd/... ./internal/... ./tests/...` with `-race` | PASS |
| Full `go test -race -tags=integration ./tests/integration` (978.6 s) | One FAIL of the whole suite: `TestM9RRecordAccessCanonicalDatasetGeneration` (stale policy-version assertion in a binary built before `bf70752` was merged); the merged test plus `TestM12*` then PASS (31.7 s). A final full run on the review-fixed tree is still required |
| `make build` | PASS |
| `make test-e2e` | PASS, 5 tests (20.8 s) |
| Migrations 1–48 applied to a freshly created isolated database (new compose project, empty volume) | PASS — confirms the committed migration 48 (including the sandbox-freezer check added after the first local apply) applies from scratch; TestM12*, TestM8* (except the Redis-restart test, which is coupled to the main compose project) and TestM11* then PASS on it (147.9 s) |

Evidence gap: the pre-fix exploit (a hand-built PASS bundle inserted by
`framework_app`) was removed with the M6R test that exercised it; it was not
re-run against the old code as a separate failing test. The post-fix claim is
asserted by `TestM12DatabaseEnforcesTheBoundariesTheAPIClaims` (no INSERT on
`release_bundles`, immutability, TRUNCATE guards, doctored `stage_candidate`
bodies refused with 22023/55000/42501).

## Threat model and limits

- The API process is trusted to run the verifier. The database cannot recompute
  it, so a compromised API process could stage a self-consistent forged body.
  `Restore` recomputation at read time is the backstop and **M13 must recompute at
  approval and activation**. A separate verifier credential is recommended.
- The verifier is deterministic evidence, not an independent attestation.
  Acceptance authorship is by authenticated actor, not proof of human authorship.
- The build runs in one REPEATABLE READ snapshot. The active base or a role can
  advance after the snapshot starts; the candidate then reads STALE on its first
  detail read (`BASE_CHANGED`, `POLICY_CHANGED`) and M13 re-evaluates at approval.
- Production volume is not measured: later releases carry `UNVERIFIED` volume, so
  any non-EQUAL second release rates HIGH or CRITICAL until M13.
- Single-actor tenants cannot build candidates (acceptance author must differ from
  requester and source author). No waiver mechanism exists.
- The requirement decider (on the requirement-set row) and the sandbox freezer (in
  the bundle and index) are recorded; excluding them from approving is M13. Only categories `approval_threshold`, `approval_gate`, `tool_effect` and
  `data_migration` are verifiable; other mandatory categories BLOCK.
- Level-2 sandbox end-to-end acceptance, redacted snapshots, field-level rebase and
  AI patch endpoints are deferred.

## Independent review findings fixed after the first report

- **M12-F1 (HIGH, found by the verifier review and the honesty audit):** `Restore`
  does not recompute the diff or storage class, so a self-consistent forged body
  (shrunk diff, facts/risk/report recomputed from it) restored with a lower risk
  level and `HighRisk=false`. Fixed by `Candidate.VerifyWithBase` wired into
  `Store.LoadCandidate` (resolves the candidate's own base release); the load path
  answers 409 `candidate_failed_verification`. Evidence: unit regression tests
  (`internal/promotion/verify_base_test.go`) and
  `TestM12CoordinatedForgeryIsRefusedWhenLoaded`, which failed with HTTP 200 and
  `high_risk:false` before the fix and passes after. ADR038 Amendment 2 and the
  promotion README were corrected; the claim "`Restore` recomputes the diff" in the
  earlier text of this report was wrong.

## Independent review

Status (partial, 2026-10-16). Reports are in `docs/verification/`:

- **M12 database layer** (`M12_DB_INDEPENDENT_REVIEW.md`): PASS WITH FINDINGS. The runtime role could
  still approve and activate releases directly in SQL (closed by migration 49 of the M13
  branch, not by M12); the ports read the jsonb view and rely on `Restore` for canonical
  form; `worker_grants_safe()` misses REFERENCES/TRIGGER grants and unlisted tables
  (hardening in migration 49).
- **Deterministic logic** (`M12_VERIFIER_INDEPENDENT_REVIEW.md`, in progress): F1 HIGH
  (`Restore` accepts a forged diff) — fixed, see above; the byte-level leaf sweep, the
  randomised verifier mutation testing and the risk monotonicity property tests did not complete.
- **API/policy/audit review:** not completed (no findings recorded).
- **Contract-honesty audit** (`CONTRACT_HONESTY_AUDIT.md`, in progress): H1 (same finding) and
  M1–M9 documentation defects; the certain corrections were applied to ADR036–038, the design appendix and the
  sandbox README.
Not run to completion: the API review, the rest of the verifier review, and the foundation finding-status audit.
