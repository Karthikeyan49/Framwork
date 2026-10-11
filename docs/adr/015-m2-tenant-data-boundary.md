# ADR-015: Verified transaction scope and migration-owned tenant RLS

- Status: accepted
- Date: 2026-10-11
- Scope: M2 / PostgreSQL infrastructure, identity, tenant, audit, API composition

## Context

Roadmap M2 requires least-privilege application access, transaction-local tenant
context, RLS, and tenant isolation tests. Architecture 14/15 and risk 26 require
trusted authentication, live membership, policy enforcement and a database
backstop. M3 metadata tables and M5 query AST/CRUD remain separate milestones.
M1 introduced IAM RLS but allowed membership visibility based on a tenant setting
alone. A reusable boundary must also handle pooled session pollution, cancellation,
failed tenant switches, atomic audit, and migration drift.

## Decision

`postgres.WithRuntime` leases a single connection and validates that connection's
role before each bounded repeatable-read transaction. It rejects privileged roles,
operator/file/server-program/all-data role membership, replication privileges,
schema/table/function ownership and non-inherited owner memberships, unsafe grants and absent forced RLS
on protected tables. It clears persistent authentication/actor/tenant settings,
uses transaction-local context, a catalog-only search path, a 2-second statement
limit, 500-ms lock limit and 3-second transaction/context limit. Cleanup resets
identity settings and role with a separate 1-second deadline; failed cleanup
causes the connection to be discarded. Rollback runs on error, panic or cancellation.
Repositories receive `postgres.Executor`, exposing query operations without
transaction lifecycle methods. Only trusted Go repository code can use SQL;
transaction-control/role-changing SQL is forbidden in repository callbacks.

Identity rebind invalidates previous actor/tenant scope. Tenant rebind clears the
previous tenant before validation, then resolves active server-side membership.
RLS checks require the current active actor to match the verified issuer/subject
and the selected active tenant/membership. Unknown/malformed scope yields no rows.
Authorization still runs in Go; RLS does not replace entity/row/field/action policy.
Protected API replies are buffered until the same transaction's audit and commit
succeed. There are no automatic retries; commit failure has an explicitly unconfirmed
outcome and must not cause a blind write retry.

Migration 00004 adds two bounded, read-only `SECURITY DEFINER` IAM lookup functions
with fixed `pg_catalog` search paths, fully qualified objects and revoked PUBLIC
execution. They expose only the authenticated actor ID or a membership boolean.
Their migration-role owner uses M1's explicit operator read policies, not
superuser/BYPASSRLS. Identity owns actor lookup; tenant owns membership lookup.
This avoids recursive membership/tenant RLS while preserving module-owned tables.
A SECURITY INVOKER tenant-ID parser returns NULL for missing or malformed UUIDs.

The migration-only `runtime.install_tenant_rls(regclass)` helper is SECURITY INVOKER,
owner-only, and unavailable to the application/PUBLIC. It installs both ENABLE and
FORCE RLS, symmetric USING/WITH CHECK scope checks, and only CRUD grants on a new
migration-owned runtime/control table. It requires nonnull UUID tenant scope, a
primary key and tenant-inclusive unique keys; INCLUDE columns do not count.
Tenant foreign keys must pair source/destination tenant columns. Global exclusion
constraints, existing policies and direct partition-child installation are rejected.
It quotes identifiers and accepts a relation identity, not arbitrary SQL text.

No production business table is introduced. Disposable integration fixtures use
typed core fields and JSONB to exercise the actual installer with the normal role.
Future tenant-table migrations must install security within their DDL transaction
and use tenant-first indexes and same-tenant relationships. Non-tenant control
records need a separate reviewed access design; do not apply this helper blindly.

## Consequences

No new dependency is needed. Small interfaces preserve current module boundaries.
The per-transaction catalog safety check adds latency, accepted for initial safety;
measure before any cached alternative that could miss privilege drift. Fixed query
limits are an initial baseline and may need reviewed workload-specific settings.

PostgreSQL custom settings are not cryptographic credentials. Possession of the
application database password and arbitrary SQL access could forge issuer/subject
settings. Protect credentials/network access, parameterize all future queries and
never expose Executor to AI/metadata. DDL administrators remain trusted and can
alter policies/functions. Function ownership, grants and schema drift need production
monitoring and reviewed changes. Repeatable read gives a consistent membership
snapshot for at most the bounded transaction; revocation applies to new transactions.

Background jobs must verify their trusted delegated actor/tenant and policy in a new
transaction. There is no ambient worker tenant, operator fallback or job queue in M2.

## Verification and recovery

Real PostgreSQL tests cover two-tenant CRUD, unrestricted scans/export-equivalent
rows, joins/aggregates, scoped keys/FKs, missing/forged/inactive context, failed and
authorized switches, identity rebind, rollback, mandatory-audit failure, panic,
parent cancellation, retained executor lifetime and concurrent pooled access.
A disposable database tests fresh migration/reapplication, disabled/non-forced
RLS, unsafe TRUNCATE grants, fail-closed readiness and recovery. Privileged migration
credentials and a `row_security=off` lease cannot enter runtime callbacks. A private
non-inherited owner-role regression reproduces SET ROLE table/RLS bypass in a
disposable database, then verifies table/schema/function owner memberships cannot
enter runtime. The real framework_app memberships are never altered by this test.

Deploy migration 00004 before the M2 binary and verify readiness/integration checks.
Goose applies it atomically. Repair an unsuccessful migration and rerun; an applied
security migration requires a reviewed forward fix. Down intentionally refuses to
restore weaker IAM policies. To revert an application binary, retain the stronger
compatible policies; do not discard audit history or pretend destructive DDL is reversible.
