# ADR036: M10 Tool Registry V2 — governed tool execution

Status: accepted for implementation on `claude/m10-m11-tools-sandbox`.
Date: 2026-10-15. Scope: M10 (tools, gateway, governed HTTP connector).

## Authorization and gate

The user explicitly authorized starting the next two milestones (M10, M11) on
2026-10-15. This is the "new user task" required by CLAUDE.md. The foundation
gate status at that point: R1, R2a–R2c, R3a, governed numeric R3b, bounded R4
and R5a–R5c independently passed; R3b (other types), R3c, R3d, R5d, R5e, R6 and
R7 remain open. M10 relies on none of the open items. R7 items that must ship
with a tool (idempotency, timeout/unknown, reconciliation, crash-after-send,
duplicate and delayed responses, lease fencing, approval freshness) are part of
M10 scope below. No production activation: workflows remain dev/test preview
(`WORKFLOWS_ENABLED` is rejected in production) until M13.

## Context

V3 16/40/41 require per-tool delivery semantics, a governed HTTP connector with
SSRF blocking, and an intent ledger with reconciliation. M9 activities run
inside one 3 s leased transaction and only `local.noop` exists; no network I/O
may run inside a runtime transaction (ADR023, ADR028).

## Decision

1. **Trusted operator catalog.** Tools are registered only by the platform
   operator in a strict JSON document (`tool-catalog-v1`, `TOOL_CATALOG_FILE`),
   loaded by the API (compiler) and worker (gateway). Tenant metadata, requests
   and AI output never define hosts, methods, secrets or delivery semantics.
   `internal/tools.Catalog` validates: `tool.*` ref + semver; side effect
   `NONE|READ|WRITE` only (financial/destructive need M13 authority); WRITE
   requires `approval: REQUIRED` and a non-GET method; delivery passes the M6R
   `Delivery.Validate`; NATIVE_KEY/DEDUPE_WINDOW declare a provider replay
   window; LOOKUP_BY_REFERENCE declares a lookup path with `{key}`; literal DNS
   hosts only; HTTPS; timeout ≤2000 ms; response ≤16 KiB; per-tool rate; typed
   scalar inputs/outputs; tenant allowlist; sandbox behavior. Each definition has
   a canonical hash; plans pin it and a changed definition never replaces a pin.
2. **Opt-in authoring 1.3.0.** Identical to 1.2.0 except an activity may be
   `{tool_ref, version, inputs{name: field}, retry}`. The compiler resolves the
   ref/version in the trusted catalog, checks tenant allowlist, types each input
   mapping against the entity field, requires every required input, and for
   `approval: REQUIRED` tools requires every path from the initial state to pass
   an approval state's `approved` transition. Plans containing tools use DSL
   `2.1.0` and pin `tool_hash`. Tool definitions are not part of `CatalogHash`,
   so existing IR hashes are unchanged; the pinned hash is inside the plan/IR.
   1.0–1.2 bytes, hashes and `local.noop` behavior are preserved.
3. **Capability.** New action `tools.invoke` on the workflow resource. Bootstrap
   owner/admin/member may invoke; custom roles must grant it; the `viewer` role
   name never can. The worker SQL decision requires it for any plan containing a
   tool activity (ADR033 parity), in addition to `workflows.activity`.
4. **Ledger.** `runtime.tool_invocations` (one row per instance/step: pinned ref,
   version, hash, deterministic key, request hash, state, attempts, first send,
   bounded validated response; the reference used for reconciliation is the
   deterministic key) and `runtime.tool_attempts` (each attempt row is finished
   once by `tool_finish`/`tool_abandon`, then immutable). The worker writes only
   through SECURITY DEFINER ports bound to the exact authenticated lease;
   `tool_begin` and `tool_abandon` also re-evaluate the live SQL policy decision,
   `tool_finish` checks the lease only, and `tool_reconcile` is an API-role port.
   They use PostgreSQL time and an explicit transition graph. No bodies, headers or
   secrets are stored in attempts; the ledger never stores credentials.
5. **Execution protocol.** A tool step uses three short transactions:
   (a) authorize `workflows.activity` + `tools.invoke`, verify approval
   evidence and snapshot freshness, record the attempt (`SENDING`), commit;
   (b) send outside any transaction with a deadline bounded by the tool timeout
   and the remaining lease; (c) record the outcome fenced by lease generation
   and attempt number, then run the normal step (receipt + advance, or failure).
   A crash or lost lease between (a) and (c) leaves `SENDING`, which the next
   lease treats as UNKNOWN. A late outcome from a stale lease is rejected.
6. **Outcome and reconciliation.** 2xx with a valid typed body is SUCCESS. A
   request provably never transmitted (resolution, dial, local rate limit) is
   FAILED retryable; a policy or mode denial is FAILED permanent (`blocked`); provider 4xx is FAILED permanent; 429 is FAILED retryable;
   timeouts, 5xx/408/409, broken transport after write and invalid responses are
   UNKNOWN. UNKNOWN routing follows the declared capability: NATIVE_KEY and
   DEDUPE_WINDOW resend with the same key only within the replay window and
   attempt budget; LOOKUP_BY_REFERENCE performs a lookup (found → SUCCESS from
   the lookup, provably absent → resend allowed, otherwise UNKNOWN); NONE with a
   WRITE effect parks in RECONCILE for an owner/admin `reconcile` signal, which
   also closes the ledger (APPLIED writes the receipt; NOT_APPLIED permits one
   fresh send). Evidence is never erased to retry. No universal exactly-once.
7. **Approval and freshness.** A `REQUIRED` tool sends only if the database finds
   an API-written approval signal for the instance by an actor other than the
   starter, and the bound record's current version still equals the approved
   snapshot version. Otherwise the step fails permanently without I/O.
8. **Connector and network policy.** HTTPS with certificate verification
   (system roots, optional operator CA file), no proxy, no redirects, no
   compression, header/body limits, path parameters escaped per segment, fixed
   host header, JSON bodies built from typed inputs only. Every resolved address
   is validated at dial time: loopback, RFC1918, link-local, CGNAT, IPv6
   ULA/link-local and IPv4-mapped forms are refused unless the definition is
   `internal` **and** the process enables internal networks (rejected in
   production); cloud metadata, unspecified, multicast and broadcast addresses stay
   refused even then. Credentials are resolved
   at send time from operator-provisioned secret files and injected as a bearer
   header; they never appear in logs, traces, audit, ledger or errors.
9. **Modes.** The runtime selects LIVE only for production releases (M13; not
   reachable in M10). Preview/sandbox executions use the tool's sandbox mode:
   BLOCK (no I/O, permanent failure), SIMULATE (declared fixture, flagged,
   never delivery evidence), REROUTE/READ_ONLY (sandbox endpoint) and
   TEST_ACCOUNT (sandbox endpoint and sandbox-only secret). M11 adds the
   sandbox environment ceiling.
10. **Deferred.** Asynchronous provider callbacks/webhooks, lease renewal for
    long tools, compensation tools, financial/destructive tools, provider
    breadth (M17) and production activation (M13).

## Consequences

Tool effects become explicit, pinned and auditable, with capability-specific
guarantees. Each tool step costs three short transactions and at most one send
per lease. Tools longer than ~2 s are unsupported until lease renewal exists.
A compromised worker within its own exact lease can still lie about a provider
response (ADR024 residual); high-risk effects stay out of scope.

## Verification and recovery

Unit tests cover catalog validation, SSRF/redirect/limit/secret handling and
decision rules; integration tests cover each idempotency mode under duplicate
delivery, timeout, crash after send, delayed/late response, unknown outcome and
reconciliation, approval/freshness and raw worker SQL denial. Policy revocation
mid-flight is implemented (`tools.invoke` in the SQL decision) but not covered by an
integration test yet. Migration 45 is additive and forward-only. Recover by removing a tool
from the catalog (pinned plans fail closed without I/O) or revoking
`tools.invoke`; never delete ledger/attempt evidence.
