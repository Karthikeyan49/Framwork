package compiler

import (
	"sort"
)

// Storage evolution classes returned by ClassifyStorage.
const (
	StorageEqual     = "EQUAL"
	StorageAdditive  = "ADDITIVE"
	StorageTransform = "TRANSFORM"
	StorageBreaking  = "BREAKING"
)

// StorageDelta classifies how the stored data layout changes between two
// programs. It never errors: anything it cannot prove safe is BREAKING with a
// machine-readable reason, so callers cannot mistake a failure for EQUAL.
//
//   - EQUAL: the storage schema hashes are identical.
//   - ADDITIVE: PlanEvolution succeeds with only ADD_ENTITY / ADD_OPTIONAL.
//   - TRANSFORM: PlanEvolution succeeds with any other operation (RENAME,
//     CONVERT, RELAX_REQUIRED, BACKFILL).
//   - BREAKING: PlanEvolution refuses (or a program has no storage layout).
//
// A zero base is the first release: ADDITIVE with FirstRelease set and a
// BaseSchemaHash of "". A non-empty intent with identical schemas is reported by
// the UNUSED_INTENT reason so a builder can reject the request.
type StorageDelta struct {
	Class             string   `json:"class"`
	Reasons           []string `json:"reasons"`
	FirstRelease      bool     `json:"first_release"`
	BaseSchemaHash    string   `json:"base_schema_hash"`
	TargetSchemaHash  string   `json:"target_schema_hash"`
	EvolutionPlanHash string   `json:"evolution_plan_hash"`
}

func breaking(d StorageDelta, reasons ...string) StorageDelta {
	d.Class = StorageBreaking
	d.Reasons = uniqueSorted(reasons)
	return d
}

func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ClassifyStorage derives the class from the same PlanEvolution that the data
// executor uses, so classification and executability cannot disagree.
func ClassifyStorage(base, target Program, intent EvolutionIntent) StorageDelta {
	delta := StorageDelta{Reasons: []string{}}
	next, nextHash, err := Storage(target)
	if err != nil {
		return breaking(delta, "INVALID_PROGRAM")
	}
	delta.TargetSchemaHash = nextHash
	if !base.Valid() {
		delta.FirstRelease = true
		delta.Class = StorageAdditive
		if len(next.Entities) > 0 {
			delta.Reasons = []string{"ADD_ENTITY"}
		}
		if len(intent.Renames) > 0 || len(intent.Backfills) > 0 {
			return breaking(delta, "UNUSED_INTENT")
		}
		return delta
	}
	old, oldHash, err := Storage(base)
	if err != nil {
		return breaking(delta, "INVALID_PROGRAM")
	}
	delta.BaseSchemaHash = oldHash
	if old.AppID != next.AppID {
		return breaking(delta, "APP_MISMATCH")
	}
	if old.Layout != next.Layout {
		return breaking(delta, "LAYOUT_MISMATCH")
	}
	if oldHash == nextHash {
		delta.Class = StorageEqual
		if len(intent.Renames) > 0 || len(intent.Backfills) > 0 {
			delta.Reasons = []string{"UNUSED_INTENT"}
		}
		return delta
	}
	plan, err := PlanEvolution(base, target, intent)
	if err != nil {
		return breaking(delta, explainRefusal(old, next, intent)...)
	}
	delta.EvolutionPlanHash = plan.Hash()
	ops := plan.Artifact().Operations
	kinds := make([]string, 0, len(ops))
	additive := plan.Artifact().Class == "COMPATIBLE"
	for _, op := range ops {
		kinds = append(kinds, op.Kind)
		if op.Kind != "ADD_ENTITY" && op.Kind != "ADD_OPTIONAL" {
			additive = false
		}
	}
	delta.Reasons = uniqueSorted(kinds)
	switch {
	case len(ops) == 0:
		// Different schema hashes must imply at least one operation; refuse to
		// call an unexplained difference safe.
		return breaking(delta, "UNEXPLAINED_DIFFERENCE")
	case additive:
		delta.Class = StorageAdditive
	default:
		delta.Class = StorageTransform
	}
	return delta
}

// explainRefusal inspects both storage schemas to name why PlanEvolution
// refused. The classification itself never depends on these reasons.
func explainRefusal(old, next StorageSchema, intent EvolutionIntent) []string {
	reasons := []string{}
	oldEntities := map[string]StorageEntity{}
	for _, e := range old.Entities {
		oldEntities[e.ID] = e
	}
	newEntities := map[string]StorageEntity{}
	for _, e := range next.Entities {
		newEntities[e.ID] = e
	}
	renamedFrom, renamedTo := map[string]bool{}, map[string]bool{}
	for _, r := range intent.Renames {
		renamedFrom[r.EntityID+"/"+r.From] = true
		renamedTo[r.EntityID+"/"+r.To] = true
	}
	filled := map[string]bool{}
	for _, b := range intent.Backfills {
		filled[b.EntityID+"/"+b.FieldID] = true
	}
	for id, e := range oldEntities {
		n, ok := newEntities[id]
		if !ok {
			reasons = append(reasons, "REMOVED_ENTITY")
			continue
		}
		newFields := schemaFields(n)
		for _, f := range e.Fields {
			g, ok := newFields[f.ID]
			if !ok {
				if !renamedFrom[id+"/"+f.ID] {
					reasons = append(reasons, "REMOVED_FIELD")
				}
				continue
			}
			switch {
			case f.CoreID != g.CoreID:
				reasons = append(reasons, "CORE_ID_CHANGE")
			case f.Currency != g.Currency:
				reasons = append(reasons, "CURRENCY_CHANGE")
			case f.ReferenceTo != g.ReferenceTo:
				reasons = append(reasons, "REFERENCE_CHANGE")
			case !equalStrings(f.EnumValues, g.EnumValues):
				reasons = append(reasons, "OPTIONS_CHANGE")
			case f.Type != g.Type && !((f.Type == "integer" && g.Type == "decimal") || (f.Type == "decimal" && g.Type == "integer")):
				reasons = append(reasons, "TYPE_CHANGE")
			case f.Type != g.Type && g.CoreID:
				reasons = append(reasons, "TYPE_CHANGE")
			}
			if g.Required && !f.Required && !filled[id+"/"+f.ID] {
				reasons = append(reasons, "MISSING_BACKFILL")
			}
		}
		oldFields := schemaFields(e)
		for _, g := range n.Fields {
			if _, existed := oldFields[g.ID]; !existed && g.Required && !filled[id+"/"+g.ID] && !renamedTo[id+"/"+g.ID] {
				reasons = append(reasons, "MISSING_BACKFILL")
			}
		}
		// A removed field plus an added field of the same type in one entity is
		// the signature of an undeclared rename.
		for _, f := range e.Fields {
			if _, kept := newFields[f.ID]; kept || renamedFrom[id+"/"+f.ID] {
				continue
			}
			for _, g := range n.Fields {
				if _, existed := oldFields[g.ID]; !existed && g.Type == f.Type && !renamedTo[id+"/"+g.ID] {
					reasons = append(reasons, "UNDECLARED_RENAME")
				}
			}
		}
	}
	// Instructions that name nothing in either schema are an invalid request.
	invalidIntent := len(intent.Renames) > 2048 || len(intent.Backfills) > 2048
	for _, r := range intent.Renames {
		o, ok1 := oldEntities[r.EntityID]
		n, ok2 := newEntities[r.EntityID]
		if !ok1 || !ok2 {
			invalidIntent = true
			continue
		}
		_, hadFrom := schemaFields(o)[r.From]
		_, hasTo := schemaFields(n)[r.To]
		_, hadTo := schemaFields(o)[r.To]
		if !hadFrom || !hasTo || hadTo {
			invalidIntent = true
		}
	}
	for _, b := range intent.Backfills {
		n, ok := newEntities[b.EntityID]
		if _, has := schemaFields(n)[b.FieldID]; !ok || !has {
			invalidIntent = true
		}
	}
	if invalidIntent {
		reasons = append(reasons, "INVALID_INTENT")
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "UNSUPPORTED_EVOLUTION")
	}
	return reasons
}
