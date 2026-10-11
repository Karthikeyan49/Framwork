# Coding-agent contract

Read README.md, docs/00_MASTER_PLAYBOOK.md, docs/01_DRAWBACK_SOLUTION_MATRIX.md,
docs/02_SCOPE_AND_NON_GOALS.md, all relevant architecture/spec/risk/implementation/
verification documents and docs/adr before changing code. The supplied buildkit is
preserved verbatim under docs; validate it with make docs-verify.

## Milestone discipline

M0 and M1 are implemented. Stop after the requested milestone; never begin M2 without
a new task. Follow docs/implementation/50_IMPLEMENTATION_ROADMAP.md. Each milestone
requires tests, negative checks, recovery notes, an independent review, and human
approval (docs/agents/82_TWO_AGENT_PROTOCOL.md). Reviewers should inspect and run
checks independently; avoid concurrent edits to the same files.

## Architecture and safety

- Go modular monolith + React/TypeScript. No early Rust/Python services,
  microservices, Kubernetes, code generation, event sourcing, or WASM.
- Business modules expose explicit contracts; no cross-module private imports or
  direct access to another module's tables. Composition belongs to platform/app.
- AI proposes structured drafts only. It cannot run arbitrary SQL/shell, obtain
  secrets, grant privileges, or bypass policy/compiler/sandbox/approval/promotion.
- Runtime consumes canonical typed IR, never raw LLM output or arbitrary scripts.
- Tenant identity is derived from authenticated membership server-side. Unknown
  authorization is denied. Frontend hiding is never an enforcement mechanism.
- M1 identity uses pinned public RS256 access-token keys, issuer/audience/time
  checks and opaque Principal/User/Access types. Ignore token roles/tenant claims.
  Resolve live membership and policy in the request transaction; commit audit
  evidence before returning protected data. Unknown obligations deny.
- IAM provisioning is an explicit operator CLI with separate migration credentials,
  atomic state/audit writes, and serialized last-owner checks. No runtime IAM writes.
  Read ADR-014 before changing authentication, roles, policy or audit contracts.
- M1 IAM already has a narrow RLS backstop; reusable business-data RLS and access
  remain M2. Tenant data must use transaction-scoped tenant context. The normal
  app role must not own tenant tables, be superuser, or have BYPASSRLS.
- Metadata production releases are immutable; promotion freezes and revalidates a
  revision, approval, base, migration, and bundle before atomic activation.
- Side effects require governed tools, idempotency, capability checks, safe sandbox
  routing, and explicit compensation limits. Business updates/events use outbox.
- PostgreSQL is authoritative; typed core + JSONB, not universal EAV. Metadata
  queries compile through a bounded typed AST into parameterized SQL.
- Do not log secrets, DSNs, raw driver errors, request bodies, or untrusted URLs.
  Do not commit .env, dependencies, binaries, build outputs, or credentials.
- Preserve signature/checksum/TLS verification and managed dependency lockfiles.
- Preserve local identity keys and owner-only private-key permissions. Never use
  the development issuer/keys in production. Never print access tokens.

## Working and verification

Use the existing checkout; each cloud task is already isolated. Do not create a
worktree unless explicitly requested. Use make setup, make infra-up, make migrate,
make dev, make check, make test, and make build. Never claim a check passed without
running it. Integration tests need the local stack and fail rather than silently
skip missing services. Later business E2E/RLS/compiler suites are not M0 evidence.
Document significant new decisions in docs/adr using its template. Keep source
buildkit documents unchanged. Add tests for failure modes, not just happy paths.
