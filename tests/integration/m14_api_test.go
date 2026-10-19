//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// answerFor scripts a provider answer computed from the stored document of a
// revision at call time.
func (e *m14Env) answerFor(revision int64, build func(metadata.Document) []byte) ai.ScriptStep {
	return ai.ScriptStep{Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: "scripted-model-1",
		Func: func(ai.ProviderRequest) ([]byte, error) { return build(e.storedDoc(revision)), nil }}
}

// TestM14APISliceProposeApply is the vertical slice of ADR040: a scripted provider
// answer becomes a stored READY proposal; a human apply with the (empty) acknowledge
// list appends a new DRAFT revision as that human; the decision happens once.
func TestM14APISliceProposeApply(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	e.provider.Push(e.answerFor(1, func(doc metadata.Document) []byte { return labelPatch(t, doc, "entity.order", "Purchase order") }))

	created := e.proposal(e.ai("POST", "/patch-proposals", e.alice, map[string]any{"request": "Call the order entity Purchase order."}), 201)
	if created.Status != ai.ProposalReady || created.Kind != ai.ProposalPatch || created.SourceRevision != 1 || len(created.ProposalHash) != 64 || created.Attempts != 1 {
		t.Fatalf("created proposal %+v", created)
	}
	var derived ai.Derived
	if err := json.Unmarshal(created.Derived, &derived); err != nil || len(derived.Sensitive) != 0 || len(derived.Targets) != 1 || derived.Targets[0].ID != "entity.order" {
		t.Fatalf("derived %s %v", created.Derived, err)
	}
	if strings.Contains(string(created.Proposal), "request") || created.RequesterID != e.f.alice {
		t.Fatalf("stored proposal leaks the request or has the wrong requester: %+v", created)
	}

	got := e.proposal(e.ai("GET", "/proposals/"+created.ID, e.alice, nil), 200)
	if got.ID != created.ID || got.ProposalHash != created.ProposalHash {
		t.Fatal("GET returned another proposal", got.ID)
	}
	// Nothing was applied by proposing.
	if rev, _ := e.heads(); rev != 1 {
		t.Fatalf("proposing moved the draft to revision %d", rev)
	}

	w := e.ai("POST", "/proposals/"+created.ID+"/apply", e.alice, map[string]any{"expected_revision": 1, "acknowledge": []string{}})
	if w.Code != 200 {
		t.Fatal("apply", w.Code, w.Body.String())
	}
	var applied struct {
		Proposal ai.Proposal      `json:"proposal"`
		Revision metadata.Version `json:"revision"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &applied); err != nil || applied.Proposal.Status != ai.ProposalApplied || applied.Revision.Revision != 2 || applied.Proposal.ResultRevision != 2 {
		t.Fatalf("apply result %s %v", w.Body.String(), err)
	}
	if label := m14Def(t, e.storedDoc(2), "entity.order").Label; label != "Purchase order" {
		t.Fatalf("revision 2 label %q", label)
	}
	var author string
	if err := e.f.operator.QueryRow(`SELECT created_by::text FROM control.metadata_versions WHERE tenant_id=$1 AND app_id=$2 AND revision=2`, e.f.tenantA, e.app).Scan(&author); err != nil || author != e.f.alice {
		t.Fatalf("revision 2 author %q (%v): the human is the actor of record", author, err)
	}

	again := e.ai("POST", "/proposals/"+created.ID+"/apply", e.alice, map[string]any{"expected_revision": 2, "acknowledge": []string{}})
	if again.Code != 409 || m14Code(again) != "proposal_decided" {
		t.Fatalf("second apply: %d %s", again.Code, again.Body.String())
	}
	for action, want := range map[string]int{"ai.proposal.admitted": 1, "ai.proposal.created": 1, "ai.proposal.applied": 1} {
		if n := e.auditCount(action, true); n != want {
			t.Errorf("audit %s = %d, want %d", action, n, want)
		}
	}
	if n := e.auditCount("ai.apply.rejected", false); n != 1 {
		t.Errorf("audit ai.apply.rejected = %d, want 1", n)
	}
}

// ---------------------------------------------------------------- policy matrix

// TestM14APIPolicyMatrix pins ADR040 A6 through the real routes: ai.read for every
// member, ai.propose and ai.apply for owner and admin, nothing for an outsider or an
// anonymous caller, custom roles by grant, and the composition rule (apply needs
// metadata.write, confirm needs requirements.decide) on top of ai.apply.
func TestM14APIPolicyMatrix(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	ready := e.readyPatch(1, "entity.order", "Purchase order")
	member := e.member("mia", "member")
	olga := e.addMemberIn(e.f.tenantB, "olga", "owner")
	reader := e.customRole("ai_reader", "ai.read")
	author := e.customRole("ai_author", "ai.read", "ai.propose")
	applier := e.customRole("ai_applier_only", "ai.read", "ai.apply")
	confirmerOnly := e.customRole("ai_confirm_only", "ai.read", "ai.apply")
	withMeta := e.customRole("ai_applier_meta", "ai.read", "ai.apply", "metadata.write")
	withReq := e.customRole("ai_confirmer", "ai.read", "ai.apply", "requirements.decide")
	_ = confirmerOnly

	type route struct {
		name   string
		method string
		path   string
		body   any
	}
	routes := []route{
		{"propose patch", "POST", "/patch-proposals", map[string]any{"request": "x"}},
		{"propose requirements", "POST", "/requirement-proposals", map[string]any{"request": "x"}},
		{"list", "GET", "/proposals", nil},
		{"get", "GET", "/proposals/" + ready.ID, nil},
		{"apply", "POST", "/proposals/" + ready.ID + "/apply", map[string]any{"expected_revision": 1, "acknowledge": []string{}}},
		{"confirm", "POST", "/proposals/" + ready.ID + "/confirm", map[string]any{"expected_requirements_version": 0, "decisions": []any{}}},
		{"reject", "POST", "/proposals/" + ready.ID + "/reject", map[string]any{"reason": "NOT_WANTED"}},
	}
	read := map[string]bool{"list": true, "get": true}
	type actor struct {
		name  string
		token string
		can   map[string]bool // routes the actor may use (they change state and are tested below)
		read  bool
	}
	actors := []actor{
		{"member", member.token, nil, true},
		{"viewer", e.viewer, nil, true},
		{"reader role", reader.token, nil, true},
		{"author role", author.token, map[string]bool{"propose patch": true, "propose requirements": true}, true},
		{"applier without metadata.write", applier.token, map[string]bool{"reject": true}, true},
		{"applier with metadata.write", withMeta.token, map[string]bool{"apply": true, "reject": true}, true},
		{"applier with requirements.decide", withReq.token, map[string]bool{"confirm": true, "reject": true}, true},
		{"outsider (tenant B owner)", olga.token, nil, false},
		{"anonymous", "", nil, false},
	}
	for _, a := range actors {
		for _, r := range routes {
			want := 403
			switch {
			case a.token == "":
				want = 401
			case read[r.name] && a.read:
				want = 200
			case a.can[r.name]:
				continue // allowed and state-changing: covered below
			}
			if w := e.ai(r.method, r.path, a.token, r.body); w.Code != want {
				t.Errorf("%s / %s: %d %s, want %d", a.name, r.name, w.Code, w.Body.String(), want)
			}
		}
	}
	if rev, _ := e.heads(); rev != 1 {
		t.Fatal("a refused request changed the draft")
	}
	if n := e.proposalRows(); n != 1 {
		rows, _ := e.f.operator.Query(`SELECT p.id::text,p.kind,p.status,p.requester_id::text FROM control.ai_proposals p WHERE p.tenant_id=$1 AND p.app_id=$2 ORDER BY p.created_at`, e.f.tenantA, e.app)
		for rows != nil && rows.Next() {
			var id, kind, status, who string
			_ = rows.Scan(&id, &kind, &status, &who)
			t.Log("row", id, kind, status, who)
		}
		t.Fatalf("refused requests left %d proposal rows", n)
	}
	// Denials are audited under the action itself, not as a success.
	if e.auditCount("ai.propose", false) == 0 || e.auditCount("ai.apply", false) == 0 {
		t.Error("policy denials must be audited with allowed=false")
	}
	// The author role may propose (and read) but its apply is refused.
	e.provider.Push(e.answerFor(1, func(doc metadata.Document) []byte { return labelPatch(t, doc, "entity.order", "By author") }))
	byAuthor := e.proposal(e.propose(author.token, "Rename the order entity."), 201)
	if byAuthor.RequesterID != author.id {
		t.Fatal("the requester of record is the authenticated human", byAuthor.RequesterID)
	}
	if w := e.apply(author.token, byAuthor.ID, 1, nil); w.Code != 403 {
		t.Fatalf("author applied: %d", w.Code)
	}
	// Composition: ai.apply with metadata.write applies; without it, it does not. A
	// veto needs ai.apply only: a role without metadata.write may reject.
	if w := e.apply(applier.token, ready.ID, 1, nil); w.Code != 403 {
		t.Fatalf("ai.apply without metadata.write applied: %d %s", w.Code, w.Body.String())
	}
	if w := e.reject(applier.token, byAuthor.ID, "UNSAFE"); w.Code != 200 {
		t.Fatalf("ai.apply alone may reject: %d %s", w.Code, w.Body.String())
	}
	if w := e.apply(withMeta.token, ready.ID, 1, nil); w.Code != 200 {
		t.Fatalf("ai.apply with metadata.write refused: %d %s", w.Code, w.Body.String())
	}
	// A different human decides: the requester is recorded, not required.
	var decidedBy string
	if err := e.f.operator.QueryRow(`SELECT decided_by::text FROM control.ai_proposals WHERE tenant_id=$1 AND id=$2`, e.f.tenantA, ready.ID).Scan(&decidedBy); err != nil || decidedBy != withMeta.id || decidedBy == ready.RequesterID {
		t.Fatalf("decided_by %q requester %q (%v)", decidedBy, ready.RequesterID, err)
	}
	// confirm needs requirements.decide on top of ai.apply.
	e.provider.Push(e.reqAnswer("po-approval"))
	req := e.proposal(e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": "Orders above 500 USD need admin approval."}), 201)
	decisions := map[string]string{"po-approval": "RESOLVE"}
	if w := e.confirm(withMeta.token, req.ID, 0, decisions); w.Code != 403 {
		t.Fatalf("confirm without requirements.decide: %d", w.Code)
	}
	if w := e.confirm(withReq.token, req.ID, 0, decisions); w.Code != 200 {
		t.Fatalf("confirm with requirements.decide: %d %s", w.Code, w.Body.String())
	}
}
