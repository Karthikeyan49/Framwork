//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func m3Registry(t *testing.T) *metadata.Registry {
	t.Helper()
	r, err := metadata.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func m3Source(t *testing.T, app string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/vendor-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return []byte(strings.Replace(string(raw), "app.vendors", app, 1))
}
func m3Handler(f *iamFixture, r *metadata.Registry, writer audit.Writer, engine policy.Engine) http.Handler {
	api := access.API{DB: f.db, Auth: f.verifier, Policy: engine, Audit: writer, Registry: r}
	return httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Origins: []string{"https://trusted.example"}, Register: api.Register, AllowPOST: api.AcceptsPOST})
}
func m3Call(h http.Handler, method, path, token string, expected int64, source []byte) *httptest.ResponseRecorder {
	var body string
	if source != nil {
		payload, _ := json.Marshal(struct {
			Expected int64           `json:"expected_revision"`
			Document json.RawMessage `json:"document"`
		}{expected, source})
		body = string(payload)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
func m3Scope(f *iamFixture, ctx context.Context, q postgres.Executor, p identity.Principal, selected string) (tenant.Access, error) {
	u, err := (identity.Store{}).Resolve(ctx, q, p)
	if err != nil {
		return tenant.Access{}, err
	}
	ts := tenant.Store{}
	if err = ts.BindActor(ctx, q, u); err != nil {
		return tenant.Access{}, err
	}
	return ts.Resolve(ctx, q, u, selected)
}
func TestM3VersionedAuthoringPolicyAuditAndTenantIsolation(t *testing.T) {
	f := newIAM(t)
	registry := m3Registry(t)
	h := m3Handler(f, registry, audit.Postgres{}, policy.Native{})
	app := "app.test_" + strings.ReplaceAll(uuid(t), "-", "")
	raw := m3Source(t, app)
	pathA := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	pathB := "/v1/tenants/" + f.tenantB + "/metadata/" + app + "/versions"
	alice, bob, viewer := f.token(t, f.aliceSubject, nil), f.token(t, f.bobSubject, nil), f.token(t, f.viewerSubject, nil)
	for _, c := range []struct {
		path, token string
		status      int
	}{{pathA, "", 401}, {pathA, bob, 403}, {pathB, alice, 403}, {pathA, viewer, 403}} {
		if w := m3Call(h, "POST", c.path, c.token, 0, raw); w.Code != c.status {
			t.Fatalf("policy status %d want %d: %s", w.Code, c.status, w.Body.String())
		}
	}
	created := m3Call(h, "POST", pathA, alice, 0, raw)
	if created.Code != 201 {
		t.Fatalf("create %d: %s", created.Code, created.Body.String())
	}
	var first metadata.Version
	if json.Unmarshal(created.Body.Bytes(), &first) != nil || first.Revision != 1 || first.CreatedBy != f.alice || first.Status != "draft" {
		t.Fatal("invalid revision/provenance")
	}
	var evidence int
	if err := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=$1 AND action='metadata.write' AND allowed AND tenant_id=$2 AND actor_id=$3 AND state->>'metadata_hash'=$4 AND state->>'metadata_revision'='1'`, created.Header().Get("X-Request-ID"), f.tenantA, f.alice, first.SourceHash).Scan(&evidence); err != nil || evidence != 1 {
		t.Fatal("missing atomic metadata audit")
	}
	if w := m3Call(h, "POST", pathA, alice, 0, raw); w.Code != 409 {
		t.Fatal("stale expected revision accepted")
	}
	changed := []byte(strings.Replace(string(raw), "Vendor editor", "Revised editor", 1))
	if w := m3Call(h, "POST", pathA, alice, 1, changed); w.Code != 201 {
		t.Fatalf("append failed: %d %s", w.Code, w.Body.String())
	}
	historical := m3Call(h, "GET", pathA+"/1", viewer, 0, nil)
	if historical.Code != 200 || strings.Contains(historical.Body.String(), "Revised editor") {
		t.Fatal("immutable history changed or viewer read denied")
	}
	if w := m3Call(h, "POST", pathB, bob, 0, raw); w.Code != 201 {
		t.Fatal("tenant-inclusive app identity collides")
	}
	if w := m3Call(h, "GET", pathB+"/1", alice, 0, nil); w.Code != 403 {
		t.Fatal("foreign metadata read")
	}
	listed := m3Call(h, "GET", pathA+"?before=2", alice, 0, nil)
	if listed.Code != 200 || strings.Contains(listed.Body.String(), `"revision":2`) || !strings.Contains(listed.Body.String(), `"revision":1`) {
		t.Fatal("version cursor failed")
	}
	if w := m3Call(h, "GET", pathA+"/999", alice, 0, nil); w.Code != 404 {
		t.Fatal("unknown version accepted")
	}
	if w := m3Call(h, "POST", pathA, alice, 2, m3Source(t, "app.other")); w.Code != 400 {
		t.Fatal("route/body app mismatch accepted")
	}
	bad := []byte(strings.Replace(string(raw), `"schema_version":"1.0.0"`, `"schema_version":"9.0.0"`, 1))
	rejected := m3Call(h, "POST", pathA, alice, 2, bad)
	if rejected.Code != 400 || !strings.Contains(rejected.Body.String(), `"request_id"`) || !strings.Contains(rejected.Body.String(), `"issues"`) {
		t.Fatal("schema error convention")
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=$1 AND action='metadata.write.rejected' AND NOT allowed`, rejected.Header().Get("X-Request-ID")).Scan(&evidence); err != nil || evidence != 1 {
		t.Fatal("invalid source rejection not audited")
	}
	// Whole-table scans with no tenant predicate still expose only the selected tenant.
	p, _ := f.verifier.Verify(alice)
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		scope, err := m3Scope(f, ctx, q, p, f.tenantA)
		if err != nil {
			return err
		}
		var foreign int
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM control.metadata_versions WHERE tenant_id=$1`, f.tenantB).Scan(&foreign); err != nil || foreign != 0 {
			return errors.New("foreign revisions visible")
		}
		old, err := registry.Get(ctx, q, scope, app, 1)
		if err != nil || old.SourceHash != first.SourceHash {
			return errors.New("JSONB source hash roundtrip failed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestM3ImmutableGrantsNoContextAndAuditRollback(t *testing.T) {
	f := newIAM(t)
	registry := m3Registry(t)
	app := "app.test_" + strings.ReplaceAll(uuid(t), "-", "")
	raw := m3Source(t, app)
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	token := f.token(t, f.aliceSubject, nil)
	good := m3Handler(f, registry, audit.Postgres{}, policy.Native{})
	if w := m3Call(good, "POST", path, token, 0, raw); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := m3Call(m3Handler(f, registry, failedAudit{}, policy.Native{}), "POST", path, token, 1, raw); w.Code != 503 {
		t.Fatal("failed audit returned success")
	}
	if w := m3Call(good, "POST", path, token, 1, raw); w.Code != 201 {
		t.Fatal("audit failure left head/version write")
	}
	if w := m3Call(m3Handler(f, registry, audit.Postgres{}, obligationPolicy{}), "POST", path, token, 2, raw); w.Code != 403 {
		t.Fatal("unsupported policy obligation bypassed")
	}
	p, _ := f.verifier.Verify(token)
	for _, query := range []string{
		`UPDATE control.metadata_versions SET document='{}'::jsonb WHERE app_id=$1`,
		`DELETE FROM control.metadata_versions WHERE app_id=$1`,
		`UPDATE control.metadata_heads SET namespace='spoof' WHERE id=$1`,
		`TRUNCATE control.metadata_versions`,
		`UPDATE control.metadata_versions SET created_by='11111111-1111-4111-8111-111111111111' WHERE app_id=$1`,
	} {
		if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, p, f.tenantA); err != nil {
				return err
			}
			var err error
			if strings.HasPrefix(query, "TRUNCATE") {
				_, err = q.ExecContext(ctx, query)
			} else {
				_, err = q.ExecContext(ctx, query, app)
			}
			return err
		}); err == nil {
			t.Fatal("immutable/grant mutation succeeded")
		}
	}
	// Deferred head FK rejects an absent version at commit, without leaving drift.
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, p, f.tenantA); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `UPDATE control.metadata_heads SET revision=999 WHERE tenant_id=$1 AND id=$2`, f.tenantA, app)
		return err
	}); !errors.Is(err, postgres.ErrCommitUnknown) {
		t.Fatal("missing history head committed")
	}
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		var count int
		err := q.QueryRowContext(ctx, `SELECT count(*) FROM control.metadata_versions`).Scan(&count)
		if err != nil || count != 0 {
			return errors.New("missing context exposed registry")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w := m3Call(good, "GET", path+"/2", token, 0, nil); w.Code != 200 {
		t.Fatal("head drift recovery failed")
	}
}
func TestM3ConcurrentAppendNoLostUpdate(t *testing.T) {
	f := newIAM(t)
	r := m3Registry(t)
	h := m3Handler(f, r, audit.Postgres{}, policy.Native{})
	app := "app.race_" + strings.ReplaceAll(uuid(t), "-", "")
	source := m3Source(t, app)
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	token := f.token(t, f.aliceSubject, nil)
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; statuses <- m3Call(h, "POST", path, token, 0, source).Code }()
	}
	close(start)
	wg.Wait()
	close(statuses)
	successes := 0
	for status := range statuses {
		if status == 201 {
			successes++
		} else if status != 409 && status != 503 {
			t.Fatalf("unexpected concurrent status %d", status)
		}
	}
	if successes != 1 {
		t.Fatal("concurrent write lost revision guard")
	}
	if w := m3Call(h, "POST", path, token, 0, source); w.Code != 409 {
		t.Fatal("stale retry accepted")
	}
	if w := m3Call(h, "POST", path, token, 1, source); w.Code != 201 {
		t.Fatal("race recovery failed")
	}
}
func TestM3POSTLimitsAndCORS(t *testing.T) {
	f := newIAM(t)
	h := m3Handler(f, m3Registry(t), audit.Postgres{}, policy.Native{})
	app := "app.bounds"
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	token := f.token(t, f.aliceSubject, nil)
	cases := []struct {
		body, contentType string
		status            int
	}{
		{`{"expected_revision":0,"expected_revision":1,"document":{}}`, "application/json", 400},
		{`{"expected_revision":0,"document":{},"tenant_id":"forged"}`, "application/json", 400},
		{`{}`, "text/plain", 415},
		{strings.Repeat(" ", metadata.MaxBytes+4097), "application/json", 413},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("POST", path, strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", tc.contentType)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("bounded POST status=%d want=%d", rec.Code, tc.status)
		}
	}
	for _, route := range []string{path, "/healthz", "/v1/session"} {
		req := httptest.NewRequest("OPTIONS", route, nil)
		req.Header.Set("Origin", "https://trusted.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		want := 403
		if route == path {
			want = 204
		}
		if rec.Code != want {
			t.Fatal("CORS POST route boundary")
		}
	}
}

func TestM3NearQuotaJSONBRoundTripAndEnvelopeStorage(t *testing.T) {
	f := newIAM(t)
	r := m3Registry(t)
	h := m3Handler(f, r, audit.Postgres{}, policy.Native{})
	app := "app.quota_" + strings.ReplaceAll(uuid(t), "-", "")
	var document map[string]any
	if json.Unmarshal(m3Source(t, app), &document) != nil {
		t.Fatal("source fixture")
	}
	defs := document["definitions"].([]any)
	raw, _ := json.Marshal(document)
	for i := 0; len(raw) < metadata.MaxBytes-1400; i++ {
		defs = append(defs, map[string]any{"id": fmt.Sprintf("rule.quota_%d", i), "schema_version": "1.0.0", "namespace": "example.vendors", "label": "Large valid rule", "kind": "rule", "rule": map[string]any{"entity_ref": "entity.vendor", "result_type": "string", "expression": map[string]any{"op": "literal", "value": strings.Repeat("x", 1000)}}})
		document["definitions"] = defs
		raw, _ = json.Marshal(document)
	}
	if len(raw) > metadata.MaxBytes {
		t.Fatal("quota fixture unexpectedly too large")
	}
	source, err := r.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	token := f.token(t, f.aliceSubject, nil)
	if w := m3Call(h, "POST", path, token, 0, raw); w.Code != 201 {
		t.Fatalf("near-limit create %d %s", w.Code, w.Body.String())
	}
	got := m3Call(h, "GET", path+"/1", token, 0, nil)
	if got.Code != 200 {
		t.Fatalf("JSONB whitespace made source unreadable: %d", got.Code)
	}
	var version metadata.Version
	if json.Unmarshal(got.Body.Bytes(), &version) != nil || version.SourceHash != source.Hash() {
		t.Fatal("near-quota source hash changed")
	}
	principal, _ := f.verifier.Verify(token)
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		var storedSize int
		if err := q.QueryRowContext(ctx, `SELECT octet_length(document::text) FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=1`, f.tenantA, app).Scan(&storedSize); err != nil {
			return err
		}
		if storedSize <= metadata.MaxBytes || storedSize > 300000 {
			return errors.New("fixture did not exercise JSONB whitespace expansion")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`{}`, `{"schema_version":null,"app_id":null,"namespace":"test","definitions":[]}`} {
		if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
				return err
			}
			_, err := q.ExecContext(ctx, `INSERT INTO control.metadata_versions(tenant_id,app_id,revision,document,source_hash) VALUES($1,$2,99,$3::jsonb,$4)`, f.tenantA, app, bad, source.Hash())
			return err
		}); err == nil {
			t.Fatal("missing/null required envelope accepted by storage")
		}
	}
	// Numeric spellings survive PostgreSQL JSONB normalization without float loss.
	numeric := []byte(strings.Replace(string(m3Source(t, app)), `"value":""`, `"value":1e3`, 1))
	if w := m3Call(h, "POST", path, token, 1, numeric); w.Code != 201 {
		t.Fatalf("exact-number source rejected: %d", w.Code)
	}
	if w := m3Call(h, "GET", path+"/2", token, 0, nil); w.Code != 200 {
		t.Fatal("exact number JSONB hash failed")
	}
}

func TestM3YAMLUsesTheSameProtectedRegistry(t *testing.T) {
	f := newIAM(t)
	h := m3Handler(f, m3Registry(t), audit.Postgres{}, policy.Native{})
	app := "app.yaml_" + strings.ReplaceAll(uuid(t), "-", "")
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	token := f.token(t, f.aliceSubject, nil)
	body := fmt.Sprintf(`expected_revision: 0
document:
  schema_version: 1.0.0
  app_id: %s
  namespace: example
  base_version: null
  definitions:
    - id: entity.vendor
      schema_version: 1.0.0
      namespace: example
      label: Vendor
      kind: entity
      entity:
        fields: []
`, app)
	call := func(body, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/yaml")
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := call(body, f.token(t, f.viewerSubject, nil)); w.Code != 403 {
		t.Fatal("YAML bypassed write policy")
	}
	created := call(body, token)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var written metadata.Version
	if json.Unmarshal(created.Body.Bytes(), &written) != nil || written.Status != "draft" {
		t.Fatal("invalid YAML version")
	}
	read := m3Call(h, http.MethodGet, path+"/1", token, 0, nil)
	var stored metadata.Version
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &stored) != nil || stored.SourceHash != written.SourceHash {
		t.Fatal("YAML source failed JSONB read/hash roundtrip")
	}
	for _, bad := range []string{
		strings.Replace(body, "expected_revision: 0", "expected_revision: 1\nexpected_revision: 1", 1),
		body + "---\nexpected_revision: 1\n",
		strings.Replace(body, "label: Vendor", "label: &alias Vendor", 1),
		strings.Replace(body, "label: Vendor", "label: !!python/object Vendor", 1),
	} {
		w := call(bad, token)
		if w.Code != 400 {
			t.Fatal("unsafe YAML accepted", w.Code, w.Body.String())
		}
		var count int
		if err := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=$1 AND action='metadata.write.rejected' AND NOT allowed`, w.Header().Get("X-Request-ID")).Scan(&count); err != nil || count != 1 {
			t.Fatal("YAML rejection not audited")
		}
	}
	if w := call(strings.Replace(body, "expected_revision: 0", "expected_revision: 1", 1), token); w.Code != 201 {
		t.Fatal("YAML rejection advanced head or broke recovery", w.Code)
	}
}
