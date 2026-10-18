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
uses the current live-generation view, with no new migration or dependency.

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

Logs are ignored `.local/reports/r3d-*.log`; no generated artifacts or secrets
belong in Git. Late-audit and retained-binary tests are added but NOT RUN yet.
Independent executable review, final root/build/browser and retained capacity
gates are NOT RUN. The new mode is not declared accepted or production capable.

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
