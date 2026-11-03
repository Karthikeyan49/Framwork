# Route inventory, authorization matrix and contract drift (final verification round)

Auditor branch `audit/route-contract-matrix`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9`.
Report-only: no product code, test or ADR was changed. Probe files (`zz_review_*`) were never committed.

## Progress

| time (UTC) | state |
|---|---|
| 06:35 | skeleton; static reading of `access.go`, helpers, `httpserver`, `signin`, `policy` done; inventory probe (91 access patterns) and HTTP-semantics probe executed DB-free |
| next | policy matrix test (DB-free), DB probe on m13 (401/403/404 ordering, action and audit emission per route), drift list |

## Executed facts so far (details in the sections that follow once complete)

* 91 patterns are registered by `access.API.Register` with preview, workflows and registry enabled (85 without workflows, 18 without preview,
  14 without registry); signin adds 3, httpserver 3 (`GET /healthz`, `GET /readyz`, catch-all `/`). Reflective walk of the `ServeMux` routing tree
  (probe `TestZZInventory`, `GO 1.27.2`), each pattern routed back to itself with `mux.Handler`: 0 mismatches.
* `AcceptsPOST` agrees with the registered POST set: 0 POST routes outside the allowlist, 0 allowlisted sample paths without a route.
* Wrong method on a routed API path: `DELETE` over all 91 registered paths answered 404 `not_found` 91 times, 405 zero times (probe
  `TestZZSemantics`, last line). Only `/healthz` and `/readyz` answer 405 with `Allow: GET, HEAD`.
