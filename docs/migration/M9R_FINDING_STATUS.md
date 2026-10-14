# External review finding status

Updated 14 October 2026 (IST). Baseline review: `81b88d2`; latest published
published checkpoint at the start of this remediation: `a5cbafd`. Subsequent
rollback/recovery fixes and FIFO capacity correction are detailed in the R4 report.
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
| Records disappear across compatible compiler/release changes | R2a plans/transforms and R2b explicit equal-schema dataset bindings independently PASS | Full R2 schema generations, transformation executor, canonical live access facts and workflow data pins remain open |
| RLS membership checked once per row | Replaced by statement-level membership evaluation; populated isolation and SQL plans verified | Preserve live authorization and fail-closed RLS; HTTP latency gate remains below |
| End-to-end query performance | R3a PARTIAL: current-host HTTP p95 gate failed despite passing SQL/correctness checks | Profile and fix the measured bottleneck; keep the 750 ms reference target |
| Shared dataset writer counter | R4 removes ordinary counter updates; overlapping writers/CAS/reference/fault checks pass; FIFO root capacity run p95 297.404 ms with zero errors | Independent corrected-code review/workload remains open; previous failed runs retained |
| Write retries / duplicate effects / unknown COMMIT | R4 adds narrowly classified bounded retries and immutable owned receipts; lost-COMMIT reply and browser reconciliation executed | R4 remains PARTIAL until independent acceptance; first 503 cause unconfirmed |
| Late command rejection can commit a record without receipt/outbox | Independently reproduced fault; rollback fix and real RLS-policy regression pass in root tests | Independent correction re-verification pending |
| Same-login navigation loses an in-flight command key | Independently reproduced; pre-dispatch actor-owned console journal and navigation guard pass root disconnect/account/workspace recovery tests | Independent correction re-verification pending; browser-restart persistence intentionally absent |
| Custom field indexes / unique projections | Not implemented | R3b/R3c governed projections, explicit metadata semantics and concurrent uniqueness proof |
| Aggregates and bounded join planner | Existing explicit budgets retained; broad SQL aggregation not implemented | R3d planner/aggregation correctness and workload gates |
| Production active-release runtime / public promotion lifecycle | Not implemented; current preview and release contracts are explicitly qualified | M12/M13 verified candidate, authoritative approval and immutable active-release execution |
| Caller-supplied risk / PASS / independence / requirement classification | Still incomplete authority integration | R6 and M12–M14 executable independent acceptance, authenticated provenance and server-derived risk |
| DataScope returns ALL for permitted built-in roles | SQL scope machinery exists; product policy remains incomplete | R5 versioned capabilities, custom roles, ownership/team/share management and field semantics |
| finance_manager / fixed four roles | Still limited | R5 identity/policy and workflow approval contract changes; never trust token role claims |
| JWT skew and token-paste development login | Still limited | R5 provider configuration and production identity UX, with explicit token verification bounds |
| M8 short outage window / dead predecessor / one consumer | Still limited to documented preview delivery | R7 outage/replay controls; future tool/subscriber contracts must preserve ordering and idempotency |
| Audit volume / retention / partitioning | Atomic, immutable audit retained; lifecycle not implemented | R7 measured audit policy and retention/archive design; do not delete receipts/evidence to retry |
| M9 DAG, noop, lease/timer/history limits and copied IR | Documented limits retained | Workflow lifecycle expansion requires explicit design, renewal/fencing tests and capacity evidence |
| Workflow approval uses start snapshot, not current document | Existing snapshot semantics retained | Before business side effects, bind current-document version or explicitly approve a snapshot and validate freshness |
| Rules have no consumer | Original statement corrected: preview and workflow conditions consume rule plans | Business tools remain later work; a rule result never grants authorization |
| External exactly-once and AI steps | No external tool/AI implementation or universal exactly-once claim | M10 per-tool delivery/reconciliation; AI milestones preserve deterministic authority |
| Rate limiting / invalid authentication DB amplification | Still open | R7 bounded admission before expensive DB work, with safe denial evidence and abuse tests |
| Per-request compile / repeated validator construction / catalog checks | Present; their measured contribution is not yet established for the failed write gate | Profile before caching; preserve immutable artifacts, live membership and role drift checks |
| Two conflicting rulebooks / missing supersession map | R0 map and concise agent rules implemented; source kits retained verbatim | Keep clause-level authority and current capability inventory updated |
| No numeric verification gates / migration ownership | R2–R4 ADRs and reference targets now explicit | Execution must pass those gates; design and numbers alone are insufficient |
| Portability claim | Qualified as deferred; format and round-trip proof absent | M17 concrete export/import specification and executed portability verification |
| CI/deployment, SSO/search/localization/residency/import/DR | Foundation CI exists; product/deployment subprojects remain scheduled gaps | Assign concrete production release deliverables; do not infer readiness from foundation tests |
| Large review batches / thin review / concurrent edits | Remediation uses smaller stable commits and independent executed probes | Preserve review evidence and fix findings before accepting each boundary; AI review does not replace human release ownership |
| Stacked unmerged branches | Latest current implementation pushed on `remediation/m9r-foundation`; main remains M6 | Pushing is not merging; no main merge or production deployment performed |

## Execution order and stop gate

Close R4's actual failed workload and independent findings, complete full R2
dataset evolution, and close R3's failed HTTP floor. Keep governed indexing,
uniqueness and aggregates as explicit R3b–R3d work rather than claiming them from
the membership fix. R5–R7 remain separate product/security/operations deliverables.
Production, AI and external ERP actions are not enabled by a foundation PASS.

M10 stays paused until the foundation gate and final backend/frontend/integration/
browser regressions pass. Do not erase failed runs, weaken assertions or substitute
contract-only tests for wired product behavior.
