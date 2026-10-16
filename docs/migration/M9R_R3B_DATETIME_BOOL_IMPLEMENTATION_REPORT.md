# R3b — Governed datetime and boolean indexes

16 October 2026 (UTC). **Bounded datetime/boolean slice STATUS: PASS.**
Implementation `a3301e6`; independent tests and final tested source `fb381d0`.
Independent adversarial review and all final root gates passed.
Starting clean commit `d4e4e4c` on `remediation/m9r-foundation`. No main merge,
production activation or M10. Complete R3b and M9R remain PARTIAL.

## Implemented contract

ADR042 was written and independently reviewed before implementation. Additive
migration 50 adds native nullable timestamptz/boolean projection columns, four
fixed partial indexes and validated physical type exclusivity. Source records,
existing indexes/migrations, immutable IR and private conversion signatures stay
unchanged. Existing authenticated register/build/GET routes, live native manager
and source binding, combined quotas, generation fencing, audit and checkpoint
atomicity, 1000-row copy/verification and database-owned maintenance are reused.
No major dependency is introduced.

The new private, distinctly named native converter preserves old converters.
JSON boolean validation distinguishes false/null and rejects truthy strings or
numbers. RFC3339 source casting pins UTC/ISO DateStyle and validates the UTC
second before rounding. The physical instant matches existing SQL timestamptz
microsecond rounding, including a valid year-9999 source rounding into year 10000.
Business JSON and cursors retain the original normalized nanosecond value.
The API continues to normalize offsets through ±23:59 and permitted boundary
inputs; raw source SQL conversion fails closed for forms the fallback cast cannot
execute. There is no nanosecond SQL-ordering claim.

Queries retain certified snapshots, source-version joins and correlated canonical
live-view point fetches. Entity/field/row policies, filters and cursor predicates
precede LIMIT. ASC/DESC null-last ordering and ascending UUID ties are preserved.
Readiness checks new indexes, validated shape invariant, private helper ACL,
ownership, volatility, search path and time settings. Older binaries can keep
using fallback queries; actual retained-binary evidence is required below.

## Commands and evidence

| Exact command | Result |
| --- | --- |
| `make setup infra-up migrate migration-status` | PASS baseline: pinned Go 1.27.2/Node 24.19.0, healthy local dependencies, migrations 1–49; existing credentials/data preserved. |
| `make check test-unit` | PASS baseline: format/vet/dependency integrity/lint/typecheck, Go races, seven Node script tests and 96 frontend tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run "^TestM9RChoiceIndex" -v ./tests/integration` | PASS baseline: four existing choice groups, 66.135s. |
| `make migrate migration-status` | PASS: additive migration 50 applied, all earlier migrations retained. |
| `./scripts/go.sh fmt ./internal/runtime/data ./tests/integration` | PASS. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run "^TestM9RNativeIndex" -v ./tests/integration` | PASS: four new populated groups, 29.890s. |
| `./scripts/go.sh test ./internal/runtime/data` | PASS, 0.168s. |
| `make test-native-performance` | PASS initial: 71.203s, 12 cases/1200 measured requests, zero errors; worst p95 270.368ms, p99 317.141ms, throughput 54.62–133.71/s. Both indexes copy/verify 100k rows in 203 checkpoints each, 43.497s total build. Verified four-CPU/8GiB tier, 1013-byte hot records, 202 source visits throughout; projection visits 101/201/50101 at offsets0/100/50000. |
| Independent executable review | PASS on `a3301e6`: 47 selected integration groups in 212.394s, zero failures/skips, including both retained executable suites; all 12 native capacity cases/1200 requests PASS in 69.968s, zero errors. Worst p95 331.625ms, p99 386.290ms, throughput 49.25–123.49/s. Review tests/report committed at `fb381d0`. |
| Artifact variables below + `GOFLAGS=-v make check test build` | PASS against stable implementation/review source `fb381d0`: 217 integration tests in 846.906s, zero failures/skips; 96 frontend tests, seven Node script tests, all static/race/dependency checks, eight Go binaries and frontend production build. Overall command 884.955s. |
| `make infra-up migrate migration-status` | PASS final: dependencies healthy, repeated up applies zero migrations; migrations 1–50 retained. |
| `make test-e2e` | PASS 9/9, 19.0s. |
| `make test-signin-e2e` | PASS 7/7, 8.6s. |
| `make test-performance` | PASS 47.388s: numeric HTTP p95 141.619ms, p99 167.960ms, 105.09 requests/s, zero errors. Its pre-fix per-row-policy baseline intentionally hits SQLSTATE57014 under the unchanged 2s limit; that expected negative is not hidden or described as a passing old policy. |
| `make test-scalar-performance` | PASS 83.922s: twelve cases/1200 requests, zero errors, maximum p95 397.298ms, p99 451.824ms, throughput 34.15–115.78/s; retained 904-byte text/date fixture, 50.183s index build. |
| `make test-choice-performance` | PASS 90.362s: twelve retained enum/reference cases/1200 requests, zero errors; maximum p95 264.485ms, p99 351.147ms, throughput 49.29–126.49/s; retained 926-byte fixture, 62.376s index build. |
| `make test-native-performance` | PASS final 88.597s: twelve datetime/boolean cases/1200 requests, zero errors; maximum p95 301.813ms, p99 369.574ms, throughput 46.41–113.68/s. Both 100k indexes copied/verified in 203 checkpoints each, 58.741s build; all plans use the requested index and 202 source visits. |
| `make test-write-performance` | PASS 61.612s: 32 clients for 60 seconds, 11153 successful distinct-record writes, zero errors; p95 209.276ms, p99 249.562ms, throughput 185.45/s. |
| `make docs-verify` and `git diff --check` | PASS final source-kit checksums and whitespace; repeated after completion-report edits. |

Focused tests cover 1206 rows, indexed/fallback cursor equivalence, checkpoint
replay, trigger updates/clears/deletes, all fraction lengths, microsecond ties,
year/DST/offset boundaries, strict API/raw types, physical CHECK negatives,
timezone/grant readiness drift, populated owner/manager/tenant/field restrictions
and registration/build rollback after audit failure.

## Independent evidence

The separate [executed review](../verification/M9R_R3B_DATETIME_BOOL_INDEPENDENT_REVIEW.md)
found no high/medium product defect in this bounded slice. Seven independent
groups cover pre-epoch reversed-UUID rounding/tie SQL oracles, bool/null and
timestamp filters, session setting invariance, mixed-column faults on old/native
types, forged witnesses, populated raw roles/tenant GUCs, actual successful-COMMIT
reply loss with authenticated GET reconciliation, observed source-lock overlap
with valid PATCH/DELETE, live masks/joins and current/retained owner/share/tombstones.
The retained API binary performs actual native fallback reads, nanosecond year-9999
PATCH/false maintenance, CREATE/DELETE and graceful shutdown after migration 50.
The combined review also preserves cold-plan scalar source visits at 202.

The reviewer initially had one unused copied import: the first command failed
compilation and executed no database tests. Removing that review-only import
allowed the six-group rerun to pass in 28.367s; a seventh group was added and all 47
selected groups passed. This failure is retained in the independent report.
No product fix or weakened budget was required by the review.

Both retained executable tests are opt-in and explicitly SKIP in a clean checkout
without the historical artifact. Final root verification enables the exact
checksum-pinned artifact; it is ignored and never committed. The selected CRUD
evidence does not certify full historical UI/deployment rollback or backup/DR.

Exact retained-artifact variables supplied to the final full command:

```sh
M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3b-enum-reference-legacy-api \
M9R_LEGACY_API_SHA256=d4ffc40199931b290ed9c57fe6c82de3b381525f932675ee462bcb918926ffea \
GOFLAGS=-v make check test build
```

The ignored `.local/reports/r3b-native-final-gates.mjs` runner executes the exact
Make commands serially and preserves each exit status. Current-run logs are
`.local/reports/r3b-native-final-*.log` with result entries in
`r3b-native-final-results.jsonl`. Logs, runner, retained executable, environment,
keys, dependencies, `bin/` and frontend builds are ignored, not Git artifacts.

Independent capacity uses PostgreSQL 18.6, verified four CPUs/8GiB/swap off, 100k hot
rows plus nine 10k-row tenants, 1013-byte average hot rows. Both native indexes copy
and verify 100k rows in 203 checkpoints each (41.897s combined build). All twelve
actual API plans use the requested index with 202 source visits; projection visits
remain 101/201/50101 at offsets 0/100/50000, so deep traversal is not constant time.
General selective policies, joins and arbitrary mutation capacity require their
own evidence; 2s statements/500ms locks and p95<=750ms remain unchanged.
Final root native capacity also passed with the same 1013-byte fixture and source
visit bounds. Existing text/date and enum/reference fixtures remain 904/926 bytes.
The measured final text/date and enum/reference builds took 50.183s/62.376s; build
times vary from the earlier reports. No constant build-time or arbitrary workload
capacity promise follows from the passing per-checkpoint/query budgets.

## Changed files and acceptance

- ADR042 records the precision, compatibility, threat and recovery choices.
- Migration `00050_m9r_datetime_boolean_indexes.sql` adds fixed projections and
  forwards the manager/build/maintenance ports without editing migrations 1–49.
- `internal/runtime/data/{scalar_index,query}.go` adds typed index admission,
  witness/readiness checks and native sort keys through the existing query plan.
- Four root groups in `m9r_native_index_test.go` and seven separate-agent groups
  in `m9r_native_review_test.go` execute populated correctness/security/fault cases.
  The shared performance harness adds native mode without replacing old fixtures;
  the old choice test now checks governed boolean admission while preserving the
  old private converter's closed contract.
- Makefile adds `test-native-performance`; README, AGENTS, finding ledger and plan
  explain current capabilities, required migration and remaining work. Source
  kits remain verbatim. No Go/npm/container dependency or lockfile changes.

The bounded acceptance checks cover schema/IR compatibility, forced tenant RLS,
live DataScope/field masking, least-privilege worker/private ports, raw invalid
values, source/projection maintenance, replay/unknown outcomes and full product
regressions. These do not certify production activation, general query capacity
or the rest of the original review. Remote CI execution is NOT OBSERVED here.

Delivery remains on `remediation/m9r-foundation`. Source, executable tests, ADR and
reports belong in Git; generated artifacts and credentials do not. Main merge and
production deployment are separate tasks. The final response records the verified
remote commit and working-tree status after the documentation commit/push.

## Recovery and remaining gaps

Apply forward migrations with controlled DDL credentials. Keep source records,
registrations, projections and checkpoint evidence. A failed build leaves the
index incomplete and fallback available; malformed data requires reviewed repair,
not silent deletion/coercion. Quiesce and forward-fix a broken trigger or helper.
Metadata rollback cannot undo data migration or external actions. No destructive
down migration, production release, old full-UI rollback or backup/DR is certified.

Search indexes, R3c uniqueness, R3d SQL aggregates/general planning, remaining
role-definition UI and external identity/MFA rollout, R6 independent authority,
R7 capacity/reliability/rate limiting/audit lifecycle/caching remain open. Previous
worker-policy timeout and workflow connection failures remain tracked; passing
reruns do not close them. Production activation, AI and external actions remain
later milestones. M10 stays paused.
