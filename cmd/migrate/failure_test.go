package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

func TestUpFailureNamesTheNumberingRuleAndTheFailedMigrationButNothingFromTheDriver(t *testing.T) {
	refused := upFailure(errors.New("detected 1 missing (out-of-order) migration lower than database version (90): version 75"))
	if !strings.Contains(refused.Error(), "version 75") || !strings.Contains(refused.Error(), "lower than database version (90)") {
		t.Fatalf("the refusal must name the versions: %v", refused)
	}
	partial := &goose.PartialError{
		Failed: &goose.MigrationResult{Source: &goose.Source{Version: 91}},
		Err:    errors.New(`failed to connect to host=db.internal user=framework_migrator: password authentication failed`),
	}
	text := upFailure(fmt.Errorf("wrapped: %w", partial)).Error()
	if !strings.Contains(text, "migration 91 failed") || strings.Contains(text, "db.internal") || strings.Contains(text, "framework_migrator") {
		t.Fatalf("the failed migration is named and the driver text is not echoed: %q", text)
	}
	generic := upFailure(errors.New(`dial tcp 10.0.0.5:5432: connect: connection refused (user=framework_migrator)`)).Error()
	if strings.Contains(generic, "10.0.0.5") || strings.Contains(generic, "framework_migrator") {
		t.Fatalf("an unrecognised failure stays generic: %q", generic)
	}
}

func TestStatusMarksThePendingMigrationsThatUpRefuses(t *testing.T) {
	migration := func(path string, version int64, state goose.State) *goose.MigrationStatus {
		return &goose.MigrationStatus{Source: &goose.Source{Path: path, Version: version}, State: state}
	}
	// Nothing below the highest applied version is pending: a plain list, nothing refused.
	lines, refused := statusLines([]*goose.MigrationStatus{migration("00001_a.sql", 1, goose.StateApplied), migration("00002_b.sql", 2, goose.StateApplied), migration("00003_c.sql", 3, goose.StatePending)})
	if refused != 0 || lines[2] != "00003_c.sql pending" || lines[0] != "00001_a.sql applied" {
		t.Fatalf("%d refused, %q", refused, lines)
	}
	// A file numbered below the highest applied version is marked, and counted, whatever follows it.
	lines, refused = statusLines([]*goose.MigrationStatus{
		migration("00057_x.sql", 57, goose.StateApplied), migration("00075_late.sql", 75, goose.StatePending),
		migration("00080_y.sql", 80, goose.StateApplied), migration("00090_z.sql", 90, goose.StatePending),
	})
	if refused != 1 || !strings.Contains(lines[1], "refused by up") || !strings.Contains(lines[1], "highest applied version 80") || strings.Contains(lines[3], "refused") {
		t.Fatalf("%d refused, %q", refused, lines)
	}
	// An empty database has no highest version: nothing is refused.
	if _, refused = statusLines([]*goose.MigrationStatus{migration("00001_a.sql", 1, goose.StatePending)}); refused != 0 {
		t.Fatalf("%d refused on an empty database", refused)
	}
}
