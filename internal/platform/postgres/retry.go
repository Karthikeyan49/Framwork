package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrSnapshotAborted requires the caller to roll back: a concurrent initializer
// committed an anchor outside this repeatable-read snapshot. No effect committed.
var ErrSnapshotAborted = errors.New("transaction snapshot must be restarted")

// KnownAbort is deliberately narrow. Transport errors and unknown COMMIT outcomes
// never authorize repeating an effect. A receipt-key collision is safe only after
// its enclosing transaction has rolled back, including the preceding mutation.
func KnownAbort(err error) bool {
	if errors.Is(err, ErrSnapshotAborted) {
		return true
	}
	var p *pgconn.PgError
	return errors.As(err, &p) && (p.Code == "40001" || p.Code == "40P01" ||
		p.Code == "23505" && p.SchemaName == "runtime" && p.TableName == "command_receipts" && p.ConstraintName == "command_receipts_pkey")
}

// RetryLocal owns a four-second total budget and at most three attempts. Use it
// only for buffered local database operations with no external side effects.
// Each attempt must own/finish its transaction and resolve live authorization.
func RetryLocal(parent context.Context, run func(context.Context) error) error {
	return RetryLocalWhen(parent, run, KnownAbort)
}

// RetryLocalWhen is RetryLocal with the caller's predicate for the failures that are known to
// have aborted the attempt without effect and that the operation may repeat. The budget and the
// number of attempts are the same; the predicate must be as narrow as KnownAbort.
func RetryLocalWhen(parent context.Context, run func(context.Context) error, retriable func(error) bool) error {
	return RetryWithin(parent, 4*time.Second, 3, run, retriable)
}

// RetryWithin is RetryLocalWhen with the caller's total budget and number of attempts, for an operation whose conflicts are
// frequent enough that three tries do not settle them (concurrent writers of one row under repeatable read: each round lets at
// least one of them through) and whose result is worth more tries. The rules are the same: every attempt owns and finishes its
// transaction, resolves live authorization and has no external side effect, and the predicate must be as narrow as KnownAbort.
// The budget bounds the waiting, not an attempt that is running: its context is the one the attempt is given.
func RetryWithin(parent context.Context, budget time.Duration, attempts int, run func(context.Context) error, retriable func(error) bool) error {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	for attempt := 1; ; attempt++ {
		err := run(ctx)
		if !retriable(err) || attempt >= attempts || ctx.Err() != nil {
			return err
		}
		n, e := rand.Int(rand.Reader, big.NewInt(21))
		if e != nil {
			return err
		}
		timer := time.NewTimer(time.Duration(5+n.Int64()) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
	}
}
