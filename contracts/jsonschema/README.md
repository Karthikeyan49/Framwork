# Metadata contracts

application-v1.schema.json is the embedded JSON Schema 2020-12 contract for M3
schema_version 1.0.0. Its closed definitions cover entity, field, relationship,
page, rule, workflow and tool. Matching models live in internal/metadata.

The tool payload-schema contract is a bounded local subset, not arbitrary customer
JSON Schema: remote refs, defaults, custom keywords and executable constructors are
rejected. Page components and expression operators are allowlisted. IDs, lengths,
array sizes and numeric limits are validated before typed decoding.

See ../examples/vendor-draft-v1.json for all seven primitive kinds. JSON-compatible
YAML authoring is supported by the backend; output/storage are normalized JSON.
A schema-valid source is draft, not compiled/authorized production functionality.
Symbol resolution, type checking and canonical IR belong to M4. Contract changes
must preserve immutable history and document explicit version compatibility.
