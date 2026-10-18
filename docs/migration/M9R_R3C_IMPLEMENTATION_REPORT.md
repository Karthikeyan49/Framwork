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
| `make test-unique-performance` after schema-cache and fixture correction | RUNNING; no final capacity claim yet. |
| Independent executable source review | NOT RUN yet; required. |
| Final `make check test build`, `make test-e2e`, `make test-signin-e2e`, retained query/write gates | NOT RUN against final source yet; required. |

A checksum-pinned older API was built from archived commit `197fe85` into ignored
`.local/reports/r3c-legacy-api`, SHA256
`bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c`.
This artifact has not yet been claimed as executed against the new unique paths.

Initial independent read-only review identified a retry regression: translating
every SQLSTATE23505 into a business conflict would swallow receipt-key races
handled by KnownAbort. Translation is now restricted to the exact
runtime.unique_claims/unique_claim_value constraint; executable concurrency
reverification is still required. No arbitrary uniqueness violation is retried.

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
