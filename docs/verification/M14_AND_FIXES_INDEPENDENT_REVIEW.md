# Independent review of the 2026-10-20/21 integration fixes and first independent review of M14

Reviewer: independent agent; did not build, fix or merge any of the reviewed code. No product code was changed.
Pinned commit: `be6ec7cae1849bf535bad1811b21bf93067598b5` (head of `origin/integration/m9r-m14` at start).
Review branch: `review/m14-and-fixes`. Stack: isolated M14 Compose stack only (`/home/user/bin/dbrunm14`); every integration
probe below runs on a disposable database created by the suite's own `m8Database` fixture.
Rules of truth: PASS / FAIL / NOT RUN. Every "held" names the attack that was executed.
Numbers: this report uses the merged numbers (migrations `00080`-`00090`, ADR100-104, tool schema/IR/compiler `1.5.0`);
older reports are read through the map in `docs/migration/LINE_INTEGRATION_PLAN.md`.
Host: 4 cores shared with two other agents (load average recorded next to timing results).

STATUS: IN PROGRESS (interim commit).

## Progress

| Time (UTC, 2026-10-21) | Step | State |
|---|---|---|
| 19:53 | worktree verified at `be6ec7c`; Docker stacks ready; required reading done | done |
| 20:00 | baseline: stack reset + `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots`, `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema`, `TestMigrationLineageRefusesTheMilestoneLinesOldNumbering`, `TestIntegrationTenantRoleApproverGatesAToolSend` (`-race`) | PASS, 34.5 s |
| 20:03-20:10 | A1 body pre-read: raw-socket probes against `httpserver.New` on the pinned tree and on a scratch copy with the pre-read disabled; memory measurement; mutation check of the slow-body test | done, F-01, F-02 |
| next | A2-A6, then Part B (M14) | pending |

## Findings

Severity: HIGH exploitable violation of a security or integrity invariant; MEDIUM realistic integrity/availability defect or
documented-contract breach; LOW hardening, documentation or test gap.

### F-01 MEDIUM - the IR-02 pre-read lets any valid token make the API buffer 1.14 MiB per connection, ignoring the route caps

`serve` (`internal/platform/access/access.go:293-302`) reads every verified-bearer POST body up to the front door's 1 MiB cap
**before** identity resolution, membership and policy, and before the handler applies its own cap (AI create 32 KiB, apply 128 KiB,
confirm 64 KiB, reject 4 KiB, metadata/datasets 260 KiB, commands 64 KiB). A caller whose token merely verifies (a subject that is
not provisioned at all, or a `viewer`) is read in full and held for up to the 10 s read timeout.

Executed (`TestZZReviewA1Memory`, real `httpserver.New` server in the test process, one tenant-owner token, each connection sends the
headers of a 1 MiB body and 1 MiB - 1 bytes, then stalls; heap measured after `runtime.GC()`; no `-race`):

| tree | connections | heap in use before -> while held | per connection | answers |
|---|---|---|---|---|
| pinned `be6ec7c` | 300 | 10.8 -> 352.0 MiB | 1.14 MiB | none (all held), no 429 |
| pinned `be6ec7c` | 1000 | 10.6 -> 1151.7 MiB (Sys 1186.9 MiB) | 1.14 MiB | none (all held), no 429 |
| scratch copy, pre-read disabled (= pre-fix path) | 300 | 10.0 -> 10.2 MiB | 0.00 MiB | 413 `body_too_large` at once (the 32 KiB handler cap) |

The traffic gate admitted all 1000 requests of one subject from one peer (burst 600 + 300/s). Within one 10 s read timeout the default
limits admit about 3,600 requests per peer (about 4 GiB) and about 12,000 in total (global burst 2,000 + 1,000/s, about 13.7 GiB): enough to
exhaust a 16 GB host and take the API down for every tenant. `TestZZReviewA1WhoIsRead` confirms that the subject needs no membership:
with `Content-Length: 900000` and one byte sent, an unprovisioned-but-signed subject and a `viewer` wait 10.03 s on the AI and metadata
routes (read), where the pre-fix copy answers 401 / 403 in 0.02-0.03 s. With `Expect: 100-continue` the pinned tree now sends
`100 Continue` to a `viewer` and reads its body before the 403; the pre-fix copy answers 403 without asking for the body.

The plan's residual ("at most 1 MiB per in-flight verified POST ... bounded in number only by the traffic gate") understates this: the
measured cost is 1.14 MiB, the caller needs no tenant membership, and the previous bound on the AI routes was the handler cap
(32 KiB create, 4 KiB reject: an amplification of 32x to 256x), not 1 MiB. The DB-slot defect IR-02 is fixed; it was traded for a memory
exhaustion that a single token can trigger with default settings.

Repro (inside the M14 fixture, `e := m14Setup(t, m14Options{}); e.draft("500")`, real server from `httpserver.New`):

```go
head := fmt.Sprintf("POST %s/ai/patch-proposals HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 1048576\r\n\r\n", e.root, e.alice)
body := bytes.Repeat([]byte("a"), 1<<20-1) // one shared buffer on the client side
for i := 0; i < 1000; i++ { c, _ := net.Dial("tcp", addr); c.Write([]byte(head)); c.Write(body) } // keep c open
time.Sleep(1500 * time.Millisecond); runtime.GC(); runtime.ReadMemStats(&m) // HeapInuse grows by ~1.14 MiB per connection
```

Fix direction (for the builder): pre-read with the cap of the matched route (the handlers' constants), not the front-door cap, and/or a
process-wide budget of buffered body bytes (per subject and global) that answers 429/503 instead of buffering.

### F-02 LOW - "an anonymous caller is never read" is false on four routes

The new comment in `serve` and the plan state that only a verified bearer is read, "so an anonymous caller cannot make the server buffer
anything". The release idempotent POSTs (`retryRelease`: `candidates`, `acceptance-specs`, up to 1 MiB) and the two `commands` routes
(`localCommand`, up to 64 KiB) read the body before authentication (`access.go:265-286`, unchanged by `f997efe`). Executed
(`TestZZReviewA1WhoIsRead`, `Content-Length: 900000`, one byte sent): anonymous and invalid-bearer requests to `candidates`,
`acceptance-specs` and `.../commands` wait 10.03 s and the connection closes without an answer; the AI and metadata routes answer 401 in
0.02 s. Identical on the pre-fix copy, so this is not a regression of the fix, but the invariant the fix relies on does not hold. Bounded by
the invalid-bearer budget (20 per peer, 50 global burst, 10/s), so about 150 MiB at most.

## Attacks that held

### A1 body pre-read (`f997efe`, `f6398bf`)

* **Mutation check of `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots`**: in a scratch copy with `preRead := false && ...` the test FAILS
  ("honest read behind 4 slow bodies: status 200 after 8.56 s"); on the pinned tree it PASSES (proposal 201 READY in 1.62 s). The test is not
  vacuous. Load average 2.72 during the mutant run.
* **Old/new differential over sizes and framings**: 9 POST routes (AI create, AI apply, metadata, datasets, approve, candidates, sandboxes,
  unique build, query) x bodies of 0, 2, 1 MiB - 1, 1 MiB, 1 MiB + 1, 3 MiB x `Content-Length` and chunked: all 108 status/code pairs are
  identical between the pinned tree and the copy without the pre-read (`TestZZReviewA1Sizes`). The cap error is replayed faithfully.
* **Keep-alive reuse after a read error**: a malformed chunk, a chunk shorter than announced and a 2 MiB body (cap hit), each followed by a
  pipelined GET on the same connection: exactly one answer and the connection is closed (no second request parsed). Valid chunked bodies and a
  body on a bodiless route are followed by the pipelined answer (reuse works). Same on both trees except the read-error code (below).
* **Audit/code difference of a failed read** (documented by the plan): the pinned tree answers a broken chunked body on the AI route with 400
  `invalid_request` before the transaction; the pre-fix copy answered 400 `invalid_ai_request` inside it. No authorization decision is skipped
  that would let a caller learn anything (the 400 comes before any decision). Not graded.
* **Anonymous / invalid bearer on pre-read routes**: AI and metadata POSTs answer 401 in 0.02 s without reading (both trees).
* **Content-Length 0, chunked empty body, protocol-level forms**: size 0 with `Content-Length: 0` and with an empty chunked body give the
  handlers' own typed 400 on all nine routes on both trees.

## Not covered

pending
