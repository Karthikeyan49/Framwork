// Package intent owns independently authored semantic acceptance, not generated metadata tests.
package intent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Karthikeyan49/Framwork/internal/erp"
)

type Requirement struct {
	ID                                                                      string `json:"id"`
	Mandatory                                                               bool   `json:"mandatory"`
	Status                                                                  string `json:"status"`
	EntityID, FieldID, Operator, Threshold, Currency, ApproverRole, Trigger string
}
type Contract struct {
	Version      string        `json:"version"`
	Requirements []Requirement `json:"requirements"`
}
type Case struct{ RequirementID, Input, Expected string }
type Acceptance struct {
	Version, IntentHash, AuthorActorID, Provenance string
	Cases                                          []Case
}

func (c Contract) Validate() error {
	if c.Version != "1.0.0" || len(c.Requirements) == 0 || len(c.Requirements) > 100 {
		return errors.New("invalid intent")
	}
	seen := map[string]bool{}
	for _, r := range c.Requirements {
		if r.ID == "" || seen[r.ID] || len(r.ID) > 160 {
			return errors.New("invalid requirement ID")
		}
		seen[r.ID] = true
		if r.Status != "RESOLVED" && r.Status != "AMBIGUOUS" && r.Status != "UNRESOLVED_REQUIREMENT" {
			return errors.New("unknown intent resolution status")
		}
		if r.Mandatory && (r.Status != "RESOLVED" || r.EntityID == "" || r.FieldID == "" || r.Operator == "" || r.Trigger == "" || r.Threshold == "" || r.Currency == "" || r.ApproverRole == "") {
			return errors.New("UNRESOLVED_REQUIREMENT")
		}
		if r.Status == "RESOLVED" {
			switch r.Operator {
			case "GT", "GTE", "LT", "LTE", "EQ", "NEQ":
			default:
				return errors.New("unresolved comparison operator")
			}
			if (erp.Money{Amount: r.Threshold, Currency: erp.Currency(r.Currency)}).Validate() != nil {
				return errors.New("unresolved monetary threshold")
			}
			if len(r.EntityID) > 160 || len(r.FieldID) > 160 || len(r.ApproverRole) > 160 || len(r.Trigger) > 160 {
				return errors.New("intent semantic bound")
			}
		}
	}
	return nil
}
func (c Contract) Hash() string {
	raw, _ := json.Marshal(c)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func (a Acceptance) Validate(c Contract) error {
	if e := c.Validate(); e != nil {
		return e
	}
	if a.Version != "1.0.0" || a.IntentHash != c.Hash() || a.AuthorActorID == "" || a.Provenance != "HUMAN_INDEPENDENT" || len(a.Cases) == 0 || len(a.Cases) > 1000 {
		return errors.New("independent acceptance required")
	}
	covered := map[string]bool{}
	known := map[string]bool{}
	for _, r := range c.Requirements {
		known[r.ID] = true
	}
	for _, test := range a.Cases {
		if !known[test.RequirementID] || test.Input == "" || test.Expected == "" {
			return errors.New("invalid acceptance case")
		}
		covered[test.RequirementID] = true
	}
	for _, r := range c.Requirements {
		if r.Mandatory && !covered[r.ID] {
			return errors.New("missing independent acceptance")
		}
	}
	return nil
}
