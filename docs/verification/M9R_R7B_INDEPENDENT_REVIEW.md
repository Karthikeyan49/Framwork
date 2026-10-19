# R7b independent adversarial review

19 October 2026 (Asia/Kolkata). Reviewed stable implementation checkpoint
`1fdefa194a178e0ab087589596350c2ecc369e55`. The implementation agent paused
source edits, builds and database/Redis workloads during the exclusive review
execution window. Reviewer changes are only the independent integration test
file and this report. No production source, migration, dependency, credential,
customer data, main branch or M10 change.

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
