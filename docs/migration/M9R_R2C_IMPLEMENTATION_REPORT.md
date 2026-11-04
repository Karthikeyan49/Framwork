# R2c — Preview schema generations and checkpointed evolution

Updated 14 October 2026 (IST). Candidate `4958df9`, following published `c269977`.
Status: R2c preview storage/evolution PASS within the tested boundary.
Full foundation remediation remains incomplete.

## Implemented boundary

Preserved M0–M9 and the equal-schema R2b API. Added immutable generation mappings,
logical owner/team/share/tombstone identity, security-invoker readers, closed
plan-driven shadow COPY/LINK/VERIFY, immutable operation witnesses/checkpoints,
bounded batches, audited CAS activation and safe pre-cutover abort. No source
record, legacy anchor, receipt, workflow, migration or source kit was removed.
An equal schema still binds without copying; unequal schemas use shadows.

New preview management routes use trusted authentication, live membership,
owner/admin policy and exact server-admitted compiler artifacts. Restore and
cutover never compile stored releases. The compiler/data boundary retains
explicit rename, lossless integer/decimal conversion and typed backfill rules.
Cycles are linked after all records exist. Unequal destinations, invalid values,
oversized data, hidden physical extras and stale checkpoints block verification.

Canonical access applies to current and retired bindings and legacy previews.
Unknown logical identity and tombstones deny. Current shares are used in SQL
DataScope; revocation is not copied into a stale grant. Product role policy remains
R5 work. The worker keeps narrow source/share capabilities, immutable generation
pins and no generic grants to the new catalogs. Legacy NULL pins keep their
original namespace. Event causal origin remains the original dataset anchor;
a protected sidecar pins physical generation without changing envelope 1.0.

See [ADR031](../adr/031-m9r-shadow-generations-and-live-access.md), supplementing
[ADR026](../adr/026-m9r-dataset-identity-and-schema-evolution.md).

## Migration and API recovery

Additive migrations 35–37 create generation/identity/evolution storage, controlled
copy/progress functions, generation-aware writer/index/event/worker ports and
workflow pins. No tenant-selected DDL or destructive down migration is exposed.
The existing development database reports all migrations 1–37 applied.

`POST .../datasets/{dataset}/evolutions` accepts `operation_id`,
`expected_binding`, `metadata_revision`, exact `ir_hash`/`compiled_ir` and typed
`intent` (`renames`, `backfills`). It freezes the base. Persist the operation UUID.
`GET .../evolutions/{operation}` reads durable state; POST its `/advance` with
`expected_checkpoint` commits one bounded batch and audit. Repeat to READY, then
POST `/activate` with its checkpoint. No frontend PASS/count/manifest flags exist.

After a lost response, GET before issuing another step. A stale checkpoint fails;
exact prepare/activation retries reconcile the same operation. `/reverify`
restarts verification when current access facts changed. Before activation,
`/abort` reopens the retained base; evidence and shadows stay. After activation,
abort denies. A reverse conversion or restore/reconciliation plan is required.
Never restore broad worker grants, edit applied migrations or delete evidence.

## Verification so far

| Actual command | Result |
|---|---|
| `./scripts/go.sh test ./internal/runtime/data ./internal/platform/access ./internal/runtime/workflow ./internal/policy` | PASS |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RDatasetAdoptionAndReleaseContinuity$' -v ./tests/integration` | PASS 4.216s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9DurableSnapshotApprovalAndImmutablePins$' -v ./tests/integration` | PASS 12.024s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RShadow' -v ./tests/integration` | Final focused run PASS 40.945s; six populated/fault/concurrency/workflow cases |
| `make check test build` | First run FAIL at integration 341.130s: seven M5 cases encountered missing reader views before development migrations ran; old worker-share test expected the now-revoked generic table grant. Check, unit and frontend tests passed; build NOT RUN after make stopped. |
| `make migrate migration-status` | PASS; zero new pending migrations and versions 1–37 applied |
| `make check test build` after migration/test correction | PASS at `4958df9`; integration 331.945s, Go race/unit/security/import tests, Node 5 and frontend 29 tests, lint/typecheck/vet/module/source checks, all backend binaries and frontend build |
| Independent protocol faults / worker/tenant probes | PASS 52.927s at `4958df9`: three independent roots plus six permanent cases under race detection; actual COMMIT-reply loss for prepare/copy/activate, current sharing for pinned worker source, forged tenant/role denial with no mutation facts |
| Independent 100k migration rehearsal | PASS 545.375s at `4958df9`: ten populated tenants, 100k hot + nine × 10k other records, mean record width 886 bytes; adoption 1,000 checkpoints/36.414s, migration 3,000 checkpoints/499.343s, maximum migration batch HTTP 422.669ms, exact 200k retained source+target records and 100k identities; unchanged request/statement budgets. Reference/shared edge density was zero; smaller populated cycle/share fixtures supply separate correctness evidence. |
| Independent mapped-generation public numeric query/index gate | PASS 29.073s: one real cutover followed by explicitly synthetic growth to 100k canonical records; public index build 203 checkpoints/16.802s; ten clients/100 HTTP samples, p95 270.905ms, zero errors, exact money/backfill/ID/cursor results; actual plan selects 101 index rows then canonical PK lookups. Unchanged 750ms HTTP / 2s statement / 3s request limits. This fixture is query evidence, not a second migration proof. |
| Independent `make test-performance` | PASS 44.894s; ordinary 100k/ten-client numeric gate p95 130.939ms, zero errors, unchanged thresholds |
| Independent `make test-write-performance` | PASS 61.517s; 32 clients/60s, 11,484 successful distinct-record writes, p95 209.973ms, zero errors, unchanged 500ms gate |
| Independent final FK/grant/tombstone probes | PASS 5.576s; foreign-tenant team assignment fails 23503, worker grant drift denies admission with raw RLS still zero, logical tombstone hides the original pinned source; no business mutation facts |
| `make test-e2e` | PASS 5 browser cases/8.3s: actual CRUD/stale recovery/pagination/read-only policy, lost HTTP response/owned receipt with no duplicate write, credential redaction and mobile layout |

Earlier iterations failed: new SQL CASE syntax, a backfill fixture using MISSING
instead of required MISSING_OR_NULL, missing fixture operator context, incorrect
assumptions about workflow step/timer counts, and concurrency fixture identity
binding/type errors. These were corrected before the six-case PASS. One stalled
test fixture was terminated; its run is not counted as passing. No production
database or external action was probed.

Read-only independent review identified delete gate upgrades, absent-operation
SQL guards, hidden extra target rows, canonical team FK and unmeasured final scans.
The first four received corrections and regression coverage. Final scans retain
normal statement timeouts and passed the independently executed 100k rehearsal.
Independent correctness execution also passed. Post-cutover query and established
read/write workload regressions remain in progress; no larger or denser migration
capacity is inferred from this fixture.

## Remaining gates

Production active releases/approval, independently executed business acceptance,
custom roles/product DataScope, uniqueness, other index types, aggregates,
rate limits, delivery operations and retention remain open in the
[finding ledger](M9R_FINDING_STATUS.md). Preview activation is not deployment.
The frontend's existing scratch preview is retained; a dataset management UI,
automatic index inheritance and migration scheduling are not implemented.
M10 remains paused. This report does not close every external review finding.
