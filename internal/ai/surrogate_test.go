package ai

import (
	"errors"
	"strings"
	"testing"
)

// TF-07 (independent review of the takeover fixes): the provider adapter said that a string payload "is used byte for byte" and decoded it with the
// JSON decoder, which answers U+FFFD for a byte that is not UTF-8 and for the escape of half of a surrogate pair; the answer of the model was stored with a
// character in it that the endpoint did not send. Invalid text is refused, as the answers of the tool gateway are (TS-04). The escapes are built from parts:
// a tool that carries this file would otherwise turn each well-formed one into the character it stands for.

// esc is the escape of the UTF-16 unit with the given four hexadecimal digits.
func esc(digits string) string { return `\u` + digits }

func TestParseEnvelopeRefusesTextThatIsNotTextInsteadOfSubstituting(t *testing.T) {
	inner := func(label string) string { return `{"label":"` + label + `"}` } // the payload, JSON inside a string: its quotes are escaped below
	quoted := func(payload string) string {
		return strings.ReplaceAll(strings.ReplaceAll(payload, `\`, `\\`), `"`, `\"`)
	}
	for name, raw := range map[string]string{
		"a stray byte in the payload":           `{"payload":"` + quoted(inner("ab\xffcd")) + `"}`,
		"a truncated sequence in the payload":   `{"payload":"` + quoted(inner("ab\xe2\x82")) + `"}`,
		"a stray byte in an object payload":     `{"payload":` + inner("ab\xffcd") + `}`,
		"a stray byte in the model label":       "{\"payload\":{},\"model\":\"m\xff\"}",
		"a lone high half in the model label":   `{"payload":{},"model":"m` + esc("d800") + `"}`,
		"a lone low half in the model label":    `{"payload":{},"model":"m` + esc("dc00") + `"}`,
		"a lone half in an object payload":      `{"payload":` + inner("a"+esc("d800")+"b") + `}`,
		"a lone half and a letter, in a string": `{"payload":"x","model":"` + esc("d800") + `A"}`,
	} {
		if _, err := parseEnvelope([]byte(raw)); !errors.Is(err, ErrProviderMalformed) {
			t.Errorf("%s: err=%v, want ErrProviderMalformed", name, err)
		}
	}
	// The controls: a pair, the escape of U+FFFD itself, and text that is UTF-8.
	for name, raw := range map[string]string{
		"a pair of escapes":         `{"payload":"` + quoted(inner("a"+esc("d83d")+esc("de00")+"b")) + `"}`,
		"the replacement character": `{"payload":"` + quoted(inner("a"+esc("fffd")+"b")) + `"}`,
		"multi-byte text":           `{"payload":` + inner("caf\xc3\xa9") + `,"model":"m` + "\xe2\x82\xac" + `"}`,
	} {
		if _, err := parseEnvelope([]byte(raw)); err != nil {
			t.Errorf("%s is refused: %v", name, err)
		}
	}
}

// The payload is JSON inside a string; its own escapes are not seen by the scan of the envelope (they are escaped once more there), so the payload is
// scanned again where it is decoded (strictTree).
func TestAPayloadWithALoneSurrogateEscapeIsMalformed(t *testing.T) {
	for name, payload := range map[string]string{
		"a high half": `{"label":"a` + esc("d800") + `b"}`,
		"a low half":  `{"label":"a` + esc("dc00") + `b"}`,
		"in a key":    `{"la` + esc("d800") + `bel":"x"}`,
	} {
		if _, perr := strictTree([]byte(payload)); perr == nil || !errors.Is(perr.Class, ErrMalformed) {
			t.Errorf("%s: %v, want malformed", name, perr)
		}
	}
	for name, payload := range map[string]string{
		"a pair":                    `{"label":"a` + esc("d83d") + esc("de00") + `b"}`,
		"the replacement character": `{"label":"a` + esc("fffd") + `b"}`,
	} {
		if _, perr := strictTree([]byte(payload)); perr != nil {
			t.Errorf("%s is refused: %v", name, perr)
		}
	}
}
