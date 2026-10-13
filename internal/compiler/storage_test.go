package compiler

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

func storageProgram(t *testing.T, mutate func(*metadata.Document), catalog string) Program {
	t.Helper()
	d := metadata.Document{SchemaVersion: metadata.SchemaVersion, AppID: "app.test", Namespace: "example.test", Definitions: []metadata.Definition{
		{ID: "entity.item", Kind: "entity", Label: "Item", Entity: &metadata.Entity{Fields: []string{"field.item.id", "field.item.name", "field.item.count"}}},
		{ID: "field.item.id", Kind: "field", Label: "ID", Field: &metadata.Field{EntityRef: "entity.item", Type: "string", Required: true}},
		{ID: "field.item.name", Kind: "field", Label: "Name", Field: &metadata.Field{EntityRef: "entity.item", Type: "string", Required: true}},
		{ID: "field.item.count", Kind: "field", Label: "Count", Field: &metadata.Field{EntityRef: "entity.item", Type: "integer"}},
	}}
	for i := range d.Definitions {
		d.Definitions[i].SchemaVersion, d.Definitions[i].Namespace = metadata.SchemaVersion, d.Namespace
	}
	if mutate != nil {
		mutate(&d)
	}
	raw, _ := json.Marshal(d)
	p, ds := Compile(source(t, raw), Catalog{Version: catalog, Permissions: []string{}, Tools: []string{}})
	if !p.Valid() {
		t.Fatal(ds)
	}
	return p
}

func TestStorageIdentityExcludesExecutionAndPresentationIdentity(t *testing.T) {
	a := storageProgram(t, nil, "catalog-one")
	b := storageProgram(t, func(d *metadata.Document) {
		for i := range d.Definitions {
			d.Definitions[i].Label += " renamed label"
		}
		for i, j := 0, len(d.Definitions)-1; i < j; i, j = i+1, j-1 {
			d.Definitions[i], d.Definitions[j] = d.Definitions[j], d.Definitions[i]
		}
	}, "catalog-two")
	_, ah, err := Storage(a)
	_, bh, other := Storage(b)
	if err != nil || other != nil || ah != bh || a.Hash() == b.Hash() {
		t.Fatal("data schema is coupled to release identity")
	}
	before := a.JSON()
	plan, err := PlanEvolution(a, b, EvolutionIntent{})
	if err != nil || plan.Artifact().Class != "COMPATIBLE" || len(plan.Artifact().Operations) != 0 {
		t.Fatal("label/catalog change should preserve physical storage", err)
	}
	restored, err := Restore(a.JSON(), a.Hash(), SupportedIRVersions())
	if err != nil || !bytes.Equal(restored.JSON(), before) {
		t.Fatal("stored IR was changed")
	}
	if _, err = RestoreEvolution(restored, b, plan.JSON(), plan.Hash()); err != nil {
		t.Fatal(err)
	}
	copy := plan.Artifact()
	copy.Class = "forged"
	raw := plan.JSON()
	raw[0] = '!'
	if plan.Artifact().Class != "COMPATIBLE" || !json.Valid(plan.JSON()) {
		t.Fatal("caller mutated immutable evolution artifact")
	}
}

func TestEvolutionRequiresExplicitSafeInstructions(t *testing.T) {
	base := storageProgram(t, nil, "one")
	rename := storageProgram(t, func(d *metadata.Document) {
		d.Definitions[0].Entity.Fields[1] = "field.item.title"
		d.Definitions[2].ID = "field.item.title"
		d.Definitions[3].Field.Type = "decimal"
	}, "two")
	if _, err := PlanEvolution(base, rename, EvolutionIntent{}); err == nil {
		t.Fatal("undeclared field removal/rename accepted")
	}
	intent := EvolutionIntent{Renames: []FieldRename{{"entity.item", "field.item.name", "field.item.title"}}}
	p, err := PlanEvolution(base, rename, intent)
	if err != nil || p.Artifact().Class != "TRANSFORM" || len(p.Artifact().Operations) != 2 {
		t.Fatal("rename/conversion did not produce executable operations", err)
	}
	if _, err = RestoreEvolution(base, rename, p.JSON(), p.Hash()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []EvolutionIntent{
		{Renames: []FieldRename{{"entity.item", "field.item.name", "field.item.title"}, {"entity.item", "field.item.name", "field.item.title"}}},
		{Renames: []FieldRename{{"entity.item", "field.item.id", "field.item.title"}}},
		{Renames: []FieldRename{{"entity.item", "field.item.count", "field.item.title"}}},
		{Renames: intent.Renames, Backfills: []FieldBackfill{{"entity.item", "field.item.absent", "MISSING", json.RawMessage(`1`)}}},
		{Renames: intent.Renames, Backfills: []FieldBackfill{{"entity.item", "field.item.count", "MISSING", json.RawMessage(`null`)}}},
	} {
		if _, err = PlanEvolution(base, rename, bad); err == nil {
			t.Fatal("ambiguous or unused instructions accepted")
		}
	}
	artifact := p.Artifact()
	artifact.Class = "COMPATIBLE"
	raw, _ := json.Marshal(artifact)
	if _, err = RestoreEvolution(base, rename, raw, digest(raw)); err == nil {
		t.Fatal("hash-consistent forged operations/class accepted")
	}
	if _, err = RestoreEvolution(rename, base, p.JSON(), p.Hash()); err == nil {
		t.Fatal("reversed base accepted")
	}
	if _, err = RestoreEvolution(base, rename, append(p.JSON(), '\n'), digest(append(p.JSON(), '\n'))); err == nil {
		t.Fatal("noncanonical bytes accepted")
	}
}

func TestEvolutionBackfillAndIncompatibleContracts(t *testing.T) {
	base := storageProgram(t, nil, "one")
	required := storageProgram(t, func(d *metadata.Document) { d.Definitions[3].Field.Required = true }, "two")
	for _, when := range []string{"", "MISSING", "ALWAYS"} {
		if _, err := PlanEvolution(base, required, EvolutionIntent{Backfills: []FieldBackfill{{"entity.item", "field.item.count", when, json.RawMessage(`0`)}}}); err == nil {
			t.Fatal("required backfill can leave null/missing unresolved")
		}
	}
	p, err := PlanEvolution(base, required, EvolutionIntent{Backfills: []FieldBackfill{{"entity.item", "field.item.count", "MISSING_OR_NULL", json.RawMessage(`0`)}}})
	if err != nil || p.Artifact().Class != "TRANSFORM" || p.Artifact().Operations[0].Kind != "BACKFILL" {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*metadata.Document){
		"lossy type": func(d *metadata.Document) { d.Definitions[3].Field.Type = "bool" },
		"drop": func(d *metadata.Document) {
			d.Definitions[0].Entity.Fields = d.Definitions[0].Entity.Fields[:2]
			d.Definitions = d.Definitions[:3]
		},
		"app": func(d *metadata.Document) { d.AppID = "app.other" },
	} {
		t.Run(name, func(t *testing.T) {
			target := storageProgram(t, mutate, "two")
			if _, err := PlanEvolution(base, target, EvolutionIntent{}); err == nil {
				t.Fatal("unsupported evolution accepted")
			}
		})
	}
	if _, err := PlanEvolution(Program{}, base, EvolutionIntent{}); err == nil {
		t.Fatal("untrusted empty base accepted")
	}
}
