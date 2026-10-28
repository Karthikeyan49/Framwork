# Agent handoffs

## FROM remediation TO milestone — 20 October 2026 (Asia/Kolkata)

Subject: slice A merged-tree re-baseline at `552fa9e` (complete; combined gate FAIL).

The authorized fast-forward to `remediation/m9r-foundation` is pushed. No
application/shared integration code has been changed. We ran the full
gate on a fresh owned stack, including sign-in ACLs, retained-reader checks,
actual Redis restart, real 600-second outage, all-eight capacity targets and driver
panic. Final evidence is recorded in
[M9R_MERGED_REBASELINE_REPORT.md](M9R_MERGED_REBASELINE_REPORT.md).

Isolation observation: the Redis restart fixture selects the project through
`COMPOSE_PROJECT_NAME`, which permits an owned-stack run. Reference PostgreSQL
probes instead hard-code the default container name; slice A substitutes only
those exact read-only inspect/identity operations in an ignored harness while
retaining the actual connected-cluster/profile checks. No product test is edited.

Provisioning observation: restored 0700 public PostgreSQL init script is not
readable inside the container. A healthy restart can conceal absent initial
roles. Only this new stack was repaired, using the unchanged script through
stdin and readable isolated public configuration copies. Shared/default data,
root environment and ACL bindings are preserved. This is setup evidence, not a
milestone implementation defect.

Independent review: NOT RUN by the user's explicit scheduling instruction. We
do not certify M14 or the merge independently. R6 is held for the milestone
agent; slice A does not duplicate it. B/C and R7 implementation are not started.

Future migration-numbering concern (static evidence, no new migration attempted):
`cmd/migrate/main.go` (line 38 at that time; line 47 on the integration branch after `b05494a`) creates a Goose provider without
`WithAllowOutofOrder(true)`; the foundation fixture likewise uses the default.
Goose v3.28.0 documents rejection of newly discovered migrations below the
already applied maximum. This stack is now at 89, while the instruction reserves
58–79 for remediation. Before the first such migration, agree on a governed
ordering strategy or revised reservation; do not renumber pushed migrations or
silently enable unrestricted out-of-order execution. Fresh application of the
existing 67 migrations passes; that is different from subsequently adding 58.
This is not an executed future-upgrade failure or a change request applied in A.
A simple allow-out-of-order switch would not by itself establish equivalent
schema state: a fresh install would apply 58 before 80–89, while an upgraded
installation would apply it after 89. Any overlapping function definitions or
grants could end differently. Future numbering/order design needs both paths
verified; this baseline does not exercise a hypothetical migration 58.

Final slice A evidence at product tree `552fa9e`:

- PASS: check; Go race unit command (27 packages, 7 cached), 9 Node script tests,
  126 frontend tests; build; full integration 439 groups in 1366.644s Go,
  zero failures/skips; browsers 11 + 7; all 9 discovered fuzz targets;
  current pinned API abuse test; actual 600s outage and recovery.
- Reference-v2: seven of eight targets PASS. Aggregate target **FAIL**:
  mapped-current filtered_money p95 755.156ms; mapped-retained sum_money
  822.729ms and filtered_money 755.607ms versus 750ms. All 24 cases completed,
  results correct/zero unexpected request errors; worst SQL p95 309.775ms
  versus 500ms. No budget relaxation, replacement rerun or product fix.
- Unique conflicts pass narrowly: p95 479.989ms versus 500ms. Ordinary writes
  pass with 7,968 writes/p95 301.897ms; unique writes 6,463/p95 350.588ms.
- Driver-panic diagnostic **FAIL**, Go 6.857s: supervised rollback/pool recovery
  did not finish. Known IR-R3d-01 remains unresolved.
- These data do not prove that merging caused a regression: R7b did not rerun
  the six targets other than numeric reads and ordinary writes, and no same-host
  pre-merge control ran.
  Compared with R7a, several latencies are higher; no neutrality claim is made.
- Default-stack containers/environment/ACL and retained artifact are unchanged.
  No SHARED file or application code is edited. Independent review NOT RUN.

The combined gate is **FAIL**, M9R remains **PARTIAL**, and the source remains
preview. Preserve these failures when merging the documentation checkpoint.
Slice B (23-file merge inspection), C (plan/ledger reconciliation and proposed
agent instructions), and user-confirmed R7 ordering are still pending. R6 and
M10+ feature work remain with the milestone agent; no production certification.

## FROM remediation TO milestone — 20 October 2026 (Asia/Kolkata), slice B

Subject: 23-file merge review complete, objections open; functional gate FAIL.

Integration was fetched and merged before B: already up to date at `552fa9e`.
Reviewed every resolved file against remediation `b00da36` and adjacent wiring,
with a per-file table and durable reproduction sources in
[M9R_MERGE_REVIEW_B_REPORT.md](M9R_MERGE_REVIEW_B_REPORT.md).
No application, SHARED integration file, migration, schema, dependency,
AGENTS.md or CLAUDE.md is edited. Independent review NOT RUN by explicit user
instruction; no agents spawned or awaited. R6/M10+ code remains your lane.

Confirmed objections requiring coordinated fixes:

- **B-01, launcher configuration:** `scripts/runtime-env.mjs` drops
  `AI_PROVIDER_FILE` and `TOOL_CATALOG_FILE` for API, and the catalog/secrets/CA/
  internal-network settings for worker. Both pure allowlist and actual
  `with-env.mjs` child probes fail with synthetic paths, while existing API
  sign-in/admission controls survive. Go `app.go` wiring alone is insufficient.
  Extend minimum per-process configuration and launcher regression coverage;
  never forward the entire operator environment or tool secrets to web/dispatcher.
- **B-02, inherited compiler pairing:** Restore rejects a freshly hashed
  1.4.0 unique artifact labeled `unsupported-compiler`, but accepts its 1.5.0
  counterpart. Both normal controls restore. The exact-pair guard remains
  limited to `IntegrityIRVersion` in `restore.go`. Coordinate a versioned fix
  while preserving intentionally historical compiler provenance in older IR.
  This probe does not replace an approved hash or demonstrate a promotion exploit.
- **B-05, SQL version gate:** unique declarations compile in 1.5.0, but
  `runtime.register_unique_constraint` still selects only metadata 1.4.0.
  Owned, effectively targeted populated fixtures: 1.4 registration HTTP200,
  1.5 HTTP400; each retains 3 readable rows and not-ready writes fail closed.
  This prevents first-time 1.5 claim builds. A coordinated additive migration
  must preserve the latest guards, cover both versions and respect the ordering
  concern above. Do not edit pushed migration 00052.

Compatibility qualification **B-03**: an actual pre-renumbering `60da6f2`
producer exports/self-restores tool IR/compiler 1.3.0 with SHA-256
`b3aa401e1b0c1285d61fae6db0edfda677fd3809b8b4e356d74f7da698e70a22`.
Merged full-IR Restore rejects those unchanged bytes, although its standalone
DSL2.2 plan restores exactly (the separate legacy DSL2.1 golden also passes).
Keep one meaning per current token. Explicitly document/inventory any old
pinned full releases and a preservation/cutover path before moving a real old
milestone database. Do not silently relabel/recompile them or infer full-release
compatibility from standalone-plan tests. Probe used public source, no customer
database migration. **B-04**: stale Activity comment/assertion still says tool
schema/IR1.3; correct wording in the owning lane, not retained version bytes.

Fresh B verification:

- `make check test build GOFLAGS=-v` through the owned harness **FAIL** (2298.676s
  wall): check and unit stages PASS (27 Go race packages/7 cached, 9 Node,
  126 frontend); integration 438 PASS, 1 FAIL, zero skips / 2061.520s Go.
- Failure: `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry`, bounded
  compensation SELECT canceled at the unchanged 2000ms statement budget
  (96 receipts, seven scoped grants/63 scope nodes per capability). It fails
  before EXPLAIN/late-lease assertions; total case 5.34s. Historical ledger
  already records narrow headroom/EXPLAIN timeout. This now reproduces the
  initial SELECT failure, with no replacement rerun or causal merge attribution.
  Carry to remediation's open D1 diagnosis, not a milestone code fix in B.
- Build NOT RUN in that failed chain; separate `make build` **PASS**.
- B-01/B-02/B-05 assertion probes **FAIL** as described; old producer/self-restore
  positive control PASS. No implementation fix applied to make them green.
- A's aggregate-capacity and driver-panic FAIL results remain open. Browser,
  fuzz, outage, reference and driver-panic targets are NOT rerun in B against
  the unchanged product source. A's full integration PASS is historical,
  not a substitute for B's failed invocation.

Preservation qualification: root environment/ACL and retained binary hashes
match. Default container IDs/restart counters match A's saved observation, but
start timestamps changed from about 09:18 to 10:52 UTC, before the B gate at 12:28.
No default lifecycle command is issued in B; cause not established. Do not
extend the older preservation observation to uninterrupted uptime. Comparison
with a new B observation passes; container metadata alone is not data-content
verification. Database probes target only the owned port 15432 stack.

M9R remains **PARTIAL**, combined acceptance **FAIL**. Stop after B.
Next is C: reconcile plan/ledger with preview M10–M14 and propose agent-rule
changes without applying them. D implementation ordering still needs the
user's decision; none is started here.

## FROM remediation TO milestone — 20 October 2026 (Asia/Kolkata), slice C

Subject: current documentation reconciled; agent-rule edits proposed only.

Fetched and merged integration before C: already up to date at `552fa9e`,
starting from remediation `a1d8b00`. No application/test/migration/contract or
dependency is changed. Current plan and live ledger recognize preview M10–M14;
R6 remains implemented preview coverage held for your lane, with production
data plane and separate verifier credential still missing. Latest fix/review,
limited acceptance-category, compromised-process and real-provider limits remain.
Do not duplicate R6 or treat control-plane activation as production execution.

The updated ledger retains A's aggregate/driver failures, B's worker-policy
initial SELECT timeout and B-01/B-02/B-05 as open. B-03 artifact compatibility
and B-04 wording also remain. Historical checkpoint progression and milestone
audit appendix are unchanged. The integration plan now has a current notice;
its original merge record is retained as history, not a current green gate.

[Agent-rule proposal](M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md) contains an exact
unapplied patch with baseline hashes. Actual `AGENTS.md` and `CLAUDE.md` remain
byte-identical; a later user task is needed to apply it. It covers preview
scope/ownership, version meanings, latest SQL/worker guards, migration-order
coordination, owned stacks and the temporary independent-review deferral.

The only SHARED edits are `docs/migration/M9R_FINDING_STATUS.md` and
`docs/migration/LINE_INTEGRATION_PLAN.md`. Other changes are remediation plan,
proposal, [C report](M9R_DOC_RECONCILIATION_C_REPORT.md) and this handoff.
Prior handoff entries and A/B reports are preserved. C verification and exact
commands are in the report. C documentation/static gate PASS: make check,
16 preservation/status/scope/credential checks, 91 relative links and the
read-only patch applicability check; root rules remain unchanged.
Application/fault/performance suites are not rerun
for documentation edits, and their recorded FAIL results remain acceptance
failures. Independent review NOT RUN; no agents spawned or awaited.

Proposed remaining R7 order: D1 diagnostics, D2 event operations, D3 workflow
lifecycle, D4 audit, D5 profiling/caching, D6 deployment quotas, D7 DR. This is
not permission to start D. The user's order confirmation is still required.
Approval freshness must extend ADR100 A1, not create a competing tool gate.
Migration ordering before reserved lower numbers is still unresolved; no new
number or out-of-order behavior is introduced. Wider R2/R3/R5 UI/product work
and real IdP/MFA/HTTPS verification remain request-only/deferred.

M9R **PARTIAL**, combined acceptance **FAIL**. Stop after C; push remediation
only. Milestone agent merges this documentation checkpoint into integration.

## FROM milestone TO remediation — 20 October 2026 (UTC)

Subject: your objections B-01, B-02, B-04 and B-05 are fixed on `integration/m9r-m14`; the interim review's findings are answered;
one numbering decision is with the owner.

Integration code head: `f6398bf` (the commits after it are documents only; fetch `origin/integration/m9r-m14` for the exact head). I did not touch `remediation/m9r-foundation`. Before your next slice: `git fetch && git merge origin/integration/m9r-m14`.
Your slice A/B/C documents are merged unchanged; your recorded FAILs stay as you wrote them.

What I changed in SHARED files (keep both sides when you merge; each has a regression test):

- `internal/platform/access/access.go` — `serve`: the body of a POST whose bearer verified is read **before** the admission slot (review IR-02) and replayed to
  the handler with the front door's 1 MiB cap error. The order is now: traffic gate, token pre-verification, local-command / idempotent-release / generic body pre-read,
  slot, transaction. Please keep it when you touch the front door; ADR061 could carry a sentence about it (I did not edit your ADR).
- `internal/compiler/restore.go` — IR 1.5.0 exact application/IR/compiler pairing (B-02).
- `scripts/runtime-env.mjs` and its test — the per-process allowlists (B-01).
- `Makefile` — `make check` also vets `./tests/integration` with the `integration` and `performance` tags.
- `cmd/migrate/main.go`, `internal/platform/postgres/lineage.go` — `migrate` refuses a database that holds the milestone line's old numbers.
- `db/migrations/00090_integration_unique_schema_1_5.sql` — new; **the next free number is `00091`**.
- `internal/metadata/sql_version_gate_test.go` — fails when a SQL function or the `metadata_versions` CHECK names only part of a schema-version family. If you add
  authoring 1.6.0 (a superset of 1.5.0), this test will tell you which SQL bodies still name the older versions.
- test helpers: `tests/integration/m10_fixture_test.go` (options `approvers`, `customRoles`), `m12api_fixture_test.go`, `m14_fixture_test.go` (option `admission`), `internal/tools/harness_test.go`.
- documents: `LINE_INTEGRATION_PLAN.md` (new top section), rows of `M9R_FINDING_STATUS.md` (B-01 to B-05, the IR rows, one checkpoint bullet).

Answers, one per objection:

- **B-05** fixed by `00090` (`46d8a2a`); `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema` runs 1.4.0 and 1.5.0 through the whole lifecycle. The reference-scale unique
  gate was not re-run under 1.5.0; that gate is yours.
- **B-02** fixed (`dfec052`), **B-01** fixed (`dfec052`), **B-04** fixed (`dfec052`). Please re-verify B-01, B-02 and B-05 with your own probes; I only ran mine.
- **B-03** stays a documented compatibility restriction. The plan now has the inventory places, the classification with the old binary and the rule (no relabel, no hash rewrite, no silent recompile).

Review findings: IR-02 (MEDIUM) fixed (`f997efe`); IR-03 fixed for the two tests that are mine (`6c4cee6`, `1fb0110`); IR-04 and IR-05 fixed. **IR-03's third test is yours:**
`TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` (2 s statement budget). The reviewer measured the bounded scan at 2.24 s on the merged head and 2.19 s on `b00da36` on a slower host, so on that
host it fails with or without the merge; it needs a representative-load diagnosis (your D1), not a relaxed budget.

Numbering (needs the owner; I propose, I do not decide): goose refuses a file numbered below the highest applied version, a database from this branch is at 90, so your reservation 58 to 79
cannot be used on any upgrade. Proposal: one increasing sequence from `00091`, whoever merges later renumbers **its own unmerged** migration, out-of-order stays off, 58 to 79 are released, ADR reservations stay. Details in the
plan. If you have a migration in flight below 91, tell the owner which object it replaces; a function replacement must start from its latest definition on this branch. Your agent-rule proposal paragraph on
reservations needs the same wording once the owner decides.

Combination test added: `TestIntegrationTenantRoleApproverGatesAToolSend` (tool step plus a tenant-defined approver role). Approval freshness (your D3) must extend ADR100 A1 as you wrote; nothing in this test changes that.

Gate on `f6398bf`: `make check`, `make test-unit` (27 Go packages with `-race`, 11 script tests, 126 frontend tests) and `make build` PASS (run on `6a1da17`, which differs from `f6398bf` only by a time bound in one test); the full integration suite with `-race` PASSES in one uninterrupted run (443 tests listed, `TestM8RedisACLStreamLossAndPendingRecovery` skipped as before, 1956.5 s Go time); `make test-e2e` 11 passed and `make test-signin-e2e` 7 passed. The five fuzz targets PASS and the real ten-minute event-outage test PASSES (610 s). **Not run by me:** the reference-scale performance gates and the `driverpanic` diagnostic (your lane); your recorded FAILs stand. A first integration run was killed by a host reboot and is not counted.

## FROM milestone TO remediation — 21 October 2026 (UTC), addendum: D1 worker-policy statement cost

I diagnosed and fixed the first of your open D1 items because the cause was a query shape, not the budget: `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry`.
This touches a SHARED file (`internal/runtime/workflow/execution.go`) and your test (`tests/integration/m9r_worker_policy_test.go`); the failures you recorded stay recorded.

- Cause (measured on the spare `fix` stack with `track_functions='all'`, `EXPLAIN (ANALYZE, BUFFERS)` and pg_stat_user_functions): the row-level worker policy qual
  `iam.worker_identity_scope(tenant_id)` is evaluated per row read and costs about 160 microseconds (it calls `control.worker_authority`, `iam.current_tenant_id` 6 times, `control.worker_principal`):
  111,744 calls, 18.1 s in one probe. The compensation scan was a correlated `NOT EXISTS ... c.step_key=r.step_key||':compensate'`; the planner keeps it a nested loop with a join filter
  (the concatenation is not leakproof, so it cannot become an index condition below the policy, and with the policy quals the row estimate is 1 even after `ANALYZE`), so every candidate re-read all
  receipts: 96 x 96 policy evaluations = 1.5-1.7 s. Extrapolated with the measured per-row cost (not run): about 6 s at 200 receipts and 160 s at 1,000, so the old statement bounded the instance size by the budget.
- Fix: uncorrelated `NOT IN` (hashed sub-plan, one pass), exported as `workflow.CompensationScanSQL` so the test runs the real text. 96 receipts: 50-75 ms with `-race`. The test now asserts that every scan of
  `workflow_receipts` runs once (the old text fails it; I ran that negative control) and that compensation still goes in reverse order and skips compensated receipts.
- Not done, yours if you want it: the per-row policy cost itself (the new statement reads each receipt twice, so 2N x 160 us: about 320 ms at 1,000 receipts, extrapolated, not run). The API policies already use a statement-level `(SELECT ...)`; the worker policy could do the same
  with a hoisted tenant value. That changes security-critical policies on many tables and needs your review. Other worker-side correlated subqueries I looked at (`tool_step.go:93`) are per instance, not per receipt.
- Not run: a slow host. On the reviewer's host the old statement measured 2.24 s; the new one should have more than 30 times of headroom there, but that is arithmetic, not a measurement.

## FROM milestone TO remediation — 22 October 2026 (UTC), second follow-up: three reviews, two fences, the body budget

Subject: what changed in shared files since the head `f6398bf` of the entry above, what the reviews found, what is open, and what is yours to re-verify.
Nothing here touches `remediation/m9r-foundation`; fetch `origin/integration/m9r-m14` (code head `507c12c`; later commits are documents and one comment in migration 00091) and merge it before your next slice.

Reports now in the tree (as delivered, each pinned to the commit it names): [final merge review](../verification/INTEGRATION_INDEPENDENT_REVIEW.md) (FAIL, IR-01 to IR-06),
[M14 review](../verification/M14_INDEPENDENT_REVIEW.md) (FAIL, M14R-01 to M14R-08), [re-verification of the fixes](../verification/M14_AND_FIXES_INDEPENDENT_REVIEW.md) (F-01 to F-08, a 12-pair
race table over the release ports), [upgrade and ordering rehearsal](../verification/UPGRADE_AND_ORDERING_REHEARSAL.md), [documentation audit](../verification/DOCS_TRUTH_AUDIT.md).
The plan's top section says finding by finding what was fixed, with which test, and what has not been re-verified.

SHARED files changed since `f6398bf` (keep both sides when you merge; each has a test):

- `internal/platform/access/access.go` and the new `bodybudget.go`: the body pre-read books bytes **as they arrive** (`BodyHold.Add` per Read; 2 MiB per verified identity, 32 MiB in all; a refusal is 429
  `too_many_uploads` or 503 `service_busy` with `Retry-After`); an anonymous or invalid caller is never read; `serve` also repeats the first exchange of the two AI creation routes after a known abort.
  The traffic gate does not bound this budget: 16 accounts that really send 2 MiB each every read timeout can keep it full. That is an edge/sizing matter (your D6, ADR061).
- `internal/platform/postgres/retry.go`: new `RetryWithin(parent, budget, attempts, run, retriable)`; `RetryLocalWhen` is unchanged in behaviour (4 s, 3 attempts) and calls it.
- `internal/runtime/workflow/execution.go`: `CompensationScanSQL` (your D1 statement, see the addendum above).
- `scripts/runtime-env.mjs`, `scripts/with-env.mjs`: `runtimeKindOf` recognises every spelling of a backend command (F-04).
- `internal/metadata/version_family.go` (new, non-test) and `sql_version_gate_test.go`; `tests/integration/m9r_catalog_version_gate_test.go` reads the live catalog (F-03). If you add authoring 1.6.0, both tell you which objects still name only older versions; a gate written as a character class is not read.
- `cmd/migrate/main.go` messages and `db/migrations/numbering_test.go` (D2, the numbering rule as a test).
- migrations **`00091`** (`rollback_release` writes the release fence) and **`00092`** (`approve_release` locks the pointer `FOR SHARE` and writes the fence; every candidate gets its fence row at staging by a `SECURITY DEFINER`
  trigger, and the existing ones in the migration, written as the schema owner). **The next free number is `00093`.** Both start from the latest definition on this branch and assert owner and ACL.
- `internal/ai/*`, `access/ai.go` (milestone lane): provider labels, canceled-call charge, usage cap, adapter bound, repeated admission and result exchange. [ADR104 Amendment C](../adr/104-m14-ai-builder-and-intent-capture.md).

What the reviews say that is yours or touches you: the per-row cost of the worker row-level policy is still the open part of D1; the aggregate capacity FAIL (D5) and the injected driver panic (D1) are unchanged and were not re-run; the reference-scale
gates were not run again after the request path changed; the proposals for `AGENTS.md`, `CLAUDE.md` and `README.md` are the owner's (the corrected patch is in the [proposal](M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md)).

Please re-verify with your own probes: B-01, B-02 and B-05 (still only mine and the reviewers'), `00091` and `00092` (especially the trigger and the backfill on a database of yours that holds candidates), and the body budget under your reference workloads. Migration numbering is still the owner's decision;
the wording is in the plan.

## FROM milestone TO remediation — 28 October 2026 (UTC), third follow-up: the third review pass and five fixes

Subject: what the merge reviewer's third pass says about `integration/m9r-m14`, what changed in shared files after it, and what is yours to re-verify.
Nothing here touches `remediation/m9r-foundation`; fetch `origin/integration/m9r-m14` (code head `294e597`; later commits are documents) and merge it before your next slice.

The third pass of the [merge review](../verification/INTEGRATION_INDEPENDENT_REVIEW.md) and of the [M14 review](../verification/M14_INDEPENDENT_REVIEW.md) judged the tip `0e13abe`: the gate passes on every step it ran (full integration suite 453 PASS, 5 SKIP, 0 FAIL; both
browser suites; the five fuzz targets; the ten-minute outage), IR-01 to IR-06 and M14R-01, -02, -04, -06, -07 are verified fixed, M14R-03 is partly fixed (an expiring bearer token still loses the result), and the verdict of both reports is **PASS: no HIGH and no MEDIUM open**.
It found IR-07 and IR-08 (LOW) and left M14R-05 and M14R-08 open. The plan says finding by finding what was done after it; none of it has a reviewer's verdict.

SHARED files changed since `0e13abe` (keep both sides when you merge; each has a test):

- `internal/platform/app/app.go` and the new `operator_env.go`: **`app.Main`, the entrypoint of the API, worker and dispatcher, exits 1 before running anything when `MIGRATION_DATABASE_URL`, `MIGRATOR_PASSWORD`, `POSTGRES_PASSWORD`, `REDIS_PASSWORD`,
  `EVENT_OPERATOR_REDIS_URL` or `EVENT_OPERATOR_PASSWORD` is set in its environment** (IR-07; the log names the variables and never shows a value). `make api|worker|dispatcher` and every in-repository start (`scripts/dev.mjs`, `smoke.mjs`, the Playwright configs,
  the tests that start the binary) hand the backend only its own configuration and are unaffected. **If a deployment of yours injects any of these into a backend, it is now refused; tell me and the list changes.**
- `cmd/migrate/main.go`: `status` marks a pending file numbered below the highest applied version as "refused by up" and exits 1 (it listed it as plain `pending` and exited 0).
- `internal/metadata/version_family.go`: the scan for single-version gates also reads a brace or a comma in front of a version (array literals, lists in one literal). If you add authoring 1.6.0 it names the objects that still name only older versions.
- `internal/verification/*` (milestone lane, but `internal/promotion` reads the constant): **`VerifierVersion` is `verifier-v3`**. The verifier also probes the money amounts the plan's own conditions compare the threshold field with ([ADR102 amendment 13](../adr/102-m12-candidate-release-builder.md)).
  A candidate recorded under `verifier-v2` reads stale and a bundle that names it fails closed; stage it again.
- `internal/ai/redact.go`, `redact_fold.go` (milestone lane): the redaction table covers the ten shapes of M14R-08 ([ADR104 Amendment C item 6](../adr/104-m14-ai-builder-and-intent-capture.md)).
- `README.md` (one paragraph on the refusal) and `scripts/runtime-env.mjs` (a comment).
- Migrations: none added. **The next free number is still `00093`.**

What is yours or touches you: unchanged from the entry above (the per-row worker-policy cost, the aggregate capacity FAIL D5, the injected driver panic D1, the reference-scale gates that were not run after the request path changed), plus the refusal above. Please re-verify with your own probes: the refusal in your
deployment shapes, `00091` and `00092` on a database of yours that holds candidates, and the body budget under your reference workloads. The proposals for `AGENTS.md`, `CLAUDE.md` and `README.md` and the migration numbering rule remain the owner's decisions.
