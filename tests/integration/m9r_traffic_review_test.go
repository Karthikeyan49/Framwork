//go:build integration

package integration

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/platform/traffic"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func TestM9RTrafficReviewKnownAbortRechecksAndRejectedCommandHasNoEffects(t *testing.T) {
	m8Database(t)
	f, _, base, owner, app := m5Fixture(t)
	scalarReviewTarget(t, f)
	auth := &r7Auth{base: f.verifier}
	writer := &m9rAbortAudit{}
	h := r7Handler(t, f, r7Gate(t, traffic.Limits{PeerRate: 1, PeerBurst: 1, GlobalRate: 1, GlobalBurst: 1}), auth, writer, nil)
	command := data.Command{Key: uuid(t), Operation: "create", Values: m5Values("Reviewed single commit")}
	if w := m5Call(h, base+"/commands", owner, command); w.Code != 200 || !writer.once.Load() || auth.calls.Load() != 3 {
		t.Fatal("known-abort retry failed to reverify without readmitting", w.Code, auth.calls.Load())
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
	r := httptest.NewRequest("POST", base+"/commands", r7UnreadBody{})
	r.Header.Set("Authorization", "Bearer "+owner)
	r.Header.Set("Content-Type", "application/json")
	r.ContentLength = 1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || auth.calls.Load() != 3 {
		t.Fatal("exhausted request consumed crypto or retried command", w.Code)
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
}

func TestM9RTrafficReviewQueuedCancellationCannotAuditOrRetainLease(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	scalarReviewTarget(t, f)
	gate, err := postgres.NewAdmission(1, 2)
	if err != nil {
		t.Fatal("review database gate unavailable")
	}
	release, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal("review database lease unavailable")
	}
	defer release()
	auth := &r7Auth{base: f.verifier, first: make(chan struct{})}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.DefaultLimits()), auth, writer, gate)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest("GET", "/v1/session", nil).WithContext(ctx)
	r.Header.Set("Authorization", "Bearer "+f.token(t, f.aliceSubject, nil))
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.ServeHTTP(w, r); close(done) }()
	select {
	case <-auth.first:
	case <-time.After(3 * time.Second):
		t.Fatal("review request did not preverify")
	}
	deadline := time.Now().Add(time.Second)
	for gate.Stats().Queued != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gate.Stats().Queued != 1 {
		t.Fatal("review request was not queued")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled review request did not complete")
	}
	if w.Code != 503 || auth.calls.Load() != 1 || writer.calls.Load() != 0 || gate.Stats().Queued != 0 || gate.Stats().Active != 1 {
		t.Fatal("canceled queued request reached database work or retained a queue slot", w.Code)
	}
	release()
	if gate.Stats().Active != 0 {
		t.Fatal("canceled request leaked database lease")
	}
}

func TestM9RTrafficReviewParallelInvalidBurstIsExactlyBounded(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	scalarReviewTarget(t, f)
	auth := &r7Auth{base: f.verifier}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.DefaultLimits()), auth, writer, nil)
	responses := make(chan int, 100)
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { responses <- r7Call(h, "/v1/session", "review-invalid", "192.0.2.77:10", "203.0.113.1").Code })
	}
	wg.Wait()
	close(responses)
	var admitted, rejected int
	for status := range responses {
		switch status {
		case 401:
			admitted++
		case 429:
			rejected++
		default:
			t.Fatal("unexpected parallel response", status)
		}
	}
	if admitted != 20 || rejected != 80 || writer.calls.Load() != 20 || auth.calls.Load() != 100 {
		t.Fatal("parallel invalid budget or audit count is not exact", admitted, rejected)
	}
}
