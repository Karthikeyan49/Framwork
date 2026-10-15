package identity

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type loginMemory struct {
	mu     sync.Mutex
	values map[string]LoginFlow
}

func (m *loginMemory) SaveLoginFlow(_ context.Context, key, _ string, f LoginFlow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[key] = f
	return nil
}
func (m *loginMemory) TakeLoginFlow(_ context.Context, key string) (LoginFlow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.values[key]
	delete(m.values, key)
	if !ok {
		return f, ErrUnauthenticated
	}
	return f, nil
}
func loginTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "login-test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	return key, raw
}
func loginSign(t *testing.T, key *rsa.PrivateKey, typ string, c map[string]any) string {
	t.Helper()
	b64 := base64.RawURLEncoding.EncodeToString
	h, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": typ, "kid": "login-test"})
	p, _ := json.Marshal(c)
	input := b64(h) + "." + b64(p)
	hash := sha256.Sum256([]byte(input))
	signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if e != nil {
		t.Fatal(e)
	}
	return input + "." + b64(signature)
}

func TestExplicitClockToleranceBoundaries(t *testing.T) {
	key, keys := loginTestKey(t)
	now := time.Now().Unix()
	for _, skew := range []time.Duration{0, 30 * time.Second, 2 * time.Minute} {
		v, e := NewVerifierWithClockSkew("https://id.example", "api", keys, skew)
		if e != nil {
			t.Fatal(e)
		}
		v.now = func() time.Time { return time.Unix(now, 0) }
		seconds := int64(skew / time.Second)
		for _, tc := range []struct {
			name   string
			claims map[string]any
			valid  bool
		}{
			{"inside expiry", map[string]any{"iat": now - 300, "exp": now - seconds + 1}, true},
			{"expiry boundary", map[string]any{"iat": now - 300, "exp": now - seconds}, false},
			{"issued boundary", map[string]any{"iat": now + seconds}, true},
			{"issued outside", map[string]any{"iat": now + seconds + 1}, false},
			{"not before boundary", map[string]any{"nbf": now + seconds}, true},
			{"not before outside", map[string]any{"nbf": now + seconds + 1}, false},
			{"lifetime unchanged", map[string]any{"iat": now - 300, "exp": now + 3301}, false},
		} {
			t.Run(skew.String()+"/"+tc.name, func(t *testing.T) {
				c := map[string]any{"iss": "https://id.example", "aud": "api", "sub": "actor", "iat": now, "exp": now + 900}
				for k, value := range tc.claims {
					c[k] = value
				}
				_, e := v.Verify(loginSign(t, key, "at+jwt", c))
				if (e == nil) != tc.valid {
					t.Fatal("unexpected time acceptance")
				}
			})
		}
	}
	for _, bad := range []time.Duration{-time.Second, 121 * time.Second, time.Millisecond} {
		if _, e := NewVerifierWithClockSkew("issuer", "api", keys, bad); e == nil {
			t.Fatal("invalid leeway")
		}
	}
}

func TestOIDCVerifiedPairAndSingleExchange(t *testing.T) {
	key, keys := loginTestKey(t)
	now := time.Now().Unix()
	for _, mode := range []string{"valid", "nonce", "issuer", "audience", "azp", "multiple_audience_no_azp", "multiple_audience_valid", "subject", "hash", "missing_id", "expired_id", "future_id", "missing_iat", "wrong_api_audience", "provider_error", "redirect", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			var requests atomic.Int32
			nonce, challenge := "", ""
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/token" || r.Method != "POST" {
					t.Error("unexpected provider request")
					w.WriteHeader(400)
					return
				}
				if r.ParseForm() != nil || r.PostForm.Get("client_id") != "web-client" || r.PostForm.Get("redirect_uri") != "https://web.example/auth/callback" || r.PostForm.Get("grant_type") != "authorization_code" {
					t.Error("exchange binding")
					w.WriteHeader(400)
					return
				}
				h := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
				if base64.RawURLEncoding.EncodeToString(h[:]) != challenge {
					t.Error("PKCE mismatch")
					w.WriteHeader(400)
					return
				}
				if mode == "provider_error" {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"PRIVATE provider response"}`))
					return
				}
				if mode == "redirect" {
					w.Header().Set("Location", "/stolen")
					w.WriteHeader(307)
					return
				}
				if mode == "oversized" {
					_, _ = w.Write([]byte(`{"id_token":"` + strings.Repeat("X", 40000) + `"}`))
					return
				}
				accessClaims := map[string]any{"iss": "https://id.example", "aud": "api", "sub": "alice", "iat": now, "exp": now + 600, "roles": []string{"owner"}, "tenant_id": "forged"}
				if mode == "wrong_api_audience" {
					accessClaims["aud"] = "other"
				}
				access := loginSign(t, key, "at+jwt", accessClaims)
				digest := sha256.Sum256([]byte(access))
				idClaims := map[string]any{"iss": "https://id.example", "aud": "web-client", "sub": "alice", "iat": now, "exp": now + 600, "nonce": nonce, "at_hash": base64.RawURLEncoding.EncodeToString(digest[:16])}
				switch mode {
				case "nonce":
					idClaims["nonce"] = "other"
				case "issuer":
					idClaims["iss"] = "https://other.example"
				case "audience":
					idClaims["aud"] = "other"
				case "azp":
					idClaims["azp"] = "other"
				case "multiple_audience_no_azp":
					idClaims["aud"] = []string{"web-client", "other"}
				case "multiple_audience_valid":
					idClaims["aud"] = []string{"web-client", "other"}
					idClaims["azp"] = "web-client"
				case "subject":
					idClaims["sub"] = "other"
				case "hash":
					idClaims["at_hash"] = "invalid"
				case "expired_id":
					idClaims["exp"] = now - 1
				case "future_id":
					idClaims["nbf"] = now + 121
				case "missing_iat":
					delete(idClaims, "iat")
				}
				reply := map[string]any{"token_type": "Bearer", "access_token": access, "id_token": loginSign(t, key, "JWT", idClaims), "refresh_token": "PRIVATE refresh value", "expires_in": 600}
				if mode == "missing_id" {
					delete(reply, "id_token")
				}
				_ = json.NewEncoder(w).Encode(reply)
			}))
			defer provider.Close()
			auth, e := NewVerifierWithClockSkew("https://id.example", "api", keys, 30*time.Second)
			if e != nil {
				t.Fatal(e)
			}
			auth.now = func() time.Time { return time.Unix(now, 0) }
			store := &loginMemory{values: map[string]LoginFlow{}}
			login, e := NewBrowserLogin(Provider{ClientID: "web-client", AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", RedirectURI: "https://web.example/auth/callback", Scopes: []string{"openid"}, Development: true}, auth, store)
			if e != nil {
				t.Fatal(e)
			}
			defer login.Close()
			location, binding, e := login.Start(context.Background(), "127.0.0.1")
			if e != nil {
				t.Fatal(e)
			}
			u, _ := url.Parse(location)
			state := u.Query().Get("state")
			nonce = u.Query().Get("nonce")
			challenge = u.Query().Get("code_challenge")
			if !ValidLoginBinding(binding) || !ValidLoginBinding(state) || u.Query().Get("response_type") != "code" || u.Query().Get("code_challenge_method") != "S256" || u.Query().Get("scope") != "openid" {
				t.Fatal("authorization binding")
			}
			token, e := login.Complete(context.Background(), binding, state, "one-use-code", "https://id.example")
			valid := mode == "valid" || mode == "multiple_audience_valid"
			if (e == nil) != valid {
				t.Fatalf("pair acceptance mode %s", mode)
			}
			if e != nil && strings.Contains(e.Error(), "PRIVATE") {
				t.Fatal("provider error disclosure")
			}
			if valid {
				p, e := auth.Verify(token)
				if e != nil || p.Subject() != "alice" {
					t.Fatal("not a verified API identity")
				}
			}
			if requests.Load() != 1 {
				t.Fatal("exchange retried or followed redirect", requests.Load())
			}
			if _, e = login.Complete(context.Background(), binding, state, "one-use-code", ""); !errors.Is(e, ErrUnauthenticated) || requests.Load() != 1 {
				t.Fatal("consumed flow reused")
			}
		})
	}
}

func TestOIDCFlowBindingAndConcurrentConsumption(t *testing.T) {
	key, keys := loginTestKey(t)
	_ = key
	auth, _ := NewVerifier("https://id.example", "api", keys)
	store := &loginMemory{values: map[string]LoginFlow{}}
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer provider.Close()
	login, e := NewBrowserLogin(Provider{ClientID: "web-client", AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", RedirectURI: "https://web.example/auth/callback", Scopes: []string{"openid"}, Development: true}, auth, store)
	if e != nil {
		t.Fatal(e)
	}
	defer login.Close()
	first, _, _ := login.Start(context.Background(), "127.0.0.1")
	older, _ := url.Parse(first)
	second, binding, _ := login.Start(context.Background(), "127.0.0.1")
	current, _ := url.Parse(second)
	state := current.Query().Get("state")
	if _, e = login.Complete(context.Background(), binding, older.Query().Get("state"), "code", ""); !errors.Is(e, ErrUnauthenticated) {
		t.Fatal("mixed browser/state accepted")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { _, e := login.Complete(context.Background(), binding, state, "code", ""); errs <- e })
	}
	wg.Wait()
	close(errs)
	unavailable, denied := 0, 0
	for e := range errs {
		if errors.Is(e, ErrLoginUnavailable) {
			unavailable++
		}
		if errors.Is(e, ErrUnauthenticated) {
			denied++
		}
	}
	if unavailable != 1 || denied != 1 || requests.Load() != 1 {
		t.Fatal("flow was lost or exchanged more than once")
	}
}
