# ADR-016: Typed metadata authoring and immutable source revisions

- Status: accepted
- Date: 2026-10-11
- Scope: M3 — Metadata Registry

## Context

Roadmap M3 and specification 41 require schemas for entities, fields, relationships,
pages, rules, workflows and tools, plus versions and JSON Schema validation.
Architecture 14/15 requires tenant isolation, audit and immutable production metadata.
Compiler semantics, canonical IR, sandbox execution and production activation belong
to subsequent milestones. An authoring revision must never imply executable approval.

## Decision

Embed a closed JSON Schema 2020-12 contract at schema version 1.0.0 and maintain
matching Go domain models. Definitions have stable kind-prefixed IDs, namespace,
label, kind and schema version. Reject unknown keys/kinds/versions, duplicate JSON
or YAML keys, duplicate definition IDs and inconsistent namespaces. JSON and a
JSON-compatible YAML subset converge through the same typed validation path.
YAML forbids anchors, aliases, merge keys, custom tags, timestamp/binary types and
multiple documents. The schema loader cannot fetch remote references. There is no
SQL, script, secret binding, provider executor or arbitrary extension property.

Limit a source to 256 KiB and 500 definitions, nesting to 32 and traversed value
nodes to 16,000. HTTP commands allow an additional 4 KiB for their envelope.
JSON numeric spelling is bounded to 64 characters, exponent magnitude and decimal
precision to 18. Exact decimal normalization avoids floating-point loss and JSONB
numeric spelling drift. Reject NUL and malformed UTF-8 before PostgreSQL insertion.
Persisted JSONB text may use up to 300,000 bytes because PostgreSQL adds whitespace;
reads use this storage ceiling but revalidate the normalized 256-KiB source ceiling.
These are initial safety quotas, not a production throughput guarantee.

Pages use approved declarative component types; expressions use a bounded operator
AST. Tool payload schemas use a closed local JSON Schema subset with no external
references, arbitrary keyword constructors or default credentials. Workflow and
tool models record bounded retry/timeout/sandbox declarations without executing them.
M4 resolves symbols, checks types/arity/workflow graphs/capabilities and generates
IR. M3 deliberately accepts structurally valid unresolved references. Unsupported
future schema versions require a reviewed contract/upgrade; no silent upgrade occurs.

A private validated Source contains normalized typed JSON and a SHA-256 storage
identity. This hash detects source round-trip drift; it is not a canonical compiler
IR hash, signature, approval or production release identity. Envelope base_version
is an unresolved future release reference, separate from source revision numbers.
Copies returned to consumers cannot mutate the validated source.

Metadata owns control.metadata_heads and control.metadata_versions. A tenant/app
head points to a monotonically increasing immutable revision. An append requires
expected_revision (0 for creation), atomically advances the head using compare and
swap and inserts the version, then mandatory audit commits in the same outer M2
transaction. A deferred tenant-inclusive foreign key prevents a committed head
without its referenced version. Namespace is fixed after application creation.
Every returned version is draft; there is no production pointer, activation,
approval, promotion, generated migration or business execution endpoint.

The migration-only M2 installer applies ENABLE/FORCE tenant RLS to both tables.
Runtime can update only the head revision column and insert only permitted source
columns. Database defaults own creator/time attribution. Version UPDATE/DELETE/
TRUNCATE privileges are absent and mutation triggers also reject ordinary DML.
Separate operator SELECT access supports recovery; it does not grant history edits.
Readiness fails for missing required constraints, disabled immutability trigger,
unsafe grants, missing schema or missing forced RLS. Privileged DDL remains trusted.

API composition resolves verified identity/live tenant membership, runs central
policy, and records audit before returning protected data. metadata.read permits
active tenant roles; metadata.write permits owner/admin only. Unknown actions and
obligations deny. Only the exact metadata append route permits POST/CORS POST;
existing operational routes remain read-only. Version lists are capped at 50 with
an exclusive before revision cursor; clients supply the stable app ID.

## Consequences

The modular monolith and existing database/sql, pgx, Goose and OTel contracts remain.
Add jsonschema/v6 v6.0.2 for schema validation and managed
Go YAML v3.0.5 for restricted authoring input. The schema dependency-test graph
adds regexp2 checksums, but that package is not linked into the runtime. No provider, microservice, ERP entity storage, event publication
or authoring UI is introduced.

M2 repeatable-read transactions may abort one overlapping writer with PostgreSQL
serialization failure. Exactly one writer commits; the loser receives sanitized
503 if its mandatory audit cannot commit. A subsequent stale expected revision is
an audited 409. Clients must reload and inspect revision/audit evidence before a
new append. There are no automatic write retries or false claims that ambiguous
COMMIT means rollback. A namespace mismatch also returns 409.

Structural validation is insufficient for executing applications. No M3 source
can become active merely because it has a source hash. Secret-like strings inside
ordinary labels cannot be semantically classified here; metadata is tenant-visible,
so authors must never put credentials in definitions. Future governed integrations
need opaque secret references and a capability model rather than plaintext values.

## Verification and recovery

Unit/race tests exercise the seven primitive models, typed round trips, exact
numbers, strict JSON/YAML, unsafe structures and bounds. Real PostgreSQL tests
exercise policy, cross-tenant reads/writes, independent app identities, historical
reads, cursors, concurrent writes, atomic audit rollback, immutable grants/triggers,
missing/null storage envelope rejection, large JSONB round trips and YAML recovery.
Fresh-database tests apply all seven migrations and detect/repair required
constraint, RLS/grant and trigger drift. Bounded parser fuzz tests add malformed
inputs; they do not replace compiler fuzzing or workload benchmarks.

Deploy additive migrations 00005–00007 before the M3 binary. 00006 adds the deferred
head integrity constraint; 00007 closes PostgreSQL CHECK/NULL envelope gaps found
in review. Previously applied files are not rewritten. Failed Goose transactions
can be repaired and rerun; applied changes require reviewed forward fixes. Down
refuses history destruction. Reverting a compatible binary retains source history.
Integration fixtures retain immutable metadata revisions and append-only audit,
even after their uniquely created IAM rows are removed. Do not reset development
volumes or delete histories to repair failures. Production retention/archival,
schema upgrades, compiler validation and released-bundle provenance need their
later documented implementations.
