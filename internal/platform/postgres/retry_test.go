package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestKnownAbortExcludesUnknownOutcomesAndOtherUniqueConstraints(t *testing.T) {
	for _, err := range []error{ErrCommitUnknown, ErrUnavailable, context.DeadlineExceeded, errors.New("transport lost"), &pgconn.PgError{Code: "23505", ConstraintName: "records_pkey"}, &pgconn.PgError{Code: "55P03"}} {
		calls := 0
		got := RetryLocal(context.Background(), func(context.Context) error { calls++; return err })
		if got != err || calls != 1 {
			t.Fatal("unproven effect outcome was repeated")
		}
	}
	for _, err := range []error{ErrSnapshotAborted, &pgconn.PgError{Code: "40001"}, &pgconn.PgError{Code: "40P01"}, &pgconn.PgError{Code: "23505", SchemaName: "runtime", TableName: "command_receipts", ConstraintName: "command_receipts_pkey"}} {
		calls := 0
		got := RetryLocal(context.Background(), func(context.Context) error { calls++; return err })
		if got != err || calls != 3 {
			t.Fatal("known abort retry bound")
		}
	}
}
