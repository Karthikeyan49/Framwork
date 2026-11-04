//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

// TestM11RFreezeRacesRecordWrites: "freeze racing a record write" was reasoned statically in the M11 review and
// never executed. Writers hammer a sandbox through the governed command route while it is frozen; afterwards the
// evidence manifest, the rows in the dataset and the number of acknowledged writes must agree exactly. A write
// acknowledged after the freeze snapshot, or a counted record that was not acknowledged, would make the evidence
// a statement about data that is not there.
func TestM11RFreezeRacesRecordWrites(t *testing.T) {
	m8Database(t)
	f, h, _, alice, app := m5Fixture(t)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	witness := m9rDatasetWitness(t, h, root+"/versions/1", alice)
	delays := []time.Duration{0, 20 * time.Millisecond, 50 * time.Millisecond, 120 * time.Millisecond, 300 * time.Millisecond}
	var total int64
	for round, delay := range delays {
		box := m11Create(t, h, root, alice, witness, "BLOCK", 200)
		path := root + "/datasets/" + box.ID + "/bindings/1/entities/entity.contact"
		var acknowledged, unexpected atomic.Int64
		var tally sync.Map // response code -> *atomic.Int64
		count := func(key string) {
			c, _ := tally.LoadOrStore(key, new(atomic.Int64))
			c.(*atomic.Int64).Add(1)
		}
		var stop atomic.Bool
		var wg sync.WaitGroup
		for w := 0; w < 12; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 400 && !stop.Load(); i++ {
					resp := m5Call(h, path+"/commands", alice, data.Command{Operation: "create", Values: m5Values(fmt.Sprintf("racer %d", i))})
					var refused struct {
						Code string `json:"code"`
					}
					_ = json.Unmarshal(resp.Body.Bytes(), &refused)
					count(fmt.Sprintf("%d %s", resp.Code, refused.Code))
					switch resp.Code {
					case 200:
						acknowledged.Add(1)
					case 409, 503: // frozen, or a serialization conflict: nothing was written
					default:
						unexpected.Add(1)
					}
				}
			}()
		}
		time.Sleep(delay)
		resp := m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", alice, struct{}{})
		stop.Store(true)
		wg.Wait()
		if resp.Code != 200 {
			t.Fatalf("round %d: freeze answered %d %s", round, resp.Code, resp.Body.String())
		}
		var ev data.SandboxEvidence
		var body struct {
			Records struct {
				Count int `json:"count"`
			} `json:"records"`
		}
		if json.Unmarshal(resp.Body.Bytes(), &ev) != nil || json.Unmarshal(ev.Body, &body) != nil {
			t.Fatalf("round %d: evidence contract %s", round, resp.Body.String())
		}
		var stored int
		if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.records r JOIN runtime.datasets d ON d.tenant_id=r.tenant_id AND d.app_id=r.app_id
 JOIN runtime.dataset_generations g ON g.tenant_id=d.tenant_id AND g.app_id=d.app_id AND g.dataset_id=d.id AND g.generation=d.schema_generation
 WHERE d.tenant_id=$1 AND d.app_id=$2 AND d.id=$3 AND r.metadata_revision=g.anchor_revision AND r.ir_hash=g.anchor_hash`, f.tenantA, app, box.ID).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if unexpected.Load() != 0 {
			t.Errorf("round %d: %d writes answered neither 200 nor 409/503", round, unexpected.Load())
		}
		total += acknowledged.Load()
		if int64(body.Records.Count) != int64(stored) || int64(stored) != acknowledged.Load() {
			t.Errorf("round %d (freeze after %v): evidence counts %d records, the dataset holds %d, %d writes were acknowledged", round, delay, body.Records.Count, stored, acknowledged.Load())
		}
		var summary []string
		tally.Range(func(k, v any) bool {
			summary = append(summary, fmt.Sprintf("%s x%d", k, v.(*atomic.Int64).Load()))
			return true
		})
		sort.Strings(summary)
		t.Logf("round %d (freeze after %v): %d records, %d acknowledged; responses: %v", round, delay, stored, acknowledged.Load(), summary)
	}
	if total < 10 {
		t.Fatalf("only %d writes were acknowledged over %d rounds: the race was not exercised", total, len(delays))
	}
}
