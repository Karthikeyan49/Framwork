// iam is an explicit operator CLI, never part of the runtime or AI tool catalog.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func run() error {
	issuer := flag.String("issuer", "", "Verified identity issuer")
	subject := flag.String("subject", "", "Stable identity provider subject, not email")
	name := flag.String("name", "", "User display name")
	tenantID := flag.String("tenant", "", "Tenant UUID")
	tenantName := flag.String("tenant-name", "", "Tenant display name")
	role := flag.String("role", "", "Explicit owner/admin/member/viewer role")
	status := flag.String("user-status", "", "Explicit active/disabled status")
	membership := flag.String("membership-status", "", "Explicit active/revoked status")
	operator := flag.String("operator", "", "Operator change/ticket reference (no secrets)")
	flag.Parse()
	if flag.NArg() != 0 || *operator == "" || len(*operator) > 64 || (*status != "active" && *status != "disabled") || (*membership != "active" && *membership != "revoked") {
		return fmt.Errorf("explicit flags and operator reference are required; see -help")
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("operator database unavailable")
	}
	defer tx.Rollback()
	userID, err := (identity.Store{}).Provision(ctx, tx, *issuer, *subject, *name, *status == "active")
	if err != nil {
		return fmt.Errorf("user provisioning failed")
	}
	if err = (tenant.Store{}).Provision(ctx, tx, *tenantID, *tenantName, userID, *role, *membership == "active"); err != nil {
		return fmt.Errorf("membership provisioning failed; validate values and retain a tenant owner")
	}
	if *status == "disabled" {
		if err = (tenant.Store{}).CheckDisable(ctx, tx, userID); err != nil {
			return fmt.Errorf("transfer all active ownership before disabling a user")
		}
	}
	evidence := audit.Entry{TenantID: *tenantID, Action: "operator.provision", Resource: userID, Allow: true, PolicyVersion: "operator-m1-v1", RequestID: *operator}
	userActive, memberActive := *status == "active", *membership == "active"
	evidence.State = audit.State{OperatorRef: *operator, Role: *role, UserActive: &userActive, MembershipActive: &memberActive}
	if (audit.Postgres{}).Append(ctx, tx, evidence) != nil {
		return fmt.Errorf("audit append failed; provisioning transaction will be rolled back")
	}
	if tx.Commit() != nil {
		return fmt.Errorf("commit outcome could not be confirmed; verify audit evidence and membership state before retrying")
	}
	fmt.Println("Provisioned identity and membership; audit evidence committed.")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
