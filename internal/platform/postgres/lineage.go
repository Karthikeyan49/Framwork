package postgres

import (
	"context"
	"database/sql"
	"errors"
)

// ErrForeignMigrationNumbering is returned for a database that was built with the milestone line's migration numbers from before
// the two lines were integrated.
var ErrForeignMigrationNumbering = errors.New("database was built with the milestone line's pre-integration migration numbers (00045-00053, 00060); rebuild it from the current migrations, do not migrate it")

// CheckMigrationLineage refuses such a database. goose compares version numbers only, and 00045-00053 once meant the milestone
// line's migrations (tool ledger, sandbox, candidate builder, approval and promotion, review fixes) while they now mean the
// remediation line's. On such a database `up` would skip the remediation migrations that share a number and fail, or worse
// succeed on objects that are not what the number says. The milestone tool ledger now arrives with migration 00080, so a database
// that holds runtime.tool_invocations without version 00080 recorded can only come from the old numbering. A database without a
// version table (never migrated) and one that recorded 00080 are fine.
func CheckMigrationLineage(ctx context.Context, db *sql.DB) error {
	var versionTable, toolLedger bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('goose_db_version') IS NOT NULL, to_regclass('runtime.tool_invocations') IS NOT NULL`).Scan(&versionTable, &toolLedger); err != nil {
		return err
	}
	if !versionTable || !toolLedger {
		return nil
	}
	var renumbered bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM goose_db_version WHERE version_id=80 AND is_applied)`).Scan(&renumbered); err != nil {
		return err
	}
	if !renumbered {
		return ErrForeignMigrationNumbering
	}
	return nil
}
