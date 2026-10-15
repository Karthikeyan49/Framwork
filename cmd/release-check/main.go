// release-check is an operator-only pre-deployment compatibility gate.
package main

import (
	"context"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"os"
	"time"
)

func run() error {
	raw, e := config.MigrationURL(os.Getenv)
	if e != nil {
		return e
	}
	db, e := postgres.Open(raw)
	if e != nil {
		return e
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return promotion.CheckActiveCompatibility(ctx, db, compiler.SupportedIRVersions())
}
func main() {
	if run() != nil {
		fmt.Fprintln(os.Stderr, "active release compatibility check failed; deployment blocked")
		os.Exit(1)
	}
	fmt.Println("Active release IR compatibility verified; no recompilation performed.")
}
