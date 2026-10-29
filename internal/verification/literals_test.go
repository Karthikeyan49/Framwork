package verification

import (
	"math/big"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/erp"
	"github.com/Karthikeyan49/Framwork/internal/intent"
)

// Independent review PF-01: the probes around the amounts of the plan ran in the world of the requirement only, so an exemption window
// written with those amounts and gated on a field the requirement does not pin ("amount between 626 and 749 and note gold") passed,
// although the same window without the gate fails. They are now repeated for the mined values of the other fields, as the boundary
// probes are. A single amount that is also a fixed probe which is not repeated (a ratio of the threshold) is probed again as a literal.
func TestAWindowGatedOnAnotherFieldIsFound(t *testing.T) {
	open := requirementSet(thresholdRequirement("po-approval", nil)) // the requirement does not pin the note
	pinned := requirementSet(thresholdRequirement("po-approval", regular))
	for _, w := range []struct {
		name string
		o    orderOptions
	}{
		{"window 626..749", orderOptions{exemptBand: true, bandLow: "626", bandHigh: "749", bandNote: "gold"}},
		{"window 501..502", orderOptions{exemptBand: true, bandLow: "501", bandHigh: "502", bandNote: "gold"}},
		{"wide window 600..900", orderOptions{exemptBand: true, bandLow: "600", bandHigh: "900", bandNote: "gold"}},
		{"one amount that is a ratio of the threshold (2 x 500)", orderOptions{exactAt: "1000", bandNote: "gold"}},
		{"one amount off every fixed probe", orderOptions{exactAt: "777.77", bandNote: "gold"}},
	} {
		r := mustVerify(t, input(orderProgram(t, w.o), open, controlSpec(open)))
		found := false
		for _, f := range r.Findings {
			// The mined variants carry the field and the value in their ids.
			if f.Code == CodeApprovalMismatch && strings.Contains(f.CaseID, "/probe/lit-") && strings.Contains(f.CaseID, "field.order.note=") {
				found = true
			}
		}
		if r.Outcome != Fail || !found {
			t.Errorf("%s: %s, findings %+v", w.name, r.Outcome, r.Findings)
		}
		// A requirement that pins the note to another value leaves the gated window out of reach by design (a world the requirement
		// pins away, ADR102 amendment 13): the same plan passes there. The control is that the ungated plans above are not that case.
		if r := mustVerify(t, input(orderProgram(t, w.o), pinned, controlSpec(pinned))); r.Outcome != Pass {
			t.Errorf("%s with the note pinned: %s %+v", w.name, r.Outcome, r.Findings)
		}
	}
	// The honest plan stays clean when the requirement leaves the note open.
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), open, controlSpec(open))); r.Outcome != Pass {
		t.Errorf("honest plan: %s %+v", r.Outcome, r.Findings)
	}
}

// Independent review PF-02: every probe amount was on the grid of the minor unit although the data layer accepts 18 decimals, so a window
// between two amounts one minor unit apart passed (the midpoint of 626.00 and 626.01 was rounded onto one of them). The exact midpoints
// reach it.
func TestWindowsBetweenTheMinorUnitsAreFound(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := controlSpec(set)
	for _, w := range []struct {
		name string
		o    orderOptions
	}{
		{"window (500.001, 500.009)", orderOptions{exemptBand: true, bandLow: "500.001", bandHigh: "500.009"}},
		{"window (626, 626.01)", orderOptions{exemptBand: true, bandLow: "626", bandHigh: "626.01"}},
		{"gte 500.01 over GT 500.00", orderOptions{op: "gte", threshold: "500.01"}},
		{"gt 500.001 over GT 500.00", orderOptions{threshold: "500.001"}},
		{"gt 500.000000000000000001", orderOptions{threshold: "500.000000000000000001"}},
	} {
		r := mustVerify(t, input(orderProgram(t, w.o), set, spec))
		if r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, "po-approval/probe/lit-") {
			t.Errorf("%s: %s, findings %+v", w.name, r.Outcome, r.Findings)
		}
	}
	// Plans that are right at that resolution still pass: a window below the threshold, and an honest GT 500.00.
	for name, o := range map[string]orderOptions{"honest": {}, "window below the threshold": {exemptBand: true, bandLow: "499.001", bandHigh: "499.009"}} {
		if r := mustVerify(t, input(orderProgram(t, o), set, spec)); r.Outcome != Pass {
			t.Errorf("%s: %s %+v", name, r.Outcome, r.Findings)
		}
	}
}

func TestMoneyTextKeepsTheDecimalsOfTheCurrencyAndNoMore(t *testing.T) {
	rat := func(s string) *big.Rat {
		v, ok := new(big.Rat).SetString(s)
		if !ok {
			t.Fatalf("bad test amount %q", s)
		}
		return v
	}
	for _, c := range []struct {
		amount string
		minor  int
		want   string
	}{
		{"500", 2, "500.00"}, {"500.5", 2, "500.50"}, {"500.005", 2, "500.005"}, {"500.000000000000000001", 2, "500.000000000000000001"},
		{"-0.000000000000000001", 2, "-0.000000000000000001"}, {"0", 2, "0.00"}, {"-0.01", 2, "-0.01"},
		{"50001", 0, "50001"}, {"50001.5", 0, "50001.5"}, {"0", 0, "0"}, {"500.001", 3, "500.001"}, {"500.0015", 3, "500.0015"}, {"500", 3, "500.000"},
		{"1/3", 2, "0.333333333333333333"},
	} {
		if got := moneyText(rat(c.amount), c.minor); got != c.want {
			t.Errorf("moneyText(%s, %d) = %q, want %q", c.amount, c.minor, got, c.want)
		}
	}
}

// Every probe text is an amount the data layer accepts, and the midpoint of two neighbours one minor unit apart is a real point between
// them in every currency (USD 2 decimals, KWD 3, JPY 0), never one of the two.
func TestLiteralProbesReachBetweenNeighboursOneMinorUnitApart(t *testing.T) {
	for _, c := range []struct {
		currency, limit, a, b string
		mid                   string
	}{
		{"USD", "500.00", "500.01", "500.02", "500.015"},
		{"KWD", "500.000", "500.001", "500.002", "500.0015"},
		{"JPY", "50000", "50001", "50002", "50001.5"},
	} {
		thr := intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: c.currency, Op: "GT", Amount: c.limit, ApproverRoles: []string{"admin"}}
		a, _ := new(big.Rat).SetString(c.a)
		b, _ := new(big.Rat).SetString(c.b)
		fixed, err := intent.DeriveProbes("r", thr)
		if err != nil {
			t.Fatal(err)
		}
		probes, err := literalProbes("r", thr, []*big.Rat{a, b}, takenAmounts(fixed, c.currency))
		if err != nil {
			t.Fatal(err)
		}
		have := map[string]bool{}
		for _, p := range probes {
			if err := (erp.Money{Amount: p.Amount, Currency: erp.Currency(p.Currency)}).Validate(); err != nil {
				t.Errorf("%s: probe %s is not an amount the data layer accepts: %v", c.currency, p.ID, err)
			}
			have[p.Amount] = true
		}
		if !have[c.mid] {
			t.Errorf("%s: no probe at the midpoint %s of %s and %s (have %d)", c.currency, c.mid, c.a, c.b, len(probes))
		}
	}
}
