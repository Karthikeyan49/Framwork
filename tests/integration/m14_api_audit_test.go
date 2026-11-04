//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

const (
	canaryRequest = "CANARY-REQUEST-7F3A9C"
	canaryModel   = "CANARY-MODEL-91B2D4"
)

// TestM14APIAuditCoversEveryOutcomeAndLeaksNoText: allowed, denied and failed requests
// leave audit rows; no audit row, trace row or stored column other than the validated
// proposal body contains the request text or the model's words.
func TestM14APIAuditCoversEveryOutcomeAndLeaksNoText(t *testing.T) {
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")

	talkative := func(revision int64, label string) ai.ScriptStep {
		return e.answerFor(revision, func(d metadata.Document) []byte {
			def := m14Def(t, d, "entity.order")
			updated := def
			updated.Label = label
			return patchPayload(t, d, func(p *ai.PatchProposal) {
				p.Operations = []ai.PatchOperation{{Op: "update", Target: def.ID, ExpectedHash: metadata.DefinitionHash(def), Definition: &updated, Rationale: canaryModel + " rationale"}}
				p.Assumptions = []string{canaryModel + " assumption"}
			})
		})
	}
	e.provider.Push(talkative(1, "Purchase order"))
	created := e.proposal(e.propose(e.alice, canaryRequest+" rename the order entity"), 201)

	for i := 0; i < 3; i++ {
		e.provider.Push(step(brokenPatch(t, doc, canaryModel+"x")))
	}
	needs := e.proposal(e.propose(e.alice, canaryRequest+" add a ghost page"), 201)
	e.provider.Push(ai.ScriptStep{Err: ai.ErrProviderRefused})
	if w := e.propose(e.alice, canaryRequest+" refused by provider"); w.Code != 503 {
		t.Fatal(w.Code)
	}
	if w := e.propose(e.viewer, canaryRequest+" viewer"); w.Code != 403 {
		t.Fatal(w.Code)
	}
	_ = e.proposal(e.ai("GET", "/proposals/"+created.ID, e.alice, nil), 200)
	e.ai("GET", "/proposals", e.alice, nil)
	if w := e.apply(e.alice, created.ID, 9, nil); w.Code != 409 {
		t.Fatal("stale apply", w.Code)
	}
	if w := e.apply(e.alice, created.ID, 1, nil); w.Code != 200 {
		t.Fatal("apply", w.Code, w.Body.String())
	}
	e.provider.Push(talkative(2, "Second"))
	second := e.proposal(e.propose(e.alice, canaryRequest+" another"), 201)
	if w := e.reject(e.alice, second.ID, "OTHER"); w.Code != 200 {
		t.Fatal("reject", w.Code)
	}
	e.provider.Push(e.reqAnswer("po-approval"))
	req := e.proposal(e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": canaryRequest + " capture"}), 201)
	if w := e.confirm(e.alice, req.ID, 0, map[string]string{"po-approval": "RESOLVE"}); w.Code != 200 {
		t.Fatal("confirm", w.Code, w.Body.String())
	}
	_ = needs

	// Every kind of outcome is audited under a closed action name.
	for _, c := range []struct {
		action  string
		allowed bool
		min     int
	}{
		{"ai.proposal.admitted", true, 5}, {"ai.proposal.created", true, 4}, {"ai.proposal.applied", true, 1}, {"ai.proposal.discarded", true, 1}, {"ai.proposal.confirmed", true, 1},
		{"ai.read", true, 2}, {"ai.propose", false, 1}, {"ai.apply.rejected", false, 1},
	} {
		if n := e.auditCount(c.action, c.allowed); n < c.min {
			t.Errorf("audit %s allowed=%v: %d rows, want at least %d", c.action, c.allowed, n, c.min)
		}
	}

	// No audit row carries a prompt or model text.
	var leaks int
	if err := e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND (state::text LIKE '%CANARY%' OR action LIKE '%CANARY%' OR resource LIKE '%CANARY%')`, e.f.tenantA).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatalf("audit rows contain request or model text: %d (%v)", leaks, err)
	}
	// The trace and the proposal rows hold hashes: the request text is stored nowhere
	// and the model's words only inside the validated canonical body.
	if err := e.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.ai_invocations i WHERE row_to_json(i)::text LIKE '%CANARY%')
 + (SELECT count(*) FROM control.ai_proposals p WHERE (to_jsonb(p) - 'body')::text LIKE '%CANARY%')
 + (SELECT count(*) FROM control.ai_proposals p WHERE p.body IS NOT NULL AND convert_from(p.body,'UTF8') LIKE '%CANARY-REQUEST%')`).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatalf("request or model text outside the validated body: %d (%v)", leaks, err)
	}
	var withModel int
	_ = e.f.operator.QueryRow(`SELECT count(*) FROM control.ai_proposals WHERE tenant_id=$1 AND convert_from(body,'UTF8') LIKE '%CANARY-MODEL%'`, e.f.tenantA).Scan(&withModel)
	if withModel == 0 {
		t.Fatal("the validated body is expected to carry the model's rationale")
	}

	// AI audit states hold only the closed field set, with hashes of the right shape.
	allowed := map[string]bool{"metadata_app": true, "metadata_revision": true, "metadata_hash": true, "requirements_version": true, "manifest_hash": true}
	rows, err := e.f.operator.Query(`SELECT action,state FROM audit.entries WHERE tenant_id=$1 AND action LIKE 'ai.%' AND state->>'metadata_app'=$2`, e.f.tenantA, e.app)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	created3 := 0
	for rows.Next() {
		var action string
		var raw []byte
		var state map[string]any
		if err = rows.Scan(&action, &raw); err != nil || json.Unmarshal(raw, &state) != nil {
			t.Fatal(err)
		}
		for k, v := range state {
			if !allowed[k] && !strings.HasPrefix(k, "ai_") {
				t.Errorf("%s: unexpected audit field %s", action, k)
			}
			if strings.HasSuffix(k, "_hash") {
				if s, _ := v.(string); len(s) != 64 {
					t.Errorf("%s: %s is not a digest: %v", action, k, v)
				}
			}
		}
		if action == "ai.proposal.created" && state["ai_status"] == "READY" {
			created3++
			if state["ai_provider"] != "scripted" || state["ai_model"] != "scripted-model-1" || state["ai_template"] != ai.TemplateVersion || state["ai_proposal_hash"] == nil || state["ai_context_hash"] == nil || state["ai_request_hash"] == nil {
				t.Errorf("created state %v", state)
			}
		}
	}
	if created3 == 0 {
		t.Error("no READY created row")
	}
}

// TestM14APIIsolation: another tenant, another application and a forged identifier see
// nothing, and the database says the same when asked directly.
func TestM14APIIsolation(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	p := e.readyPatch(1, "entity.order", "Purchase order")
	olga := e.addMemberIn(e.f.tenantB, "olga", "owner")

	rootB := "/v1/tenants/" + e.f.tenantB + "/preview/" + e.app + "/ai"
	if w := m14Do(e.h, "GET", rootB+"/proposals/"+p.ID, olga.token, nil); w.Code != 404 {
		t.Fatalf("another tenant reads a proposal: %d %s", w.Code, w.Body.String())
	}
	if w := m14Do(e.h, "GET", rootB+"/proposals", olga.token, nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"proposals":[]`) {
		t.Fatalf("another tenant lists proposals: %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"/apply", "/reject", "/confirm"} {
		if w := m14Do(e.h, "POST", rootB+"/proposals/"+p.ID+suffix, olga.token, `{"expected_revision":1,"acknowledge":[],"reason":"OTHER","expected_requirements_version":0,"decisions":[]}`); w.Code == 200 {
			t.Fatalf("another tenant decided %s", suffix)
		}
	}
	if w := m14Do(e.h, "POST", rootB+"/patch-proposals", olga.token, map[string]any{"request": "Rename it."}); w.Code != 404 {
		t.Fatalf("another tenant proposes against our application: %d %s", w.Code, w.Body.String())
	}
	if w := m14Do(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/preview/"+e.app+"/ai/proposals/"+p.ID, olga.token, nil); w.Code != 403 {
		t.Fatalf("a non-member of tenant A: %d", w.Code)
	}
	// The database refuses the same through the ports, under a forged tenant too.
	ports := &portsEnv{m12Env: e.m12Env}
	var out string
	wantState(t, "decide in another tenant", ports.call(olga.subject, e.f.tenantB, &out, `SELECT control.ai_decide($1,$2,'REJECT',NULL,NULL,NULL,'OTHER',$3)`, e.app, p.ID, strings.Repeat("a", 64)), "P0002")
	wantState(t, "finish in another tenant", ports.call(olga.subject, e.f.tenantB, &out, `SELECT control.ai_finish($1,$2,'FAILED',NULL,NULL,NULL,NULL,NULL,'INTERNAL',NULL,0,0,0)`, e.app, p.ID), "P0002")
	var n int
	if err := ports.call(olga.subject, e.f.tenantB, &n, `SELECT count(*) FROM control.ai_proposals`); err != nil || n != 0 {
		t.Fatalf("row level security leaks %d proposals to tenant B (%v)", n, err)
	}
	// Another application of the same tenant does not see it either.
	other := "app.second_" + strings.ReplaceAll(uuid(t), "-", "")
	raw, _ := json.Marshal(orderDocument(other, "500"))
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+other+"/versions", e.alice, 0, raw); w.Code != 201 {
		t.Fatal("second app", w.Code, w.Body.String())
	}
	rootOther := "/v1/tenants/" + e.f.tenantA + "/preview/" + other + "/ai"
	if w := m14Do(e.h, "GET", rootOther+"/proposals/"+p.ID, e.alice, nil); w.Code != 404 {
		t.Fatalf("another application's route reads the proposal: %d", w.Code)
	}
	if w := m14Do(e.h, "POST", rootOther+"/proposals/"+p.ID+"/apply", e.alice, map[string]any{"expected_revision": 1, "acknowledge": []string{}}); w.Code != 404 {
		t.Fatalf("another application's route applies the proposal: %d %s", w.Code, w.Body.String())
	}
	if w := m14Do(e.h, "GET", rootOther+"/proposals", e.alice, nil); !strings.Contains(w.Body.String(), `"proposals":[]`) {
		t.Fatalf("list of another application: %s", w.Body.String())
	}
	// And the proposal is still ours and untouched.
	if got := e.proposal(e.ai("GET", "/proposals/"+p.ID, e.alice, nil), 200); got.Status != ai.ProposalReady {
		t.Fatal(got.Status)
	}
}

// TestM14APINoModelCallOnOrdinaryPaths: nothing but a creation request reaches the
// provider. Reads, refusals, decisions, metadata, compile and the M12 builder routes
// are deterministic (cost and latency risk 37).
func TestM14APINoModelCallOnOrdinaryPaths(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	p := e.readyPatch(1, "entity.order", "Purchase order")
	calls := e.provider.Calls()
	e.provider.Push(step([]byte("{}")), step([]byte("{}")), step([]byte("{}")))

	e.ai("GET", "/proposals", e.alice, nil)
	e.ai("GET", "/proposals/"+p.ID, e.alice, nil)
	e.propose(e.viewer, "denied")                               // 403
	e.ai("POST", "/patch-proposals", e.alice, `{"request":""}`) // 400
	e.ai("POST", "/patch-proposals", e.alice, map[string]any{"request": "x", "expected_revision": 9})
	e.apply(e.alice, p.ID, 9, nil)
	if w := e.apply(e.alice, p.ID, 1, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	e.reject(e.alice, p.ID, "OTHER")
	e.register(e.alice, "600", 2)
	m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, 0, nil)
	m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions/3/compile", e.alice, 0, nil)
	e.requirements(e.alice, thresholdSet(e.app, "600"), 0)
	m3Call(e.h, "GET", e.root+"/requirements", e.alice, 0, nil)
	m3Call(e.h, "GET", e.root+"/candidates", e.alice, 0, nil)
	m3Call(e.h, "GET", e.root+"/releases", e.alice, 0, nil)
	if got := e.provider.Calls(); got != calls {
		t.Fatalf("%d provider calls on non-creation paths", got-calls)
	}
}
