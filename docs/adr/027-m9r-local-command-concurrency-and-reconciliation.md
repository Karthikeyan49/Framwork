# ADR027: Independent local writes and durable command reconciliation

Status: implementation in verification. Scope: M9R R4 preview/local database writes.

## Decision

Retain repeatable-read authorization/query semantics. Ordinary commands initialize
the physical anchor if absent and take shared anchor/binding locks. They no longer
increment its counter. Adoption still changes its epoch under an exclusive lock,
so a pre-adoption snapshot fails rather than missing a new mapping. Record CAS
protects full replacement updates; disjoint edits to one version conflict rather
than silently merge. Tenant-inclusive record-link FKs arbitrate insertion/deletion;
the existing JSON reference scan also protects retained legacy references. No
customer DDL or metadata uniqueness implementation is introduced here.

Lock order is command-key advisory lock, binding shared gate, anchor shared gate,
record mutation and reference FK locks. Adoption/cutover owns exclusive gates.
FK-related lock cycles are possible; they must abort, never leave partial writes.
Quiesce old writers during rollout: old binaries still use an exclusive per-write
counter and cannot benefit from this protocol. Keep the counter for adoption;
do not delete it or weaken worker restrictions.

The API buffers a bounded command body and response. At most three local database
attempts fit a four-second total budget, with 5–25 ms jitter. Only proven rolled-back
serialization/deadlock failures and the specific immutable receipt-key collision
permit retry. Every attempt resolves live identity/membership/policy. Callbacks
cannot make external effects. Lost COMMIT replies, transport errors, cancellation,
unknown constraints and external outcomes never authorize an automatic retry.
Exhausted contention and reference conflicts return a defined 409 after rollback
and a fresh authorized denial audit. Safety/availability failures remain 503.

## Receipts and unknown outcomes

Client-created UUID command keys identify tenant + actor + application requests.
The fingerprint binds the entire typed command, entity, executing IR/revision,
physical anchor and dataset/binding. Reuse with different inputs is 409. A
transaction-level advisory lock isolates only that key. It cannot refresh an RR
snapshot: an invisible concurrently committed receipt causes its specific PK
violation, rolling back every effect before a fresh attempt reads the receipt.

Mutation, immutable receipt, business audit, outbox and any local workflow trigger
commit in one transaction. Replays return original ID/version with empty values;
they do not emit another business event or trigger a workflow. Policy/read checks
still execute. Audit of a replay/request is distinct from the one business fact.

An authenticated reconciliation GET returns the current actor's COMMITTED facts
or UNKNOWN; actor-restrictive forced RLS is a second boundary. No customer values
are stored in receipts, and receipts outlive deleted records. UNKNOWN does not
prove abort: a still-running transaction might commit later. Poll/inspect rather
than automatically submitting a new intent. Update/delete/TRUNCATE are denied;
operator deletion is not a recovery mechanism. Worker drift checks include the
new table and worker principals retain no generic data/receipt access.

React generates a fresh UUID once for each attempted intent and records it before
dispatch in an in-memory journal above the workspace component. The journal is
keyed by the authenticated actor and execution scope; other signed-in actors
cannot see or clear prior actors' recovery keys. In-flight workspace selection is
disabled. Disconnect still clears credentials and aborts transport while retaining
the key for the same actor's subsequent connection. Explicit receipt lookup is
provided in the original application/revision. UNKNOWN keeps
writes blocked, including after reload. COMMITTED plus successful fresh read
releases the gate. Keys are displayed before dispatch and after account/workspace
switches for their owner; current retention lasts within the console/tab, not
across browser reloads or restarts. Copy an unknown key before closing the tab.
Legacy callers omitting keys remain compatible but
cannot claim durable reconciliation; the supplied frontend always sends keys.

## Verification and limits

Execute overlapping distinct writes, same-record CAS, concurrent duplicate keys,
real deadlock, serialization/audit rollback, parent/reference races, lost successful
COMMIT response and populated role/actor negatives. Run the 32-client workload
for at least 60 seconds on ADR025's tier without the race detector. Preserve the
500 ms p95 acceptance target and all observed failures. Independent review is
required before accepting this boundary.

This does not implement production activation, governed indexes/unique metadata,
full JSONB schema evolution, external tool delivery, a product DataScope policy or
backup/retention. R1 forbids generic worker mutations; no worker/user throughput
claim invents such a port. Existing reference constraints remain authoritative.
