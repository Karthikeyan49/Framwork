# Master Playbook

## Mission
Build one enterprise runtime where pages, entities, workflows, rules, permissions and integrations are defined as versioned metadata rather than cloned applications.

## Invariants
1. No raw AI output reaches production.
2. Tenant context comes from trusted server identity, never client claims alone.
3. UI visibility is never authorization.
4. Every effectful workflow step is idempotent.
5. Database write + event publish uses transactional outbox.
6. Sandbox blocks/reroutes dangerous real-world side effects.
7. Production metadata is immutable by version.
8. Extensions receive only explicit capabilities.
9. Destructive migrations are never auto-promoted.
10. Every milestone needs automated tests and recovery notes.

## Development order
Foundation → Metadata → Compiler → Data runtime → Dynamic UI → Rules → Workflow → Tools → Outbox → Sandbox → Promotion → AI Builder → Hardening → Optional extensions.

## V1 architecture decision
Use a **modular monolith in Go**. Keep strong internal module contracts. Extract services only after measured scaling or team-boundary evidence.

## Completion
A feature is done only when implementation, negative tests, tenant/security checks, observability, migration impact and recovery/rollback are all documented.
