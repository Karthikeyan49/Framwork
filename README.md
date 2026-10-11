# AI-native ERP/CRM framework

A Go modular-monolith and React/TypeScript foundation for one reusable,
metadata-driven enterprise runtime. **M0 + M1:** local infrastructure, telemetry,
cryptographic identity, trusted tenant membership, central policy and durable
audit. ERP entities, metadata/compiler/runtime features and AI authoring remain
later milestones.

AI will propose structured drafts; the compiler, sandbox, verification, approval
and promotion pipeline will produce immutable versions for deterministic runtime
execution. The original architecture buildkit is preserved verbatim in [docs](docs/README.md).
Start with [the master playbook](docs/00_MASTER_PLAYBOOK.md), [roadmap](docs/implementation/50_IMPLEMENTATION_ROADMAP.md),
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
programs, and runs API/idle worker/Vite. Ctrl-C stops those application processes;
`make infra-down` stops local containers and retains PostgreSQL data.

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
an approved HTTPS API origin at build time. The access console validates identity/workspace access; ERP pages remain later work.

## Verification

```sh
make check             # source checksums, nonmutating Go format, vet, integrity, TS lint/typecheck
make test              # Go race/unit tests, frontend tests, real PG/Redis integration
make build             # API, worker, migration binary, frontend production bundle
node scripts/with-env.mjs node scripts/smoke.mjs  # live API, telemetry, worker and proxy smoke
```

Integration tests fail if required credentials/services are absent. Start the
local stack first. `make test-unit` works without infrastructure. `make fmt` is the
intentional formatting command. Build outputs are `bin/` and `apps/web/dist/`.

Operational API:

| Endpoint | Behavior |
| --- | --- |
| `GET /healthz` | 200 while process is serving; independent of PG/Redis/collector |
| `GET /readyz` | 200 if PostgreSQL/Redis and identity/tenant/audit schema contracts pass; 503 otherwise |
| `GET /v1/session` | Verified active user and at most 100 active memberships |
| `GET /v1/tenants/{id}/access` | Verified active membership and a recorded policy decision |
| `GET /v1/tenants/{id}/members` | At most 100 members, authorized owner/admin only |

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
internal/platform/access/             Identity/tenant/policy/audit HTTP composition
internal/runtime/{data,rules,workflow}/  Later runtime modules, no M0 logic
apps/web/                            React shell, router, typed API client, tests
db/{migrations,rls,seeds}/            Embedded M0/M1 migrations; narrow IAM RLS backstop
contracts/jsonschema/                Future typed metadata contracts
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
M2 will introduce reusable business-data RLS and access contracts. Migration 00003
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
[independent review](docs/M1_INDEPENDENT_REVIEW.md). Stop before M2 until authorized.
