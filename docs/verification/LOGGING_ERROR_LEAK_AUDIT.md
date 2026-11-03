# Logging, error and telemetry leak audit (final verification round)

Pinned commit: 11a4ee40a12e0301472b275203ac10d8347eaad9 (code head 585499d). Branch: audit/logging-error-leaks.
Scope: secret and internal-detail leakage through logs, errors, audit rows, traces and metrics in `cmd/` and `internal/`.
Status of this file: work in progress (see Progress); findings so far are below.

## Progress

- [x] Brief read; worktree on the pinned commit.
- [x] Static inventory of log, print, panic, error and telemetry sites (summary in "Site inventory").
- [x] Front-door canary probe (executed, unit level): PASS except LE-01.
- [x] Startup and CLI probes with the built binaries (38 cases): PASS except LE-02.
- [x] Tool gateway and sign-in canary probes (executed, unit level): PASS.
- [x] Failing executors at every statement of three routes of the real `access.API` over a fake `database/sql` driver (224 injected failures, 9 kinds,
      plus policy panic, audit error, audit panic): PASS for responses and slog; LE-03 for spans.
- [ ] HTTP error mapping and audit-row probe with the real stack (integration, m13 stack; queued behind other agents at the time of writing).
- [ ] Audit-row vocabulary probe with canaries in paths and bodies (integration).
- [ ] Final write-up.

## Findings

### LE-01 (LOW) An unauthenticated caller decides whether a request is traced and adds up to ~8 KB of its own text to every exported span

- `internal/platform/httpserver/httpserver.go:130-131` copies the caller's `Traceparent` and `Tracestate` headers into the sanitized request that
  `otelhttp` sees, and `internal/observability/observability.go:28` samples with `ParentBased(TraceIDRatioBased(0.1))`.
- Consequences, both before any authentication: (1) the sampled flag of the caller's `traceparent` wins over the 10 % ratio; (2) a valid
  W3C `tracestate` (32 members of 250 characters, 8,159 bytes in the probe) is kept in the span context and exported with the server span and with
  every child span of the request (database transaction spans among them). The comment at httpserver.go:117-119 says that "arbitrary headers never
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
  (`httpserver.go:51-90`) and probes Redis through `cache.Client.Ping` (`internal/platform/cache/cache.go:24`).
- What it carries: the dial target (host and port, or the resolver's address for a name failure) and the library's file and line. No credential was in the
  line (the password is an option field, not part of the address). So the exposure is internal host names and unstructured text in the log stream, not a
  secret; the breach is of the stated rule, which is why this is MEDIUM and not HIGH.
- Same library, same default, in `internal/events/redis.go` (`OpenRedis`) and the sign-in store (`cache.Open(c.SignInRedisURL)`, `app.go`).
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
  `access/preview.go:109-111`; none of the six `panic(` sites carries request or record data. So the exposure is an unplanned channel, not an observed
  secret. It becomes a leak the day a panic value carries data. The export is gated by sampling, which an anonymous caller can force (LE-01).
- Suggested direction (not applied): end these spans through a helper that recovers, records a fixed event and re-panics, or use
  `trace.WithAttributes`-free span options; add a regression test that panics with a canary and scans the exported spans.
- Related, already documented (AGENTS.md "Injected driver/tracer panic is an unresolved R7 recovery limitation"): a panic raised inside a `database/sql` call
  leaves `Conn.Close` blocked on the leaked read lock. Observed here as well: the first run of the fake-driver probe with a driver-level panic did not finish in
  6 minutes (killed); the handler goroutine and its admission slot are lost. Not reported as new.

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
