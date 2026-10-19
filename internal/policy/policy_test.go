package policy

import (
	"context"
	"fmt"
	"slices"
	"strconv"
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

func TestReleaseBuilderCapabilities(t *testing.T) {
	preview := Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}
	for _, role := range []string{"owner", "admin", "member", "viewer"} {
		for _, action := range []string{"requirements.read", "acceptance.read", "candidates.read"} {
			if !permits("alice", "tenant-a", []string{role}, action, preview) {
				t.Fatalf("%s denied %s", role, action)
			}
		}
		for _, action := range []string{"requirements.decide", "acceptance.author", "candidates.build"} {
			if got := permits("alice", "tenant-a", []string{role}, action, preview); got != (role == "owner" || role == "admin") {
				t.Fatalf("%s/%s allow=%v", role, action, got)
			}
		}
	}
	for _, action := range []string{"requirements.read", "requirements.decide", "acceptance.read", "acceptance.author", "candidates.read", "candidates.build"} {
		if permits("alice", "tenant-a", []string{"owner"}, action, Resource{TenantID: "tenant-a", Type: "preview"}) {
			t.Fatalf("%s accepted a resource without an application", action)
		}
		if permits("alice", "tenant-a", []string{"owner"}, action, Resource{TenantID: "tenant-a", Type: "team", AppID: "app.orders"}) {
			t.Fatalf("%s accepted another resource type", action)
		}
		if permits("alice", "tenant-b", []string{"owner"}, action, preview) {
			t.Fatalf("%s crossed tenants", action)
		}
	}
	grant := `{"schema_version":"tenant-capabilities-v1","name":"%s","grants":[{"effect":"ALLOW","actions":["requirements.read","requirements.decide","acceptance.read","acceptance.author","candidates.read","candidates.build"],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	if _, err := CompileDefinition([]byte(strings.Replace(grant, "%s", "release_manager", 1)), ""); err != nil {
		t.Fatal("custom release role rejected", err)
	}
	if _, err := CompileDefinition([]byte(strings.Replace(grant, "%s", "viewer", 1)), ""); err == nil {
		t.Fatal("viewer granted release authority")
	}
	if nativeRevision(t) < 9 {
		t.Fatal("policy version not advanced for the new actions", Version)
	}
}

// nativeRevision is the numeric suffix of the native policy version; later
// milestones advance it, so earlier capability tests check a lower bound only.
func nativeRevision(t *testing.T) int {
	t.Helper()
	_, suffix, ok := strings.Cut(Version, "-native-v")
	n, err := strconv.Atoi(suffix)
	if !ok || err != nil || n < 1 {
		t.Fatal("malformed native policy version", Version)
	}
	return n
}

func TestReleasePromotionCapabilities(t *testing.T) {
	preview := Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}
	authority := []string{"releases.approve", "releases.activate", "releases.rollback"}
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown", ""} {
		builtin := role == "owner" || role == "admin" || role == "member" || role == "viewer"
		if got := permits("alice", "tenant-a", []string{role}, "releases.read", preview); got != builtin {
			t.Fatalf("%q releases.read allow=%v", role, got)
		}
		for _, action := range authority {
			if got := permits("alice", "tenant-a", []string{role}, action, preview); got != (role == "owner" || role == "admin") {
				t.Fatalf("%q/%s allow=%v", role, action, got)
			}
		}
	}
	for _, action := range append([]string{"releases.read"}, authority...) {
		if !knownAction(action) || !validResource(action, preview, "alice") {
			t.Fatalf("%s missing from the catalog or the preview resource shape", action)
		}
		for _, r := range []Resource{{TenantID: "tenant-a", Type: "preview"}, {TenantID: "tenant-a", Type: "team", AppID: "app.orders"}, {TenantID: "tenant-a", Type: "workflow", AppID: "app.orders"}} {
			if permits("alice", "tenant-a", []string{"owner"}, action, r) || validResource(action, r, "alice") {
				t.Fatalf("%s accepted resource %+v", action, r)
			}
		}
		if permits("alice", "tenant-b", []string{"owner"}, action, preview) || permits("", "tenant-a", []string{"owner"}, action, preview) {
			t.Fatalf("%s crossed tenants or admitted an anonymous actor", action)
		}
	}
	grant := `{"schema_version":"tenant-capabilities-v1","name":"%s","grants":[{"effect":"ALLOW","actions":[%s],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	define := func(role string, actions ...string) error {
		_, err := CompileDefinition([]byte(fmt.Sprintf(grant, role, `"`+strings.Join(actions, `","`)+`"`)), "")
		return err
	}
	// Stored custom roles may split approving from activating and rolling back.
	if err := define("release_manager", append([]string{"releases.read"}, authority...)...); err != nil {
		t.Fatal("custom promotion role rejected", err)
	}
	if err := define("release_approver", "releases.read", "releases.approve"); err != nil {
		t.Fatal("approve-only custom role rejected", err)
	}
	if err := define("viewer", "releases.read"); err != nil {
		t.Fatal("viewer denied release history", err)
	}
	for _, action := range authority {
		if define("viewer", action) == nil {
			t.Fatalf("viewer granted %s", action)
		}
		// A DENY is always safe, also for viewer.
		denyOnly := strings.Replace(fmt.Sprintf(grant, "viewer", `"`+action+`"`), `"ALLOW"`, `"DENY"`, 1)
		if _, err := CompileDefinition([]byte(denyOnly), ""); err != nil {
			t.Fatalf("viewer DENY of %s rejected: %v", action, err)
		}
	}
	if nativeRevision(t) < 10 {
		t.Fatal("policy version not advanced for the promotion actions", Version)
	}
}

func TestActionCatalogHashIsStableAndSensitive(t *testing.T) {
	h := ActionCatalogHash()
	if len(h) != 64 || h != ActionCatalogHash() {
		t.Fatal("unstable action catalog hash")
	}
	names := strings.Fields(actionCatalog)
	reversed := append([]string(nil), names...)
	slices.Reverse(reversed)
	if catalogHash(reversed) != h || !slices.Equal(names, strings.Fields(actionCatalog)) {
		t.Fatal("catalog hash depends on declaration order or mutated its input")
	}
	// The M13 actions changed the catalog, so candidates built under the M12
	// catalog are stale (POLICY_CHANGED) and must be rebuilt (ADR102 section 2).
	m13 := slices.DeleteFunc(append([]string(nil), names...), func(a string) bool { return strings.HasPrefix(a, "ai.") })
	if len(m13) != len(names)-3 || catalogHash(m13) == h {
		t.Fatal("action catalog hash is not sensitive to the M14 actions")
	}
	m12 := slices.DeleteFunc(append([]string(nil), m13...), func(a string) bool { return strings.HasPrefix(a, "releases.") })
	if len(m12) != len(m13)-4 || catalogHash(m12) == catalogHash(m13) {
		t.Fatal("action catalog hash is not sensitive to the M13 actions")
	}
	if catalogHash(names[1:]) == h {
		t.Fatal("action catalog hash ignored a removed action")
	}
}

// TestAICapabilities pins ADR104 A6: ai.read for every built-in role, ai.propose and
// ai.apply for owner and admin only, never for viewer or an unknown role, only on the
// preview resource of an application, never across tenants; a stored custom role may
// be granted any of them except that a role named viewer may be granted ai.read only.
func TestAICapabilities(t *testing.T) {
	preview := Resource{TenantID: "tenant-a", Type: "preview", AppID: "app.orders"}
	for _, role := range []string{"owner", "admin", "member", "viewer", "unknown", ""} {
		builtin := role == "owner" || role == "admin" || role == "member" || role == "viewer"
		if got := permits("alice", "tenant-a", []string{role}, "ai.read", preview); got != builtin {
			t.Fatalf("%q ai.read allow=%v", role, got)
		}
		for _, action := range []string{"ai.propose", "ai.apply"} {
			if got := permits("alice", "tenant-a", []string{role}, action, preview); got != (role == "owner" || role == "admin") {
				t.Fatalf("%q/%s allow=%v", role, action, got)
			}
		}
	}
	for _, action := range []string{"ai.read", "ai.propose", "ai.apply"} {
		if !knownAction(action) || !validResource(action, preview, "alice") {
			t.Fatalf("%s missing from the catalog or the preview resource shape", action)
		}
		for _, r := range []Resource{{TenantID: "tenant-a", Type: "preview"}, {TenantID: "tenant-a", Type: "metadata", AppID: "app.orders"}, {TenantID: "tenant-a", Type: "workflow", AppID: "app.orders"}} {
			if permits("alice", "tenant-a", []string{"owner"}, action, r) || validResource(action, r, "alice") {
				t.Fatalf("%s accepted resource %+v", action, r)
			}
		}
		if permits("alice", "tenant-b", []string{"owner"}, action, preview) || permits("", "tenant-a", []string{"owner"}, action, preview) {
			t.Fatalf("%s crossed tenants or admitted an anonymous actor", action)
		}
	}
	grant := `{"schema_version":"tenant-capabilities-v1","name":"%s","grants":[{"effect":"ALLOW","actions":[%s],"resource":{"app_id":"*","entity_id":"*","field_id":"*"}}]}`
	define := func(role string, actions ...string) error {
		_, err := CompileDefinition([]byte(fmt.Sprintf(grant, role, `"`+strings.Join(actions, `","`)+`"`)), "")
		return err
	}
	if err := define("ai_author", "ai.read", "ai.propose"); err != nil {
		t.Fatal("propose-only custom role rejected", err)
	}
	if err := define("ai_reviewer", "ai.read", "ai.apply"); err != nil {
		t.Fatal("apply-only custom role rejected", err)
	}
	if err := define("viewer", "ai.read"); err != nil {
		t.Fatal("viewer denied ai.read", err)
	}
	for _, action := range []string{"ai.propose", "ai.apply"} {
		if define("viewer", action) == nil {
			t.Fatalf("viewer granted %s", action)
		}
		denyOnly := strings.Replace(fmt.Sprintf(grant, "viewer", `"`+action+`"`), `"ALLOW"`, `"DENY"`, 1)
		if _, err := CompileDefinition([]byte(denyOnly), ""); err != nil {
			t.Fatalf("viewer DENY of %s rejected: %v", action, err)
		}
	}
	if Version != "m14-native-v11" || nativeRevision(t) != 11 {
		t.Fatal("policy version not advanced for the AI actions", Version)
	}
}
