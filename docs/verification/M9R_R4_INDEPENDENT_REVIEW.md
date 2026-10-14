# M9R R4 independent review

Date: 2026-10-14 UTC. Independent Codex reviewer.

**Current verdict: PARTIAL at published `a5cbafd`.** Independent probes confirmed
a HIGH late-rejection rollback invariant failure and a MEDIUM same-login tenant
navigation loss of an uncertain command key/gate. Corrections, re-verification
and the write performance gate are pending. Publication is not acceptance. Full
R2, the R3 HTTP read floor, metadata uniqueness and production remain open.

## Reviewed boundary

Stable source: `a5cbafd7d185a5e701284777767e07ed0c1471fe`. Read ADR027, R4 report,
migration30, receipts/fingerprints, shared anchor/binding locks, retry/transaction
ownership, API policy/response mapping and React command/reconciliation state.
Implementation and applied migrations were not edited by this reviewer. This
report is the sole repository deliverable. Temporary fixtures/overlays under
`/tmp/r4-review` used owned, driver-target-verified disposable PostgreSQL and a
jsdom frontend harness. No shared service restart or Redis reset occurred.

Receipts contain actor-owned committed identifiers/version, not a customer-value
snapshot. Actor-restrictive forced RLS complements the central policy. Command
fingerprints bind request and execution/physical/binding scope. Known-abort retry
classification excludes transport/lost-COMMIT outcomes. Current shared gates
replace per-write anchor increments; existing CAS, reference FKs and legacy JSON
reference scans remain. These contracts are not a production or metadata-unique
capability.

## Confirmed pre-fix findings

### HIGH: a typed rejection after mutation can commit without required facts

`data.command` writes records/links before its final `current()` read.
`receiptedCommand` inserts the receipt only after that function succeeds.
`executePreview` maps typed ErrNotFound/ErrDenied/ErrConflict to a4xx response
with nil error. The outer transaction then commits instead of rolling back.

The first attempted proof used a flipping ScopePolicy. It returned200 because
the API caches scope by action/entity/field. **That is not a demonstrated current
live-policy flip defect.** The actual reproducer used a temporary executor fault
seam in a copied `access.go`: it executes the real normal-role INSERT, then
restricts only the final record read with `AND FALSE`. No production source,
authorization or transaction semantics were changed by the seam. It injects a
late no-row result to exercise the typed ErrNotFound path.

Executed result: HTTP404, **one committed business record, zero receipts and
zero outbox intents**. This proves the rollback invariant fails under a late-read
fault; it does not prove an untrusted actor can make Native policy do this.
Every local-command rejection must roll back all attempted business facts before
mapping its HTTP response and persisting fresh authorized denial evidence.

### MEDIUM: tenant navigation loses an in-flight intent inside the same login

The outer AccessConsole tenant selector remains enabled during command submission.
Changing tenant unmounts ApplicationWorkspace/EntityPage and aborts its controller.
The command catch ignores CommandOutcomeUnknown when that controller is aborted;
the key is only retained after the unknown exception, not before dispatch.

The independent jsdom probe dispatched a POST, switched A→B→A under the same
connection/token without disconnect or browser reload, loaded A again, and found
Create enabled with no retained pending command key. The first key was not shown
before switching, so the documented instruction to copy an unknown key before
ending a session cannot recover this in-flight transition. A server might still
commit the first intent; this test simulated the transport boundary and did not
claim an actual duplicated PostgreSQL write.

Retain the key above the scope-unmount boundary before dispatch or prevent unsafe
navigation while the write/unknown outcome is unresolved. Distinguish deliberate
cross-browser/session durability limits from this same-login navigation behavior.

## Independently executed commands

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(Command|IndependentWriters|KnownSerialization|UnknownCommand|ReceiptMutation|ConcurrentSameKey|ActualDeadlock|ConcurrentReference)' -v ./tests/integration
```

**PASS16.757s, eight roots:** owned replay/conflict/isolation/immutability,
concurrent same-key deduplication, actual server deadlock, parent/reference races,
overlapping independent writes and CAS, serialization rollback, actual successful
COMMIT-reply loss/reconciliation, and receipt/audit/outbox rollback.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r4-review/overlay.json -run '^TestIndependentR4Typed' -v ./tests/integration
```

First attempt with only flipping ScopePolicy: **FAIL1.426s as a proposed proof**,
because it returned200; the cached scope prevented the hypothesized late flip.
With the final-read executor fault seam: **FAIL2.336s**, reproducing HTTP404 with
committed_records1/receipts0/outbox0. The seam, test and overlay are in
`/tmp/r4-review/{access.go,probe_test.go,overlay.json}`.

```sh
npm --prefix apps/web exec -- vitest run --config /tmp/r4-review/vitest.config.mts --reporter=dot
```

**FAIL2.92s**, one independent frontend test: the Create button was enabled after
same-login tenant switching despite an unresolved in-flight intent. The temporary
test/config are `frontend.test.tsx` and `vitest.config.mts`; a temporary node_modules
symlink supplies the repository's managed dependencies. Synthetic credentials
only were used; no real token or authorization-header output was produced.

## Performance and current limits

The implementer reported its ordinary32-client,60-second write gate **FAIL62.688s**,
duration60.260s, one503. Detailed latency/cause were not emitted before failure.
This reviewer has not independently executed that workload yet and does not
attribute the503 to a specific cause. Root retains the failure and is preparing
bounded safe diagnostics; neither a threshold increase nor selection of a later
passing run can erase it.

Corrected independent faults, live revocation/replay/receipt-grant negatives,
frontend re-verification and an isolated diagnostic ordinary write workload are
pending. No full repository/remote-CI/production/process-isolation acceptance or
metadata unique semantics is inferred. Full schema evolution/new workflow data
pins and the failed R3 HTTP read target remain separate gates.
