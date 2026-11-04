package access

import (
	"errors"
	"io"
	"sync"
)

// BodyBudget bounds the request-body bytes that serve holds in memory before the database admission slot (independent review F-01 of
// the body pre-read: without a bound, one valid token could make the API hold 1.14 MiB per connection, 1,000 connections 1.15 GiB).
//
// A request books the bytes it HAS RECEIVED, as they arrive, and gives them back when it ends. The first version booked the declared
// Content-Length before the first byte, and the review of that (F-06) showed what it cost: identities without any membership could
// open connections that declared a body and sent none, and eight of them filled the whole budget, so every body-carrying request of
// every tenant was refused for as long as they kept reconnecting. Booking what is really held means a connection that sends nothing
// holds nothing; to keep the budget full an attacker has to transmit the bytes (the total, per read timeout), from as many identities
// as the total allows. A booking that would exceed the share of one verified identity, or the total, is refused and the read stops, so
// the memory in flight is bounded by the total whatever the number of connections, and one identity cannot starve the others beyond
// its share. The identity is the verified issuer and subject; the bearer was verified, nothing else (membership, role) is known before
// the database is reached.
type BodyBudget struct {
	mu         sync.Mutex
	total      int64
	perSubject int64
	used       int64
	bySubject  map[string]int64
}

// The defaults: the share of one identity is above the largest body the front door admits (1 MiB), so an honest upload is always
// admitted, and the total allows 16 identities to hold a share each.
const (
	DefaultBodyBudgetBytes   int64 = 32 << 20
	DefaultBodyBudgetPerUser int64 = 2 << 20
)

// NewBodyBudget returns a budget of total bytes in flight, at most perSubject of them for one identity. Keep perSubject above the
// largest body the front door admits (1 MiB), or a body of that size can never be read.
func NewBodyBudget(total, perSubject int64) *BodyBudget {
	return &BodyBudget{total: total, perSubject: perSubject, bySubject: map[string]int64{}}
}

// Refusal scopes of Add.
const (
	bodyRefusedSubject = "subject"
	bodyRefusedTotal   = "total"
)

// BodyHold is the bytes one request holds. It grows with Add and is given back whole by Release.
type BodyHold struct {
	b        *BodyBudget
	subject  string
	n        int64
	released bool
}

// Hold starts an empty hold for subject. The caller owns it: one goroutine, Release when the request ends.
func (b *BodyBudget) Hold(subject string) *BodyHold { return &BodyHold{b: b, subject: subject} }

// Add books n more bytes (n <= 0 books nothing). It returns "" when they fit, otherwise the scope that is exhausted, "subject" or
// "total", and then books nothing.
func (h *BodyHold) Add(n int64) (refused string) {
	if n <= 0 || h.released {
		return ""
	}
	b := h.b
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bySubject[h.subject]+n > b.perSubject {
		return bodyRefusedSubject
	}
	if b.used+n > b.total {
		return bodyRefusedTotal
	}
	b.used += n
	b.bySubject[h.subject] += n
	h.n += n
	return ""
}

// Release gives every byte of the hold back. It is safe to call more than once.
func (h *BodyHold) Release() {
	if h.released {
		return
	}
	h.released = true
	b := h.b
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= h.n
	if left := b.bySubject[h.subject] - h.n; left > 0 {
		b.bySubject[h.subject] = left
	} else {
		delete(b.bySubject, h.subject)
	}
	h.n = 0
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

// errBodyBudget stops a read whose bytes do not fit the budget.
var errBodyBudget = errors.New("access: request body budget exhausted")

// bookedBody books the bytes of a request body against its hold as they are read. The bytes of the read that did not fit are dropped,
// never reported to the caller, and refused names the exhausted scope.
type bookedBody struct {
	src     io.Reader
	hold    *BodyHold
	refused string
}

func (b *bookedBody) Read(p []byte) (int, error) {
	n, err := b.src.Read(p)
	if n > 0 {
		if refused := b.hold.Add(int64(n)); refused != "" {
			b.refused = refused
			return 0, errBodyBudget
		}
	}
	return n, err
}
