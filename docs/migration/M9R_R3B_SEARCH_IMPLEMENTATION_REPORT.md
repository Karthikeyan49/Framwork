# R3b — Governed full-text search

16 October 2026. **Slice STATUS: PARTIAL — implementation under qualification.**
Started from clean `51e616d` on `remediation/m9r-foundation`. No main merge,
production activation or M10. Complete R3b and M9R remain PARTIAL.

## Contract and changes

ADR043 was written and independently reviewed before code. Additive migration
51 introduces separate fixed search registry/projection tables and GIN index,
NOLOGIN ownership, forced tenant RLS, restrictive worker denial and read-only app
grants. Registration/build use live manager/source-binding checks, shared eight
indexes/entity and 32/namespace quotas, audit atomicity, checkpoint reconciliation
and independent 1000-row copy/verification. Database-owned triggers atomically
maintain vectors/source versions; deletion cascades. Existing migrations/source
records, immutable IR/compiler formats and numeric/scalar converters are retained.

The optional Query.Search contract accepts one non-intrinsic string field and
fixed `simple` plain AND-of-words semantics. Existing `contains` remains literal
substring matching. ID-ordered pages allow typed filters and one declared join;
sort/aggregate combinations are rejected. Search input is at most 256 UTF-8 bytes
and 1–31 tsquery nodes (at most sixteen AND operands). PostgreSQL punctuation and
long-word semantics apply; source values retain their 4096-byte contract.
No search UI, ranking, dictionaries, language selection or caller tsquery syntax.
No new dependency or lockfile change.

Input field action/scope checks and record DataScope precede LIMIT, including when
the search field is not projected. Certified indexed reads match exact source
versions and canonical live-view point fetches. Missing/incomplete indexes use
bounded source fallback; incompatible registered witnesses fail closed. Workers
reject interactive search. The appended omitempty query field preserves old
query/cursor serialization when absent. Retained executable evidence is pending.

Normal RLS kept non-leakproof full-text matching above the security barrier in an
owned 100k-row probe (10.901ms, no GIN). An additional restrictive match policy
enabled GIN (0.612ms) while preserving independent tenant/membership predicates.
`app.search_text` only restricts matching vectors; it is never credentials or
authorization. The runtime sets validated bound text on the same transaction,
retains the SQL match and clears context at entry/exit and pool boundaries.
SQL-aborted transactions roll back. Readiness checks exact restrictive policy,
grants, owner, helper signatures, fixed index and trigger health.

Parser/config/dictionary/function mappings, empty dictionary options, exact server
version, encoding, database locale/provider and actual collation version form a
registry witness. Query/build/maintenance reject drift. Upgrade repair/rebuild
requires reviewed forward recovery; automatic reindex/reinterpretation is absent.

## Executed checks and failures so far

| Exact command | Result |
| --- | --- |
| `make infra-up migrate migration-status` | PASS baseline: dependencies healthy; migrations 1–50 retained. |
| `make check test-unit` | PASS baseline: static/checksum/vet/lint/typecheck, Go races, seven Node script tests and 96 frontend tests. |
| `make migrate` | PASS: migration 51 applied. It is frozen; later SQL changes must be additive. |
| `./scripts/go.sh test ./internal/runtime/data ./internal/platform/access ./internal/platform/postgres` | PASS focused Go packages. |
| Focused populated search integration commands | First FAIL: direct-query test omitted its required ScopeResolver and got ErrDenied. Corrected test setup without weakening product defaults; four-group reruns PASS (16.309s and 16.631s). Final exact command/evidence follows qualification. |
| `make test-search-performance` | FAIL initial (34.839s), second (34.238s) and diagnostic (32.749s): first seven cases passed exact oracles/GIN/latency; joined case returned HTTP503. Diagnostic captured only SQLSTATE57014, confirming the unchanged 2s statement timeout. No raw driver error/SQL arguments logged. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RSearch' -v ./tests/integration` | PASS stable root check: four populated groups, 17.173s, zero failures/skips. |
| `make test-search-performance` after target-join fencing | PASS 43.926s: all eight cases/800 measured requests, zero errors, worst p95 201.966ms/p99 233.561ms. Hot100k + nine10k tenants, 1002-byte records, ten clients, unchanged p95<=750ms. All use GIN; source visits 0–1114 according to candidate/page count. Search copies/verifies100k in203checkpoints,20.570s build. |
| Independent executable review | NOT RUN yet. |
| Final full checks/build/browser/all performance gates | NOT RUN yet. |

The capacity failures exposed an existing unfenced target-view join. The
fix uses a correlated LEFT JOIN LATERAL point lookup with OFFSET 0 while retaining
target namespace, DataScope, masking and null behavior. The same 100k-row case and
retained permission/join regressions remain required for independent/final closure;
budgets/fixtures are unchanged. The focused capacity rerun now passes as above.

## Recovery and residual scope

Apply migration 51 with controlled DDL credentials. Preserve records, registry,
vectors and checkpoint evidence. Incomplete builds retain bounded fallback; GET
reconciles unknown outcomes. Invalid source/witness drift needs reviewed repair,
never deletion or silent coercion. Quiesce and forward-fix a broken trigger/helper.
There is no destructive down migration, automatic index inheritance/rebuild or
full historical deployment rollback/backup-DR certification.

R3c uniqueness, R3d SQL aggregates/general planning, broad-term search capacity,
search UI, role-definition editing, external identity/MFA deployment, R6 authority
and R7 admission/events/audit/workflow/caching/DR remain open. Current preview
capabilities do not enable production, AI or external actions. Source/tests/ADR/
reports belong in Git; generated logs/binaries/env/keys/dependencies/builds do not.
