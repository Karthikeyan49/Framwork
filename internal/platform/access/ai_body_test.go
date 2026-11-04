package access

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// FU-05 (independent review of the follow-up): the request text of an AI creation went through the metadata decoder, which answered the escape of half of a surrogate pair
// with U+FFFD, so the request was stored, hashed and sent to the provider with a character that its author did not write. aiBody refuses it now, as it refuses bytes that
// are not UTF-8. The escapes are built from parts: a tool that carries this file would turn each well-formed one into a character.
func TestAIBodyRefusesALoneSurrogateEscape(t *testing.T) {
	esc := func(digits string) string { return `\u` + digits }
	read := func(text string) (reply, bool, string) {
		r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(text))
		r.Header.Set("Content-Type", "application/json")
		var dest struct {
			Request string `json:"request"`
		}
		rep, ok := aiBody(r, 1<<16, &dest, []string{"request"})
		return rep, ok, dest.Request
	}
	for name, text := range map[string]string{
		"a high half":                  `{"request":"Rename the order page` + esc("d800") + ` to Orders."}`,
		"a low half":                   `{"request":"Rename the order page` + esc("dc00") + ` to Orders."}`,
		"a half and a letter":          `{"request":"` + esc("d800") + `A"}`,
		"a pair cut by a half":         `{"request":"` + esc("d83d") + esc("d83d") + esc("de00") + `"}`,
		"raw bytes that are not UTF-8": "{\"request\":\"a\xffb\"}",
	} {
		rep, ok, _ := read(text)
		if ok || rep.status != 400 || rep.code != "invalid_ai_request" {
			t.Errorf("%s: accepted or answered %d %s", name, rep.status, rep.code)
		}
	}
	// What is text stays: a pair, the escape of U+FFFD itself, an escaped backslash that is followed by the letters of an escape, and other escapes.
	for name, c := range map[string]struct{ text, want string }{
		"a pair":                 {`{"request":"a` + esc("d83d") + esc("de00") + `b"}`, "a" + string(rune(0x1f600)) + "b"},
		"the escape of U+FFFD":   {`{"request":"a` + esc("fffd") + `b"}`, "a" + string(rune(0xfffd)) + "b"},
		"an escaped backslash":   {`{"request":"\\ud800"}`, `\ud800`},
		"escapes of other marks": {`{"request":"` + esc("0041") + esc("00e9") + `"}`, "A" + string(rune(0xe9))},
	} {
		if rep, ok, got := read(c.text); !ok || got != c.want {
			t.Errorf("%s: refused (%d %s) or read as %q, want %q", name, rep.status, rep.code, got, c.want)
		}
	}
}
