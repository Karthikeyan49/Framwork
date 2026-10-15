# M9R R5c — Governed record access and team administration

Date: 15 October 2026 (UTC). Source: `11e8097` and review fixes `8f8067a` on
`claude/dazzling-goldberg-69wbkn` (based on `remediation/m9r-foundation` at
`da42d57`). Full R5/M9R remain PARTIAL. No M10, main merge or production rollout.

## What changed

Before this slice, OWNER/TEAM/SHARE DataScopes were enforced but ownership and
explicit shares could only be changed by operator SQL. Migration 41 adds one
SECURITY DEFINER port, `runtime.administer_record_access`, executable only by
`framework_app`; migration 42 (preserving applied 41) applies independent
review fixes. [ADR034](../adr/034-m9r-record-access-administration.md) records
the design.

- Operations: `owner` (user plus optional team), `share` (user plus write flag),
  `unshare`. Typed identifiers only; no SQL, values or policy output accepted.
- Record `row_version` is the per-record conflict witness; each accepted change
  locks the exact version and advances it, so stale business edits conflict.
- Canonical datasets change `record_identity`/`identity_shares`; other datasets
  change anchor rows/`record_shares`, mirrored into any shadow identity. The
  existing fence trigger re-validates OPEN binding and current generation.
- New capabilities `data.row.assign` and `data.row.share` (bootstrap owner/admin;
  delegable to custom roles; never to a role named `viewer`). Go also requires
  entity read/write, live read+write DataScope on the exact record and row
  read/write before calling the port. The port additionally requires an active
  non-viewer actor, a trusted tenant scope, active recipient membership for
  grants, a tenant team, ≤64 shares for new grants and a non-no-op transfer.
- Routes: `GET|POST .../entities/{entity}/records/{record}/access` under
  revision and dataset-binding preview prefixes. Every rejection rolls back and
  is audited freshly; success records `records.access.changed`. No outbox event
  or workflow trigger (access is not a business data change). No automatic
  retry: after an unknown commit, reload the access view and compare version.
- Listing is bounded at 256 recipients with `shares_truncated`.

Changed files: migrations 41/42, `internal/runtime/data/access.go`,
`internal/platform/access/records.go`, route/action wiring in
`access.go`/`preview.go`/`metadata.go`, policy catalog/native permits
(`policy.Version` → `m9r-native-v5`), audit state fields, data readiness check,
policy unit test, integration test `m9r_record_access_test.go`, ADR034.
No new dependency.

## Commands executed and results

| Command | Result |
|---|---|
| Baseline `make check`, `make test` at `da42d57` | PASS; integration 488.290s |
| `-run '^TestM9RRecordAccess'` at 11e8097 | first run FAIL (plpgsql `RETURN next` parsed as `RETURN NEXT`), fixed before commit; then PASS |
| Independent review of 11e8097 (targeted suites + 4 probes incl. 8-way race) | PASS, 0 HIGH/MEDIUM, 4 LOW |
| Root `-run '^TestM9RRecordAccess'` after fixes | first FAIL: unshare of departed user rejected (real defect, fixed in 42); then PASS |
| `make check test build` at 8f8067a | PASS; integration 485.973s |
| `make test-e2e` at 8f8067a | PASS, 5 tests |
| Independent re-verification at 8f8067a (5 probes incl. custom steward role, inactive recipient, 65/257 shares; targeted suites 96.1s) | PASS; L1–L4 closed, one documented LOW (L5) |

## Threat model and limits

Trusted API runtime role with live identity/tenant context; DataScope and
capability decisions remain the Go caller's, with port defense in depth. Worker
credentials cannot execute the port. Not covered: team creation/membership
administration (R5c-b), field masking/partial writes (R5d), configurable
approval roles (R5e), UI for access administration, capacity beyond the
64/256 share budgets.

## Recovery

Migrations 41/42 add/replace one function and grant; no table, record or
history is rewritten. Revoke EXECUTE from `framework_app` to stop
administration; existing owner/share facts remain. Forward-fix only; never
restore broad DML grants on access tables.

Accepted LOW L5: `unshare` of a user without a share is a 409 (documented in ADR034); a later slice may map it to 404.

R5c-a STATUS: PASS (bounded record access administration and documented verification scope)

R5 / M9R STATUS: PARTIAL

## R5c-b — Team hierarchy and membership administration

Source: `856a4f1` and review fixes `6b50eb7`. [ADR035](../adr/035-m9r-team-administration.md).
Migration 43 adds `iam.teams.version` and SECURITY DEFINER ports
`control.save_team`, `control.set_team_member`, `control.team_members`; migration
44 (preserving applied 43) bounds retained memberships per team at 1000. Writes
take the tenant row fence and re-validate the whole hierarchy (root-reachable,
depth ≤8, ≤64 resolved teams per member, ≤256 teams). New capabilities
`teams.read`/`teams.manage` (bootstrap owner/admin, delegable, never `viewer`);
native policy version `m9r-native-v6`. Routes `GET|POST /v1/tenants/{tenant}/teams`,
`GET .../teams/{team}`, `POST .../teams/{team}/members/{user}`.

| Command | Result |
|---|---|
| `-run '^TestM9RTeam|^TestM9RRecordAccess'` at 856a4f1 | PASS |
| `make check test build` at 856a4f1 | PASS; integration 481.898s |
| Independent review of 856a4f1 | 0 HIGH; MEDIUM F1 (suffix routing captured `/roles/teams` and other `.../teams` routes), MEDIUM F2 (unbounded retained memberships → listing 503), LOW F3 (policy version), LOW F4 (coverage/audit field) — reproduced |
| `make check test build` at 6b50eb7 | PASS; integration 483.543s |
| `make test-e2e` at 6b50eb7 | PASS, 5 tests |
| Independent re-verification at 6b50eb7 (route sweep, 1000/1001 budget boundary, port contract, targeted 22 tests 49.5s) | PASS; F1–F4 closed, no new findings |

Limits: teams are not deletable; administration UI is not implemented; a tenant
already over budget via operator SQL rejects team writes until repaired.
Membership changes apply to subsequent statements, not committed effects.

R5c-b STATUS: PASS (bounded team administration and documented verification scope)

R5 / M9R STATUS: PARTIAL
