# Foundation gap analysis (validated review vs current code)

Audit date: 2026-10-17 (UTC). Audited tree: `dbc4b5b`, the head of
`claude/m12-candidate-builder` (M0 to M12), merged into the audit branch
`docs/finding-status`. Scope: every row of
[the finding ledger](../migration/M9R_FINDING_STATUS.md) (F01 to F31), every
remediation slice of [the plan](../migration/M9R_REMEDIATION_PLAN.md) (R0 to R7,
R2a to R2c, R3a to R3d, R5a to R5e) and the code added by M10 to M12.

## Progress

- [x] Source documents read: validated review, remediation plan, ledger, R1 to R5c
  reports, M10/M11 and M12 reports, supersession map, ADR024, ADR036 to ADR038, the five
  independent review and honesty documents now in the tree.
- [x] Ledger rows F01 to F31 classified with file and line evidence
  (`docs/migration/M9R_FINDING_STATUS.md`, commits `d457c04`, `20259a7`, `2d0cb77`).
- [x] Remediation slices R0 to R7 classified (same file).
- [x] Fake-assumption scan, executed probes, newly discovered gaps N-01 to N-15 (below).
- [x] Ranked order of work (last section).

Finding-status audit STATUS: COMPLETE. Every row is classified from code and tests.
Nothing that needs a database was executed (see "What could not be verified").

## Scope, baseline and method

Three code lines exist; they are not one tree:

| Line | Tip | Relation to the audited head |
|---|---|---|
| M0 to M12 (audited) | `dbc4b5b` | baseline for every verdict |
| `claude/m13-approval-promotion` | `436c2b5` | M12 plus migration 49, ADR039, promotion routes; read with `git show` only |
| `origin/remediation/m9r-foundation` | `06882b2` | diverged at `e6305df`; carries R5c UI, R5d, R5e, R5f; not in the audited tree; spot-checked with `git show` only |
| `claude/m10-fixes` | `cf02e33` | M12 plus fixes for M10R-01 to M10R-05 (migration 50, DSL 2.2.0); unmerged and unreviewed |

Method. For each ledger row the cited code, migration or test was located and read
(`file:line` in the ledger). The previous status text was not used as evidence. Then:

1. Integration tests were read for what they assert (not whether they pass).
2. Executed here, no database, `nice -n 10 ./scripts/go.sh test -count=1`: 17 unit
   packages PASS (`internal/platform/postgres`, `httpserver`, `policy`, `runtime/data`,
   `runtime/workflow`, `config`, `identity`, `audit`, `promotion`, `risk`, `intent`,
   `verification`, `metadata`, `tools`, `sandbox`, `events`, `tests/security`).
3. `node scripts/verify-docs.mjs` PASS (84 architecture and 43 V3 source checksums). Three temporary probes were run and deleted; none is committed (see the scan).
4. The fake-assumption scan searched for placeholder markers, constant returns, flags
   that gate behaviour, caller-declared authority fields, functions with no production
   caller, tests that pin a contract production does not use, and documents that
   overstate code. Repository search found no TODO/FIXME/stub markers worth reporting.

Verdict words: CLOSED, PARTIAL, OPEN, SUPERSEDED (as in the ledger). PASS / FAIL /
NOT RUN are used for executions. "Recorded PASS" is a PASS claimed by an
implementation report that this audit did not reproduce.

## Per-finding verdict summary

| ID | Finding | Verdict at the audited head | Driver or remaining work |
|---|---|---|---|
| F01 | Worker credential cross-tenant and record mutation | CLOSED (DB-credential threat model) | N-02, N-03 on the M10 surface |
| F02 | Roles own tables, broad worker grants | CLOSED | N-14 (denylist guard) |
| F03 | Tenant-controlled shared DDL | CLOSED | none |
| F04 | Records vanish across compiler/release change | PARTIAL | default routes keep hash-keyed namespace; no production dataset; no UI |
| F05 | RLS membership once per row | CLOSED | gate not in CI (N-09) |
| F06 | End-to-end query performance | PARTIAL | only registered numeric index meets the gate; N-09 |
| F07 | Shared dataset writer counter | CLOSED | gate not in CI (N-09) |
| F08 | Write retries / unknown COMMIT | CLOSED (local commands) | external effects: N-04 |
| F09 | Late rejection commits a record | CLOSED | none |
| F10 | Lost in-flight command key | CLOSED (memory-only by design) | frontend tests not run here |
| F11 | Custom indexes / uniqueness | PARTIAL | text/date/search and R3c uniqueness OPEN; N-15 |
| F12 | Aggregates in Go, one join | OPEN | R3d not started |
| F13 | Production runtime / public promotion lifecycle | OPEN at head; authority CLOSED on M13 branch (not merged) | N-05; production data plane absent everywhere |
| F14 | Caller-supplied risk / PASS / independence | PARTIAL | N-12, N-06; recompute fixed at head |
| F15 | DataScope ALL for built-in roles | PARTIAL; R5d, R5c UI on remediation line | N-01 |
| F16 | Fixed four roles | PARTIAL; R5e on remediation line | N-01 |
| F17 | JWT skew, token paste login | OPEN; R5f on remediation line | N-01 |
| F18 | M8 outage window, dead predecessor, one consumer | OPEN | R7 |
| F19 | Audit retention / partitioning | OPEN | R7 |
| F20 | M9 limits, copied IR | PARTIAL | no lease renewal; limits unchanged |
| F21 | Approval on start snapshot | PARTIAL | N-02 for the tool path |
| F22 | Rules have no consumer | CLOSED (claim was wrong) | none |
| F23 | External exactly-once, AI | PARTIAL (M10 built, not accepted) | N-04, N-11 |
| F24 | Rate limit, invalid-auth DB cost | OPEN | R7 |
| F25 | Per-request compile / catalog checks | OPEN (not profiled) | R7 |
| F26 | Two rulebooks, supersession map | PARTIAL | N-08 |
| F27 | Numeric gates, migration ownership | PARTIAL | N-09, N-01 |
| F28 | Portability | OPEN (M17) | nothing implemented |
| F29 | CI/deploy, SSO, search, DR | OPEN (scheduled) | runbooks are stubs |
| F30 | Review process | PARTIAL | M11 and M12 API reviews unfinished and unmerged |
| F31 | Stacked unmerged branches | OPEN | N-01: lineages diverged |

Totals over the 31 rows: CLOSED 9 (F01, F02, F03, F05, F07, F08, F09, F10, F22),
PARTIAL 12 (F04, F06, F11, F14, F15, F16, F20, F21, F23, F26, F27, F30), OPEN 10 (F12,
F13, F17, F18, F19, F24, F25, F28, F29, F31). Counting rule: a row is CLOSED only when
its stated acceptance is met at the audited head, within the limit named in the table;
rows closed only on an unmerged branch are not CLOSED.

### Remediation slices

| Slice | Verdict | Note |
|---|---|---|
| R0 | PARTIAL | map exists; agent contract and README stale (N-08) |
| R1 | CLOSED | recorded PASS; residual N-02, N-03, N-14 |
| R2a, R2b, R2c | CLOSED (preview) | R2 as a whole PARTIAL (F04) |
| R3a | CLOSED | recorded PASS; flaky current-host HTTP margin history |
| R3b numeric | CLOSED | other types OPEN |
| R3c, R3d | OPEN | not started |
| R4 | CLOSED (local commands) | external effects N-04 |
| R5a, R5b, R5c-a, R5c-b | CLOSED | recorded PASS |
| R5c UI, R5d, R5e, R5f | OPEN at the head | delivered on the remediation line only (N-01) |
| R6 | PARTIAL | M12 builder present; authority on M13 line; M14 not started |
| R7 | OPEN | external tool semantics built but defective (N-04) |

### Claims from the original validation that have no ledger row

| ID | Claim | Verdict | Evidence |
|---|---|---|---|
| V1 | Database hash integrity is not compiler admission (hash-consistent `{}` IR accepted by PostgreSQL) | PARTIAL | `runtime.workflow_bindings` only checks `sha256` equality (`00020:12-16`) and `framework_app` can insert it (`00020:24`); admission is Go `compiler.Restore` on capture and on the worker (`internal/platform/access/workflows.go:55-60`, `internal/runtime/workflow/store.go:49`). A forged binding is still rejected by the worker, not by the database |
| V2 | RequesterID filled by the builder | CLOSED | `Store.Stage` requires `RequesterID == actor` (`internal/promotion/store.go:50`); the port re-checks (`00048:122-128`) |
| V3 | Intent narrow, ambiguity passes as optional | PARTIAL | typed `requirements-v2` with a hashed requiredness floor (`internal/intent/requirements.go:26,496-520`); unresolved mandatory items block. Only four categories are verifiable, others BLOCK (M12 report) |
| V4 | Acceptance free text, no independence | PARTIAL | `acceptance-v2` is typed; author must differ from requester and source author in the stage port (`00048:93-145`). Independence is actor inequality, not proof of independent derivation (M12 report) |
| V5 | Import boundaries | CLOSED | `tests/security/boundaries_test.go`; executed here, PASS, `risk` and `verification` are registered |
| V6 | Immutable releases, workflow pins, atomic audit/outbox preserved | CLOSED | pins `internal/runtime/workflow/store.go:54,172`; bundle immutability triggers (`00011:47-48`, `00048:60-61`); audit immutable (`00002:91-92`); unit `internal/audit`, `internal/runtime/workflow` PASS here |
| V7 | Compiler migration plans say NONE where data changes | PARTIAL | real evolution operations exist for rename, integer/decimal conversion and backfill (`internal/compiler/storage.go`, `internal/runtime/data/evolution.go`); production use of them waits for M13 activation |

## Fake-assumption scan

A fake assumption is a place where a contract, flag, comment, test label or document
claims more than the code does. Each entry names the claim, what the code does, and how
it was established (READ = source read, EXEC = executed here, REVIEW = taken from an
independent review document in the tree and re-confirmed by reading the cited code).

| ID | Class | Claim | Reality | Basis | Gap |
|---|---|---|---|---|---|
| FA-1 | caller-declared authority | ADR036 section 7: a REQUIRED tool sends only "if the database finds" approval evidence and a fresh record version | `control.tool_begin` runs both checks only `IF needs_approval`, an argument supplied by the worker from its own catalog copy (`internal/runtime/workflow/tool_step.go:230`; `00046:125,148-153`). The pinned plan does not carry the requirement | READ + REVIEW (`M10_INDEPENDENT_REVIEW.md:52-78`, executed there) | N-02 |
| FA-2 | caller-declared state | ADR036 section 4: worker writes only through ports bound to lease and policy | `framework_worker` and `framework_app` hold `UPDATE(state_id,status,...)` on `runtime.workflow_instances` with no transition check or trigger (`00020:38`); `tool_finish` checks only the lease | READ + REVIEW | N-03 |
| FA-3 | test pins an unused contract | `internal/tools/delivery_test.go:12-28` tests `Delivery.MayRetry` as the retry rule | `MayRetry` has no production caller; `decideTool` re-implements the rules (`tool_step.go:82-126`) and disagrees | EXEC probe | N-11, N-04 |
| FA-4 | constant-true gate | `releases.go:43-45`: the gate "records that the API already holds an ALLOW decision" and "M6R's Gate interface is satisfied, not bypassed" | `AuthorizeRelease` returns `true` unconditionally (`releases.go:48`); `Store.evidence` hard-codes `Allow: true` and policy label `m6r-release-contract-v1` (`internal/promotion/store.go:40`) | READ | N-06 |
| FA-5 | comment vs behaviour | `00048:34-35`: every index column "is copied by the stage port from the verified body" | The database never verifies the body; it reads a jsonb view whose duplicate-key and case rules differ from Go's (`00048:78,96,121`; M12 DB review F2 reproduced rows `LOW/false` against a HIGH body) | READ + REVIEW | N-07 |
| FA-6 | document vs code | `AGENTS.md:3-6,62-65`: current work is M9R, do not begin M10, "only `local.noop` is registered"; plan and ledger say "M10 stays paused" | Tool activities run when `TOOL_CATALOG_FILE` is set (`internal/platform/app/app.go:154-165`); M10 to M12 are built; `README.md:14` still sends readers to the remediation plan "before M10" | READ | N-08 |
| FA-7 | comment vs behaviour | `internal/tools/delivery.go:1`: "No external tool executes in M6R"; `internal/risk/risk.go:5-8` and `facts.go`: inputs include a "measured record count" | the package now holds the M10 gateway; no record-count measurement exists (`promotion.DefaultVolume`, `internal/promotion/bundle2.go:269-276`) | READ | N-08 |
| FA-8 | flag defaults open | Preview routes and workflows are "development/test only" | The gate is `APP_ENV`, which defaults to `development` (`internal/config/config.go:45,50`; `internal/platform/app/app.go:245`). Executed: no `APP_ENV` gives `Environment="development"`, `WorkflowsEnabled=true`, dev issuer `http://localhost:8080/dev-issuer`; a typo such as `prod` is rejected | EXEC probe | N-10 |
| FA-9 | claim without enforcement | R3a to R4 and ADRs 025 to 029 state p95 gates (750 ms query, 500 ms write) | The tests carry build tag `performance` and are not run by `make test` or CI (`Makefile:37-40`, `.github/workflows/ci.yml`); `make release-check` is not in CI either | READ | N-09 |
| FA-10 | authority table without a writer | `runtime.unique_projections` is presented as the unique-field mechanism in the original review and plan | The table exists (`00009:52-58`) and nothing writes it; no metadata concept of uniqueness exists | READ | N-15 |
| FA-11 | contract without a caller | `promotion.Store.Approve/Activate/Rollback` are described as the release lifecycle | No caller outside tests at the head; the API role can still write the approval and pointer tables directly (`00011:22-38`) | READ | N-05, N-15 |
| FA-12 | test coverage label | ADR036 lists "policy revocation" among integration coverage; M10 and M11 add actions `tools.invoke`, `sandbox.read`, `sandbox.manage` | No test mentions `tools.invoke` or `sandbox.manage` (search of every `_test.go`). Executed matrix of 35 role/action cells for the new actions: 0 mismatches, so behaviour is right but unpinned | EXEC probe | N-13 |
| FA-13 | guard presented as boundary | `control.worker_grants_safe()` is cited as the runtime guard against broad worker grants | It is a fixed denylist run by the trusted worker process before each job (`internal/platform/jobs/jobs.go:46,129`); it ignores REFERENCES/TRIGGER and tables not listed (`00048:182`). The real boundary is the RESTRICTIVE policies and absent grants | READ + REVIEW (M12 DB review F5) | N-14 |
| FA-14 | verdict claim by the producer | `stage_candidate` "verifies PASS" | It checks that the body says `"PASS"` (`00048:122-128`); the verifier ran in the API process and the database cannot recompute it (documented in `M12_IMPLEMENTATION_REPORT.md` threat model); every Go restore does recompute it (`internal/promotion/bundle2.go:628-632`) | READ | N-12 |

Checked and found consistent (no finding):

- Preview-only routes described as production: none. Every data, dataset, evolution,
  sandbox, release-builder, index and workflow route is registered only inside
  `if a.PreviewEnabled && a.Registry != nil` (`internal/platform/access/access.go:64-108`).
  The routes outside that block are tenant administration and metadata authoring
  (`access.go:109-125`: roles, teams, members, session, access, metadata). Production
  mode also refuses workflows and events (`internal/config/config.go:57,67`) and tool
  internal-network opt-in (`config.go:93`).
- Functions never called (exported, no production caller): only `Store.Approve`,
  `Store.Activate`, `Store.Rollback` and `Delivery.MayRetry` of consequence (N-11,
  N-15). `ParseYAML` and the others reported by a mechanical scan are reached through
  other call sites or are trivial accessors.
- Skipped or silent tests: no `t.Skip`, `Skipf` or `testing.Short` in any test file.
  One `t.Log` records an expected baseline outcome (`m9r_performance_test.go:161`).
- Placeholder markers: no TODO/FIXME/stub in Go, SQL or TypeScript outside test data.
- Caller-declared fields elsewhere: `sandbox.Context` and bundle 1.0.0 remain
  caller-declared but cannot be staged (`internal/promotion/store.go:50`) and the API
  role cannot insert bundles (`00048:63-64`); release request bodies naming authority
  fields are rejected (`internal/platform/access/releases.go:196-203`, per the honesty
  audit, strict body decoding).

## Newly discovered gaps (ranked)

Severity follows the common brief: HIGH = exploitable violation of a security or
integrity invariant; MEDIUM = realistic integrity or availability defect or a
documented-contract breach; LOW = hardening or documentation. "Owner" is the milestone
or slice that should carry the work.

| Rank | ID | Severity | Gap | Owner |
|---|---|---|---|---|
| 1 | N-02 | HIGH | The approval requirement of a tool step is supplied by the worker | M10 follow-up, before any WRITE tool |
| 2 | N-04 | HIGH | An external write can be sent twice after an unknown outcome | M10 follow-up |
| 3 | N-01 | MEDIUM (gate blocker) | Two code lines collide on migrations, ADRs and version tokens | integration slice before the foundation gate |
| 4 | N-03 | MEDIUM | Workflow instance state is writable without transition checks | M10 follow-up (R1 residual) |
| 5 | N-05 | MEDIUM | The API role can approve and activate releases in SQL at the head | M13 |
| 6 | N-09 | MEDIUM | Performance gates, release check and restore drill are not enforced by CI | R7 / M15 |
| 7 | N-11 | MEDIUM | Unit tests pin a retry contract that production does not use | M10 follow-up |
| 8 | N-08 | MEDIUM | The agent contract and several documents are stale | R0 |
| 9 | N-12 | LOW | The verifier shares the trust domain of the stager; the database cannot check PASS | R6 / M13 |
| 10 | N-10 | LOW | A missing `APP_ENV` yields development mode | R7 |
| 11 | N-06 | LOW | Constant-true release gate and constant audit labels | M13 |
| 12 | N-07 | LOW | The stage index is documented as verified but is not | M12 follow-up |
| 13 | N-13 | LOW | New policy actions have no negative tests | M10 to M12 follow-up |
| 14 | N-14 | LOW | Worker grant guard is a fixed denylist | R1 hardening |
| 15 | N-15 | LOW | Dead or writer-less artifacts presented as mechanisms | R3c / M13 |

### N-02 (HIGH) tool approval gate is worker-declared

- Evidence. `tool_step.go:230` passes `def.Approval == "REQUIRED"` as the last argument
  of `control.tool_begin`; `00046:125` declares `needs_approval boolean`; `00046:148-153`
  performs the approval signal and record-version checks only when it is true. Plan
  states carry `tool_ref`, `version`, `inputs`, `tool_hash` and no approval field, so
  the database cannot derive it. The success path never compares the ledger's
  `request_hash` (`loadToolLedger`, `tool_step.go:72`, does not read it).
- Executed by the independent M10 review (`M10_INDEPENDENT_REVIEW.md:60-68`): raw SQL as
  `framework_worker` inside a lease, `tool_begin(...,false)` returned an attempt and
  `tool_finish` recorded a forged SUCCESS with zero approvals.
- Impact. The documented database guarantee (ADR036 section 7) is a worker convention;
  a compromised worker can create forged approval-required tool evidence. It cannot be
  stopped from calling the provider itself (it holds the network path and secrets).
- Repro (from the review, raw SQL as `framework_worker` inside the exact lease):
  `UPDATE runtime.workflow_instances SET state_id='<tool step>', status='READY'` (N-03),
  then `SELECT * FROM control.tool_begin('<tool step>', ref, ver, tool_hash,
  repeat('b',64), 'BLOCK', 'SEND', false)` returns an attempt; `control.tool_finish`
  then records SUCCESS.
- Fix status. `claude/m10-fixes` `9f0a807` (migration 50, plan pins `tool_contract`,
  DSL 2.2.0). Not merged, not independently reviewed.

### N-04 (HIGH) external write can be duplicated

- Evidence. `decideTool` returns `SEND` for any retryable FAILED ledger regardless of an
  earlier UNKNOWN send (`tool_step.go:89-96`), and `tool_begin` admits SEND from
  `FAILED AND retryable` (`00046:169`). The ledger keeps one state, so a failed lookup
  overwrites the fact that a send is unresolved.
- Executed here (temporary probe, deleted, no database): a LOOKUP_BY_REFERENCE WRITE
  tool with ledger `FAILED/retryable` after a lookup returned `decideTool = "SEND"`
  while `Delivery.MayRetry(Failed, false, false)` returned `false`. Executed by the
  independent review: provider saw two POSTs (`M10_INDEPENDENT_REVIEW.md:36-41`).
  Related defects M10R-03 (retry budget bypassed in RETRY_WAIT), M10R-04 (rejected
  lookup strands the effect in DEAD) and M10R-05 (404 trusted while the first request
  may still be in flight) are in the same review.
- Impact. Violates "reconcile unknown commit/external outcomes before repeating
  effects" (AGENTS.md) for tools with no native idempotency.
- Fix status. `claude/m10-fixes` `a3b0367`, `aa8fa33`, `88198b0`, `b4041d8`. Unmerged.

### N-01 (MEDIUM, gate blocker) the foundation exists as two incompatible lines

- Evidence (all by `git show`, nothing applied):
  - Migrations: the audited line has `00045_m10_tool_ledger.sql` and
    `00046_m11_sandbox.sql`; `origin/remediation/m9r-foundation` has
    `00045_m9r_workflow_approval_roles.sql` and `00046_m9r_field_patch_receipts.sql`.
    Both lineages also alter `metadata_versions_schema_version_check` to admit `1.3.0`.
    `claude/m13-approval-promotion` uses 49 and `claude/m10-fixes` uses 50.
  - ADR numbers: 036 to 039 are each used twice (M10/M11/M12/M13 against approval roles,
    field visibility, administration UI, OIDC sign-in).
  - Version tokens with different meaning: audited head `ToolWorkflowIRVersion`,
    `ToolWorkflowCompilerVersion`, `ToolWorkflowSchemaVersion` = `1.3.0`
    (`internal/compiler/compiler.go:26-27`, `internal/metadata/models.go:15`) and DSL
    `ToolDSLVersion` = `2.1.0` (`internal/runtime/workflow/plan.go:24`); remediation
    line `ApprovalIRVersion`, `ApprovalCompilerVersion`, `ApprovalSchemaVersion` = `1.3.0`
    and `TenantRoleVersion` = `2.1.0`. `claude/m10-fixes` introduces DSL `2.2.0`.
  - Policy decision versions: `m12-native-v9` (head), `m9r-native-v6` (remediation),
    `m13-native-v10` (M13).
  - The ledger and plan present `remediation/m9r-foundation` as the latest line
    (ledger header and row F31), which is no longer true for M10 and later work.
- Impact. Immutable artifacts are restored by version string; a stored IR, bundle or plan
  labelled `1.3.0` or `2.1.0` has two possible meanings, and applied migrations must not be
  edited, so merging requires a deliberate renumbering and new tokens before any shared
  database sees both. No single tree has passed the full suite since the split, so the
  foundation gate (R1 to R4 plus M0 to M9 regressions on the final commit) cannot be
  claimed for any commit.
- Suggested work. Choose one integration order, assign unused migration, ADR and version
  numbers, and run the full integration, browser and performance gates on the merged tree
  before anything else is stacked.

### N-03 (MEDIUM) workflow instance state is worker- and API-writable

- Evidence. `GRANT UPDATE(state_id,status,version,attempt,entered_at,due_at,termination,
  failure) ... TO framework_app,framework_worker` (`00020:38`); no trigger exists on
  `runtime.workflow_instances` in any migration. The row policy limits the worker to its
  leased job but validates no transition against the pinned plan.
- Executed by the independent M10 review: the worker set its instance to an approval-gated
  tool step (`rows=1`) and the normal worker then completed the instance with zero
  approvals (`M10_INDEPENDENT_REVIEW.md:60-66`). ADR024 accepts fabrication within a lease,
  but ADR036 section 7 claims otherwise for approvals.
- Fix status. `claude/m10-fixes` migration 50 (transition must follow an `always`
  transition of the pinned plan). Unmerged. The API role keeps the same grant.

### N-05 (MEDIUM) release approval and activation are open to the API role at the head

- Evidence. `release_approvals` INSERT policy checks tenant scope and, for HIGH risk,
  requester inequality (`00011:22-27`); `active_releases` allows INSERT and UPDATE of
  `artifact_hash,generation` when any approval row exists (`00011:35-38`). Nothing in
  M12 revokes them (`00048` only adds TRUNCATE guards). The M12 DB review reproduced a
  self-approval and activation of a non-canonical body, which then breaks
  `release-check` for every tenant (`M12_DB_INDEPENDENT_REVIEW.md:123-151`).
- Impact. Only reachable through `framework_app` SQL (no route approves at the head),
  but it contradicts ADR038 ("nothing in M12 approves or activates") and AGENTS.md
  ("approval binds the exact verified artifact activated").
- Fix status. Closed on `claude/m13-approval-promotion` (migration 49 drops the policies
  and privileges and adds definer ports); not merged.

### N-12 (LOW) the verifier shares the trust domain of the stager

- Evidence. `control.stage_candidate` accepts `Verification.Outcome == "PASS"` as a claim
  (`00048:122-128`); the verifier, risk and diff run in the API process
  (`internal/platform/access/releases.go:282-373`). The backstop is real: `restoreV2`
  re-runs the verifier over the stored IR bytes (`internal/promotion/bundle2.go:628-632`,
  `verification.Recompute`) and re-reduces risk (`:623`), and the head now also recomputes
  the diff against the candidate's own base on load (`VerifyWithBase`,
  `internal/promotion/release_store.go:155-188`). So a forged PASS fails every consumer
  that restores; the residual is trust separation, not an unchecked PASS.
- Impact. R6 asks for immutable verifier evidence from a distinct identity; today the
  producer, the stager and the first reader are one process. The M12 report recommends a
  separate verifier credential. Approval-time recomputation exists only on the M13 line.

### N-09 (MEDIUM) the numeric gates are not enforced

- Evidence. `Makefile:37-40` define `test-performance` and `test-write-performance`
  (tags `integration,performance`); `make test` is `test-unit test-integration`
  (`Makefile:41`) and CI runs `make test` (`.github/workflows/ci.yml:26`). CI never runs
  `make release-check`, a restore drill or any deployment step. The p95 targets (750 ms
  read, 500 ms write) and the recorded margins (R5b receipt scan about 67 ms below its
  timeout) are therefore verified by hand only, and the R3a report records the same test
  failing on one host and passing on another.
- Suggested work. A scheduled CI job (or documented release checklist enforced by the
  release owner) for the performance floors, `release-check` against a restored database,
  and the first restore drill.

### N-11 (MEDIUM) tested retry contract is dead code

- Evidence. `Delivery.MayRetry` (`internal/tools/delivery.go:40-70`) is covered by
  `delivery_test.go:12-28` and has no production caller; `decideTool` encodes different
  rules. The test therefore passes while production is wrong in exactly the way the test
  forbids (N-04). Same pattern risk for `Delivery.Validate`, which is used by the catalog.
- Suggested work. Make `decideTool` call the contract or delete the contract, and add a
  test that drives `decideTool` over the same table.

### N-08 (MEDIUM, documentation) stale authoritative documents

- Evidence. See FA-6 and FA-7. Also: the plan header and "Checkpoint status" and the
  ledger "Execution order" still say M10 stays paused
  (`docs/migration/M9R_REMEDIATION_PLAN.md:27`, ledger last section);
  `docs/migration/V2_V3_SUPERSESSION_MAP.md:12` still says R1 "must supersede" the worker
  claim; `internal/sandbox/README.md` was corrected on the head. The honesty audit
  recorded the same AGENTS.md and CLAUDE.md staleness (`CONTRACT_HONESTY_AUDIT.md:76-84`).
- Impact. `AGENTS.md` is "the single coding-agent contract"; an agent that believes only
  `local.noop` exists will not apply tool-delivery rules.

### LOW gaps

- N-10 default-open environment: see FA-8. Suggest requiring `APP_ENV` explicitly outside
  a development launcher, or refusing dev issuer defaults unless `APP_ENV` is set.
- N-06 constant-true `releaseGate` (`releases.go:46-48`) and constant audit labels
  (`promotion/store.go:40`). Harmless while only `Stage` uses it; M13 must not reuse it
  for approve or activate.
- N-07 stage index is not the verified body: duplicate keys and key case let the index
  and the Go reader disagree (`00048:78,96,121`; M12 DB review F2). Honest through the API
  (every reader re-restores canonical bytes); the migration comment should say so.
- N-13 add negative tests for `tools.invoke`, `sandbox.read`, `sandbox.manage`,
  `requirements.decide`, `acceptance.author`, `candidates.build` to the policy matrix
  (`internal/policy/policy_test.go:10`), including custom-role delegation limits.
- N-14 derive `worker_grants_safe()` from an allowlist of what the worker may hold,
  including REFERENCES and TRIGGER (M13 migration 49 extends the denylist only).
- N-15 retire or wire `runtime.unique_projections`; keep `Store.Approve/Activate/Rollback`
  unreachable until the M13 ports replace them.

## What could not be verified

- NOT RUN (no database by design): every integration test, the three performance
  floors, the R2c 100k migration, the M10 to M12 integration suites. For these the
  ledger says "recorded PASS" or "read". The attacks in F01 are verified as present and
  asserting denial, not as passing today.
- NOT RUN: frontend and browser tests (F10, R5c UI), `make check`, `make build`.
- Spot-checked only, by `git show` (no checkout, no execution): R5d, R5e and R5f on
  `origin/remediation/m9r-foundation`; migration 49, ADR039 and the promotion routes on
  `claude/m13-approval-promotion`; migration 50 on `claude/m10-fixes`. Their own
  "closed" claims are not certified here.
- The M11 review (`review/m11-final`) has no findings recorded; the M12 API review
  (`review/m12-api-final`) is on an unmerged branch and was not read in full; the
  verifier and gateway reviews are marked in progress.
- The original review's one-thousand-entry history cap and the 87 percent audit-row
  statistic were not re-derived.
- Statements about operating systems, TLS and deployment isolation are untested (no
  deployment exists).

## Recommended order of work

1. Resolve the line split (N-01). Decide the integration order of the remediation line
   (R5c UI, R5d, R5e, R5f), `claude/m10-fixes`, `claude/m13-approval-promotion` and the
   M10 to M12 head; assign unused migration, ADR and version-token numbers; then run the
   full integration, browser and performance suites once on the merged tree. Nothing
   below can be called part of the foundation gate until this passes.
2. Review and merge the M10 fixes (N-02, N-03, N-04, N-11) before any WRITE tool is
   enabled outside a developer machine; add raw-SQL worker regression tests for approval
   skipping and instance state changes.
3. Review and merge the M13 control plane (N-05, N-06), then call `VerifyWithBase`
   (already in `LoadCandidate`) from approval and activation, and remove or keep dead
   the M6R `Store.Approve/Activate/Rollback`.
4. Enforce the gates (N-09): scheduled performance job, `release-check`, restore drill.
5. Bring the documents back in line (N-08): `AGENTS.md`, `CLAUDE.md`, `README.md`, plan
   header, ledger last section, supersession map row 12, stale comments (FA-7).
6. Finish the open foundation slices: R3b other types, R3c uniqueness, R3d aggregates,
   R7 items (outage budget, rate limiting, audit retention, profiling, DR).
7. Separate the verifier credential (N-12) with R6 and the AI-capture milestone.
8. Hardening: N-10, N-13, N-14, N-15, N-07.

Finding-status audit STATUS: COMPLETE
