# R7b independent adversarial review

19 October 2026 (Asia/Kolkata). Reviewed stable implementation checkpoint
`1fdefa194a178e0ab087589596350c2ecc369e55`. The implementation agent paused
source edits, builds and database/Redis workloads during the exclusive review
execution window. Reviewer changes are only the independent integration test
file and this report. No production source, migration, dependency, credential,
customer data, main branch or M10 change.

**Final bounded review result: PASS at product checkpoint `031c8a1`.** All three
demonstrated findings are independently closed within the documented maintenance
cutover and credential threat model. Final root full/browser/reference gates and
the repeated real600-second rehearsal remain required. Historical failed
checkpoints and fixture mistakes are preserved below.

**Initial review result: FAIL — one MEDIUM authorization finding and one LOW
deployment-readiness finding require fixes and independent re-verification.**
The ten-minute outage rehearsal passes at this checkpoint, but that evidence
does not close the raw-credential revocation regression described below.

## Scope and threat model

Read AGENTS, the two-agent protocol, ADR062, the implementation report, migration
00055, event delivery/readiness contracts, worker job binding and authority,
transaction snapshot certification and relevant retained M8/M9R fixtures. Tests
use populated disposable databases with effective driver/database/role checks;
no shared customer database, Redis ACL or customer stream is modified.

The authorization attack uses only the worker credential and its legitimately
issued exact event pointer. Operator access is used solely to prepare fixtures
and revoke membership, as an administrator would. The operator-drift attack is
separate: it deliberately replaces a private helper using operator privileges.
It is not a runtime worker privilege escalation. No broad record DML, discovery
of another tenant or arbitrary external effect was demonstrated.

## IR-R7b-01 — MEDIUM: a raw snapshot extends revoked event authority

The new receipt window also extends the existing event capability from five
seconds to fifteen minutes. `control.worker_authority()` reads live membership
through the caller's transaction snapshot. A raw worker may select REPEATABLE
READ, bind the exact published event, retain that snapshot, and continue reading
the envelope and inserting its inbox receipt after membership is revoked.
`clock_timestamp()` still expires authority at the receipt deadline, but until
then the membership check can observe the old active membership.

Executed negative controls against historical schema54 and current schema55:

| Populated attack | Actual result |
| --- | --- |
| Original schema54, revoke membership, keep raw RR transaction open for5.2s | PASS: envelope visibility0 and inbox INSERT denied with SQLSTATE42501 after the old receipt deadline |
| Schema55, same credential/fixture/sequence | FAIL: envelope visibility1 and inbox INSERT accepted after revocation; transaction rolled back |
| Export old RR snapshot, revoke membership, wait5.2s, import into a new RR transaction | FAIL: exact event binding succeeds, envelope visibility1 and inbox INSERT accepted; new transaction age0.021s |
| Raw READ COMMITTED positive control, revoke membership between statements | PASS: initial envelope visibility1, then0; inbox INSERT denied42501 |
| Raw READ COMMITTED, explicitly accelerated receipt deadline inside an already-bound transaction | PASS: visibility changes1→0 and inbox INSERT denied42501 after actual350ms wait |

The imported-snapshot control demonstrates why checking only transaction age is
insufficient. It is not evidence that a proposed age check was actually deployed
and tested. The raw tests roll back their writes; all checked durable local
effect/audit/inbox counts remain zero. Fresh normal consumer calls also deny the
revoked membership, so ordinary short Go transactions do not reproduce the raw
credential failure. This is an exact-event revocation boundary, not a claim that
workers recovered the earlier unrestricted cross-tenant data access.

Recommended bounded remediation: database-enforced event-only READ COMMITTED
authority at binding and every use; already-bound RR contexts must fail closed
after cutover. Use READ COMMITTED for trusted event job callbacks and event
authority checks. Keep API and workflow REPEATABLE READ unchanged. An RC executor
must not satisfy `postgres.RuntimeSnapshot`, which certifies a shared snapshot
for aggregate execution. The independent callback regression currently confirms
events use `repeatable read` with `certified_snapshot=true`; the ordinary API
positive control must remain RR/certified after the event change. Preserve the
exact token/generation/audience/resource fence, transaction ownership and3s/2s/
500ms budgets. No existing pushed migration may be edited.

## IR-R7b-02 — LOW: helper shape does not certify its retry behavior

Readiness checks the private `control.event_retry_delay(integer)` owner,
signature, volatility, strictness, return type, search path and absent runtime
EXECUTE grants. An operator can replace its body with a zero interval while
retaining all these properties. All three runtime readiness roles still report
healthy, yet the populated dispatcher exhausts20 known publication failures and
reaches DEAD in206ms. The independent test restores the original definition in
cleanup. Zero durable consumer effects/audit/inbox are committed.

This is a deployment-drift false positive, not an attacker with ordinary runtime
credentials changing private SQL. Recommend a narrow private semantic probe that
validates the actual helper outputs for attempts1–20, invalid inputs and NULL,
without granting direct helper execution to runtime roles. Go readiness must
deny a false probe. A privileged operator can also change a probe, so this does
not prove integrity against an all-powerful administrator. Any SQL correction
requires a forward-only migration00056 or later; pushed00055 remains frozen.

## Executed commands and results

All commands ran from `/workspace/Framwork`. Ignored logs are under
`.local/reports/r7b-independent-*`. FAIL is preserved rather than represented as
completion. No default test SKIPs were used.

```sh
./scripts/go.sh fmt ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReview' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^Test(R7bReviewReadCommitted|R7bReviewEventCallback|R7bForward|R7bPublication|R7bReadiness|M8)' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReviewEventCallbackUsesUncertifiedReadCommitted$' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^Test(M9RConsumerCannotUseGenericBusinessMutationPort|M9RPopulatedTenantsAndAuthenticatedAudience|M9RSQLWorkerCapabilitiesAndLiveRecordScopes|M9RSQLEventCapabilityRevocation|M9RExactCapabilityMaskedSourceAndTransactionLifetime)$' -v ./tests/integration
```

| Execution | Actual result |
| --- | --- |
| Original reviewer initial probes | FAIL11.242s: retry semantic drift and revoked RR authority; raw receipt-expiry case PASS |
| Original reviewer schema54/imported-snapshot controls | FAIL21.865s: schema54 subcase PASS, current and imported-snapshot attacks FAIL. An initial diagnostic was placed before the wait; corrected logs below report actual post-wait values |
| Independent confirmation with inbox-write probes and corrected diagnostics | FAIL25.357s: schema54 visibility0, current visibility1/accepted inbox, imported snapshot age0.021s/accepted inbox; raw expiry PASS |
| Selected retained M8/R7b plus new RC/callback probes | FAIL57.618s:18 groups PASS; one new callback regression FAIL. All retained crash/unknown-COMMIT/order/replay/broker-loss/forward-manifest/readiness groups PASS; RC live-revocation control PASS1.73s |
| Corrected callback fixture only | FAIL2.263s solely because current event callback is RR/certified. Earlier selected-run fixture replaced the original local effect callback and also reported effects0; it now wraps that callback and preserves exact one-effect/audit/inbox checks |
| Five retained raw-worker resource/SQL-policy groups | PASS11.898s; no failures/skips: exact tenant/job/release/generation/token/kind, populated tenant/audience, SQL event/worker revocation and generic business-mutation denial |

The implementation agent's repeated actual `make test-event-outage` log was
inspected, not rerun by this reviewer: PASS609.451s, actual600-second absence,
recovery total606.726s, exactly3 completed/inbox/effects/audits/immutable intents,
19 retained attempts and two publication-before-confirmation redeliveries. Its
first real600-second run failed during recovery with an unclassified original
error. The later controlled `ErrNotReady` reproduction is separate evidence and
does not conclusively classify that original failure.

Full `make check test build`, both browser suites and unchanged numeric-read/
32-client-write reference gates are NOT RUN by this reviewer and remain the
implementation agent's final gates. The independent review remains FAIL until
the two findings are fixed and independently reverified. No production-readiness,
main merge, broader R7/M9R completion or M10 authorization follows.

## Corrective review at46646b4 — fresh-entry fixes pass; cutover remains open

ADR063 was committed before correction at`a98cb40`. Independent review of
`46646b4` verifies that migration00056 rejects fresh non-RC event authority and
that event callbacks use RC without a shared-snapshot certificate. The original
raw RR/imported-snapshot attacks now deny, RC live revocation and receipt expiry
still pass, ordinary API callbacks remain RR/certified, and the independent
workflow ticket/callback control remains RR/certified. RR, SERIALIZABLE and READ
UNCOMMITTED entry all deny; a bound RC transaction cannot switch to RR. The
semantic policy probe now denies the zero-delay helper and changed values for
inputs0,-1,21,2147483647,-2147483648 and7; restored helpers recover readiness,
and all three runtime roles still cannot invoke the helper directly.

IR-R7b-02's demonstrated semantic false positive is closed at this checkpoint.
IR-R7b-01 is **partially closed**, because independent execution found that the
claim about already-bound contexts across a live cutover is false:

- At schema55, bind a raw RR event transaction and prepare/read the envelope.
  Keep it open while applying56. Eight durable-table manifests remain unchanged.
- After cutover the old transaction reads visibility1 through both its retained
  prepared statement and a new envelope statement; a new direct authority query
  also returns1. `pg_get_functiondef` sees the new RC guard, so source inspection
  alone does not prove that retained execution paths have adopted it.
- A new inbox INSERT after cutover denies42501; write survival was **not**
  demonstrated. A fresh backend importing a snapshot exported before cutover
  also denies binding. Do not generalize the retained-reader result to all fresh
  callers or unrestricted data access.
- A fixture-only relation-policy replacement cannot safely be assumed online:
  an open reader causes a server lock_timeout55P03; the same DDL succeeds after
  the reader drains. An earlier client-deadline diagnostic incorrectly treated
  cancellation as known abort, then encountered duplicate-policy42710. That
  fixture failure is retained; the corrected probe uses a bounded operator
  transaction, exact server55P03 and rollback before the positive control.

The supported migration must require a proven drain/cutover barrier rather than
claim automatic revocation of all old execution plans. Root plans a new ADR
amendment, forward migration00057 with a NOWAIT barrier and direct restrictive
RC event policies, and readiness verification of those policies. This is a
proposed remedy, not executed acceptance. Do not edit pushed55/56 or silently
terminate unknown sessions. Stop delivery and report a failed barrier; an
operator must complete the reviewed drain before retrying.

### IR-R7b-03 — LOW: readiness invokes an unsafe helper before rejecting it

The new SECURITY DEFINER policy probe executes its private dependency before Go
checks that dependency's immutable/owner/ACL contract. In an explicitly
operator-owned deployment-drift fixture, a VOLATILE helper keeps the normal
timing outputs but inserts into a private marker table. All runtime readiness
roles eventually deny, yet their calls commit75 marker writes through the
schema-owner probe. No customer table or durable consumer effect is modified.

This is operator deployment drift, not ordinary runtime credentials replacing
SQL. Check the policy's own trusted metadata and the dependency's shape/owner/
private grants **before** invoking the semantic probe. It cannot prove integrity
against an administrator changing code between the checks. Root plans a Go-only
ordering correction; it remains unverified at this checkpoint.

Additional executed commands:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestR7bReview' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReviewAlreadyBoundSnapshotLosesAuthorityAtCutover$' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReview(AlreadyBound|PreCutover)' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReview(AlreadyBound|PreCutover|RelationPolicy)' -v ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReviewRelationPolicyRequiresDrainedReaders$' -v ./tests/integration
```

| Corrective execution | Actual result |
| --- | --- |
| All independent R7b groups at46646b4 | FAIL28.789s: original attacks and new API/workflow/closed-isolation/semantic-variant controls pass; retained-reader cutover and unsafe invocation fail |
| Isolated retained-reader confirmation | FAIL2.042s: prepared1/fresh1/direct authority1 despite visible new guard; inbox INSERT denied42501 |
| Retained-reader and pre-cutover export controls | FAIL6.209s: reader remains; exported-before/fresh-import bind denial PASS1.53s; initial fixture-only DDL hit its client deadline |
| Final reader/export/lock command before fixture correction | FAIL6.093s: reader persists, exported snapshot denial PASS1.46s; lock fixture fails42710 from unconfirmed earlier DDL cancellation |
| Corrected server-lock/drained-reader control | PASS3.302s: exact55P03 under held reader, known rollback, same DDL succeeds after drain |
| `make docs-verify` and `git diff --check` at initial review commit | PASS;84 retained and43 V3 checksums |

**Status at46646b4: FAIL pending safe cutover and readiness ordering
corrections.** No final full/browser/reference gates or repeated600-second test
at the corrected source have been run by this reviewer.

## Final independent closure at031c8a1

The ADR063 cutover correction was committed before code at `3840245`.
Migration00057 and the Go readiness ordering change are reviewed at `031c8a1`.
Only new independent tests and this report changed during this final exclusive
window; no production code or historical migration was edited by the reviewer.

| Finding | Independent disposition |
| --- | --- |
| IR-R7b-01 MEDIUM, stale raw event authority | CLOSED for the supported drained cutover. Fresh RR/SERIALIZABLE/RU and imported snapshots deny. Current RC observes live revocation; API/workflow RR certificates remain unchanged. The old55→56 retained-reader behavior stays in the regression as a positive historical control. Actual57 refuses that active reader with55P03, leaves no applied57 marker/partial policy or changed durable-table manifest, then succeeds after the owned transaction drains. Normal consumption of the same immutable event commits exactly one effect/audit/inbox |
| IR-R7b-02 LOW, retry semantic readiness drift | CLOSED. Zero-delay and changed valid/invalid input bodies deny all three runtime readiness roles; restored helper recovers readiness, and private EXECUTE remains denied. The operator can still change both the code and its probe; protection against an all-powerful operator is not claimed |
| IR-R7b-03 LOW, unsafe dependency invoked before denial | CLOSED. The same VOLATILE marker-writing helper now causes readiness denial before the owner-executed probe runs; marker writes remain zero for all three roles |

Two additional independent populated groups pass:

- Readiness denies eight isolation-policy drift variants: missing envelope or
  inbox policy, permissive mode, wrong or extra role, `USING true`, `WITH CHECK
  true`, and a SELECT-only command. Exact restoration recovers all three runtime
  roles after every fault.
- With the product's direct restrictive policies intact, an operator-only
  fixture removes the function-level isolation condition to simulate old
  authority behavior. Raw RR binding and direct authority return the positive
  control, but envelope visibility is zero and inbox INSERT denies42501.
  Restoring the function permits normal RC consumption with exactly one local
  effect/audit/inbox. This is defense in depth against the demonstrated stale
  function path, not a runtime role obtaining ALTER privileges.

Executed final command:

```sh
./scripts/go.sh fmt ./tests/integration
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^Test(R7bReview|R7bForward|R7bPublication|R7bReadiness|M8)' -v ./tests/integration
make docs-verify
git diff --check
```

**PASS92.968s:32 top-level groups, zero failures and zero skips**, including the
retained crash, unknown-COMMIT/no-ACK, ordering, deduplication, lease fencing,
dead-letter/replay, Redis stream-loss/pending recovery, running-service and trace
correlation cases; independent raw credential attacks; exact readiness drift;
and populated54→57 forward preservation. The forward rehearsal applies three
additive migrations and repeats idempotently without changing manifests of
deliveries, attempts, envelopes, inbox, replay, business records, outbox or audit.
Final formatting, documentation checksums84+43 and whitespace checks PASS.
Ignored detailed evidence is `.local/reports/r7b-independent-final-closure.log`.

Migration57 is explicitly a maintenance operation. Its shared worker-context
lock requires draining all owned worker jobs, including workflows, along with
API event emitters, consumers and dispatchers. A failed NOWAIT barrier is a
stop-and-report condition; do not silently kill unknown sessions or reset
receipts/generations. No successful online migration over an active old reader
is claimed. Raw-process lock/liveness guarantees and instantaneous cancellation
of an already-running statement remain outside this bounded contract.

This review does not rerun the final-source600-second rehearsal, full build/test
suite, browser suites or unchanged reference-capacity targets; those remain the
implementation agent's gates. All prior failed runs remain historical evidence.
R6, other R7 operations, production runtime, broader M9R, external identity
deployment, main merge and M10 remain outside this bounded PASS.
