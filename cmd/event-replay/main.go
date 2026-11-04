// event-replay is an operator-only recovery command, never a runtime HTTP route.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"os"
	"time"
)

func run() error {
	tenant := flag.String("tenant", "", "tenant UUID")
	event := flag.String("event", "", "event UUID")
	generation := flag.Int64("generation", -1, "expected delivery generation")
	operator := flag.String("operator", "", "reviewed operator reference, no secrets")
	reason := flag.String("reason", "", "reviewed replay reason, no secrets")
	flag.Parse()
	if *generation < 0 || len(*operator) < 1 || len(*operator) > 64 || len(*reason) < 1 || len(*reason) > 200 || flag.NArg() != 0 {
		return errors.New("explicit tenant/event/generation/operator/reason required")
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var ok bool
	if db.QueryRowContext(ctx, `SELECT control.replay_event($1::uuid,$2::uuid,$3,$4,$5)`, *tenant, *event, *generation, *operator, *reason).Scan(&ok) != nil || !ok {
		return errors.New("replay denied; inspect operator state and immutable evidence")
	}
	fmt.Println("Replay recorded; immutable event and inbox preserved.")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
