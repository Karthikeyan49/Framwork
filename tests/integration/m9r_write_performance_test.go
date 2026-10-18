//go:build integration && performance

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type m9rWritePhases struct {
	mu    sync.Mutex
	times map[string][]float64
}

func (*m9rWritePhases) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (*m9rWritePhases) Shutdown(context.Context) error                  { return nil }
func (*m9rWritePhases) ForceFlush(context.Context) error                { return nil }
func (p *m9rWritePhases) OnEnd(s sdktrace.ReadOnlySpan) {
	switch s.Name() {
	case "postgres.request.admission", "postgres.connection.acquire", "postgres.role.admission", "postgres.transaction.body", "postgres.transaction.commit":
		p.mu.Lock()
		if len(p.times[s.Name()]) < 100000 {
			p.times[s.Name()] = append(p.times[s.Name()], float64(s.EndTime().Sub(s.StartTime()).Microseconds())/1000)
		}
		p.mu.Unlock()
	}
}

// Classify failures without exposing SQL, parameters or raw driver error text.
type m9rWriteAuditDiagnostics struct {
	mu     sync.Mutex
	errors map[string]int
}

func (w *m9rWriteAuditDiagnostics) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	return w.recordError((audit.Postgres{}).Append(ctx, q, e))
}
func (w *m9rWriteAuditDiagnostics) AppendBatch(ctx context.Context, q postgres.Executor, entries []audit.Entry) error {
	return w.recordError((audit.Postgres{}).AppendBatch(ctx, q, entries))
}
func (w *m9rWriteAuditDiagnostics) recordError(err error) error {
	if err != nil {
		category := "other"
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			category = "sqlstate_" + pg.Code
		} else if errors.Is(err, context.DeadlineExceeded) {
			category = "deadline"
		} else if errors.Is(err, context.Canceled) {
			category = "canceled"
		}
		w.mu.Lock()
		w.errors[category]++
		w.mu.Unlock()
	}
	return err
}

func TestM9RIndependentWritePerformanceFloor(t *testing.T) {
	runM9RWriteFloor(t, false)
}

func TestM9RUniqueWritePerformanceFloor(t *testing.T) {
	runM9RWriteFloor(t, true)
}

func runM9RWriteFloor(t *testing.T, unique bool) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, app := m5Fixture(t)
	if unique {
		f, h, base, token, app, _ = uniqueFixture(t, 0, false)
		uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	}
	diagnostics := &m9rWriteAuditDiagnostics{errors: map[string]int{}}
	h = r5Handler(t, f, diagnostics)
	rows := make([]data.Record, 32)
	for i := range rows {
		if unique {
			rows[i] = uniqueCreate(t, h, base, token, fmt.Sprintf("initial-%d", i), 200)
		} else {
			rows[i] = m5Create(t, h, base, token, "Write floor")
		}
	}
	server := httptest.NewServer(h)
	defer server.Close()
	phases := &m9rWritePhases{times: map[string][]float64{}}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(phases))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) }()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var times []float64
	var failedTimes []float64
	failures := map[int]int{}
	failedReceipts := map[string]int{}
	committedFailures := 0
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
				if unique {
					c.Values = uniqueValues(fmt.Sprintf("client-%d-step-%d", i, counts[i]+1))
					c.Values["field.contact.name"] = strings.Repeat("Unique representative writer ", 35)
				}
				raw, _ := json.Marshal(c)
				req, _ := http.NewRequest("POST", server.URL+base+"/commands", bytes.NewReader(raw))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				begin := time.Now()
				response, err := client.Do(req)
				if err != nil {
					mu.Lock()
					failures[0]++
					failedTimes = append(failedTimes, float64(time.Since(begin).Microseconds())/1000)
					mu.Unlock()
					return
				}
				var record data.Record
				decodeErr := json.NewDecoder(response.Body).Decode(&record)
				response.Body.Close()
				elapsed := float64(time.Since(begin).Microseconds()) / 1000
				if response.StatusCode != 200 || decodeErr != nil || record.ID != rows[i].ID || record.Version != rows[i].Version+1 {
					// Inspect the owned durable receipt; never repeat the write. Its
					// absence remains UNKNOWN, not a claim that the attempt aborted.
					state := "LOOKUP_UNAVAILABLE"
					lookup, _ := http.NewRequest("GET", server.URL+m9rReceiptPath(f, app, commandKey), nil)
					lookup.Header.Set("Authorization", "Bearer "+token)
					if result, e := client.Do(lookup); e == nil {
						var receipt data.Receipt
						if result.StatusCode == 200 && json.NewDecoder(result.Body).Decode(&receipt) == nil && receipt.Key == commandKey && (receipt.State == "UNKNOWN" || receipt.State == "COMMITTED") {
							state = receipt.State
						}
						result.Body.Close()
					}
					mu.Lock()
					failures[response.StatusCode]++
					failedTimes = append(failedTimes, elapsed)
					failedReceipts[state]++
					if state == "COMMITTED" {
						committedFailures++
					}
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
	stats := f.db.Stats()
	t.Logf("pool_wait_count=%d pool_wait_duration=%s failed_receipt_states=%v audit_error_categories=%v", stats.WaitCount, stats.WaitDuration, failedReceipts, diagnostics.errors)
	for _, name := range []string{"postgres.request.admission", "postgres.connection.acquire", "postgres.role.admission", "postgres.transaction.body", "postgres.transaction.commit"} {
		if samples := phases.times[name]; len(samples) > 0 {
			t.Logf("phase=%s n=%d p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f", name, len(samples), percentile(samples, .5), percentile(samples, .95), percentile(samples, .99))
		}
	}
	if elapsed < 60*time.Second || len(failures) != 0 {
		t.Error("write workload failed", failures)
	}
	if len(failedTimes) > 0 {
		t.Logf("failed_requests n=%d p50_ms=%.3f p95_ms=%.3f", len(failedTimes), percentile(failedTimes, .5), percentile(failedTimes, .95))
	}
	if len(times) == 0 {
		t.Fatal("no successful write samples")
	}
	p95 := reportLatency(t, "successful_distinct_record_http_write", times, elapsed)
	updates := 0
	for _, n := range counts {
		updates += n
	}
	updates += committedFailures
	m9rCommandCounts(t, f, app, 32, updates, updates+32)
	var versionSum int
	if err := f.operator.QueryRow(`SELECT sum(row_version) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&versionSum); err != nil || versionSum != updates+32 {
		t.Fatal("lost updates", versionSum, updates, err)
	}
	if p95 > 500 {
		t.Fatalf("write p95 %.3f ms exceeds unchanged 500 ms gate", p95)
	}
}
