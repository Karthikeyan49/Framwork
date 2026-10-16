# M10 Tool Registry V2 and M11 Sandbox V2 — implementation report

Date: 15 October 2026 (UTC). Source: `claude/m10-m11-tools-sandbox` at `bf70752`
(based on `claude/dazzling-goldberg-69wbkn` at `e6305df`). Authorization,
decisions and recovery are in [ADR036](../adr/036-m10-tool-registry-v2.md) and
[ADR037](../adr/037-m11-sandbox-v2.md). Foundation items R3b (other types),
R3c, R3d, R5d, R5e, R6 and R7 remain open; neither milestone is production
ready and no production activation path exists.

## What changed

**M10 — governed tools.** A trusted operator catalog (`tool-catalog-v1`,
`TOOL_CATALOG_FILE`) is the only source of tool definitions; metadata schema
1.3.0 adds opt-in tool activities that the compiler resolves, type-checks and
pins by definition hash into workflow plans (DSL 2.1.0). Migrations 45 and 47
add the `runtime.tool_invocations`/`tool_attempts` ledger and SECURITY DEFINER
ports bound to the leased job and the live SQL policy decision. A tool step is
three short transactions (authorize and record `SENDING`; send outside any
transaction; record the lease-fenced outcome). Outcomes follow the declared
idempotency capability; an unreconcilable WRITE parks for an owner/admin
reconcile. A governed HTTPS connector blocks private ranges at dial time,
refuses redirects and proxies, bounds time/size and injects operator-provisioned
credentials only at send time. New action `tools.invoke` (native policy v7).

**M11 — sandboxes.** Migration 46 admits `datasets.environment='sandbox'`
only for empty synthetic datasets registered in `runtime.sandboxes`; the tool
ceiling (BLOCK…TEST_ACCOUNT) is enforced in the gateway (`tools.EffectiveMode`)
and again in `control.tool_begin` from the leased job's environment. Freezing
(`sandbox.manage`, owner/admin only) records one immutable
`runtime.sandbox_evidence` body bound to the stored IR hash, schema, record
manifest, tool-ledger manifest, server tool-catalog hash and policy version, then
makes the sandbox read-only forever. Native policy v8 adds `sandbox.read` and
`sandbox.manage`. Bundle 1.0.0 and `sandbox.Context` are unchanged.

Frontend: IR 1.3.0 renderer support was added so tool-bearing programs load.
No new Go or npm dependency.

## Commands executed and results

| Command | Result |
|---|---|
| `make check test build` at `bf70752` | PASS |
| `make test-e2e` at `bf70752` | PASS, 5 tests |
| Targeted M10/M11 integration tests (idempotency modes, crash after send, late response, lease fencing, approval freshness, sandbox isolation, ceiling forgery) | PASS |
| Full integration run before `bf70752` | FAIL: `m9r_record_access_test` pinned the policy version string; the assertion was made milestone-agnostic (`bf70752`), then PASS |
| Independent adversarial reviews | PARTIAL. M10 ledger/protocol review (docs/verification/M10_INDEPENDENT_REVIEW.md, in progress): M10R-01..05 found (2 HIGH: a compromised worker can skip the approval gate inside its own lease; a transient lookup failure turns an UNKNOWN write into a blind resend; 3 MEDIUM) — fixes are NOT merged yet. Gateway review (GATEWAY_INDEPENDENT_REVIEW.md, in progress): F1–F9 found; F1–F4 fixed on this branch, F5–F9 (LOW) open. M11 sandbox review: NOT RUN to completion (no findings recorded) |

## Known gaps and residuals

- Independent two-agent review is incomplete (see the table above): the M10 findings
  M10R-01..05 are open until the fix branch is merged, and the M11 review has not
  produced results. Do not treat these slices as reviewed.
- Tools longer than about 2 s need lease renewal (not built). Provider callbacks,
  compensation tools, financial/destructive tools and provider breadth are
  deferred (M13/M17).
- A compromised worker within its own exact lease can still misreport a provider
  response (ADR024 residual).
- Redacted snapshots and dedicated databases are not available; a frozen
  sandbox cannot be reopened or reused.
- Sandbox UI badge and operator dashboards are not built.
