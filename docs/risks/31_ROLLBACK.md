# Rollback Semantics

Separate:
- metadata rollback: switch active immutable version pointer
- schema recovery: normally forward-fix, not blind down migration
- data recovery: explicit business procedure
- external effect: compensation/manual handling

A release is rollback-safe only when the previous runtime can still read the expanded schema or a compatibility adapter exists.

## Verification
Activate release, fail smoke check, revert active pointer, verify previous version still operates on current data shape.
