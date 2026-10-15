package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
)

// DiffVersion identifies the diff contract. A change to the aspect vocabulary or
// the comparison rules needs a new version, never an edit (ADR038, appendix B).
const DiffVersion = "diff-v1"

// MaxDiffDefinitions bounds either side of a diff; larger programs fail closed.
const MaxDiffDefinitions = 5000

var ErrDiff = errors.New("unsupported or invalid release diff")

// Change kinds.
const (
	ChangeAdded    = "ADDED"
	ChangeRemoved  = "REMOVED"
	ChangeChanged  = "CHANGED"
	ChangeRepinned = "REPINNED"
)

// Aspect vocabulary. UNCLASSIFIED means the diff could not explain a change; the
// risk policy rates it HIGH rather than guessing.
const (
	AspectLabel           = "LABEL"
	AspectStorage         = "STORAGE"
	AspectRuleLogic       = "RULE_LOGIC"
	AspectRuleFinancial   = "RULE_FINANCIAL"
	AspectWorkflowGraph   = "WORKFLOW_GRAPH"
	AspectWorkflowTrigger = "WORKFLOW_TRIGGER"
	AspectApprovalGate    = "APPROVAL_GATE"
	AspectToolPin         = "TOOL_PIN"
	AspectToolDeclaration = "TOOL_DECLARATION"
	AspectPage            = "PAGE"
	AspectRelationship    = "RELATIONSHIP"
	AspectPlanOnly        = "PLAN_ONLY"
	AspectUnclassified    = "UNCLASSIFIED"
)

var aspectVocabulary = []string{AspectLabel, AspectStorage, AspectRuleLogic, AspectRuleFinancial, AspectWorkflowGraph, AspectWorkflowTrigger, AspectApprovalGate, AspectToolPin, AspectToolDeclaration, AspectPage, AspectRelationship, AspectPlanOnly, AspectUnclassified}

// DefinitionChange is one authored definition that differs. For ADDED and
// REMOVED the aspects describe what the definition is; for CHANGED they describe
// what differs. Tools lists the sorted ref@version tool pins of a workflow on
// either side so a risk policy can let a workflow change inherit its tool's level.
type DefinitionChange struct {
	Kind       string   `json:"kind"`
	ID         string   `json:"id"`
	EntityRef  string   `json:"entity_ref"`
	Change     string   `json:"change"`
	Aspects    []string `json:"aspects"`
	BaseHash   string   `json:"base_hash"`
	TargetHash string   `json:"target_hash"`
	Tools      []string `json:"tools"`
}

// ToolActivityChange is a governed tool activity added, removed or re-pinned.
type ToolActivityChange struct {
	WorkflowID string `json:"workflow_id"`
	StateID    string `json:"state_id"`
	Ref        string `json:"ref"`
	Version    string `json:"version"`
	BaseHash   string `json:"base_hash"`
	TargetHash string `json:"target_hash"`
	Change     string `json:"change"`
}

// ApprovalGateChange is an approval state added, removed or changed in roles,
// timeout or outgoing transitions.
type ApprovalGateChange struct {
	WorkflowID         string   `json:"workflow_id"`
	StateID            string   `json:"state_id"`
	Change             string   `json:"change"`
	BaseRoles          []string `json:"base_roles"`
	TargetRoles        []string `json:"target_roles"`
	TimeoutChanged     bool     `json:"timeout_changed"`
	TransitionsChanged bool     `json:"transitions_changed"`
}

// MoneyConditionChange is a workflow transition whose condition reads a
// money-typed field (or literal) and differs between the releases, or exists on
// only one side. Such a transition decides which records need approval even when
// its own state is not an approval state.
type MoneyConditionChange struct {
	WorkflowID string `json:"workflow_id"`
	StateID    string `json:"state_id"`
	Index      int    `json:"index"`
	Change     string `json:"change"`
}

// TriggerChange records a workflow trigger that differs; From is empty for an
// added workflow and To for a removed one.
type TriggerChange struct {
	WorkflowID string `json:"workflow_id"`
	From       string `json:"from"`
	To         string `json:"to"`
}

// ToolDeclarationChange describes an authored kind:"tool" definition. Side
// effect is none|read|write|destructive|financial; approval none|user|elevated.
type ToolDeclarationChange struct {
	ID                string   `json:"id"`
	Change            string   `json:"change"`
	BaseSideEffect    string   `json:"base_side_effect"`
	TargetSideEffect  string   `json:"target_side_effect"`
	BaseApproval      string   `json:"base_approval"`
	TargetApproval    string   `json:"target_approval"`
	BaseSandboxMode   string   `json:"base_sandbox_mode"`
	TargetSandboxMode string   `json:"target_sandbox_mode"`
	GainedPermissions []string `json:"gained_permissions"`
}

// PlanChange is a compiled rule or workflow plan whose canonical hash differs.
type PlanChange struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	BaseHash   string `json:"base_hash"`
	TargetHash string `json:"target_hash"`
}

// Diff is the deterministic difference between two compiled programs of one
// application. It is a pure function of the two IR byte strings.
type Diff struct {
	Version          string                  `json:"version"`
	BaseIRHash       string                  `json:"base_ir_hash"`
	TargetIRHash     string                  `json:"target_ir_hash"`
	FirstRelease     bool                    `json:"first_release"`
	IRUpgrade        bool                    `json:"ir_upgrade"`
	Changes          []DefinitionChange      `json:"changes"`
	ToolActivities   []ToolActivityChange    `json:"tool_activities"`
	ApprovalGates    []ApprovalGateChange    `json:"approval_gates"`
	MoneyConditions  []MoneyConditionChange  `json:"money_conditions"`
	TriggerChanges   []TriggerChange         `json:"trigger_changes"`
	ToolDeclarations []ToolDeclarationChange `json:"tool_declarations"`
	PlanChanges      []PlanChange            `json:"plan_changes"`
}

// JSON is the canonical encoding that Hash covers.
func (d Diff) JSON() []byte { raw, _ := json.Marshal(d); return raw }

// Hash is the hex SHA-256 of JSON.
func (d Diff) Hash() string { return digest(d.JSON()) }

// ParseDiff restores stored canonical bytes. It never recomputes the diff: that
// needs both programs (see ComputeDiff); it only proves the bytes are canonical,
// within the closed vocabularies and bound to the expected hash.
func ParseDiff(raw []byte, expectedHash string) (Diff, error) {
	if len(raw) == 0 || len(raw) > 8<<20 || digest(raw) != expectedHash {
		return Diff{}, ErrDiff
	}
	var d Diff
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || dec.More() || d.Version != DiffVersion {
		return Diff{}, ErrDiff
	}
	if canonical, err := json.Marshal(d); err != nil || !bytes.Equal(canonical, raw) {
		return Diff{}, ErrDiff
	}
	for _, c := range d.Changes {
		if c.Kind == "" || c.ID == "" || (c.Change != ChangeAdded && c.Change != ChangeRemoved && c.Change != ChangeChanged) {
			return Diff{}, ErrDiff
		}
		for _, a := range c.Aspects {
			if !contains(aspectVocabulary, a) {
				return Diff{}, ErrDiff
			}
		}
	}
	return d, nil
}

// sorted returns a sorted copy with no nil slice.
func sorted(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func setOf(in []string) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, s := range in {
		out[s] = true
	}
	return out
}

func aspectSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// normalized strips the envelope version (which equals the IR schema version and
// would otherwise mark every definition CHANGED on an upgrade).
func normalized(d metadata.Definition) metadata.Definition {
	d.SchemaVersion = ""
	return d
}

func defHash(d metadata.Definition) string { return metadata.DefinitionHash(normalized(d)) }

func unlabelled(d metadata.Definition) metadata.Definition {
	d = normalized(d)
	d.Label = ""
	return d
}

func indexDefinitions(doc metadata.Document) (map[string]metadata.Definition, error) {
	if len(doc.Definitions) > MaxDiffDefinitions {
		return nil, ErrDiff
	}
	out := make(map[string]metadata.Definition, len(doc.Definitions))
	for _, d := range doc.Definitions {
		if d.ID == "" {
			return nil, ErrDiff
		}
		if _, dup := out[d.ID]; dup {
			return nil, ErrDiff
		}
		out[d.ID] = d
	}
	return out, nil
}

func fieldTypesOf(doc metadata.Document) map[string]string {
	out := map[string]string{}
	for _, d := range doc.Definitions {
		if d.Field != nil {
			out[d.ID] = d.Field.Type
		}
	}
	return out
}

// exprMoney reports whether an authored expression is money typed or reads a
// money-typed field. It works on authored source so it also covers legacy IR
// whose rules have no compiled plan.
func exprMoney(e metadata.Expression, types map[string]string) bool {
	if e.ValueType == "money" || (e.FieldRef != "" && types[e.FieldRef] == "money") {
		return true
	}
	for _, a := range e.Args {
		if exprMoney(a, types) {
			return true
		}
	}
	return false
}

func ruleFinancial(r metadata.Rule, types map[string]string) bool {
	return r.ResultType == "money" || exprMoney(r.Expression, types)
}

type toolPin struct{ Ref, Version, Hash string }

type gateView struct {
	Roles       []string
	TimeoutMS   int
	Transitions string
}

type workflowView struct {
	def     *metadata.Workflow
	pins    map[string]toolPin
	gates   map[string]gateView
	money   map[string]string // "state|index" -> canonical transition JSON, money conditions only
	hasGate bool
}

func transitionJSON(t metadata.Transition) string {
	raw, _ := json.Marshal(t)
	return string(raw)
}

func viewOf(d metadata.Definition, plans map[string]workflow.Plan, types map[string]string) workflowView {
	v := workflowView{def: d.Workflow, pins: map[string]toolPin{}, gates: map[string]gateView{}, money: map[string]string{}}
	if d.Workflow == nil {
		return v
	}
	for _, s := range plans[d.ID].States {
		if s.IsTool() {
			v.pins[s.ID] = toolPin{s.Activity.ToolRef, s.Activity.Version, s.ToolHash}
		}
	}
	for _, s := range d.Workflow.States {
		if s.Type == "approval" {
			transitions := make([]string, 0, len(s.Transitions))
			for _, t := range s.Transitions {
				transitions = append(transitions, transitionJSON(t))
			}
			v.gates[s.ID] = gateView{sorted(s.ApprovalRoles), s.ApprovalTimeoutMS, fmt.Sprint(transitions)}
			v.hasGate = true
		}
		for i, t := range s.Transitions {
			if t.Condition != nil && exprMoney(*t.Condition, types) {
				v.money[s.ID+"|"+strconv.Itoa(i)] = transitionJSON(t)
				v.hasGate = true
			}
		}
	}
	return v
}

func (v workflowView) toolRefs() []string {
	set := map[string]bool{}
	for _, p := range v.pins {
		set[p.Ref+"@"+p.Version] = true
	}
	return aspectSet(set)
}

// ComputeDiff compares two restored or compiled programs of one application. A
// zero base is the first release: every target definition is ADDED. The result
// is deterministic and bounded; callers hash it into a candidate and the risk
// reducer consumes it.
func ComputeDiff(base, target Program) (Diff, error) {
	if !target.Valid() {
		return Diff{}, ErrDiff
	}
	t := target.IR()
	var b IR
	first := !base.Valid()
	if !first {
		b = base.IR()
		if b.Application.AppID != t.Application.AppID {
			return Diff{}, ErrDiff
		}
	}
	bd, err := indexDefinitions(b.Application)
	if err != nil {
		return Diff{}, err
	}
	td, err := indexDefinitions(t.Application)
	if err != nil {
		return Diff{}, err
	}
	bTypes, tTypes := fieldTypesOf(b.Application), fieldTypesOf(t.Application)
	bPlans, tPlans := workflowPlans(b), workflowPlans(t)
	bRules, tRules := rulePlanHashes(b), rulePlanHashes(t)

	d := Diff{Version: DiffVersion, TargetIRHash: target.Hash(), FirstRelease: first,
		Changes: []DefinitionChange{}, ToolActivities: []ToolActivityChange{}, ApprovalGates: []ApprovalGateChange{}, MoneyConditions: []MoneyConditionChange{},
		TriggerChanges: []TriggerChange{}, ToolDeclarations: []ToolDeclarationChange{}, PlanChanges: []PlanChange{}}
	if !first {
		d.BaseIRHash = base.Hash()
		d.IRUpgrade = b.IRVersion != t.IRVersion || b.CompilerVersion != t.CompilerVersion || b.SchemaSnapshot != t.SchemaSnapshot || b.CatalogHash != t.CatalogHash
	}

	ids := map[string]bool{}
	for id := range bd {
		ids[id] = true
	}
	for id := range td {
		ids[id] = true
	}
	order := make([]string, 0, len(ids))
	for id := range ids {
		order = append(order, id)
	}
	sort.Strings(order)

	for _, id := range order {
		old, hadOld := bd[id]
		now, hasNew := td[id]
		oldView, newView := viewOf(old, bPlans, bTypes), viewOf(now, tPlans, tTypes)
		var change DefinitionChange
		switch {
		case !hadOld:
			change = DefinitionChange{Kind: now.Kind, ID: id, Change: ChangeAdded, TargetHash: defHash(now)}
			change.Aspects = describe(now, tTypes, tPlans)
		case !hasNew:
			change = DefinitionChange{Kind: old.Kind, ID: id, Change: ChangeRemoved, BaseHash: defHash(old)}
			change.Aspects = describe(old, bTypes, bPlans)
		default:
			oh, nh := defHash(old), defHash(now)
			if oh == nh {
				if planHash(old, bRules, bPlans) == planHash(now, tRules, tPlans) {
					continue
				}
				// Identical authoring, different compiled plan: a platform or
				// tool-pin change rather than an authored one.
				change = DefinitionChange{Kind: now.Kind, ID: id, Change: ChangeChanged, BaseHash: oh, TargetHash: nh, Aspects: []string{AspectPlanOnly}}
				if !reflect.DeepEqual(oldView.pins, newView.pins) {
					change.Aspects = []string{AspectPlanOnly, AspectToolPin}
				}
			} else {
				change = DefinitionChange{Kind: now.Kind, ID: id, Change: ChangeChanged, BaseHash: oh, TargetHash: nh}
				change.Aspects = contrast(old, now, bTypes, tTypes)
				if !reflect.DeepEqual(oldView.pins, newView.pins) && !contains(change.Aspects, AspectToolPin) {
					change.Aspects = aspectSet(setOf(append(change.Aspects, AspectToolPin)))
				}
			}
		}
		if old.Workflow != nil || now.Workflow != nil {
			change.Tools = unionStrings(oldView.toolRefs(), newView.toolRefs())
			d.workflowDetails(id, hadOld && old.Workflow != nil, hasNew && now.Workflow != nil, oldView, newView)
		}
		if old.Tool != nil || now.Tool != nil {
			d.ToolDeclarations = append(d.ToolDeclarations, declarationChange(id, hadOld, hasNew, old, now))
		}
		if hasNew {
			change.EntityRef = entityRefOf(now)
		} else {
			change.EntityRef = entityRefOf(old)
		}
		if change.Aspects == nil {
			change.Aspects = []string{}
		}
		if change.Tools == nil {
			change.Tools = []string{}
		}
		d.Changes = append(d.Changes, change)
	}

	for _, p := range planUnion("rule", bRules, tRules) {
		d.PlanChanges = append(d.PlanChanges, p)
	}
	bWorkflowHashes, tWorkflowHashes := map[string]string{}, map[string]string{}
	for id, p := range bPlans {
		raw, _ := json.Marshal(p)
		bWorkflowHashes[id] = digest(raw)
	}
	for id, p := range tPlans {
		raw, _ := json.Marshal(p)
		tWorkflowHashes[id] = digest(raw)
	}
	d.PlanChanges = append(d.PlanChanges, planUnion("workflow", bWorkflowHashes, tWorkflowHashes)...)
	sort.Slice(d.PlanChanges, func(i, j int) bool {
		if d.PlanChanges[i].Kind != d.PlanChanges[j].Kind {
			return d.PlanChanges[i].Kind < d.PlanChanges[j].Kind
		}
		return d.PlanChanges[i].ID < d.PlanChanges[j].ID
	})
	sort.Slice(d.ToolActivities, func(i, j int) bool {
		a, c := d.ToolActivities[i], d.ToolActivities[j]
		return a.WorkflowID+"|"+a.StateID < c.WorkflowID+"|"+c.StateID
	})
	sort.Slice(d.ApprovalGates, func(i, j int) bool {
		a, c := d.ApprovalGates[i], d.ApprovalGates[j]
		return a.WorkflowID+"|"+a.StateID < c.WorkflowID+"|"+c.StateID
	})
	sort.Slice(d.MoneyConditions, func(i, j int) bool {
		a, c := d.MoneyConditions[i], d.MoneyConditions[j]
		if a.WorkflowID != c.WorkflowID {
			return a.WorkflowID < c.WorkflowID
		}
		if a.StateID != c.StateID {
			return a.StateID < c.StateID
		}
		return a.Index < c.Index
	})
	sort.Slice(d.TriggerChanges, func(i, j int) bool { return d.TriggerChanges[i].WorkflowID < d.TriggerChanges[j].WorkflowID })
	sort.Slice(d.ToolDeclarations, func(i, j int) bool { return d.ToolDeclarations[i].ID < d.ToolDeclarations[j].ID })
	return d, nil
}

// entityRefOf names the entity a definition belongs to, so a risk reducer and
// the verifier can tell which entity a rule, workflow or page change concerns.
func entityRefOf(d metadata.Definition) string {
	switch {
	case d.Entity != nil:
		return d.ID
	case d.Field != nil:
		return d.Field.EntityRef
	case d.Page != nil:
		return d.Page.EntityRef
	case d.Rule != nil:
		return d.Rule.EntityRef
	case d.Workflow != nil:
		return d.Workflow.EntityRef
	case d.Relationship != nil:
		return d.Relationship.FromEntity
	}
	return ""
}

func workflowPlans(ir IR) map[string]workflow.Plan {
	out := make(map[string]workflow.Plan, len(ir.Workflows))
	for _, p := range ir.Workflows {
		out[p.ID] = p
	}
	return out
}

func rulePlanHashes(ir IR) map[string]string {
	out := make(map[string]string, len(ir.Rules))
	for _, p := range ir.Rules {
		raw, _ := json.Marshal(p)
		out[p.ID] = digest(raw)
	}
	return out
}

func planHash(d metadata.Definition, rulePlans map[string]string, workflowPlans map[string]workflow.Plan) string {
	switch {
	case d.Rule != nil:
		return rulePlans[d.ID]
	case d.Workflow != nil:
		raw, _ := json.Marshal(workflowPlans[d.ID])
		return digest(raw)
	}
	return ""
}

func planUnion(kind string, base, target map[string]string) []PlanChange {
	ids := map[string]bool{}
	for id := range base {
		ids[id] = true
	}
	for id := range target {
		ids[id] = true
	}
	out := []PlanChange{}
	for id := range ids {
		if base[id] != target[id] {
			out = append(out, PlanChange{Kind: kind, ID: id, BaseHash: base[id], TargetHash: target[id]})
		}
	}
	return out
}

func unionStrings(a, b []string) []string {
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		set[s] = true
	}
	return aspectSet(set)
}

// contentKind names the populated payload of a definition. The reflection test
// proves every payload pointer of metadata.Definition is handled here.
var payloadHandlers = map[string]bool{"Entity": true, "Field": true, "Relationship": true, "Page": true, "Rule": true, "Workflow": true, "Tool": true}

func payloadOf(d metadata.Definition) (string, bool) {
	n, name := 0, ""
	for _, p := range []struct {
		name string
		set  bool
	}{{"Entity", d.Entity != nil}, {"Field", d.Field != nil}, {"Relationship", d.Relationship != nil}, {"Page", d.Page != nil}, {"Rule", d.Rule != nil}, {"Workflow", d.Workflow != nil}, {"Tool", d.Tool != nil}} {
		if p.set {
			n, name = n+1, p.name
		}
	}
	return name, n == 1
}

// describe lists the aspects of a definition that exists on one side only.
func describe(d metadata.Definition, types map[string]string, plans map[string]workflow.Plan) []string {
	set := map[string]bool{}
	name, ok := payloadOf(d)
	if !ok {
		set[AspectUnclassified] = true
	}
	switch name {
	case "Entity", "Field":
		set[AspectStorage] = true
	case "Relationship":
		set[AspectRelationship] = true
	case "Page":
		set[AspectPage] = true
	case "Rule":
		set[AspectRuleLogic] = true
		if ruleFinancial(*d.Rule, types) {
			set[AspectRuleFinancial] = true
		}
	case "Workflow":
		set[AspectWorkflowGraph] = true
		if d.Workflow.Trigger != "manual" {
			set[AspectWorkflowTrigger] = true
		}
		v := viewOf(d, plans, types)
		if v.hasGate {
			set[AspectApprovalGate] = true
		}
		if len(v.pins) > 0 {
			set[AspectToolPin] = true
		}
	case "Tool":
		set[AspectToolDeclaration] = true
	}
	return aspectSet(set)
}

// contrast lists what differs between two versions of one definition.
func contrast(old, now metadata.Definition, oldTypes, newTypes map[string]string) []string {
	set := map[string]bool{}
	if old.Label != now.Label {
		set[AspectLabel] = true
	}
	oldName, oldOK := payloadOf(old)
	newName, newOK := payloadOf(now)
	if !oldOK || !newOK || oldName != newName || old.Kind != now.Kind || old.Namespace != now.Namespace {
		set[AspectUnclassified] = true
		return aspectSet(set)
	}
	if reflect.DeepEqual(unlabelled(old), unlabelled(now)) {
		return aspectSet(set) // label only
	}
	explained := false
	mark := func(a string) { set[a], explained = true, true }
	switch newName {
	case "Entity":
		if !reflect.DeepEqual(sorted(old.Entity.Fields), sorted(now.Entity.Fields)) {
			mark(AspectStorage)
		}
	case "Field":
		o, n := *old.Field, *now.Field
		o.EnumValues, n.EnumValues = sorted(o.EnumValues), sorted(n.EnumValues)
		if !reflect.DeepEqual(o, n) {
			mark(AspectStorage)
		}
	case "Relationship":
		if !reflect.DeepEqual(old.Relationship, now.Relationship) {
			mark(AspectRelationship)
		}
	case "Page":
		if !reflect.DeepEqual(old.Page, now.Page) {
			mark(AspectPage)
		}
	case "Rule":
		if !reflect.DeepEqual(old.Rule, now.Rule) {
			mark(AspectRuleLogic)
			if ruleFinancial(*old.Rule, oldTypes) || ruleFinancial(*now.Rule, newTypes) {
				mark(AspectRuleFinancial)
			}
		}
	case "Workflow":
		o, n := old.Workflow, now.Workflow
		if o.Trigger != n.Trigger {
			mark(AspectWorkflowTrigger)
		}
		graph := !reflect.DeepEqual(o, n)
		if graph {
			mark(AspectWorkflowGraph)
		}
		ov := viewOf(old, nil, oldTypes)
		nv := viewOf(now, nil, newTypes)
		// Any graph change in a workflow that has an approval gate or a
		// money-reading route can change which records reach (or skip) the
		// gate, even when the gate state itself is untouched.
		if !reflect.DeepEqual(ov.gates, nv.gates) || !reflect.DeepEqual(ov.money, nv.money) || graph && (ov.hasGate || nv.hasGate) {
			mark(AspectApprovalGate)
		}
		if toolStatesDiffer(o.States, n.States) {
			mark(AspectToolPin)
		}
	case "Tool":
		if !reflect.DeepEqual(old.Tool, now.Tool) {
			mark(AspectToolDeclaration)
		}
	}
	if !explained {
		set[AspectUnclassified] = true
	}
	return aspectSet(set)
}

// toolStatesDiffer compares the authored tool references of two state lists; the
// pinned hash comparison happens separately through the compiled plans.
func toolStatesDiffer(a, b []metadata.WorkflowState) bool {
	pins := func(states []metadata.WorkflowState) map[string]string {
		out := map[string]string{}
		for _, s := range states {
			if s.Activity != nil && s.Activity.ToolRef != "" {
				out[s.ID] = s.Activity.ToolRef + "@" + s.Activity.Version
			}
		}
		return out
	}
	return !reflect.DeepEqual(pins(a), pins(b))
}

func (d *Diff) workflowDetails(id string, hadOld, hasNew bool, oldView, newView workflowView) {
	// Triggers.
	from, to := "", ""
	if hadOld {
		from = oldView.def.Trigger
	}
	if hasNew {
		to = newView.def.Trigger
	}
	if from != to {
		d.TriggerChanges = append(d.TriggerChanges, TriggerChange{WorkflowID: id, From: from, To: to})
	}
	// Tool activities, matched by state id.
	states := map[string]bool{}
	for s := range oldView.pins {
		states[s] = true
	}
	for s := range newView.pins {
		states[s] = true
	}
	for s := range states {
		o, hadPin := oldView.pins[s]
		n, hasPin := newView.pins[s]
		switch {
		case !hadPin:
			d.ToolActivities = append(d.ToolActivities, ToolActivityChange{WorkflowID: id, StateID: s, Ref: n.Ref, Version: n.Version, TargetHash: n.Hash, Change: ChangeAdded})
		case !hasPin:
			d.ToolActivities = append(d.ToolActivities, ToolActivityChange{WorkflowID: id, StateID: s, Ref: o.Ref, Version: o.Version, BaseHash: o.Hash, Change: ChangeRemoved})
		case o != n:
			d.ToolActivities = append(d.ToolActivities, ToolActivityChange{WorkflowID: id, StateID: s, Ref: n.Ref, Version: n.Version, BaseHash: o.Hash, TargetHash: n.Hash, Change: ChangeRepinned})
		}
	}
	// Approval gates, matched by state id.
	gates := map[string]bool{}
	for s := range oldView.gates {
		gates[s] = true
	}
	for s := range newView.gates {
		gates[s] = true
	}
	for s := range gates {
		o, hadGate := oldView.gates[s]
		n, hasGate := newView.gates[s]
		switch {
		case !hadGate:
			d.ApprovalGates = append(d.ApprovalGates, ApprovalGateChange{WorkflowID: id, StateID: s, Change: ChangeAdded, BaseRoles: []string{}, TargetRoles: n.Roles})
		case !hasGate:
			d.ApprovalGates = append(d.ApprovalGates, ApprovalGateChange{WorkflowID: id, StateID: s, Change: ChangeRemoved, BaseRoles: o.Roles, TargetRoles: []string{}})
		case !reflect.DeepEqual(o, n):
			d.ApprovalGates = append(d.ApprovalGates, ApprovalGateChange{WorkflowID: id, StateID: s, Change: ChangeChanged, BaseRoles: o.Roles, TargetRoles: n.Roles,
				TimeoutChanged: o.TimeoutMS != n.TimeoutMS, TransitionsChanged: o.Transitions != n.Transitions})
		}
	}
	// Money-reading conditions, matched by (state, transition index).
	keys := map[string]bool{}
	for k := range oldView.money {
		keys[k] = true
	}
	for k := range newView.money {
		keys[k] = true
	}
	for k := range keys {
		o, hadCond := oldView.money[k]
		n, hasCond := newView.money[k]
		if hadCond && hasCond && o == n {
			continue
		}
		change := ChangeChanged
		if !hadCond {
			change = ChangeAdded
		} else if !hasCond {
			change = ChangeRemoved
		}
		state, index := splitKey(k)
		d.MoneyConditions = append(d.MoneyConditions, MoneyConditionChange{WorkflowID: id, StateID: state, Index: index, Change: change})
	}
}

func splitKey(k string) (string, int) {
	for i := len(k) - 1; i >= 0; i-- {
		if k[i] == '|' {
			n, _ := strconv.Atoi(k[i+1:])
			return k[:i], n
		}
	}
	return k, 0
}

func declarationChange(id string, hadOld, hasNew bool, old, now metadata.Definition) ToolDeclarationChange {
	c := ToolDeclarationChange{ID: id, GainedPermissions: []string{}}
	switch {
	case !hadOld:
		c.Change = ChangeAdded
	case !hasNew:
		c.Change = ChangeRemoved
	default:
		c.Change = ChangeChanged
	}
	if hadOld && old.Tool != nil {
		c.BaseSideEffect, c.BaseApproval, c.BaseSandboxMode = old.Tool.SideEffect, old.Tool.Approval, old.Tool.SandboxMode
	}
	if hasNew && now.Tool != nil {
		c.TargetSideEffect, c.TargetApproval, c.TargetSandboxMode = now.Tool.SideEffect, now.Tool.Approval, now.Tool.SandboxMode
		had := map[string]bool{}
		if hadOld && old.Tool != nil {
			for _, p := range old.Tool.Permissions {
				had[p] = true
			}
		}
		for _, p := range sorted(now.Tool.Permissions) {
			if !had[p] {
				c.GainedPermissions = append(c.GainedPermissions, p)
			}
		}
	}
	return c
}
