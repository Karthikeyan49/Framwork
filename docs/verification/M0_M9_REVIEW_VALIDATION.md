# M0–M9 external review validation

Date: 2026-10-13. Reviewed commit: `81b88d2621408ecb65a08ca2f4fc2c29ccd732f7`.
Branch: `milestone/m9-workflow-runtime-v2`.
Evidence scope: validate the supplied review and produce a plan only. The user
subsequently authorized publishing this evidence/plan, then implementing M9R.
This report records the pre-remediation baseline; it does not claim fixes.

## Decision

The review identifies real foundation problems. **Pause M10 feature work until
the worker boundary, persistent data identity, and concurrent-write/query
budgets have passed a focused remediation gate.** Existing preview functionality
and passing unit tests do not establish production readiness or safety against
compromised worker credentials.

The validation below is complete within its stated scope. Remediation has not
started. Some historical statistics and broader production properties remain
unverified; those are explicitly identified rather than treated as passed.
See [the remediation plan](../migration/M9R_REMEDIATION_PLAN.md).

## Executed evidence

An ignored local Go probe created uniquely named disposable databases, applied
the repository's 21 migrations, seeded synthetic tenants/records, and connected
using the actual separate app and worker roles. Bootstrap/operator credentials
were used only for fixture administration and the benchmark control. The
credential-access probe used the worker connection alone after seeding. No
credentials, tokens, or real customer payloads are included in the evidence.

All completed probe runs dropped their databases. The earlier fixture setup
failure also dropped its database. Writes used to demonstrate unauthorized
record access were rolled back. No application source, tracked tests,
configuration, dependency manifests, or migration files changed.

Evidence: [probe source](evidence/2026-10-13/probe.go.txt) and
[final output including query plans](evidence/2026-10-13/results.txt).

Independent review found that the initial probe changed URI paths without
checking whether driver query parameters override the target database. The
original executed source/output are preserved as evidence, not recommended as
a reusable safety boundary. The [hardened probe](evidence/2026-10-13/probe-safe.go.txt)
checks effective parsed database/role before connecting and verifies
`current_database()`, session user and current user before any fixture operation.
Its [repeat-run evidence](evidence/2026-10-13/results-safe.txt) records each target
check and reproduces the findings. Use that version for future local reproduction.

### Worker credential access: reproduced, HIGH

With no context the worker saw **0 records**. Using only permitted worker SQL,
it claimed a globally due job, obtained the routed actor's identity, and supplied
the session context that the database accepts. It then read **all 5 records**
in that tenant, updated **1 unrelated record**, and deleted **1 other unrelated
record**. The job referenced neither mutated record. The mutation transaction
was rolled back and the five original records remained.

Fixture provenance: a synthetic binding with actual compiled IR/plan was inserted
through normal app-role SQL, which creates the job route. This was not the HTTP
workflow-start path and did not seed an instance row. The attack then used the
worker role and the normal routing/identity functions.

This proves excessive authority for a compromised shared worker credential.
It does not mean a context-free SELECT bypasses RLS. The role can obtain a valid
tenant context and exercise authority beyond its job's resource.

The chain is `control.claim_workflow` (migration 21), `iam.worker_identity`,
`iam.worker_tenant_scope`, and broad business-record worker grants (migration
10), with identity GUC matching from migration 4. The business-record policy
does not bind a lease token, generation, app, release, entity, record, or allowed
operation. Broad record authority predates M9; M9's global claim makes the
discovery path available through the same credential.

The existing `TestM9WorkerIsolationRolesAndRevocation` exercises the trusted Go
job path. Its cross-tenant workflow-binding query has no populated binding in
the other tenant. It therefore does not establish the stronger credential
compromise property. Prior M9 isolation PASS wording must be read with this new
limitation; it is not sufficient evidence for V3 invariant 9.

A separate app-role probe inserted hash-consistent `{}` IR and plan bytes into
a workflow binding. PostgreSQL accepted the binding; `compiler.Restore`
rejected those bytes. **Database hash integrity is not compiler admission.**
This did not demonstrate execution of a forged program.

### Record continuity: reproduced

For one tenant/app, revision 1 and the original IR returned **5 records**.
Revision 2 returned **0**. Recompiling the same authoring source with a changed
catalog version produced a different IR hash and returned **0** at revision 1.
The original namespace still contained all five records.

`runtime.record_sets` and `runtime.records` identify datasets by tenant, app,
metadata revision and IR hash; `data.Scope` and `recordWhere` use those keys.
This is intentional preview separation but cannot be adopted unchanged as
production persistence. The result demonstrates namespace invisibility, not
deletion. A binary upgrade does not necessarily change a legacy IR hash:
stored releases are restored, and compiler version handling is versioned.

### Concurrent writes: reproduced at the database boundary

The exact `lockDataset` counter UPSERT from `data/store.go` was run through two
app-role REPEATABLE READ transactions with the runtime's 500 ms lock timeout:

- An overlapping held counter update caused SQLSTATE **55P03** after **503.652 ms**.
- A transaction with a snapshot preceding the first writer's commit failed with
  SQLSTATE **40001** when updating that counter.

All CRUD commands invoke this counter, so unrelated records in the same dataset
share the lock. Source inspection found no business-transaction retry; unexpected
transaction errors become HTTP 503 in `access.go`. The HTTP response itself was
not replayed in this probe. The counter currently protects reference/delete
races; deleting it without replacement would remove an integrity guarantee.

The report's claim that shipped M9 activities already contend on this counter
needs correction: `local.noop` does not call `data.Store.Command`. Future business
activities using that store would encounter the same bottleneck.

### RLS query cost: reproduced; original absolute numbers not reproduced

Environment: PostgreSQL **18.6**, local Docker; host reports 5 logical Xeon
8573C CPUs and approximately 34 GB RAM. These are visible host resources, not
a production capacity guarantee. The review's original measurements used PG16.

The fixture contains 20,000 then 100,000 records in one dataset plus five in
another tenant. Queries select 101 IDs with tenant/app/revision/hash/entity
predicates. The numeric query sorts a JSONB money amount; the key query sorts
the physical UUID key. After ANALYZE, diagnostic/control/key medians used three
sequential samples; each runtime-deadline check used one sample. These are SQL timings, excluding HTTP, compiler and
per-transaction catalog checks, not end-to-end latency or a load-test percentile.

| Dataset | Numeric sort with RLS, 30 s diagnostic budget | Same role, RLS disabled in disposable fixture | Physical-key page with RLS | Numeric sort with runtime 2 s budget |
|---|---:|---:|---:|---|
| 20,000 | 456.323 ms | 12.060 ms | 2.505 ms | 451.942 ms; returned 101 |
| 100,000 | 2,217.904 ms | 34.681 ms | 2.469 ms | cancelled at 2,000.585 ms; SQLSTATE 57014 |

RLS was disabled only for the controlled same-role SQL comparison inside the
owned disposable database, then re-enabled. This is not a proposed optimization.
The normal Go transaction guard would reject disabled RLS.

EXPLAIN shows a sequential scan/top-N sort for the numeric query and an index
scan stopping at 101 records for the key query. Both retain
`iam.tenant_matches_scope(tenant_id)` as a row filter. This supports the reported
per-row authorization cost and explains why fast key pagination masks it.
The original 20k/2.35 s, 100k/9.98 s, and 100 microseconds per row figures are
**not confirmed on this environment**. The timeout and scaling mechanism are.

An initial diagnostic used unqualified `ORDER BY id` with a text-cast output
alias, which prevented the intended UUID-key comparison. The probe was corrected
to qualify the physical key and rerun. Only the final qualified-key results
above are used. The numeric tie-breaker is a text ID in this probe; this is a
boundary-level diagnostic, not byte-for-byte execution of the full API query.

## Claim disposition

"Source confirmed" means inspected code supports the statement; it does not
mean a fresh end-to-end test was executed. Deferred functionality still needs
an owner and an acceptance gate before production.

| Review area | Assessment and practical consequence |
|---|---|
| No production data/workflow service | Source confirmed. Data execution is under preview routes; configuration refuses production preview events/workflows. Production activation/services remain M13. The repository is foundations, not a deployable ERP product. |
| M0 single CI job and no deployment pipeline | Confirmed in `.github/workflows/ci.yml`: one foundation job, with checks/tests/builds and no deployment stage. Separate deploy/rollback infrastructure is a production operations deliverable. |
| Promotion/intent/sandbox/tool-delivery contracts only | Substantially confirmed. `cmd/release-check` is a real compatibility gate, but no public Stage/Approve/Activate service is composed. This is unfinished authority integration, not a demonstrated public promotion bypass. |
| Immutable releases and workflow pins | Existing code restores stored IR and pins workflow inputs. Preserve this. No production data runtime consumes the active release yet. |
| HighRisk, PASS and independence are caller claims | Confirmed at the trusted bundle-builder boundary. Hashes bind content, not business truth. There is no independent executable acceptance attestation or risk classifier. |
| RequesterID is filled by the bundle builder | Confirmed, with a downstream protection: `promotion.Store.Stage` requires bundle RequesterID to match the trusted actor. High-risk classification and verification provenance still need authoritative services. |
| Intent is narrow and ambiguity can pass as optional | Confirmed. The shape is a money/comparison/approval requirement. Mandatory unresolved items are rejected; optional unresolved items pass. Trusted ownership of classification and required completeness is missing. |
| Acceptance cases are free text; author inequality absent | Confirmed. A different author alone would not prove independence. V3 requires independent derivation; requester/approver separation is explicitly required for high-risk promotion. Define these separately. |
| DataScope has no product policy | Confirmed. Native returns ALL for permitted built-in roles; SQL OWNER/TEAM/SHARE/CONDITION machinery exists. Tenant checks remain enforced. Product management of ownership, teams and shares is absent. |
| Field scopes filter rows and writes need every field | Confirmed. Projection, filter, sort and join fields influence SQL visibility; commands require all-field access. Safe partial editing/masking is not implemented. |
| Four roles; finance_manager example fails | Confirmed. IAM role CHECK and workflow approval validation restrict roles to owner/admin/member/viewer. The V3 example is not executable without a custom-role design. |
| JWT clock skew and token-paste login | Source confirmed: direct timestamp checks have no configured leeway; the development UI accepts a bearer token. Production identity UX/provider onboarding remains unfinished. |
| No indexes or uniqueness support | Partly overstated. Primary-key and operational indexes exist. Custom-field index governance and `unique_projections` maintenance are absent; metadata has no general unique/default/validation contract. |
| Bounded compiler/data/UI | Source confirmed: seven definition kinds, no base overlay compilation, migration NONE and generated-test labels; one join and bounded Go aggregates; large preview UI component. These are scope limits, not evidence that every existing path is incorrect. |
| Aggregates run in Go with a 100-row bound | Confirmed. Over-budget aggregation returns a budget error. SQL aggregation and broader query planning remain necessary for scale. |
| Rules have no consumers | Incorrect as of M9. Workflow transition conditions evaluate rule plans; the preview rule endpoint also evaluates them. Business tools have not been implemented. |
| M8 receipt deadlines and dead predecessor blocking | Source confirmed. Five attempts and five-second receipt deadlines are brittle for outages. Exactly 25 seconds is only an approximation and depends on dispatch/publish/retry scheduling. DEAD blocks ordered successors within the aggregate epoch. |
| M8 single consumer, identifiers-only event | Confirmed and documented as preview receipts. General subscriber contracts and richer/versioned payload semantics remain future work. |
| 87% of 4,178 audit rows were checks | Not independently reproduced. Per-field/scope checks append audit evidence; immutable audit has no partition/retention lifecycle. The exact ratio is workload-specific and the original dataset was not supplied. |
| M9 DAG, noop, timer/lease/history bounds | Source confirmed, documented. One-second activities, five-second leases, seven-day instances, one-day timers and 1,000 history entries constrain business use. No lease renewal or revise/resubmit cycle is implemented. |
| Approval uses stale record snapshot | Confirmed semantics. Signals bind workflow instance state/version, not current record version. Define snapshot approval versus current-document approval before implementing business side effects. |
| Full IR and plan copied per instance | Confirmed. Storage/write-amplification impact not measured here. Immutable content-addressed shared artifacts are a future design option. |
| Universal exactly-once / AI workflow claims | No universal guarantee should be made. Unknown-outcome/reconciliation contracts exist; external delivery and AI are unimplemented. Keep them explicitly deferred. |
| No rate limiter; invalid auth consumes DB/audit | Confirmed for protected API handlers. It is not literally every route: health/static/unknown routes differ. Redis in API composition is a readiness dependency, not a limiter. |
| Preview recompiles; Restore rebuilds validators; per-tx role catalog checks | Source confirmed. Performance contribution was not separately profiled. Caching must preserve version binding, revocation, and safe role-drift handling. |
| Atomic audit/outbox and owner boundaries | Supported by existing transaction composition/migrations; preserve. This validation did not repeat all prior crash/rollback/security integration tests. |
| Import boundaries | A real security test exists and is included in the executed unit/security command recorded below. It does not prove every semantic module boundary. |
| Two rulebooks | V3 precedence exists, but no clause-level supersession map. Specific conflicts include V2 M8/M10 numbering, approval order, custom-field generated columns, and backup instructions that recompile active metadata. |
| V3 portability has no concrete specification or proof | Confirmed gap. Retained V2 export/import requirements remain unchanged and V3 M17 schedules portability. A concrete portable format and verified implementation are absent. Scheduling the work does not substantiate a portability claim. |
| Missing migration/IR upgrade ownership and numeric gates | Confirmed planning gaps. JSONB transformations, compatibility lanes and measurable verification need explicit ownership; production cannot rely on NONE indefinitely. |
| SSO/search/localization/residency/import/DR scheduling | Incomplete specificity. Assign subprojects and release gates; the report overstates total absence for portability, which is scheduled at M17. |

Source anchors: `internal/platform/access/{access,preview,rules}.go`,
`internal/platform/app/app.go`, `internal/platform/postgres/{postgres,transaction}.go`,
`internal/runtime/data/{store,query,scope}.go`,
`internal/runtime/workflow/{plan,contracts,signals,store}.go`,
`internal/policy/scope.go`, `internal/intent/intent.go`,
`internal/promotion/{bundle,store,compatibility}.go`,
`internal/compiler/{compiler,restore}.go`, and migrations 2, 4, 9–11, 18–21.

## Process and repository findings

Remote refs were checked using `git ls-remote`. Main remains at
`9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7` (M6); M6R, M7, M8 and M9 are stacked
branches. Remote M9 matches the reviewed commit. "Pushed" did not mean "merged
into main." Nothing was merged or pushed during this validation.

`git diff --shortstat main HEAD` reports 204 files, 19,233 insertions and 310
deletions. Feature commit timestamps run from 09:04 to 18:13 UTC on October 12.
This confirms large review batches; it does not quantify a residual defect rate.
The M9 review is 575 words and explicitly records no independent pre-fix exploit
and a transient compile failure during concurrent authoring. M6R records eight
HIGH findings. The exact all-milestone HIGH total was not independently tallied.

AGENTS.md and CLAUDE.md total 1,519 words; the audit contains `exactlease`.
Consolidation and copy editing belong in a documentation-only change, preserving
source-kit checksums. The historic M2–M6 line count and every duplicated paragraph
were not independently audited. These housekeeping items do not rank above the
reproduced security/data/concurrency issues.

Independent source review was performed by the existing reviewer agent, including
the credential path, promotion authority, policies, workflow bounds and V2/V3
conflicts. It was source-only; the root agent executed the disposable probes.

## Commands and results

| Actual command/check | Result |
|---|---|
| `git status --short --branch` | PASS; clean reviewed implementation before documentation work |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps` | PASS; PostgreSQL/Redis healthy; collector running |
| `make migration-status docs-verify` | PASS earlier in this validation; 21 applied migrations; 84 V2 and 43 V3 source checksums |
| `./scripts/go.sh fmt ./.local/audit_2026_10_13` | PASS; local probe formatting only |
| `node scripts/with-env.mjs ./scripts/go.sh run .local/audit_2026_10_13/probe.go` | Initial FAIL: fixture UUID/text parameter typing (42P08), cleanup passed. After probe correction PASS; rerun with corrected key sort and EXPLAIN also PASS. Reproduced invariants fail as described above. |
| `make test-unit` | PASS; Go race-enabled package checks (most Go results cached), security tests, 4 Node tests and 28 frontend tests |
| `node scripts/with-env.mjs ./scripts/go.sh run .local/audit_2026_10_13/probe-safe.go` | PASS; verified effective connection targets, all findings reproduced and fixture database dropped |
| `./scripts/go.sh test -race -count=1 ./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...` | PASS; Go package and security tests rerun without the test-result cache |
| `make check` | PASS; 127 source-document checksums, Go format/vet/module verification, frontend lint and typecheck |
| `git ls-remote --heads origin main migration/v3-m6r milestone/m7-rule-engine-v2 milestone/m8-outbox-inbox milestone/m9-workflow-runtime-v2` | PASS; remote branch status confirmed |
| `git diff --shortstat main HEAD`, `git log --format='%h %ad %s' --date=iso-strict main..HEAD` | PASS; batch size/timestamps above |
| Full integration suite, browser E2E, dedicated backend/frontend build, production deployment, external side-effect tests | NOT RUN in this validation; no claim that historic executions re-certify the new findings |
| Original PG16 timings, exact audit percentage, sustained load and full-HTTP concurrent writes | NOT RUN; SQL reproductions and source mapping have the explicit limits above |

To reproduce after normal local setup, copy the hardened evidence source
to `.local/audit_2026_10_13/probe-safe.go` and run its command above from the repository
root. It requires explicit development/test configuration, local database
bindings, the existing local role credentials, and bootstrap permission to create
and drop its own database. Do not point it at a shared or production database.

Final implementation status: **unchanged, foundations only; remediation not
implemented**. The new documents supersede broad interpretations of prior PASS
labels, without modifying historical evidence or architecture ZIP contents.

Git delivery branch: `remediation/m9r-foundation`. Publish these six documentation
and evidence files as a separate checkpoint before implementation. Scratch Go
probes remain ignored under `.local/`; executed source is archived as text. No
existing milestone branch or main was merged during validation.
