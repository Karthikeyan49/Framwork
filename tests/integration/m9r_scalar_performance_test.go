//go:build integration && performance

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

// Observe below the transaction port, without wrapping away RuntimeSnapshot's
// proof. SQL/arguments stay in fixture memory and are never printed in logs.
type scalarQueryTrace struct {
	mu    sync.Mutex
	query string
	args  []any
}

func (o *scalarQueryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(d.SQL, "WITH selected_page AS MATERIALIZED") && strings.Contains(d.SQL, "runtime.scalar_projections") {
		o.mu.Lock()
		o.query = d.SQL
		o.args = append([]any(nil), d.Args...)
		o.mu.Unlock()
	}
	return ctx
}
func (*scalarQueryTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
func TestM9RScalarIndexPerformanceFloor(t *testing.T) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, app := scalarFixture(t, 1)
	scope := scalarScope(t, h, base, token)
	hash := scope.Hash()
	ctx := context.Background()
	tenants := []string{f.tenantA, f.tenantB}
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	for range 8 {
		id := uuid(t)
		scalarSQL(t, (tenant.Store{}).Provision(ctx, tx, id, "Scalar reference tenant", f.bob, "owner", true))
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
 SELECT $1,$2,2,$3,'entity.contact',gen_random_uuid(),jsonb_build_object('field.contact.name',lpad(i::text,8,'0')||repeat(' scalar reference text',18),
 'field.contact.memo0',CASE WHEN i%101=0 THEN NULL ELSE (ARRAY['Alpha','alpha','Å','தமிழ்','ß','Ω'])[1+i%6]||lpad((i%50000)::text,8,'0') END,
 'field.contact.joined',CASE WHEN i%103=0 THEN NULL ELSE to_char(date '1000-01-01'+(i%12000),'YYYY-MM-DD') END,
 'field.contact.status',CASE WHEN i%997=0 THEN 'inactive' ELSE 'active' END,'field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,CASE WHEN i%2=0 THEN $4::uuid ELSE $5::uuid END FROM generate_series(1,$6::int)i`, id, app, hash, owner, f.viewer, count)
		scalarSQL(t, err)
	}
	var width int
	scalarSQL(t, f.operator.QueryRow(`SELECT avg(pg_column_size(r))::int FROM runtime.records r WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&width))
	t.Logf("scalar_reference hot_rows=100000 tenants=10 other_rows_each=10000 avg_record_bytes=%d warmup=3 samples_per_case=100 clients=10", width)
	trace := &scalarQueryTrace{}
	config, err := pgx.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal("reference trace binding")
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
	buildStart := time.Now()
	for _, field := range []string{"field.contact.memo0", "field.contact.joined"} {
		index := m9rIndexReady(t, h, base+"/indexes/"+field, token)
		if index.ScannedRows != 100000 || index.VerifiedRows != 100000 {
			t.Fatal("scalar capacity coverage")
		}
		t.Logf("scalar_index field=%s checkpoints=%d copied=%d verified=%d", field, index.Checkpoint, index.ScannedRows, index.VerifiedRows)
	}
	t.Logf("scalar_index_build_ms=%.3f", float64(time.Since(buildStart).Microseconds())/1000)
	_, err = f.operator.Exec(`ANALYZE runtime.records;ANALYZE runtime.scalar_indexes;ANALYZE runtime.scalar_projections`)
	scalarSQL(t, err)
	server := httptest.NewServer(h)
	defer server.Close()
	principal, _ := f.verifier.Verify(token)
	for _, field := range []string{"field.contact.memo0", "field.contact.joined"} {
		for _, desc := range []bool{false, true} {
			label := "text"
			expr := "custom_data->>'field.contact.memo0'"
			indexName := "scalar_text_asc"
			if field == "field.contact.joined" {
				label = "date"
				expr = "(custom_data->>'field.contact.joined')::date"
				indexName = "scalar_date_asc"
			}
			direction := "ASC"
			if desc {
				direction = "DESC"
				indexName = strings.TrimSuffix(indexName, "asc") + "desc"
			}
			label += "_" + strings.ToLower(direction)
			expected := []string{}
			rows, err := f.operator.Query(`SELECT id::text FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=2 AND ir_hash=$3 AND entity_id='entity.contact' ORDER BY `+expr+` `+direction+` NULLS LAST,id ASC LIMIT 50100`, f.tenantA, app, hash)
			scalarSQL(t, err)
			for rows.Next() {
				var id string
				scalarSQL(t, rows.Scan(&id))
				expected = append(expected, id)
			}
			scalarSQL(t, rows.Err())
			rows.Close()
			if len(expected) != 50100 {
				t.Fatal("scalar reference oracle size")
			}
			query := m5Query()
			query.Projection = append(query.Projection, "field.contact.memo0", "field.contact.joined")
			query.Limit = 100
			query.Sort = &data.Sort{Field: field, Desc: desc}
			for _, offset := range []int{0, 100, 50000} {
				if offset == 50000 {
					// Use an actual API fingerprint with the independently selected
					// fixture boundary. Cursors convey position, never authorization.
					encoded, err := base64.RawURLEncoding.DecodeString(query.After)
					scalarSQL(t, err)
					var cursor map[string]json.RawMessage
					if json.Unmarshal(encoded, &cursor) != nil {
						t.Fatal("deep cursor template")
					}
					var value json.RawMessage
					scalarSQL(t, f.operator.QueryRow(`SELECT custom_data->$4 FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, expected[offset-1], field).Scan(&value))
					cursor["id"], _ = json.Marshal(expected[offset-1])
					cursor["value"] = value
					encoded, _ = json.Marshal(cursor)
					query.After = base64.RawURLEncoding.EncodeToString(encoded)
				}
				payload, _ := json.Marshal(query)
				call := func() (float64, string, error) {
					start := time.Now()
					request, err := http.NewRequest("POST", server.URL+base+"/query", bytes.NewReader(payload))
					if err != nil {
						return 0, "", errors.New("reference request construction")
					}
					request.Header.Set("Authorization", "Bearer "+token)
					request.Header.Set("Content-Type", "application/json")
					response, err := server.Client().Do(request)
					if err != nil {
						return 0, "", errors.New("reference HTTP unavailable")
					}
					defer response.Body.Close()
					var page data.Page
					if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page) != nil || response.StatusCode != 200 || len(page.Records) != 100 || page.Next == "" {
						return 0, "", fmt.Errorf("reference response status %d", response.StatusCode)
					}
					for i, r := range page.Records {
						if r.ID != expected[offset+i] {
							return 0, "", errors.New("scalar HTTP ordering/tenant/cursor oracle mismatch")
						}
					}
					return float64(time.Since(start).Microseconds()) / 1000, page.Next, nil
				}
				var next string
				for range 3 {
					_, next, err = call()
					scalarSQL(t, err)
				}
				// Inspect the exact API-produced SQL with its original bindings in memory.
				trace.mu.Lock()
				statement, args := trace.query, append([]any(nil), trace.args...)
				trace.mu.Unlock()
				if statement == "" {
					t.Fatal("real API did not choose scalar index")
				}
				scalarSQL(t, postgres.WithRuntime(ctx, traced, func(ctx context.Context, q postgres.Executor) error {
					if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
						return err
					}
					if _, err := q.ExecContext(ctx, `SET LOCAL plan_cache_mode=force_generic_plan`); err != nil {
						return err
					}
					var raw []byte
					if err := q.QueryRowContext(ctx, `EXPLAIN(ANALYZE,BUFFERS,FORMAT JSON) `+statement, args...).Scan(&raw); err != nil {
						return err
					}
					var roots []any
					if json.Unmarshal(raw, &roots) != nil {
						return errors.New("scalar plan decode")
					}
					found := false
					var projectionVisits, sourceVisits float64
					var walk func(any) error
					walk = func(x any) error {
						switch n := x.(type) {
						case map[string]any:
							if n["Relation Name"] == "scalar_projections" || n["Relation Name"] == "records" {
								count, _ := n["Actual Rows"].(float64)
								loops, _ := n["Actual Loops"].(float64)
								removed, _ := n["Rows Removed by Filter"].(float64)
								if n["Relation Name"] == "records" {
									sourceVisits += (count + removed) * loops
								} else {
									projectionVisits += (count + removed) * loops
								}
							}
							if n["Relation Name"] == "scalar_projections" && n["Index Name"] == indexName {
								found = true
								count, _ := n["Actual Rows"].(float64)
								loops, _ := n["Actual Loops"].(float64)
								if count*loops > 201 {
									return errors.New("scalar index did not stop at first/cursor page")
								}
							}
							if n["Relation Name"] == "records" {
								count, _ := n["Actual Rows"].(float64)
								loops, _ := n["Actual Loops"].(float64)
								if count*loops > 302 {
									return errors.New("scalar page scanned unbounded records")
								}
							}
							for _, child := range n {
								if err := walk(child); err != nil {
									return err
								}
							}
						case []any:
							for _, child := range n {
								if err := walk(child); err != nil {
									return err
								}
							}
						}
						return nil
					}
					if err := walk(roots); err != nil {
						return err
					}
					if !found {
						return errors.New("requested scalar ordering index absent")
					}
					t.Logf("scalar_plan kind=%s offset=%d ordering_index=%s projection_visits=%.0f source_visits=%.0f", label, offset, indexName, projectionVisits, sourceVisits)
					return nil
				}))
				times := make(chan float64, 100)
				failures := make(chan error, 100)
				var wg sync.WaitGroup
				start := time.Now()
				for range 10 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						for range 10 {
							elapsed, _, err := call()
							if err != nil {
								failures <- err
							} else {
								times <- elapsed
							}
						}
					}()
				}
				wg.Wait()
				close(times)
				close(failures)
				for err := range failures {
					t.Error(err)
				}
				samples := []float64{}
				for v := range times {
					samples = append(samples, v)
				}
				if len(samples) != 100 {
					t.Fatal("scalar HTTP sample count")
				}
				if reportLatency(t, fmt.Sprintf("scalar_%s_offset%d_http10", label, offset), samples, time.Since(start)) > 750 {
					t.Fatal("scalar HTTP p95 exceeds unchanged 750ms")
				}
				query.After = next
			}
		}
	}
}
