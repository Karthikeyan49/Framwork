# ADR059 — Bound physical aggregate input to its trusted namespace

19 October 2026 (Asia/Kolkata). Accepted after independent review and executed
capacity/correctness verification. Proposed before code at `8f73d47`;
implementation `3a9d23e` preserves the namespace, snapshot, scope and cap contracts.

ADR058's provisional cost1.1/default4 run fails raw capacity133.412s. No observed
EXPLAIN node spills temporary blocks. SHARE count performs20000 records_pkey
lookups and80010 shared hits; filtered money scans190000 records/38008 blocks
before retaining14285. These measured costs do not explain every latency tail.

Expose physical runtime.records through a derived relation with the exact trusted
tenant, application, physical revision/hash and entity predicates and OFFSET0.
Select the existing typed namespace/identity/version/owner headers and custom_data;
PostgreSQL may prune unused JSON on certified sums/counts. Use the same relation
for unmapped and mapped physical generations. Keep the canonical identity boundary
from ADR056 and its live ownership/tombstone semantics. This prevents outer share
joins from turning physical access into one lookup per shared record; it does not
force a scan method, physical hash join or accepted latency result.

FORCE RLS remains on each physical relation. Trusted namespace predicates are
not authorization; DataScope, field coverage and filters remain outside this
boundary and before LIMIT100001 and aggregation. Never truncate the namespace
inside the boundary. Preserve missing/stale projection denial, legacy NULL
exact-generation fallback, certification semantics and repeatable-read snapshot.
No shared DDL, migration, settings, data, roles, timeouts or admission change.

Add passive per-case Go process GC/CPU and cgroup throttling observations where
available, with explicit unavailable status, and backend-type/wait counts only.
Do not print SQL, parameters, database identifiers or business values. No forced
GC, vacuum, pauses or quiet-period waits to manufacture a pass. Diagnostics do
not replace all seven100k-row/ten-client cases in each physical/canonical branch,
unchanged SQL500ms/HTTP750ms gates or all earlier regression/capacity gates.

Reverify namespace isolation, projected/filtered field authorization, cap and
invalid-value priority, canonical/retained ACLs, snapshots, real SQL cancellation
and settings cleanup. Preserve every failed workload. If capacity still fails,
R3d remains incomplete and this optimization is not a production-readiness claim.

## Executed outcome

Independent review at `1f73033` passes all 23 ordinary race groups and separately
strengthened nested OWNER+CONDITION/rare core-ID filters over 100,001 physical
rows. The telemetry-scope LOW finding is fixed in `f9cb4fa`. Root aggregate
capacity passes all 24 unmapped/current/retained cases in 619.747 seconds; full
checks/build, both browser suites and seven earlier capacity targets pass too.
The filtered source still scans 38,008 record blocks in the observed raw plan;
the fence is not claimed to eliminate that scan. No source, permission or budget
weakening was required. Exact timings, previous failures and recovery are in the
[report](../migration/M9R_R3D_IMPLEMENTATION_REPORT.md).
