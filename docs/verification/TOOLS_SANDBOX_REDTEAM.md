# Red team of the M10 tools gateway and the M11 sandbox (final verification round)

## Progress

- Pin `11a4ee4` (code head `585499d`), branch `review/tools-sandbox-redteam`, worktree `/home/user/review-tools`.
- Read: ADR100, ADR101, every non-test file of `internal/tools`, `internal/runtime/workflow/{tool_step,execution,signals}.go`, migrations 00080, 00081, 00085, 00086, `internal/platform/access/sandboxes.go`, the policy actions, `internal/compiler/workflow_tools.go`, the gateway wiring in `internal/platform/app/app.go` and `internal/config/config.go`, and the three earlier reviews (gateway F1-F9, M10R-01..05, M11 R01-R07: not rediscovered; their fixes were re-attacked).
- No database stack in this round: every executed probe is a unit test with loopback httptest/TLS listeners, run through `/home/user/bin/gt`. Everything that needs PostgreSQL is a static reading and is labelled NOT RUN.
- Status: COMPLETE.

## Verdict

| Scope | Verdict |
|---|---|
| M10 gateway (SSRF, injection, response handling, credentials, timeouts, leaks) | PASS WITH FINDINGS (TS-02 MEDIUM, TS-03/04/05 LOW) |
| M10 runtime protocol and M11 freeze (static only, no database) | PASS WITH FINDINGS (TS-01 MEDIUM, static, not executed) |
| Earlier fixes (gateway F1-F8, M11 R07 credential narrowing) | HELD at the pin |

No HIGH finding. No credential exposure, tenant-isolation break or SSRF bypass was found.

## Method

Static reading first, then temporary probe files `internal/tools/zz_review_ts_{a,b,c,e,f}_test.go` and `internal/compiler/zz_review_ts_d_test.go`. They are untracked and are not committed. The probes read the wire with a raw TLS listener (exact bytes of request line, headers and body) or with the harness' TLS test servers and classify the observed `Result`. Two temporary mutations of `internal/tools/executor.go` were applied and reverted (`git diff` empty afterwards) to show that the probes discriminate:

| Mutation | Probe | Observed |
|---|---|---|
| drop `endpoint.SecretRef = ""` of the `ModeReroute` case (executor.go:241) | `TestZZReviewCredentialMatrix` | FAIL: `credential presence true / refs [pay_sandbox] does not match mode REROUTE` (violations=1) |
| drop `CheckRedirect` (executor.go:89) | `TestZZReviewRedirectsAndProxy` | FAIL: `status 301: server saw 4 requests` (expected 2) |

Existing suite at the pin: `gt test -count=1 -skip TestZZReview ./internal/tools` PASS (4.8 s).

## Results per class

| Class | Items executed | Outcome |
|---|---|---|
| SSRF: catalog host forms | 64 host spellings through `validEndpoint` (decimal, octal, hex, short IPv4, IPv6 literals and mapped forms, userinfo, `#`, `?`, `/`, port, trailing dot, uppercase, IDN, NUL, CR/LF, backslash, percent forms, `nip.io` style) | PASS: 54 rejected; the 10 accepted are DNS-shaped names (`foo.localhost`, `169.254.169.254.nip.io`, `127.0.0.1.example.com`, `metadata.google.internal`, `1.2.3.1e1`, `1.1.1.0b1`, `1.1.1.0o7`, `a.b`, a punycode name, a 26-label name); they are decided by the dial-time check, not by the catalog |
| SSRF: address classification | 47 boundary IPv4 samples (first and last address of every special-purpose block, every metadata address, adjacent public controls) x 9 embedding forms (mapped, NAT64, 6to4, ISATAP x3, IPv4-compatible, SIIT, Teredo); sweep of all 2^24 /24 prefixes x 2 addresses against an independent oracle of the IANA special-purpose blocks | PASS: 0 form violations, 0 leaks (12.8 s); never-class addresses stay denied with `internalAllowed=true` |
| SSRF: redirects and proxies | 301/302/303/307/308 with a same-origin `Location` on a WRITE and on a READ tool; `HTTPS_PROXY`, `https_proxy`, `HTTP_PROXY`, `http_proxy`, `ALL_PROXY`, `all_proxy` set to a recording listener | PASS: one request per `Execute`, never a follow-up; WRITE -> UNKNOWN `server_error`, READ -> FAILED `rejected`; the proxy accepted 0 connections (mutation-checked) |
| SSRF: DNS rebinding, Host override | code read (`netpolicy.go:195-237`: one resolution per dial, connection to the validated literal, literal re-validated in `controlContext` before `connect(2)`); existing `TestResolutionIsOneShotSoRebindingCannotChangeTheDestination` and `TestPublicEndpointRejectsAddressesRevalidatedAtConnect` re-run inside the package run; `Host` observed on the wire in every probe (`api.example.com:<port>`) | PASS (an own rebinding variant is NOT RUN: loopback only) |
| Header, path and body injection | 45 hostile path values (CR, LF, CRLF, NUL, `%0d%0a`, `%00`, `.`, `..`, `%2e%2e`, `..;`, `/`, `\`, `//host`, `?`, `#`, `;`, `@`, `:`, `%`, `%u002e`, U+202E, U+2215, U+2028, BOM, DEL, ESC, a smuggled second request) captured raw: 8 refused before any I/O, 37 sent percent-encoded; request line always 3 tokens, no raw CR/LF/NUL in the head, header names exactly {Host, User-Agent, Accept, Content-Type, Content-Length, Idempotency-Key, Authorization, Connection}. 11 hostile body strings (quote breakout `"},"admin":true`, newline, U+2028/2029, `</script>`, NUL, invalid UTF-8, CESU surrogate, JSON text): body always valid JSON with exactly the 4 declared members, 3 refused. 28 input type confusions (float64, uint, int8, `json.Number("1e3")`, numeric string for an integer, named string type, `[]byte`, `RawMessage`, map, slice, pointer, 2^53, decimal exponent/space/plus/leading zero) | PASS (one hand-built-caller oddity, see Observations) |
| Response handling | 44 response shapes (content type missing, duplicate, comma, parameters, charset, case; content coding variants; BOM; duplicate and escaped-duplicate keys; other-case key; nested unknown member; trailing garbage; two objects; array; empty; null; number for decimal; null required; one byte over the cap and exactly at it; status 202/203/204/206/226/304/404) | PASS except TS-04 (invalid UTF-8) and TS-05 (202/203/206/226) |
| Slow and endless providers | header drip, body drip, chunked without end, endless 1xx stream, 100000 x 4 KiB chunked body, `101 Upgrade`, header flood; deadline 600 ms | PASS: the three drips end at 600 ms (UNKNOWN `timeout`), the others in <= 10 ms |
| Leaks | 120 calls (8 concurrent) against 5 hostile providers (stall before any byte, stall mid-body, oversize, immediate close, redirect); stack scan for `internal/tools`, `net/http.(*Transport)` and `net/http.(*persistConn)` goroutines before and after | PASS: 0 before, 0 after |
| Credentials | matrix of 3 tools (WRITE/REROUTE, READ/READ_ONLY, WRITE/TEST_ACCOUNT) x 8 ceilings (`""`, BLOCK, SIMULATE, READ_ONLY, REROUTE, TEST_ACCOUNT, `LIVE`, `garbage`): production host never contacted, no production secret resolved, `Authorization` on the wire exactly when the effective mode is TEST_ACCOUNT, `EffectiveMode` never LIVE (24 rows, 0 violations; mutation-checked). `FileSecrets` swap race: a goroutine swaps a regular secret file and a symlink to a foreign secret over the same name for 3 s while `Resolve` loops. No logging call in `internal/tools` (grep for `slog`, `log.`, `fmt.Print`, `Errorf` is empty); `Result` carries no error text | PASS: legit 43 227, refused 449 442, foreign 0 |
| Catalog key folding, refs, versions | 17 catalog variants (U+017F key raw and escaped, Kelvin sign, uppercase key, exact and escaped duplicates, NUL key, trailing document, BOM, unknown key, `*` plus a tenant, uppercase tenant, null tenants, nesting 40, invalid UTF-8 value); 16 hostile refs; 11 hostile versions | PASS: 16 of 17 variants rejected (the accepted one is TS-04's catalog sibling); only the valid ref and the versions `1.0.0`, `1.00.0` accepted (Observations) |
| Side-effect classes, retries | `validateDefinition` (re-run by the executor on every call: FINANCIAL/DESTRUCTIVE refused, WRITE needs approval and a non-GET method, GET only for NONE/READ); `decideTool` (tool_step.go:115-155) read against the SQL admission of `control.tool_begin` (00086:207-217): the two agree except that the SQL admits a retryable FAILED resend without checking `RetryMode=NEVER` (Go checks it), which is the compromised-worker residual only | PASS (static) |
| Rate limit across tenants | `TestZZReviewRateLimiterSharedAcrossTenants` | FAIL: TS-03 (LOW) |
| Tenant allowlist at compile | `TestZZReviewCompileIgnoresTenantAllowlist` | FAIL: TS-02 |
| Workflow termination with an outstanding unknown outcome | static (execution.go, signals.go, 00086, 00020) | FAIL: TS-01 (static; NOT RUN against PostgreSQL) |
| Approval gate at `control.tool_begin`, exactly-once of the ledger under concurrent leases, ledger and sandbox tenant isolation (RLS), freeze/evidence TOCTOU, fence triggers, evidence hash binding, HTTP routes `sandbox.read`/`sandbox.manage`, worker decision for `tools.invoke` | read in 00085/00086, `sandboxes.go`, `access.go:573-578`, `policy.go:79-99`, `definition.go:164-175`; the earlier fix tests exist | NOT RUN (no database or HTTP stack in this round) |

## Findings

### TS-01 — MEDIUM (static, not executed) — a workflow that becomes terminal while its tool ledger is `SENDING` or `UNKNOWN` can never be reconciled, and in a sandbox it blocks the freeze for good

Contract: ADR100 Amendment A3 (every terminal failure of a tool step while an unknown outcome is outstanding parks in RECONCILE, "unknowns never die") and ADR101 Amendment 2 ("the operator settles the outcome (reconcile, or wait for the attempt) and freezes again").

Where:
- `internal/runtime/workflow/execution.go:101-103`: the workflow deadline is tested before any tool or ledger logic. Any status except COMPENSATING with `now >= x.Deadline` becomes `Termination=TIMED_OUT`, then COMPENSATING, then terminal `TIMED_OUT`. Nothing reads `runtime.tool_invocations`. The job is made due at `least(due_at, deadline)` (`00020_m9_workflow_runtime.sql:157`), so even a RETRY_WAIT whose `due_at` lies after the deadline (lookup settle wait, backoff) is claimed at the deadline.
- `internal/runtime/workflow/signals.go:105-110`: `cancel` is accepted for every status except COMPENSATING and a terminal one other than DEAD, so it is accepted in READY and RETRY_WAIT, also in a RETRY_WAIT that exists because a keyed resend or a lookup is pending after an UNKNOWN outcome. `policy.go:112-115` gives `workflows.cancel` to every non-viewer.
- `signals.go:130-131` and `db/migrations/00086_m11_review_fixes.sql:289`: `reconcile` and `control.tool_reconcile` require `status='RECONCILE'`. `control.tool_abandon` (00086:265-280) needs the worker lease, and the job of a terminal instance is `FINISHED` (`00020_m9_workflow_runtime.sql:156`), so no worker leases it again. `recover` (`signals.go:114-129`) is limited to DEAD, COMPENSATION_FAILED and routing DEAD/BLOCKED.
- `00086:124-128`: `freeze_sandbox` refuses (55000 "sandbox has outstanding tool work; settle it before freezing") while any invocation of the dataset is `SENDING` or `UNKNOWN`, whatever the status of its instance.

Impact: the provider may have applied the effect; the ledger keeps `UNKNOWN`/`SENDING` as evidence, but no operator path can ever record APPLIED or NOT_APPLIED, and a sandbox that holds such a row can never be frozen, so it can never become M12 evidence (only a new sandbox helps). The deadline path needs no actor: a workflow whose `timeout_ms` ends while a keyed resend or lookup waits in RETRY_WAIT, or a worker that crashed after `tool_begin` (ledger `SENDING`) whose lease expires after the deadline. The cancel path needs only a non-viewer member, while only an owner/admin may reconcile.

Repro (NOT RUN: needs the integration stack; fixture of `tests/integration/m10r_review_fixes_test.go`): sandbox with ceiling TEST_ACCOUNT; NATIVE_KEY WRITE tool whose provider records the effect and answers 500 (UNKNOWN `server_error`), `retry.max_attempts >= 2` so the instance waits in RETRY_WAIT. As a plain member `POST .../instances/{id}/signals {"operation":"cancel"}` -> instance `CANCELLED` while `runtime.tool_invocations.state='UNKNOWN'`. Then owner `POST .../sandboxes/{id}/freeze` -> 409 `sandbox_conflict`, every time; owner `reconcile` -> 409; `SELECT control.tool_reconcile(...)` returns false. Deadline variant: plan `timeout_ms` just above the first send; the next claim after the deadline writes `TIMED_OUT` (execution.go:102) with the ledger `UNKNOWN`.

Fix direction: in the deadline branch and in `cancel`, read the ledger and, when an outcome is outstanding for the current tool step, park in RECONCILE (the operator then reconciles or cancels from there), or let `tool_reconcile` and the signal accept a terminal instance whose invocation is still `SENDING`/`UNKNOWN`.

### TS-02 — MEDIUM (documented-contract breach; the impact is disclosure only) — the compiler does not check the tool tenant allowlist, so a tenant can compile and pin tools restricted to other tenants

Contract: ADR100 decision 2: "The compiler resolves the ref/version in the trusted catalog, **checks tenant allowlist**, types each input mapping...". The code says the opposite: `internal/compiler/workflow_tools.go:53` ("Tenant allowlists are checked at execution, where the tenant is known") and `:75` (`catalog.Lookup(a.ToolRef, a.Version, "")`, no tenant). `compiler.Compile` has no tenant parameter and nothing in `internal/platform/access/metadata.go` filters. `Catalog.Summaries` (`summary.go:19-22`) states the intended property: "one tenant's context can never reveal another tenant's private tool".

Repro (executed, `internal/compiler`, `TestZZReviewCompileIgnoresTenantAllowlist`): both example tools restricted to `aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee`, compile of `contracts/examples/workflow-tool-draft-v1.5.json`:

```
compile with tools restricted to ANOTHER tenant: valid=true diagnostics=[]
IR excerpt readable by the compiling tenant: ..."tool_hash":"2ea9e501...adde","tool_contract":{"side_effect":"READ","approval":"NONE","idempotency_mode":"NONE","retry_mode":"SAFE_RETRY","replay_window_ms":0,"settle_ms":0,"timeout_ms":1000}...
control with notify tool absent: [unknown_tool]
```

Impact: any tenant that can compile metadata learns whether `tool.<name>@<version>` exists for another tenant (compile succeeds, versus `unknown_tool`) and receives that tool's definition hash (a commitment to host, secret reference name and the allowlisted tenant UUIDs; a guess-confirmation oracle) and its contract (side effect, approval, idempotency, retry, replay window, settle, timeout). Execution still fails closed without I/O (`tool_step.go:249`) and promotion refuses (`releases.go:568-572`, ADR103 `BLOCK.tool_unresolved`); the cost is a document and workflows pinned to a tool the tenant can never run. Fix: pass the tenant into `Compile`, or filter the catalog by `AllowsTenant` before `toolActivities`, and answer `unknown_tool` for another tenant's private tool; or amend ADR100 #2 and accept the disclosure.

### TS-03 — LOW — the tool rate budget is shared by all tenants (and multiplied by the number of workers)

`internal/tools/ratelimit.go:36-72` keys the bucket by `ref@version` (`executor.go:206`); `Request` carries no tenant. One tenant that fires the tool's `rate_per_second` (1..100) exhausts it for every other tenant allowed on that tool; their calls end FAILED `rate_limited` (retryable) and consume their retry budget. The limiter is process-local, so the provider sees `rate x workers`. Repro (executed): `TestZZReviewRateLimiterSharedAcrossTenants`: tenant A takes 5 of 5, tenant B's first call -> `FAILED/rate_limited`. ADR100 declares a per-tool rate, so this is a noisy-neighbour defect, not a breach. Fix: a per-tenant sub-bucket.

### TS-04 — LOW — invalid UTF-8 in a provider answer is accepted as SUCCESS with silent U+FFFD substitution

`internal/tools/catalog.go:529-541` (`decodeScalar` decodes with `encoding/json`, which replaces invalid bytes) after `connector.go:184-216` (`parseFlatObject` keeps the raw bytes). Repro (executed, `TestZZReviewResponseMatrix`): `{"balance":"1.00","currency":"\xff"}` -> `SUCCESS/ok`, `currency = "�"`. Two different byte strings collapse into one value, and a non-ASCII answer in another charset is stored corrupted instead of being `invalid_response` (ADR100 #6). The same decoder accepts a catalog file with invalid UTF-8 inside a SIMULATE fixture value (`TestZZReviewCatalogFoldingAndRefs`, "invalid utf8 value": accepted). Fix: `utf8.Valid(raw)` before `parseFlatObject` and in `LoadCatalog`.

### TS-05 — LOW — any 2xx, including 202, 203, 206 and 226, is SUCCESS for a WRITE

`internal/tools/executor.go:393-415`. Repro (executed): a valid typed body with status 202, 203, 206 or 226 -> `SUCCESS/ok`. ADR100 #6 says "2xx with a valid typed body is SUCCESS", so this is documented; but a 202 Accepted from a queueing provider means "not applied yet" and a 206 is partial content, and the catalog cannot say which statuses a tool means. Fix: an optional per-tool `success_statuses` (default 200 and 201), any other 2xx -> UNKNOWN `invalid_response`.

## Observations (not findings)

- `tools.Catalog.Lookup(ref, version, "")` (`catalog.go:273-280`) skips the pin check for an empty pin. The compiler uses that on purpose (`workflow_tools.go:75`); the other callers (`tool_step.go:243`, `verifier.go:656`, `releases.go:568`, `promotion/bundle2.go:293`) pass a pin that plan validation requires to be 64 hex (`plan.go:321`); no path to an empty pin was found. A footgun, not an exploit.
- `semverPattern` (`catalog.go:32`) accepts leading zeros in minor and patch, so `1.00.0` and `1.0.0` are two catalog keys. Pins are by hash; no confusion found.
- `json.Number("")` passed as an integer input is sent as `0` (`typedValue` marshals it so). The runtime never builds a `json.Number` (`DecodeInput` returns `int64`), so only a Go caller of `Execute` reaches it.
- The evidence `tool_catalog_hash` is the API process' catalog hash (`sandboxes.go:114`), not the worker's; the ledger manifest binds each `tool_hash` actually used, so the evidence still names the definitions that ran. The argument is already a stated residual (ADR101 Amendment 3).
- Ports are not restricted by the catalog (any 1..65535), but they are operator-chosen; `dialContext` compares the single catalog `host:port` and `controlContext` re-checks the port, so a tenant or a redirect cannot change it.

## Attacks that held (executed attack, observed result)

- IPv6 forms `::1`, `::ffff:127.0.0.1`, `fe80::`, `fc00::`, `64:ff9b::/96`, 6to4, ISATAP, SIIT, Teredo and IPv4-compatible of every special-purpose IPv4 block: 47 x 9 property check, 0 violations; all 2^24 /24 prefixes against an oracle, 0 leaks.
- Decimal, octal, hex and short IPv4, `user@host`, `#`, `?`, ports in the host: 64 spellings, none reaches the dialer as an IP literal; the 10 DNS-shaped acceptances are denied at dial time by design.
- Same-origin redirect with a bearer token: no second request in 5 status codes x 2 tools; the mutation turns the probe red.
- CRLF, NUL, dot segments and encoded slashes in tool arguments: raw bytes show percent-encoding or refusal in all 45 cases; the body is always the 4 declared members.
- Gzip, `Content-Encoding`, duplicate or odd `Content-Type`, duplicate keys (also escaped), BOM, nesting, one byte over the cap, chunked without end, slow drip, endless 1xx, 101 upgrade, header flood: all UNKNOWN, FAILED or timeout inside the deadline; nothing is ever decompressed.
- Production credential in a sandbox mode, test-account credential below TEST_ACCOUNT: 24-row matrix, mutation-checked (gateway F4 / M11 R07 hold).
- Secret file swapped for a symlink between `Lstat` and open: 492 669 resolutions, 0 foreign values (`secrets.go:40-58`: `Lstat`, no-follow open, `os.SameFile`, mode re-check).
- Unicode-folded and duplicate catalog keys (gateway F1): 16 of 17 variants rejected; `asciiKey` (catalog.go:576-587) removes the whole class.
- Goroutine and connection residue after 120 hostile calls: none.

## NOT RUN (reason)

- Every PostgreSQL-backed claim (no database stack in this round): approval gate at `control.tool_begin` (TL001/TL002), lease fencing of `tool_finish`, exactly-once of the ledger under concurrent leases, ledger and sandbox tenant isolation, `freeze_sandbox` quiescence and READ COMMITTED serialisation, fence triggers on a frozen sandbox, evidence body hash binding and M12 verification of it, `control.worker_environment()` derivation. Static reading found no defect beyond TS-01.
- HTTP routes (`sandbox.read`, `sandbox.manage`, worker decision for `tools.invoke`) against a live API; custom-role holders of `sandbox.manage` (only `policy.go:91-94` and `definition.go:164-170` were read: owner/admin only).
- Real DNS behaviour (search domains, `/etc/hosts`, `*.localhost` resolution) and any non-loopback destination: the dial decision for a public address was only exercised with the fake resolver.
- `-race`: only the gateway concurrency tests listed under Commands.
- TLS certificate-name mismatch, expired chain and SNI: existing tests only (package PASS), no new probe.

## Commands

| Command | Result |
|---|---|
| `gt test -count=1 -skip TestZZReview ./internal/tools` | PASS 4.8 s |
| `gt test -count=1 -v -run 'TestZZReview(HostForms\|EmbeddedIPv4Forms\|IPv4SweepAgainstOracle)' ./internal/tools` | PASS 12.8 s (0 violations) |
| `gt test -count=1 -v -run 'TestZZReview(WireInjection\|BodyInjection\|InputTypeConfusion\|RedirectsAndProxy)' ./internal/tools` | PASS (after correcting two assertions of mine) |
| `gt test -count=1 -v -run 'TestZZReview(ResponseMatrix\|RateLimiterSharedAcrossTenants\|SlowAndEndless\|LeakStorm\|CredentialMatrix\|FileSecretsSwapRace\|CatalogFoldingAndRefs)' ./internal/tools` | PASS as probes (the response and rate probes log TS-03/04/05) |
| `gt test -count=1 -v -run TestZZReviewCompileIgnoresTenantAllowlist ./internal/compiler` | PASS as probe (logs TS-02) |
| two temporary mutations of `executor.go` | both probes FAIL as expected; file restored, `git diff` empty |
| `hv nice -n 10 gt test -race -count=1 -run 'TestConcurrentExecutionIsRaceFree\|TestZZReviewLeakStorm\|TestZZReviewResponseMatrix\|TestRateLimiterIsExactUnderConcurrency\|TestZZReviewSlowAndEndless' ./internal/tools` | see the line below |

Race run result: PENDING (queued behind the heavy-run lock when this report was written).
