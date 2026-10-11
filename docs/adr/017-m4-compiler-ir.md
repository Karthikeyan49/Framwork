# ADR-017: Deterministic V1 compiler and preview IR

- Status: accepted
- Date: 2026-10-11
- Scope: M4

Compile only validated M3 Source through public contracts. Resolve symbols and exact
field ownership, field/core identifier types, relationship endpoints, approved UI
component/field compatibility, pure expression arity/types, workflow graph/roles/
capabilities and tool permission/effect declarations. Unsupported capabilities fail
with stable diagnostics rather than producing partially executable IR.

The immutable Program is minted only on success. Canonical typed JSON includes
IR/compiler versions, a normalized trusted catalog content hash, fixed physical
schema snapshot hybrid-preview-v1, normalized source, entity/field plans and generated
test requirements. Sort symbol/set collections; preserve ordered UI trees, expression
arguments and workflow branches. Runtime consumers receive copies. No AI/provider,
clock or database affects compilation. Golden IR and malformed-input fuzzing verify
these boundaries. No diagnostics or source literals are written to logs.

The exact conventional field ID field.<entity path>.id, if declared, is a required
string alias for the immutable platform UUID record identifier. It is explicitly
marked core_id in IR. Other fields use custom JSONB storage. V1 relationships are
many_to_one from reference fields to this core identifier; unsupported fanout fails
closed. This makes field-to-physical resolution explicit to later runtime consumers.

V1 compiles schema1.0.0 without automatic upgrades. A nonnull production base is
rejected: release/schema adapters and migration classification require promotion
milestones. The fixed preview storage snapshot requires no customer DDL; migration
class NONE means no physical change to that snapshot, never production safety.
Generated requirements are plans, not evidence that generated tests executed.
File/action/chart capabilities and executable tool adapters remain unavailable.

GET /metadata/{app}/versions/{revision}/compile resolves live tenant access and
metadata.compile policy, returns IR/hash or 422 diagnostics, and commits audit before
reply. It changes no registry/release state. Supported catalog permissions are
platform data.read/data.write; adding capabilities requires reviewed platform code.

M5/M6 are authorized by the current user request but must consume Program and remain
isolated development previews until M11/M12 implement sandbox/promotion. No direct
production execution path may be added. User authorized this three-milestone stretch;
no repeated milestone permission request is needed. Each implementation still gets
its own negative tests and independent review. Stop after M6.
