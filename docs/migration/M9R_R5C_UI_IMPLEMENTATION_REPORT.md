# R5c UI — Governed record and team administration

Date: 15 October 2026 IST. Status: implementation checkpoint; independent
adversarial review and final repository-wide verification pending. Branch
`remediation/m9r-foundation`; clean starting checkpoint `aa1fa2a`.
Full R5/M9R remain PARTIAL. No M10, main merge or production activation.

## Implemented scope

[ADR038](../adr/038-m9r-administration-ui.md) was written before implementation.
The UI consumes ADR034/035's existing governed APIs without new backend grants,
migrations, dependencies or immutable formats. Independent review also exposed
an existing R5d query-construction bug for scoped delegates; the narrow correction
preserves masking, field/row authorization and strict execution-input behavior.

- A verified workspace has a team administration panel: bounded hierarchy,
  create/rename/reparent, team versions, retained active/inactive direct
  memberships and true/false/null membership witnesses from fresh reads.
- Selected records have separate ownership and sharing panels. Ownership uses
  ordinary SQL-masked record headers and does not require share-list permission.
  A transfer explicitly acknowledges possible loss of access. Sharing supports
  read/write grants and revocation, including manual-ID revocation when the
  256-recipient list is truncated.
- Member suggestions use the separately authorized, bounded membership read;
  the UI labels its 100-ID limit. An unlisted user is not claimed to be absent.
  IDs remain usable when suggestions are denied or incomplete.
- Typed clients validate paths, target IDs, positive versions, response budgets,
  duplicate IDs and returned mutation witnesses. No role-name or token-claim
  branch grants authority. Every operation receives live server authorization.
- Conflict/denial clears editable snapshots. Known success is distinct from a
  later failed reload (for example, an owner transfer removes the actor's access).
  Business editing is invalidated when an access change starts.
- POST errors with uncertain outcomes are never automatically repeated.
  Dispatched and unknown states are distinct. Fresh reads plus explicit review
  permit a new intent; they do not establish the original request's outcome.
  Ownership-only reads cannot release an unresolved share change. A truncated
  share window cannot release a pending change whose recipient is not shown.
- A payload-free, actor-owned recovery provider above routing preserves admin
  resource IDs/conflict witnesses and existing business-command keys across
  route/workspace/account changes. Tokens, names, values and share lists are
  excluded. Abort/unmount does not imply rollback. Pending changes install a
  before-unload warning; browser closure/reload still loses the memory journal.

## Files and architecture

New frontend modules under `apps/web/src/administration/`: typed decoders,
team panel, record access panel, optional member suggestions, recovery provider
and a small mutation-state hook. `src/api/administration.ts` supplies the bounded
client; existing preview client adds exact-record/access operations. App routing,
AccessConsole and the preview workspace compose the panels and recovery gates.
Existing forms, field masks, command receipts and backend module boundaries are
preserved. Style changes keep identifiers/forms within narrow viewports.

New tests cover decoder/client behavior, team/record UI, navigation/account
isolation and command-key navigation. Two browser scenarios use real PostgreSQL
APIs to exercise administration, conflicts/denial and committed-but-lost replies.
No test-only response is claimed as proof of a database effect.

## Executed evidence so far

| Command | Result |
| --- | --- |
| `node --version`; `./scripts/go.sh version` | PASS: Node 24.19.0, Go 1.27.2 |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps --format '{{.Service}} {{.State}} {{.Health}}'` | PostgreSQL/Redis running healthy; collector running |
| `npm --prefix apps/web test` baseline | PASS 38/38 |
| `node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate status` | PASS: migrations 1–46 applied |
| `npm --prefix apps/web run typecheck` | Initial FAIL: JSX fragment and unsupported RTL `exact` test options; corrected, rerun PASS |
| `npm --prefix apps/web run lint` | Initial FAIL: control-character regex lint; replaced with Unicode control category, without disabling rule; rerun PASS |
| `npm --prefix apps/web test` during implementation | PASS 62, then 72, 73 and 74 tests as cases were added |
| `make build` initial | FAIL in frontend typecheck on test options; Go binaries built; browser checks NOT RUN in this invocation |
| `make build` after type corrections | PASS all eight Go binaries and frontend build |
| `make test-e2e` initial | FAIL: 7/8 passed in 25.6s. Viewer denial encountered both a definitive denial and a misleading transient uncertain-state alert. Dispatched/unknown states separated; no server authorization was relaxed |
| `make test-e2e` after state refinement | PASS 8/8 in 22.5s; logs exposed duplicate sibling React keys for team/preview panels. Fixed with distinct keys and added regression assertions rather than ignoring the warning |
| `npm --prefix apps/web run typecheck`; `npm --prefix apps/web run lint`; `npm --prefix apps/web test`; `make test-e2e` on the stable source | PASS: 74 frontend tests in 8.25s; 8 browser tests in 22.9s; duplicate-key assertions pass |

Independent first review at `dbe3c64` reproduced two findings, retained in the
[independent report](../verification/M9R_R5C_UI_INDEPENDENT_REVIEW.md):

- HIGH R5C-UI-01: unused SQL bind parameters caused valid OWNER-scoped
  delegates' core-only/no-field-write reads and opaque action-allowed/scope-denied
  reads to return 503. Lazily emit the entity-write scope only when consumed;
  resolve field scope before binding opaque action IDs. No grants were broadened.
- MEDIUM R5C-UI-02: an unknown unshare could be reviewed without observing its
  recipient in a truncated list. Keep the resource blocked in that case.

The first independent component run was FAIL 7/8; the populated Go role matrix
failed 3/8 subcases plus the opaque denial probe. The delegated browser stopped
on 503 before its transfer/mobile assertions. These failures remain recorded;
post-fix re-review and final repository verification follow after execution.

Implementer post-fix checks: typecheck/lint PASS, frontend 82/82 PASS (12.01s),
`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5c' ./tests/integration`
PASS (6.369s), `make build` PASS. `make test-e2e` was FAIL 8/9
(30.9s): the delegated case passed query/header/masking and 390px checks, then
correctly received a transfer denial because the independent fixture omitted
the existing required `preview.write` capability. The reviewer will retain
that negative and explicitly grant the preview mutation capability for the
transfer positive control. No server authorization will be relaxed.
Logs and runner artifacts remain ignored.

## Limits, recovery and remaining work

Record controls use the currently shipped revision-preview selector. Dataset
management UI remains R2c work; both server storage adapters are unchanged.
This is not role-definition editing, identity/provider login or a production
administration portal. Buttons do not claim a capability: optimistic controls
can receive a server denial, including a viewer's ownership attempt.

Teams are retained, not deleted. Member suggestions are partial IDs, not a
searchable identity directory. Server hierarchy/share budgets remain unchanged.
There are no new durable admin receipts; membership active-state witnesses keep
the existing ABA limitation. Failed/denied/missing reads cannot clear unknown
outcomes; an authorized administrator may need to inspect live state. Explicit
review does not promise exactly-once execution or causal commit proof.

Rollback frontend behavior while preserving all server facts, migrations and
audit. Never replay old POSTs, erase evidence or restore broad DML grants.
Resolve pending IDs with an authorized live-state read. Keep those IDs before
closing the tab; no credentials or business values belong in a recovery record.

R5 identity/provider/skew, remaining R3b types/R3c uniqueness/R3d aggregates,
R6 requirements/verification/promotion authority and R7 operations remain open.
R7 includes the earlier unconfirmed workflow database connection failure; this
slice does not claim to fix it. M10 remains paused.
