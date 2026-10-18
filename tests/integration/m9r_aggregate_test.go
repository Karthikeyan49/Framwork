//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

const aggregateUnits = "field.contact.units"
const aggregateRatio = "field.contact.ratio"
const aggregateBalance = "field.contact.balance"

func aggregateFixture(t *testing.T, requiredUnits ...bool) (*iamFixture, http.Handler, string, string, string) {
	t.Helper()
	f, h, base, token, app := m5Fixture(t)
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal("aggregate source fixture")
	}
	var doc metadata.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("aggregate source decoding")
	}
	doc.AppID = app
	for i := range doc.Definitions {
		if d := &doc.Definitions[i]; d.Entity != nil && d.ID == "entity.contact" {
			d.Entity.Fields = append(d.Entity.Fields, aggregateUnits, aggregateRatio)
		}
	}
	for i, id := range []string{aggregateUnits, aggregateRatio} {
		typ := "integer"
		if i == 1 {
			typ = "decimal"
		}
		doc.Definitions = append(doc.Definitions, metadata.Definition{SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Kind: "field", ID: id, Label: "Aggregate numeric input", Field: &metadata.Field{EntityRef: "entity.contact", Type: typ, Required: i == 0 && len(requiredUnits) == 1 && requiredUnits[0]}})
	}
	raw, _ = json.Marshal(doc)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", token, 1, raw); w.Code != 201 {
		t.Fatal("aggregate metadata fixture", w.Code)
	}
	return f, h, strings.Replace(base, "/versions/1/", "/versions/2/", 1), token, app
}

func aggregateQuery(op, field string) data.Query {
	return data.Query{Projection: []string{"field.contact.id", aggregateBalance, aggregateUnits, aggregateRatio}, Limit: 100, Aggregate: &data.Aggregate{Op: op, Field: field, Execution: data.SQLAggregateV1}}
}

func aggregateResult(t *testing.T, h http.Handler, base, token string, query data.Query) data.AggregateResult {
	t.Helper()
	w := m5Call(h, base+"/query", token, query)
	var page data.Page
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Aggregate == nil || len(page.Records) != 0 || page.Next != "" {
		t.Fatal("aggregate response contract", w.Code)
	}
	return *page.Aggregate
}

func aggregateSeed(t *testing.T, f *iamFixture, h http.Handler, base, token, app string, count int) map[string]string {
	t.Helper()
	scope := scalarScope(t, h, base, token)
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3) ON CONFLICT DO NOTHING`, f.tenantA, app, scope.Hash())
	scalarSQL(t, err)
	sums := map[string]*big.Rat{aggregateBalance: new(big.Rat), aggregateUnits: new(big.Rat), aggregateRatio: new(big.Rat)}
	money := []any{"1.000000000000000001", "-0.999999999999999999", nil, "999999999999999999.999999999999999999", "-999999999999999999.999999999999999999"}
	ratio := []any{"0.1", "0.2", "-0.3", nil, "0.000000000000000001", "-0.000000000000000001"}
	for i := 1; i <= count; i++ {
		values := m5Values(fmt.Sprintf("Aggregate fixture %d", i))
		values[aggregateBalance] = nil
		if amount := money[(i-1)%len(money)]; amount != nil {
			values[aggregateBalance] = map[string]any{"amount": amount, "currency": "USD"}
		}
		values[aggregateUnits] = json.Number("9007199254740991")
		values[aggregateRatio] = ratio[(i-1)%len(ratio)]
		for id, sum := range sums {
			v := values[id]
			if v == nil {
				continue
			}
			if id == aggregateBalance {
				v = v.(map[string]any)["amount"]
			}
			n, ok := new(big.Rat).SetString(fmt.Sprint(v))
			if !ok {
				t.Fatal("independent aggregate fixture oracle")
			}
			sum.Add(sum, n)
		}
		raw, _ := json.Marshal(values)
		_, err = tx.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 VALUES($1,$2,2,$3,'entity.contact',md5($4::text)::uuid,$5::jsonb,$6,$6,CASE WHEN $4::int%2=0 THEN $7::uuid ELSE $6::uuid END)`, f.tenantA, app, scope.Hash(), fmt.Sprint(i), string(raw), f.alice, f.viewer)
		scalarSQL(t, err)
	}
	scalarSQL(t, tx.Commit())
	want := map[string]string{}
	for id, sum := range sums {
		s := strings.TrimRight(strings.TrimRight(sum.FloatString(18), "0"), ".")
		if s == "" || s == "-0" {
			s = "0"
		}
		want[id] = s
	}
	return want
}

func TestM9RSQLAggregateExactFullSetAndLegacyCompatibility(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	want := aggregateSeed(t, f, h, base, token, app, 317)
	for _, field := range []string{aggregateUnits, aggregateRatio, aggregateBalance} {
		got := aggregateResult(t, h, base, token, aggregateQuery("sum", field))
		if got.Count != 317 || got.Sum != want[field] {
			t.Fatal("SQL aggregate disagrees with independent exact oracle")
		}
	}
	q := aggregateQuery("count", "")
	if got := aggregateResult(t, h, base, token, q); got.Count != 317 || got.Sum != "" {
		t.Fatal("full-set count")
	}
	q.Aggregate.Execution = ""
	if w := m5Call(h, base+"/query", token, q); w.Code != 400 {
		t.Fatal("legacy aggregate budget changed", w.Code)
	}
	q.Aggregate.Execution = data.SQLAggregateV1
	q.Filter = &data.Filter{Op: "eq", Field: "field.contact.id", Value: json.RawMessage(`"` + uuid(t) + `"`)}
	if got := aggregateResult(t, h, base, token, q); got.Count != 0 {
		t.Fatal("empty count")
	}
	q.Aggregate.Op, q.Aggregate.Field = "sum", aggregateRatio
	if got := aggregateResult(t, h, base, token, q); got.Count != 0 || got.Sum != "0" {
		t.Fatal("empty exact sum")
	}
	q.Filter = &data.Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"'; DROP TABLE runtime.records; --"`)}
	if got := aggregateResult(t, h, base, token, q); got.Count != 0 || got.Sum != "0" {
		t.Fatal("aggregate literal parameterization")
	}
	q.Filter = nil
	for _, mutate := range []func(*data.Query){
		func(q *data.Query) { q.Aggregate.Execution = "SQL_V2" },
		func(q *data.Query) { q.Aggregate.Op = "avg" },
		func(q *data.Query) { q.Aggregate.Field = "field.contact.name" },
		func(q *data.Query) { q.Projection = []string{"field.contact.id"} },
		func(q *data.Query) { q.Projection = append(q.Projection, q.Projection[0]) },
		func(q *data.Query) { q.Limit = 20 },
		func(q *data.Query) { q.RecordID = uuid(t) },
		func(q *data.Query) { q.After = "unsupported" },
		func(q *data.Query) { q.Sort = &data.Sort{Field: aggregateRatio} },
		func(q *data.Query) {
			q.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.id"}}
		},
		func(q *data.Query) { q.Search = &data.Search{Field: "field.contact.name", Text: "customer"} },
	} {
		bad := aggregateQuery("sum", aggregateRatio)
		mutate(&bad)
		if w := m5Call(h, base+"/query", token, bad); w.Code != 400 {
			t.Fatal("unsupported SQL aggregate admitted", w.Code)
		}
	}
}

func TestM9RSQLAggregateRejectsCorruptInputsEvenCancellation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t, true)
	aggregateSeed(t, f, h, base, token, app, 2)
	for _, tc := range []struct {
		field string
		value []any
	}{
		{aggregateUnits, []any{json.Number("9007199254740992"), json.Number("-9007199254740992")}},
		{aggregateUnits, []any{json.Number("1.5"), json.Number("-1.5")}},
		{aggregateUnits, []any{"1", "-1"}},
		{aggregateUnits, []any{nil, nil}},
		{aggregateRatio, []any{"1000000000000000001", "-1000000000000000001"}},
		{aggregateRatio, []any{json.Number("1"), json.Number("-1")}},
		{aggregateRatio, []any{"0.0000000000000000001", "-0.0000000000000000001"}},
		{aggregateRatio, []any{"NaN", "NaN"}},
		{aggregateRatio, []any{"1e0", "-1e0"}},
		{aggregateBalance, []any{map[string]any{"currency": "EUR", "amount": "1"}, map[string]any{"currency": "EUR", "amount": "-1"}}},
		{aggregateBalance, []any{map[string]any{"currency": "USD", "amount": json.Number("1")}, map[string]any{"currency": "USD", "amount": json.Number("-1")}}},
		{aggregateBalance, []any{map[string]any{"currency": "USD", "amount": "1", "extra": true}, map[string]any{"currency": "USD", "amount": "-1", "extra": true}}},
	} {
		tx, err := f.operator.Begin()
		scalarSQL(t, err)
		defer tx.Rollback()
		_, err = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA)
		scalarSQL(t, err)
		for i, value := range tc.value {
			raw, _ := json.Marshal(value)
			_, err := tx.Exec(`UPDATE runtime.records SET custom_data=jsonb_set(custom_data,ARRAY[$3::text],$4::jsonb),row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND id=md5($5::text)::uuid`, f.tenantA, app, tc.field, string(raw), fmt.Sprint(i+1))
			scalarSQL(t, err)
		}
		scalarSQL(t, tx.Commit())
		if w := m5Call(h, base+"/query", token, aggregateQuery("sum", tc.field)); w.Code != 503 || strings.Contains(w.Body.String(), `"aggregate"`) {
			t.Fatal("corrupt aggregate input produced a result", w.Code)
		}
		if got := aggregateResult(t, h, base, token, aggregateQuery("count", "")); got.Count != 2 {
			t.Fatal("count depends on unrelated projected values")
		}
	}
}

type aggregateExecutorWrapper struct{ postgres.Executor }

func TestM9RSQLAggregateRequiresClosedPolicyAndCertifiedExecutor(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	aggregateSeed(t, f, h, base, token, app, 2)
	scope := scalarScope(t, h, base, token)
	principal, _ := f.verifier.Verify(token)
	guard := data.Guard(func(context.Context, string, data.Resource) error { return nil })
	resolve := data.ScopeResolver(func(context.Context, string, data.Resource) (policy.DataScope, error) {
		return policy.DataScope{Kind: "ALL"}, nil
	})
	err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		a, err := m3Scope(f, ctx, q, principal, f.tenantA)
		if err != nil {
			return err
		}
		for _, port := range []postgres.Executor{f.db, aggregateExecutorWrapper{q}} {
			if _, err := (data.Store{}).QueryVisibleWithPolicy(ctx, port, scope, a, "entity.contact", aggregateQuery("count", ""), policy.Native{}, guard, resolve); err != data.ErrDenied {
				t.Fatal("uncertified aggregate executor admitted")
			}
		}
		if _, err := (data.Store{}).QueryVisible(ctx, q, scope, a, "entity.contact", aggregateQuery("count", ""), guard, resolve); err != data.ErrDenied {
			t.Fatal("opaque aggregate action proof admitted")
		}
		if _, err := (data.LegacyAdapter{}).Query(ctx, q, scope, a, "entity.contact", aggregateQuery("count", ""), guard, resolve); err != data.ErrDenied {
			t.Fatal("legacy adapter expanded implicitly")
		}
		if _, err := (data.WorkerSourceAdapter{}).Query(ctx, q, scope, a, "entity.contact", aggregateQuery("count", ""), guard, resolve); err != data.ErrDenied {
			t.Fatal("worker adapter expanded implicitly")
		}
		return nil
	})
	scalarSQL(t, err)
	opaque := m5Handler(t, f, scopedPolicy{mode: "owner"}, audit.Postgres{}, true)
	if w := m5Call(opaque, base+"/query", token, aggregateQuery("count", "")); w.Code != 403 {
		t.Fatal("opaque policy inherited SQL aggregate proof", w.Code)
	}
	legacy := aggregateQuery("count", "")
	legacy.Aggregate.Execution = ""
	if got := aggregateResult(t, opaque, base, token, legacy); got.Count != 1 {
		t.Fatal("legacy opaque semantics changed")
	}
}

func aggregateBulkSeed(t *testing.T, f *iamFixture, h http.Handler, base, token, app string, count int) data.Scope {
	t.Helper()
	s := scalarScope(t, h, base, token)
	_, err := f.operator.Exec(`INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES($1,$2,2,$3) ON CONFLICT DO NOTHING`, f.tenantA, app, s.Hash())
	scalarSQL(t, err)
	_, err = f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id)
 SELECT $1,$2,2,$3,'entity.contact',md5(i::text)::uuid,jsonb_build_object('field.contact.name',lpad(i::text,8,'0')||repeat(' aggregate reference text',35),
 'field.contact.balance',jsonb_build_object('amount',(i%97)::text,'currency','USD'),'field.contact.units',i,
 'field.contact.ratio',CASE WHEN i%17=0 THEN NULL ELSE (i%5)::text||'.000000000000000001' END,
 'field.contact.status',CASE WHEN i%7=0 THEN 'inactive' ELSE 'active' END),$4,$4,CASE WHEN i%2=0 THEN $5::uuid ELSE $4::uuid END
 FROM generate_series(1,$6::int)i`, f.tenantA, app, s.Hash(), f.alice, f.viewer, count)
	scalarSQL(t, err)
	return s
}

func TestM9RSQLAggregateBudgetBoundaryAndFilteredInput(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	aggregateBulkSeed(t, f, h, base, token, app, data.MaxSQLAggregateRows)
	q := aggregateQuery("count", "")
	if got := aggregateResult(t, h, base, token, q); got.Count != data.MaxSQLAggregateRows {
		t.Fatal("exact aggregate input boundary")
	}
	v := m5Values("Boundary row")
	v[aggregateUnits], v[aggregateRatio] = json.Number("1"), "1"
	if w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: v}); w.Code != 200 {
		t.Fatal("boundary insertion", w.Code)
	}
	for _, field := range []string{"", aggregateRatio} {
		q = aggregateQuery("count", "")
		if field != "" {
			q = aggregateQuery("sum", field)
		}
		if w := m5Call(h, base+"/query", token, q); w.Code != 400 || strings.Contains(w.Body.String(), `"aggregate"`) {
			t.Fatal("over-budget aggregate returned partial output", w.Code)
		}
	}
	q.Filter = &data.Filter{Op: "eq", Field: aggregateUnits, Value: json.RawMessage(`1`)}
	if got := aggregateResult(t, h, base, token, q); got.Count != 2 || got.Sum != "2.000000000000000001" {
		t.Fatal("aggregate cap preceded authorized filter")
	}
}

func TestM9RSQLAggregateLiveScopesFieldInferenceAndRevocation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	s := aggregateBulkSeed(t, f, h, base, token, app, 360)
	tx, err := f.operator.Begin()
	scalarSQL(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA)
	scalarSQL(t, err)
	parent, child := uuid(t), uuid(t)
	_, err = tx.Exec(`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'Aggregate parent')`, f.tenantA, parent)
	scalarSQL(t, err)
	_, err = tx.Exec(`INSERT INTO iam.teams(tenant_id,id,parent_id,name) VALUES($1,$2,$3,'Aggregate child')`, f.tenantA, child, parent)
	scalarSQL(t, err)
	_, err = tx.Exec(`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id) VALUES($1,$2,$3)`, f.tenantA, parent, f.viewer)
	scalarSQL(t, err)
	_, err = tx.Exec(`UPDATE runtime.records SET owner_team_id=$3,row_version=row_version+1 WHERE tenant_id=$1 AND app_id=$2 AND (custom_data->>'field.contact.units')::int%3=0`, f.tenantA, app, child)
	scalarSQL(t, err)
	_, err = tx.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id)
 SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$3 FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND (custom_data->>'field.contact.units')::int%5=0`, f.tenantA, app, f.viewer)
	scalarSQL(t, err)
	scalarSQL(t, tx.Commit())
	h = r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	viewer := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}, "tenant_id": f.tenantB})
	version := int64(0)
	condition := policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"inactive"`)}}
	for _, tc := range []struct {
		scope policy.DataScope
		allow func(int) bool
	}{
		{policy.DataScope{Kind: "OWNER"}, func(i int) bool { return i%2 == 0 }},
		{policy.DataScope{Kind: "TEAM"}, func(i int) bool { return i%3 == 0 }},
		{policy.DataScope{Kind: "SHARE"}, func(i int) bool { return i%5 == 0 }},
		{condition, func(i int) bool { return i%7 == 0 }},
		{policy.DataScope{Kind: "OR", Children: []policy.DataScope{{Kind: "OWNER"}, {Kind: "SHARE"}}}, func(i int) bool { return i%2 == 0 || i%5 == 0 }},
		{policy.DataScope{Kind: "AND", Children: []policy.DataScope{{Kind: "TEAM"}, condition}}, func(i int) bool { return i%3 == 0 && i%7 == 0 }},
	} {
		v := r5Publish(t, h, root+"/roles", token, version, r5Definition("finance_manager", app, tc.scope), true, uuid(t))
		version = v.Version
		if version == 1 {
			r5Assign(t, h, root, token, f.viewer, "finance_manager", 200)
		}
		count, sum := 0, int64(0)
		for i := 1; i <= 360; i++ {
			if tc.allow(i) {
				count++
				sum += int64(i % 97)
			}
		}
		if got := aggregateResult(t, h, base, viewer, aggregateQuery("count", "")); got.Count != count {
			t.Fatal("aggregate bypassed entity scope", tc.scope.Kind)
		}
		if got := aggregateResult(t, h, base, viewer, aggregateQuery("sum", aggregateBalance)); got.Count != count || got.Sum != fmt.Sprint(sum) {
			t.Fatal("sum bypassed field/entity scope", tc.scope.Kind)
		}
	}
	d := r5Definition("finance_manager", app, policy.DataScope{Kind: "ALL"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: aggregateBalance}})
	version = r5Publish(t, h, root+"/roles", token, version, d, true, uuid(t)).Version
	if got := aggregateResult(t, h, base, viewer, aggregateQuery("count", "")); got.Count != 360 {
		t.Fatal("count narrowed to readable projection")
	}
	if w := m5Call(h, base+"/query", viewer, aggregateQuery("sum", aggregateBalance)); w.Code != 403 {
		t.Fatal("hidden aggregate field leaked", w.Code)
	}
	for _, value := range []string{`{"amount":"1","currency":"USD"}`, `{"amount":"999999","currency":"USD"}`, `{"amount":false,"currency":"USD"}`} {
		q := aggregateQuery("count", "")
		q.Filter = &data.Filter{Op: "eq", Field: aggregateBalance, Value: json.RawMessage(value)}
		if w := m5Call(h, base+"/query", viewer, q); w.Code != 403 {
			t.Fatal("field predicate disclosed hidden value/validation", w.Code)
		}
	}
	d = r5Definition("finance_manager", app, policy.DataScope{Kind: "ALL"})
	d.Grants[1].Actions = []string{"data.entity.read"}
	owner := policy.DataScope{Kind: "OWNER"}
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.field.read"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}, Scope: &owner})
	version = r5Publish(t, h, root+"/roles", token, version, d, true, uuid(t)).Version
	if got := aggregateResult(t, h, base, viewer, aggregateQuery("count", "")); got.Count != 360 {
		t.Fatal("partial field scope narrowed count")
	}
	if w := m5Call(h, base+"/query", viewer, aggregateQuery("sum", aggregateBalance)); w.Code != 403 {
		t.Fatal("partial field scope leaked aggregate", w.Code)
	}
	d = r5Definition("finance_manager", app, policy.DataScope{Kind: "ALL"})
	version = r5Publish(t, h, root+"/roles", token, version, d, true, uuid(t)).Version
	if got := aggregateResult(t, h, base, viewer, aggregateQuery("count", "")); got.Count != 360 {
		t.Fatal("live role refresh missing")
	}
	raw, _ := json.Marshal(s.Program().IR().Application)
	bob := f.token(t, f.bobSubject, nil)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantB+"/metadata/"+app+"/versions", bob, 0, raw); w.Code != 201 {
		t.Fatal("populated foreign metadata", w.Code)
	}
	other := strings.Replace(strings.Replace(base, f.tenantA, f.tenantB, 1), "/versions/2/", "/versions/1/", 1)
	m5Create(t, h, other, bob, "Other tenant record")
	if got := aggregateResult(t, h, other, bob, aggregateQuery("count", "")); got.Count != 1 {
		t.Fatal("tenant-specific aggregate")
	}
	if w := m5Call(h, other+"/query", viewer, aggregateQuery("count", "")); w.Code != 403 {
		t.Fatal("cross-tenant aggregate admitted", w.Code)
	}
	r5Publish(t, h, root+"/roles", token, version, d, false, uuid(t))
	if w := m5Call(h, base+"/query", viewer, aggregateQuery("count", "")); w.Code != 403 {
		t.Fatal("inactive role aggregate admitted", w.Code)
	}
}

func TestM9RSQLAggregateRetainedGenerationUsesCurrentOwnersAndTombstones(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	r := m5Create(t, h, base, token, "Retained aggregate input")
	m5Create(t, h, base, token, "Other aggregate input")
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	_, path := shadowAdopt(t, h, token, root)
	old := path + "/bindings/1/entities/entity.contact"
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	witness := shadowSource(t, h, token, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", true) })
	intent := compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.contact", FieldID: "field.contact.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`7`)}}}
	x := shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, witness, intent))
	shadowStep(t, h, token, path, x, "activate")
	current := path + "/bindings/2/entities/entity.contact"
	q := data.Query{Projection: []string{"field.contact.id", aggregateBalance}, Limit: 100, Aggregate: &data.Aggregate{Op: "sum", Field: aggregateBalance, Execution: data.SQLAggregateV1}}
	for _, b := range []string{old, current} {
		if got := aggregateResult(t, h, b, token, q); got.Count != 2 || got.Sum != "24.6" {
			t.Fatal("generation aggregate lost records")
		}
	}
	h = r5Handler(t, f, audit.Postgres{})
	roleRoot := "/v1/tenants/" + f.tenantA
	r5Publish(t, h, roleRoot+"/roles", token, 0, r5Definition("finance_manager", app, policy.DataScope{Kind: "OWNER"}), true, uuid(t))
	r5Assign(t, h, roleRoot, token, f.viewer, "finance_manager", 200)
	viewer := f.token(t, f.viewerSubject, nil)
	if got := aggregateResult(t, h, old, viewer, q); got.Count != 0 {
		t.Fatal("retained owner fixture invalid")
	}
	if w := m5Call(h, current+"/records/"+r.ID+"/access", token, map[string]any{"operation": "owner", "expected_version": 1, "user_id": f.viewer}); w.Code != 200 {
		t.Fatal("current canonical owner assignment", w.Code)
	}
	for _, b := range []string{old, current} {
		if got := aggregateResult(t, h, b, viewer, q); got.Count != 1 || got.Sum != "12.3" {
			t.Fatal("aggregate used stale physical ownership")
		}
	}
	if w := m5Call(h, current+"/commands", token, data.Command{Operation: "delete", ID: r.ID, ExpectedVersion: 2}); w.Code != 200 {
		t.Fatal("current canonical deletion", w.Code)
	}
	for _, b := range []string{old, current} {
		if got := aggregateResult(t, h, b, token, q); got.Count != 1 || got.Sum != "12.3" {
			t.Fatal("retained aggregate included tombstoned record")
		}
		if got := aggregateResult(t, h, b, viewer, q); got.Count != 0 || got.Sum != "0" {
			t.Fatal("retained scoped aggregate included tombstoned record")
		}
	}
	var rows int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, f.tenantA, app, r.ID).Scan(&rows))
	if rows != 1 {
		t.Fatal("retained historical record unexpectedly deleted")
	}
}

type aggregateLateAuditFault struct{ fired bool }

func (f *aggregateLateAuditFault) Append(ctx context.Context, q postgres.Executor, e audit.Entry) error {
	if err := (audit.Postgres{}).Append(ctx, q, e); err != nil {
		return err
	}
	if e.Action == "data.aggregate.read" {
		f.fired = true
		_, err := q.ExecContext(ctx, `SELECT 1/0`)
		return err
	}
	return nil
}

func TestM9RSQLAggregateLateAuditAbortSuppressesResult(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	aggregateSeed(t, f, h, base, token, app, 2)
	aggregateResult(t, h, base, token, aggregateQuery("sum", aggregateBalance))
	var before, after int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='data.aggregate.read'`, f.tenantA).Scan(&before))
	fault := &aggregateLateAuditFault{}
	failing := r5Handler(t, f, fault)
	w := m5Call(failing, base+"/query", token, aggregateQuery("sum", aggregateBalance))
	if !fault.fired || w.Code != 503 || strings.Contains(w.Body.String(), `"aggregate"`) || strings.Contains(w.Body.String(), `"sum"`) {
		t.Fatal("late SQL audit fault exposed an uncommitted result", w.Code)
	}
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND action='data.aggregate.read'`, f.tenantA).Scan(&after))
	if after != before {
		t.Fatal("failed aggregate audit transaction partially committed")
	}
	if got := aggregateResult(t, h, base, token, aggregateQuery("count", "")); got.Count != 2 {
		t.Fatal("aggregate fault changed business records")
	}
}

func TestM9RSQLAggregateRetainedBinaryKeepsOldContract(t *testing.T) {
	if os.Getenv("M9R_LEGACY_API_BINARY") == "" {
		t.Skip("checksum-pinned legacy API artifact is opt-in")
	}
	m8Database(t)
	f, h, base, token, app := aggregateFixture(t)
	aggregateSeed(t, f, h, base, token, app, 2)
	old := uniqueReviewLegacy(t, f)
	q := aggregateQuery("sum", aggregateUnits)
	q.Aggregate.Execution = ""
	if got := aggregateResult(t, old, base, token, q); got.Count != 2 || got.Sum != "18014398509481982" {
		t.Fatal("retained exact aggregate contract changed")
	}
	q.Aggregate.Execution = data.SQLAggregateV1
	if w := m5Call(old, base+"/query", token, q); w.Code != 400 {
		t.Fatal("retained binary silently accepted new mode", w.Code)
	}
	if got := aggregateResult(t, h, base, token, q); got.Count != 2 || got.Sum != "18014398509481982" {
		t.Fatal("new explicit aggregate disagrees with retained reader")
	}
}
