# ADR042 — Governed datetime and boolean projections

Status: accepted for implementation after independent read-only design review,
16 October 2026 (UTC).
Scope: bounded R3b follow-up; overall M9R remains PARTIAL, M10 stays paused.

## Decision

Extend the existing fixed scalar projection registry, authenticated index APIs,
checkpointed copy/verification and database-owned write trigger to `datetime` and
`bool`. Use additive migration 00050. Preserve every existing record, projection,
index, private converter signature, immutable IR and retained binary reader.
There is no tenant-triggered DDL, compiler upgrade or dependency change.
Search, uniqueness (R3c), aggregate planning (R3d), production authority and wider
operations remain separate deliverables.

Add nullable `value_instant timestamptz` and `value_boolean boolean` columns and
four fixed partial ASC/DESC indexes. Validate physical type exclusivity in a
CHECK, including the existing text/date/enum/reference rows. Nontext types have
empty enum/target options and no collation witness; text contracts stay unchanged.
All types share existing eight/entity, 32/namespace quotas and live manager,
generation/epoch, tenant RLS and transactional audit gates.

A distinctly named private `scalar_native_value` converter delegates existing
types to the preserved four-argument converter and handles the two new types.
Preserve old readiness function-name counts; do not overload old routines.
Reject missing contracts, wrong JSON types, invalid/relative/local timestamps,
infinities, malformed offsets and required nulls with typed errors. Boolean
values are JSON booleans, never truthy strings/numbers. Timestamp strings use the
runtime's bounded RFC3339 shape with an explicit offset; ordinary API writers
already normalize to UTC. Pin conversion TimeZone/DateStyle to UTC/ISO YMD.

Timestamp ordering deliberately matches the existing JSONB `::timestamptz`
query expression, including PostgreSQL microsecond rounding of existing
nanosecond strings. Do not change stored nanosecond values, switch to lexical
ordering, promise nanosecond SQL ordering or introduce a new rounding rule.
Check carry at second/day/year boundaries and submicrosecond ties against the
unindexed expression, not a Go nanosecond-sort oracle. Preserve API input bounds
and canonical UTC normalization, including valid boundary inputs normalized by
older API binaries. A projection never changes the business value returned.
Validate the fraction-stripped UTC second before rounding. Raw-source SQL casting
can reject displacement forms outside PostgreSQL's parser range even though the
API accepts them and writes normalized UTC; keep that existing distinction and
test API offsets through ±23:59. Do not broaden indexed execution to raw values
whose existing fallback cast fails.

## Execution and compatibility

Reuse certified repeatable snapshots, scalar-driven correlated live-view point
fetches and source-version joins. Keep authorization, filter, cursor and DataScope
predicates before LIMIT. Preserve null-last ordering in both directions, ascending
record UUID ties, existing cursor/fingerprint formats and boolean filter limits.
No query gains authority from an index or bypasses field masking.

The trigger and bounded 1000-row build/independent verification maintain both
new columns atomically with source versions. Preserve checkpoint replay, unknown
COMMIT reconciliation, shared source locking, stale writer fencing and old types.
Readiness validates new indexes, CHECK and private function ownership/ACL/settings.
Older binaries retain fallback queries for newly indexed types and their existing
management/readiness behavior; migration must not break their CRUD maintenance.

Recovery is forward-only: preserve source data and all checkpoint evidence.
Incomplete indexes use fallback; malformed source data blocks build, with no
silent coercion, source deletion or weakened authorization. A broken installation
requires quiescence and reviewed forward repair, not a destructive down migration.

## Required evidence

Execute populated pre/post-index ASC/DESC/null/tie/cursor/filter/join/masking
equivalence for both types. Include offsets/DST instants, leap days, all supported
fraction lengths, microsecond rounding/carry, year endpoints and bool false/true.
Exercise typed/raw invalid writes, live manager/tenant/worker boundaries,
audit rollback, interruption/replay, actual lost COMMIT replies and overlapping
copy/update/delete. Verify fresh/repeated migrations, readiness/grant drift,
existing types/pinned artifacts and checksum-pinned retained API CRUD.

Preserve the PostgreSQL 18.6 four-CPU/8GiB reference tier, 2s statements/500ms
locks and numeric/text/date/enum/reference/write gates. Build both indexes over
100k hot rows plus nine 10k-row tenants, report realistic row width and build
checkpoints. Measure ASC/DESC first/second/middle (50000) pages: three warmups,
100 measured requests and ten clients per case, HTTP p95 <=750ms, zero errors,
actual requested index and <=302 source-record visits for 101 selected rows.
Report p99/throughput and projection visits; no constant-time traversal claim.
Require full backend/frontend/build/browser checks, source-kit checksums and
independent executable adversarial review of stable committed code.
