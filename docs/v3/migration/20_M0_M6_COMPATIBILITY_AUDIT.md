# M0–M6 Compatibility Audit

Run before changing code.

## M0 Repository / CI
Check modular layout, test commands, telemetry. Usually KEEP.

## M1 Tenant / Identity / Policy
Check server-trusted tenant source and whether policy can grow to DataScope. Usually KEEP+PATCH.

## M2 RLS / Data Access
Check RLS, pooled connection safety, role ownership/BYPASSRLS, worker roles. KEEP+PATCH or REFACTOR.

## M3 Metadata Registry
Check stable IDs/versioning and remove any assumption that entity creation implies DDL. KEEP+PATCH.

## M4 Compiler
Check canonical IR, deterministic inputs/hashes, stored-release IR, and tenant-DDL assumptions. KEEP+PATCH or moderate REFACTOR.

## M5 Dynamic Data Runtime
Highest-risk existing milestone. Check:
- entity → physical table?
- custom field → ALTER TABLE?
- raw SQL?
- missing DataScope?
- concurrency/version fields?
If tenant metadata causes shared DDL, REFACTOR before continuing.

## M6 Dynamic UI
Usually reusable. Ensure semantic entity/field/query/action IDs; no physical table/column coupling. Backend remains authoritative.

## Output
Create `docs/migration/M0_M6_AUDIT_REPORT.md` with KEEP / KEEP+PATCH / REFACTOR / REMOVE per subsystem.
