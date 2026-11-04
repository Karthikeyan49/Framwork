//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
)

func TestM9RCustomSortPagePreservesTiesNullsScopesAndJoins(t *testing.T) {
	m8Database(t)
	f, h, base, token, _ := m5Fixture(t)
	parent := m5Create(t, h, base, token, "Parent")
	low := []string{}
	for _, name := range []string{"Same A", "Same B"} {
		values := m5Values(name)
		values["field.contact.balance"] = map[string]any{"amount": "3", "currency": "USD"}
		values["field.contact.parent"] = parent.ID
		w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values})
		if w.Code != 200 {
			t.Fatal("fixture create", w.Code)
		}
		var record data.Record
		if json.Unmarshal(w.Body.Bytes(), &record) != nil {
			t.Fatal("fixture row")
		}
		low = append(low, record.ID)
	}
	values := m5Values("Null balance")
	delete(values, "field.contact.balance")
	w := m5Call(h, base+"/commands", token, data.Command{Operation: "create", Values: values})
	if w.Code != 200 {
		t.Fatal("null fixture")
	}
	var nullRecord data.Record
	_ = json.Unmarshal(w.Body.Bytes(), &nullRecord)
	sort.Strings(low)
	for _, desc := range []bool{false, true} {
		t.Run(map[bool]string{false: "ascending", true: "descending"}[desc], func(t *testing.T) {
			query := m5Query()
			query.Limit = 1
			query.Sort = &data.Sort{Field: "field.contact.balance", Desc: desc}
			query.Join = &data.Join{Relationship: "relationship.contact.parent", Projection: []string{"field.contact.name"}}
			actual := []string{}
			for range 4 {
				page := m5Page(t, m5Call(h, base+"/query", token, query))
				if len(page.Records) != 1 {
					t.Fatal("page cardinality")
				}
				r := page.Records[0]
				actual = append(actual, r.ID)
				if r.ID == low[0] || r.ID == low[1] {
					related := r.Related[query.Join.Relationship]
					if related == nil || related.ID != parent.ID || related.Values["field.contact.name"] != "Parent" {
						t.Fatal("declared join lost during page fetch")
					}
				}
				query.After = page.Next
			}
			expected := append(append([]string{}, low...), parent.ID, nullRecord.ID)
			if desc {
				expected = []string{parent.ID, low[0], low[1], nullRecord.ID}
			}
			for i, id := range expected {
				if actual[i] != id {
					t.Fatal("sort/cursor changed tie or null ordering")
				}
			}
			if query.After != "" {
				t.Fatal("terminal page cursor")
			}
		})
	}
	hidden := m5Handler(t, f, denyDataPolicy{"data.field.read", "field.contact.balance"}, audit.Postgres{}, true)
	query := m5Query()
	query.Sort = &data.Sort{Field: "field.contact.balance"}
	if denied := m5Call(hidden, base+"/query", token, query); denied.Code != 403 {
		t.Fatal("hidden sort field inferred through bounded page")
	}
	// A conditional row scope must select the authorized page before LIMIT.
	selected := m5Handler(t, f, pageSelectionPolicy{}, audit.Postgres{}, true)
	query.Limit = 1
	page := m5Page(t, m5Call(selected, base+"/query", token, query))
	if len(page.Records) != 1 || page.Records[0].Values["field.contact.name"] != "Same B" || page.Next != "" {
		t.Fatal("row scope applied after page selection")
	}
	// Mask an unrelated projected field without changing the numeric sort page.
	// Predicates over the denied field still fail closed in the ABAC regressions.
	scoped := m5Handler(t, f, contextualDataPolicy{"field_at_row"}, audit.Postgres{}, true)
	page = m5Page(t, m5Call(scoped, base+"/query", token, query))
	if len(page.Records) != 1 || page.Records[0].ID != low[0] || page.Records[0].FieldAccess == nil || len(page.Records[0].FieldAccess.Masked) != 1 || page.Records[0].FieldAccess.Masked[0] != "field.contact.name" {
		t.Fatal("field mask changed numeric page selection")
	}
	if _, present := page.Records[0].Values["field.contact.name"]; present {
		t.Fatal("field scope bypass")
	}
}

type pageSelectionPolicy struct{ policy.Native }

func (p pageSelectionPolicy) DataScope(ctx context.Context, s policy.Subject, action string, r policy.Resource, c policy.ScopeContext) (policy.DataScope, error) {
	scope, err := p.Native.DataScope(ctx, s, action, r, c)
	if err == nil && action == "data.read" && r.FieldID == "" {
		return policy.DataScope{Kind: "CONDITION", Condition: &policy.ScopeCondition{Field: "field.contact.name", Op: "eq", Value: json.RawMessage(`"Same B"`)}}, nil
	}
	return scope, err
}
