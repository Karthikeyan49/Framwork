package compiler

import (
	"encoding/json"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"os"
	"strings"
	"testing"
)

func source(t *testing.T, raw []byte) metadata.Source {
	t.Helper()
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	s, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func fixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/vendor-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func vendorCatalog() Catalog {
	return Catalog{Version: "test-v1", Permissions: []string{"vendor.notify"}, Tools: []string{}}
}
func TestCompilerGoldenAndOrderDeterminism(t *testing.T) {
	raw := fixture(t)
	program, ds := Compile(source(t, raw), vendorCatalog())
	if !program.Valid() || len(ds) != 0 {
		t.Fatal(ds)
	}
	var doc metadata.Document
	_ = json.Unmarshal(raw, &doc)
	for i, j := 0, len(doc.Definitions)-1; i < j; i, j = i+1, j-1 {
		doc.Definitions[i], doc.Definitions[j] = doc.Definitions[j], doc.Definitions[i]
	}
	reordered, _ := json.Marshal(doc)
	other, ds := Compile(source(t, reordered), vendorCatalog())
	if len(ds) > 0 || other.Hash() != program.Hash() {
		t.Fatal("order changed canonical hash", ds)
	}
	golden, err := os.ReadFile("testdata/vendor.ir.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(golden)) != string(program.JSON()) {
		t.Fatal("canonical IR differs from reviewed golden")
	}
	before := program.Hash()
	clone := program.IR()
	clone.Entities[0].Fields[0].ID = "mutated"
	b := program.JSON()
	b[0] = '!'
	if program.Hash() != before || !json.Valid(program.JSON()) {
		t.Fatal("caller mutated trusted IR")
	}
	changed, ds := Compile(source(t, raw), Catalog{Version: "different-v1", Permissions: []string{"vendor.notify"}, Tools: []string{}})
	if len(ds) > 0 || changed.Hash() == program.Hash() {
		t.Fatal("catalog identity absent from hash")
	}
	if program.IR().Migration.Class != "NONE" {
		t.Fatal("compiler planned unapproved physical DDL")
	}
}
func TestCompilerNegativeCorpus(t *testing.T) {
	raw := string(fixture(t))
	for name, mutated := range map[string]string{
		"unknown symbol":           strings.Replace(raw, "field.vendor.name", "field.unknown.name", 1),
		"core type":                strings.Replace(raw, `"type":"string"`, `"type":"integer"`, 1),
		"expression type":          strings.Replace(raw, `"value":""`, `"value":true`, 1),
		"input type":               strings.Replace(raw, `"type":"text_input"`, `"type":"number_input"`, 1),
		"workflow target":          strings.Replace(raw, `"to":"state.complete"`, `"to":"state.unknown"`, 1),
		"workflow cycle":           strings.Replace(raw, `"to":"state.complete"`, `"to":"state.review"`, 1),
		"unsafe tool":              strings.Replace(raw, `"approval":"user"`, `"approval":"none"`, 1),
		"relationship cardinality": strings.Replace(raw, "many_to_one", "many_to_many", 1),
	} {
		t.Run(name, func(t *testing.T) {
			p, ds := Compile(source(t, []byte(mutated)), vendorCatalog())
			if p.Valid() || len(ds) == 0 {
				t.Fatal("invalid source compiled")
			}
			for _, d := range ds {
				if d.Code == "" || d.Message == "" {
					t.Fatal("missing diagnostic")
				}
			}
		})
	}
	if p, ds := Compile(source(t, []byte(raw)), PlatformCatalog()); p.Valid() || len(ds) == 0 {
		t.Fatal("unknown permission accepted")
	}
	if p, _ := Compile(metadata.Source{}, PlatformCatalog()); p.Valid() {
		t.Fatal("forged source accepted")
	}
}
func FuzzCompileNeverPanics(f *testing.F) {
	raw, _ := os.ReadFile("../../contracts/examples/vendor-draft-v1.json")
	f.Add(raw)
	f.Add([]byte(`{}`))
	v, _ := metadata.NewValidator()
	f.Fuzz(func(t *testing.T, raw []byte) {
		s, err := v.Parse(raw)
		if err != nil {
			return
		}
		p, ds := Compile(s, PlatformCatalog())
		if p.Valid() && len(ds) > 0 {
			t.Fatal("diagnostics accompanied executable IR")
		}
	})
}

func TestExpressionOwnershipIsExact(t *testing.T) {
	fields := map[string]Field{"field.vendor.child.flag": {Type: "bool"}}
	owners := map[string]string{"field.vendor.child.flag": "entity.vendor.child"}
	diagnostics := []Diagnostic{}
	check := expressionChecker(fields, owners, func(p, c, m string) { diagnostics = append(diagnostics, Diagnostic{p, c, m}) })
	if check(metadata.Expression{Op: "field", FieldRef: "field.vendor.child.flag"}, "entity.vendor", "/test") != "invalid" || len(diagnostics) == 0 {
		t.Fatal("prefix-overlapping entity leaked expression scope")
	}
}
func TestDefaultCatalogCompilesContacts(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	p, ds := Compile(source(t, raw), PlatformCatalog())
	if !p.Valid() || len(ds) > 0 {
		t.Fatal(ds)
	}
}

func TestCanonicalSetsAndTotalWorkflowBranches(t *testing.T) {
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	var doc metadata.Document
	_ = json.Unmarshal(raw, &doc)
	for i := range doc.Definitions {
		if doc.Definitions[i].Workflow != nil {
			doc.Definitions[i].Workflow.States[0].ApprovalRoles = []string{"owner", "admin"}
		}
	}
	first, _ := json.Marshal(doc)
	a, ds := Compile(source(t, first), PlatformCatalog())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for i := range doc.Definitions {
		if f := doc.Definitions[i].Field; f != nil && len(f.EnumValues) > 1 {
			f.EnumValues[0], f.EnumValues[1] = f.EnumValues[1], f.EnumValues[0]
		}
		if w := doc.Definitions[i].Workflow; w != nil {
			w.States[0].ApprovalRoles = []string{"admin", "owner"}
		}
	}
	second, _ := json.Marshal(doc)
	b, ds := Compile(source(t, second), PlatformCatalog())
	if len(ds) > 0 || a.Hash() != b.Hash() {
		t.Fatal("set order changed IR hash", ds)
	}
	for i := range doc.Definitions {
		if w := doc.Definitions[i].Workflow; w != nil {
			w.States[0].Transitions[0].Condition = &metadata.Expression{Op: "literal", Value: json.RawMessage(`false`)}
		}
	}
	invalid, _ := json.Marshal(doc)
	p, ds := Compile(source(t, invalid), PlatformCatalog())
	if p.Valid() || len(ds) == 0 {
		t.Fatal("non-total workflow branch compiled")
	}
}
func TestCappedDiagnosticsAreDeterministic(t *testing.T) {
	var doc metadata.Document
	_ = json.Unmarshal(fixture(t), &doc)
	props := map[string]metadata.ValueSchema{}
	required := []string{"missing_a", "missing_b", "missing_c", "missing_d"}
	closed := false
	for i := 0; i < 50; i++ {
		child := map[string]metadata.ValueSchema{}
		if i%2 == 0 {
			child["nested"] = metadata.ValueSchema{Type: "object", Properties: &map[string]metadata.ValueSchema{}, Required: &required, AdditionalProperties: &closed}
		}
		props[fmt.Sprintf("p%02d", i)] = metadata.ValueSchema{Type: "object", Properties: &child, Required: &required, AdditionalProperties: &closed}
	}
	for i := range doc.Definitions {
		if doc.Definitions[i].Tool != nil {
			doc.Definitions[i].Tool.InputSchema.Properties = &props
		}
	}
	raw, _ := json.Marshal(doc)
	validated := source(t, raw)
	_, first := Compile(validated, vendorCatalog())
	if len(first) != 100 {
		t.Fatal("fixture did not exercise diagnostic cap")
	}
	expected, _ := json.Marshal(first)
	for i := 0; i < 20; i++ {
		_, ds := Compile(validated, vendorCatalog())
		actual, _ := json.Marshal(ds)
		if string(actual) != string(expected) {
			t.Fatal("capped diagnostic set is nondeterministic")
		}
	}
}

func TestRendererCompatibilityRejectsNestedFormAndInvalidBadge(t *testing.T) {
	raw, _ := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	for _, component := range []metadata.Component{
		{Type: "form", Children: []metadata.Component{{Type: "section", Children: []metadata.Component{{Type: "form"}}}}},
		{Type: "status_badge"}, {Type: "status_badge", FieldRef: "field.contact.balance"},
	} {
		var doc metadata.Document
		_ = json.Unmarshal(raw, &doc)
		for i := range doc.Definitions {
			if doc.Definitions[i].Page != nil {
				doc.Definitions[i].Page.Components = []metadata.Component{component}
			}
		}
		changed, _ := json.Marshal(doc)
		p, ds := Compile(source(t, changed), PlatformCatalog())
		if p.Valid() || len(ds) == 0 {
			t.Fatal("renderer-incompatible tree compiled", component.Type)
		}
	}
}
