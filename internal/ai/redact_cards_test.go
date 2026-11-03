package ai

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"time"
)

// PF-09 (independent post-fix review): the card rule was one regular expression that takes the longest match from the left and a check that
// refuses it, so a card number that follows another number ("Order 99871 4111 ...") or is followed by one (the expiry date) was never
// looked at, and nothing was replaced. The numbers below are made at run time from a label (luhnNumber), like every secret-shaped value of
// these tests.

// singleMatchCard is the rule as it was: one match from the left, checked once. It is kept to show that the inputs below are the ones it
// missed, so that the tests do not pass for a reason other than the scan.
var singleMatchCard = redactRule{class: "card", re: regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), ok: func(m string) bool {
	var d []int
	for i := 0; i < len(m); i++ {
		if m[i] >= '0' && m[i] <= '9' {
			d = append(d, int(m[i]-'0'))
		}
	}
	return cardIssuer(d) && luhn(d)
}}

// cardRule is the card rule of the table.
func cardRule(t *testing.T) redactRule {
	t.Helper()
	for _, r := range redactRules {
		if r.class == "card" {
			return r
		}
	}
	t.Fatal("no card rule in the table")
	return redactRule{}
}

func TestCardNumbersBesideOtherNumbers(t *testing.T) {
	flat := luhnNumber("4", "pf09-flat", 16)
	spaced := luhnNumber("4", "pf09-spaced", 16)
	hyphen := luhnNumber("5", "pf09-hyphen", 16)
	amex := luhnNumber("37", "pf09-amex", 15)
	long := luhnNumber("4", "pf09-long", 19)
	cases := []struct {
		name, text, card string
		keeps            string // text around the card that is not part of it and stays
	}{
		{"after an order number", "Order 99871 " + flat + " 12/27", flat, "Order 99871 "},
		{"after a short number", "id 123 " + groups(spaced, " "), spaced, "id 123 "},
		{"after a two digit number", "12 " + groups(spaced, " "), spaced, "12 "},
		{"after a date written without separators", "ref 20261029 " + groups(spaced, " "), spaced, "ref 20261029 "},
		{"after a number and a hyphen", "x-99871-" + groups(hyphen, "-"), hyphen, "x-99871-"},
		{"followed by the expiry date and a code", groups(spaced, " ") + " 12/27 123", spaced, " 12/27 123"},
		{"followed by one more group", groups(spaced, " ") + " 12", spaced, " 12"},
		{"flat, followed by a number", "Card " + flat + " 1234 declined", flat, "declined"},
		{"flat, between two numbers", "7 " + flat + " 7", flat, "7 "},
		{"amex in groups, followed by a number", groups(amex, " ") + " 0927", amex, " 0927"},
		{"19 digits in groups, after a number", "No. 17 " + groups(long, " "), long, "No. 17 "},
		{"inside a run of short groups", "1 2 3 " + groups(spaced, " ") + " 4 5 6", spaced, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, n := redactText(c.text)
			if !strings.Contains(out, marker("card")) || n < 1 {
				t.Fatalf("no card marker (n=%d): %q", n, out)
			}
			for _, chunk := range []string{c.card[:4], c.card[len(c.card)-4:]} {
				if strings.Contains(out, chunk) {
					t.Fatalf("a part of the card (%s) reaches the model: %q", chunk, out)
				}
			}
			if !strings.Contains(out, c.keeps) {
				t.Fatalf("the rest of the text (%q) was changed: %q", c.keeps, out)
			}
			if again, m := redactText(out); again != out || m != 0 {
				t.Fatalf("not a fixed point: %q -> %q (%d)", out, again, m)
			}
		})
	}
}

// The single-match rule leaves the repro texts of the review as written; the scan does not.
func TestTheSingleMatchRuleMissedTheReviewInputs(t *testing.T) {
	flat := luhnNumber("4", "pf09-review", 16)
	for _, text := range []string{
		"Order 99871 " + flat + " 12/27",
		"id 123 " + groups(flat, " "),
		"12 " + groups(flat, " "),
		"ref 20261029 " + groups(flat, " "),
		"x-99871-" + groups(flat, "-"),
	} {
		var old, now int
		if got := singleMatchCard.apply(text, &old); got != text || old != 0 {
			t.Fatalf("the single-match rule redacts %q (%d): the review's input is not the failure it was", text, old)
		}
		if got := cardRule(t).apply(text, &now); got == text || now != 1 {
			t.Fatalf("the scan leaves %q as written (%d)", text, now)
		}
	}
}

// windowSpan is a run of 13 to 19 digits with a single space or hyphen allowed between two digits.
var windowSpan = regexp.MustCompile(`^\d(?:[ -]?\d){12,18}$`)

// windowOracle reports whether s holds a payment card number the way the rule is defined: a span of 13 to 19 digits with single
// separators, a word boundary on each side, an issuer prefix and a valid check digit. It tries every start and every end, and shares
// only the two checks with the scan.
func windowOracle(s string) bool {
	word := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' || i > 0 && word(s[i-1]) {
			continue
		}
		for j := i + 13; j <= len(s) && j <= i+37; j++ {
			if j < len(s) && word(s[j]) || !windowSpan.MatchString(s[i:j]) {
				continue
			}
			var d []int
			for k := i; k < j; k++ {
				if s[k] >= '0' && s[k] <= '9' {
					d = append(d, int(s[k]-'0'))
				}
			}
			if cardIssuer(d) && luhn(d) {
				return true
			}
		}
	}
	return false
}

func TestNumbersThatAreNotCardsStay(t *testing.T) {
	short := luhnNumber("4", "pf09-short", 16)[:11]
	glued := luhnNumber("4", "pf09-glued", 16)
	cases := []string{
		"Number " + luhnNumber("9", "pf09-no-issuer", 16) + " has a valid check digit and no card network.",
		"Order 99871 " + luhnNumber("9", "pf09-no-issuer-2", 16) + " 12/27",
		"Stamp " + luhnNumber("20", "pf09-stamp", 14) + " is a timestamp.",
		"Tracking " + luhnNumber("1", "pf09-tracking", 16) + " left the depot.",
		"Period 2026-10-29 2026-10-30 2026-11-01 inclusive",
		"Tel 9412 3456 7890 1234 5678 9012 3456 7890",
		"Version 12 34 56 78 90 12 34 56 78 90",
		glued + "a",                  // glued to a letter: no word boundary, as before
		"a" + glued,                  // and before
		groups(glued, "_"),           // an underscore is not a separator
		groups(glued, "  "),          // two spaces break the chain
		strings.Repeat("7", 20),      // 20 digits in one token
		groups(short, " ") + " done", // 11 digits
	}
	for _, text := range cases {
		if windowOracle(text) {
			t.Fatalf("%q holds a card number by the definition: the case is wrong", text)
		}
		var n int
		if out := cardRule(t).apply(text, &n); out != text || n != 0 {
			t.Errorf("%q was changed: %q", text, out)
		}
	}
}

// A random text of numbers, separators and words: the scan redacts something exactly when the definition finds a card number in it, and
// what it leaves holds none.
func TestCardScanAgreesWithTheDefinition(t *testing.T) {
	var numbers []string
	for i, spec := range []struct {
		prefix string
		length int
	}{{"4", 16}, {"4", 13}, {"4", 19}, {"51", 16}, {"55", 16}, {"2221", 16}, {"37", 15}, {"6011", 16}, {"35", 16}, {"36", 14}, {"62", 17}, {"65", 16}} {
		n := luhnNumber(spec.prefix, "pf09-prop-"+string(rune('a'+i)), spec.length)
		numbers = append(numbers, n, groups(n, " "), groups(n, "-"), n[:4]+" "+n[4:])
	}
	rule := cardRule(t)
	rnd := rand.New(rand.NewSource(9))
	separators := []string{" ", "-", " ", "  ", "/", "x", "a", ".", "\n", ": ", "_", " - "}
	hits, misses := 0, 0
	for k := 0; k < 4000; k++ {
		var b strings.Builder
		for step, steps := 0, 1+rnd.Intn(10); step < steps; step++ {
			switch rnd.Intn(5) {
			case 0:
				b.WriteString(numbers[rnd.Intn(len(numbers))])
			case 1:
				for d, digits := 0, 1+rnd.Intn(8); d < digits; d++ {
					b.WriteByte(byte('0' + rnd.Intn(10)))
				}
			default:
				b.WriteString(separators[rnd.Intn(len(separators))])
			}
		}
		text := b.String()
		var n int
		out := rule.apply(text, &n)
		if windowOracle(text) {
			hits++
			if n == 0 || out == text {
				t.Fatalf("a card number is left in %q", text)
			}
			if windowOracle(out) {
				t.Fatalf("a card number is left after the redaction: %q -> %q", text, out)
			}
		} else {
			misses++
			if n != 0 || out != text {
				t.Fatalf("a text without a card number was changed: %q -> %q", text, out)
			}
		}
	}
	if hits < 200 || misses < 200 {
		t.Fatalf("the sample does not exercise both sides: %d with a card number, %d without", hits, misses)
	}
}

// The scan is linear: a long chain of short groups tries a few windows per group, not every window of the chain.
func TestCardScanIsLinear(t *testing.T) {
	texts := map[string]string{
		"single digits":       strings.Repeat("1 ", 100000),
		"short groups":        strings.Repeat("12-", 70000),
		"four digit groups":   strings.Repeat("5555 ", 50000),
		"one long digit run":  strings.Repeat("9", 250000),
		"digits and a letter": strings.Repeat("1234 5678 9", 20000) + "x",
	}
	rule := cardRule(t)
	for name, text := range texts {
		start := time.Now()
		var n int
		rule.apply(text, &n)
		if took := time.Since(start); took > 5*time.Second {
			t.Errorf("%s: %d bytes took %s", name, len(text), took)
		}
	}
}
