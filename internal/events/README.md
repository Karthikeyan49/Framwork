# Transactional event intent — M6R

Append coordinates audit evidence and an outbox intent using the same caller-owned
Executor as the business mutation. Failure propagates to WithRuntime and rolls back
all changes. No DB-commit-then-publish dual write exists. Intents contain identifiers,
versions and event class, without business payloads or secrets. M8 adds delivery,
inbox deduplication and recovery scheduling. See ADR-020 and M6R atomicity tests.
