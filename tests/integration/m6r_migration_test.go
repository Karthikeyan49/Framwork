//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/config"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestM6RLegacyLateCommitMigrationReadEquivalenceAndWriteSeal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	c, e := config.Load(os.Getenv)
	if e != nil {
		t.Fatal("configuration")
	}
	adminURL, e := url.Parse(c.DatabaseURL)
	if e != nil {
		t.Fatal("admin binding")
	}
	adminURL.User = url.UserPassword("postgres", os.Getenv("POSTGRES_PASSWORD"))
	admin, e := postgres.Open(adminURL.String())
	if e != nil {
		t.Fatal("admin unavailable")
	}
	defer admin.Close()
	name := "m6r_cutover_" + strings.ReplaceAll(uuid(t), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = admin.ExecContext(ctx, `CREATE DATABASE `+quoted+` OWNER framework_migrator`); e != nil {
		t.Fatal("disposable database creation")
	}
	t.Cleanup(func() {
		cleanup, e := postgres.Open(adminURL.String())
		if e != nil {
			t.Error("cleanup connection")
			return
		}
		defer cleanup.Close()
		if _, e := cleanup.ExecContext(context.Background(), `DROP DATABASE `+quoted+` WITH (FORCE)`); e != nil {
			t.Error("disposable database cleanup")
		}
	})
	if _, e = admin.ExecContext(ctx, `REVOKE ALL ON DATABASE `+quoted+` FROM PUBLIC`); e != nil {
		t.Fatal("database privileges")
	}
	if _, e = admin.ExecContext(ctx, `GRANT CONNECT ON DATABASE `+quoted+` TO framework_app,framework_worker,framework_dispatcher`); e != nil {
		t.Fatal("runtime connect")
	}
	migrationURL, _ := url.Parse(os.Getenv("MIGRATION_DATABASE_URL"))
	migrationURL.Path = "/" + name
	apiURL, _ := url.Parse(c.DatabaseURL)
	apiURL.Path = "/" + name
	m, e := postgres.Open(migrationURL.String())
	if e != nil {
		t.Fatal("migration binding")
	}
	defer m.Close()
	provider, e := goose.NewProvider(goose.DialectPostgres, m, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if e != nil {
		t.Fatal("migration provider")
	}
	if _, e = provider.UpTo(ctx, 8); e != nil {
		t.Fatal("legacy chain")
	}
	t.Setenv("DATABASE_URL", apiURL.String())
	t.Setenv("MIGRATION_DATABASE_URL", migrationURL.String())
	f := newIAM(t)
	token := f.token(t, f.aliceSubject, nil)
	registry := m3Registry(t)
	h := m5Handler(t, f, policy.Native{}, audit.Postgres{}, true)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	app := "app.legacy_" + strings.ReplaceAll(uuid(t), "-", "")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 0, raw); w.Code != 201 {
		t.Fatal("legacy metadata")
	}
	source, e := registry.Parse(raw)
	if e != nil {
		t.Fatal("source")
	}
	program, d := compiler.Compile(source, compiler.PlatformCatalog())
	if !program.Valid() {
		t.Fatal(d)
	}
	p, e := f.verifier.Verify(token)
	if e != nil {
		t.Fatal("principal")
	}
	bind := func(q postgres.Executor) (tenant.Access, error) {
		u, e := (identity.Store{}).Resolve(ctx, q, p)
		if e != nil {
			return tenant.Access{}, e
		}
		if e = (tenant.Store{}).BindActor(ctx, q, u); e != nil {
			return tenant.Access{}, e
		}
		return (tenant.Store{}).Resolve(ctx, q, u, f.tenantA)
	}
	if e = postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := bind(q); e != nil {
			return e
		}
		_, e := q.ExecContext(ctx, `INSERT INTO runtime.preview_datasets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,1,$3)`, f.tenantA, app, program.Hash())
		return e
	}); e != nil {
		t.Fatal("legacy dataset")
	}
	legacy, e := f.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal("legacy writer")
	}
	defer legacy.Rollback()
	if _, e = bind(legacy); e != nil {
		t.Fatal("legacy context")
	}
	id := uuid(t)
	values, _ := json.Marshal(m5Values("Retained legacy record"))
	if _, e = legacy.ExecContext(ctx, `INSERT INTO runtime.preview_records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data) VALUES($1,$2,1,$3,'entity.contact',$4,$5)`, f.tenantA, app, program.Hash(), id, values); e != nil {
		t.Fatal("legacy insert")
	}
	// M9 can copy without seeing the uncommitted row. The final barrier must
	// preserve this late committed legacy row before declaring cutover complete.
	copyDone := make(chan error, 1)
	go func() { _, e := provider.UpTo(ctx, 9); copyDone <- e }()
	// Privilege cutover waits for the old transaction. Commit while that cutover
	// is waiting; the post-revocation barrier must reconcile its copied snapshot.
	deadline := time.Now().Add(4 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		var count int
		if e = admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=$1 AND usename='framework_migrator' AND wait_event_type='Lock'`, name).Scan(&count); e != nil {
			t.Fatal("cutover lock observation")
		}
		if count > 0 {
			waiting = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("legacy cutover did not wait for active writer")
	}
	if e = legacy.Commit(); e != nil {
		t.Fatal("legacy commit")
	}
	if e = <-copyDone; e != nil {
		t.Fatal("initial additive copy")
	}
	if _, e = provider.Up(ctx); e != nil {
		t.Fatal("V3 cutover chain")
	}
	if second, e := provider.Up(ctx); e != nil || len(second) != 0 {
		t.Fatal("migration idempotence")
	}
	var sourceCount, destCount int
	if e = m.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM runtime.preview_records),(SELECT count(*) FROM runtime.records)`).Scan(&sourceCount, &destCount); e != nil || sourceCount != 1 || destCount != 1 {
		t.Fatal("late commit lost")
	}
	var legacyWritable bool
	if e = f.db.QueryRowContext(ctx, `SELECT has_any_column_privilege('runtime.preview_records','INSERT,UPDATE') OR has_table_privilege('runtime.preview_records','INSERT,UPDATE,DELETE')`).Scan(&legacyWritable); e != nil || legacyWritable {
		t.Fatal("legacy writes not sealed")
	}
	scope, e := data.NewScope(program, 1)
	if e != nil {
		t.Fatal(e)
	}
	if e = postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
		a, e := bind(q)
		if e != nil {
			return e
		}
		guard := data.Guard(func(context.Context, string, data.Resource) error { return nil })
		resolve := data.ScopeResolver(func(context.Context, string, data.Resource) (policy.DataScope, error) {
			return policy.DataScope{Kind: "ALL"}, nil
		})
		old, e := (data.LegacyAdapter{}).Query(ctx, q, scope, a, "entity.contact", m5Query(), guard, resolve)
		if e != nil {
			return e
		}
		current, e := (data.Store{}).Query(ctx, q, scope, a, "entity.contact", m5Query(), guard, resolve)
		if e != nil {
			return e
		}
		left, _ := json.Marshal(old)
		right, _ := json.Marshal(current)
		if string(left) != string(right) {
			t.Fatal("legacy read equivalence")
		}
		return nil
	}); e != nil {
		t.Fatal("adapter read equivalence")
	}
	// Existing typed/operator storage is retained; tenant runtime still cannot DDL.
	if _, e = f.db.ExecContext(ctx, `ALTER TABLE runtime.records ADD unsafe_custom_field text`); e == nil {
		t.Fatal("tenant shared DDL allowed")
	}
}
