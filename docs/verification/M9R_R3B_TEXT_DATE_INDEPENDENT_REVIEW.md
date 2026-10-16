# Independent review — R3b text and civil-date projections

16 October 2026 (Asia/Calcutta). Reviewed stable implementation `b17dc25`
(core `91ca010`), starting `06882b2`. The reviewer did not author production
code. Root paused source changes and all test workloads throughout this review.
Only this report and `tests/integration/m9r_scalar_review_test.go` were added.

## Finding requiring correction

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

Required correction: additive forward migration48, never edit pushed47. Reject
the reserved identifier in source-derived admission and enforce an independent
registry constraint. Validate existing registry facts; an existing incompatible
witness must stop installation rather than delete evidence or data. Re-execute
the failing regression and the combined suite, then run representative capacity
on the corrected implementation. No product fix was written by the reviewer.

## Executed evidence

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; formatted only the new review file. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReview' -v ./tests/integration` | Initial FAIL 5.580s: copy test's lock-observer assertion failed; actual lost-COMMIT and masked/join tests passed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewCopyOverlapsUpdateAndDelete$' -v ./tests/integration` | Observer-only intermediate FAIL 1.783s, then PASS 2.555s after correcting visibility assumptions. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar)(Index\|Review))' -v ./tests/integration` | PASS 69.137s: 21 top-level old numeric and new scalar attacks plus four independent review tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RScalarReviewRawReservedIdentifierRejected$' -v ./tests/integration` | FAIL 1.330s: actual unsupported intrinsic-ID registration and consequent write rejection reproduced, with rollback. |
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

**CHANGES REQUIRED** until the reproduced database admission defect is fixed
and independently reverified. No other high/medium product finding identified
in this bounded review. Root has the stable five-function review file for final
regression. No background review workload remains running at handback.

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
