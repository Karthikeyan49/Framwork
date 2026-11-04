package jobs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// DispatchWorkflow commits a lease before execution. No broker or business data
// port is needed: workers poll their authenticated PostgreSQL audience mailbox.
func DispatchWorkflow(ctx context.Context, db *sql.DB) (bool, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return false, err
	}
	token := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	_, found, err := ClaimWorkflow(ctx, db, token)
	return found, err
}
func RunWorkflowDispatcher(ctx context.Context, db *sql.DB, logger *slog.Logger) {
	for ctx.Err() == nil {
		found, err := DispatchWorkflow(ctx, db)
		if err != nil && ctx.Err() == nil {
			logger.Warn("workflow dispatch deferred")
		}
		if !found || err != nil {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
			case <-timer.C:
			}
		}
	}
}
