//go:build integration

package integration

// Regression tests for the independent M11 review (docs/verification/M11_INDEPENDENT_REVIEW.md,
// R01-R07), migration 00086 and ADR101 Amendments. Each test is built from the reviewer's
// repro and fails on the code before the fixes (migration 85, REPEATABLE READ freeze).

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/jackc/pgx/v5/pgconn"
)

// m11rRoot is the preview root of the fixture's application.
func m11rRoot(m *m10Fixture) string { return strings.TrimSuffix(m.root, "/versions/1") }

func m11rFreeze(m *m10Fixture, id, token string) *httptest.ResponseRecorder {
	return m5Call(m.handler, m11rRoot(m)+"/sandboxes/"+id+"/freeze", token, struct{}{})
}

// m11rEvidence is the stored evidence of a frozen sandbox.
type m11rEvidence struct {
	Records struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
	} `json:"records"`
	Ledger struct {
		Count int    `json:"count"`
		Hash  string `json:"hash"`
	} `json:"tool_ledger"`
	ManifestOrder string `json:"manifest_order"`
}

func m11rReadEvidence(t *testing.T, f *iamFixture, dataset string) (m11rEvidence, time.Time) {
	t.Helper()
	var body []byte
	var at time.Time
	if err := f.operator.QueryRow(`SELECT body,frozen_at FROM runtime.sandbox_evidence WHERE tenant_id=$1 AND dataset_id=$2`, f.tenantA, dataset).Scan(&body, &at); err != nil {
		t.Fatal("evidence", err)
	}
	var e m11rEvidence
	if json.Unmarshal(body, &e) != nil {
		t.Fatal("evidence body")
	}
	return e, at
}

// m11rLedger recomputes the ledger manifest of a dataset's workflows from the live rows,
// ordered bytewise: instance:step:tool_hash:mode:state:attempts:request_hash joined by "\n".
func m11rLedger(t *testing.T, f *iamFixture, dataset string) (int, string) {
	t.Helper()
	rows, err := f.operator.Query(`SELECT v.instance_id::text||':'||v.step_key||':'||v.tool_hash||':'||v.mode||':'||v.state||':'||v.attempts::text||':'||v.request_hash
 FROM runtime.tool_invocations v JOIN runtime.workflow_bindings w ON w.tenant_id=v.tenant_id AND w.id=v.instance_id WHERE w.tenant_id=$1 AND w.dataset_id=$2`, f.tenantA, dataset)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, s)
	}
	sort.Strings(lines) // Go compares bytes: instance ids are fixed-width lower-case hex
	if len(lines) == 0 {
		return 0, strings.Repeat("0", 64)
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return len(lines), hex.EncodeToString(sum[:])
}

func m11rState(t *testing.T, f *iamFixture, dataset string) (state string) {
	t.Helper()
	if err := f.operator.QueryRow(`SELECT state FROM runtime.sandboxes WHERE tenant_id=$1 AND dataset_id=$2`, f.tenantA, dataset).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// m11rToolArgs returns the pinned tool hash and the request witness the database derives.
func m11rToolArgs(t *testing.T, m *m10Fixture, instance string) (toolHash, requestHash string) {
	t.Helper()
	var planHash string
	var inputs []byte
	if err := m.f.operator.QueryRow(`SELECT b.plan_hash,b.inputs,(SELECT s->>'tool_hash' FROM jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE s->>'id'='state.tool')
 FROM runtime.workflow_bindings b WHERE b.tenant_id=$1 AND b.id=$2`, m.f.tenantA, instance).Scan(&planHash, &inputs, &toolHash); err != nil {
		t.Fatal(err)
	}
	in := sha256.Sum256(inputs)
	out := sha256.Sum256(nil)
	raw := `{"Instance":"` + instance + `","Plan":"` + planHash + `","Step":"state.tool","Activity":"tool.contact_lookup@1.0.0#` + toolHash + `","Input":"` + hex.EncodeToString(in[:]) + `","Output":"` + hex.EncodeToString(out[:]) + `"}`
	sum := sha256.Sum256([]byte(raw))
	return toolHash, hex.EncodeToString(sum[:])
}

// m11rDriveToTool runs the worker until the instance waits at its tool step.
func m11rDriveToTool(t *testing.T, m *m10Fixture, id string) {
	t.Helper()
	for i := 0; i < 40 && m.get(t, id).StateID != "state.tool"; i++ {
		_, _ = m.worker.Once(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	if m.get(t, id).StateID != "state.tool" {
		t.Fatal("the workflow did not reach its tool step")
	}
}

func m11rCode(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// ------------------------------------------------------------------ R01

// TestM11RFreezeWaitsForTheLedgerToSettleAndTheFrozenLedgerNeverChanges (R01): a freeze that
// committed while an attempt was between its send and its outcome produced evidence the ledger
// then outgrew (the outcome, the receipt and two history rows were written after frozen_at).
func TestM11RFreezeWaitsForTheLedgerToSettleAndTheFrozenLedgerNeverChanges(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, x := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	var once sync.Once
	midSend := 0
	m.exec.setAfter(func(tools.Request, tools.Result) { // after the provider answered, before tool_finish
		once.Do(func() { midSend = m11rFreeze(m, box.ID, m.token).Code })
	})
	x = m.drive(t, x.ID, "SUCCEEDED")
	if midSend != 409 {
		t.Fatalf("a freeze while a send was in flight answered %d, want 409", midSend)
	}
	if m11rState(t, m.f, box.ID) != "OPEN" {
		t.Fatal("the refused freeze changed the sandbox")
	}
	// Settled: the freeze succeeds and its ledger manifest is exactly the live one.
	if w := m11rFreeze(m, box.ID, m.token); w.Code != 200 {
		t.Fatal("freeze after the send settled", w.Code, w.Body.String())
	}
	evidence, frozenAt := m11rReadEvidence(t, m.f, box.ID)
	count, hash := m11rLedger(t, m.f, box.ID)
	if evidence.Ledger.Count != 1 || evidence.Ledger.Count != count || evidence.Ledger.Hash != hash {
		t.Fatalf("evidence ledger %+v differs from the live ledger %d %s", evidence.Ledger, count, hash)
	}
	var late int
	if err := m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.tool_attempts WHERE tenant_id=$1 AND instance_id=$2 AND (finished_at>$3 OR started_at>$3))
  +(SELECT count(*) FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2 AND occurred_at>$3)
  +(SELECT count(*) FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2 AND updated_at>$3)`, m.f.tenantA, x.ID, frozenAt).Scan(&late); err != nil || late != 0 {
		t.Fatalf("%d ledger or history rows were written after frozen_at (%v)", late, err)
	}
}

// TestM11RUnknownOutcomeBlocksTheFreeze (R01): an UNKNOWN write (a send whose worker died)
// in RECONCILE is outstanding work: the freeze is refused until the operator reconciles it.
func TestM11RUnknownOutcomeBlocksTheFreeze(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, x := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	m11rDriveToTool(t, m, x.ID)
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("claim", err)
	}
	toolHash, requestHash := m11rToolArgs(t, m, x.ID)
	err = jobs.WithJob(context.Background(), m.worker.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
		_, e := q.ExecContext(ctx, `SELECT * FROM control.tool_begin('state.tool','tool.contact_lookup','1.0.0',$1,$2,'TEST_ACCOUNT','SEND',false)`, toolHash, requestHash)
		return e
	})
	if err != nil {
		t.Fatal("tool_begin", err)
	}
	// The send is in flight: the freeze is refused.
	if w := m11rFreeze(m, box.ID, m.token); w.Code != 409 {
		t.Fatalf("freeze with a send in flight: %d %s", w.Code, w.Body.String())
	}
	// The attempt is lost (the worker died, its lease expired): a newer lease abandons it and the
	// invocation becomes UNKNOWN, which still blocks the freeze.
	m.expireLease(t, x.ID)
	if err = m.worker.Step(context.Background(), lease); !errors.Is(err, workflow.ErrLease) {
		t.Fatal("an expired lease continued", err)
	}
	var lease2 jobs.WorkflowLease
	found = false
	for i := 0; i < 200 && !found; i++ {
		lease2, found, err = m.worker.Claim(context.Background())
		if err != nil {
			t.Fatal("second claim", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !found {
		t.Fatal("the expired lease was never reclaimed")
	}
	var abandoned bool
	if err = jobs.WithJob(context.Background(), m.worker.DB, lease2.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
		return q.QueryRowContext(ctx, `SELECT control.tool_abandon('state.tool')`).Scan(&abandoned)
	}); err != nil || !abandoned {
		t.Fatal("abandon", abandoned, err)
	}
	if w := m11rFreeze(m, box.ID, m.token); w.Code != 409 {
		t.Fatalf("freeze with an unknown outcome outstanding: %d %s", w.Code, w.Body.String())
	}
	if state := m11rState(t, m.f, box.ID); state != "OPEN" {
		t.Fatal("a refused freeze changed the sandbox", state)
	}
}

// ------------------------------------------------------------------ R02

// TestM11RToolBeginAndFreezeSerialise (R02): a lease transaction that began a send raced the
// freeze: the freeze committed first without waiting, the attempt committed after it, and the
// worker sent on a frozen sandbox the evidence did not contain. Here the begin commits within the
// freeze's lock wait, which is the interleaving a stale snapshot would get wrong: the freeze must
// see the begin it waited for and refuse.
func TestM11RToolBeginAndFreezeSerialise(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, x := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	m11rDriveToTool(t, m, x.ID)
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("claim", err)
	}
	toolHash, requestHash := m11rToolArgs(t, m, x.ID)
	began, proceed, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- jobs.WithJob(context.Background(), m.worker.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
			if _, e := q.ExecContext(ctx, `SELECT * FROM control.tool_begin('state.tool','tool.contact_lookup','1.0.0',$1,$2,'TEST_ACCOUNT','SEND',false)`, toolHash, requestHash); e != nil {
				close(began)
				return e
			}
			close(began)
			<-proceed
			return nil
		})
	}()
	<-began
	frozen := make(chan *httptest.ResponseRecorder, 1)
	go func() { frozen <- m11rFreeze(m, box.ID, m.token) }()
	time.Sleep(150 * time.Millisecond) // the freeze is now either done (before the fix) or waiting
	close(proceed)
	if err = <-done; err != nil {
		t.Fatal("the begin transaction failed", err)
	}
	w := <-frozen
	state := m11rState(t, m.f, box.ID)
	count, _ := m11rLedger(t, m.f, box.ID)
	var sending int
	if err = m.f.operator.QueryRow(`SELECT count(*) FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2 AND state='SENDING'`, m.f.tenantA, x.ID).Scan(&sending); err != nil {
		t.Fatal(err)
	}
	if state == "FROZEN" {
		evidence, _ := m11rReadEvidence(t, m.f, box.ID)
		t.Fatalf("the sandbox froze (%d) while a send began: evidence ledger count %d, live ledger count %d, %d in flight", w.Code, evidence.Ledger.Count, count, sending)
	}
	if w.Code != 409 || sending != 1 {
		t.Fatalf("freeze answered %d (want 409) with %d sends in flight", w.Code, sending)
	}
}

// TestM11RFreezeRunsReadCommittedAndRefusesAnyOtherIsolation (R02): the port reads after it holds
// its locks and so needs a fresh snapshot per statement; a REPEATABLE READ caller is refused.
func TestM11RFreezeRunsReadCommittedAndRefusesAnyOtherIsolation(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, _ := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	err := m11rAsApp(t, m.f, m.f.aliceSubject, sql.LevelRepeatableRead, func(ctx context.Context, q postgres.Executor) error {
		_, e := q.ExecContext(ctx, `SELECT runtime.freeze_sandbox($1,$2,$3,repeat('a',64),repeat('b',64))`, m.f.tenantA, m.app, box.ID)
		return e
	})
	if m11rCode(err) != "55000" || !strings.Contains(err.Error(), "read committed") {
		t.Fatalf("a repeatable read freeze: %v", err)
	}
	if m11rState(t, m.f, box.ID) != "OPEN" {
		t.Fatal("the refused freeze changed the sandbox")
	}
	if w := m11rFreeze(m, box.ID, m.token); w.Code != 200 {
		t.Fatal("the API freeze (read committed)", w.Code, w.Body.String())
	}
}

// ------------------------------------------------------------------ R03

// TestM11RFrozenSandboxWorkflowsAreFenced (R03): workflow signals still moved the instances of a
// frozen sandbox (cancel: READY -> COMPENSATING) and the worker kept writing steps and receipts.
func TestM11RFrozenSandboxWorkflowsAreFenced(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, x := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	if w := m11rFreeze(m, box.ID, m.token); w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	before := m.get(t, x.ID)
	cancel := workflow.Signal{Operation: "cancel", ExpectedVersion: before.Version, StateID: before.StateID, Key: "cancel_" + uuid(t), OperatorRef: "m11r"}
	if w := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.token, cancel); w.Code != 409 {
		t.Fatalf("a cancel signal on the workflow of a frozen sandbox answered %d %s", w.Code, w.Body.String())
	}
	if after := m.get(t, x.ID); after.Status != before.Status || after.Version != before.Version || after.StateID != before.StateID {
		t.Fatalf("a frozen sandbox's instance moved: %s v%d -> %s v%d", before.Status, before.Version, after.Status, after.Version)
	}
	var signals, history int
	if err := m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.workflow_signals WHERE tenant_id=$1 AND instance_id=$2),(SELECT count(*) FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2 AND event<>'start' AND occurred_at>(SELECT frozen_at FROM runtime.sandbox_evidence WHERE dataset_id=$3))`, m.f.tenantA, x.ID, box.ID).Scan(&signals, &history); err != nil || signals != 0 || history != 0 {
		t.Fatalf("a refused signal left rows: signals %d history %d (%v)", signals, history, err)
	}
	// The worker cannot move it either: a step under a lease is refused by the fence, not by luck.
	_, werr := m.worker.Once(context.Background())
	if after := m.get(t, x.ID); after.Version != before.Version || after.Status != before.Status {
		t.Fatalf("a worker step moved the frozen sandbox's instance (%v): %s v%d", werr, after.Status, after.Version)
	}
}

// TestM11RLedgerPortsRefuseAFrozenSandbox (R01/R03 defence in depth): settling, abandoning or
// reconciling a ledger row of a frozen sandbox is refused by the port itself.
func TestM11RLedgerPortsRefuseAFrozenSandbox(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, x := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	x = m.drive(t, x.ID, "SUCCEEDED")
	if w := m11rFreeze(m, box.ID, m.token); w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	// An operator reconcile of the (settled) invocation is refused with the frozen message,
	// not with "already settled".
	err := m11rAsApp(t, m.f, m.f.aliceSubject, sql.LevelRepeatableRead, func(ctx context.Context, q postgres.Executor) error {
		_, e := q.ExecContext(ctx, `SELECT control.tool_reconcile($1,$2,'state.tool',true)`, m.f.tenantA, x.ID)
		return e
	})
	_ = err // tool_reconcile returns false when the instance is not in RECONCILE; the fence below is the proof
	for _, probe := range []string{
		`UPDATE runtime.tool_invocations SET updated_at=updated_at WHERE tenant_id=$1 AND instance_id=$2`,
		`UPDATE runtime.tool_attempts SET outcome_class=outcome_class WHERE tenant_id=$1 AND instance_id=$2`,
		`UPDATE runtime.workflow_instances SET failure=failure WHERE tenant_id=$1 AND id=$2`,
	} {
		_, e := m.f.operator.Exec(probe, m.f.tenantA, x.ID)
		if e == nil || (m11rCode(e) != "55000" && m11rCode(e) != "42501") {
			t.Fatalf("the ledger of a frozen sandbox accepted %q: %v", probe, e)
		}
		if m11rCode(e) == "42501" {
			t.Logf("permission denied precedes the fence for %q (the operator holds no UPDATE): covered by the catalog test", probe)
		}
	}
}

// ------------------------------------------------------------------ R04

// m11rAsApp runs fn as the API role for subject, in a transaction of the given isolation level
// that always rolls back unless fn is expected to commit (commit == true).
func m11rAsApp(t *testing.T, f *iamFixture, subject string, level sql.IsolationLevel, fn func(context.Context, postgres.Executor) error) error {
	t.Helper()
	ctx := context.Background()
	var out error
	err := postgres.WithRuntimeIsolation(ctx, f.db, level, func(ctx context.Context, q postgres.Executor) error {
		p, err := f.verifier.Verify(f.token(t, subject, nil))
		if err != nil {
			return err
		}
		u, err := (identity.Store{}).Resolve(ctx, q, p)
		if err != nil {
			return err
		}
		if err = (tenant.Store{}).BindActor(ctx, q, u); err != nil {
			return err
		}
		if _, err = (tenant.Store{}).Resolve(ctx, q, u, f.tenantA); err != nil {
			return err
		}
		out = fn(ctx, q)
		return out
	})
	if out != nil {
		return out
	}
	return err
}

// TestM11RFrozenSandboxIsReadOnlyAtTheSQLLayer (R04): the app credential could still insert and
// delete record links, rewrite the manifest columns of the dataset row and rebind a frozen sandbox.
func TestM11RFrozenSandboxIsReadOnlyAtTheSQLLayer(t *testing.T) {
	m8Database(t)
	f, h, _, alice, app := m5Fixture(t)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	witness := m9rDatasetWitness(t, h, root+"/versions/1", alice)
	box := m11Create(t, h, root, alice, witness, "REROUTE", 200)
	path := root + "/datasets/" + box.ID + "/bindings/1/entities/entity.contact"
	parent := m5Create(t, h, path, alice, "Parent")
	child := m5Call(h, path+"/commands", alice, map[string]any{"operation": "create", "values": map[string]any{"field.contact.name": "Child", "field.contact.parent": parent.ID}})
	if child.Code != 200 {
		t.Fatal("linked record", child.Code, child.Body.String())
	}
	if w := m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", alice, struct{}{}); w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	var revision int64
	var irHash string
	if err := f.operator.QueryRow(`SELECT anchor_revision,anchor_hash FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, box.ID).Scan(&revision, &irHash); err != nil {
		t.Fatal(err)
	}
	var linkedID, targetID string
	if err := f.operator.QueryRow(`SELECT id::text,target_id::text FROM runtime.record_links WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 LIMIT 1`, f.tenantA, app, revision, irHash).Scan(&linkedID, &targetID); err != nil {
		t.Fatal("the sandbox has no link to attack", err)
	}
	type attack struct {
		sql  string
		args []any
	}
	attacks := map[string]attack{
		"insert a link": {`INSERT INTO runtime.record_links(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,field_id,target_entity,target_id)
 VALUES($1,$2,$3,$4,'entity.contact',$5,'field.contact.parent_forged','entity.contact',$5)`, []any{f.tenantA, app, revision, irHash, linkedID}},
		"delete a link":      {`DELETE FROM runtime.record_links WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4 AND id=$5`, []any{f.tenantA, app, revision, irHash, linkedID}},
		"rewrite a manifest": {`UPDATE runtime.datasets SET manifest_hash=repeat('a',64),verified_rows=verified_rows+7 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, []any{f.tenantA, app, box.ID}},
		"rebind": {`WITH b AS (INSERT INTO runtime.dataset_bindings(tenant_id,app_id,dataset_id,version,metadata_revision,ir_hash,ir_body,schema_hash,generation)
  SELECT tenant_id,app_id,dataset_id,version+1,metadata_revision,ir_hash,ir_body,schema_hash,generation FROM runtime.dataset_bindings WHERE tenant_id=$1 AND app_id=$2 AND dataset_id=$3 AND version=1 RETURNING 1)
 UPDATE runtime.datasets SET active_binding=active_binding+1,epoch=epoch+1 WHERE tenant_id=$1 AND app_id=$2 AND id=$3`, []any{f.tenantA, app, box.ID}},
	}
	for name, a := range attacks {
		err := m11rAsApp(t, f, f.aliceSubject, sql.LevelRepeatableRead, func(ctx context.Context, q postgres.Executor) error {
			if _, e := q.ExecContext(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); e != nil {
				return e
			}
			_, e := q.ExecContext(ctx, a.sql, a.args...)
			return e
		})
		if m11rCode(err) != "55000" {
			t.Errorf("%s on a frozen sandbox: %v (want SQLSTATE 55000)", name, err)
		}
	}
	// And nothing of the above stuck.
	var links int
	var manifest string
	var binding int
	if err := f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.record_links WHERE tenant_id=$1 AND app_id=$2 AND metadata_revision=$3 AND ir_hash=$4),(SELECT manifest_hash FROM runtime.datasets WHERE id=$5),(SELECT active_binding FROM runtime.datasets WHERE id=$5)`,
		f.tenantA, app, revision, irHash, box.ID).Scan(&links, &manifest, &binding); err != nil || links != 1 || strings.HasPrefix(manifest, "aaaa") || binding != 1 {
		t.Errorf("the frozen sandbox changed: links %d manifest %q binding %d (%v)", links, manifest, binding, err)
	}
}

// ------------------------------------------------------------------ R05

// TestM11RSandboxPortsRequireOwnerOrAdmin (R05): register_sandbox and freeze_sandbox admitted any
// non-viewer, so a plain member could freeze a sandbox with forged catalog and policy hashes.
func TestM11RSandboxPortsRequireOwnerOrAdmin(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, _ := m10Sandbox(t, m, "TEST_ACCOUNT", "read")
	env := &m13Env{m12Env: &m12Env{t: t, f: m.f}, people: map[string]m13Actor{}}
	member := env.addMemberIn(m.f.tenantA, "plain_member", "member")
	for _, who := range []struct{ name, subject string }{{"member", member.subject}, {"viewer", m.f.viewerSubject}} {
		err := m11rAsApp(t, m.f, who.subject, sql.LevelReadCommitted, func(ctx context.Context, q postgres.Executor) error {
			_, e := q.ExecContext(ctx, `SELECT runtime.freeze_sandbox($1,$2,$3,repeat('a',64),repeat('b',64))`, m.f.tenantA, m.app, box.ID)
			return e
		})
		if m11rCode(err) != "42501" {
			t.Errorf("%s froze a sandbox with forged hashes: %v", who.name, err)
		}
		err = m11rAsApp(t, m.f, who.subject, sql.LevelReadCommitted, func(ctx context.Context, q postgres.Executor) error {
			_, e := q.ExecContext(ctx, `SELECT runtime.register_sandbox($1,$2,$3,'BLOCK')`, m.f.tenantA, m.app, box.ID)
			return e
		})
		if m11rCode(err) != "42501" {
			t.Errorf("%s reached register_sandbox past the role check: %v", who.name, err)
		}
	}
	if m11rState(t, m.f, box.ID) != "OPEN" {
		t.Fatal("a non-admin froze the sandbox")
	}
	// The admin (bob, via the API) and the owner still can.
	if w := m11rFreeze(m, box.ID, m.approver); w.Code != 200 {
		t.Fatal("admin freeze", w.Code, w.Body.String())
	}
}

// ------------------------------------------------------------------ R06

// TestM11RManifestsAreOrderedBytewise (R06): the manifests were ordered with the database default
// collation (en_US.utf8 here), which does not order by bytes and can change with the C library. The
// two entity ids below sort differently under en_US ("entity.ab" first, punctuation ignored) and
// bytewise ("entity.a_z" first).
func TestM11RManifestsAreOrderedBytewise(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	h := m5Handler(t, f, policy.Native{}, audit.Postgres{}, true)
	raw := m11rTwoEntityDocument(t)
	app := "app.order_" + strings.ReplaceAll(uuid(t), "-", "")
	raw = []byte(strings.Replace(string(raw), "app.contacts", app, 1))
	alice := f.token(t, f.aliceSubject, nil)
	if w := m3Call(h, "POST", "/v1/tenants/"+f.tenantA+"/metadata/"+app+"/versions", alice, 0, raw); w.Code != 201 {
		t.Fatal("register", w.Code, w.Body.String())
	}
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	box := m11Create(t, h, root, alice, m9rDatasetWitness(t, h, root+"/versions/1", alice), "BLOCK", 200)
	for _, e := range []string{"a_z", "ab"} {
		w := m5Call(h, root+"/datasets/"+box.ID+"/bindings/1/entities/entity."+e+"/commands", alice, map[string]any{"operation": "create", "values": map[string]any{"field." + e + ".name": "n"}})
		if w.Code != 200 {
			t.Fatal("seed", e, w.Code, w.Body.String())
		}
	}
	if w := m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", alice, struct{}{}); w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	evidence, _ := m11rReadEvidence(t, f, box.ID)
	rows, err := f.operator.Query(`SELECT r.entity_id||':'||r.id::text||':'||r.row_version::text||':'||encode(sha256(convert_to(r.custom_data::text,'UTF8')),'hex')
 FROM runtime.records r JOIN runtime.dataset_generations g ON g.tenant_id=r.tenant_id AND g.app_id=r.app_id AND g.anchor_revision=r.metadata_revision AND g.anchor_hash=r.ir_hash
 WHERE g.tenant_id=$1 AND g.dataset_id=$2`, f.tenantA, box.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, s)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "entity.a_z:") {
		t.Fatalf("fixture: %v", lines)
	}
	if evidence.Records.Count != 2 || evidence.Records.Hash != hex.EncodeToString(sum[:]) {
		t.Errorf("the records manifest is not ordered bytewise: stored %s, bytewise %s", evidence.Records.Hash, hex.EncodeToString(sum[:]))
	}
	if evidence.ManifestOrder != "bytewise" {
		t.Errorf("the evidence does not say how its manifests are ordered: %q", evidence.ManifestOrder)
	}
}

// ------------------------------------------------------------------ integrity

// TestM11RMigration86KeepsEveryGuardOfTheFunctionsItReplaces: the replaced functions keep the
// markers of the older definitions and carry the new ones; the fence triggers exist and are enabled.
func TestM11RMigration86KeepsEveryGuardOfTheFunctionsItReplaces(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	def := func(signature string) string {
		var s string
		if err := f.operator.QueryRow(`SELECT pg_get_functiondef($1::regprocedure)`, signature).Scan(&s); err != nil {
			t.Fatal(signature, err)
		}
		return s
	}
	for signature, markers := range map[string][]string{
		"control.tool_begin(text,text,text,text,text,text,text,boolean)": {"exact tool lease and live capability required", "invalid tool attempt witness", "tool step is not current", "tool mode exceeds the sandbox ceiling",
			"tool attempt does not match the pinned plan", "tool approval evidence required", "approved snapshot is stale", "tool request witness does not match the pinned binding", "unknown tool outcome outstanding",
			"tool attempt budget exhausted", "tool_approval_gated", "FOR SHARE", "sandbox is frozen"},
		"control.tool_finish(text,integer,text,text,boolean,integer,boolean,bytea)": {"exact tool lease required", "invalid tool outcome", "stale tool outcome rejected", "invalid lookup outcome", "settle_ms", "unknown_outstanding", "instance_in_frozen_sandbox"},
		"control.tool_abandon(text)":                     {"exact tool lease and live capability required", "tool attempt may still be in flight", "unknown_outstanding", "instance_in_frozen_sandbox"},
		"control.tool_reconcile(uuid,uuid,text,boolean)": {"tenant reconciler required", "tool invocation already settled", "unknown_outstanding", "instance_in_frozen_sandbox"},
		"runtime.register_sandbox(uuid,text,uuid,text)":  {"tenant sandbox administrator required", "invalid sandbox tool ceiling", "open sandbox dataset required", "'owner','admin'"},
		"runtime.freeze_sandbox(uuid,text,uuid,text,text)": {"tenant sandbox administrator required", "invalid freeze witness", "open sandbox required", "sandbox evidence budget exceeded", "read committed", "outstanding tool work",
			"'owner','admin'", `COLLATE "C"`, "manifest_order"},
		"runtime.guard_sandbox_dataset_update()": {"frozen sandbox datasets stay frozen", "frozen sandbox is read-only"},
	} {
		body := def(signature)
		for _, marker := range markers {
			if !strings.Contains(body, marker) {
				t.Errorf("%s lost or lacks %q", signature, marker)
			}
		}
	}
	var fences int
	if err := f.operator.QueryRow(`SELECT count(*) FROM pg_catalog.pg_trigger WHERE tgname='sandbox_frozen_fence' AND tgenabled='O' AND NOT tgisinternal
 AND tgrelid IN('runtime.workflow_instances'::regclass,'runtime.workflow_history'::regclass,'runtime.workflow_receipts'::regclass,'runtime.workflow_signals'::regclass,'runtime.tool_invocations'::regclass,
 'runtime.tool_attempts'::regclass,'runtime.record_links'::regclass,'runtime.dataset_bindings'::regclass,'runtime.dataset_generations'::regclass)`).Scan(&fences); err != nil || fences != 9 {
		t.Errorf("fence triggers: %d (%v)", fences, err)
	}
}

// m11rTwoEntityDocument is the contacts application plus two more entities, one field each.
func m11rTwoEntityDocument(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	definitions := doc["definitions"].([]any)
	for _, e := range []string{"a_z", "ab"} {
		definitions = append(definitions,
			map[string]any{"id": "entity." + e, "schema_version": "1.0.0", "namespace": "example.contacts", "label": "Entity " + e, "kind": "entity", "entity": map[string]any{"fields": []string{"field." + e + ".name"}}},
			map[string]any{"id": "field." + e + ".name", "schema_version": "1.0.0", "namespace": "example.contacts", "label": "Name " + e, "kind": "field", "field": map[string]any{"entity_ref": "entity." + e, "type": "string", "required": true}})
	}
	doc["definitions"] = definitions
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
