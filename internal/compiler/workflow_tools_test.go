package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/runtime/workflow"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// Reviewed pre-ADR036 identities (commit a6889dd). Adding schema 1.3.0 must not
// change any existing source, IR or plan byte.
const (
	workflowV12IRHash   = "ea566c0483e6b67ecf2d9b94a9e99c75b397d1c4f16ca6c929ed5c8052c32f8b"
	workflowV12PlanHash = "3efbb3583986e389b5dd37373634e7c44c5f18d17f51cdb66c73792de9f3da30"
	workflowV12Source   = "0deabef06cdb0dbb13c767d8590e1f8f9ee37541cd502f84964565884baa237e"
	rulesV11IRHash      = "ae23ec58115281bdf25a2d16f36736de2f54dfe369d6299e396902e5fce2bdf9"
	contactsV1IRHash    = "9f40020df7e1a80925b3b70ec5e1d08a022677054b300276b15f8706a5fdc66b"
)

func lookupTool() tools.Definition {
	return tools.Definition{
		Ref: "tool.vendor_lookup", Version: "1.0.0", SideEffect: "READ", Approval: "NONE",
		Delivery:  tools.DeliverySpec{IdempotencyMode: "NONE", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: tools.Connector{Endpoint: tools.Endpoint{Host: "vendors.example.com", Port: 443, Network: "public"}, Method: "GET", Path: "/vendors/{vendor_id}", TimeoutMS: 1000, MaxResponseBytes: 4096, RatePerSecond: 10},
		Input:     []tools.Field{{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "path"}},
		Output:    []tools.Field{{Name: "risk_score", Type: "integer", Required: true}},
		Sandbox:   tools.SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{"risk_score": json.RawMessage(`7`)}},
		Tenants:   []string{"*"},
	}
}
func notifyTool() tools.Definition {
	return tools.Definition{
		Ref: "tool.vendor_notify", Version: "1.0.0", SideEffect: "WRITE", Approval: "REQUIRED",
		Delivery:  tools.DeliverySpec{IdempotencyMode: "NATIVE_KEY", RetryMode: "SAFE_RETRY", ReconciliationMode: "MANUAL", ReplayWindowMS: 86400000},
		Connector: tools.Connector{Endpoint: tools.Endpoint{Host: "notify.example.com", Port: 443, Network: "public", SecretRef: "vendor_notify"}, Method: "POST", Path: "/notifications", IdempotencyHeader: "Idempotency-Key", TimeoutMS: 1500, MaxResponseBytes: 4096, RatePerSecond: 5},
		Input: []tools.Field{
			{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "body"},
			{Name: "name", Type: "string", Required: true, MaxLength: 200, In: "body"},
			{Name: "tier", Type: "string", MaxLength: 16, In: "body"},
			{Name: "credit_limit", Type: "decimal", In: "body"},
			{Name: "active", Type: "bool", In: "body"},
			{Name: "employees", Type: "integer", In: "body"},
		},
		Output:  []tools.Field{{Name: "notification_id", Type: "string", Required: true, MaxLength: 64}},
		Sandbox: tools.SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{"notification_id": json.RawMessage(`"sim-1"`)}},
		Tenants: []string{"*"},
	}
}
func testToolCatalog(t *testing.T, definitions ...tools.Definition) tools.Catalog {
	t.Helper()
	if definitions == nil {
		definitions = []tools.Definition{lookupTool(), notifyTool()}
	}
	c, e := tools.NewCatalog(definitions)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func toolCompilerCatalog(c tools.Catalog) Catalog {
	catalog := PlatformCatalog()
	catalog.ToolCatalog = c
	return catalog
}
func toolDocument(t *testing.T) metadata.Document {
	t.Helper()
	raw, e := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.3.json")
	if e != nil {
		t.Fatal(e)
	}
	var doc metadata.Document
	if e = json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	return doc
}
func toolState(doc *metadata.Document, id string) *metadata.WorkflowState {
	for i := range doc.Definitions {
		if w := doc.Definitions[i].Workflow; w != nil {
			for j := range w.States {
				if w.States[j].ID == id {
					return &w.States[j]
				}
			}
		}
	}
	return nil
}
func codes(ds []Diagnostic) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, d.Code)
	}
	return out
}

func TestToolWorkflowCompilesPinsAndRestoresDeterministically(t *testing.T) {
	raw, _ := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.3.json")
	s := source(t, raw)
	catalog := testToolCatalog(t)
	p, ds := Compile(s, toolCompilerCatalog(catalog))
	if !p.Valid() || len(ds) != 0 {
		t.Fatal(ds)
	}
	ir := p.IR()
	platformJSON, _ := json.Marshal(PlatformCatalog())
	if ir.IRVersion != ToolWorkflowIRVersion || ir.CompilerVersion != ToolWorkflowCompilerVersion || ir.CatalogHash != digest(platformJSON) || len(ir.Workflows) != 1 {
		t.Fatal("1.3 IR identity or tool catalog leaked into CatalogHash", ir.IRVersion, ir.CompilerVersion)
	}
	again, ds := Compile(s, toolCompilerCatalog(testToolCatalog(t)))
	if len(ds) != 0 || again.Hash() != p.Hash() || !bytes.Equal(again.JSON(), p.JSON()) {
		t.Fatal("tool IR is nondeterministic")
	}
	doc := toolDocument(t)
	for i, j := 0, len(doc.Definitions)-1; i < j; i, j = i+1, j-1 {
		doc.Definitions[i], doc.Definitions[j] = doc.Definitions[j], doc.Definitions[i]
	}
	reordered, _ := json.Marshal(doc)
	if other, ds := Compile(source(t, reordered), toolCompilerCatalog(catalog)); len(ds) != 0 || other.Hash() != p.Hash() {
		t.Fatal("definition order changed tool IR", ds)
	}

	restored, e := Restore(p.JSON(), p.Hash(), SupportedIRVersions())
	if e != nil || !MatchesStoredSource(restored, s) {
		t.Fatal("1.3 restore", e)
	}
	w, e := restored.Workflow("workflow.vendor.onboard")
	if e != nil {
		t.Fatal(e)
	}
	plan := w.Plan()
	if plan.DSLVersion != workflow.ToolDSLVersion || !w.HasTools() {
		t.Fatal("tool plan DSL", plan.DSLVersion)
	}
	for id, want := range map[string]tools.Definition{"state.lookup": lookupTool(), "state.notify": notifyTool()} {
		state, ok := w.State(id)
		pinned, found := catalog.Lookup(want.Ref, want.Version, state.ToolHash)
		if !ok || !state.IsTool() || !found || state.ToolHash != pinned.Hash() || state.Activity.TimeoutMS != 0 || state.Activity.LocalRef != "" {
			t.Fatal("tool pin", id, state.ToolHash)
		}
	}
	if local, _ := w.State("state.record"); local.IsTool() || local.ToolHash != "" || local.Activity.LocalRef != "local.noop" {
		t.Fatal("local activity changed inside a tool plan")
	}
	if got := w.FieldIDs(); !reflect.DeepEqual(got, []string{"field.vendor.active", "field.vendor.credit_limit", "field.vendor.id", "field.vendor.name", "field.vendor.tier"}) {
		t.Fatal("snapshot fields omit tool inputs", got)
	}

	// A changed definition is a new pin and IR; the old pin never resolves to it.
	changed := lookupTool()
	changed.Connector.TimeoutMS = 900
	newer := testToolCatalog(t, changed, notifyTool())
	next, ds := Compile(s, toolCompilerCatalog(newer))
	if len(ds) != 0 || next.Hash() == p.Hash() || next.IR().CatalogHash != ir.CatalogHash {
		t.Fatal("tool definition is not pinned inside the plan", ds)
	}
	old, _ := w.State("state.lookup")
	if _, ok := newer.Lookup("tool.vendor_lookup", "1.0.0", old.ToolHash); ok {
		t.Fatal("changed definition replaced an existing pin")
	}

	// Without the trusted catalog the compiler fails closed with a typed diagnostic.
	if none, ds := Compile(s, PlatformCatalog()); none.Valid() || !reflect.DeepEqual(codes(ds), []string{"unknown_tool", "unknown_tool"}) {
		t.Fatal("unregistered tools compiled", ds)
	}
}

func TestToolWorkflowRejectsUnsafeBindings(t *testing.T) {
	catalog := toolCompilerCatalog(testToolCatalog(t))
	cases := []struct {
		name   string
		code   string
		change func(*metadata.Document)
	}{
		{"unknown ref", "unknown_tool", func(d *metadata.Document) { toolState(d, "state.lookup").Activity.ToolRef = "tool.vendor_missing" }},
		{"unknown version", "unknown_tool", func(d *metadata.Document) { toolState(d, "state.lookup").Activity.Version = "1.0.1" }},
		{"string from bool", "tool_input_type", func(d *metadata.Document) {
			toolState(d, "state.lookup").Activity.Inputs["vendor_id"] = "field.vendor.active"
		}},
		{"decimal from integer", "tool_input_type", func(d *metadata.Document) {
			toolState(d, "state.notify").Activity.Inputs["credit_limit"] = "field.vendor.employees"
		}},
		{"integer from decimal", "tool_input_type", func(d *metadata.Document) {
			toolState(d, "state.notify").Activity.Inputs["employees"] = "field.vendor.credit_limit"
		}},
		{"bool from string", "tool_input_type", func(d *metadata.Document) {
			toolState(d, "state.notify").Activity.Inputs["active"] = "field.vendor.name"
		}},
		{"enum longer than tool string", "tool_input_type", func(d *metadata.Document) {
			for i := range d.Definitions {
				if d.Definitions[i].ID == "field.vendor.tier" {
					d.Definitions[i].Field.EnumValues = []string{"gold", "silver", strings.Repeat("p", 17)}
				}
			}
		}},
		{"missing required input", "tool_input_missing", func(d *metadata.Document) {
			delete(toolState(d, "state.notify").Activity.Inputs, "name")
		}},
		{"undeclared input", "tool_input_unknown", func(d *metadata.Document) {
			toolState(d, "state.notify").Activity.Inputs["url"] = "field.vendor.name"
		}},
		{"foreign field", "tool_input_field", func(d *metadata.Document) {
			toolState(d, "state.lookup").Activity.Inputs["vendor_id"] = "field.other.id"
		}},
		{"approval bypass branch", "tool_requires_approval", func(d *metadata.Document) {
			s := toolState(d, "state.lookup")
			gold := metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "field", FieldRef: "field.vendor.tier"}, {Op: "literal", Value: json.RawMessage(`"gold"`)}}}
			s.Transitions = []metadata.Transition{{To: "state.notify", Condition: &gold}, {To: "state.approval"}}
		}},
		{"rejected path reaches tool", "tool_requires_approval", func(d *metadata.Document) {
			s := toolState(d, "state.approval")
			s.Transitions = []metadata.Transition{{To: "state.failed", On: "approved"}, {To: "state.notify", On: "rejected"}}
		}},
		{"no approval state", "tool_requires_approval", func(d *metadata.Document) {
			toolState(d, "state.lookup").Transitions = []metadata.Transition{{To: "state.notify"}}
		}},
	}
	v, _ := metadata.NewValidator()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := toolDocument(t)
			c.change(&doc)
			raw, _ := json.Marshal(doc)
			s, e := v.Parse(raw)
			if e != nil {
				t.Fatal("schema fixture", e)
			}
			p, ds := Compile(s, catalog)
			if p.Valid() || !contains(codes(ds), c.code) || contains(codes(ds), "workflow_contract") {
				t.Fatal("unsafe tool binding", c.code, ds)
			}
			for _, d := range ds {
				if !strings.HasPrefix(d.Path, "/workflow.vendor.onboard/") || d.Message == "" {
					t.Fatal("untraceable diagnostic", d)
				}
			}
		})
	}
	// A READ tool needs no approval; the WRITE tool after an approval passes.
	doc := toolDocument(t)
	raw, _ := json.Marshal(doc)
	s, _ := v.Parse(raw)
	if p, ds := Compile(s, catalog); !p.Valid() {
		t.Fatal("approval-dominated tool rejected", ds)
	}
	// Approval dominance also holds when the approval sits on every branch.
	doc = toolDocument(t)
	start := toolState(&doc, "state.start")
	start.Transitions[1].To = "state.approval"
	raw, _ = json.Marshal(doc)
	s, _ = v.Parse(raw)
	if p, ds := Compile(s, catalog); !p.Valid() {
		t.Fatal("approval on every path rejected", ds)
	}
}

func TestToolWorkflowLeavesExistingContractsUnchanged(t *testing.T) {
	s := workflowSource(t)
	if s.Hash() != workflowV12Source {
		t.Fatal("1.2 source identity changed", s.Hash())
	}
	for _, catalog := range []Catalog{PlatformCatalog(), toolCompilerCatalog(testToolCatalog(t))} {
		p, ds := Compile(s, catalog)
		if !p.Valid() || p.Hash() != workflowV12IRHash || p.IR().IRVersion != WorkflowIRVersion {
			t.Fatal("1.2 IR changed", p.Hash(), ds)
		}
		w, e := p.Workflow("workflow.contact.review")
		if e != nil || w.Hash() != workflowV12PlanHash || w.Plan().DSLVersion != workflow.Version || w.HasTools() {
			t.Fatal("1.2 plan changed", w.Hash(), e)
		}
		if !reflect.DeepEqual(w.FieldIDs(), []string{"field.contact.status"}) {
			t.Fatal("1.2 snapshot fields changed", w.FieldIDs())
		}
	}
	for file, want := range map[string]string{"contacts-rules-v2.json": rulesV11IRHash, "contacts-draft-v1.json": contactsV1IRHash} {
		raw, _ := os.ReadFile("../../contracts/examples/" + file)
		if p, ds := Compile(source(t, raw), toolCompilerCatalog(testToolCatalog(t))); p.Hash() != want {
			t.Fatal("legacy IR changed", file, p.Hash(), ds)
		}
	}
	// 1.3 without tools behaves exactly like 1.2: identical DSL 2.0.0 plan bytes.
	bumped := strings.ReplaceAll(string(s.JSON()), `"schema_version":"1.2.0"`, `"schema_version":"1.3.0"`)
	p, ds := Compile(source(t, []byte(bumped)), PlatformCatalog())
	if !p.Valid() || p.IR().IRVersion != ToolWorkflowIRVersion {
		t.Fatal(ds)
	}
	w, e := p.Workflow("workflow.contact.review")
	if e != nil || w.Hash() != workflowV12PlanHash {
		t.Fatal("1.3 changed a tool-free plan", e)
	}
	if _, e = Restore(p.JSON(), p.Hash(), SupportedIRVersions()); e != nil {
		t.Fatal(e)
	}
}

func TestToolWorkflowStoredIRTamperingFailsClosed(t *testing.T) {
	raw, _ := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.3.json")
	catalog := testToolCatalog(t)
	p, ds := Compile(source(t, raw), toolCompilerCatalog(catalog))
	if !p.Valid() {
		t.Fatal(ds)
	}
	state := func(ir *IR, id string) *workflow.State {
		for i := range ir.Workflows[0].States {
			if ir.Workflows[0].States[i].ID == id {
				return &ir.Workflows[0].States[i]
			}
		}
		t.Fatal("missing state", id)
		return nil
	}
	restore := func(ir IR) error {
		b, _ := json.Marshal(ir)
		_, e := Restore(b, digest(b), SupportedIRVersions())
		return e
	}
	for name, mutate := range map[string]func(*IR){
		"uppercase hash": func(ir *IR) { s := state(ir, "state.lookup"); s.ToolHash = strings.ToUpper(s.ToolHash) },
		"short hash":     func(ir *IR) { state(ir, "state.lookup").ToolHash = "abc" },
		"missing hash":   func(ir *IR) { state(ir, "state.notify").ToolHash = "" },
		"hash on local":  func(ir *IR) { state(ir, "state.record").ToolHash = state(ir, "state.lookup").ToolHash },
		"plan input drift": func(ir *IR) {
			state(ir, "state.lookup").Activity.Inputs = map[string]string{"vendor_id": "field.vendor.name"}
		},
		"plan ref drift":    func(ir *IR) { state(ir, "state.lookup").Activity.ToolRef = "tool.vendor_notify" },
		"tool timeout":      func(ir *IR) { state(ir, "state.lookup").Activity.TimeoutMS = 500 },
		"legacy dsl":        func(ir *IR) { ir.Workflows[0].DSLVersion = workflow.Version },
		"legacy ir version": func(ir *IR) { ir.IRVersion = WorkflowIRVersion; ir.CompilerVersion = WorkflowCompilerVersion },
		"tool compensation": func(ir *IR) { state(ir, "state.notify").Activity.CompensationLocalRef = "local.noop" },
	} {
		t.Run(name, func(t *testing.T) {
			ir := p.IR()
			mutate(&ir)
			if restore(ir) == nil {
				t.Fatal("tampered stored IR restored")
			}
		})
	}
	// Source carries no hash, so a well-formed substituted pin restores, but it
	// changes the IR/plan identity and never resolves in the live catalog.
	ir := p.IR()
	state(&ir, "state.lookup").ToolHash = strings.Repeat("0", 64)
	b, _ := json.Marshal(ir)
	forged, e := Restore(b, digest(b), SupportedIRVersions())
	if e != nil || forged.Hash() == p.Hash() {
		t.Fatal("pin substitution", e)
	}
	w, _ := forged.Workflow("workflow.vendor.onboard")
	pin, _ := w.State("state.lookup")
	if _, ok := catalog.Lookup(pin.Activity.ToolRef, pin.Activity.Version, pin.ToolHash); ok {
		t.Fatal("forged pin resolved")
	}
}
