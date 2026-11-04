//go:build integration

package integration

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestM9RSQLAggregateRestoresExpressionModeWithinSnapshot(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	want := aggregateSeed(t, f, h, base, token, app, 2)
	s := scalarScope(t, h, base, token)
	p, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	guard := data.Guard(func(context.Context, string, data.Resource) error { return nil })
	resolve := data.ScopeResolver(func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	})
	for _, initial := range []string{"on", "off"} {
		for _, planMode := range []string{"auto", "force_custom_plan", "force_generic_plan"} {
			err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
				a, err := m3Scope(f, ctx, q, p, f.tenantA)
				if err != nil {
					return err
				}
				if _, err := q.ExecContext(ctx, `SELECT set_config('jit',$1,true),set_config('plan_cache_mode',$2,true)`, initial, planMode); err != nil {
					return err
				}
				for range 2 {
					page, err := (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("sum", aggregateBalance), policy.Native{}, guard, resolve)
					if err != nil {
						return err
					}
					if page.Aggregate == nil || page.Aggregate.Sum != want[aggregateBalance] || page.Aggregate.Count != 2 {
						t.Fatal("same-transaction aggregate changed result")
					}
					var after, afterPlan string
					if err := q.QueryRowContext(ctx, `SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&after, &afterPlan); err != nil {
						return err
					}
					if after != initial || afterPlan != planMode {
						t.Fatal("aggregate changed later same-transaction expression mode")
					}
				}
				return nil
			})
			scalarSQL(t, err)
		}
	}
}

type aggregateExpressionFault struct {
	mode  string
	fired atomic.Bool
	armed atomic.Bool
}

func (f *aggregateExpressionFault) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	match := f.mode == "aggregate" && strings.HasPrefix(d.SQL, "WITH aggregate_input AS") || f.mode == "restore" && d.SQL == `SELECT set_config('jit',$1,true),set_config('plan_cache_mode',$2,true)`
	if match && f.armed.Load() && f.fired.CompareAndSwap(false, true) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	return ctx
}
func (*aggregateExpressionFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestM9RSQLAggregateExpressionFailureSuppressesResultAndResetsPool(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	aggregateSeed(t, f, h, base, token, app, 2)
	s := scalarScope(t, h, base, token)
	p, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	guard := data.Guard(func(context.Context, string, data.Resource) error { return nil })
	resolve := data.ScopeResolver(func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	})
	for _, stage := range []string{"aggregate", "restore"} {
		fault := &aggregateExpressionFault{mode: stage}
		cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
		scalarSQL(t, err)
		cfg.Tracer = fault
		db := stdlib.OpenDB(*cfg)
		defer db.Close()
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		var expectedDB, actualDB, role, initial, initialPlan string
		scalarSQL(t, f.db.QueryRow(`SELECT current_database()`).Scan(&expectedDB))
		scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user,current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&actualDB, &role, &initial, &initialPlan))
		if actualDB != expectedDB || role != "framework_app" {
			db.Close()
			t.Fatal("expression fault target mismatch")
		}
		var page data.Page
		err = postgres.WithRuntime(context.Background(), db, func(ctx context.Context, q postgres.Executor) error {
			a, err := m3Scope(f, ctx, q, p, f.tenantA)
			if err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('jit','on',true),set_config('plan_cache_mode','force_generic_plan',true)`); err != nil {
				return err
			}
			// Do not fire the restoration fault on this fixture setup statement.
			fault.armed.Store(true)
			page, err = (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("sum", aggregateBalance), policy.Native{}, guard, resolve)
			return err
		})
		if err == nil || !fault.fired.Load() || page.Aggregate != nil || len(page.Records) != 0 {
			db.Close()
			t.Fatal("expression cancellation/restoration failure exposed a result", stage)
		}
		var after, afterPlan string
		scalarSQL(t, db.QueryRow(`SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&after, &afterPlan))
		if after != initial || afterPlan != initialPlan {
			db.Close()
			t.Fatal("expression mode leaked through reused pool", stage)
		}
		db.Close()
	}
}
