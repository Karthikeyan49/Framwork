# ADR056 — Bound canonical identity planning and stream aggregate inputs

19 October 2026 (Asia/Kolkata). Accepted after independent read-only design
review, before code. R3d capacity and final regressions remain incomplete.

The failed default4 canonical workload exposes14,285 identity point lookups in
the filtered sum (57,148 shared hits) and19,999 identity point lookups in SHARE
count (80,004 hits,169,712 total). Full decimal sums also write273 temporary
blocks in the materialized input CTE. These are measured plan costs, not proof
that they explain every tail. Preserve the12m suite timeout and latency failures.

For canonical sources only, expose record_identity as a typed derived relation
under exact trusted tenant/app/dataset/entity and NOT tombstone predicates,
selecting only ID and owner/team headers, with OFFSET0. Join source records on
ID. The unique key still guarantees at most one identity per source record.
Current canonical ownership and tombstones remain authoritative for retained
generations; missing identity excludes the record. FORCE RLS and all existing
scope/field/filter checks remain. No read_shares or general policy change.
The boundary prevents correlated identity point probes through that relation;
it does not force a physical hash plan or establish arbitrary-workload capacity.

Make aggregate_input a default single-use CTE rather than MATERIALIZED. Preserve
the raw-selection LIMIT100001 and normalized SELECT OFFSET0 projection boundary.
One statement/certified RR snapshot, pure guarded casts, per-input invalid
sentinels, cap priority and exact count/sum remain. No volatile expressions,
caller SQL or side effects exist in this kernel. Inspect the resulting plan to
verify normalization is not duplicated and temp work is avoided; do not infer
physical behavior solely from syntax. Update tracer prefixes consistently and
retain assertions proving cancellation/restoration faults actually fired.

Reverify exact/corrupt/cancelling/NULL inputs, both cap boundaries, field and
record scopes, canonical/retained/snapshot behavior, real SQL timeout, callback
panic and settings cleanup. Run ten-client capacity on all branches and all
earlier read/write/unique/browser gates under default3. No data, DDL, privilege,
driver, individual timeout, input limit or latency assertion changes. The
injected-driver-panic finding IR-R3d-01 remains an explicitly failing R7 diagnostic;
this design does not promise database/sql can recover arbitrary driver panics.

Recovery disables opt-in SQL_V1 pending a forward fix; retain existing data,
immutable pins and all authorization/effect evidence.
