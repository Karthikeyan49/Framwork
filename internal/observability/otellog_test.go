package observability

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TF-08 (independent review of the takeover fixes, the sibling of LE-02): the OpenTelemetry exporters report a malformed OTEL_EXPORTER_OTLP_*
// variable through the library's own logger, which writes to standard error with the raw value, outside slog. The logger is kept in the
// process, so each case runs in a child process of this test binary, as the Redis test does. The value is made at run time from a label: a header
// list without "=" that the exporter refuses, and an endpoint whose port is not a number, with credentials inside it.

const otelHelperEnv = "OBSERVABILITY_OTEL_HELPER"

// otelSecret is a value derived from label, the stand-in for a credential that an operator put into a variable.
func otelSecret(label string) string {
	sum := sha256.Sum256([]byte(label))
	return "k" + hex.EncodeToString(sum[:10])
}

// TestOtelHelperProcess is the child: it makes the exporters read the malformed variables and exits. It does nothing in a normal run.
func TestOtelHelperProcess(t *testing.T) {
	mode := os.Getenv(otelHelperEnv)
	if mode == "" {
		t.Skip("helper of TestOtelLibrariesLogOnlyThroughSlog")
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx := context.Background()
	if mode == "start" {
		providers, err := Start(ctx, "otel-log-regression", "test", "http://127.0.0.1:1", logger)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			shutdown, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			providers.Shutdown(shutdown)
		}()
		return
	}
	// Without Start nothing replaces the library's logger.
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	provider := sdktrace.NewTracerProvider() // reads OTEL_TRACES_SAMPLER and OTEL_RESOURCE_ATTRIBUTES, and reports a malformed one to the error handler
	shutdown, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_ = provider.Shutdown(shutdown)
	_ = exporter.Shutdown(shutdown)
}

func runOtelHelper(t *testing.T, mode string, env ...string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOtelHelperProcess$")
	cmd.Env = append(os.Environ(), append([]string{otelHelperEnv + "=" + mode}, env...)...)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper process (%s): %v\nstdout: %s\nstderr: %s", mode, err, out.String(), errs.String())
	}
	return out.String(), errs.String()
}

func TestOtelLibrariesLogOnlyThroughSlog(t *testing.T) {
	headers, endpoint := otelSecret("tf08/headers"), otelSecret("tf08/endpoint")
	sampler, attrs := otelSecret("fu03/sampler"), otelSecret("fu03/attributes")
	env := []string{
		"OTEL_EXPORTER_OTLP_HEADERS=" + headers,                                        // a list that has no "="
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://svc:" + endpoint + "@collector:bad", // a port that is not a number
		"OTEL_TRACES_SAMPLER=" + sampler,                                               // a sampler that does not exist: the SDK prints it through the error handler (FU-03)
		"OTEL_RESOURCE_ATTRIBUTES=" + attrs,                                            // an entry without a value
	}
	t.Run("without Start the library writes the raw value to standard error", func(t *testing.T) {
		_, stderr := runOtelHelper(t, "plain", env...)
		if !strings.Contains(stderr, headers) && !strings.Contains(stderr, endpoint) {
			t.Skipf("the library no longer logs these values, so this test can no longer show the fix: stderr=%q", stderr)
		}
		if !strings.Contains(stderr, sampler) && !strings.Contains(stderr, attrs) {
			t.Skipf("the SDK no longer prints the sampler or the resource entry before an error handler exists, so this test can no longer show that part of the fix: stderr=%q", stderr)
		}
	})
	t.Run("after Start nothing of the value reaches the process output and the log holds one fixed warning", func(t *testing.T) {
		stdout, stderr := runOtelHelper(t, "start", env...)
		for _, leak := range []string{headers, endpoint, sampler, attrs, "parse headers", "parse url", "collector:bad", "invalid port", "unsupported sampler", "partial resource"} {
			if strings.Contains(stderr, leak) || strings.Contains(stdout, leak) {
				t.Fatalf("%q reaches the process output\nstdout: %s\nstderr: %s", leak, stdout, stderr)
			}
		}
		if strings.TrimSpace(stderr) != "" {
			t.Errorf("the library still writes to standard error: %s", stderr)
		}
		if got := strings.Count(stdout, `"msg":"telemetry library reported a problem"`); got != 1 {
			t.Fatalf("want one fixed warning in the structured log, got %d\nstdout: %s", got, stdout)
		}
	})
}

func TestOtelWarningIsFixedAndLimitedInRate(t *testing.T) {
	var out bytes.Buffer
	sink := &otelLogSink{logger: slog.New(slog.NewJSONHandler(&out, nil))}
	secret := otelSecret("tf08/sink")
	for i := 0; i < 1000; i++ {
		sink.Error(nil, "parse headers", "input", secret)
		sink.Info(1, "escape header value", "value", secret)
	}
	if got := strings.Count(out.String(), "telemetry library reported a problem"); got != 1 {
		t.Fatalf("2000 library messages made %d warnings, want 1", got)
	}
	if strings.Contains(out.String(), secret) || strings.Contains(out.String(), "parse headers") {
		t.Fatalf("the library's text reached the log: %s", out.String())
	}
	sink.last.Store(time.Now().Add(-2 * redisWarningEvery).UnixNano())
	sink.Error(nil, "again")
	if got := strings.Count(out.String(), "telemetry library reported a problem"); got != 2 {
		t.Fatalf("a warning after the interval was not written: %d", got)
	}
	if sink.Enabled(2) || !sink.Enabled(0) || !sink.Enabled(1) {
		t.Error("only errors and warnings are enabled")
	}
	if sink.WithValues("k", secret) != sink || sink.WithName("x") != sink {
		t.Error("a derived sink must stay the same sink: its rate limit is one for the process")
	}
	(&otelLogSink{}).Error(nil, "no logger") // must not panic
}
