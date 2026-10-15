# External review finding status

Updated 15 October 2026 (IST); R5c-a added 15 October 2026 (UTC). Baseline review: `81b88d2`; source corrections
through `a5bba7b` include R2c preview evolution, bounded R5a capabilities and
[R5b worker SQL authority](M9R_R5B_IMPLEMENTATION_REPORT.md) on `remediation/m9r-foundation`. Rollback/recovery
and FIFO evidence are in the R4 report; governed numeric indexes and corrected
regressions are in the [R3b report](M9R_R3B_IMPLEMENTATION_REPORT.md).
R5c server administration and [R5e custom approval roles](M9R_R5E_IMPLEMENTATION_REPORT.md)
are now independently verified; their source/tests at `f1e2e74` passed full
checks/build and browser regressions. [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md)
source at `a4d6054` and fixture correction at `3466759` passed independent
adversarial review; its final whole-suite/build/browser/performance gate is running.
Full product R5/M9R remains PARTIAL.
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
| End-to-end query performance | Governed numeric R3b independently PASS: 100k-row/ten-client HTTP p95 96.713 ms, zero errors, unchanged 750 ms target; earlier failures retained | Register/build a supported numeric index; general/unindexed/selective workloads remain bounded and require their own evidence |
| Shared dataset writer counter | Bounded R4 independently PASS at `94932e3`: overlapping writers/CAS/reference/fault checks and FIFO workload p95 340.575 ms, zero errors | Preserve regressions during indexing/evolution; previous failed runs retained |
| Write retries / duplicate effects / unknown COMMIT | Bounded R4 independently PASS: known-abort retries, owned receipts and commit-response/browser reconciliation verified | External delivery is not covered; first historical 503 cause remains unconfirmed |
| Late command rejection can commit a record without receipt/outbox | Independently reproduced fault and independently closed; real RLS-policy regression verifies zero business facts on rejection | Preserve whole-command rollback |
| Same-login navigation loses an in-flight command key | Independently reproduced and closed; actor-owned pre-dispatch journal/navigation/account recovery verified | Browser-restart persistence intentionally absent |
| Custom field indexes / unique projections | Integer/decimal/money projection indexes implemented and independently verified; text/date/search and uniqueness remain open | Remaining R3b types and R3c explicit uniqueness metadata/concurrent uniqueness proof |
| Aggregates and bounded join planner | Existing explicit budgets retained; broad SQL aggregation not implemented | R3d planner/aggregation correctness and workload gates |
| Production active-release runtime / public promotion lifecycle | Not implemented; current preview and release contracts are explicitly qualified | M12/M13 verified candidate, authoritative approval and immutable active-release execution |
| Caller-supplied risk / PASS / independence / requirement classification | Still incomplete authority integration | R6 and M12–M14 executable independent acceptance, authenticated provenance and server-derived risk |
| DataScope returns ALL for permitted built-in roles | [R5a](M9R_R5A_IMPLEMENTATION_REPORT.md) independently PASS: tenant role versions/CAS, live API scopes, exact operation reconciliation, bound engines and trusted worker revocation; bootstrap roles remain broad | R5b bounded PASS: raw leased source/business ports enforce current head/actions/fields/record scopes, with per-row lease fences. [R5c-a](M9R_R5C_IMPLEMENTATION_REPORT.md) independently PASS: governed CAS-fenced record owner/team/share administration (`data.row.assign`/`data.row.share`). R5c-b team hierarchy/membership administration (ADR035) independently PASS. [R5e](M9R_R5E_IMPLEMENTATION_REPORT.md) custom workflow approval roles and legacy compatibility independently verified, including the closed audit-context follow-up; final full checks/tests/build and browser5/5 PASS. Field masking/partial writes (R5d) and administration UI remain pending |
| finance_manager / fixed four roles | Tenant capability roles and opt-in DSL2.1/IR1.3 custom workflow approvers implemented and independently verified; original1.2 reader/hash retained | Final full checks/tests/build PASS (integration629.247s) and browser5/5 PASS. Legacy schemas retain their declared four-role contract; role references do not provision authority. Product administration UI remains separate |
| Whole-request field denial / unrelated fields required for every update | [R5d](M9R_R5D_IMPLEMENTATION_REPORT.md) SQL masking, explicit partial writes and strict pinned-input guards passed independent adversarial review; three demonstrated HIGH findings fixed | Final full-suite/build/browser/performance verification pending. Preserve trusted owner/snapshot proofs, opaque 32-header bounds, condition-write restrictions and hidden-size reservations; administration UI is separate |
| JWT skew and token-paste development login | Still limited | R5 provider configuration and production identity UX, with explicit token verification bounds |
| Intermittent workflow fixture connection failure during R5d verification | One full run failed with `driver: bad connection` while advancing an approval fixture; the unchanged test then passed three focused repetitions | R7 connection/deadline diagnostics and repeatability follow-up; cause unconfirmed. Preserve the failed run and final whole-suite evidence in the R5d report; do not describe reruns as a reliability fix |
| M8 short outage window / dead predecessor / one consumer | Still limited to documented preview delivery | R7 outage/replay controls; future tool/subscriber contracts must preserve ordering and idempotency |
| Audit volume / retention / partitioning | Atomic, immutable audit retained; lifecycle not implemented | R7 measured audit policy and retention/archive design; do not delete receipts/evidence to retry |
| M9 DAG, noop, lease/timer/history limits and copied IR | Documented limits retained | Workflow lifecycle expansion requires explicit design, renewal/fencing tests and capacity evidence |
| Workflow approval uses start snapshot, not current document | Existing snapshot semantics retained | Before business side effects, bind current-document version or explicitly approve a snapshot and validate freshness |
| Rules have no consumer | Original statement corrected: preview and workflow conditions consume rule plans | Business tools remain later work; a rule result never grants authorization |
| External exactly-once and AI steps | No external tool/AI implementation or universal exactly-once claim | M10 per-tool delivery/reconciliation; AI milestones preserve deterministic authority |
| Rate limiting / invalid authentication DB amplification | Still open | R7 bounded admission before expensive DB work, with safe denial evidence and abuse tests |
| Per-request compile / repeated validator construction / catalog checks | Present; read/write reference gates now pass within the stated configuration | Profile before caching; preserve immutable artifacts, live membership and role drift checks |
| Two conflicting rulebooks / missing supersession map | R0 map and concise agent rules implemented; source kits retained verbatim | Keep clause-level authority and current capability inventory updated |
| No numeric verification gates / migration ownership | R2–R4 ADRs and reference targets now explicit | Execution must pass those gates; design and numbers alone are insufficient |
| Portability claim | Qualified as deferred; format and round-trip proof absent | M17 concrete export/import specification and executed portability verification |
| CI/deployment, SSO/search/localization/residency/import/DR | Foundation CI exists; product/deployment subprojects remain scheduled gaps | Assign concrete production release deliverables; do not infer readiness from foundation tests |
| Large review batches / thin review / concurrent edits | Remediation uses smaller stable commits and independent executed probes | Preserve review evidence and fix findings before accepting each boundary; AI review does not replace human release ownership |
| Stacked unmerged branches | Latest current implementation pushed on `remediation/m9r-foundation`; main remains M6 | Pushing is not merging; no main merge or production deployment performed |

## Execution order and stop gate

Bounded R4 and the configured numeric R3b HTTP gate are independently verified.
R2c preview dataset evolution now passes independent fault/capacity tests, the
full root suite/build and browser regressions. Preserve those regressions. Remaining
index types, uniqueness and aggregates stay explicit R3b–R3d work rather than
being claimed from the membership/numeric fix. R5–R7 remain separate
product/security/operations deliverables. R5b closes the bounded raw-worker
capability gap; its synthetic receipt test has a narrow timeout margin and is
not a wider capacity guarantee. The independent report records which final
checks used existing tests and which new probes were not run.
Production, AI and external ERP actions are not enabled by a foundation PASS.

M10 stays paused until the foundation gate and final backend/frontend/integration/
browser regressions pass. Do not erase failed runs, weaken assertions or substitute
contract-only tests for wired product behavior.
