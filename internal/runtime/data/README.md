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

R5d (ADR037) separates the interactive `QueryVisible` port from strict execution
inputs. SQL masks projected fields without filtering entity-visible rows;
field predicates require coverage of the entity scope. `patch` merges changed
values under CAS and preserves untouched inaccessible fields and reference
links. CONDITION write scopes and unsafe hidden-size reservations fail closed.
Existing `update` remains replacement. No worker port uses visible queries or
patch; workflow/rule inputs remain strict.
Opaque Go action callbacks additionally require an enforced RR/Serializable
snapshot and trusted ID/owner header preflight (32 headers including lookahead).
Predicates check the full entity-visible universe before caller filtering.
`QueryVisibleWithPolicy` optimizes only exact closed Native/Defined kernels;
composition must pass the same live engine used by its guard. Unknown wrappers
cannot inherit that optimization. See ADR037 for the explicit callback bounds.

R3c (ADR044) adds governed unique-field registration/copy/verification/abort over
the same physical namespace. NULL may repeat; optional empty strings and exact
Unicode bytes are real keys. Typed numeric/time conversion matches existing SQL
semantics. Incomplete declarations block entity writes; READY claims are enforced
atomically with records and receipts. New non-null keys require closed policy,
full entity/field read/write scopes and a private transaction-bound SQL permit.
Scoped/opaque/worker/legacy write paths cannot probe fresh versus duplicate keys.
Compatible bindings retain installed constraints even if their metadata omits
the flag. Exact generation/checkpoint reconciliation preserves aborted evidence;
READY removal, uniqueness-preserving shadow evolution and administration UI are
not implemented. The R3c report records executed gates and recovery limitations.
