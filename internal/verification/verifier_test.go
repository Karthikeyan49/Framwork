package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

const (
	evidenceHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tenantA      = "11111111-1111-4111-8111-111111111111"
)

// ------------------------------------------------------------------ fixtures

// orderOptions describes the purchase-order workflow under test. The zero value
// of every flag is the correct implementation: start routes amount > 500 USD to
// an approval by admin and everything else to completion.
type orderOptions struct {
	op        string   // comparison, default "gt"
	threshold string   // default "500"
	currency  string   // currency of the amount field and the literal, default "USD"
	roles     []string // approval roles, default ["admin"]
	always    bool     // constant-true route to approval
	vipSkip   bool     // a note "vip" skips approval
	discount  bool     // route reads a different money field
	gate      bool     // unconditional approval gate (for approval_gate requirements)
	gateSkip  bool     // with gate: a note "skip" bypasses the gate

	cap             string // route also requires amount <= cap (an upper cap that stops approving huge orders)
	noteExempt      bool   // route also requires note != "vip"
	noteNotNull     bool   // route also requires note to be set
	orNote          string // route is amount > threshold OR note == orNote (over-approval)
	rejectCompletes bool   // a rejected approval ends in complete instead of failed
	exemptBand      bool   // amounts strictly between bandLow (600) and bandHigh (900) skip approval
	bandLow         string // with exemptBand: the lower edge of the window
	bandHigh        string // with exemptBand: the upper edge of the window
	amountLiterals  int    // routes to completion for orders of exactly that many distinct amounts
}

func orderDocument(o orderOptions) metadata.Document {
	if o.op == "" {
		o.op = "gt"
	}
	if o.threshold == "" {
		o.threshold = "500"
	}
	if o.currency == "" {
		o.currency = "USD"
	}
	if o.roles == nil {
		o.roles = []string{"admin"}
	}
	version, namespace := metadata.WorkflowSchemaVersion, "example.orders"
	def := func(id, label, kind string) metadata.Definition {
		return metadata.Definition{ID: id, SchemaVersion: version, Namespace: namespace, Label: label, Kind: kind}
	}
	fields := []string{"field.order.amount", "field.order.id", "field.order.note"}
	doc := metadata.Document{SchemaVersion: version, AppID: "app.orders", Namespace: namespace}
	entity := def("entity.order", "Order", "entity")
	entity.Entity = &metadata.Entity{Fields: fields}
	add := func(id, typ string, required bool, currency string) {
		d := def(id, id, "field")
		d.Field = &metadata.Field{EntityRef: "entity.order", Type: typ, Required: required, Currency: currency}
		doc.Definitions = append(doc.Definitions, d)
	}
	if o.discount {
		entity.Entity.Fields = append(entity.Entity.Fields, "field.order.discount")
		add("field.order.discount", "money", false, o.currency)
	}
	doc.Definitions = append(doc.Definitions, entity)
	add("field.order.id", "string", true, "")
	add("field.order.amount", "money", true, o.currency)
	add("field.order.note", "string", false, "")
	field := "field.order.amount"
	if o.discount {
		field = "field.order.discount"
	}
	money := metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + o.threshold + `","currency":"` + o.currency + `"}`)}
	condition := &metadata.Expression{Op: o.op, Args: []metadata.Expression{{Op: "field", FieldRef: field}, money}}
	amountField := metadata.Expression{Op: "field", FieldRef: field}
	noteField := metadata.Expression{Op: "field", FieldRef: "field.order.note"}
	and := func(args ...metadata.Expression) *metadata.Expression {
		return &metadata.Expression{Op: "and", Args: args}
	}
	if o.cap != "" {
		capMoney := metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + o.cap + `","currency":"` + o.currency + `"}`)}
		condition = and(*condition, metadata.Expression{Op: "lte", Args: []metadata.Expression{amountField, capMoney}})
	}
	if o.noteExempt {
		condition = and(*condition, metadata.Expression{Op: "neq", Args: []metadata.Expression{noteField, {Op: "literal", Value: json.RawMessage(`"vip"`)}}})
	}
	if o.noteNotNull {
		condition = and(*condition, metadata.Expression{Op: "not", Args: []metadata.Expression{{Op: "is_null", Args: []metadata.Expression{noteField}}}})
	}
	if o.orNote != "" {
		condition = &metadata.Expression{Op: "or", Args: []metadata.Expression{*condition, {Op: "eq", Args: []metadata.Expression{noteField, {Op: "literal", Value: json.RawMessage(`"` + o.orNote + `"`)}}}}}
	}
	if o.always {
		condition = &metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "literal", Value: json.RawMessage(`true`)}, {Op: "literal", Value: json.RawMessage(`true`)}}}
	}
	note := func(value string) *metadata.Expression {
		return &metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "field", FieldRef: "field.order.note"}, {Op: "literal", Value: json.RawMessage(`"` + value + `"`)}}}
	}
	var start []metadata.Transition
	switch {
	case o.gate:
		if o.gateSkip {
			start = append(start, metadata.Transition{To: "state.complete", Condition: note("skip")})
		}
		if o.rejectCompletes {
			start = append(start, metadata.Transition{To: "state.failed", Condition: note("invalid")})
		}
		start = append(start, metadata.Transition{To: "state.approval"})
	default:
		if o.rejectCompletes {
			// Keeps state.failed reachable, which the workflow contract requires.
			start = append(start, metadata.Transition{To: "state.failed", Condition: &metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "field", FieldRef: "field.order.id"}, {Op: "literal", Value: json.RawMessage(`"invalid"`)}}}})
		}
		// Routes to completion for orders of exactly those amounts, eight to a route (a condition holds only so many).
		for from := 0; from < o.amountLiterals; from += 8 {
			var exact []metadata.Expression
			for i := from; i < o.amountLiterals && i < from+8; i++ {
				lit := metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(fmt.Sprintf(`{"amount":"%d","currency":"%s"}`, 10000+i, o.currency))}
				exact = append(exact, metadata.Expression{Op: "eq", Args: []metadata.Expression{amountField, lit}})
			}
			condition := &exact[0]
			if len(exact) > 1 {
				condition = &metadata.Expression{Op: "or", Args: exact}
			}
			start = append(start, metadata.Transition{To: "state.complete", Condition: condition})
		}
		if o.exemptBand {
			if o.bandLow == "" {
				o.bandLow, o.bandHigh = "600", "900"
			}
			low := metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + o.bandLow + `","currency":"` + o.currency + `"}`)}
			high := metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + o.bandHigh + `","currency":"` + o.currency + `"}`)}
			band := and(metadata.Expression{Op: "gt", Args: []metadata.Expression{amountField, low}}, metadata.Expression{Op: "lt", Args: []metadata.Expression{amountField, high}})
			start = append(start, metadata.Transition{To: "state.complete", Condition: band})
		}
		if o.vipSkip {
			start = append(start, metadata.Transition{To: "state.complete", Condition: note("vip")})
		}
		start = append(start, metadata.Transition{To: "state.approval", Condition: condition}, metadata.Transition{To: "state.complete"})
	}
	rejected := "state.failed"
	if o.rejectCompletes {
		rejected = "state.complete"
	}
	wf := def("workflow.order.approve", "Order approval", "workflow")
	wf.Workflow = &metadata.Workflow{DSLVersion: "2.0.0", Version: "1.0.0", TimeoutMS: 60000, EntityRef: "entity.order", Trigger: "manual", InitialState: "state.start", States: []metadata.WorkflowState{
		{ID: "state.approval", Type: "approval", ApprovalRoles: o.roles, ApprovalTimeoutMS: 30000, Transitions: []metadata.Transition{{To: "state.complete", On: "approved"}, {To: rejected, On: "rejected"}}},
		{ID: "state.complete", Type: "complete", Transitions: []metadata.Transition{}},
		{ID: "state.failed", Type: "failed", Transitions: []metadata.Transition{}},
		{ID: "state.start", Type: "start", Transitions: start},
	}}
	doc.Definitions = append(doc.Definitions, wf)
	return doc
}

func compileDocument(t *testing.T, doc metadata.Document, catalog compiler.Catalog) compiler.Program {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	s, err := v.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := compiler.Compile(s, catalog)
	if !p.Valid() {
		t.Fatalf("compile: %+v", ds)
	}
	// Verify what the production path verifies: the restored program.
	restored, err := compiler.Restore(p.JSON(), p.Hash(), compiler.SupportedIRVersions())
	if err != nil {
		t.Fatal(err)
	}
	return restored
}

func orderProgram(t *testing.T, o orderOptions) compiler.Program {
	return compileDocument(t, orderDocument(o), compiler.PlatformCatalog())
}

func thresholdRequirement(id string, context map[string]json.RawMessage, roles ...string) intent.RequirementV2 {
	if roles == nil {
		roles = []string{"admin"}
	}
	raw, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: "500.00", ApproverRoles: roles, Context: context})
	return requirement(id, intent.CategoryApprovalThreshold, raw)
}

func requirement(id, category string, semantics json.RawMessage) intent.RequirementV2 {
	return intent.RequirementV2{ID: id, Category: category, Requiredness: intent.Mandatory, RequirednessBasis: intent.BasisPolicyFloor, Status: intent.StatusResolved, Provenance: intent.ProvenanceHuman, Semantics: semantics}
}

func requirementSet(rs ...intent.RequirementV2) intent.RequirementSet {
	return intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.orders", Requirements: rs}
}

func amountGiven(amount string, extra map[string]string) map[string]json.RawMessage {
	given := map[string]json.RawMessage{"field.order.amount": json.RawMessage(`{"amount":"` + amount + `","currency":"USD"}`)}
	for k, v := range extra {
		given[k] = json.RawMessage(`"` + v + `"`)
	}
	return given
}

var regular = map[string]json.RawMessage{"field.order.note": json.RawMessage(`"regular"`)}

// controlSpec is an independent acceptance spec for the control requirement,
// including the adversarial "vip" case a careful tester would add.
func controlSpec(set intent.RequirementSet) intent.AcceptanceSpec {
	return intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{
		{ID: "above-threshold", RequirementID: "po-approval", Given: amountGiven("501.00", nil), Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}},
		{ID: "vip-large-order", RequirementID: "po-approval", Given: amountGiven("750.00", map[string]string{"field.order.note": "vip"}), Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}},
	}}
}

func input(p compiler.Program, set intent.RequirementSet, spec intent.AcceptanceSpec) Input {
	return Input{Program: p, Requirements: set, Acceptance: spec, StorageClass: "EQUAL", Tools: tools.EmptyCatalog(), TenantID: tenantA, SandboxEvidenceHash: evidenceHash}
}

func controlInput(t *testing.T, o orderOptions) Input {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	return input(orderProgram(t, o), set, controlSpec(set))
}

func codesOf(r Report, severity string) []string {
	out := []string{}
	for _, f := range r.Findings {
		if f.Severity == severity {
			out = append(out, f.Code)
		}
	}
	return out
}

func mustVerify(t *testing.T, in Input) Report {
	t.Helper()
	r, err := Verify(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func hasFinding(r Report, code, caseID string) bool {
	for _, f := range r.Findings {
		if f.Code == code && (caseID == "" || f.CaseID == caseID) {
			return true
		}
	}
	return false
}

func hasFindingPrefix(r Report, code, prefix string) bool {
	for _, f := range r.Findings {
		if f.Code == code && strings.HasPrefix(f.CaseID, prefix) {
			return true
		}
	}
	return false
}

// -------------------------------------------------------------------- control

func TestControlFixturePasses(t *testing.T) {
	r := mustVerify(t, controlInput(t, orderOptions{}))
	if r.Outcome != Pass || len(r.Findings) != 0 {
		t.Fatalf("%s: %+v", r.Outcome, r.Findings)
	}
	passed := map[string]bool{}
	for _, c := range r.Results {
		if c.Status != Pass {
			t.Fatalf("%+v", c)
		}
		passed[c.ID] = true
	}
	for _, id := range []string{"po-approval/probe/below", "po-approval/probe/at", "po-approval/probe/above", "po-approval/probe/zero", "po-approval/probe/negative", "po-approval/probe/currency", "above-threshold", "vip-large-order"} {
		if !passed[id] {
			t.Errorf("%s was not executed", id)
		}
	}
	if r.VerifierVersion != VerifierVersion || r.RequirementPolicyHash != intent.RequirementPolicyHash() || r.IRHash == "" || r.SandboxEvidenceHash != evidenceHash {
		t.Fatalf("report is not bound to its inputs: %+v", r)
	}
}

// ------------------------------------------------------------------ mutations

func TestMutantsFailOrBlockWithSpecificCodes(t *testing.T) {
	cases := []struct {
		name     string
		options  orderOptions
		severity string
		code     string
		caseID   string
	}{
		{"GTE instead of GT", orderOptions{op: "gte"}, Fail, CodeApprovalMismatch, "po-approval/probe/at"},
		{"threshold constant wrong", orderOptions{threshold: "5000"}, Fail, CodeApprovalMismatch, "po-approval/probe/above"},
		{"threshold constant lowered", orderOptions{threshold: "100"}, Fail, CodeApprovalMismatch, "po-approval/probe/below"},
		{"wrong approver role", orderOptions{roles: []string{"member"}}, Fail, CodeApprovalMismatch, "po-approval/probe/above"},
		{"extra approver role", orderOptions{roles: []string{"admin", "owner"}}, Fail, CodeApprovalMismatch, "po-approval/probe/above"},
		{"rule constant true", orderOptions{always: true}, Fail, CodeApprovalMismatch, "po-approval/probe/below"},
		{"vip bypass skips approval", orderOptions{vipSkip: true}, Fail, CodeApprovalMismatch, "vip-large-order"},
		{"wrong currency field", orderOptions{currency: "EUR", threshold: "500"}, Fail, CodeApprovalMismatch, "po-approval/probe/above"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := mustVerify(t, controlInput(t, c.options))
			if r.Outcome != c.severity || !hasFinding(r, c.code, c.caseID) {
				t.Fatalf("%s %+v", r.Outcome, r.Findings)
			}
		})
	}
	t.Run("route reads a different money field", func(t *testing.T) {
		r := mustVerify(t, controlInput(t, orderOptions{discount: true}))
		if r.Outcome == Pass || !hasFinding(r, CodeIncompleteCaseInput, "") && !hasFinding(r, CodeApprovalMismatch, "") {
			t.Fatalf("%s %+v", r.Outcome, r.Findings)
		}
	})
	t.Run("optional fields the route reads default to null", func(t *testing.T) {
		// Without a context the probes still run (an absent optional field reads
		// as null), and the independent vip case still catches the bypass.
		in := controlInput(t, orderOptions{vipSkip: true})
		set := requirementSet(thresholdRequirement("po-approval", nil))
		in.Requirements, in.Acceptance = set, controlSpec(set)
		r := mustVerify(t, in)
		if r.Outcome != Fail || !hasFinding(r, CodeApprovalMismatch, "vip-large-order") {
			t.Fatalf("%s %+v", r.Outcome, r.Findings)
		}
	})
}

// A requirement that pins a field ("for regular notes") scopes itself: a bypass
// that lives only in the pinned-away world is outside what it promises, and no
// example case reaches it, so it passes. That is the documented limit of a
// pinned context; the unpinned case is the next test.
func TestPinnedContextScopesTheRequirement(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := controlSpec(set)
	spec.Cases = spec.Cases[:1]
	if r := mustVerify(t, input(orderProgram(t, orderOptions{vipSkip: true}), set, spec)); r.Outcome != Pass {
		t.Fatalf("the documented limit changed: %s %+v", r.Outcome, r.Findings)
	}
}

// Independent review F4/F5: when the requirement does not pin a field, "any
// value" includes the values the route compares against. Exemptions and extra OR
// branches on unpinned fields fail without any acceptance case reaching them.
func TestUnpinnedFieldsAreMinedFromTheRoute(t *testing.T) {
	cases := []struct {
		name    string
		options orderOptions
		outcome string
		caseID  string
	}{
		{"correct route", orderOptions{}, Pass, ""},
		{"vip skips approval", orderOptions{vipSkip: true}, Fail, "po-approval/probe/above/"},
		{"vip exempt in the condition", orderOptions{noteExempt: true}, Fail, "po-approval/probe/above/"},
		{"unset note skips approval", orderOptions{noteNotNull: true}, Fail, "po-approval/probe/above"},
		{"note x approves small orders", orderOptions{orNote: "x"}, Fail, "po-approval/probe/below/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			set := requirementSet(thresholdRequirement("po-approval", nil))
			spec := controlSpec(set)
			spec.Cases = spec.Cases[:1] // no vip case: only the mechanical probes can find it
			r := mustVerify(t, input(orderProgram(t, c.options), set, spec))
			if r.Outcome != c.outcome {
				t.Fatalf("%s %+v", r.Outcome, r.Findings)
			}
			if c.caseID != "" && !hasFindingPrefix(r, CodeApprovalMismatch, c.caseID) {
				t.Fatalf("no %s finding for %q: %+v", CodeApprovalMismatch, c.caseID, r.Findings)
			}
		})
	}
}

// Independent review F3: an upper cap that stops approving large orders passed
// because the probes stopped one tick above the threshold.
func TestUpperCapOnTheApprovalRouteFails(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := controlSpec(set)
	if r := mustVerify(t, input(orderProgram(t, orderOptions{cap: "1000000"}), set, spec)); r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, "po-approval/probe/plus1e") {
		t.Fatalf("cap at one million: %s %+v", r.Outcome, r.Findings)
	}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{cap: "1000000000000"}), set, spec)); r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, "po-approval/probe/plus1e") {
		t.Fatalf("cap at 1e12: %s %+v", r.Outcome, r.Findings)
	}
}

// Independent re-verification FR-02: an exemption band between the boundary probes (orders from 600 to 900
// skip approval) passed because no probe landed inside it; ratios of the threshold now do.
func TestExemptionBandBetweenBoundaryProbesFails(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	if r := mustVerify(t, input(orderProgram(t, orderOptions{exemptBand: true}), set, controlSpec(set))); r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, "po-approval/probe/ratio-") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), set, controlSpec(set))); r.Outcome != Pass {
		t.Fatalf("honest route: %s %+v", r.Outcome, r.Findings)
	}
}

// Independent review M14R-05: a window that lets orders skip the approval and is narrower than the gap between the fixed probes was
// passed (626..749, 501..502 and 1500..1999 over a threshold of 500, with 0 findings). The probes around the amounts the plan itself
// compares the field with find it: the fixed probes alone pass each of the narrow windows (they pass without the literal probes).
func TestExemptionWindowsBetweenTheFixedProbesFail(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := controlSpec(set)
	for _, w := range []struct {
		low, high string
		narrow    bool // the fixed probes do not land inside it
	}{
		{"600", "900", false}, {"3001", "4999", false},
		{"626", "749", true}, {"501", "502", true}, {"1500", "1999", true}, {"2501", "2999", true},
	} {
		r := mustVerify(t, input(orderProgram(t, orderOptions{exemptBand: true, bandLow: w.low, bandHigh: w.high}), set, spec))
		prefix := "po-approval/probe/"
		if w.narrow {
			prefix += "lit-"
		}
		if r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, prefix) {
			t.Errorf("window %s..%s: %s, findings %+v", w.low, w.high, r.Outcome, r.Findings)
		}
	}
	// The honest plan passes, and so does a window below the threshold: orders of 10 to 499 need no approval anyway.
	for name, o := range map[string]orderOptions{"honest route": {}, "window below the threshold": {exemptBand: true, bandLow: "10", bandHigh: "499"}} {
		if r := mustVerify(t, input(orderProgram(t, o), set, spec)); r.Outcome != Pass {
			t.Errorf("%s: %s %+v", name, r.Outcome, r.Findings)
		}
	}
}

func TestLiteralProbesFollowTheAmountsOfThePlan(t *testing.T) {
	thr := intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: "500.00", ApproverRoles: []string{"admin"}}
	probesOf := func(o orderOptions) ([]string, map[string]intent.Probe) {
		wf, err := orderProgram(t, o).Workflow("workflow.order.approve")
		if err != nil {
			t.Fatal(err)
		}
		amounts := literalAmounts(wf.Plan(), thr)
		fixed, err := intent.DeriveProbes("r", thr)
		if err != nil {
			t.Fatal(err)
		}
		taken := map[string]bool{}
		for _, p := range fixed {
			if p.Currency == thr.Currency {
				taken[p.Amount] = true
			}
		}
		more, err := literalProbes("r", thr, amounts, taken)
		if err != nil {
			t.Fatal(err)
		}
		var edges []string
		for _, a := range amounts {
			edges = append(edges, a.FloatString(2))
		}
		byAmount := map[string]intent.Probe{}
		for _, p := range more {
			if _, dup := byAmount[p.Amount]; dup {
				t.Fatalf("amount %s probed twice", p.Amount)
			}
			byAmount[p.Amount] = p
		}
		return edges, byAmount
	}
	// The honest plan compares the field with 500 only, which the fixed probes already cover: nothing is added.
	if edges, more := probesOf(orderOptions{}); !reflect.DeepEqual(edges, []string{"500.00"}) || len(more) != 0 {
		t.Fatalf("honest plan: amounts %v, %d extra probes", edges, len(more))
	}
	// A route that reads another money field is not about the threshold field: its amounts are not probed.
	if edges, more := probesOf(orderOptions{discount: true}); len(edges) != 0 || len(more) != 0 {
		t.Fatalf("a route on another field: amounts %v, %d extra probes", edges, len(more))
	}
	// A window from 626 to 749 adds a unit below, at and a unit above each edge, and the midpoints of 500, 626 and 749.
	edges, more := probesOf(orderOptions{exemptBand: true, bandLow: "626", bandHigh: "749"})
	if !reflect.DeepEqual(edges, []string{"500.00", "626.00", "749.00"}) {
		t.Fatalf("amounts %v", edges)
	}
	for _, want := range []string{"625.99", "626.00", "626.01", "748.99", "749.00", "749.01", "563.00", "687.50"} {
		p, ok := more[want]
		if !ok {
			t.Errorf("no probe at %s (have %d)", want, len(more))
			continue
		}
		// The expectation never comes from the plan: every one of these amounts needs the admin approval.
		if p.Expect.Outcome != intent.OutcomeApprovalRequired || !reflect.DeepEqual(p.Expect.Roles, []string{"admin"}) {
			t.Errorf("probe %s expects %s", want, p.Expect)
		}
	}
	if len(more) != 8 {
		t.Errorf("%d extra probes, want 8", len(more))
	}
	// The midpoints include the threshold of the requirement, which the plan need not spell: a plan that compares with 5000 is probed
	// halfway between that and 500, too (5000 itself is ten times the threshold, a fixed probe).
	edges, more = probesOf(orderOptions{threshold: "5000"})
	if !reflect.DeepEqual(edges, []string{"5000.00"}) {
		t.Fatalf("amounts %v", edges)
	}
	for _, want := range []string{"4999.99", "5000.01", "2750.00"} {
		if _, ok := more[want]; !ok {
			t.Errorf("no probe at %s (have %d)", want, len(more))
		}
	}
	if len(more) != 3 {
		t.Errorf("%d extra probes, want 3", len(more))
	}
}

// A change to the checks needs a new VerifierVersion, never an edit (report.go). The probes are the checks: this pins the ids and the
// expectations of the probes of one honest plan and one plan with a window to the version, so that adding, removing or moving a probe
// fails here until the version is bumped and the digest is updated with it.
func TestProbeSetIsPinnedToTheVerifierVersion(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	sum := sha256.New()
	for _, o := range []orderOptions{{}, {exemptBand: true, bandLow: "626", bandHigh: "749"}} {
		r := mustVerify(t, input(orderProgram(t, o), set, controlSpec(set)))
		for _, c := range r.Results {
			fmt.Fprintf(sum, "%s|%s|%s\n", c.ID, c.Source, c.Expected)
		}
	}
	const pinned = "verifier-v3"
	const digest = "bab54f37caada06bded13034fc1f795066d8abc3c2f2c362c1d677f33b5a1fa9"
	if VerifierVersion != pinned || hex.EncodeToString(sum.Sum(nil)) != digest {
		t.Fatalf("the probes of %s hash to %s (this test pins %s, digest %s): bump VerifierVersion and update both", VerifierVersion, hex.EncodeToString(sum.Sum(nil)), pinned, digest)
	}
}

// The probes around the amounts of a plan are bounded: a plan that compares the field with more distinct amounts than the verifier
// probes is BLOCKED, not probed in part.
func TestAPlanWithTooManyAmountsIsBlocked(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := controlSpec(set)
	// 500 (the threshold) and 31 more amounts are exactly the bound: probed, and the routes to completion are found out.
	r := mustVerify(t, input(orderProgram(t, orderOptions{amountLiterals: MaxLiteralAmounts - 1}), set, spec))
	if r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, "po-approval/probe/lit-") {
		t.Fatalf("at the bound: %s %+v", r.Outcome, r.Findings)
	}
	r = mustVerify(t, input(orderProgram(t, orderOptions{amountLiterals: MaxLiteralAmounts}), set, spec))
	if r.Outcome != Blocked || len(codesOf(r, Blocked)) == 0 || codesOf(r, Blocked)[0] != CodeUnsupportedByVerifier {
		t.Fatalf("beyond the bound: %s %+v", r.Outcome, r.Findings)
	}
}

// Independent review F2: a rejected approval that still ends in complete means a
// rejection completes the order.
func TestRejectedApprovalThatCompletesFails(t *testing.T) {
	// The id is pinned: the fixture's failure branch reads it, and a required field
	// the route reads must be pinned or the probes block.
	pinned := map[string]json.RawMessage{"field.order.note": json.RawMessage(`"regular"`), "field.order.id": json.RawMessage(`"PO-1"`)}
	set := requirementSet(thresholdRequirement("po-approval", pinned))
	if r := mustVerify(t, input(orderProgram(t, orderOptions{rejectCompletes: true}), set, controlSpec(set))); r.Outcome != Fail || !hasFindingPrefix(r, CodeApprovalMismatch, "po-approval/probe/above") {
		t.Fatalf("threshold: %s %+v", r.Outcome, r.Findings)
	}
	gate := requirement("po-gate", intent.CategoryApprovalGate, json.RawMessage(`{"workflow_id":"workflow.order.approve","approver_roles":["admin"]}`))
	gset := requirementSet(gate)
	gspec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: gset.Hash(), Cases: []intent.AcceptanceCase{{ID: "gate", RequirementID: "po-gate", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}}}}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true, rejectCompletes: true}), gset, gspec)); r.Outcome != Fail || !hasFinding(r, CodeGateBypass, "") {
		t.Fatalf("gate: %s %+v", r.Outcome, r.Findings)
	}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true}), gset, gspec)); r.Outcome != Pass {
		t.Fatalf("honest gate: %s %+v", r.Outcome, r.Findings)
	}
}

func TestApprovalGateRequirements(t *testing.T) {
	gate := requirement("po-gate", intent.CategoryApprovalGate, json.RawMessage(`{"workflow_id":"workflow.order.approve","approver_roles":["admin"]}`))
	set := requirementSet(gate)
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{{ID: "gate", RequirementID: "po-gate", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}}}}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true}), set, spec)); r.Outcome != Pass {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true, gateSkip: true}), set, spec)); r.Outcome != Fail || !hasFinding(r, CodeGateBypass, "") {
		t.Fatalf("bypass path: %s %+v", r.Outcome, r.Findings)
	}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true, roles: []string{"member"}}), set, spec)); r.Outcome != Fail || !hasFinding(r, CodeGateRolesMismatch, "") {
		t.Fatalf("wrong roles: %s %+v", r.Outcome, r.Findings)
	}
	// The threshold-shaped workflow has an approval-free path by design.
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), set, spec)); r.Outcome != Fail || !hasFinding(r, CodeGateBypass, "") {
		t.Fatalf("conditional approval is not an unconditional gate: %s %+v", r.Outcome, r.Findings)
	}
	// Extra approvals with other roles do not weaken a satisfied gate.
	both := requirement("po-gate", intent.CategoryApprovalGate, json.RawMessage(`{"workflow_id":"workflow.order.approve","approver_roles":["admin","owner"]}`))
	set2 := requirementSet(both)
	spec2 := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set2.Hash(), Cases: spec.Cases}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{gate: true, roles: []string{"admin", "owner"}}), set2, spec2)); r.Outcome != Pass {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
}

func TestBlockedStates(t *testing.T) {
	// Unknown category cannot satisfy a mandatory requirement.
	unknown := requirement("sod", "segregation_of_duties", json.RawMessage(`{"a":1}`))
	unknown.RequirednessBasis = intent.BasisOwnerDecision
	set := requirementSet(thresholdRequirement("po-approval", regular), unknown)
	spec := controlSpec(set)
	spec.Cases = append(spec.Cases, intent.AcceptanceCase{ID: "sod-case", RequirementID: "sod", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}})
	spec.RequirementsHash = set.Hash()
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), set, spec)); r.Outcome != Blocked || !hasFinding(r, CodeUnsupportedByVerifier, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
	// Open mandatory requirement.
	open := requirementSet(thresholdRequirement("po-approval", regular))
	open.Requirements[0].Status = intent.StatusOpenQuestion
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), open, controlSpec(open))); r.Outcome != Blocked || !hasFinding(r, CodeOpenMandatory, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
	rejected := open
	rejected.Requirements = []intent.RequirementV2{open.Requirements[0]}
	rejected.Requirements[0].Status = intent.StatusRejected
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), rejected, controlSpec(rejected))); r.Outcome != Blocked {
		t.Fatalf("rejected mandatory: %s", r.Outcome)
	}
	// Floor violation: a floor category marked optional.
	floor := requirementSet(thresholdRequirement("po-approval", regular))
	floor.Requirements[0].Requiredness, floor.Requirements[0].RequirednessBasis = intent.Optional, intent.BasisOwnerDecision
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), floor, controlSpec(floor))); r.Outcome != Blocked || !hasFinding(r, CodeFloorViolation, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
	// A workflow the release does not contain.
	missing := requirementSet(requirement("po-approval", intent.CategoryApprovalThreshold, json.RawMessage(`{"workflow_id":"workflow.order.ghost","entity_id":"entity.order","field_id":"field.order.amount","currency":"USD","op":"GT","amount":"500.00","approver_roles":["admin"]}`)))
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), missing, controlSpec(missing))); r.Outcome != Blocked || !hasFinding(r, CodeNoExecutablePlan, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
	// A field that is not money.
	notMoney := requirementSet(requirement("po-approval", intent.CategoryApprovalThreshold, json.RawMessage(`{"workflow_id":"workflow.order.approve","entity_id":"entity.order","field_id":"field.order.note","currency":"USD","op":"GT","amount":"500.00","approver_roles":["admin"]}`)))
	spec3 := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: notMoney.Hash(), Cases: []intent.AcceptanceCase{{ID: "c", RequirementID: "po-approval", Given: map[string]json.RawMessage{"field.order.note": json.RawMessage(`{"amount":"501.00","currency":"USD"}`)}, Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}}}}
	if r := mustVerify(t, input(orderProgram(t, orderOptions{}), notMoney, spec3)); r.Outcome != Blocked || !hasFinding(r, CodeNoExecutablePlan, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
}

func TestLegacyIRIsBlocked(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/examples/contacts-draft-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc metadata.Document
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	legacy := compileDocument(t, doc, compiler.PlatformCatalog())
	if legacy.IR().IRVersion != compiler.IRVersion {
		t.Fatal("fixture is not legacy")
	}
	set := requirementSet(thresholdRequirement("po-approval", regular))
	r := mustVerify(t, input(legacy, set, controlSpec(set)))
	if r.Outcome != Blocked || !hasFinding(r, CodeLegacyIR, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
}

func TestMigrationRequirements(t *testing.T) {
	m := func(bound string) intent.RequirementSet {
		return requirementSet(requirement("migration", intent.CategoryDataMigration, json.RawMessage(`{"max_class":"`+bound+`"}`)))
	}
	for _, c := range []struct {
		class, bound string
		want         string
	}{
		{"EQUAL", "COMPATIBLE", Pass}, {"ADDITIVE", "COMPATIBLE", Pass}, {"TRANSFORM", "COMPATIBLE", Fail}, {"TRANSFORM", "TRANSFORM", Pass}, {"BREAKING", "TRANSFORM", Fail},
	} {
		set := m(c.bound)
		spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{{ID: "m", RequirementID: "migration", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}}}}
		in := input(orderProgram(t, orderOptions{}), set, spec)
		in.StorageClass = c.class
		r := mustVerify(t, in)
		// data_migration is only a floor for non-equal storage; an EQUAL release
		// claiming the floor is fine because the claim names a real floor row.
		if r.Outcome != c.want {
			t.Errorf("%s/%s: %s %+v", c.class, c.bound, r.Outcome, r.Findings)
		}
	}
}

// ----------------------------------------------------------------------- tools

func toolDefinition(ref, effect, approval, sandbox string) tools.Definition {
	d := tools.Definition{
		Ref: ref, Version: "1.0.0", SideEffect: effect, Approval: approval,
		Delivery:  tools.DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: tools.Connector{Endpoint: tools.Endpoint{Host: "vendors.example.com", Port: 443, Network: "public"}, Method: "GET", Path: "/vendors/{vendor_id}", TimeoutMS: 1000, MaxResponseBytes: 4096, RatePerSecond: 10},
		Input:     []tools.Field{{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "path"}},
		Output:    []tools.Field{{Name: "risk_score", Type: "integer", Required: true}},
		Sandbox:   tools.SandboxSpec{Mode: sandbox, Output: map[string]json.RawMessage{"risk_score": json.RawMessage(`7`)}},
		Tenants:   []string{"*"},
	}
	if effect == "WRITE" {
		d.Delivery = tools.DeliverySpec{IdempotencyMode: "NATIVE_KEY", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL", ReplayWindowMS: 86400000}
		d.Connector.Method, d.Connector.Path, d.Connector.IdempotencyHeader = "POST", "/vendors", "Idempotency-Key"
		d.Input = []tools.Field{{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "body"}}
		d.Output = []tools.Field{{Name: "notification_id", Type: "string", Required: true, MaxLength: 64}}
		d.Sandbox.Output = map[string]json.RawMessage{"notification_id": json.RawMessage(`"sim-1"`)}
	}
	return d
}

func toolProgram(t *testing.T, catalog tools.Catalog, ref string) compiler.Program {
	t.Helper()
	version := metadata.ToolWorkflowSchemaVersion
	ns := "example.vendors"
	def := func(id, kind string) metadata.Definition {
		return metadata.Definition{ID: id, SchemaVersion: version, Namespace: ns, Label: id, Kind: kind}
	}
	entity := def("entity.vendor", "entity")
	entity.Entity = &metadata.Entity{Fields: []string{"field.vendor.id"}}
	id := def("field.vendor.id", "field")
	id.Field = &metadata.Field{EntityRef: "entity.vendor", Type: "string", Required: true}
	wf := def("workflow.vendor.sync", "workflow")
	wf.Workflow = &metadata.Workflow{DSLVersion: "2.1.0", Version: "1.0.0", TimeoutMS: 60000, EntityRef: "entity.vendor", Trigger: "manual", InitialState: "state.start", States: []metadata.WorkflowState{
		{ID: "state.approve", Type: "approval", ApprovalRoles: []string{"admin"}, ApprovalTimeoutMS: 30000, Transitions: []metadata.Transition{{To: "state.call", On: "approved"}, {To: "state.failed", On: "rejected"}}},
		{ID: "state.failed", Type: "failed", Transitions: []metadata.Transition{}},
		{ID: "state.call", Type: "activity", Activity: &metadata.Activity{ToolRef: ref, Version: "1.0.0", Inputs: map[string]string{"vendor_id": "field.vendor.id"}, Retry: metadata.Retry{MaxAttempts: 2, InitialBackoffMS: 100, MaxBackoffMS: 1000}}, Transitions: []metadata.Transition{{To: "state.complete"}}},
		{ID: "state.complete", Type: "complete", Transitions: []metadata.Transition{}},
		{ID: "state.start", Type: "start", Transitions: []metadata.Transition{{To: "state.approve"}}},
	}}
	cc := compiler.PlatformCatalog()
	cc.ToolCatalog = catalog
	return compileDocument(t, metadata.Document{SchemaVersion: version, AppID: "app.vendors", Namespace: ns, Definitions: []metadata.Definition{entity, id, wf}}, cc)
}

func TestToolEffectRequirements(t *testing.T) {
	read := toolDefinition("tool.vendor_lookup", "READ", "NONE", "SIMULATE")
	write := toolDefinition("tool.vendor_notify", "WRITE", "REQUIRED", "SIMULATE")
	catalog, err := tools.NewCatalog([]tools.Definition{read, write})
	if err != nil {
		t.Fatal(err)
	}
	effect := func(ref, max string, approval bool, sandbox string) intent.RequirementSet {
		raw, _ := json.Marshal(intent.ToolEffect{ToolRef: ref, MaxSideEffect: max, RequiresApproval: approval, MaxSandboxMode: sandbox})
		return requirementSet(requirement("tool", intent.CategoryToolEffect, raw))
	}
	run := func(program compiler.Program, set intent.RequirementSet, cat tools.Catalog, tenant string) Report {
		spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{{ID: "t", RequirementID: "tool", Expect: intent.Expectation{Outcome: intent.OutcomeHolds}}}}
		in := input(program, set, spec)
		in.Tools, in.TenantID = cat, tenant
		return mustVerify(t, in)
	}
	lookup, notify := toolProgram(t, catalog, "tool.vendor_lookup"), toolProgram(t, catalog, "tool.vendor_notify")
	if r := run(lookup, effect("tool.vendor_lookup", "READ", false, "SIMULATE"), catalog, tenantA); r.Outcome != Pass {
		t.Fatalf("control: %+v", r.Findings)
	}
	if r := run(notify, effect("tool.vendor_notify", "READ", false, "SIMULATE"), catalog, tenantA); r.Outcome != Fail || !hasFinding(r, CodeToolEffect, "") {
		t.Fatalf("write tool under a read bound: %+v", r.Findings)
	}
	if r := run(lookup, effect("tool.vendor_lookup", "READ", true, "SIMULATE"), catalog, tenantA); r.Outcome != Fail || !hasFinding(r, CodeToolApproval, "") {
		t.Fatalf("approval required but absent: %+v", r.Findings)
	}
	if r := run(lookup, effect("tool.vendor_lookup", "READ", false, "BLOCK"), catalog, tenantA); r.Outcome != Fail || !hasFinding(r, CodeToolSandbox, "") {
		t.Fatalf("sandbox bound: %+v", r.Findings)
	}
	if r := run(notify, effect("tool.vendor_notify", "WRITE", true, "TEST_ACCOUNT"), catalog, tenantA); r.Outcome != Pass {
		t.Fatalf("notify within bounds: %+v", r.Findings)
	}
	// Re-pinned or removed from the catalog: the pinned hash no longer resolves.
	changed := write
	changed.Connector.RatePerSecond = 9
	moved, _ := tools.NewCatalog([]tools.Definition{read, changed})
	if r := run(notify, effect("tool.vendor_notify", "WRITE", true, "TEST_ACCOUNT"), moved, tenantA); r.Outcome != Fail || !hasFinding(r, CodeToolUnresolved, "") {
		t.Fatalf("stale pin: %+v", r.Findings)
	}
	if r := run(notify, effect("tool.vendor_notify", "WRITE", true, "TEST_ACCOUNT"), tools.EmptyCatalog(), tenantA); r.Outcome != Fail || !hasFinding(r, CodeToolUnresolved, "") {
		t.Fatalf("missing catalog: %+v", r.Findings)
	}
	private := write
	private.Tenants = []string{"22222222-2222-4222-8222-222222222222"}
	restricted, _ := tools.NewCatalog([]tools.Definition{read, private})
	notifyPrivate := toolProgram(t, restricted, "tool.vendor_notify")
	if r := run(notifyPrivate, effect("tool.vendor_notify", "WRITE", true, "TEST_ACCOUNT"), restricted, tenantA); r.Outcome != Fail || !hasFinding(r, CodeToolUnresolved, "") {
		t.Fatalf("tenant not allowed: %+v", r.Findings)
	}
	// A tool no activity uses is vacuously within bounds.
	if r := run(lookup, effect("tool.vendor_notify", "NONE", false, "BLOCK"), catalog, tenantA); r.Outcome != Pass {
		t.Fatalf("vacuous: %+v", r.Findings)
	}
}

// ------------------------------------------------------------- completeness

func TestCompletenessRequiresResolvedMatchingRequirements(t *testing.T) {
	changed := ChangedSurfaces{FinancialRuleEntities: []string{"entity.order"}, ApprovalGateWorkflows: []string{"workflow.order.approve"}, ToolRefs: []string{"tool.vendor_notify"}, NonEqualStorage: true}
	if got := CheckCompleteness(changed, requirementSet()); len(got) != 4 {
		t.Fatalf("empty set: %+v", got)
	}
	for _, f := range CheckCompleteness(changed, requirementSet()) {
		if f.Code != CodeOmittedRequirement || f.Severity != Blocked {
			t.Fatalf("%+v", f)
		}
	}
	set := requirementSet(
		thresholdRequirement("po-approval", regular),
		requirement("tool", intent.CategoryToolEffect, json.RawMessage(`{"tool_ref":"tool.vendor_notify","max_side_effect":"WRITE","requires_approval":true,"max_sandbox_mode":"SIMULATE"}`)),
		requirement("migration", intent.CategoryDataMigration, json.RawMessage(`{"max_class":"TRANSFORM"}`)),
	)
	if got := CheckCompleteness(changed, set); len(got) != 0 {
		t.Fatalf("covered: %+v", got)
	}
	open := set
	open.Requirements = append([]intent.RequirementV2(nil), set.Requirements...)
	open.Requirements[0].Status = intent.StatusOpenQuestion
	if got := CheckCompleteness(changed, open); len(got) != 2 {
		t.Fatalf("an unresolved requirement must not cover: %+v", got)
	}
	// A requirement about another entity, workflow or tool does not cover.
	other := requirementSet(requirement("tool", intent.CategoryToolEffect, json.RawMessage(`{"tool_ref":"tool.vendor_other","max_side_effect":"WRITE","requires_approval":true,"max_sandbox_mode":"SIMULATE"}`)))
	if got := CheckCompleteness(ChangedSurfaces{ToolRefs: []string{"tool.vendor_notify"}}, other); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	// Completeness is part of Verify: an uncovered change blocks an otherwise good release.
	in := controlInput(t, orderOptions{})
	in.Changed = ChangedSurfaces{ToolRefs: []string{"tool.vendor_notify"}}
	if r := mustVerify(t, in); r.Outcome != Blocked || !hasFinding(r, CodeOmittedRequirement, "") {
		t.Fatalf("%s %+v", r.Outcome, r.Findings)
	}
}

// ------------------------------------------------------- determinism / binding

func TestVerifyIsDeterministicAndRecomputable(t *testing.T) {
	in := controlInput(t, orderOptions{op: "gte"})
	a, b := mustVerify(t, in), mustVerify(t, in)
	if string(a.JSON()) != string(b.JSON()) || a.Hash() != b.Hash() {
		t.Fatal("nondeterministic")
	}
	if err := Recompute(in, a.JSON()); err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(a.JSON()), `"outcome":"FAIL"`, `"outcome":"PASS"`, 1)
	if err := Recompute(in, []byte(tampered)); !errors.Is(err, ErrReportMismatch) {
		t.Fatalf("tampered report accepted: %v", err)
	}
	pass := mustVerify(t, controlInput(t, orderOptions{}))
	if err := Recompute(in, pass.JSON()); !errors.Is(err, ErrReportMismatch) {
		t.Fatal("a PASS report of another program verified")
	}
	// Reordering the human cases changes the spec hash but not the findings or results.
	spec := in.Acceptance
	spec.Cases = []intent.AcceptanceCase{spec.Cases[1], spec.Cases[0]}
	in2 := in
	in2.Acceptance = spec
	c := mustVerify(t, in2)
	if !reflect.DeepEqual(a.Results, c.Results) || !reflect.DeepEqual(a.Findings, c.Findings) || a.Outcome != c.Outcome || a.AcceptanceHash == c.AcceptanceHash {
		t.Fatal("report depends on authored case order beyond its hash")
	}
}

func TestInvalidInputsAreErrorsNotReports(t *testing.T) {
	good := controlInput(t, orderOptions{})
	cases := map[string]func(*Input){
		"zero program":        func(in *Input) { in.Program = compiler.Program{} },
		"bad storage class":   func(in *Input) { in.StorageClass = "MAYBE" },
		"bad evidence hash":   func(in *Input) { in.SandboxEvidenceHash = "xyz" },
		"no tenant":           func(in *Input) { in.TenantID = "" },
		"spec of another set": func(in *Input) { in.Acceptance.RequirementsHash = strings.Repeat("0", 64) },
		"oracle disagrees": func(in *Input) {
			s := in.Acceptance
			s.Cases = append([]intent.AcceptanceCase(nil), s.Cases...)
			s.Cases[0].Expect = intent.Expectation{Outcome: intent.OutcomeNoApproval}
			in.Acceptance = s
		},
		"mandatory uncovered": func(in *Input) {
			s := in.Acceptance
			s.Cases = nil
			in.Acceptance = s
		},
		"invalid requirement set": func(in *Input) {
			s := in.Requirements
			s.SchemaVersion = "requirements-v1"
			in.Requirements = s
		},
	}
	for name, mutate := range cases {
		in := good
		mutate(&in)
		if r, err := Verify(in); !errors.Is(err, ErrInput) || r.Outcome != "" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestNonMandatoryRequirementsAreSkippedAndReported(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	advisory := requirement("advisory", "ux_review", json.RawMessage(`{"note":"nice to have"}`))
	advisory.Requiredness, advisory.RequirednessBasis = intent.Recommended, intent.BasisOwnerDecision
	set.Requirements = append(set.Requirements, advisory)
	r := mustVerify(t, input(orderProgram(t, orderOptions{}), set, controlSpec(set)))
	skipped := false
	for _, c := range r.Results {
		if c.ID == "advisory" && c.Status == Skipped {
			skipped = true
		}
	}
	if r.Outcome != Pass || !skipped {
		t.Fatalf("%s %+v", r.Outcome, r.Results)
	}
}

func TestWorkBudgetFailsClosed(t *testing.T) {
	in := controlInput(t, orderOptions{})
	// A tiny budget cannot be configured, so exercise the gate walk directly.
	v := &run{in: in, ir: in.Program.IR(), work: MaxWork}
	v.report = Report{Findings: []Finding{}, Results: []CaseResult{}}
	set := requirementSet(requirement("po-gate", intent.CategoryApprovalGate, json.RawMessage(`{"workflow_id":"workflow.order.approve","approver_roles":["admin"]}`)))
	v.in.Requirements = set
	v.gate(set.Requirements[0], intent.ApprovalGate{WorkflowID: "workflow.order.approve", ApproverRoles: []string{"admin"}})
	if !v.overflow {
		t.Fatal("budget not enforced")
	}
}

// ------------------------------------------------------------ no hidden authority

func TestPackageHasNoAmbientAuthority(t *testing.T) {
	forbidden := map[string]bool{"database/sql": true, "net": true, "net/http": true, "os": true, "os/exec": true, "time": true, "math/rand": true, "crypto/rand": true, "io/ioutil": true, "sync": true}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatal("no sources", err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if forbidden[path] || strings.Contains(path, "internal/platform") || strings.Contains(path, "internal/promotion") || strings.Contains(path, "internal/risk") {
				t.Errorf("%s imports %s", name, path)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "compiler" && x.Sel.Name == "Compile" {
					t.Errorf("%s compiles source", name)
				}
			case *ast.GoStmt:
				t.Errorf("%s starts a goroutine", name)
			}
			return true
		})
	}
}
