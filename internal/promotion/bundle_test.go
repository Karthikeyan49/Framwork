package promotion

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/sandbox"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
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

// M6R's in-process approval token and activation check are gone (ADR039
// section 9): a 1.0.0 bundle stays restorable and identified by its exact hash,
// but it can no longer be staged, so it never gets a candidate row the
// approval and activation ports would accept.
func TestFrozenCandidateIsDeterministicExactAndNotStageable(t *testing.T) {
	c, s, p := candidateFixture(t)
	same, e := Build(s, p, c.Bundle())
	if e != nil || same.Hash() != c.Hash() {
		t.Fatal("nondeterministic bundle")
	}
	b := c.Bundle()
	h := c.Header()
	if h.Version != "1.0.0" || h.TenantID != "" || h.AppID == "" || h.RequesterID != b.RequesterID || !h.HighRisk || h.Dependencies != b.Dependencies || h.IRHash != b.IRHash {
		t.Fatalf("legacy header %+v", h)
	}
	if _, e := Restore(c.JSON(), strings.Repeat("b", 64), []string{compiler.IRVersion}); e == nil {
		t.Fatal("bytes restored under another hash")
	}
	for _, gate := range []Gate{testGate(true), testGate(false), nil} {
		if staged, e := (Store{}).Stage(context.Background(), noSQL{t}, tenant.Access{}, c, gate); !errors.Is(e, ErrGate) || staged {
			t.Fatal("bundle 1.0.0 reached the stage port", e)
		}
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
