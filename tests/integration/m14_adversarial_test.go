//go:build integration

package integration

// End-to-end attacks on the AI builder (ADR104 section 13) through the real routes,
// the real database and the real gateway. The model is a ScriptedProvider replaying the
// adversarial corpus (internal/ai/testdata/corpus); no real model exists here. This is
// builder-side adversarial evidence, not an independent review.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/ai/corpus"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
)

// switchProvider lets one long-lived service replay a different script per corpus item.
type switchProvider struct {
	cur atomic.Pointer[ai.ScriptedProvider]
}

func (s *switchProvider) Complete(ctx context.Context, req ai.ProviderRequest) (ai.ProviderResponse, error) {
	p := s.cur.Load()
	if p == nil {
		return ai.ProviderResponse{}, ai.ErrProviderRefused
	}
	return p.Complete(ctx, req)
}

func (s *switchProvider) calls() int {
	if p := s.cur.Load(); p != nil {
		return p.Calls()
	}
	return 0
}

// scriptOf pads a short script (a provider scripted once keeps answering the same).
func scriptOf(c corpus.Case) []ai.ScriptStep {
	payloads := append([]corpus.ExpandedStep(nil), c.Payloads...)
	for len(payloads) > 0 && len(payloads) < ai.MaxAttempts {
		payloads = append(payloads, payloads[len(payloads)-1])
	}
	steps := make([]ai.ScriptStep, 0, len(payloads))
	for _, p := range payloads {
		s := ai.ScriptStep{Payload: p.Payload, Usage: ai.Usage{InputTokens: p.Step.Usage.InputTokens, OutputTokens: p.Step.Usage.OutputTokens}, Model: p.Step.Model, Delay: time.Duration(p.Step.DelayMS) * time.Millisecond}
		switch p.Step.Error {
		case "refused":
			s.Err = ai.ErrProviderRefused
		case "unavailable":
			s.Err = ai.ErrProviderUnavailable
		case "timeout":
			s.Err = ai.ErrProviderTimeout
		case "malformed":
			s.Err = ai.ErrProviderMalformed
		case "other":
			s.Err = fmt.Errorf("unclassified adapter failure")
		case "panic":
			s.Func = func(ai.ProviderRequest) ([]byte, error) { panic("adapter bug") }
		}
		steps = append(steps, s)
	}
	return steps
}

type membershipRow struct{ User, Role string }

func (e *m14Env) memberships() []membershipRow {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT user_id::text,role FROM iam.memberships WHERE tenant_id=$1 AND active ORDER BY user_id`, e.f.tenantA)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []membershipRow
	for rows.Next() {
		var r membershipRow
		if err = rows.Scan(&r.User, &r.Role); err != nil {
			e.t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// TestM14AdversarialCorpusEndToEnd drives every corpus item tagged e2e through the
// real routes: propose, then the human decision. It checks the item's declared
// property on what the API and the database show, and that nothing was applied without
// a human apply call, no authority surface was touched and the memberships are intact.
func TestM14AdversarialCorpusEndToEnd(t *testing.T) {
	lib, err := corpus.Load("../../internal/ai/testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	vendor, err := corpus.VendorCatalog()
	if err != nil {
		t.Fatal(err)
	}
	sw := &switchProvider{}
	e := m14Setup(t, m14Options{gateway: ai.NewGateway(sw, ai.Options{}), limits: ai.ServiceLimits{DailyTokens: 100_000_000, InFlightPerTenant: 8}, catalog: &vendor})
	surface, members := e.authoritySnapshot(), e.memberships()

	var ran, skipped int
	notes := map[string][]string{}
	for n, it := range lib.Items {
		if !it.Tagged("e2e") {
			continue
		}
		c, err := lib.Expand(it)
		if err != nil {
			t.Fatal(err)
		}
		if c.Item.Budget != nil {
			skipped++ // the budget of an HTTP request is the server's, not the item's
			continue
		}
		app := fmt.Sprintf("app.e2e_%03d", n)
		draft := c.Draft
		draft.AppID = app
		raw, _ := json.Marshal(draft)
		if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+app+"/versions", e.alice, 0, raw); w.Code != 201 {
			skipped++ // a draft the registry itself refuses (a NUL label) cannot be proposed against
			continue
		}
		ran++
		sw.cur.Store(ai.NewScriptedProvider(scriptOf(c)...))
		root := "/v1/tenants/" + e.f.tenantA + "/preview/" + app + "/ai"
		route := "/patch-proposals"
		if c.Item.Task == "requirement_capture" {
			route = "/requirement-proposals"
		}
		started := time.Now()
		w := m14Do(e.h, "POST", root+route, e.alice, map[string]any{"request": c.Request})
		res := corpus.Result{Elapsed: time.Since(started), NoOracle: true}
		var created struct {
			ai.Proposal
			ProposalID string `json:"proposal_id"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &created)
		id := created.ID
		if id == "" {
			id = created.ProposalID
		}
		switch {
		case w.Code == 400 && m14Code(w) == "invalid_ai_request", w.Code == 413 && m14Code(w) == "body_too_large":
			res.ContextErr = "invalid" // refused at the API before any model call
		case w.Code == 422 && m14Code(w) == "ai_context_too_large":
			res.ContextErr = "too_large"
		case id == "":
			t.Errorf("%s: unexpected answer %d %s", it.ID, w.Code, w.Body.String())
			continue
		}
		fail := func(format string, a ...any) { t.Errorf("%s: "+format, append([]any{it.ID}, a...)...) }
		applyAt := func(rev int64, ack []string) *httptest.ResponseRecorder {
			if ack == nil {
				ack = []string{}
			}
			return m14Do(e.h, "POST", root+"/proposals/"+id+"/apply", e.alice, map[string]any{"expected_revision": rev, "acknowledge": ack})
		}
		// Nothing is applied by proposing, whatever the outcome.
		if head := e.headOf(app); head != 1 {
			fail("proposing moved the draft to revision %d", head)
		}
		if id != "" {
			p := e.proposal(m14Do(e.h, "GET", root+"/proposals/"+id, e.alice, nil), 200)
			res.Status = string(p.Status)
			res.Tokens = p.Usage.InputTokens + p.Usage.OutputTokens
			res.Canonical = string(p.Proposal)
			rows, qerr := e.f.operator.Query(`SELECT outcome FROM control.ai_invocations WHERE tenant_id=$1 AND proposal_id=$2 ORDER BY attempt`, e.f.tenantA, id)
			if qerr != nil {
				t.Fatal(qerr)
			}
			for rows.Next() {
				var o string
				_ = rows.Scan(&o)
				res.Outcomes = append(res.Outcomes, o)
			}
			rows.Close()
			res.Calls = sw.calls()
			if p.FailureCode != "" {
				res.DiagCodes = nil
			}
			var diags []struct{ Code string }
			_ = json.Unmarshal(p.Diagnostics, &diags)
			for _, d := range diags {
				res.DiagCodes = append(res.DiagCodes, strings.ToLower(d.Code))
			}
			if p.Status == ai.ProposalFailed && p.FailureCode == "BUDGET" {
				res.Status = "BUDGET_EXCEEDED"
			}
			switch {
			case p.Status == ai.ProposalReady && p.Kind == ai.ProposalPatch:
				var derived ai.Derived
				_ = json.Unmarshal(p.Derived, &derived)
				res.Sensitive = derived.Sensitive
				if len(derived.Sensitive) > 0 {
					if w := applyAt(1, nil); w.Code != 409 || m14Code(w) != "acknowledge_mismatch" {
						fail("a sensitive proposal applied without its acknowledgement: %d %s", w.Code, w.Body.String())
					}
					if head := e.headOf(app); head != 1 {
						fail("a refused apply moved the draft")
					}
				}
				if w := applyAt(1, derived.Sensitive); w.Code != 200 {
					fail("apply with the derived acknowledgement: %d %s", w.Code, w.Body.String())
				} else {
					res.Document = true
					if head := e.headOf(app); head != 2 {
						fail("applied, but the head is %d", head)
					}
					if cw := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/metadata/"+app+"/versions/2/compile", e.alice, 0, nil); cw.Code != 200 {
						fail("the applied draft does not compile: %d", cw.Code)
					}
					if again := applyAt(2, derived.Sensitive); again.Code != 409 {
						fail("a second apply answered %d", again.Code)
					}
				}
			case p.Status == ai.ProposalReady && p.Kind == ai.ProposalRequirements:
				var preview struct {
					Requirements []struct {
						ID string `json:"id"`
					} `json:"requirements"`
				}
				_ = json.Unmarshal(p.Derived, &preview)
				decisions := map[string]string{}
				for _, r := range preview.Requirements {
					decisions[r.ID] = "RESOLVE"
				}
				cw := m14Do(e.h, "POST", root+"/proposals/"+id+"/confirm", e.alice, map[string]any{"expected_requirements_version": 0, "decisions": decisionList(decisions)})
				if cw.Code != 200 && cw.Code != 409 {
					fail("confirm answered %d %s", cw.Code, cw.Body.String())
				}
				if cw.Code == 200 && !strings.Contains(cw.Body.String(), intent.ProvenanceAIConfirmed) {
					fail("a confirmed requirement lacks the AI provenance")
				}
			default:
				if aw := applyAt(1, nil); aw.Code != 409 && aw.Code != 404 {
					fail("a %s proposal answered apply with %d", p.Status, aw.Code)
				}
			}
		}
		rep := c.Check(res, corpus.Wrapper{Begin: "<untrusted>", End: "</untrusted>"})
		for _, f := range rep.Failures {
			sev := it.Expect.Severity
			if sev == "" {
				sev = "HIGH"
			}
			t.Errorf("[%s] %s: %s (status %s, outcomes %v, calls %d)", sev, it.ID, f, res.Status, res.Outcomes, res.Calls)
		}
		if len(rep.Notes) > 0 {
			notes[it.ID] = rep.Notes
		}
	}
	if ran < 55 {
		t.Fatalf("only %d e2e items ran (%d skipped)", ran, skipped)
	}
	e.assertNoAuthoritySurface(surface)
	if !slices.Equal(members, e.memberships()) {
		t.Errorf("memberships changed during AI-only activity:\n%v\n%v", members, e.memberships())
	}
	keys := make([]string, 0, len(notes))
	for k := range notes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("note %s: %s", k, strings.Join(notes[k], " | "))
	}
	t.Logf("%d corpus items ran end to end, %d skipped (a per-item budget override has no HTTP form; or the registry refused the draft)", ran, skipped)
}

func decisionList(d map[string]string) []map[string]string {
	ids := make([]string, 0, len(d))
	for id := range d {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []map[string]string{}
	for _, id := range ids {
		out = append(out, map[string]string{"id": id, "decision": d[id]})
	}
	return out
}

// headOf reads the draft head of an app of tenant A.
func (e *m14Env) headOf(app string) int64 {
	e.t.Helper()
	var n int64
	if err := e.f.operator.QueryRow(`SELECT coalesce(max(revision),0) FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, app).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// thresholdPatch rewrites the money threshold of the order workflow condition.
func thresholdPatch(t *testing.T, doc metadata.Document, amount string) []byte {
	t.Helper()
	d := m14Def(t, doc, "workflow.order.approve")
	updated := d
	wf := *d.Workflow
	wf.States = append([]metadata.WorkflowState(nil), d.Workflow.States...)
	for i := range wf.States {
		if wf.States[i].ID != "state.start" {
			continue
		}
		wf.States[i].Transitions = append([]metadata.Transition(nil), wf.States[i].Transitions...)
		cond := *wf.States[i].Transitions[0].Condition
		cond.Args = append([]metadata.Expression(nil), cond.Args...)
		cond.Args[1].Value = json.RawMessage(`{"amount":"` + amount + `","currency":"USD"}`)
		wf.States[i].Transitions[0].Condition = &cond
	}
	updated.Workflow = &wf
	return patchPayload(t, doc, func(p *ai.PatchProposal) {
		p.Operations = []ai.PatchOperation{{Op: "update", Target: d.ID, ExpectedHash: metadata.DefinitionHash(d), Definition: &updated, Rationale: "tidy the threshold"}}
	})
}

// applyAI scripts, proposes and applies a patch as the given human (with the derived
// acknowledgement) and returns the new revision.
func (e *m14Env) applyAI(token string, revision int64, build func(metadata.Document) []byte) int64 {
	e.t.Helper()
	e.provider.Push(e.answerFor(revision, build))
	p := e.proposal(e.propose(token, "Tidy the order application."), 201)
	if p.Status != ai.ProposalReady {
		e.t.Fatalf("not ready: %s %s", p.Status, p.FailureCode)
	}
	var derived ai.Derived
	_ = json.Unmarshal(p.Derived, &derived)
	if w := e.apply(token, p.ID, revision, derived.Sensitive); w.Code != 200 {
		e.t.Fatal("apply", w.Code, w.Body.String())
	}
	return revision + 1
}

// TestM14AdversarialAIAppliedDraftStillPassesTheReleasePath: applying an AI proposal
// writes a draft and nothing else. The M11/M12 path judges the result regardless of who or
// what wrote it: an implementation that contradicts the independent requirement is
// rejected with evidence, a self-authored or source-authored acceptance is refused, and
// only independent acceptance stages a candidate.
func TestM14AdversarialAIAppliedDraftStillPassesTheReleasePath(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	surface := e.authoritySnapshot()
	candidates := func() int {
		var n int
		_ = e.f.operator.QueryRow(`SELECT count(*) FROM control.release_candidates WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&n)
		return n
	}

	// 1. The AI "tidies" the threshold from 500 to 1e18 and a human applies it (the
	// change is sensitive, so the acknowledgement was required).
	revision := e.applyAI(e.alice, 1, func(d metadata.Document) []byte { return thresholdPatch(t, d, "1000000000000000000") })
	if revision != 2 {
		t.Fatal("revision", revision)
	}
	// The independent human requirement still says 500, and bob wrote the acceptance.
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, "500")
	var stored promotion.StoredRequirements
	if w := e.requirements(e.alice, set, 0); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &stored) != nil {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	var spec promotion.StoredAcceptance
	if w := e.acceptance(e.bob, stored.Version, acceptanceFor(set, "500.01")); w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &spec) != nil {
		t.Fatal("acceptance", w.Code, w.Body.String())
	}
	art := m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
	w := e.candidate(e.alice, art)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "APPROVAL_MISMATCH") {
		t.Fatalf("the inflated threshold must fail the independent requirement: %d %s", w.Code, w.Body.String())
	}
	if candidates() != 0 {
		t.Fatal("a failing AI-applied draft was staged")
	}

	// 2. A harmless AI change (threshold back to 500 through the AI again) is judged on
	// its merits, and separation of duties applies to the human who applied it.
	revision = e.applyAI(e.bob, revision, func(d metadata.Document) []byte { return thresholdPatch(t, d, "500") })
	sandbox = e.sandbox(e.alice, revision)
	// bob applied the AI change, so bob is the source author: his own acceptance is refused ...
	if w = e.acceptance(e.bob, stored.Version, acceptanceFor(set, "500.01")); w.Code != 201 {
		t.Fatal("bob's acceptance", w.Code, w.Body.String())
	}
	var bobSpec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &bobSpec)
	if w = e.candidate(e.alice, m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: bobSpec.ID, sandboxID: sandbox}); w.Code != 403 {
		t.Fatalf("the AI change was applied by bob, who also authored the acceptance: %d %s", w.Code, w.Body.String())
	}
	// ... the requester's own acceptance is refused ...
	if w = e.acceptance(e.alice, stored.Version, acceptanceFor(set, "500.02")); w.Code != 201 {
		t.Fatal("alice's acceptance", w.Code, w.Body.String())
	}
	var aliceSpec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &aliceSpec)
	if w = e.candidate(e.alice, m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: aliceSpec.ID, sandboxID: sandbox}); w.Code != 400 {
		t.Fatalf("self-authored acceptance by the requester: %d %s", w.Code, w.Body.String())
	}
	if candidates() != 0 {
		t.Fatal("a refused build staged a candidate")
	}
	// ... and a third human's independent acceptance stages it: the path works, it just
	// does not trust who wrote the draft.
	carol := e.member("carol_m14", "admin")
	if w = e.acceptance(carol.token, stored.Version, acceptanceFor(set, "500.03")); w.Code != 201 {
		t.Fatal("carol's acceptance", w.Code, w.Body.String())
	}
	var carolSpec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &carolSpec)
	if w = e.candidate(e.alice, m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: carolSpec.ID, sandboxID: sandbox}); w.Code != 201 {
		t.Fatalf("an independently accepted AI-applied draft: %d %s", w.Code, w.Body.String())
	}
	if candidates() != 1 {
		t.Fatalf("candidates: %d", candidates())
	}
	// Nothing in the AI path itself approved, accepted or released anything: only the
	// human steps above created acceptance specs, a requirement set, a sandbox and one candidate.
	after := e.authoritySnapshot()
	for _, table := range []string{"control.release_approvals", "control.active_releases", "control.release_history", "control.release_rejections", "control.role_versions", "control.role_heads"} {
		if after[table] != surface[table] {
			t.Errorf("%s changed: %d -> %d", table, surface[table], after[table])
		}
	}
}

var _ = intent.Mandatory
