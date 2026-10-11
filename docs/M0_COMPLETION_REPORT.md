# M0 completion report

Date: 2026-10-11. Checkout: `/workspace/Framwork`. Scope: M0 only.

## 1. Summary

Implemented the Go modular-monolith foundation, API and idle worker entrypoints,
validated configuration, graceful lifecycle, health/readiness, structured logging,
server-generated request IDs, bounded/redacted HTTP errors, baseline HTTP/runtime/
PostgreSQL readiness tracing, and asynchronous OTel export. Added the React/TS
shell, routing, typed/validated API client, cancellation/timeouts, loading/error
states, error boundary, and responsive keyboard-accessible shell styling.

Added local PostgreSQL 18.6, authenticated Redis 8.2.3, OTel collector 0.148.0,
separate database roles, an embedded transactional foundation migration with CLI
advisory locking, Make targets, locked dependencies, CI, source-document checksum
verification, architecture instructions and an ADR. All 85 original buildkit files
are preserved under docs; 84 supplied SHA-256 entries verify. No M1 functionality.

## 2. Important repository structure

```text
AGENTS.md / CLAUDE.md / README.md / Makefile / .env.example / .nvmrc
.github/workflows/ci.yml
go.mod / go.sum
cmd/{api,worker,migrate}/main.go
internal/config/                  Runtime + migration configuration and negative tests
internal/observability/           HTTP/runtime OTLP providers and TLS regression test
internal/platform/{app,httpserver,postgres,cache}/
internal/{identity,tenant,policy,metadata,compiler,tools,events,sandbox,promotion,ai,audit}/
internal/runtime/{data,rules,workflow}/  Future modules: README boundaries only
apps/web/                        Shell, router, client, configuration, tests, tooling
db/migrations/                   Embedded 00001 schema bootstrap
db/{rls,seeds}/                   Documented future work
contracts/jsonschema/             Future metadata contracts
tests/integration/               Real PostgreSQL/Redis, migration and process checks
tests/security/                  AST module-import boundary check
tests/{e2e,performance}/          Documented future suites
infra/compose/                   Local stack and least-privilege role bootstrap
infra/otel/                      Collector configuration
scripts/                         Setup, environment/process helpers and smoke checks
docs/                            Verbatim buildkit, ADRs, this report
```

## 3. Commands executed

Commands below were run from the checkout root unless explicitly noted. These are
actual validation/install commands, not a proposed test plan. Make recipes contain
the exact subordinate commands. Source-inspection commands (`rg`, `cat`, `sed`,
`find`, `ls`), pinned-version registry queries, and bounded process polling were
also used for diagnosis.

```sh
make setup
make env
make infra-up
make migrate migration-status
make fmt
make check
make test
make build
make check && make test && make build
make dev
node scripts/with-env.mjs node scripts/smoke.mjs
HTTP_ADDR=127.0.0.1:8087 node scripts/with-env.mjs node scripts/smoke.mjs
./scripts/go.sh test -race -count=1 ./internal/config ./internal/observability
./scripts/go.sh test -json -race -count=1 ./cmd/... ./internal/... ./db/... ./tests/security/...
node scripts/with-env.mjs ./scripts/go.sh test -json -race -tags=integration -count=1 ./tests/integration
docker compose --env-file .env -f infra/compose/compose.yaml config --quiet
docker compose --env-file .env -f infra/compose/compose.yaml ps
curl -fsS http://127.0.0.1:13133/
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:5173/api/readyz
npm --cache /workspace/.npm-cache --prefix apps/web audit --audit-level=high
npm --cache /workspace/.npm-cache --prefix apps/web run build
git status --short --untracked-files=all
git check-ignore .env .local bin apps/web/node_modules apps/web/dist
git diff --check
```

Additional actual setup/diagnostic operations:

- Downloaded official `go1.27.2.linux-amd64.tar.gz`, verified its published SHA-256
  using `sha256sum -c -`, and installed it in ignored local toolchains. The machine's
  initial `/usr/bin/go` was unrelated software, so bare `go version` failed.
- Queried pinned Go module versions through `go list -m -json`; installed the exact
  versions listed in go.mod with `go get`. Added pgxpool's missing module checksum
  using `./scripts/go.sh get github.com/jackc/pgx/v5/pgxpool@v5.11.0`.
- Ran `npm --cache /workspace/.npm-cache install --save-exact --ignore-scripts` in
  apps/web, then added `@types/node@24.19.2` with save-dev/save-exact. Subsequent
  installations use the frozen package-lock through `npm ci --ignore-scripts`.
- Ran `./scripts/go.sh mod download` and `./scripts/go.sh mod verify` successfully.
- Changed generated cloud filesystem permissions on the nonsecret collector config
  to 0644 and executable bootstrap/helper scripts to 0755. Repaired initial role
  bootstrap with `docker compose --env-file .env -f infra/compose/compose.yaml exec
  -T postgres sh /docker-entrypoint-initdb.d/01-roles.sh`, then restarted the owned
  collector. No database volume or business data was deleted.
- Temporarily generated an ignored `.env.test` containing a public test API origin,
  ran the same npm build command with `-- --mode test`, asserted the bundle contained
  that origin, and removed only that generated file. Normal build was rerun to
  restore `/api` configuration. The first attempt raced an npm refresh and failed
  with `tsc: not found`; the serialized retry passed.

## 4. Results, including failures

| Check | Final outcome | Evidence |
| --- | --- | --- |
| `make setup` / frozen installs | PASS | Repeated install and Go module integrity verification |
| Source-document SHA-256 | PASS | 84 entries, original docs unchanged |
| `make check` | PASS | Nonmutating formatting, vet, Go integrity, ESLint, TS typecheck |
| `make test` | PASS | Go race/unit/security, frontend and real-service integration |
| Go JSON test evidence | PASS | 12 top-level unit/security tests + 37 subtests; 49 outcomes, 0 failed, 0 skipped |
| Integration JSON evidence | PASS | 3 top-level tests + 3 outage subtests; 6 outcomes, 0 failed, 0 skipped |
| Frontend Vitest | PASS | 3 files, 11 tests |
| `make build` | PASS | API/worker/migrate compile; frontend production bundle builds |
| PostgreSQL and Redis startup | PASS | Healthy containers plus real connections and role checks |
| Migration up/status/repeat | PASS | Version 00001 applied; repeated up applies 0 migrations |
| Application-role restrictions | PASS | No superuser/BYPASSRLS/role/database creation; table creation and migration-ledger reads rejected |
| Privileged-role readiness guard | PASS | Real superuser connection is rejected by runtime readiness |
| Live smoke, default and custom port | PASS | API health/readiness, frontend proxy, CORS denial, trace correlation |
| Live telemetry delivery | PASS | Collector received traces and runtime/HTTP metrics from current smoke run |
| Missing collector | PASS | API starts and serves; readiness remains dependency-based; shutdown succeeds |
| Dependency outages | PASS | Health remains 200; readiness is 503 and identifies the unavailable dependency |
| Runtime/migration PG TLS regressions | PASS | Duplicate sslmode, ssl alias and connection/credential overrides rejected |
| Redis TLS regression | PASS | skip_verify overrides rejected |
| OTel TLS regression | PASS | HTTPS traces AND metrics never reach a plaintext collector despite INSECURE env overrides |
| Production frontend root env configuration | PASS | Public test origin appeared in generated bundle; default build restored |
| `make dev` | PASS, startup/functionality | Full stack, API, worker and Vite start; real endpoint/proxy responses verified |
| Owned dev coordinator SIGINT/SIGTERM cleanup | PASS | Both signals stop the coordinator's API/frontend processes; API/worker lifecycle is also integration-tested |
| Execution runner's Ctrl-C connection interrupt | FAIL, runner limitation | Command connection closes without propagating the signal; owned coordinator was stopped explicitly |
| npm dependency audit | PASS | 0 vulnerabilities including development dependencies |
| Independent review | PASS | No unresolved BLOCKER/HIGH/MEDIUM after reproductions and regression checks |
| GitHub-hosted Actions run | NOT RUN | Workflow exists; no push/remote CI execution performed |
| Optional plain `go mod tidy` | FAIL, network limitation | Upstream Goose SQLite test zip redirects to blocked storage.googleapis.com |
| PostgreSQL tenant RLS/business/compiler/AI suites | NOT RUN | No corresponding M0 implementations; required in later milestones |
| Fresh-task restoration/publication | NOT RUN | Draft save is confirmed; user publication and new-task verification are separate |

Resolved initial failures are recorded rather than hidden:

- Default npm cache was outside writable roots. Helpers now use ignored writable caches.
- TypeScript 7 was incompatible with the linter peer range. Pinned supported TS 6.0.3.
- Early Go build lacked a pgxpool checksum. Corrected the managed module graph.
- HTTP unsupported methods initially returned 404. Corrected to JSON 405 with Allow.
- Frontend lint initially flagged unused error-boundary parameters. Removed them.
- Container bind-mounted files were initially unreadable to unprivileged processes.
  Fixed file modes; repaired missing local DB roles; collector and migrations pass.
- A root-directory npm audit had no lockfile. Reran with apps/web and the actual lockfile.
- Vite environment/proxy improvement initially lacked Node typings (TS2591).
  Added pinned typings; lint/typecheck/test/build pass.
- Optional `GOPROXY='https://proxy.golang.org|https://goproxy.io' ./scripts/go.sh mod
  tidy` also failed because the alternative proxy denied the upstream SQLite zip.
  `./scripts/go.sh mod tidy -e` normalized direct/indirect declarations but reported
  the same artifact error despite a zero process exit. It is NOT counted as successful
  tidy validation. No affected artifact was used and verification was never disabled.
- A temporary build-env test overlapped npm installation and failed with tsc missing;
  rerun after installation completed passed. Final checks ran against complete locks.
- The execution runner's Ctrl-C interrupt did not propagate to the development
  process tree. Direct SIGTERM and SIGINT to the known owned coordinator worked and
  ports closed. The helper now isolates detached frontend stdin and prints its PID;
  startup instructions document direct signals when the runner cannot propagate them.

The independent review reproduced and corrected frontend proxy/env bugs, untrusted
telemetry attributes, misleading sequential readiness diagnostics, and THREE HIGH
transport validation issues. Their regressions pass in the committed-intended source.

## 5. Architecture decisions

[ADR-013](adr/013-m0-foundation-tooling.md) records standard-library HTTP/slog, pgx,
Goose, async OTLP, pinned tooling, infrastructure packages, local process/Compose
layout and recovery. It follows the buildkit's accepted decisions 001–012; no
microservice split, AI-runtime bypass, business primitive or premature extension.
An ADR template is available in docs/adr/TEMPLATE.md.

## 6. Major dependencies

| Dependency | Purpose |
| --- | --- |
| pgx v5 | Maintained PostgreSQL driver with verified TLS and database/sql pooling |
| go-redis v9 | Small authenticated/TLS-capable Redis infrastructure adapter |
| Goose v3 | Reviewed embedded transactional SQL migrations and advisory locking |
| OTel SDK/exporters + HTTP/runtime instrumentation | Vendor-neutral traces/metrics and W3C propagation |
| React / React DOM | Application shell and error boundary |
| React Router | Declarative shell/status/not-found routing |
| TypeScript / Vite / React plugin | Strict frontend types and managed local/build tooling |
| ESLint / TypeScript ESLint / hooks plugin | Static checks and React hook correctness |
| Vitest / Testing Library / jsdom | UI, client, failure-state and configuration tests |

Transitive versions and integrity metadata are managed in go.sum/package-lock.json.
No AI provider, workflow engine, OPA, S3 SDK, Rust/Python service or Kubernetes dependency.

## 7. Security observations

No production credentials are committed. Development credentials are random and
stored only in ignored 0600 .env; this is local development, not a production
secret-management system. Production requires verified PostgreSQL TLS, verified
Redis TLS, explicit passwords, exact HTTPS CORS and HTTPS telemetry. Effective
driver settings are checked to prevent alias/duplicate/env transport downgrades.

Operational endpoints are unauthenticated and must be network-restricted in any
production deployment. Inbound HTTPS terminates at a trusted proxy; the listener
defaults to loopback. Tenant identity, policy, auth, RLS data tables, rate limiting,
business audit and promotion protections remain their planned milestones. No tenant
data or effectful business endpoint exists in M0. Do not expose this as an ERP yet.

Telemetry redacts request paths/hosts/header data, DSNs, raw driver errors, bodies
and panic details. OTel export failures do not gate application readiness. Safe
sandbox/tool/AI behavior is documented for later work, not falsely claimed implemented.

## 8. Known limitations and recovery

The worker is intentionally idle. Business modules, metadata, compiler, workflows,
outbox, integrations, S3 attachments, sandbox, promotion and AI Builder are absent.
Collector debug summaries are not durable observability storage. No production
deployment, backups/PITR, HA, restore drill, throughput SLOs, Go vulnerability scan,
business security approval, or real production TLS connection was performed.

The optional upstream dependency-test artifact needs a network policy addition;
ordinary locked setup, integrity verification, tests and builds pass already. The
storage.googleapis.com addition was saved to the environment draft, not activated.

Stop only owned processes; make infra-down retains PG data. Preserve .env alongside
existing DB credentials. Do not delete volumes to fix authentication. Migrations
have no destructive CLI down; review failures and forward-fix. Metadata rollback
and external compensation are future distinct mechanisms, never universal undo.

## 9. M0 acceptance and reusable environment

All ten requested acceptance criteria are verified: backend compilation, frontend
build, both test suites, PostgreSQL and Redis startup, migration tooling, health,
dependency-aware readiness and collector-unavailable startup. The app shell,
architecture boundaries, dev commands, configuration, CI and instructions exist.

Cloud draft persistence is confirmed (`status: saved`, `requires_publish: true`):

- install_script: checkout-root make setup, make env, make build.
- start_skill: existing isolated checkout, M0 scope, make dev, functional readiness,
  test/build commands, process restart/cleanup, credential/data preservation.
- network: added storage.googleapis.com while retaining package-manager presets.

No new secret requirements or environment values were invented. Review and save
the changes in environment settings, then publish to reuse the prepared snapshot.
Saving does not execute scripts, apply network settings, or publish; a new task
must independently validate restoration. M0 current-instance validation is complete.

## 10. Git status

The original repository had no tracked files or commits. The implementation and
documents are new untracked files pending review; no commits or pushes were made.
Commit source, migrations, configuration examples, workflow, docs and lockfiles.
Do not commit .env, .local/toolchains/caches/results/logs, node_modules, bin or dist.
All are ignored; the temporary env-test file was removed. No original tracked source
was changed and the supplied buildkit checksums remain intact.

M0 STATUS: PASS

Recommended next milestone: M1 — Tenant / Identity / Policy.
Stop here; M1 requires a new user task.
