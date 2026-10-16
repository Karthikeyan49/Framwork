# Independent review — enum and reference projection indexes

16 October 2026 (Asia/Calcutta). Reviewed stable checkpoint `d5d5802`, starting
from published `5f6455a`. Root paused source edits and database workloads during
the independent execution window. The reviewer added only
`tests/integration/m9r_choice_review_test.go` and this report; no production
implementation, applied migration or source kit was edited.

## Findings and scope

**PASS for the bounded enum/reference projection contract.** No high/medium
product finding identified. Independent combined verification and all twelve
representative capacity cases passed. Root owns final whole-project checks,
build, browser and retained performance gates before delivery.
The earlier text/date findings remain covered by retained regressions; they are
not relabelled as new enum/reference findings.

The review treats authorization, live source/view identity, typed witnesses,
physical indexes and compatibility as separate boundaries. An indexed reference
never grants target access or replaces relationship integrity. Operator/schema
owner corruption is outside runtime isolation; explicit operator faults verify
fail-closed detection/recovery rather than imply those credentials are confined.
Raw runtime/worker checks use verified owned disposable database/role bindings.
No SQL, SQL arguments, raw driver/provider errors, credentials, tokens, full
plans or customer values were printed by the new tests.

## Independent executed probes

- Exact Unicode options: mixed ASCII, Tamil/Latin, NFC and NFD strings remain
  distinct; byte-sorted choices validate without normalization or truncation.
  The 128-codepoint/512-byte enum value fits the projection with no overflow.
  Partial SQL NULLs, JSON null members, numbers, booleans, objects, inappropriate
  targets/options and noncanonical UUID variants fail closed. Invalid CHECK
  contracts return definite false rather than unknown.
- Enum and reference registry collation-witness faults independently return 503;
  restoring the exact witness restores the valid path. Private helpers deny app,
  worker and dispatcher execution. Two populated tenants remain isolated through
  raw registry/projection SELECTs. Forging a different tenant GUC without live
  membership reveals zero records. A worker column grant is detected, restrictive
  RLS still returns zero rows, and revocation restores grant safety.
- The wire proxy actually discards PostgreSQL's successful COMMIT response for
  each new type. HTTP 503 plus observed reply loss is followed by authenticated
  checkpoint GET; replaying the old checkpoint does not repeat committed copy
  effects. Build then independently verifies to READY.
- COPY overlaps actual valid record PATCH and DELETE for each type. Public
  blocking-PID observations prove both writers wait behind retained source locks;
  release occurs within unchanged 500ms locks. Both writers succeed and final
  verification preserves exact values/source versions with no deleted projection
  resurrection. No guessed concurrent start is counted as a lock test.
- Masked joined/filter/cursor pages match before and after indexes. Retained and
  current schema generations keep source data immutable while live owner/share
  revocation and tombstones apply to both indexes. Ownership filters precede
  LIMIT. The combined selector also retains old numeric, string overflow, date,
  certified-snapshot, cold source-visit, immutable-generation and worker tests.

## Maximum enum and retained binary

The maximum-enum fixture declares 100 unique members, each 128 Unicode codepoints
and 512 UTF-8 bytes. It copies and independently verifies 1000 populated source
records at the existing checkpoint size. First execution: COPY 425.288ms,
VERIFY 344.309ms, valid mutation 188.449ms (HTTP elapsed, including ordinary API
work). The usual 2s statement/500ms lock/3s transaction budgets remain. This is
bounded maximum-contract evidence, not a 100k/ten-client maximum-options guarantee.

A retained pre-extension API artifact was checksum-verified and preserved under
ignored `.local/reports/r3b-enum-reference-legacy-api`, SHA256
`d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea`.
Root's build metadata identifies `f99537f` with dirty documentation. The review
executes startup/readiness, enum/reference fallback reads against new indexed
results, valid PATCH maintaining both new projection versions, CREATE/DELETE,
post-write descending reads and graceful process exit after migration 49.
The process receives only API runtime bindings and a generated public test JWKS;
operator/worker credentials are absent. This is actual selected CRUD compatibility,
beyond the initial root startup-only probe. It does not certify all historical
binary behavior, UI, a schema down migration or complete rollback.

`TestM9RChoiceReviewRetainedBinaryCRUDAndFallback` is explicitly opt-in. A clean
CI checkout without the historical artifact reports SKIP, not PASS. During this
review it executes using:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3b-enum-reference-legacy-api \
M9R_LEGACY_API_SHA256=d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea \
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RChoiceReview' -v ./tests/integration
```

## Commands and results

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; formats only the new review file. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RChoiceReview' -v ./tests/integration` | PASS 15.714s for the initial four independent groups. |
| Opt-in command above | PASS 21.688s for all six expanded groups, including the retained artifact. |
| Same opt-in variables + `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM9R(Numeric\|Scalar\|Choice)(Index\|Review)\|TestM1FreshDatabaseMigrations)' -v ./tests/integration` | PASS 139.302s, 36 top-level tests, including all eight retained scalar-review functions, all six choice-review functions, actual old-binary CRUD and fresh/repeated migrations. Cold late-date source visits remain 202. Max-enum rerun COPY 455.682ms, VERIFY 353.428ms, mutation 177.607ms. |
| `make test-choice-performance` | PASS 80.608s: all twelve 100k-row/ten-client/100-sample cases, 1200 measured requests, zero errors; worst p95 287.883ms versus 750ms. |
| `git diff --check` | PASS at independent-test checkpoint. |
| Whole check/test/build, browser and retained numeric/text/date/write capacity | NOT RUN by reviewer; root owns final delivery checks after handback. |

No failing product attack/test is omitted from this review. One tool transport
interruption occurred before applying an expanded test patch; the file was checked
unchanged, then the edit was applied. No database/test command ran in that failed
tool call and no product or fixture failure is claimed from it.

## Representative capacity

Verified PostgreSQL 18.6 reference container: four CPUs, 8GiB memory, disabled swap.
The hot tenant has 100000 records; nine other tenants have 10000 each. Hot records
average 926 bytes. Both new indexes copied and independently verified 100000 rows
in 203 checkpoints each, 54.180407s combined build. Each case has three warmups,
100 measured requests and ten HTTP clients; all 1200 measured requests succeeded.

| Sort | Offset | p95 ms | p99 ms | requests/s |
| --- | ---: | ---: | ---: | ---: |
| Enum ASC | 0 | 119.744 | 130.820 | 128.91 |
| Enum ASC | 100 | 141.205 | 154.104 | 119.99 |
| Enum ASC | 50000 | 190.152 | 242.215 | 75.79 |
| Enum DESC | 0 | 106.712 | 125.560 | 134.06 |
| Enum DESC | 100 | 117.521 | 142.335 | 124.28 |
| Enum DESC | 50000 | 171.689 | 218.902 | 79.22 |
| Reference ASC | 0 | 113.057 | 149.464 | 123.31 |
| Reference ASC | 100 | 102.717 | 125.628 | 131.12 |
| Reference ASC | 50000 | 287.883 | 320.959 | 46.57 |
| Reference DESC | 0 | 116.376 | 215.718 | 114.74 |
| Reference DESC | 100 | 123.624 | 174.676 | 126.66 |
| Reference DESC | 50000 | 253.489 | 290.091 | 54.29 |

Actual API SQL selected each requested ascending/descending enum/reference index.
Every case visits 202 source records. Projection visits are 101/201/50101 at
offsets 0/100/50000; deep cursor traversal remains linear in skipped projection
entries. The gate establishes bounded source fetches, not constant-time cursors
or all selective/join workloads. The 100k case uses the declared representative
choice distribution; the separate 100-member/512-byte maximum-contract case
establishes 1000-row checkpoint/mutation evidence, not the same large-load claim.

At handback, all review database/test/API processes have exited. Only report
prose was updated after returning source/database exclusivity to root. The
historical binary remains ignored and available for root's explicit opt-in
final suite; it should never be committed as a generated artifact.

## Boundaries that remain

Datetime/bool/search indexes, uniqueness, expanded SQL aggregates, broader
selective-policy/join capacity, role editor UI, external identity certification,
R6 verification authority, R7 operations and production active-release execution
remain outside this slice. Real collation-library upgrade/reindex and backup/DR
drills were not executed here. Preserve 1–49 and all records/index evidence on
rollback; use reviewed forward repair, not destructive down migrations or weaker
grants. No new dependency or larger budget is introduced. M9R remains PARTIAL;
no M10 or main merge is authorized by this review.
