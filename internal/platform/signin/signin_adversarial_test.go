package signin

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/identity"
)

type deniedFlowStore struct{ writes, takes atomic.Int32 }

func (s *deniedFlowStore) SaveLoginFlow(context.Context, string, string, identity.LoginFlow) error {
	s.writes.Add(1)
	return nil
}
func (s *deniedFlowStore) TakeLoginFlow(context.Context, string) (identity.LoginFlow, error) {
	s.takes.Add(1)
	return identity.LoginFlow{}, identity.ErrUnauthenticated
}

func TestIndependentGatewayRejectsUnboundAndMalformedRequests(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal("key fixture")
	}
	keys, _ := json.Marshal(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "kid": "independent", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	auth, e := identity.NewVerifier("https://id.example", "api", keys)
	if e != nil {
		t.Fatal("verifier fixture")
	}
	flows := &deniedFlowStore{}
	login, e := identity.NewBrowserLogin(identity.Provider{ClientID: "web", AuthorizationURL: "https://id.example/authorize", TokenURL: "https://id.example/token", RedirectURI: "https://web.example/auth/callback", Scopes: []string{"openid"}}, auth, flows)
	if e != nil {
		t.Fatal("login fixture")
	}
	defer login.Close()
	mux := http.NewServeMux()
	API{Login: login}.Register(mux)
	for _, tc := range []struct {
		name, path, body, origin, content string
		duplicateOrigin                   bool
	}{
		{"missing origin", "/v1/auth/login/start", "{}", "", "application/json", false},
		{"foreign origin", "/v1/auth/login/start", "{}", "https://foreign.example", "application/json", false},
		{"multiple origin values", "/v1/auth/login/start", "{}", "https://web.example", "application/json", true},
		{"combined origins", "/v1/auth/login/start", "{}", "https://web.example, https://foreign.example", "application/json", false},
		{"query parameter", "/v1/auth/login/start?redirect_uri=foreign", "{}", "https://web.example", "application/json", false},
		{"wrong media type", "/v1/auth/login/start", "{}", "https://web.example", "text/plain", false},
		{"unknown field", "/v1/auth/login/start", `{"redirect_uri":"foreign"}`, "https://web.example", "application/json", false},
		{"trailing object", "/v1/auth/login/start", "{}{}", "https://web.example", "application/json", false},
		{"array", "/v1/auth/login/start", "[]", "https://web.example", "application/json", false},
		{"oversized request", "/v1/auth/login/start", `{"private":"` + strings.Repeat("X", 9000) + `"}`, "https://web.example", "application/json", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
			r.RemoteAddr = "127.0.0.1:12345"
			r.Header.Set("Content-Type", tc.content)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.duplicateOrigin {
				r.Header.Add("Origin", "https://web.example")
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code < 400 || flows.writes.Load() != 0 || flows.takes.Load() != 0 {
				t.Fatal("unbound request reached flow store")
			}
		})
	}
	for _, cookies := range [][]*http.Cookie{nil, {{Name: "__Host-framework_login", Value: "invalid"}}, {{Name: "__Host-framework_login", Value: strings.Repeat("A", 43)}, {Name: "__Host-framework_login", Value: strings.Repeat("A", 43)}}} {
		r := httptest.NewRequest("POST", "/v1/auth/login/complete", strings.NewReader(`{"code":"synthetic","state":"`+strings.Repeat("A", 43)+`"}`))
		r.Header.Set("Origin", "https://web.example")
		r.Header.Set("Content-Type", "application/json")
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 401 || flows.takes.Load() != 0 {
			t.Fatal("missing or ambiguous cookie reached flow store")
		}
	}
}
