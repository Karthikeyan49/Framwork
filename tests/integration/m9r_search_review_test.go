//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
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
	"reflect"
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
)

// Independent search review reuses established, effectively targeted fixtures.
// Source code and applied migrations remain untouched during this review.
func TestM9RSearchReviewActualUnknownCommitReconciliation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	scalarCreate(t, h, base, token, "Commit witness")
	scalarSeed(t, f, app, 10)
	path := base + "/search-indexes/field.contact.memo0"
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
	db, err := postgres.Open(proxy)
	scalarSQL(t, err)
	defer db.Close()
	clone := *f
	clone.db = db
	scalarReviewTarget(t, &clone)
	scalarSQL(t, postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }))
	// Preserve normal policy/audit; only the successful COMMIT reply is dropped.
	faulted := m5Handler(t, &clone, policy.Native{}, audit.Postgres{}, true)
	response := m5Call(faulted, path+"/build", token, map[string]any{"expected_checkpoint": index.Checkpoint})
	if response.Code != 503 || !observed.Load() {
		t.Fatal("review actual lost COMMIT was not observed", response.Code)
	}
	status := m3Call(h, "GET", path, token, 0, nil)
	var committed data.NumericIndex
	if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &committed) != nil || committed.Checkpoint != index.Checkpoint+1 || committed.ScannedRows != 11 || committed.State != "VERIFYING" {
		t.Fatal("review lost COMMIT checkpoint could not reconcile")
	}
	replayed := m9rIndex(t, h, path, token, index, "build")
	if replayed != committed {
		t.Fatal("review old checkpoint repeated committed build effects")
	}
	m9rIndexReady(t, h, path, token)
}

func TestM9RSearchReviewObservedCopyPatchDeleteOverlap(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	updated := scalarCreate(t, h, base, token, "Before update")
	deleted := scalarCreate(t, h, base, token, "Before delete")
	scalarSeed(t, f, app, 19)
	path := base + "/search-indexes/field.contact.memo0"
	index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	scope := scalarScope(t, h, base, token)
	principal, _ := f.verifier.Verify(token)
	copied, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	buildResult := make(chan error, 1)
	go func() {
		buildResult <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			access, err := m3Scope(f, ctx, q, principal, f.tenantA)
			if err != nil {
				return err
			}
			built, err := (data.Store{}).BuildSearchIndex(ctx, q, scope, access, "entity.contact", "field.contact.memo0", index.Checkpoint)
			if err != nil {
				return err
			}
			if built.State != "VERIFYING" || built.ScannedRows != 21 {
				return errors.New("review copy did not retain bounded source locks")
			}
			close(copied)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return errors.New("review copy lease expired")
			}
		})
	}()
	select {
	case <-copied:
	case err := <-buildResult:
		scalarSQL(t, err)
		t.Fatal("review copy did not reach lock witness")
	case <-time.After(2 * time.Second):
		t.Fatal("review copy setup timed out")
	}
	results := make(chan int, 2)
	go func() {
		results <- m5Call(h, base+"/commands", token, data.Command{Key: uuid(t), Operation: "patch", ID: updated.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": strings.Repeat("界", 200)}}).Code
	}()
	go func() {
		results <- m5Call(h, base+"/commands", token, data.Command{Key: uuid(t), Operation: "delete", ID: deleted.ID, ExpectedVersion: 1}).Code
	}()
	// Observe actual lock overlap rather than inferring concurrency from goroutine
	// start time. Release well inside the unchanged 500ms lock timeout.
	deadline := time.Now().Add(400 * time.Millisecond)
	overlapped := false
	for time.Now().Before(deadline) {
		var blocked int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE usename='framework_app' AND datname=current_database() AND cardinality(pg_blocking_pids(pid))>0`).Scan(&blocked))
		if blocked >= 2 {
			overlapped = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	unblock()
	scalarSQL(t, <-buildResult)
	for range 2 {
		if status := <-results; status != 200 {
			t.Fatal("review overlapping writer rejected", status)
		}
	}
	if !overlapped {
		t.Fatal("review did not observe both writers blocked by active copy")
	}
	m9rIndexReady(t, h, path, token)
	var wrong, removed, overflow int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.search_projections p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.field_id='field.contact.memo0' AND p.id=r.id WHERE r.tenant_id=$1 AND r.app_id=$2 AND (p.id IS NULL OR p.source_version<>r.row_version OR p.value_search IS DISTINCT FROM to_tsvector('pg_catalog.simple',coalesce(r.custom_data->>'field.contact.memo0','')))`, f.tenantA, app).Scan(&wrong))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.search_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, deleted.ID).Scan(&removed))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.search_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND value_search=to_tsvector('pg_catalog.simple',repeat('界',200)) AND source_version=2`, f.tenantA, app, updated.ID).Scan(&overflow))
	if wrong != 0 || removed != 0 || overflow != 1 {
		t.Fatal("review concurrent copy lost current value/version/delete")
	}
}

func TestM9RSearchReviewMaskedCursorsAndRetainedLiveACL(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := searchFixture(t)
	first := scalarCreate(t, h, base, token, "Alpha parent")
	second := scalarCreate(t, h, base, token, "Alpha child")
	third := scalarCreate(t, h, base, token, "Alpha other")
	if response := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: second.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.parent": first.ID, "field.contact.status": "inactive"}}); response.Code != 200 {
		t.Fatal("review search reference fixture", response.Code)
	}
	limited, viewer, _, _ := r5dLimited(t, f, app, token)
	collect := func(field string) []data.Record {
		q := searchQuery("alpha")
		q.Limit = 1
		q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance"}}
		q.Filter = &data.Filter{Op: "in", Field: "field.contact.status", Value: json.RawMessage(`["active","inactive"]`)}
		rows := choicePages(t, limited, base, viewer, q)
		for _, r := range rows {
			r5dMasked(t, r)
			if related := r.Related[q.Join.Relationship]; related != nil {
				r5dMasked(t, *related)
				if r.ID != second.ID || related.ID != first.ID || related.Values["field.contact.id"] != first.ID {
					t.Fatal("review joined search returned wrong target identity")
				}
			} else if r.ID == second.ID {
				t.Fatal("review joined search lost populated visible parent")
			}
		}
		return rows
	}
	before := collect("field.contact.memo0")
	for _, field := range []string{"field.contact.memo0"} {
		m9rIndexReady(t, h, base+"/search-indexes/"+field, token)
	}
	if !reflect.DeepEqual(before, collect("field.contact.memo0")) {
		t.Fatal("review search masked filtered joined cursor pages changed")
	}
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	dataset := m9rDatasetCreate(t, h, root+"/datasets", token, "ADOPT", witness)
	path := root + "/datasets/" + dataset.ID
	for dataset.State != "OPEN" {
		response := m5Call(h, path+"/verify", token, struct{}{})
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &dataset) != nil {
			t.Fatal("review search adoption rejected", response.Code)
		}
	}
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
	target := shadowSource(t, h, token, f.tenantA, app, 3, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, h, token, path, ready, "activate")
	current, retired := path+"/bindings/2/entities/entity.contact", path+"/bindings/1/entities/entity.contact"
	for _, field := range []string{"field.contact.memo0"} {
		m9rIndexReady(t, h, current+"/search-indexes/"+field, token)
	}
	access := r5cChange(t, h, current+"/records/"+third.ID+"/access", token, recordOwner(f.viewer, ""), 200)
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	for _, field := range []string{"field.contact.memo0"} {
		for _, b := range []string{current, retired} {
			q := searchQuery("alpha")
			q.Search.Field = field
			q.Limit = 1
			rows := choicePages(t, owners, b, token, q)
			if len(rows) != 2 {
				t.Fatal("review search canonical owner scope after limit")
			}
			for _, r := range rows {
				if r.ID == third.ID {
					t.Fatal("review search retained index leaked old owner")
				}
			}
		}
	}
	shares := m5Handler(t, f, scopedPolicy{mode: "share"}, audit.Postgres{}, true)
	no := false
	access = r5cChange(t, h, current+"/records/"+third.ID+"/access", token, data.AccessCommand{Operation: "share", ExpectedVersion: access.Version, UserID: f.alice, CanWrite: &no}, 200)
	for _, b := range []string{current, retired} {
		rows := choicePages(t, shares, b, token, searchQuery("alpha"))
		if len(rows) != 1 || rows[0].ID != third.ID {
			t.Fatal("review search current share missing in retained index")
		}
	}
	access = r5cChange(t, h, current+"/records/"+third.ID+"/access", token, data.AccessCommand{Operation: "unshare", ExpectedVersion: access.Version, UserID: f.alice}, 200)
	for _, b := range []string{current, retired} {
		if rows := choicePages(t, shares, b, token, searchQuery("alpha")); len(rows) != 0 {
			t.Fatal("review search revoked share remains indexed")
		}
	}
	if response := m5Call(h, current+"/commands", token, data.Command{Operation: "delete", ID: third.ID, ExpectedVersion: access.Version}); response.Code != 200 {
		t.Fatal("review search tombstone", response.Code)
	}
	for _, b := range []string{current, retired} {
		rows := choicePages(t, h, b, token, searchQuery("alpha"))
		if len(rows) != 2 {
			t.Fatal("review search retained projection resurrected tombstone")
		}
	}
}

func TestM9RSearchReviewRetainedBinaryCRUDAndMaintenance(t *testing.T) {
	binary := os.Getenv("M9R_LEGACY_API_BINARY")
	if binary == "" {
		t.Skip("retained pre-extension API artifact is opt-in")
	}
	expected := os.Getenv("M9R_LEGACY_API_SHA256")
	raw, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal("retained artifact unavailable")
	}
	hash := sha256.Sum256(raw)
	if len(expected) != 64 || hex.EncodeToString(hash[:]) != expected {
		t.Fatal("retained artifact checksum mismatch")
	}
	m8Database(t)
	f, current, base, token, app := searchFixture(t)
	parent := scalarCreate(t, current, base, token, "Legacy parent")
	child := scalarCreate(t, current, base, token, "Legacy child")
	for _, field := range []string{"field.contact.memo0"} {
		m9rIndexReady(t, current, base+"/search-indexes/"+field, token)
	}
	public, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	keyFile := filepath.Join(t.TempDir(), "public-jwks.json")
	if os.WriteFile(keyFile, public, 0600) != nil {
		t.Fatal("retained artifact public-key fixture unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("retained artifact loopback port unavailable")
	}
	address := listener.Addr().String()
	listener.Close()
	command := exec.Command(binary)
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "APP_ENV=test", "HTTP_ADDR=" + address, "DATABASE_URL=" + os.Getenv("DATABASE_URL"), "REDIS_URL=" + os.Getenv("REDIS_URL"), "AUTH_ISSUER=https://test-issuer.example", "AUTH_AUDIENCE=test-api", "AUTH_JWKS_FILE=" + keyFile, "EVENTS_ENABLED=false", "WORKFLOWS_ENABLED=false"}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if command.Start() != nil {
		t.Fatal("retained API process failed to start")
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = command.Wait(); close(done) }()
	defer func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("retained API shutdown timed out")
		}
		if exitErr != nil {
			t.Error("retained API exit was unsuccessful")
		}
	}()
	client := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		select {
		case <-done:
			t.Fatal("retained API exited before readiness")
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
		t.Fatal("retained API did not become ready")
	}
	legacy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	for _, field := range []string{"field.contact.memo0"} {
		query := m5Query()
		query.Filter = &data.Filter{Op: "contains", Field: field, Value: json.RawMessage(`"Legacy"`)}
		if before, after := choicePages(t, current, base, token, query), choicePages(t, legacy, base, token, query); !reflect.DeepEqual(before, after) {
			t.Fatal("retained unindexed fallback differs from new indexed path")
		}
	}
	oldQuery := m5Query()
	oldQuery.Limit = 1
	oldPage := m5Page(t, m5Call(legacy, base+"/query", token, oldQuery))
	if len(oldPage.Records) != 1 || oldPage.Next == "" {
		t.Fatal("review legacy cursor must span populated pages")
	}
	oldQuery.After = oldPage.Next
	newPage := m5Page(t, m5Call(current, base+"/query", token, oldQuery))
	legacyPage := m5Page(t, m5Call(legacy, base+"/query", token, oldQuery))
	if !reflect.DeepEqual(newPage, legacyPage) || len(newPage.Records) != 1 {
		t.Fatal("review old absent-search cursor incompatible with new reader")
	}
	response := m5Call(legacy, base+"/commands", token, data.Command{Operation: "patch", ID: child.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": "Legacy maintained orchard", "field.contact.parent": parent.ID}})
	if response.Code != 200 {
		t.Fatal("retained valid patch failed after migration51", response.Code)
	}
	var projections int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.search_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND source_version=2 AND value_search@@plainto_tsquery('pg_catalog.simple','maintained orchard')`, f.tenantA, app, child.ID).Scan(&projections))
	if projections != 1 {
		t.Fatal("retained writer did not maintain new search projection")
	}
	created := m5Create(t, legacy, base, token, "Retained created")
	if response := m5Call(legacy, base+"/commands", token, data.Command{Operation: "delete", ID: created.ID, ExpectedVersion: 1}); response.Code != 200 {
		t.Fatal("retained delete failed", response.Code)
	}
	for _, field := range []string{"field.contact.memo0"} {
		query := m5Query()
		query.Filter = &data.Filter{Op: "contains", Field: field, Value: json.RawMessage(`"Legacy"`)}
		if before, after := choicePages(t, current, base, token, query), choicePages(t, legacy, base, token, query); !reflect.DeepEqual(before, after) {
			t.Fatal("retained post-write fallback diverged")
		}
	}
	t.Logf("retained API startup/read/fallback/patch/create/delete verified sha256=%s", expected)
}

func TestM9RSearchReviewPrivatePortsPopulatedRolesAndDrift(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := searchFixture(t)
	scalarReviewTarget(t, f)
	scalarCreate(t, h, base, token, "Alpha private orchard")
	path := base + "/search-indexes/field.contact.memo0"
	m9rIndexReady(t, h, path, token)
	other, oh, ob, ot, _ := searchFixture(t)
	scalarCreate(t, oh, ob, ot, "Alpha other tenant")
	m9rIndexReady(t, oh, ob+"/search-indexes/field.contact.memo0", ot)
	principal, _ := f.verifier.Verify(token)
	scalarSQL(t, postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		for _, text := range []string{"", "alpha", "beta", strings.Repeat("a", 257), "!!!"} {
			if _, err := q.ExecContext(ctx, `SELECT set_config('app.search_text',$1,true)`, text); err != nil {
				return err
			}
			var n int
			var own bool
			if err := q.QueryRowContext(ctx, `SELECT count(*),coalesce(bool_and(tenant_id=$1),true) FROM runtime.search_projections`, f.tenantA).Scan(&n, &own); err != nil {
				return err
			}
			want := 0
			if text == "alpha" {
				want = 1
			}
			if n != want || !own {
				return errors.New("review populated projection query restriction failed")
			}
		}
		if _, err := q.ExecContext(ctx, `SELECT set_config('app.search_text','alpha',true),set_config('app.tenant_id',$1,true)`, other.tenantA); err != nil {
			return err
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.search_projections`).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return errors.New("review forged search/tenant context crossed membership")
		}
		return nil
	}))
	for _, binding := range []string{"DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db, err := postgres.Open(os.Getenv(binding))
		scalarSQL(t, err)
		var database, role string
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&database, &role))
		expected := map[string]string{"DATABASE_URL": "framework_app", "WORKER_DATABASE_URL": "framework_worker", "DISPATCHER_DATABASE_URL": "framework_dispatcher"}[binding]
		if !strings.HasPrefix(database, "m8_delivery_") || role != expected {
			db.Close()
			t.Fatal("review private probe requires effective owned database/role")
		}
		var value string
		err = db.QueryRow(`SELECT runtime.search_value('"alpha"',false)::text`).Scan(&value)
		nativePGCode(t, err, "42501")
		if binding != "DATABASE_URL" {
			err = db.QueryRow(`SELECT runtime.search_parser_hash()`).Scan(&value)
			nativePGCode(t, err, "42501")
			_, err = db.Exec(`SELECT runtime.register_search_index($1,$2,2,$3,'entity.contact','field.contact.memo0',repeat('0',64),2,$3)`, f.tenantA, app, scalarScope(t, h, base, token).Hash())
			nativePGCode(t, err, "42501")
		}
		db.Close()
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	scalarSQL(t, err)
	defer worker.Close()
	_, err = f.operator.Exec(`GRANT SELECT(value_search) ON runtime.search_projections TO framework_worker`)
	scalarSQL(t, err)
	t.Cleanup(func() {
		_, err := f.operator.Exec(`REVOKE SELECT(value_search) ON runtime.search_projections FROM framework_worker`)
		scalarSQL(t, err)
	})
	var safe bool
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe))
	if safe {
		t.Fatal("review search grant drift was invisible")
	}
	rows, err := worker.Query(`SELECT value_search FROM runtime.search_projections`)
	scalarSQL(t, err)
	exposed := rows.Next()
	scalarSQL(t, rows.Err())
	rows.Close()
	if exposed {
		t.Fatal("review worker populated vectors escaped restrictive RLS")
	}
	_, err = f.operator.Exec(`REVOKE SELECT(value_search) ON runtime.search_projections FROM framework_worker`)
	scalarSQL(t, err)
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&safe))
	if !safe {
		t.Fatal("review search grant repair failed")
	}
	// Each committed forged witness is restored even when an assertion fails.
	for _, fault := range []struct {
		column string
		value  any
	}{{"schema_hash", strings.Repeat("0", 64)}, {"parser_hash", strings.Repeat("0", 64)}, {"required", true}} {
		func() {
			var original any
			scalarSQL(t, f.operator.QueryRow(`SELECT `+fault.column+` FROM runtime.search_indexes WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&original))
			_, err := f.operator.Exec(`UPDATE runtime.search_indexes SET `+fault.column+`=$3 WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app, fault.value)
			scalarSQL(t, err)
			defer func() {
				_, err := f.operator.Exec(`UPDATE runtime.search_indexes SET `+fault.column+`=$3 WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app, original)
				scalarSQL(t, err)
			}()
			if w := m5Call(h, base+"/query", token, searchQuery("alpha")); w.Code != 503 {
				t.Fatal("review forged search witness executed", w.Code)
			}
			if fault.column == "parser_hash" {
				if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: choicePages(t, h, base, token, m5Query())[0].ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.memo0": "Beta"}}); w.Code == 200 {
					t.Fatal("review changed parser mixed projection writes")
				}
			}
		}()
	}
	_, err = f.operator.Exec(`ALTER POLICY search_match ON runtime.search_projections USING(true)`)
	scalarSQL(t, err)
	func() {
		defer func() {
			_, err := f.operator.Exec(`ALTER POLICY search_match ON runtime.search_projections USING(value_search @@ (SELECT runtime.search_context()))`)
			scalarSQL(t, err)
		}()
		if (data.Store{}).Ready(context.Background(), f.db) == nil {
			t.Fatal("review weakened search predicate passed readiness")
		}
	}()
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RSearchReviewSharedQuotaConcurrentLastSlot(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 7)
	scalarCreate(t, h, base, token, "Alpha quota")
	for _, field := range []string{"field.contact.balance", "field.contact.joined", "field.contact.memo0", "field.contact.memo1", "field.contact.memo2", "field.contact.memo3", "field.contact.memo4"} {
		m9rIndex(t, h, base+"/indexes/"+field, token, data.NumericIndex{}, "register")
	}
	start := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, path := range []string{base + "/search-indexes/field.contact.memo0", base + "/indexes/field.contact.memo5"} {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; results <- m5Call(h, path+"/register", token, struct{}{}).Code }()
	}
	close(start)
	wg.Wait()
	close(results)
	success := 0
	for code := range results {
		if code == 200 {
			success++
		} else if code != 400 && code != 409 {
			t.Fatal("review shared search quota race status", code)
		}
	}
	if success != 1 {
		t.Fatal("review last shared slot did not admit exactly one registration", success)
	}
	var n int
	scalarSQL(t, f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.numeric_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2)+(SELECT count(*) FROM runtime.search_indexes WHERE tenant_id=$1 AND app_id=$2)`, f.tenantA, app).Scan(&n))
	if n != 8 {
		t.Fatal("review cross-family quota exceeded")
	}
}

func TestM9RSearchReviewInputPrivacyPointOpaqueAndCursor(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := searchFixture(t)
	scalarReviewTarget(t, f)
	first := scalarCreate(t, h, base, token, "Alpha mixed orchard")
	scalarCreate(t, h, base, token, "Alpha mixed orchard")
	scope := scalarScope(t, h, base, token)
	searchSeed(t, f, scope.AppID(), scope.Hash(), 19)
	m9rIndexReady(t, h, base+"/search-indexes/field.contact.memo0", token)
	principal, _ := f.verifier.Verify(token)
	allow := func(context.Context, string, data.Resource) error { return nil }
	all := func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	}
	query := searchQuery("alpha")
	query.Limit = 1
	baseline := m5Page(t, m5Call(h, base+"/query", token, query))
	if baseline.Next == "" {
		t.Fatal("review search cursor fixture did not span pages")
	}
	qnext := query
	qnext.After = baseline.Next
	for _, changed := range []string{"Alpha", "alpha mixed", "beta"} {
		q := qnext
		s := *q.Search
		s.Text = changed
		q.Search = &s
		if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
			t.Fatal("review cursor accepted different exact search contract", w.Code)
		}
	}
	scalarSQL(t, postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		access, err := m3Scope(f, ctx, q, principal, f.tenantA)
		if err != nil {
			return err
		}
		opaque := &scalarOpaque{Executor: q}
		fallback, err := (data.Store{}).Query(ctx, opaque, scope, access, "entity.contact", query, allow, all)
		if err != nil {
			return err
		}
		indexed, err := (data.Store{}).Query(ctx, q, scope, access, "entity.contact", query, allow, all)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(fallback, indexed) {
			return errors.New("review unproven executor search fallback/cursor differs")
		}
		point := searchQuery("alpha")
		point.RecordID = first.ID
		point.Limit = 1
		point.Projection = []string{"field.contact.id"}
		checked := false
		guard := func(_ context.Context, action string, r data.Resource) error {
			if action == "data.field.read" && r.FieldID == "field.contact.memo0" {
				checked = r.RecordID == first.ID && r.OwnerID == f.alice
				return data.ErrDenied
			}
			return nil
		}
		if _, err := (data.Store{}).Query(ctx, q, scope, access, "entity.contact", point, guard, all); !errors.Is(err, data.ErrDenied) || !checked {
			return errors.New("review strict point search lacks trusted unprojected input authority")
		}
		if _, err := (data.WorkerSourceAdapter{}).Query(ctx, q, scope, access, "entity.contact", point, allow, all); !errors.Is(err, data.ErrDenied) {
			return errors.New("review worker accepted interactive search")
		}
		for _, text := range []string{"alpha", "beta", "!!!"} {
			in := query
			in.Search = &data.Search{Field: "field.contact.memo0", Text: text}
			_, err := (data.Store{}).Query(ctx, q, scope, access, "entity.contact", in, allow, all)
			if text == "!!!" && !errors.Is(err, data.ErrInvalid) {
				return errors.New("review punctuation admitted")
			}
			if text != "!!!" && err != nil {
				return err
			}
			var value string
			if err := q.QueryRowContext(ctx, `SELECT current_setting('app.search_text',true)`).Scan(&value); err != nil {
				return err
			}
			if value != "" {
				return errors.New("review same-transaction search text retained")
			}
		}
		return nil
	}))
	hidden := m5Handler(t, f, denyDataPolicy{"data.field.read", "field.contact.memo0"}, audit.Postgres{}, true)
	q := searchQuery("alpha")
	q.Projection = []string{"field.contact.id"}
	if w := m5Call(hidden, base+"/query", token, q); w.Code != 403 {
		t.Fatal("review hidden unprojected search inferred membership", w.Code)
	}
}

func TestM9RSearchReviewJoinedTargetValuesNullsAndDataScope(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := searchFixture(t)
	parent := scalarCreate(t, h, base, token, "Parent unrelated")
	child := scalarCreate(t, h, base, token, "Alpha child")
	other := scalarCreate(t, h, base, token, "Alpha no parent")
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: parent.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.name": "Visible joined value"}}); w.Code != 200 {
		t.Fatal("review joined value fixture", w.Code)
	}
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: child.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.parent": parent.ID}}); w.Code != 200 {
		t.Fatal("review joined relation fixture", w.Code)
	}
	q := searchQuery("alpha")
	q.Limit = 1
	q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name"}}
	verify := func(handler http.Handler, parentVisible bool) {
		rows := choicePages(t, handler, base, token, q)
		if len(rows) != 2 {
			t.Fatal("review joined search source count")
		}
		found := false
		for _, r := range rows {
			target := r.Related[q.Join.Relationship]
			if r.ID == child.ID {
				found = true
				if parentVisible {
					if target == nil || target.ID != parent.ID || target.Values["field.contact.name"] != "Visible joined value" {
						t.Fatal("review joined search returned incorrect visible target values")
					}
				} else if target != nil {
					t.Fatal("review joined target escaped live target DataScope")
				}
			} else if r.ID != other.ID || target != nil {
				t.Fatal("review LEFT JOIN null behavior changed")
			}
		}
		if !found {
			t.Fatal("review joined source missing")
		}
	}
	verify(h, true)
	m9rIndexReady(t, h, base+"/search-indexes/field.contact.memo0", token)
	verify(h, true)
	change := recordOwner(f.viewer, "")
	change.ExpectedVersion = 2
	r5cChange(t, h, base+"/records/"+parent.ID+"/access", token, change, 200)
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	verify(owners, false)
	verify(h, true)
}

func TestM9RSearchReviewVerificationRejectsCoverageDrift(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := searchFixture(t)
	scalarReviewTarget(t, f)
	scalarCreate(t, h, base, token, "Alpha verification")
	scope := scalarScope(t, h, base, token)
	searchSeed(t, f, app, scope.Hash(), 10)
	path := base + "/search-indexes/field.contact.memo0"
	idx := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
	idx = m9rIndex(t, h, path, token, idx, "build")
	if idx.State != "VERIFYING" || idx.ScannedRows != 11 {
		t.Fatal("review coverage fixture")
	}
	_, err := f.operator.Exec(`UPDATE runtime.search_projections SET value_search=''::tsvector WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	scalarSQL(t, err)
	if w := m5Call(h, path+"/build", token, map[string]any{"expected_checkpoint": idx.Checkpoint}); w.Code != 409 {
		t.Fatal("review incorrect vectors passed independent verification", w.Code)
	}
	var actual data.NumericIndex
	w := m3Call(h, "GET", path, token, 0, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &actual) != nil || actual != idx {
		t.Fatal("review failed verification advanced immutable checkpoint")
	}
	_, err = f.operator.Exec(`UPDATE runtime.search_projections p SET value_search=to_tsvector('pg_catalog.simple',coalesce(r.custom_data->>p.field_id,'')) FROM runtime.records r WHERE p.tenant_id=$1 AND p.app_id=$2 AND r.tenant_id=p.tenant_id AND r.app_id=p.app_id AND r.metadata_revision=p.metadata_revision AND r.ir_hash=p.ir_hash AND r.entity_id=p.entity_id AND r.id=p.id`, f.tenantA, app)
	scalarSQL(t, err)
	m9rIndexReady(t, h, path, token)
	if len(choicePages(t, h, base, token, searchQuery("alpha"))) < 5 {
		t.Fatal("review repaired complete search lost records")
	}
}
