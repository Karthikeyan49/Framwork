//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func shadowSource(t *testing.T, h http.Handler, token, tenant, app string, revision int64, raw []byte, mutate func(*metadata.Document)) map[string]any {
	t.Helper()
	var d metadata.Document
	if json.Unmarshal(raw, &d) != nil {
		t.Fatal("source fixture")
	}
	d.AppID = app
	mutate(&d)
	body, _ := json.Marshal(d)
	w := m3Call(h, "POST", "/v1/tenants/"+tenant+"/metadata/"+app+"/versions", token, revision-1, body)
	if w.Code != 201 {
		t.Fatal("target registry", w.Code, w.Body.String())
	}
	return m9rDatasetWitness(t, h, fmt.Sprintf("/v1/tenants/%s/preview/%s/versions/%d", tenant, app, revision), token)
}
func shadowPrepare(t *testing.T, h http.Handler, token, path string, base int64, witness map[string]any, intent compiler.EvolutionIntent) data.Evolution {
	t.Helper()
	body := map[string]any{}
	for k, v := range witness {
		body[k] = v
	}
	body["operation_id"], body["expected_binding"], body["intent"] = uuid(t), base, intent
	r := m5Call(h, path+"/evolutions", token, body)
	if r.Code != 200 {
		t.Fatal("prepare", r.Code, r.Body.String())
	}
	var x data.Evolution
	if json.Unmarshal(r.Body.Bytes(), &x) != nil {
		t.Fatal("progress")
	}
	return x
}
func shadowStep(t *testing.T, h http.Handler, token, path string, x data.Evolution, action string) data.Evolution {
	t.Helper()
	r := m5Call(h, path+"/evolutions/"+x.ID+"/"+action, token, map[string]int64{"expected_checkpoint": x.Checkpoint})
	if r.Code != 200 {
		t.Fatal(action, r.Code, r.Body.String())
	}
	var result data.Evolution
	if json.Unmarshal(r.Body.Bytes(), &result) != nil {
		t.Fatal("progress")
	}
	return result
}
func shadowReady(t *testing.T, h http.Handler, token, path string, x data.Evolution) data.Evolution {
	t.Helper()
	for i := 0; i < 50 && x.State != "READY"; i++ {
		x = shadowStep(t, h, token, path, x, "advance")
	}
	if x.State != "READY" || x.SourceManifest != x.TargetManifest {
		t.Fatal("not independently verified", x)
	}
	return x
}
func shadowAdopt(t *testing.T, h http.Handler, token, root string) (data.Dataset, string) {
	t.Helper()
	witness := m9rDatasetWitness(t, h, root+"/versions/1", token)
	d := m9rDatasetCreate(t, h, root+"/datasets", token, "ADOPT", witness)
	path := root + "/datasets/" + d.ID
	for i := 0; i < 50 && d.State != "OPEN"; i++ {
		r := m5Call(h, path+"/verify", token, struct{}{})
		if r.Code != 200 {
			t.Fatal("adopt", r.Code, r.Body.String())
		}
		_ = json.Unmarshal(r.Body.Bytes(), &d)
	}
	if d.State != "OPEN" {
		t.Fatal("adoption sealed")
	}
	return d, path
}
func addCount(d *metadata.Document, typ string, required bool) {
	for i := range d.Definitions {
		if d.Definitions[i].Entity != nil {
			d.Definitions[i].Entity.Fields = append(d.Definitions[i].Entity.Fields, "field.contact.count")
		}
	}
	d.Definitions = append(d.Definitions, metadata.Definition{ID: "field.contact.count", Kind: "field", Label: "Count", SchemaVersion: d.SchemaVersion, Namespace: d.Namespace, Field: &metadata.Field{EntityRef: "entity.contact", Type: typ, Required: required}})
}

func TestM9RShadowPopulatedResumeCutoverLineageAndTombstone(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	parent := m5Create(t, h, base, token, "Parent")
	childValues := m5Values("Child")
	childValues["field.contact.parent"] = parent.ID
	r := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: childValues})
	if r.Code != 200 {
		t.Fatal(r.Code)
	}
	var child data.Record
	_ = json.Unmarshal(r.Body.Bytes(), &child)
	cyclic := m5Values("Parent")
	cyclic["field.contact.parent"] = child.ID
	r = m5Call(h, base+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 1, Values: cyclic})
	if r.Code != 200 {
		t.Fatal("cycle fixture", r.Code)
	}
	witness := m9rDatasetWitness(t, h, strings.Split(base, "/entities/")[0], token)
	payload, _ := json.Marshal(m5Values("Bulk"))
	if _, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,owner_user_id,updated_by) SELECT $1,$2,1,$3,'entity.contact',gen_random_uuid(),$4::jsonb,$5,$5,$5 FROM generate_series(1,105)`, f.tenantA, app, witness["ir_hash"], string(payload), f.alice); err != nil {
		t.Fatal(err)
	}
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	_, path := shadowAdopt(t, h, token, root)
	bound := path + "/bindings/1/entities/entity.contact"
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	raw = []byte(strings.ReplaceAll(string(raw), "field.contact.name", "field.contact.title"))
	target := shadowSource(t, h, token, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", true) })
	intent := compiler.EvolutionIntent{Renames: []compiler.FieldRename{{EntityID: "entity.contact", From: "field.contact.name", To: "field.contact.title"}}, Backfills: []compiler.FieldBackfill{{EntityID: "entity.contact", FieldID: "field.contact.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`7`)}}}
	x := shadowPrepare(t, h, token, path, 1, target, intent)
	if r = m5Call(h, bound+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 2, Values: cyclic}); r.Code != 409 {
		t.Fatal("frozen writer", r.Code)
	}
	first := shadowStep(t, h, token, path, x, "advance")
	if first.State != "COPYING" || first.CopiedRows <= 0 || first.CopiedRows > 100 {
		t.Fatal("not bounded", first)
	}
	if r = m5Call(h, path+"/evolutions/"+x.ID+"/advance", token, map[string]int64{"expected_checkpoint": x.Checkpoint}); r.Code != 409 {
		t.Fatal("stale checkpoint", r.Code)
	}
	r = m3Call(h, "GET", path+"/evolutions/"+x.ID, token, 0, nil)
	var resumed data.Evolution
	_ = json.Unmarshal(r.Body.Bytes(), &resumed)
	if r.Code != 200 || resumed.Checkpoint != first.Checkpoint {
		t.Fatal("resume witness")
	}
	ready := shadowReady(t, h, token, path, resumed)
	if ready.CopiedRows != 107 || ready.VerifiedRows != 107 {
		t.Fatal("incomplete coverage", ready)
	}
	x = shadowStep(t, h, token, path, ready, "activate")
	if x.State != "ACTIVATED" {
		t.Fatal("activation")
	}
	again := shadowStep(t, h, token, path, ready, "activate")
	if again.Checkpoint != x.Checkpoint {
		t.Fatal("activation retry changed evidence")
	}
	newBase := path + "/bindings/2/entities/entity.contact"
	query := data.Query{RecordID: parent.ID, Projection: []string{"field.contact.title", "field.contact.count", "field.contact.parent"}, Limit: 1}
	page := m5Page(t, m5Call(h, newBase+"/query", token, query))
	if len(page.Records) != 1 || page.Records[0].Version != 2 || page.Records[0].Values["field.contact.title"] != "Parent" || fmt.Sprint(page.Records[0].Values["field.contact.count"]) != "7" {
		t.Fatal("transform lost values", page)
	}
	old := m5Page(t, m5Call(h, bound+"/query", token, data.Query{RecordID: parent.ID, Projection: []string{"field.contact.name"}, Limit: 1}))
	if len(old.Records) != 1 || old.Records[0].Values["field.contact.name"] != "Parent" {
		t.Fatal("old reader recompiled or incompatible")
	}
	if r = m5Call(h, bound+"/commands", token, data.Command{Operation: "delete", ID: child.ID, ExpectedVersion: 1}); r.Code != 409 {
		t.Fatal("old generation reopened", r.Code)
	}
	targetValues := m5Values("Changed")
	delete(targetValues, "field.contact.name")
	targetValues["field.contact.title"], targetValues["field.contact.count"] = "Changed", json.Number("9")
	r = m5Call(h, newBase+"/commands", token, data.Command{Operation: "update", ID: parent.ID, ExpectedVersion: 2, Values: targetValues, Key: uuid(t)})
	if r.Code != 200 {
		t.Fatal("active update", r.Code, r.Body.String())
	}
	var count, sourceGeneration int
	var sequence int64
	if err := f.operator.QueryRow(`SELECT count(*),max(sequence) FROM control.event_deliveries WHERE tenant_id=$1 AND aggregate_key=jsonb_build_array('m8-v1',$2::text,1,$3::text,'entity.contact',$4::uuid)::text`, f.tenantA, app, witness["ir_hash"], parent.ID).Scan(&count, &sequence); err != nil || count != 3 || sequence != 3 {
		t.Fatal("causal lane changed", count, sequence, err)
	}
	if err := f.operator.QueryRow(`SELECT max(generation) FROM runtime.event_data_sources WHERE tenant_id=$1 AND app_id=$2`, f.tenantA, app).Scan(&sourceGeneration); err != nil || sourceGeneration != 2 {
		t.Fatal("event physical provenance", err)
	}
	// Cyclic references were rebuilt after all destination rows existed.
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.record_links l JOIN runtime.dataset_generations g ON g.tenant_id=l.tenant_id AND g.app_id=l.app_id AND g.anchor_revision=l.metadata_revision AND g.anchor_hash=l.ir_hash WHERE l.tenant_id=$1 AND l.app_id=$2 AND g.generation=2`, f.tenantA, app).Scan(&count); err != nil || count != 1 {
		t.Fatal("reference rebuild/update", count, err)
	}
	// Delete is a current logical tombstone; historical preview and bindings hide it.
	values := m5Values("Child")
	delete(values, "field.contact.name")
	values["field.contact.title"], values["field.contact.count"] = "Child", json.Number("7")
	delete(values, "field.contact.parent")
	if r = m5Call(h, newBase+"/commands", token, data.Command{Operation: "update", ID: child.ID, ExpectedVersion: 1, Values: values}); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = m5Call(h, newBase+"/commands", token, data.Command{Operation: "delete", ID: parent.ID, ExpectedVersion: 3}); r.Code != 200 {
		t.Fatal("delete", r.Code, r.Body.String())
	}
	for _, b := range []string{bound, base} {
		page = m5Page(t, m5Call(h, b+"/query", token, data.Query{RecordID: parent.ID, Projection: []string{"field.contact.name"}, Limit: 1}))
		if len(page.Records) != 0 {
			t.Fatal("retired generation resurrected tombstone")
		}
	}
	if r = m5Call(h, path+"/evolutions/"+x.ID+"/abort", token, map[string]int64{"expected_checkpoint": x.Checkpoint}); r.Code != 409 {
		t.Fatal("unsafe post-cutover rollback", r.Code)
	}
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=1 AND ir_hash=$3`, f.tenantA, app, witness["ir_hash"]).Scan(&count); err != nil || count != 107 {
		t.Fatal("source data deleted", err)
	}
}

func TestM9RShadowLivePermissionEpochAndAbort(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "Owned")
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	d, path := shadowAdopt(t, h, token, root)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	target := shadowSource(t, h, token, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, target, compiler.EvolutionIntent{}))
	tx, err := f.operator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`UPDATE runtime.records SET owner_user_id=$1 WHERE tenant_id=$2 AND app_id=$3 AND id=$4`, f.viewer, f.tenantA, app, record.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if r := m5Call(h, path+"/evolutions/"+ready.ID+"/activate", token, map[string]int64{"expected_checkpoint": ready.Checkpoint}); r.Code != 409 {
		t.Fatal("ACL changed after verification", r.Code)
	}
	x := shadowStep(t, h, token, path, ready, "reverify")
	x = shadowReady(t, h, token, path, x)
	x = shadowStep(t, h, token, path, x, "activate")
	for _, binding := range []string{"1", "2"} {
		page := m5Page(t, m5Call(h, path+"/bindings/"+binding+"/entities/entity.contact/query", token, data.Query{RecordID: record.ID, Projection: []string{"field.contact.name"}, Limit: 1}))
		if len(page.Records) != 1 || page.Records[0].OwnerID != f.viewer {
			t.Fatal("stale owner", page)
		}
	}
	var canonical bool
	if err := f.operator.QueryRow(`SELECT canonical_access FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, d.ID).Scan(&canonical); err != nil || !canonical {
		t.Fatal("canonical identity missing")
	}
	// A second migration can be aborted before activation; current writes reopen,
	// and the retained, unused generation cannot be accessed as legacy scratch.
	next := shadowSource(t, h, token, f.tenantA, app, 3, raw, func(d *metadata.Document) { addCount(d, "decimal", false) })
	x = shadowPrepare(t, h, token, path, 2, next, compiler.EvolutionIntent{})
	x = shadowStep(t, h, token, path, x, "advance")
	x = shadowStep(t, h, token, path, x, "abort")
	if x.State != "ABORTED" {
		t.Fatal("abort")
	}
	r := m5Call(h, path+"/bindings/2/entities/entity.contact/commands", token, data.Command{Operation: "update", ID: record.ID, ExpectedVersion: 1, Values: m5Values("Recovered")})
	if r.Code != 200 {
		t.Fatal("pre-cutover reopen", r.Code, r.Body.String())
	}
}

func TestM9RShadowWorkflowPinsSurviveActivation(t *testing.T) {
	m := m9Setup(t, nil)
	root := "/v1/tenants/" + m.f.tenantA + "/preview/" + m.app
	_, path := shadowAdopt(t, m.handler, m.token, root)
	bound := path + "/bindings/1/workflows/workflow.contact.review"
	m.base = bound
	r := m5Call(m.handler, bound+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "pinned-generation-one"})
	if r.Code != 200 {
		t.Fatal("bound start", r.Code, r.Body.String())
	}
	var x workflow.Instance
	_ = json.Unmarshal(r.Body.Bytes(), &x)
	if x.Pin.DataGeneration != 1 || x.Pin.DatasetID == "" || x.Pin.DataHash != x.Pin.IRHash {
		t.Fatal("immutable data pin", x.Pin)
	} // ADOPT keeps the original physical hash.
	target := shadowSource(t, m.handler, m.token, m.f.tenantA, m.app, 2, m.raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, m.handler, m.token, path, shadowPrepare(t, m.handler, m.token, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, m.handler, m.token, path, ready, "activate")
	// Restore the original instance after cutover; no current-generation fallback.
	r = m3Call(m.handler, "GET", bound+"/instances/"+x.ID, m.token, 0, nil)
	if r.Code != 200 {
		t.Fatal("pinned GET", r.Code, r.Body.String())
	}
	var stored workflow.Instance
	_ = json.Unmarshal(r.Body.Bytes(), &stored)
	if stored.Pin.DataHash != x.Pin.DataHash || stored.Pin.DataGeneration != 1 {
		t.Fatal("pin replaced")
	}
	m.until(t, x.ID, "WAITING_APPROVAL")
	var state string
	if err := m.f.operator.QueryRow(`SELECT state_id FROM runtime.workflow_instances WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID).Scan(&state); err != nil || state != "state.approval" {
		t.Fatal("worker could not read pinned source", state, err)
	}
	// A workflow created on the new binding pins the new physical generation.
	newBound := path + "/bindings/2/workflows/workflow.contact.review"
	r = m5Call(m.handler, newBound+"/start", m.token, map[string]string{"record_id": m.record.ID, "idempotency_key": "pinned-generation-two"})
	if r.Code != 200 {
		t.Fatal("new start", r.Code, r.Body.String())
	}
	_ = json.Unmarshal(r.Body.Bytes(), &stored)
	if stored.Pin.DataGeneration != 2 || stored.Pin.DataHash == x.Pin.DataHash {
		t.Fatal("wrong target pin")
	}
	m.base = newBound
	m.until(t, stored.ID, "WAITING_APPROVAL")
}

func TestM9RShadowConcurrentDistinctDeletesDoNotUpgradeDatasetGate(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	one := m5Create(t, h, base, token, "One")
	two := m5Create(t, h, base, token, "Two")
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	d, path := shadowAdopt(t, h, token, root)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	target := shadowSource(t, h, token, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	x := shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, h, token, path, x, "activate")
	ready := make(chan struct{}, 2)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{one.ID, two.ID} {
		go func(id string) {
			results <- postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
				if _, err := q.ExecContext(ctx, `SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA); err != nil {
					return err
				}
				principal, err := f.verifier.Verify(token)
				if err != nil {
					return err
				}
				user, err := (identity.Store{}).Resolve(ctx, q, principal)
				if err != nil {
					return err
				}
				if err = (tenant.Store{}).BindActor(ctx, q, user); err != nil {
					return err
				}
				a, err := (tenant.Store{}).Resolve(ctx, q, user, f.tenantA)
				if err != nil {
					return err
				}
				scope, err := (data.DatasetStore{}).Scope(ctx, q, a, app, d.ID, 2)
				if err != nil {
					return err
				}
				if err = data.AdmitStart(ctx, q, scope, a); err != nil {
					return err
				}
				ready <- struct{}{}
				<-start
				_, err = q.ExecContext(ctx, `DELETE FROM runtime.records WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND entity_id='entity.contact' AND id=$5`, f.tenantA, app, scope.DataRevision(), scope.DataHash(), id)
				return err
			})
		}(id)
	}
	for range 2 {
		select {
		case <-ready:
		case err := <-results:
			close(start)
			t.Fatal("delete admission failed", err)
		}
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal("distinct delete failed", err)
		}
	}
	var tombstones int
	if err := f.operator.QueryRow(`SELECT count(*) FROM runtime.record_identity WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=$3 AND tombstone`, f.tenantA, app, d.ID).Scan(&tombstones); err != nil || tombstones != 2 {
		t.Fatal("tombstone concurrency", err)
	}
}

func TestM9RShadowRejectsUnexpectedRawTargetAndMissingOperation(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	m5Create(t, h, base, token, "Source")
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	d, path := shadowAdopt(t, h, token, root)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	target := shadowSource(t, h, token, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	x := shadowPrepare(t, h, token, path, 1, target, compiler.EvolutionIntent{})
	x = shadowStep(t, h, token, path, x, "advance")
	if x.State != "LINKING" {
		t.Fatal(x)
	}
	// This SQL port must reject absent operations without HTTP rollback helping it.
	err := postgres.WithRuntime(context.Background(), f.db, func(ctx context.Context, q postgres.Executor) error {
		p, _ := f.verifier.Verify(token)
		if _, err := m3Scope(f, ctx, q, p, f.tenantA); err != nil {
			return err
		}
		_, err := q.ExecContext(ctx, `SELECT runtime.finish_evolution($1,$2,$3,$4,1,'ABORT')`, f.tenantA, app, d.ID, uuid(t))
		return err
	})
	if err == nil {
		t.Fatal("missing operation reopened dataset")
	}
	var state string
	if err := f.operator.QueryRow(`SELECT state FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, d.ID).Scan(&state); err != nil || state != "FROZEN" {
		t.Fatal("invalid ABORT changed source", err)
	}
	if _, err := f.operator.Exec(`INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data,created_by,updated_by,owner_user_id) SELECT $1,$2,anchor_revision,anchor_hash,'entity.contact',gen_random_uuid(),$4::jsonb,$5,$5,$5 FROM runtime.dataset_generations WHERE tenant_id=$1 AND dataset_id=$3 AND generation=2`, f.tenantA, app, d.ID, `{"field.contact.name":"Hidden"}`, f.alice); err != nil {
		t.Fatal(err)
	}
	x = shadowStep(t, h, token, path, x, "advance")
	r := m5Call(h, path+"/evolutions/"+x.ID+"/advance", token, map[string]int64{"expected_checkpoint": x.Checkpoint})
	if r.Code != 409 {
		t.Fatal("hidden raw destination escaped verification", r.Code, r.Body.String())
	}
	var checkpoint int64
	if err := f.operator.QueryRow(`SELECT checkpoint FROM runtime.dataset_evolutions WHERE tenant_id=$1 AND operation_id=$2`, f.tenantA, x.ID).Scan(&checkpoint); err != nil || checkpoint != x.Checkpoint {
		t.Fatal("failed verification committed", err)
	}
	if _, err := f.operator.Exec(`UPDATE runtime.dataset_evolutions SET plan_body=plan_body,checkpoint=checkpoint+1,base_binding=base_binding+1 WHERE tenant_id=$1 AND operation_id=$2`, f.tenantA, x.ID); err == nil {
		t.Fatal("evolution witness mutable")
	}
	shadowStep(t, h, token, path, x, "abort")
}

func TestM9RShadowConversionShareRevocationAndAuditRollback(t *testing.T) {
	m8Database(t)
	f, h, base, token, app := m5Fixture(t)
	record := m5Create(t, h, base, token, "Source")
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	d, path := shadowAdopt(t, h, token, root)
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	target := shadowSource(t, h, token, f.tenantA, app, 2, raw, func(d *metadata.Document) { addCount(d, "integer", true) })
	intent := compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.contact", FieldID: "field.contact.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`9`)}}}
	x := shadowPrepare(t, h, token, path, 1, target, intent)
	bad := m5Handler(t, f, policy.Native{}, m8AfterAuditFailure{}, true)
	r := m5Call(bad, path+"/evolutions/"+x.ID+"/advance", token, map[string]int64{"expected_checkpoint": x.Checkpoint})
	if r.Code != 503 {
		t.Fatal("audit failure accepted", r.Code)
	}
	var checkpoint, rows int64
	if err := f.operator.QueryRow(`SELECT checkpoint,copied_rows FROM runtime.dataset_evolutions WHERE tenant_id=$1 AND operation_id=$2`, f.tenantA, x.ID).Scan(&checkpoint, &rows); err != nil || checkpoint != x.Checkpoint || rows != 0 {
		t.Fatal("copy not atomic with audit", err)
	}
	ready := shadowReady(t, h, token, path, x)
	shadowStep(t, h, token, path, ready, "activate")
	// Shares are current logical facts for both immutable generations.
	if _, err := f.operator.Exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id,can_write) SELECT $1,$2,anchor_revision,anchor_hash,'entity.contact',$4,$5,true FROM runtime.dataset_generations WHERE tenant_id=$1 AND dataset_id=$3 AND generation=2`, f.tenantA, app, d.ID, record.ID, f.viewer); err != nil {
		t.Fatal(err)
	}
	shareHandler := m5Handler(t, f, scopedPolicy{mode: "share"}, audit.Postgres{}, true)
	viewer := f.token(t, f.viewerSubject, nil)
	for _, binding := range []string{"1", "2"} {
		page := m5Page(t, m5Call(shareHandler, path+"/bindings/"+binding+"/entities/entity.contact/query", viewer, data.Query{RecordID: record.ID, Projection: []string{"field.contact.name"}, Limit: 1}))
		if len(page.Records) != 1 {
			t.Fatal("live share missing", binding)
		}
	}
	if _, err := f.operator.Exec(`DELETE FROM runtime.record_shares WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=1 AND ir_hash=(SELECT anchor_hash FROM runtime.dataset_generations WHERE tenant_id=$1 AND dataset_id=$3 AND generation=2) AND id=$4 AND user_id=$5`, f.tenantA, app, d.ID, record.ID, f.viewer); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []string{"1", "2"} {
		page := m5Page(t, m5Call(shareHandler, path+"/bindings/"+binding+"/entities/entity.contact/query", viewer, data.Query{RecordID: record.ID, Projection: []string{"field.contact.name"}, Limit: 1}))
		if len(page.Records) != 0 {
			t.Fatal("retired generation retained revoked share", binding)
		}
	}
	next := shadowSource(t, h, token, f.tenantA, app, 3, raw, func(d *metadata.Document) { addCount(d, "decimal", true) })
	ready = shadowReady(t, h, token, path, shadowPrepare(t, h, token, path, 2, next, compiler.EvolutionIntent{}))
	shadowStep(t, h, token, path, ready, "activate")
	page := m5Page(t, m5Call(h, path+"/bindings/3/entities/entity.contact/query", token, data.Query{RecordID: record.ID, Projection: []string{"field.contact.count"}, Limit: 1}))
	if len(page.Records) != 1 || page.Records[0].Values["field.contact.count"] != "9" {
		t.Fatal("conversion did not preserve exact integer", page)
	}
	m9rIndexReady(t, h, path+"/bindings/3/entities/entity.contact/indexes/field.contact.count", token)
}
