package tools

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestSummariesCarryNoEndpointOrCredential pins what a tool summary may reveal:
// names and classes only. The fixtures' hosts, secret references, paths and
// sandbox hosts are planted canaries and must never appear in the encoded view.
func TestSummariesCarryNoEndpointOrCredential(t *testing.T) {
	c, err := NewCatalog(validDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	all := c.Summaries(testTenant)
	if len(all) != c.Len() {
		t.Fatalf("a tenant allowed by every definition saw %d of %d tools", len(all), c.Len())
	}
	raw, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	for _, canary := range []string{"api.crm.example.com", "sandbox.crm.example.com", "pay.example.com", "geo.example.com", "crm_live", "/v1/customers", "Idempotency-Key", "simulated_output", "secret", "host", "endpoint", "connector"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(canary)) {
			t.Errorf("summary leaks %q: %s", canary, raw)
		}
	}
	for _, f := range reflect.VisibleFields(reflect.TypeOf(Summary{})) {
		switch f.Name {
		case "Ref", "Version", "SideEffect", "Approval", "Input", "Output":
		default:
			t.Errorf("Summary grew field %s: review what leaves the platform", f.Name)
		}
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Ref > all[i].Ref || all[i-1].Ref == all[i].Ref && all[i-1].Version >= all[i].Version {
			t.Fatalf("summaries are not sorted: %v", all)
		}
	}
	for _, s := range all {
		if s.Input == nil || s.Output == nil {
			t.Fatalf("nil name list encodes as null: %+v", s)
		}
	}
}

// TestSummariesAreTenantScoped: a tool restricted to other tenants is invisible,
// so a tenant's model context can never name another tenant's private tool.
func TestSummariesAreTenantScoped(t *testing.T) {
	c, err := NewCatalog(validDefinitions())
	if err != nil {
		t.Fatal(err)
	}
	other := c.Summaries("99999999-2222-4333-8444-555555555555")
	for _, s := range other {
		if s.Ref == "tool.crm.get_balance" || s.Ref == "tool.payments.create_invoice" {
			t.Fatalf("tool restricted to another tenant listed: %s", s.Ref)
		}
	}
	if len(other) == 0 || len(other) >= c.Len() {
		t.Fatalf("expected only wildcard tools, got %d of %d", len(other), c.Len())
	}
	if got := EmptyCatalog().Summaries(testTenant); got == nil || len(got) != 0 {
		t.Fatalf("empty catalog must give an empty non-nil list: %#v", got)
	}
}
