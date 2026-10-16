package tools

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MaxResponseHeaderBytes bounds provider response headers.
const MaxResponseHeaderBytes = 16 << 10

const gatewayUserAgent = "framework-tool-gateway/1"

// GatewayOptions configures a Gateway. Zero values are the production
// posture: system trust roots, public networks only, the system resolver and
// wall-clock time.
type GatewayOptions struct {
	// Secrets resolves endpoint secret_ref values at send time. A nil
	// resolver makes every secret-bearing endpoint fail as not_sent.
	Secrets SecretResolver
	// AllowInternal is the process-level opt-in for endpoints declared
	// network "internal". Production composition must reject it (ADR036 #8).
	AllowInternal bool
	// RootCAs replaces the system roots when non-nil (operator CA file).
	RootCAs *x509.CertPool
	// Resolver resolves endpoint hosts; nil uses net.DefaultResolver. Every
	// answer is still validated by DeniedAddress before any connection.
	Resolver HostResolver
	// Now supplies time for the local rate limiter; nil uses time.Now.
	Now func() time.Time
}

// Gateway is the governed M10 executor: one HTTPS connector with SSRF
// policy, bounded responses, mode handling and a local rate limiter. It is
// safe for concurrent use and keeps no per-request state between calls.
type Gateway struct {
	secrets       SecretResolver
	allowInternal bool
	resolver      HostResolver
	now           func() time.Time
	dialer        net.Dialer
	transport     *http.Transport
	client        *http.Client
	limiter       *rateLimiter
}

var _ Executor = (*Gateway)(nil)

// NewGateway builds a dedicated transport: no proxy (environment proxy
// variables are ignored), no redirects, no compression, no keep-alive reuse
// (so one Execute is at most one transmission and the transport never
// replays a request), HTTP/1.1 only, TLS 1.2+ with verification.
func NewGateway(o GatewayOptions) (*Gateway, error) {
	g := &Gateway{secrets: o.Secrets, allowInternal: o.AllowInternal, resolver: o.Resolver, now: o.Now, limiter: newRateLimiter(), dialer: newDialer()}
	if g.resolver == nil {
		g.resolver = net.DefaultResolver
	}
	if g.now == nil {
		g.now = time.Now
	}
	g.transport = &http.Transport{
		Proxy:                  nil,
		DialContext:            g.dialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: o.RootCAs},
		TLSHandshakeTimeout:    MaxTimeoutMS * time.Millisecond,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		MaxResponseHeaderBytes: MaxResponseHeaderBytes,
		TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	g.client = &http.Client{
		Transport:     g.transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return g, nil
}

// attempt is the per-Execute state shared with the dialer and trace hooks.
type attempt struct {
	host      string
	port      int
	dialAddr  string
	internal  bool
	deadline  time.Time
	denied    atomic.Bool // network policy refused the destination
	connected atomic.Bool // a connection was handed to the request: bytes may be written
}

func failedResult(class string, retryable bool, status int) Result {
	return Result{Outcome: Failed, Class: class, Retryable: retryable, Status: status}
}
func blockedResult() Result { return failedResult(ClassBlocked, false, 0) }
func notSentResult() Result { return failedResult(ClassNotSent, true, 0) }
func unknownResult(class string, status int) Result {
	return Result{Outcome: Unknown, Class: class, Status: status}
}

func knownMode(mode string) bool {
	switch mode {
	case ModeLive, ModeReroute, ModeTestAccount, ModeReadOnly, ModeSimulate, ModeBlock:
		return true
	}
	return false
}

func spanLabel(value string, valid bool) string {
	if valid {
		return value
	}
	return "invalid"
}

// Execute performs at most one bounded attempt. It never panics, returns no
// error text and never includes credentials, URLs or bodies in its Result or
// span. Blocking is bounded by min(Request.Timeout, connector timeout_ms,
// MaxTimeoutMS); a non-positive Request.Timeout leaves no budget and fails
// as not_sent without I/O.
func (g *Gateway) Execute(ctx context.Context, r Request) (result Result) {
	if ctx == nil {
		return blockedResult()
	}
	def := r.Definition
	ctx, span := otel.Tracer("framework/tools").Start(ctx, "tool.execute", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("tool_ref", spanLabel(def.Ref, len(def.Ref) <= 160 && refPattern.MatchString(def.Ref))),
		attribute.String("tool_version", spanLabel(def.Version, semverPattern.MatchString(def.Version))),
		attribute.String("tool_mode", spanLabel(r.Mode, knownMode(r.Mode))),
		attribute.String("tool_kind", spanLabel(r.Kind, r.Kind == KindSend || r.Kind == KindLookup)),
	))
	a := &attempt{}
	defer func() {
		span.SetAttributes(attribute.String("outcome", string(result.Outcome)), attribute.String("class", result.Class), attribute.Int("http_status", result.Status))
		if result.Outcome != Success {
			span.SetStatus(codes.Error, result.Class)
		}
		span.End()
	}()
	defer func() {
		if recover() != nil {
			// A defect must not claim a request was never transmitted.
			if a.connected.Load() {
				result = unknownResult(ClassTransport, 0)
			} else {
				result = blockedResult()
			}
		}
	}()
	if g == nil || g.client == nil {
		return blockedResult()
	}
	return g.execute(ctx, r, a)
}

func (g *Gateway) execute(ctx context.Context, r Request, a *attempt) Result {
	def := r.Definition
	// Defense in depth: the definition must still satisfy catalog rules even
	// if a caller built it by hand.
	if _, err := validateDefinition(def); err != nil || !knownMode(r.Mode) || r.Mode == ModeBlock {
		return blockedResult()
	}
	out, ok := buildOutbound(def, r)
	if !ok {
		return blockedResult()
	}
	if r.Mode == ModeSimulate {
		// Declared fixture only; flagged so it is never delivery evidence.
		// A tool whose sandbox is not SIMULATE has no fixture to return.
		if def.Sandbox.Mode != ModeSimulate {
			return blockedResult()
		}
		output, err := DecodeOutput(def.Output, def.Sandbox.Output)
		if err != nil {
			return blockedResult()
		}
		return Result{Outcome: Success, Class: ClassOK, Output: output, Simulated: true}
	}
	ep, ok := effectiveEndpoint(def, r.Mode)
	if !ok {
		return blockedResult()
	}
	internal := ep.Network == "internal"
	if internal && !g.allowInternal {
		return blockedResult()
	}
	timeout := effectiveTimeout(def.Connector.TimeoutMS, r.Timeout)
	if timeout <= 0 || ctx.Err() != nil {
		return notSentResult()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if !g.limiter.allow(key(def.Ref, def.Version), def.Connector.RatePerSecond, g.now()) {
		return failedResult(ClassRateLimited, true, 0)
	}
	secret := ""
	if ep.SecretRef != "" {
		value, ok := g.resolveSecret(ctx, ep.SecretRef)
		if !ok {
			return notSentResult()
		}
		secret = value
	}
	deadline, _ := ctx.Deadline()
	a.host, a.port, a.dialAddr, a.internal, a.deadline = ep.Host, ep.Port, endpointAddr(ep), internal, deadline
	return g.send(ctx, def, r, ep, out, secret, a)
}

// effectiveEndpoint selects the destination and credential for a mode.
// Sandbox modes only ever use the sandbox endpoint; only TEST_ACCOUNT sends its
// own sandbox secret_ref (REROUTE and READ_ONLY send no credential, also when a
// sandbox ceiling narrowed a TEST_ACCOUNT tool). The production secret_ref is
// never consulted.
func effectiveEndpoint(def Definition, mode string) (Endpoint, bool) {
	production := def.Connector.Endpoint
	if mode == ModeLive {
		return production, true
	}
	s := def.Sandbox.Endpoint
	if s == nil || s.Host == production.Host || s.SecretRef != "" && s.SecretRef == production.SecretRef {
		return Endpoint{}, false
	}
	endpoint := *s
	switch mode {
	case ModeReroute:
		// A ceiling narrowed a TEST_ACCOUNT tool to REROUTE: the endpoint stays, the
		// test-account credential does not.
		endpoint.SecretRef = ""
	case ModeTestAccount:
		if s.SecretRef == "" {
			return Endpoint{}, false
		}
	case ModeReadOnly:
		if def.SideEffect != "NONE" && def.SideEffect != "READ" {
			return Endpoint{}, false
		}
		endpoint.SecretRef = ""
	default:
		return Endpoint{}, false
	}
	return endpoint, true
}

func effectiveTimeout(definitionMS int, requested time.Duration) time.Duration {
	limit := time.Duration(min(definitionMS, MaxTimeoutMS)) * time.Millisecond
	if requested < limit {
		return requested
	}
	return limit
}

// resolveSecret bounds a resolver that ignores its context and contains a
// resolver panic. The value is returned only to build the bearer header.
func (g *Gateway) resolveSecret(ctx context.Context, ref string) (string, bool) {
	if g.secrets == nil {
		return "", false
	}
	type resolved struct {
		value string
		ok    bool
	}
	ch := make(chan resolved, 1)
	go func() {
		defer func() {
			if recover() != nil {
				ch <- resolved{}
			}
		}()
		value, err := g.secrets.Resolve(ctx, ref)
		ch <- resolved{value: value, ok: err == nil && validSecretValue(value)}
	}()
	select {
	case r := <-ch:
		if !r.ok {
			return "", false
		}
		return r.value, true
	case <-ctx.Done():
		return "", false
	}
}

func (g *Gateway) send(ctx context.Context, def Definition, r Request, ep Endpoint, out outbound, secret string, a *attempt) Result {
	hostport := ep.Host
	if ep.Port != 443 {
		hostport = endpointAddr(ep)
	}
	target := "https://" + hostport + out.path
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || u.Host != hostport || u.EscapedPath() != out.path || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return blockedResult()
	}
	var body io.Reader
	if out.body != nil {
		body = bytes.NewReader(out.body)
	}
	ctx = context.WithValue(ctx, attemptKey{}, a)
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		// GotConn precedes the first request byte; before it nothing of
		// this request can have been written.
		GotConn: func(httptrace.GotConnInfo) { a.connected.Store(true) },
	})
	req, err := http.NewRequestWithContext(ctx, out.method, target, body)
	if err != nil {
		return blockedResult()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", gatewayUserAgent)
	if out.body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if out.idempotencyHeader != "" {
		req.Header.Set(out.idempotencyHeader, r.Key)
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		// The error text (which embeds the URL) is deliberately discarded.
		return transportFailure(ctx, a)
	}
	defer resp.Body.Close()
	return classifyResponse(ctx, def, r.Kind, resp)
}

func transportFailure(ctx context.Context, a *attempt) Result {
	switch {
	case a.denied.Load():
		return blockedResult()
	case !a.connected.Load():
		// DNS, dial or TLS handshake failure, or the deadline or a caller
		// cancellation arrived before a connection existed.
		return notSentResult()
	}
	return unknownResult(ctxClass(ctx), 0)
}

// ctxClass labels an UNKNOWN outcome after transmission began: a deadline is
// a timeout; anything else (broken connection, caller cancellation) is a
// transport failure.
func ctxClass(ctx context.Context) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ClassTimeout
	}
	return ClassTransport
}

// classifyResponse maps a received response to an outcome (ADR036 #6).
func classifyResponse(ctx context.Context, def Definition, kind string, resp *http.Response) Result {
	status := resp.StatusCode
	switch {
	case status >= 200 && status <= 299:
		limit := min(def.Connector.MaxResponseBytes, MaxResponseBytes)
		raw, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
		if err != nil {
			return unknownResult(ctxClass(ctx), status)
		}
		if len(raw) > limit {
			return unknownResult(ClassInvalidResponse, status)
		}
		fields, ok := parseFlatObject(raw)
		if !ok {
			return unknownResult(ClassInvalidResponse, status)
		}
		output, err := DecodeOutput(def.Output, fields)
		if err != nil {
			return unknownResult(ClassInvalidResponse, status)
		}
		return Result{Outcome: Success, Class: ClassOK, Status: status, Output: output}
	case kind == KindLookup && status == http.StatusNotFound:
		return failedResult(ClassNotFound, false, status)
	case status == http.StatusTooManyRequests:
		return failedResult(ClassRateLimited, true, status)
	case status == http.StatusRequestTimeout || status == http.StatusConflict || status >= 500 && status <= 599:
		return unknownResult(ClassServerError, status)
	case status >= 400 && status <= 499:
		return failedResult(ClassRejected, false, status)
	case def.SideEffect == "WRITE":
		// 1xx/3xx/nonstandard: redirects are never followed and a write may
		// already have taken effect.
		return unknownResult(ClassServerError, status)
	}
	return failedResult(ClassRejected, false, status)
}
