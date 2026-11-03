package observability

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// redisWarningEvery is the least time between two warnings about the Redis client.
const redisWarningEvery = 10 * time.Second

// redisLogger is what the Redis client library logs through. Its own default writes lines such as
// "redis: connection pool: failed to dial after 5 attempts: dial tcp <host>:<port>: connect: connection refused" to standard error,
// outside the structured log, with the dial target and the driver's error text, and any caller that can reach /readyz can make it do so
// (LE-02). Here the library's text is dropped and the operator gets one fixed warning, at most one every ten seconds: the failure itself
// is reported by the readiness probe and by the error that the failing call returns, in the redacted words of the code that made it.
type redisLogger struct {
	logger *slog.Logger
	last   atomic.Int64 // Unix nanoseconds of the last warning
}

// Printf matches the logger interface of the Redis client library; the format and its values are never read.
func (l *redisLogger) Printf(context.Context, string, ...interface{}) {
	if l.logger == nil {
		return
	}
	now := time.Now().UnixNano()
	if last := l.last.Load(); now-last >= int64(redisWarningEvery) && l.last.CompareAndSwap(last, now) {
		l.logger.Warn("redis client reported a problem")
	}
}
