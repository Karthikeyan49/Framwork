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

ADR036 adds opt-in schema/IR 1.3.0. `Catalog.ToolCatalog` (excluded from
`CatalogHash`, so existing IR hashes are unchanged) resolves each tool activity in
the operator-owned `tools.Catalog`, type-checks its input mapping against the
workflow entity, and requires an approval state's `approved` transition on every
path to an approval-required tool (`unknown_tool`, `tool_input_*`,
`tool_requires_approval`). A plan with a tool uses workflow DSL 2.1.0 and pins the
definition's canonical hash in `State.ToolHash`; tool-free plans keep DSL 2.0.0 and
identical bytes. Restore never recompiles: it requires a well-formed pin, and the
runtime re-resolves the pin against the live catalog and checks the tenant
allowlist before any send.
