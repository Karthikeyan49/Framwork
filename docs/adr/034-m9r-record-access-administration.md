# ADR034: Governed record owner/team/share administration

Status: R5c-a implemented on `remediation/m9r-foundation`; see the
[R5c report](../migration/M9R_R5C_IMPLEMENTATION_REPORT.md) for executed scope.
Full R5/M9R remain PARTIAL.

## Context

OWNER, TEAM and SHARE DataScopes (ADR020/032/033) were enforced at the API and
worker SQL boundaries, but record ownership and explicit shares could only be
changed by operator SQL. The runtime role has no UPDATE on owner columns and no
DML on `runtime.record_shares`, `runtime.record_identity` or
`runtime.identity_shares`. Canonical datasets (ADR031) hold current owner/share
facts in logical identity tables while legacy and pre-cutover datasets hold them
on the anchor rows, so a naive column grant would silently miss one model.

## Decision

Add one SECURITY DEFINER port, `runtime.administer_record_access`, executable
only by `framework_app` and owned by the NOLOGIN schema owner. It performs one of
`owner` (user plus optional team), `share` (user plus read/write flag) or
`unshare`. It accepts typed identifiers only; it never accepts SQL, values or
policy output. It rejects calls without the session's trusted tenant scope,
non-current recipients (inactive user/membership/tenant), teams outside the
tenant, absent shares, a changed record version, more than 64 shares and a
no-op ownership transfer. Recipient/team/no-op rejections are invalid requests
(400), not retryable conflicts; only version, absent-share and fence failures
are 409. As defense in depth (migration 42, after independent review) the port
also requires the authenticated actor to be an active non-viewer member; the
full capability/DataScope decision remains the trusted Go caller's.

The record `row_version` is the conflict witness. Every accepted change locks the
exact row at the expected version and advances it. Business edits from the
earlier version therefore conflict, and concurrent access/business changes
abort under REPEATABLE READ. The existing dataset fence trigger re-validates the
active OPEN binding and current generation for every change. Canonical datasets
change `record_identity`/`identity_shares` (current across retained
generations); other datasets change anchor rows and shares, which the existing
mirror keeps consistent with any shadow identity.

Go authorization precedes the port in the same transaction: the actor needs
`data.entity.read/write`, live read and write DataScope over the exact record,
`data.row.read/write`, and the new administrative row capability:
`data.row.assign` for ownership and `data.row.share` for shares (also required
to list share recipients; a delegate holding only `data.row.assign` reads the
current owner through the ordinary record query). Listing returns at most 256
recipients (the evolution copy budget) with `shares_truncated` so excess legacy
shares stay revocable. The owner team only has to belong to the tenant; it need
not contain the new owner, matching TEAM scope semantics. Bootstrap `owner`/`admin` receive both; custom roles
may delegate them; the `viewer` role name cannot be granted them. Granting a
write share requires the granter's own live write scope on the record; the
recipient still only benefits through their own role's SHARE scope.

Routes: `GET|POST .../entities/{entity}/records/{record}/access` under both the
revision and dataset-binding preview prefixes. Every rejection rolls back the
whole transaction and is audited in a fresh live-identity transaction. Accepted
changes record `records.access.changed` evidence with operation, target user,
team and resulting version, plus the batched decision/scope evidence. Access
changes are not business data events, so no outbox `record.*` event is emitted
and workflows are not triggered; consumers observe a version gap only.

Alternatives rejected: column grants to the runtime role (bypass canonical
identity and recipient validation); a separate access version (business edits
would not detect concurrent ownership change); automatic retry (unknown commit
must be reconciled by reloading the access view and comparing its version).

## Consequences

Tenant administrators can operate ownership/shares without operator SQL. Team
creation/membership administration, field masking/partial writes and custom
approval roles remain separate R5 slices. An actor who transfers ownership may
lose OWNER-scoped access immediately; the response reports only the committed
witness. Shares to members whose role lacks SHARE scope grant nothing.

## Verification and recovery

Integration tests cover legacy and canonical datasets, retired-generation
rejection, recipient/team/budget/version rejections with zero effects, stale
business writes, bootstrap viewer/cross-tenant denial, contextless app and
worker port calls, and audit evidence. Migration 41 only adds a function and
grant; it is forward-only. Recover by revoking EXECUTE from `framework_app`
(administration stops; existing owner/share facts remain) and forward-fixing.
Never restore broad DML grants on access tables.
