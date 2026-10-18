# R3d — Bounded SQL aggregates and query planning

18 October 2026 (Asia/Kolkata). **STATUS: IN PROGRESS; not accepted complete.**
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

Earlier runs use128MiB shared buffers; ADR048 runs explicitly use2GiB shared,
6GiB effective cache estimate, unchanged4MiB work_mem/random_page_cost4 and the
same4CPU/8GiB/no-swap ceiling. Buffer misses are not proof of physical disk I/O.
Ordinary `make infra-up` retains lightweight defaults; `make reference-up`
selects the declared performance profile and preserves the named data volume.
Every reference test verifies actual connected settings/image/container limits.
Four-slot admission and a CacheDescribe driver experiment are currently explicit
test compositions; default application-mode capacity is NOT certified by them.

Logs are ignored `.local/reports/r3d-*.log`; generated artifacts/secrets do not
belong in Git. Independent executable review, final root/build/browser and all
retained capacity gates are NOT RUN. SQL_V1 is not declared accepted or production
capable. Raw100k sums remain outside a passing capacity envelope.

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
