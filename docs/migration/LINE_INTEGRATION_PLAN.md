# Integrating the milestone line (M10–M14) with remediation

## Milestone-agent follow-up on the integration branch — 20 October 2026 (UTC)

The findings of the remediation agent's slice B (B-01, B-02, B-04, B-05) and of the interim
[independent review](../verification/INTEGRATION_INDEPENDENT_REVIEW.md) (IR-01 to IR-05) that fall in the milestone lane or in
the shared integration code are **fixed on `integration/m9r-m14`**. "Fixed" here means: the probe that failed now passes, a
regression test pins it (and, where stated, was shown to fail without the change), and the milestone agent ran it. No fix has been
re-reviewed independently, and the remediation agent has not re-verified them. The recorded FAILs of the remediation line
(aggregate capacity, injected driver panic, worker-policy statement timeout) are untouched: they are its lane (D1).

| Finding | What was wrong | Fix | Evidence |
|---|---|---|---|
| B-05 / IR-01 (MEDIUM) | `runtime.register_unique_constraint` selected only metadata 1.4.0. Under 1.5.0, the only schema that admits tools, no unique claim could be registered and every write to a unique field failed closed (409 forever): an availability defect, no integrity breach | migration `00090_integration_unique_schema_1_5.sql` replaces the function from its latest definition (`00052`) with `schema_version IN ('1.4.0','1.5.0')`; owner, grants and guards unchanged, a post-check refuses any other state. `internal/metadata/sql_version_gate_test.go` scans the latest body of every SQL function and the last rewrite of the `metadata_versions` CHECK for a gate that names only part of a version family, so the next superset cannot repeat it | the scan fails without `00090` (negative control); `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema` takes 1.4.0 and 1.5.0 through 409, registration, READY, a create that succeeds and a duplicate that conflicts |
| B-02 | `Restore` pinned the schema/compiler pair of an integrity IR (1.4.0) but not of the 1.5.0 IR: a re-hashed 1.5.0 artifact with a forged compiler label restored | `internal/compiler/restore.go` pins the pair for IR 1.5.0 too; older IR keep their historical provenance | `internal/compiler/tool_ir_identity_test.go` re-hashes forgeries of both the 1.4.0 and the 1.5.0 artifact (unsupported compiler, `0`, the other contract's compiler, an approval IR label, a moved unique flag), with a control that restores the unmodified artifact |
| B-01 | `scripts/runtime-env.mjs` dropped `AI_PROVIDER_FILE` and `TOOL_CATALOG_FILE` for the API and the catalog, secrets, CA and internal-network settings for the worker, so a configured launcher ran without AI or tools | the minimum per-process allowlists; the web server and the dispatcher still receive none of them | two script tests: the pure allowlist, and the real `with-env.mjs` launcher starting a fake `bin/api` and `bin/worker` (11 script tests now) |
| B-04 | stale wording that named tool schema/IR 1.3 | comment and assertion text only; no serialized token changed | `make check` |
| IR-05 (LOW) | nothing refused a database that holds the milestone line's old migration numbers; it was safe only because migration 54 happened to need objects of the skipped ones | `postgres.CheckMigrationLineage` and `cmd/migrate` stop with a typed error when `runtime.tool_invocations` exists but `00080` is not applied | `tests/integration/m_migration_lineage_test.go`, executed against the real `migrate` command |
| IR-02 (MEDIUM) | a POST body was read by the handler inside the database admission slot, so four connections of a tenant owner that announced a body and sent a byte every 0.7 s held all four slots for the 10 s read timeout: an honest read waited 8 s and an AI proposal lost its result | `serve` reads the body of a verified-bearer POST before the slot and replays it, with the error of the front door's 1 MiB cap, to the handler; the handlers keep their own limits and error codes; an anonymous caller is never read | `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots` (real `httpserver.New` server, production timeouts, four slots): honest read 8.55 s **without** the change (FAIL), honest AI proposal 201 READY in 1.6 s with it; unit tests of the replay; `TestM14\|TestM13API\|TestM12API\|TestM3` pass |
| IR-03 (LOW) | two timing-bound tests failed on a slower host | (a) `TestM12APIDetailReadsOfALargeCandidateConverge` built its two large releases with single requests that can exceed the 3 s transaction budget; the set-up now repeats a typed 503/429 like a client honouring Retry-After and the readers get 20 attempts, while the claims are still measured on the unwrapped handler. (b) `TestTimeoutWhileServerSleepsIsUnknownAndBounded`: when the client's deadline passed, the sleeping test handler returned a default 200 that net/http can deliver instead of the deadline error (a standalone client and server show the same, 3 of 300), so the class was `invalid_response` instead of `timeout`; both are UNKNOWN_REQUIRES_RECONCILIATION, so the gateway was safe, but the test asserted one of two outcomes. The test server now aborts such a handler | (a) 3 of 3 PASS with the test pinned to 2 CPUs of which one is taken by a busy loop (about a quarter of the normal capacity); with almost no CPU left the set-up still fails on a 3 s request, which no retry repairs. (b) reproduced in 11 of 150 runs under load, 0 failures in 600 executions after the change. **Not changed:** `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` (the 2 s statement budget of the remediation line; the reviewer measured 2.24 s merged against 2.19 s before the merge on a slower host): D1 of the remediation line |
| IR-04 (LOW) | `make check` did not compile the tagged suites; the gate table counted 5 script tests; the ledger still said "M10 paused" | `make check` now vets `./tests/integration` with the `integration` and `performance` tags; the count is 9 (11 after B-01); the ledger rows are in [M9R_FINDING_STATUS.md](M9R_FINDING_STATUS.md) | `make check` |
| missing combination | no test joined a tool step with a tenant-defined approver | `TestIntegrationTenantRoleApproverGatesAToolSend`: a 1.5.0 workflow whose approval names `finance_manager`; the stored plan is DSL 2.2.0 with the tenant role and the pinned tool contract; an admin without the role, the requesting owner and a forged role claim get 403 and the provider sees nothing; a holder approves and exactly one send follows; after the role is revoked the next instance cannot be released until a holder acts | PASS (18.6 s); the same token is denied before the assignment, accepted after it, denied after the revocation and accepted again |

Gate on the code head `f6398bf` (21 October 2026, this host, the isolated Compose stack; the development database `MAIN` was not touched). One run of each step; the evidence
says nothing about flake rates beyond the IR-03 reproductions above.

| Step | Result |
|---|---|
| `make check` (docs checksums 84 + 43, format, `go vet` of the packages and of `./tests/integration` with the `integration,performance` tags, `go mod verify`, eslint, tsc) | PASS, 30 s on `6a1da17`, and PASS again on the final documents-only head of this update |
| `make test-unit` (27 Go packages with `-race`, 11 script tests, 126 frontend tests in 19 files) | PASS, 209 s (`6a1da17`) |
| `make build` | PASS, 24 s (`6a1da17`) |
| Full integration suite with `-race`, `-skip TestM8RedisACLStreamLossAndPendingRecovery` (443 tests listed, 442 run) | **PASS**, one uninterrupted run: 1956.5 s Go time, 2027 s wall including the stack reset. A first run on `6a1da17` was killed by a host reboot and is not counted |
| `make test-e2e` / `make test-signin-e2e` | 11 passed (1.0 min) / 7 passed (9.6 s) |
| `make test-rules-fuzz test-events-fuzz test-workflows-fuzz` | PASS (5 targets of 10 s: FuzzWorkflowRestore, FuzzRuleRestoreAndInputs, FuzzRuleCompile, FuzzPointer, FuzzFixedDecimalMoney), 81 s |
| `make test-event-outage` (real ten-minute consumer absence) | PASS, 610 s test time (631 s with the stack reset): recovery after 600 real seconds with 3 completed, 3 inbox rows, 3 effects, 3 audits and 19 retained attempts |

Not run, and so not claimed: the reference-scale performance gates and the `driverpanic` diagnostic of the remediation line, `TestM8RedisACLStreamLossAndPendingRecovery`
(coupled to the main Compose project), a final independent review of the merge and of M14, and the opt-in checks of retained legacy API artifacts. The recorded FAILs of the
remediation line (aggregate capacity, injected driver panic, worker-policy statement timeout) are unchanged by this work.

Residuals of the IR-02 fix, so that nobody reads it as more than it is: the memory held by pre-read bodies is at most 1 MiB per in-flight
verified POST (the front door's cap) and is bounded in number only by the traffic gate and the 10 s read timeout, not by a connection limit;
connection limits and body shaping belong to the edge and to deployment sizing (ADR061, remediation D6). A body that cannot be read (client gone,
read timeout) is answered 400 `invalid_request` before the transaction and writes no audit row, where the old path let the handler answer a typed 400
inside it. A client that sends its headers slowly or never finishes its body still occupies a connection until the read timeout; it no longer
occupies a database slot.

### Migration numbers from here on (proposal; the owner decides)

Facts: `cmd/migrate` opens goose without out-of-order support, and goose refuses any file numbered below the highest applied
version (the review's IR-05 probe shows "missing (out-of-order) migrations lower than database version"). A database built from
this branch is at 90. A migration numbered 58 to 79, which the earlier reservation gave to the remediation line, would therefore
be refused on every upgrade, and switching out-of-order on would make a fresh install (58 before 80) and an upgraded database (58
after 90) apply the same files in different orders, which is not the same schema state wherever two migrations touch one function.

Rule proposed (until the owner decides otherwise the reservation of 58 to 79 is **not** used by anyone):

1. There is one increasing sequence. The next number is `00091`; every later migration takes the highest number on
   `integration/m9r-m14` plus one at the time its line merges the integration branch. Numbers 58 to 79 stay unused, as in every database built so far.
2. A number is final when it is pushed on `integration/m9r-m14`. If two lines add the same number, the line that merges later renumbers
   **its own unmerged** migration (and the tests and documents that name it) before merging. Nobody renumbers a merged one.
3. Out-of-order execution stays off. Forward rehearsals use `UpTo(n)`, never a counted `Up`.
4. ADR numbers keep their reservations (064 to 099 remediation, 100 to 119 milestone): documents have no order constraint.

### Data written by the milestone line before the merge (B-03)

The merged `Restore` refuses a complete tool IR/compiler 1.3.0 (what `60da6f2` produced), by design: 1.3.0 keeps one meaning (approval
roles) and admits no tool activity. The standalone workflow plan inside such an artifact (DSL 2.2.0, or the legacy 2.1.0 tool shape) still restores
byte for byte, but that does not make the surrounding release restorable. Consequences and the cutover rule:

* Known holders of such data: only the development database `MAIN` (numbers 45 to 53 and 60). No production database exists (the configuration rejects workflows and
  events in production). `MAIN` was not touched; `migrate` now refuses it (IR-05).
* Before any real old milestone database or artifact is moved, inventory three places: pinned releases (`control.release_bundles.body`, the candidate and
  active-release rows that reference them), pinned workflow instances (`runtime.workflow_bindings.ir_body` and `plan_body`) and source documents
  (`control.metadata_versions` rows of schema 1.3.0 that carry a tool activity). Classify each with the **old** binary (the branch
  `claude/m14-ai-builder` at `60da6f2` stays untouched for this) and choose per release: keep and serve it with the old reader, or re-author the source
  under 1.5.0 and build a new candidate through the normal approval path.
* Never relabel the stored bytes, rewrite a hash or recompile an in-flight instance silently. A pinned artifact keeps its bytes.
* Nothing here is an executed migration: no customer data exists to inventory and no old database was migrated.

## Current remediation checkpoint — 20 October 2026

**M9R PARTIAL; combined acceptance FAIL.** The original merge record below
remains historical evidence at `552fa9e`. Remediation now includes it and
review checkpoint `a1d8b00` on `remediation/m9r-foundation`. Integration
still points to `552fa9e`; main remains `9da3f5f`. No PR/main merge or
production deployment is performed.

[Slice A](M9R_MERGED_REBASELINE_REPORT.md) subsequently ran the complete owned
gate: full integration/browsers/outage/fuzz pass, but aggregate capacity and
injected driver-panic recovery FAIL. [Slice B](M9R_MERGE_REVIEW_B_REPORT.md)
reviewed all 23 resolutions and records a fresh full integration FAIL
(438 PASS, 1 worker-policy statement timeout, zero skips), plus the confirmed
launcher, compiler-pair and SQL uniqueness-version gaps. These later results
supersede an unqualified current-green reading of the earlier merge gate.
No causal merge regression is proved without controlled comparison.

Current scope, R6 coverage/ownership, open evidence and proposed R7 sequence are
in the [remediation plan](M9R_REMEDIATION_PLAN.md),
[live finding ledger](M9R_FINDING_STATUS.md) and [handoff](AGENT_HANDOFFS.md).
M10–M14 exist as preview; this agent builds remediation only. R6 stays held for
the milestone agent, including production data plane and verifier credential.
M14/merge independent review is NOT RUN; latest fixes retain their review limits.
Root-rule updates are [proposed only](M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md).

Current user reservations are remediation migrations 00058–00079/ADR064–099
and milestone migrations 00080–00099/ADR100–119; the older “00090 and up free”
wording below is historical. A database already at 89 needs an agreed, verified
fresh/upgrade ordering strategy before new lower-number migrations. No automatic
out-of-order switch, migration renumbering or shared database rebuild is
authorized by this document. The old milestone-numbered MAIN database is
preserved; any real data cutover needs a recovery plan.

## Original integration record (historical; retained verbatim below)

Status: **merged on the branch `integration/m9r-m14` on 2026-10-20; the gate below was run on it and is green after one environment fix (see the table).** Nothing was merged into `main`,
no pull request exists, and the two source branches are untouched: `origin/remediation/m9r-foundation` is still `b00da36` and
`claude/m14-ai-builder` (also `claude/dazzling-goldberg-69wbkn`) is still `60da6f2`. This merge is a branch for the repository owner to
review. It does not certify production readiness, and it does not make M10 to M14 accepted: the independent review of M14 is still not
run, and what M11 to M14 do not build of R6 (the production data plane, a separate verifier credential) and the rest of R7 (audit retention,
profiling, restore drill) are as open as they were.

## What the branch is

| | Value |
|---|---|
| Branch | `integration/m9r-m14` |
| First parent | `b00da36` (the remediation line after R7b, 116 commits and 264 files since the common base `e6305df`) |
| Second parent | `integ/prep-m14` (not pushed; `60da6f2` plus one commit that renumbers the milestone line, 192 commits and 253 files since the base) |
| Merge commit | records 23 resolved conflicts (below) |
| Follow-up commits | test alignment, see "What had to change besides the conflicts" |

The renumbering commit is the only change the milestone line needed before the merge; it keeps the content and order of everything it renames.

## The number map (read old reports through this)

| Kind | Milestone line before | Now | Note |
|---|---|---|---|
| Migrations | `00045` to `00053`, `00060` | `00080` to `00089` in the same order (45 to 80, 46 to 81, 47 to 82, 48 to 83, 49 to 84, 50 to 85, 51 to 86, 52 to 87, 53 to 88, 60 to 89) | `00058` to `00079` and `00090` and up stay free for the remediation line and for later work; the remediation migrations `00045` to `00057` are unchanged |
| ADRs | `036` to `040` (M10 to M14) | `100` to `104` | `064` to `099` stay free for the remediation line; ADR036 to ADR063 are the remediation line's |
| Tests renamed | `TestM13IntegrationUpgradeFrom48...`, `TestM11RMigration51...` | `...From83...`, `...Migration86...` | names carried a migration number |
| Authoring schema, IR, compiler with tool activities | `1.3.0` | `1.5.0` | `1.3.0` (tenant approval roles) and `1.4.0` (`field.unique`) are the remediation line's and admit no tool |
| Example document | `contracts/examples/workflow-tool-draft-v1.3.json` | `workflow-tool-draft-v1.5.json` | |
| Policy version | `m14-native-v11` | `m14-native-v11` | the remediation line never changed it (`m9r-native-v6`); it only added the descriptive role contract |

`docs/verification/*` keeps the numbers its reviewers saw. Read those reports through this table.

## Decisions taken in the 23 conflicts

* **One meaning per version token.** Schema 1.3.0 stays approval roles, 1.4.0 stays integrity, and tool activities get 1.5.0 as a superset of
  1.4.0 (`application-v1.5.schema.json` was produced by a three-way merge of the tool delta into the 1.4.0 schema, no conflicts). Every gate
  that listed the approval and integrity versions also admits 1.5.0: `metadata.WorkflowSchema`, the new `metadata.IntegritySchema`, the compiler's
  rule, workflow and integrity restore checks, `SupportedIRVersions`, the frontend renderer (`contracts.ts`) and the AI schema map.
* **Workflow DSL.** `2.0.0` closed roles, `2.1.0` tenant roles (workflows author it in 1.3.0 and later), `2.2.0` pinned tool plans. The
  milestone line had used `2.1.0` for its pre-amendment tool shape; that shape is kept restore-only and is recognised by its tool states, so
  a tenant-role plan and a legacy tool plan never mix. Tool plans accept the tenant role grammar.
* **Migration `00080` rewrites a CHECK that both lines rewrite.** Remediation `00045` and `00052` install `metadata_versions_schema_version_check`
  with 1.3.0 and 1.4.0; the milestone version would have replaced it with a list that rejects 1.4.0 (and, on a database that already holds
  1.4.0 documents, would have failed). It now lists 1.0.0 to 1.5.0. This was the only object that both lines' migrations changed
  (a comparison of functions, tables, constraints, policies, triggers, indexes and grants found nothing else).
* **The derived worker guard.** `control.worker_grants_safe()` is redefined by three remediation migrations (fixed list) and by `00087`
  (derived). `00087` runs last, so it wins; evaluated as `framework_worker` on the merged schema it is true, and a diagnostic query found no
  privilege the worker holds that `worker_guard_granted()` does not list and no stale entry (the new remediation relations grant the worker
  nothing). `TestM12RWorkerGuard*` pass.
* **Runtime transaction wrappers.** `WithRuntimeIsolation` is now a thin wrapper on the remediation `withRuntime`; a READ COMMITTED executor does not
  certify a runtime snapshot (the sandbox freeze port does not need it; the index paths fall back).
* **HTTP front door.** Remediation traffic admission and token pre-verification stay first. The freeze route keeps READ COMMITTED, idempotent release
  POSTs keep body replay on a known abort, and role routes are matched by exact registered patterns (now including the remediation `GET` roles and
  `GET` role-contract routes, which a path suffix used to cover).
* **Wiring, configuration, Makefile and CI** carry both lines (tool catalog, AI provider, traffic gate, admission queue, sign-in store, probes). The
  integration test deadline moved to 90 minutes (CI job 120): both old values (20 and 40 minutes) are below what the merged suite needs.
* **Finding ledger.** `docs/migration/M9R_FINDING_STATUS.md` keeps the remediation table as the live ledger; the milestone line's audit
  (2026-10-17 and 2026-10-19) is an appendix with a number map. No row was re-graded by the merge.

## What had to change besides the conflicts

Seven integration tests failed on the first targeted run, none of them a product defect:

* `TestM13IntegrationReplacedFunctionsKeepTheirGuards` (milestone test) found that `runtime.register_numeric_index` lost the message `index budget`:
  remediation `00047` moved the shared quota into `runtime.check_index_budget`, which the function now calls. Recorded in `m13DroppedMessages`.
  This is the test doing the job it was written for, across both lines' migrations (39 functions compared).
* Three R7b cutover rehearsals applied "all remaining migrations" and counted them; they now stop at 57 (`r7bLastMigration`).
* `TestM9RWorkflowApprovalReview*` read `runtime.m9_test_effects`, a fixture table that the milestone line had to move out of the guarded schemas
  (`m9_fixture.effects`).

The fixtures that said 1.3.0 for tools (AI corpus of 412 items, tool example, renderer, unit and integration tests) say 1.5.0, with workflows at DSL 2.1.0
and rules unchanged at 2.0.0 (a first mechanical pass moved one rule too; the AI corpus caught it).

## Gate

All runs were made on this branch on 2026-10-20 against the isolated Compose stack of this host (the development database `MAIN` and its Compose
project were not touched). Durations are wall clock on this host; each suite ran once, so the evidence says nothing about flake rates.

| Step | Result |
|---|---|
| `make check` (docs checksums 84 + 43, format, vet, `go mod verify`, eslint, tsc) | PASS |
| `make test-unit` (27 Go packages with `-race`, 9 script tests, 126 frontend tests in 19 files) | PASS, 211 s |
| Empty database, all migrations | 67 migrations apply (`00001` to `00057`, `00080` to `00089`); the derived worker guard evaluated as `framework_worker` is true, with no unlisted privilege and no stale allowlist entry |
| Full integration suite with `-race`, `-skip TestM8RedisACLStreamLossAndPendingRecovery` (439 tests listed, 438 run) | 1904 s: 436 PASS, 2 FAIL. Both failures are `TestM9ROIDC*` and are environmental: the isolated stack's environment had no `SIGNIN_REDIS_URL` and no `framework_signin` Redis user. With that ACL user created as `scripts/m8-env.mjs` defines it, both PASS (11 s). After the test alignment above, the 7 earlier failures also PASS |
| `make test-rules-fuzz test-events-fuzz test-workflows-fuzz` | PASS (5 targets of 10 s) |
| `make build` (binaries and `vite build`) | PASS |
| `make test-e2e` | 11 passed, 37 s |
| `make test-signin-e2e` | 7 passed, 9 s |
| `make test-event-outage` (real ten-minute consumer absence, R7b) | PASS, 610 s |
| R7a invalid-bearer burst against the built `bin/api` (`-tags integration,trafficreview`, binary pinned by its SHA-256) | PASS: 40 requests, 20 x 401, 20 x 429, 20 denied audits |

Not run, and so not claimed: the reference-scale performance gates of the remediation line (build tag `performance`, seven files), the `driverpanic`
diagnostic, `TestM8RedisACLStreamLossAndPendingRecovery` (coupled to the main Compose project, skipped on isolated stacks as it was on the milestone
line), the opt-in checks of retained legacy API artifacts, the individual `t.Skip` sites (the run was not repeated with `-v` to count them), and any
independent review of the merge (brief: `/home/user/briefs/54-rev-integration.md`). The merged integration suite takes about 32 minutes here
(the milestone line alone took about 29), so the new deadlines (Makefile 90 minutes, CI job 120) leave room for a slower runner.

## For the owner and for the remediation agent

* **R6 is delivered by M11 to M14 on this branch** (requirements, executable acceptance, risk, approval and promotion), except the production
  data plane and a separate verifier credential. The remediation agent should hold R6 so that it is not built twice.
* **Reserved numbers.** The remediation line must not use migrations `00080` to `00089` or ADRs `100` to `104`. It keeps `00058` to `00079` and
  `064` to `099`. New remediation commits come in with a merge of `origin/remediation/m9r-foundation` into this branch.
* **The two contract files are stale for this branch.** `AGENTS.md` and `CLAUDE.md` (which this work may not edit) still say that M10 is
  not to be continued before the remediation gate and know nothing of ADR100 to ADR104. Whether this branch becomes the working line is the
  owner's decision.
* **The development database `MAIN` has to be rebuilt, not migrated.** It holds the milestone line's old numbers (`00045` to `00053`, `00060`),
  which collide with the remediation migrations of the same version. Nothing here touched it.
* Not done and not claimed: M15 to M19, the production data plane, a separate verifier credential, release and AI UI, a real model provider, the
  independent review of M14 (its brief exists), and the open residuals of the gap analysis.

## History: the analysis written before the merge (2026-10-19)

Status when written: **analysis only, 2026-10-19. Nothing was merged or renumbered.** The facts below come from executed trial
merges in scratch worktrees that were never pushed (the numbers were refreshed once, after the remediation line
completed R7b). The recommendation needs a decision from the repository owner, because the remediation line is being
developed by another agent. That agent's own report still lists M10 to M14 as future work ("M10 remains paused"): it has
no knowledge of the milestone line, so both agents need the same instruction.

| Line | Branch | Head at analysis time | Commits since the common base `e6305df` | Files changed since the base |
|---|---|---|---|---|
| Milestone line (M10 to M14, reviews, fixes) | `claude/m14-ai-builder` (also `claude/dazzling-goldberg-69wbkn`) | `7f2b29b` | 189 | 253 |
| Remediation line (R3c, R3d, R5c UI, R5d to R5g, R7a, R7b) | `origin/remediation/m9r-foundation` | `b00da36` | 116 | 264 |

The two lines diverged after migration `00044`. 33 files are changed by both (docs aside).

## What the trial merge showed (executed)

`git merge origin/remediation/m9r-foundation` into the milestone head:

1. **23 files conflict textually** (37 hunks; the first trial, before R7b, showed 21 files and 38 hunks; R7b added
   `internal/platform/postgres/transaction.go` and `tests/integration/m8_delivery_test.go`): `.github/workflows/ci.yml`, `Makefile`,
   `apps/web/src/preview/Workspace.test.tsx`, `apps/web/src/preview/contracts.ts`,
   `contracts/jsonschema/application-v1.3.schema.json` (added on both lines), `contracts/jsonschema/embed.go`,
   `docs/migration/M9R_FINDING_STATUS.md`, `internal/compiler/{README.md,compiler.go,rules.go,workflow_plans.go}`
   (compiler.go 4 hunks), `internal/config/config.go`, `internal/metadata/{README.md,models.go,validation.go}`
   (validation.go 4 hunks), `internal/platform/access/{access.go,metadata.go,workflows.go}`,
   `internal/platform/app/app.go`, `internal/platform/postgres/transaction.go`, `internal/runtime/workflow/{contracts.go,plan.go}`,
   `tests/integration/m8_delivery_test.go`.
2. **Nine migration versions exist on both lines with different content: `00045` to `00053`.** The merged tree does not
   start: `goose.NewProvider` over the merged `db/migrations` fails with `found duplicate migration version 45:
   existing 00045_m10_tool_ledger.sql, current 00045_m9r_workflow_approval_roles.sql` (run in the scratch tree, then
   removed). The remediation line also has `00054` to `00057` (R7b: event outage budgets, live event authority, isolation
   cutover); the milestone line also has `00060`. Renumbered milestone migrations therefore have to start above `00057`.
3. **ADR numbers `036` to `040` exist on both lines** (milestone line: M10 to M14; remediation line: workflow approval
   roles, field visibility, administration UI, OIDC sign-in, text and date projections). The remediation line continues
   to `063`.
4. **Two incompatible definitions of the published authoring contract `urn:framework:metadata:1.3.0`.** The milestone
   line makes `activity` a `oneOf` that admits tool activities (M10); the remediation line adds
   `state.approval_roles` (R5e). Both are "version 1.3.0" and both are described as immutable once published.

## Hazards that compile or merge cleanly and must be checked after a merge (not yet executed)

* `internal/policy/definition.go` and `store.go` are edited by both lines and merged without markers. The action
  catalog hash and `policy.Version` have to be re-derived; every stored candidate becomes `POLICY_CHANGED` after a
  bump, as after each earlier bump.
* The derived worker guard `control.worker_grants_safe()` (migration `00052` of the milestone line) refuses any
  relation of `control`, `runtime`, `iam` or `audit` that is not on its allowlist and carries a worker grant. The new
  relations of the remediation line (`runtime.scalar_indexes`, `search_indexes`, `unique_constraints`, and others)
  appear to grant the worker nothing and add `worker_no_generic` deny policies, so the guard should hold; running
  every worker-claiming test is the proof.
* R7b changes event delivery and worker authority (migrations `00055` to `00057`, ADR062 and ADR063): event-only READ
  COMMITTED authority, direct restrictive event RLS, and a cutover that refuses active old transactions until they
  drain. The milestone line's worker paths (M10 tool ledger and approval derivation, the M11 fences, the derived guard
  `control.worker_grants_safe()`) have to be re-run against it; the M13 and M14 API paths stay REPEATABLE READ, which
  R7b keeps for API and workflow callbacks.
* `internal/platform/app/app.go` wiring, readiness lists, `internal/platform/access/roles.go` and
  `internal/platform/postgres/transaction.go` are changed by both lines.
* Tests that count migrations or compare policy versions.
* The development database `MAIN` is migrated with the milestone line's numbers (`migrate status`, read only, shows
  `00045` to `00053` and `00060` applied). After a renumbering it has to be rebuilt, not migrated.

## Work done twice, and work about to be (compared at `7f2b29b` and `b00da36`)

The lines mostly built different things. Five places solve the same problem twice, in different ways; three more would
become duplicates if the agents are not told. This comparison read the shared files, the migrations and the commit
subjects; it did not read every commit of the remediation line.

| # | Problem | Milestone line | Remediation line | At the merge |
|---|---|---|---|---|
| 1 | Worker grant guard `control.worker_grants_safe()` | derived from the catalogs, so a new relation is refused by default (migration 52) | a fixed list of relation names, redefined again in migrations 47, 51 and 52 | the migration that runs last wins; the derived guard has to run last, and every new relation of the remediation line must be shown to pass it |
| 2 | A READ COMMITTED runtime transaction | `WithRuntimeIsolation`, for the sandbox freeze | `WithEventRuntime`, `withRuntime` and `RuntimeSnapshot`, for events, in the same file | textual conflict in `transaction.go`; keep one wrapper and express the other on it |
| 3 | The authoring schema file `application-v1.3.schema.json` | adds tool activities | adds approval roles (and the line also has `application-v1.4.schema.json`) | one version token with two meanings; one side moves to a new version |
| 4 | Integration test and CI timeouts (`Makefile`, `ci.yml`) | `-timeout 40m`, job 50 minutes | `-timeout=20m`, job 35 minutes | both values are too low for the merged suite (the milestone suite alone takes about 29 minutes) |
| 5 | A status ledger for the foundation findings | `docs/verification/FOUNDATION_GAP_ANALYSIS.md` | `docs/migration/M9R_FINDING_STATUS.md` | keep the remediation ledger as the one source; the other becomes history |

Heading for duplication:

* **R6 of the remediation plan is M11 to M14.** The plan itself assigns requirement and verification authority to the
  milestones M11, M12, M13 and M14; the milestone line implements it (requirements, executable acceptance, risk,
  approval and promotion) except the production data plane and a separate verifier credential. The remediation line
  still lists R6 as pending.
* **Approval freshness.** The remediation line lists "current-record approval freshness" for workflows as pending; the
  milestone line's `control.tool_begin` already demands approval evidence and a fresh record version before a tool send.
* **A separate verifier credential** is pending on both sides and started on neither.

Similar but complementary, not duplicates: request admission before authentication (R7a) and the bound on cold
recomputation (M12); workflow approval roles (R5e) and the check that a release approver still holds the authority
(M13R-03); retry budgets for event publication (R7b) and for tool attempts (M10).

## Options

**A. Replay the milestone line on top of the remediation line (recommended).** `CLAUDE.md` puts the remediation gate
before M10, the remediation line has more documents and evidence that cite its migration and ADR numbers, and the
milestone line has fewer. Steps once the remediation line is frozen at its gate:

1. Renumber the milestone migrations (`00045` to `00053`, `00060`) to follow the remediation line's last number, keep
   their order, and update every reference (`db/migrations`, tests that name a version, ADR039/ADR040, the M10 to M14
   reports, `internal/promotion/README.md`, `docs/migration/V2_V3_SUPERSESSION_MAP.md`).
2. Renumber ADR `036` to `040` after the remediation line's last ADR and fix the links.
3. Decide the metadata contract: either one `1.3.0` that carries both changes (acceptable only while neither is
   published outside the repository) or move the tool activities to `1.4.0` and extend the compiler's supported
   versions accordingly.
4. Resolve the 21 conflicting files by hand, with the compiler, metadata and workflow-plan files first.
5. Re-derive the policy catalog hash, rebuild the isolated and the `MAIN` databases from the merged tree and run the
   full gate (`make check`, `make test-unit`, the full integration suite, `make build`, `make test-e2e`; about 35
   minutes on this host).

**B. Merge the remediation line into the milestone line.** The renumbering then falls on the remediation line's
migrations and ADRs, which many of its reports cite. Not recommended.

**C. Keep the lines separate.** Not viable: M10 to M14 are a preview control plane that needs the foundation, and the
remediation line needs nothing from them, so the duplicated numbers only get more expensive with every commit.

## Decisions needed from the owner

1. Direction (A is recommended) and who executes it: the agent that owns the remediation line, or the milestone-line
   agent after the remediation line is frozen.
2. The freeze point of the remediation line (new migrations on either line after the freeze renumber again).
3. The metadata contract decision (step 3).

Cleanup note: the scratch worktrees of both trial merges were removed after the facts were recorded; the merges were aborted.
