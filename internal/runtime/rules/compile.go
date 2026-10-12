package rules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/erp"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"time"
)

var stable = regexp.MustCompile(`^(rule|field|entity|calendar)\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

func validID(s, prefix string) bool {
	return len(s) <= 160 && len(s) > len(prefix) && s[:len(prefix)] == prefix && stable.MatchString(s)
}
func equalType(a, b Type) bool { return reflect.DeepEqual(a, b) }
func scalar(t Type) bool       { return t.Kind == "integer" || t.Kind == "decimal" }
func compatible(a, b Type) bool {
	if equalType(a, b) || a.Kind == "null" || b.Kind == "null" || scalar(a) && scalar(b) {
		return true
	}
	return (a.Kind == "enum" || a.Kind == "reference") && b.Kind == "string" || (b.Kind == "enum" || b.Kind == "reference") && a.Kind == "string"
}
func validType(t Type) bool {
	if t.Kind == "list" {
		return t.Currency == "" && t.ReferenceTo == "" && t.Element != nil && t.Element.Kind != "list" && t.Element.Kind != "null" && validType(*t.Element)
	}
	if t.Element != nil {
		return false
	}
	if t.Kind == "money" {
		_, e := erp.Currency(t.Currency).MinorUnits()
		return e == nil && t.ReferenceTo == ""
	}
	if t.Currency != "" {
		return false
	}
	if t.Kind == "reference" {
		return validID(t.ReferenceTo, "entity.")
	}
	if t.ReferenceTo != "" {
		return false
	}
	switch t.Kind {
	case "string", "bool", "integer", "decimal", "date", "datetime", "enum", "null":
		return true
	}
	return false
}
func calendarKey(c erp.BusinessCalendar) string { return c.ID + "@" + c.Version }
func calendars(in []erp.BusinessCalendar) ([]erp.BusinessCalendar, error) {
	if len(in) > 8 {
		return nil, ErrBudget
	}
	out := append([]erp.BusinessCalendar{}, in...)
	seen := map[string]bool{}
	for i := range out {
		c := &out[i]
		key := calendarKey(*c)
		if !validID(c.ID, "calendar.") || c.Version == "" || len(c.Version) > 80 || seen[key] || len(c.WorkingDays) == 0 || len(c.WorkingDays) > 7 || len(c.Holidays) > 512 || c.OpenMinute < 0 || c.CloseMinute > 1440 || c.OpenMinute >= c.CloseMinute || c.FiscalStartMonth < 1 || c.FiscalStartMonth > 12 {
			return nil, ErrInvalid
		}
		if _, e := location(string(c.Zone)); e != nil {
			return nil, e
		}
		seen[key] = true
		c.WorkingDays = append([]time.Weekday{}, c.WorkingDays...)
		c.Holidays = append([]string{}, c.Holidays...)
		sort.Slice(c.WorkingDays, func(i, j int) bool { return c.WorkingDays[i] < c.WorkingDays[j] })
		sort.Strings(c.Holidays)
		for j, d := range c.WorkingDays {
			if d < 0 || d > 6 || j > 0 && d == c.WorkingDays[j-1] {
				return nil, ErrInvalid
			}
		}
		for j, s := range c.Holidays {
			if _, e := parseDate(s); e != nil || j > 0 && s == c.Holidays[j-1] {
				return nil, ErrInvalid
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return calendarKey(out[i]) < calendarKey(out[j]) })
	return out, nil
}
func fields(in []Field) ([]Field, error) {
	if len(in) < 1 || len(in) > 32 {
		return nil, ErrBudget
	}
	out := append([]Field{}, in...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	for i := range out {
		f := &out[i]
		if !validID(f.ID, "field.") || !validType(f.Type) || f.Type.Kind == "null" || f.Type.Kind == "list" || i > 0 && out[i-1].ID == f.ID {
			return nil, ErrInvalid
		}
		f.EnumValues = append([]string(nil), f.EnumValues...)
		sort.Strings(f.EnumValues)
		if len(f.EnumValues) > 100 || f.Type.Kind == "enum" && len(f.EnumValues) == 0 || f.Type.Kind != "enum" && len(f.EnumValues) > 0 {
			return nil, ErrInvalid
		}
		for j, s := range f.EnumValues {
			if len(s) > MaxString || j > 0 && s == f.EnumValues[j-1] {
				return nil, ErrInvalid
			}
		}
	}
	return out, nil
}
func literal(e Expression) (Value, error) {
	if e.FieldRef != "" || len(e.Args) > 0 {
		return Value{}, ErrInvalid
	}
	x, err := readJSON(e.Value)
	if err != nil {
		return Value{}, err
	}
	kind := e.ValueType
	if e.Op == "literal" {
		if kind != "" {
			return Value{}, ErrInvalid
		}
		switch v := x.(type) {
		case nil:
			kind = "null"
		case bool:
			kind = "bool"
		case string:
			kind = "string"
		case json.Number:
			kind = "integer"
			if _, er := strconv.ParseInt(v.String(), 10, 64); er != nil {
				kind = "decimal"
			}
		default:
			return Value{}, ErrInvalid
		}
	}
	if e.Op == "typed_literal" && kind == "" {
		return Value{}, ErrInvalid
	}
	t := Type{Kind: kind}
	if kind == "money" {
		m, ok := x.(map[string]any)
		if !ok {
			return Value{}, ErrInvalid
		}
		c, ok := m["currency"].(string)
		if !ok {
			return Value{}, ErrInvalid
		}
		t.Currency = c
	}
	if !validType(t) || t.Kind == "list" || t.Kind == "enum" || t.Kind == "reference" {
		return Value{}, ErrInvalid
	}
	if t.Kind == "null" {
		if x != nil {
			return Value{}, ErrInvalid
		}
		return Value{typ: t, nilValue: true}, nil
	}
	if x == nil {
		return Value{}, ErrInvalid
	}
	return inputValue(t, x)
}

type analyzer struct {
	fields    []Field
	calendars []erp.BusinessCalendar
	nodes     int
}

func (a *analyzer) check(e Expression, depth int) (Node, error) {
	a.nodes++
	if depth > MaxDepth || a.nodes > MaxNodes || len(e.Args) > MaxList {
		return Node{}, ErrBudget
	}
	n := Node{Op: e.Op, Value: append(json.RawMessage(nil), e.Value...), ValueType: e.ValueType, FieldRef: e.FieldRef}
	if e.Op == "literal" || e.Op == "typed_literal" {
		v, err := literal(e)
		n.Type = v.typ
		return n, err
	}
	if e.Op == "field" {
		if len(e.Args) > 0 || len(e.Value) > 0 || e.ValueType != "" {
			return n, ErrInvalid
		}
		for _, f := range a.fields {
			if f.ID == e.FieldRef {
				n.Type = f.Type
				return n, nil
			}
		}
		return n, ErrInvalid
	}
	if e.FieldRef != "" || len(e.Value) > 0 || e.ValueType != "" {
		return n, ErrInvalid
	}
	for _, x := range e.Args {
		child, err := a.check(x, depth+1)
		if err != nil {
			return n, err
		}
		n.Args = append(n.Args, child)
	}
	typ, err := a.infer(n)
	n.Type = typ
	return n, err
}
func (a *analyzer) infer(n Node) (Type, error) {
	boolType := Type{Kind: "bool"}
	count := len(n.Args)
	switch n.Op {
	case "and", "or":
		if count < 2 || count > 16 {
			return Type{}, ErrInvalid
		}
		for _, x := range n.Args {
			if x.Type.Kind != "bool" {
				return Type{}, ErrInvalid
			}
		}
		return boolType, nil
	case "not", "is_null", "lower", "upper", "trim", "money_round":
		if count != 1 {
			return Type{}, ErrInvalid
		}
	case "if":
		if count != 3 || n.Args[0].Type.Kind != "bool" {
			return Type{}, ErrInvalid
		}
	case "business_add_days":
		if count != 3 {
			return Type{}, ErrInvalid
		}
	case "list":
		if count < 1 || count > MaxList {
			return Type{}, ErrInvalid
		}
		t := n.Args[0].Type
		if t.Kind == "null" || t.Kind == "list" {
			return Type{}, ErrInvalid
		}
		for _, x := range n.Args[1:] {
			if !equalType(t, x.Type) {
				return Type{}, ErrInvalid
			}
		}
		return Type{Kind: "list", Element: &t}, nil
	default:
		if count != 2 {
			return Type{}, ErrInvalid
		}
	}
	t := n.Args[0].Type
	switch n.Op {
	case "not":
		if t.Kind != "bool" {
			return Type{}, ErrInvalid
		}
		return boolType, nil
	case "is_null":
		return boolType, nil
	case "eq", "neq":
		if !compatible(t, n.Args[1].Type) || t.Kind == "list" || n.Args[1].Type.Kind == "list" {
			return Type{}, ErrInvalid
		}
		return boolType, nil
	case "gt", "gte", "lt", "lte":
		b := n.Args[1].Type
		if !compatible(t, b) || !(scalar(t) || t.Kind == "money" || t.Kind == "date" || t.Kind == "datetime") || b.Kind == "null" {
			return Type{}, ErrInvalid
		}
		return boolType, nil
	case "in", "not_in":
		b := n.Args[1].Type
		if b.Kind != "list" || !compatible(t, *b.Element) {
			return Type{}, ErrInvalid
		}
		return boolType, nil
	case "contains", "concat":
		if t.Kind != "string" || n.Args[1].Type.Kind != "string" {
			return Type{}, ErrInvalid
		}
		if n.Op == "contains" {
			return boolType, nil
		}
		return t, nil
	case "lower", "upper", "trim":
		if t.Kind != "string" {
			return Type{}, ErrInvalid
		}
		return t, nil
	case "if", "coalesce":
		i := 1
		if n.Op == "if" {
			i = 2
			t = n.Args[1].Type
		}
		b := n.Args[i].Type
		if equalType(t, b) {
			return t, nil
		}
		if scalar(t) && scalar(b) {
			return Type{Kind: "decimal"}, nil
		}
		if t.Kind == "null" {
			return b, nil
		}
		if b.Kind == "null" {
			return t, nil
		}
		return Type{}, ErrInvalid
	case "add", "sub", "mul", "div":
		b := n.Args[1].Type
		if n.Op == "add" || n.Op == "sub" {
			if t.Kind == "money" && equalType(t, b) {
				return t, nil
			}
			if !scalar(t) || !scalar(b) {
				return Type{}, ErrInvalid
			}
		}
		if n.Op == "mul" || n.Op == "div" {
			if t.Kind == "money" && scalar(b) {
				return t, nil
			}
			if !scalar(t) || !scalar(b) {
				return Type{}, ErrInvalid
			}
		}
		if n.Op == "div" || t.Kind == "decimal" || b.Kind == "decimal" {
			return Type{Kind: "decimal"}, nil
		}
		return t, nil
	case "round":
		if !(scalar(t) || t.Kind == "money") || n.Args[1].Type.Kind != "integer" {
			return Type{}, ErrInvalid
		}
		return t, nil
	case "money_round":
		if t.Kind != "money" {
			return Type{}, ErrInvalid
		}
		return t, nil
	case "date_add_days", "datetime_add_seconds":
		want := "date"
		if n.Op == "datetime_add_seconds" {
			want = "datetime"
		}
		if t.Kind != want || n.Args[1].Type.Kind != "integer" {
			return Type{}, ErrInvalid
		}
		return t, nil
	case "date_diff_days":
		if t.Kind != "date" || n.Args[1].Type.Kind != "date" {
			return Type{}, ErrInvalid
		}
		return Type{Kind: "integer"}, nil
	case "date_in_zone":
		if t.Kind != "datetime" || !constantString(n.Args[1]) {
			return Type{}, ErrInvalid
		}
		var z string
		_ = json.Unmarshal(n.Args[1].Value, &z)
		if _, e := location(z); e != nil {
			return Type{}, e
		}
		return Type{Kind: "date"}, nil
	case "business_add_days", "is_business_day", "business_is_open", "fiscal_year":
		idx := 1
		if n.Op == "business_add_days" {
			idx = 2
			if n.Args[1].Type.Kind != "integer" {
				return Type{}, ErrInvalid
			}
		}
		want := "date"
		if n.Op == "business_is_open" {
			want = "datetime"
		}
		if t.Kind != want || !constantString(n.Args[idx]) {
			return Type{}, ErrInvalid
		}
		var key string
		_ = json.Unmarshal(n.Args[idx].Value, &key)
		found := false
		for _, c := range a.calendars {
			if calendarKey(c) == key {
				found = true
			}
		}
		if !found {
			return Type{}, ErrInvalid
		}
		if n.Op == "business_add_days" {
			return t, nil
		}
		if n.Op == "fiscal_year" {
			return Type{Kind: "integer"}, nil
		}
		return boolType, nil
	}
	return Type{}, ErrInvalid
}
func constantString(n Node) bool {
	return (n.Op == "literal" || n.Op == "typed_literal") && n.Type.Kind == "string"
}
func Compile(d Definition) (Program, error) {
	if d.Version != Version || !validID(d.ID, "rule.") || !validID(d.EntityID, "entity.") {
		return Program{}, ErrInvalid
	}
	fs, e := fields(d.Fields)
	if e != nil {
		return Program{}, e
	}
	cs, e := calendars(d.Calendars)
	if e != nil {
		return Program{}, e
	}
	root, e := (&analyzer{fields: fs, calendars: cs}).check(d.Expression, 0)
	if e != nil || root.Type.Kind != d.ResultType {
		return Program{}, ErrInvalid
	}
	raw, e := json.Marshal(Plan{Version, d.ID, d.EntityID, TZDataHash(), fs, cs, root})
	if e != nil || len(raw) > 256<<10 {
		return Program{}, ErrBudget
	}
	return Program{raw, Hash(raw)}, nil
}
func Hash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

// Restore validates stored typed nodes; it never compiles an authoring definition.
func Restore(raw []byte, expected string) (Program, error) {
	if len(raw) == 0 || len(raw) > 256<<10 || Hash(raw) != expected {
		return Program{}, ErrInvalid
	}
	var p Plan
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || p.Version != Version || p.TZDataHash != TZDataHash() || !validID(p.ID, "rule.") || !validID(p.EntityID, "entity.") {
		return Program{}, ErrInvalid
	}
	fs, e := fields(p.Fields)
	if e != nil || !reflect.DeepEqual(fs, p.Fields) {
		return Program{}, ErrInvalid
	}
	cs, e := calendars(p.Calendars)
	if e != nil || !reflect.DeepEqual(cs, p.Calendars) {
		return Program{}, ErrInvalid
	}
	a := analyzer{fields: fs, calendars: cs}
	var check func(Node, int) error
	check = func(n Node, depth int) error {
		a.nodes++
		if a.nodes > MaxNodes || depth > MaxDepth || len(n.Args) > MaxList || !validType(n.Type) {
			return ErrBudget
		}
		var typ Type
		var err error
		if n.Op == "literal" || n.Op == "typed_literal" {
			v, e := literal(Expression{Op: n.Op, Value: n.Value, ValueType: n.ValueType, FieldRef: n.FieldRef})
			typ = v.typ
			err = e
			if len(n.Args) > 0 {
				return ErrInvalid
			}
		}
		if n.Op == "field" {
			if len(n.Args) > 0 || len(n.Value) > 0 || n.ValueType != "" {
				return ErrInvalid
			}
			for _, f := range fs {
				if f.ID == n.FieldRef {
					typ = f.Type
				}
			}
		}
		if n.Op != "literal" && n.Op != "typed_literal" && n.Op != "field" {
			if len(n.Value) > 0 || n.ValueType != "" || n.FieldRef != "" {
				return ErrInvalid
			}
			for _, child := range n.Args {
				if e := check(child, depth+1); e != nil {
					return e
				}
			}
			typ, err = a.infer(n)
		}
		if err != nil || !equalType(typ, n.Type) {
			return ErrInvalid
		}
		return nil
	}
	if check(p.Root, 0) != nil {
		return Program{}, ErrInvalid
	}
	canonical, e := json.Marshal(p)
	if e != nil || !bytes.Equal(canonical, raw) {
		return Program{}, ErrInvalid
	}
	return Program{append([]byte(nil), raw...), expected}, nil
}

// ExpressionOf strips annotations for source/compiled-plan binding verification.
func ExpressionOf(n Node) Expression {
	e := Expression{Op: n.Op, Value: n.Value, ValueType: n.ValueType, FieldRef: n.FieldRef}
	for _, a := range n.Args {
		e.Args = append(e.Args, ExpressionOf(a))
	}
	return e
}

// CanonicalInputs compares pinned fields and calendar snapshots without compiling.
func CanonicalInputs(fs []Field, cs []erp.BusinessCalendar) ([]Field, []erp.BusinessCalendar, error) {
	f, e := fields(fs)
	if e != nil {
		return nil, nil, e
	}
	c, e := calendars(cs)
	return f, c, e
}
