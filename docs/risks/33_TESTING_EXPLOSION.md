# Testing Explosion

Generate tests from metadata.

Entities generate:
required/type/relation/permission/tenant cases.

Workflows generate:
happy path, branch paths, timeout, retry, duplicate, cancellation, failure.

Use Go fuzz/property tests for:
compiler, parser, expression evaluator, query AST, policy-input handling.

Use pairwise/covering-array techniques for large configuration matrices.

## Verification
CI reports generated scenario coverage by metadata primitive and workflow branch.
