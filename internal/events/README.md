# Transactional outbox and inbox — M8

Append coordinates audit evidence and an outbox intent using the same caller-owned
Executor as the business mutation. Failure propagates to WithRuntime and rolls back
all changes. No DB-commit-then-publish dual write exists. Intents contain identifiers,
versions and event class, without flexible business field values or secrets. M8
freezes these identifiers, source revision/IR hash, actor, UTC time, request ID and
W3C trace parent into schema1.0 envelopes. Their exact stored byte hash is stable.
Record versions are distinct from the monotonic per-aggregate event sequence.

The aggregate epoch is tenant/app/source revision/IR/entity/record, m8-v1. A dead
predecessor blocks successors in that epoch; there is no global ordering promise.
Historic intents are LEGACY_PAUSED, retained without invented payloads or dates.

The dispatcher claims one PostgreSQL-time five-second lease, with token/generation
fencing and SKIP LOCKED. Network publishing runs after the transaction. Known
serialization-aborted lease claims may retry four times; unknown commits wait for
lease expiry. ADR062 now separates the five-second publication lease from the
15-minute receipt window, with20 attempts and500ms exponential backoff capped
at60 seconds. LEASED retries wait until lease expiry plus backoff; positively
known publish failures use backoff directly. Issued rows keep their existing
deadlines across migration00055; only a later claim/finish uses the new policy.
SENT events without inbox completion are
republished, including after broker loss. The database records attempts and DEAD
state. Delivery is at least once, not universal exactly-once remote execution.

Redis carries canonical routing references only. Dedicated ACL sender can XADD;
receiver can manage its group, read/reclaim and acknowledge; cache cannot access
the stream. Operator credentials are separate. Fresh/pending reads alternate and
reclaim advances a cursor. Durable DEAD references may be acknowledged to avoid
blocking other work; their immutable event and replay history remain in PostgreSQL.

Workers verify exact tenant/event/hash/consumer/attempt/token routing before minting
an opaque job ticket. WithJob re-resolves the original actor's live membership and
release pin. Only that job's envelope/inbox is visible through forced RLS. Default
deny policy requires decision provenance and the audit obligation. Inbox, trusted
local handler effects, audit and completion share a bounded transaction; errors
rollback everything. A changed hash for the same event is rejected. ACK follows
confirmed commit; a lost commit response or ACK retains redelivery and deduplication.
Malformed/unroutable pointers are hash-only quarantined before ACK, never logged.

ADR063 requires READ COMMITTED for event authority at binding and every use;
raw RR/SERIALIZABLE and imported snapshots cannot retain revoked membership.
Event callbacks use that isolation without shared-snapshot certification. API
and workflow transactions remain REPEATABLE READ. Revocation is visible on
subsequent statements; no raw-process lock/liveness guarantee is claimed.

Production composition currently installs only a receipt handler in development/
test. This handler does not read current business records, execute a workflow or
send external actions. LocalHandler is a trusted Go integration port, never an AI
or metadata SQL capability; later handlers must use policy/DataScope and business
module ports for mutations and events.Append for new event/audit writes. Actual
business-value event snapshots need a new reviewed schema; do not hydrate mutable
records to reinterpret an old event. Production activation consumers remain M13.

`events.Ready` checks emission and receipt contracts, grants and enabled freeze/
immutability triggers. API readiness and enabled delivery process startup use it.
Correlated OTel publish/consume spans include event/tenant/source/request/attempt;
audit evidence records event and consumer IDs. No DSNs, payloads or secrets appear
in warnings. Alerting, retention automation and scale tuning remain M15/M18.

See [ADR022](../../docs/adr/022-m8-transactional-delivery.md) and the historical M8
verification reports for the original protocol. Current work follows the M9R
remediation plan; do not start M10 automatically.

Current M9R timing authority is [ADR062](../../docs/adr/062-m9r-event-outage-budgets.md).
Its reviewed correction is [ADR063](../../docs/adr/063-m9r-live-event-authority.md):
policy `event-outage-v3` verifies actual private retry outputs without exposing
the helper, while preserving timing and immutable issued evidence.
See the [outage runbook](../../docs/operations/R7B_EVENT_DELIVERY_RUNBOOK.md) for
finite retry/receipt budgets, the longer broker-loss repair delay, controlled
forward migration and the real `make test-event-outage` rehearsal. This does not
add production consumers, workflow renewal, replay privileges or external tools.
