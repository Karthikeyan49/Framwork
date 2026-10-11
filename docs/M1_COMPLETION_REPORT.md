# M1 completion report

Date: 2026-10-11. Scope: M1 — Tenant / Identity / Policy only.

## 1. Summary

Implemented provider-independent pinned-public-key RS256 authentication, durable
users/tenants/memberships/fixed roles, opaque trusted identity/tenant contexts,
central native RBAC + ABAC policy, append-only correlated security audit and an
explicit operator provisioning command. All protected routes, including session
and tenant discovery, enforce policy and supported obligations and commit audit
before returning data. Runtime credentials cannot provision identities or change
roles. A React access console connects with memory-only API tokens and verifies
server-authorized workspace selection. No ERP or M2 business-data runtime added.

The user explicitly selected provider-independent verification with a local
issuer. The original architecture documents remain unchanged; 84 original-file
checksums pass. ADR-014 documents the decisions and narrow IAM RLS prerequisite.

## 2. Important repository structure

```text
AGENTS.md / CLAUDE.md / README.md / .env.example / Makefile
cmd/{api,worker,migrate}/          Existing foundation processes
cmd/iam/main.go                  Explicit, separately credentialed operator CLI
internal/identity/               Verifier, opaque Principal/User, owned user store
internal/tenant/                 Tenant/member/role store, opaque Access context
internal/policy/                 Subject, Engine, RBAC + ABAC Native adapter
internal/audit/                  Writer, bounded state/evidence, Postgres adapter
internal/platform/access/        HTTP policy/identity/tenant/audit composition
internal/platform/{app,postgres,httpserver}/  Hardened existing wiring/role checks
internal/config/                 Explicit production identity validation + tests
apps/web/src/AccessConsole.tsx    Token/session/workspace UI and tests
apps/web/src/api/client.ts        Typed validated authenticated requests
scripts/dev-identity.mjs          Development-only key preservation/token issuance
scripts/smoke.mjs                Live foundation/IAM/auth-denial/telemetry smoke
db/migrations/00002_identity_tenant_audit.sql
db/migrations/00003_audit_provenance.sql
db/{rls,seeds}/README.md          Current scope/operator seed instructions
tests/integration/identity_policy_test.go
.github/workflows/ci.yml         Existing full-check pipeline covers new packages
docs/adr/014-m1-identity-policy-audit.md
docs/M1_INDEPENDENT_REVIEW.md / M1_COMPLETION_REPORT.md
```

## 3–4. Executed commands and results

All commands below ran from `/workspace/Framwork`. Repeated runs are grouped;
implementation-only file-writing/read/search commands are excluded from this
verification table. Make expands into the exact commands listed afterward.

| Actual command | Result | Evidence / relevant history |
| --- | --- | --- |
| `make env fmt migrate` | FAIL initially | New migration had misplaced SQL `FOR`/`TO`; PG transaction rolled back. Corrected before first successful application. |
| `make migrate` | PASS | Applied migration00002 after correction. |
| `make check test-unit` | FAIL initially | Check passed; old secure-production test fixture lacked explicit identity configuration. Updated fixture, preserving TLS regression coverage. |
| `make fmt migrate` | PASS | Applied separate additive00003; never rewrote already-applied00002 for audit state. |
| `make fmt check test-unit` | PASS | Format/check, Go race tests and 14 frontend tests. |
| `make fmt test-integration` | FAIL initially, then PASS | Initial foundation API fixture used repo-relative key path from package cwd. Corrected fixture to absolute path. Subsequent runs passed including real operator and concurrency tests. |
| `make check build` | PASS | All static checks and all four Go binaries/frontend bundle. |
| `make test-unit` | PASS | Go race tests and frontend tests after trusted-user/runtime-role fixes. |
| `make fmt check test build migration-status` | PASS | Full suite, all binaries/frontend build; migrations00001/00002/00003 applied. Repeated after fresh migration/local-issuer tests were added. |
| `make fmt check test build` | PASS | Full suite and build after central session-policy enforcement correction; rerun after module-owned audit/tenant readiness contracts. |
| `make check test-integration` | PASS | Final current-source check and real PG/Redis integration, including explicit session deny/unknown-obligation tests. |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS | Repeated, final run with rebuilt binaries: health/readiness, PG/Redis/identity/tenant/audit, frontend proxy, unauthenticated401, exact CORS, propagated trace correlation, collector traces/runtime metrics and graceful shutdown. |
| `make infra-up migrate` | PASS | Existing .env/keys preserved; PostgreSQL/Redis healthy, collector running; repeated migration up applied zero migrations. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS | Zero production-dependency vulnerabilities reported. |
| `git status --short` | PASS | All source remains untracked from the original empty M0 checkout. No commit/push. |
| `git check-ignore .env .local/identity/private.pem .local/identity/jwks.json bin/api apps/web/dist apps/web/node_modules` | PASS | All listed credentials/keys/generated/dependency artifacts ignored. |
| `git ls-files \| wc -l` | PASS | Zero tracked files, confirming inherited repository state. |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps` | PASS | PostgreSQL/Redis healthy; collector running on loopback. |
| GitHub Actions hosted run | NOT RUN | Pipeline updated and its commands executed locally; no push/hosted execution. |
| Real external identity-provider login / browser redirect E2E | NOT RUN | No provider selected/configured; pinned verifier/local issuer were explicitly selected and exercised. |
| Browser visual/automated browser E2E | NOT RUN | React component/client tests and live Vite proxy smoke executed; no full browser runner used. |

The initial migration failure was reproduced in a diagnostic command, with an
explicit transaction and rollback (no credential output):

```sh
node -e 'const fs=require("fs"); process.stdout.write("BEGIN;\n"+fs.readFileSync("db/migrations/00002_identity_tenant_audit.sql","utf8").split("-- +goose Down")[0]+"\nROLLBACK;\n")' | docker compose --env-file .env -f infra/compose/compose.yaml exec -T postgres psql -U framework_migrator -d framework -v ON_ERROR_STOP=1
```

Result: FAIL as expected while reproducing the original `CREATE POLICY` syntax
error. The failed statement aborted the transaction; the fixed migration later
applied through Goose. No earlier failures are counted as passes.

Executed Make verification expansions:

```sh
node scripts/verify-docs.mjs
./scripts/check-format.sh
./scripts/go.sh vet ./cmd/... ./internal/... ./db/... ./tests/security/...
./scripts/go.sh mod verify
npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run lint
npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run typecheck
./scripts/go.sh test -race ./cmd/... ./internal/... ./db/... ./tests/security/...
npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web test
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 ./tests/integration
./scripts/go.sh build -o bin/api ./cmd/api
./scripts/go.sh build -o bin/worker ./cmd/worker
./scripts/go.sh build -o bin/migrate ./cmd/migrate
./scripts/go.sh build -o bin/iam ./cmd/iam
npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run build
node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate status
```

Formatting used `make fmt` deliberately; checks afterward were nonmutating. All
final expansions above passed. Go packages without unit-test files are compiled;
audit/access/PostgreSQL behavior is exercised by actual DB integration tests.
Frontend: 14 tests in four files passed. No integration tests were skipped.

## 5. Architecture decisions

ADR-014: pinned public RS256 access-token contract, separate development issuer,
native replaceable RBAC + ABAC policy, opaque trusted subject/context, explicit
operator provisioning, atomic state/audit, narrow forced IAM RLS, append-only audit
provenance, and memory-only token access console. ADR-013/M0/buildkit remain intact.
M1 IAM RLS protects the tables introduced now; M2 reusable business RLS/data access
is not implemented. PostgreSQL role names follow the existing M0 contract.

## 6. Dependencies

No new Go or npm dependencies. Standard-library crypto/RSA/SHA256/base64/JSON
verify the deliberately narrow token contract; database/sql with existing pgx
provides bounded repositories/transactions. Existing Goose applies migrations;
existing OTel records sanitized PostgreSQL probes/trusted policy correlation.
Existing React/TypeScript/router/Vitest provide the access console and tests.
Node's standard crypto module provides only the local development issuer.

## 7. Security observations and review fixes

- Signature, issuer/audience/type/expiry/not-before/issued-at/lifetime/key-size/key-ID
  verification; unsupported algorithms/critical extensions/private key sets deny.
- Production identity configuration is explicit and verified independently from
  database/Redis/telemetry TLS. Partial development external bindings fail closed.
- Browser tenant headers/bodies/JWT tenant/role claims cannot grant access. Active
  user/membership/tenant checks run in each request transaction; subsequent requests
  see removed roles/revoked memberships/disabled accounts. No authorization cache.
- Default-deny policy covers unknown scopes, cross-tenant resources, ownership and
  unsupported obligations. Session discovery also enforces central policy.
- Reviewer reproduced the migration-role runtime misbinding HIGH finding. Fixed
  catalog privilege checks and protected-route checks reject migration/owner/DDL
  credentials. Normal-role IAM writes and audit mutation/provenance spoof fail.
- Audit append/commit failures return unavailable before protected data. Operator
  state and audit are atomic; unconfirmed COMMIT wording correctly requires state
  inspection rather than falsely claiming rollback. Concurrent owner removals
  serialize and retain one owner.
- Reviewer requested module-owned readiness query, public-key-only validation,
  owner bootstrap and reconstructible operator state; all were implemented and
  verified. See the independent review for actual reviewer commands/status.
- No secrets, signing keys, tokens, DSNs or generated outputs are committed.
  Audit is append-only to ordinary DML, not immutable against privileged DDL;
  stronger operational separation/external immutable storage remains future work.

## 8. Known limitations and recovery

API access-token authentication is implemented; provider redirect/PKCE login,
refresh/session cookies, MFA, rate limiting, account recovery and custom role
administration are not. Service-specific privileges, field masking, workflows,
tools and promotion scopes deny until their later milestones. Pin rotation requires
API restart, accepting old/new keys until outstanding tokens expire. Synchronize
clocks. Lists are bounded to 100 without pagination; role catalog is fixed.

Operator references are declared attribution, not authenticated human identity;
actual database writer is recorded. Operator credentials are privileged and must
be tightly controlled in production. No role-management HTTP UI is supplied.
Read-only requests already in flight can finish with their transaction snapshot;
subsequent requests observe revocation. Business-data RLS, CRUD/query AST,
metadata/compiler, workflows/outbox, AI/sandbox/promotion remain later milestones.

Preserve existing .env, private/public local key pair and PostgreSQL volume. Local
keys are owner-readable and must never be deployed as production keys. Startup
creates no identities/roles automatically. Use the README's explicit operator
command; transfer ownership before demotion/revocation/disable. Recover with a
reviewed new state/audit entry, never audit deletion or volume destruction. Failed
transactions roll back; uncertain COMMIT requires checking state before retry.
Fresh-database verification created and removed only its uniquely named temporary
DB, including an isolated missing-audit-upgrade fixture that readiness rejects. Integration IAM fixtures are cleaned up; append-only security evidence remains.

## 9. M1 acceptance

| Criterion | Result | Verification |
| --- | --- | --- |
| Trusted identity | PASS | Cryptographic negative tests, opaque types, active durable user lookup and real local issuer authentication |
| Trusted tenant context | PASS | Verified membership, forged path/header/body/token claims deny, transaction-local RLS/context pooling tests |
| Users | PASS | Explicit stable issuer/subject provisioning, durable active status, disabled/unknown identity denial |
| Memberships | PASS | Tenant-scoped durable memberships, live revocation and owner/admin inspection, no cross-tenant list leak |
| Roles | PASS | Explicit fixed role catalog, policy matrix, removed-role denial, serialized last-owner safeguards |
| Central policy interface | PASS | Replaceable Engine, RBAC+ABAC self/type/tenant/actor matching, decision ID/version and obligations, session included |
| Audit | PASS | Security decisions correlated, insert-only evidence, state/provenance, mutation denial, audit-failure and atomic recovery tests |
| CI/developer foundation retained | PASS | Check/test/build, three migration statuses/fresh application, real infrastructure and live smoke |
| Independent adversarial review | PASS | Independent Go/race, M1 integration, frontend/static checks; reproduced issues fixed and rechecked |
| Human milestone gate | PENDING USER REVIEW | Reviewable result delivered; no deployment/push or M2 work authorized by completion |
| Stop before M2 | PASS | No business-data repository/query/CRUD/RLS framework added |

## 10. Git and environment status

The original checkout has zero tracked files and an unborn branch; all M0 source
was untracked before M1. M1 changes remain alongside that foundation, unstaged and
uncommitted. No commit/push/deployment performed. Commit source, migrations,
source documentation/ADRs/reports and managed lockfiles when reviewing the complete
foundation. Do not commit .env, .local (credentials/signing keys/toolchains/caches),
node_modules, dist, logs or bin. Go/npm dependency declarations and locks did not
change for M1. The original buildkit under docs remains checksum-identical.

PostgreSQL/Redis/collector are left running; smoke-owned API/worker/Vite processes
were stopped gracefully. Use `make dev` to start the application. Updated M1 cloud
startup instructions were saved as a configuration draft; install/repository/network
settings were preserved. The draft is not applied/published: review and save in
environment settings, then publish if you want it used in future environments.

M1 STATUS: PASS

Recommended next milestone:
M2 — PostgreSQL RLS / Data Access
