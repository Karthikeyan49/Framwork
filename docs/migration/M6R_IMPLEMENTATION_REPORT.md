# V3 migration and M6R implementation report

Date: 2026-10-12 UTC.

**M6R STATUS: PASS for the V3 M6R architecture-remediation milestone.**
The framework is safe to proceed to **M7 — Rule Engine V2 development** when
requested. M7 was not started. This is not certification of the complete V3
production framework: the later sandbox, delivery, workflow, AI and promotion
services and human production sign-off remain outstanding.

## 1. Original repository and preservation

Repository: `Karthikeyan49/Framwork`, `/workspace/Framwork`. The starting branch was
`main`, HEAD `9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7`, with a clean working tree.
Before implementation, a dedicated `migration/v3-m6r` branch and annotated local
checkpoint tag `pre-v3-m6r-2026-10-12` were created at that commit. At the initial
implementation handoff, HEAD still identified that original commit and the changes
were uncommitted, unmerged and unpushed. The checkpoint permanently identifies the
original source; subsequent Git publication is recorded in section 12.

The existing Go modular monolith, React/TypeScript frontend, authentication,
tenant transaction boundary, metadata registry, compiler, preview runtime,
observability and CI were retained. The compatibility audit was written before
implementation changes. Original migrations 00001–00008 and the supplied V2
documentation are unchanged. The previous assertion that M6 was complete was
checked against code and tests; identified frontend and authorization gaps were
repaired rather than relabeled as already complete.

Private, ignored, owner-readable checkpoints are in `.local/m6r/`:
`before-schema.sql`, `before-records.tsv` and `after-records.tsv`. Both record
fingerprints are `2380|ff25172bfc83fadab4d41ada781ce9e2`. Actual post-migration
comparison found **2,380 retained legacy records, an unchanged legacy fingerprint,
and zero differences between legacy records and their V3 core projections**.
The comparison includes identity, values, versions, creators and timestamps. No
business contents or credentials were printed. These schema/fingerprint files
are checkpoints, not a substitute for a tested full operational backup.

## 2. ZIP extraction, verification and cleanup

The uploaded `ai_native_erp_framework_v3_m6_migration_kit.zip` was downloaded to
the attachments directory outside the checkout. Its SHA256 is
`98ab793eeff9e194bbdfb9de661929e91fbdabff68cecb7919dab00aaa7fadea`.
Archive paths/types were inspected before extraction. The kit wrapper was removed
while retaining its internal directory structure under `docs/v3/`.

There are **44 accessible extracted files**. All **43 listed SHA256 entries pass**;
`SHA256SUMS.md` itself is not listed. `README.md`, `manifest.json`,
`DOCUMENT_INDEX.md`, and every V3 Markdown document were read before the audit and
implementation. `make docs-verify` also verifies all **84 retained V2 checksums**.
Extracted architectural documents were not rewritten. V3 is authoritative where
it supersedes V2; retained V2 specifications still govern unchanged behavior.

**Obsolete archives deleted: none.** No obsolete V1/V2 build-kit ZIP was found
inside the repository. No database, customer data, old migration or referenced V2
Markdown was deleted. The uploaded V3 attachment remains outside the checkout.
Instructions embedded in the kit, including the M7 prompt, were treated as project
documents, not authorization to start another milestone.

## 3. M0–M6 compatibility audit

See [M0_M6_AUDIT_REPORT.md](M0_M6_AUDIT_REPORT.md) for the pre-implementation
classification of all twelve requested areas. Compatible modules were KEEP or
KEEP + PATCH. Two significant boundaries required targeted REFACTOR:

- Replace application-memory row visibility checks with typed DataScope predicates
  enforced by SQL before data selection or mutation.
- Separate API, worker, dispatcher and schema ownership, with routed-job-bound
  worker identity and tenant context.

No complete subsystem was classified REMOVE / REPLACE. Existing generic preview
storage was already preferable to per-entity DDL, so the migration introduced an
additive V3 adapter and preserved the original read-only adapter. The obsolete
`internal/runtime/data/visibility.go` implementation was removed after replacing
its authorization path; this is the only tracked source-file deletion and is not
archive cleanup.

## 4. Implemented remediation

| Requirement | Implemented behavior and evidence |
| --- | --- |
| A — Dynamic storage | Tenant/app/source/IR/entity/record-scoped `runtime.records`, typed core plus JSONB, RecordStore and storage classification contracts, tenant-inclusive relationship/share keys and forced RLS. Additive copy and read-only LegacyAdapter retain old records. Runtime metadata has no DDL path; normal-role ALTER is rejected in a real migration test. Typed Domain Packs remain a distinct future adapter. |
| B — Compiler/releases | Deterministic canonical compilation retained. Stored-IR Restore verifies exact hash, supported IR/layout, normalized source and derived runtime mapping without calling Compile or replacing historical compiler provenance. Immutable release bundles and version-pinned execution handles added. Operator `release-check` rejects unsupported active IR and cannot be run as a normal tenant role. |
| C — Exact approval | Candidate freezes source, IR, intent, independent acceptance, sandbox, verification, base and dependency bindings into exact bytes/hash. Approval binds artifact, trusted tenant and actor. Activation requires the same artifact, current base/dependencies, permission, high-risk requester/approver separation and generation CAS. Persisted stage/approve/activate/rollback transitions are exercised. No public production promotion endpoint was added. |
| D — Intent | Typed, bounded V1 monetary approval-rule requirements and separately authored acceptance specifications. Mandatory ambiguity, unresolved values, invalid comparisons/currency, missing coverage, source/IR mismatch and wrong monetary field block candidate admission. Verification provenance is a trusted composition port; a client/AI PASS assertion is not an activation capability. |
| E — DataScope | ALL/NONE/OWNER/TEAM/SHARE/CONDITION and bounded AND/OR contracts. Live team membership includes descendants and explicitly rejects overflow/cycles. SQL intersects entity and used field scopes before filtering, ordering, joins, aggregate input, export/pagination and mutation. Existing/proposed command states are checked. Unknown/missing scope capability fails closed; row-read action permission is also mandatory. |
| F — Worker/roles | NOLOGIN schema owner, retained API/migrator aliases, separate worker/dispatcher logins, reserved NOLOGIN audit/maintenance groups, role/TLS drift guards and curated child-process credentials. Private dispatcher tickets bind live actor, tenant, job and release; RLS and restrictive worker IAM policies enforce that scope. Dual-tenant actor, GUC spoofing, stale tickets, dispatcher data denial, audit provenance and pool cleanup tests pass. |
| G — Side effects | Tool-specific NATIVE_KEY/LOOKUP_BY_REFERENCE/DEDUPE_WINDOW/NONE, retry, unknown-outcome, reconciliation and compensation contracts. Unreconciled unknown delivery cannot bypass RECONCILE_FIRST. No remote delivery executor or universal exactly-once claim. |
| H — ApplicationPatch | Typed patch operations, base release/IR and exact definition hashes, stale/conflict detection and three-way merge. Automatic merge is restricted to nonconflicting presentation changes; sensitive definition or class changes require explicit resolution. Final source remains schema validated and must subsequently compile and obtain approval. |
| I — Audit/outbox | API business mutations, mandatory audit and outbox intent share the same WithRuntime transaction. Audit or outbox failure rolls back all three. No commit-then-publish operation or network publication. Immutable release/approval evidence and restricted database-stamped audit provenance added. |
| J — ERP types | Fixed decimal Money, reviewed currency/minor units, positive bounded immutable FX snapshots, UTC/IANA time zones, business calendar and draft/final document-number contracts. Final allocation/posting requires the same transaction port. No accounting/inventory engine or numbering allocator. |

Sandbox Context admits only explicit safe data/tool modes and refuses production
credential mounting. It is a tested isolation contract; M11 will implement the
sandbox manager. Existing UI primitives/pages were preserved. Frontend corrections
cover pending navigation, uncertain write outcomes across pages, explicit recovery,
post-save reload failure, detail-only pages, refreshed relationship choices and
long mobile headings/badges. Historical compiler provenance is independent of
supported IR/component version. Empty/overwide/deep/oversized renderer definitions
are rejected at compiler admission instead of failing in the browser.

Existing native roles retain their explicitly defined tenant-wide access. Narrower
ownership/team/sharing policies enter through trusted ScopeEngine adapters, not
request-supplied scope expressions. A customer permission-design UI is not included.

## 5. Files, repository structure and architecture decisions

Major additions and changes:

```text
AGENTS.md, CLAUDE.md, README.md, Makefile, .env.example
cmd/release-check/main.go
docs/v3/                              44 verified source-of-truth files
docs/adr/020-v3-m6r-architecture-remediation.md
docs/migration/{M0_M6_AUDIT_REPORT,M6R_INDEPENDENT_REVIEW,M6R_IMPLEMENTATION_REPORT}.md
db/migrations/00009_*.sql ... 00016_*.sql
infra/compose/v3-role-bootstrap.sql
internal/compiler/{compiler,restore,source_binding}.go
internal/policy/{policy,scope,scope_error}.go
internal/runtime/data/                Generic/legacy adapters and SQL scope enforcement
internal/{identity,tenant}/           Routed worker identity and live team resolution
internal/platform/{access,app,postgres,jobs}/
internal/promotion/                   Bundle, approval, persisted activation and deployment gate
internal/{intent,events,sandbox,tools,erp}/
apps/web/src/preview/{Workspace,contracts}.*, style.css, e2e/preview.spec.ts
scripts/{m6r-env,m6r-roles,runtime-env,with-env,dev,smoke,verify-docs}.mjs
scripts/runtime-env.test.mjs
tests/integration/m6r_{scope,adversarial,migration,release}_test.go
```

An exact Git change manifest appears at the end of this report. New
[ADR-020](../adr/020-v3-m6r-architecture-remediation.md) records V3 storage cutover,
preserved IR representation, SQL DataScope, local role names/ownership, immutable
bundle admission, narrow M6R contracts and recovery decisions. Historical ADRs and
source documents remain available; ADR-020 explicitly supersedes application-memory
visibility. No new production Go/npm dependency, module version, package lock or
container image was introduced. Standard-library `time/tzdata` supplies portable
IANA data. Pinned one-off Prettier 3.9.10 was used for formatting only.

## 6. Database migrations

All eight new forward migrations are applied; `make migration-status` confirms
the complete 00001–00016 chain. There is no exposed destructive Down command.

| Migration | Purpose |
| --- | --- |
| 00009_v3_dynamic_records.sql | Add generic records/datasets/links/shares and tenant keys/RLS; copy legacy projections without deleting legacy data. |
| 00010_v3_roles_and_teams.sql | Transfer protected object ownership to NOLOGIN owner; separate roles, team hierarchy and guarded routed-worker context/identity. |
| 00011_v3_release_and_outbox_contracts.sql | Immutable exact-byte bundles/approvals, approved active pointer/CAS, transaction-local outbox intents and actor stamping. |
| 00012_v3_audit_provenance.sql | Replace broad audit INSERT with evidence-column grants; preserve DB-stamped writer and timestamps. |
| 00013_v3_legacy_cutover_barrier.sql | Wait/reconcile late legacy writers; reject divergent state without overwrite. |
| 00014_v3_legacy_column_privileges.sql | Seal legacy column ACLs as well as table ACLs; add tenant-inclusive owner-team FK. |
| 00015_v3_final_cutover_barrier.sql | Recheck/reconcile after complete column-privilege sealing. |
| 00016_v3_worker_identity_scope.sql | Restrictive job-bound worker IAM, tenant, team and membership SELECT policies. |

Forward follow-ups were used once earlier migrations had been applied locally;
applied files were not rewritten. A disposable database rehearses the original
chain, holds a legacy write across cutover, observes migration locking, commits the
writer and verifies copied read equivalence, ACL sealing and denied shared DDL.
An independent overlay introduces divergence and proves rejection without
overwriting the destination. The original concern that migration 00009 could miss
an uncommitted insert was investigated but **not confirmed as an exploit**: its
policy DDL already waits on the old writer. Final barriers and writer quiescence
provide an explicit operational cutover boundary.

## 7. Executed commands and results

Commands are recorded as executed, not suggested. Unit output can use Go's valid
test cache; integration uses `-count=1` and was rerun against PostgreSQL/Redis.
Reviewer's commands/results are separately attributed in the independent report.

| Command | Final result |
| --- | --- |
| `make roles-init` | PASS: development-only dedicated bindings and role bootstrap, without printing passwords. |
| `make migrate` | PASS: full forward chain through 00016. Earlier failure described below. |
| `make migration-status` | PASS: all sixteen migrations applied. |
| `make docs-verify` | PASS: 84 V2 and 43 V3 checksums. Also rerun by `make check`. |
| `make fmt` | PASS: Go formatting completed. |
| `make check` | PASS: checksums, Go format/vet, module verification, ESLint and TypeScript typecheck. |
| `make test` | PASS: Go race unit/security checks, 2 Node tests, 27 frontend tests and full fresh race integration suite; final integration duration 28.343s. |
| `make build` | PASS: API, worker, migrate, IAM, release-check binaries and TypeScript/Vite frontend. |
| `make test-e2e` | PASS: all 4 Chromium tests, final run 14.0s; real CRUD/CAS recovery, read-only policy, credential redaction and mobile long-content layout. |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS: live health/readiness, PostgreSQL/Redis/IAM, frontend proxy, auth denial, CORS, trace correlation, Collector traces/runtime metrics and graceful API/worker exit. |
| `make release-check` | PASS: actual operator compatibility inventory, without recompilation. Unsupported active IR and runtime-role denial also exercised in integration. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestIndependentM6RWorkerRoutingAndPool$' ./tests/integration` | PASS: strengthened worker isolation with actual legitimate second-tenant actor membership; 1.724s. Also independently rerun and included in final full suite. |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps` | PASS: PostgreSQL 18.6 and Redis 8.2.3 healthy; Collector 0.148.0 running. |
| `cmp .local/m6r/before-records.tsv .local/m6r/after-records.tsv` | PASS: preserved legacy count/fingerprint equal. Independent actual legacy/V3 projection comparison also passes. |
| `git diff --exit-code pre-v3-m6r-2026-10-12 -- 'db/migrations/0000[1-8]*'` | PASS: original migration files unchanged. |
| `git diff --exit-code -- go.mod go.sum apps/web/package.json apps/web/package-lock.json` | PASS: dependency declarations/locks unchanged. |
| `git diff --check` | PASS: no whitespace errors. |
| `sha256sum /workspace/attachments/497cff01-d9c6-4c3c-b55a-f53f7cc3877a/ai_native_erp_framework_v3_m6_migration_kit.zip` | PASS: attachment digest recorded above. |
| `rg --files --hidden -g '*.zip' -g '!.git' -g '!node_modules' -g '!.local'` | No archive matches in checkout; no deletion performed. Empty matches are rg exit 1, not a failed checksum. |
| Real production deployment, customer backup restoration, external side effects, complete V3 global release gate/human sign-off | NOT RUN: outside this M6R development remediation. |

The existing foundation integration suite also executes startup with a closed
Collector endpoint, dependency-unavailable readiness, tenant RLS/role drift,
authentication and audit failures. Existing compiler determinism/golden, metadata
compatibility, query/CAS/reference and frontend regressions remain enabled.

### Failures encountered, corrections and reruns

Failures were not suppressed or counted as passes. Initial migration 00010 failed
with insufficient schema CREATE privileges for the new owner; its transaction
rolled back. Required controlled ownership grants were corrected before applying
that migration, and the complete chain subsequently passed. Initial Go compilation
failed on a scope constant name; corrected before testing. Integration exposed a
link column-name mismatch and collection row-action semantics returning 503/403;
both were corrected and the full suite rerun. An initial migration-test harness
waited for a migration before releasing its held writer and timed out; asynchronous
ordering was corrected, and the rehearsal now passes.

Independent review reproduced field-scope disclosure/mutation, ignored row-action
denial, writable audit provenance, unvalidated worker connection overrides,
tampered bundle admission and built-binary credential inheritance. Those HIGH
findings were fixed and independently retested. Nullable proposed scope originally
returned 503; it now explicitly denies with 403. Unsafe reconciliation-first retry,
quoted production helper configuration and silent hierarchy truncation were also
corrected. The final review has no unresolved BLOCKER/HIGH or confirmed MEDIUM.

Other intermediate helper compilation/quoting/path errors were corrected before
the final successful checks. A stale independent configuration overlay failed to
compile after its test was adopted under the same name; the reviewer reran the
permanent repository test successfully. During a temporary execution transport
disconnect, interrupted commands were not recorded as passes; verification was
completed after recovery. See the independent report for actual reproduction and
closure evidence rather than relying only on this summary.

## 8. Security findings and independent review

[M6R_INDEPENDENT_REVIEW.md](M6R_INDEPENDENT_REVIEW.md) is the independent review
deliverable. The reviewer executed adversarial race integration, integrity/retry
tests, worker/dispatcher/context boundaries, configuration trust, migration
preservation/divergence and deployment compatibility checks. No implementation
files were edited by that reviewer and no test service was left running.

Important enforced boundaries are default deny, verified tenant context, SQL scope
before disclosure, forced RLS, tenant-inclusive FKs, non-owning runtime roles,
job-bound worker IAM, restricted audit provenance, production TLS and exact-byte
approval. Runtime child environments exclude operator and dispatcher bindings;
frontend children exclude database/Redis credentials. `.env`, identity keys,
private checkpoints, logs, dependencies, binaries and browser/build artifacts remain
ignored. `.env.example` contains blank secret bindings, not production defaults.

Remaining LOW verification limits: the independent reviewer did not separately
execute every hierarchy depth/cycle/count negative or every privileged-role drift
permutation; those rejection paths were inspected, with normal hierarchy and real
worker boundaries exercised. Browser verification is implementer evidence rather
than an independent accessibility certification. Production requires the release
compatibility CLI in its deployment procedure and reviewed provisioning/backup.

## 9. M6R acceptance checklist

This maps every item in `docs/v3/verification/90_M6R_ACCEPTANCE.md` to implemented
M6R evidence. Original kit checkboxes are preserved verbatim, not altered.

| Acceptance criterion | Result / evidence |
| --- | --- |
| M0–M6 audit exists | PASS — pre-code audit report with all requested areas. |
| Custom metadata cannot alter shared business schema | PASS — typed runtime, no metadata DDL, real normal-role ALTER denial. |
| V3 RecordStore | PASS — generic adapter and real CRUD/query tests. |
| Domain Pack classification | PASS — separate typed storage classification; no tenant redefinition. |
| IR/release compatibility | PASS — supported-version Restore and operator deployment gate. |
| Active release immutable IR | PASS — immutable exact-byte stored bundle plus approved active pointer; persisted tests. |
| Runtime supported IR versions | PASS — explicit IR 1.0.0 support, unsupported version denial. |
| Exact candidate-hash approval | PASS — immutable private candidate/approval and same-hash activation. |
| Stale candidate invalidation | PASS — base/dependency/tenant/action/CAS admission negatives. |
| Intent Contract | PASS — typed mandatory ambiguity/resolution checks. |
| Independent Business Acceptance Spec | PASS — separate author/provenance/coverage and bound intent evidence. Execution service deferred. |
| DataScope | PASS — owner/team/descendant/share/condition SQL tests, joins/fields/commands and aggregates/export. |
| DB role taxonomy | PASS — non-login owner plus controlled migrator/API/worker/dispatcher/reserved groups. |
| Worker tenant-context flow | PASS — dual-membership routed job, spoof denial, stale binding and pool cleanup. |
| Tool delivery/idempotency semantics | PASS — capability/retry/unknown/reconciliation value tests; no remote executor. |
| Sandbox isolation contract | PASS — admitted modes and no production credentials. |
| ApplicationPatch/base/rebase | PASS — typed base/hash conflict and presentation-only automatic merge tests. |
| Transactional audit/outbox | PASS — real mutation/audit/outbox commit and injected all-or-nothing failures. |
| Money/time/calendar/number types | PASS — value/transaction-port boundary tests. |
| Legacy migration path | PASS — retained adapter/tables, additive copy, held writer/divergence/ACL rehearsal and unchanged 2,380-row checkpoint. |
| No unresolved BLOCKER/HIGH | PASS — completed independent review and exercised closures. |

## 10. Remaining architecture and verification boundaries

M6R completes foundations, not later services. Rules are M7; outbox dispatcher and
consumer inbox/dedupe are M8; scheduler/durable workflow execution are M9; remote
governed tools/connectors and actual crash/timeout/delivery reconciliation are M10;
sandbox orchestration is M11; full candidate assembly is M12; public human approval/
promotion services are M13; AI builder and broader intent generation are M14.
Complete ERP Domain Packs/posting/inventory and optional extensions are later work.

The current intent contract deliberately covers the documented monetary approval
subset, not arbitrary natural-language requirements. Currency catalog coverage is
limited to the ten reviewed currencies in the contract. Final numbering allocation
and controlled maintenance execution are ports/reserved roles, not implementations.
There is no release cache to invalidate and no workflow executor; execution-pin
tests verify the version contract rather than claiming a live workflow rollback.

Independent acceptance/report admission does not execute ERP acceptance tests.
Future trusted verification must supply independently executed evidence; production
must not accept client/AI-supplied evidence. Likewise delivery tests exercise the
declared retry decisions, not actual remote crash-after-response behavior. Those
later-service checks and the full `99_V3_RELEASE_GATE.md` are **NOT COMPLETE**.
Human architecture/security production sign-off is **NOT RUN**. This does not block
M7 development under the completed M6R foundation, but prohibits treating this
report as complete V3 production approval.

## 11. Recovery and rollback instructions

1. Before an existing deployment's cutover, stop all legacy API/worker writers.
   Take an encrypted full database backup and validate restoration in a separate
   environment; capture schema, legacy/V3 counts and projection hashes. The local
   schema-only checkpoint cannot restore customer values.
2. Provision reviewed roles using operator-controlled infrastructure. Existing
   credentials and volumes must be preserved. `make roles-init`/`make migrate`
   helpers are development/test only; production uses the migrate binary with
   explicit operator binding and production TLS/auth configuration.
3. Apply the complete forward chain and inspect status. Compare legacy identifiers,
   values, versions, creators/timestamps and adapter reads. A divergence exception
   aborts the migration transaction; retain both copies and review the difference.
   Do not overwrite, delete, disable RLS or reset the migration ledger to force
   completion. Reconcile under a reviewed forward plan or restore separately.
4. Run the operator release compatibility gate before deploying the binary. Reject
   unsupported stored IR; never recompile old production releases to make it pass.
   Start only the V3-compatible runtime and rerun probes/tenant checks. Legacy write
   table and column grants remain sealed after cutover.
5. Metadata rollback uses the tested approved-pointer switch with a recorded storage
   and external-effect recovery assessment. Existing executions keep their original
   release pin. External effects or destructive schema changes cannot be undone by
   switching the pointer; use specific reconciliation/compensation plans.
6. Code recovery uses the checkpoint tag to inspect/recreate the original source.
   Do not run that old writable adapter against the migrated database. Keep the V3
   compatibility changes, apply a reviewed forward repair, or restore a verified
   full backup into a separate recovery environment. No volume deletion or unsafe
   reverse-copy/down migration was executed in this task.

## 12. Git and cloud setup status

All migration source, tests, verified docs, ADR and reports belong in the dedicated
migration branch's review. At the initial implementation handoff, changes were
intentionally uncommitted and no remote push or main merge had occurred. The Git
manifest below records that pre-commit snapshot. Ignored `.env`, keys, `.local/`, `node_modules`, `bin/`,
`dist/`, logs and test artifacts must not be committed. The local checkpoint tag
provides source recovery, not a remote backup or a database backup.

The cloud-environment-onboarding setup workflow saved refreshed V3 startup
instructions as a configuration **draft**. Persistence was confirmed, with
`requires_publish=true`. Apply it by Review/Save/Publish in environment settings.
No repository membership, secret value, domain allowance or installation script
was changed in that update. Saving did not publish/apply the configuration or
establish fresh-task restoration of the local uncommitted migration.

**M6R STATUS: PASS. Safe to proceed to M7 — Rule Engine V2 development after the
next user task. M7 has not been implemented.**

## Exact Git change manifest

The following snapshot includes tracked modifications, the deliberately removed
visibility implementation and all untracked deliverables. `??` denotes files to
add in the migration commit; generated/private ignored files are excluded.

```text
 M .env.example
 M AGENTS.md
 M CLAUDE.md
 M Makefile
 M README.md
 M apps/web/e2e/preview.spec.ts
 M apps/web/src/preview/Workspace.test.tsx
 M apps/web/src/preview/Workspace.tsx
 M apps/web/src/preview/contracts.ts
 M apps/web/src/style.css
 M internal/audit/audit.go
 M internal/compiler/compiler.go
 M internal/config/config.go
 M internal/events/README.md
 M internal/platform/access/access.go
 M internal/platform/access/preview.go
 M internal/platform/app/app.go
 M internal/platform/postgres/postgres.go
 M internal/platform/postgres/transaction.go
 M internal/policy/policy.go
 M internal/promotion/README.md
 M internal/runtime/data/README.md
 M internal/runtime/data/model.go
 M internal/runtime/data/query.go
 M internal/runtime/data/store.go
 D internal/runtime/data/visibility.go
 M internal/tools/README.md
 M scripts/dev.mjs
 M scripts/smoke.mjs
 M scripts/verify-docs.mjs
 M scripts/with-env.mjs
 M tests/integration/data_runtime_test.go
 M tests/integration/foundation_test.go
 M tests/integration/identity_policy_test.go
?? cmd/release-check/main.go
?? db/migrations/00009_v3_dynamic_records.sql
?? db/migrations/00010_v3_roles_and_teams.sql
?? db/migrations/00011_v3_release_and_outbox_contracts.sql
?? db/migrations/00012_v3_audit_provenance.sql
?? db/migrations/00013_v3_legacy_cutover_barrier.sql
?? db/migrations/00014_v3_legacy_column_privileges.sql
?? db/migrations/00015_v3_final_cutover_barrier.sql
?? db/migrations/00016_v3_worker_identity_scope.sql
?? docs/adr/020-v3-m6r-architecture-remediation.md
?? docs/migration/M0_M6_AUDIT_REPORT.md
?? docs/migration/M6R_IMPLEMENTATION_REPORT.md
?? docs/migration/M6R_INDEPENDENT_REVIEW.md
?? docs/v3/00_MASTER_PLAYBOOK_V3.md
?? docs/v3/01_M6_CURRENT_STATE_STRATEGY.md
?? docs/v3/02_V3_ARCHITECTURE_FREEZE.md
?? docs/v3/03_KEY_CORRECTIONS_SUMMARY.md
?? docs/v3/99_CHANGELOG_FROM_V2.md
?? docs/v3/DOCUMENT_INDEX.md
?? docs/v3/README.md
?? docs/v3/SHA256SUMS.md
?? docs/v3/agents/80_CODEX_M6R_MASTER_PROMPT.md
?? docs/v3/agents/81_CODEX_REVIEW_M6R.md
?? docs/v3/agents/82_M7_NEXT_PROMPT.md
?? docs/v3/architecture/10_DYNAMIC_STORAGE_MODEL.md
?? docs/v3/architecture/11_VERSION_COMPATIBILITY_MODEL.md
?? docs/v3/architecture/12_APPROVAL_BINDING.md
?? docs/v3/architecture/13_INTENT_CONTRACT.md
?? docs/v3/architecture/14_DATASCOPE_AUTHORIZATION.md
?? docs/v3/architecture/15_DATABASE_ROLE_MODEL.md
?? docs/v3/architecture/16_TOOL_DELIVERY_SEMANTICS.md
?? docs/v3/architecture/17_AI_WORKFLOW_TRUST_MODEL.md
?? docs/v3/architecture/18_SANDBOX_ISOLATION.md
?? docs/v3/architecture/19_APPLICATION_PATCH_REBASE.md
?? docs/v3/erp/30_FOUNDATIONAL_TYPES.md
?? docs/v3/erp/31_DOMAIN_PACKS.md
?? docs/v3/erp/32_LOCALIZATION_SEARCH_IDENTITY.md
?? docs/v3/integrations/40_GENERIC_HTTP_CONNECTOR.md
?? docs/v3/integrations/41_TRANSACTIONAL_AUDIT_OUTBOX.md
?? docs/v3/manifest.json
?? docs/v3/migration/20_M0_M6_COMPATIBILITY_AUDIT.md
?? docs/v3/migration/21_M6R_REMEDIATION_PLAN.md
?? docs/v3/migration/22_EXISTING_DB_MIGRATION.md
?? docs/v3/migration/23_M6_UI_COMPATIBILITY.md
?? docs/v3/migration/24_DO_NOT_REWRITE_LIST.md
?? docs/v3/research/100_V3_DECISION_LOG.md
?? docs/v3/research/101_PRIMARY_REFERENCES.md
?? docs/v3/roadmap/50_RESTRUCTURED_MILESTONES.md
?? docs/v3/roadmap/51_DEPENDENCY_ORDER.md
?? docs/v3/verification/90_M6R_ACCEPTANCE.md
?? docs/v3/verification/91_STORAGE_TESTS.md
?? docs/v3/verification/92_VERSION_TESTS.md
?? docs/v3/verification/93_APPROVAL_TESTS.md
?? docs/v3/verification/94_DATASCOPE_TESTS.md
?? docs/v3/verification/95_WORKER_ROLE_TESTS.md
?? docs/v3/verification/96_SIDE_EFFECT_TESTS.md
?? docs/v3/verification/99_V3_RELEASE_GATE.md
?? infra/compose/v3-role-bootstrap.sql
?? internal/compiler/restore.go
?? internal/compiler/source_binding.go
?? internal/config/m6r_test.go
?? internal/erp/types.go
?? internal/erp/types_test.go
?? internal/events/transaction.go
?? internal/identity/worker.go
?? internal/intent/intent.go
?? internal/intent/intent_test.go
?? internal/metadata/patch.go
?? internal/metadata/patch_test.go
?? internal/platform/jobs/jobs.go
?? internal/policy/scope.go
?? internal/policy/scope_error.go
?? internal/promotion/bundle.go
?? internal/promotion/bundle_test.go
?? internal/promotion/compatibility.go
?? internal/promotion/store.go
?? internal/runtime/data/legacy.go
?? internal/runtime/data/scope.go
?? internal/sandbox/context.go
?? internal/sandbox/context_test.go
?? internal/tenant/teams.go
?? internal/tenant/worker.go
?? internal/tools/delivery.go
?? internal/tools/delivery_test.go
?? scripts/m6r-env.mjs
?? scripts/m6r-roles.mjs
?? scripts/runtime-env.mjs
?? scripts/runtime-env.test.mjs
?? tests/integration/m6r_adversarial_test.go
?? tests/integration/m6r_migration_test.go
?? tests/integration/m6r_release_test.go
?? tests/integration/m6r_scope_test.go
```
