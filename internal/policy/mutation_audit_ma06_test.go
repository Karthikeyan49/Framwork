package policy

import "testing"

func TestBuiltInRolesAndTheLessCommonActions(t *testing.T) {
	role := Resource{TenantID: "tenant-a", Type: "role"}
	preview := Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}
	workflow := Resource{TenantID: "tenant-a", Type: "workflow", AppID: "app.orders", EntityID: "entity.order", RecordID: "r1", WorkflowID: "workflow.order.approve"}
	for _, tc := range []struct {
		role, action string
		resource     Resource
		want         bool
	}{
		{"owner", "roles.write", role, true}, {"admin", "roles.write", role, false}, {"member", "roles.write", role, false},
		{"owner", "roles.assign", role, true}, {"admin", "roles.assign", role, false},
		{"member", "commands.reconcile", preview, true}, {"viewer", "commands.reconcile", preview, false},
		{"owner", "workflows.reconcile", workflow, true}, {"admin", "workflows.reconcile", workflow, true}, {"member", "workflows.reconcile", workflow, false}, {"viewer", "workflows.reconcile", workflow, false},
		{"admin", "workflows.recover", workflow, true}, {"member", "workflows.recover", workflow, false},
		{"member", "workflows.cancel", workflow, true}, {"viewer", "workflows.cancel", workflow, false}, {"viewer", "workflows.read", workflow, true},
	} {
		if got := permits("alice", "tenant-a", []string{tc.role}, tc.action, tc.resource); got != tc.want {
			t.Errorf("%s/%s: allow=%v, want %v", tc.role, tc.action, got, tc.want)
		}
	}
}
