package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestRetryWithinRepeatsKnownAbortsUpToTheGivenAttempts(t *testing.T) {
	abort := &pgconn.PgError{Code: "40001"}
	for _, attempts := range []int{1, 2, 5} {
		calls := 0
		got := RetryWithin(context.Background(), 2*time.Second, attempts, func(context.Context) error { calls++; return abort }, KnownAbort)
		if got != abort || calls != attempts {
			t.Fatalf("attempts %d: %d calls, error %v", attempts, calls, got)
		}
	}
	calls := 0
	other := errors.New("transport lost")
	if got := RetryWithin(context.Background(), 2*time.Second, 5, func(context.Context) error { calls++; return other }, KnownAbort); got != other || calls != 1 {
		t.Fatalf("an unknown outcome was repeated: %d calls, %v", calls, got)
	}
	calls = 0
	if got := RetryWithin(context.Background(), 2*time.Second, 5, func(context.Context) error {
		calls++
		if calls < 3 {
			return abort
		}
		return nil
	}, KnownAbort); got != nil || calls != 3 {
		t.Fatalf("a success after two aborts: %d calls, %v", calls, got)
	}
}

func TestRetryWithinStopsWhenTheBudgetIsSpent(t *testing.T) {
	abort := &pgconn.PgError{Code: "40001"}
	calls := 0
	started := time.Now()
	got := RetryWithin(context.Background(), 40*time.Millisecond, 1000, func(ctx context.Context) error { calls++; return abort }, KnownAbort)
	if got != abort || calls < 2 || calls > 20 || time.Since(started) > time.Second {
		t.Fatalf("%d calls in %v, error %v: the budget must end the repetition", calls, time.Since(started), got)
	}
}
