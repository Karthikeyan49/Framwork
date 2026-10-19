package policy

import (
	"encoding/json"
	"testing"
)

func TestAuthoringContractNeverSuggestsUnsupportedGrants(t *testing.T) {
	for _, name := range []string{"owner", "admin", "member", "viewer", "finance_manager"} {
		c, err := DefinitionContract(name)
		if err != nil || c.RootProtected != (name == "owner") || c.SchemaVersion != DefinitionVersion {
			t.Fatal("invalid authoring contract")
		}
		seen := map[string]bool{}
		for _, a := range c.Actions {
			if seen[a.Name] {
				t.Fatal("repeated capability")
			}
			seen[a.Name] = true
			g := Grant{Effect: "ALLOW", Actions: []string{a.Name}, Resource: Selector{"*", "*", "*"}}
			if a.ScopeRequired {
				g.Scope = &DataScope{Kind: "NONE"}
			}
			raw, _ := json.Marshal(Definition{DefinitionVersion, name, []Grant{g}})
			_, err := CompileDefinition(raw, "")
			if (err == nil) != a.AllowSupported {
				t.Fatal("contract disagrees with actual compiler admission", name, a.Name)
			}
		}
	}
	if _, err := DefinitionContract("Unknown"); err == nil {
		t.Fatal("invalid contract role accepted")
	}
}
