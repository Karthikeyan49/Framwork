package intent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/erp"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Requirement sets and acceptance specs (ADR038 section 4, appendix E). They are
// typed, strictly parsed, canonical, hash-addressed artifacts. Nothing here reads
// an IR, a compiled plan or a source revision: the requirement owner and the
// independent acceptance author state business intent, and only the verifier
// compares it with the implementation.
const (
	RequirementSetVersion    = "requirements-v2"
	AcceptanceSpecVersion    = "acceptance-v2"
	VocabularyVersion        = "acceptance-vocabulary-v1"
	RequirementPolicyVersion = "requirement-policy-v1"
)

// Size bounds.
const (
	MaxRequirementBytes = 256 << 10
	MaxRequirements     = 100
	MaxAcceptanceBytes  = 1 << 20
	MaxAcceptanceCases  = 1000
	MaxContextEntries   = 32
	MaxScalarBytes      = 256
	maxUnknownSemantics = 4096
	maxRoles            = 4
)

// Requirement categories the verifier understands. Any other well-formed
// category is recorded but can never satisfy a mandatory requirement.
const (
	CategoryApprovalThreshold = "approval_threshold"
	CategoryApprovalGate      = "approval_gate"
	CategoryToolEffect        = "tool_effect"
	CategoryDataMigration     = "data_migration"
)

// Requiredness, basis, status and provenance vocabularies.
const (
	Mandatory   = "MANDATORY"
	Recommended = "RECOMMENDED"
	Optional    = "OPTIONAL"

	BasisPolicyFloor   = "POLICY_FLOOR"
	BasisOwnerDecision = "OWNER_DECISION"

	StatusResolved     = "RESOLVED"
	StatusOpenQuestion = "OPEN_QUESTION"
	StatusRejected     = "REJECTED"

	ProvenanceHuman       = "AUTHENTICATED_HUMAN"
	ProvenanceAIConfirmed = "AI_PROPOSED_HUMAN_CONFIRMED"
)

// Violation is a stable, machine-readable rejection.
type Violation struct{ Code, Path, Detail string }

func (v *Violation) Error() string {
	if v.Path == "" {
		return v.Code + ": " + v.Detail
	}
	return v.Code + " at " + v.Path + ": " + v.Detail
}

// Violation codes.
const (
	CodeInvalidJSON      = "INVALID_JSON"
	CodeInvalidSchema    = "INVALID_SCHEMA"
	CodeNotCanonical     = "NOT_CANONICAL"
	CodeTooLarge         = "TOO_LARGE"
	CodeInvalidSemantics = "INVALID_SEMANTICS"
	CodeFloorViolation   = "FLOOR_VIOLATION"
	CodeOracleDisagrees  = "ORACLE_DISAGREES"
	CodeUnknownReq       = "UNKNOWN_REQUIREMENT"
	CodeMissingCoverage  = "MISSING_COVERAGE"
	CodeInvalidCase      = "INVALID_CASE"
	CodeHashMismatch     = "HASH_MISMATCH"
)

func violation(code, path, format string, args ...any) *Violation {
	return &Violation{code, path, fmt.Sprintf(format, args...)}
}

var (
	requirementID = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)
	categoryName  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	appID         = regexp.MustCompile(`^app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	workflowID    = regexp.MustCompile(`^workflow\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	entityID      = regexp.MustCompile(`^entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	fieldID       = regexp.MustCompile(`^field\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	toolRef       = regexp.MustCompile(`^tool\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)
	hashPattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var (
	comparisons  = []string{"GT", "GTE", "LT", "LTE", "EQ", "NEQ"}
	approverSet  = []string{"admin", "member", "owner", "viewer"}
	sideEffects  = []string{"NONE", "READ", "WRITE"}
	sandboxModes = []string{"BLOCK", "SIMULATE", "READ_ONLY", "REROUTE", "TEST_ACCOUNT"}
	classBounds  = []string{"COMPATIBLE", "TRANSFORM"}
)

func oneOf(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// SandboxRank orders the tool modes by how much they may touch.
func SandboxRank(mode string) int {
	for i, m := range sandboxModes {
		if m == mode {
			return i
		}
	}
	return -1
}

// SideEffectRank orders NONE < READ < WRITE.
func SideEffectRank(effect string) int {
	for i, e := range sideEffects {
		if e == effect {
			return i
		}
	}
	return -1
}

// ApprovalThreshold: records of Entity whose money Field compares to Amount under
// Op must be approved by exactly the ApproverRoles in Workflow. Context fixes the
// values of other fields that the workflow's conditions read.
type ApprovalThreshold struct {
	WorkflowID    string                     `json:"workflow_id"`
	EntityID      string                     `json:"entity_id"`
	FieldID       string                     `json:"field_id"`
	Currency      string                     `json:"currency"`
	Op            string                     `json:"op"`
	Amount        string                     `json:"amount"`
	ApproverRoles []string                   `json:"approver_roles"`
	Context       map[string]json.RawMessage `json:"context,omitempty"`
}

// ApprovalGate: no path of Workflow reaches completion without an approval state
// whose roles are exactly ApproverRoles.
type ApprovalGate struct {
	WorkflowID    string   `json:"workflow_id"`
	ApproverRoles []string `json:"approver_roles"`
}

// ToolEffect: every pinned use of ToolRef stays within the stated side effect,
// approval and sandbox-mode bounds.
type ToolEffect struct {
	ToolRef          string `json:"tool_ref"`
	MaxSideEffect    string `json:"max_side_effect"`
	RequiresApproval bool   `json:"requires_approval"`
	MaxSandboxMode   string `json:"max_sandbox_mode"`
}

// DataMigration: the storage evolution between releases stays within MaxClass.
type DataMigration struct {
	MaxClass string `json:"max_class"`
}

// RequirementV2 is one typed business requirement.
type RequirementV2 struct {
	ID                string          `json:"id"`
	Category          string          `json:"category"`
	Requiredness      string          `json:"requiredness"`
	RequirednessBasis string          `json:"requiredness_basis"`
	Status            string          `json:"status"`
	Provenance        string          `json:"provenance"`
	Semantics         json.RawMessage `json:"semantics"`
}

// Known reports whether the verifier understands the requirement's category.
func (r RequirementV2) Known() bool {
	switch r.Category {
	case CategoryApprovalThreshold, CategoryApprovalGate, CategoryToolEffect, CategoryDataMigration:
		return true
	}
	return false
}

func strictDecode(raw []byte, into any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil || d.More() {
		return fmt.Errorf("semantics do not match their category")
	}
	return nil
}

// Threshold decodes approval_threshold semantics.
func (r RequirementV2) Threshold() (ApprovalThreshold, error) {
	var t ApprovalThreshold
	if r.Category != CategoryApprovalThreshold {
		return t, fmt.Errorf("not an approval_threshold requirement")
	}
	return t, strictDecode(r.Semantics, &t)
}

// Gate decodes approval_gate semantics.
func (r RequirementV2) Gate() (ApprovalGate, error) {
	var g ApprovalGate
	if r.Category != CategoryApprovalGate {
		return g, fmt.Errorf("not an approval_gate requirement")
	}
	return g, strictDecode(r.Semantics, &g)
}

// Tool decodes tool_effect semantics.
func (r RequirementV2) Tool() (ToolEffect, error) {
	var t ToolEffect
	if r.Category != CategoryToolEffect {
		return t, fmt.Errorf("not a tool_effect requirement")
	}
	return t, strictDecode(r.Semantics, &t)
}

// Migration decodes data_migration semantics.
func (r RequirementV2) Migration() (DataMigration, error) {
	var m DataMigration
	if r.Category != CategoryDataMigration {
		return m, fmt.Errorf("not a data_migration requirement")
	}
	return m, strictDecode(r.Semantics, &m)
}

// RequirementSet is the typed requirement document of one application.
type RequirementSet struct {
	SchemaVersion string          `json:"schema_version"`
	AppID         string          `json:"app_id"`
	Requirements  []RequirementV2 `json:"requirements"`
}

func validRoles(roles []string) bool {
	if len(roles) < 1 || len(roles) > maxRoles {
		return false
	}
	for i, r := range roles {
		if !oneOf(approverSet, r) || i > 0 && roles[i-1] >= r {
			return false
		}
	}
	return true
}

// ValidScalar reports a bounded JSON scalar usable as a field value: a string,
// number or boolean, or a money object {"amount","currency"}.
func ValidScalar(raw json.RawMessage) bool {
	_, ok := canonicalScalar(raw)
	return ok
}

func canonicalScalar(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > MaxScalarBytes {
		return nil, false
	}
	v, err := metadata.DecodeJSON(raw)
	if err != nil {
		return nil, false
	}
	switch x := v.(type) {
	case string, bool:
	case json.Number:
		if len(x.String()) > 64 {
			return nil, false
		}
	case map[string]any:
		amount, ok1 := x["amount"].(string)
		currency, ok2 := x["currency"].(string)
		if len(x) != 2 || !ok1 || !ok2 || (erp.Money{Amount: amount, Currency: erp.Currency(currency)}).Validate() != nil {
			return nil, false
		}
	default:
		return nil, false
	}
	out, err := json.Marshal(v)
	if err != nil {
		return nil, false
	}
	return out, true
}

// canonicalSemantics validates the typed semantics of a known category and
// returns its canonical encoding; an unknown category keeps a bounded canonical
// JSON object.
func canonicalSemantics(r RequirementV2, path string) (json.RawMessage, *Violation) {
	bad := func(format string, args ...any) (json.RawMessage, *Violation) {
		return nil, violation(CodeInvalidSemantics, path, format, args...)
	}
	switch r.Category {
	case CategoryApprovalThreshold:
		t, err := r.Threshold()
		if err != nil {
			return bad("%v", err)
		}
		minor, err := erp.Currency(t.Currency).MinorUnits()
		if err != nil || !workflowID.MatchString(t.WorkflowID) || len(t.WorkflowID) > 160 || !entityID.MatchString(t.EntityID) || len(t.EntityID) > 160 || !fieldID.MatchString(t.FieldID) || len(t.FieldID) > 160 || !oneOf(comparisons, t.Op) || !validRoles(t.ApproverRoles) {
			return bad("approval_threshold identifiers, currency, operator or roles are invalid")
		}
		if (erp.Money{Amount: t.Amount, Currency: erp.Currency(t.Currency)}).Validate() != nil || fractionDigits(t.Amount) > minor {
			return bad("amount must be a valid %s amount with at most %d fractional digits", t.Currency, minor)
		}
		if len(t.Context) > MaxContextEntries {
			return bad("context has too many entries")
		}
		for id, value := range t.Context {
			canonical, ok := canonicalScalar(value)
			if !fieldID.MatchString(id) || len(id) > 160 || id == t.FieldID || !ok {
				return bad("context entry %q is invalid", id)
			}
			t.Context[id] = canonical
		}
		raw, _ := json.Marshal(t)
		return raw, nil
	case CategoryApprovalGate:
		g, err := r.Gate()
		if err != nil {
			return bad("%v", err)
		}
		if !workflowID.MatchString(g.WorkflowID) || len(g.WorkflowID) > 160 || !validRoles(g.ApproverRoles) {
			return bad("approval_gate workflow or roles are invalid")
		}
		raw, _ := json.Marshal(g)
		return raw, nil
	case CategoryToolEffect:
		t, err := r.Tool()
		if err != nil {
			return bad("%v", err)
		}
		if !toolRef.MatchString(t.ToolRef) || len(t.ToolRef) > 160 || !oneOf(sideEffects, t.MaxSideEffect) || !oneOf(sandboxModes, t.MaxSandboxMode) {
			return bad("tool_effect reference or bounds are invalid")
		}
		raw, _ := json.Marshal(t)
		return raw, nil
	case CategoryDataMigration:
		m, err := r.Migration()
		if err != nil {
			return bad("%v", err)
		}
		if !oneOf(classBounds, m.MaxClass) {
			return bad("data_migration bound is invalid")
		}
		raw, _ := json.Marshal(m)
		return raw, nil
	}
	if len(r.Semantics) > maxUnknownSemantics {
		return bad("semantics of an unrecognised category are too large")
	}
	v, err := metadata.DecodeJSON(r.Semantics)
	if _, isObject := v.(map[string]any); err != nil || !isObject {
		return bad("semantics must be a JSON object")
	}
	raw, _ := json.Marshal(v)
	return raw, nil
}

func fractionDigits(amount string) int {
	for i := 0; i < len(amount); i++ {
		if amount[i] == '.' {
			return len(amount) - i - 1
		}
	}
	return 0
}

// canonicalized validates the set and returns a copy with canonical semantics.
func (s RequirementSet) canonicalized() (RequirementSet, *Violation) {
	if s.SchemaVersion != RequirementSetVersion {
		return s, violation(CodeInvalidSchema, "/schema_version", "unsupported requirement set version")
	}
	if !appID.MatchString(s.AppID) || len(s.AppID) > 160 {
		return s, violation(CodeInvalidSchema, "/app_id", "invalid application id")
	}
	if len(s.Requirements) > MaxRequirements {
		return s, violation(CodeTooLarge, "/requirements", "more than %d requirements", MaxRequirements)
	}
	out := s
	out.Requirements = make([]RequirementV2, len(s.Requirements))
	seen := map[string]bool{}
	for i, r := range s.Requirements {
		path := fmt.Sprintf("/requirements/%d", i)
		switch {
		case !requirementID.MatchString(r.ID) || seen[r.ID]:
			return s, violation(CodeInvalidSchema, path+"/id", "invalid or duplicate requirement id")
		case !categoryName.MatchString(r.Category):
			return s, violation(CodeInvalidSchema, path+"/category", "invalid category")
		case !oneOf([]string{Mandatory, Recommended, Optional}, r.Requiredness):
			return s, violation(CodeInvalidSchema, path+"/requiredness", "unknown requiredness")
		case !oneOf([]string{BasisPolicyFloor, BasisOwnerDecision}, r.RequirednessBasis):
			return s, violation(CodeInvalidSchema, path+"/requiredness_basis", "unknown basis")
		case !oneOf([]string{StatusResolved, StatusOpenQuestion, StatusRejected}, r.Status):
			return s, violation(CodeInvalidSchema, path+"/status", "unknown status")
		case !oneOf([]string{ProvenanceHuman, ProvenanceAIConfirmed}, r.Provenance):
			return s, violation(CodeInvalidSchema, path+"/provenance", "unknown provenance")
		}
		seen[r.ID] = true
		semantics, v := canonicalSemantics(r, path+"/semantics")
		if v != nil {
			return s, v
		}
		r.Semantics = semantics
		out.Requirements[i] = r
	}
	return out, nil
}

// Validate checks the whole set.
func (s RequirementSet) Validate() error {
	if _, v := s.canonicalized(); v != nil {
		return v
	}
	return nil
}

// Canonical returns the canonical encoding Hash covers: the validated set with
// canonical semantics, compact, in declared field order.
func (s RequirementSet) Canonical() ([]byte, error) {
	c, v := s.canonicalized()
	if v != nil {
		return nil, v
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, violation(CodeInvalidJSON, "", "encode")
	}
	return raw, nil
}

// Hash is the hex SHA-256 of Canonical, or "" for an invalid set.
func (s RequirementSet) Hash() string {
	raw, err := s.Canonical()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// sameGeneric reports whether two JSON documents are equal as generic values, so
// an input with a differently cased, extra, missing or reordered-list member is
// rejected even though encoding/json would accept it.
func sameGeneric(a, b []byte) bool {
	x, err1 := metadata.DecodeJSON(a)
	y, err2 := metadata.DecodeJSON(b)
	return err1 == nil && err2 == nil && reflect.DeepEqual(x, y)
}

// ParseRequirementSet strictly parses a requirement set: no duplicate keys,
// unknown keys, differently cased keys, trailing data, unsorted role lists or
// non-canonical values. It returns the canonical bytes and their hash.
func ParseRequirementSet(raw []byte) (RequirementSet, []byte, string, error) {
	var s RequirementSet
	if len(raw) == 0 || len(raw) > MaxRequirementBytes {
		return s, nil, "", violation(CodeTooLarge, "", "requirement set must be 1..%d bytes", MaxRequirementBytes)
	}
	if _, err := metadata.DecodeJSON(raw); err != nil {
		return s, nil, "", violation(CodeInvalidJSON, "", "malformed or ambiguous JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil || d.More() {
		return RequirementSet{}, nil, "", violation(CodeInvalidSchema, "", "document does not match the requirement set schema")
	}
	if s.Requirements == nil {
		return RequirementSet{}, nil, "", violation(CodeInvalidSchema, "/requirements", "requirements are required")
	}
	canonical, err := s.Canonical()
	if err != nil {
		return RequirementSet{}, nil, "", err
	}
	if !sameGeneric(raw, canonical) {
		return RequirementSet{}, nil, "", violation(CodeNotCanonical, "", "document is not in canonical form (case, ordering or defaults differ)")
	}
	c, _ := s.canonicalized()
	sum := sha256.Sum256(canonical)
	return c, canonical, hex.EncodeToString(sum[:]), nil
}

// RequirementPolicy floors (requirement-policy-v1, operator-owned, hashed).
type floorRow struct {
	Category string `json:"category"`
	When     string `json:"when"`
}

var floorTable = []floorRow{
	{CategoryApprovalThreshold, "always"},
	{CategoryApprovalGate, "always"},
	{CategoryToolEffect, "always"},
	{CategoryDataMigration, "storage_not_equal"},
}

// RequirementPolicyHash covers the floor table, so changing a floor means
// publishing a new policy version.
func RequirementPolicyHash() string {
	raw, _ := json.Marshal(struct {
		Version string     `json:"version"`
		Floors  []floorRow `json:"floors"`
	}{RequirementPolicyVersion, floorTable})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// FloorFor returns the minimum requiredness of a category for a release whose
// storage evolution class is storageClass (EQUAL, ADDITIVE, TRANSFORM, BREAKING).
func FloorFor(category, storageClass string) string {
	for _, f := range floorTable {
		if f.Category != category {
			continue
		}
		if f.When == "always" || f.When == "storage_not_equal" && storageClass != "EQUAL" {
			return Mandatory
		}
	}
	return Optional
}

// CheckFloors rejects a set that downgrades a floor-mandatory category or claims
// a policy floor where none exists. It does not judge status: an open mandatory
// requirement is the verifier's BLOCKED OPEN_MANDATORY, not an authoring error.
func CheckFloors(s RequirementSet, storageClass string) error {
	for i, r := range s.Requirements {
		floor := FloorFor(r.Category, storageClass)
		path := fmt.Sprintf("/requirements/%d", i)
		switch {
		case floor == Mandatory && (r.Requiredness != Mandatory || r.RequirednessBasis != BasisPolicyFloor):
			return violation(CodeFloorViolation, path, "category %s is mandatory by policy for this release", r.Category)
		case r.RequirednessBasis == BasisPolicyFloor && !hasFloorRow(r.Category):
			return violation(CodeFloorViolation, path, "no policy floor applies to category %s", r.Category)
		case r.RequirednessBasis == BasisPolicyFloor && r.Requiredness != Mandatory:
			return violation(CodeFloorViolation, path, "a policy floor basis requires mandatory requiredness")
		}
	}
	return nil
}

func hasFloorRow(category string) bool {
	for _, f := range floorTable {
		if f.Category == category {
			return true
		}
	}
	return false
}

// Index returns requirement ids in sorted order, for deterministic iteration.
func (s RequirementSet) Index() []string {
	ids := make([]string, 0, len(s.Requirements))
	for _, r := range s.Requirements {
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	return ids
}

// Find returns the requirement with the given id.
func (s RequirementSet) Find(id string) (RequirementV2, bool) {
	for _, r := range s.Requirements {
		if r.ID == id {
			return r, true
		}
	}
	return RequirementV2{}, false
}
