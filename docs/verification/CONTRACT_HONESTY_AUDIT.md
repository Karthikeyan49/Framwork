# Contract-honesty audit — M10, M11, M12 documents versus code and tests

Audited tree: `fb52ad3` ("docs(m12): record full integration result and decider wording"),
branch `review/honesty-final`. ADR039 is audited at `6418293`
(`claude/m13-approval-promotion`, file `docs/adr/039-m13-approval-and-promotion.md`),
read with `git show`; it is not part of the audited tree and was not edited.

## Progress

- [x] Branch created; scope documents read (ADR036/037/038, design appendix, M10/M11
  and M12 reports, supersession map, module READMEs, ADR039, migrations 45-48).
- [x] Pass 1 (read and cross-check): migrations 45-48, `internal/tools`, `internal/risk`,
  `internal/promotion`, `internal/platform/access/releases.go`, policy actions,
  M10/M11 integration tests. Results are in "Findings" and the claim tables.
- [ ] Pass 2: compiler (diff, classify, workflow tools), intent, verifier, metadata
  rebase, workflow `tool_step.go`, sandbox API, report numbers, links, ADR039 facts.
- [ ] Probe: does `promotion.Restore` accept a self-consistent forged diff? (finding H1)
- [ ] Corrections commit (documents only) and `node scripts/verify-docs.mjs`.

The claim tables list only claims that were actually checked; a claim that is not in
a table was NOT checked (see "Not checked").

## Method

Every numbered Decision, Consequence, Amendment and measurable sentence is located in
code, SQL or a test (file:line), executed when cheap (unit packages only, `nice -n 10`,
no database), and classified:

- VERIFIED: code or test proves the sentence as written.
- FALSIFIED: code or test contradicts it.
- OVERSTATED: true in a narrower sense than written.
- NOT TESTED: the code agrees, but no test would fail if it were false.
- STALE: was true or intended earlier and no longer matches.

Truth rules follow `/home/user/briefs/00-common.md`: PASS / FAIL / NOT RUN only.
Severity: HIGH = a document asserts a security/integrity guarantee the code does not
provide; MEDIUM = materially overstated or stale; LOW = wording.
Evidence is quoted as `file:line` at `fb52ad3`; migrations are abbreviated `00045` etc.

## Findings

### HIGH

**H1. `Restore` is documented as recomputing the diff and storage class; it does not.**
(status: established by code reading; the executed probe is listed under Progress)
- Claims: `docs/adr/038-m12-candidate-release-builder.md:208-213` (Amendment 2: 2.0.0
  bodies "are fully recomputed (diff, storage class, facts, risk, ...)"), `:160-162`
  (`Restore` recomputation "is the backstop"), `internal/promotion/README.md:10-13`
  ("`Restore` recomputes the diff, storage class, risk facts ..."),
  `docs/migration/M12_DESIGN_APPENDIX.md:193-196` ("recompute Diff/Risk/Verification
  ... and require byte equality"), `docs/migration/M12_IMPLEMENTATION_REPORT.md:77-80`,
  and ADR039 (`6418293`) section 1 step 2 ("`LoadCandidate` ... `Restore`s: risk,
  verification, diff, dependency hashes are recomputed from the stored bytes").
- Code: `internal/promotion/bundle2.go:474-476` says the diff is "the one artifact it
  can only check structurally; VerifyAgainstBase recomputes it". `restoreV2` only calls
  `compiler.ParseDiff` (canonical bytes and hash binding, `:529-532`) and checks that
  the storage class agrees with the schema hashes (`:533-539`). Risk facts are
  re-reduced from the STORED diff (`:612`), so a forged diff yields matching forged facts.
  `VerifyAgainstBase` (`:644-687`) is the only recomputation of diff, storage class and
  evolution plan, and nothing calls it: `Store.LoadCandidate`
  (`internal/promotion/release_store.go:157-178`) and `getCandidate`
  (`internal/platform/access/releases.go:375-392`) use `Restore` only.
- Consequence: a self-consistent body whose diff/facts/risk understate the real change
  (for example `ADDITIVE` for a `TRANSFORM`, or no financial-rule change) passes
  `Restore`. ADR038 section 6 names `Restore` as the backstop against a compromised API
  process; for diff-derived facts (risk level, storage class, completeness inputs) it is
  not one unless the caller also runs `VerifyAgainstBase` with the base program.
- Correct statement (applied in the corrections commit): `Restore` re-derives the
  IR/source/dependency identities, requirement and acceptance validity, risk from the
  stored facts and the verifier report; diff and storage class are checked only
  structurally; approval and activation MUST also call `Candidate.VerifyAgainstBase`.
  The code gap (no caller wires it) is reported here, not papered over.

### MEDIUM

**M1. AGENTS.md and CLAUDE.md status text is stale (not edited, per brief).**
`AGENTS.md:3-6` ("Current authorized work is M9R ... Do not begin M10 automatically"),
`AGENTS.md:62-65` ("Only `local.noop` is registered ... external tools and production
activation remain later milestones") and `CLAUDE.md` ("Do not continue M10 without the
remediation gate and a new user task") predate ADR036-038, which record the user's
authorization (ADR036 "Authorization and gate", ADR038 "Authorization"). With
`TOOL_CATALOG_FILE` set, operator catalog tools (`tool.*`) are registered and run
(`internal/compiler/workflow_tools.go`, `internal/runtime/workflow/tool_step.go`).
The owner should update both files.

**M2. `internal/sandbox/README.md` is stale.** It says "Reserved module boundary. No
implementation in M0." The package holds the M6R caller-declared `Context`
(`internal/sandbox/context.go`), and M11's server-owned sandbox lives in
`internal/runtime/data/dataset.go`, `internal/platform/access/sandboxes.go`,
`internal/tools/mode.go` and migration 46, not in this package. Corrected.

**M3. ADR036 decision 4 lists an "external ref" column that does not exist.**
`runtime.tool_invocations` (`00045:9-26`) has no external reference column, and
`grep -ri external_ref` finds nothing in Go or SQL. The reference used for
reconciliation is the deterministic `idempotency_key`. Corrected.

**M4. ADR036 decision 6 says a policy block is "FAILED retryable"; the code says permanent.**
`internal/tools/gateway.go:27-28` (`ClassBlocked` "FAILED permanent: mode or policy
forbids I/O"), `internal/tools/executor.go:106` (`blockedResult` is `Retryable: false`),
`internal/tools/README.md:86`; integration `TestM10CatalogTenantPinAndNetworkPolicyFailClosedWithoutIO`
(`tests/integration/m10_tools_test.go:314-318`) asserts class `blocked` and instance DEAD.
Only resolution/dial failures (`not_sent`) and rate limits are retryable. Corrected.

**M5. ADR036 "Verification" claims policy-revocation integration coverage that does not exist.**
No test in the repository mentions `tools.invoke` (grep over all `*_test.go`); no M10/M11
test revokes a role or a grant mid-flight; `internal/policy/policy_test.go` covers the
M12 actions only (`TestReleaseBuilderCapabilities`), not `tools.invoke`,
`sandbox.read` or `sandbox.manage`. The code is present (`internal/policy/policy.go:90-93`,
`00045:94-97`). Verdict NOT TESTED. Corrected as a stated gap.

**M6. ADR038 section 2 describes three derived dependencies incorrectly.**
- Relevant tool hash: documented as a hash of sorted `ref@version=tool_hash` strings
  "plus the pin list"; code hashes the canonical JSON array of sorted
  `{ref,version,hash}` pins only (`internal/promotion/dependencies.go:63-69`).
- Base schema hash "(the target's for a first release)": it is EMPTY for a first
  release and `restoreV2` requires that (`internal/promotion/bundle2.go:418-424,524-526`).
- Components hash "over ... the event envelope version": no event envelope version is
  included; the document also covers diff, risk-policy, verifier, vocabulary,
  requirement-policy, requirement-set and acceptance-spec versions
  (`internal/promotion/dependencies.go:113-137`). Corrected.

**M7. The M12 design appendix is stale against the shipped types.**
- G (bundle field list, `M12_DESIGN_APPENDIX.md:181-196`): omits `TenantID`, `AppID`,
  `Storage`, `Diff.Version`, `RuntimeContract.Components`, `Dependencies.PolicyBundle`,
  `Dependencies.ToolDefinitions` and `Sandbox.FrozenBy` (`bundle2.go:33-126`);
  `TenantID`, `AppID` and `FrozenBy` are read by `control.stage_candidate`
  (`00048:122,151`); and "Go tags are the JSON keys" is wrong: there are no tags, the Go
  field names are the keys (`bundle2.go:26-28`).
- D (risk): `Tools []{..., Approval, ...Change(ADDED|REMOVED|REPINNED|UNCHANGED)}` does
  not match `ToolFact` (no `Approval`; `INHERITED`, not `UNCHANGED`; plus
  `Unresolved`), `Facts` also has `ToolDeclarations`, `Assessment` also has `Rollback`,
  and `PolicyHash` also covers `rollback` (`internal/risk/risk.go:155-244`,
  `internal/risk/policy.go:38-45`).
- E: `approval_gate` "roles ⊇ required roles" contradicts Amendment 3 and the verifier
  README (set equality).
Corrected.

**M8. ADR038 section 4 names an audit action that is never written.**
Body: "FAIL/BLOCKED ... write `release.candidate_rejected`" (`038:132-133`); no code uses
it. The shipped action is `candidates.build.rejected`
(`internal/platform/access/access.go:465-467`), which Amendment 8 states. Corrected by
pointing to Amendment 8.

**M9. ADR038 section 6 says `stage_candidate` checks "app id equals the body's IR app".**
The port compares the body's top-level `AppID` with its argument (`00048:122`); it does
not parse the IR. IR/app agreement is checked in Go by `restoreV2` (`bundle2.go:496`) and
`BuildCandidate` (`:327`). Corrected.

### LOW

- L1. Amendment 7: "writes the bundle and its derived ... row in one statement" - two
  INSERT statements in one function call/transaction (`00048:161-167`). Corrected.
- L2. Amendment 8 omits that a successful stage ALSO appends the M6R `release.staged`
  entry from `Store.Stage` (`internal/promotion/store.go:60`, policy version
  `m6r-release-contract-v1`, `store.go:40`); the test counts only
  `release.candidate_staged` (`tests/integration/m12_candidate_test.go:309`). Corrected.
- L3. ADR037 decision 4 "forbids equal production/sandbox hosts": the catalog forbids an
  equal host AND port (`internal/tools/catalog.go:293`); equal secret refs are refused
  when the sandbox one is set (`:297`). Corrected.
- L4. ADR036 decision 8 lists metadata, unspecified, multicast and broadcast as refused
  "unless the definition is `internal`"; they stay denied even then
  (`internal/tools/netpolicy.go:141-155`, `internal/tools/README.md:73-75`). Corrected.
- L5. ADR036/037 "owner/admin" for freeze, register and reconcile is an API policy rule;
  the SECURITY DEFINER ports check only an active non-viewer member
  (`00046:74,87`; `00045:238-240`; `00048:68`). Wording clarified, no defect claimed.
- L6. ADR036 decision 4: the worker ports are "bound to the exact authenticated lease and
  the live SQL policy decision"; `control.tool_finish` checks only the lease
  (`00045:192-213`), `tool_begin`/`tool_abandon` check both (`:152`, `:222`), and
  `tool_reconcile` is an API-role port (`:261`). Corrected.
- L7. ADR038 section 7 "Reads report FRESH/STALE": the API returns `stale` (bool) and
  `stale_reasons`; only the detail route derives it (`releases.go:227-228,375-392`). The
  list route (`:103-109`) does not. Corrected.
- L8. ADR038 section 9 omits the extra read routes `.../requirements/{version}` and
  `.../acceptance-specs/{spec}` (`releases.go:34,37`). Corrected.
- L9. Go comments (not edited, outside scope): `internal/risk/risk.go:5-8` and
  `internal/risk/facts.go:188` say volume is "server-measured"; no measurement exists
  (`promotion.DefaultVolume`, ADR038 Amendment 1).

## Claim tables (pass 1)

Columns: document:line | claim | verdict | evidence.

### ADR036 (`docs/adr/036-m10-tool-registry-v2.md`)

| Doc:line | Claim | Verdict | Evidence |
|---|---|---|---|
| 21-23 | M9 activities run inside one 3 s leased transaction; only `local.noop` existed | VERIFIED (3 s transaction, 5 s lease) | `internal/platform/postgres/transaction.go:52`; `00020_m9_workflow_runtime.sql:131` |
| 27-29 | Tools registered only by the operator in strict JSON `tool-catalog-v1` | VERIFIED | `internal/tools/catalog.go:19,149-165,493-538` |
| 31-32 | Side effect `NONE\|READ\|WRITE` only | VERIFIED | `catalog.go:223` |
| 32-33 | WRITE requires `approval: REQUIRED` and a non-GET method | VERIFIED | `catalog.go:226,246` |
| 34-36 | Replay window for NATIVE_KEY/DEDUPE_WINDOW; lookup path with `{key}` | VERIFIED | `catalog.go:233,236,249` |
| 36-38 | Literal DNS hosts, HTTPS, timeout <=2000 ms, response <=16 KiB, per-tool rate | VERIFIED | `catalog.go:23-24,243,334` |
| 37-38 | Canonical hash; a changed definition never replaces a pin | VERIFIED | `catalog.go:120,200-207`; `m10_tools_test.go:299-312` |
| 48-51 | `tools.invoke`: non-viewer builtin roles; custom roles must grant it; `viewer` never | VERIFIED (code) / NOT TESTED | `policy/policy.go:90-93`; `policy/definition.go:131,221`; no test names it |
| 50-51 | Worker SQL decision requires `tools.invoke` for plans with a tool activity | VERIFIED (code) / NOT TESTED | `00045:94-97`; no test revokes it |
| 54 | Ledger includes an "external ref" | FALSIFIED (no such column) | `00045:9-26`; grep finds none |
| 54-55 | `tool_attempts` immutable | OVERSTATED (one finishing UPDATE, then immutable) | `00045:207`; `00047:10` |
| 55-57 | Worker writes only via definer ports bound to lease AND live policy decision | OVERSTATED for `tool_finish` | `00045:152,192-213,222` |
| 57-58 | No bodies/headers/secrets in attempts; ledger never stores credentials | VERIFIED (no such columns); secrets absent from audit/ledger/receipts/outbox (tested) | `00045:29-38`; `m10_tools_test.go:68-78` |
| 65-66 | Crash/lost lease between (a) and (c) leaves SENDING, next lease treats it as UNKNOWN | VERIFIED | `00045:216-230`; `m10_tools_test.go:101-119` |
| 66 | Late outcome from a stale lease is rejected | VERIFIED | `00045:204-205`; `m10_tools_test.go:323-340` |
| 67-70 | 2xx + valid typed body = SUCCESS; 4xx permanent; 429 retryable; timeouts, 5xx/408/409, broken transport, invalid responses UNKNOWN | VERIFIED | `executor.go:331-388`; `m10_tools_test.go:245-267` |
| 68-69 | never-transmitted (resolution, dial, policy, local limit) = FAILED retryable | FALSIFIED for "policy" | `executor.go:106,171-196`; M4 |
| 71-77 | NATIVE_KEY/DEDUPE resend same key in window; LOOKUP found/absent; NONE parks in RECONCILE; reconcile closes ledger; APPLIED/NOT_APPLIED | VERIFIED | `m10_tools_test.go:101-212`; `00045:233-249` |
| 78-81 | REQUIRED tool sends only with API-written approval by a non-starter and unchanged record version; otherwise fails without I/O | VERIFIED | `00045:161-172`; `m10_tools_test.go:46-84,269-283` |
| 82-85 | HTTPS verification, no proxy/redirect/compression, escaped path segments, fixed host header, typed JSON body | VERIFIED | `executor.go:63-90,294`; `connector.go:156-180` |
| 85-89 | Listed ranges refused "unless internal and opted in" | OVERSTATED (some never allowed) | `netpolicy.go:141-155` |
| 89-91 | Credentials never appear in logs, traces, audit, ledger or errors | VERIFIED for audit/ledger/receipts/outbox (tested); span/error text by code | `m10_tools_test.go:68-78`; `executor.go:137-149,324` |
| 105-106 | Tools longer than ~2 s unsupported | VERIFIED | `catalog.go:23` |
| 112-118 | Integration tests cover each idempotency mode, crash after send, late response, reconciliation, approval/freshness, policy revocation, raw worker SQL denial | VERIFIED except "policy revocation" (NOT TESTED) | `m10_tools_test.go` (13 tests); M5 |
| 116 | Migration 45 additive, forward-only | VERIFIED | `00045:262-265` |

### ADR037 (`docs/adr/037-m11-sandbox-v2.md`)

| Doc:line | Claim | Verdict | Evidence |
|---|---|---|---|
| 26-34 | `datasets.environment` admits `sandbox`; created only OPEN and EMPTY; registered in `runtime.sandboxes` in the same transaction (deferred constraint trigger) | VERIFIED | `00046:6-7,36-51,66-67`; `m11_sandbox_test.go:134-142` |
| 31-32 | Sandbox identity immutable, rows cannot be deleted | VERIFIED | `00046:53-58,68`; `m11_sandbox_test.go:127-132` |
| 35-39 | Only `SYNTHETIC_SEPARATE` admitted | VERIFIED | `00046:11` |
| 40-48 | Effective mode = `tools.EffectiveMode`; `control.tool_begin` independently rejects a mode above the ceiling or any attempt on a frozen sandbox; environment from `control.worker_environment()` | VERIFIED | `tools/mode.go:27-58`; `00046:113-122,138-144`; `m10_tools_test.go:390-468` |
| 49-53 | Ceiling can only reduce the declared mode; catalog forbids equal production/sandbox hosts and secrets; gateway never resolves the production ref in a sandbox (recording resolver) | VERIFIED except "hosts" (host AND port) | `catalog.go:293-299`; `executor.go:223-246`; `m10_tools_test.go:395,417` |
| 54-63 | Freeze records one immutable evidence body with the listed bindings, then marks sandbox and dataset FROZEN; FROZEN cannot be left | VERIFIED | `00046:81-111,59-64`; `m11_sandbox_test.go:150-186` |
| 60-61 | Record writes refused after freeze | VERIFIED | `m11_sandbox_test.go:168` |
| 60-61 | Workflow starts refused, evolution impossible after freeze | NOT TESTED (dataset scope requires OPEN) | `runtime/data/dataset.go:290` |
| 61-62 | Catalog and policy hashes computed by the server, not request fields | VERIFIED as to the HTTP request; the port trusts its caller | `00046:81,89` |
| 64-66 | `sandbox.Context` and bundle 1.0.0 bytes unchanged | VERIFIED | `internal/sandbox/context.go`, `promotion/bundle.go` unchanged in `git diff e6305df..fb52ad3` |
| 67-72 | Routes; `sandbox.read` all roles, `sandbox.manage` owner/admin, not delegable | VERIFIED (code) / NOT TESTED for custom roles | `policy/policy.go:76-90`; `policy/definition.go:127-128` |
| 87-90 | Tests cover EMPTY-only, isolation, viewer/cross-tenant denial, replay/ceiling conflict, direct-DML denial, freeze immutability, frozen write refusal, forged worker mode, tool modes | VERIFIED | `m11_sandbox_test.go:34-186`; `m10_tools_test.go:390-468` |
| 91 | Migration 46 additive, forward-only | VERIFIED | `00046:194-197` |

### ADR038, appendix, READMEs (pass 1, partial)

| Doc:line | Claim | Verdict | Evidence |
|---|---|---|---|
| 038:36-40 | Requests naming authority fields fail with 400 | VERIFIED (strict body) | `releases.go:196-203,282-285` |
| 038:48-49 | Base from `control.active_releases`; mismatched `expected_base` is 409 | VERIFIED | `releases.go:317-324` |
| 038:54-56 | `HighRisk` top-level, derived `risk >= HIGH`; `RequesterID` = actor | VERIFIED | `bundle2.go:426`; `00048:122-128` |
| 038:60-64 | Policy bundle hash over engine/definition versions, action catalog hash, role heads | VERIFIED | `dependencies.go:79-108`; `releases.go:254-264` |
| 038:65-75 | Relevant tool hash, schema snapshot, components hash as written | FALSIFIED in detail | M6 |
| 038:79-96 | `release-risk-v1` rules, matrix, volume thresholds, separation | VERIFIED | `release_risk_v1.go:28-95`; `risk_test.go:468-492` |
| 038:88-91 | Volume "measured by a tenant-scoped definer function" | STALE (Amendment 1) | `bundle2.go:268-276`; `releases.go:443` |
| 038:140-147 | Separation of duties at build time, DB-enforced in the stage port | VERIFIED for requester/acceptance/source author; decider/freezer NOT compared (Amendment 4 states it) | `00048:143-145` |
| 038:149-162 | `stage_candidate` owner, search_path, execute grant; direct INSERT revoked | VERIFIED | `00048:63-64,172-179` |
| 038:164-171 | Ten staleness reasons, derived not stored | VERIFIED | `staleness.go:12-23,73-124` |
| 038:198-251 | Amendments 1, 3 (set equality), 5, 6 | VERIFIED | `bundle2.go:271`; verification README; `facts.go:220`; `policy.go:19,86-89`; `definition.go:127` |
| 038:208-213 | Amendment 2 "fully recomputed" | OVERSTATED (H1) | `bundle2.go:474-476,529-539` |
| Appendix D | Rule table S1-S14, storage x volume matrix, separation | VERIFIED | `release_risk_v1.go:28-95` |
| promotion README:10-13 | `Restore` recomputes the diff, storage class ... | OVERSTATED (H1) | as H1 |
| risk README | pure, monotone max, `Registry.Verify`, case-insensitive `CheckSeparation`, `VolumeBucket` thresholds | VERIFIED (stdlib-only import claim pending) | `risk/policy.go:238-350,428-440`; `registry.go:79-93`; `separation.go:39-88` |
| tools README | modes table, request construction, network policy, outcomes table, secrets | VERIFIED | `executor.go:167-389`; `connector.go`; `netpolicy.go`; `secrets.go` |

### Reports and map (pass 1)

| Doc:line | Claim | Verdict | Evidence |
|---|---|---|---|
| M12 report:59,64 | "`-run TestM12` (5 tests)" | VERIFIED (count) | five `TestM12*` functions in `m12_candidate_test.go` |
| M12 report:70-73 | Exploit proof is `TestM12DatabaseEnforcesTheBoundariesTheAPIClaims` | pending (pass 2) | `m12_candidate_test.go:325` |
| Supersession map:29 | Baseline `81b88d2` | VERIFIED (commit exists) | `git cat-file -t 81b88d2` |
| M12 appendix A | `internal/risk` and `internal/verification` added to the boundary list | VERIFIED | `git diff e6305df..fb52ad3 -- tests/security/boundaries_test.go` |

## Not checked

_(updated at the end of pass 2)_
