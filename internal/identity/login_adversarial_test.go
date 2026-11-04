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
	"sync/atomic"
	"testing"
	"time"
)

// Independent review probes use only synthetic tokens. Failure messages never
// print a token, provider reply, redirect URL or browser binding.
func TestOIDCIndependentSignedTokenBoundaries(t *testing.T) {
	key, keys := loginTestKey(t)
	now := time.Now().Unix()
	for _, mode := range []string{"valid", "expiry_within_skew", "expiry_at_boundary", "iat_outside_skew", "nbf_outside_skew", "lifetime_3601", "fractional_exp", "missing_exp", "api_typ", "id_typ", "unknown_kid", "critical_header", "wrong_algorithm", "too_many_audiences", "empty_subject", "bad_token_type", "bad_content_type", "malformed_json"} {
		t.Run(mode, func(t *testing.T) {
			auth, err := NewVerifierWithClockSkew("https://independent.example", "api", keys, 30*time.Second)
			if err != nil {
				t.Fatal("verifier fixture")
			}
			auth.now = func() time.Time { return time.Unix(now, 0) }
			var nonce string
			var requests atomic.Int32
			sign := func(header map[string]any, claims map[string]any) string {
				h, _ := json.Marshal(header)
				c, _ := json.Marshal(claims)
				input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(c)
				digest := sha256.Sum256([]byte(input))
				signature, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
				if e != nil {
					t.Fatal("signing fixture")
				}
				return input + "." + base64.RawURLEncoding.EncodeToString(signature)
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if mode == "bad_content_type" {
					w.Header().Set("Content-Type", "text/plain")
				}
				if mode == "malformed_json" {
					_, _ = w.Write([]byte(`{"PRIVATE":`))
					return
				}
				accessClaims := map[string]any{"iss": "https://independent.example", "sub": "synthetic-actor", "aud": "api", "iat": now - 300, "exp": now + 600}
				accessTyp := "at+jwt"
				if mode == "api_typ" {
					accessTyp = "JWT"
				}
				access := loginSign(t, key, accessTyp, accessClaims)
				claims := map[string]any{"iss": "https://independent.example", "sub": "synthetic-actor", "aud": "web-client", "iat": now - 300, "exp": now + 600, "nonce": nonce}
				header := map[string]any{"alg": "RS256", "kid": "login-test", "typ": "JWT"}
				switch mode {
				case "expiry_within_skew":
					claims["exp"] = now - 29
				case "expiry_at_boundary":
					claims["exp"] = now - 30
				case "iat_outside_skew":
					claims["iat"] = now + 31
				case "nbf_outside_skew":
					claims["nbf"] = now + 31
				case "lifetime_3601":
					claims["exp"] = now + 3301
				case "fractional_exp":
					claims["exp"] = float64(now) + 600.5
				case "missing_exp":
					delete(claims, "exp")
				case "id_typ":
					header["typ"] = "at+jwt"
				case "unknown_kid":
					header["kid"] = "unconfigured"
				case "critical_header":
					header["crit"] = []string{"unexpected"}
				case "wrong_algorithm":
					header["alg"] = "HS256"
				case "too_many_audiences":
					audiences := []string{"web-client"}
					for range 16 {
						audiences = append(audiences, "other")
					}
					claims["aud"] = audiences
					claims["azp"] = "web-client"
				case "empty_subject":
					claims["sub"] = ""
				}
				tokenType := "Bearer"
				if mode == "bad_token_type" {
					tokenType = "MAC"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "id_token": sign(header, claims), "token_type": tokenType, "expires_in": 600})
			}))
			defer provider.Close()
			store := &loginMemory{values: map[string]LoginFlow{}}
			login, err := NewBrowserLogin(Provider{ClientID: "web-client", AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", RedirectURI: "https://web.example/auth/callback", Scopes: []string{"openid"}, Development: true}, auth, store)
			if err != nil {
				t.Fatal("login fixture")
			}
			defer login.Close()
			location, binding, err := login.Start(context.Background(), "127.0.0.1")
			if err != nil {
				t.Fatal("flow fixture")
			}
			redirect, _ := url.Parse(location)
			nonce = redirect.Query().Get("nonce")
			state := redirect.Query().Get("state")
			token, err := login.Complete(context.Background(), binding, state, "synthetic-code", "")
			valid := mode == "valid" || mode == "expiry_within_skew"
			if (err == nil) != valid || (err != nil && token != "") {
				t.Fatal("unexpected signed-pair acceptance")
			}
			if requests.Load() != 1 {
				t.Fatal("token exchange repeated")
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("provider reply disclosed")
			}
			if _, err = login.Complete(context.Background(), binding, state, "synthetic-code", ""); !errors.Is(err, ErrUnauthenticated) || requests.Load() != 1 {
				t.Fatal("spent flow retried")
			}
		})
	}
}

func TestOIDCIndependentConfigurationAndFlowAge(t *testing.T) {
	_, keys := loginTestKey(t)
	auth, err := NewVerifier("https://independent.example", "api", keys)
	if err != nil {
		t.Fatal("verifier fixture")
	}
	now := time.Now().Unix()
	auth.now = func() time.Time { return time.Unix(now, 0) }
	var requests atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer provider.Close()
	for _, mode := range []string{"configuration", "expired", "future", "wrong_issuer"} {
		t.Run(mode, func(t *testing.T) {
			store := &loginMemory{values: map[string]LoginFlow{}}
			p := Provider{ClientID: "web-client", AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", RedirectURI: "https://web.example/auth/callback", Scopes: []string{"openid"}, Development: true}
			login, e := NewBrowserLogin(p, auth, store)
			if e != nil {
				t.Fatal("login fixture")
			}
			defer login.Close()
			location, binding, e := login.Start(context.Background(), "127.0.0.1")
			if e != nil {
				t.Fatal("flow fixture")
			}
			redirect, _ := url.Parse(location)
			state := redirect.Query().Get("state")
			if mode == "configuration" {
				p.Scopes = []string{"openid", "profile"}
				login, e = NewBrowserLogin(p, auth, store)
				if e != nil {
					t.Fatal("replacement fixture")
				}
				defer login.Close()
			}
			if mode == "expired" || mode == "future" {
				store.mu.Lock()
				f := store.values[flowKey(binding, state)]
				if mode == "expired" {
					f.CreatedAt = now - 300
				} else {
					f.CreatedAt = now + 1
				}
				store.values[flowKey(binding, state)] = f
				store.mu.Unlock()
			}
			issuer := ""
			if mode == "wrong_issuer" {
				issuer = "https://other.example"
			}
			if token, e := login.Complete(context.Background(), binding, state, "synthetic-code", issuer); e == nil || token != "" {
				t.Fatal("invalid flow accepted")
			}
			if requests.Load() != 0 {
				t.Fatal("invalid flow reached provider")
			}
		})
	}
}
