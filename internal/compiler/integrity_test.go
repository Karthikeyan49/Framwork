package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

func integrityDocument(t *testing.T) metadata.Document {
	t.Helper()
	d := approvalDocument(t)
	d.SchemaVersion = metadata.IntegritySchemaVersion
	for i := range d.Definitions {
		d.Definitions[i].SchemaVersion = d.SchemaVersion
		if f := d.Definitions[i].Field; f != nil && !slices.Contains([]string{"field.contact.id"}, d.Definitions[i].ID) {
			f.Unique = true
		}
	}
	return d
}
func compileIntegrity(t *testing.T, d metadata.Document) Program {
	t.Helper()
	raw, _ := json.Marshal(d)
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	s, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := Compile(s, PlatformCatalog())
	if !p.Valid() {
		t.Fatal(ds)
	}
	return p
}
func TestIntegrityDeterminismStorageAndImmutableReaders(t *testing.T) {
	d := integrityDocument(t)
	p := compileIntegrity(t, d)
	if p.IR().IRVersion != IntegrityIRVersion || p.IR().CompilerVersion != IntegrityCompilerVersion || !HasUniqueConstraints(p) {
		t.Fatal("missing integrity version")
	}
	slices.Reverse(d.Definitions)
	again := compileIntegrity(t, d)
	if !bytes.Equal(p.JSON(), again.JSON()) {
		t.Fatal("nondeterministic integrity IR")
	}
	r, err := Restore(p.JSON(), p.Hash(), SupportedIRVersions())
	if err != nil || !bytes.Equal(p.JSON(), r.JSON()) {
		t.Fatal("immutable restore", err)
	}
	if _, err = Restore(p.JSON(), p.Hash(), []string{IRVersion, RuleIRVersion, WorkflowIRVersion, ApprovalIRVersion}); err == nil {
		t.Fatal("old reader accepted new IR")
	}
	for i := range d.Definitions {
		if d.Definitions[i].Field != nil {
			d.Definitions[i].Field.Unique = false
		}
	}
	plain := compileIntegrity(t, d)
	_, h, err := Storage(p)
	_, other, e := Storage(plain)
	if err != nil || e != nil || h != other || p.Hash() == plain.Hash() {
		t.Fatal("integrity changed physical identity")
	}
	if _, err = PlanEvolution(plain, p, EvolutionIntent{}); err == nil {
		t.Fatal("unique evolution admitted")
	}
	if _, err = PlanEvolution(p, plain, EvolutionIntent{}); err == nil {
		t.Fatal("unique removal admitted")
	}
	for _, mutate := range []func(*IR){func(x *IR) { x.CompilerVersion = "other" }, func(x *IR) { x.IRVersion = ApprovalIRVersion }, func(x *IR) { x.Entities[0].Fields[1].Unique = !x.Entities[0].Fields[1].Unique }} {
		x := p.IR()
		mutate(&x)
		raw, _ := json.Marshal(x)
		if _, err = Restore(raw, digest(raw), SupportedIRVersions()); err == nil {
			t.Fatal("forged integrity witness accepted")
		}
	}
}
func TestIntegritySchemaTypesAndBudgets(t *testing.T) {
	v, _ := metadata.NewValidator()
	for _, version := range []string{metadata.SchemaVersion, metadata.RuleSchemaVersion, metadata.WorkflowSchemaVersion, metadata.ApprovalSchemaVersion} {
		d := integrityDocument(t)
		d.SchemaVersion = version
		for i := range d.Definitions {
			d.Definitions[i].SchemaVersion = version
		}
		raw, _ := json.Marshal(d)
		if _, err := v.Parse(raw); err == nil {
			t.Fatal("old schema accepted uniqueness", version)
		}
	}
	d := integrityDocument(t)
	for i := range d.Definitions {
		if d.Definitions[i].ID == "field.contact.id" {
			d.Definitions[i].Field.Unique = true
		}
	}
	raw, _ := json.Marshal(d)
	s, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := Compile(s, PlatformCatalog()); p.Valid() {
		t.Fatal("intrinsic unique admitted")
	}
	// Nine declarations fit the ordinary field budget but exceed integrity quota.
	d = metadata.Document{SchemaVersion: metadata.IntegritySchemaVersion, AppID: "app.integrity", Namespace: "example.integrity"}
	e := metadata.Definition{ID: "entity.item", Kind: "entity", Label: "Item", Entity: &metadata.Entity{Fields: []string{}}}
	for i := range 9 {
		id := fmt.Sprintf("field.item.key%d", i)
		e.Entity.Fields = append(e.Entity.Fields, id)
		d.Definitions = append(d.Definitions, metadata.Definition{ID: id, Kind: "field", Label: "Key", Field: &metadata.Field{EntityRef: e.ID, Type: "string", Unique: true}})
	}
	d.Definitions = append(d.Definitions, e)
	for i := range d.Definitions {
		d.Definitions[i].Namespace = d.Namespace
		d.Definitions[i].SchemaVersion = d.SchemaVersion
	}
	raw, _ = json.Marshal(d)
	s, err = v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := Compile(s, PlatformCatalog())
	if p.Valid() || len(ds) == 0 || ds[0].Code != "unique_contract" {
		t.Fatal("quota accepted", ds)
	}
}
