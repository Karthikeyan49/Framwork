# Strategy With M0–M6 Already Built

Most likely reusable:
- repository and CI
- Go/React shells
- logging and OpenTelemetry
- PostgreSQL/Redis wiring
- identity/tenant middleware
- RLS foundation
- metadata APIs
- compiler scaffolding
- dynamic UI component registry

Highest-risk areas:
- M5 physical storage model
- custom field DDL
- compiler/release version assumptions
- record-level authorization
- background DB roles
- dynamic UI coupled to table names
- audit/event transaction timing

## Audit output
For each subsystem create:
- current files/modules
- current DB objects
- tests
- architectural assumption
- V3 compatibility
- action: KEEP / KEEP+PATCH / REFACTOR / REMOVE
