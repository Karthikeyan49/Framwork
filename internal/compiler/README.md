# Compiler V1 (M4)

Compile validated metadata Source with a trusted versioned Catalog; return stable
Diagnostic objects or an immutable Program. Runtime receives Program, never raw
source. The canonical IR includes compiler/catalog/schema identities and explicit
entity/field plans. See ADR-017 and compiler_test/testdata golden corpus.

The HTTP compile operation is read-only and audited. It never approves, promotes,
executes or provisions a production application. Unsupported schema upgrades,
release bases and capabilities fail closed. Workflow/tool execution is later work.

M7 adds opt-in schema/IR1.1 and compiled DSL2 typed plans. Legacy1.0 output/golden
remains unchanged. Program.Rule restores stored plans, never compiles authoring
source at execution. SupportedIRVersions returns the explicit runtime matrix.
Stored restoration verifies source/plan/field/calendar/type/hash agreement and
preserves historical compiler provenance. See ADR021 and rules_test.go.

R5e adds explicit schema/IR/compiler1.3 and Workflow DSL2.1 for tenant-defined
approval role identifiers. Legacy schemas/compiled bytes/hashes stay unchanged;
restoration dispatches by stored versions without compiling. Tenant catalog
existence and capability authority are live runtime checks, never inferred from
a syntactically valid role reference. See ADR036 and approval_roles_test.go.

R3c adds opt-in schema/IR/compiler1.4 and typed `Field.Unique`. Restore verifies
the exact version pair, source/field agreement and declaration budgets without
recompiling. Integrity flags do not alter the physical storage hash; existing
compatible records retain their identity. Constraint-preserving shadow evolution
is not implemented, so declared or installed uniqueness fences that operation.
Schemas1.0–1.3 and their readers/hashes remain unchanged. See ADR044 and
integrity_test.go; SQL enforcement is a separate runtime contract.

ADR100 adds opt-in schema/IR 1.5.0 (a superset of 1.4.0; 1.3.0 and 1.4.0 admit no tool). `Catalog.ToolCatalog` (excluded from
`CatalogHash`, so existing IR hashes are unchanged) resolves each tool activity in
the operator-owned `tools.Catalog`, type-checks its input mapping against the
workflow entity, and requires an approval state's `approved` transition on every
path to an approval-required tool (`unknown_tool`, `tool_input_*`,
`tool_requires_approval`). A plan with a tool uses workflow DSL 2.2.0 (tenant approval roles, as DSL 2.1.0) and
pins the definition's canonical hash and contract in `State.ToolHash` and
`State.ToolContract`; tool-free plans keep their DSL and identical bytes. Restore never recompiles: it requires a well-formed pin, and the
runtime re-resolves the pin against the live catalog and checks the tenant
allowlist before any send.

## Release diff and storage classification (M12, ADR102)

`ComputeDiff(base, target)` compares two compiled or restored programs of one
application and returns a deterministic `Diff` (`diff-v1`). A zero base is the
first release. Definitions are matched by id; hashes use
`metadata.DefinitionHash` after clearing the envelope `schema_version`, so an IR
1.2.0 to 1.5.0 upgrade does not mark every definition changed (it sets
`ir_upgrade` instead, also when the compiler version, schema snapshot or catalog
hash differ). Each change carries sorted aspects from a closed vocabulary
(`LABEL`, `STORAGE`, `RULE_LOGIC`, `RULE_FINANCIAL`, `WORKFLOW_GRAPH`,
`WORKFLOW_TRIGGER`, `APPROVAL_GATE`, `TOOL_PIN`, `TOOL_DECLARATION`, `PAGE`,
`RELATIONSHIP`, `PLAN_ONLY`, `UNCLASSIFIED`); anything it cannot explain is
`UNCLASSIFIED`. "Financial" means a rule whose result is money or that reads a
money-typed field or literal. A graph change in a workflow that has an approval
state or a money-reading route also counts as `APPROVAL_GATE`: it can change who
reaches the gate even when the gate state is untouched. Separate lists expose
tool activities (ADDED/REMOVED/REPINNED by pinned hash), approval gates, money
conditions, trigger changes, authored tool declarations and changed compiled plan
hashes. Programs above 5,000 definitions, duplicate ids or different
applications are errors. `ParseDiff` proves stored bytes are canonical and bound
to a hash; it does not recompute (that needs both programs).

`ClassifyStorage(base, target, intent)` never errors. It returns `EQUAL` when
the storage schema hashes match, otherwise defers to `PlanEvolution`, the same
planner the data executor uses: `ADDITIVE` for only `ADD_ENTITY`/`ADD_OPTIONAL`
operations, `TRANSFORM` for rename, convert, relax-required or backfill, and
`BREAKING` (with machine-readable reasons) when the planner refuses. A zero base
is a first release (`ADDITIVE`, `first_release`).
