package metadata

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

func TestEmbeddedSchemasKeepConcurrentSourcesAndFailuresIsolated(t *testing.T) {
	type fixture struct {
		raw      []byte
		expected Source
	}
	fixtures := []fixture{}
	for _, version := range []string{SchemaVersion, RuleSchemaVersion, WorkflowSchemaVersion, ApprovalSchemaVersion, IntegritySchemaVersion} {
		d := Document{SchemaVersion: version, AppID: "app.cache", Namespace: "example.cache", Definitions: []Definition{
			{ID: "entity.item", Kind: "entity", Label: "Item", Entity: &Entity{Fields: []string{"field.item.key"}}},
			{ID: "field.item.key", Kind: "field", Label: "Key", Field: &Field{EntityRef: "entity.item", Type: "string", Unique: version == IntegritySchemaVersion}},
		}}
		for i := range d.Definitions {
			d.Definitions[i].SchemaVersion = version
			d.Definitions[i].Namespace = d.Namespace
		}
		raw, _ := json.Marshal(d)
		v := validator(t)
		s, err := v.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		fixtures = append(fixtures, fixture{raw, s})
	}
	var group sync.WaitGroup
	for i := range 32 {
		group.Go(func() {
			for round := range 5 {
				f := fixtures[(i+round)%len(fixtures)]
				v, err := NewValidator()
				if err != nil {
					t.Error(err)
					return
				}
				if _, err = v.Parse([]byte(`{"schema_version":"1.4.0","unknown":true}`)); err == nil {
					t.Error("invalid source accepted")
					return
				}
				s, err := v.Parse(f.raw)
				if err != nil || s.Hash() != f.expected.Hash() || !bytes.Equal(s.JSON(), f.expected.JSON()) {
					t.Error("request state leaked across fixed-schema validation")
					return
				}
				copy := s.JSON()
				copy[0] = '!'
				if !bytes.Equal(s.JSON(), f.expected.JSON()) {
					t.Error("source alias escaped")
					return
				}
			}
		})
	}
	group.Wait()
	// Simulate a package-local caller invalidating its own returned handle. This
	// must neither mutate the cached graph nor another caller's container.
	v := validator(t)
	v.schema = nil
	next := validator(t)
	if _, err := next.Parse(fixtures[0].raw); err != nil {
		t.Fatal("validator containers alias", err)
	}
}
