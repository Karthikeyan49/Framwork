# Dependency Order

```text
M0–M6 existing
   ↓
M6R
   ↓
M7 Rules
   ↓
M8 Outbox
   ↓
M9 Workflow
   ↓
M10 Tools
   ↓
M11 Sandbox
   ↓
M12 Candidate Bundle
   ↓
M13 Approval/Promotion
   ↓
M14 AI Builder
   ↓
M15 Hardening
   ↓
M16 Domain Packs
   ↓
M17 Integration/Portability
   ↓
M18 Scale
   ↓
M19 WASM optional
```

Outbox is before workflow/tools because reliable execution depends on event/idempotency foundations.
