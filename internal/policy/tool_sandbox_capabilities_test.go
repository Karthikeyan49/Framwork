package policy

import (
	"fmt"
	"strings"
	"testing"
)

// The M10 and M11 actions were pinned by no test that names them (gap N-13 of the foundation analysis). These tests fix
// the built-in matrix, the resource shapes and what a stored custom role may be granted.

func TestToolInvokeCapability(t *testing.T) {
	step := Resource{TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"}
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown", ""} {
		// Governed tool sends run only through pinned workflow plans; a viewer can read but never send.
		want := role == "owner" || role == "admin" || role == "member"
		if got := permits("alice", "tenant-a", []string{role}, "tools.invoke", step); got != want {
			t.Fatalf("%q tools.invoke allow=%v, want %v", role, got, want)
		}
	}
	incomplete := map[string]Resource{
		"no application": {TenantID: "tenant-a", Type: "workflow", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"},
		"no entity":      {TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", RecordID: "r1", WorkflowID: "workflow.order.approve"},
		"no record":      {TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", EntityID: "entity.order", WorkflowID: "workflow.order.approve"},
		"no workflow":    {TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1"},
		"a preview":      {TenantID: "tenant-a", Type: "preview", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"},
		"a team":         {TenantID: "tenant-a", Type: "team", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"},
	}
	for name, r := range incomplete {
		if permits("alice", "tenant-a", []string{"owner"}, "tools.invoke", r) || validResource("tools.invoke", r, "alice") {
			t.Fatalf("tools.invoke accepted a resource with %s", name)
		}
	}
	if !validResource("tools.invoke", step, "alice") || !knownAction("tools.invoke") {
		t.Fatal("tools.invoke is missing from the catalog or from the workflow resource shape")
	}
	if permits("alice", "tenant-b", []string{"owner"}, "tools.invoke", step) || permits("", "tenant-a", []string{"owner"}, "tools.invoke", step) {
		t.Fatal("tools.invoke crossed tenants or admitted an anonymous actor")
	}
	// Execution is not a capability: there is no action that runs a tool outside a workflow plan.
	for _, action := range []string{"tools.execute", "tools.send", "tools.register", "tools.manage"} {
		if knownAction(action) || permits("alice", "tenant-a", []string{"owner"}, action, step) {
			t.Fatalf("%s exists as a capability", action)
		}
	}
}

func TestSandboxCapabilities(t *testing.T) {
	preview := Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown", ""} {
		builtin := role == "owner" || role == "admin" || role == "member" || role == "viewer"
		if got := permits("alice", "tenant-a", []string{role}, "sandbox.read", preview); got != builtin {
			t.Fatalf("%q sandbox.read allow=%v, want %v", role, got, builtin)
		}
		if got := permits("alice", "tenant-a", []string{role}, "sandbox.manage", preview); got != (role == "owner" || role == "admin") {
			t.Fatalf("%q sandbox.manage allow=%v", role, got)
		}
	}
	for _, action := range []string{"sandbox.read", "sandbox.manage"} {
		if !knownAction(action) || !validResource(action, preview, "alice") {
			t.Fatalf("%s is missing from the catalog or from the preview resource shape", action)
		}
		for _, r := range []Resource{{TenantID: "tenant-a", Type: "preview"}, {TenantID: "tenant-a", Type: "team", AppID: "app.orders"}, {TenantID: "tenant-a", Type: "workflow", AppID: "app.orders"}, {TenantID: "tenant-a", Type: "metadata", AppID: "app.orders"}} {
			if permits("alice", "tenant-a", []string{"owner"}, action, r) || validResource(action, r, "alice") {
				t.Fatalf("%s accepted resource %+v", action, r)
			}
		}
		if permits("alice", "tenant-b", []string{"owner"}, action, preview) || permits("", "tenant-a", []string{"owner"}, action, preview) {
			t.Fatalf("%s crossed tenants or admitted an anonymous actor", action)
		}
	}
}

// What a stored custom role can hold: tools.invoke for any role but viewer, sandbox.read for every role, and
// sandbox.manage for none of them (its SQL reservation stays with owner and admin); a DENY is always safe.
func TestToolAndSandboxGrantsInCustomRoles(t *testing.T) {
	template := `{"schema_version":"tenant-capabilities-v1","name":"%s","grants":[{"effect":"%s","actions":["%s"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	compile := func(role, effect, action string) error {
		_, err := CompileDefinition([]byte(fmt.Sprintf(template, role, effect, action)), "")
		return err
	}
	for _, tc := range []struct {
		role, action string
		allowed      bool
	}{
		{"release_manager", "tools.invoke", true},
		{"viewer", "tools.invoke", false},
		{"release_manager", "sandbox.read", true},
		{"viewer", "sandbox.read", true},
		{"release_manager", "sandbox.manage", false},
		{"viewer", "sandbox.manage", false},
	} {
		if err := compile(tc.role, "ALLOW", tc.action); (err == nil) != tc.allowed {
			t.Errorf("ALLOW %s for role %q: error %v, want accepted=%v", tc.action, tc.role, err, tc.allowed)
		}
		if err := compile(tc.role, "DENY", tc.action); err != nil {
			t.Errorf("DENY %s for role %q rejected: %v", tc.action, tc.role, err)
		}
	}
	// A granted tools.invoke is honoured for the named workflow step and for nothing else.
	d, err := CompileDefinition([]byte(strings.Replace(fmt.Sprintf(template, "tool_sender", "ALLOW", "tools.invoke"), `"app_id":"*"`, `"app_id":"app.orders"`, 1)), "")
	if err != nil {
		t.Fatal(err)
	}
	step := Resource{TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"}
	other := step
	other.AppID = "app.billing"
	if !d.holds("tenant-a", "alice", "tools.invoke", step) || d.holds("tenant-a", "alice", "tools.invoke", other) || d.holds("tenant-a", "alice", "sandbox.manage", Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}) {
		t.Fatal("a tools.invoke grant is not confined to its application and action")
	}
}
