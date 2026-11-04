package compiler

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

func addField(doc *metadata.Document, entity, id, typ string, required bool, mutate func(*metadata.Field)) {
	f := &metadata.Field{EntityRef: entity, Type: typ, Required: required}
	if mutate != nil {
		mutate(f)
	}
	doc.Definitions = append(doc.Definitions, metadata.Definition{ID: id, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: id, Kind: "field", Field: f})
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == entity {
			doc.Definitions[i].Entity.Fields = append(doc.Definitions[i].Entity.Fields, id)
		}
	}
}

func dropField(doc *metadata.Document, entity, id string) {
	kept := doc.Definitions[:0]
	for _, d := range doc.Definitions {
		if d.ID == id {
			continue
		}
		if d.ID == entity {
			fields := []string{}
			for _, f := range d.Entity.Fields {
				if f != id {
					fields = append(fields, f)
				}
			}
			d.Entity.Fields = fields
		}
		kept = append(kept, d)
	}
	doc.Definitions = kept
}

func setField(doc *metadata.Document, id string, mutate func(*metadata.Field)) {
	defOf(doc, id).Field = func() *metadata.Field { f := *defOf(doc, id).Field; mutate(&f); return &f }()
}

func newEntity(doc *metadata.Document, entity string) {
	idField := "field." + entity[len("entity."):] + ".id"
	doc.Definitions = append(doc.Definitions,
		metadata.Definition{ID: entity, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: entity, Kind: "entity", Entity: &metadata.Entity{Fields: []string{idField}}},
		metadata.Definition{ID: idField, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: idField, Kind: "field", Field: &metadata.Field{EntityRef: entity, Type: "string", Required: true}})
}

func TestClassifyStorageTable(t *testing.T) {
	backfill := func(entity, field, value string) EvolutionIntent {
		return EvolutionIntent{Backfills: []FieldBackfill{{EntityID: entity, FieldID: field, When: "MISSING_OR_NULL", Value: json.RawMessage(value)}}}
	}
	withPrice := func(currency string) func(*metadata.Document) {
		return func(d *metadata.Document) {
			addField(d, "entity.item", "field.item.price", "money", false, func(f *metadata.Field) { f.Currency = currency })
		}
	}
	withChoice := func(values ...string) func(*metadata.Document) {
		return func(d *metadata.Document) {
			addField(d, "entity.item", "field.item.choice", "enum", false, func(f *metadata.Field) { f.EnumValues = values })
		}
	}
	withRef := func(target string) func(*metadata.Document) {
		return func(d *metadata.Document) {
			newEntity(d, "entity.other")
			newEntity(d, "entity.third")
			addField(d, "entity.item", "field.item.link", "reference", false, func(f *metadata.Field) { f.ReferenceTo = target })
		}
	}
	cases := []struct {
		name     string
		base     func(*metadata.Document)
		target   func(*metadata.Document)
		intent   EvolutionIntent
		class    string
		reasons  []string
		hasPlan  bool
		sameHash bool
	}{
		{name: "identical", class: StorageEqual, sameHash: true},
		{name: "labels only", target: func(d *metadata.Document) { defOf(d, "entity.item").Label = "Renamed label" }, class: StorageEqual, sameHash: true},
		{name: "optional field added", target: func(d *metadata.Document) { addField(d, "entity.item", "field.item.note", "string", false, nil) }, class: StorageAdditive, reasons: []string{"ADD_OPTIONAL"}, hasPlan: true},
		{name: "entity added", target: func(d *metadata.Document) { newEntity(d, "entity.extra") }, class: StorageAdditive, reasons: []string{"ADD_ENTITY"}, hasPlan: true},
		{name: "entity and optional field", target: func(d *metadata.Document) {
			newEntity(d, "entity.extra")
			addField(d, "entity.item", "field.item.note", "string", false, nil)
		}, class: StorageAdditive, reasons: []string{"ADD_ENTITY", "ADD_OPTIONAL"}, hasPlan: true},
		{name: "required field without backfill", target: func(d *metadata.Document) { addField(d, "entity.item", "field.item.code", "string", true, nil) }, class: StorageBreaking, reasons: []string{"MISSING_BACKFILL"}},
		{name: "required field with backfill", target: func(d *metadata.Document) { addField(d, "entity.item", "field.item.code", "string", true, nil) },
			intent: backfill("entity.item", "field.item.code", `"none"`), class: StorageTransform, reasons: []string{"BACKFILL"}, hasPlan: true},
		{name: "optional becomes required without backfill", base: func(d *metadata.Document) {
			setField(d, "field.item.count", func(f *metadata.Field) { f.Required = false })
		},
			target: func(d *metadata.Document) {
				setField(d, "field.item.count", func(f *metadata.Field) { f.Required = true })
			}, class: StorageBreaking, reasons: []string{"MISSING_BACKFILL"}},
		{name: "required relaxed", target: func(d *metadata.Document) {
			setField(d, "field.item.name", func(f *metadata.Field) { f.Required = false })
		}, class: StorageTransform, reasons: []string{"RELAX_REQUIRED"}, hasPlan: true},
		{name: "declared rename", target: func(d *metadata.Document) {
			dropField(d, "entity.item", "field.item.name")
			addField(d, "entity.item", "field.item.title", "string", true, nil)
		}, intent: EvolutionIntent{Renames: []FieldRename{{EntityID: "entity.item", From: "field.item.name", To: "field.item.title"}}}, class: StorageTransform, reasons: []string{"RENAME"}, hasPlan: true},
		{name: "undeclared rename", target: func(d *metadata.Document) {
			dropField(d, "entity.item", "field.item.name")
			addField(d, "entity.item", "field.item.title", "string", true, nil)
		}, class: StorageBreaking, reasons: []string{"MISSING_BACKFILL", "REMOVED_FIELD", "UNDECLARED_RENAME"}},
		{name: "integer to decimal", target: func(d *metadata.Document) {
			setField(d, "field.item.count", func(f *metadata.Field) { f.Type = "decimal" })
		}, class: StorageTransform, reasons: []string{"CONVERT"}, hasPlan: true},
		{name: "unsupported type change", target: func(d *metadata.Document) {
			setField(d, "field.item.count", func(f *metadata.Field) { f.Type = "string" })
		}, class: StorageBreaking, reasons: []string{"TYPE_CHANGE"}},
		{name: "field removed", target: func(d *metadata.Document) { dropField(d, "entity.item", "field.item.count") }, class: StorageBreaking, reasons: []string{"REMOVED_FIELD"}},
		{name: "entity removed", base: func(d *metadata.Document) { newEntity(d, "entity.extra") }, class: StorageBreaking, reasons: []string{"REMOVED_ENTITY"}},
		{name: "currency changed", base: withPrice("USD"), target: withPrice("EUR"), class: StorageBreaking, reasons: []string{"CURRENCY_CHANGE"}},
		{name: "enum options changed", base: withChoice("a", "b"), target: withChoice("a", "b", "c"), class: StorageBreaking, reasons: []string{"OPTIONS_CHANGE"}},
		{name: "reference target changed", base: withRef("entity.other"), target: withRef("entity.third"), class: StorageBreaking, reasons: []string{"REFERENCE_CHANGE"}},
		{name: "intent for nothing", target: func(d *metadata.Document) { addField(d, "entity.item", "field.item.note", "string", false, nil) },
			intent: EvolutionIntent{Renames: []FieldRename{{EntityID: "entity.item", From: "field.item.a", To: "field.item.b"}}}, class: StorageBreaking, reasons: []string{"INVALID_INTENT"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base, target := storageProgram(t, c.base, "catalog-one"), storageProgram(t, c.target, "catalog-two")
			got := ClassifyStorage(base, target, c.intent)
			if got.Class != c.class || !reflect.DeepEqual(got.Reasons, append([]string{}, c.reasons...)) && !(len(got.Reasons) == 0 && len(c.reasons) == 0) {
				t.Fatalf("%+v", got)
			}
			if got.FirstRelease || got.BaseSchemaHash == "" || got.TargetSchemaHash == "" {
				t.Fatalf("identity: %+v", got)
			}
			if (got.EvolutionPlanHash != "") != c.hasPlan {
				t.Fatalf("plan hash %q", got.EvolutionPlanHash)
			}
			// Properties against PlanEvolution and the storage hashes.
			plan, err := PlanEvolution(base, target, c.intent)
			_, bh, _ := Storage(base)
			_, th, _ := Storage(target)
			switch {
			case (got.Class == StorageBreaking) != (err != nil && bh != th):
				t.Fatalf("BREAKING iff PlanEvolution refuses: class %s err %v", got.Class, err)
			case (got.Class == StorageEqual) != (bh == th):
				t.Fatal("EQUAL iff schema hashes are equal")
			case got.Class == StorageAdditive:
				for _, op := range plan.Artifact().Operations {
					if op.Kind != "ADD_ENTITY" && op.Kind != "ADD_OPTIONAL" {
						t.Fatalf("ADDITIVE with %s", op.Kind)
					}
				}
				if got.EvolutionPlanHash != plan.Hash() {
					t.Fatal("plan hash differs from PlanEvolution")
				}
			case got.Class == StorageTransform:
				if got.EvolutionPlanHash != plan.Hash() {
					t.Fatal("plan hash differs from PlanEvolution")
				}
				only := true
				for _, op := range plan.Artifact().Operations {
					only = only && (op.Kind == "ADD_ENTITY" || op.Kind == "ADD_OPTIONAL")
				}
				if only {
					t.Fatal("TRANSFORM with only additive operations")
				}
			}
		})
	}
}

func TestClassifyStorageFirstReleaseAndInvalidPrograms(t *testing.T) {
	target := storageProgram(t, nil, "catalog-one")
	got := ClassifyStorage(Program{}, target, EvolutionIntent{})
	if got.Class != StorageAdditive || !got.FirstRelease || got.BaseSchemaHash != "" || got.TargetSchemaHash == "" || !reflect.DeepEqual(got.Reasons, []string{"ADD_ENTITY"}) {
		t.Fatalf("first release %+v", got)
	}
	got = ClassifyStorage(Program{}, target, EvolutionIntent{Renames: []FieldRename{{EntityID: "entity.item", From: "a", To: "b"}}})
	if got.Class != StorageBreaking || !reflect.DeepEqual(got.Reasons, []string{"UNUSED_INTENT"}) {
		t.Fatalf("first release with intent %+v", got)
	}
	got = ClassifyStorage(target, Program{}, EvolutionIntent{})
	if got.Class != StorageBreaking || !reflect.DeepEqual(got.Reasons, []string{"INVALID_PROGRAM"}) {
		t.Fatalf("invalid target %+v", got)
	}
	// Different applications never evolve into each other.
	other := workflowProgram(t, nil)
	got = ClassifyStorage(target, other, EvolutionIntent{})
	if got.Class != StorageBreaking || !reflect.DeepEqual(got.Reasons, []string{"APP_MISMATCH"}) {
		t.Fatalf("app mismatch %+v", got)
	}
	// Identical schemas with an intent are EQUAL but flagged so callers can reject.
	got = ClassifyStorage(target, storageProgram(t, nil, "catalog-two"), EvolutionIntent{Backfills: []FieldBackfill{{EntityID: "entity.item", FieldID: "field.item.name", When: "MISSING", Value: json.RawMessage(`"x"`)}}})
	if got.Class != StorageEqual || !reflect.DeepEqual(got.Reasons, []string{"UNUSED_INTENT"}) {
		t.Fatalf("equal with intent %+v", got)
	}
}

func TestClassifyStorageIsDeterministic(t *testing.T) {
	base := storageProgram(t, nil, "catalog-one")
	target := storageProgram(t, func(d *metadata.Document) {
		addField(d, "entity.item", "field.item.note", "string", false, nil)
		newEntity(d, "entity.extra")
	}, "catalog-one")
	a, b := ClassifyStorage(base, target, EvolutionIntent{}), ClassifyStorage(base, target, EvolutionIntent{})
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	if string(x) != string(y) || a.Class != StorageAdditive {
		t.Fatalf("%s %s", x, y)
	}
}
