# Module Boundaries

- `identity`: authentication/users/service identities
- `tenant`: membership, limits, trusted tenant context
- `policy`: central authorization decisions
- `metadata`: draft + immutable definitions
- `compiler`: source → diagnostics → canonical IR
- `runtime/data`: typed Query AST and commands
- `runtime/rules`: deterministic expressions
- `runtime/workflow`: state/timers/approvals/durability adapter
- `tools`: governed side effects
- `events`: outbox/inbox/dedup/replay
- `sandbox`: preview/data/capability isolation
- `promotion`: approval/release/migrations/activation
- `ai`: model gateway + semantic context + patch generation
- `audit`: append-only evidence
- `observability`: logs/traces/metrics

Modules depend on contracts, not another module's private tables.
