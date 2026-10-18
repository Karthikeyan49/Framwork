package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// applyOps parses ops into a proposal (so the strict parser runs too) and applies it.
func applyOps(t testing.TB, doc metadata.Document, ops ...map[string]any) (metadata.Document, []Conflict, error) {
	t.Helper()
	return ApplyProposal(doc, parseOK(t, proposalMap(ops...)))
}

func applyOK(t testing.TB, doc metadata.Document, ops ...map[string]any) metadata.Document {
	t.Helper()
	out, conflicts, err := applyOps(t, doc, ops...)
	if err != nil || len(conflicts) > 0 {
		t.Fatalf("apply: %v %+v", err, conflicts)
	}
	return out
}

func docBytes(t testing.TB, doc metadata.Document) []byte { return mustJSON(t, doc) }

func TestApplyProposalAddUpdateDelete(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	before := docBytes(t, doc)
	out := applyOK(t, doc,
		addPageOp(t, doc, "page.contact.summary", "Summary"),
		updateLabelOp(t, doc, "entity.contact", "Person"),
		deleteOp(t, doc, "relationship.contact.parent"))
	if !bytes.Equal(before, docBytes(t, doc)) {
		t.Fatal("ApplyProposal mutated its input")
	}
	if got := defByID(t, out, "entity.contact").Label; got != "Person" {
		t.Fatalf("label %q", got)
	}
	if _, found := slices.BinarySearchFunc(out.Definitions, "relationship.contact.parent", func(d metadata.Definition, id string) int { return strings.Compare(d.ID, id) }); found {
		t.Fatal("the deleted definition is still present")
	}
	defByID(t, out, "page.contact.summary")
	if !slices.IsSortedFunc(out.Definitions, func(a, b metadata.Definition) int { return strings.Compare(a.ID, b.ID) }) {
		t.Fatal("definitions are not sorted by id")
	}
	if len(out.Definitions) != len(doc.Definitions) { // +1 page, -1 relationship
		t.Fatalf("%d definitions, want %d", len(out.Definitions), len(doc.Definitions))
	}
	if out.SchemaVersion != doc.SchemaVersion || out.AppID != doc.AppID || out.Namespace != doc.Namespace || !reflect.DeepEqual(out.BaseVersion, doc.BaseVersion) {
		t.Fatal("the envelope changed")
	}
	// The patched document is valid and compiles.
	c := newCompiler(t)
	if _, diags, err := c.Compile(out); err != nil || len(diags) > 0 {
		t.Fatalf("patched document does not compile: %v %v", err, diags)
	}
}

func TestApplyProposalSharesNoMemoryWithItsInputs(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	p := parseOK(t, proposalMap(updateLabelOp(t, doc, "page.contact.edit", "Edited")))
	out, _, err := ApplyProposal(doc, p)
	if err != nil {
		t.Fatal(err)
	}
	beforeDoc, beforeProposal := docBytes(t, doc), mustJSON(t, p)
	for i := range out.Definitions {
		if pg := out.Definitions[i].Page; pg != nil {
			pg.EntityRef = "entity.mutated"
			pg.Components = nil
		}
		if f := out.Definitions[i].Field; f != nil {
			f.Type = "mutated"
		}
	}
	if !bytes.Equal(beforeDoc, docBytes(t, doc)) || !bytes.Equal(beforeProposal, mustJSON(t, p)) {
		t.Fatal("the result aliases the draft or the proposal")
	}
}

func TestApplyProposalIsDeterministic(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	p := parseOK(t, proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary"), updateLabelOp(t, doc, "entity.contact", "Person")))
	var first []byte
	for i := 0; i < 10; i++ {
		// A shuffled input order must not matter either.
		shuffled := doc
		shuffled.Definitions = slices.Clone(doc.Definitions)
		if i%2 == 1 {
			slices.Reverse(shuffled.Definitions)
		}
		out, conflicts, err := ApplyProposal(shuffled, p)
		if err != nil || len(conflicts) > 0 {
			t.Fatal(err, conflicts)
		}
		if raw := docBytes(t, out); i == 0 {
			first = raw
		} else if !bytes.Equal(raw, first) {
			t.Fatal("output differs between runs or input orders")
		}
	}
}

func TestApplyProposalConflicts(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	tests := []struct {
		name     string
		ops      func() []map[string]any
		category string
		target   string
	}{
		{"stale update", func() []map[string]any {
			op := updateLabelOp(t, doc, "entity.contact", "Person")
			op["expected_hash"] = strings.Repeat("0", 64)
			return []map[string]any{op}
		}, ConflictStaleHash, "entity.contact"},
		{"stale delete", func() []map[string]any {
			op := deleteOp(t, doc, "rule.contact.named")
			op["expected_hash"] = strings.Repeat("f", 64)
			return []map[string]any{op}
		}, ConflictStaleHash, "rule.contact.named"},
		{"update of a missing target", func() []map[string]any {
			op := updateLabelOp(t, doc, "entity.contact", "Person")
			op["target"] = "entity.absent"
			op["definition"].(map[string]any)["id"] = "entity.absent"
			return []map[string]any{op}
		}, ConflictMissing, "entity.absent"},
		{"delete of a missing target", func() []map[string]any {
			return []map[string]any{{"op": "delete", "target": "page.absent", "expected_hash": strings.Repeat("a", 64), "rationale": "x"}}
		}, ConflictMissing, "page.absent"},
		{"add collision", func() []map[string]any {
			return []map[string]any{addPageOp(t, doc, "page.contact.edit", "Again")}
		}, ConflictCollision, "page.contact.edit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := docBytes(t, doc)
			out, conflicts, err := applyOps(t, doc, tc.ops()...)
			if err != nil {
				t.Fatal(err)
			}
			if len(conflicts) != 1 || conflicts[0].Category != tc.category || conflicts[0].Target != tc.target || conflicts[0].Reason == "" {
				t.Fatalf("conflicts %+v", conflicts)
			}
			if len(out.Definitions) != 0 || out.AppID != "" {
				t.Fatal("a conflicting proposal returned a document (all or nothing)")
			}
			if !bytes.Equal(before, docBytes(t, doc)) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestApplyProposalIsAllOrNothing(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	bad := updateLabelOp(t, doc, "entity.contact", "Person")
	bad["expected_hash"] = strings.Repeat("0", 64)
	out, conflicts, err := applyOps(t, doc, addPageOp(t, doc, "page.contact.summary", "Summary"), bad)
	if err != nil || len(conflicts) != 1 || len(out.Definitions) != 0 {
		t.Fatalf("%v %+v %d", err, conflicts, len(out.Definitions))
	}
}

func TestApplyProposalRejectsADraftThatMoved(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	p := parseOK(t, proposalMap(updateLabelOp(t, doc, "entity.contact", "Person")))
	moved := applyOK(t, doc, updateLabelOp(t, doc, "entity.contact", "Someone else's edit"))
	_, conflicts, err := ApplyProposal(moved, p)
	if err != nil || len(conflicts) != 1 || conflicts[0].Category != ConflictStaleHash {
		t.Fatalf("apply on a moved draft: %v %+v", err, conflicts)
	}
	// And the same proposal applies to the draft it was written against.
	if _, conflicts, err = ApplyProposal(doc, p); err != nil || len(conflicts) != 0 {
		t.Fatal(err, conflicts)
	}
}

func TestApplyProposalDuplicateTargets(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	// Built by hand: the strict parser would already refuse this proposal.
	a := parseOK(t, proposalMap(updateLabelOp(t, doc, "entity.contact", "A"))).Operations[0]
	b := parseOK(t, proposalMap(updateLabelOp(t, doc, "entity.contact", "B"))).Operations[0]
	_, conflicts, err := ApplyProposal(doc, PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{a, b}})
	if err != nil || len(conflicts) != 1 || conflicts[0].Category != ConflictDuplicateTarget || conflicts[0].Target != "entity.contact" {
		t.Fatalf("%v %+v", err, conflicts)
	}
}

func TestApplyProposalMalformedProposalsAreErrors(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	good := parseOK(t, proposalMap(updateLabelOp(t, doc, "entity.contact", "Person"))).Operations[0]
	with := func(f func(*PatchOperation)) PatchProposal {
		op := good
		f(&op)
		return PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{op}}
	}
	many := PatchProposal{Version: PatchProposalVersion}
	for i := 0; i <= MaxOperations; i++ {
		many.Operations = append(many.Operations, good)
	}
	for name, p := range map[string]PatchProposal{
		"wrong version":      {Version: "ai-patch-proposal-v0"},
		"unknown op":         with(func(o *PatchOperation) { o.Op = "replace" }),
		"target not an id":   with(func(o *PatchOperation) { o.Target = "../etc" }),
		"delete+definition":  with(func(o *PatchOperation) { o.Op = "delete" }),
		"update, no def":     with(func(o *PatchOperation) { o.Definition = nil }),
		"more than the max":  many,
		"target is too long": with(func(o *PatchOperation) { o.Target = "page." + longString(200) }),
	} {
		t.Run(name, func(t *testing.T) {
			out, conflicts, err := ApplyProposal(doc, p)
			if err == nil || len(conflicts) != 0 || len(out.Definitions) != 0 {
				t.Fatalf("want an error, got %v %+v", err, conflicts)
			}
		})
	}
	if _, _, err := ApplyProposal(doc, many); !strings.Contains(err.Error(), "too large") {
		t.Fatalf("MaxOperations error: %v", err)
	}
}

// TestAllowlistCoversEveryDefinitionKind pins the allowlist against the payload
// fields of metadata.Definition. A new kind added to metadata fails this test until
// somebody decides whether a model may touch it.
func TestAllowlistCoversEveryDefinitionKind(t *testing.T) {
	var payloads []string
	typ := reflect.TypeOf(metadata.Definition{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			payloads = append(payloads, name)
		}
	}
	slices.Sort(payloads)
	known := append(AllowedKinds(), "tool")
	slices.Sort(known)
	if !slices.Equal(payloads, known) {
		t.Fatalf("metadata kinds %v differ from allowlist+tool %v: decide for the new kind", payloads, known)
	}
	if kindAllowed("tool") {
		t.Fatal("tool must never be allowed")
	}
	if !slices.IsSorted(allowedKinds) {
		t.Fatal("allowedKinds must stay sorted")
	}
	// AllowedKinds is a copy.
	k := AllowedKinds()
	k[0] = "tool"
	if kindAllowed("tool") || AllowedKinds()[0] == "tool" {
		t.Fatal("AllowedKinds leaks its backing array")
	}
}

func TestApplyProposalRefusesEveryNonAllowlistedKind(t *testing.T) {
	vendor := fixtureDoc(t, exVendor)
	page := parseOK(t, proposalMap(addPageOp(t, fixtureDoc(t, exWorkflow), "page.contact.summary", "S"))).Operations[0]
	toolDef := defByID(t, vendor, "tool.vendor.notify")
	toolHash := metadata.DefinitionHash(toolDef)
	pageDef := *page.Definition

	tests := []struct {
		name string
		doc  metadata.Document
		op   PatchOperation
	}{
		{"add a tool", vendor, PatchOperation{Op: "add", Target: "tool.vendor.other", Definition: &metadata.Definition{ID: "tool.vendor.other", Kind: "tool", Tool: toolDef.Tool}, Rationale: "x"}},
		{"update an existing tool", vendor, PatchOperation{Op: "update", Target: "tool.vendor.notify", ExpectedHash: toolHash, Definition: &toolDef, Rationale: "x"}},
		{"delete an existing tool", vendor, PatchOperation{Op: "delete", Target: "tool.vendor.notify", ExpectedHash: toolHash, Rationale: "x"}},
		{"a tool definition under an allowed target", vendor, PatchOperation{Op: "add", Target: "page.vendor.new", Definition: &metadata.Definition{ID: "page.vendor.new", Kind: "tool", Tool: toolDef.Tool}, Rationale: "x"}},
		{"a tool payload under a page definition", vendor, func() PatchOperation {
			d := pageDef
			d.ID, d.Tool = "page.vendor.new", toolDef.Tool
			return PatchOperation{Op: "add", Target: "page.vendor.new", Definition: &d, Rationale: "x"}
		}()},
	}
	for _, kind := range []string{"policy", "role", "secret", "release", "approval", "acceptance", "sandbox", "tenant", "user", "tool"} {
		tests = append(tests, struct {
			name string
			doc  metadata.Document
			op   PatchOperation
		}{"target kind " + kind, vendor, PatchOperation{Op: "delete", Target: kind + ".x", ExpectedHash: toolHash, Rationale: "x"}})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before := docBytes(t, tc.doc)
			out, conflicts, err := ApplyProposal(tc.doc, PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{tc.op}})
			if err != nil {
				t.Fatal(err)
			}
			if len(conflicts) != 1 || conflicts[0].Category != ConflictForbiddenKind || len(out.Definitions) != 0 {
				t.Fatalf("%+v", conflicts)
			}
			if !bytes.Equal(before, docBytes(t, tc.doc)) {
				t.Fatal("input mutated")
			}
		})
	}
}

// A draft whose id promises an allowed kind but whose stored kind is a tool: the
// existing definition is judged by its kind, not by its id.
func TestApplyProposalRefusesToReplaceAnExistingToolUnderAnAllowedId(t *testing.T) {
	doc := fixtureDoc(t, exVendor)
	for i := range doc.Definitions {
		if doc.Definitions[i].ID == "tool.vendor.notify" {
			doc.Definitions[i].ID = "rule.vendor.notify" // inconsistent on purpose
		}
	}
	disguised := defByID(t, doc, "rule.vendor.notify")
	replacement := defByID(t, doc, "rule.vendor.named")
	replacement.ID = "rule.vendor.notify"
	for _, op := range []PatchOperation{
		{Op: "update", Target: "rule.vendor.notify", ExpectedHash: metadata.DefinitionHash(disguised), Definition: &replacement, Rationale: "x"},
		{Op: "delete", Target: "rule.vendor.notify", ExpectedHash: metadata.DefinitionHash(disguised), Rationale: "x"},
	} {
		_, conflicts, err := ApplyProposal(doc, PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{op}})
		if err != nil || len(conflicts) != 1 || conflicts[0].Category != ConflictForbiddenKind {
			t.Fatalf("%s: %v %+v", op.Op, err, conflicts)
		}
	}
}

func TestApplyProposalValidatesDefinitionsWithTheMetadataSchema(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	base := parseOK(t, proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary"))).Operations[0]
	mutate := func(f func(d *metadata.Definition)) PatchProposal {
		op := base
		d, _ := cloneJSON(*op.Definition)
		f(&d)
		op.Definition = &d
		return PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{op}}
	}
	for name, tc := range map[string]struct {
		p    PatchProposal
		code string
	}{
		"namespace differs from the document": {mutate(func(d *metadata.Definition) { d.Namespace = "other.ns" }), "namespace_mismatch"},
		"schema version differs":              {mutate(func(d *metadata.Definition) { d.SchemaVersion = "1.0.0" }), "schema_version_mismatch"},
		"label is empty":                      {mutate(func(d *metadata.Definition) { d.Label = "" }), "schema_violation"},
		"page without components":             {mutate(func(d *metadata.Definition) { d.Page.Components = nil }), "schema_violation"},
	} {
		t.Run(name, func(t *testing.T) {
			r, err := applyProposal(doc, tc.p)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.conflicts) != 1 || r.conflicts[0].Category != ConflictInvalid || r.conflicts[0].Target != "page.contact.summary" {
				t.Fatalf("%+v", r.conflicts)
			}
			if len(r.diagnostics) != 1 || r.diagnostics[0].Code != tc.code || !strings.HasPrefix(r.diagnostics[0].Path, "/page.contact.summary") {
				t.Fatalf("diagnostics %+v", r.diagnostics)
			}
			if len(r.doc.Definitions) != 0 {
				t.Fatal("document returned with a conflict")
			}
		})
	}
}

func TestApplyProposalRefusesRedactionPlaceholders(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	for _, label := range []string{"Contact " + marker("secret"), redactionPrefix + "x]"} {
		op := updateLabelOp(t, doc, "entity.contact", label)
		_, conflicts, err := applyOps(t, doc, op)
		if err != nil || len(conflicts) != 1 || conflicts[0].Category != ConflictInvalid {
			t.Fatalf("%q: %v %+v", label, err, conflicts)
		}
	}
}

func TestApplyProposalWithNoOperationsReturnsTheSortedDraft(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	p := PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{}}
	out, conflicts, err := ApplyProposal(doc, p)
	if err != nil || len(conflicts) != 0 || len(out.Definitions) != len(doc.Definitions) {
		t.Fatal(err, conflicts)
	}
}

func TestApplyProposalReportsAnInvalidDraft(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	doc.Definitions = append(doc.Definitions, doc.Definitions[0]) // repeated id
	_, _, err := ApplyProposal(doc, PatchProposal{Version: PatchProposalVersion})
	if err == nil {
		t.Fatal("a draft that repeats an id must be an error")
	}
}

func TestApplyProposalUnderConcurrency(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	p := parseOK(t, proposalMap(updateLabelOp(t, doc, "entity.contact", "Person")))
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_, conflicts, err := ApplyProposal(doc, p)
			if err == nil && len(conflicts) > 0 {
				err = fmt.Errorf("conflicts %+v", conflicts)
			}
			errs <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

// ------------------------------------------------------- DeriveSensitiveAspects

func compileDoc(t testing.TB, c *compileCounter, doc metadata.Document) compiler.Program {
	t.Helper()
	p, diags, err := c.Compile(doc)
	if err != nil || len(diags) > 0 || !p.Valid() {
		t.Fatalf("compile: %v %v", err, diags)
	}
	return p
}

// cloneDef copies a definition including everything its pointers reach.
func cloneDef(t testing.TB, d metadata.Definition) metadata.Definition {
	t.Helper()
	out, err := cloneJSON(d)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDeriveSensitiveAspects(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	c := newCompiler(t)
	base := compileDoc(t, c, doc)

	rule := cloneDef(t, defByID(t, doc, "rule.contact.named"))
	ruleHash := metadata.DefinitionHash(rule)
	rule.Rule.Expression.Args[1].Value = json.RawMessage(`"x"`)

	wf := cloneDef(t, defByID(t, doc, "workflow.contact.review"))
	wfHash := metadata.DefinitionHash(wf)
	for i := range wf.Workflow.States {
		if wf.Workflow.States[i].Type == "approval" {
			wf.Workflow.States[i].ApprovalRoles = []string{"admin", "member", "owner"}
		}
	}
	newField := metadata.Definition{ID: "field.contact.nickname", SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: "Nickname", Kind: "field", Field: &metadata.Field{EntityRef: "entity.contact", Type: "string"}}
	entity := cloneDef(t, defByID(t, doc, "entity.contact"))
	entityHash := metadata.DefinitionHash(entity)
	entity.Entity.Fields = append(slices.Clone(entity.Entity.Fields), "field.contact.nickname")

	op := func(kind, id, hash string, d *metadata.Definition) map[string]any {
		m := map[string]any{"op": kind, "target": id, "expected_hash": hash, "rationale": "x"}
		if d != nil {
			m["definition"] = asMap(t, *d)
		}
		return m
	}
	tests := []struct {
		name      string
		ops       []map[string]any
		sensitive []string
		aspects   []string
		targets   []DerivedTarget
	}{
		{"label only", []map[string]any{updateLabelOp(t, doc, "entity.contact", "Person")}, []string{}, []string{"LABEL"},
			[]DerivedTarget{{ID: "entity.contact", Kind: "entity", Op: "update"}}},
		{"page added", []map[string]any{addPageOp(t, doc, "page.contact.summary", "Summary")}, []string{}, []string{"PAGE"},
			[]DerivedTarget{{ID: "page.contact.summary", Kind: "page", Op: "add"}}},
		{"rule logic", []map[string]any{op("update", "rule.contact.named", ruleHash, &rule)}, []string{"rule.contact.named:RULE_LOGIC"}, []string{"RULE_LOGIC"},
			[]DerivedTarget{{ID: "rule.contact.named", Kind: "rule", Op: "update"}}},
		{"approval roles", []map[string]any{op("update", "workflow.contact.review", wfHash, &wf)},
			[]string{"workflow.contact.review:APPROVAL_GATE", "workflow.contact.review:WORKFLOW_GRAPH"}, []string{"APPROVAL_GATE", "WORKFLOW_GRAPH"},
			[]DerivedTarget{{ID: "workflow.contact.review", Kind: "workflow", Op: "update"}}},
		// A new field changes the compiled plans of the rule and the workflow on the
		// entity although their authoring is untouched: the diff reports them as
		// PLAN_ONLY changes of definitions that are not operation targets.
		{"storage", []map[string]any{op("add", "field.contact.nickname", "", &newField), op("update", "entity.contact", entityHash, &entity)},
			[]string{"entity.contact:STORAGE", "field.contact.nickname:STORAGE", "rule.contact.named:PLAN_ONLY", "workflow.contact.review:PLAN_ONLY"}, []string{"PLAN_ONLY", "STORAGE"},
			[]DerivedTarget{{ID: "entity.contact", Kind: "entity", Op: "update"}, {ID: "field.contact.nickname", Kind: "field", Op: "add"},
				{ID: "rule.contact.named", Kind: "rule", Op: "update"}, {ID: "workflow.contact.review", Kind: "workflow", Op: "update"}}},
		{"relationship removed", []map[string]any{deleteOp(t, doc, "relationship.contact.parent")}, []string{"relationship.contact.parent:RELATIONSHIP"}, []string{"RELATIONSHIP"},
			[]DerivedTarget{{ID: "relationship.contact.parent", Kind: "relationship", Op: "delete"}}},
		{"label plus rule logic", []map[string]any{updateLabelOp(t, doc, "page.contact.edit", "Editor"), op("update", "rule.contact.named", ruleHash, &rule)},
			[]string{"rule.contact.named:RULE_LOGIC"}, []string{"LABEL", "RULE_LOGIC"},
			[]DerivedTarget{{ID: "page.contact.edit", Kind: "page", Op: "update"}, {ID: "rule.contact.named", Kind: "rule", Op: "update"}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			patched := applyOK(t, doc, tc.ops...)
			if tc.name == "relationship removed" {
				// The compiler requires the relationship's absence to be harmless: fine here.
				_ = patched
			}
			derived, err := DeriveSensitiveAspects(base, compileDoc(t, c, patched))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(derived.Sensitive, tc.sensitive) {
				t.Errorf("sensitive %v, want %v", derived.Sensitive, tc.sensitive)
			}
			if !reflect.DeepEqual(derived.Aspects, tc.aspects) {
				t.Errorf("aspects %v, want %v", derived.Aspects, tc.aspects)
			}
			if !reflect.DeepEqual(derived.Targets, tc.targets) {
				t.Errorf("targets %+v, want %+v", derived.Targets, tc.targets)
			}
		})
	}
}

func TestDeriveSensitiveAspectsFirstProgramAndErrors(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	c := newCompiler(t)
	prog := compileDoc(t, c, doc)
	// A draft that does not compile has no program: everything it brings is added.
	derived, err := DeriveSensitiveAspects(compiler.Program{}, prog)
	if err != nil {
		t.Fatal(err)
	}
	if len(derived.Sensitive) == 0 || len(derived.Targets) != len(doc.Definitions) {
		t.Fatalf("first program: %d sensitive, %d targets", len(derived.Sensitive), len(derived.Targets))
	}
	for _, target := range derived.Targets {
		if target.Op != "add" {
			t.Fatalf("%+v", target)
		}
	}
	// An unchanged program yields an empty, non-nil result.
	same, err := DeriveSensitiveAspects(prog, prog)
	if err != nil || same.Sensitive == nil || len(same.Sensitive) != 0 || same.Targets == nil || len(same.Targets) != 0 || same.Aspects == nil {
		t.Fatalf("%+v %v", same, err)
	}
	if _, err = DeriveSensitiveAspects(prog, compiler.Program{}); err == nil {
		t.Fatal("an invalid patched program must be refused")
	}
	// Deterministic.
	a, _ := DeriveSensitiveAspects(compiler.Program{}, prog)
	b, _ := DeriveSensitiveAspects(compiler.Program{}, prog)
	if !reflect.DeepEqual(a, b) || !slices.IsSorted(a.Sensitive) {
		t.Fatal("derivation is not deterministic or not sorted")
	}
}

func TestSensitiveAspectVocabulary(t *testing.T) {
	for aspect, want := range map[string]bool{
		compiler.AspectLabel: false, compiler.AspectPage: false,
		compiler.AspectStorage: true, compiler.AspectRuleLogic: true, compiler.AspectRuleFinancial: true,
		compiler.AspectWorkflowGraph: true, compiler.AspectWorkflowTrigger: true, compiler.AspectApprovalGate: true,
		compiler.AspectToolPin: true, compiler.AspectToolDeclaration: true, compiler.AspectRelationship: true,
		compiler.AspectPlanOnly: true, compiler.AspectUnclassified: true,
		"A_FUTURE_ASPECT": true, "": true,
	} {
		if SensitiveAspect(aspect) != want {
			t.Errorf("SensitiveAspect(%q) = %v, want %v", aspect, !want, want)
		}
	}
}
