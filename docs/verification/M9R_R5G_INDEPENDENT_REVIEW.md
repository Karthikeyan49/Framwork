# M9R R5g independent adversarial review

19 October 2026 UTC. Initial reviewed source: `c36a0e8` on
`remediation/m9r-foundation`; comparison baseline `8cf4de2`. The checkout was
clean before review. The implementation agent paused product edits, builds and
shared database workloads throughout the exclusive review window. This reviewer
added only independent tests and this report; no product fix, migration,
dependency, secret, main merge, external provider change or M10 work.

Initial result: **CHANGES REQUIRED**. One MEDIUM and one LOW finding reproduced;
no BLOCKER/HIGH identified in this bounded review. Both findings require fixes
and independent re-verification before R5g acceptance. The full final root gates
remain separate from the executed checks below.

Follow-up at **`5c05d0b`: both findings independently CLOSED**. The bounded R5g
review passes after the fixes and stronger executed probes below. Final root
check/test/build, complete browser suites and read/write capacity gates still
remain required; external identity deployment remains deferred.

## Scope and threat model

Read AGENTS/CLAUDE, README, V3 kit, compatibility/supersession guidance, remediation
plan/finding ledger, ADR032/038/039/060 and the complete three-commit change.
Review covered live tenant/member/role authority, route classification, bounded
role discovery/member points, native bootstrap replacement, typed scopes and
lossless condition values, exact Go HTTP wire hashes, semantic acknowledgements,
immutable receipt recovery and stale/unmounted/connection preparation lifetimes.
The deployment runbook and blank evidence template retain the user-authorized
local-only boundary and all external verification as NOT RUN.

Executed attacks use populated isolated test fixtures, signed-valid forged-claim
bearers, revoked live membership/role heads, actual dropped PostgreSQL COMMIT
response, concurrent owner demotions, audit failure, deliberate malformed receipt
responses and delayed client/preparation lifetimes. Operator fixture changes
verify the effective configured and connected database/role first. The frontend
response-fault tests model corrupted/mismatched acknowledgements, not a proof
against an entirely compromised server. No unauthorized database capability or
cross-tenant disclosure was demonstrated.

## R5G-IR-01 — MEDIUM — Receipt body is not bound to its hash

At `c36a0e8`, `publicationMatches` compares role name, operation, next version,
expected hash label, active state and bootstrap marker. It never recomputes the
returned definition's hash. `rolesClient.operation` validates structure and
operation ID, also without checking this binding.

The independent component test records a pending publication with an empty-grant
definition's Go-wire SHA-256. Its receipt response keeps every expected witness
but replaces the body with an ALLOW `tenant.access` grant. The UI displays
"Publication committed as version 2" and clears the recovery journal. Expected:
retain the unknown outcome and reject the inconsistent receipt.

Reproduction:

```sh
npm --prefix apps/web test -- src/administration/RolesPanel.review.test.tsx
```

Test: `review: does not accept a changed receipt definition under unchanged hash
witnesses`. Initial result: **FAIL**, proving the journal was cleared. This
does not mutate a real immutable database receipt; it demonstrates the promised
malformed-success response handling is incomplete on the recovery path.

Required correction: validate the exact normalized Go V1 HTTP definition hash
for this UI-originated intent before reporting receipt success/clearing recovery.
Preserve historical stored bytes and hashes. The intent journal must still omit
policy bodies, conditions and tokens. Test a valid original receipt after the
head advances as well as changed bodies with matching labels.

## R5G-IR-02 — LOW — Canceled preparation leaves a new connection disabled

During asynchronous SHA preparation, replace the client while retaining the
selected-role component. The old lifetime abort correctly prevents POST dispatch,
but its `finally` skips `setPreparing(false)` and the new read effect does not
reset preparation state. The new connection loads the role but the editor stays
disabled with "Publishing…". Closing/reopening the roles panel recovers; no
mutation or journal leakage was observed.

Reproduction:

```sh
npm --prefix apps/web test -- src/administration/RolesPanel.review.test.tsx -t 'connection changes'
```

Test: `review: aborts old preparation when the connection changes`. Result:
**FAIL** on enabled-editing assertion, with four tests intentionally filtered.
Required correction: scope preparation state and its guard to the current
connection/lifetime; late completion must not disable or clear a newer operation.

## Executed evidence

| Command/check | Result |
| --- | --- |
| `git status --short` before review | PASS; clean `c36a0e8` |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestM9RRoleAdministration' -v ./tests/integration` | PASS; original 4 populated groups, 11.441s |
| `npm --prefix apps/web test` before new probes | PASS; 116/116, 17 files, 10.19s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^(TestM9RRoleAdministration\|TestM9RCapabilityWitnessCASAuditRollbackAndUnknownCommit\|TestM9RConcurrentOwnerDemotionsCannotRemoveLastOwner)' -v ./tests/integration` | PASS; 8 groups including two new independent groups, 12.525s. In the actual command the regex alternatives use ordinary `\|`-free pipe characters; table escaping is Markdown only |
| `npm --prefix apps/web test -- src/administration/RolesPanel.review.test.tsx` initial probe | FAIL; receipt finding; four then-current lifetime/denial probes PASS, 2.50s |
| `npm --prefix apps/web test -- src/administration/RolesPanel.review.test.tsx -t 'connection changes'` after adding usable-new-connection assertion | FAIL; preparation finding, 1 failed/4 intentionally filtered, 1.22s |
| `npm --prefix apps/web test -- src/AccessConsole.review.test.tsx` | PASS; 2 populated route/account and self-assignment checks, 1.89s |
| `DEBUG_PRINT_LIMIT=1000 npm --prefix apps/web test > .local/r5g-independent-frontend.log 2>&1` | FAIL; exactly 2 finding regressions, 121 PASS, no skips, 19 files/123 tests, 12.80s |
| `PLAYWRIGHT_BROWSERS_PATH=$PWD/.local/cache/ms-playwright npm --prefix apps/web run test:e2e -- roles.spec.ts` | PASS; 2/2 real browser/API cases, 5.5s; mobile/nested scope/publication/assignment/deactivation/last owner and actual response-loss/SPA/original receipt after head advance |
| `./scripts/go.sh test -race -count=1 -run 'Test(RoleEditorHTTPHashFixture\|AuthoringContractNeverSuggestsUnsupportedGrants)' ./internal/platform/access ./internal/policy` | PASS; shared actual-Go wire hash fixture and catalog/compiler agreement; pipe escaping above is Markdown only |
| `npm --prefix apps/web run lint` | PASS, including new probes |
| `npm --prefix apps/web run typecheck` | Initial reviewer-test-only FAIL: Testing Library `ByRoleOptions` does not accept Playwright's `exact`; replaced it with anchored name regex. Rerun PASS |
| `make docs-verify`, `git diff --check` | PASS; 84 retained architecture and 43 V3 checksums, no whitespace errors |

The two independent backend groups separately prove `roles.read` and
`memberships.read` do not substitute for each other, the contract grants no
workspace access, signed-valid bearer authority is lost after live membership
revocation, configured empty grants lose native viewer authority, native
publication receipts survive head replacement, and an upper cursor gives an
empty bounded page. Existing populated groups execute 115 role summaries in two
pages, member point reads beyond 100, reserved role names, cross-tenant/claim
denials, last-owner rejection and atomic audit rollback. Retained receipt/CAS
tests execute real lost COMMIT, tenant-scoped recovery and immutable-history
mutation denials, plus actual simultaneous owner demotions.

New frontend probes also prove route/account recovery ownership, another actor
cannot see the original actor's journal, reconnect restores it, no automatic
POST retry, unmount before hashing dispatches nothing, aborted dispatched changes
retain unknown intent, denied current read cannot create implicitly, and known
self-assignment refreshes live workspace authority without misreporting a saved
mutation when the subsequent access read denies.

## Limits, recovery and remaining gates

No new schema or migration rehearsal is needed for the read/UI-only change;
existing migrations through 54 remain frozen. No sustained R5g workload or new
capacity envelope was certified. Full `make check test build`, all application
and sign-in browser cases and unchanged reference numeric/write capacity targets
are NOT RUN by this reviewer; the implementation agent owns them after fixes.
Existing independent numerical/SQL/worker limits remain unchanged.

The implementation agent reported pre-review wire-hash and reload-cancellation
test failures that were fixed before `c36a0e8`. This reviewer did not execute the
pre-fix versions; preserve their original evidence in the implementation report
alongside the new failures above. Reruns do not erase those failures.

Recovery remains fresh authorized reads and immutable operation receipts, never
automatic administrative POST replay or deletion of membership/audit/history.
The browser journal is actor-owned and memory-only; full reload/close loses it.
Membership expected-role CAS keeps its explicit ABA limitation. Provider/domain,
real MFA, production gateway/TLS/log canary, secret provisioning, directory
enrollment and other R6/R7 release checks remain DEFERRED/NOT RUN. This review
does not authorize deployment, a main merge, production activation or M10.

## Independent fix verification — `5c05d0b`

The second review began with a clean checkout at `5c05d0b`. The implementation
agent again paused source edits, builds and shared database workloads. This
reviewer inspected the two-file fix and added three stronger regression tests;
no product source was changed by the reviewer.

**R5G-IR-01 CLOSED.** Publication preparation and receipt recovery now share the
exact normalized Go V1 HTTP definition hash. An unchanged label with changed
grants fails verification and retains the original journal entry. The original
populated failure now passes. An additional delayed-hash test proves unmount
cannot clear the pending intent; remount plus explicit valid receipt verification
then clears it. The real browser again reconciles the correct original receipt
after a newer head exists. No historical role bytes or stored hashes changed.

**R5G-IR-02 CLOSED.** Preparation state and its synchronous guard bind both the
client and a unique preparation key. Lifetime abort releases only its own
pre-dispatch marker. The original disabled-editor failure now passes. Two
stronger probes execute the overlapping promises: a fresh connection prepares
and successfully publishes before the old digest settles, and completion of the
old digest while a newer digest is pending cannot clear its busy marker, enable
editing or dispatch a POST. Resolving the newer digest yields exactly one
publication under the new connection. Old completion produces no extra write.

| Executed follow-up command | Result |
| --- | --- |
| `npm --prefix apps/web test -- src/administration/RolesPanel.review.test.tsx src/AccessConsole.review.test.tsx src/administration/RolesPanel.test.tsx src/administration/roleContracts.test.ts` | PASS; 28/28 in 4 files, 3.63s, including both actual AccessConsole review tests and the two stronger preparation probes |
| `npm --prefix apps/web test` after adding delayed receipt-hash unmount probe | PASS; 126/126 in 19 files, no failures/skips, 10.74s |
| `PLAYWRIGHT_BROWSERS_PATH=$PWD/.local/cache/ms-playwright npm --prefix apps/web run test:e2e -- roles.spec.ts` | PASS; 2/2 actual browser/API cases, 5.0s |
| `npm --prefix apps/web run lint`, `npm --prefix apps/web run typecheck`, `git diff --check` after all new probes | PASS |

The implementation agent reports an initial fix typecheck failure from missing
`useRef` initialization, corrected before `5c05d0b`; this reviewer did not execute
that pre-commit state. Its independent post-fix typecheck passes as recorded
above. The initial product failures and reviewer-test-only typecheck failure
above remain preserved.

The backend is unchanged since the independently passing eight-group run. It was
not rerun in this second window; no claim of a new full backend regression run
follows. Full foundation gates remain with the implementation agent. No further
finding was identified within this bounded review, and all scope/threat-model,
performance, memory-journal/ABA and external-deployment limits above remain.

Source-inspected authoring limits: the 65,536-byte HTTP decoder ceiling includes
the command envelope, while the definition contract's 65,536-byte limit concerns
only the definition. A near-limit draft can therefore fail server admission even
after local definition validation. The condition editor also accepts an escaped
NUL that the server's PostgreSQL-compatible strict JSON decoder rejects. Both
paths fail closed with 400 and cannot grant access or commit a publication;
they remain local validation/usability limits, not proof that every locally
accepted draft is server-admissible. This review did not separately execute
these two boundary cases. The editor intentionally rejects unsafe JSON numeric
representations, so no claim that all historical/server-valid definitions are
editable follows.
