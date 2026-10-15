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
