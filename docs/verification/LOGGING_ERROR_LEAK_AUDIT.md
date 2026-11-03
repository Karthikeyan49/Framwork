# Logging, error and telemetry leak audit (final verification round)

Pinned commit: 11a4ee40a12e0301472b275203ac10d8347eaad9 (code head 585499d). Branch: audit/logging-error-leaks. Auditor: independent, report-only.
Scope: secret and internal-detail leakage through logs, errors, audit rows, traces and metrics in `cmd/` and `internal/`.

## Verdict

PASS WITH FINDINGS for the executed scope: no HIGH. One MEDIUM (LE-02, a third-party raw driver error in the log stream), three LOW (LE-01, LE-03, LE-04
below). No credential, token, DSN, request body, SQL text, driver message, constraint or table name reached a response, a slog line, a span attribute, a metric
label or a startup message in any executed attack (about 330 canary-bearing requests and 38 program starts; canaries listed per sink below). The stack-level probe on the
shared m13 database (real RLS, cross-tenant existence oracles, AI request text through the real routes) did NOT run: see "Real-stack probe".

Temporary probe sources (never committed; the `zz_review_*` files named in the repros): `/home/user/audit-leaks-probes/`, same relative paths as in the tree.

## Progress

- [x] Brief read; worktree on the pinned commit.
- [x] Static inventory of log, print, panic, error and telemetry sites (next section).
- [x] Static review of every place a Go error becomes a response (`httpserver`, `access`, `signin`, `identity`): all refusals are fixed text or closed codes.
- [x] Front-door canary probe (executed, unit level): PASS except LE-01.
- [x] Startup and CLI probes with the built binaries (38 cases): PASS except LE-02.
- [x] Tool gateway and sign-in canary probes (executed, unit level): PASS.
- [x] Failing executors at every statement of three routes of the real `access.API` over a fake `database/sql` driver (224 injected failures, 8 kinds, plus
      policy panic, audit error, audit panic): PASS for responses and slog; LE-03 for spans.
- [x] Audit state: executed repros of client text stored before validation for five fields (LE-04).
- [x] Bodies of the data, metadata, release, role and team planes with canaries over the fake driver (16 requests): PASS.
- [ ] Real-stack probe (integration, m13): NOT RUN (stack lock queue), see its section.

## Site inventory

Non-test Go files in `cmd/` and `internal/`: 187. Counted with ripgrep on the pinned tree.

| class | sites | where | data each can carry |
|---|---|---|---|
| `slog` calls | 22 | httpserver 3, platform/app 11 (app.go 9, ai.go 1, operator_env.go 1), observability 4, events 2, jobs 1, runtime/workflow 1 | fixed message plus: `request_id` (server-generated hex), `trace_id` (hex, may come from the caller's `traceparent`), method, status, duration, `rejected_requests` count, service, environment, bound listen address, AI kind / model label / config SHA-256, the NAMES of operator credentials found in the environment, tool count. No error value is ever passed to a logger, except `app.go:331` (`config.Load` errors, which name a variable and never its value) |
| writes to stdout / stderr | 11 in `cmd/` | migrate (3), iam (2), worker-admin (2), event-replay (2), release-check (2) | fixed success lines; `fmt.Fprintln(os.Stderr, err)` of errors that are all fixed text (see "Startup" matrix); `migrate status` prints migration file paths from the embedded set |
| `panic(` | 7 | risk/registry.go (2, init, text of a built-in validation error), ai/schema_text.go:260 (static structure), access/access.go:80 (fixed text), re-panics at runtime/data/aggregate.go:124, access/preview.go:111, postgres/transaction.go:94 | no request or record data |
| `recover()` | 12 | httpserver (logs request id only), traffic, tools (3), ai (3), workflow, access/preview, postgres/transaction, runtime/data | the value is dropped everywhere except the re-panics |
| `errors.New` / `fmt.Errorf` | 448 | ai 145, config 47, platform 43, runtime 23, compiler 20, risk 19, intent 18, audit 17, identity 16, metadata 12, promotion 11, tools 10, cmd 26 | internal only. Reaching a client: none by text, every refusal is a typed value mapped to a fixed message |
| `.Error()` | 19 | cmd/migrate (3: version numbers by regexp), app.go:331, tools/secrets.go:21 (fixed reason), events/redis.go:77 (prefix test), risk/registry.go (init panics), ai (config 5, errors 2, service, store), promotion/decisions.go, metadata/validation.go | none logged or returned except app.go:331 |
| HTTP refusals (`denied`, `aiDeny`, `aiRejection`, `httpserver.Error/JSON`) | 138 + 21 | platform/access, signin, httpserver | all literal messages and codes, two concatenations of a closed code (`releases.go:122` `violation.Code`, `promote.go:505` closed role name) |
| span attribute calls | 62 on 27 source lines | access, runtime/data, runtime/workflow, events, tools, postgres, traffic | actor / tenant / decision / policy / dataset / workflow / event ids, hashes, compiler version, fixed labels, `tool_*` labels validated by `spanLabel`; `entity_id` only after the entity was found in the compiled program (`data/query.go:205`, `store.go:156`) |
| metric instruments | 1 custom (`framework.api.admission.rejected`: labels `reason` from a closed list of 7, `policy` constant) + otelhttp's 3 histograms (method, status, protocol, constant `server.address`, scheme) + Go runtime metrics | traffic, httpserver, observability | none |
| audit evidence sites (`audit.Entry{` and `.State.X =`) | 172 lines (175 matches) | platform/access 138 (15 files), runtime/workflow 30, events 2, cmd 2 (operator-typed reference) | ids, hashes, closed codes, counts; see LE-04 for the client text that gets through |

## Findings

### LE-01 (LOW) An unauthenticated caller decides whether a request is traced and adds up to ~8 KB of its own text to every exported span

- `internal/platform/httpserver/httpserver.go:130-131` copies the caller's `Traceparent` and `Tracestate` headers into the sanitized request that
  `otelhttp` sees, and `internal/observability/observability.go:28` samples with `ParentBased(TraceIDRatioBased(0.1))`.
- Consequences, both before any authentication: (1) the sampled flag of the caller's `traceparent` wins over the 10 % ratio; (2) a valid
  W3C `tracestate` (32 members of 250 characters, 8,159 bytes in the probe) is kept in the span context and exported with the server span and with
  every child span of the request (database transaction spans among them). The comment at httpserver.go:103-105 says that "arbitrary headers never
  become telemetry attributes"; the trace state is not an attribute, and is not mentioned.
- Repro (executed): `internal/platform/httpserver` temporary probe `TestZZTraceStateAmplification`, production sampler configuration:
  200 `GET /healthz` with `traceparent` flags `01`: 200 spans exported; 200 with flags `00`: 0 spans; 50 requests with flags `01` and a
  32-member `tracestate`: 50 spans, 407,950 trace-state bytes (8,159 bytes per span). The limiters (`traffic.Gate`) bound the request rate, not the
  telemetry bytes; the batch processor bounds memory (default queue 2,048 spans), so this is cost and noise amplification and caller text in the
  trace backend, not a secret exposure.
- Not documented as a residual in `docs/` (searched for tracestate, ParentBased, traceparent).
- Suggested direction (not applied): drop `Tracestate` from the sanitized request and either ignore the remote sampled flag for anonymous callers or
  cap the header length; keep `Traceparent` only if cross-service correlation is wanted.

### LE-02 (MEDIUM) The Redis client writes raw driver errors to stderr outside slog, and an anonymous `GET /readyz` can trigger it

- AGENTS.md ("Security and persistence"): "No secrets, tokens, DSNs, raw driver errors or customer payloads in logs/reports." The process installs no logger
  for go-redis (`grep -rn "SetLogger\|slog.SetDefault\|log.SetOutput" cmd internal` finds nothing), so the library's default writes to the standard
  `log` package, unstructured, with a source position.
- Repro (executed with the built `cmd/api`, clean environment, `DATABASE_URL`/`REDIS_URL` pointing at `127.0.0.1:1`, then one `GET /readyz`): the
  process output contains, between two JSON lines,
  `redis: 2026/11/03 06:39:10 pool.go:770: redis: connection pool: failed to dial after 5 attempts: dial tcp 127.0.0.1:1: connect: connection refused`
  (library source `github.com/redis/go-redis/v9@v9.23.0/internal/pool/pool.go:770`, `internal.Logger.Printf(... "%v", lastErr)`). `/readyz` is public
  (`httpserver.go:53`) and probes Redis through `cache.Client.Ping` (`internal/platform/cache/cache.go:26`, wired at `platform/app/app.go:288`).
- What it carries: the dial target (host and port, or the resolver's address for a name failure) and the library's file and line. No credential was in the
  line (the password is an option field, not part of the address). So the exposure is internal host names and unstructured text in the log stream, not a
  secret; the breach is of the stated rule, which is why this is MEDIUM and not HIGH.
- Same library, same default, in `internal/events/redis.go` (`OpenRedis`, started at `platform/app/app.go:198`) and the sign-in store (`cache.Open(c.SignInRedisURL)`, `platform/app/app.go:274`).
- Not found: the same kind of output from pgx (`database/sql` path), from the OpenTelemetry exporters (executed: `OTEL_EXPORTER_OTLP_ENDPOINT` unreachable plus
  `OTEL_EXPORTER_OTLP_HEADERS` carrying a canary: only `telemetry export unavailable` appears), or from `net/http` (no TLS listener; the recorder guards
  double `WriteHeader`).
- Suggested direction (not applied): `redis.SetLogger` with a discard or a static-message logger at start-up, next to `otel.SetErrorHandler`.

### LE-03 (LOW) A panic that unwinds through a span exports the panic value on the span (OpenTelemetry SDK `End()` records it)

- `internal/platform/postgres/transaction.go:90` (`defer span.End()` of `postgres.transaction`) and `:139-140` (`defer bodySpan.End()` of
  `postgres.transaction.body`) wrap every handler body. The SDK's `End()` recovers a panic in flight, adds an `exception` event with `exception.type` and
  `exception.message` (`fmt.Sprint` of the panic value) to the span, and re-panics. The code's own intent is the opposite: `transaction.go:93` sets the fixed
  status "transaction panicked", `httpserver.go:194` logs only the request id, and ADR014 (docs/adr/014-m1-identity-policy-audit.md:57) says spans carry trusted
  actor/tenant/decision/policy ids.
- Repro (executed, no database needed): `internal/platform/access` temporary probe over a fake `database/sql` driver; the real `access.API` with a policy engine
  that panics with a run-time canary. Response: `500 internal_error "An internal error occurred"` (correct). Exported spans: `postgres.transaction.body` and
  `postgres.transaction` each carry `exception.message = "policy engine panic <canary>"` and `exception.type`. The SDK adds no stack trace in this version
  (v1.47.0). The same for an audit writer that panics. No canary reached the response or the slog output.
- Impact now: the panic texts that can reach this path in the current tree are runtime-error texts and the re-panics of `runtime/data/aggregate.go:121-124` and
  `access/preview.go:109-111`; none of the seven `panic(` sites carries request or record data. So the exposure is an unplanned channel, not an observed
  secret. It becomes a leak the day a panic value carries data. The export is gated by sampling, which an anonymous caller can force (LE-01).
- Suggested direction (not applied): the SDK records the exception only because `End` itself recovers the panic; end these spans from a deferred function
  that recovers first, sets the fixed status, ends the span and re-panics; add a regression test that panics with a canary and scans the exported spans.
- Related, already documented (AGENTS.md "Injected driver/tracer panic is an unresolved R7 recovery limitation"): a panic raised inside a `database/sql` call
  leaves the deferred `tx.Rollback()` (`transaction.go:132`) blocked on `database/sql.(*closingMutex).Lock`. Observed here as well (goroutine dump of a 15 s
  run: `database/sql.(*Tx).rollback` waiting in `closemu.go:79`); the handler goroutine and its admission slot are lost. Not reported as new.

### LE-04 (LOW) Client text is copied into audit state before it is validated; only its length is bounded

- `internal/audit/audit.go:1` calls the package "bounded, secret-free security evidence" and the AI and promotion fields are closed codes, hashes and labels
  (`validPromotionEvidence` `audit.go:195`, `validAIEvidence` `audit.go:216`). Most of the other string fields are bounded by length only (`encodeEntry`, `audit.go:143-192`), and
  several are filled from the request before the object is looked up:
  `state.role` <- body `role` of the role-assignment route (`access/roles.go:82`, before `store.Assign` validates the name, and kept on the rejection path);
  `state.dataset_id` <- path (`access/evolutions.go:54`); `state.workflow_id` <- path (`access/workflows.go:291`, after a length test only);
  `state.entity_id` and `state.field_id` <- path of the GET index routes and `entity_id` of the GET record-access route (`access/preview.go:154-155`, `:225-226`, `:232`;
  `access/records.go:60`); `state.operator_ref` <- body of a workflow signal, charset-checked only for `recover` and `reconcile` (`runtime/workflow/signals.go:16-18` against `:188`).
  POST routes are protected by the allowlist regexes of `AcceptsPOST` (identifier-shaped app, entity, rule and workflow segments); GET routes and body fields are not.
- Repro (executed, no database; fake `database/sql` driver that serves the stored contacts example as revision 1, real `access.API`, real registry, compiler and
  policy; temporary probes `zz_review_audit*_test.go` in `internal/platform/access`, not committed). Each request answers a fixed typed refusal and appends an audit row
  that holds the text:
  `POST /v1/tenants/{t}/members/{m}/role` with `{"role":"Not A Role <canary>","expected_role":"viewer"}` -> `400 invalid_role`; row `roles.assign.rejected`,
  `state.role = "Not A Role <canary>"`.
  `GET /v1/tenants/{t}/preview/app.x/datasets/<canary>/evolutions/<uuid>` -> `400 invalid_evolution`; row `datasets.evolve.rejected`, `state.dataset_id = "<canary>"`.
  `GET .../versions/1/entities/<canary1>/indexes/<canary2>` -> `400 invalid_index`; row `indexes.manage.rejected`, `state.entity_id = "<canary1>"`, `state.field_id = "<canary2>"`.
  `GET .../versions/1/entities/<canary>/records/<uuid>/access` -> `400 invalid_record_access`; row `preview.read.rejected`, `state.entity_id = "<canary>"`.
  `GET .../versions/1/workflows/<canary>/instances/<uuid>` -> `404 not_found`; row `workflows.rejected`, `state.workflow_id = "<canary>"`.
  Read from the code, not executed: `state.operator_ref` from the body of a workflow signal.
- Impact: audit rows are insert-only and no API route reads them, so the text reaches only whoever queries the table. A pasted secret in a `role` field of a
  failed assignment, or in a GET path, is stored for good in an immutable table (up to 64 or 160 bytes). A value longer than the bound makes `Append` fail and the
  request answers `503 service_unavailable` instead of the typed 4xx (an availability quirk of the same cause; no leak).
- Suggested direction (not applied): copy a path or body value into the state only after the lookup succeeded, or store its SHA-256 for the rejected case; give
  `Role`, `OperatorRef`, `EntityID`, `FieldID`, `WorkflowID` and `DatasetID` the identifier charset check that the AI labels already have.

## Canary matrix (front door, executed)

Run-time random canaries (prefix plus 12 random bytes in hex) planted in: request path segment, query string, `User-Agent`, `Referer`, `Cookie`,
`Authorization: Bearer`, `X-Forwarded-For`, client-chosen `X-Request-ID`, `Origin` (not allowed), `Host`, POST body (refused and accepted routes),
`Baggage`, `Tracestate`, an arbitrary custom header, and a panic value (handler panic, probe error). Looked for in: response body and headers (9
requests: 400, 404, 405, 413, 403 origin, 500 panic x2, healthz, readyz 503), captured slog JSON output, every span (name, attributes, events, status
description, resource, trace state), every metric data point attribute.

| planted | response | slog | span attributes / names | metrics | span trace state |
|---|---|---|---|---|---|
| path, query, user-agent, referer, cookie, authorization, x-forwarded-for, client x-request-id, origin, host, body, baggage, custom header | not found | not found | not found | not found | not found |
| handler panic value, probe error text | not found | not found (log line holds request_id only) | not found (status text is "internal error"-class only) | not found | not found |
| tracestate | not found | not found | not found | not found | FOUND (LE-01) |

Observed and not a finding: server spans carry `network.peer.address`, `network.peer.port` and `client.address` (the TCP peer, not a header),
`url.path` is the constant `/unmatched` (or `/healthz`, `/readyz`), `server.address` is the constant `framework-api`.

## Canary matrix (startup, configuration and command-line programs, executed)

Built from the pinned tree: `cmd/api`, `cmd/migrate`, `cmd/iam`, `cmd/worker-admin`, `cmd/event-replay`, `cmd/release-check`, `cmd/worker`,
`cmd/dispatcher`. Run with `env -i` (no inherited secrets), a run-time canary (`ZZS` + 20 hex) inside every credential position, unreachable
endpoints only (`127.0.0.1:1`). 38 cases; output of every case searched for the canary.

| planted | programs and cases | found in output |
|---|---|---|
| password inside `MIGRATION_DATABASE_URL` (unreachable, malformed port, not a URL, production without TLS) | migrate up, migrate status | no (static: "migration database unavailable", "... must be a valid PostgreSQL URL") |
| canary in every operator flag (subject, name, tenant, role, operator, reason, principal, event) | iam, worker-admin, event-replay | no (static texts) |
| canary as a numeric flag value | event-replay `-generation` | YES: Go's `flag` package echoes `invalid value "<value>" for flag -generation: parse error` to the operator's own terminal. Not a finding (the operator typed it); listed for completeness |
| password inside `DATABASE_URL`, `REDIS_URL`, `EVENT_REDIS_URL`, `SIGNIN_REDIS_URL`, `AUTH_ISSUER`, `OTEL_EXPORTER_OTLP_ENDPOINT`; canary as `LOG_LEVEL`, `HTTP_ADDR`, `APP_ENV`, `API_DB_CONCURRENCY`, `WORKFLOWS_ENABLED`, `OIDC_CLIENT_ID`, `CORS_ORIGINS`, file paths for `AI_PROVIDER_FILE`, `TOOL_CATALOG_FILE`, `TOOL_CA_FILE`, `AUTH_JWKS_FILE` | api (18 configuration cases), production with insecure URLs, worker, dispatcher | no (the configuration error names the variable, never the value; a file or dependency failure logs one fixed line) |
| operator credentials (`MIGRATION_DATABASE_URL`, `POSTGRES_PASSWORD`) in a runtime process | api | no (names only, `operator_env.go:33`) |
| canary in path, query, bearer, body, user-agent against a running API with dead dependencies | api `/readyz`, POST, `/v1/session` | no in the responses; no in the log (but see LE-02) |
| `OTEL_EXPORTER_OTLP_HEADERS` carrying a canary plus an unreachable collector | api | no (`telemetry export unavailable`) |

## Canary matrix (tool gateway, executed, `internal/tools` harness)

Planted: bearer secret (read from the secret file at send time), tool arguments (body text and a path parameter), the provider's error body, an
`X-Error-Detail` header, a `WWW-Authenticate` header and a `Set-Cookie` header. Ten provider answers (400, 401, 403 HTML, 429, 500, 502, 200 with an
undeclared field, 200 with a wrong type, 200 not JSON, 201 with the canary in a declared output field), a closed port and a resolver failure.
Looked for in the `Result` (`%+v` and `%#v`) and in every span attribute, event and status.

| planted | Result | spans |
|---|---|---|
| bearer secret, both tool arguments, provider headers and cookie | not found | not found |
| provider error body (all nine failing answers, resolver failure) | not found | not found |
| provider body in a declared output field of a 201 | found in `Output` (the contract: typed output) | not found |

## Canary matrix (sign-in, executed, `internal/platform/signin` with a fake identity provider)

Planted: authorization code, state, issuer, cookie, unknown JSON field, malformed body, foreign origin, query string, and the identity provider's answers
(400 error body with `error_description`, 500 with a body, 200 with a canary access token and ID token, 200 with token type `mac`). Every failure
answered one of four fixed bodies (`sign_in_unavailable` 503, `sign_in_rejected` 401, `invalid_sign_in` 400, `origin_denied` 403); no canary in any response
or in the captured log.

## Canary matrix (failing executors, executed over a fake driver, real `access.API`)

Routes: `GET /v1/session`, `GET /v1/tenants/{t}/access`, `GET /v1/tenants/{t}/members` with a valid RS256 token whose subject and query string carry canaries.
The fake driver completes each flow (baseline 200 for all three), then fails the n-th statement of the transaction (n = 1 until the flow has no n-th statement)
with: a `pgconn.PgError` whose message, detail, hint, schema, table, column, constraint, where and internal query all carry a canary, for codes `XX000`,
`23505`, `23503`, `55000`, `42501`, `40001`, `22023`; or a plain error naming an internal host and `password=<canary>`.
| failure | injected | response | slog | spans |
|---|---|---|---|---|
| driver error, 8 kinds, every statement | 224 | all 224: `503 service_unavailable "A required service or security transaction is unavailable"`; no canary, no marker (`SQLSTATE`, `pgconn`, `violates`, `constraint`, `iam.`, `.go:`, `goroutine`, `password=`) | only the access line (`http request`, status 503); no error line at all | status description "transaction failed" only |
| policy engine panics | 3 | `500 internal_error` | `request panic recovered` + request id | canary in `exception.message` of two spans (LE-03) |
| audit writer returns an error with a canary | 3 | `503 service_unavailable` | none | none |
| audit writer panics | 3 | `500 internal_error` | `request panic recovered` | canary in `exception.message` (LE-03) |

Observation (not a leak): a 503 caused by a database or audit failure leaves no log line beyond the access line, so an operator cannot tell the causes apart
from the log alone; the closed-vocabulary error class that would help is absent. The price of that silence is zero leakage.

## Consolidated canary matrix (planted where -> looked for where -> found?)

Canaries are generated at run time (prefix plus 10 random bytes in hex, or `ghp_`-style shapes from the system random source); none is in a file.
"found" means found outside the place the contract allows. Sinks: R = response body and headers, L = slog output, S = span names, attributes, events,
status and trace state, M = metric data point attributes, A = audit row, P = provider-bound request, O = program output (stdout and stderr).

| planted in | R | L | S | M | A | P / O |
|---|---|---|---|---|---|---|
| request path segment, query string, `User-Agent`, `Referer`, `Cookie`, `Authorization`, `X-Forwarded-For`, client `X-Request-ID`, `Host`, `Origin`, body, `Baggage`, custom header (front door, 9 requests) | no | no | no | no | not run (see Real-stack probe) | - |
| `Tracestate` header | no | no | YES (LE-01, by design of W3C propagation, unbounded by the code) | no | - | - |
| panic value of a handler (front door, recovered by `recoverer`) | no | no | no | no | - | - |
| panic value of the policy engine or the audit writer (under `postgres.transaction`) | no | no | YES (LE-03) | no | - | - |
| driver error text, all `PgError` fields, plain error with host and password (224 injections over 3 routes) | no | no | no | no | - | - |
| bearer secret, tool arguments, provider error body, headers and cookie (tool gateway, 12 executions) | no | - | no | - | - | - |
| authorization code, state, issuer, cookie, provider error description, provider tokens (sign-in, 13 requests) | no | no | - | - | - | - |
| passwords in `DATABASE_URL`, `REDIS_URL`, `EVENT_REDIS_URL`, `SIGNIN_REDIS_URL`, `MIGRATION_DATABASE_URL`, `AUTH_ISSUER`; canary values for 12 other variables; operator flags (38 starts) | no | no (but the go-redis line of LE-02) | - | - | - | no (one echo of an operator's own numeric flag) |
| role text in a rejected role assignment (body); dataset, entity, index and workflow path text in rejected GET requests | no | no | - | - | YES (LE-04, 5 fields, executed) | - |
| `OTEL_EXPORTER_OTLP_HEADERS` value with an unreachable collector | - | no | - | - | - | no |

## Mutation check of the probes

A probe that cannot fail proves nothing. With `internal/platform/access/access.go:423` temporarily changed to append `err.Error()` to the 503 message, the
failing-executor probe failed with `response ... contains a canary`, `contains marker "db.internal"` and `contains marker "password="` (executed, then reverted with
`git checkout -- internal/platform/access/access.go`). The span probes found the two real exceptions (tracestate, exception events) and the audit probes found the
two stored texts, so each of the span and audit sinks has a positive control; the slog sink has one in the CLI case (`event-replay` echo) and the mutation above.

## Observations (not findings)

- `GET /readyz` is public and lists twelve dependency names with `ok` or `unavailable` (`postgres`, `redis`, `workflows`, `signin`, `policy`, `events`, `identity`,
  `tenant`, `audit`, `data`, `metadata`, `ai`); text of the failure is never included (`httpserver.go:53-94`, executed: `{"status":"not_ready","dependencies":{...}}`).
  Unconfigured `signin` and `ai` report `ok`, so the list does not reveal configuration.
- An anonymous caller can tell a development or test deployment from production by `POST` with a body to a preview or AI route: `401 unauthenticated` when preview
  is enabled, `413 body_not_allowed` when it is not (`AcceptsPOST` allowlist, executed with `PreviewEnabled` true and false). It reveals the mode and which route
  groups are registered, not a tenant, user or record.
- Server spans carry `network.peer.address`, `network.peer.port` and `client.address` (the TCP peer; behind a proxy that is the proxy). Not secrets; not in the
  list of excluded attributes in the comment at `httpserver.go:103-105`.
- Tenant resolution answers `403 forbidden "Access is denied"` for a random UUID, a UUID of an existing tenant the caller does not belong to, and a malformed
  identifier alike (`tenant/tenant.go:80-101`: `ValidID` failure, no row and scope mismatch all return `ErrDenied`); verified by code reading, executed in the
  real-stack probe only if that section says so.
- The access log line carries the `trace_id` of the caller's `traceparent` when one is sent (32 hex characters, validated by the parser): a caller can choose the
  value of that field, but only from the hex alphabet.
- `validRequestText` (`ai/service.go`) states that an AI request text is never stored, only its hash; the existing M14 test
  `TestM14APIAuditCoversEveryOutcomeAndLeaksNoText` (`tests/integration/m14_api_audit_test.go:18`) scans audit rows and AI tables for a request canary. I read it and did
  not re-run it.
- The redactor (`internal/ai/redact.go`) is a documented mechanical filter (ADR104 residuals); its coverage was not part of this audit.

## Bodies of the other planes (executed over the fake driver, real `access.API`, real registry and compiler)

16 requests as an owner with the stored contacts example as revision 1: data create with a canary value and an unknown field name, unknown operation, query with
unknown projection, sort and filter fields, malformed JSON, duplicate keys, wrong content type; metadata append with an invalid document, YAML with an alias,
malformed JSON; release candidate, acceptance spec and requirements with garbage; role publish with a garbage definition; team create with garbage; role contract with
a canary query; metadata list with a canary cursor. Every request answered a fixed typed refusal (`invalid_query_or_command`, `invalid_metadata`,
`invalid_release_request`, `invalid_role`, `invalid_team`, `invalid_cursor`). Ten canaries (value, unknown field, key, operation, sort, filter, document key,
definition, note, spec) were looked for in the responses, the slog output, every span and the 17 audit rows these requests appended (closed action names such
as `preview.write.rejected`, `metadata.write.rejected`, `roles.write.rejected`): none found anywhere.

## Real-stack probe (m13 database stack)

NOT RUN. Reason: the shared m13 stack lock and the global heavy lock were held by other agents' integration runs for the whole session (one run had waited more than
30 minutes when I stopped); a queued `it-run` of my probe never reached its compile step and I cancelled it so that it would not start unattended later. What this leaves
unchecked (everything else in this report was executed without a database): row-level-security behaviour behind the tenant, user and record existence answers
(read from the code only: `tenant/tenant.go:80-101` returns `ErrDenied` alike for a malformed id, an unknown id and a foreign tenant; `member_not_found` and
`not_found` are produced after the RLS-scoped lookup), the AI request-text shapes through the real creation route and the provider-bound request, and the audit rows
of the 8 routes under real database failures.
The probe is written and type-checked (`go vet -tags=integration ./tests/integration`); it is NOT in the commit. Copy of the temporary files:
`/home/user/audit-leaks-probes/` (same relative paths). To run it: copy `tests/integration/zz_review_leaks_test.go` back into the tree and run
`RESET=0 /home/user/bin/it-run m13 <worktree> <log> '^TestZZLeakMatrix$' -v -timeout 14m`; it prints `CANARY <name> found in: [...]`, the oracle comparisons and
the distinct log lines, and finishes within 5 minutes (its failure-injection loop stops at 4 minutes). It deliberately has no driver-panic kind: a panic inside a
`database/sql` call blocks the request forever (documented R7 limitation, observed in LE-03).

## NOT RUN

- The real-stack probe above.
- A production-mode start (`APP_ENV=production`): needs HTTPS issuer, verified TLS URLs and a key set; only its configuration refusals were executed.
- A live OTLP collector (only an unreachable one), a live identity provider (a fake one was used), a real AI provider (the HTTP adapter is covered by the builder's
  own tests `provider_http_test.go`, which I ran: `TestHTTPProviderStatusMapping`, `...CredentialIsReadPerCallAndNeverRetained`, `...ConnectionFailureIsTransient`,
  `...BehindTheGatewayMatchesTheScriptedProvider` all PASS).
- Tool arguments through a workflow tool step with a catalog; only `tools.Gateway.Execute` was exercised (the step stores `outcome/class` in the audit state,
  `runtime/workflow/tool_step.go:350`, closed values).
- `cmd/migrate up|status` against a reachable database (forbidden: no `make migrate`, no other stack), so goose's own output was not observed.
- Timing and size oracles.
- The same go-redis log line for the events transport and the sign-in store (same library and default; executed only for the main Redis client through `/readyz`).
- `operator_ref` in a workflow signal (read from the code).
