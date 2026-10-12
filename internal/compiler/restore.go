package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"sort"
	"strings"
)

// Restore reads an already compiled immutable release. It never invokes Compile
// or replaces its compiler version with the current binary's version.
func Restore(raw []byte, expectedHash string, supportedIR []string) (Program, error) {
	if len(raw) == 0 || len(raw) > 4<<20 || digest(raw) != expectedHash {
		return Program{}, errors.New("stored release integrity failure")
	}
	var ir IR
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&ir) != nil || !contains(supportedIR, ir.IRVersion) || ir.CompilerVersion == "" || ir.CatalogHash == "" || ir.SchemaSnapshot == "" || ir.Application.AppID == "" {
		return Program{}, errors.New("unsupported or invalid stored IR")
	}
	canonical, err := json.Marshal(ir)
	if err != nil || !bytes.Equal(canonical, raw) {
		return Program{}, errors.New("stored IR is not canonical")
	}
	seen := map[string]bool{}
	for _, e := range ir.Entities {
		if e.ID == "" || seen[e.ID] || len(e.Fields) < 1 || len(e.Fields) > 32 {
			return Program{}, errors.New("invalid stored entity")
		}
		seen[e.ID] = true
		fields := map[string]bool{}
		for _, f := range e.Fields {
			if f.ID == "" || fields[f.ID] {
				return Program{}, errors.New("invalid stored field")
			}
			fields[f.ID] = true
		}
	}
	if validateStoredEntities(ir) != nil {
		return Program{}, errors.New("stored runtime model disagrees with application")
	}
	if validateStoredRules(ir) != nil {
		return Program{}, errors.New("stored rule model disagrees with application")
	}
	return Program{raw: append([]byte(nil), raw...), hash: expectedHash}, nil
}

// Verify the versioned physical-layout derivation, not today's compiler pipeline.
func validateStoredEntities(ir IR) error {
	if !contains(SupportedIRVersions(), ir.IRVersion) || ir.SchemaSnapshot != SchemaSnapshot || ir.Migration.Class != "NONE" || len(ir.Migration.Steps) != 0 {
		return errors.New("unsupported stored layout")
	}
	v, e := metadata.NewValidator()
	if e != nil {
		return e
	}
	raw, e := json.Marshal(ir.Application)
	if e != nil {
		return e
	}
	if _, e = v.Parse(raw); e != nil {
		return e
	}
	definitions := map[string]metadata.Definition{}
	for _, d := range ir.Application.Definitions {
		definitions[d.ID] = d
	}
	derived := []Entity{}
	used := map[string]bool{}
	for _, d := range ir.Application.Definitions {
		if d.Entity == nil {
			continue
		}
		entity := Entity{ID: d.ID, Label: d.Label, Fields: []Field{}}
		for _, id := range d.Entity.Fields {
			fd, ok := definitions[id]
			if !ok || fd.Field == nil || fd.Field.EntityRef != d.ID || used[id] {
				return errors.New("invalid stored field ownership")
			}
			used[id] = true
			f := fd.Field
			core := id == "field."+strings.TrimPrefix(d.ID, "entity.")+".id"
			if core && (f.Type != "string" || !f.Required) {
				return errors.New("invalid core ID")
			}
			values := append([]string{}, f.EnumValues...)
			sort.Strings(values)
			entity.Fields = append(entity.Fields, Field{ID: id, Label: fd.Label, Type: f.Type, Required: f.Required, CoreID: core, EnumValues: values, Currency: f.Currency, ReferenceTo: f.ReferenceTo})
		}
		sort.Slice(entity.Fields, func(i, j int) bool { return entity.Fields[i].ID < entity.Fields[j].ID })
		derived = append(derived, entity)
	}
	for _, d := range ir.Application.Definitions {
		if d.Field != nil && !used[d.ID] {
			return errors.New("orphan stored field")
		}
	}
	sort.Slice(derived, func(i, j int) bool { return derived[i].ID < derived[j].ID })
	want, _ := json.Marshal(derived)
	actual, _ := json.Marshal(ir.Entities)
	if !bytes.Equal(want, actual) {
		return errors.New("inconsistent stored runtime layout")
	}
	return nil
}
