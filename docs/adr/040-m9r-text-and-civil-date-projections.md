# ADR040 — Governed text and civil-date projections

Status: accepted for implementation, 16 October 2026 (Asia/Calcutta).
Scope: the next bounded R3b slice. M9R remains PARTIAL; no M10 or main merge.

## Decision and compatibility

Extend ADR030's governed physical indexes to `string` and `date` fields. Keep
JSONB primary, existing numeric indexes/readers intact, immutable IR unchanged,
and the same authenticated GET/register/build routes and checkpoint protocol.
Enum/reference/instant indexes, full-text search, uniqueness (R3c) and aggregate
planning (R3d) are separate work. This is not full R3b or production activation.

Use additive migration 00047 with fixed shared `runtime.scalar_indexes` and
`runtime.scalar_projections`, NOLOGIN ownership, forced tenant RLS, SELECT-only
app access and no generic worker access. SECURITY DEFINER registration/build
ports reuse the private live owner/admin and active-generation check. Source
metadata supplies field type/requiredness; callers cannot supply SQL, casts,
collations or READY. Numeric and scalar registrations share the existing eight
fields/entity and 32 fields/physical-namespace quota under the exclusive anchor
gate. Forward-replace the numeric registration routine only to share that quota.

Copy and independent verification each visit at most 1000 source rows per
checkpoint, with NO KEY UPDATE registry locking and SHARE source locking.
An additional database-owned record trigger maintains scalar values, overflow
markers and source row versions atomically, including writes by compatible older
binaries. Cascading FKs remove projections on delete. Do not add a dataset-wide
write counter. Keep epoch fencing, checkpoint/audit atomicity and lost-response
reconciliation. Never delete source records or old index evidence to recover.

Independent review found that SQL registration admitted the reserved intrinsic
record identifier even though Go rejected it. Forward migration 00048 must
reject that identifier before registration and enforce the same invariant with
a registry CHECK constraint. Existing incompatible registrations block migration;
installation never deletes witnesses or records to repair them. The intrinsic
ID remains stored in the typed record column, outside custom JSONB projections.

## Text size and ordering

The existing text contract accepts 4096 UTF-8 bytes. A full text B-tree key can
exceed PostgreSQL's index-row budget. Preserve valid writes: store exact values
up to 512 bytes in the ordered projection; mark longer values as overflow without
putting them in the B-tree. Null is distinct from overflow. A fixed partial
overflow index provides a bounded existence probe. If any source row overflows,
use the existing typed JSONB query path for that field; do not truncate values,
omit rows, narrow writes or pretend the fast-query capacity gate covers it.

Use the database default collation for both the text projection and existing
JSONB expression. Registration must not change locale ordering. Store the
database's actual collation version; block a stale witness or reported/actual
version drift until reviewed operator repair/reindex. Do not accept a caller's
collation or silently normalize/fold case/accents. Test Unicode, case, nulls,
ties and both directions against the unindexed path.

Overflow admission and execution must share the exact transaction snapshot.
Use the existing `postgres.RuntimeSnapshot` proof; a setting labelled repeatable
read, an autocommit pool or an opaque wrapper is insufficient. Such callers use
the unindexed path. In the certified path, a concurrent long insert is either
invisible to both the probe and page or visible to both. Row/field DataScope,
filters, cursor predicates and source-version joins remain before LIMIT.

Independent execution also exposed a cold-statistics plan that flattened the
security-invoker record view and scanned the dataset for each candidate ID.
Drive scalar pages from the projection and use a correlated LATERAL record
lookup with namespace, ID and source version inside an OFFSET 0 planning
barrier. Use the identical projection ID for cursor comparisons and ties so
projection-only cursor predicates can run before record fetches. Fetch the
selected page through a second correlated point lookup, also preserving the
live record view. Keep every authorization/filter predicate before LIMIT; do
not replace the view with raw records, disable planner strategies or raise
timeouts. Apply the outer point lookup to the existing custom-field page path
too. Verify cold, unanalysed fixtures as well as the stated warm capacity gate;
JIT was not the reproduced cause and its configuration remains unchanged.

## Civil dates and cursors

Dates retain Gregorian years 1–9999 and exact YYYY-MM-DD validation using explicit
components, independent of session DateStyle/time zone. Store native `date` and
preserve the existing ASC/DESC, null-last and ascending UUID tie convention.

The existing 1024-byte decoded cursor/2048-character frontend limit cannot carry
a valid long text sort value. Bound decoded cursor JSON to 32 KiB and encoded
input to its base64 length, before allocation; align the frontend decoder. This
covers 4096-byte strings even with JSON escaping and keeps existing cursor bytes
and fingerprints readable. No signing, authorization or query budget is removed.

## Executed acceptance required

Run populated API/raw-role tenant, hidden-field, record-scope and worker-denial
probes; quota races; typed/date/long-value validation; interrupted copy/verify,
stale writer fencing, write/delete/backfill concurrency and audit rollback.
Compare indexed/unindexed ordering, filters, joins, cursor pages and masking;
include overflow transitions and a concurrent snapshot probe. Check readiness and
grant drift, existing pinned numeric/data/workflow compatibility, all migrations,
full backend/frontend/browser regressions and independent adversarial review.

On the existing PostgreSQL 18.6, 4 CPU/8 GiB reference tier, build 100k populated
rows in checkpoints and exercise text and civil-date first/cursor pages,
including a boundary at position 50,000 in both directions, with
ten HTTP clients, at least 100 measured requests after warmup. Require unchanged
HTTP p95 <=750 ms and zero errors, report p99/throughput, and inspect actual index
plans and distinguish projection entries filtered from source-record probes.
A passing middle-page gate does not certify constant-time cursor traversal.
Retain 2s statements/500ms locks and the existing numeric/write gates.
Selective policies, overflow and unindexed fields retain explicit bounded-query
limits, not the unrestricted fast-path guarantee.

Recovery is forward-only: keep records and index checkpoints; incomplete indexes
fall back, unsafe witnesses fail closed. Quiesce and forward-fix a broken trigger
or collation/index installation rather than restoring weaker grants or removing
evidence. No automatic down migration or destructive cleanup is provided.
