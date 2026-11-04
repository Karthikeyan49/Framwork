//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type aggregateReviewPlanTrace struct {
	mu   sync.Mutex
	sql  string
	args []any
}

func (p *aggregateReviewPlanTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "WITH aggregate_input AS") {
		args := d.Args
		if len(args) != 0 {
			if _, ok := args[0].(pgx.QueryResultFormatsByOID); ok {
				args = args[1:]
			}
		}
		p.mu.Lock()
		p.sql, p.args = d.SQL, append([]any(nil), args...)
		p.mu.Unlock()
	}
	return ctx
}
func (*aggregateReviewPlanTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestIndependentR3dCanonicalPlanStreamsOneNormalizedProjection(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	scalarReviewTarget(t, f)
	aggregateSeed(t, f, h, base, token, app, 317)
	m9rIndexReady(t, h, base+"/indexes/"+aggregateRatio, token)
	_, path := aggregateReviewAdopt(t, f, h, token, app, 317)
	ready := aggregateReviewEvolution(t, f, h, token, app, path)
	shadowStep(t, h, token, path, ready, "activate")
	old, current := path+"/bindings/1/entities/entity.contact", path+"/bindings/2/entities/entity.contact"
	id := aggregateReviewID(t, f, 1)
	if w := m5Call(h, current+"/commands", token, data.Command{Operation: "patch", ID: id, ExpectedVersion: 1, Values: map[string]any{aggregateRatio: "-1.1"}}); w.Code != 200 {
		t.Fatal("independent plan current-only patch", w.Code)
	}
	m9rIndexReady(t, h, current+"/indexes/"+aggregateRatio, token)
	_, err := f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.record_identity;ANALYZE runtime.numeric_projections;ANALYZE runtime.numeric_indexes;ANALYZE runtime.dataset_generations;ANALYZE runtime.datasets`)
	scalarSQL(t, err)
	trace := &aggregateReviewPlanTrace{}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	scalarSQL(t, err)
	cfg.Tracer = trace
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	var actual, expected, role string
	scalarSQL(t, f.db.QueryRow(`SELECT current_database()`).Scan(&expected))
	scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&actual, &role))
	if actual != expected || role != "framework_app" {
		t.Fatal("independent plan target mismatch")
	}
	original := f.db
	f.db = db
	h = r5Handler(t, f, audit.Postgres{})
	f.db = original
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	for _, tc := range []struct{ path, sum string }{{old, "0.000000000000000001"}, {current, "-1.199999999999999999"}} {
		got := aggregateResult(t, h, tc.path, token, aggregateQuery("sum", aggregateRatio))
		if got.Count != 317 || got.Sum != tc.sum {
			t.Fatal("streamed aggregate changed the independent exact oracle")
		}
		trace.mu.Lock()
		statement, args := trace.sql, append([]any(nil), trace.args...)
		trace.mu.Unlock()
		if statement == "" || strings.Contains(statement, "AS MATERIALIZED") {
			t.Fatal("plan capture did not observe the actual new aggregate statement")
		}
		var raw []byte
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('jit','off',true),set_config('plan_cache_mode','force_custom_plan',true)`); err != nil {
				return err
			}
			return q.QueryRowContext(ctx, "EXPLAIN (ANALYZE,BUFFERS,VERBOSE,FORMAT JSON) "+statement, args...).Scan(&raw)
		})
		scalarSQL(t, err)
		var root []struct {
			Plan map[string]any `json:"Plan"`
		}
		if json.Unmarshal(raw, &root) != nil || len(root) != 1 {
			t.Fatal("independent normalized plan decoding")
		}
		identities, cteScans, normalized := 0, 0, 0
		diagnostics := []map[string]any{}
		var walk func(map[string]any)
		walk = func(n map[string]any) {
			if n["Node Type"] == "CTE Scan" {
				cteScans++
			}
			if n["Relation Name"] == "record_identity" {
				identities++
				if n["Actual Loops"] != float64(1) {
					t.Fatal("bounded canonical identity scan became per-record work")
				}
			}
			output, _ := json.Marshal(n["Output"])
			columns, _ := n["Output"].([]any)
			cases := strings.Count(string(output), "CASE")
			if (n["Node Type"] == "Subquery Scan" || n["Node Type"] == "Result") && len(columns) == 1 && cases != 0 {
				normalized++
			}
			diagnostics = append(diagnostics, map[string]any{"node": n["Node Type"], "alias": n["Alias"], "loops": n["Actual Loops"], "output_columns": len(columns), "output_case_text_count": cases})
			if children, ok := n["Plans"].([]any); ok {
				for _, child := range children {
					walk(child.(map[string]any))
				}
			}
		}
		walk(root[0].Plan)
		// VERBOSE may expand an OUTER-slot Var into the child's target text.
		// CASE text in Aggregate.Output alone cannot prove repeated evaluation.
		encoded, _ := json.Marshal(diagnostics)
		t.Logf("independent_normalized_plan_structure=%s", encoded)
		if identities != 1 || cteScans != 0 || normalized == 0 {
			t.Fatal("canonical identity or normalized streaming plan proof absent")
		}
		t.Logf("independent_small_plan rows=317 identity_scans=%d identity_loops=1 cte_scans=%d normalized_projection_nodes=%d; functional plan inspection only, no capacity claim", identities, cteScans, normalized)
	}
}
