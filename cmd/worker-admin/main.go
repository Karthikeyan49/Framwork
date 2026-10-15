// worker-admin assigns an independently provisioned DB principal to one tenant.
// Operator-only; runtime processes never receive this credential.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"os"
	"time"
)

func run() error {
	principal := flag.String("principal", "", "DB LOGIN role provisioned by the operator")
	tenantID := flag.String("tenant", "", "assigned tenant UUID")
	disable := flag.Bool("disable", false, "revoke future job bindings for this assignment")
	operator := flag.String("operator", "", "reviewed change reference, no secrets")
	check := flag.Bool("check", false, "check the current worker binding without operator privileges")
	flag.Parse()
	if *check {
		if flag.NArg() != 0 || *principal != "" || *tenantID != "" || *operator != "" || *disable {
			return errors.New("check accepts no assignment flags")
		}
		db, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
		if err != nil {
			return err
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if jobs.CheckRole(ctx, db, "framework_worker") != nil {
			return errors.New("worker audience is unavailable or unassigned")
		}
		return nil
	}
	if len(*principal) < 1 || len(*principal) > 63 || !tenant.ValidID(*tenantID) || len(*operator) < 1 || len(*operator) > 64 || flag.NArg() != 0 {
		return errors.New("explicit principal and tenant required")
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
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("operator transaction unavailable")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT control.assign_worker($1,$2::uuid,$3)`, *principal, *tenantID, !*disable); err != nil {
		return errors.New("worker assignment denied; inspect operator role and tenant assignment")
	}
	if err = (audit.Postgres{}).Append(ctx, tx, audit.Entry{TenantID: *tenantID, Action: "operator.worker_assignment", Resource: *principal, Allow: true, PolicyVersion: "operator-m9r-v1", RequestID: *operator, State: audit.State{OperatorRef: *operator}}); err != nil {
		return errors.New("worker assignment audit failed; transaction rolled back")
	}
	if tx.Commit() != nil {
		return errors.New("assignment commit outcome unconfirmed; inspect assignment and audit before retrying")
	}
	fmt.Println("Worker assignment recorded; existing business records preserved.")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
