//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

func m9rDatasetWitness(t *testing.T, h http.Handler, descriptor, token string) map[string]any {
	t.Helper()
	w := m3Call(h, "GET", descriptor, token, 0, nil)
	if w.Code != 200 {
		t.Fatal("descriptor", w.Code, w.Body.String())
	}
	var d struct {
		Revision int64           `json:"revision"`
		Hash     string          `json:"ir_hash"`
		IR       json.RawMessage `json:"ir"`
	}
	if json.Unmarshal(w.Body.Bytes(), &d) != nil {
		t.Fatal("invalid descriptor")
	}
	return map[string]any{"metadata_revision": d.Revision, "ir_hash": d.Hash, "compiled_ir": d.IR}
}

func m9rDatasetCreate(t *testing.T, h http.Handler, path, token, mode string, witness map[string]any) data.Dataset {
	t.Helper()
	body := map[string]any{}
	for k, v := range witness {
		body[k] = v
	}
	body["workspace_id"], body["mode"] = uuid(t), mode
	w := m5Call(h, path, token, body)
	if w.Code != 200 {
		t.Fatal("create dataset", w.Code, w.Body.String())
	}
	var d data.Dataset
	if json.Unmarshal(w.Body.Bytes(), &d) != nil || d.ID == "" {
		t.Fatal("dataset identity")
	}
	return d
}

func TestM9RDatasetAdoptionAndReleaseContinuity(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	parent := m5Create(t, h, base, token, "Parent")
	values := m5Values("Child")
	values["field.contact.parent"] = parent.ID
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values})
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var child data.Record
	_ = json.Unmarshal(w.Body.Bytes(), &child)
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	team := uuid(t)
	tx, err := f.operator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Dataset fixture')`, f.tenantA, team); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE runtime.records SET owner_team_id=$1 WHERE tenant_id=$2 AND app_id=$3 AND id=$4`, team, f.tenantA, app, parent.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id,can_write) VALUES($1,$2,1,$3,'entity.contact',$4,$5,false)`, f.tenantA, app, witness["ir_hash"], parent.ID, f.viewer); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	d := m9rDatasetCreate(t, h, path, token, "ADOPT", witness)
	if d.State != "ADOPTING" {
		t.Fatal("unverified adoption opened")
	}
	bound := path + "/" + d.ID + "/bindings/1/entities/entity.contact"
	if w = m5Call(h, bound+"/query", token, m5Query()); w.Code != 409 {
		t.Fatal("unverified reads accepted", w.Code)
	}
	if w = m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 1, Values: m5Values("must stay sealed")}); w.Code != 409 {
		t.Fatal("legacy writer bypassed seal", w.Code)
	}
	w = m5Call(h, path+"/"+d.ID+"/verify", token, struct{}{})
	if w.Code != 200 {
		t.Fatal("adoption verify", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d.State != "OPEN" || d.VerifiedRows != 2 || d.ManifestHash == strings.Repeat("0", 64) {
		t.Fatal("verification evidence", d)
	}
	page := m5Page(t, m5Call(h, bound+"/query", token, m5Query()))
	if len(page.Records) != 2 {
		t.Fatal("adoption lost rows")
	}
	// The new source is a label-only edit. Its compiled hash/revision change,
	// while records, current owners/shares and causal sequence remain the same.
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	raw = []byte(strings.Replace(string(raw), `"label": "Contact"`, `"label": "Updated label"`, 1))
	if w = m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	newWitness := m9rDatasetWitness(t, h, strings.Replace(strings.Split(base, "/entities/")[0], "/versions/1", "/versions/2", 1), token)
	newWitness["expected_binding"] = 1
	w = m5Call(h, path+"/"+d.ID+"/bindings", token, newWitness)
	if w.Code != 200 {
		t.Fatal("bind", w.Code, w.Body.String())
	}
	if w = m5Call(h, path+"/"+d.ID+"/bindings", token, newWitness); w.Code != 409 {
		t.Fatal("stale binding CAS accepted", w.Code)
	}
	newBase := strings.Replace(bound, "/bindings/1/", "/bindings/2/", 1)
	page = m5Page(t, m5Call(h, newBase+"/query", token, m5Query()))
	if len(page.Records) != 2 {
		t.Fatal("release orphaned data")
	}
	if w = m5Call(h, bound+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 1, Values: m5Values("stale")}); w.Code != 409 {
		t.Fatal("retired binding wrote", w.Code)
	}
	w = m5Call(h, newBase+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 1, Values: m5Values("Changed")})
	if w.Code != 200 {
		t.Fatal("active binding write", w.Code, w.Body.String())
	}
	page = m5Page(t, m5Call(h, bound+"/query", token, m5Query()))
	seen := false
	for _, r := range page.Records {
		if r.ID == parent.ID {
			seen = r.Version == 2 && r.Values["field.contact.name"] == "Changed" && r.TeamID == team
		}
	}
	if !seen {
		t.Fatal("old immutable reader/current identity mismatch")
	}
	var count int
	var maxSequence int64
	if err := f.operator.QueryRow(`SELECT count(*),max(sequence) FROM control.event_deliveries WHERE tenant_id=$1 AND aggregate_key=jsonb_build_array('m8-v1',$2::text,1,$3::text,'entity.contact',$4::uuid)::text`, f.tenantA, app, witness["ir_hash"], parent.ID).Scan(&count, &maxSequence); err != nil || count != 2 || maxSequence != 2 {
		t.Fatal("causal lane reset", count, maxSequence, err)
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.record_shares WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND user_id=$4`, f.tenantA, app, parent.ID, f.viewer).Scan(&count); err != nil || count != 1 {
		t.Fatal("share lost", err)
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.record_links WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND target_id=$4`, f.tenantA, app, child.ID, parent.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("reference lost", err)
	}
	// Independent scratch previews and explicit EMPTY workspaces stay isolated.
	if page = m5Page(t, m5Call(h, strings.Replace(base, "/versions/1/", "/versions/2/", 1)+"/query", token, m5Query())); len(page.Records) != 0 {
		t.Fatal("implicit revision adoption")
	}
	delete(newWitness, "expected_binding")
	empty := m9rDatasetCreate(t, h, path, token, "EMPTY", newWitness)
	emptyBase := path + "/" + empty.ID + "/bindings/1/entities/entity.contact"
	if page = m5Page(t, m5Call(h, emptyBase+"/query", token, m5Query())); len(page.Records) != 0 {
		t.Fatal("empty workspace merged records")
	}
	m5Create(t, h, emptyBase, token, "Independent")
	cursorQuery := m5Query()
	cursorQuery.Limit = 1
	selected := m5Page(t, m5Call(h, newBase+"/query", token, cursorQuery))
	if selected.Next == "" {
		t.Fatal("cursor fixture did not paginate")
	}
	cursorQuery.After = selected.Next
	if w = m5Call(h, emptyBase+"/query", token, cursorQuery); w.Code != 400 {
		t.Fatal("cursor crossed explicit datasets", w.Code)
	}
	if page = m5Page(t, m5Call(h, newBase+"/query", token, m5Query())); len(page.Records) != 2 {
		t.Fatal("independent workspace affected adopted data")
	}
}

func TestM9RDatasetPopulatedTenantPolicyAndImmutableWitness(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "A")
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	viewer := f.token(t, f.viewerSubject, nil)
	body := map[string]any{"workspace_id": uuid(t), "mode": "ADOPT"}
	for k, v := range witness {
		body[k] = v
	}
	if w := m5Call(h, path, viewer, body); w.Code != 403 {
		t.Fatal("viewer managed dataset", w.Code)
	}
	body["ir_hash"] = strings.Repeat("f", 64)
	if w := m5Call(h, path, token, body); w.Code != 400 {
		t.Fatal("wrong witness accepted", w.Code)
	}
	body["ir_hash"] = witness["ir_hash"]
	body["tenant_id"] = f.tenantB
	if w := m5Call(h, path, token, body); w.Code != 400 {
		t.Fatal("untrusted tenant field accepted", w.Code)
	}
	d := m9rDatasetCreate(t, h, path, token, "ADOPT", witness)
	if w := m5Call(h, path+"/"+d.ID+"/verify", token, struct{}{}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	bob := f.token(t, f.bobSubject, nil)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	bBase := strings.Replace(base, f.tenantA, f.tenantB, 1)
	m5Create(t, h, bBase, bob, "B")
	bWitness := m9rDatasetWitness(t, h, strings.Split(bBase, "/entities/")[0], bob)
	bPath := strings.Replace(path, f.tenantA, f.tenantB, 1)
	b := m9rDatasetCreate(t, h, bPath, bob, "ADOPT", bWitness)
	if w := m5Call(h, bPath+"/"+b.ID+"/verify", bob, struct{}{}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, fixture := range []struct{ path, token, name string }{{path + "/" + d.ID + "/bindings/1/entities/entity.contact", token, "A"}, {bPath + "/" + b.ID + "/bindings/1/entities/entity.contact", bob, "B"}} {
		page := m5Page(t, m5Call(h, fixture.path+"/query", fixture.token, m5Query()))
		if len(page.Records) != 1 || page.Records[0].Values["field.contact.name"] != fixture.name {
			t.Fatal("legitimate populated audience missing")
		}
	}
	if w := m5Call(h, bPath+"/"+d.ID+"/bindings/1/entities/entity.contact/query", bob, m5Query()); w.Code != 404 {
		t.Fatal("foreign dataset appeared", w.Code)
	}
	if w := m5Call(h, path+"/"+d.ID+"/bindings/1/entities/entity.contact/query", bob, m5Query()); w.Code != 403 {
		t.Fatal("foreign tenant accepted", w.Code)
	}
	// Immutability is database enforced, including the migration credential.
	if _, err := f.operator.Exec(`UPDATE runtime.dataset_bindings SET ir_hash=ir_hash WHERE tenant_id=$1 AND dataset_id=$2`, f.tenantA, d.ID); err == nil {
		t.Fatal("stored binding mutable")
	}
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.datasets`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("unbound pooled context exposed datasets")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestM9RDatasetBoundedAdoptionResumeAndRawFence(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	first := m5Create(t, h, base, token, "Seed")
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	if _, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,owner_user_id,updated_by) SELECT $1,$2,1,$3,'entity.contact',('20000000-0000-4000-8000-'||lpad(i::text,12,'0'))::uuid,'{"field.contact.name":"fixture"}'::jsonb,$4,$4,$4 FROM generate_series(1,204) i`, f.tenantA, app, witness["ir_hash"], f.alice); err != nil {
		t.Fatal(err)
	}
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	d := m9rDatasetCreate(t, h, path, token, "ADOPT", witness)
	for step, want := range []int64{100, 200, 205} {
		w := m5Call(h, path+"/"+d.ID+"/verify", token, struct{}{})
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		_ = json.Unmarshal(w.Body.Bytes(), &d)
		if d.VerifiedRows != want || step < 2 && d.State != "ADOPTING" || step == 2 && d.State != "OPEN" {
			t.Fatal("checkpoint", d)
		}
	}
	before := d.ManifestHash
	w := m5Call(h, path+"/"+d.ID+"/verify", token, struct{}{})
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d.VerifiedRows != 205 || d.ManifestHash != before {
		t.Fatal("replayed verification duplicated rows")
	}
	// Raw app SQL with valid identity but no admitted binding must fail, while
	// existing source rows/versions remain intact after rollback.
	err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		p, er := f.verifier.Verify(token)
		if er != nil {
			return er
		}
		u, er := (identity.Store{}).Resolve(ctx, q, p)
		if er != nil {
			return er
		}
		ts := tenant.Store{}
		if er = ts.BindActor(ctx, q, u); er != nil {
			return er
		}
		if _, er = ts.Resolve(ctx, q, u, f.tenantA); er != nil {
			return er
		}
		_, er = q.ExecContext(ctx, `UPDATE runtime.records SET custom_data='{"field.contact.name":"bypass"}'::jsonb,row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, first.ID)
		return er
	})
	if err == nil {
		t.Fatal("raw legacy writer bypassed bound dataset")
	}
	var version int64
	var name string
	if err = f.operator.QueryRow(`SELECT row_version,custom_data->>'field.contact.name' FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, first.ID).Scan(&version, &name); err != nil || version != 1 || name != "Seed" {
		t.Fatal("raw failure changed source", err)
	}
	// Missing historical witness is blocked; no silent compilation or adoption.
	p, er := compiler.Restore(witness["compiled_ir"].(json.RawMessage), witness["ir_hash"].(string), compiler.SupportedIRVersions())
	if er != nil {
		t.Fatal(er)
	}
	var otherVersion int64
	if er = f.operator.QueryRow(`SELECT metadata_revision FROM runtime.dataset_bindings WHERE tenant_id=$1 AND dataset_id=$2`, f.tenantA, d.ID).Scan(&otherVersion); er != nil || otherVersion != 1 || p.Hash() != witness["ir_hash"] {
		t.Fatal("stored witness changed")
	}
	var rowCount int
	if er = f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&rowCount); er != nil || rowCount != 205 {
		t.Fatal("adoption copied/deleted data")
	}
}

func TestM9RDatasetStaleSnapshotCannotMissAdoptionFence(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "Before")
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	conn, err := f.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('statement_timeout','2000',true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = tx.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&before); err != nil || before != 1 {
		t.Fatal("populated stale snapshot", err)
	}
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	m9rDatasetCreate(t, h, path, token, "ADOPT", witness)
	_, err = tx.Exec(`UPDATE runtime.records SET custom_data='{"field.contact.name":"stale write"}'::jsonb WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "40001" {
		t.Fatal("stale snapshot did not hit the epoch fence", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	var name string
	if err = f.operator.QueryRow(`SELECT custom_data->>'field.contact.name' FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID).Scan(&name); err != nil || name != "Before" {
		t.Fatal("stale write altered retained source")
	}
}

func TestM9RDatasetUnknownCommitReconcilesWorkspaceAndCheckpoint(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "Retained")
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	body := map[string]any{"workspace_id": uuid(t), "mode": "ADOPT"}
	for k, v := range witness {
		body[k] = v
	}
	for _, operation := range []string{"create", "verify"} {
		t.Run(operation, func(t *testing.T) {
			proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
			db, err := postgres.Open(proxy)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if err = postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }); err != nil {
				t.Fatal("prime commit boundary", err)
			}
			a := access.API{DB: db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true}
			fault := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: a.Register, AllowPOST: a.AcceptsPOST})
			request := path
			var payload any = body
			if operation == "verify" {
				w := m5Call(h, path, token, body)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				var d data.Dataset
				_ = json.Unmarshal(w.Body.Bytes(), &d)
				request = path + "/" + d.ID + "/verify"
				payload = struct{}{}
			}
			w := m5Call(fault, request, token, payload)
			if !observed.Load() || w.Code != 503 {
				t.Fatal("lost COMMIT reply not executed", observed.Load(), w.Code)
			}
			w = m5Call(h, path, token, body)
			if w.Code != 200 {
				t.Fatal("workspace reconciliation", w.Code, w.Body.String())
			}
			var d data.Dataset
			_ = json.Unmarshal(w.Body.Bytes(), &d)
			var n int
			if err = f.operator.QueryRow(`SELECT count(*) FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2 AND workspace_id=$3`, f.tenantA, app, body["workspace_id"]).Scan(&n); err != nil || n != 1 {
				t.Fatal("unknown create duplicated dataset")
			}
			if operation == "verify" && (d.State != "OPEN" || d.VerifiedRows != 1) {
				t.Fatal("committed checkpoint not reconciled", d)
			}
		})
	}
}

func TestM9RDatasetCatalogContinuityAndUnsafeSchemaRejection(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "Retained")
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	d := m9rDatasetCreate(t, h, path, token, "ADOPT", witness)
	if w := m5Call(h, path+"/"+d.ID+"/verify", token, struct{}{}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	source, err := m3Registry(t).Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	catalog := compiler.PlatformCatalog()
	catalog.Version = "catalog-upgrade-fixture"
	p, ds := compiler.Compile(source, catalog)
	if !p.Valid() {
		t.Fatal(ds)
	}
	body := map[string]any{"metadata_revision": 1, "ir_hash": p.Hash(), "compiled_ir": p.JSON(), "expected_binding": 1}
	if p.Hash() == witness["ir_hash"] {
		t.Fatal("release hash fixture unchanged")
	}
	if w := m5Call(h, path+"/"+d.ID+"/bindings", token, body); w.Code != 400 {
		t.Fatal("caller-supplied catalog was treated as trusted compiler provenance", w.Code)
	}
	a := access.API{DB: f.db, Auth: f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(t), PreviewEnabled: true, CompilerCatalog: &catalog}
	upgraded := httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: a.Register, AllowPOST: a.AcceptsPOST})
	if w := m5Call(upgraded, path+"/"+d.ID+"/bindings", token, body); w.Code != 200 {
		t.Fatal("catalog upgrade orphaned data", w.Code, w.Body.String())
	}
	bound := path + "/" + d.ID + "/bindings/2/entities/entity.contact"
	page := m5Page(t, m5Call(h, bound+"/query", token, m5Query()))
	if len(page.Records) != 1 || page.Records[0].ID != record.ID || page.Records[0].Version != 1 {
		t.Fatal("catalog change lost identity/version")
	}
	// Restore returns original stored bytes: it never uses today's PlatformCatalog.
	w := m3Call(h, "GET", strings.Split(bound, "/entities/")[0], token, 0, nil)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var descriptor struct {
		Hash string          `json:"ir_hash"`
		IR   json.RawMessage `json:"ir"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &descriptor)
	if descriptor.Hash != p.Hash() || string(descriptor.IR) != string(p.JSON()) {
		t.Fatal("binding silently recompiled")
	}
	var doc metadata.Document
	_ = json.Unmarshal(raw, &doc)
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == "field.contact.balance" {
			doc.Definitions[i].Field.Currency = "EUR"
		}
	}
	raw, _ = json.Marshal(doc)
	if w = m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	changed := m9rDatasetWitness(t, h, strings.Replace(strings.Split(base, "/entities/")[0], "/versions/1", "/versions/2", 1), token)
	changed["expected_binding"] = 2
	if w = m5Call(h, path+"/"+d.ID+"/bindings", token, changed); w.Code != 400 {
		t.Fatal("unsupported currency migration activated", w.Code)
	}
	page = m5Page(t, m5Call(h, bound+"/query", token, m5Query()))
	if len(page.Records) != 1 || page.Records[0].Values["field.contact.balance"].(map[string]any)["currency"] != "USD" {
		t.Fatal("rejected migration changed records")
	}
}

func TestM9RDatasetAuditFailureRollsBackVerification(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "Retained")
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	path := "/v1/tenants/" + f.tenantA + "/preview/" + app + "/datasets"
	d := m9rDatasetCreate(t, h, path, token, "ADOPT", witness)
	fault := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
	if w := m5Call(fault, path+"/"+d.ID+"/verify", token, struct{}{}); w.Code != 503 {
		t.Fatal("failed checkpoint audit acknowledged", w.Code)
	}
	w := m3Call(h, "GET", path+"/"+d.ID, token, 0, nil)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	if d.State != "ADOPTING" || d.VerifiedRows != 0 || d.ManifestHash != strings.Repeat("0", 64) {
		t.Fatal("failed audit committed checkpoint", d)
	}
	if w = m5Call(h, path+"/"+d.ID+"/verify", token, struct{}{}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &d)
	var n int
	if err := f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='datasets.manage' AND state->>'dataset_id'=$2 AND state->>'manifest_hash'=$3 AND state->>'verified_rows'='1'`, f.tenantA, d.ID, d.ManifestHash).Scan(&n); err != nil || n != 1 {
		t.Fatal("checkpoint audit evidence missing or duplicated", n, err)
	}
	if w = m3Call(h, "GET", path+"/"+d.ID, f.token(t, f.viewerSubject, nil), 0, nil); w.Code != 403 {
		t.Fatal("viewer learned administrative verification counts", w.Code)
	}
}
