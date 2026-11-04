//go:build integration

package integration

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/platform/traffic"
	"github.com/Karthikeyan49/Framwork/internal/policy"
)

type r7Auth struct {
	base  identity.Authenticator
	calls atomic.Int64
	first chan struct{}
}

func (a *r7Auth) Verify(raw string) (identity.Principal, error) {
	n := a.calls.Add(1)
	p, e := a.base.Verify(raw)
	if n == 1 && a.first != nil {
		close(a.first)
	}
	return p, e
}

type r7Audit struct{ calls atomic.Int64 }

func (a *r7Audit) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	a.calls.Add(1)
	return (audit.Postgres{}).Append(ctx, q, e)
}
func r7Gate(t *testing.T, l traffic.Limits) *traffic.Gate {
	t.Helper()
	g, e := traffic.New(l, func() time.Time { return time.Unix(1000, 0) })
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func r7Handler(t *testing.T, f *iamFixture, g *traffic.Gate, a identity.Authenticator, w audit.Writer, dbGate *postgres.Admission) http.Handler {
	t.Helper()
	api := access.API{DB: f.db, Traffic: g, Admission: dbGate, Auth: a, Audit: w, Policy: policy.Native{}, PolicyLoader: (policy.RoleStore{}).Load, Registry: m3Registry(t), PreviewEnabled: true}
	return httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
}
func r7Call(h http.Handler, path, token, remote, forwarded string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.RemoteAddr = remote
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("X-Forwarded-For", forwarded)
	r.Header.Set("Forwarded", "for="+forwarded)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestM9RTrafficInvalidAuthenticationCannotFloodPostgresAudit(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	auth := &r7Auth{base: f.verifier}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.DefaultLimits()), auth, writer, nil)
	var ids []string
	for i := range 40 {
		w := r7Call(h, "/v1/session", "invalid-"+strings.Repeat("x", i), "192.0.2.1:1234", "198.51.100."+string(rune('a'+i)))
		ids = append(ids, w.Header().Get("X-Request-ID"))
		want := 401
		if i >= 20 {
			want = 429
		}
		if w.Code != want {
			t.Fatal("invalid request quota", i, w.Code)
		}
		if want == 429 && (w.Header().Get("Retry-After") != "1" || w.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("safe backoff response")
		}
	}
	if writer.calls.Load() != 20 {
		t.Fatal("excess authentication audits", writer.calls.Load())
	}
	var n int
	if e := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=ANY($1::text[]) AND action='authentication.denied'`, ids).Scan(&n); e != nil || n != 20 {
		t.Fatal("retained denial audit evidence", n)
	}
	// A closed pool is a populated negative control: an admitted audit would fail503.
	if e := f.db.Close(); e != nil {
		t.Fatal("close fixture pool")
	}
	if w := r7Call(h, "/v1/session", "invalid-new-token", "192.0.2.1:4444", "203.0.113.99"); w.Code != 429 {
		t.Fatal("exhausted gate touched closed database", w.Code)
	}
	if w := r7Call(h, "/v1/session", "invalid-new-token", "192.0.2.2:4444", "203.0.113.99"); w.Code != 503 {
		t.Fatal("new peer did not reach closed-pool negative control", w.Code)
	}
}
func TestM9RTrafficPeerAndGlobalRejectBeforeSignatureOrBody(t *testing.T) {
	m8Database(t)
	f, base, _, owner, app := m5Fixture(t)
	auth := &r7Auth{base: f.verifier}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.Limits{PeerRate: 1, PeerBurst: 2, GlobalRate: 1, GlobalBurst: 2}), auth, writer, nil)
	root := "/v1/tenants/" + f.tenantA
	for range 2 {
		if w := r7Call(h, root+"/access", owner, "192.0.2.1:1", ""); w.Code != 200 {
			t.Fatal("initial admitted access", w.Code)
		}
	}
	before := auth.calls.Load()
	if before != 4 {
		t.Fatal("valid tokens not checked before and inside transaction", before)
	}
	r := httptest.NewRequest("POST", root+"/metadata/"+app+"/versions", r7UnreadBody{})
	r.RemoteAddr = "192.0.2.1:2"
	r.ContentLength = 1
	r.Header.Set("Authorization", "Bearer "+owner)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || auth.calls.Load() != before || writer.calls.Load() != 2 {
		t.Fatal("throttled mutation entered signature/body/SQL path", w.Code)
	}
	if w := r7Call(h, root+"/access", owner, "192.0.2.2:2", "192.0.2.9"); w.Code != 429 || auth.calls.Load() != before {
		t.Fatal("global budget bypassed with new peer", w.Code)
	}
	if w := m3Call(base, "GET", root+"/metadata/"+app+"/versions/2", owner, 0, nil); w.Code != 404 {
		t.Fatal("rejected mutation created a revision", w.Code)
	}
}

type r7UnreadBody struct{}

func (r7UnreadBody) Read([]byte) (int, error) { panic("rate-rejected body must not be read") }
func TestM9RTrafficSignedSubjectCannotRotatePeersOrClaims(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	auth := &r7Auth{base: f.verifier}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.Limits{PeerRate: 1, PeerBurst: 2, GlobalRate: 100, GlobalBurst: 200}), auth, writer, nil)
	root := "/v1/tenants/" + f.tenantA
	owner := f.token(t, f.aliceSubject, nil)
	for _, remote := range []string{"192.0.2.1:1", "192.0.2.2:2"} {
		if w := r7Call(h, root+"/access", owner, remote, ""); w.Code != 200 {
			t.Fatal("initial signed-subject access", w.Code)
		}
	}
	before := auth.calls.Load()
	if e := f.db.Close(); e != nil {
		t.Fatal(e)
	}
	forged := f.token(t, f.aliceSubject, map[string]any{"tenant_id": f.tenantB, "roles": []string{"owner"}, "jti": "rotated"})
	w := r7Call(h, "/v1/tenants/"+f.tenantB+"/access", forged, "192.0.2.3:3", "203.0.113.1")
	if w.Code != 429 || auth.calls.Load() != before+1 || writer.calls.Load() != 2 {
		t.Fatal("subject rotation reached database", w.Code)
	}
}
func TestM9RTrafficRechecksActualTokenExpiryAfterDatabaseAdmissionWait(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	gate, e := postgres.NewAdmission(1, 2)
	if e != nil {
		t.Fatal(e)
	}
	release, e := gate.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer release()
	auth := &r7Auth{base: f.verifier, first: make(chan struct{})}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.DefaultLimits()), auth, writer, gate)
	expires := time.Now().Unix() + 2
	token := f.token(t, f.aliceSubject, map[string]any{"exp": expires})
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- r7Call(h, "/v1/tenants/"+f.tenantA+"/access", token, "192.0.2.1:1", "") }()
	select {
	case <-auth.first:
	case <-time.After(3 * time.Second):
		t.Fatal("pre-verification did not execute")
	}
	// Token remains signed-valid when queued, then actually expires before release.
	time.Sleep(time.Until(time.Unix(expires, 0).Add(50 * time.Millisecond)))
	release()
	select {
	case w := <-done:
		if w.Code != 401 || auth.calls.Load() != 2 || writer.calls.Load() != 1 {
			t.Fatal("queued expired token authorized", w.Code)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("queued request did not complete")
	}
}
func TestM9RTrafficPreservesLiveRevocationAndAuditFailure(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	auth := &r7Auth{base: f.verifier}
	writer := &r7Audit{}
	h := r7Handler(t, f, r7Gate(t, traffic.DefaultLimits()), auth, writer, nil)
	root := "/v1/tenants/" + f.tenantA
	owner := f.token(t, f.aliceSubject, nil)
	if w := r7Call(h, root+"/access", owner, "192.0.2.1:1", ""); w.Code != 200 {
		t.Fatal("initial access")
	}
	// Existing fixture helper verifies actual connected target before operator DML.
	scalarReviewTarget(t, f)
	scalarSQL(t, (func() error {
		_, e := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice)
		return e
	})())
	if w := r7Call(h, root+"/access", owner, "192.0.2.1:1", ""); w.Code != 403 {
		t.Fatal("admission cached revoked membership", w.Code)
	}
	failed := r7Handler(t, f, r7Gate(t, traffic.DefaultLimits()), f.verifier, failedAudit{}, nil)
	if w := r7Call(failed, "/v1/session", "invalid", "192.0.2.9:1", ""); w.Code != 503 {
		t.Fatal("admitted denial ignored audit failure", w.Code)
	}
}
