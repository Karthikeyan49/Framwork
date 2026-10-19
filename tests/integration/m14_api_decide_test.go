//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// approvalRolesPatch rewrites the approval gate of the order workflow: a sensitive
// change (workflow graph / approval gate) that apply must make the caller acknowledge.
func approvalRolesPatch(t *testing.T, doc metadata.Document, roles []string) []byte {
	t.Helper()
	d := m14Def(t, doc, "workflow.order.approve")
	updated := d
	wf := *d.Workflow
	wf.States = append([]metadata.WorkflowState(nil), d.Workflow.States...)
	for i := range wf.States {
		if wf.States[i].Type == "approval" {
			wf.States[i].ApprovalRoles = roles
		}
	}
	updated.Workflow = &wf
	return patchPayload(t, doc, func(p *ai.PatchProposal) {
		p.Operations = []ai.PatchOperation{{Op: "update", Target: d.ID, ExpectedHash: metadata.DefinitionHash(d), Definition: &updated, Rationale: "change the approvers"}}
	})
}

func (e *m14Env) sensitiveProposal(revision int64, roles []string) ai.Proposal {
	e.t.Helper()
	e.provider.Push(e.answerFor(revision, func(doc metadata.Document) []byte { return approvalRolesPatch(e.t, doc, roles) }))
	p := e.proposal(e.propose(e.alice, "Let owners approve orders too."), 201)
	if p.Status != ai.ProposalReady {
		e.t.Fatalf("status %s %s", p.Status, p.FailureCode)
	}
	return p
}

func TestM14APIApplyRequiresTheServerDerivedAcknowledgement(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	surface := e.authoritySnapshot()
	p := e.sensitiveProposal(1, []string{"admin", "owner"})
	var derived ai.Derived
	if err := json.Unmarshal(p.Derived, &derived); err != nil || len(derived.Sensitive) == 0 {
		t.Fatalf("a change of the approval gate is sensitive: %s", p.Derived)
	}
	for _, s := range derived.Sensitive {
		if !strings.HasPrefix(s, "workflow.order.approve:") {
			t.Fatalf("sensitive entry %q", s)
		}
	}
	bad := map[string][]string{
		"empty":     {},
		"one short": derived.Sensitive[1:],
		"extra":     append(slices.Clone(derived.Sensitive), "workflow.order.approve:LABEL"),
		"invented":  {"entity.order:STORAGE"},
		"duplicate": append(slices.Clone(derived.Sensitive), derived.Sensitive[0]),
		"upper":     {strings.ToUpper(derived.Sensitive[0])},
	}
	if len(derived.Sensitive) == 1 {
		delete(bad, "one short")
	}
	for name, ack := range bad {
		w := e.apply(e.alice, p.ID, 1, ack)
		var reply struct {
			Code      string   `json:"code"`
			Sensitive []string `json:"sensitive"`
		}
		if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Code != "acknowledge_mismatch" || !slices.Equal(reply.Sensitive, derived.Sensitive) {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	if rev, _ := e.heads(); rev != 1 {
		t.Fatal("a refused apply moved the draft")
	}
	if got := e.proposal(e.ai("GET", "/proposals/"+p.ID, e.alice, nil), 200); got.Status != ai.ProposalReady {
		t.Fatalf("a refused apply must leave the proposal READY: %s", got.Status)
	}
	// The exact list, in any order, applies once.
	reversed := slices.Clone(derived.Sensitive)
	slices.Reverse(reversed)
	w := e.apply(e.alice, p.ID, 1, reversed)
	if w.Code != 200 {
		t.Fatal("apply with the derived list", w.Code, w.Body.String())
	}
	var res struct {
		Sensitive []string         `json:"sensitive"`
		Revision  metadata.Version `json:"revision"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if !slices.Equal(res.Sensitive, derived.Sensitive) || res.Revision.Revision != 2 {
		t.Fatalf("%s", w.Body.String())
	}
	var ack string
	if err := e.f.operator.QueryRow(`SELECT state->>'ai_ack_hash' FROM audit.entries WHERE tenant_id=$1 AND action='ai.proposal.applied' AND state->>'metadata_app'=$2`, e.f.tenantA, e.app).Scan(&ack); err != nil || len(ack) != 64 {
		t.Fatalf("the acknowledgement is audited as a hash: %q %v", ack, err)
	}
	// What was applied is a draft: no approval, acceptance, release, role or tool appeared.
	e.assertNoAuthoritySurface(surface)
}

var authorityTables = []string{"control.release_approvals", "control.acceptance_specs", "control.release_candidates", "control.active_releases", "control.release_history",
	"control.release_rejections", "control.role_versions", "control.role_heads", "runtime.sandboxes", "runtime.sandbox_evidence"}

// authoritySnapshot counts the rows of every table an AI proposal must never touch.
func (e *m14Env) authoritySnapshot() map[string]int {
	e.t.Helper()
	out := map[string]int{}
	for _, table := range authorityTables {
		var n int
		if err := e.f.operator.QueryRow(fmt.Sprintf(`SELECT count(*) FROM %s WHERE tenant_id=$1`, table), e.f.tenantA).Scan(&n); err != nil {
			e.t.Fatalf("%s: %v", table, err)
		}
		out[table] = n
	}
	return out
}

// assertNoAuthoritySurface checks that AI-only activity created no approval, acceptance
// spec, candidate, release, sandbox or role since the snapshot was taken.
func (e *m14Env) assertNoAuthoritySurface(before map[string]int) {
	e.t.Helper()
	for table, n := range e.authoritySnapshot() {
		if n != before[table] {
			e.t.Errorf("%s changed from %d to %d rows during AI-only activity", table, before[table], n)
		}
	}
}

func TestM14APIApplyRevalidatesAgainstTheCurrentDraft(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	p := e.readyPatch(1, "entity.order", "Purchase order")

	// The draft moves: the old expected_revision is refused, the proposal stays READY.
	other := orderDocument(e.app, "500")
	other.Definitions[1].Label = "Amount (edited)" // a different definition
	raw, _ := json.Marshal(other)
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, 1, raw); w.Code != 201 {
		t.Fatal("append revision 2", w.Code, w.Body.String())
	}
	if w := e.apply(e.alice, p.ID, 1, nil); w.Code != 409 || m14Code(w) != "draft_changed" {
		t.Fatalf("stale revision: %d %s", w.Code, w.Body.String())
	}
	// Against revision 2 the target is unchanged, so per-target CAS passes and the
	// proposal applies to the CURRENT draft (the other edit survives).
	w := e.apply(e.alice, p.ID, 2, nil)
	if w.Code != 200 {
		t.Fatalf("apply on the moved draft: %d %s", w.Code, w.Body.String())
	}
	final := e.storedDoc(3)
	if m14Def(t, final, "entity.order").Label != "Purchase order" || m14Def(t, final, "field.order.id").Label != "Amount (edited)" && m14Def(t, final, other.Definitions[1].ID).Label != "Amount (edited)" {
		t.Fatalf("the current draft was not preserved: %+v", final.Definitions[1])
	}

	// A proposal whose own target changed underneath it conflicts.
	q := e.readyPatch(3, "entity.order", "Second rename")
	changed := e.storedDoc(3)
	for i := range changed.Definitions {
		if changed.Definitions[i].ID == "entity.order" {
			changed.Definitions[i].Label = "Edited by a person"
		}
	}
	raw, _ = json.Marshal(changed)
	if w = m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, 3, raw); w.Code != 201 {
		t.Fatal("append revision 4", w.Code, w.Body.String())
	}
	w = e.apply(e.alice, q.ID, 4, nil)
	var reply struct {
		Code      string        `json:"code"`
		Conflicts []ai.Conflict `json:"conflicts"`
	}
	if w.Code != 409 || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Code != "proposal_conflict" || len(reply.Conflicts) != 1 || reply.Conflicts[0].Category != ai.ConflictStaleHash || reply.Conflicts[0].Target != "entity.order" {
		t.Fatalf("conflict: %d %s", w.Code, w.Body.String())
	}
	if rev, _ := e.heads(); rev != 4 {
		t.Fatal("a conflict moved the draft", rev)
	}
}

func TestM14APIApplyStateMachine(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	surface := e.authoritySnapshot()
	bob := e.bob

	// Another human may decide; the requester is recorded, the decider is stamped.
	p := e.readyPatch(1, "entity.order", "By alice, applied by bob")
	if w := e.apply(bob, p.ID, 1, nil); w.Code != 200 {
		t.Fatalf("admin applies another's proposal: %d %s", w.Code, w.Body.String())
	}
	got := e.proposal(e.ai("GET", "/proposals/"+p.ID, e.alice, nil), 200)
	if got.Status != ai.ProposalApplied || got.RequesterID != e.f.alice || got.DecidedBy != e.f.bob || got.ResultRevision != 2 || len(got.ResultHash) != 64 || got.DecidedAt == nil {
		t.Fatalf("%+v", got)
	}
	// The applied revision belongs to bob and compiles.
	var author string
	_ = e.f.operator.QueryRow(`SELECT created_by::text FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, e.f.tenantA, e.app).Scan(&author)
	if author != e.f.bob {
		t.Fatalf("revision 2 author %s, want the applying human", author)
	}
	if w := m3Call(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions/2/compile", e.alice, 0, nil); w.Code != 200 {
		t.Fatalf("the applied draft must compile: %d", w.Code)
	}

	// Reject, then everything else is refused.
	r := e.readyPatch(2, "entity.order", "To be rejected")
	for _, reason := range []string{"BOGUS", ""} {
		if w := e.reject(e.alice, r.ID, reason); w.Code != 400 {
			t.Errorf("reason %q: %d", reason, w.Code)
		}
	}
	w := e.reject(e.alice, r.ID, "INCORRECT")
	var rejected struct {
		Proposal ai.Proposal `json:"proposal"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &rejected) != nil || rejected.Proposal.Status != ai.ProposalRejected || rejected.Proposal.RejectReason != "INCORRECT" || rejected.Proposal.ResultRevision != 0 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for name, call := range map[string]func() int{
		"apply after reject":   func() int { return e.apply(e.alice, r.ID, 2, nil).Code },
		"reject after reject":  func() int { return e.reject(e.alice, r.ID, "OTHER").Code },
		"confirm after reject": func() int { return e.confirm(e.alice, r.ID, 0, map[string]string{"a": "RESOLVE"}).Code },
	} {
		if c := call(); c != 409 {
			t.Errorf("%s: %d", name, c)
		}
	}
	if rev, _ := e.heads(); rev != 2 {
		t.Fatal("rejection moved the draft", rev)
	}
	// Confirming a patch proposal, applying a requirements proposal and an unknown id.
	s := e.readyPatch(2, "entity.order", "Confirm me")
	if w := e.confirm(e.alice, s.ID, 0, map[string]string{"a": "RESOLVE"}); w.Code != 409 || m14Code(w) != "proposal_wrong_kind" {
		t.Fatalf("confirm a patch proposal: %d %s", w.Code, w.Body.String())
	}
	e.provider.Push(e.reqAnswer("po-approval"))
	req := e.proposal(e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": "Orders above 500 USD need admin approval."}), 201)
	if w := e.apply(e.alice, req.ID, 2, nil); w.Code != 409 || m14Code(w) != "proposal_wrong_kind" {
		t.Fatalf("apply a requirements proposal: %d %s", w.Code, w.Body.String())
	}
	if w := e.apply(e.alice, "11111111-2222-4333-8444-555555555555", 2, nil); w.Code != 404 {
		t.Fatalf("unknown id: %d", w.Code)
	}
	// The stored views are what a client may rely on.
	list := e.ai("GET", "/proposals", e.alice, nil)
	var l struct {
		Proposals []map[string]any `json:"proposals"`
	}
	if list.Code != 200 || json.Unmarshal(list.Body.Bytes(), &l) != nil || len(l.Proposals) != 4 {
		t.Fatalf("list %d %s", list.Code, list.Body.String())
	}
	for _, row := range l.Proposals {
		for _, hidden := range []string{"proposal", "derived", "diagnostics"} {
			if _, present := row[hidden]; present {
				t.Errorf("the list shows %s", hidden)
			}
		}
	}
	created := []string{}
	for _, row := range l.Proposals {
		created = append(created, row["created_at"].(string))
	}
	if !slices.IsSortedFunc(created, func(a, b string) int { return strings.Compare(b, a) }) {
		t.Errorf("the list is not newest first: %v", created)
	}
	e.assertNoAuthoritySurface(surface)
}

func TestM14APIRequirementCaptureAndHumanConfirmation(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	surface := e.authoritySnapshot()
	e.provider.Push(e.reqAnswer("po-approval"))
	req := e.proposal(e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": "Orders above 500 USD need admin approval."}), 201)
	if req.Status != ai.ProposalReady || req.Kind != ai.ProposalRequirements {
		t.Fatalf("%+v", req)
	}
	var preview struct {
		Requirements []struct {
			ID                string `json:"id"`
			Category          string `json:"category"`
			Requiredness      string `json:"requiredness"`
			RequirednessBasis string `json:"requiredness_basis"`
			Verifiable        bool   `json:"verifiable"`
		} `json:"requirements"`
		OpenQuestions int `json:"open_questions"`
	}
	if json.Unmarshal(req.Derived, &preview) != nil || len(preview.Requirements) != 1 || preview.OpenQuestions != 1 {
		t.Fatalf("derived %s", req.Derived)
	}
	r0 := preview.Requirements[0]
	if r0.Requiredness != intent.Mandatory || r0.RequirednessBasis != intent.BasisPolicyFloor || !r0.Verifiable {
		t.Fatalf("requiredness comes from the policy floor, not the model: %+v", r0)
	}
	// Nothing was recorded by proposing.
	if _, v := e.heads(); v != 0 {
		t.Fatal("a requirement proposal wrote a requirement set")
	}
	// Decisions must name each draft exactly once.
	for name, decisions := range map[string]map[string]string{
		"none": {}, "unknown id": {"po-approval": "RESOLVE", "other": "RESOLVE"}, "wrong id": {"nope": "RESOLVE"},
		"bad decision": {"po-approval": "ACCEPT"}, "lower case": {"po-approval": "resolve"},
	} {
		if w := e.confirm(e.alice, req.ID, 0, decisions); w.Code != 400 || m14Code(w) != "invalid_ai_request" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	dup := e.ai("POST", "/proposals/"+req.ID+"/confirm", e.alice, `{"expected_requirements_version":0,"decisions":[{"id":"po-approval","decision":"RESOLVE"},{"id":"po-approval","decision":"REJECT"}]}`)
	if dup.Code != 400 {
		t.Errorf("duplicate decision: %d", dup.Code)
	}
	if w := e.confirm(e.alice, req.ID, 5, map[string]string{"po-approval": "RESOLVE"}); w.Code != 409 || m14Code(w) != "requirements_changed" {
		t.Fatalf("stale version: %d %s", w.Code, w.Body.String())
	}
	// A human keeps the mandatory requirement open.
	w := e.confirm(e.alice, req.ID, 0, map[string]string{"po-approval": "KEEP_OPEN"})
	var done struct {
		Proposal     ai.Proposal `json:"proposal"`
		Requirements struct {
			Version   int64           `json:"version"`
			Hash      string          `json:"hash"`
			DecidedBy string          `json:"decided_by"`
			Body      json.RawMessage `json:"requirements"`
		} `json:"requirements"`
		Recorded []ai.RecordedRequirement `json:"recorded"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &done) != nil || done.Proposal.Status != ai.ProposalConfirmed || done.Requirements.Version != 1 || done.Requirements.DecidedBy != e.f.alice || done.Proposal.ResultRevision != 1 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	rec := done.Recorded[0]
	if rec.Provenance != intent.ProvenanceAIConfirmed || rec.Requiredness != intent.Mandatory || rec.RequirednessBasis != intent.BasisPolicyFloor || rec.Status != intent.StatusOpenQuestion {
		t.Fatalf("recorded %+v", rec)
	}
	// The M12 route returns the same set; the requirement is parsed by M12's own decoder.
	get := e.get("/requirements/1", e.alice)
	var stored struct {
		Body json.RawMessage `json:"requirements"`
	}
	if get.Code != 200 || json.Unmarshal(get.Body.Bytes(), &stored) != nil {
		t.Fatalf("%d %s", get.Code, get.Body.String())
	}
	set, _, _, err := intent.ParseRequirementSet(stored.Body)
	if err != nil || len(set.Requirements) != 1 || set.Requirements[0].Provenance != intent.ProvenanceAIConfirmed || set.Requirements[0].Status != intent.StatusOpenQuestion || set.Requirements[0].Requiredness != intent.Mandatory {
		t.Fatalf("%+v %v", set, err)
	}
	// Once only, and the id cannot be claimed twice.
	if w = e.confirm(e.alice, req.ID, 1, map[string]string{"po-approval": "RESOLVE"}); w.Code != 409 || m14Code(w) != "proposal_decided" {
		t.Fatalf("second confirm: %d %s", w.Code, w.Body.String())
	}
	e.provider.Push(e.reqAnswer("po-approval"))
	again := e.proposal(e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": "Same again."}), 201)
	if w = e.confirm(e.alice, again.ID, 1, map[string]string{"po-approval": "RESOLVE"}); w.Code != 409 || m14Code(w) != "requirements_changed" {
		t.Fatalf("an existing requirement id: %d %s", w.Code, w.Body.String())
	}
	e.assertNoAuthoritySurface(surface)
}
