# ADR-013: M0 process, migration, and development tooling

- Status: accepted
- Date: 2026-10-11
- Scope: M0 only

## Context

The buildkit fixes the Go modular monolith, React/TypeScript, PostgreSQL 18,
Redis and OTel architecture (existing decisions 001–012). M0 needs reproducible
tooling but does not implement any business modules or tenant data.

## Decision

Use Go 1.27.2 (supported release verified against go.dev release metadata), Node
24.19.0, React 19.3, Vite 8.3, and TypeScript 6.0.3. TypeScript 7 is deferred
because typescript-eslint 8.71.1 supports versions below 6.1. Lock all packages.
Use Go net/http and slog, pgx through database/sql with a bounded pool, go-redis,
and the OTel SDK plus HTTP/runtime instrumentation and asynchronous OTLP HTTP
export. PostgreSQL readiness spans are the M0 database instrumentation; future
repository methods must instrument sanitized operations without SQL parameters.

Use embedded Goose SQL migrations in a dedicated cmd/migrate process with an
explicit migration DSN, transaction support, and PostgreSQL advisory locking.
Expose only up/status. No generic application SQL endpoint or destructive down.
The sole M0 migration creates iam/control/runtime/audit schemas, no business tables.
Create separate development admin/migration/application roles; no application
DDL or migration-ledger access. Tenant context, policy and RLS remain M1/M2 work.

Use infra Compose for local PostgreSQL 18.6, Redis 8.2.3, and collector 0.148.0.
Bind ports to loopback, generate random local credentials, and preserve .env and
database volumes. The API/idle worker/frontend run as local processes. Collector
debug export is a lightweight local destination, not a production storage backend.
Use infra health checks plus an explicit collector readiness request: a running
container alone is insufficient. No optional observability UI or object store yet.

Add internal/config and internal/platform as infrastructure/composition packages,
plus cmd/migrate beyond the proposed directory layout. This keeps configuration
and adapters out of business module contracts. Future bounded modules are README
placeholders, not invented interfaces or empty implementation packages.

## Consequences

Few infrastructure dependencies, clear entrypoints, one build, and no microservice
split. Go and TypeScript caches/builds are ignored. Local .env contains development
secrets on disk (0600); production uses deployment-injected credentials and TLS.
OTel export failures degrade telemetry and never block application readiness.
Production binary deployment and metadata promotion remain separate workflows.

## Verification and recovery

make check, make test, make build, and scripts/smoke.mjs cover the foundation.
Migrations are repeatable and normal-role privileges are checked against real PG.
Dependency-down tests check health stays live while readiness returns 503. API
and worker cancellation are bounded. Stop owned processes with Ctrl-C and local
infrastructure with make infra-down; the database volume is retained. Restore
credentials from your secure local copy for an existing volume; do not regenerate
.env and expect PostgreSQL's stored passwords to change. Forward-fix database
changes; do not delete volumes or imply metadata rollback reverses external effects.
