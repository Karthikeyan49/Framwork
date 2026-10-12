// Package jobs binds worker transactions to server-resolved routing tickets.
// It is not a workflow scheduler or queue implementation.
package jobs

import (
	"context"
	"database/sql"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

var ErrJob = errors.New("worker job denied")

type Ticket struct{ job, tenant, release string }

// FindRoute avoids assuming that a bounded routing page contains a particular
// job. It remains dispatcher-only and returns no business payload.
func FindRoute(ctx context.Context, db *sql.DB, tenantID, id string) (Ticket, error) {
	if CheckRole(ctx, db, "framework_dispatcher") != nil {
		return Ticket{}, ErrJob
	}
	var t Ticket
	err := postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
		return q.QueryRowContext(ctx, `SELECT id::text,tenant_id::text,release_hash FROM control.job_routes WHERE tenant_id=$1::uuid AND id=$2::uuid`, tenantID, id).Scan(&t.job, &t.tenant, &t.release)
	})
	return t, err
}

func (t Ticket) JobID() string       { return t.job }
func (t Ticket) ReleaseHash() string { return t.release }
func CheckRole(ctx context.Context, db *sql.DB, expected string) error {
	if db == nil {
		return ErrJob
	}
	var role string
	if e := db.QueryRowContext(ctx, `SELECT session_user`).Scan(&role); e != nil || role != expected {
		return ErrJob
	}
	return postgres.Ping(ctx, db)
}

// Routes uses the dispatcher credential, which cannot access business payloads.
func Routes(ctx context.Context, db *sql.DB) ([]Ticket, error) {
	if CheckRole(ctx, db, "framework_dispatcher") != nil {
		return nil, ErrJob
	}
	out := []Ticket{}
	e := postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
		rows, e := q.QueryContext(ctx, `SELECT id::text,tenant_id::text,release_hash FROM control.job_routes ORDER BY tenant_id,id LIMIT 100`)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var t Ticket
			if e = rows.Scan(&t.job, &t.tenant, &t.release); e != nil {
				return e
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, e
}
func WithJob(ctx context.Context, db *sql.DB, t Ticket, run func(context.Context, postgres.Executor, tenant.Access) error) error {
	if t.job == "" || t.tenant == "" || t.release == "" || run == nil || CheckRole(ctx, db, "framework_worker") != nil {
		return ErrJob
	}
	return postgres.WithRuntime(ctx, db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.job_id',$1,true),set_config('app.tenant_id',$2,true)`, t.job, t.tenant); e != nil {
			return e
		}
		u, e := (identity.Store{}).ResolveWorker(ctx, q, t.job)
		if e != nil {
			return ErrJob
		}
		if e = (tenant.Store{}).BindActor(ctx, q, u); e != nil {
			return e
		}
		a, e := (tenant.Store{}).ResolveWorker(ctx, q, u, t.tenant)
		if e != nil {
			return ErrJob
		}
		// Routing changes invalidate an old ticket; no implicit release switching.
		var hash string
		if e = q.QueryRowContext(ctx, `SELECT release_hash FROM control.job_routes WHERE tenant_id=$1::uuid AND id=$2::uuid`, t.tenant, t.job).Scan(&hash); e != nil || hash != t.release {
			return ErrJob
		}
		return run(ctx, q, a)
	})
}
