package promotion

import (
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/sandbox"
	"os"
	"strings"
	"testing"
)

type testGate bool

func (g testGate) AuthorizeRelease(string, string, string, string) bool { return bool(g) }
func candidateFixture(t *testing.T) (Candidate, metadata.Source, compiler.Program) {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if e != nil {
		t.Fatal(e)
	}
	v, e := metadata.NewValidator()
	if e != nil {
		t.Fatal(e)
	}
	s, e := v.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	p, d := compiler.Compile(s, compiler.PlatformCatalog())
	if !p.Valid() {
		t.Fatal(d)
	}
	i := intent.Contract{Version: "1.0.0", Requirements: []intent.Requirement{{ID: "approval", Mandatory: true, Status: "RESOLVED", EntityID: "entity.contact", FieldID: "field.contact.balance", Operator: "GT", Threshold: "500", Currency: "USD", ApproverRole: "admin", Trigger: "submit"}}}
	a := intent.Acceptance{Version: "1.0.0", IntentHash: i.Hash(), AuthorActorID: "11111111-1111-1111-1111-111111111111", Provenance: "HUMAN_INDEPENDENT", Cases: []intent.Case{{RequirementID: "approval", Input: "500001 INR", Expected: "finance approval"}}}
	aRaw, _ := json.Marshal(a)
	h := strings.Repeat("a", 64)
	b := Bundle{Version: "1.0.0", Dependencies: Dependencies{h, h, h}, Intent: i, Acceptance: a, Sandbox: sandbox.Context{ID: "sandbox.fixture", DataMode: "SYNTHETIC_SEPARATE", ToolMode: "BLOCK"}, RequesterID: a.AuthorActorID, HighRisk: true, Verification: Verification{Version: "1.0.0", SandboxID: "sandbox.fixture", IntentHash: i.Hash(), AcceptanceHash: Hash(aRaw), ReportHash: h, Outcome: "PASS"}}
	c, e := Build(s, p, b)
	if e != nil {
		t.Fatal(e)
	}
	return c, s, p
}
func TestFrozenCandidateExactApprovalStaleDependenciesAndSeparation(t *testing.T) {
	c, s, p := candidateFixture(t)
	same, e := Build(s, p, c.Bundle())
	if e != nil || same.Hash() != c.Hash() {
		t.Fatal("nondeterministic bundle")
	}
	b := c.Bundle()
	if _, e := Approve(c, c.Hash(), "33333333-3333-3333-3333-333333333333", b.RequesterID, testGate(true)); e == nil {
		t.Fatal("self approval")
	}
	actor := "22222222-2222-2222-2222-222222222222"
	a, e := Approve(c, c.Hash(), "33333333-3333-3333-3333-333333333333", actor, testGate(true))
	if e != nil {
		t.Fatal(e)
	}
	if ValidateActivation(c, a, "", b.Dependencies, "33333333-3333-3333-3333-333333333333", actor, testGate(true)) != nil {
		t.Fatal("exact artifact denied")
	}
	for _, base := range []string{"changed"} {
		if ValidateActivation(c, a, base, b.Dependencies, "33333333-3333-3333-3333-333333333333", actor, testGate(true)) == nil {
			t.Fatal("stale base")
		}
	}
	deps := b.Dependencies
	deps.ToolsHash = strings.Repeat("b", 64)
	if ValidateActivation(c, a, "", deps, "33333333-3333-3333-3333-333333333333", actor, testGate(true)) == nil {
		t.Fatal("stale tools")
	}
	if _, e := Approve(c, strings.Repeat("b", 64), "33333333-3333-3333-3333-333333333333", actor, testGate(true)); e == nil {
		t.Fatal("changed hash approved")
	}
	if ValidateActivation(c, a, "", b.Dependencies, "33333333-3333-3333-3333-333333333333", actor, testGate(false)) == nil {
		t.Fatal("authorization bypass")
	}
}
func TestApprovalCannotMoveAcrossTenantContext(t *testing.T) {
	c, _, _ := candidateFixture(t)
	a, e := Approve(c, c.Hash(), "33333333-3333-3333-3333-333333333333", "22222222-2222-2222-2222-222222222222", testGate(true))
	if e != nil {
		t.Fatal(e)
	}
	if ValidateActivation(c, a, "", c.Bundle().Dependencies, "44444444-4444-4444-4444-444444444444", "22222222-2222-2222-2222-222222222222", testGate(true)) == nil {
		t.Fatal("approval crossed tenant boundary")
	}
}
func TestRestoreRejectsChangedEvidenceSourceIRAndUnsupportedRuntime(t *testing.T) {
	c, _, p := candidateFixture(t)
	if _, e := Restore(c.JSON(), c.Hash(), []string{compiler.IRVersion}); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Bundle){
		func(b *Bundle) { b.Verification.Outcome = "FAIL" }, func(b *Bundle) { b.Verification.AcceptanceHash = strings.Repeat("c", 64) }, func(b *Bundle) { b.Dependencies.PolicyHash = "" }, func(b *Bundle) { b.CompilerVersion = "other" }, func(b *Bundle) { b.Sandbox.ProductionCredentialsMounted = true },
		func(b *Bundle) {
			var doc metadata.Document
			_ = json.Unmarshal(b.Source, &doc)
			doc.Definitions[0].Label = "Changed"
			b.Source, _ = json.Marshal(doc)
			b.SourceHash = Hash(b.Source)
		},
	} {
		b := c.Bundle()
		mutate(&b)
		raw, _ := json.Marshal(b)
		if _, e := Restore(raw, Hash(raw), []string{compiler.IRVersion}); e == nil {
			t.Fatal("changed evidence accepted")
		}
	}
	if _, e := Restore(c.JSON(), c.Hash(), []string{"2.0.0"}); e == nil {
		t.Fatal("unsupported runtime")
	}
	// A historical compiler version remains stored, with identical executable IR;
	// restoring never silently substitutes the compiler version of this binary.
	ir := p.IR()
	ir.CompilerVersion = "historical-compiler"
	raw, _ := json.Marshal(ir)
	old, e := compiler.Restore(raw, Hash(raw), []string{compiler.IRVersion})
	if e != nil || old.IR().CompilerVersion != "historical-compiler" {
		t.Fatal("historical compiler discarded")
	}
	pin := ExecutionPin{c.Hash(), "workflow.approval", "1"}
	before := pin
	if pin.Validate() != nil || pin != before {
		t.Fatal("execution pin")
	}
}
