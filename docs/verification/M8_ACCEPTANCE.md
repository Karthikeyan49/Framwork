# M8 Transactional Outbox + Inbox acceptance

V3 milestone M8 only. Evidence is in the implementation and independent-review
reports. These checks certify the development/test delivery foundation; production
activation, workflow/tool consumers and human deployment approval remain later work.

- [x] Preserve M0–M7/M6R, old migrations and customer data; additive migrations18/19.
- [x] Business mutation, audit, immutable envelope and dispatch intent commit together.
- [x] Closed versioned envelopes bind event/tenant/actor/source/IR, UTC time and correlation.
- [x] Stable stored-byte SHA256; no mutable-record hydration during replay.
- [x] Event sequence distinct from record version; ordered aggregate epochs.
- [x] PostgreSQL authoritative delivery leases, attempts, completion and replay evidence.
- [x] PostgreSQL-time expiry, generation/token fencing and concurrent SKIP LOCKED claims.
- [x] Bounded retries, backoff, receipt deadlines and durable dead-letter handling.
- [x] Redis Streams adapter transports references only, with bounded retention.
- [x] Broker loss/restart recovers pending intent; lost publication permits duplicates.
- [x] Fair fresh/pending reads and reclaim cursor; dead work cannot starve another tenant.
- [x] Exact routing lookup before worker context; original live membership/policy required.
- [x] Forced RLS and separate least-privilege DB/Redis roles; no dispatcher payload reads.
- [x] Inbox key binds tenant/consumer/event/hash; replay never clears deduplication history.
- [x] Inbox, local effects, business audit, derived outbox and completion share one transaction.
- [x] ACK after confirmed commit only; actual lost-COMMIT-response recovery tested.
- [x] Malformed/unroutable references receive durable hash-only quarantine before ACK.
- [x] Operator-only reviewed replay with expected generation and immutable replay evidence.
- [x] Historical intents remain LEGACY_PAUSED; no invented original snapshots.
- [x] Production preview delivery refused; only preview receipt consumer composed.
- [x] Per-process environment curation and distinct cache/sender/receiver/operator credentials.
- [x] Role-aware readiness detects missing grants, functions and disabled emission triggers.
- [x] OTel trace/request correlation and event/consumer/source audit evidence.
- [x] Four actual process-kill checkpoints, real Redis restart and unknown-commit fault injection.
- [x] Existing backend/frontend/browser regressions, builds, checks and bounded fuzz campaigns.
- [x] Independent review, disclosed failures, recovery instructions and no unresolved blockers.

M9 is not implemented. At-least-once delivery plus atomic local deduplication does
not guarantee universal exactly-once external effects. Retention automation,
additional consumer registration and production scale policies remain later work.
