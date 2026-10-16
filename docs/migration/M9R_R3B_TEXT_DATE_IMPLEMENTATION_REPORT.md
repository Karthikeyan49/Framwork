# R3b — Governed text and civil-date indexes

16 October 2026 (Asia/Calcutta). **Bounded text/civil-date slice STATUS: PASS.**
Independent review and final root gates passed. Starting commit `06882b2`,
branch `remediation/m9r-foundation`; tested implementation/tests `f99537f`.
Full R3b/M9R remains PARTIAL. No M10, main merge or production activation.

## Scope

ADR040 was written before code and independently reviewed as a read-only design.
This slice adds fixed shared string/civil-date projections and preserves existing
numeric contracts, immutable IR, source JSONB and preview dataset identities.
The authenticated index GET/register/build routes now dispatch from the compiled
field type. Their existing checkpoint-only response stays compatible.

Forward migration 47 adds `runtime.scalar_indexes`/`scalar_projections`, forced RLS,
NOLOGIN ownership, SELECT-only app access, no generic worker access, typed conversion,
private management/build functions and database-owned record maintenance. The
new fixed indexes cover ascending/descending text/date and overflow lookup.
Numeric/scalar registration shares the existing exclusive epoch gate and combined
eight-field/entity, 32-field/physical-namespace quotas. Nothing introduces a
per-dataset writer counter or tenant DDL. Existing numeric migrations are unchanged.

Forward migration 48 closes the independently reproduced intrinsic-ID admission
defect with source admission and a validated table constraint. Readiness requires
the guard. Existing incompatible registrations stop migration rather than being
deleted. Migration 47 and all previously applied migrations remain unchanged.

Copy and verification remain independently checkpointed at 1000 rows each,
source SHARE locks and registry NO KEY UPDATE, with atomic audit/checkpoint and
source-version maintenance. New valid writes/deletes maintain projections in
one transaction. Unready indexes fall back; unsafe witnesses fail closed.

Text up to 512 UTF-8 bytes uses the ordered projection. Existing valid values
through 4096 bytes remain valid: overflow markers omit the large B-tree key and
force the field's existing JSONB query path. Null and overflow are distinct.
No value is truncated or silently omitted. Default database collation is retained,
including legitimate NULL version witnesses; registry/reported/actual drift
rejects. Overflow admission requires the exact transaction-bound RuntimeSnapshot
proof plus stable isolation; wrappers/autocommit pools use the old query path.
All field/row policy, filters, cursor and version predicates remain before LIMIT.

Civil dates use explicit Gregorian components/years 1–9999, independent of
DateStyle/time zone. A discovered existing pagination mismatch rejected valid
long text cursors: Go limited decoded JSON to 1024 bytes and encoded requests to
2048 characters, and the frontend also capped at 2048. Bounded decoded 32KiB and
encoded 43691 now cover JSON escaping of 4096-byte values; old cursors/fingerprints
stay readable. Limits are checked before base64 allocation and remain finite.

## Evidence so far

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh test -race ./internal/runtime/data ./internal/platform/access` | PASS baseline 1.695s and implementation 1.704s; access has no unit test files. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RNumericIndex' ./tests/integration` | PASS baseline 30.549s. |
| `make migrate migration-status` | PASS, additive47 applied; existing1–46 retained. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarIndex' ./tests/integration` | PASS first23.287s and expanded40.001s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -timeout=5m -count=1 -run '^TestM9RScalarIndexPerformanceFloor$' -v ./tests/integration` | PASS initial62.972s, then expanded deep-cursor64.464s. Twelve cases: first/second/middle page, text/date, ASC/DESC; each100 samples/ten clients. Deep HTTP p95 ranges233.483–286.036ms, zero errors, unchanged750ms target. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarIndexPopulatedRawIsolation' ./tests/integration` | PASS2.698s: two populated tenants, raw forced RLS, worker column-grant drift/recovery, civil date under SQL/DMY and Pacific/Kiritimati. |
| `npm --prefix apps/web run typecheck` | PASS. |
| `npm --prefix apps/web test` | PASS96/96,6.86s, including escaped long-cursor decoding. |
| `make check` | PASS: frozen checksums, format, vet, module verification, frontend lint/typecheck. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)Index\|TestM1FreshDatabaseMigrations)' ./tests/integration` | PASS69.845s: old numeric/new scalar roots and fresh migration installation. |
| Independent executable adversarial review | PASS on corrected `2823a53`, with eight independent probes retained at `f99537f`. Combined numeric/scalar/review/fresh migrations: 26 tests, 81.587s. Both reproduced findings closed; details below. |
| `GOFLAGS=-v make check test build` | PASS on source/tests `f99537f`: format/vet/module verification, seven Node script tests, frontend lint/typecheck and 96 tests, 196 integration tests (592.607s), all eight Go binaries and frontend production build. No test failures. |
| `make infra-up migrate migration-status` | PASS: PostgreSQL/Redis/collector running; existing credentials preserved; all migrations 1–48 applied, repeat application safe. |
| `make test-e2e` | PASS 9/9, 17.6s. |
| `make test-signin-e2e` | PASS 7/7, 7.5s. |
| `make test-performance` | PASS 39.805s: existing 100k-row/ten-client numeric HTTP p95 123.958ms, p99 146.305ms, 123.55 requests/s, zero errors. |
| `make test-scalar-performance` | PASS 87.836s: all twelve 100k-row/ten-client cases, 1200 requests, zero errors; worst p95 321.890ms against 750ms, p99 355.237ms, throughput 47.60–127.65 requests/s. Both indexes copied/verified 100000 rows in 203 checkpoints each (58.198s combined build); every actual plan selected its requested index and visited 202 source records. |
| `make test-write-performance` | PASS 61.573s: 32 clients over 60 seconds; 11822 successful distinct-record HTTP writes, p95 197.770ms, p99 224.617ms, throughput 196.53/s, zero errors. |
| `make docs-verify` and `git diff --check` | PASS final rerun: 84 V2 + 43 V3 checksums and no whitespace defects. |

Executed cases include 1206-row copy/verification/replay, Unicode/default-order
ASC/DESC/null/tie/cursor equivalence, patch/delete consistency, maximum escaped
text cursors, UTF-8 overflow/re-entry, civil-date/typed rejection, collation witness
fault, field/tenant/manager/worker denial, audit rollback/corrupt coverage, combined
quota race, opaque fallback, concurrent overflow snapshot, old-RR epoch fencing,
routine/trigger drift, populated owner scope, hidden sort and adopted index identity.
Independent review extended these cases with executed faults rather than
approving their labels alone.

## Reproduced findings and corrections

The [independent report](../verification/M9R_R3B_TEXT_DATE_INDEPENDENT_REVIEW.md)
retains exact failed commands, fixture errors, structural evidence and corrected
verification. It was written by a separate reviewer against stable commits,
without concurrent implementation changes or competing test workloads.

- **MEDIUM, raw database admission:** Go rejected the intrinsic ID but the SQL
  port admitted it. A rollback-only owner/app probe registered that required
  string and demonstrated a valid subsequent update failing SQLSTATE22023.
  Forward 48 at `283ac0b` now rejects admission (22023) and direct registry
  insertion (23514). Missing/unvalidated guards fail readiness; an incompatible
  registry rehearsal blocks the actual migration and retains evidence.
- **HIGH, cold cursor plan:** after READY, a civil-date page on 1206 records
  intermittently returned 503/SQLSTATE57014 at the unchanged two-second limit.
  Structural replay showed roughly 245678 source visits for a 101-row page,
  despite selecting the requested projection index. Warm capacity alone had
  missed this. JIT was excluded as the cause. `2823a53` uses correlated live-view
  LATERAL point fetches with OFFSET 0 barriers, source version and namespace/ID
  constraints. Projection-ID cursor/tie predicates prune before source fetch;
  every policy/filter remains before LIMIT. The same cold plan now visits 202
  source rows and executes in 3.229ms independently. No planner strategy, query
  timeout or permission check was disabled.

Root executed the formerly failing intrinsic-ID test after forward 48 (PASS).
Root's exact focused command after the point-fetch correction was
`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9RScalarReviewOrderingWithSafeDriverDiagnostics|TestM9RScalarIndexOrderingCheckpointsAndMaintenance)$' -v ./tests/integration`
(PASS 23.618s, 202 source visits, 2.909ms replay). The reviewer independently
repeated the corrected suite and capacity rather than accepting that rerun.
Initial reviewer lock-observer failures were test-fixture visibility mistakes,
explicitly corrected and retained; they are not product pre-fix evidence.

Eight independent probes additionally execute actual dropped-COMMIT recovery,
overlapping copy/update/delete, masked joins/filter/cursor equivalence, raw
non-manager denial, real autocommit-RR fallback, UTF-8/date boundaries, intrinsic
admission/guard/recovery, cold-plan visits and indexed retained/current-generation
ownership, share revocation and tombstones. No mock PASS label substitutes for
these exercised paths.

Independent `make test-scalar-performance` PASS 86.287s: PostgreSQL 18.6 with
verified four CPUs, 8GiB and disabled swap; ten populated tenants, 100000 hot rows
(904-byte average), nine other tenants with 10000 rows each. Both indexes copied
and independently verified 100000 rows in 203 checkpoints each (56.963s build).
Twelve text/date ASC/DESC cases at offsets 0, 100 and 50000 each used three
warmups, 100 measurements and ten HTTP clients. All 1200 requests succeeded;
worst p95 271.638ms, p99 317.621ms, throughput 47.99–126.62/s. Every requested
index was selected and each plan visited 202 source records. Projection visits
were 101/201/50101 respectively; this is not constant-time cursor traversal.
The independent report contains every case, prior failures and exact commands.

## Changed files and dependencies

- `db/migrations/00047_m9r_text_date_indexes.sql` and
  `00048_m9r_scalar_intrinsic_id_guard.sql`: fixed platform schema, typed
  registration/build/maintenance, combined quotas and forward review correction.
- `internal/runtime/data/scalar_index.go`, `query.go`, `store.go` and
  `internal/platform/access/indexes.go`: witnessed management, readiness,
  snapshot/overflow admission, bounded point plans and existing route dispatch.
- `apps/web/src/preview/contracts.ts` and `contracts.cursor.test.ts`: bounded
  long-cursor compatibility. Existing numeric tests now use an actually
  unsupported enum rather than a newly supported string for denial fixtures.
- `tests/integration/m9r_scalar_{index,lifecycle,performance,review}_test.go`:
  root correctness/lifecycle/reference workload and eight independent probes.
- `Makefile`: explicit `make test-scalar-performance` reference gate.
  ADR040, README, AGENTS, this report, independent review, plan and finding ledger
  document the contracts, reproduced failures, recovery and remaining work.

No major dependency, compiler/metadata schema version or stored IR format was
introduced. Existing Go/PostgreSQL/React/TypeScript libraries and lockfiles are
retained. Source architecture kits and their checksum manifests remain verbatim.

## Bounded acceptance and publication

- PASS: source/IR compatibility, existing numeric behavior, frozen architecture
  kits, fresh migration installation and safe repeated migration application.
- PASS: typed text/date conversion, null/tie/Unicode/ASC/DESC/cursor equivalence,
  valid long-value fallback and bounded frontend/backend cursor decoding.
- PASS: tenant isolation, raw app/worker denial, live manager authority, forced
  RLS, owner/field scopes, masking and retained/current-generation canonical ACL.
- PASS: combined quotas and registration races, copy/verification checkpoints,
  atomic audit rollback, actual unknown-COMMIT reconciliation, overlapping
  copy/update/delete and stale-writer fencing.
- PASS: independently reproduced intrinsic-ID and cold-plan defects closed;
  direct invariant/readiness/migration faults and bounded source visits reverified.
- PASS: full static/unit/integration/frontend/build/browser checks plus existing
  numeric and 32-client write gates and new twelve-case scalar reference gate.
- NOT RUN: real collation-library upgrade/reindex, external production database
  recovery/DR, overflow/selective-policy load certification and remote CI.
  These remain explicit release/capacity gaps; no production exposure is implied.

The designated publication target is `remediation/m9r-foundation`, with normal
fast-forward history only. No main merge, deployment or M10 was performed. Source,
migrations, tests and review/report updates belong in Git; `.env`, credentials,
keys, dependencies, binaries, frontend outputs and local run logs remain ignored.
The task completion records the final commit and direct remote-head verification.
Overall R3b/M9R stays **PARTIAL**: remaining index types/search, R3c uniqueness,
R3d aggregation/planning, role-definition UI, external identity deployment,
R6 authority and R7 operations/reliability remain separate work. Historical R7
failures remain open despite this passing full run. M10 stays paused.

## Limits and recovery

Fast text capacity requires no overflow anywhere in that physical field. Unindexed,
overflow and selective-policy capacity remain uncertified. The executed middle-page
gate covers only the reference workload above; deeper/wider distributions need
their own stated evidence.
No general full-text search, enum/reference/instant index, uniqueness, expanded
aggregate planner, automatic index inheritance, production release activation,
external provider certification or complete M9R resolution is implied.
No new major dependency. Existing R7 connection/worker capacity findings stay open.

Retain records, old numeric facts and checkpoint evidence. Unknown build COMMIT
uses authenticated GET before another request. Broken trigger/ownership/collation
requires reviewed forward repair; never restore broad grants or destructive down
migrations. A real collation-library upgrade/reindex drill is not yet executed.
If forward 48 rejects an existing incompatible witness, stop writers and preserve
the registry/records for a reviewed forward repair; do not drop the guard, delete
registrations or run destructive cleanup. Keep migrations 47/48 installed when
reverting a compatible application binary. No production data copy, cutover or
deletion is performed by this slice; backup/DR certification remains an operations
release gate.
Secrets, keys, logs, binaries, dependencies and data stay ignored. Commit only
source/migrations/tests/docs on the designated remediation branch.
