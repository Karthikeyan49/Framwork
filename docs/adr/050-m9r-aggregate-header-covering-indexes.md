# ADR050 — Fixed platform covering indexes for aggregate headers

18 October2026 (Asia/Kolkata). Status: accepted for implementation after
independent read-only design review, before migration/planner code. R3d only;
executable review and all gates remain required. No M10 or production deployment.

## Problem and decision

The versioned numeric witness removes repeated raw validation, but the aggregate
still scans large record heaps to read row_version/owner/team and numeric heap
pages for projected value/version/validity. Ten-client HTTP targets continue to
miss despite correct values and zero errors. Preserve those failed measurements.

Add two fixed platform indexes in forward-only migration54:

- runtime.records namespace/entity/ID keys, INCLUDE(row_version,owner_user_id,
  owner_team_id).
- runtime.numeric_projections namespace/entity/field/ID keys,
  INCLUDE(value_number,source_version,aggregate_valid_v1).

These contain only existing platform fields. No customer-selected DDL, promoted
custom columns, new data table, approximate counter, policy cache, RLS/grant change
or data rewrite is introduced. Planner choice remains PostgreSQL's; never force
scan methods or edit latency/statement/lock thresholds. They add storage and write
amplification, which must pass all existing32-client write/uniqueness gates.
Including row_version/source_version changes index entries on each mutation and
can prevent HOT updates. Measure that cost; do not treat unchanged existing
column values as evidence that this index is free to maintain.

For an exact READY numeric witness, extract raw legacy JSON only through a
correlated point subquery when the validity flag is NULL. The lookup includes
trusted tenant/app/physical revision/hash/entity/ID and the selected row_version
inside the same certified snapshot. Main source headers still enforce exact
physical identity/current canonical owner/team/tombstones; never replace them
with stale indexed owner facts. Missing/stale/false projection inputs remain
invalid sentinels and are counted, not filtered out. ALL-certified input avoids
requesting raw JSON from the main source, allowing covering scans. Filters or
conditions that need raw fields still read them. Legacy NULL flags still validate
the original physical value. Count remains independent of numeric witnesses.

## Verification and recovery

Measure actual generated plans, heap fetches/buffers/temp work, whole HTTP queue
time and strict independent results. Test mixed true/NULL/false/missing/stale
witnesses, canonical retained reads and ownership/share/tombstone changes. Verify
late lookup exactness/row-version matching, concurrent writes/cutover and indexed
build/write lock order. Do not assume visibility-map bits are set after fresh
writes or claim cold-cache/general capacity from warm measurements. Report any
natural vacuum during fixture construction rather than silently adding VACUUM.

Migration index construction takes a platform maintenance lock and extra disk
space. Local owned/disposable migrations are authorized here; production online
index scheduling/lock/disk assessment remains an operations/deployment task.
Failed transactional construction rolls back. Retain data and use forward recovery;
turn off opt-in aggregates while fixing the plan if needed. Rerun all old gates
and independent adversarial review before accepting the indexed envelope.
