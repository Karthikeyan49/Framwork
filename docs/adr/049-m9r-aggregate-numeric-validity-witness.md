# ADR049 — Transactional numeric validity witnesses for SQL aggregates

18 October 2026 (Asia/Kolkata). Status: accepted for implementation following
independent read-only design review, before code. Supplements ADR046–048;
executable review and capacity remain required. No production activation.

## Evidence and decision

Raw per-input validation remains required when no equivalent database witness
exists. The refined raw path passes correctness, but repeated100k JSON validation
misses the unchanged ten-client capacity gate, even with explicit four-slot API
admission. Preserve every failed measurement. Existing numeric projections alone
are insufficient: their original converter accepted integer1.0, whereas the
runtime integer contract rejects that representation. Migration33 made the
current converter strict; historical READY rows were not recertified.

Add a nullable versioned `aggregate_valid_v1` column to existing numeric projections in a new
forward-only migration. Do not update existing rows or their values. A fixed-
search-path SECURITY DEFINER BEFORE INSERT/UPDATE trigger independently reads
the exact index field contract and source row, requires matching source_version,
and derives validity from raw JSON using SQL_V1's strict required/null, native
type, integer lexical/safe range, decimal length/18-place/range and exact money
object/currency rules. Reuse the current strict numeric converter, catch only
its expected22023 data rejection, and bind the flag to null-safe equality of the
derived numeric and NEW.value_number. Never accept a caller-supplied flag or
certify a projected number merely because the raw input is valid. No new
runtime grants, tenant DDL, mutable registry or customer SQL is introduced.

After normal action/field/scope admission, SQL_V1 sum may use an existing READY
numeric index only when its storage/type/currency/required witness exactly
matches the pinned compiler contract. Keep source selection and canonical access
in the same certified snapshot. LEFT JOIN projections on every tenant, app,
physical revision/hash, entity, field and record key. Materialize one numeric
scalar per source row: missing projection or mismatched source_version is an
invalid NULL sentinel; true validity uses its exact numeric value (optional-null
becomes zero); false validity remains invalid; a legacy NULL flag evaluates the
existing raw guarded normalization. Never filter invalid inputs or silently
shrink the count. Any invalid count rejects the entire result. Building/absent
indexes keep raw normalization. Count remains independent of unrelated fields.

The witness is transactional data, not a cache of policy or authorization.
Source values and its projection/version/validity commit together. Current
owner/team/share/tombstone rules still come from canonical source planning;
retained generations keep their own historical values. Expose no raw projection
write path. Database operators remain trusted maintenance authority; ordinary
app/worker/dispatcher credentials cannot forge or erase projection evidence.
Readiness verifies the versioned column/trigger, NOLOGIN function owner, fixed
search path, absent public/runtime execution and projection write privileges.
Do not add registry FOR SHARE locks: builders already lock registry then source,
whereas writers lock source first. Use the existing compatible key-share/snapshot
semantics and test build/writer concurrency rather than recreate lock inversion.

## Verification and recovery

Exercise fresh builds, legacy NULL flags, valid zero/optional-null, required-null,
integer1.0, wrong currency/types, out-of-range cancelling pairs, role grants,
caller-forged flags, missing/stale projections and exact sums. Demonstrate atomic
source/projection rollback and concurrent updates. Measure both raw/unindexed
and governed-index paths honestly. Existing reader binaries and index behavior
must remain compatible. No capacity PASS without executed unchanged100k/ten-client
gates plus independent adversarial review and all older regression/workload gates.

Recovery disables opt-in aggregate use and forward-fixes the new trigger/planner;
retain the nullable column, records, indexes and immutable metadata. Existing
legacy rows require no data rewrite and remain readable through raw validation.
This does not introduce grouping, arbitrary expressions, reports, production
release execution or M10. Freshly governed indexes are a declared prerequisite
for any accepted indexed capacity envelope, not a claim about all raw queries.
