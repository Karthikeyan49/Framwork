package compiler

import (
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

func (p Program) Workflow(id string) (workflow.Program, error) {
	if !p.Valid() {
		return workflow.Program{}, workflow.ErrInvalid
	}
	for _, x := range p.IR().Workflows {
		if x.ID == id {
			b, _ := json.Marshal(x)
			return workflow.Restore(b, workflow.Hash(b))
		}
	}
	return workflow.Program{}, workflow.ErrInvalid
}

// validateStoredWorkflows checks stored plans against their authored source
// without compiling. Tool pins cannot be derived from source; Restore only
// requires them well-formed and the runtime re-resolves each pin in the live
// trusted catalog before any send.
func validateStoredWorkflows(ir IR) error {
	tools := ir.IRVersion == ToolWorkflowIRVersion
	if ir.IRVersion != WorkflowIRVersion && !tools {
		if len(ir.Workflows) != 0 {
			return workflow.ErrInvalid
		}
		return nil
	}
	if !tools && ir.Application.SchemaVersion != metadata.WorkflowSchemaVersion || tools && ir.Application.SchemaVersion != metadata.ToolWorkflowSchemaVersion {
		return workflow.ErrInvalid
	}
	index := 0
	for _, d := range ir.Application.Definitions {
		if d.Workflow == nil {
			continue
		}
		if index >= len(ir.Workflows) {
			return workflow.ErrInvalid
		}
		plan := ir.Workflows[index]
		index++
		raw, _ := json.Marshal(plan)
		p, e := workflow.Restore(raw, workflow.Hash(raw))
		if e != nil {
			return e
		}
		if !tools && p.HasTools() {
			return workflow.ErrInvalid
		}
		var entity Entity
		for _, x := range ir.Entities {
			if x.ID == d.Workflow.EntityRef {
				entity = x
			}
		}
		fs := ruleDefinition(d.ID, metadata.Rule{EntityRef: d.Workflow.EntityRef}, entity).Fields
		if !p.MatchesDefinition(d.ID, *d.Workflow, fs) {
			return workflow.ErrInvalid
		}
	}
	if index != len(ir.Workflows) {
		return workflow.ErrInvalid
	}
	return nil
}
