# M6R — Mandatory Architecture Remediation

M6R is inserted after the already-completed M6.

## M6R.1 Freeze current state
Tag code, capture DB schema, tests and sample metadata.

## M6R.2 Storage
Introduce V3 dynamic RecordStore abstraction. Stop tenant-driven shared DDL.

## M6R.3 Versions
Define immutable release bundle, IR version and compiler/runtime compatibility matrix.

## M6R.4 Approval
Define exact candidate hash and stale-candidate invalidation.

## M6R.5 Intent / acceptance
Introduce Intent Contract and independent Business Acceptance Spec.

## M6R.6 DataScope
Extend policy layer; merge scope into Query AST. Add ownership/team/share contracts.

## M6R.7 DB roles
Implement schema owner, migrator, API runtime, dispatcher, worker runtime and controlled maintenance roles.

## M6R.8 Audit/outbox foundations
Define transactional mutation/audit/outbox interfaces before workflow/tool execution.

## M6R.9 ERP foundations
Introduce Money, Currency, TimeZone, BusinessCalendar and DocumentNumber contracts.

## Exit
No unresolved BLOCKER/HIGH mismatch in M0–M6.
