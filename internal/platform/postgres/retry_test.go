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

// ER-04 (independent edge rehearsal): the result exchange of a paid AI answer was refused with a bare 503 when the admission queue was
// full at the moment it arrived, and the tries of RetryWithin (5 to 25 ms apart) end long before a queue drains. RetryPaced spaces them
// as the caller says.
func TestRetryPacedWaitsAsLongAsThePauseSays(t *testing.T) {
	again := errors.New("try again")
	calls := 0
	started := time.Now()
	pause := func(attempt int, err error) (time.Duration, bool) {
		if err != again {
			t.Errorf("the pause was given %v", err)
		}
		return time.Duration(attempt) * 30 * time.Millisecond, true // 30 ms after the first try, 60 ms after the second
	}
	got := RetryPaced(context.Background(), 5*time.Second, 3, pause, func(context.Context) error { calls++; return again }, func(err error) bool { return err == again })
	if got != again || calls != 3 || time.Since(started) < 90*time.Millisecond {
		t.Fatalf("%d calls in %v, error %v: three tries with pauses of 30 and 60 ms", calls, time.Since(started), got)
	}
	// A pause that cannot be drawn ends the repetition with the error in hand, as RetryWithin always did.
	calls = 0
	if got := RetryPaced(context.Background(), time.Second, 5, func(int, error) (time.Duration, bool) { return 0, false }, func(context.Context) error { calls++; return again }, func(error) bool { return true }); got != again || calls != 1 {
		t.Fatalf("%d calls, error %v", calls, got)
	}
	// The budget ends a long pause.
	calls = 0
	started = time.Now()
	long := func(int, error) (time.Duration, bool) { return time.Hour, true }
	if got := RetryPaced(context.Background(), 50*time.Millisecond, 5, long, func(context.Context) error { calls++; return again }, func(error) bool { return true }); got != again || calls != 1 || time.Since(started) > time.Second {
		t.Fatalf("%d calls in %v, error %v: the budget must cut a pause short", calls, time.Since(started), got)
	}
}

// The real queue: full, then drained while the exchange waits. RetryWithin gives up long before; RetryPaced gets through.
func TestRetryPacedRidesOutAFullAdmissionQueue(t *testing.T) {
	queue, err := NewAdmission(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	holder, err := queue.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	waiting := make(chan func(), 1)
	go func() { // fills the one place of the queue, and takes the slot when the holder leaves
		release, _ := queue.Acquire(context.Background())
		waiting <- release
	}()
	for queue.Stats().Queued != 1 {
		time.Sleep(time.Millisecond)
	}
	enter := func(ctx context.Context) error {
		release, err := queue.Acquire(ctx)
		if err != nil {
			return err
		}
		release()
		return nil
	}
	full := func(err error) bool { return errors.Is(err, ErrAdmissionFull) }
	if err := enter(context.Background()); !errors.Is(err, ErrAdmissionFull) {
		t.Fatalf("the queue is not full: %v", err)
	}
	// The same call RetryWithin makes: six tries within a few dozen milliseconds, all refused.
	if err := RetryWithin(context.Background(), 2*time.Second, 6, enter, full); !errors.Is(err, ErrAdmissionFull) {
		t.Fatalf("RetryWithin: %v; the queue is still full for it", err)
	}
	go func() { // the queue drains after 250 ms
		time.Sleep(250 * time.Millisecond)
		holder()
		(<-waiting)()
	}()
	pause := func(attempt int, _ error) (time.Duration, bool) {
		return time.Duration(attempt) * 100 * time.Millisecond, true
	}
	if err := RetryPaced(context.Background(), 5*time.Second, 6, pause, enter, full); err != nil {
		t.Fatalf("RetryPaced did not get through a queue that drained after 250 ms: %v", err)
	}
}
