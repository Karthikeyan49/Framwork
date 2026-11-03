package observability

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
)

// otelLogSink is what the OpenTelemetry libraries log through. Their own default is a logger on standard error, outside slog, and the
// exporters hand it the raw text of a malformed OTEL_EXPORTER_OTLP_* variable as a value ("parse headers" with the whole header list, "parse url"
// with the endpoint and the credentials inside it), so a malformed operator setting could put a secret on the terminal (independent review
// TF-08, the sibling of LE-02). Here the keys, the values and the text are dropped and the operator gets one fixed warning, at most one every ten
// seconds, for a warning or an error of the libraries; what they report as information or debug output is not read.
type otelLogSink struct {
	logger *slog.Logger
	last   atomic.Int64 // Unix nanoseconds of the last warning
}

func (s *otelLogSink) warn() {
	if s.logger == nil {
		return
	}
	now := time.Now().UnixNano()
	if last := s.last.Load(); now-last >= int64(redisWarningEvery) && s.last.CompareAndSwap(last, now) {
		s.logger.Warn("telemetry library reported a problem")
	}
}

func (s *otelLogSink) Init(logr.RuntimeInfo) {}

// Enabled is true for errors (level 0) and warnings (level 1) only.
func (s *otelLogSink) Enabled(level int) bool { return level <= 1 }

func (s *otelLogSink) Info(int, string, ...any) { s.warn() }

func (s *otelLogSink) Error(error, string, ...any) { s.warn() }

func (s *otelLogSink) WithValues(...any) logr.LogSink { return s }

func (s *otelLogSink) WithName(string) logr.LogSink { return s }
