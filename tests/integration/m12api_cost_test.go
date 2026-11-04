//go:build integration

package integration

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/access"
)

// TestM12APIDetailReadsOfALargeCandidateConverge is the regression test of finding F9.
// A second release of a large application (15 entities x 30 fields, 1000 acceptance
// cases, a bundle of about 600 KB, an active base) used to be restored, verified and
// diffed three times inside every detail read; ten parallel reads by a viewer all ended in
// 503 after the 3 s transaction budget and held every pool connection while they did,
// so that another member's cheap request waited behind them as well. Readers that retry
// after a refusal, as a client honouring Retry-After would, must now all be served, and
// the cheap request must not wait for them.
func TestM12APIDetailReadsOfALargeCandidateConverge(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.m12apiBigRelease("500", true)
	second := e.m12apiBigRelease("600", false)

	var health struct {
		sync.Mutex
		worst  time.Duration
		failed []int
		calls  int
	}
	stop := make(chan struct{})
	var poller sync.WaitGroup
	poller.Add(1)
	go func() {
		defer poller.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			start := time.Now()
			w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/access", e.alice, 0, nil)
			took := time.Since(start)
			health.Lock()
			health.calls++
			if took > health.worst {
				health.worst = took
			}
			if w.Code != 200 {
				health.failed = append(health.failed, w.Code)
			}
			health.Unlock()
			time.Sleep(40 * time.Millisecond)
		}
	}()

	start := time.Now()
	reads := e.m12apiParallelReads(10, 20, e.viewer, "/candidates/"+second, 400*time.Millisecond)
	took := time.Since(start)
	close(stop)
	poller.Wait()

	refusals := 0
	for i, r := range reads {
		refusals += r.refusals
		var v access.CandidateView
		if r.status != 200 || json.Unmarshal([]byte(r.body), &v) != nil || v.ArtifactHash != second || v.Stale == nil || *v.Stale || v.BaseRelease == "" {
			t.Errorf("reader %d ended with %d after %d attempts (%d refused): %.200s", i, r.status, r.attempts, r.refusals, r.body)
		}
	}
	t.Logf("10 parallel detail reads: %v in all, %d refused attempts; concurrent GET /access: %d calls, worst %v", took.Round(time.Millisecond), refusals, health.calls, health.worst.Round(time.Millisecond))
	if len(health.failed) > 0 {
		t.Errorf("a cheap request failed while the reads were running: %v", health.failed)
	}
	if health.worst > 2500*time.Millisecond {
		t.Errorf("a cheap request waited %v behind the detail reads", health.worst)
	}
}
