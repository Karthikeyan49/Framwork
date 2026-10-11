# audit

M1 Writer.Append adds typed bounded security evidence in the caller's transaction.
Postgres owns audit.entries; Ready checks its insert-only schema/column/RLS
contract through metadata without reading or writing evidence. Runtime credentials have column-limited INSERT only;
writer/time defaults cannot be overridden, and mutation triggers reject ordinary
UPDATE/DELETE/TRUNCATE. Operator state records explicit role/status/reference and
DB writer provenance. Tokens/secrets/raw inputs are never evidence. Audit storage
is not cryptographically immutable against privileged DDL. See ADR-014.
