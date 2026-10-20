package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

const (
	exWorkflow = "workflow-draft-v1.2.json" // schema 1.2.0: entity, fields, page, rule, workflow, relationship
	exVendor   = "vendor-draft-v1.json"     // schema 1.0.0: includes a tool definition
)

// fixtureDoc loads and validates a published example document.
func fixtureDoc(t testing.TB, name string) metadata.Document {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	src, err := v.Parse(raw)
	if err != nil {
		t.Fatalf("example %s: %v", name, err)
	}
	var doc metadata.Document
	if err = json.Unmarshal(src.JSON(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func testCatalog() compiler.Catalog {
	return compiler.Catalog{Version: "test-v1", Permissions: []string{"data.read", "data.write", "vendor.notify"}, Tools: []string{}}
}

// compileCounter wraps the real validator and compiler the way the platform's
// CompileFunc does and counts the calls.
type compileCounter struct {
	v     *metadata.Validator
	calls atomic.Int64
}

func newCompiler(t testing.TB) *compileCounter {
	t.Helper()
	v, err := metadata.NewValidator()
	if err != nil {
		t.Fatal(err)
	}
	return &compileCounter{v: v}
}

func (c *compileCounter) Compile(doc metadata.Document) (compiler.Program, []compiler.Diagnostic, error) {
	c.calls.Add(1)
	raw, err := json.Marshal(doc)
	if err != nil {
		return compiler.Program{}, nil, err
	}
	src, err := c.v.Parse(raw)
	if err != nil {
		var invalid *metadata.Invalid
		if errors.As(err, &invalid) {
			out := []compiler.Diagnostic{}
			for _, i := range invalid.Issues {
				out = append(out, compiler.Diagnostic{Path: i.Path, Code: i.Code, Message: i.Message})
			}
			return compiler.Program{}, out, nil
		}
		return compiler.Program{}, []compiler.Diagnostic{{Code: "invalid_source", Message: "A validated source is required"}}, nil
	}
	p, ds := compiler.Compile(src, testCatalog())
	if len(ds) > 0 {
		return compiler.Program{}, ds, nil
	}
	return p, nil, nil
}

func defByID(t testing.TB, doc metadata.Document, id string) metadata.Definition {
	t.Helper()
	for _, d := range doc.Definitions {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no definition %s", id)
	return metadata.Definition{}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// asMap converts any JSON-encodable value into a generic map for mutation.
func asMap(t testing.TB, v any) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(mustJSON(t, v), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func deepCopy(t testing.TB, m map[string]any) map[string]any { return asMap(t, m) }

// proposalMap builds a valid patch proposal payload around the given operations.
func proposalMap(ops ...map[string]any) map[string]any {
	list := make([]any, len(ops))
	for i, o := range ops {
		list[i] = o
	}
	return map[string]any{
		"version": PatchProposalVersion, "operations": list, "assumptions": []any{},
		"unresolved_requirements": []any{}, "proposed_tests": []any{}, "risk_flags": []any{},
	}
}

// addPageOp is an add of a new page; the definition schema_version follows the document.
func addPageOp(t testing.TB, doc metadata.Document, id, label string) map[string]any {
	t.Helper()
	d := metadata.Definition{
		ID: id, SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: label, Kind: "page",
		Page: &metadata.Page{EntityRef: "entity.contact", Components: []metadata.Component{{Type: "section", Label: "Section", Children: []metadata.Component{{Type: "table", EntityRef: "entity.contact"}}}}},
	}
	return map[string]any{"op": "add", "target": id, "expected_hash": "", "definition": asMap(t, d), "rationale": "Add a summary page."}
}

// updateLabelOp relabels an existing definition (a presentation-only change).
func updateLabelOp(t testing.TB, doc metadata.Document, id, label string) map[string]any {
	t.Helper()
	d := defByID(t, doc, id)
	hash := metadata.DefinitionHash(d)
	d.Label = label
	return map[string]any{"op": "update", "target": id, "expected_hash": hash, "definition": asMap(t, d), "rationale": "Relabel."}
}

func deleteOp(t testing.TB, doc metadata.Document, id string) map[string]any {
	t.Helper()
	return map[string]any{"op": "delete", "target": id, "expected_hash": metadata.DefinitionHash(defByID(t, doc, id)), "rationale": "Remove."}
}

func parseOK(t testing.TB, payload any) PatchProposal {
	t.Helper()
	raw, ok := payload.([]byte)
	if !ok {
		raw = mustJSON(t, payload)
	}
	p, err := ParsePatchProposal(raw)
	if err != nil {
		t.Fatalf("expected a valid proposal: %v\n%s", err, raw)
	}
	return p
}

// fakeClock advances only when asked, so durations in traces are exact.
type fakeClock struct{ now time.Time }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func errClass(err error) string {
	switch {
	case err == nil:
		return "nil"
	case errors.Is(err, ErrServerField):
		return "server_field"
	case errors.Is(err, ErrUnknownField):
		return "unknown_field"
	case errors.Is(err, ErrSchema):
		return "schema"
	case errors.Is(err, ErrForbiddenKind):
		return "forbidden_kind"
	case errors.Is(err, ErrDuplicateKey):
		return "duplicate_key"
	case errors.Is(err, ErrTooLarge):
		return "too_large"
	case errors.Is(err, ErrMalformed):
		return "malformed"
	case errors.Is(err, ErrSemantics):
		return "semantics"
	}
	return "other:" + fmt.Sprint(err)
}

func longString(n int) string { return strings.Repeat("a", n) }
