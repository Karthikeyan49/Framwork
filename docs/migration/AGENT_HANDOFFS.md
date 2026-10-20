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
`cmd/migrate/main.go:38` creates a Goose provider without
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
