package traffic

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type reviewMetric struct {
	metric.Int64Counter
	mu   sync.Mutex
	sets []attribute.Set
	bad  bool
}

func (m *reviewMetric) Add(_ context.Context, value int64, options ...metric.AddOption) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value != 1 {
		m.bad = true
	}
	m.sets = append(m.sets, metric.NewAddConfig(options).Attributes())
}

func TestReviewAllRejectionMetricsUseOnlyClosedReasonAndPolicy(t *testing.T) {
	m := &reviewMetric{}
	attach := func(l Limits) *Gate { g, _ := testGate(t, l); g.rejected = m; return g }
	g := attach(Limits{1, 1, 100, 200})
	g.Peer(testContext, "192.0.2.1:1")
	g.Peer(testContext, "192.0.2.1:2") // peer
	g.Subject(testContext, "private-issuer", "private-subject")
	g.Subject(testContext, "private-issuer", "private-subject") // subject
	h := attach(Limits{1, 1, 1, 1})
	h.Peer(testContext, "192.0.2.2:1")
	h.Peer(testContext, "192.0.2.3:2") // global
	for range 20 {
		g.Invalid(testContext, "192.0.2.1:1")
	}
	g.Invalid(testContext, "192.0.2.1:2") // invalid_peer
	for range 20 {
		g.Invalid(testContext, "192.0.2.2:1")
	}
	for range 10 {
		g.Invalid(testContext, "192.0.2.3:1")
	}
	g.Invalid(testContext, "192.0.2.4:1") // invalid_global
	full := attach(Limits{10000, 20000, 10000, 20000})
	for i := range maxKeys {
		full.Peer(testContext, fmt.Sprintf("[2001:db8::%x]:1", i))
		full.Subject(testContext, "private-issuer", fmt.Sprint(i))
	}
	full.Peer(testContext, "192.0.2.10:1")
	full.Subject(testContext, "private-issuer", "overflow")
	closed := map[string]bool{"peer": false, "global": false, "subject": false, "invalid_peer": false, "invalid_global": false, "peer_capacity": false, "subject_capacity": false}
	if m.bad || len(m.sets) != len(closed) {
		t.Fatal("rejection counter did not measure exactly one per rejection")
	}
	for _, set := range m.sets {
		if set.Len() != 2 {
			t.Fatal("unexpected metric label cardinality")
		}
		for _, item := range set.ToSlice() {
			switch item.Key {
			case "reason":
				if _, exists := closed[item.Value.AsString()]; !exists {
					t.Fatal("metric leaked an unbounded reason")
				}
				closed[item.Value.AsString()] = true
			case "policy":
				if item.Value.AsString() != Version {
					t.Fatal("metric policy not fixed")
				}
			default:
				t.Fatal("request-controlled metric label")
			}
		}
	}
	for _, seen := range closed {
		if !seen {
			t.Fatal("review did not execute a closed rejection reason")
		}
	}
}

func TestReviewRepeatedExpiryKeepsPoolStorageBoundedAndActiveDebt(t *testing.T) {
	g, now := testGate(t, Limits{10000, 20000, 10000, 20000})
	for cycle := range 4 {
		for i := range maxKeys {
			if !g.Subject(testContext, fmt.Sprint(cycle), fmt.Sprint(i)).Allowed() {
				t.Fatal("reclaimed subject capacity remained exhausted")
			}
		}
		if len(g.subjects.entries) != maxKeys || len(g.subjects.slots) > maxKeys || len(g.subjects.free) > maxKeys {
			t.Fatal("subject expiry grew storage")
		}
		*now = now.Add(idleTTL + time.Nanosecond)
	}
	limited, clock := testGate(t, Limits{1, 1, 100, 200})
	if !limited.Subject(testContext, "issuer", "actor").Allowed() {
		t.Fatal("initial subject admission")
	}
	for range 100 {
		if limited.Subject(testContext, "issuer", "actor").Allowed() {
			t.Fatal("active rejected subject reset its debt")
		}
	}
	*clock = clock.Add(500 * time.Millisecond)
	if limited.Subject(testContext, "issuer", "actor").Allowed() {
		t.Fatal("fractional subject refill gave free token")
	}
}
