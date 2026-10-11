package compiler

import "sort"
import "github.com/Karthikeyan49/Framwork/internal/metadata"

func validateWorkflow(w *metadata.Workflow, path string, symbols map[string]metadata.Definition, catalog Catalog, expression expressionCheck, add func(string, string, string)) {
	states := map[string]metadata.WorkflowState{}
	for _, s := range w.States {
		if _, ok := states[s.ID]; ok {
			add(path, "duplicate_state", "Workflow state IDs must be unique")
		}
		states[s.ID] = s
	}
	if _, ok := states[w.InitialState]; !ok {
		add(path, "initial_state", "Initial state must exist")
	}
	terminal := false
	for _, s := range w.States {
		if s.Type == "complete" || s.Type == "failed" {
			terminal = true
			if len(s.Transitions) > 0 {
				add(path, "terminal_transition", "Terminal states cannot transition")
			}
		} else if len(s.Transitions) == 0 {
			add(path, "dead_end", "Nonterminal states require a completion path")
		}
		if s.Type == "approval" {
			sort.Strings(s.ApprovalRoles)
			for _, role := range s.ApprovalRoles {
				if !contains([]string{"owner", "admin", "member", "viewer"}, role) {
					add(path, "approval_role", "Approval roles must be known tenant roles")
				}
			}
		}
		defaults := 0
		for i, t := range s.Transitions {
			if t.Condition == nil {
				defaults++
				if i != len(s.Transitions)-1 {
					add(path, "branch_order", "An unconditional fallback must be the final transition")
				}
			}
			if _, ok := states[t.To]; !ok {
				add(path, "transition_target", "Transition target must exist")
			}
			if t.Condition != nil && expression(*t.Condition, w.EntityRef, path+"/condition") != "bool" {
				add(path, "transition_type", "Transition conditions must be boolean")
			}
		}
		if s.Type != "complete" && s.Type != "failed" && defaults != 1 {
			add(path, "missing_fallback", "Nonterminal states require an unconditional fallback transition")
		}
		if defaults > 1 {
			add(path, "ambiguous_branch", "A state can have only one unconditional fallback")
		}
		if (s.Type != "activity" && s.Activity != nil) || (s.Type != "approval" && len(s.ApprovalRoles) > 0) || (s.Type != "timer" && s.TimerMS != 0) {
			add(path, "state_options", "State options must match the state type")
		}
		if s.Activity != nil {
			a := s.Activity
			tool, ok := symbols[a.ToolRef]
			if !ok || tool.Tool == nil {
				add(path, "activity_tool", "Activity must reference a tool definition")
			}
			if !contains(catalog.Tools, a.ToolRef) {
				add(path, "unavailable_capability", "Activity tool has no trusted execution capability")
			}
			if a.Retry.InitialBackoffMS > a.Retry.MaxBackoffMS {
				add(path, "activity_retry", "Retry bounds are inconsistent")
			}
			if a.CompensationToolRef != "" {
				if d, ok := symbols[a.CompensationToolRef]; !ok || d.Tool == nil || !contains(catalog.Tools, a.CompensationToolRef) {
					add(path, "compensation_tool", "Compensation requires an available tool")
				}
			}
		}
	}
	if !terminal {
		add(path, "missing_terminal", "Workflow requires a completion or failure state")
	}
	colors := map[string]int{}
	var visit func(string)
	visit = func(id string) {
		if colors[id] == 1 {
			add(path, "workflow_cycle", "Unbounded workflow cycles are forbidden in V1")
			return
		}
		if colors[id] == 2 {
			return
		}
		colors[id] = 1
		for _, t := range states[id].Transitions {
			if _, exists := states[t.To]; exists {
				visit(t.To)
			}
		}
		colors[id] = 2
	}
	visit(w.InitialState)
	for id := range states {
		if colors[id] == 0 {
			add(path, "unreachable_state", "All workflow states must be reachable")
		}
	}
}
