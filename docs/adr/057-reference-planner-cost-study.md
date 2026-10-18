# ADR057 — Calibrate planner costs with controlled app-role measurements

19 October 2026 (Asia/Kolkata). Accepted for diagnostic implementation after
independent read-only review, before code. A new cost profile is NOT accepted yet.

The default3/streamed run fails its unmapped floor134.75s. All SQL p95 values
remain below500ms but HTTP integer1130.093, decimal1482.324, money784.300,
filtered1013.037 and SHARE1000.764 exceed750ms; count and OWNER pass. Zero errors
in completed cases. The implementer interrupts the later mapped fixture after
the proven failure to investigate rather than spending another full rehearsal.
The whole command FAIL488.664s; mapped capacity is INCOMPLETE, not a pass.

Plans choose a sequential scan across300k numeric projections to select100k,
and a sequential scan over190k records for the selective filter, despite the
fixed covering indexes. Current random_page_cost4 plus incompletely visible fresh
heaps may explain those choices. It is a hypothesis, not an SSD/device claim or
guaranteed fix; random index heap probes can legitimately cost more than a scan.

Implement an explicit diagnostic that replays the exact generated statement and
bindings with actual app role/identity, JIT off/custom planning, under LOCAL
random_page_cost4 and1.1 in alternating order. Use ANALYZE/BUFFERS, report actual
heap fetches, loops, timing and relevant relpages/relallvisible/statistics and
correlation. Preserve all limits, default3 admission, ten populated tenants,
100k hot rows, three built indexes and identical data. Do not vacuum, change
statistics targets or simultaneously alter visibility/resource/driver settings.
Do not print SQL, parameters or business values. Buffer hits/misses do not prove
physical device I/O. A diagnostic PASS is execution evidence, not capacity PASS.

Only if measurements support it may an explicit versioned reference profile
adopt1.1. Such a revision must be documented before its configuration change,
verify the actual connected cost, retain all4-cost failures and rerun every
earlier read/write/unique/aggregate gate and browsers. No timeout/latency/client/
row-cap relaxation, scan-method forcing or metadata-controlled SQL hint. Actual
ten-client HTTP including FIFO wait determines acceptance. Production operators
must calibrate their deployment; this is not a production/default hardware claim.
