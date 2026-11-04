package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// The tests are written with "@u" for the backslash and the u of an escape and rewritten when the file is made: a tool that carries this file
// would turn every well-formed escape into the character it stands for, and the file would no longer test the escapes.

// TF-07: the escape form of a character the decoder cannot represent. The decoder answers U+FFFD for it, and for the escape of U+FFFD itself, so the
// two cannot be told apart afterwards: the scan finds the first on the text.
func TestHasLoneSurrogateEscape(t *testing.T) {
	for name, text := range map[string]string{
		"a high half alone":                  `{"a":"@ud800"}`,
		"a low half alone":                   `{"a":"@udc00"}`,
		"the last low half alone":            `{"a":"@udfff"}`,
		"the last high half alone":           `{"a":"@udbff"}`,
		"a high half and a letter":           `{"a":"@ud800A"}`,
		"a high half and another escape":     `{"a":"@ud800\n"}`,
		"a high half and a high half":        `{"a":"@ud800@ud800@udc00"}`,
		"a low half before a high half":      `{"a":"@udc00@ud800"}`,
		"a half in a member name":            `{"@ud800":1}`,
		"upper-case digits":                  `{"a":"@uD800"}`,
		"mixed case digits":                  `{"a":"@uDc00"}`,
		"after a pair":                       `{"a":"@ud83d@ude00@ud83d"}`,
		"in an array":                        `["x","@udc00"]`,
		"after an escaped backslash":         `{"a":"\\@ud800"}`,
		"at the end of the text, truncated":  `{"a":"@ud800`,
		"a high half and a short low escape": `{"a":"@ud800@udc0"}`,
		"a high half and a non-low escape":   `{"a":"@ud800@u0041"}`,
	} {
		text = strings.ReplaceAll(text, "@u", `\u`)
		if !HasLoneSurrogateEscape([]byte(text)) {
			t.Errorf("%s: %s is not found", name, text)
		}
	}
	for name, text := range map[string]string{
		"a pair":                          `{"a":"@ud83d@ude00"}`,
		"the replacement character":       `{"a":"@ufffd"}`,
		"the character itself":            "{\"a\":\"\xef\xbf\xbd\"}",
		"escapes of other characters":     `{"a":"@u0041@u00e9@u20ac\n\t\"\\\/"}`,
		"an escaped backslash then text":  `{"a":"\\ud800"}`,
		"the text of an escape in a name": `{"\\ud800":1}`,
		"outside a string":                `{"a":1}`,
		"a pair at the edges":             `["@ud800@udc00","@udbff@udfff"]`,
		"a malformed escape":              `{"a":"@ud8"}`,
		"a bad digit":                     `{"a":"@ud80g"}`,
		"a quote after a backslash":       `{"a":"\"@ud800@udc00"}`,
		"empty":                           ``,
	} {
		text = strings.ReplaceAll(text, "@u", `\u`)
		if HasLoneSurrogateEscape([]byte(text)) {
			t.Errorf("%s: %s is refused", name, text)
		}
	}
}

// The scan agrees with the decoder: for a JSON string the decoder puts U+FFFD where the text had a half of a pair, and the scan finds exactly the
// texts for which it does. Random strings of escapes, pairs, characters and the replacement character.
func TestLoneSurrogateScanAgreesWithTheDecoder(t *testing.T) {
	pieces := []string{"@ud800", "@udbff", "@udc00", "@udfff", "@ud83d@ude00", "@ufffd", "\xef\xbf\xbd", "@u0041", "a", `\\`, `\"`, `\n`, "@u00e9", "\xc3\xa9", `\\u`, "u", "@ud83d", "@ude00"}
	for i := range pieces {
		pieces[i] = strings.ReplaceAll(pieces[i], "@u", `\u`)
	}
	escFFFD := `\u` + "fffd" // the six characters of the escape of U+FFFD
	state := uint32(7)
	next := func(n int) int {
		state = state*1664525 + 1013904223
		return int(state>>16) % n
	}
	lone, clean := 0, 0
	for k := 0; k < 20000; k++ {
		var b strings.Builder
		spelled := 0 // the replacement characters that the text spells itself, as the character or as its escape
		for i, n := 0, 1+next(6); i < n; i++ {
			piece := pieces[next(len(pieces))]
			if piece == escFFFD || piece == "\xef\xbf\xbd" {
				spelled++
			}
			b.WriteString(piece)
		}
		text := `"` + b.String() + `"`
		var decoded string
		if json.Unmarshal([]byte(text), &decoded) != nil {
			t.Fatalf("a text of well-formed pieces is not a JSON string: %q", text)
		}
		substituted := strings.Count(decoded, "\xef\xbf\xbd") > spelled
		if got := HasLoneSurrogateEscape([]byte(text)); got != substituted {
			t.Fatalf("text %q decodes to %q: scan says %v, decoder substituted %v (spelled %d, decoded %d)", text, decoded, got, substituted, spelled, strings.Count(decoded, "\xef\xbf\xbd"))
		}
		if substituted {
			lone++
		} else {
			clean++
		}
		if !utf8.ValidString(decoded) {
			t.Fatalf("the decoder produced invalid UTF-8 for %q", text)
		}
	}
	if lone < 500 || clean < 500 {
		t.Fatalf("the sample does not exercise both sides: %d with a half, %d without", lone, clean)
	}
}
