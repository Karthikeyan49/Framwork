package ai

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Redaction of secret-shaped tokens from author-controlled text before it leaves the
// platform inside a model request (ADR040 section 4). It is a safety net for the
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
}

var redactRules = []redactRule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?(?:-----END [A-Z0-9 ]+-----|$)`)},
	{"url_credentials", regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.\-]*://[^\s/?#@"'<>\\]*@[^\s"'<>\\]+`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)},
	{"api_key", regexp.MustCompile(`\b(?:(?:sk|rk|pk)-[A-Za-z0-9_-]{16,}|(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{10,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{30,}|ya29\.[0-9A-Za-z_-]{20,}|glpat-[A-Za-z0-9_-]{16,}|npm_[A-Za-z0-9]{30,}|SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,})`)},
	{"aws_key_id", regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA|ANVA|AIPA|ABIA|ACCA)[0-9A-Z]{16}\b`)},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`)},
	{"authorization", regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer\s+|basic\s+|token\s+)?[^\s,;"']{8,}`)},
}

// assignment finds "name = value" pairs with a secret-like name. The "=" form is
// redacted for any value of four or more characters; the ":" form (ordinary prose
// such as "Password: required") only when the value looks like a credential.
var assignment = regexp.MustCompile(`(?i)\b(api[_-]?key|apikey|secret(?:[_-]?key)?|client[_-]?secret|access[_-]?token|auth[_-]?token|refresh[_-]?token|token|password|passwd|pwd|passphrase|private[_-]?key|credentials?)\b(\s*[:=]\s*)("[^"\n]{1,200}"|'[^'\n]{1,200}'|[^\s,;&"']{1,200})`)

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
	if strings.Contains(sep, "=") {
		if len(value) < 4 {
			return m
		}
		return marker("secret")
	}
	if len(value) < 8 || !strings.ContainsAny(value, "0123456789-_/+=.@!#$%^&*") {
		return m
	}
	return marker("secret")
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
func redactText(s string) (string, int) {
	n := 0
	count := func(class string) func(string) string {
		return func(string) string { n++; return marker(class) }
	}
	for _, r := range redactRules {
		s = r.re.ReplaceAllStringFunc(s, count(r.class))
	}
	s = assignment.ReplaceAllStringFunc(s, func(m string) string {
		out := assignmentReplace(m)
		if out != m {
			n++
		}
		return out
	})
	s = longHex.ReplaceAllStringFunc(s, count("hex"))
	s = longBase64.ReplaceAllStringFunc(s, func(m string) string {
		out := base64Replace(m)
		if out != m {
			n++
		}
		return out
	})
	return s, n
}

// redactStructural is the part of the redactor that also applies to identifier-shaped
// strings: the credential formats with a recognisable prefix and long hex runs. An
// identifier-shaped string is structure (ids, references, versions), so the generic
// assignment and base64-entropy heuristics skip it, but a lower-case token such as
// "ghp_..." or a 64-digit digest starting with a letter is shaped like an identifier
// too and must not slip through on that account.
func redactStructural(s string) (string, int) {
	n := 0
	count := func(class string) func(string) string {
		return func(string) string { n++; return marker(class) }
	}
	for _, r := range redactRules {
		s = r.re.ReplaceAllStringFunc(s, count(r.class))
	}
	s = longHex.ReplaceAllStringFunc(s, count("hex"))
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
