# Independent review — datetime and boolean projection indexes

16 October 2026 (UTC). Reviewed stable implementation checkpoint `a3301e6`,
based on published `d4e4e4c`. Root paused source edits and database workloads
throughout the independent execution window. The reviewer added only
`tests/integration/m9r_native_review_test.go` and this report. No production
implementation, applied migration or source kit was edited by the reviewer.

## Verdict and scope

**PASS for the bounded datetime/boolean projection contract.** No high/medium
product finding identified in the inspected changes and executed probes.
Forty-seven combined integration tests and all twelve representative capacity
cases passed, with no skipped historical-artifact tests in this execution.
Root owns final whole-project checks, builds, browser checks and retained
numeric/text/date/enum/reference/write capacity gates before delivery.

The design review specifically challenged unrounded UTC bounds, PostgreSQL
microsecond rounding, year-10000 physical carry, negative-epoch fractions, raw
cursor preservation and strict JSON booleans. The implementation retains the
existing SQL cast as its ordering oracle; it does not claim nanosecond SQL
ordering. The provisional question about undeclared performance-fixture fields
was resolved by inspecting the base contacts metadata: both parent and joined
fields are declared. It is not a product finding.

The threat model distinguishes trusted HTTP request policy from raw tenant and
worker database boundaries. A negative tenant-GUC probe does not establish that
GUCs are credentials. Schema-owner/operator corruption is outside runtime
isolation; selected operator faults verify fail-closed detection and restoration,
not confinement of those credentials. Disposable probes verify effective driver
targets and connected database/roles. New tests print no secrets, tokens, DSNs,
SQL arguments, raw driver errors or customer payloads.

## Independently executed attacks and faults

- A SQL oracle orders populated pre-1970, epoch, year-1 and year-9999 records.
  Reverse UUIDs prevent insertion order from concealing microsecond ties.
  Fractions immediately below/at/above rounding ties and day/year carries are
  paged one record at a time, ASC and DESC, with nulls. Indexed results equal
  fallback SQL and preserve original JSON values/cursors, including a physical
  year-10000 key. Cast-compatible positive/negative 15:59 offsets are included.
- Raw conversion rejects whitespace/newlines, lowercase/local/relative forms,
  malformed offsets, excess fractions, impossible/leap dates, year zero,
  out-of-range unrounded UTC instants, incompatible PostgreSQL raw offsets and
  wrong JSON types. SQL NULL contracts, JSON null options, truthy strings/numbers
  and required-null booleans fail with typed errors. Root's executed groups
  additionally preserve API normalization of valid extreme ±23:59 offsets and
  boundary inputs accepted by retained binaries.
- Boolean false/true/null, equality/inequality, IN/NOT IN and null filtering
  retain exact results before/after indexes. Unsupported boolean ranges and
  truthy filter values fail. Timestamp equality uses the same rounded SQL key.
  Caller timezone and DateStyle changes do not alter absolute conversion.
  Populated string/date/native rows reject mixed physical columns.
- Schema/type/required/collation witness faults independently return HTTP 503;
  restoring witnesses restores valid queries. Populated tenants stay isolated
  in raw registry/projection reads, and changing only the tenant GUC exposes
  zero records. App, worker and dispatcher cannot execute the private converter.
  A worker column-grant fault is detected while restrictive RLS still returns
  zero rows; revocation restores grant safety. Root's executed native tests also
  verify helper grant/timezone readiness drift and audit rollback.
- The wire proxy actually drops a successful PostgreSQL COMMIT response for
  each native type. HTTP 503 and observed reply loss are reconciled through
  authenticated checkpoint GET; replaying the old checkpoint does not repeat
  committed copy effects, and independent verification reaches READY.
- COPY overlaps valid PATCH and DELETE for both types. Blocking-PID observations
  prove both writers wait behind source locks before release within existing
  budgets. Both mutations succeed; verification finds exact typed values and
  source versions, and no deleted projection is resurrected.
- Masked, filtered, joined cursor pages match before/after indexing. Current and
  retired generations use live canonical owner/share changes, revocation and
  tombstones before LIMIT. Retained scalar review also checks certified snapshot
  requirements, overflow fallback, old intrinsic-ID rejection and cold plans.

## Retained executable compatibility

The opt-in artifact `.local/reports/r3b-enum-reference-legacy-api` was verified
against SHA256
`d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea`.
Root identifies its build metadata as `f99537f` plus documentation changes.
Actual native compatibility checks execute startup/readiness, old fallback reads
against new indexes, PATCH of year-9999 nanosecond/false values maintaining both
projection versions, CREATE/DELETE, post-write DESC reads and graceful exit after
migration 50. The process receives only API bindings and a public test JWKS;
operator/worker credentials are absent.

Both native and retained choice artifact tests executed in the combined run.
Without the supplied artifact, a clean checkout explicitly reports SKIP. This
selected CRUD evidence does not certify every historical binary/UI operation,
schema downgrade, complete deployment rollback or backup/DR procedure.

## Commands and outcomes

Artifact variables used on each opt-in test invocation:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3b-enum-reference-legacy-api M9R_LEGACY_API_SHA256=d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea
```

| Exact command (prefix the two opt-in test commands with the variables above) | Result |
| --- | --- |
| `.local/toolchains/go/bin/gofmt -w tests/integration/m9r_native_review_test.go` | PASS; only the owned review file formatted. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RNativeReview' -v ./tests/integration` | Initial attempt FAIL at compilation because the review file contained an unused copied `pgconn` import; no database test executed. Removed that import. Rerun PASS 28.367s for the initial six independent groups, including retained native API CRUD. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar\|Choice\|Native)(Index\|Review)\|TestM1FreshDatabaseMigrations)' -v ./tests/integration` | PASS 212.394s: 47 top-level tests, zero failures/skips, all seven expanded independent native groups, retained numeric/scalar/choice groups, old-binary CRUD and fresh/repeated migrations 1–50. Cold scalar source visits remain 202 in tested planner modes. |
| `make test-native-performance` | PASS 69.968s, all twelve 100k-row cases, 1200 measured requests, zero errors. |
| `git diff --no-index --check /dev/null tests/integration/m9r_native_review_test.go` and corresponding command for this report | PASS whitespace validation: no diagnostics. Each returns the expected no-index difference status 1 because the file is new. |
| Final whole-project checks/build/browser and retained capacity gates | NOT RUN by reviewer in this window; root owns these final gates after handback. |

Ignored logs are `.local/reports/r3b-native-independent-initial.log` (compile
failure), `r3b-native-independent-focused.log`, `r3b-native-independent-combined.log`
and `r3b-native-independent-capacity.log`. No failing product attack is omitted;
the sole initial failure was in review-file compilation and was corrected before
the successful executions. No product fix was required by this review.

## Measured reference envelope

The gate verified PostgreSQL 18.6 on four CPUs, 8GiB memory and disabled swap.
Hot tenant: 100000 rows; nine other tenants: 10000 each. Average hot record:
1013 bytes. Each index copied and independently verified all 100000 rows in 203
checkpoints; combined build took 41.896908 seconds. Each case has three warmups,
100 measured requests and ten HTTP clients. Existing 2s statements, 500ms locks,
source-visit bound 302 and HTTP p95 ceiling 750ms remain unchanged.

| Sort | Offset | p95 ms | p99 ms | requests/s |
| --- | ---: | ---: | ---: | ---: |
| Datetime ASC | 0 | 151.959 | 192.857 | 111.46 |
| Datetime ASC | 100 | 126.286 | 177.888 | 110.93 |
| Datetime ASC | 50000 | 331.625 | 386.290 | 49.25 |
| Datetime DESC | 0 | 113.205 | 151.797 | 116.27 |
| Datetime DESC | 100 | 131.708 | 185.160 | 110.88 |
| Datetime DESC | 50000 | 229.274 | 248.339 | 53.08 |
| Boolean ASC | 0 | 116.581 | 140.284 | 113.12 |
| Boolean ASC | 100 | 140.381 | 160.025 | 108.34 |
| Boolean ASC | 50000 | 218.232 | 257.180 | 63.79 |
| Boolean DESC | 0 | 106.816 | 201.700 | 118.43 |
| Boolean DESC | 100 | 115.797 | 133.202 | 123.49 |
| Boolean DESC | 50000 | 208.091 | 246.820 | 68.42 |

Actual API SQL used each requested datetime/boolean ASC/DESC index. Every case
visited 202 source records. Projection visits were 101/201/50101 at offsets
0/100/50000. Deep cursor traversal remains linear in skipped projection entries;
this is bounded source-fetch evidence, not constant-time traversal or a claim
for every selective-policy/join/mutation workload.

## Remaining boundaries and recovery

Search indexes, uniqueness, expanded SQL aggregates, broader workload capacity,
role editor UI, external identity certification, R6 verification authority, R7
operations and production active-release execution remain separate work. A real
database collation-library upgrade/reindex, backup/DR drill and arbitrary old
deployment rollback were not executed in this review. Preserve migrations 1–50,
source records and all immutable/checkpoint evidence; recover with reviewed
forward repair rather than destructive down migrations or weaker grants.

At handback, review test/API processes have exited. Only the two owned review
files are uncommitted for root to inspect and commit. Generated logs and the
historical executable remain ignored. Overall M9R remains PARTIAL; no M10 or
main merge is authorized by this review.
