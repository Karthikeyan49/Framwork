# Metadata contracts

application-v1.schema.json is the embedded JSON Schema 2020-12 contract for M3
schema_version 1.0.0. Its closed definitions cover entity, field, relationship,
page, rule, workflow and tool. Matching models live in internal/metadata.

The tool payload-schema contract is a bounded local subset, not arbitrary customer
JSON Schema: remote refs, defaults, custom keywords and executable constructors are
rejected. Page components and expression operators are allowlisted. IDs, lengths,
array sizes and numeric limits are validated before typed decoding.

Registered versions are immutable: 1.0.0 (application-v1), 1.1.0 (rule DSL2),
1.2.0 (workflow DSL2, `local.noop` activities) and 1.3.0 (ADR100 opt-in tools).
application-v1.3.schema.json is identical to 1.2.0 except that an activity is
`oneOf` the unchanged local shape or a tool shape `{tool_ref, version,
inputs{name: field id}, retry}` (1–32 inputs, no timeout or compensation). The
tool ref/version must resolve in the operator-owned trusted tool catalog at
compile time; authoring never declares hosts, methods, secrets or delivery
semantics. See ../examples/workflow-tool-draft-v1.5.json.

See ../examples/vendor-draft-v1.json for all seven primitive kinds. JSON-compatible
YAML authoring is supported by the backend; output/storage are normalized JSON.
A schema-valid source is draft, not compiled/authorized production functionality.
Symbol resolution, type checking and canonical IR belong to M4. Contract changes
must preserve immutable history and document explicit version compatibility.

R5e adds opt-in schema1.3 (`application-v1.3.schema.json`) and Workflow DSL2.1
for tenant-defined approval-role names. Schemas1.0/1.1/1.2 retain their original
contracts; schema1.2 workflows still require DSL2.0 and the four built-in role
names. Rules in1.1–1.3 remain DSL2.0. A role reference is not a permission grant:
live membership, role head, capabilities and DataScope govern approval.
See ADR036 and the R5e implementation report for compatibility and recovery.

R3c adds opt-in schema1.4 (`application-v1.4.schema.json`) with `field.unique`.
Schemas1.0–1.3 stay immutable and reject that property. False/absent flags retain
their omitted canonical representation. Rules remain DSL2.0; workflows retain
the explicit DSL2.0/2.1 contracts. The compiler validates eligible scalar types
and budgets (eight unique fields/entity,32/application). Runtime enforcement
requires registered, independently verified READY constraints; a valid draft
does not provision them. See ADR044 and the R3c implementation report.
