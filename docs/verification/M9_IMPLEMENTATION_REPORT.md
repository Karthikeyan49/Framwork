# M9 Workflow Runtime V2 implementation report

Scope: **M9 only**, over retained M0–M8/M6R. Production activation is M13;
external tools are M10. Verification is complete; publication evidence is recorded below.

## Original repository and architecture

The checkout was clean on `milestone/m8-outbox-inbox` at
`7cc8077dcc628157ba48623fc94d5c4573d6bdef`. Created recovery tag
`pre-m9-2026-10-12` and branch `milestone/m9-workflow-runtime-v2` before source
changes. V3 remains authoritative, supplemented by V2 workflow specs47, risks28/29
and verification94. All84 V2 and43 V3 source checksums pass. No supplied architecture
documents, existing migrations, customer records or ZIP archives were deleted.
The pinned Rule V2 timezone ZIP remains required.

## Implemented behavior

- Opt-in metadata/schema/IR1.2 and Workflow DSL2 plans; typed Rule V2 branch
  conditions, deterministic normalization, explicit defaults, bounded acyclic
  graphs, versioned local capabilities and stored plan/source validation.
- Legacy1.0/1.1 IR bytes/hashes retained; stored instances never invoke today's
  compiler or implicitly switch revision, IR, workflow version, plan or inputs.
- PostgreSQL durability adapter behind framework-owned contracts: immutable
  bindings/snapshots, durable state, ordered history, timers, deadlines, bounded
  retries/backoff, receipts, cancellation, compensation and reviewed recovery.
- Exact PG-clock token/generation leases; append-only routing attempts; live
  single-job delegated identity and forced RLS.20 failed leases produce routing
  DEAD; revoked membership produces BLOCKED without payload access.
- Atomic local effect/receipt/history/audit/lifecycle-outbox commit. A lost
  COMMIT response preserves receipts and never blindly repeats an effect.
- Start/signal idempotency binds request hash and actor. Approvals require live
  policy, compiled roles, a distinct approver and expected instance version/state.
- Live entity/row/field SQL DataScope rechecked at start/read/resume/control,
  without replacing captured values. Record conditions receive actual ownership.
- Unknown outcomes pause for reviewed APPLIED/NOT_APPLIED reconciliation.
  APPLIED facts survive condition failures into explicit compensation. Conditions
  failing after local effects likewise enter FAILED compensation.
- Preview start/read/signal APIs; record-created/updated triggers start in the
  original business transaction using that snapshot/event ID. M8 replay cannot
  rehydrate or create workflows. Disabled workflows block matching mutations.
- HTTP workflow service errors roll back all business changes before responding;
  fresh live actor/tenant scope records safe denial evidence, including actual
  inner policy refusal provenance. No partial commit on budget/typed errors.
- Exact schema/grant/RLS/trigger readiness, workflow-only audit startup gate,
  graceful worker lifecycle, trace/request/workflow correlation and safe errors.
- Existing React entity/page renderer accepts IR1.2. No existing ERP UI rebuilt.

## Important files and repository structure

```
contracts/jsonschema/application-v1.2.schema.json
contracts/examples/workflow-draft-v1.2.json
internal/compiler/workflow_plans.go
internal/runtime/workflow/
  plan.go, contracts.go, store.go, execution.go, signals.go, readiness.go
internal/platform/access/workflows.go, workflow_triggers.go
internal/platform/jobs/workflows.go
internal/events/workflows.go
db/migrations/00020_m9_workflow_runtime.sql
db/migrations/00021_m9_workflow_lease_history.sql
tests/integration/m9_workflow_test.go, m9_failures_test.go, m9_crash_test.go
docs/adr/023-m9-workflow-runtime-v2.md
docs/verification/M9_ACCEPTANCE.md, M9_INDEPENDENT_REVIEW.md
```

Also updated metadata/compiler version admission/restoration, audit/policy/config
contracts, API/worker composition, frontend decoder/regression, Makefile/CI,
README, AGENTS/CLAUDE, process environment curation, smoke readiness and the fresh
database test's migration count. Generated binaries/dependencies/output remain ignored.

## Database migrations and preservation

Migration20 adds version1.2 admission, workflow bindings/instances/history/
receipts/signals/lifecycle outbox and controlled routing functions. Migration21
adds immutable claim/expiry/completion/block/dead evidence. Tenant-inclusive
keys/FKs, NOLOGIN ownership and forced RLS apply. API cannot claim worker work;
dispatcher cannot read workflow payloads; worker access is one routed job.

All21 migrations applied; rerun applied zero. Fresh database/repeatability tests
run against owned disposable databases. Existing2380 legacy preview records and
436 LEGACY_PAUSED intents were verified unchanged. Existing runtime data is never
rebuilt or removed. Lifecycle outbox belongs to events and is accessed through
its public contract; workflow code does not access M8 private delivery tables.

## Executed checks and results

Commands ran from `/workspace/Framwork`. Some targeted commands ran multiple
times while correcting fixtures/findings. Earlier failures are disclosed below.

| Exact command | Result/evidence |
| --- | --- |
| `make docs-verify` | PASS:84 V2/43 V3 checksums |
| `make setup` | PASS:managed Go modules and frozen frontend install |
| `make migrate migration-status` | PASS:20 initially applied;21 subsequently added |
| `make migrate` | PASS:migration21 applied |
| `make infra-up migrate migration-status release-check` | PASS:PG/Redis/OTel ready,21 applied, repeat zero, stored active IR compatible without recompilation |
| `make fmt` | PASS:Go formatting, repeated after source edits |
| `./scripts/go.sh test ./internal/compiler ./internal/metadata ./internal/runtime/workflow` | PASS:legacy regressions and initial admission |
| `./scripts/go.sh test -count=1 ./internal/compiler ./internal/runtime/workflow ./internal/platform/access` | Initial FAIL:new example schema fixture; corrected |
| `./scripts/go.sh test -run TestWorkflowPlansDeterminism -v ./internal/compiler` | Initial FAIL:fixture version/legacy definitions; corrected |
| `./scripts/go.sh test -run TestWorkflow -v ./internal/compiler` | PASS:determinism, restore, branches, malformed graph/capability/retry negatives |
| `./scripts/go.sh test ./internal/platform/access ./internal/runtime/workflow` | PASS |
| `./scripts/go.sh test ./internal/platform/app ./internal/platform/access ./internal/config ./internal/runtime/workflow` | PASS |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -run '^TestM9' -count=1 -v ./tests/integration` | PASS:initial four PG cases10.056s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run '^TestM9' -count=1 ./tests/integration` | First FAIL:short polling bound under race; subsequent PASS41.213s |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -run '^TestM9' -count=1 ./tests/integration` | Authoring FAIL:undefined test helper; corrected, then PASS20.649s |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -run '^TestM9(UnknownCommit\|ActualProcess)' -count=1 -v ./tests/integration` | PASS10.849s:actual lost COMMIT and SIGKILL before/after commit |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run '^TestM9(Rejected\|TimerTimeout)' -count=1 -v ./tests/integration` | Authoring FAIL:unused test import; corrected |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run '^TestM9(Rejected\|TimerTimeout\|FieldScope\|AppliedReconciliation)' -count=1 -v ./tests/integration` | PASS24.118s:rollback/budget, actual old ticket, field scope, APPLIED compensation |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run '^TestM9WorkflowOnly' -count=1 -v ./tests/integration` | PASS5.474s:mandatory audit refusal, execution without broker, graceful shutdown |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run '^TestM9PolicyDenial' -count=1 -v ./tests/integration` | PASS2.659s:actual refusal provenance survives rollback |
| `make check` | PASS:format/vet/modules/docs/ESLint/TypeScript |
| `make build test-workflows-fuzz` | PASS:all binaries/frontend;226,747 workflow fuzz executions |
| `make check test-unit` | PASS:race Go/Node4/frontend28 |
| `make check build` | PASS |
| `make check build test-unit` | PASS:latest source, Node4/frontend28 |
| `make test` | Initial FAIL150.504s:fresh DB test expected19 migrations; changed to21 |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -run '^TestM1FreshDatabaseMigrations$' -count=1 -v ./tests/integration` | Pre-fix FAIL:stale19-count assertion |
| `make test-integration` | PASS147.692s before final review fixes; latest full rerun179.271s after every source fix |
| `make check build test` | PASS168.947s full integration after major review fixes; final provenance change covered by its exact PG regression |
| `make test-workflows-fuzz test-events-fuzz test-rules-fuzz` | PASS:169,490 workflow;122,669 pointers;307,418 rule compile;62,955 restore/input;2,905 money executions |
| `npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS:zero vulnerabilities |
| `make test-e2e` | PASS:4 actual Chromium tests,7.1s |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS:health/readiness including workflow contract, PG/Redis/IAM, authentication/CORS, frontend proxy, trace correlation, collector traces/runtime metrics, M8/M9 initialization and graceful shutdown |
| `git diff --check` | PASS |

## Failure disclosures and review

Failures were not hidden: new-schema example initially retained incompatible
legacy definitions/versions; a race fixture's3s polling budget was too short;
temporary authoring helper/import mistakes prevented integration compilation;
the fresh migration test still expected19 rather than21. Each was corrected and
the relevant checks rerun. No checks were disabled or replaced with trivial tests.
The intentional Redis-unavailable negative can emit connection-refused diagnostics.

Independent read-only review found and verified closure of one HIGH transaction
boundary issue, five MEDIUM fencing/readiness/recovery/startup issues, and one LOW
denial-provenance issue. See [exact independent evidence](M9_INDEPENDENT_REVIEW.md).
No unresolved code findings remain. The test child entrypoint without its parent
environment is a helper;18 main M9 cases and real child-process subcases carry
the assertions, rather than counting its idle invocation as extra evidence.

## Architecture decisions and dependencies

ADR023 records opt-in executable plans, immutable input/version pins, PostgreSQL
durability behind framework contracts, scoped worker claiming, explicit local
activity limits, separate lifecycle outbox, recovery and Temporal evaluation.
No new third-party dependency was introduced. Existing pgx/database/sql, managed
JSON Schema validation, typed rules, OTel and React/TypeScript are reused.
No Rust/Python, microservices, Kubernetes, code generation or event sourcing.

## Security observations and known limits

- Only trusted `local.noop` is registered. Test adapters exercise atomic local
  SQL effects, not real external payments/messages. Additional adapters must
  use explicit policy/data contracts and honor deadlines. External calls must
  not run inside the transaction; M10 will add governed tool execution.
- Public execution is development/test preview only. No approved production
  workflow service/activation or production rollback integration is claimed.
- Lifecycle outbox events are immutable intents awaiting explicit subscriber
  registration/delivery. M8's record receipt topic remains unchanged.
- Acyclic graphs,100 states,256 transitions,1 MiB plans, bounded input/output,
 10 activity attempts and1000 lifetime history entries limit resource use.
  The history cap blocks further mutation/recovery; an operational forward fix
  is required. Never erase history/pins/receipts to extend it. Unlimited manual
  replay, automated archival/retention and production scale testing are not provided.
- Source record/field access must remain live. Revocation/deletion can block
  resumed work; restoring authorized access or reviewed recovery is required.
- No workflow editor UI, AI activities, full ERP modules, external exactly-once,
  production rollout, remote GitHub CI outcome or fresh cloud restoration was
  executed. These are NOT RUN/outside M9, not passing claims.

## Recovery and rollback

1. Back up PostgreSQL and preserve `.env`, identity keys and database volumes.
2. Apply forward migrations20/21 before binaries. Check readiness and roles.
3. Fix underlying policy/membership/adapter/dependency problems before recovery.
4. Use owner/admin signals with expected instance version/state, unique key and
   reviewed operator reference. APPLIED/NOT_APPLIED must reflect verified facts.
   Preserve IR/plan/input pins, receipts, history and routing attempts.
5. Cancellation/timeout runs only declared feasible compensation; irreversible
   actions are not rolled back by metadata switching.
6. Pause `WORKFLOWS_ENABLED=false` before old binaries; matching triggered writes
   are blocked by the M9 runtime. Keep additive schemas/evidence. Old binaries
   do not understand M9 definitions, so quiesce such applications before rollback.
7. Use reviewed forward fixes or restore backups into a separate recovery
   environment. Never destructive down migrations, history deletion or customer
   data removal. Pre-M9 checkpoint is for code recovery, not data reversal.

## Git and cloud environment

Changes belong on `milestone/m9-workflow-runtime-v2` with the pre-M9 checkpoint.
Implementation commit `f12bd4dc3cd0c46efba6eefb74e8e25e392bc37f` was pushed
and verified against `refs/heads/milestone/m9-workflow-runtime-v2`. Recovery tag
`pre-m9-2026-10-12` was pushed and verified at the original M8 HEAD. This publication
record is a documentation follow-up commit. Main is not merged/reset/force-pushed. `.env`, `.local`,
`bin`, `node_modules`, `dist` and caches are ignored and must not be committed.
M9 `start_skill` was saved successfully in the cloud environment draft; existing
installation/network/secrets were preserved. The repository declaration is updated
to the final exact local HEAD after this documentation commit. Review and save the
changes in environment settings, then publish the environment. Draft saving does
not publish or prove fresh-task restoration.

Git commands executed: `git status --short --branch`, `git switch -c milestone/m9-workflow-runtime-v2`,
`git tag pre-m9-2026-10-12`, `git add` with the enumerated source/report paths,
`git commit -m "feat: implement M9 workflow runtime v2 with durable recovery"`,
`git push -u origin milestone/m9-workflow-runtime-v2`, `git push origin pre-m9-2026-10-12`,
`git ls-remote origin refs/heads/milestone/m9-workflow-runtime-v2 refs/tags/pre-m9-2026-10-12`,
and `git diff --check`. All publication operations succeeded; tracked artifacts
contain no environment/credential/build outputs. The final documentation push
and remote equality/clean-tree checks follow this record.

M9 STATUS: PASS (documented development/test workflow scope)

Recommended next milestone: M10 — Tool Registry V2. Do not begin it automatically.
