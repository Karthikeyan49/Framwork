# ADR104: M14 AI Builder and Intent Capture — proposals only, never authority

Status: accepted for implementation on `claude/m14-ai-builder`.
Date: 2026-10-18. Scope: M14 model gateway, semantic context, application-patch and
requirement proposals, bounded compiler repair loop, human-gated apply/confirm.
M14 is a **development/test preview capability**. It delivers the machinery that
makes model output safe to consume; it does **not** claim any property of a real
language model (see §15). No production data plane exists (ADR103 §10).

## Authorization

On 2026-10-18 the user asked to spawn 7 agents to finish the interrupted work and to
"additionally complete other one milestone". The next roadmap milestone after M13 is
"M14 AI Builder + Intent Contract" (`docs/v3/roadmap/50_RESTRUCTURED_MILESTONES.md`,
`51_DEPENDENCY_ORDER.md`). The V3 sources that bind it are
`docs/v3/architecture/13_INTENT_CONTRACT.md`, `17_AI_WORKFLOW_TRUST_MODEL.md`,
`19_APPLICATION_PATCH_REBASE.md`, `docs/specs/53_AI_BUILDER.md`,
`54_MODEL_GATEWAY.md`, `docs/risks/25_AI_CORRECTNESS.md`,
`35_PROMPT_INJECTION_AGENCY.md`, `37_AI_LOCKIN_COST_LATENCY.md`.
Foundation items R3b–R3d, R5d, R5e, R6 and R7 remain open (ADR100).

## Context — what exists (verified in the tree, not assumed)

- `internal/ai` is a reserved, empty module boundary (README only).
- `metadata.ApplicationPatch.Rebase` (ops `add|update|delete` over whole typed
  definitions, `expected_hash` CAS, server-derived change class) auto-applies
  presentation only; every non-page change is a `SENSITIVE_CHANGE` conflict.
- `compiler.Compile` returns machine-readable `Diagnostic{Path, Code, Message}`;
  `compiler.ComputeDiff` classifies a change by aspect (rule logic/financial, workflow
  graph, approval gate, tool pin, …); M12 reduces diffs to risk facts and levels.
- `intent` already defines `RequirementV2` with `Provenance` values
  `AUTHENTICATED_HUMAN` and `AI_PROPOSED_HUMAN_CONFIRMED`, requirement statuses
  (`OPEN_QUESTION` blocks a mandatory requirement), floors (`requirement-policy-v1`)
  and the typed `AcceptanceCase`. M12 ports `append_requirements` / `author_acceptance`
  record the authenticated human as author/decider.
- M12's verifier and M13's approval floors judge a release regardless of who or what
  wrote the source. A deterministic verifier is not a separate credential.
- There is no model provider credential, no verified egress to a model service and no
  real model in this environment.

## Decision

### 1. Invariant: a model holds no authority and has no identity
Model output is **data**. It is parsed into a closed, typed, hashed *proposal*,
stored, shown, and applied only by an authenticated human through an ordinary
endpoint. There is no AI actor, role, token or membership. The requesting human is
the actor of record everywhere (proposal requester, metadata revision author), so
every M12 separation-of-duties rule (acceptance author ≠ requester ≠ source author;
M13 approver floors) applies unchanged. A proposal never carries an approval,
acceptance, sandbox evidence, release, tool definition, policy or role. Those
surfaces are not representable in either proposal schema.

### 2. Module boundary
`internal/ai` is the only module that imports a provider or builds a prompt.
Its public contract is `internal/ai/contract.go` (this ADR's Go form). Storage
(`control.ai_*`) is accessed only by `internal/ai` store code through definer ports.
`internal/platform/access/ai.go` is the only HTTP surface. No other package may
import a provider adapter; an architecture test enforces it. No ordinary CRUD,
rule, workflow or tool path calls a model (cost/latency risk 37).

### 3. Model gateway (`ai.Provider`)
A provider turns a typed request (task, template version, trust-labelled
segments, closed output schema, token/time limits) into a JSON payload with usage
and latency. Business modules never see a vendor format. Shipped:
- `ScriptedProvider` — deterministic, in-memory, for tests, evaluations and
  development; can simulate malformed, oversized, slow, refusing and hostile output.
- `HTTPJSONProvider` — one generic adapter (`https-json`, `ai-provider-v1`) for an
  operator-controlled endpoint speaking the envelope in `contract.go`. HTTPS only
  (plain HTTP only to loopback in tests), SSRF-hardened dialer equivalent to the M10
  gateway (private/link-local/loopback/metadata ranges blocked at dial time, no
  redirects, no proxies), bounded request/response bytes, per-attempt deadline,
  credential read from an operator-provisioned file at call time and never stored,
  logged, audited or returned.
- Real vendor adapters (Anthropic, OpenAI, …) are **not** shipped and not claimed.
Configuration is operator-owned: `AI_PROVIDER_FILE` (`ai-provider-v1`: kind,
endpoint, model label, limits, defaults). Unset → every AI route answers
`503 ai_unavailable` and nothing else changes.

### 4. Semantic context bundle (`ai.BuildContext`)
Metadata only. Included: the draft document's definitions rendered as compact,
deterministic rows (ids, kinds, labels, field types, rule/workflow structure), the
diff of the draft against the active release when one exists, a tool *catalog
summary* (ref, version, side-effect class, approval requirement, input/output
names — never endpoints, hosts, credentials, sandbox outputs), role *names*, naming
rules, the open requirement ids. **Never** included: record data, user identities,
secrets, DSNs, tenant ids, policy bodies, audit rows. Size is capped
(`MaxContextBytes`); an app that does not fit is refused with a typed error, never
silently truncated. A redactor removes secret-shaped tokens from free text. Every
author-controlled string (labels, descriptions, requirement text, tool names,
diagnostics messages echoing input) goes into a segment marked `UNTRUSTED`; only
template text and server-built structure are `TRUSTED`. The bundle is built inside
the caller's tenant scope (RLS), so another tenant's data cannot enter it. The
bundle is canonical and hashed (`ContextHash`).

### 5. Structured output (closed, server-validated)
Two schemas: `ai-patch-proposal-v1` and `ai-requirement-proposal-v1`
(`contract.go`). Decoding is strict (the `metadata.DecodeJSON` rules: unknown
fields, duplicate keys, case-fold duplicate keys, trailing data, depth and size
caps → rejection). The model supplies **content only**. The server derives and the
model can never set: change class, risk, sensitive aspects, provenance,
requiredness and its basis, status, ids of proposals/revisions, hashes, actor,
timestamps. A payload that names such a field fails the schema. Model-declared
`risk_flags` and `proposed_tests` are advisory text/typed cases stored with the
proposal; they never lower a server-derived value and never become acceptance.

### 6. Allowed targets
Patch operations may target definitions of kind `page`, `entity`, `field`,
`relationship`, `rule` and `workflow` only (the implementation pins the list from
`metadata` kinds and tests that every other kind, including `tool`, is refused).
A workflow tool *activity* may reference only a tool already in the operator
catalog; the compiler decides availability. The AI cannot add or edit a tool
definition, policy, role, secret, sandbox, release, approval or acceptance.

### 7. Bounded repair loop (`ai.Gateway.RunPatch`)
attempt → strict parse → allowlist → apply to an in-memory copy of the current
draft with expected-hash CAS per target → validate and `Compile` with the real
compiler and catalogs → on diagnostics, feed back **only** the machine-readable
`Diagnostic` list (path, code, message — capped, sanitized, `TRUSTED` because the
compiler authored it) and the hash of the previous proposal → next attempt.
`MaxAttempts` is a server ceiling (3; config may lower, never raise). Provider
errors: at most one retry for a transient error within the same attempt budget.
Outcomes: `READY` (compiles; may still be wrong), `NEEDS_HUMAN` (retry limit with
diagnostics), `FAILED` (provider/parse/policy failure), `BUDGET_EXCEEDED`.
No outcome mutates the draft. The loop is pure orchestration over injected
`Provider`, `Compile` and clock; it opens no transaction.

### 8. Proposals and traceability (database)
Forced-RLS, tenant-scoped, definer-port-written tables (no DML for `framework_app`):
`control.ai_proposals` (immutable canonical body + hashes; status moves only via
ports), `control.ai_invocations` (append-only trace per attempt: provider, model
label, template version, context hash, request hash, output hash, usage, latency,
outcome, compiler/catalog hashes), `control.ai_usage` (per tenant per UTC day token
and request counters used for the budget). Raw prompts and raw model output are
**not** stored (hashes only; data minimisation). Ports: begin (admission:
authority, budget, concurrency), record-invocation, finish, decide (apply/reject/
confirm) with compare-and-set on status and on the draft revision. Model calls
happen **outside** any DB transaction (begin tx → record PENDING; call; finish tx),
like the M10 tool steps. A `PENDING` row older than the wall-clock limit is
reported `EXPIRED` and may be re-requested. All of this is specified to the
level of an ADR amendment by the DB builder before code (§16 process).

### 9. Apply (human, draft only)
`apply` is a separate authenticated request. The server re-validates against the
*current* draft (not the stored result): expected-hash CAS per target, strict
re-parse, compile, and `compiler.ComputeDiff(currentProgram, patchedProgram)` to
derive the sensitive aspects. If the diff contains any sensitive aspect (rule
logic/financial, workflow graph/approval gate/tool pin, relationship, storage), the
caller must echo the server-derived list in `acknowledge`; a mismatch is refused.
The result is a new metadata revision appended by the existing registry path as the
authenticated human, CAS on `expected_revision`, proposal status `APPLIED` (once).
It is a draft: sandbox (M11), candidate build and verification (M12) and approval
(M13) still apply in full and re-derive everything.

### 10. Intent capture
`requirement_capture` returns typed `RequirementDraft`s in the supported
categories plus open questions. They are stored in the proposal, not in the
requirement tables. `confirm` is a human act with a decision per draft
(`RESOLVE`, `KEEP_OPEN`, `REJECT`); the server then calls the M12 requirement port
as that human with provenance `AI_PROPOSED_HUMAN_CONFIRMED`, requiredness from
`requirement-policy-v1` floors (never from the model), status from the decision.
Missing required semantics are open questions (`OPEN_QUESTION`) and block promotion
exactly as in M12. Proposed tests are shown and may be copied by a human into the
ordinary acceptance route; the acceptance author rules are unchanged.

### 11. Budgets, cost and abuse
Server-side only: request text ≤ `MaxRequestRunes`, context ≤ `MaxContextBytes`,
output ≤ `MaxOutputBytes`, ≤ `MaxOperations` operations, ≤ `MaxAttempts` attempts,
wall clock per proposal, per-tenant daily token budget and a per-tenant in-flight
limit, all checked in the begin port (fail closed). Over budget → `429
ai_budget_exhausted`; provider off → `503 ai_unavailable`. Usage is recorded from
the provider response and clamped to the budget estimate if absent (pessimistic).

### 12. Policy and API
Native policy `m14-native-v11`; actions `ai.read`, `ai.propose`, `ai.apply`
(owner, admin, member for propose/apply/read where metadata write is allowed today;
viewer: `ai.read` only; custom roles per ADR032 conventions, default-deny).
Preview-gated, exact-pattern routes with strict bodies:
`POST /preview/{app}/ai/patch-proposals`, `POST /preview/{app}/ai/requirement-proposals`,
`GET /preview/{app}/ai/proposals`, `GET /preview/{app}/ai/proposals/{id}`,
`POST /preview/{app}/ai/proposals/{id}/apply`, `…/confirm`, `…/reject`.
Every attempt, apply, confirm and reject is audited with hashes (never prompts).

### 13. Prompt-injection and agency controls (tested, not asserted)
Corpus executed against the scripted provider and HTTP: direct injection in the
request; indirect injection in labels, descriptions, requirement text, tool
descriptions and diagnostics; payloads that try to name server-derived fields,
unknown ops, forbidden kinds (`tool`, policy-like ids), schema-version downgrades,
oversized/deep/duplicate-key/case-fold-key JSON; hostile segments imitating
delimiters; attempts to apply without acknowledgement, apply twice, apply after the
draft moved, apply another tenant's proposal. Required outcome: nothing is applied,
no actor/role/permission/tool/policy surface is touched, everything is audited, no
cross-tenant context.

### 14. Provider-swap evaluation
`ai.Evaluate(provider, corpus)` reports validity rate, repair count, latency, token
use and acceptance of golden cases. The same corpus must produce the same
validation outcomes through the scripted provider and through the HTTP adapter
(against a local test server). This demonstrates substitutability of the gateway,
not model quality.

### 15. Non-goals and honest limits
- No real model was called; template quality, repair success rates and cost are
  **unmeasured** against any real model. Real vendor adapters are not shipped.
- Closed schemas and the compiler protect *structure*; a model can still propose
  syntactically valid but business-wrong logic. The control for that is M12 (verifier
  + independent acceptance) and M13 (approval), which this milestone does not weaken
  and does not replace.
- Metadata (labels, rule/workflow structure) leaves the platform to the configured
  provider; enabling AI is an operator privacy decision. Record data never does.
- Synchronous request/response only (bounded by a wall clock below the HTTP
  server limits); async authoring, streaming, embeddings, UI, multi-turn chat,
  autonomous agents and AI workflow activities are out of scope.
- Identity binding of the requester is the API process's (ADR103 residual).

### 16. Process
Builders work on separate branches and exchange only through this ADR,
`internal/ai/contract.go` and the contract log. Contract changes are appended to
`/home/user/m14-env/CONTRACT_CHANGES.md` and mirrored as an "Amendments" section
here. The independent review of M14 is a separate agent and, until it has run, the
milestone is reported **unreviewed**.

## Consequences
Proposals become auditable, replayable records (hashes + canonical body); the
repair loop and apply are deterministic given the provider's bytes. Operators must
provide a provider file and credential to use AI at all. Tenants' definitions are
shared with that provider. Every apply is still a human act and every promotion
still passes M11–M13.

## Verification and recovery
Unit: strict decoding corpus, allowlist, context determinism/redaction/caps,
repair-loop table tests with scripted providers, apply CAS and acknowledgement,
provider SSRF/size/timeout tests, evaluation harness equivalence. Integration
(isolated stack): ports/RLS/grants, budget and concurrency, expiry, all routes with
the policy matrix, injection corpus end to end, audit rows, cross-tenant denial,
apply → draft revision → existing sandbox/candidate path still works. Recovery:
migrations are additive and forward-only; revoke EXECUTE on the begin port to stop
all AI; proposals and traces are never deleted.

## Amendments — persistence and API (m14-api, 2026-10-18)

These specify §8, §11 and §12 down to tables, ports, statuses, HTTP and numbers. They
supersede any looser wording above. Implementation: migration
`db/migrations/00089_m14_ai_proposals.sql`, `internal/ai/store*.go`,
`internal/ai/service*.go`, `internal/platform/access/ai*.go`, policy `m14-native-v11`,
`internal/platform/app/ai.go`. Everything here is a development/test preview; the
provider seam, not a real model, is what is exercised.

### A1. Tables (migration 00089)

All three: tenant-inclusive keys, `FORCE ROW LEVEL SECURITY` via `runtime.install_tenant_rls`
(owner `framework_schema_owner`), `SELECT` for `framework_app` only through the tenant scope
policy, **no** INSERT/UPDATE/DELETE/TRUNCATE/REFERENCES/TRIGGER for any runtime role,
restrictive `worker_no_generic` policy, no worker or dispatcher access, DELETE and TRUNCATE
refused by `control.immutable_artifact()`. Rows are written only by the ports of A3: a trigger
(`control.ai_port_write`) refuses any INSERT/UPDATE unless `session_user='framework_app'` and
`current_user='framework_schema_owner'` (an operator cannot hand-write a proposal).

`control.ai_proposals`, PK `(tenant_id, app_id, id)`, UNIQUE `(tenant_id, id)`, FK
`(tenant_id, app_id)` → `control.metadata_heads(tenant_id, id)`:

| group | columns |
|---|---|
| identity (immutable) | `id uuid` (default `gen_random_uuid()`), `kind` `patch\|requirements`, `requester_id uuid` (default `iam.authenticated_actor()`; the authenticated human is the only actor of record — `(tenant_id, requester_id)` is the membership, `iam.memberships` has no surrogate key), `request_hash` (sha256 of the human's request text; **the text is not stored**), `source_revision` + `draft_hash` (the draft head revision and its metadata source hash the context was built from), `usage_day date` (UTC day charged), `reserved_tokens`, `created_at`, `expires_at` |
| result (set once at finish) | `status`, `finished_at`, `context_hash`, `proposal_hash` (`= sha256(body)`, CHECK), `body bytea` (the validated canonical proposal, 2..262144 bytes — never raw model output), `derived jsonb` (server-derived; A9), `diagnostics jsonb` (≤ 40 compiler diagnostics, compiler-authored), `diagnostics_hash`, `failure_code`, `attempts`, `input_tokens`, `output_tokens` |
| decision (set once at decide) | `decided_by`, `decided_at`, `reject_reason`, `result_revision` (patch: the appended metadata revision; requirements: the appended requirement-set version), `result_hash` (that revision's source hash / the set's `body_hash`), `decision_hash` (sha256 of the canonical decision record) |

`status` ∈ `PENDING READY NEEDS_HUMAN FAILED EXPIRED APPLIED CONFIRMED REJECTED`.
`failure_code` ∈ the attempt outcomes `PARSE_ERROR SCHEMA_ERROR FORBIDDEN_TARGET CONFLICT
COMPILE_ERROR PROVIDER_ERROR TIMEOUT BUDGET` plus `EXPIRED NOT_CONFIGURED INTERNAL`.
Table CHECKs make an inconsistent row unrepresentable even for the definer: READY/APPLIED/
CONFIRMED/REJECTED carry a body whose hash matches and a `derived` object; APPLIED requires
`kind='patch'` and CONFIRMED `kind='requirements'`, both with `result_revision` and
`result_hash`; APPLIED/CONFIRMED/REJECTED carry `decided_by/at`; PENDING carries no result.
A partial UNIQUE index on `(tenant_id, app_id, kind, result_revision)` makes a revision the
result of at most one proposal. Trigger `control.ai_proposal_step` allows only the transitions
of A4 and forbids changing any identity, result or decision column once set.

`control.ai_invocations` (append-only trace, one row per provider attempt), PK
`(tenant_id, proposal_id, attempt)`, FK `(tenant_id, app_id, proposal_id)` → `ai_proposals`:
`attempt` 1..16, `provider` and `model` (labels, `[A-Za-z0-9._:/@+-]`, ≤ 64/128), `template_version`,
`context_hash`, `request_hash` (canonical request, hash of the bytes sent), `output_hash`
(empty when no output), `input_tokens`, `output_tokens`, `latency_ms`, `outcome` (the
`Outcome*` constants of `contract.go`), `diagnostics_hash` (empty or sha256), `compiler_hash`
(sha256 over the compiler version and the operator catalog hashes), `recorded_at`. UPDATE,
DELETE and TRUNCATE are refused. **No prompt, no segment text and no model output is a column.**

`control.ai_usage`, PK `(tenant_id, usage_day)`: `requests`, `used_tokens` (non-negative;
only ever grow). `usage_day` is the UTC date of `clock_timestamp()`; a new day is a new row
(the daily rollover is the key, nothing is reset). DELETE and TRUNCATE are refused.

`control.worker_grants_safe()` is replaced from its latest definition (migration 84) with the
three tables added; the migration ends with a self-check that fails it if any runtime role
keeps a direct privilege or an extra policy on them. *(Struck by Amendment B.7: migration 00089
does not replace the guard; since migration 87 it is derived and refuses every unlisted relation.)*

### A2. Authority and identity

Every port derives tenant and actor from the leased session settings (`iam.current_tenant_id()`,
`iam.authenticated_actor()`) exactly as the M12/M13 ports do and requires
`control.release_actor_ok(tenant)` (active non-viewer member of the scope). The caller never
passes a tenant, an actor, a role or a status. A model has no identity: `requester_id` and
`decided_by` are humans; there is no AI membership, token or role anywhere.

### A3. Ports (SECURITY DEFINER, owner `framework_schema_owner`, `search_path=pg_catalog,pg_temp`, EXECUTE only for `framework_app`)

All four take `pg_advisory_xact_lock(hashtextextended('framework.ai:'||tenant,0))` first
(admission, accounting and expiry of one tenant are serialised; the runtime `lock_timeout`
of 500 ms bounds the wait).

1. `control.ai_begin(p_app text, p_kind text, p_request_hash text, p_source_revision bigint,
   p_draft_hash text, p_daily_tokens bigint, p_reserve_tokens bigint, p_inflight integer,
   p_lease_seconds integer) RETURNS uuid` — admission. Validates arguments (22023: app
   pattern, kind, hashes, `1 ≤ revision ≤ 1e9`, `1 ≤ daily ≤ 100 000 000`, `1 ≤ reserve ≤
   10 000 000`, `1 ≤ inflight ≤ 16`, `1 ≤ lease ≤ 600` — hard database ceilings a compromised
   API cannot exceed); requires the draft head to exist (P0002) and to equal
   `p_source_revision` with `source_hash = p_draft_hash` (55000 `draft_changed`); marks
   this tenant's PENDING rows past `expires_at` as `EXPIRED` and charges their reservation to
   `used_tokens` of their day (pessimistic: the tokens may have been spent); refuses when the
   non-expired PENDING count ≥ `p_inflight` (53400 DETAIL `in_flight`) or when `used_today +
   reserved(non-expired PENDING) + p_reserve > p_daily` (53400 DETAIL `daily_budget`); inserts
   the PENDING row (`expires_at = now + lease`) and bumps `requests`. Returns the proposal id.
2. `control.ai_record_attempt(p_app, p_proposal uuid, p_attempt integer, p_provider, p_model,
   p_template, p_context_hash, p_request_hash, p_output_hash, p_input_tokens bigint,
   p_output_tokens bigint, p_latency_ms integer, p_outcome, p_diagnostics_hash,
   p_compiler_hash) RETURNS void` — appends one trace row. The proposal must exist in the
   tenant and app (P0002), be PENDING and unexpired (55000 DETAIL `not_pending`), and belong to
   the caller (42501). Charset/length of labels and hash formats are checked (22023).
3. `control.ai_finish(p_app, p_proposal uuid, p_status text, p_body bytea, p_proposal_hash
   text, p_derived jsonb, p_diagnostics jsonb, p_diagnostics_hash text, p_failure_code text,
   p_context_hash text, p_attempts integer, p_input_tokens bigint, p_output_tokens bigint)
   RETURNS text` — compare-and-set PENDING → `READY | NEEDS_HUMAN | FAILED`. Verifies the
   caller is the requester (42501), `sha256(p_body) = p_proposal_hash` and the body's top-level
   `version` equals the kind's schema version (22023), status/body/derived consistency
   (READY requires body and `derived`; FAILED carries no body and a `failure_code`),
   `octet_length ≤ 262144`. Charges the actual usage to `used_tokens` (even if the row is
   expired — then the row becomes `EXPIRED` and the function **returns `'EXPIRED'`** rather than
   raising, so the charge commits). Returns the stored status.
4. `control.ai_decide(p_app, p_proposal uuid, p_decision text, p_expected_revision bigint,
   p_result_revision bigint, p_result_hash text, p_reject_reason text, p_decision_hash text)
   RETURNS text` — `p_decision` ∈ `APPLY CONFIRM REJECT`. Row-locks the proposal; binds
   tenant (session), app (`p_app` must equal the row's), and the caller as an active non-viewer
   member; requires status READY (55000 DETAIL `not_ready` for PENDING/NEEDS_HUMAN/FAILED/
   EXPIRED, DETAIL `decided` for APPLIED/CONFIRMED/REJECTED) — so a decision happens **once**;
   APPLY only for `kind=patch`, CONFIRM only for `kind=requirements` (22023). APPLY requires
   `p_result_revision = p_expected_revision + 1`, that revision to be the current head of the
   app and its `source_hash = p_result_hash` and `created_by = caller` (the append happened in
   this transaction, by this human); CONFIRM requires the same of the app's requirement set
   (`version = p_result_revision`, `body_hash = p_result_hash`, `decided_by = caller`, and the
   current head). REJECT requires `p_reject_reason` ∈ `NOT_WANTED INCORRECT UNSAFE SUPERSEDED
   OTHER`. Sets status `APPLIED | CONFIRMED | REJECTED` and the decision columns, returns it.
   **The decider need not be the requester**: `ai.propose` and `ai.apply` are separately
   delegable (A6), and the requester is recorded, not required; every downstream separation
   rule (M12 acceptance author, M13 approver floors) keys on the human actors of record.

SQLSTATE contract of the AI ports: `22023` invalid argument; `42501` not a member / not the
requester; `P0002` not found (also another tenant's or app's id); `55000` state conflict with
DETAIL ∈ `draft_changed not_pending not_ready decided expired`; `53400` limit with DETAIL ∈
`daily_budget in_flight`; `40001 40P01 55P03 23505 23503` concurrency. Revoking EXECUTE on
`ai_begin` stops all new AI requests (a missing privilege surfaces as `42501`, answered 403).

### A4. Status machine

```
PENDING ─finish─► READY | NEEDS_HUMAN | FAILED          (requester, once)
PENDING ─expiry─► EXPIRED                                (begin sweep, or finish after expires_at)
READY ──decide──► APPLIED (patch) | CONFIRMED (requirements) | REJECTED
```
NEEDS_HUMAN, FAILED, EXPIRED, APPLIED, CONFIRMED and REJECTED are terminal. Reads report a
PENDING row past `expires_at` as `EXPIRED` even before a sweep stored it. `expires_at = created_at +
lease`, lease = effective wall clock + 20 s (covers both short transactions and the provider
phase); a re-request is simply a new POST. A crash between admission and finish therefore
leaves a PENDING row that stops counting against the in-flight limit and is charged
pessimistically when swept; it can never become READY later.

### A5. Budget, concurrency and the numbers that are server configuration

Nothing below is caller-controlled. Run budget = reservation: the gateway receives
`Budget.TokensRemaining = reserve`. `reserve = attempts × (ceil(min(draft+request bytes+2048,
MaxContextBytes)/4) + MaxOutputTokens)`. Daily budget is per tenant per UTC day over
reserved + used tokens. Usage is taken from the provider's report; the gateway substitutes the
pessimistic estimate when none is given, the port never lowers it.

| number | source | default | ceiling |
|---|---|---|---|
| attempts | `ai-provider-v1` `max_attempts` / `Limits.MaxAttempts` | `ai.MaxAttempts` (3) | 3 (contract) |
| output tokens | `max_output_tokens` | 4096 | 16384 |
| daily tokens per tenant | `daily_token_budget` | `ai.DefaultDailyToken` (200 000) | 100 000 000 (port) |
| in-flight per tenant | `in_flight_per_tenant` | 2 | 16 (port) |
| provider-phase wall clock | `wall_clock_ms`; never above `ai.APIWallClockCeiling` (= `DefaultWallClock`) | 60 s per contract | 60 s (Amendment B.9; this row said 6 s before) |
| lease | wall clock + 20 s | 80 s | 600 s (port) |
| request text | `MaxRequestRunes` | 4000 runes | — |
| list page | `MaxListItems` | 50 | — |
| body caps | create 32 KiB, apply 128 KiB, confirm 64 KiB, reject 4 KiB | | 1 MiB (global) |

`httpserver.New` sets 10 s read and write deadlines and each runtime transaction is capped at
3 s (`postgres.WithRuntime`). The provider phase holds neither a transaction nor a connection,
so only the write deadline limits it: after the admission transaction has committed and before
the provider is called, the route moves the write deadline of its own connection to
`wall clock + 10 s` (`reply.nextWithin` in `access.serve`, `http.NewResponseController`); the
margin covers the result transaction and the response. No other route changes its deadline.
A proxy or load balancer in front of the API may still impose a shorter limit; the API then
sees the client gone, cancels the provider call and records the proposal `FAILED`/`INTERNAL`
(the result transaction is detached from the client's cancellation), which frees the in-flight
slot. Provider calls that need more than 60 s need the asynchronous slice (non-goal §15).

### A6. Policy `m14-native-v11`

New action names `ai.read`, `ai.propose`, `ai.apply` (catalog hash changes, so candidates
built under `m13-native-v10` are `POLICY_CHANGED`, as for M12/M13). Resource type `preview`
with the route's `app`.

| role | `ai.read` | `ai.propose` | `ai.apply` |
|---|---|---|---|
| owner, admin | allow | allow | allow |
| member | allow | deny | deny |
| viewer | allow | deny (cannot be granted) | deny (cannot be granted) |
| stored custom role | per grants | per grants (never a role named `viewer`) | per grants (never `viewer`) |
| any other / unknown | deny | deny | deny |

Rationale for `member`: ADR104 §12 grants propose/apply "where metadata write is allowed
today"; `metadata.write` is owner/admin, so a member gets read only (a member who could apply
would write metadata the member cannot write). `ai.apply` covers `apply`, `confirm` and
`reject`. Composition, enforced by the handler with a second `Decide` (its decision id is
audited on denial): **apply additionally requires `metadata.write`** and **confirm additionally
requires `requirements.decide`** for the same app, so delegating `ai.apply` alone never
bypasses the capability the underlying write needs.

### A7. HTTP (all under `/v1/tenants/{tenant}/preview/{app}/ai/…`, registered only where preview exists; exact patterns, drift-tested against the body allowlist)

| route | action | body (strict) |
|---|---|---|
| `POST …/ai/patch-proposals` | `ai.propose` | `{"request":"…≤4000 runes","expected_revision":N?}` |
| `POST …/ai/requirement-proposals` | `ai.propose` | same |
| `GET …/ai/proposals` | `ai.read` | none; newest 50, summaries (no body) |
| `GET …/ai/proposals/{id}` | `ai.read` | none; full view |
| `POST …/ai/proposals/{id}/apply` | `ai.apply` | `{"expected_revision":N,"acknowledge":["<target>:<aspect>",…]}` |
| `POST …/ai/proposals/{id}/confirm` | `ai.apply` | `{"expected_requirements_version":N,"decisions":[{"id":"…","decision":"RESOLVE\|KEEP_OPEN\|REJECT"},…]}` |
| `POST …/ai/proposals/{id}/reject` | `ai.apply` | `{"reason":"NOT_WANTED\|INCORRECT\|UNSAFE\|SUPERSEDED\|OTHER"}` |

Bodies: `application/json`, one object, lowercase keys, unknown / duplicate / Unicode
case-folded keys, trailing data and depth bombs → 400 `invalid_ai_request`; over the cap →
413 `body_too_large`. No field of a proposal, status, hash, risk, actor, requiredness,
provenance or revision result is accepted from a caller. `expected_revision` is a caller
assertion checked against the head (compare-and-set), never a source of truth. Every
`{id}` is a UUID; any other string, another tenant's or another app's id is 404 `not_found`.

Proposal view (create → 201, get → 200, also embedded in decision replies): `id, app_id,
kind, status, requester_id, created_at, expires_at, source_revision, draft_hash,
request_hash, context_hash, proposal_hash, attempts, usage{input_tokens,output_tokens},
proposal` (the stored canonical object, absent for FAILED), `derived`, `diagnostics`,
`failure_code`, and once decided `decided_by, decided_at, reject_reason, result_revision,
result_hash`. The list returns the same fields except `proposal`, `derived` and `diagnostics`.

A creation request answers **201 with the stored proposal for every pipeline outcome**:
`READY`, `NEEDS_HUMAN` (a normal result carrying `diagnostics`, never 422) and `FAILED`
(`failure_code` set). Exceptions, all carrying the proposal id when one exists:
`503 ai_unavailable` when no provider is configured or the run failed with
`PROVIDER_ERROR`/`TIMEOUT`; `429 ai_budget_exhausted` (daily budget at admission, or
`BUDGET` during the run); `429 ai_busy` (in-flight limit); `422 ai_context_too_large`
(nothing stored, nothing charged).

HTTP mapping of the rest: policy denied 403 `forbidden`; app without a draft 404
`not_found`; `expected_revision` ≠ head 409 `draft_changed`; proposal not READY 409
`proposal_not_ready`; already decided 409 `proposal_decided`; per-target conflicts against the
current draft 409 `proposal_conflict` (with `conflicts`); `acknowledge` not equal to the
server-derived sensitive list 409 `acknowledge_mismatch` (with the derived `sensitive`
list); metadata head moved during apply 409 `draft_changed`; requirement ids already present or
set head moved 409 `requirements_changed`; patched draft no longer compiles 422
`proposal_invalid` (with `diagnostics`); RESOLVE of a draft in a category the verifier does not
understand 422 `category_not_resolvable`; finish after expiry 409 `proposal_expired`;
serialization/deadlock/lock timeout 409 `ai_conflict` (after the tries of Amendment C.4; it was "no automatic
retry" before the independent review); every unexpected failure fails closed (503 `service_unavailable`,
transaction rolled back).

**Unconfigured provider:** with no `ai.Provider` injected and `AI_PROVIDER_FILE` unset, the
routes still exist (in preview) and every one answers 503 `ai_unavailable` to an
authenticated, authorized caller, before any body is read or any database row is written; no
other route, port or table is affected.

### A8. Orchestration (the model call is never inside a transaction)

Creation is three exchanges, each re-authenticating the same bearer token (identity, tenant
membership and policy are re-evaluated in every one):

1. **Admission transaction** — authenticate, policy `ai.propose`, strict body, read the draft
   head, `ai_begin`, build the context inside the caller's tenant scope
   (`Gateway.BuildContext`), audit `ai.proposal.admitted`, commit. A refusal rolls everything
   back (including the PENDING row) and is audited in a fresh transaction.
2. **Provider phase** — no transaction, no connection held: `Gateway.RunPatch` or
   `RunRequirements` with the request context, the wall clock (A5) and `Budget`.
3. **Result transaction** (detached from client cancellation, own deadline) —
   re-authenticate, `ai_record_attempt` per attempt, `ai_finish`, audit
   `ai.proposal.created` (READY/NEEDS_HUMAN/FAILED) and build the response.

Apply, confirm, reject and the GETs are single ordinary transactions (apply recompiles the
current draft and the patched draft inside the 3 s transaction, like the M12 builder).

### A9. Apply and confirm in detail

**apply**: load the proposal (RLS) → must be READY patch (else 409) → `sha256(body)` equals
`proposal_hash` and the body re-parses strictly (`ParsePatchProposal`) → `expected_revision`
equals the head (else 409 `draft_changed`) → `ApplyProposal(currentDraft, proposal)` (per-target
expected-hash CAS against the **current** draft, never the stored result; conflicts → 409) →
the patched document must pass the metadata validator and compile with the operator catalogs
(else 422) → `DeriveSensitiveAspects(currentProgram, patchedProgram)`; when the current draft
itself does not compile, the active release program is the base, and without either the base
is empty (every aspect counts as new) → the sorted `acknowledge` must equal the sorted derived
`sensitive` list exactly (no duplicates, no extras, empty list when none) → `Registry.Append`
with `expected_revision` as the authenticated human → `ai_decide(APPLY)` in the same
transaction. The result is a new draft revision; sandbox, candidate and approval are untouched.

**confirm**: load → READY requirements proposal → re-parse strictly → `decisions` must name each
draft exactly once (missing, unknown or duplicate ids → 400) → a draft whose category is not
verifiable (`RequirementV2.Known()` false) may not be RESOLVE (422) → new set = current head
set + one `RequirementV2` per draft with `Provenance = AI_PROPOSED_HUMAN_CONFIRMED`,
`Requiredness/Basis` from `requirement-policy-v1` evaluated at its strictest storage class
(`FloorFor(category, non-EQUAL)`: MANDATORY/`POLICY_FLOOR` for the four verifiable categories,
OPTIONAL/`OWNER_DECISION` otherwise — never from the model), `Status` from the decision
(RESOLVE → RESOLVED, KEEP_OPEN → OPEN_QUESTION, REJECT → REJECTED), validated with the typed
decoders → `expected_requirements_version` must equal the head (0 for none) and no id may
already exist → the M12 port `control.append_requirements` as the human (stamps `decided_by`) →
`ai_decide(CONFIRM)`. A mandatory OPEN_QUESTION/REJECTED requirement therefore blocks a
candidate build exactly as in M12. The proposal's `open_questions` and `proposed_tests` stay
informational (the human may copy a test into the ordinary acceptance route); statements are
not part of `requirements-v2` and are kept only in the proposal.

`derived` for `kind=patch` is `ai.Derived` (targets with server-derived kind and op,
sensitive `<target>:<aspect>` list, aspects) computed against the draft at proposal time and
recomputed at apply; for `kind=requirements` it is `{"requirements":[{"id","category",
"requiredness","requiredness_basis","verifiable"}],"open_questions":N}`.

### A10. Audit (hashes and closed codes only)

Allowed outcomes: `ai.proposal.admitted`, `ai.proposal.created`, `ai.proposal.applied`,
`ai.proposal.confirmed`, `ai.proposal.discarded` (a human reject — named so it cannot be
confused with the `.rejected` suffix of refused requests); reads record `ai.read`. Refused or
failed requests record `<action>.rejected` (`ai.propose.rejected`, `ai.apply.rejected`,
`ai.read.rejected`, allowed=false); a policy denial records the action itself with
allowed=false. State fields (all `omitempty`, validated in `internal/audit`): `ai_proposal_id`,
`ai_kind`, `ai_status`, `ai_failure`, `ai_request_hash`, `ai_context_hash`, `ai_proposal_hash`,
`ai_diagnostics_hash`, `ai_ack_hash`, `ai_template`, `ai_provider`, `ai_model`, `ai_attempts`,
`ai_input_tokens`, `ai_output_tokens`, `ai_source_revision`, plus the existing
`metadata_revision/metadata_hash` (apply result) and `requirements_version/manifest_hash`
(confirm result). A prompt, request text, model output, definition text or free-text note is
never an audit field.

### A11. Wiring

`access.API.AI *ai.Service` (nil = unconfigured). `internal/platform/app/ai.go` builds it from
`AI_PROVIDER_FILE` (`ai.LoadProviderConfig` + `ai.NewProvider` + `ai.NewGateway`); a bad file
stops startup like a bad tool catalog. Readiness adds an `ai` probe that is ok when AI is not
configured and otherwise verifies the ports, grants and forced RLS of A1/A3. Tests inject
a provider or a gateway directly.

### A12. Residuals stated plainly

- The ports bind the actor from the API's session settings (ADR103 residual): a compromised
  API can open, finish and decide proposals as any member whose identity it can bind. It
  gains no authority it did not have: a decision requires an append of the very revision or
  requirement-set version the port checks, and the human could already write both.
- Budget limits are passed by the API; the database caps them at fixed ceilings (A3) but
  cannot know the configured value. The body hash proves the stored body is what was
  submitted, not that it is what a model returned.
- Expiry charges a crashed request pessimistically only when a later admission sweeps it; a
  crashed request keeps its in-flight slot until the lease lapses (80 s at the 60 s ceiling).
- A provider phase of up to 60 s holds an HTTP connection (and a goroutine) for up to 70 s without
  holding a database connection. The per-tenant in-flight limit bounds this per tenant; the API
  has no global limit on such requests, and a proxy in front of it may cut them earlier (A5).
- No real model was called: this slice proves the seam, the durability and the authority model, not
  model quality, cost or latency (§15).
- The first independent review of M14 ran after this was written (2026-10-20 to 22, `docs/verification/M14_INDEPENDENT_REVIEW.md` and
  `M14_AND_FIXES_INDEPENDENT_REVIEW.md`); what it found and what changed is Amendment C. Everything above Amendment C is builder-side evidence.

## Amendment B — pipeline decisions and corrections (2026-10-19)

Decisions made while building `internal/ai`, and corrections of numbers stated earlier in this ADR. The
code is the source of truth for each of them.

1. **Repair diagnostics are `UNTRUSTED` data (corrects §7).** Compiler diagnostics can echo strings that an
   author controlled (paths and identifiers), so the repair segment is labelled `UNTRUSTED`; only the
   repair lead-in text is `TRUSTED`. The data/instruction split of the system template covers it
   (`template.go`).
2. **Token reservation (corrects A5).** `reserve = attempts × (ceil((min(draft+request+4096, MaxContextBytes) +
   schema + 26 KiB repair overhead)/4) + MaxOutputTokens)`, capped; the schema text is part of every request
   and so of the reservation (`service.go: reserveFor`).
3. **Output token numbers (corrects A5).** The default is 8192 (`DefaultMaxOutputTokens`) and the ceiling is
   `MaxOutputBytes/4` = 65536 (`MaxOutputTokensCeiling`), not 4096 and 16384. Attempts remain capped at 3.
4. **Redaction rules.** Before anything reaches a prompt, free text passes these rules, each replaced by a
   class marker: PEM blocks, URLs with credentials, JWTs, API-key shapes (`sk-`, `rk-`, `pk-`, live/test
   keys, GitHub tokens), AWS key ids, bearer tokens, `authorization:` values, `name = value` assignments with
   a secret-like name, hex runs of 32+ characters and high-entropy base64 runs of 40+ characters. It is a
   mechanical filter, not a guarantee that no secret survives (§15). (Extended after the third independent review: Amendment C item 6.)
5. **HTTP provider.** HTTPS only; a guarded dialer refuses private, link-local, loopback and metadata
   addresses at dial time; no redirect is followed, including a redirect to the same origin (the credential
   is never sent twice); request and response are bounded; the credential file is read per call and never
   retained or logged. Loopback exists only through a test-only option that configuration cannot set.
6. **A proposal of the wrong kind is `409 proposal_wrong_kind`** (applying a requirement proposal or
   confirming a patch). Rejecting a proposal needs `ai.apply` only.
7. **Worker drift guard.** Migration 00089 does not replace `control.worker_grants_safe()`: since migration 87
   the guard is derived and refuses every unlisted relation of control, runtime, iam and audit, and the AI
   tables grant the worker nothing. (An earlier draft replaced the guard from its migration-84 definition,
   which the merge with migration 87 would have undone.)
8. **Test evidence added by the builder side** (not independent review): a 412-item attack corpus run against
   the pure pipeline and, for 62 items, through the real routes; a provider-swap comparison over 145 items;
   architecture rules ARCH-01..10; eleven mutation checks (`docs/verification/M14_ADVERSARIAL_PLAN.md`).
9. **The provider phase is no longer clamped to 6 s (corrects A5, A8 and the A12 residual).** The clamp existed
   because the server's 10 s write deadline would cut a longer exchange. The phase holds no transaction and
   no connection, so the route now moves the write deadline of its own connection to `wall clock + 10 s` and
   `ai.APIWallClockCeiling` equals the contract's `DefaultWallClock` (60 s). Evidence: an 11 s scripted
   answer behind a real `httpserver.New` server gives a stored READY proposal
   (`TestM14APIProviderPhaseOutlivesTheServerWriteTimeout`); with the extension removed the same request
   ends in EOF at the 10 s mark (mutation check, `M14_ADVERSARIAL_PLAN.md`); a client that gives up
   mid-phase leaves a `FAILED`/`INTERNAL` proposal, one provider call and a free slot
   (`TestM14APIClientGoneDuringProviderPhaseStillRecordsTheOutcome`). Not executed: a 60 s phase, a real
   proxy in front of the API, a real provider.

## Amendment C — independent review corrections (2026-10-22)

The independent review of M14 (findings M14R-01 to M14R-08, `docs/verification/M14_INDEPENDENT_REVIEW.md`) and the re-verification of
the fixes (F-05 to F-08, `docs/verification/M14_AND_FIXES_INDEPENDENT_REVIEW.md`) changed the following. Each item names the test that fails
without the change; the reviewers' own probes were run against the fixed code by the reviewer.

1. **Provider labels are reduced in the gateway (M14R-01, F-07; corrects §3 and A10).** The provider and model labels of an answer are
   provider-chosen text. `gateway.loop` reduces both once, with `apiLabel`, to `[A-Za-z0-9._:/@+-]`: each run of other characters becomes one
   underscore between two kept characters (`Beta-3.1 (70B)` is `Beta-3.1_70B`, `alpha 4 turbo` is `alpha_4_turbo`), a provider with nothing left is
   `unknown`, a model is empty. The trace, the audit entry and the API see the same text. Before, a label with a space, a parenthesis or a non-ASCII
   letter reached the audit entry raw, the audit refused it and a paid answer became a 503 with a PENDING proposal that held its slot until the
   lease (`TestM14R01ProviderLabelsOutsideTheAuditCharsetKeepTheResult`).
2. **A canceled provider call is charged (M14R-02; corrects the A12 statement that a client that gives up leaves no usage).** When the request
   context is canceled while the provider works, the attempt is recorded as `TIMEOUT` with the input estimate and the whole output that was
   reserved (`MaxOutputTokens`), the run is `FAILED`/`INTERNAL`, and the result exchange stores the trace and the usage. The closed outcome
   vocabulary has no `CANCELED`; `TIMEOUT` means "the effect on the provider is unknown", which is what the daily budget needs. Twenty canceled cycles
   used to make twenty provider calls and charge nothing; with the daily budget of the test the sixth is refused 429 `ai_budget_exhausted`, and the
   control that spends the same budget on completed requests is refused at the same point
   (`TestM14R02CanceledProviderCallsAreChargedToTheDailyBudget`).
3. **A reported usage is capped per half (M14R-06, F-08; extends A5).** The charge of an attempt is the provider's report, never less than
   the estimate of the bytes sent and returned, and never more than `2 x estimate + 1024` for the input and `2 x MaxOutputTokens + 1024` for the
   output (a larger answer is still charged its byte estimate). One answer that reported 100 million tokens used to charge them all and lock the
   tenant out of the UTC day; now an attempt costs about twice its reservation at most
   (`TestM14R06AnEnormousReportedUsageDoesNotLockTheTenantOut`, `TestChargeUsageIsCappedNearTheSizeOfTheRequest`).
4. **Admission and the result exchange are repeated after a known abort (M14R-03; corrects A7 and A8).** Serialization failures (`40001`), deadlocks
   (`40P01`) and the other aborts of `postgres.KnownAbort` leave nothing behind, so they are tried again: the first exchange of the two creation routes
   up to 5 times within 4 s (in `serve`, the body is replayed), the result exchange up to 6 times within 6 s of its detached 8 s context
   (`aiResultExchange`). Every try authenticates, decides and audits again; an aborted try stored and charged nothing and a finished proposal answers
   `not_pending`, so a repetition cannot store or charge twice (the re-verification: 18 of 18 results stored, one admitted and one created audit row each,
   tokens charged equal `used_tokens`). When the last try still aborts the request is refused `409 ai_conflict`, audited, as before. Without it, results that
   finished together lost 3 of 6 paid answers per round (`TestM14R03ResultsThatFinishTogetherAreNotLost`) and the proposal stayed PENDING until the lease
   (`TestM14R03CreationsRacingForTheUsageRowLoseNoPaidAnswer`).
5. **The wall clock binds an adapter that ignores its context (M14R-07; corrects the contract of `ai.Provider`).** `gateway.call` runs the adapter in its
   own goroutine and abandons it when the context ends (`TIMEOUT`, charged the input estimate). About 16 abandoned calls are tolerated per gateway (the bound is soft: calls already
   started when the count is reached are not stopped); beyond that new calls are refused as unavailable, for every tenant, because the provider is one per process and is stuck, and the refusal ends when the
   stuck calls return. An abandoned call keeps its goroutine and memory until the adapter returns. The production HTTP adapter honours its context
   (`TestM14R07AnAdapterThatIgnoresItsContextCannotHoldTheRequest`, `TestAbandonedProviderCallsAreBounded`).

6. **Redaction covers the shapes the review found surviving (M14R-08, 2026-10-28; extends B.4).** The third reviewer's probe found 10 of 28 secret
   shapes surviving the redactor. Run against the unchanged table with values derived at run time (no secret-shaped literal is in the source), nine of
   the ten classes were reproduced; the tenth, an Azure storage connection string, survived only with a key too short for the generic base64 rule, so
   it is now covered by name. The table now also redacts: the signature of a shared access signature (`sig=` or `signature=` with 16 or more characters of
   URL-safe base64); `Basic <token>` when the token decodes to printable `user:password` (so "basic information" stays); `Cookie` and `Set-Cookie`
   lines that hold a pair with a value of 8 or more characters; payment card numbers (13 to 19 digits, alone or in groups, a known issuer prefix and
   length for Visa, Mastercard, American Express, Discover, JCB, Diners Club and UnionPay, and a valid check digit, so that a timestamp or a tracking
   number that merely passes the check digit stays); the prefixes `hf_`, `api_org_`, `whsec_` and `xapp-`; the names `AccountKey`, `SharedAccessKey` and
   `secret_access_key`; and "name is value" in prose (`the admin password is ...`) when the value has a digit or a symbol and is not a word like
   "required". Variable names are now matched behind other words (`DB_PASSWORD`, `STRIPE_SECRET_KEY`, `AWS_SECRET_ACCESS_KEY`): the `\b` of the old pattern
   does not allow a name after an underscore, so a pasted `.env` file was not redacted by name (found by this work's own probes, not by the review).
   A secret written with characters that only look like ASCII is found by also reading the text the way a person does: fullwidth, circled and
   mathematical letters, a small table of Cyrillic and Greek look-alikes, no-break and other spaces and hyphen variants become their ASCII
   forms and the characters that are invisible inside a word (soft hyphen, joiners, variation selectors) are dropped. That reading is kept only when it shows a
   secret the plain pass could not: a string without a hidden secret is returned byte for byte in any script, a string with one is returned folded.
   A redacted key that ends its line takes the following lines that hold nothing but a credential-shaped word, the rest of a wrapped key. The table is
   applied until it finds nothing more, so the result is a fixed point (a property run of 300,000 random strings found one counterexample while the
   table was written; it is a regression test). Evidence: `internal/ai/redact_shapes_test.go` (26 shape cases, 14 for the review's classes and 12 own
   probes; near-miss lists for the Basic, prose-password, cookie, `sig=`, card-number, variable-name, prefix and wrapped-key rules; the fixed-point property; `FuzzRedactText`, a 90 s run of 209,433 executions with no failure) and 25 mutations of
   the new code, each failed by the test that names it; inputs of about 1 MB built against the rules run in 0.2 to 1.7 s. **Not covered:** the Unicode
   confusables list (the table is short on purpose), a precomposed accent, other scripts, a password in prose that is a plain word ("the password is
   correcthorse"), a key that a program broke in the middle of a paragraph (the tail shares its line with prose). Refusing a request that still contains a
   secret-shaped token, the review's third suggestion, is a product decision and is not made here. The filter stays mechanical, not a guarantee (§15).

7. **A creation whose bearer token ends before its run can finish is refused at admission (the M14R-03 residual, 2026-10-29; extends A8).** The result
   exchange authenticates again (A8), so a token that lapsed during the provider phase lost a paid answer, left the proposal PENDING and, when the sweep ran,
   charged its whole reservation (about 6.7% of the full-length requests with a 15-minute token and a 60 s phase). `identity.Verifier.VerifyRemaining` verifies
   exactly as `Verify` does and also returns how long the token stays valid by the verifier's own clock, without the skew that `Verify` tolerates;
   `aiPropose` asks an `identity.Expiring` authenticator for it before it reserves anything and answers `401 token_expiring` (an audited
   `ai.propose.rejected`, with `WWW-Authenticate`) when less than the effective wall clock plus the result margin (10 s) is left. Nothing is reserved, charged or
   sent to the provider; the client signs in again and retries. The authentication policy is unchanged: the result exchange still verifies the token and still
   refuses an expired one, and an authenticator that cannot say how long its token lasts is not asked. Evidence:
   `TestM14R03ATokenThatEndsBeforeTheRunCanFinishIsRefusedBeforeAnythingIsSpent` (tokens with 5, 25 and 36 s left against a run of 40 s: three 401, no provider call,
   no token used, no proposal row, three rejection audit rows; a token with 120 s left is served), which fails without the check (the 5 s token is accepted and
   reaches the provider), and `TestVerifyRemainingReportsTheRestOfTheLifetime`. A client that treats every 401 as "sign out" will ask for a new sign-in up to
   70 seconds early; there is no release-AI user interface in this build that does.

8. **The redactor after the red team (RT-01 to RT-05 and RT-08, 2026-11-03; extends item 6).** The independent red team of the redactor
   ([report](../verification/REDACTION_REDTEAM.md), pinned at `11a4ee4`; FAIL for the claim that it catches the accidental pastes of common credentials: five
   MEDIUM, four LOW) was reproduced with values derived at run time. The assignment rule now reads a key that is quoted or subscripted
   (`"password": "..."`, `os.environ["DB_PASSWORD"] = "..."`, `define('DB_PASSWORD', '...')`, RT-01), a name that ends a longer word (`secret_key_base`,
   `client-secret`, a camel-cased `dbPassword`, RT-02), a name followed by neither `=` nor `:` in the comma form of a call (`name', 'value`, RT-01; the connector
   `the admin password is ...` was in the table before, and RT-05 limited it), and keeps a sentence that says what a credential is instead of being one ("The
   token is 64-character hex.", "HS256-signed", RT-05; **the first form of that exemption, and of the password rule below, were wrong in two ways that the independent
   review of the fixes found: item 15 a and b**). A key that a line break cut
   before its first part was long enough for any rule is read across up to three following key-shaped lines (RT-03). The `api_key` rule names ten more vendor
   families (Google OAuth client secrets, Docker Hub tokens, Shopify, HashiCorp Vault, GitLab, Square, New Relic, Telegram bot tokens, Discord bot tokens and
   Azure AD client secrets, RT-04), and a password given as an argument or a quoted literal after the word is redacted (`curl -u user:SECRET`, `mysql -pSECRET`,
   `sshpass -p`, `docker login -p`, `redis-cli -a`, `IDENTIFIED BY`, a `.netrc` `password`, SQL `PASSWORD '...'`). Evidence: `redact_shapes_test.go`,
   `redact_surfaces_test.go` and the near-miss lists they hold. **Not fixed, documented:** RT-06 (a string shaped like an identifier skips the generic rules
   on purpose: ids and references are structure, the prefix rules still apply), RT-07 (hex glued to a word character), RT-09 (delimiters inside the secret).
   The filter stays mechanical, not a guarantee (§15).

9. **The card rule looks at every window (PF-09, 2026-11-03; extends item 6).** The card rule was one regular expression that takes the longest match from the
   left and a check that refuses it, so "Order 99871 4111 ..." or "... 1111 12/27" (the card after or before another number) returned unchanged. It is now a
   scan over the chains of digit groups (digits, one space or hyphen between groups): from every group that a word boundary precedes, the windows of 13 to 19
   digits that end at a group are tried, the longest first, and one with a card network prefix, length and check digit is found. Definition and
   boundaries are those of the old rule; the windows it hid are found. Windows that overlap are one span and are replaced as a whole, so that no digit of any
   valid window stays (the first form replaced the first window found and consumed it, which left the other groups of a real card visible when a window of
   chance overlapped it: item 15 c). Random digits pass both checks about once in 38 windows of 16 digits, once in 374 of 14 and about once in 100 across the lengths
   (measured by the independent review over 2,000,000 windows per length), so a long list of short numbers separated by single spaces or hyphens can lose a number to
   the marker (accepted: the cheaper error for a safety net; item 16 d has the loss measured: about a tenth of a long list, most of a counter). Evidence: `redact_cards_test.go` (the review's five inputs and seven more; the old rule on the same
   inputs; numbers that are not cards; 10,000 random texts against the exact output of an independent definition; the number of four groups before a card; linear
   time on chains of 100,000 groups) and the negative controls with the old rule and with the first-window loop behind it.

10. **Asking for a proposal and reading one need `metadata.read` (AO-02, 2026-11-03; extends A6).** A6 composed the writes (`ai.apply` needs `metadata.write`,
    confirm needs `requirements.decide`) and nothing composed the reads, so a stored role that held only `ai.read` read the full definition of every definition
    a proposal changes (403 on the metadata routes, 200 here) and a role with `ai.read` and `ai.propose` had the whole draft sent to the operator's provider and
    returned again in the READY proposal. The two creation routes and the read of one proposal now also require `metadata.read`, evaluated before anything is
    reserved or sent; a refusal is audited as `ai.propose.rejected` or `ai.read.rejected` with the decision id. The list holds hashes and states only and keeps
    `ai.read` alone. A veto still needs only `ai.apply` and now answers with the summary of the proposal (`Store.Summary`), not its body; so do apply and confirm,
    and the header of the new revision or of the new requirement set replaces the document and the set (item 15 d). The built-in roles hold
    `metadata.read` and are unchanged; a custom role built for the AI builder must be granted it. Evidence: `TestM14APIPolicyMatrix` (the list and the body are
    separate columns) and `TestM14APIAskingAndReadingNeedMetadataRead`, both failing without the composition.

11. **A refused creation stops at the cap (PF-11, 2026-11-03; extends A5).** `BuildContext` redacted every string of every editable definition before it checked the
    64 KiB limit; a draft may hold 256 KiB, and a refusal (`422 ai_context_too_large`, nothing reserved or charged) cost four times the work of an accepted
    creation inside the admission slot and the transaction. It now returns `ErrContextTooLarge` as soon as the redacted bodies pass the cap, so a refusal costs what a
    context that just fits costs. **Not done:** no limit on how often a member may ask (a refusal charges nothing, so neither the in-flight limit nor the daily budget
    fires, and the traffic gate allows 300 requests a second per peer), and `Prepare` still runs inside the transaction. A per-member limit on refused
    creations, or moving `Prepare` out of the transaction, would close it.

12. **The result exchange waits for a full admission queue (ER-04, 2026-11-03; extends A8 and item 4).** The exchange that stores a paid answer takes a slot of the
    same 4 + 128 admission as every other request and was repeated only after a known abort; with the queue full it ended in a bare 503, the proposal stayed
    PENDING and the sweep charged the whole reservation (8 of 8 answers lost under a burst of reads in the edge rehearsal). It is now repeated after a full queue too,
    with waits of 100 ms to 1.5 s, each spread by a quarter either way so that the answers that waited for the same moment do not come back at the same moment
    (`postgres.RetryPaced`, `postgres.Jittered`), inside the same 6 s budget and the 8 s context. With a full queue every try fails at once, so the six tries end after
    about three seconds of the six; a burst that lasts longer loses the answer as before (a bare 503, no audit). **Not tested** at the burst size of the rehearsal (more than
    132 database requests at one moment); the mechanism is tested against a real `Admission` that drains after 250 ms.

13. **A stopping API waits for its paid phases (ER-03, 2026-11-03; extends A5).** The API gave its running requests 10 s after SIGTERM; a creation holds its request up
    to 70 s. A routine restart abandoned every running phase, left the proposals PENDING, charged each its whole reservation (about 31% of the default daily budget per
    request) and held the tenant's slot for 80 s. `Config.ShutdownTimeout` is 10 s, 80 s when `AI_PROVIDER_FILE` is set, and `SHUTDOWN_TIMEOUT` (1 s to 5 min; a duration with a unit, `30s`: a bare `30` is refused, by the API, the worker and the dispatcher alike, because `config.Load`
    reads it for all three and only the API uses it) overrides both. A creation that still waits in the admission queue when the signal arrives and starts its provider phase after it
    can need more than the 80 s and is then cut by `Close` (by reading, not run). **Deployment constraints that follow** (edge rehearsal ER-01 to ER-03, [report](../verification/EDGE_REHEARSAL.md)): the termination grace of the
    orchestrator must be longer than the value in force; a proxy in front of the API needs a response and idle timeout of at least 75 s (90 s recommended) on the two
    creation routes, because a cut at T costs a FAILED/INTERNAL row charged 22.6% of the reservation or a READY proposal the client never saw; nothing may repeat
    a `POST .../patch-proposals` or `.../requirement-proposals` automatically (each repetition is a second paid run: the creations are not idempotent); all replicas need
    the same `in_flight_per_tenant` and `daily_token_budget` (the limits are arguments of the database port, not database state).

14. **A token that no longer verifies at the pre-admission check is refused (PF-03, 2026-11-03; extends item 7).** `aiTokenOutlivesTheRun` let a request through when the
    second verification of the token failed (a token that verified at the front door and ended in between); it is now answered `401 unauthenticated` at once, as any
    other would be, because letting it through pays for an answer the result exchange cannot deliver. Evidence: `TestTokenCheckBeforeAdmission`.

15. **Corrections after the independent review of items 8 to 14 ([report](../verification/TAKEOVER_FIXES_INDEPENDENT_REVIEW.md), pinned at `0155f7b`, 2026-11-03;
    PASS WITH FINDINGS: no HIGH, five MEDIUM, seven LOW).** The reviewer did not write the fixes and changed no code; its probes were deleted. Each finding below was
    reproduced here before it was changed, and each change has a test that fails without it. The changes are the integrator's and are not re-verified by a reviewer.

    a. **TF-01 (MEDIUM): the password rule took words out of ordinary requests.** The rule of item 8 (`password_arg`) redacted any word of four letters after `identified by`
    ("Each customer is identified by email address."), after `machine ... password` ("The machine learning model validates the password strength"), any quoted word after
    `password` (`Show a password "hint" field`) and the value after a `-p` of `zip`, `redis-cli` or `mysql` ("zip codes with -plus4", `redis-cli -p 6379`, `mysql -P3306`,
    `docker login --password-stdin`); the request the model read lost its meaning, and a definition whose label held such words could not be updated by the AI builder (the
    placeholder is refused when written back). Every form now needs the command or the statement around it. The flag of each command is its own (`-p` is the port of
    redis-cli, `-P` that of mysql; the case of a flag counts). A `.netrc` entry needs `machine HOST login USER password`, a host with a dot, or `default login USER password` at the start of a line ("the default login admin password rotation" is a sentence).
    The SQL forms are read as a statement; the words of the statement stay and the clause with the password is replaced. **The first form of that scan** (a statement of at most
    1,000 bytes, a closed list of option words before `PASSWORD`) **let passwords through that the rule before it redacted, and the second review replaced it (FU-01): item 16 a
    describes the scan as it is.** Added in the same pass:
    `curl -uUSER:PASSWORD` without a space, `redis-cli --pass`, `az login`, `gpg --passphrase`, `ldapsearch -w`, a `.netrc` on several lines. **Residuals:** a command that
    stands in prose is read as a command (`curl -u username:password`, `sshpass -p option`, `mongo -pretty`; item 16 b lists the eight that remain), and the value of a
    command-line password ends at a `;`. Evidence: `redact_prose_test.go` (37 requests and commands that stay as written at `f33a0b5`, not the 35 that this item first said, and 24
    command and statement forms that are redacted), the idempotence property run on 300,000 texts with the new fragments (a one-off run through `REDACT_PROPERTY_RUNS`; the shipped test
    runs 6,000), and a cost probe: the second review measured 3.0 to 4.9 microseconds a byte over 50 shapes, linear, the slowest being `mysql -p<4 characters>` repeated and `sshpass -p`
    (the shape that this item named, `create user x identified by '...'`, cost 3.0 to 3.7); the near-miss tests fail on the first form of the rule.

    b. **TF-04 (MEDIUM): the exemption for sentences about credential formats kept passphrases.** `^(?:[A-Za-z0-9]+-)+[a-z]{3,}$` is also the shape of a passphrase: "the password
    is" followed by a word, a hyphen and a lower-case word was kept, as was the same value in `define('DB_PASSWORD', '...')`. The exemption is now a closed list: a size and its
    unit (`64-character`, `256-bit`, `15-minute`) or a named algorithm, standard or protocol with its size or a plain participle (`SHA-256`, `RSA-2048`, `HS256-signed`,
    `OAuth2-compliant`, `2FA-verified`, `RFC-7519`). A value spelled like one of them stays kept; any other value with a digit or a symbol is redacted. The price the red team
    named is paid in the other direction too: values such as `Summer-2026` are redacted, as before.

    c. **TF-06 (LOW): a window of chance could leave twelve digits of a card.** See item 9: 19,478 texts of a 16-digit number in four groups followed by a card in four groups, and
    in 7.5% of them the first window found (one that began in the number and ended in the first group of the card) was redacted and consumed, and up to 12 of the 16 digits of the
    card stayed. Windows that overlap are now one span. The test compares the scan with the exact text and count of an independent definition (`redactedByDefinition`), not with
    "no window is left" (TF-12), and asserts the neighbours of the case that had an empty one.

    d. **TF-02 (MEDIUM): AO-02 was closed for the creations, the read of one proposal and the veto, not for apply and confirm.** The answer to an apply held the proposal with its
    operations and the revision with its document, the answer to a confirm the proposal and the whole stored requirement set, to a role with `ai.apply` and `metadata.write` (or
    `requirements.decide`) and without `metadata.read`. They now hold the summary of the proposal (`Store.Summary`), the header of the new revision (no document) and the header of
    the new requirement set (no body, `aiRequirementsHeader`); `Recorded` stays: the ids, requiredness, status and provenance of the decisions the caller made, and `category`, a token of at most 64 characters that **the model chose** and the caller did not send (FU-06: this item first called it a decision of the caller). The caller reads the result where it
    may. Evidence: `TestM14APIPolicyMatrix` asserts the bodies of the apply and the confirm of the two roles; with the three changes undone it fails ("holds "operations"", run on
    the database).

    e. **TF-07 (LOW): a lone surrogate escape was stored as U+FFFD, and the provider adapter substituted.** The check for invalid UTF-8 (TS-04) does not see `\ud800`, which is ASCII,
    and `parseEnvelope` decoded a payload "byte for byte" with a decoder that substitutes. `tools.HasLoneSurrogateEscape` finds the escape of half of a pair that the other half does
    not complete (checked against the decoder on 20,000 random strings, both sides); it is applied to the answers of a tool, to the catalog file, to the envelope of the provider
    (which now also refuses invalid UTF-8) and to every proposal payload (`strictTree`). A pair and the escape of U+FFFD itself are text and stay. **Not changed in that pass, changed after the second review
    (item 16 e, FU-05):** the general metadata decoder (`metadata.DecodeJSON`) turned such an escape into U+FFFD in a document an author sends and in the AI request body (it refused invalid bytes).

    f. **TF-08 (LOW): the OpenTelemetry exporters wrote the raw value of a malformed `OTEL_EXPORTER_OTLP_*` variable to standard error,** the sibling of LE-02 (`parse headers` with the
    whole header list, `parse url` with the credentials inside the endpoint, at start-up, from the operator's own setting). `observability.Start` replaces the library's logger before
    the first exporter is made; the log holds one fixed warning at most every ten seconds. Evidence: a child-process test (without `Start` the value is on standard error, with `Start`
    nothing is) that fails without the call. The call covered the exporters; the error handler, and so the sampler and the resource attributes, was left for item 16 c (FU-03).

    g. **TF-09 (LOW): forms next to the closed ones.** Fixed, with tests: the hash rocket (`'password' => '...'`, `:password => "..."`), the short declaration (`dbPassword := "..."`) and a
    comparison with a literal (`password == "..."`); a bare word on their right is a name (`password == confirmPassword`) and stays. **Still open, documented:** escaped quotes
    (`password=\"...\"`), a quoted JSON value of 7 characters or of 14 letters (the `:` form needs 8 characters and a digit or a symbol), a hard-wrapped key whose continuation lines
    carry a quote prefix, a hard-wrapped key over five lines, and a continuation followed by prose (RT-03, RT-06, RT-07 and RT-09 are listed above).

    h. **TF-10, TF-11 (LOW).** The comment on the chance rate said one window in two hundred (it is one in 38 for 16 digits and about one in 100 across the lengths); item 12 pointed at
    item 3 (item 4 is the repetition) and item 8 attributed the `is` connector to RT-08 (it was in the table before): corrected. The comment of `operator_env_test.go` said AGENTS.md names
    the six credentials (it does not; the launcher does): corrected. The pauses of ER-04 are jittered (item 12). `scripts/m6r-env.mjs` and `scripts/m8-env.mjs` set the mode of `.env`
    before they append credentials, not after (IS-03). `scripts/bootstrap.sh` removes `.local/toolchains/go` before it unpacks a toolchain, so two releases are never mixed (the block runs only when `scripts/go.sh version` fails, so a
    local toolchain that runs and is older is not replaced); the message that says a SHUTDOWN_TIMEOUT needs a unit is in `internal/config/config.go`, not in the script (FU-07).

    i. **TF-12, TF-11 (tests).** `TestM14APIPolicyMatrix` asserts the bodies (d). TS-01 has a reproduction that the next owner can run: `TestTS01ACancelledInstanceLeavesNoInvocationThatCannotBeSettled`
    (`tests/integration/m10_ts01_test.go`) states the invariant that the fix must give and FAILS today, so it runs only with `FRAMEWORK_REPRODUCE_TS01=1`. `TestBuildContextRefusalCostsNoMoreThanAContextThatFits`
    is a ratio test (threshold 3, measured 1.1 on a quiet host and 1.7 to 2.0 on a loaded one) and can flake on a host that is heavier still.

16. **Corrections after the second independent review ([report](../verification/TAKEOVER_FOLLOWUP_INDEPENDENT_REVIEW.md), pinned at `f33a0b5`, 2026-11-04; PASS WITH
    FINDINGS: no HIGH, one MEDIUM, eight LOW).** The reviewer did not write the changes under review and changed no product code; its probes were deleted. Each finding was
    reproduced here before it was changed, and each change has a test that fails without it (the negative controls are named below). The changes are the integrator's and are
    not re-verified by a reviewer.

    a. **FU-01 (MEDIUM): the statement scan of item 15 a let through passwords that the rule before it redacted.** It read SQL through a closed list of the words that may stand
    before `PASSWORD` (`WITH LOGIN CONNECTION LIMIT 5`, `IN ROLE`, `VALID UNTIL`, `ROLE`, `ADMIN` and `SYSID` left the password in: 35 of the reviewer's 100 role statements), had no
    `CREATE OR REPLACE USER` (MariaDB), ended a statement at the first `;` even inside a quoted password (`PASSWORD 'ab;<S>'`) and read at most 1,000 bytes (a `CREATE USER` of 60
    users showed the secrets of 34; a cut inside a secret left its tail next to the marker). The scan is rewritten. A statement runs from its head (`CREATE [OR REPLACE]
    USER|ROLE|GROUP|LOGIN`, `ALTER` of the same, `GRANT`) to the first `;` that is outside a quoted literal (single, double and back quotes, `$$`; inside a quote a backslash escapes the
    next character, as MySQL reads it) or to the end of the text, so whatever stands between the name and the password, whatever the length, nothing is cut. The clauses it reads are
    `IDENTIFIED [WITH plugin] BY value` after the first `CREATE` or `ALTER` head or after the target (`ON ... TO`) of a `GRANT`, and `PASSWORD value` (a quoted literal, `E'...'`,
    `$$...$$`) after the first `CREATE` or `ALTER` head; the words of the statement stay, and each clause is replaced by one marker, in the order of the text. A quoted literal is a
    password when it ends the statement or a closing quote follows it (`psql -c "ALTER USER app PASSWORD 'x'"`, a JSON value, a code span), when a `;` ended the statement, when the
    head of the statement and the first word of its clause are in capitals (the SQL of a script, a dump or a manual: a sentence does not shout `CREATE USER app IDENTIFIED BY`), or when it
    has a digit or a symbol (in "Create user form with password 'confirm' field" it is a label); a bare word when it has a digit or a symbol or when it ends a statement that a `;` ended
    (Oracle writes `IDENTIFIED BY tiger;`); capitals do not decide a bare word (`IDENTIFIED BY RANDOM PASSWORD` has no password in it). Evidence:
    `TestRedactionOfRoleStatementsWhateverStandsBeforeThePassword` (2,340 statements, 9 verbs x 13 option lists x 4 literal forms x 5 endings, each also a fixed point),
    `TestRedactionOfAStatementOfManyUsersAndOfAnyLength` (3, 60 and 700 users), `TestRedactionOfASecretThatHoldsASemicolonOrAQuote` (nine shapes, among them a backslash-escaped
    quote and a quote that nothing closes), the tests of a bare word and of a quoted word of letters, `TestRedactionCountsAClauseOnlyWhereItIsAPassword`,
    `TestRedactStatementMakesItsEditsInTheOrderOfTheText`, `TestSQLStatementEnd`, the idempotence property with the SQL and `.netrc` fragments added (200,000 texts before the cost
    fix described below and 100,000 after it; the shipped test runs 6,000 by default and `REDACT_PROPERTY_RUNS` lengthens it), and **30 mutants of `redact.go`**, each killed by a test
    (a statement end that ignores quotes, backslashes or `$$`; a bare word that is always or never a password; a quoted word that is always or never one; a `GRANT` clause counted from
    the head of the `GRANT`; a `PASSWORD` literal counted anywhere in the statement; edits made in the order they were found; the heads inside a statement scanned again; blanks that
    hide the end; the plugin form; `OR REPLACE`; the scan removed; a closing quote or capitals that do not count, capitals of the head or of the keyword alone, capitals that decide a bare
    word; every `GRANT` looked at). Nine of them were not detected by the first form of the tests that were meant to kill them (the `GRANT` and `PASSWORD` mutants, a `;` that does not make a
    quoted word a password, the unsorted edits, the heads scanned again, which only the property test caught, the closing quote, the capitals of the keyword and every `GRANT` looked at,
    which a run without the linear test missed); each has its own test now.
    **A defect of my own first form of this scan was found by a cost probe before anything was pushed.** It looked for the target of every `GRANT` of a statement, each look reading
    the rest of the statement, so the time grew with the square of the length: `grant ` repeated 8,000 times (48,000 bytes) took 10.9 s (2,000 times: 0.4 s), and `grant on ` and
    `grant a on b ` 8,000 times took more than 20 s. Only the first `GRANT` is looked at now (a later one has a target only where the first one has). Twenty-one shapes from 12 KB to
    1.4 MB then cost time in proportion to their size (four times the size, three to five times the time; the slowest 3.7 microseconds a byte, the largest text 2.4 s), and
    `TestStatementScanIsLinear` fails on the old form (11.6 s against 91 ms for prose of the same size; its limit is 50 times the cost of prose measured in the same run, so it holds
    with `-race`, which the unit gate uses, and on a busy host). **Three losses of my first form were found by comparing it with `f33a0b5` before the gate, not by the review.** (1) A quoted password of letters only was a label unless the clause ended
    the text or a `;` ended the statement, so `psql -c "ALTER USER postgres PASSWORD 'changeme'"`, a JSON value, a list of arguments, a call of code and a code span with such a password
    were not redacted, which `f33a0b5` redacted (nine shapes tried; the closing quote above is the answer). The comparison was then made systematic: 77,760
    generated contexts (eight verbs, six clauses, five literal forms, nine endings, four wrappers, three kinds of secret, three cases) through both redactors, and the operator and
    `.netrc` families. It found (2) the same loss in SQL in capitals that has text after the literal (the capitals above are the answer) and (3) the operators: `'password' => 'secret'` and
    `dbPassword := "changeme"` were no longer redacted (item 16 b says what was done). **What `f33a0b5` redacted and this tree does not:** a quoted password of letters only in SQL that is in lower case or mixed case and has neither a `;` nor a
    closing quote after the literal (`create user app identified by 'changeme'.`, followed by a full stop, a word or a line; it is the same text as the label `Create user accounts
    identified by 'email'.`), a bare word of letters followed by a closing quote (`mysql -e "CREATE USER app IDENTIFIED BY changeme"`; `f33a0b5` redacted it by accident), a quoted word of
    letters after `==` (`if token == 'changeme'`, item 16 b), and the `.netrc` forms that a sentence can imitate when the password is a word of letters
    (`default login anonymous password changeme`, `machine host.example.com password changeme`, item 16 b). Everything else that the 77,760 contexts and the operator family
    differ in is a gain (1,448 groups of contexts that `f33a0b5` left in and this tree redacts: a password with a digit or a symbol in a statement without `;`, `$$` and
    double quotes, `OR REPLACE`, a bulk statement, a statement longer than 1,000 bytes). **Not fixed**, open before and after this change in the review's three tables, or found by a
    probe of mine:
    `SET PASSWORD FOR 'u'@'h' = PASSWORD('...')` and `= '...'`, the old password after `REPLACE` (MySQL, Oracle), `IDENTIFIED BY VALUES '...'`, MariaDB `IDENTIFIED VIA plugin USING
    PASSWORD('...')`, T-SQL `WITH PASSWORD = N'...'` (the `N` prefix), `sp_addlogin` and `sp_password`, `net user NAME PASSWORD`, and of the commands a `curl` whose `-u USER:PASSWORD` is
    on the line after a backslash, `--proxy-user`, `unzip -P`, `7z -p`, `sqlcmd -P`, `htpasswd -b`, `net use`, `openssl -passin pass:` and `SSHPASS=` with `sshpass -e`. A bare word of
    letters after `IDENTIFIED BY` in text that no `;` ends stays (a sentence), and so does a quoted word of letters in lower case or mixed case SQL that neither ends the statement, nor is
    followed by a `;` or by a closing quote.

    b. **FU-02 (LOW): the narrower rules still destroyed ordinary text, two classes of it new against both older tables.** The `.netrc` forms that a sentence can imitate need a value
    that looks like a credential: `default login USER password WORD` at the start of a line (seven of the review's lines began so; the follow-up had fixed the sentence of its message
    and left the line start) and a host with a dot and no login ("Each machine web01.corp.example.com password rotates every 30 days") need a quoted value or one with a digit or a
    symbol; the full form `machine HOST login USER password WORD` keeps any word. A comparison (`==`) redacts a quoted literal that has a digit or a
    symbol and nothing else (`if password == "admin" then ...` and `Warn when token == "expired"` are conditions on a state). A declaration (`:=`) and a hash rocket (`=>`) redact any quoted
    literal, as the first review asked (TF-09): `'password' => 'secret'` and `dbPassword := "changeme"` are how configuration and code hold a password, and the review's two sentences for them
    (`Set password := "required" for new users.`, `The password => "hidden" mapping in the form.`) are pseudo-code in prose, destroyed again. The first form of this change took the
    review's direction for all three and lost real passwords; the comparison with `f33a0b5` showed it. The list of formats grew by what
    the review named (`x509`, `ed25519`, `ed448`, `curve25519`, `-or-more`, `-or-fewer`, `-times`, `-uses`, `-bearer`, `HMAC-SHA256`, `TLS1.3`, a size with a decimal). Of the 43 sentences
    that the review lists as destroyed and kept by `11a4ee4`, 30 are kept now and 13 are not; of its nine format sentences (the review names shapes, not always the sentence), eight are
    kept. **What stays destroyed, by design or for want of a rule:** eight commands that stand in prose and cannot be told from the command (`curl -u username:password`, `curl --user
    USER:PASSWORD`, `mysql ... -pretty`, `mongo -pretty`, `mongosh -pretty`, `docker login -password-stdin`, `sshpass -p option`, `wget --password option`); `Create login screen with
    password "show/hide" icon` (a quoted literal with a symbol after `CREATE LOGIN`); `Create user records identified by 4471 and name` (a bare word with a digit); `Show the machine
    api.example.com login bob password rotation date` (the full `.netrc` form); the two pseudo-code sentences of the operators above; and `The token is v4.public.` (a value with a digit that
    is not on the closed list). **The price, measured against `f33a0b5`:** a password of letters after `==` in quotes, and on a `default login USER password WORD` line or a `.netrc` entry
    without a login, is not redacted (a word of letters there is a word, as it has been after `:` and `is` since RT-01 and RT-02); the other direction destroys the sentences above. Evidence:
    `redact_prose_test.go` (68 requests, labels and descriptions that stay as written, against 37 at `f33a0b5`; 25 sentences about formats), and the mutants of item 16 a for
    this change (the loose `.netrc` rule removed or without its shape check, the comparison without its rule, a declaration or a hash rocket with or without one, names taken off the list)
    are killed.

    c. **FU-03 (LOW): TF-08 was closed for the OTLP variables and open for the sampler and the resource.** `otel.SetErrorHandler` ran after the tracer provider was built, and until it
    runs the SDK reports a malformed `OTEL_TRACES_SAMPLER` (`unsupported sampler: <value>`) and a malformed `OTEL_RESOURCE_ATTRIBUTES` entry (`partial resource: missing value: [<entry>]`)
    through `log.Print`, to standard error, with the value. `observability.Start` installs the handler first now, next to the logger. Evidence: `TestOtelLibrariesLogOnlyThroughSlog` sets
    both variables, with the two it had, in the child process and checks that neither the values nor the library's words reach standard output or error, that standard error is empty
    and that the warning is one; its control (without `Start`) still shows the raw value on standard error; with the handler moved back after the providers the test fails ("reaches the
    process output"). **Not fixed, and not reachable from `Start`:** the `OTEL_*_LIMIT` variables of the span limits are read by `go.opentelemetry.io/auto/sdk` in a package initializer,
    before `main`, and a malformed one is written by `slog.Warn` on the default logger with its value; every binary that imports `go.opentelemetry.io/otel` has that initializer. The
    documented launcher (`scripts/runtime-env.mjs`) hands its children only `OTEL_EXPORTER_OTLP_ENDPOINT` of the `OTEL_*` variables, so these reach a process only when it is started
    without the launcher; LOW (operator-supplied, start-up only, not reachable by a caller).

    d. **FU-04 (LOW): the card union loses more of a numeric list than item 9 says.** Item 9 says that a long list of short numbers "can lose a number to the marker". Measured by the
    review on random chains of four-digit groups separated by single spaces: 4.0% of the groups at five groups and 9.7 to 9.8% from 50 to 1,000 (the worst chain lost 152 of 1,000 groups in
    38 markers); counters lose most of theirs, because a window that begins with 4 or 5 passes the Luhn check far more often than chance does (`4000 4001 ... 4399`: 284 of 400 groups, 71%;
    `5100 ...`: 70%; 3,000 counters from 1000: 20%); a chain of 400 groups built so that every window from the fifth on is valid collapses into two markers. The chance rates of
    item 9 hold within sampling error (the review's exact figures: one valid window in 38.7 for 16 digits, one in 385 for 14, about one in 100 on average). No secret is exposed; a pasted table of numbers loses about a tenth of its
    cells. **Not changed:** the cheaper error for a safety net, as item 9 says, now with the figures. Merging only the windows that overlap by at least one group, with a cap on the span,
    would lose less and is not done.

    e. **FU-05 (LOW): the other decoders of author text substituted U+FFFD silently.** `metadata.decodeJSON` (behind `DecodeJSON` and `DecodeJSONBounded`) refuses the escape of half of a
    surrogate pair beside its refusal of bytes that are not UTF-8, so the documents an author sends, previews, promotion, dataset admission and the AI request and decision bodies (`aiBody`)
    inherit it. Evidence: `TestDecodeJSONRefusesALoneSurrogateEscape` (seven shapes refused; a pair, U+FFFD itself, an escaped backslash that is followed by the letters of an escape and other
    escapes accepted) and `TestAIBodyRefusesALoneSurrogateEscape` (the review's request, a rename of a page with a lone half in it, is a 400 `invalid_ai_request`); with the check taken out of
    `decodeJSON` both fail. **Still substitutes (the review's table, read in the source and not executed):** the sign-in body (`signin.readJSON`, which also has no check for bytes that are not
    UTF-8), the records and datasets of the end users of an application and the workflow signals (`internal/runtime/data`, `internal/runtime/workflow`), `tools.DecodeInput` for an input that
    does not come through a document, and the operator's AI provider file (`ai/config.go`).

    f. **FU-06 (LOW): what a role without `metadata.read` can still learn from an apply and a confirm.** The review enumerated every member of every body (the views marshalled with every
    field set, and `aiReply` over every refusal): definition ids and fixed validator texts (`conflicts[].target`, `diagnostics[].path`, `sensitive[]` as `<definition id>:<aspect>`, which the
    caller must echo), the hashes of the draft and of the request, and `recorded[].category`, a token of at most 64 characters (`categoryPattern`) that the model chose. The boundary of AO-02
    (definitions, labels, expressions, requirement statements) holds. **Not changed:** the members stay; item 15 d says that `category` is the model's, not the caller's.

    g. **FU-07 (LOW): statements of the documents that the tree did not back.** Corrected: the count of requests that stay as written (37 at `f33a0b5`, not 35; item 15 a), the place of the
    SHUTDOWN_TIMEOUT message (`internal/config/config.go`; item 15 h), the cost claim (item 15 a), `Recorded` (item 15 d), the sentence of the handoff that the OpenTelemetry libraries log
    through slog with one fixed warning (true for the OTLP family and, since item 16 c, for the sampler and the resource; not for the span-limit variables), the claim that every fix has a test
    that fails without it (item 16 h says which ones have), and the residual lists of item 15 a (item 16 a). The review could not check the figures of the gate record and did not claim them;
    the gate of this round replaces them.

    h. **FU-08 (LOW): tests that did not detect everything they were credited with.** Strengthened, each with a negative control that fails without the fix: the chmod before the append and the
    `rm -rf` before the `tar` are tested by their order in `m6r-env.mjs`, `m8-env.mjs` and `bootstrap.sh` (`runtime-env.test.mjs`; with the two lines of each swapped it fails); the message that a
    bare `SHUTDOWN_TIMEOUT` needs a unit (`TestShutdownTimeoutFollowsTheAIBuilder`; with the words removed it fails); RC-04 now generates the neighbours of every registered POST path (a character
    added before or after a segment, its last character taken away, upper case, an encoded slash, a newline, an empty segment, the path cut after the segment, a trailing slash, an extra or a
    doubled segment: at least 150) and compares by that, so an extra alternative such as `proposals-x` is found (`TestAIPOSTAllowlistAcceptsExactlyTheRegisteredPOSTRoutes`; with the alternative
    added it fails); the catalog case "a half in the member name" says that `strictKeys` refuses it as well, so that only the two value cases need the new check. **Not changed:**
    `TestRedactionOfAStatementKeepsItsName` guards the design of the scan, not TF-01 (it passes against the rule it replaced).

    i. **FU-09 (LOW): the TS-01 reproduction could not pass under a fix that refuses the cancel.** `m.signal` fails the test on any status but 200, so a fix that answers the cancel 409 (the
    instance waits for its outstanding outcome, nothing is stranded) failed it although the invariant held. The cancel is sent with `m5Call` now: a 409 is a pass and any other status but 200
    fails with its body. Executed once against the unfixed tree on the isolated stack (`FRAMEWORK_REPRODUCE_TS01=1`): it fails as before, with the message that the plan quotes (`TS-01: 1
    invocation(s) of a CANCELLED instance stay SENDING or UNKNOWN and the reconcile is answered 409 ...`); not executed against a fix, because none exists.

**Residuals after Amendment C, stated plainly.**

- A bearer token that expires during the provider phase lost the result: the result exchange re-verifies it (A8). About 6.7% of the full-length requests with a
  15-minute token and a 60 s phase (60/900). Closed on 29 October by item 7: such a request is refused at admission, before anything is spent.
- `control.ai_usage` is still one row per tenant and day that every admission and every result updates under REPEATABLE READ. The retries make the
  contention survivable, not absent: a burst of 24 simultaneous creations still saw 4 `409 ai_conflict` at admission after 0.3 s (nothing stored,
  nothing charged). An insert-only ledger summed by the budget check would remove the row; it is not done.
- A retry budget that ends in the middle of a try answers 503 `service_unavailable` without the audited 409; an abort outside `Admit` in the first exchange
  is now a 409 after the tries (it was a 503).
- M14R-05 (a narrow exemption band between the probe amounts passed `verifier-v2`) is fixed by `verifier-v3` (ADR102 amendment 13) and M14R-08 (10 of 28
  secret shapes survived, B.4) by item 6 above, both after the third independent review and neither re-verified by a reviewer; the verifier stays a
  mechanical floor and the redactor a mechanical filter.
- Found by the AI-output red team and not fixed ([report](../verification/AI_OUTPUT_REDTEAM.md), all LOW): AO-01 (`holdsRedaction` refuses only the exact bytes
  `[REDACTED:`, so a differently cased, spaced or invisible-character spelling of the placeholder can be written into a label), AO-03 (a NEEDS_HUMAN proposal reports
  `derived.sensitive: []` and `aspects: []` for changes that were never classified), AO-04 (326 invisible or format characters still pass the rule for model text, the
  supplement variation selectors among them) and AO-05 (the model's free text is stored and returned to every `ai.read` holder unredacted; the document states that output
  is not filtered, this names the consequence).
- Items 8 to 14 above were reviewed by the independent review of the takeover fixes (`0155f7b`); its findings are item 15, fixed by the integrator and not re-verified by a
  reviewer. Item 15 and everything else that followed it were reviewed by the second independent review (`f33a0b5`); its findings are item 16, fixed by the integrator and not
  re-verified by a reviewer. Item 6 and 7 before them were reviewed in the third pass of the merge review. The red-team reports judge the tree they were pinned to (`11a4ee4`).
- The redactor stays a mechanical filter with closed lists: the forms that item 16 a lists as not fixed leave a password in, and the sentences that item 16 b lists as destroyed lose
  their words. Each review of it has found both kinds; none of its claims is that it is complete.
