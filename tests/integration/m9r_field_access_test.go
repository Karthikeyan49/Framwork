//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func r5dRole(app string) policy.Definition {
	d := r5Definition("limited_editor", app, policy.DataScope{Kind: "ALL"})
	all := policy.DataScope{Kind: "ALL"}
	d.Grants = append(d.Grants,
		policy.Grant{Effect: "ALLOW", Actions: []string{"preview.write", "commands.reconcile"}, Resource: policy.Selector{AppID: app, EntityID: "*", FieldID: "*"}},
		policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.write", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}, Scope: &all},
		policy.Grant{Effect: "ALLOW", Actions: []string{"data.row.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}},
	)
	for _, id := range []string{"field.contact.name", "field.contact.balance"} {
		d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: id}})
	}
	return d
}
func r5dLimited(t *testing.T, f *iamFixture, app, alice string) (http.Handler, string, string, policy.RoleVersion) {
	t.Helper()
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	v := r5Publish(t, h, root+"/roles", alice, 0, r5dRole(app), true, uuid(t))
	r5Assign(t, h, root, alice, f.viewer, "limited_editor", 200)
	return h, f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}}), root, v
}
func r5dMasked(t *testing.T, r data.Record) {
	t.Helper()
	if r.FieldAccess == nil || !slices.Contains(r.FieldAccess.Masked, "field.contact.name") || !slices.Contains(r.FieldAccess.Masked, "field.contact.balance") {
		t.Fatal("missing mask")
	}
	for _, id := range r.FieldAccess.Masked {
		if _, ok := r.Values[id]; ok {
			t.Fatal("masked value crossed port")
		}
	}
}
func TestM9RFieldMasksInferenceJoinAndRevocation(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	parent := m5Create(t, legacy, base, alice, "PRIVATE parent value")
	values := m5Values(strings.Repeat("PRIVATE child value", 100))
	values["field.contact.parent"] = parent.ID
	w := m5Call(legacy, base+"/commands", alice, data.Command{Operation: "create", Values: values})
	if w.Code != 200 {
		t.Fatal("populated child", w.Code)
	}
	var child data.Record
	if json.Unmarshal(w.Body.Bytes(), &child) != nil {
		t.Fatal("child")
	}
	h, user, root, v := r5dLimited(t, f, app, alice)
	q := data.Query{Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance", "field.contact.status", "field.contact.parent"}, Limit: 1}
	ids := map[string]bool{}
	for i := 0; i < 3; i++ {
		w = m5Call(h, base+"/query", user, q)
		p := m5Page(t, w)
		if strings.Contains(w.Body.String(), "PRIVATE") || strings.Contains(w.Body.String(), "12.3") {
			t.Fatal("projection exposed secret")
		}
		for _, r := range p.Records {
			r5dMasked(t, r)
			ids[r.ID] = true
			if !slices.Contains(r.FieldAccess.Writable, "field.contact.status") {
				t.Fatal("writable public field")
			}
		}
		if p.Next == "" {
			break
		}
		q.After = p.Next
	}
	if len(ids) != 2 {
		t.Fatal("mask changed pagination")
	}
	q.After = ""
	q.Limit = 100
	q.Aggregate = &data.Aggregate{Op: "count"}
	if p := m5Page(t, m5Call(h, base+"/query", user, q)); p.Aggregate == nil || p.Aggregate.Count != 2 {
		t.Fatal("projection leaked through count")
	}
	q.Aggregate = nil
	q.RecordID = child.ID
	q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance"}}
	p := m5Page(t, m5Call(h, base+"/query", user, q))
	if len(p.Records) != 1 || p.Records[0].Related[q.Join.Relationship] == nil {
		t.Fatal("masked join removed visible target")
	}
	r5dMasked(t, *p.Records[0].Related[q.Join.Relationship])
	q.Join = nil
	q.RecordID = ""
	probes := []data.Query{q, q, q, q}
	probes[0].Filter = &data.Filter{Op: "eq", Field: "field.contact.name", Value: json.RawMessage(`"PRIVATE parent value"`)}
	probes[1].Sort = &data.Sort{Field: "field.contact.name"}
	probes[2].Aggregate = &data.Aggregate{Op: "sum", Field: "field.contact.balance"}
	probes[3].Filter = &data.Filter{Op: "is_null", Field: "field.contact.balance"}
	for _, probe := range probes {
		if w = m5Call(h, base+"/query", user, probe); w.Code != 403 {
			t.Fatal("inference allowed", w.Code)
		}
	}
	// Projected keys do not disclose whether hidden optional data is absent/null.
	for _, value := range []any{nil, map[string]any{"amount": "999999999.99", "currency": "USD"}} {
		values = m5Values("DIFFERENT hidden value")
		values["field.contact.balance"] = value
		if w = m5Call(legacy, base+"/commands", alice, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: parent.Version, Values: values}); w.Code != 200 {
			t.Fatal("variant fixture", w.Code)
		}
		parent.Version++
		q.RecordID = parent.ID
		p = m5Page(t, m5Call(h, base+"/query", user, q))
		r5dMasked(t, p.Records[0])
		if !slices.Equal(p.Records[0].FieldAccess.Masked, []string{"field.contact.name", "field.contact.balance"}) {
			t.Fatal("hidden presence changed masks")
		}
	}
	r5Publish(t, h, root+"/roles", alice, v.Version, r5dRole(app), false, uuid(t))
	if w = m5Call(h, base+"/query", user, q); w.Code != 403 {
		t.Fatal("revocation not live", w.Code)
	}
}

func TestM9RFieldPatchPreservesHiddenValuesLinksAndAtomicEvidence(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	parent := m5Create(t, legacy, base, alice, "PRIVATE parent")
	values := m5Values("PRIVATE required name")
	values["field.contact.parent"] = parent.ID
	w := m5Call(legacy, base+"/commands", alice, data.Command{Operation: "create", Values: values})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var child data.Record
	if json.Unmarshal(w.Body.Bytes(), &child) != nil {
		t.Fatal("child")
	}
	h, user, _, _ := r5dLimited(t, f, app, alice)
	c := data.Command{Key: uuid(t), Operation: "patch", ID: child.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": "inactive"}}
	w = m5Call(h, base+"/commands", user, c)
	if w.Code != 200 {
		t.Fatal("partial write", w.Code, w.Body.String())
	}
	var saved data.Record
	if json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Version != 2 {
		t.Fatal("saved")
	}
	r5dMasked(t, saved)
	if strings.Contains(w.Body.String(), "PRIVATE") || saved.Values["field.contact.status"] != "inactive" {
		t.Fatal("patch response")
	}
	if w = m5Call(h, base+"/commands", user, c); w.Code != 200 || !strings.Contains(w.Body.String(), `"replayed":true`) || strings.Contains(w.Body.String(), "PRIVATE") {
		t.Fatal("idempotent replay", w.Code)
	}
	q := data.Query{RecordID: child.ID, Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance", "field.contact.parent", "field.contact.status"}, Limit: 1}
	actual := m5Page(t, m5Call(legacy, base+"/query", alice, q)).Records[0]
	if actual.Values["field.contact.name"] != "PRIVATE required name" || actual.Values["field.contact.parent"] != parent.ID || actual.Values["field.contact.balance"].(map[string]any)["amount"] != "12.3" {
		t.Fatal("partial write lost untouched values")
	}
	var links, receipts, outbox int
	if err := f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.record_links WHERE tenant_id=$1 AND id=$2 AND target_id=$3),(SELECT count(*) FROM runtime.command_receipts WHERE tenant_id=$1 AND command_key=$4 AND operation='patch'),(SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1 AND record_id=$2 AND record_version=2)`, f.tenantA, child.ID, parent.ID, c.Key).Scan(&links, &receipts, &outbox); err != nil || links != 1 || receipts != 1 || outbox != 1 {
		t.Fatal("atomic links/evidence", err, links, receipts, outbox)
	}
	// Authorization precedes value validation, including required-field nulls.
	for _, value := range []any{nil, "", 123, strings.Repeat("x", 4097)} {
		denied := data.Command{Operation: "patch", ID: child.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.name": value}}
		if w = m5Call(h, base+"/commands", user, denied); w.Code != 403 {
			t.Fatal("hidden validation oracle", w.Code)
		}
	}
	for _, bad := range []data.Command{
		{Operation: "patch", ID: child.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.status": "bad enum"}},
		{Operation: "patch", ID: child.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.id": uuid(t)}},
		{Operation: "patch", ID: child.ID, ExpectedVersion: 2, Values: map[string]any{}},
	} {
		if w = m5Call(h, base+"/commands", user, bad); w.Code != 400 {
			t.Fatal("invalid patch", w.Code)
		}
	}
	stale := c
	stale.Key = uuid(t)
	if w = m5Call(h, base+"/commands", user, stale); w.Code != 409 {
		t.Fatal("CAS", w.Code)
	}
	clear := data.Command{Key: uuid(t), Operation: "patch", ID: child.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.parent": nil}}
	failing := r5Handler(t, f, failedAudit{})
	if w = m5Call(failing, base+"/commands", user, clear); w.Code != 503 {
		t.Fatal("audit failure", w.Code)
	}
	actual = m5Page(t, m5Call(legacy, base+"/query", alice, q)).Records[0]
	if actual.Version != 2 || actual.Values["field.contact.parent"] != parent.ID {
		t.Fatal("audit rollback lost data")
	}
	if w = m5Call(h, base+"/commands", user, clear); w.Code != 200 {
		t.Fatal("clear", w.Code, w.Body.String())
	}
	actual = m5Page(t, m5Call(legacy, base+"/query", alice, q)).Records[0]
	if actual.Values["field.contact.parent"] != nil || actual.Version != 3 {
		t.Fatal("explicit null")
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.record_links WHERE tenant_id=$1 AND id=$2`, f.tenantA, child.ID).Scan(&links); err != nil || links != 0 {
		t.Fatal("cleared reference", err)
	}
}

// Field scope can be narrower than entity visibility without dropping rows.
type r5dScoped struct{ scopedPolicy }

func (p r5dScoped) DataScope(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	if r.FieldID == "field.contact.name" {
		return p.scopedPolicy.DataScope(ctx, s, action, r, c)
	}
	return (policy.Native{}).DataScope(ctx, s, action, r, c)
}
func TestM9RFieldScopeMasksAndRejectsConditionalWriteOracle(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, _ := m5Fixture(t)
	a := m5Create(t, legacy, base, alice, "Owned visible")
	b := m5Create(t, legacy, base, alice, "Foreign hidden")
	team := uuid(t)
	tx, err := f.operator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, []any{f.aliceSubject, f.alice, f.tenantA}},
		{`UPDATE runtime.records SET owner_user_id=$1 WHERE tenant_id=$2 AND id=$3`, []any{f.bob, f.tenantA, b.ID}},
		{`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Mask scope')`, []any{f.tenantA, team}},
		{`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id) VALUES($1,$2,$3)`, []any{f.tenantA, team, f.alice}},
		{`UPDATE runtime.records SET owner_team_id=$1 WHERE tenant_id=$2 AND id=$3`, []any{team, f.tenantA, a.ID}},
		{`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id,can_write) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$1,true FROM runtime.records WHERE tenant_id=$2 AND id=$3`, []any{f.alice, f.tenantA, a.ID}},
	} {
		if _, err = tx.Exec(stmt.sql, stmt.args...); err != nil {
			t.Fatal("scope fixture", err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"owner", "team", "share", "condition"} {
		t.Run(mode, func(t *testing.T) {
			h := m5Handler(t, f, r5dScoped{scopedPolicy{mode}}, audit.Postgres{}, true)
			q := data.Query{Projection: []string{"field.contact.name", "field.contact.id"}, Limit: 100}
			p := m5Page(t, m5Call(h, base+"/query", alice, q))
			if len(p.Records) != 2 {
				t.Fatal("field filtered rowset")
			}
			for _, r := range p.Records {
				masked := slices.Contains(r.FieldAccess.Masked, "field.contact.name")
				if masked != (r.ID == b.ID) {
					t.Fatal("wrong mask")
				}
			}
			q.Sort = &data.Sort{Field: "field.contact.name"}
			if w := m5Call(h, base+"/query", alice, q); w.Code != 403 {
				t.Fatal("narrow field sort oracle", w.Code)
			}
			q.Sort = nil
			q.Filter = &data.Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"hidden"`)}
			if w := m5Call(h, base+"/query", alice, q); w.Code != 403 {
				t.Fatal("narrow field filter oracle", w.Code)
			}
			if mode == "condition" {
				for _, r := range []data.Record{a, b} {
					if w := m5Call(h, base+"/commands", alice, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: r.Version, Values: map[string]any{"field.contact.name": "Owned changed"}}); w.Code != 403 {
						t.Fatal("conditional write oracle", w.Code)
					}
				}
			} else {
				if w := m5Call(h, base+"/commands", alice, data.Command{Operation: "patch", ID: a.ID, ExpectedVersion: a.Version, Values: map[string]any{"field.contact.name": "Owned changed"}}); w.Code != 200 {
					t.Fatal("authorized scoped patch", w.Code, w.Body.String())
				}
				a.Version++
				if w := m5Call(h, base+"/commands", alice, data.Command{Operation: "patch", ID: b.ID, ExpectedVersion: b.Version, Values: map[string]any{"field.contact.name": nil}}); w.Code != 404 {
					t.Fatal("masked scoped patch", w.Code)
				}
			}
		})
	}
}

func TestM9RFieldPatchUnknownCommitReconciliation(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	record := m5Create(t, legacy, base, alice, "PRIVATE retained")
	h, user, _, _ := r5dLimited(t, f, app, alice)
	proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
	db, err := postgres.Open(proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }); err != nil {
		t.Fatal(err)
	}
	proxied := *f
	proxied.db = db
	faulted := r5Handler(t, &proxied, audit.Postgres{})
	command := data.Command{Key: uuid(t), Operation: "patch", ID: record.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": "inactive"}}
	if w := m5Call(faulted, base+"/commands", user, command); w.Code != 503 || !observed.Load() {
		t.Fatal("lost COMMIT response not reproduced", w.Code)
	}
	receipt := m9rReceipt(t, h, m9rReceiptPath(f, app, command.Key), user)
	if receipt.State != "COMMITTED" || receipt.Version != 2 || receipt.Operation != "patch" {
		t.Fatal("patch receipt unavailable")
	}
	m9rCommandCounts(t, f, app, 1, 1, 2)
	if w := m5Call(h, base+"/commands", user, command); w.Code != 200 {
		t.Fatal("reconciled replay", w.Code)
	}
	m9rCommandCounts(t, f, app, 1, 1, 2)
}

// An opaque callback cannot prove unseen rows safe by selecting a tiny page.
func TestM9RFieldOpaqueHeaderBudgetAndNativePageCompatibility(t *testing.T) {
	m8Database(t)
	f, owner, base, alice, _ := m5Fixture(t)
	first := m5Create(t, owner, base, alice, "Owned first")
	for i := 0; i < data.MaxOpaqueActionRows; i++ {
		m5Create(t, owner, base, alice, "Owned remaining")
	}
	query := data.Query{Projection: []string{"field.contact.id", "field.contact.name"}, Limit: 100}
	if page := m5Page(t, m5Call(owner, base+"/query", alice, query)); len(page.Records) != data.MaxOpaqueActionRows+1 {
		t.Fatal("native page limit changed")
	}
	opaque := m5Handler(t, f, r5dScoped{scopedPolicy{"owner"}}, audit.Postgres{}, true)
	if w := m5Call(opaque, base+"/query", alice, query); w.Code != 400 {
		t.Fatal("opaque header page is unbounded", w.Code)
	}
	query.Limit = 1
	query.Filter = &data.Filter{Op: "eq", Field: "field.contact.id", Value: json.RawMessage(`"` + first.ID + `"`)}
	if w := m5Call(opaque, base+"/query", alice, query); w.Code != 400 {
		t.Fatal("filter/limit shrank predicate universe", w.Code)
	}
	query.RecordID = first.ID
	if page := m5Page(t, m5Call(opaque, base+"/query", alice, query)); len(page.Records) != 1 || page.Records[0].ID != first.ID {
		t.Fatal("bounded exact record callback unsupported")
	}
}
