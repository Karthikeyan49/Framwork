package metadata

import "testing"

// FU-05 (independent review of the follow-up): DecodeJSON refused bytes that are not UTF-8 and accepted the escape of half of a surrogate pair, which the decoder
// answers with U+FFFD, in a value and in a key. Every author-facing decoder inherits the refusal (documents, previews, promotion, dataset admission, the AI
// request body, the tool inputs of a workflow). The escapes are built from parts: a tool that carries this file would turn each well-formed one into a character.
func TestDecodeJSONRefusesALoneSurrogateEscape(t *testing.T) {
	esc := func(digits string) string { return `\u` + digits }
	for name, text := range map[string]string{
		"a high half in a value":     `{"label":"a` + esc("d800") + `b"}`,
		"a low half in a value":      `{"label":"a` + esc("dc00") + `b"}`,
		"a half in a key":            `{"la` + esc("d800") + `bel":"x"}`,
		"a half in an array":         `["x","` + esc("dbff") + `"]`,
		"a high half and a letter":   `{"label":"` + esc("d800") + `A"}`,
		"a pair cut by another half": `{"label":"` + esc("d83d") + esc("d83d") + esc("de00") + `"}`,
		"after an escaped backslash": `{"label":"\\` + esc("d800") + `"}`,
	} {
		if _, err := DecodeJSON([]byte(text)); err == nil {
			t.Errorf("%s: accepted: %s", name, text)
		}
		if _, err := DecodeJSONBounded([]byte(text), 1<<16); err == nil {
			t.Errorf("%s: accepted by the bounded decoder: %s", name, text)
		}
	}
	for name, text := range map[string]string{
		"a pair":                         `{"label":"a` + esc("d83d") + esc("de00") + `b"}`,
		"the escape of U+FFFD itself":    `{"label":"a` + esc("fffd") + `b"}`,
		"the character itself":           "{\"label\":\"a\xef\xbf\xbdb\"}",
		"an escaped backslash then text": `{"label":"\\ud800"}`,
		"escapes of other characters":    `{"label":"` + esc("0041") + esc("00e9") + esc("20ac") + `\n"}`,
	} {
		if _, err := DecodeJSON([]byte(text)); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
	// A raw invalid byte stays refused.
	if _, err := DecodeJSON([]byte("{\"label\":\"a\xffb\"}")); err == nil {
		t.Error("an invalid byte is accepted")
	}
}
