# M12 API, policy, audit and composition — independent adversarial review

## Progress

- 2026-10-15 20:20 UTC: branch `review/m12-api-final` created at `fb52ad3`; briefs, AGENTS.md,
  ADR038 and the M12 implementation report read; code reading of the scope files.
- 2026-10-16 05:30 UTC (resumed after a session stop): probe file
  `tests/integration/zz_review_m12_api_test.go` (temporary, deleted before commit) written.
  Authorization matrix, forged/strict bodies, builder authority, audit, composition and
  audit-failure probes executed (PASS runs, results below). Findings F1–F6 recorded.
- 2026-10-17: F3 re-verified as fixed at `dbc4b5b` over HTTP (export of that commit in the
  scratchpad, probe copied in). Races (F7) and build-vs-activation (F8) executed.
  Cost measured (F9, MEDIUM). Snapshot bound (F10), identity disclosure (F11) and invalid-app
  audit (F12) executed. Policy unit probe, scope unit tests and shipped `TestM12*` baseline run.
  Report complete; probe files deleted.

## Scope & commit

Reviewed commit: `fb52ad3` ("docs(m12): record full integration result and decider wording")
on branch `review/m12-api-final` (worktree `/home/user/review-m12`). All file:line references
are to `fb52ad3`. Findings already reported by other reviewers (promotion.Restore not
recomputing the diff, fixed later at `e26dfaf`; direct SQL approve/activate by `framework_app`)
were not re-investigated.

Scope: `internal/platform/access/{releases.go,access.go,metadata.go,preview.go,sandboxes.go}`,
`internal/policy/{policy.go,definition.go,store.go}`, `internal/audit/audit.go`,
`internal/promotion/{release_store.go,store.go}`, `internal/tools/catalog.go`,
`internal/runtime/data/dataset.go`, `internal/platform/app/app.go`; ADR038 §1, §9,
Amendments 6 and 8.

## Method

- Code reading of every scope file and of the stage ports in migration 48.
- Black-box HTTP probes through `httpserver.Handler` built like production
  (`internal/platform/app/app.go:245`: `PolicyLoader: RoleStore.Load`, operator compiler
  catalog). Note: the shipped M12 tests (`m12Setup`, `tests/integration/m12_candidate_test.go:59`)
  wire `policy.Native{}` without a `PolicyLoader`, so stored custom roles were never exercised
  by them; the probes below do.
- Each probe test runs in its own disposable database created by `m8Database` on the MAIN
  stack (migrations through 00048), always through `/home/user/bin/dbrun`.
- Principals: owner (alice), admin (bob, also owner of tenant B), viewer, built-in `member`,
  custom roles published through `POST /v1/tenants/{t}/roles` and assigned through
  `POST /v1/tenants/{t}/members/{m}/role`: `builder_only` (candidates.build), `decider_only`
  (requirements.decide), `author_only` (acceptance.author), `scoped_builder`
  (candidates.build on app B only), and an outsider who is only a member of tenant B.

## Commands

All with `export PATH=/tmp/node24/node-v24.22.0-linux-x64/bin:$PATH` from the worktree root.

| Command | Result | Duration |
|---|---|---|
| `./scripts/go.sh vet -tags=integration ./tests/integration/` (probe file) | PASS | <1 min |
| `/home/user/bin/dbrun nice -n 10 node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -timeout 10m -v -run 'TestZZReviewM12AuthorizationMatrix' ./tests/integration` | PASS (probe; results below) | 11 s |
| same, `-run 'TestZZReviewM12ForgedFieldsAndStrictBodies'` | PASS (probe) | 3 s |
| same, `-run 'TestZZReviewM12BuilderAuthorityAndAudit\|TestZZReviewM12AuditFailureRollsBack'` | first run FAIL (probe bug: audit id is a uuid), re-run PASS | 5 s + 7 s |
| same, `-run 'TestZZReviewM12ForgedFieldsAndStrictBodies'` in a `git archive dbc4b5b` export | PASS (probe) | 16 s |
| same, `-run 'TestZZReviewM12FoldedKeys'` at `fb52ad3` and in the `dbc4b5b` export | PASS (probe) | ~10 s each |
| exhaustive fold scan (`go run` in scratchpad, Go 1.27.2) | PASS | ~1 min |
| `... go.sh test -race -tags=integration ... -run 'TestZZReviewM12Races'` | PASS (probe; F7), no data race | 92 s |
| `... -run 'TestZZReviewM12BuildRacingActivation'` | PASS (probe; F8) | ~15 s |
| `... -run 'TestZZReviewM12Cost'` | first two runs FAIL (probe sizing: 32-field entity budget, then the sandbox envelope), third PASS | 25 s, 6 s, 13 s |
| `... -run 'TestZZReviewM12CostWithBase'` at `fb52ad3` and in the `dbc4b5b` export | PASS (probe; F9) | 34 s, 37 s |
| `... -run 'TestZZReviewM12SnapshotBoundsAndLeakage'` | PASS (probe; F10–F12) | 7 s |
| `./scripts/go.sh test -count=1 -run TestZZReviewM12Policy ./internal/policy/` (temporary unit probe) | PASS (probe) | <1 s |
| `./scripts/go.sh test -count=1 ./internal/policy/ ./internal/audit/ ./internal/promotion/ ./internal/platform/access/ ./internal/platform/httpserver/ ./internal/tools/` | PASS (access has no unit tests) | 7 s |
| `/home/user/bin/dbrun ... go.sh test -tags=integration -count=1 -run 'TestM12' ./tests/integration` (shipped tests, `fb52ad3`) | PASS | 9 s |
| Full integration suite, `make check`, browser tests | NOT RUN (out of scope; shared host rules) | — |

## Findings

### F9 — MEDIUM — uncached full recomputation inside the request transaction: a viewer can exhaust the API

- Where: `GET .../candidates/{hash}` → `promotion.Store.LoadCandidate` → `Restore`
  (`internal/promotion/release_store.go:157-182`, full re-validation of source, IR, risk and the
  verifier over every case) and `API.staleness` → `activeBase` → `Store.Release` → a second full
  `Restore` of the base (`internal/platform/access/releases.go:240-252,396-399`); at `dbc4b5b`
  `LoadCandidate` adds a third restore for `VerifyWithBase`. `POST .../candidates` compiles,
  restores the base, builds and self-restores (`releases.go:282-373`,
  `internal/promotion/bundle2.go:446`), and an identical replay redoes all of it. All of this CPU
  work runs while holding one of the process-wide admission slots / pool connections
  (`internal/platform/postgres/postgres.go:23` 10 connections; admission limit = pool size,
  `access.go:57-62`) inside `postgres.WithRuntime`'s 3 s budget
  (`internal/platform/postgres/transaction.go:52`). Nothing caches the result although bundles
  are immutable and content-addressed; there is no per-tenant rate limit.
- Repro (`TestZZReviewM12CostWithBase`; app with 15 entities × 30 fields, 122 KB source, 218 KB
  IR — the largest that M11 sandbox creation accepts, see F4 — 1000 acceptance cases, 757 KB
  bundle, second release with an active base; shared 4-core host, `nice -n 10`):

  | Measurement | `fb52ad3` | `dbc4b5b` export |
  |---|---|---|
  | `POST .../candidates` with base | 2.09 s | 1.84 s |
  | one `GET .../candidates/{hash}` (viewer) | 1.23–1.43 s | 2.09–2.54 s |
  | 10 parallel detail GETs (viewer) | 10 × 503 in 4.6 s | 10 × 503 in 5.2 s |
  | worst concurrent `GET /v1/tenants/{t}/access` by another user | 3.0 s | 3.4 s |
  | 30 parallel detail GETs | 29 × 503, 1 × 200 in 14.7 s | 30 × 503 in 14.4 s |
  | worst concurrent `GET /access` | 12.7 s | 11.2 s |

  Without a base (first release): build 1.2 s, replay 1.14 s, detail 0.57–0.60 s, 8 parallel
  details all 200 in 2.2 s. List `GET .../candidates` (LIMIT 50, index columns only) 10 ms.
- Impact: (1) any member holding `candidates.read` — every viewer by default — can stall the whole
  API process for every tenant with a few parallel GETs of one large candidate; (2) legitimate
  builds and detail reads of large second releases fail with 503 under modest concurrency or on a
  slower host, and a single request already uses 60–85 % of the 3 s budget; (3) the 503s are not
  audited (only 3–4 `candidates.read` rows for 43 requests), so the abuse leaves no trace.
- Fix: cache the restore/verification result per `(tenant, app, artifact_hash)` (immutable,
  content-addressed), or read the bytes in a short transaction and recompute outside it; bound
  concurrent recomputations per tenant; short-circuit an identical replay by looking up the
  derived index before rebuilding; audit admission/timeout failures of authenticated requests.

### F1 — LOW — a successful stage writes two audit rows; one carries no correlation

- Where: `internal/promotion/store.go:61-62` (`Store.Stage` calls `st.evidence(..., "release.staged")`;
  `store.go:35-40` writes it with `PolicyVersion "m6r-release-contract-v1"` and no request id,
  trace id or decision id) plus `internal/platform/access/releases.go:362-365` / `access.go:213`
  (the API row `release.candidate_staged`).
- Repro (probe `BUILD success`): one `POST .../candidates` (201) produces exactly
  `release.staged|true|release|m6r-release-contract-v1|req=false|dec=false` and
  `release.candidate_staged|true|preview|tenant-role-b4:...|req=true|dec=true`.
- Impact: ADR038 Amendment 8 names only `release.candidate_staged`; the extra row duplicates the
  event with a stale contract version and cannot be joined to the request or decision. Counting
  staged candidates from audit gives 2 per stage. Not an integrity break (both rows commit or
  roll back together — verified by `AUDITFAIL release.candidate_staged`, 0 bundles).
- Fix: drop the store-level row for 2.0.0 stages (the API row is authoritative) or document it and
  give it the request/decision correlation.

### F2 — LOW — `/roles` suffix capture of release detail routes

- Where: `internal/platform/access/access.go:400-406` runs after the exact release mapping
  (`access.go:396-398`) and re-routes any path ending in `/roles` to the role handler.
- Repro: `GET /v1/tenants/{A}/preview/{app}/candidates/roles` (also `/requirements/roles`,
  `/acceptance-specs/roles`, `/candidates/x%2Froles`): owner gets 400 `invalid_role`, audited as
  `roles.read.rejected` (resource `role`); viewer gets 403 audited as `roles.read`. Expected: the
  release handler (404 `not_found`, `candidates.read.rejected`).
- Impact: wrong authorization action and audit label for release routes, contrary to ADR038 §9
  "exact-pattern matched" and the codebase's own fix for `/teams` (`access.go:411-418`). No data
  exposure (role name is empty, `RoleStore.Current` rejects it). The `/members` suffix is not
  captured (verified: 404 `candidates.read.rejected`).
- Fix: decide role routes by exact registered pattern (as `teamPatterns`), never by path suffix.

### F3 — LOW — case-variant duplicate keys bypass duplicate-key rejection in release envelopes

- Where: `internal/platform/access/datasets.go:33-55` (`readDatasetRequest`): `metadata.DecodeJSON`
  rejects only byte-identical duplicate keys, then `encoding/json` matches field names
  case-insensitively with last-wins.
- Repro (probe `FORGE`): `{"revision":1,...,"Revision":99}` → 404 (revision 99 used);
  `{...,"sandbox_id":S,"SANDBOX_ID":X}` → 404 (X used); `POST .../requirements` with
  `{"expected_version":1,"requirements":A,"Requirements":B}` → 201 storing B; `"Revision"` alone →
  201. Exact duplicates are 400.
- Impact: the comment "Bound authoring requests retain duplicate-key rejection" and ADR038 §1
  "strict request" are not fully true; an intermediary that inspects the first key sees different
  content than the server stores. No authority-bearing field is reachable (every such name, in any
  spelling, is an unknown field → 400), and the inner requirement/acceptance documents are strict
  (`intent.ParseRequirementSet` canonical comparison → `NOT_CANONICAL`). The policy package already
  guards this pattern (`internal/policy/definition.go:136-155`, `definitionKeys`).
- Fix: apply a lowercase-only key check (like `definitionKeys`) to release request envelopes.
- Status: independently found and **fixed at `dbc4b5b`** (`metadata.DecodeJSON` treats keys that
  fold to the same name as duplicates). Re-verified over HTTP against an export of `dbc4b5b`
  (`TestZZReviewM12ForgedFieldsAndStrictBodies`, `TestZZReviewM12FoldedKeys`): ASCII case-variant
  duplicates and Unicode simple-fold duplicates (`reviſion` with U+017F, `requirementſ`,
  `EXPECTED_VERSION`, `ACCEPTANCE`, `requirementſ_version`) on `/candidates`, `/requirements` and
  `/acceptance-specs` → 400; the same bodies at `fb52ad3` → 404/409/201 (last key wins). An
  exhaustive scan of every rune (Go 1.27.2) found the fold-orbit loop always terminates and that
  `encoding/json` aliases ASCII field names only through U+017F and U+212A, both of which the fix
  folds, so the fix is complete for these envelopes. Residual (not a finding): a single aliased key
  without a duplicate (`"Revision"`, `"reviſion"`) is still accepted as the field.

### F4 — LOW — the effective body cap is 260 KiB, not 1 MiB; the 1 MiB acceptance limits are dead

- Where: `internal/platform/access/datasets.go:40-44` reads `metadata.MaxBytes+4097` and
  `metadata.DecodeJSON` rejects more than `MaxBytes+4096` = 266,240 bytes; the outer
  `http.MaxBytesReader` is 1 MiB (`internal/platform/httpserver/httpserver.go`, `middleware`).
  `intent.MaxAcceptanceBytes = 1<<20` (`internal/intent/requirements.go:33`),
  `promotion.Store.AuthorAcceptance` (`release_store.go:59`) and the DB CHECK on
  `control.acceptance_specs.body` (migration 48) all allow 1 MiB.
- Repro (probe `FORGE padding`): total body 266,240 bytes → 200; 266,241 → 400; 1 MiB+1 → 400.
- Impact: declared limits disagree; an acceptance spec above ~260 KiB (envelope included) can
  never be authored through the only authoring route. Fails closed. The same envelope bounds the
  M11 sandbox witness, which carries the compiled IR: in `TestZZReviewM12Cost` a 165 KB source
  (305 KB IR) and a 230 KB source (435 KB IR) get 400 `invalid_sandbox`; the largest sandboxable
  app measured was 122 KB source / 218 KB IR. So no candidate can be built for an app whose
  source is roughly above half of the 256 KiB metadata limit.
- Fix: one constant for the release authoring envelope, documented, and the dead limits aligned.

### F5 — LOW — evidence gap in the shipped M12 tests

- `tests/integration/m12_candidate_test.go:274`: the cross-tenant read assertion is
  `w.Code == 200 && e.f.bob == e.f.alice`, which is never true, and `outsider` is bob's token
  (`m12_candidate_test.go:63`), who is an admin of tenant A. The check asserts nothing.
- `m12Setup` (`m12_candidate_test.go:59`) has no `PolicyLoader` and no `CompilerCatalog`, so no
  shipped test exercises stored custom roles or the production composition.
- The probes in this review cover both (Verified V1–V3); the shipped tests should be fixed.

### F6 — LOW — rejection audit omits the reason and the assessment hash

- Where: `internal/platform/access/releases.go:338-352`: a verifier rejection stores the report
  hash in `state.artifact_hash`; a risk rejection stores only `risk_level`; `STORAGE_BREAKING` and
  `INVALID_EVOLUTION_INTENT` store nothing beyond the action.
- Repro (probe `BUILD evolution intent on first release`): 422 `{"reason":"STORAGE_BREAKING"}`,
  audit `candidates.build.rejected` with no reason, report or assessment hash.
- Impact: ADR038 Amendment 8 says rejections record "the report/assessment hashes"; the
  assessment hash and the rejection reason are not recorded, so audit cannot show why a build
  was refused.
- Fix: record the reason and the assessment facts/policy hash (bounded fields) in the audit state.

### F7 — LOW — idempotency holds only sequentially; concurrent identical requests get 409

- Where: `control.stage_candidate` (`INSERT ... ON CONFLICT DO NOTHING`, migration 48) and
  `control.author_acceptance` (`SELECT` then `INSERT`) run in the REPEATABLE READ transaction of
  `postgres.WithRuntime` (`internal/platform/postgres/transaction.go:93`); `releaseReply` maps
  40001/23505 to 409 (`internal/platform/access/releases.go:70`).
- Repro (`TestZZReviewM12Races`, `-race`): 4 concurrent identical `POST .../candidates`, three
  rounds → each round exactly `[201 staged, 409, 409, 409]`, one bundle and one candidate row per
  round, 1 `release.candidate_staged`, 3 `candidates.build.rejected`, 0 replays. 4 concurrent
  identical acceptance posts by the same author → `[201, 409, 409, 409]`, one new spec. 4
  concurrent requirement appends with the same `expected_version` → `[201, 409, 409, 409]`, head
  +1. No data race reported.
- Impact: no double stage, orphan row or lost update (integrity holds). But ADR038 Amendment 7
  ("idempotent for the same author and content") and the replay contract (200 `staged:false`) are
  true only for sequential retries; a client retrying concurrently gets 409 and an audit row
  `candidates.build.rejected` for a request that would have been a replay. A retry converges.
- Fix: document it, or map the unique-violation/serialization outcome of an identical request to
  a re-read and return the replay.

### F8 — LOW — "the base is the current active release" is snapshot-current, not commit-current

- Where: `control.stage_candidate` base check (`EXISTS(SELECT 1 FROM control.active_releases ...)`,
  migration 48) runs in the build's REPEATABLE READ snapshot.
- Repro (`TestZZReviewM12BuildRacingActivation`): activation of candidate c1 is executed but held
  uncommitted; a build of another revision starts; the activation commits 15 ms later; the build
  returns 201 staging a candidate with `base_release ""`, `base_generation 0` although c1 was
  active before the stage port ran. Its first detail read reports
  `stale: true, ["BASE_CHANGED","SCHEMA_CHANGED"]`; a rebuild binds the real base.
- Impact: a candidate whose base claim was false at commit time is staged. The implementation
  report documents this residual ("the candidate then reads STALE on its first detail read") and
  M13 must re-evaluate staleness at approval/activation, so this is a wording issue: ADR038 §6
  states the port checks that "the base is the current active release or absent".
- Fix: say "as of the build snapshot" in ADR038 §6, or serialize stage against activation (for
  example an advisory lock per tenant/app taken by both, which turns the race into 409).

### F10 — LOW — more than `MaxRoleHeads` role heads permanently disables building and detail reads (unaudited 503)

- Where: `internal/policy/store.go:203-225` (`Snapshot` returns `ErrDefinition` above 1024 heads)
  is not mapped by `releaseReply` (`internal/platform/access/releases.go:50-78`), so it becomes a
  503 with no audit; `control.publish_role` (migration 38) has no role-count limit and role heads
  cannot be deleted (deactivated heads still count).
- Repro (`TestZZReviewM12SnapshotBoundsAndLeakage`): owner publishes 1025 roles (1029 heads):
  `POST .../candidates` → 503 `service_unavailable`, `GET .../candidates/{hash}` (viewer) → 503,
  list → 200; only the list request is audited.
- Impact: owner-triggered (roles.write is owner-only) and fail-closed, but permanent for the
  tenant without operator intervention, reported as a platform outage, and invisible in audit.
  `MaxRoleHeads` is documented as "a tenant above it cannot build release candidates until roles
  are retired" — there is no way to retire a head.
- Fix: enforce the bound at publication (refuse the 1025th head), map `ErrDefinition` from the
  snapshot to an audited 409/422, and count only active heads or allow retiring heads.

### F11 — LOW — actor ids of other members are shown to viewers; not documented

- Where: `StoredRequirements.DecidedBy` (`internal/promotion/bundle2.go:200-207`),
  `StoredAcceptance.AuthorID` (`bundle2.go:208-216`), `CandidateRow` requester / acceptance author
  / sandbox freezer / source author (`release_store.go:99-121`) and `CandidateView`
  (`releases.go:207-229`), all readable with `requirements.read` / `acceptance.read` /
  `candidates.read`, which every role holds (`internal/policy/policy.go:76-79`).
- Repro (`LEAK` probe): a viewer's `GET .../requirements/1`, `.../acceptance-specs/{id}`,
  `.../candidates` and `.../candidates/{hash}` contain the owner's and the admin's user ids, while
  the same viewer gets 403 on `GET /v1/tenants/{t}/members`.
- Impact: membership enumeration is owner/admin-only, but release reads disclose who decided,
  authored, froze and requested. This matches the pre-existing `created_by` in metadata versions,
  and approvers need these ids for separation of duties, so it is probably intended — but ADR038
  §9 says only "Reads for all roles". The ids are opaque UUIDs (no names or e-mail).
- Fix: document the disclosure in ADR038 §9, or restrict identity fields to roles that hold
  `memberships.read`.

### F12 — LOW (pre-existing, reached by M12 routes) — invalid app id audited as `authentication.denied`

- Where: `internal/platform/access/access.go:419-425` returns 400 `invalid_application` before
  `evidence.TenantID/Action/Resource` are set (`access.go:427-430`).
- Repro: `GET /v1/tenants/{A}/preview/app.X/candidates` (owner) → 400, audit row
  `authentication.denied | allowed=false | resource identity | tenant NULL`.
- Impact: an authenticated, tenant-resolved request is recorded as an authentication failure with
  no tenant, so tenant audit queries miss it and authentication-failure metrics are polluted.
  (POST with an invalid app never reaches the handler: 413 from the `AcceptsPOST` allowlist.)
- Fix: set tenant, action and resource before validating the application selector.

## Verified

- **V1 Authorization matrix** (`TestZZReviewM12AuthorizationMatrix`, production composition with
  stored roles). Results (POST requirements / POST acceptance / POST candidates / 4 GETs):
  outsider 403 on every route; viewer and built-in member 403/403/403, reads 200; `author_only`
  only POST acceptance 201; `decider_only` only POST requirements 201; `builder_only` only POST
  candidates 201 (delegated build works end to end; the DB `release_actor_ok` accepts the custom
  role); `scoped_builder` 403 on app A, 201 on app B; admin 201/201/400 (admin authored the
  acceptance, so requester = acceptance author is refused); owner 201/201/201. Custom roles
  without read grants get 403 on reads (explicit grants only). Candidate detail: viewer, member,
  admin, owner 200; all custom-only roles and the outsider 403.
- **V2 Role publication**: `sandbox.manage` in a custom role, `candidates.build`,
  `requirements.decide` or `acceptance.author` in a role named `viewer`, the unknown action
  `candidates.approve` and `candidates.*` are all refused with 400 `invalid_role`.
- **V3 Cross-tenant / cross-app confusion**: outsider responses are identical 403 for existing
  and unknown acceptance ids and candidate hashes (no existence oracle). Tenant B's owner using
  tenant A's ids under tenant B's path: 404 for requirements, acceptance, candidate and build.
  Inside tenant A, app B's acceptance id, sandbox id or candidate hash, an unknown requirement
  version and revision 99 under app A's route: 404, identical to unknown ids.
- **V4 Staleness is reader-independent**: viewer, member, admin and owner see the same
  FRESH/`[]` result for the same candidate (RLS on the inputs is tenant-scoped, not role-scoped).
- **V5 Forged authority fields** (`TestZZReviewM12ForgedFieldsAndStrictBodies`): 32 top-level
  names (`outcome`, `high_risk`, `HighRisk`, `requester(_id)`, `RequesterID`, `provenance`,
  `verification`, `dependencies`, `risk`, `sandbox`, `author`, `base_release`, `BaseRelease`,
  `policy_hash`, `tools_hash`, `report`, `acceptance_author`, `decided_by`, `freezer`, `frozen_by`,
  `generation`, `base_generation`, `hash`, `artifact_hash`, `tenant_id`, `app_id`, `ir`, `ir_hash`,
  `staged`, `volume`, `intent`), nested names inside `evolution_intent` and its `renames[]`, and a
  unicode-escaped `outcome` all → 400 with nothing staged. Requirements route: top-level
  `decided_by`, `version`, `hash` → 400; inner `decided_by` → `INVALID_SCHEMA`; inner `Status`
  case variant or duplicate → `NOT_CANONICAL`. Acceptance route: top-level `author_id`, `id` → 400;
  inner `author_id`, `provenance`, `ir_hash` → `INVALID_SCHEMA`.
- **V6 Strict body handling**: exact duplicate keys, trailing garbage, a second document, array /
  empty / `null` bodies, `text/plain`, missing content type, `application/json-patch+json`,
  `1e0`, `1.0`, `-0`, `0x1`, overflowing and string revisions, `expected_base` number, nesting
  depth 40, invalid UTF-8 and `\u0000` → 400. `application/json; charset=utf-8`,
  `expected_base: null` and `evolution_intent: null` are accepted and replay (200).
- **V7 Builder authority** (`TestZZReviewM12BuilderAuthorityAndAudit`): open (unfrozen) sandbox,
  a preview dataset id as sandbox, a sandbox frozen for revision 1 used for revision 2, an
  acceptance bound to requirement set v1 used with v2, and a self-authored acceptance → 400
  `invalid_release_request`; unused evolution intent on a first release → 422 `STORAGE_BREAKING`;
  `expected_base` lie → 409 `base_changed`. Each wrote exactly one `candidates.build.rejected`
  row and staged nothing (candidates/bundles stayed 1/1 from the single success).
- **V8 Audit per path**: success → see F1; replay → one `release.candidate_replayed`; viewer
  build → one `candidates.build` allowed=false; forged field 400 → one `candidates.build.rejected`;
  unknown candidate 404 → one `candidates.read.rejected`; requirements CAS conflict 409 → one
  `requirements.decide.rejected`; oracle violation → one `acceptance.author.rejected`. Every row
  carries the request id and decision id. The `requestRejection` path rolls the business
  transaction back and records the rejection in a fresh transaction (rows above, nothing staged).
- **V9 Audit failure rolls back the effect** (`TestZZReviewM12AuditFailureRollsBack`): failing the
  API append of `release.candidate_staged`, `requirements.decide`, `acceptance.author` or
  `candidates.build.rejected` → 503, 0 candidates, 0 bundles, no new requirement version or spec,
  and no orphan `release.staged` row.
- **V10 Composition**: `PreviewEnabled=false` → GET routes 404 `Route not found`, POST routes 413
  `body_not_allowed`; `Registry=nil` → POST 413. Nothing release-related is exposed.
  `POST .../candidates/`, `POST .../candidates/{hash}`, `PUT`, `PATCH` → 413 before routing;
  `GET .../candidates/`, `DELETE`, `OPTIONS` without Origin → 404 catch-all with no audit;
  `HEAD .../candidates` runs the GET handler and audits `candidates.read`; `GET //candidates` →
  307 to the canonical path; `%2e%2e` as a hash → 404 `candidates.read.rejected`; CORS preflight
  from a non-allowlisted origin → 403. `AcceptsPOST` (`metadata.go:27`, `releasePOST`
  `releases.go:28`) and the registered POST patterns agree for every valid app id; an app id the
  regex rejects gets 413 with no audit (fail closed).

- **V11 Policy**: the action catalog has 46 unique names and `ActionCatalogHash` hashes the sorted
  list (`internal/policy/store.go:190-197`), so reordering the constant does not change it while any
  added/removed action stales every candidate (intended, fail closed). `CompileDefinition` accepts
  `candidates.build` for `member`, `auditor` and `admin`, `candidates.read` for `viewer`; refuses
  `sandbox.manage` for `member` and `release_builder` and `candidates.build` for `viewer`. The
  `sandbox.manage` rule is name-based: the owner may republish the role named `admin` with it
  (accepted), which matches Amendment 6's "owner/admin only". A DENY grant scoped to one app and
  `validResource` (preview type plus app id required) behave as specified. Bootstrap-role
  decisions are recorded as `tenant-role-b4:<hash>` under the production `PolicyLoader`; the
  native `m12-native-v9` appears in the policy bundle hash input.
- **V12 Error leakage**: every refusal body is one of the generic messages in `releaseReply` /
  `denied` (no SQL text, constraint names, stack traces or other tenants' hashes); 422 bodies
  carry only the caller's own verification report / risk assessment / compile diagnostics.
  403 (policy or tenant) precedes any lookup, and 404 for another app's or tenant's artifacts is
  identical to 404 for unknown ids (V3).

## Contract honesty (ADR038 §1, §9, Amendments 6 and 8)

| Claim | Verdict | Evidence |
|---|---|---|
| §1 authority-bearing request fields fail with 400; unknown fields rejected | VERIFIED | V5, V6 |
| §1 "strict request" | FALSIFIED at `fb52ad3` (case-variant duplicates), VERIFIED at `dbc4b5b` | F3 |
| §1 everything derived in one REPEATABLE READ transaction | VERIFIED (code `transaction.go:93`; behaviour F8) | F8 |
| §1(2) IR from the trusted compiler with the operator catalog | VERIFIED by code (`releases.go:297-298`, `app.go:243-245`); tool catalog NOT TESTED dynamically | — |
| §1(3) frozen sandbox evidence must match revision / IR / schema / data mode | VERIFIED for unfrozen, non-sandbox and other-revision evidence; schema-hash and data-mode mismatch NOT TESTED dynamically | V7 |
| §1(4) base from `active_releases`; `expected_base` mismatch is 409 | VERIFIED, with the snapshot caveat | V7, F8 |
| §1(6) staged through the definer port in the same transaction as its audit | VERIFIED | V9 |
| §1 `HighRisk` derived, `RequesterID` = authenticated actor | VERIFIED | V1, V5 |
| §9 preview-gated (development/test) routes | VERIFIED | V10 |
| §9 exact-pattern matched | FALSIFIED in part (`/roles` suffix capture) | F2 |
| §9 actions `requirements.read\|decide`, `acceptance.read\|author`, `candidates.read\|build` | VERIFIED | V1 |
| §9 reads for all roles; writes owner/admin by default, delegable, never `viewer` | VERIFIED | V1, V2 |
| §9 policy version bump | VERIFIED (`m12-native-v9`, `policy.go:19`) | V11 |
| Amendment 6 role grants; `sandbox.manage` owner/admin only | VERIFIED (name-based rule noted) | V1, V2, V11 |
| Amendment 7 (context) acceptance authoring idempotent for the same author and content | VERIFIED sequentially, FALSIFIED under concurrency (409) | F7 |
| Amendment 8 success records `release.candidate_staged`; replay `release.candidate_replayed` | VERIFIED, plus an extra uncorrelated `release.staged` row | V8, F1 |
| Amendment 8 rejections record `candidates.build.rejected` with the report/assessment hashes | Action VERIFIED; hashes FALSIFIED in part (no assessment hash, no reason) | V7, V8, F6 |
| Amendment 8 requirement decisions record `requirements.decide` with version and hash | VERIFIED by code (`releases.go:140`) and row presence | V8 |
| `MaxRoleHeads`: "a tenant above it cannot build ... until roles are retired" | FALSIFIED in part (no retirement path; unaudited 503) | F10 |

## Not covered

- The M13 approve/activate routes on the moved head (excluded by instruction); approval-time
  recomputation; the direct-SQL approve/activate and diff-recompute findings of other reviewers.
- Candidates with operator tool catalogs (tool pins, `TOOL_*` staleness, `tools.Catalog.Current`):
  reviewed by code only (`internal/tools/catalog.go:209-215`, `releases.go:416-427`; `Current` is
  used only to classify a changed pin and never authorizes).
- Evolutions beyond a first release with an unused intent (TRANSFORM/rename/backfill risk paths),
  100-requirement sets and multi-workflow apps in the cost probe; risk BLOCKED path over HTTP.
- Real network server behaviour (`http.Server` Read/WriteTimeout 10 s, client disconnect during a
  build); probes used in-process `httptest`. Latencies come from a shared 4-core host running other
  agents under `nice`; ratios, not absolute values, are the evidence for F9.
- A compromised API process and SQL-level probes of the definer ports (DB review scope).
- Everything except F3 and F9 was executed at `fb52ad3` only; the `dbc4b5b` export was used for
  F3 re-verification and the F9 re-measurement.
- Process note: the trailers of builder commits `a1c1916`, `7d4b9c3` and `fb52ad3` name a specific model
  version, contrary to the common rules for commits; not a product issue.

M12 API review STATUS: PASS WITH FINDINGS
