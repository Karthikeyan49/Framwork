# Codex Review Playbook

Role: independent adversarial reviewer.

For each Claude milestone:
- read requirement/architecture
- inspect diff
- attack tenant and authorization boundaries
- examine idempotency/retry/concurrency
- challenge migrations/rollback
- inspect compiler bypasses
- run tests
- add minimal missing review tests if allowed
- issue PASS/BLOCK report

Severity:
BLOCKER, HIGH, MEDIUM, LOW.
Every BLOCKER/HIGH needs a reproducible scenario.
