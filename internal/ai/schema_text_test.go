package ai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/intent"
	js "github.com/santhosh-tekuri/jsonschema/v6"
)

// The closed output schemas are guidance for a provider that can constrain its
// output; the strict decoders are the gate. These tests pin that the two agree on the
// proposals the decoders accept and that the schema text itself keeps the closed shape.

type noLoader struct{}

func (noLoader) Load(string) (any, error) { return nil, jsonschemaNoLoad }

var jsonschemaNoLoad = strErr("external schema loading is disabled")

type strErr string

func (e strErr) Error() string { return string(e) }

func compileSchema(t *testing.T, raw []byte) *js.Schema {
	t.Helper()
	doc, err := js.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	c := js.NewCompiler()
	c.DefaultDraft(js.Draft2020)
	c.UseLoader(noLoader{})
	if err = c.AddResource("urn:test:schema", doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("urn:test:schema")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func decodeInstance(t *testing.T, raw []byte) any {
	t.Helper()
	v, err := js.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestPatchOutputSchemaAcceptsWhatTheDecoderAccepts(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	schema := compileSchema(t, mustSchema(t, doc))
	cases := map[string]any{
		"update a label":        proposalMap(updateLabelOp(t, doc, "entity.contact", "Customer")),
		"add a page":            proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary")),
		"delete a relationship": proposalMap(deleteOp(t, doc, "relationship.contact.parent")),
		"questions only": func() map[string]any {
			p := proposalMap()
			p["unresolved_requirements"] = []any{map[string]any{"id": "q1", "question": "Which currency applies?", "category": "approval_threshold"}}
			return p
		}(),
		"with advice": func() map[string]any {
			p := proposalMap(updateLabelOp(t, doc, "entity.contact", "Customer"))
			p["assumptions"] = []any{"The label is user facing."}
			p["risk_flags"] = []any{map[string]any{"code": "LABEL", "detail": "Presentation only."}}
			return p
		}(),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			canonical, _, err := CanonicalPatch(parseOK(t, payload))
			if err != nil {
				t.Fatal(err)
			}
			if err = schema.Validate(decodeInstance(t, canonical)); err != nil {
				t.Fatalf("the decoder accepted what the schema refuses:\n%v\n%s", err, canonical)
			}
		})
	}
}

func TestPatchOutputSchemaRefusesStructuralViolations(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	schema := compileSchema(t, mustSchema(t, doc))
	base := func() map[string]any {
		return deepCopy(t, proposalMap(updateLabelOp(t, doc, "entity.contact", "Customer")))
	}
	op := func(m map[string]any) map[string]any { return m["operations"].([]any)[0].(map[string]any) }
	for name, mutate := range map[string]func(map[string]any){
		"extra top-level field":  func(m map[string]any) { m["change_class"] = "PRESENTATION" },
		"extra operation field":  func(m map[string]any) { op(m)["risk"] = "LOW" },
		"wrong version":          func(m map[string]any) { m["version"] = "ai-patch-proposal-v2" },
		"unknown op":             func(m map[string]any) { op(m)["op"] = "replace" },
		"tool target":            func(m map[string]any) { op(m)["target"] = "tool.vendor.notify" },
		"policy-like target":     func(m map[string]any) { op(m)["target"] = "policy.admin" },
		"role-like target":       func(m map[string]any) { op(m)["target"] = "role.owner" },
		"update without hash":    func(m map[string]any) { op(m)["expected_hash"] = "" },
		"short hash":             func(m map[string]any) { op(m)["expected_hash"] = "abc" },
		"upper-case hash":        func(m map[string]any) { op(m)["expected_hash"] = strings.Repeat("A", 64) },
		"add with a hash":        func(m map[string]any) { op(m)["op"] = "add"; op(m)["target"] = "page.contact.summary" },
		"delete with definition": func(m map[string]any) { op(m)["op"] = "delete" },
		"missing rationale":      func(m map[string]any) { delete(op(m), "rationale") },
		"too many operations": func(m map[string]any) {
			list := make([]any, MaxOperations+1)
			for i := range list {
				list[i] = op(m)
			}
			m["operations"] = list
		},
		"null list":              func(m map[string]any) { m["assumptions"] = nil },
		"tool definition":        func(m map[string]any) { op(m)["definition"].(map[string]any)["kind"] = "tool" },
		"definition extra field": func(m map[string]any) { op(m)["definition"].(map[string]any)["approval"] = true },
	} {
		m := base()
		mutate(m)
		if err := schema.Validate(decodeInstance(t, mustJSON(t, m))); err == nil {
			t.Errorf("%s: the schema accepted it", name)
		}
	}
}

func TestPatchOutputSchemaShape(t *testing.T) {
	for _, version := range schemaVersions() {
		raw, err := PatchOutputSchema(version)
		if err != nil {
			t.Fatalf("%s: %v", version, err)
		}
		again, _ := PatchOutputSchema(version)
		if !bytes.Equal(raw, again) {
			t.Fatalf("%s: not deterministic", version)
		}
		compileSchema(t, raw)
		var root map[string]any
		if err = json.Unmarshal(raw, &root); err != nil || root["additionalProperties"] != false {
			t.Fatalf("%s: the root is not closed", version)
		}
		text := string(raw)
		for _, forbidden := range []string{`"const":"tool"`, "tool_ref", "approval_roles\":{\"$ref\":\"#/$defs/tool"} {
			if strings.Contains(text, forbidden) && forbidden == `"const":"tool"` {
				t.Fatalf("%s: a tool definition is representable", version)
			}
		}
		for _, name := range []string{"change_class", "risk", "provenance", "requiredness", "actor"} {
			if strings.Contains(text, `"`+name+`"`) {
				t.Fatalf("%s: the schema names the server-derived field %s", version, name)
			}
		}
	}
	if _, err := PatchOutputSchema("9.9.9"); err == nil {
		t.Fatal("an unknown document version has no schema")
	}
}

func TestRequirementOutputSchemaAcceptsWhatTheDecoderAccepts(t *testing.T) {
	schema := compileSchema(t, RequirementOutputSchema())
	good := reqPayload(t, func(p *RequirementProposal) {
		p.OpenQuestions = []UnresolvedRequirement{{ID: "q1", Question: "Who approves?", Category: "approval_gate"}}
		p.Assumptions = []string{"Amounts are in USD."}
	})
	p, err := ParseRequirementProposal(good)
	if err != nil {
		t.Fatal(err)
	}
	canonical, _, err := CanonicalRequirementProposal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = schema.Validate(decodeInstance(t, canonical)); err != nil {
		t.Fatalf("the decoder accepted what the schema refuses:\n%v\n%s", err, canonical)
	}
	var m map[string]any
	_ = json.Unmarshal(canonical, &m)
	for name, mutate := range map[string]func(map[string]any){
		"requiredness": func(m map[string]any) { m["requirements"].([]any)[0].(map[string]any)["requiredness"] = "OPTIONAL" },
		"provenance": func(m map[string]any) {
			m["requirements"].([]any)[0].(map[string]any)["provenance"] = "AUTHENTICATED_HUMAN"
		},
		"status":           func(m map[string]any) { m["requirements"].([]any)[0].(map[string]any)["status"] = "RESOLVED" },
		"unknown category": func(m map[string]any) { m["requirements"].([]any)[0].(map[string]any)["category"] = "pricing" },
		"wrong version":    func(m map[string]any) { m["version"] = PatchProposalVersion },
		"extra field":      func(m map[string]any) { m["approved"] = true },
		"bad semantics": func(m map[string]any) {
			m["requirements"].([]any)[0].(map[string]any)["semantics"].(map[string]any)["op"] = "ROUGHLY"
		},
	} {
		c := deepCopy(t, m)
		mutate(c)
		if schema.Validate(decodeInstance(t, mustJSON(t, c))) == nil {
			t.Errorf("%s: the schema accepted it", name)
		}
	}
	_ = intent.CategoryApprovalGate
}
