package policy

import (
	"context"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"testing"
)

func TestPolicyMatrix(t *testing.T) {
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown", ""} {
		for _, tc := range []struct {
			action, kind, owner string
			permitted           bool
		}{
			{"tenant.access", "tenant", "", tenant.ValidRole(role)},
			{"memberships.read", "membership", "", role == "owner" || role == "admin"},
			{"profile.read", "user", "alice", tenant.ValidRole(role)},
			{"profile.read", "user", "bob", false},
			{"tenant.access", "tool", "", false},
			{"unknown", "tenant", "", false},
			{"promotion.approve", "tenant", "", false},
			{"tools.execute", "tenant", "", false},
			{"memberships.manage", "membership", "", false},
		} {
			t.Run(role+"/"+tc.action+"/"+tc.owner+"/"+tc.kind, func(t *testing.T) {
				resource := Resource{TenantID: "tenant-a", Type: tc.kind, OwnerID: tc.owner}
				if got := permits("alice", "tenant-a", []string{role}, tc.action, resource); got != tc.permitted {
					t.Fatalf("allow=%v want=%v", got, tc.permitted)
				}
				if permits("alice", "tenant-b", []string{role}, tc.action, resource) {
					t.Fatal("cross-tenant grant")
				}
				if permits("", "tenant-a", []string{role}, tc.action, resource) {
					t.Fatal("anonymous grant")
				}
			})
		}
	}
	engine := Native{}
	d := engine.Decide(context.Background(), Subject{}, "tenant.access", Resource{}, Context{})
	if d.Allow || d.ID == "" || d.Version != Version || len(d.Obligations) != 0 {
		t.Fatal("invalid context not denied with traceable evidence")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if engine.Decide(ctx, Subject{}, "tenant.access", Resource{}, Context{}).Allow {
		t.Fatal("cancelled decision allowed")
	}
}

func TestSelfSessionAttributes(t *testing.T) {
	good := Resource{Type: "user", OwnerID: "alice"}
	if !permitsSelf("alice", "identity.session.read", good) {
		t.Fatal("own identity session denied")
	}
	for _, r := range []Resource{{Type: "user", OwnerID: "bob"}, {Type: "tenant", OwnerID: "alice"}, {Type: "user", OwnerID: "alice", TenantID: "other"}} {
		if permitsSelf("alice", "identity.session.read", r) {
			t.Fatal("self session leaked resources")
		}
	}
	if permitsSelf("", "identity.session.read", good) || permitsSelf("alice", "unknown", good) {
		t.Fatal("unknown session authority accepted")
	}
}
