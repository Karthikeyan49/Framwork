//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// ------------------------------------------------------- pipeline outcomes over HTTP

// brokenPatch is a patch that decodes strictly but does not compile: a page whose
// entity does not exist.
func brokenPatch(t *testing.T, doc metadata.Document, label string) []byte {
	t.Helper()
	d := metadata.Definition{ID: "page.ghost", SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: label, Kind: "page",
		Page: &metadata.Page{EntityRef: "entity.ghost", Components: []metadata.Component{{Type: "section", Label: "S", Children: []metadata.Component{{Type: "table", EntityRef: "entity.ghost"}}}}}}
	return patchPayload(t, doc, func(p *ai.PatchProposal) {
		p.Operations = []ai.PatchOperation{{Op: "add", Target: "page.ghost", Definition: &d, Rationale: "add a page"}}
	})
}

func TestM14APINeedsHumanAndFailedAreStoredResultsNotErrors(t *testing.T) {
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")
	for i := 0; i < 3; i++ {
		e.provider.Push(step(brokenPatch(t, doc, fmt.Sprintf("Ghost %d", i))))
	}
	needs := e.proposal(e.propose(e.alice, "Add a page for the ghost entity."), 201)
	if needs.Status != ai.ProposalNeedsHuman || needs.Attempts != 3 || len(needs.Diagnostics) == 0 || len(needs.Proposal) == 0 || needs.FailureCode != "" {
		t.Fatalf("NEEDS_HUMAN is a normal result with diagnostics: %+v", needs)
	}
	var diags []struct{ Path, Code, Message string }
	if json.Unmarshal(needs.Diagnostics, &diags) != nil || len(diags) == 0 || !strings.HasPrefix(diags[0].Path, "/") {
		t.Fatalf("diagnostics %s", needs.Diagnostics)
	}
	if e.provider.Calls() != 3 {
		t.Fatalf("exactly the attempt ceiling: %d calls", e.provider.Calls())
	}
	if w := e.apply(e.alice, needs.ID, 1, nil); w.Code != 409 || m14Code(w) != "proposal_not_ready" {
		t.Fatalf("a NEEDS_HUMAN proposal cannot be applied: %d %s", w.Code, w.Body.String())
	}
	if w := e.reject(e.alice, needs.ID, "NOT_WANTED"); w.Code != 409 || m14Code(w) != "proposal_not_ready" {
		t.Fatalf("nor rejected (it is terminal): %d", w.Code)
	}

	// Garbage output: FAILED with the outcome of the last attempt, still 201.
	e.provider.Push(step([]byte("I am sorry, I cannot do that.")), step([]byte("{")), step([]byte("")))
	failed := e.proposal(e.propose(e.alice, "Do something."), 201)
	if failed.Status != ai.ProposalFailed || failed.FailureCode != "PARSE_ERROR" || len(failed.Proposal) != 0 || failed.Attempts != 3 {
		t.Fatalf("failed %+v", failed)
	}

	// A hostile structured answer (a tool target) is a policy failure, stored as such.
	tool := patchPayloadRaw(map[string]any{"op": "delete", "target": "tool.crm", "expected_hash": strings.Repeat("0", 64), "rationale": "x"})
	e.provider.Push(step(tool), step(tool), step(tool))
	forbidden := e.proposal(e.propose(e.alice, "Remove the CRM tool."), 201)
	if forbidden.Status != ai.ProposalFailed || forbidden.FailureCode != "FORBIDDEN_TARGET" {
		t.Fatalf("forbidden %+v", forbidden)
	}

	// The provider fails: the proposal is stored FAILED and the answer is 503 naming it.
	e.provider.Push(ai.ScriptStep{Err: ai.ErrProviderRefused})
	w := e.propose(e.alice, "Anything.")
	var body struct {
		Code       string `json:"code"`
		ProposalID string `json:"proposal_id"`
	}
	if w.Code != 503 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != "ai_unavailable" || body.ProposalID == "" {
		t.Fatalf("provider failure: %d %s", w.Code, w.Body.String())
	}
	if p := e.proposal(e.ai("GET", "/proposals/"+body.ProposalID, e.alice, nil), 200); p.Status != ai.ProposalFailed || p.FailureCode != "PROVIDER_ERROR" {
		t.Fatalf("stored %+v", p)
	}
	// A timeout is the same class.
	e.provider.Push(ai.ScriptStep{Err: ai.ErrProviderTimeout})
	if w = e.propose(e.alice, "Anything."); w.Code != 503 || !strings.Contains(w.Body.String(), "proposal_id") {
		t.Fatalf("timeout: %d %s", w.Code, w.Body.String())
	}

	if rev, _ := e.heads(); rev != 1 {
		t.Fatal("no outcome of the pipeline may touch the draft")
	}
	var trace int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.ai_invocations WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&trace); err != nil || trace < 9 {
		t.Fatalf("one trace row per provider attempt: %d %v", trace, err)
	}
}

func patchPayloadRaw(ops ...map[string]any) []byte {
	list := make([]any, len(ops))
	for i, o := range ops {
		list[i] = o
	}
	raw, _ := json.Marshal(map[string]any{"version": ai.PatchProposalVersion, "operations": list, "assumptions": []any{}, "unresolved_requirements": []any{}, "proposed_tests": []any{}, "risk_flags": []any{}})
	return raw
}

func TestM14APITransientProviderFailureIsRetriedOnce(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	e.provider.Push(ai.ScriptStep{Err: ai.ErrProviderUnavailable}, e.answerFor(1, func(doc metadata.Document) []byte { return labelPatch(t, doc, "entity.order", "Retried") }))
	p := e.proposal(e.propose(e.alice, "Rename the order entity."), 201)
	if p.Status != ai.ProposalReady || p.Attempts != 2 || e.provider.Calls() != 2 {
		t.Fatalf("%+v calls %d", p, e.provider.Calls())
	}
}

func TestM14APIAdmissionBudgetsAndLimits(t *testing.T) {
	t.Run("daily budget smaller than one reservation", func(t *testing.T) {
		e := m14Setup(t, m14Options{limits: ai.ServiceLimits{DailyTokens: 1000}})
		e.draft("500")
		w := e.propose(e.alice, "Rename the order entity.")
		if w.Code != 429 || m14Code(w) != "ai_budget_exhausted" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if e.proposalRows() != 0 || e.provider.Calls() != 0 {
			t.Fatal("a refused admission stores nothing and calls nothing")
		}
	})
	t.Run("usage accumulates until the budget is spent", func(t *testing.T) {
		e := m14Setup(t, m14Options{limits: ai.ServiceLimits{DailyTokens: 100_000}})
		e.draft("500")
		var codes []int
		for i := 0; i < 6; i++ {
			e.provider.Push(ai.ScriptStep{Payload: []byte("not json"), Usage: ai.Usage{InputTokens: 2000, OutputTokens: 500}})
			e.provider.Push(ai.ScriptStep{Payload: []byte("not json"), Usage: ai.Usage{InputTokens: 2000, OutputTokens: 500}})
			e.provider.Push(ai.ScriptStep{Payload: []byte("not json"), Usage: ai.Usage{InputTokens: 2000, OutputTokens: 500}})
			codes = append(codes, e.propose(e.alice, "Anything at all.").Code)
		}
		if codes[0] != 201 || codes[len(codes)-1] != 429 {
			t.Fatalf("status sequence %v: spend must eventually refuse", codes)
		}
		var used, requests int64
		if err := e.f.operator.QueryRow(`SELECT used_tokens,requests FROM control.ai_usage WHERE tenant_id=$1 AND usage_day=(clock_timestamp() AT TIME ZONE 'UTC')::date`, e.f.tenantA).Scan(&used, &requests); err != nil || used < 15_000 || requests < 2 {
			t.Fatalf("usage %d tokens, %d requests (%v)", used, requests, err)
		}
		if used > 100_000 || requests < 3 {
			t.Fatalf("the budget was exceeded or too few requests were admitted: %d tokens, %d requests", used, requests)
		}
	})
	t.Run("a run that spends its reservation ends BUDGET and answers 429", func(t *testing.T) {
		e := m14Setup(t, m14Options{})
		doc := e.draft("500")
		// An answer that reports far more than a call can cost is charged at its cap (about twice the reservation of one attempt: the
		// input at twice its estimate, the output at twice the 8192 tokens asked for), not at the figure it reports (review M14R-06,
		// F-08). One such answer therefore does not spend the reservation of a run, which allows three attempts; two of them do.
		for i := 0; i < 3; i++ {
			e.provider.Push(ai.ScriptStep{Payload: brokenPatch(t, doc, "Ghost"), Usage: ai.Usage{InputTokens: 50_000_000, OutputTokens: 50_000_000}})
		}
		w := e.propose(e.alice, "Add a page.")
		var body struct {
			Code       string `json:"code"`
			ProposalID string `json:"proposal_id"`
		}
		if w.Code != 429 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != "ai_budget_exhausted" || body.ProposalID == "" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		p := e.proposal(e.ai("GET", "/proposals/"+body.ProposalID, e.alice, nil), 200)
		if p.Status != ai.ProposalFailed || p.FailureCode != "BUDGET" || p.Attempts < 1 || p.Attempts >= ai.MaxAttempts || e.provider.Calls() != p.Attempts {
			t.Fatalf("%+v calls %d: the run must end BUDGET before its last attempt, every attempt a provider call", p, e.provider.Calls())
		}
		// The charge is recorded, at the cap of each attempt, and it is what the tenant's day shows.
		var sum, outputs, used int64
		if err := e.f.operator.QueryRow(`SELECT coalesce(sum(input_tokens+output_tokens),0),coalesce(min(output_tokens),0) FROM control.ai_invocations WHERE tenant_id=$1 AND proposal_id=$2`, e.f.tenantA, body.ProposalID).Scan(&sum, &outputs); err != nil {
			t.Fatal(err)
		}
		if err := e.f.operator.QueryRow(`SELECT used_tokens FROM control.ai_usage WHERE tenant_id=$1 AND usage_day=(clock_timestamp() AT TIME ZONE 'UTC')::date`, e.f.tenantA).Scan(&used); err != nil {
			t.Fatal(err)
		}
		if want := int64(2*ai.DefaultMaxOutputTokens + 1024); outputs != want || used != sum || used > 100_000 {
			t.Fatalf("each attempt is charged %d output tokens (want the cap %d); the day shows %d tokens for attempts worth %d", outputs, want, used, sum)
		}
		// One answer that reported 100 million tokens no longer locks the tenant out of the day: the next request is admitted (the
		// script is spent, so the provider fails it, which is not a budget refusal).
		if w = e.propose(e.alice, "Again."); w.Code == 429 {
			t.Fatalf("a reported figure locked the day: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("in-flight limit under real concurrency", func(t *testing.T) {
		e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
		doc := e.draft("500")
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Slow"), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}})
		var wg sync.WaitGroup
		results := make([]int, 4)
		wg.Add(1)
		go func() { defer wg.Done(); results[0] = e.propose(e.alice, "Slow request.").Code }()
		time.Sleep(500 * time.Millisecond) // the first request is inside its provider phase
		for i := 1; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results[i] = e.propose(e.alice, "Concurrent request.").Code }()
		}
		wg.Wait()
		busy := 0
		for _, c := range results[1:] {
			if c == 429 {
				busy++
			}
		}
		if results[0] != 201 || busy != 3 {
			t.Fatalf("results %v: the first must finish and the others be refused as busy", results)
		}
		w := e.propose(e.alice, "after")
		if w.Code == 429 {
			t.Fatalf("the slot must be free again: %d %s", w.Code, w.Body.String())
		}
	})
	t.Run("a stale PENDING row expires and frees its slot", func(t *testing.T) {
		e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
		e.draft("500")
		p := &portsEnv{m12Env: e.m12Env}
		var id string
		revision, hash := e.headAndHash()
		if err := p.call(e.f.aliceSubject, e.f.tenantA, &id, `SELECT control.ai_begin($1,'patch',$2,$3,$4,200000,10000,1,1)::text`, e.app, strings.Repeat("a", 64), revision, hash); err != nil {
			t.Fatal(err)
		}
		// The slot is taken while the lease runs ...
		if w := e.propose(e.alice, "Blocked by the pending row."); w.Code != 429 || m14Code(w) != "ai_busy" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		time.Sleep(1300 * time.Millisecond)
		// ... a read already reports the lapsed row as EXPIRED ...
		if got := e.proposal(e.ai("GET", "/proposals/"+id, e.alice, nil), 200); got.Status != ai.ProposalExpired || got.FailureCode != "EXPIRED" {
			t.Fatalf("lapsed row reported %s/%s", got.Status, got.FailureCode)
		}
		// ... and the next admission sweeps it, charging its reservation.
		e.provider.Push(e.answerFor(1, func(doc metadata.Document) []byte { return labelPatch(t, doc, "entity.order", "After expiry") }))
		if q := e.proposal(e.propose(e.alice, "Rename."), 201); q.Status != ai.ProposalReady {
			t.Fatal(q.Status)
		}
		var status string
		var used int64
		if err := e.f.operator.QueryRow(`SELECT status FROM control.ai_proposals WHERE tenant_id=$1 AND id=$2`, e.f.tenantA, id).Scan(&status); err != nil || status != "EXPIRED" {
			t.Fatalf("stored status %s (%v)", status, err)
		}
		if err := e.f.operator.QueryRow(`SELECT used_tokens FROM control.ai_usage WHERE tenant_id=$1 AND usage_day=(clock_timestamp() AT TIME ZONE 'UTC')::date`, e.f.tenantA).Scan(&used); err != nil || used < 10_000 {
			t.Fatalf("the abandoned reservation is charged pessimistically: %d (%v)", used, err)
		}
	})
}

// headAndHash reads the draft head and its source hash.
func (e *m14Env) headAndHash() (int64, string) {
	e.t.Helper()
	var revision int64
	var hash string
	if err := e.f.operator.QueryRow(`SELECT revision,source_hash FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 ORDER BY revision DESC LIMIT 1`, e.f.tenantA, e.app).Scan(&revision, &hash); err != nil {
		e.t.Fatal(err)
	}
	return revision, hash
}

func TestM14APIContextTooLargeStoresAndChargesNothing(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	// A draft whose description cannot fit the bundle: many long-labelled entities.
	doc := orderDocument(e.app, "500")
	for i := 0; i < 160; i++ {
		id := fmt.Sprintf("entity.bulk%03d", i)
		field := fmt.Sprintf("field.bulk%03d.id", i)
		doc.Definitions = append(doc.Definitions,
			metadata.Definition{ID: id, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: strings.Repeat("Bulk entity ", 15), Kind: "entity", Entity: &metadata.Entity{Fields: []string{field}}},
			metadata.Definition{ID: field, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: strings.Repeat("Bulk field ", 15), Kind: "field", Field: &metadata.Field{EntityRef: id, Type: "string"}})
	}
	raw, _ := json.Marshal(doc)
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, 1, raw); w.Code != 201 {
		t.Fatal("register the large draft", w.Code, w.Body.String())
	}
	var before int64
	_ = e.f.operator.QueryRow(`SELECT coalesce(sum(used_tokens),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&before)
	w := e.propose(e.alice, "Rename the order entity.")
	if w.Code != 422 || m14Code(w) != "ai_context_too_large" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var after, requests int64
	_ = e.f.operator.QueryRow(`SELECT coalesce(sum(used_tokens),0),coalesce(sum(requests),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&after, &requests)
	if e.proposalRows() != 0 || e.provider.Calls() != 0 || after != before || requests != 0 {
		t.Fatalf("a refused context left rows=%d calls=%d tokens %d->%d requests=%d", e.proposalRows(), e.provider.Calls(), before, after, requests)
	}
}

func TestM14APIProposeAgainstTheWrongDraft(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	if w := e.ai("POST", "/patch-proposals", e.alice, map[string]any{"request": "x", "expected_revision": 7}); w.Code != 409 || m14Code(w) != "draft_changed" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if e.proposalRows() != 0 {
		t.Fatal("a refused admission stores nothing")
	}
	// An application without a draft is not found.
	other := "/v1/tenants/" + e.f.tenantA + "/preview/app.nothing_here/ai/patch-proposals"
	if w := m5Call(e.h, other, e.alice, map[string]any{"request": "x"}); w.Code != 404 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	// An application id that is not an application id is refused by the envelope.
	bad := "/v1/tenants/" + e.f.tenantA + "/preview/not_an_app/ai/patch-proposals"
	if w := m5Call(e.h, bad, e.alice, map[string]any{"request": "x"}); w.Code != 400 && w.Code != 413 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
