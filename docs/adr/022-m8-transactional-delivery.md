# ADR022: PostgreSQL outbox/inbox with Redis routing transport

Status: Accepted for M8 implementation. Date: 2026-10-12.

PostgreSQL owns immutable event envelopes, aggregate sequences, fenced delivery
leases, attempts, inbox receipts, and replay evidence. Business writes, audit and
new event snapshots commit together. A trigger freezes identifiers-only payloads;
delivery never reads mutable records to reconstruct an event. Record versions are
not event sequences: update and delete may use the same record version.

Redis Streams carries routing references only through a replaceable adapter.
The dispatcher has routing privileges, never business payload privileges. A worker
resolves a reference against the database and executes one live delegated job with
RLS and policy. Consumer-local effects, audit and inbox completion share one
transaction; Redis acknowledgement follows confirmed commit. Unknown commit or
publish outcomes are retried with the same event identity. Delivery is at least
once; this does not promise exactly-once external actions.

Delivery uses PostgreSQL time, bounded exponential backoff, expiring leases and
generation/token fencing. Uncompleted events are republished after a receipt
deadline, including after Redis loss. Ordered delivery applies within one
tenant/app/source revision/IR/entity/record epoch, with failed predecessors
blocking successors. Manual replay preserves event bytes and inbox receipts and
requires a controlled operator, expected generation and recorded reason.

Historical M6R intents lack this envelope contract and remain LEGACY_PAUSED. The
additive migration preserves them without reconstructing historical payloads.
M8 runs a preview receipt consumer only in development/test. Workflows, governed
tools and production release consumers remain M9/M10/M13. No public replay route
or customer-defined SQL callback is introduced.
