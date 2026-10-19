package metadata

import (
	"errors"
	"strings"
	"testing"
)

func TestDecodeJSONBoundedSharesTheStrictRules(t *testing.T) {
	code := func(err error) string {
		var invalid *Invalid
		if errors.As(err, &invalid) && len(invalid.Issues) == 1 {
			return invalid.Issues[0].Code
		}
		return "plain"
	}
	for name, tc := range map[string]struct {
		raw  string
		max  int
		want string // "" = accepted
	}{
		"object":        {`{"a":1,"b":[true,null,"x"]}`, 64, ""},
		"duplicate":     {`{"a":1,"a":2}`, 64, "duplicate_key"},
		"case folded":   {`{"revision":1,"Revision":2}`, 64, "duplicate_key"},
		"trailing":      {`{"a":1} {"b":2}`, 64, "trailing_json"},
		"too big":       {`{"a":"` + strings.Repeat("x", 100) + `"}`, 64, "invalid_json"},
		"empty":         {``, 64, "invalid_json"},
		"deep":          {strings.Repeat("[", 40) + strings.Repeat("]", 40), 1024, "complexity_limit"},
		"inexact":       {`{"a":0.1234567890123456789}`, 64, "number_limit"},
		"nul in string": {"{\"a\":\"x\\u0000y\"}", 64, "plain"},
	} {
		_, err := DecodeJSONBounded([]byte(tc.raw), tc.max)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: rejected: %v", name, err)
		case tc.want != "" && err == nil:
			t.Errorf("%s: accepted", name)
		case tc.want != "" && code(err) != tc.want && tc.want != "plain":
			t.Errorf("%s: code %q, want %q", name, code(err), tc.want)
		}
	}
}
