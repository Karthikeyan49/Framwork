package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func toolSample(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.3.json")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// toolActivity returns the activity map of a workflow state in a decoded document.
func toolActivity(t *testing.T, d map[string]any, state string) map[string]any {
	t.Helper()
	for _, def := range d["definitions"].([]any) {
		w, ok := def.(map[string]any)["workflow"].(map[string]any)
		if !ok {
			continue
		}
		for _, s := range w["states"].([]any) {
			if s.(map[string]any)["id"] == state {
				return s.(map[string]any)["activity"].(map[string]any)
			}
		}
	}
	t.Fatal("missing state", state)
	return nil
}
func TestWorkflowSchemaVersions(t *testing.T) {
	for v, want := range map[string]bool{"1.0.0": false, "1.1.0": false, "1.2.0": true, "1.3.0": true, "1.4.0": false, "": false} {
		if WorkflowSchema(v) != want {
			t.Fatal("WorkflowSchema", v)
		}
	}
	if ToolWorkflowSchemaVersion != "1.3.0" || WorkflowSchemaVersion != "1.2.0" || RuleSchemaVersion != "1.1.0" || SchemaVersion != "1.0.0" {
		t.Fatal("published schema versions changed")
	}
}
func TestToolActivitiesValidateUnderSchema13(t *testing.T) {
	v := validator(t)
	source, err := v.Parse(toolSample(t))
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err = json.Unmarshal(source.JSON(), &doc); err != nil || doc.SchemaVersion != ToolWorkflowSchemaVersion {
		t.Fatal("document", err)
	}
	var tool *Activity
	for _, def := range doc.Definitions {
		if def.Workflow == nil {
			continue
		}
		for i, s := range def.Workflow.States {
			if s.ID == "state.notify" {
				tool = def.Workflow.States[i].Activity
			}
		}
	}
	if tool == nil || tool.ToolRef != "tool.vendor_notify" || tool.Version != "1.0.0" || tool.TimeoutMS != 0 || len(tool.Inputs) != 4 || tool.Inputs["vendor_id"] != "field.vendor.id" {
		t.Fatal("typed tool activity", tool)
	}
	round, err := v.Parse(source.JSON())
	if err != nil || round.Hash() != source.Hash() {
		t.Fatal("1.3 round trip changed storage identity")
	}
	compact, _ := json.Marshal(doc)
	if again, err := v.Parse(compact); err != nil || again.Hash() != source.Hash() {
		t.Fatal("typed Go encoding changed 1.3 storage identity", err)
	}
	// JSON-compatible YAML converges on the same source identity.
	if viaYAML, err := v.ParseYAML(toolSample(t)); err != nil || viaYAML.Hash() != source.Hash() {
		t.Fatal("YAML diverged for 1.3", err)
	}
}
func TestToolActivityNegativeCorpus(t *testing.T) {
	v := validator(t)
	many := map[string]any{}
	for i := 0; i < 33; i++ {
		many[fmt.Sprintf("in_%02d", i)] = "field.vendor.id"
	}
	exactly := map[string]any{}
	for i := 0; i < 32; i++ {
		exactly[fmt.Sprintf("in_%02d", i)] = "field.vendor.id"
	}
	set := func(state, key string, value any) func(map[string]any) {
		return func(d map[string]any) { toolActivity(t, d, state)[key] = value }
	}
	remove := func(state, key string) func(map[string]any) {
		return func(d map[string]any) { delete(toolActivity(t, d, state), key) }
	}
	retry := map[string]any{"max_attempts": 1, "initial_backoff_ms": 100, "max_backoff_ms": 100}
	for name, mutate := range map[string]func(map[string]any){
		"unknown property":          set("state.lookup", "url", "https://evil.example/"),
		"host override":             set("state.lookup", "host", "169.254.169.254"),
		"secret":                    set("state.lookup", "secret_ref", "vendor"),
		"timeout on tool":           set("state.lookup", "timeout_ms", 500),
		"compensation local":        set("state.lookup", "compensation_local_ref", "local.noop"),
		"compensation tool":         set("state.lookup", "compensation_tool_ref", "tool.vendor_lookup"),
		"local ref beside tool":     set("state.lookup", "local_ref", "local.noop"),
		"tool ref beside local":     set("state.record", "tool_ref", "tool.vendor_lookup"),
		"inputs on local":           set("state.record", "inputs", map[string]any{"a": "field.vendor.id"}),
		"uppercase ref":             set("state.lookup", "tool_ref", "tool.Vendor"),
		"missing prefix":            set("state.lookup", "tool_ref", "vendor_lookup"),
		"local ref as tool":         set("state.lookup", "tool_ref", "local.noop"),
		"trailing dot":              set("state.lookup", "tool_ref", "tool.vendor."),
		"ref too long":              set("state.lookup", "tool_ref", "tool."+strings.Repeat("a", 156)),
		"ref not string":            set("state.lookup", "tool_ref", 7),
		"missing ref":               remove("state.lookup", "tool_ref"),
		"missing version":           remove("state.lookup", "version"),
		"version not semver":        set("state.lookup", "version", "1.0"),
		"version leading zero":      set("state.lookup", "version", "01.0.0"),
		"version too long":          set("state.lookup", "version", "1.0.0.0.0.0.0.0.0.0.0.0"),
		"missing inputs":            remove("state.lookup", "inputs"),
		"inputs empty":              set("state.lookup", "inputs", map[string]any{}),
		"inputs not object":         set("state.lookup", "inputs", []any{"field.vendor.id"}),
		"inputs over limit":         set("state.lookup", "inputs", many),
		"input name uppercase":      set("state.lookup", "inputs", map[string]any{"Vendor": "field.vendor.id"}),
		"input name leading digit":  set("state.lookup", "inputs", map[string]any{"1v": "field.vendor.id"}),
		"input name too long":       set("state.lookup", "inputs", map[string]any{"a" + strings.Repeat("b", 64): "field.vendor.id"}),
		"input value not field id":  set("state.lookup", "inputs", map[string]any{"vendor_id": "vendor.id"}),
		"input value entity id":     set("state.lookup", "inputs", map[string]any{"vendor_id": "entity.vendor"}),
		"input value literal":       set("state.lookup", "inputs", map[string]any{"vendor_id": 7}),
		"input value object":        set("state.lookup", "inputs", map[string]any{"vendor_id": map[string]any{"field_ref": "field.vendor.id"}}),
		"input value too long":      set("state.lookup", "inputs", map[string]any{"vendor_id": "field." + strings.Repeat("a", 160)}),
		"missing retry":             remove("state.lookup", "retry"),
		"retry unbounded":           set("state.lookup", "retry", map[string]any{"max_attempts": 11, "initial_backoff_ms": 100, "max_backoff_ms": 100}),
		"retry unknown key":         set("state.lookup", "retry", map[string]any{"max_attempts": 1, "initial_backoff_ms": 100, "max_backoff_ms": 100, "jitter": true}),
		"empty activity":            func(d map[string]any) { clear(toolActivity(t, d, "state.lookup")) },
		"definition on old version": func(d map[string]any) { d["definitions"].([]any)[0].(map[string]any)["schema_version"] = "1.2.0" },
		"unknown envelope version":  func(d map[string]any) { d["schema_version"] = "1.4.0" },
	} {
		t.Run(name, func(t *testing.T) {
			var d map[string]any
			if json.Unmarshal(toolSample(t), &d) != nil {
				t.Fatal("fixture")
			}
			mutate(d)
			bad, _ := json.Marshal(d)
			if _, err := v.Parse(bad); !errors.Is(err, ErrInvalid) {
				t.Fatal("unsafe tool activity accepted")
			}
		})
	}
	for name, mutate := range map[string]func(map[string]any){
		"thirty-two inputs":       set("state.lookup", "inputs", exactly),
		"single character input":  set("state.lookup", "inputs", map[string]any{"v": "field.vendor.id"}),
		"sixty-four char input":   set("state.lookup", "inputs", map[string]any{"a" + strings.Repeat("b", 63): "field.vendor.id"}),
		"retry bounds are shared": set("state.lookup", "retry", retry),
	} {
		t.Run("accepts "+name, func(t *testing.T) {
			var d map[string]any
			_ = json.Unmarshal(toolSample(t), &d)
			mutate(d)
			raw, _ := json.Marshal(d)
			if _, err := v.Parse(raw); err != nil {
				t.Fatal("valid boundary rejected", err)
			}
		})
	}
}
func TestToolActivitiesStayUnavailableBeforeSchema13(t *testing.T) {
	v := validator(t)
	// The same document under 1.2.0 must still reject a tool activity: tools
	// are opt-in and 1.0.0-1.2.0 bytes and semantics are preserved.
	old := strings.ReplaceAll(string(toolSample(t)), `"1.3.0"`, `"1.2.0"`)
	if _, err := v.Parse([]byte(old)); !errors.Is(err, ErrInvalid) {
		t.Fatal("tool activity accepted by schema 1.2.0")
	}
	// 1.3.0 with only local activities is the 1.2.0 example, retagged.
	raw, err := os.ReadFile("../../contracts/examples/workflow-draft-v1.2.json")
	if err != nil {
		t.Fatal(err)
	}
	retagged := strings.ReplaceAll(string(raw), `"schema_version": "1.2.0"`, `"schema_version": "1.3.0"`)
	if _, err = v.Parse([]byte(retagged)); err != nil {
		t.Fatal("1.3.0 rejected a local-only 1.2.0 workflow", err)
	}
	if _, err = v.Parse(raw); err != nil {
		t.Fatal("1.2.0 example no longer validates", err)
	}
}
