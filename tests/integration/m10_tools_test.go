//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/jackc/pgx/v5/pgconn"
)

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestM10ReadToolRunsOutsideTransactionAndRecordsLedger(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "read")
	x = m.drive(t, x.ID, "SUCCEEDED")
	l := m.ledger(t, x.ID)
	if l.State != "SUCCEEDED" || l.Attempts != 1 || l.Mode != "TEST_ACCOUNT" {
		t.Fatal("ledger", l)
	}
	// A preview execution uses the tool's own sandbox mode, never production.
	if m.provider.requests(m10ProdHost, "") != 0 || m.provider.requests(m10SandboxHost, "/contacts/") != 1 {
		t.Fatal("wrong endpoint", m.provider.calls)
	}
	if m.secrets.resolved("prod_token") != 0 || m.secrets.resolved("sandbox_token") != 1 {
		t.Fatal("secret use", m.secrets.refs)
	}
	var out string
	if e := m.f.operator.QueryRow(`SELECT convert_from(output,'UTF8') FROM runtime.workflow_receipts WHERE tenant_id=$1 AND instance_id=$2 AND step_key='state.tool'`, m.f.tenantA, x.ID).Scan(&out); e != nil || !strings.Contains(out, `"risk_score":7`) {
		t.Fatal("typed receipt", e, out)
	}
}

func TestM10NativeKeyDeliveryApprovalAndSecretHygiene(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "native")
	x = m.drive(t, x.ID, "WAITING_APPROVAL")
	if m.provider.requests("", "") != 0 || m.ledgerRows(t, x.ID) != 0 {
		t.Fatal("tool ran before approval")
	}
	m.signal(t, x, "approve", m.approver)
	x = m.drive(t, x.ID, "SUCCEEDED")
	l := m.ledger(t, x.ID)
	call := m.provider.lastCall()
	if m.provider.requests("", "/notify") != 1 || m.provider.effectCount() != 1 || l.State != "SUCCEEDED" || l.Attempts != 1 {
		t.Fatal("exactly one delivery expected", m.provider.calls, l)
	}
	var key string
	if e := m.f.operator.QueryRow(`SELECT idempotency_key FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, x.ID).Scan(&key); e != nil || !hex64.MatchString(key) || call.Key != key {
		t.Fatal("idempotency key must be the durable ledger key", key, call.Key)
	}
	if call.Auth != "Bearer tok-sandbox_token" || call.Host != m10SandboxHost || !strings.Contains(call.Body, `"name":"Tool snapshot"`) {
		t.Fatal("request shape", call)
	}
	// Secrets never reach durable evidence: ledger, attempts, receipts, history, audit, outbox.
	for _, probe := range []string{
		`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND state::text LIKE '%tok-%'`,
		`SELECT count(*) FROM runtime.tool_invocations WHERE tenant_id=$1 AND (convert_from(coalesce(response,''::bytea),'UTF8') LIKE '%tok-%' OR tool_ref LIKE '%tok-%')`,
		`SELECT count(*) FROM runtime.workflow_receipts WHERE tenant_id=$1 AND convert_from(output,'UTF8') LIKE '%tok-%'`,
		`SELECT count(*) FROM runtime.workflow_outbox WHERE tenant_id=$1 AND convert_from(body,'UTF8') LIKE '%tok-%'`,
	} {
		var n int
		if e := m.f.operator.QueryRow(probe, m.f.tenantA).Scan(&n); e != nil || n != 0 {
			t.Fatal("secret persisted", probe, e, n)
		}
	}
	// The API view shows bounded tool status and no payload.
	view := m.get(t, x.ID)
	if len(view.Tools) != 1 || view.Tools[0].State != "SUCCEEDED" || view.Tools[0].Mode != "TEST_ACCOUNT" {
		t.Fatal("tool status", view.Tools)
	}
}

// crashScenario drives a WRITE tool whose first remote response is lost to a
// worker crash, then returns the instance after the next stable state.
func m10Crash(t *testing.T, flow string, o m10Options, provider func(*m10Provider), until string) (*m10Fixture, workflow.Instance) {
	t.Helper()
	m := m10Setup(t, o)
	if provider != nil {
		provider(m.provider)
	}
	x := m.startFlow(t, flow)
	x = m.drive(t, x.ID, "WAITING_APPROVAL")
	m.crashAfterFirstSend(x.ID, t)
	m.signal(t, x, "approve", m.approver)
	return m, m.drive(t, x.ID, until)
}

func TestM10CrashAfterSendNativeKeyResendsSameKeyOneEffect(t *testing.T) {
	m, x := m10Crash(t, "native", m10Options{allowInternal: true}, nil, "SUCCEEDED")
	l := m.ledger(t, x.ID)
	if m.provider.requests("", "/notify") != 2 || m.provider.effectCount() != 1 || l.State != "SUCCEEDED" || l.Attempts != 2 {
		t.Fatal("duplicate delivery must collapse to one effect", m.provider.requests("", "/notify"), m.provider.effectCount(), l)
	}
	keys := map[string]bool{}
	for _, c := range m.provider.calls {
		keys[c.Key] = true
	}
	if len(keys) != 1 {
		t.Fatal("resend changed the idempotency key", keys)
	}
	// The abandoned first attempt is retained evidence, not erased.
	var lost, ok int
	if e := m.f.operator.QueryRow(`SELECT count(*) FILTER(WHERE outcome_class='lost'),count(*) FILTER(WHERE outcome='SUCCESS') FROM runtime.tool_attempts WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, x.ID).Scan(&lost, &ok); e != nil || lost != 1 || ok != 1 {
		t.Fatal("attempt evidence", e, lost, ok)
	}
}

func TestM10CrashAfterSendLookupFindsEffectWithoutResend(t *testing.T) {
	m, x := m10Crash(t, "lookup", m10Options{allowInternal: true}, nil, "SUCCEEDED")
	if m.provider.requests("", "/notify-l/") != 1 || m.provider.effectCount() != 1 {
		t.Fatal("lookup must reconcile the unknown send", m.provider.calls)
	}
	posts := 0
	for _, c := range m.provider.calls {
		if c.Method == "POST" {
			posts++
		}
	}
	if posts != 1 || m.ledger(t, x.ID).State != "SUCCEEDED" {
		t.Fatal("resent despite existing effect", posts)
	}
}

func TestM10LookupAbsentAllowsExactlyOneResend(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	// The first POST fails before any effect is created; the response is lost.
	m.provider.setBehavior(func(c m10Call, _ int) string {
		if c.Method == "POST" && m.provider.requests("", "/notify-l") == 1 {
			return "reject500"
		}
		return ""
	})
	x := m.startFlow(t, "lookup")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x = m.drive(t, x.ID, "SUCCEEDED")
	if m.provider.effectCount() != 1 {
		t.Fatal("exactly one effect after proven absence", m.provider.calls)
	}
	var notApplied int
	if e := m.f.operator.QueryRow(`SELECT count(*) FROM runtime.tool_attempts WHERE tenant_id=$1 AND instance_id=$2 AND outcome_class='not_found'`, m.f.tenantA, x.ID).Scan(&notApplied); e != nil || notApplied != 1 {
		t.Fatal("proof of absence not recorded", e, notApplied)
	}
}

func TestM10DedupeWindowResendsInsideAndParksOutsideWindow(t *testing.T) {
	m, x := m10Crash(t, "dedupe", m10Options{allowInternal: true}, nil, "SUCCEEDED")
	if m.provider.effectCount() != 1 || m.provider.requests("", "/notify-d") != 2 || m.ledger(t, x.ID).State != "SUCCEEDED" {
		t.Fatal("inside the provider window the same key is resent", m.provider.calls)
	}
	m2, y := m10Crash(t, "expired", m10Options{allowInternal: true}, nil, "RECONCILE")
	if m2.provider.requests("", "/notify-d") != 1 || m2.ledger(t, y.ID).State != "UNKNOWN" {
		t.Fatal("expired window must not resend", m2.provider.calls)
	}
}

func TestM10NoIdempotencyParksUnknownForOperatorReconciliation(t *testing.T) {
	m, x := m10Crash(t, "none", m10Options{allowInternal: true}, nil, "RECONCILE")
	if m.provider.requests("", "/notify-n") != 1 || m.ledger(t, x.ID).State != "UNKNOWN" {
		t.Fatal("unknown external outcome without idempotency must never resend", m.provider.calls)
	}
	// Time passing never retries it either.
	time.Sleep(600 * time.Millisecond)
	_, _ = m.worker.Once(context.Background())
	if m.provider.requests("", "/notify-n") != 1 {
		t.Fatal("automatic retry of an unknown effect")
	}
	// A viewer cannot reconcile; an owner closes the ledger and the flow resumes.
	viewer := m.f.token(t, m.f.viewerSubject, nil)
	sig := workflow.Signal{Operation: "reconcile", ExpectedVersion: x.Version, StateID: x.StateID, Key: "reconcile-" + uuid(t), OperatorRef: "ops-1", Outcome: "APPLIED", Output: json.RawMessage(`{"notification_id":"n-1"}`)}
	if r := m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", viewer, sig); r.Code != 403 {
		t.Fatal("viewer reconciled", r.Code)
	}
	m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.token, sig))
	x = m.drive(t, x.ID, "SUCCEEDED")
	if l := m.ledger(t, x.ID); l.State != "APPLIED" || m.provider.requests("", "/notify-n") != 1 {
		t.Fatal("reconciled ledger", l)
	}
}

func TestM10ReconcileNotAppliedPermitsOneFreshSend(t *testing.T) {
	m, x := m10Crash(t, "none", m10Options{allowInternal: true}, func(p *m10Provider) {
		p.setBehavior(func(c m10Call, _ int) string {
			if c.Method == "POST" {
				return "reject500"
			}
			return ""
		})
	}, "RECONCILE")
	if m.provider.effectCount() != 0 || m.provider.requests("", "/notify-n") != 1 {
		t.Fatal("setup: first send failed without effect", m.provider.calls)
	}
	m.provider.setBehavior(nil)
	sig := workflow.Signal{Operation: "reconcile", ExpectedVersion: x.Version, StateID: x.StateID, Key: "reconcile-" + uuid(t), OperatorRef: "ops-2", Outcome: "NOT_APPLIED"}
	m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.token, sig))
	x = m.drive(t, x.ID, "SUCCEEDED")
	if m.provider.effectCount() != 1 || m.provider.requests("", "/notify-n") != 2 || m.ledger(t, x.ID).State != "SUCCEEDED" {
		t.Fatal("one fresh send expected", m.provider.calls)
	}
}

func TestM10DelayedProviderResponseIsUnknownThenReconciledByKey(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	m.provider.setBehavior(func(c m10Call, n int) string {
		if c.Method == "POST" && m.provider.requests("", "/notify") == 1 {
			return "slow:1800" // longer than the tool timeout; the response arrives late
		}
		return ""
	})
	x := m.startFlow(t, "native")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x = m.drive(t, x.ID, "SUCCEEDED")
	if m.provider.effectCount() != 1 || m.provider.requests("", "/notify") != 2 {
		t.Fatal("delayed response must collapse to one effect", m.provider.calls)
	}
	rows, err := m.f.operator.Query(`SELECT attempt,outcome,outcome_class FROM runtime.tool_attempts WHERE tenant_id=$1 AND instance_id=$2 ORDER BY attempt`, m.f.tenantA, x.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var n int
		var outcome, class string
		_ = rows.Scan(&n, &outcome, &class)
		got = append(got, outcome+"/"+class)
	}
	if len(got) != 2 || got[0] != "UNKNOWN_REQUIRES_RECONCILIATION/timeout" || got[1] != "SUCCESS/ok" {
		t.Fatal("attempt history", got)
	}
}

func TestM10ProviderClassificationRejectedRateLimitedAndServerError(t *testing.T) {
	// 4xx is a permanent failure and is never retried.
	m := m10Setup(t, m10Options{allowInternal: true})
	m.provider.setBehavior(func(m10Call, int) string { return "400" })
	x := m.startFlow(t, "read")
	x = m.drive(t, x.ID, "DEAD")
	if l := m.ledger(t, x.ID); l.State != "FAILED" || l.Class != "rejected" || l.Retryable || m.provider.requests("", "/contacts/") != 1 {
		t.Fatal("4xx must be permanent", l, m.provider.calls)
	}
	// 429 is retryable with backoff and then succeeds.
	m2 := m10Setup(t, m10Options{allowInternal: true})
	m2.provider.setBehavior(func(c m10Call, _ int) string {
		if m2.provider.requests("", "/contacts/") == 1 {
			return "429"
		}
		return ""
	})
	y := m2.startFlow(t, "read")
	y = m2.drive(t, y.ID, "SUCCEEDED")
	if l := m2.ledger(t, y.ID); l.State != "SUCCEEDED" || l.Attempts != 2 {
		t.Fatal("429 retry", l)
	}
}

func TestM10ApprovalIsBoundToTheApprovedRecordVersion(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "native")
	x = m.drive(t, x.ID, "WAITING_APPROVAL")
	// The record changes after the snapshot was approved-for; the approval is stale.
	values := m5Values("Changed after approval request")
	if r := m5Call(m.handler, m.entity+"/commands", m.token, data.Command{Operation: "update", ID: m.record.ID, ExpectedVersion: 1, Values: values}); r.Code != 200 {
		t.Fatal("record change", r.Code)
	}
	m.signal(t, x, "approve", m.approver)
	x = m.drive(t, x.ID, "DEAD")
	if m.provider.requests("", "") != 0 || m.ledgerRows(t, x.ID) != 0 {
		t.Fatal("stale approval reached the provider", m.provider.calls)
	}
}

func TestM10CatalogTenantPinAndNetworkPolicyFailClosedWithoutIO(t *testing.T) {
	// Tool not licensed for the tenant.
	m := m10Setup(t, m10Options{allowInternal: true, tenantOnly: uuidFor(t)})
	x := m.drive(t, m.startFlow(t, "read").ID, "DEAD")
	if m.provider.requests("", "") != 0 || m.ledgerRows(t, x.ID) != 0 {
		t.Fatal("unlicensed tool performed I/O")
	}
	// Tool removed from the worker's operator catalog.
	empty := tools.EmptyCatalog()
	m = m10Setup(t, m10Options{allowInternal: true, workerCatalog: &empty})
	x = m.drive(t, m.startFlow(t, "read").ID, "DEAD")
	if m.provider.requests("", "") != 0 {
		t.Fatal("removed tool performed I/O")
	}
	// Tool definition changed after the plan pinned its hash.
	def := m10Tools(1)
	for i := range def {
		def[i].Connector.RatePerSecond = 49
	}
	changed, err := tools.NewCatalog(def)
	if err != nil {
		t.Fatal(err)
	}
	m = m10Setup(t, m10Options{allowInternal: true, workerCatalog: &changed})
	x = m.drive(t, m.startFlow(t, "read").ID, "DEAD")
	if m.provider.requests("", "") != 0 {
		t.Fatal("changed tool definition replaced a pin")
	}
	// Loopback is refused without the process-level internal-network opt-in.
	m = m10Setup(t, m10Options{allowInternal: false})
	x = m.drive(t, m.startFlow(t, "read").ID, "DEAD")
	if l := m.ledger(t, x.ID); l.State != "FAILED" || l.Class != "blocked" || m.provider.requests("", "") != 0 {
		t.Fatal("SSRF policy not enforced", l)
	}
}

func uuidFor(t *testing.T) string { return uuid(t) }

func TestM10StaleLeaseCannotContinueAndWorkerSQLIsConfined(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "read")
	// Advance to the tool step, then hold a lease and let it go stale.
	for i := 0; i < 20 && m.get(t, x.ID).StateID != "state.tool"; i++ {
		_, _ = m.worker.Once(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("claim", err)
	}
	m.expireLease(t, x.ID)
	if err = m.worker.Step(context.Background(), lease); !errors.Is(err, workflow.ErrLease) {
		t.Fatal("expired lease continued", err)
	}
	if m.provider.requests("", "") != 0 {
		t.Fatal("stale lease sent a request")
	}
	// Raw worker credentials cannot write the ledger or call ports outside a lease.
	var pe *pgconn.PgError
	for _, sql := range []string{
		`INSERT INTO runtime.tool_invocations(tenant_id,instance_id,step_key,tool_ref,tool_version,tool_hash,idempotency_key,request_hash,mode,state,attempts) VALUES(gen_random_uuid(),gen_random_uuid(),'s','tool.x','1.0.0',repeat('a',64),repeat('b',64),repeat('c',64),'BLOCK','SENDING',1)`,
		`UPDATE runtime.tool_invocations SET state='SUCCEEDED'`,
		`DELETE FROM runtime.tool_attempts`,
		`SELECT control.tool_finish('state.tool',1,'SUCCESS','ok',false,200,false,'{}'::bytea)`,
		`SELECT control.tool_begin('state.tool','tool.contact_lookup','1.0.0',repeat('a',64),repeat('b',64),'TEST_ACCOUNT','SEND',false)`,
		`SELECT control.tool_abandon('state.tool')`,
	} {
		if _, err = m.worker.DB.Exec(sql); err == nil || !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatal("worker credential escaped its lease boundary", sql, err)
		}
	}
	// The runtime API role cannot invoke worker ports or write the ledger.
	for _, sql := range []string{
		`SELECT control.tool_begin('state.tool','tool.contact_lookup','1.0.0',repeat('a',64),repeat('b',64),'TEST_ACCOUNT','SEND',false)`,
		`INSERT INTO runtime.tool_attempts(tenant_id,instance_id,step_key,attempt,kind,mode,lease_generation) VALUES(gen_random_uuid(),gen_random_uuid(),'s',1,'SEND','BLOCK',1)`,
	} {
		if _, err = m.f.db.Exec(sql); err == nil || !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatal("API role reached worker ledger authority", sql, err)
		}
	}
	// The next fresh lease completes normally.
	x = m.drive(t, x.ID, "SUCCEEDED")
	if m.provider.requests("", "/contacts/") != 1 {
		t.Fatal("exactly one send after recovery")
	}
	// Evidence is immutable once finished, even for the operator.
	for _, sql := range []string{`UPDATE runtime.tool_attempts SET outcome_class='ok'`, `DELETE FROM runtime.tool_attempts`, `TRUNCATE runtime.tool_invocations CASCADE`, `DELETE FROM runtime.tool_invocations`} {
		if _, err = m.f.operator.Exec(sql); err == nil {
			t.Fatal("tool evidence mutated", sql)
		}
	}
}

// ---- M11: sandbox ceiling meets the M10 gateway ----

func m10Sandbox(t *testing.T, m *m10Fixture, ceiling, flow string) (data.Dataset, workflow.Instance) {
	t.Helper()
	witness := m9rDatasetWitness(t, m.handler, m.root, m.token)
	box := m11Create(t, m.handler, strings.TrimSuffix(m.root, "/versions/1"), m.token, witness, ceiling, 200)
	path := strings.TrimSuffix(m.root, "/versions/1") + "/datasets/" + box.ID + "/bindings/1"
	seeded := m5Create(t, m.handler, path+"/entities/entity.contact", m.token, "Synthetic contact")
	m.record, m.base = seeded, path+"/workflows/workflow.contact."+flow
	return box, m.start(t, "sandbox-"+flow+"-"+uuid(t))
}

func TestM11SandboxCeilingGovernsEffectiveToolMode(t *testing.T) {
	// BLOCK ceiling: no I/O even though the tool declares a test account.
	m := m10Setup(t, m10Options{allowInternal: true})
	_, x := m10Sandbox(t, m, "BLOCK", "read")
	x = m.drive(t, x.ID, "DEAD")
	if l := m.ledger(t, x.ID); l.Mode != "BLOCK" || l.Class != "blocked" || m.provider.requests("", "") != 0 || m.secrets.resolved("sandbox_token") != 0 || m.secrets.resolved("prod_token") != 0 {
		t.Fatal("BLOCK ceiling performed I/O", l, m.provider.calls)
	}
	// SIMULATE ceiling cannot be honored by a tool without a fixture: BLOCK.
	m = m10Setup(t, m10Options{allowInternal: true})
	_, x = m10Sandbox(t, m, "SIMULATE", "read")
	x = m.drive(t, x.ID, "DEAD")
	if l := m.ledger(t, x.ID); l.Mode != "BLOCK" || m.provider.requests("", "") != 0 {
		t.Fatal("unhonorable ceiling must block", l)
	}
	// A tool that declares a SIMULATE fixture succeeds with zero I/O and says so.
	m = m10Setup(t, m10Options{allowInternal: true})
	_, x = m10Sandbox(t, m, "TEST_ACCOUNT", "simulated")
	x = m.drive(t, x.ID, "SUCCEEDED")
	var simulated bool
	if e := m.f.operator.QueryRow(`SELECT simulated FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2`, m.f.tenantA, x.ID).Scan(&simulated); e != nil || !simulated || m.provider.requests("", "") != 0 || m.ledger(t, x.ID).Mode != "SIMULATE" {
		t.Fatal("simulation must be flagged and do no I/O", e)
	}
	// REROUTE ceiling narrows a test-account tool to the sandbox endpoint.
	m = m10Setup(t, m10Options{allowInternal: true})
	_, x = m10Sandbox(t, m, "REROUTE", "read")
	x = m.drive(t, x.ID, "SUCCEEDED")
	if l := m.ledger(t, x.ID); l.Mode != "REROUTE" || m.provider.requests(m10SandboxHost, "/contacts/") != 1 || m.provider.requests(m10ProdHost, "") != 0 || m.secrets.resolved("prod_token") != 0 {
		t.Fatal("reroute", l, m.provider.calls)
	}
	// READ_ONLY never reaches a WRITE tool.
	m = m10Setup(t, m10Options{allowInternal: true})
	_, x = m10Sandbox(t, m, "READ_ONLY", "native")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x = m.drive(t, x.ID, "DEAD")
	if m.provider.requests("", "") != 0 || m.ledger(t, x.ID).Mode != "BLOCK" {
		t.Fatal("read-only sandbox sent a write", m.provider.calls)
	}
}

func TestM11ForgedWorkerModeAndFrozenSandboxAreRefusedByTheDatabase(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	box, x := m10Sandbox(t, m, "BLOCK", "read")
	for i := 0; i < 20 && m.get(t, x.ID).StateID != "state.tool"; i++ {
		_, _ = m.worker.Once(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	var toolHash string
	if e := m.f.operator.QueryRow(`SELECT s->>'tool_hash' FROM runtime.workflow_bindings b,jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE b.tenant_id=$1 AND b.id=$2 AND s->>'id'='state.tool'`, m.f.tenantA, x.ID).Scan(&toolHash); e != nil {
		t.Fatal(e)
	}
	lease, found, err := m.worker.Claim(context.Background())
	if err != nil || !found {
		t.Fatal("claim", err)
	}
	attempt := func(mode string) error {
		return jobs.WithJob(context.Background(), m.worker.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
			_, e := q.ExecContext(ctx, `SELECT control.tool_begin('state.tool','tool.contact_lookup','1.0.0',$1,repeat('b',64),$2,'SEND',false)`, toolHash, mode)
			return e
		})
	}
	var pe *pgconn.PgError
	for _, forged := range []string{"TEST_ACCOUNT", "REROUTE", "READ_ONLY", "SIMULATE"} {
		if err = attempt(forged); !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatal("worker exceeded the sandbox ceiling with", forged, err)
		}
	}
	if m.ledgerRows(t, x.ID) != 0 || m.provider.requests("", "") != 0 {
		t.Fatal("forged attempt left effects")
	}
	// Freezing the sandbox seals tool sends for its in-flight workflows.
	root := strings.TrimSuffix(m.root, "/versions/1")
	if w := m5Call(m.handler, root+"/sandboxes/"+box.ID+"/freeze", m.token, struct{}{}); w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	if err = attempt("BLOCK"); !errors.As(err, &pe) || pe.Code != "55000" {
		t.Fatal("frozen sandbox accepted a tool attempt", err)
	}
}
