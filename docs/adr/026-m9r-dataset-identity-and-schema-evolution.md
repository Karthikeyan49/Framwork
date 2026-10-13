# ADR026: Durable datasets and explicit JSONB schema evolution

Status: staged implementation. R2a schema plans/transforms independently verified;
R2b preview equal-schema bindings independently verified at `145eaa4`. Full
schema evolution remains pending.

## Implemented tranche boundary

R2b supports generation 1 and identical storage-schema hashes only. Labels and
catalog/release hashes can change without copying records. Existing records,
owners, teams, shares and links remain the single current physical source used
by both old and new readers. New incompatible generations/canonical identity
tables, optional-addition reader projection, shadow migration and new workflow
data pins are not implemented. Their design below is not an implementation claim.

New EMPTY/binding admission runs the trusted server compiler/catalog once and
requires its exact artifact hash. Restore alone cannot attest caller-supplied
compiler provenance. Historical ADOPT instead requires exact bytes matching the
established legacy namespace and immutable registry source; missing provenance
through that namespace/witness blocks adoption. Binding execution never compiles.

An ADOPT request seals the explicit witnessed namespace, then checks at most
100 records per durable verification transaction. EMPTY allocates an independent
anchor. The caller retains a workspace UUID for create reconciliation. Bindings
restore exact stored IR; only the active binding writes. Known incompatible
schemas and unimplemented new workflow/data-pin combinations fail closed.

## Problem and decision

Preview records currently use source revision and compiled IR hash as their
namespace. That is appropriate for isolated scratch previews, but a new compiler
or compatible release cannot continue using existing application data. Keep the
legacy preview contract; add explicit tenant/application/dataset UUID identity.
An operator/authorized sandbox administrator must choose whether to adopt an
existing namespace or create an empty dataset. Never infer a merge by record ID,
application name or source similarity.

Adoption requires a supported immutable IR witness whose hash equals the legacy
namespace. An explicitly compiled legacy witness is usable only if its exact hash
matches; it is not a replacement artifact. Missing/unsupported historical IR
blocks adoption rather than inferring a schema or recompiling an active release.
It must also match application identity and the legacy storage contract.

Keep the physical record tables and their tenant keys/FKs. Immutable dataset
bindings map each compiled release to a data schema generation and physical
anchor. Compatible releases share their chosen anchor; IR/compiler/catalog hashes
do not define durable identity. Preserve original stored IR bytes and restore
with explicitly supported readers, without invoking today's compiler.

Environment and workspace UUID are immutable. The initial environment is preview
only; production and M11 sandbox creation remain gated. All keys include tenant,
application and dataset; a unique tenant/application/physical-anchor key prevents
an adopted namespace from aliasing unrelated datasets. Schema generations belong
to their existing logical dataset and receive separate physical anchors.

Dataset identity, schema generation, metadata revision, IR version and compiler
version are separate facts. A CAS-controlled sandbox pointer chooses new writes;
old bindings remain immutable and readable according to their compatibility
window. Production activation still requires R6/M13 approval and verified
candidate authority. A sandbox binding is not a production approval.

## Ownership, permissions and in-flight work

Stable record identity also owns current owner/team/share facts and tombstones.
Schema shadow copies must not resurrect deleted records or retain stale grants
for pinned workflows. Policy checks use live logical identity and sharing;
immutable workflow IR/input pins continue to reference their declared data
generation. Retired incompatible generations are retained read-only snapshots.
They are not promised to reflect later writes to a new schema. Worker ports stay
limited to the exact bound record and captured fields; workers receive no generic
dataset/record/schema mutation or global mapping discovery grants.

All readers use the current canonical ownership/sharing/tombstone facts, including
old preview/legacy routes and the R1 SQL source port. Sealing writes alone is
insufficient. A security-invoker reader joins mapped records to logical identity;
unmapped legacy records keep their original behavior. Mapped missing identity or
tombstoned records deny. The worker port returns only the exact masked source and
current identity facts under its existing job capability. It resolves an immutable
pinned data generation, never the active generation. Worker share reads use a
narrow corresponding port, without broad table grants.

Audit/events retain the execution release, while aggregate ordering and command
receipts use durable dataset/record identity. Adopting an existing namespace must
preserve its earlier sequence/receipt lineage; a compiler change cannot restart
that record's causal stream. Independent scratch previews keep separate streams.

All app writes take a shared dataset gate; binding/freeze/cutover uses its exclusive
gate. A database trigger also fences old binaries and legacy routes after adoption.
Missing/stale binding context denies adopted-dataset writes. The shared gate does
not serialize independent writers. App GUCs carry trusted composition context,
not worker credentials; R1 capability enforcement remains separate.

The gate is a row lock: `FOR SHARE` for app commands and `FOR UPDATE` for freeze/
binding/cutover. `KEY SHARE` and advisory-only locks are not substitutes. Commands
lock dataset before record and reference/sequence rows; simultaneous references
use a documented deterministic key order. RR locking must reject changed epochs
instead of accepting a stale snapshot. Existing workflow instance→job fencing
stays unchanged; worker source reads use retained generations and do not acquire
the app writer gate in the opposite order.

## Schema changes

The compiler/data boundary owns a canonical, versioned storage evolution plan
derived from the old and target immutable programs. Labels do not change schema
identity. Optional additions can share storage; old writers are disabled once a
new binding becomes active so full replacement cannot remove newly added fields.
Field ID renames are explicit operations, not inferred from labels. Closed plans
support declared renames, validated lossless conversions and required-field
backfills. Reject entity removal, undeclared field loss, ambiguous mappings and
unsupported currency/reference changes. No customer SQL or shared-table DDL.

Compatible old reads project only their declared schema before value validation;
new optional fields must not break their reader. Command validation remains
strict, and this projection is not an excuse to accept corrupted unmapped data.

Changing a standalone compiler's `NONE` plan without a base schema would invent
knowledge it lacks. Bound compilation therefore emits a separate hashed evolution
artifact with its explicit base schema/release, operations and target schema.
Candidate construction must include this artifact in the approved bundle.

Incompatible evolution freezes writes and creates an isolated shadow anchor.
Bounded resumable phases copy/transform records, rebuild validated references,
preserve current ownership/shares and verify counts plus canonical manifests.
Copy, audit and checkpoint commit together. Replayed batches must be equivalent;
unequal pre-existing destinations fail rather than overwrite. Invalid values
block cutover with bounded diagnostics; old data stays untouched. Only a validated
target and unchanged base generation can become active through CAS.

States are ADOPTING/OPEN/FROZEN/COPYING/LINKING/VERIFYING/READY with a monotonic
freeze epoch and immutable base/target artifact identities. Phase checkpoints
include deterministic last-key/count/hash facts and a unique operation key.
Manifests cover record IDs, versions, typed values, owner/team/share/tombstone and
reference facts. Lost COMMIT replies are reconciled through the checkpoint before
replay; a timeout does not mean a batch failed to commit.

## Recovery and gates

No destructive cleanup belongs to this change. Retain source anchors, bindings,
identity facts, IR and migration evidence. Interrupt/resume/failure tests and
manifest reconciliation are required before acceptance. On failure pause the
migration and forward-fix or restore the pre-cutover binding after verifying that
the source remained sealed. Post-cutover writes require an explicit reverse plan
or restore/reconciliation; metadata rollback cannot undo incompatible data or
external effects. Typed ERP Domain Pack storage remains a separate adapter.

Deliver and review binding/identity and evolution execution as separate changes.
Do not claim the entire R2 gate complete after the first tranche.
