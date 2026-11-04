package policy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClosedCapabilityDefinitions(t *testing.T) {
	raw := []byte(`{"schema_version":"tenant-capabilities-v1","name":"finance_manager","grants":[{"effect":"ALLOW","actions":["data.entity.read","data.field.read"],"resource":{"app_id":"app.contacts","entity_id":"entity.contact","field_id":"*"},"scope":{"kind":"OWNER"}}]}`)
	good, err := CompileDefinition(raw, "")
	if err != nil || good.Name() != "finance_manager" || len(good.Hash()) != 64 {
		t.Fatal("valid definition rejected", err)
	}
	bad := map[string][]byte{
		"case alias":       []byte(strings.Replace(string(raw), `"name":"finance_manager"`, `"name":"finance_manager","Name":"member"`, 1)),
		"duplicate":        []byte(strings.Replace(string(raw), `"name":"finance_manager"`, `"name":"member","name":"finance_manager"`, 1)),
		"trailing":         append(append([]byte{}, raw...), []byte(`{}`)...),
		"invalid utf8":     {0xff},
		"bootstrap":        []byte(strings.Replace(string(raw), `"grants":`, `"bootstrap":true,"grants":`, 1)),
		"unknown action":   []byte(strings.ReplaceAll(string(raw), "data.entity.read", "tools.execute_sql")),
		"wildcard action":  []byte(strings.ReplaceAll(string(raw), "data.entity.read", "data.*")),
		"row scope":        []byte(strings.ReplaceAll(string(raw), "data.entity.read", "data.row.read")),
		"aggregate scope":  []byte(strings.ReplaceAll(string(raw), "data.entity.read", "data.aggregate.read")),
		"preview scope":    []byte(strings.ReplaceAll(string(raw), "data.entity.read", "preview.read")),
		"arbitrary teams":  []byte(strings.Replace(string(raw), `"kind":"OWNER"`, `"kind":"TEAM","teams":["forged"]`, 1)),
		"write share":      []byte(strings.Replace(string(raw), `"kind":"OWNER"`, `"kind":"SHARE","write_share":true`, 1)),
		"conditional deny": []byte(strings.Replace(string(raw), `"effect":"ALLOW"`, `"effect":"DENY"`, 1)),
		"null grants":      []byte(`{"schema_version":"tenant-capabilities-v1","name":"member","grants":null}`),
		"missing scope":    []byte(strings.Replace(string(raw), `,"scope":{"kind":"OWNER"}`, "", 1)),
		"code":             []byte(strings.Replace(string(raw), `"kind":"OWNER"`, `"kind":"SQL","sql":"select true"`, 1)),
		"wrong selector":   []byte(strings.Replace(string(raw), "app.contacts", "entity.contacts", 1)),
	}
	for name, v := range bad {
		t.Run(name, func(t *testing.T) {
			if _, err := CompileDefinition(v, ""); err == nil {
				t.Fatal("unsafe definition accepted")
			}
		})
	}
	if _, err := CompileDefinition(raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}

func TestCombinedScopeBudgetAndResourceShapes(t *testing.T) {
	leaf := DataScope{Kind: "OR", Children: []DataScope{{Kind: "OWNER"}, {Kind: "TEAM"}, {Kind: "SHARE"}, {Kind: "NONE"}, {Kind: "ALL"}, {Kind: "OWNER"}, {Kind: "TEAM"}, {Kind: "SHARE"}}}
	def := Definition{SchemaVersion: DefinitionVersion, Name: "member", Grants: []Grant{}}
	for i := 0; i < 8; i++ {
		def.Grants = append(def.Grants, Grant{Effect: "ALLOW", Actions: []string{"data.entity.read"}, Resource: Selector{AppID: "*", EntityID: "*", FieldID: "*"}, Scope: &leaf})
	}
	raw, _ := json.Marshal(def)
	if _, err := CompileDefinition(raw, ""); err == nil {
		t.Fatal("combined SQL scope budget exceeded")
	}
	for _, tc := range []struct {
		action string
		r      Resource
		want   bool
	}{
		{"profile.read", Resource{Type: "user", OwnerID: "bob"}, false},
		{"profile.read", Resource{Type: "user", OwnerID: "alice"}, true},
		{"data.field.read", Resource{Type: "field", AppID: "app.contacts", EntityID: "entity.contact"}, false},
		{"data.field.read", Resource{Type: "field", AppID: "app.contacts", EntityID: "entity.contact", FieldID: "field.contact.name"}, true},
		{"workflows.execute", Resource{Type: "workflow", AppID: "app.contacts", EntityID: "entity.contact"}, false},
		{"roles.assign", Resource{Type: "entity"}, false},
	} {
		if validResource(tc.action, tc.r, "alice") != tc.want {
			t.Fatal("malformed capability resource allowed", tc.action)
		}
	}
	if (Defined{hash: strings.Repeat("a", 64), legacy: true}).policyVersion() == (Defined{hash: strings.Repeat("a", 64)}).policyVersion() {
		t.Fatal("bootstrap adapter version lost")
	}
}

func TestScopeCopiesDoNotModifyHashedDefinition(t *testing.T) {
	source := DataScope{Kind: "OR", Children: []DataScope{{Kind: "CONDITION", Condition: &ScopeCondition{Field: "field.contact.name", Op: "eq", Value: json.RawMessage(`"value"`)}}, {Kind: "TEAM"}, {Kind: "SHARE"}}}
	c := ScopeContext{Teams: []string{"trusted"}}
	a := liveScope(source, c, true)
	a.Children[0].Condition.Field = "forged"
	a.Children[0].Condition.Value[1] = 'X'
	a.Children[1].Teams[0] = "forged"
	if source.Children[0].Condition.Field != "field.contact.name" || string(source.Children[0].Condition.Value) != `"value"` || c.Teams[0] != "trusted" || !a.Children[2].WriteShare {
		t.Fatal("scope result aliases immutable policy or trusted teams")
	}
}
