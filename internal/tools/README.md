# Tools — M6R delivery contracts and M10 governed execution

This package holds two layers. `delivery.go` (M6R) declares native keys, reference
lookup, dedupe windows and non-idempotent providers and validates that a declaration can
be reconciled (`Delivery.Validate`). M10 (ADR036) adds the operator catalog and the
gateway that actually performs a governed HTTPS call. Which attempt may follow which
outcome is decided by the workflow runtime over the durable ledger
(`internal/runtime/workflow.decideTool`, with its own table test), not here: an earlier
`Delivery.MayRetry` that duplicated those rules had no production caller and was removed.
Neither layer promises exactly-once external effects: a timeout or a broken connection
after a write is `UNKNOWN` until it is reconciled.

## Catalog (`catalog.go`)

Tools are registered only by the platform operator in a strict JSON document
(`tool-catalog-v1`). Tenant metadata, requests and AI output never define hosts,
methods, secrets or delivery semantics; they only reference a `tool.*` ref and
semver. `LoadCatalog`/`NewCatalog` fail closed on duplicate or differently cased
keys, unknown fields, trailing data and unsafe definitions:

- side effect `NONE|READ|WRITE` only; `WRITE` needs `approval: REQUIRED` and a
  non-GET method; financial and destructive tools wait for M13;
- delivery passes `Delivery.Validate`; `NATIVE_KEY`/`DEDUPE_WINDOW` declare a
  replay window, `LOOKUP_BY_REFERENCE` declares a `{key}` lookup path and may
  declare `lookup.settle_ms` (0..600000, omitted when zero): only with it can a
  404 lookup prove absence (ADR036 Amendment A4);
- literal DNS hosts only (no IP literals or numeric shorthand), HTTPS, timeout
  at most 2000 ms, response at most 16 KiB, per-tool rate, typed scalar inputs
  and outputs, path templates of whole `{name}` segments used exactly once;
- a tenant allowlist (`*` alone, or distinct tenant UUIDs);
- sandbox behavior; a sandbox endpoint differs from production and a sandbox
  secret is never the production secret.

Each definition has a canonical hash. It covers the decoded `simulated_output`
fixture, not the spelling of its values (an escape, padding or a null optional
member does not re-pin a tool); a definition that cannot be encoded hashes to
`tools.Unhashable`, never to a digest. Plans pin it, and `Catalog.Lookup` never
lets a changed definition satisfy an existing pin. DSL 2.2.0 plans also pin the
database-enforced contract (`workflow.ToolContractOf`: approval, side effect,
idempotency/retry mode, replay window, settle interval, timeout).

## Gateway (`executor.go`, `connector.go`, `netpolicy.go`)

`Gateway` implements `Executor`. `NewGateway(GatewayOptions{Secrets, AllowInternal,
RootCAs, Resolver, Now})` builds a dedicated HTTP client. `Execute` performs at
most one bounded attempt, never panics, honors context cancellation, and returns a
secret-free `Result` using the `Class*` constants. It blocks no longer than the
smaller of `Request.Timeout`, the connector `timeout_ms` and `MaxTimeoutMS`.

### Modes

| Mode | Behavior |
| --- | --- |
| `LIVE` | Production endpoint with the production `secret_ref`. |
| `REROUTE`, `READ_ONLY` | Sandbox endpoint only. `READ_ONLY` refuses every `WRITE` tool. |
| `TEST_ACCOUNT` | Sandbox endpoint with its mandatory sandbox-only secret. |
| `SIMULATE` | No I/O. Returns the declared fixture, `Simulated: true`. Never delivery evidence. |
| `BLOCK` and unknown | No I/O. `FAILED`, class `blocked`, not retryable. |

Sandbox modes never resolve or use the production secret reference. The runtime
selects the mode from the server-derived environment; LIVE is not reachable until
M13.

### Request construction

Inputs must already be typed and exactly match the declared names. They are
validated before any I/O. `SEND` uses the connector method; `{name}` path
parameters are `url.PathEscape`d per segment, and empty, `.`, `..` or
slash-containing values are refused. POST/PUT bodies are JSON objects of the
`body` inputs (integers as numbers, decimals as strings); GET has no body. The
idempotency header carries the request key (64 lowercase hex). `LOOKUP` is a GET of
the declared lookup path with `{key}`. Credentials are resolved at send time and
sent only as `Authorization: Bearer`.

### Network policy

Hosts are resolved by the gateway and **every** answer is validated; the
connection goes to a validated literal address, so there is no second lookup to
rebind. `DeniedAddress` refuses loopback, unspecified, RFC1918, link-local,
CGNAT, multicast, broadcast, reserved, IPv6 ULA, documentation and benchmarking
ranges, plus IPv4-mapped, NAT64, 6to4 and ISATAP forms carrying a denied IPv4
address. Internal ranges are allowed only for an endpoint
declared `internal` in a process started with `AllowInternal` (production
composition rejects it). Unspecified, multicast, broadcast, the whole IPv4
link-local block 169.254.0.0/16 (where AWS, GCP, Azure, ECS, EKS and Tencent keep
their metadata services), 100.100.100.200, 192.0.0.192, AWS's fd00:ec2::/32 and the
Azure WireServer 168.63.129.16 stay denied even then. The list is a best-effort
floor, not a proof that no credential endpoint is reachable. The address is re-checked at `connect(2)`.

The transport ignores proxy environment variables, follows no redirects, disables
compression and keep-alive reuse, speaks HTTP/1.1 over TLS 1.2+ with verification
(optional operator CA pool), bounds response headers to 16 KiB and the body to the
connector limit, and parses a single strict flat JSON object (no duplicate keys).

### Outcomes

| Situation | Outcome |
| --- | --- |
| Mode or policy forbids I/O, invalid input | `FAILED` `blocked`, not retryable |
| Resolution, dial, TLS, secret failure, expired budget before connect | `FAILED` `not_sent`, retryable |
| Local rate limit or provider 429 | `FAILED` `rate_limited`, retryable |
| Provider 4xx; any 3xx on a read | `FAILED` `rejected`, not retryable |
| Lookup 404 | `FAILED` `not_found`, not retryable |
| 408, 409, 5xx; 3xx on a write | `UNKNOWN` `server_error` |
| Failure after the connection was used; caller cancel | `UNKNOWN` `transport` |
| Deadline after the connection was used | `UNKNOWN` `timeout` |
| 2xx with oversize or invalid body, a content coding other than identity, or a media type other than `application/json` (charset utf-8 only) | `UNKNOWN` `invalid_response` |
| 2xx with a valid typed body | `SUCCESS` with the declared output only |

These are attempt outcomes. The ledger (`control.tool_finish`, ADR036
Amendments A2-A4) keeps the invocation UNKNOWN while an earlier send's outcome
is unknown: a lookup that is not a valid 2xx or a settled 404 never settles it,
and the database refuses a resend unless the pinned capability permits it.

The per-tool token bucket (`ref@version`, burst equal to `rate_per_second`) is
process-local. Each call records one `tool.execute` span with only `tool_ref`,
`tool_version`, `tool_mode`, `tool_kind`, `outcome`, `class` and `http_status`.

## Secrets (`secrets.go`)

`FileSecrets{Dir}` reads `Dir/<ref>` for refs matching `^[a-z][a-z0-9_]{0,63}$`.
It refuses symlinks, non-regular files, files readable by group or other (mode
must be 0600 or stricter on unix) and files over 4096 bytes, trims one trailing
newline, accepts visible ASCII only, and never puts a path or value in an error.

## Not in this package

Asynchronous provider callbacks and webhooks are out of scope (ADR036 decision
10). M10 has one generic HTTPS connector; M17 adds provider breadth. The intent
ledger, reconciliation routing, approval freshness and the compiler integration
belong to the workflow runtime and use `Executor`, `Catalog` and `Delivery`. See
ADR-020, ADR036 and `delivery_test.go`.
