//go:build integration

package integration

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

type scalarReviewTraceKey struct{}
type scalarReviewTraceStart struct {
	stage string
	at    time.Time
}
type scalarReviewDiagnosticTrace struct {
	t               *testing.T
	mu              sync.Mutex
	maximum         map[string]time.Duration
	phase           string
	statement       string
	args            []any
	failedStatement string
	failedArgs      []any
}

func (o *scalarReviewDiagnosticTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	stage := "other"
	switch {
	case strings.HasPrefix(d.SQL, "WITH selected_page AS MATERIALIZED") && strings.Contains(d.SQL, "JOIN runtime.scalar_projections"):
		stage = "indexed_page"
	case strings.HasPrefix(d.SQL, "WITH selected_page AS MATERIALIZED"), strings.HasPrefix(d.SQL, "SELECT r.id::text"):
		stage = "unindexed_page"
	case strings.Contains(d.SQL, "scalar_collation_safe"):
		stage = "scalar_witness"
	case strings.Contains(d.SQL, "AND NOT EXISTS(SELECT 1 FROM runtime.scalar_projections"):
		stage = "overflow_probe"
	case strings.HasPrefix(d.SQL, "SELECT rolsuper OR rolbypassrls"):
		stage = "role_admission"
	case strings.EqualFold(d.SQL, "commit"):
		stage = "commit"
	case strings.EqualFold(d.SQL, "rollback"):
		stage = "rollback"
	}
	if stage == "indexed_page" || stage == "unindexed_page" {
		o.mu.Lock()
		o.statement = d.SQL
		o.args = append([]any(nil), d.Args...)
		o.mu.Unlock()
	}
	return context.WithValue(ctx, scalarReviewTraceKey{}, scalarReviewTraceStart{stage, time.Now()})
}
func (o *scalarReviewDiagnosticTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	start, ok := ctx.Value(scalarReviewTraceKey{}).(scalarReviewTraceStart)
	if !ok {
		return
	}
	elapsed := time.Since(start.at)
	o.mu.Lock()
	if o.maximum == nil {
		o.maximum = map[string]time.Duration{}
	}
	if elapsed > o.maximum[start.stage] {
		o.maximum[start.stage] = elapsed
	}
	o.mu.Unlock()
	if d.Err != nil {
		o.mu.Lock()
		phase := o.phase
		if start.stage == "indexed_page" || start.stage == "unindexed_page" {
			o.failedStatement = o.statement
			o.failedArgs = append([]any(nil), o.args...)
		}
		o.mu.Unlock()
		kind, code := "driver", "none"
		var pg *pgconn.PgError
		if errors.As(d.Err, &pg) {
			kind, code = "postgres", pg.Code
		}
		if errors.Is(d.Err, driver.ErrBadConn) {
			kind = "bad_connection"
		}
		if errors.Is(d.Err, context.DeadlineExceeded) {
			kind = "context_deadline"
		}
		if errors.Is(d.Err, context.Canceled) {
			kind = "context_canceled"
		}
		o.t.Logf("review_sql_failure stage=%s phase=%s class=%s sqlstate=%s duration_ms=%.3f", start.stage, phase, kind, code, float64(elapsed.Microseconds())/1000)
	}
}

// The review never assumes that changing an environment URI changed the actual
// driver target. Check the effective database and role before targeted probes.
func scalarReviewTarget(t *testing.T, f *iamFixture) {
	t.Helper()
	for _, item := range []struct {
		variable, role string
		operator       bool
	}{{"DATABASE_URL", "framework_app", false}, {"MIGRATION_DATABASE_URL", "framework_migrator", true}} {
		config, err := pgx.ParseConfig(os.Getenv(item.variable))
		if err != nil {
			t.Fatal("review driver configuration unavailable")
		}
		db := f.db
		if item.operator {
			db = f.operator
		}
		var database, role string
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&database, &role))
		if database != config.Database || role != item.role {
			t.Fatal("review effective database or role mismatch")
		}
	}
}

func TestM9RScalarReviewCopyOverlapsUpdateAndDelete(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	updated := scalarCreate(t, h, base, token, "Before update")
	deleted := scalarCreate(t, h, base, token, "Before delete")
	scalarSeed(t, f, app, 19)
	path := base + "/indexes/field.contact.memo0"
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
			built, err := (data.Store{}).BuildFieldIndex(ctx, q, scope, access, "entity.contact", "field.contact.memo0", index.Checkpoint)
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
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records r LEFT JOIN runtime.scalar_projections p ON p.tenant_id=r.tenant_id AND p.app_id=r.app_id AND p.metadata_revision=r.metadata_revision AND p.ir_hash=r.ir_hash AND p.entity_id=r.entity_id AND p.field_id='field.contact.memo0' AND p.id=r.id WHERE r.tenant_id=$1 AND r.app_id=$2 AND (p.id IS NULL OR p.source_version<>r.row_version OR p.overflow<>(octet_length(r.custom_data->>'field.contact.memo0')>512))`, f.tenantA, app).Scan(&wrong))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, deleted.ID).Scan(&removed))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_projections WHERE tenant_id=$1 AND app_id=$2 AND id=$3 AND overflow AND value_text IS NULL AND source_version=2`, f.tenantA, app, updated.ID).Scan(&overflow))
	if wrong != 0 || removed != 0 || overflow != 1 {
		t.Fatal("review concurrent copy lost current value/version/delete")
	}
}

func TestM9RScalarReviewActualUnknownCommitReconciliation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	scalarCreate(t, h, base, token, "Commit witness")
	scalarSeed(t, f, app, 10)
	path := base + "/indexes/field.contact.memo0"
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

func TestM9RScalarReviewMaskedJoinFilterPaginationAndRawManager(t *testing.T) {
	m8Database(t)
	f, owner, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	parent := scalarCreate(t, owner, base, token, "Parent")
	child := scalarCreate(t, owner, base, token, "Child")
	if w := m5Call(owner, base+"/commands", token, data.Command{Operation: "patch", ID: child.ID, ExpectedVersion: 1, Values: map[string]any{"field.contact.parent": parent.ID}}); w.Code != 200 {
		t.Fatal("review related fixture", w.Code)
	}
	scalarSeed(t, f, app, 31)
	h, viewer, _, _ := r5dLimited(t, f, app, token)
	collect := func(field string) []data.Record {
		q := m5Query()
		q.Projection = append(q.Projection, "field.contact.memo0", "field.contact.joined")
		q.Sort = &data.Sort{Field: field}
		q.Filter = &data.Filter{Op: "and", Args: []data.Filter{{Op: "gte", Field: "field.contact.memo0", Value: json.RawMessage(`"Alpha"`)}, {Op: "gte", Field: "field.contact.joined", Value: json.RawMessage(`"2024-01-01"`)}}}
		q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name", "field.contact.balance"}}
		q.Limit = 7
		rows := []data.Record{}
		for range 10 {
			page := m5Page(t, m5Call(h, base+"/query", viewer, q))
			for _, r := range page.Records {
				r5dMasked(t, r)
				if related := r.Related[q.Join.Relationship]; related != nil {
					r5dMasked(t, *related)
				}
			}
			rows = append(rows, page.Records...)
			if page.Next == "" {
				return rows
			}
			q.After = page.Next
		}
		t.Fatal("review masked join pagination did not finish")
		return nil
	}
	textBefore, dateBefore := collect("field.contact.memo0"), collect("field.contact.joined")
	if len(textBefore) < 8 || len(dateBefore) != len(textBefore) {
		t.Fatal("review masked cursor fixture did not span pages")
	}
	for _, field := range []string{"field.contact.memo0", "field.contact.joined", "field.contact.name"} {
		m9rIndexReady(t, owner, base+"/indexes/"+field, token)
	}
	if !reflect.DeepEqual(textBefore, collect("field.contact.memo0")) || !reflect.DeepEqual(dateBefore, collect("field.contact.joined")) {
		t.Fatal("review index changed masked filtered joined pages")
	}
	for _, query := range []data.Query{{Projection: []string{"field.contact.id"}, Limit: 1, Sort: &data.Sort{Field: "field.contact.name"}}, {Projection: []string{"field.contact.id"}, Limit: 1, Filter: &data.Filter{Op: "is_null", Field: "field.contact.name"}}} {
		if response := m5Call(h, base+"/query", viewer, query); response.Code != 403 {
			t.Fatal("review hidden indexed predicate admitted", response.Code)
		}
	}
	viewerPrincipal, _ := f.verifier.Verify(viewer)
	err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		access, err := m3Scope(f, ctx, q, viewerPrincipal, f.tenantA)
		if err != nil {
			return err
		}
		var hash, schema string
		if err = q.QueryRowContext(ctx, `SELECT ir_hash,schema_hash FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 LIMIT 1`, f.tenantA, app).Scan(&hash, &schema); err != nil {
			return err
		}
		_, err = q.ExecContext(ctx, `SELECT runtime.register_scalar_index($1,$2,2,$3,'entity.contact','field.contact.memo0',$4,2,$3)`, access.TenantID(), app, hash, schema)
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatal("review direct scalar SQL manager boundary failed")
	}
}

func TestM9RScalarReviewAutocommitRepeatableReadPoolAndUnicodeBoundaries(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	scalarCreate(t, h, base, token, "Autocommit first")
	scalarCreate(t, h, base, token, strings.Repeat("界", 200))
	m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", token)
	scope := scalarScope(t, h, base, token)
	principal, _ := f.verifier.Verify(token)
	var access tenant.Access
	scalarSQL(t, postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		var err error
		access, err = m3Scope(f, ctx, q, principal, f.tenantA)
		return err
	}))
	config, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("review pool configuration unavailable")
	}
	config.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool := stdlib.OpenDB(*config)
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	conn, err := pool.Conn(context.Background())
	scalarSQL(t, err)
	defer conn.Close()
	var database, role, isolation string
	scalarSQL(t, conn.QueryRowContext(context.Background(), `SELECT current_database(),session_user,current_setting('transaction_isolation')`).Scan(&database, &role, &isolation))
	if database != config.Database || role != "framework_app" || isolation != "repeatable read" {
		t.Fatal("review autocommit pool effective target or isolation mismatch")
	}
	_, err = conn.ExecContext(context.Background(), `SELECT set_config('app.auth_issuer','https://test-issuer.example',false),set_config('app.auth_subject',$1,false),set_config('app.actor_id',$2,false),set_config('app.tenant_id',$3,false)`, f.aliceSubject, f.alice, f.tenantA)
	scalarSQL(t, err)
	observed := &scalarOpaque{Executor: conn}
	query := m5Query()
	query.Projection = append(query.Projection, "field.contact.memo0")
	query.Sort = &data.Sort{Field: "field.contact.memo0"}
	query.Limit = 100
	allow := func(context.Context, string, data.Resource) error { return nil }
	all := func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	}
	page, err := (data.Store{}).Query(context.Background(), observed, scope, access, "entity.contact", query, allow, all)
	scalarSQL(t, err)
	if len(page.Records) != 2 || observed.scalarCalls != 0 || postgres.RuntimeSnapshot(conn) || postgres.RuntimeSnapshot(pool) {
		t.Fatal("review autocommit RR acquired unsafe overflow admission")
	}
	for _, c := range []struct {
		value, typ      string
		valid, overflow bool
	}{
		{strings.Repeat("😀", 128), "string", true, false}, {strings.Repeat("😀", 129), "string", true, true},
		{strings.Repeat("😀", 1024), "string", true, true}, {strings.Repeat("😀", 1025), "string", false, false},
		{"0001-01-01", "date", true, false}, {"2000-02-29", "date", true, false}, {"1900-02-29", "date", false, false},
		{"2024-00-01", "date", false, false}, {"2024-01-00", "date", false, false}, {"2024-13-01", "date", false, false},
	} {
		raw, _ := json.Marshal(c.value)
		var overflow bool
		err := f.operator.QueryRow(`SELECT overflow FROM runtime.scalar_value($1::jsonb,$2,true)`, string(raw), c.typ).Scan(&overflow)
		if c.valid {
			scalarSQL(t, err)
			if overflow != c.overflow {
				t.Fatal("review multibyte cutoff mismatch")
			}
		} else {
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "22023" {
				t.Fatal("review invalid civil date or byte length admitted")
			}
		}
	}
}

func TestM9RScalarReviewRawReservedIdentifierRejected(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	record := scalarCreate(t, h, base, token, "Intrinsic identifier boundary")
	m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", token)
	principal, _ := f.verifier.Verify(token)
	accepted, blockedWrite := false, false
	rollback := errors.New("review deliberately rolls back unsupported registration")
	err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		var hash, schema string
		if err := q.QueryRowContext(ctx, `SELECT ir_hash,schema_hash FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 LIMIT 1`, f.tenantA, app).Scan(&hash, &schema); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `SELECT runtime.register_scalar_index($1,$2,2,$3,'entity.contact','field.contact.id',$4,2,$3)`, f.tenantA, app, hash, schema)
		if err != nil {
			return err
		}
		accepted = true
		_, err = q.ExecContext(ctx, `UPDATE runtime.records SET row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, record.ID)
		var pg *pgconn.PgError
		blockedWrite = errors.As(err, &pg) && pg.Code == "22023"
		return rollback
	})
	if accepted {
		t.Fatalf("raw scalar port admitted intrinsic ID; subsequent valid write rejected=%v; fixture rolled back=%v", blockedWrite, errors.Is(err, rollback))
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "22023" {
		t.Fatal("reserved scalar identifier rejection must be typed")
	}
	var count int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.id'`, f.tenantA, app).Scan(&count))
	if count != 0 {
		t.Fatal("review unsupported registration survived rollback")
	}
}

func TestM9RScalarReviewRegistryGuardAndIncompatibleMigration(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	var database string
	scalarSQL(t, f.operator.QueryRow(`SELECT current_database()`).Scan(&database))
	if !strings.HasPrefix(database, "m8_delivery_") {
		t.Fatal("guard drift rehearsal requires owned disposable database")
	}
	scalarCreate(t, h, base, token, "Guard witness")
	m9rIndexReady(t, h, base+"/indexes/field.contact.memo0", token)
	poison := `INSERT INTO runtime.scalar_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,required,collation_version,source_revision,source_ir_hash,created_by)
 SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,'field.contact.id',schema_hash,'string',true,collation_version,source_revision,source_ir_hash,created_by FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.memo0'`
	// Even trusted maintenance cannot accidentally bypass the table invariant.
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	_, err = tx.Exec(poison, f.tenantA, app)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" {
		_ = tx.Rollback()
		t.Fatal("review intrinsic registry CHECK failed")
	}
	scalarSQL(t, tx.Rollback())
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes DROP CONSTRAINT scalar_indexes_custom_field`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("review absent intrinsic guard remained ready")
	}
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes ADD CONSTRAINT scalar_indexes_custom_field CHECK(field_id <> 'field.' || substring(entity_id FROM 8) || '.id') NOT VALID`)
	scalarSQL(t, err)
	if (data.Store{}).Ready(context.Background(), f.db) == nil {
		t.Fatal("review unvalidated intrinsic guard remained ready")
	}
	_, err = f.operator.Exec(`ALTER TABLE runtime.scalar_indexes VALIDATE CONSTRAINT scalar_indexes_custom_field`)
	scalarSQL(t, err)
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	// Rehearse migration48 against an incompatible registry only inside this
	// disposable transaction. Execute the actual embedded UP SQL, retaining its
	// evidence-validation step. Rollback restores the valid guard and all rows.
	raw, err := migrations.Files.ReadFile("00048_m9r_scalar_intrinsic_id_guard.sql")
	if err != nil {
		t.Fatal("review forward migration unavailable")
	}
	tx, err = f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`ALTER TABLE runtime.scalar_indexes DROP CONSTRAINT scalar_indexes_custom_field`)
	scalarSQL(t, err)
	_, err = tx.Exec(poison, f.tenantA, app)
	scalarSQL(t, err)
	_, err = tx.Exec(strings.Split(string(raw), "-- +goose Down")[0])
	pg = nil
	if !errors.As(err, &pg) || pg.Code != "23514" {
		t.Fatal("review migration48 accepted incompatible registry")
	}
	scalarSQL(t, tx.Rollback())
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
	var count int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.scalar_indexes WHERE tenant_id=$1 AND app_id=$2 AND field_id='field.contact.id'`, f.tenantA, app).Scan(&count))
	if count != 0 {
		t.Fatal("review incompatible rehearsal committed poison")
	}
}

func TestM9RScalarReviewOrderingWithSafeDriverDiagnostics(t *testing.T) {
	m8Database(t)
	f, _, base, token, app := scalarFixture(t, 1)
	scalarReviewTarget(t, f)
	config, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("review trace configuration unavailable")
	}
	trace := &scalarReviewDiagnosticTrace{t: t}
	config.Tracer = trace
	db := stdlib.OpenDB(*config)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	defer db.Close()
	clone := *f
	clone.db = db
	scalarReviewTarget(t, &clone)
	h := m5Handler(t, &clone, policy.Native{}, audit.Postgres{}, true)
	scalarCreate(t, h, base, token, "Ordering diagnostic")
	scalarSeed(t, f, app, 1205)
	principal, _ := f.verifier.Verify(token)
	sourceBudgetExceeded := false
	planVerificationFailed := false
	diagnose := func() {
		trace.mu.Lock()
		statement, args := trace.failedStatement, append([]any(nil), trace.failedArgs...)
		if statement == "" {
			statement, args = trace.statement, append([]any(nil), trace.args...)
		}
		trace.mu.Unlock()
		if statement == "" {
			return
		}
		var records, projections int
		scalarSQL(t, f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.records),(SELECT count(*) FROM runtime.scalar_projections)`).Scan(&records, &projections))
		t.Logf("review_failure_counts records=%d projections=%d", records, projections)
		probes := []struct{ mode, jit string }{{"force_generic_plan", "on"}}
		if os.Getenv("M9R_SCALAR_PLAN_DIAGNOSTICS") == "true" {
			probes = append(probes, struct{ mode, jit string }{"force_custom_plan", "on"}, struct{ mode, jit string }{"force_generic_plan", "off"})
		}
		for _, probe := range probes {
			mode := probe.mode
			err := postgres.WithRuntime(context.Background(), db, func(ctx context.Context, q postgres.Executor) error {
				if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
					return err
				}
				if _, err := q.ExecContext(ctx, `SELECT set_config('plan_cache_mode',$1,true),set_config('jit',$2,true)`, mode, probe.jit); err != nil {
					return err
				}
				var raw []byte
				if err := q.QueryRowContext(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) `+statement, args...).Scan(&raw); err != nil {
					return err
				}
				var roots []any
				if json.Unmarshal(raw, &roots) != nil {
					return errors.New("review structural plan decode failed")
				}
				var walk func(any)
				var sourceVisits float64
				walk = func(v any) {
					switch n := v.(type) {
					case map[string]any:
						if n["Relation Name"] != nil {
							t.Logf("review_plan mode=%s jit=%s node=%v relation=%v index=%v rows=%v actual_rows=%v loops=%v removed=%v cost=%v", mode, probe.jit, n["Node Type"], n["Relation Name"], n["Index Name"], n["Plan Rows"], n["Actual Rows"], n["Actual Loops"], n["Rows Removed by Filter"], n["Total Cost"])
						}
						if n["Relation Name"] == "records" {
							rows, _ := n["Actual Rows"].(float64)
							loops, _ := n["Actual Loops"].(float64)
							removed, _ := n["Rows Removed by Filter"].(float64)
							sourceVisits += (rows + removed) * loops
						}
						if n["Functions"] != nil {
							t.Logf("review_jit mode=%s jit=%s functions=%v timing=%v", mode, probe.jit, n["Functions"], n["Timing"])
						}
						if n["Execution Time"] != nil {
							t.Logf("review_explain_time mode=%s jit=%s execution_ms=%v planning_ms=%v", mode, probe.jit, n["Execution Time"], n["Planning Time"])
						}
						for _, child := range n {
							walk(child)
						}
					case []any:
						for _, child := range n {
							walk(child)
						}
					}
				}
				walk(roots)
				t.Logf("review_source_visits mode=%s jit=%s visits=%.0f limit=302", mode, probe.jit, sourceVisits)
				if sourceVisits > 302 {
					sourceBudgetExceeded = true
				}
				return nil
			})
			if err != nil {
				planVerificationFailed = true
				var pg *pgconn.PgError
				code := "none"
				if errors.As(err, &pg) {
					code = pg.Code
				}
				t.Logf("review_explain_failure mode=%s jit=%s sqlstate=%s", mode, probe.jit, code)
			}
		}
	}
	defer func() {
		trace.mu.Lock()
		defer trace.mu.Unlock()
		for _, stage := range []string{"indexed_page", "unindexed_page", "scalar_witness", "overflow_probe", "role_admission", "commit", "rollback", "other"} {
			t.Logf("review_sql_max stage=%s duration_ms=%.3f", stage, float64(trace.maximum[stage].Microseconds())/1000)
		}
	}()
	collect := func(field string, desc bool, phase string) []data.Record {
		q := m5Query()
		q.Projection = append(q.Projection, "field.contact.memo0", "field.contact.joined")
		q.Limit = 100
		q.Sort = &data.Sort{Field: field, Desc: desc}
		out := []data.Record{}
		for page := range 100 {
			trace.mu.Lock()
			trace.phase = fmt.Sprintf("%s_desc%v_page%d", phase, desc, page)
			trace.mu.Unlock()
			w := m5Call(h, base+"/query", token, q)
			if w.Code != 200 {
				diagnose()
				t.Fatal("review ordering request failed", w.Code)
			}
			if field == "field.contact.joined" && !desc && strings.HasPrefix(phase, "after_") && page == 9 {
				diagnose()
				if sourceBudgetExceeded || planVerificationFailed {
					t.Fatal("review cold indexed cursor source budget not satisfied")
				}
			}
			var p data.Page
			if json.Unmarshal(w.Body.Bytes(), &p) != nil {
				t.Fatal("review ordering response invalid")
			}
			out = append(out, p.Records...)
			if p.Next == "" {
				return out
			}
			q.After = p.Next
		}
		t.Fatal("review ordering pagination did not finish")
		return nil
	}
	for _, field := range []string{"field.contact.memo0", "field.contact.joined"} {
		beforeAsc, beforeDesc := collect(field, false, "before_"+field), collect(field, true, "before_"+field)
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
		if !reflect.DeepEqual(beforeAsc, collect(field, false, "after_"+field)) || !reflect.DeepEqual(beforeDesc, collect(field, true, "after_"+field)) {
			t.Fatal("review traced ordering changed after index")
		}
	}
}
