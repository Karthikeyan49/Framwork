# M9 independent adversarial review

Verdict: **PASS for the documented development/test workflow runtime**. Review
was performed by the existing independent review agent, read-only. It did not
edit implementation, tests or documentation, reset shared services, or use shared
application ports. PostgreSQL checks used uniquely owned disposable databases.
No unresolved BLOCKER/HIGH/MEDIUM/LOW code findings remain.

## Findings and executed closure evidence

| Severity | Finding | Fix and verification |
| --- | --- | --- |
| HIGH | A history-budget rejection after UPDATE could become HTTP400 while committing partial state. | Pre-mutation budget guards; service failures roll back before response; fresh live-identity denial audit. Version1000 and injected known-error-after-write negatives passed. |
| MEDIUM | Early worker save paths omitted final lease check. | Every worker save uses the fenced helper. Expired lease and concurrent signals passed. |
| MEDIUM | Readiness checked only representative grants/unqualified triggers. | Complete required/forbidden column grants, exact relations/functions/triggers and forced RLS. Revoked-column/disabled-trigger negatives passed. |
| MEDIUM | Condition failure after a successful local effect stranded compensation. | Persist the receipt and enter FAILED compensation. Executed regression passed. |
| MEDIUM | APPLIED reconciliation could be stranded by a failing next condition. | Preserve the APPLIED fact/receipt, then enter compensation. Executed regression passed. |
| MEDIUM | Workflow-only startup omitted mandatory audit readiness. | Refuse missing audit grants before starting; actual execution without event broker and graceful shutdown passed. |
| LOW | Rejection auditing lost the actual inner policy denial provenance on rollback. | Carry bounded denied decision evidence through the rollback error; rebind actor/tenant for denial audit. HTTP403, correct denial ID/version and zero bindings independently verified. |

These findings originated in source inspection. No pre-fix independent exploit
execution is claimed. Closure evidence combines inspected fixes and real
post-fix PostgreSQL negatives. Exact old-ticket pin rejection and field-only
DataScope tests were strengthened during review and independently passed.

## Commands actually executed by the reviewer

| Command | Result |
| --- | --- |
| `./scripts/go.sh test -race -count=1 ./internal/runtime/workflow ./internal/compiler ./internal/config ./tests/security` | PASS:1.044s/7.240s/1.019s/1.027s |
| `./scripts/go.sh test ./internal/runtime/workflow -run '^$' -fuzz '^FuzzWorkflowRestore$' -fuzztime=5s -parallel=2` | PASS:34,318 executions,5.516s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=120s -run '^TestM9' -v ./tests/integration` | PASS:89.608s, including actual process kills and lost COMMIT response |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=60s -run '^TestM9(RejectedMutationsRollbackBeforeHTTPResponse\|TimerTimeoutAndFailurePinCannotSwitch)$' -v ./tests/integration` | FAIL during concurrent authoring: unused test import; fixed |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=90s -run '^TestM9(RejectedMutationsRollbackBeforeHTTPResponse\|TimerTimeoutAndFailurePinCannotSwitch\|DataScopeAndFieldDenialApplyToSnapshotsAndResume\|AppliedReconciliationConditionFailureCompensates\|WorkflowOnlyStartupRequiresAuditAndStopsGracefully)$' -v ./tests/integration` | PASS:22.665s; three matching tests ran, two guessed names did not match |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=60s -run '^TestM9(FieldScopeDeniesStartSnapshotAndWorkerResume\|AppliedReconciliationPreservesFactAndCompensatesConditionFailure\|WorkflowOnlyStartupRequiresAuditAndRunsWithoutEventBroker)$' -v ./tests/integration` | PASS:all three exact tests,21.331s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=40s -run '^TestM9PolicyDenialEvidenceSurvivesBusinessRollback$' -v ./tests/integration` | PASS:2.889s; actual denial provenance preserved |
| `npm --prefix apps/web test -- src/preview/Workspace.test.tsx` | PASS:14 tests, IR1.2 compatibility included |
| `npm --prefix apps/web run typecheck` | PASS |
| `git diff --check` | PASS |

## Limits of the verdict

Lifetime history is limited to1000 entries. Reaching the cap blocks further
mutations/recovery signals; it requires an operational forward fix. Pins, inputs,
receipts and history must not be cleared to extend it.

Production activation, external tools, remote CI, universal exactly-once external
effects, complete browser/smoke verification and human deployment approval were
NOT independently verified. Root browser/smoke evidence is recorded separately.
Registered activity is trusted `local.noop`; M10 external tools and later
production services remain outside this milestone.
