package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

func approvalDocument(t *testing.T) metadata.Document {
	t.Helper()
	var doc metadata.Document
	if e := json.Unmarshal(workflowSource(t).JSON(), &doc); e != nil {
		t.Fatal(e)
	}
	doc.SchemaVersion = metadata.ApprovalSchemaVersion
	for i := range doc.Definitions {
		d := &doc.Definitions[i]
		d.SchemaVersion = doc.SchemaVersion
		if d.Workflow != nil {
			d.Workflow.DSLVersion = workflow.TenantRoleVersion
			for j := range d.Workflow.States {
				s := &d.Workflow.States[j]
				if s.Type == "approval" {
					s.ApprovalRoles = []string{"finance_manager", "admin"}
				}
			}
		}
	}
	return doc
}
func TestApprovalRolesLegacyGoldenAndVersionedRestore(t *testing.T) {
	const legacyHash = "ea566c0483e6b67ecf2d9b94a9e99c75b397d1c4f16ca6c929ed5c8052c32f8b"
	raw, e := os.ReadFile("testdata/workflow-v1.2.ir.json")
	if e != nil {
		t.Fatal(e)
	}
	if digest(raw) != legacyHash {
		t.Fatal("pre-R5e artifact changed")
	}
	old, e := Restore(raw, legacyHash, SupportedIRVersions())
	if e != nil {
		t.Fatal(e)
	}
	compiled, ds := Compile(workflowSource(t), PlatformCatalog())
	if len(ds) > 0 || !bytes.Equal(compiled.JSON(), raw) {
		t.Fatal("legacy compiler output changed", ds)
	}
	w, e := old.Workflow("workflow.contact.review")
	if e != nil || w.Plan().DSLVersion != workflow.Version {
		t.Fatal("legacy plan unreadable", e)
	}
	// Historical compiler provenance must survive; restore is not a compile path.
	ir := old.IR()
	ir.CompilerVersion = "historical-m9-compiler"
	raw, _ = json.Marshal(ir)
	restored, e := Restore(raw, digest(raw), SupportedIRVersions())
	if e != nil || !bytes.Equal(restored.JSON(), raw) {
		t.Fatal("historical artifact rewritten", e)
	}
	doc := approvalDocument(t)
	v, _ := metadata.NewValidator()
	b, _ := json.Marshal(doc)
	source, e := v.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	p, ds := Compile(source, PlatformCatalog())
	if len(ds) > 0 || !p.Valid() || p.IR().IRVersion != ApprovalIRVersion {
		t.Fatal(ds)
	}
	slices.Reverse(doc.Definitions)
	for i := range doc.Definitions {
		if w := doc.Definitions[i].Workflow; w != nil {
			slices.Reverse(w.States)
			for j := range w.States {
				slices.Reverse(w.States[j].ApprovalRoles)
			}
		}
	}
	b, _ = json.Marshal(doc)
	source, e = v.Parse(b)
	if e != nil {
		t.Fatal(e)
	}
	reordered, ds := Compile(source, PlatformCatalog())
	if len(ds) > 0 || !bytes.Equal(p.JSON(), reordered.JSON()) {
		t.Fatal("approval role canonicalization", ds)
	}
	restored, e = Restore(p.JSON(), p.Hash(), SupportedIRVersions())
	if e != nil {
		t.Fatal(e)
	}
	w, e = restored.Workflow("workflow.contact.review")
	if e != nil || w.Plan().DSLVersion != workflow.TenantRoleVersion {
		t.Fatal("custom role restore", e)
	}
	if _, e = Restore(p.JSON(), p.Hash(), []string{IRVersion, RuleIRVersion, WorkflowIRVersion}); e == nil {
		t.Fatal("old reader silently accepts new IR")
	}
	ir = p.IR()
	ir.IRVersion = WorkflowIRVersion
	b, _ = json.Marshal(ir)
	if _, e = Restore(b, digest(b), SupportedIRVersions()); e == nil {
		t.Fatal("mixed version accepted")
	}
	plan := w.Plan()
	plan.DSLVersion = workflow.Version
	b, _ = json.Marshal(plan)
	if _, e = workflow.Restore(b, workflow.Hash(b)); e == nil {
		t.Fatal("legacy plan accepted custom roles")
	}
	ir = p.IR()
	for i := range ir.Workflows[0].States {
		if ir.Workflows[0].States[i].Type == "approval" {
			ir.Workflows[0].States[i].ApprovalRoles = []string{"owner"}
		}
	}
	b, _ = json.Marshal(ir)
	if _, e = Restore(b, digest(b), SupportedIRVersions()); e == nil {
		t.Fatal("role/source disagreement accepted")
	}
}
func TestApprovalRoleSchemaContracts(t *testing.T) {
	v, _ := metadata.NewValidator()
	for _, name := range []string{"Finance_Manager", "finance-manager", "", "_manager", strings.Repeat("a", 65), "finance manager", "owner\n"} {
		t.Run(name, func(t *testing.T) {
			doc := approvalDocument(t)
			for i := range doc.Definitions {
				if w := doc.Definitions[i].Workflow; w != nil {
					for j := range w.States {
						if w.States[j].Type == "approval" {
							w.States[j].ApprovalRoles = []string{name}
						}
					}
				}
			}
			b, _ := json.Marshal(doc)
			if _, e := v.Parse(b); e == nil {
				t.Fatal("invalid role admitted")
			}
		})
	}
	for _, version := range []string{metadata.SchemaVersion, metadata.RuleSchemaVersion, metadata.WorkflowSchemaVersion} {
		doc := approvalDocument(t)
		doc.SchemaVersion = version
		for i := range doc.Definitions {
			doc.Definitions[i].SchemaVersion = version
		}
		b, _ := json.Marshal(doc)
		if _, e := v.Parse(b); e == nil {
			t.Fatal("new roles implicitly enabled on old schema", version)
		}
	}
	doc := approvalDocument(t)
	for i := range doc.Definitions {
		if w := doc.Definitions[i].Workflow; w != nil {
			w.DSLVersion = workflow.Version
		}
	}
	b, _ := json.Marshal(doc)
	if _, e := v.Parse(b); e == nil {
		t.Fatal("mixed DSL/schema admitted")
	}
}
