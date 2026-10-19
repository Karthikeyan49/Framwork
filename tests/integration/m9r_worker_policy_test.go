//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/events"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQL credentials alone must enforce role and record revocation. No Go policy
// resolver participates in these probes; positive controls expose the same job.
func r5RawWorker(t *testing.T, m *m9Fixture, lease jobs.WorkflowLease, allow bool, hash string) {
	t.Helper()
	tx, err := m.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if ok, e := bindM9R(tx, lease); e != nil || !ok {
		t.Fatal("exact identity/lease admission", e)
	}
	var actor, tenantID, issuer, subject string
	if e := tx.QueryRow(`SELECT actor::text,tenant::text,issuer,subject FROM iam.worker_identity($1::uuid)`, lease.Ticket.JobID()).Scan(&actor, &tenantID, &issuer, &subject); e != nil || actor != m.f.alice || tenantID != m.f.tenantA {
		t.Fatal("exact denial identity", e)
	}
	if _, e := tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer',$2,true),set_config('app.auth_subject',$3,true)`, actor, issuer, subject); e != nil {
		t.Fatal(e)
	}
	var got bool
	var version string
	if e := tx.QueryRow(`SELECT allowed,policy_version FROM control.worker_policy_decision()`).Scan(&got, &version); e != nil || got != allow || version != "tenant-role-v1:"+hash {
		t.Fatal("live SQL decision", e, got, version)
	}
	want := 0
	if allow {
		want = 1
	}
	for _, query := range []string{
		`SELECT count(*) FROM runtime.worker_source_record()`,
		`SELECT count(*) FROM runtime.workflow_bindings WHERE id=$1::uuid`,
		`SELECT count(*) FROM runtime.workflow_instances WHERE id=$1::uuid`,
	} {
		var n int
		var e error
		if strings.Contains(query, "$1") {
			e = tx.QueryRow(query, lease.Ticket.JobID()).Scan(&n)
		} else {
			e = tx.QueryRow(query).Scan(&n)
		}
		if e != nil || n != want {
			t.Fatal("raw source/snapshot boundary", e, n, want)
		}
	}
	if allow {
		var fields []byte
		if e := tx.QueryRow(`SELECT custom_data FROM runtime.worker_source_record()`).Scan(&fields); e != nil {
			t.Fatal(e)
		}
		var values map[string]any
		if json.Unmarshal(fields, &values) != nil || len(values) != 1 || values["field.contact.status"] == nil {
			t.Fatal("allowed source lost declared-field masking")
		}
		return
	}
	result, e := tx.Exec(`UPDATE runtime.workflow_instances SET version=version+1 WHERE id=$1::uuid`, lease.Ticket.JobID())
	if e != nil {
		t.Fatal(e)
	}
	if n, e := result.RowsAffected(); e != nil || n != 0 {
		t.Fatal("revoked raw state write", e, n)
	}
	if e = tx.QueryRow(`SELECT control.check_workflow_lease($1::uuid,$2::uuid,$3,$4,$5::uuid)`, tenantID, lease.Ticket.JobID(), lease.Ticket.ReleaseHash(), lease.Generation, lease.Token).Scan(&got); e != nil || got {
		t.Fatal("revoked business lease accepted", e)
	}
	if _, e = tx.Exec(`SAVEPOINT denied_effect`); e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(`INSERT INTO m9_fixture.effects(tenant_id,instance_id,step_key) VALUES($1::uuid,$2::uuid,'raw-denied')`, tenantID, lease.Ticket.JobID())
	var rejected *pgconn.PgError
	if !errors.As(e, &rejected) || rejected.Code != "42501" {
		t.Fatal("raw effect expected RLS rejection")
	}
	if _, e = tx.Exec(`ROLLBACK TO SAVEPOINT denied_effect`); e != nil {
		t.Fatal(e)
	}
	// Denial evidence remains available, without granting an allowed audit or
	// business write. This append is rolled back with the probe transaction.
	if _, e = tx.Exec(`INSERT INTO audit.entries(actor_id,tenant_id,action,resource,allowed,decision_id,policy_version,request_id,trace_id,state) VALUES($1::uuid,$2::uuid,'workflows.execute','workflow',false,'raw-denial',$3,'raw-probe','','{}'::jsonb)`, actor, tenantID, version); e != nil {
		t.Fatal("denial evidence rejected", e)
	}
}

func TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry(t *testing.T) {
	m := m9Setup(t, nil)
	x := m.start(t, "sql-statement-policy")
	h := r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	d := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "ALL"})
	condition := policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}}
	children := make([]policy.DataScope, 8)
	for i := range children {
		children[i] = condition
	}
	scope := policy.DataScope{Kind: "OR", Children: children}
	grant := d.Grants[1]
	grant.Scope = &scope
	d.Grants = append(d.Grants[:1], d.Grants[2:]...)
	for i := 0; i < 7; i++ {
		d.Grants = append(d.Grants, grant)
	}
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	r5Publish(t, h, root+"/roles", m.token, 0, d, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, d.Name, 200)
	// Bounded synthetic scan, not evidence of a full 96-step business lifecycle.
	if _, e := m.f.operator.Exec(`INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) SELECT $1,$2,'scan-'||g,'local.noop',repeat('a',64),convert_to('{}','UTF8'),encode(sha256(convert_to('{}','UTF8')),'hex'),g,'local.noop' FROM generate_series(1,96) g`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	bind := func() *sql.Tx {
		t.Helper()
		tx, e := m.db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		if ok, e := bindM9R(tx, lease); e != nil || !ok {
			tx.Rollback()
			t.Fatal("scan lease", e)
		}
		if _, e = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true),set_config('statement_timeout','2000',true)`, m.f.alice, m.f.aliceSubject); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		return tx
	}
	const query = `SELECT r.step_key FROM runtime.workflow_receipts r WHERE r.tenant_id=$1 AND r.instance_id=$2 AND r.compensation_ref<>'' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts c WHERE c.tenant_id=r.tenant_id AND c.instance_id=r.instance_id AND c.step_key=r.step_key||':compensate') ORDER BY r.sequence DESC,r.step_key DESC LIMIT 1`
	tx := bind()
	start := time.Now()
	var step string
	if e = tx.QueryRow(query, m.f.tenantA, x.ID).Scan(&step); e != nil || step != "scan-96" {
		tx.Rollback()
		t.Fatal("bounded compensation scan", e)
	}
	t.Logf("96 receipts, 7 scopes/63 nodes per capability: %s", time.Since(start))
	var raw []byte
	if e = tx.QueryRow("EXPLAIN (ANALYZE, FORMAT JSON, TIMING OFF) "+query, m.f.tenantA, x.ID).Scan(&raw); e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	var plan any
	if json.Unmarshal(raw, &plan) != nil {
		t.Fatal("query plan")
	}
	decisions := 0
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if value["Function Name"] == "worker_policy_decision" {
				decisions++
				if loops, ok := value["Actual Loops"].(float64); !ok || loops > 1 {
					t.Error("policy definition/IR evaluation still scales with receipt rows")
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(plan)
	if decisions == 0 {
		t.Error("plan did not execute live authorization")
	}
	if e = tx.Rollback(); e != nil {
		t.Fatal(e)
	}
	// A statement-wide decision must not cache lease freshness for a slow write.
	if _, e = m.f.operator.Exec(`UPDATE control.workflow_jobs SET lease_until=clock_timestamp()+interval '500 milliseconds' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	tx = bind()
	defer tx.Rollback()
	result, e := tx.Exec(`UPDATE runtime.workflow_instances SET version=(SELECT version+1 FROM pg_sleep(0.7)) WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID)
	if e != nil {
		var denied *pgconn.PgError
		if !errors.As(e, &denied) || denied.Code != "42501" {
			t.Fatal("expected expired lease RLS denial", e)
		}
	} else if n, e := result.RowsAffected(); e != nil || n != 0 {
		t.Fatal("statement decision cached an expired write lease", e, n)
	}
}

func TestM9RSQLWorkerCanonicalACLAndRetiredCondition(t *testing.T) {
	m := m9Setup(t, nil)
	root := "/v1/tenants/" + m.f.tenantA
	dataset, path := shadowAdopt(t, m.handler, m.token, root+"/preview/"+m.app)
	m.base = path + "/bindings/1/workflows/workflow.contact.review"
	m.start(t, "sql-old-generation")
	target := shadowSource(t, m.handler, m.token, m.f.tenantA, m.app, 2, m.raw, func(d *metadata.Document) { addCount(d, "integer", false) })
	ready := shadowReady(t, m.handler, m.token, path, shadowPrepare(t, m.handler, m.token, path, 1, target, compiler.EvolutionIntent{}))
	shadowStep(t, m.handler, m.token, path, ready, "activate")
	h := r5Handler(t, m.f, audit.Postgres{})
	definition := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "OWNER"})
	definition.Grants = append(definition.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	version := r5Publish(t, h, root+"/roles", m.token, 0, definition, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, definition.Name, 200)
	lease, found, e := m.worker.Claim(context.Background())
	if e != nil || !found {
		t.Fatal(e)
	}
	r5RawWorker(t, m, lease, true, version.Hash)
	// Current logical owner overrides the retained generation's physical owner.
	if _, e = m.f.operator.Exec(`UPDATE runtime.record_identity SET owner_user_id=$1 WHERE tenant_id=$2 AND dataset_id=$3 AND id=$4`, m.f.bob, m.f.tenantA, dataset.ID, m.record.ID); e != nil {
		t.Fatal(e)
	}
	r5RawWorker(t, m, lease, false, version.Hash)
	scope := policy.DataScope{Kind: "SHARE"}
	definition.Grants[1].Scope = &scope
	version = r5Publish(t, h, root+"/roles", m.approver, version.Version, definition, true, uuid(t))
	if _, e = m.f.operator.Exec(`INSERT INTO runtime.identity_shares(tenant_id,app_id,dataset_id,entity_id,id,user_id,can_write) VALUES($1,$2,$3,'entity.contact',$4,$5,false)`, m.f.tenantA, m.app, dataset.ID, m.record.ID, m.f.alice); e != nil {
		t.Fatal(e)
	}
	r5RawWorker(t, m, lease, true, version.Hash)
	if _, e = m.f.operator.Exec(`DELETE FROM runtime.identity_shares WHERE tenant_id=$1 AND dataset_id=$2 AND id=$3 AND user_id=$4`, m.f.tenantA, dataset.ID, m.record.ID, m.f.alice); e != nil {
		t.Fatal(e)
	}
	r5RawWorker(t, m, lease, false, version.Hash)
	// Even a predicate matching the retained snapshot cannot authorize against
	// stale data. This preserves the binding while suspending its business access.
	scope = policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}}
	definition.Grants[1].Scope = &scope
	version = r5Publish(t, h, root+"/roles", m.approver, version.Version, definition, true, uuid(t))
	r5RawWorker(t, m, lease, false, version.Hash)
	// A deliberate non-conditional grant can recover the existing job; no IR or
	// binding is rewritten and no automatic migration approval is manufactured.
	scope = policy.DataScope{Kind: "ALL"}
	definition.Grants[1].Scope = &scope
	version = r5Publish(t, h, root+"/roles", m.approver, version.Version, definition, true, uuid(t))
	r5RawWorker(t, m, lease, true, version.Hash)
}

func TestM9RSQLEventCapabilityRevocation(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	m5Create(t, legacy, base, alice, "SQL event")
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	d := r5Definition("finance_manager", app, policy.DataScope{Kind: "NONE"})
	d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"events.consume"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	v := r5Publish(t, h, root+"/roles", alice, 0, d, true, uuid(t))
	r5Assign(t, h, root, alice, f.viewer, "owner", 200)
	owner := f.token(t, f.viewerSubject, nil)
	r5Assign(t, h, root, owner, f.alice, d.Name, 200)
	dispatcher, db := m8Roles(t)
	dispatch := events.Dispatcher{DB: dispatcher}
	p := m8Claim(t, dispatch)
	if e := dispatch.Finish(context.Background(), p, true); e != nil {
		t.Fatal(e)
	}
	probe := func(t *testing.T, allow bool) {
		t.Helper()
		ticket, completed, ready, dead, e := jobs.EventTicket(context.Background(), db, p.TenantID, p.EventID, p.BodyHash, p.ConsumerID, p.Generation, p.LeaseToken)
		if e != nil || completed || !ready || dead {
			t.Fatal("event identity routing", e)
		}
		tx, e := db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		var bound bool
		if e = tx.QueryRow(`SELECT control.bind_worker_job($1::uuid,$2::uuid,'event',$3,$4::uuid,$5)`, p.TenantID, p.EventID, p.Generation, p.LeaseToken, ticket.ReleaseHash()).Scan(&bound); e != nil || !bound {
			t.Fatal("event bind", e)
		}
		if _, e = tx.Exec(`SELECT set_config('app.actor_id',$1,true),set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$2,true)`, f.alice, f.aliceSubject); e != nil {
			t.Fatal(e)
		}
		var got bool
		var hash string
		if e = tx.QueryRow(`SELECT allowed,policy_version FROM control.worker_policy_decision()`).Scan(&got, &hash); e != nil || got != allow || hash != "tenant-role-v1:"+v.Hash {
			t.Fatal("event SQL authority", e)
		}
		want := 0
		if allow {
			want = 1
		}
		var n int
		if e = tx.QueryRow(`SELECT count(*) FROM runtime.event_envelopes`).Scan(&n); e != nil || n != want {
			t.Fatal("event envelope boundary", e, n)
		}
		if e = tx.QueryRow(`SELECT count(*) FROM runtime.worker_source_record()`).Scan(&n); e != nil || n != 0 {
			t.Fatal("event exposed workflow data", e)
		}
		if _, e = tx.Exec(`SAVEPOINT inbox_probe`); e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(`INSERT INTO runtime.event_inbox(tenant_id,consumer_id,event_id,body_hash) VALUES($1,$2,$3,$4)`, p.TenantID, p.ConsumerID, p.EventID, p.BodyHash)
		if allow {
			if e != nil {
				t.Fatal("authorized SQL receipt", e)
			}
		} else {
			var rejected *pgconn.PgError
			if !errors.As(e, &rejected) || rejected.Code != "42501" {
				t.Fatal("revoked raw receipt not denied")
			}
		}
		if _, e = tx.Exec(`ROLLBACK TO SAVEPOINT inbox_probe`); e != nil {
			t.Fatal(e)
		}
	}
	probe(t, true) // DataScope NONE does not reveal record fields through events.
	d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"events.consume"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}})
	v = r5Publish(t, h, root+"/roles", owner, v.Version, d, true, uuid(t))
	probe(t, false)
	d.Grants = d.Grants[:len(d.Grants)-1]
	v = r5Publish(t, h, root+"/roles", owner, v.Version, d, false, uuid(t))
	probe(t, false)
	var n int
	if e := f.operator.QueryRow(`SELECT count(*) FROM runtime.event_inbox`).Scan(&n); e != nil || n != 0 {
		t.Fatal("probes committed a receipt", e)
	}
}

func TestM9RSQLWorkerCapabilitiesAndLiveRecordScopes(t *testing.T) {
	m := m9Setup(t, nil)
	m.start(t, "sql-policy")
	h := r5Handler(t, m.f, audit.Postgres{})
	root := "/v1/tenants/" + m.f.tenantA
	definition := r5Definition("finance_manager", m.app, policy.DataScope{Kind: "ALL"})
	definition.Grants = append(definition.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
	version := r5Publish(t, h, root+"/roles", m.token, 0, definition, true, uuid(t))
	r5Assign(t, h, root, m.token, m.f.bob, "owner", 200)
	r5Assign(t, h, root, m.approver, m.f.alice, "finance_manager", 200)
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("populated lease", err)
	}
	r5RawWorker(t, m, lease, true, version.Hash)
	setScope := func(scope policy.DataScope) {
		definition = r5Definition("finance_manager", m.app, scope)
		definition.Grants = append(definition.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"workflows.execute", "workflows.activity"}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
		version = r5Publish(t, h, root+"/roles", m.approver, version.Version, definition, true, uuid(t))
	}
	for _, action := range []string{"workflows.execute", "workflows.activity", "data.entity.read", "data.row.read", "data.field.read"} {
		t.Run(action, func(t *testing.T) {
			setScope(policy.DataScope{Kind: "ALL"})
			deny := definition
			deny.Grants = append(append([]policy.Grant{}, definition.Grants...), policy.Grant{Effect: "DENY", Actions: []string{action}, Resource: policy.Selector{AppID: m.app, EntityID: "entity.contact", FieldID: "*"}})
			version = r5Publish(t, h, root+"/roles", m.approver, version.Version, deny, true, uuid(t))
			r5RawWorker(t, m, lease, false, version.Hash)
		})
	}
	t.Run("inactive head", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "ALL"})
		version = r5Publish(t, h, root+"/roles", m.approver, version.Version, definition, false, uuid(t))
		r5RawWorker(t, m, lease, false, version.Hash)
	})
	for _, kind := range []string{"NONE", "TEAM", "SHARE"} {
		t.Run("empty "+kind, func(t *testing.T) {
			setScope(policy.DataScope{Kind: kind})
			r5RawWorker(t, m, lease, false, version.Hash)
		})
	}
	exec := func(query string, args ...any) {
		t.Helper()
		tx, e := m.f.operator.Begin()
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if _, e = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, m.f.aliceSubject, m.f.alice, m.f.tenantA); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(query, args...); e != nil {
			t.Error("owned ACL fixture", e)
			return
		}
		if e = tx.Commit(); e != nil {
			t.Error(e)
		}
	}
	t.Run("live owner change without role change", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "OWNER"})
		r5RawWorker(t, m, lease, true, version.Hash)
		exec(`UPDATE runtime.records SET owner_user_id=$1 WHERE tenant_id=$2 AND id=$3`, m.f.bob, m.f.tenantA, m.record.ID)
		r5RawWorker(t, m, lease, false, version.Hash)
		exec(`UPDATE runtime.records SET owner_user_id=$1 WHERE tenant_id=$2 AND id=$3`, m.f.alice, m.f.tenantA, m.record.ID)
	})
	parent, child := uuid(t), uuid(t)
	exec(`INSERT INTO iam.teams(tenant_id,id,name) VALUES($1,$2,'SQL parent')`, m.f.tenantA, parent)
	exec(`INSERT INTO iam.teams(tenant_id,id,parent_id,name) VALUES($1,$2,$3,'SQL child')`, m.f.tenantA, child, parent)
	exec(`UPDATE runtime.records SET owner_team_id=$1 WHERE tenant_id=$2 AND id=$3`, child, m.f.tenantA, m.record.ID)
	t.Run("live hierarchical team revocation", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "TEAM"})
		exec(`INSERT INTO iam.team_memberships(tenant_id,team_id,user_id) VALUES($1,$2,$3)`, m.f.tenantA, parent, m.f.alice)
		r5RawWorker(t, m, lease, true, version.Hash)
		exec(`UPDATE iam.team_memberships SET active=false WHERE tenant_id=$1 AND team_id=$2 AND user_id=$3`, m.f.tenantA, parent, m.f.alice)
		r5RawWorker(t, m, lease, false, version.Hash)
	})
	t.Run("live share revocation", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "SHARE"})
		exec(`INSERT INTO runtime.record_shares(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id) SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,$1 FROM runtime.records WHERE tenant_id=$2 AND id=$3`, m.f.alice, m.f.tenantA, m.record.ID)
		r5RawWorker(t, m, lease, true, version.Hash)
		exec(`DELETE FROM runtime.record_shares WHERE tenant_id=$1 AND id=$2 AND user_id=$3`, m.f.tenantA, m.record.ID, m.f.alice)
		r5RawWorker(t, m, lease, false, version.Hash)
	})
	t.Run("condition and live record change", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.status", Op: "eq", Value: json.RawMessage(`"active"`)}})
		r5RawWorker(t, m, lease, true, version.Hash)
		exec(`UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.status}','"inactive"'::jsonb) WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, m.record.ID)
		r5RawWorker(t, m, lease, false, version.Hash)
		exec(`UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.status}','"active"'::jsonb) WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, m.record.ID)
	})
	t.Run("AND OR scopes", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "AND", Children: []policy.DataScope{{Kind: "OWNER"}, {Kind: "TEAM"}}})
		r5RawWorker(t, m, lease, false, version.Hash)
		setScope(policy.DataScope{Kind: "OR", Children: []policy.DataScope{{Kind: "OWNER"}, {Kind: "TEAM"}}})
		r5RawWorker(t, m, lease, true, version.Hash)
	})
	for _, tc := range []struct {
		name, field, op, value string
		allow                  bool
	}{
		{"enum in", "status", "in", `["active","inactive"]`, true},
		{"enum not in", "status", "not_in", `["inactive"]`, true},
		{"invalid enum", "status", "eq", `"missing"`, false},
		{"invalid enum after matching list item", "status", "in", `["active","missing"]`, false},
		{"money exact", "balance", "eq", `{"amount":"12.300","currency":"USD"}`, true},
		{"money lower bound", "balance", "gte", `{"amount":"12.31","currency":"USD"}`, false},
		{"money wrong currency", "balance", "eq", `{"amount":"12.30","currency":"INR"}`, false},
		{"name substring", "name", "contains", `"snapshot"`, true},
		{"null date", "joined", "is_null", ``, true},
		{"absent date comparison", "joined", "gt", `"2026-01-01"`, false},
		{"invalid date", "joined", "gt", `"2026-02-30"`, false},
		{"absent reference not in", "parent", "not_in", `["` + m.record.ID + `"]`, false},
		{"core identity", "id", "eq", `"` + m.record.ID + `"`, true},
		{"uppercase core identity denied", "id", "eq", `"` + strings.ToUpper(m.record.ID) + `"`, false},
		{"uppercase reference denied", "parent", "eq", `"` + strings.ToUpper(m.record.ID) + `"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setScope(policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact." + tc.field, Op: tc.op, Value: json.RawMessage(tc.value)}})
			r5RawWorker(t, m, lease, tc.allow, version.Hash)
		})
	}
	t.Run("private helper grant drift", func(t *testing.T) {
		setScope(policy.DataScope{Kind: "NONE"})
		exec(`GRANT EXECUTE ON FUNCTION runtime.worker_lease_source_record() TO framework_worker`)
		defer exec(`REVOKE EXECUTE ON FUNCTION runtime.worker_lease_source_record() FROM framework_worker`)
		if e := (policy.RoleStore{}).Ready(context.Background(), m.db, true); e == nil {
			t.Fatal("unsafe private function grant readiness passed")
		}
		tx, e := m.db.Begin()
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback()
		if ok, e := bindM9R(tx, lease); e != nil || !ok {
			t.Fatal(e)
		}
		var n int
		e = tx.QueryRow(`SELECT count(*) FROM runtime.worker_lease_source_record()`).Scan(&n)
		if e == nil && n != 0 {
			t.Fatal("invoker helper bypassed revocation after accidental grant")
		}
	})
	// Definition/ACL changes never rewrite the immutable job or execute an effect.
	var n int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM m9_fixture.effects`).Scan(&n); e != nil || n != 0 {
		t.Fatal("raw policy probes executed an effect", e)
	}
}
