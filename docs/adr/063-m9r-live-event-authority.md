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
event use must also satisfy it. Already-bound RR/SERIALIZABLE contexts therefore
lose authority after cutover. PostgreSQL does not permit importing a snapshot
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

## Verification

Independently reverify the demonstrated raw RR/imported-snapshot and receipt
write attacks, RC live revocation, event RC/non-certified callbacks and API RR
certification. Test already-bound RR contexts across54→56 cutover, SERIALIZABLE
denial and unchanged workflow RR. Rehearse populated migration preservation,
helper semantic drift and private grants. Retain all initial failures.

After independent fixes close, execute the real600-second outage again at the
corrected checkpoint, `make check test build`, both browser suites, and the
unchanged numeric-read/32-client-write reference gates. R6, other R7 operations,
production runtime and M10/main merge remain outside this bounded correction.
