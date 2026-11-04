//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

var nativeFields = []string{"field.contact.occurred", "field.contact.enabled"}

func nativeFixture(t *testing.T) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f, h, base, token, app := m5Fixture(t)
	scalarReviewTarget(t, f)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal("native source unavailable")
	}
	var doc metadata.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("native source decode")
	}
	doc.AppID = app
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == "entity.contact" {
			doc.Definitions[i].Entity.Fields = append(doc.Definitions[i].Entity.Fields, nativeFields...)
		}
	}
	for i, typ := range []string{"datetime", "bool"} {
		doc.Definitions = append(doc.Definitions, metadata.Definition{ID: nativeFields[i], SchemaVersion: "1.0.0", Namespace: "example.contacts", Label: typ, Kind: "field", Field: &metadata.Field{EntityRef: "entity.contact", Type: typ}})
	}
	raw, _ = json.Marshal(doc)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw); w.Code != 201 {
		t.Fatal("native revision rejected", w.Code)
	}
	return f, h, strings.Replace(base, "/versions/1/", "/versions/2/", 1), token, app
}
func nativeQuery(field string, desc bool) data.Query {
	q := m5Query()
	q.Projection = append(q.Projection, nativeFields...)
	q.Sort = &data.Sort{Field: field, Desc: desc}
	q.Limit = 100
	return q
}
func nativeSeed(t *testing.T, f *iamFixture, app, hash string, count int) {
	t.Helper()
	_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name','Native fixture','field.contact.status','active',
 'field.contact.occurred',CASE WHEN i%13=0 THEN NULL ELSE to_char(timestamptz '2024-02-29T00:00:00Z'+(i%43)*interval '1 second','YYYY-MM-DD"T"HH24:MI:SS.US"Z"') END,
 'field.contact.enabled',CASE WHEN i%11=0 THEN NULL ELSE i%2=0 END,
 'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int)i`, f.tenantA, app, hash, f.alice, f.viewer, count)
	scalarSQL(t, err)
}
func nativePGCode(t *testing.T, err error, code string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code {
		t.Fatal("native fixture expected typed SQLSTATE", code)
	}
}
func TestM9RNativeIndexOrderingLifecycleAndMaintenance(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	r := m5Create(t, h, base, token, "Original native")
	scope := scalarScope(t, h, base, token)
	nativeSeed(t, f, app, scope.Hash(), 1205)
	for _, field := range nativeFields {
		before := map[bool][]data.Record{}
		for _, desc := range []bool{false, true} {
			before[desc] = choicePages(t, h, base, token, nativeQuery(field, desc))
		}
		path := base + "/indexes/" + field
		idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
		if !reflect.DeepEqual(before[false], choicePages(t, h, base, token, nativeQuery(field, false))) {
			t.Fatal("native incomplete index changed fallback")
		}
		idx = m9rIndex(t, h, path, token, idx, "build")
		if idx.State != "BUILDING" || idx.ScannedRows != 1000 {
			t.Fatal("native copy budget")
		}
		if replay := m9rIndex(t, h, path, token, data.NumericIndex{Checkpoint: 1}, "build"); replay != idx {
			t.Fatal("native checkpoint repeated effects")
		}
		idx = m9rIndexReady(t, h, path, token)
		if idx.ScannedRows != 1206 || idx.VerifiedRows != 1206 {
			t.Fatal("native verification coverage")
		}
		for _, desc := range []bool{false, true} {
			if !reflect.DeepEqual(before[desc], choicePages(t, h, base, token, nativeQuery(field, desc))) {
				t.Fatal("native indexed order/null/tie/cursor changed")
			}
		}
	}
	patch := data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{nativeFields[0]: "9999-12-31T23:59:59.999999999Z", nativeFields[1]: false}}
	if w := m5Call(h, base+"/commands", token, patch); w.Code != 200 {
		t.Fatal("native valid boundary patch rejected", w.Code)
	}
	var instant string
	var boolean bool
	scalarSQL(t, f.operator.QueryRow(`SELECT value_instant::text FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND field_type='datetime' AND source_version=2`, f.tenantA, app, r.ID).Scan(&instant))
	scalarSQL(t, f.operator.QueryRow(`SELECT value_boolean FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND field_type='bool' AND source_version=2`, f.tenantA, app, r.ID).Scan(&boolean))
	if !strings.HasPrefix(instant, "10000-01-01") || boolean {
		t.Fatal("native carry/false projection incorrect")
	}
	patch.ExpectedVersion, patch.Values = 2, map[string]any{nativeFields[0]: nil, nativeFields[1]: nil}
	if w := m5Call(h, base+"/commands", token, patch); w.Code != 200 {
		t.Fatal("native nullable clear", w.Code)
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: r.ID, ExpectedVersion: 3}); w.Code != 200 {
		t.Fatal("native delete", w.Code)
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID).Scan(&n))
	if n != 0 {
		t.Fatal("native deleted projection retained")
	}
}

func TestM9RNativeIndexTimestampPrecisionOffsetsAndAPIBounds(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	originals := []string{
		"2024-02-29T00:00:00.123456400Z", "2024-02-29T00:00:00.123456499Z", "2024-02-29T00:00:00.123456500Z", "2024-02-29T00:00:00.123456501Z", "2024-02-29T00:00:00.123456600Z",
		"2024-02-29T23:59:59.999999499Z", "2024-02-29T23:59:59.999999500Z", "2024-02-29T23:59:59.999999999Z",
		"2024-03-10T01:59:59-05:00", "2024-03-10T03:00:00-04:00", "2024-02-29T23:59:00+23:59", "2024-02-29T00:00:00-23:59",
		"0001-01-01T00:01:00+00:01", "0000-12-31T23:59:00-00:01", "9999-12-31T23:59:59.999999999Z",
	}
	for i, s := range originals {
		values := m5Values("Precision")
		values[nativeFields[0]] = s
		values[nativeFields[1]] = i%2 == 0
		w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values})
		if w.Code != 200 {
			t.Fatal("native compatible datetime API rejected", i, w.Code)
		}
	}
	for n := 1; n <= 9; n++ {
		values := m5Values("Fraction length")
		values[nativeFields[0]] = "2024-02-29T00:00:00." + strings.Repeat("1", n) + "Z"
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values}); w.Code != 200 {
			t.Fatal("native fraction length rejected", n, w.Code)
		}
	}
	// Small pages force raw cursor values across ties and the rounded year-10000 key.
	before := map[string][]data.Record{}
	for _, field := range nativeFields {
		for _, desc := range []bool{false, true} {
			q := nativeQuery(field, desc)
			q.Limit = 1
			before[fmt.Sprint(field, desc)] = choicePages(t, h, base, token, q)
		}
	}
	for _, field := range nativeFields {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	for _, field := range nativeFields {
		for _, desc := range []bool{false, true} {
			q := nativeQuery(field, desc)
			q.Limit = 1
			if !reflect.DeepEqual(before[fmt.Sprint(field, desc)], choicePages(t, h, base, token, q)) {
				t.Fatal("native fractional/offset/carry cursor changed")
			}
		}
	}
	r := before[fmt.Sprint(nativeFields[0], false)][0]
	for _, s := range []string{"0001-01-01T00:00:00+00:01", "9999-12-31T23:59:59-00:01", "2024-02-30T00:00:00Z", "2024-01-01T00:00:00+24:00", "2024-01-01T00:00:00.1234567890Z", "2024-01-01T24:00:00Z", "2024-01-01T00:00:60Z", "infinity", "now", "2024-01-01T00:00:00"} {
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{nativeFields[0]: s}}); w.Code != 400 {
			t.Fatal("native invalid datetime API admitted", w.Code)
		}
	}
	for _, v := range []any{"false", 0, 1, []any{false}} {
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{nativeFields[1]: v}}); w.Code != 400 {
			t.Fatal("native truthy boolean admitted", w.Code)
		}
	}
	// Verify every projected instant equals the existing SQL cast, including carry.
	var wrong int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections p JOIN runtime.records r USING(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) WHERE p.tenant_id=$1 AND p.app_id=$2 AND p.field_type='datetime' AND p.value_instant IS DISTINCT FROM (r.custom_data->>p.field_id)::timestamptz`, f.tenantA, app).Scan(&wrong))
	if wrong != 0 {
		t.Fatal("native instant changed SQL precision")
	}
}

func TestM9RNativeIndexPrivateConverterAndPhysicalContracts(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	r := m5Create(t, h, base, token, "Native contract")
	for _, field := range nativeFields {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	for _, c := range []struct {
		raw, typ        string
		required, valid bool
	}{
		{`true`, "bool", true, true}, {`false`, "bool", true, true}, {`null`, "bool", false, true}, {`null`, "bool", true, false}, {`"false"`, "bool", false, false}, {`1`, "bool", false, false},
		{`"0001-01-01T00:00:00Z"`, "datetime", true, true}, {`"9999-12-31T23:59:59.999999999Z"`, "datetime", true, true}, {`"2024-02-29T10:00:00.123456789+02:00"`, "datetime", true, true},
		{`"0001-01-01T00:00:00+00:01"`, "datetime", false, false}, {`"9999-12-31T23:59:59-00:01"`, "datetime", false, false}, {`"2024-02-30T00:00:00Z"`, "datetime", false, false},
		{`"2024-02-29T00:00:00+23:59"`, "datetime", false, false}, {`"infinity"`, "datetime", false, false}, {`"now"`, "datetime", false, false}, {`"2024-01-01"`, "datetime", false, false}, {`true`, "datetime", false, false},
		{`null`, "datetime", false, true}, {`null`, "datetime", true, false},
	} {
		tx, err := f.operator.Begin()
		scalarSQL(t, err)
		_, err = tx.Exec(`SET LOCAL TimeZone='America/Los_Angeles';SET LOCAL DateStyle='SQL, DMY'`)
		scalarSQL(t, err)
		var text, date, instant sql.NullString
		var boolean sql.NullBool
		var overflow bool
		err = tx.QueryRow(`SELECT value_text,value_date::text,value_instant::text,value_boolean,overflow FROM runtime.scalar_native_value($1::jsonb,$2,$3,'[]')`, c.raw, c.typ, c.required).Scan(&text, &date, &instant, &boolean, &overflow)
		if c.valid {
			scalarSQL(t, err)
			if text.Valid || date.Valid || overflow {
				t.Fatal("native converter physical type")
			}
			if c.typ == "bool" && instant.Valid || c.typ == "datetime" && boolean.Valid {
				t.Fatal("native converter mixed physical columns")
			}
		} else {
			nativePGCode(t, err, "22023")
		}
		scalarSQL(t, tx.Rollback())
	}
	for _, c := range []struct{ field, raw string }{{nativeFields[0], `"tomorrow"`}, {nativeFields[0], `"2024-02-30T00:00:00Z"`}, {nativeFields[1], `"true"`}, {nativeFields[1], `1`}} {
		principal, _ := f.verifier.Verify(token)
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
				return err
			}
			_, err := q.ExecContext(ctx, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,ARRAY[$4::text],$5::jsonb),row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID, c.field, c.raw)
			return err
		})
		nativePGCode(t, err, "22023")
	}
	for _, set := range []string{"value_boolean=true WHERE field_type='datetime'", "value_instant=now() WHERE field_type='bool'", "value_text='wrong' WHERE field_type='bool'", "value_date=current_date WHERE field_type='datetime'", "overflow=true WHERE field_type='datetime'", "value_instant='infinity' WHERE field_type='datetime'"} {
		_, err := f.operator.Exec(`UPDATE runtime.scalar_projections SET `+set+` AND tenant_id=$1 AND app_id=$2`, f.tenantA, app)
		nativePGCode(t, err, "23514")
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	_, err := f.operator.Exec(`GRANT EXECUTE ON FUNCTION runtime.scalar_native_value(jsonb,text,boolean,jsonb) TO framework_app`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("native helper grant drift remained ready")
	}
	_, err = f.operator.Exec(`REVOKE EXECUTE ON FUNCTION runtime.scalar_native_value(jsonb,text,boolean,jsonb) FROM framework_app`)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`ALTER FUNCTION runtime.scalar_native_value(jsonb,text,boolean,jsonb) RESET TimeZone`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("native timezone setting drift remained ready")
	}
	_, err = f.operator.Exec(`ALTER FUNCTION runtime.scalar_native_value(jsonb,text,boolean,jsonb) SET TimeZone='UTC'`)
	scalarSQL(t, err)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RNativeIndexManagerScopeAndAuditRollback(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	m5Create(t, h, base, token, "Native permissions")
	scope := scalarScope(t, h, base, token)
	nativeSeed(t, f, app, scope.Hash(), 21)
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	for _, field := range nativeFields {
		path := base + "/indexes/" + field
		for _, subject := range []string{f.viewerSubject, f.bobSubject} {
			if w := m5Call(h, path+"/register", f.token(t, subject, nil), struct{}{}); w.Code != 403 {
				t.Fatal("native manager/tenant bypass", w.Code)
			}
		}
		hidden := m5Handler(t, f, denyDataPolicy{"data.field.read", field}, audit.Postgres{}, true)
		if w := m5Call(hidden, path+"/register", token, struct{}{}); w.Code != 403 {
			t.Fatal("native hidden registration", w.Code)
		}
		fault := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
		if w := m5Call(fault, path+"/register", token, struct{}{}); w.Code != 503 {
			t.Fatal("native registration audit fault", w.Code)
		}
		var n int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field).Scan(&n))
		if n != 0 {
			t.Fatal("native rejected registration persisted")
		}
		before := choicePages(t, owners, base, token, nativeQuery(field, false))
		if len(before) != 11 {
			t.Fatal("native populated owner scope")
		}
		idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
		if w := m5Call(fault, path+"/build", token, map[string]any{"expected_checkpoint": idx.Checkpoint}); w.Code != 503 {
			t.Fatal("native build audit fault", w.Code)
		}
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field).Scan(&n))
		if n != 0 {
			t.Fatal("native rejected build persisted")
		}
		m9rIndexReady(t, h, path, token)
		if !reflect.DeepEqual(before, choicePages(t, owners, base, token, nativeQuery(field, false))) {
			t.Fatal("native scope changed after index")
		}
		if w := m5Call(hidden, base+"/query", token, nativeQuery(field, false)); w.Code != 403 {
			t.Fatal("native hidden sort inferred", w.Code)
		}
	}
}
