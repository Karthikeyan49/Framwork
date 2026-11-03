# Logging, error and telemetry leak audit (final verification round)

Pinned commit: 11a4ee40a12e0301472b275203ac10d8347eaad9 (code head 585499d). Branch: audit/logging-error-leaks.
Scope: secret and internal-detail leakage through logs, errors, audit rows, traces and metrics in `cmd/` and `internal/`.
Status of this file: work in progress (see Progress); findings so far are below.

## Progress

- [x] Brief read; worktree on the pinned commit.
- [x] Static inventory of log, print, panic, error and telemetry sites (summary in "Site inventory").
- [x] Front-door canary probe (executed, unit level): PASS except LE-01.
- [ ] HTTP error mapping with the real stack and injected executor failures (integration, m13 stack).
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
