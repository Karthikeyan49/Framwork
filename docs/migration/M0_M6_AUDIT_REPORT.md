# Existing M0–M6 → V3 compatibility audit

2026-10-12. Baseline9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7, clean main,
now isolated on migration/v3-m6r. V3 kit44files/43listed SHA256 values verified;
all Markdown read before this audit. No obsolete V1/V2 archives found/deleted.
Existing V2 specifications remain preserved, subordinate where V3 supersedes them.
Schema-only checkpoint and whole legacy-record count/checksum are retained privately
under ignored.local/m6r. Existing sample metadata/tests and commit provide code
recovery. No existing data/table/migration is deleted or rewritten.

| Subsystem | Classification | Current evidence and required action |
| --- | --- | --- |
| M0 repository/CI/bootstrap/OTel/Go+React | KEEP + PATCH | Make check/test/build, pinned tools, migrations00001–00008 and owned smoke pass. Extend checks to V3 manifests and new worker bindings; preserve shells/logging/probes/Redis. |
| M1 identity/tenant | KEEP + PATCH | Pinned RS256, opaque Principal/User/Access, server membership, atomic audit. Add live team/hierarchy membership port and job-delegated tenant flow; no token-supplied roles/teams. |
| M2 PostgreSQL/RLS | KEEP + PATCH | WithRuntime checks exactlease, clears scope, repeatable transaction, forcedRLS and owner/role drift tests. Add job context cleanup and role-specific worker guard; controlled NOLOGIN ownership transition. |
| Database/dynamic physical storage | KEEP + PATCH | runtime.preview_records is already generic typed-core+JSONB, scoped tenant/app/revision/IR/entity/UUID. No per-entitytables/EAV/customer ALTER TABLE. Introduce public RecordStore contract and V3 adapter; additive copy with equivalence checks, retain read-only legacy adapter. |
| M3 metadata entities/fields/relationships | KEEP + PATCH | Closed JSON/YAML schemas, stableIDs, immutable revision/CAS/hash, no SQL/DDL. Keep version1 compatibility; add separate V3 intent/patch/release contracts, no destructive schema changes. |
| M4 compiler/canonical IR | KEEP + PATCH | Private immutable Program, golden/order/hash/reference/type checks. Preview recompilation is allowed; no production exists. Add storedIR restore and supported-version contract so production never recompiles. Align UI budgets/emptyentity admission. |
| M5 query/data authorization | REFACTOR | SQLparameterization/CAS/references/query budgets are sound. Unfiltered candidate preflight and per-row callbacks violate V3 SQLDataScope requirement. Replace with typed SQL predicates for ownership/teams/shares/hierarchy/conditions and enforce mutations/joins/aggregates. Failclosed for engines lacking scope contracts. |
| M6 semantic renderer | KEEP + PATCH | Uses entity/field IDs and typed HTTP, no physical columns/SQL;12builtins, safe text, server policy. Auditfound reload/pending-navigation/detail-only/admission/mobile/choice-refresh bugs. Patch those flows and preserve primitives; file/report/tool UI remains unavailable rather than inert success. |
| Authorization/record access | KEEP + PATCH | RBAC/ABAC interface and mandatory audit retained. Add DataScope decision + trusted resolved teams. Field scope joins entityscope; client cannot provide policyScope. Native roles remain compatible; explicit scoped policies exercise narrower access. |
| Database roles/background worker | REFACTOR | app runtime leastprivilege; migration login owns objects. Worker currently opens APIcredentials and is idle. Add NOLOGINschema_owner, controlled migrator/API aliases, dedicatedworker/dispatcher/audit/maintenance roles and one immutable routed-job scope. Dispatcher has routingonly; worker credential mandatory. |
| Application versions/approval/intent | KEEP + PATCH (new foundations) | Source revisions are immutable drafts; no production release/approval exists. Persist immutable compiled release bundles and activepointer foundation; hashbound approval, stale dependency/base checks, requester/approver separation; typed intent + independent acceptance. No public promotion/AI workflow service. |
| Schema migration strategy | KEEP + PATCH | Existing migrations forward-only/transactional/advisorylock; retained legacy data. Additive migration, exact old→new equivalence and checkpointed route; no legacy drop. Typed Domain Packs classified separately and cannot be redefined by tenantmetadata. |
| Audit/events | KEEP + PATCH | Protected business/audit share WithRuntime; audit mandatory before reply. No eventpublisher/outbox exists yet. Add transactionally coordinated mutation/audit/outbox foundation; dispatcher/inbox delivery remains M8. |
| Tool/sandbox/ERP contracts | KEEP + PATCH (new foundations) | No tool/workflow/AI execution to replace. Add capability-specific retry/unknown/reconciliation, sandbox isolation, patch3way conflicts, Money/FX/zone/calendar/number types. No accounting/inventory engine, rule evaluator or externalHTTP execution. |
| Tests/coverage | KEEP + PATCH | Retain Go races, realRLS/registry/compiler/data tests,23frontend/3browser tests and golden. Add V3 scope/worker/release/approval/migration/noDDL tests and regressions from M6 re-audit. Coverage is not full WCAG/load/production certification. |

No REMOVE/REPLACE classification is justified. No M0–M6 restart. The V3 kit says
M0–M6 complete as its starting assumption; the latest executed audit found M6 partial.
That evidence remains relevant and is repaired where within M6R. No silent renaming
of prior history or claim that later release/workflow/tool services already exist.

## Freeze and mandatory boundaries

V3 generic storage remains isolated preview data during M6R; an immutable stored
production release registry/pinning contract is introduced without exposing a
production write/promotion HTTP route. Full sandbox manager/candidate builder/approval
UI/promotion service remain M11–M13. Contracts and tested state guards are foundations,
not a claim that the future user lifecycle is deployed.

The old generic adapter stays available for read-equivalence/recovery. New writes
route to V3 records. Existing coreIDs, values, creators/timestamps, versions and
source/IR keys must match; no overwrite of unequal destination state is allowed.
Schema owner/role changes require explicit controlled local administrator bootstrap;
no production credential is inferred or normal runtime granted migration access.

M6R closes HIGH gaps in DataScope/worker roles and release binding first. Independent
adversarial review is required before M7; M7 is not authorized by this task.
