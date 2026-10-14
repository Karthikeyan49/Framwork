# ADR035: Governed team hierarchy and membership administration

Status: R5c-b implemented on `remediation/m9r-foundation` lineage; see the
[R5c report](../migration/M9R_R5C_IMPLEMENTATION_REPORT.md). Full R5/M9R PARTIAL.

## Context

TEAM DataScope resolves a user's active memberships plus descendants, failing
closed beyond depth 8, 64 teams or a cycle (`tenant.Store.Teams`, migration 39).
Teams and memberships could only be created by operator SQL; the runtime role
has SELECT only, and memberships are visible only to their own user.

## Decision

Three SECURITY DEFINER ports owned by the NOLOGIN schema owner and executable
only by `framework_app`: `control.save_team` (create with a caller-chosen UUID
at expected version 0, or rename/reparent at the exact `version`, added to
`iam.teams`), `control.set_team_member` (activate/deactivate with the observed
active state, NULL for no row, as witness) and `control.team_members` (list one
team). Each requires the trusted tenant scope; writes also require an active
non-viewer actor, while the trusted Go caller decides the new `teams.read` /
`teams.manage` capabilities (bootstrap owner/admin; delegable; never to a role
named `viewer`).

Writes take the tenant row fence used by role administration, so concurrent
hierarchy edits abort under REPEATABLE READ instead of jointly forming a cycle.
After each write `control.team_hierarchy_valid` re-checks the whole tenant:
every team reachable from a root within depth 8 and no member resolving more
than 64 teams. Budget: 256 teams; names 1–120 characters without control
characters or surrounding whitespace. Activation requires a current tenant
member; deactivation works for departed users. No-op changes are rejected.
Teams are not deleted (records, identities and history reference them).

Routes: `GET|POST /v1/tenants/{tenant}/teams`, `GET .../teams/{team}`,
`POST .../teams/{team}/members/{user}`. Rejections roll back and are audited
freshly; successes record `teams.changed` / `teams.membership.changed`. No
automatic retry: after an unknown commit reload the team and compare.

## Consequences

Administrators can operate TEAM scopes without operator SQL. Membership or
hierarchy changes take effect at the next authorized statement; committed
effects are not revoked retroactively. A tenant already over budget through
operator SQL rejects all team writes until an operator repairs it.

## Verification and recovery

Integration tests cover create/rename/reparent CAS, cycle and depth rejection,
membership witness/member checks, TEAM scope following membership and hierarchy,
viewer/cross-tenant/contextless/worker denials and a concurrent cyclic reparent
race. Migration 43 is additive (a defaulted column and functions) and
forward-only. Recover by revoking EXECUTE from `framework_app`; existing teams
remain. Never grant runtime DML on `iam.teams`/`iam.team_memberships`.
