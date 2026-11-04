package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// Change classes of a patch operation. Only the server derives the class; the
// class a caller declares is checked against it and never trusted.
const (
	ClassPresentation = "PRESENTATION"
	ClassSensitive    = "SENSITIVE"
)

// Typed conflict categories reported by Rebase. Any conflict means nothing was
// merged: a patch is applied entirely or not at all.
const (
	ConflictSensitiveChange      = "SENSITIVE_CHANGE"
	ConflictAddCollision         = "ADD_COLLISION"
	ConflictCurrentDiverged      = "CURRENT_DIVERGED"
	ConflictStaleExpectedHash    = "STALE_EXPECTED_HASH"
	ConflictDeclaredClassMistake = "DECLARED_CLASS_MISMATCH"
	ConflictMissingTarget        = "MISSING_TARGET"
)

type PatchOperation struct {
	Op, Target, ExpectedHash, ChangeClass string
	Definition                            *Definition
}
type ApplicationPatch struct {
	Version, BaseRelease, BaseIRHash string
	Operations                       []PatchOperation
}
type PatchConflict struct{ Target, Category, Reason string }

func definitionHash(d Definition) string {
	raw, _ := json.Marshal(d)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// DefinitionHash is the exact typed base snapshot digest used by patch authors.
func DefinitionHash(d Definition) string { return definitionHash(d) }

// Rebase merges whole typed page definitions into the current draft with a true
// three-way comparison of base (what the patch author saw), current (the draft
// now) and theirs (the patch). It never auto-applies anything but presentation:
// the class is derived here from the definition kinds on every side that
// exists, so a caller cannot declare an entity, field, rule, workflow or tool
// edit to be presentation. A change that current already contains (convergent)
// merges as a no-op; divergent edits are typed conflicts that need a human.
//
// The returned document is only a draft. It must be appended as a new metadata
// revision and then recompiled, re-sandboxed and re-accepted; rebasing never
// carries an approval, an acceptance or a release across.
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
	// Structural validation first: a malformed patch is an error, not a conflict.
	seen := map[string]bool{}
	for _, op := range p.Operations {
		if op.Target == "" || seen[op.Target] {
			return Document{}, nil, errors.New("duplicate patch target")
		}
		seen[op.Target] = true
		switch op.Op {
		case "add", "update":
			if op.Definition == nil || op.Definition.ID != op.Target {
				return Document{}, nil, errors.New("patch definition must carry its target id")
			}
		case "delete":
			if op.Definition != nil {
				return Document{}, nil, errors.New("delete carries no definition")
			}
		default:
			return Document{}, nil, errors.New("unknown patch operation")
		}
	}
	conflicts := []PatchConflict{}
	conflict := func(target, category, reason string) {
		conflicts = append(conflicts, PatchConflict{Target: target, Category: category, Reason: reason})
	}
	merged := make(map[string]Definition, len(c))
	for id, d := range c {
		merged[id] = d
	}
	for _, op := range p.Operations {
		old, inBase := b[op.Target]
		now, inCurrent := c[op.Target]
		// Derived class: presentation only if every side that exists is a page.
		derived := ClassPresentation
		if inBase && old.Kind != "page" || inCurrent && now.Kind != "page" || op.Definition != nil && op.Definition.Kind != "page" {
			derived = ClassSensitive
		}
		if derived == ClassSensitive {
			conflict(op.Target, ConflictSensitiveChange, "sensitive change requires explicit resolution")
			continue
		}
		if op.ChangeClass != derived {
			conflict(op.Target, ConflictDeclaredClassMistake, "declared change class does not match the derived class")
			continue
		}
		switch op.Op {
		case "add":
			switch {
			case op.ExpectedHash != "":
				conflict(op.Target, ConflictStaleExpectedHash, "an add carries no expected hash")
			case inBase:
				conflict(op.Target, ConflictAddCollision, "the target already exists in the base")
			case inCurrent && definitionHash(now) == definitionHash(*op.Definition):
				// Convergent: current already holds exactly this page.
			case inCurrent:
				conflict(op.Target, ConflictAddCollision, "the target was added differently in current")
			default:
				merged[op.Target] = *op.Definition
			}
		case "update":
			switch {
			case !inBase:
				conflict(op.Target, ConflictMissingTarget, "the base does not contain the target")
			case op.ExpectedHash != definitionHash(old):
				conflict(op.Target, ConflictStaleExpectedHash, "the patch was authored against another base definition")
			case !inCurrent:
				conflict(op.Target, ConflictCurrentDiverged, "the target was deleted in current")
			case definitionHash(now) == definitionHash(old):
				merged[op.Target] = *op.Definition
			case definitionHash(now) == definitionHash(*op.Definition):
				// Convergent: current already holds the patched page.
			default:
				conflict(op.Target, ConflictCurrentDiverged, "BASE_DRAFT_CURRENT conflict")
			}
		case "delete":
			switch {
			case !inBase:
				conflict(op.Target, ConflictMissingTarget, "the base does not contain the target")
			case op.ExpectedHash != definitionHash(old):
				conflict(op.Target, ConflictStaleExpectedHash, "the patch was authored against another base definition")
			case !inCurrent:
				// Convergent: already deleted.
			case definitionHash(now) == definitionHash(old):
				delete(merged, op.Target)
			default:
				conflict(op.Target, ConflictCurrentDiverged, "BASE_DRAFT_CURRENT conflict")
			}
		}
	}
	if len(conflicts) > 0 {
		sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Target < conflicts[j].Target })
		return Document{}, conflicts, nil
	}
	out := current
	out.Definitions = []Definition{}
	keys := []string{}
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.Definitions = append(out.Definitions, merged[key])
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
