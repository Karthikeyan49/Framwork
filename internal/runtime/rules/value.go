package rules

import (
	"bytes"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/erp"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

type Value struct {
	typ      Type
	n        decimal
	text     string
	flag     bool
	nilValue bool
	list     []Value
}

func (v Value) Type() Type {
	var t Type
	b, _ := json.Marshal(v.typ)
	_ = json.Unmarshal(b, &t)
	return t
}
func (v Value) IsNull() bool { return v.nilValue }
func (v Value) Bool() (bool, error) {
	if v.nilValue || v.typ.Kind != "bool" {
		return false, ErrInput
	}
	return v.flag, nil
}
func (v Value) MarshalJSON() ([]byte, error) {
	var x any
	if !v.nilValue {
		switch v.typ.Kind {
		case "integer", "decimal":
			x = v.n.String()
		case "money":
			x = struct {
				Amount   string `json:"amount"`
				Currency string `json:"currency"`
			}{v.n.String(), v.typ.Currency}
		case "bool":
			x = v.flag
		case "list":
			x = v.list
		default:
			x = v.text
		}
	}
	return json.Marshal(struct {
		Type  Type `json:"type"`
		Value any  `json:"value"`
	}{v.typ, x})
}
func readJSON(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > MaxString+512 || !utf8.Valid(raw) {
		return nil, ErrInput
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		nodes++
		if nodes > 128 || depth > 4 {
			return nil, ErrBudget
		}
		token, e := d.Token()
		if e != nil {
			return nil, ErrInput
		}
		if delim, ok := token.(json.Delim); ok {
			if delim == '{' {
				m := map[string]any{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return nil, ErrInput
					}
					s, ok := k.(string)
					if !ok {
						return nil, ErrInput
					}
					if _, ok = m[s]; ok {
						return nil, ErrInput
					}
					v, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					m[s] = v
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return nil, ErrInput
				}
				return m, nil
			}
			return nil, ErrInput
		}
		return token, nil
	}
	x, e := read(0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, ErrInput
	}
	return x, nil
}

var referenceID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)
var instant = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

func parseDate(s string) (time.Time, error) {
	t, e := time.Parse("2006-01-02", s)
	if e != nil || t.Year() < 1 || t.Year() > 9999 {
		return time.Time{}, ErrDate
	}
	return t, nil
}
func parseInstant(s string) (time.Time, error) {
	if !instant.MatchString(s) {
		return time.Time{}, ErrDate
	}
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || t.Year() < 1 || t.Year() > 9999 || t.UTC().Year() < 1 || t.UTC().Year() > 9999 {
		return time.Time{}, ErrDate
	}
	return t.UTC(), nil
}
func input(t Type, raw []byte) (Value, error) {
	x, e := readJSON(raw)
	if e != nil {
		return Value{}, e
	}
	return inputValue(t, x)
}
func inputValue(t Type, x any) (Value, error) {
	v := Value{typ: t}
	if x == nil {
		v.nilValue = true
		return v, nil
	}
	switch t.Kind {
	case "integer", "decimal":
		s, ok := x.(string)
		if !ok {
			n, ok := x.(json.Number)
			if !ok {
				return Value{}, ErrInput
			}
			s = n.String()
		}
		n, e := number(s)
		if e != nil {
			return Value{}, e
		}
		if t.Kind == "integer" {
			if _, e = n.integer(); e != nil {
				return Value{}, e
			}
		}
		v.n = n
	case "money":
		m, ok := x.(map[string]any)
		if !ok || len(m) != 2 {
			return Value{}, ErrInput
		}
		s, ok := m["amount"].(string)
		c, cok := m["currency"].(string)
		if !ok || !cok || c != t.Currency {
			return Value{}, ErrInput
		}
		if (erp.Money{Amount: s, Currency: erp.Currency(c)}).Validate() != nil {
			return Value{}, ErrInput
		}
		n, e := number(s)
		if e != nil {
			return Value{}, e
		}
		v.n = n
	case "bool":
		b, ok := x.(bool)
		if !ok {
			return Value{}, ErrInput
		}
		v.flag = b
	case "string", "enum", "reference", "date", "datetime":
		s, ok := x.(string)
		if !ok || len(s) > MaxString || !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
			return Value{}, ErrInput
		}
		v.text = s
		if t.Kind == "reference" && !referenceID.MatchString(s) {
			return Value{}, ErrInput
		}
		if t.Kind == "date" {
			if _, e := parseDate(s); e != nil {
				return Value{}, e
			}
		}
		if t.Kind == "datetime" {
			n, e := parseInstant(s)
			if e != nil {
				return Value{}, e
			}
			v.text = n.Format(time.RFC3339Nano)
		}
	default:
		return Value{}, ErrInput
	}
	return v, nil
}
