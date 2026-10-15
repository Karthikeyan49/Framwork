# R5d — Field visibility and partial writes

Date: 15 October 2026 IST. Status: implementation checkpoint; independent review
and final whole-suite verification pending. Full R5/M9R remain PARTIAL. M10 is
paused. Branch: `remediation/m9r-foundation`; starting clean checkpoint `cbf323e`.

## Implemented scope

ADR037 defines an explicit SQL-masked interactive read port. Entity DataScope
selects records. PostgreSQL returns only readable projected values, with
`field_access.masked` and `field_access.writable` decisions independent of hidden
property presence. Required hidden fields do not invalidate a read. Masking a
joined projection retains the entity-visible target. Intrinsic record ID,
version, ownership and timestamps remain visible record headers.

Filters/sorts/sums/source relationship keys require field readability covering
the entity-visible set: ALL or exact normalized entity scope. Other scoped
predicates fail closed. Count and ID pagination are independent of field
projection visibility. Internal strict queries, rules, workflows and private
worker source enforcement retain their existing contracts; no masked null is
substituted for an executable input. IR/source versions are unchanged.

Explicit `patch` merges touched values in PostgreSQL under CAS. It checks
entity/row read+write and touched-field read+write, validates touched values,
preserves omitted values/reference links and supports optional explicit-null
clearing. Create/delete/full-replacement `update` remain strict. Only changed
reference links are replaced; numeric projection triggers remain in force.
Responses are SQL-masked. Receipt, business mutation, audit, outbox and workflow
trigger effects share the existing transaction. Patch emits `record.updated`.
Receipt replay returns headers without data; unknown commit outcomes require
owned receipt reconciliation. Role authors must explicitly grant
`commands.reconcile` for that recovery UI.

Partial writes reject CONDITION scopes on entity or touched fields to prevent
write success probing hidden business conditions. Byte admission uses known
readable values plus public worst-case reservations for hidden fields. It never
queries actual hidden lengths. Large hidden schemas can be conservatively
rejected even when the actual record is small; fully readable wide records can
be patched within the existing 64 KiB budget. Field hints remain advisory and
commands recheck policy.

The frontend distinguishes Restricted from empty/null, disables masked or
non-writable inputs and submits changed writable fields only. New typed access
responses are validated; legacy responses without hints remain readable.
Migration 46 extends only the receipt operation constraint; no data rewrite,
new dependencies, worker grants, production endpoints or shared customer DDL.

## Evidence recorded so far

| Command actually executed | Result |
| --- | --- |
| `make migration-status` | PASS; 1–45 applied at baseline |
| `make migrate` | PASS; migration 46 applied |
| `make fmt` | PASS |
| `./scripts/go.sh test ./internal/runtime/data` | PASS |
| `npm --prefix apps/web run typecheck` | Initial FAIL TS narrowing in new decoder; fixed, rerun PASS |
| `npm --prefix apps/web test` | Initial FAIL two expectations for old whole-replacement/no-change behavior; updated to changed-field patch behavior; PASS 30/30 |
| `make check` | PASS; source-kit checksums, Go format/vet/modules and TS lint/types |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^(TestM5CRUDTypedValuesCASAndVersionIsolation|TestM6RSQLDataScopeOwnershipTeamsHierarchySharesAndConditions|TestM9RCapabilitiesPublicAPISQLScopesAndRevocation)$' ./tests/integration` | PASS 8.824s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RField' ./tests/integration` | Initial three tests PASS 10.644s. After adding unknown-commit probe, FAIL because new role fixture omitted commands.reconcile; explicit fixture grant added. Rerun four tests PASS 11.110s |
| `make build` | PASS all eight Go binaries and frontend |
| `make test-e2e` | Initial 5/6 PASS; new limited-role fixture lacked tenant.access/preview selector. Fixed fixture without widening product authorization; rerun PASS 6/6 in 13.3s |

A subsequent refinement uses already-readable values for byte admission so wide
fully readable schemas retain editing. Final results for that source follow in
the completion section after independent review.

## Recovery and limitations

Retain migration 46 and immutable receipts. Roll forward if migration/startup
fails; no rollback DDL or customer record deletion is needed. Old binaries do
not understand patch commands; retain the new reader during a behavior rollback.
Existing update callers retain replacement semantics. Preview remains isolated
from production. This is no claim of production readiness, arbitrary scope
implication proofs, timing noninterference, policy-author correctness, arbitrary
blind writes or unlimited hidden-record size. Conditions and large hidden-field
reservations are explicit conservative limits, not completed general solutions.

Administration UI, identity login/clock skew, R3b–R3d, R6 and R7 remain separate
open work. No main merge and no M10.
