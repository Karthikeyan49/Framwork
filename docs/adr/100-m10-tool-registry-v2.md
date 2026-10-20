# ADR100: M10 Tool Registry V2 — governed tool execution

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
2. **Opt-in authoring 1.3.0 (1.5.0 since the integration, see Amendment I1).** Identical to 1.2.0 except an activity may be
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
integration test yet. Migration 80 is additive and forward-only. Recover by removing a tool
from the catalog (pinned plans fail closed without I/O) or revoking
`tools.invoke`; never delete ledger/attempt evidence.

## Amendments

Date: 2026-10-16/17. Trigger: the independent M10 review
([M10_INDEPENDENT_REVIEW.md](../verification/M10_INDEPENDENT_REVIEW.md))
findings M10R-01..05 and candidate C6. Each amendment is enforced by migration
00085 (additive, forward-only; 80-83 unchanged) and has a regression test in
`tests/integration/m10r_review_fixes_test.go` that failed on the reviewed code.

- **A1 (M10R-02, amends #2 and #7): the database owns the approval gate.** Plans
  with tools compile to DSL **2.2.0**: each tool state pins `tool_contract`
  (side effect, approval, idempotency and retry mode, replay window, lookup
  settle interval, timeout) derived from the definition whose `tool_hash` is
  pinned beside it; the runtime fails closed if the catalog definition and the
  pin disagree. DSL 2.1.0 is restore-only: stored plans restore byte-for-byte
  and still match their source, and the database treats a 2.1.0 tool state as
  approval-required whenever it is structurally gated by an approval state.
  `control.tool_begin` ignores its `needs_approval` argument (kept for the
  signature) and derives the requirement from the pinned plan, and derives the
  step request witness from the binding (a forged `request_hash` is refused).
  The worker's column UPDATE grant on its leased instance is bound by a trigger
  to the pinned plan: a worker-made state change must follow an `always`
  transition of a start/activity/timer state, and leaving an activity needs its
  receipt; approval transitions remain API-only. A worker tool receipt must equal
  the settled ledger (request hash, response hash, pinned activity). Residual: a
  compromised worker inside an approved lease can still lie about a provider
  response (unchanged ADR024 residual).
- **A2 (M10R-01, amends #6): unknown outcomes are durable.** `tool_invocations`
  gains `unknown_outstanding` (NULL for pre-00085 rows, derived conservatively
  from retained attempts and materialized by the next port). The invocation
  state describes the write: a failed, rejected or rate-limited lookup, or any
  non-success attempt while an outcome is unknown, leaves it UNKNOWN. While it
  holds, SEND is admitted only for (a) a fresh send after lookup-proved absence
  or operator NOT_APPLIED, (b) an in-window keyed resend (NATIVE_KEY or
  DEDUPE_WINDOW with SAFE_RETRY) whose whole send deadline ends inside the replay
  window measured from the first send (C6 margin), or (c) a NONE/READ safe retry;
  otherwise `TL003` and RECONCILE. A failed lookup is retried as a lookup. Stored
  2.1.0 plans never resend after an unknown outcome.
- **A3 (M10R-03/M10R-04, amends #5 and #6): every outcome follows the retry
  policy and unknowns never die.** Each fresh non-success outcome passes through
  the failure path (attempt count, configured backoff, `max_attempts`); the
  20-attempt ledger budget is a settled failure (`TL004`). The ledger is read
  before the catalog fail-closed check, and any terminal failure of a tool step
  while an unknown outcome is outstanding (permanent, budget, abandon at the
  last attempt, catalog change, stale approval) parks in RECONCILE, never DEAD.
  Consequence: `max_attempts` also bounds lookups; `max_attempts: 1` parks an
  unknown write in RECONCILE without a lookup.
- **A4 (M10R-05, amends #6): proof of absence needs a settle interval.** A
  LOOKUP_BY_REFERENCE tool may declare `connector.lookup.settle_ms`
  (0..600000; omitted when zero, so existing hashes and pins are unchanged;
  part of the hash when set). A lookup 404 proves absence only if the pinned
  contract declares it and the lookup started at or after the latest send's
  start + timeout + settle; the runtime schedules the lookup after that point.
  Without a declaration a lookup can find an effect but never prove absence.

Not amended (still open): C7 (`control.tool_reconcile` admits any non-viewer
member while Go policy restricts `workflows.reconcile` to owner/admin), C8
(reconcile APPLIED output is not validated against the declared typed output),
C9 (wording of #4 for `tool_finish`), and `Definition.Hash` covering the tenant
allowlist (an allowlist edit re-pins every tenant; with A3 an in-flight unknown
send now parks in RECONCILE instead of DEAD).

### Amendment after the independent gateway review (2026-10-19)

Trigger: `docs/verification/GATEWAY_INDEPENDENT_REVIEW.md` F5-F9 (F1-F4 were fixed earlier). Not
independently reviewed. Unit tests in `internal/tools/review_regression_test.go` and
`netpolicy_test.go` failed before the fixes.

- **F5.** A 2xx is SUCCESS only if its content coding is identity (or absent) and it carries
  exactly one media type, `application/json`, whose only parameter may be `charset=utf-8`; any other
  answer is `UNKNOWN` `invalid_response` (a write may have been applied). Real providers send
  `Content-Type: application/json`; a provider that does not is not accepted.
- **F6/F7.** `Definition.Hash` is canonical over the decoded fixture; null optional members are
  absent; `decodeScalar` accepts exactly one scalar. Definitions with `simulated_output` values
  spelled with escapes, padding or nulls change their hash once (they re-pin); every other
  definition keeps its hash. An unencodable hand-built definition hashes to `tools.Unhashable`.
- **F8.** 169.254.0.0/16, fd00:ec2::/32 and 168.63.129.16 are never allowed, and ISATAP interface
  identifiers are classified by their embedded IPv4. The never-allowed list stays best-effort.
- **F9 (documentation).** #6 already says a policy or mode denial is FAILED permanent (`blocked`);
  the README table agrees. A denied DNS answer that would recover later therefore fails the step
  permanently; the safe direction was kept.
- **M11 R07** (a REROUTE/READ_ONLY ceiling still sent the test-account credential) was fixed with
  gateway F4; `TestNarrowedTestAccountToolNeverResolvesItsSandboxCredential` and
  `TestOnlyTestAccountSendsTheSandboxCredential` pin it.

## Amendment I1: integration with the remediation line (2026-10-20)

The remediation line (`origin/remediation/m9r-foundation`) used the same version tokens for different contracts, so the
milestone line moved to free ones when the two were merged into `integration/m9r-m14`. Nothing else in this ADR changes.

- **Authoring schema, IR and compiler: `1.3.0` becomes `1.5.0`** (`metadata.ToolWorkflowSchemaVersion`,
  `compiler.ToolWorkflowIRVersion`, `ToolWorkflowCompilerVersion`; `contracts/jsonschema/application-v1.5.schema.json`).
  `1.3.0` (tenant approval roles, DSL 2.1) and `1.4.0` (`field.unique`) belong to the remediation line and never admit a tool
  activity. `1.5.0` is a superset of `1.4.0`: it carries both and adds the tool activity of #2, so a workflow in it authors DSL
  `2.1.0` and may name a tenant-defined approver. `metadata.WorkflowSchema(v)` is true for 1.2.0 to 1.5.0 and
  `metadata.IntegritySchema(v)` for 1.4.0 and 1.5.0.
- **Workflow DSL.** A plan with a tool still compiles to `2.2.0` (A1); it now accepts the tenant role grammar like `2.1.0`, and its
  source authors `2.1.0`. The token `2.1.0` is shared: the remediation line uses it for tenant-role plans, this ADR used it for the
  pre-A1 tool shape. A stored `2.1.0` plan that carries tool states is the restore-only legacy shape of A1; one without tool states is a
  tenant-role plan. The compiler never emits the legacy shape.
- **Not carried over.** A document or IR that the milestone line stored with `schema_version` `1.3.0` and a tool activity no longer
  validates (1.3.0 has no tool activity). No such artifact exists outside the development database of the milestone line, which is
  rebuilt, not migrated, because its migration numbers changed.
- **Numbers.** Migrations 45 to 53 and 60 are 80 to 89; this ADR was ADR036. `00080` keeps every schema version that the remediation
  migrations 00045 and 00052 admit (`1.3.0`, `1.4.0`) and adds `1.5.0`.
