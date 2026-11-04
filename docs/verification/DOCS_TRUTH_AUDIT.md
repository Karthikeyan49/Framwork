# Documentation truth audit

Independent audit of the written documentation of `integration/m9r-m14`. Pinned tree: `be6ec7c` (head of `origin/integration/m9r-m14`; the audit
branch is `audit/docs-truth`). The auditor did not write or fix any audited statement. Question asked of every statement: does the tree, the git
history or an executed command prove it? Classes: TRUE, FALSE, STALE (true when written, no longer), OVERCLAIM (stronger than its evidence),
UNVERIFIABLE (no durable evidence in the tree and not reproducible without a database or docker). Result words follow the project rule
PASS / FAIL / NOT RUN; an unexecuted check is NOT RUN, never PASS.

Later input (after the pin, read only with `git show`): the FINAL reports of the other reviewer on `origin/review/integration-final`
(`1d7867f` phase 1, `899bf68` phase 2). They are compared with the three scope documents in the section "Agreement with the final reviews";
the pin did not move and those files were not edited.

Status: first resume COMPLETE; second resume (22 October) phase 1 DONE, phase 2 (re-audit of the four files and this report against the coordinator's final commit) WAITING for its hash. `make check`: PASS, exit 0, 39.8 s, run once on the branch at 30287ca (84 + 43 checksums, gofmt, go vet of the packages and of ./tests/integration with the integration and performance tags, go mod verify, eslint, tsc); after the later one-line edit of the M13 report only node scripts/verify-docs.mjs was repeated (PASS, 84 + 43).

## Progress

| Scope | Item | State |
|---|---|---|
| 1 | `M9R_FINDING_STATUS.md` current checkpoint, all 43 live rows, appendix map | DONE |
| 2 | `LINE_INTEGRATION_PLAN.md` follow-up section, checkpoint, original record | DONE |
| 3 | `AGENT_HANDOFFS.md`, `M9R_REMEDIATION_PLAN.md`, proposal patch applicability, supersession map | DONE (historical R-slice progression: numbers traced, not re-derived line by line) |
| 4 | M10-M14 report banners, ADR100-104, interim review in the tree | DONE |
| 4+ | Final reviews on `origin/review/integration-final`: statements about the tree, agreement of ids, statuses, numbers | DONE (static); executed probes NOT RUN |
| 5 | `AGENTS.md` / `CLAUDE.md` stale list (report only) | DONE |
| 6 | Mechanical checks: links, paths, hashes, make targets, migration names, test names, version tokens, counts | DONE |
| 2nd resume 1 | Merge of trunk `43d417e`; corrections to other files; proposed text for AGENTS.md, CLAUDE.md, README.md | merge NOT DONE (refused by the permission layer); corrections DONE (`35d8068`, `24ab7e0`, `cef0677`, `eb16cc0`); proposed text DONE |
| 2nd resume 2 | Re-audit of the four files and this report against the final tree | WAITING for the coordinator's hash |
| end | `make check` once | PASS, exit 0, 39.8 s, run once on the branch at 30287ca (84 + 43 checksums, gofmt, go vet of the packages and of ./tests/integration with the integration and performance tags, go mod verify, eslint, tsc); after the later one-line edit of the M13 report only node scripts/verify-docs.mjs was repeated (PASS, 84 + 43) |

## Result in numbers

178 statements were classified (full ledger at the end of this file; classification as found, the STALE and FALSE statements fixed since are listed under Corrections applied).

| Area | Statements | TRUE | FALSE | STALE | OVERCLAIM | UNVERIFIABLE |
|---|---:|---:|---:|---:|---:|---:|
| M9R_FINDING_STATUS.md | 54 | 42 | 0 | 6 | 4 | 2 |
| LINE_INTEGRATION_PLAN.md | 44 | 30 | 2 | 3 | 4 | 5 |
| AGENT_HANDOFFS.md | 15 | 11 | 0 | 1 | 1 | 2 |
| M9R_REMEDIATION_PLAN.md | 8 | 5 | 0 | 3 | 0 | 0 |
| M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md | 4 | 3 | 0 | 1 | 0 | 0 |
| V2_V3_SUPERSESSION_MAP.md | 3 | 3 | 0 | 0 | 0 | 0 |
| M10-M14 reports and ADR100-104 | 9 | 4 | 0 | 4 | 1 | 0 |
| independent reviews (interim, final phase 1, M14) | 10 | 7 | 0 | 0 | 0 | 3 |
| AGENTS.md, CLAUDE.md, README.md | 8 | 2 | 1 | 5 | 0 | 0 |
| mechanical checks over docs/ | 9 | 9 | 0 | 0 | 0 | 0 |
| trunk commits up to 43d417e (second resume) | 14 | 12 | 1 | 0 | 0 | 1 |
| **Total** | **178** | **128** | **4** | **23** | **10** | **13** |

Defects: 26 (HIGH 0, MEDIUM 9, LOW 17); FIXED 5, PARTLY FIXED 2, REPORTED 19.

## Defects

Severity: HIGH = a security, isolation or gate result stated against the evidence; MEDIUM = false or misleading claim that could change a decision;
LOW = stale wording, typo, small count error. Action FIXED = corrected on this branch (see "Edits made"); REPORTED = left to the owner (a status call,
a rewording, a historical record, or a file this audit may not edit).

No HIGH defect was found. No stated PASS is contradicted by anything in the tree; the PASS results that need a database or docker are listed as
UNVERIFIABLE or NOT RUN, not as confirmed.

| Id | Sev | Document and line | Statement | Evidence | Action |
|---|---|---|---|---|---|
| DT-01 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:224`; `docs/migration/LINE_INTEGRATION_PLAN.md:84-85` | "integration/m9r-m14 remains `552fa9e`" / "Integration still points to `552fa9e`" | `git rev-parse origin/integration/m9r-m14` = `be6ec7c` (code head `f6398bf`); `552fa9e` is an ancestor, 17 commits behind (15 on the first-parent line). True when written on 20 October, STALE now; both sentences sit under or beside text that says the fixes are on the integration branch | REPORTED (status text, dated checkpoint) |
| DT-02 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:242-244` (section "Execution order and stop gate") | "Current combined gate is FAIL: ... as do B-01/B-02/B-05" | Contradicts the same file: lines 28-30 and rows B-01, B-02, B-05 (lines 225, 226, 229) say fixed on `integration/m9r-m14` (`dfec052`, `46d8a2a`). The follow-up edit changed 9 rows and one bullet and left this paragraph | REPORTED |
| DT-03 | MEDIUM | `docs/migration/M9R_REMEDIATION_PLAN.md:27-32, 43, 316, 466, 468, 509-512` | B-01, B-02, B-05 "confirmed open integration gaps" / "remain open"; B-04 "stale version wording" | Fixed in `dfec052` and `46d8a2a` (negative controls executed by this audit, see "Verified TRUE"). The plan is the remediation lane's document and was not touched by the milestone follow-up (`git diff e0fb83d HEAD --stat -- docs` lists only the ledger, the integration plan, the handoffs and the review) | FIXED (`35d8068`) |
| DT-04 | MEDIUM | `docs/migration/M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md:68-69, 103-104, 126` (lines of the embedded patch) | The "exact" patch for `AGENTS.md`/`CLAUDE.md` | Mechanically valid: baseline SHA-256 of both files match, `git apply --check` exits 0 on scratch copies, and applying to the scratch copies yields the blob ids named in the patch header (`90bb183`, `1ed343d`). But it would write now-false statements into the contract: "B-02 ... and B-05 ... remain open", "B-01 launcher settings remain dropped", "The applied maximum is 89" (a database built from this branch is at 90) | FIXED (`24ab7e0`, `aab5c16`) |
| DT-05 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:225`; `docs/migration/LINE_INTEGRATION_PLAN.md:16` | The real launcher is exercised "starting a fake `bin/api`/`bin/worker`" | `scripts/runtime-env.test.mjs:72-85` creates only `bin/api`; no worker child is launched, although the test title says "API and worker children". Worker propagation is pinned only by the pure allowlist test. Both new tests do fail against the pre-fix `runtime-env.mjs` (executed in a scratch copy: 9 pass, 2 fail) | REPORTED |
| DT-06 | MEDIUM | `AGENTS.md:122-123` | "Only `local.noop` is registered. ... external tools and production activation remain later milestones" | FALSE on this branch: `internal/platform/app/app.go:59-77, 156-160, 260-270` load the operator tool catalog and gateway; ADR100; `internal/runtime/workflow/tool_step.go`. (M13 activation is a preview pointer, so "production activation" stays true.) | REPORTED (file not editable) |
| DT-07 | MEDIUM | `AGENTS.md:3-6`; `CLAUDE.md:13` | "Do not begin M10 automatically" / "Do not continue M10 without the remediation gate and a new user task" | STALE for this branch: M10-M14 are merged preview code (ADR100-104, migrations 00080-00090). `AGENTS.md` also has no mention of ADR100-104 at all | REPORTED (file not editable) |
| DT-08 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:129` | Second parent `integ/prep-m14`: "189 commits and 253 files since the base" | `git rev-list --count e6305df..8366090` = 192 (189 is the count at `7f2b29b`, the analysis-time head used in the table at line 237); files 253 is right | FIXED (`4e2a028`) |
| DT-09 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:157-160` | The `metadata_versions` CHECK "was the only object that both lines' migrations changed" | Mechanical extraction of function/table/constraint/policy/index/trigger names from `00045-00057` and `00080-00089`: the intersection is the CHECK, `control.metadata_versions` and `control.worker_grants_safe` (remediation `00047/00051/00052`, milestone `00081/00083/00084/00087`). The next bullet of the same list says so | REPORTED |
| DT-10 | LOW | `docs/migration/M9R_FINDING_STATUS.md:220`; `docs/migration/LINE_INTEGRATION_PLAN.md:108` | "Merged migrations 1-57/80-89 apply fresh"; "A database already at 89" | `ls db/migrations`: 68 files, `00001-00057` and `00080-00090`; the plan's own follow-up says a database built from this branch is at 90. True of slice A's run, stale as live status | PARTLY FIXED (`35d8068`: remediation plan line; ledger and plan lines are in the coordinator rewrite) |
| DT-11 | LOW | `docs/migration/M9R_FINDING_STATUS.md:22-23`; `docs/migration/M9R_REMEDIATION_PLAN.md:33-36, 609-611`; `docs/migration/LINE_INTEGRATION_PLAN.md:103`; `docs/migration/M14_IMPLEMENTATION_REPORT.md:10-11` | "Independent M14/merge review is NOT RUN" | `docs/verification/INTEGRATION_INDEPENDENT_REVIEW.md` is in the tree as an interim independent review of the merge (IN PROGRESS, five findings). Since the pin, `origin/review/integration-final` carries the FINAL merge review (`1d7867f`, six findings) and the M14 review (`899bf68`, eight findings), both verdict FAIL. Not yet in the tree, so the sentences are stale only once those reports are merged; the interim one already makes them understate | PARTLY FIXED (`35d8068`, `eb16cc0`; ledger, integration plan and M14 report banner left) |
| DT-12 | LOW | `docs/migration/M9R_FINDING_STATUS.md:28-30` | "B-01, B-02, B-04, B-05 and IR-01 to IR-05 are fixed" | Row 230 and the plan say one of the three timing tests of IR-03 (`TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry`) is not changed (D1), and IR-04's ledger refresh is incomplete (DT-01, DT-02). The bullet is stronger than its rows | REPORTED |
| DT-13 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:14`; `docs/migration/AGENT_HANDOFFS.md:219-220` | The version-gate test means "the next superset cannot repeat it" / "will tell you which SQL bodies still name the older versions" | `internal/metadata/sql_version_gate_test.go:25-27` hard-codes `knownSchemaVersions()`; a future 1.6.0 is invisible until someone adds it there. It reads only quoted `'1.x.0'` literals in function bodies and one CHECK. The scan does fail without `00090` (executed with `-overlay`) | REPORTED |
| DT-14 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:18` | IR-05 evidence: "`tests/integration/m_migration_lineage_test.go`, executed against the real `migrate` command" | The test calls `postgres.CheckMigrationLineage` directly; nothing in the tree executes `cmd/migrate`, so the three-line wiring in `cmd/migrate/main.go:35-40` is unpinned. The commit message of `dfec052` records a manual run; no log is in the tree (UNVERIFIABLE) | REPORTED |
| DT-15 | LOW | `docs/migration/M9R_FINDING_STATUS.md:230` | "IR-04 LOW: `make check` vets the tagged suites" | `Makefile:65` vets with `-tags=integration,performance` only. `tests/integration` also has files tagged `trafficreview`, `eventoutage`, `driverpanic` and `race` that this vet does not compile. The plan and the handoff word it correctly ("with the integration and performance tags") | REPORTED |
| DT-16 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:9-10` | The recorded FAILs "(aggregate capacity, injected driver panic, worker-policy statement timeout) ... are its lane (D1)" | `M9R_REMEDIATION_PLAN.md:501,505`: D1 is worker-policy, driver panic and the connection failure; the aggregate capacity miss belongs to D5 (the ledger row 200 says D5) | REPORTED |
| DT-17 | LOW | `docs/migration/AGENT_HANDOFFS.md:32` | `cmd/migrate/main.go:38` creates a Goose provider without `WithAllowOutofOrder(true)` | True at `552fa9e`; after `dfec052` the call is at `cmd/migrate/main.go:45`. `WithAllowOutofOrder` appears nowhere in the tree | REPORTED |
| DT-18 | LOW | `docs/adr/104-m14-ai-builder-and-intent-capture.md:290-293` (A1) | Migration 00089 replaces `control.worker_grants_safe()` from the migration-84 definition with three tables added | `db/migrations/00089_m14_ai_proposals.sql:289` only mentions the guard in a comment; the last definition is `00087`. Amendment B.7 (lines 596-599) of the same ADR corrects it but A1 is not struck | REPORTED |
| DT-19 | LOW | `docs/migration/M10_M11_IMPLEMENTATION_REPORT.md:20` | Tool plans "(DSL 2.1.0)" | The banner (line 6) says "Tool plans keep workflow DSL 2.2.0" and line 51 says "DSL 2.2.0 tool plans"; 2.1.0 is the pre-review shape (ADR100 A1). Old and new token in one report without a mapping for this one | FIXED (`cef0677`) |
| DT-20 | LOW | `docs/migration/M9R_FINDING_STATUS.md:227`; `docs/migration/LINE_INTEGRATION_PLAN.md:71-72` | "Only the development database MAIN holds such data"; "No production database exists (the configuration rejects workflows and events in production)" | UNVERIFIABLE deployment facts. The plan hedges ("Known holders"), the ledger does not. `internal/config/config.go:77-89, 113, 125` and `app.go:270` (`PreviewEnabled` only in development/test) show that production cannot create tool IR through the preview routes; they do not show that no other database holds milestone-numbered data | REPORTED |
| DT-21 | LOW | `docs/migration/M9R_FINDING_STATUS.md:194, 195, 199` | Slice A's reference results (numeric 124.505 ms, writes 7,968 / 301.897 ms, unique 331.310/479.989/350.588 ms) stand in the "Live findings" table without the tree they were measured on | Measured at `552fa9e`; `serve` in `internal/platform/access/access.go` and `runtime.register_unique_constraint` changed afterwards and the reference gates were NOT RUN again (the plan says so at line 37; row 199 says it for unique only) | REPORTED |
| DT-22 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:22-23, 28-30, 230`; `docs/migration/LINE_INTEGRATION_PLAN.md:5-6, 103`; `docs/migration/AGENT_HANDOFFS.md:203-233` | The follow-up covers IR-01 to IR-05; row 230: "its final report and any new finding are pending"; the plan: M14/merge review NOT RUN | The final reports on `origin/review/integration-final` add IR-06 (MEDIUM, `rollback_release` writes no `release_fences` row) and M14R-01 to M14R-08 (M14R-04 = IR-06; M14R-01..03 MEDIUM, M14R-05..08 LOW), all open, none in the three documents. This audit confirmed IR-06 statically: `00088_m13_review_fixes.sql:51` (reject) and `:70` (activate) write the fence, `:99-128` (rollback) reads `release_rejections` at `:115` and writes none; the migration is unchanged since `552fa9e`. The race itself is NOT RUN here (needs a database) | REPORTED |
| DT-23 | LOW | `docs/adr/103-m13-approval-and-promotion.md:305` (amendment 12); `docs/migration/M13_IMPLEMENTATION_REPORT.md:84` | "M13R-01, a veto is not overtaken" / "FIXED: per-hash fence row written by both ports" | The text names two ports (true) but the heading and the FIXED mark read as covering every writer of the pointer; `rollback_release` is a third writer without the fence (IR-06 / M14R-04, DT-22) | REPORTED |
| DT-24 | MEDIUM | `README.md:14, 645, 652, 757` (outside the audit scope list, found by the stale-number scan) | "Follow the remediation plan before M10"; "M10 stays paused"; "no ... AI execution capabilities"; "Only `local.noop` is registered; external tools are M10" | Same family as DT-06/DT-07: M10-M14 are merged preview code (tool wiring `app.go:59-77`, AI routes behind `PreviewEnabled`, `app.go:270`). README is not under `docs/`, so not edited | REPORTED |
| DT-25 | LOW | `docs/migration/M13_IMPLEMENTATION_REPORT.md:99` | Re-run recipe: the reset "applies migrations 1-53" | Old milestone numbering in a report whose banner says the numbers below are the renumbered ones; the same set is 1-44 and 80-88 in the integrated tree (`git show -M 8366090`). The M12 report words the same range correctly ("1-44 and 80-83 (then numbered 1-48)") | FIXED (`789b87a`) |
| DT-26 | LOW | `db/migrations/numbering_test.go:10-14` (trunk `43d417e`, code comment) | "Proposed by the upgrade and ordering rehearsal (docs/verification/UPGRADE_AND_ORDERING_REHEARSAL.md)" | The path does not exist in the trunk tree; the report exists only on branch `audit/upgrade-rehearsal` (`git ls-tree -r --name-only 43d417e docs/verification` has no such file). The comment also calls the numbering rule "the owner's decision" while the ledger and plan still say the owner has not decided | REPORTED |

## Agreement with the final reviews

Compared: `M9R_FINDING_STATUS.md` (rows 225-231, bullet at lines 28-30), `LINE_INTEGRATION_PLAN.md` (follow-up section) and `AGENT_HANDOFFS.md`
(entry 4) against the final phase 1 report (IR-01..IR-06) and the M14 report (M14R-01..M14R-08). Both final reports are pinned at `552fa9e`, so
they do not review the fixes of `46d8a2a`..`f6398bf`; the documents' "not independently re-reviewed" therefore stays correct.

| Item | Final review | Scope documents | Agreement |
|---|---|---|---|
| IR-01 | MEDIUM; 1.5.0 unique registration answers 400, writes 409 | Plan "B-05 / IR-01 (MEDIUM)"; ledger "IR-01 = B-05 (fixed)"; handoff "fixed by 00090" | Agree (id, severity, symptom) |
| IR-02 | MEDIUM; honest GET 8.02 s, AI proposal 503 after 9.54 s | Plan "IR-02 (MEDIUM)", "waited 8 s"; ledger; handoff "fixed (f997efe)" | Agree |
| IR-03 | LOW; three timing-bound assertions on a slower host; a faster session passes the whole suite as one run (433 PASS, 5 SKIP, 0 FAIL, 1880.094 s) | Plan/ledger/handoff: two tests fixed, worker-policy test unchanged (D1); 2.24 s vs 2.19 s | Agree; the single-run pass and the 5 opt-in skips are not mentioned |
| IR-04 | LOW; ledger contradicts the tree, 5 vs 9 script tests, `make check` does not vet tagged suites | Plan IR-04 row; ledger row 230 | Agree on id, severity and fixes; ledger refresh incomplete (DT-01, DT-02, DT-15) |
| IR-05 | LOW; no guard against the old numbering | Plan/ledger/handoff: `CheckMigrationLineage` | Agree (evidence wording DT-14) |
| IR-06 = M14R-04 | MEDIUM; `rollback_release` has no fence row; open | Not in any of the three documents | DISAGREE: missing (DT-22); confirmed statically by this audit |
| M14R-01, 02, 03 | MEDIUM; provider label turns a paid answer into 503, cancelled calls uncharged, results lost under concurrency | Not mentioned; documents say the M14 review is NOT RUN | DISAGREE: missing (DT-22, DT-11) |
| M14R-05..08 | LOW; verifier band, usage cap, hanging adapter, redaction 10 of 28 | Not mentioned | DISAGREE: missing (DT-22) |
| Verdicts | Phase 1 FAIL (3 MEDIUM, 3 LOW, no HIGH); phase 2 FAIL (4 MEDIUM, 4 LOW, no HIGH) | Ledger: M9R PARTIAL, combined acceptance FAIL; review called interim | Same direction; no document records the final verdicts |
| Numbers | 439 listed / 438 run at the pin; 2716.8 s; 2.24 s vs 2.19 s; 103 definers; 60 allowlist rows | Same numbers where the documents quote them | Agree |

Statements of the final reports about the tree that this audit could check statically are TRUE: the 21 line references into `internal/ai`,
`access/ai.go`, `audit.go`, `httpserver.go`, `00089`, ADR102/104 and the constants (in-flight 2/8, daily 200,000, wall clock 60 s); the IR-02 line
references at `552fa9e`; the literal list of Decision 2; 103 definers with the 99/4 `search_path` split; 60 allowlist rows and 16 table-level
SELECT relations; the rollback fence absence. Their executed probes (races, timings, live catalog, 45 POST routes) are NOT RUN here.

## Verified TRUE (what is solid)

Re-derived by this audit, not copied:

- Number map: `git show -M 8366090` lists the ten migration and five ADR renames (45-53,60 to 80-89; 036-040 to 100-104). No unmapped old number is left in ADR100-104, the M10-M14 reports or the READMEs.
- Version tokens match the code: `internal/metadata/models.go:9-19`, `internal/compiler/compiler.go:18-32`, `internal/runtime/workflow/plan.go:22-30`, `internal/runtime/rules/types.go:10`. `contracts/jsonschema/application-v1.3/1.4.schema.json` are byte-identical to `b00da36`; only 1.5 names `tool_ref`; migrations 1-57 are unchanged against `b00da36`.
- Counts: 443 integration tests listed (`go test -list`, `-tags=integration`); 27 Go packages with tests in `GO_PACKAGES`; 11 script tests (11 pass); 126 frontend tests in 19 files (126 pass); 9 fuzz functions, 5 fuzz make targets; 11 + 7 Playwright cases (static count); 68 migration files; the 443 listed integration tests are the 439 of the pinned review plus exactly the four tests added by the follow-up (`git diff 552fa9e be6ec7c`).
- Merge record: 23 conflicting files and 37 hunks (`git merge-tree`), 33 non-document files changed by both lines, nine duplicated migration versions `00045-00053`, 116/264 and 189/253 commit/file counts, `integ/prep-m14` = `60da6f2` plus one commit.
- `00090` is `00052`'s function byte for byte except the predicate, with an owner/ACL/other-literal post-check; `00087` is the last definition of `control.worker_grants_safe`.
- Fixes with executed negative controls: B-02 (regression test fails with the pre-fix `restore.go` through `go test -overlay`); B-05 scan (`TestSQLFunctionsNameWholeSchemaVersionFamilies` fails with `00090` hidden); B-01 (both new script tests fail on the pre-fix `runtime-env.mjs`). IR-02 replay unit tests pass.
- Front door order in `access.go`: traffic gate, token pre-verification, local-command / idempotent-release / generic body pre-read for verified bearers only, admission slot, transaction.
- `config.go:77-89` rejects workflows and events in production; `:113` internal tool networks; `:125` the AI provider; `app.go:270` enables preview routes only in development/test.
- Slice A/B numbers quoted in the checkpoint and rows are the numbers of the A and B reports; the quoted historical R-slice numbers are present in the implementation reports.
- `AGENTS.md`/`CLAUDE.md` baselines in the proposal equal `sha256sum`; the patch applies (`git apply --check`, scratch copies) and reproduces the blob ids in its header.
- The 103 SECURITY DEFINER functions, the 4 `pg_catalog`-only R7b ones (`00055`, `00056`) and the re-pin loop at `00026:38-43`; 60 allowlist rows and 16 table-level SELECT relations in `00087`.
- `rollback_release` (`00088:99-128`) has no fence row while `reject_release` (`:51`) and `activate_release` (`:70`) do.

## Edits made

Two factual corrections, one commit per document. No frozen source document, code, test, migration, `AGENTS.md` or `CLAUDE.md` was touched.

| File and line | Before | After | Evidence | Commit |
|---|---|---|---|---|
| `docs/migration/LINE_INTEGRATION_PLAN.md:129` | "... renumbers the milestone line, 189 commits and 253 files since the base" | "... 192 commits and 253 files since the base" | `git rev-list --count e6305df..8366090` = 192 (189 is the count at `7f2b29b`); the file list of `git diff --name-only e6305df 8366090` has 253 entries | `4e2a028` |
| `docs/migration/M13_IMPLEMENTATION_REPORT.md:99` | "... applies migrations 1-53" | "... applies migrations 1-44 and 80-88 (then numbered 1-53)" | `git show -M 8366090`: 45-53 became 80-88; the M12 report uses the same wording pattern at line 72 | `789b87a` |

Everything else is REPORTED: the remaining defects are status calls (DT-01..03, 11, 12, 22), rewordings of someone's conclusion (DT-05, 09, 13-16, 19-21, 23), dated or
retained historical text (DT-10, 17, 18) or files this audit may not edit (DT-04, 06, 07, 24).

## Not covered

- Everything that needs PostgreSQL, Redis, docker or a browser: the integration suite, reference performance gates, fuzz and outage runs, browser suites, the live-catalog claims of the independent reviews (RLS on 71 of 72 tables, grants, ACLs, execute lists, 45 POST routes), and every executed probe of the final reviews (M14R-01..M14R-08, IR-06 race). The durations and PASS results of the gate on `f6398bf` (209 s, 24 s, 1956.5 s, 81 s, 610 s) have no durable log in the tree: UNVERIFIABLE, consistent between the plan and the handoff.
- The historical checkpoint progression (ledger lines 36-178), the 2026-10-17/19 evidence-audit tables (`file:line` citations against trees `dbc4b5b` and `e6b7877`) and the R-slice progression in the remediation plan: numbers traced to the implementation reports, not re-derived line by line.
- `review/m14-and-fixes` (the second reviewer's in-progress report on the integration fixes) and anything on branches other than the pin and `origin/review/integration-final`.
- Whether any database other than `MAIN` holds milestone-numbered data (DT-20); whether a pull request exists on the remote.

- Second resume: the trunk merge was not made (refused); the 11 new integration tests, the live-catalog scan of `43d417e`, the uncommitted F-06 rework in `/home/user/integ` and the announced `00092` are NOT RUN / not read. The four files being rewritten were not re-audited yet (phase 2).

## Commands run

Environment: `export PATH=/tmp/node24/node-v24.22.0-linux-x64/bin:$PATH GOPATH=/home/user/m14-int/.local/gopath GOCACHE=/home/user/m14-int/.local/cache/go`; Go through `./scripts/go.sh`, everything that compiles under `nice -n 15`.

```
git log -1 / -3, git status --short, git worktree list, git branch -a, git rev-parse origin/{integration/m9r-m14,remediation/m9r-foundation,main,claude/m14-ai-builder,review/integration-final}
git show --stat <46d8a2a dfec052 f997efe 6c4cee6 1fb0110 bbf2465 f6398bf>; git show / git diff of their files
git cat-file -e <hash>^{commit}   (every 7-40 hex token of the audited docs, loop)
git show --no-patch e219b31; git merge-base b00da36 8366090; git rev-list --count e6305df..{b00da36,7f2b29b,8366090,60da6f2}; git diff --name-only e6305df <rev>
git merge-tree $(git merge-base b00da36 8366090) b00da36 8366090   (conflict files and hunks counted by script; legacy mode, no objects written)
git show --stat -M 8366090; git show --name-status -M 8366090
git diff --stat {552fa9e,6a1da17,f6398bf,e0fb83d,b00da36} {be6ec7c,f6398bf,HEAD} [-- paths]; git rev-list --count 552fa9e..be6ec7c
git merge-base --is-ancestor <a1d8b00|552fa9e|e0fb83d|46d8a2a> <origin/remediation/m9r-foundation|be6ec7c>; git diff 552fa9e be6ec7c -- tests/integration | grep '^+func Test'
git show 552fa9e:<path> | sed -n ...   (ledger lines, access.go, ai.go, datasets.go, promote.go, preview.go, metadata.go)
git ls-tree --name-only {b00da36,7f2b29b} db/migrations/; git hash-object AGENTS.md CLAUDE.md; sha256sum AGENTS.md CLAUDE.md
git show origin/review/integration-final:docs/verification/{INTEGRATION_INDEPENDENT_REVIEW,M14_INDEPENDENT_REVIEW}.md > scratch
awk (extract the embedded diff) ; git apply --check [-v|--stat] proposal.patch   (scratch copies of AGENTS.md and CLAUDE.md only); git apply on the scratch copies + git hash-object
node --test scripts/*.test.mjs
node --test runtime-env.test.mjs   (scratch copy with scripts/runtime-env.mjs from dfec052^)
cd apps/web && nice -n 15 npx vitest run --maxWorkers=1 --reporter=dot
./scripts/go.sh list ./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...   (with and without the -f test filter)
./scripts/go.sh test -tags=integration -list '.*' ./tests/integration
./scripts/go.sh test -count=1 -run 'TestSQLFunctionsNameWholeSchemaVersionFamilies|TestFamilyGapsSeesTheOriginalDefect|TestMetadataVersionsCheckListsEveryKnownVersion' ./internal/metadata   (plain, and with -overlay hiding 00090)
./scripts/go.sh test -count=1 -run TestIRRestoreKeepsTheExactCompilerIdentityOfEveryIntegrityCapableContract ./internal/compiler   (plain, and with -overlay restoring the pre-fix restore.go)
./scripts/go.sh test -count=1 -run TestReplayedBody -v ./internal/platform/access
date -u; uptime; ls /home/user/briefs/54-rev-integration.md (host-local path named in the plan: exists, not in the repository)
grep / sed / ls / wc / diff over the tree (versions, line references, make targets, build tags, migrations, ADR numbers)
python3 -I helper scripts in the session scratchpad: relative links, repository paths, test names, decimal metrics against other docs, backticked make targets,
  intersection of objects of 00045-00057 and 00080-00089, SECURITY DEFINER inventory (type-normalised), allowlist row count, ledger rows changed by slice C, statement ledger and counts
git -C /home/user/integ log --oneline -15; git -C /home/user/integ status --short   (read only)
git merge --no-ff 43d417e   (REFUSED by the permission layer; not retried)
git merge-tree $(git merge-base HEAD 43d417e) HEAD 43d417e   (in memory, nothing written: 0 conflict markers)
git diff --stat be6ec7c 43d417e; git show --stat <dd4daba 9d2928d adc1898 c76968f b05494a bfa47c4 a2c5c52 8528fde 2ca08d8 953be65 43d417e>
git show 07b51b1:docs/verification/M14_AND_FIXES_INDEPENDENT_REVIEW.md > scratch   (other reviewer's round 2, read only)
git archive 43d417e | tar -x -C <scratchpad>/trunk43   (read-only extraction outside the repository)
(in the extraction, nice -n 10) node --test scripts/*.test.mjs; ./scripts/go.sh test -count=1 ./db/migrations ./cmd/migrate ./internal/metadata ./internal/platform/postgres ./internal/platform/access;
  ./scripts/go.sh test -count=1 -run 'TestChargeUsage|TestProviderLabels|TestACanceledCall|TestAnAdapterThatIgnores|TestAbandonedProviderCalls' ./internal/ai;
  ./scripts/go.sh test -tags=integration -list '.*' ./tests/integration; ./scripts/go.sh list ... ; ls db/migrations/*.sql
python3 -I: diff of the rollback_release bodies of 00088 and 00091
git apply --check / git apply on scratch copies of AGENTS.md and CLAUDE.md after editing the proposal; git hash-object; git diff --check; node scripts/verify-docs.mjs; links.py
make check   (once, at the end): PASS, exit 0, 39.8 s, run once on the branch at 30287ca (84 + 43 checksums, gofmt, go vet of the packages and of ./tests/integration with the integration and performance tags, go mod verify, eslint, tsc); after the later one-line edit of the M13 report only node scripts/verify-docs.mjs was repeated (PASS, 84 + 43)
```

## Second resume (22 October 2026)

New instruction: merge the newest trunk commit into this branch, apply the REPORTED corrections that touch files outside the four files the
coordinator is rewriting (`LINE_INTEGRATION_PLAN.md`, `M9R_FINDING_STATUS.md`, `AGENT_HANDOFFS.md`, `docs/adr/104-...`), list the proposed text
for `AGENTS.md`, `CLAUDE.md` and `README.md`, then wait for the hash of the coordinator's documentation commit and re-audit.

### Trunk merge: NOT DONE

Asked: merge `43d417e` (head of `integration/m9r-m14` in `/home/user/integ`, not pushed; 11 commits after the pin `be6ec7c`) into `audit/docs-truth`.
Result: `git merge --no-ff 43d417e` was refused by the permission layer of this session (reason given: "Modify Shared Resources"). I did not retry it and did
not reproduce it with plumbing or a cherry-pick. The trunk was read instead: `git show` / `git diff` by hash and a read-only `git archive 43d417e`
extraction into the session scratchpad (outside the repository), in which the DB-free tests were run.
Consequences: this branch still holds the pin plus audit commits; every correction below touches a file that the trunk has not changed
(`git diff --stat be6ec7c 43d417e` lists, among documents, only `AGENT_HANDOFFS.md`, `LINE_INTEGRATION_PLAN.md`, `M9R_FINDING_STATUS.md`), so the branch
merges into the trunk without conflicts. Evidence: a three-way merge of `43d417e` into this branch computed in memory with the legacy `git merge-tree <base> <ours> <theirs>`
(nothing written) has 0 conflict markers; the only file changed on both sides is `LINE_INTEGRATION_PLAN.md` (my one-line edit at line 129 against the trunk's inserted table row), which merges cleanly.
To proceed the owner can run `git -C /home/user/audit-docs merge --no-ff 43d417e`, ask me to, or merge `audit/docs-truth` into the trunk.

### Corrections applied to other files

| File | Change | Evidence | Commit |
|---|---|---|---|
| `docs/migration/M9R_REMEDIATION_PLAN.md` | B-01, B-02, B-04, B-05 are fixed on the integration branch (not "confirmed open"), with the hardening commits; review status says what exists; "already at 89" becomes the highest applied number; worker-policy scan and D1 notes follow `9d2928d`; stop-gate sentence lists what is still a recorded FAIL | DT-03, DT-10, DT-11; `dfec052`, `46d8a2a`, `953be65`, `43d417e`, `9d2928d` | `35d8068` |
| `docs/migration/M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md` | Four passages of the patch rewritten (ADR044 bullet, ADR100-104 bullet, number reservations, worker-policy sentence) with the same line counts; header blob id of `AGENTS.md` updated; a note records the correction | `git apply --check` of the corrected patch on scratch copies of the unchanged `AGENTS.md`/`CLAUDE.md`: exit 0; resulting blob ids `6866689`, `1ed343d` | `24ab7e0`, `aab5c16` |
| `docs/migration/M10_M11_IMPLEMENTATION_REPORT.md:20` | "(DSL 2.1.0)" for tool plans now says it is the shape as first built and restore-only since ADR100 A1 | DT-19; ADR100 A1; `plan.go:30` | `cef0677` |
| `docs/migration/V2_V3_SUPERSESSION_MAP.md:42` | "independent review NOT RUN" for M14 replaced by the existence and verdict of the review on `review/integration-final` | `M14_INDEPENDENT_REVIEW.md` on that branch: FAIL, 4 MEDIUM, 4 LOW | `eb16cc0` |

Not edited, by instruction: `AGENTS.md`, `CLAUDE.md`, `README.md` (owner decisions; text below) and the four files being rewritten. Left alone on purpose: dated
reports (A, B, C, M14 report banner "independent review has not been run" of 19 October), the other reviewer's reports, `ADR103` (needs an amendment, see below).

### Counts at trunk commit `43d417e` (re-derived from the read-only extraction)

| Quantity | At the pin `be6ec7c` | At `43d417e` | How |
|---|---:|---:|---|
| Integration tests listed (`-tags=integration`) | 443 | **454** | `go test -list`; +3 `TestM13R01Rollback...`, +1 `TestM14PreReadBufferingIsBounded`, +6 `TestM14R0*`, +1 `TestM9RNoCatalogObjectGatesOnPartOfTheAuthoringFamily` |
| Go packages in `GO_PACKAGES` / with tests | 40 / 27 | 40 / **29** | `go list`; `db/migrations` and `cmd/migrate` gained tests (`b05494a`) |
| Script tests (`node --test scripts/*.test.mjs`) | 11 | **13** | 13 pass; `953be65` adds two |
| Frontend tests / files | 126 / 19 | 126 / 19 | `apps/web` unchanged (`git diff --stat`); files counted, tests not re-run |
| Migration files | 68 (`00001-00057`, `00080-00090`) | **69** (`00080-00091`) | `ls db/migrations/*.sql`; `00092` is announced, not in the trunk |
| Fuzz functions | 9 | 9 | grep |

DB-free tests that I ran on the extraction and that PASS: `./db/migrations`, `./cmd/migrate`, `./internal/metadata`, `./internal/platform/postgres`,
`./internal/platform/access`, the five new `internal/ai` gateway tests (`-run`), all 13 script tests. Everything that needs a database (the 11 new
integration tests, the live-catalog scan of `43d417e`, the figures in the commit messages) is NOT RUN.

### Review findings against the code at `43d417e`

| Id (severity) | Code status at `43d417e` | Evidence |
|---|---|---|
| IR-01 / B-05 (MEDIUM) | fixed `46d8a2a` (`00090`); scan widened `43d417e` | `00090` differs from `00052` only in the predicate |
| IR-02 (MEDIUM) | slot fix `f997efe`; memory bound `c76968f` (4 MiB per verified identity, 32 MiB in all: `bodybudget.go:23-26`) | F-06 (MEDIUM) open: the budget books the declared length; the rework is uncommitted in `/home/user/integ` (`access.go`, `bodybudget.go` modified) and is not in `43d417e` |
| IR-03 (LOW) | tests `6c4cee6`, `1fb0110`, `dd4daba`; statement `9d2928d` (`execution.go:273-281`, uncorrelated `NOT IN`) | per-row worker-policy cost untouched (commit message) |
| IR-04 (LOW) | counts and vet tags fixed (`6a1da17`, `6c4cee6`) | ledger refresh is the coordinator's rewrite |
| IR-05 (LOW) | guard `dfec052`; messages and numbering rule test `b05494a` | no test runs `cmd/migrate` main wiring (`main.go:37`) |
| IR-06 = M14R-04 (MEDIUM) | fixed `bfa47c4`, `00091` | body equals the `00088` body plus the four fence lines; post-check requires the fence statement (`00091:60`) |
| M14R-01 (MEDIUM), -02 (MEDIUM), -06 (LOW), -07 (LOW) | fixed `a2c5c52`; regression tests `2ca08d8` | `repair.go`, `maxAbandonedCalls = 16`; F-07 and F-08 (LOW) are residuals of this fix per round 2 |
| M14R-03 (MEDIUM) | concurrent results fixed `8528fde` (`aiResultAttempts = 6`, `aiResultBudget = 6 s`) | residuals named in the commit: token expiring during the provider phase, creations racing for the usage row, single usage row |
| M14R-05 (LOW), M14R-08 (LOW) | open | no commit touches `internal/verification`, `internal/intent` or the redaction table |
| F-01 (MEDIUM), F-02 (LOW) | fixed `c76968f`, which opens F-06 | round 2 re-verification |
| F-03 (LOW), F-04 (LOW) | fixed `43d417e`, `953be65` | scan reads more spellings, "a guard, not a proof" (commit message); launcher detection `runtimeKindOf` |
| F-05 (LOW) | open: `approve_release` is defined only in `00084`; fix announced as `00092` | `grep -l 'FUNCTION control.approve_release' db/migrations/*.sql` |
| F-07, F-08 (LOW) | open (fix announced) | round 2 report |

### Status of my defects against `43d417e`

| Defect | Status |
|---|---|
| DT-01, DT-02 | unchanged in the trunk commit; in the four files, being rewritten |
| DT-03, DT-04, DT-19 | FIXED (above) |
| DT-05 | launcher test now covers api, worker and dispatcher through the real launcher (`953be65`, two new script tests); the documents must cite that test, not the `bin/api`-only one |
| DT-06, DT-07, DT-24 | open; text proposed below |
| DT-09 | unchanged (plan, being rewritten) |
| DT-10, DT-11 | remediation plan and supersession map fixed; ledger, plan and M14 report banner remain |
| DT-12 | IR-03's third test is now addressed by `9d2928d`; IR-04 ledger refresh depends on the rewrite |
| DT-13 | scan widened by `43d417e` but "not a proof" (character classes unread); wording "cannot repeat it" still stronger than the evidence |
| DT-14 | still true: the new `cmd/migrate` tests cover `upFailure` and the numbering, not the lineage wiring |
| DT-15, DT-16 | unchanged (Makefile and plan text) |
| DT-17 | `goose.NewProvider` is now at `cmd/migrate/main.go:47` (38 at `552fa9e`, 45 at `be6ec7c`) |
| DT-18 | ADR104 is among the files being rewritten |
| DT-20, DT-21 | unchanged; DT-21 grows: `c76968f`, `a2c5c52`, `8528fde` changed the request path after A's measurements |
| DT-22 | IR-06 and M14R-01/-02/-03/-06/-07 fixed in code; open items listed in the table above |
| DT-23 | after `00091` rollback writes the fence; ADR103 amendment 12 still says only reject and activate do, and calls an approval beside a rejection "harmless" (F-05 shows it is recorded) |
| DT-26 (new) | below |

### Proposed text for the files that stay untouched

`AGENTS.md`, `CLAUDE.md`: the corrected patch in `M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md` (applies with `git apply`) is the proposed text; the lines it replaces are:

| File:line | Current text | Proposed text |
|---|---|---|
| `AGENTS.md:3-6` | "Current authorized work is M9R foundation remediation after the published M0-M9 review. ... Do not begin M10 automatically." | "Current authorized work is M9R foundation remediation on the merged preview. M10-M14 preview code exists; remediation remains this agent's lane. Follow the current plan, live ledger and handoffs, with the original validated review as baseline. M9R remains PARTIAL and combined acceptance FAIL. Preserve working code/data; R6 and new M10+ feature work belong to the milestone agent, not this lane." |
| `AGENTS.md:76-78` | "ADR044 adds opt-in schema/IR/compiler1.4 unique fields. A declaration is not enforcement: ..." | "ADR044 adds opt-in schema/IR/compiler1.4 unique fields; schema 1.5 is its tool superset: Restore pins the exact 1.5 pairing and every SQL gate names 1.4 and 1.5 alike. A declaration is not enforcement: ..." |
| `AGENTS.md:115` | "Event jobs require RC with no shared-snapshot certificate; API/workflow stay RR." | "Event jobs require RC with no shared-snapshot certificate; API/workflow default to RR. Only the exact sandbox-freeze route uses RC through the common withRuntime port. RuntimeSnapshot certifies only its own RR executor." |
| `AGENTS.md:122-123` | "Only `local.noop` is registered. M9R corrects the historic worker boundary; external tools and production activation remain later milestones." | "local.noop and governed operator-catalog tools exist in preview; R7 still owns general revise/resubmit, renewal and approval semantics. M9R corrects the historic worker boundary; M13 activation is a preview control-plane pointer, without a production data plane." |
| `AGENTS.md` (after the ADR023 bullet) | no ADR100-104 contract | the "ADR100-104" bullet of the patch: M10 operator catalog and per-tool idempotency; M11 synthetic sandboxes/frozen evidence; M12 server-derived candidates/risk and executable acceptance; M13 SQL approval/activation authority; M14 typed AI proposals with human apply/confirm; never duplicate R6; the verifier runs in the API trust domain; the launcher gives AI/tool settings only to API and worker |
| `CLAUDE.md:6-7` and `13` | "Current work is M9R foundation remediation, following the validated review." / "Do not continue M10 without the remediation gate and a new user task." | "Current work is M9R foundation remediation on the merged M10-M14 development/test preview, following the validated review, live ledger and handoffs." / "M9R remains PARTIAL and combined acceptance FAIL. This agent builds remediation only; the milestone agent owns R6 and new M10+ work, including the production data plane and separate verifier credential. Do not duplicate them. ..." |

`README.md` (outside `docs/`):

| Line | Current text | Proposed text |
|---|---|---|
| 14 | "Follow the remediation plan before M10." | "M10-M14 exist as development/test preview (see the finding ledger); follow the remediation plan for the remaining foundation work." |
| 645 | "Workflow foundations now exist. Production promotion, external tools, AI and full scale/security certification remain later work. M10 stays paused." | "Workflow foundations exist. M10-M14 (tools, sandbox, candidate builder, approval/promotion control plane, AI proposals) are development/test preview; the production data plane, production activation, a real model provider and full scale/security certification remain later work." |
| 652 | "There are no public production activation endpoints or AI execution capabilities." | "Release approval/activation routes (M13) and AI proposal routes (M14) exist only in development/test (`PreviewEnabled`; `AI_PROVIDER_FILE` is refused in production); there is no production activation endpoint and no production AI execution." |
| 757 | "`local.noop` is registered; external tools are M10." | "`local.noop` and, when `TOOL_CATALOG_FILE` is set, operator-catalog tools (ADR100) are registered." |

Proposed ADR103 amendment (owner's file, not edited): "15. After the second review round, `rollback_release` also writes the per-hash fence row (migration 00091), so reject, activate and rollback
are fenced against each other; `approve_release` does not and may record an approval beside a committed rejection or on a superseded candidate (F-05); the approval cannot move the pointer."
Adjust the last clause when `00092` lands.

## Statement ledger

| Id | Where | Statement | Class | Evidence |
|---|---|---|---|---|
| FS-01 | FINDING_STATUS:5 | Overall M9R PARTIAL, combined acceptance FAIL | TRUE | Unresolved FAILs in the A and B reports (aggregate 822.729 ms, driver panic, worker-policy 2000 ms) are not closed by the follow-up |
| FS-02 | FINDING_STATUS:6 | Product tree 552fa9e combines remediation with M10-M14 | TRUE | git show e219b31 (parents b00da36, 8366090); 552fa9e = merge + test alignment + docs only |
| FS-03 | FINDING_STATUS:6-7 | a1d8b00 is pushed on remediation/m9r-foundation | TRUE | ancestor of origin/remediation/m9r-foundation (e0fb83d) |
| FS-04 | FINDING_STATUS:7 | main stays 9da3f5f | TRUE | git rev-parse origin/main |
| FS-05 | FINDING_STATUS:10-12 | A: 439 PASS, browsers 11+7, nine fuzz targets, 600 s outage | TRUE | A report tables; 9 func Fuzz re-counted in the tree |
| FS-06 | FINDING_STATUS:12-13 | 7 of 8 reference targets PASS, aggregate 822.729 vs 750 ms FAIL, driver panic FAIL | TRUE | A report (aggregate and driver tables) |
| FS-07 | FINDING_STATUS:14-16 | B: 23 files, 438 PASS / 1 FAIL / 0 skips, 2061.520 s | TRUE | B report; 23 files and 37 hunks re-derived with git merge-tree |
| FS-08 | FINDING_STATUS:16-17 | Worker-policy initial SELECT exceeds 2000 ms | TRUE | B report; m9r_worker_policy_test.go:158 is the t.Fatal of the bounded scan |
| FS-09 | FINDING_STATUS:17-18 | B-01/B-02/B-05 confirmed OPEN | STALE | Fixed in dfec052 and 46d8a2a; only qualified by the next bullet (L28-30) |
| FS-10 | FINDING_STATUS:19-21 | R6 is implemented preview coverage in M11-M14, held for the milestone agent | TRUE | Plan R6 table; ADR103 section 10 |
| FS-11 | FINDING_STATUS:22-23 | Independent M14/merge review is NOT RUN | STALE | Interim review is in the tree; final merge and M14 reports exist at origin/review/integration-final (1d7867f, 899bf68) |
| FS-12 | FINDING_STATUS:24-27 | R7 D1-D7 proposed, not started | TRUE | Plan lines 493-512 |
| FS-13 | FINDING_STATUS:28-30 | B-01, B-02, B-04, B-05 and IR-01 to IR-05 are fixed | OVERCLAIM | IR-03's worker-policy test is unchanged (row 230) and IR-04's ledger refresh is incomplete (L224, L242-244) |
| FS-14 | FINDING_STATUS:31-34 | Agent-rule changes are proposed only | TRUE | AGENTS.md and CLAUDE.md unchanged since b00da36; hashes equal the proposal baseline |
| FS-15 | FINDING_STATUS:189 | R1 PASS within the database credential model | TRUE | M9R_R1_INDEPENDENT_REVIEW verdict PASS |
| FS-16 | FINDING_STATUS:190 | Derived worker allowlist of 00087 is the latest; A evaluated it as framework_worker | TRUE | 00087:173,244 are the last definitions; A report |
| FS-17 | FINDING_STATUS:192 | Procedure for old pinned releases is in the integration plan | TRUE | Plan section Data written by the milestone line before the merge (B-03) |
| FS-18 | FINDING_STATUS:194 | A reference numbers 124.505 / 385.771 / 271.953 / 244.004 / 184.812 ms | TRUE | A report table |
| FS-19 | FINDING_STATUS:194,195,199 | A's PASS results stand in the live table without the tree they were measured on | OVERCLAIM | Measured at 552fa9e; access.go serve and register_unique_constraint changed later; gates not re-run (DT-21) |
| FS-20 | FINDING_STATUS:195 | R4 verified at 94932e3; 7,968 writes, p95 301.897 ms | TRUE | R4 review; A report |
| FS-21 | FINDING_STATUS:199 | Unique gates 331.310 / 479.989 / 350.588 ms | TRUE | A report |
| FS-22 | FINDING_STATUS:199 | Reference-scale unique gates ran under 1.4; the 1.5 lifecycle fixture is small | TRUE | m9r_unique_performance_test.go:23 uses uniqueFixture (1.4.0); lifecycle test uses 60 rows |
| FS-23 | FINDING_STATUS:200 | Aggregate 755.156 / 822.729 / 755.607 ms vs 750; SQL worst 309.775 vs 500 | TRUE | A report |
| FS-24 | FINDING_STATUS:201 | M13 control plane is preview, no production data plane | TRUE | ADR103 section 10; config.go:77-89; app.go:270 |
| FS-25 | FINDING_STATUS:203-207 | R5a-R5g numbers and commits (629.247 s, 829.428 s, 561.291 s, 605.921 s, 112.045/243.551 ms, 5c05d0b, ba21eb0, 0-120 s skew) | TRUE | Traced to the implementation reports; identity.go:62 bounds skew to 0..2 min; commits exist |
| FS-26 | FINDING_STATUS:209 | Worker-policy case: 96 receipts, 63 scope nodes, 5.34 s | TRUE | B report |
| FS-27 | FINDING_STATUS:210 | Driver panic diagnostic FAIL 6.857 s | TRUE | A report |
| FS-28 | FINDING_STATUS:211 | R7b budgets 5 s / 15 m, 20 attempts, 600 s outage | TRUE | ADR062 lines 25-39; A report |
| FS-29 | FINDING_STATUS:214 | 00085 derives the approval need from the pinned plan and checks the record version in tool_begin | TRUE | 00085:42-78 (tool_approval_gated, TL002) |
| FS-30 | FINDING_STATUS:216 | B-01 launcher loss is fixed on the integration branch | TRUE | dfec052; both new script tests fail on the pre-fix runtime-env.mjs (executed) |
| FS-31 | FINDING_STATUS:219 | AGENTS.md and CLAUDE.md still contain stale preview-absence wording | TRUE | DT-06, DT-07 |
| FS-32 | FINDING_STATUS:220 | Merged migrations 1-57/80-89 apply fresh | STALE | 68 migration files, 00001-00057 and 00080-00090 |
| FS-33 | FINDING_STATUS:220 | Milestone agent proposes one sequence from 00091 and releasing 58-79 | TRUE | Plan lines 48-63 |
| FS-34 | FINDING_STATUS:224 | integration/m9r-m14 remains 552fa9e | STALE | origin/integration/m9r-m14 is be6ec7c (DT-01) |
| FS-35 | FINDING_STATUS:225 | B-01: exact allowlists, web and dispatcher get none, 11 script tests | TRUE | scripts/runtime-env.mjs; node --test: 11 pass |
| FS-36 | FINDING_STATUS:225 | The real launcher starts a fake bin/api and bin/worker | OVERCLAIM | runtime-env.test.mjs:72-85 creates only bin/api (DT-05) |
| FS-37 | FINDING_STATUS:226 | B-02: Restore pins the application/IR/compiler pair for 1.5.0; five forgeries of both contracts and a control | TRUE | restore.go:48-52; test passes, fails on pre-fix restore.go (go test -overlay, executed) |
| FS-38 | FINDING_STATUS:227 | B-03 documented, not fixed; inventory, classification and cutover rule are in the plan | TRUE | Plan lines 65-79; named tables and columns exist in the migrations |
| FS-39 | FINDING_STATUS:227 | Only the development database MAIN holds such data | UNVERIFIABLE | Deployment fact; the plan hedges (Known holders) (DT-20) |
| FS-40 | FINDING_STATUS:228 | B-04 fixed, wording only | TRUE | dfec052 diff: models.go comment and two test messages |
| FS-41 | FINDING_STATUS:229 | B-05: 00090 replaces the function from its latest definition with IN (1.4.0,1.5.0), same owner/ACL/guards, post-check | TRUE | Function bodies of 00052 and 00090 differ only in the predicate; post-check read |
| FS-42 | FINDING_STATUS:229 | sql_version_gate_test scans every function and the CHECK and fails without 00090 | TRUE | go test passes; fails with 00090 hidden through -overlay (executed) |
| FS-43 | FINDING_STATUS:229 | TestM9RUniqueLifecycle... takes 1.4.0 and 1.5.0 through 409, registration, READY, create, duplicate | TRUE | Test source m9r_unique_tool_contract_test.go; its PASS needs a database (NOT RUN here) |
| FS-44 | FINDING_STATUS:230 | IR-02: serve reads a verified-bearer POST body before the slot and replays it with the 1 MiB cap error | TRUE | access.go preRead branch; TestReplayedBody* pass (executed) |
| FS-45 | FINDING_STATUS:230 | IR-02 real-server test fails without the change (8.55 s) and passes with it (201 in 1.6 s) | UNVERIFIABLE | Needs a database; test source and bounds (5 s, 7 s) read |
| FS-46 | FINDING_STATUS:230 | IR-03: set-up retries typed 503/429; tools test aborts a stalled handler | TRUE | Diffs of 6c4cee6 and 1fb0110 |
| FS-47 | FINDING_STATUS:230 | IR-04: make check vets the tagged suites | OVERCLAIM | Only -tags=integration,performance (Makefile:65); trafficreview, eventoutage, driverpanic, race files are not vetted (DT-15) |
| FS-48 | FINDING_STATUS:230 | IR-05: migrate refuses a database with the old numbers (dfec052) | TRUE | lineage.go; cmd/migrate/main.go:35-40 |
| FS-49 | FINDING_STATUS:230 | The review is interim; its final report and any new finding are pending | STALE | Final reports exist at origin/review/integration-final; IR-06 and M14R-01..08 are new findings (DT-22) |
| FS-50 | FINDING_STATUS:231 | Combination test: DSL 2.2.0 plan with tenant role and tool contract, 403 for three non-holders, one send, revoked holder blocked | TRUE | Test source m_tenant_approver_tool_test.go (PASS 18.6 s needs a database: NOT RUN) |
| FS-51 | FINDING_STATUS:242-244 | Current combined gate is FAIL ... as do B-01/B-02/B-05 | STALE | Contradicts rows 225, 226, 229 and L28-30 (DT-02) |
| FS-52 | FINDING_STATUS:259-265 | Appendix map: migrations 45-53,60 to 80-89; ADR036-040 to ADR100-104; 1.3.0 to 1.5.0; DSL 2.1.0 to 2.2.0; present in the integrated branch | TRUE | git show -M 8366090; version constants in code |
| FS-53 | FINDING_STATUS:253-257,267-268 | Appendix was written on the milestone line on 17 and 19 Oct and is read through the map | TRUE | Commit dates; map rows verified |
| FS-54 | FINDING_STATUS:356-357 | 1e42f0a corrected two ranges and five shorthand citations | TRUE | Commit subject of 1e42f0a |
| LP-01 | LINE_PLAN:5-10 | Definition of fixed; no independent re-review | TRUE | Consistent with the commits and tests |
| LP-02 | LINE_PLAN:14 | B-05 row (00090, post-check, scan test, lifecycle test) | TRUE | Same evidence as FS-41 to FS-43 |
| LP-03 | LINE_PLAN:14 | so the next superset cannot repeat it | OVERCLAIM | knownSchemaVersions is hard-coded; literals and one CHECK only (DT-13) |
| LP-04 | LINE_PLAN:15 | B-02 row | TRUE | Same evidence as FS-37 |
| LP-05 | LINE_PLAN:16 | B-01: exact allowlists, 11 script tests, pure test | TRUE | Same evidence as FS-35 |
| LP-06 | LINE_PLAN:16 | The real with-env.mjs launcher starts a fake bin/api and bin/worker | OVERCLAIM | Only bin/api (DT-05) |
| LP-07 | LINE_PLAN:17 | B-04 row | TRUE | dfec052 diff |
| LP-08 | LINE_PLAN:18 | IR-05 row: CheckMigrationLineage stops with a typed error when tool_invocations exists without 00080 | TRUE | lineage.go |
| LP-09 | LINE_PLAN:18 | IR-05 evidence: test executed against the real migrate command | OVERCLAIM | The test calls CheckMigrationLineage directly; manual run only in the commit message (DT-14) |
| LP-10 | LINE_PLAN:19 | IR-02 row: order traffic gate, pre-verification, pre-read, slot, transaction; anonymous never read; 400 invalid_request without audit row | TRUE | access.go serve (lines 258-310) |
| LP-11 | LINE_PLAN:19 | IR-02 timings 8.55 s without / 1.6 s with; TestM14/TestM13API/TestM12API/TestM3 pass | UNVERIFIABLE | Needs a database |
| LP-12 | LINE_PLAN:20 | IR-03 row: set-up retry (six attempts, readers 20), test server aborts stalled handler; worker-policy test unchanged | TRUE | Diffs; the worker-policy test file is unchanged since 552fa9e |
| LP-13 | LINE_PLAN:20 | IR-03 reproduction counts (3 of 3, 11 of 150, 0 of 600) | UNVERIFIABLE | Commit-message figures, no log in the tree |
| LP-14 | LINE_PLAN:21 | IR-04 row: 5 script tests counted, 9 now, 11 after B-01; make check vets with the integration and performance tags | TRUE | git show 552fa9e (5 in the table, 9 tests); node --test 11; Makefile:65 |
| LP-15 | LINE_PLAN:22 | Combination test row | TRUE | Test source (PASS 18.6 s: NOT RUN) |
| LP-16 | LINE_PLAN:24-35 | Gate counts: 84 + 43 checksums, 27 packages, 11 script tests, 126 tests in 19 files, 443 listed / 442 run, 5 fuzz targets by name, 11 and 7 browser cases, outage 3/3/3/3/19 | TRUE | Re-derived (verify-docs.mjs, go list, node --test, vitest, go test -list, Makefile, Playwright specs); outage figures equal A and R7b |
| LP-17 | LINE_PLAN:24-35 | Durations and PASS of the gate steps on f6398bf (30 s, 209 s, 24 s, 1956.5 s, 81 s, 610 s) | UNVERIFIABLE | No durable log; consistent between plan and handoff; make check re-run by this audit |
| LP-18 | LINE_PLAN:29-30 | Results measured on 6a1da17 stand for f6398bf | TRUE | git diff 6a1da17 f6398bf = one integration-tagged test file |
| LP-19 | LINE_PLAN:37-39 | Not run and not claimed: performance gates, driverpanic, Redis ACL test, final review, legacy artifacts | TRUE | Consistent with the reports and with the final review's Not covered |
| LP-20 | LINE_PLAN:41-46 | IR-02 residuals (1 MiB per verified POST, 10 s timeout, 400 without audit, slow headers) | TRUE | access.go preRead; httpserver ReadTimeout 10 s asserted by the test |
| LP-21 | LINE_PLAN:50-53 | cmd/migrate has no out-of-order option; a database from this branch is at 90 | TRUE | No WithAllowOutofOrder in the tree; 00090 is the last migration |
| LP-22 | LINE_PLAN:56-63 | Rule proposed: next number 00091; numbers 58-79 unused; ADR reservations | TRUE | Highest file is 00090; ADRs 036-063 and 100-104 exist |
| LP-23 | LINE_PLAN:65-79 | B-03 section: 1.3.0 keeps one meaning; inventory places exist | TRUE | v1.3 schema identical to b00da36 and has no tool_ref; release_bundles.body, workflow_bindings.ir_body/plan_body, metadata_versions exist |
| LP-24 | LINE_PLAN:71-72 | Known holders: only MAIN; no production database exists | UNVERIFIABLE | Deployment fact; config gates show production cannot create tool IR through preview routes (DT-20) |
| LP-25 | LINE_PLAN:84-85 | Integration still points to 552fa9e | STALE | be6ec7c now (DT-01) |
| LP-26 | LINE_PLAN:89-96 | Slice A and B results as quoted | TRUE | A and B reports |
| LP-27 | LINE_PLAN:106-112 | Reservations 58-79/064-099 and 80-99/100-119 | TRUE | User reservation text; consistent with files |
| LP-28 | LINE_PLAN:108 | A database already at 89 | STALE | 90 (DT-10) |
| LP-29 | LINE_PLAN:116-121 | Original status: merged, gate green after one environment fix; source branches untouched, remediation still b00da36 | STALE | Historical; remediation head is e0fb83d |
| LP-30 | LINE_PLAN:128 | First parent b00da36: 116 commits, 264 files | TRUE | git rev-list --count; git diff --name-only |
| LP-31 | LINE_PLAN:129 | Second parent: 189 commits, 253 files | FALSE | 192 commits (FIXED, DT-08); 253 files right |
| LP-32 | LINE_PLAN:137-146 | Number map: migrations, ADRs, renamed tests, schema/IR/compiler, example document, policy version | TRUE | git show -M 8366090; test names exist; policy.Version m14-native-v11, remediation m9r-native-v6 |
| LP-33 | LINE_PLAN:150-156 | Decision 1: 1.3.0 approval roles, 1.4.0 integrity, 1.5.0 superset; DSL 2.0.0/2.1.0/2.2.0 | TRUE | models.go:9-19, compiler.go:18-32, plan.go:22-30 |
| LP-34 | LINE_PLAN:157-160 | The CHECK was the only object both lines' migrations changed | FALSE | control.worker_grants_safe and control.metadata_versions also (DT-09) |
| LP-35 | LINE_PLAN:161-164 | Derived worker guard: three remediation redefinitions, 00087 last, true as framework_worker | TRUE | Redefinitions 00047/00051/00052 and 00087 last; omits milestone 00081/00083/00084 |
| LP-36 | LINE_PLAN:165-174 | Wrappers, front door, wiring; deadline 90 minutes, CI 120 | TRUE | transaction.go withRuntime; Makefile:41; ci.yml:13 |
| LP-37 | LINE_PLAN:177-187 | Test alignments (m13DroppedMessages, r7bLastMigration, m9_fixture/m8_fixture, 1.5.0 fixtures) | TRUE | Names found in tests/integration |
| LP-38 | LINE_PLAN:194-205 | Original gate: 67 migrations, 439 listed / 438 run, 436 PASS + 2 FAIL (environmental) | TRUE | 57 + 10 = 67; counts equal the review's reading; durations NOT RUN here |
| LP-39 | LINE_PLAN:210 | Merged suite takes about 32 minutes | TRUE | 1904 s = 31.7 min |
| LP-40 | LINE_PLAN:215-225 | For the owner: AGENTS.md and CLAUDE.md stale; MAIN rebuilt not migrated; R6 held | TRUE | DT-06, DT-07; lineage guard |
| LP-41 | LINE_PLAN:235-240 | Analysis table 189/253 (7f2b29b), 116/264, 33 shared non-document files | TRUE | Re-derived |
| LP-42 | LINE_PLAN:244-265 | Trial merge: 23 files, 37 hunks, nine duplicated versions 00045-00053, ADR036-040 on both lines | TRUE | git merge-tree; ls-tree comparison |
| LP-43 | LINE_PLAN:246-247 | First trial showed 21 files and 38 hunks (before R7b) | UNVERIFIABLE | Pre-R7b trial not reproducible without its remediation head |
| LP-44 | LINE_PLAN:9-10 | Aggregate capacity, driver panic and worker-policy are the remediation lane (D1) | OVERCLAIM | Aggregate capacity is D5 in the remediation plan (DT-16) |
| AH-01 | HANDOFFS:3-29 | Entry 1 headline and A figures (27 packages, 7 cached, 9 Node, 126, 439 groups 1366.644 s) | TRUE | A report |
| AH-02 | HANDOFFS:31-45 | cmd/migrate/main.go:38 creates the provider without out-of-order; goose v3.28.0 rejects lower numbers | STALE | Line 45 after dfec052; goose behaviour confirmed in module source (DT-17) |
| AH-03 | HANDOFFS:47-67 | Reference figures (7,968, 301.897, 6,463, 350.588, 755.156, 822.729, 6.857 s) | TRUE | A report |
| AH-04 | HANDOFFS:75-150 | Entry 2 (B): 2298.676 s, 438/1/0, 2061.520 s, B-01..B-05 descriptions | TRUE | B report |
| AH-05 | HANDOFFS:110-113 | SHA-256 b3aa401e... of the old tool IR | UNVERIFIABLE | Export is an ignored local artifact |
| AH-06 | HANDOFFS:155-199 | Entry 3 (C): 16 checks, 91 links, patch check, D1-D7 proposal | TRUE | C report |
| AH-07 | HANDOFFS:206 | Integration code head f6398bf; later commits are documents only | TRUE | git diff --stat f6398bf be6ec7c |
| AH-08 | HANDOFFS:206 | I did not touch remediation/m9r-foundation | TRUE | origin/remediation/m9r-foundation = e0fb83d, no milestone commit |
| AH-09 | HANDOFFS:207 | Slice A/B/C documents merged unchanged | TRUE | git diff e0fb83d HEAD --stat -- docs |
| AH-10 | HANDOFFS:209-222 | SHARED file list (access.go, restore.go, runtime-env, Makefile, migrate, 00090, test helpers, documents) | TRUE | git diff 552fa9e be6ec7c --stat |
| AH-11 | HANDOFFS:219-220 | The gate test will tell you which SQL bodies still name the older versions | OVERCLAIM | Needs the new version added to knownSchemaVersions (DT-13) |
| AH-12 | HANDOFFS:226-231 | Answers per objection and fix commits (46d8a2a, dfec052, f997efe, 6c4cee6, 1fb0110) | TRUE | Commit subjects and diffs |
| AH-13 | HANDOFFS:231-233 | IR-03 third test is the remediation line's; 2.24 s merged vs 2.19 s on b00da36 | TRUE | Equals the independent review (interim and final) |
| AH-14 | HANDOFFS:235-238 | Numbering proposal (goose, 90, from 00091, renumber own unmerged) | TRUE | Consistent with plan and code |
| AH-15 | HANDOFFS:242 | Gate on f6398bf: 443 listed, durations, 11 + 7 passed, outage 610 s | UNVERIFIABLE | Counts re-derived (TRUE); durations have no log |
| RP-01 | REMEDIATION_PLAN:11-13 | Product tree 552fa9e; slice A docs e78282f; slice B a1d8b00 | TRUE | git log subjects |
| RP-02 | REMEDIATION_PLAN:18-26 | A and B results | TRUE | A and B reports |
| RP-03 | REMEDIATION_PLAN:27-32,43,316,466,468,509-512 | B-01/B-02/B-05 confirmed open; B-04 stale wording | STALE | Fixed on the integration branch (DT-03) |
| RP-04 | REMEDIATION_PLAN:33-36,609-611 | Independent review of the merge/M14 NOT RUN | STALE | See FS-11 (DT-11) |
| RP-05 | REMEDIATION_PLAN:62-69 | Reservations; merged database already at 89 | STALE | Reservations true; 89 is 90 now (DT-10) |
| RP-06 | REMEDIATION_PLAN:71-76 | Version meanings 1.3/1.4/1.5 and DSL 2.0/2.1/2.2 | TRUE | Code constants |
| RP-07 | REMEDIATION_PLAN:493-512 | D1-D7 content and mapping | TRUE | Consistent with the ledger rows |
| RP-08 | REMEDIATION_PLAN:84-174,380-435 | Historical numbers (304 groups, 884.394 s, 283, 275, 269, 246, 230, 217, 206; p95 values) | TRUE | Traced to the implementation reports |
| PR-01 | PROPOSAL:9-14 | Baseline SHA-256 of AGENTS.md and CLAUDE.md | TRUE | sha256sum equals the table; unchanged since b00da36 |
| PR-02 | PROPOSAL:36-176 | The patch applies to the current files; index ids fd94ab4..90bb183 and 8612746..1ed343d | TRUE | git apply --check exit 0 on scratch copies; resulting blob ids reproduce |
| PR-03 | PROPOSAL:68-69,103-104,126 | B-02/B-05 remain open; B-01 settings remain dropped; applied maximum is 89 | STALE | Fixed in dfec052/46d8a2a; maximum is 90 (DT-04) |
| PR-04 | PROPOSAL:77-85,119-125 | RC only for the freeze route; m8_fixture/m9_fixture; m13DroppedMessages; UpTo | TRUE | access.go:221-224,318; tests; transaction.go |
| SM-01 | SUPERSESSION_MAP:8-20 | Quoted source-kit clauses (roadmap M8/M10, promotion steps 7-8, backup recompiles, V3 milestone numbering) | TRUE | Four of the twelve rows opened in the V2/V3 sources; all cited paths exist under docs/ or docs/v3/ |
| SM-02 | SUPERSESSION_MAP:27-43 | Capability inventory baseline 81b88d2 and the M12/M13/M14 rows | TRUE | Commit exists; ADR102-104 and reviews agree |
| SM-03 | SUPERSESSION_MAP:50-65 | R3c, R3d, R7a references (8f9805e, cf45300) | TRUE | Commits and reports exist |
| RB-01 | M10_M11/M12/M13/M14 reports:3-6 | Integration note: migration map, ADR map, 1.3.0 read as 1.5.0, tool plans 2.2.0 | TRUE | Equals the verified map |
| RB-02 | M10_M11_REPORT:20 | Tool plans (DSL 2.1.0) | STALE | Pre-review shape; banner says 2.2.0 (DT-19) |
| RB-03 | M10_M11_REPORT:8 | Source claude/m10-m11-tools-sandbox at bf70752 | TRUE | Commit exists; worktree head |
| RB-04 | M14_REPORT:10-11 | Independent review of M14 has not been run | STALE | Final M14 review exists at origin/review/integration-final (DT-11) |
| RB-05 | ADR100:125-228 | Amendment list A1-A4, gateway amendment, I1 tokens and constants | TRUE | Code constants; 00080 CHECK lists 1.0.0-1.5.0 |
| RB-06 | ADR101-ADR103 amendments | Migration numbers inside amendments (81, 85, 86, 87, 84, 83, 88; 00089) | TRUE | Each number matches the renamed file |
| RB-07 | ADR104:290-293 | 00089 replaces control.worker_grants_safe from the migration-84 definition | STALE | 00089:289 only comments; Amendment B.7 corrects (DT-18) |
| RB-08 | ADR103 amendment 12 | M13R-01 a veto is not overtaken | OVERCLAIM | rollback_release writes no fence (00088:99-128) (DT-23) |
| IV-01 | INTEGRATION_REVIEW (interim, in tree) | 00052:145 selects schema_version 1.4.0; trigger refuses with 55000 | TRUE | 00052:145-146, :224 |
| IV-02 | INTEGRATION_REVIEW (interim, in tree) | IR-02 evidence lines at 552fa9e (access.go 251-260, 289; ai.go:88; datasets.go:34-40; promote.go:103-113; preview.go:34-38; metadata.go:45); default 300 per second | TRUE | git show 552fa9e:path; traffic/gate.go:27 |
| IV-03 | INTEGRATION_REVIEW (interim, in tree) | IR-04 line references into the ledger at 552fa9e (17, 30, 160, 175, 183, 207) | TRUE | git show 552fa9e |
| IV-04 | INTEGRATION_REVIEW (interim, in tree) | Decision 2 literal list: CHECK rewrites 00017, 00020, 00045, 00052, 00080 and 00052:145; intersection of both migration sets | TRUE | grep over migrations; own extraction found the same three objects |
| IV-05 | INTEGRATION_REVIEW (interim, in tree) | 103 SECURITY DEFINER functions; 99 with pg_catalog,pg_temp; 4 with pg_catalog (R7b); 60-row allowlist; 16 table-level SELECT relations | TRUE | Static extraction: 103 after type normalising; 4 late pg_catalog-only (00055, 00056); 00026:38-43 re-pins the rest; 60 rows and 16 relations counted in 00087 |
| IV-06 | INTEGRATION_REVIEW (interim, in tree) | Live catalog facts (RLS enabled and forced on 71 of 72 tables, grants, ACLs, execute lists), gate durations, 45 POST routes | UNVERIFIABLE | Needs a database or a running router (NOT RUN) |
| FIN-01 | INTEGRATION_REVIEW final (review branch) | IR-06 rollback_release writes no release_fences row; reject_release and activate_release do (00088:51,70,99-128) | TRUE | 00088 read; the overtaking race itself NOT RUN |
| FIN-02 | INTEGRATION_REVIEW final (review branch) | Session 2 full run 433 PASS / 5 SKIP / 0 FAIL in 1880.094 s; 5 opt-in skips | UNVERIFIABLE | Needs a database; the five skip files exist (m9r_aggregate, choice_review, native_review, search_review, unique_review) |
| FIN-03 | M14_INDEPENDENT_REVIEW (review branch) | Line references M14R-01..M14R-07 and constants (in-flight 2/8, daily 200,000, wall clock 60 s, 409 ai_conflict) | TRUE | Twenty-one references checked in internal/ai, access/ai.go, audit.go, httpserver.go, 00089, ADR102/104 |
| FIN-04 | M14_INDEPENDENT_REVIEW (review branch) | Probe results (503 for a label with a space, 20 uncharged cancelled calls, 3 of 6 results lost, used_tokens 100,005,674, 10 of 28 secrets survive) | UNVERIFIABLE | Executed probes need a database; code paths cited are TRUE |
| AC-01 | AGENTS:3-6 | Current work remediation; do not begin M10 | STALE | M10-M14 are merged preview (DT-07) |
| AC-02 | CLAUDE:13 | Do not continue M10 without the remediation gate | STALE | DT-07 |
| AC-03 | AGENTS:122-123 | Only local.noop is registered; external tools remain later milestones | FALSE | app.go:59-77,156-160,260-270; ADR100 (DT-06) |
| AC-04 | AGENTS:116 | Event jobs RC; API/workflow stay RR | STALE | The sandbox-freeze route is an API route at READ COMMITTED (access.go:221-224,318) |
| AC-05 | AGENTS (whole file) | Module contracts list ends at ADR063 | STALE | No ADR100-104 (M10-M14); ADR044 names only 1.4 |
| AC-06 | AGENTS:129-132 | Commands make setup, infra-up, migrate, check, test, build, browser-install, test-e2e | TRUE | All targets exist in the Makefile |
| AC-07 | AGENTS:50-125 | ADR numbers and topics 014-063; API_DB_CONCURRENCY 1..10, default 4, FIFO 128 | TRUE | docs/adr names; config.go:58-61; app.go:248 |
| RM-01 | README:14,645,652,757 | M10 stays paused; follow the plan before M10; no AI execution capabilities; only local.noop, external tools are M10 | STALE | M10-M14 preview exist (DT-24) |
| ME-01 | all audited docs | Relative links resolve | TRUE | 22 files checked; 14 hits are inside the embedded patch (paths relative to the repository root) |
| ME-02 | six core docs | Repository paths named exist | TRUE | Only the old name workflow-tool-draft-v1.3.json is missing, as the before value of the rename table |
| ME-03 | all audited docs | Every hash-like token is a commit | TRUE | git cat-file -e on all 7-40 character tokens |
| ME-04 | AGENTS, CLAUDE, README, docs | Every backticked make target exists in the Makefile | TRUE | 36 targets |
| ME-05 | all audited docs | Test names cited exist | TRUE | 873 test funcs; only prefix patterns, table words and untracked reviewer probes (by design) are not definitions |
| ME-06 | tables | Version tokens match code | TRUE | models.go, compiler.go, plan.go, rules/types.go, contracts/jsonschema |
| ME-07 | docs | Counts 443 / 27 / 11 / 126 / 19 / 9 / 5 / 68 | TRUE | go test -list, go list, node --test, vitest, grep, ls |
| ME-08 | ADR100-104, reports, READMEs | No unmapped old migration or ADR number remains | TRUE | grep over docs and code; every residue is a banner, Amendment I1 or a remediation-line ADR |
| RB-09 | M13_REPORT:99 | Re-run recipe: the reset applies migrations 1-53 | STALE | Old numbering; 1-44 and 80-88 in the integrated tree (FIXED, DT-25) |
| ME-09 | tests/integration | 443 tests listed = 439 at the pin plus four added by the follow-up | TRUE | git diff 552fa9e be6ec7c adds exactly TestM14SlowRequestBodies..., TestM9RUniqueLifecycle..., TestMigrationLineage..., TestIntegrationTenantRoleApprover...; none removed |
| TR-01 | trunk 43d417e | Eleven trunk commits since be6ec7c changed documents only in AGENT_HANDOFFS.md, LINE_INTEGRATION_PLAN.md and M9R_FINDING_STATUS.md | TRUE | git diff --stat be6ec7c 43d417e |
| TR-02 | 953be65 message | 18 spellings recognised, 14 near-misses refused, 8 forms through the real launcher, migrate keeps the whole environment | TRUE | runtime-env.test.mjs lists 18 and 14 entries and 8 forms; 13 script tests pass on the extraction |
| TR-03 | c76968f message | Body budget of 4 MiB per verified identity and 32 MiB in all, booked from the declared length | TRUE | bodybudget.go:23-26; the declared-length booking is review finding F-06 (rework uncommitted in /home/user/integ) |
| TR-04 | 8528fde message | The AI result exchange is tried up to 6 times within 6 s | TRUE | ai.go aiResultAttempts = 6, aiResultBudget = 6 s |
| TR-05 | a2c5c52 message | At most 16 abandoned provider calls are tolerated | TRUE | repair.go maxAbandonedCalls = 16 |
| TR-06 | 9d2928d message | The compensation scan is an uncorrelated NOT IN exported as workflow.CompensationScanSQL | TRUE | execution.go:273-281 |
| TR-07 | bfa47c4 message | 00091 replaces rollback_release byte for byte except the fence write; the post-check requires the fence statement | TRUE | Function bodies of 00088 and 00091 differ in four added lines (three comments and the INSERT into control.release_fences); 00091:60 |
| TR-08 | b05494a message | The numbering rule and the migrate failure messages are tested without a database | TRUE | go test ./db/migrations ./cmd/migrate PASS on the extraction |
| TR-09 | 43d417e message | The version scan reads every listed spelling (unit part) | TRUE | go test ./internal/metadata PASS on the extraction |
| TR-10 | 43d417e message | Live catalog of a migrated database: 1,020 definitions (150 functions, 260 constraints, 225 policies, 102 triggers, 165 defaults, 3 views, 115 indexes), five naming a version | UNVERIFIABLE | Needs a migrated database (NOT RUN) |
| TR-11 | trunk 43d417e | Counts at the trunk head: 454 integration tests, 29 of 40 packages with tests, 13 script tests, 69 migration files, 9 fuzz functions | TRUE | go test -list, go list, node --test, ls, grep on the extraction |
| TR-12 | db/migrations/numbering_test.go:10-14 | Comment cites docs/verification/UPGRADE_AND_ORDERING_REHEARSAL.md | FALSE | No such file in the trunk tree; it exists on branch audit/upgrade-rehearsal only (DT-26) |
| TR-13 | round-2 review F-05 | approve_release has only its 00084 definition; neither 00088 nor 00091 replaces it | TRUE | grep -l 'FUNCTION control.approve_release' db/migrations/*.sql |
| TR-14 | proposal patch (corrected) | The patch applies to the unchanged AGENTS.md and CLAUDE.md; result blob ids 6866689 and 1ed343d | TRUE | git apply --check exit 0 on scratch copies; git hash-object after apply |
