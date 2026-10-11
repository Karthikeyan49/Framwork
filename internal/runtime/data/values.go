package data

import (
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
	"math/big"
	"regexp"
	"time"
	"unicode/utf8"
)

var timestampPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](\.[0-9]{1,9})?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)

var decimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)

func decimal(s string) (string, error) {
	if len(s) > 64 || !decimalPattern.MatchString(s) {
		return "", ErrInvalid
	}
	n, ok := new(big.Rat).SetString(s)
	if !ok || new(big.Rat).Abs(n).Cmp(new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))) > 0 {
		return "", ErrInvalid
	}
	if n.IsInt() {
		return n.Num().String(), nil
	}
	out := n.FloatString(18)
	for out[len(out)-1] == '0' {
		out = out[:len(out)-1]
	}
	return out, nil
}
func fieldValue(f compiler.Field, value any) (any, error) {
	if value == nil {
		if f.Required {
			return nil, ErrInvalid
		}
		return nil, nil
	}
	if f.CoreID {
		s, ok := value.(string)
		if !ok || !tenant.ValidID(s) {
			return nil, ErrInvalid
		}
		return s, nil
	}
	switch f.Type {
	case "string":
		s, ok := value.(string)
		if !ok || len(s) > 4096 || !utf8.ValidString(s) {
			return nil, ErrInvalid
		}
		if f.Required && s == "" {
			return nil, ErrInvalid
		}
		return s, nil
	case "enum":
		s, ok := value.(string)
		if ok {
			for _, e := range f.EnumValues {
				if e == s {
					return s, nil
				}
			}
		}
		return nil, ErrInvalid
	case "bool":
		v, ok := value.(bool)
		if !ok {
			return nil, ErrInvalid
		}
		return v, nil
	case "integer":
		v, ok := value.(json.Number)
		if !ok {
			return nil, ErrInvalid
		}
		n, err := v.Int64()
		if err != nil || n < -9007199254740991 || n > 9007199254740991 {
			return nil, ErrInvalid
		}
		return json.Number(v.String()), nil
	case "decimal":
		v, ok := value.(string)
		if !ok {
			return nil, ErrInvalid
		}
		return decimal(v)
	case "money":
		v, ok := value.(map[string]any)
		if !ok || len(v) != 2 || v["currency"] != f.Currency {
			return nil, ErrInvalid
		}
		amount, ok := v["amount"].(string)
		if !ok {
			return nil, ErrInvalid
		}
		amount, err := decimal(amount)
		if err != nil {
			return nil, err
		}
		return map[string]any{"amount": amount, "currency": f.Currency}, nil
	case "reference":
		s, ok := value.(string)
		if !ok || !tenant.ValidID(s) {
			return nil, ErrInvalid
		}
		return s, nil
	case "date":
		s, ok := value.(string)
		if !ok || len(s) != 10 {
			return nil, ErrInvalid
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil || t.Year() < 1 || t.Year() > 9999 {
			return nil, ErrInvalid
		}
		return t.Format("2006-01-02"), nil
	case "datetime":
		s, ok := value.(string)
		if !ok || len(s) > 40 || !timestampPattern.MatchString(s) {
			return nil, ErrInvalid
		}
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil || t.UTC().Year() < 1 || t.UTC().Year() > 9999 {
			return nil, ErrInvalid
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	}
	return nil, ErrInvalid
}
func field(e compiler.Entity, id string) (compiler.Field, bool) {
	for _, f := range e.Fields {
		if f.ID == id {
			return f, true
		}
	}
	return compiler.Field{}, false
}
func values(e compiler.Entity, input map[string]any, id string, write bool) (map[string]any, error) {
	out := map[string]any{}
	for key := range input {
		f, ok := field(e, key)
		if !ok || write && f.CoreID {
			return nil, ErrInvalid
		}
	}
	for _, f := range e.Fields {
		value, exists := input[f.ID]
		if f.CoreID {
			out[f.ID] = id
			continue
		}
		if !exists && !f.Required {
			continue
		}
		normalized, err := fieldValue(f, value)
		if err != nil {
			return nil, err
		}
		out[f.ID] = normalized
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > MaxRecordBytes {
		return nil, ErrInvalid
	}
	if _, err = metadata.DecodeJSON(raw); err != nil {
		return nil, ErrInvalid
	}
	return out, nil
}
