# ADR031: Checkpointed shadow generations and live record access

Status: implementation candidate; independent execution and capacity review pending.

ADR026 remains the durable identity decision. This tranche executes its schema
plans in the existing preview dataset API, with no production release activation.
Equal storage hashes still share their original physical anchor through Bind.
Every unequal schema, including optional additions, uses a retained shadow
generation. This deliberately avoids adding in-place projection semantics to
old readers. Each old reader keeps its original typed values and IR; after
cutover its data generation is a read-only snapshot.

## Protocol

Owner/admin submits an immutable target IR witness and typed evolution intent,
an operation UUID and expected base binding. The API admits the exact trusted
compiler artifact once. Stored base/target IR and the hashed closed plan are
restored, never recompiled, for COPY/LINK/VERIFY and activation.

Writes use a shared dataset gate. Prepare exclusively freezes it and allocates
an immutable generation mapping. The copy function admits one exact destination
record through a private backend/transaction context; no generic shadow-writing
grant, metadata SQL or tenant-selected DDL exists. A batch processes at most 100
records and 512 work units, with at most 256 shares and 32 links per record.
All records are copied before links, so cycles and self references work. Unequal
existing destinations reject instead of being overwritten. Headers, versions
and source rows are preserved. Typed transforms and serialized size limits apply.

Verification recomputes target values and exact header/link facts, hashes ordered
manifests with live access facts, and checks physical counts and identity coverage.
Final scans retain the normal statement timeout; batches alone are not evidence
of a 100k-record capacity guarantee. Checkpoint/audit commit together. GET progress
reconciles a lost response; an old expected checkpoint cannot repeat effects.
READY remains sealed. Activation restores original witnesses and CAS-checks
the base binding/generation, checkpoint, manifests, counts and access epoch in
one transaction with audit. This is preview pointer activation, not approval to
deploy production metadata.

## Current authorization and causality

The initial single-source adapter remains authoritative until copy establishes
complete logical identity. Thereafter security-invoker record/share views apply
current owner/team/share facts and tombstones to every mapped generation and
legacy reader. Missing identity denies, with no raw-record fallback. Source
identity updates are mirrored only from the current generation. Team foreign keys
include tenant identity. Product policy still requires R5; this change does not
turn built-in ALL scopes into an ownership policy.

The access epoch changes during FROZEN administrative changes, invalidating prior
verification. Ordinary OPEN deletes do not upgrade the shared dataset gate or
increment a dataset-wide counter. Administrative record/access mutations must
lock the dataset exclusively before its records, following the same lock order
as migration. Operator/migrator credentials remain trusted maintenance authority;
they are absent from API/worker processes and are not customer capabilities.

Workflow bindings pin dataset, binding, generation and physical anchor separately
from execution release. Start capture shares the write gate before binding; old
NULL pins retain their original namespace. Worker source/share ports remain exact
job, actor, record and captured-field capabilities. Workers cannot select generic
generation, identity, share, evolution or private context tables.

Events retain the original causal lane across generations. An immutable,
server-derived sidecar records physical source generation; existing envelope 1.0
bytes are unchanged. Command receipts retain their original fingerprint and
actor/app/key ownership, so old ambiguous commands reconcile without replaying
effects into a new generation.

## Recovery and limits

Before activation, abort reopens the unchanged base binding and retains shadows,
maps, witnesses, identity and checkpoints. After activation, abort is rejected.
Reverse conversion or backup/restore plus reconciliation requires its own plan;
old snapshots are not a reversible history of subsequent new-schema writes.
No destructive cleanup/down migration belongs to this tranche.

Incompatible entity/field removal, currency/reference changes, lossy conversions,
oversized records and unsupported IR versions block. Quarantine UI, automatic
index inheritance, scheduled migrations, production approval and dataset-management
UI remain separate work. Numeric indexes can be explicitly built for the current
generation; old index facts are retained. Typed ERP Domain Pack storage is unchanged.
