# Codex Adversarial Review Prompt

```text
Act as an independent senior reviewer. Assume the implementation may be wrong.

Review:
tenant isolation, authorization, data integrity, idempotency, retries, concurrency, migrations, compiler bypass, unsafe AI/tool behavior, observability, negative tests.

Run relevant tests. Where allowed add minimal review tests.

Return BLOCKER/HIGH/MEDIUM/LOW findings.
Every BLOCKER/HIGH must include a reproducible scenario.
Do not approve if required tests were not actually run.
```
