package traffic

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric"
)

var testContext = context.Background()

func testGate(t *testing.T, l Limits) (*Gate, *time.Time) {
	t.Helper()
	now := time.Unix(1000, 0)
	g, err := New(l, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return g, &now
}
func TestRefillFractionalCreditRetryAndClockRegression(t *testing.T) {
	g, now := testGate(t, Limits{2, 4, 100, 200})
	for range 4 {
		if !g.Peer(testContext, "192.0.2.1:1").Allowed() {
			t.Fatal("initial burst")
		}
	}
	d := g.Peer(testContext, "192.0.2.1:2")
	if d.Reason != "peer" || d.RetryAfter != 500*time.Millisecond || d.RetrySeconds() != 1 {
		t.Fatal("fractional retry", d)
	}
	*now = now.Add(250 * time.Millisecond)
	if d = g.Peer(testContext, "192.0.2.1:3"); d.RetryAfter != 250*time.Millisecond {
		t.Fatal("partial refill", d)
	}
	*now = now.Add(-time.Hour)
	if g.Peer(testContext, "192.0.2.1:1").Allowed() {
		t.Fatal("backward clock refilled")
	}
	*now = time.Unix(1000, 0).Add(500 * time.Millisecond)
	if !g.Peer(testContext, "192.0.2.1:1").Allowed() {
		t.Fatal("exact refill")
	}
	if g.Peer(testContext, "[::ffff:192.0.2.1]:88").Allowed() {
		t.Fatal("mapped address bypass")
	}
}
func TestGlobalAndPeerIsolationAndUnknownAddresses(t *testing.T) {
	g, _ := testGate(t, Limits{1, 2, 2, 4})
	for _, addr := range []string{"192.0.2.1:1", "192.0.2.1:2"} {
		if !g.Peer(testContext, addr).Allowed() {
			t.Fatal("initial peer")
		}
	}
	if g.Peer(testContext, "192.0.2.1:3").Reason != "peer" {
		t.Fatal("same IP new port bypass")
	}
	if !g.Peer(testContext, "[2001:db8::1]:1").Allowed() || !g.Peer(testContext, "[2001:db8::2]:2").Allowed() {
		t.Fatal("other peer isolated")
	}
	if g.Peer(testContext, "192.0.2.20:1").Reason != "global" {
		t.Fatal("global bound")
	}
	h, _ := testGate(t, Limits{1, 2, 100, 200})
	for _, remote := range []string{"", "spoof.example", "malformed:peer"} {
		h.Peer(testContext, remote)
	}
	if len(h.peers.entries) != 1 || h.Peer(testContext, "arbitrary-new-text").Allowed() {
		t.Fatal("unknown peers formed fresh buckets")
	}
}
func TestSubjectKeyIsSignedIdentityTupleAndNotPeerOrConcatenation(t *testing.T) {
	g, _ := testGate(t, Limits{1, 2, 100, 200})
	for range 2 {
		if !g.Subject(testContext, "ab", "c").Allowed() {
			t.Fatal("initial subject")
		}
	}
	if g.Subject(testContext, "ab", "c").Reason != "subject" {
		t.Fatal("subject budget")
	}
	if !g.Subject(testContext, "a", "bc").Allowed() || !g.Subject(testContext, "other", "c").Allowed() {
		t.Fatal("ambiguous identity key")
	}
	if len(g.subjects.entries) != 3 {
		t.Fatal("identity separation")
	}
}
func TestInvalidAuthenticationHasIndependentPeerAndGlobalBudgets(t *testing.T) {
	g, now := testGate(t, DefaultLimits())
	for range 20 {
		if !g.Invalid(testContext, "192.0.2.1:1").Allowed() {
			t.Fatal("initial negative audit budget")
		}
	}
	if g.Invalid(testContext, "192.0.2.1:2").Reason != "invalid_peer" {
		t.Fatal("rotated token/port invalid quota")
	}
	for range 20 {
		if !g.Invalid(testContext, "192.0.2.2:1").Allowed() {
			t.Fatal("other peer budget")
		}
	}
	for range 10 {
		if !g.Invalid(testContext, "192.0.2.3:1").Allowed() {
			t.Fatal("global initial budget")
		}
	}
	if g.Invalid(testContext, "192.0.2.4:1").Reason != "invalid_global" {
		t.Fatal("global invalid bound")
	}
	*now = now.Add(time.Second)
	if !g.Invalid(testContext, "192.0.2.1:4").Allowed() || g.Invalid(testContext, "192.0.2.1:5").Allowed() {
		t.Fatal("invalid quota refill")
	}
	if !g.Subject(testContext, "issuer", "valid-subject").Allowed() {
		t.Fatal("invalid-auth floods consumed subject budget")
	}
}
func TestCardinalityIsBoundedAndActiveEntriesAreNotEvicted(t *testing.T) {
	g, now := testGate(t, Limits{10000, 20000, 10000, 20000})
	for i := range maxKeys {
		if !g.Peer(testContext, fmt.Sprintf("[2001:db8::%x]:1", i)).Allowed() {
			t.Fatal("peer capacity early")
		}
		if !g.Subject(testContext, "issuer", fmt.Sprint(i)).Allowed() {
			t.Fatal("subject capacity early")
		}
	}
	if g.Peer(testContext, "192.0.2.1:1").Reason != "peer_capacity" || g.Subject(testContext, "issuer", "overflow").Reason != "subject_capacity" {
		t.Fatal("unbounded keys")
	}
	if len(g.peers.entries) != maxKeys || len(g.peers.slots) != maxKeys || len(g.subjects.entries) != maxKeys || len(g.subjects.slots) != maxKeys {
		t.Fatal("unbounded storage")
	}
	*now = now.Add(idleTTL - time.Second)
	if !g.Peer(testContext, "[2001:db8::1]:1").Allowed() {
		t.Fatal("existing peer starved at cardinality cap")
	}
	*now = now.Add(2 * time.Second)
	if !g.Peer(testContext, "192.0.2.1:1").Allowed() || !g.Subject(testContext, "issuer", "overflow").Allowed() {
		t.Fatal("expired slots not reclaimed")
	}
	if len(g.peers.slots) > maxKeys || len(g.subjects.slots) > maxKeys {
		t.Fatal("slot ring grew")
	}
	if g.peers.entries[peerKey("[2001:db8::1]:1")] == nil {
		t.Fatal("recent entry was evicted")
	}
}
func TestParallelAdmissionCannotOverdrawBurst(t *testing.T) {
	g, _ := testGate(t, Limits{25, 50, 10000, 20000})
	var n atomic.Int64
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() {
			if g.Peer(testContext, "192.0.2.1:1").Allowed() {
				n.Add(1)
			}
		})
	}
	wg.Wait()
	if n.Load() != 50 {
		t.Fatal("parallel burst", n.Load())
	}
}

type panicMetric struct{ metric.Int64Counter }

func (panicMetric) Add(context.Context, int64, ...metric.AddOption) {
	panic("synthetic observer failure")
}
func TestObserverFailureCannotChangeAdmissionOrHoldLock(t *testing.T) {
	g, now := testGate(t, Limits{1, 1, 1, 1})
	g.rejected = panicMetric{}
	if !g.Peer(testContext, "192.0.2.1:1").Allowed() || g.Peer(testContext, "192.0.2.1:1").Allowed() {
		t.Fatal("observer changed rejection")
	}
	*now = now.Add(time.Second)
	if !g.Peer(testContext, "192.0.2.1:1").Allowed() {
		t.Fatal("observer held lock")
	}
}
func TestInvalidConfigurationCannotDisableAdmission(t *testing.T) {
	for _, l := range []Limits{{}, {0, 1, 1, 1}, {1, 3, 1, 3}, {2, 4, 1, 2}, {10001, 1, 10001, 1}, {1, 1, 1, 0}} {
		if _, err := New(l, nil); err == nil {
			t.Fatal("invalid limits accepted")
		}
	}
}
