//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"os"
	"strings"
	"testing"
)

func TestM4CompileIsAuthorizedAuditedAndNeverActivates(t *testing.T) {
	f := newIAM(t)
	h := m3Handler(f, m3Registry(t), audit.Postgres{}, policy.Native{})
	raw, e := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	app := "app.compiler_" + strings.ReplaceAll(uuid(t), "-", "")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	token := f.token(t, f.aliceSubject, nil)
	if w := m3Call(h, "POST", path, token, 0, raw); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, test := range []struct {
		token  string
		status int
	}{{"", 401}, {f.token(t, f.bobSubject, nil), 403}, {f.token(t, f.viewerSubject, nil), 200}} {
		if w := m3Call(h, "GET", path+"/1/compile", test.token, 0, nil); w.Code != test.status {
			t.Fatal(w.Code)
		}
	}
	first := m3Call(h, "GET", path+"/1/compile", token, 0, nil)
	again := m3Call(h, "GET", path+"/1/compile", token, 0, nil)
	if first.Code != 200 || first.Body.String() != again.Body.String() {
		t.Fatal("unstable IR or compile failed")
	}
	var out struct {
		Hash string `json:"ir_hash"`
	}
	_ = json.Unmarshal(first.Body.Bytes(), &out)
	if len(out.Hash) != 64 {
		t.Fatal("missing deterministic IR hash")
	}
	var count int
	if err := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=$1 AND action='metadata.compile' AND allowed`, first.Header().Get("X-Request-ID")).Scan(&count); err != nil || count != 1 {
		t.Fatal("compile not audited")
	}
	if w := m3Call(h, "GET", path+"/1", token, 0, nil); !strings.Contains(w.Body.String(), `"status":"draft"`) {
		t.Fatal("compile activated source")
	}
	broken := []byte(strings.Replace(string(raw), `"to": "state.complete"`, `"to": "state.missing"`, 1))
	if w := m3Call(h, "POST", path, token, 1, broken); w.Code != 201 {
		t.Fatal("structural draft rejected prematurely")
	}
	rejected := m3Call(h, "GET", path+"/2/compile", token, 0, nil)
	if rejected.Code != 422 || strings.Contains(rejected.Body.String(), `"ir":`) {
		t.Fatal("invalid graph produced trusted IR", rejected.Code)
	}
}

func TestM4ApplicationScopedCompilePolicy(t *testing.T) {
	f, _, _, token, app := m5Fixture(t)
	h := m5Handler(t, f, appCompilerPolicy{}, audit.Postgres{}, true)
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions/1/compile"
	if w := m3Call(h, "GET", path, token, 0, nil); w.Code != 403 {
		t.Fatal("application-specific compiler authorization bypass", w.Code)
	}
}

type appCompilerPolicy struct{}

func (appCompilerPolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	d := (policy.Native{}).Decide(ctx, s, a, r, c)
	if a == "metadata.compile" && r.AppID != "" {
		d.Allow = false
	}
	return d
}
