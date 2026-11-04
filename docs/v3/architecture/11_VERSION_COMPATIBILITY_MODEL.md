# Version Compatibility Model

Every release stores:
- release_id
- source_metadata_hash
- canonical_ir_hash
- metadata_schema_version
- ir_version
- compiler_version
- runtime_contract_version
- base_release_id
- policy_bundle_hash
- tool_catalog_hash
- schema_snapshot_id

## Rule
Compiler upgrades never silently recompile active releases.

Production executes immutable stored IR.

Every platform build declares supported IR versions and is blocked if active releases use unsupported IR.

In-flight workflows pin release/workflow versions; tenant rollback affects new executions by default.
