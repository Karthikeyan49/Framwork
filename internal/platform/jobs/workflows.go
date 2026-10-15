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

// ClaimWorkflow is dispatcher-only issuance. It cannot read business payloads.
func ClaimWorkflow(ctx context.Context, db *sql.DB, token string) (WorkflowLease, bool, error) {
	if CheckRole(ctx, db, "framework_dispatcher") != nil {
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
	out.Ticket.kind, out.Ticket.generation, out.Ticket.token = "workflow", out.Generation, out.Token
	return out, found, nil
}

// AssignedWorkflow retrieves an existing lease for the authenticated DB audience.
// It cannot issue/renew leases or select another principal's tenant.
func AssignedWorkflow(ctx context.Context, db *sql.DB) (WorkflowLease, bool, error) {
	if CheckRole(ctx, db, "framework_worker") != nil {
		return WorkflowLease{}, false, ErrJob
	}
	var out WorkflowLease
	found := false
	err := postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
		err := q.QueryRowContext(ctx, `SELECT tenant_id::text,id::text,ir_hash,generation,token::text FROM control.assigned_workflow()`).Scan(&out.Ticket.tenant, &out.Ticket.job, &out.Ticket.release, &out.Generation, &out.Token)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		return WorkflowLease{}, false, err
	}
	out.Ticket.kind, out.Ticket.generation, out.Ticket.token = "workflow", out.Generation, out.Token
	return out, found, nil
}
