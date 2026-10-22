package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || (args[0] != "up" && args[0] != "status") {
		return fmt.Errorf("usage: migrate up|status (destructive down is not exposed)")
	}
	raw, err := config.MigrationURL(os.Getenv)
	if err != nil {
		return err
	}
	db, err := postgres.Open(raw)
	if err != nil {
		return err
	}
	defer db.Close()
	if db.PingContext(ctx) != nil {
		return fmt.Errorf("migration database unavailable")
	}
	if err := postgres.CheckMigrationLineage(ctx, db); err != nil {
		if errors.Is(err, postgres.ErrForeignMigrationNumbering) {
			return err
		}
		return fmt.Errorf("migration lineage check failed")
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migration lock initialization failed")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
	if err != nil {
		// The provider reads only the embedded files, never the database, so its text names files and versions, not connection details
		// (a duplicate number, for example); the operator needs it to see which two migrations collide.
		return fmt.Errorf("migration provider initialization failed: %s", strings.Join(strings.Fields(err.Error()), " "))
	}
	if args[0] == "up" {
		results, err := provider.Up(ctx)
		if err != nil {
			return upFailure(err)
		}
		fmt.Printf("applied %d migration(s)\n", len(results))
		return nil
	}
	results, err := provider.Status(ctx)
	if err != nil {
		return fmt.Errorf("migration status failed")
	}
	for _, r := range results {
		fmt.Printf("%s %s\n", r.Source.Path, r.State)
	}
	return nil
}

// outOfOrder is goose's refusal of a migration numbered below the database's highest applied version; it carries version numbers only.
var outOfOrder = regexp.MustCompile(`detected [0-9]+ missing \(out-of-order\) migrations? lower than database version \([0-9]+\): versions? [0-9, ]+`)

// upFailure explains a failed "up" without echoing driver text, which may name the host and user. It says which migration failed,
// or that a numbering rule was broken, because those are what the operator has to act on.
func upFailure(err error) error {
	var partial *goose.PartialError
	switch {
	case errors.As(err, &partial) && partial.Failed != nil:
		return fmt.Errorf("migration %d failed (no connection details logged); review migration and database state", partial.Failed.Source.Version)
	case outOfOrder.MatchString(err.Error()):
		return fmt.Errorf("migration refused: %s; a migration numbered below the highest applied version is never applied (docs/migration/LINE_INTEGRATION_PLAN.md, migration numbers)", outOfOrder.FindString(err.Error()))
	}
	return fmt.Errorf("migration failed (no connection details logged); review migration and database state")
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
