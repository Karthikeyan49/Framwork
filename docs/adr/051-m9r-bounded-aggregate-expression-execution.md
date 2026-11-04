# ADR051 — Avoid per-request JIT compilation for bounded aggregates

18 October2026 (Asia/Kolkata). Status: accepted for implementation after
independent conditional design review and executed JIT measurement, before code.
R3d only; all latency, authorization, snapshot and query budgets remain unchanged.

The actual generated100k money aggregate was measured with JIT enabled/disabled
against the same owned fixture/parameters. PostgreSQL reports394.627ms compilation
with JIT on and0ms off; the non-JIT analyzed statement takes181.034ms including
ordinary plan/expression execution. Diagnostic command passes44.485s. This is
measured compilation cost, not an inference from cumulative node timings. The
preceding ten-client header-index run fails94.218s with integer SQLSTATE57014;
retain it. Cached-plan/admission experiments did not satisfy the whole gate and
are rejected; default driver and ten-slot API admission are retained.

Only the existing certified interactive SQL_V1 kernel may temporarily disable
JIT through transaction-local `set_config`. Capture the prior effective setting
from the database. If it was on, set LOCAL off before aggregate execution and
restore on at every normal/non-SQL-aborted return. An already-off setting remains
off. Prior setting is trusted database state, not a metadata/request option.
No persistent/global setting, scan-method forcing, role/RLS/privilege change,
compiler IR upgrade, timeout increase, float arithmetic or expression weakening.

On SQL-aborted transactions, cancellation or failed restoration, WithRuntime
rolls back LOCAL settings and resets/discards its leased connection normally.
If restoration fails after apparently successful aggregation, suppress the page
and fail the operation. Callback composition must propagate the error. Do not
leave changed state available to a later query in a successful transaction.

Tests must prove previous on/off restoration within one certified transaction,
early error/invalid/budget paths, actual cancellation/failure and pooled reuse.
Measure full HTTP latency including reading/changing/restoring settings and all
ordinary policy/queue/commit work. EXPLAIN capacity diagnostics must use the same
local expression mode as execution; explicit ON/OFF diagnostics remain separate
and cannot replace the unchanged100k/ten-client workload. Rerun all read/write/
uniqueness gates and independent executable review before accepting capacity.

Recovery disables opt-in SQL_V1 while forward-fixing its kernel; data, metadata,
indexes, worker behavior and default application driver/connection limits remain.
