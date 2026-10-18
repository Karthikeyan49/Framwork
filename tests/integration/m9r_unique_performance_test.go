//go:build integration && performance

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func TestM9RUniqueConstraintPerformanceFloor(t *testing.T) {
	m8Database(t)
	requireM9RReferencePostgres(t)
	f, h, base, token, app, s := uniqueFixture(t, 100000, false)
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	for i := 0; i < 9; i++ {
		id := uuid(t)
		scalarSQL(t, (tenant.Store{}).Provision(context.Background(), tx, id, "Integrity reference tenant", f.bob, "owner", true))
		_, err = tx.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3)`, id, app, s.DataHash())
		scalarSQL(t, err)
		_, err = tx.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name',repeat('Other tenant integrity text ',35),'field.contact.memo0','unique-'||i::text,'field.contact.status','active','field.contact.balance',jsonb_build_object('amount',i::text,'currency','USD')),$4,$4,$4 FROM generate_series(1,10000)i`, id, app, s.DataHash(), f.bob)
		scalarSQL(t, err)
	}
	scalarSQL(t, tx.Commit())
	var width int
	scalarSQL(t, f.operator.QueryRow(`SELECT avg(pg_column_size(r))::int FROM runtime.records r WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&width))
	if width < 900 {
		t.Fatal("nonrepresentative integrity width", width)
	}
	begin := time.Now()
	x := uniqueReady(t, h, base+"/unique-constraints/field.contact.memo0", token)
	if x.ScannedRows != 100000 || x.VerifiedRows != 100000 {
		t.Fatal("100k integrity coverage")
	}
	t.Logf("unique_reference hot_rows=100000 tenants=10 other_rows_each=10000 avg_record_bytes=%d checkpoints=%d build=%s", width, x.Checkpoint, time.Since(begin))
	server := httptest.NewServer(h)
	defer server.Close()
	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	for _, conflict := range []bool{false, true} {
		call := func(i int) (float64, int) {
			value := fmt.Sprintf("capacity-%d", i)
			want := http.StatusOK
			if conflict {
				value = "unique-1"
				want = http.StatusConflict
			}
			raw, _ := json.Marshal(data.Command{Key: uuid(t), Operation: "create", Values: uniqueValues(value)})
			r, _ := http.NewRequest("POST", server.URL+base+"/commands", bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+token)
			start := time.Now()
			response, err := client.Do(r)
			if err != nil {
				return float64(time.Since(start).Microseconds()) / 1000, 0
			}
			defer response.Body.Close()
			var result map[string]any
			if json.NewDecoder(response.Body).Decode(&result) != nil {
				return 0, 0
			}
			if response.StatusCode == want && !conflict && result["version"] != float64(1) {
				return 0, 0
			}
			return float64(time.Since(start).Microseconds()) / 1000, response.StatusCode
		}
		want := 200
		if conflict {
			want = 409
		}
		for i := -3; i < 0; i++ {
			if _, code := call(i); code != want {
				t.Fatal("integrity warmup", code)
			}
		}
		samples := make([]float64, 100)
		codes := make([]int, 100)
		jobs := make(chan int)
		var group sync.WaitGroup
		begin := time.Now()
		for range 10 {
			group.Go(func() {
				for i := range jobs {
					samples[i], codes[i] = call(i)
				}
			})
		}
		for i := range 100 {
			jobs <- i
		}
		close(jobs)
		group.Wait()
		for _, code := range codes {
			if code != want {
				t.Fatalf("unique unexpected HTTP status %d (want %d)", code, want)
			}
		}
		name := "unique_fresh_key_write"
		if conflict {
			name = "unique_expected_conflict"
		}
		if p95 := reportLatency(t, name, samples, time.Since(begin)); p95 > 500 {
			t.Fatalf("unique p95 %.3f exceeds unchanged 500ms", p95)
		}
		t.Logf("case=%s measured_requests=100 clients=10 expected_conflicts=%d unexpected_errors=0", name, map[bool]int{true: 100, false: 0}[conflict])
	}
	var count int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&count))
	if count != 100103 {
		t.Fatal("conflicts changed source count", count)
	}
}
