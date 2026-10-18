//go:build integration && performance

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5/pgconn"
)

func percentile(v []float64, p float64) float64 {
	x := append([]float64(nil), v...)
	sort.Float64s(x)
	i := int(float64(len(x))*p+0.999999) - 1
	if i < 0 {
		i = 0
	}
	return x[i]
}
func reportLatency(t *testing.T, name string, times []float64, elapsed time.Duration) float64 {
	t.Helper()
	p95 := percentile(times, .95)
	t.Logf("%s n=%d p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f throughput_per_s=%.2f errors=0", name, len(times), percentile(times, .5), p95, percentile(times, .99), float64(len(times))/elapsed.Seconds())
	return p95
}

// Run explicitly on the ADR025 reference tier. This is an executed capacity gate,
// not a unit-test benchmark with invented service-level guarantees.
func TestM9RMembershipPerformanceFloor(t *testing.T) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, application := m5Fixture(t)
	h = r5Handler(t, f, audit.Postgres{})
	ctx := context.Background()
	descriptor := m3Call(h, "GET", strings.Split(base, "/entities/")[0], token, 0, nil)
	var release struct {
		Hash string          `json:"ir_hash"`
		IR   json.RawMessage `json:"ir"`
	}
	if descriptor.Code != 200 || json.Unmarshal(descriptor.Body.Bytes(), &release) != nil || release.Hash == "" {
		t.Fatal("descriptor")
	}
	tenants := []string{f.tenantA, f.tenantB}
	tx, err := f.operator.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for range 8 {
		id := uuid(t)
		if err = (tenant.Store{}).Provision(ctx, tx, id, "Benchmark tenant", f.bob, "owner", true); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		tenants = append(tenants, id)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	team := uuid(t)
	if _, err = f.operator.Exec(`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Benchmark team')`, f.tenantA, team); err != nil {
		t.Fatal(err)
	}
	for i, id := range tenants {
		count := 10000
		owner := f.bob
		if i == 0 {
			count = 100000
			owner = f.alice
		}
		if _, err = f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,1,$3)`, id, application, release.Hash); err != nil {
			t.Fatal(err)
		}
		if _, err = f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id,owner_team_id)
   SELECT $1,$2,1,$3,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name',lpad(i::text,8,'0')||repeat(' representative customer text',10),'field.contact.balance',jsonb_build_object('amount',(i%100003)::text,'currency','USD'),'field.contact.status',CASE WHEN i%997=0 THEN 'inactive' ELSE 'active' END,'field.contact.joined','2026-10-13'),$4::uuid,$4::uuid,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END,CASE WHEN $6::boolean AND i%3=0 THEN $7::uuid ELSE NULL END FROM generate_series(1,$8::int) i`, id, application, release.Hash, owner, f.viewer, i == 0, team, count); err != nil {
			t.Fatal("seed", err)
		}
	}
	if _, err = f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$2::uuid FROM runtime.records WHERE tenant_id=$1 AND mod((custom_data->'field.contact.balance'->>'amount')::int,4)=0`, f.tenantA, f.alice); err != nil {
		t.Fatal(err)
	}
	if _, err = f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.record_shares`); err != nil {
		t.Fatal(err)
	}
	var width int
	var version, buffers, workMem string
	if err = f.operator.QueryRow(`SELECT current_setting('server_version'),current_setting('shared_buffers'),current_setting('work_mem'),(SELECT avg(pg_column_size(r))::int FROM runtime.records r WHERE tenant_id=$1)`, f.tenantA).Scan(&version, &buffers, &workMem, &width); err != nil {
		t.Fatal(err)
	}
	t.Logf("PostgreSQL=%s shared_buffers=%s work_mem=%s hot_rows=100000 tenants=10 other_rows_each=10000 avg_record_bytes=%d warmup=3", version, buffers, workMem, width)
	principal, _ := f.verifier.Verify(token)
	run := func(sql string) (float64, error) {
		var elapsed float64
		err := postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
			if _, e := m3Scope(f, ctx, q, principal, f.tenantA); e != nil {
				return e
			}
			start := time.Now()
			rows, e := q.QueryContext(ctx, sql, f.tenantA, application, release.Hash)
			if e != nil {
				return e
			}
			defer rows.Close()
			n := 0
			for rows.Next() {
				var id string
				if e = rows.Scan(&id); e != nil {
					return e
				}
				n++
			}
			if e = rows.Err(); e != nil {
				return e
			}
			if n < 1 || n > 101 {
				return errors.New("benchmark page count")
			}
			elapsed = float64(time.Since(start).Microseconds()) / 1000
			return nil
		})
		return elapsed, err
	}
	prefix := `SELECT r.id::text FROM runtime.records r WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.metadata_revision=1 AND r.ir_hash=$3 AND r.entity_id='entity.contact'`
	numeric := prefix + ` ORDER BY (r.custom_data->'field.contact.balance'->>'amount')::numeric,r.id LIMIT 101`
	// Reproduce the original policy under the same pinned hardware and data. This
	// fixture-only policy change never disables RLS and is restored before gates.
	old := `ALTER POLICY tenant_scope ON runtime.records USING(tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id)) WITH CHECK(tenant_id=iam.current_tenant_id() AND iam.tenant_matches_scope(tenant_id))`
	fixed := `ALTER POLICY tenant_scope ON runtime.records USING(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id()))) WITH CHECK(tenant_id=(SELECT iam.current_tenant_id()) AND (SELECT iam.tenant_matches_scope(iam.current_tenant_id())))`
	if _, err = f.operator.Exec(old); err != nil {
		t.Fatal(err)
	}
	baselineMS, baselineErr := run(numeric)
	if _, err = f.operator.Exec(fixed); err != nil {
		t.Fatal(err)
	}
	var pg *pgconn.PgError
	if baselineErr == nil {
		t.Logf("baseline original policy: completed %.3fms under unchanged2s budget; earlier timeout is workload/hardware dependent", baselineMS)
	} else if errors.As(baselineErr, &pg) && pg.Code == "57014" {
		t.Log("baseline original policy: EXPECTED FAIL SQLSTATE57014 under2s budget")
	} else {
		t.Fatal("baseline failed outside its statement budget", baselineErr)
	}
	for _, c := range []struct {
		name, sql string
		budget    float64
	}{{"key_sql", prefix + ` ORDER BY r.id LIMIT 101`, 100}, {"numeric_sql", numeric, 500}, {"selective_sql", prefix + ` AND r.custom_data->>'field.contact.status'='inactive' ORDER BY (r.custom_data->'field.contact.balance'->>'amount')::numeric,r.id LIMIT 101`, 500}} {
		for range 3 {
			if _, err = run(c.sql); err != nil {
				t.Fatal(err)
			}
		}
		times := []float64{}
		start := time.Now()
		for range 100 {
			elapsed, e := run(c.sql)
			if e != nil {
				t.Fatal(c.name, e)
			}
			times = append(times, elapsed)
		}
		if reportLatency(t, c.name, times, time.Since(start)) > c.budget {
			t.Fatal(c.name, "p95 budget")
		}
	}
	if err = postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, e := m3Scope(f, ctx, q, principal, f.tenantA); e != nil {
			return e
		}
		var raw []byte
		if e := q.QueryRowContext(ctx, `EXPLAIN(ANALYZE,BUFFERS,VERBOSE,FORMAT JSON) `+numeric, f.tenantA, application, release.Hash).Scan(&raw); e != nil {
			return e
		}
		t.Logf("numeric_plan=%s", raw)
		var root []map[string]any
		if json.Unmarshal(raw, &root) != nil {
			return errors.New("explain parse")
		}
		found := false
		var visit func(any)
		visit = func(n any) {
			switch x := n.(type) {
			case map[string]any:
				if name, ok := x["Subplan Name"].(string); ok && strings.Contains(name, "InitPlan") {
					if x["Actual Loops"] != float64(1) {
						t.Error("InitPlan executed repeatedly")
					}
					output, _ := json.Marshal(x["Output"])
					if strings.Contains(string(output), "iam.tenant_matches_scope(") {
						found = true
					}
				}
				for _, v := range x {
					visit(v)
				}
			case []any:
				for _, v := range x {
					visit(v)
				}
			}
		}
		visit(root[0])
		if !found {
			return errors.New("membership InitPlan absent")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(h)
	defer server.Close()
	query := m5Query()
	query.Limit = 100
	query.Sort = &data.Sort{Field: "field.contact.balance"}
	payload, _ := json.Marshal(query)
	program, err := compiler.Restore(release.IR, release.Hash, compiler.SupportedIRVersions())
	if err != nil {
		t.Fatal("benchmark stored IR")
	}
	scope, err := data.NewScope(program, 1)
	if err != nil {
		t.Fatal("benchmark scope")
	}
	// R3b configuration is explicit: preserve the unindexed SQL baseline above,
	// then build the governed index through the same authenticated non-test API.
	// Dataset, HTTP clients/samples, budgets and expected page are unchanged.
	indexStart := time.Now()
	index := m9rIndexReady(t, h, base+"/indexes/field.contact.balance", token)
	if index.ScannedRows != 100000 || index.VerifiedRows != 100000 {
		t.Fatal("numeric index coverage count")
	}
	t.Logf("governed_numeric_index state=%s checkpoints=%d scanned_rows=%d build_ms=%.3f", index.State, index.Checkpoint, index.ScannedRows, float64(time.Since(indexStart).Microseconds())/1000)
	if _, err = f.operator.Exec(`ANALYZE runtime.numeric_indexes; ANALYZE runtime.numeric_projections`); err != nil {
		t.Fatal("projection statistics", err)
	}
	if err = postgres.WithRuntime(ctx, f.db, func(ctx context.Context, q postgres.Executor) error {
		access, e := m3Scope(f, ctx, q, principal, f.tenantA)
		if e != nil {
			return e
		}
		observer := &pagePlanObserver{Executor: q}
		page, e := (data.Store{}).Query(ctx, observer, scope, access, "entity.contact", query, func(context.Context, string, data.Resource) error { return nil }, func(context.Context, string, data.Resource) (policy.DataScope, error) {
			return policy.DataScope{Kind: "ALL"}, nil
		})
		if e != nil {
			return e
		}
		if len(page.Records) != 100 {
			return errors.New("runtime plan page size")
		}
		if _, e = q.ExecContext(ctx, `SET LOCAL plan_cache_mode=force_generic_plan`); e != nil {
			return e
		}
		var raw []byte
		if e = q.QueryRowContext(ctx, `EXPLAIN(ANALYZE,BUFFERS,VERBOSE,FORMAT JSON) `+observer.query, observer.args...).Scan(&raw); e != nil {
			return e
		}
		t.Logf("runtime_page_plan=%s", raw)
		var root []map[string]any
		if json.Unmarshal(raw, &root) != nil {
			return errors.New("indexed explain parse")
		}
		indexed := false
		var visit func(any)
		visit = func(node any) {
			switch x := node.(type) {
			case map[string]any:
				if x["Relation Name"] == "numeric_projections" {
					indexed = x["Index Name"] == "numeric_projection_asc" && x["Actual Rows"] == float64(101) && x["Actual Loops"] == float64(1)
				}
				if x["Relation Name"] == "records" && x["Actual Rows"].(float64)*x["Actual Loops"].(float64) > 202 {
					t.Error("indexed page scanned records outside its authorized page")
				}
				for _, v := range x {
					visit(v)
				}
			case []any:
				for _, v := range x {
					visit(v)
				}
			}
		}
		visit(root[0])
		if !indexed {
			return errors.New("authorized 101-row numeric index scan absent")
		}
		return nil
	}); err != nil {
		t.Fatal("runtime page plan", err)
	}
	expected := map[int]string{}
	rows, err := f.operator.Query(`SELECT (custom_data->'field.contact.balance'->>'amount')::int,id::text FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND (custom_data->'field.contact.balance'->>'amount')::int<=100`, f.tenantA, application)
	if err != nil {
		t.Fatal("expected page")
	}
	for rows.Next() {
		var amount int
		var id string
		if err = rows.Scan(&amount, &id); err != nil {
			rows.Close()
			t.Fatal("expected row")
		}
		expected[amount] = id
	}
	if err = rows.Err(); err != nil {
		t.Fatal("expected rows")
	}
	rows.Close()
	if len(expected) != 100 {
		t.Fatal("expected page size")
	}
	call := func() (float64, error) {
		start := time.Now()
		request, e := http.NewRequest("POST", server.URL+base+"/query", bytes.NewReader(payload))
		if e != nil {
			return 0, e
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		response, e := server.Client().Do(request)
		if e != nil {
			return 0, e
		}
		defer response.Body.Close()
		var page data.Page
		e = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page)
		if e != nil || response.StatusCode != 200 || len(page.Records) != 100 || page.Next == "" {
			return 0, fmt.Errorf("HTTP status%d", response.StatusCode)
		}
		for i, row := range page.Records {
			amount, ok := row.Values["field.contact.balance"].(map[string]any)
			if !ok || row.ID != expected[i+1] || amount["amount"] != fmt.Sprint(i+1) || amount["currency"] != "USD" || !strings.HasPrefix(fmt.Sprint(row.Values["field.contact.name"]), fmt.Sprintf("%08d", i+1)) {
				return 0, errors.New("HTTP page contents/order/tenant mismatch")
			}
		}
		return float64(time.Since(start).Microseconds()) / 1000, nil
	}
	for range 3 {
		if _, err = call(); err != nil {
			t.Fatal(err)
		}
	}
	samples := make(chan float64, 100)
	failures := make(chan error, 100)
	var wg sync.WaitGroup
	start := time.Now()
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				elapsed, e := call()
				if e != nil {
					failures <- e
				} else {
					samples <- elapsed
				}
			}
		}()
	}
	wg.Wait()
	close(samples)
	close(failures)
	for e := range failures {
		t.Error("API", e)
	}
	times := []float64{}
	for elapsed := range samples {
		times = append(times, elapsed)
	}
	if len(times) != 100 {
		t.Fatal("HTTP sample count")
	}
	if reportLatency(t, "numeric_http_10clients", times, time.Since(start)) > 750 {
		t.Fatal("API p95 budget")
	}
}

type pagePlanObserver struct {
	postgres.Executor
	query string
	args  []any
}

func (o *pagePlanObserver) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	o.query = query
	o.args = append([]any{}, args...)
	return o.Executor.QueryContext(ctx, query, args...)
}

// Refuse to label a run as the reference tier just because a caller supplied a
// string flag. Verify the container limits and the connected server's cluster ID.
func requireM9RReferencePostgres(t *testing.T) {
	t.Helper()
	info, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.NanoCpus}} {{.HostConfig.Memory}} {{.HostConfig.MemorySwap}} {{.Image}} {{.Config.Image}}", "framework-m0-postgres-1").Output()
	fields := strings.Fields(string(info))
	if err != nil || len(fields) != 5 || fields[0] != "4000000000" || fields[1] != "8589934592" || fields[2] != "8589934592" || fields[4] != "postgres:18.6" {
		t.Fatal("performance gate requires make reference-up: pinned PG18.6 at4 CPUs/8GiB, with swap disabled")
	}
	u, err := url.Parse(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("reference binding")
	}
	u.User = url.UserPassword("postgres", os.Getenv("POSTGRES_PASSWORD"))
	probe, err := postgres.Open(u.String())
	if err != nil {
		t.Fatal("reference probe")
	}
	defer probe.Close()
	var actual string
	if err = probe.QueryRow(`SELECT system_identifier::text FROM pg_catalog.pg_control_system()`).Scan(&actual); err != nil {
		t.Fatal("reference cluster identity")
	}
	inside, err := exec.Command("docker", "exec", "framework-m0-postgres-1", "psql", "-U", "postgres", "-d", "postgres", "-Atc", "SELECT system_identifier::text FROM pg_catalog.pg_control_system()").Output()
	if err != nil || strings.TrimSpace(string(inside)) != actual {
		t.Fatal("connected PostgreSQL is not the constrained reference container")
	}
	var shared, effective, work, cost string
	if err = probe.QueryRow(`SELECT current_setting('shared_buffers'),current_setting('effective_cache_size'),current_setting('work_mem'),current_setting('random_page_cost')`).Scan(&shared, &effective, &work, &cost); err != nil || shared != "2GB" || effective != "6GB" || work != "4MB" || cost != "1.1" {
		t.Fatal("performance gate requires actual ADR048 reference memory/planner settings; run make reference-up")
	}
	t.Logf("verified_reference profile=ADR058-v2-candidate cpus=4 memory_bytes=8589934592 swap_disabled=true image=%s shared_buffers=%s effective_cache_size=%s work_mem=%s random_page_cost=%s", fields[3], shared, effective, work, cost)
}
