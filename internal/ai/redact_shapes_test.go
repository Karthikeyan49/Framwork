package ai

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// Secret shapes of the independent M14 review (M14R-08: 10 of 28 shapes survived the redactor) and the ones its probes did not
// reach. The review did not reproduce its literals (they are secret-shaped), and neither does this file: every value is derived
// from a label at run time, so the source holds no string a scanner or a reader could take for a credential.

// material is n deterministic bytes derived from label.
func material(label string, n int) []byte {
	var out []byte
	for i := 0; len(out) < n; i++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", label, i)))
		out = append(out, sum[:]...)
	}
	return out[:n]
}

// alnum is n letters and digits derived from label, with at least one digit and one capital in the middle (a token without either
// is not credential-shaped, and a test of the shape must not depend on luck).
func alnum(label string, n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i, c := range material(label, n) {
		b[i] = alphabet[int(c)%len(alphabet)]
	}
	b[n/3], b[n/2] = '7', 'Q'
	return string(b)
}

func b64(label string, n int) string { return base64.StdEncoding.EncodeToString(material(label, n)) }

// luhnNumber is a number of the given length that starts with prefix and passes the Luhn check.
func luhnNumber(prefix, label string, length int) string {
	digits := []byte(prefix)
	for _, c := range material(label, length-len(prefix)-1) {
		digits = append(digits, '0'+c%10)
	}
	sum := 0
	for i, double := len(digits)-1, true; i >= 0; i, double = i-1, !double {
		d := int(digits[i] - '0')
		if double {
			if d *= 2; d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return string(digits) + string(rune('0'+(10-sum%10)%10))
}

// groups writes digits in groups of four separated by sep.
func groups(digits, sep string) string {
	var parts []string
	for len(digits) > 4 {
		parts = append(parts, digits[:4])
		digits = digits[4:]
	}
	return strings.Join(append(parts, digits), sep)
}

// fullwidth writes the ASCII text of s in the fullwidth forms (U+FF01 to U+FF5E), as a CJK input method does.
func fullwidth(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x21 && r <= 0x7e {
			r += 0xfee0
		}
		b.WriteRune(r)
	}
	return b.String()
}

type secretShape struct {
	name string
	text string   // what an author wrote
	gone []string // fragments of the secret that must not reach a model
}

// reviewShapes are the ten survivors of M14R-08.
func reviewShapes() []secretShape {
	sas := url.QueryEscape(b64("sas", 32))
	account := b64("account", 64)
	basic := base64.StdEncoding.EncodeToString([]byte("svc-reports:" + alnum("basic-pw", 14)))
	prosePassword := "Tr0ub4dor&" + alnum("prose", 8)
	hf := alnum("hf", 34)
	whsec := alnum("whsec", 32)
	session := alnum("session", 24)
	card := luhnNumber("4", "card", 16)
	cyrillic := alnum("cyrillic", 32)
	head, tail := "sk-"+alnum("split", 27), alnum("split-tail", 17)
	return []secretShape{
		{"azure sas signature", "Download it from https://acct.blob.core.windows.net/files/report.csv?sv=2022-11-02&ss=b&sp=rl&se=2030-01-01T00:00:00Z&spr=https&sig=" + sas + " before Friday.", []string{sas[:24]}},
		{"azure storage connection string", "Set it to DefaultEndpointsProtocol=https;AccountName=acct;AccountKey=" + account + ";EndpointSuffix=core.windows.net and restart.", []string{account[:32]}},
		{"azure storage connection string, short key", "DefaultEndpointsProtocol=https;AccountName=acct;AccountKey=" + b64("account-short", 18) + ";EndpointSuffix=core.windows.net", []string{b64("account-short", 18)}},
		{"service bus connection string", "Endpoint=sb://bus.servicebus.windows.net/;SharedAccessKeyName=send;SharedAccessKey=" + b64("bus", 24), []string{b64("bus", 24)}},
		{"basic scheme without a header name", "Send Basic " + basic + " with every call.", []string{basic}},
		{"password in prose", "The admin password is " + prosePassword + " and it rotates monthly.", []string{prosePassword}},
		{"hugging face token", "Use the model hub token " + "hf" + "_" + hf + " for the download.", []string{hf[:20]}},
		{"stripe webhook secret", "The webhook signs with " + "whsec" + "_" + whsec + " as configured.", []string{whsec[:20]}},
		{"cookie header", "Replay it with Cookie: theme=dark; session=" + session + "; lang=en to reproduce.", []string{session}},
		{"set-cookie header", "The response said Set-Cookie: sid=" + session + "; Path=/; HttpOnly", []string{session}},
		{"card number in groups of four", "The customer paid with " + groups(card, " ") + " on Monday.", []string{card, card[:4], card[12:]}},
		{"card number with hyphens", "Card " + groups(card, "-") + " was refused.", []string{card, card[:4], card[12:]}},
		{"look-alike prefix", "Our key is " + "ѕ" + "k-" + cyrillic + " for now.", []string{cyrillic[:20]}},
		{"key split over a line break", "The key:\n" + head + "\n" + tail + "\nplease rotate it.", []string{head[3:20], tail}},
	}
}

// ownShapes are shapes the review's 28 did not include, found by probing the same code.
func ownShapes() []secretShape {
	pw := alnum("env-pw", 20)
	stripe := alnum("env-stripe", 30)
	aws := b64("env-aws", 30)
	card := luhnNumber("5", "card-flat", 16)
	nbsp := alnum("nbsp", 32)
	full := alnum("full", 32)
	soft := alnum("soft", 32)
	return []secretShape{
		{"slack app-level token", "Socket mode uses " + "xapp" + "-1-" + alnum("xapp", 30) + " for the app.", []string{alnum("xapp", 30)[:20]}},
		{"env style prefixed password", "DB_PASSWORD=" + pw, []string{pw}},
		{"env style prefixed secret key", "STRIPE_SECRET_KEY=" + stripe, []string{stripe}},
		{"env style secret access key", "AWS_SECRET_ACCESS_KEY=" + aws, []string{aws[:24]}},
		{"unspaced card number", "Card " + card + " declined.", []string{card}},
		{"bearer after a no-break space", "Authorization header: Bearer " + nbsp, []string{nbsp}},
		{"fullwidth key", "Key " + fullwidth("sk-"+full) + " here.", []string{full, fullwidth(full)[:20]}},
		{"non-breaking hyphen in the prefix", "Key sk\u2011" + alnum("hyphen", 32) + " here.", []string{alnum("hyphen", 32)[:20]}},
		{"mathematical letters in the prefix", "Key \U0001d42c\U0001d424-" + alnum("math", 32) + " here.", []string{alnum("math", 32)[:20]}},
		{"circled letters in the prefix", "Key \u24e2\u24da-" + alnum("circled", 32) + " here.", []string{alnum("circled", 32)[:20]}},
		{"soft hyphen inside a key", "Key sk-" + soft[:12] + "­" + soft[12:] + " here.", []string{soft[:12], soft[12:]}},
		{"joiner inside a key", "Key sk-" + soft[:12] + "‍" + soft[12:] + " here.", []string{soft[:12], soft[12:]}},
	}
}

func TestRedactionCoversTheReviewedShapes(t *testing.T) {
	for _, group := range []struct {
		source string
		shapes []secretShape
	}{{"M14R-08", reviewShapes()}, {"probe", ownShapes()}} {
		for _, s := range group.shapes {
			t.Run(group.source+"/"+s.name, func(t *testing.T) {
				out := redactString(s.text)
				if !strings.Contains(out, redactionPrefix) {
					t.Errorf("nothing was redacted: %q", out)
				}
				for _, g := range s.gone {
					if strings.Contains(out, g) {
						t.Errorf("%q survived in %q", g, out)
					}
				}
				if again := redactString(out); again != out {
					t.Errorf("not idempotent: %q then %q", out, again)
				}
				// The same text inside a JSON tree (a label, a description, a request) goes through the same table.
				tree, n, err := redactedJSON(map[string]any{"label": s.text, "nested": []any{s.text}})
				if err != nil || n < 2 {
					t.Errorf("json: %d replacements, err %v", n, err)
				}
				for _, g := range s.gone {
					if strings.Contains(tree, g) {
						t.Errorf("%q survived in the json form %s", g, tree)
					}
				}
			})
		}
	}
}

// Ordinary text near each rule is left alone: a false positive costs the model a word, but a rule that fires on prose is a rule
// someone removes.
func TestRedactionLeavesTextNearTheNewRulesAlone(t *testing.T) {
	flat := luhnNumber("4", "flat", 16)
	bad := flat[:15] + string(rune('0'+(int(flat[15]-'0')+1)%10)) // the check digit is wrong
	for _, s := range []string{
		"Basic information about the customer is shown first.",
		"Basic requirements: a name and an address.",
		"Use basic authentication for the legacy partner API.",
		"The password is required.",
		"The password is hashed before it is stored.",
		"Password is case-sensitive.",
		"The token is valid for 30 days.",
		"The secret is stored in the vault.",
		"The credentials are checked at sign-in.",
		"Cookie: consent banner shown on first visit",
		"We use a cookie to remember the language.",
		"A sig=ok flag marks signed orders.",
		"signature=required",
		"Invoice 20261018123456 was paid.",
		"Epoch 1700000000000 is the start.",
		"Call +1 415 555 0132 after nine.",
		"Reference 2026 1018 1234 5678 9012 3456 7890 is a batch id.",
		"Card " + bad + " fails the check digit.",
		"Number " + luhnNumber("9", "no-issuer", 16) + " has a valid check digit and no card network.",
		"Stamp " + luhnNumber("20", "stamp", 14) + " is a timestamp.",
		"Tracking " + luhnNumber("1", "tracking", 16) + " left the depot.",
		"Order " + groups(bad, " ") + " is not a card.",
		"Sum 1 234 567 890.",
		"The key to success is a clear name.",
		"Remember the cookie policy page.",
		"DB_HOST=db.internal",
		"MAX_TOKENS=4096",
		"Создать поле Пароль для клиента",
		"Δημιουργία πελάτη",
		"می‌خواهم یک فرم",
		"Family \U0001F468‍\U0001F469‍\U0001F467 is an emoji sequence",
		fullwidth("OK") + "　テスト",
	} {
		if out := redactString(s); out != s {
			t.Errorf("false positive: %q -> %q", s, out)
		}
	}
}

// A string that holds a secret hidden by look-alike characters is redacted as the characters read; a string that does not is kept
// byte for byte, whatever script it is in.
func TestRedactionFoldsLookalikesOnlyToExposeASecret(t *testing.T) {
	key := alnum("mixed", 32)
	out := redactString("Ключ: " + "ѕ" + "k-" + key + " конец")
	if !strings.Contains(out, redactionPrefix) || strings.Contains(out, key[:16]) {
		t.Fatalf("the disguised key survived: %q", out)
	}
	if again := redactString(out); again != out {
		t.Fatalf("not idempotent: %q then %q", out, again)
	}
	// An ASCII secret next to Cyrillic prose is redacted without touching the prose.
	plain := redactString("Ключ: sk-" + key + " конец")
	if want := "Ключ: " + marker("api_key") + " конец"; plain != want {
		t.Fatalf("got %q, want %q", plain, want)
	}
}

func TestRedactionOfAnEnvFileKeepsTheNames(t *testing.T) {
	env := "DB_HOST=db.internal\nDB_PASSWORD=" + alnum("env-file", 18) + "\nLOG_LEVEL=debug\n"
	out := redactString(env)
	if !strings.Contains(out, "DB_HOST=db.internal") || !strings.Contains(out, "LOG_LEVEL=debug") || strings.Contains(out, alnum("env-file", 18)) {
		t.Fatalf("%q", out)
	}
}

// A line that holds one credential-shaped word right after a redacted token is the rest of a key that was wrapped; the words of prose
// that follow a key stay, and so do lines that have more on them than the word.
func TestRedactionOfAWrappedKeyStopsAtProse(t *testing.T) {
	key := "sk-" + alnum("wrapped", 30)
	tail := alnum("tail", 12)
	for _, c := range []struct{ name, in, want string }{
		{"prose after a key", "Key " + key + "\nConfiguration continues below.\nWARNING: rotate it.", "Key " + marker("api_key") + "\nConfiguration continues below.\nWARNING: rotate it."},
		{"one word of prose alone on its line", "Key " + key + "\nConfiguration\nWARNING\nDone.", "Key " + marker("api_key") + "\nConfiguration\nWARNING\nDone."},
		{"short word after a key", "Key " + key + "\nNext step.", "Key " + marker("api_key") + "\nNext step."},
		{"the tail alone on its line", "Key " + key + "\n" + tail + "\nDone.", "Key " + marker("api_key") + "\n" + marker("api_key") + "\nDone."},
		{"the tail indented, with trailing blanks", "Key " + key + " \n   " + tail + "  \nDone.", "Key " + marker("api_key") + " \n   " + marker("api_key") + "  \nDone."},
		{"a tail with text after it is a residual", "Key " + key + "\n" + tail + " is the rest\nDone.", "Key " + marker("api_key") + "\n" + tail + " is the rest\nDone."},
		{"a key in the middle of a line", "Key " + key + " and\n" + tail, "Key " + marker("api_key") + " and\n" + tail},
		{"windows line ends", "Key " + key + "\r\n" + tail + "\r\nDone.", "Key " + marker("api_key") + "\n" + marker("api_key") + "\nDone."}, // the carriage returns are control characters and go first
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := redactString(c.in); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	// A key wrapped over many lines is followed to its end, wherever that is.
	var lines []string
	for i := 0; i < 8; i++ {
		lines = append(lines, alnum(fmt.Sprintf("line-%d", i), 10))
	}
	out := redactString("Key " + key + "\n" + strings.Join(lines, "\n") + "\nDone.")
	if got := strings.Count(out, redactionPrefix); got != 9 || !strings.HasSuffix(out, "\nDone.") {
		t.Errorf("%d markers for a key wrapped over 9 lines: %q", got, out)
	}
}

// Redaction is idempotent whatever the text: the redactor is applied to text that may already have been redacted (a proposal that
// echoes its context, a stored request). Strings built from the fragments of every rule, in random order, on both sides of the fold.
func TestRedactionIsIdempotentOnMixedFragments(t *testing.T) {
	fragments := []string{
		"sk-", "hf_", "whsec_", "ghp_", "AKIA", "password", "token", "secret_key", "DB_PASSWORD", "Cookie:", "Set-Cookie:", "sig=", "AccountKey=", "Basic ", "Bearer ",
		"authorization:", " is ", " was ", "=", ":", " ", " ", "\n", "\r\n", "\t", "-", "_", ".", "%2F", "%3D", "/", "+", ";", "&", "'", "\"",
		"x", "Q", "7", "0", "a1B2", "Zm9v", "dXNlcjpwYXNz", "true", "required", "Configuration", "WARNING",
		alnum("frag-a", 12), alnum("frag-b", 20), alnum("frag-c", 34), alnum("frag-d", 44), b64("frag-e", 24), b64("frag-f", 33),
		luhnNumber("4", "frag-card", 16), groups(luhnNumber("5", "frag-card-2", 16), " "), groups(luhnNumber("37", "frag-card-3", 15), "-"), "4111 11",
		"ѕ", "а", "ο", "ｓ", "ｋ", "­", "‍", " ", "‑", "\U0001d41a", "ⓒ", "Привет", "日本語", "[REDACTED:secret]", "[REDACTED:api_key]\n",
	}
	runs := 6000
	if v, err := strconv.Atoi(os.Getenv("REDACT_PROPERTY_RUNS")); err == nil && v > 0 {
		runs = v // a longer search when the table changes
	}
	rng := rand.New(rand.NewSource(20261022))
	for i := 0; i < runs; i++ {
		var b strings.Builder
		for j, n := 0, 1+rng.Intn(14); j < n; j++ {
			b.WriteString(fragments[rng.Intn(len(fragments))])
		}
		in := b.String()
		out, n := redactText(in)
		again, m := redactText(out)
		if again != out || m != 0 {
			t.Fatalf("not idempotent (%d then %d replacements):\n in    %q\n out   %q\n again %q", n, m, in, out, again)
		}
		if n == 0 && out != in {
			t.Fatalf("text changed with no replacement counted:\n in  %q\n out %q", in, out)
		}
	}
}

// FuzzRedactText: whatever text it is given, redaction does not panic, changes nothing without counting it, and is a fixed point.
func FuzzRedactText(f *testing.F) {
	for _, group := range [][]secretShape{reviewShapes(), ownShapes()} {
		for _, s := range group {
			f.Add(s.text)
		}
	}
	f.Add("Key \u0455k-" + alnum("fuzz", 30) + "\n" + alnum("fuzz-tail", 12) + "\nDone")
	f.Add("The password is \uff48\uff55\uff4e\uff54\uff45\uff52\uff12 and Basic dXNlcjpwYXNz")
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) { // callers sanitize first, which drops invalid UTF-8
			return
		}
		out, n := redactText(in)
		again, m := redactText(out)
		if again != out || m != 0 {
			t.Fatalf("not idempotent (%d then %d replacements):\n in    %q\n out   %q\n again %q", n, m, in, out, again)
		}
		if n == 0 && out != in {
			t.Fatalf("text changed with no replacement counted:\n in  %q\n out %q", in, out)
		}
	})
}

// Replacing a token can leave its neighbour a token of its own; the second look is part of the first call, so the result is a fixed
// point. This is the first counterexample the property above found while the table was written.
func TestRedactionSettlesToAFixedPoint(t *testing.T) {
	in := "t1AC7TQhiGSpQtrueConfiguration3721-1583-8441-364 3721-1583-8441-364;=true"
	out, n := redactText(in)
	if again, m := redactText(out); again != out || m != 0 {
		t.Fatalf("%q then %q", out, again)
	}
	if n != 2 || strings.Contains(out, "3721") {
		t.Fatalf("%d replacements, %q", n, out)
	}
}
