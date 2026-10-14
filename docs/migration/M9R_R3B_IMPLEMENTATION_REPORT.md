# R3b governed numeric indexing

14 October 2026 (IST). Status: implementation under verification; independent
acceptance pending. This is a numeric-index subset, not full R3 or foundation PASS.

## Implemented boundary

Fixed shared numeric registry/projection tables retain JSONB as primary storage.
Tenant metadata cannot create tables, columns, expressions or SQL. Owner/admin
management derives integer/decimal/money fields from immutable compiled source;
PostgreSQL independently checks live membership, source fields and active binding.
Every key/FK includes tenant and physical namespace. Compatible preview bindings
retain their anchor/index; unsupported fields and mismatched schema witnesses deny.

Database-owned triggers maintain exact numeric values and source row versions
atomically with record writes; deletion cascades without resurrection. Application
roles have SELECT only on derived tables, never direct DML. Workers have neither
generic grants nor permissive policies; new tables join worker grant-drift checks.
Readiness verifies ownership/FORCE RLS, enabled triggers and required read ports.

Registration advances the existing anchor epoch under an exclusive gate, fencing
older RR writers. Copy and verification process at most1000 rows per checkpoint.
BUILDING/VERIFYING indexes use the existing query plan; only READY indexes enter
ordered page selection. Entity/field/row DataScope, filters and cursors remain
before LIMIT; JSONB records are fetched in the same statement snapshot. Nulls are
last in both directions, with UUID ties ascending.

## API and recovery

Under either `/versions/{revision}` or `/datasets/{dataset}/bindings/{binding}`,
the entity path has `/indexes/{field}` GET, `/register` POST `{}`, and `/build`
POST `{"expected_checkpoint":N}`. These are real authenticated preview routes,
not a production release API or a new UI. GET returns state/checkpoint/counts and
schema identity, never record values.

After an unknown response, GET the checkpoint before repeating a build. Repeating
the same expected checkpoint returns committed progress without advancing twice.
Business data, management changes and audit failure roll back together. Copy
uses shared source locks; a newer update/delete aborts a stale builder rather than
being overwritten. Forward-only migrations preserve all source records. Keep
BUILDING/VERIFYING on a failed witness/coverage check; do not declare it READY or
delete records/checkpoints to retry. Schema-owner corruption requires reviewed
operator repair. There is no automatic destructive down migration.

## Executed commands and retained failures

| Exact command | Outcome |
|---|---|
| `./scripts/go.sh fmt ./internal/runtime/data ./internal/platform/access ./internal/policy` | PASS |
| `./scripts/go.sh test ./internal/runtime/data ./internal/platform/access ./internal/policy` | PASS |
| `make migrate migration-status` | PASS: migration31 installed;1–31 applied |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RNumericIndex' -v ./tests/integration` | Initial four roots PASS11.566s; expanded corrected33 suite PASS16.645s; audit/grant-drift expansion PASS17.425s. Later joined-batch result pending below |
| `make test-performance > /tmp/m9r-r3b-performance.log 2>&1` | FAIL29.566s:100k build completed8.379s and indexed101-row plan executed1.316ms; plan visitor skipped typed root slice. HTTP samples NOT RUN. Corrected visitor, no product defect claimed |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RNumericIndexBuildConcurrentWriter$' -v ./tests/integration` | Initial fixture FAIL/terminated87.729s: used restricted pg_stat_activity wait fields and had incorrect cleanup ordering. Fixed owned-backend pg_locks observation/cleanup. Then FAIL2.119s reproduced actual40P01 build registry/FK deadlock. Migration32 correction PASS2.062s; final update/delete subcases pass |
| `make test-performance > /tmp/m9r-r3b-performance-fixed.log 2>&1` | FAIL30.653s: final full coverage scan exceeded unchanged2s statement budget; HTTP NOT RUN. Migration33 replaced it with checkpoints |
| `make migrate` (initial33) | FAIL42601: generated regex replacement duplicated SQL suffix. Atomic rollback retained applied1–32. Corrected the still-unapplied33; subsequent migration PASS and repeated no-op PASS |
| Numeric suite before corrected33 | FAIL4.613s, all fixtures rejected migration33 syntax. Retained; no accepted functionality claim from these failures |
| `make test-performance > /tmp/m9r-r3b-performance-bounded.log 2>&1` | FAIL30.142s: separate cached projection lookups exceeded2s without fresh stats. Migration34 joins the bounded batch once. HTTP NOT RUN |
| `make migrate` (32,33,34 individually) | PASS: additive changes, no record cleanup; repeated33 invocation applied0 |

The migration34 correction passed all eight numeric race-test roots18.848s,
including update/delete subcases, derived-table drift and audit rollback.
`make test-performance > /tmp/m9r-r3b-performance-joined.log 2>&1` PASSED34.066s:
100k rows copied and independently scanned for coverage in203 checkpoints,
10.492s build time. Unindexed SQL p95 key0.424ms/numeric83.874ms/selective58.776ms
passed. Numeric HTTP ten-client p50/p95/p99 was56.898/104.506/116.930ms,
154.00requests/s and zero errors; the unchanged750ms p95 gate passed. The actual
generic runtime plan uses the numeric ascending index for101 rows and two sets
of101 source primary-key probes. All100 response samples validate exact expected
IDs, values, ordering and cursor. PostgreSQL18.6/4CPU/8GiB/swap-off image digest
`29754c7520f4b9654cf8ff98e19ac73b88e255c3a53f6236ebe9d52249bb6df3` was verified.

Full checks/write workload/browser and independent review are still pending;
root workload success alone is not independent acceptance.
Earlier current-host R3 HTTP FAIL898.985ms remains historical evidence.
No threshold, client count, dataset width, pool size or resource limit was raised.
The load fixture explicitly builds through the authenticated API, preserves the
unindexed SQL baseline, and asserts the indexed plan visits authorized101 rows.

## Remaining scope

Text/date/search projections, uniqueness metadata/concurrent uniqueness, SQL
aggregates, general join planning and selective-policy performance guarantees
remain separate R3 subgates. Incompatible schema generations/canonical live ACLs
and workflow data pins remain full R2 work. Product custom roles, independent
acceptance authority, rate limiting/operations, production activation and external
tools are not delivered by this change. No major dependency was introduced.

Commit migrations, Go/API code, tests and reports. Keep `.env`, credentials,
dependencies, local logs/profiles, binaries and frontend build outputs ignored.
