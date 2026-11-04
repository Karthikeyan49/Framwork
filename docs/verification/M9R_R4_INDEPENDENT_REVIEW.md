# M9R R4 independent review

Date: 2026-10-14 UTC. Independent Codex reviewer.

**Verdict: PASS for bounded local-command R4 at `94932e3`.** The HIGH rollback
and MEDIUM recovery-key findings at published `a5cbafd` are fixed and independently
reverified. The isolated ordinary32-client/60-second write gate passed its unchanged
500ms target with zero errors. No confirmed unresolved BLOCKER, HIGH or MEDIUM
finding remains in this reviewed boundary. Publication of the earlier partial
checkpoint was not acceptance. Full R2, the R3 HTTP read floor, metadata uniqueness
and production remain open.

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

### HIGH, CLOSED: a typed rejection after mutation could commit without required facts

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

The corrected outer local-command boundary forces rollback even when a typed
HTTP rejection has no service error. Fresh live identity/membership resolution
and denial audit occur after rollback. The corrected executor probe returned404
with records/receipts/outbox all zero. The new repository test additionally uses a
real restrictive SELECT policy under the normal application role, covering both
keyless and receipted commands and exactly one fresh denial fact.

### MEDIUM, CLOSED: tenant navigation lost an in-flight intent inside the same login

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

Corrected UI retains the key before dispatch in AccessConsole, above workspace
unmount, indexed by trusted actor plus execution scope. In-flight tenant selection
is disabled; disconnect still clears credentials. Unknown keys survive same-tab
disconnect and scope changes while another actor cannot see them. Browser/tab
restart durability is still outside this in-memory journal. Independent navigation
re-verification and the repository's account/disconnect/UNKNOWN→COMMITTED tests
passed without another write.

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

## Corrected batching/admission review and executed evidence

Final stable source: `94932e3108d9afea56c8dd1f95189d57e5384ff2`, following the
two fault fixes in05aa14c. Reviewed ADR028/029, bounded audit batches and API-owned
FIFO admission. Applied migration30 was not rewritten. All repository edits by
this reviewer remain limited to this report; temporary copied access.go has only
a fault-observer seam, not a production fix.

Every decision still governs immediately and remains a separate evidence row.
Postgres batching validates the complete set before SQL, bounds8192 entries and
uses128-row parameterized chunks. Error/panic paths do not issue further flushes
into aborted transactions; failures propagate to outer rollback. Legacy writer
fallback preserves errors. FIFO is per registered API/pool, keeps the existing
ten-connection limit and128 queued-request bound, and releases after transaction/
connection cleanup. Retries rejoin; authentication and live membership resolve
after admission. It is not distributed capacity coordination or an abuse limiter.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r4-review/overlay.json -run '^TestIndependentR4Typed|^TestM9RFinalReadRejection|^TestM9RQueued' -v ./tests/integration
```

**PASS6.991s at94932e3:** independent late-read fault; real restrictive-RLS legacy
and receipted rollback/fresh denial; queued membership revoked before admission,
then403 with no facts and no leaked capacity. Earlier independent correction at
05aa14c passed4.727s, and its own navigation probe passed2.38s.

The same temporary frontend command listed above, updated to require retention
before dispatch, disabled in-flight tenant selection, then unknown transport
settlement and A→B→A under the same login: **PASS2.53s at94932e3**.

```sh
./scripts/go.sh test -race -count=1 ./internal/audit ./internal/platform/postgres
npm --prefix apps/web test -- --reporter=dot
```

**PASS:** audit1.022s and postgres1.235s; FIFO order/overflow/idempotent release,
50 simultaneous cancellation/grant cases, evidence parameterization, complete
prevalidation/budget and retry classification. **PASS:** frontend29 tests,5.73s,
including same-tab disconnect, another actor's isolation, returning owner recovery,
UNKNOWN retaining the gate and COMMITTED plus fresh read releasing it.

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r4-review/overlay.json -run '^TestIndependentR4(Panic|Receipts|Fallback)|^TestM9RAudit' -v ./tests/integration
```

The temporary overlay initially failed to build because its unused app variable
remained; corrected only that fixture. Next run **FAIL6.349s as a proposed panic
assertion**: HTTP middleware recovered the injected panic to500, so a caller-level
recover did not observe it. The other four roots passed. Corrected assertion
requires500, zero facts, released gate and a successful fresh command afterward.
Rerun **PASS7.861s, five roots**:

- Panic after a real business-audit INSERT rolls back record/receipt/audit/outbox,
  leaves active/queued capacity zero and permits a successful follow-up.
- A column-only worker receipt SELECT grant makes role readiness deny. Spoofed
  valid actor/tenant GUCs still expose zero receipt rows through forced RLS.
- A legacy fallback writer fails after its130th real INSERT of257; the owned
  transaction leaves zero evidence rows.
- Real257-entry batching preserves257 distinct decision identities; a foreign
  tenant in the final chunk fails42501 and rolls back all earlier chunks.
- A poisoned batch after mutation rolls back every command fact and leaves its
  receipt UNKNOWN.

These are bounded selected fault/unit/frontend checks, not a claim that this
reviewer reran the complete repository or all M8/M9 integration tests at this head.

## Independently executed write performance gate

After all correctness processes exited and root kept CPU/database tests idle:

```sh
make test-write-performance
```

This expands to the ordinary, non-race command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -run '^TestM9RIndependentWritePerformanceFloor$' -v ./tests/integration
```

**PASS61.928s at94932e3.**32 clients ran60.229853704s, completed7643 updates, and
returned zero errors. Exact returned IDs/versions and final version sum plus
receipt/business-audit/outbox counts passed. Verified PostgreSQL reference:
4 CPU quota,8GiB memory, swap disabled, pinned image
`sha256:29754c7520f4b9654cf8ff98e19ac73b88e255c3a53f6236ebe9d52249bb6df3`.
Pool/admission limits and acceptance targets were unchanged.

| Independent write workload | n | p50 ms | p95 ms | p99 ms | Throughput/s | Result |
|---|---:|---:|---:|---:|---:|---|
| Distinct-record HTTP writes,32 clients |7643|242.985|340.575|392.162|126.90|PASS p95≤500ms,0errors|

Database pool wait count/duration were zero. Marginal phase p95 values were
request admission237.995ms, connection acquisition0.005ms, role checks13.950ms,
transaction body99.885ms and commit15.695ms. These are separate distributions;
adding them does not reconstruct overall request latency.

The workload uses real auth/policy/RLS/audit/receipt/outbox with32 records and
one synthetic actor/application in an httptest server. It does not measure
production ingress/TLS, complex metadata mixes, cold-cache/open-loop traffic,
long-duration capacity or cross-process fairness.

## Prior failures retained and remaining gates

The implementer reported its ordinary32-client,60-second write gate **FAIL62.688s**,
duration60.260s, one503. Detailed latency/cause were not emitted before failure.
This reviewer did not independently execute that original failure and does not
attribute its503 to a specific cause. Later root observations, also distinct from
the independent gate above, remain retained:

- Diagnostic pre-batching FAIL61.838s:6471 successes,0errors, p95=701.506ms.
- Batch-only FAIL62.235s: p95=788.839ms.
- Phase-instrumented FAIL61.646s: p95=839.408ms; acquisition p95=717.820ms,
  role20.666ms/body184.554ms/commit21.535ms as separate distributions.
- Root FIFO gate PASS61.879s:8500 writes/60.196s,0errors, p95=297.404ms,
  p99=337.961ms,141.20/s and exact counts passed.

The code corrections, measured batching/FIFO change and independent passing gate
support acceptance of this bounded R4 floor. Earlier observations are not erased
or relabeled; no universal availability/capacity promise is inferred.

Corrected independent faults, queued live revocation, receipt-grant negatives,
frontend re-verification and the isolated ordinary write gate are complete within
the stated scope. NOT RUN by this reviewer: an exhaustive per-attempt revocation
campaign, complete repository/remote-CI/browser suite, production deployment or
process-isolation acceptance. No metadata unique semantics is inferred.
Full schema evolution/new workflow data
pins and the failed R3 HTTP read target remain separate gates.

Root's current R3 read gate also remained FAIL41.143s, HTTP p95=898.985ms>750ms
despite passing SQL gates; that is not closed by the write result. No reviewer
R3 rerun was performed here. All owned processes finished and fixture cleanup ran;
CPU/DB ownership was released. No shared service was reset.
