package data

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/policy"
)

func TestPatchPublicSizeBoundIncludesEscaping(t *testing.T) {
	e := compiler.Entity{ID: "entity.x", Fields: []compiler.Field{{ID: "field.x.id", CoreID: true, Type: "string"}, {ID: "field.x.a", Type: "string"}, {ID: "field.x.b", Type: "string"}}}
	hidden := []string{"field.x.id", "field.x.a", "field.x.b"}
	if visiblePatchBudget(e, nil, hidden) != nil {
		t.Fatal("bounded public schema denied")
	}
	raw, _ := json.Marshal(map[string]any{"field.x.a": strings.Repeat("\x01", 4096), "field.x.b": strings.Repeat("\x02", 4096)})
	if len(raw) > MaxRecordBytes {
		t.Fatal("admitted schema can exceed byte budget")
	}
	e.Fields = append(e.Fields, compiler.Field{ID: "field.x.c", Type: "string"})
	hidden = append(hidden, "field.x.c")
	if visiblePatchBudget(e, nil, hidden) != ErrBudget {
		t.Fatal("escaping can exceed public bound")
	}
	if visiblePatchBudget(e, map[string]any{"field.x.id": "11111111-1111-1111-1111-111111111111", "field.x.a": "small", "field.x.b": "small", "field.x.c": "small"}, nil) != nil {
		t.Fatal("fully readable wide schema regressed")
	}
}
func TestFieldScopeCoverageIsConservative(t *testing.T) {
	entity := policy.DataScope{Kind: "OWNER"}
	for _, s := range []policy.DataScope{{Kind: "ALL"}, entity, {Kind: "OR", Children: []policy.DataScope{{Kind: "TEAM"}, {Kind: "ALL"}}}} {
		if !coversEntity(s, entity) {
			t.Fatal("safe coverage denied")
		}
	}
	for _, s := range []policy.DataScope{{Kind: "TEAM"}, {Kind: "NONE"}, {Kind: "AND", Children: []policy.DataScope{entity, {Kind: "SHARE"}}}} {
		if coversEntity(s, entity) {
			t.Fatal("unsafe coverage admitted")
		}
	}
}
