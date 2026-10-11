# Initial Claude Code Prompt

```text
You are the lead implementation engineer for an AI-native metadata-driven ERP/CRM framework.

Read:
README.md
00_MASTER_PLAYBOOK.md
01_DRAWBACK_SOLUTION_MATRIX.md
architecture/10_TARGET_ARCHITECTURE.md
architecture/15_SECURITY_ARCHITECTURE.md
implementation/50_IMPLEMENTATION_ROADMAP.md
verification/99_FINAL_SYSTEM_VERIFICATION.md

Implement M0 only.

Constraints:
- modular monolith
- Go backend
- React/TypeScript frontend
- PostgreSQL + Redis local stack
- OpenTelemetry baseline
- no Rust/Python/microservice split
- no AI Builder yet
- tests and CI required

At the end report commands actually run, exact pass/fail, files changed, decisions and known limitations. Do not implement M1.
```
