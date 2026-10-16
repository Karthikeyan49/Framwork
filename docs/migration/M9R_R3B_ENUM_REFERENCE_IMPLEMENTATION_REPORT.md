# R3b — Governed enum and reference indexes

16 October 2026 (Asia/Calcutta). **Bounded enum/reference slice STATUS: PASS.**
Independent review and all final root gates passed. Implementation `d5d5802`,
review tests and tested source `0c15297`.
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
| `make test-choice-performance` | PASS 62.042s: twelve cases/1200 measured requests, zero errors; worst p95 259.768ms, p99 282.625ms, throughput 52.10–126.77/s. Both 100k indexes copied/verified in 203 checkpoints (36.255s combined build), 926-byte average hot record. Actual requested indexes/202 source visits in every case. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RChoiceIndexRawMaintenanceAndReadinessDrift$' -v ./tests/integration` | PASS 2.759s, expanded explicit NULL-array witness rejection and effective driver/role validation. |
| `make check` | PASS: checksums/format/vet/module verification/frontend lint/typecheck. |
| `node scripts/with-env.mjs node .local/reports/r3b-enum-reference-legacy-probe.mjs` | PASS: retained pre-extension API binary starts and responds 200 healthy/ready after 49, graceful shutdown. Binary module reports f99537f with dirty documentation; SHA256 d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea. This is startup/readiness evidence, not a complete old-binary CRUD/rollback certification. |
| Independent executable review | PASS: 36 selected tests, 139.302s, and all 12 capacity cases, 80.608s; no high/medium finding identified. See independent evidence below. |
| Exact retained-artifact variables below + `GOFLAGS=-v make check test build` | PASS on implementation/tests `0c15297`: 206 integration tests (693.313s), zero integration skips/failures; 96 frontend tests; seven Node script tests; checksums, format/vet/modules/lint/typecheck; all eight Go binaries and frontend production build. |
| `make infra-up migrate migration-status` | PASS final: healthy dependencies, repeated application safe, all migrations 1–49 present. |
| `make test-e2e` | PASS 9/9, 17.7s. |
| `make test-signin-e2e` | PASS 7/7, 7.6s. |
| `make test-performance` | PASS 41.244s: numeric HTTP p95 111.036ms, p99 175.469ms, 133.86 requests/s, zero errors. |
| `make test-scalar-performance` | PASS 65.643s: retained text/date twelve cases/1200 requests, zero errors; maximum p95 313.131ms, p99 398.615ms, throughput 44.08–129.97/s; source visits 202 throughout; unchanged 904-byte fixture. |
| `make test-choice-performance` | PASS final 65.402s: twelve cases/1200 requests, zero errors; maximum p95 275.685ms, p99 341.959ms, throughput 47.62–122.73/s; source visits 202 throughout; 926-byte hot records; both 100k indexes copied/verified in 203 checkpoints each (38.737s build). |
| `make test-write-performance` | PASS 61.637s: 32 clients for 60 seconds, 11735 successful distinct-record writes, p95 197.414ms, p99 225.000ms, throughput 195.11/s, zero errors. |
| `make docs-verify` and `git diff --check` | PASS final: 84 V2 + 43 V3 source checksums, no whitespace defects. |

Executed cases include 1206 populated rows, independently captured pre-index
ASC/DESC/null/tie/cursor/join results, checkpoint replay, patch/clear/delete
maintenance, 128-emoji enum keys, source option witness faults, raw invalid enum/
UUID writes, private helper denial, owner/manager/tenant/field denial, actual
audit rollback, options/readiness drift and legacy catalog-reader compatibility.

## Independent evidence and compatibility

The separate [review](../verification/M9R_R3B_ENUM_REFERENCE_INDEPENDENT_REVIEW.md)
executed six additional groups against stable code and an exclusive database.
Actual successful-COMMIT reply loss for both types reconciles through GET; old
checkpoints do not duplicate copy effects. Real overlapping copy/PATCH/delete is
observed through blocking PIDs, releases within unchanged lock limits and retains
exact values/versions/deletions. Unicode/NFC/NFD, malformed/NULL options, private
helper denial, two populated raw tenants, worker grant drift and retained/current
generation masks, owner/share revocation and tombstones passed. The 36-test combined
run includes all eight older scalar-review functions, fresh migrations and the
unchanged cold late-date 202-source assertion.

The maximum enum fixture has 100 members, each 128 codepoints/512 bytes. Independent
1000-row copy 455.682ms, independent verification 353.428ms and mutation 177.607ms
passed ordinary 2s statement/500ms lock/3s transaction budgets. This is a bounded
maximum-contract test, not a ten-client 100k maximum-options capacity guarantee.

The independent retained-binary probe also executes enum/reference fallback
reads, PATCH maintaining both projection versions, CREATE/DELETE and descending
reads after 49, with API-only credentials and a generated public test JWKS. It
checks the same retained artifact SHA as the root startup probe. The test is
explicitly opt-in on clean CI machines; SKIP must not be reported as PASS. Root's
final full run enabled both exact variables and executed the historical probe
with zero integration skips:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3b-enum-reference-legacy-api \
M9R_LEGACY_API_SHA256=d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea \
GOFLAGS=-v make check test build
```

Independent reference capacity PASS 80.608s: PostgreSQL 18.6, verified 4 CPU/8GiB and
disabled swap, 100k hot records plus nine 10k-row tenants, 926-byte average hot row.
Both indexes copied and independently verified 100k rows in 203 checkpoints each
(54.180s build). Twelve ASC/DESC enum/reference cases at offsets 0/100/50000 used
three warmups plus 100 measurements with ten HTTP clients each. All 1200 requests
succeeded; worst p95 287.883ms, p99 320.959ms, throughput 46.57–134.06/s. Requested
partial indexes were selected, source visits stayed 202 and projection visits
were 101/201/50101 respectively. This does not promise constant-time deep traversal.
The independent report retains every case, exact command and the historical-artifact
conditions. No failing product test or timeout/budget increase was concealed.

## Files and dependency scope

- Migration49 and ADR041 add the typed registry options, private validators,
  four fixed indexes and forward management/build/trigger changes.
- `internal/runtime/data/scalar_index.go` and `query.go` add type/options witness
  comparison, readiness and two constant partial-index predicates. No new API
  route or frontend protocol is needed; existing index dispatch remains.
- Root `m9r_choice_index_test.go` and independent `m9r_choice_review_test.go`
  exercise the contracts. The shared scalar performance harness gains a choice
  mode while preserving the original text/date fixture and command.
- Makefile adds `make test-choice-performance`. README/agent rules, reports,
  finding ledger and plan track verified scope and deferred work.

No new major dependency, metadata/IR/compiler version or source-kit edit. Retained
checksums remain 84 V2 + 43 V3. Commit source/migrations/tests/docs; ignore secrets,
`.env`, keys, retained test binary, local run logs, dependencies and build outputs.

The initial enum negative in the older numeric route fixture became valid under
the shared API; it now retains intrinsic/missing rejection and the new fixture
executes an actually unsupported boolean field. Assertions were not weakened to
hide a product failure. No source kit was edited. Local logs/configuration,
credentials, dependencies and generated outputs remain ignored.

## Acceptance and Git disposition

- PASS: old metadata/IR, catalog readers and selected retained-binary CRUD;
  fresh/repeated migrations preserve records, indexes and earlier schemas.
- PASS: typed enum/reference/null/Unicode contracts and exact options/target/
  collation witnesses; fixed indexes and existing overflow behavior are preserved.
- PASS: live tenant/manager/field/record/canonical scopes and private worker/
  dispatcher boundaries, including populated raw credential probes.
- PASS: checkpoint replay, actual lost COMMIT, concurrent copy/update/delete,
  atomic audit rollback, grant/constraint drift and maximum enum bounds.
- PASS: full static/unit/integration/frontend/build/browser checks and unchanged
  numeric, text/date, enum/reference and write reference gates. No timeout,
  pool, resource, latency or source-visit threshold was raised.
- NOT RUN: real collation-library upgrade/reindex, complete historical-version
  rollback/UI/DR, broad selective/join or maximum-options concurrent capacity,
  external production deployment and remote CI. These remain explicit gates.

Publish all task source/tests/reports to `remediation/m9r-foundation` using normal
fast-forward Git history only. Main merge and deployment remain separate tasks.
No generated artifact or credential belongs in Git. The task completion records
the final commit and direct live-remote comparison. Overall R3b/M9R remains
**PARTIAL**; M10 stays paused. This feature PASS does not close the remaining
items listed below or the historical R7 failures.

## Recovery and remaining gates

Forward recovery only: retain source records, options and checkpoints. Unknown
POST outcomes require authenticated GET reconciliation before further builds.
Invalid witnesses/source values block readiness/build; never coerce, drop
constraints, delete evidence or broaden grants to recover. Retain additive schema
when reverting a compatible binary; incomplete indexes use the bounded fallback.
Quiesce writers before operator-managed platform DDL; this is not an online
production migration certification. Retained-binary selected CRUD is narrower
than a complete old-version rollback/DR drill. No source data is copied/deleted
by migration49, and no destructive cleanup or automated production cutover occurs.

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
