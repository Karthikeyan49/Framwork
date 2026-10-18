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
	if !strings.HasPrefix(d.SQL, "WITH aggregate_input AS MATERIALIZED") {
		return ctx
	}
	p.mu.Lock()
	p.sql, p.args = d.SQL, append([]any(nil), d.Args...)
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

func aggregateExplain(t *testing.T, f *iamFixture, obs *aggregateTrace, token string) {
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
		return q.QueryRowContext(ctx, "EXPLAIN (ANALYZE,BUFFERS,VERBOSE,FORMAT JSON) "+statement, args...).Scan(&raw)
	})
	scalarSQL(t, err)
	var plan any
	if json.Unmarshal(raw, &plan) != nil {
		t.Fatal("aggregate plan decoding")
	}
	membership, aggregates := 0, 0
	diagnostics := []map[string]any{}
	var walk func(any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if n["Node Type"] != nil {
				d := map[string]any{}
				for _, key := range []string{"Node Type", "Relation Name", "Index Name", "Actual Rows", "Actual Loops", "Actual Total Time", "Shared Hit Blocks", "Shared Read Blocks", "Temp Written Blocks"} {
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
}

func TestM9RSQLAggregatePerformanceFloor(t *testing.T) {
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
	}
	_, err = f.operator.Exec(`ANALYZE runtime.numeric_indexes;ANALYZE runtime.numeric_projections`)
	scalarSQL(t, err)
	t.Logf("aggregate_reference hot_rows=100000 tenants=10 other_rows_each=10000 avg_record_bytes=%d warmup=3 samples=100 clients=10", width)
	obs := &aggregateTrace{}
	cfg, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("aggregate trace configuration")
	}
	cfg.Tracer = obs
	cfg.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
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
	admission, err := postgres.NewAdmission(4, 128)
	scalarSQL(t, err)
	h = r5Handler(t, f, audit.Postgres{}, admission)
	t.Log("aggregate_reference_experiment query_mode=cache_describe trusted_api_admission=4 offered_http_clients=10; HTTP includes all FIFO wait")
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
	for _, tc := range cases {
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
			if !tc.allow(i) {
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
			r, _ := http.NewRequest("POST", server.URL+base+"/query", bytes.NewReader(body))
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
		aggregateExplain(t, f, obs, token)
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
		for _, ok := range valid {
			if !ok {
				obs.mu.Lock()
				state := obs.state
				obs.mu.Unlock()
				t.Fatal("aggregate request failed or disagreed with oracle", tc.name, "SQLSTATE", state)
			}
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
