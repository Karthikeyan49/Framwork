package data

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

func evolutionProgram(t *testing.T, numberType, name string, required bool) compiler.Program {
	t.Helper()
	d := metadata.Document{SchemaVersion: metadata.SchemaVersion, AppID: "app.test", Namespace: "example.test", Definitions: []metadata.Definition{
		{ID: "entity.item", Kind: "entity", Label: "Item", Entity: &metadata.Entity{Fields: []string{"field.item.id", name, "field.item.count"}}},
		{ID: "field.item.id", Kind: "field", Label: "ID", Field: &metadata.Field{EntityRef: "entity.item", Type: "string", Required: true}},
		{ID: name, Kind: "field", Label: "Name", Field: &metadata.Field{EntityRef: "entity.item", Type: "string", Required: true}},
		{ID: "field.item.count", Kind: "field", Label: "Count", Field: &metadata.Field{EntityRef: "entity.item", Type: numberType, Required: required}},
	}}
	for i := range d.Definitions {
		d.Definitions[i].SchemaVersion, d.Definitions[i].Namespace = metadata.SchemaVersion, d.Namespace
	}
	raw, _ := json.Marshal(d)
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	s, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := compiler.Compile(s, compiler.PlatformCatalog())
	if !p.Valid() {
		t.Fatal(ds)
	}
	return p
}

func TestEvolutionTransformsExactValuesWithoutMutatingSource(t *testing.T) {
	base := evolutionProgram(t, "integer", "field.item.name", false)
	target := evolutionProgram(t, "decimal", "field.item.title", true)
	intent := compiler.EvolutionIntent{Renames: []compiler.FieldRename{{EntityID: "entity.item", From: "field.item.name", To: "field.item.title"}}, Backfills: []compiler.FieldBackfill{{EntityID: "entity.item", FieldID: "field.item.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`"0"`)}}}
	p, err := compiler.PlanEvolution(base, target, intent)
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000001"
	for _, count := range []any{json.Number("9007199254740991"), nil} {
		input := map[string]any{"field.item.name": "Preserved", "field.item.count": count}
		before, _ := json.Marshal(input)
		out, err := TransformRecord(p, base, target, "entity.item", id, input)
		if err != nil || out["field.item.title"] != "Preserved" || out["field.item.name"] != nil || out["field.item.id"] != nil {
			t.Fatal(out, err)
		}
		want := "9007199254740991"
		if count == nil {
			want = "0"
		}
		if out["field.item.count"] != want {
			t.Fatal("lost numeric precision", out)
		}
		after, _ := json.Marshal(input)
		if string(before) != string(after) {
			t.Fatal("source was mutated")
		}
	}
	missing, err := TransformRecord(p, base, target, "entity.item", id, map[string]any{"field.item.name": "Missing"})
	if err != nil || missing["field.item.count"] != "0" {
		t.Fatal(missing, err)
	}
	if _, err = TransformRecord(p, target, base, "entity.item", id, map[string]any{}); err == nil {
		t.Fatal("wrong plan binding accepted")
	}
}

func TestEvolutionRejectsLossAndCorruptionAndValidatesEmptyBackfills(t *testing.T) {
	base := evolutionProgram(t, "decimal", "field.item.name", false)
	target := evolutionProgram(t, "integer", "field.item.name", false)
	p, err := compiler.PlanEvolution(base, target, compiler.EvolutionIntent{})
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000001"
	for _, amount := range []string{"1.5", "9007199254740992", "1e4", "0.0000000000000000001"} {
		if _, err = TransformRecord(p, base, target, "entity.item", id, map[string]any{"field.item.name": "x", "field.item.count": amount}); err == nil {
			t.Fatal("lossy number accepted", amount)
		}
	}
	out, err := TransformRecord(p, base, target, "entity.item", id, map[string]any{"field.item.name": "x", "field.item.count": "12.000"})
	if err != nil || !reflect.DeepEqual(out["field.item.count"], json.Number("12")) {
		t.Fatal(out, err)
	}
	for _, input := range []map[string]any{
		{"field.item.name": "x", "unknown": true},
		{"field.item.name": "x", "field.item.id": id},
		{"field.item.count": "1"},
	} {
		if _, err = TransformRecord(p, base, target, "entity.item", id, input); err == nil {
			t.Fatal("corrupt input accepted")
		}
	}
	required := evolutionProgram(t, "integer", "field.item.name", true)
	bad, err := compiler.PlanEvolution(base, required, compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.item", FieldID: "field.item.count", When: "MISSING_OR_NULL", Value: json.RawMessage(`"wrong type"`)}}})
	if err != nil {
		t.Fatal(err)
	}
	if ValidateBackfills(bad, required) == nil {
		t.Fatal("empty dataset accepted an invalid required default")
	}
	if _, err = TransformRecord(bad, base, required, "entity.item", id, map[string]any{"field.item.name": "x", "field.item.count": "1"}); err == nil {
		t.Fatal("unused invalid default accepted")
	}
}

func TestEvolutionMissingAndNullHaveDifferentSemantics(t *testing.T) {
	base := evolutionProgram(t, "integer", "field.item.name", false)
	p, err := compiler.PlanEvolution(base, base, compiler.EvolutionIntent{Backfills: []compiler.FieldBackfill{{EntityID: "entity.item", FieldID: "field.item.count", When: "MISSING", Value: json.RawMessage(`7`)}}})
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000-0000-4000-8000-000000000001"
	out, err := TransformRecord(p, base, base, "entity.item", id, map[string]any{"field.item.name": "x", "field.item.count": nil})
	if err != nil || out["field.item.count"] != nil {
		t.Fatal("explicit null overwritten", out, err)
	}
	out, err = TransformRecord(p, base, base, "entity.item", id, map[string]any{"field.item.name": "x"})
	if err != nil || out["field.item.count"] != json.Number("7") {
		t.Fatal("missing value not backfilled", out, err)
	}
}
