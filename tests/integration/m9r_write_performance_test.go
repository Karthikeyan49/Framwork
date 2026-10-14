//go:build integration && performance

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func TestM9RIndependentWritePerformanceFloor(t *testing.T) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, app := m5Fixture(t)
	rows := make([]data.Record, 32)
	for i := range rows {
		rows[i] = m5Create(t, h, base, token, "Write floor")
	}
	server := httptest.NewServer(h)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var times []float64
	failures := map[int]int{}
	counts := make([]int, 32)
	deadline := time.Now().Add(60 * time.Second)
	for i := range rows {
		key := uuid(t)
		wg.Go(func() {
			<-start
			for time.Now().Before(deadline) {
				// Each intent has a distinct stable UUID; encode an increment in the
				// final UUID bytes while retaining this client's random namespace.
				commandKey := key[:24] + fmt.Sprintf("%012x", counts[i]+1)
				c := data.Command{Key: commandKey, Operation: "update", ID: rows[i].ID, ExpectedVersion: rows[i].Version, Values: m5Values("Write floor")}
				raw, _ := json.Marshal(c)
				req, _ := http.NewRequest("POST", server.URL+base+"/commands", bytes.NewReader(raw))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				begin := time.Now()
				response, err := client.Do(req)
				if err != nil {
					mu.Lock()
					failures[0]++
					mu.Unlock()
					return
				}
				var record data.Record
				decodeErr := json.NewDecoder(response.Body).Decode(&record)
				response.Body.Close()
				elapsed := float64(time.Since(begin).Microseconds()) / 1000
				if response.StatusCode != 200 || decodeErr != nil || record.ID != rows[i].ID || record.Version != rows[i].Version+1 {
					mu.Lock()
					failures[response.StatusCode]++
					mu.Unlock()
					return
				}
				rows[i].Version = record.Version
				counts[i]++
				mu.Lock()
				times = append(times, elapsed)
				mu.Unlock()
			}
		})
	}
	begin := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(begin)
	t.Logf("clients=32 duration=%s visible_errors=%v", elapsed, failures)
	if elapsed < 60*time.Second || len(failures) != 0 {
		t.Fatal("write workload failed", failures)
	}
	p95 := reportLatency(t, "distinct_record_http_write", times, elapsed)
	updates := 0
	for _, n := range counts {
		updates += n
	}
	m9rCommandCounts(t, f, app, 32, updates, updates+32)
	var versionSum int
	if err := f.operator.QueryRow(`SELECT sum(row_version) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&versionSum); err != nil || versionSum != updates+32 {
		t.Fatal("lost updates", versionSum, updates, err)
	}
	if p95 > 500 {
		t.Fatalf("write p95 %.3f ms exceeds unchanged 500 ms gate", p95)
	}
}
