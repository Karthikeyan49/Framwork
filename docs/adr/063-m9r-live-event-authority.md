# ADR063 — Preserve live event authority across a longer receipt window

19 October 2026 (Asia/Kolkata). Recorded before the corrective implementation.
Independent review of `1fdefa1`, committed at `7bd6938`, demonstrated two R7b
findings. This supplements ADR062; migration00055 is pushed and remains frozen.

## Problem and decision

An event receipt deadline also bounds its exact worker capability. Extending
that deadline from five seconds to fifteen minutes permits a raw REPEATABLE READ
worker transaction to retain revoked membership from its old snapshot. The
reviewer read that event and inserted its inbox receipt after revocation. An
exported snapshot imported into a fresh transaction reproduces the problem, so
a transaction-age check alone is insufficient. Broad record or cross-tenant
access was not demonstrated.

Forward migration00056 requires READ COMMITTED in the event branch of private
`control.worker_authority()`. Binding already calls this function; every later
event use must also satisfy it. Fresh RR/SERIALIZABLE contexts fail closed.
PostgreSQL does not permit importing a snapshot
into READ COMMITTED. Live revocation is observed on subsequent statements, not
an instantaneous cancellation of a statement already executing. Exact audience,
job, generation, token, actor and receipt-expiry fences remain.

Trusted event job callbacks and event ticket-authority checks use an explicit
READ COMMITTED transaction entry point. API/data and workflow callbacks keep
REPEATABLE READ. An RC executor is not certified by `postgres.RuntimeSnapshot`;
SQL aggregate/index paths must never infer a shared snapshot from it. Preserve
the existing transaction ownership, atomic inbox/effect/audit/completion, unique
inbox deduplication, row locks and 3s transaction/2s SQL/500ms lock budgets. No
database processing lease or raw-process lock/liveness guarantee is introduced.

The reviewer also replaced the private retry helper with a zero-delay body
while preserving its owner, shape and grants. Readiness passed even though
twenty known failures reached DEAD in206ms. Migration00056 changes the existing
no-argument policy function to a schema-owner SECURITY DEFINER probe. It returns
the fixed `event-outage-v3` contract only if all actual helper outputs1–20,
out-of-range inputs and NULL match the fixed schedule. Runtime roles still
cannot invoke the helper directly. This detects the demonstrated operator
deployment drift; it cannot certify integrity against an administrator who can
also replace the probe. Publication/receipt timing and retry budgets stay as
defined by ADR062.

## Compatibility and recovery

Keep SQL signatures, event envelope1.0, routing1.1 and immutable evidence.
Migration00056 replaces functions only; it does not rewrite data, attempts,
deadlines, tokens, inboxes, audit, IR or workflow plans. A new binary denies the
old policy. Quiesce owned consumers and dispatchers, apply forward migrations,
verify readiness, and restart the compatible binary. A pre-correction RR event
consumer fails closed after migration and must be upgraded; do not restore its
authority by weakening the SQL gate. Workflow/API isolation is unchanged.
On failure stop owned delivery and forward-fix. No destructive down or erased
receipts; existing DEAD/LEGACY_PAUSED rows stay paused.

## Cutover correction recorded before migration00057

Independent re-verification of `46646b4`, recorded at `418e78c`, found that an
already-bound RR backend can retain old SQL read plans across function-only
replacement. Prepared/fresh envelope reads and a cached authority call stayed
visible; a new inbox INSERT was denied. A fresh transaction importing a snapshot
exported before the replacement was denied. Do not claim automatic online
fencing of old contexts from migration00056.

Forward migration00057 first acquires transaction-held NOWAIT ACCESS EXCLUSIVE
locks on the private worker-context and event envelope/inbox relations. An
active old reader/binder causes migration failure, with no partial policy or
data change. Operators must quiesce owned API event emitters, all workers
(including workflows), and dispatchers, drain their database transactions, then
retry. The worker-context lock is shared. Never silently kill
unknown sessions or infer rollback from a cancelled client response. This is a
maintenance cutover, not an online revocation promise.

Once the barrier succeeds, add direct restrictive worker READ COMMITTED RLS
policies on both event tables, with identical USING/WITH CHECK expressions.
New readiness must inspect their exact role/mode/expressions. Table locks remain
until migration commit, so old readers cannot overlap successful installation.
Retain the function-level guard and v3 timing/probe contract. No row or evidence
is rewritten. Historical migrations55/56 stay frozen.

The review also demonstrated75 marker writes when a structurally unsafe
VOLATILE helper ran through the owner-executed probe before readiness denied
it. Check helper shape/owner/private grants before invoking the semantic probe.
This LOW operator-drift correction changes Go check order only.

## Verification

Independently reverify the demonstrated raw RR/imported-snapshot and receipt
write attacks, RC live revocation, event RC/non-certified callbacks and API RR
certification. Test that an already-bound RR context blocks migration57 with
SQLSTATE55P03, manifests/policies remain unchanged on failure, and draining then
retrying installs the guard. Test SERIALIZABLE denial and unchanged workflow RR.
Rehearse populated migration preservation,
helper semantic drift and private grants. Retain all initial failures.

After independent fixes close, execute the real600-second outage again at the
corrected checkpoint, `make check test build`, both browser suites, and the
unchanged numeric-read/32-client-write reference gates. R6, other R7 operations,
production runtime and M10/main merge remain outside this bounded correction.
