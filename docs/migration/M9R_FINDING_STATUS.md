# External review finding status

Current checkpoint, 19 October 2026: [R7b event outage budgets](M9R_R7B_IMPLEMENTATION_REPORT.md)
pass bounded independent review at product `031c8a1` and review `a15d629`.
All three review findings are closed within the enforced drained-cutover scope.
Final root check/test/build passes304 integration groups884.394s with zero
failures/skips,126 frontend tests and9 Node tests; browsers pass11+7. The corrected
source survives600 real seconds of consumer/broker outage and recovers exactly3
effects/audits/inbox with19 retained attempts. Unchanged reference gates pass:
ten-client read p95 158.903ms;32-client writes6527/p95 349.656ms, zero errors.
These measurements are slower than R7a; no performance neutrality is claimed.
Migrations55–57 preserve evidence;57 refuses an active old reader/binder and
requires all owned workers (including workflows), emitters and dispatchers to
drain. Event jobs use RC/direct restrictive RLS; API/workflow stay RR. The first
600s failure and all independent failures remain in the reports. Replay,
subscriber/payload contracts, other workflow/operations work and R6 remain open.
**Overall M9R remains PARTIAL; no M10/main merge/production activation.**

Previous checkpoint, 19 October 2026: [R7a protected request admission](M9R_R7A_IMPLEMENTATION_REPORT.md)
passes bounded independent review at product `8f9805e`, review `cf45300`, and
final root checks/build with 283 integration groups without failures/skips,
126 frontend tests, browser suites 11 + 7 and all eight unchanged reference targets.
The actual pinned old API produced 40 denial audits for 40 malformed requests;
the fixed API produces 20 denial audits plus 20 HTTP 429 responses, with zero
audit rows for rejected requests. The 24 aggregate workload cases have exact
results and zero errors, worst HTTP p95 505.454ms; deliberate forced-generic-plan
diagnostic timeouts remain recorded as failures of that diagnostic mode.
This is process-local protection, not distributed abuse capacity or durable
security retention. R6 and the other
R7 requirements remain open. Overall M9R remains PARTIAL; no M10/main merge.

Previous checkpoint, 19 October 2026: [R5g role administration](M9R_R5G_IMPLEMENTATION_REPORT.md)
completes the remaining local R5 UI and deployment preparation. Product fix
`5c05d0b` and independent review/regressions `ba21eb0` close one demonstrated
MEDIUM receipt-integrity finding and one LOW connection-preparation finding.
Final check/test/build passes 275 integration groups without failures/skips and
126 frontend tests; browser suites pass 11 application plus 7 local sign-in cases.
Representative unchanged gates pass with zero errors: ten-client numeric-read
p95 112.045ms and 32-client writes p95 243.551ms (9011 writes). The linked report
records exact budgets/profile and all initial failures. Local R5 completion is
the user-selected scope: no external provider yet.
Real IdP/client, MFA, production HTTPS gateway/log/TLS/rotation witnesses remain
DEFERRED/NOT RUN and remain deployment release gates. R6 authority, R7 operations,
wider search/uniqueness lifecycle and production active-runtime wiring remain
open. Overall M9R remains PARTIAL; no main merge, deployment or M10.

Previous checkpoint, 19 October 2026: [R3d SQL aggregation](M9R_R3D_IMPLEMENTATION_REPORT.md)
passes bounded independent functional review and final root verification. All 24
unmapped/current/retained 100k-row/ten-client capacity cases pass with zero errors;
worst HTTP p95 is 712.030 ms against 750 ms. Full check/test/build passes 269
integration groups without failures/skips and 97 frontend tests; browser 9+7 and
all eight reference capacity targets pass. The calibrated ADR058 reference-v2
profile and actual default 4 API admission are accepted for this measured tier.
32-client writes (8,163; p95 283.368 ms) and unique writes (7,081; p95 304.839 ms)
pass, with
lower throughput than historical R3c results retained as a tradeoff. The explicit
driver-panic diagnostic still FAILs under R7; no arbitrary driver recovery claim.
R5 role-definition UI/external-provider deployment, R6 authority, R7 operations,
wider search/uniqueness lifecycle and production wiring remain open. Overall
M9R remains PARTIAL. No M10 or main merge.

Previous checkpoint, 18 October 2026: [R3c unique-field integrity](M9R_R3C_IMPLEMENTATION_REPORT.md)
passes bounded independent review and final root verification on source `d15be92`
with review/tests `2cf2f57`. Opt-in schema/IR1.4, governed copy/verification,
private live-authority key permits, abort generations and compatible legacy
bindings are implemented without tenant DDL or record loss. The reviewer
reproduced and closed the receipt-retry regression. Final checks/build pass
246 integration groups with zero failures/skips, 97 frontend tests, 9 application
plus 7 sign-in browser cases and all seven capacity gates. Unique-key writes:
10,061 updates, p95 227.569ms, zero errors at 32 clients. Uniqueness UI/removal/
evolution, wider search, R3d, remaining R5 deployment/UI, R6 and R7 stay open.
**Overall M9R remains PARTIAL; M10/main merge/production remain outside this task.**

Publication checkpoint, 17 October 2026: the completed work through `197fe85`
was recovered from the remote after the provided workspace returned to an older
baseline. The unfinished R3c files were unavailable and are not claimed as pushed
or complete. See the [resumption checkpoint](M9R_RESUMPTION_CHECKPOINT_2026_10_17.md)
for the recovery evidence, completed/pending inventory and reconstruction notes.

Updated 16 October 2026 (Asia/Calcutta). Baseline review: `81b88d2`; source corrections
through `a5bba7b` include R2c preview evolution, bounded R5a capabilities and
[R5b worker SQL authority](M9R_R5B_IMPLEMENTATION_REPORT.md) on `remediation/m9r-foundation`. Rollback/recovery
and FIFO evidence are in the R4 report; governed numeric indexes and corrected
regressions are in the [R3b report](M9R_R3B_IMPLEMENTATION_REPORT.md).
R5c server administration and [R5e custom approval roles](M9R_R5E_IMPLEMENTATION_REPORT.md)
are now independently verified; their source/tests at `f1e2e74` passed full
checks/build and browser regressions. [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md)
source at `a4d6054` and fixture correction at `3466759` passed independent
adversarial review and final full checks/tests/build (integration 829.428s),
browser 6/6, migration idempotence and configured numeric 100k/ten-client
performance checks (HTTP p95 216.415 ms, zero errors). Its earlier connection
failure remains unconfirmed and tracked under R7 below.
[R5c record/team UI](M9R_R5C_UI_IMPLEMENTATION_REPORT.md) now passes independent
adversarial review, full checks/tests/build (177 integration tests, 561.291s;
frontend 86/86), browser 9/9, idempotent migrations and the configured
numeric 100k/ten-client gate (p95 113.715ms, zero errors). Its three demonstrated findings are closed;
the separate worker-capacity failure remains below.
[R5f](M9R_R5F_IMPLEMENTATION_REPORT.md) now passes independent executable review
and full checks/tests/build (179 integration tests, 605.921s; frontend 94/94).
Pinned OIDC/PKCE and bounded clock tolerance are wired to browser sign-in;
the reproduced MEDIUM concurrent-cookie finding is closed. External provider
deployment and production gateway/MFA verification remain unverified.
[Text/civil-date R3b](M9R_R3B_TEXT_DATE_IMPLEMENTATION_REPORT.md) now passes
independent executable review and all twelve 100k-row/ten-client capacity cases.
The review reproduced and closed one MEDIUM intrinsic-ID admission defect and
one HIGH cold-plan scan-amplification defect; failed runs remain in the review.
[Enum/reference R3b](M9R_R3B_ENUM_REFERENCE_IMPLEMENTATION_REPORT.md) now passes
independent review (36 tests) and twelve reference capacity cases, with actual
retained-binary CRUD and maximum enum checks. No high/medium finding was identified
in that bounded review. Final root checks/build pass: 206 integration tests with
zero skips, 96 frontend tests, 9 application + 7 sign-in browser cases, migrations
through 49 and all four numeric/scalar/choice/write performance gates. Exact
commands and limitations are in the linked report.
[Datetime/boolean R3b](M9R_R3B_DATETIME_BOOL_IMPLEMENTATION_REPORT.md) now also
passes independent review (47 selected groups) and twelve reference capacity
cases, with actual retained-binary CRUD, unrounded UTC boundary checks and native
SQL rounding/cursor equivalence. No high/medium product finding was identified.
Final tested source `fb381d0` passes 217 integration tests with zero skips,
96 frontend tests, 9 application + 7 sign-in browser cases, migrations through 50
and all five numeric/scalar/choice/native/write performance gates. The final
native gate has worst p95 301.813ms and zero errors; the preserved 32-client write
gate completes 11153 writes, p95 209.276ms, zero errors. Uniqueness/aggregate
and wider product/authority/operations work remain open.
[Full-text search R3b](M9R_R3B_SEARCH_IMPLEMENTATION_REPORT.md) now passes
independent review (55 retained groups, nine new race-reviewed adversarial groups)
and all eight selective 100k-row/ten-client capacity cases. Actual lost-COMMIT,
observed copy/write overlap, current/retained ACL/tombstones, raw roles, parser and
policy faults, shared quota races, corrupt-vector verification and retained old-API
CRUD/cursor continuity pass. Initial joined capacity failed HTTP503/SQLSTATE57014;
fenced canonical target lookups fixed it under unchanged budgets. Final source
`29a61ac` passes 230 integration tests with zero failures/skips, 96 frontend tests,
9 application plus 7 sign-in browser cases, migrations through 51 and all six
numeric/scalar/choice/native/search/write gates. Final search worst p95 203.519ms,
zero errors; 32-client write gate 11989 writes, p95 192.061ms, zero errors.
Search is an API foundation with fixed simple/plain AND semantics; search UI,
ranking/languages and broad/common-term capacity are separate work.
Full product R3b/R5/M9R remains PARTIAL.
This is a progress ledger,
not a claim that the review's complete scope has passed.

The original [validation](../verification/M0_M9_REVIEW_VALIDATION.md) records
which claims were reproduced, source-confirmed, corrected or unverified. The
[remediation plan](M9R_REMEDIATION_PLAN.md) owns implementation and acceptance.
Historical timing measurements and audit percentages are not current guarantees.

| Finding | Current disposition | Remaining acceptance / owner |
|---|---|---|
| Compromised worker credential can discover another tenant and mutate unrelated records | Fixed within the independently tested database credential/resource threat model; R1 PASS | Preserve exact lease, record, field and tenant confinement in every later adapter; process/OS secret isolation still requires deployment verification |
| Normal roles own tables / broad worker grants | NOLOGIN ownership retained; worker grants and drift admission hardened | Regression required for each new table and function |
| Tenant-controlled shared DDL | Generic JSONB runtime has no such path; existing ALTER denial retained | Future indexes/migrations must remain governed platform operations |
| Records disappear across compatible compiler/release changes | R2a/R2b and [R2c preview schema evolution](M9R_R2C_IMPLEMENTATION_REPORT.md) independently PASS: immutable generation maps, checkpointed transforms, current access/tombstones and workflow data pins; actual 100k migration and post-cutover queries verified | Production active-release execution remains M12/M13; dataset-management UI, automatic index inheritance and denser/larger migration capacity remain separate work |
| RLS membership checked once per row | Replaced by statement-level membership evaluation; populated isolation and SQL plans verified | Preserve live authorization and fail-closed RLS; HTTP latency gate remains below |
| End-to-end query performance | Governed numeric, text/civil-date, enum/reference, datetime/boolean and [selective full-text search](M9R_R3B_SEARCH_IMPLEMENTATION_REPORT.md) independently PASS within stated workloads. Final root p95 on18 October: numeric118.359ms, text/date worst264.759ms, enum/reference worst209.020ms, native worst207.305ms, search worst169.357ms; zero errors, unchanged750ms target. Source and target cold-scan amplification were reproduced and fixed with correlated live-view point lookups | Register/build a supported index; overflow, general/unindexed/selective and broad/common-term workloads require their own evidence. Deep projection traversal is not constant time |
| Shared dataset writer counter | Bounded R4 independently PASS at `94932e3`: overlapping writers/CAS/reference/fault checks and FIFO workload p95 340.575 ms, zero errors | Preserve regressions during indexing/evolution; previous failed runs retained |
| Write retries / duplicate effects / unknown COMMIT | Bounded R4 independently PASS: known-abort retries, owned receipts and commit-response/browser reconciliation verified | External delivery is not covered; first historical 503 cause remains unconfirmed |
| Late command rejection can commit a record without receipt/outbox | Independently reproduced fault and independently closed; real RLS-policy regression verifies zero business facts on rejection | Preserve whole-command rollback |
| Same-login navigation loses an in-flight command key | Independently reproduced and closed; actor-owned pre-dispatch journal/navigation/account recovery verified | Browser-restart persistence intentionally absent |
| Custom field indexes / unique projections | Integer/decimal/money, string/civil-date, enum/reference, datetime/boolean and fixed plain full-text indexes plus [R3c typed unique-field enforcement](M9R_R3C_IMPLEMENTATION_REPORT.md) independently verified; source JSONB, existing records, old readers/cursors and immutable releases retained. Populated backfill, conflict rollback, private permits, raw roles, concurrent claims/receipt replay and 100k/32-client gates pass | Uniqueness UI/READY removal/constraint-preserving evolution and search UI/ranking/languages/broad-term capacity remain separate. Bounded R3d aggregation now passes; wider planner features remain outside that acceptance |
| Aggregates and bounded join planner | [R3d](M9R_R3D_IMPLEMENTATION_REPORT.md) bounded PASS: opt-in exact SQL count/sums, transactional numeric validity witness, certified physical/canonical snapshot planning and configurable bounded API concurrency. Independent23-group race review, full269 integration groups, browser9+7, all24 aggregate capacity cases and all eight reference targets pass; earlier failures retained | No arbitrary expressions, GROUP BY, join expansion, aggregate/report UI or general workload guarantee. Worst aggregate p95 leaves only37.970ms headroom. Injected driver-panic recovery remains R7; production active-runtime wiring remains R6/M11–M14 |
| Production active-release runtime / public promotion lifecycle | Not implemented; current preview and release contracts are explicitly qualified | M12/M13 verified candidate, authoritative approval and immutable active-release execution |
| Caller-supplied risk / PASS / independence / requirement classification | Still incomplete authority integration | R6 and M12–M14 executable independent acceptance, authenticated provenance and server-derived risk |
| DataScope returns ALL for permitted built-in roles | [R5a](M9R_R5A_IMPLEMENTATION_REPORT.md) independently PASS: tenant role versions/CAS, live API scopes, exact operation reconciliation, bound engines and trusted worker revocation; bootstrap roles remain broad | R5b bounded PASS: raw leased source/business ports enforce current head/actions/fields/record scopes, with per-row lease fences. [R5c-a](M9R_R5C_IMPLEMENTATION_REPORT.md) independently PASS: governed CAS-fenced record owner/team/share administration (`data.row.assign`/`data.row.share`). R5c-b team hierarchy/membership administration (ADR035) independently PASS. [R5e](M9R_R5E_IMPLEMENTATION_REPORT.md) custom workflow approval roles and legacy compatibility independently verified, including the closed audit-context follow-up; final full checks/tests/build and browser5/5 PASS. [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md) field masking/partial writes now pass bounded independent and final full/browser/performance checks; [R5c record/team UI](M9R_R5C_UI_IMPLEMENTATION_REPORT.md) passes independent/full/browser gates; R5f provider sign-in/clock tolerance now passes bounded independent/full checks; [R5g role definition/assignment UI](M9R_R5G_IMPLEMENTATION_REPORT.md) now has independent and full local verification; real provider/gateway/MFA deployment remains DEFERRED/NOT RUN at user request |
| finance_manager / fixed four roles | Tenant capability roles and opt-in DSL2.1/IR1.3 custom workflow approvers implemented and independently verified; original1.2 reader/hash retained | Final full checks/tests/build PASS (integration629.247s) and browser5/5 PASS. Legacy schemas retain their declared four-role contract; role references do not provision authority. Record/team administration UI now has bounded independent/full/browser evidence; R5g role-definition/member-assignment UI now also passes local independent/full gates |
| Whole-request field denial / unrelated fields required for every update | [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md) bounded PASS: SQL masking, explicit partial writes and strict pinned-input guards independently reviewed; three demonstrated HIGH findings fixed; final full-suite/build/browser/performance checks passed | Preserve trusted owner/snapshot proofs, opaque 32-header bounds, condition-write restrictions and hidden-size reservations; record/team administration UI now has bounded independent/full/browser evidence. The intermittent connection failure remains an open R7 diagnostic item |
| R5g inconsistent publication receipt / canceled preparation state | Independent review reproduced one MEDIUM and one LOW defect; fix `5c05d0b` and strengthened regressions `ba21eb0` close both. Final local results are in the R5g report | Preserve normalized definition rehashing before journal release, client/key preparation fences, actor-owned memory recovery and no automatic POST retries. External identity deployment remains a separate gate |
| JWT skew and token-paste development login | [R5f](M9R_R5F_IMPLEMENTATION_REPORT.md) bounded PASS: configured public-client OIDC/PKCE, pinned ID/API token validation, one-time browser-bound Redis flows, explicit 0–120s tolerance and provider-first memory-only browser connection. Independent cookie-race finding closed; full regression passed | External provider/client interoperability, production gateway/query-log verification and MFA assurance remain release gates. No refresh/provider logout, implicit enrollment, SAML or SCIM. Manual token entry is development-labelled and hidden by the production descriptor |
| Intermittent workflow fixture connection failure during R5d verification | One full run failed with `driver: bad connection` while advancing an approval fixture; the unchanged test then passed three focused repetitions | R7 connection/deadline diagnostics and repeatability follow-up; cause unconfirmed. Preserve the failed run and final whole-suite evidence in the R5d report; do not describe reruns as a reliability fix |
| Worker policy receipt-scan capacity | R5c UI final regression hit the unchanged 2s statement timeout during EXPLAIN on the existing 96-receipt/63-node policy fixture; preceding query 1657.145ms. Three isolated unchanged-budget repetitions then passed (16.066s total), as did the final full rerun (worker first query 1146.412ms). Historical R5b already documented narrow timing headroom | R7 profile statement/identity/lease/anti-join costs and repeatability under representative load. Preserve first failure; aggregate suite timeout adjustment is separate and no capacity fix is claimed |
| Injected database driver/tracer panic | Independent [R3d review](../verification/M9R_R3D_INDEPENDENT_REVIEW.md) reproduces database/sql rollback lock hang in a supervised subprocess; explicit diagnostic FAIL7.272s. Ordinary callback-panic rollback/reuse passes; default connector installs no custom tracer | IR-R3d-01 remains an open R7 infrastructure/process recovery limit; do not claim arbitrary driver panics recover or are bounded by the transaction deadline |
| M8 short outage window / dead predecessor / one consumer | [R7b](M9R_R7B_IMPLEMENTATION_REPORT.md) closes bounded publication/receipt outage budgets:5s/15m,20 backed-off attempts, actual600s outage/recovery and independently verified live RC/RLS authority plus drained cutover | Still preview delivery. Least-privilege replay/quarantine, dead-predecessor operation, subscriber/payload versioning and faster broker-loss repair remain R7; no raw-process liveness or universal external exactly-once claim |
| Audit volume / retention / partitioning | Atomic, immutable audit retained; lifecycle not implemented | R7 measured audit policy and retention/archive design; do not delete receipts/evidence to retry |
| M9 DAG, noop, lease/timer/history limits and copied IR | Documented limits retained | Workflow lifecycle expansion requires explicit design, renewal/fencing tests and capacity evidence |
| Workflow approval uses start snapshot, not current document | Existing snapshot semantics retained | Before business side effects, bind current-document version or explicitly approve a snapshot and validate freshness |
| Rules have no consumer | Original statement corrected: preview and workflow conditions consume rule plans | Business tools remain later work; a rule result never grants authorization |
| External exactly-once and AI steps | No external tool/AI implementation or universal exactly-once claim | M10 per-tool delivery/reconciliation; AI milestones preserve deterministic authority |
| Rate limiting / invalid authentication DB amplification | [R7a](M9R_R7A_IMPLEMENTATION_REPORT.md) implements bounded process-local peer/global/verified-subject admission and invalid-bearer budgets before SQL, with closed rejection counters and aggregated redacted429 logs. Independent actual before/after binary attack, queued expiry/cancellation, known-abort retries and parallel abuse tests pass | Distributed gateway/proxy quotas, deployed abuse envelope, telemetry export/tail loss and durable security retention remain R7/deployment work. Valid signed but unprovisioned users still require live SQL under general quotas; no negative membership cache |
| Per-request compile / repeated validator construction / catalog checks | R3c/ADR045 profiles and removes repeated construction of immutable embedded JSON-Schema graphs only; concurrent validation and final read/write gates pass. Per-request source compilation and live catalog checks remain | Broader R7 profiling/admission/caching still open; preserve exact immutable keys, live membership/revocation and privilege drift detection |
| Two conflicting rulebooks / missing supersession map | R0 map and concise agent rules implemented; source kits retained verbatim | Keep clause-level authority and current capability inventory updated |
| No numeric verification gates / migration ownership | R2–R4 ADRs and reference targets now explicit | Execution must pass those gates; design and numbers alone are insufficient |
| Portability claim | Qualified as deferred; format and round-trip proof absent | M17 concrete export/import specification and executed portability verification |
| CI/deployment, SSO/search/localization/residency/import/DR | Foundation CI exists; product/deployment subprojects remain scheduled gaps | Assign concrete production release deliverables; do not infer readiness from foundation tests |
| Large review batches / thin review / concurrent edits | Remediation uses smaller stable commits and independent executed probes | Preserve review evidence and fix findings before accepting each boundary; AI review does not replace human release ownership |
| Stacked unmerged branches | Latest current implementation pushed on `remediation/m9r-foundation`; main remains M6 | Pushing is not merging; no main merge or production deployment performed |

## Execution order and stop gate

Bounded R4 and the configured numeric, text/civil-date, enum/reference,
datetime/boolean and selective full-text search R3b HTTP gates are independently verified.
R2c preview dataset evolution now passes independent fault/capacity tests, the
full root suite/build and browser regressions. Preserve those regressions.
Bounded R3c uniqueness and R3d SQL aggregation now also pass independent review
and final root gates, including all eight reference capacity targets. Broader
search, aggregate/report UI, general query planning and uniqueness administration/
removal/evolution remain separate. The bounded APIs do not complete product search
or general query capacity. R5 local role UI and deployment preparation now pass;
external provider verification is deferred. R7a local request admission now
passes independent/full/browser/all-eight capacity gates. R7b outage budgets now
pass independent/full/browser, actual600s recovery and representative unchanged
read/write gates. R6 and other R7 remain separate
product/security/operations deliverables. R5b closes the bounded raw-worker
capability gap; its synthetic receipt test has a narrow timeout margin and is
not a wider capacity guarantee. The independent report records which final
checks used existing tests and which new probes were not run.
Production, AI and external ERP actions are not enabled by a foundation PASS.

The bounded foundation gate and final backend/frontend/integration/browser
regressions now pass. M10 remains paused pending a separate user task; it must
not depend on unfinished capabilities or imply production readiness. Do not erase
failed runs, weaken assertions or substitute contract-only tests for wired
product behavior.

## Appendix: milestone-line audit (history, written before the two lines were integrated)

Everything below was written on the milestone line (`claude/m14-ai-builder`, M10 to M14) on 2026-10-17 and
2026-10-19, when it was a separate line from `origin/remediation/m9r-foundation`. It is kept as the record of
what that audit saw, with the numbers and verdict words of that time; it is not the live ledger. The live ledger is
the table above. The integrated branch `integration/m9r-m14` renumbers the milestone line, so read the references
below through this map:

| Milestone-line name in this appendix | Name in `integration/m9r-m14` |
|---|---|
| migrations `00045` to `00053`, `00060` (M10 to M14) | `00080` to `00089` (same order: 45 to 80, 46 to 81, 47 to 82, 48 to 83, 49 to 84, 50 to 85, 51 to 86, 52 to 87, 53 to 88, 60 to 89) |
| ADR036 to ADR040 (M10 to M14) | ADR100 to ADR104 |
| authoring schema, IR and compiler `1.3.0` with tool activities | `1.5.0` (`1.3.0` and `1.4.0` stay the remediation line's approval-role and integrity contracts) |
| workflow DSL `2.1.0` (M10 tool plans) | restore-only legacy shape; tool plans use DSL `2.2.0`, tenant roles keep `2.1.0` |
| "delivered on the remediation branch, not merged" | present in the integrated branch |

Rows that say a remediation slice is "not in the audited lineage" or "not merged" describe the audited trees only.
The status of the integrated branch is recorded in `docs/migration/LINE_INTEGRATION_PLAN.md`.

### Evidence audit (verified 2026-10-17)

An independent status audit re-checked every row against the code and tests of the
tree below. The two original columns are kept unchanged as history; the last two
columns are the audit result. The full analysis, the fake-assumption scan and the
list of newly discovered gaps (N-01 and later) are in
[the foundation gap analysis](../verification/FOUNDATION_GAP_ANALYSIS.md).

- Audited tree: `dbc4b5b` (head of `claude/m12-candidate-builder`: M0 to M12), merged
  into the audit branch `docs/finding-status`. This is the baseline for every verdict.
- M13 control-plane work lives on `claude/m13-approval-promotion` (`436c2b5`). It was
  read with `git show` only. Rows it affects say "closed on claude/m13-approval-promotion,
  not merged" and are NOT counted as closed at the baseline.
- The remediation line `origin/remediation/m9r-foundation` (`06882b2`: R5c UI, R5d, R5e,
  R5f) diverged from the M10 to M12 lineage at `e6305df`. Its code is not in the audited
  tree. Rows it affects say "delivered on origin/remediation/m9r-foundation, not in the
  audited lineage"; facts about it were spot-checked with `git show` only.
- Verdict words: CLOSED (code and a relevant test are present and the claim holds as
  written), PARTIAL (what remains is stated), OPEN, SUPERSEDED (replaced by a later design).
- Execution: no database was available to this audit. Integration tests were READ, not
  run; "recorded PASS" means the cited implementation report states a PASS that this audit
  did not reproduce. Executed here with `./scripts/go.sh test -count=1` and PASS:
  `internal/platform/postgres`, `httpserver`, `policy`, `runtime/data`, `runtime/workflow`,
  `config`, `identity`, `audit`, `promotion`, `risk`, `intent`, `verification`, `metadata`,
  `tools`, `sandbox`, `events` and `tests/security` (17 packages, exit 0).
- A claim in the first two columns that mentions an independent PASS is history. It is
  evidence that a review happened, not evidence for the current commit.

| Finding | Current disposition | Remaining acceptance / owner | Audit status (2026-10-17) | Evidence (verified 2026-10-17) |
|---|---|---|---|---|
| F01 Compromised worker credential can discover another tenant and mutate unrelated records | Fixed within the independently tested database credential/resource threat model; R1 PASS | Preserve exact lease, record, field and tenant confinement in every later adapter; process/OS secret isolation still requires deployment verification | CLOSED within the DB-credential threat model; two residuals are new gaps N-02, N-03 | Global claim is dispatcher-only: `00022:206-207` (revoke from worker, grant to dispatcher) and `00022:225-229` (`session_user<>'framework_dispatcher'`). Authority needs a context row bound to backend and transaction plus a live lease and membership: `00022:75-92` (`worker_authority`), `00022:96-133` (`bind_worker_job`). Generic record, metadata and outbox grants revoked `00022:153-156`; RESTRICTIVE `worker_no_generic USING(false)` `00026:13-20` and repeated for every later table (`00028:40`, `00030:22`, `00031:20,31`, `00035:19-67`, `00038:22,30`, `00046:19,28`, `00048:20,33,52`). Drift guard `control.worker_grants_safe()` (current body `00048:182-184`) is enforced in Go before every job (`internal/platform/jobs/jobs.go:46,79,129`) and in readiness (`internal/events/readiness.go:19`, `internal/policy/readiness.go:30`). Since migration 27 the worker gained only exact-lease ports (`00037:100`, `00038:105`, `00039:216`, `00045:62,259-260`, `00046:187`); no generic grant was re-added. Pre-fix attack replays exist: `tests/integration/m9r_worker_test.go:32` (self-issued claim denied 42501), `:569` (forged GUCs cannot read, update or delete an unrelated record), `:91`, `:259` (two populated tenants), `:335`, `:392`. Executed: NOT RUN (needs DB); recorded PASS in R1/R5b reports. Residual: the worker still holds `UPDATE(state_id,status,...)` on its leased instance (`00020:38`), see N-02 |
| F02 Normal roles own tables / broad worker grants | NOLOGIN ownership retained; worker grants and drift admission hardened | Regression required for each new table and function | CLOSED; guard hardening is a LOW residual (N-14) | Runtime admission refuses a role that owns schemas/tables/functions, holds CREATE, is a member of an owner role or has unsafe table grants: `internal/platform/postgres/postgres.go:50-76`, run on every transaction (`internal/platform/postgres/transaction.go:84`) and in `Ping` (`postgres.go:39`). Owner is `framework_schema_owner` NOLOGIN, checked in readiness (`internal/runtime/workflow/readiness.go:20`, `internal/runtime/data/store.go:23`). Installer is migration-only and rewrites ownership (`00027:13-35`). Tests: `tenant_data_test.go:447`, `identity_policy_test.go:269`. Unit `internal/platform/postgres` PASS here. The worker drift guard is a fixed denylist without REFERENCES/TRIGGER and misses unlisted tables such as `release_approvals` (independent M12 DB review F5; confirmed by reading `00048:182`); extended on claude/m13-approval-promotion (`00049`), not merged |
| F03 Tenant-controlled shared DDL | Generic JSONB runtime has no such path; existing ALTER denial retained | Future indexes/migrations must remain governed platform operations | CLOSED | No DDL in non-test Go: a search of `internal` and `cmd` for CREATE/ALTER/DROP TABLE, CREATE INDEX and `EXECUTE format` finds nothing. Numeric indexes use fixed shared tables maintained by triggers (`00031`), registered through definer ports granted to `framework_app` only (`00031:159-160`). `install_tenant_rls` raises unless run by migrator or schema owner (`00027:13-22`) and is revoked from runtime roles (`00027:37`). Test `tenant_data_test.go:447` (unsafe DDL and runtime execution rejected). Executed: source search only |
| F04 Records disappear across compatible compiler/release changes | R2a/R2b and [R2c preview schema evolution](M9R_R2C_IMPLEMENTATION_REPORT.md) independently PASS: immutable generation maps, checkpointed transforms, current access/tombstones and workflow data pins; actual 100k migration and post-cutover queries verified | Production active-release execution remains M12/M13; dataset-management UI, automatic index inheritance and denser/larger migration capacity remain separate work | PARTIAL | Preview-only continuity is implemented: dataset create/bind/scope `internal/runtime/data/dataset.go:45-310`, shadow evolution `shadow.go:19-307`, migrations 28 and 35-37, routes `internal/platform/access/access.go:66-84`. Tests: `m9r_dataset_test.go` (10 tests, lines 66-611), `m9r_shadow_test.go` (6 tests, lines 105-438); unit `internal/runtime/data` PASS here. Not closed: (1) the default `.../versions/{revision}` routes still key data by metadata revision and IR hash (`data.ScopeForRevision`, `preview.go:76`, `model.go:36`) and recompile per request (`preview.go:82`), so the original loss recurs unless an owner/admin explicitly creates or adopts a dataset; (2) the dataset environment CHECK admits only `preview` and `sandbox` (`00028:6`, `00046:7`): no production dataset exists; (3) no dataset-management UI. Executed: NOT RUN (DB) |
| F05 RLS membership checked once per row | Replaced by statement-level membership evaluation; populated isolation and SQL plans verified | Preserve live authorization and fail-closed RLS; HTTP latency gate remains below | CLOSED | Policies rewritten to uncorrelated `(SELECT iam.tenant_matches_scope(iam.current_tenant_id()))` and installer updated for future tables: `00027:5-35`. Fail-closed populated negatives `m9r_rls_test.go:16` and a policy-shape check `:134-139`; plan assertion (membership InitPlan `Actual Loops` = 1) in `m9r_performance_test.go:205-207,225` (build tag `performance`). Executed: NOT RUN (DB). The performance tests are not part of `make test` or CI (`Makefile:37-40`, `.github/workflows/ci.yml`): see N-09 |
| F06 End-to-end query performance | Governed numeric R3b independently PASS: 100k-row/ten-client HTTP p95 96.713 ms, zero errors, unchanged 750 ms target; earlier failures retained | Register/build a supported numeric index; general/unindexed/selective workloads remain bounded and require their own evidence | PARTIAL | The gate is met only for a registered integer/decimal/money index (`00031-00034`, `internal/runtime/data/index.go`; tests `m9r_numeric_index_test.go:88-522`, floor `m9r_performance_test.go:51`). Unindexed, text, date and selective sorts still scan JSONB; the R3a report records an independent FAIL at 899 ms before the index. Recorded PASS only; NOT RUN here, and the gate is not wired to CI (N-09) |
| F07 Shared dataset writer counter | Bounded R4 independently PASS at `94932e3`: overlapping writers/CAS/reference/fault checks and FIFO workload p95 340.575 ms, zero errors | Preserve regressions during indexing/evolution; previous failed runs retained | CLOSED | `lockDataset` now takes `FOR SHARE` on the dataset row instead of incrementing the counter (`internal/runtime/data/store.go:114-127`); the counter write remains only for adoption and index registration (`dataset.go:117`). Tests: `m9r_commands_test.go:290` (independent writers, CAS), `:239` (parent delete vs reference insert), `:208` (real deadlock retry); load floor `m9r_write_performance_test.go:74` (32 clients, tag `performance`). Recorded PASS; NOT RUN here |
| F08 Write retries / duplicate effects / unknown COMMIT | Bounded R4 independently PASS: known-abort retries, owned receipts and commit-response/browser reconciliation verified | External delivery is not covered; first historical 503 cause remains unconfirmed | CLOSED for local commands; external effects PARTIAL (N-04) | `KnownAbort` `internal/platform/postgres/retry.go:20`; `RetryLocal` at most three attempts in four seconds `retry.go:29-32`; unknown COMMIT maps to `ErrCommitUnknown` and is never retried (`internal/platform/postgres/transaction.go:120-123`); immutable receipts (migration 30). Tests `m9r_commands_test.go:58,128,339,351,389`. Unit `internal/platform/postgres` PASS here. The external tool path added later has an open HIGH from independent review: a failed lookup after an UNKNOWN send permits a blind resend (M10R-01, `docs/verification/M10_INDEPENDENT_REVIEW.md:30`), confirmed by reading `internal/runtime/workflow/tool_step.go:89-96` and `00046:169` (N-04); fix `a3b0367` is on `claude/m10-fixes`, not merged |
| F09 Late command rejection can commit a record without receipt/outbox | Independently reproduced fault and independently closed; real RLS-policy regression verifies zero business facts on rejection | Preserve whole-command rollback | CLOSED | Any local-command status >= 400 is returned as `requestRejection` so the transaction rolls back before fresh denial evidence is written (`internal/platform/access/access.go:135-142,210-214`). Regression with a real restrictive SELECT policy `m9r_commands_test.go:412`. Recorded PASS; NOT RUN here |
| F10 Same-login navigation loses an in-flight command key | Independently reproduced and closed; actor-owned pre-dispatch journal/navigation/account recovery verified | Browser-restart persistence intentionally absent | CLOSED (memory-only by design) | Actor-scoped journal above the workspace component: `apps/web/src/AccessConsole.tsx:16-20,59`; test `apps/web/src/CommandRecovery.test.tsx:41`. The journal is React state, so a reload loses it (documented). Executed: NOT RUN (frontend tests were not run in this audit) |
| F11 Custom field indexes / unique projections | Integer/decimal/money projection indexes implemented and independently verified; text/date/search and uniqueness remain open | Remaining R3b types and R3c explicit uniqueness metadata/concurrent uniqueness proof | PARTIAL: numeric closed; text/date/search and uniqueness OPEN | Numeric: see F06. `runtime.unique_projections` is created in `00009:52-58` and has no writer: no Go file references it other than grant lists, and metadata/compiler contain no uniqueness concept (searched `internal/metadata`, `internal/compiler`). R3c is not started |
| F12 Aggregates and bounded join planner | Existing explicit budgets retained; broad SQL aggregation not implemented | R3d planner/aggregation correctness and workload gates | OPEN | Aggregates are validated and computed in Go over the fetched page: `internal/runtime/data/query.go:355-373` (op/field checks) and `:463-494` (over-budget `hasMore` returns the budget error, then sum/count in Go). One join only (`query.go:299`). R3d not started |
| F13 Production active-release runtime / public promotion lifecycle | Not implemented; current preview and release contracts are explicitly qualified | M12/M13 verified candidate, authoritative approval and immutable active-release execution | OPEN at the audited head; approval/activation/rollback authority CLOSED on claude/m13-approval-promotion, not merged; production runtime OPEN everywhere | At the head: only requirement, acceptance and candidate routes exist (`internal/platform/access/releases.go:32-41`); `Store.Approve/Activate/Rollback` (`internal/promotion/store.go:20-33,69-106`) have no caller outside tests; yet `framework_app` keeps INSERT on `release_approvals` and INSERT plus UPDATE on `active_releases` with policies that check only tenant membership and, for HIGH risk, requester inequality (`00011:22-38`), so a self-approved activation is possible by raw SQL from the API role (independent M12 DB review F1; reproduced by reading the policies; N-05). On `claude/m13-approval-promotion`: `00049` drops those policies and privileges and adds definer ports `approve_release`, `reject_release`, `activate_release`, `rollback_release` with a `release_port_write` trigger and contiguous history; ADR039 section 10 states the production data plane is NOT delivered there (config still rejects workflows/events in production, `internal/config/config.go:57,67`). Dataset environment still excludes `production` (`00028:6`, `00046:7`) |
| F14 Caller-supplied risk / PASS / independence / requirement classification | Still incomplete authority integration | R6 and M12–M14 executable independent acceptance, authenticated provenance and server-derived risk | PARTIAL | Bundle 2.0.0 is assembled by the server and the API role has no INSERT on `release_bundles` (`00048:63-64`); `stage_candidate` binds tenant, app, requester, acceptance rows and authors, frozen sandbox evidence and the current base (`00048:122-161`); 1.0.0 bundles can no longer be staged (`internal/promotion/store.go:50`). The database still cannot recompute IR, diff, risk facts or the verifier report: `Verification.Outcome=="PASS"` is a claim made by the API process (independent M12 DB review F4, confirmed by reading `00048:122-128`). The documented Restore backstop did not recompute the diff (independent verifier review F1, HIGH); at the audited head this is fixed in code: `LoadCandidate` resolves the candidate's own base and calls `VerifyWithBase` (`internal/promotion/release_store.go:155-188`, `bundle2.go:641-672`, test `verify_base_test.go`), and the detail route fails closed (`releases.go:378-382`); unit `internal/promotion` PASS here. `releaseGate.AuthorizeRelease` is a constant `true` placeholder (`releases.go:46-48`, N-06). Only four requirement categories are verifiable, the rest BLOCK (M12 report); the M10 to M12 independent reviews are not complete. Approval-time recomputation exists only on claude/m13-approval-promotion |
| F15 DataScope returns ALL for permitted built-in roles | [R5a](M9R_R5A_IMPLEMENTATION_REPORT.md) independently PASS: tenant role versions/CAS, live API scopes, exact operation reconciliation, bound engines and trusted worker revocation; bootstrap roles remain broad | R5b bounded PASS: raw leased source/business ports enforce current head/actions/fields/record scopes, with per-row lease fences. [R5c-a](M9R_R5C_IMPLEMENTATION_REPORT.md) independently PASS: governed CAS-fenced record owner/team/share administration (`data.row.assign`/`data.row.share`). R5c-b team hierarchy/membership administration (ADR035) independently PASS. Field masking/partial writes (R5d), configurable approval roles (R5e) and administration UI remain pending | PARTIAL at the audited head; R5d and the R5c UI are delivered on origin/remediation/m9r-foundation, not in the audited lineage | `Native.DataScope` still returns ALL for owner, admin and member (and viewer for reads): `internal/policy/scope.go:30-41`, so the four bootstrap roles stay tenant-wide. Present and wired: versioned tenant roles (`00038`, `internal/policy/definition.go`), record owner/team/share administration (`00041-42`, `internal/platform/access/records.go`), team administration (`00043-44`, `teams.go`), SQL worker policy (`00039-40`). Tests `m9r_capabilities_test.go`, `m9r_record_access_test.go`, `m9r_team_admin_test.go`, `m9r_worker_policy_test.go`. Field handling is still whole-record: every command reads and write-checks every field (`internal/runtime/data/store.go:187-203`), so there is no masking or partial write at the head. On the remediation branch: `internal/runtime/data/patch.go` and migration `00046_m9r_field_patch_receipts.sql` (R5d), spot-checked by `git show` only. No administration UI at the head |
| F16 finance_manager / fixed four roles | Custom tenant membership/capability roles now supported through owner-authorized API | Workflow approval contract still limits role names; R5 must expand it safely | PARTIAL at the audited head; R5e delivered on origin/remediation/m9r-foundation, not in the audited lineage | Custom roles can be published and assigned (`00038:104`, `internal/platform/access/roles.go`), but workflow approval roles are still validated against the four names: `internal/compiler/workflow.go:30` and `internal/runtime/workflow/plan.go:274`. R5e (`00045_m9r_workflow_approval_roles.sql` on the remediation branch) uses the same version tokens (schema, IR, compiler 1.3.0; DSL 2.1.0) and migration numbers (45, 46) as M10 and M11 with different meaning: collision N-01 |
| F17 JWT skew and token-paste development login | Still limited | R5 provider configuration and production identity UX, with explicit token verification bounds | OPEN at the audited head; R5f delivered on origin/remediation/m9r-foundation, not in the audited lineage | Verification is strict with no leeway: `internal/identity/identity.go:144` (`Exp<=now`, `Iat>now`, `Nbf>now`; clock `time.Now` at `:59`). The only login is a pasted bearer token: `apps/web/src/AccessConsole.tsx:52`. On the remediation branch `NewVerifierWithClockSkew` (bounded to 120 s) exists in `internal/identity/identity.go` (spot-checked by `git show`) |
| F18 M8 short outage window / dead predecessor / one consumer | Still limited to documented preview delivery | R7 outage/replay controls; future tool/subscriber contracts must preserve ordering and idempotency | OPEN (R7 not started) | Delivery is still the preview receipt consumer. Five attempts then DEAD (`00022:260-262`), five-second lease and next attempt (`00022:263-264`), a dead predecessor blocks every later event of the aggregate (`00022:255`), and completion hard-codes one consumer, `preview.receipt.v1` (`00022:324`). Operator-only replay exists: `cmd/event-replay/main.go:21-43` calls `control.replay_event` (`00018:178`, granted to the migrator only, `00018:196`); there is no outage-budget retry, subscriber versioning or least-privilege replay role. Config rejects events in production (`internal/config/config.go:67`, `internal/platform/app/app.go:174`). Tests only cover the short window (`m8_delivery_test.go`, `m8_crash_test.go`). Unit `internal/events` PASS here |
| F19 Audit volume / retention / partitioning | Atomic, immutable audit retained; lifecycle not implemented | R7 measured audit policy and retention/archive design; do not delete receipts/evidence to retry | OPEN (R7 not started) | Audit is atomic and immutable: `audit_immutable` blocks UPDATE, DELETE and TRUNCATE (`00002:91-92`); decisions are batched in the same transaction (`internal/audit/batch.go:14,25`, `MaxBatchEntries=8192`). No partitioning, retention, archive or export exists: the only partition references in migrations are `relispartition` guards (`00004:69`, `00010:102`, `00027:19`). High-volume decision diagnostics are not separated from business audit. Unit `internal/audit` PASS here |
| F20 M9 DAG, noop, lease/timer/history limits and copied IR | Documented limits retained | Workflow lifecycle expansion requires explicit design, renewal/fencing tests and capacity evidence | PARTIAL: limits unchanged and documented; M10 added tool steps | Per-instance copies: IR up to 4 MiB and plan up to 1 MiB are stored in every workflow binding (`00020:12-16`). Lease 5 s (`00022:240,264`), transaction 3 s (`internal/platform/postgres/transaction.go:52`), timers capped at 24 h (`internal/runtime/workflow/plan.go:250`), default activity timeout 1000 ms (`internal/runtime/workflow/execution.go:337`). No lease renewal exists; M10 states tools longer than about 2 s are unsupported (ADR036 section 10). Registered activities are `local.noop` plus operator-catalog tools (`plan.go:269`, `tool_step.go`). The one-thousand-entry history cap named in the original review was not re-verified. Unit `internal/runtime/workflow` PASS here |
| F21 Workflow approval uses start snapshot, not current document | Existing snapshot semantics retained | Before business side effects, bind current-document version or explicitly approve a snapshot and validate freshness | PARTIAL | Ordinary approvals still approve the start snapshot: the record version is pinned at start (`internal/runtime/workflow/store.go:54,172`, `contracts.go:31`) and signals bind instance state, not the current record. Only approval-required tool steps re-check that the current record version equals the approved snapshot (`00046:150-153`, error `TL002`). That check runs only when the worker passes `needs_approval=true`, a worker-declared flag (`tool_step.go:230`; independent M10 review M10R-02 HIGH; N-02), so the freshness guarantee is not database-enforced against a compromised worker; `claude/m10-fixes` (`9f0a807`, migration `00050`) derives the gate from the pinned plan, not merged. Tests `m10_tools_test.go` (approval and freshness cases, read only) |
| F22 Rules have no consumer | Original statement corrected: preview and workflow conditions consume rule plans | Business tools remain later work; a rule result never grants authorization | CLOSED (original statement was incorrect) | Rule plans are consumed by workflow transition conditions (`internal/runtime/workflow/plan.go:117,181,304,386`) and by the preview endpoint `POST .../rules/{rule}/evaluate` (`internal/platform/access/access.go:88`, `rules.go`). A rule result is never an authorization input: `policy.Native.Decide` takes no rule argument (`internal/policy/policy.go:39-55`). Unit `internal/runtime/rules` was not re-run; `internal/runtime/workflow` PASS here |
| F23 External exactly-once and AI steps | No external tool/AI implementation or universal exactly-once claim | M10 per-tool delivery/reconciliation; AI milestones preserve deterministic authority | PARTIAL: M10 delivered, not accepted | No universal exactly-once claim: delivery follows the declared idempotency capability (`internal/runtime/workflow/tool_step.go:82-126`, catalog validation `internal/tools/catalog.go:223-249`, ADR036 decision 6). AI is not implemented (`internal/ai` holds only a README). Open defects in the external path from independent review, each confirmed by reading `tool_step.go` and `00046:169`: blind resend after a failed lookup (M10R-01 HIGH), retry budget bypass in RETRY_WAIT (M10R-03), UNKNOWN stranded in DEAD after a rejected lookup (M10R-04), lookup racing an in-flight send (M10R-05); see N-04 (fixes on `claude/m10-fixes`, not merged). Unit `internal/tools` PASS here |
| F24 Rate limiting / invalid authentication DB amplification | Still open | R7 bounded admission before expensive DB work, with safe denial evidence and abuse tests | OPEN (R7 not started) | No limiter exists in identity, tenant or HTTP packages (search for rate limit, throttle in `internal/platform`, `internal/identity`, `internal/tenant` finds none; `internal/tools/ratelimit.go` is the outbound per-tool limiter). A request opens a runtime transaction (connection, catalog guard, `set_config`) before the token is verified: `internal/platform/access/access.go:192-196`; an invalid token then follows the normal audited rejection path (`internal/platform/access/access.go:280-289`). The FIFO admission queue (`internal/platform/postgres/admission.go:32-82`, 128 waiters) is a bulkhead, not a limiter (ADR029). No abuse test exists |
| F25 Per-request compile / repeated validator construction / catalog checks | Present; read/write reference gates now pass within the stated configuration | Profile before caching; preserve immutable artifacts, live membership and role drift checks | OPEN (not profiled; by design) | Preview recompiles the source on every request (`internal/platform/access/preview.go:82`, `datasets.go:80`, `metadata.go:112`); workflow routes restore and rebuild validators per request (`access/workflows.go:57,79,191`); the role catalog guard runs in every transaction (`internal/platform/postgres/transaction.go:84`, `postgres.go:50-76`). `internal/platform/cache` is only the Redis client used for readiness. No profile or cache design is recorded; the plan asks to profile before caching, so this is correctly open |
| F26 Two conflicting rulebooks / missing supersession map | R0 map and concise agent rules implemented; source kits retained verbatim | Keep clause-level authority and current capability inventory updated | PARTIAL: map present, agent contract stale | The supersession map and capability inventory exist and were updated for M12 (`docs/migration/V2_V3_SUPERSESSION_MAP.md:6-20,31-43`). Source-kit checksums are enforced by `make docs-verify` (`Makefile:61-62`). But `AGENTS.md:3-6` still says current work is M9R and not to begin M10, and `AGENTS.md:62-65` says only `local.noop` is registered and external tools are later milestones, while operator-catalog tools run when `TOOL_CATALOG_FILE` is set (`internal/platform/app/app.go:154-165`); `CLAUDE.md` repeats the pause. The map row for V2 workflow (`V2_V3_SUPERSESSION_MAP.md:12`) still says R1 must supersede the worker-only global claim, which R1 did (N-08). Executed: `tests/security` PASS here |
| F27 No numeric verification gates / migration ownership | R2–R4 ADRs and reference targets now explicit | Execution must pass those gates; design and numbers alone are insufficient | PARTIAL | Numeric floors exist as executable tests (`Makefile:37-40`, `m9r_performance_test.go:51`, `m9r_write_performance_test.go:74`, tag `performance`) but neither `make test` nor CI runs them, and CI never runs `make release-check` (`.github/workflows/ci.yml:20-37`): the gates are enforced by hand only (N-09). Migrations are forward-only (every Down block raises, for example `00045:262-265`) but two lineages now use the same numbers 45 and 46 (N-01) |
| F28 Portability claim | Qualified as deferred; format and round-trip proof absent | M17 concrete export/import specification and executed portability verification | OPEN (scheduled M17; nothing implemented) | No export, import or manifest code exists: the module list is `ai audit compiler config erp events identity intent metadata observability platform policy promotion risk runtime sandbox tenant tools verification` and none implements it. The claim is correctly deferred in the supersession map (`V2_V3_SUPERSESSION_MAP.md:19`); stored IR plus content-addressed bundles are a precondition, not a portability proof |
| F29 CI/deployment, SSO/search/localization/residency/import/DR | Foundation CI exists; product/deployment subprojects remain scheduled gaps | Assign concrete production release deliverables; do not infer readiness from foundation tests | OPEN (scheduled; foundation CI only) | CI is one job `foundation` (`.github/workflows/ci.yml:10-37`: check, migrate, test, fuzz, build, e2e, audit); no deployment, rollback, `release-check`, performance or restore stage. SSO exists only on origin/remediation/m9r-foundation (R5f). Search, localization, residency, import and disaster recovery have no code; the only substantive runbook is `docs/migration/M9R_WORKER_RUNBOOK.md` (92 lines); `docs/runbooks/120-122` are 5 to 8 line stubs |
| F30 Large review batches / thin review / concurrent edits | Remediation uses smaller stable commits and independent executed probes | Preserve review evidence and fix findings before accepting each boundary; AI review does not replace human release ownership | PARTIAL | Smaller stable commits are visible (85 commits between `main` and the head), but M10 to M12 landed as large feature batches; the implementation reports were corrected after the first reviews to say review is PARTIAL (`docs/migration/M10_M11_IMPLEMENTATION_REPORT.md:46,50`, `M12_IMPLEMENTATION_REPORT.md:111-125`). Reviews now exist in the tree for M10 (HIGH findings open), M12 database (PASS WITH FINDINGS), M12 verifier (HIGH, IN PROGRESS), the gateway (NOT RUN in progress) and contract honesty; the M11 review (`review/m11-final`) and the M12 API review (`review/m12-api-final`) are on unmerged branches and the M11 one has no findings recorded yet. Fixes for M10R-01..05 exist on the unmerged branch `claude/m10-fixes` (`cf02e33`, migration `00050`, DSL 2.2.0) and carry their own regression tests, but have not been reviewed or merged |
| F31 Stacked unmerged branches | Latest current implementation pushed on `remediation/m9r-foundation`; main remains M6 | Pushing is not merging; no main merge or production deployment performed | OPEN | `main` and `origin/main` are still `9da3f5f` (M6); the audited head is 85 commits ahead of it. The lineage split is wider than the ledger states: `origin/remediation/m9r-foundation` (`06882b2`) carries R5c UI, R5d, R5e and R5f that are not in the M10 to M12 lineage (34 commits since the split at `e6305df`), and `claude/m13-approval-promotion` (`436c2b5`) adds M13 on top of M12. The two lines already collide on migration numbers 45/46 and on the tokens 1.3.0 and 2.1.0 (N-01). Pushing is not merging; no integration plan exists |

### Re-audit at the integrated tree `e6b7877` (verified 2026-10-19)

The table above judges `dbc4b5b` (M0 to M12). Since then the M10 review fixes (`46db427`), the M12 verifier fixes
(`a5e6292`) and the M13 control plane were merged into `claude/m13-approval-promotion` (`e6b7877`, 64 commits after
`dbc4b5b`, the commit the independent M13 review pinned). This section re-judges the rows that change and re-runs what a
database made possible. The 2026-10-17 text stays as history.

- Tree: `e6b7877`, read from the worktree `/home/user/review-m13` (branch `review/m13-final`) and with
  `git show e6b7877:<path>`. Citations in this section are to that tree, not to the working tree of this audit branch
  (which stays at `dbc4b5b` plus docs). NOT in that tree: the remediation line (`origin/remediation/m9r-foundation`
  `06882b2`, 31 commits after the split at `e6305df`, not an ancestor of `e6b7877`), the M11, M12 API and M12 DB fixes
  still being written on other branches, and M14.
- Executed this time (the MAIN stack was available; each command alone, `nice -n 10`, disposable databases, through the
  queue wrapper): `-run TestM9R` 73 tests PASS (173 s: worker authority, commands, datasets, shadow generations, numeric
  indexes, record access, team administration, worker policy); `-run 'TestM1RuntimePrivileges|TestM1ConcurrentLastOwner|
  TestM1FreshDatabase|TestM2RLSInstaller|TestM2ConcurrentTenant'` 5 PASS; `-run TestM13` 31 PASS (88 s); `-run TestM10R`
  11 PASS (84 s); unit packages promotion, verification, risk, intent, runtime/workflow, tools, policy, audit,
  platform/access, platform/postgres, metadata, compiler PASS. Still NOT RUN: the `performance`-tagged floors (F05, F06,
  F07), frontend and browser tests, and the integration files for M3 to M8 and the other M10 to M12 suites.
- Effect on "Executed: NOT RUN (DB)" / "recorded PASS" above: the correctness tests cited for F01 (`m9r_worker_test.go`),
  F02 and F03 (`TestM1RuntimePrivilegesRLSAndPoolContext`, `TestM2RLSInstallerRejectsUnsafeDDLAndRuntimeExecution`), F04
  (`m9r_dataset_test.go`, `m9r_shadow_test.go`), F05 (`TestM9RStatementMembershipRemainsFailClosed`), F07 to F09
  (`m9r_commands_test.go`), F11 (numeric index tests) and F15 (`m9r_capabilities`, `m9r_record_access`,
  `m9r_team_admin`, `m9r_worker_policy` tests) were EXECUTED and PASS at `e6b7877`. The p95 floors remain "recorded PASS".
- A mechanical check of all 230 `file:line` citations in this ledger and in the gap analysis against the audited tree
  found two out-of-range ranges and five ambiguous basenames; all were corrected (`1e42f0a`).

| Finding | Status at `dbc4b5b` | Status at `e6b7877` | Evidence at `e6b7877` |
|---|---|---|---|
| F01 | CLOSED within the DB-credential model; residuals N-02, N-03 | CLOSED within the model; N-02 CLOSED, N-03 PARTIAL | Executed PASS: `TestM9RWorkerCredentialCannotSelfIssueWorkflowLease`, `TestM9RWorkerForgedSessionCannotReadOrMutateRecords`, `TestM9RPopulatedTenantsAndAuthenticatedAudience`, `TestM9RGenericGrantDriftFailsReadinessAndStillDeniesRawData`. N-02: the database derives the approval requirement from the pinned plan (`00050:12-22,67`; DSL 2.2.0 pins `tool_contract`, `internal/runtime/workflow/plan.go:23-27,65-83`), the worker flag is ignored (`00050:50`, still sent at `internal/runtime/workflow/tool_step.go:278`); `TestM10R02WorkerCannotSkipApprovalInsideOwnLease` PASS. N-03: `runtime.guard_worker_transition` (`00050:205-216,233`) binds a worker `state_id` change to an `always` transition of the plan, but returns for an unchanged `state_id` and for any other session (`00050:208`): status, termination and failure writes by the worker are still accepted (executed by the fix re-verification, FR-01: `rows=1` for `status='SUCCEEDED'`, `status='FAILED'`, `termination='CANCELLED'`), and the API role keeps `UPDATE(state_id,status,...)` (`00020:38`) |
| F02 | CLOSED; guard hardening LOW (N-14) | CLOSED; N-14 narrowed, still LOW | `control.worker_grants_safe()` now tests SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES and TRIGGER at table and column level on 37 tables including the four release tables (`00049:252-254`); the table list of every successive definition from migration 26 to 49 is a superset of the previous one (checked mechanically). Executed by the M13 review: the function turns false for table SELECT, column SELECT, column INSERT, REFERENCES, TRIGGER, a PUBLIC TRUNCATE and a column REFERENCES grant on the new tables, and true again after revoking. It is still a fixed denylist, not an allowlist |
| F08 | CLOSED for local commands; external effects PARTIAL (N-04) | CLOSED for local commands; external effects: fixes merged and regression-tested, see F23 | M10R-01 (`a3b0367`), M10R-03 (`88198b0`), M10R-04 (`aa8fa33`), M10R-05 (`b4041d8`) are in the tree. Executed: `TestM10R01TransientLookupFailureNeverResendsUnknownWrite`, `TestM10R01DatabaseRefusesSendWhileUnknownOutstanding`, `TestM10R03*`, `TestM10R04*`, `TestM10R05*` PASS |
| F13 | OPEN at the head; authority CLOSED on the M13 branch, not merged | OPEN overall: approval, rejection, activation and rollback AUTHORITY closed with findings; production runtime OPEN | The API role has no write on `release_approvals` / `active_releases` any more and the permissive policies are gone (`00049:53-60`); the four definer ports are `00049:120-237` and the only writers; routes `internal/platform/access/promote.go:31-38`, handlers `:417-622`. Executed: `TestM13*` 31 PASS and the independent M13 review (`review/m13-final`, `docs/verification/M13_INDEPENDENT_REVIEW.md`): the database floor held against over 100 direct attempts (privileges, SET ROLE, DDL, triggers, definer shadowing, cross-tenant, forged settings, operator TRUNCATE/DELETE/UPDATE, worker roles) with 2 MEDIUM findings (a rejection can be overtaken by an activation whose transaction began earlier; the rollback class gate ignores irreversible releases between target and current), 4 LOW and 8 INFO. No production data plane: ADR039 section 10; the dataset environment still excludes `production` (`00028:6`, `00046:7`); `internal/config/config.go:57,67` still refuses workflows and events in production |
| F14 | PARTIAL | PARTIAL (narrower) | Every M13 decision loads through `Store.LoadCandidate` (`Restore` plus `VerifyWithBase`): `internal/platform/access/promote.go:246-269`, `internal/promotion/release_store.go:159-193`; the database still cannot recompute the verifier (unchanged), so a compromised API process stays bounded only by Go (documented residual of ADR039). Verifier fixes F2 to F5, F7 (`a5e6292`) re-verified by 34 variants (`docs/verification/FIX_REVERIFICATION.md` on `review/m13-final`): HELD, with one LOW residual (an exemption band between the derived probes still passes, FR-02). `releaseGate` is still a constant `true` (`internal/platform/access/releases.go:53-55`) but only `Stage` uses it (`releases.go:397`); the M13 handlers do not |
| F21 | PARTIAL; tool path not database-enforced (N-02) | PARTIAL; the tool path is now database-derived | `00050:67` (`need` from the pinned `tool_contract`, or from `tool_approval_gated` for stored 2.1.0 plans); `TestM10R02...` PASS: forged request witness (22023) and forged tool receipt (`guard_tool_receipt`, `00050:219-231`) refused. Ordinary (non-tool) approvals still approve the start snapshot (unchanged) |
| F23 | PARTIAL (M10 built, not accepted) | PARTIAL (M10 fixes merged and re-verified) | M10R-01 to M10R-05 fixed: durable `unknown_outstanding` (`00050:34-39,91-109,143-158`), attempt budget (`00050:112`), settle interval (`00050:146-155`, `internal/tools/catalog.go:61-70`), `decideTool` (`tool_step.go:115-150`, table test `internal/runtime/workflow/tool_decide_test.go:12`). Re-verification by variants (expired dedupe window, idempotency-less write, lookup answering 500, a pre-fix ledger row): HELD. Absence is proven only by the declared `settle_ms`. AI is still not implemented (`internal/ai` holds a README only) |
| F26 | PARTIAL (stale agent contract, N-08) | PARTIAL, unchanged | `git diff dbc4b5b e6b7877 -- AGENTS.md CLAUDE.md README.md docs/migration/M9R_REMEDIATION_PLAN.md docs/migration/V2_V3_SUPERSESSION_MAP.md .github Makefile internal/config` is empty: N-08, N-09 and N-10 are unchanged |
| F27 | PARTIAL | PARTIAL | Inside the M10 to M13 line the migration numbers 45 to 50 are unique (`00049` M13, `00050` M10 fixes; `00050:3` notes the gap). The remediation line still collides on 45 and 46; the numeric gates are still run by hand only |
| F30 | PARTIAL | PARTIAL | Reports now in the tree: M10 (HIGH M10R-01/02 and MEDIUM 03 to 05, all fixed and regression-tested), gateway (F1 to F4 MEDIUM fixed in `e26dfaf`; F5 to F9 LOW open), M11 (COMPLETE, PASS WITH FINDINGS: 0 HIGH, 3 MEDIUM, 4 LOW; fixes in progress), M12 DB (PASS WITH FINDINGS), M12 verifier (F1 to F5, F7 fixed and re-verified), M12 API (PASS WITH FINDINGS; F9 MEDIUM, the rest LOW; fixes in progress), the contract honesty audit, and on `review/m13-final` the M13 review and the fix re-verification. M14 independent review NOT RUN. M10 to M13 landed in large batches and the independent reviews found HIGH defects in M10 and M12 that the builders' own suites had not |
| F31 | OPEN | OPEN | `origin/main` is still `9da3f5f` (M6); `e6b7877` is 147 commits ahead. The M10 to M13 line (`e6b7877`) and the remediation line (`06882b2`: R5c UI, R5d, R5e, R5f; 31 commits after the split at `e6305df`) are still unmerged and still collide on migrations 45/46, ADR numbers 036 to 039 and the tokens `1.3.0` / `2.1.0` (N-01). Nothing is pushed |

Rows not listed (F03, F04 to F07, F09 to F12, F15 to F20, F22, F24, F25, F28, F29) keep their 2026-10-17 status; for F01 to
F05, F07 to F09, F11 and F15 the correctness tests behind them now have an EXECUTED PASS at `e6b7877` (see above). Counting
the 31 rows at `e6b7877` with the same rule gives the same totals as before: CLOSED 9, PARTIAL 12, OPEN 10 (F13 stays OPEN
because the production runtime does not exist; its authority half is closed with findings).

### Remediation slice status (audited 2026-10-17)

Same baseline and verdict words as above. "Remediation branch" means
`origin/remediation/m9r-foundation` (`06882b2`), whose code is not in the audited tree.

| Slice | Audit status | Evidence and what remains |
|---|---|---|
| R0 scope and ownership | PARTIAL | Map and inventory exist (`docs/migration/V2_V3_SUPERSESSION_MAP.md:6-43`) and `make docs-verify` pins the source kits (`Makefile:61-62`). The concise agent contract is stale against M10 to M12 (`AGENTS.md:3-6,62-65`, `CLAUDE.md`, `README.md:6-14`): N-08 |
| R1 worker authority | CLOSED | F01 and F02. Residuals N-02, N-03 (M10 surface), N-14 |
| R2a pure schema plans | CLOSED | `internal/compiler/storage.go` with `storage_test.go`; `internal/runtime/data/evolution.go` with `evolution_test.go:39,76,115` (unit, PASS here) |
| R2b equal-schema bindings | CLOSED (preview) | `internal/runtime/data/dataset.go:61-310`, migrations 28-29, `m9r_dataset_test.go:66-611` (recorded PASS) |
| R2c shadow generations | CLOSED (preview) | `internal/runtime/data/shadow.go:19-307`, migrations 35-37, `m9r_shadow_test.go:105-438` (recorded PASS). Capacity of larger or denser migrations unmeasured |
| R2 as a whole | PARTIAL | F04: default routes keep the hash-keyed namespace, no production dataset, no UI |
| R3a authorization evaluation | CLOSED | F05. The recorded current-host HTTP gate flipped between FAIL and PASS across runs (R3a report); the gate is not in CI (N-09) |
| R3b governed numeric indexes | CLOSED | F06, `00031-00034`, `m9r_numeric_index_test.go:88-522` (recorded PASS) |
| R3b other types (text, date, search) | OPEN | F11 |
| R3c uniqueness | OPEN | F11: `runtime.unique_projections` has no writer |
| R3d aggregates and join planner | OPEN | F12 |
| R4 concurrent writes | CLOSED (local commands) | F07 to F10 |
| R5a tenant capabilities | CLOSED | `00038`, `internal/policy/definition.go`, `m9r_capabilities_test.go`; unit `internal/policy` PASS here |
| R5b worker SQL capability | CLOSED | `00039-00040`, `m9r_worker_policy_test.go:106,214,264,347` (recorded PASS); receipt-scan timing margin about 67 ms (R5b report) |
| R5c-a record access administration | CLOSED | `00041-00042`, `internal/platform/access/records.go`, `m9r_record_access_test.go` |
| R5c-b team administration | CLOSED | `00043-00044`, `internal/platform/access/teams.go`, `m9r_team_admin_test.go` |
| R5c UI (record and team administration) | OPEN at the audited head | Delivered on the remediation branch (`c993362`), not merged |
| R5d field masking and partial writes | OPEN at the audited head | Whole-record reads and writes remain (`internal/runtime/data/store.go:187-203`). Delivered on the remediation branch (`88c8528`, `internal/runtime/data/patch.go`), not merged |
| R5e tenant workflow approval roles | OPEN at the audited head | Four fixed role names remain (`internal/compiler/workflow.go:30`, `internal/runtime/workflow/plan.go:274`). Delivered on the remediation branch (`44a3979`), not merged; version collision N-01 |
| R5f identity (clock tolerance, OIDC sign-in) | OPEN at the audited head | `internal/identity/identity.go:144` strict timestamps; token paste login. Delivered on the remediation branch (`7112cd0`, `c027809`), not merged |
| R6 requirement and verification authority | PARTIAL | M12 builder, requirements-v2, acceptance-v2, verifier-v1 and risk policy are present with unit tests (PASS here for `intent`, `verification`, `risk`, `promotion`); approval and activation authority only on `claude/m13-approval-promotion`; M14 capture not started (`internal/ai` holds a README only); the verifier is not an independent credential (N-12) |
| R7 reliability and operations | OPEN | Outage-budget retry, subscriber versioning (F18), lease renewal and revise cycles (F20), rate limiting (F24), audit retention (F19), profiling (F25), restore drill and RPO/RTO (F29): none implemented. External tool semantics are implemented but defective (F23, N-04) |

Slices at `e6b7877` (2026-10-19): R1 stays CLOSED (N-02 closed by `00050`, N-03 PARTIAL, N-14 LOW); R6 stays PARTIAL
(M12 builder and verifier fixes plus the M13 approval and activation authority are in the integrated tree, with the M13
findings M13R-01 and M13R-02 open; the verifier still runs in the stager's trust domain; M14 capture not started);
R7 stays OPEN (the external tool defects that made it "implemented but defective" are fixed, F23; outage budget, rate
limiting, audit retention, profiling and restore drill are unchanged). All other slices keep their status.
