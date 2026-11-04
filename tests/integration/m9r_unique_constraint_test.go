//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

// Existing non-unique records are populated before binding the new integrity
// contract. This rehearses real adoption rather than bypassing the new trigger.
func uniqueFixture(t *testing.T, count int, duplicates bool) (*iamFixture, http.Handler, string, string, string, data.Scope) {
	t.Helper()
	return uniqueFixtureAt(t, count, duplicates, "1.4.0")
}

// uniqueFixtureAt authors the unique-field draft at the given authoring schema version: 1.4.0 introduced field.unique and its
// superset 1.5.0 (the tool contract) carries it too.
func uniqueFixtureAt(t *testing.T, count int, duplicates bool, version string) (*iamFixture, http.Handler, string, string, string, data.Scope) {
	t.Helper()
	f, h, old, token, app := scalarFixture(t, 1)
	scope := scalarScope(t, h, old, token)
	_, err := f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3) ON CONFLICT DO NOTHING`, f.tenantA, app, scope.Hash())
	scalarSQL(t, err)
	_, err = f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name',repeat('Representative integrity text ',35),'field.contact.status','active',
 'field.contact.memo0',CASE WHEN $7::boolean THEN 'duplicate' WHEN i%17=0 THEN NULL ELSE 'unique-'||i::text END,
 'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int)i`, f.tenantA, app, scope.Hash(), f.alice, f.viewer, count, duplicates)
	scalarSQL(t, err)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	datasets := root + "/datasets"
	ds := m9rDatasetCreate(t, h, datasets, token, "ADOPT", m9rDatasetWitness(t, h, strings.Split(old, "/entities/")[0], token))
	for i := 0; ds.State != "OPEN" && i < count/100+5; i++ {
		w := m5Call(h, datasets+"/"+ds.ID+"/verify", token, struct{}{})
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ds) != nil {
			t.Fatal("unique adoption", w.Code)
		}
	}
	if ds.State != "OPEN" {
		t.Fatal("unique adoption unfinished")
	}
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("unique document")
	}
	doc["schema_version"] = version
	defs := []any{}
	for _, item := range doc["definitions"].([]any) {
		d := item.(map[string]any)
		if d["kind"] == "rule" || d["kind"] == "workflow" {
			continue
		}
		d["schema_version"] = version
		if d["id"] == "field.contact.memo0" {
			d["field"].(map[string]any)["unique"] = true
		}
		defs = append(defs, d)
	}
	doc["definitions"] = defs
	raw, _ = json.Marshal(doc)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 2, raw); w.Code != 201 {
		t.Fatal("unique authoring", w.Code)
	}
	witness := m9rDatasetWitness(t, h, root+"/versions/3", token)
	witness["expected_binding"] = int64(1)
	if w := m5Call(h, datasets+"/"+ds.ID+"/bindings", token, witness); w.Code != 200 {
		t.Fatal("unique bind", w.Code)
	}
	base := datasets + "/" + ds.ID + "/bindings/2/entities/entity.contact"
	return f, h, base, token, app, scope
}
func uniqueStatus(t *testing.T, h http.Handler, path, token string) data.UniqueConstraint {
	t.Helper()
	w := m3Call(h, "GET", path, token, 0, nil)
	var x data.UniqueConstraint
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &x) != nil {
		t.Fatal("unique GET", w.Code)
	}
	return x
}
func uniqueStep(t *testing.T, h http.Handler, path, token, step string, x data.UniqueConstraint) data.UniqueConstraint {
	t.Helper()
	var body any = map[string]any{"expected_generation": x.Generation, "expected_checkpoint": x.Checkpoint}
	if step == "register" {
		body = map[string]any{"expected_generation": x.Generation}
	}
	w := m5Call(h, path+"/"+step, token, body)
	var next data.UniqueConstraint
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &next) != nil {
		t.Fatal("unique "+step, w.Code)
	}
	return next
}
func uniqueReady(t *testing.T, h http.Handler, path, token string) data.UniqueConstraint {
	t.Helper()
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	for i := 0; x.State != "READY" && i < 1000; i++ {
		x = uniqueStep(t, h, path, token, "build", x)
	}
	if x.State != "READY" {
		t.Fatal("unique build unfinished")
	}
	return x
}
func uniqueValues(value any) map[string]any {
	v := m5Values("Unique create")
	v["field.contact.memo0"] = value
	return v
}
func uniqueCreate(t *testing.T, h http.Handler, base, token string, value any, want int) data.Record {
	t.Helper()
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Key: uuid(t), Values: uniqueValues(value)})
	if w.Code != want {
		t.Fatal("unique create", w.Code, "want", want)
	}
	var r data.Record
	if want == 200 && json.Unmarshal(w.Body.Bytes(), &r) != nil {
		t.Fatal("unique response")
	}
	return r
}
func TestM9RUniqueLifecycleConflictsAndAtomicMaintenance(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, s := uniqueFixture(t, 1205, false)
	path := base + "/unique-constraints/field.contact.memo0"
	uniqueCreate(t, h, base, token, "before-registration", 409)
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	if x.State != "BUILDING" || x.Generation != 1 {
		t.Fatal("unique registration")
	}
	x = uniqueStep(t, h, path, token, "build", x)
	if x.ScannedRows != 1000 {
		t.Fatal("unique batch budget")
	}
	if again := uniqueStep(t, h, path, token, "build", data.UniqueConstraint{NumericIndex: data.NumericIndex{Checkpoint: 1}, Generation: 1}); again != x {
		t.Fatal("checkpoint replay")
	}
	uniqueCreate(t, h, base, token, "during-build", 409)
	for x.State != "READY" {
		x = uniqueStep(t, h, path, token, "build", x)
	}
	if x.ScannedRows != 1205 || x.VerifiedRows != 1205 {
		t.Fatal("unique independent coverage")
	}
	for _, value := range []any{nil, nil, "", "Alpha", "alpha", "é", "e\u0301", strings.Repeat("x", 3900)} {
		uniqueCreate(t, h, base, token, value, 200)
	}
	uniqueCreate(t, h, base, token, "", 409)
	uniqueCreate(t, h, base, token, "unique-1", 409)
	r := uniqueCreate(t, h, base, token, "reserved", 200)
	before := uniqueStatus(t, h, path, token)
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", Key: uuid(t), ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": "unique-1"}})
	if w.Code != 409 {
		t.Fatal("duplicate patch", w.Code)
	}
	if uniqueStatus(t, h, path, token) != before {
		t.Fatal("conflict advanced registry")
	}
	if w = m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": nil}}); w.Code != 200 {
		t.Fatal("clear unique", w.Code)
	}
	uniqueCreate(t, h, base, token, "reserved", 200)
	var valid bool
	scalarSQL(t, f.operator.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM runtime.records r LEFT JOIN runtime.unique_claims p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.id=r.id AND p.field_id='field.contact.memo0' AND p.generation=1 WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=$3 AND r.ir_hash=$4 AND (p.source_version IS DISTINCT FROM r.row_version OR p.value_key IS DISTINCT FROM runtime.unique_key(r.custom_data->'field.contact.memo0','string','',false,'[]')))`, f.tenantA, app, s.DataRevision(), s.DataHash()).Scan(&valid))
	if !valid {
		t.Fatal("source/claim oracle")
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM control.unique_write_context`).Scan(&n))
	if n != 0 {
		t.Fatal("permit not cleared")
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}
func TestM9RUniqueDuplicateBuildAbortAndGenerationFences(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, s := uniqueFixture(t, 3, true)
	path := base + "/unique-constraints/field.contact.memo0"
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	w := m5Call(h, path+"/build", token, map[string]any{"expected_generation": 1, "expected_checkpoint": 1})
	if w.Code != 409 {
		t.Fatal("duplicate backfill admitted", w.Code)
	}
	if got := uniqueStatus(t, h, path, token); got != x {
		t.Fatalf("failed backfill changed descriptor: expected=%+v actual=%+v", x, got)
	}
	x = uniqueStep(t, h, path, token, "abort", x)
	if x.State != "ABORTED" {
		t.Fatal("abort")
	}
	if repeat := uniqueStep(t, h, path, token, "abort", data.UniqueConstraint{NumericIndex: data.NumericIndex{Checkpoint: 1}, Generation: 1}); repeat != x {
		t.Fatal("abort replay")
	}
	if replay := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{}); replay != x {
		t.Fatal("lost registration restarted aborted attempt")
	}
	x = uniqueStep(t, h, path, token, "register", x)
	if x.Generation != 2 || x.State != "BUILDING" {
		t.Fatal("new attempt")
	}
	for _, step := range []string{"build", "abort"} {
		if w := m5Call(h, path+"/"+step, token, map[string]any{"expected_generation": 1, "expected_checkpoint": 1}); w.Code != 409 {
			t.Fatal("stale generation", step, w.Code)
		}
	}
	var count, history, claims int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4`, f.tenantA, app, s.DataRevision(), s.DataHash()).Scan(&count))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.unique_constraint_history WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&history))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.unique_claims WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&claims))
	if count != 3 || history != 1 || claims != 0 {
		t.Fatal("abort lost records/evidence or failed batch leaked claims", count, history, claims)
	}
}
func TestM9RUniqueConcurrentClaimsAndHiddenValueDenial(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 0, false)
	path := base + "/unique-constraints/field.contact.memo0"
	uniqueReady(t, h, path, token)
	codes := make(chan int, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Key: uuid(t), Values: uniqueValues("race")})
			codes <- w.Code
		}()
	}
	group.Wait()
	close(codes)
	seen := map[int]int{}
	for code := range codes {
		seen[code]++
	}
	if seen[200] != 1 || seen[409] != 1 {
		t.Fatal("claim arbitration", seen)
	}
	for _, engine := range []policy.Engine{scopedPolicy{mode: "owner"}, denyDataPolicy{"data.field.read", "field.contact.memo0"}} {
		other := m5Handler(t, f, engine, audit.Postgres{}, true)
		for _, value := range []string{"race", "fresh-hidden-probe"} {
			uniqueCreate(t, other, base, token, value, 403)
		}
	}
	var records, receipts, outbox int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&records))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.command_receipts WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&receipts))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.outbox_intents WHERE tenant_id=$1 AND app_id=$2 AND event_type='record.created'`, f.tenantA, app).Scan(&outbox))
	if records != 1 || receipts != 1 || outbox != 1 {
		t.Fatal("rejected claims persisted business facts", records, receipts, outbox)
	}
}
func TestM9RUniqueCanonicalTypedKeys(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	for _, test := range []struct{ typ, cur, a, b string }{
		{"string", "", `"é"`, `"é"`}, {"decimal", "", `"1.00"`, `"1"`}, {"money", "USD", `{"amount":"-0.00","currency":"USD"}`, `{"amount":"0","currency":"USD"}`}, {"integer", "", `-0`, `0`}, {"datetime", "", `"2024-01-01T00:00:00.0000004Z"`, `"2024-01-01T01:00:00+01:00"`}, {"bool", "", `true`, `true`}, {"date", "", `"2024-02-29"`, `"2024-02-29"`},
	} {
		t.Run(test.typ, func(t *testing.T) {
			var equal bool
			err := f.operator.QueryRow(`SELECT runtime.unique_key($1::jsonb,$3,$4,false,'[]')=runtime.unique_key($2::jsonb,$3,$4,false,'[]')`, test.a, test.b, test.typ, test.cur).Scan(&equal)
			scalarSQL(t, err)
			if equal != (test.typ != "string") {
				t.Fatal("canonical equality", test.typ)
			}
		})
	}
	for _, db := range []string{"framework_app", "framework_worker", "framework_dispatcher"} {
		var can bool
		scalarSQL(t, f.operator.QueryRow(`SELECT has_function_privilege($1,'runtime.unique_key(jsonb,text,text,boolean,jsonb)','EXECUTE') OR has_table_privilege($1,'runtime.unique_claims','SELECT,INSERT,UPDATE,DELETE')`, db).Scan(&can))
		if can {
			t.Fatal("private integrity material exposed", db)
		}
	}
}
