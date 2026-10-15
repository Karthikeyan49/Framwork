package policy

import (
	"context"
	"strings"

	"testing"
)

func TestPolicyMatrix(t *testing.T) {
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown", ""} {
		builtin := role == "owner" || role == "admin" || role == "member" || role == "viewer"
		for _, tc := range []struct {
			action, kind, owner string
			permitted           bool
		}{
			{"tenant.access", "tenant", "", builtin},
			{"memberships.read", "membership", "", role == "owner" || role == "admin"},
			{"profile.read", "user", "alice", builtin},
			{"metadata.read", "metadata", "", builtin},
			{"metadata.write", "metadata", "", role == "owner" || role == "admin"},
			{"metadata.write", "tool", "", false},
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

func TestRecordAccessAdministrationCapabilities(t *testing.T) {
	row := Resource{TenantID: "tenant-a", Type: "row", AppID: "app.contacts", EntityID: "entity.contact", RecordID: "record"}
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown"} {
		for _, action := range []string{"data.row.share", "data.row.assign"} {
			want := role == "owner" || role == "admin"
			if got := permits("alice", "tenant-a", []string{role}, action, row); got != want {
				t.Fatalf("%s/%s allow=%v", role, action, got)
			}
			collection := row
			collection.RecordID = ""
			if permits("alice", "tenant-a", []string{role}, action, collection) {
				t.Fatal("collection-level record administration")
			}
		}
	}
	grant := `{"schema_version":"tenant-capabilities-v1","name":"%s","grants":[{"effect":"ALLOW","actions":["data.row.share","data.row.assign"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	if _, err := CompileDefinition([]byte(strings.Replace(grant, "%s", "steward", 1)), ""); err != nil {
		t.Fatal("custom record administration rejected", err)
	}
	if _, err := CompileDefinition([]byte(strings.Replace(grant, "%s", "viewer", 1)), ""); err == nil {
		t.Fatal("viewer granted record administration")
	}
	scoped := strings.Replace(strings.Replace(grant, "%s", "steward", 1), `"field_id":"*"}`, `"field_id":"*"},"scope":{"kind":"ALL"}`, 1)
	if _, err := CompileDefinition([]byte(scoped), ""); err == nil {
		t.Fatal("unused scope accepted on row administration gate")
	}
}

func TestTeamAdministrationCapabilities(t *testing.T) {
	team := Resource{TenantID: "tenant-a", Type: "team"}
	for _, role := range []string{"owner", "admin", "member", "viewer"} {
		for _, action := range []string{"teams.read", "teams.manage"} {
			if got := permits("alice", "tenant-a", []string{role}, action, team); got != (role == "owner" || role == "admin") {
				t.Fatalf("%s/%s allow=%v", role, action, got)
			}
		}
	}
	if permits("alice", "tenant-a", []string{"owner"}, "teams.manage", Resource{TenantID: "tenant-a", Type: "role"}) {
		t.Fatal("team action accepted another resource type")
	}
	grant := `{"schema_version":"tenant-capabilities-v1","name":"%s","grants":[{"effect":"ALLOW","actions":["teams.read","teams.manage"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	if _, err := CompileDefinition([]byte(strings.Replace(grant, "%s", "people_ops", 1)), ""); err != nil {
		t.Fatal("custom team administration rejected", err)
	}
	if _, err := CompileDefinition([]byte(strings.Replace(grant, "%s", "viewer", 1)), ""); err == nil {
		t.Fatal("viewer granted team administration")
	}
}
