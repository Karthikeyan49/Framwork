# ADR030: Governed numeric projections for dynamic query ordering

Status: implemented; acceptance and independent review pending. Scope: numeric
R3b subset. See the R3b implementation report for executed outcomes.

## Trigger and boundary

After the R4 capacity correction, the unchanged R3 workload still failed:
100k-row numeric HTTP sorts with ten clients had p95 898.985 ms against 750 ms.
Solo numeric SQL p95 197.161 ms passed. Statement-level membership and narrow
authorized page selection are retained, but each non-key sort still scans and
sorts the dataset. Do not claim the HTTP floor passed or raise the threshold.

Introduce a fixed-schema numeric projection table and a governed field-index
registry, not tenant-generated columns or tables. JSONB remains primary record
state. The first supported projection is exact numeric ordering for integer,
decimal and money fields. Text/date/search and general uniqueness remain separate
contracts. No metadata field gains an implicit unique guarantee.

Index registration must be authorized centrally for an owner/admin, use trusted
tenant context and derive field type/currency from a supported immutable program
and its physical dataset binding. Store immutable field/schema/anchor witnesses.
Unknown or mismatched types block registration. All keys/FKs include tenant,
application and the physical record namespace; compatible bindings share their
chosen anchor without tying the index to a new compiler hash.

## Consistency and resumable build

Forward migrations create shared registry/projection tables, fixed platform
indexes, forced RLS and NOLOGIN ownership. Workers receive no generic discovery,
read or mutation grants; grant-drift checks must cover both tables. Runtime roles
cannot alter these objects.

Registration takes the anchor's exclusive gate and increments its epoch before
BUILDING becomes visible. Existing database record-write fencing rejects an older
repeatable-read snapshot. Database-owned triggers maintain registered projections
atomically with record insert/update/delete, including compatible older binaries.
Projection values, currency, null placement and source row version must agree with
the record. Application roles cannot independently forge projection rows.

Backfill and verification each use at most 1000 source rows per checkpoint. The
state is BUILDING → VERIFYING → READY. A repeated expected checkpoint observes
already-committed progress without advancing again. Record SHARE locks and the
source-version comparison prevent copying over a newer write or resurrecting a
deleted row; older repeatable-read snapshots abort. Registry NO KEY UPDATE locks
allow projection foreign-key checks by concurrent writers. A bounded MATERIALIZED
source batch joins projections once, avoiding a cached sequential scan per row
before auto-analysis of a new table.

Batch/audit/checkpoint commit together; unknown COMMIT is reconciled through the
authenticated checkpoint GET. Only complete validated coverage becomes READY.
After a verified row unlocks, the database-owned record trigger/FK maintains its
value, version and coverage on later writes/deletes, including inserts behind the
cursor. Runtime credentials cannot mutate projections or skip verification.
Operator corruption after verification is outside that invariant: the schema
owner can already change records/triggers. Retain records and build evidence;
no destructive cleanup belongs to this change.

## Query and acceptance

Use READY projections only when they match the executing field schema and anchor.
Apply entity/field/row DataScope, filters and cursor conditions before LIMIT, with
the existing numeric/null/UUID tie-break ordering. Unready indexes use the existing
bounded query path. Changing the physical plan must not change returned rows,
authorization, pagination or aggregate semantics.

Execute valid/invalid registration, index-build interruption/resumption, stale-RR
writer fencing, concurrent update/delete/backfill, partial/corrupt coverage,
cross-tenant/field-scope/worker negatives and ASC/DESC/null/tie/cursor equivalence.
Repeat migration installation and the unchanged 100k/ten-client HTTP gate. Verify
an indexed plan stops at the authorized page rather than scanning all records;
measure selective-policy cases separately. Independent review is required.

The management API accepts field identity and expected checkpoint, never SQL,
types, currency or a caller-declared READY flag. Central owner/admin and field
permission checks precede execution; PostgreSQL independently verifies live
owner/admin membership, source-field types and active dataset binding. Maximum
registered fields are eight per entity and 32 per physical namespace.

Migrations31–34 are retained as forward corrections: initial platform structures,
compatible registry locking, bounded verification, and bounded batch joins. The
failed earlier workloads and test-fixture defects remain in the report. No new
dependency or increased timeout/pool/latency threshold is required.

This does not complete schema evolution, canonical access across generations,
unique-field metadata, production activation or full query planning. Update the
R3 report with actual outcomes before claiming this design implemented.
