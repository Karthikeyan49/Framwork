//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
)

// Real tenant data is confined to uniquely named integration fixtures. M2 does
// not introduce production business entities or expose a SQL/CRUD HTTP endpoint.
type dataFixture struct {
	*iamFixture
	parent, child string
}

func newData(t *testing.T) *dataFixture {
	t.Helper()
	f := &dataFixture{iamFixture: newIAM(t)}
	name := "m2_" + strings.ReplaceAll(uuid(t), "-", "")
	f.parent = pgx.Identifier{"runtime", name}.Sanitize()
	f.child = pgx.Identifier{"runtime", name + "_child"}.Sanitize()
	tx, err := f.operator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE ` + f.parent + ` (tenant_id uuid NOT NULL,id integer NOT NULL,name text NOT NULL,custom jsonb NOT NULL DEFAULT '{}',PRIMARY KEY(tenant_id,id))`,
		`CREATE TABLE ` + f.child + ` (tenant_id uuid NOT NULL,id integer NOT NULL,parent_id integer NOT NULL,PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,parent_id) REFERENCES ` + f.parent + `(tenant_id,id))`,
	} {
		if _, err = tx.Exec(q); err != nil {
			t.Fatal("tenant fixture DDL failed")
		}
	}
	for _, name := range []string{f.parent, f.child} {
		if _, err = tx.Exec(`SELECT runtime.install_tenant_rls($1::regclass)`, name); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.operator.Exec(`DROP TABLE ` + f.child + `,` + f.parent); err != nil {
			t.Error("tenant fixture cleanup failed")
		}
	})
	for _, scope := range []struct{ subject, tenant string }{{f.aliceSubject, f.tenantA}, {f.bobSubject, f.tenantB}} {
		err = f.scoped(t, scope.subject, scope.tenant, func(ctx context.Context, q postgres.Executor) error {
			if _, err := q.ExecContext(ctx, `INSERT INTO `+f.parent+`(tenant_id,id,name,custom) VALUES($1,1,'private row','{"label":"custom"}')`, scope.tenant); err != nil {
				return err
			}
			_, err := q.ExecContext(ctx, `INSERT INTO `+f.child+`(tenant_id,id,parent_id) VALUES($1,1,1)`, scope.tenant)
			return err
		})
		if err != nil {
			t.Fatal("scoped seed failed")
		}
	}
	return f
}
func (f *dataFixture) bind(t *testing.T, ctx context.Context, q postgres.Executor, subject, selected string) error {
	t.Helper()
	p, err := f.verifier.Verify(f.token(t, subject, nil))
	if err != nil {
		return err
	}
	u, err := (identity.Store{}).Resolve(ctx, q, p)
	if err != nil {
		return err
	}
	if err = (tenant.Store{}).BindActor(ctx, q, u); err != nil {
		return err
	}
	_, err = (tenant.Store{}).Resolve(ctx, q, u, selected)
	return err
}
func (f *dataFixture) scoped(t *testing.T, subject, selected string, run func(context.Context, postgres.Executor) error) error {
	t.Helper()
	return postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := f.bind(t, ctx, q, subject, selected); err != nil {
			return err
		}
		return run(ctx, q)
	})
}
func (f *dataFixture) count(ctx context.Context, q postgres.Executor) (int, error) {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM `+f.parent).Scan(&count)
	return count, err
}

func TestM2TenantCRUDJoinAggregateAndExportIsolation(t *testing.T) {
	f := newData(t)
	for _, scope := range []struct{ subject, own, other string }{{f.aliceSubject, f.tenantA, f.tenantB}, {f.bobSubject, f.tenantB, f.tenantA}} {
		err := f.scoped(t, scope.subject, scope.own, func(ctx context.Context, q postgres.Executor) error {
			for _, query := range []string{`SELECT count(*) FROM ` + f.parent, `SELECT count(*) FROM ` + f.parent + ` p JOIN ` + f.child + ` c USING(tenant_id,id)`} {
				var n int
				if err := q.QueryRowContext(ctx, query).Scan(&n); err != nil || n != 1 {
					return errors.New("join/aggregate isolation failed")
				}
			}
			// An export-style full scan has no tenant predicate; RLS still scopes it.
			rows, err := q.QueryContext(ctx, `SELECT tenant_id,name,custom FROM `+f.parent+` ORDER BY id`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var tid, name, custom string
				if err = rows.Scan(&tid, &name, &custom); err != nil {
					rows.Close()
					return err
				}
				if tid != scope.own {
					rows.Close()
					return errors.New("export leaked a tenant")
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			var n int
			if err = q.QueryRowContext(ctx, `SELECT count(*) FROM `+f.parent+` WHERE tenant_id=$1`, scope.other).Scan(&n); err != nil || n != 0 {
				return errors.New("foreign select visible")
			}
			for _, query := range []string{`UPDATE ` + f.parent + ` SET name='foreign' WHERE tenant_id=$1`, `DELETE FROM ` + f.parent + ` WHERE tenant_id=$1`} {
				res, err := q.ExecContext(ctx, query, scope.other)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil || n != 0 {
					return errors.New("foreign mutation succeeded")
				}
			}
			if _, err = q.ExecContext(ctx, `INSERT INTO `+f.parent+`(tenant_id,id,name) VALUES($1,2,'own')`, scope.own); err != nil {
				return err
			}
			if _, err = q.ExecContext(ctx, `UPDATE `+f.parent+` SET name='updated' WHERE id=2`); err != nil {
				return err
			}
			res, err := q.ExecContext(ctx, `DELETE FROM `+f.parent+` WHERE id=2`)
			if err != nil {
				return err
			}
			n64, err := res.RowsAffected()
			if err != nil || n64 != 1 {
				return errors.New("own CRUD failed")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for name, run := range map[string]func(context.Context, postgres.Executor) error{
			"foreign insert": func(ctx context.Context, q postgres.Executor) error {
				_, err := q.ExecContext(ctx, `INSERT INTO `+f.parent+`(tenant_id,id,name) VALUES($1,2,'foreign')`, scope.other)
				return err
			},
			"move tenant": func(ctx context.Context, q postgres.Executor) error {
				_, err := q.ExecContext(ctx, `UPDATE `+f.parent+` SET tenant_id=$1`, scope.other)
				return err
			},
			"foreign relationship": func(ctx context.Context, q postgres.Executor) error {
				_, err := q.ExecContext(ctx, `INSERT INTO `+f.child+`(tenant_id,id,parent_id) VALUES($1,2,999)`, scope.own)
				return err
			},
			"truncate": func(ctx context.Context, q postgres.Executor) error {
				_, err := q.ExecContext(ctx, `TRUNCATE `+f.parent)
				return err
			},
		} {
			t.Run(name+scope.own, func(t *testing.T) {
				if err := f.scoped(t, scope.subject, scope.own, run); err == nil {
					t.Fatal("forbidden operation succeeded")
				}
			})
		}
	}
	// Only tenant B has parent 999: tenant A cannot reference it through the FK.
	if err := f.scoped(t, f.bobSubject, f.tenantB, func(ctx context.Context, q postgres.Executor) error {
		_, err := q.ExecContext(ctx, `INSERT INTO `+f.parent+`(tenant_id,id,name) VALUES($1,999,'foreign-only')`, f.tenantB)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.scoped(t, f.aliceSubject, f.tenantA, func(ctx context.Context, q postgres.Executor) error {
		_, err := q.ExecContext(ctx, `INSERT INTO `+f.child+`(tenant_id,id,parent_id) VALUES($1,9,999)`, f.tenantA)
		return err
	}); err == nil {
		t.Fatal("cross-tenant FK succeeded")
	}
}

func TestM2MissingForgedInactiveAndSwitchedContext(t *testing.T) {
	f := newData(t)
	for _, setting := range []string{"", f.tenantA, f.tenantB, "invalid"} {
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, setting); err != nil {
				return err
			}
			n, err := f.count(ctx, q)
			if err != nil || n != 0 {
				return errors.New("tenant setting alone exposed rows")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// Valid identity A plus an unverified selection of B still denies.
	if err := f.scoped(t, f.aliceSubject, f.tenantA, func(ctx context.Context, q postgres.Executor) error {
		if _, err := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenantB); err != nil {
			return err
		}
		n, err := f.count(ctx, q)
		if err != nil || n != 0 {
			return errors.New("forged scope exposed rows")
		}
		var memberships int
		if err = q.QueryRowContext(ctx, `SELECT count(*) FROM iam.memberships WHERE tenant_id=$1`, f.tenantB).Scan(&memberships); err != nil || memberships != 0 {
			return errors.New("IAM scope spoof leaked rows")
		}
		// A mismatched actor also denies, even when actor B is real.
		if _, err = q.ExecContext(ctx, `SELECT set_config('app.actor_id',$1,true)`, f.bob); err != nil {
			return err
		}
		n, err = f.count(ctx, q)
		if err != nil || n != 0 {
			return errors.New("forged actor exposed rows")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.scoped(t, f.aliceSubject, f.tenantA, func(ctx context.Context, q postgres.Executor) error {
		p, err := f.verifier.Verify(f.token(t, f.aliceSubject, nil))
		if err != nil {
			return err
		}
		u, err := (identity.Store{}).Resolve(ctx, q, p)
		if err != nil {
			return err
		}
		for _, selected := range []string{f.tenantB, "invalid"} {
			if _, err = (tenant.Store{}).Resolve(ctx, q, u, selected); !errors.Is(err, tenant.ErrDenied) {
				return errors.New("failed switch was accepted")
			}
			n, err := f.count(ctx, q)
			if err != nil || n != 0 {
				return errors.New("failed switch retained previous scope")
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		query string
		args  []any
	}{
		{`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, []any{f.tenantA, f.alice}},
		{`UPDATE iam.tenants SET active=false WHERE id=$1`, []any{f.tenantA}},
		{`UPDATE iam.users SET active=false WHERE id=$1`, []any{f.alice}},
	} {
		if _, err := f.operator.Exec(fixture.query, fixture.args...); err != nil {
			t.Fatal("inactive fixture failed")
		}
		if err := f.scoped(t, f.aliceSubject, f.tenantA, func(context.Context, postgres.Executor) error { return errors.New("must never reach callback") }); !errors.Is(err, tenant.ErrDenied) && !errors.Is(err, identity.ErrUnauthenticated) {
			t.Fatal("inactive principal/scope not denied")
		}
		if _, err := f.operator.Exec(`UPDATE iam.memberships SET active=true WHERE tenant_id=$1 AND user_id=$2;`, f.tenantA, f.alice); err != nil {
			t.Fatal(err)
		}
		if _, err := f.operator.Exec(`UPDATE iam.tenants SET active=true WHERE id=$1`, f.tenantA); err != nil {
			t.Fatal(err)
		}
		if _, err := f.operator.Exec(`UPDATE iam.users SET active=true WHERE id=$1`, f.alice); err != nil {
			t.Fatal(err)
		}
	}
}

func TestM2PoolReuseRollbackCancellationPanicAndAuditAtomicity(t *testing.T) {
	f := newData(t)
	f.db.SetMaxOpenConns(1)
	f.db.SetMaxIdleConns(1)
	// Simulate accidental legacy session SET, then verify the lease resets it.
	if _, err := f.db.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',false),set_config('app.auth_subject',$1,false),set_config('app.actor_id',$2,false),set_config('app.tenant_id',$3,false)`, f.aliceSubject, f.alice, f.tenantA); err != nil {
		t.Fatal(err)
	}
	if err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		n, err := f.count(ctx, q)
		if err != nil || n != 0 {
			return errors.New("persistent scope survived lease")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertClean := func() {
		t.Helper()
		var clear bool
		if err := f.db.QueryRow(`SELECT coalesce(current_setting('app.actor_id',true),'')='' AND coalesce(current_setting('app.tenant_id',true),'')='' AND coalesce(current_setting('app.auth_issuer',true),'')='' AND coalesce(current_setting('app.auth_subject',true),'')=''`).Scan(&clear); err != nil || !clear {
			t.Fatal("connection returned with identity context")
		}
	}
	var retained postgres.Executor
	sentinel := errors.New("repository failure")
	for _, failure := range []string{"rollback", "panic", "cancel", "audit"} {
		t.Run(failure, func(t *testing.T) {
			run := func(ctx context.Context, q postgres.Executor) error {
				retained = q
				if _, err := q.ExecContext(ctx, `INSERT INTO `+f.parent+`(tenant_id,id,name) VALUES($1,2,'must rollback')`, f.tenantA); err != nil {
					return err
				}
				switch failure {
				case "panic":
					panic(sentinel)
				case "cancel":
					child, cancel := context.WithCancel(ctx)
					cancel()
					_, err := q.ExecContext(child, `SELECT 1`)
					return err
				case "audit":
					return (failedAudit{}).Append(ctx, q, audit.Entry{})
				default:
					return sentinel
				}
			}
			if failure == "panic" {
				func() {
					defer func() {
						if recover() != sentinel {
							t.Error("panic missing")
						}
					}()
					_ = f.scoped(t, f.aliceSubject, f.tenantA, run)
				}()
			} else if err := f.scoped(t, f.aliceSubject, f.tenantA, run); err == nil {
				t.Fatal("failure committed")
			}
			assertClean()
			if _, err := retained.ExecContext(context.Background(), `SELECT 1`); !errors.Is(err, sql.ErrTxDone) {
				t.Fatal("executor escaped transaction lifetime")
			}
			if err := f.scoped(t, f.aliceSubject, f.tenantA, func(ctx context.Context, q postgres.Executor) error {
				n, err := f.count(ctx, q)
				if err != nil || n != 1 {
					return errors.New("failed transaction left a write")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			assertClean()
		})
	}
	// Cancel the transaction's parent while SQL is running, then reuse the pool.
	ctx, cancel := context.WithCancel(context.Background())
	err := postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := f.bind(t, ctx, q, f.aliceSubject, f.tenantA); err != nil {
			return err
		}
		timer := time.AfterFunc(30*time.Millisecond, cancel)
		defer timer.Stop()
		_, err := q.ExecContext(ctx, `SELECT pg_catalog.pg_sleep(1)`)
		return err
	})
	cancel()
	if err == nil {
		t.Fatal("cancelled query succeeded")
	}
	assertClean()
	var executions int
	if err := postgres.WithRuntime(context.Background(), f.db, func(context.Context, postgres.Executor) error { executions++; return sentinel }); !errors.Is(err, sentinel) || executions != 1 {
		t.Fatal("unexpected automatic retry")
	}
}

func TestM2ConcurrentTenantTransactions(t *testing.T) {
	f := newData(t)
	f.db.SetMaxOpenConns(2)
	var wg sync.WaitGroup
	failures := make(chan error, 20)
	// Generate tokens before goroutines, avoiding concurrent test helper operations.
	tokens := []string{f.token(t, f.aliceSubject, nil), f.token(t, f.bobSubject, nil)}
	tenants := []string{f.tenantA, f.tenantB}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
				p, err := f.verifier.Verify(tokens[i%2])
				if err != nil {
					return err
				}
				u, err := (identity.Store{}).Resolve(ctx, q, p)
				if err != nil {
					return err
				}
				ts := tenant.Store{}
				if err = ts.BindActor(ctx, q, u); err != nil {
					return err
				}
				if _, err = ts.Resolve(ctx, q, u, tenants[i%2]); err != nil {
					return err
				}
				var n int
				var id string
				if err = q.QueryRowContext(ctx, `SELECT count(*),min(tenant_id::text) FROM `+f.parent).Scan(&n, &id); err != nil {
					return err
				}
				if n != 1 || id != tenants[i%2] {
					return fmt.Errorf("concurrent tenant isolation failed")
				}
				return nil
			})
			if err != nil {
				failures <- err
			}
		}(i)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
}

func TestM2RLSInstallerRejectsUnsafeDDLAndRuntimeExecution(t *testing.T) {
	f := newIAM(t)
	for _, definition := range []string{
		`(tenant_id uuid,id int PRIMARY KEY)`,
		`(tenant_id uuid NOT NULL,id int PRIMARY KEY)`,
		`(tenant_id uuid NOT NULL,id int,PRIMARY KEY(tenant_id,id),UNIQUE(id))`,
		`(tenant_id uuid NOT NULL,id int,name text,PRIMARY KEY(tenant_id,id),UNIQUE(name) INCLUDE(tenant_id))`,
		`(tenant_id uuid NOT NULL,id text,PRIMARY KEY(tenant_id,id),FOREIGN KEY(id) REFERENCES iam.roles(name))`,
	} {
		// DDL is valid PostgreSQL; security validation must reject each definition.
		tx, err := f.operator.Begin()
		if err != nil {
			t.Fatal(err)
		}
		name := pgx.Identifier{"runtime", "unsafe_" + strings.ReplaceAll(uuid(t), "-", "")}.Sanitize()
		if _, err = tx.Exec(`CREATE TABLE ` + name + definition); err != nil {
			tx.Rollback()
			t.Fatal("unsafe fixture creation failed")
		}
		{
			if _, err = tx.Exec(`SELECT runtime.install_tenant_rls($1::regclass)`, name); err == nil {
				tx.Rollback()
				t.Fatal("unsafe DDL installer accepted")
			}
		}
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`SELECT runtime.install_tenant_rls('iam.users'::regclass)`); err == nil {
		t.Fatal("application can install RLS")
	}
	if err := postgres.WithRuntime(context.Background(), f.operator, func(context.Context, postgres.Executor) error {
		t.Error("operator reached runtime callback")
		return nil
	}); !errors.Is(err, postgres.ErrUnavailable) {
		t.Fatal("operator runtime accepted")
	}
	// row_security=off must be rejected on the exact leased connection.
	f.db.SetMaxOpenConns(1)
	f.db.SetMaxIdleConns(1)
	if _, err := f.db.Exec(`SET row_security=off`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.WithRuntime(context.Background(), f.db, func(context.Context, postgres.Executor) error { t.Error("unsafe lease callback ran"); return nil }); !errors.Is(err, postgres.ErrUnavailable) {
		t.Fatal("unsafe lease accepted")
	}
	if err := postgres.WithRuntime(context.Background(), f.db, func(context.Context, postgres.Executor) error { return nil }); err != nil {
		t.Fatal("discarded unsafe lease did not recover")
	}
}

func TestM2AuthorizedScopeSwitchAndIdentityRebind(t *testing.T) {
	f := newData(t)
	tx, err := f.operator.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = (tenant.Store{}).Provision(context.Background(), tx, f.tenantB, "Test workspace", f.alice, "member", true); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = f.scoped(t, f.aliceSubject, f.tenantA, func(ctx context.Context, q postgres.Executor) error {
		p, err := f.verifier.Verify(f.token(t, f.aliceSubject, nil))
		if err != nil {
			return err
		}
		u, err := (identity.Store{}).Resolve(ctx, q, p)
		if err != nil {
			return err
		}
		n, err := f.count(ctx, q)
		if err != nil || n != 0 {
			return errors.New("identity rebind retained tenant")
		}
		ts := tenant.Store{}
		if err = ts.BindActor(ctx, q, u); err != nil {
			return err
		}
		for _, selected := range []string{f.tenantB, f.tenantA} {
			if _, err = ts.Resolve(ctx, q, u, selected); err != nil {
				return err
			}
			var id string
			if err = q.QueryRowContext(ctx, `SELECT tenant_id FROM `+f.parent).Scan(&id); err != nil || id != selected {
				return errors.New("authorized scope switch failed")
			}
		}
		if _, err = (identity.Store{}).Resolve(ctx, q, identity.Principal{}); !errors.Is(err, identity.ErrUnauthenticated) {
			return errors.New("invalid identity rebound")
		}
		n, err = f.count(ctx, q)
		if err != nil || n != 0 {
			return errors.New("failed identity rebind retained scope")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
