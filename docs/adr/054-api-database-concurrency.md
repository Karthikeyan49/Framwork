# ADR054 — Configure bounded API database concurrency

19 October 2026 (Asia/Kolkata). Accepted after independent read-only review,
before code. R3d capacity acceptance remains conditional on final gates.

On the declared reference machine the API and PostgreSQL share four parent CPUs.
Ten simultaneous aggregate transactions exceed the unchanged latency gates;
four admitted transactions with ten offered HTTP clients pass all seven unmapped
indexed aggregate cases, including all queue time. This is evidence for bounded
CPU scheduling, not more database authority or a general production guarantee.

Add trusted process setting `API_DB_CONCURRENCY`: integer 1..10, default4.
Reject explicit environment zero, negative, malformed or out-of-range values.
A zero-valued Go Config retains default4 for existing programmatic callers.
The application composition root constructs the existing FIFO Admission(limit,128)
and injects it into access.API. Register preserves explicitly injected admission;
its fallback is min(positive pool maximum,4), or4 for unlimited/unknown pools.
Register does not read environment variables. Keep database pool maximum10,
driver mode, worker/migrator behavior, query/lock/transaction/queue deadlines,
queue bound128, client workload and all latency assertions unchanged.

This governs access.API database work only; health/readiness and Redis sign-in
do not use this gate. It is not rate limiting, a distributed limiter or R7 closure.
Operators may tune within1..10 only with their own workload/resource evidence.
Test configuration rejection, fallback and explicit-gate preservation, queued
membership revocation, all eight reference read/write/unique/aggregate gates and
application/sign-in browsers. Measure mapped and retained aggregates separately.

No schema, data, secrets or dependencies change. Recovery can set an explicitly
measured prior concurrency value, or disable opt-in SQL_V1 while forward-fixing;
do not weaken authorization, reset receipts or increase timeouts to pass.
