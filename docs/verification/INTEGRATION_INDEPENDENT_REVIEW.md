# Independent review of the integration branch `integration/m9r-m14`

Reviewer: independent agent, no part in building either line; no product code was changed. Pinned commit
`552fa9efe2092a592e39974ee23ec1ecbc057729` (head of `origin/integration/m9r-m14` when the review started). Review branch
`review/integration-final`. Rules of truth: PASS / FAIL / NOT RUN. Every "held" names the attack that was executed.
The M14 half of the review is `docs/verification/M14_INDEPENDENT_REVIEW.md`.

STATUS: COMPLETE for the attacks listed (what was not covered: last section). Overall: **FAIL** - 3 MEDIUM (IR-01, IR-02, IR-06),
3 LOW (IR-03 to IR-05), no HIGH. The merge kept the authority and tenant-isolation invariants of both lines in every attack executed; what it
did not keep is one meaning of a version token in one SQL literal (IR-01), the availability of the admission slots (IR-02, inherited), one M13
fence (IR-06), a gate that reproduces (IR-03) and truthful ledgers (IR-04).

## Verdict by decision of the plan

| # | Decision | Verdict | Deciding attack |
|---|---|---|---|
| 1 | One meaning per version token (authoring schema, IR and compiler 1.3.0 / 1.4.0 / 1.5.0; workflow DSL 2.0.0 / 2.1.0 / 2.2.0) | **FAIL** (IR-01) | `TestReviewUniqueFieldUnderEachSchemaVersion`; nine re-shaped IR artifacts restored (`TestReviewVersionTokenReadings`, held) |
| 2 | Migration order and the shared `metadata_versions_schema_version_check` | PASS (IR-05 LOW) | mechanical object intersection of both migration sets; `TestReviewMigrateOverMilestoneNumbering` |
| 3 | Derived worker guard `control.worker_grants_safe()` | PASS | evaluated in a real `framework_worker` session; privilege inventory of the four guarded schemas |
| 4 | R7b event authority against the M10 ledger and the M11 freeze | PASS | `TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts`; isolation-level read of `RuntimeSnapshot`, `jobs.go`, `freezePattern` |
| 5 | HTTP front door (`internal/platform/access/access.go`) | PASS for authority, **FAIL** for availability (IR-02) | `TestReviewRouteMatrix` (45 POST routes); `TestReviewSlowBodyHoldsAdmissionSlots` |
| 6 | Honesty of the documents | **FAIL** (IR-04) | reading against the tree |
| - | M13 fixes carried by the merge (migration `00088`) | **FAIL** (IR-06) | `TestReviewRollbackOvertakesConcurrentRejection` |

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

## Findings

Severity: HIGH exploitable violation of a security or integrity invariant; MEDIUM realistic integrity/availability defect or breach of a
documented contract; LOW hardening, documentation or test gap.

### IR-01 MEDIUM - `field.unique` cannot be enabled under authoring schema/IR 1.5.0, and every write to such an entity is refused

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

The plan says `MAIN` "has to be rebuilt, not migrated". Executed (`TestReviewMigrateOverMilestoneNumbering`, Appendix A): a database at 44 with
`goose_db_version` rows for the milestone numbers, then `migrate up`: with 45-53 the run stops at 54 with `column "aggregate_valid_v1" does not exist` and
nothing is applied; with 45-53 and 60 goose refuses ("4 missing (out-of-order) migrations lower than database version (60): versions 54,55,56,57"). It is safe,
but only because 54 happens to depend on objects of the skipped remediation migrations and 60 happens to exceed 57; there is no explicit guard (name or
checksum of the applied versions), so a milestone database that stopped at 53 and a later remediation migration that did not depend on 47-52 would be skipped
silently.

### IR-06 MEDIUM - `rollback_release` does not write the M13R-01 fence: a rollback that began before a committed rejection moves the pointer onto the rejected hash

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
	if upErr != nil || version != 89 || len(applied) != 11 {
		t.Errorf("the retry did not apply 57 and 80-89")
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
