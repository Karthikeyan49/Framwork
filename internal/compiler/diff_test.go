package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

func exampleDoc(t *testing.T, name string) metadata.Document {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var doc metadata.Document
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func defOf(doc *metadata.Document, id string) *metadata.Definition {
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == id {
			return &doc.Definitions[i]
		}
	}
	return nil
}

func compileDoc(t *testing.T, doc metadata.Document, catalog Catalog) Program {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	p, ds := Compile(source(t, raw), catalog)
	if !p.Valid() {
		t.Fatal(ds)
	}
	return p
}

// workflowProgram compiles the schema 1.2.0 contact workflow example after mutate.
func workflowProgram(t *testing.T, mutate func(*metadata.Document)) Program {
	t.Helper()
	doc := exampleDoc(t, "workflow-draft-v1.2.json")
	if mutate != nil {
		mutate(&doc)
	}
	return compileDoc(t, doc, PlatformCatalog())
}

// moneyGate makes the start state route on balance > threshold, the shape of an
// approval threshold that does not itself live on an approval state.
func moneyGate(op, amount string) func(*metadata.Document) {
	return func(doc *metadata.Document) {
		w := defOf(doc, "workflow.contact.review").Workflow
		w.States[stateIndex(w, "state.start")].Transitions[0].Condition = &metadata.Expression{Op: op, Args: []metadata.Expression{
			{Op: "field", FieldRef: "field.contact.balance"},
			{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + amount + `","currency":"USD"}`)},
		}}
	}
}

func stateIndex(w *metadata.Workflow, id string) int {
	for i := range w.States {
		if w.States[i].ID == id {
			return i
		}
	}
	panic("no state " + id)
}

func diffOf(t *testing.T, base, target Program) Diff {
	t.Helper()
	d, err := ComputeDiff(base, target)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func change(d Diff, id string) (DefinitionChange, bool) {
	for _, c := range d.Changes {
		if c.ID == id {
			return c, true
		}
	}
	return DefinitionChange{}, false
}

func wantAspects(t *testing.T, d Diff, id, kind string, aspects ...string) {
	t.Helper()
	c, ok := change(d, id)
	if !ok || c.Change != kind || !reflect.DeepEqual(c.Aspects, aspects) {
		t.Fatalf("%s: %+v (want %s %v)", id, c, kind, aspects)
	}
}

func TestDiffFirstReleaseAddsEverything(t *testing.T) {
	target := workflowProgram(t, nil)
	d := diffOf(t, Program{}, target)
	if !d.FirstRelease || d.BaseIRHash != "" || d.IRUpgrade || d.TargetIRHash != target.Hash() || d.Version != DiffVersion {
		t.Fatalf("%+v", d)
	}
	n := len(target.IR().Application.Definitions)
	if len(d.Changes) != n {
		t.Fatalf("changes %d definitions %d", len(d.Changes), n)
	}
	for _, c := range d.Changes {
		if c.Change != ChangeAdded || c.BaseHash != "" || c.TargetHash == "" {
			t.Fatalf("%+v", c)
		}
	}
	wantAspects(t, d, "entity.contact", ChangeAdded, AspectStorage)
	wantAspects(t, d, "page.contact.edit", ChangeAdded, AspectPage)
	wantAspects(t, d, "workflow.contact.review", ChangeAdded, AspectApprovalGate, AspectWorkflowGraph)
	if len(d.ApprovalGates) != 1 || d.ApprovalGates[0].Change != ChangeAdded || len(d.TriggerChanges) != 1 || d.TriggerChanges[0].From != "" {
		t.Fatalf("workflow details %+v", d)
	}
	if len(d.PlanChanges) != len(target.IR().Rules)+len(target.IR().Workflows) {
		t.Fatalf("plan changes %+v", d.PlanChanges)
	}
}

func TestDiffIdenticalProgramsAreEmptyAndDeterministic(t *testing.T) {
	a := workflowProgram(t, nil)
	b := workflowProgram(t, func(doc *metadata.Document) {
		for i, j := 0, len(doc.Definitions)-1; i < j; i, j = i+1, j-1 {
			doc.Definitions[i], doc.Definitions[j] = doc.Definitions[j], doc.Definitions[i]
		}
	})
	d := diffOf(t, a, b)
	if len(d.Changes) != 0 || len(d.PlanChanges) != 0 || d.IRUpgrade || d.FirstRelease || d.BaseIRHash != a.Hash() {
		t.Fatalf("%+v", d)
	}
	again := diffOf(t, a, b)
	if !bytes.Equal(d.JSON(), again.JSON()) || d.Hash() != again.Hash() {
		t.Fatal("nondeterministic")
	}
	parsed, err := ParseDiff(d.JSON(), d.Hash())
	if err != nil || !bytes.Equal(parsed.JSON(), d.JSON()) {
		t.Fatal("parse", err)
	}
	for _, mutate := range []func([]byte) []byte{
		func(r []byte) []byte {
			return bytes.Replace(r, []byte(`"first_release":false`), []byte(`"first_release":true`), 1)
		},
		func(r []byte) []byte { return append(r[:len(r)-1:len(r)-1], []byte(`,"extra":1}`)...) },
	} {
		raw := mutate(d.JSON())
		if _, err = ParseDiff(raw, d.Hash()); err == nil {
			t.Fatal("tampered diff accepted")
		}
		if _, err = ParseDiff(raw, digest(raw)); err == nil && !bytes.Equal(raw, d.JSON()) {
			// A rehashed but non-canonical or unknown-field diff must still fail.
			if strings.Contains(string(raw), "extra") {
				t.Fatal("unknown field accepted")
			}
		}
	}
}

func TestDiffLabelPageRelationshipAndStorage(t *testing.T) {
	base := workflowProgram(t, nil)
	label := workflowProgram(t, func(doc *metadata.Document) { defOf(doc, "entity.contact").Label = "Customer" })
	wantAspects(t, diffOf(t, base, label), "entity.contact", ChangeChanged, AspectLabel)
	if d := diffOf(t, base, label); len(d.Changes) != 1 {
		t.Fatalf("label change leaked: %+v", d.Changes)
	}

	page := workflowProgram(t, func(doc *metadata.Document) {
		p := defOf(doc, "page.contact.edit").Page
		p.Components[0].Label = "Customers"
	})
	d := diffOf(t, base, page)
	wantAspects(t, d, "page.contact.edit", ChangeChanged, AspectPage)
	if len(d.Changes) != 1 {
		t.Fatalf("page change leaked: %+v", d.Changes)
	}

	rel := workflowProgram(t, func(doc *metadata.Document) { defOf(doc, "relationship.contact.parent").Label = "Parent" })
	wantAspects(t, diffOf(t, base, rel), "relationship.contact.parent", ChangeChanged, AspectLabel)

	field := workflowProgram(t, func(doc *metadata.Document) {
		defOf(doc, "field.contact.balance").Field.Currency = "EUR"
	})
	d = diffOf(t, base, field)
	wantAspects(t, d, "field.contact.balance", ChangeChanged, AspectStorage)

	added := workflowProgram(t, func(doc *metadata.Document) {
		doc.Definitions = append(doc.Definitions, metadata.Definition{ID: "field.contact.notes", SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: "Notes", Kind: "field", Field: &metadata.Field{EntityRef: "entity.contact", Type: "string"}})
		e := defOf(doc, "entity.contact").Entity
		e.Fields = append(e.Fields, "field.contact.notes")
	})
	d = diffOf(t, base, added)
	wantAspects(t, d, "field.contact.notes", ChangeAdded, AspectStorage)
	wantAspects(t, d, "entity.contact", ChangeChanged, AspectStorage)
	removed := diffOf(t, added, base)
	wantAspects(t, removed, "field.contact.notes", ChangeRemoved, AspectStorage)
}

func TestDiffRuleLogicAndFinancialRules(t *testing.T) {
	base := compileDoc(t, exampleDoc(t, "contacts-rules-v2.json"), PlatformCatalog())
	named := compileDoc(t, func() metadata.Document {
		doc := exampleDoc(t, "contacts-rules-v2.json")
		defOf(&doc, "rule.contact.named").Rule.Expression.Op = "eq"
		return doc
	}(), PlatformCatalog())
	d := diffOf(t, base, named)
	wantAspects(t, d, "rule.contact.named", ChangeChanged, AspectRuleLogic)
	if len(d.PlanChanges) != 1 || d.PlanChanges[0].Kind != "rule" {
		t.Fatalf("plan changes %+v", d.PlanChanges)
	}
	threshold := compileDoc(t, func() metadata.Document {
		doc := exampleDoc(t, "contacts-rules-v2.json")
		defOf(&doc, "rule.contact.approval_required").Rule.Expression.Args[1].Value = json.RawMessage(`{"amount":"600","currency":"USD"}`)
		return doc
	}(), PlatformCatalog())
	wantAspects(t, diffOf(t, base, threshold), "rule.contact.approval_required", ChangeChanged, AspectRuleFinancial, AspectRuleLogic)
	wantAspects(t, diffOf(t, threshold, base), "rule.contact.approval_required", ChangeChanged, AspectRuleFinancial, AspectRuleLogic)
	rounded := diffOf(t, Program{}, base)
	wantAspects(t, rounded, "rule.contact.rounded_balance", ChangeAdded, AspectRuleFinancial, AspectRuleLogic)
}

func TestDiffWorkflowGateRolesTriggersAndMoneyConditions(t *testing.T) {
	gt := workflowProgram(t, moneyGate("gt", "500"))
	gte := workflowProgram(t, moneyGate("gte", "500"))
	d := diffOf(t, gt, gte)
	c, ok := change(d, "workflow.contact.review")
	if !ok || !contains(c.Aspects, AspectApprovalGate) || !contains(c.Aspects, AspectWorkflowGraph) || contains(c.Aspects, AspectWorkflowTrigger) || contains(c.Aspects, AspectToolPin) {
		t.Fatalf("GT->GTE on a non-approval state: %+v", c)
	}
	if len(d.MoneyConditions) != 1 || d.MoneyConditions[0].StateID != "state.start" || d.MoneyConditions[0].Change != ChangeChanged || len(d.ApprovalGates) != 0 {
		t.Fatalf("money condition details %+v", d)
	}
	threshold := workflowProgram(t, moneyGate("gt", "5000"))
	if d = diffOf(t, gt, threshold); len(d.MoneyConditions) != 1 {
		t.Fatalf("threshold constant change %+v", d.MoneyConditions)
	}
	// Condition not reading money (status) is a plain graph change.
	plain := workflowProgram(t, nil)
	other := workflowProgram(t, func(doc *metadata.Document) {
		w := defOf(doc, "workflow.contact.review").Workflow
		w.States[stateIndex(w, "state.start")].Transitions[0].Condition.Args[1].Value = json.RawMessage(`"inactive"`)
	})
	d = diffOf(t, plain, other)
	c, _ = change(d, "workflow.contact.review")
	// The workflow has an approval gate, so any graph change may change who
	// reaches it and is reported as an approval-gate change too.
	if !reflect.DeepEqual(c.Aspects, []string{AspectApprovalGate, AspectWorkflowGraph}) || len(d.MoneyConditions) != 0 || len(d.ApprovalGates) != 0 {
		t.Fatalf("non-money condition: %+v", c)
	}
	// Approval roles.
	narrow := workflowProgram(t, func(doc *metadata.Document) {
		w := defOf(doc, "workflow.contact.review").Workflow
		w.States[stateIndex(w, "state.approval")].ApprovalRoles = []string{"admin"}
	})
	d = diffOf(t, plain, narrow)
	if len(d.ApprovalGates) != 1 || !reflect.DeepEqual(d.ApprovalGates[0].BaseRoles, []string{"admin", "owner"}) || !reflect.DeepEqual(d.ApprovalGates[0].TargetRoles, []string{"admin"}) || d.ApprovalGates[0].Change != ChangeChanged {
		t.Fatalf("roles %+v", d.ApprovalGates)
	}
	c, _ = change(d, "workflow.contact.review")
	if !contains(c.Aspects, AspectApprovalGate) {
		t.Fatalf("aspects %v", c.Aspects)
	}
	timeout := workflowProgram(t, func(doc *metadata.Document) {
		w := defOf(doc, "workflow.contact.review").Workflow
		w.States[stateIndex(w, "state.approval")].ApprovalTimeoutMS = 20000
	})
	if d = diffOf(t, plain, timeout); len(d.ApprovalGates) != 1 || !d.ApprovalGates[0].TimeoutChanged || d.ApprovalGates[0].TransitionsChanged {
		t.Fatalf("timeout %+v", d.ApprovalGates)
	}
	// Bypass: the start state routes straight to completion.
	bypass := workflowProgram(t, func(doc *metadata.Document) {
		w := defOf(doc, "workflow.contact.review").Workflow
		a := &w.States[stateIndex(w, "state.activity")]
		a.Transitions = []metadata.Transition{{To: "state.complete", Condition: &metadata.Expression{Op: "eq", Args: []metadata.Expression{
			{Op: "field", FieldRef: "field.contact.status"}, {Op: "literal", Value: json.RawMessage(`"inactive"`)}}}}, a.Transitions[0]}
	})
	d = diffOf(t, plain, bypass)
	c, _ = change(d, "workflow.contact.review")
	if !contains(c.Aspects, AspectWorkflowGraph) || !contains(c.Aspects, AspectApprovalGate) || len(d.ApprovalGates) != 0 {
		t.Fatalf("a bypass around an unchanged gate must still count as a gate change: %+v %+v", c, d.ApprovalGates)
	}
	// Trigger widening.
	widened := workflowProgram(t, func(doc *metadata.Document) {
		defOf(doc, "workflow.contact.review").Workflow.Trigger = "record.created"
	})
	d = diffOf(t, plain, widened)
	if len(d.TriggerChanges) != 1 || d.TriggerChanges[0].From != "manual" || d.TriggerChanges[0].To != "record.created" {
		t.Fatalf("trigger %+v", d.TriggerChanges)
	}
	c, _ = change(d, "workflow.contact.review")
	if !contains(c.Aspects, AspectWorkflowTrigger) {
		t.Fatalf("aspects %v", c.Aspects)
	}
}

func toolsProgram(t *testing.T, catalog tools.Catalog, mutate func(*metadata.Document)) Program {
	t.Helper()
	doc := toolDocument(t)
	if mutate != nil {
		mutate(&doc)
	}
	return compileDoc(t, doc, toolCompilerCatalog(catalog))
}

func TestDiffToolActivitiesAddRemoveAndRepin(t *testing.T) {
	catalog := testToolCatalog(t)
	base := toolsProgram(t, catalog, nil)
	same := toolsProgram(t, testToolCatalog(t), nil)
	if d := diffOf(t, base, same); len(d.Changes) != 0 || len(d.ToolActivities) != 0 {
		t.Fatalf("recompiled identical program differs: %+v", d)
	}
	// Re-pin: the authored workflow is byte-identical, the catalog definition moved.
	changed := notifyTool()
	changed.Connector.RatePerSecond = 9
	repinned := toolsProgram(t, testToolCatalog(t, lookupTool(), changed), nil)
	d := diffOf(t, base, repinned)
	c, ok := change(d, "workflow.vendor.onboard")
	if !ok || c.Change != ChangeChanged || !reflect.DeepEqual(c.Aspects, []string{AspectPlanOnly, AspectToolPin}) || !reflect.DeepEqual(c.Tools, []string{"tool.vendor_lookup@1.0.0", "tool.vendor_notify@1.0.0"}) {
		t.Fatalf("repin %+v", c)
	}
	if len(d.ToolActivities) != 1 || d.ToolActivities[0].Change != ChangeRepinned || d.ToolActivities[0].Ref != "tool.vendor_notify" || d.ToolActivities[0].BaseHash == d.ToolActivities[0].TargetHash {
		t.Fatalf("activities %+v", d.ToolActivities)
	}
	// Removal: the lookup activity becomes a local no-op.
	removedProgram := toolsProgram(t, catalog, func(doc *metadata.Document) {
		w := defOf(doc, "workflow.vendor.onboard").Workflow
		s := &w.States[stateIndex(w, "state.lookup")]
		s.Activity = &metadata.Activity{LocalRef: "local.noop", Version: "1.0.0", TimeoutMS: 500, Retry: metadata.Retry{MaxAttempts: 1, InitialBackoffMS: 100, MaxBackoffMS: 100}}
	})
	d = diffOf(t, base, removedProgram)
	if len(d.ToolActivities) != 1 || d.ToolActivities[0].Change != ChangeRemoved || d.ToolActivities[0].Ref != "tool.vendor_lookup" || d.ToolActivities[0].StateID != "state.lookup" {
		t.Fatalf("removed %+v", d.ToolActivities)
	}
	c, _ = change(d, "workflow.vendor.onboard")
	if !contains(c.Aspects, AspectToolPin) || !contains(c.Tools, "tool.vendor_lookup@1.0.0") {
		t.Fatalf("aspects %+v", c)
	}
	d = diffOf(t, removedProgram, base)
	if len(d.ToolActivities) != 1 || d.ToolActivities[0].Change != ChangeAdded {
		t.Fatalf("added %+v", d.ToolActivities)
	}
}

func TestDiffAuthoredToolDeclaration(t *testing.T) {
	catalog := vendorCatalog()
	catalog.Permissions = append(catalog.Permissions, "data.write")
	doc := func(mutate func(*metadata.Tool)) Program {
		d := exampleDoc(t, "vendor-draft-v1.json")
		if mutate != nil {
			mutate(defOf(&d, "tool.vendor.notify").Tool)
		}
		return compileDoc(t, d, catalog)
	}
	base := doc(nil)
	if d := diffOf(t, base, doc(nil)); len(d.Changes) != 0 || len(d.ToolDeclarations) != 0 {
		t.Fatal("identical declarations differ")
	}
	grown := doc(func(x *metadata.Tool) { x.Permissions = []string{"data.write", "vendor.notify"} })
	d := diffOf(t, base, grown)
	wantAspects(t, d, "tool.vendor.notify", ChangeChanged, AspectToolDeclaration)
	if len(d.ToolDeclarations) != 1 || !reflect.DeepEqual(d.ToolDeclarations[0].GainedPermissions, []string{"data.write"}) || d.ToolDeclarations[0].BaseSideEffect != "write" {
		t.Fatalf("%+v", d.ToolDeclarations)
	}
	weaker := doc(func(x *metadata.Tool) { x.Approval = "none"; x.SandboxMode = "block"; x.SideEffect = "read" })
	d = diffOf(t, base, weaker)
	if len(d.ToolDeclarations) != 1 || d.ToolDeclarations[0].BaseApproval != "user" || d.ToolDeclarations[0].TargetApproval != "none" || d.ToolDeclarations[0].TargetSideEffect != "read" || d.ToolDeclarations[0].TargetSandboxMode != "block" {
		t.Fatalf("%+v", d.ToolDeclarations)
	}
	first := diffOf(t, Program{}, base)
	wantAspects(t, first, "tool.vendor.notify", ChangeAdded, AspectToolDeclaration)
}

func TestDiffIRUpgradeDoesNotMarkEveryDefinitionChanged(t *testing.T) {
	base := workflowProgram(t, nil)
	// The same application, authored at schema 1.3.0 (tool-capable), compiles to a
	// newer IR/compiler version without any definition changing.
	doc := exampleDoc(t, "workflow-draft-v1.2.json")
	doc.SchemaVersion = metadata.ToolWorkflowSchemaVersion
	for i := range doc.Definitions {
		doc.Definitions[i].SchemaVersion = metadata.ToolWorkflowSchemaVersion
	}
	target := compileDoc(t, doc, toolCompilerCatalog(testToolCatalog(t)))
	if base.IR().IRVersion == target.IR().IRVersion {
		t.Fatal("fixture did not upgrade the IR")
	}
	d := diffOf(t, base, target)
	if !d.IRUpgrade {
		t.Fatal("IR upgrade not reported")
	}
	for _, c := range d.Changes {
		if c.Change == ChangeChanged && !contains(c.Aspects, AspectPlanOnly) {
			t.Fatalf("upgrade marked %s changed: %+v", c.ID, c)
		}
	}
}

func TestDiffRejectsInvalidInputs(t *testing.T) {
	good := workflowProgram(t, nil)
	if _, err := ComputeDiff(good, Program{}); err == nil {
		t.Fatal("invalid target accepted")
	}
	other := storageProgram(t, nil, "catalog-one")
	if _, err := ComputeDiff(other, good); err == nil {
		t.Fatal("different applications compared")
	}
	// Duplicate definition identifiers cannot be compiled, so exercise the index.
	doc := metadata.Document{Definitions: []metadata.Definition{{ID: "entity.a"}, {ID: "entity.a"}}}
	if _, err := indexDefinitions(doc); err == nil {
		t.Fatal("duplicate ids accepted")
	}
	big := metadata.Document{Definitions: make([]metadata.Definition, MaxDiffDefinitions+1)}
	for i := range big.Definitions {
		big.Definitions[i].ID = "entity.e" + strings.Repeat("a", 1) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + string(rune('a'+i/676%26)) + string(rune('a'+i/17576))
	}
	if _, err := indexDefinitions(big); err == nil {
		t.Fatal("oversized program accepted")
	}
}

// Every payload pointer of metadata.Definition must be known to the diff: add a
// new definition kind and this test fails until the diff handles it.
func TestEveryDefinitionPayloadIsHandled(t *testing.T) {
	typ := reflect.TypeOf(metadata.Definition{})
	found := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() == reflect.Pointer {
			found[f.Name] = true
			if !payloadHandlers[f.Name] {
				t.Errorf("definition payload %s is not handled by the diff", f.Name)
			}
		}
	}
	for name := range payloadHandlers {
		if !found[name] {
			t.Errorf("handler %s has no definition payload", name)
		}
	}
	// payloadOf covers exactly the same set.
	d := metadata.Definition{}
	if _, ok := payloadOf(d); ok {
		t.Fatal("empty definition has a payload")
	}
	d = metadata.Definition{Entity: &metadata.Entity{}, Page: &metadata.Page{}}
	if _, ok := payloadOf(d); ok {
		t.Fatal("two payloads accepted")
	}
}

func TestContrastFailsClosedOnUnexplainedDifferences(t *testing.T) {
	a := metadata.Definition{ID: "entity.a", Kind: "entity", Namespace: "n", Label: "A", Entity: &metadata.Entity{Fields: []string{"field.a.id"}}}
	b := a
	b.Namespace = "other"
	if got := contrast(a, b, nil, nil); !reflect.DeepEqual(got, []string{AspectUnclassified}) {
		t.Fatalf("namespace change: %v", got)
	}
	c := a
	c.Kind = "page"
	if got := contrast(a, c, nil, nil); !reflect.DeepEqual(got, []string{AspectUnclassified}) {
		t.Fatalf("kind change: %v", got)
	}
	d := a
	d.Entity, d.Field = nil, &metadata.Field{EntityRef: "entity.a", Type: "string"}
	if got := contrast(a, d, nil, nil); !reflect.DeepEqual(got, []string{AspectUnclassified}) {
		t.Fatalf("payload change: %v", got)
	}
	e := metadata.Definition{ID: "entity.x", Kind: "mystery"}
	if got := describe(e, nil, nil); !reflect.DeepEqual(got, []string{AspectUnclassified}) {
		t.Fatalf("payload-less add: %v", got)
	}
}
