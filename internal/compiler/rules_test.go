package compiler

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"os"
	"strings"
	"testing"
)

func ruleFixture(t *testing.T) []byte {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/examples/contacts-rules-v2.json")
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

func TestM7OversizedApplicationIRHasBudgetDiagnostic(t *testing.T) {
	doc := metadata.Document{SchemaVersion: metadata.RuleSchemaVersion, AppID: "app.budget", Namespace: "test.rules", Definitions: []metadata.Definition{}}
	entity := metadata.Definition{ID: "entity.test", SchemaVersion: metadata.RuleSchemaVersion, Namespace: doc.Namespace, Label: "Budget fixture", Kind: "entity", Entity: &metadata.Entity{Fields: []string{}}}
	choices := []string{}
	for i := 0; i < 100; i++ {
		choices = append(choices, fmt.Sprintf("choice_%03d_%s", i, strings.Repeat("x", 24)))
	}
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("field.test.value_%d", i)
		entity.Entity.Fields = append(entity.Entity.Fields, id)
		doc.Definitions = append(doc.Definitions, metadata.Definition{ID: id, SchemaVersion: metadata.RuleSchemaVersion, Namespace: doc.Namespace, Label: "Enum", Kind: "field", Field: &metadata.Field{EntityRef: entity.ID, Type: "enum", EnumValues: choices}})
	}
	doc.Definitions = append(doc.Definitions, entity)
	for i := 0; i < 56; i++ {
		doc.Definitions = append(doc.Definitions, metadata.Definition{ID: fmt.Sprintf("rule.test.constant_%d", i), SchemaVersion: metadata.RuleSchemaVersion, Namespace: doc.Namespace, Label: "Constant", Kind: "rule", Rule: &metadata.Rule{EntityRef: entity.ID, DSLVersion: "2.0.0", ResultType: "bool", Expression: metadata.Expression{Op: "typed_literal", ValueType: "bool", Value: json.RawMessage(`true`)}}})
	}
	raw, _ := json.Marshal(doc)
	p, ds := Compile(source(t, raw), PlatformCatalog())
	if p.Valid() || len(ds) != 1 || ds[0].Code != "ir_budget" {
		t.Fatal("IR budget not explicit", ds)
	}
}
func TestM7OptInPlansStoredExecutionAndLegacyHashes(t *testing.T) {
	raw := ruleFixture(t)
	s := source(t, raw)
	p, ds := Compile(s, PlatformCatalog())
	if !p.Valid() || len(ds) > 0 || p.IR().IRVersion != RuleIRVersion || p.IR().CompilerVersion != RuleCompilerVersion || len(p.IR().Rules) == 0 {
		t.Fatal(ds)
	}
	if !MatchesStoredSource(p, s) {
		t.Fatal("source binding")
	}
	r, e := p.Rule("rule.contact.approval_required")
	if e != nil {
		t.Fatal(e)
	}
	v, e := r.Evaluate(context.Background(), map[string]json.RawMessage{"field.contact.balance": json.RawMessage(`{"amount":"500.000000000000000001","currency":"USD"}`)})
	if e != nil {
		t.Fatal(e)
	}
	if b, _ := v.Bool(); !b {
		t.Fatal("threshold precision")
	}
	restored, e := Restore(p.JSON(), p.Hash(), SupportedIRVersions())
	if e != nil {
		t.Fatal(e)
	}
	if restored.IR().CompilerVersion != RuleCompilerVersion {
		t.Fatal("rewrote provenance")
	}
	r, e = restored.Rule("rule.contact.approval_required")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Evaluate(context.Background(), map[string]json.RawMessage{"field.contact.balance": json.RawMessage(`{"amount":"501","currency":"USD"}`)}); e != nil {
		t.Fatal(e)
	}
	if _, e = Restore(p.JSON(), p.Hash(), []string{IRVersion}); e == nil {
		t.Fatal("unsupported IR accepted")
	}
	var doc metadata.Document
	_ = json.Unmarshal(raw, &doc)
	for i, j := 0, len(doc.Definitions)-1; i < j; i, j = i+1, j-1 {
		doc.Definitions[i], doc.Definitions[j] = doc.Definitions[j], doc.Definitions[i]
	}
	for _, d := range doc.Definitions {
		if d.Rule != nil {
			for _, c := range d.Rule.Calendars {
				for i, j := 0, len(c.WorkingDays)-1; i < j; i, j = i+1, j-1 {
					c.WorkingDays[i], c.WorkingDays[j] = c.WorkingDays[j], c.WorkingDays[i]
				}
			}
		}
	}
	reordered, _ := json.Marshal(doc)
	other, ds := Compile(source(t, reordered), PlatformCatalog())
	if len(ds) > 0 || other.Hash() != p.Hash() {
		t.Fatal("rule/calendar order changed hash", ds)
	}
	old, ds := Compile(source(t, fixture(t)), vendorCatalog())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	golden, e := os.ReadFile("testdata/vendor.ir.json")
	if e != nil || strings.TrimSpace(string(golden)) != string(old.JSON()) {
		t.Fatal("legacy IR changed")
	}
	if _, e = Restore(old.JSON(), old.Hash(), SupportedIRVersions()); e != nil {
		t.Fatal(e)
	}
	if _, e = old.Rule("rule.vendor.named"); e == nil {
		t.Fatal("legacy rules silently executed as DSL2")
	}
}
func TestM7StoredPlanSourceAndTypeTamperingDenied(t *testing.T) {
	p, ds := Compile(source(t, ruleFixture(t)), PlatformCatalog())
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	for _, alter := range []func(*IR){func(x *IR) { x.Rules[0].Root.Type.Kind = "money" }, func(x *IR) { x.Rules[0].Root.Op = "sql" }, func(x *IR) { x.Rules[0].Fields[0].Required = !x.Rules[0].Fields[0].Required }, func(x *IR) { x.Rules[0].TZDataHash = strings.Repeat("0", 64) }, func(x *IR) { x.Rules = append(x.Rules, x.Rules[0]) }, func(x *IR) { x.IRVersion = IRVersion }} {
		ir := p.IR()
		alter(&ir)
		raw, _ := json.Marshal(ir)
		if _, e := Restore(raw, digest(raw), SupportedIRVersions()); e == nil {
			t.Fatal("tampered plan accepted")
		}
	}
	ir := p.IR()
	ir.CompilerVersion = "historical-rule-compiler"
	raw, _ := json.Marshal(ir)
	old, e := Restore(raw, digest(raw), SupportedIRVersions())
	if e != nil || old.IR().CompilerVersion != "historical-rule-compiler" {
		t.Fatal("historical provenance lost", e)
	}
}
func TestM7InvalidTypeCurrencyCalendarAndOwnershipDiagnosed(t *testing.T) {
	for name, change := range map[string]func(*metadata.Document){
		"cross currency": func(d *metadata.Document) {
			for _, x := range d.Definitions {
				if x.ID == "rule.contact.approval_required" {
					x.Rule.Expression.Args[1].Value = json.RawMessage(`{"amount":"500","currency":"INR"}`)
				}
			}
		},
		"cross entity": func(d *metadata.Document) {
			for _, x := range d.Definitions {
				if x.ID == "rule.contact.approval_required" {
					x.Rule.Expression.Args[0].FieldRef = "field.unknown.amount"
				}
			}
		},
		"unknown function": func(d *metadata.Document) {
			for _, x := range d.Definitions {
				if x.ID == "rule.contact.approval_required" {
					x.Rule.Expression.Op = "contains"
				}
			}
		},
		"bad zone": func(d *metadata.Document) {
			for _, x := range d.Definitions {
				if x.ID == "rule.contact.next_business_date" {
					x.Rule.Calendars[0].Zone = "Local"
				}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			var d metadata.Document
			_ = json.Unmarshal(ruleFixture(t), &d)
			change(&d)
			raw, _ := json.Marshal(d)
			p, ds := Compile(source(t, raw), PlatformCatalog())
			if p.Valid() || len(ds) == 0 {
				t.Fatal("bad rule compiled")
			}
		})
	}
}
