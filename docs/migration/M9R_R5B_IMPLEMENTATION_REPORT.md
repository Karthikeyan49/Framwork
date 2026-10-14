# M9R R5b — Live worker SQL capabilities

Date: 15 October 2026 (IST). Source: `d093e80` and forward fixes `a5bba7b`,
on `remediation/m9r-foundation`, both pushed. Root checks and bounded independent acceptance PASS. Final independent execution
used the existing regression suite after a platform content interruption; new
independently authored final probes were NOT RUN, as the review explains.
Full R5/M9R remain PARTIAL. No M10, main merge or production rollout.

## What changed

Migration 39 adds a database-owned, no-argument decision for the exact authenticated
lease. Business RLS/private ports enforce the current active role head, workflow
execution/activity actions, entity/row/declared-field read grants and typed record
scopes. Explicit DENY wins. Unknown or invalid input fails closed. It evaluates
one leased record, not every business row in an API query.

Exact identity/routing remains available for bounded denial evidence, with no
business permission. Go Worker.Step checks SQL before exposing the immutable
snapshot. A rejected business transaction rolls back; the existing exact-ticket
append records the current policy hash/revision in a separate audit transaction.
Denied raw workers cannot read the source/binding/instance or write workflow
state, local effects or allowed audit evidence. Event envelope/receipt access
requires the current events.consume grant; completed receipts reconcile without
repeating their effect.

Ownership/shares use current logical identity across retained generations.
Conditional access over retired business data is suspended until there is a
verified typed mapping to current data; pinned IR/input bytes are preserved.
Whole-plan activity permission is required before execution if the plan includes
an activity. The private source helper is an ungranted SECURITY INVOKER function;
an accidental EXECUTE grant does not give workers schema-owner privileges.

Independent review found two issues at d093e80: SQL accepted an uppercase UUID
condition rejected by Go, and row policies repeatedly parsed the policy/IR until
a receipt query timed out. Migration 40 preserves applied 39, aligns UUID/reference
constants with the existing lowercase contract, and evaluates expensive policy
work in statement InitPlans. Identity and lease-clock checks remain on each row,
including WITH CHECK. Direct security-definer business ports still check policy.
No timeout was raised, privilege broadened, artifact recompiled or record deleted.

Changed files: migrations 39/40, policy readiness, workflow SQL denial handling,
tenant worker identity admission, raw integration regressions, ADR033 and reports.
No new dependency. [ADR033](../adr/033-m9r-worker-sql-capability-enforcement.md)
records the design; [independent review](../verification/M9R_R5B_INDEPENDENT_REVIEW.md)
preserves original attacks, failed probes, fixes and final review evidence.

## Commands executed and results

All database fixtures are owned, verified disposable local databases. Logs and
builds remain ignored. Source kits are unchanged: 84 V2 and 43 V3 checksums pass.

| Exact command / run | Result |
|---|---|
| Raw/worker/event/pinned-generation command below | PASS, 42.470s |
| `make migrate migration-status` at d093e80 | PASS; migrations 1–39 applied |
| `make check test build` at d093e80 | PASS; full integration 587.550s, static/Go race/Node/frontend/builds |
| Independent initial raw/migration/snapshot controls | PASS; HIGH UUID mismatch and MEDIUM repeated-evaluation timeout reproduced, not accepted |
| Forward40 command below | PASS, 28.535s; uppercase UUID/reference denied, InitPlan loops verified, slow expired-lease write rejected |
| `make migrate migration-status` at a5bba7b | PASS; migrations 1–40 applied |
| `make check test build` at a5bba7b | PASS; integration 558.071s, Go race, format/vet/static/module checks, Node 5 and frontend 29 tests, backend/frontend builds |
| `make test-e2e` at a5bba7b | PASS; all five browser tests, 10.3s |
| Independent final existing seven-root command at a5bba7b | PASS, 33.457s; bounded source review and regression acceptance |
| New independent final probes / manual 39→40 rehearsal | NOT RUN after platform interruption; earlier 38→39 rehearsal PASS |
| Production deployment; full 96-step lifecycle; maximum-policy Go lifecycle | NOT RUN / not claimed |

Raw/worker/event/pinned-generation command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RSQL|^TestM9RWorkerCapability|^TestM9REventConsumer|^TestM9RShadowWorkflowPins' -v ./tests/integration
```

Forward40 command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RSQL|^TestM9RWorkerCapability|^TestM9REventConsumer|^TestM9RExactCapability' -v ./tests/integration
```

The targeted root synthetic receipt scan uses 96 operator-seeded receipts and a
publicly admitted seven-grant/63-node policy per data capability. It completes in
1470.337ms under the unchanged 2000ms timeout. EXPLAIN verifies each expensive
policy Function Scan runs at most once. This is a bounded SQL regression, not
full 96-step business lifecycle or a larger capacity promise. The independent
existing-regression run measured 1933.184ms, only about 67ms below the unchanged
2000ms limit. Remaining identity/anti-join cost needs broader profiling; this is
not a sustained p95 or scalability certification. Ordinary 100k API
query and 32-writer gates were independently measured at the preceding R5a source;
this slice changes worker paths, not those API query/write implementations.

Earlier FAILs are retained: a revoked Go operation was initially misclassified
as a stale lease (fixed by separate identity/business admission); raw fixtures
omitted required audit trace_id/trusted actor context; a fixture used an enum
value absent from its schema. Independent UUID and timeout findings are recorded
with pre-fix runs. Numeric normalization was investigated and discarded after
actual SQL/Go controls agreed. Dense 99-state fixtures failed before independent
worker measurement; they do not prove capacity or a migration39 regression.

## Acceptance and remaining limits

Root tests cover configured action/field/head revocation; OWNER, hierarchical TEAM,
SHARE, AND/OR and typed CONDITION; changes without publishing another role; current
canonical access after cutover; inactive definitions; masked source fields;
raw state/effect writes and denial audit; exact lease/transaction lifetime;
event receipt revocation; private helper grant drift/readiness; and preserved
in-flight pins. Full M0–M9/M9R regressions and browser behavior pass.

Threat model: compromised worker database credentials with a legitimately issued
exact tenant/job lease. Generic records, other jobs/tenants, undeclared fields and
operator functions remain inaccessible. This does not certify compromised DBA,
application producer provenance, external tools or a worker's business correctness.
Policy uses the authorized transaction/statement snapshot; revocation does not
retroactively cancel committed effects. SQL lease clocks remain checked per row.
The trusted Go adapter may apply stricter execution/combined-scope budgets.

Record owner/team/share administration and its UI, field masking/partial updates,
custom workflow approval-role contracts, membership revision CAS, provider/login
UX, uniqueness/aggregate expansion, authoritative acceptance and production
activation, outage/audit/rate-limit/DR operations remain open in the
[finding ledger](M9R_FINDING_STATUS.md). Broad bootstrap roles remain until an
owner publishes constrained definitions. Full R5 and the foundation gate remain
open; do not start M10 or claim production readiness.

## Recovery and Git

Migrations 39/40 change functions/policies and preserve tables, records, bindings,
IR/input bytes, histories, receipts and audit. They are forward-only. Quiesce
workers during an operational cutover and restart with the compatible binary.
Keep owner authority; recover a role with another reviewed version and head CAS.
Recover a retired conditional job through verified compatibility or an explicitly
reviewed valid grant, without rewriting its binding. Never restore broad grants
or roll back to an evaluator that ignores current authority.

Push source and final reports on the existing remediation branch; main stays at
its separate checkpoint. Ignore .env/private keys, dependencies, binaries, builds
and local logs. No customer data, source-kit document or archive was deleted here.

R5b STATUS: PASS (bounded worker SQL authority and documented verification scope)

R5 / M9R STATUS: PARTIAL
