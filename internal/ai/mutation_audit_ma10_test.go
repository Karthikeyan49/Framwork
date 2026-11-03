package ai

import "testing"

func TestCleanTextRefusesEveryInvisibleOrReorderingCharacter(t *testing.T) {
	for _, ok := range []string{"plain text", "line one\nline two\ttabbed", "é ü 日本語 🚀"} {
		if !cleanText(ok) {
			t.Errorf("%q refused", ok)
		}
	}
	bad := map[string]rune{
		"NUL": 0x00, "BEL": 0x07, "ESC": 0x1b, "DEL": 0x7f, "C1 start": 0x80, "NEL": 0x85, "C1 end": 0x9f,
		"LRE": 0x202a, "RLO": 0x202e, "LRI": 0x2066, "FSI": 0x2068, "PDI": 0x2069,
		"line separator": 0x2028, "paragraph separator": 0x2029, "BOM": 0xfeff, "zero width space": 0x200b, "word joiner": 0x2060, "mongolian vowel separator": 0x180e,
		"tag start": 0xe0000, "tag end": 0xe007f,
	}
	for name, r := range bad {
		if cleanText("a" + string(r) + "b") {
			t.Errorf("%s (U+%04X) accepted", name, r)
		}
	}
}
