# AI-Native ERP/CRM Framework V2 — Research & Implementation Build Kit

This kit defines how to build a safe metadata-driven AI-native enterprise application framework.

## Product thesis
User intent → AI structured patch → Compiler → Sandbox → Automated Verification → User Approval → Immutable Production Version → Deterministic Runtime.

## Non-negotiable principle
**AI may propose; deterministic systems validate and execute. Raw model output never changes production directly.**

## V1 stack
- React + TypeScript frontend
- Go modular-monolith backend
- PostgreSQL source of truth
- Redis cache / coordination / optional short-retention streams
- S3-compatible object storage
- OpenTelemetry
- JSON Schema + typed Go models
- Optional OPA policy engine
- Optional Temporal durability adapter
- WASM extensions later, not V1

## Start here
1. `00_MASTER_PLAYBOOK.md`
2. `01_DRAWBACK_SOLUTION_MATRIX.md`
3. `architecture/10_TARGET_ARCHITECTURE.md`
4. `implementation/50_IMPLEMENTATION_ROADMAP.md`
5. `agents/80_CLAUDE_CODE_PLAYBOOK.md`
6. `verification/99_FINAL_SYSTEM_VERIFICATION.md`
