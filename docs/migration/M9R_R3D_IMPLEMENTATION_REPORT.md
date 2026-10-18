# R3d — Bounded SQL aggregates and query planning

19 October 2026 (Asia/Kolkata). **STATUS: IN PROGRESS; not accepted complete.**
Design: ADR046, committed before source at `8ebc75d`. Branch:
`remediation/m9r-foundation`; M9R remains PARTIAL. No M10, main merge or production
activation. Existing records, immutable metadata/IR and pinned workflows remain.

The new opt-in query mode `aggregate.execution: "SQL_V1"` computes count and exact
integer/decimal/fixed-currency sums in PostgreSQL, with per-input SQL validity,
live action/DataScope enforcement and a100000-row authorized-matching input cap.
An empty execution mode preserves the original bounded aggregate behavior and
canonical request encoding. Workers, legacy adapters, opaque policies and
uncertified executors cannot use expanded aggregation. The initial implementation
preserves live-generation semantics through a snapshot-certified source planner
(ADR047). ADR048 introduces an explicit reference memory profile. ADR049 adds
migration53's nullable versioned numeric validity witness without rewriting old
projections. No new dependency is introduced. Independent executable review and
final whole-suite acceptance remain pending.

## Executed evidence and failures

- Existing Node24.19.0/Go1.27.2/Docker28.4.0 and PostgreSQL/Redis verified. All52
  migrations applied. The reused OTel container was restarting because its public
  config file was0600 after restoration. Local `chmod644 infra/otel/collector.yaml`
  and service restart restored its HTTP200 health. No tracked content or private
  key/credential permissions changed.
- Focused pre-change aggregate/field privacy baseline: PASS3 groups,4.653s.
- Independent read-only design review identified the need to validate EACH summed
  input before aggregation, including wrong currencies and invalid cancelling
  pairs. ADR046 and the implementation include this correction.
- Initial focused run: FAIL4.102s from fixture driver parameter encoding; text
  placeholders now receive text. Next two runs: FAIL5.982s/5.034s because fixture
  corruption updates lacked the trusted actor context required by the unchanged
  update trigger. Owned operator transactions now supply that context and retain
  version increments; no trigger was disabled. Three groups then PASS6.747s.
- Six populated functional groups: PASS25.483s. Exact independent arithmetic,
  legacy budgets, unsupported shapes, corruption/cancellation, policy/executor
  proof,100000/100001 boundary, filtering before cap, live OWNER/TEAM/SHARE/
  CONDITION combinations, field inference/denial, role revocation, populated
  foreign tenant and retained-generation ownership/tombstones pass.
- Initial `make test-aggregate-performance`: FAIL38.676s (test38.65s). The pinned
  four-CPU/8GiB PG18.6 fixture has100k hot records, nine other tenants with10k
  each, average1382-byte records and mixed owners/shares. The actual generated
  full count returns correct results and zero errors at10 clients, but SQL p95
  1476.616ms and HTTP p95 1527.163ms exceed the unchanged500/750ms limits. This is
  a real capacity failure. Query-plan instrumentation is being expanded before
  changing the planner; no timeout/RLS/assertion weakening is authorized.

Further executed evidence (all failures retained):

- Eight raw-path groups PASS29.864s, zero skips, including late-audit SQL abort
  and checksum-pinned pre49 API SHA256
  `03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19`.
- Planner/canonical/FROZEN/scratch-expanded eight groups PASS33.779s, zero skips.
- Three witness-era raw compatibility/proof/canonical groups PASS7.656s.
- New populated317-row/three-index witness group PASS4.170s: exact independent
  results, forged flag/value, app write/function denial, legacy NULL fallback,
  missing projection failure, count independence, atomic command/audit abort and
  actual data readiness. No destructive data migration.
- Old-view diagnostic capacity FAIL37.313s: count SQL1113.690/HTTP1277.570ms p95.
  Snapshot raw-source specialization FAIL28.376s:612.464/706.619. Rejected ORDER
  BY experiment FAIL34.661s:1017.584/1059.142 with sort/temp work.
- Explicit2GiB reference profile first run FAIL32.832s: count731.963/904.808.
  Typed normalization with repeated extraction FAIL48.240s: count464.788/523.160
  but integer sum hits SQLSTATE57014. Isolated raw scalar refinement FAIL59.348s:
  count427.910/492.782; integer sum1357.396/1398.640, then decimal timeout.
- Trusted four-slot API admission experiment (still TEN offered clients, all FIFO
  wait included) FAIL96.611s. Count161.579/471.991 and share228.434/571.621 pass;
  integer430.161/1070.955, decimal802.588/1794.876, money642.152/1432.376,
  filtered money335.116/873.720, owner money830.857/1775.645 miss at least one
  unchanged SQL500/HTTP750ms target. Zero errors in this completed seven-case run.
- First governed-index witness capacity run FAIL128.179s. Count53.106/186.458,
  integer316.058/834.514, decimal325.139/798.681, money306.239/720.142,
  filtered360.853/884.452, owner793.568/2188.610, share222.552/624.868. Zero
  errors; several HTTP and owner SQL gates remain failures. Count's plan became
  index-only during the longer build; no explicit VACUUM was run. Do not attribute
  that visibility-map/cache change purely to aggregate code.
- First gated-JSON/CacheDescribe experiment FAIL88.068s at integer warmup from an
  unused positional parameter introduced by replacing the input expression.
  Reusing the original bound field parameter fixes that query-construction
  defect. This failed run does not measure indexed-sum capacity for that driver
  mode; the corrected run remains pending. Data runtime unit race tests PASS1.330s.
- Corrected gated-JSON/CacheDescribe experiment FAIL138.132s. Full integer HTTP
  p95 remains801.510ms; decimal1045.755, money970.509, filtered797.308,
  owner893.723. Zero errors. Driver-mode changes are rejected; default restored.
- Migration54 fixed covering indexes plus exact late legacy-JSON lookup pass
  witness functional checks4.158s. Initial default-ten-slot capacity FAIL94.218s
  with integer SQLSTATE57014. Actual JIT diagnostic then PASS44.485s: same100k
  generated money statement with JIT on reports394.627ms compilation and577.394ms
  total, off reports0ms compilation and181.034ms total. This measures a real cost.
  ADR051 scopes LOCAL JIT disabling/restoration to the certified aggregate kernel.
- Same-transaction prior-on/off and actual driver cancellation at aggregate/
  restoration, result suppression and pooled reuse: PASS2 groups4.203s.
- Default-ten-slot/JIT-off capacity FAIL121.053s: count143.374/230.438 passes,
  integer755.498/819.925, decimal787.116/819.218, money759.354/804.132,
  filtered999.777/1037.248, owner1431.314/1475.345 and share748.606/806.323
  miss the unchanged targets. Zero errors. Explicit four-slot scheduling with
  this final physical/expression plan is being measured before selecting any
  application concurrency default. No accepted capacity result is claimed yet.
- Four-slot/JIT-off/header run FAIL123.770s: full sums pass (worst HTTP617.225),
  but filtered HTTP807.830 and OWNER SQL664.298/HTTP1543.657 miss; share passes.
  Projection-namespace boundary run FAIL124.575s: filtered execution times out.
- Initial prepared-plan instrumentation FAIL109.948s and FAIL97.911s from an
  observer arity error: pgx's database/sql result-format option was counted as a
  SQL parameter (12 types versus13 tracer arguments). The observer now strips
  that known option. Corrected probe/workload FAIL148.276s preserves other tails
  and directly shows the SAME filtered SUM/parameters: custom182.565ms, generic
  hits SQLSTATE57014 at2.012s with JIT off. OWNER custom149.433/generic153.273ms
  both hash joins; this does not establish generic plans as every tail's cause.
  No driver mode or timeout change. The cloud parent cgroup supplies4 CPUs total
  shared by Go/API and Docker,32GiB RAM; PostgreSQL itself remains capped4CPU/8GiB.

Earlier runs use128MiB shared buffers; ADR048 runs explicitly use2GiB shared,
6GiB effective cache estimate, unchanged4MiB work_mem/random_page_cost4 and the
same4CPU/8GiB/no-swap ceiling. Buffer misses are not proof of physical disk I/O.
Ordinary `make infra-up` retains lightweight defaults; `make reference-up`
selects the declared performance profile and preserves the named data volume.
Every reference test verifies actual connected settings/image/container limits.
Four-slot admission and a CacheDescribe driver experiment were explicit
test compositions; default application-mode capacity is NOT certified by them.

Logs are ignored `.local/reports/r3d-*.log`; generated artifacts/secrets do not
belong in Git. Independent executable review, final root/build/browser and all
retained capacity gates are NOT RUN. SQL_V1 is not declared accepted or production
capable. Raw unindexed100k sums remain outside a passing capacity envelope.

## Pending verification and scope

Resolve the measured count plan before claiming aggregate capacity. Measure the
real generated SQL, including validity checks, under full count/sum, selective
filters and ownership/sharing cases. Preserve canonical-generation ACL/tombstone
authority and immutable legacy readers in any optimization. Use stable commits
and an exclusive independent review/test window.

General GROUP BY, arbitrary expressions, distinct/average, join expansion,
report/UI builders and arbitrary-workload capacity are outside SQL_V1. R5
deployment/UI, R6 release authority and R7 operations remain pending. Recovery
disables opt-in use and forward-fixes; no destructive data change is involved.

## Custom-plan checkpoint — 19 October 2026 (Asia/Kolkata)

ADR053 captures/restores prior JIT and plan-cache mode, with LOCAL off/custom
restricted to certified SQL_V1. Focused four groups PASS4.324s, covering six
prior-mode combinations and actual aggregate/restoration cancellation. The
unchanged unmapped indexed100k/ten-client reference workload now PASS119.819s:
count SQL/HTTP p95 55.528/177.899ms; integer245.575/573.965; decimal261.648/635.354;
money245.128/702.126; filtered290.095/731.666; OWNER208.152/492.827;
SHARE228.720/589.006. Zero request errors. This uses explicit four-slot admission,
not the still-ten-slot application default, and does not certify mapped/canonical
capacity. The deliberate diagnostic generic filtered-plan timeout is recorded
separately from successful API samples. All earlier failures remain above.

Independent read-only review found restoration SQL could run during driver panic
unwinding. The defer now re-panics immediately and leaves rollback to WithRuntime;
the independent executable panic probe is still pending. Full/root/browser,
retained capacity gates, canonical capacity and independent executable review
remain pending. R3d IN PROGRESS; overall M9R PARTIAL; no M10 or main merge.

## Application configuration and mapped verification checkpoint

ADR054 precedes application wiring in96da469: API_DB_CONCURRENCY integer1..10,
default4, existing FIFO128, unchanged pool10 and worker behavior. Configuration
unit tests pass; actual focused aggregate/queued-live-revocation selection
passes28.382s. That selection did not supply the optional legacy artifact; final
verification must supply it and report zero skips explicitly. Runtime launcher
whitelist initially omitted the new setting; a0ede6f forwards it to API only,
with8/8 Node environment-isolation tests passing.

The mapped workload now uses actual100k adoption and verified shadow evolution,
then current/retained bindings, governed owner/share/deletion changes and a
current-only balance101 versus historical1 oracle. The aggregate target now runs
both raw and mapped fixtures. Its12m total test timeout accommodates the added
real migration/index builds; no per-query/lock/transaction/queue or latency limit
changes. This expanded variant has not completed execution yet.

## Canonical failure, independent findings and streaming remediation

The first real100k adoption/evolution/default4 mapped run FAIL720.050s: current
integer SQL/HTTP p95 353.140/887.348ms, decimal371.763/1134.864,
money553.856/1267.286, filtered698.319/1653.025, OWNER539.429/1277.209,
SHARE620.084/1799.529. Retained integer413.520/1133.581,
decimal421.240/1208.863 and money493.508/1295.064 also fail HTTP. Completed
cases return correct results with zero errors, but the total test times out
during retained filtered warmup; its later cases are NOT RUN. Actual migration
reached ACTIVATED with100k copied/verified and six READY numeric indexes.

ADR055 precedes actual default3 scheduling (strict overrides1..10 unchanged).
ADR056 precedes the exact canonical identity namespace boundary and streamed
single-use aggregate CTE. Normalized projection and inner cap remain; no scope,
RLS, view, data or arithmetic weakening. Individual deadlines remain unchanged.
The two-fixture aggregate target now has20m total suite time to cover real100k
adoption/evolution and six index builds; this does not extend any query budget.

Independent review9ecb3b8 passes22 ordinary groups88.328s. Source d7b64dd
reverification reports those same22 passing, with a combined command FAIL87.352s
from a new reviewer EXPLAIN-text assertion. The corrected structural probe then
PASS11.981s: old/current317-row generated plans each have one normalized child
projection, one identity scan/loop and no CTE Scan. EXPLAIN CASE deparsing did not
prove duplicate execution. Exact differentiated sums, legacy NULL raw fallback,
current ownership/sharing/tombstones, snapshots, settings/timeout/fault assertions
and retained executable compatibility all retain executed evidence.

An injected pgx driver-tracer panic DOES hang database/sql rollback. The explicit
`integration,driverpanic` supervised diagnostic FAIL8.576s then FAIL7.272s remains
IR-R3d-01/R7, documented in the [independent report](../verification/M9R_R3D_INDEPENDENT_REVIEW.md).
Default postgres.Open installs no custom tracer; ordinary callback panic rollback
and same-backend reuse pass. There is no arbitrary driver-panic recovery claim.
No assertion is removed: the separate explicit diagnostic still fails. A bounded
rollback goroutine would not repair the leaked library lock and is not introduced.

Final full/build/browser and all eight capacity gates remain pending. Functional
review does not accept mapped capacity or declare overall M9R complete.
