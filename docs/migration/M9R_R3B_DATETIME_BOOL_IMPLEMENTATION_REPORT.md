# R3b — Governed datetime and boolean indexes

16 October 2026 (UTC). **Slice STATUS: PARTIAL — verification in progress.**
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
microsecond rounding, including a valid year-9999 source rounding into year10000.
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
| `make setup infra-up migrate migration-status` | PASS baseline: pinned Go1.27.2/Node24.19.0, healthy local dependencies, migrations1–49; existing credentials/data preserved. |
| `make check test-unit` | PASS baseline: format/vet/dependency integrity/lint/typecheck, Go races, seven Node script tests and 96 frontend tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run "^TestM9RChoiceIndex" -v ./tests/integration` | PASS baseline: four existing choice groups, 66.135s. |
| `make migrate migration-status` | PASS: additive migration50 applied, all earlier migrations retained. |
| `./scripts/go.sh fmt ./internal/runtime/data ./tests/integration` | PASS. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run "^TestM9RNativeIndex" -v ./tests/integration` | PASS: four new populated groups, 29.890s. |
| `./scripts/go.sh test ./internal/runtime/data` | PASS, 0.168s. |
| `make test-native-performance` | PASS initial: 71.203s, 12 cases/1200 measured requests, zero errors; worst p95 270.368ms, p99 317.141ms, throughput 54.62–133.71/s. Both indexes copy/verify 100k rows in 203 checkpoints each, 43.497s total build. Verified four-CPU/8GiB tier, 1013-byte hot records, 202 source visits throughout; projection visits 101/201/50101 at offsets0/100/50000. |
| Independent executable review | NOT RUN yet; design review completed. |
| Final `make check test build`, browser and retained capacity gates | NOT RUN yet for final source. |

Focused tests cover 1206 rows, indexed/fallback cursor equivalence, checkpoint
replay, trigger updates/clears/deletes, all fraction lengths, microsecond ties,
year/DST/offset boundaries, strict API/raw types, physical CHECK negatives,
timezone/grant readiness drift, populated owner/manager/tenant/field restrictions
and registration/build rollback after audit failure.

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
