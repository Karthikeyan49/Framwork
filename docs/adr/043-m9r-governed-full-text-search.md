# ADR043 — Governed full-text search

Status: accepted for implementation after independent read-only design review.
16 October 2026 (Asia/Calcutta). Scope: bounded R3b; M10 stays paused.

## Proposed contract

Add explicit optional query `search: {field, text}` for one non-intrinsic string
field, with PostgreSQL's fixed `pg_catalog.simple` configuration and plain
AND-of-words semantics. Preserve literal substring `contains`, immutable IR,
old query/cursor serialization when search is absent, and source values.
No caller SQL, tsquery syntax, dictionary/configuration, ranking, language model,
highlighting, hidden-field search or tenant DDL. Initially support ID-ordered
pages in default ascending ID order, existing typed filters and declared joins;
all explicit sorts/aggregates remain
separate plans. Worker source reads reject the new interactive option.

Use additive migration 51 with separate fixed search registry/projection tables,
NOLOGIN ownership, FORCE RLS, read-only app access and restrictive worker denial.
Registration/build use live manager/source-binding/epoch checks and the existing
checkpoint protocol, audit atomicity and unknown-COMMIT reconciliation. Share the
eight/entity and 32/namespace quota with existing index families. Keep independent
1000-row copy/verification, transactional trigger maintenance and cascading delete.
The same string field can have ordering and search indexes; these count as two
quota slots. No earlier function signature or readiness function-count contract
changes. Add both tables to worker grant-drift checks.

Search is an input field: action checks, entity-covering field scope and live
record DataScope apply before LIMIT. Use exact source versions and canonical
live record views, including retained generations. Bound query UTF-8 bytes and
parsed lexemes; tokenize only bound parameters using plainto_tsquery. Unsupported
or empty/punctuation-only searches fail explicitly. Dictionary/parser/locale
witness drift must fail closed; repair/rebuild needs reviewed forward recovery.

## Physical plan and query context

An owned disposable 100k-row probe found no GIN use with a normal app RLS policy:
the non-leakproof full-text operator was kept above the tenant security barrier
(10.901ms scan). A second probe kept FORCE RLS and unchanged tenant membership
predicates, and added the same plain-text match as an additional restrictive
policy on projections. The actual app plan used GIN (0.612ms). No function was
marked leakproof, no RLS was disabled, and no limit/grant was weakened.

Use that explicit additional restriction: app projection reads require their
vector to match transaction-local `app.search_text`, as well as the independent
tenant/membership policy. This GUC contains query text, never credentials or
authority. Changing it can only change matching vectors inside the existing
tenant boundary. Ordinary index/catalog/record permissions do not widen. The
NOLOGIN schema-owner policy remains available to database-owned build/triggers;
workers still have restrictive false policies and no generic read grant.

The executing query always binds/validates plain text and sets the same local
query context on its exact transaction; it also repeats the explicit match in
its SQL. Clear the context at runtime transaction entry and use the existing
snapshot proof; unproven/legacy adapters use raw full-text fallback. Empty or
missing context returns no app projection rows. Readiness checks policy presence
and context settings, and populated raw-role/pooled-reuse negatives are required.
Neither query text nor vectors are logged. This optimization does not make
caller-set GUCs trusted tenant identities or authorization decisions.

Drive indexed selection from the matching projection and correlated canonical
live-view record point fetches using source versions. Select all eligible matches
before the authorized ID page; do not apply a candidate LIMIT or count cap before
DataScope/field checks. ID cursors can prune projection IDs first. Plain terms
are limited to 256 UTF-8 bytes and 1–16 parsed lexemes; no stemming or caller
configuration. Source text preserves the existing 4096-byte contract.
Candidate generation must never truncate before authorization or return partial
matches. General broad-term capacity is separate from a selective-search floor.

## Required evidence

Pin parser/config/dictionary mappings, template init/lexize functions and empty
dictionary options, database LC_CTYPE/locale/provider/actual collation version and
server_version_num in a database-owned hash. Reject changed/unsupported witnesses
in query/build/maintenance; never mix old vectors and a changed parser. Deployment
upgrade/rebuild is reviewed forward recovery, not automatic reinterpretation.
Only the built-in simple dictionary without external StopWords files is admitted.

Define the query bound by `numnode(plainto_tsquery(...))` in 1–31 (at most sixteen
plain AND operands, including parser expansions). PostgreSQL standard long-word
and punctuation tokenization applies; source text is never truncated. Search is
not substring matching and does not promise every 4096-byte word is a lexeme.
Clear context at search entry/exit as well as pool boundaries, including normal
validation failures and sequential queries; SQL-aborted transactions roll back.
Check the restrictive policy's exact role/mode/expression, not its name alone.

Require indexed/fallback exact results, null/long/Unicode text, word versus
substring semantics, bounded malformed/plain punctuation inputs, cursor binding,
filters/joins/masks/live revocation, populated raw tenant/worker/private-port
negatives, quota races, stale writers, actual lost COMMIT and overlapping writes.
Fresh/repeated migrations and checksum-pinned old API CRUD must remain valid.
Use independent executable adversarial review at a stable commit.

On the existing PG18.6 four-CPU/8GiB tier, 100k hot records plus nine 10k-row
tenants, execute actual selective HTTP search with three warmups, 100 measured
requests and ten clients per case. Require p95 <=750ms and zero errors, inspect
GIN/source plans, and report match count, source visits, p99 and throughput.
Preserve 2s statement/500ms lock budgets and existing five performance gates.
Full check/test/build/browser and source-kit checksum gates remain required.

The first capacity runs exposed an existing unfenced target-view join: selective
search plus a declared self-join returned HTTP503/SQLSTATE57014 under the unchanged
2s statement limit. Fence target joins as correlated live-view point lookups with
OFFSET 0, retaining target DataScope, namespace predicates, masking and LEFT JOIN
null behavior. Qualify this through the same populated 100k-row join case and
retained join/permission tests; do not increase the statement budget.
