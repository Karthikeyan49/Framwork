# SQL catalog audit (migrations 00001-00092)

Auditor: independent, report-only. Branch `audit/sql-catalog`, pinned commit `11a4ee4`.
Database: stack `m12db` (PostgreSQL 18.6), read-only session (`default_transaction_read_only=on`),
every query through `dbrunm12db`; `current_database()` = `framework` checked first.

## Progress

- [x] Text scan of the 70 migration files (check 8) and data-type pitfalls (check 9, text part).
- [x] Function catalog (check 1): 151 functions, 104 SECURITY DEFINER.
- [ ] Table/RLS/policy/privilege/constraint/trigger catalogs (checks 2-7): queued behind another agent's run on the shared stack.

(This section is replaced by the full report below as results arrive.)
