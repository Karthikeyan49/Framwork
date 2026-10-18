package compiler

import (
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// toolInputKinds maps a trusted tool input type to the entity field types that
// may feed it (ADR036). Integers, exact decimals and booleans accept only their
// own kind; money, lists and every other type are rejected.
var toolInputKinds = map[string][]string{
	"string":  {"string", "enum", "reference", "date", "datetime"},
	"integer": {"integer"},
	"decimal": {"decimal"},
	"bool":    {"bool"},
}

// stringBound is the known maximum encoded length of a string-like field value.
// Free-form strings have no authored bound; tools.DecodeInput enforces the
// tool's MaxLength on the captured snapshot value before any send.
func stringBound(f Field) (int, bool) {
	switch f.Type {
	case "date":
		return len("2006-01-02"), true
	case "datetime":
		return len("2006-01-02T15:04:05.999999999+00:00"), true
	case "reference":
		return len("00000000-0000-0000-0000-000000000000"), true
	case "enum":
		n := 0
		for _, v := range f.EnumValues {
			n = max(n, len(v))
		}
		return n, true
	}
	return 0, false
}
func toolInputCompatible(input tools.Field, f Field) bool {
	if !contains(toolInputKinds[input.Type], f.Type) {
		return false
	}
	if n, known := stringBound(f); input.Type == "string" && known && n > input.MaxLength {
		return false
	}
	return true
}

// toolActivities resolves every 1.3.0 tool activity in the trusted catalog,
// type-checks its input mapping against the workflow entity and enforces
// approval dominance. Tenant allowlists are checked at execution, where the
// tenant is known. The returned resolver yields the pinned definition hash and
// its database-enforced contract.
func toolActivities(w *metadata.Workflow, path string, entity Entity, catalog tools.Catalog, add func(string, string, string)) (workflow.ToolResolver, bool) {
	fields := map[string]Field{}
	for _, f := range entity.Fields {
		fields[f.ID] = f
	}
	failed := false
	report := func(at, code, message string) {
		failed = true
		add(at, code, message)
	}
	states := append([]metadata.WorkflowState(nil), w.States...)
	sort.SliceStable(states, func(i, j int) bool { return states[i].ID < states[j].ID })
	pins := map[string]workflow.ToolResolution{}
	for _, s := range states {
		a := s.Activity
		if a == nil || a.ToolRef == "" {
			continue
		}
		at := path + "/" + s.ID + "/activity"
		definition, ok := catalog.Lookup(a.ToolRef, a.Version, "")
		if !ok {
			report(at, "unknown_tool", "Tool reference and version must be registered in the trusted tool catalog")
			continue
		}
		names := make([]string, 0, len(a.Inputs))
		for name := range a.Inputs {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			input, declared := definition.InputField(name)
			field, local := fields[a.Inputs[name]]
			switch {
			case !declared:
				report(at+"/inputs/"+name, "tool_input_unknown", "Mapped input is not declared by the tool")
			case !local:
				report(at+"/inputs/"+name, "tool_input_field", "Tool inputs must map to a field of the workflow entity")
			case !toolInputCompatible(input, field):
				report(at+"/inputs/"+name, "tool_input_type", "Entity field type is incompatible with the tool input type")
			}
		}
		for _, input := range definition.Input {
			if _, mapped := a.Inputs[input.Name]; input.Required && !mapped {
				report(at+"/inputs/"+input.Name, "tool_input_missing", "Every required tool input must be mapped")
			}
		}
		if definition.Approval == "REQUIRED" && !approvalDominates(w, s.ID) {
			report(at, "tool_requires_approval", "Every path to an approval-required tool must pass an approval state's approved transition")
		}
		pins[a.ToolRef+"@"+a.Version] = workflow.ToolResolution{Hash: definition.Hash(), Contract: workflow.ToolContractOf(definition)}
	}
	return func(ref, version string) (workflow.ToolResolution, bool) {
		pin, ok := pins[ref+"@"+version]
		return pin, ok
	}, failed
}

// approvalDominates reports whether every path from the initial state to target
// traverses an `approved` transition out of an approval state: target must be
// unreachable once those transitions are removed.
func approvalDominates(w *metadata.Workflow, target string) bool {
	states := map[string]metadata.WorkflowState{}
	for _, s := range w.States {
		states[s.ID] = s
	}
	seen := map[string]bool{w.InitialState: true}
	queue := []string{w.InitialState}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == target {
			return false
		}
		s := states[id]
		for _, t := range s.Transitions {
			if s.Type == "approval" && t.On == "approved" || seen[t.To] {
				continue
			}
			seen[t.To] = true
			queue = append(queue, t.To)
		}
	}
	return true
}
