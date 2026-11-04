package compiler

import (
	"bytes"
	"os"
	"reflect"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/tools"
)

// TS-02 (independent tools and sandbox red team): ADR100 decision 2 says the compiler checks the tenant allowlist, and it did not, so a
// tenant could compile and pin a tool restricted to other tenants, and tell by the result that the tool exists.

const (
	tenantMine  = "11111111-1111-4111-8111-111111111111"
	tenantOther = "22222222-2222-4222-8222-222222222222"
)

// restrictedTools is the example catalog with the allowlist of every tool set to tenants.
func restrictedTools(t *testing.T, tenants ...string) tools.Catalog {
	t.Helper()
	lookup, notify := lookupTool(), notifyTool()
	lookup.Tenants, notify.Tenants = tenants, tenants
	return testToolCatalog(t, lookup, notify)
}

func TestToolRestrictedToOtherTenantsIsUnknownToTheCompilingTenant(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/examples/workflow-tool-draft-v1.5.json")
	if err != nil {
		t.Fatal(err)
	}
	s := source(t, raw)
	compileFor := func(c tools.Catalog, tenant string) (Program, []Diagnostic) {
		catalog := toolCompilerCatalog(c)
		catalog.Tenant = tenant
		return Compile(s, catalog)
	}

	base, ds := compileFor(testToolCatalog(t), "")
	if !base.Valid() || len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, c := range []struct {
		name    string
		tenants []string
		tenant  string
		valid   bool
	}{
		{"restricted to another tenant", []string{tenantOther}, tenantMine, false},
		{"the tenant is on the list", []string{tenantOther, tenantMine}, tenantMine, true},
		{"open to all tenants", []string{"*"}, tenantMine, true},
		{"no tenant asks (examples, tooling)", []string{tenantOther}, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, ds := compileFor(restrictedTools(t, c.tenants...), c.tenant)
			if p.Valid() != c.valid || (len(ds) == 0) != c.valid {
				t.Fatalf("valid=%v diagnostics=%v, want valid=%v", p.Valid(), ds, c.valid)
			}
			// The pinned hash of a tool holds its allowlist, so the comparison is with the same catalog compiled without a tenant.
			if same, _ := compileFor(restrictedTools(t, c.tenants...), ""); c.valid && (p.Hash() != same.Hash() || !bytes.Equal(p.JSON(), same.JSON())) {
				t.Fatal("the tenant view changed the IR of a tool the tenant may use")
			}
		})
	}

	// The answer for a tool the tenant may not use is the answer for a tool that is not registered, byte for byte: no oracle.
	_, absent := compileFor(tools.EmptyCatalog(), tenantMine)
	_, other := compileFor(restrictedTools(t, tenantOther), tenantMine)
	if len(absent) == 0 || !reflect.DeepEqual(absent, other) {
		t.Fatalf("a restricted tool is told apart from an unregistered one:\n absent:     %v\n restricted: %v", absent, other)
	}
	for _, d := range other {
		if d.Code != "unknown_tool" {
			t.Fatalf("unexpected diagnostic for a restricted tool: %+v", d)
		}
	}
}

// The tenant is not part of the catalog hash: the IR of a tenant's compile is the IR of every other compile of the same source.
func TestTenantIsNotPartOfTheCatalogHash(t *testing.T) {
	a, b := PlatformCatalog(), PlatformCatalog()
	b.Tenant = tenantMine
	pa, _ := Compile(source(t, mustRead(t, "../../contracts/examples/contacts-draft-v1.json")), a)
	pb, _ := Compile(source(t, mustRead(t, "../../contracts/examples/contacts-draft-v1.json")), b)
	if !pa.Valid() || pa.Hash() != pb.Hash() {
		t.Fatal("the tenant view changed the hash of a program without tools")
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
