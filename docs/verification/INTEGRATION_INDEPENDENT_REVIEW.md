# Independent review of the integration branch `integration/m9r-m14`

Reviewer: independent agent, no part in building either line; no product code was changed. Pinned commit
`552fa9efe2092a592e39974ee23ec1ecbc057729` (head of `origin/integration/m9r-m14` when the review started). Review branch
`review/integration-final`. Rules of truth: PASS / FAIL / NOT RUN. Every "held" names the attack that was executed.
The M14 half of the review is `docs/verification/M14_INDEPENDENT_REVIEW.md`.

STATUS at `552fa9e` (the pinned commit): COMPLETE for the attacks listed (what was not covered: last section). Overall: **FAIL** - 3 MEDIUM (IR-01, IR-02, IR-06),
3 LOW (IR-03 to IR-05), no HIGH. The merge kept the authority and tenant-isolation invariants of both lines in every attack executed; what it
did not keep is one meaning of a version token in one SQL literal (IR-01), the availability of the admission slots (IR-02, inherited), one M13
fence (IR-06), a gate that reproduces (IR-03) and truthful ledgers (IR-04).

STATUS at `be6ec7cae1849bf535bad1811b21bf93067598b5` (re-verified 2026-10-22, the head of `origin/integration/m9r-m14` after the builders' fixes): the gate **PASS**es on every step I ran (`make check`, `make build`, `make test-unit`, the full integration suite as one run with 437 PASS / 5 SKIP / 0 FAIL, e2e 11, sign-in e2e 7, the five fuzz targets, the ten-minute outage); IR-01 and IR-05 are verified **FIXED** by attacks that the builders' tests do not make, and IR-02 is fixed for the database slots (the third pass corrects this: the fix opened a memory lever that I had not measured); IR-04 is fixed for the statements cited (two sentences that still name `552fa9e` as the tip remain); IR-03 is **partly fixed** (two of three timing-bound causes repaired in the tests); **IR-06 (MEDIUM) is open and reproduces unchanged**. Overall: **FAIL** - one MEDIUM open in this report (IR-06) and, in the M14 report, three more (M14R-01 to M14R-03) and four LOW, none fixed by any commit after the pinned one; no HIGH.

STATUS at `0e13abe` (third pass, 2026-10-22, the tip of `origin/integration/m9r-m14` after 37 more commits): the gate **PASS**es on every step I ran (`make check` 96.2 s, `make build` 22.4 s, `make test-unit` 256.7 s, the full integration suite as one run with 453 PASS / 5 SKIP / 0 FAIL in 1845.922 s, e2e 11, sign-in e2e 7, the five fuzz targets, the ten-minute outage); IR-01, IR-03, IR-04, IR-05 and IR-06 are verified **FIXED**, and IR-02 is **FIXED** with the body budget - the verdict "IR-02 FIXED" of the previous pass is corrected in the section below (the first fix opened a memory lever that I had read and not measured; the second review found it); the code the builders added since (the body budget, the approval and rollback fences, the repeated AI exchanges, the compensation scan, the launcher and the version-gate scan) was attacked with probes of my own (A.13 to A.18) and held, except for two guards that fail open (IR-07, IR-08, LOW). Overall: **PASS** - no HIGH and no MEDIUM open in this report; open LOW: IR-07 and IR-08 here, the documented bandwidth-bound residual of the body budget (A.14), and M14R-03 (the expiring-token residual), M14R-05 and M14R-08 in the M14 report. A PASS here does not certify production readiness: what I did not cover is the last section.

Everything from "Verdict by decision" down to the heading "Re-verification at `be6ec7c`" is the review of the pinned commit and is kept as written; the
later passes are the sections "Re-verification at `be6ec7c`" and "Third pass at `0e13abe`", before "Not covered". The third pass corrects one verdict of the second (IR-02).

## Verdict by decision of the plan (at the pinned commit; the last columns are the two later passes)

| # | Decision | Verdict at `552fa9e` | Deciding attack | Verdict at `be6ec7c` | Verdict at `0e13abe` |
|---|---|---|---|---|---|
| 1 | One meaning per version token (authoring schema, IR and compiler 1.3.0 / 1.4.0 / 1.5.0; workflow DSL 2.0.0 / 2.1.0 / 2.2.0) | **FAIL** (IR-01) | `TestReviewUniqueFieldUnderEachSchemaVersion`; nine re-shaped IR artifacts restored (`TestReviewVersionTokenReadings`, held) | **PASS** (IR-01 fixed) | **PASS** |
| 2 | Migration order and the shared `metadata_versions_schema_version_check` | PASS (IR-05 LOW) | mechanical object intersection of both migration sets; `TestReviewMigrateOverMilestoneNumbering` | PASS (IR-05 fixed for a real milestone database) | PASS (the migrations are now 00001-00057 and 00080-00092; the milestone-database refusal and the numbering probe ran again; `00091` replaces `rollback_release` of `00088` and differs from it by four lines (three of comment and one `INSERT` into `control.release_fences`), `00092` replaces `approve_release` of `00084` and adds one trigger function and one trigger; no object of the milestone line's numbers is touched) |
| 3 | Derived worker guard `control.worker_grants_safe()` | PASS | evaluated in a real `framework_worker` session; privilege inventory of the four guarded schemas | PASS (no file of the guard differs from the pinned commit) | PASS (the privilege inventory of the four guarded schemas was repeated on a database migrated to the tip (A.18): 104 SECURITY DEFINER functions (one more: `release_candidate_fence`, executable by nobody), 4 that pin `pg_catalog` alone (the R7b ones, as before), 0 not owned by the schema owner, 0 executable by PUBLIC, 72 tables (71 with forced row-level security), the worker holds 16 relations and 26 functions, the dispatcher 5, the 60 rows of the worker allowlist are all held, 833 internal references and none unresolved) |
| 4 | R7b event authority against the M10 ledger and the M11 freeze | PASS | `TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts`; isolation-level read of `RuntimeSnapshot`, `jobs.go`, `freezePattern` | PASS (`TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts` run again: the live reader still stops the migrator at 57 (55P03) and the database stays at 56; after the reader ends the same call applies 57 and 80-90 and the version is 90) | PASS (`TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts` ran again: the live reader still stops the migrator at 57 (55P03) and the database stays at 56; after it ends the same call applies 57 and 80-92) |
| 5 | HTTP front door (`internal/platform/access/access.go`) | PASS for authority, **FAIL** for availability (IR-02) | `TestReviewRouteMatrix` (45 POST routes); `TestReviewSlowBodyHoldsAdmissionSlots` | **PASS** for authority and for the database slots (corrected in the third pass: the fix opened a memory lever, F-01) | **PASS** (authority: the route matrix again, 45 POST routes, 74 denied calls and 90 denial rows; availability: the slow-body probes and the body budget (A.13, A.14), see the third pass; an anonymous caller is now never read on any route (F-02, A.14)) |
| 6 | Honesty of the documents | **FAIL** (IR-04) | reading against the tree | **PASS** for the statements cited in IR-04; two sentences that name `552fa9e` as the tip remain (LOW, see the re-verification) | **PASS** (the two sentences that named `552fa9e` as the tip are rewritten and the "review is NOT RUN" sentence is gone; the builders' own documentation audit lists further defects that I did not re-verify) |
| - | M13 fixes carried by the merge (migration `00088`) | **FAIL** (IR-06) | `TestReviewRollbackOvertakesConcurrentRejection` | **FAIL** (IR-06 open, unchanged) | **PASS** (IR-06 fixed by `00091`, the approval fence `00092` added; `TestReviewRollbackOvertakesConcurrentRejection` passes, and the randomized stress of the four release ports found no violation in 168 rounds) |

## Progress (UTC)

| Time | Step | State |
|---|---|---|
| 2026-10-20 13:58 | Docker daemon started (it was not running); no pre-existing containers or volumes | done |
| 14:02 | `make setup` (Go 1.27.2; Node 24.22.0 installed outside the repo, checksum verified), `make infra-up`, `make migrate` | 67 migrations applied (`00001`-`00057`, `00080`-`00089`) |
| 14:03 | `make check` | PASS, 49.8 s |
| 14:06 | `make test-unit` | first run FAIL (`internal/tools`, IR-03); node and web tests run separately: 9 + 126 PASS |
| 14:12-14:57 | full integration suite, `-race`, 439 listed / 438 run | 2716.8 s, **2 FAIL** (IR-03) |
| 14:58-15:25 | static review of the merge (decisions 1-6) and executed probes | IR-01, IR-02, IR-05, held attacks |
| 15:13-15:30 | `make build` (24.7 s), `make test-e2e` (11 passed), `make test-signin-e2e` (7 passed), the three fuzz targets, `make test-event-outage` (611 s) | all PASS |
| 2026-10-22 07:18 | resumed after an environment pause: Docker daemon restarted, stack healthy, database at migration 89 | done |
| 07:20-07:55 | M14 probes (see the M14 report), rollback race (IR-06), the two timing-bound integration tests alone | IR-06; the two tests pass 6 of 6 and 3 of 3 |
| 07:41-08:12 | second full integration run, `-race -v`, clean tree (no probe files) | **PASS**: `ok` after 1880.094 s; 433 PASS, 5 SKIP, 0 FAIL |
| 08:13-08:16 | `make test-unit` again | **PASS**, 194.6 s |
| 08:16-08:25 | the flaky unit test 12 times while `internal/ai`, `internal/compiler`, `internal/promotion` ran under `-race` in parallel | 12 of 12 PASS |
| 08:26-08:31 | every probe of the appendices re-run in one batch against the unchanged tree | every probe vets clean; the probes that show a defect fail as listed under Reproduction, the others pass; the working tree was clean afterwards |
| 2026-10-22 11:44-12:00 | follow-up on items first marked NOT RUN, after another environment pause (Docker daemon restarted again): R7a burst against `bin/api`, frozen-sandbox records, `strict_candidate_json` differential, HTTP provider dial path and requirement-proposal fuzz (the last two are in the M14 report) | all PASS, see "Follow-up" |
| 16:40-17:03 | re-verification at `be6ec7c` in a separate worktree (the head had moved): `make check`, `make build`, `make test-unit`, then the probes of the fixes and of the open findings | `make check` 90.2 s, `make build` 16.9 s, `make test-unit` 237.8 s: all PASS |
| 17:03-17:34 | full integration suite at `be6ec7c`, `-race -v`, clean tree | **PASS** as one run, 1854.972 s: 437 PASS, 5 SKIP (the opt-in retained legacy artifact tests), 0 FAIL; 442 tests run; no data race, no panic |
| 17:34-17:53 | e2e, sign-in e2e, fuzz, the probe batch, the negative control of the builders' SQL scan, the test count, the ten-minute outage, all at `be6ec7c` | all PASS; the probes confirm the three fixes and still reproduce IR-06 and M14R-01 to M14R-08 |
| 17:53-18:01 | the IR-03 test fixes under CPU load (A.12), pinned tree against `be6ec7c` | (a) `TestM12APIDetailReadsOfALargeCandidateConverge`: pinned tree 0 of 3 failed (11/11/12 s), `be6ec7c` 0 of 3 failed (11/10/10 s); (b) `TestTimeoutWhileServerSleepsIsUnknownAndBounded` x300: pinned tree 33 failing executions, `be6ec7c` 0; (c) `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` (unchanged by the builders) at `be6ec7c`: 0 of 3 failed (8/8/8 s) |
| 2026-10-22 20:36-20:43 | third pass: worktree at `0e13abe` (37 commits further), product diff read, `make check`, `make build`, `make test-unit` | `make check` 96.2 s, `make build` 22.4 s, `make test-unit` 256.7 s (29 Go packages, 13 script tests, 126 frontend tests in 19 files): all PASS |
| 20:47-20:53 | third pass: the earlier probes against the tip and my new ones (A.13 to A.15); the Docker daemon had died in an environment pause and was restarted first (a first batch that ran against the dead stack was discarded) | every earlier probe and the new ones ran against the tip: the probes of IR-06 and of M14R-01, -02, -06, -07 now PASS, M14R-05 and M14R-08 still reproduce, the held attacks hold; two probes fail for reasons that are not findings (below) |
| 20:55-21:26 | third pass: full integration suite at the tip, `-race -v`, clean tree | **PASS** as one run, 1845.922 s: 453 PASS, 5 SKIP (the opt-in retained legacy artifact tests), 0 FAIL; 458 of 459 tests run (the excluded one is the 459th); no data race, no panic |
| 21:26-22:17 | third pass: e2e, sign-in e2e, fuzz, test count, the ten-minute outage, then the extras (compensation scan timing, R7a burst against the binary of the tip, the fence stress of 168 rounds, the body budget probes - accounting, a full budget and its release, declared-and-unsent bodies, anonymous callers, isolation - the abandoned-call counter, stored model labels, the launcher spellings, the version-gate scan and the catalog inventory; the last final run of the probes of A.13 and A.14 is the one quoted) | all PASS (details in the gate table and in the attacks below) |

## Findings

Severity: HIGH exploitable violation of a security or integrity invariant; MEDIUM realistic integrity/availability defect or breach of a
documented contract; LOW hardening, documentation or test gap.

### IR-01 MEDIUM - `field.unique` cannot be enabled under authoring schema/IR 1.5.0, and every write to such an entity is refused

Status at `be6ec7c`: **FIXED** by migration `00090`. At `0e13abe`: **FIXED**, unchanged (the probes pass; the builders widened their SQL scan). The finding below is as made at `552fa9e`.

Contract: ADR100 Amendment I1: "`1.5.0` is a superset of `1.4.0`: it carries both and adds the tool activity";
`metadata.IntegritySchema(v)` is true for 1.4.0 and 1.5.0; the compiler, `validateIntegrity` and the renderer accept `unique` under 1.5.0.

Evidence: `db/migrations/00052_m9r_unique_field_integrity.sql:145` (function `runtime.register_unique_constraint`) selects the field
definition with `m.schema_version='1.4.0'`. No migration of either line replaced it, so under a 1.5.0 document the SELECT finds nothing,
`typ` stays NULL and the function raises `unsupported unique field` (22023), which the API answers as HTTP 400 `invalid_index`.
The record-write trigger `runtime.maintain_unique_claims` (`00052:209-231`) refuses (55000 `unique integrity not ready`) every INSERT/UPDATE of an
entity that declares a unique field without a READY claim - also a write that leaves the unique field out. The plan's Decision 1 lists every place that
names the versions ("the frontend renderer, the AI schema map, ..."); this SQL literal is the one it missed.

Executed (`TestReviewUniqueFieldUnderEachSchemaVersion`, Appendix A: the builders' own `uniqueFixture` with the document's schema version as a parameter):

| Document schema | create before registration (value set / field absent) | register | build | create after (value set / field absent) |
|---|---|---|---|---|
| 1.4.0 | 409 / 409 (`unique integrity not ready`) | 200 `BUILDING` | `READY` | **200 / 200** |
| 1.5.0 | 409 / 409 | **400 `invalid_index`** | impossible: no claim can exist | **409 / 409**, executed after the failed registration |

Impact: an application that needs both tools (1.5.0 is the only schema that admits them) and a unique field cannot write to that entity at all; the
failure is closed (no duplicate is stored), so this is an availability/contract defect, not an integrity breach. The tool-capable contract does not
carry integrity, although the plan, ADR100 I1 and the compiler say it does. No test exercises `unique` under 1.5.0 (`m9r_unique_*` use 1.4.0 only; the
tool tests use 1.5.0 without `unique`).

Fix direction (forward-only, a number from 00090 up): replace `runtime.register_unique_constraint` with the 1.4.0 body and
`m.schema_version IN ('1.4.0','1.5.0')`; add the unique lifecycle test under both schemas; add to the plan's version checklist: grep the migrations for
version literals.

### IR-02 MEDIUM - request bodies are read inside the database admission slot and transaction (inherited from the remediation line, extended by every M12-M14 POST route)

Status at `be6ec7c`: **FIXED for the database slots** by the body pre-read in `serve`; the third pass corrects this: the change opened a memory lever (the second review's F-01, MEDIUM). At `0e13abe`: **FIXED** with the body budget (`bodybudget.go`), see "Third pass at `0e13abe`". The finding below is as made at `552fa9e`.

Contract: R7a (ADR061) "bounded process-local admission before protected API work"; `API_DB_CONCURRENCY` (default 4) bounds database work; the
commands route reads its body *before* the transaction (`access.go`, `localCommand`) precisely to avoid this.

Evidence: `internal/platform/access/access.go:251-260` (commands) and `:264-272` (the two content-idempotent release POSTs) read the body before the
transaction. For every other POST route `serve` takes the admission slot (`transactionAt`, `:212-223`, `Admission.Acquire` at `:215`), opens the REPEATABLE READ
transaction (`:289`) and only then lets the handler read the body: `aiBody` (`ai.go:88`), `readDatasetRequest` (`datasets.go:34-40`; called from the dataset, evolution,
index, record-access, role, sandbox, team, unique-constraint and workflow handlers), `readPromotionRequest` (`promote.go:103-113`; requirements, acceptance, candidates and
release decisions), `readDataCommand` (`preview.go:34-38`; entity query and rule evaluation) and the metadata append (`metadata.go:45`, which pre-dates the merge).
The server's `ReadTimeout` (10 s) is the only bound, and `postgres.WithRuntime` does not cancel a blocked `Body.Read`.
The traffic gate counts requests, not holds (defaults 300 per second per peer), so four requests suffice.

Executed (`TestReviewSlowBodyHoldsAdmissionSlots`, Appendix A; real `http.Server` from `httpserver.New`, four connections of the tenant owner,
`Content-Length: 30000`, one byte per 0.7 s): an honest `GET` waited **8.02 s** (baseline 10 ms); on the inherited metadata route **8.04 s** (so it is not
specific to M14); an honest AI proposal (scripted provider answers in 1.5 s) returned **503 after 9.54 s** and stayed **PENDING** (baseline 201 in 1.57 s):
the result exchange of an AI request needs a slot again after the provider phase and its 8 s context expired in the queue (this is one of the triggers of
M14R-03). After the attackers left: 40 ms.

Impact: any tenant owner/admin (the policy check passes before the body is read) can stall the whole API process for every tenant and make AI results get
lost; no data is exposed.

Fix direction: read and bound the body before `transactionAt` for every POST (as `localCommand`/`retryRelease` do), or set a short per-request read deadline
with `http.NewResponseController(w).SetReadDeadline` before taking the slot.

### IR-03 LOW - the gate is host-bound: three timing-bound assertions failed on a slower host session, and the plan's "green" was a composite (the pinned head passes as one run on a faster one)

Status at `be6ec7c`: **PARTLY FIXED**: the test-side causes of two of the three timing-bound failures were repaired (a retrying set-up, a test server that aborts a departed client's handler), the worker-policy budget is unchanged by design; the gate passed as one run at `be6ec7c` on this host. At `0e13abe`: **FIXED** for the three assertions: the compensation statement reads the receipts once (`internal/runtime/workflow/execution.go:273-281`, an uncorrelated `NOT IN`), 1.22-1.35 s -> 32-36 ms for the same 96 receipts (A.15) with identical rows on 4,000 random receipt sets (A.13); in the full run `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` took 3.24 s (6.10 s at `be6ec7c`) and `TestM12APIDetailReadsOfALargeCandidateConverge` 8.32 s. The slower-database variant is **NOT RUN**. The finding below is as made at `552fa9e`.

Executed on this host (4 vCPU, `nice -n 10`, one process at a time) in two sessions: 2026-10-20 (session 1) and 2026-10-22 (session 2, after an environment
pause; the host was visibly faster, the whole integration suite took 1880.1 s instead of 2716.8 s). The step-by-step table is under "Gate reproduction".

Session 1: `make test-unit` failed once in `internal/tools` (`TestTimeoutWhileServerSleepsIsUnknownAndBounded`, `gateway_outcome_test.go:230`: the second phase, a
250 ms connector timeout against a handler that sleeps, got `UNKNOWN_REQUIRES_RECONCILIATION/invalid_response` with status 200 instead of `.../timeout`; both are the
safe UNKNOWN class, only the label differs). The full integration run failed 2 of 438: `TestM12APIDetailReadsOfALargeCandidateConverge` (503 `release_timeout`) and
`TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` (the bounded compensation scan met the 2 s statement timeout). Isolated reruns: the first passed once and
failed once, the second failed 3 of 3. A timing probe (the unchanged test body with a 60 s statement timeout, Appendix A.3) measured the scan at 2.24 s on the merged
head and 2.19 s on the pre-merge remediation tip `b00da36` on that host: not a merge regression, but the 2 s budget has only the narrow headroom the ledger itself
records (1.15 to 1.66 s on the builders' host).

Session 2: `make test-unit` **PASS** (194.6 s; `internal/tools` ran fresh and passed; 12 of 12 repetitions of the failing unit test under CPU load passed), the complete
integration suite **PASS as one run** (`-race -v`, 438 run: 433 PASS, 5 SKIP, 0 FAIL, `ok` after 1880.094 s, no data race, no panic) and the two timing-bound integration tests
pass 6 of 6 and 3 of 3 alone. The 5 skips are the opt-in retained legacy API artifact tests (`m9r_aggregate`, `m9r_choice_review`, `m9r_native_review`,
`m9r_search_review`, `m9r_unique_review`); the plan could not count them.

Interpretation: on a host about 43% slower than the builders' (2716.8 s against 1904 s) three assertions of the gate fail; on a host as fast as theirs the pinned head
passes. The plan says "each suite ran once, so the evidence says nothing about flake rates", and its integration "green" was a composite (a full run with 2 environmental
failures, a re-run of those after an environment fix, a re-run of 7 earlier failures after the test alignment); session 2 is the first single end-to-end pass of the pinned
head that I can attest. The Makefile's 90-minute deadline leaves a factor of 2 on the slow session.

Fix direction: express the timing-bound assertions as a documented multiple of a measured baseline (the 2 s statement timeout against a 2.2 s scan, the 3 s request envelope
that the large-candidate test runs inside, the 750 ms slack of the unit test) or assert on rows scanned instead of wall time; state the minimum runner speed beside the 90-minute deadline.

### IR-04 LOW - documents of the merged tree contradict the tree

Status at `be6ec7c`: **FIXED** for the statements cited, two new stale sentences listed. At `0e13abe`: **FIXED**: those sentences are gone and the ledger agrees with the tree on the points I checked. The finding below is as made at `552fa9e`.

* `docs/migration/M9R_FINDING_STATUS.md` is declared the live ledger (plan lines 59-60; the appendix header, lines 212-217: "it is not the live ledger. The
  live ledger is the table above"). In the tree that contains M10 to M14 the table above says: "no M10/main merge" (lines 17 and 30), "M10 remains paused
  pending a separate user task" (line 207), row "Production active-release runtime / public promotion lifecycle | Not implemented ... | M12/M13" (line 160),
  row "External exactly-once and AI steps | No external tool/AI implementation ..." (line 175). The plan says "No row was re-graded by the merge"; that is the
  problem, not a defence: a reader of the live ledger is told the opposite of the tree. (Line 183, "main remains M6", is true of `main` and is not a
  contradiction.) `AGENTS.md` and the contract file that points to it are acknowledged stale by the plan.
* The plan's gate table says "5 script tests"; there are 9 (`node --test scripts/*.test.mjs`).
* `make check` runs `go vet` on `GO_PACKAGES`, which does not include `./tests/integration` (build tag); the builders' reports ran
  `go vet -tags=integration ./tests/integration` separately, `make check` does not.

### IR-05 LOW - nothing refuses a database that holds the milestone line's migration numbers

Status at `be6ec7c`: **FIXED** for a real milestone database by `postgres.CheckMigrationLineage`. At `0e13abe`: **FIXED**, unchanged. The finding below is as made at `552fa9e`.

The plan says `MAIN` "has to be rebuilt, not migrated". Executed (`TestReviewMigrateOverMilestoneNumbering`, Appendix A): a database at 44 with
`goose_db_version` rows for the milestone numbers, then `migrate up`: with 45-53 the run stops at 54 with `column "aggregate_valid_v1" does not exist` and
nothing is applied; with 45-53 and 60 goose refuses ("4 missing (out-of-order) migrations lower than database version (60): versions 54,55,56,57"). It is safe,
but only because 54 happens to depend on objects of the skipped remediation migrations and 60 happens to exceed 57; there is no explicit guard (name or
checksum of the applied versions), so a milestone database that stopped at 53 and a later remediation migration that did not depend on 47-52 would be skipped
silently.

### IR-06 MEDIUM - `rollback_release` does not write the M13R-01 fence: a rollback that began before a committed rejection moves the pointer onto the rejected hash

Status at `be6ec7c`: **OPEN**, reproduced. At `0e13abe`: **FIXED** by migration `00091` (and its neighbour, the approval fence, by `00092`). The finding below is as made at `552fa9e`.

Full write-up, evidence and repro: `docs/verification/M14_INDEPENDENT_REVIEW.md`, finding M14R-04 (the item belongs to the review of migration `00088`).
In short: `db/migrations/00088_m13_review_fixes.sql:51,70` make `reject_release` and `activate_release` write the same `control.release_fences` row so that the
older snapshot aborts with 40001; `rollback_release` (`:99-128`) checks `release_rejections` for its target (`:115`) but writes no fence. Executed:
`TestReviewRollbackOvertakesConcurrentRejection` - c1 and c2 activated, a rollback transaction takes its snapshot, the other approver rejects c1 and commits,
the rollback to c1 then **succeeds** (`err=<nil> pointer=2ffe0ddc61f1@3 rejections of c1=1`; the hash differs per run).

## Gate reproduction

| Step | Plan (2026-10-20, builders' host) | Session 1 (2026-10-20, slower host) | Session 2 (2026-10-22, faster host) |
|---|---|---|---|
| `make setup`, `make infra-up`, `make migrate` | 67 migrations apply | **PASS**: 67 applied (`00001`-`00057`, `00080`-`00089`), version 89 | stack restarted, version 89 |
| `make check` | PASS | **PASS**, 49.8 s (docs checksums 84 + 43, format, `go vet`, `go mod verify`, eslint, tsc) | not repeated |
| `make test-unit` (27 Go packages `-race`, script tests, frontend tests) | PASS, 211 s | **FAIL**, 326 s: `internal/tools` `TestTimeoutWhileServerSleepsIsUnknownAndBounded` (IR-03). The target stops at the first failure, so the node and web tests ran separately: `node --test scripts/*.test.mjs` 9 PASS, `npm test` 126 PASS | **PASS**, 194.6 s: 27 packages ok (`internal/ai` 169.7 s, `internal/tools` ran fresh), 9 script tests, 126 frontend tests |
| Full integration suite, `-race`, `-skip TestM8RedisACLStreamLossAndPendingRecovery` (439 listed, 438 run) | 1904 s: 436 PASS, 2 environmental FAIL, fixed and re-run (composite) | **FAIL**, 2716.8 s: 2 of 438 (`TestM12APIDetailReadsOfALargeCandidateConverge`, `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry`) | **PASS** as one run, 1880.1 s (`-v`): 433 PASS, 5 SKIP, 0 FAIL, no data race, no panic; the 5 skips are the opt-in retained legacy API artifact tests |
| `make build` | PASS | **PASS**, 24.7 s (8 Go binaries and `vite build`, 45 modules) | not repeated |
| `make test-e2e` | 11 passed, 37 s | **PASS**, 11 passed, 35.8 s | not repeated |
| `make test-signin-e2e` | 7 passed, 9 s | **PASS**, 7 passed, 13.6 s | not repeated |
| `make test-events-fuzz` | PASS | **PASS**: `FuzzPointer` 94,352 executions in 10 s | not repeated |
| `make test-rules-fuzz` | PASS | **PASS**: `FuzzRuleCompile` 109,424; `FuzzRuleRestoreAndInputs` 1,275; `FuzzFixedDecimalMoney` 28,065 executions | not repeated |
| `make test-workflows-fuzz` | PASS | **PASS**: `FuzzWorkflowRestore` 1,064 executions | not repeated |
| `make test-event-outage` | PASS, 610 s | **PASS**, 611.3 s: real 600.003 s absence; recovery completed=3 inbox=3 effects=3 audits=3 immutable_intents=3 attempts_retained=19 unconfirmed_redeliveries=2 | not repeated |
| R7a invalid-bearer burst against `bin/api` (`-tags integration,trafficreview`) | PASS | not on my first list | **PASS** (follow-up): 40 requests, 20 x 401, 20 x 429, 20 denial audits, 129 ms |

## Attacks that held (each executed)

* **Decision 1, one meaning per version token** - `TestReviewVersionTokenReadings` (unit, compiler): from a compiled 1.5.0 tool IR, nine re-shaped artifacts were
  restored: plan 2.2.0 without `tool_contract`, plan 2.1.0 with `tool_contract`, plan 2.0.0 with tool states, IR relabelled 1.4.0 / 1.3.0 with matching document
  labels, IR 1.5.0 with the plans dropped: all refused. The restore-only legacy shape (plan 2.1.0, tool states, no contract) is accepted inside a 1.5.0 IR, as
  documented; it can only come from an artifact the compiler did not produce. `TestReviewToolActivityUnderOlderSchemas`: a document that names a tool activity
  is refused by the validator under schemas 1.0.0 to 1.4.0, and a 1.5.0 tool workflow authored with DSL 2.0.0 or 2.2.0 is refused. The validator, compiler
  restore, `workflow_plans`, `rules`, `integrity`, the renderer (`contracts.ts`), `internal/ai/schema_text.go` and `internal/promotion/dependencies.go` were
  read; the only place missed is the SQL literal of IR-01.
* **Decision 2, migration order and the shared CHECK** - the objects each set of migrations creates, replaces, alters or drops were extracted mechanically from the
  migration text (functions, tables, views, types, sequences, schemas: 45 names in 00045-00057 and 67 in 00080-00089; triggers, policies, indexes and added
  constraints with their tables: 47 and 68): the intersection is exactly `control.worker_grants_safe`, the table `control.metadata_versions` and its CHECK
  `metadata_versions_schema_version_check`, as the plan says. Every SQL literal that names an authoring schema version was listed (`grep` over the migrations):
  the five CHECK rewrites (`00017`, `00020`, `00045`, `00052`, `00080`) and `00052:145` (IR-01); no other. The last CHECK (`00080:8-10`) lists 1.0.0 to 1.5.0. A scan of
  the bodies of every function of the four guarded schemas for `control|runtime|iam|audit.<name>` references (830 references, 117 distinct names, a read-only catalog query) found 0 that resolve to no relation, function or
  type of the merged database.
* **Decision 3, derived worker guard** - evaluated as a real `framework_worker` session on the merged schema: `control.worker_grants_safe()` is true (it
  evaluates `session_user`, `00087:244-253`; the migrator session correctly gets false and the dispatcher has no EXECUTE; all 60 rows of the allowlist
  `worker_guard_granted()` are really held by the worker: 0 stale entries, checked with `has_table_privilege`/`has_column_privilege`); the worker holds table-level
  `SELECT` only on 16 listed relations; no sequence privilege; no default ACL; no role is member of another except `framework_migrator` of
  `framework_schema_owner`; all 103 SECURITY DEFINER functions of the four schemas pin their `search_path` (99 to `pg_catalog, pg_temp`; 4 - the R7b `claim_event`,
  `finish_event`, `event_delivery_policy`, `worker_authority` - to `pg_catalog` alone, which searches the temporary schema first; no runtime role holds TEMP on the
  database, so this is a hardening note, not an exposure), none is owned by a role other than `framework_schema_owner`, no function of the four schemas is
  executable by PUBLIC; the dispatcher can execute 5 functions (`claim_event`, `claim_workflow`, `event_audience`, `event_delivery_policy`, `finish_event`) and the worker 26
  (worker authority and event ports, the three M10 ledger ports `tool_begin`, `tool_finish`, `tool_abandon`, identity helpers): none of the index, unique, release, approval,
  candidate or AI ports. 71 of 72 tables have RLS enabled and forced (`iam.roles` is a global name catalog with no runtime grant).
* **Decision 4, R7b cutover** - `TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts`: a database at 56 with a live old reader of
  `runtime.event_envelopes`; `migrate up` stops at 57 (`could not obtain lock on relation "runtime.event_envelopes"`, 55P03), the tool ledger and the M13 fences
  are not installed, the database stays at 56; after the reader ends the same call applies 57 and 80-89 and the worker guard is true. `RuntimeSnapshot` is
  certified only when `isolation == REPEATABLE READ` (`postgres/transaction.go`); the only READ COMMITTED callers are event jobs (`platform/jobs/jobs.go`) and
  the sandbox freeze route (`access.go: freezePattern`).
* **Decision 5, front door** - `TestReviewRouteMatrix`: 45 POST routes of both lines, one distinct peer per anonymous call: every one answers 401 without a
  token; viewer and member get 403 `forbidden` on every route that must deny them (74 denied calls: datasets, evolutions, sandboxes, requirements, acceptance,
  candidates, approve/reject/activate/rollback, the five AI POST routes, indexes, search indexes, unique constraints, roles, teams, metadata) and every denial
  wrote an audit row (90 denial rows); the read-like routes (query, evaluate) and the commands/record-access routes behave as designed. The POST allowlist
  `AcceptsPOST` is the union of the remediation list and `sandboxPOST`, `releasePOST`, `aiPOST` (diff against `b00da36`). The write-deadline extension: a standalone
  server on Go 1.27.2 (`WriteTimeout` 2 s) shows that a handler's `SetWriteDeadline(+30 s)` does **not** leak to the next request on the same keep-alive connection (the
  next request is cut at 2 s), and that `ReadTimeout` does not cancel a running handler's context (the read deadline is cleared when the background reader starts). With
  `WriteTimeout` 0 there is no deadline at all. The invalid-bearer budget (R7a, 20 then 429 per peer) tripped when a first version of the matrix reused one peer; with one
  peer per route it did not, as designed.
* **SSRF forms** - 58 address forms (IPv4-mapped, IPv4-compatible, NAT64 `64:ff9b::/96`, 6to4, Teredo, ISATAP, metadata 169.254.169.254 / 168.63.129.16 /
  fd00:ec2::254, CGNAT, documentation ranges) are all denied by `tools.DeniedAddress`, which the AI provider dials through; public controls are allowed
  (`TestReviewDeniedAddressForms`).

## Follow-up: items first marked NOT RUN, now run (2026-10-22; the tree is unchanged: no file differs from the pinned head except the two reports)

* **R7a invalid-bearer burst against the built API** (`TestM9RTrafficReviewPinnedBinaryInvalidBurst`, `-tags 'integration trafficreview'`, `bin/api` built from the pinned
  source on 2026-10-20, sha256 `976230584ce208179cd1344fbade024e25704d8e537eaa157259af50d330e19a`): **PASS** - 40 actual HTTP requests, 20 x 401 and 20 x 429, 20 matching
  denial audits, 0 rejected-request audits, 129 ms. This is the plan's own number reproduced.
* **Frozen sandbox records stay evidence** (`TestReviewFrozenSandboxRecordsStayEvidence`, Appendix A.9): after the freeze, the stored evidence (2 records, hash `9573a4a98e37...`)
  equals a recomputation from the live rows; API create, update and delete on the frozen binding answer 409; a SQL `DELETE` and a SQL `UPDATE` of a record as the
  application credential are refused with 55000 `dataset binding is stale or sealed` (`dataset_record_fence`); after all five attempts the recomputed hash is unchanged. **PASS.**
* **`control.strict_candidate_json` differential** (`TestReviewStrictCandidateJSONDifferential`, Appendix A.10, `00087:50-54`): 200,000 generated bodies (nested objects and
  arrays, keys spelled with escapes, surrogate pairs, empty and slash keys, 20 number spellings with exponents, strings that contain objects, six whitespace variants,
  leading and trailing whitespace) were checked by the function and by a reference built on a real tokenizer (no duplicate member by decoded name at any depth, no exponent,
  no surrounding whitespace). They agree on every body: 86,716 accepted by both, 0 accepted by the function only (holes), 0 refused by the function only (over-strict). **PASS.**

## Re-verification at `be6ec7c` (2026-10-22)

After the interim findings the builders moved `integration/m9r-m14` from the pinned `552fa9e` to `be6ec7cae1849bf535bad1811b21bf93067598b5` (`git log 552fa9e..be6ec7c`: `46d8a2a`, `dfec052`,
`f997efe`, `6c4cee6`, `1fb0110`, `bbf2465`, `f6398bf` and the document commits). I repeated the gate on that commit in a separate worktree and attacked the fixes (probe files copied in,
run, deleted; no product file was changed). The change set is small and was enumerated mechanically (`git diff --name-only 552fa9e be6ec7c`):

* product Go files: `cmd/migrate/main.go`, `internal/compiler/restore.go`, `internal/metadata/models.go` (a comment), `internal/platform/access/access.go`,
  `internal/platform/postgres/lineage.go`; besides them `scripts/runtime-env.mjs`, one `Makefile` line (`:65`) and the migration `00090`;
* `internal/ai`, `internal/verification`, `internal/intent`, `internal/promotion`, `internal/audit`, `internal/runtime`, `internal/policy`, `internal/identity`,
  `internal/tenant`, `internal/sandbox` and the migrations `00001` to `00089`: **no file differs** from the pinned commit. A held attack that touches only those holds by code identity;
  the probes that cross a changed file were run again (below).

### Status of the findings

| Finding | At `552fa9e` | At `be6ec7c` | Deciding attack, executed at `be6ec7c` |
|---|---|---|---|
| IR-01 MEDIUM | open | **FIXED** | `TestReviewUniqueIsEnforcedUnderSchema15` (A.11): schemas 1.4.0 and 1.5.0 both register, build to `READY`, then accept the first value (200), refuse the duplicate (409) and accept an absent field (200) |
| IR-02 MEDIUM | open | **FIXED for the database slots** (corrected in the third pass: the change opened F-01, a memory lever) | `TestReviewSlowBodyHoldsAdmissionSlots` (A.2, the probe of the pinned commit, unchanged): with four trickling bodies the honest `GET` takes 10 ms (8.02 s at `552fa9e`) and the honest AI proposal answers 201 `READY` in 1.53 s (baseline 1.55 s; at `552fa9e` 503 after 9.54 s and `PENDING`); `TestReviewSlowBodiesAfterTheFix` (A.11): six kinds of attacker, six connections each, 14 s each: honest `GET` 10-41 ms and honest AI proposal 201 in 530-563 ms every time |
| IR-03 LOW | open | **PARTLY FIXED**: two of the three timing-bound assertions were repaired in the tests, the third is unchanged by design | the full run (`TestM12APIDetailReadsOfALargeCandidateConverge` 9.59 s, `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` 6.10 s, `internal/tools` ok in the unit run) and a load experiment (A.12; a busy loop on one of the two CPUs the test may use): (a) `TestM12APIDetailReadsOfALargeCandidateConverge`: pinned tree 0 of 3 failed (11/11/12 s), `be6ec7c` 0 of 3 failed (11/10/10 s); (b) `TestTimeoutWhileServerSleepsIsUnknownAndBounded` x300: pinned tree 33 failing executions, `be6ec7c` 0; (c) `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` (unchanged by the builders) at `be6ec7c`: 0 of 3 failed (8/8/8 s). (a): no failure at the pinned tree either (0 of 3), so this load does not reproduce the defect and shows only that `be6ec7c` does not fail under it; (b): the load reproduced the failure at the pinned tree (33 of 300) and not at `be6ec7c` (0 of 300), so the repair works under it |
| IR-04 LOW | open | **FIXED** for the statements cited (live ledger, script-test count, `make check`); two sentences that name `552fa9e` as the tip remain | reading against the tree; `make check` (below) |
| IR-05 LOW | open | **FIXED** for a real milestone database; the guard is a heuristic | `TestReviewRealMilestoneDatabaseIsRefusedByTheMigrator` (A.11), `TestReviewMigrateOverMilestoneNumbering` (A.4) |
| IR-06 MEDIUM | open | **OPEN, unchanged** | `TestReviewRollbackOvertakesConcurrentRejection` (A.2 of the M14 report) fails as before: the rollback succeeds onto the hash that a committed rejection had just refused |

### The fixes, attacked beyond the builders' tests

* **IR-01, migration `00090`.** The new function text was compared with the only earlier definition (`00052`, `CREATE FUNCTION runtime.register_unique_constraint`, 29 lines): after
  `CREATE` became `CREATE OR REPLACE` exactly one line differs, the predicate (`m.schema_version='1.4.0'` against `m.schema_version IN('1.4.0','1.5.0')`, `00090:24`), as the file's
  header says ("byte for byte except that predicate"). `register_unique_constraint` appears in `00052` and `00090` only, so `00090` replaces the latest definition and reverts nothing.
  Its closing block refuses a changed owner (`:51`), a drifted ACL (`:54`) and any function of the four schemas whose text still matches `schema_version = '1.4.0'` (`:57`; that pattern does not see
  other spellings such as `->>'schema_version'`, but the list I made of every SQL literal naming an authoring version at the pinned head found none, and `00090` adds none).
  The lifecycle probe uses the builders' own fixture (`uniqueFixtureAt`) but asserts the behaviour that matters, which is uniqueness: under 1.4.0 and under 1.5.0 the claim is registered
  (200) and built to `READY`, the first value is then accepted (200), the same value again is refused (409) and a record without the field is accepted (200).
* **IR-02, the body pre-read.** Read: `access.go:293` (`preRead` only for a POST with a verified bearer, a body announced, and not one of the two routes that already buffer),
  `:296` (`io.ReadAll(io.LimitReader(r.Body, 1<<20+1))` on a body that `httpserver.go:180` has already wrapped in a 1 MiB `MaxBytesReader`, so the 1 MiB error arrives as a
  `*http.MaxBytesError`), `:297-301` (any other read error is 400 `invalid_request` before the slot), `:314-315` (the handler reads a replay that ends with the same error). There is no PUT, PATCH or DELETE
  route in the non-test code (`grep` over `internal` and `cmd`), and the R7a gate is a rate (token-bucket) limiter, not a concurrency limit (`traffic/gate.go:45-67`), so a slow body holds a connection and up to 1 MiB per connection (the number of connections is not bounded: see the correction in the third pass), but no database slot
  and no gate slot. Executed (A.11): six kinds of slow attacker, six connections each for 14 s against a real `httpserver.New` server with the production timeouts and four slots: an owner
  trickling a `Content-Length` body to the AI route; the same to the metadata route; a viewer (whose request is denied only after the read); an owner trickling a chunked body;
  an invalid token; an owner who declares 2 MiB, sends 1.2 MiB at once and stops. Results: honest `GET` 10, 14, 41, 13, 24, 17 ms and honest AI proposal 201 after 545, 530, 551, 550, 561, 563 ms (the scripted provider takes 500 ms), in the order of the kinds above; the sixth kind was answered 413 by the server 3,863 times; the six proposals made during the test are all `READY`. The probe of the pinned commit (A.2) on the same head: 10 ms, and 201 in 1.53 s with both proposals `READY` and 0 idle-in-transaction backends at +2 s.
* **IR-05, `postgres.CheckMigrationLineage`** (`lineage.go:19-37`, called at `cmd/migrate/main.go:35` before the lock and the provider). The migrator exposes `up` and `status` only
  (`:20`), both pass through the check. Executed with the real milestone line: its original `00045`-`00053` and `00060` (extracted from the milestone line's last commit, `60da6f2`) were applied
  with goose on top of a database at 44, then `bin/migrate up` and `bin/migrate status` were run: both exit non-zero with `ErrForeignMigrationNumbering`, the database is untouched
  (version 60). The heuristic is "`runtime.tool_invocations` exists and goose version 80 is not recorded as applied"; it cannot see a milestone database that never reached `00045` (it is
  identical to the remediation line below 45, so migrating it is correct), and it says nothing about a database that holds a different foreign numbering.
* **B-02, the restore pin** (`restore.go:50`): the probe of decision 1 was run again (A.7, unit). The control restores; seven of the nine re-shaped artifacts are refused with the same messages as at the pinned commit ("stored workflow model disagrees with application", "invalid stored integrity contract", "stored runtime model disagrees with application"); the other two are the restore-only legacy shapes, which restore, as documented; `TestReviewToolActivityUnderOlderSchemas` PASS.

### Gate at `be6ec7c`

| Step | Builders' claim (`LINE_INTEGRATION_PLAN.md:27-35`) | Result at `be6ec7c` |
|---|---|---|
| `make check` | PASS, 30 s | **PASS**, 90.2 s: docs checksums 84 + 43, format, `go vet` of the packages and of `./tests/integration` with the `integration,performance` tags (new, `Makefile:65`), `go mod verify`, eslint, tsc (cold build cache in the new worktree) |
| `make test-unit` | PASS, 209 s | **PASS**, 237.8 s: 27 Go packages `ok` (`internal/ai` 171.0 s), 11 script tests, 126 frontend tests in 19 files |
| `make build` | PASS, 24 s | **PASS**, 16.9 s: 8 Go binaries and `vite build` (45 modules) |
| Full integration suite, `-race -v`, `-skip TestM8RedisACLStreamLossAndPendingRecovery` | PASS, 1956.5 s; 443 listed, 442 run | **PASS** as one run, 1854.972 s: 437 PASS, 5 SKIP (the opt-in retained legacy artifact tests), 0 FAIL; 442 run of the 443 that `-list` reports (the excluded test is the other one); no data race, no panic. The tests the builders added for the fixes pass (`TestM14SlowRequestBodiesDoNotHoldDatabaseSlots` 5.14 s, `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema` 6.03 s, `TestMigrationLineageRefusesTheMilestoneLinesOldNumbering` 1.97 s, `TestIntegrationTenantRoleApproverGatesAToolSend` 17.42 s), and so do the two timing-bound ones (`TestM12APIDetailReadsOfALargeCandidateConverge` 9.59 s, `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` 6.10 s) |
| `make test-e2e`, `make test-signin-e2e` | 11 passed, 7 passed | **PASS**, 11 passed (33.5 s); **PASS**, 7 passed (9.8 s) |
| `make test-rules-fuzz test-events-fuzz test-workflows-fuzz` | PASS (5 targets of 10 s) | **PASS**: `FuzzPointer` 147,099, `FuzzRuleCompile` 248,805, `FuzzRuleRestoreAndInputs` 19,098, `FuzzFixedDecimalMoney` 12,164, `FuzzWorkflowRestore` 181,951 executions |
| `make test-event-outage` | PASS, 610 s | **PASS**, 608.60 s: real_absence_s=600.000 total_elapsed_s=606.378 completed=3 inbox=3 effects=3 audits=3 immutable_intents=3 attempts_retained=19 unconfirmed_redeliveries=2 |
| The SQL version-gate scan without `00090` (the builders' negative control) | "fails without 00090" | **confirmed**: with the file moved away `TestSQLFunctionsNameWholeSchemaVersionFamilies` fails ("runtime.register_unique_constraint does not accept schema 1.5.0"); with it the three tests pass |

### What the fixes do not do (so that nobody reads them as more)

* IR-02 moves the wait, it does not remove it: a slow body still occupies a connection and up to 1 MiB until the 10 s read timeout, bounded by the rate limits and not by a connection limit
  (the plan says so, `LINE_INTEGRATION_PLAN.md:41-46`). **I did not measure that residual; the second review did, and it is a MEDIUM (F-01): see the correction in the third pass.** An unreadable body is answered 400 before the transaction and writes no audit row.
* IR-03: the worker-policy assertion (`TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry`, a 2 s statement budget against a compensation scan that took 2.2 s on the slower host of session 1) is unchanged on purpose (the plan gives it to D1); it passed here in 6.10 s as part of the full run and in 3 of 3 runs under the load of A.12. That load slows the Go test process only, not the PostgreSQL container in which the scan and most of the set-up run, so it is a weak model of the slower host of session 1; it does not show that the assertion became robust, and a variant with a CPU-limited database container was **NOT RUN**. No minimum runner speed is stated beside the 90-minute deadline (`Makefile:41`; the plan says the deadlines "leave room for a slower runner", `LINE_INTEGRATION_PLAN.md:211`). The host of this re-verification was about as fast as the builders' (1855.0 s against their 1956.5 s), so the full run says nothing new about a slower one.
* IR-04: the live ledger section (`M9R_FINDING_STATUS.md:185`, "Live findings - merged preview") now says that M10 and M14 are implemented preview and that the M12/M13 control plane exists; the
  checkpoint (`:3-34`) names the tree as the combination of remediation and M10-M14. The statements of the old state (`no M10/main merge`, `Not implemented`, `AI is not implemented`) remain at
  `:56`, `:69`, `:312`, `:322`, under the headings "Historical checkpoint progression" (`:36`, which declares its M10 pause and tool/AI wording historical) and "Appendix: milestone-line audit"
  (`:251`, "it is not the live ledger"); a reader who jumps to the appendix table sees dated rows. What is left is new and of the same class: two sentences written when the tip was `552fa9e` are false at
  `be6ec7c`, one in the live part (`M9R_FINDING_STATUS.md:224`, "integration/m9r-m14 remains `552fa9e`") and one in a section dated 20 October (`LINE_INTEGRATION_PLAN.md:85-87`, "Integration still
  points to `552fa9e`"); `LINE_INTEGRATION_PLAN.md:103` ("M14/merge independent review is NOT RUN") is superseded by these reports.
* IR-05: the check protects `cmd/migrate`; a person who runs goose directly against a milestone database is not stopped.
* IR-06 and the findings of the M14 report are not fixed by any commit after the pinned one (the IR-02 fix removes one of the five triggers of M14R-03, nothing else).

## Third pass at `0e13abe` (2026-10-22)

While the previous section was being written the integration branch moved again: 37 commits after `be6ec7c` (`git log be6ec7c..0e13abe`). They contain the builders' fixes of IR-06 and of the M14
findings (migration `00091`, changes in `internal/ai` and in the AI routes), a second migration (`00092`, the approval fence), a body budget on the request path (`bodybudget.go`), a rewrite of the
compensation scan and many documents. A second independent review (`docs/verification/M14_AND_FIXES_INDEPENDENT_REVIEW.md`, branch `review/m14-and-fixes`) had re-verified part of this work and found F-01 to
F-08. The builders gated `507c12c`; the tip differs from it by documents and by one comment line of `00091` (`git diff 507c12c 0e13abe -- . ':!docs'` prints that line only). I repeated the gate on the tip, in
a new worktree, and attacked the new code with probes of my own (A.13 to A.18).

### Correction of the IR-02 verdict of the previous section

"IR-02 FIXED" was right for the database slots and wrong as a verdict on the fix. The pre-read buffers every verified-bearer POST body, up to the front door's 1 MiB, before identity resolution, membership and
the route's own cap (`access.go:293-302` at `be6ec7c`). The second review measured what that costs: 1.14 MiB per connection held until the 10 s read timeout, 1,000 connections 1.15 GiB, for any signed token and
without a membership (F-01, MEDIUM), where the AI routes cap their bodies at 4 to 128 KiB. I had read the same lines and wrote the cost down as a residual ("bounded by the rate limits"); I did not measure it,
and the rate limits do not bound it (the default gate admits about 3,600 requests per peer within one read timeout, by the second review's count). It also found that the sentence the fix rests on, "only a verified bearer is read, so
an anonymous caller cannot make the server buffer anything", was false on four routes: the two commands routes and the two content-idempotent release POSTs read their body before authentication (F-02, LOW; the same on the
code before the fix, bounded by the invalid-bearer budget). I had listed those routes as "the routes that already buffer" and did not ask whether they read before authentication. The status of IR-02 at `be6ec7c` is
therefore: fixed for the database slots, with a MEDIUM memory lever (F-01) opened by the change and a LOW read of anonymous callers (F-02) that the change did not touch; both were found by the second review, not by me.

### Status of the findings at `0e13abe`

| Finding | At `552fa9e` | At `be6ec7c` | At `0e13abe` | Deciding attack, executed at `0e13abe` |
|---|---|---|---|---|
| IR-01 MEDIUM | open | FIXED | **FIXED** | `TestReviewUniqueFieldUnderEachSchemaVersion` (A.1) and `TestReviewUniqueIsEnforcedUnderSchema15` (A.11): under 1.4.0 and under 1.5.0 a write to the unique field is refused before the registration (409), the registration answers 200 `BUILDING`, the build reaches `READY`, then the first value is accepted (200), the same value refused (409) and a record without the field accepted (200) |
| IR-02 MEDIUM | open | FIXED for the database slots (corrected above) | **FIXED** with the body budget | the unchanged probe of the pinned commit (A.2): four trickling bodies, honest GET 10 ms, honest AI proposal 201 in 1.53 s (baseline 1.55 s), both proposals `READY`; A.11: five of six kinds hold, the sixth is explained below; the budget: A.13, A.14 |
| IR-03 LOW | open | partly fixed | **FIXED** (slow-database variant NOT RUN) | the compensation statement 1.22-1.35 s -> 32-36 ms (A.15), the same rows on 4,000 random receipt sets (A.13); the worker-policy test took 3.24 s in the full run |
| IR-04 LOW | open | FIXED for the statements cited | **FIXED** | reading against the tree: the two sentences that named `552fa9e` as the tip now read "has carried the fixes ... since" (`M9R_FINDING_STATUS.md:235`, `LINE_INTEGRATION_PLAN.md:112-114`); `LINE_INTEGRATION_PLAN.md` no longer says that the review "is NOT RUN"; `make check` passes |
| IR-05 LOW | open | FIXED | **FIXED** | `TestReviewRealMilestoneDatabaseIsRefusedByTheMigrator` (A.11): with the milestone line's own `00045`-`00053` and `00060` applied (version 60) `migrate up` and `migrate status` exit 1 with the lineage message and the database is untouched; `TestReviewMigrateOverMilestoneNumbering` (A.4) |
| IR-06 MEDIUM | open | OPEN | **FIXED** | `TestReviewRollbackOvertakesConcurrentRejection` (A.2 of the M14 report) passes: the rollback after a committed rejection of its target fails with 40001 and the pointer stays; the randomized stress of the four release ports (A.13): 168 rounds, 0 violations, no hang |
| IR-07 LOW (new) | - | - | **OPEN** | the launcher hands the whole environment to a command it does not recognise: 12 of 16 fringe spellings (A.16) |
| IR-08 LOW (new) | - | - | **OPEN** | the static scan for single-version gates does not see 10 of 21 further spellings (A.17) |

The table is about the findings of this report. The residuals of the fixes and the findings of the M14 report are in the sections below and in the M14 report.

### The new code, attacked

* **Body budget, accounting (IR-02, F-01).** `TestReviewBodyBudgetAccountingReturnsToZero` (A.13): 700 requests in nine kinds of ending - the whole body and the answer read; half of the body and the client leaves; the whole body and the client leaves
  without reading; `Expect: 100-continue`; a chunked body with the client leaving in the middle of a chunk; a declared body and nothing sent; a third of the body and silence; a body above the front door's 1 MiB; a valid-looking JSON body - from six kinds of caller
  (the owner, a viewer, two signed identities without a membership, no bearer, an invalid bearer) on six routes, against a budget of 8 MiB in all and 3 MiB per identity. The peak of the booked bytes was 7.91 MiB (never above the total) and
  after the last request every booking was back (`InFlight()` 0 bytes, 0 identities, polled for up to 25 s). Answers: 100:56 400:51 401:61 403:24 413:8 429:102 503:6 (100 is the interim `100 Continue` of the `Expect` kind). The code: `bodybudget.go` books the bytes read (`bookedBody.Read`), `access.go:294-295` takes the hold and
  releases it with a `defer`, so no ending can leak a booking.
* **Body budget, a full budget (F-01, F-06, and the residual).** `TestReviewBodyBudgetRefusesWhenFull` (A.14): 16 signed identities with two connections each send 1 MiB minus one byte of a declared 1 MiB (32.0 MiB in 1.59 s, one peer address) and stop. The budget holds
  33,554,400 of 33,554,432 bytes for 16 identities and the heap of the API grew by 36.4 MiB (the memory in flight is bounded by the total whatever the number of connections). While it is held, a POST of the tenant owner, of an admin of the same tenant and of an admin of
  another tenant is refused at once (503 `service_busy`, 0-1 ms, `access.go:305`) and a GET is served (200). The first honest POST is admitted again 10.2 s after the attack began: the server's 10 s read timeout ends the 32 connections and the bookings go back
  (`TestReviewBudgetIsReleasedAtTheReadTimeout`: 10.06 s: 0 bytes, 0 identities; all 32 connections ended at 10.06 s, 8 answered 400 and 24 closed without a response; the next honest POST, which that probe sends once a second, was admitted at 11.00 s). To keep the denial the attacker
  has to transmit 32.0 MiB per read timeout, 26.8 Mbit/s, from at least 16 identities. That is the bandwidth-bound residual that the builders state (`LINE_INTEGRATION_PLAN.md:44`); I reproduced it from one peer address with signed identities that have no membership.
  What it costs while it lasts: every body-carrying request of every tenant is refused with a `Retry-After`; reads are not affected. Memory is no longer the lever; availability of POSTs is, for as long as the attacker keeps sending (LOW, documented). The grade assumes that the API is not the first thing a slow client meets: a buffering reverse proxy in front of it holds the slow bodies itself and delivers only complete ones, and I did not run one; exposed directly, this is a platform-wide outage of writes that costs the attacker one home connection.
* **Declared and unsent bodies book nothing (F-06 of the second review).** `TestReviewDeclaredButUnsentBodiesBookNothing` (A.14): 64 connections of 16 signed identities declare 1 MiB each and send nothing; after 1.5 s the budget holds 0 bytes for 0 identities, and
  an honest owner's proposal is served (201 after 350 ms) while they stay open. The first version of the budget booked the declared length; the second review showed that eight identities without a membership could fill it that way.
* **An anonymous caller is never read (F-02 of the second review).** `TestReviewAnonymousCallersAreNotReadOnAnyRoute` (A.14): the four routes that read their body before the bearer was looked at on `be6ec7c` (the two commands routes, `POST .../candidates`,
  `POST .../acceptance-specs`) and the AI route as a control, each with no bearer and with an invalid bearer, declaring 900,000 bytes and sending one: 10 of 10 answered 401 `unauthenticated` in 4-7 ms (the second review measured 10.03 s on `be6ec7c` for candidates, acceptance-specs and commands). `access.go:289`
  reads only when `preError == nil`, that is for a bearer that verified.
* **Isolation between peers and identities, and the limits that refuse.** `TestReviewBudgetIsolationAcrossPeersAndIdentities` (A.14), run eight times (the code of each refusal is logged in the last four): six connections of the tenant owner flood the AI route for 14 s from 127.0.0.1 with 1.2 MiB bodies that declare 2 MiB. They are answered 413 `body_too_large`,
  429 `too_many_uploads` (his own share of the body budget is full) and, when the flood outruns the bucket of R7a, 429 `rate_limited` (in the run quoted from the final batch: 1,208, 3,589 and 15,499 times); the booked bytes were back to zero afterwards in every run. Honest callers from a second peer address (127.0.0.2): the viewer's GET and the admin's proposal were served in 8 of 8 runs
  (GET 200 in 19-55 ms; proposal 201 in 385-1,711 ms). The attacked owner's own proposal, from the second address, was served in 3 of 8 runs, and every one of the 3 refusals whose code was logged is 429 `too_many_uploads`: the owner's share (`DefaultBodyBudgetPerUser`, 2 MiB) is held by his own six connections,
  wherever the honest request comes from. Callers behind the attackers' address were served in 9 of 16 requests; every one of the 3 refusals whose code was logged is 429 `rate_limited`, the per-peer bucket of R7a (`traffic/gate.go:25-27`: 300 per second, burst 600), which empties when a flood outruns it and then refuses everybody behind the address. Both limits work as designed
  - the identity is the unit of fairness of the budget and the address the unit of the rate gate - and neither touched an honest identity on another address. What it means for a deployment: a reverse proxy that puts every client behind one address makes the per-peer bucket global (not run, below).
* **Slow bodies (IR-02) at the tip.** The unchanged probe of the pinned commit (A.2) and the six kinds of A.11 (six connections each, 14 s each, the real `httpserver.New` with its 10 s timeouts, four database slots). The first five kinds hold: honest GET 9-13 ms and
  honest proposal 201 in 530-544 ms (the scripted provider takes 500 ms). The sixth kind (the owner declares 2 MiB, sends 1.2 MiB at once and reconnects) **fails** at the tip, in both runs I made, because the attacker is the identity whose honest proposal is measured: that proposal is refused 429 (`too_many_uploads` in the run that logged the code: the owner's share of
  the body budget is held by his own six connections). In the first run the honest GET of a viewer was refused 429 too; a GET carries no body and the viewer is not the flooding identity, so it was the per-peer bucket of R7a (`serve` checks it first, `access.go:243`): the attackers were answered 972 times 413 and 13,398 times 429 in 14 s, about 1,026 per second against a bucket that refills 300 per second; in the second run they were answered 5,172 times, 369 per second, and the GET was served (200 after 80 ms).
  At `be6ec7c` (previous pass, not run again on this host) the same attack ended with both honest requests served, and was answered 3,863 times in 14 s, all 413; there was no per-identity share then, which is the lever that F-01 found. I did not establish why the attackers' raw rate varies between 369 and 1,026 per second. The failure of this kind is the budget and the gate working as designed against the identity that floods; I do not count it as a defect, and the probe stays as written.
* **Release fences (IR-06, F-05).** `00091` differs from the latest definition of `rollback_release` (`00088`) by four lines: three of comment and `INSERT INTO control.release_fences ... ON CONFLICT ... DO UPDATE SET writes=f.writes+1` before the rejection is read, as `activate_release` does. `00092` replaces `approve_release` of `00084`:
  the pointer row is read `FOR SHARE` first (the lock order of activation and rollback) and the fence row is written `ON CONFLICT DO NOTHING`; every candidate gets its fence row when it is staged (trigger `release_candidate_fence`, `00092:25-34`) and the existing ones in the migration, and the closing block refuses a drifted owner or ACL (`00092:77-96`).
  Executed: `TestReviewRollbackOvertakesConcurrentRejection` now fails the rollback with 40001 and the pointer stays; `TestReviewReleaseFenceStress` (A.13) per round stages a candidate and its neighbour, pre-approves one at random, and calls approve (two approvers), reject (the candidate and the rollback target), activate and rollback at random offsets of 0-25 ms,
  half of them on a transaction that took its snapshot 0-25 ms before it calls the port. A violation is an undocumented SQLSTATE (anything but 40001, 40P01, 55000, 55P03, 42501), a call above 10 s, a round above 30 s (a hang or an undetected deadlock), an approval, activation or rollback of a hash that succeeded after a rejection of that hash had been acknowledged
  before the call began, a pointer resting on such a hash, or an inconsistent history. 168 rounds in two runs (18 rounds with seed 20261022, 150 rounds with seed 424242): 0 violations, no 40P01, no hang. Outcomes of the 150 rounds: activate 27 x 40001,
  55 x 55000, 35 ok; approve 46 x 40001, 55 x 55000, 118 ok; reject 16 x 40001, 183 ok;
  rollback 11 x 40001, 44 x 55000, 17 ok. The builders' own tests pass in the full run (`TestF05ApprovalOvertakenByACommittedRejection`, `...ByAnActivationOfAnotherCandidate`, `...ByARollback`, `TestF05ApprovalsStillRunTogether`,
  `TestF05EveryCandidateHasItsFenceRow`, `TestM13R01RollbackOvertakenByACommittedRejection`, `TestM13AdversarialRaceRejectVersusApprove`). The stress starts after two activations, so a first activation racing an approval of another first candidate (the residual that the builders name for F-05) is **NOT RUN**.
* **The compensation scan (IR-03, D1).** `CompensationScanSQL` (`execution.go:273-281`) replaces the correlated `NOT EXISTS` by an uncorrelated `NOT IN` over the instance's receipts. `step_key` is `NOT NULL` (`00020_m9_workflow_runtime.sql:52`, so `NOT IN` has no NULL trap, as the code comment says) and both statements filter the same tenant and instance.
  `TestReviewCompensationScanEquivalence` (A.13) runs the old and the new statement on 4,000 random receipt sets in a temporary table that has the constraints of `runtime.workflow_receipts` that matter (`step_key text NOT NULL`, primary key tenant/instance/step key): up to 10 receipts per set, step keys from an adversarial list (`a`, `a:compensate`,
  `a:compensate:compensate`, `A`, `é`, `a `, ` a`, generated keys, a third of them with `:compensate` appended), sequences 0-5, compensation references empty, `local.noop` or `x`; 3,191 of the sets have a pending compensation. The two statements return the same row, or none, on all 4,000 (the logic only: the temporary table has no row-level policy). `TestReviewWorkerPolicyScanTimingOldVsNew` (A.15), the same 96 receipts in the worker session: the old statement
  1.347, 1.216, 1.215 s; the new 31.7, 32.0, 35.6 ms.
* **Repeated AI exchanges store and charge once; abandoned provider calls are bounded (M14R-03, M14R-07).** `TestReviewAIRetryIsExactlyOnce` (A.13): one sequential proposal first (the charge of one finished proposal: 5,794 tokens), then eight concurrent creations of one tenant whose provider phases end together, three rounds: 23 of 24 answered 201 and
  1 answered 409 `ai_conflict` at admission (nothing stored, nothing charged; in the three earlier runs of this probe all 24 of 24 answered 201); no proposal stayed `PENDING`, no provider call was repeated, and the tokens charged were exactly the number of finished proposals times 5,794. `TestReviewAbandonedCallsAreBoundedAndReleased` (A.14):
  an adapter that never returns, wall clock 1 s, 20 requests: 16 reached the adapter, 4 were refused without reaching it, all 20 answered 503 `ai_unavailable` (the 20 took 16.602 s); after the adapters were released the next request reached the adapter again (call 17), so the count falls back.
* **R7a on the binary of the tip.** `TestM9RTrafficReviewPinnedBinaryInvalidBurst` against `bin/api` built from the tip (sha256 `a16aa077d0d4...`): 40 actual HTTP requests: 401=20 429=20 matching denied audits=20 rejected audits=0 elapsed=87ms.
* **Migration 00091 and 00092 against the guard (decision 3).** The inventory of A.18 on a database migrated to the tip: 104 SECURITY DEFINER functions in the four schemas (103 at the pinned commit, plus `release_candidate_fence`), 4 that pin `pg_catalog` alone (`worker_authority`, `event_delivery_policy`, `claim_event`, `finish_event`; as before, no runtime role holds TEMP),
  0 not owned by `framework_schema_owner`, 0 executable by PUBLIC; `approve_release` and `rollback_release` are executable by the application role only, `release_candidate_fence` by nobody; the worker holds table-level privileges on 16 relations and may execute 26 functions, the dispatcher 5; the 60 rows of `worker_guard_granted()` are all held (0 stale); 833 internal references, 117 distinct names, 0 unresolved.
* **The migrator's output (D2).** `bin/migrate up` and `status` against an unreachable database with a made-up user, password and host: both print only "migration database unavailable" (0 occurrences of the user, password, host or database name); the refusal of a milestone database prints the lineage message (above). `cmd/migrate/main.go:47-52` prints the provider's text only for a failure that cannot involve the database.
* **Attacks of the earlier passes, run again against the tip and still held:** the route matrix (45 POST routes, 74 denied calls, 90 denial rows); `TestReviewStrictCandidateJSONDifferential` (200,000 generated texts, 86,716 accepted by the function and by the reference, 0 holes, 0 over-strict); `TestReviewFrozenSandboxRecordsStayEvidence`; `TestReviewHoldsVsDecideExtended` (7 role shapes);
  `TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts`; `TestReviewVersionTokenReadings` and `TestReviewToolActivityUnderOlderSchemas`; the SSRF and dial-path unit probes; the two fuzz probes (259,626 and 661,147 executions in 60 s each, no input found); `TestReviewMigrateOverMilestoneNumbering`; the 58 address forms.

### New findings of the third pass

#### IR-07 LOW - the launcher hands the whole environment to any command it does not recognise

`scripts/with-env.mjs:15-16` starts the child with `runtimeKind ? runtimeEnv(env, runtimeKind) : env`: a command that `runtimeKindOf` (`scripts/runtime-env.mjs:7-15`) does not name as a backend gets the whole environment, `.env` included, and that environment holds `MIGRATION_DATABASE_URL`,
`POSTGRES_PASSWORD` and `MIGRATOR_PASSWORD` (the second review's F-04 showed it for `go run ./cmd/worker/`; the builders widened the recognised spellings in `953be65` and the code comment says that an unrecognised spelling "is handed the whole environment"). Executed (A.16, `node launcher-spellings.mjs scripts/runtime-env.mjs`):
all 18 spellings that start a backend directly are named (`go run` with and without `./`, a trailing slash, `main.go`, the module path, flags; `./scripts/go.sh run`; `bin/worker`, `./bin/worker`, an absolute path, a backslash path; `env A=1 bin/worker`; `nice -n 5 bin/dispatcher`; `bash -c bin/worker`), which is the builders' table. Of 16 fringe spellings 4 are named and **12 are not**: `sh -c './bin/worker --flag'`
(one argument that is a whole command line with a flag), `bash -lc 'exec bin/api'`, `go run -C cmd worker`, `./w` (a link), `node -e "...spawn('bin/worker')"`, `make run-worker`, `npm run worker` and the glob, case and trailing-space variants. Some of those cannot start a worker at all and some are wrappers that no spelling table can know; `sh -c './bin/worker --flag'`, `bash -lc 'exec bin/api'` and `go run -C cmd worker` are real ways to start a backend that the launcher misses.
The fault is the default, not the table: an unknown command is trusted with every credential. A launcher that fails closed (an unknown command gets the shared variables only, or must name its kind) would not depend on the list being complete. LOW because it needs a person who starts a process through the launcher with an unusual spelling, on a host that holds the operator credentials.

#### IR-08 LOW - the static scan for single-version gates is blind to spellings that an honest author could write

The scan that the builders added for F-03 (`internal/metadata/version_family.go:14-18`: `sqlVersionToken` = a quote, `$` or `^` immediately followed by `1.N`) catches every spelling in the second review's list and in `sql_version_gate_test.go`, and it reads the live catalog (`m9r_catalog_version_gate_test.go`: 1,022 definitions of functions, CHECKs, policies, triggers, defaults, views and indexes).
Executed (A.17, unit test, no database): 10 of 21 further spellings of a gate on 1.4.0 alone are **not** reported: array literal, one member; array literal, 1.3.0 and 1.4.0; array literal through unnest; concatenation with a number; format; split_part on the minor; split_part cast to int; substring of the minor; regex with a character class; LIKE with an underscore. The plausible one is the PostgreSQL array literal (`= ANY('{1.4.0}'::text[])`, the opening brace stands in front of the digits); the others are contrived. A.18 asks whether the present catalog hides such a gate: of the 1,022 definitions 5 are named by the scan (`runtime.freeze_event` and two defaults name 1.0.0, `runtime.register_unique_constraint` names 1.4.0 and 1.5.0,
the CHECK of `metadata_versions` names all six) and 13 more mention `schema_version` without any version-shaped token; I read each of them: they copy the envelope field or compare the version of another document kind (`tenant-capabilities-v1`, `requirements-v2`, `acceptance-v2`, `sandbox-evidence-v1`), so **there is no hidden gate today**. The scan is a net for the next honest omission and not a proof; the behavioural test (`TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema`)
covers the one feature that failed once, and no other feature has one. LOW: a guard with blind spots, no defect in the present tree.

### Gate at `0e13abe`

| Step | Builders' claim on `507c12c` (`LINE_INTEGRATION_PLAN.md:51-57`) | Result at `0e13abe` |
|---|---|---|
| `make check` | PASS, 32 s (11 s on the final head) | **PASS**, 96.2 s: docs checksums 84 + 43, format, `go vet` of the packages and of `./tests/integration` with the `integration,performance` tags, `go mod verify`, eslint, tsc (cold build cache in the new worktree) |
| `make test-unit` | PASS, 221 s: 29 Go packages, 13 script tests, 126 frontend tests in 19 files | **PASS**, 256.7 s: 29 Go packages `ok`, 13 script tests, 126 frontend tests in 19 files |
| `make build` | PASS, 33 s | **PASS**, 22.4 s: 8 Go binaries and `vite build` (45 modules) |
| Full integration suite, `-race -v`, `-skip TestM8RedisACLStreamLossAndPendingRecovery` | PASS, 1976.7 s; 459 listed, 458 run | **PASS** as one run, 1845.922 s: 453 PASS, 5 SKIP (the opt-in retained legacy artifact tests), 0 FAIL; 458 of 459 tests run (the excluded one is the 459th); no data race, no panic. The tests the builders added for the fixes pass: `TestM14PreReadBufferingIsBounded` 19.87 s, `TestM14R01ProviderLabelsOutsideTheAuditCharsetKeepTheResult` 7.94 s, `TestM14R02CanceledProviderCallsAreChargedToTheDailyBudget` 5.51 s, `TestM14R03ResultsThatFinishTogetherAreNotLost` 10.70 s, `TestM14R03CreationsRacingForTheUsageRowLoseNoPaidAnswer` 11.58 s, `TestM14R06AnEnormousReportedUsageDoesNotLockTheTenantOut` 2.04 s, `TestM14R07AnAdapterThatIgnoresItsContextCannotHoldTheRequest` 5.92 s, `TestF05ApprovalOvertakenByACommittedRejection` 3.01 s, `TestM13R01RollbackOvertakenByACommittedRejection` 2.71 s, `TestM9RNoCatalogObjectGatesOnPartOfTheAuthoringFamily` 2.00 s, and the two timing-bound ones (`TestM12APIDetailReadsOfALargeCandidateConverge` 8.32 s, `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` 3.24 s) |
| `make test-e2e`, `make test-signin-e2e` | 11 passed (36.3 s), 7 passed (8.9 s) | **PASS**, 11 passed (25.3 s), 7 passed (9.6 s) |
| `make test-rules-fuzz test-events-fuzz test-workflows-fuzz` | PASS (5 targets of 10 s), 80 s | **PASS**: `FuzzPointer` 120,016, `FuzzRuleCompile` 251,075, `FuzzRuleRestoreAndInputs` 44,975, `FuzzFixedDecimalMoney` 9,286, `FuzzWorkflowRestore` 138,474 executions |
| `make test-event-outage` (ten-minute consumer absence) | PASS, 609.5 s | **PASS**, 608.96 s: real_absence_s=600.000 total_elapsed_s=606.693 completed=3 inbox=3 effects=3 audits=3 immutable_intents=3 attempts_retained=19 unconfirmed_redeliveries=2 |
| R7a invalid-bearer burst against `bin/api` built from the tip | - | **PASS**: 40 actual HTTP requests: 401=20 429=20 matching denied audits=20 rejected audits=0 elapsed=87ms |

The builders gated `507c12c`; the tip differs from it by documents and one comment line of `00091`, so the comparison is of the same code. My host was about as fast as theirs (suite 1845.922 s against their 1976.7 s). One run of each step; the numbers say nothing about flake rates.

### What the third pass does not settle

* **Residuals of the fixes, as I measured them** (the first three are stated by the builders, ADR104 Amendment C and `LINE_INTEGRATION_PLAN.md:38-44`; the fourth is my observation): the body budget can be kept full by at least 16 identities that transmit 32.0 MiB per 10 s (26.8 Mbit/s), which refuses every body-carrying request of every tenant while it lasts (A.14); a bearer token that expires during the provider phase loses the answer
  (M14 report, M14R-03); the single `control.ai_usage` row still contends (1 of the 24 creations of the last run of A.13 answered 409 at admission, nothing stored or charged); a flood is refused by the per-peer bucket of R7a together with the honest callers behind the same address, and by the per-identity share of the body budget together with the other requests of the same identity.
* **Cost of cancellation and of an oversized answer** (observations, not findings): a cancelled provider call is charged its input estimate plus the whole output reservation (about 6.7k tokens each in the probe of M14R-02), and a 3 MiB answer from the provider is charged by its byte estimate (792,106 tokens, `FAILED`/`BUDGET`, M14 report). Both are conservative by design; the tenant's daily token budget is the only ceiling above them, and the API has no global limit on provider phases (ADR104:566-568).
* **NOT RUN - the performance gates** of the remediation line (build tag `performance`, reference scale): the request path changed after they were last run (`f997efe`, `c76968f`, `b427e8b`, `a2c5c52`, `8528fde`); the builders say the same. A body pre-read, a budget lock and an extra rate-gate pass on every request are not free and nobody has measured what they cost at the reference load.
* **NOT RUN - the slower-database variant of IR-03.** I tried to throttle the CPU of the PostgreSQL container (the environment refused the `docker update --cpus` call); its allowance is 4 CPUs, the whole host, so the 2 s statement budget has not been tried on a database slower than the builders'. The statement is 38 times faster than before, which is the only evidence.
* **NOT RUN - a multi-peer body-budget attack**: one peer address was used (loopback; 127.0.0.2 as the honest one). A real network path, HTTP/2, TLS termination, a reverse proxy or load balancer in front of the API (every client then shares one address for the per-peer buckets) and more than one API replica (the budget, the traffic gate and the AI in-flight cap are process-local) were not tried.
* **NOT RUN - how many identities an attacker can obtain.** The strength of the budget depends on the cost of a verified identity at the real identity provider (self sign-up, MFA, rate limits); the probes sign their own tokens.
* **NOT RUN - the residuals the builders name that I did not construct:** a first activation racing an approval of another first candidate (F-05), a retry budget that ends in the middle of a try (503 without the audited 409), `migrate status` listing a below-maximum migration file as plain `pending` (exit 0) while `migrate up` refuses it.
* **NOT RUN - the builders' documentation audit** (`docs/verification/DOCS_TRUTH_AUDIT.md`, 26 defects by its own count): IR-04 is verified for the statements I cited and the two I found, not for the rest.
* **Still excluded, as in the earlier passes:** `TestM8RedisACLStreamLossAndPendingRecovery` (by instruction), the `driverpanic` diagnostic, the opt-in checks of retained legacy API artifacts (5 skips), a real identity provider with MFA, a real model provider, backup and restore, an upgrade from a database older than migration 44, a manual review of the web UI beyond `make test-e2e` and `make test-signin-e2e`.
* **What a PASS means here.** The project's implementation contract says that a milestone PASS does not certify production readiness. This PASS says that, for the attacks listed, at `0e13abe`, I found no HIGH or MEDIUM defect open; it says nothing about attacks I did not think of.

## Not covered

* The reference-scale performance gates of the remediation line (build tag `performance`, seven files; the R7a/R7b reference profile: ten-client read p95,
  32-client writes): **NOT RUN**. The ledger's headline numbers (read p95 158.903 ms; 6,527 writes at p95 349.656 ms) are the builders' and unverified here.
* `TestM8RedisACLStreamLossAndPendingRecovery` (excluded by instruction), the `driverpanic` diagnostic and the opt-in checks of retained legacy API artifacts (5 skips in
  the session 2 run): **NOT RUN**.
* Session 1 did not count runtime `t.Skip` sites (its output was filtered); session 2 did (5, all opt-in retained artifacts).
* HTTP/2, a real reverse proxy or load balancer in front of the API, TLS termination; more than one API replica (the admission queue, the traffic gate and the AI
  in-flight cap are process-local).
* Worker behaviour for a workflow job whose instance lives in a sandbox that is frozen while the job is RUNNABLE: read only (the fence raises 55000 `frozen sandbox is
  read-only`, `00086:44`; a job whose lease fails 20 times ends `DEAD`, `00022`), **NOT RUN**.
* Frozen-sandbox index or unique-claim mutation (those tables are outside the fence list of `00086`; they are not part of the evidence hash): **NOT RUN**.
* A real IdP, MFA and a production HTTPS gateway (the ledger itself lists them as deferred), backup and restore, an upgrade from a database older than migration 44.
* A manual review of the web UI beyond `make test-e2e` and `make test-signin-e2e`.
* The AI half of the merge is in the M14 report, including its own list of what was not covered.
* At `be6ec7c`: the items above stay as they were (performance gates, `driverpanic`, the M8 Redis ACL test, HTTP/2 and a proxy, more than one API replica, a real IdP, backup and restore), and one is new:
  the launcher fix B-01 was checked by its script tests (11 PASS) and by reading `scripts/runtime-env.mjs`, not by starting `bin/api` with a real provider file - **NOT RUN**. The builders' counts
  of failures under load (11 of 150 before, 0 of 600 after) are theirs; A.12 is my own, smaller experiment, and it loads the test process, not the database: IR-03 with a CPU-limited PostgreSQL
  container is **NOT RUN**. `postgres.CheckMigrationLineage` protects `cmd/migrate` only; goose used directly is not stopped.

## Appendix A - probe sources (Phase 1)

The probes need the helpers of the repository at the pinned commit (`m8Database`, `scalarFixture`, `m5Call`, `m14Setup`, ...); copy each block to the named file
under `tests/integration` (or the named package), then run
`node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -timeout 30m -v -run '^<TestName>$' ./tests/integration`
(unit probes: `./scripts/go.sh test -count=1 -v -run '^<TestName>$' ./internal/compiler`). They were never committed; delete them after use.

### A.1 IR-01 - `field.unique` under each authoring schema version

File: `tests/integration/zz_review_unique15_test.go`

```go
//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

// Review probe (not product code): the same fixture as uniqueFixture, but the authoring schema of the
// document that declares the unique field is a parameter. ADR100 Amendment I1 says schema 1.5.0 is a
// superset of 1.4.0 and carries field.unique. runtime.register_unique_constraint (migration 00052)
// selects the field definition with m.schema_version='1.4.0'.
func uniqueFixtureAt(t *testing.T, version string) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f, h, old, token, app := scalarFixture(t, 1)
	scope := scalarScope(t, h, old, token)
	_, err := f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3) ON CONFLICT DO NOTHING`, f.tenantA, app, scope.Hash())
	scalarSQL(t, err)
	_, err = f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name','n','field.contact.status','active','field.contact.memo0','unique-'||i::text,
 'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,$4::uuid FROM generate_series(1,5)i`, f.tenantA, app, scope.Hash(), f.alice)
	scalarSQL(t, err)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	datasets := root + "/datasets"
	ds := m9rDatasetCreate(t, h, datasets, token, "ADOPT", m9rDatasetWitness(t, h, strings.Split(old, "/entities/")[0], token))
	for i := 0; ds.State != "OPEN" && i < 10; i++ {
		w := m5Call(h, datasets+"/"+ds.ID+"/verify", token, struct{}{})
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ds) != nil {
			t.Fatal("unique adoption", w.Code)
		}
	}
	if ds.State != "OPEN" {
		t.Fatal("unique adoption unfinished")
	}
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("unique document")
	}
	doc["schema_version"] = version
	defs := []any{}
	for _, item := range doc["definitions"].([]any) {
		d := item.(map[string]any)
		if d["kind"] == "rule" || d["kind"] == "workflow" {
			continue
		}
		d["schema_version"] = version
		if d["id"] == "field.contact.memo0" {
			d["field"].(map[string]any)["unique"] = true
		}
		defs = append(defs, d)
	}
	doc["definitions"] = defs
	raw, _ = json.Marshal(doc)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 2, raw); w.Code != 201 {
		t.Fatal("unique authoring", version, w.Code, w.Body.String())
	}
	witness := m9rDatasetWitness(t, h, root+"/versions/3", token)
	witness["expected_binding"] = int64(1)
	if w := m5Call(h, datasets+"/"+ds.ID+"/bindings", token, witness); w.Code != 200 {
		t.Fatal("unique bind", version, w.Code, w.Body.String())
	}
	base := datasets + "/" + ds.ID + "/bindings/2/entities/entity.contact"
	return f, h, base, token, app
}

func TestReviewUniqueFieldUnderEachSchemaVersion(t *testing.T) {
	for _, version := range []string{"1.4.0", "1.5.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			m8Database(t)
			_, h, base, token, _ := uniqueFixtureAt(t, version)
			path := base + "/unique-constraints/field.contact.memo0"
			tryCreates := func(when string) {
				for _, c := range []struct {
					name   string
					values map[string]any
				}{{"unique value set", uniqueValues("fresh-" + when)}, {"unique field absent", m5Values("plain " + when)}} {
					w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Key: uuid(t), Values: c.values})
					t.Logf("schema %s: create with %s %s -> HTTP %d", version, c.name, when, w.Code)
				}
			}
			tryCreates("before registration")
			w := m5Call(h, path+"/register", token, map[string]any{"expected_generation": int64(0)})
			t.Logf("schema %s: register -> HTTP %d %s", version, w.Code, strings.TrimSpace(w.Body.String()))
			if w.Code != 200 {
				t.Errorf("schema %s: unique constraint registration answered %d", version, w.Code)
				tryCreates("after the failed registration")
				return
			}
			x := data.UniqueConstraint{}
			_ = json.Unmarshal(w.Body.Bytes(), &x)
			for i := 0; x.State != "READY" && i < 20; i++ {
				w = m5Call(h, path+"/build", token, map[string]any{"expected_generation": x.Generation, "expected_checkpoint": x.Checkpoint})
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &x) != nil {
					t.Fatalf("schema %s: build -> %d %s", version, w.Code, w.Body.String())
				}
			}
			t.Logf("schema %s: state %s", version, x.State)
			tryCreates("after READY")
		})
	}
}
```

### A.2 IR-02 - slow request bodies hold the admission slots

File: `tests/integration/zz_review_slowbody_test.go`

```go
//go:build integration

package integration

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

// Review probe (not product code). Authenticated, authorized requests whose bodies arrive slowly: the route reads
// its body inside the runtime transaction, after the database admission slot (default 4) was taken. The attackers
// keep their connections cycling (the server ends each one at its 10 s read timeout). Honest requests are measured
// through the real http.Server with the production timeouts, an honest AI proposal included: its result exchange
// needs a slot again after the provider phase.
func slowBodyAttack(t *testing.T, e *m14Env, path string, attackers int, duration time.Duration, withAI bool) {
	t.Helper()
	doc := e.storedDoc(1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httpserver.New(ln.Addr().String(), e.h)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	addr := ln.Addr().String()

	get := func(label string) {
		started := time.Now()
		req, _ := http.NewRequest("GET", "http://"+addr+"/v1/tenants/"+e.f.tenantA+"/access", nil)
		req.Header.Set("Authorization", "Bearer "+e.viewer)
		client := &http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("%-34s honest GET failed after %v: %v", label, time.Since(started).Round(10*time.Millisecond), err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		t.Logf("%-34s honest GET -> HTTP %d after %v", label, resp.StatusCode, time.Since(started).Round(10*time.Millisecond))
	}
	propose := func(label string) {
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Honest"), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
		started := time.Now()
		req, _ := http.NewRequest("POST", "http://"+addr+e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Rename the order entity."}`))
		req.Header.Set("Authorization", "Bearer "+e.alice)
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		resp, err := client.Do(req)
		if err != nil {
			t.Logf("%-34s honest proposal failed after %v: %v", label, time.Since(started).Round(10*time.Millisecond), err)
			return
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("%-34s honest proposal -> HTTP %d after %v %.80s", label, resp.StatusCode, time.Since(started).Round(10*time.Millisecond), strings.TrimSpace(string(b)))
	}
	get("baseline (no attack)")
	if withAI {
		propose("baseline (no attack)")
	}

	stop := time.Now().Add(duration)
	var wg sync.WaitGroup
	for i := 0; i < attackers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				c, err := net.Dial("tcp", addr)
				if err != nil {
					return
				}
				head := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 30000\r\n\r\n{\"request\":\"", path, e.alice)
				if _, err = c.Write([]byte(head)); err != nil {
					c.Close()
					return
				}
				for time.Now().Before(stop) {
					time.Sleep(700 * time.Millisecond)
					if _, err = c.Write([]byte("a")); err != nil {
						break // the server ended the connection (read timeout)
					}
				}
				c.Close()
			}
		}()
	}
	time.Sleep(2 * time.Second)
	var inTx int
	_ = e.f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`).Scan(&inTx)
	t.Logf("attackers=%d, idle-in-transaction backends at +2s: %d", attackers, inTx)
	label := fmt.Sprintf("under %d slow bodies", attackers)
	get(label)
	if withAI {
		propose(label)
	}
	get(label + " (again)")
	wg.Wait()
	time.Sleep(time.Second)
	get("after the attackers left")
	t.Logf("proposals now: %s", e.statuses())
}

func TestReviewSlowBodyHoldsAdmissionSlots(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	t.Run("metadata POST (inherited route), 4 slow bodies", func(t *testing.T) {
		slowBodyAttack(t, e, "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", 4, 16*time.Second, false)
	})
	t.Run("ai patch-proposals, 4 slow bodies", func(t *testing.T) { slowBodyAttack(t, e, e.root+"/ai/patch-proposals", 4, 26*time.Second, true) })
}
```

### A.3 IR-03 - the bounded compensation scan with a 60 s statement timeout (timing probe)

File: `tests/integration/zz_review_workerpolicy_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/jackc/pgx/v5/pgconn"
)

// Review probe (not product code): the unchanged body of TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry with a
// 60 s statement timeout, to read how long the bounded compensation scan takes where the 2 s budget fails.
func TestReviewWorkerPolicyScanTiming(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "sql-statement-policy")
	h := r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	d := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "ALL"})
	condition := policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}}
	children := make([]policy.DataScope, 8)
	for i := range children {
		children[i] = condition
	}
	scope := policy.DataScope{Kind: "OR", Children: children}
	grant := d.Grants[1]
	grant.Scope = &scope
	d.Grants = append(d.Grants[:1], d.Grants[2:]...)
	for i := 0; i < 7; i++ {
		d.Grants = append(d.Grants, grant)
	}
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, h, root+"/roles", m.token, 0, d, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, d.Name, 200)
	// Bounded synthetic scan, not evidence of a full 96-step business lifecycle.
	if _, e := m.f.operator.Exec(`INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) SELECT $1,$2,'scan-'||g,'local.noop',repeat('a',64),convert_to('{}','UTF8'),encode(sha256(convert_to('{}','UTF8')),'hex'),g,'local.noop' FROM generate_series(1,96) g`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	bind := func() *sql.Tx {
		t.Helper()
		tx, e := m.db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		if ok, e := bindM9R(tx, lease); e != nil || !ok {
			tx.Rollback()
			t.Fatal("scan lease", e)
		}
		if _, e = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true),set_config('statement_timeout','60000',true)`, m.f.alice, m.f.aliceSubject); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		return tx
	}
	const query = `SELECT r.step_key FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`
	tx := bind()
	start := time.Now()
	var step string
	if e = tx.QueryRow(query, m.f.tenantA, x.ID).Scan(&step); e != nil || step != "scan-96" {
		tx.Rollback()
		t.Fatal("bounded compensation scan", e)
	}
	t.Logf("REVIEW 96 receipts, 7 scopes/63 nodes per capability: %s", time.Since(start))
	var raw []byte
	if e = tx.QueryRow("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query, m.f.tenantA, x.ID).Scan(&raw); e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	var plan any
	if json.Unmarshal(raw, &plan) != nil {
		t.Fatal("query plan")
	}
	decisions := 0
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if value["Function Name"] == "worker_policy_decision" {
				decisions++
				if loops, ok := value["Actual Loops"].(float64); !ok || loops > 1 {
					t.Error("policy definition/IR evaluation still scales with receipt rows")
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(plan)
	if decisions == 0 {
		t.Error("plan did not execute live authorization")
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	// A statement-wide decision must not cache lease freshness for a slow write.
	if _, e = m.f.operator.Exec(`UPDATE control.workflow_jobs SET lease_until=clock_timestamp()+interval '500 milliseconds' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	tx = bind()
	defer tx.Rollback()
	result, e := tx.Exec(`UPDATE runtime.workflow_instances SET version=(SELECT version+1 FROM pg_sleep(0.7)) WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID)
	if e != nil {
		var denied *pgconn.PgError
		if !errors.As(e, &denied) || denied.Code != "42501" {
			t.Fatal("expected expired lease RLS denial", e)
		}
	} else if n, e := result.RowsAffected(); e != nil || n != 0 {
		t.Fatal("statement decision cached an expired write lease", e, n)
	}
}
```

### A.4 IR-05 - migrating over the milestone line's numbers

File: `tests/integration/zz_review_renumber_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/pressly/goose/v3"
	"os"
)

// Review probe (not product code). The milestone line used the migration versions 45-53 and 60 for other
// files than the integrated tree does. A development database that ran the milestone line holds those
// versions in goose_db_version. LINE_INTEGRATION_PLAN says it "has to be rebuilt, not migrated"; this shows what
// `make migrate` would do to it if somebody runs it anyway.
func TestReviewMigrateOverMilestoneNumbering(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []int64
	}{
		{"milestone line through M13 (45-53)", []int64{45, 46, 47, 48, 49, 50, 51, 52, 53}},
		{"milestone line through M14 (45-53, 60)", []int64{45, 46, 47, 48, 49, 50, 51, 52, 53, 60}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m8DatabaseAt(t, 44)
			m, err := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			for _, v := range tc.versions {
				if _, err = m.Exec(`INSERT INTO goose_db_version(version_id,is_applied) VALUES($1,true)`, v); err != nil {
					t.Fatal(err)
				}
			}
			p, err := goose.NewProvider(goose.DialectPostgres, m, migrations.Files, goose.WithDisableGlobalRegistry(true))
			if err != nil {
				t.Fatal(err)
			}
			results, err := p.Up(context.Background())
			applied := []int64{}
			for _, r := range results {
				applied = append(applied, r.Source.Version)
			}
			var hasToolTable, hasWorkflowRoles bool
			_ = m.QueryRow(`SELECT to_regclass('runtime.tool_invocations') IS NOT NULL`).Scan(&hasToolTable)
			_ = m.QueryRow(`SELECT to_regclass('runtime.scalar_indexes') IS NOT NULL`).Scan(&hasWorkflowRoles)
			t.Logf("Up over fake %v: applied=%v err=%v; tool ledger table exists=%v; remediation scalar_indexes exists=%v", tc.versions, applied, err, hasToolTable, hasWorkflowRoles)
			if err == nil {
				t.Errorf("migration over milestone numbering reported success: the remediation migrations 45-53 were silently skipped")
			}
		})
	}
}
```

### A.5 Decision 4 - the drained cutover refuses a live old reader

File: `tests/integration/zz_review_cutover_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/pressly/goose/v3"
)

// Review probe (not product code). Decision 4: migration 00057 (R7b isolation cutover) refuses to run while an old
// reader of the event tables is alive. In the integrated tree `migrate up` continues after 00057 with 00080-00089.
// With a live old transaction the run must stop at 00057 and install none of the milestone ports; after the old
// transaction ended, the same command must apply 00057 and 00080-00089.
func TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts(t *testing.T) {
	m8DatabaseAt(t, 56)
	ctx := context.Background()
	m, err := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	old, err := worker.BeginTx(ctx, nil) // an old-style (pre-cutover) reader: plain transaction, no isolation certificate
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = old.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, m, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	results, upErr := p.Up(ctx)
	applied := []int64{}
	for _, r := range results {
		applied = append(applied, r.Source.Version)
	}
	var tools, fences bool
	var version int64
	_ = m.QueryRow(`SELECT to_regclass('runtime.tool_invocations') IS NOT NULL, to_regclass('control.release_fences') IS NOT NULL, (SELECT max(version_id) FROM goose_db_version WHERE is_applied)`).Scan(&tools, &fences, &version)
	t.Logf("live old reader: Up applied=%v err=%v; db version=%d; tool ledger installed=%v; M13 fences installed=%v", applied, upErr, version, tools, fences)
	if upErr == nil || tools || fences || version != 56 {
		t.Errorf("the cutover did not stop the run before the milestone ports")
	}
	if err = old.Rollback(); err != nil {
		t.Fatal(err)
	}
	results, upErr = p.Up(ctx)
	applied = applied[:0]
	for _, r := range results {
		applied = append(applied, r.Source.Version)
	}
	_ = m.QueryRow(`SELECT (SELECT max(version_id) FROM goose_db_version WHERE is_applied)`).Scan(&version)
	t.Logf("old reader gone: Up applied=%v err=%v; db version=%d", applied, upErr, version)
	// 57 and every milestone port from 80 on (80-89 at the pinned commit; the integration branch has since added 90).
	if upErr != nil || len(applied) < 11 || applied[0] != 57 || applied[1] != 80 || version != applied[len(applied)-1] {
		t.Errorf("the retry did not apply 57 and 80 onwards")
	}
	var safe bool
	wdb, _ := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	defer wdb.Close()
	if err = wdb.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe); err != nil || !safe {
		t.Errorf("worker guard after the staged upgrade: %v %v", safe, err)
	}
}
```

### A.6 Decision 5 - every POST route against anonymous, viewer and member

File: `tests/integration/zz_review_routes_test.go`

```go
//go:build integration

package integration

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// Review probe (not product code): every POST route registered by access.API.Register, sent with a
// viewer's and a member's valid token and with no token. A route that lost its action mapping in the merge
// of the two `if` chains falls back to preview.read (allowed to viewers) or tenant.access and shows up here as
// something other than 403 for a role that must not be able to call it. A route missing from the POST body
// allowlist shows up as 413 body_not_allowed for an unauthenticated caller instead of 401.
func TestReviewRouteMatrix(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	mia := e.member("mia", "member")
	A := e.f.tenantA
	app := e.app
	id, id2 := uuid(t), uuid(t)
	hash := strings.Repeat("ab", 32)
	p := "/v1/tenants/" + A + "/preview/" + app
	type route struct {
		name, method, path string
		// viewer / member: "deny" must be 403 forbidden; "any" is only logged.
		viewer, member string
	}
	routes := []route{
		{"datasets create", "POST", p + "/datasets", "deny", "deny"},
		{"datasets verify", "POST", p + "/datasets/" + id + "/verify", "deny", "deny"},
		{"datasets bind", "POST", p + "/datasets/" + id + "/bindings", "deny", "deny"},
		{"evolution create", "POST", p + "/datasets/" + id + "/evolutions", "deny", "deny"},
		{"evolution advance", "POST", p + "/datasets/" + id + "/evolutions/" + id2 + "/advance", "deny", "deny"},
		{"evolution activate", "POST", p + "/datasets/" + id + "/evolutions/" + id2 + "/activate", "deny", "deny"},
		{"evolution abort", "POST", p + "/datasets/" + id + "/evolutions/" + id2 + "/abort", "deny", "deny"},
		{"evolution reverify", "POST", p + "/datasets/" + id + "/evolutions/" + id2 + "/reverify", "deny", "deny"},
		{"sandbox create", "POST", p + "/sandboxes", "deny", "deny"},
		{"sandbox freeze", "POST", p + "/sandboxes/" + id + "/freeze", "deny", "deny"},
		{"requirements append", "POST", p + "/requirements", "deny", "deny"},
		{"acceptance author", "POST", p + "/acceptance-specs", "deny", "deny"},
		{"candidate build", "POST", p + "/candidates", "deny", "deny"},
		{"candidate approve", "POST", p + "/candidates/" + hash + "/approve", "deny", "deny"},
		{"candidate reject", "POST", p + "/candidates/" + hash + "/reject", "deny", "deny"},
		{"candidate activate", "POST", p + "/candidates/" + hash + "/activate", "deny", "deny"},
		{"release rollback", "POST", p + "/releases/rollback", "deny", "deny"},
		{"ai patch-proposals", "POST", p + "/ai/patch-proposals", "deny", "deny"},
		{"ai requirement-proposals", "POST", p + "/ai/requirement-proposals", "deny", "deny"},
		{"ai apply", "POST", p + "/ai/proposals/" + id + "/apply", "deny", "deny"},
		{"ai confirm", "POST", p + "/ai/proposals/" + id + "/confirm", "deny", "deny"},
		{"ai reject", "POST", p + "/ai/proposals/" + id + "/reject", "deny", "deny"},
		{"entity query (version)", "POST", p + "/versions/1/entities/entity.order/query", "any", "any"},
		{"entity commands (version)", "POST", p + "/versions/1/entities/entity.order/commands", "deny", "any"},
		{"entity query (binding)", "POST", p + "/datasets/" + id + "/bindings/1/entities/entity.order/query", "any", "any"},
		{"entity commands (binding)", "POST", p + "/datasets/" + id + "/bindings/1/entities/entity.order/commands", "deny", "any"},
		{"rule evaluate", "POST", p + "/versions/1/rules/rule.x/evaluate", "any", "any"},
		{"record access (version)", "POST", p + "/versions/1/entities/entity.order/records/" + id + "/access", "deny", "any"},
		{"record access (binding)", "POST", p + "/datasets/" + id + "/bindings/1/entities/entity.order/records/" + id + "/access", "deny", "any"},
		{"index register (version)", "POST", p + "/versions/1/entities/entity.order/indexes/field.order.total/register", "deny", "deny"},
		{"index build (version)", "POST", p + "/versions/1/entities/entity.order/indexes/field.order.total/build", "deny", "deny"},
		{"search-index register", "POST", p + "/versions/1/entities/entity.order/search-indexes/field.order.total/register", "deny", "deny"},
		{"search-index build", "POST", p + "/versions/1/entities/entity.order/search-indexes/field.order.total/build", "deny", "deny"},
		{"unique register", "POST", p + "/versions/1/entities/entity.order/unique-constraints/field.order.total/register", "deny", "deny"},
		{"unique build", "POST", p + "/versions/1/entities/entity.order/unique-constraints/field.order.total/build", "deny", "deny"},
		{"unique abort", "POST", p + "/versions/1/entities/entity.order/unique-constraints/field.order.total/abort", "deny", "deny"},
		{"unique register (binding)", "POST", p + "/datasets/" + id + "/bindings/1/entities/entity.order/unique-constraints/field.order.total/register", "deny", "deny"},
		{"workflow start (version)", "POST", p + "/versions/1/workflows/workflow.x/start", "any", "any"},
		{"workflow start (binding)", "POST", p + "/datasets/" + id + "/bindings/1/workflows/workflow.x/start", "any", "any"},
		{"workflow signal (version)", "POST", p + "/versions/1/workflows/workflow.x/instances/" + id + "/signals", "any", "any"},
		{"roles publish", "POST", "/v1/tenants/" + A + "/roles", "deny", "deny"},
		{"role assign", "POST", "/v1/tenants/" + A + "/members/" + id + "/role", "deny", "deny"},
		{"team create", "POST", "/v1/tenants/" + A + "/teams", "deny", "deny"},
		{"team member", "POST", "/v1/tenants/" + A + "/teams/" + id + "/members/" + id2, "deny", "deny"},
		{"metadata append", "POST", "/v1/tenants/" + A + "/metadata/" + app + "/versions", "deny", "deny"},
	}
	deniedRows := func() (int, map[string]int) {
		var total int
		_ = e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND allowed=false`, e.f.tenantA).Scan(&total)
		by := map[string]int{}
		rows, err := e.f.operator.Query(`SELECT action, count(*) FROM audit.entries WHERE tenant_id=$1 AND allowed=false GROUP BY action`, e.f.tenantA)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var a string
				var c int
				_ = rows.Scan(&a, &c)
				by[a] = c
			}
		}
		return total, by
	}
	before, _ := deniedRows()
	denyCalls := 0
	n := 0
	for _, r := range routes {
		n++
		areq := httptest.NewRequest(r.method, r.path, strings.NewReader("{}"))
		areq.RemoteAddr = fmt.Sprintf("198.51.100.%d:4000", n) // one peer per route: the invalid-bearer budget is per peer (R7a)
		areq.Header.Set("Content-Type", "application/json")
		arec := httptest.NewRecorder()
		e.h.ServeHTTP(arec, areq)
		anon := arec
		vw := m14DoType(e.h, r.method, r.path, e.viewer, "application/json", "{}")
		mb := m14DoType(e.h, r.method, r.path, mia.token, "application/json", "{}")
		t.Logf("%-28s anon=%d viewer=%d(%s) member=%d(%s)", r.name, anon.Code, vw.Code, m14Code(vw), mb.Code, m14Code(mb))
		if anon.Code != 401 {
			t.Errorf("%s: unauthenticated POST answered %d %s, want 401", r.name, anon.Code, m14Code(anon))
		}
		if r.viewer == "deny" {
			denyCalls++
		}
		if r.member == "deny" {
			denyCalls++
		}
		if r.viewer == "deny" && (vw.Code != 403 || m14Code(vw) != "forbidden") {
			t.Errorf("%s: viewer got %d %s, want 403 forbidden", r.name, vw.Code, m14Code(vw))
		}
		if r.member == "deny" && (mb.Code != 403 || m14Code(mb) != "forbidden") {
			t.Errorf("%s: member got %d %s, want 403 forbidden", r.name, mb.Code, m14Code(mb))
		}
	}
	after, by := deniedRows()
	t.Logf("denied audit rows written by the matrix: %d for %d denied calls; by action: %v", after-before, denyCalls, by)
	if after-before < denyCalls {
		t.Errorf("%d denied calls wrote only %d denial audit rows", denyCalls, after-before)
	}
	for _, action := range []string{"datasets.manage", "datasets.evolve", "sandbox.manage", "requirements.decide", "acceptance.author", "candidates.build", "releases.approve", "releases.activate", "releases.rollback", "ai.propose", "ai.apply", "preview.write", "indexes.manage", "roles.write", "roles.assign", "teams.manage", "metadata.write"} {
		if by[action] == 0 {
			t.Errorf("no denial was audited under the action %s", action)
		}
	}
}
```

### A.7 Decision 1 - one version token, several readings (unit test)

File: `internal/compiler/zz_review_versions_test.go`

```go
package compiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Review probe (not product code): one version token, several readings. A 1.5.0 IR is compiled from the
// tool example, then re-shaped the way a stored or forged artifact could be, and restored.
func TestReviewVersionTokenReadings(t *testing.T) {
	raw, _ := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.5.json")
	s := source(t, raw)
	p, ds := Compile(s, toolCompilerCatalog(testToolCatalog(t)))
	if !p.Valid() || len(ds) != 0 {
		t.Fatal(ds)
	}
	restore := func(mutate func(ir *IR)) error {
		ir := p.IR()
		mutate(&ir)
		b, err := json.Marshal(ir)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Restore(b, digest(b), SupportedIRVersions())
		return err
	}
	check := func(name string, want bool, mutate func(ir *IR)) {
		err := restore(mutate)
		got := err == nil
		t.Logf("%-62s restore ok=%v (want %v) %v", name, got, want, err)
		if got != want {
			t.Errorf("%s: restore ok=%v, want %v", name, got, want)
		}
	}
	check("compiled 1.5.0 IR (control)", true, func(ir *IR) {})
	check("legacy shape: plan 2.1.0 without tool_contract", true, func(ir *IR) {
		for i := range ir.Workflows {
			ir.Workflows[i].DSLVersion = "2.1.0"
			for j := range ir.Workflows[i].States {
				ir.Workflows[i].States[j].ToolContract = nil
			}
		}
	})
	check("plan 2.2.0 whose tool states lost tool_contract", false, func(ir *IR) {
		for i := range ir.Workflows {
			for j := range ir.Workflows[i].States {
				ir.Workflows[i].States[j].ToolContract = nil
			}
		}
	})
	check("plan 2.1.0 that also carries tool_contract", false, func(ir *IR) {
		for i := range ir.Workflows {
			ir.Workflows[i].DSLVersion = "2.1.0"
		}
	})
	check("plan 2.0.0 with tool states", false, func(ir *IR) {
		for i := range ir.Workflows {
			ir.Workflows[i].DSLVersion = "2.0.0"
			for j := range ir.Workflows[i].States {
				ir.Workflows[i].States[j].ToolContract = nil
			}
		}
	})
	check("IR 1.5.0 relabelled IR 1.4.0 (document stays 1.5.0)", false, func(ir *IR) { ir.IRVersion, ir.CompilerVersion = "1.4.0", "1.4.0" })
	check("IR 1.4.0 + schema 1.4.0 labels + tool plan", false, func(ir *IR) {
		ir.IRVersion, ir.CompilerVersion = "1.4.0", "1.4.0"
		ir.Application.SchemaVersion = "1.4.0"
		for i := range ir.Application.Definitions {
			ir.Application.Definitions[i].SchemaVersion = "1.4.0"
		}
	})
	check("IR 1.3.0 + schema 1.3.0 labels + tool plan", false, func(ir *IR) {
		ir.IRVersion, ir.CompilerVersion = "1.3.0", "1.3.0"
		ir.Application.SchemaVersion = "1.3.0"
		for i := range ir.Application.Definitions {
			ir.Application.Definitions[i].SchemaVersion = "1.3.0"
		}
	})
	check("IR 1.5.0, schema 1.5.0, but plans dropped", false, func(ir *IR) { ir.Workflows = nil })
	check("legacy shape + a WRITE tool downgraded to approval NONE (legacy has no contract)", true, func(ir *IR) {
		for i := range ir.Workflows {
			ir.Workflows[i].DSLVersion = "2.1.0"
			for j := range ir.Workflows[i].States {
				ir.Workflows[i].States[j].ToolContract = nil
			}
		}
	})
}

// A document that names a tool activity must not validate, and must not compile, under any schema but 1.5.0, and a
// tool workflow must author DSL 2.1.0 only.
func TestReviewToolActivityUnderOlderSchemas(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.5.json")
	if err != nil {
		t.Fatal(err)
	}
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = v.Parse(raw); err != nil {
		t.Fatalf("control: the 1.5.0 example does not parse: %v", err)
	}
	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0", "1.3.0", "1.4.0"} {
		doc := strings.ReplaceAll(string(raw), `"schema_version": "1.5.0"`, `"schema_version": "`+version+`"`)
		doc = strings.ReplaceAll(doc, `"schema_version":"1.5.0"`, `"schema_version":"`+version+`"`)
		if doc == string(raw) {
			t.Fatal("the example spells its schema version differently than this probe expects")
		}
		if _, err = v.Parse([]byte(doc)); err == nil {
			t.Errorf("a tool activity validates under schema %s", version)
		}
	}
	for _, dsl := range []string{"2.0.0", "2.2.0"} {
		doc := strings.ReplaceAll(strings.ReplaceAll(string(raw), `"dsl_version": "2.1.0"`, `"dsl_version": "`+dsl+`"`), `"dsl_version":"2.1.0"`, `"dsl_version":"`+dsl+`"`)
		if doc == string(raw) {
			t.Fatal("the example spells its DSL version differently than this probe expects")
		}
		if _, err = v.Parse([]byte(doc)); err == nil {
			t.Errorf("a 1.5.0 tool workflow authored with DSL %s validates", dsl)
		}
	}
}
```

### A.8 SSRF address forms (unit test)

File: `internal/ai/zz_review_ssrf_test.go`

```go
package ai

import (
	"net/netip"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// Review probe (not product code): addresses that name an internal, loopback, link-local or metadata
// destination in a form a resolver (or a hostile DNS answer) could return. All must be denied by the guard the
// HTTP provider dials through (tools.DeniedAddress, internalAllowed=false). Public controls must be allowed.
func TestReviewDeniedAddressForms(t *testing.T) {
	deny := []string{
		"127.0.0.1", "127.255.255.254", "0.0.0.0", "0.1.2.3", "10.0.0.1", "172.16.0.1", "172.31.255.255", "192.168.1.1", "100.64.0.1", "100.127.255.255",
		"169.254.169.254", "169.254.0.1", "168.63.129.16", "192.0.0.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.0.0.1", "239.255.255.255", "240.0.0.1", "255.255.255.255",
		"::", "::1", "fe80::1", "fe80::1%eth0", "fc00::1", "fd00::1", "fd00:ec2::254", "ff02::1", "2001:db8::1", "100::1",
		"::ffff:127.0.0.1", "::ffff:10.0.0.1", "::ffff:169.254.169.254", "::ffff:7f00:1", "0:0:0:0:0:ffff:a9fe:a9fe",
		"::127.0.0.1", "::10.0.0.1", "::169.254.169.254", "::a9fe:a9fe",
		"64:ff9b::7f00:1", "64:ff9b::a9fe:a9fe", "64:ff9b::a00:1", "64:ff9b::c0a8:101", "64:ff9b:1::a9fe:a9fe",
		"2002:7f00:1::", "2002:a9fe:a9fe::", "2002:a00:1::1", "2002:c0a8:101::1",
		"2001:0:4136:e378:8000:63bf:3fff:fdd2", "2001:0:a9fe:a9fe::1", "2001:0:5efe:7f00:1::",
		"fe80::5efe:7f00:1", "fe80::200:5efe:169.254.169.254", "::5efe:a9fe:a9fe",
		"fec0::1", "3fff::1",
	}
	allow := []string{"8.8.8.8", "93.184.216.34", "1.1.1.1", "2606:4700:4700::1111", "2a00:1450:4001:81b::200e"}
	for _, s := range deny {
		ip, err := netip.ParseAddr(s)
		if err != nil {
			t.Logf("unparseable (skipped, a resolver cannot return it): %s", s)
			continue
		}
		if !tools.DeniedAddress(ip, false) {
			t.Errorf("ALLOWED %s (unmapped %s)", s, ip.Unmap())
		}
	}
	for _, s := range allow {
		if tools.DeniedAddress(netip.MustParseAddr(s), false) {
			t.Errorf("public address %s is denied", s)
		}
	}
}
```

### A.9 Follow-up - the records of a frozen sandbox stay evidence

File: `tests/integration/zz_review_frozen_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

// Review probe (not product code). The records of a frozen sandbox are evidence (the freeze hashes them). Everything the app credential
// and the API can do to them afterwards, then a recomputation of the freeze hash from the live rows.
func TestReviewFrozenSandboxRecordsStayEvidence(t *testing.T) {
	m8Database(t)
	f, h, _, alice, app := m5Fixture(t)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	witness := m9rDatasetWitness(t, h, root+"/versions/1", alice)
	box := m11Create(t, h, root, alice, witness, "REROUTE", 200)
	path := root + "/datasets/" + box.ID + "/bindings/1/entities/entity.contact"
	first := m5Create(t, h, path, alice, "Alpha")
	second := m5Create(t, h, path, alice, "Beta")
	if w := m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", alice, struct{}{}); w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	evidence, _ := m11rReadEvidence(t, f, box.ID)
	live := func() (int, string) {
		var n int
		var hash string
		err := f.operator.QueryRow(`SELECT count(*),coalesce(encode(sha256(convert_to(string_agg(r.entity_id||':'||r.id::text||':'||r.row_version::text||':'||encode(sha256(convert_to(r.custom_data::text,'UTF8')),'hex'),E'\n' ORDER BY r.entity_id,r.id),'UTF8')),'hex'),repeat('0',64))
 FROM runtime.records r JOIN runtime.datasets d ON d.tenant_id=r.tenant_id AND d.app_id=r.app_id AND r.metadata_revision=d.anchor_revision AND r.ir_hash=d.anchor_hash WHERE d.tenant_id=$1 AND d.id=$2`, f.tenantA, box.ID).Scan(&n, &hash)
		if err != nil {
			t.Fatal(err)
		}
		return n, hash
	}
	n0, h0 := live()
	t.Logf("stored evidence: %d records, hash %s...; recomputed from the live rows: %d records, hash %s...", evidence.Records.Count, evidence.Records.Hash[:12], n0, h0[:12])
	if n0 != evidence.Records.Count || h0 != evidence.Records.Hash {
		t.Fatalf("the recomputation does not reproduce the freeze (probe error)")
	}
	var revision int64
	var irHash string
	if err := f.operator.QueryRow(`SELECT anchor_revision,anchor_hash FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, box.ID).Scan(&revision, &irHash); err != nil {
		t.Fatal(err)
	}
	for name, cmd := range map[string]data.Command{
		"api create": {Operation: "create", Values: m5Values("Gamma")},
		"api update": {Operation: "update", ID: first.ID, ExpectedVersion: first.Version, Values: map[string]any{"field.contact.name": "Tampered"}},
		"api delete": {Operation: "delete", ID: second.ID, ExpectedVersion: second.Version},
	} {
		w := m5Call(h, path+"/commands", alice, cmd)
		t.Logf("%-12s on the frozen sandbox -> HTTP %d %s", name, w.Code, strings.TrimSpace(w.Body.String())[:min(120, len(strings.TrimSpace(w.Body.String())))])
		if w.Code/100 == 2 {
			t.Errorf("%s was accepted on a frozen sandbox", name)
		}
	}
	for name, stmt := range map[string]string{
		"sql delete": `DELETE FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND id=$5`,
		"sql update": `UPDATE runtime.records SET custom_data=custom_data||'{"field.contact.name":"Tampered"}'::jsonb WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND id=$5`,
	} {
		err := m11rAsApp(t, f, f.aliceSubject, sql.LevelRepeatableRead, func(ctx context.Context, q postgres.Executor) error {
			if _, e := q.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
				return e
			}
			_, e := q.ExecContext(ctx, stmt, f.tenantA, app, revision, irHash, first.ID)
			return e
		})
		t.Logf("%-12s as the app credential -> SQLSTATE %s (%v)", name, m11rCode(err), err)
		if err == nil {
			t.Errorf("%s was accepted on a frozen sandbox", name)
		}
	}
	n1, h1 := live()
	t.Logf("after every attempt: %d records, hash %s...", n1, h1[:12])
	if n1 != evidence.Records.Count || h1 != evidence.Records.Hash {
		t.Errorf("the live records no longer match the frozen evidence")
	}
}
```

### A.10 Follow-up - differential test of `control.strict_candidate_json`

File: `tests/integration/zz_review_strictjson_test.go`

```go
//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"math/rand"
	"strings"
	"testing"
)

// Review probe (not product code): differential test of control.strict_candidate_json(txt, doc) (migration 00087) against a reference that
// implements its documented contract with a real tokenizer: the text is one object without surrounding whitespace, no object has two members
// whose DECODED names are equal (at any depth), and no number is spelled with an exponent. A text the function accepts but the
// reference refuses is a body whose bytes Go and the jsonb view could read differently; a text the reference accepts but the function
// refuses is only over-strict.
func refStrictCandidateJSON(txt string) bool {
	if len(txt) == 0 || txt[0] != '{' || txt[len(txt)-1] != '}' {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(txt))
	dec.UseNumber()
	var walk func() bool
	walk = func() bool {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{':
				seen := map[string]bool{}
				for dec.More() {
					kt, err := dec.Token()
					key, ok := kt.(string)
					if err != nil || !ok || seen[key] {
						return false
					}
					seen[key] = true
					if !walk() {
						return false
					}
				}
				_, err := dec.Token()
				return err == nil
			case '[':
				for dec.More() {
					if !walk() {
						return false
					}
				}
				_, err := dec.Token()
				return err == nil
			}
		case json.Number:
			if strings.ContainsAny(string(v), "eE") {
				return false
			}
		}
		return true
	}
	if !walk() {
		return false
	}
	_, err := dec.Token()
	return err == io.EOF
}

var (
	strictKeys = []string{`"a"`, `"b"`, `"A"`, `"a"`, `"b"`, `"k1"`, `"k1"`, `"é"`, `"é"`, `"😀"`, `"😀"`, `"a\\"`, `"a\""`, `""`, `"\/"`, `"/"`, `"a b"`, `"a b"`}
	strictNums = []string{"0", "-0", "1", "10", "123", "1.5", "-1", "0.5", "1e1", "1E1", "1e+1", "1e-1", "0e0", "1.0", "100", "-0.0", "0.1e2", "12345678901234567890", "1.5E3", "9e9"}
	strictStrs = []string{`""`, `"x"`, `"1e5"`, `"01"`, `"a\"b"`, `"\\"`, `"é"`, `"é"`, `"\n"`, `"{\"a\":1,\"a\":2}"`, `"\"\""`, `"\\\""`}
	strictWS   = []string{"", "", "", " ", "\n", "\t", "\r\n"}
)

func genStrict(r *rand.Rand, depth int) string {
	ws := func() string { return strictWS[r.Intn(len(strictWS))] }
	if depth > 3 {
		return strictNums[r.Intn(len(strictNums))]
	}
	switch r.Intn(8) {
	case 0, 1, 2:
		n := r.Intn(5)
		parts := make([]string, 0, n)
		for i := 0; i < n; i++ {
			parts = append(parts, ws()+strictKeys[r.Intn(len(strictKeys))]+ws()+":"+ws()+genStrict(r, depth+1)+ws())
		}
		return "{" + strings.Join(parts, ",") + "}"
	case 3:
		n := r.Intn(4)
		parts := make([]string, 0, n)
		for i := 0; i < n; i++ {
			parts = append(parts, ws()+genStrict(r, depth+1)+ws())
		}
		return "[" + strings.Join(parts, ",") + "]"
	case 4:
		return strictStrs[r.Intn(len(strictStrs))]
	case 5:
		return strictNums[r.Intn(len(strictNums))]
	case 6:
		return []string{"true", "false", "null"}[r.Intn(3)]
	}
	return strictStrs[r.Intn(len(strictStrs))]
}

func TestReviewStrictCandidateJSONDifferential(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	r := rand.New(rand.NewSource(87))
	const total = 200000
	const batch = 2000
	var valid, sqlTrue, refTrue, holes, over int
	var holeExamples, overExamples []string
	for done := 0; done < total; done += batch {
		texts := make([]string, 0, batch)
		for len(texts) < batch {
			members := r.Intn(5)
			parts := make([]string, 0, members)
			for i := 0; i < members; i++ {
				parts = append(parts, strictKeys[r.Intn(len(strictKeys))]+":"+genStrict(r, 1))
			}
			text := "{" + strings.Join(parts, ",") + "}"
			switch r.Intn(12) {
			case 0:
				text = " " + text
			case 1:
				text += "\n"
			}
			texts = append(texts, text)
		}
		rows, err := f.operator.Query(`SELECT t, pg_input_is_valid(t,'jsonb') AS ok, CASE WHEN pg_input_is_valid(t,'jsonb') THEN control.strict_candidate_json(t, t::jsonb) END AS strict FROM unnest($1::text[]) t`, texts)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var text string
			var ok bool
			var strict *bool
			if err = rows.Scan(&text, &ok, &strict); err != nil {
				t.Fatal(err)
			}
			if !ok {
				continue
			}
			valid++
			s := strict != nil && *strict
			ref := refStrictCandidateJSON(text)
			if s {
				sqlTrue++
			}
			if ref {
				refTrue++
			}
			switch {
			case s && !ref:
				holes++
				if len(holeExamples) < 12 {
					holeExamples = append(holeExamples, text)
				}
			case !s && ref:
				over++
				if len(overExamples) < 12 {
					overExamples = append(overExamples, text)
				}
			}
		}
		rows.Close()
	}
	t.Logf("%d texts generated, %d valid for jsonb; accepted by the function: %d; accepted by the reference: %d", total, valid, sqlTrue, refTrue)
	t.Logf("accepted by the function but refused by the reference (holes): %d", holes)
	for _, e := range holeExamples {
		t.Logf("   HOLE %q", e)
	}
	t.Logf("refused by the function but accepted by the reference (over-strict): %d", over)
	for _, e := range overExamples {
		t.Logf("   OVER %q", e)
	}
	if holes > 0 {
		t.Errorf("%d texts pass strict_candidate_json although they hold a duplicate member or an exponent number", holes)
	}
}
```

**Note on A.11 and A.12 (they need the repository at `be6ec7c`).** The block A.11 compiles against the helpers of `be6ec7c` (`m14Setup`, `uniqueFixtureAt(t, count, duplicates, version)`, `m8DatabaseAt`, ...), not against the pinned commit. To run the earlier probes A.1 and A.2 at `be6ec7c` rename the helper `uniqueFixtureAt` of A.1 (the builders added a function of that name with four parameters); A.2 needs `statuses` from A.1 of the M14 report. The third test needs `OLD_MIGRATIONS_DIR`, a directory holding the milestone line's original `00045`-`00053` and `00060` (`git show 60da6f2:db/migrations/<file>` for each; `60da6f2` is the last commit of the milestone line), and `MIGRATE_BIN`, a built `cmd/migrate` (`make build`); without them it skips.

### A.11 Re-verification at `be6ec7c` - slow bodies after the fix, unique constraints under schema 1.5.0, a real milestone database

File: `tests/integration/zz_review_reverify_test.go` - in the third pass the only change is the logging of the `code` of a refused honest request

```go
//go:build integration

package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/pressly/goose/v3"
)

// Review probes (not product code) for the fixes of integration/m9r-m14 after the pinned commit: the body pre-read of `serve` (IR-02),
// the unique registration under schema 1.5.0 (IR-01) and the migration lineage guard (IR-05).

// ---------------------------------------------------------------- IR-02: slow bodies of every kind

type slowKind struct {
	name  string
	path  func(e *m14Env) string
	token func(e *m14Env) string
	// run sends the request on c and keeps it alive until stop; it returns the first line of the server's answer, if one came.
	run func(c net.Conn, path, token string, stop time.Time) string
}

func firstLine(c net.Conn, wait time.Duration) string {
	_ = c.SetReadDeadline(time.Now().Add(wait))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(line)
}

func TestReviewSlowBodiesAfterTheFix(t *testing.T) {
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httpserver.New(ln.Addr().String(), e.h)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	addr := ln.Addr().String()
	// the "code" of the last error answer of an honest request (third pass: the 429s of the sixth kind come from two different limits)
	var honestCode string
	codeOf := func(raw []byte) string {
		text := string(raw)
		if i := strings.Index(text, `"code":"`); i >= 0 {
			if j := strings.Index(text[i+8:], `"`); j >= 0 {
				return text[i+8 : i+8+j]
			}
		}
		return "-"
	}
	honestGET := func() (int, time.Duration) {
		started := time.Now()
		req, _ := http.NewRequest("GET", "http://"+addr+"/v1/tenants/"+e.f.tenantA+"/access", nil)
		req.Header.Set("Authorization", "Bearer "+e.viewer)
		resp, err := (&http.Client{Timeout: 40 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
		if err != nil {
			return 0, time.Since(started)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode >= 400 {
			honestCode = codeOf(raw)
		}
		resp.Body.Close()
		return resp.StatusCode, time.Since(started)
	}
	honestPropose := func() (int, time.Duration) {
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Honest"), Delay: 500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
		started := time.Now()
		req, _ := http.NewRequest("POST", "http://"+addr+e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Rename the order entity."}`))
		req.Header.Set("Authorization", "Bearer "+e.alice)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 90 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}).Do(req)
		if err != nil {
			return 0, time.Since(started)
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode >= 400 {
			honestCode = codeOf(raw)
		}
		resp.Body.Close()
		return resp.StatusCode, time.Since(started)
	}
	drip := func(c net.Conn, stop time.Time, chunked bool) {
		for time.Now().Before(stop) {
			time.Sleep(700 * time.Millisecond)
			piece := "a"
			if chunked {
				piece = "1\r\na\r\n"
			}
			if _, err := c.Write([]byte(piece)); err != nil {
				return
			}
		}
	}
	head := func(path, token, extra string) string {
		return fmt.Sprintf("POST %s HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\n%s\r\n", path, token, extra)
	}
	aiPath := func(e *m14Env) string { return e.root + "/ai/patch-proposals" }
	metaPath := func(e *m14Env) string { return "/v1/tenants/" + e.f.tenantA + "/metadata/" + e.app + "/versions" }
	owner := func(e *m14Env) string { return e.alice }
	viewer := func(e *m14Env) string { return e.viewer }
	kinds := []slowKind{
		{"owner, content-length drip, AI route", aiPath, owner, func(c net.Conn, p, tok string, stop time.Time) string {
			_, _ = c.Write([]byte(head(p, tok, "Content-Length: 30000\r\n") + `{"request":"`))
			drip(c, stop, false)
			return ""
		}},
		{"owner, content-length drip, metadata route", metaPath, owner, func(c net.Conn, p, tok string, stop time.Time) string {
			_, _ = c.Write([]byte(head(p, tok, "Content-Length: 30000\r\n") + `{"expected_revision":1,"document":`))
			drip(c, stop, false)
			return ""
		}},
		{"viewer (denied after the read), content-length drip, AI route", aiPath, viewer, func(c net.Conn, p, tok string, stop time.Time) string {
			_, _ = c.Write([]byte(head(p, tok, "Content-Length: 30000\r\n") + `{"request":"`))
			drip(c, stop, false)
			return ""
		}},
		{"owner, chunked drip, AI route", aiPath, owner, func(c net.Conn, p, tok string, stop time.Time) string {
			_, _ = c.Write([]byte(head(p, tok, "Transfer-Encoding: chunked\r\n") + "c\r\n{\"request\":\"\r\n"))
			drip(c, stop, true)
			return ""
		}},
		{"invalid token, content-length drip, AI route", aiPath, func(*m14Env) string { return "not-a-token" }, func(c net.Conn, p, tok string, stop time.Time) string {
			_, _ = c.Write([]byte(head(p, tok, "Content-Length: 30000\r\n") + `{"request":"`))
			drip(c, stop, false)
			return ""
		}},
		{"owner, 2 MiB declared, 1.2 MiB sent at once, then silence", aiPath, owner, func(c net.Conn, p, tok string, stop time.Time) string {
			_, _ = c.Write([]byte(head(p, tok, "Content-Length: 2097152\r\n")))
			_, _ = c.Write([]byte(strings.Repeat("a", 1_200_000)))
			line := firstLine(c, 5*time.Second)
			return line
		}},
	}
	for _, k := range kinds {
		k := k
		t.Run(k.name, func(t *testing.T) {
			stop := time.Now().Add(14 * time.Second)
			var wg sync.WaitGroup
			var mu sync.Mutex
			answers := map[string]int{}
			for i := 0; i < 6; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for time.Now().Before(stop) {
						c, err := net.Dial("tcp", addr)
						if err != nil {
							return
						}
						line := k.run(c, k.path(e), k.token(e), stop)
						c.Close()
						if line != "" {
							mu.Lock()
							answers[line]++
							mu.Unlock()
						}
					}
				}()
			}
			time.Sleep(2 * time.Second)
			code, took := honestGET()
			t.Logf("honest GET under 6 attackers -> HTTP %d after %v", code, took.Round(time.Millisecond))
			if code >= 400 {
				t.Logf("  the answer to the honest GET carried the code %q", honestCode)
			}
			if code != 200 || took > 1500*time.Millisecond {
				t.Errorf("an honest GET was delayed or refused: HTTP %d after %v", code, took)
			}
			pcode, ptook := honestPropose()
			t.Logf("honest AI proposal under 6 attackers -> HTTP %d after %v", pcode, ptook.Round(time.Millisecond))
			if pcode >= 400 {
				t.Logf("  the answer to the honest proposal carried the code %q", honestCode)
			}
			if pcode != 201 || ptook > 6*time.Second {
				t.Errorf("an honest AI proposal was delayed or lost: HTTP %d after %v", pcode, ptook)
			}
			wg.Wait()
			mu.Lock()
			if len(answers) > 0 {
				t.Logf("what the attackers were answered: %v", answers)
			}
			mu.Unlock()
		})
	}
	t.Logf("proposals afterwards: %s", reviewProposalStatuses(e))
}

// ---------------------------------------------------------------- IR-01: the unique lifecycle under 1.5.0 enforces uniqueness

func TestReviewUniqueIsEnforcedUnderSchema15(t *testing.T) {
	for _, version := range []string{"1.4.0", "1.5.0"} {
		version := version
		t.Run(version, func(t *testing.T) {
			m8Database(t)
			_, h, base, token, _, _ := uniqueFixtureAt(t, 5, false, version)
			path := base + "/unique-constraints/field.contact.memo0"
			w := m5Call(h, path+"/register", token, map[string]any{"expected_generation": int64(0)})
			if w.Code != 200 {
				t.Fatalf("schema %s: register -> %d %s", version, w.Code, w.Body.String())
			}
			x := struct {
				State      string `json:"state"`
				Generation int64  `json:"generation"`
				Checkpoint int64  `json:"checkpoint"`
			}{}
			decode := func(b []byte) {
				if err := json.Unmarshal(b, &x); err != nil {
					t.Fatal(err)
				}
			}
			decode(w.Body.Bytes())
			for i := 0; x.State != "READY" && i < 20; i++ {
				w = m5Call(h, path+"/build", token, map[string]any{"expected_generation": x.Generation, "expected_checkpoint": x.Checkpoint})
				if w.Code != 200 {
					t.Fatalf("schema %s: build -> %d %s", version, w.Code, w.Body.String())
				}
				decode(w.Body.Bytes())
			}
			first := m5Call(h, base+"/commands", token, map[string]any{"operation": "create", "command_key": uuid(t), "values": uniqueValues("only-one")})
			second := m5Call(h, base+"/commands", token, map[string]any{"operation": "create", "command_key": uuid(t), "values": uniqueValues("only-one")})
			absent := m5Call(h, base+"/commands", token, map[string]any{"operation": "create", "command_key": uuid(t), "values": m5Values("no unique value")})
			t.Logf("schema %s: state %s; create 'only-one' -> %d, the same value again -> %d (%s), a record without the field -> %d", version, x.State, first.Code, second.Code, strings.TrimSpace(second.Body.String())[:min(90, len(strings.TrimSpace(second.Body.String())))], absent.Code)
			if x.State != "READY" || first.Code != 200 || second.Code != 409 || absent.Code != 200 {
				t.Errorf("schema %s: the unique lifecycle does not hold (state %s, %d, %d, %d)", version, x.State, first.Code, second.Code, absent.Code)
			}
		})
	}
}

// ---------------------------------------------------------------- IR-05: a REAL database built with the milestone line's old numbers

func TestReviewRealMilestoneDatabaseIsRefusedByTheMigrator(t *testing.T) {
	dir, bin := os.Getenv("OLD_MIGRATIONS_DIR"), os.Getenv("MIGRATE_BIN")
	if dir == "" || bin == "" {
		t.Skip("needs OLD_MIGRATIONS_DIR (the milestone line's migrations 45-53 and 60) and MIGRATE_BIN (a built cmd/migrate)")
	}
	m8DatabaseAt(t, 44)
	m, err := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	ctx := context.Background()
	p, err := goose.NewProvider(goose.DialectPostgres, m, os.DirFS(dir), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	results, err := p.Up(ctx)
	version := func() int64 {
		var v int64
		_ = m.QueryRow(`SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&v)
		return v
	}
	t.Logf("the milestone line's own migrations applied on top of 44: %d migrations, error %v, database version %d", len(results), err, version())
	if err != nil || version() != 60 {
		t.Fatalf("could not build the old-numbered database: %v", err)
	}
	var toolLedger bool
	_ = m.QueryRow(`SELECT to_regclass('runtime.tool_invocations') IS NOT NULL`).Scan(&toolLedger)
	t.Logf("tool ledger table present: %v", toolLedger)
	if err = postgres.CheckMigrationLineage(ctx, m); !errors.Is(err, postgres.ErrForeignMigrationNumbering) {
		t.Errorf("CheckMigrationLineage on a real milestone database answered %v", err)
	}
	for _, arg := range []string{"up", "status"} {
		out, runErr := exec.Command(bin, arg).CombinedOutput()
		t.Logf("migrate %-6s -> %v: %.200s", arg, runErr, strings.TrimSpace(string(out)))
		if runErr == nil {
			t.Errorf("migrate %s succeeded on a milestone database", arg)
		}
	}
	if v := version(); v != 60 {
		t.Errorf("the refused migrate changed the database: version %d", v)
	}
}

func reviewProposalStatuses(e *m14Env) string {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT status||'/'||coalesce(failure_code,'') FROM control.ai_proposals WHERE tenant_id=$1 AND app_id=$2 ORDER BY created_at`, e.f.tenantA, e.app)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		out = append(out, v)
	}
	return strings.Join(out, ",")
}
```

### A.12 Re-verification at `be6ec7c` - the IR-03 test fixes under CPU load (shell)

File: `a scratch script, not a repository file` - the script that produced the numbers quoted in the re-verification (it also logged the time of every run and counted the failing executions)

```bash
#!/bin/bash
# IR-03 under load, before and after the builders' test changes.
#   MAIN = checkout of the pinned tree (product and test code equal 552fa9e), NEW = worktree of be6ec7c, OUT = a scratch directory.
#   Everything goes through ./scripts/go.sh and node scripts/with-env.mjs as elsewhere; taskset is needed.
set -u

# 1. the four test binaries, built unloaded
( cd $NEW  && ./scripts/go.sh test -race -tags=integration -c -o $OUT/it-new.test ./tests/integration )
( cd $NEW  && ./scripts/go.sh test -race -c -o $OUT/tools-new.test ./internal/tools )
( cd $MAIN && ./scripts/go.sh test -race -tags=integration -c -o $OUT/it-old.test ./tests/integration )
( cd $MAIN && ./scripts/go.sh test -race -c -o $OUT/tools-old.test ./internal/tools )

# 2. the load: one busy loop on CPU 0; the tests may use CPUs 0 and 1 (about a quarter of the normal capacity of a 4 vCPU host)
taskset -c 0 bash -c 'while :; do :; done' &
BUSY=$!
trap 'kill $BUSY' EXIT

run_it() { # label binary tree test runs
  for i in $(seq 1 $5); do
    node $3/scripts/with-env.mjs taskset -c 0,1 bash -c "cd $3/tests/integration && exec $2 -test.run '^$4\$' -test.v -test.count=1 -test.timeout=20m"
  done
}

# (a) the large-candidate test: before and after the retrying set-up
run_it pinned  $OUT/it-old.test $MAIN TestM12APIDetailReadsOfALargeCandidateConverge 3
run_it be6ec7c $OUT/it-new.test $NEW  TestM12APIDetailReadsOfALargeCandidateConverge 3
# (c) the worker-policy test, which the builders did not change
run_it be6ec7c $OUT/it-new.test $NEW  TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry 3
# (b) the tools timeout test, 300 executions each, before and after the test server aborts a departed client's handler
for pair in "$OUT/tools-old.test $MAIN" "$OUT/tools-new.test $NEW"; do
  set -- $pair
  ( cd $2/internal/tools && taskset -c 0,1 $1 -test.run '^TestTimeoutWhileServerSleepsIsUnknownAndBounded$' -test.count=300 -test.timeout=30m )
done
```

### A.13 Third pass at `0e13abe` - body budget accounting, a randomized stress of the release ports, an exactly-once check of the AI retries, the compensation scan against its predecessor

File: `tests/integration/zz_review_third_test.go` - needs the repository at `0e13abe`; `REVIEW_FENCE_ROUNDS` and `REVIEW_FENCE_SEED` set the length and the seed of the stress (18 rounds and seed 20261022 by default; the long run used 150 and 424242)

```go
//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/jackc/pgx/v5/pgconn"
)

// Review probes for the third pass (head 0e13abe). Not product code. The starvation probe of the budget is in the second file (A.14).

func reviewUsage(e *m14Env) (requests, used int64) {
	e.t.Helper()
	_ = e.f.operator.QueryRow(`SELECT coalesce(sum(requests),0),coalesce(sum(used_tokens),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&requests, &used)
	return
}

// ------------------------------------------------------------------------------------------------ body budget: accounting

// A booking is given back whatever happened to the request: every kind of ending is thrown at a small budget by several identities, and
// the budget must be empty afterwards and never above its total in between.
func TestReviewBodyBudgetAccountingReturnsToZero(t *testing.T) {
	const total, share = 8 << 20, 3 << 20
	budget := access.NewBodyBudget(total, share)
	e := m14Setup(t, m14Options{bodies: budget})
	e.draft("500")
	addr := m14Serve(t, e)
	tokens := []string{e.alice, e.viewer, e.f.token(t, uuid(t), nil), e.f.token(t, uuid(t), nil), "", "not-a-token"}
	paths := []string{
		e.root + "/ai/patch-proposals", e.root + "/candidates", e.root + "/acceptance-specs",
		e.root + "/versions/1/entities/entity.order/commands", "/v1/tenants/" + e.f.tenantA + "/metadata/" + e.app + "/versions", e.root + "/datasets",
	}
	var peak atomic.Int64
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			if used, _ := budget.InFlight(); used > peak.Load() {
				peak.Store(used)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	r := rand.New(rand.NewSource(20261022))
	var rmu sync.Mutex
	pick := func(n int) int { rmu.Lock(); defer rmu.Unlock(); return r.Intn(n) }
	var wg sync.WaitGroup
	sem := make(chan struct{}, 48)
	var statuses sync.Map
	count := func(code int) {
		v, _ := statuses.LoadOrStore(code, new(atomic.Int64))
		v.(*atomic.Int64).Add(1)
	}
	const requests = 700
	for i := 0; i < requests; i++ {
		kind, token, path, size := pick(9), tokens[pick(len(tokens))], paths[pick(len(paths))], 1+pick(1<<20)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				return
			}
			defer conn.Close()
			body := bytes.Repeat([]byte("a"), size)
			head := m14Head(addr, path, token, size)
			switch kind {
			case 0: // the whole body, read the answer
				_, _ = conn.Write(append([]byte(head), body...))
				s, _, _ := m14Answer(conn, 6*time.Second)
				count(s)
			case 1: // half of the body, then the client leaves
				_, _ = conn.Write(append([]byte(head), body[:size/2]...))
			case 2: // the whole body, the client leaves without reading
				_, _ = conn.Write(append([]byte(head), body...))
			case 3: // Expect: 100-continue, then the body
				h := strings.Replace(head, "\r\n\r\n", "\r\nExpect: 100-continue\r\n\r\n", 1)
				_, _ = conn.Write([]byte(h))
				time.Sleep(20 * time.Millisecond)
				_, _ = conn.Write(body)
				s, _, _ := m14Answer(conn, 6*time.Second)
				count(s)
			case 4: // chunked, one chunk and the client leaves in the middle of the next
				h := fmt.Sprintf("POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n", path, addr, token)
				_, _ = conn.Write([]byte(h))
				_, _ = conn.Write([]byte(fmt.Sprintf("%x\r\n", size/2+1)))
				_, _ = conn.Write(body[:size/2+1])
				_, _ = conn.Write([]byte("\r\n10\r\nabc"))
			case 5: // announces and sends nothing
				_, _ = conn.Write([]byte(head))
			case 6: // part of the body, then silence until the server's read deadline is near, then the client leaves
				_, _ = conn.Write(append([]byte(head), body[:size/3]...))
				time.Sleep(300 * time.Millisecond)
			case 7: // a body that is larger than the front door admits
				big := bytes.Repeat([]byte("a"), 1<<20+4096)
				_, _ = conn.Write(append([]byte(m14Head(addr, path, token, len(big))), big...))
				s, _, _ := m14Answer(conn, 6*time.Second)
				count(s)
			case 8: // valid-looking JSON of a size the route may accept
				doc := []byte(`{"request":"` + strings.Repeat("b", size%20000) + `"}`)
				_, _ = conn.Write(append([]byte(m14Head(addr, path, token, len(doc))), doc...))
				s, _, _ := m14Answer(conn, 6*time.Second)
				count(s)
			}
		}()
	}
	wg.Wait()
	deadline := time.Now().Add(25 * time.Second)
	for {
		used, subjects := budget.InFlight()
		if used == 0 && subjects == 0 {
			break
		}
		if time.Now().After(deadline) {
			close(stop)
			t.Fatalf("the booked bytes were not returned: %d bytes of %d identities, %v after the last request ended", used, subjects, 25*time.Second)
		}
		time.Sleep(100 * time.Millisecond)
	}
	close(stop)
	var summary []string
	statuses.Range(func(k, v any) bool {
		summary = append(summary, fmt.Sprintf("%d:%d", k.(int), v.(*atomic.Int64).Load()))
		return true
	})
	sort.Strings(summary)
	t.Logf("%d mixed requests; answers %v; the peak of booked bytes was %.2f MiB of a total of %d MiB; all returned", requests, summary, float64(peak.Load())/(1<<20), total>>20)
	if peak.Load() > total {
		t.Errorf("the booked bytes (%d) exceeded the total (%d)", peak.Load(), total)
	}
}

// ------------------------------------------------------------------------------------------------ release fences: randomized stress

func sqlstateOf(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return "-"
}

type fenceOp struct {
	kind, hash string
	held       bool
	call, ack  time.Time
	err        error
}

// Approve, reject, activate and roll back one candidate and its neighbours at random offsets, half of them on a transaction whose
// snapshot is older than the moment they call the port. The veto must never be overtaken, every outcome must be a documented one, and
// nothing may hang.
func TestReviewReleaseFenceStress(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	first, second := e.twoActivations(t)
	_ = first
	_ = second
	rounds, seed := 18, int64(20261022)
	if v, err := strconv.Atoi(os.Getenv("REVIEW_FENCE_ROUNDS")); err == nil && v > 0 {
		rounds = v
	}
	if v, err := strconv.ParseInt(os.Getenv("REVIEW_FENCE_SEED"), 10, 64); err == nil {
		seed = v
	}
	r := rand.New(rand.NewSource(seed))
	allowed := map[string]bool{"40001": true, "40P01": true, "55000": true, "55P03": true, "42501": true}
	outcomes := map[string]int{}
	violations := 0
	for round := 0; round < rounds; round++ {
		h, _ := e.build("500", m13Relabel(fmt.Sprintf("Stress %d", round)), e.carol)
		if r.Intn(2) == 0 {
			if _, err := e.approvePort(e.dave.subject, h); err != nil {
				t.Fatal("pre-approval", err)
			}
		}
		pointer, generation, _ := e.activePointer()
		rows := e.historyRows()
		prev := ""
		if len(rows) > 0 {
			prev = rows[len(rows)-1].previous
		}
		var ops []*fenceOp
		var mu sync.Mutex
		var wg sync.WaitGroup
		start := make(chan struct{})
		add := func(kind, hash, subject, query string, args ...any) {
			op := &fenceOp{kind: kind, hash: hash, held: r.Intn(2) == 0}
			delay := time.Duration(r.Intn(25)) * time.Millisecond
			hold := time.Duration(r.Intn(25)) * time.Millisecond
			ops = append(ops, op)
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				time.Sleep(delay)
				if op.held {
					ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
					go func() { done <- e.m13rHold(subject, ready, proceed, query, args...) }()
					select {
					case <-ready:
					case err := <-done:
						mu.Lock()
						op.call, op.ack, op.err = time.Now(), time.Now(), err
						mu.Unlock()
						return
					}
					time.Sleep(hold)
					mu.Lock()
					op.call = time.Now()
					mu.Unlock()
					close(proceed)
					err := <-done
					mu.Lock()
					op.ack, op.err = time.Now(), err
					mu.Unlock()
					return
				}
				mu.Lock()
				op.call = time.Now()
				mu.Unlock()
				var dst any
				err := e.portIn(subject, e.f.tenantA, &dst, query, args...)
				mu.Lock()
				op.ack, op.err = time.Now(), err
				mu.Unlock()
			}()
		}
		if r.Intn(10) < 8 {
			add("approve", h, e.dave.subject, `SELECT control.approve_release($1,$2)`, e.app, h)
		}
		if r.Intn(10) < 6 {
			add("approve", h, e.erin.subject, `SELECT control.approve_release($1,$2)`, e.app, h)
		}
		if r.Intn(10) < 8 {
			add("reject", h, e.f.bobSubject, `SELECT control.reject_release($1,$2,$3,$4)`, e.app, h, "RISK_UNACCEPTABLE", "stress veto")
		}
		if prev != "" && r.Intn(10) < 5 {
			add("reject", prev, e.f.bobSubject, `SELECT control.reject_release($1,$2,$3,$4)`, e.app, prev, "RISK_UNACCEPTABLE", "stress veto of the rollback target")
		}
		if r.Intn(10) < 8 {
			add("activate", h, e.erin.subject, `SELECT control.activate_release($1,$2,$3,$4)`, e.app, h, pointer, generation)
		}
		if prev != "" && r.Intn(10) < 5 {
			add("rollback", prev, e.erin.subject, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, e.app, prev, pointer, generation, "", false)
		}
		if len(ops) < 2 {
			add("approve", h, e.dave.subject, `SELECT control.approve_release($1,$2)`, e.app, h)
			add("reject", h, e.f.bobSubject, `SELECT control.reject_release($1,$2,$3,$4)`, e.app, h, "RISK_UNACCEPTABLE", "stress veto")
		}
		close(start)
		finished := make(chan struct{})
		go func() { wg.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(30 * time.Second):
			t.Fatalf("round %d: a release port call did not return within 30 s (a hang or an undetected deadlock)", round)
		}
		for _, op := range ops {
			code := "ok"
			if op.err != nil {
				code = sqlstateOf(op.err)
				if !allowed[code] {
					violations++
					t.Errorf("round %d: %s %s ended with an undocumented outcome %s: %v", round, op.kind, op.hash[:8], code, op.err)
				}
			}
			outcomes[op.kind+"/"+code]++
			if d := op.ack.Sub(op.call); d > 10*time.Second {
				violations++
				t.Errorf("round %d: %s took %v", round, op.kind, d)
			}
		}
		for _, v := range ops {
			if v.kind != "reject" || v.err != nil {
				continue
			}
			for _, x := range ops {
				if x.err == nil && x.hash == v.hash && x.kind != "reject" && x.call.After(v.ack) {
					violations++
					t.Errorf("round %d: a %s of %s succeeded although the veto had been acknowledged before the call began (veto acked %v before it)", round, x.kind, v.hash[:8], x.call.Sub(v.ack))
				}
			}
		}
		e.checkHistory()
		if hash, _, _ := e.activePointer(); len(e.rejectionsOf(hash)) > 0 {
			// A release may be rejected after it ran; it is a violation only when the rejection was acknowledged before the pointer moved onto it.
			for _, v := range ops {
				if v.kind == "reject" && v.err == nil && v.hash == hash {
					for _, x := range ops {
						if x.err == nil && (x.kind == "activate" || x.kind == "rollback") && x.hash == hash && x.call.After(v.ack) {
							violations++
							t.Errorf("round %d: the pointer is on a rejected hash that was vetoed before the move", round)
						}
					}
				}
			}
		}
	}
	keys := make([]string, 0, len(outcomes))
	for k := range outcomes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%d", k, outcomes[k]))
	}
	t.Logf("%d rounds (seed %d); outcomes: %s; violations: %d", rounds, seed, strings.Join(out, " "), violations)
}

// ------------------------------------------------------------------------------------------------ AI: exactly once under contention

// Eight proposals of one tenant that finish together, on the repeated exchanges: how many answers are lost, and is anything stored or
// charged twice? The scripted provider reports the same usage for every call, so a double charge shows as used_tokens above
// (proposals that finished) x (the charge of one).
func TestReviewAIRetryIsExactlyOnce(t *testing.T) {
	for round := 1; round <= 3; round++ {
		round := round
		t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
			const n = 8
			e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: n, DailyTokens: 50_000_000}})
			doc := e.draft("500")
			// one sequential request: the charge of one finished proposal
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Label 0"), Delay: 100 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
			if w := e.propose(e.alice, "Rename the order entity."); w.Code != 201 {
				t.Fatalf("baseline proposal: %d %s", w.Code, w.Body.String())
			}
			_, base := reviewUsage(e)
			calls0 := e.provider.Calls()
			for i := 0; i < n; i++ {
				e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", fmt.Sprintf("Label %d", i+1)), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
			}
			var wg sync.WaitGroup
			codes := make([]string, n)
			start := make(chan struct{})
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					w := e.propose(e.alice, "Rename the order entity.")
					codes[i] = fmt.Sprintf("%d %s", w.Code, m14Code(w))
				}(i)
			}
			close(start)
			wg.Wait()
			// let the leases of orphans lapse is not needed: count what exists
			requests, used := reviewUsage(e)
			var ready, pending, failed, rows int
			_ = e.f.operator.QueryRow(`SELECT count(*), count(*) FILTER (WHERE status='READY'), count(*) FILTER (WHERE status='PENDING'), count(*) FILTER (WHERE status='FAILED') FROM control.ai_proposals WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&rows, &ready, &pending, &failed)
			created := 0
			refusedAtAdmission := 0
			for _, c := range codes {
				switch {
				case strings.HasPrefix(c, "201"):
					created++
				default:
					refusedAtAdmission++
				}
			}
			calls := e.provider.Calls() - calls0
			t.Logf("round %d: answers=%v; proposals: %d rows, %d READY, %d PENDING, %d FAILED; provider calls %d; usage: requests=%d used=%d (one finished proposal costs %d)", round, codes, rows, ready, pending, failed, calls, requests, used, base)
			if base == 0 {
				t.Fatal("no baseline charge")
			}
			if ready != created+1 {
				t.Errorf("%d answers were 201 but %d proposals are READY (the baseline is the +1)", created, ready)
			}
			if pending != 0 {
				t.Errorf("%d proposals are PENDING: a paid answer was discarded", pending)
			}
			if int64(calls) > int64(rows-1) {
				t.Errorf("%d provider calls for %d proposals (a call was repeated)", calls, rows-1)
			}
			if want := base * int64(ready); used != want {
				t.Errorf("used_tokens=%d, want %d (= %d finished x %d): charged twice or not at all", used, want, ready, base)
			}
		})
	}
}

// ------------------------------------------------------------------------------------------------ D1: the compensation scan

const oldCompensationScan = "SELECT r.step_key,r.compensation_ref,r.output FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1"

// The rewritten statement (an uncorrelated NOT IN) against the one it replaced (a correlated NOT EXISTS), on the same random receipts.
func TestReviewCompensationScanEquivalence(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	ctx := context.Background()
	conn, err := f.operator.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `CREATE TEMP TABLE r(tenant_id uuid NOT NULL,instance_id uuid NOT NULL,step_key text NOT NULL CHECK(length(step_key)<=200),output bytea NOT NULL DEFAULT '\x',sequence bigint NOT NULL,compensation_ref text NOT NULL DEFAULT '',PRIMARY KEY(tenant_id,instance_id,step_key))`); err != nil {
		t.Fatal(err)
	}
	oldSQL := strings.ReplaceAll(oldCompensationScan, "runtime.workflow_receipts", "r")
	newSQL := strings.ReplaceAll(workflow.CompensationScanSQL, "runtime.workflow_receipts", "r")
	if strings.Count(newSQL, "NOT IN") != 1 || strings.Contains(newSQL, "runtime.") {
		t.Fatalf("the statement under test is not what this probe expects: %s", newSQL)
	}
	r := rand.New(rand.NewSource(7))
	names := []string{"a", "b", "c", "d", "a:compensate", "b:compensate", "a:compensate:compensate", "x:compensate", "A", "é", "a ", " a", "", "s:compensate:compensate:compensate"}
	refs := []string{"", "", "local.noop", "x"}
	tenant := "00000000-0000-0000-0000-000000000001"
	differences, nonEmpty := 0, 0
	const datasets = 4000
	for i := 0; i < datasets; i++ {
		instance := fmt.Sprintf("00000000-0000-0000-0000-%012d", i+1)
		n := r.Intn(11)
		used := map[string]bool{}
		for j := 0; j < n; j++ {
			key := names[r.Intn(len(names))]
			if key == "" {
				key = fmt.Sprintf("k%d", r.Intn(4))
			}
			if r.Intn(3) == 0 {
				key += ":compensate"
			}
			if used[key] {
				continue
			}
			used[key] = true
			if _, err = conn.ExecContext(ctx, `INSERT INTO r(tenant_id,instance_id,step_key,output,sequence,compensation_ref) VALUES($1,$2,$3,convert_to($3,'UTF8'),$4,$5)`, tenant, instance, key, r.Intn(6), refs[r.Intn(len(refs))]); err != nil {
				t.Fatal(err)
			}
		}
		scan := func(q string) string {
			var k, ref string
			var out []byte
			e := conn.QueryRowContext(ctx, q, tenant, instance).Scan(&k, &ref, &out)
			if errors.Is(e, sql.ErrNoRows) {
				return "<none>"
			}
			if e != nil {
				t.Fatal(e)
			}
			return k + "|" + ref + "|" + string(out)
		}
		a, b := scan(oldSQL), scan(newSQL)
		if a != "<none>" {
			nonEmpty++
		}
		if a != b {
			differences++
			if differences <= 5 {
				t.Errorf("dataset %d differs: old %q new %q", i, a, b)
			}
		}
	}
	t.Logf("%d random receipt sets (%d with a pending compensation): the two statements differ on %d", datasets, nonEmpty, differences)
}
```

### A.14 Third pass at `0e13abe` - the budget when it is full and when it comes back, bodies declared and never sent, anonymous callers on the routes that used to be read, isolation between peers and identities, the counter of abandoned provider calls, the stored model labels

File: `tests/integration/zz_review_fourth_test.go` - needs the repository at `0e13abe` (and `127.0.0.2` as a second loopback peer address); the isolation test is random in which honest request wins a token of the rate gate, so its served/429 split differs from run to run

```go
//go:build integration

package integration

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
)

// Review probes for the third pass (head 0e13abe), second batch. Not product code.

// holdBudget opens connections that fill the budget: identities x perIdentity connections, each sending a body one byte short of the cap.
func holdBudget(t *testing.T, e *m14Env, addr, path string, identities, perIdentity int) (conns []net.Conn, sent int64) {
	t.Helper()
	almost := bytes.Repeat([]byte("a"), 1<<20-1)
	for i := 0; i < identities; i++ {
		token := e.f.token(t, uuid(t), nil)
		for j := 0; j < perIdentity; j++ {
			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			conns = append(conns, c)
			if _, err = c.Write(append([]byte(m14Head(addr, path, token, 1<<20)), almost...)); err != nil {
				t.Fatal(err)
			}
			sent += int64(len(almost))
		}
	}
	t.Cleanup(func() {
		for _, c := range conns {
			c.Close()
		}
	})
	return conns, sent
}

// The starvation probe of the first batch declared 40 bytes and sent 38, which made the honest request wait for two bytes; the same
// attack with exact lengths, and the moment the denial ends without further transmission.
func TestReviewBodyBudgetRefusesWhenFull(t *testing.T) {
	budget := access.NewBodyBudget(access.DefaultBodyBudgetBytes, access.DefaultBodyBudgetPerUser)
	e := m14Setup(t, m14Options{bodies: budget})
	e.draft("500")
	addr := m14Serve(t, e)
	path := e.root + "/ai/patch-proposals"
	started := time.Now()
	heapBefore := heapInUse()
	_, sent := holdBudget(t, e, addr, path, 16, 2)
	time.Sleep(1500 * time.Millisecond)
	used, subjects := budget.InFlight()
	t.Logf("%d identities hold %d of %d bytes (%d identities) after %v; the heap grew by %.1f MiB while the budget is full", 16, used, access.DefaultBodyBudgetBytes, subjects, time.Since(started).Round(time.Millisecond), float64(int64(heapInUse())-int64(heapBefore))/(1<<20))
	body := []byte(`{"request":"Rename the order entity."}`)
	for _, who := range []struct{ name, token string }{{"tenant owner", e.alice}, {"admin of the same tenant", e.carol.token}, {"admin of ANOTHER tenant", e.f.token(t, e.f.bobSubject, nil)}} {
		s, code, took := m14RawPost(t, addr, path, who.token, len(body), body, 5*time.Second)
		t.Logf("honest %-26s POST during the attack -> %d %s after %v", who.name, s, code, took.Round(time.Millisecond))
	}
	// A request with no body (nothing to book) and a GET are not affected.
	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/tenants/"+e.f.tenantA+"/access", nil)
	req.Header.Set("Authorization", "Bearer "+e.viewer)
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	t.Logf("honest GET during the attack -> %d", resp.StatusCode)
	// When does the denial end if the attacker transmits nothing more? The server's read timeout ends the held connections.
	began := time.Now()
	var relief time.Duration
	for relief = 0; relief < 15*time.Second; relief = time.Since(began) {
		s, code, _ := m14RawPost(t, addr, path, e.alice, len(body), body, 2*time.Second)
		if !(s == 503 && code == "service_busy") { // the scripted provider has no step, so an admitted request ends in another 503
			t.Logf("first honest POST admitted again (status %d %s) %v after the poll began, %v after the attack began", s, code, relief.Round(100*time.Millisecond), time.Since(started).Round(100*time.Millisecond))
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Logf("RESULT: to hold the denial the attacker transmits %.1f MiB per read timeout (10 s): %.1f Mbit/s from 16 identities", float64(sent)/(1<<20), float64(sent)*8/1e6/10)
}

// Attackers flood with bodies that exceed the front door's cap from one identity and one peer address; honest users come from another peer
// address (127.0.0.2 is loopback too) with other identities. The per-identity share and the per-peer rate limit must keep them apart; the same
// honest users on the attackers' peer are measured as well, to show what the shared peer costs.
func TestReviewBudgetIsolationAcrossPeersAndIdentities(t *testing.T) {
	budget := access.NewBodyBudget(access.DefaultBodyBudgetBytes, access.DefaultBodyBudgetPerUser)
	e := m14Setup(t, m14Options{bodies: budget, limits: ai.ServiceLimits{InFlightPerTenant: 4, DailyTokens: 50_000_000}})
	doc := e.draft("500")
	addr := m14Serve(t, e)
	path := e.root + "/ai/patch-proposals"
	from := func(ip string) *http.Client {
		return &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, DialContext: (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(ip)}}).DialContext}}
	}
	honestGET := func(ip string) string {
		req, _ := http.NewRequest("GET", "http://"+addr+"/v1/tenants/"+e.f.tenantA+"/access", nil)
		req.Header.Set("Authorization", "Bearer "+e.viewer)
		s := time.Now()
		resp, err := from(ip).Do(req)
		if err != nil {
			return "error " + err.Error()
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return fmt.Sprintf("%d%s in %v", resp.StatusCode, codeNote(resp.StatusCode, raw), time.Since(s).Round(time.Millisecond))
	}
	honestPropose := func(ip, token string, n int) string {
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", fmt.Sprintf("Honest %d", n)), Delay: 300 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
		req, _ := http.NewRequest("POST", "http://"+addr+path, strings.NewReader(`{"request":"Rename the order entity."}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		s := time.Now()
		resp, err := from(ip).Do(req)
		if err != nil {
			return "error " + err.Error()
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return fmt.Sprintf("%d%s in %v", resp.StatusCode, codeNote(resp.StatusCode, raw), time.Since(s).Round(time.Millisecond))
	}
	// attackers: six connections of the tenant owner, each sending 1.2 MiB (declared 2 MiB) again and again, from 127.0.0.1
	stop := time.Now().Add(14 * time.Second)
	var wg sync.WaitGroup
	var mu sync.Mutex
	answers := map[string]int{}
	big := bytes.Repeat([]byte("a"), 1_200_000)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(stop) {
				c, err := (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}}).Dial("tcp", addr)
				if err != nil {
					return
				}
				_, _ = c.Write(append([]byte(m14Head(addr, path, e.alice, 2<<20)), big...))
				s, code, _ := m14Answer(c, 5*time.Second)
				c.Close()
				mu.Lock()
				answers[fmt.Sprintf("%d %s", s, code)]++
				mu.Unlock()
			}
		}()
	}
	time.Sleep(3 * time.Second)
	t.Logf("honest viewer GET from ANOTHER peer (127.0.0.2): %s", honestGET("127.0.0.2"))
	t.Logf("honest admin (carol) proposal from ANOTHER peer, another identity: %s", honestPropose("127.0.0.2", e.carol.token, 1))
	t.Logf("honest tenant owner (the attacked identity) proposal from another peer: %s", honestPropose("127.0.0.2", e.alice, 2))
	t.Logf("honest viewer GET from the ATTACKERS' peer (127.0.0.1): %s", honestGET("127.0.0.1"))
	t.Logf("honest admin (carol) proposal from the ATTACKERS' peer: %s", honestPropose("127.0.0.1", e.carol.token, 3))
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("what the attackers were answered in 14 s: %v", answers)
	deadline := time.Now().Add(12 * time.Second)
	for {
		if used, subjects := budget.InFlight(); used == 0 && subjects == 0 {
			break
		}
		if time.Now().After(deadline) {
			used, subjects := budget.InFlight()
			t.Errorf("the booked bytes were not returned: %d bytes, %d identities", used, subjects)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// codeNote names the "code" of an error answer, so that a 429 of the rate gate and a 429 of the body budget can be told apart.
func codeNote(status int, raw []byte) string {
	if status < 400 {
		return ""
	}
	text := string(raw)
	if i := strings.Index(text, `"code":"`); i >= 0 {
		if j := strings.Index(text[i+8:], `"`); j >= 0 {
			return " " + text[i+8:i+8+j]
		}
	}
	return " -"
}

// An adapter that ignores its context: the gateway abandons the call at the wall clock and counts it, refuses new calls once 16 are still
// running (ai/repair.go, maxAbandonedCalls), and must count them down when the adapters return.
type hangForever struct {
	release chan struct{}
	calls   atomic.Int32
}

func (h *hangForever) Complete(ctx context.Context, req ai.ProviderRequest) (ai.ProviderResponse, error) {
	h.calls.Add(1)
	<-h.release
	return ai.ProviderResponse{}, errors.New("released")
}

func TestReviewAbandonedCallsAreBoundedAndReleased(t *testing.T) {
	hang := &hangForever{release: make(chan struct{})}
	e := m14Setup(t, m14Options{gateway: ai.NewGateway(hang, ai.Options{}), limits: ai.ServiceLimits{WallClock: time.Second, InFlightPerTenant: 8, DailyTokens: 50_000_000}})
	e.draft("500")
	var codes []string
	started := time.Now()
	for i := 0; i < 20; i++ {
		w := e.propose(e.alice, "Rename the order entity.")
		codes = append(codes, fmt.Sprintf("%d %s", w.Code, m14Code(w)))
	}
	reached := hang.calls.Load()
	t.Logf("20 requests in %v: %d reached the adapter; answers %v", time.Since(started).Round(time.Millisecond), reached, codes)
	if reached != 16 {
		t.Errorf("%d calls reached an adapter that never returns, want exactly the 16 that are tolerated", reached)
	}
	// let the adapters return: the abandoned count must fall back to zero, so that the next request reaches the adapter again
	close(hang.release)
	time.Sleep(500 * time.Millisecond)
	w := e.propose(e.alice, "Rename the order entity.")
	t.Logf("after the adapters returned: HTTP %d %s; calls that reached the adapter %d", w.Code, m14Code(w), hang.calls.Load())
	if hang.calls.Load() != reached+1 {
		t.Errorf("the abandoned count did not fall back: the next request did not reach the adapter (%d calls)", hang.calls.Load())
	}
}

// When is the budget given back? The probe above polls with honest requests; this one watches the budget itself (InFlight) and what the server does
// to the connections of the attackers, every 100 ms for 30 s, with one honest POST per second.
func TestReviewBudgetIsReleasedAtTheReadTimeout(t *testing.T) {
	budget := access.NewBodyBudget(access.DefaultBodyBudgetBytes, access.DefaultBodyBudgetPerUser)
	e := m14Setup(t, m14Options{bodies: budget})
	e.draft("500")
	addr := m14Serve(t, e)
	path := e.root + "/ai/patch-proposals"
	started := time.Now()
	conns, _ := holdBudget(t, e, addr, path, 16, 2)
	body := []byte(`{"request":"Rename the order entity."}`)
	lastUsed, lastSubjects := int64(-1), -1
	lastHonest := ""
	var lastPost time.Time
	closedAt := map[int]time.Duration{}
	answers := map[int]string{}
	for time.Since(started) < 30*time.Second {
		used, subjects := budget.InFlight()
		if used != lastUsed || subjects != lastSubjects {
			t.Logf("t=%6.2fs budget in flight: %d bytes, %d identities", time.Since(started).Seconds(), used, subjects)
			lastUsed, lastSubjects = used, subjects
		}
		// what happened to the connections of the attackers: a response, a close, or nothing yet
		for i, c := range conns {
			if _, done := closedAt[i]; done {
				continue
			}
			_ = c.SetReadDeadline(time.Now().Add(time.Millisecond))
			resp, err := http.ReadResponse(bufio.NewReader(c), nil)
			if err == nil {
				closedAt[i] = time.Since(started)
				answers[i] = resp.Status
				resp.Body.Close()
			} else if ne, ok := err.(interface{ Timeout() bool }); !ok || !ne.Timeout() {
				closedAt[i] = time.Since(started)
				answers[i] = "closed: " + err.Error()
			}
		}
		if time.Since(lastPost) >= time.Second {
			lastPost = time.Now()
			s, code, took := m14RawPost(t, addr, path, e.alice, len(body), body, 2*time.Second)
			now := ""
			if s != 0 {
				now = http.StatusText(s)
			}
			honest := now + " " + code
			if honest != lastHonest {
				t.Logf("t=%6.2fs honest POST -> %d %s (%v)", time.Since(started).Seconds(), s, code, took.Round(time.Millisecond))
				lastHonest = honest
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	byAnswer := map[string]int{}
	var first, last time.Duration
	for i, d := range closedAt {
		byAnswer[answers[i]]++
		if first == 0 || d < first {
			first = d
		}
		if d > last {
			last = d
		}
	}
	t.Logf("of %d attacker connections, %d were answered or closed by the server between %.2fs and %.2fs: %v", len(conns), len(closedAt), first.Seconds(), last.Seconds(), byAnswer)
}

// F-06 of the second review: booking the declared length let identities without a membership fill the budget with connections that
// declared a body and sent none. Sixty-four such connections of sixteen identities must book nothing, and an honest request must be served.
func TestReviewDeclaredButUnsentBodiesBookNothing(t *testing.T) {
	budget := access.NewBodyBudget(access.DefaultBodyBudgetBytes, access.DefaultBodyBudgetPerUser)
	e := m14Setup(t, m14Options{bodies: budget, limits: ai.ServiceLimits{InFlightPerTenant: 4, DailyTokens: 50_000_000}})
	doc := e.draft("500")
	addr := m14Serve(t, e)
	path := e.root + "/ai/patch-proposals"
	var conns []net.Conn
	t.Cleanup(func() {
		for _, c := range conns {
			c.Close()
		}
	})
	for i := 0; i < 16; i++ {
		token := e.f.token(t, uuid(t), nil)
		for j := 0; j < 4; j++ {
			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			conns = append(conns, c)
			if _, err = c.Write([]byte(m14Head(addr, path, token, 1<<20))); err != nil {
				t.Fatal(err)
			}
		}
	}
	time.Sleep(1500 * time.Millisecond)
	used, subjects := budget.InFlight()
	t.Logf("%d connections of 16 identities declared 1 MiB each and sent nothing: the budget holds %d bytes for %d identities", len(conns), used, subjects)
	if used != 0 || subjects != 0 {
		t.Errorf("declared but unsent bodies booked %d bytes for %d identities", used, subjects)
	}
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Honest"), Delay: 300 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
	body := []byte(`{"request":"Rename the order entity."}`)
	s, code, took := m14RawPost(t, addr, path, e.alice, len(body), body, 5*time.Second)
	t.Logf("honest owner POST during the attack -> %d %s after %v", s, code, took.Round(time.Millisecond))
	if s != 201 {
		t.Errorf("an honest proposal was not served while 64 connections declared bodies and sent nothing: %d %s", s, code)
	}
}

// F-02 of the second review: "an anonymous caller is never read" was false on the two commands routes and the two content-idempotent release POSTs, which
// read their body before authentication. Each of the four, and one route that always pre-read, with no bearer and with an invalid bearer, declares
// 900,000 bytes and sends one: the answer must be the 401, not a wait for the read timeout.
func TestReviewAnonymousCallersAreNotReadOnAnyRoute(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	addr := m14Serve(t, e)
	id := uuid(t)
	routes := []struct{ name, path string }{
		{"commands (version)", e.root + "/versions/1/entities/entity.order/commands"},
		{"commands (binding)", e.root + "/datasets/" + id + "/bindings/1/entities/entity.order/commands"},
		{"candidates (release POST)", e.root + "/candidates"},
		{"acceptance-specs (release POST)", e.root + "/acceptance-specs"},
		{"ai patch-proposals (control)", e.root + "/ai/patch-proposals"},
	}
	var slowest time.Duration
	for _, r := range routes {
		for _, token := range []string{"", "not-a-token"} {
			c, err := net.Dial("tcp", addr)
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			_, _ = c.Write([]byte(m14Head(addr, r.path, token, 900_000) + "x"))
			status, code, _ := m14Answer(c, 15*time.Second)
			took := time.Since(started)
			c.Close()
			kind := "no bearer"
			if token != "" {
				kind = "invalid bearer"
			}
			t.Logf("%-32s %-14s -> %d %s after %v", r.name, kind, status, code, took.Round(time.Millisecond))
			if status != 401 || took > 2*time.Second {
				t.Errorf("%s with %s: %d %s after %v (want 401 at once)", r.name, kind, status, code, took)
			}
			if took > slowest {
				slowest = took
			}
		}
	}
	t.Logf("slowest answer %v", slowest.Round(time.Millisecond))
}

// What the audit trail keeps of a provider-chosen label after the reduction (F-07 of the second review). The builders state a residual: a leading run
// of non-ASCII letters is dropped and no hash of the raw label is kept, so two labels with the same ASCII tail collapse. This reads what is stored.
func TestReviewAIStoredModelLabels(t *testing.T) {
	for _, label := range []string{"alpha 4 turbo", "Beta-3.1 (70B)", "модель-1", "модель-2", "model=fast", strings.Repeat("x", 200), "a\x00b\nc", "ok‮evil", "../../x"} {
		label := label
		t.Run(fmt.Sprintf("%.12q", label), func(t *testing.T) {
			e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
			doc := e.draft("500")
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Renamed"), Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: label})
			w := e.propose(e.alice, "Rename the order entity.")
			var stored string
			_ = e.f.operator.QueryRow(`SELECT model FROM control.ai_invocations WHERE tenant_id=$1 LIMIT 1`, e.f.tenantA).Scan(&stored)
			t.Logf("provider label %q -> HTTP %d, stored %q (%d bytes)", label, w.Code, stored, len(stored))
			if w.Code != 201 {
				t.Errorf("label %q: HTTP %d", label, w.Code)
			}
		})
	}
}
```

### A.15 Third pass at `0e13abe` - the compensation statement before and after the rewrite, timed in the worker session

File: `tests/integration/zz_review_workerpolicy2_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

// Review probe (not product code), third pass: the set-up of TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry with a 60 s statement
// timeout, and the statement it replaced against the statement that replaced it (workflow.CompensationScanSQL), on the same 96 receipts,
// in the same worker session. Each is timed three times.
func TestReviewWorkerPolicyScanTimingOldVsNew(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "sql-statement-policy")
	h := r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	d := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "ALL"})
	condition := policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}}
	children := make([]policy.DataScope, 8)
	for i := range children {
		children[i] = condition
	}
	scope := policy.DataScope{Kind: "OR", Children: children}
	grant := d.Grants[1]
	grant.Scope = &scope
	d.Grants = append(d.Grants[:1], d.Grants[2:]...)
	for i := 0; i < 7; i++ {
		d.Grants = append(d.Grants, grant)
	}
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, h, root+"/roles", m.token, 0, d, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, d.Name, 200)
	// Bounded synthetic scan, not evidence of a full 96-step business lifecycle.
	if _, e := m.f.operator.Exec(`INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) SELECT $1,$2,'scan-'||g,'local.noop',repeat('a',64),convert_to('{}','UTF8'),encode(sha256(convert_to('{}','UTF8')),'hex'),g,'local.noop' FROM generate_series(1,96) g`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	bind := func() *sql.Tx {
		t.Helper()
		tx, e := m.db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		if ok, e := bindM9R(tx, lease); e != nil || !ok {
			tx.Rollback()
			t.Fatal("scan lease", e)
		}
		if _, e = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true),set_config('statement_timeout','60000',true)`, m.f.alice, m.f.aliceSubject); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		return tx
	}
	const query = `SELECT r.step_key FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`
	tx := bind()
	defer tx.Rollback()
	timeOne := func(label, q string, cols int) {
		for i := 0; i < 3; i++ {
			start := time.Now()
			var step, ref string
			var out []byte
			var e error
			if cols == 1 {
				e = tx.QueryRow(q, m.f.tenantA, x.ID).Scan(&step)
			} else {
				e = tx.QueryRow(q, m.f.tenantA, x.ID).Scan(&step, &ref, &out)
			}
			if e != nil || step != "scan-96" {
				t.Fatal(label, e, step)
			}
			t.Logf("REVIEW %s run %d: %s (step %s)", label, i+1, time.Since(start), step)
		}
	}
	timeOne("old statement (correlated NOT EXISTS)", query, 1)
	timeOne("new statement (uncorrelated NOT IN)  ", workflow.CompensationScanSQL, 3)
}
```

### A.16 Third pass at `0e13abe` - which launcher spellings are recognised (Node, no database)

File: `a scratch script` - run as `node launcher-spellings.mjs <path to scripts/runtime-env.mjs>`

```js
// Review probe (not product code): which spellings of "start a backend process" does runtimeKindOf recognise, and which are handed the whole environment?
// Usage: node launcher-spellings.mjs <path to scripts/runtime-env.mjs>
import { pathToFileURL } from 'node:url'
const { runtimeKindOf } = await import(pathToFileURL(process.argv[2]).href)
const startsWorker = [
  ['go', ['run', './cmd/worker']], ['go', ['run', 'cmd/worker']], ['go', ['run', './cmd/worker/']], ['go', ['run', './cmd/./worker']], ['go', ['run', 'cmd/../cmd/worker']],
  ['go', ['run', 'github.com/Karthikeyan49/Framwork/cmd/worker']], ['go', ['run', './cmd/worker/main.go']], ['go', ['run', '-tags', 'x', './cmd/worker']],
  ['./scripts/go.sh', ['run', './cmd/worker']], ['bin/worker', []], ['./bin/worker', []], ['/abs/path/bin/worker', []], ['bin\\worker', []],
  ['./bin/api', ['-flag']], ['env', ['A=1', 'bin/worker']], ['nice', ['-n', '5', 'bin/dispatcher']], ['bash', ['-c', 'bin/worker']], ['sh', ['-c', './bin/worker']],
]
const shellWrapped = [
  ['sh', ['-c', './bin/worker --flag']], ['bash', ['-c', 'cd . && ./bin/worker']], ['bash', ['-lc', 'exec bin/api']], ['sh', ['-c', 'go run ./cmd/worker']], ['sh', ['-c', 'GOFLAGS=-v go run ./cmd/api']],
  ['go', ['run', './cmd/worker@latest']], ['go', ['run', './cmd/worker/...']], ['go', ['run', './cmd/wor*']], ['go', ['run', '-C', '.', 'cmd/worker']], ['go', ['run', '-C', 'cmd', 'worker']],
  ['go', ['run', 'cmd/WORKER']], ['go', ['run', './cmd/worker ']], ['./w', []], ['node', ['-e', "require('child_process').spawn('bin/worker')"]], ['make', ['run-worker']], ['npm', ['run', 'worker']],
]
const show = (rows, label) => {
  console.log(label)
  for (const [cmd, args] of rows) console.log(String(runtimeKindOf(cmd, args) ?? 'NONE (whole environment)').padEnd(26), [cmd, ...args].join(' '))
}
show(startsWorker, '== spellings that start a backend directly (the kind should be named)')
show(shellWrapped, '== fringe spellings (a kind here is a bonus; NONE is a spelling that is not recognised)')
```

### A.17 Third pass at `0e13abe` - spellings of a single-version gate that the scan of `version_family.go` does not see (unit test, no database)

File: `internal/metadata/zz_review_gate_test.go` - needs the repository at `0e13abe`

```go
package metadata

import (
	"strings"
	"testing"
)

// Review probe (not product code), third pass. F-03 of the second review was re-verified only by the builders' own tests; this asks the scan
// of version_family.go about spellings of a gate on 1.4.0 alone that the builders' list does not contain. A spelling is MISSED when the
// scan names no later member of the family as left out (it names no version at all, or it names the whole family).
func TestReviewVersionGateSpellingsBeyondTheBuildersList(t *testing.T) {
	// the control: the builders' own spellings must be reported (they are tested in sql_version_gate_test.go; repeated here with the same helper)
	control := map[string]string{
		"literal":       `WHERE m.schema_version='1.4.0'`,
		"concatenation": `WHERE m.schema_version='1.4.'||'0'`,
		"dollar":        `WHERE m.schema_version=$v$1.4.0$v$`,
		"jsonb":         `WHERE m.document @> '{"schema_version":"1.4.0"}'`,
		"LIKE prefix":   `WHERE m.schema_version LIKE '1.4%'`,
		"regex":         `WHERE m.schema_version ~ '^1\.4\.'`,
	}
	for name, body := range control {
		if len(AuthoringFamilyGaps(AuthoringVersionsNamed(body))) == 0 {
			t.Errorf("control %q is not reported: the probe is not comparable with the builders' test", name)
		}
	}
	more := []struct{ name, body string }{
		{"array literal, one member", `WHERE m.schema_version = ANY('{1.4.0}'::text[])`},
		{"array literal, 1.3.0 and 1.4.0", `WHERE m.schema_version = ANY('{1.3.0,1.4.0}'::text[])`},
		{"array literal through unnest", `WHERE m.schema_version IN (SELECT unnest('{1.4.0}'::text[]))`},
		{"comma list in one literal", `WHERE m.schema_version = ANY(string_to_array('1.4.0,1.3.0',','))`},
		{"concatenation with a number", `WHERE m.schema_version = '1.'||4||'.0'`},
		{"format", `WHERE m.schema_version = format('1.%s.0', 4)`},
		{"split_part on the minor", `WHERE split_part(m.schema_version,'.',2) = '4'`},
		{"split_part cast to int", `WHERE split_part(m.schema_version,'.',2)::int = 4`},
		{"substring of the minor", `WHERE substr(m.schema_version,3,1) = '4'`},
		{"regex with a character class", `WHERE m.schema_version ~ '^1[.]4[.]'`},
		{"LIKE with an underscore", `WHERE m.schema_version LIKE '1_4%'`},
		{"SIMILAR TO", `WHERE m.schema_version SIMILAR TO '1.4.%'`},
		{"left()", `WHERE left(m.schema_version,3) = '1.4'`},
		{"starts_with", `WHERE starts_with(m.schema_version,'1.4')`},
		{"typed literal", `WHERE m.schema_version = text '1.4.0'`},
		{"cast of a literal", `WHERE m.schema_version = CAST('1.4.0' AS text)`},
		{"a variable assigned from a literal", `v:='1.4.0'; WHERE m.schema_version = v`},
		{"a jsonpath with single quotes", `WHERE jsonb_path_exists(m.document, '$.schema_version ? (@ == "1.4.0")')`},
		{"a jsonb array literal", `WHERE m.document->'versions' @> '["1.4.0"]'`},
		{"a comparison", `WHERE m.schema_version = '1.4.0'::text`},
		{"two-digit minor in the family", `WHERE m.schema_version = '1.4.0' OR m.schema_version = '1.10.0'`},
	}
	var missed []string
	for _, c := range more {
		named := AuthoringVersionsNamed(c.body)
		gaps := AuthoringFamilyGaps(named)
		verdict := "reported"
		if len(gaps) == 0 {
			verdict = "MISSED"
			missed = append(missed, c.name)
		}
		t.Logf("%-40s names %-14v -> %s", c.name, named, verdict)
	}
	t.Logf("%d of %d spellings of a gate on 1.4.0 alone that are not in the builders' list are not reported: %s", len(missed), len(more), strings.Join(missed, "; "))
}
```

### A.18 Third pass at `0e13abe` - the live catalog read with a looser net than the builders' scan, and the privilege inventory of the four guarded schemas on the tip (read-only queries on a disposable database)

File: `tests/integration/zz_review_catalog_test.go` - needs the repository at `0e13abe`

```go
//go:build integration

package integration

import (
	"database/sql"
	"regexp"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Review probe (not product code), third pass. The scan of the live catalog (m9r_catalog_version_gate_test.go) recognises a version only in the
// spellings of version_family.go. This reads the same 1,000-odd definitions with a looser net: every definition that mentions schema_version, or
// anything shaped like an authoring version, where the builders' scan names no version at all. What it prints is what the scan is blind to
// today; each line is read by hand in the report.
func TestReviewCatalogLooseVersionReferences(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	const userSchemas = `n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg\_%'`
	rows, err := f.operator.Query(`
	SELECT 'function', n.nspname||'.'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')', pg_get_functiondef(p.oid)
	  FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE ` + userSchemas + ` AND p.prokind IN ('f','p')
	UNION ALL
	SELECT 'constraint', coalesce(NULLIF(c.conrelid::regclass::text,'-'),'domain '||c.contypid::regtype::text)||'.'||c.conname, pg_get_constraintdef(c.oid)
	  FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE ` + userSchemas + ` AND c.contype IN ('c','x')
	UNION ALL
	SELECT 'policy', pol.polrelid::regclass::text||'.'||pol.polname, coalesce(pg_get_expr(pol.polqual,pol.polrelid),'')||' '||coalesce(pg_get_expr(pol.polwithcheck,pol.polrelid),'')
	  FROM pg_policy pol JOIN pg_class c ON c.oid=pol.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE ` + userSchemas + `
	UNION ALL
	SELECT 'trigger', t.tgrelid::regclass::text||'.'||t.tgname, pg_get_triggerdef(t.oid)
	  FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE NOT t.tgisinternal AND ` + userSchemas + `
	UNION ALL
	SELECT 'default', d.adrelid::regclass::text||'.'||a.attname, pg_get_expr(d.adbin,d.adrelid)
	  FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid=d.adrelid AND a.attnum=d.adnum JOIN pg_class c ON c.oid=d.adrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE ` + userSchemas + `
	UNION ALL
	SELECT 'view', c.oid::regclass::text, pg_get_viewdef(c.oid)
	  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('v','m') AND ` + userSchemas + `
	UNION ALL
	SELECT 'index', i.indexrelid::regclass::text, pg_get_indexdef(i.indexrelid)
	  FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE ` + userSchemas)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	loose := regexp.MustCompile(`(?is)schema_version|[^0-9.a-z_]1\.[0-9]+\.[0-9]+|\{1\.[0-9]`)
	versionShaped := regexp.MustCompile(`[^0-9.a-z_]1\.[0-9]+(\.[0-9]+)?|\{1\.[0-9]`)
	seen, namedByScan, blind := 0, 0, 0
	for rows.Next() {
		var kind, name, definition string
		if err = rows.Scan(&kind, &name, &definition); err != nil {
			t.Fatal(err)
		}
		seen++
		if len(metadata.AuthoringVersionsNamed(definition)) > 0 {
			namedByScan++
			continue
		}
		loc := loose.FindStringIndex(definition)
		if loc == nil {
			continue
		}
		blind++
		from, to := max(0, loc[0]-70), min(len(definition), loc[1]+90)
		shaped := versionShaped.FindAllString(definition, -1)
		t.Logf("BLIND %-10s %s: version-shaped tokens %q; first mention: ...%s...", kind, name, shaped, strings.Join(strings.Fields(definition[from:to]), " "))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d definitions read, %d are named by the scan, %d mention schema_version or a version-shaped token that the scan does not see", seen, namedByScan, blind)
}

// The privilege inventory of the four guarded schemas (control, runtime, iam, audit) on a database migrated to the tip: the same questions that were asked
// of the merge at the pinned commit (read-only catalog queries), now that 00091 and 00092 exist.
func TestReviewCatalogInventoryAtTheTip(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	for _, q := range []struct{ label, sql string }{
		{"secdef functions", `select count(*) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and p.prosecdef`},
		{"secdef without the pg_catalog,pg_temp search_path", `select count(*) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and p.prosecdef and not coalesce(p.proconfig::text[] @> array['search_path=pg_catalog, pg_temp'], false)`},
		{"  ... which", `select coalesce(string_agg(p.oid::regprocedure::text||' ['||array_to_string(p.proconfig,';')||']', ', ' order by 1),'-') from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and p.prosecdef and not coalesce(p.proconfig::text[] @> array['search_path=pg_catalog, pg_temp'], false)`},
		{"secdef not owned by framework_schema_owner", `select count(*) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and p.prosecdef and pg_get_userbyid(p.proowner) <> 'framework_schema_owner'`},
		{"functions executable by PUBLIC in the four schemas", `select count(*) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and exists(select 1 from aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a where a.grantee=0 and a.privilege_type='EXECUTE')`},
		{"tables: all / RLS enabled / RLS forced", `select count(*)||' / '||count(*) filter (where c.relrowsecurity)||' / '||count(*) filter (where c.relforcerowsecurity) from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname in ('control','runtime','iam','audit') and c.relkind in ('r','p')`},
		{"relations with a table-level privilege of framework_worker", `select count(*) from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname in ('control','runtime','iam','audit') and c.relkind in ('r','p','v','m','f') and (has_table_privilege('framework_worker', c.oid, 'SELECT') or has_table_privilege('framework_worker', c.oid, 'INSERT') or has_table_privilege('framework_worker', c.oid, 'UPDATE') or has_table_privilege('framework_worker', c.oid, 'DELETE'))`},
		{"functions the worker may execute (not through PUBLIC)", `select count(*) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and has_function_privilege('framework_worker', p.oid, 'EXECUTE') and not exists(select 1 from aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a where a.grantee=0 and a.privilege_type='EXECUTE')`},
		{"functions the dispatcher may execute (not through PUBLIC)", `select count(*) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname in ('control','runtime','iam','audit') and has_function_privilege('framework_dispatcher', p.oid, 'EXECUTE') and not exists(select 1 from aclexplode(coalesce(p.proacl, acldefault('f', p.proowner))) a where a.grantee=0 and a.privilege_type='EXECUTE')`},
		{"new ports executable by app / worker / dispatcher (release_candidate_fence, approve_release, rollback_release)", `select string_agg(p.proname||':'||has_function_privilege('framework_app', p.oid,'EXECUTE')||'/'||has_function_privilege('framework_worker', p.oid,'EXECUTE')||'/'||has_function_privilege('framework_dispatcher', p.oid,'EXECUTE'), ', ' order by p.proname) from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname='control' and p.proname in ('release_candidate_fence','approve_release','rollback_release')`},
		{"worker guard allowlist rows / held / stale", `select count(*)||' / '||count(*) filter (where (g.col is null and has_table_privilege('framework_worker', g.rel, g.priv)) or (g.col is not null and has_column_privilege('framework_worker', g.rel, g.col, g.priv)))||' / '||count(*) filter (where not ((g.col is null and has_table_privilege('framework_worker', g.rel, g.priv)) or (g.col is not null and has_column_privilege('framework_worker', g.rel, g.col, g.priv)))) from control.worker_guard_granted() g`},
		{"references to control|runtime|iam|audit objects / distinct names / unresolved", `with refs as (select m[1] as sch, m[2] as nm from pg_proc p join pg_namespace n on n.oid=p.pronamespace, lateral regexp_matches(p.prosrc, '\m(control|runtime|iam|audit)\.([a-z_][a-z0-9_]*)', 'g') m where n.nspname in ('control','runtime','iam','audit')), unresolved as (select distinct sch, nm from refs r where not exists(select 1 from pg_class c join pg_namespace n on n.oid=c.relnamespace where n.nspname=r.sch and c.relname=r.nm) and not exists(select 1 from pg_proc q join pg_namespace n on n.oid=q.pronamespace where n.nspname=r.sch and q.proname=r.nm) and not exists(select 1 from pg_type t join pg_namespace n on n.oid=t.typnamespace where n.nspname=r.sch and t.typname=r.nm)) select (select count(*) from refs)||' / '||(select count(distinct sch||'.'||nm) from refs)||' / '||(select count(*) from unresolved)`},
		{"TEMP / CREATE on the database for app, worker, dispatcher", `select string_agg(r||':'||has_database_privilege(r, current_database(), 'TEMP')||'/'||has_database_privilege(r, current_database(), 'CREATE'), ', ' order by r) from unnest(array['framework_app','framework_worker','framework_dispatcher']) r`},
	} {
		var out sql.NullString
		if err := f.operator.QueryRow(q.sql).Scan(&out); err != nil {
			t.Errorf("%s: %v", q.label, err)
			continue
		}
		t.Logf("%-86s %s", q.label, out.String)
	}
}
```
