# External review finding status

Current checkpoint, 18 October 2026: [R3c unique-field integrity](M9R_R3C_IMPLEMENTATION_REPORT.md)
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
| Custom field indexes / unique projections | Integer/decimal/money, string/civil-date, enum/reference, datetime/boolean and fixed plain full-text indexes plus [R3c typed unique-field enforcement](M9R_R3C_IMPLEMENTATION_REPORT.md) independently verified; source JSONB, existing records, old readers/cursors and immutable releases retained. Populated backfill, conflict rollback, private permits, raw roles, concurrent claims/receipt replay and 100k/32-client gates pass | Uniqueness UI/READY removal/constraint-preserving evolution and search UI/ranking/languages/broad-term capacity remain separate; aggregates/planning remain R3d |
| Aggregates and bounded join planner | Existing explicit budgets retained; broad SQL aggregation not implemented | R3d planner/aggregation correctness and workload gates |
| Production active-release runtime / public promotion lifecycle | Not implemented; current preview and release contracts are explicitly qualified | M12/M13 verified candidate, authoritative approval and immutable active-release execution |
| Caller-supplied risk / PASS / independence / requirement classification | Still incomplete authority integration | R6 and M12–M14 executable independent acceptance, authenticated provenance and server-derived risk |
| DataScope returns ALL for permitted built-in roles | [R5a](M9R_R5A_IMPLEMENTATION_REPORT.md) independently PASS: tenant role versions/CAS, live API scopes, exact operation reconciliation, bound engines and trusted worker revocation; bootstrap roles remain broad | R5b bounded PASS: raw leased source/business ports enforce current head/actions/fields/record scopes, with per-row lease fences. [R5c-a](M9R_R5C_IMPLEMENTATION_REPORT.md) independently PASS: governed CAS-fenced record owner/team/share administration (`data.row.assign`/`data.row.share`). R5c-b team hierarchy/membership administration (ADR035) independently PASS. [R5e](M9R_R5E_IMPLEMENTATION_REPORT.md) custom workflow approval roles and legacy compatibility independently verified, including the closed audit-context follow-up; final full checks/tests/build and browser5/5 PASS. [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md) field masking/partial writes now pass bounded independent and final full/browser/performance checks; [R5c record/team UI](M9R_R5C_UI_IMPLEMENTATION_REPORT.md) passes independent/full/browser gates; R5f provider sign-in/clock tolerance now passes bounded independent/full checks; external provider deployment remains unverified and role-definition editing remains API-only |
| finance_manager / fixed four roles | Tenant capability roles and opt-in DSL2.1/IR1.3 custom workflow approvers implemented and independently verified; original1.2 reader/hash retained | Final full checks/tests/build PASS (integration629.247s) and browser5/5 PASS. Legacy schemas retain their declared four-role contract; role references do not provision authority. Record/team administration UI now has bounded independent/full/browser evidence; role-definition UI remains separate |
| Whole-request field denial / unrelated fields required for every update | [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md) bounded PASS: SQL masking, explicit partial writes and strict pinned-input guards independently reviewed; three demonstrated HIGH findings fixed; final full-suite/build/browser/performance checks passed | Preserve trusted owner/snapshot proofs, opaque 32-header bounds, condition-write restrictions and hidden-size reservations; record/team administration UI now has bounded independent/full/browser evidence. The intermittent connection failure remains an open R7 diagnostic item |
| JWT skew and token-paste development login | [R5f](M9R_R5F_IMPLEMENTATION_REPORT.md) bounded PASS: configured public-client OIDC/PKCE, pinned ID/API token validation, one-time browser-bound Redis flows, explicit 0–120s tolerance and provider-first memory-only browser connection. Independent cookie-race finding closed; full regression passed | External provider/client interoperability, production gateway/query-log verification and MFA assurance remain release gates. No refresh/provider logout, implicit enrollment, SAML or SCIM. Manual token entry is development-labelled and hidden by the production descriptor |
| Intermittent workflow fixture connection failure during R5d verification | One full run failed with `driver: bad connection` while advancing an approval fixture; the unchanged test then passed three focused repetitions | R7 connection/deadline diagnostics and repeatability follow-up; cause unconfirmed. Preserve the failed run and final whole-suite evidence in the R5d report; do not describe reruns as a reliability fix |
| Worker policy receipt-scan capacity | R5c UI final regression hit the unchanged 2s statement timeout during EXPLAIN on the existing 96-receipt/63-node policy fixture; preceding query 1657.145ms. Three isolated unchanged-budget repetitions then passed (16.066s total), as did the final full rerun (worker first query 1146.412ms). Historical R5b already documented narrow timing headroom | R7 profile statement/identity/lease/anti-join costs and repeatability under representative load. Preserve first failure; aggregate suite timeout adjustment is separate and no capacity fix is claimed |
| M8 short outage window / dead predecessor / one consumer | Still limited to documented preview delivery | R7 outage/replay controls; future tool/subscriber contracts must preserve ordering and idempotency |
| Audit volume / retention / partitioning | Atomic, immutable audit retained; lifecycle not implemented | R7 measured audit policy and retention/archive design; do not delete receipts/evidence to retry |
| M9 DAG, noop, lease/timer/history limits and copied IR | Documented limits retained | Workflow lifecycle expansion requires explicit design, renewal/fencing tests and capacity evidence |
| Workflow approval uses start snapshot, not current document | Existing snapshot semantics retained | Before business side effects, bind current-document version or explicitly approve a snapshot and validate freshness |
| Rules have no consumer | Original statement corrected: preview and workflow conditions consume rule plans | Business tools remain later work; a rule result never grants authorization |
| External exactly-once and AI steps | No external tool/AI implementation or universal exactly-once claim | M10 per-tool delivery/reconciliation; AI milestones preserve deterministic authority |
| Rate limiting / invalid authentication DB amplification | Still open | R7 bounded admission before expensive DB work, with safe denial evidence and abuse tests |
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
Bounded R3c uniqueness now also passes independent and final root gates. Broader
search and aggregates stay explicit R3b/R3d work; uniqueness administration/removal/
evolution remain separate. The bounded search API does not complete product search
or general query capacity. R5–R7 remain separate
product/security/operations deliverables. R5b closes the bounded raw-worker
capability gap; its synthetic receipt test has a narrow timeout margin and is
not a wider capacity guarantee. The independent report records which final
checks used existing tests and which new probes were not run.
Production, AI and external ERP actions are not enabled by a foundation PASS.

M10 stays paused until the foundation gate and final backend/frontend/integration/
browser regressions pass. Do not erase failed runs, weaken assertions or substitute
contract-only tests for wired product behavior.
