# R3b — Governed enum and reference indexes

16 October 2026 (Asia/Calcutta). Implementation checkpoint, not final acceptance.
Starting clean commit `5f6455a` on `remediation/m9r-foundation`. No main merge,
production activation or M10. Complete R3b/M9R remains PARTIAL.

## Contract and implementation

ADR041 was written and independently reviewed before implementation. Additive
migration 49 extends the existing scalar registry/projections to enum/reference;
no source records, IR, earlier migrations or indexes are rewritten/deleted.
Existing GET/register/build routes, owner/admin/live binding checks, quotas,
epoch fencing, atomic audit/checkpoint, copy/independent verification and trigger
maintenance remain. Four fixed partial text indexes are platform DDL only.

Registration derives sorted enum members and reference targets from immutable
source. A private options invariant requires canonical byte ordering, exact
unique strings, 1–100 members and existing 128-character/512-byte bounds. Invalid
NULL/shapes return definite false. Reference syntax remains lowercase UUID text;
ordering stays database-collated text and does not grant target access or replace
relationship integrity. Readers compare exact type/options/target/storage hash
and collation witnesses. New helpers are private, distinctly named and invoker;
the old three-argument converter and old readiness function-name contract remain.

All certified queries retain snapshot admission, source-version joins and
correlated live-view point fetches, with authorization/filter/cursor predicates
before LIMIT. Existing string overflow fallback and numeric/date behavior remain.
No compiler/metadata format change or new major dependency is introduced.

## Evidence at checkpoint

| Exact command | Result |
| --- | --- |
| `make infra-up migrate migration-status` | PASS baseline: healthy PG/Redis/collector, migrations 1–48; credentials preserved. |
| `./scripts/go.sh test -race ./internal/runtime/data ./internal/platform/access` | PASS baseline 1.831s; access has no unit files. Implementation rerun also PASS. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarIndexPermissionsAndFailureRollback$' ./tests/integration` | NOT RUN: initial selector matched zero tests. Corrected selector below executed the real security fixture. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarIndexPermissionsAuditAndCoverage$' -v ./tests/integration` | PASS 2.658s baseline. |
| `make migrate migration-status` | PASS: forward 49 applied, all earlier migrations retained. |
| `./scripts/go.sh fmt ./internal/runtime/data ./tests/integration` | PASS; changed Go files formatted. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RChoiceIndex' -v ./tests/integration` | PASS 55.093s, initial three groups. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar\|Choice)Index\|TestM1FreshDatabaseMigrations)' -v ./tests/integration` | PASS 98.020s, 22 groups: old numeric/string/date, fresh migrations and four expanded choice groups. |
| `make test-choice-performance` | PASS 62.042s: twelve cases/1200 measured requests, zero errors; worst p95 259.768ms, p99 282.625ms, throughput 52.10–126.77/s. Both100k indexes copied/verified in203 checkpoints (36.255s combined build),926-byte average hot record. Actual requested indexes/202 source visits in every case. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RChoiceIndexRawMaintenanceAndReadinessDrift$' -v ./tests/integration` | PASS 2.759s, expanded explicit NULL-array witness rejection and effective driver/role validation. |
| `make check` | PASS: checksums/format/vet/module verification/frontend lint/typecheck. |
| `node scripts/with-env.mjs node .local/reports/r3b-enum-reference-legacy-probe.mjs` | PASS: retained pre-extension API binary starts and responds200 healthy/ready after49, graceful shutdown. Binary module reports f99537f with dirty documentation; SHA256 d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea. This is startup/readiness evidence, not a complete old-binary CRUD/rollback certification. |
| Independent executable review, final full suite/build/browser and retained performance gates | NOT RUN yet. |

Executed cases include 1206 populated rows, independently captured pre-index
ASC/DESC/null/tie/cursor/join results, checkpoint replay, patch/clear/delete
maintenance, 128-emoji enum keys, source option witness faults, raw invalid enum/
UUID writes, private helper denial, owner/manager/tenant/field denial, actual
audit rollback, options/readiness drift and legacy catalog-reader compatibility.

The initial enum negative in the older numeric route fixture became valid under
the shared API; it now retains intrinsic/missing rejection and the new fixture
executes an actually unsupported boolean field. Assertions were not weakened to
hide a product failure. No source kit was edited. Local logs/configuration,
credentials, dependencies and generated outputs remain ignored.

## Recovery and remaining gates

Forward recovery only: retain source records, options and checkpoints. Unknown
POST outcomes require authenticated GET reconciliation before further builds.
Invalid witnesses/source values block readiness/build; never coerce, drop
constraints, delete evidence or broaden grants to recover. Retain additive schema
when reverting a compatible binary; incomplete indexes use the bounded fallback.

Reference indexes do not promise target disclosure, new foreign-key semantics,
uniqueness or constant-time deep cursor traversal. Datetime/bool/search indexes,
R3c uniqueness, R3d SQL aggregates, general selective/join capacity, role-definition
UI, external identity certification, R6 authority, R7 operations and production
execution remain separate work. Real collation-library upgrade/reindex and backup/
DR drills are not executed here. Historical R7 failures remain open.

Environment tools/services and real baseline checks passed. The reusable startup
draft was refreshed to preserve M9R branch/data/credential boundaries and replace
the obsolete fixed migration count with embedded-migration verification. Draft
persistence is confirmed; publication/fresh-machine restoration is not performed.
