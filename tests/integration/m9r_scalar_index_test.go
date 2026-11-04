//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func scalarSQL(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			t.Fatalf("scalar fixture SQLSTATE %s", pg.Code)
		}
		t.Fatal("scalar fixture database operation failed")
	}
}
func scalarFixture(t *testing.T, extra int, otherEntities ...int) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f, h, base, token, app := m5Fixture(t)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal("source fixture unavailable")
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("source fixture decode")
	}
	doc["app_id"] = app
	defs := doc["definitions"].([]any)
	for i := range extra {
		id := fmt.Sprintf("field.contact.memo%d", i)
		for _, raw := range defs {
			d := raw.(map[string]any)
			if d["kind"] == "entity" && d["id"] == "entity.contact" {
				e := d["entity"].(map[string]any)
				e["fields"] = append(e["fields"].([]any), id)
			}
		}
		defs = append(defs, map[string]any{"schema_version": "1.0.0", "namespace": "example.contacts", "kind": "field", "id": id, "label": "Optional memo", "field": map[string]any{"entity_ref": "entity.contact", "type": "string", "required": false}})
	}
	if len(otherEntities) == 1 {
		for n := range otherEntities[0] {
			entity := fmt.Sprintf("entity.quota%d", n)
			id := fmt.Sprintf("field.quota%d.id", n)
			fields := []any{id}
			defs = append(defs, map[string]any{"schema_version": "1.0.0", "namespace": "example.contacts", "kind": "field", "id": id, "label": "Identifier", "field": map[string]any{"entity_ref": entity, "type": "string", "required": true}})
			for i := range 8 {
				id := fmt.Sprintf("field.quota%d.value%d", n, i)
				fields = append(fields, id)
				typ := "string"
				if i%2 == 0 {
					typ = "integer"
				}
				defs = append(defs, map[string]any{"schema_version": "1.0.0", "namespace": "example.contacts", "kind": "field", "id": id, "label": "Quota value", "field": map[string]any{"entity_ref": entity, "type": typ, "required": false}})
			}
			defs = append(defs, map[string]any{"schema_version": "1.0.0", "namespace": "example.contacts", "kind": "entity", "id": entity, "label": "Quota entity", "entity": map[string]any{"fields": fields}})
		}
	}
	doc["definitions"] = defs
	raw, _ = json.Marshal(doc)
	w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw)
	if w.Code != 201 {
		t.Fatal("scalar revision fixture", w.Code)
	}
	return f, h, strings.Replace(base, "/versions/1/", "/versions/2/", 1), token, app
}
func scalarCreate(t *testing.T, h http.Handler, base, token, memo string) data.Record {
	t.Helper()
	v := m5Values("Scalar fixture")
	v["field.contact.memo0"] = memo
	v["field.contact.joined"] = "2024-02-29"
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v})
	var r data.Record
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal("scalar create", w.Code)
	}
	return r
}
func scalarSeed(t *testing.T, f *iamFixture, app string, count int) string {
	t.Helper()
	var hash string
	scalarSQL(t, f.operator.QueryRow(`SELECT ir_hash FROM runtime.record_sets WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=2`, f.tenantA, app).Scan(&hash))
	_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name','Scalar row','field.contact.status',CASE WHEN i%2=0 THEN 'active' ELSE 'inactive' END,
 'field.contact.memo0',CASE WHEN i%17=0 THEN NULL ELSE (ARRAY['Alpha','alpha','Å','தமிழ்','ß','Ω','a-z','a z'])[1+i%8]||lpad((i%23)::text,3,'0') END,
 'field.contact.joined',CASE WHEN i%13=0 THEN NULL ELSE to_char(date '2024-01-01'+(i%300),'YYYY-MM-DD') END,
 'field.contact.balance',jsonb_build_object('amount',(i%37)::text,'currency','USD')),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int) i`, f.tenantA, app, hash, f.alice, f.viewer, count)
	scalarSQL(t, err)
	return hash
}
func scalarPages(t *testing.T, h http.Handler, base, token, field string, desc bool, filter *data.Filter) []data.Record {
	t.Helper()
	query := m5Query()
	query.Projection = append(query.Projection, "field.contact.memo0", "field.contact.joined")
	query.Limit = 100
	query.Sort = &data.Sort{Field: field, Desc: desc}
	query.Filter = filter
	out := []data.Record{}
	for range 100 {
		w := m5Call(h, base+"/query", token, query)
		if w.Code != 200 {
			t.Fatal("scalar page", w.Code)
		}
		var p data.Page
		if json.Unmarshal(w.Body.Bytes(), &p) != nil {
			t.Fatal("scalar page shape")
		}
		out = append(out, p.Records...)
		if p.Next == "" {
			return out
		}
		query.After = p.Next
	}
	t.Fatal("scalar pagination did not terminate")
	return nil
}
func TestM9RScalarIndexOrderingCheckpointsAndMaintenance(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	r := scalarCreate(t, h, base, token, "Original")
	hash := scalarSeed(t, f, app, 1205)
	for _, field := range []string{"field.contact.memo0", "field.contact.joined"} {
		beforeAsc, beforeDesc := scalarPages(t, h, base, token, field, false, nil), scalarPages(t, h, base, token, field, true, nil)
		path := base + "/indexes/" + field
		idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
		idx = m9rIndex(t, h, path, token, idx, "build")
		if idx.State != "BUILDING" || idx.ScannedRows != 1000 {
			t.Fatal("scalar bounded build")
		}
		replay := m5Call(h, path+"/build", token, map[string]any{"expected_checkpoint": int64(1)})
		var seen data.FieldIndex
		if replay.Code != 200 || json.Unmarshal(replay.Body.Bytes(), &seen) != nil || seen != idx {
			t.Fatal("scalar replay advanced checkpoint")
		}
		idx = m9rIndexReady(t, h, path, token)
		if idx.ScannedRows != 1206 || idx.VerifiedRows != 1206 || idx.State != "READY" {
			t.Fatal("scalar verification coverage")
		}
		if !reflect.DeepEqual(beforeAsc, scalarPages(t, h, base, token, field, false, nil)) || !reflect.DeepEqual(beforeDesc, scalarPages(t, h, base, token, field, true, nil)) {
			t.Fatal("scalar index changed ordering/null/tie/cursor semantics")
		}
	}
	patch := data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": "Changed", "field.contact.joined": "9999-12-31"}}
	if w := m5Call(h, base+"/commands", token, patch); w.Code != 200 {
		t.Fatal("scalar atomic update", w.Code)
	}
	var text string
	var version int64
	scalarSQL(t, f.operator.QueryRow(`SELECT value_text,source_version FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND field_id='field.contact.memo0'`, f.tenantA, app, r.ID).Scan(&text, &version))
	if text != "Changed" || version != 2 {
		t.Fatal("scalar projection stale after patch")
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: r.ID, ExpectedVersion: 2}); w.Code != 200 {
		t.Fatal("scalar delete", w.Code)
	}
	var remaining int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=2 AND ir_hash=$3 AND id=$4`, f.tenantA, app, hash, r.ID).Scan(&remaining))
	if remaining != 0 {
		t.Fatal("scalar delete resurrected projection")
	}
}
func TestM9RScalarIndexOverflowLongCursorAndReentry(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	records := []data.Record{}
	for _, s := range []string{"a", "b", "c"} {
		records = append(records, scalarCreate(t, h, base, token, strings.Repeat("&", 4095)+s))
	}
	query := m5Query()
	query.Projection = append(query.Projection, "field.contact.memo0")
	query.Sort = &data.Sort{Field: "field.contact.memo0"}
	query.Limit = 1
	first := m5Page(t, m5Call(h, base+"/query", token, query))
	decoded, err := base64.RawURLEncoding.DecodeString(first.Next)
	if err != nil || len(decoded) <= 1024 || len(first.Next) <= 2048 {
		t.Fatal("fixture did not exercise long cursor")
	}
	query.After = first.Next
	second := m5Page(t, m5Call(h, base+"/query", token, query))
	if len(second.Records) != 1 || second.Records[0].ID == first.Records[0].ID {
		t.Fatal("long cursor cannot resume")
	}
	before := scalarPages(t, h, base, token, "field.contact.memo0", false, nil)
	m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", token)
	if !reflect.DeepEqual(before, scalarPages(t, h, base, token, "field.contact.memo0", false, nil)) {
		t.Fatal("overflow dropped or truncated records")
	}
	var overflow int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND overflow AND value_text IS NULL`, f.tenantA, app).Scan(&overflow))
	if overflow != 3 {
		t.Fatal("overflow confused with SQL null")
	}
	for i, r := range records {
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": fmt.Sprint(i)}}); w.Code != 200 {
			t.Fatal("overflow-to-indexed write", w.Code)
		}
	}
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND overflow`, f.tenantA, app).Scan(&overflow))
	if overflow != 0 {
		t.Fatal("overflow marker remained")
	}
	// A future long write remains valid after READY; it cannot cause a B-tree error.
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: records[0].ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.memo0": strings.Repeat("界", 1365)}}); w.Code != 200 {
		t.Fatal("valid UTF-8 overflow write rejected", w.Code)
	}
	query.After = strings.Repeat("A", 43692)
	if w := m5Call(h, base+"/query", token, query); w.Code != 400 {
		t.Fatal("oversized cursor admitted", w.Code)
	}
}
func TestM9RScalarIndexTypedValidationAndCollationWitness(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarCreate(t, h, base, token, "Witness")
	for _, c := range []struct {
		raw, typ                  string
		required, valid, overflow bool
	}{
		{`null`, "string", false, true, false}, {`null`, "string", true, false, false}, {`""`, "string", true, false, false},
		{`""`, "string", false, true, false}, {`123`, "string", false, false, false}, {`"2024-02-29"`, "date", true, true, false},
		{`"2023-02-29"`, "date", true, false, false}, {`"2024-04-31"`, "date", true, false, false}, {`"0000-01-01"`, "date", true, false, false},
		{`"9999-12-31"`, "date", true, true, false}, {`"01/02/2024"`, "date", false, false, false}, {`"2024-01-01T00:00:00Z"`, "date", false, false, false},
		{`"` + strings.Repeat("x", 512) + `"`, "string", true, true, false}, {`"` + strings.Repeat("x", 513) + `"`, "string", true, true, true},
		{`"` + strings.Repeat("x", 4097) + `"`, "string", false, false, false}, {`null`, "datetime", false, false, false},
	} {
		var text, date sql.NullString
		var overflow bool
		err := f.operator.QueryRow(`SELECT value_text,value_date::text,overflow FROM runtime.scalar_value($1::jsonb,$2,$3)`, c.raw, c.typ, c.required).Scan(&text, &date, &overflow)
		var pg *pgconn.PgError
		if c.valid {
			scalarSQL(t, err)
			if overflow != c.overflow {
				t.Fatal("scalar byte-boundary mismatch")
			}
		} else if !errors.As(err, &pg) || pg.Code != "22023" {
			t.Fatal("scalar invalid value not rejected")
		}
	}
	m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", token)
	var original sql.NullString
	scalarSQL(t, f.operator.QueryRow(`SELECT collation_version FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&original))
	_, err := f.operator.Exec(`UPDATE runtime.scalar_indexes SET collation_version='independent-drift' WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	scalarSQL(t, err)
	query := m5Query()
	query.Sort = &data.Sort{Field: "field.contact.memo0"}
	if w := m5Call(h, base+"/query", token, query); w.Code != 503 {
		t.Fatal("stale collation witness used", w.Code)
	}
	_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET collation_version=$3 WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app, original)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", token, query); w.Code != 200 {
		t.Fatal("valid collation witness rejected", w.Code)
	}
}
func TestM9RScalarIndexPermissionsAuditAndCoverage(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarCreate(t, h, base, token, "Protected")
	path := base + "/indexes/field.contact.memo0"
	for _, subject := range []string{f.viewerSubject, f.bobSubject} {
		if w := m5Call(h, path+"/register", f.token(t, subject, nil), struct{}{}); w.Code != 403 {
			t.Fatal("scalar manager/tenant bypass", w.Code)
		}
	}
	hidden := m5Handler(t, f, denyDataPolicy{"data.field.read", "field.contact.memo0"}, audit.Postgres{}, true)
	if w := m5Call(hidden, path+"/register", token, struct{}{}); w.Code != 403 {
		t.Fatal("hidden scalar field admitted", w.Code)
	}
	fault := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
	if w := m5Call(fault, path+"/register", token, struct{}{}); w.Code != 503 {
		t.Fatal("scalar audit registration fault", w.Code)
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&n))
	if n != 0 {
		t.Fatal("scalar registration survived audit failure")
	}
	idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	if w := m5Call(fault, path+"/build", token, map[string]any{"expected_checkpoint": idx.Checkpoint}); w.Code != 503 {
		t.Fatal("scalar audit backfill fault", w.Code)
	}
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&n))
	if n != 0 {
		t.Fatal("scalar backfill survived audit failure")
	}
	idx = m9rIndex(t, h, path, token, idx, "build")
	if idx.State != "VERIFYING" {
		t.Fatal("scalar copy skipped verification")
	}
	_, err := f.operator.Exec(`DELETE FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, path+"/build", token, map[string]any{"expected_checkpoint": idx.Checkpoint}); w.Code != 409 {
		t.Fatal("scalar corrupt coverage became READY", w.Code)
	}
	principal, _ := f.verifier.Verify(token)
	for _, statement := range []string{`UPDATE runtime.scalar_indexes SET state='READY' WHERE tenant_id=$1`, `DELETE FROM runtime.scalar_projections WHERE tenant_id=$1`, `ALTER TABLE runtime.scalar_projections ADD COLUMN forged text`} {
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, e := m3Scope(f, ctx, q, principal, f.tenantA); e != nil {
				return e
			}
			var e error
			if strings.Contains(statement, "$1") {
				_, e = q.ExecContext(ctx, statement, f.tenantA)
			} else {
				_, e = q.ExecContext(ctx, statement)
			}
			return e
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatal("scalar app forgery/DDL permitted")
		}
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	scalarSQL(t, err)
	defer worker.Close()
	for _, table := range []string{"scalar_indexes", "scalar_projections"} {
		err := worker.QueryRow(`SELECT count(*) FROM runtime.` + table).Scan(&n)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatal("worker discovered scalar table")
		}
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func scalarScope(t *testing.T, h http.Handler, base, token string) data.Scope {
	t.Helper()
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	p, err := compiler.Restore(witness["compiled_ir"].(json.RawMessage), witness["ir_hash"].(string), compiler.SupportedIRVersions())
	scalarSQL(t, err)
	scope, err := data.NewScope(p, 2)
	scalarSQL(t, err)
	return scope
}
