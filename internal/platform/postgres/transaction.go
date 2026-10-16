package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
)

// Executor is the repository port. It deliberately exposes no commit, rollback,
// or raw-connection methods. SQL remains a trusted-Go boundary, not a capability
// sandbox: callbacks must never execute transaction-control or role-changing SQL.
// Customer metadata/AI never supplies SQL to this interface.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

var ErrUnavailable = errors.New("database transaction unavailable")
var ErrCommitUnknown = errors.New("database commit outcome is unconfirmed")

type executor struct{ tx *sql.Tx }

// RuntimeSnapshot identifies the transaction-bound port created by WithRuntime.
// An isolation setting alone cannot prove a shared snapshot: an autocommit DB
// pool may also default to REPEATABLE READ across separate transactions.
// Opaque/decorated executors are deliberately not certified by this proof.
func RuntimeSnapshot(q Executor) bool {
	e, ok := q.(executor)
	return ok && e.tx != nil
}

func (e executor) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return e.tx.ExecContext(ctx, q, args...)
}
func (e executor) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return e.tx.QueryContext(ctx, q, args...)
}
func (e executor) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return e.tx.QueryRowContext(ctx, q, args...)
}

const clearContext = `SELECT pg_catalog.set_config('app.auth_issuer','',false),
 pg_catalog.set_config('app.auth_subject','',false),pg_catalog.set_config('app.actor_id','',false),
 pg_catalog.set_config('app.tenant_id','',false),pg_catalog.set_config('app.job_id','',false),
 pg_catalog.set_config('app.dataset_id','',false),pg_catalog.set_config('app.dataset_binding','',false),
 pg_catalog.set_config('app.execution_ir_hash','',false),pg_catalog.set_config('app.search_text','',false)`

// WithRuntime leases one connection, checks THAT connection's effective role,
// resets persistent identity context, and owns a bounded transaction. The caller
// must buffer results until this function succeeds. No automatic retry occurs.
func WithRuntime(parent context.Context, db *sql.DB, run func(context.Context, Executor) error) (result error) {
	if db == nil || run == nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	ctx, span := otel.Tracer("framework/postgres").Start(ctx, "postgres.transaction")
	defer span.End()
	defer func() {
		if value := recover(); value != nil {
			span.SetStatus(codes.Error, "transaction panicked")
			panic(value)
		}
		if result != nil {
			span.SetStatus(codes.Error, "transaction failed")
		}
	}()
	poolCtx, poolSpan := otel.Tracer("framework/postgres").Start(ctx, "postgres.connection.acquire")
	conn, err := db.Conn(poolCtx)
	poolSpan.End()
	if err != nil {
		return ErrUnavailable
	}
	defer conn.Close()
	// Cleanup has its own deadline: request cancellation must not return a polluted
	// connection to the pool. If reset fails, database/sql discards the connection.
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_, roleErr := conn.ExecContext(cleanup, `RESET ROLE`)
		_, clearErr := conn.ExecContext(cleanup, clearContext)
		if roleErr != nil || clearErr != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	admissionCtx, admissionSpan := otel.Tracer("framework/postgres").Start(ctx, "postgres.role.admission")
	err = checkRuntime(admissionCtx, conn)
	admissionSpan.End()
	if err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		return ErrUnavailable
	}
	if _, err = conn.ExecContext(ctx, clearContext); err != nil {
		return ErrUnavailable
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback() // Also runs during panic unwinding, before connection cleanup.
	if _, err = tx.ExecContext(ctx, `SELECT pg_catalog.set_config('search_path','pg_catalog',true),
 pg_catalog.set_config('statement_timeout','2000',true),pg_catalog.set_config('lock_timeout','500',true),
 pg_catalog.set_config('idle_in_transaction_session_timeout','3000',true)`); err != nil {
		return ErrUnavailable
	}
	err = func() error {
		bodyCtx, bodySpan := otel.Tracer("framework/postgres").Start(ctx, "postgres.transaction.body")
		defer bodySpan.End()
		return run(bodyCtx, executor{tx})
	}()
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ErrUnavailable
	}
	_, commitSpan := otel.Tracer("framework/postgres").Start(ctx, "postgres.transaction.commit")
	err = tx.Commit()
	commitSpan.End()
	if err != nil {
		// PostgreSQL's serialization/deadlock reply proves that COMMIT aborted.
		// A lost reply, connection error or cancellation proves no such outcome.
		if KnownAbort(err) {
			return err
		}
		return ErrCommitUnknown
	}
	return nil
}
