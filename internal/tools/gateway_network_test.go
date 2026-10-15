package tools

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestPublicEndpointResolvingToDeniedAddressIsBlockedWithoutConnecting(t *testing.T) {
	denied := []string{
		"127.0.0.1", "127.8.9.10", "::1", "10.0.0.5", "172.16.5.5", "192.168.1.1", "169.254.169.254", "169.254.10.10",
		"100.64.0.1", "0.0.0.0", "::", "224.0.0.1", "255.255.255.255", "fd00::1", "fe80::1", "::ffff:127.0.0.1",
		"::ffff:10.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "192.0.2.1", "198.18.0.1", "2001:db8::1",
	}
	for _, ip := range denied {
		t.Run(ip, func(t *testing.T) {
			h := newHarness(t)
			prod := h.server(reply(201, okWrite))
			def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", prod.port+1)
			def.Connector.Network = "public"
			h.resolver.answer("api.example.com", ip)
			started := time.Now()
			got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
			assertResult(t, got, Failed, ClassBlocked, false, 0)
			if prod.conns.Load() != 0 || prod.hits.Load() != 0 || time.Since(started) > time.Second {
				t.Fatalf("connection attempted: conns=%d elapsed=%v", prod.conns.Load(), time.Since(started))
			}
		})
	}
}

func TestOneDeniedAnswerRefusesTheWholeDestination(t *testing.T) {
	h := newHarness(t)
	prod := h.server(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", prod.port+1)
	// Public endpoint: a public answer cannot launder a loopback answer.
	def.Connector.Network = "public"
	for _, answers := range [][]string{{"8.8.8.8", "127.0.0.1"}, {"127.0.0.1", "8.8.8.8"}, {"8.8.8.8", "::ffff:10.0.0.1"}} {
		h.resolver.answer("api.example.com", answers...)
		assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	}
	// Internal endpoint: allowed internal addresses may be mixed, metadata and
	// other never-allowed addresses may not.
	def.Connector.Network = "internal"
	h.resolver.answer("api.example.com", "127.0.0.1", "169.254.169.254")
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	h.resolver.answer("api.example.com", "127.0.0.1", "fd00:ec2::254")
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	h.resolver.answer("api.example.com", "127.0.0.1", "0.0.0.0")
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	h.resolver.answer("api.example.com", "224.0.0.1")
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	if prod.conns.Load() != 0 {
		t.Fatal("connected despite a denied answer")
	}
	h.resolver.answer("api.example.com", "127.0.0.1", "10.0.0.9")
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Success, ClassOK, false, 201)
}

func TestInternalEndpointRequiresProcessOptIn(t *testing.T) {
	h := newHarness(t)
	h.rebuild(func(o *GatewayOptions) { o.AllowInternal = false })
	prod, sandbox := h.pair(reply(201, okWrite))
	write := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	assertResult(t, h.run(write, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	assertResult(t, h.run(write, ModeReroute, KindSend, testKey, writeInputs()), Failed, ClassBlocked, false, 0)
	h.assertNoIO(prod, sandbox)

}

func TestSandboxEndpointResolvingToLoopbackIsDeniedWhenPublic(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	def.Sandbox.Endpoint.Network = "public"
	got := h.run(def, ModeReroute, KindSend, testKey, writeInputs())
	assertResult(t, got, Failed, ClassBlocked, false, 0)
	if sandbox.conns.Load() != 0 || prod.conns.Load() != 0 {
		t.Fatal("sandbox policy bypassed")
	}
}

func TestResolutionIsOneShotSoRebindingCannotChangeTheDestination(t *testing.T) {
	h := newHarness(t)
	prod := h.server(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", prod.port+1)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Success, ClassOK, false, 201)
	if h.resolver.count() != 1 {
		t.Fatalf("host resolved %d times for one request", h.resolver.count())
	}
	h.resolver.mu.Lock()
	if h.resolver.hosts[0] != "api.example.com" {
		t.Fatalf("resolved %q", h.resolver.hosts[0])
	}
	h.resolver.mu.Unlock()
}

func TestDialToClosedPortAndDNSFailureAreNotSent(t *testing.T) {
	h := newHarness(t)
	def := writeDefinition("api.example.com", closedPort(t), "sandbox.example.com", closedPort(t))
	started := time.Now()
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)
	if time.Since(started) > 2*time.Second {
		t.Fatal("refused connection was slow")
	}

	h.resolver.mu.Lock()
	delete(h.resolver.answers, "api.example.com")
	h.resolver.mu.Unlock()
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)

	h.resolver.mu.Lock()
	h.resolver.errs["api.example.com"] = errors.New("SERVFAIL for api.example.com")
	h.resolver.mu.Unlock()
	got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
	assertResult(t, got, Failed, ClassNotSent, true, 0)

	h.resolver.mu.Lock()
	delete(h.resolver.errs, "api.example.com")
	h.resolver.answers["api.example.com"] = []netip.Addr{}
	h.resolver.mu.Unlock()
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)

	// A panicking resolver is contained and still means "never sent".
	h.resolver.mu.Lock()
	h.resolver.panics = true
	h.resolver.mu.Unlock()
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)
}

type blockingResolver struct{ release chan struct{} }

func (r blockingResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	select {
	case <-r.release:
	case <-time.After(10 * time.Second):
	}
	return nil, errors.New("late")
}

func TestResolverIgnoringContextCannotExtendTheDeadline(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	h.rebuild(func(o *GatewayOptions) { o.Resolver = blockingResolver{release} })
	def := writeDefinition("api.example.com", closedPort(t), "sandbox.example.com", closedPort(t))
	started := time.Now()
	got := h.gw.Execute(context.Background(), Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: 200 * time.Millisecond})
	assertResult(t, got, Failed, ClassNotSent, true, 0)
	if time.Since(started) > time.Second {
		t.Fatalf("resolver stalled the call for %v", time.Since(started))
	}
}

func TestEnvironmentProxyIsNeverUsed(t *testing.T) {
	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	connections := make(chan struct{}, 16)
	go func() {
		for {
			c, err := proxy.Accept()
			if err != nil {
				return
			}
			connections <- struct{}{}
			c.Close()
		}
	}()
	url := "http://" + proxy.Addr().String()
	for _, name := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		t.Setenv(name, url)
	}
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	h := newHarness(t)
	if h.gw.transport.Proxy != nil {
		t.Fatal("transport has a proxy function")
	}
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Success, ClassOK, false, 201)
	select {
	case <-connections:
		t.Fatal("request was routed through the environment proxy")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestTransportPostureIsHardened(t *testing.T) {
	h := newHarness(t)
	tr := h.gw.transport
	if tr.Proxy != nil || !tr.DisableCompression || !tr.DisableKeepAlives || tr.MaxResponseHeaderBytes != MaxResponseHeaderBytes || tr.ForceAttemptHTTP2 {
		t.Fatalf("transport %+v", tr)
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 || tr.TLSClientConfig.InsecureSkipVerify || tr.TLSClientConfig.RootCAs != h.pool {
		t.Fatalf("tls %+v", tr.TLSClientConfig)
	}
	if tr.DialContext == nil || tr.DialTLSContext != nil || tr.Dial != nil || tr.DialTLS != nil {
		t.Fatal("only the policy dialer may open connections")
	}
	if err := h.gw.client.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatal("redirects must not be followed")
	}
	if len(tr.TLSNextProto) != 0 {
		t.Fatal("HTTP/2 must stay disabled")
	}
}

func TestTLSVerificationFailuresAreNotSent(t *testing.T) {
	h := newHarness(t)
	prod, sandbox := h.pair(reply(201, okWrite))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)

	// Server certificate not in the trust pool (also the default-roots case).
	_, otherPool := newCert(t, testHosts...)
	for name, pool := range map[string]*x509.CertPool{"other CA": otherPool, "empty pool": x509.NewCertPool(), "system roots": nil} {
		h.rebuild(func(o *GatewayOptions) { o.RootCAs = pool })
		got := h.run(def, ModeLive, KindSend, testKey, writeInputs())
		if got.Outcome != Failed || got.Class != ClassNotSent || !got.Retryable {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if prod.hits.Load() != 0 {
		t.Fatal("request reached a server with an unverified certificate")
	}

	// Certificate valid for other names only.
	mismatch, mismatchPool := newCert(t, "elsewhere.example.org")
	wrong := h.serverTLS(reply(201, okWrite), &tls.Config{Certificates: []tls.Certificate{mismatch}})
	h.rebuild(func(o *GatewayOptions) { o.RootCAs = mismatchPool })
	def = writeDefinition("api.example.com", wrong.port, "sandbox.example.com", wrong.port+1)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)

	// A server that only speaks TLS 1.1 is refused (minimum is TLS 1.2).
	h.rebuild(nil)
	old := h.serverTLS(reply(201, okWrite), &tls.Config{Certificates: []tls.Certificate{h.cert}, MaxVersion: tls.VersionTLS11})
	def = writeDefinition("api.example.com", old.port, "sandbox.example.com", old.port+1)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)
	if wrong.hits.Load() != 0 || old.hits.Load() != 0 {
		t.Fatal("request written over a failed handshake")
	}

	// A plaintext HTTP listener on the endpoint port is not a TLS server.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				buf := make([]byte, 1024)
				_, _ = c.Read(buf)
				_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n{}"))
				c.Close()
			}()
		}
	}()
	def = writeDefinition("api.example.com", l.Addr().(*net.TCPAddr).Port, "sandbox.example.com", 1)
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, writeInputs()), Failed, ClassNotSent, true, 0)
}

func TestTLSHandshakeStallIsNotSentAndBounded(t *testing.T) {
	h := newHarness(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	stalled := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			stalled <- c // accept but never speak
		}
	}()
	defer func() {
		for {
			select {
			case c := <-stalled:
				c.Close()
			default:
				return
			}
		}
	}()
	def := writeDefinition("api.example.com", l.Addr().(*net.TCPAddr).Port, "sandbox.example.com", 1)
	started := time.Now()
	got := h.gw.Execute(context.Background(), Request{Definition: def, Mode: ModeLive, Kind: KindSend, Key: testKey, Inputs: writeInputs(), Timeout: 250 * time.Millisecond})
	assertResult(t, got, Failed, ClassNotSent, true, 0)
	if time.Since(started) > time.Second {
		t.Fatalf("handshake stall took %v", time.Since(started))
	}
}

func TestPublicEndpointRejectsAddressesRevalidatedAtConnect(t *testing.T) {
	// The connect-time guard re-validates the literal socket address even if
	// the resolution step were bypassed.
	a := &attempt{port: 443, internal: false}
	ctx := context.WithValue(context.Background(), attemptKey{}, a)
	for _, address := range []string{"127.0.0.1:443", "[::1]:443", "10.0.0.1:443", "169.254.169.254:443", "8.8.8.8:444", "not-an-address"} {
		if err := controlContext(ctx, "tcp", address, nil); err == nil {
			t.Errorf("%s allowed", address)
		}
	}
	if err := controlContext(ctx, "tcp", "8.8.8.8:443", nil); err != nil {
		t.Error("public address refused")
	}
	if err := controlContext(context.Background(), "tcp", "8.8.8.8:443", nil); err == nil {
		t.Error("missing attempt context allowed")
	}
	g, _ := NewGateway(GatewayOptions{})
	if _, err := g.dialContext(context.Background(), "tcp", "api.example.com:443"); err == nil {
		t.Error("dial without attempt context")
	}
	a2 := &attempt{host: "api.example.com", port: 443, dialAddr: "api.example.com:443", deadline: time.Now().Add(time.Second)}
	ctx = context.WithValue(context.Background(), attemptKey{}, a2)
	if _, err := g.dialContext(ctx, "tcp", "evil.example.org:443"); err == nil || !a2.denied.Load() {
		t.Error("dial to a different address than the attempt's endpoint")
	}
}

func TestSpanCarriesOnlyBoundedAttributes(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	h := newHarness(t)
	prod, sandbox := h.pair(reply(400, `{"error":"`+liveToken+`"}`))
	def := writeDefinition("api.example.com", prod.port, "sandbox.example.com", sandbox.port)
	inputs := writeInputs()
	inputs["customer_id"] = "very-secret-customer"
	assertResult(t, h.run(def, ModeLive, KindSend, testKey, inputs), Failed, ClassRejected, false, 400)
	assertResult(t, h.run(def, "bogus <mode> https://evil.example", KindSend, testKey, inputs), Failed, ClassBlocked, false, 0)

	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("%d spans", len(spans))
	}
	allowed := map[attribute.Key]bool{"tool_ref": true, "tool_version": true, "tool_mode": true, "tool_kind": true, "outcome": true, "class": true, "http_status": true}
	for _, span := range spans {
		if span.Name() != "tool.execute" {
			t.Fatalf("span %q", span.Name())
		}
		for _, kv := range span.Attributes() {
			if !allowed[kv.Key] {
				t.Errorf("unexpected attribute %s", kv.Key)
			}
			value := kv.Value.Emit()
			if contains(value, liveToken, "very-secret-customer", "example.com", "/v1/", "bogus", "Bearer", testKey, strconv.Itoa(prod.port)) {
				t.Errorf("attribute %s leaks: %q", kv.Key, value)
			}
		}
		if contains(span.Status().Description, liveToken, "example.com") {
			t.Errorf("status leaks %q", span.Status().Description)
		}
	}
	got := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	want := map[string]string{"tool_ref": "tool.crm.create_note", "tool_version": "1.0.0", "tool_mode": "LIVE", "tool_kind": "SEND", "outcome": "FAILED", "class": "rejected", "http_status": "400"}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attribute %s = %q, want %q", k, got[k], v)
		}
	}
	invalid := map[string]string{}
	for _, kv := range spans[1].Attributes() {
		invalid[string(kv.Key)] = kv.Value.Emit()
	}
	if invalid["tool_mode"] != "invalid" || strings.Contains(invalid["class"], " ") {
		t.Errorf("unbounded mode label: %v", invalid)
	}
}

func TestConcurrentExecutionIsRaceFree(t *testing.T) {
	h := newHarness(t)
	def, prod, _ := h.writeFixture(reply(201, okWrite))
	def.Connector.RatePerSecond = 100
	done := make(chan Result, 24)
	for i := 0; i < 24; i++ {
		go func(i int) {
			inputs := writeInputs()
			inputs["customer_id"] = "c" + strconv.Itoa(i)
			done <- h.run(def, ModeLive, KindSend, testKey, inputs)
		}(i)
	}
	for i := 0; i < 24; i++ {
		if r := <-done; r.Outcome != Success {
			t.Fatalf("%+v", r)
		}
	}
	if prod.hits.Load() != 24 {
		t.Fatalf("hits %d", prod.hits.Load())
	}
}
