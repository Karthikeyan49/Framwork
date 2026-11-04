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
	"reflect"
	"sort"
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
	"github.com/jackc/pgx/v5/pgconn"
)

// Historical artifacts are opt-in; an ordinary clean CI checkout reports SKIP.
func TestM9RChoiceReviewRetainedBinaryCRUDAndFallback(t *testing.T) {
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
	f, current, base, token, app := choiceFixture(t)
	parent := m5Create(t, current, base, token, "Legacy parent")
	child := m5Create(t, current, base, token, "Legacy child")
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, current, base+"/indexes/"+field, token)
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
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		query := choiceQuery(field, false)
		if before, after := choicePages(t, current, base, token, query), choicePages(t, legacy, base, token, query); !reflect.DeepEqual(before, after) {
			t.Fatal("retained unindexed fallback differs from new indexed path")
		}
	}
	response := m5Call(legacy, base+"/commands", token, data.Command{Operation: "patch", ID: child.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": "inactive", "field.contact.parent": parent.ID}})
	if response.Code != 200 {
		t.Fatal("retained valid patch failed after migration49", response.Code)
	}
	var projections int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND source_version=2 AND value_text IS NOT NULL AND NOT overflow`, f.tenantA, app, child.ID).Scan(&projections))
	if projections != 2 {
		t.Fatal("retained writer did not maintain new choices")
	}
	created := m5Create(t, legacy, base, token, "Retained created")
	if response := m5Call(legacy, base+"/commands", token, data.Command{Operation: "delete", ID: created.ID, ExpectedVersion: 1}); response.Code != 200 {
		t.Fatal("retained delete failed", response.Code)
	}
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		query := choiceQuery(field, true)
		if before, after := choicePages(t, current, base, token, query), choicePages(t, legacy, base, token, query); !reflect.DeepEqual(before, after) {
			t.Fatal("retained post-write fallback diverged")
		}
	}
	t.Logf("retained API startup/read/fallback/patch/create/delete verified sha256=%s", expected)
}

func TestM9RChoiceReviewMaximumEnumCheckpointAndMutation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	var source []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&source))
	var document metadata.Document
	if json.Unmarshal(source, &document) != nil {
		t.Fatal("review maximum enum source invalid")
	}
	choices := make([]string, 100)
	for i := range choices {
		choices[i] = strings.Repeat("😀", 127) + string(rune(0x1f300+i))
	}
	for i := range document.Definitions {
		d := &document.Definitions[i]
		if d.ID == "field.contact.status" {
			d.Field.EnumValues = choices
		}
	}
	source, _ = json.Marshal(document)
	if response := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 2, source); response.Code != 201 {
		t.Fatal("review maximum valid enum source rejected", response.Code)
	}
	base = strings.Replace(base, "/versions/2/", "/versions/3/", 1)
	values := m5Values("Maximum enum")
	values["field.contact.status"] = choices[0]
	response := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values})
	var record data.Record
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &record) != nil {
		t.Fatal("review maximum enum fixture rejected", response.Code)
	}
	var hash string
	scalarSQL(t, f.operator.QueryRow(`SELECT ir_hash FROM runtime.record_sets WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=3`, f.tenantA, app).Scan(&hash))
	_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id) SELECT $1,$2,3,$3,'entity.contact',gen_random_uuid(),$4::jsonb,$5,$5,$5 FROM generate_series(1,999)`, f.tenantA, app, hash, responseRecordValues(t, values), f.alice)
	scalarSQL(t, err)
	index := m9rIndex(t, h, base+"/indexes/field.contact.status", token, data.NumericIndex{}, "register")
	for index.State != "READY" {
		start := time.Now()
		index = m9rIndex(t, h, base+"/indexes/field.contact.status", token, index, "build")
		t.Logf("max_enum_checkpoint state=%s copied=%d verified=%d elapsed_ms=%.3f", index.State, index.ScannedRows, index.VerifiedRows, float64(time.Since(start).Microseconds())/1000)
	}
	if index.ScannedRows != 1000 || index.VerifiedRows != 1000 {
		t.Fatal("review maximum enum checkpoint coverage")
	}
	start := time.Now()
	if response := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: record.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.status": choices[99]}}); response.Code != 200 {
		t.Fatal("review maximum enum mutation rejected", response.Code)
	}
	t.Logf("max_enum_mutation elapsed_ms=%.3f", float64(time.Since(start).Microseconds())/1000)
}

func responseRecordValues(t *testing.T, values map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal("review value encoding failed")
	}
	return string(raw)
}

func TestM9RChoiceReviewUnicodeNullContractsAndPrivatePorts(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	m5Create(t, h, base, token, "Choice review")
	values := []string{"é", "e\u0301", strings.Repeat("😀", 128), "Å", "Z", "a"}
	sort.Strings(values)
	encoded, _ := json.Marshal(values)
	var safe bool
	scalarSQL(t, f.operator.QueryRow(`SELECT runtime.scalar_options_safe('enum',$1::jsonb,'')`, string(encoded)).Scan(&safe))
	if !safe {
		t.Fatal("review byte-sorted Unicode choices rejected")
	}
	for _, value := range values {
		raw, _ := json.Marshal(value)
		var actual sql.NullString
		var overflow bool
		scalarSQL(t, f.operator.QueryRow(`SELECT value_text,overflow FROM runtime.scalar_project_value($1::jsonb,'enum',true,$2::jsonb)`, string(raw), string(encoded)).Scan(&actual, &overflow))
		if !actual.Valid || actual.String != value || overflow {
			t.Fatal("review enum normalization/truncation occurred")
		}
	}
	for _, args := range [][]any{{nil, `[]`, ""}, {"enum", nil, ""}, {"reference", `[]`, nil}, {"enum", `[null]`, ""}, {"enum", `["a",null]`, ""}, {"enum", `[true]`, ""}, {"enum", `{"a":1}`, ""}, {"enum", `["a"]`, "entity.contact"}, {"reference", `["a"]`, "entity.contact"}} {
		var result sql.NullBool
		scalarSQL(t, f.operator.QueryRow(`SELECT runtime.scalar_options_safe($1,$2::jsonb,$3)`, args...).Scan(&result))
		if !result.Valid || result.Bool {
			t.Fatal("review invalid options returned true or unknown")
		}
	}
	for _, value := range []string{"11111111-1111-1111-1111-111111111111\n", "11111111-1111-1111-1111-111111111111\r", "11111111-1111-1111-1111-111111111111 ", "{11111111-1111-1111-1111-111111111111}", "urn:uuid:11111111-1111-1111-1111-111111111111"} {
		raw, _ := json.Marshal(value)
		var count int
		err := f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_project_value($1::jsonb,'reference',false,'[]')`, string(raw)).Scan(&count)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "22023" {
			t.Fatal("review permissive reference parser admitted noncanonical text")
		}
	}
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
		var before sql.NullString
		scalarSQL(t, f.operator.QueryRow(`SELECT collation_version FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field).Scan(&before))
		_, err := f.operator.Exec(`UPDATE runtime.scalar_indexes SET collation_version='review-drift' WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field)
		scalarSQL(t, err)
		if response := m5Call(h, base+"/query", token, choiceQuery(field, false)); response.Code != 503 {
			t.Fatal("review choice collation drift executed", response.Code)
		}
		_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET collation_version=$4 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field, before)
		scalarSQL(t, err)
	}
	other, otherHandler, otherBase, otherToken, _ := choiceFixture(t)
	m5Create(t, otherHandler, otherBase, otherToken, "Other tenant choice")
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, otherHandler, otherBase+"/indexes/"+field, otherToken)
	}
	principal, _ := f.verifier.Verify(token)
	scalarSQL(t, postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		for _, table := range []string{"scalar_indexes", "scalar_projections"} {
			var count int
			var own bool
			if err := q.QueryRowContext(ctx, `SELECT count(*),bool_and(tenant_id=$1) FROM runtime.`+table+` WHERE field_type IN('enum','reference')`, f.tenantA).Scan(&count, &own); err != nil {
				return err
			}
			if count != 2 || !own {
				return errors.New("review populated choice tenant isolation failed")
			}
		}
		if _, err := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, other.tenantA); err != nil {
			return err
		}
		var count int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.scalar_projections`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("review forged tenant context exposed choice rows")
		}
		return nil
	}))
	for _, binding := range []string{"DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db, err := postgres.Open(os.Getenv(binding))
		scalarSQL(t, err)
		var database, role string
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&database, &role))
		if !strings.HasPrefix(database, "m8_delivery_") {
			db.Close()
			t.Fatal("review private-port probe requires owned database")
		}
		for _, call := range []string{`SELECT runtime.scalar_options_safe('enum','["a"]','')`, `SELECT value_text FROM runtime.scalar_project_value('"a"','enum',true,'["a"]')`} {
			var v string
			err := db.QueryRow(call).Scan(&v)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "42501" {
				db.Close()
				t.Fatal("review private choice helper leaked execute")
			}
		}
		db.Close()
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	scalarSQL(t, err)
	defer worker.Close()
	_, err = f.operator.Exec(`GRANT SELECT(field_id) ON runtime.scalar_projections TO framework_worker`)
	scalarSQL(t, err)
	var grantsSafe bool
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&grantsSafe))
	if grantsSafe {
		t.Fatal("review choice worker grant drift not detected")
	}
	rows, err := worker.Query(`SELECT field_id FROM runtime.scalar_projections`)
	scalarSQL(t, err)
	visible := rows.Next()
	scalarSQL(t, rows.Err())
	rows.Close()
	if visible {
		t.Fatal("review restrictive worker RLS exposed populated choices")
	}
	_, err = f.operator.Exec(`REVOKE SELECT(field_id) ON runtime.scalar_projections FROM framework_worker`)
	scalarSQL(t, err)
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&grantsSafe))
	if !grantsSafe {
		t.Fatal("review worker drift recovery failed")
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RChoiceReviewActualUnknownCommitAndCheckpointReconciliation(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := choiceFixture(t)
	m5Create(t, h, base, token, "Choice commit witness")
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		path := base + "/indexes/" + field
		index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
		proxy, observed := m8DropCommitResponse(t, os.Getenv("DATABASE_URL"))
		db, err := postgres.Open(proxy)
		scalarSQL(t, err)
		clone := *f
		clone.db = db
		scalarReviewTarget(t, &clone)
		scalarSQL(t, postgres.WithRuntime(context.Background(), db, func(context.Context, postgres.Executor) error { return nil }))
		faulted := m5Handler(t, &clone, policy.Native{}, audit.Postgres{}, true)
		response := m5Call(faulted, path+"/build", token, map[string]any{"expected_checkpoint": index.Checkpoint})
		db.Close()
		if response.Code != 503 || !observed.Load() {
			t.Fatal("review actual choice COMMIT reply loss absent", response.Code)
		}
		var committed data.NumericIndex
		status := m3Call(h, "GET", path, token, 0, nil)
		if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &committed) != nil || committed.Checkpoint != index.Checkpoint+1 || committed.State != "VERIFYING" || committed.ScannedRows != 1 {
			t.Fatal("review choice checkpoint did not reconcile committed copy")
		}
		if replay := m9rIndex(t, h, path, token, index, "build"); replay != committed {
			t.Fatal("review choice lost COMMIT repeated effects")
		}
		m9rIndexReady(t, h, path, token)
	}
}

func TestM9RChoiceReviewCopyOverlapsValidUpdateAndDelete(t *testing.T) {
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		t.Run(field, func(t *testing.T) {
			m8Database(t)
			f, h, base, token, app := choiceFixture(t)
			updated := m5Create(t, h, base, token, "Choice updated")
			deleted := m5Create(t, h, base, token, "Choice deleted")
			target := m5Create(t, h, base, token, "Choice target")
			path := base + "/indexes/" + field
			index := m9rIndex(t, h, path, token, data.NumericIndex{}, "register")
			scope := scalarScope(t, h, base, token)
			principal, _ := f.verifier.Verify(token)
			copied, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			result := make(chan error, 1)
			go func() {
				result <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
					access, err := m3Scope(f, ctx, q, principal, f.tenantA)
					if err != nil {
						return err
					}
					copiedIndex, err := (data.Store{}).BuildFieldIndex(ctx, q, scope, access, "entity.contact", field, index.Checkpoint)
					if err != nil {
						return err
					}
					if copiedIndex.State != "VERIFYING" || copiedIndex.ScannedRows != 3 {
						return errors.New("review choice copy witness mismatch")
					}
					close(copied)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return errors.New("review choice copy deadline")
					}
				})
			}()
			select {
			case <-copied:
			case err := <-result:
				scalarSQL(t, err)
				t.Fatal("review choice copy did not pause")
			case <-time.After(2 * time.Second):
				t.Fatal("review choice copy timed out")
			}
			results := make(chan int, 2)
			patch := map[string]any{"field.contact.status": "inactive", "field.contact.parent": target.ID}
			go func() {
				results <- m5Call(h, base+"/commands", token, data.Command{Key: uuid(t), Operation: "patch", ID: updated.ID, ExpectedVersion: 1, Values: patch}).Code
			}()
			go func() {
				results <- m5Call(h, base+"/commands", token, data.Command{Key: uuid(t), Operation: "delete", ID: deleted.ID, ExpectedVersion: 1}).Code
			}()
			deadline := time.Now().Add(400 * time.Millisecond)
			overlap := false
			for time.Now().Before(deadline) {
				var blocked int
				scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE usename='framework_app' AND datname=current_database() AND cardinality(pg_blocking_pids(pid))>0`).Scan(&blocked))
				if blocked >= 2 {
					overlap = true
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			unblock()
			scalarSQL(t, <-result)
			for range 2 {
				if status := <-results; status != 200 {
					t.Fatal("review overlapping choice writer rejected", status)
				}
			}
			if !overlap {
				t.Fatal("review choice source lock overlap was not observed")
			}
			m9rIndexReady(t, h, path, token)
			var wrong, removed int
			scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.scalar_projections p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.field_id=$3 AND p.id=r.id WHERE r.tenant_id=$1 AND r.app_id=$2 AND (p.id IS NULL OR p.source_version<>r.row_version OR p.value_text IS DISTINCT FROM r.custom_data->>$3 OR p.overflow)`, f.tenantA, app, field).Scan(&wrong))
			scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, deleted.ID).Scan(&removed))
			if wrong != 0 || removed != 0 {
				t.Fatal("review choice backfill lost valid concurrent value/version/delete")
			}
		})
	}
}

func TestM9RChoiceReviewMaskedCursorsAndRetiredLiveACL(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := choiceFixture(t)
	first := m5Create(t, h, base, token, "Choice parent")
	second := m5Create(t, h, base, token, "Choice child")
	third := m5Create(t, h, base, token, "Choice other")
	if response := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: second.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.parent": first.ID, "field.contact.status": "inactive"}}); response.Code != 200 {
		t.Fatal("review choice reference fixture", response.Code)
	}
	limited, viewer, _, _ := r5dLimited(t, f, app, token)
	collect := func(field string) []data.Record {
		q := choiceQuery(field, false)
		q.Limit = 1
		q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance"}}
		q.Filter = &data.Filter{Op: "in", Field: "field.contact.status", Value: json.RawMessage(`["active","inactive"]`)}
		rows := choicePages(t, limited, base, viewer, q)
		for _, r := range rows {
			r5dMasked(t, r)
			if related := r.Related[q.Join.Relationship]; related != nil {
				r5dMasked(t, *related)
			}
		}
		return rows
	}
	beforeEnum, beforeReference := collect("field.contact.status"), collect("field.contact.parent")
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	if !reflect.DeepEqual(beforeEnum, collect("field.contact.status")) || !reflect.DeepEqual(beforeReference, collect("field.contact.parent")) {
		t.Fatal("review choice masked filtered joined cursor pages changed")
	}
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	dataset := m9rDatasetCreate(t, h, root+"/datasets", token, "ADOPT", witness)
	path := root + "/datasets/" + dataset.ID
	for dataset.State != "OPEN" {
		response := m5Call(h, path+"/verify", token, struct{}{})
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &dataset) != nil {
			t.Fatal("review choice adoption rejected", response.Code)
		}
	}
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
	target := shadowSource(t, h, token, f.tenantA, app, 3, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, h, token, path, ready, "activate")
	current, retired := path+"/bindings/2/entities/entity.contact", path+"/bindings/1/entities/entity.contact"
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		m9rIndexReady(t, h, current+"/indexes/"+field, token)
	}
	access := r5cChange(t, h, current+"/records/"+third.ID+"/access", token, recordOwner(f.viewer, ""), 200)
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	for _, field := range []string{"field.contact.status", "field.contact.parent"} {
		for _, b := range []string{current, retired} {
			q := choiceQuery(field, false)
			q.Limit = 1
			rows := choicePages(t, owners, b, token, q)
			if len(rows) != 2 {
				t.Fatal("review choice canonical owner scope after limit")
			}
			for _, r := range rows {
				if r.ID == third.ID {
					t.Fatal("review choice retained index leaked old owner")
				}
			}
		}
	}
	shares := m5Handler(t, f, scopedPolicy{mode: "share"}, audit.Postgres{}, true)
	no := false
	access = r5cChange(t, h, current+"/records/"+third.ID+"/access", token, data.AccessCommand{Operation: "share", ExpectedVersion: access.Version, UserID: f.alice, CanWrite: &no}, 200)
	for _, b := range []string{current, retired} {
		rows := choicePages(t, shares, b, token, choiceQuery("field.contact.status", false))
		if len(rows) != 1 || rows[0].ID != third.ID {
			t.Fatal("review choice current share missing in retained index")
		}
	}
	access = r5cChange(t, h, current+"/records/"+third.ID+"/access", token, data.AccessCommand{Operation: "unshare", ExpectedVersion: access.Version, UserID: f.alice}, 200)
	for _, b := range []string{current, retired} {
		if rows := choicePages(t, shares, b, token, choiceQuery("field.contact.status", false)); len(rows) != 0 {
			t.Fatal("review choice revoked share remains indexed")
		}
	}
	if response := m5Call(h, current+"/commands", token, data.Command{Operation: "delete", ID: third.ID, ExpectedVersion: access.Version}); response.Code != 200 {
		t.Fatal("review choice tombstone", response.Code)
	}
	for _, b := range []string{current, retired} {
		rows := choicePages(t, h, b, token, choiceQuery("field.contact.parent", false))
		if len(rows) != 2 {
			t.Fatal("review choice retained projection resurrected tombstone")
		}
	}
}
