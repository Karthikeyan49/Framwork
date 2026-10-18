//go:build integration && driverpanic

package integration

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Explicit failing infrastructure diagnostic. The default pgx connector has no
// injected tracer; this upstream driver-panic recovery limit remains open in R7.
type aggregateReviewPanicTracer struct {
	armed, fired atomic.Bool
	restores     atomic.Int32
}

func (p *aggregateReviewPanicTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if p.armed.Load() && d.SQL == `SELECT set_config('jit',$1,true),set_config('plan_cache_mode',$2,true)` {
		p.restores.Add(1)
	}
	if p.armed.Load() && strings.HasPrefix(d.SQL, "WITH aggregate_input AS") && p.fired.CompareAndSwap(false, true) {
		panic("independent aggregate driver panic")
	}
	return ctx
}
func (*aggregateReviewPanicTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestIndependentR3dPanicDoesNotIssueRestorationSQLAndPoolRecovers(t *testing.T) {
	if os.Getenv("M9R_R3D_DRIVER_PANIC_CHILD") != "true" {
		// Supervise the injected driver fault: database/sql may leak a Tx read
		// lock on driver panic. Keep that failure bounded and preserve this
		// owned disposable database's cleanup in the parent test process.
		m8Database(t)
		ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIndependentR3dPanicDoesNotIssueRestorationSQLAndPoolRecovers$", "-test.timeout=6s")
		command.Env = append(os.Environ(), "M9R_R3D_DRIVER_PANIC_CHILD=true")
		command.Stdout, command.Stderr = io.Discard, io.Discard
		if err := command.Run(); err != nil {
			t.Fatal("injected driver panic did not complete rollback and pool recovery within the supervised deadline")
		}
		return
	}
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	s := aggregateBulkSeed(t, f, h, base, token, app, 6)
	tracer := &aggregateReviewPanicTracer{}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	scalarSQL(t, err)
	cfg.Tracer = tracer
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	var actual, expected, role, beforeJIT, beforePlan string
	scalarSQL(t, f.db.QueryRow(`SELECT current_database()`).Scan(&expected))
	scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user,current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&actual, &role, &beforeJIT, &beforePlan))
	if actual != expected || role != "framework_app" {
		t.Fatal("independent panic target mismatch")
	}
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	var caught any
	func() {
		defer func() { caught = recover() }()
		_ = postgres.WithRuntime(context.Background(), db, func(ctx context.Context, q postgres.Executor) error {
			a, engine, guard, resolve, err := aggregateReviewAuthority(ctx, q, f, principal, app)
			if err != nil {
				return err
			}
			tracer.armed.Store(true)
			_, err = (data.Store{}).QueryVisibleWithPolicy(ctx, q, s, a, "entity.contact", aggregateQuery("sum", aggregateBalance), engine, guard, resolve)
			return err
		})
	}()
	if caught != "independent aggregate driver panic" || !tracer.fired.Load() || tracer.restores.Load() != 0 {
		t.Fatal("panic changed or issued SQL during unwinding")
	}
	var afterJIT, afterPlan string
	scalarSQL(t, db.QueryRow(`SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&afterJIT, &afterPlan))
	if afterJIT != beforeJIT || afterPlan != beforePlan {
		t.Fatal("panic leaked local expression settings into the pool")
	}
	if got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateUnits)); got.Count != 6 || got.Sum != "21" {
		t.Fatal("panic damaged subsequent aggregate behavior")
	}
}
