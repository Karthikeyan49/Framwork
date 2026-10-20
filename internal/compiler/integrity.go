package compiler

import (
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// HasUniqueConstraints describes an immutable integrity requirement, not proof
// that a physical namespace has built or verified its claims.
func HasUniqueConstraints(p Program) bool {
	for _, e := range p.IR().Entities {
		for _, f := range e.Fields {
			if f.Unique {
				return true
			}
		}
	}
	return false
}

func validateIntegrity(version string, entities []Entity) error {
	total := 0
	for _, e := range entities {
		count := 0
		for _, f := range e.Fields {
			if !f.Unique {
				continue
			}
			if !metadata.IntegritySchema(version) || f.CoreID || !contains([]string{"string", "integer", "decimal", "money", "bool", "date", "datetime", "enum", "reference"}, f.Type) {
				return errors.New("unsupported unique field")
			}
			count++
		}
		if count > 8 {
			return errors.New("unique entity budget")
		}
		total += count
	}
	if total > 32 {
		return errors.New("unique application budget")
	}
	return nil
}
