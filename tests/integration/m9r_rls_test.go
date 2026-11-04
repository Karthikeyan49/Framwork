//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func TestM9RStatementMembershipRemainsFailClosed(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	m5Create(t, h, base, token, "Tenant A record")
	if _, err := f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) SELECT $1,app_id,metadata_revision,ir_hash FROM runtime.record_sets WHERE tenant_id=$2`, f.tenantB, f.tenantA); err != nil {
		t.Fatal(err)
	}
	if _, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id) SELECT $1,app_id,metadata_revision,ir_hash,entity_id,$3,custom_data,$4,$4,$4 FROM runtime.records WHERE tenant_id=$2`, f.tenantB, f.tenantA, uuid(t), f.bob); err != nil {
		t.Fatal(err)
	}
	bob, err := f.verifier.Verify(f.token(t, f.bobSubject, nil))
	if err != nil {
		t.Fatal("tenant B principal")
	}
	if err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(f, ctx, q, bob, f.tenantB); e != nil {
			return e
		}
		var n int
		if e := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.records`).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return errors.New("negative tenant was not populated and accessible")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	principal, err := f.verifier.Verify(token)
	if err != nil {
		t.Fatal("principal")
	}
	f.db.SetMaxOpenConns(1)
	if err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(f, ctx, q, principal, f.tenantA); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, `SET LOCAL plan_cache_mode=force_generic_plan`); e != nil {
			return e
		}
		var n int
		count := func(want int) error {
			if e := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.records`).Scan(&n); e != nil {
				return e
			}
			if n != want {
				return errors.New("membership/context scope mismatch")
			}
			return nil
		}
		if e := count(1); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenantB); e != nil {
			return e
		}
		if e := count(0); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id','',true)`); e != nil {
			return e
		}
		if e := count(0); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenantA); e != nil {
			return e
		}
		return count(1)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.alice); err != nil {
		t.Fatal(err)
	}
	if err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		// Authentication can remain valid while tenant membership has been revoked.
		u, e := (identity.Store{}).Resolve(ctx, q, principal)
		if e != nil {
			return e
		}
		if e = (tenant.Store{}).BindActor(ctx, q, u); e != nil {
			return e
		}
		if _, e := q.ExecContext(ctx, `SELECT set_config('app.tenant_id',$1,true)`, f.tenantA); e != nil {
			return e
		}
		var validActor bool
		if e := q.QueryRowContext(ctx, `SELECT iam.authenticated_actor()=$1::uuid AND iam.current_tenant_id()=$2::uuid`, f.alice, f.tenantA).Scan(&validActor); e != nil {
			return e
		}
		if !validActor {
			return errors.New("revocation test lacked valid actor/tenant context")
		}
		var n int
		if e := q.QueryRowContext(ctx, `SELECT count(*) FROM runtime.records`).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return errors.New("revoked membership exposed data")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = f.db.QueryRow(`SELECT count(*) FROM runtime.records`).Scan(&n); err != nil || n != 0 {
		t.Fatal("pooled context exposed data")
	}
	if _, err = f.operator.Exec(`CREATE TABLE runtime.r3_scope_probe(tenant_id uuid NOT NULL,id uuid NOT NULL,PRIMARY KEY(tenant_id,id));SELECT runtime.install_tenant_rls('runtime.r3_scope_probe')`); err != nil {
		t.Fatal(err)
	}
	var expression string
	var forced bool
	if err = f.operator.QueryRow(`SELECT pg_get_expr(p.polqual,p.polrelid),c.relforcerowsecurity FROM pg_catalog.pg_policy p JOIN pg_catalog.pg_class c ON c.oid=p.polrelid WHERE p.polrelid='runtime.r3_scope_probe'::regclass AND p.polname='tenant_scope'`).Scan(&expression, &forced); err != nil || !forced {
		t.Fatal("future installer isolation")
	}
	if !containsInitPlanExpression(expression) {
		t.Fatal("future installer retained correlated membership")
	}
}

func containsInitPlanExpression(s string) bool {
	// The execution-frequency proof is EXPLAIN ANALYZE in the performance gate.
	return strings.Contains(s, "SELECT iam.current_tenant_id()") && strings.Contains(s, "SELECT iam.tenant_matches_scope(iam.current_tenant_id())")
}
