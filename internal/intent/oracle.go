package intent

import (
	"fmt"
	"math/big"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/erp"
)

// Expectation outcomes of acceptance cases and of the oracle.
const (
	OutcomeNoApproval       = "NO_APPROVAL"
	OutcomeApprovalRequired = "APPROVAL_REQUIRED"
	OutcomeInputRejected    = "INPUT_REJECTED"
	OutcomeHolds            = "HOLDS"
)

// Reasons an input can be rejected.
const (
	ReasonType     = "TYPE"
	ReasonCurrency = "CURRENCY"
	ReasonEnum     = "ENUM"
	ReasonRequired = "REQUIRED"
	ReasonRange    = "RANGE"
)

// Expectation is what a case, probe or oracle says must happen.
type Expectation struct {
	Outcome string   `json:"outcome"`
	Roles   []string `json:"roles,omitempty"`
	Reason  string   `json:"reason,omitempty"`
}

// Equal compares outcome, roles (as sets) and reason.
func (e Expectation) Equal(o Expectation) bool {
	if e.Outcome != o.Outcome || e.Reason != o.Reason || len(e.Roles) != len(o.Roles) {
		return false
	}
	a, b := append([]string(nil), e.Roles...), append([]string(nil), o.Roles...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (e Expectation) String() string {
	switch e.Outcome {
	case OutcomeApprovalRequired:
		return fmt.Sprintf("%s%v", e.Outcome, e.Roles)
	case OutcomeInputRejected:
		return e.Outcome + "(" + e.Reason + ")"
	}
	return e.Outcome
}

// validExpectation checks the vocabulary of one expectation.
func validExpectation(e Expectation) bool {
	switch e.Outcome {
	case OutcomeNoApproval, OutcomeHolds:
		return len(e.Roles) == 0 && e.Reason == ""
	case OutcomeApprovalRequired:
		return validRoles(e.Roles) && e.Reason == ""
	case OutcomeInputRejected:
		return len(e.Roles) == 0 && oneOf([]string{ReasonType, ReasonCurrency, ReasonEnum, ReasonRequired, ReasonRange}, e.Reason)
	}
	return false
}

func rat(amount string) (*big.Rat, bool) {
	r, ok := new(big.Rat).SetString(amount)
	return r, ok
}

// OracleThreshold is the pure expected outcome of putting `amount` of `currency`
// into the threshold field. It is an exact rational comparison of the
// requirement and the input only: it never reads an IR, plan, workflow or source,
// so a wrong implementation cannot influence what is expected of it.
func OracleThreshold(t ApprovalThreshold, amount, currency string) (Expectation, error) {
	limit, ok := rat(t.Amount)
	if !ok {
		return Expectation{}, fmt.Errorf("threshold amount is not a decimal")
	}
	if _, err := erp.Currency(currency).MinorUnits(); err != nil || currency != t.Currency {
		return Expectation{Outcome: OutcomeInputRejected, Reason: ReasonCurrency}, nil
	}
	if (erp.Money{Amount: amount, Currency: erp.Currency(currency)}).Validate() != nil {
		return Expectation{Outcome: OutcomeInputRejected, Reason: ReasonType}, nil
	}
	value, ok := rat(amount)
	if !ok {
		return Expectation{Outcome: OutcomeInputRejected, Reason: ReasonType}, nil
	}
	c := value.Cmp(limit)
	var hit bool
	switch t.Op {
	case "GT":
		hit = c > 0
	case "GTE":
		hit = c >= 0
	case "LT":
		hit = c < 0
	case "LTE":
		hit = c <= 0
	case "EQ":
		hit = c == 0
	case "NEQ":
		hit = c != 0
	default:
		return Expectation{}, fmt.Errorf("unknown comparison")
	}
	if !hit {
		return Expectation{Outcome: OutcomeNoApproval}, nil
	}
	return Expectation{Outcome: OutcomeApprovalRequired, Roles: append([]string{}, t.ApproverRoles...)}, nil
}

// Probe is one mechanically derived boundary input of a threshold requirement.
type Probe struct {
	ID       string      `json:"id"`
	Amount   string      `json:"amount"`
	Currency string      `json:"currency"`
	Expect   Expectation `json:"expect"`
}

// amountText formats a rational that is a multiple of 10^-minor exactly.
func amountText(r *big.Rat, minor int) string { return r.FloatString(minor) }

// DeriveProbes returns the boundary probes of a threshold requirement: one minor
// currency unit below, at and above the threshold, zero, a negative amount and a
// wrong currency. They are always added and a human cannot remove them.
// Duplicate (amount, currency) pairs keep the first kind in that order.
func DeriveProbes(requirementID string, t ApprovalThreshold) ([]Probe, error) {
	minor, err := erp.Currency(t.Currency).MinorUnits()
	if err != nil {
		return nil, err
	}
	limit, ok := rat(t.Amount)
	if !ok {
		return nil, fmt.Errorf("threshold amount is not a decimal")
	}
	unit := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(minor)), nil))
	wrong := "EUR"
	if t.Currency == wrong {
		wrong = "USD"
	}
	type spec struct {
		kind, amount, currency string
	}
	specs := []spec{
		{"below", amountText(new(big.Rat).Sub(limit, unit), minor), t.Currency},
		{"at", amountText(limit, minor), t.Currency},
		{"above", amountText(new(big.Rat).Add(limit, unit), minor), t.Currency},
		{"zero", amountText(new(big.Rat), minor), t.Currency},
		{"negative", amountText(new(big.Rat).Neg(unit), minor), t.Currency},
		{"currency", amountText(limit, minor), wrong},
	}
	seen := map[string]bool{}
	probes := []Probe{}
	for _, s := range specs {
		key := s.amount + " " + s.currency
		if seen[key] {
			continue
		}
		seen[key] = true
		expect, err := OracleThreshold(t, s.amount, s.currency)
		if err != nil {
			return nil, err
		}
		probes = append(probes, Probe{ID: requirementID + "/probe/" + s.kind, Amount: s.amount, Currency: s.currency, Expect: expect})
	}
	return probes, nil
}
