//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Independent R5d review: use the verified effective-target disposable DB helper,
// real live role loader and populated records. No production fixtures are used.
func reviewR5dWide(t *testing.T, f *iamFixture, h http.Handler, app, token, base string) (string, []string) {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc metadata.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("wide metadata fixture")
	}
	doc.AppID = app
	ids := []string{}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("field.contact.extra_%d", i)
		ids = append(ids, id)
		doc.Definitions[0].Entity.Fields = append(doc.Definitions[0].Entity.Fields, id)
		doc.Definitions = append(doc.Definitions, metadata.Definition{ID: id, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: fmt.Sprintf("Extra %d", i), Kind: "field", Field: &metadata.Field{EntityRef: "entity.contact", Type: "string"}})
	}
	raw, _ = json.Marshal(doc)
	path := "/v1/tenants/" + f.tenantA + "/metadata/" + app + "/versions"
	if w := m3Call(h, "POST", path, token, 1, raw); w.Code != 201 {
		t.Fatal("wide metadata revision", w.Code)
	}
	return strings.Replace(base, "/versions/1/", "/versions/2/", 1), ids
}
func reviewR5dRecord(t *testing.T, h http.Handler, base, token string, values map[string]any) data.Record {
	t.Helper()
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values})
	var row data.Record
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &row) != nil {
		t.Fatal("owned populated record", w.Code)
	}
	return row
}
func TestIndependentR5dWideReadableAndHiddenAdmission(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	base, extra := reviewR5dWide(t, f, owner, app, alice, base)
	small := m5Values("Private small")
	large := m5Values("Private large")
	for _, id := range extra {
		small[id] = "x"
		large[id] = strings.Repeat("q", 1500)
	}
	a := reviewR5dRecord(t, owner, base, alice, small)
	b := reviewR5dRecord(t, owner, base, alice, large)
	// Publicly readable wide schemas must be editable without reserving their
	// maximum possible sizes; only actual known values count.
	for _, row := range []data.Record{a, b} {
		w := m5Call(owner, base+"/commands", alice, data.Command{Key: uuid(t), Operation: "patch", ID: row.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": "inactive"}})
		if w.Code != 200 {
			t.Fatal("fully readable wide patch", w.Code)
		}
	}
	h := r5Handler(t, f, audit.Postgres{})
	d := r5dRole(app)
	for _, id := range extra {
		d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: id}})
	}
	root := "/v1/tenants/" + f.tenantA
	r5Publish(t, h, root+"/roles", alice, 0, d, true, uuid(t))
	r5Assign(t, h, root, alice, f.viewer, d.Name, 200)
	user := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}})
	projection := append([]string{"field.contact.id", "field.contact.name", "field.contact.balance", "field.contact.status"}, extra...)
	for _, row := range []data.Record{a, b} {
		page := m5Page(t, m5Call(h, base+"/query", user, data.Query{RecordID: row.ID, Projection: projection, Limit: 1}))
		if len(page.Records) != 1 || len(page.Records[0].FieldAccess.Writable) != 0 || len(page.Records[0].FieldAccess.Masked) != 22 {
			t.Fatal("large hidden schema hints")
		}
		key := uuid(t)
		w := m5Call(h, base+"/commands", user, data.Command{Key: key, Operation: "patch", ID: row.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.status": "active"}})
		if w.Code != 400 {
			t.Fatal("hidden-size dependent admission", w.Code)
		}
		if receipt := m9rReceipt(t, h, m9rReceiptPath(f, app, key), user); receipt.State != "UNKNOWN" {
			t.Fatal("denied budget committed receipt")
		}
	}
	m9rCommandCounts(t, f, app, 2, 2, 4)
}

func TestIndependentR5dPredicateCoverageAndHiddenJoinKey(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	if err := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Review", f.bob, "member", true); err != nil {
		t.Fatal(err)
	}
	bob := f.token(t, f.bobSubject, nil)
	own := m5Create(t, owner, base, bob, "Owned visible")
	m5Create(t, owner, base, alice, "Other private")
	childValues := m5Values("Owned child")
	childValues["field.contact.parent"] = own.ID
	child := reviewR5dRecord(t, owner, base, bob, childValues)
	h := r5Handler(t, f, audit.Postgres{})
	d := r5Definition("review_owner", app, policy.DataScope{Kind: "OWNER"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.balance"}})
	root := "/v1/tenants/" + f.tenantA
	v := r5Publish(t, h, root+"/roles", alice, 0, d, true, uuid(t))
	r5Assign(t, h, root, alice, f.bob, d.Name, 200)
	q := data.Query{Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance"}, Limit: 100, Filter: &data.Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"Owned"`)}, Sort: &data.Sort{Field: "field.contact.name"}}
	p := m5Page(t, m5Call(h, base+"/query", bob, q))
	if len(p.Records) != 2 {
		t.Fatal("identical entity/field scope rejected or leaked rows")
	}
	for _, r := range p.Records {
		if !slices.Contains(r.FieldAccess.Masked, "field.contact.balance") {
			t.Fatal("unrelated hidden projection")
		}
	}
	// Reuse an authorized cursor after a live field revocation. A stale cursor
	// must not authorize the sort predicate or expose a now-hidden relation key.
	q.Limit = 1
	p = m5Page(t, m5Call(h, base+"/query", bob, q))
	if p.Next == "" {
		t.Fatal("missing populated cursor")
	}
	q.After = p.Next
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.name"}}, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.parent"}})
	r5Publish(t, h, root+"/roles", alice, v.Version, d, true, uuid(t))
	if w := m5Call(h, base+"/query", bob, q); w.Code != 403 {
		t.Fatal("revoked cursor predicate", w.Code)
	}
	q = data.Query{RecordID: child.ID, Projection: []string{"field.contact.id", "field.contact.parent"}, Join: &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id"}}, Limit: 1}
	if w := m5Call(h, base+"/query", bob, q); w.Code != 403 {
		t.Fatal("hidden source relationship key disclosed target", w.Code)
	}
	q.Join = nil
	p = m5Page(t, m5Call(h, base+"/query", bob, q))
	if len(p.Records) != 1 || !slices.Contains(p.Records[0].FieldAccess.Masked, "field.contact.parent") {
		t.Fatal("masked source projection dropped visible row")
	}
	if _, ok := p.Records[0].Values["field.contact.parent"]; ok {
		t.Fatal("hidden relation key escaped")
	}
	foreign := strings.Replace(base, f.tenantA, f.tenantB, 1)
	if w := m5Call(h, foreign+"/query", bob, q); w.Code != 404 {
		t.Fatal("foreign tenant metadata lookup", w.Code)
	}
}

func TestIndependentR5dConcurrentPatchCASAndPrivatePreservation(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	record := m5Create(t, owner, base, alice, "Private concurrently retained")
	h, user, _, _ := r5dLimited(t, f, app, alice)
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, values := range []map[string]any{{"field.contact.status": "inactive"}, {"field.contact.joined": "2026-10-15"}} {
		c := data.Command{Key: uuid(t), Operation: "patch", ID: record.ID, ExpectedVersion: 1, Values: values}
		wg.Add(1)
		go func() { defer wg.Done(); <-start; codes <- m5Call(h, base+"/commands", user, c).Code }()
	}
	close(start)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for code := range codes {
		counts[code]++
	}
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal("concurrent CAS outcome", counts)
	}
	page := m5Page(t, m5Call(owner, base+"/query", alice, data.Query{RecordID: record.ID, Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance", "field.contact.status", "field.contact.joined"}, Limit: 1}))
	row := page.Records[0]
	if row.Version != 2 || row.Values["field.contact.name"] != "Private concurrently retained" || row.Values["field.contact.balance"].(map[string]any)["amount"] != "12.3" {
		t.Fatal("concurrent patch lost private state")
	}
	m9rCommandCounts(t, f, app, 1, 1, 2)
}

func TestIndependentR5dPatchPreservesTrustedFieldOwnerABAC(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m5Fixture(t)
	record := m5Create(t, owner, base, alice, "Trusted owner protected")
	restricted := m5Handler(t, f, contextualDataPolicy{mode: "owner_write"}, audit.Postgres{}, true)
	full := data.Command{Operation: "update", ID: record.ID, ExpectedVersion: 1, Values: m5Values("Forbidden full update")}
	if w := m5Call(restricted, base+"/commands", alice, full); w.Code != 403 {
		t.Fatal("existing full update owner fence", w.Code)
	}
	partial := data.Command{Key: uuid(t), Operation: "patch", ID: record.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.name": "Forbidden partial update"}}
	if w := m5Call(restricted, base+"/commands", alice, partial); w.Code != 403 {
		t.Fatal("patch omitted trusted field owner attributes", w.Code)
	}
}

type independentR5dOwnerFieldPolicy struct{ onlyOwner bool }

func (p independentR5dOwnerFieldPolicy) Decide(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.Context) policy.Decision {
	decision := (policy.Native{}).Decide(ctx, s, action, r, c)
	if (action == "data.field.read" || action == "data.field.write") && r.FieldID == "field.contact.name" {
		owns := r.OwnerID == s.User.ID()
		if p.onlyOwner {
			decision.Allow = decision.Allow && owns
		} else if owns {
			decision.Allow = false
		}
	}
	return decision
}
func (p independentR5dOwnerFieldPolicy) DataScope(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	return (policy.Native{}).DataScope(ctx, s, action, r, c)
}
func TestIndependentR5dPatchAllowsTrustedOwnerFieldPolicy(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m5Fixture(t)
	record := m5Create(t, owner, base, alice, "Owner editable")
	restricted := m5Handler(t, f, independentR5dOwnerFieldPolicy{onlyOwner: true}, audit.Postgres{}, true)
	command := data.Command{Operation: "patch", ID: record.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.name": "Owner changed"}}
	if w := m5Call(restricted, base+"/commands", alice, command); w.Code != 200 {
		t.Fatal("owner-allow field policy lacks trusted attributes", w.Code)
	}
	page := m5Page(t, m5Call(owner, base+"/query", alice, data.Query{RecordID: record.ID, Projection: []string{"field.contact.name"}, Limit: 1}))
	if page.Records[0].Values["field.contact.name"] != "Owner changed" {
		t.Fatal("owned patch not persisted")
	}
}
func TestIndependentR5dVisibleProjectionHonorsTrustedFieldOwnerABAC(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m5Fixture(t)
	record := m5Create(t, owner, base, alice, "Owner restricted field")
	restricted := m5Handler(t, f, independentR5dOwnerFieldPolicy{}, audit.Postgres{}, true)
	page := m5Page(t, m5Call(restricted, base+"/query", alice, data.Query{RecordID: record.ID, Projection: []string{"field.contact.id", "field.contact.name"}, Limit: 1}))
	if len(page.Records) != 1 {
		t.Fatal("entity-visible row disappeared")
	}
	if page.Records[0].FieldAccess == nil || !slices.Contains(page.Records[0].FieldAccess.Masked, "field.contact.name") {
		t.Fatal("SQL mask omitted trusted owner denial")
	}
	if _, present := page.Records[0].Values["field.contact.name"]; present {
		t.Fatal("trusted owner denied field crossed the visible port")
	}
}

func TestIndependentR5dPatchNumericProjectionAndRollback(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	record := m5Create(t, owner, base, alice, "Private indexed field")
	m9rIndexReady(t, owner, base+"/indexes/field.contact.balance", alice)
	command := data.Command{Key: uuid(t), Operation: "patch", ID: record.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.balance": map[string]any{"amount": "17.25", "currency": "USD"}}}
	if w := m5Call(owner, base+"/commands", alice, command); w.Code != 200 {
		t.Fatal("numeric patch", w.Code)
	}
	h, user, _, _ := r5dLimited(t, f, app, alice)
	command = data.Command{Key: uuid(t), Operation: "patch", ID: record.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.status": "inactive"}}
	if w := m5Call(h, base+"/commands", user, command); w.Code != 200 {
		t.Fatal("masked indexed field preservation", w.Code)
	}
	var version int64
	var value string
	if err := f.operator.QueryRow(`SELECT source_version,value_number::text FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID).Scan(&version, &value); err != nil || version != 3 || value != "17.25" {
		t.Fatal("projection/source version changed independently", version, err)
	}
	// Fault after real audit insertion must roll back row, receipt, numeric
	// projection and outbox together; a new request can reconcile as UNKNOWN.
	command.Key = uuid(t)
	command.ExpectedVersion = 3
	command.Values = map[string]any{"field.contact.status": "active"}
	failed := r5Handler(t, f, m8AfterAuditFailure{})
	if w := m5Call(failed, base+"/commands", user, command); w.Code != 503 {
		t.Fatal("after-insert audit fault", w.Code)
	}
	if receipt := m9rReceipt(t, h, m9rReceiptPath(f, app, command.Key), user); receipt.State != "UNKNOWN" {
		t.Fatal("failed numeric patch retained receipt")
	}
	if err := f.operator.QueryRow(`SELECT source_version,value_number::text FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID).Scan(&version, &value); err != nil || version != 3 || value != "17.25" {
		t.Fatal("numeric projection survived rolled-back patch", version, err)
	}
	m9rCommandCounts(t, f, app, 1, 2, 3)
}

func TestIndependentR5dVisibleListHonorsTrustedFieldOwnerABAC(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m5Fixture(t)
	own := m5Create(t, owner, base, alice, "Owner restricted list field")
	if err := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Owned review", f.bob, "member", true); err != nil {
		t.Fatal(err)
	}
	bob := f.token(t, f.bobSubject, nil)
	other := m5Create(t, owner, base, bob, "Other readable field")
	restricted := m5Handler(t, f, independentR5dOwnerFieldPolicy{}, audit.Postgres{}, true)
	page := m5Page(t, m5Call(restricted, base+"/query", alice, data.Query{Projection: []string{"field.contact.id", "field.contact.name"}, Limit: 100}))
	if len(page.Records) != 2 {
		t.Fatal("field action masks changed visible list")
	}
	for _, row := range page.Records {
		masked := row.FieldAccess != nil && slices.Contains(row.FieldAccess.Masked, "field.contact.name")
		if row.ID == own.ID {
			if !masked {
				t.Fatal("list omitted trusted owner denial")
			}
			if _, present := row.Values["field.contact.name"]; present {
				t.Fatal("owned hidden list value escaped")
			}
		} else if row.ID == other.ID {
			if masked || row.Values["field.contact.name"] != "Other readable field" {
				t.Fatal("other visible field incorrectly masked")
			}
		} else {
			t.Fatal("unexpected list identity")
		}
	}
}
func TestIndependentR5dVisiblePredicateHonorsTrustedFieldOwnerABAC(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m5Fixture(t)
	m5Create(t, owner, base, alice, "Owner protected predicate")
	restricted := m5Handler(t, f, independentR5dOwnerFieldPolicy{}, audit.Postgres{}, true)
	query := data.Query{Projection: []string{"field.contact.id"}, Limit: 100, Filter: &data.Filter{Op: "eq", Field: "field.contact.name", Value: json.RawMessage(`"Owner protected predicate"`)}}
	if w := m5Call(restricted, base+"/query", alice, query); w.Code != 403 {
		t.Fatal("contextless field action authorized hidden filter", w.Code)
	}
	query.Filter = nil
	query.Sort = &data.Sort{Field: "field.contact.name"}
	if w := m5Call(restricted, base+"/query", alice, query); w.Code != 403 {
		t.Fatal("contextless field action authorized hidden sort", w.Code)
	}
}

// Embedding a concrete optimized kernel must not inherit its proof when a
// wrapper overrides field actions with trusted-owner-dependent decisions.
type independentR5dEmbeddedOwnerPolicy struct {
	policy.Native
	field string
}

func (p independentR5dEmbeddedOwnerPolicy) Decide(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.Context) policy.Decision {
	d := p.Native.Decide(ctx, s, action, r, c)
	if action == "data.field.read" && r.FieldID == p.field && r.OwnerID == s.User.ID() {
		d.Allow = false
	}
	return d
}
func TestIndependentR5dStrictRuleCannotInferOwnerDeniedField(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m7Fixture(t)
	record := m7Record(t, owner, base, alice, "501")
	engine := independentR5dEmbeddedOwnerPolicy{field: "field.contact.balance"}
	restricted := m5Handler(t, f, engine, audit.Postgres{}, true)
	page := m5Page(t, m5Call(restricted, base+"/entities/entity.contact/query", alice, data.Query{RecordID: record.ID, Projection: []string{"field.contact.balance"}, Limit: 1}))
	if len(page.Records) != 1 || page.Records[0].FieldAccess == nil || !slices.Contains(page.Records[0].FieldAccess.Masked, engine.field) {
		t.Fatal("embedded engine inherited SQL action proof")
	}
	w := m5Call(restricted, base+"/rules/rule.contact.approval_required/evaluate", alice, map[string]string{"record_id": record.ID})
	if w.Code != 403 || strings.Contains(w.Body.String(), `"result"`) {
		t.Fatal("strict rule inferred an owner-denied amount", w.Code)
	}
}
func TestIndependentR5dStrictWorkflowCannotCaptureOwnerDeniedField(t *testing.T) {
	m := m9Setup(t, nil)
	// Status is consumed by the executable workflow transition. Name is only
	// referenced by an unrelated rule and is deliberately tested below as an
	// authorized positive rather than an alleged capture bypass.
	engine := independentR5dEmbeddedOwnerPolicy{field: "field.contact.status"}
	api := access.API{DB: m.f.db, Auth: m.f.verifier, Policy: engine, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	restricted := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	w := m5Call(restricted, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "independent-owned-source-denied"})
	var count int
	if err := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.workflow_bindings WHERE tenant_id=$1 AND app_id=$2`, m.f.tenantA, m.app).Scan(&count); err != nil {
		t.Fatal("capture evidence unavailable", err)
	}
	if w.Code != 403 || count != 0 {
		t.Fatal("strict workflow captured owner-denied source", w.Code, "persisted bindings", count)
	}
}

func TestIndependentR5dStrictWorkflowExcludesUnconsumedHiddenField(t *testing.T) {
	m := m9Setup(t, nil)
	engine := independentR5dEmbeddedOwnerPolicy{field: "field.contact.name"}
	api := access.API{DB: m.f.db, Auth: m.f.verifier, Policy: engine, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, WorkflowsEnabled: true}
	restricted := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
	w := m5Call(restricted, m.base+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "independent-unconsumed-name"})
	if w.Code != 200 {
		t.Fatal("unconsumed hidden field unnecessarily blocked workflow", w.Code)
	}
	var inputs []byte
	if err := m.f.operator.QueryRow(`SELECT inputs FROM runtime.workflow_bindings WHERE tenant_id=$1 AND app_id=$2`, m.f.tenantA, m.app).Scan(&inputs); err != nil {
		t.Fatal("snapshot evidence unavailable", err)
	}
	var snapshot struct {
		Fields []string                   `json:"fields"`
		Values map[string]json.RawMessage `json:"values"`
	}
	if json.Unmarshal(inputs, &snapshot) != nil || !slices.Contains(snapshot.Fields, "field.contact.status") || slices.Contains(snapshot.Fields, engine.field) {
		t.Fatal("workflow did not capture precisely consumed fields")
	}
	if _, present := snapshot.Values[engine.field]; present {
		t.Fatal("unconsumed hidden value entered snapshot")
	}
}

func reviewR5dScope(t *testing.T, app string) data.Scope {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := m3Registry(t).Parse([]byte(strings.Replace(string(raw), "app.contacts", app, 1)))
	if err != nil {
		t.Fatal("scope authoring fixture", err)
	}
	program, diagnostics := compiler.Compile(source, compiler.PlatformCatalog())
	if !program.Valid() {
		t.Fatal("scope compiler fixture", diagnostics)
	}
	scope, err := data.NewScope(program, 1)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
func TestIndependentR5dJoinedTargetOwnerDenialStaysInSQL(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	parent := m5Create(t, owner, base, alice, "Joined hidden parent")
	if err := (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Joined review", f.bob, "member", true); err != nil {
		t.Fatal(err)
	}
	bob := f.token(t, f.bobSubject, nil)
	values := m5Values("Readable child")
	values["field.contact.parent"] = parent.ID
	child := reviewR5dRecord(t, owner, base, bob, values)
	engine := independentR5dEmbeddedOwnerPolicy{field: "field.contact.name"}
	restricted := m5Handler(t, f, engine, audit.Postgres{}, true)
	query := data.Query{RecordID: child.ID, Projection: []string{"field.contact.name", "field.contact.parent"}, Join: &data.Join{Relationship: "relationship.contact.parent", Projection: []string{engine.field}}, Limit: 1}
	w := m5Call(restricted, base+"/query", alice, query)
	page := m5Page(t, w)
	related := page.Records[0].Related[query.Join.Relationship]
	if related == nil || related.ID != parent.ID || related.FieldAccess == nil || !slices.Contains(related.FieldAccess.Masked, engine.field) {
		t.Fatal("joined owner denial lost target visibility/mask")
	}
	if strings.Contains(w.Body.String(), "Joined hidden parent") {
		t.Fatal("joined owner-denied value escaped SQL mask")
	}
	if page.Records[0].Values[engine.field] != "Readable child" {
		t.Fatal("source owner field incorrectly masked")
	}
	// Strict internal point joins must reject inaccessible inputs rather than
	// returning a masked value; the same target owner context is authoritative.
	scope := reviewR5dScope(t, app)
	principal, err := f.verifier.Verify(alice)
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		authority, err := m3Scope(f, ctx, q, principal, f.tenantA)
		if err != nil {
			return err
		}
		guard := data.Guard(func(_ context.Context, action string, r data.Resource) error {
			if action == "data.field.read" && r.FieldID == engine.field && r.OwnerID == f.alice {
				return data.ErrDenied
			}
			return nil
		})
		resolve := data.ScopeResolver(func(context.Context, string, data.Resource) (policy.DataScope, error) {
			return policy.DataScope{Kind: "ALL"}, nil
		})
		_, err = (data.Store{}).Query(ctx, q, scope, authority, "entity.contact", query, guard, resolve)
		if !errors.Is(err, data.ErrDenied) {
			return fmt.Errorf("strict joined target not denied: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIndependentR5dOpaqueBoundCannotBeShrunkAndNativeBudgetsRemain(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	first := m5Create(t, owner, base, alice, "Bounded first")
	m9rNumericSeed(t, f, app, data.MaxOpaqueActionRows+8)
	query := data.Query{Projection: []string{"field.contact.id", "field.contact.name", "field.contact.status"}, Limit: 100}
	native := m5Page(t, m5Call(owner, base+"/query", alice, query))
	if len(native.Records) != data.MaxOpaqueActionRows+9 {
		t.Fatal("native query inherited opaque budget")
	}
	defined, user, _, _ := r5dLimited(t, f, app, alice)
	p := m5Page(t, m5Call(defined, base+"/query", user, query))
	if len(p.Records) != len(native.Records) {
		t.Fatal("tenant-defined native kernel inherited opaque budget")
	}
	for _, row := range p.Records {
		if row.FieldAccess == nil || !slices.Contains(row.FieldAccess.Masked, "field.contact.name") {
			t.Fatal("optimized kernel bypassed explicit deny")
		}
	}
	opaque := m5Handler(t, f, independentR5dEmbeddedOwnerPolicy{field: "field.contact.name"}, audit.Postgres{}, true)
	query.Limit = 1
	query.Filter = &data.Filter{Op: "eq", Field: "field.contact.status", Value: json.RawMessage(`"active"`)}
	if w := m5Call(opaque, base+"/query", alice, query); w.Code != 400 {
		t.Fatal("filter/limit shrank opaque universe", w.Code)
	}
	query.RecordID = first.ID
	if p = m5Page(t, m5Call(opaque, base+"/query", alice, query)); len(p.Records) != 1 || p.Records[0].ID != first.ID {
		t.Fatal("exact-point opaque universe unnecessarily blocked")
	}
}

type independentR5dExecutorWrapper struct{ postgres.Executor }

func TestIndependentR5dOpaqueSnapshotProofRejectsAutocommitAndRawRR(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, app := m5Fixture(t)
	record := m5Create(t, owner, base, alice, "Snapshot proof fixture")
	scope := reviewR5dScope(t, app)
	principal, err := f.verifier.Verify(alice)
	if err != nil {
		t.Fatal(err)
	}
	guard := data.Guard(func(context.Context, string, data.Resource) error { return nil })
	resolve := data.ScopeResolver(func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	})
	query := data.Query{RecordID: record.ID, Projection: []string{"field.contact.name"}, Limit: 1}
	var authority tenant.Access
	if err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		var er error
		authority, er = m3Scope(f, ctx, q, principal, f.tenantA)
		if er != nil {
			return er
		}
		if !postgres.RuntimeSnapshot(q) {
			return errors.New("actual runtime port not certified")
		}
		page, er := (data.Store{}).QueryVisible(ctx, q, scope, authority, "entity.contact", query, guard, resolve)
		if er != nil {
			return er
		}
		if len(page.Records) != 1 {
			return errors.New("actual runtime snapshot failed populated positive")
		}
		wrapped := independentR5dExecutorWrapper{q}
		if postgres.RuntimeSnapshot(wrapped) {
			return errors.New("unknown wrapper inherited proof")
		}
		if _, er = (data.Store{}).QueryVisible(ctx, wrapped, scope, authority, "entity.contact", query, guard, resolve); !errors.Is(er, data.ErrDenied) {
			return fmt.Errorf("unknown wrapper not rejected: %v", er)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	reject := func(name string, q postgres.Executor) {
		t.Helper()
		if postgres.RuntimeSnapshot(q) {
			t.Fatal(name, "falsely certified")
		}
		if _, er := (data.Store{}).QueryVisible(context.Background(), q, scope, authority, "entity.contact", query, guard, resolve); !errors.Is(er, data.ErrDenied) {
			t.Fatal(name, "opaque visible executor accepted", er)
		}
		if _, er := (data.Store{}).Query(context.Background(), q, scope, authority, "entity.contact", query, guard, resolve); !errors.Is(er, data.ErrDenied) {
			t.Fatal(name, "strict point executor accepted", er)
		}
	}
	reject("autocommit pool", f.db)
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(context.Background(), `SET default_transaction_isolation='repeatable read'`); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	var isolation string
	if err = conn.QueryRowContext(context.Background(), `SELECT current_setting('transaction_isolation')`).Scan(&isolation); err != nil || isolation != "repeatable read" {
		conn.Close()
		t.Fatal("autocommit RR fixture", err)
	}
	reject("autocommit RR connection", conn)
	if _, err = conn.ExecContext(context.Background(), `SET default_transaction_isolation TO DEFAULT`); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	conn.Close()
	tx, err := f.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(context.Background(), `SELECT current_setting('transaction_isolation')`).Scan(&isolation); err != nil || isolation != "repeatable read" {
		t.Fatal("raw RR fixture", err)
	}
	reject("raw RR transaction", tx)
}
