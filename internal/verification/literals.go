package verification

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/erp"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

// MaxLiteralAmounts bounds the distinct money amounts the conditions of a plan may compare the threshold field with. Every amount
// is probed on both sides and between its neighbours, so a plan beyond the bound is BLOCKED rather than probed in part.
const MaxLiteralAmounts = 32

// literalAmounts returns, ascending and without duplicates, the money amounts in the currency of the requirement that the
// conditions of the plan compare against. Only conditions that read the threshold field count: an exemption window that lets orders
// skip the approval must be written there, with the amounts of its edges, and the fixed probes of the oracle cannot know them.
func literalAmounts(plan workflow.Plan, t intent.ApprovalThreshold) []*big.Rat {
	found := map[string]*big.Rat{}
	for _, state := range plan.States {
		for _, tr := range state.Transitions {
			if tr.Condition == nil {
				continue
			}
			// A condition lists every field of the entity, so it is the expression that tells whether the field is read.
			readsField := false
			var money []*big.Rat
			var walk func(n rules.Node)
			walk = func(n rules.Node) {
				if n.Op == "field" && n.FieldRef == t.FieldID {
					readsField = true
				}
				if (n.Op == "literal" || n.Op == "typed_literal") && n.Type.Kind == "money" && n.Type.Currency == t.Currency {
					var m struct {
						Amount string `json:"amount"`
					}
					if json.Unmarshal(n.Value, &m) == nil {
						if r, ok := new(big.Rat).SetString(m.Amount); ok {
							money = append(money, r)
						}
					}
				}
				for _, a := range n.Args {
					walk(a)
				}
			}
			walk(tr.Condition.Root)
			if !readsField {
				continue
			}
			for _, r := range money {
				found[r.RatString()] = r
			}
		}
	}
	out := make([]*big.Rat, 0, len(found))
	for _, r := range found {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cmp(out[j]) < 0 })
	return out
}

// literalProbes are the boundary probes of the amounts the plan compares the threshold field with (independent review M14R-05: an
// exemption window narrower than the gap between the fixed probes of intent.DeriveProbes passed). For every amount: one minor unit
// below, at and one above; and the midpoint of every two neighbours among those amounts and the threshold. The expectation of a probe
// is the oracle's, computed from the requirement alone, so the plan decides what is probed and never what is expected. Amounts that
// an earlier probe already uses (taken) are left out. It is still a mechanical floor: a window that the plan writes with arithmetic
// instead of with the amounts of its edges, or on another field, is not found here.
func literalProbes(requirement string, t intent.ApprovalThreshold, amounts []*big.Rat, taken map[string]bool) ([]intent.Probe, error) {
	minor, err := erp.Currency(t.Currency).MinorUnits()
	if err != nil {
		return nil, err
	}
	limit, ok := new(big.Rat).SetString(t.Amount)
	if !ok {
		return nil, fmt.Errorf("threshold amount is not a decimal")
	}
	unit := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(minor)), nil))
	type spec struct {
		kind   string
		amount *big.Rat
	}
	var specs []spec
	for _, a := range amounts {
		specs = append(specs, spec{"lit-below", new(big.Rat).Sub(a, unit)}, spec{"lit-at", a}, spec{"lit-above", new(big.Rat).Add(a, unit)})
	}
	points := append([]*big.Rat{limit}, amounts...)
	sort.Slice(points, func(i, j int) bool { return points[i].Cmp(points[j]) < 0 })
	for i := 0; i+1 < len(points); i++ {
		if points[i].Cmp(points[i+1]) != 0 {
			specs = append(specs, spec{"lit-mid", new(big.Rat).Quo(new(big.Rat).Add(points[i], points[i+1]), big.NewRat(2, 1))})
		}
	}
	probes := []intent.Probe{}
	for _, s := range specs {
		text := s.amount.FloatString(minor)
		if taken[text] {
			continue
		}
		taken[text] = true
		expect, err := intent.OracleThreshold(t, text, t.Currency)
		if err != nil {
			return nil, err
		}
		probes = append(probes, intent.Probe{ID: requirement + "/probe/" + s.kind + "-" + text, Amount: text, Currency: t.Currency, Expect: expect})
	}
	return probes, nil
}
