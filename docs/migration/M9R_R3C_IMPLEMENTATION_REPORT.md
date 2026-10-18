# R3c — Governed unique-field integrity

18 October 2026 (Asia/Kolkata). **STATUS: IN PROGRESS; not accepted as complete.**
Branch: remediation/m9r-foundation. Reconstructed from the interruption checkpoint
at `124fe4e`; design committed/pushed at `3310880`. No main merge, production
activation or M10. Overall M9R remains PARTIAL.

## Implementation under verification

ADR044 defines opt-in metadata/IR/compiler 1.4, typed `Field.Unique`, declaration
budgets and immutable legacy readers. The physical storage hash excludes the new
integrity flag. Restore checks the exact 1.4 version pair, source/runtime-field
agreement and budgets. Older schemas cannot smuggle the flag. Full shadow
evolution involving unique declarations is rejected pending a constraint-preserving
design; database fencing also checks installed constraints across the namespace.

Migration 52 adds shared constraint, private claim, immutable abort-history and
transaction-bound write-permit tables, forced RLS and NOLOGIN ownership. The
database UNIQUE constraint arbitrates canonical hashes within the exact tenant /
namespace / entity / field / attempt. NULL may repeat; text has exact Unicode
bytes; typed numeric and datetime keys use existing exact conversion/rounding.
Collisions fail closed. No tenant DDL or source record deletion is introduced.

Owner/admin registration derives the field witness from immutable bound source.
Copy and independent verification use 1000-row checkpoints. Incomplete integrity
blocks entity writes; abort/restart uses explicit generation and checkpoint
witnesses, preserves records/history and does not reuse old-generation claims.
Existing data is adopted before binding a unique declaration in the populated
fixtures. No trigger bypass is used to seed the acceptance fixture.

API commands require closed policy proof and full entity/field read/write scopes
before non-null key claims. SQL separately locks and checks live membership / role
authority. Private permits bind backend/xid8, actor, resource, generation and key.
Triggers enforce permits even for compatible metadata without the unique flag.
Workers cannot mint permits; legacy write paths do not mint them. PostgreSQL
cannot identify binary age when app processes share credentials. Duplicate SQL
details and canonical keys are not exposed by the management/data endpoints.

GET/register/build/abort management routes use existing authenticated preview and
dataset-binding boundaries. Registration requires explicit expected generation;
build/abort require generation plus checkpoint. GET supports reconciliation after
unknown COMMIT. Commands retain their atomic receipt, audit and outbox behavior.
The renderer reads the new typed flag; write hints remain advisory and stay false
until all declared constraints are READY. An administration UI for these ports is
not implemented.

## Executed checks and retained failures

The ignored `.local/reports/r3c-*.log` files contain current-instance output;
generated logs, binaries, dependencies, keys and environment files are not Git
content. This report is updated with final evidence after independent review.

| Command | Result so far |
| --- | --- |
| `git push origin HEAD:refs/heads/remediation/m9r-foundation` before implementation | PASS: existing `124fe4e` was already published; reconstructed ADR subsequently pushed at `3310880`. |
| `make setup` | PASS: pinned tools/dependencies restored without lockfile changes. |
| `make infra-up migrate migration-status` baseline | PASS: dependencies healthy; retained local DB advanced from 27 through 51 without destructive changes. |
| `make check test` baseline | PASS: static, Go/script/frontend checks, 96 frontend tests and integration suite in 684.997s. Integration executable embeds the pre-change source/migrations; existing fixture files remained unchanged. |
| `./scripts/go.sh test ./internal/compiler ./internal/metadata ./internal/runtime/data ./internal/platform/access` | PASS initial focused packages. |
| `./scripts/go.sh test ./internal/compiler` after new compiler cases | PASS 5.077s; deterministic IR, storage identity, restore/version/source negatives and declaration budgets. |
| `make check test-unit` | PASS static, race/unit/script checks and 97 frontend tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RUnique' -v ./tests/integration` first attempt | FAIL build: unused test import; removed. |
| Same focused command, second attempt and duplicate-build diagnostic | FAIL: three groups passed; duplicate-build status comparison exposed GET returning the general descriptor. This was a real routing defect, not committed backfill progress. Guarding the descriptor route fixes it. |
| Same focused command after route fix | PASS 19.803s: four populated groups, including independent backfill coverage, conflict/permit cleanup, abort/restart/stale generations, concurrent claims, hidden-value denial and typed key checks. Migration 52 successfully applied in disposable fixtures and is now frozen. Later SQL fixes must be forward-only. |
| `make test-unique-performance` initial | FAIL 240.485s overall: 100k/ten-tenant 1509-byte case PASS (177.17s, 203 checkpoints, build20.478s, fresh p95470.164ms / expected-conflict p95477.845ms). Bound 32-client writes had 3259 successes/zero errors but p95690.646ms above 500ms; fixture also incorrectly included 32 initial create receipts in an updates-only assertion. Both failures are retained. |
| `./scripts/go.sh run .local/reports/r3c-validator-profile.go` before/after ADR045 | Initial fixed-schema construction100 samples mean25.476ms versus existing-validator parse0.583ms. Immutable compiled-schema sharing reduces construction to0.306ms amortized including first compilation; parse0.719ms. No source/policy/record/catalog cache. |
| `./scripts/go.sh test -race ./internal/metadata ./internal/compiler ./internal/runtime/data` after ADR045 | PASS: concurrent valid/invalid validation across all five versions, source/hash isolation and independent returned handles; retained compiler/data race tests pass. |
| `make test-unique-performance` after schema-cache and fixture correction | PASS 199.809s interim capacity run: 100k case137.49s, 203 checkpoints/build15.438s, fresh p95355.755ms/p99443.432ms (40.22/s), expected-conflict p9574.097ms/p99118.208ms (177.57/s), no unexpected errors. 32-client/60s bound-dataset writer: 10330 successes, zero errors, p95219.378ms/p99241.092ms, 171.73/s; exact source/version/receipt/outbox checks pass. Runtime statement2s/lock500ms/write-p95500ms targets unchanged. The executable compiled before the narrow receipt-error translation follow-up; final stable-source gates remain required. |
| Independent executable source review | PASS on stable `d15be92`: 12 populated adversarial groups in 20.883s, no skips; independent capacity PASS in 200.046s. Review and dedicated tests committed/pushed at `2cf2f57`. Final root gates remain required. |
| Final `make infra-up migrate migration-status` | PASS18.003s: local dependencies healthy; forward migration52 applied. |
| `make browser-install` | PASS0.038s using the verified available browser. |
| Final `GOFLAGS=-v make check test build` | PASS523.800s:246 integration groups in479.806s, zero failures/skips;97 frontend tests, Go race/unit/script/static checks and all binaries/frontend build pass. Actual pre49 retained API enabled for choice/native/search/unique compatibility. |
| `make test-e2e` | PASS17.523s:9 application browser cases. |
| `make test-signin-e2e` | PASS8.499s:7 sign-in browser cases. |
| Final numeric/scalar/choice/native/search/write/unique capacity gates | IN PROGRESS; not claimed passed yet. |

A checksum-pinned older API was built from archived commit `197fe85` into ignored
`.local/reports/r3c-legacy-api`, SHA256
`bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c`.
The independent reviewer actually launched this artifact against populated
compatible legacy1.0 bindings with installed constraints. Reads, unrelated PATCH,
nullable CREATE, clear and DELETE pass. Fresh and duplicate non-null claims both
return the same generic503 because the older handler predates42501 translation.
That limitation is retained; it does not reveal whether a value exists.

Initial independent read-only review identified a retry regression: translating
every SQLSTATE23505 into a business conflict would swallow receipt-key races
handled by KnownAbort. Translation is now restricted to the exact
runtime.unique_claims/unique_claim_value constraint. The reviewer reproduced the
pre-fix409 against archived `1f8ab79`, then verified eight concurrent callers all
receive the same successful receipt with one business/audit/outbox fact at
`d15be92`. No arbitrary uniqueness violation is retried. See the
[independent report](../verification/M9R_R3C_INDEPENDENT_REVIEW.md) for exact
commands, coverage, retained failures and optional probes not executed.

Independent capacity on PostgreSQL18.6/four CPUs/8GiB/disabled swap verified
100k source records and nine other populated tenants through203 checkpoints.
Fresh/conflict CREATE p95 was144.937/68.700ms. The32-client60s writer completed
10,365 writes, p95215.076ms and zero errors, below the unchanged500ms target.
These local reference measurements do not certify arbitrary production workloads.

Final root retained regressions use a separate actual pre49 API built from
archived `f99537f`, SHA256
`03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19`.
It was compiled without switching/resetting the checkout and launched by the
populated compatibility tests. Set `M9R_LEGACY_API_BINARY` and
`M9R_LEGACY_API_SHA256` to that ignored artifact for the final runner. All246
integration groups passed without skips, including the actual retained choice,
native, search and unique paths. A newer artifact was not substituted for the
pre49 compatibility claim.

## Recovery and remaining gates

Inspect each target's migration history before deployment. The missing preceding
implementation's migration bytes cannot be assumed equal to the reconstructed
52; never replace an already applied migration on another target. Reconcile any
such target with its exact applied bytes and use a reviewed forward migration.
Current local targets were verified through 51 before this change.

Preserve registry, claims, checkpoints, records, receipts and immutable history.
Reconcile GET after unknown outcomes; never blindly restart or clear evidence.
Abort only an incomplete exact attempt. Repair duplicate source data through an
explicit active compatible non-unique binding over the same physical generation,
then bind the unique contract and start the next generation. No READY constraint
drop, destructive down, automatic cleanup or uniqueness-preserving schema copy
is provided. Quiesce affected writes and forward-fix on trigger/helper failures.

R3c remains unaccepted until independent executable review, migration/role/legacy
and fault/race probes, representative capacity and final regression gates pass.
R3d, broader R5 deployment/UI, R6 authority and R7 operations remain pending.
