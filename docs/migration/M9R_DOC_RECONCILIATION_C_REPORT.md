# Documentation reconciliation — remediation slice C

20 October 2026 (Asia/Kolkata). Starting checkpoint:
`a1d8b0025f58347b1185e4cbf76f12bfad0cd1e9`; unchanged product tree `552fa9e`.

**Slice C: COMPLETE. Documentation/static gate: PASS.
M9R STATUS: PARTIAL. Combined acceptance: FAIL.**
This report is not a code fix, independent review or production acceptance.
The user requires stopping after each bounded slice and confirming D ordering.

## What changed

- [Remediation plan](M9R_REMEDIATION_PLAN.md): recognizes existing preview
  M10–M14, distinguishes historical checkpoints from current gates, assigns
  R6 to the milestone lane and records its remaining production data plane
  and separate verifier credential. It preserves requirement-category,
  compromised-process and independent-review limits. D1–D7 are proposed R7
  subdivisions, not new milestones or authorization to start implementation.
- [Live ledger](M9R_FINDING_STATUS.md): updates 19 existing live rows and
  adds B-01–B-05. Current state retains aggregate, worker-policy and driver-panic
  failures. The historical checkpoint progression and entire milestone audit
  appendix retain their original evidence; they are not re-graded as merged-tree
  results. “M10 paused” and “no tool/AI implementation” are no longer live claims.
- [Integration plan](LINE_INTEGRATION_PLAN.md): adds a current status notice
  and labels the original merge record as historical. Its original body and
  number map remain unchanged. The notice distinguishes the original gate from
  later A/B failures and the current migration reservations/order concern.
- [Agent-rule proposal](M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md): provides an
  exact unapplied patch and baseline SHA-256 values for root AGENTS.md/CLAUDE.md.
  Both actual root files remain byte-identical. The proposal preserves security
  rules and addresses stale preview absence, ownership, version/SQL/worker
  guard coordination, branch restrictions and the user's review deferral.
- [Handoff](AGENT_HANDOFFS.md): appends the C result for the milestone agent;
  prior A/B entries remain unchanged. R6 and its code are not duplicated.

The SHARED files edited are **`docs/migration/M9R_FINDING_STATUS.md`** and
**`docs/migration/LINE_INTEGRATION_PLAN.md`**. No other SHARED file, application
source, test, migration, dependency, root agent rule or frozen source kit is
changed. No new ADR or architecture decision is introduced. No service is
started, restarted, migrated or reset for C; no customer data is accessed.

## Evidence retained, not rerun as fixes

| Evidence | Actual recorded result and implication |
|---|---|
| A full integration | 439 PASS / zero failures/skips; later B result still remains a failure |
| A full reference gate | Seven/eight targets PASS. Aggregate HTTP p95 FAIL in three cases, worst 822.729ms versus 750ms; SQL worst 309.775ms versus 500ms; all 24 exact-oracle cases complete with zero request errors |
| A driver-panic diagnostic | FAIL, 6.857s Go; ordinary callback recovery PASS does not clear injected driver/tracer recovery |
| B functional chain | FAIL: check/unit stages PASS; integration 438 PASS / 1 FAIL / zero skips, 2061.520s; initial worker compensation SELECT exceeds unchanged 2000ms; build not reached in chain, separate build PASS |
| B-01 | Launcher propagation probes FAIL; configured API/worker tool/provider options dropped |
| B-02 | Version probe FAIL: unknown compiler label rejected for 1.4, accepted for 1.5 after a new matching hash; no approved-hash bypass demonstrated |
| B-03 | Old producer/self-restore PASS; merged full old tool IR unsupported while standalone plan preserves bytes; explicit artifact inventory/cutover still needed |
| B-04 | Stale schema/IR wording remains; no implementation correction in C |
| B-05 | Populated API/SQL assertion FAIL: schema 1.4 registration 200, schema 1.5 registration 400, three records retained per fixture, unready writes 409 |
| Current independent review | NOT RUN for M14/merge under user's scheduling deferral; later milestone fixes retain their own review qualifications |

No causal merge-performance claim follows from these observations without a
controlled pre-merge comparison. No failed command is replaced by a historical
or focused pass. The older default-container start-time discrepancy qualified
in B remains in that report; C makes no uninterrupted-uptime/data-content claim.

## Verification for this documentation slice

The C gate is documentation/scope verification plus `make check`: no executable
source changes justify repeating the entire application/fault/performance suite
for this slice. The A/B failed acceptance remains authoritative. No database
operation is needed for these checks; the existing owned harness supplies the
same isolated environment for consistency.

| Exact command / observation | Result |
|---|---|
| `git fetch origin` | PASS |
| `git fetch origin refs/heads/integration/m9r-m14:refs/remotes/origin/integration/m9r-m14` | PASS |
| `git merge --no-edit origin/integration/m9r-m14` | PASS, already up to date |
| `node .local/merge-docs-c-20261020/verify.mjs` | PASS: 16 preservation/scope/status/link/credential checks; 91 relative links resolve |
| `git apply --check .local/merge-docs-c-20261020/proposed-agent-rules.diff` | PASS, read-only applicability check; patch not applied |
| `node .local/merged-rebaseline-20261020/run.mjs make check` | PASS: 84 architecture and 43 V3 checksums, format, Go vet, module verification, frontend lint and TypeScript |
| `git diff --check` | PASS |
| First `git diff --cached --check` | FAIL: seven blank context lines in the newly staged embedded diff contained a trailing space. No application code involved |
| Final `git diff --cached --check` | PASS after presentation correction |
| Unit/integration/build/browser/fuzz/outage/reference/driver-panic suites in C | NOT RUN; unchanged executable tree, preserve A/B actual results above |
| Independent agents/review in C | NOT RUN; user explicitly deferred; no agents spawned or awaited |

The ignored verification helper checks actual baseline bytes, not only status
words: root agent files unchanged; old ledger appendix and checkpoint progression
unchanged; original integration record unchanged; historical plan progression
unchanged; prior handoffs unchanged; exactly the six allowed Markdown files
changed; all relative Markdown links outside code fences resolve. It also checks
the current statuses, five B rows, D1–D7 proposal and proposal-only marker.
The read-only patch check tests whether the proposal can apply to its exact
baseline. It does not change the agent files. Ignored harness and evidence stay
under `.local/merge-docs-c-20261020/`; no credentials or generated builds belong
in this commit.

The staged whitespace check includes new files that the earlier unstaged
`git diff --check` did not inspect. Blank context-marker lines were normalized
in the Markdown proposal, then patch applicability was checked again and passed.
This is a concrete documentation correction; the earlier failed check is retained.

## Remaining decisions and stop

The proposed order is **D1 diagnostics → D2 event operations → D3 workflow
lifecycle → D4 audit → D5 profiling/caching → D6 deployment quotas → D7 DR**.
The user must confirm the order before D implementation. D1 explicitly includes
the unconfirmed workflow connection issue, B's worker-policy timeout and A's
injected driver-panic failure; it cannot close them with passing retries alone.

Agent-rule edits are proposed only and require a later explicit task to apply.
Migration ordering before new reserved lower versions also remains unresolved.
Coordinate B findings with the milestone owner; no fix or schema token is
allocated in C. Wider R2/R3/R5 product additions and real IdP/MFA/HTTPS rollout
remain request-only/deferred. Production R6 and M10+ features remain outside
this agent's lane. Stop after this slice and push only remediation.
