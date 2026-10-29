//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
)

// Regression tests of the independent M14 review (docs/verification/M14_INDEPENDENT_REVIEW.md) through the real routes: the labels of a
// provider answer (M14R-01), a canceled provider call (M14R-02), results that finish together (M14R-03), an enormous reported usage
// (M14R-06) and an adapter that ignores its context (M14R-07). Every "model answer" is a scripted byte string.

// m14rUsage is the tenant's usage row of the day, operator view.
func (e *m14Env) m14rUsage() (requests, used int64) {
	e.t.Helper()
	if err := e.f.operator.QueryRow(`SELECT coalesce(sum(requests),0),coalesce(sum(used_tokens),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&requests, &used); err != nil {
		e.t.Fatal(err)
	}
	return requests, used
}

// m14rProposals lists status/failure of the app's proposals in creation order.
func (e *m14Env) m14rProposals() []string {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT status||'/'||coalesce(failure_code,'') FROM control.ai_proposals WHERE tenant_id=$1 AND app_id=$2 ORDER BY created_at`, e.f.tenantA, e.app)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func countOf(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}

// M14R-01: a provider-chosen label outside [A-Za-z0-9._:/@+-] reached the audit entry raw, the audit refused it, and a paid answer
// became a 503 with a PENDING proposal that held its in-flight slot until the lease. The labels are reduced once, in the gateway.
func TestM14R01ProviderLabelsOutsideTheAuditCharsetKeepTheResult(t *testing.T) {
	for label, want := range map[string]string{"alpha 4 turbo": "alpha_4_turbo", "Beta-3.1 (70B)": "Beta-3.1_70B", "модель-1": "-1", "model=fast": "model_fast"} {
		t.Run(label, func(t *testing.T) {
			e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
			doc := e.draft("500")
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Renamed"), Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: label})
			p := e.proposal(e.propose(e.alice, "Rename the order entity."), 201)
			if p.Status != ai.ProposalReady {
				t.Fatalf("proposal %s/%s", p.Status, p.FailureCode)
			}
			var model string
			if err := e.f.operator.QueryRow(`SELECT model FROM control.ai_invocations WHERE tenant_id=$1 AND proposal_id=$2 AND attempt=1`, e.f.tenantA, p.ID).Scan(&model); err != nil || model != want {
				t.Fatalf("stored model label %q (%v), want %q", model, err, want)
			}
			states := e.auditStates("ai.proposal.created")
			if len(states) != 1 || states[0]["ai_model"] != want {
				t.Fatalf("audit model label %v, want %q", states, want)
			}
			// The slot is free again: with one allowed in flight, the next honest request is served.
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Again"), Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}, Model: "m"})
			e.proposal(e.propose(e.alice, "Rename again."), 201)
		})
	}
}

// M14R-02: a provider call that the client cancels was neither traced nor charged, so a member could drive provider calls the daily
// budget never saw. A canceled call is charged like a crash the sweep would find: the input estimate and the output that was reserved.
func TestM14R02CanceledProviderCallsAreChargedToTheDailyBudget(t *testing.T) {
	const cycles = 16
	limits := ai.ServiceLimits{InFlightPerTenant: 1, DailyTokens: 40000, MaxAttempts: 1, MaxOutputTokens: 1000}

	e := m14Setup(t, m14Options{limits: limits})
	doc := e.draft("500")
	for i := 0; i < cycles+2; i++ {
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Slow"), Delay: 5 * time.Second, Usage: ai.Usage{InputTokens: 3000, OutputTokens: 800}, Model: "m"})
	}
	refused := 0
	for i := 0; i < cycles; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest("POST", e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Rename the order entity."}`)).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+e.alice)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		done := make(chan struct{})
		before := e.provider.Calls()
		go func() { e.h.ServeHTTP(w, req); close(done) }()
		started := false
		for deadline := time.Now().Add(10 * time.Second); !started && time.Now().Before(deadline); {
			select {
			case <-done: // refused before the provider was called
				started = true
				refused++
				if w.Code != 429 || m14Code(w) != "ai_budget_exhausted" {
					t.Fatalf("cycle %d: refused with %d %s", i, w.Code, w.Body.String())
				}
			default:
				if e.provider.Calls() > before {
					started = true
					cancel() // the client leaves while the provider works
					<-done
				} else {
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
		cancel()
		if !started {
			t.Fatalf("cycle %d: neither a provider call nor an answer within 10 s", i)
		}
	}
	calls := e.provider.Calls()
	requests, used := e.m14rUsage()
	var timeouts int
	var minIn, minOut int64
	if err := e.f.operator.QueryRow(`SELECT count(*),coalesce(min(input_tokens),0),coalesce(min(output_tokens),0) FROM control.ai_invocations WHERE tenant_id=$1 AND outcome='TIMEOUT'`, e.f.tenantA).Scan(&timeouts, &minIn, &minOut); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d canceled cycles: %d provider calls, %d refused by the budget, usage requests=%d used_tokens=%d, TIMEOUT attempts=%d (min %d in, %d out)", cycles, calls, refused, requests, used, timeouts, minIn, minOut)
	if calls+refused != cycles || refused == 0 {
		t.Fatalf("%d calls and %d refusals for %d cycles: the budget never ran out, so canceled calls are not charged", calls, refused, cycles)
	}
	if timeouts != calls || minIn == 0 || minOut != int64(limits.MaxOutputTokens) {
		t.Fatalf("%d TIMEOUT attempts for %d calls; smallest charge %d in, %d out (the reserved output is %d)", timeouts, calls, minIn, minOut, limits.MaxOutputTokens)
	}
	if used < int64(calls)*(minIn+minOut) {
		t.Fatalf("used_tokens %d is below the charge of %d canceled calls", used, calls)
	}

	// Control: the same budget spent honestly allows about as many requests as the canceled ones got through.
	e2 := m14Setup(t, m14Options{limits: limits})
	doc2 := e2.draft("500")
	completed := 0
	for i := 0; i < cycles; i++ {
		e2.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc2, "entity.order", "Fast"), Usage: ai.Usage{InputTokens: 3000, OutputTokens: 800}, Model: "m"})
		if w := e2.propose(e2.alice, "Rename the order entity."); w.Code == 201 {
			completed++
		}
	}
	t.Logf("control, the same budget spent on completed requests: %d accepted", completed)
	if calls > completed+1 {
		t.Fatalf("%d canceled provider calls got through a budget that allows %d completed ones", calls, completed)
	}
}

// M14R-03, results: six admissions 250 ms apart (no two overlap) whose scripted answers arrive within a few milliseconds, so only the
// result exchanges run at once. Each updates the tenant's usage row under repeatable read and the losers aborted with 40001, which lost
// a paid answer (3 of 6 per round). The result exchange now repeats after a known abort.
func TestM14R03ResultsThatFinishTogetherAreNotLost(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 4 s per round")
	}
	for round := 1; round <= 2; round++ {
		const n = 6
		const spacing = 250 * time.Millisecond
		e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: n, DailyTokens: 50_000_000}})
		doc := e.draft("500")
		for i := 0; i < n; i++ {
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", fmt.Sprintf("Label %d", i)), Delay: 3*time.Second - time.Duration(i)*spacing, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
		}
		var wg sync.WaitGroup
		codes := make([]string, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				w := e.propose(e.alice, "Rename the order entity.")
				codes[i] = fmt.Sprintf("%d %s", w.Code, m14Code(w))
			}(i)
			time.Sleep(spacing)
		}
		wg.Wait()
		requests, used := e.m14rUsage()
		states := e.m14rProposals()
		t.Logf("round %d: answers=%v proposals=%v usage requests=%d used_tokens=%d", round, codes, states, requests, used)
		if countOf(states, "READY/") != n || countOf(codes, "201 ") != n {
			t.Fatalf("round %d: %d of %d paid answers were lost: answers=%v proposals=%v", round, n-countOf(states, "READY/"), n, codes, states)
		}
	}
}

// M14R-03, admissions: eight creations released at the same instant contest the usage row of their tenant. Admission stores and
// charges nothing before it commits, so a creation that is still refused after its tries is cheap and retryable; what must never
// happen is a paid answer lost, or a proposal left PENDING (it would hold its slot until the lease and be charged the reservation).
func TestM14R03CreationsRacingForTheUsageRowLoseNoPaidAnswer(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 3 s per round")
	}
	for round := 1; round <= 3; round++ {
		const n = 8
		e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: n, DailyTokens: 50_000_000}})
		doc := e.draft("500")
		for i := 0; i < n; i++ {
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", fmt.Sprintf("Label %d", i)), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make([]string, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				w := e.propose(e.alice, "Rename the order entity.")
				codes[i] = fmt.Sprintf("%d %s", w.Code, m14Code(w))
			}(i)
		}
		close(start)
		wg.Wait()
		states := e.m14rProposals()
		ready := countOf(states, "READY/")
		created := countOf(codes, "201 ")
		t.Logf("round %d: answers=%v proposals=%v provider calls=%d", round, codes, states, e.provider.Calls())
		if pending := countOf(states, "PENDING/"); pending != 0 {
			t.Fatalf("round %d: %d proposals left PENDING (answers %v)", round, pending, codes)
		}
		if ready != created || e.provider.Calls() != ready {
			t.Fatalf("round %d: %d provider calls, %d READY proposals, %d answers 201: a paid answer was lost", round, e.provider.Calls(), ready, created)
		}
		for _, c := range codes {
			if !strings.HasPrefix(c, "201 ") && c != "409 ai_conflict" {
				t.Fatalf("round %d: answer %q is neither a stored result nor the conflict of an admission that stored nothing", round, c)
			}
		}
		if created < n/2 {
			t.Fatalf("round %d: only %d of %d racing creations were admitted after their tries: %v", round, created, n, codes)
		}
	}
}

// m14rHangProvider ignores its context on purpose, as an adapter without a deadline would.
type m14rHangProvider struct{ release chan struct{} }

func (h *m14rHangProvider) Complete(context.Context, ai.ProviderRequest) (ai.ProviderResponse, error) {
	<-h.release
	return ai.ProviderResponse{}, fmt.Errorf("released")
}

// M14R-07: the request goroutine was blocked in the adapter and answered nothing after the wall clock.
func TestM14R07AnAdapterThatIgnoresItsContextCannotHoldTheRequest(t *testing.T) {
	hang := &m14rHangProvider{release: make(chan struct{})}
	t.Cleanup(func() { close(hang.release) })
	e := m14Setup(t, m14Options{gateway: ai.NewGateway(hang, ai.Options{}), limits: ai.ServiceLimits{InFlightPerTenant: 1, WallClock: 2 * time.Second}})
	e.draft("500")
	type result struct {
		code int
		body string
	}
	done := make(chan result, 1)
	started := time.Now()
	go func() {
		w := e.propose(e.alice, "Rename the order entity.")
		done <- result{w.Code, w.Body.String()}
	}()
	select {
	case r := <-done:
		t.Logf("answered HTTP %d after %v", r.code, time.Since(started).Round(time.Millisecond))
		if r.code != 503 || !strings.Contains(r.body, "ai_unavailable") {
			t.Fatalf("HTTP %d: %s", r.code, r.body)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no answer 10 s after a 2 s wall clock: the request is held by an adapter that ignores its context")
	}
	if got := e.m14rProposals(); len(got) != 1 || got[0] != "FAILED/TIMEOUT" {
		t.Fatalf("proposals %v, want one FAILED/TIMEOUT", got)
	}
	// The slot is free: with one allowed in flight, the next request is admitted (it fails the same way, but it is not 429 ai_busy).
	next := make(chan int, 1)
	go func() { next <- e.propose(e.alice, "Rename again.").Code }()
	select {
	case code := <-next:
		if code == 429 {
			t.Fatal("the in-flight slot of the abandoned call was not released")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second request was not answered either")
	}
}

// M14R-06: one answer that reported 100 million tokens charged them all and locked the tenant out of the UTC day.
func TestM14R06AnEnormousReportedUsageDoesNotLockTheTenantOut(t *testing.T) {
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Greedy"), Usage: ai.Usage{InputTokens: -1 << 62, OutputTokens: 1 << 62}, Model: "m"})
	p := e.proposal(e.propose(e.alice, "Rename the order entity."), 201)
	_, used := e.m14rUsage()
	t.Logf("a report of %d output tokens was charged %d in all (daily budget %d)", int64(1)<<62, used, ai.DefaultDailyToken)
	if p.Status != ai.ProposalReady || used <= 0 || used > ai.DefaultDailyToken/4 {
		t.Fatalf("proposal %s, used_tokens %d: the charge must stay at the order of the request, a quarter of the day at most", p.Status, used)
	}
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Next"), Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}, Model: "m"})
	if w := e.propose(e.alice, "Rename again."); w.Code != 201 {
		t.Fatalf("the next honest request: HTTP %d %s", w.Code, w.Body.String())
	}
}

// M14R-03, the residual that its fixes name: the result exchange authenticates again, so a bearer token that lapsed during the provider phase lost a
// paid answer and left the proposal PENDING until the sweep charged the whole reservation (the reviewer: 401 after the provider answered,
// PENDING, the next request 429 ai_busy). A creation whose token ends before its run can finish (the wall clock plus the result margin) is now
// refused before anything is reserved or sent to the provider.
func TestM14R03ATokenThatEndsBeforeTheRunCanFinishIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	e := m14Setup(t, m14Options{limits: ai.ServiceLimits{WallClock: 30 * time.Second, InFlightPerTenant: 1}}) // the run needs 30 s + 10 s
	doc := e.draft("500")
	expiring := func(left time.Duration) string {
		return e.f.token(t, e.f.aliceSubject, map[string]any{"exp": time.Now().Add(left).Unix()})
	}
	before := e.provider.Calls()
	for _, left := range []time.Duration{5 * time.Second, 25 * time.Second, 36 * time.Second} {
		w := e.propose(expiring(left), "Rename the order entity.")
		if w.Code != 401 || m14Code(w) != "token_expiring" || w.Header().Get("WWW-Authenticate") == "" {
			t.Fatalf("a token with %v left (the run needs 40 s): HTTP %d %s %q", left, w.Code, m14Code(w), w.Body.String())
		}
	}
	if calls := e.provider.Calls() - before; calls != 0 {
		t.Fatalf("%d provider calls for requests that were refused", calls)
	}
	if requests, used := e.m14rUsage(); requests != 0 || used != 0 {
		t.Fatalf("the refused requests used %d requests and %d tokens", requests, used)
	}
	if got := e.m14rProposals(); len(got) != 0 {
		t.Fatalf("proposals left by refused requests: %v", got)
	}
	// The refusal is an audited rejection of the creation, not an authentication failure.
	if states := e.auditStates("ai.propose.rejected"); len(states) != 3 {
		t.Fatalf("%d rejection audit rows, want 3", len(states))
	}
	// A token that lasts through the run is served, and so is the same member with a fresh token right after the refusals.
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Renamed"), Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: "m"})
	if p := e.proposal(e.propose(expiring(120*time.Second), "Rename the order entity."), 201); p.Status != ai.ProposalReady {
		t.Fatalf("proposal %s/%s", p.Status, p.FailureCode)
	}
	if got := e.m14rProposals(); len(got) != 1 {
		t.Fatalf("proposals %v, want the one that was served", got)
	}
}
