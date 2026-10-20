package tools

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func (h *harness) writeFixture(handler http.HandlerFunc) (Definition, *testServer, *testServer) {
	prod, sandbox := h.pair(handler)
	return writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port), prod, sandbox
}

func TestStatusClassification(t *testing.T) {
	type row struct {
		status    int
		outcome   Outcome
		class     string
		retryable bool
	}
	rows := []row{
		{400, Failed, ClassRejected, false}, {401, Failed, ClassRejected, false}, {403, Failed, ClassRejected, false},
		{404, Failed, ClassRejected, false}, {410, Failed, ClassRejected, false}, {422, Failed, ClassRejected, false},
		{499, Failed, ClassRejected, false},
		{429, Failed, ClassRateLimited, true},
		{408, Unknown, ClassServerError, false}, {409, Unknown, ClassServerError, false},
		{500, Unknown, ClassServerError, false}, {502, Unknown, ClassServerError, false}, {503, Unknown, ClassServerError, false},
		{504, Unknown, ClassServerError, false}, {599, Unknown, ClassServerError, false},
	}
	for _, r := range rows {
		t.Run(strconv.Itoa(r.status), func(t *testing.T) {
			h := newHarness(t)
			def, _, _ := h.writeFixture(reply(r.status, `{"error":"detail that must not surface"}`))
			got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
			assertResult(t, got, r.outcome, r.class, r.retryable, r.status)
		})
	}
	// Reads classify identically for provider errors.
	h := newHarness(t)
	prod, sandbox := h.pair(reply(503, `{}`))
	read := readDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	in := map[string]any{"account": "a", "year": 1, "active": true}
	assertResult(t, h.run(read, ModeLive, KindSend, "", in), Unknown, ClassServerError, false, 503)
	prod.set(reply(429, `{}`))
	assertResult(t, h.run(read, ModeLive, KindSend, "", in), Failed, ClassRateLimited, true, 429)
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			h := newHarness(t)
			target := h.server(reply(200, okWrite))
			prod := h.server(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://sandbox.example.com:"+strconv.Itoa(target.port)+"/stolen")
				w.WriteHeader(status)
			})
			write := writeDefinition("api.example.com", prod.port, "sandbox.example.com", target.port+1)
			// A write may already have taken effect: UNKNOWN, never retryable.
			assertResult(t, h.run(write, ModeLive, KindSend, testKey, writeInputs()), Unknown, ClassServerError, false, status)
			// A read has no effect to reconcile: permanent rejection.
			read := readDefinition("api.example.com", prod.port, "sandbox.example.com", target.port+1)
			assertResult(t, h.run(read, ModeLive, KindSend, "", map[string]any{"account": "a", "year": 1, "active": true}), Failed, ClassRejected, false, status)
			if target.hits.Load() != 0 || target.conns.Load() != 0 {
				t.Fatalf("redirect target contacted: hits=%d conns=%d", target.hits.Load(), target.conns.Load())
			}
			// The credential must not be sent to the redirect target either.
			if len(h.secrets.refs()) != 1 || h.secrets.refs()[0] != "crm_live" {
				t.Fatalf("secret refs %v", h.secrets.refs())
			}
		})
	}
}

func TestNonStandardSuccessLikeStatusesAreNotSuccess(t *testing.T) {
	for _, status := range []int{300, 304, 305, 306} {
		h := newHarness(t)
		prod := h.server(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		write := writeDefinition("api.example.com", prod.port, "sandbox.example.com", prod.port+1)
		got := h.run(write, ModeLive, KindSend, testKey, writeInputs())
		if got.Outcome == Success {
			t.Errorf("status %d reported success", status)
		}
		if got.Outcome != Unknown && got.Outcome != Failed {
			t.Errorf("status %d: %+v", status, got)
		}
	}
}

func TestResponseBodyLimitsAndValidation(t *testing.T) {
	h := newHarness(t)
	def, prod, _ := h.writeFixture(reply(200, okWrite))
	limit := def.Connector.MaxResponseBytes // 4096
	pad := func(total int) string {
		prefix, suffix := `{"id":"n-1","pad":"`, `"}`
		return prefix + strings.Repeat("p", total-len(prefix)-len(suffix)) + suffix
	}
	// Exactly at the limit is accepted; one byte more is not.
	prod.set(reply(200, pad(limit)))
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Success, ClassOK, false, 200)
	prod.set(reply(200, pad(limit+1)))
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Unknown, ClassInvalidResponse, false, 200)
	// A very large body is cut off by the limit rather than buffered.
	prod.set(reply(200, pad(4<<20)))
	started := time.Now()
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Unknown, ClassInvalidResponse, false, 200)
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("oversize body was not cut off")
	}

	invalid := map[string]string{
		"empty body":          ``,
		"whitespace":          "  \n",
		"not json":            `id=n-1`,
		"truncated json":      `{"id":"n-1"`,
		"array":               `[{"id":"n-1"}]`,
		"string":              `"n-1"`,
		"null":                `null`,
		"trailing data":       `{"id":"n-1"} {"id":"n-2"}`,
		"trailing garbage":    `{"id":"n-1"}x`,
		"duplicate key":       `{"id":"n-1","id":"n-2"}`,
		"duplicate unknown":   `{"id":"n-1","x":1,"x":2}`,
		"nested object":       `{"id":"n-1","nested":{"a":1}}`,
		"nested array":        `{"id":"n-1","list":[1]}`,
		"missing required":    `{"status":"created"}`,
		"null required":       `{"id":null}`,
		"wrong string type":   `{"id":7}`,
		"wrong integer type":  `{"id":"n-1","total":"7"}`,
		"fractional integer":  `{"id":"n-1","total":7.5}`,
		"huge integer":        `{"id":"n-1","total":9007199254740993}`,
		"wrong bool type":     `{"id":"n-1","accepted":"true"}`,
		"decimal as number":   `{"id":"n-1","balance":10.25}`,
		"bad decimal":         `{"id":"n-1","balance":"1e3"}`,
		"string too long":     `{"id":"` + strings.Repeat("x", 65) + `"}`,
		"nul in string":       `{"id":"a\u0000b"}`,
		"non-string key type": `{1:"a"}`,
		"html":                `<html>gateway error</html>`,
	}
	for name, body := range invalid {
		prod.set(reply(200, body))
		got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
		if got.Outcome != Unknown || got.Class != ClassInvalidResponse || got.Retryable || got.Status != 200 || len(got.Output) != 0 {
			t.Errorf("%s: %+v", name, got)
		}
	}

	// A 2xx without a body (e.g. 204) cannot satisfy required outputs.
	prod.set(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Unknown, ClassInvalidResponse, false, 204)

	// Unknown members and optional outputs are dropped, not stored.
	prod.set(reply(200, `{"id":"n-1","extra":"x","status":null,"other":false}`))
	got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
	assertResult(t, got, Success, ClassOK, false, 200)
	if len(got.Output) != 1 || got.Output["id"] != "n-1" {
		t.Fatalf("output %#v", got.Output)
	}
}

func TestOversizeBodyStopsTheReadEarly(t *testing.T) {
	h := newHarness(t)
	var written int64
	finished := make(chan struct{})
	def, prod, _ := h.writeFixture(nil)
	prod.set(func(w http.ResponseWriter, _ *http.Request) {
		defer close(finished)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		chunk := bytes.Repeat([]byte("x"), 32<<10)
		_, _ = w.Write([]byte(`{"id":"n-1","pad":"`))
		for i := 0; i < 4096; i++ { // up to 128 MiB if the client keeps reading
			if _, err := w.Write(chunk); err != nil {
				return
			}
			written += int64(len(chunk))
		}
	})
	got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
	assertResult(t, got, Unknown, ClassInvalidResponse, false, 200)
	select {
	case <-finished:
	case <-time.After(20 * time.Second):
		t.Fatal("provider never observed the client closing")
	}
	// Socket buffers absorb a few MiB; reading on would drain all 128 MiB.
	if written > 32<<20 {
		t.Fatalf("client pulled %d bytes of an oversize response", written)
	}
}

func TestCompressedResponsesAreNotDecoded(t *testing.T) {
	h := newHarness(t)
	var packed bytes.Buffer
	zw := gzip.NewWriter(&packed)
	_, _ = zw.Write([]byte(okWrite))
	_ = zw.Close()
	def, prod, _ := h.writeFixture(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(200)
		_, _ = w.Write(packed.Bytes())
	})
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Unknown, ClassInvalidResponse, false, 200)
	if prod.last().header.Get("Accept-Encoding") != "" {
		t.Fatal("client must not advertise compression")
	}
}

func TestTimeoutWhileServerSleepsIsUnknownAndBounded(t *testing.T) {
	h := newHarness(t)
	var prod *testServer
	def, prod, _ := h.writeFixture(nil)
	prod.set(func(w http.ResponseWriter, r *http.Request) { prod.wait(r, 10*time.Second) })
	started := time.Now()
	got := h.gw.Execute(context.Background(), Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: 200 * time.Millisecond})
	elapsed := time.Since(started)
	assertResult(t, got, Unknown, ClassTimeout, false, 0)
	if elapsed < 150*time.Millisecond || elapsed > 200*time.Millisecond+750*time.Millisecond {
		t.Fatalf("elapsed %v for a 200ms timeout", elapsed)
	}

	// The connector's own timeout caps a longer request timeout.
	def.Connector.TimeoutMS = 250
	started = time.Now()
	got = h.gw.Execute(context.Background(), Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: time.Minute})
	assertResult(t, got, Unknown, ClassTimeout, false, 0)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond+750*time.Millisecond {
		t.Fatalf("connector timeout not honored: %v", elapsed)
	}

	// A caller deadline shorter than the request timeout also wins.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	started = time.Now()
	got = h.gw.Execute(ctx, Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: time.Minute})
	assertResult(t, got, Unknown, ClassTimeout, false, 0)
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond+750*time.Millisecond {
		t.Fatalf("context deadline not honored: %v", elapsed)
	}
}

func TestTimeoutWhileReadingBodyIsUnknownTimeout(t *testing.T) {
	h := newHarness(t)
	var prod *testServer
	def, prod, _ := h.writeFixture(nil)
	prod.set(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"id":"n`))
		w.(http.Flusher).Flush()
		prod.wait(r, 10*time.Second)
	})
	started := time.Now()
	got := h.gw.Execute(context.Background(), Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: 250 * time.Millisecond})
	assertResult(t, got, Unknown, ClassTimeout, false, 200)
	if time.Since(started) > time.Second+250*time.Millisecond {
		t.Fatal("body read exceeded the timeout")
	}
}

func hijackAndClose(prefix string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		if prefix != "" {
			_, _ = conn.Write([]byte(prefix))
		}
		_ = conn.Close()
	}
}

func TestBrokenTransportAfterWriteIsUnknown(t *testing.T) {
	cases := map[string]struct {
		handler func() http.HandlerFunc
		status  int
	}{
		"closed after request read": {func() http.HandlerFunc { return hijackAndClose("") }, 0},
		"truncated headers":         {func() http.HandlerFunc { return hijackAndClose("HTTP/1.1 200 OK\r\nContent-Le") }, 0},
		"truncated body": {func() http.HandlerFunc {
			return hijackAndClose("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"id\":\"a\"")
		}, 200},
		"garbage response": {func() http.HandlerFunc { return hijackAndClose("not http at all\r\n\r\n") }, 0},
		"oversize headers": {func() http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Pad", strings.Repeat("h", 2*MaxResponseHeaderBytes))
				w.WriteHeader(200)
			}
		}, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			def, prod, _ := h.writeFixture(c.handler())
			got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
			assertResult(t, got, Unknown, ClassTransport, false, c.status)
			// DisableKeepAlives: the transport must never replay the request.
			if prod.hits.Load() != 1 {
				t.Fatalf("request delivered %d times", prod.hits.Load())
			}
		})
	}
}

func TestCancellationHonoredAtEveryPhase(t *testing.T) {
	h := newHarness(t)
	var prod *testServer
	def, prod, _ := h.writeFixture(nil)
	arrived := make(chan struct{}, 1)
	prod.set(func(w http.ResponseWriter, r *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		prod.wait(r, 10*time.Second)
	})

	// Canceled before the call: nothing is resolved, authenticated or sent.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := h.gw.Execute(ctx, Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: 2 * time.Second})
	assertResult(t, got, Failed, ClassNotSent, true, 0)
	h.assertNoIO(prod)

	// Canceled while the provider holds the request: the effect may exist.
	ctx, cancel = context.WithCancel(context.Background())
	go func() {
		<-arrived
		cancel()
	}()
	started := time.Now()
	got = h.gw.Execute(ctx, Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: 5 * time.Second})
	assertResult(t, got, Unknown, ClassTransport, false, 0)
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("cancellation was not prompt")
	}

	// A non-positive timeout leaves no budget: nothing is attempted.
	before := prod.hits.Load()
	for _, d := range []time.Duration{0, -time.Second} {
		got = h.gw.Execute(context.Background(), Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: d})
		assertResult(t, got, Failed, ClassNotSent, true, 0)
	}
	if prod.hits.Load() != before {
		t.Fatal("zero timeout sent a request")
	}
}

func TestExecuteNeverPanicsOnDegenerateInput(t *testing.T) {
	h := newHarness(t)
	var nilGateway *Gateway
	var zero Gateway
	for name, g := range map[string]*Gateway{"nil": nilGateway, "zero": &zero, "built": h.gw} {
		for _, r := range []Request{{}, {Mode: ModeLive}, {Mode: ModeSimulate, Kind: KindSend}, {Mode: ModeLive, Kind: KindLookup, Key: testKey}} {
			func() {
				defer func() {
					if recover() != nil {
						t.Errorf("%s gateway panicked", name)
					}
				}()
				got := g.Execute(context.Background(), r)
				if got.Outcome != Failed || got.Class != ClassBlocked {
					t.Errorf("%s: %+v", name, got)
				}
			}()
		}
	}
	var noContext context.Context
	if got := h.gw.Execute(noContext, Request{}); got.Outcome != Failed || got.Class != ClassBlocked {
		t.Fatalf("nil context: %+v", got)
	}
}

func TestRateLimiterIsPerToolAndRefills(t *testing.T) {
	h := newHarness(t)
	def, prod, sandbox := h.writeFixture(reply(201, okWrite))
	def.Connector.RatePerSecond = 2
	read := readDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	prod.set(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reply(200, `{"balance":"1.00"}`)(w, r)
			return
		}
		reply(201, okWrite)(w, r)
	})
	send := func() Result { return h.run(def, ModeLive, KindSend, testKey, writeInputs()) }
	assertResult(t, send(), Success, ClassOK, false, 201)
	assertResult(t, send(), Success, ClassOK, false, 201)
	hits := prod.hits.Load()
	secretCalls := len(h.secrets.refs())
	resolved := h.resolver.count()
	assertResult(t, send(), Failed, ClassRateLimited, true, 0)
	if prod.hits.Load() != hits || len(h.secrets.refs()) != secretCalls || h.resolver.count() != resolved {
		t.Fatal("rate limited call performed I/O")
	}
	// Another tool has its own bucket; so does another version of this tool.
	assertResult(t, h.run(read, ModeLive, KindSend, "", map[string]any{"account": "a", "year": 1, "active": true}), Success, ClassOK, false, 200)
	next := def
	next.Version = "1.0.1"
	assertResult(t, h.run(next, ModeLive, KindSend, testKey, writeInputs()), Success, ClassOK, false, 201)
	// Invalid requests do not consume tokens.
	h.clock.advance(time.Second)
	_ = h.run(def, ModeLive, KindSend, "bad", writeInputs())
	assertResult(t, send(), Success, ClassOK, false, 201)
	// Tokens refill with time, capped at the burst.
	h.clock.advance(10 * time.Second)
	assertResult(t, send(), Success, ClassOK, false, 201)
	assertResult(t, send(), Success, ClassOK, false, 201)
	assertResult(t, send(), Failed, ClassRateLimited, true, 0)
	h.clock.advance(500 * time.Millisecond)
	assertResult(t, send(), Success, ClassOK, false, 201)
	assertResult(t, send(), Failed, ClassRateLimited, true, 0)
}

func TestRateLimiterIsExactUnderConcurrency(t *testing.T) {
	h := newHarness(t)
	def, prod, _ := h.writeFixture(reply(201, okWrite))
	def.Connector.RatePerSecond = 5
	results := make(chan Result, 40)
	for i := 0; i < 40; i++ {
		go func() { results <- h.run(def, ModeLive, KindSend, testKey, writeInputs()) }()
	}
	ok, limited := 0, 0
	for i := 0; i < 40; i++ {
		switch r := <-results; r.Class {
		case ClassOK:
			ok++
		case ClassRateLimited:
			limited++
		default:
			t.Fatalf("unexpected %+v", r)
		}
	}
	if ok != 5 || limited != 35 || prod.hits.Load() != 5 {
		t.Fatalf("ok=%d limited=%d hits=%d", ok, limited, prod.hits.Load())
	}
}

func TestRateLimiterBoundsMemoryAndIgnoresInvalidRates(t *testing.T) {
	l := newRateLimiter()
	now := time.Unix(1000, 0)
	if l.allow("k", 0, now) || l.allow("k", -1, now) {
		t.Fatal("non-positive rate allowed")
	}
	for i := 0; i < maxRateBuckets; i++ {
		if !l.allow(fmt.Sprintf("tool@%d", i), 1, now) {
			t.Fatalf("bucket %d", i)
		}
	}
	// All buckets are drained: a new key is refused rather than growing memory.
	if l.allow("overflow", 1, now) || len(l.buckets) != maxRateBuckets {
		t.Fatal("limiter grew unbounded")
	}
	// Once buckets refill they are reclaimed and the new key is admitted.
	if !l.allow("overflow", 1, now.Add(2*time.Second)) || len(l.buckets) > maxRateBuckets {
		t.Fatal("limiter did not reclaim full buckets")
	}
	// A lowered declaration cannot be bypassed by the old burst.
	l = newRateLimiter()
	for i := 0; i < 10; i++ {
		if !l.allow("t", 10, now) {
			t.Fatal("burst")
		}
	}
	if l.allow("t", 1, now) {
		t.Fatal("changed rate granted a fresh burst")
	}
}
