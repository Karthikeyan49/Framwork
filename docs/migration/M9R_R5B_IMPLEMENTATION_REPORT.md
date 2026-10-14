# M9R R5b — Live worker SQL capabilities

Date: 15 October 2026 (IST). Status: implementation checkpoint; targeted
regressions PASS, full checks and independent acceptance PENDING. Full R5/M9R
remain PARTIAL. This is a remediation branch checkpoint, not production rollout.

Baseline: bdc7675. Migration 39 adds database-owned, no-argument decisions for the
exact authenticated lease. Business RLS/private ports check live role head,
actions, declared-field grants and owner/team/share/typed-condition scopes.
Exact identity/routing remains available for denial evidence, not business data.
Go Worker.Step checks the SQL decision before reading its immutable snapshot.

Current logical ownership/sharing applies across retained generations. Conditional
access over retired business data is suspended until schema compatibility is
verified; pinned IR/input bytes are preserved. Whole-plan activity permission is
required before execution if the plan includes an activity. No external tool or
production route is enabled. The private source helper cannot elevate raw workers.

Targeted commands actually executed:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RSQL|^TestM9RWorkerCapability|^TestM9REventConsumer|^TestM9RShadowWorkflowPins' -v ./tests/integration
```

PASS: 42.470s. Includes populated raw SQL allow/deny controls; workflow action,
entity/row/field/head revocation; state/effect writes; owner changes; hierarchical
team and share revocation; typed condition/list/money/null/reference checks;
canonical ACL after cutover; retired-condition denial/recovery; event receipt
revocation; original trusted worker denial evidence and immutable pin tests.

Earlier failures retained: first Go denial was misclassified as a stale lease
(fixed by distinct identity and business access); raw fixtures omitted required
audit trace_id and trusted actor context; one condition fixture used an enum value
not in its schema. Corrected runs passed without weakening access assertions.

Recovery: additive forward migration only. Quiesce workers for operational cutover,
retain records/jobs/IR/receipts/audit, restart with the compatible binary. Recover
role changes with another reviewed version; never restore broad worker grants or
roll back to an evaluator that ignores current policy. No customer data deleted.

Full `make check test build`, shared-local migration/readiness, browser regression,
independent raw-credential probes and performance checks are pending at this
checkpoint. No claim of full completion is made. ADR033 records the SQL boundary.
