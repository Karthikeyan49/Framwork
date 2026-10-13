//go:build integration

package integration

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

type iamFixture struct {
	db, operator                                                                  *sql.DB
	key                                                                           *rsa.PrivateKey
	verifier                                                                      *identity.Verifier
	tenantA, tenantB, alice, bob, viewer, aliceSubject, bobSubject, viewerSubject string
}

func uuid(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func newIAM(t *testing.T) *iamFixture {
	t.Helper()
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	db, err := postgres.Open(c.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	operator, err := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": "AQAB"}}})
	verifier, err := identity.NewVerifier("https://test-issuer.example", "test-api", jwks)
	if err != nil {
		t.Fatal(err)
	}
	f := &iamFixture{db: db, operator: operator, key: key, verifier: verifier, tenantA: uuid(t), tenantB: uuid(t), aliceSubject: uuid(t), bobSubject: uuid(t), viewerSubject: uuid(t)}
	ctx := context.Background()
	tx, err := operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal("operator transaction unavailable")
	}
	defer tx.Rollback()
	for _, user := range []struct {
		subject, name string
		id            *string
	}{{f.aliceSubject, "Alice", &f.alice}, {f.bobSubject, "Bob", &f.bob}, {f.viewerSubject, "Viewer", &f.viewer}} {
		*user.id, err = (identity.Store{}).Provision(ctx, tx, "https://test-issuer.example", user.subject, user.name, true)
		if err != nil {
			t.Fatal("user seed failed")
		}
	}
	for _, m := range []struct{ tenant, user, role string }{{f.tenantA, f.alice, "owner"}, {f.tenantB, f.bob, "owner"}, {f.tenantA, f.viewer, "viewer"}} {
		if err = (tenant.Store{}).Provision(ctx, tx, m.tenant, "Test workspace", m.user, m.role, true); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("M9R_DISPOSABLE_WORKER_BINDING") == "true" {
		if _, err = operator.Exec(`SELECT control.assign_worker('framework_worker',$1::uuid,true)`, f.tenantA); err != nil {
			t.Fatal("disposable worker assignment", err)
		}
	}
	t.Cleanup(func() {
		if os.Getenv("M9R_DISPOSABLE_WORKER_BINDING") == "true" {
			_, _ = operator.Exec(`DELETE FROM control.worker_principals WHERE tenant_id IN($1,$2)`, f.tenantA, f.tenantB)
		}
		_, _ = operator.Exec(`DELETE FROM iam.memberships WHERE tenant_id IN ($1,$2)`, f.tenantA, f.tenantB)
		_, _ = operator.Exec(`DELETE FROM iam.tenants WHERE id IN ($1,$2)`, f.tenantA, f.tenantB)
		_, _ = operator.Exec(`DELETE FROM iam.users WHERE id IN ($1,$2,$3)`, f.alice, f.bob, f.viewer)
		db.Close()
		operator.Close()
	})
	return f
}
func (f *iamFixture) token(t *testing.T, subject string, changes map[string]any) string {
	t.Helper()
	now := time.Now().Unix()
	claims := map[string]any{"iss": "https://test-issuer.example", "aud": "test-api", "sub": subject, "iat": now, "nbf": now, "exp": now + 900}
	for k, v := range changes {
		claims[k] = v
	}
	hb, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "at+jwt", "kid": "test"})
	cb, _ := json.Marshal(claims)
	b64 := base64.RawURLEncoding.EncodeToString
	input := b64(hb) + "." + b64(cb)
	hash := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + b64(sig)
}
func (f *iamFixture) handler(writer audit.Writer, engine policy.Engine) http.Handler {
	api := access.API{DB: f.db, Auth: f.verifier, Policy: engine, Audit: writer}
	return httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register})
}
func call(h http.Handler, path, token string, headers map[string]string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestM1AuthenticatedTenantAndPolicyBoundary(t *testing.T) {
	f := newIAM(t)
	h := f.handler(audit.Postgres{}, policy.Native{})
	alice := f.token(t, f.aliceSubject, nil)
	bob := f.token(t, f.bobSubject, nil)
	viewer := f.token(t, f.viewerSubject, nil)
	cases := []struct {
		name, path, token, body string
		headers                 map[string]string
		status                  int
	}{
		{name: "authentication required", path: "/v1/session", status: 401},
		{name: "unprovisioned identity", path: "/v1/session", token: f.token(t, "unknown", nil), status: 401},
		{name: "invalid token", path: "/v1/session", token: "forged-token", status: 401},
		{name: "session", path: "/v1/session", token: alice, status: 200},
		{name: "own tenant", path: "/v1/tenants/" + f.tenantA + "/access", token: alice, status: 200},
		{name: "cross tenant", path: "/v1/tenants/" + f.tenantB + "/access", token: alice, status: 403},
		{name: "forged token role and tenant", path: "/v1/tenants/" + f.tenantB + "/access", token: f.token(t, f.aliceSubject, map[string]any{"tenant_id": f.tenantB, "roles": []string{"owner"}}), status: 403},
		{name: "forged tenant header", path: "/v1/tenants/" + f.tenantA + "/access", token: bob, headers: map[string]string{"X-Tenant-ID": f.tenantA}, status: 403},
		{name: "header cannot override path", path: "/v1/tenants/" + f.tenantA + "/access", token: alice, headers: map[string]string{"X-Tenant-ID": f.tenantB}, status: 200},
		{name: "tenant body rejected", path: "/v1/tenants/" + f.tenantA + "/access", token: alice, body: `{"tenant_id":"forged"}`, status: 413},
		{name: "malformed tenant", path: "/v1/tenants/forged/access", token: alice, status: 403},
		{name: "viewer membership list denied", path: "/v1/tenants/" + f.tenantA + "/members", token: viewer, status: 403},
		{name: "owner membership list", path: "/v1/tenants/" + f.tenantA + "/members", token: alice, status: 200},
		{name: "direct admin endpoint bypass", path: "/v1/admin/grant", token: alice, status: 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := call(h, tc.path, tc.token, tc.headers, tc.body)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing correlation")
			}
			if tc.name == "session" {
				if strings.Contains(w.Body.String(), f.tenantB) || !strings.Contains(w.Body.String(), f.tenantA) {
					t.Fatal("session leaked another tenant")
				}
			}
			if tc.name == "owner membership list" && strings.Contains(w.Body.String(), f.bob) {
				t.Fatal("membership list leaked another tenant")
			}
			if tc.name == "own tenant" {
				var body struct {
					Decision policy.Decision `json:"decision"`
				}
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || !body.Decision.Allow {
					t.Fatal("decision missing")
				}
				var recorded bool
				if err := f.operator.QueryRow(`SELECT allowed FROM audit.entries WHERE decision_id=$1 AND request_id=$2 AND actor_id=$3 AND tenant_id=$4 AND policy_version=$5`, body.Decision.ID, w.Header().Get("X-Request-ID"), f.alice, f.tenantA, policy.Version).Scan(&recorded); err != nil || !recorded {
					t.Fatal("policy decision not correlated with audit")
				}
			}
		})
	}
	// A role change is read fresh, independent of JWT role claims or the last request.
	if _, err := f.operator.Exec(`UPDATE iam.memberships SET role='member' WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice); err != nil {
		t.Fatal(err)
	}
	if w := call(h, "/v1/tenants/"+f.tenantA+"/members", alice, nil, ""); w.Code != 403 {
		t.Fatal("removed role retained access")
	}
	if _, err := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice); err != nil {
		t.Fatal(err)
	}
	if w := call(h, "/v1/tenants/"+f.tenantA+"/access", alice, nil, ""); w.Code != 403 {
		t.Fatal("revoked membership retained access")
	}
	if _, err := f.operator.Exec(`UPDATE iam.users SET active=false WHERE id=$1`, f.bob); err != nil {
		t.Fatal(err)
	}
	if w := call(h, "/v1/session", bob, nil, ""); w.Code != 401 {
		t.Fatal("disabled identity retained access")
	}
}

type failedAudit struct{}

func (failedAudit) Append(context.Context, postgres.Executor, audit.Entry) error {
	return errors.New("simulated unavailable audit sink")
}

type obligationPolicy struct{}
type denyingPolicy struct{}

func (denyingPolicy) Decide(context.Context, policy.Subject, string, policy.Resource, policy.Context) policy.Decision {
	return policy.Decision{Allow: false, ID: "deny", Version: "test", Reason: "deny"}
}

func (obligationPolicy) Decide(context.Context, policy.Subject, string, policy.Resource, policy.Context) policy.Decision {
	return policy.Decision{Allow: true, ID: "test", Version: "test", Obligations: []string{"unimplemented-mfa"}}
}
func TestM1AuditAndObligationsFailClosed(t *testing.T) {
	f := newIAM(t)
	token := f.token(t, f.aliceSubject, nil)
	path := "/v1/tenants/" + f.tenantA + "/access"
	if w := call(f.handler(failedAudit{}, policy.Native{}), path, token, nil, ""); w.Code != 503 || strings.Contains(w.Body.String(), "roles") {
		t.Fatal("returned protected data without durable audit")
	}
	if w := call(f.handler(audit.Postgres{}, obligationPolicy{}), path, token, nil, ""); w.Code != 403 {
		t.Fatal("unknown obligation permitted")
	}
	if w := call(f.handler(audit.Postgres{}, nil), path, token, nil, ""); w.Code != 503 {
		t.Fatal("missing policy permitted")
	}
	if w := call(f.handler(audit.Postgres{}, obligationPolicy{}), "/v1/session", token, nil, ""); w.Code != 403 {
		t.Fatal("identity session bypassed policy obligations")
	}
	if w := call(f.handler(audit.Postgres{}, denyingPolicy{}), "/v1/session", token, nil, ""); w.Code != 403 {
		t.Fatal("identity session bypassed a deny decision")
	}
}
func TestM1RuntimePrivilegesRLSAndPoolContext(t *testing.T) {
	f := newIAM(t)
	ctx := context.Background()
	f.db.SetMaxOpenConns(1)
	if postgres.Ping(ctx, f.operator) == nil {
		t.Fatal("migration role accepted for runtime")
	}
	// Directly configuring the HTTP adapter with migration credentials fails closed.
	api := access.API{DB: f.operator, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}}
	mux := http.NewServeMux()
	api.Register(mux)
	if w := call(mux, "/v1/session", f.token(t, f.aliceSubject, nil), nil, ""); w.Code != 503 {
		t.Fatal("unsafe runtime credentials served protected route")
	}
	for _, table := range []string{"iam.users", "iam.tenants", "iam.memberships"} {
		var count int
		if err := f.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("missing context exposes %s count=%d", table, count)
		}
	}
	for _, subject := range []string{f.aliceSubject, f.bobSubject, f.aliceSubject} {
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := f.verifier.Verify(f.token(t, subject, nil))
		if err != nil {
			t.Fatal(err)
		}
		u, err := (identity.Store{}).Resolve(ctx, tx, p)
		if err != nil {
			t.Fatal(err)
		}
		if err = (tenant.Store{}).BindActor(ctx, tx, u); err != nil {
			t.Fatal(err)
		}
		selected, other := f.tenantA, f.tenantB
		if subject == f.bobSubject {
			selected, other = other, selected
		}
		trusted, err := (tenant.Store{}).Resolve(ctx, tx, u, selected)
		if err != nil || trusted.TenantID() != selected {
			t.Fatal("trusted resolution failed")
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM iam.memberships WHERE tenant_id=$1`, other).Scan(&count); err != nil || count != 0 {
			t.Fatal("cross tenant membership read")
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var leaked bool
		if err = f.db.QueryRow(`SELECT coalesce(current_setting('app.actor_id',true),'')<>'' OR coalesce(current_setting('app.tenant_id',true),'')<>'' OR coalesce(current_setting('app.auth_subject',true),'')<>''`).Scan(&leaked); err != nil || leaked {
			t.Fatal("pooled tenant/actor context leaked after commit")
		}
	}
	for _, query := range []string{`INSERT INTO iam.tenants(id,name) VALUES(gen_random_uuid(),'unauthorized')`, `UPDATE iam.memberships SET role='owner'`, `DELETE FROM iam.memberships`, `UPDATE audit.entries SET allowed=true`, `DELETE FROM audit.entries`, `TRUNCATE audit.entries`, `INSERT INTO audit.entries(action,resource,allowed,decision_id,policy_version,request_id,trace_id,writer_role) VALUES('authentication.denied','identity',false,'','','','','forged')`} {
		if _, err := f.db.ExecContext(ctx, query); err == nil {
			t.Fatal("runtime write privilege exceeded")
		}
	}
	// Table owner also cannot erase evidence through ordinary DML.
	if _, err := f.operator.Exec(`DELETE FROM audit.entries WHERE false`); err == nil {
		t.Fatal("audit mutation trigger bypassed")
	}
	var forced int
	if err := f.operator.QueryRow(`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname IN ('iam','audit') AND c.relname IN ('users','tenants','memberships','entries') AND c.relrowsecurity AND c.relforcerowsecurity`).Scan(&forced); err != nil || forced != 4 {
		t.Fatal("IAM RLS backstop missing")
	}
}
func TestM1ProvisioningRecoveryAndOwnerInvariant(t *testing.T) {
	f := newIAM(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, tenantID, user, role string
		active                     bool
	}{
		{"cannot remove last owner", f.tenantA, f.alice, "viewer", true},
		{"cannot revoke last owner", f.tenantA, f.alice, "owner", false},
		{"cannot bootstrap viewer", uuid(t), f.viewer, "viewer", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := f.operator.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err = (tenant.Store{}).Provision(ctx, tx, tc.tenantID, "Test", tc.user, tc.role, tc.active); err == nil {
				t.Fatal("owner invariant violated")
			}
		})
	}
	tx, err := f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	subject := uuid(t)
	id, err := (identity.Store{}).Provision(ctx, tx, "https://test-issuer.example", subject, "Rollback fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	if (failedAudit{}).Append(ctx, tx, audit.Entry{}) == nil {
		t.Fatal("failure fixture")
	}
	_ = tx.Rollback()
	var count int
	if err = f.operator.QueryRow(`SELECT count(*) FROM iam.users WHERE id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed provisioning was committed")
	}
	// Successful change + state evidence share one transaction.
	tx, err = f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = (tenant.Store{}).Provision(ctx, tx, f.tenantA, "Test", f.viewer, "member", true); err != nil {
		t.Fatal(err)
	}
	active := true
	e := audit.Entry{TenantID: f.tenantA, Action: "operator.provision", Resource: f.viewer, Allow: true, PolicyVersion: "operator-m1-v1", State: audit.State{OperatorRef: "integration-operator", Role: "member", UserActive: &active, MembershipActive: &active}}
	if err = (audit.Postgres{}).Append(ctx, tx, e); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var role, writer string
	if err = f.operator.QueryRow(`SELECT state->>'role',writer_role FROM audit.entries WHERE tenant_id=$1 AND action='operator.provision'`, f.tenantA).Scan(&role, &writer); err != nil || role != "member" || writer != "framework_migrator" {
		t.Fatal("operator audit state/provenance missing")
	}
}

func TestM1ConcurrentLastOwnerChanges(t *testing.T) {
	f := newIAM(t)
	ctx := context.Background()
	tx, err := f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = (tenant.Store{}).Provision(ctx, tx, f.tenantA, "Test", f.viewer, "owner", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	results := make(chan bool, 2)
	for _, user := range []string{f.alice, f.viewer} {
		go func() {
			<-ready
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			tx, err := f.operator.BeginTx(ctx, nil)
			if err != nil {
				results <- false
				return
			}
			defer tx.Rollback()
			if err = (tenant.Store{}).Provision(ctx, tx, f.tenantA, "Test", user, "viewer", true); err != nil {
				results <- false
				return
			}
			results <- tx.Commit() == nil
		}()
	}
	close(ready)
	successes := 0
	for range 2 {
		if <-results {
			successes++
		}
	}
	var owners int
	if err = f.operator.QueryRow(`SELECT count(*) FROM iam.memberships WHERE tenant_id=$1 AND role='owner' AND active`, f.tenantA).Scan(&owners); err != nil || owners != 1 || successes != 1 {
		t.Fatalf("concurrent owner changes: owners=%d successful=%d", owners, successes)
	}
}

func TestM1OperatorCommand(t *testing.T) {
	f := newIAM(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"run", "./cmd/iam", "--issuer", "https://test-issuer.example", "--subject", f.viewerSubject, "--name", "Viewer", "--tenant", f.tenantA, "--tenant-name", "Test workspace", "--role", "member", "--user-status", "active", "--membership-status", "active", "--operator", "integration-cli-review"}
	command := exec.CommandContext(ctx, filepath.Join(root, "scripts/go.sh"), args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("operator CLI failed: %s", output)
	}
	var role string
	if err = f.operator.QueryRow(`SELECT role FROM iam.memberships WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.viewer).Scan(&role); err != nil || role != "member" {
		t.Fatal("CLI role change not committed")
	}
	var recorded bool
	if err = f.operator.QueryRow(`SELECT EXISTS(SELECT 1 FROM audit.entries WHERE tenant_id=$1 AND resource=$2 AND state->>'operator_ref'='integration-cli-review' AND state->>'role'='member')`, f.tenantA, f.viewer).Scan(&recorded); err != nil || !recorded {
		t.Fatal("CLI state evidence missing")
	}
	// Reusing runtime credentials for the operator CLI must fail, without a grant.
	command = exec.CommandContext(ctx, filepath.Join(root, "scripts/go.sh"), args...)
	command.Dir = root
	command.Env = append(os.Environ(), "MIGRATION_DATABASE_URL="+os.Getenv("DATABASE_URL"))
	if command.Run() == nil {
		t.Fatal("operator command accepted runtime credentials")
	}
}

func TestM1DevelopmentIssuer(t *testing.T) {
	f := newIAM(t)
	ctx := context.Background()
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	keysPath := c.AuthJWKSFile
	if !filepath.IsAbs(keysPath) {
		keysPath = filepath.Join(root, keysPath)
	}
	keys, err := os.ReadFile(keysPath)
	if err != nil {
		t.Fatal("local public keys unavailable")
	}
	verifier, err := identity.NewVerifier(c.AuthIssuer, c.AuthAudience, keys)
	if err != nil {
		t.Fatal(err)
	}
	subject := uuid(t)
	command := exec.CommandContext(ctx, "node", "scripts/dev-identity.mjs", "token", subject)
	command.Dir = root
	token, err := command.Output()
	if err != nil {
		t.Fatal("local token issuance failed; only local development issuer is supported by this fixture")
	}
	if _, err = verifier.Verify(strings.TrimSpace(string(token))); err != nil {
		t.Fatal("local issuer produced an unverifiable access token")
	}
	tx, err := f.operator.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	userID, err := (identity.Store{}).Provision(ctx, tx, c.AuthIssuer, subject, "Local issuer fixture", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = (tenant.Store{}).Provision(ctx, tx, f.tenantA, "Test", userID, "viewer", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = f.operator.Exec(`DELETE FROM iam.memberships WHERE user_id=$1`, userID)
		_, _ = f.operator.Exec(`DELETE FROM iam.users WHERE id=$1`, userID)
	})
	api := access.API{DB: f.db, Auth: verifier, Policy: policy.Native{}, Audit: audit.Postgres{}}
	mux := http.NewServeMux()
	api.Register(mux)
	if w := call(mux, "/v1/session", strings.TrimSpace(string(token)), nil, ""); w.Code != 200 {
		t.Fatal("local issuer authentication failed")
	}
	command = exec.CommandContext(ctx, "node", "scripts/dev-identity.mjs", "token", subject)
	command.Dir = root
	command.Env = append(os.Environ(), "APP_ENV=production")
	if command.Run() == nil {
		t.Fatal("local issuer allowed production token issuance")
	}
}

func TestM1FreshDatabaseMigrations(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := config.Load(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if c.Environment == "production" {
		t.Fatal("fresh database verification is local development/test only")
	}
	adminURL, err := url.Parse(c.DatabaseURL)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		t.Fatal("local admin binding required")
	}
	adminURL.User = url.UserPassword("postgres", password)
	admin, err := postgres.Open(adminURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := "m1_check_" + strings.ReplaceAll(uuid(t), "-", "")
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()+` OWNER framework_migrator`); err != nil {
		t.Fatal("fresh verification database creation failed")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Error("fresh verification database cleanup failed")
		}
	}()
	if _, err = admin.ExecContext(ctx, `REVOKE ALL ON DATABASE `+pgx.Identifier{name}.Sanitize()+` FROM PUBLIC`); err != nil {
		t.Fatal("fresh database privilege setup failed")
	}
	if _, err = admin.ExecContext(ctx, `GRANT CONNECT ON DATABASE `+pgx.Identifier{name}.Sanitize()+` TO framework_app`); err != nil {
		t.Fatal("fresh runtime connect grant failed")
	}
	migrateURL, err := url.Parse(os.Getenv("MIGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal("migration binding invalid")
	}
	migrateURL.Path = "/" + name
	query := migrateURL.Query()
	query.Del("dbname")
	query.Del("database")
	query.Del("user")
	migrateURL.RawQuery = query.Encode()
	binding, e := pgx.ParseConfig(migrateURL.String())
	if e != nil || binding.Database != name || binding.User != "framework_migrator" {
		t.Fatal("effective fresh migration binding mismatch")
	}
	migrate, err := postgres.Open(migrateURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer migrate.Close()
	var actual, role string
	if e = migrate.QueryRowContext(ctx, `SELECT current_database(),session_user`).Scan(&actual, &role); e != nil || actual != name || role != "framework_migrator" {
		t.Fatal("connected fresh migration binding mismatch")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, migrate, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal("fresh migration provider failed")
	}
	results, err := provider.Up(ctx)
	files, fileErr := migrations.Files.ReadDir(".")
	total := 0
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".sql") {
			total++
		}
	}
	if err != nil || fileErr != nil || total == 0 || len(results) != total {
		t.Fatal("fresh migration chain failed")
	}
	results, err = provider.Up(ctx)
	if err != nil || len(results) != 0 {
		t.Fatal("fresh migrations not repeatable")
	}
	runtimeURL, err := url.Parse(c.DatabaseURL)
	if err != nil {
		t.Fatal("runtime binding invalid")
	}
	runtimeURL.Path = "/" + name
	query = runtimeURL.Query()
	query.Del("dbname")
	query.Del("database")
	query.Del("user")
	runtimeURL.RawQuery = query.Encode()
	binding, e = pgx.ParseConfig(runtimeURL.String())
	if e != nil || binding.Database != name || binding.User != "framework_app" {
		t.Fatal("effective fresh runtime binding mismatch")
	}
	runtimeDB, err := postgres.Open(runtimeURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeDB.Close()
	if err = postgres.Ping(ctx, runtimeDB); err != nil {
		t.Fatal("fresh least-privilege runtime unavailable")
	}
	if (identity.Store{}).Ready(ctx, runtimeDB) != nil || (tenant.Store{}).Ready(ctx, runtimeDB) != nil || (audit.Postgres{}).Ready(ctx, runtimeDB) != nil {
		t.Fatal("fresh IAM/audit readiness contract failed")
	}
	var count int
	if err = runtimeDB.QueryRowContext(ctx, `SELECT count(*) FROM iam.memberships`).Scan(&count); err != nil || count != 0 {
		t.Fatal("fresh IAM context fails closed")
	}

	// Reusable RLS is installed and deliberately broken only in this disposable DB.
	if _, err = migrate.ExecContext(ctx, `CREATE TABLE runtime.m2_guard (tenant_id uuid NOT NULL,id int NOT NULL,PRIMARY KEY(tenant_id,id)); SELECT runtime.install_tenant_rls('runtime.m2_guard'::regclass)`); err != nil {
		t.Fatal("fresh RLS fixture failed")
	}
	for _, ddl := range []string{`ALTER TABLE runtime.m2_guard DISABLE ROW LEVEL SECURITY`, `ALTER TABLE runtime.m2_guard NO FORCE ROW LEVEL SECURITY`, `GRANT TRUNCATE ON runtime.m2_guard TO framework_app`, `GRANT REFERENCES(id) ON runtime.m2_guard TO framework_app`, `REVOKE ALL ON runtime.m2_guard FROM framework_app; GRANT SELECT(id) ON runtime.m2_guard TO framework_app; ALTER TABLE runtime.m2_guard DISABLE ROW LEVEL SECURITY`} {
		if _, err = migrate.ExecContext(ctx, ddl); err != nil {
			t.Fatal("isolated security drift failed")
		}
		called := false
		err = postgres.WithRuntime(ctx, runtimeDB, func(context.Context, postgres.Executor) error { called = true; return nil })
		if !errors.Is(err, postgres.ErrUnavailable) || called || postgres.Ping(ctx, runtimeDB) == nil {
			t.Fatal("unsafe table privileges/RLS accepted")
		}
		if _, err = migrate.ExecContext(ctx, `ALTER TABLE runtime.m2_guard ENABLE ROW LEVEL SECURITY; ALTER TABLE runtime.m2_guard FORCE ROW LEVEL SECURITY; REVOKE TRUNCATE ON runtime.m2_guard FROM framework_app; REVOKE SELECT(id),REFERENCES(id) ON runtime.m2_guard FROM framework_app; GRANT SELECT,INSERT,UPDATE,DELETE ON runtime.m2_guard TO framework_app`); err != nil {
			t.Fatal("security drift recovery failed")
		}
		if err = postgres.WithRuntime(ctx, runtimeDB, func(context.Context, postgres.Executor) error { return nil }); err != nil {
			t.Fatal("security drift recovery unavailable")
		}
	}
	checkNonInheritedOwnerMembership(t, ctx, adminURL, name)
	registry, err := metadata.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if registry.Ready(ctx, runtimeDB) != nil {
		t.Fatal("fresh metadata readiness failed")
	}
	for _, contract := range []struct{ table, name string }{{"control.metadata_heads", "metadata_head_revision"}, {"control.metadata_versions", "metadata_envelope_required"}} {
		var definition string
		if err = migrate.QueryRowContext(ctx, `SELECT pg_catalog.pg_get_constraintdef(oid) FROM pg_catalog.pg_constraint WHERE conrelid=$1::regclass AND conname=$2`, contract.table, contract.name).Scan(&definition); err != nil {
			t.Fatal("metadata constraint fixture unavailable")
		}
		if _, err = migrate.ExecContext(ctx, "ALTER TABLE "+contract.table+" DROP CONSTRAINT "+contract.name); err != nil {
			t.Fatal("isolated metadata drift failed")
		}
		if registry.Ready(ctx, runtimeDB) == nil {
			t.Fatal("metadata accepted missing integrity constraint")
		}
		if _, err = migrate.ExecContext(ctx, "ALTER TABLE "+contract.table+" ADD CONSTRAINT "+contract.name+" "+definition); err != nil {
			t.Fatal("isolated metadata constraint recovery failed")
		}
		if registry.Ready(ctx, runtimeDB) != nil {
			t.Fatal("metadata constraint recovery unavailable")
		}
	}
	if _, err = migrate.ExecContext(ctx, `ALTER TABLE control.metadata_versions DISABLE TRIGGER metadata_immutable`); err != nil {
		t.Fatal("isolated metadata trigger drift failed")
	}
	if registry.Ready(ctx, runtimeDB) == nil {
		t.Fatal("metadata accepted disabled immutable trigger")
	}
	if _, err = migrate.ExecContext(ctx, `ALTER TABLE control.metadata_versions ENABLE TRIGGER metadata_immutable`); err != nil {
		t.Fatal("metadata trigger recovery failed")
	}
	// Prove the trigger backstop with its owner in this empty disposable database.
	// Shared development metadata is never subjected to a TRUNCATE probe.
	for _, statement := range []string{
		`UPDATE control.metadata_versions SET source_hash=source_hash WHERE false`,
		`DELETE FROM control.metadata_versions WHERE false`,
		`TRUNCATE control.metadata_heads,control.metadata_versions`,
	} {
		if _, mutationErr := migrate.ExecContext(ctx, statement); mutationErr == nil || !strings.Contains(mutationErr.Error(), "metadata source revisions are immutable") {
			t.Fatal("metadata owner mutation did not hit immutable trigger")
		}
	}
	// Exercise drift only in the uniquely owned disposable verification database.
	// A previously applied IAM schema alone must not hide an absent audit upgrade.
	if _, err = migrate.ExecContext(ctx, `ALTER TABLE audit.entries DROP COLUMN state`); err != nil {
		t.Fatal("isolated audit-drift fixture failed")
	}
	if (audit.Postgres{}).Ready(ctx, runtimeDB) == nil {
		t.Fatal("readiness accepted an incomplete audit migration")
	}
}
