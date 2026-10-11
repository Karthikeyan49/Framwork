# Drawback → Technical Solution Matrix

| Gap / drawback | Technical solution | Required verification |
|---|---|---|
| Framework complexity | modular monolith, bounded modules, dependency rules | architecture tests |
| Over-engineering | V1 capability budget, admission rule for new primitives | roadmap gate |
| Metadata complexity | canonical schema, namespaces, compiler/linter | golden compiler tests |
| Bad metadata evolution | versioned canonical IR + migration adapters | compatibility suite |
| Schema evolution | expand → backfill → contract | migration rehearsal |
| Existing data migration | staging + mapping + validation + reconciliation | count/total checks |
| Slow dynamic queries | query AST, budgets, indexes, pagination, telemetry | p95/p99 load tests |
| JSONB overuse | typed core columns + JSONB custom fields + promotion policy | query benchmark |
| Custom logic ceiling | metadata → DSL → capability-limited WASM | sandbox tests |
| Custom UI ceiling | component registry + approved custom components | E2E/accessibility |
| AI hallucination | structured output + compiler + sandbox + approval | adversarial AI suite |
| AI non-determinism | canonical IR; AI only authors drafts | deterministic IR hash |
| Ambiguous requirement | unresolved-requirement objects that block promotion | approval test |
| Tenant leakage | trusted context + PostgreSQL RLS + policy + attack tests | cross-tenant suite |
| Permission complexity | central RBAC+ABAC policy layer | policy matrix |
| Workflow failures | durable state, retry, timeout, DLQ, compensation | failure injection |
| Duplicate side effects | idempotency ledger | replay tests |
| DB/event dual writes | transactional outbox | crash tests |
| Rollback limits | immutable config rollback + forward-fix/compensation | rollback drill |
| Version explosion | base templates + overlays + diffs + resolved bundle hash | scale test |
| Testing explosion | generated tests + property/fuzz + pairwise coverage | CI gates |
| Debug complexity | OpenTelemetry correlation end-to-end | trace completeness |
| Prompt injection | untrusted-context boundary + policy/tool gates | injection suite |
| Excessive AI agency | least-capability tools + approval thresholds | red-team suite |
| Integration failures | adapters, inbox, retries, circuit breakers, replay | outage tests |
| AI provider lock-in | provider-independent model gateway | provider swap test |
| AI cost | budgets/routing; no LLM in deterministic path | cost test |
| AI latency | async authoring + cache + streaming UX | latency SLO |
| Customer lock-in | documented portable metadata/data export bundle | roundtrip import/export |
| Multi-language complexity | Go+TS first; Rust/Python only behind stable contracts | architecture gate |
| Compliance | immutable audit + retention + domain packages | audit completeness |
| Sandbox/prod drift | same runtime binary, different capability/data context | parity tests |
| State conflicts | DB transactions + optimistic concurrency/version columns | race tests |
