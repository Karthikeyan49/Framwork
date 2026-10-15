# Master Playbook V3

## Mission
Preserve safe M0–M6 implementation, repair architectural gaps, freeze V3 contracts, then continue development.

## Non-negotiable invariants
1. Raw LLM output never mutates production.
2. Tenant metadata never alters shared tenant business schema directly.
3. Runtime executes canonical IR, not raw metadata.
4. Live releases are not automatically recompiled when compiler changes.
5. Approval binds to the exact candidate artifact activated.
6. Required ambiguity blocks promotion.
7. Record authorization is enforced through DataScope/query scope.
8. Runtime roles do not own tenant tables or bypass RLS.
9. Cross-tenant dispatch does not imply cross-tenant business-data access.
10. External-effect guarantees are capability-specific.
11. AI is never authorization.
12. Business mutation + audit + outbox are transactionally coordinated where applicable.
13. Rollback distinguishes metadata, schema, data and external effects.
14. Compiler output is deterministic for identical canonical inputs/version.

## Current-state rule
M0–M6 must first be classified as KEEP, KEEP+PATCH, REFACTOR or REMOVE/REPLACE.

No M7 begins with unresolved BLOCKER/HIGH M6R findings.
