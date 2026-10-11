package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migration lock initialization failed")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.Files, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migration provider initialization failed")
	}
	if args[0] == "up" {
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("migration failed (no connection details logged); review migration and database state")
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
