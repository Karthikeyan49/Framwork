package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
)

const testToolHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func itemFields() []rules.Field {
	return []rules.Field{
		{ID: "field.item.id", Type: rules.Type{Kind: "string"}, Required: true},
		{ID: "field.item.qty", Type: rules.Type{Kind: "integer"}},
		{ID: "field.item.note", Type: rules.Type{Kind: "string"}},
	}
}
func itemResolver(hash string) ToolResolver {
	return func(ref, version string) (string, bool) {
		return hash, ref == "tool.vendor_lookup" && version == "1.0.0"
	}
}
func toolActivity() *metadata.Activity {
	return &metadata.Activity{ToolRef: "tool.vendor_lookup", Version: "1.0.0", Inputs: map[string]string{"vendor_id": "field.item.id"}, Retry: metadata.Retry{MaxAttempts: 2, InitialBackoffMS: 100, MaxBackoffMS: 1000}}
}
func toolWorkflow() metadata.Workflow {
	return metadata.Workflow{DSLVersion: Version, Version: "1.0.0", EntityRef: "entity.item", Trigger: "manual", TimeoutMS: 60000, InitialState: "state.start", States: []metadata.WorkflowState{
		{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.call"}}},
		{ID: "state.call", Type: "activity", Activity: toolActivity(), Transitions: []metadata.Transition{{To: "state.done"}}},
		{ID: "state.done", Type: "complete", Transitions: []metadata.Transition{}},
	}}
}
func toolProgram(t *testing.T) Program {
	t.Helper()
	p, e := CompileDefinitionWithTools("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}, itemResolver(testToolHash))
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func restorePlan(plan Plan) error {
	raw, _ := json.Marshal(plan)
	_, e := Restore(raw, Hash(raw))
	return e
}
func planState(plan *Plan, id string) *State {
	for i := range plan.States {
		if plan.States[i].ID == id {
			return &plan.States[i]
		}
	}
	return nil
}

func TestToolPlanPinsHashAndUsesDSL210(t *testing.T) {
	p := toolProgram(t)
	plan := p.Plan()
	call, _ := p.State("state.call")
	if plan.DSLVersion != ToolDSLVersion || ToolDSLVersion != "2.1.0" || !p.HasTools() || !call.IsTool() || call.ToolHash != testToolHash {
		t.Fatal("tool plan is not pinned", plan.DSLVersion, call.ToolHash)
	}
	if call.Activity.LocalRef != "" || call.Activity.TimeoutMS != 0 || !reflect.DeepEqual(call.Activity.Inputs, map[string]string{"vendor_id": "field.item.id"}) {
		t.Fatal("tool activity shape")
	}
	if raw := string(p.JSON()); !strings.Contains(raw, `"tool_hash":"`+testToolHash+`"`) || strings.Contains(raw, `"timeout_ms":0`) {
		t.Fatal("canonical encoding", raw)
	}
	again, e := CompileDefinitionWithTools("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}, itemResolver(testToolHash))
	if e != nil || again.Hash() != p.Hash() || !bytes.Equal(again.JSON(), p.JSON()) {
		t.Fatal("tool plan is nondeterministic")
	}
	other, e := CompileDefinitionWithTools("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}, itemResolver(strings.Repeat("a", 64)))
	if e != nil || other.Hash() == p.Hash() {
		t.Fatal("pin is not part of plan identity")
	}
	restored, e := Restore(p.JSON(), p.Hash())
	if e != nil || !reflect.DeepEqual(restored.Plan(), plan) {
		t.Fatal("restore", e)
	}
	next, e := restored.Next(t.Context(), "state.call", "always", nil)
	if e != nil || next != "state.done" {
		t.Fatal("tool state transition", next, e)
	}
}

func TestToolActivitiesRequireAResolverThatKnowsThem(t *testing.T) {
	if _, e := CompileDefinition("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}); e == nil {
		t.Fatal("legacy entry point admitted a tool")
	}
	if _, e := CompileDefinitionWithTools("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}, nil); e == nil {
		t.Fatal("nil resolver admitted a tool")
	}
	denied := func(string, string) (string, bool) { return testToolHash, false }
	if _, e := CompileDefinitionWithTools("workflow.item", toolWorkflow(), itemFields(), []string{"local.noop"}, denied); e == nil {
		t.Fatal("unresolved tool admitted")
	}
	w := toolWorkflow()
	w.DSLVersion = ToolDSLVersion
	if _, e := CompileDefinitionWithTools("workflow.item", w, itemFields(), []string{"local.noop"}, itemResolver(testToolHash)); e == nil {
		t.Fatal("authored DSL 2.1.0 accepted; only the compiler may select it")
	}
	// A tool-free workflow never consults the resolver and stays DSL 2.0.0.
	consulted := false
	free := func(string, string) (string, bool) { consulted = true; return testToolHash, true }
	plain := testPlanWorkflow()
	p, e := CompileDefinitionWithTools("workflow.item", plain, []rules.Field{{ID: "field.item.id", Type: rules.Type{Kind: "string"}, Required: true}}, nil, free)
	if e != nil || consulted || p.Plan().DSLVersion != Version || p.HasTools() {
		t.Fatal("tool-free plan changed", e)
	}
}

func testPlanWorkflow() metadata.Workflow {
	return metadata.Workflow{DSLVersion: Version, Version: "1.0.0", EntityRef: "entity.item", Trigger: "manual", TimeoutMS: 1000, InitialState: "state.start", States: []metadata.WorkflowState{{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.done"}}}, {ID: "state.done", Type: "complete", Transitions: []metadata.Transition{}}}}
}

// This hash was produced before DSL 2.1.0 existed (base a6889dd). Admitting
// tools must not change one byte of a tool-free plan.
const legacyTestPlanHash = "1221fc184595b985f227df148c028be77566a49e494d0af92f22b77b2af09920"

func TestToolFreePlansKeepLegacyBytesAndHash(t *testing.T) {
	p := testPlan(t)
	if p.Hash() != legacyTestPlanHash || p.Plan().DSLVersion != Version {
		t.Fatal("tool-free plan identity changed", p.Hash())
	}
	if raw := string(p.JSON()); strings.Contains(raw, "tool_hash") || strings.Contains(raw, `"inputs"`) {
		t.Fatal("tool fields leaked into a tool-free plan", raw)
	}
	via, e := CompileDefinitionWithTools("workflow.item", testPlanWorkflow(), []rules.Field{{ID: "field.item.id", Type: rules.Type{Kind: "string"}, Required: true}}, nil, itemResolver(testToolHash))
	if e != nil || via.Hash() != p.Hash() || !bytes.Equal(via.JSON(), p.JSON()) {
		t.Fatal("variant entry point changed tool-free bytes")
	}
	// A DSL 2.0.0 plan can never carry a tool, however it is assembled.
	plan := toolProgram(t).Plan()
	plan.DSLVersion = Version
	if restorePlan(plan) == nil {
		t.Fatal("DSL 2.0.0 accepted a tool_ref")
	}
	// A local-only plan labelled 2.1.0 is rejected: 2.1.0 means "has a tool".
	local := p.Plan()
	local.DSLVersion = ToolDSLVersion
	if restorePlan(local) == nil {
		t.Fatal("DSL 2.1.0 accepted without a tool state")
	}
}

func TestValidatePlanRejectsMalformedToolStates(t *testing.T) {
	if e := restorePlan(toolProgram(t).Plan()); e != nil {
		t.Fatal(e)
	}
	many := map[string]string{}
	for i := 0; i < MaxToolInputs+1; i++ {
		many[fmt.Sprintf("in_%02d", i)] = "field.item.id"
	}
	exactly := map[string]string{}
	for i := 0; i < MaxToolInputs; i++ {
		exactly[fmt.Sprintf("in_%02d", i)] = "field.item.id"
	}
	cases := map[string]func(*State){
		"tool ref without prefix":    func(s *State) { s.Activity.ToolRef = "vendor_lookup" },
		"tool ref uppercase":         func(s *State) { s.Activity.ToolRef = "tool.Vendor" },
		"tool ref too long":          func(s *State) { s.Activity.ToolRef = "tool." + strings.Repeat("a", 156) },
		"local ref shaped as tool":   func(s *State) { s.Activity.ToolRef = "local.noop" },
		"version not semver":         func(s *State) { s.Activity.Version = "1.0" },
		"version leading zero":       func(s *State) { s.Activity.Version = "01.0.0" },
		"version empty":              func(s *State) { s.Activity.Version = "" },
		"hash empty":                 func(s *State) { s.ToolHash = "" },
		"hash short":                 func(s *State) { s.ToolHash = testToolHash[:63] },
		"hash long":                  func(s *State) { s.ToolHash = testToolHash + "0" },
		"hash uppercase":             func(s *State) { s.ToolHash = strings.ToUpper(testToolHash) },
		"hash not hex":               func(s *State) { s.ToolHash = strings.Repeat("g", 64) },
		"inputs missing":             func(s *State) { s.Activity.Inputs = nil },
		"inputs empty":               func(s *State) { s.Activity.Inputs = map[string]string{} },
		"inputs over limit":          func(s *State) { s.Activity.Inputs = many },
		"input name uppercase":       func(s *State) { s.Activity.Inputs = map[string]string{"Vendor": "field.item.id"} },
		"input name leading digit":   func(s *State) { s.Activity.Inputs = map[string]string{"1vendor": "field.item.id"} },
		"input name too long":        func(s *State) { s.Activity.Inputs = map[string]string{"a" + strings.Repeat("b", 64): "field.item.id"} },
		"input field not in plan":    func(s *State) { s.Activity.Inputs = map[string]string{"vendor_id": "field.other.id"} },
		"input field malformed":      func(s *State) { s.Activity.Inputs = map[string]string{"vendor_id": "item.id"} },
		"input field empty":          func(s *State) { s.Activity.Inputs = map[string]string{"vendor_id": ""} },
		"local ref set":              func(s *State) { s.Activity.LocalRef = "local.noop" },
		"compensation local ref":     func(s *State) { s.Activity.CompensationLocalRef = "local.noop" },
		"compensation tool ref":      func(s *State) { s.Activity.CompensationToolRef = "tool.vendor_lookup" },
		"timeout set":                func(s *State) { s.Activity.TimeoutMS = 500 },
		"retry attempts zero":        func(s *State) { s.Activity.Retry.MaxAttempts = 0 },
		"retry attempts over limit":  func(s *State) { s.Activity.Retry.MaxAttempts = 11 },
		"retry initial too small":    func(s *State) { s.Activity.Retry.InitialBackoffMS = 99 },
		"retry max below initial":    func(s *State) { s.Activity.Retry.MaxBackoffMS = 99 },
		"retry max over limit":       func(s *State) { s.Activity.Retry.MaxBackoffMS = 3600001 },
		"hash on non-activity state": func(s *State) { s.Type = "timer"; s.Activity = nil; s.TimerMS = 100 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			plan := toolProgram(t).Plan()
			mutate(planState(&plan, "state.call"))
			if restorePlan(plan) == nil {
				t.Fatal("malformed tool state restored")
			}
		})
	}
	t.Run("boundary inputs are accepted", func(t *testing.T) {
		plan := toolProgram(t).Plan()
		planState(&plan, "state.call").Activity.Inputs = exactly
		if e := restorePlan(plan); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("hash on local state", func(t *testing.T) {
		plan := toolProgram(t).Plan()
		planState(&plan, "state.start").ToolHash = testToolHash
		if restorePlan(plan) == nil {
			t.Fatal("pin on a non-tool state restored")
		}
	})
	t.Run("local activity beside a tool", func(t *testing.T) {
		w := toolWorkflow()
		w.States = append(w.States, metadata.WorkflowState{ID: "state.local", Type: "activity", Activity: &metadata.Activity{LocalRef: "local.noop", Version: ActivityVersion, TimeoutMS: 500, Retry: metadata.Retry{MaxAttempts: 1, InitialBackoffMS: 100, MaxBackoffMS: 100}}, Transitions: []metadata.Transition{{To: "state.done"}}})
		w.States[1].Transitions = []metadata.Transition{{To: "state.local"}}
		p, e := CompileDefinitionWithTools("workflow.item", w, itemFields(), []string{"local.noop"}, itemResolver(testToolHash))
		if e != nil {
			t.Fatal(e)
		}
		local, _ := p.State("state.local")
		if local.IsTool() || local.ToolHash != "" || local.Activity.TimeoutMS != 500 {
			t.Fatal("local activity changed")
		}
		plan := p.Plan()
		planState(&plan, "state.local").Activity.Inputs = map[string]string{"vendor_id": "field.item.id"}
		if restorePlan(plan) == nil {
			t.Fatal("local activity with tool inputs restored")
		}
		plan = p.Plan()
		planState(&plan, "state.local").ToolHash = testToolHash
		if restorePlan(plan) == nil {
			t.Fatal("local activity with a pin restored")
		}
		plan = p.Plan()
		planState(&plan, "state.local").Activity.TimeoutMS = 0
		if restorePlan(plan) == nil {
			t.Fatal("local activity without a timeout restored")
		}
	})
}

func TestFieldIDsIncludeToolInputsAndConditions(t *testing.T) {
	if got := testPlan(t).FieldIDs(); len(got) != 0 {
		t.Fatal("tool-free plan without conditions captured fields", got)
	}
	if got := toolProgram(t).FieldIDs(); !reflect.DeepEqual(got, []string{"field.item.id"}) {
		t.Fatal("tool inputs not captured", got)
	}
	w := toolWorkflow()
	w.States[0].Transitions = []metadata.Transition{
		{To: "state.call", Condition: &metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "field", FieldRef: "field.item.qty"}, {Op: "literal", Value: json.RawMessage(`1`)}}}},
		{To: "state.done"},
	}
	w.States[1].Activity.Inputs = map[string]string{"vendor_id": "field.item.id", "note": "field.item.note", "again": "field.item.id"}
	p, e := CompileDefinitionWithTools("workflow.item", w, itemFields(), []string{"local.noop"}, itemResolver(testToolHash))
	if e != nil {
		t.Fatal(e)
	}
	if got := p.FieldIDs(); !reflect.DeepEqual(got, []string{"field.item.id", "field.item.note", "field.item.qty"}) {
		t.Fatal("condition and tool fields must merge sorted and unique", got)
	}
	// Existing tool-free workflows with a condition keep exactly their old set.
	free := testPlanWorkflow()
	free.States[0].Transitions = []metadata.Transition{
		{To: "state.done", Condition: &metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "field", FieldRef: "field.item.qty"}, {Op: "literal", Value: json.RawMessage(`1`)}}}},
		{To: "state.done"},
	}
	q, e := CompileDefinition("workflow.item", free, itemFields(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if got := q.FieldIDs(); !reflect.DeepEqual(got, []string{"field.item.qty"}) {
		t.Fatal("tool-free field set changed", got)
	}
}

func TestMatchesDefinitionComparesToolPlansWithoutAHash(t *testing.T) {
	fields := itemFields()
	p := toolProgram(t)
	if !p.MatchesDefinition("workflow.item", toolWorkflow(), fields) {
		t.Fatal("stored tool plan disagrees with its source")
	}
	cases := map[string]func(*metadata.Workflow){
		"input remapped": func(w *metadata.Workflow) {
			w.States[1].Activity.Inputs = map[string]string{"vendor_id": "field.item.note"}
		},
		"input renamed":   func(w *metadata.Workflow) { w.States[1].Activity.Inputs = map[string]string{"id": "field.item.id"} },
		"input added":     func(w *metadata.Workflow) { w.States[1].Activity.Inputs["note"] = "field.item.note" },
		"tool swapped":    func(w *metadata.Workflow) { w.States[1].Activity.ToolRef = "tool.other" },
		"version changed": func(w *metadata.Workflow) { w.States[1].Activity.Version = "1.1.0" },
		"retry changed":   func(w *metadata.Workflow) { w.States[1].Activity.Retry.MaxAttempts = 3 },
		"became local": func(w *metadata.Workflow) {
			w.States[1].Activity = &metadata.Activity{LocalRef: "local.noop", Version: ActivityVersion, TimeoutMS: 500, Retry: toolActivity().Retry}
		},
		"authored dsl 2.1.0":  func(w *metadata.Workflow) { w.DSLVersion = ToolDSLVersion },
		"timeout on the tool": func(w *metadata.Workflow) { w.States[1].Activity.TimeoutMS = 500 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := toolWorkflow()
			mutate(&w)
			if p.MatchesDefinition("workflow.item", w, fields) {
				t.Fatal("source/plan disagreement accepted")
			}
		})
	}
	// The source has no hash: a source-less pin swap is the runtime's to catch
	// against the live catalog, but a malformed or missing pin never matches.
	plan := p.Plan()
	planState(&plan, "state.call").ToolHash = strings.Repeat("f", 64)
	raw, _ := json.Marshal(plan)
	swapped, e := Restore(raw, Hash(raw))
	if e != nil || !swapped.MatchesDefinition("workflow.item", toolWorkflow(), fields) || swapped.Hash() == p.Hash() {
		t.Fatal("a well-formed pin is compared by plan identity, not by source", e)
	}
	// A tool-free plan must not match a source that now declares a tool, and
	// the 2.0.0 tool-free comparison is unchanged.
	if testPlan(t).MatchesDefinition("workflow.item", toolWorkflow(), fields) {
		t.Fatal("tool-free plan matched a tool source")
	}
	if !testPlan(t).MatchesDefinition("workflow.item", testPlanWorkflow(), []rules.Field{{ID: "field.item.id", Type: rules.Type{Kind: "string"}, Required: true}}) {
		t.Fatal("tool-free comparison regressed")
	}
}
