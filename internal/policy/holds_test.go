package policy

import (
	"encoding/json"
	"testing"
)

const holdsTenant = "11111111-1111-4111-8111-111111111111"
const holdsMember = "22222222-2222-4222-8222-222222222222"

func previewOf(app string) Resource {
	return Resource{TenantID: holdsTenant, Type: "preview", AppID: app}
}

// The built-in roles: owner and admin hold the release authorities, member and viewer never do,
// and nothing is granted for another tenant, another resource type or an unknown role.
func TestNativeHolds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		role     string
		action   string
		resource Resource
		tenant   string
		member   string
		want     bool
	}{
		{"owner approves", "owner", "releases.approve", previewOf("app.orders"), holdsTenant, holdsMember, true},
		{"admin approves", "admin", "releases.approve", previewOf("app.orders"), holdsTenant, holdsMember, true},
		{"admin activates", "admin", "releases.activate", previewOf("app.orders"), holdsTenant, holdsMember, true},
		{"member does not", "member", "releases.approve", previewOf("app.orders"), holdsTenant, holdsMember, false},
		{"viewer does not", "viewer", "releases.approve", previewOf("app.orders"), holdsTenant, holdsMember, false},
		{"unknown role", "auditor", "releases.approve", previewOf("app.orders"), holdsTenant, holdsMember, false},
		{"another tenant's resource", "admin", "releases.approve", Resource{TenantID: "33333333-3333-4333-8333-333333333333", Type: "preview", AppID: "app.orders"}, holdsTenant, holdsMember, false},
		{"no application", "admin", "releases.approve", previewOf(""), holdsTenant, holdsMember, false},
		{"wrong resource type", "admin", "releases.approve", Resource{TenantID: holdsTenant, Type: "metadata", AppID: "app.orders"}, holdsTenant, holdsMember, false},
		{"no tenant", "admin", "releases.approve", previewOf("app.orders"), "", holdsMember, false},
		{"no member", "admin", "releases.approve", previewOf("app.orders"), holdsTenant, "", false},
		{"unknown action", "admin", "releases.delete", previewOf("app.orders"), holdsTenant, holdsMember, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NativeHolds(tc.tenant, tc.member, tc.role, tc.action, tc.resource); got != tc.want {
				t.Fatalf("NativeHolds = %v, want %v", got, tc.want)
			}
		})
	}
}

func definitionOf(t *testing.T, name string, grants ...Grant) Defined {
	t.Helper()
	if grants == nil {
		grants = []Grant{} // null grants are not a definition
	}
	raw, err := json.Marshal(Definition{SchemaVersion: DefinitionVersion, Name: name, Grants: grants})
	if err != nil {
		t.Fatal(err)
	}
	d, err := CompileDefinition(raw, "")
	if err != nil {
		t.Fatal("definition rejected:", err)
	}
	return d
}

func grant(effect, app string, actions ...string) Grant {
	return Grant{Effect: effect, Actions: actions, Resource: Selector{AppID: app, EntityID: "*", FieldID: "*"}}
}

// A custom role holds an action only through an ALLOW grant that names it for a matching
// application; an explicit DENY wins wherever it sits; bootstrap definitions follow the native rules.
func TestDefinedHoldsFollowsTheGrants(t *testing.T) {
	orders, billing := previewOf("app.orders"), previewOf("app.billing")
	anyApp := definitionOf(t, "release_approver", grant("ALLOW", "*", "releases.approve", "releases.read"))
	onlyOrders := definitionOf(t, "orders_approver", grant("ALLOW", "app.orders", "releases.approve"))
	denyBilling := definitionOf(t, "careful_approver", grant("DENY", "app.billing", "releases.approve"), grant("ALLOW", "*", "releases.approve"))
	denyFirst := definitionOf(t, "careful_first", grant("ALLOW", "*", "releases.approve"), grant("DENY", "app.billing", "releases.approve"))
	reader := definitionOf(t, "reader", grant("ALLOW", "*", "releases.read", "candidates.read"))
	for _, tc := range []struct {
		name     string
		d        Defined
		action   string
		resource Resource
		want     bool
	}{
		{"wildcard application", anyApp, "releases.approve", billing, true},
		{"the granted action only", anyApp, "releases.activate", orders, false},
		{"named application", onlyOrders, "releases.approve", orders, true},
		{"other application", onlyOrders, "releases.approve", billing, false},
		{"deny on one application", denyBilling, "releases.approve", billing, false},
		{"deny leaves the others", denyBilling, "releases.approve", orders, true},
		{"deny wins when it comes last", denyFirst, "releases.approve", billing, false},
		{"read only", reader, "releases.approve", orders, false},
		{"unknown action", anyApp, "releases.delete", orders, false},
		{"wrong resource type", anyApp, "releases.approve", Resource{TenantID: holdsTenant, Type: "metadata", AppID: "app.orders"}, false},
		{"another tenant's resource", anyApp, "releases.approve", Resource{TenantID: "33333333-3333-4333-8333-333333333333", Type: "preview", AppID: "app.orders"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.d.holds(holdsTenant, holdsMember, tc.action, tc.resource); got != tc.want {
				t.Fatalf("holds = %v, want %v", got, tc.want)
			}
		})
	}
	for name, want := range map[string]bool{"owner": true, "admin": true, "member": false, "viewer": false} {
		d := definitionOf(t, name)
		d.legacy = true
		if got := d.holds(holdsTenant, holdsMember, "releases.approve", orders); got != want {
			t.Errorf("bootstrap role %s: holds = %v, want %v", name, got, want)
		}
	}
}
