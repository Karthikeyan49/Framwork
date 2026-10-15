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

func TestIndependentR5cMaskedDelegateAccessChanges(t *testing.T) {
	m8Database(t)
	f, legacy, base, alice, app := m5Fixture(t)
	h := r5Handler(t, f, audit.Postgres{})
	root := "/v1/tenants/" + f.tenantA
	token := f.token(t, f.viewerSubject, nil)
	var version int64
	cases := []struct {
		name                                         string
		previewWrite, fieldWrite, hiddenName, assign bool
		foreignOwner, noWriteScope, corruptHidden    bool
		want                                         int
	}{
		{name: "missing_preview_write_control", hiddenName: true, assign: true, want: 403},
		{name: "masked_assign_only", previewWrite: true, hiddenName: true, assign: true, want: 200},
		{name: "broad_fields_missing_assign_control", previewWrite: true, fieldWrite: true, want: 403},
		{name: "outside_owner_scope_control", previewWrite: true, fieldWrite: true, assign: true, foreignOwner: true, want: 404},
		{name: "none_write_scope_control", previewWrite: true, fieldWrite: true, assign: true, noWriteScope: true, want: 403},
		{name: "malformed_hidden_business_value", previewWrite: true, hiddenName: true, assign: true, corruptHidden: true, want: 200},
		{name: "broad_fields_assign_control", previewWrite: true, fieldWrite: true, assign: true, want: 200},
	}
	for _, tc := range cases {
		record := m5Create(t, legacy, base, alice, "PRIVATE administration fixture")
		path := base + "/records/" + record.ID + "/access"
		initialOwner, initialVersion := f.alice, int64(1)
		if !tc.foreignOwner {
			r5cChange(t, legacy, path, alice, data.AccessCommand{Operation: "owner", ExpectedVersion: 1, UserID: f.viewer}, 200)
			initialOwner, initialVersion = f.viewer, 2
		}
		if tc.corruptHidden {
			tx, err := f.operator.Begin()
			if err != nil {
				t.Fatal("fixture transaction", err)
			}
			if _, err = tx.Exec(`SELECT set_config('app.auth_issuer','https://test-issuer.example',true),set_config('app.auth_subject',$1,true),set_config('app.actor_id',$2,true),set_config('app.tenant_id',$3,true)`, f.aliceSubject, f.alice, f.tenantA); err != nil {
				tx.Rollback()
				t.Fatal("fixture identity", err)
			}
			if _, err = tx.Exec(`UPDATE runtime.records SET custom_data=jsonb_set(custom_data,'{field.contact.balance}','"not money"'::jsonb) WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID); err != nil {
				tx.Rollback()
				t.Fatal("hidden malformed fixture", err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal("fixture commit", err)
			}
		}
		resource := policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "*"}
		all := policy.DataScope{Kind: "ALL"}
		owner := policy.DataScope{Kind: "OWNER"}
		d := r5Definition("review_assign_change", app, all)
		d.Grants[1].Actions = []string{"data.field.read"}
		writeScope := owner
		if tc.noWriteScope {
			writeScope = policy.DataScope{Kind: "NONE"}
		}
		d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.read"}, Resource: resource, Scope: &owner}, policy.Grant{Effect: "ALLOW", Actions: []string{"data.entity.write"}, Resource: resource, Scope: &writeScope}, policy.Grant{Effect: "ALLOW", Actions: []string{"data.row.write"}, Resource: resource})
		if tc.previewWrite {
			d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"preview.write"}, Resource: policy.Selector{AppID: app, EntityID: "*", FieldID: "*"}})
		}
		if tc.assign {
			d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.row.assign"}, Resource: resource})
		}
		if tc.fieldWrite {
			d.Grants = append(d.Grants, policy.Grant{Effect: "ALLOW", Actions: []string{"data.field.write"}, Resource: resource, Scope: &all})
		}
		if tc.hiddenName {
			d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.name"}})
		}
		if tc.corruptHidden {
			d.Grants = append(d.Grants, policy.Grant{Effect: "DENY", Actions: []string{"data.field.read", "data.field.write"}, Resource: policy.Selector{AppID: app, EntityID: "entity.contact", FieldID: "field.contact.balance"}})
		}
		role := r5Publish(t, h, root+"/roles", alice, version, d, true, uuid(t))
		version = role.Version
		if version == 1 {
			r5Assign(t, h, root, alice, f.viewer, d.Name, 200)
		}
		t.Run(tc.name, func(t *testing.T) {
			if !tc.foreignOwner {
				r5cAccess(t, h, path, token, 403)
			} // Never delegate share-list authority implicitly.
			no := false
			if !tc.foreignOwner {
				if denied := m5Call(h, path, token, data.AccessCommand{Operation: "share", ExpectedVersion: initialVersion, UserID: f.alice, CanWrite: &no}); denied.Code != 403 {
					t.Fatal("missing share authority did not deny", denied.Code)
				}
			}
			w := m5Call(h, path, token, data.AccessCommand{Operation: "owner", ExpectedVersion: initialVersion, UserID: f.alice})
			if w.Code != tc.want {
				t.Errorf("owner administration expected %d, got %d (body omitted)", tc.want, w.Code)
			}
			var observed data.Access
			if err := f.operator.QueryRow(`SELECT owner_user_id::text,row_version FROM runtime.records WHERE tenant_id=$1 AND id=$2`, f.tenantA, record.ID).Scan(&observed.OwnerID, &observed.Version); err != nil {
				t.Fatal("record header witness", err)
			}
			expectedOwner, expectedVersion := initialOwner, initialVersion
			if w.Code == 200 {
				expectedOwner, expectedVersion = f.alice, 3
			}
			if observed.OwnerID != expectedOwner || observed.Version != expectedVersion {
				t.Fatal("denial/success did not retain exact witness")
			}
		})
	}
}
