# Complete Implementation Roadmap

## M0 Repository & CI
Local stack, Go/React skeletons, migration tooling, OTel baseline, lint/test/build.

## M1 Tenant / Identity / Policy
Trusted tenant context, users, memberships, roles, policy interface, audit.

## M2 PostgreSQL RLS / Data Access
RLS policies, least-privilege app role, transaction-scoped tenant context, isolation tests.

## M3 Metadata Registry
Entity/field/relationship/page/rule/workflow/tool schemas, versions, JSON Schema validation.

## M4 Compiler V1
Reference/type checks, canonical IR, deterministic hashes, diagnostics.

## M5 Dynamic Data Runtime
Typed query AST, CRUD commands, custom fields, pagination, concurrency control.

## M6 Dynamic UI
Forms/tables/detail/layout/component registry.

## M7 Rule Engine
Pure typed expression evaluator + fuzz tests.

## M8 Workflow
Business workflow model + durable adapter + approvals/timers/retries.

## M9 Tool Registry
Side-effect classes, policy, idempotency, sandbox modes.

## M10 Transactional Outbox
Atomic events, dispatcher, inbox/dedup, replay.

## M11 Sandbox
Draft version, safe datasets, dry-run/reroute tools, generated verification.

## M12 Promotion
Diff, stale-base handling, approvals, migrations, immutable release, atomic activation, rollback pointer.

## M13 AI Builder
Model gateway, structured ApplicationPatch, compiler repair loop.

## M14 Observability / Operations
Traces, metrics, logs, dashboards, alerts, incident bundle.

## M15 Security Hardening
Prompt-injection, excessive-agency, secret, rate-limit and authorization attack suites.

## M16 Performance / Scale
Load, query budgets, cache, worker split if justified.

## M17 Portability
Export/import application bundle and data dictionary.

## M18 Optional WASM Extensions
Only after a real requirement proves need.

Each milestone ends with independent Codex review and human gate.
