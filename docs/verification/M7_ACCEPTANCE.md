# M7 Rule Engine V2 acceptance

V3 M7 scope is the pure typed engine plus compiler and safe preview composition.
M8 outbox/inbox delivery and later workflow/tool/promotion services are excluded.
See ADR021, the implementation report and independent review for executed evidence.

- [x] Immutable versioned DSL2 programs produced through the application compiler.
- [x] Closed structural schema1.1 and type/currency/entity-reference checks.
- [x] Canonical deterministic rule/IR hashes and stable copied outputs.
- [x] Stored plan restoration checks source/types/budgets/version/TZ-data bindings.
- [x] Existing schema/IR1.0 hashes preserved; no automatic release recompilation.
- [x] Integer, decimal, Money, dates/instants, enums/references and homogeneous lists.
- [x] Exact fixed-decimal arithmetic, rounding, overflow and zero-division failures.
- [x] Money currency compatibility; explicit reviewed minor-unit rounding.
- [x] Explicit IANA zones and versioned calendar snapshots; no host timezone lookup.
- [x] UTC/DST, leap dates, year boundaries, holidays/hours/fiscal/calendar tests.
- [x] Explicit missing/null, lazy branching and invalid-input semantics.
- [x] AST, list, string, output, delta and shared execution-work limits/cancellation.
- [x] No ambient clock, database, network, filesystem, secrets or tool capabilities.
- [x] Rule result does not grant privileges or cause mutation/event/tool execution.
- [x] Scoped actual-record preview with live rule/entity/row/field/DataScope/RLS checks.
- [x] Record-specific rule authorization receives actual record ID and actual owner.
- [x] Tenant isolation and audit-before-disclosure failure negatives.
- [x] Migration17 retains history/roles/RLS and derives the admitted schema column.
- [x] Algebraic properties and independent rational arithmetic oracle.
- [x] Timed fuzz campaigns and CI fuzz target, plus compiler/restore/adversarial tests.

Full regression/build/browser/smoke results and independent exit status are recorded
in the implementation report once executed. These checkboxes do not certify complete
V3 production deployment or human architecture/security sign-off.
