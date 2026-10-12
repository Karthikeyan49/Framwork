package compiler

import (
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"reflect"
)

func ruleDefinition(id string, r metadata.Rule, e Entity) rules.Definition {
	var expr rules.Expression
	b, _ := json.Marshal(r.Expression)
	_ = json.Unmarshal(b, &expr)
	fs := []rules.Field{}
	for _, f := range e.Fields {
		t := rules.Type{Kind: f.Type, Currency: f.Currency, ReferenceTo: f.ReferenceTo}
		fs = append(fs, rules.Field{ID: f.ID, Type: t, Required: f.Required, EnumValues: f.EnumValues})
	}
	return rules.Definition{ID: id, EntityID: r.EntityRef, Version: r.DSLVersion, ResultType: r.ResultType, Fields: fs, Calendars: r.Calendars, Expression: expr}
}

// Rule returns a stored compiled plan. It does not compile source or run a rule.
// Legacy IR intentionally contains no executable DSL2 plan.
func (p Program) Rule(id string) (rules.Program, error) {
	if !p.Valid() {
		return rules.Program{}, rules.ErrInvalid
	}
	for _, plan := range p.IR().Rules {
		if plan.ID == id {
			raw, e := json.Marshal(plan)
			if e != nil {
				return rules.Program{}, e
			}
			return rules.Restore(raw, rules.Hash(raw))
		}
	}
	return rules.Program{}, rules.ErrInvalid
}
func validateStoredRules(ir IR) error {
	if ir.IRVersion == IRVersion {
		if ir.Application.SchemaVersion != metadata.SchemaVersion || len(ir.Rules) > 0 {
			return rules.ErrInvalid
		}
		return nil
	}
	if !(ir.IRVersion == RuleIRVersion && ir.Application.SchemaVersion == metadata.RuleSchemaVersion || ir.IRVersion == WorkflowIRVersion && ir.Application.SchemaVersion == metadata.WorkflowSchemaVersion) {
		return rules.ErrInvalid
	}
	index := 0
	for _, d := range ir.Application.Definitions {
		if d.Rule == nil {
			continue
		}
		if index >= len(ir.Rules) {
			return rules.ErrInvalid
		}
		plan := ir.Rules[index]
		index++
		var entity Entity
		for _, e := range ir.Entities {
			if e.ID == d.Rule.EntityRef {
				entity = e
			}
		}
		definition := ruleDefinition(d.ID, *d.Rule, entity)
		fs, cs, e := rules.CanonicalInputs(definition.Fields, definition.Calendars)
		if e != nil {
			return e
		}
		if plan.ID != d.ID || plan.EntityID != d.Rule.EntityRef || plan.Version != d.Rule.DSLVersion || plan.Root.Type.Kind != d.Rule.ResultType || !reflect.DeepEqual(plan.Fields, fs) || !reflect.DeepEqual(plan.Calendars, cs) {
			return rules.ErrInvalid
		}
		authored, _ := json.Marshal(definition.Expression)
		stored, _ := json.Marshal(rules.ExpressionOf(plan.Root))
		if string(authored) != string(stored) {
			return rules.ErrInvalid
		}
		raw, _ := json.Marshal(plan)
		if _, e = rules.Restore(raw, rules.Hash(raw)); e != nil {
			return e
		}
	}
	if index != len(ir.Rules) {
		return rules.ErrInvalid
	}
	return nil
}
