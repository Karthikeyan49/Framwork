# PostgreSQL row-level security

M1 introduces a narrow forced-RLS backstop for users, memberships, tenants and
append-only audit in embedded migration00002. Verified identity/actor/tenant
settings are transaction-local; absence denies row access. Runtime role checks
reject table/schema owners and operator privileges. See ADR-014 and the real
normal-role integration suite.

M2 remains responsible for reusable business-data repository/transaction contracts,
RLS policies for future business tables and broader isolation tests. Do not add
business data until that milestone is explicitly authorized.
