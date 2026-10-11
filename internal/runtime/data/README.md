# Typed data runtime (M5)

Public Store Query/Command ports require a compiled private Scope, verified tenant
Access, caller-owned M2 Executor and mandatory Guard. No raw metadata/SQL execution.
API composition owns policy/audit/commit; SQL implementations stay within this module.

Uses isolated preview records with typed core and JSONB custom fields. Central policy
checks projected/filter/sort/reference/join fields and rows; safe exact values, budgets,
keyset cursors and optimistic versions apply. Dataset writes serialize reference safety.
The runtime publishes no events, runs no external tools and is disabled in production.
See ADR-018, values_test and real integration/data_runtime_test for bounds/recovery.
