package observability

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// LE-02 (independent logging and error-leak audit): the Redis client library writes the dial target and the driver's error text to standard
// error, outside slog, and an anonymous GET /readyz makes it do so. The library keeps its logger in the process, so each case runs in a
// child process of this test binary: the case without Start shows that the library does log and that this test can see it, the case with
// Start shows what the process writes after the fix.

const redisHelperEnv = "OBSERVABILITY_REDIS_HELPER"

// TestRedisHelperProcess is the child: it dials a port nothing listens on and exits. It does nothing in a normal run.
func TestRedisHelperProcess(t *testing.T) {
	mode := os.Getenv(redisHelperEnv)
	if mode == "" {
		t.Skip("helper of TestRedisClientLogsOnlyThroughSlog")
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if mode == "start" {
		providers, err := Start(context.Background(), "redis-log-regression", "test", "", logger)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			providers.Shutdown(ctx)
		}()
	}
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: time.Second, MaxRetries: -1})
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		if client.Ping(ctx).Err() == nil {
			t.Fatal("a dial to a closed port succeeded")
		}
	}
}

func runRedisHelper(t *testing.T, mode string) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRedisHelperProcess$")
	cmd.Env = append(os.Environ(), redisHelperEnv+"="+mode)
	var out, errs bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper process (%s): %v\nstdout: %s\nstderr: %s", mode, err, out.String(), errs.String())
	}
	return out.String(), errs.String()
}

func TestRedisClientLogsOnlyThroughSlog(t *testing.T) {
	t.Run("without Start the library writes its own line to standard error", func(t *testing.T) {
		_, stderr := runRedisHelper(t, "plain")
		if !strings.Contains(stderr, "failed to dial") || !strings.Contains(stderr, "127.0.0.1:1") {
			t.Skipf("the library no longer logs this failure, so this test can no longer show the fix: stderr=%q", stderr)
		}
	})
	t.Run("after Start nothing reaches standard error and the log holds one fixed warning", func(t *testing.T) {
		stdout, stderr := runRedisHelper(t, "start")
		for _, leak := range []string{"redis:", "127.0.0.1", "dial tcp", "connection refused", "failed to dial"} {
			if strings.Contains(stderr, leak) || strings.Contains(stdout, leak) {
				t.Fatalf("%q reaches the process output\nstdout: %s\nstderr: %s", leak, stdout, stderr)
			}
		}
		if got := strings.Count(stdout, `"msg":"redis client reported a problem"`); got != 1 {
			t.Fatalf("want one fixed warning in the structured log, got %d\nstdout: %s", got, stdout)
		}
	})
}

func TestRedisWarningIsLimitedInRate(t *testing.T) {
	var out bytes.Buffer
	l := &redisLogger{logger: slog.New(slog.NewJSONHandler(&out, nil))}
	for i := 0; i < 1000; i++ {
		l.Printf(context.Background(), "redis: %v", "dial tcp 10.0.0.1:6379: connect: connection refused")
	}
	if got := strings.Count(out.String(), "redis client reported a problem"); got != 1 {
		t.Fatalf("1000 library messages made %d warnings, want 1", got)
	}
	if strings.Contains(out.String(), "10.0.0.1") || strings.Contains(out.String(), "refused") {
		t.Fatalf("the library's text reached the log: %s", out.String())
	}
	l.last.Store(time.Now().Add(-2 * redisWarningEvery).UnixNano())
	l.Printf(context.Background(), "again")
	if got := strings.Count(out.String(), "redis client reported a problem"); got != 2 {
		t.Fatalf("a warning after the interval was not written: %d", got)
	}
	(&redisLogger{}).Printf(context.Background(), "no logger") // must not panic
}
