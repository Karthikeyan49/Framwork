package data

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"strings"
	"testing"
)

func TestExactValuesAndTypeBounds(t *testing.T) {
	for _, test := range []struct {
		f     compiler.Field
		value any
		want  bool
	}{
		{compiler.Field{Type: "integer"}, json.Number("9007199254740991"), true},
		{compiler.Field{Type: "integer"}, json.Number("9007199254740992"), false},
		{compiler.Field{Type: "integer"}, json.Number("1.5"), false},
		{compiler.Field{Type: "decimal"}, "0.123456789012345678", true},
		{compiler.Field{Type: "decimal"}, "0.1234567890123456789", false},
		{compiler.Field{Type: "decimal"}, "1e999999", false},
		{compiler.Field{Type: "money", Currency: "USD"}, map[string]any{"amount": "12.30", "currency": "USD"}, true},
		{compiler.Field{Type: "money", Currency: "USD"}, map[string]any{"amount": "12.30", "currency": "EUR"}, false},
		{compiler.Field{Type: "date"}, "2026-02-30", false},
		{compiler.Field{Type: "date"}, "0000-01-01", false},
		{compiler.Field{Type: "date"}, "0001-01-01", true},
		{compiler.Field{Type: "datetime"}, "0001-01-01T00:00:00+01:00", false},
		{compiler.Field{Type: "datetime"}, "9999-12-31T23:59:59-01:00", false},
		{compiler.Field{Type: "datetime"}, "2026-10-11T12:00:00+02:00", true},
		{compiler.Field{Type: "datetime"}, "2026-10-11T12:00:00.123456789Z", true},
		{compiler.Field{Type: "datetime"}, "2026-10-11T12:00:00.1234567890Z", false},
		{compiler.Field{Type: "datetime"}, "2026-10-11T12:00:00+24:00", false},
		{compiler.Field{Type: "datetime"}, "2026-02-30T12:00:00Z", false},
		{compiler.Field{Type: "reference"}, "not-a-uuid", false},
		{compiler.Field{Type: "string", Required: true}, "", false},
		{compiler.Field{Type: "string"}, strings.Repeat("x", 4097), false},
	} {
		_, err := fieldValue(test.f, test.value)
		if (err == nil) != test.want {
			t.Fatalf("type %s validity=%v want %v", test.f.Type, err == nil, test.want)
		}
	}
	got, err := decimal("12.300000000000000000")
	if err != nil || got != "12.3" {
		t.Fatal("decimal roundtrip")
	}
}
func TestQueryBoundsRejectRawSQLAndHiddenFields(t *testing.T) {
	e := compiler.Entity{ID: "entity.contact", Fields: []compiler.Field{{ID: "field.contact.name", Type: "string"}}}
	allow := Guard(func(_ context.Context, _ string, _ Resource) error { return nil })
	b := builder{}
	nodes := 0
	sql, err := filterSQL(context.Background(), e, Filter{Op: "contains", Field: "field.contact.name", Value: json.RawMessage(`"'; DROP TABLE runtime.preview_records; --"`)}, &b, allow, 0, &nodes)
	if err != nil || strings.Contains(sql, "DROP") || len(b.args) != 2 {
		t.Fatal("value became SQL syntax")
	}
	nodes = 0
	if _, err = filterSQL(context.Background(), e, Filter{Op: "sql", Field: "field.contact.name", Value: json.RawMessage(`"x"`)}, &builder{}, allow, 0, &nodes); err == nil {
		t.Fatal("unknown operator accepted")
	}
	nodes = 0
	if _, err = filterSQL(context.Background(), e, Filter{Op: "eq", Field: "field.hidden", Value: json.RawMessage(`"x"`)}, &builder{}, allow, 0, &nodes); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func FuzzTypedFilterNeverEmitsCallerSQL(f *testing.F) {
	f.Add([]byte(`{"op":"contains","field":"field.contact.name","value":"x"}`))
	f.Add([]byte(`{"op":"and","args":[]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > MaxRecordBytes {
			return
		}
		v, err := metadata.DecodeJSON(raw)
		if err != nil {
			return
		}
		raw, _ = json.Marshal(v)
		var filter Filter
		if json.Unmarshal(raw, &filter) != nil {
			return
		}
		e := compiler.Entity{ID: "entity.contact", Fields: []compiler.Field{{ID: "field.contact.name", Type: "string"}}}
		nodes := 0
		fragment, err := filterSQL(context.Background(), e, filter, &builder{}, Guard(func(context.Context, string, Resource) error { return nil }), 0, &nodes)
		if err == nil && (strings.Contains(fragment, ";") || strings.Contains(fragment, "--") || strings.Contains(fragment, "SELECT") || strings.Contains(fragment, "DROP")) {
			t.Fatal("caller SQL reached query fragment")
		}
	})
}
