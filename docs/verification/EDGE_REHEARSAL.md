# Edge rehearsal (independent verification of the HTTP edge of the AI route and the body budget)

Target: `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`), branch `review/edge-rehearsal`.
Method: probes in `tests/integration/zz_review_*_test.go` (never committed), run with `it-run m13`, loopback only, scripted provider.

## Progress

| time (UTC) | state |
|---|---|
| 17:35 | brief read; harness written (edge provider with a per-request delay, production `httpserver.New` server, reverse proxy, connection and descriptor counters); integration run 1 (scenarios 1 and 6) queued |

Nothing below the Progress table is a result yet.
