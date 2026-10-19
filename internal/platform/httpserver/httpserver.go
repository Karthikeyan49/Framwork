// Package httpserver provides M0 operational endpoints and bounded HTTP middleware.
package httpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/trace"
)

type Probe func(context.Context) error
type Options struct {
	Logger       *slog.Logger
	Origins      []string
	Probes       map[string]Probe
	ProbeTimeout time.Duration
	Register     func(*http.ServeMux)
	AllowPOST    func(string) bool
}
type ErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}
type requestIDKey struct{}
type originalRequestKey struct{}

func RequestID(ctx context.Context) string { id, _ := ctx.Value(requestIDKey{}).(string); return id }
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func Error(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	JSON(w, status, ErrorResponse{code, message, RequestID(r.Context())})
}

func Handler(opts Options) http.Handler {
	mux := http.NewServeMux()
	if opts.Register != nil {
		opts.Register(mux)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), opts.ProbeTimeout)
		defer cancel()
		status := http.StatusOK
		dependencies := map[string]string{}
		// Concurrent probes share a deadline so one outage does not hide a healthy
		// dependency. Buffered results let late context-aware probes exit safely.
		type result struct {
			name string
			err  error
		}
		results := make(chan result, len(opts.Probes))
		for name, probe := range opts.Probes {
			go func() { results <- result{name, probe(ctx)} }()
		}
		for range len(opts.Probes) {
			select {
			case outcome := <-results:
				if outcome.err != nil {
					status = http.StatusServiceUnavailable
					dependencies[outcome.name] = "unavailable"
				} else {
					dependencies[outcome.name] = "ok"
				}
			case <-ctx.Done():
				status = http.StatusServiceUnavailable
			}
		}
		for name := range opts.Probes {
			if _, ok := dependencies[name]; !ok {
				dependencies[name] = "unavailable"
			}
		}
		state := "ready"
		if status != http.StatusOK {
			state = "not_ready"
		}
		JSON(w, status, struct {
			Status       string            `json:"status"`
			Dependencies map[string]string `json:"dependencies"`
		}{state, dependencies})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			w.Header().Set("Allow", "GET, HEAD")
			Error(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not permitted")
			return
		}
		Error(w, r, http.StatusNotFound, "not_found", "Route not found")
	})
	// Instrument a sanitized request. The application still sees the original request
	// with the span context and instrumented body. Untrusted paths, host, user-agent,
	// query strings and arbitrary headers never become telemetry attributes.
	application := accessLog(recoverer(mux, opts.Logger), opts.Logger)
	instrumented := otelhttp.NewHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		original := r.Context().Value(originalRequestKey{}).(*http.Request)
		actual := original.WithContext(r.Context())
		actual.Body = r.Body
		application.ServeHTTP(w, actual)
	}), "http.server", otelhttp.WithServerName("framework-api"), otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
			return r.Method + " " + r.URL.Path
		}
		return "http.request"
	}))
	return middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		safe := r.Clone(context.WithValue(r.Context(), originalRequestKey{}, r))
		if safe.URL.Path != "/healthz" && safe.URL.Path != "/readyz" {
			safe.URL.Path = "/unmatched"
		}
		safe.URL.RawPath = ""
		safe.URL.RawQuery = ""
		safe.URL.User = nil
		safe.URL.Host = "framework-api"
		safe.Host = "framework-api"
		safe.RequestURI = safe.URL.Path
		safe.Header = make(http.Header)
		safe.Header.Set("Traceparent", r.Header.Get("Traceparent"))
		safe.Header.Set("Tracestate", r.Header.Get("Tracestate"))
		instrumented.ServeHTTP(w, safe)
	}), opts)
}

func middleware(next http.Handler, opts Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bytes := make([]byte, 16)
		if _, err := rand.Read(bytes); err != nil {
			JSON(w, http.StatusInternalServerError, map[string]string{"code": "internal_error"})
			return
		}
		id := hex.EncodeToString(bytes)
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Add("Vary", "Origin")
			allowed := false
			for _, candidate := range opts.Origins {
				if candidate == origin {
					allowed = true
					break
				}
			}
			if !allowed {
				Error(w, r, http.StatusForbidden, "origin_denied", "Origin is not permitted")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, Retry-After")
			if r.Method == http.MethodOptions {
				method := r.Header.Get("Access-Control-Request-Method")
				if method != http.MethodGet && method != http.MethodHead && !(method == http.MethodPost && opts.AllowPOST != nil && opts.AllowPOST(r.URL.Path)) {
					Error(w, r, http.StatusForbidden, "method_denied", "Method is not permitted")
					return
				}
				methods := "GET, HEAD"
				if opts.AllowPOST != nil && opts.AllowPOST(r.URL.Path) {
					methods += ", POST"
				}
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Traceparent, Tracestate")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		// Bodies are accepted only on an explicitly registered write-route allowlist.
		if (r.ContentLength > 0 || len(r.TransferEncoding) > 0) && !(r.Method == http.MethodPost && opts.AllowPOST != nil && opts.AllowPOST(r.URL.Path)) {
			Error(w, r, http.StatusRequestEntityTooLarge, "body_not_allowed", "Request bodies are not accepted")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() != nil {
				logger.ErrorContext(r.Context(), "request panic recovered", "request_id", RequestID(r.Context()))
				Error(w, r, http.StatusInternalServerError, "internal_error", "An internal error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (w *recorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *recorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func accessLog(next http.Handler, logger *slog.Logger) http.Handler {
	throttled := &rejectionLog{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &recorder{ResponseWriter: w}
		next.ServeHTTP(rw, r)
		if rw.status == http.StatusTooManyRequests {
			throttled.record(r.Context(), time.Now(), logger)
			return
		}
		span := trace.SpanContextFromContext(r.Context())
		logger.InfoContext(r.Context(), "http request", "request_id", RequestID(r.Context()), "trace_id", span.TraceID().String(), "method", r.Method, "status", rw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

// Aggregate transport denials instead of replacing a database/audit flood with
// one log per rejected packet. Metrics retain a closed, low-cardinality count.
type rejectionLog struct {
	mu    sync.Mutex
	last  time.Time
	count uint64
}

func (l *rejectionLog) record(ctx context.Context, now time.Time, logger *slog.Logger) {
	l.mu.Lock()
	if l.count < ^uint64(0) {
		l.count++
	}
	if !l.last.IsZero() && now.Sub(l.last) < 10*time.Second {
		l.mu.Unlock()
		return
	}
	count := l.count
	l.count = 0
	l.last = now
	l.mu.Unlock()
	logger.WarnContext(ctx, "http rate rejections", "status", http.StatusTooManyRequests, "rejected_requests", count)
}

func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
