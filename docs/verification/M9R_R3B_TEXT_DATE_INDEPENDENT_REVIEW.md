# Independent review — R3b text and civil-date projections

16 October 2026 (Asia/Calcutta). Reviewed stable implementation `b17dc25`
(core `91ca010`) and corrections `283ac0b` / `2823a53`, starting `06882b2`.
The reviewer did not author production code. Root paused source changes and
all test workloads during each independent executable review window.
Only this report and `tests/integration/m9r_scalar_review_test.go` were added.

## Finding and forward correction

**MEDIUM — the database scalar registration port admits reserved intrinsic IDs.**
Go `scalarField` rejects `CoreID`, but migration47's
`runtime.register_scalar_index` admits the stored required-string definition
`field.contact.id`. The projection trigger reads `custom_data`, where the
intrinsic identifier is intentionally absent. A successful unsupported
registration therefore makes a subsequent otherwise-valid record update fail
with SQLSTATE 22023. Public Go routes already reject this input; this is an
independent database admission/invariant defect, not a demonstrated tenant leak.

Executed `TestM9RScalarReviewRawReservedIdentifierRejected` as the actual app
role with live owner identity, matching source/schema witnesses and populated
records. It observed registration success, then a valid update rejection. The
test deliberately rolled the transaction back, including the registration;
no poisoned registration or customer record change was committed. Failure:
`raw scalar port admitted intrinsic ID; subsequent valid write rejected=true;
fixture rolled back=true`.

Root implemented additive migration48 in `283ac0b`, preserving pushed47.
Source-derived admission rejects the reserved identifier, independently validated
registry CHECK rejects incompatible facts, and readiness requires the validated
guard. CREATE OR REPLACE preserves routine ownership/ACLs.

Independent correction probes PASS 3.232s. The exact former attack now fails
with SQLSTATE 22023, and direct maintenance insertion independently fails 23514.
In an owned disposable database, removing or leaving the guard unvalidated makes
readiness reject; validation restores readiness. A transaction then removed the
guard, staged an incompatible registry fact and executed the actual embedded
migration48 UP SQL. Validation failed 23514; rollback retained the original valid
guard and data and committed no poisoned evidence. The effective database/roles
were checked before these probes. No product fix was written by the reviewer.

**HIGH — cold-stat indexed cursor plans repeatedly scan the source namespace.**
Corrected `283ac0b` passed the intrinsic-ID probes, but its combined regression
returned 503 during the 1206-row indexed ordering oracle. This was reproduced,
not classified as the historical unconfirmed bad-connection issue. A pgx tracer
below the RuntimeSnapshot port recorded SQLSTATE 57014 after 2005.763ms on the
READY civil-date ASC page at ordinal 9. The database held exactly 1206 source rows
and 2412 projections, verified in its owned disposable database; historical
performance fixtures did not contribute.

Actual bounded EXPLAIN replay chose `scalar_date_asc`, but the inner records
scan returned about 1202.64 rows over 103 loops (~123872 source visits), and the
outer page fetch returned 1206 rows over 101 loops (121806 visits). Generation
lookups repeated at the same scale. Generic/custom plans shared this shape.
JIT-on generic/custom execution was 414.907/378.843ms in one replay; JIT-off
was 370.934ms, with no reported JIT compilation. This does not justify disabling
JIT or changing the 2s timeout. Other repetitions actually exceeded that timeout.

The new diagnostic regression retains SQL/bindings only in fixture memory,
prints static stages/SQLSTATE/timing/structural node counts only, and enforces the
existing maximum 302 source-record visits for a 101-row selected page. It does
not require constant-time projection traversal. The production query must bound
both projection-to-authorized-record and selected-page-to-record fetches while
preserving live canonical ACL, every scope/filter/cursor before LIMIT and source
version joins. Fixture-only ANALYZE, larger budgets or passing reruns are not fixes.
The first diagnostic label classified every selected-page CTE as indexed; that
label was corrected before the phase-specific evidence above was obtained.

Root corrected the query in `2823a53`, after documenting the decision in ADR040.
Projection-driven LATERAL namespace/ID/version lookups retain the security-invoker
record view and use an OFFSET 0 planning barrier. Cursor/tie ID uses the proved
equal projection ID. The selected-page fetch also uses a bounded correlated
lookup. Every authorization/filter predicate remains before LIMIT; no raw-record
substitution, JIT change or timeout increase occurred.

Independent combined regression PASS 81.587s on that correction: 26 top-level
tests, including old numeric/new scalar cases, all eight review tests and fresh
migrations. The actual cold generic plan now visits 202 source rows (101 candidates
and 101 selected records), below the unchanged 302 bound, at 3.229ms SQL execution.
The additional indexed retained/current-generation regression PASS 6.63s proves
owner scope precedes LIMIT and live owner/share revocation plus tombstones apply
through both scalar indexes. Index existence never substitutes for canonical ACL.

## Executed evidence

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; formatted only the new review file. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReview' -v ./tests/integration` | Initial FAIL 5.580s: copy test's lock-observer assertion failed; actual lost-COMMIT and masked/join tests passed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewCopyOverlapsUpdateAndDelete$' -v ./tests/integration` | Observer-only intermediate FAIL 1.783s, then PASS 2.555s after correcting visibility assumptions. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)(Index\|Review))' -v ./tests/integration` | PASS 69.137s: 21 top-level old numeric and new scalar attacks plus four independent review tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewRawReservedIdentifierRejected$' -v ./tests/integration` | FAIL 1.330s: actual unsupported intrinsic-ID registration and consequent write rejection reproduced, with rollback. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReview(RawReservedIdentifierRejected\|RegistryGuardAndIncompatibleMigration)$' -v ./tests/integration` | PASS 3.232s on correction `283ac0b`: source admission, independent CHECK, readiness and incompatible-registry migration rehearsal. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)(Index\|Review)\|TestM1FreshDatabaseMigrations)' -v ./tests/integration` | FAIL 81.492s: 23/24 top-level tests passed, only indexed page equivalence returned 503. All six independent probes and fresh migrations passed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=2 -run '^TestM9RScalar(IndexOrderingCheckpointsAndMaintenance\|ReviewOrderingWithSafeDriverDiagnostics)$' -v ./tests/integration` | FAIL 93.662s: original oracle passed once/failed once; traced oracle failed once/passed once. A real page query hit SQLSTATE 57014. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=2 -run '^TestM9RScalarReviewOrderingWithSafeDriverDiagnostics$' -v ./tests/integration` | FAIL 41.672s: phase-specific READY date ASC page 9 timeout reproduced, one repetition then passed. |
| `M9R_SCALAR_PLAN_DIAGNOSTICS=true node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewOrderingWithSafeDriverDiagnostics$' -v ./tests/integration` | PASS 28.972s before adding deterministic source-visit assertion; actual plans exposed repeated source scans under generic/custom/JIT-on/off. This PASS was diagnostic evidence, not capacity acceptance. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewOrderingWithSafeDriverDiagnostics$' -v ./tests/integration` | FAIL 31.144s after adding cold-plan probe: bounded EXPLAIN timed out at page 9, and actual page 10 also timed out (SQLSTATE 57014). The final assertion additionally rejects an unmeasurable/timed-out EXPLAIN rather than merely logging it. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)(Index\|Review)\|TestM1FreshDatabaseMigrations)' -v ./tests/integration` | PASS 81.587s on query correction `2823a53`: 26 top-level tests, including eight independent probes, actual cold source-visit bound and indexed canonical-ACL generations. |
| `git diff --check` | PASS before handback. |
| `make test-scalar-performance` | PASS 86.287s on `2823a53`: all twelve 100k-row/ten-client/100-sample cases, zero errors, unchanged p95 threshold. Previously postponed until both corrections. |
| Whole check/test/build, migrations and browser regression | NOT RUN by reviewer; root owns the final aggregate after corrections. |

The initial/intermediate copy-observer failures were review-fixture failures,
not product pre-fix evidence. The least-privilege migrator cannot see another
role's `pg_stat_activity.wait_event_type`. The corrected observer uses the
public blocking-PID function; it then proved both actual record writers blocked
behind retained source locks. Releasing within the unchanged 500ms lock budget
let both writers succeed. Verification found current source/projection versions,
the new overflow marker and no projection resurrection after delete.

The actual lost-COMMIT test uses a local wire proxy that discards PostgreSQL's
successful COMMIT response. The API returned 503, the independent authenticated
GET observed committed copy/checkpoint evidence, and repeating the old expected
checkpoint did not perform another copy. This is executed fault evidence,
not a simulated return code or only a replay label.

The new masked-query test used populated parent/child rows and a live tenant
role denying name/balance. Text/date sort, filters, joined masking and multiple
cursor pages matched before/after index installation; hidden indexed sort/filter
requests and a direct non-manager SQL registration were denied. A real separate
autocommit pool defaulting to REPEATABLE READ still fell back, retained overflow
rows and could not obtain the certified RuntimeSnapshot proof. Four-byte Unicode
512/516/4096/4100-byte boundaries and Gregorian leap-century/year 1 cases executed.
Each targeted driver connection checked effective database and role before probes.

Existing executed scalar/numeric cases additionally cover combined quotas/races,
stale-RR fencing, missing-projection verification rejection, transactional audit
faults, two populated tenants, worker generic/column grants, trigger/routine drift,
NULL/stale collation witnesses, long escaped cursors, adoption and live row scope.

## Representative executed capacity

PostgreSQL 18.6 reference container verified four CPUs, 8GiB memory and disabled
swap. Ten populated tenants: 100000 hot rows and nine tenants with 10000 rows
each; average hot record 904 bytes. Both indexes copied and independently verified
all 100000 rows in 203 checkpoints each. Build elapsed 56963.194ms. Every measured
case used three warmups, 100 samples and ten HTTP clients. No policy, statement,
lock, pool, latency or source-visit budget was increased.

| Sort | Offset | p95 ms | p99 ms | requests/s |
| --- | ---: | ---: | ---: | ---: |
| Text ASC | 0 | 121.710 | 131.485 | 118.46 |
| Text ASC | 100 | 112.068 | 161.280 | 122.98 |
| Text ASC | 50000 | 267.701 | 304.814 | 50.60 |
| Text DESC | 0 | 132.041 | 151.656 | 122.59 |
| Text DESC | 100 | 126.043 | 141.486 | 117.77 |
| Text DESC | 50000 | 271.638 | 317.621 | 47.99 |
| Date ASC | 0 | 114.336 | 138.156 | 123.19 |
| Date ASC | 100 | 141.023 | 184.973 | 107.63 |
| Date ASC | 50000 | 220.902 | 240.615 | 61.51 |
| Date DESC | 0 | 99.052 | 151.334 | 126.62 |
| Date DESC | 100 | 132.447 | 151.751 | 116.20 |
| Date DESC | 50000 | 248.678 | 270.236 | 55.50 |

All 1200 measured requests succeeded. The largest p95 was 271.638ms against 750ms;
largest p99 was 317.621ms. Actual API SQL selected the requested ascending/descending
text/date index. Each plan visited 202 source records, while projection entries
visited were 101/201/50101 for offsets 0/100/50000. The middle page still traverses
projection entries before its cursor; this is explicitly not constant-time
pagination. SQL, bound values, complete plans, credentials and tokens were never
printed by the diagnostic additions.

## Scope and verdict at handback

**PASS for the bounded text/civil-date projection contract.** Both findings are
corrected and independently reverified; cold-plan and representative warm-capacity
gates pass. No remaining high/medium finding identified in this review scope.
Root receives eight review functions for the final aggregate; no source-kit,
production implementation or already-pushed migration was edited by the reviewer.
No reviewer workload remains running at handback.

The capacity contract is 100k hot rows, nine other populated tenants, text/date
ASC/DESC first/second/position 50000 pages and ten HTTP clients, unchanged 750ms
p95, 2s statements and 500ms locks. Actual plans must distinguish scanned projection
entries from source-record probes; a passing deep page is not constant-time
pagination. That gate was executed on the corrected code as recorded above.

This is a bounded preview remediation. It does not certify overflow/selective
policy capacity, real collation-library upgrade/reindex recovery, general search,
enum/reference/instant indexing, uniqueness, aggregate planning, production
activation, R7 operations or full M9R completion. Source kits, IR, existing
migrations and customer records were preserved. No new dependency or threshold
increase was introduced by this review.
