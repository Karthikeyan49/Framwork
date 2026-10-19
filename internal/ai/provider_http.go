package ai

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// HTTPJSONProvider is the one generic adapter of ADR040 section 3 (kind https-json):
// an operator-controlled HTTPS endpoint that accepts the ProviderRequest as JSON and
// answers the envelope {"payload", "model", "usage"}. It holds no vendor format and
// no credential: the credential file is read at call time and never retained. The
// network policy is that of the M10 tool gateway (tools.DeniedAddress): private,
// loopback, link-local, metadata and reserved ranges are refused at dial time on every
// resolved address and again immediately before connect(2); redirects are not
// followed and no proxy is consulted. Request and response sizes are bounded and the
// deadline is the call context's. Errors carry fixed text only: never a URL, a header,
// a credential or a body.
type HTTPJSONProvider struct {
	host      string
	port      int
	url       string
	label     string
	secrets   tools.FileSecrets
	secretRef string
	client    *http.Client
	maxBody   int64
	resolver  hostResolver
	loopback  bool
}

type hostResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// httpOptions are the test-only seams. They are unexported on purpose: configuration
// can never select them (a loopback test server needs allowLoopback and its own CA).
type httpOptions struct {
	rootCAs       *x509.CertPool
	allowLoopback bool
	resolver      hostResolver
}

var (
	errDenied  = errors.New("destination refused by the network policy")
	errResolve = errors.New("host resolution failed")
	errDial    = errors.New("connection failed")
)

const (
	maxResolvedAddresses = 32
	headerTimeout        = 30 * time.Second
)

func newHTTPJSONProvider(cfg ProviderConfig, o httpOptions) (*HTTPJSONProvider, error) {
	if cfg.Kind != KindHTTPSJSON {
		return nil, fmt.Errorf("%w: not an https-json configuration", ErrInvalidInput)
	}
	host, port, u, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if !o.allowLoopback {
		if verr := validateEndpoint(cfg.Endpoint); verr != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, verr)
		}
	}
	p := &HTTPJSONProvider{host: host, port: port, url: u.String(), label: cfg.Model, maxBody: MaxProviderBytes, resolver: o.resolver, loopback: o.allowLoopback}
	if p.resolver == nil {
		p.resolver = net.DefaultResolver
	}
	if cfg.CredentialFile != "" {
		dir, ref, cerr := credentialRef(cfg.CredentialFile)
		if cerr != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidInput, cerr)
		}
		p.secrets, p.secretRef = tools.FileSecrets{Dir: dir}, ref
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            p.dialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.rootCAs},
		ForceAttemptHTTP2:      true,
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 16 << 10,
		ResponseHeaderTimeout:  headerTimeout,
		TLSHandshakeTimeout:    10 * time.Second,
	}
	p.client = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return p, nil
}

func (p *HTTPJSONProvider) denied(ip netip.Addr) bool {
	if p.loopback && ip.Unmap().IsLoopback() {
		return false
	}
	return tools.DeniedAddress(ip, false)
}

// dialContext resolves the endpoint host, validates EVERY answer (one refused answer
// refuses the destination, so a mixed answer cannot reach an internal address on a
// later attempt) and connects to a validated literal address only.
func (p *HTTPJSONProvider) dialContext(ctx context.Context, _, addr string) (net.Conn, error) {
	if addr != net.JoinHostPort(p.host, strconv.Itoa(p.port)) {
		return nil, errDenied
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(p.host); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		type answer struct {
			addrs []netip.Addr
			err   error
		}
		ch := make(chan answer, 1)
		go func() {
			defer func() {
				if recover() != nil {
					ch <- answer{err: errResolve}
				}
			}()
			a, e := p.resolver.LookupNetIP(ctx, "ip", p.host)
			ch <- answer{a, e}
		}()
		select {
		case r := <-ch:
			addrs = r.addrs
			if r.err != nil {
				return nil, errResolve
			}
		case <-ctx.Done():
			return nil, errResolve
		}
	}
	if len(addrs) == 0 {
		return nil, errResolve
	}
	if len(addrs) > maxResolvedAddresses {
		return nil, errDenied
	}
	for _, ip := range addrs {
		if p.denied(ip) {
			return nil, errDenied
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1, ControlContext: tools.AddressGuard(p.port, p.denied)}
	for _, ip := range addrs {
		conn, err := dialer.DialContext(ctx, "tcp", netip.AddrPortFrom(ip.Unmap(), uint16(p.port)).String())
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errDial
}

type envelope struct {
	Payload json.RawMessage
	Model   string
	Usage   Usage
}

// parseEnvelope reads {"payload", "model", "usage"} with exactly those top-level
// keys, each at most once, and nothing after the object. The payload is opaque: a JSON
// string is used byte for byte (the way a text-generating endpoint answers), a JSON
// object is used as its compact encoding, anything else is malformed. The payload is
// never inspected here, so a hostile one reaches the strict proposal decoder exactly
// as a scripted one would.
func parseEnvelope(raw []byte) (envelope, error) {
	bad := fmt.Errorf("%w: unreadable envelope", ErrProviderMalformed)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return envelope{}, bad
	}
	var env envelope
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		if err != nil || !ok || seen[key] {
			return envelope{}, bad
		}
		seen[key] = true
		var value json.RawMessage
		if err = dec.Decode(&value); err != nil {
			return envelope{}, bad
		}
		switch key {
		case "payload":
			env.Payload = value
		case "model":
			if json.Unmarshal(value, &env.Model) != nil {
				return envelope{}, bad
			}
		case "usage":
			u := json.NewDecoder(bytes.NewReader(value))
			u.DisallowUnknownFields()
			if u.Decode(&env.Usage) != nil {
				return envelope{}, bad
			}
		default:
			return envelope{}, bad
		}
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return envelope{}, bad
	}
	if _, err := dec.Token(); err != io.EOF || !seen["payload"] {
		return envelope{}, bad
	}
	var text string
	switch {
	case json.Unmarshal(env.Payload, &text) == nil:
		env.Payload = json.RawMessage(text)
	case len(env.Payload) > 0 && env.Payload[0] == '{':
		var compact bytes.Buffer
		if json.Compact(&compact, env.Payload) != nil {
			return envelope{}, bad
		}
		env.Payload = compact.Bytes()
	default:
		return envelope{}, bad
	}
	return env, nil
}

// Complete sends one request. It never retries and never logs.
func (p *HTTPJSONProvider) Complete(ctx context.Context, req ProviderRequest) (ProviderResponse, error) {
	body, err := json.Marshal(req)
	if err != nil || int64(len(body)) > p.maxBody {
		return ProviderResponse{}, fmt.Errorf("%w: request cannot be sent", ErrProviderRefused)
	}
	if _, has := ctx.Deadline(); !has {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultWallClock)
		defer cancel()
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return ProviderResponse{}, fmt.Errorf("%w: request cannot be built", ErrProviderRefused)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if p.secretRef != "" {
		credential, serr := p.secrets.Resolve(ctx, p.secretRef)
		if serr != nil {
			return ProviderResponse{}, fmt.Errorf("%w: credential unavailable", ErrProviderUnavailable)
		}
		httpReq.Header.Set("Authorization", "Bearer "+credential)
	}
	started := time.Now()
	resp, err := p.client.Do(httpReq)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded:
			return ProviderResponse{}, fmt.Errorf("%w: deadline exceeded", ErrProviderTimeout)
		case ctx.Err() != nil:
			return ProviderResponse{}, ctx.Err()
		case errors.Is(err, errDenied):
			return ProviderResponse{}, fmt.Errorf("%w: destination refused by the network policy", ErrProviderRefused)
		}
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return ProviderResponse{}, fmt.Errorf("%w: timed out", ErrProviderTimeout)
		}
		return ProviderResponse{}, fmt.Errorf("%w: transport failure", ErrProviderUnavailable)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 && resp.StatusCode != http.StatusGatewayTimeout:
		return ProviderResponse{}, fmt.Errorf("%w: status %d", ErrProviderUnavailable, resp.StatusCode)
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusGatewayTimeout:
		return ProviderResponse{}, fmt.Errorf("%w: status %d", ErrProviderTimeout, resp.StatusCode)
	default: // 3xx (a redirect is never followed), 4xx
		return ProviderResponse{}, fmt.Errorf("%w: status %d", ErrProviderRefused, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, p.maxBody+1))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return ProviderResponse{}, fmt.Errorf("%w: deadline exceeded", ErrProviderTimeout)
		}
		return ProviderResponse{}, fmt.Errorf("%w: response not readable", ErrProviderUnavailable)
	}
	if int64(len(raw)) > p.maxBody {
		return ProviderResponse{}, fmt.Errorf("%w: response larger than %d bytes", ErrProviderMalformed, p.maxBody)
	}
	env, err := parseEnvelope(raw)
	if err != nil {
		return ProviderResponse{}, err
	}
	model := clip(env.Model, MaxModelLabelRunes)
	if model == "" {
		model = p.label
	}
	return ProviderResponse{Payload: env.Payload, Provider: "https-json", Model: model, Usage: env.Usage, Latency: time.Since(started)}, nil
}
