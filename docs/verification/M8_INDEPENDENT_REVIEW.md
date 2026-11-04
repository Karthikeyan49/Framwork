# M8 independent review

Date: 2026-10-12. Reviewer: independent Codex review agent. Scope: M8 Transactional
Outbox + Inbox on `milestone/m8-outbox-inbox`, including the final readiness
refinement. Implementation was reviewed read-only; the reviewer changed no
implementation or repository test files. Temporary helpers used ignored `.local`
paths and a Go overlay in `/tmp`.

## Verdict

**PASS for the documented M8 development/test delivery foundation.** No unresolved
BLOCKER, HIGH, or MEDIUM finding remains in the reviewed scope. This is an
independent local review verdict, not production certification, remote CI evidence,
human approval, or approval to implement M9.

The independently executed PostgreSQL tests cover immutable source snapshots,
separate aggregate sequences, transactional producer rollback, leased claim
concurrency, worker isolation and live revocation, bounded retry/dead/replay,
atomic inbox/business mutation/audit/derived-outbox effects, deduplication,
acknowledgement after commit, durable quarantine, and unknown commit response
recovery. Independent negative probes confirm credential separation, process
binding rejection, and fail-closed event readiness.

Real shared Redis loss/restart, all transport crash checkpoints, pending-message
fairness and configured delivery-process tests were **not independently run**:
the implementer owned the shared stream/services during review. Their source was
inspected; implementer execution is distinguished below.

## Architecture and preservation

The review followed AGENTS.md, the two-agent protocol, relevant V3 architecture,
audit/role/acceptance and milestone guidance, the M6R audit, retained event/outbox
specification and dual-write risk, and ADR022.

- PostgreSQL is authoritative. Envelope bytes/hash are frozen in the producer
  transaction; delivery does not reconstruct historical payloads from mutable
  records. Redis carries bounded routing references only.
- Dispatcher functions expose routing, not envelope payloads. The worker resolves
  an exact persisted attempt and binds a live delegated actor/tenant/job. Forced
  RLS and normal-role guards remain active; arbitrary customer callbacks/SQL are
  not exposed.
- Consumer inbox receipt, local business effects, audit, completion and any
  derived outbox event use the same transaction. Confirmed commit precedes ACK;
  an unknown commit result retains redelivery. Duplicate receipt does not repeat
  local effects.
- PostgreSQL time, generation/token fencing, independent aggregate sequence and
  bounded retries govern delivery. Replay preserves identity/hash/inbox and
  records operator intent; it does not silently invent a new actor or authorize
  external actions.
- Migrations 00018/00019 are additive. Historical intents remain preserved as
  `LEGACY_PAUSED`; missing historical envelopes are not fabricated. The disposable
  database fixture exercised preservation and repeat application of these
  migrations. This review did not mutate customer/shared business data.
- The only composed consumer is development/test preview receipts. Production
  preview delivery is refused. No workflow runtime, external tool execution,
  customer-defined consumer, public replay endpoint or M9 implementation was
  introduced.

## Findings and closure evidence

### HIGH: cache credential reused for broker operator — closed

Initial source assigned the cache password to an `event_operator` Redis ACL with
administrative permissions. This was a concrete credential separation violation
in `scripts/m8-env.mjs`; no pre-fix escalation command was executed. The fix
generates/preserves a distinct operator binding and excludes it from curated
runtime child environments. The independent read-only AUTH probe now confirms
the cache credential cannot authenticate as the operator, while the dedicated
operator credential resolves to the intended ACL identity. No credential value
was printed and no broker configuration was changed by the probe.

### MEDIUM: allowing policy decisions without provenance — closed

The initial consumer accepted an allow/audit decision with empty decision ID and
policy version. The independent overlay failed: one inbox, local effect and audit
row committed. `internal/events/delivery.go` now rejects missing or oversized
provenance before inserting the receipt or invoking the handler. The same
independent negative case passed after the fix, with zero committed effects,
inbox rows and audit rows. A persistent repository regression also covers this
boundary.

### MEDIUM: wrong Redis service binding accepted at startup — closed

The independent startup helper supplied the sender-only Redis URL to the worker.
Initially it passed startup and returned normally on cancellation despite lacking
consumer operations. The helper failed accordingly. `Config.EventBinding` and
the application composition now verify the worker/dispatcher ACL username before
starting delivery. The identical helper now confirms startup refusal. This test
did not restart Redis or consume shared messages.

### MEDIUM: event contracts absent from readiness — closed

The initial omission was identified from source, not an executed pre-fix outage.
Role-aware `events.Ready` is now called by API readiness and enabled delivery
startup. Independent disposable-database probes confirm healthy API, worker and
dispatcher contracts pass, while a disabled freeze trigger and a missing worker
resolver fail closed.

The first readiness implementation regressed healthy dispatcher startup by
resolving a qualified runtime table with `to_regclass`, requiring runtime schema
USAGE that the dispatcher intentionally lacks. Independent runs failed and
confirmed PostgreSQL 42501. Catalog OID resolution using `pg_class` and
`pg_namespace` fixed the regression without granting payload-schema access.

The first corrected probe still reported producer readiness after required
`request_id` INSERT privilege was revoked in a uniquely owned disposable DB.
That independent case failed. Final readiness checks all eleven permitted
producer columns, the four inbox INSERT columns and required inbox SELECT;
it also rejects forged actor/time INSERT and even partial envelope SELECT for
API/dispatcher roles. The identical revoked-column probe now passes (readiness
is false). Final healthy-role and drift cases also pass.

### Pending-message starvation/terminal handling — source concern addressed

Inspection found that retaining terminal pending messages and always reclaiming
from the beginning could obstruct unrelated fresh messages. A pre-fix starvation
exploit was **not independently executed**, so this is not reported as a
confirmed exploited HIGH. Forward migration 00019 exposes durable DEAD routing;
the consumer ACKs only the corresponding durable terminal state. Redis reads
now alternate fresh/pending work and advance the reclaim cursor. These fixes and
the real pending-message regression were inspected. The actual Redis fairness
test was executed by the implementer, not this reviewer.

## Independently executed verification

Commands ran from `/workspace/Framwork`. Tests using `m8Database` alter only their
uniquely owned disposable databases, including drift probes. No shared stream
reset or service restart was performed by the reviewer.

| Command | Result and evidence |
| --- | --- |
| `node scripts/with-env.mjs ./scripts/go.sh run ./.local/m8-review` | PASS after credential fix: cache-to-operator AUTH rejected, separate operator identity verified. This directory command was run before adding the separate startup helper. |
| `node scripts/with-env.mjs ./scripts/go.sh run ./.local/m8-review/main.go` | Final PASS: identical read-only credential probe. |
| `node scripts/with-env.mjs ./scripts/go.sh run ./.local/m8-review/startup.go` | FAIL before binding fix; final PASS: worker with sender binding refused. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m8-review-delivery-overlay.json -run '^TestIndependentM8MissingPolicyProvenance$' -v ./tests/integration` | FAIL before fix (effects/inbox/audit each 1); PASS after fix (1.837s, all zero). The temporary overlay was subsequently refreshed for readiness probes. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8(ImmutableEnvelopeOrderAndDeleteSequence\|InboxAtomicFailureDeduplicationAndRevocation\|ConcurrentClaimsConsumersAndWorkerIsolation\|LeaseFencingBoundedRetriesDeadLetterAndReplay\|AckAfterCommitAndDurableQuarantine)$' -v ./tests/integration` | PASS, 5.434s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=35s -run '^TestM8UnknownCommitDoesNotAckAndRedeliveryDeduplicates$' -v ./tests/integration` | PASS, 1.997s. Real PostgreSQL protocol proxy drops consumer COMMIT response after database success; no premature ACK, exactly one local effect, then deduplicated redelivery. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=30s -run '^TestM8CrashRecovery$/^producer-before-commit$' -v ./tests/integration` | PASS, 2.023s. Actual SIGKILL before producer commit rolls back business version, audit and envelope. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m8-review-delivery-overlay.json -run '^TestIndependentM8EventReadinessContracts$' -v ./tests/integration` | Initial FAIL (healthy dispatcher), diagnostic FAIL (42501); PASS after catalog fix; later FAIL after revoked required INSERT; final PASS, 1.706s, including healthy roles, trigger/function drift and revoked-column negative. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=35s -run '^TestM8(ImmutableEnvelopeOrderAndDeleteSequence\|InboxAtomicFailureDeduplicationAndRevocation\|ConcurrentClaimsConsumersAndWorkerIsolation\|LeaseFencingBoundedRetriesDeadLetterAndReplay\|AckAfterCommitAndDurableQuarantine\|ReadinessAndProcessBindingFailures\|ConsumerLocalBusinessAuditAndDerivedEventAreAtomic\|UnknownCommitDoesNotAckAndRedeliveryDeduplicates)$' -v ./tests/integration` | Final PASS, 6.548s: seven actual matching tests ran. The guessed business-mutation name did not match; its exact name was run separately next. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=35s -run '^TestM8(ConsumerMutationAuditDerivedOutboxAndInboxAtomic\|ProducerEnvelopeConsumerAuditTraceCorrelation)$' -v ./tests/integration` | Final PASS, 2.426s: both tests ran. Audit failure rolls back mutation/inbox/derived event; success and duplicate preserve one effect; trace and request provenance are bound. |
| `./scripts/go.sh test -race -count=1 ./internal/events ./internal/config ./tests/security` | Final PASS (1.014s, 1.018s, 1.019s). |
| `node --test scripts/runtime-env.test.mjs` | Final PASS: 3 tests, 183.345ms; process credential curation and production provisioning refusal. |
| `./scripts/go.sh test ./internal/events -run '^$' -fuzz '^FuzzPointer$' -fuzztime=5s -parallel=2` | PASS, 63,298 executions, 6.031s. This is bounded fuzz evidence, not exhaustive proof. |
| `git diff --check` | PASS. |

Read-only inspections also included source, SQL, ADR/agent guidance and test
fixtures. Two incidental inspection paths were mistyped and returned file-not-found
(`internal/config/events.go` and `/tmp/m8-review-delivery-overlay.go`); actual code
and helper paths are `internal/config/config.go` and
`/tmp/m8-review-delivery_test.go`. These were inspection errors, not passing checks
or implementation failures.

## Not independently executed and residual limits

| Check | Independent status |
| --- | --- |
| Redis restart/stream-loss/ACL integration case | NOT RUN; shared service owned by implementer. Read-only credential probe ran separately. |
| Publisher-after-claim, publisher-after-send and consumer-after-commit SIGKILL cases | NOT RUN; these fixtures reset the shared stream. Producer-before-commit case independently passed. |
| DEAD pending-message fairness across tenants | NOT RUN; source and fixture inspected. |
| Real configured dispatcher/worker subprocess integration | NOT RUN; source inspected. Wrong-binding startup rejection independently passed. |
| Full repository setup/check/test/build, browser and smoke | NOT RUN by this reviewer for M8; implementer evidence is separate. |
| Remote CI, production deployment/failover, human milestone approval | NOT RUN. |

At review close, the implementer reported full integration PASS (55.504s), frontend
28 tests, browser 4 tests, fuzz campaigns, drift/trace checks and smoke PASS, with
final check/build and final M8 verification being repeated after readiness changes.
These are attributed reports, not substituted for independent execution.

No unresolved code defect is identified in reviewed M8 scope. Operational limits
remain explicit: five bounded delivery attempts can require reviewed replay;
a revoked actor is not silently replaced; failed predecessors block successors
within their aggregate epoch; historical intents stay paused; Redis retention is
bounded and PostgreSQL receipts govern recovery. The guarantee covers atomic
local PostgreSQL effects, not exactly-once external side effects. Future consumers
must enforce their own business policy/DataScope and emit transactional outbox
events for business mutations, as the exercised repository fixture does.

The temporary helpers/overlay are not commit artifacts. Only this review document
was authored by the reviewer for the repository. No test service ports remain
held by the reviewer. M8 ends here; M9 remains unauthorized.

## Final supplement: tenant route lookup and fixture safeguards

The final small refinements were independently inspected after the main review.
`jobs.FindRoute` requires the dispatcher role and now queries both `tenant_id`
and job `id`, matching the tenant-inclusive routing key. The actual two-tenant,
same-job-ID regression runs each opaque ticket through one reused worker
connection, verifies the intended actor/tenant and absence of foreign membership
rows, and confirms the pooled tenant context is cleared.

M8 database fixtures now require explicit development/test and loopback primary
and fallback driver hosts; broker fixtures require loopback Redis. The local ACL
helper checks all cache/sender/receiver/operator passwords are distinct before
writing `.env` or the ACL file. These fixture host checks were inspected, not
independently exercised with remote endpoints. The credential-reuse and production
refusal cases were exercised by the Node tests below.

| Exact command | Result |
| --- | --- |
| `node --test scripts/runtime-env.test.mjs` | PASS: 4 tests, 206.233ms, including credential reuse refusal before writes and production refusal. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout=35s -run '^TestM8ExactTenantJobRoutesAndPoolRebinding$' -v ./tests/integration` | PASS, 1.606s; exact tenant lookup, same job ID across tenants and pooled context cleanup. |
| `git diff --check` | PASS after this supplement. |

The final verdict remains PASS within the stated M8 scope. No shared Redis reset,
service restart, implementation edit or additional milestone work occurred.
