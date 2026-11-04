package metadata

import "testing"

// Independent reviews found a parser differential: encoding/json folds Unicode
// case when it matches struct fields, so a second key that differs only by case
// (or by a folding rune such as U+017F) silently overrode the first.
func TestDecodeJSONRejectsKeysThatDifferOnlyByCase(t *testing.T) {
	for name, raw := range map[string]string{
		"ascii case":   `{"revision":1,"Revision":2}`,
		"long s":       `{"host":"a","ho` + "ſ" + `t":"b"}`,
		"kelvin sign":  `{"k":1,"` + "K" + `":2}`,
		"nested":       `{"outer":{"id":1,"ID":2}}`,
		"inside array": `[{"a":1,"A":2}]`,
	} {
		if _, err := DecodeJSON([]byte(raw)); err == nil {
			t.Errorf("%s: accepted %s", name, raw)
		}
	}
	for _, raw := range []string{`{"revision":1}`, `{"a":1,"b":2}`, `{"a":{"a":1},"b":{"a":2}}`} {
		if _, err := DecodeJSON([]byte(raw)); err != nil {
			t.Errorf("control %s: %v", raw, err)
		}
	}
}
