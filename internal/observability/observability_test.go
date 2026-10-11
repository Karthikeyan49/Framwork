package observability

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

func TestHTTPSExportCannotBeDowngradedByEnvironment(t *testing.T) {
	for _, name := range []string{"OTEL_EXPORTER_OTLP_INSECURE", "OTEL_EXPORTER_OTLP_TRACES_INSECURE", "OTEL_EXPORTER_OTLP_METRICS_INSECURE"} {
		t.Setenv(name, "true")
	}
	var plaintextRequests atomic.Int32
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { plaintextRequests.Add(1); w.WriteHeader(200) }))
	defer collector.Close()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	providers, err := Start(context.Background(), "tls-regression", "test", "https://"+strings.TrimPrefix(collector.URL, "http://"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		providers.Shutdown(ctx)
	}()
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, Remote: true})
	_, span := providers.Traces.Tracer("tls-regression").Start(trace.ContextWithRemoteSpanContext(context.Background(), parent), "transport-check")
	span.End()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := providers.Traces.ForceFlush(ctx); err == nil {
		t.Fatal("HTTPS exporter unexpectedly succeeded against plaintext collector")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if err := providers.Metrics.ForceFlush(ctx2); err == nil {
		t.Fatal("HTTPS metrics exporter unexpectedly succeeded against plaintext collector")
	}
	if plaintextRequests.Load() != 0 {
		t.Fatal("insecure environment overrode HTTPS exporter configuration")
	}
}
