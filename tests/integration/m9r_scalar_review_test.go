//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

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
