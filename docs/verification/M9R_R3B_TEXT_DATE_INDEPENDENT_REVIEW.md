# Independent review — R3b text and civil-date projections

16 October 2026 (Asia/Calcutta). Reviewed stable implementation `b17dc25`
(core `91ca010`) and correction `283ac0b`, starting `06882b2`. The reviewer did not author production
code. Root paused source changes and all test workloads throughout this review.
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

## Executed evidence

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

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; formatted only the new review file. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReview' -v ./tests/integration` | Initial FAIL 5.580s: copy test's lock-observer assertion failed; actual lost-COMMIT and masked/join tests passed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewCopyOverlapsUpdateAndDelete$' -v ./tests/integration` | Observer-only intermediate FAIL 1.783s, then PASS 2.555s after correcting visibility assumptions. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)(Index\|Review))' -v ./tests/integration` | PASS 69.137s: 21 top-level old numeric and new scalar attacks plus four independent review tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewRawReservedIdentifierRejected$' -v ./tests/integration` | FAIL 1.330s: actual unsupported intrinsic-ID registration and consequent write rejection reproduced, with rollback. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReview(RawReservedIdentifierRejected\|RegistryGuardAndIncompatibleMigration)$' -v ./tests/integration` | PASS 3.232s on correction `283ac0b`: source admission, independent CHECK, readiness and incompatible-registry migration rehearsal. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)(Index\|Review)\|TestM1FreshDatabaseMigrations)' -v ./tests/integration` | FAIL81.492s:23/24 top-level tests passed, only indexed page equivalence returned503. All six independent probes and fresh migrations passed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=2 -run '^TestM9RScalar(IndexOrderingCheckpointsAndMaintenance\|ReviewOrderingWithSafeDriverDiagnostics)$' -v ./tests/integration` | FAIL93.662s: original oracle passed once/failed once; traced oracle failed once/passed once. A real page query hit SQLSTATE57014. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=2 -run '^TestM9RScalarReviewOrderingWithSafeDriverDiagnostics$' -v ./tests/integration` | FAIL41.672s: phase-specific READY date ASC page9 timeout reproduced, one repetition then passed. |
| `M9R_SCALAR_PLAN_DIAGNOSTICS=true node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewOrderingWithSafeDriverDiagnostics$' -v ./tests/integration` | PASS28.972s before adding deterministic source-visit assertion; actual plans exposed repeated source scans under generic/custom/JIT-on/off. This PASS was diagnostic evidence, not capacity acceptance. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewOrderingWithSafeDriverDiagnostics$' -v ./tests/integration` | FAIL 31.144s after adding cold-plan probe: bounded EXPLAIN timed out at page9, and actual page10 also timed out (SQLSTATE57014). The final assertion additionally rejects an unmeasurable/timed-out EXPLAIN rather than merely logging it. |
| `git diff --check` | PASS before handback. |
| `make test-scalar-performance` | NOT RUN by reviewer yet; postponed until the required forward correction to avoid certifying defective code. |
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

## Scope and verdict at handback

**CHANGES REQUIRED.** The MEDIUM intrinsic-ID defect is corrected and its
independent guard/recovery probes pass. The reproduced HIGH cold-stat source-scan
defect still requires a production query correction and independent verification.
Root receives seven review functions for the final regression; no source-kit,
production implementation or already-pushed migration was edited by the reviewer.

The capacity contract is 100k hot rows, nine other populated tenants, text/date
ASC/DESC first/second/position 50000 pages and ten HTTP clients, unchanged 750ms
p95, 2s statements and 500ms locks. Actual plans must distinguish scanned projection
entries from source-record probes; a passing deep page is not constant-time
pagination. This review has not yet executed that gate on corrected code.

This is a bounded preview remediation. It does not certify overflow/selective
policy capacity, real collation-library upgrade/reindex recovery, general search,
enum/reference/instant indexing, uniqueness, aggregate planning, production
activation, R7 operations or full M9R completion. Source kits, IR, existing
migrations and customer records were preserved. No new dependency or threshold
increase was introduced by this review.
