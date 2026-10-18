package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

func TestParsePatchProposalAcceptsValidProposals(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	cases := map[string]map[string]any{
		"add a page":            proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary")),
		"relabel an entity":     proposalMap(updateLabelOp(t, doc, "entity.contact", "Person")),
		"delete a relationship": proposalMap(deleteOp(t, doc, "relationship.contact.parent")),
		"questions only": func() map[string]any {
			p := proposalMap()
			p["unresolved_requirements"] = []any{map[string]any{"id": "q1", "question": "Which currency applies?", "category": "approval_threshold"}}
			return p
		}(),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			p := parseOK(t, payload)
			if p.Version != PatchProposalVersion {
				t.Fatalf("version %q", p.Version)
			}
			if p.Operations == nil || p.Assumptions == nil || p.UnresolvedRequirements == nil || p.ProposedTests == nil || p.RiskFlags == nil {
				t.Fatal("empty lists must be non-nil after parsing")
			}
		})
	}
}

func TestParsePatchProposalRejects(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	good := func() map[string]any { return proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary")) }
	op := func(m map[string]any) map[string]any { return m["operations"].([]any)[0].(map[string]any) }
	mutate := func(f func(m map[string]any)) any {
		m := deepCopy(t, good())
		f(m)
		return m
	}
	hexHash := strings.Repeat("a", 64)
	tests := []struct {
		name    string
		payload any // map (marshalled) or []byte
		want    string
	}{
		{"empty", []byte(``), "malformed"},
		{"not json", []byte(`{`), "malformed"},
		{"trailing data", append(mustJSON(t, good()), []byte(` {}`)...), "malformed"},
		{"invalid utf8", []byte("{\"version\":\"\xff\"}"), "malformed"},
		{"nul byte", []byte("{\"version\":\"a\\u0000b\"}"), "malformed"},
		{"array root", []byte(`[]`), "schema"},
		{"null root", []byte(`null`), "schema"},
		{"string root", []byte(`"x"`), "schema"},
		{"oversized", append(append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), MaxOutputBytes)...), []byte(`"}`)...), "too_large"},
		{"too deep", []byte(strings.Repeat("[", 40) + strings.Repeat("]", 40)), "too_large"},
		{"duplicate key", []byte(`{"version":"` + PatchProposalVersion + `","version":"` + PatchProposalVersion + `"}`), "duplicate_key"},
		{"case folded duplicate", []byte(`{"assumptions":[],"Assumptions":[]}`), "duplicate_key"},
		{"unicode folded duplicate", []byte(`{"assumptions":[],"a` + "ſ" + `sumptions":[]}`), "duplicate_key"},
		{"nested duplicate", []byte(`{"operations":[{"op":"add","op":"add"}]}`), "duplicate_key"},
		{"inexact number", []byte(`{"operations":[],"x":0.1234567890123456789}`), "schema"},
		{"unknown top-level field", mutate(func(m map[string]any) { m["extra"] = 1 }), "unknown_field"},
		{"unknown operation field", mutate(func(m map[string]any) { op(m)["extra"] = 1 }), "unknown_field"},
		{"unknown definition field", mutate(func(m map[string]any) { op(m)["definition"].(map[string]any)["extra"] = 1 }), "unknown_field"},
		{"wrong-case key", mutate(func(m map[string]any) { m["Version"] = m["version"]; delete(m, "version") }), "unknown_field"},
		{"missing version", mutate(func(m map[string]any) { delete(m, "version") }), "schema"},
		{"missing operations", mutate(func(m map[string]any) { delete(m, "operations") }), "schema"},
		{"missing assumptions", mutate(func(m map[string]any) { delete(m, "assumptions") }), "schema"},
		{"missing risk_flags", mutate(func(m map[string]any) { delete(m, "risk_flags") }), "schema"},
		{"missing op", mutate(func(m map[string]any) { delete(op(m), "op") }), "schema"},
		{"missing expected_hash", mutate(func(m map[string]any) { delete(op(m), "expected_hash") }), "schema"},
		{"missing rationale", mutate(func(m map[string]any) { delete(op(m), "rationale") }), "schema"},
		{"null list", mutate(func(m map[string]any) { m["assumptions"] = nil }), "schema"},
		{"null scalar", mutate(func(m map[string]any) { op(m)["rationale"] = nil }), "schema"},
		{"list is a string", mutate(func(m map[string]any) { m["operations"] = "none" }), "schema"},
		{"version downgrade", mutate(func(m map[string]any) { m["version"] = "ai-patch-proposal-v0" }), "schema"},
		{"version upgrade", mutate(func(m map[string]any) { m["version"] = "ai-patch-proposal-v2" }), "schema"},
		{"requirement version", mutate(func(m map[string]any) { m["version"] = RequirementProposalVersion }), "schema"},
		{"unknown op", mutate(func(m map[string]any) { op(m)["op"] = "replace" }), "schema"},
		{"upper-case op", mutate(func(m map[string]any) { op(m)["op"] = "ADD" }), "schema"},
		{"target is not an id", mutate(func(m map[string]any) { op(m)["target"] = "contact" }), "schema"},
		{"target upper case", mutate(func(m map[string]any) { op(m)["target"] = "Page.contact" }), "schema"},
		{"target too long", mutate(func(m map[string]any) { op(m)["target"] = "page." + longString(170) }), "schema"},
		{"tool target", mutate(func(m map[string]any) { op(m)["target"] = "tool.vendor.notify" }), "forbidden_kind"},
		{"policy-like target", mutate(func(m map[string]any) { op(m)["target"] = "policy.admin" }), "forbidden_kind"},
		{"role-like target", mutate(func(m map[string]any) { op(m)["target"] = "role.owner" }), "forbidden_kind"},
		{"secret-like target", mutate(func(m map[string]any) { op(m)["target"] = "secret.db" }), "forbidden_kind"},
		{"release-like target", mutate(func(m map[string]any) { op(m)["target"] = "release.v1" }), "forbidden_kind"},
		{"approval-like target", mutate(func(m map[string]any) { op(m)["target"] = "approval.x" }), "forbidden_kind"},
		{"sandbox-like target", mutate(func(m map[string]any) { op(m)["target"] = "sandbox.x" }), "forbidden_kind"},
		{"definition of kind tool", mutate(func(m map[string]any) { op(m)["definition"].(map[string]any)["kind"] = "tool" }), "forbidden_kind"},
		{"tool payload", mutate(func(m map[string]any) { op(m)["definition"].(map[string]any)["tool"] = map[string]any{} }), "forbidden_kind"},
		{"definition id differs from target", mutate(func(m map[string]any) { op(m)["definition"].(map[string]any)["id"] = "page.contact.other" }), "schema"},
		{"definition kind differs from target", mutate(func(m map[string]any) {
			op(m)["target"] = "rule.contact.summary"
			op(m)["definition"].(map[string]any)["id"] = "rule.contact.summary"
		}), "schema"},
		{"add with expected hash", mutate(func(m map[string]any) { op(m)["expected_hash"] = hexHash }), "schema"},
		{"update without hash", mutate(func(m map[string]any) { op(m)["op"] = "update" }), "schema"},
		{"update with upper-case hash", mutate(func(m map[string]any) { op(m)["op"] = "update"; op(m)["expected_hash"] = strings.ToUpper(hexHash) }), "schema"},
		{"update with short hash", mutate(func(m map[string]any) { op(m)["op"] = "update"; op(m)["expected_hash"] = "abc" }), "schema"},
		{"delete with definition", mutate(func(m map[string]any) { op(m)["op"] = "delete"; op(m)["expected_hash"] = hexHash }), "schema"},
		{"add without definition", mutate(func(m map[string]any) { delete(op(m), "definition") }), "schema"},
		{"empty rationale", mutate(func(m map[string]any) { op(m)["rationale"] = "" }), "schema"},
		{"rationale too long", mutate(func(m map[string]any) { op(m)["rationale"] = longString(MaxTextRunes + 1) }), "schema"},
		{"rationale control character", mutate(func(m map[string]any) { op(m)["rationale"] = "ok\x07bad" }), "schema"},
		{"rationale bidi override", mutate(func(m map[string]any) { op(m)["rationale"] = "ok‮bad" }), "schema"},
		{"rationale tag character", mutate(func(m map[string]any) { op(m)["rationale"] = "ok\U000E0041" }), "schema"},
		{"label control character", mutate(func(m map[string]any) { op(m)["definition"].(map[string]any)["label"] = "bad\u0000label" }), "malformed"},
		{"label zero width space", mutate(func(m map[string]any) { op(m)["definition"].(map[string]any)["label"] = "a​b" }), "schema"},
		{"definition violates metadata schema", mutate(func(m map[string]any) {
			op(m)["definition"].(map[string]any)["page"].(map[string]any)["components"] = "x"
		}), "schema"},
		{"definition id pattern", mutate(func(m map[string]any) {
			op(m)["target"] = "page.Contact"
		}), "schema"},
		{"duplicate target", mutate(func(m map[string]any) {
			m["operations"] = append(m["operations"].([]any), deepCopy(t, op(m)))
		}), "schema"},
		{"too many operations", mutate(func(m map[string]any) {
			ops := []any{}
			for i := 0; i <= MaxOperations; i++ {
				ops = append(ops, deepCopy(t, op(m)))
			}
			m["operations"] = ops
		}), "too_large"},
		{"empty proposal", mutate(func(m map[string]any) { m["operations"] = []any{} }), "schema"},
		{"too many assumptions", mutate(func(m map[string]any) {
			a := []any{}
			for i := 0; i <= MaxListItems; i++ {
				a = append(a, "x")
			}
			m["assumptions"] = a
		}), "too_large"},
		{"empty assumption", mutate(func(m map[string]any) { m["assumptions"] = []any{""} }), "schema"},
		{"unresolved bad id", mutate(func(m map[string]any) {
			m["unresolved_requirements"] = []any{map[string]any{"id": "Q 1", "question": "?"}}
		}), "schema"},
		{"unresolved duplicate id", mutate(func(m map[string]any) {
			q := map[string]any{"id": "q1", "question": "Which?"}
			m["unresolved_requirements"] = []any{q, deepCopy(t, q)}
		}), "schema"},
		{"unresolved bad category", mutate(func(m map[string]any) {
			m["unresolved_requirements"] = []any{map[string]any{"id": "q1", "question": "Which?", "category": "Bad Category"}}
		}), "schema"},
		{"unresolved server field", mutate(func(m map[string]any) {
			m["unresolved_requirements"] = []any{map[string]any{"id": "q1", "question": "Which?", "status": "RESOLVED"}}
		}), "server_field"},
		{"risk flag bad code", mutate(func(m map[string]any) { m["risk_flags"] = []any{map[string]any{"code": "1 bad", "detail": "x"}} }), "schema"},
		{"risk flag server field", mutate(func(m map[string]any) {
			m["risk_flags"] = []any{map[string]any{"code": "x", "detail": "x", "risk": "LOW"}}
		}), "server_field"},
		{"proposed test bad expectation", mutate(func(m map[string]any) {
			m["proposed_tests"] = []any{map[string]any{"id": "t1", "requirement_id": "r1", "expect": map[string]any{"outcome": "MAYBE"}}}
		}), "schema"},
		{"proposed test given not scalar", mutate(func(m map[string]any) {
			m["proposed_tests"] = []any{map[string]any{"id": "t1", "requirement_id": "r1", "given": map[string]any{"field.contact.name": []any{1}}, "expect": map[string]any{"outcome": "HOLDS"}}}
		}), "schema"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, ok := tc.payload.([]byte)
			if !ok {
				raw = mustJSON(t, tc.payload)
			}
			_, err := ParsePatchProposal(raw)
			if err == nil {
				t.Fatalf("accepted:\n%.300s", raw)
			}
			if got := errClass(err); got != tc.want {
				t.Fatalf("class %s, want %s (%v)", got, tc.want, err)
			}
			var perr *ProposalError
			if !errors.As(err, &perr) || !errors.Is(err, ErrProposal) {
				t.Fatalf("not a *ProposalError wrapping ErrProposal: %T", err)
			}
		})
	}
}

func TestParsePatchProposalNamesServerDerivedFields(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	names := []string{"change_class", "class", "risk", "risk_level", "sensitive", "sensitive_aspects", "aspects", "derived",
		"status", "provenance", "requiredness", "requiredness_basis", "actor", "actor_id", "author", "approved_by", "approval",
		"acceptance", "hash", "proposal_hash", "proposal_id", "revision", "created_at", "tenant_id", "role", "roles",
		"permissions", "policy", "release", "sandbox", "acknowledge", "Status", "RISK-LEVEL", "changeClass"}
	for _, name := range names {
		for place, put := range map[string]func(m map[string]any, v any){
			"proposal":  func(m map[string]any, v any) { m[name] = v },
			"operation": func(m map[string]any, v any) { m["operations"].([]any)[0].(map[string]any)[name] = v },
			"definition": func(m map[string]any, v any) {
				m["operations"].([]any)[0].(map[string]any)["definition"].(map[string]any)[name] = v
			},
		} {
			m := proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary"))
			put(m, "x")
			_, err := ParsePatchProposal(mustJSON(t, m))
			// "role"/"permissions"-like keys are not fields of any struct, so they are server fields.
			if got := errClass(err); got != "server_field" {
				t.Errorf("%s in %s: class %s (%v)", name, place, got, err)
			}
		}
	}
}

func TestParsePatchProposalCanonicalForm(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	p := parseOK(t, proposalMap(
		addPageOp(t, doc, "page.contact.summary", "Summary"),
		updateLabelOp(t, doc, "entity.contact", "Person"),
		deleteOp(t, doc, "relationship.contact.parent")))
	canonical, hash, err := CanonicalPatch(p)
	if err != nil {
		t.Fatal(err)
	}
	// Fixed point: the canonical bytes parse and re-encode to themselves.
	again, err := ParsePatchProposal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	canonical2, hash2, err := CanonicalPatch(again)
	if err != nil || !bytes.Equal(canonical, canonical2) || hash != hash2 {
		t.Fatalf("canonical form is not a fixed point: %v", err)
	}
	if len(hash) != 64 || sha256Hex(canonical) != hash {
		t.Fatal("hash is not the SHA-256 of the canonical bytes")
	}
	// Key order and whitespace do not matter.
	var generic map[string]any
	if err = json.Unmarshal(canonical, &generic); err != nil {
		t.Fatal(err)
	}
	spaced, _ := json.MarshalIndent(generic, "  ", "\t")
	third, err := ParsePatchProposal(spaced)
	if err != nil {
		t.Fatal(err)
	}
	if c3, h3, _ := CanonicalPatch(third); h3 != hash || !bytes.Equal(c3, canonical) {
		t.Fatal("whitespace or key order changed the canonical form")
	}
	// A different proposal has a different hash.
	other := parseOK(t, proposalMap(addPageOp(t, doc, "page.contact.summary", "Other")))
	if _, h, _ := CanonicalPatch(other); h == hash {
		t.Fatal("distinct proposals share a hash")
	}
	// Programmatic proposals may leave top-level lists nil.
	if _, _, err = CanonicalPatch(PatchProposal{Version: PatchProposalVersion, Operations: p.Operations}); err != nil {
		t.Fatalf("nil top-level lists: %v", err)
	}
	if _, _, err = CanonicalPatch(PatchProposal{Version: "x"}); err == nil {
		t.Fatal("an invalid proposal has no canonical form")
	}
}

func TestParsePatchProposalIsDeterministic(t *testing.T) {
	doc := fixtureDoc(t, exWorkflow)
	raw := mustJSON(t, proposalMap(addPageOp(t, doc, "page.contact.summary", "Summary")))
	var first string
	for i := 0; i < 20; i++ {
		p, err := ParsePatchProposal(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, h, err := CanonicalPatch(p)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = h
		} else if h != first {
			t.Fatal("hash differs between runs")
		}
	}
}

func TestParsePatchProposalErrorsDoNotEchoModelText(t *testing.T) {
	secret := "SENTINEL-" + "d34db33f"
	doc := fixtureDoc(t, exWorkflow)
	for name, m := range map[string]map[string]any{
		"unknown key": func() map[string]any { x := proposalMap(); x[secret] = 1; return x }(),
		"bad op":      proposalMap(map[string]any{"op": secret, "target": "page.a", "expected_hash": "", "rationale": "r"}),
		"bad target":  proposalMap(map[string]any{"op": "add", "target": secret, "expected_hash": "", "rationale": "r"}),
		"bad question": func() map[string]any {
			x := proposalMap()
			x["unresolved_requirements"] = []any{map[string]any{"id": secret, "question": "q"}}
			return x
		}(),
		"given key": func() map[string]any {
			x := proposalMap(addPageOp(t, doc, "page.contact.summary", "S"))
			x["proposed_tests"] = []any{map[string]any{"id": "t1", "requirement_id": "r1", "given": map[string]any{secret: "x"}, "expect": map[string]any{"outcome": "HOLDS"}}}
			return x
		}(),
	} {
		_, err := ParsePatchProposal(mustJSON(t, m))
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if strings.Contains(err.Error(), secret) {
			t.Errorf("%s: error echoes model text: %v", name, err)
		}
	}
}

var _ = metadata.SchemaVersion
