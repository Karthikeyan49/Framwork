# Edge rehearsal (independent verification of the HTTP edge of the AI route and the body budget)

Target: `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`), branch `review/edge-rehearsal`. I wrote none of the code under test and changed none of it.
Method: probes `tests/integration/zz_review_edge_*_test.go` (deleted before every commit), run with `it-run m13 ... -v -race=false`, loopback only, scripted provider, one
disposable database per test, real `httpserver.New` server (production timeouts: header 5 s, read 10 s, write 10 s, idle 60 s). Host: 4 cores shared with five other agents
(load average 3.6 to 8.7 during run 1); latencies below can be inflated by that load, the long-phase timings (seconds) are not materially affected.

## Progress

| time | state |
|---|---|
| 29 Oct 17:46 | run 1 (scenarios 1 and 6) finished: PASS, 340 s test time, 391 s with the lock wait |
| 3 Nov 06:22 | report with scenarios 1 and 6 pushed |
| 3 Nov 06:35 | run 2 finished (scenarios 2 and 4, real `cmd/api` processes, 94 s): ER-03 found |
| 3 Nov 06:45 | run 3 finished (scenario 5, h2 status codes, result shed, 177 s) |
| 3 Nov 06:55 | run 4 finished (scenario 3: ramps of 50, 200, 400 and the result burst at production sizes, 332 s): ER-04 found; run 5 (repeat of the process test) queued |

## Verdict (so far)

Scenarios 1, 4 and 6: **PASS WITH FINDINGS** (no integrity violation: the 60 s phase works end to end behind the production server; limits and the pessimistic charge are global and exact
across processes; behind a proxy the phase works only if the proxy waits longer than about 75 s; the production server speaks HTTP/1.1 only). Scenario 2: **FAIL**, a routine SIGTERM
abandons every in-flight paid phase longer than 10 s (ER-03). Scenarios 3, 5, 7: see the table.

## Scenario table

| # | what was executed | observed numbers | result |
|---|---|---|---|
| 1a | 4 concurrent creations (phases 30, 55, 58, 65 s) straight to `httpserver.New` | 201 READY at 30.19 / 55.07 / 58.12 s; the 65 s phase 503 `ai_unavailable` at 60.22 s (FAILED/TIMEOUT); `used_tokens` 23,596 = sum(in+out); audit admitted 4, created 4; slots 4 to 3 to 0 | PASS |
| 1b | keep-alive, pipelining, other routes | next request on a kept connection: own 10 s deadline (GET stalled 9 s: 200; stalled 11 s: EOF at 11.01 s), same as a fresh connection; pipelined 201 then 200, 200 in order; only `aiPropose` sets `nextWithin` (`ai.go:428`) | PASS |
| 1c | behind a Go reverse proxy, upstream timeout 30/60/120 s, client write timeout 60/120 s, plus cut-in-result, retry and idempotency probes | see section "Scenario 1 behind a proxy" | PASS WITH FINDINGS (ER-01, ER-02) |
| 6 | protocol probes against the production server; h2 TLS proxy in front | HTTP/1.1 only (h2c prior knowledge, h2 preface, upgrade header, TLS ALPN all fail or are ignored); behind an h2 proxy: long stream 12.08 s, 30 parallel reads p50 65 ms p99 98 ms | N/A for the server; PASS behind a proxy |
| 2 | real `cmd/api` process, SIGTERM during five 30 s phases (and during one 5 s phase), then a fresh process over the same database | in-grace phase served (201 at 5.17 s, exit 0); the five 30 s phases: all cut at SIGTERM+10.00 s, exit 1, **5 of 5 left PENDING**, 307,335 tokens charged pessimistically at the sweep (exactly once) | **FAIL (ER-03)** |
| 3 | 50, 200 and 400 concurrent provider phases (40 s, 40 s, 57 s) over 7, 25 and 50 tenants (8 each), arrivals 25/s from 16 addresses; other tenants probed throughout; plus a read burst at production queue sizes while 8 answers arrive | 400 phases in flight at once, all 400 READY, 0 shed, 0 x 429; per phase about 6 goroutines and 2 descriptors (both ends are in this process) and 0.13 MiB RSS; no database connection held; other-tenant read p99 141 ms against 45 ms idle; **but 8 of 8 answers lost** when a read burst filled the database admission (ER-04) | PASS for the storm; **FAIL (ER-04)** for the result under load |
| 4 | two (three) real `cmd/api` processes over one database: in-flight limit, daily budget, SIGKILL mid-phase, sweeps inside and after the lease, two sweeps at once | limit 2: 2 of 6 admitted over two processes; budget for 2.5 reservations: 2 of 6 admitted (one on each process); sweep inside the lease touched nothing; after the lease 2 rows EXPIRED and charged 61,467 once each, also with two sweepers at once | PASS |
| 5 | 16 verified identities x 2 held connections from 16 addresses (and from one); owners' POSTs meanwhile; recovery; forwarding headers; 8,392 distinct addresses | 32.00 MiB booked in 20 ms; owner 503 `service_busy` / holder 429 `too_many_uploads`, both with Retry-After 1; sustained 24.3 s: 24 of 24 owner POSTs refused; recovery 10.08 s; headers ignored (20 of 25 either way); peer table full at 8,192 | **residual confirmed and sharpened (PASS of the documented numbers; no new defect)** |
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
355 x 200 and **345 x 503**, 0 x 429. Repeated in run 3 with the code recorded (host load 10): 700 streams in 1.4 s gave 195 x 200 and **505 x 503 `service_unavailable`, no Retry-After**, 0 x 429: the shed is the
database admission (4 active + 128 queued), not the traffic gate (700 requests in 1.4 s is under its 600 burst + 300/s). By code (`gate.go:140`, ADR061) all clients behind one proxy share one peer key.

## Scenario 2, shutdown during a phase (run 2, `TestZZEdgeProcesses`, real process)

`cmd/api` built from this tree (`gt build -o .local/edge/api ./cmd/api`), started with `APP_ENV=test`, a scripted provider file (`wall_clock_ms` 60000, `in_flight_per_tenant` 8), no operator
credential in its environment (the process refuses to start with one, `operator_env.go`), the disposable database and the stack's Redis. The signal path is `cmd/api/main.go:12` ->
`app.go:312-317`: `server.Shutdown(ctx)` with `ShutdownTimeout` = **10 s, fixed in `config.go:57`, no environment variable**; on expiry `server.Close()`, `Run` returns, `Main` exits 1.

| step | observed |
|---|---|
| control: one 5 s phase, SIGTERM about 1 s after its admission | client 201 READY at 5.17 s; process exit code **0** at SIGTERM+5.33 s, log "API stopped" |
| five 30 s phases admitted, SIGTERM | listener closed at once (a request 1 s later: connection refused); the five clients end with EOF at SIGTERM+10.0 s; process exit code **1** at SIGTERM+10.00 s; its log says only "service failed; inspect configuration and dependency status" |
| database right after the exit | **5 PENDING rows, 0 FAILED**; audit admitted 6, created 1 (the control); `used_tokens` 5,972 (the control only); reservations of 5 x 61,467 = 307,335 tokens still held |
| in-flight slots | held until the lease (80 s after admission): an instance configured for 8 per tenant admitted a new request at once (201 READY); with the default of 2 the tenant would get 429 `ai_busy` for 80 s |
| first creation after the lease (81.5 s), fresh process | the 5 rows become EXPIRED/EXPIRED, each charged its whole reservation: `used_tokens` 325,253 = 5,972 + 5 x 61,467 + 2 x 5,973; one more creation: 331,226 = the sum over the rows, no second charge |

So a phase that ends inside the 10 s grace is served and the process exits 0; every phase that needs longer is cut, its result exchange never runs (0 of 5 reached FAILED, so
the answer is lost and the proposal cannot even say so), and the pessimistic charge is about ten times the actual use (61,467 against 5,973 here).

## Scenario 4, several API processes over one database (run 2)

Three to five real processes (A, B, C, ...) over one database; the limits are decided in `ai_begin` under one tenant advisory lock (`00089:142`).

| check | executed | observed | result |
|---|---|---|---|
| 4a in-flight limit (2) is global | 3 creations on A and 3 on B at once, both configured for 2 per tenant | 2 admitted (201 READY at 20.4 s), 4 x 429 `ai_busy` at 0.40 to 0.45 s, 2 proposal rows | PASS |
| 4b daily budget is global | both configured for a budget of 2.5 reservations (153,668); 3 + 3 creations | 2 admitted (one on each process), 4 x 429 `ai_budget_exhausted`; usage requests 2, used 11,946 = sum over rows | PASS |
| 4c kill and sweep | A holds 2 + 2 PENDING runs (two tenants), SIGKILL after 5 s (clients: EOF at 5.31 s) | rows stay PENDING; at +30 s an instance configured for 2 gets 429 `ai_busy` (the dead process still holds its slots), one configured for 8 gets 201; **the sweep inside the lease touched nothing** (both A rows PENDING, `used_tokens` = only B's own 5,973) | PASS |
| 4c after the lease (81.5 s) | B alone creates in tenant K; B and C create in tenant L at the same moment | K: both A rows EXPIRED, usage 140,853 = 2 x 61,467 + 3 x 5,973 (exactly once); L (two sweepers at once): both creations 201, both rows EXPIRED, usage 134,880 = 2 x 61,467 + 2 x 5,973 | PASS |

The in-flight and budget numbers come from the configuration of the process that handles the request (`p_inflight`, `p_daily_tokens` are arguments of the port), so replicas with different
`in_flight_per_tenant` or `daily_token_budget` apply different limits to the same tenant (seen in 4c: the same dead rows refuse one instance and not the other).

## Scenario 5, the body budget with several peers (run 3, `TestZZEdge5BodyBudget`)

Default budget (32 MiB in all, 2 MiB per verified identity, `bodybudget.go:32-33`), the production server, 20 signed but unprovisioned identities, source addresses 127.0.0.2 to 127.0.0.150 and
127.1.x.y (bound with `net.Dialer.LocalAddr`; Linux answers all of 127/8). A holder is one POST to the AI creation route that declares 1 MiB and sends 1 MiB minus one byte, then waits; an identity
holds two of them (its 2 MiB share).

| step | observed |
|---|---|
| E1 fill | 32 connections from 16 addresses, 16 identities: **33,554,400 bytes (32.00 MiB of 32) booked by 16 identities 20 ms after the last write** (32.0 MiB sent) |
| E2 others meanwhile | the tenant owner's creation POST (45 bytes) from a new address: **503 `service_busy`, Retry-After 1, in 3 ms**; a holder's third connection (100 bytes): **429 `too_many_uploads`**, Retry-After 1, also from a never-seen address (the share is per identity, not per address); GET `/v1/session` without token: 401 in 8 ms; the owner's GET (no body): 200 in 10 ms; a 2-byte body on a non-AI route was admitted (400 `invalid_metadata` from the handler) because each holder stops one byte short and leaves 32 bytes free |
| E3 recovery | holders only wait: budget empty again **9.94 s later (10.08 s after they began)**, the 10 s read timeout; of the 32 held connections 10 were answered 400 and 22 closed without an answer; the owner's creation then 201 in 148 ms |
| E4 one address | the same 32 connections from ONE address: 33,554,400 bytes booked after 20 ms, the owner's POST 503 `service_busy`: the per-address gate is not involved (32 requests against 300/s, burst 600) |
| E5 sustained | three windows of 8 s, holders re-send 32 MiB per window (4.0 MiB/s average with the pauses, 33 Mbit/s): **the owner's POSTs 24 of 24 refused (429/503), 0 served** over 24.3 s |
| E6 headers | 25 invalid-bearer requests from one peer (its budget is 20, then 1/s): no header 20 x 401 / 5 x 429; `X-Forwarded-For` rotating 20 / 5; `Forwarded` rotating 21 / 4 (one refill); `X-Real-IP` rotating 20 / 5. Attacker 127.0.0.14 claiming `X-Forwarded-For: 127.0.0.15` in 25 requests, then the claimed victim from its own address: 1 x 401 admitted, 0 x 429 |
| E7 peer table | 8,392 requests from 8,392 distinct addresses (a valid token each, paced, 36 s): 8,170 admitted, 222 x 429; afterwards a legitimate new address: **429 `rate_limited`**, Retry-After 1; an address already in the table: 200 |

The residual of `LINE_INTEGRATION_PLAN.md` ("16 identities that really send 2 MiB each, again every read timeout, keep the body-carrying requests of every tenant at 503, and the traffic gate does
not stop them") is **confirmed by execution** and can be stated in numbers: the price is 16 accounts and 32 MiB per 10 s, that is **3.2 MiB/s (26.8 Mbit/s) of uplink**, from one address or from 16;
the effect is a 503 for every POST with a body of 33 bytes or more of every tenant, the AI creations included, while GETs and the result exchange of runs already in flight (they read no body)
are unaffected; recovery takes 10.1 s after the last byte; the traffic gate never engages (32 requests). Headers `X-Forwarded-For`, `Forwarded` and `X-Real-IP` are not trusted (E6; no use of them in
non-test code under `internal/` or `cmd/`; the peer key is `r.RemoteAddr`, `access.go:243`, `gate.go:140`): a peer can neither dodge its own budget nor frame another. E7 is the documented fail-closed
behaviour of the 8,192-key table (ADR061): 8,192 source addresses (an IPv6 /64 holds far more) keep every new address at 429 for as long as they are re-touched within two minutes.

## Scenario 3, many long provider phases at once (run 4, `TestZZEdge3Storm`, `TestZZEdge3ResultBurst`, `TestZZEdge3ResultShed`)

The server and the test client share one process, so every phase costs two sockets and both sets of goroutines. Arrivals: 10 requests every 0.4 s from 16 loopback addresses (about 25 per second),
tenants of 8 in-flight each (limit 8), daily budget 100 million. The observer is another tenant's owner from its own address: 3 workers, one read every 60 ms each, kinds `healthz` (no database),
`session` and `access` (database reads). **The host was at load average 11 to 14 (other agents) throughout**, so every latency is inflated; the baseline below is measured under the same load.

| stage | ramp | phases in flight (provider peak) | goroutines (idle 19) | descriptors (idle 20) | RSS (idle 50 to 65) | heap (idle 10 to 13) | app pool in use | responses | landed within |
|---|---|---|---|---|---|---|---|---|---|
| 50 x 40 s | 2.0 s | 50 | 319 | 120 | 57 MiB | 14 MiB | 0 | 50 x 201 READY | 1.6 s |
| 200 x 40 s | 8.0 s | 200 | 1,219 | 420 | 85 MiB | 25 MiB | 0 to 1 | 200 x 201 READY | 7.7 s |
| 400 x 57 s | 16.0 s | 400 | 2,423 | 820 | 116 MiB | 37 MiB | 0 to 2 | 400 x 201 READY | 15.8 s |

After each stage goroutines returned to 19 and descriptors to 20 (RSS 82 MiB after the last stage, heap 18 MiB, not yet returned to the OS). Per phase in flight, both ends together: **6 goroutines,
2 descriptors (one is the server's), 0.13 to 0.20 MiB RSS, 0.07 MiB heap**. No database connection is held in a phase (the pool counter is 0, the 1 to 2 seen during the ramp are admissions).
The traffic gate counts a request once, when it arrives (`Peer`, `Subject`); a phase holds nothing in it. Nothing was shed or limited: 650 creations, 0 x 429, 0 x 503.

| latency of the other tenant (ms) | access p50 / p99 / max | session p50 / p99 / max | healthz p99 |
|---|---|---|---|
| idle (n = 86 each) | 11 / 45 / 56 | 9 / 33 / 56 | 3 |
| during 50 phases (n = 435) | 14 / 254 / 457 | 13 / 276 / 402 | 2 |
| during 200 phases (n = 450) | 15 / 167 / 552 | 13 / 233 / 289 | 4 |
| during 400 phases (n = 701) | 13 / 141 / 186 | 12 / 148 / 180 | 4 |
| while the 400 results landed (n = 260) | 13 / 122 / 138 | 12 / 106 / 140 | 8 (max 32) |

0 failures in about 2,100 probe requests. **The first thing that breaks was not found by concurrency up to 400**: not memory, descriptors (the process limit here is 20,000, which at 2 per phase is about
10,000 phases in this combined process; linear extrapolation, not measured), goroutines, database connections or the gate. What does break is the result exchange when the database admission is full (ER-04).

**ER-04 evidence.** (1) `TestZZEdge3ResultShed`, admission limit 1 and queue 2 (4 and 128 in production) so that it is deterministic: control with a free queue 201 READY at 3.07 s; with the slot held and
the two queue places taken when the answer arrives (3 s): **503 `service_unavailable` at 3.02 s, the proposal stays PENDING**, audit admitted 2 / created 1, `used_tokens` 5,974 (the control only).
(2) `TestZZEdge3ResultBurst`, production sizes: 20 creations of one tenant (12 refused 429 `ai_busy`, the tenant limit being 8 in this probe), whose 8 answers fall due 6.0 to 6.4 s after their start;
at 5 s 400 looping clients (16 identities, 16 addresses) began reading `/v1/session` for 4.2 s: 17,977 requests offered, 226 answered, **5,195 x 503 shed by the admission queue, 12,556 x 429 shed by the
gate**. Result: **8 provider answers, 8 x 503 `service_unavailable`, 8 PENDING rows, 0 READY, `used_tokens` 0, audit admitted 8 / created 0.**

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

**ER-03 (MEDIUM, availability and cost): SIGTERM abandons every in-flight AI phase that needs more than 10 s, and the process exits 1.** `config.go:57` fixes `ShutdownTimeout` at 10 s;
a phase lasts up to 60 s plus a 10 s margin (ADR104 A5). Evidence: scenario 2 table (5 of 5 phases lost, all PENDING, 307,335 tokens charged at the sweep, tenant slots held 80 s). ADR104 A12
describes the slot and the charge for a "crashed request" only; nothing says that an ordinary stop or restart does the same. Repro: `TestZZEdgeProcesses` (S2 lines). Consequence for a
routine deployment: every restart costs each running request its answer, 61,467 tokens of daily budget (about 31% of the default 200,000 per request, so two requests in flight at the default
limit burn 61%) and 80 s of that tenant's capacity. What to change (not done here): a shutdown grace of at least 75 s when the AI builder is configured, or stop admitting creations on
SIGTERM and wait for the running ones, and let a cancelled run store its FAILED result before the process ends; and log how many requests were abandoned.
Also write down: the orchestrator's termination grace must exceed the application's (the application ends itself at 10 s today).

**ER-04 (MEDIUM, availability and cost): the result exchange of a paid answer takes a slot of the same 4 + 128 database admission as every other request and is not retried when the queue is full, so an
overload loses the answer.** `aiResultExchange` repeats only known aborts (`ai.go:564-579`, `postgres.KnownAbort`, `retry.go:19-25`); `Admission.Acquire` returns `ErrAdmissionFull` at once when 128 are
waiting (`admission.go:49-52`), which becomes the bare 503 `service_unavailable` (the "retry budget" residual of ADR104 Amendment C names a different case). Evidence above: 8 of 8 answers lost under a read burst;
the user sees a 503 without a proposal id, the row stays PENDING for the lease (80 s, slot held), and the sweep charges the whole 61,470 reservation (about ten times the scripted use). Not claimed: that a
normal load reaches 132 concurrent transactions (the idle API served 400 phases and 2,100 probes without a single shed); the burst needs more than 132 database requests at one moment. What to change (not
done here): retry `ErrAdmissionFull` for the result exchange inside its 8 s context (it is not latency sensitive), or give result exchanges their own reserved slots.

(Further findings are added below as the scenarios finish.)

## Deployment constraints that follow

1. **Proxy or load balancer timeouts.** Response and idle timeout of at least 75 s (recommended 90 s) on the two AI creation routes, and a client-facing write timeout of the same size. At 60 s the client sees
   EOF instead of the API's own 503 (60.2 s); below 60 s a cut costs 13,866 tokens (22.6% of the reservation) and the answer, or leaves a READY proposal the client never saw.
2. **No automatic retry of `POST .../patch-proposals` and `.../requirement-proposals`** at any layer (proxy `retry` on 5xx or reset, client libraries, service-mesh defaults): each retry is a new paid run
   (3 retries = 41,598 tokens, no result); the creations are not idempotent.
3. **Restarts (until ER-03 is addressed).** The process ends itself 10 s after SIGTERM whatever the orchestrator allows. Drain a replica of AI traffic and wait for the phases (up to 70 s) before stopping it,
   or accept that every running proposal is lost, left PENDING for the 80 s lease, holding its slot, and charged its whole reservation (61,467 tokens, about 31% of the default daily budget) at the next admission.
4. **Capacity.** A phase costs the server about 1 descriptor, 3 goroutines and under 0.1 MiB; 400 at once did not move another tenant's reads beyond p99 150 ms on a loaded host and held no database connection.
   What bites first are per-tenant numbers and the queue: 61,470 tokens are reserved per run, so with the default daily budget of 200,000 a tenant can have 3 runs in flight whatever its in-flight limit
   (arithmetic from the measured reservation), and the 4 + 128 database admission must not fill while results arrive (ER-04).
5. **Replicas.** Identical `in_flight_per_tenant` and `daily_token_budget` on all of them (the limits are arguments of the database port, not database state; measured in 4c). The in-flight limit, the daily budget
   and the pessimistic sweep are global and exact across processes; the traffic gate, the body budget and the peer table are per process (replicas multiply them).
6. **Body-carrying requests are the exposed surface.** Put per-source connection and bandwidth limits (and per-account limits) at the edge: the API's own protection is the 32 MiB budget, which 16 accounts
   can hold with 3.2 MiB/s, and the 8,192-key peer table, which 8,192 source addresses (one IPv6 /64) can fill.
7. **TLS and HTTP/2** are terminated in front of the API; the API speaks neither. Behind one proxy all users share one peer bucket (300/s, burst 600) and one 4 + 128 admission queue; the headers
   `X-Forwarded-For`, `Forwarded` and `X-Real-IP` are deliberately not used.

## NOT RUN

- **nginx, haproxy, caddy:** no image or binary on the host (`docker images`: postgres, redis, the OpenTelemetry collector); `docker pull` is forbidden. Only the Go reverse proxy stood in.
- **Scenario 7, UTC day rollover:** `ai_begin` takes the day from `clock_timestamp()` inside the database (`00089:142`); the clock cannot be injected, the usage table accepts writes only through the
  port (`ai_port_write` trigger, `00089:94`) so a "yesterday" row cannot be seeded, and the UTC day boundary was not reachable within the session. Reading, not execution: the proposal pins `usage_day` at admission, finish and
  sweep charge that stored day, and the in-flight and reservation sums are not day-scoped.
- **Real provider, real model, real IdP, TLS on the API** (it has none), and a run of the whole matrix on a quiet host: the host load average was 3.6 to 14 while the probes ran.
- **The race detector:** the probes ran with `-race=false` (the integration suite normally runs with it); goroutine, descriptor and RSS numbers are those of an ordinary build.
- **pg_stat_activity as proof of idleness:** the migrator cannot see the `state` of the application role's sessions (NULL); the application pool's `InUse` counter was used instead (0 during every phase).
