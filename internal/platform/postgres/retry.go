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
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	for attempt := 0; ; attempt++ {
		err := run(ctx)
		if !KnownAbort(err) || attempt == 2 || ctx.Err() != nil {
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
