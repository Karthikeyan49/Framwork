# M9R R3d independent adversarial review

19 October 2026 (Asia/Kolkata). Status: bounded functional review PASS, overall
R3d/M9R PARTIAL. The latest frozen-source run passes all23 ordinary groups
together: twelve independent functional/fault/plan groups and eleven existing
aggregate regressions, with zero skips and the checksum-pinned retained executable.
A focused extension also passes nested live-scope/core-ID selection within an
over-cap physical namespace. An explicit driver-panic diagnostic previously
FAILED and remains an unresolved R7 infrastructure finding. The implementer's
latest24-case capacity PASS is separate evidence; this reviewer did not repeat
that workload. Wider profile and overall R3d/M9R acceptance remain pending.

ADR056 re-verification additionally passes the same22 ordinary regression groups
on stable `d7b64dd3acc56d387bc697a029accab863e03d24` and a separate corrected
317-row canonical normalized-plan probe. See the retained failed plan assertion
and exact separate results below; no new large capacity run was performed.

## Scope and stable source

The reviewer independently assessed ADR046–054 and read the aggregate kernel,
canonical source planning, numeric projection migrations, policy/transaction
proofs, configuration composition and benchmark fixtures. The initial executable
review target was `a0ede6f`; executed review uses stable
`f4d9890e1d93a300b18373b08617b3b15425a4a9` including ADR055's default3 admission.
The implementer
retains product ownership; this reviewer edits only the new review test/report.

The existing checkout and forced-RLS modular-monolith architecture remain in
place. No production database, customer records, main merge or M10 is authorized.
Every independent database probe uses the existing disposable local fixture,
including parsed effective driver targets and connected database/role checks.
Additional traced pools compare the connected database and role before arming
faults. Test reports never print credentials, SQL parameters, business values or
raw driver error payloads.

## Independent design findings

1. Checking only the final numeric sum misses invalid individual inputs that
   cancel. The accepted design now validates every raw input or uses an exact
   versioned transactional validity/value/source witness.
2. Canonical specialization must inspect real mapping in the certified snapshot,
   including scratch namespaces adopted after their Scope was constructed.
   Retained and FROZEN reads must not require the active write generation.
3. A witness must bind the projected numeric value as well as source validity
   and row version. Legacy NULL, invalid false and certified true are distinct.
   The trigger preserves existing build/write lock ordering.
4. The expression-setting defer originally attempted SQL while a driver panic
   unwound. The implementer changed it to preserve the panic and leave rollback
   to the owned transaction. Ordinary callback panic now passes an executed
   rollback/connection-reuse regression. An injected panic inside pgx's driver
   tracer instead exposes a separate database/sql lock-recovery limitation;
   see the unresolved finding below.
5. Identical numeric values across old/current benchmark generations did not
   prove historical-value selection. The final fixture differentiates them with
   a governed current-only update and current canonical owner/share/tombstone
   changes; actual final capacity verification is pending. The independent
   paused-snapshot test additionally forces a NULL old-generation witness so
   the historical late raw-value lookup is exercised after the current value
   changes.

Reference cache/admission settings are explicit workload conditions, not more
authorization or proof of general production capacity. Shared-buffer misses do
not themselves prove physical disk I/O. The measured generic-plan timeout
supports custom planning for that query, not attribution of every latency tail
to generic plans. Keep all failed experiments and raw/unindexed limitations in
the implementation report.

## Independent executable probes prepared

The new `tests/integration/m9r_aggregate_review_test.go` contains eleven groups:

- Populated missing/mismatched dataset and generation pins, including adopted
  scratch scopes.
- A certified snapshot held across real cutover plus owner, business value and
  tombstone mutations, followed by fresh old/current/scratch reads.
- Canonical governed sharing, team membership and revocation, populated stale
  physical share distractors and missing canonical identity denial.
- An ordinary callback panic after an actual uncommitted write, rollback and
  reuse of the same PostgreSQL backend with its original settings.
- Early authorization/shape/value errors and preservation of prior settings.
- Mixed certified/legacy/invalid/stale/missing projections, including invalid
  inputs outside the entity scope and projection-independent counts.
- Readiness trigger/function/column-privilege drift and raw worker/dispatcher
  denial.
- Actual registered API fallback/explicit admission limits, including unlimited
  and small pools and database pool-wait observations.
- Corrupt historical integer/money input and caller-forged certification flags,
  with both false and legacy NULL witness paths.
- Over-cap settings restoration and actual PostgreSQL statement timeout with
  transaction-local settings reset on pool reuse.
- An observed numeric build/write/delete lock overlap, then exact projection,
  source-version, validity and sum checks.

The explicit `integration && driverpanic` file contains the separately executed
failing infrastructure diagnostic. It is not in the default integration build;
there is no default SKIP and no assertion of driver-panic recovery.

## Executed evidence and retained failures

The reviewer waited for the exclusive execution handoff. No implementation edit,
competing root build or root database workload occurred during execution.

| Executed command or probe | Result |
| --- | --- |
| `.local/toolchains/go/bin/gofmt -w tests/integration/m9r_aggregate_review_test.go` | PASS, formatting only. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR3d' -v ./tests/integration` | FAIL 42.563s: six groups passed; five probes failed before their intended assertions because the review helper resolved identity twice, correctly clearing its trusted context. This was a reviewer harness defect. |
| Same initial command after fixing the helper | FAIL, manually terminated at133.870s after an injected driver panic hung in database/sql rollback. The captured stack identifies the leaked transaction lock. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR3d(Pinned\|Snapshot\|Canonical\|Early\|Mixed\|Readiness\|Admission\|Certification\|Budget\|Numeric)' -v ./tests/integration` | PASS 44.332s: ten groups, zero skips. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestIndependentR3d(Snapshot\|Ordinary)' -v ./tests/integration` | FAIL 5.475s: strengthened retained raw-fallback snapshot test passed; the new callback fixture attempted forbidden app header INSERT columns and stopped before its panic. Corrected to the actual allowed INSERT columns. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration,driverpanic -timeout=30s -count=1 -run '^TestIndependentR3dPanicDoesNotIssueRestorationSQLAndPoolRecovers$' -v ./tests/integration` | FAIL 8.576s: a supervised child could not finish rollback/reuse after an injected driver panic. The parent retained owned disposable cleanup. This failure remains unresolved. |
| `sha256sum .local/reports/r3c-legacy-api` | PASS: `bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c`; retained source `197fe85`. |
| `M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-legacy-api M9R_LEGACY_API_SHA256=bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestIndependentR3d\|TestM9RSQLAggregate)' -v ./tests/integration` | PASS 88.328s: all22 groups, zero skips, including all11 independent groups, same-transaction mode restoration, actual cancellation/restoration faults and the retained executable reader. |
| `git diff --check` | PASS. |

Ignored execution logs are retained under `.local/reports/r3d-independent-*`.
The owned process terminated for stack capture was PID909768; no broad process
kill or customer database cleanup was used. The original abruptly terminated
fixture may leave a disposable database; later supervised diagnostic cleanup
belongs to its parent test. No claim of universal interrupted-fixture cleanup is
made.

## Unresolved finding IR-R3d-01: injected driver panic recovery

Severity: MEDIUM infrastructure reliability; tracked as R7 follow-up. The default
`postgres.Open` connector has no custom pgx query tracer. No tenant-supplied
metadata, query value or workflow selects this injected tracer or grants SQL
authority. Ordinary callback panics are a different supported case and pass the
executed rollback/connection-reuse test.

An actual panic from `TraceQueryStart` occurs inside database/sql's QueryContext,
before it releases the transaction's `closemu` read lock. The aggregate defer
preserves the panic, but WithRuntime's deferred `Tx.Rollback` waits on the leaked
lock. The advertised transaction deadline cannot repair a leaked Go mutex.
Captured stacks show `database/sql.(*Tx).rollback`, the aggregate defer and the
panicking driver/tracer call. The explicit child-process diagnostic preserves a
failing recovery assertion and exits under supervision. It does not demonstrate
successful rollback, bounded recovery or pool reuse for arbitrary driver panics.

Do not add an ad-hoc rollback goroutine timeout and call that recovery. A supported
driver-guard/process-recovery boundary requires separate design and verification.
Until then, keep the default connector's supported configuration explicit and
avoid broad panic-recovery claims. This review does not claim the injected
infrastructure fault is resolved.

## Results and handback

Independent mapped or unmapped capacity runs are NOT RUN in this window. The
reviewer inspected the implementer's failed canonical benchmark plans: selective
filtered and SHARE paths perform repeated current-identity point lookups. Root's
earlier capacity failures and its720.050s expanded-suite timeout remain failures;
later unexecuted cases remain NOT RUN. Default3 and any subsequent canonical plan
change require fresh actual evidence; correct small fixtures are not a capacity
PASS.

The ordinary callback positive first failed only because the reviewer fixture
explicitly inserted actor/owner header columns that the app role correctly
cannot write. After using the actual allowed INSERT columns, the final run
demonstrated rollback of that uncommitted source row and reuse of the same
PostgreSQL backend with original settings. Neither harness correction changed
product code or weakened an assertion.

Root check/test/build, browsers and older workload gates are NOT RUN by this
reviewer and remain separate completion obligations. No production execution,
general reporting/optimizer support or M10 is certified. Product semantics passed
the completed populated functional probes; broader R3d/M9R remains PARTIAL.
No review execution remains active at handback. The proposed subsequent ADR056
canonical identity boundary and CTE inlining were not implemented at the initial
handback. The following section records the later stable-source re-verification.

## ADR056 stable-source re-verification

The implementer committed the prior design before changing the canonical identity
namespace boundary and making the aggregate CTE single-use/inlinable. The
reviewer then received another exclusive execution window at
`d7b64dd3acc56d387bc697a029accab863e03d24`. Product source remained frozen; only
the new independent plan test and this report changed during the review.

The canonical identity relation still includes exact trusted tenant, app,
dataset and entity predicates plus NOT tombstone. Its unique key bounds each
source match to one canonical identity. The new OFFSET0 boundary changes planning,
not tenant membership, scope, shares, ownership or historical physical values.
The normalized projection and inner100001 limit remain, and every cancellation/
timeout tracer matches the new prefix with its fault-firing assertion intact.

| Executed command or probe | Result |
| --- | --- |
| `sha256sum .local/reports/r3c-legacy-api` | PASS, unchanged retained artifact checksum recorded above. |
| `M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-legacy-api M9R_LEGACY_API_SHA256=bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestIndependentR3d\|TestM9RSQLAggregate)' -v ./tests/integration` | FAIL 87.352s overall: the same22 prior ordinary groups all PASS, zero skips; the new23rd plan probe failed an overly strict reviewer assertion about CASE text in Aggregate.Output. No semantic, permission, snapshot, cancellation or settings failure occurred. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestIndependentR3dCanonicalPlanStreamsOneNormalizedProjection$' -v ./tests/integration` | PASS 11.981s after correcting that plan assertion; both actual retained/current generated SQL plans and exact differentiated decimal sums were checked. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration,driverpanic -timeout=30s -count=1 -run '^TestIndependentR3dPanicDoesNotIssueRestorationSQLAndPoolRecovers$' -v ./tests/integration` | FAIL 7.272s; the broadened tracer still reaches the injected driver fault, and bounded subprocess supervision preserves the unresolved recovery assertion. This diagnostic is outside the ordinary integration build. |
| `git diff --check` | PASS. |

The failed structural assertion treated CASE text printed in the top Aggregate
as proof of repeated evaluation. EXPLAIN VERBOSE expands child OUTER-slot target
expressions while deparsing. The actual populated plans contain a single
one-column normalized Subquery Scan below Aggregate, executing once. Both plans
show exactly one record_identity scan with Actual Loops=1 and zero CTE Scan
nodes. The corrected test checks those structures rather than rejecting
deparsed CASE text. Only sanitized node kinds, aliases, loop counts and counts
of output columns/CASE text are logged; query literals and parameters stay in
memory. This is a small functional plan check, not a100k capacity result or proof
that the planner will choose the same physical join on every workload.

The original22 regression groups and the corrected new plan group passed across
the stated executions; no single combined23-group PASS is claimed. Large mapped/
unmapped capacity, older workload gates, root check/test/build and browser gates
remain NOT RUN by this reviewer. IR-R3d-01 remains unresolved and overall R3d/M9R
remains PARTIAL.

## ADR059 final frozen-source re-verification

The final exclusive window uses product source
`f9cb4fa798843e66db3d4c53ad57b7c7bc67dcc2`, including ADR058's provisional
reference-v2/default4 configuration and ADR059's physical namespace boundary.
No implementer source edits, competing database workload or root build occurred
during this window. Only this independent report and review-test assertions
changed. Earlier results and failed diagnostics above remain historical evidence;
the latest combined command below now provides the previously missing single
23-group PASS.

Read-only comparison confirms that physical records are constrained by all five
trusted tenant/app/revision/hash/entity keys inside OFFSET0, without an inner
limit. Outer DataScope and caller predicates still precede LIMIT100001. The
relation retains JSON for nested policy conditions, while current canonical
ownership and tombstones still come from record_identity. Missing/stale numeric
witness denial, exact retained-generation fallback, normalized scalar projection
and transaction-local mode restoration are unchanged. Exact small retained and
current generated plans still have one normalized projection and one identity
scan/loop with no CTE Scan. These are functional plan observations, not a claim
about every physical plan or large-workload latency.

LOW finding IR-R3d-02 is closed at this source: backend boundary diagnostics now
apply current_database() to both active clients and autovacuum workers. The
previous OR branch counted autovacuum workers from other databases. No SQL,
parameters, database identifiers or business values are emitted. GC/cgroup
counters and boundary wait counts remain passive correlations, outside the
individual request timers; they do not prove the cause of a latency tail.

The actual API registration probe verifies default4 with pool10 and an unlimited
pool, fallback2 with a two-connection pool, and preserved explicit1/5 injections.
No hidden pool waiting occurs in that fixture. Config race tests additionally
verify default4, valid1/4/10 and rejection of explicit0 and other invalid values.

Executed commands and results:

```sh
sha256sum .local/reports/r3c-legacy-api
```

PASS: unchanged checksum
`bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c`,
retained source `197fe85`.

```sh
set -o pipefail
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-legacy-api M9R_LEGACY_API_SHA256=bdcbffb3be0e7bda6eb4d29fe4f30b6b9bcc63acbf3a85680e4809b5864ef48c node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestIndependentR3d|TestM9RSQLAggregate)' -v ./tests/integration 2>&1 | tee .local/reports/r3d-independent-adr059-final.log
```

PASS60.415s: all23 ordinary groups, zero skips, including the actual retained
executable reader, real SQL timeout/cancellation, ordinary callback rollback,
fault-firing assertions, pinned snapshots, canonical ACLs and settings cleanup.
The log-count assertion also PASSes:

```sh
awk '/^--- PASS:/ {passed++} /^--- SKIP:/ {skipped++} /^--- FAIL:/ {failed++} END {printf "passed=%d skipped=%d failed=%d\n", passed, skipped, failed; exit !(passed==23 && skipped==0 && failed==0)}' .local/reports/r3d-independent-adr059-final.log
```

The reviewer then strengthened the existing independent budget/timeout group,
reusing its100001-row disposable fixture rather than creating a capacity run.
A live role with nested OWNER AND CONDITION narrows the namespace to7142 inputs;
its exact integer sum passes. A core-ID filter returns exactly one included
record and excludes a populated out-of-scope record. This closes the specific
gap between existing over-cap caller-filter coverage and smaller nested-scope
fixtures. The extension changes no product code or existing assertion.

```sh
.local/toolchains/go/bin/gofmt -w tests/integration/m9r_aggregate_review_test.go
set -o pipefail
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestIndependentR3dBudgetRestorationAndActualStatementTimeout$' -v ./tests/integration 2>&1 | tee .local/reports/r3d-independent-adr059-scope-cap.log
./scripts/go.sh test -race ./internal/config
git diff --check
```

PASS: formatting; strengthened integration group12.831s with zero skips;
config1.019s; whitespace check. Product source remains unchanged. The combined
23-group run preceded this test-only extension; the strengthened group was then
executed separately, without implying a repeated combined run.

The implementer reports `make test-aggregate-performance` PASS619.747s: all24
raw/current/retained cases,100k hot rows, ten offered clients,100 samples/case,
zero errors and worst HTTP p95712.030ms. This is root-observed evidence, not an
independent workload execution or acceptance of the wider profile. Independent
large capacity, root full checks/build, browsers and all seven earlier capacity
targets are NOT RUN in this review window and remain parent-owned obligations.
The injected-driver-panic diagnostic is NOT RERUN here; its previous executed
FAIL and unresolved IR-R3d-01/R7 status are preserved. No arbitrary driver-panic
recovery or general production readiness is claimed. No independent execution
remains active at handback.
