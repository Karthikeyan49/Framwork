# Edge rehearsal (independent verification of the HTTP edge of the AI route and the body budget)

Target: `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`), branch `review/edge-rehearsal`. I wrote none of the code under test and changed none of it.
Method: probes `tests/integration/zz_review_edge_*_test.go` (deleted before every commit), run with `it-run m13 ... -v -race=false`, loopback only, scripted provider, one
disposable database per test, real `httpserver.New` server (production timeouts: header 5 s, read 10 s, write 10 s, idle 60 s). Host: 4 cores shared with five other agents
(load average 3.6 to 8.7 during run 1); latencies below can be inflated by that load, the long-phase timings (seconds) are not materially affected.

## Progress

| time | state |
|---|---|
| 29 Oct 17:46 | run 1 (scenarios 1 and 6) finished: PASS, 340 s test time, 391 s with the lock wait |
| 3 Nov 06:22 | report with scenarios 1 and 6 pushed; run 2 (scenarios 2 and 4, real `cmd/api` processes) queued; 5, 3, 7 follow |

## Verdict (so far)

Scenarios 1 and 6 executed: **PASS WITH FINDINGS** (no integrity violation; the 60 s phase works end to end behind the production server; behind a proxy it works only if the
proxy waits longer than about 75 s, otherwise the user pays for an answer that is lost; the production server speaks HTTP/1.1 only). Scenarios 2 to 5 and 7: see the table.

## Scenario table

| # | what was executed | observed numbers | result |
|---|---|---|---|
| 1a | 4 concurrent creations (phases 30, 55, 58, 65 s) straight to `httpserver.New` | 201 READY at 30.19 / 55.07 / 58.12 s; the 65 s phase 503 `ai_unavailable` at 60.22 s (FAILED/TIMEOUT); `used_tokens` 23,596 = sum(in+out); audit admitted 4, created 4; slots 4 to 3 to 0 | PASS |
| 1b | keep-alive, pipelining, other routes | next request on a kept connection: own 10 s deadline (GET stalled 9 s: 200; stalled 11 s: EOF at 11.01 s), same as a fresh connection; pipelined 201 then 200, 200 in order; only `aiPropose` sets `nextWithin` (`ai.go:428`) | PASS |
| 1c | behind a Go reverse proxy, upstream timeout 30/60/120 s, client write timeout 60/120 s, plus cut-in-result, retry and idempotency probes | see section "Scenario 1 behind a proxy" | PASS WITH FINDINGS (ER-01, ER-02) |
| 6 | protocol probes against the production server; h2 TLS proxy in front | HTTP/1.1 only (h2c prior knowledge, h2 preface, upgrade header, TLS ALPN all fail or are ignored); behind an h2 proxy: long stream 12.08 s, 30 parallel reads p50 65 ms p99 98 ms | N/A for the server; PASS behind a proxy |
| 2 | queued (run 2) | | NOT RUN yet |
| 3 | queued | | NOT RUN yet |
| 4 | queued (run 2) | | NOT RUN yet |
| 5 | queued | | NOT RUN yet |
| 7 | UTC day rollover | no clock injection; `ai_begin` reads `clock_timestamp()` (`00089:142`) and the usage table refuses writes outside the port (`ai_port_write` trigger) | NOT RUN |

## Scenario 1 directly behind the production server (run 1, `TestZZEdge1Direct`)

Four requests at once (tenant limit 8, daily budget 100 million so that nothing but the wall clock limits them). Reservation per request: 61,470 tokens.

| request | client sees | proposal | tokens charged (in + out) |
|---|---|---|---|
| phase 30 s | 201 READY at 30.19 s | READY, stored at +30.05 s | 5,674 + 300 |
| phase 55 s | 201 READY at 55.07 s | READY, +55.04 s | 5,674 + 300 |
| phase 58 s | 201 READY at 58.12 s | READY, +58.03 s | 5,674 + 300 |
| phase 65 s | 503 `ai_unavailable` with the proposal id at 60.22 s | FAILED/TIMEOUT, +60.02 s, provider cancelled after 60.000 s | 5,674 + 0 |

The cost of admission plus result exchange is the difference between the scripted delay and the client's end time: 0.07 to 0.22 s here.
Resources (this process holds both the server and the test client, so both sides of every connection are counted):

| moment | goroutines | descriptors | RSS | heap | app pool in use / open | PENDING rows |
|---|---|---|---|---|---|---|
| baseline | 7 | 11 | 59 MiB | 10 MiB | 0 / 1 | 0 |
| t+8 s (4 in flight) | 31 | 22 | 51 MiB | 11 MiB | **0** / 4 | 4 |
| t+40 s (3 in flight) | 28 | 22 | 48 MiB | 11 MiB | **0** / 4 | 3 |
| all done | 19 | 22 | 46 MiB | 11 MiB | 0 / 4 | 0 |
| client connections closed +2 s | 7 | 14 | 46 MiB | 11 MiB | 0 / 4 | 0 |

No leak: goroutines return to 7; the 3 extra descriptors are the 3 extra pooled database connections (open 1 to 4). No database connection is held during a phase (pool in-use
0 while 3 to 4 proposals are PENDING). `pg_stat_activity` hides the state of the application role from the migrator (`state` is NULL), so the pool counter is the evidence.

## Scenario 1 keep-alive, pipelining and other routes (`TestZZEdge1KeepAlive`)

A GET is made to wait on the database admission (all 4 slots held by the probe) so that its answer is written after a chosen time; a write after the connection's deadline fails.

| connection | request | wait | result |
|---|---|---|---|
| kept, after a 12 s creation (201 at 12.12 s) | GET `/ai/proposals` | 9 s | 200 at 9.02 s |
| kept, after a 12 s creation (201 at 12.09 s) | GET `/ai/proposals` | 11 s | unexpected EOF at 11.01 s |
| fresh | GET `/ai/proposals` | 9 s / 11 s | 200 at 9.02 s / EOF at 11.01 s |
| pipelined in one write | creation 6 s, GET, GET | | 201 at 6.08 s, then 200 and 200 after 0.01 s each, in order |
| same connection, idle 11 s, then GET | | | 200 in 0.01 s |

The extension (write deadline = wall clock + 10 s) does not reach the next request of a kept connection, a pipelined request or another route: `net/http` re-arms the write
deadline in `readRequest` for every request, and `nextWithin` is set only in `aiPropose` (`internal/platform/access/ai.go:428`; `grep nextWithin` finds no other writer).

## Scenario 1 behind a proxy (`TestZZEdge1Proxy`)

Go `httputil.ReverseProxy` on a loopback listener; "up" is its response-header timeout towards the API (the API sends nothing until it is done, so this is also nginx's
`proxy_read_timeout`), "write" its client-facing `WriteTimeout`. No nginx, haproxy or caddy image or binary exists on the host (`docker images`: postgres, redis, otel collector), so those are NOT RUN.

| proxy (up / write) | phase | client sees | stored proposal | charged (in + out) |
|---|---|---|---|---|
| 30 / 60 | 20 s | 201 READY at 20.05 s | READY | 5,674 + 300 |
| 30 / 60 | 40 s | **504 at 30.00 s** | FAILED/INTERNAL at +30.00 s (provider cancelled after 29.98 s) | 5,674 + **8,192** |
| 60 / 60 | 40 s, 58 s | 201 at 40.04 s, 58.12 s | READY | 5,674 + 300 each |
| 60 / 60 | 65 s | **EOF at 60.00 s** (no status; the proxy's 504 was written after its own 60 s write deadline) | FAILED/INTERNAL at +60.00 s | 5,674 + 8,192 |
| 120 / 60 | 58 s | 201 at 58.03 s | READY | 5,674 + 300 |
| 120 / 60 | 65 s | **EOF at 60.03 s**: the API answered 503 at 60.02 s; consistent with the proxy's 60 s write deadline having passed | FAILED/TIMEOUT | 5,674 + 0 |
| 120 / 120 | 58 s | 201 at 58.04 s | READY | 5,674 + 300 |

Eight requests: 8 provider calls (5 answered, 3 cancelled), `used_tokens` 63,276, audit 8 admitted and 8 created, no PENDING row left. A cut by the proxy is noticed by the API within
about 20 ms (the provider call was cancelled 29.98 s into a 30 s cut): the slot is free at once and nothing stays PENDING.

**Cut inside the result exchange** (answer at 28 s, database admission held 27.9 s to 31.5 s, proxy cut at 30 s): the client got **504 at 30.01 s**; the proposal was stored **READY at
+31.60 s**; `GET /ai/proposals/{id}` returned 200 READY and `GET /ai/proposals` 200, so the paid answer is recoverable if the client knows to look (it never received the id).
**Retries**: three identical POSTs through a 10 s proxy (phase 15 s) gave 504, 504, 504 at 10.03 / 10.00 / 10.02 s, **3 new proposals, 3 provider calls, 41,598 tokens charged**, no
result. **Idempotency**: two identical POSTs at once gave 2 proposals, 2 provider calls, both READY (no deduplication anywhere).

## Scenario 6, HTTP/2 (`TestZZEdge6HTTP2`)

`app.go:299-305` serves `httpserver.New(...)` on a plain `net.Listen("tcp", ...)`; `Protocols`, `TLSConfig` and `TLSNextProto` are nil; no `ServeTLS` exists in `internal/` or `cmd/`.
Executed: Go client with unencrypted HTTP/2 prior knowledge: `http2: failed reading the frame payload: frame too large, note that the frame header looked like an HTTP/1.1 header`;
raw `PRI * HTTP/2.0` preface: `HTTP/1.1 400 Bad Request`; `Upgrade: h2c` request: `HTTP/1.1 200 OK` (ignored); TLS handshake with ALPN h2 against the port: `first record does not look like a TLS handshake`.
So the per-stream write-deadline, stream leak, multiplexing and `nextWithin` questions are **N/A for the server**. Behind a TLS proxy that speaks h2 to clients and HTTP/1.1 to the API: a 12 s
creation returned 201 READY at 12.08 s while 30 reads on the same h2 connection took p50 65 ms, p99 98 ms (no head-of-line blocking); 700 streams in 1.0 s from the proxy's single address:
355 x 200 and **345 x 503**, 0 x 429: the 503 of this path is `service_unavailable`, a failed transaction admission (4 active + 128 queued), not the traffic gate (700 requests in 1 s is under its
600 burst + 300/s; run 1 recorded the status only, the code is recorded in a later run). By code (`gate.go:140`, ADR061) all clients behind one proxy share one peer key.

## Findings

**ER-01 (LOW, a documented residual that now has numbers and a constraint that is not written down): a proxy timeout below the API's own 60 s wall clock loses the paid answer.**
ADR104 A5 already says that a proxy "may still impose a shorter limit" and that the API then records FAILED/INTERNAL; it gives neither the cost nor the minimum timeout. Evidence: table
"behind a proxy". A cut at T leaves either a FAILED/INTERNAL row charged 13,866 tokens (22.6% of the 61,470 reserved; 6.9% of the default 200,000 daily budget) when the provider had not
answered, or a READY row the client never saw when the answer landed inside the result exchange (the exchange takes 0.07 to 0.22 s unloaded, which is the width of that window; in the probe a
full admission queue stretched it to 3.6 s). Repro: `TestZZEdge1Proxy` cases up30 (phase 40 s) and "cut in result". Write down: any proxy in front of the API needs a response/idle timeout
of at least 75 s on the AI creation routes (4 s admission retries + 60 s wall clock + 8 s result context = 72 s worst case) and a client-facing write timeout of the same size; 90 s is a safe round number.

**ER-02 (LOW), a retried or repeated creation is a second paid run.** Three retries after a proxy cut charged 41,598 tokens for no result; two identical POSTs made two proposals and two
provider calls. Creations are not idempotent (`ai_begin` stores only the request hash, it never compares it). Write down: no automatic retry of `POST .../patch-proposals` or
`.../requirement-proposals` at any layer (the same rule ADR061 already states for 429).

(Further findings are added below as the scenarios finish.)

## Deployment constraints that follow (so far)

1. Response and idle timeout of every proxy or load balancer in front of the API: at least 75 s (recommended 90 s) for the two AI creation routes; a client-facing write timeout of 60 s
   makes the client see EOF instead of the API's own 503 at 60.2 s.
2. No automatic POST retry on the creation routes (proxy `retry` on 5xx/reset, client libraries, service-mesh defaults).
3. TLS and HTTP/2, if wanted, are terminated in front of the API: the API cannot do either. Behind a proxy all users share one peer bucket (300/s, burst 600) and one 4+128 admission queue.

## NOT RUN

- nginx, haproxy, caddy: no image or binary present, `docker pull` forbidden.
- Scenario 7 (rollover): see the table. Code reading only, not executed: the proposal pins `usage_day` at admission (`00089:142`), finish and sweep charge that day, the reservation
  check sums all PENDING rows regardless of day.
