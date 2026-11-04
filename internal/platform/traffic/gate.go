// Package traffic bounds API work before signature verification and PostgreSQL.
// It is process-local admission, never authentication or tenant authorization.
package traffic

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const Version = "api-admission-v1"
const maxKeys = 8192
const idleTTL = 2 * time.Minute
const cleanupSteps = 16

type Limits struct{ PeerRate, PeerBurst, GlobalRate, GlobalBurst int }

func DefaultLimits() Limits { return Limits{300, 600, 1000, 2000} }
func (l Limits) Validate() error {
	if l.PeerRate < 1 || l.PeerRate > 10000 || l.GlobalRate < 1 || l.GlobalRate > 10000 || l.PeerBurst < 1 || l.PeerBurst > 2*l.PeerRate || l.GlobalBurst < 1 || l.GlobalBurst > 2*l.GlobalRate || l.PeerRate > l.GlobalRate || l.PeerBurst > l.GlobalBurst {
		return errors.New("invalid API request admission bounds")
	}
	return nil
}

type Decision struct {
	Reason     string
	RetryAfter time.Duration
}

func (d Decision) Allowed() bool { return d.Reason == "" }
func (d Decision) RetrySeconds() int64 {
	return max(1, int64((d.RetryAfter+time.Second-1)/time.Second))
}

type bucket struct {
	credit int64
	last   time.Time
}

func full(now time.Time, burst int) bucket { return bucket{int64(burst) * int64(time.Second), now} }
func (b *bucket) ready(now time.Time, rate, burst int) time.Duration {
	elapsed := now.Sub(b.last)
	if elapsed > 0 {
		ceiling := int64(burst) * int64(time.Second)
		if elapsed >= time.Duration(ceiling/int64(rate)+1) {
			b.credit = ceiling
		} else {
			b.credit = min(ceiling, b.credit+int64(elapsed)*int64(rate))
		}
		b.last = now
	}
	if b.credit >= int64(time.Second) {
		return 0
	}
	return time.Duration((int64(time.Second) - b.credit + int64(rate) - 1) / int64(rate))
}
func (b *bucket) take() { b.credit -= int64(time.Second) }

type entry struct {
	main, invalid bucket
	seen          time.Time
}
type slot[K comparable] struct {
	key  K
	used bool
}
type pool[K comparable] struct {
	entries map[K]*entry
	slots   []slot[K]
	free    []int
	scan    int
}

func newPool[K comparable]() pool[K] { return pool[K]{entries: make(map[K]*entry)} }
func (p *pool[K]) get(key K, now time.Time, burst int) *entry {
	for range min(cleanupSteps, len(p.slots)) {
		i := p.scan
		p.scan = (p.scan + 1) % len(p.slots)
		s := p.slots[i]
		if s.used && now.Sub(p.entries[s.key].seen) >= idleTTL {
			delete(p.entries, s.key)
			p.slots[i].used = false
			p.free = append(p.free, i)
		}
	}
	if e := p.entries[key]; e != nil {
		e.seen = now
		return e
	}
	if len(p.entries) >= maxKeys {
		return nil
	}
	e := &entry{full(now, burst), full(now, 20), now}
	p.entries[key] = e
	if len(p.free) > 0 {
		i := p.free[len(p.free)-1]
		p.free = p.free[:len(p.free)-1]
		p.slots[i] = slot[K]{key, true}
	} else {
		p.slots = append(p.slots, slot[K]{key, true})
	}
	return e
}

type Gate struct {
	mu              sync.Mutex
	limits          Limits
	clock           func() time.Time
	last            time.Time
	global, invalid bucket
	peers           pool[netip.Addr]
	subjects        pool[[32]byte]
	rejected        metric.Int64Counter
}

func New(limits Limits, clock func() time.Time) (*Gate, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	counter, err := otel.Meter("framework/traffic").Int64Counter("framework.api.admission.rejected", metric.WithDescription("Requests rejected before protected API database work"))
	if err != nil {
		return nil, errors.New("API admission telemetry unavailable")
	}
	now := clock()
	return &Gate{limits: limits, clock: clock, last: now, global: full(now, limits.GlobalBurst), invalid: full(now, 50), peers: newPool[netip.Addr](), subjects: newPool[[32]byte](), rejected: counter}, nil
}
func peerKey(remote string) netip.Addr {
	if ap, err := netip.ParseAddrPort(remote); err == nil {
		return ap.Addr().Unmap().WithZone("")
	}
	return netip.Addr{} // malformed/absent addresses share a single bounded key
}
func (g *Gate) decide(ctx context.Context, run func(time.Time) Decision) Decision {
	g.mu.Lock()
	now := g.clock()
	if now.Before(g.last) {
		now = g.last
	} else {
		g.last = now
	}
	d := run(now)
	g.mu.Unlock()
	if !d.Allowed() {
		g.observe(ctx, d.Reason)
	}
	return d
}

// A failing optional metric observer must not alter an admission decision.
func (g *Gate) observe(ctx context.Context, reason string) {
	defer func() { _ = recover() }()
	g.rejected.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason), attribute.String("policy", Version)))
}
func (g *Gate) Peer(ctx context.Context, remote string) Decision {
	key := peerKey(remote)
	return g.decide(ctx, func(now time.Time) Decision {
		globalWait := g.global.ready(now, g.limits.GlobalRate, g.limits.GlobalBurst)
		if globalWait > 0 {
			return Decision{"global", globalWait}
		}
		e := g.peers.get(key, now, g.limits.PeerBurst)
		if e == nil {
			return Decision{"peer_capacity", time.Second}
		}
		wait := e.main.ready(now, g.limits.PeerRate, g.limits.PeerBurst)
		if wait > 0 {
			return Decision{"peer", wait}
		}
		g.global.take()
		e.main.take()
		return Decision{}
	})
}
func (g *Gate) Invalid(ctx context.Context, remote string) Decision {
	key := peerKey(remote)
	return g.decide(ctx, func(now time.Time) Decision {
		e := g.peers.get(key, now, g.limits.PeerBurst)
		if e == nil {
			return Decision{"peer_capacity", time.Second}
		}
		if wait := e.invalid.ready(now, 1, 20); wait > 0 {
			return Decision{"invalid_peer", wait}
		}
		if wait := g.invalid.ready(now, 10, 50); wait > 0 {
			return Decision{"invalid_global", wait}
		}
		e.invalid.take()
		g.invalid.take()
		return Decision{}
	})
}
func (g *Gate) Subject(ctx context.Context, issuer, subject string) Decision {
	hash := sha256.New()
	var size [8]byte
	for _, part := range []string{issuer, subject} {
		binary.BigEndian.PutUint64(size[:], uint64(len(part)))
		_, _ = hash.Write(size[:])
		_, _ = io.WriteString(hash, part)
	}
	var key [32]byte
	copy(key[:], hash.Sum(nil))
	return g.decide(ctx, func(now time.Time) Decision {
		e := g.subjects.get(key, now, g.limits.PeerBurst)
		if e == nil {
			return Decision{"subject_capacity", time.Second}
		}
		if wait := e.main.ready(now, g.limits.PeerRate, g.limits.PeerBurst); wait > 0 {
			return Decision{"subject", wait}
		}
		e.main.take()
		return Decision{}
	})
}
