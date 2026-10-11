# Authorization Complexity

## Model
Use RBAC + ABAC through one central policy interface.

Decision input:
subject + tenant + action + resource + context.

Decision output:
allow/deny + obligations + policy version + decision ID.

Example obligations:
- recent MFA
- manager approval
- audit high risk
- mask selected fields

OPA/Rego is a strong optional implementation because policy decision is separate from enforcement, but domain code depends only on your interface.

## Rule
Default deny.

## Verification
Policy matrix, unknown action, role removal, ownership, field masking, workflow/tool/promotion permissions.
