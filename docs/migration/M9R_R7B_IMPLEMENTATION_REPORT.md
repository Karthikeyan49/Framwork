# R7b — Event publication and receipt outage budgets

19 October 2026 (Asia/Kolkata). **R7b STATUS: bounded PASS. Overall M9R STATUS:
PARTIAL.** Starting checkpoint `0a4f7d2` was clean and directly matched the remote
`remediation/m9r-foundation` branch. Main remains
`9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7`. Final production code is `031c8a1`,
with independent tests and closure at `a15d629`. Final checks below ran against
that stable source; subsequent completion edits are documentation only.
No main merge, production activation or M10 work.

## Result and boundaries

The original five-second publication/receipt deadlines and five attempts
reproduced premature dead-lettering during an actual 30-second outage. The final
fixed database contract, `event-outage-v3`, provides:

- Five-second fenced publication leases, a 15-minute consumer receipt window,
  and 20 attempts with 500ms exponential retry backoff capped at 60 seconds.
- Backoff after an expired publication lease as well as known publish failure.
  Immediate known failures cannot consume a fresh budget before about 783.5s;
  dispatch delays extend that. Repeated receipt timeouts can occupy roughly five
  hours plus dispatch delays. Neither number is an end-to-end delivery SLA.
- Event-only READ COMMITTED authority at binding and subsequent SQL use, plus
  direct restrictive event-envelope/inbox RLS. Raw RR, SERIALIZABLE and imported
  snapshots cannot establish current event authority. API and workflow callbacks
  keep REPEATABLE READ. Event executors do not certify a shared runtime snapshot.
- Readiness checks actual private retry outputs and invalid-input behavior,
  inspecting helper structure/owner/private grants before the owner-executed
  semantic probe. Incorrect isolation policies also deny readiness.

The receipt deadline also bounds the assigned worker's exact event capability.
Live revocation is visible on subsequent statements, not instantaneous
cancellation of a statement already running. Trusted Go transaction/SQL/lock
budgets remain 3s/2s/500ms. No database processing lease or raw-process
lock/liveness guarantee was introduced. Exact tenant, actor, audience, event,
hash, generation and token fences remain; no generic business-record DML grant.

Broker stream loss can now delay republication by the remaining 15-minute
receipt window. Dead predecessors still block their aggregate. Single-consumer,
identifier/version-only event payloads and migrator-only reviewed replay remain.
No production consumer, workflow renewal, external action or universal
exactly-once guarantee is added. Local effects below are populated test-fixture
effects, not independent ERP business acceptance.

## Decisions, migrations and preservation

[ADR062](../adr/062-m9r-event-outage-budgets.md) was committed before implementation
at `237ca65`. [ADR063](../adr/063-m9r-live-event-authority.md) was committed before
the isolation/probe fix at `a98cb40`; its cutover correction was committed before
migration 57 at `3840245`. See the [runbook](../operations/R7B_EVENT_DELIVERY_RUNBOOK.md).

| Forward migration | Change |
| --- | --- |
| 00055 | Retry/receipt policy, 20-attempt constraint and compatible dispatcher ports |
| 00056 | Event RC authority and closed no-argument semantic policy probe |
| 00057 | NOWAIT exclusive drain barrier and direct restrictive event RC policies |

All 57 migrations are actually applied locally. Pushed migrations were never
edited. No migration rewrites delivery states, deadlines, tokens, generations,
attempts, envelope bytes, inbox, business records or audit. Existing DEAD and
LEGACY_PAUSED rows stay paused. Event schema 1.0, routing 1.1 and SQL signatures
remain compatible; no IR/release/workflow artifact is recompiled.

The populated 54-to57 rehearsal applies three migrations, repeats idempotently,
and preserves SHA-256 manifests across eight tables: deliveries, attempts,
envelopes, inbox, replay, business records, outbox intents and audit. It includes
COMPLETED/SENT/DEAD/LEASED/PENDING/LEGACY_PAUSED fixtures, including a genuine old
five-attempt DEAD row. This is executed fixture evidence, not a customer-volume
production migration claim.

Function-only replacement at56 does not revoke every cached read in an already
open old backend. Migration57 therefore requires a maintenance cutover: quiesce
owned API event emitters, all workers (including workflows), and dispatchers;
drain their transactions. Its NOWAIT locks refuse a live old reader/binder with
SQLSTATE55P03. The test proves no applied57 marker, partial policy or changed
manifest on failure, then successful installation after the owned transaction
drains. It does not silently terminate unknown sessions or promise online
revocation. Down migrations deliberately refuse destructive reversal.

## Independent findings and fixes

The [independent review](../verification/M9R_R7B_INDEPENDENT_REVIEW.md) preserves
all attacks, failures and re-verification commands. Reviewers had exclusive
execution windows against stable commits; implementation paused edits, builds
and database/Redis tests during those windows.

| Finding | Executed evidence and closure |
| --- | --- |
| IR-R7b-01 MEDIUM: stale raw worker snapshot | Before fix, revoked RR and imported snapshots read the exact event and accepted inbox INSERT after 5.2s; schema54 denied both. Age-only mitigation was rejected by a fresh importer aged 0.021s. Fresh RC enforcement fixed entry; further review exposed cached reads across 55-to56. The enforced drained57 cutover and direct RLS close the supported boundary. Historical read retention remains an explicit positive control |
| IR-R7b-02 LOW: retry semantics not checked | Operator-only zero-delay helper drift passed readiness and exhausted 20 attempts in206ms. Actual output/input checks now deny all three runtime roles while direct helper execution stays private |
| IR-R7b-03 LOW: unsafe helper invoked before denial | A VOLATILE marker-writing helper produced 75 writes through the owner probe before rejection. Reordered structural checks now deny with zero marker writes |

Final independent selection: **PASS 32 top-level groups in 92.968s, zero failures
or skips**. It includes raw credential attacks, RC/API/workflow isolation,
prepared/imported snapshots, eight readiness-policy faults, private grants,
semantic variants, cutover preservation, and retained crash/unknown-COMMIT/
ordering/dedupe/replay/broker-loss checks. A fixture that simulates stale
function authority still gets zero envelope visibility and denied inbox INSERT
through the direct policies. Protection against an administrator able to replace
both code and its probe is not claimed.

## Actual duration and final gates

Outage fixtures use effective-target-verified disposable databases, populated
tenants, uniquely assigned worker credentials/Redis streams and fixture-owned
sender/receiver ACLs. Only the owned sender loses XADD. Shared ACLs, services and
customer streams are not modified. Real-duration tests do not alter timestamps
or shorten policy. Fast boundary tests explicitly accelerate owned due times.

| Check | Actual result |
| --- | --- |
| Old-policy30s outage | Expected FAIL; 31.980s: at30.004 monotonic/30.113 PostgreSQL seconds, receipt and broker events DEAD at5 attempts; successor0, receipts0 |
| First corrected30s outage | PASS; 35.028s: SENT/1, broker PENDING/6 at30s; exactly3 local facts of each kind after recovery |
| First actual600s run | FAIL; 602.485s at recovery; absence completed600.003 monotonic/600.111 PostgreSQL seconds. Original error was unclassified, not conclusively diagnosed by a later reproduction |
| Second actual600s run at 1fdefa1 | PASS; 609.451s; absence600.004/600.104, recovery606.726s total,3 facts of each kind,19 attempts,2 unconfirmed redeliveries. This predates review corrections |
| Final actual600s run at a15d629 | **PASS; 609.388s**; absence600.002/600.123, SENT/1, broker PENDING/16, successor0, receipts0. Recovery606.446s total; exactly3 completed/inbox/effects/audits/immutable intents,19 attempts retained,2 unconfirmed redeliveries |
| Full check/test/build | **PASS**:304 integration groups, 884.394s, zero failures/skips;126 frontend tests,9 Node tests; Go race/security/format/vet/module checks, TypeScript lint/typecheck, frontend and backend builds |
| Application browsers | PASS; 11/11,26.9s |
| Local sign-in browsers | PASS; 7/7,10.4s |
| Numeric-read reference target | PASS; 76.921s: ten-client HTTP p95 158.903ms, p99 183.382ms,100 measurements, zero errors; unchanged750ms p95 limit |
| Concurrent-write reference target | PASS; 62.094s:32 clients,6527 writes, p95 349.656ms, p99 393.181ms,108.29/s, zero errors; unchanged500ms p95 limit |

The read benchmark's deliberately restored original-policy baseline fails with
SQLSTATE57014 under the unchanged 2s budget as expected; it is not a corrected
query PASS. Reference-v2 was selected with `make reference-up` and verified on
the connected PG18.6 cluster: 4CPU/8GiB/no swap, shared_buffers2GiB,
effective_cache_size6GiB, work_mem4MiB and random_page_cost1.1. API and Docker
share the parent's four CPUs. Workloads ran serially without competing builds or
DB tests, forced GC/vacuum, client reductions or raised per-operation deadlines.

Current read/write results are slower than recorded R7a p95 values 75.786ms and
165.909ms/13334 writes. No cause or performance neutrality is claimed. Both
unchanged gates pass for this bounded tier; wider profiling remains R7 work.
The other six capacity targets are **NOT RUN in R7b**; retain R7a's prior all-eight
evidence rather than describing it as a new rerun. No general workload or
production capacity guarantee follows.

## Commands actually executed

From `/workspace/Framwork`; ignored evidence logs are `.local/reports/r7b-*`.
Repeated outcomes are distinguished below. Independent commands are separately
listed in the linked review report.

| Command | Actual result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS |
| `./scripts/go.sh fmt ./internal/events ./tests/integration` | PASS |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestR7bConsumerOutageFirstThirtySeconds$' -v ./tests/integration` | Old-policy expected FAIL; 31.980s; initial fix PASS; 35.028s; cold diagnostic PASS; 35.049s; controlled confirmation-gap harness FAIL; 32.115s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=4m -count=1 -run '^Test(R7bSQL\|R7bPublication\|R7bReadiness\|R7bForward\|M8)' -v ./tests/integration` | PASS; 17 groups54.520s |
| `make check test-unit` | PASS, repeated after readiness/fixture corrections |
| `make migrate migration-status` | PASS at55, then57; actual forward application |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bForwardMigrationPreservesIssuedStateAndEvidence$' -v ./tests/integration` | PASS; 3.261s with expanded eight-table manifests at55 |
| `make test-event-outage` | First FAIL; 602.485s; second PASS; 609.451s; final-source PASS; 609.388s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bReadinessRequiresPrivateRetryDependency$' -v ./tests/integration` | Pre-fix FAIL; 3.259s; one cleanup-fixture FAIL; 6.165s; corrected PASS; 5.729s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^Test(R7bReadinessRequires\|R7bConsumerOutageFirst)' -v ./tests/integration` | PASS; 39.795s after confirmation-gap harness fix |
| `./scripts/go.sh fmt ./internal/events ./internal/platform/jobs ./internal/platform/postgres ./tests/integration` | PASS |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=4m -count=1 -run '^Test(R7bReview\|R7bForward\|R7bReadiness)' -v ./tests/integration` | PASS; 32.867s at56; expanded57 selection PASS; 44.544s |
| `make migrate migration-status check test-unit` | PASS at56,126 frontend tests |
| `sha256sum .local/reports/r3c-pre49-api` | PASS: actual retained artifact matches the pinned hash below |
| `GOFLAGS=-v M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-pre49-api M9R_LEGACY_API_SHA256=03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19 make check test build` | PASS; 304 integration groups; retained-binary compatibility actually executed, no skips |
| `make test-e2e` | PASS; 11 |
| `make test-signin-e2e` | PASS; 7 |
| `make reference-up` | PASS; actual connected profile verified by both benchmarks |
| `make test-performance` | PASS; expected old-policy baseline failure retained |
| `make test-write-performance` | PASS |
| `make docs-verify` | PASS; source-kit checksums84+43 |
| `git diff --check` | PASS |

A controlled200ms gap between real XADD and SQL Finish reproduced `ErrNotReady`.
The harness now follows the existing consumer contract: only that transient
leaves the broker message pending for bounded reclaim, with no new local facts;
all unexpected errors still fail. This does not conclusively classify the first
600s failure. Forcing a cold connection passed but did not fix the historical
connection finding. The custom-grant test once failed cleanup because it left
its injected ACL; cleanup now revokes first. Its single leftover role was
identified from the failed DROP and removed only after proving safe owned-test
attributes, zero dependencies and zero connections.

Independent failed probes, the corrected callback fixture, old cached reads,
unsafe-helper marker and client-cancelled DDL diagnostic are all preserved in
the review report. The latter was replaced with a server55P03 plus rollback and
post-drain positive control; cancellation alone did not prove DDL rollback.

## Files, dependencies, recovery and remaining work

Changes include Makefile's opt-in outage target; migrations 55–57;
internal/events policy/readiness/docs; explicit event transaction selection in
internal/platform/jobs and internal/platform/postgres; M8 expectation updates;
populated protocol/outage/review tests; ADR062–063; the runbook, reports and
finding/plan links. No new dependency, secret, environment variable or customer
DDL path was introduced. Only public source, tests, configuration and docs
belong in Git; never `.env`, keys, ACL credentials, binaries, caches, generated
builds or test logs. Those local artifacts remain ignored. Completion documentation is committed on
`remediation/m9r-foundation` alongside all source/review commits; the final chat
checkpoint records the exact directly verified pushed HEAD and clean status.
Main is unchanged.

On a cutover failure stop, inspect migration state, identify/drain only owned
transactions and forward-fix or retry the reviewed migration. Do not silently
terminate unknown sessions. Never erase receipts, reset generations or revive
DEAD rows to force progress. Old RR consumer binaries fail after the corrected
cutover and must be upgraded. A metadata rollback cannot undo external effects.

Still open: R6 authenticated requirement/independent acceptance/promotion
integration and production active-release execution; other R7 replay privileges,
subscriber/payload contracts, workflow cycles/renewal/current-record approval,
audit retention/DR, distributed quotas and profiling; broader search/uniqueness
administration/removal/evolution. The injected driver/tracer-panic diagnostic
remains a historical FAIL and was NOT RUN here; no arbitrary driver recovery is
claimed. Unconfirmed connection and narrow worker-policy timing findings remain
open despite passing regressions. The user-deferred real IdP/MFA/HTTPS-gateway
verification, production activation and fresh-cloud restoration are NOT RUN.

**R7b STATUS: bounded PASS. M9R STATUS: PARTIAL. M10 remains paused.**
