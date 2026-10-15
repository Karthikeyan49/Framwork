//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func m5Handler(t *testing.T, f *iamFixture, p policy.Engine, w audit.Writer, enabled bool) http.Handler {
	a := access.API{DB: f.db, Auth: f.verifier, Policy: p, Audit: w, Registry: m3Registry(t), PreviewEnabled: enabled}
	return httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: a.Register, AllowPOST: a.AcceptsPOST})
}
func m5Call(h http.Handler, path, token string, payload any) *httptest.ResponseRecorder {
	var raw []byte
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	req := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}
func m5Fixture(t *testing.T) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f := newIAM(t)
	h := m5Handler(t, f, policy.Native{}, audit.Postgres{}, true)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	app := "app.data_" + strings.ReplaceAll(uuid(t), "-", "")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	token := f.token(t, f.aliceSubject, nil)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 0, raw); w.Code != 201 {
		t.Fatal(w.Code)
	}
	base := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/versions/1/entities/entity.contact"
	return f, h, base, token, app
}
func m5Values(name string) map[string]any {
	return map[string]any{"field.contact.name": name, "field.contact.status": "active", "field.contact.balance": map[string]any{"amount": "12.30", "currency": "USD"}}
}
func m5Create(t *testing.T, h http.Handler, base, token, name string) data.Record {
	t.Helper()
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: m5Values(name)})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var r data.Record
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Version != 1 || r.ID == "" {
		t.Fatal("record creation contract")
	}
	return r
}
func m5Query() data.Query {
	return data.Query{Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance", "field.contact.parent"}, Limit: 2}
}
func m5Page(t *testing.T, w *httptest.ResponseRecorder) data.Page {
	t.Helper()
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p data.Page
	if json.Unmarshal(w.Body.Bytes(), &p) != nil {
		t.Fatal("page contract")
	}
	return p
}
func TestM5CRUDTypedValuesCASAndVersionIsolation(t *testing.T) {
	f, h, base, token, app := m5Fixture(t)
	first := m5Create(t, h, base, token, "Alpha")
	m5Create(t, h, base, token, "Beta")
	m5Create(t, h, base, token, "Gamma")
	query := m5Query()
	query.Sort = &data.Sort{Field: "field.contact.name"}
	page := m5Page(t, m5Call(h, base+"/query", token, query))
	if len(page.Records) != 2 || page.Next == "" || page.Records[0].Values["field.contact.name"] != "Alpha" {
		t.Fatal("sort/page")
	}
	query.After = page.Next
	last := m5Page(t, m5Call(h, base+"/query", token, query))
	if len(last.Records) != 1 || last.Next != "" {
		t.Fatal("keyset terminal page")
	}
	values := m5Values("Changed")
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: first.ID, ExpectedVersion: 1, Values: values})
	if w.Code != 200 {
		t.Fatal("update", w.Code, w.Body.String())
	}
	if w = m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: first.ID, ExpectedVersion: 1, Values: values}); w.Code != 409 {
		t.Fatal("stale update")
	}
	viewer := f.token(t, f.viewerSubject, nil)
	if w = m5Call(h, base+"/commands", viewer, data.Command{Operation: "delete", ID: first.ID, ExpectedVersion: 2}); w.Code != 403 {
		t.Fatal("viewer mutation")
	}
	if w = m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: first.ID, ExpectedVersion: 2}); w.Code != 200 {
		t.Fatal("delete")
	}
	query = m5Query()
	query.Limit = 100
	page = m5Page(t, m5Call(h, base+"/query", viewer, query))
	if len(page.Records) != 2 {
		t.Fatal("CRUD history")
	}
	if w = m5Call(h, base+"/query", f.token(t, f.bobSubject, nil), query); w.Code != 403 {
		t.Fatal("foreign tenant query")
	}
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	metadataPath := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	if w = m3Call(h, "POST", metadataPath, token, 1, raw); w.Code != 201 {
		t.Fatal("second metadata revision")
	}
	newBase := strings.Replace(base, "/versions/1/", "/versions/2/", 1)
	if page = m5Page(t, m5Call(h, newBase+"/query", token, query)); len(page.Records) != 0 {
		t.Fatal("draft revision data leaked")
	}
	bob := f.token(t, f.bobSubject, nil)
	if w = m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); w.Code != 201 {
		t.Fatal("tenant B draft")
	}
	foreignBase := strings.Replace(base, f.tenantA, f.tenantB, 1)
	if page = m5Page(t, m5Call(h, foreignBase+"/query", bob, query)); len(page.Records) != 0 {
		t.Fatal("dataset tenant collision")
	}
	if w = m5Call(h, foreignBase+"/commands", bob, data.Command{Operation: "update", ID: pageID(first), ExpectedVersion: 1, Values: m5Values("Spoof")}); w.Code != 404 {
		t.Fatal("cross-tenant record update")
	}
}
func pageID(r data.Record) string { return r.ID }

type denyDataPolicy struct{ action, field string }

func (p denyDataPolicy) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}

func (p denyDataPolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	d := (policy.Native{}).Decide(ctx, s, a, r, c)
	if a == p.action && (p.field == "" || r.FieldID == p.field) {
		d.Allow = false
	}
	return d
}

type failCommandAudit struct{}

func (failCommandAudit) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if e.Action == "preview.write" {
		return errors.New("audit fixture unavailable")
	}
	return (audit.Postgres{}).Append(ctx, q, e)
}
func TestM5FieldRowPolicyAndAuditRollback(t *testing.T) {
	f, h, base, token, _ := m5Fixture(t)
	first := m5Create(t, h, base, token, "Alpha")
	query := m5Query()
	query.Projection = []string{"field.contact.id"}
	query.Filter = &data.Filter{Op: "eq", Field: "field.contact.name", Value: json.RawMessage(`"Alpha"`)}
	restricted := m5Handler(t, f, denyDataPolicy{"data.field.read", "field.contact.name"}, audit.Postgres{}, true)
	if w := m5Call(restricted, base+"/query", token, query); w.Code != 403 {
		t.Fatal("hidden filter field bypass")
	}
	restricted = m5Handler(t, f, denyDataPolicy{"data.row.read", ""}, audit.Postgres{}, true)
	if w := m5Call(restricted, base+"/query", token, m5Query()); w.Code != 403 {
		t.Fatal("row read bypass")
	}
	if w := m5Call(restricted, base+"/commands", token, data.Command{Operation: "create", Values: m5Values("Denied")}); w.Code != 403 {
		t.Fatal("write response row read bypass")
	}
	restricted = m5Handler(t, f, denyDataPolicy{"data.field.write", "field.contact.balance"}, audit.Postgres{}, true)
	if w := m5Call(restricted, base+"/commands", token, data.Command{Operation: "delete", ID: first.ID, ExpectedVersion: 1}); w.Code != 403 {
		t.Fatal("delete field write bypass")
	}
	if w := m5Call(m5Handler(t, f, policy.Native{}, failCommandAudit{}, true), base+"/commands", token, data.Command{Operation: "create", Values: m5Values("Rollback")}); w.Code != 503 {
		t.Fatal("audit failure success")
	}
	q := m5Query()
	q.Limit = 100
	if page := m5Page(t, m5Call(h, base+"/query", token, q)); len(page.Records) != 1 {
		t.Fatal("denied/failed command committed record")
	}
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		var n int
		err := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.preview_records`).Scan(&n)
		if err != nil || n != 0 {
			return errors.New("missing context exposed preview records")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w := m5Call(m5Handler(t, f, policy.Native{}, audit.Postgres{}, false), base+"/query", token, q); w.Code != 413 {
		t.Fatal("disabled preview body accepted", w.Code)
	}
	descriptor := strings.Split(base, "/entities/")[0]
	if w := m3Call(m5Handler(t, f, policy.Native{}, audit.Postgres{}, false), "GET", descriptor, token, 0, nil); w.Code != 404 {
		t.Fatal("disabled preview descriptor registered", w.Code)
	}
}
func TestM5QueryBudgetsParameterizationAndAggregate(t *testing.T) {
	_, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "A'; DROP TABLE runtime.preview_records; --")
	m5Create(t, h, base, token, "Beta")
	query := m5Query()
	query.Filter = &data.Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"DROP TABLE"`)}
	page := m5Page(t, m5Call(h, base+"/query", token, query))
	if len(page.Records) != 1 {
		t.Fatal("parameterized literal query")
	}
	query.Filter = nil
	query.Aggregate = &data.Aggregate{Op: "sum", Field: "field.contact.balance"}
	page = m5Page(t, m5Call(h, base+"/query", token, query))
	if page.Aggregate == nil || page.Aggregate.Count != 2 || page.Aggregate.Sum != "24.6" {
		t.Fatal("exact aggregate", page.Aggregate)
	}
	query.Aggregate = nil
	for _, bad := range []data.Query{{Projection: query.Projection, Limit: 101}, {Projection: []string{"field.unknown"}, Limit: 1}, {Projection: query.Projection, Limit: 1, Filter: &data.Filter{Op: "sql", Field: "field.contact.name", Value: json.RawMessage(`"SELECT"`)}}, {Projection: query.Projection, Limit: 1, After: "forged"}} {
		if w := m5Call(h, base+"/query", token, bad); w.Code != 400 {
			t.Fatal("unsafe query accepted", w.Code)
		}
	}
	for _, v := range []map[string]any{{"field.contact.name": "x", "tenant_id": "forged"}, {"field.contact.name": "x", "field.contact.balance": map[string]any{"amount": "1.2", "currency": "EUR"}}, {"field.contact.name": "x", "field.contact.joined": "2026-02-30"}, {"field.contact.name": "x", "field.contact.id": uuid(t)}} {
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}); w.Code != 400 {
			t.Fatal("invalid custom values accepted", w.Code)
		}
	}
	if w := m5Call(h, base+"/query", token, map[string]any{"projection": query.Projection, "limit": 1, "sql": "SELECT"}); w.Code != 400 {
		t.Fatal("raw SQL request key accepted")
	}
}
func TestM5ReferencesAndConcurrentVersions(t *testing.T) {
	_, h, base, token, _ := m5Fixture(t)
	parent := m5Create(t, h, base, token, "Parent")
	v := m5Values("Child")
	v["field.contact.parent"] = parent.ID
	created := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v})
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: parent.ID, ExpectedVersion: 1}); w.Code != 409 {
		t.Fatal("referenced parent deletion")
	}
	v["field.contact.parent"] = uuid(t)
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}); w.Code != 400 {
		t.Fatal("dangling reference")
	}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			statuses <- m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 1, Values: m5Values("Revised")}).Code
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	success := 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status != 409 {
			t.Fatal("unexpected concurrent status", status)
		}
	}
	if success != 1 {
		t.Fatal("lost update")
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 2, Values: m5Values("Recovered")}); w.Code != 200 {
		t.Fatal("concurrent recovery", w.Code, w.Body.String())
	}
}

func TestM5DeclaredJoinAndReferenceDeleteRace(t *testing.T) {
	_, h, base, token, _ := m5Fixture(t)
	parent := m5Create(t, h, base, token, "Parent")
	v := m5Values("Child")
	v["field.contact.parent"] = parent.ID
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	query := m5Query()
	query.Filter = &data.Filter{Op: "eq", Field: "field.contact.name", Value: json.RawMessage(`"Child"`)}
	query.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.name"}}
	page := m5Page(t, m5Call(h, base+"/query", token, query))
	if len(page.Records) != 1 || page.Records[0].Related[query.Join.Relationship] == nil || page.Records[0].Related[query.Join.Relationship].Values["field.contact.name"] != "Parent" {
		t.Fatal("declared relationship join failed")
	}
	candidate := m5Create(t, h, base, token, "Race parent")
	v = m5Values("Race child")
	v["field.contact.parent"] = candidate.ID
	start := make(chan struct{})
	var createStatus, deleteStatus int
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		createStatus = m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}).Code
	}()
	go func() {
		defer wg.Done()
		<-start
		deleteStatus = m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: candidate.ID, ExpectedVersion: 1}).Code
	}()
	close(start)
	wg.Wait()
	if createStatus == 200 && deleteStatus == 200 {
		t.Fatal("reference/delete race broke referential integrity")
	}
	if createStatus != 200 && createStatus != 400 && createStatus != 409 {
		t.Fatal("unexpected reference race status", createStatus)
	}
	if deleteStatus != 200 && deleteStatus != 409 {
		t.Fatal("unexpected delete race status", deleteStatus)
	}
	query = m5Query()
	query.Limit = 100
	page = m5Page(t, m5Call(h, base+"/query", token, query))
	ids := map[string]bool{}
	for _, r := range page.Records {
		ids[r.ID] = true
	}
	for _, r := range page.Records {
		if ref, ok := r.Values["field.contact.parent"].(string); ok && !ids[ref] {
			t.Fatal("dangling reference after race")
		}
	}
}

type contextualDataPolicy struct{ mode string }

func (p contextualDataPolicy) DataScope(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	// V3 adapters express record-conditional field restrictions as SQL scopes,
	// rather than depending on application-memory callbacks after fetching rows.
	if p.mode == "field_at_row" && a == "data.read" && r.FieldID == "field.contact.name" {
		return policy.DataScope{Kind: "NONE"}, policy.ErrScope
	}
	return (policy.Native{}).DataScope(ctx, s, a, r, c)
}

func (p contextualDataPolicy) Decide(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.Context) policy.Decision {
	d := (policy.Native{}).Decide(ctx, s, action, r, c)
	if p.mode == "field_at_row" && action == "data.field.read" && r.FieldID == "field.contact.name" && r.RecordID != "" {
		d.Allow = false
	}
	if p.mode == "owner_write" && action == "data.field.write" && r.OwnerID == s.User.ID() {
		d.Allow = false
	}
	if p.mode == "target_entity" && action == "data.entity.read" && r.EntityID == "entity.company" {
		d.Allow = false
	}
	return d
}
func TestM5RecordSpecificFieldABACAndReferenceEntityPolicy(t *testing.T) {
	f, h, base, token, app := m5Fixture(t)
	r := m5Create(t, h, base, token, "Owned")
	restricted := m5Handler(t, f, contextualDataPolicy{"field_at_row"}, audit.Postgres{}, true)
	query := m5Query()
	if w := m5Call(restricted, base+"/query", token, query); w.Code != 403 {
		t.Fatal("record-specific field projection bypass")
	}
	query.Projection = []string{"field.contact.id"}
	query.Filter = &data.Filter{Op: "eq", Field: "field.contact.name", Value: json.RawMessage(`"Owned"`)}
	if w := m5Call(restricted, base+"/query", token, query); w.Code != 403 {
		t.Fatal("record-specific field filter bypass")
	}
	query.Filter = nil
	query.Sort = &data.Sort{Field: "field.contact.name"}
	if w := m5Call(restricted, base+"/query", token, query); w.Code != 403 {
		t.Fatal("record-specific sort bypass")
	}
	restricted = m5Handler(t, f, contextualDataPolicy{"owner_write"}, audit.Postgres{}, true)
	if w := m5Call(restricted, base+"/commands", token, data.Command{Operation: "update", ID: r.ID, ExpectedVersion: 1, Values: m5Values("Denied")}); w.Code != 403 {
		t.Fatal("write omitted trusted owner attributes")
	}
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	var doc metadata.Document
	_ = json.Unmarshal(raw, &doc)
	doc.AppID = app
	for i := range doc.Definitions {
		if field := doc.Definitions[i].Field; field != nil && field.Type == "reference" {
			field.ReferenceTo = "entity.company"
		}
		if rel := doc.Definitions[i].Relationship; rel != nil {
			rel.ToEntity = "entity.company"
			rel.ToField = "field.company.id"
		}
	}
	common := func(id, label, kind string) metadata.Definition {
		return metadata.Definition{ID: id, Label: label, Kind: kind, SchemaVersion: metadata.SchemaVersion, Namespace: doc.Namespace}
	}
	entity := common("entity.company", "Company", "entity")
	entity.Entity = &metadata.Entity{Fields: []string{"field.company.id", "field.company.name"}}
	id := common("field.company.id", "Identifier", "field")
	id.Field = &metadata.Field{EntityRef: "entity.company", Type: "string", Required: true}
	name := common("field.company.name", "Name", "field")
	name.Field = &metadata.Field{EntityRef: "entity.company", Type: "string", Required: true}
	doc.Definitions = append(doc.Definitions, entity, id, name)
	raw, _ = json.Marshal(doc)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw); w.Code != 201 {
		t.Fatal("reference policy fixture")
	}
	companyBase := strings.Replace(strings.Replace(base, "/versions/1/", "/versions/2/", 1), "entity.contact", "entity.company", 1)
	created := m5Call(h, companyBase+"/commands", token, data.Command{Operation: "create", Values: map[string]any{"field.company.name": "Secret company"}})
	if created.Code != 200 {
		t.Fatal(created.Code, created.Body.String())
	}
	var company data.Record
	_ = json.Unmarshal(created.Body.Bytes(), &company)
	values := m5Values("Contact")
	values["field.contact.parent"] = company.ID
	contactBase := strings.Replace(base, "/versions/1/", "/versions/2/", 1)
	restricted = m5Handler(t, f, contextualDataPolicy{"target_entity"}, audit.Postgres{}, true)
	if w := m5Call(restricted, contactBase+"/commands", token, data.Command{Operation: "create", Values: values}); w.Code != 403 {
		t.Fatal("target entity authorization bypass", w.Code, w.Body.String())
	}
}

func TestM5UnfilteredVisibilityAppPolicyAndAggregateProjection(t *testing.T) {
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "Secret")
	m5Create(t, h, base, token, "Other")
	restricted := m5Handler(t, f, contextualDataPolicy{"field_at_row"}, audit.Postgres{}, true)
	for _, value := range []string{`"Secret"`, `"No match"`} {
		query := m5Query()
		query.Projection = []string{"field.contact.id"}
		query.Filter = &data.Filter{Op: "eq", Field: "field.contact.name", Value: json.RawMessage(value)}
		if w := m5Call(restricted, base+"/query", token, query); w.Code != 403 {
			t.Fatal("filter visibility oracle", w.Code)
		}
	}
	restricted = m5Handler(t, f, denyDataPolicy{"data.row.read", ""}, audit.Postgres{}, true)
	query := m5Query()
	query.Limit = 1
	query.Aggregate = &data.Aggregate{Op: "count"}
	if w := m5Call(restricted, base+"/query", token, query); w.Code != 403 {
		t.Fatal("aggregate budget preceded policy", w.Code)
	}
	query = m5Query()
	query.Projection = []string{"field.contact.id"}
	query.Filter = &data.Filter{Op: "eq", Field: "field.contact.balance", Value: json.RawMessage(`{"amount":"12.3","currency":"USD"}`)}
	query.Aggregate = &data.Aggregate{Op: "sum", Field: "field.contact.balance"}
	if w := m5Call(h, base+"/query", token, query); w.Code != 400 {
		t.Fatal("aggregate silently summed absent projection", w.Code)
	}
	restricted = m5Handler(t, f, appPreviewPolicy{}, audit.Postgres{}, true)
	if w := m3Call(restricted, "GET", strings.Split(base, "/entities/")[0], token, 0, nil); w.Code != 403 {
		t.Fatal("app policy bypass", w.Code)
	}
	values := m5Values("Bad date")
	values["field.contact.joined"] = "0000-01-01"
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values}); w.Code != 400 {
		t.Fatal("year zero accepted", w.Code)
	}
	values["field.contact.joined"] = "0001-01-01"
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values}); w.Code != 200 {
		t.Fatal("valid date rejected", w.Code)
	}
	query = m5Query()
	query.Sort = &data.Sort{Field: "field.contact.joined"}
	m5Page(t, m5Call(h, base+"/query", token, query))
}

type appPreviewPolicy struct{}

func (appPreviewPolicy) Decide(ctx context.Context, s policy.Subject, a string, r policy.Resource, c policy.Context) policy.Decision {
	d := (policy.Native{}).Decide(ctx, s, a, r, c)
	if strings.HasPrefix(a, "preview.") && r.AppID != "" {
		d.Allow = false
	}
	return d
}
