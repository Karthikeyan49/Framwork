package risk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// OutcomeBlocked is the rule outcome that blocks a release without raising its
// level. All other outcomes are level names.
const OutcomeBlocked = "BLOCKED"

// Separation-of-duties roles that a policy may exclude from approving.
const (
	RoleRequester        = "requester"
	RoleAcceptanceAuthor = "acceptance_author"
	RoleSandboxFreezer   = "sandbox_freezer"
)

const (
	flagGainsDataWrite  = "gains_data_write"
	flagWeakensApproval = "weakens_approval"
	maxRules            = 128
	maxApprovals        = 8
	reasonAddOptional   = "ADD_OPTIONAL"
)

var (
	roles     = []string{RoleRequester, RoleAcceptanceAuthor, RoleSandboxFreezer}
	rollbacks = []string{RollbackPointerSafe, RollbackDataNotReversible, RollbackExternalEffects}
)

// Spec is the complete definition of one policy version and exactly what
// PolicyHash covers: the hash is sha256 over its compact JSON in the field order
// below (no maps, so the bytes are deterministic). Changing any entry means
// publishing a new version, never editing a published one.
type Spec struct {
	Version    string           `json:"version"`
	Rules      []Rule           `json:"rules"`
	Matrix     []Cell           `json:"matrix"`
	Thresholds Thresholds       `json:"thresholds"`
	Separation []SeparationRule `json:"separation"`
	Rollback   RollbackSpec     `json:"rollback"`
}

// Rule is one row of the rule table. Level is LOW, MEDIUM, HIGH or CRITICAL
// (raises the level to at least that) or BLOCKED (blocks the release).
type Rule struct {
	ID      string  `json:"id"`
	Trigger Trigger `json:"trigger"`
	Level   string  `json:"level"`
}

// Trigger is a closed declarative predicate over Facts; exactly one of Any,
// Tool, Decl and Storage is set. Any fires when at least one of the named
// boolean facts is true; Tool, Decl and Storage fire when at least one fact
// matches. MinLevel (blocking rules only) additionally requires the level of
// all unconditional rules to reach it.
type Trigger struct {
	Any      []string      `json:"any,omitempty"`
	Tool     *ToolMatch    `json:"tool,omitempty"`
	Decl     *DeclMatch    `json:"decl,omitempty"`
	Storage  *StorageMatch `json:"storage,omitempty"`
	MinLevel string        `json:"min_level,omitempty"`
}

// ToolMatch matches a ToolFact whose side effect, idempotency and change are
// each in the corresponding (non-empty) list; Unresolved additionally requires
// the fact to be unresolved.
type ToolMatch struct {
	SideEffect  []string `json:"side_effect"`
	Idempotency []string `json:"idempotency"`
	Change      []string `json:"change"`
	Unresolved  bool     `json:"unresolved,omitempty"`
}

// DeclMatch matches a ToolDeclFact by side effect and change; Flag, when set,
// requires that boolean to be true (gains_data_write or weakens_approval).
type DeclMatch struct {
	SideEffect []string `json:"side_effect"`
	Change     []string `json:"change"`
	Flag       string   `json:"flag,omitempty"`
}

// StorageMatch matches a storage class in Class and, when Reason is set, a
// storage reason code.
type StorageMatch struct {
	Class  []string `json:"class"`
	Reason string   `json:"reason,omitempty"`
}

// Cell is one entry of the storage x volume matrix. A cell with an ID is
// reported in Assessment.Reasons when it applies; cells without one are silent.
type Cell struct {
	ID     string `json:"id,omitempty"`
	Class  string `json:"class"`
	Volume string `json:"volume"`
	Level  string `json:"level"`
}

// Thresholds are the inclusive record-count upper bounds of the SMALL and LARGE
// buckets; larger counts are UNVERIFIED (see Policy.VolumeBucket).
type Thresholds struct {
	SmallMax int64 `json:"small_max"`
	LargeMax int64 `json:"large_max"`
}

// SeparationRule fixes the approval requirement of one level.
type SeparationRule struct {
	Level     string   `json:"level"`
	Approvals int      `json:"approvals"`
	Excluded  []string `json:"excluded"`
}

// RollbackSpec derives the rollback class: the class of the most restrictive
// fired rule by position in Order, or Order[0] when none fires.
type RollbackSpec struct {
	Order []string       `json:"order"`
	Rules []RollbackRule `json:"rules"`
}

// RollbackRule yields Class when Trigger fires.
type RollbackRule struct {
	Class   string  `json:"class"`
	Trigger Trigger `json:"trigger"`
}

func cloneStrings(s []string) []string { return append([]string{}, s...) }

func (t Trigger) clone() Trigger {
	c := t
	c.Any = cloneStrings(t.Any)
	if t.Tool != nil {
		m := *t.Tool
		m.SideEffect, m.Idempotency, m.Change = cloneStrings(m.SideEffect), cloneStrings(m.Idempotency), cloneStrings(m.Change)
		c.Tool = &m
	}
	if t.Decl != nil {
		m := *t.Decl
		m.SideEffect, m.Change = cloneStrings(m.SideEffect), cloneStrings(m.Change)
		c.Decl = &m
	}
	if t.Storage != nil {
		m := *t.Storage
		m.Class = cloneStrings(m.Class)
		c.Storage = &m
	}
	return c
}

// clone deep-copies s with no nil slices, so equal specs encode identically.
func (s Spec) clone() Spec {
	c := s
	c.Rules = make([]Rule, len(s.Rules))
	for i, r := range s.Rules {
		c.Rules[i] = Rule{ID: r.ID, Trigger: r.Trigger.clone(), Level: r.Level}
	}
	c.Matrix = append([]Cell{}, s.Matrix...)
	c.Separation = make([]SeparationRule, len(s.Separation))
	for i, x := range s.Separation {
		c.Separation[i] = SeparationRule{Level: x.Level, Approvals: x.Approvals, Excluded: cloneStrings(x.Excluded)}
	}
	c.Rollback = RollbackSpec{Order: cloneStrings(s.Rollback.Order), Rules: make([]RollbackRule, len(s.Rollback.Rules))}
	for i, r := range s.Rollback.Rules {
		c.Rollback.Rules[i] = RollbackRule{Class: r.Class, Trigger: r.Trigger.clone()}
	}
	return c
}

// Document returns the canonical bytes PolicyHash is computed over.
func (s Spec) Document() []byte {
	raw, _ := json.Marshal(s.clone())
	return raw
}

// Hash is the hex SHA-256 of Document.
func (s Spec) Hash() string {
	sum := sha256.Sum256(s.Document())
	return hex.EncodeToString(sum[:])
}

type outcome struct {
	level   Level
	blocked bool
}

func parseOutcome(s string) (outcome, bool) {
	if s == OutcomeBlocked {
		return outcome{blocked: true}, true
	}
	l, err := ParseLevel(s)
	return outcome{level: l}, err == nil
}

// rank orders outcomes for monotonicity checks: BLOCKED is above CRITICAL.
func (o outcome) rank() int {
	if o.blocked {
		return int(Critical) + 1
	}
	return int(o.level)
}

type crule struct {
	id  string
	t   Trigger
	out outcome
	min Level
}

type ccell struct {
	id  string
	out outcome
}

type crollback struct {
	class string
	rank  int
	t     Trigger
}

// Policy is an immutable, validated policy version. Build one with NewPolicy;
// the zero value is invalid. A Policy is safe for concurrent use.
type Policy struct {
	spec  Spec
	hash  string
	doc   []byte
	rules []crule
	cells [4][4]ccell
	sep   [int(Critical) + 1]Separation
	rb    []crollback
}

// NewPolicy validates s and returns an immutable Policy. It rejects malformed
// tables and any policy that could lower a level when a fact is added: the
// matrix must be non-decreasing in class and volume and the separation
// requirement must not weaken as the level rises.
func NewPolicy(s Spec) (Policy, error) {
	s = s.clone()
	p := Policy{spec: s}
	bad := func(format string, args ...any) (Policy, error) {
		return Policy{}, fmt.Errorf("%w: %s", ErrInvalidPolicy, fmt.Sprintf(format, args...))
	}
	if !token(s.Version, 64, idChar) {
		return bad("version")
	}
	if len(s.Rules) == 0 || len(s.Rules) > maxRules {
		return bad("rules: count")
	}
	ids := map[string]bool{}
	for i, r := range s.Rules {
		o, ok := parseOutcome(r.Level)
		if !ok || !token(r.ID, 64, idChar) || ids[r.ID] {
			return bad("rules[%d]", i)
		}
		ids[r.ID] = true
		if err := r.Trigger.validate(); err != nil {
			return bad("rules[%d].trigger: %v", i, err)
		}
		c := crule{id: r.ID, t: r.Trigger, out: o}
		if r.Trigger.MinLevel != "" {
			min, err := ParseLevel(r.Trigger.MinLevel)
			if err != nil || !o.blocked {
				return bad("rules[%d]: only blocking rules may have a min_level", i)
			}
			c.min = min
		}
		p.rules = append(p.rules, c)
	}
	if len(s.Matrix) != len(classes)*len(volumes) {
		return bad("matrix: need %d cells", len(classes)*len(volumes))
	}
	var seen [4][4]bool
	var rank [4][4]int
	for i, c := range s.Matrix {
		o, ok := parseOutcome(c.Level)
		ci, vi := indexOf(classes, c.Class), indexOf(volumes, c.Volume)
		if !ok || ci < 0 || vi < 0 || seen[ci][vi] {
			return bad("matrix[%d]", i)
		}
		if c.ID != "" {
			if !token(c.ID, 64, idChar) || ids[c.ID] {
				return bad("matrix[%d].id", i)
			}
			ids[c.ID] = true
		}
		seen[ci][vi], rank[ci][vi] = true, o.rank()
		p.cells[ci][vi] = ccell{id: c.ID, out: o}
	}
	for ci := range classes {
		for vi := range volumes {
			if ci > 0 && rank[ci][vi] < rank[ci-1][vi] || vi > 0 && rank[ci][vi] < rank[ci][vi-1] {
				return bad("matrix: not monotone at %s/%s", classes[ci], volumes[vi])
			}
		}
	}
	if s.Thresholds.SmallMax < 1 || s.Thresholds.LargeMax <= s.Thresholds.SmallMax {
		return bad("thresholds")
	}
	if len(s.Separation) != int(Critical) {
		return bad("separation: need one rule per level")
	}
	for i, x := range s.Separation {
		l, err := ParseLevel(x.Level)
		if err != nil || l != Level(i+1) || x.Approvals < 1 || x.Approvals > maxApprovals {
			return bad("separation[%d]", i)
		}
		last := -1
		for _, role := range x.Excluded {
			// Roles appear in declaration order without repeats.
			k := indexOf(roles, role)
			if k <= last {
				return bad("separation[%d].excluded", i)
			}
			last = k
		}
		if i > 0 {
			prev := s.Separation[i-1]
			if x.Approvals < prev.Approvals || !subset(prev.Excluded, x.Excluded) {
				return bad("separation: weaker at %s", x.Level)
			}
		}
		p.sep[l] = Separation{Approvals: x.Approvals, Excluded: cloneStrings(x.Excluded)}
	}
	if len(s.Rollback.Order) != len(rollbacks) {
		return bad("rollback.order")
	}
	for i, c := range s.Rollback.Order {
		if c != rollbacks[i] {
			return bad("rollback.order")
		}
	}
	if len(s.Rollback.Rules) > maxRules {
		return bad("rollback.rules: count")
	}
	for i, r := range s.Rollback.Rules {
		k := indexOf(s.Rollback.Order, r.Class)
		if k < 1 || r.Trigger.MinLevel != "" {
			return bad("rollback.rules[%d]", i)
		}
		if err := r.Trigger.validate(); err != nil {
			return bad("rollback.rules[%d].trigger: %v", i, err)
		}
		p.rb = append(p.rb, crollback{class: r.Class, rank: k, t: r.Trigger})
	}
	p.doc = s.Document()
	sum := sha256.Sum256(p.doc)
	p.hash = hex.EncodeToString(sum[:])
	return p, nil
}

func idChar(c byte) bool { return lower(c) || upper(c) || digit(c) || c == '.' || c == '_' || c == '-' }

func subset(a, b []string) bool {
	for _, x := range a {
		if !in(b, x) {
			return false
		}
	}
	return true
}

// listOf reports a non-empty list of distinct members of set.
func listOf(list, set []string) bool {
	if len(list) == 0 {
		return false
	}
	for i, v := range list {
		if !in(set, v) || indexOf(list, v) != i {
			return false
		}
	}
	return true
}

func (t Trigger) validate() error {
	n := 0
	if len(t.Any) > 0 {
		n++
		for i, name := range t.Any {
			if _, ok := boolFact(name); !ok || indexOf(t.Any, name) != i {
				return fmt.Errorf("any[%d]", i)
			}
		}
	}
	if m := t.Tool; m != nil {
		n++
		if !listOf(m.SideEffect, toolEffects) || !listOf(m.Idempotency, idempotencies) || !listOf(m.Change, toolChanges) {
			return fmt.Errorf("tool")
		}
	}
	if m := t.Decl; m != nil {
		n++
		if !listOf(m.SideEffect, declEffects) || !listOf(m.Change, declChanges) || m.Flag != "" && m.Flag != flagGainsDataWrite && m.Flag != flagWeakensApproval {
			return fmt.Errorf("decl")
		}
	}
	if m := t.Storage; m != nil {
		n++
		if !listOf(m.Class, classes) || m.Reason != "" && !validReason(m.Reason) {
			return fmt.Errorf("storage")
		}
	}
	if n != 1 {
		return fmt.Errorf("exactly one of any, tool, decl, storage")
	}
	return nil
}

// Version is the policy version string.
func (p Policy) Version() string { return p.spec.Version }

// Hash is PolicyHash: sha256 over the policy document.
func (p Policy) Hash() string { return p.hash }

// Document returns the canonical bytes PolicyHash covers.
func (p Policy) Document() []byte { return append([]byte(nil), p.doc...) }

// Spec returns a deep copy of the definition, for inspection or to derive
// another version; modifying it never affects p.
func (p Policy) Spec() Spec { return p.spec.clone() }

// VolumeBucket maps a measured record count to its bucket under p's thresholds.
// An unknown or negative count and any count above LargeMax are UNVERIFIED: the
// platform has verified neither a backfill envelope beyond that size nor an
// unbounded or capped count, so uncertainty must rate at least as high as the
// largest measured bucket.
func (p Policy) VolumeBucket(count int64, known bool) string {
	switch {
	case !known || count < 0:
		return VolumeUnverified
	case count == 0:
		return VolumeNone
	case count <= p.spec.Thresholds.SmallMax:
		return VolumeSmall
	case count <= p.spec.Thresholds.LargeMax:
		return VolumeLarge
	}
	return VolumeUnverified
}
