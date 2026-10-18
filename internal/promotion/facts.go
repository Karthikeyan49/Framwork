package promotion

import (
	"sort"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tools"
	"github.com/Karthikeyan49/Framwork/internal/verification"
)

// FactsInput is everything the risk reducer reads. All of it is derived on the
// server from trusted artifacts: the diff of two compiled programs, the storage
// classification, the operator tool catalog definitions that the pins resolve
// to, and the build identities. A request supplies none of it.
type FactsInput struct {
	Diff        compiler.Diff
	Storage     compiler.StorageDelta
	TargetPins  []ToolPin
	Definitions map[string]tools.Definition // key: DefinitionKey(ref, version, hash)
	TenantID    string
	Volume      string

	AcceptanceAuthorIsRequester bool
}

// DefinitionKey addresses a catalog definition by its pin.
func DefinitionKey(ref, version, hash string) string { return ref + "|" + version + "|" + hash }

var (
	effectOrder = []string{risk.SideNone, risk.SideRead, risk.SideWrite, risk.SideFinancial, risk.SideDestructive}
	idemOrder   = []string{risk.IdemNativeKey, risk.IdemLookupByReference, risk.IdemDedupeWindow, risk.IdemNone}
	declOrder   = []string{risk.DeclNone, risk.DeclRead, risk.DeclWrite, risk.DeclDestructive, risk.DeclFinancial}
	approvals   = []string{"none", "user", "elevated"}
)

func rank(order []string, v string) int {
	for i, s := range order {
		if s == v {
			return i
		}
	}
	return len(order) // unknown is the most severe
}

func labelOnly(c compiler.DefinitionChange) bool {
	return c.Change == compiler.ChangeChanged && len(c.Aspects) == 1 && c.Aspects[0] == compiler.AspectLabel
}

func hasAspect(c compiler.DefinitionChange, a string) bool {
	for _, x := range c.Aspects {
		if x == a {
			return true
		}
	}
	return false
}

// ReduceFacts maps a diff to the closed vocabulary of release-risk-v1. Anything
// it cannot explain is Unclassified, which the policy rates HIGH.
func ReduceFacts(in FactsInput) risk.Facts {
	f := risk.Facts{
		FirstRelease: in.Diff.FirstRelease, IRUpgrade: in.Diff.IRUpgrade, AcceptanceAuthorIsRequester: in.AcceptanceAuthorIsRequester,
		Storage: risk.StorageFact{Class: in.Storage.Class, Reasons: append([]string{}, in.Storage.Reasons...)}, Volume: in.Volume,
		Tools: []risk.ToolFact{}, ToolDeclarations: []risk.ToolDeclFact{},
	}
	changedWorkflowTools := map[string]bool{}
	for _, c := range in.Diff.Changes {
		if hasAspect(c, compiler.AspectUnclassified) {
			f.Unclassified = true
		}
		if labelOnly(c) {
			f.LabelChanged = true
			continue
		}
		switch c.Kind {
		case "page":
			f.PageChanged = true
		case "entity":
			if c.Change == compiler.ChangeAdded {
				f.EntityAdded = true
			}
		case "field":
		case "relationship":
			f.RelationshipChanged = true
		case "rule":
			switch {
			case hasAspect(c, compiler.AspectRuleFinancial):
				f.FinancialRuleChanged = true
			case hasAspect(c, compiler.AspectRuleLogic):
				f.RuleChanged = true
			case hasAspect(c, compiler.AspectPlanOnly):
				f.PlanOnlyChanged = true
			}
		case "workflow":
			// Any change to a workflow that contains a tool inherits the tool's level,
			// including a plan-only recompilation of the conditions in front of it
			// (independent review F7).
			for _, ref := range c.Tools {
				changedWorkflowTools[ref] = true
			}
			if len(c.Aspects) == 1 && c.Aspects[0] == compiler.AspectPlanOnly {
				f.PlanOnlyChanged = true
				break
			}
			f.WorkflowChanged = true
			if hasAspect(c, compiler.AspectApprovalGate) {
				f.ApprovalGateChanged = true
			}
		case "tool":
		default:
			f.Unclassified = true
		}
	}
	for _, t := range in.Diff.TriggerChanges {
		if (t.To == "record.created" || t.To == "record.updated") && t.From != t.To {
			f.TriggerWidened = true
		}
	}

	type factKey struct{ ref, version, change string }
	merged := map[factKey]risk.ToolFact{}
	put := func(ref, version, change, effect, idem string, unresolved bool) {
		k := factKey{ref, version, change}
		if old, ok := merged[k]; ok {
			if rank(effectOrder, old.SideEffect) > rank(effectOrder, effect) {
				effect = old.SideEffect
			}
			if rank(idemOrder, old.Idempotency) > rank(idemOrder, idem) {
				idem = old.Idempotency
			}
			unresolved = unresolved || old.Unresolved
		}
		merged[k] = risk.ToolFact{Ref: ref, Version: version, SideEffect: effect, Idempotency: idem, Change: change, Unresolved: unresolved}
	}
	resolve := func(ref, version, hash string) (tools.Definition, bool) {
		d, ok := in.Definitions[DefinitionKey(ref, version, hash)]
		return d, ok && d.Hash() == hash
	}
	explicit := map[string]bool{}
	for _, a := range in.Diff.ToolActivities {
		explicit[a.Ref+"@"+a.Version] = true
		// Without a resolvable definition assume the worst effect; only the
		// target side can make the release unresolved (a removed pin need not
		// exist in today's catalog).
		effect, idem, unresolved := risk.SideWrite, risk.IdemNone, false
		newDef, haveNew := resolve(a.Ref, a.Version, a.TargetHash)
		oldDef, haveOld := resolve(a.Ref, a.Version, a.BaseHash)
		if a.Change != compiler.ChangeRemoved {
			unresolved = !haveNew || !newDef.AllowsTenant(in.TenantID)
		}
		var picks []tools.Definition
		if haveNew && a.Change != compiler.ChangeRemoved {
			picks = append(picks, newDef)
		}
		if haveOld && a.Change != compiler.ChangeAdded {
			picks = append(picks, oldDef)
		}
		if len(picks) > 0 {
			effect, idem = picks[0].SideEffect, picks[0].Delivery.IdempotencyMode
			for _, p := range picks[1:] {
				if rank(effectOrder, p.SideEffect) > rank(effectOrder, effect) {
					effect = p.SideEffect
				}
				if rank(idemOrder, p.Delivery.IdempotencyMode) > rank(idemOrder, idem) {
					idem = p.Delivery.IdempotencyMode
				}
			}
		}
		put(a.Ref, a.Version, a.Change, effect, idem, unresolved)
	}
	// A changed workflow inherits the level of every tool it still contains.
	for _, pin := range in.TargetPins {
		ref := pin.Ref + "@" + pin.Version
		if !changedWorkflowTools[ref] || explicit[ref] {
			continue
		}
		def, ok := resolve(pin.Ref, pin.Version, pin.Hash)
		effect, idem := risk.SideWrite, risk.IdemNone
		if ok {
			effect, idem = def.SideEffect, def.Delivery.IdempotencyMode
		}
		put(pin.Ref, pin.Version, risk.ChangeInherited, effect, idem, !ok || !def.AllowsTenant(in.TenantID))
	}
	for _, t := range merged {
		f.Tools = append(f.Tools, t)
	}
	sort.Slice(f.Tools, func(i, j int) bool {
		a, b := f.Tools[i], f.Tools[j]
		if a.Ref != b.Ref {
			return a.Ref < b.Ref
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		return a.Change < b.Change
	})

	for _, d := range in.Diff.ToolDeclarations {
		effect := d.TargetSideEffect
		if d.Change == compiler.ChangeRemoved {
			effect = d.BaseSideEffect
		} else if d.Change == compiler.ChangeChanged && rank(declOrder, d.BaseSideEffect) > rank(declOrder, effect) {
			effect = d.BaseSideEffect
		}
		fact := risk.ToolDeclFact{ID: d.ID, SideEffect: effect, Change: d.Change}
		for _, p := range d.GainedPermissions {
			if p == "data.write" {
				fact.GainsDataWrite = true
			}
		}
		if d.Change == compiler.ChangeChanged && rank(approvals, d.TargetApproval) < rank(approvals, d.BaseApproval) {
			fact.WeakensApproval = true
		}
		f.ToolDeclarations = append(f.ToolDeclarations, fact)
	}
	sort.Slice(f.ToolDeclarations, func(i, j int) bool { return f.ToolDeclarations[i].ID < f.ToolDeclarations[j].ID })
	return f
}

// ChangedFor lists what the release changed that requirements must cover.
func ChangedFor(d compiler.Diff, s compiler.StorageDelta) verification.ChangedSurfaces {
	out := verification.ChangedSurfaces{NonEqualStorage: s.Class != compiler.StorageEqual && !s.FirstRelease}
	for _, c := range d.Changes {
		switch {
		case c.Kind == "rule" && hasAspect(c, compiler.AspectRuleFinancial) && c.EntityRef != "":
			out.FinancialRuleEntities = append(out.FinancialRuleEntities, c.EntityRef)
		case c.Kind == "workflow" && hasAspect(c, compiler.AspectApprovalGate):
			out.ApprovalGateWorkflows = append(out.ApprovalGateWorkflows, c.ID)
		}
	}
	for _, a := range d.ToolActivities {
		out.ToolRefs = append(out.ToolRefs, a.Ref)
	}
	return out
}
