# M3 completion report

Date: 2026-10-11. Scope: M3 — Metadata Registry only.

## 1. Summary

Implemented embedded JSON Schema 2020-12 and matching Go models for seven authoring
primitives: entity, field, relationship, page, rule, workflow and tool. Strict JSON
and restricted YAML converge into a private validated typed Source with an exact
normalized storage hash. A tenant-scoped registry exposes immutable draft append,
historical read and bounded version listing. Central identity/membership/policy and
mandatory audit compose these operations in the existing M2 transaction boundary.

Additive migrations 00005–00007 provide forced RLS, immutable source storage,
restricted column grants/database provenance, an authoring head with expected
revision concurrency, deferred head integrity and required envelope checks.
Readiness detects selected storage/grant/trigger/constraint drift. No compiler,
production activation, business CRUD or ERP runtime is implemented.

## 2. Important repository structure

```text
AGENTS.md / CLAUDE.md / README.md
contracts/jsonschema/{application-v1.schema.json,embed.go,README.md}
contracts/examples/vendor-draft-v1.json
internal/metadata/{models.go,validation.go,yaml.go,registry.go,validation_test.go,README.md}
internal/platform/access/{access.go,metadata.go}  Policy/audit/API composition
internal/platform/{app,httpserver}/              Registry readiness and bounded POST
internal/{policy,audit}/                         Explicit metadata actions/evidence
db/migrations/00005_metadata_registry.sql
db/migrations/00006_metadata_head_integrity.sql
db/migrations/00007_metadata_storage_contract.sql
db/rls/README.md
tests/integration/{metadata_test.go,identity_policy_test.go}
Makefile / scripts/{check-format.sh,smoke.mjs}     Contract checks / readiness smoke
go.mod / go.sum
docs/adr/016-m3-metadata-source-registry.md
docs/{M3_COMPLETION_REPORT.md,M3_INDEPENDENT_REVIEW.md}
```

Original architecture/buildkit files remain unchanged. M2 implementation and its
ADR/report are the preserved, uncommitted baseline for this task.

## 3–4. Commands actually executed and results

Executed from `/workspace/Framwork`; repeated commands grouped. This records
verification/dependency commands, not every file read or shell edit. Initial
failures are disclosed separately from final acceptance.

| Actual command | Result | Evidence / history |
| --- | --- | --- |
| `make setup` | PASS | Installed/verified managed dependencies; preserved .env/local identity keys. |
| `make infra-up migrate migration-status` | PASS | Restored PG/Redis/collector; initial existing migrations applied, repeat up made no changes. |
| `./scripts/go.sh get github.com/santhosh-tekuri/jsonschema/v6@v6.0.2` | PASS | Added managed schema validator. |
| `./scripts/go.sh get go.yaml.in/yaml/v3@v3.0.5` | PASS | Added managed YAML authoring decoder. |
| `./scripts/go.sh mod tidy` | PASS | Direct dependencies/checksums resolved; repeated after YAML addition. |
| `make fmt` | FAIL initially; final PASS | An intermediate edit produced a duplicate Go header and failed formatting. Corrected the edit before accepting it; subsequent format and nonmutating checks passed. |
| `make migrate` | PASS | Applied additive 00005, then 00006, then 00007; preserved already-applied SQL. |
| `./scripts/go.sh test ./internal/metadata` | PASS | Initial compile/unit validation. |
| `./scripts/go.sh test ./internal/metadata ./internal/policy ./internal/platform/httpserver` | PASS | Initial schema/model/policy/HTTP checks. |
| `./scripts/go.sh test ./internal/platform/access ./internal/platform/app ./internal/metadata ./internal/policy` | PASS | Composition compilation and unit checks. |
| `./scripts/go.sh test -race ./internal/metadata ./internal/policy ./internal/platform/httpserver` | PASS | Race-enabled unit/policy/HTTP checks. |
| `./scripts/go.sh test -race ./internal/metadata` | PASS | Repeated after quota/NUL and JSON/YAML changes. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -v ./tests/integration -run TestM3` | PASS | All six final M3 roots; real PG, policy, tenant scope, history, CAS, audit rollback, immutable grants, body/CORS, near-quota/hash/decimal/storage envelope and HTTP YAML/recovery. Final focused run: 4.529s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 ./tests/integration -run TestM3` | PASS | Repeated focused checks after initial review fixes. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -v ./tests/integration -run TestM1FreshDatabaseMigrations` | FAIL on new fixture; final PASS | Standalone TRUNCATE was rejected by the head/version FK before reaching the trigger; the fixture expected the trigger message. Corrected the empty disposable-DB probe to include both referencing tables. UPDATE/DELETE/TRUNCATE then all hit the immutable trigger. Final targeted run: 1.645s. Shared metadata was never truncated. |
| `./scripts/go.sh test ./internal/metadata -run '^$' -fuzz FuzzJSONInputIsBounded -fuzztime=10s -parallel=2` | PASS | 22,627 executions; no crash; 11.034s reported runtime. |
| `./scripts/go.sh test ./internal/metadata -run '^$' -fuzz FuzzYAMLInputIsBounded -fuzztime=5s -parallel=2` | PASS | 43,638 executions; no crash; 5.079s reported runtime. |
| `make docs-verify` | PASS | Final 84 original buildkit checksums after report/review updates. |
| `make check` | PASS | 84 source checksums, format, vet, managed modules, frontend lint/typecheck; contracts included. |
| `make test` | PASS | Go race/unit tests, all 14 frontend tests and the complete real infrastructure/M1/M2/M3 integration suite; no service-dependent skips. Final full integration rerun after the owner-trigger fixture correction: 9.119s. |
| `make build` | PASS | API, idle worker, migration and IAM operator binaries; frontend production bundle. |
| `./scripts/go.sh vet -tags=integration ./tests/integration` | PASS | Integration fixtures static validation. |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS | Live health/readiness including metadata, PG/Redis/IAM, proxy, auth denial, CORS, trace/metric correlation and graceful shutdown. |
| `make migration-status` | PASS | 00001–00007 applied. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS | Zero production frontend dependency vulnerabilities reported. |
| `docker compose --env-file .env -f infra/compose/compose.yaml ps` | PASS | PG/Redis healthy; collector running with loopback bindings. |
| `git diff --check` | PASS | No whitespace errors. |
| `git check-ignore .env .local/identity/private.pem .local/identity/jwks.json bin/api apps/web/dist apps/web/node_modules` | PASS | Credentials, keys, dependencies and generated artifacts ignored. |
| `./scripts/go.sh mod graph` | PASS | Inspected new managed dependency graph. |
| `./scripts/go.sh list -deps ./internal/metadata` | PASS | Schema validator and YAML are linked; regexp2 is not a runtime dependency. |
| `git status --short`, `git diff --numstat`, `git diff go.mod`, `git diff go.sum`, `git log -1 --format='%h %s'` | PASS | Inspected cumulative M2/M3 source changes and unchanged base commit. |
| Hosted GitHub Actions / M3 push | NOT RUN | Local checks executed; changes remain uncommitted/unpushed. |
| Ambiguous-commit network partition fault injection | NOT RUN | Existing unconfirmed-commit/no-blind-retry behavior retained; no live partition claim. |
| Metadata browser E2E, compiler semantic fuzzing, load/aggregate quota/DR tests | NOT RUN | No authoring UI/compiler/production execution or later operations milestone implemented. |

`make check`, `make test` and `make build` actually execute:

```sh
node scripts/verify-docs.mjs
./scripts/check-format.sh
./scripts/go.sh vet ./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...
./scripts/go.sh mod verify
npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run lint
npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run typecheck
./scripts/go.sh test -race ./cmd/... ./internal/... ./db/... ./contracts/... ./tests/security/...
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

Final commands pass after fixes. Packages without test files are compiled; registry,
transaction/audit/API behavior is exercised against actual PostgreSQL. Independent
reviewer evidence is recorded separately and is not counted as implementer commands.

## 5. Architecture decisions

ADR-016 records typed closed versioned schemas, JSON/YAML normalization and limits,
source hashes versus future IR/release identity, immutable draft history, mutable
expected-revision authoring heads, tenant-inclusive deferred integrity, mandatory
policy/audit, selected readiness drift checks, concurrent-writer recovery and additive
forward-only migrations. ADR-013–015 and original architectural decisions remain.

## 6. Dependencies introduced

- github.com/santhosh-tekuri/jsonschema/v6 v6.0.2: full JSON Schema 2020-12 validation
  of the embedded contract, compiled once, with remote loading disabled. Avoids a
  partial handwritten schema implementation.
- go.yaml.in/yaml/v3 v3.0.5: maintained YAML node parser for the documented authoring
  format. The application walks a bounded restricted subset without constructors
  or alias expansion, then applies the same strict JSON/schema validation.
- go.sum includes schema dependency-test checksums such as regexp2 v1.11.0; regexp2
  is not linked into this runtime. Tidy also resolved existing Goose dependency-test
  checksums for SQLite and supporting packages; no SQLite database/runtime is added.

Existing pgx/database/sql, Goose, OTel and React dependencies remain. No frontend
package or architecture framework was added; dependency manifests/checksums should
be committed with source.

## 7. Security observations and review fixes

- Trusted identity/live membership and central policy precede authoring. Payload
  tenant IDs are rejected; read/write SQL is parameterized and forced RLS scoped.
  No application/AI SQL executor or production activation endpoint is exposed.
- Immutable versions, restricted column grants, database creator/time and mutation
  triggers prevent ordinary DML history/provenance edits. Deferred tenant-inclusive
  head integrity prevents absent-history commits. Privileged DDL remains trusted.
- Appends, head advances and mandatory audit commit atomically. Audit failure leaves
  no write; stale revisions cannot overwrite history. Replies follow outer commit.
- JSON/YAML size/complexity and exact numeric limits protect parsing. Unknown keys,
  remote refs, duplicate keys/IDs, invalid UTF-8/NUL, alias/custom YAML construction,
  inline secret-binding keys and executable structural properties are rejected.
- Independent review reproduced a HIGH accepted-source/JSONB-size read failure and
  MEDIUM NUL/storage-envelope/readiness gaps. Fixed with separate persisted-input
  bound while preserving normalized quota/hash, early NUL rejection, migration00007
  explicit nonnull envelope checks and required constraint/trigger readiness.
  Persistent real-DB regressions and independent rechecks pass.
- Metadata is tenant-visible: semantic detection of credentials inside ordinary
  strings is not provided. Never insert secrets into labels/values. Future integration
  secret handling needs reviewed opaque references and governed access.
- Aggregate tenant quotas/rate limiting, definition-level ABAC, immutable external
  archival and production dependency vulnerability tooling remain future work.
  npm audit here checks only production frontend dependencies, not all Go security.

## 8. Known limitations and recovery

All source revisions remain draft. Schema validation does not resolve references,
check expression types/arity, validate workflow graphs/capabilities/side effects or
produce executable IR. Those are M4. Rules/workflows/tools are definitions, not
executors. ERP tables/query AST/CRUD, dynamic UI, sandbox, promotion and AI remain
later milestones. Production release versioning is not approximated by source heads.

Only schema1.0.0 and a documented bounded authoring subset are accepted. No silent
schema upgrades or versioned custom extension namespaces. Rule literal arrays and
arbitrary tool payload schema keywords are unsupported. Lists cap at 50 with a
before cursor; application enumeration/metadata UI is absent. No throughput claim.

A simultaneous repeatable-read loser can return sanitized503 because its audit
transaction cannot commit; exactly one concurrent writer wins. Subsequent stale
expected revision is409. Reload/inspect durable revision and audit before retrying;
never interpret unconfirmed COMMIT as guaranteed rollback. Namespace is fixed.

Deploy migrations00005–00007 before the binary. Goose applies migrations atomically;
repair unsuccessful application and rerun. Applied changes require reviewed forward
fixes; Down refuses history destruction. Retain strengthened compatible schema and
histories on binary rollback. Tests use unique disposable DB/IAM fixtures; immutable
metadata revisions and append-only audit remain retained under test tenant IDs after
IAM cleanup. Existing .env, keys and development volume were preserved.

## 9. Verification against M3

| Acceptance criterion | Result | Evidence |
| --- | --- | --- |
| Entity schema | PASS | Closed typed entity/field references; sample/roundtrip validation |
| Field schema | PASS | Bounded types and conditional enum/currency/reference requirements |
| Relationship schema | PASS | Stable entity/field references and cardinality declarations |
| Page schema | PASS | Approved bounded component declarations, no scripts/HTML execution |
| Rule schema | PASS | Bounded allowlisted expression AST; semantic compilation deferred |
| Workflow schema | PASS | State/transition/activity/approval/timer/retry declarations; no execution |
| Tool schema | PASS | Local payload schemas, permissions, effects/sandbox/approval/timeouts/retry/audit declarations |
| JSON Schema + typed Go validation | PASS | Embedded2020-12, closed keys, typed serialization revalidation, negative corpus |
| Documented JSON/YAML authoring | PASS | Same validated source/hash path, restricted YAML and real protected API negatives |
| Versioned registry | PASS | Immutable historical revisions, tenant app heads, expected-revision concurrency, bounded cursor |
| Security + audit + failure recovery | PASS | Real normal-role isolation, policy denials, immutable grants/owner trigger, rollback, quota/storage/drift recovery |
| Existing foundation retained | PASS | Full race/frontend/infrastructure tests/build/check, seven fresh migrations, live startup/telemetry/shutdown smoke |
| Independent adversarial review | PASS | Review findings fixed and independently reverified; see M3_INDEPENDENT_REVIEW.md |
| Human milestone gate | PENDING USER REVIEW | Concrete implementation/reports delivered for review; no assumption of production approval |
| Stop before M4 | PASS | No compiler symbol/type checking/canonical IR or subsequent milestone implemented |

## 10. Git and environment status

Previously pushed M0/M1 base is 7d60ad50087a624beb53626025c5c409a58cdba6 on main.
Cumulative M2/M3 source changes are currently unstaged/uncommitted; no new commit,
push or production deployment occurred. Commit source, additive migrations, contracts,
Go manifests/checksums, agent/README instructions, ADRs and completion/review reports.
No bin, dist, node_modules, .local, .env, private/public local keys, logs, caches or
fuzz cache should be committed. Original buildkit remains verbatim (84 hashes).

Local PG/Redis/collector remain running; the development database is migrated through
00007. Smoke-owned application processes exited gracefully. Use make dev to start
applications. No destructive volume reset or history deletion.

M3 STATUS: PASS

Recommended next milestone:
M4 — Compiler V1

Delivery update2026-10-11: the user subsequently authorized later milestones and
explicitly requested pushing all work. Earlier gate/Git statements above describe
that milestone snapshot. Cumulative M2–M6 delivery and final Git evidence are recorded
in M4_M6_COMPLETION_REPORT.md. No production activation is authorized or performed.
