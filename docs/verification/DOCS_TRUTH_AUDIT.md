# Documentation truth audit

Independent audit of the written documentation of `integration/m9r-m14`. Pinned tree: `be6ec7c`
(`git log -1` of the worktree before this report; the audit branch is `audit/docs-truth`). The auditor did not write or
fix any audited statement. Question asked of every statement: does the tree, the git history or an executed command prove
it? Classes: TRUE, FALSE, STALE (was true, no longer), OVERCLAIM (stronger than its evidence), UNVERIFIABLE (no durable
evidence in the tree and not reproducible without a database or docker). Result words follow the project rule PASS / FAIL /
NOT RUN; an unexecuted check is NOT RUN, never PASS.

Status: IN PROGRESS. This file is committed early and updated at least every 30 minutes.

## Progress

| Scope | Item | State |
|---|---|---|
| 1 | `M9R_FINDING_STATUS.md` current checkpoint | DONE (first pass) |
| 1 | `M9R_FINDING_STATUS.md` live findings table, all 43 rows | DONE (first pass) |
| 1 | `M9R_FINDING_STATUS.md` appendix map | DONE |
| 2 | `LINE_INTEGRATION_PLAN.md` follow-up section, checkpoint, original record | DONE (first pass) |
| 3 | `AGENT_HANDOFFS.md` (4 entries) | DONE (first pass) |
| 3 | `M9R_REMEDIATION_PLAN.md` | DONE (status statements); historical progression only spot-checked |
| 3 | `M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md`, patch applicability | DONE (`git apply --check` on scratch copies) |
| 3 | `V2_V3_SUPERSESSION_MAP.md` | DONE (first pass) |
| 4 | M10-M14 report banners, ADR100-104, `INTEGRATION_INDEPENDENT_REVIEW.md` | DONE (static claims); database claims NOT RUN |
| 5 | `AGENTS.md` / `CLAUDE.md` stale list (report only) | DONE |
| 6 | Mechanical checks (links, paths, hashes, make targets, migrations, tests, versions, counts) | DONE for the six core documents; remaining docs in progress |
| end | `make check` once | NOT RUN (reserved for the very end) |

## Defects

Severity: HIGH = a security, isolation or gate result stated against the evidence; MEDIUM = false or misleading claim that
could change a decision; LOW = stale wording, typo, small count error. Action FIXED = corrected in this branch (see
"Edits made"); REPORTED = left to the owner (a status call, a rewording, or a file this audit may not edit).

No HIGH defect was found. The gate results that the documents state as PASS are not contradicted by anything in the tree;
the ones that cannot be checked without a database or docker are listed under "Not covered" as NOT RUN.

| Id | Sev | Document and line | Statement | Evidence | Action |
|---|---|---|---|---|---|
| DT-01 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:224`; `docs/migration/LINE_INTEGRATION_PLAN.md:84-85` | "integration/m9r-m14 remains `552fa9e`" / "Integration still points to `552fa9e`" | `git rev-parse origin/integration/m9r-m14` = `be6ec7c` (code head `f6398bf`); `552fa9e` is an ancestor, 12 commits behind. FALSE now; both sentences sit under or beside text that says the fixes are on the integration branch | REPORTED (status text, dated checkpoint) |
| DT-02 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:242-244` (section "Execution order and stop gate") | "Current combined gate is FAIL: ... as do B-01/B-02/B-05" | Contradicts the same file: lines 28-30 and rows B-01, B-02, B-05 (lines 225, 226, 229) say fixed on `integration/m9r-m14` (`dfec052`, `46d8a2a`). The follow-up edit changed 9 rows and one bullet and left this paragraph | REPORTED |
| DT-03 | MEDIUM | `docs/migration/M9R_REMEDIATION_PLAN.md:27-32, 43, 316, 466, 468, 509-512` | B-01, B-02, B-05 "confirmed open integration gaps" / "remain open"; B-04 "stale version wording" | Fixed in `dfec052` and `46d8a2a` (negative controls executed by this audit, see "Verified TRUE"). The plan is the remediation lane's document and was not touched by the milestone follow-up (`git diff e0fb83d HEAD --stat -- docs` lists only the ledger, the integration plan, the handoffs and the review) | REPORTED |
| DT-04 | MEDIUM | `docs/migration/M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md` patch hunks (diff lines 68-70, 103-104, 125-128 of the file at lines `+  superset. B-02 ...`, `+  dropped until a verified fix lands`, `+  00080-00099 ... The applied maximum is 89`) | The "exact" patch for `AGENTS.md`/`CLAUDE.md` | Mechanically valid: baseline SHA-256 of both files match, `git apply --check` exits 0 on scratch copies, and applying to the scratch copies yields the blob ids named in the patch header (`90bb183`, `1ed343d`). But it would write now-false statements into the contract: "B-02 ... and B-05 ... remain open", "B-01 launcher settings remain dropped", "The applied maximum is 89" (a database built from this branch is at 90) | REPORTED |
| DT-05 | MEDIUM | `docs/migration/M9R_FINDING_STATUS.md:225`; `docs/migration/LINE_INTEGRATION_PLAN.md:16` | The real launcher is exercised "starting a fake `bin/api`/`bin/worker`" | `scripts/runtime-env.test.mjs:72-85` creates only `bin/api`; no worker child is launched, although the test title says "API and worker children". Worker propagation is pinned only by the pure allowlist test. Both new tests do fail against the pre-fix `runtime-env.mjs` (executed in a scratch copy: 9 pass, 2 fail) | REPORTED |
| DT-06 | MEDIUM | `AGENTS.md:122-123` | "Only `local.noop` is registered. ... external tools and production activation remain later milestones" | FALSE on this branch: `internal/platform/app/app.go:59-77, 156-160, 260-270` load the operator tool catalog and gateway; ADR100; `internal/runtime/workflow/tool_step.go`. (M13 activation is a preview pointer, so "production activation" stays true.) | REPORTED (file not editable) |
| DT-07 | MEDIUM | `AGENTS.md:3-6`; `CLAUDE.md:13` | "Do not begin M10 automatically" / "Do not continue M10 without the remediation gate and a new user task" | STALE for this branch: M10-M14 are merged preview code (ADR100-104, migrations 00080-00090). `AGENTS.md` also has no mention of ADR100-104 at all | REPORTED (file not editable) |
| DT-08 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:129` | Second parent `integ/prep-m14`: "189 commits and 253 files since the base" | `git rev-list --count e6305df..8366090` = 192 (189 is the count at `7f2b29b`, the analysis-time head used in the table at line 237); files 253 is right | FIXED |
| DT-09 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:157-160` | The `metadata_versions` CHECK "was the only object that both lines' migrations changed" | Mechanical extraction of function/table/constraint/policy/index/trigger names from `00045-00057` and `00080-00089`: the intersection is the CHECK, `control.metadata_versions` and `control.worker_grants_safe` (remediation `00047/00051/00052`, milestone `00081/00083/00084/00087`). The next bullet of the same list says so | REPORTED |
| DT-10 | LOW | `docs/migration/M9R_FINDING_STATUS.md:220`; `docs/migration/LINE_INTEGRATION_PLAN.md:108` | "Merged migrations 1-57/80-89 apply fresh"; "A database already at 89" | `ls db/migrations`: 68 files, `00001-00057` and `00080-00090`; the plan's own follow-up says a database built from this branch is at 90. True of slice A's run, stale as live status | REPORTED |
| DT-11 | LOW | `docs/migration/M9R_FINDING_STATUS.md:22-23`; `docs/migration/M9R_REMEDIATION_PLAN.md:33-36, 609-611`; `docs/migration/LINE_INTEGRATION_PLAN.md:103` | "Independent M14/merge review is NOT RUN" | `docs/verification/INTEGRATION_INDEPENDENT_REVIEW.md` is an independent review of the merge (status IN PROGRESS, five findings). It is interim, not final, and no M14 review exists in the tree; the sentence understates the assurance that exists | REPORTED |
| DT-12 | LOW | `docs/migration/M9R_FINDING_STATUS.md:28-30` | "B-01, B-02, B-04, B-05 and IR-01 to IR-05 are fixed" | Row 230 and the plan say one of the three timing tests of IR-03 (`TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry`) is not changed (D1), and IR-04's ledger refresh is incomplete (DT-01, DT-02). The bullet is stronger than its rows | REPORTED |
| DT-13 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:14`; `docs/migration/AGENT_HANDOFFS.md:219-220` | The version-gate test means "the next superset cannot repeat it" / "will tell you which SQL bodies still name the older versions" | `internal/metadata/sql_version_gate_test.go:25-27` hard-codes `knownSchemaVersions()`; a future 1.6.0 is invisible until someone adds it there. It reads only quoted `'1.x.0'` literals in function bodies and one CHECK. The scan does fail without `00090` (executed with `-overlay`) | REPORTED |
| DT-14 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:18` | IR-05 evidence: "`tests/integration/m_migration_lineage_test.go`, executed against the real `migrate` command" | The test calls `postgres.CheckMigrationLineage` directly; nothing in the tree executes `cmd/migrate`, so the three-line wiring in `cmd/migrate/main.go:35-40` is unpinned. The commit message of `dfec052` records a manual run; no log is in the tree (UNVERIFIABLE) | REPORTED |
| DT-15 | LOW | `docs/migration/M9R_FINDING_STATUS.md:230` | "IR-04 LOW: `make check` vets the tagged suites" | `Makefile:65` vets with `-tags=integration,performance` only. `tests/integration` also has files tagged `trafficreview`, `eventoutage`, `driverpanic` and `race` that this vet does not compile. The plan and the handoff word it correctly ("with the integration and performance tags") | REPORTED |
| DT-16 | LOW | `docs/migration/LINE_INTEGRATION_PLAN.md:9-10` | The recorded FAILs "(aggregate capacity, injected driver panic, worker-policy statement timeout) ... are its lane (D1)" | `M9R_REMEDIATION_PLAN.md:501,505`: D1 is worker-policy, driver panic and the connection failure; the aggregate capacity miss belongs to D5 (the ledger row 200 says D5) | REPORTED |
| DT-17 | LOW | `docs/migration/AGENT_HANDOFFS.md:32` | `cmd/migrate/main.go:38` creates a Goose provider without `WithAllowOutofOrder(true)` | True at `552fa9e`; after `dfec052` the call is at `cmd/migrate/main.go:45`. `WithAllowOutofOrder` appears nowhere in the tree | REPORTED |
| DT-18 | LOW | `docs/adr/104-m14-ai-builder-and-intent-capture.md:290-293` (A1) | Migration 00089 replaces `control.worker_grants_safe()` from the migration-84 definition with three tables added | `db/migrations/00089_m14_ai_proposals.sql:289` only mentions the guard in a comment; the last definition is `00087`. Amendment B.7 (lines 596-599) of the same ADR corrects it but A1 is not struck | REPORTED |
| DT-19 | LOW | `docs/migration/M10_M11_IMPLEMENTATION_REPORT.md:20` | Tool plans "(DSL 2.1.0)" | The banner (line 6) says "Tool plans keep workflow DSL 2.2.0" and line 51 says "DSL 2.2.0 tool plans"; 2.1.0 is the pre-review shape (ADR100 A1). Old and new token in one report without a mapping for this one | REPORTED |
| DT-20 | LOW | `docs/migration/M9R_FINDING_STATUS.md:227`; `docs/migration/LINE_INTEGRATION_PLAN.md:71-72` | "Only the development database MAIN holds such data"; "No production database exists (the configuration rejects workflows and events in production)" | UNVERIFIABLE deployment facts. The plan hedges ("Known holders"), the ledger does not. `internal/config/config.go:77-89, 113, 125` and `app.go:270` (`PreviewEnabled` only in development/test) show that production cannot create tool IR through the preview routes; they do not show that no other database holds milestone-numbered data | REPORTED |
| DT-21 | LOW | `docs/migration/M9R_FINDING_STATUS.md:194, 195, 199` | Slice A's reference results (numeric 124.505 ms, writes 7,968 / 301.897 ms, unique 331.310/479.989/350.588 ms) stand in the "Live findings" table without the tree they were measured on | Measured at `552fa9e`; `serve` in `internal/platform/access/access.go` and `runtime.register_unique_constraint` changed afterwards and the reference gates were NOT RUN again (the plan says so at line 37; row 199 says it for unique only) | REPORTED |

## Verified TRUE (what is solid)

Re-derived by this audit, not copied:

- Number map (migrations `00045-00053,00060` to `00080-00089`, ADR036-040 to ADR100-104): `git show -M 8366090` lists the ten migration and five ADR renames; `db/migrations` and `docs/adr` match. No old milestone number is left in ADR100-104, the M10-M14 reports or the READMEs except where a banner or Amendment I1 names it.
- Version tokens in the tables match the code: `internal/metadata/models.go:9-19` (1.0.0, 1.1.0, 1.2.0, 1.3.0, 1.4.0, 1.5.0), `internal/compiler/compiler.go:18-32` (IR and compiler 1.0.0 to 1.5.0), `internal/runtime/workflow/plan.go:22-30` (DSL 2.0.0, 2.1.0, 2.2.0), `internal/runtime/rules/types.go:10` (2.0.0). `contracts/jsonschema/application-v1.3/1.4.schema.json` are byte-identical to the remediation line `b00da36`; only 1.5 names `tool_ref`.
- Counts: 443 integration tests listed with `-tags=integration` (`go test -list`); 27 Go packages with tests in `GO_PACKAGES`; 11 script tests (`node --test scripts/*.test.mjs`, 11 pass); 126 frontend tests in 19 files (vitest, 126 pass); 9 fuzz functions; 5 fuzz make targets; 11 + 7 Playwright cases (static count of the specs); 68 migration files.
- The merge record: 23 conflicting files and 37 hunks (re-derived with `git merge-tree`), 33 non-document files changed by both lines, nine duplicated migration versions `00045-00053`, 116/264 and 189/253 commit/file counts (`7f2b29b`), `integ/prep-m14` = `60da6f2` plus one commit.
- Migration `00090` is `00052`'s function byte for byte except the predicate (diff), with an owner/ACL/other-literal post-check; `00087` is the last definition of `control.worker_grants_safe`; migrations 1-57 and schemas 1.3/1.4 are unchanged against `b00da36`.
- B-02 fix: `internal/compiler/restore.go` pins the pair for IR 1.5.0. The regression test passes on the tree and fails (1.5.0 subtest, "unsupported compiler") with the pre-fix `restore.go` through `go test -overlay`.
- B-05 scan: `TestSQLFunctionsNameWholeSchemaVersionFamilies` passes on the tree and fails with `00090` hidden through `-overlay`.
- B-01: both new script tests pass on the tree and fail on the pre-fix `runtime-env.mjs` (scratch copy); web and dispatcher get none of the AI/tool variables.
- IR-02 front door order (`access.go`): traffic gate, token pre-verification, local-command / idempotent-release / generic body pre-read for verified bearers only, admission slot, transaction. The two replay unit tests pass.
- `config.go:77-89` rejects workflows and events in production; `:113` internal tool networks; `:125` the AI provider.
- Slice A/B numbers quoted in the checkpoint and the rows (439 groups, 438 PASS / 1 FAIL / 0 skips, 2061.520 s, 822.729 ms, 6.857 s, 7,968 writes, 301.897 ms, 479.989 ms, 350.588 ms) are the numbers of `M9R_MERGED_REBASELINE_REPORT.md` and `M9R_MERGE_REVIEW_B_REPORT.md`.
- The agent-rule baselines: `sha256sum AGENTS.md CLAUDE.md` equals the hashes in the proposal; the files are unchanged since `b00da36`.
- `INTEGRATION_INDEPENDENT_REVIEW.md` static claims: `00052:145` selects `m.schema_version='1.4.0'` (at the pinned `552fa9e`), 103 SECURITY DEFINER functions in `control/runtime/iam/audit` all with an explicit `SET search_path` (static extraction from the migrations after normalising argument types; the live catalog was NOT RUN).

## Not covered

- Everything that needs PostgreSQL, Redis, docker or a browser: the integration suite, the reference performance gates, the fuzz and outage runs, the browser suites, the live-catalog claims of the independent review (RLS on 71 of 72 tables, grants, ACLs). The recorded durations and PASS results of the gate on `f6398bf` (209 s, 24 s, 1956.5 s, 81 s, 610 s) have no durable log in the tree: UNVERIFIABLE, consistent between the plan and the handoff.
- The historical checkpoint progression (lines 36-178 of the ledger) and the 2026-10-17/19 evidence-audit tables (`file:line` citations against trees `dbc4b5b` and `e6b7877`) were spot-checked (numbers traced to the implementation reports) but not re-derived line by line.
- `review/m14-and-fixes` (a second reviewer in progress) is not part of the pinned tree and was not read.

## Commands run

See the end of the file (kept current).
