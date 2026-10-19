// Package compiler converts validated authoring source into immutable, typed IR.
package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

const Version = "1.0.0"
const IRVersion = "1.0.0"
const RuleIRVersion = "1.1.0"
const RuleCompilerVersion = "1.1.0"
const WorkflowIRVersion = "1.2.0"
const WorkflowCompilerVersion = "1.2.0"

// ToolWorkflowIRVersion is emitted only for opt-in schema 1.3.0 (ADR100).
const ToolWorkflowIRVersion = "1.3.0"
const ToolWorkflowCompilerVersion = "1.3.0"

func SupportedIRVersions() []string {
	return []string{IRVersion, RuleIRVersion, WorkflowIRVersion, ToolWorkflowIRVersion}
}

const SchemaSnapshot = "hybrid-preview-v1"

type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Catalog struct {
	Version     string   `json:"version"`
	Permissions []string `json:"permissions"`
	Tools       []string `json:"tools"`
	// ToolCatalog is the operator-owned trusted tool registry (ADR100). It is
	// excluded from CatalogHash: each resolved definition's canonical hash is
	// pinned inside the workflow plan instead, so existing IR hashes are stable.
	ToolCatalog tools.Catalog `json:"-"`
}

func PlatformCatalog() Catalog {
	return Catalog{Version: "platform-v1", Permissions: []string{"data.read", "data.write"}, Tools: []string{}}
}

type Field struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	CoreID      bool     `json:"core_id"`
	EnumValues  []string `json:"enum_values,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	ReferenceTo string   `json:"reference_to,omitempty"`
}
type Entity struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Fields []Field `json:"fields"`
}
type TestCase struct {
	Definition string `json:"definition"`
	Kind       string `json:"kind"`
}
type MigrationPlan struct {
	Class string   `json:"class"`
	Steps []string `json:"steps"`
}
type IR struct {
	IRVersion       string            `json:"ir_version"`
	CompilerVersion string            `json:"compiler_version"`
	CatalogHash     string            `json:"catalog_hash"`
	SchemaSnapshot  string            `json:"schema_snapshot"`
	Application     metadata.Document `json:"application"`
	Entities        []Entity          `json:"entities"`
	Migration       MigrationPlan     `json:"migration"`
	Tests           []TestCase        `json:"tests"`
	Rules           []rules.Plan      `json:"rules,omitempty"`
	Workflows       []workflow.Plan   `json:"workflows,omitempty"`
}

// Program is minted by Compile. Copies of IR cannot mutate its trusted state.
type Program struct {
	raw        []byte
	hash       string
	sourceHash string
}

func (p Program) MatchesSource(s metadata.Source) bool {
	return p.sourceHash != "" && p.sourceHash == digest(s.JSON())
}
func (p Program) Valid() bool           { return len(p.raw) > 0 && p.hash != "" }
func (p Program) Hash() string          { return p.hash }
func (p Program) JSON() json.RawMessage { return append(json.RawMessage(nil), p.raw...) }
func (p Program) IR() IR                { var ir IR; _ = json.Unmarshal(p.raw, &ir); return ir }
func (p Program) Entity(id string) (Entity, bool) {
	for _, e := range p.IR().Entities {
		if e.ID == id {
			return e, true
		}
	}
	return Entity{}, false
}
func digest(raw []byte) string { d := sha256.Sum256(raw); return hex.EncodeToString(d[:]) }
func Compile(source metadata.Source, catalog Catalog) (Program, []Diagnostic) {
	var doc metadata.Document
	diagnostics := []Diagnostic{}
	add := func(path, code, message string) {
		if len(diagnostics) < 100 {
			diagnostics = append(diagnostics, Diagnostic{path, code, message})
		}
	}
	if len(source.JSON()) == 0 || json.Unmarshal(source.JSON(), &doc) != nil {
		return Program{}, []Diagnostic{{"", "invalid_source", "A validated source is required"}}
	}
	if catalog.Version == "" {
		return Program{}, []Diagnostic{{"", "invalid_catalog", "A versioned trusted catalog is required"}}
	}
	catalog.Permissions = append([]string{}, catalog.Permissions...)
	catalog.Tools = append([]string{}, catalog.Tools...)
	sort.Strings(catalog.Permissions)
	sort.Strings(catalog.Tools)
	if doc.BaseVersion != nil {
		add("/base_version", "unavailable_base", "Production base compilation requires a release/schema adapter unavailable in V1 preview")
	}
	catalogJSON, _ := json.Marshal(catalog)
	sort.Slice(doc.Definitions, func(i, j int) bool { return doc.Definitions[i].ID < doc.Definitions[j].ID })
	symbols := map[string]metadata.Definition{}
	for _, d := range doc.Definitions {
		symbols[d.ID] = d
	}
	resolve := func(id, kind, path string) (metadata.Definition, bool) {
		d, ok := symbols[id]
		if !ok || d.Kind != kind {
			add(path, "unknown_symbol", "Reference must resolve to the required definition kind")
			return metadata.Definition{}, false
		}
		return d, true
	}
	fields := map[string]Field{}
	for _, d := range doc.Definitions {
		if d.Field == nil {
			continue
		}
		f := d.Field
		if _, ok := resolve(f.EntityRef, "entity", "/"+d.ID+"/entity_ref"); !ok {
			continue
		}
		core := d.ID == "field."+strings.TrimPrefix(f.EntityRef, "entity.")+".id"
		if core && (f.Type != "string" || !f.Required) {
			add("/"+d.ID, "core_id_type", "The reserved record identifier must be a required string")
		}
		if f.Type == "reference" {
			resolve(f.ReferenceTo, "entity", "/"+d.ID+"/reference_to")
		}
		values := append([]string{}, f.EnumValues...)
		sort.Strings(values)
		f.EnumValues = values
		fields[d.ID] = Field{d.ID, d.Label, f.Type, f.Required, core, values, f.Currency, f.ReferenceTo}
		if f.Type != "enum" && len(f.EnumValues) > 0 || f.Type != "money" && f.Currency != "" || f.Type != "reference" && f.ReferenceTo != "" {
			add("/"+d.ID, "field_options", "Field options must match its type")
		}
	}
	entities := []Entity{}
	tests := []TestCase{}
	membership := map[string]string{}
	for i := range doc.Definitions {
		d := &doc.Definitions[i]
		if d.Entity == nil {
			continue
		}
		sort.Strings(d.Entity.Fields)
		e := Entity{ID: d.ID, Label: d.Label, Fields: []Field{}}
		if len(d.Entity.Fields) < 1 || len(d.Entity.Fields) > 32 {
			add("/"+d.ID, "entity_field_budget", "Runtime entities require 1–32 fields")
		}
		for _, id := range d.Entity.Fields {
			fd, ok := resolve(id, "field", "/"+d.ID+"/fields")
			if !ok {
				continue
			}
			if fd.Field.EntityRef != d.ID {
				add("/"+d.ID+"/fields", "field_owner", "Entity fields must belong to the entity")
				continue
			}
			membership[id] = d.ID
			e.Fields = append(e.Fields, fields[id])
			tests = append(tests, TestCase{id, "type"})
			if fd.Field.Required {
				tests = append(tests, TestCase{id, "required"})
			}
		}
		tests = append(tests, TestCase{d.ID, "tenant_isolation"}, TestCase{d.ID, "authorization"})
		entities = append(entities, e)
	}
	for _, definition := range doc.Definitions {
		id := definition.ID
		if definition.Field != nil && membership[id] == "" {
			add("/"+id, "orphan_field", "Every field must be listed by its owning entity")
		}
	}
	expression := expressionChecker(fields, membership, add)
	rulePlans := []rules.Plan{}
	workflowPlans := []workflow.Plan{}
	componentNodes := 0
	var component func(metadata.Component, string, string, bool, int)
	component = func(c metadata.Component, owner, path string, inForm bool, depth int) {
		componentNodes++
		if componentNodes > 1000 || depth > 8 {
			add(path, "component_budget", "UI component count/depth exceeds renderer limits")
			return
		}
		if c.Type == "form" && inForm {
			add(path, "nested_form", "Forms cannot contain another form")
		}
		containers := map[string]bool{"form": true, "section": true, "tabs": true}
		inputTypes := map[string][]string{"text_input": {"string"}, "number_input": {"integer", "decimal"}, "money_input": {"money"}, "date_input": {"date", "datetime"}, "select": {"enum", "bool"}, "relation_picker": {"reference"}, "status_badge": {"enum", "string", "bool"}}
		if c.FieldRef != "" {
			f, ok := fields[c.FieldRef]
			if !ok || membership[c.FieldRef] != owner {
				add(path, "component_field", "Component field must belong to its page entity")
			} else if allowed, exists := inputTypes[c.Type]; exists && !contains(allowed, f.Type) {
				add(path, "component_type", "Input component is incompatible with its field type")
			}
		}
		if _, input := inputTypes[c.Type]; input && c.FieldRef == "" {
			add(path, "component_field", "Input components require a field reference")
		}
		if c.EntityRef != "" && c.EntityRef != owner {
			add(path, "component_entity", "Component entity must match its page entity")
		}
		if len(c.Children) > 0 && !containers[c.Type] {
			add(path, "component_children", "Only layout components accept children")
		}
		// Attachments/actions/charts require later governed capabilities, never an inert success.
		if c.Type == "file_input" || c.Type == "button" || c.Type == "chart" || c.Type == "kpi" {
			add(path, "unavailable_component", "This component requires a capability unavailable in this compiler version")
		}
		for i, child := range c.Children {
			component(child, owner, fmt.Sprintf("%s/children/%d", path, i), inForm || c.Type == "form", depth+1)
		}
	}
	for i := range doc.Definitions {
		d := &doc.Definitions[i]
		path := "/" + d.ID
		if r := d.Relationship; r != nil {
			_, a := resolve(r.FromEntity, "entity", path)
			_, b := resolve(r.ToEntity, "entity", path)
			f, fok := fields[r.FromField]
			target, tok := fields[r.ToField]
			if !a || !b || !fok || !tok || membership[r.FromField] != r.FromEntity || membership[r.ToField] != r.ToEntity {
				add(path, "invalid_relationship", "Relationship endpoints must resolve to their declared entities")
			} else if r.Cardinality != "many_to_one" || f.Type != "reference" || f.ReferenceTo != r.ToEntity || !target.CoreID {
				add(path, "relationship_budget", "V1 relationships require a reference to the target record identifier and many_to_one cardinality")
			}
		}
		if p := d.Page; p != nil {
			resolve(p.EntityRef, "entity", path)
			for j, c := range p.Components {
				component(c, p.EntityRef, fmt.Sprintf("%s/components/%d", path, j), false, 0)
			}
		}
		if r := d.Rule; r != nil {
			resolve(r.EntityRef, "entity", path)
			if doc.SchemaVersion == metadata.RuleSchemaVersion || metadata.WorkflowSchema(doc.SchemaVersion) {
				var entity Entity
				for _, e := range entities {
					if e.ID == r.EntityRef {
						entity = e
					}
				}
				definition := ruleDefinition(d.ID, *r, entity)
				executable, err := rules.Compile(definition)
				if err != nil {
					add(path, "rule_type", "Rule V2 violates type, currency, calendar or resource contracts")
				} else {
					plan := executable.Plan()
					rulePlans = append(rulePlans, plan)
					r.Calendars = plan.Calendars
					tests = append(tests, TestCase{d.ID, "rule_determinism"})
				}
			} else {
				typ := expression(r.Expression, r.EntityRef, path+"/expression")
				if typ != r.ResultType && !(r.ResultType == "decimal" && typ == "integer") {
					add(path, "rule_type", "Rule result type must match its declared expression type")
				}
			}
		}
		if w := d.Workflow; w != nil {
			resolve(w.EntityRef, "entity", path)
			if metadata.WorkflowSchema(doc.SchemaVersion) {
				var entity Entity
				for _, e := range entities {
					if e.ID == w.EntityRef {
						entity = e
					}
				}
				definition := ruleDefinition(d.ID, metadata.Rule{EntityRef: w.EntityRef}, entity)
				var resolver workflow.ToolResolver
				toolErrors := false
				if doc.SchemaVersion == metadata.ToolWorkflowSchemaVersion {
					resolver, toolErrors = toolActivities(w, path, entity, catalog.ToolCatalog, add)
				}
				executable, err := workflow.CompileDefinitionWithTools(d.ID, *w, definition.Fields, []string{"local.noop"}, resolver)
				if err != nil {
					if !toolErrors {
						add(path, "workflow_contract", "Workflow V2 violates graph, condition, activity or approval contracts")
					}
				} else {
					workflowPlans = append(workflowPlans, executable.Plan())
					tests = append(tests, TestCase{d.ID, "workflow_failure_recovery"})
				}
				for i := range w.States {
					sort.Strings(w.States[i].ApprovalRoles)
				}
			} else {
				validateWorkflow(w, path, symbols, catalog, expression, add)
			}
			sort.Slice(w.States, func(i, j int) bool { return w.States[i].ID < w.States[j].ID })
		}
		if t := d.Tool; t != nil {
			sort.Strings(t.Permissions)
			for _, permission := range t.Permissions {
				if !contains(catalog.Permissions, permission) {
					add(path, "unknown_permission", "Tool requires a permission unavailable in the trusted catalog")
				}
			}
			if t.Retry.InitialBackoffMS > t.Retry.MaxBackoffMS {
				add(path, "invalid_retry", "Retry backoff bounds are inconsistent")
			}
			if t.SideEffect != "read" && t.SideEffect != "none" && (t.SandboxMode == "read_only" || t.Approval == "none") {
				add(path, "unsafe_tool", "Effectful tools require approval and a safe side-effect mode")
			}
			if t.SideEffect == "financial" && (t.SandboxMode != "block" || t.Approval != "elevated") {
				add(path, "financial_tool", "Financial tools require blocked preview effects and elevated approval")
			}
			validateValueSchema(t.InputSchema, path+"/input_schema", add)
			validateValueSchema(t.OutputSchema, path+"/output_schema", add)
			normalizeValueSchema(&t.InputSchema)
			normalizeValueSchema(&t.OutputSchema)
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		return diagnostics[i].Code < diagnostics[j].Code
	})
	if len(diagnostics) > 0 {
		return Program{}, diagnostics
	}
	ir := IR{IRVersion: IRVersion, CompilerVersion: Version, CatalogHash: digest(catalogJSON), SchemaSnapshot: SchemaSnapshot, Application: doc, Entities: entities, Migration: MigrationPlan{"NONE", []string{}}, Tests: tests}
	if doc.SchemaVersion == metadata.RuleSchemaVersion {
		ir.IRVersion = RuleIRVersion
		ir.CompilerVersion = RuleCompilerVersion
		ir.Rules = rulePlans
	}
	if doc.SchemaVersion == metadata.WorkflowSchemaVersion {
		ir.IRVersion = WorkflowIRVersion
		ir.CompilerVersion = WorkflowCompilerVersion
		ir.Rules = rulePlans
		ir.Workflows = workflowPlans
	}
	if doc.SchemaVersion == metadata.ToolWorkflowSchemaVersion {
		ir.IRVersion = ToolWorkflowIRVersion
		ir.CompilerVersion = ToolWorkflowCompilerVersion
		ir.Rules = rulePlans
		ir.Workflows = workflowPlans
	}
	raw, err := json.Marshal(ir)
	if err != nil {
		return Program{}, []Diagnostic{{"", "serialization", "IR serialization failed"}}
	}
	if len(raw) > 4<<20 {
		return Program{}, []Diagnostic{{"", "ir_budget", "Compiled application IR exceeds the 4 MiB limit"}}
	}
	return Program{raw: raw, hash: digest(raw), sourceHash: digest(source.JSON())}, diagnostics
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func normalizeValueSchema(s *metadata.ValueSchema) {
	if s.Required != nil {
		sort.Strings(*s.Required)
	}
	if s.Properties != nil {
		for k, v := range *s.Properties {
			normalizeValueSchema(&v)
			(*s.Properties)[k] = v
		}
	}
	if s.Items != nil {
		normalizeValueSchema(s.Items)
	}
}

func validateValueSchema(s metadata.ValueSchema, path string, add func(string, string, string)) {
	if s.Properties != nil {
		if s.Required != nil {
			for _, key := range *s.Required {
				if _, ok := (*s.Properties)[key]; !ok {
					add(path, "schema_required", "Required payload properties must be declared")
				}
			}
		}
		keys := make([]string, 0, len(*s.Properties))
		for key := range *s.Properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			validateValueSchema((*s.Properties)[key], path+"/properties/"+strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1"), add)
		}
	}
	if s.Items != nil {
		validateValueSchema(*s.Items, path+"/items", add)
	}
}
