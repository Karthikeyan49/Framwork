# Codex Cloud Master Prompt — Existing M0–M6 → V3 M6R

```text
You are working on an EXISTING AI-native ERP/CRM framework repository.

M0 through M6 are already implemented from an older plan.
DO NOT restart the repository.
DO NOT blindly rewrite working modules.

Read this complete V3 kit first, especially:
README.md
00_MASTER_PLAYBOOK_V3.md
01_M6_CURRENT_STATE_STRATEGY.md
02_V3_ARCHITECTURE_FREEZE.md
architecture/*
erp/*
integrations/*
migration/*
roadmap/*
verification/*

PHASE A — AUDIT ONLY

Inspect the repository and create:
docs/migration/M0_M6_AUDIT_REPORT.md

For each M0–M6 subsystem classify:
KEEP
KEEP+PATCH
REFACTOR
REMOVE/REPLACE

Specifically inspect:
1. dynamic entity physical storage
2. custom-field DDL
3. compiler IR/versioning
4. live-release recompilation assumptions
5. record-level authorization
6. DB role ownership/BYPASSRLS
7. background worker tenant access
8. UI coupling to tables/columns
9. migrations
10. audit/event transaction timing

Do not modify implementation until the audit report is complete.

PHASE B — IMPLEMENT M6R ONLY

Follow migration/21_M6R_REMEDIATION_PLAN.md.

Preserve compatible M0–M6 code.

Required outcomes:
- no tenant-driven DDL on shared business tables
- V3 dynamic RecordStore abstraction
- typed Domain Pack path remains possible
- release/IR compatibility model
- exact candidate-hash approval contracts
- Intent Contract + independent Business Acceptance Spec
- DataScope integrated with query authorization
- explicit DB role model
- tool delivery/idempotency/reconciliation contract
- explicit sandbox isolation contract
- ApplicationPatch/rebase contract
- transactionally coordinated business write/audit/outbox contract
- Money/Currency/TimeZone/BusinessCalendar/DocumentNumber types

If M5 storage is incompatible, use strangler migration:
legacy adapter + V3 adapter + controlled migration.
DO NOT DROP EXISTING DATA OR TABLES.

Run all existing M0–M6 tests plus new:
M6R unit/integration tests
tenant/RLS tests
DataScope tests
worker-role tests
compiler/IR determinism tests
migration tests

At the end report:
audit summary
files changed
migrations
exact commands run
PASS/FAIL
remaining compatibility issues
rollback/recovery
whether M7 is safe to begin

STOP after M6R.
DO NOT implement M7.
```
