package tools

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Hostnames used by fixtures. They never reach real DNS: the gateway under
// test uses a fake resolver that maps them to the loopback test servers.
var testHosts = []string{"api.example.com", "sandbox.example.com", "pay.example.com", "pay-sandbox.example.com", "geo.example.com"}

func newCert(t *testing.T, names ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 100))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "tools test"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, DNSNames: names, IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// seen is one request as the server received it.
type seen struct {
	method, uri, host string
	header            http.Header
	body              []byte
}

type testServer struct {
	*httptest.Server
	hits  atomic.Int64 // requests that reached the handler
	conns atomic.Int64 // TCP connections accepted
	port  int

	mu       sync.Mutex
	requests []seen
	handler  http.HandlerFunc
	release  chan struct{} // closed on cleanup to free blocked handlers
}

func (s *testServer) set(h http.HandlerFunc) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}
func (s *testServer) last() seen {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return seen{}
	}
	return s.requests[len(s.requests)-1]
}

// wait blocks until the server has been released, a hard stop for handlers
// that simulate a stalled provider. When the client leaves first (its deadline
// passed) the handler aborts the connection instead of returning: a return
// makes the server send a default 200, and net/http can hand that answer to a
// client whose deadline has just passed instead of the deadline error (about 1
// request in 100 on a loaded host), so a test asserting the timeout class saw
// invalid_response.
func (s *testServer) wait(r *http.Request, max time.Duration) {
	select {
	case <-s.release:
	case <-r.Context().Done():
		panic(http.ErrAbortHandler)
	case <-time.After(max):
	}
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

type fakeResolver struct {
	mu      sync.Mutex
	answers map[string][]netip.Addr
	errs    map[string]error
	panics  bool
	lookups int
	hosts   []string
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	r.mu.Lock()
	r.lookups++
	r.hosts = append(r.hosts, host)
	panics, err, answer := r.panics, r.errs[host], r.answers[host]
	r.mu.Unlock()
	if panics {
		panic("resolver defect")
	}
	if err != nil {
		return nil, err
	}
	if answer == nil {
		return nil, errors.New("no such host")
	}
	return append([]netip.Addr(nil), answer...), nil
}
func (r *fakeResolver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lookups
}
func (r *fakeResolver) answer(host string, ips ...string) {
	addrs := make([]netip.Addr, len(ips))
	for i, ip := range ips {
		addrs[i] = netip.MustParseAddr(ip)
	}
	r.mu.Lock()
	r.answers[host] = addrs
	r.mu.Unlock()
}

// fakeSecrets records every reference it is asked to resolve.
type fakeSecrets struct {
	mu     sync.Mutex
	values map[string]string
	calls  []string
	fail   bool
	panics bool
}

func (s *fakeSecrets) Resolve(_ context.Context, ref string) (string, error) {
	s.mu.Lock()
	s.calls = append(s.calls, ref)
	value, ok := s.values[ref]
	fail, panics := s.fail, s.panics
	s.mu.Unlock()
	// Failure paths deliberately embed the secret value, as a careless
	// backend might; the gateway must never surface resolver error text.
	if panics {
		panic("secret " + value)
	}
	if fail || !ok {
		return "", errors.New("lookup failed for " + value)
	}
	return value, nil
}
func (s *fakeSecrets) refs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

const (
	liveToken    = "live-token-AAAA1111"
	sandboxToken = "sandbox-token-BBBB2222"
	payToken     = "pay-live-token-CCCC3333"
	paySandbox   = "pay-sandbox-token-DDDD4444"
)

type harness struct {
	t        *testing.T
	cert     tls.Certificate
	pool     *x509.CertPool
	resolver *fakeResolver
	secrets  *fakeSecrets
	clock    *fakeClock
	gw       *Gateway
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cert, pool := newCert(t, testHosts...)
	h := &harness{t: t, cert: cert, pool: pool,
		resolver: &fakeResolver{answers: map[string][]netip.Addr{}, errs: map[string]error{}},
		secrets: &fakeSecrets{values: map[string]string{
			"crm_live": liveToken, "crm_sandbox": sandboxToken, "pay_live": payToken, "pay_sandbox": paySandbox, "geo_live": "geo-token-EEEE5555",
		}},
		clock: &fakeClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)},
	}
	for _, host := range testHosts {
		h.resolver.answer(host, "127.0.0.1")
	}
	h.rebuild(nil)
	return h
}

// rebuild constructs the gateway under test with the standard test options,
// optionally adjusted. The rate limiter state is reset.
func (h *harness) rebuild(adjust func(*GatewayOptions)) {
	h.t.Helper()
	o := GatewayOptions{Secrets: h.secrets, AllowInternal: true, RootCAs: h.pool, Resolver: h.resolver, Now: h.clock.now}
	if adjust != nil {
		adjust(&o)
	}
	gw, err := NewGateway(o)
	if err != nil {
		h.t.Fatal(err)
	}
	h.gw = gw
}

func (h *harness) server(handler http.HandlerFunc) *testServer {
	return h.serverTLS(handler, &tls.Config{Certificates: []tls.Certificate{h.cert}})
}

func (h *harness) serverTLS(handler http.HandlerFunc, config *tls.Config) *testServer {
	h.t.Helper()
	s := &testServer{release: make(chan struct{}), handler: handler}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s.mu.Lock()
		s.requests = append(s.requests, seen{method: r.Method, uri: r.RequestURI, host: r.Host, header: r.Header.Clone(), body: body})
		current := s.handler
		s.mu.Unlock()
		current(w, r)
	}))
	s.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			s.conns.Add(1)
		}
	}
	s.Config.ErrorLog = nil
	s.TLS = config
	s.StartTLS()
	s.port = s.Listener.Addr().(*net.TCPAddr).Port
	h.t.Cleanup(s.Close)
	h.t.Cleanup(func() { close(s.release) }) // runs before Close (LIFO)
	return s
}

// closedPort returns a loopback port with nothing listening.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func (h *harness) run(def Definition, mode, kind, key string, inputs map[string]any) Result {
	h.t.Helper()
	return h.gw.Execute(context.Background(), Request{Definition: def, Mode: mode, Kind: kind, Key: key, Inputs: inputs, Timeout: 2 * time.Second})
}

// assertNoIO fails if anything was resolved, authenticated or connected.
func (h *harness) assertNoIO(servers ...*testServer) {
	h.t.Helper()
	if n := h.resolver.count(); n != 0 {
		h.t.Fatalf("resolver called %d times", n)
	}
	if refs := h.secrets.refs(); len(refs) != 0 {
		h.t.Fatalf("secrets resolved: %v", refs)
	}
	for _, s := range servers {
		if s.hits.Load() != 0 || s.conns.Load() != 0 {
			h.t.Fatalf("server contacted: hits=%d conns=%d", s.hits.Load(), s.conns.Load())
		}
	}
}

func assertResult(t *testing.T, got Result, outcome Outcome, class string, retryable bool, status int) {
	t.Helper()
	if got.Outcome != outcome || got.Class != class || got.Retryable != retryable || got.Status != status {
		t.Fatalf("got %s/%s retryable=%v status=%d simulated=%v, want %s/%s retryable=%v status=%d", got.Outcome, got.Class, got.Retryable, got.Status, got.Simulated, outcome, class, retryable, status)
	}
	if outcome != Success && (len(got.Output) != 0 || got.Simulated) {
		t.Fatalf("non-success result carries output: %+v", got)
	}
}

func contains(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

const secretValue = "s3cr3t-Value_0123456789"

// writeSecret creates a credential file with an explicit mode.
func writeSecret(t *testing.T, dir, name, content string, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
