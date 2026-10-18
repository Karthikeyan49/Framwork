# M13 Approval and Promotion (control plane) — implementation report

Date: 16–17 October 2026 (UTC). Source: `claude/m13-approval-promotion` (based on the M12 branch
`claude/m12-candidate-builder`). Decisions, amendments and recovery are in
[ADR039](../adr/039-m13-approval-and-promotion.md). **M13 is PARTIAL against the roadmap line
"Approval + Promotion":** the approval, rejection, activation and rollback *authority* is built and
tested; there is **no production data plane** (no production datasets, no execution of storage
migrations, no release-pinned production routes — ADR039 §10). An activated release is an auditable
pointer and nothing serves it. This is a development/test preview capability, not production ready.

## What changed

Before M13, approval and activation were Go checks over tables the runtime role could write
directly: `framework_app` could INSERT approvals and INSERT/UPDATE the active-release pointer, and
the only database rules were two RLS policies. A buggy or compromised API runtime could approve
anything and point the pointer at any approved hash, stale or never activated.

- **Migration 49** (additive, forward-only): `framework_app` loses INSERT/UPDATE on
  `control.release_approvals` and `control.active_releases` (table and column level). New
  append-only `control.release_rejections` and `control.release_history`. SECURITY DEFINER ports
  `approve_release`, `reject_release`, `activate_release`, `rollback_release` (owner
  `framework_schema_owner`, `search_path=pg_catalog,pg_temp`, EXECUTE only for `framework_app`):
  database-floor separation of duties by risk level (HIGH excludes requester and acceptance author,
  CRITICAL also the sandbox freezer and needs two distinct approvers), approvers re-checked as
  active non-viewer members at activation, rejection permanence, compare-and-set activation
  with a row lock, rollback only to a previously activated and not rejected release with a
  class gate on the current release's `Risk.Rollback`. Guards: the pointer moves one generation
  at a time and only with its history row (deferred constraint trigger), history is a contiguous
  chain, approval/rejection/history rows can be written only from inside a port, and
  `control.worker_grants_safe()` now also trips on REFERENCES/TRIGGER grants and lists the release
  tables.
- **Go**: policy actions `releases.read|approve|activate|rollback` (policy `m13-native-v10`),
  audit fields, the promotion store (`ApproveRelease`, `RejectRelease`, `ActivateRelease`,
  `RollbackRelease`, reads, typed errors) and pure helpers (quorum floors, status derivation,
  rollback-plan rule). The M6R direct-write `Store.Approve/Activate/Rollback` were removed.
- **API** (preview-gated, exact patterns, strict bodies): `POST …/candidates/{c}/approve|reject|activate`,
  `POST …/releases/rollback`, `GET …/releases`, `GET …/releases/active`; candidate views gained
  status, approvals, quorum and rejections. Every decision loads the candidate through
  `Store.LoadCandidate` (Restore plus verification against the candidate's own base release), evaluates
  staleness in the same transaction and never compiles source ("no recompile after approval", enforced by
  a unit test).
- **Findings fixed on the way** (see the M12 report): the Restore/diff forgery (M12-F1), gateway
  catalog findings F1–F4, case-folded duplicate JSON keys.

## Commands executed and results

| Command | Result |
|---|---|
| `TestM13Ports*` (6 tests: authority, separation/quorum/rejection, CAS, rollback gate, append-only/RLS/hygiene incl. worker-grant guard, closing the M6R write gap; mutation checks caught 3/3 weakenings) | PASS |
| `TestM13API*` (8 tests), `TestM13Adversarial*` (compromised API forged candidate, identity impersonation residual, rogue-approver matrix, quorum with demoted approver, legacy write paths, 5 race scenarios, staleness, rejection permanence, rollback abuse, cross-tenant/app, replay, end-to-end) and `TestM12*` (5) on a freshly reset isolated database | PASS, 37 tests, 404 s (`-race`) |
| `make check` (docs checksums, format, vet, mod verify, web lint, typecheck) on the integrated tree | PASS |
| Go unit tests `-race` (`./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...`), `node --test scripts/*.test.mjs`, web `npm test` | PASS (5 / 29 tests) |
| Full `go test -race -tags=integration ./tests/integration` on the integrated tree (main stack, migrations 1–49), run while other agents loaded the 4-core host | 1 failure of the whole suite: `TestM10CrashAfterSendLookupFindsEffectWithoutResend` returned a 503 (3 s transaction envelope exceeded under load); it passed 2/2 when rerun alone (23 s) |
| Independent adversarial review of M13 | NOT RUN (the adversarial suite above was written by the builder agent against its own contract) |

## Threat model and limits

- **Identity is bound by the API process** (session settings after token verification). A fully
  compromised API process that knows another member's issuer and subject can bind it and approve and
  activate as that member (executed: `TestM13AdversarialCompromisedAPIBindsAnotherMembersIdentity`).
  Credential-bound approvals are a later slice.
- **Forged candidate bodies**: the database cannot recompute the verifier, diff or policy hash; the raw
  ports accept a self-consistent forged body; `LoadCandidate` refuses it before any API decision.
- **Availability envelope**: restore plus verification plus staleness runs inside the 3 s runtime
  transaction; with `-race` on a loaded host several parallel approvals of one candidate time out
  with 503 and write nothing.
- **Activation of a non-EQUAL storage class** is allowed (HIGH/CRITICAL approval applies) but nothing
  migrates data; a production data plane must refuse to serve a release whose storage is not provisioned.
- A HIGH candidate needs three distinct people, CRITICAL two more approvers; tenants must add members.
- Tool-catalog staleness at approve/activate is covered by unit tests only (the integration order app has no tool pins).
- Not built (ADR039 §10): production data plane, approval expiry, configurable approver roles (R5e),
  volume measurement, a separate verifier credential, UI, release lifecycle events.
