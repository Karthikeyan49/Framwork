# Event / Outbox Specification

Outbox fields:
ID, tenant, topic, aggregate type/id, event type, schema version, aggregate sequence, payload, trace ID, metadata version, created/dispatched timestamps, attempts, last error.

Consumers store processed event IDs/idempotency keys.

Redis Streams is suitable for moderate short-retention streaming; keep a transport adapter so a different broker can replace it.
