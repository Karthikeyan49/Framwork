# Claude Code Playbook

Role: primary bounded implementation agent.

Before each milestone read master playbook, relevant architecture/risk/spec docs, ADRs and existing tests.

Process:
inspect → state constraints → add/update tests → implement smallest coherent slice → run checks → update docs → report.

Forbidden:
production bypass, client-trusted tenant IDs, raw AI SQL, generic shell/SQL agent tools, unrelated refactors, unapproved Rust/Python/microservices, claiming tests passed without running them.
