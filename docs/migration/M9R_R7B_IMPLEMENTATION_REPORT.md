# R7b — Event publication and receipt outage budgets

19 October 2026 (Asia/Kolkata). Starting checkpoint `0a4f7d2` was clean and
directly matched `remediation/m9r-foundation` on the remote. Main remains
`9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7`. ADR062 was committed before code at
`237ca65`; implementation checkpoint `8099055`. No M10, main merge or production
activation. Current status: verification IN PROGRESS, not a completion claim.

## Change and boundaries

The original publication and receipt deadlines were both five seconds, with
five attempts. Actual pre-fix execution reproduced the review's outage problem:
after30 seconds the published event and broker-denied event were both DEAD at
five attempts, with no inbox receipt. The new `event-outage-v2` policy separates
the five-second publication lease from a15-minute consumer receipt window.
Twenty attempts use500ms exponential retry backoff capped at60 seconds. An
expired publication lease waits backoff after expiry; a known publish failure
waits backoff directly. Immediate known failures cannot consume a fresh
20-attempt budget before about783.5 seconds. Actual dispatch delays extend that.

Forward migration00055 replaces only dispatcher SQL ports and the attempts
upper-bound constraint, plus private retry/readiness contract functions. It does
not rewrite issued states, deadlines, tokens, generations, attempts, bodies,
inbox, business data or audit. Existing DEAD/LEGACY_PAUSED rows stay paused.
SQL signatures, event schema1.0 and routing schema1.1 remain compatible. No
compiler/release/workflow artifact is recompiled. New event readiness denies
a missing/mismatched policy before dispatch. No new dependency or secret.

The receipt window is also the existing event capability's expiry. The assigned
worker may resolve only that event/hash/token/generation during the longer
window. Live assignment/membership/policy and per-use RLS remain. This does not
extend workflow leases or grant business-record DML. Trusted Go consumer
callbacks retain3-second transaction,2-second SQL and500ms lock budgets; these
are not a new raw-credential processing-lease protocol or proof of liveness
against a compromised process. Raw-role isolation remains a separate required
regression boundary.

After broker stream loss, republication may wait the remaining15-minute receipt
window. Repeated receipt timeouts may occupy roughly5 hours plus dispatch
delays; no delivery SLA is claimed. A dead predecessor still blocks its own
aggregate. Least-privilege replay, subscriber/payload contract expansion, faster
broker-loss repair and alerting/retention remain open. Existing migrator-only
reviewed replay preserves bodies/inbox/history; it was not exposed publicly.

Source areas: `db/migrations/00055_m9r_event_outage_budgets.sql`,
`internal/events/{delivery_policy,readiness}.go`, the event README, Makefile,
new outage/policy integration fixtures and existing M8 retry/crash expectations.
The [runbook](../operations/R7B_EVENT_DELIVERY_RUNBOOK.md) describes rollout,
tradeoffs and recovery. Frozen migrations/source kits remain unchanged.

## Executed evidence

The same ordinary30-second acceptance ran before and after the SQL change.
It uses a fresh effective-target-verified disposable database and populated
tenantB behind a unique worker role and Redis stream. Only a uniquely owned
sender ACL loses XADD; no shared ACL, Redis service or customer stream changes.
The dispatcher continues; the receiver is absent. No policy shortening or
timestamp adjustment occurs in either real-duration test.

| Check | Actual result |
| --- | --- |
| Pre-fix30-second acceptance | Expected FAIL;31.980s. At30.004 monotonic/30.113 PostgreSQL seconds: receipt DEAD/attempts5, broker DEAD/attempts5, successor attempts0, receipts0 |
| Fixed same30-second acceptance | PASS;35.028s. At30.019 monotonic/30.123 PostgreSQL seconds: receipt SENT/attempts1, broker PENDING/attempts6, successor attempts0, receipts0. Recovery32.141s total, exactly3 inbox/effects/audits/intents;9 attempts retained |
| Existing M8 plus new SQL/readiness/migration tests | PASS;17 groups54.520s, no failures/skips |
| Expanded forward migration manifest | PASS;3.261s. Existing COMPLETED/SENT/DEAD/LEASED/PENDING/LEGACY_PAUSED states populated; SHA-256 manifests unchanged across delivery/attempt/envelope/inbox/replay/business-record/outbox/audit tables; migration55 idempotent |
| First real600-second outage and recovery | FAIL;602.485s. At600.003 monotonic/600.111 PostgreSQL seconds, receipt SENT/attempts1, broker PENDING/attempts16, successor attempts0, receipts0. Recovery failed from the harness treating the normal publish-before-confirmation `ErrNotReady` window as terminal; this first run is not a PASS |
| Independent adversarial review | NOT RUN yet |
| Final full regression/build/browser/reference gates | NOT RUN yet |

Fast boundary tests intentionally accelerate only owned fixture due times;
they verify all20 actual SQL backoffs, the20th terminal transition, no claim
before due time, publication-versus-receipt expiry, stale completion, reissue
fences and exact body identity. They do not replace the real600-second gate.
Readiness drift and missing grants deny. App/worker claim ports and all runtime
roles' direct private retry-helper calls fail with SQLSTATE42501 against
populated fixtures. Existing crash, unknown-COMMIT, Redis loss, inbox dedupe,
ordering, revocation and worker tests remain required.

## Commands actually executed

Logs are ignored under `.local/reports/r7b-*`. Commands run from
`/workspace/Framwork`; PASS denotes executed results only.

| Command | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS; fixture formatting |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestR7bConsumerOutageFirstThirtySeconds$' -v ./tests/integration` | Before: expected FAIL as above. After: PASS |
| `./scripts/go.sh fmt ./internal/events ./tests/integration` | PASS |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=4m -count=1 -run '^Test(R7bSQL|R7bPublication|R7bReadiness|R7bForward|M8)' -v ./tests/integration` | PASS;17 populated groups54.520s |
| `make check test-unit` | PASS; format/vet/module integrity/docs, Go race/security, Node9 and frontend126/126 |
| `make migrate migration-status` | PASS; all55 migrations applied; no owned API/worker/dispatcher process was running at cutover |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=2m -count=1 -run '^TestR7bForwardMigrationPreservesIssuedStateAndEvidence$' -v ./tests/integration` | PASS;3.261s with expanded manifests |
| `make test-event-outage` | First run FAIL602.485s at recovery; fixed600-second absence completed. Repeat required after correcting the harness |

The cold-worker diagnostic passed35.049s; forcing connection renewal did not
reproduce the failure and does not resolve the historic connection finding.
A controlled200ms gap after real XADD and before SQL Finish reproduced the
recovery failure in32.115s: the first event had completed, the next pointer was
not yet confirmed, and its ticket was not ready/dead/completed. The harness
now follows the existing RunConsumer contract for **only** `ErrNotReady`: leave
the broker message pending, assert no new local effects/audit/inbox, and permit
bounded reclaim. Unexpected errors still fail with closed diagnostic categories,
not raw driver text. The same controlled gap then passes with two observed
unconfirmed redeliveries and exactly3 effects/audits/inbox; combined selected
check39.795s. The repeat600-second rehearsal must still pass.

Root review also reproduced a LOW readiness omission: a missing private retry
dependency made a populated claim fail while readiness still reported ready;
an improper runtime grant was similarly accepted. Raw acceptance FAIL3.259s.
Readiness now validates the helper's existence, owner, function/type/strictness,
immutable/search-path contract and absence of base/current-runtime execution
privileges. Missing, base-grant and custom-worker-grant cases pass5.729s.
One earlier custom-grant probe failed6.165s during test-role cleanup because it
had not revoked its injected ACL. The fixture now revokes before role removal;
the single leftover role was identified from its actual failed DROP log and
removed only after proving safe test-role attributes, zero dependencies and
zero connections. No unrelated role or customer data was changed.

No arbitrary-driver/tracer panic, external provider deployment, production
activation or fresh-cloud restoration is claimed. Their existing limits and
deferred status remain in the finding ledger. No attempt budget, offered client
count, SQL/lock/transaction deadline or latency gate is relaxed for verification.

## Recovery and remaining scope

Quiesce owned delivery, apply the additive migration, verify event readiness,
then restart. Preserve all durable evidence and customer data. On failure stop
delivery and forward-fix. Down migration intentionally refuses; restoring a
five-attempt constraint can reject retained rows above five. Do not reset
generations or erase receipts to force replay. The existing operator replay
needs exact generation and an explicit reviewed reason; DEAD is not silently
revived by deployment. Details and the longer broker-loss tradeoff are in the
runbook. Only public source/tests/config/docs belong in Git; no `.env`, keys,
credentials, binaries, caches or generated reports/test output.

R6 authority/independent acceptance/production runtime, other R7 reliability and
workflow lifecycle, replay privileges, subscriber/payload contracts, audit
retention and DR remain open. Wider search/uniqueness lifecycle and the user's
deferred real IdP/MFA/HTTPS-gateway deployment remain separate. This tranche
does not complete M9R or authorize M10, production exposure or a main merge.

**R7b STATUS: verification IN PROGRESS. Overall M9R STATUS: PARTIAL.**
