# R5d — Field visibility and partial writes

Date: 15 October 2026 IST. Status: independent source and fixture review PASS;
final whole-suite/build/browser/performance verification running. Full R5/M9R remain PARTIAL. M10 is
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

## Independent findings and remediation checkpoint

Independent review of `88c8528`, committed as `fa22701`, reproduced two HIGH
findings with executed negative fixtures. Patch omitted trusted owner attributes
from field actions; SQL-masked projections/predicates also used contextless
field actions. Five owner-policy regressions initially failed. The independent
report preserves exploit evidence and successful boundaries; these findings
were not dismissed as unsupported callbacks.

The fix moves definitive touched-field read/write guards after trusted owner
loading and before field-value validation. Interactive custom Go engines now
use a bounded header-only preflight. Guards receive actual record ID/owner;
allowed ID arrays become SQL CASE predicates, so denied values never reach Go.
Predicates check the complete entity-visible header universe before caller
filters/sorts/cursors/limits/joins (except exact ID selection). Any denied field
rejects the predicate. Opaque universes/pages cap at 32 headers, including
lookahead; larger cases fail budget. The opaque port verifies actual RR or
SERIALIZABLE isolation; weak-isolation executors fail before value selection
or patch mutation. Existing WithRuntime already provides RR.

A closed proof in policy recognizes exact Native/Defined concrete kernels;
API supplies its actual live policy engine to that optimized port. Unknown
wrappers, including embedded Native, cannot inherit the optimization. Native/
tenant-definition page and workload gates are unchanged. This is an explicit
custom-callback bound, not a widened SQL permission or post-fetch redaction.

Post-fix focused owner/root/legacy projection regressions passed 34.162s.
A new opaque-budget test initially expected budget rejection while filtering a
field whose OWNER scope correctly rejects coverage first (403). The fixture now
filters the public ID to exercise budget admission independently; all existing
scope/inference denials remain. The corrected owner/budget regressions passed
22.521s, and Go policy/data race tests passed. Final snapshot guard and full
verification are recorded after independent re-verification.

The final snapshot proof also certifies the unexported WithRuntime executor;
an autocommit pool with default RR cannot impersonate a shared transaction.
Unknown executor wrappers/raw transactions are rejected by the opaque port.
`make check` passed again. The final owner/root/legacy projection command
`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RField|^TestIndependentR5d|^TestIndependentM6RFieldScope|^TestM5RecordSpecificField|^TestM9RCustomSortPage' ./tests/integration`
passed in 41.176s. Independent second review and final whole-suite run remain
pending at this checkpoint.

## Alternate input-path review

Root requested owner-dependent action checks through rules/workflows as well as
interactive reads. At `c96c534`, the reviewer executed a valid HIGH rule oracle:
interactive masking hid balance, but strict rule evaluation still consumed it
and returned a result. The test/report checkpoint is `d9140d3`.

An initial workflow probe denied Name and observed a successful start/binding.
That interpretation was withdrawn after inspecting actual executable FieldIDs:
the fixture workflow consumes Status, not Name. It did not demonstrate an
unauthorized workflow capture. The corrected independent verification must deny
the consumed Status field and verify zero bindings, and separately permit the
unrelated hidden Name while asserting it is absent from the snapshot.

Strict point reads now load the trusted record header before field decisions,
cover all projected and predicate fields, and require the certified runtime
snapshot. They select only declared JSONB fields and core/cursor necessities.
Strict point joins preflight the target owner before target value selection.
These are hard denials, without masked nulls passed to execution. Private worker
sources continue using their exact lease-confined function. This strengthens
runtime authorization without changing/recompiling immutable IR or plans.

The first broad post-fix command ran 155.114s: the rule regression and other
selected tests passed; the workflow test still failed its incorrect Name-denial
expectation. Its fixture/interpretation correction is recorded explicitly above.

On the final strict-point source, `make check` passed and
`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5dStrictRule|^TestM9RField|^TestM7' ./tests/integration`
passed 22.191s. Policy/data unit tests also passed. Independent corrected workflow,
join and snapshot proofs, then the final full suite, are still required.

## Independent closure and full-suite fixture correction

The separate reviewer passed the bounded R5d source at `a4d6054`, with regression
tests/report committed as `acef7f8`. All three demonstrated HIGH findings are
closed. Twenty focused race-enabled integration tests passed in 41.567s and
eight frontend decoder tests passed in 1.50s. The review explicitly withdraws
the incorrect Name/workflow interpretation and verifies the actual Status
negative plus the unrelated hidden-Name positive. See the
[independent report](../verification/M9R_R5D_INDEPENDENT_REVIEW.md) for exact
commands, populated negative fixtures and threat-model limits.

The first final `make check test build` run **FAILED**: checks, Go race units,
five script tests and 38 frontend tests passed, but the integration suite failed
after 773.184s at `TestM9RNumericIndexDataScopeEquivalence`. Build was **NOT RUN
in that invocation**, because make stopped at the integration failure. The test
used an opaque Go callback to sort/page a 78-row universe, beyond the new
32-header custom-callback admission contract. No runtime limit or negative
assertion was relaxed to make it pass.

Fixture-only commit `ea6d20e` replaces that callback with real live tenant role
definitions, preserving all 78 records, four scope modes and exact before/after
index equivalence. Root's focused numeric/opaque-budget command passed in
8.999s. Independent commit `3466759` further requires exact populated counts:
39 actual-owned rows, 39 shared foreign-owned rows, one condition-matching row
and 78 rows with Name absent and explicitly masked for field DENY. It also
requires visible Name in the allowed modes. Four independent focused race
tests passed in 11.370s, including unchanged opaque-universe admission and
owner-sensitive hidden-predicate denials. Runtime source remains `a4d6054`.

Root focused command actually executed:

`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9RNumericIndexDataScopeEquivalence$|^TestM9RFieldOpaqueHeaderBudget' ./tests/integration`

The separate reviewer records its exact four-test command in its report. The
root full-suite rerun uses the unchanged `make check test build` command and
15-minute aggregate integration deadline. That rerun was **INTERRUPTED**, not
passed: the execution server disconnected, then killed the original make,
Node, Go and integration process group with SIGKILL (recorded exit status 9
in `/proc`). Checks, five script tests and 38 frontend tests had passed, but
no final integration result was produced and build was NOT RUN. This was not
reported as an assertion failure or a successful regression run.

Verification was restarted as `GOFLAGS=-v make check test build` in an ignored
detached wrapper (`nohup setsid sh .local/reports/r5d-verify.sh`) that saves the
aggregate exit status. Verbose output adds per-test progress only; assertions,
test selection and query/statement/request/integration deadlines are unchanged.
Its outcome and subsequent browser, performance and migration-idempotence
results must be recorded before this report declares bounded R5d completion.
Generated logs/scripts under `.local/` remain ignored; no credentials or
customer payloads are committed.

The first durable verbose run completed with exit **2 / FAIL** (integration
813.914s). All R5d cases and the corrected numeric-scope fixture passed, but
`TestM9RWorkflowApprovalReviewFieldCapabilityAndReceiptRevocation` failed with
`driver: bad connection` while advancing its fixture to WAITING_APPROVAL.
Build was NOT RUN after that failure. No assertion or application behavior was
changed in response. The exact focused command
`node scripts/with-env.mjs ./scripts/go.sh test -v -race -tags=integration -timeout=5m -count=3 -run '^TestM9RWorkflowApprovalReviewFieldCapabilityAndReceiptRevocation$' ./tests/integration`
passed all three repetitions in 52.466s. The cause of the one connection failure
is unconfirmed; a repeat pass is not a claimed connection-reliability fix. It
remains operational evidence for R7, and a clean full rerun is required.

A separate read-only reviewer confirmed the evidence limit: `m.until` propagates
`m.worker.Once` errors without identifying dispatcher claim, assigned-work
lookup or Step/WithJob. The fixture failed before its revocation assertions.
Connection invalidation, backend/transport loss and existing 3-second
transaction/idle budgets under load are possible explanations, not established
causes. The three repeat passes show nondeterminism, not a fix. No new tests,
database calls or source edits were performed during that triage. R7 owns
sanitized stage/connection/deadline diagnostics and reliability follow-up.

## Changed areas and dependency impact

| Area | Files / purpose |
| --- | --- |
| Runtime | `internal/runtime/data/{visibility,field_actions,patch}.go`, model/query/store and unit contracts: SQL visibility, trusted header admission, explicit partial update |
| Policy / transaction composition | `internal/policy/query.go`, `internal/platform/postgres/transaction.go`, `internal/platform/access/preview.go`: closed policy/snapshot proofs and live-engine wiring |
| Web | `apps/web/src/api/preview.ts`, `src/preview/contracts.ts`, `src/preview/Workspace.tsx` and tests: typed hints, restricted display, changed-field patch and decoder checks |
| Database | `db/migrations/00046_m9r_field_patch_receipts.sql`: allow typed patch receipts, preserve all existing data/receipts |
| Verification | Root and independent field-access integration tests, populated historic projection/scope/index fixtures, six browser scenarios |
| Architecture / delivery | ADR037, this report, independent report, root README/AGENTS and current finding/plan status |

No Go/npm module, lockfile or external service was added. Existing Go standard
library, PostgreSQL JSONB/transactions, policy contracts and React components
provide the implementation. No compiler/schema/IR version upgrade, immutable
artifact rewrite or customer metadata-triggered DDL is introduced.

Browser execution covers a limited editor editing a public field while a secret
stays masked and unchanged, alongside existing CRUD/CAS/pagination, lost-response
reconciliation, credential redaction and mobile-layout scenarios. This is the
preview editor; the separate record/team administration UI is not implemented
by this slice.

## Final verification evidence

| Command actually executed | Result |
| --- | --- |
| `make build` | PASS; all eight Go binaries and production frontend bundle |
| `make test-performance` | PASS 61.937s; PostgreSQL 18.6, 100k hot rows plus nine 10k-row tenants, 761-byte average hot record, 128 MB shared buffers / 4 MB work_mem. Ten clients / 100 HTTP samples: p95 216.415 ms, p99 287.217 ms, zero errors; unchanged 750 ms HTTP target. Key/numeric/selective SQL p95 1.741 / 170.995 / 98.441 ms pass their unchanged targets. Governed numeric index required for the HTTP workload. |
| `make migrate migration-status` | PASS; idempotent reapplication, versions 1–46 applied, no pending migration |
| `make test-e2e` | PASS 6/6 in 13.6s against the final built API and current frontend: CRUD/CAS/pagination, real lost-response receipt reconciliation, two redaction cases, mobile layout and limited-editor masked patch |

Performance is evidence for this explicit indexed fixture and host, not an
unlimited query/aggregate capacity claim. Unindexed plans, other index types,
uniqueness and general SQL aggregation remain separately tracked R3 work.

The final whole-suite confirmation repeats `GOFLAGS=-v make check test build`
without code changes, saved to ignored `r5d-full-confirm.log` and `.exit`.
It runs alone after the performance/browser workloads. The failed first durable
run and all three successful focused approval repetitions remain evidence above.
