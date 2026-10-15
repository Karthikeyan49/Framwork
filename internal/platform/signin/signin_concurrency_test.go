package signin

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/identity"
)

type concurrentReviewFlows struct {
	mu     sync.Mutex
	values map[string]identity.LoginFlow
}

func (s *concurrentReviewFlows) SaveLoginFlow(_ context.Context, key, _ string, f identity.LoginFlow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = f
	return nil
}
func (s *concurrentReviewFlows) TakeLoginFlow(_ context.Context, key string) (identity.LoginFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.values[key]
	delete(s.values, key)
	if !ok {
		return f, identity.ErrUnauthenticated
	}
	return f, nil
}

func TestIndependentOlderCompletionPreservesNewerBrowserBinding(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal("key fixture")
	}
	keys, _ := json.Marshal(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "independent", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	auth, e := identity.NewVerifier("https://id.example", "api", keys)
	if e != nil {
		t.Fatal("verifier fixture")
	}
	sign := func(typ string, claims map[string]any) string {
		h, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "independent", "typ": typ})
		p, _ := json.Marshal(claims)
		input := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p)
		digest := sha256.Sum256([]byte(input))
		sig, e := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if e != nil {
			t.Fatal("signature fixture")
		}
		return input + "." + base64.RawURLEncoding.EncodeToString(sig)
	}
	var nonce string
	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseProvider := func() { releaseOnce.Do(func() { close(release) }) }
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		now := time.Now().Unix()
		access := sign("at+jwt", map[string]any{"iss": "https://id.example", "aud": "api", "sub": "synthetic", "iat": now, "exp": now + 600})
		id := sign("JWT", map[string]any{"iss": "https://id.example", "aud": "web", "sub": "synthetic", "iat": now, "exp": now + 600, "nonce": nonce})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "id_token": id, "token_type": "Bearer"})
	}))
	mux := http.NewServeMux()
	gateway := httptest.NewTLSServer(mux)
	defer func() { releaseProvider(); gateway.Close(); provider.Close() }()
	login, e := identity.NewBrowserLogin(identity.Provider{ClientID: "web", AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", RedirectURI: gateway.URL + "/auth/callback", Scopes: []string{"openid"}, Development: true}, auth, &concurrentReviewFlows{values: map[string]identity.LoginFlow{}})
	if e != nil {
		t.Fatal("login fixture")
	}
	defer login.Close()
	API{Login: login}.Register(mux)
	client := gateway.Client()
	client.Jar, _ = cookiejar.New(nil)
	client.Timeout = 10 * time.Second
	post := func(path, body string) (*http.Response, error) {
		request, e := http.NewRequest("POST", gateway.URL+path, strings.NewReader(body))
		if e != nil {
			return nil, e
		}
		request.Header.Set("Origin", gateway.URL)
		request.Header.Set("Content-Type", "application/json")
		return client.Do(request)
	}
	start := func() string {
		response, e := post("/v1/auth/login/start", "{}")
		if e != nil {
			t.Fatal("start transport")
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("start rejected")
		}
		var v struct {
			URL string `json:"authorization_url"`
		}
		if json.NewDecoder(response.Body).Decode(&v) != nil {
			t.Fatal("start reply")
		}
		return v.URL
	}
	first, _ := url.Parse(start())
	nonce = first.Query().Get("nonce")
	body, _ := json.Marshal(map[string]string{"code": "synthetic-code", "state": first.Query().Get("state")})
	completed := make(chan int, 1)
	go func() {
		response, e := post("/v1/auth/login/complete", string(body))
		if e != nil {
			completed <- 0
			return
		}
		defer response.Body.Close()
		completed <- response.StatusCode
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("first exchange did not start")
	}
	_ = start()
	origin, _ := url.Parse(gateway.URL)
	newer := client.Jar.Cookies(origin)
	if len(newer) != 1 {
		t.Fatal("new browser binding missing before first completion")
	}
	expected := newer[0].Value
	releaseProvider()
	select {
	case status := <-completed:
		if status != 200 {
			t.Fatal("first completion was not valid")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("first completion did not finish")
	}
	after := client.Jar.Cookies(origin)
	if len(after) != 1 || after[0].Value != expected {
		t.Fatal("older successful completion erased the newer flow's browser binding")
	}
}
