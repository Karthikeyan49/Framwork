package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var ErrLoginUnavailable = errors.New("sign-in service unavailable")
var ErrLoginBusy = errors.New("sign-in admission exceeded")

const LoginFlowTTL = 5 * time.Minute

type Provider struct {
	ClientID         string
	AuthorizationURL string
	TokenURL         string
	RedirectURI      string
	Scopes           []string
	Development      bool
}

// ValidateProvider checks operator configuration only; requests cannot select endpoints.
func ValidateProvider(p Provider) error {
	if len(p.ClientID) == 0 || len(p.ClientID) > 200 || strings.ContainsAny(p.ClientID, "\r\n\x00") || len(p.Scopes) == 0 || len(p.Scopes) > 16 {
		return errors.New("invalid sign-in configuration")
	}
	openid := false
	seen := map[string]bool{}
	for _, scope := range p.Scopes {
		if scope == "openid" {
			openid = true
		}
		if len(scope) == 0 || len(scope) > 200 || scope == "offline_access" || seen[scope] {
			return errors.New("invalid sign-in scopes")
		}
		seen[scope] = true
		for _, r := range scope {
			if r < 0x21 || r > 0x7e || r == '"' || r == '\\' {
				return errors.New("invalid sign-in scopes")
			}
		}
	}
	if !openid {
		return errors.New("openid scope required")
	}
	for _, raw := range []string{p.AuthorizationURL, p.TokenURL, p.RedirectURI} {
		u, e := url.Parse(raw)
		if e != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(raw, "\\\r\n") || (u.Scheme != "https" && !(p.Development && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
			return errors.New("invalid sign-in endpoint")
		}
	}
	redirect, _ := url.Parse(p.RedirectURI)
	if redirect.Path != "/auth/callback" || redirect.RawPath != "" {
		return errors.New("sign-in redirect must use /auth/callback")
	}
	return nil
}

type LoginFlow struct {
	State         string `json:"state"`
	Nonce         string `json:"nonce"`
	Verifier      string `json:"verifier"`
	Configuration string `json:"configuration"`
	CreatedAt     int64  `json:"created_at"`
}

// Stores provide bounded admission and atomic destructive reads, never retries.
type LoginFlowStore interface {
	SaveLoginFlow(context.Context, string, string, LoginFlow) error
	TakeLoginFlow(context.Context, string) (LoginFlow, error)
}

type BrowserLogin struct {
	provider      Provider
	auth          *Verifier
	flows         LoginFlowStore
	oauth         oauth2.Config
	oidc          *oidc.IDTokenVerifier
	configuration string
	client        *http.Client
}

type pinnedIDKeys struct{ verifier *Verifier }

func (l *BrowserLogin) Close() { l.client.CloseIdleConnections() }

func (k pinnedIDKeys) VerifySignature(_ context.Context, raw string) ([]byte, error) {
	return k.verifier.signedPayload(raw, true)
}

type loginTransport struct{ base http.RoundTripper }

func (t loginTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, ErrLoginUnavailable
	}
	media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		response.Body.Close()
		return nil, ErrLoginUnavailable
	}
	response.Body = http.MaxBytesReader(nil, response.Body, 32768)
	return response, nil
}

func NewBrowserLogin(p Provider, auth *Verifier, flows LoginFlowStore) (*BrowserLogin, error) {
	if err := ValidateProvider(p); err != nil {
		return nil, err
	}
	if auth == nil || flows == nil {
		return nil, errors.New("sign-in dependencies required")
	}
	p.Scopes = append([]string(nil), p.Scopes...)
	raw, _ := json.Marshal(struct {
		Provider         Provider
		Issuer, Audience string
		Skew             time.Duration
	}{p, auth.issuer, auth.audience, auth.clockSkew})
	hash := sha256.Sum256(raw)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 2 * time.Second
	transport.ResponseHeaderTimeout = 3 * time.Second
	transport.MaxConnsPerHost = 4
	transport.MaxIdleConns = 4
	transport.IdleConnTimeout = 30 * time.Second
	return &BrowserLogin{provider: p, auth: auth, flows: flows, configuration: hex.EncodeToString(hash[:]),
		oauth: oauth2.Config{ClientID: p.ClientID, RedirectURL: p.RedirectURI, Scopes: p.Scopes, Endpoint: oauth2.Endpoint{AuthURL: p.AuthorizationURL, TokenURL: p.TokenURL, AuthStyle: oauth2.AuthStyleInParams}},
		// Only library time checks are replaced: framework validation below applies
		// the same explicit exp/iat/nbf policy to ID and API tokens, with no default 5m leeway.
		oidc:   oidc.NewVerifier(auth.issuer, pinnedIDKeys{auth}, &oidc.Config{ClientID: p.ClientID, SupportedSigningAlgs: []string{oidc.RS256}, SkipExpiryCheck: true}),
		client: &http.Client{Timeout: 5 * time.Second, Transport: loginTransport{transport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}
func (l *BrowserLogin) BrowserOrigin() string {
	u, _ := url.Parse(l.provider.RedirectURI)
	return u.Scheme + "://" + u.Host
}
func (l *BrowserLogin) SecureCookie() bool {
	return !l.provider.Development || strings.HasPrefix(l.provider.RedirectURI, "https:")
}

func loginRandom() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", ErrLoginUnavailable
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func ValidLoginBinding(v string) bool {
	b, e := base64.RawURLEncoding.DecodeString(v)
	return e == nil && len(b) == 32 && len(v) == 43
}
func flowKey(binding, state string) string {
	h := sha256.Sum256([]byte(binding + ":" + state))
	return hex.EncodeToString(h[:])
}

// Start returns only a redirect and browser binding. Verifier and nonce remain server-side.
func (l *BrowserLogin) Start(ctx context.Context, remote string) (location, binding string, err error) {
	if len(remote) == 0 || len(remote) > 255 {
		return "", "", ErrUnauthenticated
	}
	state, e := loginRandom()
	if e != nil {
		return "", "", e
	}
	nonce, e := loginRandom()
	if e != nil {
		return "", "", e
	}
	binding, e = loginRandom()
	if e != nil {
		return "", "", e
	}
	verifier, e := loginRandom()
	if e != nil {
		return "", "", e
	}
	f := LoginFlow{State: state, Nonce: nonce, Verifier: verifier, Configuration: l.configuration, CreatedAt: l.auth.now().Unix()}
	remoteHash := sha256.Sum256([]byte(remote))
	if e = l.flows.SaveLoginFlow(ctx, flowKey(binding, state), hex.EncodeToString(remoteHash[:]), f); e != nil {
		return "", "", e
	}
	return l.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), binding, nil
}

// Complete consumes exactly one browser-bound flow before exchanging a code.
// Exchange errors are not retried and provider error bodies never leave this port.
func (l *BrowserLogin) Complete(ctx context.Context, binding, state, code, issuer string) (string, error) {
	if !ValidLoginBinding(binding) || !ValidLoginBinding(state) || code == "" || len(code) > 4096 || strings.ContainsAny(code, "\r\n\x00") || issuer != "" && issuer != l.auth.issuer {
		return "", ErrUnauthenticated
	}
	flow, err := l.flows.TakeLoginFlow(ctx, flowKey(binding, state))
	if err != nil {
		return "", err
	}
	now := l.auth.now().Unix()
	if subtle.ConstantTimeCompare([]byte(flow.State), []byte(state)) != 1 || !ValidLoginBinding(flow.Nonce) || !ValidLoginBinding(flow.Verifier) || flow.Configuration != l.configuration || flow.CreatedAt <= 0 || flow.CreatedAt > now || now-flow.CreatedAt >= int64(LoginFlowTTL/time.Second) {
		return "", ErrUnauthenticated
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, l.client)
	token, err := l.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return "", ErrLoginUnavailable
	}
	if !strings.EqualFold(token.TokenType, "Bearer") {
		return "", ErrUnauthenticated
	}
	principal, err := l.auth.Verify(token.AccessToken)
	if err != nil {
		return "", ErrUnauthenticated
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || len(rawID) > 8192 {
		return "", ErrUnauthenticated
	}
	id, err := l.oidc.Verify(ctx, rawID)
	if err != nil {
		return "", ErrUnauthenticated
	}
	var claims struct {
		Exp, Iat, Nbf int64
		Azp           string
	}
	if id.Claims(&claims) != nil || !l.auth.validTimes(claims.Exp, claims.Iat, claims.Nbf) || len(id.Audience) > 16 || (len(id.Audience) > 1 && claims.Azp != l.provider.ClientID) || (claims.Azp != "" && claims.Azp != l.provider.ClientID) || id.Issuer != principal.Issuer() || id.Subject != principal.Subject() || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(flow.Nonce)) != 1 {
		return "", ErrUnauthenticated
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return "", ErrUnauthenticated
	}
	return token.AccessToken, nil
}
