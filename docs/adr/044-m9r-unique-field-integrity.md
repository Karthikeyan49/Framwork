# ADR044 — Governed unique-field integrity

18 October 2026 (Asia/Kolkata). Status: accepted and independently verified within
the bounded API foundation. See the [implementation report](../migration/M9R_R3C_IMPLEMENTATION_REPORT.md)
and [independent review](../verification/M9R_R3C_INDEPENDENT_REVIEW.md) for actual
executions, retained failures and limits; overall M9R remains PARTIAL.
Scope: R3c, on remediation/m9r-foundation. No M10 or production activation.

## Contract and compatibility

Add opt-in metadata/IR/compiler 1.4 with boolean `Field.Unique`. Preserve schemas,
canonical bytes and declared readers for 1.0–1.3; restore never recompiles.
Uniqueness is integrity, separate from the physical JSONB layout hash. Reject
intrinsic-ID and unsupported/file fields. Bound declarations to eight per entity
and 32 per application, with the same runtime quota shared by index families.
The executable scalar kinds are string, integer, decimal, money, bool, date,
datetime, enum and reference. NULL/missing may repeat; optional empty text is a
real unique value. Text is exact UTF-8, without case folding, trim or normalization.
Numbers use existing exact SQL conversion and normalized scale; money includes
currency, references preserve their target contract, datetimes use existing UTC
microsecond rounding, including a possible carry beyond year 9999.

Do not accept a declaration as enforcement. Before its constraints are READY,
entity creation/update/patch must fail closed. Reads and ordinary authorized
deletion remain available. Existing declarations do not implicitly create DDL.

## Storage and lifecycle

Use additive shared tenant/namespace/entity/field constraint and private claim
tables, forced RLS, NOLOGIN ownership and no generic worker grants. A fixed SQL
UNIQUE constraint arbitrates exact canonical-key hashes; retain the framed typed
canonical value and fail closed even on hash collision. Claim/source mutation,
receipt, audit and outbox commit or roll back together. App roles cannot edit
claims, declare READY or invoke private converters/trigger functions.

Registration derives its field contract from the bound immutable source and
requires live management plus full relevant read/write authority. Serialize quota
and namespace epoch changes. Use expected generation (initial zero), state and
checkpoint reconciliation; retries must not accidentally start a new attempt.
BUILDING → VERIFYING → READY uses independent batches of at most 1000 source
rows. Freeze entity inserts/updates while building/verifying; keep deletions
atomic with cascading claims. A duplicate aborts that transaction without changing
the checkpoint. Preserve source records and full immutable attempt evidence.

Abort only an incomplete exact-generation/checkpoint attempt. Preserve its
claims/evidence, mark ABORTED and allow an explicit next generation. Stale requests
cannot mutate that next attempt. An aborted declaration remains non-executable;
repair requires explicitly binding compatible non-unique metadata over the same
physical generation, authorized repair, and a new unique build. Do not repair an
unrelated empty preview or silently drop a READY constraint.

Block whole-dataset shadow evolution when any installed active or source/target
declared constraint could be lost, including a transform on another entity.
Registration must reject a frozen dataset; namespace/epoch locks fence races.
Compatible metadata bindings retain installed constraints even if an older source
does not describe uniqueness. Explicit constraint removal/evolution is later work.

Registration, abort and restart lock/update the namespace write epoch. Every
source writer holds its shared gate; older repeatable-read snapshots must abort.
Use dataset → namespace → constraint → source record lock order. Builders use
constraint NO KEY UPDATE, compatible with trigger KEY SHARE, avoiding a head/source
lock inversion. Generation-specific claim keys exclude previous aborted attempts
from current arbitration. Restore pairs application/IR/compiler 1.4 exactly and
derives types, flags and budgets from immutable source rather than caller claims.

## Authorization and existence privacy

A uniqueness conflict discloses value existence, even as a generic HTTP409.
Before attempting ANY new non-null canonical key, require closed Native/Defined
policy and unconditional full-namespace read/write scopes for entity and field,
plus normal entity/row/field actions. Unknown wrappers and scoped/opaque policies
deny fresh and duplicate claims identically before lookup. SQL independently
checks live membership, active role head/hash, deny precedence and ALL grants,
holding tenant/user/membership/head locks through mutation and commit.
Token roles, caller flags and GUCs are not authority.

Mint private permits through an app-only governed SQL port. Bind permits to the
exact backend and xid8, actor, tenant/namespace/entity/record/field, generation and
canonical value hash. Bound slots, clear them after DML and rely on transaction
rollback fencing. Trigger enforcement covers compatible/old metadata and raw DML:
every changed NEW non-null canonical key needs a permit, whether or not the
caller declared uniqueness. Workers cannot mint permits. Legacy write paths omit
permits and deny fresh and duplicate claims identically; the database cannot
identify binary age when processes share the same app credential.
Preserve permitted old reads, unrelated
patches, key clearing and deletions. Do not grant tenant discovery or generic
business reads to a worker to make this feature work.

## Rollout and acceptance

The previous unpushed implementation is unavailable. The current local database
reports migrations through 51; inspect other targets before assigning migration
52. Never replace bytes for a migration already applied elsewhere. Preserve data
and existing immutable artifacts. Quiesce the feature and forward-fix on failure;
there is no destructive down, automatic uniqueness removal or silent rebuild.

Execute deterministic/schema/restore/legacy golden negatives; populated lifecycle,
duplicate rollback, generation replay/abort, exact numeric/time/Unicode semantics,
concurrent same-value claims, hidden-value noninterference, raw tenant/worker/old
writer confinement, quota/evolution races and grant/trigger drift checks. Retain
failed runs and independently review stable source under the two-agent protocol.
Reference capacity uses PG18.6 four CPUs/8GiB, ten tenants, hot 100k records with
representative width, 100 samples after warmup, ten clients and unchanged 2s/500ms
statement/lock budgets. Report expected conflicts separately from unexpected
errors; key-write p95 target 500ms. Retain the 32-client/60s write gate and all
existing query gates. Run full check/test/build and both browser suites before
declaring this bounded feature complete. A compiler-only preparatory slice must
explicitly reject execution rather than promise unenforced uniqueness.

Never log canonical keys, customer values or raw duplicate-key details. Return a
generic conflict only after full-scope authorization has already admitted a claim.
