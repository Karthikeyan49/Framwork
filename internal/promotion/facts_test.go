package promotion

import (
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/risk"
	"github.com/Karthikeyan49/Framwork/internal/tools"
)

func writeToolDefinition() tools.Definition {
	return tools.Definition{Ref: "tool.vendor_update", Version: "1.0.0", SideEffect: "WRITE", Approval: "REQUIRED",
		Delivery:  tools.DeliverySpec{IdempotencyMode: "NATIVE_KEY", RetryMode: "SAFE_RETRY", ReconciliationMode: "NONE"},
		Connector: tools.Connector{Endpoint: tools.Endpoint{Host: "vendors.example.com", Port: 443, Network: "public"}, Method: "POST", Path: "/vendors/{vendor_id}", TimeoutMS: 1000, MaxResponseBytes: 4096, RatePerSecond: 10},
		Input:     []tools.Field{{Name: "vendor_id", Type: "string", Required: true, MaxLength: 64, In: "path"}},
		Output:    []tools.Field{{Name: "ok", Type: "boolean", Required: true}},
		Sandbox:   tools.SandboxSpec{Mode: "SIMULATE", Output: map[string]json.RawMessage{"ok": json.RawMessage(`true`)}}, Tenants: []string{"*"}}
}

// Independent review F7: a workflow change that is plan-only (identical
// authoring, recompiled conditions) around a pinned WRITE tool inherits the
// tool's level exactly like a graph change does.
func TestPlanOnlyWorkflowChangeInheritsTheToolsLevel(t *testing.T) {
	def := writeToolDefinition()
	pin := ToolPin{Ref: def.Ref, Version: def.Version, Hash: def.Hash()}
	for _, aspects := range [][]string{{compiler.AspectPlanOnly}, {compiler.AspectWorkflowGraph}} {
		diff := compiler.Diff{Changes: []compiler.DefinitionChange{{Kind: "workflow", ID: "workflow.vendor.update", Change: compiler.ChangeChanged, Aspects: aspects, Tools: []string{pin.Ref + "@" + pin.Version}}}}
		facts := ReduceFacts(FactsInput{
			Diff: diff, Storage: compiler.StorageDelta{Class: "EQUAL"}, TargetPins: []ToolPin{pin},
			Definitions: map[string]tools.Definition{DefinitionKey(pin.Ref, pin.Version, pin.Hash): def},
			TenantID:    "11111111-1111-4111-8111-111111111111", Volume: "UNVERIFIED",
		})
		if len(facts.Tools) != 1 || facts.Tools[0].Change != risk.ChangeInherited || facts.Tools[0].SideEffect != risk.SideWrite {
			t.Fatalf("aspects %v: the changed workflow must inherit its WRITE tool: %+v", aspects, facts.Tools)
		}
	}
}
