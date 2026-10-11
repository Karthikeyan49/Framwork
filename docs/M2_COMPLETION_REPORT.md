# M2 completion report

Date: 2026-10-11. Scope: M2 — PostgreSQL RLS / Data Access only.

## 1. Summary

Implemented a reusable checked, bounded PostgreSQL transaction boundary and narrow
repository Executor port; protected API replies now wait for the outer audit/commit.
Identity/tenant rebinding clears previous context. Migration 00004 verifies actor
claims against active IAM identity and tenant membership in RLS, and installs a
migration-only forced-RLS helper for future tenant tables. Real normal-role tests
exercise isolation and recovery on disposable typed-core/JSONB tables; no production
business entity, metadata registry, query AST or CRUD API is introduced.

## 2. Important repository structure

```text
AGENTS.md / CLAUDE.md / README.md
internal/platform/postgres/{postgres.go,transaction.go}
internal/platform/access/access.go
internal/{identity,tenant,audit}/     Public repository contracts use Executor
internal/runtime/data/README.md      M2 infrastructure / M5 business boundary
db/migrations/00004_tenant_data_boundary.sql
db/rls/README.md                    Future migration security contract
tests/integration/tenant_data_test.go
tests/integration/role_membership_test.go
tests/integration/identity_policy_test.go   Full fresh chain and security drift
docs/adr/015-m2-tenant-data-boundary.md
docs/M2_INDEPENDENT_REVIEW.md / M2_COMPLETION_REPORT.md
```

The Go modular monolith, React frontend, Compose, CI and managed dependencies remain
in the existing repository structure. Source buildkit documents are unchanged.

## 3–4. Commands actually executed and results

Commands ran from `/workspace/Framwork`. Repeated runs are grouped. Read/search,
file-writing and formatting details are excluded except the explicit format target;
this is a verification log, not a transcription of every shell operation.

| Actual command | Result | Evidence / history |
| --- | --- | --- |
| `make fmt` | PASS | Explicit formatting before nonmutating checks; repeated after changes. |
| `make migrate` | PASS | Applied additive migration00004 through Goose. |
| `make docs-verify` | PASS | Final 84 original source checksums after report/review creation. |
| `./scripts/go.sh vet -tags=integration ./tests/integration` | PASS | Integration fixtures static validation. |
| `make migration-status` | PASS | 00001–00004 applied. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 ./tests/integration -run 'TestM1AuthenticatedTenantAndPolicyBoundary\|TestM1RuntimePrivilegesRLSAndPoolContext\|TestM1FreshDatabaseMigrations'` | PASS | Initial M1 compatibility/fresh-chain regression after M2 migration. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -v ./tests/integration -run TestM2` | FAIL initially | Inactive fixture passed two arguments to a one-argument SQL statement. Isolation, pool/recovery, concurrency and installer tests passed; corrected fixture arguments before acceptance. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 ./tests/integration -run TestM2` | PASS | Corrected M2 tests; added authorized scope switch and identity-rebind negatives. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 ./tests/integration -run TestM1FreshDatabaseMigrations` | PASS | Fresh four-migration chain, repeated up, RLS/privilege drift and recovery. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -v ./tests/integration -run TestM1FreshDatabaseMigrations` | PASS | Private non-inherited table/function/schema owner-role regressions included. |
| `make check` | PASS | 84 source checksums, format, Go vet/module integrity, frontend lint/typecheck. |
| `make test` | PASS | Go race tests, all 14 frontend tests, real PG/Redis integration; no skips. Repeated after role/column-grant review fixes. |
| `make build` | PASS | API, worker, migration/operator binaries and production frontend bundle. |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS | Live health/readiness, PG/Redis/IAM, frontend proxy, auth denial, CORS, trace correlation, collector traces/metrics, graceful shutdown. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS | Zero production dependency vulnerabilities reported. |
| `curl --fail --retry 10 --retry-connrefused http://127.0.0.1:13133/` | PASS | Collector health reported available. |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps` | PASS | PostgreSQL/Redis healthy; collector running with loopback bindings. |
| `git diff --check` | PASS | No whitespace errors. |
| `git check-ignore .env .local/identity/private.pem .local/identity/jwks.json bin/api apps/web/dist apps/web/node_modules` | PASS | Secrets, keys, dependencies and generated artifacts ignored. |
| `git status --short` | PASS | M2 source/docs changes recorded; no generated artifacts staged. |
| Hosted GitHub Actions | NOT RUN | CI commands executed locally; no M2 push/hosted run. |
| Commit-acknowledgment loss / network partition fault injection | NOT RUN | Unconfirmed commit error and no automatic retry implemented; no live ambiguous-commit fault injection claimed. |
| Business browser E2E / metadata Query AST / file-cache-queue isolation / backups or multi-node load | NOT RUN | These features are outside M2. |

Make verification commands actually expanded and ran:

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
node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate up
node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate status
```

All final verification commands above passed. Packages with no direct unit-test
files are compiled; transaction/audit/access behavior is tested against PostgreSQL.
Independent reviewer commands and findings are recorded separately in the review.

## 5. Architecture decisions

ADR-015 records transaction ownership, deadlines and cleanup; trusted-Go-only SQL;
identity/membership-backed RLS with bounded read-only SECURITY DEFINER lookup ports;
migration-only installer; scoped keys/FKs; privilege drift checks; audit-before-reply;
commit uncertainty/no blind retry; forward-only security recovery. ADR-013/014 and
original architecture decisions remain intact. No microservices or early extensions.

## 6. Dependencies introduced

None. Existing database/sql + pgx supply connections/transactions, Goose applies the
atomic migration, and OpenTelemetry instruments sanitized transaction spans. Existing
Go standard library, React/Vitest and Compose verification are retained. Dependency
manifests and lockfiles are unchanged.

## 7. Security observations and review fixes

- Normal roles cannot own protected schemas/tables/functions, bypass RLS, use
  replication/DDL operator privileges, or hold dangerous file/server-program/all-data
  memberships. Non-inherited owner memberships are rejected too: the independent
  reviewer demonstrated that the prior predicate could miss SET ROLE ownership.
  Fixed guard and repository private-probe regression reject this configuration.
- Both table-level and column-level privileges are checked where applicable.
  Disabled/non-forced RLS, TRUNCATE, column REFERENCES, column-only reads without
  RLS and row_security=off leases fail before runtime callback/readiness succeeds.
- No tenant setting alone grants access. Verified identity, active actor, membership
  and tenant are required. Failed tenant switches and identity rebind clear old scope.
  Tenant-inclusive keys/FKs and both USING/WITH CHECK protect reads and mutations.
- SQL/GUC access is a trusted application boundary: PostgreSQL does not verify JWT
  signatures. Stolen application credentials plus arbitrary SQL could forge all
  identity claims. Protect credentials/network access; never expose SQL to AI/metadata.
- Forced RLS does not make privileged DDL operators untrusted or audit externally
  immutable. Reviewed migrations and production grant/function drift monitoring remain
  operational requirements. Go entity/row/field/action policy is still required.
- Results are buffered until mandatory audit and outer commit. Panic/cancellation/error
  rolls back, cleanup is separately bounded, unsafe leases are discarded, retained
  executors fail after completion, and no automatic write retries occur.
- No secrets are committed or logged by the added paths. Raw driver SQL/parameters
  and panic payloads are excluded from transaction telemetry/API errors.

## 8. Known limitations and recovery

M2 creates no production business tables. Metadata registry is M3; query AST/CRUD
is M5. There are no business worker jobs, queues, outbox, AI, sandbox or promotion.
Future migrations must call the installer atomically and review backfill access;
operators have no automatic business-data RLS policy. Partition child access and
non-tenant control records need explicit reviewed designs, not blanket runtime grants.

Repeatable-read membership is valid for the bounded transaction snapshot; new
transactions observe revocation. Per-transaction catalog checks and fixed limits
need workload measurements before tuning; no production throughput claim is made.
Commit failure is unconfirmed, not proof of rollback: inspect durable state before
retrying. Network-partition/ambiguous-commit fault injection was not run.

Migration00004 must precede the M2 API binary. Goose applies it atomically; repair
and rerun failed application. Applied security changes require a forward fix; Down
intentionally refuses weaker IAM policies. Retain strengthened compatible policies
when reverting an application binary. Never delete audit history or reset the volume.
Integration tests remove only unique tenant/table/database/private-role fixtures;
existing local keys, .env, data volume and append-only audit are preserved.

## 9. Verification against M2

| Acceptance criterion | Result | Evidence |
| --- | --- | --- |
| PostgreSQL tenant RLS policies | PASS | Actual migration helper, ENABLE/FORCE, symmetric read/write checks, no-scope denial and scoped keys/FKs |
| Least-privilege normal application role | PASS | Ownership/global/built-in/member-role checks, normal-role negative DDL/RLS tests, non-inherited owner regression, column grants/drift |
| Transaction-scoped tenant context | PASS | Opaque verified identity/tenant ports, live membership, scoped settings, exact lease check, pool cleanup, switch/rebind negatives |
| Tenant isolation tests | PASS | Two-tenant SELECT/INSERT/UPDATE/DELETE, unrestricted scan/export-equivalent rows, joins/aggregates, relationship/unique scope, concurrency |
| Failure/recovery tests | PASS | Repository/audit failure, panic, cancellation, rollback, retained executor, unsafe lease discard/reuse and security drift repair |
| Existing M0/M1 foundation retained | PASS | Full check/test/build, four-migration chain, live health/readiness/dependency/telemetry/shutdown smoke |
| Independent adversarial review | PASS | Review findings fixed and rechecked; see M2_INDEPENDENT_REVIEW.md |
| Human milestone gate | PENDING USER REVIEW | Reviewable implementation delivered; no M3 implementation started |
| Stop before M3 | PASS | No metadata registry/compiler/business query runtime introduced |

## 10. Git and environment status

M0/M1 were previously committed and pushed to origin/main at
7d60ad50087a624beb53626025c5c409a58cdba6. M2 changes are currently unstaged/uncommitted
on main; no M2 commit/push or production deployment was performed.
The local development database was migrated to00004. Commit M2 source, SQL migrations,
README/agent contracts, ADR and completion/review reports together after review.
Do not commit bin, dist, node_modules, .local, .env, keys, logs or caches. No generated
artifacts or dependency changes need committing. Original buildkit checksums pass.

PostgreSQL, Redis and collector remain running. Smoke-owned application processes
were shut down gracefully; use make dev to start them. No destructive volume reset.

M2 STATUS: PASS

Recommended next milestone:
M3 — Metadata Registry

Delivery update2026-10-11: the user subsequently authorized later milestones and
explicitly requested pushing all work. Earlier gate/Git statements above describe
that milestone snapshot. Cumulative M2–M6 delivery and final Git evidence are recorded
in M4_M6_COMPLETION_REPORT.md. No production activation is authorized or performed.
