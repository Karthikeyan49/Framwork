//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Independent probes use the existing effectively targeted disposable fixtures.
// They never alter production source or the already-applied migration bytes.
func uniqueReviewBinding(base string) (string, string) {
	parts := strings.Split(base, "/")
	return parts[7], strings.Join(parts[:8], "/")
}

func uniqueReviewTransaction(t *testing.T, f *iamFixture, base, token string, run func(context.Context, postgres.Executor, tenant.Access, data.Scope) error) error {
	t.Helper()
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	ds, _ := uniqueReviewBinding(base)
	var app string
	scalarSQL(t, f.operator.QueryRow(`SELECT app_id FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, ds).Scan(&app))
	return postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		a, err := m3Scope(f, ctx, q, principal, f.tenantA)
		if err != nil {
			return err
		}
		var version int64
		if err = q.QueryRowContext(ctx, `SELECT active_binding FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, ds).Scan(&version); err != nil {
			return err
		}
		s, err := (data.DatasetStore{}).Scope(ctx, q, a, app, ds, version)
		if err != nil {
			return err
		}
		if err = data.AdmitStart(ctx, q, s, a); err != nil {
			return err
		}
		return run(ctx, q, a, s)
	})
}

func uniqueReviewSource(t *testing.T, f *iamFixture, h http.Handler, token, app string, baseVersion int64, sourceRevision int64) string {
	t.Helper()
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=$3`, f.tenantA, app, sourceRevision).Scan(&raw))
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, baseVersion, raw); w.Code != 201 {
		t.Fatal("review compatible source", w.Code)
	}
	return root + "/versions/" + stringInt(baseVersion+1)
}

func stringInt(n int64) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

func uniqueReviewBind(t *testing.T, h http.Handler, base, token, revisionPath string, expected int64) string {
	t.Helper()
	_, root := uniqueReviewBinding(base)
	w := m9rDatasetWitness(t, h, revisionPath, token)
	w["expected_binding"] = expected
	if response := m5Call(h, root+"/bindings", token, w); response.Code != 200 {
		t.Fatal("review compatible bind", response.Code)
	}
	return root + "/bindings/" + stringInt(expected+1) + "/entities/entity.contact"
}

func TestM9RUniqueReviewRawClaimsAndRoleIsolation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 5, false)
	scalarReviewTarget(t, f)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	rows := choicePages(t, h, base, token, m5Query())
	if len(rows) != 5 {
		t.Fatal("review populated raw fixture")
	}
	rid := rows[0].ID
	var duplicate string
	scalarSQL(t, f.operator.QueryRow(`SELECT custom_data->>'field.contact.memo0' FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id<>$3 LIMIT 1`, f.tenantA, app, rid).Scan(&duplicate))
	for _, value := range []string{duplicate, "new-raw-key"} {
		err := uniqueReviewTransaction(t, f, base, token, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
			_, err := q.ExecContext(ctx, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.memo0}',to_jsonb($3::text)),row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, a.TenantID(), rid, value)
			return err
		})
		nativePGCode(t, err, "42501")
	}
	scalarSQL(t, uniqueReviewTransaction(t, f, base, token, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
		_, err := q.ExecContext(ctx, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.name}','"Unrelated legacy change"'),row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, a.TenantID(), rid)
		return err
	}))
	// A valid permit cannot be moved to another record or another canonical key.
	err := uniqueReviewTransaction(t, f, base, token, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
		if _, err := q.ExecContext(ctx, `SELECT runtime.authorize_unique_write($1,$2,$3,$4,'entity.contact',$5,'{"field.contact.memo0":"permitted-key"}')`, a.TenantID(), app, s.DataRevision(), s.DataHash(), rid); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.memo0}','"different-key"'),row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, a.TenantID(), rid)
		return err
	})
	nativePGCode(t, err, "42501")
	for _, binding := range []string{"DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db, err := postgres.Open(os.Getenv(binding))
		scalarSQL(t, err)
		var database, role string
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&database, &role))
		if !strings.HasPrefix(database, "m8_delivery_") || role != map[string]string{"DATABASE_URL": "framework_app", "WORKER_DATABASE_URL": "framework_worker", "DISPATCHER_DATABASE_URL": "framework_dispatcher"}[binding] {
			db.Close()
			t.Fatal("review role target mismatch")
		}
		var n int
		err = db.QueryRow(`SELECT count(*) FROM runtime.unique_claims`).Scan(&n)
		nativePGCode(t, err, "42501")
		if binding != "DATABASE_URL" {
			_, err = db.Exec(`SELECT runtime.authorize_unique_write($1,$2,2,repeat('0',64),'entity.contact',$3,'{"field.contact.memo0":"x"}')`, f.tenantA, app, rid)
			nativePGCode(t, err, "42501")
		}
		db.Close()
	}
	var sourceVersion, claimVersion int64
	scalarSQL(t, f.operator.QueryRow(`SELECT r.row_version,p.source_version FROM runtime.records r JOIN runtime.unique_claims p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.id=r.id WHERE r.tenant_id=$1 AND r.id=$2`, f.tenantA, rid).Scan(&sourceVersion, &claimVersion))
	if sourceVersion != 2 || claimVersion != 2 {
		t.Fatal("review raw denials changed source or claim")
	}
}

func TestM9RUniqueReviewActualUnknownCommitReconciliation(t *testing.T) {
	m8Database(t)
	f, h, base, token, _, _ := uniqueFixture(t, 17, false)
	scalarReviewTarget(t, f)
	path := base + "/unique-constraints/field.contact.memo0"
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
	db, err := postgres.Open(proxy)
	scalarSQL(t, err)
	defer db.Close()
	clone := *f
	clone.db = db
	scalarReviewTarget(t, &clone)
	scalarSQL(t, postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }))
	faulted := m5Handler(t, &clone, policy.Native{}, audit.Postgres{}, true)
	w := m5Call(faulted, path+"/build", token, map[string]any{"expected_generation": x.Generation, "expected_checkpoint": x.Checkpoint})
	if w.Code != 503 || !observed.Load() {
		t.Fatal("review actual lost COMMIT not observed", w.Code)
	}
	got := uniqueStatus(t, h, path, token)
	if got.Checkpoint != x.Checkpoint+1 || got.ScannedRows != 17 || got.State != "VERIFYING" {
		t.Fatal("review committed build did not reconcile")
	}
	if replay := uniqueStep(t, h, path, token, "build", x); replay != got {
		t.Fatal("review retry repeated committed batch")
	}
	uniqueReady(t, h, path, token)
}

func TestM9RUniqueReviewAbortRepairCompatibleBindingAndEvidence(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 3, true)
	scalarReviewTarget(t, f)
	path := base + "/unique-constraints/field.contact.memo0"
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	if w := m5Call(h, path+"/build", token, map[string]any{"expected_generation": 1, "expected_checkpoint": 1}); w.Code != 409 {
		t.Fatal("review duplicate fixture", w.Code)
	}
	x = uniqueStep(t, h, path, token, "abort", x)
	uniqueCreate(t, h, base, token, "while-aborted", 409)
	plainSource := uniqueReviewSource(t, f, h, token, app, 3, 2)
	plain := uniqueReviewBind(t, h, base, token, plainSource, 2)
	rows := choicePages(t, h, plain, token, m5Query())
	if len(rows) != 3 {
		t.Fatal("review repair lost source")
	}
	for i, r := range rows {
		if w := m5Call(h, plain+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: r.Version, Values: map[string]any{"field.contact.memo0": "repaired-" + stringInt(int64(i))}}); w.Code != 200 {
			t.Fatal("review authorized repair", w.Code)
		}
	}
	declared := uniqueReviewSource(t, f, h, token, app, 4, 3)
	next := uniqueReviewBind(t, h, plain, token, declared, 3)
	path = next + "/unique-constraints/field.contact.memo0"
	x = uniqueStep(t, h, path, token, "register", x)
	if x.Generation != 2 {
		t.Fatal("review repair generation")
	}
	for x.State != "READY" {
		x = uniqueStep(t, h, path, token, "build", x)
	}
	uniqueCreate(t, h, next, token, "repaired-0", 409)
	uniqueCreate(t, h, next, token, "after-repair", 200)
	var count, history int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&count))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.unique_constraint_history WHERE tenant_id=$1 AND app_id=$2 AND generation=1`, f.tenantA, app).Scan(&history))
	if count != 4 || history != 1 {
		t.Fatal("review repair lost records/evidence")
	}
	_, err := f.operator.Exec(`UPDATE runtime.unique_constraint_history SET snapshot='{}' WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	if err == nil {
		t.Fatal("review abort evidence mutable")
	}
}

func TestM9RUniqueReviewSameCommandKeyPreservesReceiptRetry(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 0, false)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	command := data.Command{Key: uuid(t), Operation: "create", Values: uniqueValues(nil)}
	start := make(chan struct{})
	results := make(chan *httpResult, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			w := m5Call(h, base+"/commands", token, command)
			results <- &httpResult{w.Code, w.Body.Bytes()}
		}()
	}
	close(start)
	group.Wait()
	close(results)
	var id string
	for w := range results {
		var r data.Record
		if w.status != 200 || json.Unmarshal(w.body, &r) != nil {
			t.Fatal("review same-key command", w.status)
		}
		if id == "" {
			id = r.ID
		} else if id != r.ID {
			t.Fatal("review same-key identity changed")
		}
	}
	m9rCommandCounts(t, f, app, 1, 1, 1)
}

func TestM9RUniqueReviewLiveDefinedRoleScopesAndRevocation(t *testing.T) {
	m8Database(t)
	f, h, base, owner, app, _ := uniqueFixture(t, 0, false)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", owner)
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	all := policy.DataScope{Kind: "ALL"}
	d := r5Definition("unique_editor", app, all)
	d.Grants = append(d.Grants,
		policy.Grant{Effect: "ALLOW", Actions: []string{"preview.write", "commands.reconcile"}, Resource: policy.Selector{AppID: app, EntityID: "*", FieldID: "*"}},
		policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.write", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}, Scope: &all},
		policy.Grant{Effect: "ALLOW", Actions: []string{"data.row.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}},
	)
	v := r5Publish(t, h, root+"/roles", owner, 0, d, true, uuid(t))
	r5Assign(t, h, root, owner, f.viewer, d.Name, 200)
	user := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}, "tenant_id": f.tenantB})
	uniqueCreate(t, h, base, user, "custom-role-key", 200)
	uniqueCreate(t, h, base, user, "custom-role-key", 409)
	for i := range d.Grants {
		if d.Grants[i].Scope != nil {
			d.Grants[i].Scope = &policy.DataScope{Kind: "OWNER"}
		}
	}
	v = r5Publish(t, h, root+"/roles", owner, v.Version, d, true, uuid(t))
	for _, key := range []string{"custom-role-key", "fresh-scope-probe"} {
		uniqueCreate(t, h, base, user, key, 403)
	}
	uniqueCreate(t, h, base, user, nil, 200)
	for i := range d.Grants {
		if d.Grants[i].Scope != nil {
			d.Grants[i].Scope = &all
		}
	}
	v = r5Publish(t, h, root+"/roles", owner, v.Version, d, true, uuid(t))
	uniqueCreate(t, h, base, user, "after-live-role-change", 200)
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.memo0"}})
	v = r5Publish(t, h, root+"/roles", owner, v.Version, d, true, uuid(t))
	for _, key := range []string{"custom-role-key", "fresh-deny-probe"} {
		uniqueCreate(t, h, base, user, key, 403)
	}
	r5Publish(t, h, root+"/roles", owner, v.Version, d, false, uuid(t))
	uniqueCreate(t, h, base, user, "inactive-head-probe", 403)
}

func TestM9RUniqueReviewRegistrationFencesOldSnapshotAndBuildDeleteOverlap(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 19, false)
	scalarReviewTarget(t, f)
	rows := choicePages(t, h, base, token, m5Query())
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	ds, _ := uniqueReviewBinding(base)
	old, err := f.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	scalarSQL(t, err)
	defer old.Rollback()
	a, err := m3Scope(f, context.Background(), old, principal, f.tenantA)
	scalarSQL(t, err)
	scope, err := (data.DatasetStore{}).Scope(context.Background(), old, a, app, ds, 2)
	scalarSQL(t, err)
	_, err = old.Exec(`SELECT set_config('app.dataset_id',$1,true),set_config('app.dataset_binding','2',true),set_config('app.execution_ir_hash',$2,true)`, ds, scope.Hash())
	scalarSQL(t, err)
	path := base + "/unique-constraints/field.contact.memo0"
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	_, err = old.Exec(`UPDATE runtime.records SET row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, f.tenantA, rows[0].ID)
	nativePGCode(t, err, "40001")
	scalarSQL(t, old.Rollback())
	copied, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	build := make(chan error, 1)
	go func() {
		build <- uniqueReviewTransaction(t, f, base, token, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
			got, err := (data.Store{}).BuildUniqueConstraint(ctx, q, s, a, "entity.contact", "field.contact.memo0", x.Generation, x.Checkpoint)
			if err != nil {
				return err
			}
			if got.State != "VERIFYING" || got.ScannedRows != 19 {
				return errors.New("review copy checkpoint missing")
			}
			close(copied)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-copied:
	case err := <-build:
		scalarSQL(t, err)
		t.Fatal("review copy did not reach source lock")
	case <-time.After(2 * time.Second):
		t.Fatal("review copy setup timeout")
	}
	deleted := make(chan int, 1)
	go func() {
		deleted <- m5Call(h, base+"/commands", token, data.Command{Key: uuid(t), Operation: "delete", ID: rows[0].ID, ExpectedVersion: 1}).Code
	}()
	blocked := false
	for deadline := time.Now().Add(350 * time.Millisecond); time.Now().Before(deadline); {
		var n int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE usename='framework_app' AND datname=current_database() AND cardinality(pg_blocking_pids(pid))>0`).Scan(&n))
		if n > 0 {
			blocked = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	unblock()
	scalarSQL(t, <-build)
	if code := <-deleted; code != 200 {
		t.Fatal("review delete after bounded source lock", code)
	}
	if !blocked {
		t.Fatal("review did not observe delete/build overlap")
	}
	for x.State != "READY" {
		x = uniqueStep(t, h, path, token, "build", x)
	}
	var records, claims int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&records))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.unique_claims WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&claims))
	if records != 18 || claims != 18 {
		t.Fatal("review deletion left orphan claims", records, claims)
	}
}

func TestM9RUniqueReviewInstalledHeadFencesUnrelatedEntityEvolution(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 5, false)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	plainSource := uniqueReviewSource(t, f, h, token, app, 3, 2)
	plain := uniqueReviewBind(t, h, base, token, plainSource, 2)
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=4`, f.tenantA, app).Scan(&raw))
	witness := shadowSource(t, h, token, f.tenantA, app, 5, raw, func(d *metadata.Document) {
		d.Definitions = append(d.Definitions,
			metadata.Definition{ID: "entity.other", Kind: "entity", Label: "Unrelated entity", SchemaVersion: d.SchemaVersion, Namespace: d.Namespace, Entity: &metadata.Entity{Fields: []string{"field.other.id"}}},
			metadata.Definition{ID: "field.other.id", Kind: "field", Label: "ID", SchemaVersion: d.SchemaVersion, Namespace: d.Namespace, Field: &metadata.Field{EntityRef: "entity.other", Type: "string", Required: true}},
		)
	})
	_, root := uniqueReviewBinding(plain)
	witness["operation_id"], witness["expected_binding"], witness["intent"] = uuid(t), int64(3), compiler.EvolutionIntent{}
	if w := m5Call(h, root+"/evolutions", token, witness); w.Code != 409 {
		t.Fatal("review installed constraint lost to unrelated evolution", w.Code)
	}
	var state string
	var version, records, operations int
	scalarSQL(t, f.operator.QueryRow(`SELECT state,active_binding FROM runtime.datasets WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&state, &version))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&records))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.dataset_evolutions WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&operations))
	if state != "OPEN" || version != 3 || records != 5 || operations != 0 {
		t.Fatal("review rejected evolution changed live source")
	}
	uniqueCreate(t, h, plain, token, "unique-1", 409)
	uniqueCreate(t, h, plain, token, "compatible-new-key", 200)
}

func uniqueReviewLegacy(t *testing.T, f *iamFixture) http.Handler {
	t.Helper()
	binary, expected := os.Getenv("M9R_LEGACY_API_BINARY"), os.Getenv("M9R_LEGACY_API_SHA256")
	if binary == "" {
		t.Skip("checksum-pinned pre-uniqueness API artifact is opt-in")
	}
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal("review retained artifact unavailable")
	}
	hash := sha256.Sum256(raw)
	if len(expected) != 64 || hex.EncodeToString(hash[:]) != expected {
		t.Fatal("review retained artifact checksum mismatch")
	}
	public, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	keyFile := filepath.Join(t.TempDir(), "public-jwks.json")
	if os.WriteFile(keyFile, public, 0600) != nil {
		t.Fatal("review retained public-key fixture")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("review retained owned port")
	}
	address := listener.Addr().String()
	listener.Close()
	command := exec.Command(binary)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "APP_ENV=test", "HTTP_ADDR=" + address, "DATABASE_URL=" + os.Getenv("DATABASE_URL"), "REDIS_URL=" + os.Getenv("REDIS_URL"), "AUTH_ISSUER=https://test-issuer.example", "AUTH_AUDIENCE=test-api", "AUTH_JWKS_FILE=" + keyFile, "EVENTS_ENABLED=false", "WORKFLOWS_ENABLED=false"}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if command.Start() != nil {
		t.Fatal("review retained process failed to start")
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = command.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("review retained shutdown timeout")
		}
		if exitErr != nil {
			t.Error("review retained process exit")
		}
	})
	client := &http.Client{Timeout: 3 * time.Second}
	t.Cleanup(client.CloseIdleConnections)
	ready := false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		select {
		case <-done:
			t.Fatal("review retained exited before readiness")
		default:
		}
		response, err := client.Get("http://" + address + "/readyz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("review retained readiness unavailable")
	}
	t.Logf("retained_api_sha256=%s", expected)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Clone(r.Context())
		request.URL.Scheme = "http"
		request.URL.Host = address
		request.Host = address
		request.RequestURI = ""
		response, err := client.Do(request)
		if err != nil {
			w.WriteHeader(503)
			return
		}
		defer response.Body.Close()
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, io.LimitReader(response.Body, 1<<20))
	})
}

func TestM9RUniqueReviewRetainedBinaryReadPatchClearDeleteAndKeyDenial(t *testing.T) {
	if os.Getenv("M9R_LEGACY_API_BINARY") == "" {
		t.Skip("checksum-pinned pre-uniqueness API artifact is opt-in")
	}
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 5, false)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	plainSource := uniqueReviewSource(t, f, h, token, app, 3, 2)
	plain := uniqueReviewBind(t, h, base, token, plainSource, 2)
	legacy := uniqueReviewLegacy(t, f)
	rows := choicePages(t, legacy, plain, token, m5Query())
	if len(rows) != 5 {
		t.Fatal("review retained populated read")
	}
	r := rows[0]
	if w := m5Call(legacy, plain+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: r.Version, Values: map[string]any{"field.contact.name": "Legacy unrelated patch"}}); w.Code != 200 {
		t.Fatal("review retained unrelated patch", w.Code)
	}
	for _, key := range []string{"unique-1", "new-legacy-key"} {
		// The retained binary predates the new 42501 translation. Both requests
		// fail with its generic 503, never a value-dependent uniqueness result.
		uniqueCreate(t, legacy, plain, token, key, 503)
	}
	if w := m5Call(legacy, plain+"/commands", token, data.Command{Operation: "patch", ID: r.ID, ExpectedVersion: 2, Values: map[string]any{"field.contact.memo0": nil}}); w.Code != 200 {
		t.Fatal("review retained key clear", w.Code)
	}
	created := uniqueCreate(t, legacy, plain, token, nil, 200)
	if w := m5Call(legacy, plain+"/commands", token, data.Command{Operation: "delete", ID: created.ID, ExpectedVersion: 1}); w.Code != 200 {
		t.Fatal("review retained delete", w.Code)
	}
	var wrong, count int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.unique_claims p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.id=r.id AND p.field_id='field.contact.memo0' AND p.generation=1 WHERE r.tenant_id=$1 AND r.app_id=$2 AND (p.source_version IS DISTINCT FROM r.row_version OR p.value_key IS DISTINCT FROM runtime.unique_key(r.custom_data->'field.contact.memo0','string','',false,'[]'))`, f.tenantA, app).Scan(&wrong))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&count))
	if wrong != 0 || count != 5 {
		t.Fatal("review retained writes lost claims or changed denied sources")
	}
}

func TestM9RUniqueReviewPopulatedTenantWorkerAndReadinessDrift(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 5, false)
	scalarReviewTarget(t, f)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=3`, f.tenantA, app).Scan(&raw))
	bob := f.token(t, f.bobSubject, nil)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); w.Code != 201 {
		t.Fatal("review other-tenant authoring", w.Code)
	}
	other := "/v1/tenants/" + f.tenantB + "/preview/" + app + "/versions/1/entities/entity.contact"
	uniqueReady(t, h, other+"/unique-constraints/field.contact.memo0", bob)
	uniqueCreate(t, h, other, bob, "other-tenant-key", 200)
	if w := m3Call(h, "GET", other+"/unique-constraints/field.contact.memo0", token, 0, nil); w.Code != 403 {
		t.Fatal("review cross-tenant management", w.Code)
	}
	scalarSQL(t, uniqueReviewTransaction(t, f, base, token, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
		var n int
		var own bool
		if err := q.QueryRowContext(ctx, `SELECT count(*),coalesce(bool_and(tenant_id=$1),true) FROM runtime.unique_constraints`, f.tenantA).Scan(&n, &own); err != nil {
			return err
		}
		if n != 1 || !own {
			return errors.New("review populated tenant RLS failed")
		}
		if _, err := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenantB); err != nil {
			return err
		}
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.unique_constraints`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("review forged tenant context exposed populated registry")
		}
		return nil
	}))
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	scalarSQL(t, err)
	defer worker.Close()
	var database, role string
	scalarSQL(t, worker.QueryRow(`SELECT current_database(),session_user`).Scan(&database, &role))
	if !strings.HasPrefix(database, "m8_delivery_") || role != "framework_worker" {
		t.Fatal("review worker target mismatch")
	}
	_, err = f.operator.Exec(`GRANT SELECT(value_hash) ON runtime.unique_claims TO framework_worker`)
	scalarSQL(t, err)
	defer func() {
		_, _ = f.operator.Exec(`REVOKE SELECT(value_hash) ON runtime.unique_claims FROM framework_worker`)
	}()
	var safe bool
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe))
	if safe {
		t.Fatal("review worker privilege drift invisible")
	}
	rows, err := worker.Query(`SELECT value_hash FROM runtime.unique_claims`)
	scalarSQL(t, err)
	visible := rows.Next()
	scalarSQL(t, rows.Err())
	rows.Close()
	if visible {
		t.Fatal("review restrictive worker RLS exposed populated claims")
	}
	_, err = f.operator.Exec(`REVOKE SELECT(value_hash) ON runtime.unique_claims FROM framework_worker`)
	scalarSQL(t, err)
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe))
	if !safe {
		t.Fatal("review worker grant repair failed")
	}
	for _, fault := range []struct{ apply, repair string }{
		{`ALTER TABLE runtime.records DISABLE TRIGGER record_unique_claims`, `ALTER TABLE runtime.records ENABLE TRIGGER record_unique_claims`},
		{`GRANT EXECUTE ON FUNCTION runtime.unique_key(jsonb,text,text,boolean,jsonb) TO framework_app`, `REVOKE EXECUTE ON FUNCTION runtime.unique_key(jsonb,text,text,boolean,jsonb) FROM framework_app`},
		{`ALTER POLICY worker_no_generic ON runtime.unique_constraints USING(true) WITH CHECK(true)`, `ALTER POLICY worker_no_generic ON runtime.unique_constraints USING(false) WITH CHECK(false)`},
	} {
		func() {
			_, err := f.operator.Exec(fault.apply)
			scalarSQL(t, err)
			defer func() { _, err := f.operator.Exec(fault.repair); scalarSQL(t, err) }()
			if (data.Store{}).Ready(context.Background(), f.db) == nil {
				t.Fatal("review weakened integrity boundary remained ready")
			}
		}()
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RUniqueReviewCrossFamilyQuotaRaceAndCoverageCorruption(t *testing.T) {
	m8Database(t)
	f, h, base, token, app, _ := uniqueFixture(t, 17, false)
	for _, field := range []string{"field.contact.balance", "field.contact.joined", "field.contact.name", "field.contact.status", "field.contact.parent", "field.contact.memo0"} {
		m9rIndex(t, h, base+"/indexes/"+field, token, data.NumericIndex{}, "register")
	}
	m9rIndex(t, h, base+"/search-indexes/field.contact.name", token, data.NumericIndex{}, "register")
	start := make(chan struct{})
	results := make(chan int, 2)
	var group sync.WaitGroup
	for _, item := range []struct {
		path string
		body any
	}{
		{base + "/search-indexes/field.contact.memo0/register", struct{}{}},
		{base + "/unique-constraints/field.contact.memo0/register", map[string]any{"expected_generation": 0}},
	} {
		group.Add(1)
		go func() { defer group.Done(); <-start; results <- m5Call(h, item.path, token, item.body).Code }()
	}
	close(start)
	group.Wait()
	close(results)
	success := 0
	for code := range results {
		if code == 200 {
			success++
		} else if code != 400 && code != 409 {
			t.Fatal("review quota race status", code)
		}
	}
	var count int
	scalarSQL(t, f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.search_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.unique_constraints WHERE tenant_id=$1 AND app_id=$2 AND state<>'ABORTED')`, f.tenantA, app).Scan(&count))
	if success != 1 || count != 8 {
		t.Fatal("review shared quota exceeded", success, count)
	}
	// An independent namespace keeps this corruption probe independent of which
	// contender won the shared final slot above.
	f, h, base, token, app, _ = uniqueFixture(t, 17, false)
	path := base + "/unique-constraints/field.contact.memo0"
	x := uniqueStep(t, h, path, token, "register", data.UniqueConstraint{})
	x = uniqueStep(t, h, path, token, "build", x)
	if x.State != "VERIFYING" {
		t.Fatal("review verification fixture")
	}
	_, err := f.operator.Exec(`UPDATE runtime.unique_claims SET value_key='corrupt' WHERE tenant_id=$1 AND app_id=$2 AND id=(SELECT id FROM runtime.unique_claims WHERE tenant_id=$1 AND app_id=$2 AND value_key IS NOT NULL LIMIT 1)`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, path+"/build", token, map[string]any{"expected_generation": x.Generation, "expected_checkpoint": x.Checkpoint}); w.Code != 409 {
		t.Fatal("review corrupt canonical key passed verification", w.Code)
	}
	if got := uniqueStatus(t, h, path, token); got != x {
		t.Fatal("review failed coverage advanced checkpoint")
	}
	_, err = f.operator.Exec(`UPDATE runtime.unique_claims p SET value_key=runtime.unique_key(r.custom_data->p.field_id,'string','',false,'[]') FROM runtime.records r WHERE p.tenant_id=$1 AND p.app_id=$2 AND r.tenant_id=p.tenant_id AND r.app_id=p.app_id AND r.metadata_revision=p.metadata_revision AND r.ir_hash=p.ir_hash AND r.entity_id=p.entity_id AND r.id=p.id`, f.tenantA, app)
	scalarSQL(t, err)
	for x.State != "READY" {
		x = uniqueStep(t, h, path, token, "build", x)
	}
}

func TestM9RUniqueReviewCanonicalBoundariesAndSessionInvariance(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	scalarReviewTarget(t, f)
	for _, session := range []string{"UTC", "Asia/Kolkata", "America/New_York"} {
		tx, err := f.operator.Begin()
		scalarSQL(t, err)
		_, err = tx.Exec(`SELECT set_config('TimeZone',$1,true),set_config('DateStyle','SQL, DMY',true)`, session)
		scalarSQL(t, err)
		var same, carry, reference bool
		scalarSQL(t, tx.QueryRow(`SELECT runtime.unique_key('"1969-12-31T23:59:59.9999996Z"','datetime','',false,'[]')=runtime.unique_key('"1970-01-01T05:30:00+05:30"','datetime','',false,'[]'),runtime.unique_key('"9999-12-31T23:59:59.9999995Z"','datetime','',false,'[]')='["unique-v1", "datetime", "", "253402300800000000"]',runtime.unique_key('"12345678-1234-1234-1234-123456789abc"','reference','',false,'[]') IS NOT NULL`).Scan(&same, &carry, &reference))
		if !same || !carry || !reference {
			t.Fatal("review canonical session or rounding drift", session)
		}
		scalarSQL(t, tx.Rollback())
	}
	for _, test := range []struct{ value, typ, cur, choices string }{
		{`1.0`, "integer", "", "[]"}, {`"10000-01-01T00:00:00Z"`, "datetime", "", "[]"}, {`"2023-02-29"`, "date", "", "[]"},
		{`{"amount":"1","currency":"EUR"}`, "money", "USD", "[]"}, {`"missing"`, "enum", "", `["allowed"]`}, {`"12345678-1234-1234-1234-123456789ABC"`, "reference", "", "[]"},
	} {
		var key string
		err := f.operator.QueryRow(`SELECT runtime.unique_key($1::jsonb,$2,$3,false,$4::jsonb)`, test.value, test.typ, test.cur, test.choices).Scan(&key)
		nativePGCode(t, err, "22023")
	}
}

func TestM9RUniqueReviewPermitLocksLiveAuthorityThroughCommit(t *testing.T) {
	m8Database(t)
	f, h, base, owner, app, _ := uniqueFixture(t, 1, false)
	uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", owner)
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	all := policy.DataScope{Kind: "ALL"}
	d := r5Definition("unique_locked", app, all)
	d.Grants = append(d.Grants,
		policy.Grant{Effect: "ALLOW", Actions: []string{"preview.write"}, Resource: policy.Selector{AppID: app, EntityID: "*", FieldID: "*"}},
		policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.write", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}, Scope: &all},
		policy.Grant{Effect: "ALLOW", Actions: []string{"data.row.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}},
	)
	v := r5Publish(t, h, root+"/roles", owner, 0, d, true, uuid(t))
	r5Assign(t, h, root, owner, f.viewer, d.Name, 200)
	user := f.token(t, f.viewerSubject, nil)
	rid := choicePages(t, h, base, owner, m5Query())[0].ID
	locked, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	written := make(chan error, 1)
	go func() {
		written <- uniqueReviewTransaction(t, f, base, user, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
			if _, err := q.ExecContext(ctx, `SELECT runtime.authorize_unique_write($1,$2,$3,$4,'entity.contact',$5,'{"field.contact.memo0":"locked-key"}')`, a.TenantID(), app, s.DataRevision(), s.DataHash(), rid); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.memo0}','"locked-key"'),row_version=row_version+1 WHERE tenant_id=$1 AND id=$2`, a.TenantID(), rid); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT runtime.clear_unique_write()`); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case err := <-written:
		scalarSQL(t, err)
		t.Fatal("review permit did not reach lock witness")
	case <-time.After(2 * time.Second):
		t.Fatal("review permit lock setup timeout")
	}
	revoked := make(chan int, 1)
	go func() {
		revoked <- m5Call(h, root+"/roles", owner, map[string]any{"operation_id": uuid(t), "expected_version": v.Version, "active": false, "definition": d}).Code
	}()
	blocked := false
	for deadline := time.Now().Add(350 * time.Millisecond); time.Now().Before(deadline); {
		var n int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE usename='framework_app' AND datname=current_database() AND cardinality(pg_blocking_pids(pid))>0`).Scan(&n))
		if n > 0 {
			blocked = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	unblock()
	scalarSQL(t, <-written)
	if code := <-revoked; code != 200 {
		t.Fatal("review fenced live role revocation", code)
	}
	if !blocked {
		t.Fatal("review did not observe authority lock through mutation")
	}
	err := uniqueReviewTransaction(t, f, base, user, func(ctx context.Context, q postgres.Executor, a tenant.Access, s data.Scope) error {
		_, err := q.ExecContext(ctx, `SELECT runtime.authorize_unique_write($1,$2,$3,$4,'entity.contact',$5,'{"field.contact.memo0":"revoked-key"}')`, a.TenantID(), app, s.DataRevision(), s.DataHash(), rid)
		return err
	})
	nativePGCode(t, err, "42501")
}
