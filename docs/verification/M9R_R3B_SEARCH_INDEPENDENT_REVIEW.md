# Independent review — governed full-text search

16 October 2026 (Asia/Calcutta). Stable implementation checkpoint `d61f696`,
following the published datetime/boolean milestone. Root paused implementation
edits and database workloads during the review. The reviewer owns only
`tests/integration/m9r_search_review_test.go` and this report; no implementation,
applied migration, source kit, customer data or Git history was changed.

## Scope and threat model

The bounded contract is explicit, single-string-field PostgreSQL simple/plain
AND search, default ascending ID pages, existing filters/joins, no ranking,
substring reinterpretation, caller tsquery/configuration or shared DDL. Search
uses its own governed registry/projections with the existing combined quotas,
source versions, verification and canonical access views. This does not complete
all R3/R5/R6/R7 work or establish production readiness.

The additional restrictive match policy is a query optimization, not an identity
or authorization credential. Existing tenant/live membership FORCE RLS remains
independent. Trusted Go supplies policy/DataScope before LIMIT and masked output;
raw app-role RLS does not replace field policy. Worker/dispatcher retain no
ambient projection/private-helper access. Operator corruption is outside runtime
confinement; selected committed faults check fail-closed detection/restoration.
Disposable fixtures verify effective driver targets and connected database/roles.
No secrets, SQL arguments, DSNs, tokens, raw driver errors or customer payloads
are printed by the new tests.

## Independently executed probes

- Actual successful COMMIT response is dropped by the PostgreSQL wire proxy.
  HTTP503 and observed reply loss reconcile through authenticated checkpoint GET;
  replay of the old checkpoint repeats no committed copy, and verification reaches
  READY. This is observed transport failure rather than a mock error return.
- COPY and valid PATCH/DELETE overlap. Two blocked app backends are observed with
  `pg_blocking_pids` before releasing source locks inside the unchanged lock
  budget. Mutations succeed; verification proves exact current vectors/source
  versions and no resurrected deleted projection.
- Masked, filtered, joined one-row cursor pages agree before/after READY, with
  exact parent identities, masks and null targets. An additional populated case
  checks the returned parent name/ID, normal no-parent null and live target
  ownership DataScope denial, while keeping matching source records visible.
- Current and retained generations enforce current owners, shares, revocation
  and tombstones before pagination. Neither a retained index nor an old record
  owner resurrects revoked/deleted access.
- Populated raw tenant reads with empty, valid, nonmatching, oversized and
  punctuation-only query context remain restricted; forged tenant/query GUCs do
  not grant another tenant membership. App/worker/dispatcher cannot run the
  private converter; worker/dispatcher cannot register indexes or read parser
  helper state. A worker projection-column grant fault is detected while forced
  restrictive RLS still returns no populated vectors; revocation restores safety.
- Committed schema/parser/required witness faults reject search. A changed parser
  witness also rejects mutation. A weakened match policy fails readiness; exact
  restoration restores readiness. No actual PostgreSQL/library upgrade is claimed.
- Search versus scalar registration races for the last combined entity slot;
  exactly one wins and the total remains eight. Existing registrations reconcile.
- An opaque executor uses raw fallback with identical results/cursors. Strict point
  search checks the unprojected input using trusted record/owner headers; worker
  inputs reject interactive search. Hidden unprojected search is denied. Exact
  search-text changes invalidate cursors, and sequential valid/invalid queries
  leave no transaction search text.
- Incorrect stored vectors fail independent verification without advancing state
  or checkpoint. Restoring vectors from the unchanged source permits verification
  and complete results. This tests verification failure, not runtime corruption
  confinement of operator credentials.

Retained scalar/choice/native reviews additionally cover SQL ordering, overflow,
Unicode, source-version races, historical native precision, raw ports and live ACL.
Independent R5d and declared-join/page suites exercise the shared target-join change.

## Retained executable compatibility

Opt-in artifact `.local/reports/r3b-enum-reference-legacy-api` is pinned to SHA256
`d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea`.
The process receives only API bindings and public fixture JWKS, never operator or
worker credentials. It executes real startup/readiness, populated old reads,
PATCH maintaining search vector/version, CREATE/DELETE and graceful exit after
migration 51. A populated cursor produced by the older API with Search absent
is consumed by the new API with exactly the same next page. This verifies absent
Search serialization compatibility, not every historical deployment/UI or DR path.
Without the opt-in artifact a clean checkout reports SKIP explicitly.

## Failures and correction history

Before this stable review, root capacity runs reproduced the declared joined
search failure as HTTP503/SQLSTATE57014 under the original 2s timeout. Read-only
review identified the unfenced canonical target-view join. Root fenced target
lookups with correlated LATERAL/OFFSET0, retaining namespace, target DataScope,
masking and LEFT JOIN nulls. Root's post-fix eight-case capacity gate passed.
The independent probes above and capacity below qualify that stable fix; budgets
were not raised and forced RLS was never disabled.

Review-only initial failures are retained in ignored logs:

1. `r3b-search-independent-first.log`: unused copied `database/sql` import caused
   compilation FAIL; no database test executed. Removed import.
2. `r3b-search-independent-current.log`: new target-ownership fixture used row
   version1 after a name PATCH advanced it to2; access CAS correctly returned 409.
   Corrected fixture expectation to2; combined suite then passed.
3. `r3b-search-independent-race.log`: newly added corruption test expected503;
   documented `index_conflict` mapping correctly returned 409 for SQLSTATE55000.
   The bad vectors were rejected. Corrected the assertion while preserving the
   unchanged checkpoint/state and successful repair requirements. This first
   race attempt also explicitly SKIPPED the optional retained artifact because
   its opt-in variables were absent. Final artifact-enabled rerun executes it.

These are test compilation/setup/assertion defects, not product regressions.
No high/medium product finding has been identified in the inspected stable change.

## Commands and results

All commands ran in `/workspace/Framwork`. Artifact-enabled commands use:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3b-enum-reference-legacy-api M9R_LEGACY_API_SHA256=d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea
```

| Exact command (prefix artifact-enabled rows with the variables above) | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; only owned new review file changed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -timeout=5m -count=1 -run '^TestM9RSearchReview' -v ./tests/integration` | Initial compile FAIL as recorded; artifact-enabled seven-group rerun PASS 7.925s. Expanded eighth-group run hit fixture CAS error above. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -timeout=10m -count=1 -run '^(TestM9RSearch\|TestM9RField\|TestIndependentR5d\|TestM5DeclaredJoin\|TestM9RCustomSortPage\|TestM9RScalarReview\|TestM9RChoiceReview\|TestM9RNativeReview)' -v ./tests/integration` | Artifact-enabled PASS 67.600s:55 top-level tests, zero failures/skips, including all eight initial independent groups and retained executable tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -timeout=5m -count=1 -run '^TestM1FreshDatabaseMigrations$' -v ./tests/integration` | PASS 0.687s; fresh/repeated migrations 1–51 and owner-boundary negatives. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RSearchReview' -v ./tests/integration` | Initial FAIL due overly narrow expected error above; final artifact-enabled PASS 23.497s, nine groups, zero failures/skips. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RSearchReviewVerificationRejectsCoverageDrift$' -v ./tests/integration` | PASS 2.642s after pinning exact documented 409 rejection in the final assertion. |
| `make test-search-performance` | PASS 34.174s, all eight cases/800 measured requests, zero errors. |
| `git diff --no-index --check /dev/null tests/integration/m9r_search_review_test.go` and corresponding report command | PASS whitespace validation, no diagnostics; expected no-index difference status 1 for each new file. |
| Whole-project check/test/build, browser and five retained performance gates | NOT RUN by reviewer in this window; root owns final gates after handback. |

## Independent reference capacity

Verified PostgreSQL 18.6 with four CPUs, 8GiB and disabled swap; hot tenant 100000
records plus nine tenants 10000 each. Mean hot record 1002 bytes. The search index
copied and independently verified 100000 records in 203 checkpoints; build 11.849s.
Each case ran three warmups, 100 measured requests and ten HTTP clients. The 750ms
HTTP p95, 2s statement and 500ms lock limits were unchanged. Every captured actual
app query used GIN; exact source-ID/cursor oracles passed.

| Case | Matches/eligible | Source visits | p95 ms | p99 ms | Requests/s |
| --- | ---: | ---: | ---: | ---: | ---: |
| Rare first 50 | 100/100 | 151 | 126.166 | 129.485 | 125.49 |
| Rare next 50 | 100/100 | 100 | 130.392 | 236.209 | 123.56 |
| Medium first 100 | 1000/1000 | 1101 | 194.867 | 269.398 | 81.70 |
| Medium next 100 | 1000/1000 | 1001 | 164.322 | 200.348 | 87.56 |
| Plain AND | 300/300 | 401 | 162.201 | 179.421 | 96.96 |
| Zero matches | 0/0 | 0 | 79.033 | 129.082 | 184.52 |
| Filtered | 1000/333 | 1101 | 174.222 | 182.930 | 82.57 |
| Declared joined | 1000/1000 | 1114 | 199.937 | 235.576 | 79.78 |

Worst p95 was 199.937ms; worst p99 was 269.398ms. This is bounded selective-term
capacity, not constant-time traversal or broad/common-term/general-join capacity.
Related payloads/nulls/masks/target DataScope are separately checked by independent
populated correctness fixtures, not inferred from this capacity oracle. Search
registration/build preserves raw JSON and immutable schema/compiler artifacts.
Ranking/language/search UI and larger deployments remain separate work.

## Verdict and remaining boundaries

**PASS for the bounded search contract.** No high/medium product finding was
identified in the inspected stable change and executed probes. The combined 55
retained tests, final nine race-reviewed groups, fresh/repeated migrations and
all eight selective capacity cases passed. Root owns final whole-project gates.

No real locale/server/library upgrade, full GIN rebuild under upgrade, arbitrary
old deployment rollback, backup/DR drill or remote CI publication was performed.
Preserve migrations 1–51 and immutable source/checkpoint evidence. Drift requires
reviewed forward repair/rebuild; do not erase evidence, weaken grants/RLS, change
budgets or use destructive down migrations. Uniqueness, aggregates, remaining
role/identity/product work, R6 and R7 remain open. Overall M9R remains PARTIAL;
no M10 or main merge is authorized.

Ignored evidence logs are under `.local/reports/r3b-search-independent-*`:
first, second, current, combined, migrations, race, race-final,
verification-final and capacity. A draft documentation edit briefly duplicated
its suffix through JavaScript replacement-string expansion; fixed with a literal
write and reviewed for unique sections/intact command rows before handback.
It did not affect source, migrations or tests. All review API/test processes
exited; only the two owned review files remain for root inspection/commit.
