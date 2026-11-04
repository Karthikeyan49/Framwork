package tools

import (
	"sync"
	"time"
)

// maxRateBuckets bounds limiter memory. Keys come from trusted catalog
// definitions (ref@version), so the bound is only reached across many reloads.
const maxRateBuckets = 4096

// rateLimiter is a process-local token bucket per tool ref@version with
// burst equal to the declared rate. Exceeding it never performs I/O.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}
type bucket struct {
	tokens float64
	rate   int
	last   time.Time
}

func newRateLimiter() *rateLimiter { return &rateLimiter{buckets: map[string]*bucket{}} }

func (b *bucket) refill(now time.Time) {
	if elapsed := now.Sub(b.last); elapsed > 0 {
		b.tokens += elapsed.Seconds() * float64(b.rate)
		if b.tokens > float64(b.rate) {
			b.tokens = float64(b.rate)
		}
		b.last = now
	}
}

func (l *rateLimiter) allow(k string, rate int, now time.Time) bool {
	if rate < 1 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[k]
	if !ok {
		if len(l.buckets) >= maxRateBuckets {
			// Full buckets carry no state and can be forgotten safely.
			for name, candidate := range l.buckets {
				candidate.refill(now)
				if candidate.tokens >= float64(candidate.rate) {
					delete(l.buckets, name)
				}
			}
			if len(l.buckets) >= maxRateBuckets {
				return false
			}
		}
		b = &bucket{tokens: float64(rate), rate: rate, last: now}
		l.buckets[k] = b
	}
	if b.rate != rate {
		// A changed declaration never grants a fresh burst.
		b.rate = rate
		if b.tokens > float64(rate) {
			b.tokens = float64(rate)
		}
	}
	b.refill(now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
