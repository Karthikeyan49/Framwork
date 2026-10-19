# ADR-020: V3 migration and M6R contracts

Status: Accepted for M6R development remediation
Date: 2026-10-12

## Context

V3 supersedes V2 where they disagree. M0–M6 already have working bootstrap,
identity, tenant resolution, RLS, immutable draft source, canonical compiler IR,
JSONB preview data and a semantic React renderer. The audit identified missing SQL
DataScope, separate workers, immutable executable release storage and exact approval
contracts. These findings justify targeted changes, not a project restart.

## Decision

Keep the Go modular monolith, React/TypeScript, PostgreSQL, Redis and telemetry.
Keep the original V2 documents and migrations 1–8 unchanged. Preserve the original
source/IR 1.0.0 representation and logical `hybrid-preview-v1` field layout: a
storage adapter change does not change the meaning of existing compiled fields.

Use a generic `runtime.records` store with tenant/app/source-revision/IR/entity/record
keys, JSONB custom fields and typed ownership, team, timestamps and versions. Keep
`runtime.preview_records` as a read-only compatibility adapter. Migrations 9–16 are
additive, compare records, retain source data and seal both table and column write
ACLs. The final barrier waits for legacy transactions and reconciles late committed
rows; divergence aborts without overwriting either copy. Stop legacy writers for
operational cutover. Typed Domain Packs remain a distinct storage classification;
M6R does not implement Domain Packs or per-tenant DDL.

Retain the established `framework_app` and `framework_migrator` login names as the
API/migrator aliases instead of renaming retained credentials. Introduce a NOLOGIN
schema owner, dedicated worker and dispatcher logins, and NOLOGIN audit-writer and
maintenance groups. Maintenance and standalone audit execution are intentionally
unavailable. Runtime guards reject privileged ownership, role flags and dangerous
memberships. Worker transactions bind private dispatcher tickets to a live actor,
tenant, job and release hash; RLS checks that job while pool cleanup clears context.
There is no scheduler, lease protocol or workflow executor yet (M9).

`policy.ScopeEngine` returns a bounded typed DataScope. Compile it into SQL before
projection, user filters, ordering, joins, aggregate input or mutation. Intersect
entity and used field scopes; commands check original and proposed states. Check
row-read action permission once for each queried collection, with DataScope owning
record predicates. Do not translate arbitrary Go per-record callbacks by fetching
all rows. Adapters must implement ScopeEngine or access fails closed. Native keeps
explicit existing tenant-wide role permissions; narrower ownership/team/share/
condition policies are trusted adapters, never request-supplied scopes. Teams come
from live memberships; depth >8, cycles or >64 teams fail explicitly. Scope decisions
are cached within one transaction and audited by hash without logging values.

Store immutable canonical source/IR/evidence bundles as exact bytes and content
hashes. Build and restore share admission checks, including source normalization,
derived runtime field consistency, independent acceptance, verification and sandbox
bindings. Restore never calls Compile; historical compiler provenance remains intact.
Approval binds exact bytes, high-risk requester/approver separation and trusted gate
authorization. Activation compares base/dependency snapshots and CAS generation.
Rollback switches only an approved pointer after recovery assessment. An execution
pin retains its release through rollback. `make release-check` is the operator-only
pre-deployment supported-IR inventory gate; a normal RLS connection cannot falsely
report that the global inventory is empty. There are no public production promotion
endpoints. Sandbox execution is M11; complete candidate orchestration is M12 and
human approval/promotion services are M13.

Introduce bounded V1 monetary approval-rule Intent Contract and independently
human-authored acceptance specifications. Mandatory ambiguity, malformed comparisons,
unknown currency, missing coverage or mismatched entity/monetary field blocks bundle
admission. These are contracts, not execution of business acceptance tests. A future
trusted verifier must execute tests and supply report provenance; AI cannot supply
a PASS assertion to a production route.

ApplicationPatch uses typed definitions, base release/IR tracking and exact definition
hashes. Three-way automatic merge is restricted to nonconflicting presentation pages.
Sensitive kinds/class changes require explicit resolution; final documents are schema
validated and still require compilation and approval.

Tool delivery declares capability-specific retry/reconciliation behavior. Unknown
remote outcomes remain unknown; reconciliation-first and dedupe windows are enforced.
These contracts do not execute external tools or promise universal exactly-once.

The API composition root appends business audit and an outbox intent on the same M2
transaction as the record mutation. Either failure aborts the transaction. No network
publish occurs. M8 implements draining, inbox dedupe and delivery orchestration.

Money, reviewed currency minor units, immutable FX snapshots, IANA zones, calendars
and draft/final document numbering are reusable value/port contracts. Posting,
accounting, inventory and gapless number allocation implementations are M16 work.

## Consequences and verification

Preserve source revisions, compatible UI and baseline toolchains. Production data
routes remain unavailable. Existing previews retain their original compiled identity;
unsupported empty/wide/deep/oversized UI definitions now fail at compilation. UI reads
compiler provenance independently of its supported IR/component version.

Use the M6R integration, independent adversarial, late-writer cutover, approval,
rollback and outbox failure tests, then all original checks, browser tests and smoke.
Retain checkpoint schemas/fingerprints privately; never commit secrets or test data.
The implementation report records actual results, remaining later milestones and
production deployment/recovery procedures.
