# ADR029: Bounded FIFO admission for API database transactions

Status: implemented and independently verified at `94932e3`. Scope: M9R runtime capacity.

## Measured problem and decision

The diagnostic 32-client write workload still failed after audit batching:
p95 788.839 ms, no errors. Transaction phase tracing then measured p95 connection
acquisition of 717.820 ms, role admission 20.666 ms, transaction body 184.554 ms
and COMMIT 21.535 ms. Those percentiles describe separate sample distributions;
they cannot be added to reconstruct one request. The overall p95 was 839.408 ms.

The ten-connection database pool was saturated. Go's database/sql selects pooled
waiters randomly, so later requests can repeatedly bypass older ones. Add a
bounded FIFO gate owned by the registered API and sized to its existing pool.
Do not increase connections, CPU/memory allocation, SQL timeouts or the acceptance
target. Release a permit only after the owned transaction and connection cleanup
finish, including rollback, cancellation and panic. Retries rejoin the queue.

Admission has at most 128 queued requests and honors request cancellation. A
cancelled waiter is removed; a simultaneous grant/cancellation releases exactly
one permit. Releases are idempotent. Authentication is verified after admission,
then live identity/membership/policy resolves within each attempt's transaction.
Fresh denial transactions use the same gate. No global mutable registry or tenant
identity cache is introduced. Worker pools and lease authority remain separate.

## Observed verification and boundaries

The first root workload with FIFO completed 8,500 writes in 60.196 seconds,
zero errors, p95 297.404 / p99 337.961 ms and 141.20 writes/s. Database pool waiting
was zero; request admission p95 was 205.799 ms. Exact row versions, receipts,
business audit and outbox counts passed. This is a root observation on the
reported 4-CPU/8-GiB reference PostgreSQL container, not independent acceptance
or a universal production capacity promise. Retain all prior failures.

Execute FIFO order, queue overflow, cancellation/grant races, double release,
panic and resource recovery tests. Re-run command/dataset/worker/authentication
regressions, browser reconciliation and both performance gates on a stable commit
with independent review before declaring capacity accepted.

This bulkhead is not a per-user rate limiter. Invalid authentication still reaches
the existing audit transaction; R7 admission/abuse protection remains open.
Readiness probes sharing the pool can still use a connection outside the API
gate. External services or multiple registered APIs must budget shared pools
explicitly; this does not coordinate distributed connection capacity.
