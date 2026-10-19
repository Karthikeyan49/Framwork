//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

func r5gRolePage(t *testing.T, h http.Handler, path, token string) policy.RolePage {
	t.Helper()
	w := m3Call(h, "GET", path, token, 0, nil)
	var page policy.RolePage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Roles == nil || len(page.Roles) > 100 || strings.Contains(w.Body.String(), `"definition"`) {
		t.Fatal("role summary page contract", w.Code)
	}
	return page
}

func TestM9RRoleAdministrationDiscoveryPaginationAndReservedNames(t *testing.T) {
	m8Database(t)
	f, _, _, owner, _ := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	for n := range 105 {
		r5Publish(t, h, root+"/roles", owner, 0, policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: fmt.Sprintf("role_%03d", n), Grants: []policy.Grant{}}, n%2 == 0, uuid(t))
	}
	for _, name := range []string{"catalog", "options", "operations", "teams", "members", "roles"} {
		r5Publish(t, h, root+"/roles", owner, 0, policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: name, Grants: []policy.Grant{}}, true, uuid(t))
		if w := m3Call(h, "GET", root+"/roles/"+name, owner, 0, nil); w.Code != 200 {
			t.Fatal("valid role name shadowed by new management routes", name, w.Code)
		}
	}
	first := r5gRolePage(t, h, root+"/roles", owner)
	if len(first.Roles) != 100 || first.Next != first.Roles[99].Name {
		t.Fatal("role pagination omitted continuation")
	}
	second := r5gRolePage(t, h, root+"/roles?after="+first.Next, owner)
	if len(second.Roles) != 15 || second.Next != "" {
		t.Fatal("role pagination did not include all tenant heads")
	}
	seen := map[string]bool{}
	previous := ""
	for _, role := range append(first.Roles, second.Roles...) {
		if role.Name <= previous || seen[role.Name] || role.Version != 1 || len(role.Hash) != 64 || role.OperationID == "" {
			t.Fatal("unordered, repeated or invalid role summary")
		}
		previous, seen[role.Name] = role.Name, true
	}
	for _, path := range []string{"/roles?after=Upper", "/roles?after=member&after=viewer", "/roles?unknown=x", "/roles?after=member&other=x", "/roles?after=%zz", "/role-contract", "/role-contract?name=owner&name=member", "/role-contract?name=bad-name", "/role-contract?other=owner"} {
		if w := m3Call(h, "GET", root+path, owner, 0, nil); w.Code != 400 {
			t.Fatal("ambiguous management query accepted", path, w.Code)
		}
	}
	var contract policy.RoleContract
	w := m3Call(h, "GET", root+"/role-contract?name=viewer", owner, 0, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &contract) != nil || contract.Name != "viewer" || contract.RootProtected || contract.Limits.DefinitionBytes != 65536 {
		t.Fatal("role authoring contract", w.Code)
	}
	for _, action := range contract.Actions {
		if action.Name == "roles.write" && action.AllowSupported || action.Name == "data.row.write" && action.AllowSupported || action.Name == "data.field.read" && !action.ScopeRequired {
			t.Fatal("authoring contract suggests an unsupported capability")
		}
	}
	w = m3Call(h, "GET", root+"/role-contract?name=owner", owner, 0, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &contract) != nil || !contract.RootProtected {
		t.Fatal("owner contract did not describe protected root")
	}
}

func TestM9RRoleAdministrationReadsUseLiveTenantAuthority(t *testing.T) {
	m8Database(t)
	f, _, _, owner, app := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	otherRoot := "/v1/tenants/" + f.tenantB
	bob := f.token(t, f.bobSubject, nil)
	viewer := f.token(t, f.viewerSubject, map[string]any{"roles": []string{"owner"}, "tenant_id": f.tenantB})
	r5Publish(t, h, otherRoot+"/roles", bob, 0, policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "foreign_only", Grants: []policy.Grant{}}, true, uuid(t))
	for _, role := range r5gRolePage(t, h, root+"/roles", owner).Roles {
		if role.Name == "foreign_only" {
			t.Fatal("role discovery escaped tenant")
		}
	}
	for _, path := range []string{root + "/roles", root + "/role-contract?name=viewer", root + "/members/" + f.alice, otherRoot + "/roles", otherRoot + "/role-contract?name=viewer", otherRoot + "/members/" + f.bob} {
		if w := m3Call(h, "GET", path, viewer, 0, nil); w.Code != 403 {
			t.Fatal("claims or membership bypassed role read authority", w.Code)
		}
	}
	d := r5Definition("role_reader", app, policy.DataScope{Kind: "NONE"})
	d.Grants = []policy.Grant{{Effect: "ALLOW", Actions: []string{"roles.read", "memberships.read"}, Resource: policy.Selector{AppID: "*", EntityID: "*", FieldID: "*"}}}
	r5Publish(t, h, root+"/roles", owner, 0, d, true, uuid(t))
	r5Assign(t, h, root, owner, f.viewer, d.Name, 200)
	if len(r5gRolePage(t, h, root+"/roles", viewer).Roles) != 5 {
		t.Fatal("explicit reader capability denied")
	}
	if w := m3Call(h, "GET", root+"/members/"+f.alice, viewer, 0, nil); w.Code != 200 {
		t.Fatal("explicit member reader denied", w.Code)
	}
	r5Publish(t, h, root+"/roles", owner, 1, d, false, uuid(t))
	for _, path := range []string{root + "/roles", root + "/role-contract?name=viewer", root + "/members/" + f.alice} {
		if w := m3Call(h, "GET", path, viewer, 0, nil); w.Code != 403 {
			t.Fatal("inactive live role retained management read access", w.Code)
		}
	}
}

func TestM9RRoleAdministrationMemberPointBeyondDirectory(t *testing.T) {
	m8Database(t)
	f, _, _, owner, _ := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	// Known owned fixture, with a connected-target proof before operator writes.
	scalarReviewTarget(t, f)
	for i := range 104 {
		id := fmt.Sprintf("eeeeeeee-eeee-eeee-eeee-%012d", i)
		_, err := f.operator.Exec(`INSERT INTO iam.users(id,issuer,subject,name,active) VALUES($1,'https://test-issuer.example',$2,'Point member',true)`, id, "r5g-point-"+fmt.Sprint(i))
		scalarSQL(t, err)
		scalarSQL(t, (tenant.Store{}).Provision(context.Background(), f.operator, f.tenantA, "Roles", id, "viewer", true))
	}
	const target = "eeeeeeee-eeee-eeee-eeee-000000000103"
	w := m3Call(h, "GET", root+"/members", owner, 0, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), target) {
		t.Fatal("member fixture is not beyond bounded directory", w.Code)
	}
	w = m3Call(h, "GET", root+"/members/"+target, owner, 0, nil)
	var member tenant.Member
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &member) != nil || member.UserID != target || member.Role != "viewer" || !member.Active {
		t.Fatal("point member read did not return exact live target", w.Code)
	}
	if w = m3Call(h, "GET", root+"/members/"+f.bob, owner, 0, nil); w.Code != 404 {
		t.Fatal("point read exposed a foreign tenant member", w.Code)
	}
	if w = m3Call(h, "GET", root+"/members/not-a-uuid", owner, 0, nil); w.Code != 400 {
		t.Fatal("invalid member identifier accepted", w.Code)
	}
}

func TestM9RRoleAdministrationAssignmentWitnessAndAuditRollback(t *testing.T) {
	m8Database(t)
	f, _, _, owner, _ := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	body := map[string]string{"role": "member", "expected_role": "viewer"}
	w := m5Call(h, root+"/members/"+f.viewer+"/role", owner, body)
	var ack struct {
		Assigned     bool   `json:"assigned"`
		UserID       string `json:"user_id"`
		Role         string `json:"role"`
		ExpectedRole string `json:"expected_role"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ack) != nil || !ack.Assigned || ack.UserID != f.viewer || ack.Role != "member" || ack.ExpectedRole != "viewer" {
		t.Fatal("assignment acknowledgement is not bound to the requested target", w.Code)
	}
	if w = m5Call(h, root+"/members/"+f.viewer+"/role", owner, body); w.Code != 409 || strings.Contains(w.Body.String(), `"assigned":true`) {
		t.Fatal("stale assignment returned success", w.Code)
	}
	if w = m5Call(h, root+"/members/"+f.alice+"/role", owner, map[string]string{"role": "member", "expected_role": "owner"}); w.Code != 409 {
		t.Fatal("last owner was removed", w.Code)
	}
	failing := r5Handler(t, f, failedAudit{})
	if w = m5Call(failing, root+"/members/"+f.viewer+"/role", owner, map[string]string{"role": "admin", "expected_role": "member"}); w.Code != 503 {
		t.Fatal("assignment acknowledged before audit committed", w.Code)
	}
	w = m3Call(h, "GET", root+"/members/"+f.viewer, owner, 0, nil)
	var current tenant.Member
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &current) != nil || current.Role != "member" {
		t.Fatal("failed audit left an assigned role")
	}
	op := uuid(t)
	if w = m5Call(failing, root+"/roles", owner, map[string]any{"operation_id": op, "expected_version": 0, "active": false, "definition": policy.Definition{SchemaVersion: policy.DefinitionVersion, Name: "audit_aborted", Grants: []policy.Grant{}}}); w.Code != 503 {
		t.Fatal("publication acknowledged before audit committed", w.Code)
	}
	for _, path := range []string{root + "/roles/audit_aborted", root + "/roles/operations/" + op} {
		if w = m3Call(h, "GET", path, owner, 0, nil); w.Code != 404 {
			t.Fatal("failed audit left a role head or operation receipt", w.Code)
		}
	}
}
