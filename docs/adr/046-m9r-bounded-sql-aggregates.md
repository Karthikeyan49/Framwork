# ADR046 — Explicit bounded SQL aggregate execution

18 October 2026 (Asia/Kolkata). Status: accepted for implementation after
independent read-only design review and the per-input validity correction below.
Executable review and final gates remain required.
Scope: R3d preview data foundation. No M10, production activation or main merge.

## Compatibility and request contract

Existing count/sum aggregation fetches at most the requested page and fails its
budget if more rows exist. Preserve that behavior, existing cursor fingerprints,
immutable source/IR bytes, readers and pinned workflow execution. An absent or
empty aggregate execution mode continues the existing implementation.

Add an explicit query-only `aggregate.execution: "SQL_V1"`. Unknown nonempty
modes fail closed. This does not change metadata schemas or require migrations.
The supported operations are count and exact sum over integer, decimal or
fixed-currency money. Count means all matching entity-visible records, including
records whose optional sum value is absent/null; sum ignores absent/null values
and returns canonical zero for an empty/all-null set. Results contain no records
or cursor. Money uses the field's immutable currency contract, without FX.

SQL_V1 requires the normal query envelope with valid unique projections and
`limit: 100`; it is an explicit full-set aggregate, not a page-sized result.
Require a projected sum field as before. Projection visibility does not narrow
count, and unrelated masked projections do not affect the result. Reject exact
record selection, cursor, sort, join, search and unsupported operations/types.
GROUP BY, arbitrary expressions, distinct, averages, report/UI builders and
general join/optimizer expansion remain separate work.

## Authority and execution

Admit this path only through the interactive visible-query port with an exact
closed Native/Defined action kernel and the certified transaction executor from
WithRuntime. The API must supply the same live engine used by its guards and
scope resolver. Unknown engines/wrappers, raw/autocommit executors, legacy storage
adapters and worker source adapters cannot opt into expanded aggregation. Their
existing bounded semantics remain available when the new mode is absent.

Check entity, row and aggregate actions. Resolve live entity DataScope and compile
its predicates before caller filters or row limits. Every filter field and the
sum field must pass its normal field-read action and cover the entire entity
scope (ALL or the same normalized scope). Do this before validating field values
or reading data. Denied/unknown policy never becomes a smaller count or a masked
zero. Preserve current repeatable-read revocation semantics and tenant RLS.

Use the existing typed filter AST, physical field resolution, bound parameters
and runtime.read_records live-generation/tombstone view. No metadata SQL,
tenant-triggered DDL, SECURITY DEFINER bypass or new worker grant is introduced.
One MATERIALIZED CTE selects only the required scalar after tenant/namespace,
entity, scope and validated filter predicates, capped at100001 input rows. One
SQL statement computes count and optional PostgreSQL numeric SUM. If count exceeds
100000 return the existing explicit budget error without a partial result.
The cap counts authorized matching inputs, not unobservable physical scan work;
the unchanged2s statement/500ms lock/3s transaction deadlines bound wider scans.

Validate every included sum input inside SQL before casting: required/null and
native JSON type, integer lexical/integer/JavaScript-safe bounds, decimal string
length/lexical/18-place/absolute1e18 bounds, and the exact two-key money object,
currency and decimal amount contract. Current JSONB storage alone does not prove
these invariants. Guard casts with CASE and return an invalid-input count together
with count/sum; any invalid input rejects the whole result, even if its value
would cancel another invalid value. Count ignores unrelated projected values.

Keep exact fixed-point summation in PostgreSQL numeric, then validate/canonicalize
the single returned scalar using the existing rational/18-place formatting
semantics. Do not use floating point or the per-field maximum as a sum maximum.
Bound scalar decoding and fail closed on malformed/nonfinite/corrupt values.
No record values leave SQL for application-side aggregation. No result, policy,
membership or source cache is added. Emit a static aggregate-execution trace
attribute without customer query literals, values or raw driver errors.

## Acceptance and recovery

Use populated 100+ row fixtures and independent exact integer/decimal/money
oracles; test empty/null/negative/max-value/18-place cancellation. Preserve legacy
100-row errors and canonical fingerprints. Verify OWNER/TEAM/SHARE/CONDITION and
combinations, field denial/inference, live revocation and tenant isolation.
Exercise current/retained dataset bindings and tombstones, raw/worker/wrapper
denial, unsupported request shapes, parameterized literals and transaction fault
rollback. Test100000/100001 boundaries and no partial output. Old binaries must
still execute old aggregate requests and reject the unknown execution property.

Reference capacity uses the existing pinned PG18.6 four CPUs/8GiB/disabled swap,
ten populated tenants (hot100k, other10k each), representative900+ byte records
with mixed ownership and shares. Warm3 times, measure at least100 requests per
case at10 clients, report p50/p95/p99/throughput/errors and independent results.
Supported full count, exact sum and selective filtered aggregates require SQL
p95<=500ms and HTTP p95<=750ms with zero unexpected errors. Inspect EXPLAIN/
BUFFERS and membership InitPlan loops. Retain all existing query, uniqueness and
write gates; do not weaken RLS, raise timeouts or edit budgets after a miss.

Write the independent adversarial report on stable source, fix findings, and run
full check/test/build, both browser suites and reference gates before PASS. Record
failed and NOT RUN cases. SQL_V1 is a bounded aggregate expansion, not evidence
for arbitrary query plans or complete reporting. On failure stop opting into the
new mode and forward-fix; legacy requests, data, releases and receipts remain
unchanged. No destructive rollback or data migration is required.
