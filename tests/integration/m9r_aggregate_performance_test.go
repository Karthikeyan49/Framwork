//go:build integration && performance

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type aggregateTraceKey struct{}
type aggregateTrace struct {
	mu    sync.Mutex
	times []float64
	sql   string
	args  []any
	state string
}

func (p *aggregateTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if !strings.HasPrefix(d.SQL, "WITH aggregate_input AS") {
		return ctx
	}
	args := d.Args
	// database/sql prepends a pgx result-format option. It is not a bound
	// SQL parameter and must not be replayed as part of a PREPARE contract.
	if len(args) != 0 {
		if _, ok := args[0].(pgx.QueryResultFormatsByOID); ok {
			args = args[1:]
		}
	}
	p.mu.Lock()
	p.sql, p.args = d.SQL, append([]any(nil), args...)
	p.mu.Unlock()
	return context.WithValue(ctx, aggregateTraceKey{}, time.Now())
}
func (p *aggregateTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	if start, ok := ctx.Value(aggregateTraceKey{}).(time.Time); ok {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.times = append(p.times, float64(time.Since(start).Microseconds())/1000)
		var e interface{ SQLState() string }
		if errors.As(d.Err, &e) {
			p.state = e.SQLState()
		}
	}
}

func aggregateExplain(t *testing.T, f *iamFixture, obs *aggregateTrace, token string, jit ...bool) float64 {
	t.Helper()
	compilation, _ := aggregateExplainWithCost(t, f, obs, token, len(jit) == 1 && jit[0], "")
	return compilation
}

func aggregateExplainWithCost(t *testing.T, f *iamFixture, obs *aggregateTrace, token string, jit bool, cost string) (float64, float64) {
	t.Helper()
	obs.mu.Lock()
	statement, args := obs.sql, append([]any(nil), obs.args...)
	obs.mu.Unlock()
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	var raw []byte
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		mode := "off" // Same static expression mode as the SQL_V1 kernel.
		if jit {
			mode = "on"
		}
		if _, err := q.ExecContext(ctx, `SELECT set_config('jit',$1,true),set_config('plan_cache_mode','force_custom_plan',true)`, mode); err != nil {
			return err
		}
		if cost != "" {
			if cost != "4" && cost != "1.1" {
				t.Fatal("unexpected diagnostic cost model")
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('random_page_cost',$1,true)`, cost); err != nil {
				return err
			}
		}
		return q.QueryRowContext(ctx, "EXPLAIN (ANALYZE,BUFFERS,VERBOSE,FORMAT JSON) "+statement, args...).Scan(&raw)
	})
	scalarSQL(t, err)
	var plan any
	if json.Unmarshal(raw, &plan) != nil {
		t.Fatal("aggregate plan decoding")
	}
	membership, aggregates := 0, 0
	jitMillis, executionMillis := float64(0), float64(0)
	diagnostics := []map[string]any{}
	var walk func(any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if ms, ok := n["Execution Time"].(float64); ok {
				executionMillis = ms
			}
			if j, ok := n["JIT"].(map[string]any); ok {
				if timing, ok := j["Timing"].(map[string]any); ok {
					jitMillis, _ = timing["Total"].(float64)
				}
				// JIT diagnostic metadata contains only counts, boolean options
				// and timing; never SQL, arguments, source code or row values.
				safe := map[string]any{}
				for _, key := range []string{"Functions", "Options", "Timing"} {
					safe[key] = j[key]
				}
				encoded, _ := json.Marshal(safe)
				t.Logf("aggregate_jit_diagnostics=%s", encoded)
			}
			if n["Node Type"] != nil {
				d := map[string]any{}
				for _, key := range []string{"Node Type", "Relation Name", "Index Name", "Actual Rows", "Actual Loops", "Actual Total Time", "Shared Hit Blocks", "Shared Read Blocks", "Temp Written Blocks", "Heap Fetches"} {
					if value := n[key]; value != nil {
						d[key] = value
					}
				}
				diagnostics = append(diagnostics, d)
			}
			if n["Node Type"] == "Aggregate" {
				aggregates++
			}
			if name, _ := n["Subplan Name"].(string); strings.Contains(name, "InitPlan") {
				out, _ := json.Marshal(n["Output"])
				if strings.Contains(string(out), "tenant_matches_scope") {
					loops, _ := n["Actual Loops"].(float64)
					if loops > 1 {
						t.Fatal("aggregate membership cost scales per input")
					}
					if loops == 1 {
						membership++
					}
				}
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(plan)
	encoded, _ := json.Marshal(diagnostics)
	t.Logf("aggregate_plan_diagnostics=%s", encoded)
	if membership == 0 || aggregates == 0 {
		t.Fatal("aggregate SQL or single-evaluation membership proof absent")
	}
	t.Logf("aggregate_plan membership_initplans_executed_once=%d sql_aggregate_nodes=%d", membership, aggregates)
	return jitMillis, executionMillis
}

func aggregateCostStatistics(t *testing.T, f *iamFixture) {
	t.Helper()
	// pg_stats deliberately hides RLS-protected column statistics from normal
	// roles. Use the existing disposable-fixture admin binding for metadata
	// inspection only; all analyzed business queries remain the app role.
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	scalarSQL(t, err)
	cfg.User, cfg.Password = "postgres", os.Getenv("POSTGRES_PASSWORD")
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	var expectedDB, actualDB, role string
	scalarSQL(t, f.operator.QueryRow(`SELECT current_database()`).Scan(&expectedDB))
	scalarSQL(t, admin.QueryRow(`SELECT current_database(),session_user`).Scan(&actualDB, &role))
	if actualDB != expectedDB || !strings.HasPrefix(actualDB, "m8_delivery_") || role != "postgres" {
		t.Fatal("cost statistics target is not the owned disposable fixture")
	}
	var relations, columns []byte
	scalarSQL(t, f.operator.QueryRow(`SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]'::jsonb) FROM
 (SELECT relname,relpages,relallvisible,reltuples FROM pg_class WHERE oid IN('runtime.records'::regclass,'runtime.numeric_projections'::regclass,'runtime.record_identity'::regclass)) x`).Scan(&relations))
	scalarSQL(t, admin.QueryRow(`SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]'::jsonb) FROM
 (SELECT tablename,attname,n_distinct,correlation FROM pg_stats WHERE schemaname='runtime' AND tablename IN('records','numeric_projections','record_identity') AND attname IN('tenant_id','app_id','metadata_revision','ir_hash','entity_id','field_id','id') ORDER BY tablename,attname) x`).Scan(&columns))
	if len(columns) < 3 {
		t.Fatal("cost statistics were not visible to the fixture inspector")
	}
	t.Logf("aggregate_cost_relations=%s aggregate_cost_columns=%s", relations, columns)
}

// Diagnostic, not an alternative capacity gate. Compare the SAME generated
// statement, parameters and fixture; each LOCAL setting dies with its owned tx.
func TestM9RSQLAggregateJITDiagnostic(t *testing.T) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, app := aggregateFixture(t)
	aggregateBulkSeed(t, f, h, base, token, app, 100000)
	m9rIndexReady(t, h, base+"/indexes/"+aggregateBalance, token)
	_, err := f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.numeric_indexes;ANALYZE runtime.numeric_projections`)
	scalarSQL(t, err)
	obs := &aggregateTrace{}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	scalarSQL(t, err)
	cfg.Tracer = obs
	db := stdlib.OpenDB(*cfg)
	defer db.Close()
	var actualDB, expectedDB, role string
	scalarSQL(t, f.db.QueryRow(`SELECT current_database()`).Scan(&expectedDB))
	scalarSQL(t, db.QueryRow(`SELECT current_database(),session_user`).Scan(&actualDB, &role))
	if actualDB != expectedDB || role != "framework_app" {
		t.Fatal("JIT diagnostic target mismatch")
	}
	original := f.db
	f.db = db
	h = r5Handler(t, f, audit.Postgres{})
	f.db = original
	got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateBalance))
	if got.Count != 100000 {
		t.Fatal("JIT diagnostic must execute the full input")
	}
	on := aggregateExplain(t, f, obs, token, true)
	off := aggregateExplain(t, f, obs, token, false)
	t.Logf("aggregate_jit_comparison on_compile_ms=%.3f off_compile_ms=%.3f; diagnostic only", on, off)
}

// Inspect the SAME manually prepared statement under both planning modes.
// EXECUTE utility arguments cannot be driver parameters, so PostgreSQL itself
// quotes values, and casts come only from this finite built-in type allowlist.
// Neither the SQL text nor its literals are logged.
func aggregatePreparedExplain(t *testing.T, f *iamFixture, obs *aggregateTrace, token string) {
	t.Helper()
	obs.mu.Lock()
	statement, args := obs.sql, append([]any(nil), obs.args...)
	obs.mu.Unlock()
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		var raw []byte
		begin := time.Now()
		err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('jit','off',true),set_config('plan_cache_mode',$1,true)`, mode); err != nil {
				return err
			}
			name := "m9r_aggregate_" + strings.ReplaceAll(uuid(t), "-", "")
			if _, err := q.ExecContext(ctx, "PREPARE "+name+" AS "+statement); err != nil {
				return err
			}
			var encoded []byte
			if err := q.QueryRowContext(ctx, `SELECT to_json(ARRAY(SELECT x::text FROM unnest(parameter_types) x)) FROM pg_prepared_statements WHERE name=$1`, name).Scan(&encoded); err != nil {
				return err
			}
			var types []string
			if json.Unmarshal(encoded, &types) != nil || len(types) != len(args) {
				t.Fatalf("prepared aggregate parameter contract types=%d args=%d", len(types), len(args))
			}
			literals := []string{}
			for i, arg := range args {
				switch types[i] {
				case "text", "uuid", "bigint", "integer", "numeric", "boolean", "date", "timestamp with time zone", "timestamp without time zone", "jsonb":
				default:
					t.Fatal("unexpected prepared aggregate built-in type")
				}
				value := fmt.Sprint(arg)
				switch v := arg.(type) {
				case []byte:
					value = string(v)
				case json.RawMessage:
					value = string(v)
				}
				var quoted string
				if err := q.QueryRowContext(ctx, `SELECT quote_nullable($1::text)`, value).Scan(&quoted); err != nil {
					return err
				}
				literals = append(literals, quoted+"::"+types[i])
			}
			if err := q.QueryRowContext(ctx, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) EXECUTE `+name+"("+strings.Join(literals, ",")+")").Scan(&raw); err != nil {
				return err
			}
			_, err := q.ExecContext(ctx, "DEALLOCATE "+name)
			return err
		})
		if err != nil {
			var state interface{ SQLState() string }
			if errors.As(err, &state) && state.SQLState() == "57014" {
				t.Logf("aggregate_prepared_diagnostic mode=%s SQLSTATE57014 after=%s (diagnostic failure, not a capacity pass)", mode, time.Since(begin))
				continue
			}
			scalarSQL(t, err)
		}
		var plan []struct {
			Plan          map[string]any `json:"Plan"`
			ExecutionTime float64        `json:"Execution Time"`
		}
		if json.Unmarshal(raw, &plan) != nil || len(plan) != 1 {
			t.Fatal("prepared aggregate plan decoding")
		}
		joins, scans := []string{}, []string{}
		var walk func(map[string]any)
		walk = func(p map[string]any) {
			node, _ := p["Node Type"].(string)
			rows, _ := p["Actual Rows"].(float64)
			loops, _ := p["Actual Loops"].(float64)
			if strings.Contains(node, "Join") || node == "Nested Loop" {
				joins = append(joins, node)
			}
			if table, ok := p["Relation Name"].(string); ok {
				scans = append(scans, node+":"+table+":rows="+strconv.FormatFloat(rows, 'f', 0, 64)+":loops="+strconv.FormatFloat(loops, 'f', 0, 64))
			}
			if children, ok := p["Plans"].([]any); ok {
				for _, child := range children {
					if c, ok := child.(map[string]any); ok {
						walk(c)
					}
				}
			}
		}
		walk(plan[0].Plan)
		t.Logf("aggregate_prepared_diagnostic mode=%s execution_ms=%.3f joins=%v scans=%v", mode, plan[0].ExecutionTime, joins, scans)
	}
}

func TestM9RSQLAggregatePerformanceFloor(t *testing.T) {
	runAggregatePerformance(t, false)
}

func TestM9RSQLAggregateMappedPerformanceFloor(t *testing.T) {
	runAggregatePerformance(t, true)
}

func TestM9RSQLAggregatePlannerCostDiagnostic(t *testing.T) {
	runAggregatePerformance(t, false, true)
}

func runAggregatePerformance(t *testing.T, mapped bool, costStudy ...bool) {
	diagnostic := len(costStudy) == 1 && costStudy[0]
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, owner, app := aggregateFixture(t)
	scope := aggregateBulkSeed(t, f, h, base, owner, app, 100000)
	for i := 0; i < 9; i++ {
		id := f.tenantB
		if i != 0 {
			id = uuid(t)
			scalarSQL(t, (tenant.Store{}).Provision(context.Background(), f.operator, id, "Aggregate reference tenant", f.bob, "owner", true))
		}
		_, err := f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3)`, id, app, scope.Hash())
		scalarSQL(t, err)
		_, err = f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,entity_id,id,custom_data,$4,$4,$4 FROM runtime.records WHERE tenant_id=$5 AND app_id=$2 ORDER BY id LIMIT 10000`, id, app, scope.Hash(), f.bob, f.tenantA)
		scalarSQL(t, err)
	}
	_, err := f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id)
 SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$3 FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND (custom_data->>'field.contact.units')::int%5=0`, f.tenantA, app, f.viewer)
	scalarSQL(t, err)
	_, err = f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.record_shares;ANALYZE runtime.dataset_generations;ANALYZE runtime.datasets;ANALYZE runtime.record_identity`)
	scalarSQL(t, err)
	var width int
	scalarSQL(t, f.operator.QueryRow(`SELECT avg(pg_column_size(r))::int FROM runtime.records r WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&width))
	if width < 900 {
		t.Fatal("nonrepresentative aggregate fixture")
	}
	for _, field := range []string{aggregateUnits, aggregateRatio, aggregateBalance} {
		idx := m9rIndexReady(t, h, base+"/indexes/"+field, owner)
		if idx.ScannedRows != 100000 || idx.VerifiedRows != 100000 {
			t.Fatal("aggregate governed numeric coverage")
		}
		t.Logf("aggregate_source_index_ready field=%s verified=%d", field, idx.VerifiedRows)
	}
	_, err = f.operator.Exec(`ANALYZE runtime.numeric_indexes;ANALYZE runtime.numeric_projections`)
	scalarSQL(t, err)
	bases := []string{base}
	if mapped {
		root := "/v1/tenants/" + f.tenantA + "/preview/" + app
		witness := m9rDatasetWitness(t, h, root+"/versions/2", owner)
		d := m9rDatasetCreate(t, h, root+"/datasets", owner, "ADOPT", witness)
		path := root + "/datasets/" + d.ID
		for n := 0; d.State != "OPEN" && n < 2001; n++ {
			w := m5Call(h, path+"/verify", owner, struct{}{})
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
				t.Fatal("mapped capacity adoption", w.Code)
			}
			if n%250 == 0 || d.State == "OPEN" {
				t.Logf("aggregate_adoption state=%s verified=%d", d.State, d.VerifiedRows)
			}
		}
		if d.State != "OPEN" || d.VerifiedRows != 100000 {
			t.Fatal("mapped capacity adoption coverage")
		}
		var raw []byte
		scalarSQL(t, f.operator.QueryRow(`SELECT document FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, f.tenantA, app).Scan(&raw))
		target := shadowSource(t, h, owner, f.tenantA, app, 3, raw, func(doc *metadata.Document) { addCount(doc, "integer", true) })
		intent := compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.contact", FieldID: "field.contact.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`7`)}}}
		x := shadowPrepare(t, h, owner, path, 1, target, intent)
		for n := 0; x.State != "READY" && n < 6001; n++ {
			x = shadowStep(t, h, owner, path, x, "advance")
			if n%250 == 0 || x.State == "READY" {
				t.Logf("aggregate_migration state=%s copied=%d verified=%d", x.State, x.CopiedRows, x.VerifiedRows)
			}
		}
		if x.State != "READY" || x.CopiedRows != 100000 || x.VerifiedRows != 100000 || x.SourceManifest != x.TargetManifest {
			t.Fatal("mapped capacity migration coverage")
		}
		shadowStep(t, h, owner, path, x, "activate")
		old := path + "/bindings/1/entities/entity.contact"
		current := path + "/bindings/2/entities/entity.contact"
		var id, shared, deleted string
		scalarSQL(t, f.operator.QueryRow(`SELECT md5('1')::uuid::text,md5('2')::uuid::text,md5('100000')::uuid::text`).Scan(&id, &shared, &deleted))
		var changedAccess data.Access
		if w := m5Call(h, current+"/records/"+id+"/access", owner, map[string]any{"operation": "owner", "expected_version": 1, "user_id": f.viewer}); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &changedAccess) != nil {
			t.Fatal("mapped capacity governed owner change", w.Code)
		}
		if w := m5Call(h, current+"/commands", owner, data.Command{Key: uuid(t), Operation: "delete", ID: deleted, ExpectedVersion: 1}); w.Code != 200 {
			t.Fatal("mapped capacity governed deletion", w.Code)
		}
		if w := m5Call(h, current+"/records/"+shared+"/access", owner, map[string]any{"operation": "share", "expected_version": 1, "user_id": f.viewer, "can_write": false}); w.Code != 200 {
			t.Fatal("mapped capacity governed canonical sharing", w.Code)
		}
		for _, field := range []string{aggregateUnits, aggregateRatio, aggregateBalance} {
			idx := m9rIndexReady(t, h, current+"/indexes/"+field, owner)
			if idx.ScannedRows != 99999 || idx.VerifiedRows != 99999 {
				t.Fatal("mapped current numeric coverage")
			}
			t.Logf("aggregate_current_index_ready field=%s verified=%d", field, idx.VerifiedRows)
		}
		if w := m5Call(h, current+"/commands", owner, data.Command{Key: uuid(t), Operation: "patch", ID: id, ExpectedVersion: changedAccess.Version, Values: map[string]any{aggregateBalance: map[string]any{"amount": "101", "currency": "USD"}}}); w.Code != 200 {
			t.Fatal("mapped capacity current-only certified value change", w.Code)
		}
		_, err = f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.record_identity;ANALYZE runtime.identity_shares;ANALYZE runtime.numeric_projections;ANALYZE runtime.numeric_indexes;ANALYZE runtime.dataset_generations;ANALYZE runtime.datasets`)
		scalarSQL(t, err)
		bases = []string{current, old}
		t.Log("aggregate_mapped_reference real100k adoption/evolution; current and retained physical generations; current owner change and tombstone; no manual VACUUM")
	}
	t.Logf("aggregate_reference hot_rows=100000 tenants=10 other_rows_each=10000 avg_record_bytes=%d warmup=3 samples=100 clients=10", width)
	obs := &aggregateTrace{}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("aggregate trace configuration")
	}
	cfg.Tracer = obs
	traced := stdlib.OpenDB(*cfg)
	defer traced.Close()
	traced.SetMaxOpenConns(10)
	traced.SetMaxIdleConns(10)
	var expectedDB, expectedRole, actualDB, actualRole string
	scalarSQL(t, f.db.QueryRow(`SELECT current_database(),session_user`).Scan(&expectedDB, &expectedRole))
	scalarSQL(t, traced.QueryRow(`SELECT current_database(),session_user`).Scan(&actualDB, &actualRole))
	if expectedDB != actualDB || expectedRole != actualRole || actualRole != "framework_app" {
		t.Fatal("aggregate trace targets an unexpected database or role")
	}
	original := f.db
	f.db = traced
	h = r5Handler(t, f, audit.Postgres{})
	t.Log("aggregate_reference api_admission=default3 offered_http_clients=10; default driver mode and complete HTTP latency")
	f.db = original
	server := httptest.NewServer(h)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	viewer := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}})
	version := int64(0)
	cases := []struct {
		name, field, role string
		query             data.Query
		allow             func(int) bool
	}{
		{"count", "", "", aggregateQuery("count", ""), func(int) bool { return true }},
		{"sum_integer", aggregateUnits, "", aggregateQuery("sum", aggregateUnits), func(int) bool { return true }},
		{"sum_decimal", aggregateRatio, "", aggregateQuery("sum", aggregateRatio), func(int) bool { return true }},
		{"sum_money", aggregateBalance, "", aggregateQuery("sum", aggregateBalance), func(int) bool { return true }},
		{"filtered_money", aggregateBalance, "", aggregateQuery("sum", aggregateBalance), func(i int) bool { return i%7 == 0 }},
		{"owner_money", aggregateBalance, "OWNER", aggregateQuery("sum", aggregateBalance), func(i int) bool { return i%2 == 0 }},
		{"share_count", "", "SHARE", aggregateQuery("count", ""), func(i int) bool { return i%5 == 0 }},
	}
	for branch, queryBase := range bases {
		for _, tc := range cases {
			if diagnostic && tc.name != "sum_money" && tc.name != "filtered_money" {
				continue
			}
			t.Logf("aggregate_case mapped=%t branch=%d case=%s", mapped, branch, tc.name)
			token := owner
			if tc.role != "" {
				d := r5Definition("finance_manager", app, policy.DataScope{Kind: tc.role})
				version = r5Publish(t, h, "/v1/tenants/"+f.tenantA+"/roles", owner, version, d, true, uuid(t)).Version
				if version == 1 {
					r5Assign(t, h, "/v1/tenants/"+f.tenantA, owner, f.viewer, "finance_manager", 200)
				}
				token = viewer
			}
			if tc.name == "filtered_money" {
				tc.query.Filter = &data.Filter{Op: "eq", Field: "field.contact.status", Value: json.RawMessage(`"inactive"`)}
			}
			want := data.AggregateResult{}
			sum := new(big.Rat)
			for i := 1; i <= 100000; i++ {
				allowed := tc.allow(i)
				if mapped && tc.role == "OWNER" && i == 1 {
					allowed = true
				}
				if mapped && tc.role == "SHARE" && i == 2 {
					allowed = true
				}
				if !allowed || mapped && i == 100000 {
					continue
				}
				want.Count++
				value := "0"
				switch tc.field {
				case aggregateUnits:
					value = fmt.Sprint(i)
				case aggregateRatio:
					if i%17 != 0 {
						value = fmt.Sprintf("%d.000000000000000001", i%5)
					}
				case aggregateBalance:
					value = fmt.Sprint(i % 97)
					if mapped && branch == 0 && i == 1 {
						value = "101" // Retained generation must keep historical1.
					}
				}
				n, ok := new(big.Rat).SetString(value)
				if !ok {
					t.Fatal("aggregate reference oracle")
				}
				sum.Add(sum, n)
			}
			if tc.field != "" {
				want.Sum = strings.TrimRight(strings.TrimRight(sum.FloatString(18), "0"), ".")
				if want.Sum == "" {
					want.Sum = "0"
				}
			}
			body, _ := json.Marshal(tc.query)
			call := func() (float64, bool) {
				r, _ := http.NewRequest("POST", server.URL+queryBase+"/query", bytes.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("Authorization", "Bearer "+token)
				start := time.Now()
				response, err := client.Do(r)
				if err != nil {
					return 0, false
				}
				defer response.Body.Close()
				var page data.Page
				valid := response.StatusCode == 200 && json.NewDecoder(response.Body).Decode(&page) == nil && page.Aggregate != nil && *page.Aggregate == want && len(page.Records) == 0 && page.Next == ""
				return float64(time.Since(start).Microseconds()) / 1000, valid
			}
			for range 3 {
				if _, ok := call(); !ok {
					obs.mu.Lock()
					state := obs.state
					obs.mu.Unlock()
					t.Fatal("aggregate warmup failed", tc.name, "SQLSTATE", state)
				}
			}
			if diagnostic {
				aggregateCostStatistics(t, f)
				for attempt, cost := range []string{"4", "1.1", "1.1", "4"} {
					_, ms := aggregateExplainWithCost(t, f, obs, token, false, cost)
					if ms <= 0 {
						t.Fatal("cost diagnostic did not report analyzed execution")
					}
					t.Logf("aggregate_cost_diagnostic query=%s attempt=%d random_page_cost=%s execution_ms=%.3f (not a capacity gate)", tc.name, attempt+1, cost, ms)
				}
				aggregateCostStatistics(t, f)
				continue
			}
			aggregateExplain(t, f, obs, token)
			if tc.name == "owner_money" || tc.name == "filtered_money" {
				aggregatePreparedExplain(t, f, obs, token)
			}
			obs.mu.Lock()
			obs.times = nil
			obs.mu.Unlock()
			latency := make([]float64, 100)
			valid := make([]bool, 100)
			jobs := make(chan int)
			var group sync.WaitGroup
			begin := time.Now()
			for range 10 {
				group.Go(func() {
					for i := range jobs {
						latency[i], valid[i] = call()
					}
				})
			}
			for i := range 100 {
				jobs <- i
			}
			close(jobs)
			group.Wait()
			unexpected := 0
			for _, ok := range valid {
				if !ok {
					unexpected++
				}
			}
			if unexpected != 0 {
				obs.mu.Lock()
				state := obs.state
				obs.mu.Unlock()
				t.Errorf("aggregate request failure/oracle mismatch case=%s errors=%d SQLSTATE=%s", tc.name, unexpected, state)
				continue // Remains FAIL; collect independent remaining cases honestly.
			}
			obs.mu.Lock()
			sqlTimes := append([]float64(nil), obs.times...)
			obs.mu.Unlock()
			if len(sqlTimes) != 100 {
				t.Fatal("aggregate SQL sample count")
			}
			elapsed := time.Since(begin)
			sqlP95 := reportLatency(t, "aggregate_"+tc.name+"_sql10", sqlTimes, elapsed)
			httpP95 := reportLatency(t, "aggregate_"+tc.name+"_http10", latency, elapsed)
			if sqlP95 > 500 || httpP95 > 750 {
				t.Error("aggregate unchanged SQL500ms/HTTP750ms floor exceeded", tc.name)
			}
		}
	}
}
