# M8 implementation report

Date: 2026-10-12. Scope: V3 M8 — Transactional Outbox + Inbox only.

## Outcome

**M8 STATUS: PASS** for the documented development/test delivery foundation.
M0–M7/M6R remain intact. No M9 workflow runtime or external tool execution was added.
Independent review passed, including follow-up fixes; see
[the review](M8_INDEPENDENT_REVIEW.md) and [acceptance](M8_ACCEPTANCE.md).
This status is not production deployment/security certification or human release approval.

## Original repository and preservation

The original tree was clean on `milestone/m7-rule-engine-v2` at
`084aa559389761acd16083953d34820638a8d55d`. Origin is
`Karthikeyan49/Framwork`. Work used the existing checkout, a new
`milestone/m8-outbox-inbox` branch and `pre-m8-2026-10-12` checkpoint tag.
No project restart, worktree, force push or main-branch merge was performed.

V3 remains authoritative. `make docs-verify` verified all 43 V3 and 84 retained V2
source checksums; supplied kit documents were unchanged. No archives or application
files were deleted in M8. The read-only legacy store retained **2,380 rows**, matching
the pre-M8 checkpoint. All **436** historical outbox intents were retained and
classified LEGACY_PAUSED. Inspection found zero intents without delivery classification.
Tests created/dropped only uniquely named disposable databases. One disposable
database left by a terminated faulty test proxy was identified and removed; no
customer/shared database or PostgreSQL volume was removed.

## Implemented behavior

New preview CRUD freezes a schema1.0 identifiers-only envelope and SHA256 in the
same transaction as business state and audit. PostgreSQL stamps actor/time and
allocates an event sequence independently of record version. In particular,
update and delete can both use record version2 while their sequences remain2/3.
The envelope pins source revision/IR hash and request/W3C trace correlation. Replay
uses exact stored bytes, never current record values or source recompilation.

The metadata-only dispatcher claims fenced PostgreSQL-time leases with SKIP LOCKED;
Redis publishing happens outside the transaction. Attempts, bounded exponential
retry, receipt deadlines and DEAD state are durable. Known serialization-aborted
claims alone may retry with a fresh transaction; unknown commits are retained for
expiry/recovery. A missing receipt causes republishing, so actual Redis loss does
not erase authoritative events. Ordering applies within an aggregate epoch; a
dead predecessor blocks its successors. There is no global ordering promise.

The Redis adapter publishes canonical references containing tenant/event/hash/
consumer/generation/token. Dedicated sender and receiver ACLs differ from cache
and operator identities. Fresh/pending reads alternate and reclaim advances a
cursor. Durable DEAD references can be acknowledged while their event/history
remain available for reviewed replay. Invalid references are hash-only quarantined
before acknowledgement; untrusted bodies are neither stored nor logged.

Workers resolve exact stored routing attempts before WithJob binds a live original
actor and one tenant/job/release context. Forced RLS exposes only that event's
envelope/inbox. Policy requires an allow decision with provenance and the audit
obligation. Inbox, trusted local effects, audit, derived outbox and completion commit
together; errors roll back all components. ACK follows confirmed commit. A real
PostgreSQL protocol fault test discards the successful consumer COMMIT response:
the caller reports unknown commit, does not ACK, and replay observes one committed
effect and deduplicates. Changed event/hash or cross-tenant routing is rejected.

The composed worker is a **preview receipt consumer** in development/test, without
business mutation, workflow or external effects. The tested LocalHandler port is
trusted Go, with explicit business policy/DataScope ports; it is not customer SQL
or an AI capability. Production preview delivery is refused. Operator replay requires
explicit tenant/event, expected generation and review reference/reason. It preserves
event identity/hash, attempts and inbox; completed events remain completed.

API readiness and enabled process startup inspect forced RLS, owners, emission/
immutability triggers, functions and required grants. Swapped sender/receiver
bindings fail startup. Local helpers preserve secrets, refuse production and reject
reused role passwords. API, worker, dispatcher and frontend use curated environments;
the development coordinator no longer passes its full operator environment to children.

## Important repository structure

```text
cmd/dispatcher/main.go                    new delivery process
cmd/event-replay/main.go                  operator recovery CLI
internal/events/
  transaction.go                         atomic audit/intent port
  envelope.go                            closed versioned envelopes/references
  delivery.go                            dispatcher, consumer and loops
  redis.go                               replaceable Streams transport
  readiness.go                           role-aware schema contract
  envelope_test.go, README.md
internal/platform/jobs/events.go          verified event routing tickets
internal/platform/jobs/jobs.go            tenant-inclusive exact route lookup
internal/platform/app/app.go              composition and readiness
internal/config/                         delivery configuration/binding checks
internal/policy/, internal/audit/         consume policy and correlated evidence
db/migrations/00018_m8_outbox_inbox.sql    additive storage/fenced functions
db/migrations/00019_m8_terminal_routing.sql durable terminal routing refinement
scripts/m8-env.mjs                        local separate broker ACL identities
scripts/dev.mjs, runtime-env*.mjs         per-process credential curation
tests/integration/m8_*                    real PG/Redis/fault/process regressions
docs/adr/022-m8-transactional-delivery.md
docs/verification/M8_*
Makefile, .github/workflows/ci.yml         process/build/parser-fuzz targets
```

Existing API/worker entrypoints, frontend pages/components, migrations1–17 and
dynamic data/compiler/rules implementations are retained. Existing integration
fixtures now expect19 migrations and use explicit tenant-inclusive routing; tests
were strengthened rather than disabled. No major dependency was introduced:
database/sql and existing pgx handle persistence, existing go-redis supplies Streams,
existing OTel supplies spans, and standard Go/Node libraries supply validation,
hashes, configuration and process management. Lockfiles/go.mod/go.sum are unchanged.

## Executed checks

Commands ran from `/workspace/Framwork`. No remote CI or production test is claimed.
Some unchanged Go unit packages reused valid test cache; integration commands use
`-count=1`, and frontend/browser suites actually ran.

| Exact command | Final result/evidence |
| --- | --- |
| `make setup` | PASS; managed Go modules verified and frozen npm install completed. |
| `make env` | PASS, repeatedly; existing secrets/keys retained, distinct ACL bindings prepared. |
| `make infra-up` | PASS; PostgreSQL, Redis and OTel healthy without removing volumes. |
| `make migrate` | PASS; forward18 then19 applied; subsequent migrations repeat safely. |
| `make migration-status` | PASS; all19 migrations applied. |
| `make release-check` | PASS; stored IR compatibility, no recompilation. |
| `make check` | PASS after final code refinements;127 source checksums, Go format/vet/module verification, TypeScript lint/typecheck. |
| `make test` | PASS; Go race/unit/security tests, Node then3 tests, frontend28 tests, full integration47.923s. Final extra Node case separately passed4/4. |
| `make test-integration` | Final PASS,53.798s, after readiness, tenant-route and local-binding refinements. Previous final regression also passed55.504s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8' -v ./tests/integration` | PASS,28.344s;13 then-existing M8 tests plus4 crash subcases. New exact-tenant regression ran separately, bringing current M8 top-level tests to14. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8ExactTenant\|^TestIndependentM6RWorker' -v ./tests/integration` | PASS,2.259s; same job ID across tenants, actor rebinding/pool cleanup and retained M6R worker tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8Readiness\|^TestM8ProducerEnvelope' -v ./tests/integration` | PASS,2.338s; grant/function/trigger negatives and producer→consumer trace/request audit. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8UnknownCommit' -v -timeout=30s ./tests/integration` | PASS,1.877s; actual committed database response loss with ACK withheld and deduplicated replay. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8LeaseFencing' -v ./tests/integration` | PASS,1.898s; includes final receipt-timeout empty-claim commit regression. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8Ack\|^TestM8UnknownCommit\|^TestM8Running' -v ./tests/integration` | Final PASS,6.838s; missing consumer policy retains a valid event, unknown commit retains ACK, and real configured delivery completes safely. |
| `./scripts/go.sh test -race ./internal/events ./internal/config ./internal/policy ./tests/security` | PASS after final implementation refinements. |
| `node --test scripts/*.test.mjs` | Final PASS4/4; environment curation, production refusal and credential-reuse rejection. |
| `make build` | PASS; all Go binaries and React/TypeScript/Vite build. |
| `make browser-install` | PASS; installed Chromium selected. |
| `make test-e2e` | PASS4/4,7.8s; real CRUD/version conflict/paging/policy/mobile/redaction. |
| `make test-events-fuzz` | PASS;10-second campaign,161,063 executions. |
| `make test-rules-fuzz` | PASS; all3 retained M7 campaigns (266,563 /20,861 /3,998 executions). |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | Final PASS; live health/readiness including events, frontend proxy, auth/CORS negatives, collector traces/metrics, initialized delivery processes and graceful API/worker/dispatcher shutdown. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS;0 reported production dependency vulnerabilities. |
| `git diff --check` | PASS. |
| Remote GitHub CI / production deployment / human release approval | NOT RUN. |

M8 real-service tests include actual Redis restart with lost stream, pending reclaim,
DEAD-message fairness between two actors/tenants, four SIGKILL checkpoints, configured
dispatcher/worker subprocesses, database-role/RLS/live revocation, exact immutable
identity replay, and atomic actual data-runtime mutation/derived outbox rollback.
The final expired-receipt regression additionally proves that an empty claim result
commits its terminal DEAD update instead of rolling it back.

### Failures found and corrected

- Initial targeted M8 run FAIL: concurrent lease claims hit confirmed PostgreSQL
  serialization aborts; bounded abort-only claim retry fixed this. The same run FAIL
  on Redis loss proof because default RDB snapshots survived restart. Local Redis
  now explicitly disables snapshot/AOF persistence and uses a separate cache
  snapshot filename; no retained PostgreSQL data or old Redis snapshot was deleted.
- The first lost-COMMIT test harness had a URL mutation race and was terminated
  after111.586s (FAIL), then initially missed lowercase `commit` (FAIL). The local
  proxy now captures immutable backend address, waits for accept shutdown and
  identifies commit case-insensitively. Actual unknown-commit test passed under race.
- First full `make test` FAIL: old fixture expected17 migrations; readiness's first
  qualified-table lookup required runtime-schema access that dispatcher lacks.
  The fixture now verifies19 and readiness resolves catalog OIDs without broadening
  role grants. Healthy startup and full regressions passed after correction.
- Standalone trace fixture initially FAIL because its test SDK did not install a
  W3C propagator. It now installs/restores both SDK and propagation, matching actual
  application startup; standalone and full-suite correlation passed.
- Independent credential, missing policy provenance, service binding and required
  INSERT-grant findings, including their failed probes and verified closures, are
  fully recorded in the independent review. No check was skipped to hide a failure.

## Recovery and security limits

Stop owned dispatcher/worker processes or set EVENTS_ENABLED=false to pause without
removing intent/inbox history. Leases expire under PostgreSQL time. Restore broker
connectivity and legitimate live membership/policy before reviewed replay. Use the
operator-only CLI with separate migration credentials, exact expected generation,
operator review reference and a nonsecret reason; do not impersonate a new actor.
Replay refuses stale generations, active leases and legacy paused intents. Completed
receipts are never deleted or reexecuted. A dead predecessor blocks its aggregate.

Keep additive schemas during a code rollback and pause delivery while verifying
compatibility. Never run destructive Down/reset, drop customer data, clear inbox or
reconstruct old events from current records. Apply a reviewed forward correction,
or restore a tested operational backup into a separate recovery environment. The
development tests drop only their own disposable databases; fixtures now require
explicit development/test and local database/broker bindings.

Envelope payload currently contains identifiers and record version, not flexible
business values. Additional governed snapshots need a new reviewed schema. There
is one composed preview consumer; future registered workflow/tool/production
consumers remain M9/M10/M13. Five bounded attempts and short development lease/
receipt deadlines may require manual recovery; production throughput/failover and
retention/archiving policies remain M15/M18. PostgreSQL event/inbox history currently
has no automated pruning. Local processes share a developer OS user; production
deployment must separately restrict secret mounts. External exactly-once effects,
reconciliation, workflow state, AI and complete ERP domain packs are not implemented
by M8. These are explicit milestone limits, not claimed completed features.

## Architecture, environment and Git

ADR022 records the concrete delivery choice and guarantees. AGENTS.md/CLAUDE.md now
authorize M8 and stop before M9. Source kits and existing ADRs remain unchanged.

The onboarding skill's reusable install/start configuration was saved as a confirmed
draft with M8 startup/readiness and preservation instructions. Current-instance
validation passed. Environment publication/fresh-task restoration is not claimed;
the user must review/save and publish the draft in environment settings.

Commit source, tests, migration SQL, docs and managed build/CI/config examples.
Do not commit generated binaries, node_modules, frontend dist, local test logs,
Redis ACL material, .env or identity keys. These remain ignored. Git publication
and final remote verification are recorded below once executed; main stays unchanged.

Recommended next milestone: **M9 — Workflow Runtime V2**, only after a new user task.
