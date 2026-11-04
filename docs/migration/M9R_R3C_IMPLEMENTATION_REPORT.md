# R3c — Governed unique-field integrity

18 October 2026 (Asia/Kolkata). **R3c STATUS: PASS within the bounded API foundation below.**
Branch: remediation/m9r-foundation. Reconstructed from the interruption checkpoint
at `124fe4e`; design committed/pushed at `3310880`. No main merge, production
activation or M10. Overall M9R remains PARTIAL.

Functional source: `d15be92`; independent review/tests: `2cf2f57`. Later
`bc94af0`/`819d23a` are documentation checkpoints. Final root gates execute the
same functional source plus the committed independent tests. This does not close
all original review findings or certify production readiness.

## Implemented behavior

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
content. Final evidence and earlier failed runs are both retained below.

| Command | Executed result |
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
| `make test-unique-performance` initial | FAIL 240.485s overall: 100k/ten-tenant 1509-byte case PASS (177.17s, 203 checkpoints, build 20.478s, fresh p95 470.164ms / expected-conflict p95 477.845ms). Bound 32-client writes had 3259 successes/zero errors but p95 690.646ms above 500ms; fixture also incorrectly included 32 initial create receipts in an updates-only assertion. Both failures are retained. |
| `./scripts/go.sh run .local/reports/r3c-validator-profile.go` before/after ADR045 | Initial fixed-schema construction,100 samples: mean 25.476ms versus existing-validator parse 0.583ms. Immutable compiled-schema sharing reduces construction to 0.306ms amortized including first compilation; parse 0.719ms. No source/policy/record/catalog cache. |
| `./scripts/go.sh test -race ./internal/metadata ./internal/compiler ./internal/runtime/data` after ADR045 | PASS: concurrent valid/invalid validation across all five versions, source/hash isolation and independent returned handles; retained compiler/data race tests pass. |
| `make test-unique-performance` after schema-cache and fixture correction | PASS 199.809s interim capacity run: 100k case 137.49s, 203 checkpoints/build 15.438s, fresh p95 355.755ms/p99 443.432ms (40.22/s), expected-conflict p95 74.097ms/p99 118.208ms (177.57/s), no unexpected errors. 32-client/60s bound-dataset writer: 10330 successes, zero errors, p95 219.378ms/p99 241.092ms, 171.73/s; exact source/version/receipt/outbox checks pass. Runtime statement 2s/lock 500ms/write-p95 500ms targets unchanged. The executable compiled before the narrow receipt-error translation follow-up; final stable-source gates are completed below. |
| Independent executable source review | PASS on stable `d15be92`: 12 populated adversarial groups in 20.883s, no skips; independent capacity PASS in 200.046s. Review and dedicated tests committed/pushed at `2cf2f57`. Final root gates are completed below. |
| Final `make infra-up migrate migration-status` | PASS 18.003s: local dependencies healthy; forward migration 52 applied. |
| `make browser-install` | PASS 0.038s using the verified available browser. |
| Final `GOFLAGS=-v make check test build` | PASS 523.800s: 246 integration groups in 479.806s, zero failures/skips; 97 frontend tests, Go race/unit/script/static checks and all binaries/frontend build pass. Actual pre49 retained API enabled for choice/native/search/unique compatibility. |
| `make test-e2e` | PASS 17.523s: 9 application browser cases. |
| `make test-signin-e2e` | PASS 8.499s: 7 sign-in browser cases. |
| `make test-performance` | PASS 47.470s; numeric API p95 118.359ms, zero errors. |
| `make test-scalar-performance` | PASS 75.899s; all 12 text/date cases, worst p95 264.759ms, zero errors. |
| `make test-choice-performance` | PASS 67.197s; all 12 enum/reference cases, worst p95 209.020ms, zero errors. |
| `make test-native-performance` | PASS 85.109s; all 12 datetime/bool cases, worst p95 207.305ms, zero errors. |
| `make test-search-performance` | PASS 45.120s; all 8 selective search cases, worst p95 169.357ms, zero errors. |
| `make test-write-performance` | PASS 62.509s; 32 clients/60s, 12,547 writes, p95 183.117ms, p99 204.382ms, 208.63/s, zero errors. |
| `make test-unique-performance` final stable source | PASS 199.981s; 100k case 137.13s, 203 checkpoints/build 15.134s; fresh CREATE p95 139.038ms/p99 165.857ms, 135.79/s; expected conflicts p95 95.133ms/p99 165.527ms, 159.95/s. The 32-client writer completes 10,061 updates, p95 227.569ms/p99 248.284ms, 167.25/s, zero errors. Source/version/receipt/outbox checks pass. |
| `make docs-verify` after final gates | PASS 0.038s; both frozen source kits unchanged. |

A checksum-pinned older API was built from archived commit `197fe85` into ignored
`.local/reports/r3c-legacy-api`, SHA256
`bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c`.
The independent reviewer actually launched this artifact against populated
compatible legacy 1.0 bindings with installed constraints. Reads, unrelated PATCH,
nullable CREATE, clear and DELETE pass. Fresh and duplicate non-null claims both
return the same generic 503 because the older handler predates 42501 translation.
That limitation is retained; it does not reveal whether a value exists.

Initial independent read-only review identified a retry regression: translating
every SQLSTATE 23505 into a business conflict would swallow receipt-key races
handled by KnownAbort. Translation is now restricted to the exact
runtime.unique_claims/unique_claim_value constraint. The reviewer reproduced the
pre-fix 409 against archived `1f8ab79`, then verified eight concurrent callers all
receive the same successful receipt with one business/audit/outbox fact at
`d15be92`. No arbitrary uniqueness violation is retried. See the
[independent report](../verification/M9R_R3C_INDEPENDENT_REVIEW.md) for exact
commands, coverage, retained failures and optional probes not executed.

Independent capacity on PostgreSQL 18.6/four CPUs/8GiB/disabled swap verified
100k source records and nine other populated tenants through 203 checkpoints.
Fresh/conflict CREATE p95 was 144.937/68.700ms. The 32-client/60s writer completed
10,365 writes, p95 215.076ms and zero errors, below the unchanged 500ms target.
These local reference measurements do not certify arbitrary production workloads.

Final root retained regressions use a separate actual pre49 API built from
archived `f99537f`, SHA256
`03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19`.
It was compiled without switching/resetting the checkout and launched by the
populated compatibility tests. Set `M9R_LEGACY_API_BINARY` and
`M9R_LEGACY_API_SHA256` to that ignored artifact for the final runner. All 246
integration groups passed without skips, including the actual retained choice,
native, search and unique paths. A newer artifact was not substituted for the
pre49 compatibility claim.

The exact serial invocation was:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-pre49-api M9R_LEGACY_API_SHA256=03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19 node .local/reports/r3c-final-gates.mjs
```

The ignored runner invokes each listed `make` command sequentially and sets
`GOFLAGS=-v` for the full check/test/build command. Its per-gate exit codes,
durations and logs are retained in `.local/reports/r3c-final-results.jsonl` and
`.local/reports/r3c-final-*.log`. All 13 gates returned exit 0; no source change,
assertion weakening, timeout increase or concurrent database workload was needed
between final gates. Full integration execution took 479.806s; the 523.800s figure
includes checks, all unit/script/frontend tests and builds.

## Files, migration and dependencies

The change adds `contracts/jsonschema/application-v1.4.schema.json`, compiler
`integrity.go`, runtime `unique.go`, guarded management/data API composition,
typed frontend decoding, populated integration/capacity tests and ADR044/045.
Existing metadata models, versioned restore, storage hashes, readiness and query
contracts are extended without changing historical schema bytes. No new external
dependency is introduced; shared schema graphs use the existing pinned
JSON-Schema library and Go `sync.OnceValues`.

The only new migration is forward-only
`db/migrations/00052_m9r_unique_field_integrity.sql`, SHA256
`26ff8244599eec2401df5d699d8d2b2cef949671bf2ae0c0ab261246c9492591`.
It applied in fresh populated disposable fixtures and the retained development
database. Records are adopted and verified before uniqueness binding; no source
data deletion, shared tenant DDL, broad worker grant or destructive down is added.

## Recovery and remaining work

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

Independent executable review, populated migration/role/legacy/fault/race cases,
reference capacity and the final regression/build/browser gates now pass.
The optional additional partial-copy-abort and direct SQL OWNER probes listed in
the independent report remain NOT RUN; they are not extra PASS evidence. This
bounded acceptance does not promise arbitrary-workload capacity.

Remaining API/product limits include uniqueness administration UI, READY
constraint removal, constraint-preserving shadow evolution and broader search
UI/ranking/languages/common-term capacity. R3d SQL aggregates/planning, R5
role-definition UI/external provider deployment, R6 requirement/verification/
release authority and R7 operations remain pending. Existing intermittent
workflow/worker diagnostic findings remain open; passing regressions are not
claimed as their fixes. No M10, main merge or production activation was performed.

Publication is only to `remediation/m9r-foundation`. Source, tests, ADRs and reports
are committed; ignored environment files, keys, archives, logs, dependencies and
generated binaries/builds must not be committed. The final response identifies
the verified publication hash. **M9R STATUS: PARTIAL.**
