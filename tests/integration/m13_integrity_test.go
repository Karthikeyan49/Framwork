//go:build integration

package integration

// Integrity checks for the integrated M10..M13 tree. They exist because the
// branches were developed apart and merged: a candidate built before the DSL
// 2.2.0 runtime-components change must not stay promotable, and every SQL
// function that several migrations replace must keep the guards of the older
// definitions. Everything here is reusable after any further merge.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
)

// m13StaleSummary prints the staleness fields of a candidate view, never the
// stored artifacts.
func m13StaleSummary(v access.CandidateView) string {
	if v.Stale == nil {
		return "stale=<absent>"
	}
	return fmt.Sprintf("stale=%v reasons=%v", *v.Stale, v.StaleReasons)
}

// TestM13IntegrationRuntimeContractChangeStalesOlderCandidates: a candidate
// whose recorded runtime components differ from the running binary (here the
// workflow DSL list recorded before DSL 2.2.0) is self-consistent, restores and
// verifies, yet must read STALE with a reason, be refused by approve and by
// activate, and a rebuilt candidate must go through approve and activate.
func TestM13IntegrationRuntimeContractChangeStalesOlderCandidates(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()

	// Control: a candidate built by this binary is fresh.
	if v := viewOf(t, e.httpCandidate(e.viewer, tmpl.hash), 200); v.Stale == nil || *v.Stale || len(v.StaleReasons) != 0 {
		t.Fatalf("a candidate built by this binary is not fresh: %s", m13StaleSummary(v))
	}

	// The same candidate as the binary before DSL 2.2.0 recorded it: the
	// components document without "2.2.0", with its own consistent hash.
	var b promotion.BundleV2
	if err := json.Unmarshal(tmpl.raw, &b); err != nil {
		t.Fatal(err)
	}
	older := b.RuntimeContract.Components
	older.WorkflowDSL = append([]string{}, older.WorkflowDSL...)
	if n := len(older.WorkflowDSL); n < 2 || older.WorkflowDSL[n-1] != "2.2.0" {
		t.Fatalf("the current workflow DSL list does not end in 2.2.0: %v", older.WorkflowDSL)
	}
	older.WorkflowDSL = older.WorkflowDSL[:len(older.WorkflowDSL)-1]
	raw, _ := json.Marshal(older)
	sum := sha256.Sum256(raw)
	olderHash, err := e.stageDoctored(func(d *promotion.BundleV2) {
		d.RuntimeContract.Components, d.RuntimeContract.ComponentsHash = older, hex.EncodeToString(sum[:])
	})
	if err != nil {
		t.Fatal("stage the older candidate", err)
	}
	if olderHash == tmpl.hash {
		t.Fatal("the older candidate must be a different artifact")
	}

	// It restores (so it is not candidate_failed_verification) and reads stale.
	v := viewOf(t, e.httpCandidate(e.viewer, olderHash), 200)
	if v.Stale == nil || !*v.Stale || !reflect.DeepEqual(v.StaleReasons, []promotion.StaleReason{"RUNTIME_CHANGED"}) {
		t.Fatalf("an older runtime contract did not read stale: %s", m13StaleSummary(v))
	}

	// Approve is refused with the reason and writes nothing.
	before := e.state()
	r := m13apiExpect(t, "approval of an older runtime contract", e.httpApprove(e.dave.token, olderHash), 409, "candidate_stale")
	if strings.Join(r.Reasons, ",") != "RUNTIME_CHANGED" {
		t.Fatalf("stale reasons at approval: %v", r.Reasons)
	}
	e.unchanged("stale approval", before)

	// An approval recorded by a compromised caller through the raw port does not
	// make it activatable: activation re-evaluates staleness.
	if _, err = e.approvePort(e.dave.subject, olderHash); err != nil {
		t.Fatal("raw approval of the older candidate", err)
	}
	before = e.state()
	r = m13apiExpect(t, "activation of an older runtime contract", e.httpActivate(e.erin.token, olderHash, "", 0), 409, "candidate_stale")
	if strings.Join(r.Reasons, ",") != "RUNTIME_CHANGED" {
		t.Fatalf("stale reasons at activation: %v", r.Reasons)
	}
	e.unchanged("stale activation", before)
	if hash, _, ok := e.activePointer(); ok {
		t.Fatal("the older candidate became active", hash)
	}

	// A rebuilt candidate (new hash, this binary's components) is promotable.
	rebuilt, _ := e.build("520", nil, e.carol)
	if rebuilt == olderHash || rebuilt == tmpl.hash {
		t.Fatal("a rebuild must be a new artifact")
	}
	if v = viewOf(t, e.httpCandidate(e.viewer, rebuilt), 200); v.Stale == nil || *v.Stale {
		t.Fatalf("the rebuilt candidate is stale: %s", m13StaleSummary(v))
	}
	viewOf(t, e.httpApprove(e.dave.token, rebuilt), 201)
	if rel := m13apiRelease(t, e.httpActivate(e.erin.token, rebuilt, "", 0), 200); rel.Generation != 1 {
		t.Fatalf("activation of the rebuilt candidate: %+v", rel)
	}
	if hash, generation, ok := e.activePointer(); !ok || hash != rebuilt || generation != 1 {
		t.Fatal("the rebuilt candidate is not the active release", hash, generation, ok)
	}
	e.checkHistory()
}
