# AI-Native ERP/CRM Framework V3 — M6 Migration & Restructuring Kit

This V3 kit is specifically for a repository that already implemented **M0–M6** under the earlier plan.

Do not restart.

```text
Existing M0–M6
      ↓
Compatibility Audit
      ↓
M6R Architecture Remediation
      ↓
V3 Architecture Freeze
      ↓
Continue M7+
```

## Corrected product flow

User Intent → Intent Contract → AI Builder → ApplicationPatch → Compiler → Canonical IR → Sandbox → Independent Acceptance Verification → Candidate Bundle → Hash Approval → Atomic Activation → Deterministic Runtime.

## Major V3 corrections

1. Tenant custom metadata never performs arbitrary DDL on shared business tables.
2. Dynamic entities use a generic tenant-scoped record store.
3. Invariant-heavy ERP functions use typed Domain Packs.
4. Active releases run stored immutable IR; compiler upgrades do not silently recompile production.
5. Approval is bound to the exact candidate bundle hash.
6. Ambiguity is represented by an Intent Contract, not left to the LLM to self-report.
7. Record-level authorization uses DataScope in addition to tenant RLS.
8. Background workers have explicit least-privilege database roles.
9. External delivery guarantees are tool-specific; no false universal exactly-once promise.
10. AI workflow steps cannot authorize high-risk side effects.
11. Sandbox isolation, ApplicationPatch/rebase and audit/outbox semantics are explicit.
12. Money, currency, time zones, calendars and document numbering are first-class.

## Start here

1. `00_MASTER_PLAYBOOK_V3.md`
2. `01_M6_CURRENT_STATE_STRATEGY.md`
3. `02_V3_ARCHITECTURE_FREEZE.md`
4. `migration/20_M0_M6_COMPATIBILITY_AUDIT.md`
5. `migration/21_M6R_REMEDIATION_PLAN.md`
6. `roadmap/50_RESTRUCTURED_MILESTONES.md`
7. `agents/80_CODEX_M6R_MASTER_PROMPT.md`
8. `verification/99_V3_RELEASE_GATE.md`
