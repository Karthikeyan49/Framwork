# ADR041 — Governed enum and reference projections

Status: accepted for implementation after independent read-only design review,
16 October 2026 (Asia/Calcutta).
Scope: the next bounded R3b slice; M9R remains PARTIAL and M10 stays paused.

## Decision and compatibility

Extend ADR040's fixed scalar projection lifecycle to enum and reference fields.
Keep the existing authenticated index GET/register/build API, quotas, epoch
fencing, checkpoint/audit transaction, trigger maintenance, tenant keys, forced
RLS and NOLOGIN ownership. No tenant DDL, compiler/metadata/IR format change,
recompilation, source-data transformation or new dependency is required.
Uniqueness, datetime/bool/search indexes and SQL aggregate expansion remain
separate designs. Do not imply production active-release execution.

Use additive migration 00049; never edit applied migrations. Add enum-values
JSONB and reference-target witnesses to the existing registry, with empty defaults
for old string/date registrations. Validate a private immutable options contract
in a table CHECK. Registration derives and validates options from stored source,
rejects intrinsic IDs, checks referenced entity existence, and stores enum values
in byte-sorted canonical order to match compiled IR. The executing Go reader
compares these exact options as well as type, requiredness and storage hash.
Callers cannot supply type/options/collation or READY. Preserve live manager and
active-generation checks and combined eight/entity, 32/namespace quotas.
The private CHECK returns definite false for NULL/wrong shapes and requires
canonical byte ordering, unique nonempty strings and the existing size limits.

Enum ordering remains the existing database-collated text ordering, not declaration
order. Values must exactly match a declared member; no folding or normalization.
The existing 128-Unicode-character enum limit needs at most 512 UTF-8 bytes, so
every valid enum fits the text key budget. Reference values retain the existing
canonical lowercase 36-character UUID text contract and text ordering. An index
does not establish reference integrity or grant read access to the target;
existing relationship/command policy and FK handling remain authoritative.

Add four fixed partial text indexes for enum/reference ASC/DESC. Keep existing
text/date/numeric indexes untouched. Each new registry/projection field-type
constraint admits the two additional types; enum/reference cannot overflow.
Store the same database default-collation witness as other text ordering.
Reject reported/actual/registry collation drift, including legitimate NULL handling.
Null-last ordering and ascending record-UUID ties/cursors remain unchanged.

## Validation, maintenance and recovery

Preserve the existing three-argument string/date conversion function for old
callers. Use a distinctly named helper, not an overload: older readiness readers
count functions by name. The new private typed projection helper delegates old types and
validates enum/reference values against the stored options. The database-owned
write trigger and copy/verification routine use it atomically with source version
maintenance. No worker/dispatcher/public execute or generic table access is added.
Readiness checks the new validated options invariant, indexes and private routine
ownership/ACL/search path. Old binaries keep their existing fallback for these
previously unsupported index types; their string/date/numeric behavior is unchanged.

Copy/independent verification stays at 1000 source rows per checkpoint. Use the
ADR040 certified snapshot and correlated live-view point fetches, with all entity,
row and field authorization/filter/cursor predicates before LIMIT. Do not replace
canonical views with raw records. Snapshot proof, unknown-COMMIT reconciliation,
stale-writer fencing, overlapping backfill/update/delete and audit rollback remain
requirements. Retained/current generations apply live owners/shares/tombstones.

Recovery is forward-only and retains records/registrations/checkpoints. Invalid
existing witnesses or source values block installation/build; never silently
coerce values, remove evidence or weaken grants to recover. An incomplete index
uses the existing bounded query path. Existing string overflow fallback remains.

## Executed acceptance required

Compare pre/post-index ASC/DESC/null/tie/cursor results for Unicode and maximum
enum members, reference UUIDs, selective filters and joins/masking. Reject wrong
types, undeclared/oversized enum members, malformed/uppercase references,
intrinsic IDs, stale/forged option witnesses and cross-tenant reference commands.
Execute populated raw app/worker/tenant/manager/field/record-scope negatives,
backfill/concurrent maintenance, unknown results and transactional audit faults.
Rehearse fresh/repeated migrations, readiness/grant/options drift and old numeric/
text/date/pinned-generation compatibility. Use independent executable adversarial
review of stable commits and fix findings before acceptance.

On the existing verified PostgreSQL 18.6 four-CPU/8GiB reference tier, build both
indexes over 100k hot records plus nine populated 10k-row tenants. Compare actual
HTTP output with an independent unindexed oracle. Exercise enum/reference ASC/DESC
at first/second/middle (position 50000) pages with three warmups, 100 measured
requests and ten clients for each case. Require p95 <=750ms, zero errors, actual
requested indexes and <=302 source-record visits for a 101-row selected page;
report p99/throughput and projection visits rather than claiming constant-time
traversal. Keep 2s statements/500ms locks and existing numeric/text/date/write gates.
Full backend/frontend/build/browser checks and source-kit checksums must pass.
