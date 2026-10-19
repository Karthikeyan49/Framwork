package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRejectionLogBoundsAndRedactsActualHTTPStack(t *testing.T) {
	var buf bytes.Buffer
	opts := options()
	opts.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	opts.Register = func(m *http.ServeMux) {
		m.HandleFunc("GET /limited", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "1")
			Error(w, r, 429, "rate_limited", "Request rate exceeded")
		})
	}
	h := Handler(opts)
	for range 1000 {
		r := httptest.NewRequest("GET", "http://sensitive-host.example/limited?secret=sensitive-query", nil)
		r.Header.Set("Authorization", "Bearer sensitive-token")
		r.Header.Set("Origin", "http://localhost:5173")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 429 || w.Header().Get("X-Request-ID") == "" || !strings.Contains(w.Header().Get("Access-Control-Expose-Headers"), "Retry-After") || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("rate response contract")
		}
	}
	if bytes.Count(buf.Bytes(), []byte("\n")) != 1 || strings.Contains(buf.String(), "sensitive-") || strings.Contains(buf.String(), "http request") {
		t.Fatal("unbounded or sensitive rejection log")
	}
}
func TestRejectionLogWindowCountsAndConcurrentBound(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	log := &rejectionLog{}
	now := time.Unix(1000, 0)
	var wg sync.WaitGroup
	for range 1000 {
		wg.Go(func() { log.record(context.Background(), now, logger) })
	}
	wg.Wait()
	log.record(context.Background(), now.Add(10*time.Second), logger)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatal("unbounded log windows")
	}
	var a, b struct {
		Count uint64 `json:"rejected_requests"`
	}
	if json.Unmarshal([]byte(lines[0]), &a) != nil || json.Unmarshal([]byte(lines[1]), &b) != nil || a.Count+b.Count != 1001 {
		t.Fatal("incorrect bounded summary counts")
	}
}
