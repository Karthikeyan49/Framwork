package risk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Closed vocabularies, ascending severity where an order exists.
var (
	classes       = []string{ClassEqual, ClassAdditive, ClassTransform, ClassBreaking}
	volumes       = []string{VolumeNone, VolumeSmall, VolumeLarge, VolumeUnverified}
	toolEffects   = []string{SideNone, SideRead, SideWrite, SideFinancial, SideDestructive}
	idempotencies = []string{IdemNativeKey, IdemLookupByReference, IdemDedupeWindow, IdemNone}
	toolChanges   = []string{ChangeAdded, ChangeRemoved, ChangeRepinned, ChangeInherited}
	declEffects   = []string{DeclNone, DeclRead, DeclWrite, DeclDestructive, DeclFinancial}
	declChanges   = []string{ChangeAdded, ChangeRemoved, ChangeChanged}
)

// boolFacts names the boolean facts a Trigger may test, by JSON name.
var boolFacts = []struct {
	name string
	get  func(*Facts) bool
}{
	{"first_release", func(f *Facts) bool { return f.FirstRelease }},
	{"ir_upgrade", func(f *Facts) bool { return f.IRUpgrade }},
	{"page_changed", func(f *Facts) bool { return f.PageChanged }},
	{"label_changed", func(f *Facts) bool { return f.LabelChanged }},
	{"entity_added", func(f *Facts) bool { return f.EntityAdded }},
	{"relationship_changed", func(f *Facts) bool { return f.RelationshipChanged }},
	{"rule_changed", func(f *Facts) bool { return f.RuleChanged }},
	{"financial_rule_changed", func(f *Facts) bool { return f.FinancialRuleChanged }},
	{"workflow_changed", func(f *Facts) bool { return f.WorkflowChanged }},
	{"trigger_widened", func(f *Facts) bool { return f.TriggerWidened }},
	{"approval_gate_changed", func(f *Facts) bool { return f.ApprovalGateChanged }},
	{"plan_only_changed", func(f *Facts) bool { return f.PlanOnlyChanged }},
	{"unclassified", func(f *Facts) bool { return f.Unclassified }},
	{"unknown_capability", func(f *Facts) bool { return f.UnknownCapability }},
	{"acceptance_author_is_requester", func(f *Facts) bool { return f.AcceptanceAuthorIsRequester }},
}

func boolFact(name string) (func(*Facts) bool, bool) {
	for _, b := range boolFacts {
		if b.name == name {
			return b.get, true
		}
	}
	return nil, false
}

func indexOf(set []string, v string) int {
	for i, s := range set {
		if s == v {
			return i
		}
	}
	return -1
}

func in(set []string, v string) bool { return indexOf(set, v) >= 0 }

// token reports 1..max bytes drawn from the given class of characters.
func token(s string, max int, ok func(byte) bool) bool {
	if len(s) < 1 || len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

func lower(c byte) bool { return c >= 'a' && c <= 'z' }
func digit(c byte) bool { return c >= '0' && c <= '9' }
func upper(c byte) bool { return c >= 'A' && c <= 'Z' }

func validID(s string) bool {
	return token(s, 160, func(c byte) bool { return lower(c) || digit(c) || c == '_' || c == '.' })
}

func validVersion(s string) bool {
	return token(s, 64, func(c byte) bool { return lower(c) || upper(c) || digit(c) || c == '.' || c == '+' || c == '-' })
}

func validReason(s string) bool {
	return token(s, 64, func(c byte) bool { return upper(c) || digit(c) || c == '_' }) && !digit(s[0]) && s[0] != '_'
}

func (f Facts) validate() error {
	if !in(classes, f.Storage.Class) {
		return invalid("storage.class")
	}
	if !in(volumes, f.Volume) {
		return invalid("volume")
	}
	if len(f.Storage.Reasons) > MaxEntries {
		return invalid("storage.reasons: more than %d entries", MaxEntries)
	}
	for i, r := range f.Storage.Reasons {
		if !validReason(r) {
			return invalid("storage.reasons[%d]", i)
		}
	}
	if len(f.Tools) > MaxEntries {
		return invalid("tools: more than %d entries", MaxEntries)
	}
	for i, t := range f.Tools {
		switch {
		case !validID(t.Ref):
			return invalid("tools[%d].ref", i)
		case !validVersion(t.Version):
			return invalid("tools[%d].version", i)
		case !in(toolEffects, t.SideEffect):
			return invalid("tools[%d].side_effect", i)
		case !in(idempotencies, t.Idempotency):
			return invalid("tools[%d].idempotency", i)
		case !in(toolChanges, t.Change):
			return invalid("tools[%d].change", i)
		}
	}
	if len(f.ToolDeclarations) > MaxEntries {
		return invalid("tool_declarations: more than %d entries", MaxEntries)
	}
	for i, d := range f.ToolDeclarations {
		switch {
		case !validID(d.ID):
			return invalid("tool_declarations[%d].id", i)
		case !in(declEffects, d.SideEffect):
			return invalid("tool_declarations[%d].side_effect", i)
		case !in(declChanges, d.Change):
			return invalid("tool_declarations[%d].change", i)
		}
	}
	return nil
}

// Validate checks every enum, token and bound. Order is not checked: Normalize
// sorts, and CheckCanonical reports unsorted input.
func (f Facts) Validate() error { return f.validate() }

// Normalize validates f and returns a deep copy in canonical form: tool facts
// sorted by (ref, version, change), declarations by (id, change), reasons
// ascending, and no nil slices. Duplicate tool facts (same ref, version and
// change), duplicate declaration ids and duplicate reasons are errors.
func Normalize(f Facts) (Facts, error) {
	if err := f.validate(); err != nil {
		return Facts{}, err
	}
	n := f
	n.Storage.Reasons = append([]string{}, f.Storage.Reasons...)
	n.Tools = append([]ToolFact{}, f.Tools...)
	n.ToolDeclarations = append([]ToolDeclFact{}, f.ToolDeclarations...)
	sort.Strings(n.Storage.Reasons)
	sort.Slice(n.Tools, func(i, j int) bool {
		a, b := n.Tools[i], n.Tools[j]
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Change < b.Change
	})
	sort.Slice(n.ToolDeclarations, func(i, j int) bool {
		a, b := n.ToolDeclarations[i], n.ToolDeclarations[j]
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Change < b.Change
	})
	for i := 1; i < len(n.Storage.Reasons); i++ {
		if n.Storage.Reasons[i] == n.Storage.Reasons[i-1] {
			return Facts{}, invalid("storage.reasons: duplicate")
		}
	}
	for i := 1; i < len(n.Tools); i++ {
		a, b := n.Tools[i-1], n.Tools[i]
		if a.Ref == b.Ref && a.Version == b.Version && a.Change == b.Change {
			return Facts{}, invalid("tools: duplicate fact")
		}
	}
	for i := 1; i < len(n.ToolDeclarations); i++ {
		if n.ToolDeclarations[i].ID == n.ToolDeclarations[i-1].ID {
			return Facts{}, invalid("tool_declarations: duplicate id")
		}
	}
	return n, nil
}

// canonicalize returns the normalized facts and their canonical JSON.
func canonicalize(f Facts) (Facts, []byte, error) {
	n, err := Normalize(f)
	if err != nil {
		return Facts{}, nil, err
	}
	raw, err := json.Marshal(n)
	if err != nil {
		return Facts{}, nil, invalid("encode")
	}
	return n, raw, nil
}

// Canonical returns the canonical JSON of f: the normalized facts encoded by
// encoding/json in declared field order, compact, with empty lists as [].
// Equal facts in any input order yield identical bytes.
func Canonical(f Facts) ([]byte, error) {
	_, raw, err := canonicalize(f)
	return raw, err
}

// FactsHash is the hex SHA-256 of Canonical(f).
func FactsHash(f Facts) (string, error) {
	raw, err := Canonical(f)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// CheckCanonical requires f to be valid and already in canonical form, that is,
// json.Marshal(f) equals Canonical(f). Stored facts must pass it so one logical
// fact set has one byte representation.
func CheckCanonical(f Facts) error {
	raw, err := Canonical(f)
	if err != nil {
		return err
	}
	got, err := json.Marshal(f)
	if err != nil || !bytes.Equal(got, raw) {
		return ErrNotCanonical
	}
	return nil
}
