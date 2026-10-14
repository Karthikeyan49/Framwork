package tenant

import "testing"

func TestAccessAndValidation(t *testing.T) {
	a := Access{actor: "actor", tenant: "tenant", roles: []string{"viewer"}}
	roles := a.Roles()
	roles[0] = "owner"
	if a.Roles()[0] != "viewer" {
		t.Fatal("trusted roles mutated")
	}
	for _, s := range []string{"", "FORGED", "11111111-1111-1111-1111-11111111111z", "11111111_1111-1111-1111-111111111111"} {
		if ValidID(s) {
			t.Fatal("malformed tenant accepted")
		}
	}
	if !ValidID("11111111-1111-1111-1111-111111111111") {
		t.Fatal("UUID rejected")
	}
	if !ValidRole("finance_manager") || ValidRole("") || ValidRole("Finance Manager") || ValidRole("*") || ValidRole("0owner") {
		t.Fatal("role identifier validation violated")
	}
}
