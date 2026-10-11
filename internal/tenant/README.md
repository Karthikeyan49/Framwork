# tenant

M1 owns tenants, memberships, fixed roles and immutable Access contexts. BindActor,
List and Resolve accept a database-resolved identity.User. A selected tenant UUID
becomes trusted only after live active-membership verification. Context settings
are transaction-local. Consumers must enforce policy before reading Members.
Provision and CheckDisable are operator ports with owner-retention safeguards.
No other module may query tenant-owned tables. See ADR-014; reusable business
RLS/data access remain M2.
