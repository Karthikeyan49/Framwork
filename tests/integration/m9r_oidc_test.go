//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/cache"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/signin"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/redis/go-redis/v9"
)

func oidcSigned(t *testing.T, f *iamFixture, claims map[string]any) string {
	t.Helper()
	b64 := base64.RawURLEncoding.EncodeToString
	head, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test"})
	payload, _ := json.Marshal(claims)
	input := b64(head) + "." + b64(payload)
	hash := sha256.Sum256([]byte(input))
	sig, e := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, hash[:])
	if e != nil {
		t.Fatal("test token signing failed")
	}
	return input + "." + b64(sig)
}
func oidcCall(h http.Handler, path, body, origin string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestM9ROIDCBrowserBindingLiveDatabaseIdentityAndNoDisclosure(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	ctx := context.Background()
	flows, e := cache.Open(os.Getenv("SIGNIN_REDIS_URL"))
	if e != nil {
		t.Fatal("flow store unavailable")
	}
	defer flows.Close()
	const origin = "https://web.example"
	var exchanges atomic.Int32
	type grant struct{ nonce, challenge string }
	var grants sync.Map
	accessToken := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}, "tenant_id": f.tenantB})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		exchanges.Add(1)
		if r.ParseForm() != nil {
			t.Error("provider form")
			w.WriteHeader(400)
			return
		}
		value, ok := grants.LoadAndDelete(r.PostForm.Get("code"))
		if !ok {
			t.Error("unknown fixture grant")
			w.WriteHeader(400)
			return
		}
		g := value.(grant)
		digest := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(digest[:]) != g.challenge || r.PostForm.Get("redirect_uri") != origin+"/auth/callback" || r.PostForm.Get("client_id") != "integration-web" {
			t.Error("unbound exchange")
			w.WriteHeader(400)
			return
		}
		now := time.Now().Unix()
		hash := sha256.Sum256([]byte(accessToken))
		id := oidcSigned(t, f, map[string]any{"iss": "https://test-issuer.example", "aud": "integration-web", "sub": f.viewerSubject, "nonce": g.nonce, "iat": now, "exp": now + 600, "at_hash": base64.RawURLEncoding.EncodeToString(hash[:16])})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"token_type": "Bearer", "access_token": accessToken, "id_token": id, "expires_in": 600, "refresh_token": "PRIVATE ignored refresh"})
	}))
	defer provider.Close()
	login, e := identity.NewBrowserLogin(identity.Provider{ClientID: "integration-web", AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", RedirectURI: origin + "/auth/callback", Scopes: []string{"openid"}, Development: true}, f.verifier, flows)
	if e != nil {
		t.Fatal("provider fixture")
	}
	defer login.Close()
	api := signin.API{Login: login, Development: true}
	protected := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}}
	var logs bytes.Buffer
	handler := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Origins: []string{origin}, AllowPOST: api.AcceptsPOST, Register: func(m *http.ServeMux) { api.Register(m); protected.Register(m) }})
	for _, badOrigin := range []string{"", "https://other.example"} {
		if w := oidcCall(handler, "/v1/auth/login/start", "{}", badOrigin); w.Code != 403 {
			t.Fatal("missing/foreign origin accepted", w.Code)
		}
	}
	start := oidcCall(handler, "/v1/auth/login/start", "{}", origin)
	if start.Code != 200 || len(start.Result().Cookies()) != 1 {
		t.Fatal("flow start", start.Code)
	}
	cookie := start.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.Domain != "" || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || !strings.HasPrefix(cookie.Name, "__Host-") {
		t.Fatal("browser binding attributes")
	}
	var redirect struct {
		URL string `json:"authorization_url"`
	}
	if json.Unmarshal(start.Body.Bytes(), &redirect) != nil {
		t.Fatal("start shape")
	}
	u, e := url.Parse(redirect.URL)
	if e != nil {
		t.Fatal("start location")
	}
	state := u.Query().Get("state")
	code := "private-code-" + uuid(t)
	grants.Store(code, grant{u.Query().Get("nonce"), u.Query().Get("code_challenge")})
	encoded, _ := json.Marshal(map[string]string{"state": state, "code": code, "issuer": "https://test-issuer.example"})
	if w := oidcCall(handler, "/v1/auth/login/complete", string(encoded), origin); w.Code != 401 {
		t.Fatal("missing browser binding", w.Code)
	}
	if w := oidcCall(handler, "/v1/auth/login/complete", string(encoded), origin, cookie, cookie); w.Code != 401 {
		t.Fatal("duplicate browser binding", w.Code)
	}
	wrong, _ := json.Marshal(map[string]string{"state": strings.Repeat("a", 43), "code": code})
	if w := oidcCall(handler, "/v1/auth/login/complete", string(wrong), origin, cookie); w.Code != 401 {
		t.Fatal("state mismatch", w.Code)
	}
	if exchanges.Load() != 0 {
		t.Fatal("unbound request reached provider")
	}
	// Flow cookies alone cannot authenticate the business API.
	r := httptest.NewRequest("GET", "/v1/session", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("login cookie authorized API", w.Code)
	}
	completed := oidcCall(handler, "/v1/auth/login/complete", string(encoded), origin, cookie)
	if completed.Code != 200 || completed.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("verified completion", completed.Code)
	}
	var reply struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(completed.Body.Bytes(), &reply) != nil || reply.AccessToken != accessToken || strings.Contains(completed.Body.String(), "PRIVATE") {
		t.Fatal("completion returned extra credentials")
	}
	if replay := oidcCall(handler, "/v1/auth/login/complete", string(encoded), origin, cookie); replay.Code != 401 || exchanges.Load() != 1 {
		t.Fatal("replayed code")
	}
	session := call(handler, "/v1/session", reply.AccessToken, nil, "")
	if session.Code != 200 || !strings.Contains(session.Body.String(), f.viewer) {
		t.Fatal("live identity resolution", session.Code)
	}
	member := call(handler, "/v1/tenants/"+f.tenantA+"/access", reply.AccessToken, nil, "")
	if member.Code != 200 || !strings.Contains(member.Body.String(), `"viewer"`) || strings.Contains(member.Body.String(), `"owner"`) {
		t.Fatal("token role granted authority")
	}
	if other := call(handler, "/v1/tenants/"+f.tenantB+"/access", reply.AccessToken, nil, ""); other.Code != 403 {
		t.Fatal("token tenant granted authority", other.Code)
	}
	tx, e := f.operator.Begin()
	if e != nil {
		t.Fatal("operator fixture")
	}
	if _, e = (identity.Store{}).Provision(ctx, tx, "https://test-issuer.example", f.viewerSubject, "Viewer", false); e != nil {
		tx.Rollback()
		t.Fatal("disable fixture")
	}
	if e = tx.Commit(); e != nil {
		t.Fatal("disable commit")
	}
	if disabled := call(handler, "/v1/session", reply.AccessToken, nil, ""); disabled.Code != 401 {
		t.Fatal("disabled identity remained active", disabled.Code)
	}
	for _, secret := range []string{state, code, cookie.Value, accessToken, "PRIVATE"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatal("credential reached access logs")
		}
	}
}

func TestM9ROIDCRedisAdmissionTTLAndAtomicTake(t *testing.T) {
	options, e := redis.ParseURL(os.Getenv("SIGNIN_REDIS_URL"))
	if e != nil {
		t.Fatal("Redis fixture configuration")
	}
	u, _ := url.Parse(os.Getenv("SIGNIN_REDIS_URL"))
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		t.Fatal("Redis probe requires local fixture")
	}
	operatorOptions, parseErr := redis.ParseURL(os.Getenv("EVENT_OPERATOR_REDIS_URL"))
	if parseErr != nil || operatorOptions.Addr != options.Addr || operatorOptions.DB != options.DB {
		t.Fatal("Redis observer target mismatch")
	}
	observer := redis.NewClient(operatorOptions)
	defer observer.Close()
	ctx := context.Background()
	if role, err := observer.Do(ctx, "ACL", "WHOAMI").Text(); err != nil || role != "event_operator" {
		t.Fatal("Redis observer binding mismatch")
	}
	if observer.Ping(ctx).Err() != nil {
		t.Fatal("Redis fixture unavailable")
	}
	flows, e := cache.Open(os.Getenv("SIGNIN_REDIS_URL"))
	if e != nil {
		t.Fatal("flow adapter")
	}
	defer flows.Close()
	before := observer.Get(ctx, "framework:signin:v1:global").Val()
	if flows.ReadyLoginFlows(ctx) != nil {
		t.Fatal("sign-in ACL readiness")
	}
	if after := observer.Get(ctx, "framework:signin:v1:global").Val(); after != before {
		t.Fatal("readiness consumed login admission")
	}
	hash := sha256.Sum256([]byte(uuid(t)))
	remote := hex.EncodeToString(hash[:])
	keys := []string{}
	defer func() {
		for _, key := range keys {
			_ = observer.Del(ctx, "framework:signin:v1:flow:"+key).Err()
		}
		_ = observer.Del(ctx, "framework:signin:v1:remote:"+remote).Err()
	}()
	for i := range 21 {
		digest := sha256.Sum256([]byte(uuid(t)))
		key := hex.EncodeToString(digest[:])
		keys = append(keys, key)
		err := flows.SaveLoginFlow(ctx, key, remote, identity.LoginFlow{State: "bounded fixture", CreatedAt: time.Now().Unix()})
		if i == 20 {
			if !errors.Is(err, identity.ErrLoginBusy) {
				t.Fatal("per-address quota not enforced")
			}
			if observer.Exists(ctx, "framework:signin:v1:flow:"+key).Val() != 0 {
				t.Fatal("rejected flow stored")
			}
			break
		}
		if err != nil {
			t.Fatal("admission fixture")
		}
		ttl := observer.TTL(ctx, "framework:signin:v1:flow:"+key).Val()
		if ttl <= 0 || ttl > identity.LoginFlowTTL {
			t.Fatal("flow lifetime unbounded")
		}
	}
	key := keys[0]
	for _, bindingName := range []string{"REDIS_URL", "EVENT_RECEIVER_REDIS_URL", "EVENT_SENDER_REDIS_URL"} {
		otherOptions, err := redis.ParseURL(os.Getenv(bindingName))
		if err != nil || otherOptions.Addr != options.Addr || otherOptions.DB != options.DB {
			t.Fatal("Redis negative target mismatch")
		}
		other := redis.NewClient(otherOptions)
		if err = other.Get(ctx, "framework:signin:v1:flow:"+key).Err(); !redis.HasErrorPrefix(err, "NOPERM") {
			other.Close()
			t.Fatal("non-sign-in credential reached login state")
		}
		other.Close()
	}
	var wg sync.WaitGroup
	var claimed atomic.Int32
	for range 2 {
		wg.Go(func() {
			if _, e := flows.TakeLoginFlow(ctx, key); e == nil {
				claimed.Add(1)
			}
		})
	}
	wg.Wait()
	if claimed.Load() != 1 {
		t.Fatal("GETDEL not single-use")
	}
	for _, suffix := range []string{"remote:" + remote, "global"} {
		ttl := observer.TTL(ctx, "framework:signin:v1:"+suffix).Val()
		if ttl <= 0 || ttl > time.Minute {
			t.Fatal("admission counter lacks TTL")
		}
	}
	// A separately closed adapter is an actual unavailable-client path, not an
	// in-memory substitute for Redis. It cannot create or return a flow.
	offline, e := cache.Open(os.Getenv("SIGNIN_REDIS_URL"))
	if e != nil {
		t.Fatal("offline fixture")
	}
	offline.Close()
	if e = offline.SaveLoginFlow(ctx, keys[1], remote, identity.LoginFlow{}); !errors.Is(e, identity.ErrLoginUnavailable) {
		t.Fatal("unavailable Redis failed open")
	}
	if _, e = offline.TakeLoginFlow(ctx, keys[1]); !errors.Is(e, identity.ErrLoginUnavailable) {
		t.Fatal("unavailable take failed open")
	}
}
