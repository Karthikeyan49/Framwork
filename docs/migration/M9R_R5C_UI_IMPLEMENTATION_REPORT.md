# R5c UI — Governed record and team administration

Date: 15 October 2026 IST. Status: **PASS within the bounded R5c preview UI
contract**, with independent review and final verification passed. Branch
`remediation/m9r-foundation`; clean starting checkpoint
`aa1fa2a`, implementation `c993362`, production fixes `f81d207` and `c0df7c2`,
independent acceptance `e03cc80`. Full R5/M9R remain PARTIAL. No M10, main merge
or production activation.

## Scope and behavior

[ADR038](../adr/038-m9r-administration-ui.md) was written before implementation
and extended before the necessary integration corrections. The UI consumes
ADR034/035's existing governed APIs. No new capability grants, dependencies,
SQL migrations or immutable metadata/IR formats were introduced.

- A connected workspace has a team panel: bounded hierarchy, create,
  rename/reparent, team versions and retained active/inactive memberships.
  Writes use the exact observed team version or membership true/false/null
  witness; creation generates an ID before dispatch.
- Selected preview records have separate ownership and sharing panels.
  Ownership reads ordinary masked record headers without share-list permission.
  A transfer acknowledges possible access loss. Sharing grants/revokes read or
  write access and supports manual recipient IDs for truncated lists.
- Optional member suggestions use a separately authorized read. The UI labels
  its 100-ID limit and does not treat an unlisted user as absent. The share
  list labels its 256-recipient limit. IDs remain usable without suggestions.
- Typed clients validate paths, target IDs, positive safe versions, response
  budgets, duplicate IDs and exact mutation witnesses. Role names and token
  claims never grant authority. The server authorizes every operation live.
- Conflict/denial clears editable snapshots; access changes invalidate business
  editing. A successful transfer remains a success if its following reload is
  denied because the old owner lost access.
- There is no automatic POST retry. Transport/5xx/malformed-success failures
  retain an uncertain intent; 4xx is a rejection. Dispatched and unknown states
  are distinct. A fresh authorized state read plus explicit manual review
  permits a new intent, without proving the original request's outcome.
  Ownership-only reads cannot clear share uncertainty. A truncated list cannot
  clear a pending change whose recipient is not observable.
- An actor-owned recovery provider above routing preserves only operation IDs,
  resource IDs and conflict witnesses through SPA navigation, workspace switches
  and account changes. No tokens, names, values or share lists enter the journal.
  Abort/unmount does not imply rollback. Existing business-command recovery keys
  use the same provider. Pending operations install a browser unload warning.

Independent integration tests also exposed two existing backend dependencies:
unused SQL bind parameters in masked queries, and full-business-field reads
inside record access administration. The narrow fixes preserve field rules for
business operations, default-deny actions, SQL scopes, trusted owner checks,
forced RLS, CAS, dataset fences and transaction boundaries. Access administration
now fetches typed headers only; it does not fetch or decode business JSON.

## Files and decisions

| Area | Important files |
| --- | --- |
| Administration UI and bounded contracts | `apps/web/src/administration/{TeamsPanel,RecordAccessPanel,MemberSuggestions,Recovery,useChange,contracts}` |
| API clients and preview composition | `apps/web/src/api/{administration,preview}.ts`, `src/preview/{Workspace,contracts}`, `AccessConsole.tsx`, `App.tsx`, `style.css` |
| Backend integration corrections | `internal/runtime/data/visibility.go`, `internal/runtime/data/access.go` |
| Tests | administration decoder/client/component/navigation tests; `CommandRecovery.test.tsx`; browser `preview.spec.ts` and `administration.review.spec.ts`; `tests/integration/m9r_administration_ui_review_test.go` |
| Evidence and authority | ADR038, this report, independent review, finding ledger, remediation plan, README and concise agent contract |
| Test-runner capacity | `Makefile`, `.github/workflows/ci.yml` |

No dependency or lockfile change. Existing Go/PostgreSQL/React/TypeScript and
Vitest/Playwright tooling suffice. Existing V2/V3 source kits remain verbatim.
Migrations 1–46 were already applied; this slice adds none. Final migration and
checksum evidence is recorded below.

## Independent findings and closure

The [independent report](../verification/M9R_R5C_UI_INDEPENDENT_REVIEW.md)
preserves its pre-fix failures, fixture mistakes, executed negative controls
and final bounded acceptance. Source remained stable during each review; the
reviewer added only tests/evidence. No concurrent test workloads were used.

| Finding | Reproduction and correction |
| --- | --- |
| HIGH R5C-UI-01 | Valid OWNER-write roles with core-only/no-field-write projections returned 503 (3/8 live-role matrix cases). An opaque allowed-action/denied-scope read also returned 503. Lazily emit the entity-write predicate only when consumed; resolve field scope before binding opaque action IDs. Query masks and permission decisions remain enforced. |
| MEDIUM R5C-UI-02 | Unknown unshare to an absent recipient could clear its gate from a truncated window. Keep the gate until that target is observable or the list is complete. Displayed-target and complete-absence positive controls pass; malformed targets remain blocked. |
| HIGH R5C-UI-03 | A masked assign-only delegate could not transfer ownership because the preflight required scopes for every business field and decoded unrelated hidden values. Replace that preflight with typed-header selection under live entity read/write scopes, followed by the existing row/admin checks. Hidden-field and malformed-hidden-value cases pass; missing capabilities, non-owner, NONE scope and revoked role still deny without changes. |

Final independent evidence at `e03cc80`: seven top-level Go integration tests
PASS (25.903s), including eight-case query and seven-case mutation matrices,
opaque denial, masked share read/grant/revoke and live role revocation. Twelve
independent frontend probes PASS (3.37s); typecheck/lint/diff PASS. The real
browser case PASS (4.4s scenario, 7.2s runner) verifies missing `preview.write`
denial with unchanged facts, explicit live grant, masked transfer to version 3,
loss of OWNER visibility, and ownership/team/sharing at a 390×844 viewport.
This is bounded narrow-viewport coverage, not all-browser accessibility proof.

## Commands and results

These are executed checks, including failures. Logs, credentials and generated
runner artifacts remain ignored under `.local/`; they are not committed.

| Command | Result |
| --- | --- |
| `node --version`; `./scripts/go.sh version` | PASS: Node 24.19.0, Go 1.27.2 |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps --format '{{.Service}} {{.State}} {{.Health}}'` | PostgreSQL and Redis healthy; collector running |
| `npm --prefix apps/web test` baseline | PASS 38/38 |
| `node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate status` | PASS: migrations 1–46 applied |
| `npm --prefix apps/web run typecheck` | Initial FAIL on JSX fragment and unsupported RTL `exact` test options; corrected, subsequent PASS |
| `npm --prefix apps/web run lint` | Initial FAIL on control regex; corrected with Unicode control category, without disabling the lint rule; subsequent PASS |
| `npm --prefix apps/web test` incremental | PASS 62, 72, 73, 74 and 82 tests as cases were added; final count below |
| `make build` initial | FAIL in frontend test typecheck; Go binaries built, browser NOT RUN in that invocation |
| `make build` after corrections | PASS eight Go binaries and frontend |
| `make test-e2e` initial | FAIL 7/8 (25.6s): definitive denial displayed alongside transient unknown state. Separate dispatched/unknown states; keep authorization unchanged. |
| `make test-e2e` next | PASS 8/8 (22.5s), but logs exposed duplicate sibling React keys. Corrected distinct team/preview keys and added regression assertions. |
| Typecheck/lint/unit/browser on first stable source | PASS 74 frontend tests (8.25s), browser 8/8 (22.9s), duplicate-key assertions pass |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5c' ./tests/integration` after first fixes | PASS (6.369s) |
| `make test-e2e` after first fixes | FAIL 8/9 (30.9s): independent delegate fixture omitted required `preview.write`. Reviewer retained that denial and granted it explicitly for positive control. A second transfer failure then exposed R5C-UI-03. |
| Targeted integration command below, after header fix | PASS (23.942s) |
| `./scripts/go.sh build -o bin/api ./cmd/api` | PASS |
| `PLAYWRIGHT_BROWSERS_PATH="$PWD/.local/cache/ms-playwright" npm --prefix apps/web run test:e2e -- administration.review.spec.ts` | PASS 1/1 (6.5s runner) after header fix; independent final result above |
| `GOFLAGS=-v make check test build` first final run | FAIL: checks, Go/Node unit tests and frontend 86/86 passed. Worker-policy EXPLAIN hit the unchanged 2s SQL timeout after the preceding 96-receipt query took 1657.145ms. Later the aggregate integration deadline expired at 900.080s, with 162 top-level tests passed and the current test only 12s old. Build NOT RUN in this invocation. |
| `node scripts/with-env.mjs ./scripts/go.sh test -v -race -tags=integration -timeout=5m -count=3 -run '^TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry$' ./tests/integration` | PASS 3/3 (16.066s); first-query measurements 1107.686/1007.408/1171.343ms. Repetitions do not prove a reliability fix. |

Exact targeted integration command (the same selection used by the independent
final review):

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5c|^TestM9RRecordAccess' ./tests/integration
```

The 177-test suite needed more cumulative time: passed tests alone summed to
880.2s before the first run's deadline. Commit `c139253` adjusts only the aggregate
integration ceiling from 15 to 20 minutes and CI job ceiling from 20 to 35
minutes. Independent read-only review PASS: no selection, assertion, SQL/query/
lock timeout, workflow lease or performance target changed. This does not fix
the separate worker EXPLAIN timeout. R5b had already documented its narrow
capacity margin; R7 must profile it. The first failed run remains evidence.

Final whole-repository confirmation:

| Command | Result |
| --- | --- |
| `GOFLAGS=-v make check test build` | PASS, saved exit 0; 592s total, integration 561.291s. All 177 top-level integration tests, Go race/unit/security/import-boundary tests, Node script tests, frontend 86/86, format/vet/module/lint/type checks, eight Go binaries and frontend build passed. Source checksums 84/84 and V3 checksums 43/43 passed. |

The worker workload also passed in this full rerun (first query 1146.412ms,
complete test 5.65s), under its unchanged statement budget. This establishes a
passing run, not a resolution of the earlier intermittent capacity failure.

| Additional final command | Result |
| --- | --- |
| `make migrate migration-status` | PASS, saved exit 0; rerun applies no new migration, status 1–46 applied |
| `make test-e2e` | PASS 9/9 (19.0s), saved exit 0; real CRUD/masking/recovery, team/record administration and delegated mobile transfer |
| `make test-performance` | PASS (40.816s), saved exit 0; PostgreSQL 18.6, four CPUs/8 GiB reference, 100k hot rows plus nine 10k tenants; numeric index READY with 100k scanned/verified, 100 HTTP samples over ten clients, p95 113.715ms, zero errors, unchanged 750ms target |

The performance result requires the governed numeric index and stated workload.
It is not unindexed/general query, text/date/search, uniqueness, aggregate or
worker-receipt capacity evidence. `make test-write-performance` was NOT RUN in
this slice: write scheduling/receipts were unchanged; existing full write
regressions and browser/database checks passed. Remote GitHub Actions
execution is separate; no remote CI PASS is claimed.

`make docs-verify`: PASS, 84/84 source and 43/43 V3 checksums.
`git diff --check`: PASS. Only documentation changed after final runtime checks.

## Limits, security, recovery and remaining work

Record controls use the shipped revision-preview selector. Dataset-management
UI remains R2c work; both storage adapters are unchanged. This is not
role-definition editing, production identity/provider login or a production
administration portal. Optimistic controls can receive server denials; they
are not claims of authority. Bootstrap owner/admin policy remains broad.

Teams are retained, not deleted. Suggestions are partial IDs, not a searchable
identity directory. Server hierarchy/share budgets remain unchanged. There
are no new durable admin receipts; membership active-state witnesses retain
ADR035's ABA limitation. Browser closure/reload loses the in-memory journal;
keep pending operation/resource IDs before closing. Never retain credentials
or business values in a recovery note. A denied/missing read or unobservable
truncated-list target keeps the resource blocked; another authorized
administrator may need to inspect it. There is no universal exactly-once claim.

Rollback is a frontend/query-path rollback while preserving server migrations,
owner/team/share facts, record versions and audit. Reverting the query fixes
reintroduces the documented delegate limitations. Never replay old POSTs, erase
evidence, restore broad DML grants or reverse customer data to resolve a UI
outcome. Forward-fix server authorization defects.

R5 provider/login/clock skew, remaining R3b index types, R3c uniqueness, R3d SQL
aggregates, R6 requirement/verification/promotion authority and R7 operations
remain open. R7 includes both the earlier unconfirmed workflow connection
failure and this worker-policy statement timeout. No external action, active
production release or M10 work was enabled.

## Git

All work stays on `remediation/m9r-foundation`; no main merge or history rewrite.
The server accepted a normal push from `aa1fa2a` to `e717224`; earlier R5d
work was already present at that server checkpoint. The initial 18-commit ahead
count came from an older cached remote-tracking ref, not the live server head,
and is not evidence that R5d had been missing remotely. A direct remote read
confirmed `e717224` matched local HEAD with a clean worktree. This documentation
correction is a forward commit on the same branch; delivery verifies and records
its final local/remote SHA in the completion message. Source, tests, ADRs, reports and CI changes belong in
git; `.env`, keys, dependencies, logs, Playwright output, caches and generated
binaries/builds remain ignored.

R5c UI STATUS: PASS (bounded preview administration)

R5 / M9R STATUS: PARTIAL — identity, remaining data capabilities, R6 and R7 open.
M10 remains paused.
