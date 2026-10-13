# AI-native ERP/CRM framework

A Go modular-monolith and React/TypeScript foundation for one reusable,
metadata-driven enterprise runtime. **M0–M6:** local infrastructure, telemetry,
cryptographic identity, trusted tenant membership, central policy and durable
audit, bounded transactions, reusable tenant RLS and a typed draft metadata registry.
Canonical compiler IR, bounded typed preview CRUD/query execution and dynamic React
forms/tables/details are implemented. M6R adds V3 storage/authorization/release
foundations; M7 adds pure typed Rule Engine V2, M8 adds transactional delivery,
and M9 adds durable Workflow Runtime V2. Production activation and AI remain later work.

**M9R remediation is in progress.** The [review validation](docs/verification/M0_M9_REVIEW_VALIDATION.md)
reproduced worker-authority, data-namespace, query-cost and write-contention gaps.
Follow the [remediation plan](docs/migration/M9R_REMEDIATION_PLAN.md) before M10.
The [worker boundary](docs/migration/M9R_R1_IMPLEMENTATION_REPORT.md) and
[RLS reference floor](docs/migration/M9R_R3A_IMPLEMENTATION_REPORT.md) have
independent evidence. Page-fetch correctness passed independent review; the
current-host HTTP performance gate failed. Dataset continuity and concurrent
writes remain open.
The [supersession map and capability inventory](docs/migration/V2_V3_SUPERSESSION_MAP.md)
qualify historical PASS reports and distinguish previews from contract-only and
deferred production services.

AI will propose structured drafts; the compiler, sandbox, verification, approval
and promotion pipeline will produce immutable versions for deterministic runtime
execution. V3 is authoritative: read the [V3 kit](docs/v3/README.md), [master playbook](docs/v3/00_MASTER_PLAYBOOK_V3.md), and [V3 roadmap](docs/v3/roadmap/50_RESTRUCTURED_MILESTONES.md). Retained [V2 documents](docs/README.md) supplement unchanged specifications. Read the [compatibility audit](docs/migration/M0_M6_AUDIT_REPORT.md), [M6R decision](docs/adr/020-v3-m6r-architecture-remediation.md),
[agent contract](AGENTS.md), and [M0 tooling decision](docs/adr/013-m0-foundation-tooling.md), and
[M1 identity decision](docs/adr/014-m1-identity-policy-audit.md).

## Prerequisites and quick start

- Go **1.27.2**, Node **24.19.0** / npm 11, Docker Engine + Compose v2, Make, curl.
- Linux amd64: make setup installs Go locally if needed, checking the official
  SHA-256. Other platforms: install the pinned Go release yourself. Node must be 24.
- Available loopback ports: 5432, 6379, 4318, 13133, 8080, 5173.

From the checkout root:

```sh
make dev
```

This installs locked dependencies, preserves or generates a 0600 `.env` with random
development passwords, starts the local stack, applies migrations, builds the
programs, and runs API/worker/dispatcher/Vite. V3 assigns separate API/worker credentials;
operator and dispatcher secrets are excluded from application children. Ctrl-C stops those application processes;
`make infra-down` stops local containers and retains PostgreSQL data.

M9R requires an explicit tenant assignment for the worker. On a fresh database,
`make dev` starts API/web/dispatcher and reports the worker as paused. Create the
local owner below, assign the worker, then run `make worker` or restart `make dev`.
An unassigned worker cannot claim or execute another tenant's work.

The coordinator prints its PID. Some cloud runners close a command connection
without propagating Ctrl-C to child processes. In that case, send SIGINT or SIGTERM
to the printed coordinator PID that you started, and verify the API/frontend ports
close. Stop only your own coordinator; do not use broad process-kill commands.

If you only need dependencies and infrastructure:

```sh
make setup
make infra-up
make migrate
make migration-status
```

The API defaults to loopback port 8080; the browser development server to 5173.
The development proxy maps `/api/*` to the API. Deployment must serve frontend SPA
fallback routes and proxy `/api` to the backend, or set public VITE_API_BASE_URL to
an approved HTTPS API origin at build time. The access console validates identity/workspace access; compiled draft previews are available after selecting a verified workspace.

## Verification

```sh
make check             # source checksums, nonmutating Go format, vet, integrity, TS lint/typecheck
make test              # Go race/unit tests, frontend tests, real PG/Redis integration
make build             # API, worker, operator binaries, frontend bundle
make release-check     # operator-only supported active-IR deployment gate
node scripts/with-env.mjs node scripts/smoke.mjs  # live API, telemetry, worker and proxy smoke
```

Integration tests fail if required credentials/services are absent. Start the
local stack first. `make test-unit` works without infrastructure. `make fmt` is the
intentional formatting command. Build outputs are `bin/` and `apps/web/dist/`.

Operational API:

| Endpoint | Behavior |
| --- | --- |
| `GET /healthz` | 200 while process is serving; independent of PG/Redis/collector |
| `GET /readyz` | 200 if PostgreSQL/Redis and identity/tenant/audit/metadata/data schema contracts pass; 503 otherwise |
| `GET /v1/session` | Verified active user and at most 100 active memberships |
| `GET /v1/tenants/{id}/access` | Verified active membership and a recorded policy decision |
| `GET /v1/tenants/{id}/members` | At most 100 members, authorized owner/admin only |
| `POST /v1/tenants/{id}/metadata/{app}/versions` | Owner/admin: structurally validated immutable draft append |
| `GET /v1/tenants/{id}/metadata/{app}/versions` | Active member: up to 50 revision headers, `?before=N` cursor |
| `GET /v1/tenants/{id}/metadata/{app}/versions/{revision}` | Active member: validated historical draft source |

Responses include server-generated `X-Request-ID`. Errors use `{code,message,request_id}`
and do not include private driver/panic details. OTel propagates W3C trace context;
access logs correlate request and trace IDs. Trusted actor/tenant/decision/policy IDs are span attributes and correlate through
audit request/trace IDs. Business correlation remains later work. HTTP requests and Go runtime metrics are
instrumented; PG readiness checks create sanitized database spans. Asynchronous
OTLP export is bounded; a temporarily missing collector does not prevent startup.
The local collector exports basic summaries to container logs, not a dashboard.

## Configuration and security

See [.env.example](.env.example). Backend processes read the environment; local
helpers parse `.env` as data rather than executing it. Already-set process variables
take precedence. All `VITE_` variables are public; never put secrets in them.

| Variable | Purpose |
| --- | --- |
| APP_ENV | development / test / production |
| HTTP_ADDR | API listen address; defaults to loopback |
| LOG_LEVEL | debug / info / warn / error |
| DATABASE_URL | least-privilege application PostgreSQL connection |
| MIGRATION_DATABASE_URL | separate DDL connection for migration CLI |
| REDIS_URL | authenticated Redis connection |
| CORS_ORIGINS | comma-separated exact origins; empty denies cross-origin requests |
| OTEL_EXPORTER_OTLP_ENDPOINT | optional collector origin; empty disables network export |
| VITE_API_BASE_URL | public API prefix/origin; `/api` default |
| AUTH_ISSUER | Exact signed-token issuer; HTTPS required in production |
| AUTH_AUDIENCE | Required API token audience |
| AUTH_JWKS_FILE | Operator-managed public RS256 JWKS file; relative paths use repository cwd |

`POSTGRES_PASSWORD`, `MIGRATOR_PASSWORD`, `APP_DB_PASSWORD`, and `REDIS_PASSWORD` are
development Compose bootstrap inputs. They have no committed defaults. Existing
`.env` is never overwritten. Do not delete it while retaining the PG volume: role
passwords are initialized once. Rotate database role passwords explicitly.

Production runtime and migration configuration refuse PostgreSQL connections without `sslmode=verify-full`, non-TLS
Redis, absent connection passwords, non-HTTPS CORS origins, and insecure telemetry
origins. Runtime readiness also rejects superuser/BYPASSRLS/create-role/create-database
roles, protected-schema/table owners, DDL privileges and IAM writes. Protected
identity endpoints independently enforce this runtime-role check. Inject credentials using the deployment's secret manager and verified CA
trust. Terminate inbound HTTPS at a trusted reverse proxy and restrict the backend
listener/network to that proxy. Compose is exclusively a local development stack;
its plaintext loopback connections and local credential files are not production
deployment instructions. No default production credentials, wildcard CORS, remote
debug ports, request-body logging, or raw SQL/LLM execution endpoints are provided.

HTTP header/read/write/idle timeouts, header bounds and a 1 MiB body ceiling apply.
All current read-only routes additionally reject any body. Public operational endpoints
must be restricted by the production network; liveness/readiness are not auth.
Frontend request timeouts, cancellation, validation and redacted errors prevent
unbounded loading and accidental server-detail disclosure.

## Repository layout

```text
cmd/{api,worker,migrate,iam}/             Process entrypoints
internal/{config,platform,observability}/  M0 infrastructure and composition
internal/{identity,tenant,policy,audit}/   M1 public ports and owned persistence
internal/metadata/                   M3 models, validation and registry ports
internal/platform/access/             Identity/tenant/policy/audit/metadata HTTP composition
internal/runtime/{data,rules,workflow}/  Later runtime modules, no M0 logic
apps/web/                            React shell, router, typed API client, tests
db/{migrations,rls,seeds}/            Embedded forward migrations and reusable tenant RLS
contracts/jsonschema/                Embedded typed metadata contract
tests/{integration,e2e,security,performance}/
infra/{compose,otel}/                Local PostgreSQL/Redis/collector
scripts/                            Setup, env, process helpers and validation
.github/workflows/ci.yml             Format/vet/test/build/TS/integration/audit
docs/                               Unchanged buildkit + ADRs + milestone evidence
```

## CI and recovery

CI runs the same Make targets against actual local services with random credentials.
Actions are pinned to commit IDs; Go/npm dependencies have managed lockfiles and
container versions are pinned. GitHub Actions execution is separate from local
verification and must be observed after pushing. Do not add production secrets to CI.

Migration 00001 adds the iam/control/runtime/audit schemas without business data.
Goose runs it transactionally under the migration role and serializes CLI calls via
PG advisory locking. A repeat up applies zero migrations. The app role cannot own
schemas, create tables, or read the migration ledger. M1 adds read-only runtime IAM grants, forced actor/tenant RLS and insert-only audit.
M2 adds reusable bounded transaction/Executor contracts and membership-backed tenant RLS. Migration 00003
adds audit state/provenance without modifying already-applied versions.

No destructive down command is exposed. Failed migration transactions do not mark
the migration as applied; fix forward after reviewing database state. Never remove
the persisted development volume to repair a password or migration error without
preserving data. M0 does not claim backup/PITR, business rollback, production HA,
durable queues, or production telemetry retention. See [M0 report](docs/M0_COMPLETION_REPORT.md)
for actual checks and remaining milestone scope.

## M1 local identity and workspace access

No accounts or privileged memberships are created during startup. Run this explicit
operator command to create your local owner (local development only):

```sh
node scripts/with-env.mjs ./bin/iam \
  --issuer http://localhost:8080/dev-issuer \
  --subject local-owner --name "Local owner" \
  --tenant 11111111-1111-4111-8111-111111111111 --tenant-name "Local workspace" \
  --role owner --user-status active --membership-status active \
  --operator local-setup
```

Assign the local worker after creating that tenant:

```sh
node scripts/with-env.mjs ./bin/worker-admin \
  --principal framework_worker \
  --tenant 11111111-1111-4111-8111-111111111111 --operator local-worker-setup
```

This records assignment and operator audit together. `--disable` revokes future
bindings. Further tenants require distinct LOGIN principals inheriting only the
worker privilege group, unique credentials and audience-specific Redis receiver
ACLs. See [the M9R worker runbook](docs/migration/M9R_WORKER_RUNBOOK.md).

Generate a 15-minute local access token in your terminal:

```sh
node scripts/with-env.mjs node scripts/dev-identity.mjs token local-owner
```

Open **Workspace access** in the console, paste the token, connect, and select the
workspace. Treat the token as a credential; do not paste it into issues/logs.
The browser keeps it in memory only. Disconnect/reload clears it; observed 401
responses clear expired/disabled sessions. The server verifies membership on every
request and ignores token role/tenant claims and tenant headers. Tokens cannot
create users or grant roles. The local issuer refuses production and configured
external-issuer token minting. Generated private keys are ignored, owner-readable
only and preserved across setup; public JWKS is the only key file the API reads.

For a real provider, bind AUTH_ISSUER, AUTH_AUDIENCE and AUTH_JWKS_FILE explicitly.
Provision users by their stable provider subject. The chosen token contract is
RS256 with typ=at+jwt, issued-at/expiration and a maximum one-hour lifetime;
provider integration must issue this API-specific contract. Pinned key rotation
requires an API restart. This milestone does not implement provider redirect login,
refresh tokens, MFA, password handling or self-service account recovery.

Operator changes require separate migration credentials and all explicit flags;
there is no role-grant HTTP endpoint or automatic owner bootstrap. Change role or
revoke membership using the same command with the explicit desired state. Transfer
ownership before revoking/demoting the last owner or disabling an owning user.
Both state and typed audit evidence commit together. The operator reference is
asserted attribution; database writer provenance is automatic. Production operator
credentials need controlled deployment access, beyond the read-only runtime role.
On an unconfirmed COMMIT, inspect membership and audit evidence before retrying.
Audit failure causes protected requests to fail closed. Do not erase audit history
or retained data to recover. Audit rows are append-only to application/ordinary
operator DML; privileged DDL administration and immutable external archival require
separate operational controls. Lists are bounded to 100 without pagination in M1.

See [M1 completion report](docs/M1_COMPLETION_REPORT.md) and
[independent review](docs/M1_INDEPENDENT_REVIEW.md). M3 is complete; stop before M4 until authorized.

## M2 tenant data boundary

Protected API access now uses a single checked connection and bounded transaction,
clears pooled identity context, verifies live actor/membership-backed tenant scope,
and sends results only after audit and commit. Repository ports receive a query
executor without lifecycle methods. Errors, cancellation and panics roll back;
failed cleanup discards the connection. Commit uncertainty never triggers a blind retry.

Migration 00004 hardens IAM RLS and adds a migration-only installer for future
runtime/control tenant tables: ENABLE + FORCE RLS, USING + WITH CHECK, tenant-inclusive
keys and scoped relationships. No production business entity tables, metadata registry,
query AST or dynamic CRUD endpoints are introduced. Normal app credentials cannot
run DDL/install RLS, own protected tables or bypass row security.

Use the existing `make infra-up migrate migration-status`, `make check`,
`make test` and `make build` commands. Integration tests exercise the actual
installer on unique disposable tables with the normal database role, plus fresh
migration and privilege/RLS drift recovery in a disposable database.

See [ADR-015](docs/adr/015-m2-tenant-data-boundary.md),
[RLS migration contract](db/rls/README.md),
[M2 completion report](docs/M2_COMPLETION_REPORT.md) and
[independent review](docs/M2_INDEPENDENT_REVIEW.md).

## M3 metadata authoring registry

The schema 1.0.0 envelope is `{schema_version,app_id,namespace,base_version,definitions}`.
See [vendor draft](contracts/examples/vendor-draft-v1.json) for entity, field,
relationship, page, rule, workflow and tool definitions. The source and its typed
normalized form are capped at 256 KiB/500 definitions. Input is strict JSON or a
JSON-compatible YAML subset; aliases, anchors, merge keys, custom tags and multiple
YAML documents are rejected. Unknown properties/schema versions, duplicate IDs,
NUL strings and unsafe executable properties are rejected.

An authorized append body contains exactly `expected_revision` and `document`:

```json
{
  "expected_revision": 0,
  "document": {
    "schema_version": "1.0.0",
    "app_id": "app.example",
    "namespace": "example",
    "base_version": null,
    "definitions": [{
      "id": "entity.vendor", "schema_version": "1.0.0",
      "namespace": "example", "label": "Vendor", "kind": "entity",
      "entity": {"fields": []}
    }]
  }
}
```

POST this command to `/v1/tenants/{verified-workspace-id}/metadata/app.example/versions`
with the bearer token and `Content-Type: application/json` (or `application/yaml`).
The tenant selection is verified against live membership; payload tenant claims
are never accepted. Initial expected revision is 0; subsequent appends use the
current revision. The response is a draft revision with source hash and database
creator/time. GET the revision route to inspect normalized JSON. Only this bounded
append route permits POST/CORS POST. Commands allow 4 KiB envelope overhead.

All tenant roles may read; owner/admin may write. Source, head and mandatory audit
commit atomically; historical rows cannot be updated or deleted by ordinary roles.
A stale revision or namespace change returns 409. Overlapping writers can return
503 under the bounded repeatable-read transaction; reload/inspect committed history
before retrying. No automatic write retry. Validation errors use the standard
error envelope plus safe `issues`; raw source values/SQL errors are not logged.

A source hash identifies storage, not compiler IR, approval or production release.
References may remain unresolved. M4 will compile types/symbols/graphs/capabilities;
M3 executes no rules, workflows or tools, creates no ERP tables and activates no
release. Authoring UI is later work; no AI endpoint is provided. Never put secrets
in tenant-readable metadata labels or values.

Apply migrations 00005–00007 before starting the M3 binary. Readiness verifies
required storage/head constraints, immutable trigger, RLS and restricted grants.
Recover through reviewed forward fixes; retain history when reverting a binary.
Integration tests keep immutable metadata/audit evidence under their unique
fixture tenants after IAM fixture cleanup. No automatic metadata seed is installed.

See [ADR-016](docs/adr/016-m3-metadata-source-registry.md),
[M3 completion report](docs/M3_COMPLETION_REPORT.md) and
[independent review](docs/M3_INDEPENDENT_REVIEW.md). Stop before M4 — Compiler V1.

## Compiled application previews (M4–M6)

Development/test only: append a structurally validated source using the existing
metadata API (see contracts/examples/contacts-draft-v1.json), then compile it with
`GET /v1/tenants/{tenant}/metadata/{app}/versions/{revision}/compile`. Compilation
returns deterministic IR/hash or422 diagnostics and does not activate the draft.
In Workspace access, connect, select a workspace, enter the application ID and draft
revision, then Load preview. Forms/tables/details use isolated records pinned to that
source revision and IR hash; a new revision begins a separate preview dataset.

Preview API: `GET /v1/tenants/{tenant}/preview/{app}/versions/{revision}`; entity queries
and commands are POST under that path at `/entities/{entity}/query` and `/commands`.
Commands use operation create/update/delete, typed values and expected_version for
update/delete. Viewer is read-only; server policy governs every operation. Query
projection32, page100, typed filters, one declared join and bounded aggregates apply.
Queries authorize the unfiltered candidate dataset before predicates; any denied
record/used field rejects the whole query. Preview ceiling1,000 candidates and the
3-second transaction budget are conservative limits, not production scale claims.

`make browser-install` uses installed Chromium or installs Playwright-managed Chromium
(no security checks disabled). After `make build`, with infrastructure running and
API/Vite ports free, `make test-e2e` launches owned servers and verifies browser CRUD,
exact values, conflict recovery, paging, safe text and server-enforced read-only access.
CI installs managed Chromium with OS dependencies. E2E fixtures are unique development
users/tenants/drafts; existing data is preserved. Tokens stay in memory and browser
traces/video/screenshots are off. Generated dependencies/builds/artifacts stay ignored.

Decisions: [ADR017](docs/adr/017-m4-compiler-ir.md),
[ADR018](docs/adr/018-m5-preview-data-runtime.md),
[ADR019](docs/adr/019-m6-compiled-ui-renderer.md). Completion and independent review
reports are under docs/M4*, M5*, M6*. Workflows/tools/sandbox/promotion,
released schema migrations, AI builder and full scale/security certification remain
their later milestones. Do not start M9 without a new user task.

## V3 migration and recovery

M6R adds generic records, SQL DataScope, least-privilege worker roles, immutable
release/approval contracts, transactional audit/outbox and ERP value contracts.
See [the implementation report](docs/migration/M6R_IMPLEMENTATION_REPORT.md).
There are no public production activation endpoints or AI execution capabilities.
Use the V3 roadmap; M8 is outbox/inbox, M9 workflows, M10 tools,
M11 sandbox, M12 candidate builder and M13 approval/promotion services.

For an existing database, stop all legacy API/worker writers before migration.
Take and test an encrypted operational backup plus schema/row checkpoints. Keep
existing credentials and volumes. Local development uses make infra-up and make
migrate, which provisions development-only dedicated roles before applying 9–19.
No Down or destructive reset is exposed. Legacy tables and a read-only adapter remain.
The final barrier waits for legacy transactions, reconciles late inserts and aborts
on divergence rather than overwriting data. The report contains the rehearsal and
forward-recovery instructions. Do not restart old writers after cutover.

For production, provision reviewed roles using operator-controlled infrastructure;
local credential helpers reject production. Run the migrate binary with explicit
MIGRATION_DATABASE_URL and production TLS/auth configuration, then release-check
before deploying binaries. Never mount operator/dispatcher credentials into API or
worker processes. A failed compatibility check blocks deployment.

Rollback of release metadata is an audited, approved pointer switch after storage
and external-effect assessment. It cannot undo destructive schema changes or remote
actions. Code rollback must retain V3 storage/role compatibility; returning an old
binary to the legacy writable adapter is unsafe. Restore into a separate verified
recovery environment or apply a reviewed forward reconciliation; retain both copies.

## M7 — Rule Engine V2

Read [ADR021](docs/adr/021-m7-rule-engine-v2.md),
[rule semantics](internal/runtime/rules/README.md), the
[implementation report](docs/verification/M7_IMPLEMENTATION_REPORT.md) and
[independent review](docs/verification/M7_INDEPENDENT_REVIEW.md).
Opt-in metadata schema 1.1.0 requires DSL 2.0.0 and emits typed rule plans in IR1.1.0.
Existing schema/IR1.0.0 remains supported with its original hashes; it contains no
executable DSL2 plans. Use [the example](contracts/examples/contacts-rules-v2.json)
for exact Money thresholds, minor-unit rounding and a pinned business calendar.

With migration17 applied, development/test preview exposes
`POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/rules/{rule}/evaluate`
with `{"record_id":"<UUID>"}`. It loads the record through existing action/field/
DataScope/RLS checks, enforces record-specific rule authorization, computes only a
value and commits audit before returning it. Requests cannot supply expressions,
values, tenant context, calendar overrides or tools. No production route is exposed.
Money/decimal JSON outputs use exact strings; no automatic business action follows.

`make test` includes deterministic/property tests and fuzz seeds. For timed fuzzing:

```sh
make test-rules-fuzz
GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzRuleCompile$' -fuzztime=10s -parallel=2 ./internal/runtime/rules
GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzRuleRestoreAndInputs$' -fuzztime=10s -parallel=2 ./internal/runtime/rules
GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzFixedDecimalMoney$' -fuzztime=10s -parallel=2 ./internal/runtime/rules
```

## M8 — Transactional Outbox + Inbox

Read [ADR022](docs/adr/022-m8-transactional-delivery.md),
[delivery and recovery semantics](internal/events/README.md), and
[M8 verification](docs/verification/M8_IMPLEMENTATION_REPORT.md).
`make dev` now runs API, dispatcher, worker and web with separate credential
bindings. `make env` preserves secrets and generates local broker ACL identities;
`make infra-up migrate build` prepares migrations18/19 and binaries. Existing
PostgreSQL volumes, data and all historical intents remain intact.

`EVENTS_ENABLED=true` is development/test only. New preview CRUD atomically freezes
an immutable event; the dispatcher publishes its reference, and a receipt-only
worker commits policy/audit/inbox completion. Redis can restart without losing
authoritative events. Production activation and external tools remain
later milestones; production preview delivery is rejected. Set EVENTS_ENABLED=false
to pause delivery without removing event or inbox history. Manual process commands
are `make api`, `make dispatcher`, `make worker`; `make test` runs M8 failure tests
alongside all existing regressions. M8 tests own disposable databases and restart
the local Redis cache; run them against development/test infrastructure only.

For operator-reviewed recovery, inspect delivery generation/attempts and fix the
underlying membership/configuration problem first. Then use separate operator
credentials (never mount them into API/worker/dispatcher):

```sh
node scripts/with-env.mjs bin/event-replay \
  -tenant <tenant-uuid> -event <event-uuid> -generation <expected-generation> \
  -operator <review-reference> -reason <reviewed-reason>
```

Replay retains event bytes/identity, attempts and inbox. Completed events remain
completed; stale generations, active leases and legacy paused intents are refused.
A dead predecessor blocks its aggregate successors until recovery. Old binaries
may be used only with delivery disabled and additive schemas retained; database
rollback never deletes event/inbox evidence. Apply reviewed forward fixes or restore
a verified backup into a separate recovery environment. See the report for limits.

## M9 — Workflow Runtime V2

Read [ADR023](docs/adr/023-m9-workflow-runtime-v2.md), the
[runtime/API contracts](internal/runtime/workflow/README.md) and
[verification report](docs/verification/M9_IMPLEMENTATION_REPORT.md).
Apply migrations20/21 with `make migrate` before deploying M9 binaries.
`WORKFLOWS_ENABLED` defaults true for development/test and false for production;
production refuses preview execution. `make worker` runs both M8 receipts and
the independent PostgreSQL workflow loop. PostgreSQL owns timers/state/history;
Redis is not the workflow durability provider.

The [schema1.2 example](contracts/examples/workflow-draft-v1.2.json) demonstrates
captured conditions, a local activity, timer, separate-actor approval and failure
paths. Start/read/signal APIs are documented in the runtime contract. Local
activities commit receipts/history/audit/lifecycle outbox atomically. Only
`local.noop` is registered; external tools are M10. The existing React preview
renders1.2 entity/page plans; a workflow authoring/editor UI is later work.

`make test` runs actual crash, lost-COMMIT-response, isolation and recovery checks;
`make test-workflows-fuzz` runs the bounded plan-restore campaign. Recovery never
changes the instance's captured inputs, IR, plan/version or committed receipts.
Unknown outcomes require reviewed reconciliation; compensation cannot undo
arbitrary external actions. Pause with `WORKFLOWS_ENABLED=false` before rolling
back binaries, retain all additive schemas/evidence, and use reviewed forward fixes.
