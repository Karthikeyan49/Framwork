package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Redaction of secret-shaped tokens from author-controlled text before it leaves the
// platform inside a model request (ADR104 section 4). It is a safety net for the
// accident (a key pasted into a label, a DSN in a description), not a data-loss
// prevention system: a deliberate exfiltration through free text is outside its
// threat model and is covered by the operator's decision to enable a provider at all.
//
// The replacement is "[REDACTED:<class>]". A proposal whose definition contains that
// prefix is refused by ApplyProposal, so a placeholder the model echoes back can
// never be written into the draft.

const redactionPrefix = "[REDACTED:"

func marker(class string) string { return redactionPrefix + class + "]" }

type redactRule struct {
	class string
	re    *regexp.Regexp
	// ok, when set, decides whether a match really is a secret (a checksum, a decoding); a match it refuses stays as written.
	ok func(match string) bool
}

func (r redactRule) apply(s string, n *int) string {
	return r.re.ReplaceAllStringFunc(s, func(m string) string {
		if r.ok != nil && !r.ok(m) {
			return m
		}
		*n++
		return marker(r.class)
	})
}

var redactRules = []redactRule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?(?:-----END [A-Z0-9 ]+-----|$)`), nil},
	{"url_credentials", regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.\-]*://[^\s/?#@"'<>\\]*@[^\s"'<>\\]+`), nil},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`), nil},
	{"api_key", regexp.MustCompile(`\b(?:(?:sk|rk|pk)-[A-Za-z0-9_-]{16,}|(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{10,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|xapp-[0-9]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{30,}|ya29\.[0-9A-Za-z_-]{20,}|glpat-[A-Za-z0-9_-]{16,}|npm_[A-Za-z0-9]{30,}|SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}|hf_[A-Za-z0-9]{30,}|api_org_[A-Za-z0-9]{30,}|whsec_[A-Za-z0-9]{16,})`), nil},
	{"aws_key_id", regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA|ANVA|AIPA|ABIA|ACCA)[0-9A-Z]{16}\b`), nil},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`), nil},
	{"authorization", regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer\s+|basic\s+|token\s+)?[^\s,;"']{8,}`), nil},
	// The signature of an Azure shared access signature (a query value, URL-encoded base64) and of any other "sig=" link.
	{"sas_signature", regexp.MustCompile(`(?i)\b(?:sig|signature)=[A-Za-z0-9%+/_-]{16,}={0,3}`), nil},
	// "Basic <token>" without the header name around it; only a token that decodes to "user:password" is a credential, so "basic
	// information" and "basic requirements" stay.
	{"basic_auth", regexp.MustCompile(`(?i)\bbasic\s+[A-Za-z0-9+/_-]{6,}={0,2}`), basicCredentials},
	// A Cookie or Set-Cookie header whose line holds a pair with a value of eight characters or more: a session, not a preference.
	{"cookie", regexp.MustCompile(`(?i)\b(?:set-)?cookie[ \t]*:[^\r\n]*?[A-Za-z0-9_.%-]+=[^\s;]{8,}[^\r\n]*`), nil},
	// A payment card number: 13 to 19 digits, alone or in groups, with a known issuer prefix and a valid check digit.
	{"card", regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), cardNumber},
}

// assignment finds "name = value" pairs with a secret-like name, also behind other words of a variable name (DB_PASSWORD,
// STRIPE_SECRET_KEY). The "=" form is redacted for any value of four or more characters; the ":" form (ordinary prose such as
// "Password: required") only when the value looks like a credential; "name is value" (the admin password is ...) only when the
// value has a digit or a symbol and so is not a word like "required".
var assignment = regexp.MustCompile(`(?i)\b(?:[a-z0-9]+[_-])*(api[_-]?key|apikey|secret(?:[_-]?(?:access[_-]?)?key)?|client[_-]?secret|access[_-]?token|auth[_-]?token|refresh[_-]?token|token|password|passwd|pwd|passphrase|private[_-]?key|account[_-]?key|(?:shared[_-]?)?access[_-]?key|credentials?)\b(\s*[:=]\s*|\s+(?:is|was|are)\s+)("[^"\n]{1,200}"|'[^'\n]{1,200}'|[^\s,;&"']{1,200})`)

var (
	longHex    = regexp.MustCompile(`\b[0-9A-Fa-f]{32,}\b`)
	longBase64 = regexp.MustCompile(`[A-Za-z0-9+/_-]{40,}={0,2}`)
)

func assignmentReplace(m string) string {
	parts := assignment.FindStringSubmatch(m)
	if parts == nil {
		return m
	}
	sep, value := parts[2], strings.Trim(parts[3], `"'`)
	switch {
	case strings.Contains(sep, "="):
		if len(value) < 4 {
			return m
		}
	case strings.Contains(sep, ":"):
		if len(value) < 8 || !strings.ContainsAny(value, "0123456789-_/+=.@!#$%^&*") {
			return m
		}
	default:
		// Prose: the end of the sentence is not part of the value, and "." and "-" are as common in words as in passwords.
		trailing := value[len(strings.TrimRight(value, ".,;:!?)]}")):]
		if value = value[:len(value)-len(trailing)]; len(value) < 6 || !strings.ContainsAny(value, "0123456789@!#$%^&*+/=") {
			return m
		}
		return marker("secret") + trailing
	}
	return marker("secret")
}

// basicCredentials reports whether the match of the basic_auth rule is HTTP Basic credentials: a base64 token (standard or URL
// alphabet, padded or not) that decodes to printable text "user:password".
func basicCredentials(m string) bool {
	fields := strings.Fields(m)
	if len(fields) != 2 {
		return false
	}
	token := strings.NewReplacer("-", "+", "_", "/").Replace(strings.TrimRight(fields[1], "="))
	decoded, err := base64.RawStdEncoding.DecodeString(token)
	if err != nil || !utf8.Valid(decoded) || bytes.IndexByte(decoded, ':') < 0 {
		return false
	}
	for _, c := range decoded {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// cardNumber reports whether the match of the card rule is a payment card number.
func cardNumber(m string) bool {
	digits := make([]int, 0, 19)
	for i := 0; i < len(m); i++ {
		if m[i] >= '0' && m[i] <= '9' {
			digits = append(digits, int(m[i]-'0'))
		}
	}
	return cardIssuer(digits) && luhn(digits)
}

// cardIssuer checks the prefix and the length of the major card networks (Visa, Mastercard, American Express, Discover, JCB, Diners
// Club, UnionPay); without it one in ten long numbers (a timestamp, a tracking number) would pass the check digit.
func cardIssuer(d []int) bool {
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	prefix := func(k int) int {
		v := 0
		for _, x := range d[:k] {
			v = v*10 + x
		}
		return v
	}
	n := len(d)
	switch p2, p3, p4 := prefix(2), prefix(3), prefix(4); {
	case d[0] == 4:
		return n == 13 || n == 16 || n == 19
	case p2 >= 51 && p2 <= 55 || p4 >= 2221 && p4 <= 2720:
		return n == 16
	case p2 == 34 || p2 == 37:
		return n == 15
	case p4 == 6011 || p2 == 65 || p3 >= 644 && p3 <= 649:
		return n >= 16
	case p4 >= 3528 && p4 <= 3589:
		return n >= 16
	case p2 == 36 || p2 == 38 || p3 >= 300 && p3 <= 305:
		return n >= 14
	case p2 == 62:
		return n >= 16
	}
	return false
}

// luhn is the check digit of card numbers.
func luhn(d []int) bool {
	sum := 0
	for i, double := len(d)-1, false; i >= 0; i, double = i-1, !double {
		v := d[i]
		if double {
			if v *= 2; v > 9 {
				v -= 9
			}
		}
		sum += v
	}
	return sum%10 == 0
}

// keyMarkerAtEnd finds a redacted token of a key class at the end of a line: the head of a key that may be wrapped onto the next.
var keyMarkerAtEnd = regexp.MustCompile(`\[REDACTED:(api_key|aws_key_id|hex|token|jwt)\]$`)

// keyWord reports a word of the key alphabet, six characters or longer.
func keyWord(w string) bool {
	if len(w) < 6 {
		return false
	}
	for i := 0; i < len(w); i++ {
		switch c := w[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '+', c == '/', c == '_', c == '=', c == '-':
		default:
			return false
		}
	}
	return true
}

// credentialShaped reports a word that reads as part of a key rather than as a word of prose: it holds a digit or one of "+/=",
// or it mixes lower and upper case after its first letter.
func credentialShaped(w string) bool {
	lower, inner := false, false
	for i := 0; i < len(w); i++ {
		switch c := w[i]; {
		case c >= '0' && c <= '9' || c == '+' || c == '/' || c == '=':
			return true
		case c >= 'a' && c <= 'z':
			lower = true
		case c >= 'A' && c <= 'Z' && i > 0:
			inner = true
		}
	}
	return lower && inner
}

// redactWrappedTails redacts the lines that continue a redacted token that ends its line: a line that holds one credential-shaped
// word of the key alphabet and nothing else, then the next such line, to the end of the key. A word followed by other text on its
// line is prose or a different token and is left alone: the tail of a key that a program broke in the middle of a paragraph is a
// residual (ADR104 Amendment C). One pass over the lines.
func redactWrappedTails(s string, n *int) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	class := "" // the class of the key token that ends the previous line
	changed := false
	for i, line := range lines {
		body := strings.TrimRight(line, " \t\r")
		if class != "" {
			start := len(body) - len(strings.TrimLeft(body, " \t"))
			if word := body[start:]; keyWord(word) && credentialShaped(word) {
				lines[i] = line[:start] + marker(class) + line[len(body):]
				*n++
				changed = true
				continue // the marker just written ends this line, so the next one may continue it
			}
		}
		class = ""
		if m := keyMarkerAtEnd.FindStringSubmatch(body); m != nil {
			class = m[1]
		}
	}
	if !changed {
		return s
	}
	return strings.Join(lines, "\n")
}

// entropy is the Shannon entropy of s in bits per byte.
func entropy(s string) float64 {
	if s == "" {
		return 0
	}
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	var h float64
	n := float64(len(s))
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / n
			h -= p * math.Log2(p)
		}
	}
	return h
}

func base64Replace(m string) string {
	hasDigit, hasLetter := false, false
	for i := 0; i < len(m); i++ {
		switch {
		case m[i] >= '0' && m[i] <= '9':
			hasDigit = true
		case m[i] >= 'a' && m[i] <= 'z' || m[i] >= 'A' && m[i] <= 'Z':
			hasLetter = true
		}
	}
	if !hasDigit || !hasLetter || entropy(m) < 4.0 {
		return m
	}
	return marker("token")
}

// redactText replaces secret-shaped tokens in s and reports how many it replaced.
// It is idempotent: a marker is not secret-shaped.
//
// A secret written with characters that only look like ASCII (a Cyrillic "s" in "sk-", fullwidth letters, a soft hyphen or a joiner
// in the middle of a key, a no-break space after "Bearer") is not seen by the table, so the text is also read the way a person
// reads it (foldLookalikes). That reading is kept only when it shows a secret the plain pass could not: a string without a hidden
// secret is returned exactly as written, in whatever script, and a string with one is returned folded.
func redactText(s string) (string, int) {
	out, n := settled(s)
	if folded := foldLookalikes(out); folded != out {
		if again, m := settled(folded); m > 0 {
			return again, n + m
		}
	}
	return out, n
}

// settled applies the table until it finds nothing more. Replacing one token can leave its neighbour a token of its own (a card
// number that was glued to a key by a hyphen), and a second look must not find what the first left, so the result is a fixed point.
func settled(s string) (string, int) {
	out, n := redactPlain(s)
	for i := 0; n > 0 && i < 3; i++ {
		next, m := redactPlain(out)
		if m == 0 {
			break
		}
		out, n = next, n+m
	}
	return out, n
}

func redactPlain(s string) (string, int) {
	n := 0
	for _, r := range redactRules {
		s = r.apply(s, &n)
	}
	s = assignment.ReplaceAllStringFunc(s, func(m string) string {
		out := assignmentReplace(m)
		if out != m {
			n++
		}
		return out
	})
	s = longHex.ReplaceAllStringFunc(s, func(string) string { n++; return marker("hex") })
	s = longBase64.ReplaceAllStringFunc(s, func(m string) string {
		out := base64Replace(m)
		if out != m {
			n++
		}
		return out
	})
	return redactWrappedTails(s, &n), n
}

// redactStructural is the part of the redactor that also applies to identifier-shaped
// strings: the credential formats with a recognisable prefix and long hex runs. An
// identifier-shaped string is structure (ids, references, versions), so the generic
// assignment and base64-entropy heuristics skip it, but a lower-case token such as
// "ghp_..." or a 64-digit digest starting with a letter is shaped like an identifier
// too and must not slip through on that account.
func redactStructural(s string) (string, int) {
	n := 0
	for _, r := range redactRules {
		s = r.apply(s, &n)
	}
	s = longHex.ReplaceAllStringFunc(s, func(string) string { n++; return marker("hex") })
	return s, n
}

// dirtyRune reports a rune that never reaches a model or a reviewer: C0/C1 controls
// other than newline and tab, bidirectional overrides and isolates, line and
// paragraph separators, zero-width space, word joiner, byte-order mark and Unicode
// tag characters (an invisible-instruction channel).
func dirtyRune(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20 || r == 0x7f || r >= 0x80 && r <= 0x9f:
		return true
	case r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069:
		return true
	case r == 0x2028 || r == 0x2029 || r == 0xfeff || r == 0x200b || r == 0x2060 || r == 0x180e:
		return true
	case r >= 0xe0000 && r <= 0xe007f:
		return true
	}
	return false
}

// sanitizeText drops dirty runes and invalid UTF-8 from author text.
func sanitizeText(s string) string {
	if !strings.ContainsFunc(s, dirtyRune) && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if r == utf8.RuneError || dirtyRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// identifierLike strings (ids, references, versions, currency codes) are structure,
// not prose: they are never redacted, or a long id would break its own references.
var identifierLike = regexp.MustCompile(`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)*$|^[0-9]+(?:\.[0-9]+)*$|^[A-Z]{3}$`)

// redactTree sanitizes and redacts every string value of a decoded JSON tree in
// place; object keys are structure and stay. It returns the number of replacements.
func redactTree(node any) (any, int) {
	switch v := node.(type) {
	case string:
		v = sanitizeText(v)
		if identifierLike.MatchString(v) {
			return redactStructural(v)
		}
		return redactText(v)
	case []any:
		total := 0
		for i, e := range v {
			var n int
			v[i], n = redactTree(e)
			total += n
		}
		return v, total
	case map[string]any:
		total := 0
		for k, e := range v {
			var n int
			v[k], n = redactTree(e)
			total += n
		}
		return v, total
	}
	return node, 0
}

// redactString sanitizes and redacts one author-supplied string. Identifier-shaped
// strings are structure and stay.
func redactString(s string) string {
	s = sanitizeText(s)
	if identifierLike.MatchString(s) {
		out, _ := redactStructural(s)
		return out
	}
	out, _ := redactText(s)
	return out
}

// redactRaw encodes an author-controlled value as compact JSON with every string
// sanitized and redacted. Server-built values (hashes, revisions) are never passed
// through it: a 64-digit digest is exactly what the hex rule removes from author text.
func redactRaw(v any) (json.RawMessage, error) {
	out, _, err := redactedJSON(v)
	return json.RawMessage(out), err
}

// redactedJSON encodes v as compact JSON with every string sanitized and redacted.
func redactedJSON(v any) (string, int, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", 0, err
	}
	var tree any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err = dec.Decode(&tree); err != nil {
		return "", 0, err
	}
	tree, n := redactTree(tree)
	out, err := json.Marshal(tree)
	return string(out), n, err
}

// holdsRedaction reports a definition that contains a redaction placeholder.
func holdsRedaction(d metadata.Definition) bool {
	raw, err := json.Marshal(d)
	return err != nil || strings.Contains(string(raw), redactionPrefix)
}
