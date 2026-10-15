# ApplicationPatch and Rebase

Patch example:
```yaml
base_release: rel_123
operations:
  - op: update
    target: workflow.purchase_approval
    expected_hash: abc123
    changes:
      threshold: 500000
```

Use 3-way merge: BASE vs DRAFT vs CURRENT_PRODUCTION.

Never auto-merge conflicts involving permissions, financial rules, side effects, migrations, tools or domain-pack invariants.
