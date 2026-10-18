package verification

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/erp"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/runtime/rules"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// ErrInput rejects an input contract that is not even verifiable (an invalid
// program, requirement set or acceptance spec, or mismatched bindings). Verify
// never turns such an input into a report.
var ErrInput = errors.New("verification input is invalid")

// ErrReportMismatch is returned by Recompute when stored bytes differ from the
// recomputed report.
var ErrReportMismatch = errors.New("stored verification report does not match its recomputation")

// MaxWork bounds the walk steps of one verification, so hostile cases fail
// closed (BLOCKED BUDGET) instead of hanging a request.
const MaxWork = 50000

// ChangedSurfaces lists what the release changed that requirements must cover.
// The builder derives it from the diff; the verifier only applies it.
type ChangedSurfaces struct {
	FinancialRuleEntities []string // entity ids of added/changed/removed financial rules
	ApprovalGateWorkflows []string // workflow ids with an approval-gate change
	ToolRefs              []string // tool refs of added/removed/re-pinned/inherited activities
	NonEqualStorage       bool
}

// Input is everything Verify reads.
type Input struct {
	Program             compiler.Program
	Requirements        intent.RequirementSet
	Acceptance          intent.AcceptanceSpec
	StorageClass        string // EQUAL | ADDITIVE | TRANSFORM | BREAKING
	Changed             ChangedSurfaces
	Tools               tools.Catalog
	TenantID            string
	SandboxEvidenceHash string
}

var hexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (in Input) validate() error {
	switch in.StorageClass {
	case "EQUAL", "ADDITIVE", "TRANSFORM", "BREAKING":
	default:
		return fmt.Errorf("%w: storage class", ErrInput)
	}
	if !in.Program.Valid() || !hexHash.MatchString(in.SandboxEvidenceHash) || in.TenantID == "" {
		return fmt.Errorf("%w: program, evidence hash or tenant", ErrInput)
	}
	if err := in.Requirements.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInput, err)
	}
	if in.Requirements.Hash() != in.Acceptance.RequirementsHash {
		return fmt.Errorf("%w: acceptance is bound to another requirement set", ErrInput)
	}
	// The authoring gate runs again: a stored spec that contradicts the oracle or
	// leaves a mandatory requirement uncovered must never verify.
	if err := intent.ValidateAcceptance(in.Acceptance, in.Requirements); err != nil {
		return fmt.Errorf("%w: %v", ErrInput, err)
	}
	return nil
}

type run struct {
	in       Input
	ir       compiler.IR
	report   Report
	work     int
	overflow bool
}

func (v *run) add(severity, code, requirement, caseID, format string, args ...any) {
	v.report.Findings = append(v.report.Findings, Finding{Code: code, Severity: severity, RequirementID: requirement, CaseID: caseID, Detail: fmt.Sprintf(format, args...)})
}
func (v *run) block(code, requirement, caseID, format string, args ...any) {
	v.add(Blocked, code, requirement, caseID, format, args...)
}
func (v *run) fail(code, requirement, caseID, format string, args ...any) {
	v.add(Fail, code, requirement, caseID, format, args...)
}
func (v *run) result(id, requirement, source, expected, observed, status string) {
	v.report.Results = append(v.report.Results, CaseResult{ID: id, RequirementID: requirement, Source: source, Expected: expected, Observed: observed, Status: status})
}

// Verify checks a candidate against its requirements and independent acceptance.
func Verify(in Input) (Report, error) {
	if err := in.validate(); err != nil {
		return Report{}, err
	}
	v := &run{in: in, ir: in.Program.IR()}
	v.report = Report{
		VerifierVersion: VerifierVersion, VocabularyVersion: intent.VocabularyVersion,
		RequirementPolicyVersion: intent.RequirementPolicyVersion, RequirementPolicyHash: intent.RequirementPolicyHash(),
		RequirementsHash: in.Requirements.Hash(), AcceptanceHash: in.Acceptance.Hash(), IRHash: in.Program.Hash(),
		SandboxEvidenceHash: in.SandboxEvidenceHash, StorageClass: in.StorageClass,
		Findings: []Finding{}, Results: []CaseResult{},
	}
	if err := intent.CheckFloors(in.Requirements, in.StorageClass); err != nil {
		v.block(CodeFloorViolation, "", "", "%v", err)
	}
	for _, f := range CheckCompleteness(in.Changed, in.Requirements) {
		v.report.Findings = append(v.report.Findings, f)
	}
	for _, r := range in.Requirements.Requirements {
		if v.overflow {
			break
		}
		switch {
		case r.Requiredness != intent.Mandatory:
			v.result(r.ID, r.ID, SourceStatic, "NOT_MANDATORY", "not verified", Skipped)
		case r.Status != intent.StatusResolved:
			v.block(CodeOpenMandatory, r.ID, "", "mandatory requirement is %s", r.Status)
			v.result(r.ID, r.ID, SourceStatic, "RESOLVED", r.Status, Blocked)
		case !r.Known():
			v.block(CodeUnsupportedByVerifier, r.ID, "", "category %q cannot be verified", r.Category)
			v.result(r.ID, r.ID, SourceStatic, "VERIFIABLE_CATEGORY", r.Category, Blocked)
		default:
			v.requirement(r)
		}
	}
	if v.overflow {
		v.block(CodeBudget, "", "", "verification work exceeded %d steps", MaxWork)
	}
	v.report.settle()
	return v.report, nil
}

// Recompute re-runs Verify and requires the stored bytes to match exactly.
func Recompute(in Input, stored []byte) error {
	report, err := Verify(in)
	if err != nil {
		return err
	}
	if !bytes.Equal(report.JSON(), stored) {
		return ErrReportMismatch
	}
	return nil
}

func (v *run) requirement(r intent.RequirementV2) {
	switch r.Category {
	case intent.CategoryApprovalThreshold:
		t, err := r.Threshold()
		if err != nil {
			v.block(CodeUnsupportedByVerifier, r.ID, "", "semantics are invalid")
			return
		}
		v.threshold(r, t)
	case intent.CategoryApprovalGate:
		g, err := r.Gate()
		if err != nil {
			v.block(CodeUnsupportedByVerifier, r.ID, "", "semantics are invalid")
			return
		}
		v.gate(r, g)
	case intent.CategoryToolEffect:
		t, err := r.Tool()
		if err != nil {
			v.block(CodeUnsupportedByVerifier, r.ID, "", "semantics are invalid")
			return
		}
		v.toolEffect(r, t)
	case intent.CategoryDataMigration:
		m, err := r.Migration()
		if err != nil {
			v.block(CodeUnsupportedByVerifier, r.ID, "", "semantics are invalid")
			return
		}
		v.migration(r, m)
	}
}

// legacy reports an IR without executable workflow plans.
func (v *run) legacy(requirement string) bool {
	if v.ir.IRVersion == compiler.IRVersion || v.ir.IRVersion == compiler.RuleIRVersion {
		v.block(CodeLegacyIR, requirement, "", "IR %s has no executable workflow plans", v.ir.IRVersion)
		v.result(requirement, requirement, SourceStatic, "EXECUTABLE_PLAN", "IR "+v.ir.IRVersion, Blocked)
		return true
	}
	return false
}

func (v *run) workflowOf(requirement, id string) (workflow.Program, workflow.Plan, bool) {
	wf, err := v.in.Program.Workflow(id)
	if err != nil {
		v.block(CodeNoExecutablePlan, requirement, "", "workflow %s is not in the release", id)
		v.result(requirement, requirement, SourceStatic, "EXECUTABLE_PLAN", "absent", Blocked)
		return workflow.Program{}, workflow.Plan{}, false
	}
	return wf, wf.Plan(), true
}

// ---------------------------------------------------------------- thresholds

type observation struct {
	expect  intent.Expectation
	blocked string // non-empty: why the case could not be evaluated
}

func (v *run) threshold(r intent.RequirementV2, t intent.ApprovalThreshold) {
	if v.legacy(r.ID) {
		return
	}
	wf, plan, ok := v.workflowOf(r.ID, t.WorkflowID)
	if !ok {
		return
	}
	entity, found := v.in.Program.Entity(t.EntityID)
	var field compiler.Field
	for _, f := range entity.Fields {
		if f.ID == t.FieldID {
			field = f
		}
	}
	if !found || field.ID == "" || field.Type != "money" || plan.EntityID != t.EntityID {
		v.block(CodeNoExecutablePlan, r.ID, "", "%s on %s is not a money field of a workflow entity", t.FieldID, t.EntityID)
		v.result(r.ID, r.ID, SourceStatic, "EXECUTABLE_PLAN", "absent", Blocked)
		return
	}
	probes, err := intent.DeriveProbes(r.ID, t)
	if err != nil {
		v.block(CodeUnsupportedByVerifier, r.ID, "", "cannot derive probes")
		return
	}
	known := map[string]bool{}
	for _, f := range entity.Fields {
		known[f.ID] = true
	}
	type item struct {
		id, source string
		values     map[string]json.RawMessage
		want       intent.Expectation
		optional   bool // a mined variant: extra coverage that is skipped, never blocking, when it cannot be evaluated
	}
	var items []item
	mined := v.minedValues(plan, t)
	for _, p := range probes {
		values := map[string]json.RawMessage{}
		for id, raw := range t.Context {
			values[id] = raw
		}
		money, _ := json.Marshal(struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		}{p.Amount, p.Currency})
		values[t.FieldID] = money
		items = append(items, item{p.ID, SourceProbe, values, p.Expect, false})
		// Optional fields the requirement does not pin are "any value": repeat the
		// boundary probes with the values the route's own conditions compare against
		// (independent review F4/F5: an exemption such as note != "vip" or an extra
		// OR branch is invisible in the all-null world).
		if !minedProbe(p.ID) {
			continue
		}
		for _, m := range mined {
			variant := map[string]json.RawMessage{}
			for id, raw := range values {
				variant[id] = raw
			}
			variant[m.field] = m.raw
			items = append(items, item{p.ID + "/" + m.label, SourceProbe, variant, p.Expect, true})
		}
	}
	humans := []intent.AcceptanceCase{}
	for _, c := range v.in.Acceptance.Cases {
		if c.RequirementID == r.ID {
			humans = append(humans, c)
		}
	}
	sort.Slice(humans, func(i, j int) bool { return humans[i].ID < humans[j].ID })
	for _, c := range humans {
		values := map[string]json.RawMessage{}
		for id, raw := range t.Context {
			values[id] = raw
		}
		for id, raw := range c.Given {
			values[id] = raw
		}
		items = append(items, item{c.ID, SourceCase, values, c.Expect, false})
	}
	for _, it := range items {
		if v.overflow {
			return
		}
		observed, why := v.observe(wf, plan, field, t, it.values, known, it.optional)
		switch {
		case why != "" && it.optional:
			continue
		case why != "":
			v.block(CodeIncompleteCaseInput, r.ID, it.id, "%s", why)
			v.result(it.id, r.ID, it.source, it.want.String(), "not evaluated", Blocked)
		case !observed.Equal(it.want):
			v.fail(CodeApprovalMismatch, r.ID, it.id, "expected %s but the release produces %s", it.want, observed)
			v.result(it.id, r.ID, it.source, it.want.String(), observed.String(), Fail)
		default:
			v.result(it.id, r.ID, it.source, it.want.String(), observed.String(), Pass)
		}
	}
}

// observe runs one input through the workflow exactly as the runtime would: the
// money input is validated against the IR field, then the plan is walked from
// its initial state with Program.Next until an approval state or a terminal.
func (v *run) observe(wf workflow.Program, plan workflow.Plan, field compiler.Field, t intent.ApprovalThreshold, values map[string]json.RawMessage, known map[string]bool, optional bool) (intent.Expectation, string) {
	for id := range values {
		if !known[id] {
			return intent.Expectation{}, fmt.Sprintf("value for %s names a field that the entity does not have", id)
		}
	}
	amount, currency, ok := intent.Money(values[t.FieldID])
	if !ok {
		return intent.Expectation{}, "the threshold field needs a money value"
	}
	if currency != field.Currency {
		return intent.Expectation{Outcome: intent.OutcomeInputRejected, Reason: intent.ReasonCurrency}, ""
	}
	if (erp.Money{Amount: amount, Currency: erp.Currency(currency)}).Validate() != nil {
		return intent.Expectation{Outcome: intent.OutcomeInputRejected, Reason: intent.ReasonType}, ""
	}
	states := map[string]workflow.State{}
	for _, s := range plan.States {
		states[s.ID] = s
	}
	current := plan.InitialState
	for steps := 0; steps <= len(plan.States); steps++ {
		v.work++
		if v.work > MaxWork {
			v.overflow = true
			return intent.Expectation{}, "work budget exceeded"
		}
		s, exists := states[current]
		if !exists {
			return intent.Expectation{}, "the plan names a state that does not exist"
		}
		switch s.Type {
		case "approval":
			if v.rejectionCompletes(states, s) {
				// The approval decides nothing: a rejection ends in the same successful
				// completion (independent review F2).
				return intent.Expectation{Outcome: intent.OutcomeNoApproval, Reason: intent.ReasonRejectionCompletes}, ""
			}
			return intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: append([]string{}, s.ApprovalRoles...)}, ""
		case "complete", "failed":
			if s.Type == "failed" && optional {
				// A mined world that ends the order in failure completes nothing; it
				// is neither a bypass nor evidence of one.
				return intent.Expectation{}, "the variant ends in a failed state"
			}
			return intent.Expectation{Outcome: intent.OutcomeNoApproval}, ""
		}
		next, err := wf.Next(context.Background(), current, "always", values)
		if err != nil {
			return intent.Expectation{}, fmt.Sprintf("state %s cannot be evaluated with the given values: add the fields its conditions read to the requirement context", current)
		}
		current = next
	}
	return intent.Expectation{}, "the plan did not terminate"
}

// rejectionCompletes reports whether a rejected approval can still end in a
// successful completion. The walk ignores conditions (it over-approximates), so
// it fails closed; an exhausted work budget also answers true.
func (v *run) rejectionCompletes(states map[string]workflow.State, approval workflow.State) bool {
	seen := map[string]bool{}
	var walk func(id string) bool
	walk = func(id string) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		v.work++
		if v.work > MaxWork {
			v.overflow = true
			return true
		}
		s, ok := states[id]
		if !ok {
			return true
		}
		switch s.Type {
		case "complete":
			return true
		case "failed":
			return false
		}
		for _, t := range s.Transitions {
			if walk(t.To) {
				return true
			}
		}
		return false
	}
	for _, t := range approval.Transitions {
		if t.On == "rejected" && walk(t.To) {
			return true
		}
	}
	return false
}

// minedValue is one extra value for an optional field the requirement does not pin.
type minedValue struct {
	field, label string
	raw          json.RawMessage
}

const (
	maxMinedFields   = 8
	maxMinedPerField = 6
	minedOtherString = "zz-other"
)

// minedProbe selects the probes repeated for every mined value: the boundary and
// one decade each way, which is where an exemption or a second branch shows.
func minedProbe(id string) bool {
	for _, kind := range []string{"below", "at", "above", "plus1e6", "minus1e6"} {
		if len(id) > len(kind) && id[len(id)-len(kind)-1:] == "/"+kind {
			return true
		}
	}
	return false
}

// minedValues collects, for every field a workflow condition reads other than the
// threshold field and the fields the requirement pins, the values that condition
// itself compares against (string, enum, boolean and integer literals) plus one
// value that matches none of them. The EXPECTED outcome of a mined variant stays
// the oracle's, which never reads the IR: the requirement says "any value".
func (v *run) minedValues(plan workflow.Plan, t intent.ApprovalThreshold) []minedValue {
	type candidates struct {
		kind string
		set  map[string]json.RawMessage
	}
	fields := map[string]*candidates{}
	order := []string{}
	for _, state := range plan.States {
		for _, tr := range state.Transitions {
			if tr.Condition == nil {
				continue
			}
			cond := tr.Condition
			literals := map[string][]json.RawMessage{}
			var walk func(n rules.Node)
			walk = func(n rules.Node) {
				if (n.Op == "literal" || n.Op == "typed_literal") && len(n.Value) > 0 {
					literals[n.Type.Kind] = append(literals[n.Type.Kind], n.Value)
				}
				for _, a := range n.Args {
					walk(a)
				}
			}
			walk(cond.Root)
			for _, f := range cond.Fields {
				if f.ID == t.FieldID {
					continue
				}
				if _, pinned := t.Context[f.ID]; pinned {
					continue
				}
				kind := f.Type.Kind
				switch kind {
				case "string", "enum", "bool", "integer":
				default:
					continue
				}
				c := fields[f.ID]
				if c == nil {
					c = &candidates{kind: kind, set: map[string]json.RawMessage{}}
					fields[f.ID] = c
					order = append(order, f.ID)
				}
				for _, raw := range literals[kind] {
					c.set[string(raw)] = raw
				}
				if kind == "enum" {
					for _, ev := range f.EnumValues {
						b, _ := json.Marshal(ev)
						c.set[string(b)] = b
					}
				}
				switch kind {
				case "string":
					b, _ := json.Marshal(minedOtherString)
					c.set[string(b)] = b
				case "bool":
					c.set["true"], c.set["false"] = json.RawMessage("true"), json.RawMessage("false")
				case "integer":
					c.set["0"] = json.RawMessage("0")
					for _, raw := range literals["integer"] {
						var n int64
						if json.Unmarshal(raw, &n) == nil {
							for _, d := range []int64{-1, 1} {
								b := []byte(fmt.Sprintf("%d", n+d))
								c.set[string(b)] = json.RawMessage(b)
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(order)
	if len(order) > maxMinedFields {
		order = order[:maxMinedFields]
	}
	var out []minedValue
	for _, id := range order {
		c := fields[id]
		keys := make([]string, 0, len(c.set))
		for k := range c.set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > maxMinedPerField {
			keys = keys[:maxMinedPerField]
		}
		for _, k := range keys {
			out = append(out, minedValue{field: id, label: id + "=" + k, raw: c.set[k]})
		}
	}
	return out
}

// --------------------------------------------------------------------- gates

func (v *run) gate(r intent.RequirementV2, g intent.ApprovalGate) {
	if v.legacy(r.ID) {
		return
	}
	_, plan, ok := v.workflowOf(r.ID, g.WorkflowID)
	if !ok {
		return
	}
	states := map[string]workflow.State{}
	wrongRoles := false
	for _, s := range plan.States {
		states[s.ID] = s
		if s.Type == "approval" && !sameRoles(s.ApprovalRoles, g.ApproverRoles) {
			wrongRoles = true
		}
	}
	// A state is "bypassable" when completion is reachable from it without
	// passing an approval state that has exactly the required roles. The plan is
	// acyclic (validated), so a memoised walk is linear in states + transitions.
	memo := map[string]bool{}
	var bypass func(string) bool
	bypass = func(id string) bool {
		if answer, done := memo[id]; done {
			return answer
		}
		v.work++
		if v.work > MaxWork {
			v.overflow = true
			return true
		}
		s := states[id]
		var answer bool
		switch {
		case s.Type == "approval" && sameRoles(s.ApprovalRoles, g.ApproverRoles):
			// Only the approved edges are gated: a rejection must not complete either.
			for _, t := range s.Transitions {
				if t.On == "rejected" && bypass(t.To) {
					answer = true
					break
				}
			}
		case s.Type == "complete":
			answer = true
		case s.Type == "failed":
			answer = false
		default:
			for _, t := range s.Transitions {
				if bypass(t.To) {
					answer = true
					break
				}
			}
		}
		memo[id] = answer
		return answer
	}
	bypassed := bypass(plan.InitialState)
	switch {
	case v.overflow:
	case bypassed && wrongRoles:
		v.fail(CodeGateRolesMismatch, r.ID, "", "the workflow's approval states do not have exactly the roles %v", g.ApproverRoles)
		v.result(r.ID, r.ID, SourceStatic, "ALL_PATHS_GATED_BY "+fmt.Sprint(g.ApproverRoles), "approval roles differ", Fail)
	case bypassed:
		v.fail(CodeGateBypass, r.ID, "", "a path reaches completion without an approval by %v (or a rejection by them still completes)", g.ApproverRoles)
		v.result(r.ID, r.ID, SourceStatic, "ALL_PATHS_GATED_BY "+fmt.Sprint(g.ApproverRoles), "approval-free path exists", Fail)
	default:
		v.result(r.ID, r.ID, SourceStatic, "ALL_PATHS_GATED_BY "+fmt.Sprint(g.ApproverRoles), "all paths gated", Pass)
	}
}

func sameRoles(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return len(x) == len(y) && func() bool {
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}()
}

// ---------------------------------------------------------------------- tools

func (v *run) toolEffect(r intent.RequirementV2, e intent.ToolEffect) {
	used, bad := 0, false
	for _, plan := range v.ir.Workflows {
		for _, s := range plan.States {
			if !s.IsTool() || s.Activity.ToolRef != e.ToolRef {
				continue
			}
			used++
			id := plan.ID + "/" + s.ID
			def, ok := v.in.Tools.Lookup(s.Activity.ToolRef, s.Activity.Version, s.ToolHash)
			switch {
			case !ok || !def.AllowsTenant(v.in.TenantID):
				v.fail(CodeToolUnresolved, r.ID, id, "pin %s@%s does not resolve for this tenant", s.Activity.ToolRef, s.Activity.Version)
				bad = true
			case intent.SideEffectRank(def.SideEffect) > intent.SideEffectRank(e.MaxSideEffect):
				v.fail(CodeToolEffect, r.ID, id, "side effect %s exceeds %s", def.SideEffect, e.MaxSideEffect)
				bad = true
			case e.RequiresApproval && def.Approval != "REQUIRED":
				v.fail(CodeToolApproval, r.ID, id, "the tool does not require approval")
				bad = true
			case intent.SandboxRank(def.Sandbox.Mode) < 0 || intent.SandboxRank(def.Sandbox.Mode) > intent.SandboxRank(e.MaxSandboxMode):
				v.fail(CodeToolSandbox, r.ID, id, "sandbox mode %s exceeds %s", def.Sandbox.Mode, e.MaxSandboxMode)
				bad = true
			}
		}
	}
	status, observed := Pass, fmt.Sprintf("%d activities within bounds", used)
	if bad {
		status, observed = Fail, "bounds exceeded"
	}
	v.result(r.ID, r.ID, SourceStatic, fmt.Sprintf("<=%s approval=%t sandbox<=%s", e.MaxSideEffect, e.RequiresApproval, e.MaxSandboxMode), observed, status)
}

// ----------------------------------------------------------------- migrations

func (v *run) migration(r intent.RequirementV2, m intent.DataMigration) {
	allowed := false
	switch v.in.StorageClass {
	case "EQUAL", "ADDITIVE":
		allowed = true
	case "TRANSFORM":
		allowed = m.MaxClass == "TRANSFORM"
	}
	if !allowed {
		v.fail(CodeMigrationExceeded, r.ID, "", "storage evolution %s exceeds the allowed %s", v.in.StorageClass, m.MaxClass)
		v.result(r.ID, r.ID, SourceStatic, "<="+m.MaxClass, v.in.StorageClass, Fail)
		return
	}
	v.result(r.ID, r.ID, SourceStatic, "<="+m.MaxClass, v.in.StorageClass, Pass)
}

// --------------------------------------------------------------- completeness

// CheckCompleteness requires every changed financial rule, approval gate, tool
// activity and non-equal storage evolution to be covered by a RESOLVED
// requirement of the matching category. A release that changes money or approval
// behavior without a stated requirement is BLOCKED, not silently accepted.
func CheckCompleteness(changed ChangedSurfaces, set intent.RequirementSet) []Finding {
	findings := []Finding{}
	missing := func(kind, subject string) {
		findings = append(findings, Finding{Code: CodeOmittedRequirement, Severity: Blocked, Detail: fmt.Sprintf("no resolved requirement covers the changed %s %s", kind, subject)})
	}
	resolved := func(category string, match func(r intent.RequirementV2) bool) bool {
		for _, r := range set.Requirements {
			if r.Category == category && r.Status == intent.StatusResolved && match(r) {
				return true
			}
		}
		return false
	}
	for _, entity := range uniqueSorted(changed.FinancialRuleEntities) {
		if !resolved(intent.CategoryApprovalThreshold, func(r intent.RequirementV2) bool {
			t, err := r.Threshold()
			return err == nil && t.EntityID == entity
		}) {
			missing("financial rule on", entity)
		}
	}
	for _, id := range uniqueSorted(changed.ApprovalGateWorkflows) {
		covered := resolved(intent.CategoryApprovalThreshold, func(r intent.RequirementV2) bool {
			t, err := r.Threshold()
			return err == nil && t.WorkflowID == id
		}) || resolved(intent.CategoryApprovalGate, func(r intent.RequirementV2) bool {
			g, err := r.Gate()
			return err == nil && g.WorkflowID == id
		})
		if !covered {
			missing("approval gate of", id)
		}
	}
	for _, ref := range uniqueSorted(changed.ToolRefs) {
		if !resolved(intent.CategoryToolEffect, func(r intent.RequirementV2) bool {
			t, err := r.Tool()
			return err == nil && t.ToolRef == ref
		}) {
			missing("tool activity of", ref)
		}
	}
	if changed.NonEqualStorage && !resolved(intent.CategoryDataMigration, func(intent.RequirementV2) bool { return true }) {
		missing("storage evolution", "(data layout)")
	}
	return findings
}

func uniqueSorted(in []string) []string {
	set := map[string]bool{}
	for _, s := range in {
		set[s] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
