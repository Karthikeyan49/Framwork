# ADR062 — Separate event publication leases from receipt outage budgets

19 October 2026 (Asia/Kolkata). Recorded before implementation. Scope: R7b
preview event outage tolerance; not M10, production subscribers, workflow lease
renewal, public replay or audit retention. Starting checkpoint `0a4f7d2`, clean
and directly verified on `remediation/m9r-foundation`. Preserve all frozen
migrations through00054 and immutable event/inbox/replay evidence.

Review correction: [ADR063](063-m9r-live-event-authority.md) supersedes the
event-isolation/readiness assumptions below. Final policy is `event-outage-v3`
through additive migration00056; the timing budgets in this decision remain.

## Problem

ADR022 currently uses a five-second publication lease, a five-second receipt
window and five attempts. An absent consumer can therefore dead-letter a
successfully published event in roughly25 seconds. Broker failures and expired
dispatcher leases also spend their budgets too quickly. A dead predecessor
blocks its aggregate; unrelated aggregates must continue to make progress.

## Decision

Forward migration00055 introduces the fixed database-owned contract
`event-outage-v2`: publication lease5 seconds, receipt window15 minutes,
maximum20 attempts, exponential retry500ms capped at60 seconds. Runtime event
readiness must validate the expected contract. No tenant metadata or environment
variable selects these values. Changing them requires a reviewed versioned
decision and forward migration.

The dispatcher still commits a claim before publishing. A new LEASED attempt
expires after5 seconds; its `next_at` includes retry backoff after lease expiry,
so a crashed publisher cannot burn attempts every5 seconds. A positively known
publish failure sets PENDING with the same bounded backoff. A confirmed publish
sets SENT with15 minutes to obtain a committed inbox receipt. Receipt timeout
reissues a new fenced generation; max20 is terminal. Generation/token/audience,
original event bytes/hash, live membership and worker assignment remain required.
No application or worker receives dispatcher/replay permissions.

Known publish failures cannot exhaust20 attempts sooner than about783.5 seconds
even with instantaneous failures; expired publication leases take longer.
Actual network/runtime delays can extend this. A first successfully published
reference tolerates a consumer absence of10 minutes without republication. The
bounded worst case of repeated receipt timeouts is roughly5 hours, plus dispatch
delays; this is not an end-to-end delivery SLA. No universal external exactly-once
execution or automatic side-effect retry is introduced.

The receipt deadline is also the existing event capability's expiry. Extending
it deliberately permits the assigned worker to resolve that exact immutable
event during the receipt window; it does not extend workflow/activity leases or
grant record DML. Each trusted Go consumer callback remains a3-second bounded
transaction with2-second SQL/500ms lock budgets, live authorization and per-use
RLS. These are not a new raw-credential processing-lease protocol or proof of
liveness against a compromised process. All
current wrong-tenant/audience/token/generation/revocation tests must remain.

## Compatibility and recovery

Replace only dispatcher SQL functions and the attempt-count constraint; retain
their signatures, routing schema1.1 and event envelope schema1.0. Old Go
dispatcher/consumer callers obtain the new timing through the same SQL ports;
no IR, active release or in-flight workflow is recompiled. New binaries fail
event readiness before migration00055.

Do not update existing delivery rows, tokens, deadlines, attempts or states in
the migration. Already-issued deadlines remain until completion or expiry; a
later claim uses the new policy and advances its fence. Existing DEAD and
LEGACY_PAUSED rows do not resume automatically. Operator replay keeps its
existing expected-generation/reason and immutable-history requirements; least-
privilege replay redesign remains a separate R7 gate. Rollout quiesces owned
dispatchers before applying the forward migration and checks readiness before
restarting. On failure stop delivery and forward-fix; never erase receipts,
reset generations or automatically revive a dead aggregate. No destructive down.

## Required verification

First execute an actual30-second no-consumer acceptance against the old
checkpoint, expecting FAIL, then the same test against the fix, expecting PASS.
Use disposable populated databases with effective driver/database/role checks,
real Redis publication/receiver and a fixture-owned sender ACL. Revoke XADD only
from that owned ACL to exercise real broker rejection while another event waits
for its consumer. No shared ACL, service or customer stream is disabled.

Add an explicit `make test-event-outage` target: at least600 seconds of actual
monotonic and PostgreSQL time, with the dispatcher running, consumer absent and
the owned sender denied. No timestamp manipulation or shortened policy in this
rehearsal. Verify first event stays SENT at one attempt; a successor stays
unclaimed; broker failures remain recoverable; restoring the owned sender and
starting the receiver produces exactly one local effect/audit/inbox per event,
preserving hashes and ordering. The opt-in wall-clock test has its own15-minute
suite deadline, not enlarged individual transaction/query limits.

Fast populated SQL tests exercise all20 backoff bounds, terminal state, lease
and receipt expiry, stale finishes, replay, competing claims, live revocation,
raw role denials and migration preservation of existing state/evidence. Operator
timestamp acceleration is permitted only in explicitly labeled boundary tests,
never presented as the real outage rehearsal. Readiness must deny a missing or
changed policy contract. Preserve previous M8 crash, unknown-COMMIT, broker-loss,
deduplication and raw-worker tests, updating only intentional old attempt-count
expectations.

Commit stable work for independent adversarial review with an exclusive execution
window. Fix/reverify findings; final `make check test build`, both application
and local sign-in browsers, and unchanged numeric-read/32-client-write reference
targets must pass. Broader R6, remaining R7, external identity deployment and
production runtime remain open. No M10/main merge or production-ready claim.
