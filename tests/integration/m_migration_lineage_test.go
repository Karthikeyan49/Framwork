//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// The milestone line's migrations 00045-00053 and 00060 were renumbered 00080-00089 when it was integrated. A database built with the
// old numbers must be refused by the migrator instead of being skipped over by goose (independent review IR-05).
func TestMigrationLineageRefusesTheMilestoneLinesOldNumbering(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	ctx := context.Background()
	if err := postgres.CheckMigrationLineage(ctx, f.operator); err != nil {
		t.Fatal("a database migrated with the current numbers was refused", err)
	}
	// The old numbering leaves the tool ledger (now created by 00080) without version 80 on record.
	scalarSQL(t, func() error { _, e := f.operator.Exec(`DELETE FROM goose_db_version WHERE version_id=80`); return e }())
	if err := postgres.CheckMigrationLineage(ctx, f.operator); !errors.Is(err, postgres.ErrForeignMigrationNumbering) {
		t.Fatal("a database with the tool ledger but without version 00080 was not refused", err)
	}
	// Never migrated: no version table, nothing to refuse.
	scalarSQL(t, func() error { _, e := f.operator.Exec(`DROP TABLE goose_db_version`); return e }())
	if err := postgres.CheckMigrationLineage(ctx, f.operator); err != nil {
		t.Fatal("a database without a version table was refused", err)
	}
}
