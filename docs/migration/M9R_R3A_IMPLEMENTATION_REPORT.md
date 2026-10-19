# M9R R3a implementation and verification

Report date: 14 October 2026 (IST). Test captures: 13 October 2026 (UTC).

Current update: the [R3b numeric index](M9R_R3B_IMPLEMENTATION_REPORT.md) now
passes the unchanged 100k-row/ten-client HTTP gate independently (p95 96.713 ms).
The R3a results below are retained historical evidence, including the failed
unindexed path. Full R3 and full dataset schema evolution remain open.

**R3a STATUS: PARTIAL at the current checkpoint.** The original AMD reference
passed; the resumed Intel host exposed HTTP misses under the same database limits.
The added page-fetch optimization has a passing root run, but the independent
current-host run failed the HTTP gate. SQL membership/authorization evidence and
the focused correctness/security regression selection remain verified.
At this historical checkpoint R2 continuity and R4 concurrent writes had not
been implemented. Equal-schema continuity and bounded concurrent writes now
have separate independently verified reports. R3b–R3d remain separately scoped.
This does not declare the system production capable or permit M10.

## Change and recovery

Migration 27 keeps forced RLS, tenant equality, live membership checks, owners,
grants and keys. Uncorrelated membership/context subqueries become PostgreSQL
InitPlans instead of running for every scanned business row. The migration-only
installer applies the same rule to future tables. Team hierarchy and the R1
worker capability predicates are preserved. There is no cross-request identity
cache; revocation retains the existing transaction snapshot semantics.

No customer records are copied, changed or deleted by this migration. Local
forward migration succeeded. Failed migration rolls back atomically; deployed
recovery must use a reviewed forward policy change with authorization negatives.
See [ADR025](../adr/025-m9r-membership-evaluation-and-performance-floor.md).

## Commands actually executed

| Exact command | Result |
|---|---|
| `docker update --cpus 4 --memory 8g --memory-swap 8g framework-m0-postgres-1` | PASS; owned local container limited to the preselected reference tier |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RStatement' -v ./tests/integration` | PASS 1.872 s initially; strengthened populated/valid-actor negatives PASS 1.968 s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration,performance -count=1 -run '^TestM9RMembershipPerformanceFloor$' -v ./tests/integration` | First FAIL 7.269 s: incorrect assumption that the old policy must always time out. Next FAIL 35.291 s: SQL gates passed, race-build HTTP p95 838.674 ms exceeded 750 ms. Both failures retained |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -run '^TestM9RMembershipPerformanceFloor$' -v ./tests/integration` | Ordinary-build PASS 32.421 s, then PASS 35.914 s with resource/cluster verification. Independent final corrected UUID-order run PASS 27.622 s at 376eb9c |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM5\|^TestM6R\|^TestM9RStatement' -v ./tests/integration` | PASS 17.700 s, typed CRUD/query/CAS, legacy migration preservation, release gates and owner/team/hierarchy/share/condition DataScope |
| `make check test-unit` | PASS: Go format/vet/dependency verification, source kit checksums 84+43, Go race tests, 5 Node tests, 28 frontend tests, lint/typecheck |
| `make migrate` | PASS: one additive migration applied locally; disposable fixtures also rehearse the complete chain and repeat no-op |
| `git diff --check` | PASS |
| Final complete R1–R4 regression / remote CI / production deployment | NOT RUN; final foundation work is still in progress |

After host resumption (Intel Xeon Platinum8573C rather than AMD EPYC9V74),
`make test-performance` FAILED45.603s, HTTP p95 1485.555ms. A diagnostic run with
`node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -run '^TestM9RMembershipPerformanceFloor$' -cpuprofile .local/m9r/r3a-current-host.cpu ./tests/integration`
FAILED32.568s, HTTP p95 858.323ms. The Go profile recorded2.53s CPU in32.54s.
These failures are not explained away as race overhead.

At42e1f26 custom sorts first select authorized IDs/keys in one MATERIALIZED CTE,
then fetch their complete rows in the same statement/snapshot. Main DataScope,
filter and cursor predicates precede LIMIT; the fetch matches exact namespace and
selected IDs. Worker and UUID paths stay unchanged. Initial narrow-page benchmark
FAILED37.459s (HTTP p95 901.114ms). After adding actual runtime/generic plan
diagnostics, `make test-performance` PASSED30.538s (p95 658.981ms,p99 725.121ms,
21.80 requests/s,0errors). That diagnostic shows a48-byte selected sort tuple and
101 primary-key fetches. Hardware/run variation and all failures remain explicit;
one passing run does not establish a universal host-independent guarantee.

`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM5\|^TestM6R(SQLDataScope\|Nullable)' -v ./tests/integration`
PASSED12.375s. The new custom-sort/null/tie/ASC/DESC/cursor/join/hidden-field and
conditional-scope test passed 3.064 s. Independent review of 42e1f26 passed 15
focused race-test roots in 29.326 s, with no confirmed safety regression. Its
single isolated ordinary performance run FAILED in 40.142 s: HTTP p95 899.709 ms,
p99 995.374 ms, 17.00 requests/s and zero errors. SQL p95 values were 2.778 ms
(key), 215.011 ms (numeric) and 137.407 ms (selective); all SQL gates passed.
The captured runtime plan confirms the bounded page and one membership evaluation
per InitPlan. This independent failure leaves R3a PARTIAL; no further run was
selected to obtain a passing sample and no target was raised.

Race instrumentation is used for correctness, not ordinary service latency.
No latency target was raised. The categorical baseline-timeout assertion was
removed because the old policy's outcome depends on cache/hardware/workload.
It timed out in some runs and completed near 2 s in others.

## Historical independently verified AMD reference floor

PostgreSQL 18.6, 4 CPU limit, 8 GiB memory, swap disabled; 128 MB shared buffers and 4 MB
work_mem. The fixture verifies that the connected server is the constrained
container. Image digest and host limits are in the independent report. Ten
tenants contain 190k records, 100k hot and 10k each elsewhere; hot rows average 761
bytes with mixed owner/team/share facts. Each case has three warmups and 100
samples. Statement/lock budgets remain 2000/500 ms.

| Workload | p95 ms | p99 ms | Target | Result |
|---|---:|---:|---:|---|
| UUID key page SQL |0.510|0.522|≤100 ms p95|PASS|
| Custom money numeric sort SQL |95.441|113.052|≤500 ms p95|PASS|
| Selective filter/sort SQL |60.730|74.558|≤500 ms p95|PASS|
| Numeric HTTP query, 10 clients |658.923|706.252|≤750 ms p95|PASS|

HTTP throughput was 20.50 requests/s with zero errors. Every HTTP sample checks 100
expected tenant-specific IDs, amounts, currency, names, sort order and a cursor.
VERBOSE EXPLAIN identifies the membership-specific InitPlan with Actual Loops=1.
The ordinary handler/auth/policy/audit path runs through an HTTP fixture; this
does not measure production TLS/ingress, cold caches or a long open-loop workload.

Independent review also tested populated foreign tenants, valid actor after
membership revocation, repeated A/B actor/context switching in generic prepared
queries, RR snapshot semantics, pooled cleanup and R1 worker regressions. It
found and corrected evidence weaknesses in these tests and the SQL ordering
alias, rather than accepting zero-row or any 200 response as proof. Exact commands
and findings: [independent review](../verification/M9R_R3A_INDEPENDENT_REVIEW.md).

## Remaining work

The numeric query still scans/sorts JSONB. SQL targets passed at 100k on both
hosts; the HTTP target is not consistently met on the current host. Historical
results do not establish current-host acceptance or arbitrary scale. Stable
datasets, writer contention,
product policy, verifier authority and operations remain R2/R4/R5–R7. No new
major dependencies were introduced. Source, migration, tests and reports belong
in Git; credentials, local fixtures/logs, binaries and generated builds do not.
