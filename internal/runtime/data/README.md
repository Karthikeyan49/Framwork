# Typed data runtime — V3 M6R

RecordStore Query/Command require immutable compiler Scope, verified tenant Access,
caller-owned bounded Executor, action Guard and SQL ScopeResolver. Customer metadata
never supplies SQL or DDL. Dynamic data uses tenant-scoped JSONB records; typed Domain
Pack storage is a separate future adapter. LegacyAdapter is read-only.

DataScope is compiled into predicates before reads/filter/sort/join/aggregate input.
Commands check existing and proposed state, field scopes and optimistic versions;
dataset serialization and tenant-inclusive reference links protect relationships.
API composition commits mutation, audit and outbox intent together. It does not publish
or execute external effects. HTTP preview is development/test only.

ADR-020 supersedes ADR-018's application-memory visibility preflight. See the real
M6R scope, migration, release, worker and atomicity integration tests. Budgets remain
100 rows per query, 32 entity fields, bounded predicates and a three-second transaction.

M9R R2b adds explicit preview dataset/workspace UUIDs and immutable equal-schema
bindings. Legacy scratch previews remain isolated by revision/hash. ADOPT seals
one exact witnessed namespace, verifies bounded durable batches, then exposes the
same records through a restored binding. EMPTY allocates separate data. Only the
active binding writes; old bindings read the retained source and current ACLs.
Event sequencing retains the origin anchor while evidence retains execution IR.
Different storage schemas and new workflow/data-pin combinations are rejected;
full shadow schema evolution and R4 lock/retry/idempotency work remain pending.
