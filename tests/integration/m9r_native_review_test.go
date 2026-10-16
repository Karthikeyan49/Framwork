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
	"fmt"
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

// Compare actual SQL ordering, not Go's nanosecond ordering: SQL rounds the
// projection and the fallback to the same microsecond while cursors keep JSON.
func TestM9RNativeReviewBeforeEpochRoundingAndRawCursorOracle(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	m5Create(t, h, base, token, "Native oracle seed")
	scope := scalarScope(t, h, base, token)
	instants := []any{
		"1969-12-31T23:59:59.123456400Z", "1969-12-31T23:59:59.123456499Z", "1969-12-31T23:59:59.123456500Z", "1969-12-31T23:59:59.123456501Z", "1969-12-31T23:59:59.123456600Z",
		"1969-12-31T23:59:59.999999499Z", "1969-12-31T23:59:59.999999500Z", "1969-12-31T23:59:59.999999999Z", "1970-01-01T00:00:00Z",
		"0001-01-01T00:00:00.000000001Z", "0001-01-01T00:00:00.000000499Z", "0001-01-01T00:00:00.000000500Z", "0001-01-01T00:00:00.000000501Z",
		"9999-12-31T23:59:59.999999499Z", "9999-12-31T23:59:59.999999500Z", "9999-12-31T23:59:59.999999999Z",
		"2024-01-01T00:00:00.123456789+15:59", "2024-01-01T00:00:00.123456789-15:59", nil,
	}
	for i, instant := range instants {
		values := m5Values("Native SQL oracle")
		values[nativeFields[0]], values[nativeFields[1]] = instant, i%2 == 0
		// Reverse IDs relative to insertion/time so an accidental nanosecond tie
		// breaker cannot pass solely because record IDs follow the fraction order.
		_, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id) VALUES($1,$2,2,$3,'entity.contact',$4,$5::jsonb,$6,$6,$6)`, f.tenantA, app, scope.Hash(), fmt.Sprintf("99999999-9999-4999-8999-%012d", 100-i), responseRecordValues(t, values), f.alice)
		scalarSQL(t, err)
	}
	before := map[string][]data.Record{}
	for _, field := range nativeFields {
		for _, desc := range []bool{false, true} {
			q := nativeQuery(field, desc)
			q.Limit = 1
			key := fmt.Sprint(field, desc)
			before[key] = choicePages(t, h, base, token, q)
			direction, cast := "ASC", "timestamptz"
			if desc {
				direction = "DESC"
			}
			if field == nativeFields[1] {
				cast = "boolean"
			}
			rows, err := f.operator.Query(`SELECT id::text FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 ORDER BY (custom_data->>$3)::`+cast+` `+direction+` NULLS LAST,id ASC`, f.tenantA, app, field)
			scalarSQL(t, err)
			var ids []string
			for rows.Next() {
				var id string
				scalarSQL(t, rows.Scan(&id))
				ids = append(ids, id)
			}
			scalarSQL(t, rows.Err())
			rows.Close()
			if len(ids) != len(before[key]) {
				t.Fatal("review native SQL oracle count")
			}
			for i, id := range ids {
				if before[key][i].ID != id {
					t.Fatal("review native fallback differs from SQL cast oracle")
				}
			}
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
				t.Fatal("review native raw cursor/SQL precision diverged")
			}
		}
	}
}

func TestM9RNativeReviewStrictPrivateContractsAndPopulatedRoles(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	m5Create(t, h, base, token, "Native private source")
	for _, field := range nativeFields {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	for _, raw := range []string{`"2024-01-01T00:00:00Z\n"`, `"2024-01-01T00:00:00Z "`, `"2024-01-01t00:00:00z"`, `"2024-01-01T00:00:00,1Z"`, `"2024-01-01T00:00:00.1234567890Z"`, `"2024-01-01T00:00:00+15:60"`, `"0000-12-31T23:59:59-00:01"`, `"0001-01-01T00:00:00.999999999+00:01"`, `"9999-12-31T23:59:59.000000001-00:01"`, `"2023-02-29T00:00:00Z"`, `"2024-01-01T00:00:00+16:00"`, `"epoch"`, `{}`, `[]`, `0`} {
		var n int
		err := f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_native_value($1::jsonb,'datetime',false,'[]')`, raw).Scan(&n)
		nativePGCode(t, err, "22023")
	}
	for _, args := range [][]any{{`false`, nil, false, `[]`}, {`false`, "bool", nil, `[]`}, {`false`, "bool", false, nil}, {`false`, "bool", false, `null`}, {`false`, "bool", false, `[false]`}, {`"t"`, "bool", false, `[]`}, {`"0"`, "bool", false, `[]`}, {`{}`, "bool", false, `[]`}, {`null`, "bool", true, `[]`}} {
		var n int
		err := f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_native_value($1::jsonb,$2,$3,$4::jsonb)`, args...).Scan(&n)
		nativePGCode(t, err, "22023")
	}
	for _, field := range nativeFields {
		for _, fault := range []struct {
			column string
			value  any
		}{{"schema_hash", strings.Repeat("0", 64)}, {"required", true}, {"collation_version", "review-invalid-collation"}, {"field_type", "date"}} {
			tx, err := f.operator.Begin()
			scalarSQL(t, err)
			var original any
			// Capture the original witness before separately committing a fault
			// observed by the HTTP reader; restore it after fail-closed detection.
			scalarSQL(t, tx.QueryRow(`SELECT `+fault.column+` FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field).Scan(&original))
			scalarSQL(t, tx.Rollback())
			_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET `+fault.column+`=$4 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field, fault.value)
			scalarSQL(t, err)
			if w := m5Call(h, base+"/query", token, nativeQuery(field, false)); w.Code != 503 {
				t.Fatal("review forged native witness executed", w.Code)
			}
			_, err = f.operator.Exec(`UPDATE runtime.scalar_indexes SET `+fault.column+`=$4 WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3`, f.tenantA, app, field, original)
			scalarSQL(t, err)
		}
	}
	other, otherHandler, otherBase, otherToken, _ := nativeFixture(t)
	m5Create(t, otherHandler, otherBase, otherToken, "Other native tenant")
	for _, field := range nativeFields {
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
			if err := q.QueryRowContext(ctx, `SELECT count(*),bool_and(tenant_id=$1) FROM runtime.`+table+` WHERE field_type IN('datetime','bool')`, f.tenantA).Scan(&count, &own); err != nil {
				return err
			}
			if count != 2 || !own {
				return errors.New("review native populated tenant isolation failed")
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
			return errors.New("review native forged tenant context disclosed rows")
		}
		return nil
	}))
	for _, binding := range []string{"DATABASE_URL", "WORKER_DATABASE_URL", "DISPATCHER_DATABASE_URL"} {
		db, err := postgres.Open(os.Getenv(binding))
		scalarSQL(t, err)
		var database, role string
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&database, &role))
		expectedRole := map[string]string{"DATABASE_URL": "framework_app", "WORKER_DATABASE_URL": "framework_worker", "DISPATCHER_DATABASE_URL": "framework_dispatcher"}[binding]
		if !strings.HasPrefix(database, "m8_delivery_") || role != expectedRole {
			db.Close()
			t.Fatal("review native private probe requires owned database")
		}
		var value sql.NullBool
		err = db.QueryRow(`SELECT value_boolean FROM runtime.scalar_native_value('false','bool',true,'[]')`).Scan(&value)
		nativePGCode(t, err, "42501")
		db.Close()
	}
	worker, err := postgres.Open(os.Getenv("WORKER_DATABASE_URL"))
	scalarSQL(t, err)
	defer worker.Close()
	_, err = f.operator.Exec(`GRANT SELECT(value_boolean) ON runtime.scalar_projections TO framework_worker`)
	scalarSQL(t, err)
	var grantsSafe bool
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&grantsSafe))
	if grantsSafe {
		t.Fatal("review native worker grant drift invisible")
	}
	rows, err := worker.Query(`SELECT value_boolean FROM runtime.scalar_projections`)
	scalarSQL(t, err)
	visible := rows.Next()
	scalarSQL(t, rows.Err())
	rows.Close()
	if visible {
		t.Fatal("review native restrictive worker RLS disclosed populated rows")
	}
	_, err = f.operator.Exec(`REVOKE SELECT(value_boolean) ON runtime.scalar_projections FROM framework_worker`)
	scalarSQL(t, err)
	scalarSQL(t, worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&grantsSafe))
	if !grantsSafe {
		t.Fatal("review native worker grant repair failed")
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}

func TestM9RNativeReviewNativeFiltersSessionInvarianceAndPhysicalExclusivity(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	for _, v := range []any{true, false, nil} {
		values := m5Values("Native filter source")
		values[nativeFields[0]] = "2024-02-29T23:59:59.999999500Z"
		values[nativeFields[1]] = v
		if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values}); w.Code != 200 {
			t.Fatal("review native filter source rejected", w.Code)
		}
	}
	filters := []data.Filter{
		{Op: "eq", Field: nativeFields[1], Value: json.RawMessage(`false`)},
		{Op: "neq", Field: nativeFields[1], Value: json.RawMessage(`false`)},
		{Op: "in", Field: nativeFields[1], Value: json.RawMessage(`[false]`)},
		{Op: "not_in", Field: nativeFields[1], Value: json.RawMessage(`[false]`)},
		{Op: "is_null", Field: nativeFields[1]},
		{Op: "eq", Field: nativeFields[0], Value: json.RawMessage(`"2024-03-01T00:00:00Z"`)},
	}
	before := map[string][]data.Record{}
	for _, field := range nativeFields {
		for i, filter := range filters {
			q := nativeQuery(field, false)
			q.Limit = 1
			q.Filter = &filter
			key := fmt.Sprint(field, i)
			before[key] = choicePages(t, h, base, token, q)
			expected := 1
			if i == len(filters)-1 {
				expected = 3
			}
			if len(before[key]) != expected {
				t.Fatal("review native filter semantics", i)
			}
		}
	}
	for _, field := range append(append([]string{}, nativeFields...), "field.contact.name", "field.contact.joined") {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	for _, field := range nativeFields {
		for i, filter := range filters {
			q := nativeQuery(field, false)
			q.Limit = 1
			q.Filter = &filter
			if !reflect.DeepEqual(before[fmt.Sprint(field, i)], choicePages(t, h, base, token, q)) {
				t.Fatal("review native indexed filter changed")
			}
		}
	}
	for _, filter := range []data.Filter{{Op: "gt", Field: nativeFields[1], Value: json.RawMessage(`false`)}, {Op: "eq", Field: nativeFields[1], Value: json.RawMessage(`"false"`)}, {Op: "in", Field: nativeFields[1], Value: json.RawMessage(`["false"]`)}} {
		q := nativeQuery(nativeFields[1], false)
		q.Filter = &filter
		if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
			t.Fatal("review native filter widened boolean contract", w.Code)
		}
	}
	for _, typ := range []string{"string", "date", "datetime", "bool"} {
		assignment := "value_boolean=true"
		if typ == "bool" {
			assignment = "value_instant='2024-01-01T00:00:00Z'"
		}
		_, err := f.operator.Exec(`UPDATE runtime.scalar_projections SET `+assignment+` WHERE tenant_id=$1 AND app_id=$2 AND field_type=$3`, f.tenantA, app, typ)
		nativePGCode(t, err, "23514")
	}
	for _, zone := range []string{"UTC", "Pacific/Kiritimati", "America/New_York"} {
		tx, err := f.operator.Begin()
		scalarSQL(t, err)
		_, err = tx.Exec(`SELECT set_config('TimeZone',$1,true),set_config('DateStyle','SQL, DMY',true)`, zone)
		scalarSQL(t, err)
		var same bool
		scalarSQL(t, tx.QueryRow(`SELECT value_instant = timestamptz '1970-01-01T00:00:00Z' AND value_text IS NULL AND value_date IS NULL AND value_boolean IS NULL AND NOT overflow FROM runtime.scalar_native_value('"1969-12-31T23:59:59.999999999Z"','datetime',true,'[]')`).Scan(&same))
		if !same {
			t.Fatal("review native converter inherited caller timezone/date style")
		}
		scalarSQL(t, tx.Rollback())
	}
}

// Historical artifacts are opt-in; an ordinary clean CI checkout reports SKIP.
func TestM9RNativeReviewRetainedBinaryCRUDAndFallback(t *testing.T) {
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
	f, current, base, token, app := nativeFixture(t)
	m5Create(t, current, base, token, "Legacy other")
	child := m5Create(t, current, base, token, "Legacy child")
	for _, field := range nativeFields {
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
	for _, field := range nativeFields {
		query := nativeQuery(field, false)
		if before, after := choicePages(t, current, base, token, query), choicePages(t, legacy, base, token, query); !reflect.DeepEqual(before, after) {
			t.Fatal("retained unindexed fallback differs from new indexed path")
		}
	}
	response := m5Call(legacy, base+"/commands", token, data.Command{Operation: "patch", ID: child.ID, ExpectedVersion: 1, Values: map[string]any{nativeFields[0]: "9999-12-31T23:59:59.999999999Z", nativeFields[1]: false}})
	if response.Code != 200 {
		t.Fatal("retained valid patch failed after migration50", response.Code)
	}
	var projections int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND source_version=2 AND (value_instant IS NOT NULL OR value_boolean IS NOT NULL) AND NOT overflow`, f.tenantA, app, child.ID).Scan(&projections))
	if projections != 2 {
		t.Fatal("retained writer did not maintain new natives")
	}
	created := m5Create(t, legacy, base, token, "Retained created")
	if response := m5Call(legacy, base+"/commands", token, data.Command{Operation: "delete", ID: created.ID, ExpectedVersion: 1}); response.Code != 200 {
		t.Fatal("retained delete failed", response.Code)
	}
	for _, field := range nativeFields {
		query := nativeQuery(field, true)
		if before, after := choicePages(t, current, base, token, query), choicePages(t, legacy, base, token, query); !reflect.DeepEqual(before, after) {
			t.Fatal("retained post-write fallback diverged")
		}
	}
	t.Logf("retained API startup/read/fallback/patch/create/delete verified sha256=%s", expected)
}

func TestM9RNativeReviewActualUnknownCommitAndCheckpointReconciliation(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := nativeFixture(t)
	m5Create(t, h, base, token, "Native commit witness")
	for _, field := range nativeFields {
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
			t.Fatal("review actual native COMMIT reply loss absent", response.Code)
		}
		var committed data.NumericIndex
		status := m3Call(h, "GET", path, token, 0, nil)
		if status.Code != 200 || json.Unmarshal(status.Body.Bytes(), &committed) != nil || committed.Checkpoint != index.Checkpoint+1 || committed.State != "VERIFYING" || committed.ScannedRows != 1 {
			t.Fatal("review native checkpoint did not reconcile committed copy")
		}
		if replay := m9rIndex(t, h, path, token, index, "build"); replay != committed {
			t.Fatal("review native lost COMMIT repeated effects")
		}
		m9rIndexReady(t, h, path, token)
	}
}

func TestM9RNativeReviewCopyOverlapsValidUpdateAndDelete(t *testing.T) {
	for _, field := range nativeFields {
		t.Run(field, func(t *testing.T) {
			m8Database(t)
			f, h, base, token, app := nativeFixture(t)
			updated := m5Create(t, h, base, token, "Native updated")
			deleted := m5Create(t, h, base, token, "Native deleted")
			m5Create(t, h, base, token, "Native other")
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
						return errors.New("review native copy witness mismatch")
					}
					close(copied)
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return errors.New("review native copy deadline")
					}
				})
			}()
			select {
			case <-copied:
			case err := <-result:
				scalarSQL(t, err)
				t.Fatal("review native copy did not pause")
			case <-time.After(2 * time.Second):
				t.Fatal("review native copy timed out")
			}
			results := make(chan int, 2)
			patch := map[string]any{nativeFields[0]: "1969-12-31T23:59:59.999999500Z", nativeFields[1]: true}
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
					t.Fatal("review overlapping native writer rejected", status)
				}
			}
			if !overlap {
				t.Fatal("review native source lock overlap was not observed")
			}
			m9rIndexReady(t, h, path, token)
			var wrong, removed int
			scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.scalar_projections p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.field_id=$3 AND p.id=r.id WHERE r.tenant_id=$1 AND r.app_id=$2 AND (p.id IS NULL OR p.source_version<>r.row_version OR (p.field_type='datetime' AND p.value_instant IS DISTINCT FROM (r.custom_data->>$3)::timestamptz) OR (p.field_type='bool' AND p.value_boolean IS DISTINCT FROM (r.custom_data->>$3)::boolean) OR p.overflow)`, f.tenantA, app, field).Scan(&wrong))
			scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, deleted.ID).Scan(&removed))
			if wrong != 0 || removed != 0 {
				t.Fatal("review native backfill lost valid concurrent value/version/delete")
			}
		})
	}
}

func TestM9RNativeReviewMaskedCursorsAndRetiredLiveACL(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := nativeFixture(t)
	first := m5Create(t, h, base, token, "Native parent")
	second := m5Create(t, h, base, token, "Native child")
	third := m5Create(t, h, base, token, "Native other")
	if response := m5Call(h, base+"/commands", token, data.Command{Operation: "patch", ID: second.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.parent": first.ID, "field.contact.status": "inactive"}}); response.Code != 200 {
		t.Fatal("review native reference fixture", response.Code)
	}
	limited, viewer, _, _ := r5dLimited(t, f, app, token)
	collect := func(field string) []data.Record {
		q := nativeQuery(field, false)
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
	beforeInstant, beforeBoolean := collect(nativeFields[0]), collect(nativeFields[1])
	for _, field := range nativeFields {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
	}
	if !reflect.DeepEqual(beforeInstant, collect(nativeFields[0])) || !reflect.DeepEqual(beforeBoolean, collect(nativeFields[1])) {
		t.Fatal("review native masked filtered joined cursor pages changed")
	}
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	dataset := m9rDatasetCreate(t, h, root+"/datasets", token, "ADOPT", witness)
	path := root + "/datasets/" + dataset.ID
	for dataset.State != "OPEN" {
		response := m5Call(h, path+"/verify", token, struct{}{})
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &dataset) != nil {
			t.Fatal("review native adoption rejected", response.Code)
		}
	}
	var raw []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
	target := shadowSource(t, h, token, f.tenantA, app, 3, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, h, token, path, ready, "activate")
	current, retired := path+"/bindings/2/entities/entity.contact", path+"/bindings/1/entities/entity.contact"
	for _, field := range nativeFields {
		m9rIndexReady(t, h, current+"/indexes/"+field, token)
	}
	access := r5cChange(t, h, current+"/records/"+third.ID+"/access", token, recordOwner(f.viewer, ""), 200)
	owners := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	for _, field := range nativeFields {
		for _, b := range []string{current, retired} {
			q := nativeQuery(field, false)
			q.Limit = 1
			rows := choicePages(t, owners, b, token, q)
			if len(rows) != 2 {
				t.Fatal("review native canonical owner scope after limit")
			}
			for _, r := range rows {
				if r.ID == third.ID {
					t.Fatal("review native retained index leaked old owner")
				}
			}
		}
	}
	shares := m5Handler(t, f, scopedPolicy{mode: "share"}, audit.Postgres{}, true)
	no := false
	access = r5cChange(t, h, current+"/records/"+third.ID+"/access", token, data.AccessCommand{Operation: "share", ExpectedVersion: access.Version, UserID: f.alice, CanWrite: &no}, 200)
	for _, b := range []string{current, retired} {
		rows := choicePages(t, shares, b, token, nativeQuery(nativeFields[0], false))
		if len(rows) != 1 || rows[0].ID != third.ID {
			t.Fatal("review native current share missing in retained index")
		}
	}
	access = r5cChange(t, h, current+"/records/"+third.ID+"/access", token, data.AccessCommand{Operation: "unshare", ExpectedVersion: access.Version, UserID: f.alice}, 200)
	for _, b := range []string{current, retired} {
		if rows := choicePages(t, shares, b, token, nativeQuery(nativeFields[0], false)); len(rows) != 0 {
			t.Fatal("review native revoked share remains indexed")
		}
	}
	if response := m5Call(h, current+"/commands", token, data.Command{Operation: "delete", ID: third.ID, ExpectedVersion: access.Version}); response.Code != 200 {
		t.Fatal("review native tombstone", response.Code)
	}
	for _, b := range []string{current, retired} {
		rows := choicePages(t, h, b, token, nativeQuery(nativeFields[1], false))
		if len(rows) != 2 {
			t.Fatal("review native retained projection resurrected tombstone")
		}
	}
}
