# R3b — Governed full-text search

16 October 2026 (UTC). **Bounded full-text search slice STATUS: PASS.**
Started from clean `51e616d` on `remediation/m9r-foundation`. No main merge,
production activation or M10. Complete R3b and M9R remain PARTIAL.
Implementation `d61f696`; independent review and final tested source `29a61ac`.

## Contract and changes

ADR043 was written and independently reviewed before code. Additive migration
51 introduces separate fixed search registry/projection tables and GIN index,
NOLOGIN ownership, forced tenant RLS, restrictive worker denial and read-only app
grants. Registration/build use live manager/source-binding checks, shared eight
indexes/entity and 32/namespace quotas, audit atomicity, checkpoint reconciliation
and independent 1000-row copy/verification. Database-owned triggers atomically
maintain vectors/source versions; deletion cascades. Existing migrations/source
records, immutable IR/compiler formats and numeric/scalar converters are retained.

The optional Query.Search contract accepts one non-intrinsic string field and
fixed `simple` plain AND-of-words semantics. Existing `contains` remains literal
substring matching. ID-ordered pages allow typed filters and one declared join;
sort/aggregate combinations are rejected. Search input is at most 256 UTF-8 bytes
and 1–31 tsquery nodes (at most sixteen AND operands). PostgreSQL punctuation and
long-word semantics apply; source values retain their 4096-byte contract.
No search UI, ranking, dictionaries, language selection or caller tsquery syntax.
No new dependency or lockfile change.

Input field action/scope checks and record DataScope precede LIMIT, including when
the search field is not projected. Certified indexed reads match exact source
versions and canonical live-view point fetches. Missing/incomplete indexes use
bounded source fallback; incompatible registered witnesses fail closed. Workers
reject interactive search. The appended omitempty query field preserves old
query/cursor serialization when absent. Executed retained-binary evidence follows.

Normal RLS kept non-leakproof full-text matching above the security barrier in an
owned 100k-row probe (10.901ms, no GIN). An additional restrictive match policy
enabled GIN (0.612ms) while preserving independent tenant/membership predicates.
`app.search_text` only restricts matching vectors; it is never credentials or
authorization. The runtime sets validated bound text on the same transaction,
retains the SQL match and clears context at entry/exit and pool boundaries.
SQL-aborted transactions roll back. Readiness checks exact restrictive policy,
grants, owner, helper signatures, fixed index and trigger health.

Parser/config/dictionary/function mappings, empty dictionary options, exact server
version, encoding, database locale/provider and actual collation version form a
registry witness. Query/build/maintenance reject drift. Upgrade repair/rebuild
requires reviewed forward recovery; automatic reindex/reinterpretation is absent.

## Executed checks and retained failures

| Exact command | Result |
| --- | --- |
| `make infra-up migrate migration-status` | PASS baseline: dependencies healthy; migrations 1–50 retained. |
| `make check test-unit` | PASS baseline: static/checksum/vet/lint/typecheck, Go races, seven Node script tests and 96 frontend tests. |
| `make migrate` | PASS: migration 51 applied. It is frozen; later SQL changes must be additive. |
| `./scripts/go.sh test ./internal/runtime/data ./internal/platform/access ./internal/platform/postgres` | PASS focused Go packages. |
| Focused populated search integration commands | First FAIL: direct-query test omitted its required ScopeResolver and got ErrDenied. Corrected test setup without weakening product defaults; four-group reruns PASS (16.309s and 16.631s). Exact stable command/evidence follows below. |
| `make test-search-performance` | FAIL initial (34.839s), second (34.238s) and diagnostic (32.749s): first seven cases passed exact oracles/GIN/latency; joined case returned HTTP503. Diagnostic captured only SQLSTATE57014, confirming the unchanged 2s statement timeout. No raw driver error/SQL arguments logged. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RSearch' -v ./tests/integration` | PASS stable root check: four populated groups, 17.173s, zero failures/skips. |
| `make test-search-performance` after target-join fencing | PASS 43.926s: eight cases/800 measured requests, zero errors, worst p95 201.966ms/p99 233.561ms. Hot 100k plus nine 10k-row tenants, 1002-byte records, ten clients, unchanged p95<=750ms. All use GIN; source visits 0–1114 according to candidate/page count. Search copies/verifies 100k rows in 203 checkpoints, 20.570s build. |
| `make check` before implementation commit | PASS static/checksum/vet/dependency/lint/type checks. |
| Independent executable review | PASS on stable `d61f696`: combined 55 retained groups (67.600s), nine adversarial race groups (23.497s), exact verification-conflict recheck (2.642s), fresh/repeated migrations 1–51 (0.687s), all eight search capacity cases (34.174s), no final failures/skips. Commands and initial review-only failures are in the linked independent report. |
| Artifact variables below + `GOFLAGS=-v make check test build` | PASS against `29a61ac`: 230 integration tests in 791.751s with zero failures/skips, 96 frontend tests, seven Node script tests, static/race/dependency checks, eight Go binaries and frontend production build. Complete command 828.528s. |
| `make infra-up migrate migration-status` final | PASS 2.033s: dependencies healthy, repeated up applies zero migrations, migrations 1–51 retained. |
| `make test-e2e` | PASS 9/9 application browser cases; command 19.141s. |
| `make test-signin-e2e` | PASS 7/7 sign-in browser cases; command 8.957s. |
| `make test-performance` | PASS 42.169s: numeric HTTP p95 110.682ms, p99 154.032ms, throughput 126.70/s, zero errors. Expected pre-fix RLS baseline SQLSTATE57014 under the unchanged 2s timeout is preserved as a negative, not hidden. |
| `make test-scalar-performance` | PASS 75.301s: twelve cases/1200 requests, zero errors, worst p95 302.545ms/p99 353.247ms, throughput 42.90–118.58/s. Existing 904-byte fixture retained; build 44.741s. |
| `make test-choice-performance` | PASS 91.356s: twelve cases/1200 requests, zero errors, worst p95 297.361ms/p99 308.340ms, throughput 47.68–129.29/s. Existing 926-byte fixture retained; build 62.624s. |
| `make test-native-performance` | PASS 87.024s: twelve cases/1200 requests, zero errors, worst p95 249.209ms/p99 280.445ms, throughput 55.83–126.75/s. Existing 1013-byte fixture retained; build 57.823s. |
| `make test-search-performance` final | PASS 42.151s: eight cases/800 requests, zero errors, worst p95 203.519ms/p99 232.788ms, throughput 73.30–160.62/s. All use GIN and bounded source visits; 1002-byte fixture, build 19.671s. |
| `make test-write-performance` | PASS 61.458s: 32 clients for 60 seconds, 11989 successful distinct-record writes, zero errors, p95 192.061ms/p99 210.600ms, throughput 199.36/s. |
| `make docs-verify` and `git diff --check` | PASS after final gates; source kits retain all 84 V2 and 43 V3 checksums. Repeated after final documentation edits. |

The capacity failures exposed an existing unfenced target-view join. The
fix uses a correlated LEFT JOIN LATERAL point lookup with OFFSET 0 while retaining
target namespace, DataScope, masking and null behavior. The same 100k-row case and
retained permission/join regressions now pass independent and final correctness
checks; budgets/fixtures are unchanged. The post-fix capacity rerun passes above.

## Independent and final evidence

The separate [executed review](../verification/M9R_R3B_SEARCH_INDEPENDENT_REVIEW.md)
found no high/medium product defect in the inspected bounded search contract.
Nine new populated groups execute actual successful-COMMIT reply loss with GET
reconciliation, observed source-lock overlap with valid PATCH/DELETE, current and
retained ownership/sharing/tombstones, returned joined values/nulls/target DataScope,
raw populated tenant/worker/private ports and grant drift, parser/schema/required
witness faults, exact restrictive-policy readiness, shared last-slot quota race,
opaque fallback/strict point/hidden input/cursor/context cleanup and wrong-vector
verification rejection without checkpoint advancement. Restored vectors verify
successfully. Operator corruption probes test detection, not operator confinement.

A checksum-pinned older API executes actual startup/readiness, populated reads,
PATCH/CREATE/DELETE maintaining vectors/versions, old-cursor to new-API continuity
when Search is absent, and graceful exit after migration 51. It receives only API
bindings and public fixture JWKS, without operator or worker credentials.
These three retained executable suites are opt-in and explicitly SKIP without the
artifact in a clean checkout. Final root verification enables the exact artifact
and executes all three; it is ignored and never committed. This does not certify
every historical deployment/UI operation, schema downgrade or backup/DR path.

Exact retained-artifact variables for the final full command:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3b-enum-reference-legacy-api \
M9R_LEGACY_API_SHA256=d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea \
GOFLAGS=-v make check test build
```

The independent reference workload verified PostgreSQL 18.6 with four CPUs,
8GiB/swap off, one hot 100k-record tenant plus nine 10k-record tenants, mean hot
record 1002 bytes. It copied/verified 100k records in 203 checkpoints (11.849s).
Eight cases each run three warmups, 100 measured requests and ten clients. All
800 requests pass exact source-ID/cursor oracles, zero errors, worst p95 199.937ms,
p99 269.398ms. Every plan uses GIN; source visits range from zero to 1114 according
to candidate/page count. The 750ms HTTP p95, 2s statement and 500ms lock budgets
are unchanged. This is selective-term capacity; broad/common terms, general join
plans, ranking/languages and arbitrary deployment scale need separate evidence.

The independent report preserves initial unused-import, stale-fixture-CAS and
incorrect-status-assertion failures and a default-artifact SKIP. The final combined
and artifact-enabled race runs pass; no product fix was required by that review.
The final root worker receipt/policy regression passes its 96-receipt/63-node case
at 1.123s; earlier intermittent timeout/connection failures remain open R7 items.
One passing run does not close those reliability findings. Remote CI is NOT
OBSERVED; no real locale/server/library upgrade or GIN upgrade rebuild is tested.

The ignored `.local/reports/r3b-search-final-gates.mjs` runner executes the Make
commands serially and preserves exit status and duration in
`r3b-search-final-results.jsonl`, with `r3b-search-final-*.log` evidence. Root
initial/focused/capacity and independent logs remain in the same ignored directory.
Logs, runner, retained executable, env/keys, dependencies, bin/ and frontend builds
are generated artifacts, not Git content.

## Files and acceptance

- Migration `00051_m9r_full_text_search.sql` adds the fixed search tables, GIN,
  private typed converter/maintenance and governed manager/build ports, parser
  witness, restrictive match policy, shared quota and worker grant-drift checks.
  Migrations 1–50 are unchanged; no down/data-deletion path is introduced.
- `internal/runtime/data/search.go` owns typed admission, readiness, witnesses and
  physical search planning. Query/model/field/legacy/store integration preserves
  old JSON, guards and fallback. Shared joins now fence target live-view lookups.
- `internal/platform/access` exposes search-index GET/register/build under existing
  authenticated index-management authority; PostgreSQL transaction cleanup clears
  query context. Makefile adds a separate search performance gate.
- Four root integration groups, nine independent groups and a new eight-case
  capacity suite cover correctness/faults/isolation/compatibility. The shared
  query tracer records only safe SQLSTATE on failure, never SQL parameters.
- ADR043, README, AGENTS and the implementation/review reports describe the
  contract and evidence. Source kits and dependency/lockfiles remain unchanged.

Schema/IR compatibility, forced tenant RLS, live DataScope/masking, least-privilege
workers, no tenant DDL, strict inputs, projection/source consistency, checkpoint
atomicity, retained API and populated corruption/recovery checks pass in the
bounded tests. Final whole-project/static/build, both browser suites and all six
numeric/scalar/choice/native/search/write capacity gates pass against `29a61ac`.
Documentation-only closure follows the tested source. This does not certify
production activation or the complete external review. Source/tests/ADRs/reports
are committed on `remediation/m9r-foundation`; the final response records verified
remote delivery and working-tree status. Main merge is a separate task.

## Recovery and residual scope

Apply migration 51 with controlled DDL credentials. Preserve records, registry,
vectors and checkpoint evidence. Incomplete builds retain bounded fallback; GET
reconciles unknown outcomes. Invalid source/witness drift needs reviewed repair,
never deletion or silent coercion. Quiesce and forward-fix a broken trigger/helper.
There is no destructive down migration, automatic index inheritance/rebuild or
full historical deployment rollback/backup-DR certification.

R3c uniqueness, R3d SQL aggregates/general planning, broad-term search capacity,
search UI, role-definition editing, external identity/MFA deployment, R6 authority
and R7 admission/events/audit/workflow/caching/DR remain open. Current preview
capabilities do not enable production, AI or external actions. Source/tests/ADR/
reports belong in Git; generated logs/binaries/env/keys/dependencies/builds do not.
