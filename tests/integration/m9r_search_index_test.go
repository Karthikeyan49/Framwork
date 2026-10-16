//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func searchFixture(t *testing.T) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	return scalarFixture(t, 1)
}
func searchQuery(text string) data.Query {
	q := m5Query()
	q.Search = &data.Search{Field: "field.contact.memo0", Text: text}
	q.Projection = append(q.Projection, "field.contact.memo0")
	q.Limit = 100
	return q
}
func searchSeed(t *testing.T, f *iamFixture, app, hash string, count int) {
	t.Helper()
	_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name','Search fixture','field.contact.status','active',
 'field.contact.memo0',CASE WHEN i%13=0 THEN NULL WHEN i%5=0 THEN 'beta unrelated' ELSE 'Alpha Mixed orchard தமிழ்' END,
 'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int)i`, f.tenantA, app, hash, f.alice, f.viewer, count)
	scalarSQL(t, err)
}
func TestM9RSearchIndexLifecycleEquivalenceAndMaintenance(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := searchFixture(t)
	r := scalarCreate(t, h, base, token, "Alpha orchard")
	scope := scalarScope(t, h, base, token)
	searchSeed(t, f, app, scope.Hash(), 1205)
	before := map[string][]data.Record{}
	for _, text := range []string{"alpha", "ALPHA orchard", "தமிழ்", "orch", "missing", "beta | unrelated"} {
		before[text] = choicePages(t, h, base, token, searchQuery(text))
	}
	if len(before["orch"]) != 0 || len(before["alpha"]) < 500 {
		t.Fatal("search semantics fixture")
	}
	path := base + "/search-indexes/field.contact.memo0"
	idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	if !reflect.DeepEqual(before["alpha"], choicePages(t, h, base, token, searchQuery("alpha"))) {
		t.Fatal("search incomplete fallback changed")
	}
	idx = m9rIndex(t, h, path, token, idx, "build")
	if idx.ScannedRows != 1000 || idx.State != "BUILDING" {
		t.Fatal("search copy budget")
	}
	if replay := m9rIndex(t, h, path, token, data.NumericIndex{Checkpoint: 1}, "build"); replay != idx {
		t.Fatal("search checkpoint repeated effects")
	}
	idx = m9rIndexReady(t, h, path, token)
	if idx.ScannedRows != 1206 || idx.VerifiedRows != 1206 {
		t.Fatal("search coverage")
	}
	for text, expected := range before {
		if !reflect.DeepEqual(expected, choicePages(t, h, base, token, searchQuery(text))) {
			t.Fatal("search indexed result/cursor changed")
		}
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": "Beta unrelated"}}); w.Code != 200 {
		t.Fatal("search patch", w.Code)
	}
	var valid bool
	scalarSQL(t, f.operator.QueryRow(`SELECT value_search@@plainto_tsquery('pg_catalog.simple','beta unrelated') AND source_version=2 FROM runtime.search_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID).Scan(&valid))
	if !valid {
		t.Fatal("search maintenance mismatch")
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.memo0": nil}}); w.Code != 200 {
		t.Fatal("search clear", w.Code)
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: r.ID, ExpectedVersion: 3}); w.Code != 200 {
		t.Fatal("search delete", w.Code)
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.search_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID).Scan(&n))
	if n != 0 {
		t.Fatal("search deleted projection retained")
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}
func TestM9RSearchInputsPoliciesAndAuditRollback(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := searchFixture(t)
	scalarCreate(t, h, base, token, "Alpha orchard")
	scope := scalarScope(t, h, base, token)
	searchSeed(t, f, app, scope.Hash(), 21)
	path := base + "/search-indexes/field.contact.memo0"
	for _, subject := range []string{f.viewerSubject, f.bobSubject} {
		if w := m5Call(h, path+"/register", f.token(t, subject, nil), struct{}{}); w.Code != 403 {
			t.Fatal("search manager boundary", w.Code)
		}
	}
	for _, field := range []string{"field.contact.id", "field.contact.balance", "field.contact.status", "field.contact.joined", "field.contact.parent", "field.contact.missing"} {
		if w := m5Call(h, base+"/search-indexes/"+field+"/register", token, struct{}{}); w.Code != 400 {
			t.Fatal("search wrong field admitted", w.Code)
		}
	}
	hidden := m5Handler(t, f, denyDataPolicy{"data.field.read", "field.contact.memo0"}, audit.Postgres{}, true)
	if w := m5Call(hidden, path+"/register", token, struct{}{}); w.Code != 403 {
		t.Fatal("search hidden manager", w.Code)
	}
	q := searchQuery("alpha")
	q.Projection = m5Query().Projection
	if w := m5Call(hidden, base+"/query", token, q); w.Code != 403 {
		t.Fatal("unprojected hidden search inferred", w.Code)
	}
	fault := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
	if w := m5Call(fault, path+"/register", token, struct{}{}); w.Code != 503 {
		t.Fatal("search registration audit fault", w.Code)
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.search_indexes WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&n))
	if n != 0 {
		t.Fatal("search rejected registration persisted")
	}
	idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	if w := m5Call(fault, path+"/build", token, map[string]any{"expected_checkpoint": idx.Checkpoint}); w.Code != 503 {
		t.Fatal("search build audit fault", w.Code)
	}
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.search_projections WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&n))
	if n != 0 {
		t.Fatal("search rejected copy persisted")
	}
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	before := choicePages(t, owners, base, token, searchQuery("alpha"))
	if len(before) == 0 || len(before) >= 22 {
		t.Fatal("search populated owner fixture")
	}
	m9rIndexReady(t, h, path, token)
	if !reflect.DeepEqual(before, choicePages(t, owners, base, token, searchQuery("alpha"))) {
		t.Fatal("search owner scope changed")
	}
	for _, text := range []string{"", " ", "!!!", strings.Repeat("a", 257), "alpha\x00beta", "a b c d e f g h i j k l m n o p q"} {
		if w := m5Call(h, base+"/query", token, searchQuery(text)); w.Code != 400 {
			t.Fatal("search invalid input admitted", w.Code)
		}
	}
	q = searchQuery("alpha")
	q.Sort = &data.Sort{Field: "field.contact.id"}
	if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
		t.Fatal("search unsupported sort", w.Code)
	}
	q = searchQuery("alpha")
	q.Aggregate = &data.Aggregate{Op: "count"}
	if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
		t.Fatal("search unsupported aggregate", w.Code)
	}
	q = searchQuery("alpha")
	q.Search.Field = "field.contact.status"
	if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
		t.Fatal("search unsupported type", w.Code)
	}
	// Normal substring matching keeps its earlier behavior.
	q = m5Query()
	q.Filter = &data.Filter{Op: "contains", Field: "field.contact.memo0", Value: json.RawMessage(`"orch"`)}
	if len(choicePages(t, h, base, token, q)) != len(choicePages(t, h, base, token, searchQuery("orchard"))) {
		t.Fatal("search changed contains semantics")
	}
}
func TestM9RSearchContextRawTenantFenceWitnessAndPoolCleanup(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := searchFixture(t)
	scalarCreate(t, h, base, token, "Alpha orchard")
	m9rIndexReady(t, h, base+"/search-indexes/field.contact.memo0", token)
	other, oh, ob, ot, _ := searchFixture(t)
	scalarCreate(t, oh, ob, ot, "Alpha private")
	m9rIndexReady(t, oh, ob+"/search-indexes/field.contact.memo0", ot)
	scope := scalarScope(t, h, base, token)
	principal, _ := f.verifier.Verify(token)
	scalarSQL(t, postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		access, err := m3Scope(f, ctx, q, principal, f.tenantA)
		if err != nil {
			return err
		}
		var count int
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.search_projections`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("missing search context exposed vectors")
		}
		if _, err = q.ExecContext(ctx, `SELECT set_config('app.search_text','alpha',true)`); err != nil {
			return err
		}
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.search_projections`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return errors.New("search populated raw tenant boundary")
		}
		for _, text := range []string{"alpha", "beta", "!!!"} {
			_, err := (data.Store{}).Query(ctx, q, scope, access, "entity.contact", searchQuery(text), func(context.Context, string, data.Resource) error { return nil }, func(context.Context, string, data.Resource) (policy.DataScope, error) {
				return policy.DataScope{Kind: "ALL"}, nil
			})
			if text == "!!!" && !errors.Is(err, data.ErrInvalid) {
				return errors.New("search invalid same-tx query admitted")
			}
			if text != "!!!" && err != nil {
				return err
			}
			var value string
			if err = q.QueryRowContext(ctx, `SELECT current_setting('app.search_text',true)`).Scan(&value); err != nil {
				return err
			}
			if value != "" {
				return errors.New("search query left transaction context")
			}
		}
		if _, err = q.ExecContext(ctx, `SELECT set_config('app.search_text','alpha',true),set_config('app.tenant_id',$1,true)`, other.tenantA); err != nil {
			return err
		}
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.search_projections`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("search query GUC granted another tenant")
		}
		return nil
	}))
	_, err := f.operator.Exec(`UPDATE runtime.search_indexes SET parser_hash=repeat('0',64) WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", token, searchQuery("alpha")); w.Code != 503 {
		t.Fatal("search stale parser silently fell back", w.Code)
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: m5Values("Stale parser")}); w.Code == 200 {
		t.Fatal("search stale parser mixed writes")
	}
	_, err = f.operator.Exec(`UPDATE runtime.search_indexes SET parser_hash=runtime.search_parser_hash() WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`ALTER POLICY search_match ON runtime.search_projections USING(true)`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("search weakened policy remained ready")
	}
	_, err = f.operator.Exec(`ALTER POLICY search_match ON runtime.search_projections USING(value_search @@ (SELECT runtime.search_context()))`)
	scalarSQL(t, err)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	var valid bool
	scalarSQL(t, f.operator.QueryRow(`SELECT runtime.search_value('null',false)=''::tsvector`).Scan(&valid))
	if !valid {
		t.Fatal("search optional null")
	}
	for _, raw := range []string{`null`, `1`, `true`, `""`} {
		var v string
		err := f.operator.QueryRow(`SELECT runtime.search_value($1::jsonb,true)::text`, raw).Scan(&v)
		nativePGCode(t, err, "22023")
	}
}

func TestM9RSearchLongTextQueryWitnessCursorAndSharedQuota(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := scalarFixture(t, 7)
	for i, text := range []string{"Alpha " + strings.Repeat("prefix ", 500) + " orchard", strings.Repeat("x", 3000), "ALPHA orchard beta", "beta orchard"} {
		r := scalarCreate(t, h, base, token, text)
		if i == 0 && r.Version != 1 {
			t.Fatal("search long source rejected")
		}
	}
	queries := []string{"alpha", "alpha:*", "alpha | beta", "alpha -beta", "orchard", "prefix", strings.Repeat("x", 255)}
	before := map[string][]data.Record{}
	for _, text := range queries {
		before[text] = choicePages(t, h, base, token, searchQuery(text))
	}
	for i := range 7 {
		m9rIndex(t, h, base+"/indexes/field.contact.memo"+string(rune('0'+i)), token, data.NumericIndex{}, "register")
	}
	path := base + "/search-indexes/field.contact.memo0"
	m9rIndexReady(t, h, path, token)
	for _, field := range []string{"field.contact.memo1", "field.contact.name"} {
		if w := m5Call(h, base+"/search-indexes/"+field+"/register", token, struct{}{}); w.Code != 400 {
			t.Fatal("search shared entity quota bypass", w.Code)
		}
	}
	if w := m5Call(h, base+"/indexes/field.contact.balance/register", token, struct{}{}); w.Code != 400 {
		t.Fatal("numeric ignored search quota", w.Code)
	}
	for text, expected := range before {
		if !reflect.DeepEqual(expected, choicePages(t, h, base, token, searchQuery(text))) {
			t.Fatal("long/plain search indexed semantics changed")
		}
	}
	q := searchQuery("orchard")
	q.Limit = 1
	w := m5Call(h, base+"/query", token, q)
	var p data.Page
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Next == "" {
		t.Fatal("search cursor fixture")
	}
	q.After = p.Next
	q.Search.Text = "beta"
	if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
		t.Fatal("search cursor accepted different words", w.Code)
	}
	q.Search.Text = "orchard"
	q.Search.Field = "field.contact.name"
	if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
		t.Fatal("search cursor accepted different field", w.Code)
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}
