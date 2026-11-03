# Robustness audit: panics, leaks, context handling, unbounded growth, concurrency

Branch `audit/panic-leak-robustness`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`).
Scope: `cmd/` and `internal/`. No database stack (every database-dependent claim is NOT RUN). Report-only: no product code,
test or document was changed; temporary probes `zz_review_*` were deleted before each commit.

## Progress

| step | state |
|---|---|
| static pass over `go` statements, recover coverage, timers, contexts, rows/tx/body closing | in progress |
| `go vet ./...` (via hv) | PASS (rc=0, no output) |
| goroutine-leak probe: HTTP server, 200 requests with aborts | PASS (see E-2) |
| goroutine-leak probe: AI gateway, 200 runs with aborts | PASS with RB-02 (see E-3) |
| `-race -count=3` on the concurrent packages | queued |
| 30 s native fuzz of six parsers | queued |

## Findings

| id | severity | title |
|---|---|---|
| RB-01 | MEDIUM | `/readyz` is an anonymous, unthrottled fan-out of twelve concurrent probes that bypasses every admission control |
| RB-02 | LOW | `maxAbandonedCalls` is not an upper bound under concurrency (41 observed with a cap of 16) |

(details below; this file is updated as the audit proceeds)
