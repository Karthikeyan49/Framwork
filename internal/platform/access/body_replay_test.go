package access

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serve reads a body before the database slot and hands the handler a replay. The replay has to look to the handler exactly
// like the live body: the bytes, then the end of the body or the error that stopped the read.
func TestReplayedBodyEndsLikeTheLiveBody(t *testing.T) {
	got, err := io.ReadAll(replayedBody([]byte("abc"), nil))
	if err != nil || string(got) != "abc" {
		t.Fatalf("complete body: %q, %v", got, err)
	}
	cause := errors.New("stopped")
	got, err = io.ReadAll(replayedBody([]byte("abc"), cause))
	if !errors.Is(err, cause) || string(got) != "abc" {
		t.Fatalf("interrupted body: %q, %v", got, err)
	}
	// Every later read keeps failing the same way, as a live body does.
	body := replayedBody(nil, cause)
	for i := 0; i < 3; i++ {
		if n, err := body.Read(make([]byte, 4)); n != 0 || !errors.Is(err, cause) {
			t.Fatalf("read %d: %d, %v", i, n, err)
		}
	}
}

// The front door caps every body at 1 MiB with http.MaxBytesReader. A handler with a smaller limit never reaches the cap, and
// a handler that reads up to the cap sees the cap's error: both must be unchanged by the pre-read.
func TestReplayedBodyKeepsTheFrontDoorCap(t *testing.T) {
	over := strings.Repeat("x", 1<<20+100)
	live := func() io.Reader {
		return http.MaxBytesReader(httptest.NewRecorder(), io.NopCloser(strings.NewReader(over)), 1<<20)
	}
	pre, preErr := io.ReadAll(io.LimitReader(live(), 1<<20+1))
	var tooLarge *http.MaxBytesError
	if !errors.As(preErr, &tooLarge) || len(pre) != 1<<20 {
		t.Fatalf("the pre-read of an oversize body: %d bytes, %v", len(pre), preErr)
	}
	for name, limit := range map[string]int64{"a 64 KiB handler": 64<<10 + 1, "a handler at the cap": 1<<20 + 1} {
		want, wantErr := io.ReadAll(io.LimitReader(live(), limit))
		replay, replayErr := io.ReadAll(io.LimitReader(replayedBody(pre, preErr), limit))
		if !bytes.Equal(want, replay) || (wantErr == nil) != (replayErr == nil) {
			t.Fatalf("%s: live %d bytes, %v; replayed %d bytes, %v", name, len(want), wantErr, len(replay), replayErr)
		}
	}
}
