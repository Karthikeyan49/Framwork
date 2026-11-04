//go:build integration && performance

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestM9RSearchPerformanceFloor(t *testing.T) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, app := searchFixture(t)
	scope := scalarScope(t, h, base, token)
	hash := scope.Hash()
	ctx := context.Background()
	tenants := []string{f.tenantA, f.tenantB}
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	for range 8 {
		id := uuid(t)
		scalarSQL(t, (tenant.Store{}).Provision(ctx, tx, id, "Search reference tenant", f.bob, "owner", true))
		tenants = append(tenants, id)
	}
	scalarSQL(t, tx.Commit())
	for i, id := range tenants {
		owner, count := f.bob, 10000
		if i == 0 {
			owner, count = f.alice, 100000
		}
		_, err = f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3)`, id, app, hash)
		scalarSQL(t, err)
		_, err = f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name',lpad(i::text,8,'0')||repeat(' representative search text',18),
 'field.contact.memo0',CASE WHEN i%1000=0 THEN 'orchidtarget harvest blue orchard' WHEN i%400=0 THEN 'harvest blue orchard' WHEN i%100=0 THEN 'harvest orchard' ELSE 'ordinary common orchard' END,
 'field.contact.status',CASE WHEN i%3=0 THEN 'active' ELSE 'inactive' END,'field.contact.parent',CASE WHEN i%7=0 THEN md5('1')::uuid::text ELSE NULL END,
 'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,CASE WHEN i%3=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int)i`, id, app, hash, owner, f.viewer, count)
		scalarSQL(t, err)
	}
	var width int
	scalarSQL(t, f.operator.QueryRow(`SELECT avg(pg_column_size(r))::int FROM runtime.records r WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&width))
	t.Logf("search_reference hot_rows=100000 tenants=10 other_rows_each=10000 avg_record_bytes=%d warmup=3 samples_per_case=100 clients=10", width)
	trace := &scalarQueryTrace{}
	config, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("search trace binding")
	}
	config.Tracer = trace
	traced := stdlib.OpenDB(*config)
	defer traced.Close()
	traced.SetMaxOpenConns(10)
	traced.SetMaxIdleConns(5)
	original := f.db
	f.db = traced
	h = r5Handler(t, f, audit.Postgres{})
	f.db = original
	start := time.Now()
	idx := m9rIndexReady(t, h, base+"/search-indexes/field.contact.memo0", token)
	if idx.ScannedRows != 100000 || idx.VerifiedRows != 100000 {
		t.Fatal("search capacity coverage")
	}
	t.Logf("search_index checkpoints=%d copied=%d verified=%d build_ms=%.3f", idx.Checkpoint, idx.ScannedRows, idx.VerifiedRows, float64(time.Since(start).Microseconds())/1000)
	_, err = f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.search_indexes;ANALYZE runtime.search_projections`)
	scalarSQL(t, err)
	server := httptest.NewServer(h)
	defer server.Close()
	principal, _ := f.verifier.Verify(token)
	for _, c := range []struct {
		name, text    string
		offset, limit int
		filter, join  bool
	}{
		{"rare_first", "orchidtarget", 0, 50, false, false}, {"rare_second", "orchidtarget", 50, 50, false, false},
		{"medium_first", "harvest", 0, 100, false, false}, {"medium_second", "harvest", 100, 100, false, false},
		{"plain_and", "harvest blue", 0, 100, false, false}, {"zero", "missingtarget", 0, 100, false, false},
		{"filtered", "harvest", 0, 100, true, false}, {"joined", "harvest", 0, 100, false, true},
	} {
		var candidates int
		scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND to_tsvector('pg_catalog.simple',coalesce(custom_data->>'field.contact.memo0',''))@@plainto_tsquery('pg_catalog.simple',$3)`, f.tenantA, app, c.text).Scan(&candidates))
		extra := ""
		if c.filter {
			extra = " AND custom_data->>'field.contact.status'='active'"
		}
		rows, err := f.operator.Query(`SELECT id::text FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND to_tsvector('pg_catalog.simple',coalesce(custom_data->>'field.contact.memo0',''))@@plainto_tsquery('pg_catalog.simple',$3)`+extra+` ORDER BY id`, f.tenantA, app, c.text)
		scalarSQL(t, err)
		ids := []string{}
		for rows.Next() {
			var id string
			scalarSQL(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		scalarSQL(t, rows.Err())
		rows.Close()
		q := searchQuery(c.text)
		q.Limit = c.limit
		if c.filter {
			q.Filter = &data.Filter{Op: "eq", Field: "field.contact.status", Value: json.RawMessage(`"active"`)}
		}
		if c.join {
			q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id", "field.contact.name"}}
		}
		if c.offset > 0 {
			w := m5Call(h, base+"/query", token, q)
			var p data.Page
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &p) != nil || p.Next == "" {
				t.Fatal("search second cursor unavailable", w.Code)
			}
			q.After = p.Next
		}
		expected := len(ids) - c.offset
		if expected > c.limit {
			expected = c.limit
		}
		if expected < 0 {
			t.Fatal("search oracle offset")
		}
		payload, _ := json.Marshal(q)
		call := func() (float64, error) {
			start := time.Now()
			request, err := http.NewRequest("POST", server.URL+base+"/query", bytes.NewReader(payload))
			if err != nil {
				return 0, errors.New("search reference request")
			}
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", "application/json")
			response, err := server.Client().Do(request)
			if err != nil {
				return 0, errors.New("search reference HTTP")
			}
			defer response.Body.Close()
			var p data.Page
			if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&p) != nil || response.StatusCode != 200 || len(p.Records) != expected {
				trace.mu.Lock()
				state := trace.lastSQLState
				trace.mu.Unlock()
				return 0, fmt.Errorf("search reference status=%d records=%d expected=%d sqlstate=%s", response.StatusCode, len(p.Records), expected, state)
			}
			if (p.Next != "") != (len(ids) > c.offset+c.limit) {
				return 0, errors.New("search reference next cursor")
			}
			for i, r := range p.Records {
				if r.ID != ids[c.offset+i] {
					return 0, errors.New("search reference exact oracle mismatch")
				}
			}
			return float64(time.Since(start).Microseconds()) / 1000, nil
		}
		for range 3 {
			_, err = call()
			if err != nil {
				t.Fatalf("search warmup %s: %v", c.name, err)
			}
		}
		trace.mu.Lock()
		statement, args := trace.query, append([]any(nil), trace.args...)
		trace.mu.Unlock()
		if statement == "" {
			t.Fatal("search API did not choose indexed plan")
		}
		scalarSQL(t, postgres.WithRuntime(ctx, traced, func(ctx context.Context, q postgres.Executor) error {
			if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
				return err
			}
			if _, err := q.ExecContext(ctx, `SELECT set_config('app.search_text',$1,true),set_config('plan_cache_mode','force_generic_plan',true)`, c.text); err != nil {
				return err
			}
			var raw []byte
			if err := q.QueryRowContext(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) `+statement, args...).Scan(&raw); err != nil {
				return err
			}
			var roots []any
			if json.Unmarshal(raw, &roots) != nil {
				return errors.New("search plan decode")
			}
			found := false
			var visits float64
			var walk func(any)
			walk = func(v any) {
				switch n := v.(type) {
				case map[string]any:
					if n["Index Name"] == "search_terms" {
						found = true
					}
					if n["Relation Name"] == "records" {
						rows, _ := n["Actual Rows"].(float64)
						loops, _ := n["Actual Loops"].(float64)
						removed, _ := n["Rows Removed by Filter"].(float64)
						visits += (rows + removed) * loops
					}
					for _, x := range n {
						walk(x)
					}
				case []any:
					for _, x := range n {
						walk(x)
					}
				}
			}
			walk(roots)
			t.Logf("search_plan case=%s matches=%d eligible=%d source_visits=%.0f gin=%v", c.name, candidates, len(ids), visits, found)
			if !found || visits > float64(candidates+303) {
				return errors.New("search GIN/source visit gate")
			}
			return nil
		}))
		samples := make([]float64, 100)
		failures := make(chan error, 100)
		jobs := make(chan int)
		var wg sync.WaitGroup
		begin := time.Now()
		for range 10 {
			wg.Go(func() {
				for i := range jobs {
					elapsed, err := call()
					samples[i] = elapsed
					if err != nil {
						failures <- err
					}
				}
			})
		}
		for i := range samples {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		p50, p95, p99 := percentile(samples, .5), percentile(samples, .95), percentile(samples, .99)
		t.Logf("search_%s_http10 n=100 p50_ms=%.3f p95_ms=%.3f p99_ms=%.3f throughput_per_s=%.2f errors=0", c.name, p50, p95, p99, 100/time.Since(begin).Seconds())
		if p95 > 750 {
			t.Fatal("search HTTP p95 budget")
		}
	}
}
