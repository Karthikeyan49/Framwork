# M9R — Foundation remediation on the merged preview

Current checkpoint: **20 October 2026 (Asia/Kolkata), slice C documentation
reconciliation. M9R STATUS: PARTIAL. Combined acceptance: FAIL.**
Original plan: 13 October 2026; historical milestone numbering is unchanged.
The [validated review](../verification/M0_M9_REVIEW_VALIDATION.md) remains the
baseline, not the current implementation inventory.

## Current integration and acceptance

The remediation branch contains merged product tree `552fa9e`, including
M10–M14 **development/test preview** work. Slice A documentation is `e78282f`;
slice B review/handoff is `a1d8b00`. The source has not changed in those slices.
M10 is implemented as preview on this tree; the remediation agent's authorized
lane remains remediation. No new M10+ feature work, main merge, PR or production
deployment is authorized here.

- [Slice A re-baseline](M9R_MERGED_REBASELINE_REPORT.md): full integration
  439 PASS/zero skips, browsers 11+7, nine fuzz targets and real 600-second outage
  pass. Seven of eight reference targets pass; three aggregate HTTP p95 cases
  exceed 750ms (worst 822.729ms). Injected driver-panic recovery **FAILS**.
- [Slice B merge review](M9R_MERGE_REVIEW_B_REPORT.md): all 23 resolved files
  inspected. Fresh full integration: **438 PASS, 1 FAIL, zero skips**; the
  96-receipt worker-policy compensation SELECT exceeds the unchanged 2000ms
  statement limit. Check/unit stages and a separately executed build pass.
  Passing A does not replace this failed B invocation.
- B-01 launcher option loss, B-02 the missing inherited 1.5 compiler-pair guard
  and B-05 SQL uniqueness registration's 1.4-only selector are confirmed open
  integration gaps. B-03 records the unsupported full pre-renumbering tool IR;
  standalone plan compatibility does not establish full-release compatibility.
  B-04 is stale version wording. See the [live ledger](M9R_FINDING_STATUS.md)
  and [handoff](AGENT_HANDOFFS.md) for evidence, owners and limits.
- Independent review of M14 and the merge is **NOT RUN**; the user deferred
  agents until 22 October, 21:00 UTC. Historical independent reviews certify
  their pinned slices, not this merged tree or later fixes. No causal merge
  regression is established by these runs without a controlled pre-merge run.

| Workstream | Current disposition and remaining gate |
|---|---|
| R0 authority/ownership | Current plan/ledger reconciled in C; root agent rules remain unchanged pending the [proposed update](M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md) |
| R1 worker confinement | Existing bounded database-credential/resource review retained; preserve exact job/lease/record boundaries and later tool residuals |
| R2 continuity/evolution | Governed preview datasets and bounded evolution verified historically; production release-bound execution belongs to milestone R6/M13; wider preview UI/index inheritance remains request-only |
| R3 query/integrity | Bounded index/unique/aggregate reviews retained; current aggregate capacity FAIL, 1.5 unique registration B-05 and compiler pairing B-02 remain open |
| R4 local writes | Bounded concurrent/idempotent local-command evidence retained; A 32-client writes p95 301.897ms/zero errors; external delivery has separate M10 contracts |
| R5 authorization | User-selected local role/record/team/field/sign-in work complete within its scope; real IdP/MFA/HTTPS deployment verification DEFERRED; worker-policy capacity remains R7 D1 |
| R6 requirements/verification/release authority | Implemented preview coverage in M11–M14; **PARTIAL**, held for the milestone agent. Production data plane and separate verifier credential remain architectural deliverables; review/fix acceptance and threat-model residuals remain open |
| R7 operations | Bounded R7a admission and R7b outage work retained; diagnostics, event operations, workflow lifecycle, audit retention, profiling, edge quotas and DR remain open |

## Ownership and change boundaries

| Lane | Current responsibility |
|---|---|
| Milestone agent | R6; tools, sandbox, risk, intent, verification, promotion and AI; schema/IR/compiler 1.5.0 and tool DSL2.2; tool/release/AI routes; production data plane; verifier credential; M10–M14 independent review; M15+ |
| Remediation agent | Remaining R7 and diagnostics; reference gates; events/traffic; identity/OIDC; data runtime/indexes/aggregates/uniqueness; field access and role UI. Wider R2/R3/R5 product additions and external identity verification require the user's request |
| Shared | `internal/platform/access/access.go`, `internal/platform/app/app.go`, `internal/config/config.go`, compiler/metadata/workflow contracts, frontend `contracts.ts`, Makefile, CI, live ledger and integration plan. Preserve both lanes and name edited files in each report |

Use only `remediation/m9r-foundation`; fetch and merge integration before each
slice. The milestone agent integrates this branch. Do not push integration/main,
rebase, reset, force-push, delete branches or create a PR as part of this task.
Coordinate through [AGENT_HANDOFFS.md](AGENT_HANDOFFS.md).

Number reservations: remediation migrations 00058–00079 and ADR064–099;
milestone migrations 00080–00099 and ADR100–119. **Migration ordering is
unresolved:** the merged database is already at 89 and default Goose rejects
new lower-numbered migrations. Even allowing out-of-order application could
produce different latest function/grant definitions on fresh versus upgraded
databases. Agree and verify both paths before allocating/applying a new
migration. No historical migration is renumbered/edited and no out-of-order
switch is enabled by this plan.

Version meanings remain: schema/IR/compiler 1.3 approval roles, 1.4 unique,
1.5 tools as a superset; workflow DSL2.0 closed roles, 2.1 tenant roles,
2.2 pinned tools (legacy 2.1 tool shape is restore-only). Coordinate a future
1.6 superset and every Go, frontend, AI, JSON-schema and SQL version gate if
needed. Start SQL replacements from the latest merged definition; preserve
guards and the derived 00087 worker allowlist. Use `UpTo(n)` in forward tests.

## Historical remediation checkpoints (through 19 October 2026)

The following dated progression is retained as history. References to a paused
M10, future R6 or an earlier green gate describe those source checkpoints;
the current state and acceptance above take precedence.

Latest closure,19 October2026: [R7b](M9R_R7B_IMPLEMENTATION_REPORT.md) is bounded
PASS after independent review, actual600s outage/recovery,304-group final
regression/build, browser11+7 and unchanged read/write gates. R5 local scope and
R7a remain complete; external identity is deferred by user choice. R6, other R7
and wider product lifecycle remain open. Earlier dated checkpoints below are
history, not claims that the whole remediation is complete. No M10/main merge.

Earlier checkpoint progression (latest closure below): R0 and R1 are implemented; R1 independently passed the
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
index types, R3d and full R5–R7 are not complete; bounded R3c is recorded below. Bounded R5a and
[R5b SQL worker authority](M9R_R5B_IMPLEMENTATION_REPORT.md) now pass documented
review/full/browser gates at a5bba7b; remaining product policy and capacity work
are explicit.
[R2 progress](M9R_R2_IMPLEMENTATION_REPORT.md) and
[R2c closure/evidence](M9R_R2C_IMPLEMENTATION_REPORT.md).

[Text and civil-date R3b](M9R_R3B_TEXT_DATE_IMPLEMENTATION_REPORT.md) now passes
independent review and the unchanged 100k-row/ten-client capacity gate. Review
reproduced and closed raw intrinsic-ID admission and cold-plan source-scan
amplification. The latter requires correlated live-view point fetches, not weaker
authorization or longer timeouts. Additional query work and R3d aggregates
remain separate; bounded R3c is recorded below. Final whole-suite evidence is recorded in the
slice report.

[Enum/reference R3b](M9R_R3B_ENUM_REFERENCE_IMPLEMENTATION_REPORT.md) also passes
independent executed review: exact options/target witnesses, Unicode/NULL contracts,
tenant/worker/field/canonical-access faults, actual dropped COMMIT/concurrent copy,
maximum enum bounds, retained-binary CRUD and twelve 100k-row capacity cases.
R3d and the wider product/operations gates remain open; bounded R3c is recorded below;
final root verification belongs to the linked report.

[Datetime/boolean R3b](M9R_R3B_DATETIME_BOOL_IMPLEMENTATION_REPORT.md) passes
independent executed review: native typed projections, strict private conversion,
unrounded UTC bounds, existing SQL precision/raw cursors, real commit/concurrency
faults, live retained-generation authorization, retained API CRUD and all twelve
100k-row capacity cases. The slice report records final root verification.
R3d and broader product/operations gates remain separate; bounded R3c is recorded below.

[Governed full-text search R3b](M9R_R3B_SEARCH_IMPLEMENTATION_REPORT.md) now
passes independent executable review at `d61f696`, with final source/review
`29a61ac`: 55 retained groups, nine new adversarial race groups, actual old-API
CRUD/cursor compatibility, fresh migrations through 51 and all eight selective
100k-row capacity cases. The first capacity runs reproduced a target-view join
timeout; correlated live-view target point lookups fixed it without weakening
authorization, fixtures or budgets. Final root check/test/build passes 230
integration tests with zero skips, 96 frontend tests, 9 application plus 7 sign-in
browser cases and all six numeric/scalar/choice/native/search/write gates.
Final search worst p95 is 203.519ms with zero errors; the 32-client write gate
completes 11989 writes, p95 192.061ms, zero errors. Search is fixed simple/plain
AND, default ID ASC pages and one string field; no search UI/ranking/languages
or arbitrary broad-term capacity is claimed. Wider R3b, R3d and R6–R7 remain
open. No production activation, main merge or M10.

[Governed uniqueness R3c](M9R_R3C_IMPLEMENTATION_REPORT.md) now passes independent
executable review on source `d15be92`, with dedicated review/tests `2cf2f57`.
Opt-in schema/IR/compiler1.4 separates integrity from storage identity; verified
claims, private transaction permits, live scope checks and immutable abort
generations preserve populated compatible bindings. The concurrent receipt-key
regression was actually reproduced and fixed. Final check/test/build passes
246 integration groups without skips and 97 frontend tests; browser9+7 and all
seven capacity gates pass. The final unique writer completes10,061 updates at
32 clients, p95 227.569ms and zero errors. UI, READY removal and constraint-
preserving evolution are not part of this bounded API foundation.

[R3d SQL aggregates/planning](M9R_R3D_IMPLEMENTATION_REPORT.md) now passes bounded
independent functional review, 269 integration groups without failures/skips,
97 frontend tests, browser suites 9+7 and all eight reference capacity targets.
All 24 aggregate cases over unmapped/current/retained 100k-row generations pass
with exact oracles, zero errors and worst HTTP p95 712.030 ms. ADR058–059 and the
report retain the accepted tier, failed experiments and write-throughput cost.
No general query or production capacity is claimed. Wider search/uniqueness
lifecycle, external identity deployment, R6 and R7 stay open; the latest local R5 closure is below.

## Objective and release gate

Preserve working M0–M14 preview code while closing the remaining foundation
and operating-limit findings. The original R1–R4 foundation dependency and R5
policy requirements remain safety constraints on consumers. Preview M10–M14
already exist; their presence does not clear current failures or authorize this
agent to expand them. Production release requires the remaining R6 and R7 gates.

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

Keep the separate R3a authorization, R3b index, R3c integrity and R3d aggregate
boundaries and their recorded designs/reviews. Their bounded historical PASS
results do not clear the current merged aggregate gate or B-02/B-05. Future
consumers may rely only on the capability and measured tier actually delivered;
deliver fixes in small changes on the authorized branch, without a combined
compiler/data rewrite or an automatically created PR.

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

R5a at caa9d84 passes independent API, trusted-Go, fault and workload checks.
See [implementation](M9R_R5A_IMPLEMENTATION_REPORT.md) and
[review](../verification/M9R_R5A_INDEPENDENT_REVIEW.md). It does not close full R5.
[R5b](M9R_R5B_IMPLEMENTATION_REPORT.md) at a5bba7b now passes its bounded SQL
worker authority gate, full regressions and browser checks. Live role/field/record
scopes and inactive heads deny at private SQL ports; identity remains available
for exact-ticket denial evidence. Independent review found and the forward40
fixes corrected typed UUID and repeated-evaluation issues. Its final existing
regression suite passed; new final probes were not run after a platform content
interruption. The synthetic receipt workload passed with a narrow margin, not
a wider capacity certification. Preserve the exact confinement and lease gates.

[R5c-a](M9R_R5C_IMPLEMENTATION_REPORT.md) (record owner/team/share administration,
ADR034) passes independent review at 8f8067a and R5c-b team administration (ADR035)
at 6b50eb7. [R5e](M9R_R5E_IMPLEMENTATION_REPORT.md) adds opt-in metadata/IR 1.3
and Workflow DSL 2.1 tenant approval roles, preserving legacy 1.2 artifacts and
live membership/head/capability/DataScope checks. Independent review at `44a3979`
passed; audit-correlation follow-up `d4364a3` passed independent re-review, with
regressions at `f1e2e74`. Final full checks/tests/build passed (integration 629.247s)
and browser 5/5 passed. Full R5/M9R remains PARTIAL.
[R5d](M9R_R5D_IMPLEMENTATION_REPORT.md) now has independently verified SQL field
masking, changed-field patches and strict point-input authorization (ADR037),
including the closed owner-sensitive findings. Final full checks/tests/build
passed (integration 829.428s), browser 6/6 and the numeric 100k/ten-client gate
passed (p95 216.415 ms, zero errors). Preserve its explicit custom-policy,
conditional-write and hidden-size limits. The earlier intermittent workflow
connection failure remains unconfirmed and tracked for R7 diagnosis.
[R5c administration UI](M9R_R5C_UI_IMPLEMENTATION_REPORT.md) (ADR038) now passes
independent review, full checks/tests/build (177 integration tests in 561.291s,
frontend 86/86) and browser 9/9. Three demonstrated review findings are closed.
Preserve actor-owned recovery, observable-target review and typed-header access
administration without unrelated business values. The initial full-run worker
statement timeout remains an R7 capacity issue despite passing reruns.
[R5f provider sign-in and clock tolerance](M9R_R5F_IMPLEMENTATION_REPORT.md)
(ADR039) now passes independent executable review: pinned OIDC/PKCE, live DB
authority, dedicated one-time Redis flows, StrictMode-safe browser completion
and stale-connection guards. The MEDIUM concurrent-cookie finding is closed by
executed regression. Final full checks/tests/build pass (179 integration tests
in 605.921s, frontend 94/94). External provider/client deployment, MFA assurance
and production gateway validation remain required; a local fixture is not SSO
deployment evidence. R5f did not deliver role-definition editing.

[R5g local role administration](M9R_R5G_IMPLEMENTATION_REPORT.md) (ADR060)
completes the missing governed editor, role discovery and member assignment UI.
Independent review reproduced and closed receipt-body/hash and connection-
preparation findings at product fix `5c05d0b`, review `ba21eb0`. Final root
check/test/build passes 275 integration groups without failures/skips, 126 frontend
tests, browser 11+7 and unchanged numeric-read/write gates (p95 112.045/243.551ms,
zero errors). Exact commands/profile and earlier failures are in the report.
Native bootstrap replacement,
owner protection, exact publication receipts and label-CAS limitations remain
explicit. The user selected local completion/deployment preparation with no
external provider yet. Real IdP/MFA/production gateway verification is DEFERRED,
not passed; the [operator runbook](../operations/R5_IDENTITY_DEPLOYMENT_RUNBOOK.md)
and blank evidence template define that separate release gate. Local R5
completion does not close overall M9R or authorize business production rollout.
Wider query/search/uniqueness lifecycle and R7 remain open. R6 has merged
preview coverage in M11–M14 and stays partial under the milestone owner below;
it is not another unstarted remediation implementation.

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

**Status: PARTIAL — implemented preview coverage; held for the milestone agent.
Do not duplicate these modules in remediation.**
Read [M10/M11](M10_M11_IMPLEMENTATION_REPORT.md),
[M12](M12_IMPLEMENTATION_REPORT.md), [M13](M13_IMPLEMENTATION_REPORT.md) and
[M14](M14_IMPLEMENTATION_REPORT.md), with ADR100–104 and the integration map.
Those reports retain their original gate results and review qualifications.

| Requirement | Implemented preview coverage | Remaining boundary |
|---|---|---|
| Sandbox evidence | M11 server-owned synthetic datasets, capability ceilings, frozen immutable record/tool manifests | Redacted/dedicated modes, UI and residual port trust; latest fixes lack independent re-verification |
| Business intent | M14 typed human apply/confirm of AI proposals; M12 `requirements-v2` requiredness floors and provenance | No real model/provider deployment verified; B-01 launcher loss; open/unsupported mandatory categories block |
| Executable acceptance | M12 `acceptance-v2`, exact independent money oracle and `verifier-v2` over pinned plans; four supported categories | Deterministic verifier runs in API trust domain; separate verifier credential/attestation is absent; no claim of arbitrary ERP acceptance |
| Risk and exact candidate | M12 derives diff/storage/risk and builds bundle2.0; API bodies cannot declare authority; load recomputes against the candidate's own base | Database cannot independently recompute verifier/diff/risk; B-02 version guard and B-05 1.5 unique registration remain open |
| Approval/activation/rollback | M13 preview HTTP lifecycle, SQL ports, exact approved hash, current authority/quorum, CAS/fences and immutable history | Active pointer is control-plane state: no production datasets/routes/storage-migration execution; credential-bound approvals and latest fix reviews remain open |

The two architectural deliverables still missing from R6 are the **production
data plane** and a **separate verifier credential**. Completion also requires
resolving review/integration findings and supplying the missing acceptance
evidence; “two deliverables” does not mean only two unresolved defects. The
compromised-API identity/verifier residual, limited requirement categories,
unreviewed latest fixes, M14/merge independent review and real-provider checks
remain explicitly qualified in the milestone reports and live ledger.

Release acceptance stays unchanged: executable valid-but-business-wrong fixtures
must fail; changed artifacts/attestations, omitted required facts, forged PASS
and stale approvals must deny. The production runtime must resolve immutable
active releases with provisioned compatible storage and truthful rollback
limits. Existing Go verification is not independent credential attestation.
The remediation agent records findings and coordinates shared boundaries;
the milestone agent owns implementation and integration of R6.

## R7 — Reliability and operating limits

Owner: remediation, with milestone coordination at shared workflow/tool ports.
This remains a production gate. Existing preview tool contracts are maintained
by the milestone agent; R7 does not certify a deployment or extend them silently.

### Remaining R7 order — proposed, awaiting the user's confirmation

The user requires an order decision before D implementation. The following
seven D slices are subdivisions of R7, not seven new framework milestones.
Complete and report one bounded slice before starting the next.

| Proposed order | Deliverable and acceptance |
|---|---|
| D1 diagnostics | Diagnose unconfirmed workflow connection failure, worker-policy SELECT/EXPLAIN timeouts and injected driver-panic recovery. Preserve B's failed initial SELECT and A's driver failure; reproduce in owned populated fixtures, capture cause/recovery evidence and keep 2s/lease/auth budgets unchanged. A passing retry alone does not close a diagnostic |
| D2 events | Least-privilege replay/quarantine and dead-predecessor operations; versioned subscriber/payload contracts. Preserve R7b RC/restrictive RLS, leases, exact attempts, inbox/receipt evidence and real-outage recovery |
| D3 workflows | Budgeted revise/resubmit and history lifecycle; renewal/fencing; explicit snapshot/current-record approvals. Extend ADR100 A1's database-derived tool approval/current-version gate; do not introduce a competing tool gate or weaken old pinned readers |
| D4 audit | Measure business/security decision volume; design authorized retention, partitioning, archive/export and recovery. Preserve immutable business facts and never erase receipts to replay |
| D5 profiling/caching | Profile complete request/worker paths and current aggregate/unique headroom. Preserve A's three aggregate HTTP misses; re-run all affected reference targets after changes. Cache only immutable, version-keyed values with explicit invalidation and live authorization |
| D6 deployment quotas | Distributed/edge limits under the actual proxy/deployment topology; preserve local R7a bounds and identity rules. Requires deployment evidence, not only another in-process limiter |
| D7 disaster recovery | Agree tier/ownership, execute restore from backup using stored IR without compilation, verify identities/references/release pointers and recovery. Proposed RPO ≤15m/RTO ≤4h are objectives until a measured drill passes |

B-01/B-02/B-05 fixes, B-03 artifact cutover, B-04 wording and migration-order
governance remain coordinated integration work in parallel ownership lanes;
they are not marked fixed by this proposed D sequence. No D implementation
starts in slice C.

### Historical R7a/R7b slice evidence (19 October 2026)

The following slice results remain bounded historical evidence. Current
merged-tree results and failures are in the checkpoint above and the A/B reports.

**R7a is pulled forward independently of R6:** ADR061 and the
[implementation report](M9R_R7A_IMPLEMENTATION_REPORT.md) cover bounded
pre-database protected API admission. Stable product `8f9805e` and independent
review/tests `cf45300` pass the adversarial review, including a checksum-pinned
real API before/after attack. Final root checks/build pass 283 integration groups
without failures/skips, 126 frontend tests, browser suites 11 + 7 and all eight
unchanged reference targets. This closes only local request admission. It does
not establish distributed
edge quotas, durable rejection retention or the other R7 requirements below.
No M10 or production promotion is introduced.

**R7b is also bounded PASS:** ADR062–063 and the
[report](M9R_R7B_IMPLEMENTATION_REPORT.md) separate5s publication leases from15m
receipt windows, with20 backed-off attempts and existing3s Go processing budgets.
Event jobs require RC and direct restrictive RLS; API/workflow snapshots stay RR.
All three independent findings close at product031c8a1/reviewa15d629. Corrected
source survives600 real seconds and recovers3 exact local facts of each kind,
retaining19 attempts. Final full304-group check/test/build, browser11+7 and
unchanged numeric-read/32-client-write gates pass. Migration57 enforces a drained
maintenance cutover and refuses active old readers/binders; no online revocation
or raw-process liveness guarantee. First failed600s recovery remains unclassified
and preserved. Current read/write p95 values158.903/349.656ms pass their original
750/500ms limits but are slower than R7a; broader profiling remains open.

- Provide least-privilege reviewed replay/quarantine controls and explain
  dead-predecessor ordering consequences. Broker-loss repair may wait the
  remaining15m receipt window; subscriber/recovery operations remain separate.
  A database processing-lease protocol is not supplied by the bounded Go budget.
- Version consumer/subscription and payload contracts. Keep reference-only broker
  messages and idempotent inbox behavior where appropriate; make business event
  payload semantics explicit rather than assuming record ID/version is enough.
- Define revise/resubmit cycles with budgets and durable history archival; add
  lease renewal and fencing if activities outlive leases. Choose snapshot versus
  current-record approval explicitly, with invalidation on changed records where
  required. Coordinate with ADR100 Amendment A1: `control.tool_begin` already
  derives tool approval from the pinned plan and checks fresh record versions.
  Deduplicate immutable IR storage only after retention/refcount design.
- Preserve R7a rate limits before expensive authentication DB/audit work and
  verify deployment edge/proxy quotas separately. Bounded security telemetry
  must still retain meaningful denial evidence. Separate immutable
  business audit from high-volume decision diagnostics, with retention, partition,
  export and deletion authority appropriate to each class.
- Diagnose the worker-policy receipt scan under the unchanged 2s SQL limit
  (96 receipts, 63 scope nodes): the earlier EXPLAIN timeout now has a failed
  initial SELECT in B, before EXPLAIN/late-lease assertions run. Preserve all
  failures, historical narrow-margin passes and the separate unconfirmed
  workflow connection failure. Include A's reproducible injected driver panic.
- Profile compilation, validator construction and catalog guards; cache only with
  explicit immutable keys and invalidation/revocation guarantees. Preserve startup
  and runtime privilege-drift detection.
- Preserve M10's implemented per-tool idempotency/retry/unknown/reconciliation
  contracts and crash-after-send/late-response regressions. Provider breadth,
  callback/compensation tools, reconciliation residuals and tool reviews remain
  with the milestone agent. Never claim universal exactly-once.

Proposed production DR gate: documented RPO ≤15 minutes and RTO ≤4 hours for the
initial deployment tier, followed by an executed restore drill using stored IR
without recompilation. These are proposed objectives, not current guarantees;
data residency/retention and deployment owners must approve the chosen tier.

## Remaining roadmap ownership

| Capability | Proposed owner / gate |
|---|---|
| SSO/OIDC/SAML and configurable clock skew | R5f implements and independently verifies the bounded OIDC/PKCE adapter and clock tolerance. R5g completes local role UI and deployment preparation. The user deferred external IdP/client deployment and production gateway/MFA verification until a provider/domain is selected; these remain an identity release gate. SAML only when required |
| Search and localization | Product/platform workstream before the first ERP Domain Pack release at M16; scope languages and search authorization explicitly |
| Residency, backup/restore and audit retention | Operations/security M15, with database prerequisites in R2/R7; production gate, not an optional future note |
| Import/export and portability format | M17; publish a versioned manifest, hash/compatibility rules and import round-trip tests before claiming portability |
| JSONB evolution / IR compatibility | R2, maintained by data/compiler owners; candidate M12 consumes their plans |
| Intent / acceptance / verified candidate | R6 assigns M14 capture, M12 verification, M13 authority; M11 supplies the sandbox |
| Large-scale performance | R3/R4 establish a usable floor now; M18 extends measured capacity rather than postponing foundation correctness |

## Delivery and completion discipline

Continue the existing `remediation/m9r-foundation` checkout and preserve the
merged code. Use small coherent commits, focused evidence and stable boundaries
for independent review. Main merge, PR creation and production exposure require
separate user tasks; a push is not acceptance or a main merge.

For each change record: failing pre-fix reproduction, implementation/ADR,
post-fix positive and negative tests, regression selection, threat model,
performance envelope, recovery rehearsal and residual gaps. Independent review
must use a stable commit with populated negative fixtures and actual attacks or
faults where claimed. A human reviews the concrete design and evidence before
production exposure; an AI review is not a substitute for that ownership.

R1–R4 completion requires the reproduced problems to be demonstrably fixed and
the existing M0–M9 backend/frontend/integration/browser suites to pass on the
final commit. Do not relabel deferred R5–R7 services as complete to pass this gate.
After each bounded slice, report gate results, commit and SHARED edits and stop.
M10–M14 remain preview work in the milestone lane. Independent review of the
merge/M14 is NOT RUN under the user's temporary scheduling deferral; preserve
that status rather than substituting builder-side tests.

**Current stop gate: M9R PARTIAL; combined acceptance FAIL.** Historical bounded
reviews remain evidence. Current failures are aggregate capacity, worker-policy
receipt-scan timing, injected driver-panic recovery and B's confirmed integration
gaps. R6 preview exists and stays milestone-owned/partial; R7 D1–D7 remain open.
Wider R2/R3/R5 product features and real IdP/MFA/HTTPS verification stay
request-only/deferred. Slice C reconciles documentation and proposes agent
changes; it does not resolve those code/operations gaps or authorize D ordering.
