# Independent review of the integration branch `integration/m9r-m14`

Reviewer: independent agent, no part in building either line. Pinned commit: `552fa9efe2092a592e39974ee23ec1ecbc057729`
(head of `origin/integration/m9r-m14` when the review started). Review branch: `review/integration-final`.
Rules of truth: PASS / FAIL / NOT RUN. Every "held" names the attack that was executed. No product code was changed.

STATUS: IN PROGRESS (interim commit). Sections marked "pending" are not finished.

## Progress (updated as the work goes on)

| Time (UTC, 2026-10-20) | Step | State |
|---|---|---|
| 13:58 | Docker daemon started (it was not running); no pre-existing containers or volumes | done |
| 14:02 | `make setup` (Go 1.27.2; Node 24.22.0 installed outside the repo, checksum verified), `make infra-up`, `make migrate` | 67 migrations applied (`00001`-`00057`, `00080`-`00089`) |
| 14:03 | `make check` | PASS, 50 s |
| 14:06 | `make test-unit` | first run: 1 FAIL (`internal/tools`, see IR-03); the node and web tests did not run in that invocation (make stops at the first failure) and were run separately: 9 + 126 PASS |
| 14:12-14:57 | full integration suite, `-race`, 439 listed / 438 run | 2716.8 s, **2 FAIL** (IR-03) |
| 14:58-15:25 | static review of the merge (decisions 1-6) and executed probes | see "Findings" and "Attacks that held" |
| pending | `make build`, `make test-e2e`, `make test-signin-e2e`, three fuzz targets, `make test-event-outage` | pending |

## Findings

Severity: HIGH exploitable violation of a security or integrity invariant; MEDIUM realistic integrity/availability defect
or breach of a documented contract; LOW hardening, documentation or test gap.

### IR-01 MEDIUM - `field.unique` cannot be enabled under authoring schema/IR 1.5.0, and every write to such an entity is refused

Contract: ADR100 Amendment I1: "`1.5.0` is a superset of `1.4.0`: it carries both and adds the tool activity";
`metadata.IntegritySchema(v)` is true for 1.4.0 and 1.5.0; the compiler, `validateIntegrity` and the renderer accept `unique` under 1.5.0.

Evidence: `db/migrations/00052_m9r_unique_field_integrity.sql:145` (function `runtime.register_unique_constraint`) selects the field
definition with `m.schema_version='1.4.0'`. No migration of either line replaced it, so under a 1.5.0 document the SELECT finds nothing,
`typ` stays NULL and the function raises `unsupported unique field` (22023), which the API answers as HTTP 400 `invalid_index`.
The record-write trigger `runtime.maintain_unique_claims` (`00052:209-231`) refuses (55000 `unique integrity not ready`) every INSERT/UPDATE of an
entity that declares a unique field without a READY claim - also a write that leaves the unique field out. The plan's Decision 1 lists every place that
names the versions ("the frontend renderer, the AI schema map, ..."); this SQL literal is the one it missed.

Executed (repro below, `TestReviewUniqueFieldUnderEachSchemaVersion`, the builders' own `uniqueFixture` with the schema version as a parameter):

| schema | create before registration | register | after register + build | create after |
|---|---|---|---|---|
| 1.4.0 | 409 (unique value) / 409 (field absent) | 200 BUILDING, then READY | READY | 200 / 200 |
| 1.5.0 | 409 / 409 | **400 `invalid_index`** | no claim can exist | **409 / 409 forever** |

Impact: an application that needs both tools (1.5.0 is the only schema that admits them) and a unique field cannot write to that entity at all;
the failure is closed (no duplicate is stored), so this is an availability/contract defect, not an integrity breach. The tool-capable contract
therefore does not carry integrity, although the plan, ADR100 I1 and the compiler say it does. No test exercises `unique` under 1.5.0
(`m9r_unique_*` use 1.4.0 only; the tool tests use 1.5.0 without `unique`).

Fix direction (forward-only, number from the free range 00090 or above, or 58-79 if the remediation agent agrees): replace
`runtime.register_unique_constraint` with the 1.4.0 body and `m.schema_version IN ('1.4.0','1.5.0')`; add the unique lifecycle test under both schemas;
add a rule to the plan's version checklist: grep the migrations for version literals.

### IR-02 MEDIUM - request bodies are read inside the database admission slot and transaction (inherited from the remediation line, extended by every M12-M14 POST route)

Contract: R7a (ADR061) "bounded process-local admission before protected API work"; `API_DB_CONCURRENCY` (default 4) bounds database work; the
commands route reads its body *before* the transaction (`access.go`, `localCommand`) precisely to avoid this.

Evidence: for every other POST route `serve` takes the admission slot (`Admission.Acquire`), opens the transaction and only then lets the handler
`io.ReadAll` the body (`metadata.go:45` at `b00da36`; `ai.go: aiBody`; the release, sandbox, dataset, role and team handlers likewise). The server's
`ReadTimeout` (10 s) is the only bound, and `postgres.WithRuntime` does not cancel a blocked `Body.Read`. The traffic gate counts requests, not holds
(defaults 300 per second per peer), so four requests suffice.

Executed (`TestReviewSlowBodyHoldsAdmissionSlots`, real `http.Server` from `httpserver.New`, four connections of the tenant owner, `Content-Length: 30000`,
one byte per 0.7 s): an honest `GET` waited **8.02 s** (baseline 10 ms); on the inherited metadata route **8.04 s** (so it is not specific to M14);
an honest AI proposal (scripted provider answers in 1.5 s) returned **503 after 9.54 s** and stayed **PENDING** (baseline 201 in 1.57 s): the result
exchange of an AI request needs a slot again after the provider phase and its 8 s context expired in the queue. After the attackers left: 40 ms.
Impact: any tenant owner/admin (the policy check passes before the body is read) can stall the whole API process for every tenant and make AI
results get lost; no data is exposed.

Fix direction: read and bound the body before `transactionAt` for every POST (as `localCommand`/`retryRelease` do), or set a short per-request read
deadline with `http.NewResponseController(w).SetReadDeadline` before taking the slot.

### IR-03 LOW - the recorded gate is not reproducible as written on a slower host; three timing-bound tests failed

Executed on this host (4 cores, `nice -n 10`, one process at a time):

| Step | Plan | Here |
|---|---|---|
| `make test-unit` | PASS, 211 s | first run **FAIL** in `internal/tools: TestTimeoutWhileServerSleepsIsUnknownAndBounded` (second phase got `invalid_response` status 200 instead of `timeout`), 326 s; the package passes on rerun and in 48 isolated and loaded repetitions (not reproduced) |
| integration suite | 1904 s, 2 environmental failures then PASS | **2716.8 s**, 2 FAIL: `TestM12APIDetailReadsOfALargeCandidateConverge` (503 `release_timeout`; isolated reruns: PASS once, FAIL once) and `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` (statement timeout 2 s, 3 of 3 runs FAIL) |

The second is not a merge regression: a timing probe (the unchanged test body with a 60 s statement timeout) measures the bounded scan at **2.24 s on the merged
head and 2.19 s on the pre-merge remediation tip `b00da36`** on this host; the 2 s budget has the narrow headroom the ledger itself records (1.15-1.66 s on the
builders' host). The gate is host-bound: a runner about 40% slower than the builders' fails two of 438 tests and the Makefile's 90-minute deadline leaves a factor of 2.
The plan's "green" is also a composite: its table combines a full run (436 PASS, 2 environmental FAIL), a re-run of those two after an environment fix and a
re-run of seven earlier failures after the test alignment; the pinned head had not been shown to pass as one end-to-end suite, and on this host it still does not.

### IR-04 LOW - documents of the merged tree contradict the tree

* `docs/migration/M9R_FINDING_STATUS.md` (declared the live ledger) still says, in the tree that contains M10 to M14: "no M10/main merge" (line 17),
  "M10 remains paused pending a separate user task" (line 207), "External exactly-once and AI steps | No external tool/AI implementation" (line 175),
  "Production active-release runtime ... Not implemented ... M12/M13" and "main remains M6" (line 183). The plan says "No row was re-graded by the merge";
  that is the problem, not a defence: a reader of the live ledger is told the opposite of the tree. `AGENTS.md`/`CLAUDE.md` are acknowledged stale by the plan.
* The plan's gate table says "5 script tests"; there are 9 (`node --test scripts/*.test.mjs`).
* `make check` runs `go vet` on `GO_PACKAGES`, which does not include `./tests/integration` (build tag); the builders' reports ran
  `go vet -tags=integration ./tests/integration` separately, `make check` does not.

### IR-05 LOW - nothing refuses a database that holds the milestone line's migration numbers

The plan says `MAIN` "has to be rebuilt, not migrated". Executed (`TestReviewMigrateOverMilestoneNumbering`): a database at 44 with `goose_db_version` rows for
the milestone numbers, then `migrate up`: with 45-53 the run stops at 54 with `column "aggregate_valid_v1" does not exist` and nothing is applied; with 45-53 and 60
goose refuses ("4 missing (out-of-order) migrations lower than database version (60): versions 54,55,56,57"). It is safe, but only because 54 happens to depend on
objects of the skipped remediation migrations and 60 happens to exceed 57; there is no explicit guard (name or checksum of the applied versions), so a milestone
database that stopped at 53 and a later remediation migration that did not depend on 47-52 would be skipped silently.

## Attacks that held (each executed)

* **Decision 1, one meaning per version token** - `TestReviewVersionTokenReadings` (unit, compiler): from a compiled 1.5.0 tool IR, nine re-shaped artifacts were
  restored: plan 2.2.0 without `tool_contract`, plan 2.1.0 with `tool_contract`, plan 2.0.0 with tool states, IR relabelled 1.4.0 / 1.3.0 with matching document
  labels, IR 1.5.0 with the plans dropped: all refused. The restore-only legacy shape (plan 2.1.0, tool states, no contract) is accepted inside a 1.5.0 IR, as documented;
  it can only come from an artifact the compiler did not produce. The validator, compiler restore, `workflow_plans`, `rules`, `integrity`, the renderer (`contracts.ts`),
  `internal/ai/schema_text.go` and `internal/promotion/dependencies.go` were read; the only place missed is the SQL literal of IR-01.
* **Decision 2, migration order and the shared CHECK** - the objects each set of migrations touches were extracted mechanically (113 objects for 00045-00057, 154 for
  00080-00089): the intersection is exactly the CHECK `metadata_versions_schema_version_check`, its table and `control.worker_grants_safe`, as the plan says. The CHECK of the
  merged database lists 1.0.0 to 1.5.0. A scan of the plpgsql bodies of the four guarded schemas for `schema.function(` and `schema.table` references found no dangling name.
* **Decision 3, derived worker guard** - evaluated as a real `framework_worker` session on the merged schema: `control.worker_grants_safe()` is true; the worker holds
  table-level `SELECT` only on 16 listed relations; no sequence privilege; no default ACL; no role is member of another except `framework_migrator` of `framework_schema_owner`;
  of 103 SECURITY DEFINER functions none has a loose `search_path`, none is owned by a role other than `framework_schema_owner`, no function of the four schemas is executable by PUBLIC,
  and the worker and dispatcher can execute only their listed functions (none of the new remediation index/unique functions). 71 of 72 tables have RLS forced (`iam.roles` is a
  global name catalog with no runtime grant).
* **Decision 4, R7b cutover** - `TestReviewCutoverRefusesLiveOldReaderBeforeMilestonePorts`: a database at 56 with a live old reader of `runtime.event_envelopes`; `migrate up`
  stops at 57 (`could not obtain lock on relation "runtime.event_envelopes"`, 55P03), the tool ledger and the M13 fences are not installed, the database stays at 56; after the
  reader ends the same call applies 57 and 80-89 and the worker guard is true. `RuntimeSnapshot` is certified only when `isolation == REPEATABLE READ` (`transaction.go`); the only
  READ COMMITTED callers are event jobs (`jobs.go`) and the freeze route (`access.go: freezePattern`).
* **Decision 5, front door** - `TestReviewRouteMatrix`: 45 POST routes of both lines, one distinct peer per anonymous call: every one answers 401 without a token; viewer and member get
  403 `forbidden` on every route that must deny them (datasets, evolutions, sandboxes, requirements, acceptance, candidates, approve/reject/activate/rollback, the seven AI routes,
  indexes, search indexes, unique constraints, roles, teams, metadata); the read-like routes (query, evaluate) and the commands/record-access routes behave as designed. The POST allowlist
  `AcceptsPOST` is the union of the remediation list and `sandboxPOST`, `releasePOST`, `aiPOST` (diff against `b00da36`). The write-deadline extension: a standalone server on Go 1.27.2
  (`WriteTimeout` 2 s) shows that a handler's `SetWriteDeadline(+30 s)` does **not** leak to the next request on the same keep-alive connection (the next request is cut at 2 s), and that `ReadTimeout`
  does not cancel a running handler's context (the read deadline is cleared when the background reader starts). With `WriteTimeout` 0 there is no deadline at all.
* **SSRF forms** - 58 address forms (IPv4-mapped, IPv4-compatible, NAT64 `64:ff9b::/96`, 6to4, Teredo, ISATAP, metadata 169.254.169.254 / 168.63.129.16 / fd00:ec2::254, CGNAT, documentation ranges)
  are all denied by `tools.DeniedAddress`, which the AI provider dials through; public controls are allowed.

## Not covered (so far)

pending
