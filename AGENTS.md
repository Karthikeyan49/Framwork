# Coding-agent contract

Current authorized work is M9R foundation remediation after the published
M0–M9 review. Follow [the plan](docs/migration/M9R_REMEDIATION_PLAN.md) and
[findings](docs/verification/M0_M9_REVIEW_VALIDATION.md). Preserve working code
and data. Do not begin M10 automatically.

## Architecture authority

Read README, all V3 Markdown, the M0–M6 compatibility audit, relevant contracts
and ADRs before coding. Use [the supersession map](docs/migration/V2_V3_SUPERSESSION_MAP.md)
for V2/V3 conflicts; retained V2 specs supplement unchanged modules. Preserve
both source kits verbatim and run `make docs-verify`. Historical prompts do not
override current user scope.

Go modular monolith + React/TypeScript, PostgreSQL, Redis and OpenTelemetry remain
the stack. No early microservices, Rust/Python, Kubernetes, event sourcing, WASM
or generic code execution. Modules use explicit public contracts; composition
belongs to `internal/platform/app`.

## Security and persistence

- AI proposes typed drafts only. Never expose SQL/shell executors, secrets,
  privilege grants or production mutation to metadata/LLM output.
- Runtime executes immutable canonical IR. Identical inputs/version are
  deterministic. Restore never recompiles or replaces active/in-flight releases.
  Approval binds the exact verified artifact activated.
- Authentication supplies trusted identity; resolve live tenant membership and
  enforce default-deny policy plus SQL DataScope and forced RLS. Frontend hiding
  and rule results are never authorization.
- Tenant keys/FKs include tenant identity. Runtime roles cannot own tenant tables,
  bypass RLS, inherit operator roles or perform tenant-triggered shared DDL.
  Keep operator credentials out of runtime subprocesses.
- PostgreSQL GUCs are not credentials. Trusted Go tests do not prove safety
  against compromised credentials/processes. Worker authority requires an
  authenticated, database-enforced job/resource boundary.
- Repository callbacks cannot commit, change roles or control transactions.
  Business mutation, audit and outbox commit together. Retry only known-aborted,
  repeatable operations with bounded/idempotent semantics; reconcile unknown
  commit/external outcomes before repeating effects.
- Preserve records and immutable evidence. Use additive migrations, checkpointed
  validation and explicit cutovers. Metadata rollback cannot undo incompatible
  data changes or external actions. Never erase inbox/receipts to retry.
- No secrets, tokens, DSNs, raw driver errors or customer payloads in logs/reports.
  Never commit `.env`, private keys, dependencies, binaries or generated builds.
  Preserve local key permissions and checksum/TLS verification.

## Module contracts

- ADR014–016: RS256 identity, operator IAM, transaction scope, strict authoring,
  immutable draft revisions. Token role/tenant claims do not grant membership.
  Schema-valid source is not executable.
- ADR017–020: compiler, preview data/UI and V3 contracts. Preserve legacy adapters;
  preview isolation is not a production persistence model. Field policy applies
  to returned, filtered, joined and modified data.
  ADR037: interactive queries use SQL masking; strict point inputs check trusted
  record/owner headers. Preserve snapshot proofs, opaque-policy budgets and
  omitted-field semantics in partial patches.
- ADR038: access administration reads typed headers under live entity/row
  authority, never unrelated business values. Preserve actor-owned recovery
  across SPA routes; unknown outcomes and unobservable truncated-list targets
  block new changes. No automatic administrative POST retry.
- ADR021: pure DSL2 has no ambient clock/network/filesystem/DB/tool access.
  Preserve exact fixed-point money, rounding, pinned calendar/timezone data,
  budgets and declared legacy IR compatibility. Rules never authorize effects.
- ADR022: PostgreSQL owns envelope/lease/inbox state; brokers carry references.
  Exact attempts, delegated policy and atomic local effect/audit/receipt precede
  ACK. Preserve identity and evidence during dead-letter/replay.
- ADR023: workflows pin IR/plan/input bytes. Preserve fenced leases, durable
  history, versioned signals, cancellation, recovery and compensation limits.
  Only `local.noop` is registered. M9R corrects the historic worker boundary;
  external tools and production activation remain later milestones. ADR036 adds
  opt-in metadata/IR1.3 and Workflow DSL2.1 tenant approval roles; preserve legacy
  readers and live revocation fences. Role references never grant capabilities.

## Delivery and verification

Use the existing checkout; no worktree unless requested. Deliver small coherent
changes and ADRs for new decisions. Commands: `make setup`, `make infra-up`,
`make migrate`, `make check`, `make test`, `make build`. Browser checks use
`make browser-install` then `make test-e2e` with owned ports.

Apply the [two-agent protocol](docs/agents/82_TWO_AGENT_PROTOCOL.md): independent
adversarial review of stable code, without concurrent edits/test runs against
moving files. Use populated negative fixtures and execute attacks, faults,
migration rehearsals and representative workloads where claimed. Disposable
DB probes must verify effective driver targets and connected database/role
before modifying fixtures; changing a URI path alone is insufficient.

Report exact commands, PASS/FAIL/NOT RUN, threat model, performance envelope,
remaining gaps and recovery. Contract-only services and IR test labels are not
business acceptance evidence. Fix and reverify review findings before declaring
completion. Existing user authorization governs work; request a human decision
only for a concrete consequential action outside that scope.
