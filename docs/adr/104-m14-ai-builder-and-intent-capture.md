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
   probes; near-miss lists for every rule; the fixed-point property; `FuzzRedactText`, a 90 s run of 209,433 executions with no failure) and 25 mutations of
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
