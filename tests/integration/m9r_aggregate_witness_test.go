//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func TestM9RSQLAggregateNumericWitnessLegacyAndTampering(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	want := aggregateSeed(t, f, h, base, token, app, 317)
	s := scalarScope(t, h, base, token)
	for _, field := range []string{aggregateUnits, aggregateRatio, aggregateBalance} {
		m9rIndexReady(t, h, base+"/indexes/"+field, token)
		if got := aggregateResult(t, h, base, token, aggregateQuery("sum", field)); got.Count != 317 || got.Sum != want[field] {
			t.Fatal("certified projection disagrees with independent raw oracle")
		}
	}
	var certified int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND aggregate_valid_v1 IS TRUE`, f.tenantA, app).Scan(&certified))
	if certified != 951 {
		t.Fatal("numeric build did not certify every input")
	}
	principal, err := f.verifier.Verify(token)
	scalarSQL(t, err)
	err = postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		if _, err := m3Scope(f, ctx, q, principal, f.tenantA); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `UPDATE runtime.numeric_projections SET aggregate_valid_v1=true WHERE tenant_id=$1`, f.tenantA)
		return err
	})
	nativePGCode(t, err, "42501")
	_, err = f.db.Exec(`SELECT runtime.certify_aggregate_projection_v1()`)
	nativePGCode(t, err, "42501")
	// A caller-supplied true flag cannot certify a different projected number,
	// even through this trusted operator-only fixture path.
	_, err = f.operator.Exec(`UPDATE runtime.numeric_projections SET value_number=1,aggregate_valid_v1=true WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	var valid bool
	scalarSQL(t, f.operator.QueryRow(`SELECT aggregate_valid_v1 FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, aggregateUnits).Scan(&valid))
	if valid {
		t.Fatal("projection value not bound to certified raw value")
	}
	if w := m5Call(h, base+"/query", token, aggregateQuery("sum", aggregateUnits)); w.Code != 503 {
		t.Fatal("false witness produced a partial sum", w.Code)
	}
	_, err = f.operator.Exec(`UPDATE runtime.numeric_projections SET value_number=9007199254740991,aggregate_valid_v1=false WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('1')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	// Simulate pre53 rows without rewriting source/customer data. This owned
	// disposable operator fixture restores the trigger before any API call.
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`ALTER TABLE runtime.numeric_projections DISABLE TRIGGER certify_aggregate_projection_v1`)
	scalarSQL(t, err)
	_, err = tx.Exec(`UPDATE runtime.numeric_projections SET aggregate_valid_v1=NULL WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app)
	scalarSQL(t, err)
	_, err = tx.Exec(`ALTER TABLE runtime.numeric_projections ENABLE TRIGGER certify_aggregate_projection_v1`)
	scalarSQL(t, err)
	scalarSQL(t, tx.Commit())
	for _, field := range []string{aggregateUnits, aggregateRatio, aggregateBalance} {
		if got := aggregateResult(t, h, base, token, aggregateQuery("sum", field)); got.Count != 317 || got.Sum != want[field] {
			t.Fatal("uncertified legacy projection bypassed raw contract")
		}
	}
	_, err = f.operator.Exec(`DELETE FROM runtime.numeric_projections WHERE tenant_id=$1 AND app_id=$2 AND field_id=$3 AND id=md5('2')::uuid`, f.tenantA, app, aggregateUnits)
	scalarSQL(t, err)
	if w := m5Call(h, base+"/query", token, aggregateQuery("sum", aggregateUnits)); w.Code != 503 {
		t.Fatal("missing READY projection silently shrank aggregate", w.Code)
	}
	if got := aggregateResult(t, h, base, token, aggregateQuery("count", "")); got.Count != 317 {
		t.Fatal("count depended on unrelated projection corruption")
	}
	_, err = f.operator.Exec(`INSERT INTO runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_number,source_version)
 SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,$5,id,9007199254740991,row_version FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND id=md5('2')::uuid`, f.tenantA, app, s.DataRevision(), s.DataHash(), aggregateUnits)
	scalarSQL(t, err)
	if got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateUnits)); got.Sum != want[aggregateUnits] {
		t.Fatal("projection repair changed exact result")
	}
	faulty := r5Handler(t, f, m8AfterAuditFailure{})
	if w := m5Call(faulty, base+"/commands", token, data.Command{Operation: "create", Values: m5Values("Aborted numeric projection")}); w.Code != 503 {
		t.Fatal("witness command fault fixture", w.Code)
	}
	if got := aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateBalance)); got.Count != 317 || got.Sum != want[aggregateBalance] {
		t.Fatal("aborted source/projection/audit partially committed")
	}
	scalarSQL(t, (data.Store{}).Ready(context.Background(), f.db))
}
