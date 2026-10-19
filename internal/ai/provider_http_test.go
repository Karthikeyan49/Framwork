package ai

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Tests of the HTTPS JSON adapter against a local TLS test server. The server is the
// only "model" here: it returns whatever bytes the test scripts, so nothing in this file
// is evidence about a real model or a real provider.

const testCredential = "cred-0123456789abcdef"

type testServer struct {
	*httptest.Server
	hits     atomic.Int64
	requests chan *http.Request
	bodies   chan []byte
	pool     *x509.CertPool
}

func newTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *testServer {
	t.Helper()
	s := &testServer{requests: make(chan *http.Request, 64), bodies: make(chan []byte, 64)}
	s.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		select {
		case s.requests <- r:
			s.bodies <- body
		default:
		}
		handler(w, r)
	}))
	t.Cleanup(s.Close)
	s.pool = x509.NewCertPool()
	s.pool.AddCert(s.Certificate())
	return s
}

func (s *testServer) config(t *testing.T, credential string) ProviderConfig {
	t.Helper()
	cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: s.URL + "/v1/complete", Model: "test-model"}
	if credential != "" {
		cfg.CredentialFile = writeCredential(t, credential)
	}
	return cfg
}

func (s *testServer) provider(t *testing.T, credential string) *HTTPJSONProvider {
	t.Helper()
	p, err := newHTTPJSONProvider(s.config(t, credential), httpOptions{rootCAs: s.pool, allowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func writeCredential(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "provider_key")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func answer(payload any, model string, usage Usage) []byte {
	raw, _ := json.Marshal(map[string]any{"payload": payload, "model": model, "usage": usage})
	return raw
}

func sampleRequest() ProviderRequest {
	return ProviderRequest{Task: TaskApplicationPatch, TemplateVersion: TemplateVersion, Segments: []Segment{{Name: SegmentInstructions, Trust: TrustTrusted, Text: "t"}, {Name: SegmentRequest, Trust: TrustUntrusted, Text: wrapperBegin + "\n{}\n" + wrapperEnd}},
		OutputSchema: json.RawMessage(`{"type":"object"}`), MaxOutputTokens: 100, Attempt: 1}
}

func reply(body []byte) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func TestHTTPProviderRoundTrip(t *testing.T) {
	s := newTestServer(t, reply(answer(`{"version":"x"}`, "remote-model-7", Usage{InputTokens: 12, OutputTokens: 34})))
	p := s.provider(t, testCredential)
	resp, err := p.Complete(context.Background(), sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Payload) != `{"version":"x"}` || resp.Model != "remote-model-7" || resp.Usage != (Usage{InputTokens: 12, OutputTokens: 34}) || resp.Provider != "https-json" || resp.Latency <= 0 {
		t.Fatalf("response %+v", resp)
	}
	r := <-s.requests
	body := <-s.bodies
	if r.Method != "POST" || r.URL.Path != "/v1/complete" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Authorization") != "Bearer "+testCredential {
		t.Fatalf("request %s %s %v", r.Method, r.URL.Path, r.Header)
	}
	var got ProviderRequest
	if json.Unmarshal(body, &got) != nil || got.Task != TaskApplicationPatch || len(got.Segments) != 2 || got.Segments[0].Trust != TrustTrusted || got.Segments[1].Trust != TrustUntrusted || got.Attempt != 1 {
		t.Fatalf("the envelope sent is the ProviderRequest: %s", body)
	}
}

func TestHTTPProviderModelLabelFallsBackToTheConfiguredOne(t *testing.T) {
	s := newTestServer(t, reply([]byte(`{"payload":"{}"}`)))
	resp, err := s.provider(t, "").Complete(context.Background(), sampleRequest())
	if err != nil || resp.Model != "test-model" || resp.Usage != (Usage{}) {
		t.Fatalf("%+v %v", resp, err)
	}
	if h := (<-s.requests).Header.Get("Authorization"); h != "" {
		t.Fatalf("no credential file configured, yet Authorization was sent: %q", h)
	}
}

func TestHTTPProviderPayloadForms(t *testing.T) {
	bytesOf := func(s string) []byte { return []byte(s) }
	cases := []struct {
		name string
		body []byte
		want string // payload bytes, "" when an error is expected
	}{
		{"string payload is used byte for byte", bytesOf(`{"payload":"not json at all \u0000 ‮"}`), "not json at all \x00 ‮"},
		{"object payload is compacted", bytesOf(`{"payload": { "a" : 1 }}`), `{"a":1}`},
		{"empty string", bytesOf(`{"payload":""}`), ""},
		{"array", bytesOf(`{"payload":[1]}`), "!"},
		{"number", bytesOf(`{"payload":3}`), "!"},
		{"null", bytesOf(`{"payload":null}`), "!"},
		{"missing payload", bytesOf(`{"model":"m"}`), "!"},
		{"unknown key", bytesOf(`{"payload":"x","extra":1}`), "!"},
		{"duplicate payload", bytesOf(`{"payload":"x","payload":"y"}`), "!"},
		{"duplicate model", bytesOf(`{"payload":"x","model":"a","model":"b"}`), "!"},
		{"trailing data", bytesOf(`{"payload":"x"} {"payload":"y"}`), "!"},
		{"not an object", bytesOf(`["payload"]`), "!"},
		{"empty body", bytesOf(``), "!"},
		{"usage with unknown key", bytesOf(`{"payload":"x","usage":{"input_tokens":1,"cost":2}}`), "!"},
		{"model of the wrong type", bytesOf(`{"payload":"x","model":5}`), "!"},
		{"truncated", bytesOf(`{"payload":"x`), "!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newTestServer(t, reply(c.body))
			resp, err := s.provider(t, "").Complete(context.Background(), sampleRequest())
			if c.want == "!" {
				if !errors.Is(err, ErrProviderMalformed) {
					t.Fatalf("err %v resp %+v", err, resp)
				}
				return
			}
			if err != nil || string(resp.Payload) != c.want {
				t.Fatalf("payload %q err %v", resp.Payload, err)
			}
		})
	}
}

func TestHTTPProviderStatusMapping(t *testing.T) {
	for status, want := range map[int]error{
		429: ErrProviderUnavailable, 500: ErrProviderUnavailable, 502: ErrProviderUnavailable, 503: ErrProviderUnavailable,
		408: ErrProviderTimeout, 504: ErrProviderTimeout,
		400: ErrProviderRefused, 401: ErrProviderRefused, 403: ErrProviderRefused, 404: ErrProviderRefused, 413: ErrProviderRefused,
		204: ErrProviderRefused, 301: ErrProviderRefused, 304: ErrProviderRefused,
	} {
		s := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("SECRET-BODY-MARKER"))
		})
		_, err := s.provider(t, testCredential).Complete(context.Background(), sampleRequest())
		if !errors.Is(err, want) {
			t.Errorf("status %d: %v, want %v", status, err, want)
		}
		if err != nil && (strings.Contains(err.Error(), "SECRET-BODY-MARKER") || strings.Contains(err.Error(), s.URL) || strings.Contains(err.Error(), testCredential)) {
			t.Errorf("status %d: the error leaks the body, URL or credential: %v", status, err)
		}
	}
}

func TestHTTPProviderNeverFollowsARedirect(t *testing.T) {
	target := newTestServer(t, reply(answer("{}", "m", Usage{})))
	origin := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/stolen", http.StatusTemporaryRedirect)
	})
	_, err := origin.provider(t, testCredential).Complete(context.Background(), sampleRequest())
	if !errors.Is(err, ErrProviderRefused) {
		t.Fatalf("err %v", err)
	}
	if target.hits.Load() != 0 {
		t.Fatal("the redirect target received a request (and with it the credential)")
	}
}

// A redirect to the SAME host and port passes the dial pinning, so only the client's
// own refusal to follow redirects stops the second request (and the credential with it).
func TestHTTPProviderNeverFollowsASameOriginRedirect(t *testing.T) {
	origin := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/complete" {
			t.Errorf("a redirect was followed to %q", r.URL.Path)
		}
		http.Redirect(w, r, "/v1/other", http.StatusTemporaryRedirect)
	})
	_, err := origin.provider(t, testCredential).Complete(context.Background(), sampleRequest())
	if !errors.Is(err, ErrProviderRefused) {
		t.Fatalf("err %v", err)
	}
	if got := origin.hits.Load(); got != 1 {
		t.Fatalf("the origin saw %d requests, want exactly the first", got)
	}
}

func TestHTTPProviderBoundsRequestAndResponse(t *testing.T) {
	t.Run("response larger than the cap", func(t *testing.T) {
		huge := answer(strings.Repeat("x", MaxProviderBytes), "m", Usage{})
		s := newTestServer(t, reply(huge))
		start := time.Now()
		_, err := s.provider(t, "").Complete(context.Background(), sampleRequest())
		if !errors.Is(err, ErrProviderMalformed) || time.Since(start) > 5*time.Second {
			t.Fatalf("err %v after %v", err, time.Since(start))
		}
	})
	t.Run("an endless response is cut off", func(t *testing.T) {
		s := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			chunk := []byte(strings.Repeat("a", 64<<10))
			for i := 0; i < 400; i++ {
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		})
		_, err := s.provider(t, "").Complete(context.Background(), sampleRequest())
		if !errors.Is(err, ErrProviderMalformed) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("a request over the cap is not sent", func(t *testing.T) {
		s := newTestServer(t, reply(answer("{}", "m", Usage{})))
		req := sampleRequest()
		req.Segments = append(req.Segments, Segment{Name: "big", Trust: TrustUntrusted, Text: strings.Repeat("a", MaxProviderBytes)})
		_, err := s.provider(t, testCredential).Complete(context.Background(), req)
		if !errors.Is(err, ErrProviderRefused) || s.hits.Load() != 0 {
			t.Fatalf("err %v hits %d", err, s.hits.Load())
		}
	})
}

func TestHTTPProviderDeadlines(t *testing.T) {
	s := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	p := s.provider(t, "")
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		start := time.Now()
		_, err := p.Complete(ctx, sampleRequest())
		if !errors.Is(err, ErrProviderTimeout) || time.Since(start) > 2*time.Second {
			t.Fatalf("err %v after %v", err, time.Since(start))
		}
	})
	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(100 * time.Millisecond); cancel() }()
		_, err := p.Complete(ctx, sampleRequest())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err %v", err)
		}
	})
}

func TestHTTPProviderConnectionFailureIsTransient(t *testing.T) {
	s := newTestServer(t, reply(nil))
	p := s.provider(t, "")
	s.Close()
	_, err := p.Complete(context.Background(), sampleRequest())
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), s.URL) || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("the error leaks the endpoint: %v", err)
	}
}

func TestHTTPProviderCredentialIsReadPerCallAndNeverRetained(t *testing.T) {
	s := newTestServer(t, reply(answer("{}", "m", Usage{})))
	cfg := s.config(t, testCredential)
	p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: s.pool, allowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Complete(context.Background(), sampleRequest()); err != nil {
		t.Fatal(err)
	}
	if got := (<-s.requests).Header.Get("Authorization"); got != "Bearer "+testCredential {
		t.Fatal("first call", got)
	}
	// Rotated on disk: the next call uses the new value, nothing was cached.
	if err = os.WriteFile(cfg.CredentialFile, []byte("rotated-credential-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Complete(context.Background(), sampleRequest()); err != nil {
		t.Fatal(err)
	}
	if got := (<-s.requests).Header.Get("Authorization"); got != "Bearer rotated-credential-value" {
		t.Fatal("second call", got)
	}
	// Nothing in the provider value (any depth of plain fields) holds a credential.
	for _, secret := range []string{testCredential, "rotated-credential-value"} {
		if holds(reflect.ValueOf(p), secret, 0) {
			t.Fatalf("the provider retains the credential %q", secret)
		}
	}
	// A credential that cannot be trusted stops the call before anything is sent.
	hits := s.hits.Load()
	for name, mutate := range map[string]func(){
		"missing": func() { _ = os.Remove(cfg.CredentialFile) },
		"group read": func() {
			_ = os.WriteFile(cfg.CredentialFile, []byte("x-cred\n"), 0o640)
			_ = os.Chmod(cfg.CredentialFile, 0o640)
		},
		"symlink": func() { _ = os.Remove(cfg.CredentialFile); _ = os.Symlink("/etc/hostname", cfg.CredentialFile) },
		"whitespace": func() {
			_ = os.Remove(cfg.CredentialFile)
			_ = os.WriteFile(cfg.CredentialFile, []byte("a b\n"), 0o600)
		},
		"empty": func() { _ = os.Remove(cfg.CredentialFile); _ = os.WriteFile(cfg.CredentialFile, nil, 0o600) },
		"oversized": func() {
			_ = os.Remove(cfg.CredentialFile)
			_ = os.WriteFile(cfg.CredentialFile, []byte(strings.Repeat("a", 5000)), 0o600)
		},
		"header line": func() {
			_ = os.Remove(cfg.CredentialFile)
			_ = os.WriteFile(cfg.CredentialFile, []byte("tok\r\nX-Evil: 1\n"), 0o600)
		},
	} {
		mutate()
		_, err = p.Complete(context.Background(), sampleRequest())
		if !errors.Is(err, ErrProviderUnavailable) || s.hits.Load() != hits {
			t.Errorf("%s: err %v, hits %d -> %d", name, err, hits, s.hits.Load())
		}
		if err != nil && (strings.Contains(err.Error(), cfg.CredentialFile) || strings.Contains(err.Error(), "x-cred")) {
			t.Errorf("%s: the error names the credential file or value: %v", name, err)
		}
	}
}

// holds reports whether any string reachable through exported or unexported plain
// fields, pointers and slices of v contains needle.
func holds(v reflect.Value, needle string, depth int) bool {
	if depth > 6 || !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.String:
		return strings.Contains(v.String(), needle)
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil() && holds(v.Elem(), needle, depth+1)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if holds(v.Field(i), needle, depth+1) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return v.Kind() == reflect.Slice && strings.Contains(string(v.Bytes()), needle)
		}
		for i := 0; i < v.Len() && i < 64; i++ {
			if holds(v.Index(i), needle, depth+1) {
				return true
			}
		}
	}
	return false
}

type stubResolver struct {
	mu    sync.Mutex
	calls int
	next  func(call int) []netip.Addr
}

func (r *stubResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return r.next(r.calls), nil
}

func addrs(list ...string) []netip.Addr {
	out := make([]netip.Addr, len(list))
	for i, s := range list {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

// TestHTTPProviderRefusesEveryNonPublicDestinationAtDialTime resolves a public-looking
// name to non-public addresses: the adapter must not connect, however the answer is
// shaped. A loopback server is the canary: it must see no request.
func TestHTTPProviderRefusesEveryNonPublicDestinationAtDialTime(t *testing.T) {
	canary := newTestServer(t, reply(answer("{}", "m", Usage{})))
	port := strings.TrimPrefix(canary.URL, "https://127.0.0.1")
	for _, answers := range [][]string{
		{"10.0.0.5"}, {"172.16.0.1"}, {"192.168.1.1"}, {"127.0.0.1"}, {"127.1.2.3"}, {"169.254.169.254"}, {"169.254.1.1"}, {"100.64.0.1"}, {"100.100.100.200"},
		{"0.0.0.0"}, {"224.0.0.1"}, {"255.255.255.255"}, {"::1"}, {"::"}, {"fe80::1"}, {"fc00::1"}, {"fd00:ec2::254"}, {"::ffff:10.0.0.1"}, {"::ffff:127.0.0.1"},
		{"64:ff9b::a00:1"}, {"2002:a00:1::1"}, {"8.8.8.8", "10.0.0.1"}, {"10.0.0.1", "8.8.8.8"}, {"8.8.8.8", "::1"}, {},
	} {
		resolver := &stubResolver{next: func(int) []netip.Addr { return addrs(answers...) }}
		cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: "https://api.example.com" + port + "/v1", Model: "m"}
		p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: canary.pool, resolver: resolver})
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Complete(context.Background(), sampleRequest())
		if err == nil {
			t.Errorf("%v: a call succeeded", answers)
		}
		if len(answers) > 0 && !errors.Is(err, ErrProviderRefused) {
			t.Errorf("%v: err %v, want a refusal", answers, err)
		}
		if canary.hits.Load() != 0 {
			t.Fatalf("%v: the canary server received a request", answers)
		}
	}
}

func TestHTTPProviderChecksEveryDialAgainstRebinding(t *testing.T) {
	canary := newTestServer(t, reply(answer("{}", "m", Usage{})))
	port := strings.TrimPrefix(canary.URL, "https://127.0.0.1")
	resolver := &stubResolver{next: func(call int) []netip.Addr {
		if call == 1 {
			return addrs("127.0.0.1") // the allowed loopback of the test option
		}
		return addrs("10.1.2.3") // the second answer is private
	}}
	cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: "https://api.example.com" + port + "/v1", Model: "m"}
	p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: canary.pool, resolver: resolver, allowLoopback: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Complete(context.Background(), sampleRequest()); err != nil {
		t.Fatalf("the first, allowed, answer: %v", err)
	}
	hits := canary.hits.Load()
	if _, err = p.Complete(context.Background(), sampleRequest()); !errors.Is(err, ErrProviderRefused) || canary.hits.Load() != hits {
		t.Fatalf("a changed answer must be validated again: %v", err)
	}
}

func TestHTTPProviderLoopbackIsOnlyReachableThroughTheTestOption(t *testing.T) {
	s := newTestServer(t, reply(answer("{}", "m", Usage{})))
	cfg := s.config(t, "")
	if _, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: s.pool}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a loopback endpoint must be refused at construction: %v", err)
	}
	// Even the test option permits loopback only: a private address stays refused.
	resolver := &stubResolver{next: func(int) []netip.Addr { return addrs("192.168.0.9") }}
	cfg.Endpoint = "https://api.example.com" + strings.TrimPrefix(s.URL, "https://127.0.0.1") + "/v1"
	p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: s.pool, allowLoopback: true, resolver: resolver})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Complete(context.Background(), sampleRequest()); !errors.Is(err, ErrProviderRefused) {
		t.Fatalf("err %v", err)
	}
	// NewProvider, the only entry configuration reaches, offers no loopback option.
	if _, err = NewProvider(cfg); !errors.Is(err, ErrInvalidInput) && err != nil {
		t.Fatal(err)
	}
	cfg.Endpoint = s.URL + "/v1"
	if _, err = NewProvider(cfg); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewProvider accepted a loopback endpoint: %v", err)
	}
}

func TestHTTPProviderIgnoresProxyEnvironment(t *testing.T) {
	var proxied atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { proxied.Add(1) }))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	s := newTestServer(t, reply(answer("{}", "m", Usage{})))
	if _, err := s.provider(t, "").Complete(context.Background(), sampleRequest()); err != nil {
		t.Fatal(err)
	}
	if proxied.Load() != 0 || s.hits.Load() != 1 {
		t.Fatalf("proxy hits %d, server hits %d", proxied.Load(), s.hits.Load())
	}
}

func TestHTTPProviderRefusesAServerWithAnUntrustedCertificate(t *testing.T) {
	s := newTestServer(t, reply(answer("{}", "m", Usage{})))
	p, err := newHTTPJSONProvider(s.config(t, testCredential), httpOptions{allowLoopback: true}) // no test CA
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Complete(context.Background(), sampleRequest()); !errors.Is(err, ErrProviderUnavailable) && !errors.Is(err, ErrProviderRefused) {
		t.Fatalf("err %v", err)
	}
	select {
	case r := <-s.requests:
		t.Fatalf("a request reached a server whose certificate is not trusted: %v", r.Header)
	default:
	}
}

func TestHTTPProviderIsSafeForConcurrentUse(t *testing.T) {
	s := newTestServer(t, reply(answer("{}", "m", Usage{InputTokens: 1, OutputTokens: 1})))
	p := s.provider(t, testCredential)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := p.Complete(context.Background(), sampleRequest()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.hits.Load() != 12 {
		t.Fatal("hits", s.hits.Load())
	}
}

// TestHTTPProviderBehindTheGateway runs the whole pipeline over the adapter: the same
// bytes through the scripted provider and through the test server give one outcome.
func TestHTTPProviderBehindTheGatewayMatchesTheScriptedProvider(t *testing.T) {
	r := newRig(t, Options{})
	good, bad := r.good(), r.broken("Ghost")
	for name, steps := range map[string][][]byte{"first try": {good}, "repaired": {bad, good}, "never converges": {bad, bad, bad}, "garbage": {[]byte("nope"), []byte("nope"), []byte("nope")}} {
		t.Run(name, func(t *testing.T) {
			rs := newRig(t, Options{})
			for _, p := range steps {
				rs.prov.Push(ScriptStep{Payload: p, Usage: Usage{InputTokens: 100, OutputTokens: 50}, Model: "m"})
			}
			want := rs.run(okBudget())

			var next atomic.Int64
			s := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
				i := int(next.Add(1)) - 1
				if i >= len(steps) {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_, _ = w.Write(answer(string(steps[i]), "m", Usage{InputTokens: 100, OutputTokens: 50}))
			})
			rh := newRig(t, Options{})
			rh.g = NewGateway(s.provider(t, ""), Options{Clock: rh.clock.Now})
			got := rh.run(okBudget())

			if got.Status != want.Status || got.Hash != want.Hash || fmt.Sprint(outcomes(got.Attempts)) != fmt.Sprint(outcomes(want.Attempts)) {
				t.Fatalf("scripted %s %v %s, http %s %v %s", want.Status, outcomes(want.Attempts), want.Hash, got.Status, outcomes(got.Attempts), got.Hash)
			}
			if got.Attempts[0].Provider != "https-json" {
				t.Fatal("provider label", got.Attempts[0].Provider)
			}
		})
	}
}
