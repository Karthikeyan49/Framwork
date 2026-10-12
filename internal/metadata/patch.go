package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

type PatchOperation struct {
	Op, Target, ExpectedHash, ChangeClass string
	Definition                            *Definition
}
type ApplicationPatch struct {
	Version, BaseRelease, BaseIRHash string
	Operations                       []PatchOperation
}
type PatchConflict struct{ Target, Reason string }

func definitionHash(d Definition) string {
	raw, _ := json.Marshal(d)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// DefinitionHash is the exact typed base snapshot digest used by patch authors.
func DefinitionHash(d Definition) string { return definitionHash(d) }

// Rebase merges whole typed definitions; sensitive edits always require human resolution.
func (p ApplicationPatch) Rebase(baseRelease, baseHash string, base, current Document) (Document, []PatchConflict, error) {
	if p.Version != "1.0.0" || p.BaseRelease != baseRelease || p.BaseIRHash != baseHash || len(p.Operations) == 0 || len(p.Operations) > 100 || base.AppID != current.AppID {
		return Document{}, nil, errors.New("invalid patch base")
	}
	v, e := NewValidator()
	if e != nil {
		return Document{}, nil, e
	}
	for _, doc := range []Document{base, current} {
		raw, e := json.Marshal(doc)
		if e != nil {
			return Document{}, nil, e
		}
		if _, e = v.Parse(raw); e != nil {
			return Document{}, nil, e
		}
	}
	b := map[string]Definition{}
	c := map[string]Definition{}
	for _, d := range base.Definitions {
		if _, exists := b[d.ID]; exists {
			return Document{}, nil, errors.New("duplicate base definition")
		}
		b[d.ID] = d
	}
	for _, d := range current.Definitions {
		if _, exists := c[d.ID]; exists {
			return Document{}, nil, errors.New("duplicate current definition")
		}
		c[d.ID] = d
	}
	conflicts := []PatchConflict{}
	seen := map[string]bool{}
	for _, op := range p.Operations {
		if op.Target == "" || seen[op.Target] {
			return Document{}, nil, errors.New("duplicate patch target")
		}
		seen[op.Target] = true
		if op.ChangeClass != "PRESENTATION" {
			conflicts = append(conflicts, PatchConflict{op.Target, "sensitive change requires explicit resolution"})
			continue
		}
		old, exists := b[op.Target]
		now, currentExists := c[op.Target]
		if exists && (op.ExpectedHash != definitionHash(old) || !currentExists || definitionHash(now) != definitionHash(old)) {
			conflicts = append(conflicts, PatchConflict{op.Target, "BASE_DRAFT_CURRENT conflict"})
			continue
		}
		if op.Op == "add" {
			if exists || currentExists || op.ExpectedHash != "" {
				conflicts = append(conflicts, PatchConflict{op.Target, "add collision"})
				continue
			}
		} else if !exists {
			return Document{}, nil, errors.New("missing patch target")
		}
		switch op.Op {
		case "add", "update":
			if op.Op == "update" && old.Kind != "page" {
				return Document{}, nil, errors.New("sensitive kind change")
			}
			if op.Definition == nil || op.Definition.ID != op.Target || op.Definition.Kind != "page" {
				return Document{}, nil, errors.New("only presentation pages auto-merge")
			}
			c[op.Target] = *op.Definition
		case "delete":
			if old.Kind != "page" {
				return Document{}, nil, errors.New("sensitive delete")
			}
			delete(c, op.Target)
		default:
			return Document{}, nil, errors.New("unknown patch operation")
		}
	}
	if len(conflicts) > 0 {
		return Document{}, conflicts, nil
	}
	out := current
	out.Definitions = []Definition{}
	keys := []string{}
	for key := range c {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.Definitions = append(out.Definitions, c[key])
	}
	raw, e := json.Marshal(out)
	if e != nil {
		return Document{}, nil, e
	}
	if _, e = v.Parse(raw); e != nil {
		return Document{}, nil, e
	}
	return out, nil, nil
}
