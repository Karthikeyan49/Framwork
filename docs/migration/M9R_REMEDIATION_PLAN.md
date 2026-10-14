# Proposed M9R — Foundation remediation before M10

Date: 2026-10-13 (UTC). Status: **implementation authorized and in progress**.
Evidence: [external review validation](../verification/M0_M9_REVIEW_VALIDATION.md).
This is a focused follow-up to M9; historical milestone numbering is unchanged.

Current checkpoint: R0 and R1 are implemented; R1 independently passed the
database credential/resource threat model. R3a was pulled forward because it
does not depend on storage format. Its original reference floor passed; the
resumed host exposed HTTP misses. The page-fetch change passed independent
correctness/security review but failed the current-host HTTP performance gate.
[R1 evidence](M9R_R1_IMPLEMENTATION_REPORT.md) and
[R3a evidence](M9R_R3A_IMPLEMENTATION_REPORT.md) retain failed checks and limits.
R2's first pure schema-plan/transform tranche passed independent review. Its
equal-schema preview binding/adoption tranche passed root fault/regression and
browser tests and independent review at `145eaa4`. R4 local command changes passed
root correctness/fault/browser checks; its original write gate failed with one
503 and later p95 misses. After rollback/recovery fixes and measured FIFO admission,
the root 32-client gate passed at p95 297.404 ms with no errors; independent
R4 review/workload passed at `94932e3` (p95 340.575 ms, zero errors). R2c preview
evolution now passes root/full/browser and independent fault/capacity review at
`4958df9`: actual 100k migration, mapped-generation query p95 270.905ms,
and preserved 32-client write p95 209.973ms with zero errors. Production authority
and broader migration capacity remain separate. Governed numeric R3b passes the unchanged HTTP floor in root
and independent runs (independent p95 96.713 ms, zero errors). See
[R3b evidence](M9R_R3B_IMPLEMENTATION_REPORT.md) and
[R4 progress](M9R_R4_IMPLEMENTATION_REPORT.md). M10 remains paused. Additional
index types, R3c/R3d and R5–R7 are not complete.
[R2 progress](M9R_R2_IMPLEMENTATION_REPORT.md) and
[R2c closure/evidence](M9R_R2C_IMPLEMENTATION_REPORT.md).

## Objective and release gate

Preserve working M0–M9 code while closing the reproduced worker authority,
dataset continuity, and contention/query-budget problems. Do not continue M10
tool execution until R1–R4 below have passed their foundation gates. R5 policy
must precede business features that require ownership, sharing or custom roles.
Production release requires the later authority and operations gates too.

The user authorized pushing this checkpoint and beginning foundation remediation.
Production exposure, main-branch merge, and subsequent milestones remain separate
tasks. Implementation should use small reviewed changes
with fresh evidence at a stable commit, rather than another combined milestone
commit. Proposed numbers below are acceptance targets, not achieved results.

## R0 — Make scope and ownership explicit

Owner: architecture and QA. Deliver one documentation-only change.

1. Add a V2→V3 supersession map listing document/section, replacement authority,
   affected code, responsible milestone and status. Preserve checksum-frozen
   source documents. Explicitly supersede old roadmap numbering, approval order,
   generated-column customization, and restore-time recompilation instructions.
2. Give each capability a status: contract only, preview wired, verified under a
   stated threat model, production wired, or deferred. Link the current finding
   register from README and prior completion reports instead of rewriting history.
3. Replace accumulated agent-rule duplication with one concise contract and links.
   State that passing trusted Go-path tests is not credential-compromise testing.
4. Record dependency order and the remaining product roadmap below; distinguish
   implementation ownership from future operational rollout.

Acceptance: every review finding has an owner, gate, and evidence status; no
production-ready claim rests on contract-only or preview-only work. All source
kit checksums remain unchanged.

## R1 — Restrict worker database authority (HIGH, first implementation)

Owners: security, database and workflow engineers. Keep broad business data
access disabled for any new tool/worker deployment while this is unresolved.

Design decision first: choose a boundary that PostgreSQL can enforce even when
the worker process/credential is compromised. Recommended direction is a
separate routing principal and a narrow execution capability/credential scoped
to a job. A shared role plus caller-set GUCs is insufficient. A signed context
alone is also insufficient if the compromised principal can mint or obtain
arbitrary capabilities without an independent authorization boundary.

The enforced authority must bind worker identity/audience, tenant, job,
app/entity/record or explicitly authorized resource set, immutable release/schema,
allowed operations, lease generation/token and expiry. Decide whether to use
short-lived scoped database principals or a narrowly privileged database/gateway
port; document the threat model and credential issuance/revocation design in an
ADR before implementing it. Do not add a check only in the trusted Go caller.

Separate global routing claims from business DML. Remove generic record grants
from the workflow worker unless required by a registered activity. Prevent the
worker from selecting or changing arbitrary other records within its tenant.
Validate workflow artifact admission at the trusted producer boundary; distinguish
stored hash checks from authenticated compiler/candidate provenance.

Acceptance:

- In a populated two-tenant fixture, raw worker credentials cannot discover and
  bind arbitrary tenants or read/update/delete records outside a granted job.
- A legitimate ticket permits only its authorized operations/resources; wrong
  audience, record, app, release, tenant, generation, expired/replayed lease and
  forged GUCs all deny at the enforcement boundary.
- Dispatcher credentials cannot read payloads or mutate business records.
- Membership/policy revocation, pooled connection reuse and concurrent claims
  preserve denial. Normal trusted workflow execution and recovery still work.
- Execute the pre-fix attack against the fixed commit and record the denied
  operations, populated negative fixtures, and independent review.

Recovery: quiesce workers, preserve durable jobs/history, apply additive role/port
changes, then enable the new worker. On failure stop workers and forward-fix;
do not restore broad grants as an automatic rollback. Rotate deployed worker
credentials as part of rollout if they existed outside isolated development.

## R2 — Separate durable data identity from compiled release identity

Owners: data runtime, metadata/compiler and database engineers.

Define stable tenant/application/entity/record identities with a separate data
schema version and explicit environment/workspace identity. Keep sandbox/preview
datasets isolated; bind production execution to immutable active releases without
using every compiler/catalog hash or source revision as a new production dataset.
Preserve typed ERP Domain Pack storage through explicit adapters.

Create a versioned mapping from legacy preview namespaces to new datasets.
Never merge records merely because their UUIDs or app names match: two revisions
may contain deliberately different data. Require a declared destination and
conflict report for every migration. Keep the legacy adapter/read path until
row counts, hashes, references, ownership, shares and versions are reconciled.

Assign JSONB schema evolution to the data/compiler boundary: stable field IDs,
rename versus label change, lossless conversions, reject/quarantine rules,
resumable backfills, checkpoints, compatibility windows, validation, and index
updates. Compiler migration plans must describe real operations instead of NONE
when data transformation is required. No tenant-triggered shared-table DDL.

IR compatibility is a separate lane: retain original immutable artifact bytes;
support explicitly versioned readers/executors; an upgrade creates a new candidate
with migration/verification and approval. Never silently recompile an active or
in-flight release during startup, restore or rollback.

Acceptance:

- Two compatible releases/compiler or catalog versions read the same intended
  production record identities and versions; independent previews remain isolated.
- Label-only changes preserve records; rename/type/backfill cases have executable
  plans and reject unsafe conversions before activation.
- Pre/post fixture manifests match for unaffected records, including references,
  ownership and shares. Interrupt/resume/replay cannot duplicate or lose records.
- Old pinned workflows retain their IR and declared schema compatibility; active
  pointer rollback cannot pretend to undo incompatible data conversion.

Recovery: additive schema and shadow/backfill validation first, explicit cutover
barrier second. Preserve legacy data and mapping checkpoints. Rehearse backup
restore before a destructive cleanup is even proposed; removal requires a
separate reviewed change after the compatibility window.

## R3 — Remove the per-row authorization cost without weakening RLS

Owners: database, policy and performance engineering; build on R1/R2 identities.

Capture representative EXPLAIN plans and SQL/HTTP latency baselines. Move stable
trusted membership/context validation to a transaction-safe evaluation while
retaining tenant predicates and FORCE RLS for every relevant table. Verify the
planner executes the expensive authorization work at the intended frequency;
do not assume STABLE alone prevents per-row calls. Preserve live revocation
semantics and fail-closed context checks.

Introduce governed custom-field query/index requirements and unique-field
semantics. Reuse or replace `unique_projections` only with a complete write,
backfill, conflict and concurrency design. Indexes must be operator/platform
managed and bounded by quotas, not uncontrolled tenant DDL. Push supported
aggregates into validated parameterized SQL; preserve exact numeric behavior and
return explicit budget errors for unsupported plans.

Deliver R3 in separate subgates: R3a transaction-safe authorization evaluation
and measured query budgets; R3b governed indexes/projections; R3c unique-field
semantics; R3d aggregate expansion. R3a is the immediate foundation blocker.
R3b–R3d must each have their own design, PR and acceptance evidence; their
feature scope must not turn the R3a fix into a combined compiler/data rewrite.
M10 may not rely on an unfinished R3b–R3d feature or claim its capability.

Proposed acceptance environment: pinned PostgreSQL image/configuration and fixed
reported CPU/memory limits, realistic row widths, ten populated tenants, at least
100k records in the hot dataset, mixed ownership/shares and selective filters.
Record warm/cold conditions and at least 100 measured requests after warmup.

Proposed gates on that reference environment:

- Authorized key page: SQL p95 ≤100 ms; supported custom numeric sort and selective
  filter: SQL p95 ≤500 ms at 100k; no normal query hits the 2 s statement timeout.
- Representative API query p95 ≤750 ms with ten concurrent clients. Report
  throughput, errors and p99 as well; do not substitute a three-sample median.
- Query plans/diagnostics prove membership validation does not scale once per
  scanned business row. Do not satisfy the budget by disabling RLS or increasing
  the timeout alone.
- Cross-tenant, missing/stale context, DataScope, field inference and revocation
  negatives pass under every optimized query and pooled connection path.
- Duplicate values are rejected consistently under concurrent writes/backfill;
  nullable/normalization rules are explicit. Aggregates match independent fixtures.

The chosen budgets must be reviewed against the deployment tier before coding;
any revision belongs in a recorded design decision, not a post-failure test edit.
Recovery: additive indexes/projections, bounded backfills, feature-gated query
plans and a verified old query path. Never roll back to weaker authorization.

## R4 — Allow independent record writes while preserving integrity

Owners: data runtime and database engineers.

Replace the single dataset counter with the smallest required locks and database
constraints for record CAS, uniqueness and relationship/delete integrity.
Document lock ordering and supported isolation. Preserve optimistic row versions
and define whether disjoint partial edits merge or conflict.

Add bounded jittered retries only for known-aborted serialization/deadlock
transactions whose full operation can safely be repeated. Use stable command
idempotency keys; business update, audit and outbox must still commit once.
Unknown commit outcomes require lookup/reconciliation, not blind replay. Return
an explicit conflict for stale row versions instead of a generic 503.

Acceptance:

- Thirty-two clients mutate distinct records in one dataset for at least 60 s:
  no user-visible lock/serialization 503, no lost updates, bounded retries, and
  reported p95 latency target ≤500 ms on the reference environment.
- Two writers with the same expected record version yield one committed version
  and one defined conflict/idempotent result, not duplicate effects.
- Concurrent parent deletion/reference insertion, unique-value claims and
  worker/user writes preserve all constraints with controlled lock order.
- Inject rollback, serialization, deadlock and lost-commit-response failures;
  count exactly the committed business/audit/outbox facts and no duplicated event.

Recovery: compatibility flag or staged writer rollout with preserved constraints;
quiesce writes before switching incompatible lock protocols. Never remove the
counter in isolation without replacement reference/delete protection.

## R5 — Make authorization usable as a product capability

Owners: identity/policy, data and frontend engineers.

Replace fixed-role branching with versioned tenant role/capability definitions
and central RBAC/ABAC policy. Support `finance_manager` without trusting token
role claims. Add authorized ownership/team/share administration and audit.
Specify field visibility/masking, filter/sort inference protection and partial
updates so unmodified inaccessible fields do not require gratuitous read/write.

Acceptance: owner/team/shared/conditional fixtures, negative grants, revocation,
hierarchy limits, deny precedence and custom workflow roles pass through both
API and SQL paths. Frontend behavior follows server policy. A user cannot learn
a hidden value through sort, filter, count, validation or write responses.

Gate: complete before shipping business workflows that promise these policies;
until then label tenant-wide built-in roles and field-edit limits explicitly.

## R6 — Establish requirement and verification authority (M11–M14)

Owners: architecture, security and QA; integrations belong to the existing V3
milestones M11 Sandbox, M12 Candidate Builder, M13 Promotion and M14 AI Builder.

M6R owns the typed contracts; M14 captures requirements; M12 owns completeness
and independent verifier orchestration; M13 trusts only authenticated evidence
and approvals. Assign these responsibilities now to resolve the overlap.

Use typed requirement categories with provenance and requiredness controlled by
the product owner/policy. Unknown mandatory business facts block candidates; a
builder cannot evade a mandatory requirement by marking it optional. Independent
acceptance derives from requirements, is executable, and produces immutable
verifier evidence bound to inputs, exact artifact hash, verifier version and
result. Define trust/identity separation for requester, acceptance author,
verifier and approver; author inequality alone is not enough.

Compute risk from the compiler diff, data migration and capabilities using
versioned policy. Bind authenticated staging/approval identities server-side;
reject caller-invented PASS/risk/provenance at public endpoints. Activate exactly
the verified and approved bundle through the existing hash/CAS mechanisms.

Acceptance: structurally valid but business-wrong fixtures fail executable tests;
changed input/artifact/attestation, omitted required facts, forged PASS and stale
approval all block activation. Production data runtime reads the immutable active
release and rollback preserves its true data/side-effect limitations.

## R7 — Reliability and operating limits

Owners: events/workflow, DevOps and security engineering. Final production gate;
any earlier M10 dependency must be delivered before the relevant tool ships.

- Separate broker delivery and consumer processing deadlines; retry based on
  outage budgets with backoff. Provide least-privilege reviewed replay/quarantine
  controls and explain aggregate ordering consequences. Test at least a ten-minute
  consumer outage without exhausting a five-attempt budget immediately.
- Version consumer/subscription and payload contracts. Keep reference-only broker
  messages and idempotent inbox behavior where appropriate; make business event
  payload semantics explicit rather than assuming record ID/version is enough.
- Define revise/resubmit cycles with budgets and durable history archival; add
  lease renewal and fencing if activities outlive leases. Choose snapshot versus
  current-record approval explicitly, with invalidation on changed records where
  required. Deduplicate immutable IR storage only after retention/refcount design.
- Add rate limits before expensive authentication DB/audit work; bounded security
  telemetry must still retain meaningful denial evidence. Separate immutable
  business audit from high-volume decision diagnostics, with retention, partition,
  export and deletion authority appropriate to each class.
- Profile compilation, validator construction and catalog guards; cache only with
  explicit immutable keys and invalidation/revocation guarantees. Preserve startup
  and runtime privilege-drift detection.
- For each external tool in M10 define idempotency keys, provider guarantees,
  retries, timeout/unknown outcome and reconciliation. Demonstrate crash-after-send,
  duplicate callback and delayed provider response. Never claim universal exactly-once.

Proposed production DR gate: documented RPO ≤15 minutes and RTO ≤4 hours for the
initial deployment tier, followed by an executed restore drill using stored IR
without recompilation. These are proposed objectives, not current guarantees;
data residency/retention and deployment owners must approve the chosen tier.

## Remaining roadmap ownership

| Capability | Proposed owner / gate |
|---|---|
| SSO/OIDC/SAML and configurable clock skew | Identity workstream alongside R5; required provider flows before enterprise production; SAML only when required |
| Search and localization | Product/platform workstream before the first ERP Domain Pack release at M16; scope languages and search authorization explicitly |
| Residency, backup/restore and audit retention | Operations/security M15, with database prerequisites in R2/R7; production gate, not an optional future note |
| Import/export and portability format | M17; publish a versioned manifest, hash/compatibility rules and import round-trip tests before claiming portability |
| JSONB evolution / IR compatibility | R2, maintained by data/compiler owners; candidate M12 consumes their plans |
| Intent / acceptance / verified candidate | R6 assigns M14 capture, M12 verification, M13 authority; M11 supplies the sandbox |
| Large-scale performance | R3/R4 establish a usable floor now; M18 extends measured capacity rather than postponing foundation correctness |

## Delivery and completion discipline

Start with a dedicated remediation branch from the reviewed M9 commit; preserve
the existing branch stack. One PR/change per R1–R4 boundary, with focused tests
and stable commits for independent review. Inspect existing PRs/branch ancestry
before planning an ordered merge to main; a push is not a merge. No automatic
merge is part of this plan.

For each change record: failing pre-fix reproduction, implementation/ADR,
post-fix positive and negative tests, regression selection, threat model,
performance envelope, recovery rehearsal and residual gaps. Independent review
must use a stable commit with populated negative fixtures and actual attacks or
faults where claimed. A human reviews the concrete design and evidence before
production exposure; an AI review is not a substitute for that ownership.

R1–R4 completion requires the reproduced problems to be demonstrably fixed and
the existing M0–M9 backend/frontend/integration/browser suites to pass on the
final commit. Do not relabel deferred R5–R7 services as complete to pass this gate.
After the focused gate, return a report and explicit M10 recommendation; do not
start another milestone automatically.

**Checkpoint status: R0/R1, R2a/R2b, bounded R4 and governed numeric R3b
independently verified. Full R2, remaining R3b types/R3c/R3d and R5–R7 are pending.
The complete foundation
gate and broader review findings are not accepted as resolved.**
