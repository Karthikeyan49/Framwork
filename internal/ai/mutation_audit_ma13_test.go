package ai

import (
	"strings"
	"testing"
)

func TestRequestTextRefusesInvisibleAndControlCharactersAndBlank(t *testing.T) {
	for _, ok := range []string{"add a field", "two\nlines\tand\rreturns", "é 日本語 🚀", strings.Repeat("x", MaxRequestRunes)} {
		if !validRequestText(ok) {
			t.Errorf("%.20q refused", ok)
		}
	}
	around := func(r rune) string { return "a" + string(r) + "b" }
	bad := map[string]string{"empty": "", "blank": " \t\n", "invalid utf8": "a\xffb", "too long": strings.Repeat("x", MaxRequestRunes+1)}
	for name, r := range map[string]rune{
		"NUL": 0x00, "ESC": 0x1b, "DEL": 0x7f, "C1": 0x85, "ALM": 0x061c, "LRM": 0x200e, "RLM": 0x200f, "RLO": 0x202e, "LRI": 0x2066, "PDI": 0x2069,
		"line separator": 0x2028, "paragraph separator": 0x2029, "BOM": 0xfeff, "zero width space": 0x200b, "word joiner": 0x2060, "invisible times": 0x2062, "annotation anchor": 0xfff9,
	} {
		bad[name] = around(r)
	}
	for name, s := range bad {
		if validRequestText(s) {
			t.Errorf("%s accepted", name)
		}
	}
}
