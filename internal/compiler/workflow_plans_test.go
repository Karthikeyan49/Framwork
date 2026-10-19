package compiler

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"os"
	"testing"
)

func workflowSource(t *testing.T) metadata.Source {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/examples/workflow-draft-v1.2.json")
	if e != nil {
		t.Fatalf("%#v", e)
	}
	v, e := metadata.NewValidator()
	if e != nil {
		t.Fatal(e)
	}
	s, e := v.Parse(raw)
	if e != nil {
		t.Fatalf("metadata: %#v", e)
	}
	return s
}
func TestWorkflowPlansDeterminismRestoreAndSnapshotConditions(t *testing.T) {
	s := workflowSource(t)
	p, ds := Compile(s, PlatformCatalog())
	if !p.Valid() {
		t.Fatal(ds)
	}
	r, ds := Compile(s, PlatformCatalog())
	if len(ds) > 0 || p.Hash() != r.Hash() || !bytes.Equal(p.JSON(), r.JSON()) {
		t.Fatal("nondeterminism")
	}
	restored, e := Restore(p.JSON(), p.Hash(), SupportedIRVersions())
	if e != nil || !MatchesStoredSource(restored, s) {
		t.Fatal("restore", e)
	}
	w, e := restored.Workflow("workflow.contact.review")
	if e != nil {
		t.Fatal(e)
	}
	if w.Plan().DSLVersion != workflow.Version {
		t.Fatal("runtime version")
	}
	next, e := w.Next(context.Background(), "state.start", "always", map[string]json.RawMessage{"field.contact.status": json.RawMessage(`"active"`)})
	if e != nil || next != "state.activity" {
		t.Fatal("branch", next, e)
	}
	next, e = w.Next(context.Background(), "state.start", "always", map[string]json.RawMessage{"field.contact.status": json.RawMessage(`"inactive"`)})
	if e != nil || next != "state.failed" {
		t.Fatal("fallback", next, e)
	}
	ir := p.IR()
	ir.Workflows[0].States[0].ApprovalRoles = []string{"viewer"}
	raw, _ := json.Marshal(ir)
	if _, e = Restore(raw, digest(raw), SupportedIRVersions()); e == nil {
		t.Fatal("plan/source disagreement accepted")
	}
	ir = p.IR()
	for i := range ir.Workflows[0].States {
		if ir.Workflows[0].States[i].ID == "state.timer" {
			ir.Workflows[0].States[i].TimerMS = 500
		}
	}
	raw, _ = json.Marshal(ir)
	if _, e = Restore(raw, digest(raw), SupportedIRVersions()); e == nil {
		t.Fatal("mutated timer accepted")
	}
}
func TestWorkflowRejectsUnsafeGraphsCapabilitiesAndLegacyExecution(t *testing.T) {
	original := workflowSource(t)
	var doc metadata.Document
	_ = json.Unmarshal(original.JSON(), &doc)
	v, _ := metadata.NewValidator()
	cases := []struct {
		name   string
		change func(*metadata.Workflow)
	}{
		{"cycle", func(w *metadata.Workflow) { w.States[2].Transitions[0].To = "state.start" }},
		{"unavailable_activity", func(w *metadata.Workflow) { w.States[1].Activity.LocalRef = "local.shell" }},
		{"missing_rejection_path", func(w *metadata.Workflow) { w.States[3].Transitions = w.States[3].Transitions[:1] }},
		{"nonboolean_condition", func(w *metadata.Workflow) {
			w.States[0].Transitions[0].Condition = &metadata.Expression{Op: "literal", Value: json.RawMessage(`1`)}
		}},
		{"bad_retry", func(w *metadata.Workflow) {
			w.States[1].Activity.Retry.MaxBackoffMS = 100
			w.States[1].Activity.Retry.InitialBackoffMS = 1000
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, _ := json.Marshal(doc)
			var copy metadata.Document
			_ = json.Unmarshal(raw, &copy)
			for i := range copy.Definitions {
				if copy.Definitions[i].Workflow != nil {
					c.change(copy.Definitions[i].Workflow)
				}
			}
			raw, _ = json.Marshal(copy)
			s, e := v.Parse(raw)
			if e != nil {
				t.Fatal("schema fixture", e)
			}
			p, ds := Compile(s, PlatformCatalog())
			if p.Valid() || len(ds) == 0 {
				t.Fatal("unsafe plan accepted")
			}
		})
	}
	legacy, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	s, e := v.Parse(legacy)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := Compile(s, PlatformCatalog())
	if _, e = p.Workflow("workflow.contact.review"); e == nil {
		t.Fatal("legacy executed")
	}
}
