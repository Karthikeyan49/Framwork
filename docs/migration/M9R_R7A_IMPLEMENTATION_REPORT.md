# R7a — Bounded admission before protected API work

19 October 2026 (Asia/Kolkata). Started from clean, remotely verified
`4f01dfd9b4b969b7d9411e4731849529a32da96e` on
`remediation/m9r-foundation`. The requested initial push returned
`Everything up-to-date`. Main remains
`9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7`; no main merge or M10.

**R7a STATUS: PASS within the process-local boundary below. Overall M9R remains
PARTIAL.** Independent review found no product defect. Final root check/test/build
passes 283 integration groups without failures/skips and 126 frontend tests;
browser suites 11 + 7 and all eight unchanged capacity targets pass.

## Scope and decisions

The user requested pushing the existing work and continuing pending remediation.
Local R5 was already complete and pushed. External provider deployment remains
deferred by the user's explicit choice. R6 requires the later M11–M14 authority
chain; this independent R7a foundation is pulled forward to close a concrete
abuse gap without inventing production promotion or starting M10.

[ADR061](../adr/061-m9r-pre-database-request-admission.md), committed at
`be7b01a` before implementation, defines the boundary. Product `8f9805e` and
independent review/tests `cf45300` preserve existing authentication, live
membership, default-deny policy, RLS, atomic business/audit/outbox effects,
known-abort retries and unknown-outcome reconciliation. There is no dependency,
migration, database role, schema, customer-record or frozen source-kit change.
Migrations remain forward-only through `00054`.

## Implementation

`internal/platform/traffic` implements per-API-instance integer-credit token
buckets. Global and canonical socket-peer checks precede bearer verification
and body reading. A verified issuer/subject selects the subject budget;
malformed or crypto/claim/time-invalid bearers select the invalid budget.
Forwarding headers, bearer tenant/role claims, token IDs and ports cannot select
new trusted identities. IPv4-mapped addresses normalize to IPv4; malformed or
missing peer addresses share one bucket. Identity keys are length-delimited
issuer/subject SHA-256 values, never stored bearer tokens.

| Budget | Refill / burst | Configuration |
| --- | --- | --- |
| Socket peer and verified subject | 300/s; 600 burst each | `API_REQUESTS_PER_SECOND`, `API_REQUEST_BURST` |
| API instance global | 1000/s; 2000 burst | `API_GLOBAL_REQUESTS_PER_SECOND`, `API_GLOBAL_REQUEST_BURST` |
| Invalid bearer per peer | 1/s; 20 burst | Fixed by admission version |
| Invalid bearer global | 10/s; 50 burst | Fixed by admission version |

General rates accept strict integers 1..10000, bursts 1..2×rate, peer bounds no
larger than global. Missing bursts default to twice the corresponding rate.
Invalid or disable-like settings fail configuration; zero-valued programmatic
API configuration receives the enabled default. Operator values reach the API
child only, not worker/dispatcher/frontend processes. No new secret is required.

Peer and subject pools each cap at 8192 entries. Two-minute idle expiry exceeds
the maximum refill time. Sixteen cleanup steps per lookup bound work without
scanning every entry; active entries are never evicted for a new identity. Full
pools reject new keys. Repeated expiry/reuse retains bounded arrays and maps.
Clock regression cannot refill credit. One mutex protects state; no per-request
timer/goroutine or new queue is introduced. Metric observation happens outside
the lock, with optional observer panics contained.

Rejected protected requests return the standard safe HTTP 429 envelope with a
server request ID, `Cache-Control: no-store` and integer `Retry-After`, also
exposed through the configured CORS contract. No transaction, denial audit,
body processing or business mutation occurs after rejection. Peer/global
rejection also precedes signature verification. Admitted invalid tokens retain
normal denial audit; valid bearers are reverified inside the transaction and
every known-aborted retry, preserving expiry after queue waits. Signed-valid
unprovisioned/revoked users still need live SQL and use general quotas; there is
no negative membership cache or cached authorization result.

Every gate rejection increments `framework.api.admission.rejected` with only
closed `reason` and policy-version attributes. All HTTP 429 access logs are
aggregated: first occurrence, then at most one count summary per 10 seconds per
handler. They include no address, token, subject, path, query or request payload.
Other admitted request logs and immutable audit semantics remain unchanged.

Changed areas: API configuration/composition/access transaction entry, HTTP
logging/CORS, new traffic package, API runtime environment allowlist and
`.env.example`. Tests cover those packages plus populated integration and actual
API binary behavior. README, agent contract, finding ledger and remediation plan
link this report. Generated binaries, reports/logs, private keys, `.env`, caches,
test output and dependencies remain ignored and must not be committed.

## Independent evidence

The [independent review](../verification/M9R_R7A_INDEPENDENT_REVIEW.md) used an
exclusive stable-code/database window and committed only its own tests/report.
No BLOCKER/HIGH/MEDIUM/LOW product defect was found in the bounded review.

The same raw acceptance test ran against actual checksum-pinned API binaries:

| Artifact | Actual result |
| --- | --- |
| Retained before API, SHA `ee24c2fb16c7fa6c4b0b9d5588d2a0c5f39168587ecd6609916e57aa989fdaa8` | **Expected raw acceptance FAIL**: 40 malformed requests → 40 HTTP 401 and 40 matching denial audit rows; no 429; 79ms request loop |
| Fixed API built from product `8f9805e`, SHA `c3f57d1563b5de0b02848f1a0f01430399c7e349d7f140ba8a9ae26f8019e4f3` | **PASS**: 40 requests → 20 HTTP 401/20 denial audits plus 20 HTTP 429; zero audits for all rejected request IDs; 50ms request loop |

The old build records revision `ba21eb0`, modified=true from R5 documentation edits;
it is not claimed to be a clean build of 4f01dfd. The fixed binary records
8f9805e, modified=true from independent test files; product source was stable.
Final rebuilding changes the artifact hash. Neither binary is committed.

Eight populated real-PG groups pass, including 100 concurrent invalid requests
(20 admitted/80 rejected), exact actual audit request IDs, unread rejected POST
bodies, closed-pool negative controls, signed claim/peer/token rotation, live
membership revocation, required-audit failure, real queued JWT expiry, queued
cancellation without leaked leases, and an injected SQLSTATE 40001 proving retry
reverification plus exactly one local business/receipt/outbox effect. Unit
attacks exercise 1000-way contention, capacity/TTL reuse, clocks, observer faults
and all seven metric reasons; wrapper tests execute 1000 actual responses and
parallel log calls. This is workload-specific evidence, not a production abuse
capacity certification.

Initial independent binary attempts failed from a reviewer fixture generating
invalid HTTP header control bytes. The corrected valid IPv4-header fixture was
rerun against both binaries; the original logs are preserved. Those fixture
failures are distinct from the expected before-product acceptance FAIL above.
No failure or negative control is relabeled as a product PASS.

## Executed commands and results

Commands run from `/workspace/Framwork`. Root logs are ignored under
`.local/reports/r7a-*`; reviewer exact commands, hashes and timings are in the
linked independent report. Only actual executed checks are marked PASS.

| Exact command | Result / evidence |
| --- | --- |
| `git push origin HEAD:refs/heads/remediation/m9r-foundation` | PASS;  initial existing checkpoint already up-to-date, remote SHA independently matched |
| `./scripts/go.sh fmt ./internal/platform/traffic ./internal/platform/access ./internal/platform/app ./internal/platform/httpserver ./internal/config` | PASS;  intentional implementation formatting |
| `./scripts/go.sh test -race ./internal/config ./internal/platform/access ./internal/platform/httpserver ./internal/platform/traffic ./tests/security` | PASS;  initial traffic package had no tests at this point |
| `./scripts/go.sh fmt ./internal/platform/traffic ./internal/config ./internal/platform/httpserver` | PASS;  format populated unit tests |
| `./scripts/go.sh test -race ./internal/platform/traffic ./internal/config ./internal/platform/httpserver ./internal/platform/access ./tests/security` | PASS;  populated units and security checks |
| `./scripts/go.sh fmt ./tests/integration` | PASS;  integration fixture formatting |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestM9RTraffic' -v ./tests/integration` | PASS;  initial 5 groups in 5.910s; independent expanded 8 groups in 8.343s |
| `node --test scripts/runtime-env.test.mjs` | PASS; 9/9, API-only configuration forwarding |
| `make check test-unit` | PASS;  format/vet/module integrity/docs, Go race/security, Node 9 and frontend 126/126 |
| `make reference-up` | PASS;  existing reference-v2 profile selected; no later ordinary infra-up |
| `sha256sum .local/reports/r3c-pre49-api` | PASS;  retained compatibility artifact `03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19` |
| `GOFLAGS=-v M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-pre49-api M9R_LEGACY_API_SHA256=03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19 make check test build` | PASS; 283 integration groups in 399.496s, no failures/skips;126 frontend tests, Node 9, Go race/security, checks and all binaries/frontend build |
| `make test-e2e` | PASS; 11/11,15.8s, actual built API and browser |
| `make test-signin-e2e` | PASS; 7/7,6.2s, local OIDC fixture, not an external provider deployment |
| `make test-performance` | PASS; 38.119s, numeric HTTP p95 75.786ms, zero errors |
| `make test-scalar-performance` | PASS; 63.941s, 12 text/date cases, worst HTTP p95 186.845ms, zero errors |
| `make test-choice-performance` | PASS; 62.491s, 12 enum/reference cases, worst HTTP p95 178.285ms, zero errors |
| `make test-native-performance` | PASS; 56.340s, 12 datetime/bool cases, worst HTTP p95 165.110ms, zero errors |
| `make test-search-performance` | PASS; 30.394s, 8 search cases, worst HTTP p95 112.226ms, zero errors |
| `make test-write-performance` | PASS; 61.015s, 32 clients, 13,334 writes, p95 165.909ms/p99 179.128ms, 221.74/s, zero errors |
| `make test-unique-performance` | PASS; 132.991s, fresh keys p95 75.606ms, 100/100 expected conflicts p95 72.443ms; 32 clients/10,428 unique-key writes, p95 214.344ms/p99 236.795ms, 173.31/s, zero unexpected errors |
| `make test-aggregate-performance` | PASS; 380.959s, 24 unmapped/current/retained cases, exact results and zero request errors; worst SQL p95 186.878ms/HTTP p95 505.454ms |
| `make docs-verify` | PASS; 84 retained architecture and 43 V3 checksums unchanged |
| `git diff --check` | PASS; documentation handoff has no whitespace errors |

Capacity commands run serially with the same exported retained-binary bindings
shown in the full command. Actual connected-cluster/container guards verify
pinned PostgreSQL 18.6, 4 CPUs, 8GiB/no swap, 2GiB shared buffers, 6GiB effective cache,
4MiB work memory and random_page_cost1.1. API and PostgreSQL share the cloud
parent's four CPUs. Individual budgets, client counts, default 4 database
admission and new request-admission defaults remain unchanged. No forced GC,
VACUUM or manufactured quiet period. This is the bounded ADR058 reference-v2
tier, not a production-capacity claim. Read HTTP limits remain 750ms; SQL
aggregate limits remain 500ms and writes 500ms. Each read/aggregate case retains
100 measured samples at 10 offered clients; write gates retain 32 clients for at
least 60 seconds. These measurements do not prove performance neutrality versus
another run/host, arbitrary queries or an abuse workload.

The aggregate target also deliberately probes forced generic prepared plans
outside the runtime's accepted LOCAL custom-plan execution. Five such diagnostic
queries hit SQLSTATE57014 at 2.004–2.007s under the unchanged 2s statement timeout.
Those are **diagnostic FAILs**, retained in the log, not capacity passes for
generic plans. All 24 measured runtime cases still return exact results with
zero request errors. The original-policy numeric diagnostic completed 1258.473ms
on this host; it did not reproduce the older host's timeout. Neither diagnostic
changes the historical R3 findings or the implemented admission boundary.

The explicit injected-driver/tracer-panic R7 diagnostic was NOT RUN in this
slice; its previously demonstrated FAIL remains unresolved, not covered by the
default suite. No external IdP/client/MFA/gateway/TLS/log/rotation deployment was
run; the user deferred it. No production activation or fresh-cloud restore was
performed.

## Recovery, limits and remaining work

No database rollback or customer-data migration is required. To revert source,
assess the restored abuse exposure first, then deploy a reviewed source revert
without removing data, audits, command/inbox receipts or immutable artifacts.
Existing admitted work follows existing atomic/unknown-outcome contracts.
HTTP 429 must not trigger automatic administrative/command POST retries. Restart
clears local quota state; it is not a retry or evidence-reset mechanism.

This protects only registered protected access API routes within one process.
Health/readiness, unknown routes and provider login retain separate contracts;
network/gateway controls are still needed. Clients behind one proxy share its
socket-IP allowance. Replicas and restarts change aggregate budgets. No cluster
quota, DDoS guarantee, production gateway profile or production capacity is
claimed. A valid signed identity can still consume live authorization/audit
work within general limits. All quotas consume real resources and need
deployment-level sizing/abuse verification.

The last partial 429 log window has no timer flush: if traffic stops, suppressed
tail counts may never appear in a summary. Counters observe each rejection but
export loss/restart is possible. This is not durable forensic retention or an
audit partition/archive/delete policy. Existing high-volume admitted policy
audit is not redesigned.

R6 remains open: authoritative requiredness/provenance, executable independent
acceptance, derived risk, authenticated approvals and production active-release
runtime, coordinated with M11–M14. Other R7 remains open: driver-panic/process
recovery, connection and worker timing, consumer outage/replay/deadline controls,
workflow lease/cycle/history/current-document approval semantics, per-request
compiler/catalog profiling, audit retention and executed DR. Passing the current
full suite does not diagnose the historical connection or worker timing failures.
Wider search, unique-field UI/removal/evolution and query/report
features remain outside the bounded completed APIs. External identity release
evidence stays deferred. Overall M9R is PARTIAL; no M10 or main merge.

## Acceptance and Git handoff

- Actual retained API fails the raw abuse acceptance; reviewed fixed API passes.
- All exhausted gates stop SQL/audit effects; peer/global stop signature/body
  work. Concurrency, bounded identity storage, clock/TTL and metric/log tests pass.
- Queued expiry/cancellation, live revocation, required-audit failure and known-
  aborted retries preserve their contracts under populated real-PG probes.
- Full checks/tests/build, application/sign-in browser suites and all eight
  unchanged reference capacity targets pass, with diagnostic limits above.
- No customer/schema/migration/dependency/source-kit change, production rollout,
  main merge or M10. Only source, tests, public config and documentation belong
  in the designated branch; ignored generated artifacts remain local.

Stable reviewed product `8f9805e` and independent tests/report `cf45300` are
followed only by the documentation handoff. Final publication verifies remote
ancestry and the exact pushed branch HEAD; its hash is reported in the task
handoff rather than embedding a self-referential report commit hash.

**R7a STATUS: PASS. Overall M9R STATUS: PARTIAL. R6 and the remaining R7 work
remain pending; M10 is not started.**
