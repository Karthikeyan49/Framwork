// Package risk maps server-derived release facts to a risk verdict under a
// versioned, hashed policy (release-risk-v1, ADR038 section 3).
//
// The package is pure: it reads no clock, randomness, database, tool catalog or
// compiler state and imports only the standard library. Callers reduce trusted
// artifacts (restored base program, trusted-compiler target, evolution plan,
// operator tool catalog, measured record count) to Facts; a request never
// supplies one. Level is the monotone maximum over the fired rules, there are no
// downgrade rules, and BLOCKED is a separate verdict that always wins for gating.
package risk

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidFacts rejects facts outside the closed vocabulary or bounds.
	ErrInvalidFacts = errors.New("risk: invalid facts")
	// ErrNotCanonical rejects facts that are valid but not in canonical form.
	ErrNotCanonical = errors.New("risk: facts are not canonical")
	// ErrUnknownPolicy fails closed for a policy version that is not registered.
	ErrUnknownPolicy = errors.New("risk: unknown policy version")
	// ErrInvalidPolicy rejects a malformed or non-monotone policy definition.
	ErrInvalidPolicy = errors.New("risk: invalid policy")
	// ErrMismatch reports an assessment that differs from re-evaluation.
	ErrMismatch = errors.New("risk: assessment does not match its facts")
	// ErrSeparation reports a violated separation-of-duties requirement.
	ErrSeparation = errors.New("risk: separation of duties violated")
	// ErrLevel rejects a level name or value outside LOW..CRITICAL.
	ErrLevel = errors.New("risk: invalid level")
)

// PolicyV1 names the first policy version.
const PolicyV1 = "release-risk-v1"

// MaxEntries bounds Tools, ToolDeclarations and Storage.Reasons.
const MaxEntries = 256

// Level is the ordered risk level LOW < MEDIUM < HIGH < CRITICAL. The zero value
// is invalid and cannot be serialized, so an unset level is never read as LOW.
type Level uint8

const (
	Low Level = iota + 1
	Medium
	High
	Critical
)

var levelNames = [...]string{Low: "LOW", Medium: "MEDIUM", High: "HIGH", Critical: "CRITICAL"}

func (l Level) Valid() bool { return l >= Low && l <= Critical }

func (l Level) String() string {
	if l.Valid() {
		return levelNames[l]
	}
	return "INVALID"
}

// ParseLevel is the strict inverse of String.
func ParseLevel(s string) (Level, error) {
	for l := Low; l <= Critical; l++ {
		if levelNames[l] == s {
			return l, nil
		}
	}
	return 0, ErrLevel
}

func (l Level) MarshalText() ([]byte, error) {
	if !l.Valid() {
		return nil, ErrLevel
	}
	return []byte(levelNames[l]), nil
}

func (l *Level) UnmarshalText(b []byte) error {
	v, err := ParseLevel(string(b))
	if err != nil {
		return err
	}
	*l = v
	return nil
}

// Storage evolution classes (StorageFact.Class), ascending severity.
const (
	ClassEqual     = "EQUAL"
	ClassAdditive  = "ADDITIVE"
	ClassTransform = "TRANSFORM"
	ClassBreaking  = "BREAKING"
)

// Record-volume buckets (Facts.Volume), ascending size. UNVERIFIED means the
// count is unknown or beyond what the platform has verified; it is the top
// bucket so unknown volume can never rate lower than a measured one.
const (
	VolumeNone       = "NONE"
	VolumeSmall      = "SMALL"
	VolumeLarge      = "LARGE"
	VolumeUnverified = "UNVERIFIED"
)

// Tool catalog side effects (ToolFact.SideEffect). The M10 catalog admits only
// NONE, READ and WRITE; FINANCIAL and DESTRUCTIVE are the reserved classes of
// tools.Delivery and rate CRITICAL.
const (
	SideNone        = "NONE"
	SideRead        = "READ"
	SideWrite       = "WRITE"
	SideFinancial   = "FINANCIAL"
	SideDestructive = "DESTRUCTIVE"
)

// Tool delivery idempotency modes (ToolFact.Idempotency).
const (
	IdemNativeKey         = "NATIVE_KEY"
	IdemLookupByReference = "LOOKUP_BY_REFERENCE"
	IdemDedupeWindow      = "DEDUPE_WINDOW"
	IdemNone              = "NONE"
)

// Change kinds. ToolFact uses ADDED, REMOVED, REPINNED and INHERITED;
// ToolDeclFact uses ADDED, REMOVED and CHANGED.
const (
	ChangeAdded     = "ADDED"
	ChangeRemoved   = "REMOVED"
	ChangeRepinned  = "REPINNED"
	ChangeInherited = "INHERITED"
	ChangeChanged   = "CHANGED"
)

// Authored tool declaration side effects (ToolDeclFact.SideEffect).
const (
	DeclNone        = "none"
	DeclRead        = "read"
	DeclWrite       = "write"
	DeclDestructive = "destructive"
	DeclFinancial   = "financial"
)

// Rollback classes (Assessment.Rollback), ascending restriction.
const (
	RollbackPointerSafe       = "POINTER_SAFE"
	RollbackDataNotReversible = "DATA_NOT_REVERSIBLE"
	RollbackExternalEffects   = "EXTERNAL_EFFECTS"
)

// Facts is the complete, closed input of release-risk-v1. Every field is derived
// on the server; the type is frozen with the policy version, so a new fact needs
// a new policy version and a new type. Facts are bounded: the lists hold at most
// MaxEntries entries and every enum is validated, never defaulted.
type Facts struct {
	// FirstRelease: no release is active for the application (no base).
	FirstRelease bool `json:"first_release"`
	// IRUpgrade: base and target IR or compiler version differ.
	IRUpgrade bool `json:"ir_upgrade"`
	// PageChanged: a page definition was added, removed or changed.
	PageChanged bool `json:"page_changed"`
	// LabelChanged: a definition changed in its label.
	LabelChanged bool `json:"label_changed"`
	// EntityAdded: an entity definition was added.
	EntityAdded bool `json:"entity_added"`
	// RelationshipChanged: a relationship was added, removed or changed.
	RelationshipChanged bool `json:"relationship_changed"`
	// RuleChanged: a non-financial rule was added, changed or removed.
	RuleChanged bool `json:"rule_changed"`
	// FinancialRuleChanged: a financial (money-typed) rule was added, changed or removed.
	FinancialRuleChanged bool `json:"financial_rule_changed"`
	// WorkflowChanged: a workflow was added or changed.
	WorkflowChanged bool `json:"workflow_changed"`
	// TriggerWidened: a workflow trigger widened from manual to record events.
	TriggerWidened bool `json:"trigger_widened"`
	// ApprovalGateChanged: an approval gate changed, or a gating condition reads money.
	ApprovalGateChanged bool `json:"approval_gate_changed"`
	// PlanOnlyChanged: a compiled plan changed while its definition is identical.
	PlanOnlyChanged bool `json:"plan_only_changed"`
	// Unclassified: a change the diff could not classify.
	Unclassified bool `json:"unclassified"`
	// UnknownCapability: the target needs a capability the platform does not know.
	UnknownCapability bool `json:"unknown_capability"`
	// AcceptanceAuthorIsRequester: the acceptance spec author is the requester.
	AcceptanceAuthorIsRequester bool `json:"acceptance_author_is_requester"`
	// Storage is the data-layout evolution class between base and target.
	Storage StorageFact `json:"storage"`
	// Volume is the server-measured record-volume bucket (see VolumeBucket).
	Volume string `json:"volume"`
	// Tools lists governed tool activities touched by the release.
	Tools []ToolFact `json:"tools"`
	// ToolDeclarations lists authored tool declarations touched by the release.
	ToolDeclarations []ToolDeclFact `json:"tool_declarations"`
}

// StorageFact carries the storage classification. Reasons are machine-readable
// codes (upper-case letters, digits, underscore; at most 64 bytes) in ascending
// order without duplicates once normalized. The policy interprets ADD_OPTIONAL
// only (rule S3); the vocabulary is otherwise open.
type StorageFact struct {
	Class   string   `json:"class"`
	Reasons []string `json:"reasons"`
}

// ToolFact is one governed tool pin affected by the release: one fact per
// (Ref, Version, Change). INHERITED means the activity itself is unchanged but
// its workflow changed, so the change inherits the tool's level (S9b). For
// REPINNED, SideEffect and Idempotency describe the more severe of the old and
// new definitions. Unresolved: the pin is not in the operator catalog or the
// tenant is not allowed to use it; that blocks the release.
type ToolFact struct {
	Ref         string `json:"ref"`
	Version     string `json:"version"`
	SideEffect  string `json:"side_effect"`
	Idempotency string `json:"idempotency"`
	Change      string `json:"change"`
	Unresolved  bool   `json:"unresolved"`
}

// ToolDeclFact is one authored `kind:"tool"` definition affected by the release,
// one fact per ID. For CHANGED, SideEffect is the more severe of the old and new
// declarations. GainsDataWrite: the declaration gained the data.write
// permission. WeakensApproval: its approval requirement was weakened.
type ToolDeclFact struct {
	ID              string `json:"id"`
	SideEffect      string `json:"side_effect"`
	Change          string `json:"change"`
	GainsDataWrite  bool   `json:"gains_data_write"`
	WeakensApproval bool   `json:"weakens_approval"`
}

// Assessment is the verdict of one policy version over one set of facts. Level
// is the maximum over fired level rules and is carried even when Blocked;
// Blocked wins for gating. Reasons are the sorted ids of every fired row.
type Assessment struct {
	PolicyVersion string     `json:"policy_version"`
	PolicyHash    string     `json:"policy_hash"`
	Level         Level      `json:"level"`
	Blocked       bool       `json:"blocked"`
	Reasons       []string   `json:"reasons"`
	FactsHash     string     `json:"facts_hash"`
	Separation    Separation `json:"separation"`
	Rollback      string     `json:"rollback"`
}

// HighRisk reports Level >= HIGH. It is the value bundles store as HighRisk.
func (a Assessment) HighRisk() bool { return a.Level >= High }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidFacts, fmt.Sprintf(format, args...))
}
