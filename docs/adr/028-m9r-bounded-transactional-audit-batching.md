# ADR028: Bounded transactional batching of authorization evidence

Status: implemented with root correctness verification; independent review pending.
Batching alone did not pass the capacity target. FIFO admission is recorded in ADR029.

## Evidence and decision

The R4 diagnostic workload at the reviewed-fix checkpoint ran 32 clients for
60 seconds with 6,471 successful writes and no errors. It preserved record-version,
receipt, business-audit and outbox counts, but p95 was 701.506 ms against the
unchanged 500 ms target. The ten-connection pool waited 6,461 times for a cumulative
1,316 seconds. Earlier R3 measurements passed SQL gates while failing HTTP latency.
These observations establish significant queueing; they do not identify the
earlier single 503's cause or prove which SQL statements dominate service time.
The batching-only run failed at p95 788.839 ms with zero errors and correct fact
counts. It provides no demonstrated standalone latency improvement. Transaction
phase measurements and the FIFO correction followed; do not attribute its passing
result solely to batching.

Preview authorization currently appends individual entity, row, field and scope
audit entries, with a SQL round trip for each. Test a bounded batch INSERT as a
controlled optimization. Preserve every evidence entry and its identifying
fields, not just an aggregate count. Keep the existing pool size and acceptance
targets so improvement cannot be attributed to weakening the gate.

Action and DataScope decisions still evaluate before their governed operations.
Only their persistence is collected within the same local database transaction.
Before the transaction may commit or return a protected response, persist all
collected evidence. Batch failure rolls back mutation, receipt, business audit,
workflow triggers and outbox together. This is suitable for local transaction
operations; it must never surround an external action that could escape rollback.
Business/event audit remains coordinated through its existing public contracts.

## Bounds and compatibility

Use parameterized SQL only. Limit collected entries per request and split INSERTs
into bounded parameter batches, retaining forced RLS and the same insert-only role
grants. Validate typed evidence before writing; never include tokens, SQL, secrets
or customer-value payloads. A writer without the optional batch port receives its
entries through the existing Append interface and must propagate every failure.

SQL-aborted transactions must not attempt a deferred batch that masks the original
serialization/deadlock error. Typed command rejection still forces rollback and
fresh denial evidence under live authentication. Query errors retain their existing
non-business audit behavior. Worker capability predicates and grant checks remain
unchanged; no additional database role or table privileges are introduced.

## Verification and limitations

Verify exact entry identities/counts and tenant/actor provenance, populated RLS
negatives, denial evidence, chunk-boundary rollback, poisoned/oversized evidence,
and failure after a business mutation but before authorization batch persistence.
Run prior command/dataset/worker/query regressions, browser recovery checks and
independent fault probes on a stable commit. Execute the unchanged 60-second write
gate and R3 HTTP workload on the reported reference tier.

This does not implement retention, partitioning, sampling, product policy, rate
limiting, production activation or schema evolution. An audit batch preserves
evidence volume; it does not solve its storage lifecycle. If measured latency
still fails, preserve the failure and profile the remaining bottleneck.
