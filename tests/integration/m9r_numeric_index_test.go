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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func m9rIndex(t *testing.T, h http.Handler, path, token string, index data.NumericIndex, op string) data.NumericIndex {
	t.Helper()
	var wbody any = struct{}{}
	if op == "build" {
		wbody = map[string]any{"expected_checkpoint": index.Checkpoint}
	}
	w := m5Call(h, path+"/"+op, token, wbody)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &index) != nil {
		status := m3Call(h, "GET", path, token, 0, nil)
		var current data.NumericIndex
		if status.Code == 200 && json.Unmarshal(status.Body.Bytes(), &current) == nil {
			t.Logf("failed index checkpoint state=%s checkpoint=%d copied=%d verified=%d", current.State, current.Checkpoint, current.ScannedRows, current.VerifiedRows)
		}
		t.Fatal("index operation", op, w.Code, w.Body.String())
	}
	return index
}

func m9rIndexReady(t *testing.T, h http.Handler, path, token string) data.NumericIndex {
	t.Helper()
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	for n := 0; index.State != "READY" && n < 1001; n++ {
		index = m9rIndex(t, h, path, token, index, "build")
	}
	if index.State != "READY" {
		t.Fatal("index did not complete")
	}
	return index
}

func m9rNumericSeed(t *testing.T, f *iamFixture, app string, count int) string {
	t.Helper()
	var hash string
	if err := f.operator.QueryRow(`SELECT ir_hash FROM runtime.record_sets WHERE tenant_id=$1 AND app_id=$2 LIMIT 1`, f.tenantA, app).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,1,$3,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name','Index seed '||i,'field.contact.status',CASE WHEN i%2=0 THEN 'active' ELSE 'inactive' END,'field.contact.balance',CASE WHEN i%41=0 THEN 'null'::jsonb ELSE jsonb_build_object('amount',(i/7)::text,'currency','USD') END),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int) i`, f.tenantA, app, hash, f.alice, f.viewer, count)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func m9rAllPages(t *testing.T, h http.Handler, base, token string, desc bool) []data.Record {
	t.Helper()
	query := m5Query()
	query.Limit = 50
	query.Sort = &data.Sort{Field: "field.contact.balance", Desc: desc}
	var records []data.Record
	for n := 0; n < 100; n++ {
		page := m5Page(t, m5Call(h, base+"/query", token, query))
		records = append(records, page.Records...)
		if page.Next == "" {
			return records
		}
		query.After = page.Next
	}
	t.Fatal("pagination did not terminate")
	return nil
}

func TestM9RNumericIndexOrderingResumeAndMaintenance(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	original := m5Create(t, h, base, token, "Original")
	hash := m9rNumericSeed(t, f, app, 1205)
	asc, desc := m9rAllPages(t, h, base, token, false), m9rAllPages(t, h, base, token, true)
	path := base + "/indexes/field.contact.balance"
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	if index.State != "BUILDING" || index.Checkpoint != 1 {
		t.Fatal("initial checkpoint")
	}
	if !reflect.DeepEqual(asc, m9rAllPages(t, h, base, token, false)) {
		t.Fatal("building index changed fallback semantics")
	}
	index = m9rIndex(t, h, path, token, index, "build")
	if index.ScannedRows != 1000 || index.State != "BUILDING" {
		t.Fatal("bounded first batch")
	}
	// Replay a lost response at checkpoint 1: no second batch or cursor skip.
	replayed := m9rIndex(t, h, path, token, data.NumericIndex{Checkpoint: 1}, "build")
	if replayed != index {
		t.Fatal("checkpoint reconciliation advanced twice")
	}
	index = m9rIndex(t, h, path, token, index, "build")
	if index.State != "VERIFYING" || index.ScannedRows != 1206 {
		t.Fatal("copy completion", index)
	}
	index = m9rIndex(t, h, path, token, index, "build")
	index = m9rIndex(t, h, path, token, index, "build")
	if index.State != "READY" || index.ScannedRows != 1206 || index.VerifiedRows != 1206 {
		t.Fatal("final coverage", index)
	}
	if !reflect.DeepEqual(asc, m9rAllPages(t, h, base, token, false)) || !reflect.DeepEqual(desc, m9rAllPages(t, h, base, token, true)) {
		t.Fatal("indexed ASC/DESC/null/tie/cursor semantics differ")
	}
	// Raw app INSERT/UPDATE use the database trigger too, not just Go commands.
	values := m5Values("Updated")
	values["field.contact.balance"] = map[string]any{"amount": "-42.000000000000000001", "currency": "USD"}
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: original.ID, ExpectedVersion: 1, Values: values})
	if w.Code != 200 {
		t.Fatal("indexed update", w.Code, w.Body.String())
	}
	var number string
	var version int64
	if err := f.operator.QueryRow(`SELECT value_number::text,source_version FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, original.ID).Scan(&number, &version); err != nil || number != "-42.000000000000000001" || version != 2 {
		t.Fatal("exact projection update", number, version, err)
	}
	w = m5Call(h, base+"/commands", token, data.Command{Operation: "delete", ID: original.ID, ExpectedVersion: 2})
	if w.Code != 200 {
		t.Fatal("indexed delete", w.Code)
	}
	var remaining int
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, original.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("delete resurrection", err)
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.numeric_projections p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.id=r.id AND p.field_id='field.contact.balance' WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.ir_hash=$3 AND (p.id IS NULL OR p.source_version<>r.row_version)`, f.tenantA, app, hash).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("coverage preserved", remaining, err)
	}
}

func TestM9RNumericIndexPermissionsAndWitnesses(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "Index protected")
	path := base + "/indexes/field.contact.balance"
	for _, id := range []string{"field.contact.name", "field.contact.id", "field.contact.missing"} {
		w := m5Call(h, base+"/indexes/"+id+"/register", token, struct{}{})
		if w.Code != 400 {
			t.Fatal("unsupported field", id, w.Code)
		}
	}
	for _, subject := range []string{f.viewerSubject, f.bobSubject} {
		if w := m5Call(h, path+"/register", f.token(t, subject, nil), struct{}{}); w.Code != 403 {
			t.Fatal("manager/foreign tenant denial", w.Code)
		}
	}
	restricted := m5Handler(t, f, denyDataPolicy{"data.field.read", "field.contact.balance"}, audit.Postgres{}, true)
	if w := m5Call(restricted, path+"/register", token, struct{}{}); w.Code != 403 {
		t.Fatal("field policy denial", w.Code)
	}
	m9rIndexReady(t, h, path, token)
	principal, _ := f.verifier.Verify(token)
	for _, query := range []string{
		`UPDATE runtime.numeric_projections SET value_number=0 WHERE tenant_id=$1`,
		`DELETE FROM runtime.numeric_projections WHERE tenant_id=$1`,
		`UPDATE runtime.numeric_indexes SET state='READY' WHERE tenant_id=$1`,
		`ALTER TABLE runtime.numeric_projections ADD COLUMN forged text`,
	} {
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, e := m3Scope(f, ctx, q, principal, f.tenantA); e != nil {
				return e
			}
			if strings.Contains(query, "$1") {
				_, e := q.ExecContext(ctx, query, f.tenantA)
				return e
			}
			_, e := q.ExecContext(ctx, query)
			return e
		})
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatal("projection forgery/DDL must fail", err)
		}
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	for _, table := range []string{"numeric_indexes", "numeric_projections"} {
		var n int
		err = worker.QueryRow(`SELECT count(*) FROM runtime.` + table).Scan(&n)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatal("worker cannot discover indexes", err)
		}
		if _, err = f.operator.Exec(`GRANT SELECT(field_id) ON runtime.` + table + ` TO framework_worker`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = f.operator.Exec(`REVOKE SELECT(field_id) ON runtime.` + table + ` FROM framework_worker`)
		})
		var safe bool
		if err = worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe); err != nil || safe {
			t.Fatal("new-table column grant drift", table, err)
		}
		rows, err := worker.Query(`SELECT field_id FROM runtime.` + table)
		if err != nil {
			t.Fatal(err)
		}
		visible := rows.Next()
		rowErr := rows.Err()
		_ = rows.Close()
		if visible || rowErr != nil {
			t.Fatal("worker restrictive RLS", table, rowErr)
		}
		if _, err = f.operator.Exec(`REVOKE SELECT(field_id) ON runtime.` + table + ` FROM framework_worker`); err != nil {
			t.Fatal(err)
		}
	}
	viewerPrincipal, _ := f.verifier.Verify(f.token(t, f.viewerSubject, nil))
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		access, e := m3Scope(f, ctx, q, viewerPrincipal, f.tenantA)
		if e != nil {
			return e
		}
		var hash, schema string
		if e = q.QueryRowContext(ctx, `SELECT ir_hash,schema_hash FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2`, access.TenantID(), app).Scan(&hash, &schema); e != nil {
			return e
		}
		_, e = q.ExecContext(ctx, `SELECT runtime.register_numeric_index($1,$2,1,$3,'entity.contact','field.contact.balance',$4,1,$3)`, access.TenantID(), app, hash, schema)
		return e
	})
	var managerError *pgconn.PgError
	if !errors.As(err, &managerError) || managerError.Code != "42501" {
		t.Fatal("direct SQL manager bypass", err)
	}
	// A claimed type cannot override the stored source field. The SQL port has
	// no type parameter at all. A forged schema witness also fails Go admission.
	if _, err = f.operator.Exec(`UPDATE runtime.numeric_indexes SET schema_hash=repeat('0',64) WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app); err != nil {
		t.Fatal(err)
	}
	query := m5Query()
	query.Sort = &data.Sort{Field: "field.contact.balance"}
	if w := m5Call(h, base+"/query", token, query); w.Code != 503 {
		t.Fatal("witness mismatch fail closed", w.Code)
	}
}

func TestM9RNumericIndexAuditAtomicity(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "Atomic index")
	path := base + "/indexes/field.contact.balance"
	fault := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
	if w := m5Call(fault, path+"/register", token, struct{}{}); w.Code != 503 {
		t.Fatal("audit failure not propagated", w.Code)
	}
	var count int
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&count); err != nil || count != 0 {
		t.Fatal("registration survived audit rollback", count, err)
	}
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	if w := m5Call(fault, path+"/build", token, map[string]any{"expected_checkpoint": index.Checkpoint}); w.Code != 503 {
		t.Fatal("build audit failure", w.Code)
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&count); err != nil || count != 0 {
		t.Fatal("backfill survived audit rollback", count, err)
	}
	w := m3Call(h, "GET", path, token, 0, nil)
	var actual data.NumericIndex
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &actual) != nil || actual != index {
		t.Fatal("checkpoint advanced after rollback", w.Code, actual, index)
	}
}

func TestM9RNumericIndexStaleWriterAndCoverageFailure(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "Fence")
	m9rNumericSeed(t, f, app, 1205)
	principal, _ := f.verifier.Verify(token)
	tx, err := f.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = m3Scope(f, context.Background(), tx, principal, f.tenantA); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = tx.QueryRow(`SELECT count(*) FROM runtime.numeric_indexes WHERE tenant_id=$1`, f.tenantA).Scan(&before); err != nil {
		t.Fatal(err)
	}
	path := base + "/indexes/field.contact.balance"
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	_, err = tx.Exec(`UPDATE runtime.records SET custom_data=custom_data,row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "40001" {
		t.Fatal("older RR writer missed registration", err)
	}
	_ = tx.Rollback()
	index = m9rIndex(t, h, path, token, index, "build")
	// Corrupt a completed prefix as the operator; final activation must notice.
	if _, err = f.operator.Exec(`DELETE FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND id=(SELECT after_id FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2)`, f.tenantA, app); err != nil {
		t.Fatal(err)
	}
	index = m9rIndex(t, h, path, token, index, "build")
	if index.State != "VERIFYING" {
		t.Fatal("must verify before Ready", index)
	}
	w := m5Call(h, path+"/build", token, map[string]any{"expected_checkpoint": index.Checkpoint})
	if w.Code != 409 {
		t.Fatal("partial coverage became ready", w.Code, w.Body.String())
	}
	w = m3Call(h, "GET", path, token, 0, nil)
	var actual data.NumericIndex
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &actual) != nil || actual != index {
		t.Fatal("failed batch/checkpoint must roll back", w.Code, actual, index)
	}
}

func TestM9RNumericIndexCompatibleDatasetBinding(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	original := m5Create(t, h, base, token, "Adopt index")
	legacyPath := base + "/indexes/field.contact.balance"
	index := m9rIndexReady(t, h, legacyPath, token)
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	d := m9rDatasetCreate(t, h, root, token, "ADOPT", witness)
	for d.State != "OPEN" {
		w := m5Call(h, root+"/"+d.ID+"/verify", token, struct{}{})
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatal("adoption", w.Code)
		}
	}
	bound := fmt.Sprintf("%s/%s/bindings/1/entities/entity.contact", root, d.ID)
	actual := m9rIndexReady(t, h, bound+"/indexes/field.contact.balance", token)
	if actual != index {
		t.Fatal("adoption changed existing index identity")
	}
	query := m5Query()
	query.Sort = &data.Sort{Field: "field.contact.balance"}
	page := m5Page(t, m5Call(h, bound+"/query", token, query))
	if len(page.Records) != 1 || page.Records[0].ID != original.ID {
		t.Fatal("bound index lost records")
	}
}

func TestM9RNumericIndexBuildConcurrentWriter(t *testing.T) {
	t.Run("update", func(t *testing.T) { m9rNumericConcurrent(t, false) })
	t.Run("delete", func(t *testing.T) { m9rNumericConcurrent(t, true) })
}

func m9rNumericConcurrent(t *testing.T, deletion bool) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "Concurrent projection")
	path := base + "/indexes/field.contact.balance"
	m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	principal, _ := f.verifier.Verify(token)
	ctx := context.Background()
	writer, err := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err = m3Scope(f, ctx, writer, principal, f.tenantA); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = writer.QueryRow(`SELECT id::text FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 ORDER BY id LIMIT 1 FOR UPDATE`, f.tenantA, app).Scan(&id); err != nil {
		t.Fatal(err)
	}
	build, err := f.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(); _ = build.Rollback() }()
	if _, err = m3Scope(f, ctx, build, principal, f.tenantA); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err = build.QueryRow(`SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var hash, schema string
	if err = f.operator.QueryRow(`SELECT ir_hash,schema_hash FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&hash, &schema); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, e := build.Exec(`SELECT runtime.build_numeric_index($1,$2,1,$3,'entity.contact','field.contact.balance',$4,1,$3,1)`, f.tenantA, app, hash, schema)
		done <- e
	}()
	// Observe only this owned backend's wait state, never SQL text/credentials.
	deadline := time.Now().Add(2 * time.Second)
	for {
		var waiting bool
		if err = f.operator.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_locks WHERE pid=$1 AND NOT granted)`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("builder never reached owned record lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if deletion {
		_, err = writer.Exec(`DELETE FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, id)
	} else {
		_, err = writer.Exec(`UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.balance,amount}','"99.000000000000000001"'),row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, id)
	}
	if err != nil {
		_ = writer.Rollback()
		<-done
		t.Fatal("writer blocked by build registry/FK lock", err)
	}
	if err = writer.Commit(); err != nil {
		t.Fatal(err)
	}
	// The older builder snapshot must abort, rather than overwrite version 2.
	err = <-done
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "40001" {
		t.Fatal("stale backfill must abort", err)
	}
	_ = build.Rollback()
	m9rIndexReady(t, h, path, token)
	var amount string
	var version int64
	err = f.operator.QueryRow(`SELECT value_number::text,source_version FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, id).Scan(&amount, &version)
	if deletion {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("deleted row resurrected", err)
		}
		return
	}
	if err != nil || amount != "99.000000000000000001" || version != 2 {
		t.Fatal("newer write overwritten", amount, version, err)
	}
}

func TestM9RNumericIndexTypedValuesAndScopes(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc metadata.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("source")
	}
	doc.AppID = app
	for _, spec := range []struct{ id, typ string }{{"field.contact.score", "integer"}, {"field.contact.distance", "decimal"}} {
		for i := range doc.Definitions {
			if doc.Definitions[i].ID == "entity.contact" {
				doc.Definitions[i].Entity.Fields = append(doc.Definitions[i].Entity.Fields, spec.id)
			}
		}
		doc.Definitions = append(doc.Definitions, metadata.Definition{ID: spec.id, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Kind: "field", Label: spec.id, Field: &metadata.Field{EntityRef: "entity.contact", Type: spec.typ}})
	}
	raw, _ = json.Marshal(doc)
	w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw)
	if w.Code != 201 {
		t.Fatal("typed source", w.Code, w.Body.String())
	}
	base = strings.Replace(base, "/versions/1/", "/versions/2/", 1)
	for i := range 9 {
		v := m5Values("Owned typed")
		v["field.contact.score"] = i - 4
		v["field.contact.distance"] = fmt.Sprintf("%d.000000000000000001", i-4)
		if w = m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}); w.Code != 200 {
			t.Fatal("typed create", w.Code, w.Body.String())
		}
	}
	for _, id := range []string{"field.contact.score", "field.contact.distance"} {
		query := m5Query()
		query.Projection = append(query.Projection, id)
		query.Sort = &data.Sort{Field: id}
		query.Limit = 100
		before := m5Page(t, m5Call(h, base+"/query", token, query))
		m9rIndexReady(t, h, base+"/indexes/"+id, token)
		after := m5Page(t, m5Call(h, base+"/query", token, query))
		if !reflect.DeepEqual(before, after) {
			t.Fatal("typed numeric semantics", id)
		}
	}
	for _, check := range []struct {
		raw, typ, cur string
		req, valid    bool
	}{
		{`9007199254740991`, "integer", "", true, true}, {`-9007199254740991`, "integer", "", true, true},
		{`1.0`, "integer", "", false, false}, {`9007199254740992`, "integer", "", false, false},
		{`"1000000000000000000"`, "decimal", "", true, true}, {`"1000000000000000000.1"`, "decimal", "", false, false},
		{`"0.1234567890123456789"`, "decimal", "", false, false}, {`"01"`, "decimal", "", false, false},
		{`null`, "decimal", "", false, true}, {`null`, "decimal", "", true, false},
		{`{"amount":"-0.000000000000000001","currency":"USD"}`, "money", "USD", true, true},
		{`{"amount":"1","currency":"EUR"}`, "money", "USD", true, false},
		{`{"amount":"1","currency":"USD","extra":1}`, "money", "USD", true, false},
	} {
		var result sql.NullString
		err = f.operator.QueryRow(`SELECT runtime.numeric_value($1::jsonb,$2,$3,$4)::text`, check.raw, check.typ, check.cur, check.req).Scan(&result)
		var pg *pgconn.PgError
		if check.valid && err != nil || !check.valid && (!errors.As(err, &pg) || pg.Code != "22023") {
			t.Fatal("SQL typed numeric validation", check.raw, err)
		}
	}
}

func TestM9RNumericIndexDataScopeEquivalence(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	original := m5Create(t, h, base, token, "Owned original")
	m9rNumericSeed(t, f, app, 77)
	if _, err := f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$3 FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND custom_data->>'field.contact.status'='inactive'`, f.tenantA, app, f.alice); err != nil {
		t.Fatal(err)
	}
	// This 78-row equivalence fixture exercises the production SQL-scope kernel.
	// Opaque Go callbacks have a separate, explicit 32-header admission contract.
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	r5Assign(t, h, root, token, f.viewer, "owner", 200)
	admin := f.token(t, f.viewerSubject, nil)
	var version int64
	setMode := func(mode string) {
		scope := policy.DataScope{Kind: "ALL"}
		switch mode {
		case "owner":
			scope.Kind = "OWNER"
		case "share":
			scope.Kind = "SHARE"
		case "condition":
			scope = policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.name", Op: "contains", Value: json.RawMessage(`"Owned"`)}}
		}
		definition := r5Definition("scope_reader", app, scope)
		if mode == "field_none" {
			definition.Grants = append(definition.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.name"}})
		}
		v := r5Publish(t, h, root+"/roles", admin, version, definition, true, uuid(t))
		version = v.Version
		if version == 1 {
			r5Assign(t, h, root, admin, f.alice, definition.Name, 200)
		}
	}
	modes := []string{"owner", "share", "condition", "field_none"}
	// Equivalence alone can pass if both plans incorrectly return nothing or
	// both expose every tenant row. Demand the exact populated policy result.
	wantRows := map[string]int{"owner": 39, "share": 39, "condition": 1, "field_none": 78}
	assertScope := func(mode string, records []data.Record) {
		t.Helper()
		if len(records) != wantRows[mode] {
			t.Fatal("populated scope cardinality", mode, len(records), wantRows[mode])
		}
		for _, record := range records {
			if mode == "owner" && record.OwnerID != f.alice {
				t.Fatal("owner scope exposed foreign-owned row")
			}
			if mode == "share" && (record.OwnerID != f.viewer || record.ID == original.ID) {
				t.Fatal("share scope exposed an unshared owned row")
			}
			if mode == "condition" && record.ID != original.ID {
				t.Fatal("condition scope exposed a nonmatching populated row")
			}
			_, hasName := record.Values["field.contact.name"]
			maskedName := record.FieldAccess != nil && slices.Contains(record.FieldAccess.Masked, "field.contact.name")
			if mode == "field_none" && (hasName || !maskedName) || mode != "field_none" && (!hasName || maskedName) {
				t.Fatal("scope field visibility", mode)
			}
		}
	}
	before := map[string][]data.Record{}
	for _, mode := range modes {
		setMode(mode)
		before[mode] = m9rAllPages(t, h, base, token, false)
		assertScope(mode, before[mode])
	}
	m9rIndexReady(t, h, base+"/indexes/field.contact.balance", admin)
	for _, mode := range modes {
		setMode(mode)
		after := m9rAllPages(t, h, base, token, false)
		assertScope(mode, after)
		if !reflect.DeepEqual(before[mode], after) {
			t.Fatal("scope changed before LIMIT", mode)
		}
	}
}
