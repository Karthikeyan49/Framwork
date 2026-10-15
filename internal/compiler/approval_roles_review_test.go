package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

func TestApprovalRolesIndependentVersionAndPlanGuards(t *testing.T) {
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	doc := approvalDocument(t)
	raw, _ := json.Marshal(doc)
	source, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := Compile(source, PlatformCatalog())
	if len(ds) != 0 || !p.Valid() {
		t.Fatal("review fixture did not compile", ds)
	}
	mutations := []struct {
		name string
		edit func(*IR)
	}{
		{"IR downgrade", func(ir *IR) { ir.IRVersion = WorkflowIRVersion }},
		{"IR future", func(ir *IR) { ir.IRVersion = "1.4.0" }},
		{"plan DSL downgrade", func(ir *IR) { ir.Workflows[0].DSLVersion = workflow.Version }},
		{"plan DSL future", func(ir *IR) { ir.Workflows[0].DSLVersion = "2.2.0" }},
		{"missing plan", func(ir *IR) { ir.Workflows = nil }},
		{"extra plan", func(ir *IR) { ir.Workflows = append(ir.Workflows, ir.Workflows[0]) }},
		{"source role disagreement", func(ir *IR) {
			for i := range ir.Application.Definitions {
				if w := ir.Application.Definitions[i].Workflow; w != nil {
					for j := range w.States {
						if w.States[j].Type == "approval" {
							w.States[j].ApprovalRoles = []string{"other_manager"}
						}
					}
				}
			}
		}},
		{"plan role disagreement", func(ir *IR) {
			for i := range ir.Workflows[0].States {
				if ir.Workflows[0].States[i].Type == "approval" {
					ir.Workflows[0].States[i].ApprovalRoles = []string{"other_manager"}
				}
			}
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			ir := p.IR()
			tc.edit(&ir)
			raw, _ := json.Marshal(ir)
			if _, err := Restore(raw, digest(raw), SupportedIRVersions()); err == nil {
				t.Fatal("rehashed incompatible stored artifact restored")
			}
		})
	}
	legacy, err := os.ReadFile("testdata/workflow-v1.2.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	const hash = "ea566c0483e6b67ecf2d9b94a9e99c75b397d1c4f16ca6c929ed5c8052c32f8b"
	restored, err := Restore(legacy, hash, []string{WorkflowIRVersion})
	if err != nil || !bytes.Equal(restored.JSON(), legacy) || restored.IR().CompilerVersion != WorkflowCompilerVersion {
		t.Fatal("legacy-only dispatch changed pinned bytes/provenance")
	}
	if _, err := Restore(p.JSON(), p.Hash(), []string{WorkflowIRVersion}); err == nil {
		t.Fatal("legacy-only reader accepted 1.3")
	}
}

func TestApprovalRolesIndependentCountAndUniquenessBoundaries(t *testing.T) {
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		count int
		dup   bool
		valid bool
	}{{"one", 1, false, true}, {"hundred", 100, false, true}, {"empty", 0, false, false}, {"over budget", 101, false, false}, {"duplicate", 2, true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			doc := approvalDocument(t)
			roles := make([]string, tc.count)
			for i := range roles {
				roles[i] = fmt.Sprintf("r%03d", i)
			}
			if tc.dup {
				roles[1] = roles[0]
			}
			for i := range doc.Definitions {
				if w := doc.Definitions[i].Workflow; w != nil {
					for j := range w.States {
						if w.States[j].Type == "approval" {
							w.States[j].ApprovalRoles = roles
						}
					}
				}
			}
			raw, _ := json.Marshal(doc)
			source, err := v.Parse(raw)
			if !tc.valid {
				if err == nil {
					t.Fatal("role cardinality contract admitted invalid source")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			p, ds := Compile(source, PlatformCatalog())
			if len(ds) != 0 || !p.Valid() {
				t.Fatal("valid role boundary did not compile", ds)
			}
			if _, err = Restore(p.JSON(), p.Hash(), SupportedIRVersions()); err != nil {
				t.Fatal("valid role boundary did not restore")
			}
		})
	}
}
