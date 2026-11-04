//go:build integration

package integration

import (
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
)

func TestM9RRoleAdministrationReviewExactReadAuthorityAndRevocation(t *testing.T) {
	m8Database(t)
	f, _, _, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	reader := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}})
	for _, capability := range []string{"roles.read", "memberships.read"} {
		d := policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "exact_reader", Grants: []policy.Grant{{Effect: "ALLOW", Actions: []string{capability}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}}
		base := int64(0)
		if capability == "memberships.read" {
			base = 1
		}
		r5Publish(t, h, root+"/roles", owner, base, d, true, uuid(t))
		if base == 0 {
			r5Assign(t, h, root, owner, f.viewer, d.Name, 200)
		}
		for _, path := range []string{"/roles", "/roles/owner", "/role-contract?name=operations", "/members", "/members/" + f.alice} {
			want := 403
			rolePath := path == "/roles" || path == "/roles/owner" || path == "/role-contract?name=operations"
			if rolePath == (capability == "roles.read") {
				want = 200
			}
			if w := m3Call(h, "GET", root+path, reader, 0, nil); w.Code != want {
				t.Fatal("read route used another capability", capability, path, w.Code)
			}
		}
		if w := m3Call(h, "GET", root+"/access", reader, 0, nil); w.Code != 403 {
			t.Fatal("read contract silently granted workspace access", w.Code)
		}
	}
	// Change only this populated member after verifying effective driver targets.
	_, err := f.operator.Exec(`UPDATE iam.memberships SET active=false WHERE tenant_id=$1 AND user_id=$2`, f.tenantA, f.viewer)
	scalarSQL(t, err)
	for _, path := range []string{"/roles", "/role-contract?name=viewer", "/members/" + f.alice} {
		if w := m3Call(h, "GET", root+path, reader, 0, nil); w.Code != 403 {
			t.Fatal("signed bearer retained authority after live membership revocation", w.Code)
		}
	}
}

func TestM9RRoleAdministrationReviewBootstrapReplacementAndReceiptHistory(t *testing.T) {
	m8Database(t)
	f, _, _, owner, _ := m5Fixture(t)
	scalarReviewTarget(t, f)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	viewer := f.token(t, f.viewerSubject, nil)
	w := m3Call(h, "GET", root+"/roles/viewer", owner, 0, nil)
	var native policy.RoleVersion
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &native) != nil || !native.Bootstrap || native.Version != 1 {
		t.Fatal("native role fixture unavailable", w.Code)
	}
	if w = m3Call(h, "GET", root+"/access", viewer, 0, nil); w.Code != 200 {
		t.Fatal("native empty grants lost compatibility", w.Code)
	}
	d := policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "viewer", Grants: []policy.Grant{}}
	configured := r5Publish(t, h, root+"/roles", owner, 1, d, true, uuid(t))
	if configured.Bootstrap || !configured.Active || configured.Version != 2 {
		t.Fatal("configured role retained bootstrap marker")
	}
	if w = m3Call(h, "GET", root+"/access", viewer, 0, nil); w.Code != 403 {
		t.Fatal("configured empty grants inherited native capabilities", w.Code)
	}
	w = m3Call(h, "GET", root+"/roles/operations/"+native.OperationID, owner, 0, nil)
	var receipt policy.RoleVersion
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Hash != native.Hash || receipt.Version != 1 || !receipt.Bootstrap {
		t.Fatal("native receipt changed with the configured head", w.Code)
	}
	page := r5gRolePage(t, h, root+"/roles?after=zzzzzzzz", owner)
	if len(page.Roles) != 0 || page.Next != "" {
		t.Fatal("empty cursor page is not a bounded empty result")
	}
}
