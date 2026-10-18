# ADR040: M14 AI Builder and Intent Capture — proposals only, never authority

Status: accepted for implementation on `claude/m14-ai-builder`.
Date: 2026-10-18. Scope: M14 model gateway, semantic context, application-patch and
requirement proposals, bounded compiler repair loop, human-gated apply/confirm.
M14 is a **development/test preview capability**. It delivers the machinery that
makes model output safe to consume; it does **not** claim any property of a real
language model (see §15). No production data plane exists (ADR039 §10).

## Authorization

On 2026-10-18 the user asked to spawn 7 agents to finish the interrupted work and to
"additionally complete other one milestone". The next roadmap milestone after M13 is
"M14 AI Builder + Intent Contract" (`docs/v3/roadmap/50_RESTRUCTURED_MILESTONES.md`,
`51_DEPENDENCY_ORDER.md`). The V3 sources that bind it are
`docs/v3/architecture/13_INTENT_CONTRACT.md`, `17_AI_WORKFLOW_TRUST_MODEL.md`,
`19_APPLICATION_PATCH_REBASE.md`, `docs/specs/53_AI_BUILDER.md`,
`54_MODEL_GATEWAY.md`, `docs/risks/25_AI_CORRECTNESS.md`,
`35_PROMPT_INJECTION_AGENCY.md`, `37_AI_LOCKIN_COST_LATENCY.md`.
Foundation items R3b–R3d, R5d, R5e, R6 and R7 remain open (ADR036).

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
- Identity binding of the requester is the API process's (ADR039 residual).

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
