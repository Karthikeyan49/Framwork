//go:build integration

package integration

// Regression tests for the independent M10 review findings M10R-01..M10R-05
// (docs/verification/M10_INDEPENDENT_REVIEW.md). Each test was built from the
// reviewer's executed probe and failed on the reviewed code; it asserts the
// ADR100 contract (including its Amendments section).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/jobs"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/jackc/pgx/v5/pgconn"
)

// m10rRun drives the worker without failing on worker errors; it returns the
// last instance view and the distinct worker errors observed.
func m10rRun(t *testing.T, m *m10Fixture, id string, until []string, d time.Duration) (workflow.Instance, []string) {
	t.Helper()
	seen := map[string]bool{}
	var errs []string
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		x := m.get(t, id)
		for _, s := range until {
			if x.Status == s {
				return x, errs
			}
		}
		found, e := m.worker.Once(context.Background())
		if e != nil && !errors.Is(e, workflow.ErrLease) {
			if msg := e.Error(); !seen[msg] {
				seen[msg] = true
				errs = append(errs, msg)
			}
		}
		if !found || e != nil {
			time.Sleep(20 * time.Millisecond)
		}
	}
	return m.get(t, id), errs
}

func m10rAttempts(t *testing.T, m *m10Fixture, id string) []string {
	t.Helper()
	rows, err := m.f.operator.Query(`SELECT kind,outcome,outcome_class FROM runtime.tool_attempts WHERE tenant_id=$1 AND instance_id=$2 ORDER BY attempt`, m.f.tenantA, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var kind, outcome, class string
		if err = rows.Scan(&kind, &outcome, &class); err != nil {
			t.Fatal(err)
		}
		out = append(out, kind+"/"+outcome+"/"+class)
	}
	return out
}

func m10rPosts(m *m10Fixture, prefix string) int {
	m.provider.mu.Lock()
	defer m.provider.mu.Unlock()
	n := 0
	for _, c := range m.provider.calls {
		if c.Method == "POST" && strings.HasPrefix(c.Path, prefix) {
			n++
		}
	}
	return n
}

func m10rCode(e error) string {
	var pe *pgconn.PgError
	if errors.As(e, &pe) {
		return pe.Code
	}
	if e == nil {
		return "ok"
	}
	return e.Error()
}

// m10rLease claims the exact lease of instance id (other jobs are stepped).
func m10rLease(t *testing.T, m *m10Fixture, id string) jobs.WorkflowLease {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		lease, found, err := m.worker.Claim(context.Background())
		if err != nil {
			t.Fatal("claim", err)
		}
		if found && lease.Ticket.JobID() == id {
			return lease
		}
		if found {
			_ = m.worker.Step(context.Background(), lease)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("lease for", id, "not claimed")
	return jobs.WorkflowLease{}
}

// m10rInLease runs one raw statement as framework_worker inside the exact lease.
func m10rInLease(m *m10Fixture, lease jobs.WorkflowLease, sql string, args ...any) (string, error) {
	var out string
	e := jobs.WithJob(context.Background(), m.worker.DB, lease.Ticket, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
		if strings.HasPrefix(sql, "UPDATE") || strings.HasPrefix(sql, "INSERT") || strings.HasPrefix(sql, "DELETE") {
			res, e := q.ExecContext(ctx, sql, args...)
			if e != nil {
				return e
			}
			n, _ := res.RowsAffected()
			out = fmt.Sprintf("rows=%d", n)
			return nil
		}
		return q.QueryRowContext(ctx, sql, args...).Scan(&out)
	})
	return out, e
}

// m10rToolPin returns the pinned tool hash and the exact step request hash
// (computed as workflow.pendingActivity does) for state.tool.
func m10rToolPin(t *testing.T, m *m10Fixture, id, ref string) (toolHash, requestHash string) {
	t.Helper()
	var planHash string
	var inputs []byte
	if e := m.f.operator.QueryRow(`SELECT s->>'tool_hash',b.plan_hash,b.inputs FROM runtime.workflow_bindings b,jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE b.tenant_id=$1 AND b.id=$2 AND s->>'id'='state.tool'`, m.f.tenantA, id).Scan(&toolHash, &planHash, &inputs); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(struct{ Instance, Plan, Step, Activity, Input, Output string }{id, planHash, "state.tool", ref + "@1.0.0#" + toolHash, workflow.Hash(inputs), workflow.Hash(nil)})
	return toolHash, workflow.Hash(body)
}

// M10R-02: a compromised worker inside its own exact lease can neither jump
// past the approval state, nor declare that no approval is needed, nor forge a
// tool request witness or a tool receipt. Built from the reviewer's raw-SQL
// probe TestZZReviewM10C4WorkerSkipsApprovalInsideOwnLease.
func TestM10R02WorkerCannotSkipApprovalInsideOwnLease(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "native")
	toolHash, requestHash := m10rToolPin(t, m, x.ID, "tool.notify_native")
	lease := m10rLease(t, m, x.ID)
	// 1. The pinned plan has no `always` transition from state.start to
	// state.tool: the database refuses the jump.
	jump, e := m10rInLease(m, lease, `UPDATE runtime.workflow_instances SET state_id='state.tool',status='READY' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID)
	if e == nil {
		t.Errorf("FINDING M10R-02: worker moved the instance past the approval state inside its lease (%s)", jump)
	}
	// 2. Even at the tool state (placed there by the operator here), the
	// database derives the approval requirement from the pinned plan and
	// ignores the caller's needs_approval flag.
	if _, e = m.f.operator.Exec(`UPDATE runtime.workflow_instances SET state_id='state.tool',status='READY' WHERE tenant_id=$1 AND id=$2`, m.f.tenantA, x.ID); e != nil {
		t.Fatal(e)
	}
	att, e := m10rInLease(m, lease, `SELECT attempt::text FROM control.tool_begin('state.tool','tool.notify_native','1.0.0',$1,$2,'TEST_ACCOUNT','SEND',false)`, toolHash, requestHash)
	if m10rCode(e) != "TL001" {
		t.Errorf("FINDING M10R-02: approval-REQUIRED tool attempt accepted without approval evidence (attempt=%s err=%s)", att, m10rCode(e))
	}
	// 3. A tool receipt without a matching settled ledger is refused.
	forged, e := m10rInLease(m, lease, `INSERT INTO runtime.workflow_receipts(tenant_id,instance_id,step_key,activity_ref,request_hash,output,output_hash,sequence,compensation_ref) VALUES($1,$2,'state.tool',$3,$4,convert_to('{"notification_id":"forged"}','UTF8'),encode(sha256(convert_to('{"notification_id":"forged"}','UTF8')),'hex'),2,'')`, m.f.tenantA, x.ID, "tool.notify_native@1.0.0#"+toolHash, requestHash)
	if e == nil {
		t.Errorf("FINDING M10R-02: forged tool receipt accepted without ledger evidence (%s)", forged)
	}
	y, errs := m10rRun(t, m, x.ID, []string{"SUCCEEDED", "DEAD", "RECONCILE"}, 10*time.Second)
	var approvals, receipts int
	if e = m.f.operator.QueryRow(`SELECT (SELECT count(*) FROM runtime.workflow_history WHERE tenant_id=$1 AND instance_id=$2 AND event='approve'),(SELECT count(*) FROM runtime.workflow_receipts WHERE tenant_id=$1 AND instance_id=$2 AND step_key='state.tool')`, m.f.tenantA, x.ID).Scan(&approvals, &receipts); e != nil {
		t.Fatal(e)
	}
	t.Logf("final status=%s approvals=%d receipts=%d provider_requests=%d errs=%v", y.Status, approvals, receipts, m.provider.requests("", ""), errs)
	if y.Status != "DEAD" || receipts != 0 || m.provider.requests("", "") != 0 {
		t.Errorf("FINDING M10R-02: unapproved tool step ended %s with %d receipts and %d provider requests", y.Status, receipts, m.provider.requests("", ""))
	}

	// 4. With real approval evidence, a forged request witness is still refused.
	m2 := m10Setup(t, m10Options{allowInternal: true})
	z := m2.startFlow(t, "native")
	z = m2.signal(t, m2.drive(t, z.ID, "WAITING_APPROVAL"), "approve", m2.approver)
	hash2, request2 := m10rToolPin(t, m2, z.ID, "tool.notify_native")
	lease2 := m10rLease(t, m2, z.ID)
	if _, e = m10rInLease(m2, lease2, `SELECT attempt::text FROM control.tool_begin('state.tool','tool.notify_native','1.0.0',$1,repeat('b',64),'TEST_ACCOUNT','SEND',true)`, hash2); m10rCode(e) != "22023" {
		t.Errorf("FINDING M10R-02: forged request witness accepted (%s)", m10rCode(e))
	}
	// The honest worker still completes the approved instance with one effect.
	z = m2.drive(t, z.ID, "SUCCEEDED")
	var ledgerRequest string
	if e = m2.f.operator.QueryRow(`SELECT request_hash FROM runtime.tool_invocations WHERE tenant_id=$1 AND instance_id=$2`, m2.f.tenantA, z.ID).Scan(&ledgerRequest); e != nil || ledgerRequest != request2 || m2.provider.effectCount() != 1 {
		t.Fatal("approved instance", e, ledgerRequest == request2, m2.provider.effectCount())
	}
}

// M10R-01: an UNKNOWN LOOKUP_BY_REFERENCE write is never resent without a
// lookup that proved absence; a transient lookup failure (429) retries the
// lookup. Built from TestZZReviewM10C1LookupTransientFailureResendsWrite.
func TestM10R01TransientLookupFailureNeverResendsUnknownWrite(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	var mu sync.Mutex
	posts, gets := 0, 0
	m.provider.setBehavior(func(c m10Call, _ int) string {
		mu.Lock()
		defer mu.Unlock()
		if c.Method == "POST" {
			if posts++; posts == 1 {
				return "drop" // effect created, response lost after write: UNKNOWN
			}
		}
		if c.Method == "GET" {
			if gets++; gets == 1 {
				return "429" // transient lookup failure
			}
		}
		return ""
	})
	x := m.startFlow(t, "lookup")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x, errs := m10rRun(t, m, x.ID, []string{"SUCCEEDED", "DEAD", "RECONCILE"}, 20*time.Second)
	att := m10rAttempts(t, m, x.ID)
	t.Logf("status=%s posts=%d lookups=%d effects=%d attempts=%v errs=%v", x.Status, m10rPosts(m, "/notify-l"), m.provider.requests("", "/notify-l/"), m.provider.effectCount(), att, errs)
	if m10rPosts(m, "/notify-l") != 1 {
		t.Errorf("FINDING M10R-01: LOOKUP_BY_REFERENCE write sent %d times without a lookup proving absence; attempts=%v", m10rPosts(m, "/notify-l"), att)
	}
	if x.Status != "SUCCEEDED" || m.ledger(t, x.ID).State != "SUCCEEDED" {
		t.Errorf("the second lookup must find the effect: status=%s attempts=%v", x.Status, att)
	}
}

// M10R-01 (database side): while an unknown outcome is outstanding the
// database refuses a SEND that is not one of the permitted paths, whatever
// the worker claims.
func TestM10R01DatabaseRefusesSendWhileUnknownOutstanding(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "lookup")
	x = m.signal(t, m.drive(t, x.ID, "WAITING_APPROVAL"), "approve", m.approver)
	toolHash, requestHash := m10rToolPin(t, m, x.ID, "tool.notify_lookup")
	lease := m10rLease(t, m, x.ID)
	begin := func(kind string) (string, error) {
		return m10rInLease(m, lease, `SELECT attempt::text FROM control.tool_begin('state.tool','tool.notify_lookup','1.0.0',$1,$2,'TEST_ACCOUNT',$3,true)`, toolHash, requestHash, kind)
	}
	finish := func(n int, outcome, class string, retry bool, status int) (string, error) {
		return m10rInLease(m, lease, `SELECT control.tool_finish('state.tool',$1,$2,$3,$4,$5,false,NULL)`, n, outcome, class, retry, status)
	}
	if n, e := begin("SEND"); e != nil || n != "1" {
		t.Fatal("first send", n, e)
	}
	if s, e := finish(1, "UNKNOWN_REQUIRES_RECONCILIATION", "transport", false, 0); e != nil || s != "UNKNOWN" {
		t.Fatal("unknown send", s, e)
	}
	// A transient lookup failure does not settle the unknown write.
	if n, e := begin("LOOKUP"); e != nil || n != "2" {
		t.Fatal("lookup", n, e)
	}
	s, e := finish(2, "FAILED", "rate_limited", true, 429)
	if s != "UNKNOWN" {
		t.Errorf("FINDING M10R-01: a failed lookup moved the unknown write to %s (%s)", s, m10rCode(e))
	}
	if n, e := begin("SEND"); e == nil {
		t.Errorf("FINDING M10R-01: database admitted a blind resend (attempt %s) while an unknown outcome is outstanding", n)
	}
	if posts := m10rPosts(m, ""); posts != 0 {
		t.Fatal("raw SQL must not reach the provider", posts)
	}
}

// M10R-04: a lookup answered 4xx (not 404) leaves the original write UNKNOWN
// and reconcilable. Built from TestZZReviewM10C3bLookupRejectedStrandsUnknownWrite.
func TestM10R04RejectedLookupKeepsUnknownWriteReconcilable(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	var mu sync.Mutex
	posts := 0
	m.provider.setBehavior(func(c m10Call, _ int) string {
		mu.Lock()
		defer mu.Unlock()
		if c.Method == "POST" {
			if posts++; posts == 1 {
				return "drop"
			}
		}
		if c.Method == "GET" {
			return "400"
		}
		return ""
	})
	x := m.startFlow(t, "lookup")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x, errs := m10rRun(t, m, x.ID, []string{"RECONCILE", "DEAD", "SUCCEEDED"}, 20*time.Second)
	l := m.ledger(t, x.ID)
	t.Logf("status=%s ledger=%+v effects=%d attempts=%v errs=%v", x.Status, l, m.provider.effectCount(), m10rAttempts(t, m, x.ID), errs)
	if x.Status != "RECONCILE" || l.State != "UNKNOWN" || m10rPosts(m, "/notify-l") != 1 {
		t.Fatalf("FINDING M10R-04: write with an effect at the provider ended %s with ledger %s/%s after %d posts", x.Status, l.State, l.Class, m10rPosts(m, "/notify-l"))
	}
	sig := workflow.Signal{Operation: "reconcile", ExpectedVersion: x.Version, StateID: x.StateID, Key: "reconcile-" + uuid(t), OperatorRef: "ops-1", Outcome: "APPLIED", Output: []byte(`{"notification_id":"n-1"}`)}
	m9Decode(t, m5Call(m.handler, m.base+"/instances/"+x.ID+"/signals", m.token, sig))
	x = m.drive(t, x.ID, "SUCCEEDED")
	if l = m.ledger(t, x.ID); l.State != "APPLIED" || m10rPosts(m, "/notify-l") != 1 {
		t.Fatal("reconciled ledger", l)
	}
}

// M10R-04: a catalog change while a send is unsettled must park the
// instance in RECONCILE (the ledger is read before the catalog fail-closed
// check), never DEAD with an unrecorded effect.
func TestM10R04CatalogChangeWhileUnknownParksInReconcile(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "native")
	x = m.drive(t, x.ID, "WAITING_APPROVAL")
	m.crashAfterFirstSend(x.ID, t)
	m.signal(t, x, "approve", m.approver)
	for i := 0; i < 200 && m10rPosts(m, "/notify") == 0; i++ {
		_, _ = m.worker.Once(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	// The operator removes the tool while its first send is unsettled.
	m.worker.Store.ToolCatalog = tools.EmptyCatalog()
	y, errs := m10rRun(t, m, x.ID, []string{"RECONCILE", "DEAD", "SUCCEEDED"}, 15*time.Second)
	l := m.ledger(t, x.ID)
	t.Logf("status=%s ledger=%+v posts=%d effects=%d errs=%v", y.Status, l, m10rPosts(m, "/notify"), m.provider.effectCount(), errs)
	if y.Status != "RECONCILE" || l.State != "UNKNOWN" {
		t.Fatalf("FINDING M10R-04: catalog change stranded an unsettled send: status=%s ledger=%s/%s", y.Status, l.State, l.Class)
	}
	if m10rPosts(m, "/notify") != 1 {
		t.Fatal("removed tool performed I/O", m10rPosts(m, "/notify"))
	}
}

// M10R-03: retry policy bounds sends of a tool that keeps failing retryably.
// Built from TestZZReviewM10C2RetryWaitBypassesMaxAttempts.
func TestM10R03RetryWaitHonoursMaxAttempts(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	m.provider.setBehavior(func(c m10Call, _ int) string {
		if strings.HasPrefix(c.Path, "/contacts/") {
			return "429"
		}
		return ""
	})
	x := m.startFlow(t, "read")
	began := time.Now()
	x, errs := m10rRun(t, m, x.ID, []string{"DEAD", "SUCCEEDED", "RECONCILE"}, 15*time.Second)
	l := m.ledger(t, x.ID)
	t.Logf("after %s status=%s attempt=%d requests=%d ledger=%+v errs=%v", time.Since(began).Round(time.Millisecond), x.Status, x.Attempt, m.provider.requests("", "/contacts/"), l, errs)
	if x.Status != "DEAD" || m.provider.requests("", "/contacts/") != 3 || l.Attempts != 3 {
		t.Errorf("FINDING M10R-03: max_attempts=3 not enforced: status=%s provider requests=%d ledger attempts=%d errs=%v", x.Status, m.provider.requests("", "/contacts/"), l.Attempts, errs)
	}
	// Backoff is honoured between sends (100 ms then 200 ms at least).
	if elapsed := time.Since(began); elapsed < 300*time.Millisecond {
		t.Errorf("FINDING M10R-03: backoff ignored, three sends within %s", elapsed)
	}
}

// M10R-03: repeated UNKNOWN outcomes of a NATIVE_KEY write are resent only
// within the attempt budget and then park in RECONCILE. Built from
// TestZZReviewM10C2bUnknownResendsUnboundedNeverReconcile.
func TestM10R03UnknownKeyedResendsAreBoundedThenReconcile(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	m.provider.setBehavior(func(c m10Call, _ int) string {
		if c.Method == "POST" {
			return "500"
		}
		return ""
	})
	x := m.startFlow(t, "native")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x, errs := m10rRun(t, m, x.ID, []string{"RECONCILE", "DEAD", "SUCCEEDED"}, 15*time.Second)
	l := m.ledger(t, x.ID)
	t.Logf("status=%s attempt=%d posts=%d effects=%d ledger=%+v errs=%v", x.Status, x.Attempt, m10rPosts(m, "/notify"), m.provider.effectCount(), l, errs)
	if x.Status != "RECONCILE" || m10rPosts(m, "/notify") != 3 || l.State != "UNKNOWN" {
		t.Errorf("FINDING M10R-03: UNKNOWN write resent %d times; status=%s (expected RECONCILE after max_attempts=3); errs=%v", m10rPosts(m, "/notify"), x.Status, errs)
	}
}

// M10R-05: the lookup that may prove absence waits for the provider's
// declared settle interval after the original send's deadline. Built from
// TestZZReviewM10C5LookupWhileOriginalRequestInFlight.
func TestM10R05LookupWaitsForDeclaredSettleInterval(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	var mu sync.Mutex
	posts := 0
	var firstPost, firstPostDone, firstGet time.Time
	m.provider.setBehavior(func(c m10Call, _ int) string {
		mu.Lock()
		if c.Method == "POST" {
			if posts++; posts == 1 {
				firstPost = time.Now()
				mu.Unlock()
				// A provider that commits the write at the end of slow processing.
				time.Sleep(1800 * time.Millisecond)
				mu.Lock()
				firstPostDone = time.Now()
				mu.Unlock()
				return ""
			}
		}
		if c.Method == "GET" && firstGet.IsZero() {
			firstGet = time.Now()
		}
		mu.Unlock()
		return ""
	})
	x := m.startFlow(t, "lookup")
	x = m.approve(t, m.drive(t, x.ID, "WAITING_APPROVAL").ID)
	x, errs := m10rRun(t, m, x.ID, []string{"SUCCEEDED", "DEAD", "RECONCILE"}, 20*time.Second)
	time.Sleep(2 * time.Second)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("status=%s posts=%d attempts=%v lookup_after_first_post=%s first_post_processing_ended_after=%s errs=%v", x.Status, posts, m10rAttempts(t, m, x.ID), firstGet.Sub(firstPost).Round(time.Millisecond), firstPostDone.Sub(firstPost).Round(time.Millisecond), errs)
	if posts > 1 {
		t.Errorf("FINDING M10R-05: lookup issued %s after the first POST while the provider was still processing it (until %s); write sent %d times", firstGet.Sub(firstPost).Round(time.Millisecond), firstPostDone.Sub(firstPost).Round(time.Millisecond), posts)
	}
	if x.Status != "SUCCEEDED" {
		t.Errorf("the settled lookup must find the effect: %s", x.Status)
	}
}

// M10R-05 (database side): a 404 lookup that starts before the original
// send's deadline plus the declared settle interval never proves absence.
func TestM10R05EarlyNotFoundCannotProveAbsence(t *testing.T) {
	m := m10Setup(t, m10Options{allowInternal: true})
	x := m.startFlow(t, "lookup")
	x = m.signal(t, m.drive(t, x.ID, "WAITING_APPROVAL"), "approve", m.approver)
	toolHash, requestHash := m10rToolPin(t, m, x.ID, "tool.notify_lookup")
	lease := m10rLease(t, m, x.ID)
	begin := func(kind string) (string, error) {
		return m10rInLease(m, lease, `SELECT attempt::text FROM control.tool_begin('state.tool','tool.notify_lookup','1.0.0',$1,$2,'TEST_ACCOUNT',$3,true)`, toolHash, requestHash, kind)
	}
	if n, e := begin("SEND"); e != nil || n != "1" {
		t.Fatal("send", n, e)
	}
	if s, e := m10rInLease(m, lease, `SELECT control.tool_finish('state.tool',1,'UNKNOWN_REQUIRES_RECONCILIATION','timeout',false,0,false,NULL)`); e != nil || s != "UNKNOWN" {
		t.Fatal("unknown", s, e)
	}
	if n, e := begin("LOOKUP"); e != nil || n != "2" {
		t.Fatal("lookup", n, e)
	}
	s, e := m10rInLease(m, lease, `SELECT control.tool_finish('state.tool',2,'FAILED','not_found',false,404,false,NULL)`)
	if s != "UNKNOWN" {
		t.Errorf("FINDING M10R-05: an immediate 404 lookup proved absence (%s %s)", s, m10rCode(e))
	}
	// The honest worker later proves absence after the settle interval and
	// performs exactly one send (the raw-SQL attempt never reached the provider).
	y, errs := m10rRun(t, m, x.ID, []string{"SUCCEEDED", "DEAD", "RECONCILE"}, 20*time.Second)
	t.Logf("status=%s attempts=%v errs=%v", y.Status, m10rAttempts(t, m, x.ID), errs)
	if y.Status != "SUCCEEDED" || m10rPosts(m, "/notify-l") != 1 || m.provider.effectCount() != 1 {
		t.Errorf("settled absence must permit exactly one send: status=%s posts=%d", y.Status, m10rPosts(m, "/notify-l"))
	}
}
