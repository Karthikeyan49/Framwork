# V2/V3 authority and capability status

The source kits remain checksum-frozen. This map supersedes specific clauses,
not whole retained V2 files. Current user scope overrides historical prompts.

| Retained source / clause | Governing replacement | Owner / gate |
|---|---|---|
| V2 `implementation/50_IMPLEMENTATION_ROADMAP.md`: M8 workflow, M10 outbox | V3 `roadmap/50_RESTRUCTURED_MILESTONES.md`: M8 outbox, M9 workflow, M10 tools | Architecture; preserve history; M9R foundation gate before M10 |
| V2 `specs/50_PROMOTION_PROTOCOL.md` steps 7–8: approve then hash | V3 `architecture/12_APPROVAL_BINDING.md`: build and verify final bundle before approval; activate exact approved hash | M12 builder/M13 promotion; current hash/CAS contracts are not a public authority service |
| V2 `architecture/14_DATA_ARCHITECTURE.md`: generated/promoted custom columns | V3 `architecture/10_DYNAMIC_STORAGE_MODEL.md`: JSONB plus governed projections; typed Domain Packs | Data/database; no tenant-triggered shared DDL; R2/R3 continuity/index gates |
| V2 `specs/57_BACKUP_DR.md`: restore recompiles active metadata | V3 `architecture/11_VERSION_COMPATIBILITY_MODEL.md`: restore immutable stored IR without recompilation | Compiler/operations; R2 compatibility/M15 restore drill; do not follow old recompilation instruction |
| V2 `specs/47_WORKFLOW_MODEL.md`, `risks/28_WORKFLOW_RELIABILITY.md`: broad workflow goals | V3 versions and ADR023 define the bounded preview implementation | Workflow/security; R1 must supersede ADR023 worker-only global claims; current limits remain explicit |
| V2 `specs/52_POLICY_ENGINE.md`: broad RBAC/ABAC | V3 `architecture/14_DATASCOPE_AUTHORIZATION.md`: SQL scope plus action policy | Policy/data R5; Native built-in roles remain tenant-wide |
| V2 `specs/53_AI_BUILDER.md`, `risks/25_AI_CORRECTNESS.md`: generated checks | V3 `architecture/13_INTENT_CONTRACT.md`, `17_AI_WORKFLOW_TRUST_MODEL.md`: independent acceptance; AI is not authority | M14 captures, M12 verifies, M13 enforces evidence/approval |
| V2 `risks/29_SIDE_EFFECTS_IDEMPOTENCY.md`, `specs/48_TOOL_REGISTRY.md` | V3 `architecture/16_TOOL_DELIVERY_SEMANTICS.md`: per-tool guarantees/reconciliation | M10; no universal external exactly-once |
| V2 `specs/49_SANDBOX_MODEL.md`, `implementation/55_SANDBOX_IMPLEMENTATION.md` | V3 `architecture/18_SANDBOX_ISOLATION.md`: separate data and capabilities | M11; contracts are not an orchestrated service |
| V2 `risks/32_VERSION_EXPLOSION.md` | V3 `architecture/19_APPLICATION_PATCH_REBASE.md`: base/hash and governed three-way conflicts | Metadata/compiler/M12; sensitive conflicts require review |
| V2 `specs/51_EVENT_OUTBOX.md`, `55_AUDIT_LOG.md` | V3 `integrations/41_TRANSACTIONAL_AUDIT_OUTBOX.md`: coordinated mutation/audit/outbox | Events/data; preserve atomicity during concurrent-write changes |
| V2 `risks/38_LOCKIN_EXPORTABILITY.md`: portability goals | V3 M17 owns concrete format and import/export round-trip proof | Integrations; documentation is not portability evidence |
| V2 `implementation/58_PERFORMANCE_BUDGETS.md`: measure before numeric SLOs | Retained; M9R proposes reference-environment budgets based on baseline | Performance/QA; proposed numbers are not achieved guarantees |

Unchanged V2 detail remains applicable: Go modular monolith, React/TypeScript,
trusted tenant context, parameterized typed queries, configuration, telemetry,
module contracts and controlled migrations. Typed Domain Packs retain business
invariants; dynamic JSONB does not replace their posting/stock semantics.

## Capability inventory

Baseline: `81b88d2`. Later remediation must cite its verified commit/evidence.

| Capability | Baseline status | Remaining gate |
|---|---|---|
| Repository, CI, local services, telemetry | Working foundation | Production deployment/operations |
| Identity and four built-in roles | Wired preview/development | R5 provider UX/custom roles |
| Tenant RLS/API policy | Wired, existing test coverage | R1 compromised-worker authority fails |
| Registry/compiler/data/UI | Wired preview subset | R2 stable persistence; R3/R4 budgets/concurrency |
| Typed rule DSL | Used by preview and workflow conditions | Preserve exact math, pinned time data and budgets |
| M8 delivery | Wired preview receipt consumer | R7 outage/replay/retention; future subscribers |
| M9 workflows | Wired bounded DAG/noop preview | R1 boundary; later lifecycle/approval semantics |
| OWNER/TEAM/SHARE/CONDITION | Tested SQL adapter | R5 product policy/administration |
| Release hashes/CAS/compatibility | M12 server-built candidates (bundle 2.0.0) staged through a definer port; M13 approval, rejection, activation and rollback through definer ports with a database-enforced floor (preview control plane: no production data plane, no storage execution); the operator compatibility gate remains | M13 production data plane and storage execution (M13c); independent reviews of M12 and M13 are complete with findings fixed, the latest fixes are not independently re-verified |
| Intent/acceptance/sandbox/tool delivery | M10 governed tools, M11 sandboxes and M12 deterministic acceptance verifier (`verifier-v2`) implemented for preview; their independent reviews are complete and the findings fixed (re-verification of the M10 and verifier fixes held) | M14 AI builder and intent capture implemented as a preview (proposals only, human apply/confirm; no real model called; independent review NOT RUN); a deterministic verifier is not a separate credential |
| AI, production ERP packs, portability | Deferred | Explicit milestones and executed verification |

Read the [findings](../verification/M0_M9_REVIEW_VALIDATION.md) and
[remediation plan](M9R_REMEDIATION_PLAN.md). Historical PASS reports certify only
their tested scope. Contract placeholders and unexecuted test labels must never
be reported as wired, verified product capabilities.
