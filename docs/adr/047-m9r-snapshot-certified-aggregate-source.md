# ADR047 — Snapshot-certified aggregate source planning

18 October 2026 (Asia/Kolkata). Status: accepted for implementation after
independent read-only design review, before planner code. Supplements ADR046;
executable review/final gates remain required and all unchanged budgets apply.

## Measured problem

The initial SQL_V1 full count is correct but fails the reference workload: SQL
p95 1476.616ms and HTTP p95 1527.163ms at10 clients, zero errors. A diagnostic
repeat also fails. The actual generated plan scans the190k-row shared records
relation (38,006 heap blocks), loops an empty materialized generation join100000
times, and hashes the empty identity relation. A single observed plan takes
179.329ms; membership itself remains an InitPlan executed once. The global live
view's CASE/outer joins prevent a narrow namespace aggregate plan.

## Decision

Only SQL_V1's closed interactive kernel and certified WithRuntime transaction may
use this planner. Before constructing its one bounded aggregate statement, query
the immutable dataset-generations mapping and dataset canonical-access state by
trusted tenant/app/physical DataRevision/DataHash. Read that witness and records
under the SAME repeatable-read snapshot; never cache it or infer it from the URI,
caller flags, metadata revision or compiler hash. Scratch URIs may already map
to adopted/evolved namespaces. Table RLS and live membership remain enabled.

- No mapped namespace, or mapped canonical_access=false: use runtime.records.
  This is exactly the view's source-value/physical-owner branch in that snapshot.
- canonical_access=true: construct a typed derived relation joining the exact
  physical namespace to record_identity for the witnessed dataset/entity/ID and
  excluding tombstones. Select physical stored values and canonical current
  owner/team headers. Missing canonical identity gives no row, never a fallback.
- For an explicitly bound Scope, require that its dataset/generation matches the
  database witness. Missing/mismatched witnesses fail closed. A normal scratch
  scope still resolves any actual database mapping before deciding its source.
  A retained pin need not equal the latest generation/binding, and FROZEN reads
  remain valid; do not turn a read witness into an active-write admission check.

The relation exposes only columns needed by typed aggregate filters/scopes. Keep
all tenant/app/physical revision/hash/entity predicates, action/DataScope/field
authority and caller filters before the100001 limit. Shared-record authorization
continues through runtime.read_shares, preserving canonical current shares across
retained generations. No private SQL function, new grant, policy weakening,
tenant DDL, metadata rewrite, counter or approximate count is introduced.

Concurrent adoption/cutover after the witness query cannot change the certified
snapshot; it therefore cannot make the raw branch observe different authority
than the original live view in that transaction. Normal next-transaction policy/
membership revocation semantics are unchanged. Generic/autocommit/wrapped
executors cannot claim this proof. This is a local query-plan decision, not a
global bypass or mutable dataset cache.

An explicit primary-key ordering experiment was rejected: it added sorting and
temporary spill, worsening SQL p95 from612.464ms to1017.584ms. The capped input
therefore remains unordered; accepted count/sum results are order independent
and over-cap results are rejected. Never move scope/filter after the cap or
disable planner methods/RLS to force a pass. Measure the actual generated plan,
including its fresh-row heap work. ADR048 declares the reference memory profile.

## Required verification

Independently test scratch namespaces before and after adoption/canonical
cutover, bound/retained namespaces, missing identity, current owner/team/share
changes and tombstones. Compare results with the existing view under matching
snapshots. Exercise an actual concurrent mapping/cutover while the witness
transaction remains open. Bound witness mismatch must deny. Worker/raw/opaque
proof negatives stay required. Capture plans and rerun the unchanged reference
aggregate workload including per-input validity, ownership/sharing and all older
query/write/uniqueness gates. Preserve failed measurements. On failure stop
opting into the expanded mode and forward-fix; no data migration is required.
