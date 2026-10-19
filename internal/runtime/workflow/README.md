# Workflow Runtime V2

M9 owns the business model (`Plan`, `Program`, `Durability`) and a PostgreSQL
adapter. The compiler converts metadata1.2 into executable DSL2 plans.
Legacy workflows remain authoring graphs and cannot execute as DSL2.

Instances pin source revision, IR bytes/hash, workflow version/plan hash, record
correlation and captured inputs. Restore verifies source/plan agreement without
compiling. DataScope checks read the live record to authorize an operation,
without replacing captured inputs used in conditions.

Each worker uses one server-claimed routed job with live identity/membership,
policy, RLS and a PG-time token/generation lease. A final fenced save commits
local effects, idempotent receipts, ordered history, audit and lifecycle outbox
together. `LocalActivity` is trusted transactional Go only: no network, arbitrary
metadata SQL or ambient capabilities. Registered V1 activity: `local.noop`.
Additional business adapters need their own policy/data contracts; M10 owns tools.

States: READY, WAITING_TIMER, WAITING_APPROVAL, RETRY_WAIT, SUCCEEDED, FAILED,
CANCELLED, TIMED_OUT, DEAD, RECONCILE, COMPENSATING, COMPENSATION_FAILED.
Timers/deadlines use PostgreSQL time. Known local rollback permits bounded retry;
unknown effect outcomes pause for reviewed reconciliation. Tool steps (ADR100
Amendments): every fresh non-success outcome is counted, backed off and bounded
by `max_attempts`; a tool step with an unknown outcome outstanding ends in
RECONCILE, never DEAD; worker state changes must follow an `always` transition
of the pinned plan (database trigger), and tool receipts must equal the ledger. A lost commit response
is resolved by stored receipts and versions. Lease crashes retain append-only
routing attempts;20 failed leases cause routing DEAD. Disabled actors produce
BLOCKED routing without reading payloads.

Approvals require live allowed roles, a distinct approver and expected instance
version/state. Idempotency keys bind request hashes and actor. Recovery/reconciliation
requires owner/admin, expected state/version and operator reference; it preserves
all pins, inputs and receipts. Cancellation/timeouts enter compensation. Explicit
compensators run in reverse receipt order; failures pause for reviewed recovery.
This does not reverse arbitrary external actions.

Preview API paths:

```
POST /v1/tenants/{tenant}/preview/{app}/versions/{revision}/workflows/{workflow}/start
GET  .../workflows/{workflow}/instances/{instance}
POST .../workflows/{workflow}/instances/{instance}/signals
```

Start body: `record_id`, `idempotency_key`. Signal body: `operation`,
`expected_version`, `state_id`, `idempotency_key`; recover/reconcile also require
`operator_ref`. Reconcile requires `outcome: APPLIED|NOT_APPLIED` and a bounded
captured `output` for APPLIED. Operations: approve, reject, cancel, recover,
reconcile. Responses contain safe state/pins/history, never raw inputs or outputs.

Record-created/updated triggers start in the original mutation transaction using
its record snapshot and event ID. M8 redelivery records receipts and cannot
rehydrate/start workflows. A disabled runtime blocks matching mutations.
Workflow lifecycle outbox events are immutable pending subscriber contracts;
external delivery/registration is later work, separate from the M8 record topic.

`WORKFLOWS_ENABLED` defaults true in development/test and false in production.
Production rejects true; public production endpoints await M13. Disable execution
before binary rollback; preserve migrations20/21, pins, receipts, histories and
PostgreSQL backups. Use reviewed forward fixes, never edit immutable history.

Lifetime history is capped at1000 entries per instance, not just paginated in
responses. The cap rejects new mutation/recovery signals. At that boundary pause
the job and use a reviewed operational forward fix; never clear immutable history,
pins, snapshots or receipts to extend it. This bounded preview adapter does not
promise unlimited manual retries. Local callbacks must honor context deadlines.
