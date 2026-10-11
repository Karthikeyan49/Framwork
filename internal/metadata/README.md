# Metadata registry (M3)

Owns typed authoring models, embedded JSON Schema validation and immutable draft
source revisions. Public Validator/Source and Registry ports are consumed by API
composition; other modules must not query metadata tables or private implementation.

JSON and restricted YAML converge through strict JSON, schema and typed round-trip
validation. Source.Hash is a normalized storage identity, not M4 canonical IR or
production approval. Get revalidates stored source/hash; List returns bounded
revision headers. All versions remain draft. References, expression types and
workflow graphs are not semantically compiled in M3.

Registry operations receive the caller-owned M2 Executor and verified tenant Access.
Composition must perform policy and mandatory audit in that same transaction; only
the outer transaction commits. No retries or tenant defaults. Database grants,
forced RLS, immutable triggers and deferred head integrity protect storage.

Read [ADR-016](../../docs/adr/016-m3-metadata-source-registry.md) for quotas,
concurrency, security boundaries and recovery. No UI renderer, rule/workflow/tool
execution, sandbox, promotion or AI behavior is implemented here.
