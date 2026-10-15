//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func TestIndependentR5cAssignOnlyHeaderQueries(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	record := m5Create(t, legacy, base, alice, "PRIVATE assign-only fixture")
	r5cChange(t, legacy, base+"/records/"+record.ID+"/access", alice, data.AccessCommand{Operation: "owner", ExpectedVersion: 1, UserID: f.viewer}, 200)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	token := f.token(t, f.viewerSubject, nil)
	var version int64
	cases := []struct {
		name, writeScope string
		fieldWrite       bool
	}{
		{"read_only_control", "", false},
		{"all_write_no_field_write_control", "ALL", false},
		{"owner_write_no_field_write", "OWNER", false},
		{"owner_write_with_field_write", "OWNER", true},
	}
	for _, tc := range cases {
		d := r5Definition("review_assign_only", app, policy.DataScope{Kind: "ALL"})
		// Restrict entity visibility separately from field projection visibility.
		d.Grants[1].Actions = []string{"data.field.read"}
		owner := policy.DataScope{Kind: "OWNER"}
		resource := policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}
		d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.read"}, Resource: resource, Scope: &owner},
			policy.Grant{Effect: "ALLOW", Actions: []string{"data.row.write", "data.row.assign"}, Resource: resource},
			policy.Grant{Effect: "DENY", Actions: []string{"data.field.read", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.name"}})
		if tc.writeScope != "" {
			scope := policy.DataScope{Kind: tc.writeScope}
			d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.write"}, Resource: resource, Scope: &scope})
		}
		if tc.fieldWrite {
			all := policy.DataScope{Kind: "ALL"}
			d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.field.write"}, Resource: resource, Scope: &all})
		}
		published := r5Publish(t, h, root+"/roles", alice, version, d, true, uuid(t))
		version = published.Version
		if version == 1 {
			r5Assign(t, h, root, alice, f.viewer, d.Name, 200)
		}
		t.Run(tc.name, func(t *testing.T) {
			for _, projection := range [][]string{{"field.contact.id"}, {"field.contact.id", "field.contact.name", "field.contact.status"}} {
				suffix := "core_only"
				if len(projection) > 1 {
					suffix = "masked_fields"
				}
				t.Run(suffix, func(t *testing.T) {
					w := m5Call(h, base+"/query", token, data.Query{RecordID: record.ID, Projection: projection, Limit: 1})
					if w.Code != 200 {
						t.Fatalf("valid assigned owner query status %d (body omitted)", w.Code)
					}
					var page data.Page
					if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Records) != 1 || page.Records[0].OwnerID != f.viewer {
						t.Fatal("invalid owner header")
					}
					if _, ok := page.Records[0].Values["field.contact.name"]; ok {
						t.Fatal("hidden field crossed the port")
					}
				})
			}
		})
	}
}

type independentR5cScopeDenied struct{ policy.Native }

func (independentR5cScopeDenied) DataScope(ctx context.Context, subject policy.Subject, action string, resource policy.Resource, scope policy.ScopeContext) (policy.DataScope, error) {
	if resource.FieldID == "field.contact.name" {
		return policy.DataScope{Kind: "NONE"}, policy.ErrScope
	}
	return (policy.Native{}).DataScope(ctx, subject, action, resource, scope)
}

func TestIndependentR5cOpaqueAllowedActionDeniedScope(t *testing.T) {
	m8Database(t)
	f, h, base, alice, _ := m5Fixture(t)
	record := m5Create(t, h, base, alice, "PRIVATE denied-scope fixture")
	h = m5Handler(t, f, independentR5cScopeDenied{}, audit.Postgres{}, true)
	w := m5Call(h, base+"/query", alice, data.Query{RecordID: record.ID, Projection: []string{"field.contact.id", "field.contact.name"}, Limit: 1})
	if w.Code != 200 {
		t.Fatalf("field DataScope denial should mask without query failure: status %d (body omitted)", w.Code)
	}
	var page data.Page
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Records) != 1 {
		t.Fatal("invalid masked page")
	}
	if _, ok := page.Records[0].Values["field.contact.name"]; ok {
		t.Fatal("denied scope exposed value")
	}
}
