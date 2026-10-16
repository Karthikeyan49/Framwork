//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func choiceFixture(t *testing.T) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f, h, base, token, app := m5Fixture(t)
	scalarReviewTarget(t, f)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal("choice source unavailable")
	}
	var doc metadata.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("choice source decode")
	}
	doc.AppID = app
	for i := range doc.Definitions {
		d := &doc.Definitions[i]
		if d.ID == "field.contact.status" {
			d.Field.EnumValues = []string{"inactive", strings.Repeat("😀", 128), "active", "தமிழ்", "Å"}
		}
		if d.ID == "entity.contact" {
			d.Entity.Fields = append(d.Entity.Fields, "field.contact.enabled")
		}
	}
	doc.Definitions = append(doc.Definitions, metadata.Definition{ID: "field.contact.enabled", SchemaVersion: "1.0.0", Namespace: "example.contacts", Label: "Enabled", Kind: "field", Field: &metadata.Field{EntityRef: "entity.contact", Type: "bool"}})
	raw, _ = json.Marshal(doc)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw); w.Code != 201 {
		t.Fatal("choice revision rejected", w.Code)
	}
	return f, h, strings.Replace(base, "/versions/1/", "/versions/2/", 1), token, app
}

func choiceQuery(field string, desc bool) data.Query {
	q := m5Query()
	q.Projection = append(q.Projection, "field.contact.status")
	q.Sort = &data.Sort{Field: field, Desc: desc}
	q.Limit = 100
	return q
}

func choicePages(t *testing.T, h http.Handler, base, token string, query data.Query) []data.Record {
	t.Helper()
	out := []data.Record{}
	for range 100 {
		w := m5Call(h, base+"/query", token, query)
		if w.Code != 200 {
			t.Fatal("choice page rejected", w.Code)
		}
		var page data.Page
		if json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatal("choice page decode")
		}
		out = append(out, page.Records...)
		if page.Next == "" {
			return out
		}
		query.After = page.Next
	}
	t.Fatal("choice pagination did not finish")
	return nil
}

func choiceSeed(t *testing.T, f *iamFixture, app, hash string, count int) {
	t.Helper()
	_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name','Choice fixture',
 'field.contact.status',CASE WHEN i%11=0 THEN NULL ELSE (ARRAY['active','inactive','Å','தமிழ்',repeat('😀',128)])[1+i%5] END,
 'field.contact.parent',CASE WHEN i%13=0 THEN NULL ELSE md5((i%3+1)::text)::uuid::text END,
 'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int)i`, f.tenantA, app, hash, f.alice, f.viewer, count)
	scalarSQL(t, err)
}

func TestM9RChoiceIndexOrderingLifecycleAndMaintenance(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	r := m5Create(t, h, base, token, "Original choice")
	var hash string
	scalarSQL(t, f.operator.QueryRow(`SELECT ir_hash FROM runtime.record_sets WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=2`, f.tenantA, app).Scan(&hash))
	choiceSeed(t, f, app, hash, 1205)
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		before := map[bool][]data.Record{}
		for _, desc := range []bool{false, true} {
			query := choiceQuery(field, desc)
			query.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name"}}
			before[desc] = choicePages(t, h, base, token, query)
		}
		for _, desc := range []bool{false, true} {
			query := choiceQuery(field, desc)
			query.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name"}}
			path := base + "/indexes/" + field
			if !desc {
				idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
				if !reflect.DeepEqual(before[desc], choicePages(t, h, base, token, query)) {
					t.Fatal("choice incomplete index changed fallback")
				}
				idx = m9rIndex(t, h, path, token, idx, "build")
				if idx.ScannedRows != 1000 || idx.State != "BUILDING" {
					t.Fatal("choice copy budget")
				}
				if seen := m9rIndex(t, h, path, token, data.NumericIndex{Checkpoint: 1}, "build"); seen != idx {
					t.Fatal("choice checkpoint repeated effects")
				}
				idx = m9rIndexReady(t, h, path, token)
				if idx.ScannedRows != 1206 || idx.VerifiedRows != 1206 {
					t.Fatal("choice verification coverage")
				}
			}
			if !reflect.DeepEqual(before[desc], choicePages(t, h, base, token, query)) {
				t.Fatal("choice indexed order/null/tie/join/cursor changed")
			}
		}
	}
	patch := data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": strings.Repeat("😀", 128), "field.contact.parent": "c4ca4238-a0b9-2382-0dcc-509a6f75849b"}}
	if w := m5Call(h, base+"/commands", token, patch); w.Code != 200 {
		t.Fatal("choice valid patch rejected", w.Code)
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND source_version=2 AND NOT overflow`, f.tenantA, app, r.ID).Scan(&n))
	if n != 2 {
		t.Fatal("choice atomic projection maintenance")
	}
	patch.ExpectedVersion, patch.Values = 2, map[string]any{"field.contact.parent": nil}
	if w := m5Call(h, base+"/commands", token, patch); w.Code != 200 {
		t.Fatal("choice nullable reference clear", w.Code)
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: r.ID, ExpectedVersion: 3}); w.Code != 200 {
		t.Fatal("choice delete rejected", w.Code)
	}
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID).Scan(&n))
	if n != 0 {
		t.Fatal("choice deleted projection retained")
	}
}

func TestM9RChoiceIndexTypedContractsAndWitnesses(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	m5Create(t, h, base, token, "Typed choice")
	for _, c := range []struct {
		raw, typ, choices string
		required, valid   bool
	}{
		{`"active"`, "enum", `["active","inactive"]`, true, true},
		{`null`, "enum", `["active"]`, false, true}, {`null`, "enum", `["active"]`, true, false},
		{`"unknown"`, "enum", `["active"]`, false, false}, {`1`, "enum", `["active"]`, false, false},
		{`"active"`, "enum", `["inactive","active"]`, false, false}, {`"active"`, "enum", `["active","active"]`, false, false},
		{`"active"`, "enum", `[1]`, false, false}, {`"active"`, "enum", `null`, false, false},
		{`"00000000-0000-0000-0000-000000000000"`, "reference", `[]`, true, true},
		{`"11111111-1111-1111-1111-111111111111"`, "reference", `[]`, true, true},
		{`"AAAAAAAA-1111-1111-1111-111111111111"`, "reference", `[]`, false, false},
		{`"11111111111111111111111111111111"`, "reference", `[]`, false, false},
		{`"active"`, "reference", `[]`, false, false}, {`null`, "reference", `[]`, false, true},
		{`true`, "bool", `[]`, false, false},
	} {
		var text, date sql.NullString
		var overflow bool
		err := f.operator.QueryRow(`SELECT value_text,value_date::text,overflow FROM runtime.scalar_project_value($1::jsonb,$2,$3,$4::jsonb)`, c.raw, c.typ, c.required, c.choices).Scan(&text, &date, &overflow)
		if c.valid {
			scalarSQL(t, err)
			if overflow || date.Valid {
				t.Fatal("choice converter returned wrong physical kind")
			}
		} else {
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "22023" {
				t.Fatal("choice invalid contract/value accepted")
			}
		}
	}
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	if w := m5Call(h, base+"/indexes/field.contact.enabled/register", token, struct{}{}); w.Code != 400 {
		t.Fatal("unsupported boolean index admitted", w.Code)
	}
	// Operator-injected valid but different options cannot replace compiled IR.
	_, err := f.operator.Exec(`UPDATE runtime.scalar_indexes SET enum_values='["changed"]' WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.status'`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", token, choiceQuery("field.contact.status", false)); w.Code != 503 {
		t.Fatal("forged enum witness executed", w.Code)
	}
	_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET reference_to='entity.other' WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.parent'`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", token, choiceQuery("field.contact.parent", false)); w.Code != 503 {
		t.Fatal("forged reference witness executed", w.Code)
	}
}

func TestM9RChoiceIndexPermissionsScopesAndAtomicFailure(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	r := m5Create(t, h, base, token, "Owned choice")
	var hash string
	scalarSQL(t, f.operator.QueryRow(`SELECT ir_hash FROM runtime.record_sets WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=2`, f.tenantA, app).Scan(&hash))
	choiceSeed(t, f, app, hash, 21)
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		path := base + "/indexes/" + field
		for _, subject := range []string{f.viewerSubject, f.bobSubject} {
			if w := m5Call(h, path+"/register", f.token(t, subject, nil), struct{}{}); w.Code != 403 {
				t.Fatal("choice manager/tenant bypass", w.Code)
			}
		}
		hidden := m5Handler(t, f, denyDataPolicy{"data.field.read", field}, audit.Postgres{}, true)
		if w := m5Call(hidden, path+"/register", token, struct{}{}); w.Code != 403 {
			t.Fatal("hidden choice registration admitted", w.Code)
		}
		fault := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
		if w := m5Call(fault, path+"/register", token, struct{}{}); w.Code != 503 {
			t.Fatal("choice registration audit fault", w.Code)
		}
		var n int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field).Scan(&n))
		if n != 0 {
			t.Fatal("choice audit rejection left registration")
		}
		query := choiceQuery(field, false)
		before := choicePages(t, owners, base, token, query)
		if len(before) != 11 {
			t.Fatal("choice populated owner fixture")
		}
		idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
		if w := m5Call(fault, path+"/build", token, map[string]any{"expected_checkpoint": idx.Checkpoint}); w.Code != 503 {
			t.Fatal("choice build audit fault", w.Code)
		}
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field).Scan(&n))
		if n != 0 {
			t.Fatal("choice audit rejection left projections")
		}
		m9rIndexReady(t, h, path, token)
		if !reflect.DeepEqual(before, choicePages(t, owners, base, token, query)) {
			t.Fatal("choice indexed owner scope changed")
		}
		if w := m5Call(hidden, base+"/query", token, query); w.Code != 403 {
			t.Fatal("choice hidden sort inferred", w.Code)
		}
	}
	// SQL has no generic converter privilege and no tenant-triggered DDL path.
	principal, _ := f.verifier.Verify(token)
	err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `SELECT * FROM runtime.scalar_project_value('"active"','enum',true,'["active"]')`)
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("choice converter app privilege leaked")
	}
	for _, value := range []any{"undeclared", strings.Repeat("😀", 129)} {
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": value}}); w.Code != 400 {
			t.Fatal("invalid enum API mutation admitted", w.Code)
		}
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RChoiceIndexRawMaintenanceAndReadinessDrift(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	r := m5Create(t, h, base, token, "Raw compatible writer")
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	principal, _ := f.verifier.Verify(token)
	for _, c := range []struct{ field, raw string }{
		{"field.contact.status", `"not-declared"`}, {"field.contact.parent", `"AAAAAAAA-1111-1111-1111-111111111111"`},
	} {
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
				return err
			}
			_, err := q.ExecContext(ctx, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,ARRAY[$4::text],$5::jsonb),row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID, c.field, c.raw)
			return err
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "22023" {
			t.Fatal("choice raw invalid write passed typed trigger")
		}
	}
	var version int
	scalarSQL(t, f.operator.QueryRow(`SELECT row_version FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID).Scan(&version))
	if version != 1 {
		t.Fatal("choice rejected raw mutation committed")
	}
	// The old readiness function-name contract remains readable (no overloads).
	var safe bool
	scalarSQL(t, f.db.QueryRow(`SELECT count(*)=6 AND bool_and(p.proowner='framework_schema_owner'::regrole AND p.proconfig @> ARRAY['search_path=pg_catalog, pg_temp']
 AND p.prosecdef=(p.proname IN('register_scalar_index','build_scalar_index','maintain_scalar_projection'))
 AND has_function_privilege(p.oid,'EXECUTE')=(p.proname IN('scalar_collation_safe','register_scalar_index','build_scalar_index')))
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='runtime' AND p.proname IN('scalar_collation_safe','scalar_value','check_index_budget','register_scalar_index','maintain_scalar_projection','build_scalar_index')`).Scan(&safe))
	if !safe {
		t.Fatal("choice migration broke old readiness reader")
	}
	for _, c := range []struct{ typ, choices, target string }{
		{"enum", `null`, ""}, {"enum", `[]`, ""}, {"enum", `["b","a"]`, ""}, {"enum", `["a","a"]`, ""},
		{"enum", `[1]`, ""}, {"enum", `[""]`, ""}, {"reference", `[]`, "bad"}, {"string", `["a"]`, ""},
	} {
		scalarSQL(t, f.operator.QueryRow(`SELECT runtime.scalar_options_safe($1,$2::jsonb,$3)`, c.typ, c.choices, c.target).Scan(&safe))
		if safe {
			t.Fatal("choice options CHECK returned true for invalid shape")
		}
	}
	scalarSQL(t, f.operator.QueryRow(`SELECT runtime.scalar_options_safe(NULL,NULL,NULL) IS FALSE`).Scan(&safe))
	if !safe {
		t.Fatal("choice NULL options returned unknown")
	}
	_, err := f.operator.Exec(`GRANT EXECUTE ON FUNCTION runtime.scalar_project_value(jsonb,text,boolean,jsonb) TO framework_app`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("choice helper privilege drift remained ready")
	}
	_, err = f.operator.Exec(`REVOKE EXECUTE ON FUNCTION runtime.scalar_project_value(jsonb,text,boolean,jsonb) FROM framework_app`)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes DROP CONSTRAINT scalar_indexes_options`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("choice absent options invariant remained ready")
	}
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes ADD CONSTRAINT scalar_indexes_options CHECK(runtime.scalar_options_safe(field_type,enum_values,reference_to)) NOT VALID`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("choice unvalidated options invariant remained ready")
	}
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes VALIDATE CONSTRAINT scalar_indexes_options`)
	scalarSQL(t, err)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	// A JSON null is not the empty options array, even for an ordinary string.
	m9rIndexReady(t, h, base+"/indexes/field.contact.name", token)
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes DROP CONSTRAINT scalar_indexes_options`)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET enum_values='null'::jsonb WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.name'`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", token, choiceQuery("field.contact.name", false)); w.Code != 503 {
		t.Fatal("null legacy options executed as an empty array", w.Code)
	}
	_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET enum_values='[]'::jsonb WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.name'`, f.tenantA, app)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes ADD CONSTRAINT scalar_indexes_options CHECK(runtime.scalar_options_safe(field_type,enum_values,reference_to))`)
	scalarSQL(t, err)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}
