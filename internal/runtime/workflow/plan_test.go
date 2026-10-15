package workflow

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"testing"
)

func testPlan(t *testing.T) Program {
	t.Helper()
	w := metadata.Workflow{DSLVersion: Version, Version: "1.0.0", EntityRef: "entity.item", Trigger: "manual", TimeoutMS: 1000, InitialState: "state.start", States: []metadata.WorkflowState{{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.done"}}}, {ID: "state.done", Type: "complete", Transitions: []metadata.Transition{}}}}
	p, e := CompileDefinition("workflow.item", w, []rules.Field{{ID: "field.item.id", Type: rules.Type{Kind: "string"}, Required: true}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestPlanIsImmutableAndClosed(t *testing.T) {
	p := testPlan(t)
	raw := p.JSON()
	raw[0] = 'x'
	copy := p.Plan()
	copy.States[0].ID = "state.hacked"
	restored, e := Restore(p.JSON(), p.Hash())
	if e != nil || restored.Plan().States[0].ID == "state.hacked" {
		t.Fatal("mutable program")
	}
	if _, e = Restore(append(p.JSON(), []byte(` {}`)...), Hash(append(p.JSON(), []byte(` {}`)...))); e == nil {
		t.Fatal("trailing data accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = Noop(ctx, nil, tenant.Access{}, ActivityRequest{ActivityRef: "local.noop", Version: ActivityVersion}); e == nil {
		t.Fatal("cancelled activity succeeded")
	}
}
func FuzzWorkflowRestore(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"dsl_version":"2.0.0"}`))
	w := metadata.Workflow{DSLVersion: Version, Version: "1.0.0", EntityRef: "entity.item", Trigger: "manual", TimeoutMS: 1000, InitialState: "state.start", States: []metadata.WorkflowState{{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.done"}}}, {ID: "state.done", Type: "complete", Transitions: []metadata.Transition{}}}}
	seed, e := CompileDefinition("workflow.item", w, []rules.Field{{ID: "field.item.id", Type: rules.Type{Kind: "string"}, Required: true}}, nil)
	if e != nil {
		f.Fatal(e)
	}
	f.Add(seed.JSON())
	tool, e := CompileDefinitionWithTools("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}, itemResolver(testToolHash))
	if e != nil {
		f.Fatal(e)
	}
	f.Add(tool.JSON())
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxPlanBytes {
			return
		}
		p, e := Restore(raw, Hash(raw))
		if e == nil {
			if !p.Valid() || p.Hash() != Hash(raw) {
				t.Fatal("invalid restored program")
			}
			if _, e = Restore(p.JSON(), p.Hash()); e != nil {
				t.Fatal("restore not stable")
			}
			for _, s := range p.Plan().States {
				if s.IsTool() && (p.Plan().DSLVersion != ToolDSLVersion || !toolHashPattern.MatchString(s.ToolHash)) || !s.IsTool() && s.ToolHash != "" {
					t.Fatal("unpinned or misplaced tool activity")
				}
				for _, tr := range s.Transitions {
					if tr.Condition != nil {
						b, _ := json.Marshal(tr.Condition)
						if _, e = rules.Restore(b, rules.Hash(b)); e != nil {
							t.Fatal("unverified condition")
						}
					}
				}
			}
		}
	})
}
