# ADR037 — Field visibility and partial writes (R5d)

Status: accepted for implementation, 15 October 2026 IST.

## Decision

Keep entity DataScope as the row boundary. The interactive preview uses an
explicit visible-query port. PostgreSQL constructs the projected JSONB and
per-record `field_access.masked` / `field_access.writable` lists; hidden values
never cross that port, including hidden required fields and missing/null values.
Mask membership describes the policy decision, independently of property
presence. Intrinsic record ID, version, ownership and timestamps remain visible
headers of an authorized row. Metadata/schema labels are not secret values.

Rules, workflows, worker source capabilities and legacy adapters retain strict
input semantics: reject inaccessible inputs instead of evaluating redacted null.
Strict point reads preflight the requested/predicate fields with trusted record
ID/owner in the certified snapshot, before fetching custom values. They fetch
only declared fields plus core/cursor necessities. Strict point joins check the
target's trusted owner before fetching target values. The worker adapter keeps
its private source function and exact lease confinement. Unrelated hidden fields
are excluded from input payloads and do not require gratuitous authorization.

Action callbacks also receive trusted row ID/owner context. For interactive
reads, opaque Go engines use a header-only preflight and bounded ID-array SQL
predicates; values still remain in SQL until the masking decision. Predicate
preflight examines the entire entity-visible universe before any caller filter,
sort, cursor, join or result limit, except an exact record selection. Reject a
predicate if any row denies the field. Opaque universes/projected pages are
limited to 32 headers; larger requests fail budget. Arrays bound SQL parameters
per field/action, rather than expanding one parameter per row/field pair.
The opaque port verifies actual PostgreSQL REPEATABLE READ or SERIALIZABLE
isolation before preflight. READ COMMITTED/default executors fail closed. The
API's existing WithRuntime transaction supplies the stable snapshot; headers,
owner decisions and final value selection cannot observe different snapshots.
Postgres also certifies the actual WithRuntime executor, so an autocommit pool
with a REPEATABLE READ default cannot masquerade as one transaction. Unknown
executor wrappers fail closed; trusted code must preserve the provided port.

The policy module proves scope-only field actions for exact concrete Native and
Defined engines. API composition supplies the same live engine used by its guard
to the optimized query port; unknown wrappers and the default visible-query port
use header preflight. An inherited marker cannot claim the optimization. Native
and tenant-defined policy workload/page budgets remain unchanged.

A filter, sort, sum or source relationship key must be readable over the entire
entity-visible set. Accept ALL (including OR containing ALL), or exactly the
same normalized scope as the entity boundary; conservatively reject other
scopes before value validation/execution. Joined projections are masked without
removing the visible related record. Count and ID pagination depend on entity
visibility, not projection visibility. Policy scope membership is an intentional
permission disclosure; this does not promise timing/physical side-channel
noninterference or hide the existence of an authorized record.

Add explicit `operation: patch`. Existing `update` remains full replacement;
create/delete remain strict whole-record operations. Patch requires entity/row
read+write and read+write for touched fields only, rejects core/unknown fields,
validates touched values only, and merges JSONB inside PostgreSQL under record
CAS. Omitted fields and their reference projections remain untouched; explicit
null clears an optional field. Reject inaccessible touched fields before value
validation, using the trusted existing owner after loading the row header.
Responses use the same SQL masking port. Receipts, audit, outbox,
reference/index updates and workflow triggers retain one transaction. Patch
emits `record.updated`; receipt replay returns headers only.

For patch, reject CONDITION scopes on entity or touched fields: a proposed
change must not expose a hidden condition through success/failure. OWNER, TEAM,
SHARE and their combinations remain supported. Until a public field-size
reservation contract exists, compute patch admission from already-readable
values plus a conservative public schema reservation for hidden fields
(including JSON escaping and keys).
The total must fit 64 KiB. Never inspect actual hidden lengths. Fully readable
wide schemas retain normal patch editing; schemas with many hidden long fields
may need strict replacement with full access or a later public per-field size
contract. Do not introduce a hidden length oracle or increase the storage budget.
Writable hints reflect these bounds and are advisory; every command rechecks
live policy.

The UI displays Restricted distinctly from an empty value, disables masked or
non-writable fields and submits only changed, writable fields with patch. A
malicious client cannot bypass server checks. Legacy responses without field
access retain their existing renderer compatibility.

## Migration and recovery

Forward migration 00046 only extends the immutable command receipt operation
constraint. No record rewrite, DDL from metadata, IR change, recompilation,
customer data deletion, new worker grant or role credential is needed. Roll
forward on failure. Old binaries cannot replay new patch commands; retain the
new reader when rolling application behavior back. Immutable old receipts and
release/workflow bytes are preserved.

## Verification

Use populated paired hidden-value fixtures, live role revocation, owner/team/
share/condition projections, joins, cursor/count/filter/sort/sum negatives,
partial validation/clear/CAS/reference preservation, audit/outbox/receipt
atomicity and browser/form regressions. Preserve existing strict rule/workflow
and worker negative tests. Independent adversarial review and full check/test/
build/browser runs are required before completion. This remains preview
foundation remediation; no production activation or M10 is authorized.
