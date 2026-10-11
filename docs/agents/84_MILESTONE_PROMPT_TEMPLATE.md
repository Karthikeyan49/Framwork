# Milestone Prompt Template

```text
Implement milestone <ID> only.

Read the master playbook, target architecture, relevant risk solutions/specs, and final verification.

Objective:
...

Required deliverables:
...

Acceptance criteria:
...

Security invariants:
trusted tenant context; deny-by-default; no AI/raw-metadata bypass; side effects policy checked/idempotent.

Tests:
unit + integration + negative/security + relevant failure injection.

Do not refactor unrelated modules.

At completion report:
implementation, commands actually run, results, migrations, limitations, recovery/rollback notes.
```
