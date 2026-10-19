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
