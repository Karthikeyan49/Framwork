package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func options() Options {
	return Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), ProbeTimeout: 20 * time.Millisecond, Origins: []string{"http://localhost:5173"}, Probes: map[string]Probe{"postgres": func(context.Context) error { return nil }, "redis": func(context.Context) error { return nil }}}
}
func TestHealthAndReadiness(t *testing.T) {
	for _, unavailable := range []string{"", "postgres", "redis"} {
		t.Run("unavailable="+unavailable, func(t *testing.T) {
			opts := options()
			if unavailable != "" {
				opts.Probes[unavailable] = func(context.Context) error { return errors.New("secret detail") }
			}
			h := Handler(opts)
			for _, path := range []string{"/healthz", "/readyz"} {
				w := httptest.NewRecorder()
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.Header.Set("X-Request-ID", "untrusted")
				h.ServeHTTP(w, r)
				want := 200
				if path == "/readyz" && unavailable != "" {
					want = 503
				}
				if w.Code != want {
					t.Fatalf("%s status %d", path, w.Code)
				}
				if len(w.Header().Get("X-Request-ID")) != 32 {
					t.Fatal("missing server request ID")
				}
				if strings.Contains(w.Body.String(), "secret") {
					t.Fatal("leaked driver error")
				}
				var body map[string]any
				if json.Unmarshal(w.Body.Bytes(), &body) != nil {
					t.Fatal("invalid JSON")
				}
			}
		})
	}
}
func TestReadinessDeadline(t *testing.T) {
	opts := options()
	opts.Probes["redis"] = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	w := httptest.NewRecorder()
	start := time.Now()
	Handler(opts).ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || time.Since(start) > time.Second {
		t.Fatal("probe did not respect deadline")
	}
}
func TestBoundaries(t *testing.T) {
	h := Handler(options())
	for _, tc := range []struct {
		name, method, path, origin, body string
		want                             int
	}{{"denied origin", "GET", "/healthz", "https://evil.example", "", 403}, {"allowed origin", "GET", "/healthz", "http://localhost:5173", "", 200}, {"no ERP route", "GET", "/entities", "", "", 404}, {"body rejected", "GET", "/healthz", "", "secret", 413}, {"unsupported method", "POST", "/healthz", "", "", 405}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Origin", tc.origin)
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
}
func TestPreflight(t *testing.T) {
	for _, method := range []string{"GET", "DELETE"} {
		r := httptest.NewRequest("OPTIONS", "/healthz", nil)
		r.Header.Set("Origin", "http://localhost:5173")
		r.Header.Set("Access-Control-Request-Method", method)
		w := httptest.NewRecorder()
		Handler(options()).ServeHTTP(w, r)
		want := 204
		if method == "DELETE" {
			want = 403
		}
		if w.Code != want {
			t.Fatalf("status %d", w.Code)
		}
	}
}
func TestPanicRedacted(t *testing.T) {
	w := httptest.NewRecorder()
	recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("private secret") }), options().Logger).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("panic response leaked")
	}
}

func TestProbeOutageDoesNotHideHealthyDependency(t *testing.T) {
	opts := options()
	opts.ProbeTimeout = 100 * time.Millisecond
	opts.Probes["postgres"] = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	w := httptest.NewRecorder()
	Handler(opts).ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	var body struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Dependencies["redis"] != "ok" || body.Dependencies["postgres"] != "unavailable" {
		t.Fatal("healthy dependency hidden by timeout")
	}
}

func TestTelemetryRedactsUntrustedAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer func() { otel.SetTracerProvider(previous); _ = provider.Shutdown(context.Background()) }()
	r := httptest.NewRequest("GET", "http://sensitive-host.example/sensitive-path?token=sensitive-query", nil)
	r.Header.Set("User-Agent", "sensitive-agent")
	r.Header.Set("Authorization", "Bearer sensitive-auth")
	w := httptest.NewRecorder()
	Handler(options()).ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("sanitization changed routing")
	}
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no HTTP span captured")
	}
	for _, span := range spans {
		for _, attribute := range span.Attributes {
			if strings.Contains(attribute.Value.Emit(), "sensitive-") {
				t.Fatalf("untrusted telemetry attribute: %s", attribute.Key)
			}
		}
	}
}
