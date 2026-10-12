package jobs

import (
	"context"
	"database/sql"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// EventTicket resolves routing facts with the worker credential. Broker-supplied
// tenant identity alone never authorizes a job; the exact persisted attempt must match.
func EventTicket(ctx context.Context, db *sql.DB, tenant, event, hash, consumer string, generation int64, token string) (Ticket, bool, bool, bool, error) {
	if CheckRole(ctx, db, "framework_worker") != nil {
		return Ticket{}, false, false, false, ErrJob
	}
	var release string
	var completed, ready, dead bool
	err := postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
		return q.QueryRowContext(ctx, `SELECT release_hash,completed,ready,dead FROM control.resolve_event_v2($1::uuid,$2::uuid,$3,$4,$5,$6::uuid)`, tenant, event, hash, consumer, generation, token).Scan(&release, &completed, &ready, &dead)
	})
	if err != nil {
		return Ticket{}, false, false, false, err
	}
	return Ticket{job: event, tenant: tenant, release: release}, completed, ready, dead, nil
}
