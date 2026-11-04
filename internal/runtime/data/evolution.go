package data

import (
	"encoding/json"
	"math/big"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// TransformRecord validates a closed evolution against an actual record. It is
// pure: source maps and stored IR are unchanged. A migration must still verify
// references/ACLs/manifests and commit checkpoints before activating its output.
func TransformRecord(p compiler.EvolutionPlan, base, target compiler.Program, entity, id string, input map[string]any) (map[string]any, error) {
	if !p.Matches(base, target) {
		return nil, ErrInvalid
	}
	if err := ValidateBackfills(p, target); err != nil {
		return nil, err
	}
	old, exists := base.Entity(entity)
	next, targetExists := target.Entity(entity)
	if !exists || !targetExists {
		return nil, ErrInvalid
	}
	// Persisted JSONB must contain no synthetic core ID or undeclared property.
	validated, err := values(old, input, id, true)
	if err != nil {
		return nil, err
	}
	a := p.Artifact()
	renames := map[string]string{}
	for _, r := range a.Intent.Renames {
		if r.EntityID == entity {
			renames[r.To] = r.From
		}
	}
	backfills := map[string]compiler.FieldBackfill{}
	for _, b := range a.Intent.Backfills {
		if b.EntityID == entity {
			backfills[b.FieldID] = b
		}
	}
	out := map[string]any{}
	for _, f := range next.Fields {
		if f.CoreID {
			continue
		}
		from := f.ID
		if renamed, ok := renames[f.ID]; ok {
			from = renamed
		}
		v, found := validated[from]
		prior, hadField := field(old, from)
		if found && v != nil && hadField && prior.Type != f.Type {
			v, err = convertNumber(prior.Type, f.Type, v)
			if err != nil {
				return nil, err
			}
		}
		if b, ok := backfills[f.ID]; ok && (!found || v == nil && b.When == "MISSING_OR_NULL") {
			v, err = metadata.DecodeJSON(b.Value)
			if err != nil {
				return nil, ErrInvalid
			}
			found = true
		}
		if found {
			out[f.ID] = v
		}
	}
	normalized, err := values(next, out, id, true)
	if err != nil {
		return nil, err
	}
	for _, f := range next.Fields {
		if f.CoreID {
			delete(normalized, f.ID)
		}
	}
	return normalized, nil
}

func convertNumber(from, to string, value any) (any, error) {
	if from == "integer" && to == "decimal" {
		n, ok := value.(json.Number)
		if !ok {
			return nil, ErrInvalid
		}
		return decimal(n.String())
	}
	if from == "decimal" && to == "integer" {
		s, ok := value.(string)
		if !ok {
			return nil, ErrInvalid
		}
		n, ok := new(big.Rat).SetString(s)
		if !ok || !n.IsInt() || !n.Num().IsInt64() || n.Num().Int64() < -9007199254740991 || n.Num().Int64() > 9007199254740991 {
			return nil, ErrInvalid
		}
		return json.Number(n.Num().String()), nil
	}
	return nil, ErrInvalid
}

// ValidateBackfills runs typed checks even for empty datasets. Passing this check
// is necessary, not evidence that all source records can be transformed.
func ValidateBackfills(p compiler.EvolutionPlan, target compiler.Program) error {
	if !p.Valid() || !target.Valid() || p.Artifact().TargetIRHash != target.Hash() {
		return ErrInvalid
	}
	for _, b := range p.Artifact().Intent.Backfills {
		e, ok := target.Entity(b.EntityID)
		if !ok {
			return ErrInvalid
		}
		f, ok := field(e, b.FieldID)
		if !ok || f.CoreID {
			return ErrInvalid
		}
		value, err := metadata.DecodeJSON(b.Value)
		if err != nil {
			return ErrInvalid
		}
		if _, err = fieldValue(f, value); err != nil {
			return err
		}
	}
	return nil
}
