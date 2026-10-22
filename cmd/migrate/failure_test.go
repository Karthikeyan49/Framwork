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
