# Metadata Complexity

## Failure mode
Metadata grows into an undocumented second programming language.

## Solution
Keep a small set of first-class primitives:
entity, field, relationship, page, query, action, rule, workflow, policy reference, tool, integration, report.

Every primitive has:
- stable ID
- schema version
- namespace
- typed schema
- compiler rules
- owner/module
- documentation

## Governance
A new primitive enters the core only when multiple real use cases require it or security/correctness demands it.

## Verification
- unknown keys rejected
- duplicate IDs rejected
- schema compatibility corpus
- canonicalization produces stable output
