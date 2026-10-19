package intent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// AcceptanceCase is one independently authored example. Given holds the typed
// values of the fields it sets (a money field uses {"amount","currency"}).
type AcceptanceCase struct {
	ID            string                     `json:"id"`
	RequirementID string                     `json:"requirement_id"`
	Given         map[string]json.RawMessage `json:"given,omitempty"`
	Expect        Expectation                `json:"expect"`
}

// AcceptanceSpec is bound to its requirement set by hash and to nothing else: its
// closed schema has no member for an IR, source, compiler or artifact hash, so
// an acceptance author cannot be steered by, or depend on, the implementation.
type AcceptanceSpec struct {
	SchemaVersion    string           `json:"schema_version"`
	RequirementsHash string           `json:"requirements_hash"`
	Cases            []AcceptanceCase `json:"cases"`
}

// Money extracts the amount and currency of a money given-value.
func Money(raw json.RawMessage) (amount, currency string, ok bool) {
	v, err := metadata.DecodeJSON(raw)
	if err != nil {
		return "", "", false
	}
	m, isMap := v.(map[string]any)
	if !isMap || len(m) != 2 {
		return "", "", false
	}
	a, ok1 := m["amount"].(string)
	c, ok2 := m["currency"].(string)
	return a, c, ok1 && ok2
}

func (s AcceptanceSpec) canonicalized() (AcceptanceSpec, *Violation) {
	if s.SchemaVersion != AcceptanceSpecVersion {
		return s, violation(CodeInvalidSchema, "/schema_version", "unsupported acceptance spec version")
	}
	if !hashPattern.MatchString(s.RequirementsHash) {
		return s, violation(CodeInvalidSchema, "/requirements_hash", "requirements hash must be 64 hex digits")
	}
	if len(s.Cases) > MaxAcceptanceCases {
		return s, violation(CodeTooLarge, "/cases", "more than %d cases", MaxAcceptanceCases)
	}
	out := s
	out.Cases = make([]AcceptanceCase, len(s.Cases))
	seen := map[string]bool{}
	for i, c := range s.Cases {
		path := fmt.Sprintf("/cases/%d", i)
		if !requirementID.MatchString(c.ID) || seen[c.ID] {
			return s, violation(CodeInvalidCase, path+"/id", "invalid or duplicate case id")
		}
		seen[c.ID] = true
		if !requirementID.MatchString(c.RequirementID) {
			return s, violation(CodeInvalidCase, path+"/requirement_id", "invalid requirement id")
		}
		if !validExpectation(c.Expect) {
			return s, violation(CodeInvalidCase, path+"/expect", "invalid expectation")
		}
		if len(c.Given) > MaxContextEntries {
			return s, violation(CodeInvalidCase, path+"/given", "too many given values")
		}
		if len(c.Given) > 0 {
			given := make(map[string]json.RawMessage, len(c.Given))
			for id, value := range c.Given {
				canonical, ok := canonicalScalar(value)
				if !fieldID.MatchString(id) || len(id) > 160 || !ok {
					return s, violation(CodeInvalidCase, path+"/given", "value for %q is not a bounded scalar or money object", id)
				}
				given[id] = canonical
			}
			c.Given = given
		}
		out.Cases[i] = c
	}
	return out, nil
}

// Validate checks the spec's own shape.
func (s AcceptanceSpec) Validate() error {
	if _, v := s.canonicalized(); v != nil {
		return v
	}
	return nil
}

// Canonical returns the canonical encoding Hash covers.
func (s AcceptanceSpec) Canonical() ([]byte, error) {
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

// Hash is the hex SHA-256 of Canonical, or "" for an invalid spec.
func (s AcceptanceSpec) Hash() string {
	raw, err := s.Canonical()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ParseAcceptanceSpec strictly parses an acceptance spec (see ParseRequirementSet).
func ParseAcceptanceSpec(raw []byte) (AcceptanceSpec, []byte, string, error) {
	var s AcceptanceSpec
	if len(raw) == 0 || len(raw) > MaxAcceptanceBytes {
		return s, nil, "", violation(CodeTooLarge, "", "acceptance spec must be 1..%d bytes", MaxAcceptanceBytes)
	}
	if _, err := metadata.DecodeJSON(raw); err != nil {
		return s, nil, "", violation(CodeInvalidJSON, "", "malformed or ambiguous JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil || d.More() {
		return AcceptanceSpec{}, nil, "", violation(CodeInvalidSchema, "", "document does not match the acceptance spec schema")
	}
	if s.Cases == nil {
		return AcceptanceSpec{}, nil, "", violation(CodeInvalidSchema, "/cases", "cases are required")
	}
	canonical, err := s.Canonical()
	if err != nil {
		return AcceptanceSpec{}, nil, "", err
	}
	if !sameGeneric(raw, canonical) {
		return AcceptanceSpec{}, nil, "", violation(CodeNotCanonical, "", "document is not in canonical form (case, ordering or defaults differ)")
	}
	c, _ := s.canonicalized()
	sum := sha256.Sum256(canonical)
	return c, canonical, hex.EncodeToString(sum[:]), nil
}

// ValidateAcceptance is the authoring-time gate. It binds the spec to the set by
// hash, requires every mandatory requirement to be covered, and checks each
// human case against the oracle: a case that disagrees with the requirement is
// rejected (ORACLE_DISAGREES) instead of being allowed to define the behavior.
// Human cases may add to, but never remove, the probes derived by DeriveProbes.
func ValidateAcceptance(spec AcceptanceSpec, set RequirementSet) error {
	if _, v := spec.canonicalized(); v != nil {
		return v
	}
	if _, v := set.canonicalized(); v != nil {
		return v
	}
	if spec.RequirementsHash != set.Hash() {
		return violation(CodeHashMismatch, "/requirements_hash", "acceptance spec is not bound to this requirement set")
	}
	covered := map[string]bool{}
	for _, c := range spec.Cases {
		r, ok := set.Find(c.RequirementID)
		if !ok {
			return violation(CodeUnknownReq, "/cases/"+c.ID, "case names requirement %q which does not exist", c.RequirementID)
		}
		covered[r.ID] = true
		if r.Category != CategoryApprovalThreshold {
			if c.Expect.Outcome != OutcomeHolds || len(c.Given) != 0 {
				return violation(CodeInvalidCase, "/cases/"+c.ID, "static requirements are asserted with HOLDS and no given values")
			}
			continue
		}
		t, err := r.Threshold()
		if err != nil {
			return violation(CodeInvalidSemantics, "/cases/"+c.ID, "requirement semantics are invalid")
		}
		if c.Expect.Outcome == OutcomeHolds {
			return violation(CodeInvalidCase, "/cases/"+c.ID, "a threshold case states an approval outcome, not HOLDS")
		}
		raw, ok := c.Given[t.FieldID]
		if !ok {
			return violation(CodeInvalidCase, "/cases/"+c.ID, "a threshold case must give the value of %s", t.FieldID)
		}
		amount, currency, ok := Money(raw)
		if !ok {
			return violation(CodeInvalidCase, "/cases/"+c.ID, "%s must be a money value", t.FieldID)
		}
		want, err := OracleThreshold(t, amount, currency)
		if err != nil {
			return violation(CodeInvalidSemantics, "/cases/"+c.ID, "oracle cannot evaluate the requirement")
		}
		if !want.Equal(c.Expect) {
			return violation(CodeOracleDisagrees, "/cases/"+c.ID, "case expects %s but requirement %q implies %s", c.Expect, r.ID, want)
		}
	}
	missing := []string{}
	for _, r := range set.Requirements {
		if r.Requiredness == Mandatory && !covered[r.ID] {
			missing = append(missing, r.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return violation(CodeMissingCoverage, "/cases", "mandatory requirements without an acceptance case: %v", missing)
	}
	return nil
}
