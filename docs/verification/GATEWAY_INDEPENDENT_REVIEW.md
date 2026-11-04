# Gateway independent adversarial review (M10/M11 governed HTTPS tool gateway)

## Progress

- 2026-10-15 20:20 UTC: started; branch `review/gateway-final` created at `fb52ad3`.
  Read the briefs, AGENTS.md, ADR036, ADR037, every `internal/tools` file and test,
  `app.go` (`newToolGateway`, `loadToolCatalog`), the tool settings in `config.go`, and
  `workflow/tool_step.go` and `execution.go`. Baseline unit tests PASS.
- Stopped by a session limit, resumed 2026-10-16 05:30 UTC.
- Batch 1 (catalog strictness, sandbox separation, ceiling, response handling, SNI/ALPN,
  path escaping, metadata coverage): done. Findings F1–F9 recorded below.
- Next: rate limiter, leaks, EffectiveMode truth table, DNS rebinding, FileSecrets races,
  config opt-in, hostile catalogs, and a race-detector run.

## Scope and commit

Reviewed commit `fb52ad3` ("docs(m12): record full integration result and decider wording").
In scope: ADR036 decisions 1, 8 and 9; ADR037 decisions 3 and 4; AGENTS.md;
`internal/tools/{gateway,netpolicy,connector,ratelimit,secrets*,catalog,executor,mode}.go`
and their tests; `internal/platform/app/app.go` (`loadToolCatalog`, `newToolGateway`);
`internal/config/config.go` (tool settings); the gateway call site in
`internal/runtime/workflow/tool_step.go`.

## Method

I read the code first, then wrote temporary probe tests (`internal/tools/zz_review_gw_test.go`,
package `tools`, no database). They reuse the package's own harness: TLS test servers on
loopback, a fake resolver, a recording secret resolver and a fake clock. Every probe
observes the wire (what the server received) or the decoded catalog, not just return
values. I deleted the probe files before the final commit; short repro snippets are below.

## Findings

### F1 — MEDIUM — Catalog keys that differ by Unicode case folding get past `strictKeys` and override security fields

- Where: `internal/tools/catalog.go:493-538` (`strictKeys`, the check at `:513` is
  `name != strings.ToLower(name)` plus an exact-string duplicate test), and
  `catalog.go:153-156` (`encoding/json` with `DisallowUnknownFields`).
- Contract: `LoadCatalog` says "Unknown, duplicate or differently cased keys ... fail
  closed" (`catalog.go:147-148`). ADR036 #1 requires a strict JSON document.
- Repro: take a valid catalog and add a second key that uses U+017F (LATIN SMALL LETTER
  LONG S, which is already lowercase) after the real key:
  `"host":"geo.example.com","hoſt":"evil.example.net"` (raw UTF-8 or the `ſ` escape).
  `LoadCatalog` accepts the file and the loaded definition has
  `Connector.Host == "evil.example.net"`. The same works for `secret_ref`, `side_effect`
  (NONE→READ), `timeout_ms`, `schema_version` and `tenants`:
  `"tenants":["1111…","aaaa…"],"tenantſ":["*"]` loads as `tenants=[*]`, so
  `AllowsTenant` returns true for every tenant. Python `json` and `jq` read the same file
  as two specific tenants plus an extra, ignored key `tenantſ`. The Kelvin-sign variant
  (`networK`) is rejected, because `strings.ToLower` changes it.
- Impact: this is a parser differential on the operator-owned security configuration.
  Anyone who reviews or lints the catalog with a different JSON tool (code review,
  `jq`, a policy checker) sees one host, tenant allowlist, credential reference or
  side-effect class, while the API compiler and the worker both load another. Fields
  that contain an `s` can be overridden this way: `host`, `secret_ref`, `tenants`,
  `side_effect`, `sandbox`, `timeout_ms`, `max_response_bytes`, `rate_per_second`,
  `replay_window_ms`, `version`, `tools`, `simulated_output` and `schema_version`.
  It does not bypass `validateDefinition`: the smuggled values must still be valid.
- Fix: make `strictKeys` reject every key that is not ASCII `[a-z0-9_]`, or compare
  `foldName`-equivalent keys (e.g. `strings.EqualFold` plus a `unicode.SimpleFold`
  canonical form) when detecting duplicates. Add the U+017F cases to
  `TestLoadCatalogRejectsMalformedDocuments`.

### F2 — MEDIUM — A sandbox endpoint can be the production host on another port (ADR037 #4 says equal hosts are forbidden)

- Where: `catalog.go:293` (`s.Endpoint.Host == c.Host && s.Endpoint.Port == c.Port`),
  `executor.go:229` (the same host-and-port test).
- Contract: ADR037 #4 says "the catalog forbids equal production/sandbox hosts and secrets."
- Repro: `writeDefinition("api.example.com", p, "api.example.com", s)` (the sandbox is
  the production host on another port) passes `NewCatalog`. `Execute(ModeReroute)` sends
  `Host: api.example.com:<s>` with the sandbox bearer token. The shipped test fixture
  `validDefinitions()` itself uses `readDefinition("api.crm.example.com", 443,
  "api.crm.example.com", 8443)`.
- Impact: sandbox traffic, including WRITE requests, can reach the production host
  whenever that host also answers on the sandbox port. "Production endpoint never used
  in a sandbox mode" holds only for the exact host:port pair, not for the host.
- Fix: refuse `s.Endpoint.Host == c.Host` whatever the port (both at `catalog.go:293`
  and as defense in depth at `executor.go:229`), or change ADR037 #4 to say host:port
  and justify it.

### F3 — MEDIUM — A sandbox mode can resolve and send another tool's production credential (and reach another tool's production endpoint)

- Where: `catalog.go:297` compares the sandbox `secret_ref` only with the same
  definition's production `secret_ref`; `catalog.go:293` does the same for the endpoint;
  `NewCatalog` (`catalog.go:167-194`) has no catalog-wide check. `executor.go:223-246`
  then trusts the sandbox ref.
- Contract: ADR036 #9 ("TEST_ACCOUNT (sandbox endpoint and sandbox-only secret)"),
  ADR037 #4 ("the gateway in a sandbox never resolves the production reference"), and
  V3 18 ("Production credentials are not mounted in ordinary sandbox execution").
- Repro: a catalog holding `crm` (production `secret_ref: crm_live`) and `pay` whose
  TEST_ACCOUNT sandbox has `secret_ref: crm_live` is accepted.
  `Execute(pay, ModeTestAccount)` makes the recording resolver log `[crm_live]`, and
  the sandbox server receives `Authorization: Bearer live-token-AAAA1111` (crm's
  production token). Likewise, a sandbox endpoint equal to another tool's production
  endpoint is accepted.
- Impact: a production credential goes out from sandbox execution to a sandbox host,
  and sandbox writes can reach a production endpoint, through one catalog entry. It
  takes an operator catalog error: no tenant can force it. But the separation is the
  stated control, and the existing recording-resolver test checks only the tool's own
  production ref.
- Fix: in `NewCatalog`, build the set of every production `secret_ref` and host and
  reject any definition whose sandbox ref or host is in it, and the reverse.

### F4 — MEDIUM — Lowering the sandbox ceiling below TEST_ACCOUNT does not withhold the test-account credential

- Where: `mode.go:42-56` (TEST_ACCOUNT under a REROUTE ceiling becomes REROUTE; under
  READ_ONLY it becomes READ_ONLY), `executor.go:232-245` (REROUTE and READ_ONLY return
  the full sandbox endpoint, `SecretRef` included), and
  `db/migrations/00046_m11_sandbox.sql:143` (the database compares mode ranks only).
- Contract: ADR036 #9 lists the modes as "REROUTE/READ_ONLY (sandbox endpoint) and
  TEST_ACCOUNT (sandbox endpoint and sandbox-only secret)". ADR037 #3–4 gives the
  ceiling order BLOCK < SIMULATE < READ_ONLY < REROUTE < TEST_ACCOUNT and says
  "The ceiling can only reduce a tool's declared sandbox mode."
- Repro: for `lookupDefinition` (declared TEST_ACCOUNT, sandbox secret `pay_sandbox`),
  `EffectiveMode(d, "REROUTE") == "REROUTE"`. The request the sandbox server receives
  in REROUTE mode is byte-identical to the TEST_ACCOUNT request (method, URI, Host,
  body, `X-Reference-Key` and `Authorization: Bearer pay-sandbox-token-DDDD4444`); the
  resolver log is `[pay_sandbox pay_sandbox]`. A READ tool declared TEST_ACCOUNT under
  a READ_ONLY ceiling also sends its sandbox bearer token.
- Impact: a sandbox created with a REROUTE or READ_ONLY ceiling, meaning no dedicated
  test-account authority, still acts as the test account at the provider. On the wire,
  narrowing from TEST_ACCOUNT to REROUTE does nothing, and for NONE/READ tools
  READ_ONLY, REROUTE and TEST_ACCOUNT are identical. Only BLOCK, SIMULATE and
  READ_ONLY-on-WRITE take capability away. The database check cannot catch this
  because it sees only the mode name.
- Fix: send the sandbox `secret_ref` only when the effective mode is TEST_ACCOUNT
  (REROUTE and READ_ONLY send no credential), or forbid a `secret_ref` on REROUTE and
  READ_ONLY sandboxes and drop the credential when narrowing. Then document the
  semantics in ADR036 #9 and ADR037 #3.

### F5 — LOW — A 2xx response's `Content-Encoding` and `Content-Type` are not checked

- Where: `executor.go:354-374`.
- Repro: a 200 response with `Content-Encoding: gzip` (or `br`) and a plain JSON body
  gives SUCCESS, and so do `Content-Type: text/html` and `application/xml`. A really
  gzip-compressed body gives UNKNOWN `invalid_response`; the existing
  `TestCompressedResponsesAreNotDecoded` covers that.
- Impact: nothing is ever decompressed, so there is no bomb. The "no compression"
  contract is only enforced on the client side, and a mislabelled body is accepted.
- Fix: treat a non-identity `Content-Encoding`, or a 2xx whose media type is not
  `application/json`, as `invalid_response`.

### F6 — LOW — Definition hash depends on how `simulated_output` values are spelled in JSON

- Where: `catalog.go:120-124` (`json.Marshal` of `json.RawMessage` compacts whitespace
  but keeps escape spellings).
- Repro: `"region":"north"` and `"region":"north"` decode to the same fixture
  but produce different definition hashes and different catalog hashes.
- Impact: re-encoding the operator file (for example a formatter that rewrites
  escapes) changes the pins of SIMULATE tools. Pinned plans then fail closed, which is
  an availability problem, not a safety one. The "canonical hash" is not canonical over
  values.
- Fix: hash the decoded fixture (`DecodeOutput` result, canonically encoded) rather
  than the raw bytes.

### F7 — LOW — `Definition.Hash` ignores a marshal error, so distinct hand-built definitions share one hash

- Where: `catalog.go:121` (`raw, _ := json.Marshal(d)`).
- Repro: two hand-built SIMULATE definitions with `RawMessage(`"north"]`)` and
  `RawMessage(`"south"}`)` pass `validateDefinition`: `decodeScalar` stops at the first
  value, and `Decoder.More` is false before `]` or `}`. `NewCatalog` accepts both, both
  hash to `sha256("")`, and a pin taken from one satisfies `Lookup` of the other.
- Impact: only Go callers that build definitions by hand can reach this, because
  `LoadCatalog` never produces such raw values. It is a pin-integrity hazard for future
  callers.
- Fix: make `Hash` return an error (or panic), and have `decodeScalar` reject any
  trailing byte that is not whitespace.

### F8 — LOW — The never-allowed metadata list misses some credential endpoints, which become reachable with the internal opt-in

- Where: `netpolicy.go:38-46` (fixed metadata set), `:52-56` (169.254/16 is "internal").
- Repro: with `internalAllowed=true`, `DeniedAddress` returns false for
  `169.254.169.252` (GKE metadata server), `169.254.0.23` (Tencent Cloud metadata) and
  other `fd00:ec2::/32` service addresses. `168.63.129.16` (Azure WireServer) is
  classified public. ISATAP-style addresses that embed an IPv4 (`…::5efe:7f00:1`) are
  public too, which is exotic and informational.
- Impact: the opt-in is refused in production (`config.go:93-95`), so this matters in
  development and test only. The README's "metadata addresses ... stay denied even
  then" holds only for the listed addresses.
- Fix: classify all of 169.254.0.0/16 and fd00:ec2::/32 as never-allowed (or document
  the list as best-effort), and consider adding 168.63.129.16.

### F9 — LOW — ADR036 #6 says a network-policy denial is FAILED retryable; the gateway makes it permanent

- Where: `executor.go:331-334` and `netpolicy.go:204-207` return `blocked`
  (FAILED, not retryable); ADR036 #6 lists "resolution, dial, policy, local limit" as
  FAILED retryable. The README table says `blocked`.
- Impact: a denied DNS answer that would recover (for example during a provider DNS
  change) fails the step permanently. That is the safe direction, but the ADR and the
  code disagree.
- Fix: align ADR036 #6 with the README (policy denial is permanent), or make it
  retryable.

Gateway review STATUS: NOT RUN (in progress)
