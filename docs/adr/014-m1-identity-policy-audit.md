# ADR-014: M1 identity, trusted tenant access, native policy and audit

- Status: accepted
- Date: 2026-10-11
- Scope: M1 — Tenant / Identity / Policy

## Context

The roadmap requires trusted tenant context, users, memberships, roles, central
policy and audit. The buildkit leaves the first identity provider and policy
implementation open. The user selected provider-independent verification with a
local development issuer. Business data access remains M2.

## Decision

Authenticate API bearer access tokens with standard-library RSA verification and
an operator-controlled **public** JWKS, pinned issuer and audience. Accept only
RS256, `typ=at+jwt`, an explicit known key ID, RSA modulus 2048–8192 bits/exponent
65537, required subject/issued-at/expiration and optional not-before. Maximum token
lifetime is one hour; local tokens last 15 minutes. Reject expired/future tokens,
unknown critical extensions, private key components, unsupported algorithms,
missing/partial config and oversized tokens/key sets. Do not follow token key URLs
or trust token tenant/role claims. No identity-provider network dependency is
introduced. Production requires explicit HTTPS issuer/audience/public-key binding;
it refuses default local identity configuration. Up to 16 pinned keys allow staged
rotation by file update and process restart. Clocks must be synchronized.

Identity owns users keyed by `(issuer,subject)`; tenant owns tenants, memberships
and the fixed owner/admin/member/viewer role catalog. An active database user and
active membership in an active tenant are required. Principal, User and Access
hold private fields so consumers cannot construct trusted claims from browser
input. Per-request transactions bind only verified identity claims and resolved
actor/tenant IDs using transaction-local `set_config`. A path tenant UUID is a
selection request; membership verification makes it trusted. No tenant data or
roles are cached in Redis or taken from the JWT. In-flight reads may complete under
the transaction's prior authorization snapshot; subsequent requests see revocation.

Use the public `policy.Engine` port with a native Go RBAC + ABAC implementation.
Actions are allowlisted: self-owned identity session discovery, tenant access,
owner/admin membership inspection, and
self-owned profile-read decisions. All other entities/fields/actions/workflows/
tools/promotions/service scopes deny until their milestones explicitly implement
them. Policy returns reason, obligations, random decision ID and fixed policy
version. The HTTP enforcement point supports audit; an unknown obligation denies.
ABAC checks matching trusted User/Access actor IDs, tenant equality, resource type
and ownership. Identity session discovery explicitly checks the authenticated
self resource without requiring an existing tenant; it also goes through central
policy, obligation enforcement and correlated audit. OPA remains an
optional future adapter; no OPA service or new dependency is necessary for M1.

Protected endpoints compose module public ports in `platform/access`. Modules own
their SQL; composition never queries identity/tenant/audit tables directly.
`sql.Tx` is the small explicit transaction boundary in PostgreSQL adapters. Reads,
policy and audit share a repeatable-read transaction; audit must commit before
any protected payload is returned. Missing policy, unsafe database credentials,
unknown obligations and audit failures fail closed. Invalid/unregistered/disabled
identities get bounded anonymous denial evidence, never token content. Spans carry
trusted actor/tenant/decision/policy IDs; audit correlates request and trace IDs.

Provision identities and memberships with a separate, explicit `cmd/iam` operator
command using migration credentials. Require role, user/membership status and an
operator reference; never automatically create privileged accounts during startup.
User/membership state and bounded typed audit state commit together. Serialize
membership changes on the tenant row; bootstrap requires an active owner and the
last active owner cannot be demoted/revoked. Disable a user only after transferring
all active ownership. Retries upsert state; audit records each committed command.
Operator reference is a declared attribution/ticket, not cryptographic proof of
human identity. Database `session_user` records the actual writer; the target user
is the audit resource, never misrepresented as the performing actor.

Migration 00002 adds IAM tables and audit evidence. New tenant-scoped IAM records
need a **narrow RLS backstop now**, so enable/force actor/tenant-scoped IAM policies;
this security prerequisite does not implement M2's business-data repository/RLS
framework, CRUD, joins or query runtime. Dedicated operator policies permit
provisioning with the migration role. Runtime IAM privileges are SELECT only,
audit INSERT only. Migration 00003 adds bounded state/provenance additively,
preserving already-applied migration00002 and historical evidence. Column-level
app INSERT prevents overriding audit timestamps/writer identity. An immutable
trigger rejects UPDATE/DELETE/TRUNCATE, including ordinary owner DML. Privileged
DDL administrators can still alter storage; external immutable archival is future
work. Runtime checks reject superuser/BYPASSRLS, DDL roles, schema/table owners,
and IAM writers; protected routes also check this, independently of readiness.
Readiness composes module-owned identity/tenant/audit schema contracts; audit
checks its insert-column grants, state/writer provenance and forced RLS using
metadata only, without reading evidence or writing synthetic audit events.
A missing audit upgrade must return not-ready even if IAM tables already exist.

The frontend provides a token-based access console, server-provided workspace
selection and verified-access display. Tokens live only in component memory,
never URL/storage/logs, and are cleared on disconnect/reload or an observed 401.
This is an API-access console; provider authorization-code/PKCE redirect login,
MFA, refresh/session cookies and account recovery are separate future decisions.
No browser role display grants server permissions. Development-only Node tooling
generates/preserves a 0600 signing key plus public set under ignored `.local`,
refuses production/external-issuer minting, and never automatically grants a role.

## Consequences

No new Go/npm dependencies, microservices or ERP modules. Small explicit ports
keep authentication, membership, decisions and audit separate. Root module store
ports expose PostgreSQL-backed behavior without private cross-module imports.
IAM and audit are durable; metadata/compiler/runtime workflows remain placeholders.
The current database-role names follow M0 local provisioning; deployments must
provision those distinct role contracts or supply a reviewed follow-up migration.
Tenant/member lists are bounded to 100 records without pagination in M1.

## Verification and recovery

`make check`, `make test`, `make build`, migration status and live smoke validate
this milestone. Real PG tests use the normal role, attack cross-tenant paths,
headers/bodies/JWT claims, revoked roles/memberships, disabled users, missing
context, pooled connection reuse, audit failure and migration-role misbinding.
Parallel owner removals retain one owner. Real operator/local-issuer commands and
fresh-database migration application/repeatability are exercised.

Preserve `.env` and local keys against retained data. Restore both local key files
if either is missing; never silently regenerate just one or rotate credentials.
Rotate pinned provider keys by accepting old/new public keys, restarting API,
then removing retired keys after outstanding token lifetime. Correct membership
mistakes with a reviewed explicit operator command and new evidence; never edit
past audit. A failed audit append rolls back provisioning. A COMMIT transport
error has an uncertain outcome: inspect membership and audit state before retrying.
Migrations are additive/transactional; forward-fix failures and never delete a
volume or historical evidence as repair. M2 requires a separate user task.
