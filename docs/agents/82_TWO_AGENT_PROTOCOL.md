# Claude + Codex Two-Agent Protocol

Do not let both agents freely edit the same files simultaneously.

```text
Human/Architect
  ↓ milestone contract
Claude Code
  ↓ implementation + tests
Codex
  ↓ independent adversarial review
Claude Code
  ↓ fixes
Codex
  ↓ re-verification
Human
  ↓ approval
```

Independent criticism is more valuable than duplicate code generation.
