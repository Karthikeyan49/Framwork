package access

import "sync"

// BodyBudget bounds the request-body bytes that serve buffers before the database admission slot (independent review F-01 of the
// body pre-read: without a bound, one valid token could make the API hold 1.14 MiB per connection, 1,000 connections 1.15 GiB).
//
// A request books the most it can buffer, its declared length capped at the front door's 1 MiB, BEFORE its first byte is read and
// gives the bytes back when it ends. A booking that would exceed the share of one verified identity, or the total, is refused
// without reading anything, so the memory in flight is bounded by the total whatever the number of connections, and one identity
// cannot starve the others beyond its share. The identity is the verified issuer and subject; the bearer was verified, nothing
// else (membership, role) is known before the database is reached.
type BodyBudget struct {
	mu         sync.Mutex
	total      int64
	perSubject int64
	used       int64
	bySubject  map[string]int64
}

// The defaults: a request books its declared length, so ordinary calls (a few hundred bytes to a few hundred KiB) book little and
// the total allows hundreds of them; four 1 MiB bodies per identity is far above any honest use of this API.
const (
	DefaultBodyBudgetBytes   int64 = 32 << 20
	DefaultBodyBudgetPerUser int64 = 4 << 20
)

// NewBodyBudget returns a budget of total bytes in flight, at most perSubject of them for one identity. A request that books more
// than perSubject is always refused, so keep perSubject above the largest body the front door admits (1 MiB).
func NewBodyBudget(total, perSubject int64) *BodyBudget {
	return &BodyBudget{total: total, perSubject: perSubject, bySubject: map[string]int64{}}
}

// Refusal scopes of Reserve.
const (
	bodyRefusedSubject = "subject"
	bodyRefusedTotal   = "total"
)

// Reserve books n bytes (at least one) for subject. On success it returns the release function (safe to call more than once) and
// an empty refusal; otherwise it returns no release and the scope that is exhausted: "subject" or "total".
func (b *BodyBudget) Reserve(subject string, n int64) (release func(), refused string) {
	if n < 1 {
		n = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bySubject[subject]+n > b.perSubject {
		return nil, bodyRefusedSubject
	}
	if b.used+n > b.total {
		return nil, bodyRefusedTotal
	}
	b.used += n
	b.bySubject[subject] += n
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.used -= n
			if left := b.bySubject[subject] - n; left > 0 {
				b.bySubject[subject] = left
			} else {
				delete(b.bySubject, subject)
			}
		})
	}, ""
}

// InFlight reports the booked bytes and the number of identities that hold a booking.
func (b *BodyBudget) InFlight() (bytes int64, subjects int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used, len(b.bySubject)
}

// defaultBodies is the budget of an API whose Bodies is nil: one per process, because one process runs one API.
var defaultBodies = NewBodyBudget(DefaultBodyBudgetBytes, DefaultBodyBudgetPerUser)

func (a API) bodies() *BodyBudget {
	if a.Bodies != nil {
		return a.Bodies
	}
	return defaultBodies
}

// reservation is what a request books: its declared length when that is below the cap (the server never delivers more), else
// the cap (an undeclared length, as with chunked bodies, or one above it, which the front door cuts at the cap).
func reservation(contentLength, limit int64) int64 {
	if contentLength >= 0 && contentLength < limit {
		return max(contentLength, 1)
	}
	return limit
}
