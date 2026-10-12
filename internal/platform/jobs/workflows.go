package jobs

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

type WorkflowLease struct {
	Ticket     Ticket
	Generation int64
	Token      string
}

func (t Ticket) TenantID() string { return t.tenant }

// ClaimWorkflow mints a single exact routed ticket from PostgreSQL. It does not
// read workflow definitions or business inputs, and cannot use an app credential.
func ClaimWorkflow(ctx context.Context, db *sql.DB, token string) (WorkflowLease, bool, error) {
	if CheckRole(ctx, db, "framework_worker") != nil {
		return WorkflowLease{}, false, ErrJob
	}
	var out WorkflowLease
	found := false
	err := postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
		e := q.QueryRowContext(ctx, `SELECT tenant_id::text,id::text,ir_hash,generation,token::text FROM control.claim_workflow($1::uuid)`, token).Scan(&out.Ticket.tenant, &out.Ticket.job, &out.Ticket.release, &out.Generation, &out.Token)
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		found = e == nil
		return e
	})
	if err != nil {
		return WorkflowLease{}, false, err
	}
	return out, found, nil
}
