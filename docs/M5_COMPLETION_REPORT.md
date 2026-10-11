# M5 completion evidence

Implemented compiler.Program-only typed CRUD/query execution in isolated development/
test previews; entity/field/row policy and mandatory atomic audit; forced tenant RLS;
exact decimal/money values; parameterized bounded filters, keyset pages, declared join
and count/sum; optimistic row versions and dataset serialization protecting references.
Production preview routes remain disabled. No raw SQL, customer DDL, events, workflow,
AI, sandbox promotion or production activation. ADR018 records bounds and recovery.

Commands actually executed (final cumulative verification is recorded with M6):

| Command | Result |
| --- | --- |
| `make migrate` | PASS; applied00008 |
| `make fmt` | PASS |
| `make check` | PASS after final review fixes |
| `./scripts/go.sh test -race ./internal/runtime/data ./internal/policy ./internal/compiler` | PASS before final review fixes; rerun pending |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run TestM5 -count=1 -v ./tests/integration` | PASS; seven root tests4.355s after fixes |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -run 'TestM4\|TestM5\|TestM1FreshDatabaseMigrations' -count=1 ./tests/integration` | PASS3.862s before final review fixes |
| `./scripts/go.sh test ./internal/runtime/data -run '^$' -fuzz FuzzTypedFilterNeverEmitsCallerSQL -fuzztime=5s -parallel=2` | PASS16,682 executions/6.031s after added SQL assertion |

Earlier focused integration attempts failed for a fixture compilation mistake, then
an incorrect expected status on disabled POST routes (413 body boundary, not405).
A permanent owner-specific field-write regression also initially failed. These were
corrected; no failed run is represented as passing. Independent review additionally
found app-scoped decisions, filtered ABAC inference, aggregate projection/budget order,
joined lookahead and SQL-incompatible year-zero dates. Fixes and permanent regressions
are present; independent final review PASS in M5_INDEPENDENT_REVIEW.md.

Acceptance: typed query AST, CRUD/custom fields, pagination and concurrency control
implemented and exercised against real PostgreSQL. Audit failure rolls back mutation.
Normal-role tenant/source/IR isolation, malformed values, SQL literals, policy denial,
reference/delete races and stale row versions covered. Request/query budgets and
whole-dataset visibility fail closed. Preview candidate ceiling1,000; page100,
projection32, filter depth3/nodes32 and exact-number bounds. Existing3-second runtime
transaction may reject expensive audited requests below those ceilings.

New production dependencies: none. SQL00008 is forward-only and adds preview datasets/
records, not future customer business tables. No plaintext credentials or seeds added.
Future production releases/authorization predicates, custom-field indexes, policy
masking, rate quotas, event outbox and production rollback remain later milestones.
Generated binaries, caches, browser artifacts, .env and identity keys remain ignored.

M5 STATUS: PASS. Independent review validates all seven root tests, fresh/repeatable
migrations and independent normal-role adversarial probes. Final cumulative evidence
will be appended after M6 verification.
