package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

const StorageContractVersion = "dynamic-jsonb-schema-v1"
const EvolutionVersion = "dynamic-jsonb-evolution-v1"

var ErrEvolution = errors.New("unsupported or invalid storage evolution")

// StorageSchema identifies data independently of compiler/catalog/UI identity.
// It does not grant access or activate a release.
type StorageSchema struct {
	Version  string          `json:"version"`
	AppID    string          `json:"app_id"`
	Layout   string          `json:"layout"`
	Entities []StorageEntity `json:"entities"`
}

type StorageEntity struct {
	ID     string         `json:"id"`
	Fields []StorageField `json:"fields"`
}

type StorageField struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	CoreID      bool     `json:"core_id"`
	EnumValues  []string `json:"enum_values,omitempty"`
	Currency    string   `json:"currency,omitempty"`
	ReferenceTo string   `json:"reference_to,omitempty"`
}

func Storage(p Program) (StorageSchema, string, error) {
	if !p.Valid() || p.IR().SchemaSnapshot != SchemaSnapshot {
		return StorageSchema{}, "", ErrEvolution
	}
	ir := p.IR()
	s := StorageSchema{Version: StorageContractVersion, AppID: ir.Application.AppID, Layout: ir.SchemaSnapshot, Entities: []StorageEntity{}}
	for _, e := range ir.Entities {
		entity := StorageEntity{ID: e.ID, Fields: []StorageField{}}
		for _, f := range e.Fields {
			enums := append([]string(nil), f.EnumValues...)
			sort.Strings(enums)
			entity.Fields = append(entity.Fields, StorageField{f.ID, f.Type, f.Required, f.CoreID, enums, f.Currency, f.ReferenceTo})
		}
		sort.Slice(entity.Fields, func(i, j int) bool { return entity.Fields[i].ID < entity.Fields[j].ID })
		s.Entities = append(s.Entities, entity)
	}
	sort.Slice(s.Entities, func(i, j int) bool { return s.Entities[i].ID < s.Entities[j].ID })
	raw, err := json.Marshal(s)
	if err != nil {
		return StorageSchema{}, "", ErrEvolution
	}
	return s, digest(raw), nil
}

type FieldRename struct {
	EntityID string `json:"entity_id"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// Backfill distinguishes absent properties from explicit null. Values are typed
// by the data executor; planning alone cannot attest that a data set is valid.
type FieldBackfill struct {
	EntityID string          `json:"entity_id"`
	FieldID  string          `json:"field_id"`
	When     string          `json:"when"` // MISSING or MISSING_OR_NULL
	Value    json.RawMessage `json:"value"`
}

type EvolutionIntent struct {
	Renames   []FieldRename   `json:"renames"`
	Backfills []FieldBackfill `json:"backfills"`
}

type EvolutionOperation struct {
	Kind     string `json:"kind"`
	EntityID string `json:"entity_id"`
	FieldID  string `json:"field_id,omitempty"`
	From     string `json:"from,omitempty"`
	FromType string `json:"from_type,omitempty"`
	ToType   string `json:"to_type,omitempty"`
}

type EvolutionArtifact struct {
	Version        string               `json:"version"`
	BaseIRHash     string               `json:"base_ir_hash"`
	TargetIRHash   string               `json:"target_ir_hash"`
	BaseSchemaHash string               `json:"base_schema_hash"`
	SchemaHash     string               `json:"schema_hash"`
	Class          string               `json:"class"` // COMPATIBLE or TRANSFORM
	Intent         EvolutionIntent      `json:"intent"`
	Operations     []EvolutionOperation `json:"operations"`
}

// EvolutionPlan is an immutable compiler artifact, not a deployment approval.
type EvolutionPlan struct {
	raw  []byte
	hash string
}

func (p EvolutionPlan) Valid() bool           { return len(p.raw) > 0 && p.hash != "" }
func (p EvolutionPlan) Hash() string          { return p.hash }
func (p EvolutionPlan) JSON() json.RawMessage { return append(json.RawMessage(nil), p.raw...) }
func (p EvolutionPlan) Artifact() EvolutionArtifact {
	var a EvolutionArtifact
	_ = json.Unmarshal(p.raw, &a)
	return a
}
func (p EvolutionPlan) Matches(base, target Program) bool {
	return p.Valid() && base.Valid() && target.Valid() && p.Artifact().BaseIRHash == base.Hash() && p.Artifact().TargetIRHash == target.Hash()
}

func schemaFields(e StorageEntity) map[string]StorageField {
	out := make(map[string]StorageField, len(e.Fields))
	for _, f := range e.Fields {
		out[f.ID] = f
	}
	return out
}

// PlanEvolution compares explicit immutable programs. Standalone Compile keeps
// its historical NONE layout; candidate builders must hash this separate plan.
// No destructive operation, inferred rename, SQL, or arbitrary conversion exists.
func PlanEvolution(base, target Program, intent EvolutionIntent) (EvolutionPlan, error) {
	old, oldHash, err := Storage(base)
	if err != nil {
		return EvolutionPlan{}, err
	}
	next, nextHash, err := Storage(target)
	if err != nil || old.AppID != next.AppID || old.Layout != next.Layout || len(intent.Renames) > 2048 || len(intent.Backfills) > 2048 {
		return EvolutionPlan{}, ErrEvolution
	}
	// Copy and normalize caller-owned slices/JSON before constructing the artifact.
	intent.Renames = append([]FieldRename{}, intent.Renames...)
	intent.Backfills = append([]FieldBackfill{}, intent.Backfills...)
	renames := map[string]FieldRename{}
	renameSources := map[string]bool{}
	backfills := map[string]FieldBackfill{}
	for _, r := range intent.Renames {
		key, from := r.EntityID+"/"+r.To, r.EntityID+"/"+r.From
		if r.From == r.To || r.EntityID == "" || r.From == "" || r.To == "" || renames[key].To != "" || renameSources[from] {
			return EvolutionPlan{}, ErrEvolution
		}
		renames[key], renameSources[from] = r, true
	}
	for i, b := range intent.Backfills {
		key := b.EntityID + "/" + b.FieldID
		if b.EntityID == "" || b.FieldID == "" || backfills[key].FieldID != "" || (b.When != "MISSING" && b.When != "MISSING_OR_NULL") || len(b.Value) > 64<<10 {
			return EvolutionPlan{}, ErrEvolution
		}
		value, e := metadata.DecodeJSON(b.Value)
		if e != nil || value == nil {
			return EvolutionPlan{}, ErrEvolution
		}
		b.Value, e = json.Marshal(value)
		if e != nil {
			return EvolutionPlan{}, ErrEvolution
		}
		intent.Backfills[i] = b
		backfills[key] = b
	}
	sort.Slice(intent.Renames, func(i, j int) bool {
		a, b := intent.Renames[i], intent.Renames[j]
		return a.EntityID+"/"+a.To < b.EntityID+"/"+b.To
	})
	sort.Slice(intent.Backfills, func(i, j int) bool {
		a, b := intent.Backfills[i], intent.Backfills[j]
		return a.EntityID+"/"+a.FieldID < b.EntityID+"/"+b.FieldID
	})
	a := EvolutionArtifact{EvolutionVersion, base.Hash(), target.Hash(), oldHash, nextHash, "COMPATIBLE", intent, []EvolutionOperation{}}
	oldEntities := map[string]StorageEntity{}
	for _, e := range old.Entities {
		oldEntities[e.ID] = e
	}
	usedRenames, usedBackfills := map[string]bool{}, map[string]bool{}
	for _, e := range next.Entities {
		previous, exists := oldEntities[e.ID]
		if !exists {
			a.Operations = append(a.Operations, EvolutionOperation{Kind: "ADD_ENTITY", EntityID: e.ID})
			continue // New entities have no existing rows to backfill.
		}
		delete(oldEntities, e.ID)
		oldFields := schemaFields(previous)
		usedFields := map[string]bool{}
		for _, f := range e.Fields {
			key := e.ID + "/" + f.ID
			prior, found := oldFields[f.ID]
			if rename, ok := renames[key]; ok {
				if found { // Swaps, overwrite and ambiguous merges are forbidden.
					return EvolutionPlan{}, ErrEvolution
				}
				prior, found = oldFields[rename.From]
				if !found || prior.CoreID || f.CoreID {
					return EvolutionPlan{}, ErrEvolution
				}
				usedRenames[key] = true
				a.Class = "TRANSFORM"
				a.Operations = append(a.Operations, EvolutionOperation{Kind: "RENAME", EntityID: e.ID, FieldID: f.ID, From: rename.From})
			}
			if found {
				if usedFields[prior.ID] || prior.CoreID != f.CoreID || prior.Currency != f.Currency || prior.ReferenceTo != f.ReferenceTo || !equalStrings(prior.EnumValues, f.EnumValues) {
					return EvolutionPlan{}, ErrEvolution
				}
				usedFields[prior.ID] = true
				if prior.Type != f.Type {
					if !((prior.Type == "integer" && f.Type == "decimal") || (prior.Type == "decimal" && f.Type == "integer")) || f.CoreID {
						return EvolutionPlan{}, ErrEvolution
					}
					a.Class = "TRANSFORM"
					a.Operations = append(a.Operations, EvolutionOperation{Kind: "CONVERT", EntityID: e.ID, FieldID: f.ID, FromType: prior.Type, ToType: f.Type})
				}
				if prior.Required && !f.Required {
					// Old readers require this value: retain a separate generation.
					a.Class = "TRANSFORM"
					a.Operations = append(a.Operations, EvolutionOperation{Kind: "RELAX_REQUIRED", EntityID: e.ID, FieldID: f.ID})
				}
			} else if !f.Required {
				a.Operations = append(a.Operations, EvolutionOperation{Kind: "ADD_OPTIONAL", EntityID: e.ID, FieldID: f.ID})
			}
			backfill, filled := backfills[key]
			needsFill := f.Required && (!found || !prior.Required)
			if needsFill && (!filled || backfill.When != "MISSING_OR_NULL") || filled && (f.CoreID || found && prior.Required) {
				return EvolutionPlan{}, ErrEvolution
			}
			if filled {
				usedBackfills[key] = true
				a.Class = "TRANSFORM"
				a.Operations = append(a.Operations, EvolutionOperation{Kind: "BACKFILL", EntityID: e.ID, FieldID: f.ID})
			}
		}
		if len(usedFields) != len(oldFields) {
			return EvolutionPlan{}, ErrEvolution // Field removal requires another design.
		}
	}
	if len(oldEntities) != 0 || len(usedRenames) != len(renames) || len(usedBackfills) != len(backfills) {
		return EvolutionPlan{}, ErrEvolution
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return EvolutionPlan{}, ErrEvolution
	}
	return EvolutionPlan{raw, digest(raw)}, nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RestoreEvolution validates stored canonical bytes against their original base
// and target programs. It never recompiles, approves, or changes either program.
func RestoreEvolution(base, target Program, raw []byte, expectedHash string) (EvolutionPlan, error) {
	if len(raw) > 2<<20 || digest(raw) != expectedHash {
		return EvolutionPlan{}, ErrEvolution
	}
	var a EvolutionArtifact
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&a) != nil {
		return EvolutionPlan{}, ErrEvolution
	}
	plan, err := PlanEvolution(base, target, a.Intent)
	if err != nil || !bytes.Equal(plan.raw, raw) {
		return EvolutionPlan{}, ErrEvolution
	}
	return plan, nil
}
