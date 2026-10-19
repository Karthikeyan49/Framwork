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

M7 additionally admits explicit schema1.1.0 through its own embedded closed schema.
Its rules require DSL2.0.0 and may contain pinned calendar snapshots and typed
literals. Schema1.0.0 is retained verbatim. Migration17 derives schema_version from
the inserted envelope without extending runtime column grants or mutating history.

ADR036 adds opt-in schema 1.3.0 (`ToolWorkflowSchemaVersion`): identical to 1.2.0
except a workflow activity may be `{tool_ref, version, inputs{name: field}, retry}`
with no timeout or compensation. `WorkflowSchema(v)` is true for 1.2.0 and 1.3.0;
use it wherever 1.2.0 selects workflow/rule V2 semantics. Schemas 1.0.0-1.2.0 are
unchanged.

## Patch rebase (M12 hardening, ADR038 section 8)

`ApplicationPatch.Rebase` is a true three-way merge of whole definitions over
base (what the patch author saw), current (the draft now) and theirs (the
patch). The change class is derived here, never trusted: an operation is
`PRESENTATION` only if every side that exists is a `page`; a declared class that
differs is the typed conflict `DECLARED_CLASS_MISMATCH`, and any non-page edit is
`SENSITIVE_CHANGE`. Convergent changes (current already equals the result, a
delete of an already deleted page) merge as no-ops; divergent edits are typed
conflicts (`CURRENT_DIVERGED`, `ADD_COLLISION`, `STALE_EXPECTED_HASH`,
`MISSING_TARGET`) and the patch is applied entirely or not at all. Output is
sorted, order independent and idempotent. The merged document is only a draft:
append it as a new revision and recompile, re-sandbox and re-accept it; a rebase
never carries an approval, acceptance or release across.
