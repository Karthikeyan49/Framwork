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
	// scan, when set, replaces re and ok: a rule whose secrets a single left-to-right match cannot find (the payment card).
	scan func(s string, n *int) string
}

func (r redactRule) apply(s string, n *int) string {
	if r.scan != nil {
		return r.scan(s, n)
	}
	return r.re.ReplaceAllStringFunc(s, func(m string) string {
		if r.ok != nil && !r.ok(m) {
			return m
		}
		*n++
		return marker(r.class)
	})
}

// passwordValue is the value of a password argument: quoted (with spaces inside), or one word of four or more characters that is not an option.
const passwordValue = `(?:'[^'\n]{4,200}'|"[^"\n]{4,200}"|["']?[^\s"';-][^\s"';]{3,}["']?)`

var (
	// The flag of each command is written for that command: curl -u/--user USER:PASSWORD, sshpass -p, redis-cli -a/--pass (-p is its port), the password flag of
	// mongo, mongosh, docker login and az login, zip -P, wget, mysql -pPASSWORD (glued; -P is its port), gpg --passphrase, ldapsearch -w. Flags with a capital
	// are case sensitive, the words around them are not. --password-stdin is not a password.
	passwordOfACommand = regexp.MustCompile(`(?i)(?:\bcurl\b[^\n]*?\s(?:-u|--user)\s*["']?[^\s:"']+:` +
		`|\bsshpass\b[^\n]*?\s(?-i:-p)\s*` +
		`|\bredis-cli\b[^\n]*?\s(?:(?-i:-a)\s*|--pass\s+)` +
		`|\b(?:mongo|mongosh|docker\s+login|az\s+login)\b[^\n]*?\s(?:(?-i:-p)\s*|--password(?:\s+|=))` +
		`|\bzip\b[^\n]*?\s(?:(?-i:-P)\s*|--password\s+)` +
		`|\bwget\b[^\n]*?--(?:http-|ftp-)?password[\s=]+` +
		`|\bmysql(?:dump|admin)?\b[^\n]*?\s(?-i:-p)` +
		`|\bgpg2?\b[^\n]*?\s--passphrase(?:\s+|=)` +
		`|\bldap(?:search|modify|add|delete|passwd|compare)\b[^\n]*?\s(?-i:-w)\s*)` + passwordValue)

	// A .netrc entry: "machine HOST login USER password SECRET" (on one line or several), "default login ... password ...", or a host with a dot and no login.
	passwordOfANetrcEntry = regexp.MustCompile(`(?i)\b(?:(?:machine\s+[^\s"']+|default)\s+login\s+[^\s"']+\s+password\s+` +
		`|machine\s+[^\s"'/]+\.[^\s"'/]+\s+password\s+)` + passwordValue)
)

// sqlStatementSpan is how far past its first words a statement that names a user or a role is read for a password.
const sqlStatementSpan = 1000

var (
	// The first words of a statement that creates or alters a user or a role, or grants rights (MySQL grants a password with IDENTIFIED BY).
	sqlUserHead = regexp.MustCompile(`(?i)\b(?:(?:create|alter)\s+(?:user|role|group|login)\b|grant\b)`)
	// The target of a GRANT: ... ON ... TO. Only what follows it can name a password.
	sqlGrantTarget = regexp.MustCompile(`(?is)^grant\b.*?\son\s.*?\sto\s`)
	// IDENTIFIED [WITH plugin] BY value.
	sqlIdentifiedBy = regexp.MustCompile(`(?i)\bidentified\s+(?:with\s+\w+\s+)?by\s+` + passwordValue)
	// The quoted PASSWORD of PostgreSQL's CREATE|ALTER ROLE: only the option words of the statement may stand between the name and the word, so that
	// "Create user roles with a password \"hint\" field" is not one. The first group is kept.
	sqlRolePassword = regexp.MustCompile(`(?i)^((?:create|alter)\s+(?:user|role|group|login)\s+[^\s;]+(?:\s+(?:with|login|nologin|(?:un)?encrypted|(?:no)?(?:superuser|createdb|createrole|createuser|inherit|replication|bypassrls)))*\s+)` +
		`(password\s+(?:'[^'\n]{4,200}'|"[^"\n]{4,200}"))`)
)

// bareSecretOrQuoted decides on an IDENTIFIED BY clause: a quoted literal is a password, a bare word is one only when it has a digit or a symbol, as the
// connector of assignment does ("identified by email address" is a way of naming things, "identified by Welcome1" is not).
func bareSecretOrQuoted(match string) bool {
	if last := match[len(match)-1]; last == '\'' || last == '"' {
		return true
	}
	return strings.ContainsAny(match[strings.LastIndexAny(match, " \t\r\n")+1:], "0123456789@!#$%^&*+/=")
}

// redactStatementPasswords redacts the password of a statement that creates, alters or grants a user or a role (CREATE USER ... IDENTIFIED BY '...',
// ALTER ROLE ... PASSWORD '...', MySQL's GRANT ... TO ... IDENTIFIED BY ..., several users in one statement). The statement runs from its first words to
// the ";" or the next such words, at most sqlStatementSpan bytes; the words of the statement stay, only the clause with the secret is replaced. The same
// words in a request ("each customer is identified by email address") are not in such a statement and are not touched (TF-01).
func redactStatementPasswords(s string, n *int) string {
	heads := sqlUserHead.FindAllStringIndex(s, -1)
	if len(heads) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for i, h := range heads {
		end := min(len(s), h[1]+sqlStatementSpan)
		if i+1 < len(heads) && heads[i+1][0] < end {
			end = heads[i+1][0]
		}
		if j := strings.IndexByte(s[h[1]:end], ';'); j >= 0 {
			end = h[1] + j
		}
		for end > h[1] && end < len(s) && !utf8.RuneStart(s[end]) {
			end--
		}
		span, from := s[h[0]:end], h[1]-h[0]
		if loc := sqlGrantTarget.FindStringIndex(span); strings.EqualFold(span[:min(5, len(span))], "grant") {
			if loc == nil {
				continue
			}
			from = loc[1]
		}
		out := span[:from] + sqlIdentifiedBy.ReplaceAllStringFunc(span[from:], func(m string) string {
			if !bareSecretOrQuoted(m) {
				return m
			}
			*n++
			return marker("password_arg")
		})
		if sub := sqlRolePassword.FindStringSubmatchIndex(out); sub != nil {
			*n++
			out = out[:sub[4]] + marker("password_arg") + out[sub[5]:]
		}
		if out != span {
			b.WriteString(s[last:h[0]])
			b.WriteString(out)
			last = end
		}
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

var redactRules = []redactRule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?(?:-----END [A-Z0-9 ]+-----|$)`), nil, nil},
	{"url_credentials", regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.\-]*://[^\s/?#@"'<>\\]*@[^\s"'<>\\]+`), nil, nil},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`), nil, nil},
	{"api_key", regexp.MustCompile(`\b(?:(?:sk|rk|pk)-[A-Za-z0-9_-]{16,}|(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{10,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|xapp-[0-9]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{30,}|ya29\.[0-9A-Za-z_-]{20,}|glpat-[A-Za-z0-9_-]{16,}|npm_[A-Za-z0-9]{30,}|SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}|hf_[A-Za-z0-9]{30,}|api_org_[A-Za-z0-9]{30,}|whsec_[A-Za-z0-9]{16,}|GOCSPX-[A-Za-z0-9_-]{24,}|dckr_pat_[A-Za-z0-9_-]{20,}|shp(?:at|ca|pa|ss)_[A-Fa-f0-9]{32}|hvs\.[A-Za-z0-9_-]{20,}|gl(?:rt|dt|ptt|ft|imt|oas|soat|cbt)-[A-Za-z0-9_-]{16,}|sq0(?:atp|csp)-[A-Za-z0-9_-]{22,}|NRAK-[A-Z0-9]{27}|\d{8,10}:A[A-Za-z0-9_-]{34}|[MNO][A-Za-z0-9_-]{23,25}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,38}|[A-Za-z0-9_.-]{3}\dQ~[A-Za-z0-9_.~-]{31,34})`), nil, nil},
	{"aws_key_id", regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA|ANVA|AIPA|ABIA|ACCA)[0-9A-Z]{16}\b`), nil, nil},
	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`), nil, nil},
	{"authorization", regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer\s+|basic\s+|token\s+)?[^\s,;"']{8,}`), nil, nil},
	// The signature of an Azure shared access signature (a query value, URL-encoded base64) and of any other "sig=" link.
	{"sas_signature", regexp.MustCompile(`(?i)\b(?:sig|signature)=[A-Za-z0-9%+/_-]{16,}={0,3}`), nil, nil},
	// "Basic <token>" without the header name around it; only a token that decodes to "user:password" is a credential, so "basic
	// information" and "basic requirements" stay.
	{"basic_auth", regexp.MustCompile(`(?i)\bbasic\s+[A-Za-z0-9+/_-]{6,}={0,2}`), basicCredentials, nil},
	// A Cookie or Set-Cookie header whose line holds a pair with a value of eight characters or more: a session, not a preference.
	{"cookie", regexp.MustCompile(`(?i)\b(?:set-)?cookie[ \t]*:[^\r\n]*?[A-Za-z0-9_.%-]+=[^\s;]{8,}[^\r\n]*`), nil, nil},
	// A payment card number: 13 to 19 digits, alone or in groups, with a known issuer prefix and a valid check digit (redactCards).
	{"card", nil, nil, redactCards},
	// A password given as an argument of a command, or as the literal of a statement, not as NAME=value: curl -u user:SECRET, mysql -pSECRET,
	// sshpass -p SECRET, docker login -p SECRET, IDENTIFIED BY 'SECRET', a .netrc "password SECRET", PostgreSQL PASSWORD 'SECRET'. Every form
	// needs the command or the statement around it, because the same words are ordinary prose ("each customer is identified by email address",
	// "a password hint field", "a machine learning model", "zip codes"); a rule that took them out would blind the model to a request
	// (independent review of the takeover fixes, TF-01). The flag of each command is its own: -p is the port of redis-cli and -P that of mysql.
	{"password_arg", passwordOfACommand, nil, nil},
	{"password_arg", passwordOfANetrcEntry, nil, nil},
	{"password_arg", nil, nil, redactStatementPasswords},
}

// assignment finds "name = value" pairs with a secret-like name, also at the end of a longer variable name (DB_PASSWORD, PGPASSWORD,
// dbPassword, STRIPE_SECRET_KEY, SECRET_KEY_BASE) and also when the name is quoted or subscripted ("password": "...",
// os.environ["DB_PASSWORD"] = "...", define('DB_PASSWORD', '...')). The "=" form is redacted for any value of four or more
// characters; the ":" form (ordinary prose such as "Password: required") only when the value looks like a credential; "name is
// value" (the admin password is ...) and "name', 'value" (a call that passes the name and the value) only when the value has a digit or a
// symbol and so is not a word like "required" (independent red team RT-01, RT-02).
var assignment = regexp.MustCompile(`(?i)\b[a-z0-9_-]*(api[_-]?key|apikey|secret(?:[_-]?(?:access[_-]?)?key)?(?:[_-]base)?|client[_-]?secret|access[_-]?token|auth[_-]?token|refresh[_-]?token|token|password|passwd|pwd|passphrase|private[_-]?key|account[_-]?key|(?:shared[_-]?)?access[_-]?key|credentials?)\b(["'\x60\]]*\s*(?::=|=>|==|[:=])\s*|["']\s*,\s*|\s+(?:is|was|are)\s+)("[^"\n]{1,200}"|'[^'\n]{1,200}'|[^\s,;&"']{1,200})`)

var (
	longHex    = regexp.MustCompile(`\b[0-9A-Fa-f]{32,}\b`)
	longBase64 = regexp.MustCompile(`[A-Za-z0-9+/_-]{40,}={0,2}`)
)

// proseSpecification matches a value that says what a credential is rather than being one: a size with its unit ("64-character", "256-bit",
// "15-minute") or a named algorithm, standard or protocol, alone or with its size or a plain participle ("SHA-256", "AES256", "RSA-2048",
// "HS256-signed", "OAuth2-compliant", "2FA-verified", "RFC-7519"). "The token is 64-character hex." keeps its subject (independent red team
// RT-05). The lists are closed on purpose: the first version let through any word, a hyphen and a lower-case word, which is also the shape of a
// passphrase ("the password is <word>-<word>" was kept, independent review TF-04). A secret that is spelled like one of these stays kept (a value
// that is only an algorithm name is not worth hiding); any other value that has a digit or a symbol is redacted.
var proseSpecification = regexp.MustCompile(`(?i)^(?:` +
	`[0-9]{1,4}-(?:bits?|bytes?|chars?|characters?|digits?|letters?|symbols?|words?|hex|hexadecimal|alphanumeric|seconds?|minutes?|hours?|days?|weeks?|months?|years?)` +
	`|(?:sha|aes|rsa|md|hmac|des|ecdsa|eddsa|rs|hs|es|ps|bcrypt|scrypt|argon|pbkdf|blake|base|utf|ascii|iso|rfc|fips|pkcs|oauth|oidc|saml|jwt|jws|jwe|totp|hotp|2fa|mfa|sso|tls|ssl|ldap|ntlm)` +
	`-?[0-9]{0,5}(?:-(?:[0-9]{1,5}|based|compliant|signed|encoded|verified|hashed|encrypted|protected|secured|generated|safe|style|formatted|only))?` +
	`)$`)

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
		// A comparison, a short declaration and a hash rocket (password == "...", dbPassword := "...", 'password' => '...') are redacted for a literal
		// only: a bare word on their right is a name or an expression ("password == confirmPassword", "password => password.length").
		if (strings.Contains(sep, "==") || strings.Contains(sep, ":=") || strings.Contains(sep, "=>")) && parts[3][0] != '"' && parts[3][0] != '\'' {
			return m
		}
	case strings.Contains(sep, ":"):
		if len(value) < 8 || !strings.ContainsAny(value, "0123456789-_/+=.@!#$%^&*") {
			return m
		}
	default:
		// Prose: the end of the sentence is not part of the value, and "." and "-" are as common in words as in passwords.
		trailing := value[len(strings.TrimRight(value, ".,;:!?)]}")):]
		if value = value[:len(value)-len(trailing)]; len(value) < 6 || !strings.ContainsAny(value, "0123456789@!#$%^&*+/=") || proseSpecification.MatchString(value) {
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

// digitGroup is a run of digits of the text, as byte offsets (the end is exclusive).
type digitGroup struct{ start, end int }

func digit(c byte) bool { return c >= '0' && c <= '9' }

// wordByte is the \w of a regular expression: a word boundary lies between it and any other byte.
func wordByte(c byte) bool {
	return digit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
}

// digitChain reads the groups of digits that begin at s[i], a digit: each group is followed by one space or hyphen and a digit of the
// next group, or the chain ends. It returns the groups and the offset after the last one.
func digitChain(s string, i int) ([]digitGroup, int) {
	var groups []digitGroup
	for {
		j := i
		for j < len(s) && digit(s[j]) {
			j++
		}
		groups = append(groups, digitGroup{i, j})
		if j+1 < len(s) && (s[j] == ' ' || s[j] == '-') && digit(s[j+1]) {
			i = j + 1
			continue
		}
		return groups, j
	}
}

// cardWindow looks for the payment card number that starts with groups[a]: the longest window of 13 to 19 digits made of whole groups,
// with a word boundary before and after it, that has the prefix and the length of a card network and a valid check digit. It returns the
// index of the last group of the window.
func cardWindow(s string, groups []digitGroup, a int) (int, bool) {
	if a == 0 && groups[0].start > 0 && wordByte(s[groups[0].start-1]) {
		return 0, false // a letter or an underscore right before the first group: no word boundary
	}
	var ends [8]int
	count, total := 0, 0
	for b := a; b < len(groups); b++ {
		if total += groups[b].end - groups[b].start; total > 19 {
			break
		}
		if total < 13 {
			continue
		}
		if b == len(groups)-1 && groups[b].end < len(s) && wordByte(s[groups[b].end]) {
			continue // a letter or an underscore right after the last group: no word boundary
		}
		ends[count] = b
		count++
	}
	var buf [19]int
	for ; count > 0; count-- {
		b := ends[count-1]
		digits := buf[:0]
		for _, g := range groups[a : b+1] {
			for k := g.start; k < g.end; k++ {
				digits = append(digits, int(s[k]-'0'))
			}
		}
		if cardIssuer(digits) && luhn(digits) {
			return b, true
		}
	}
	return 0, false
}

// redactCards redacts the payment card numbers of s: 13 to 19 digits, alone or in groups separated by one space or hyphen, with a known
// issuer prefix and a valid check digit. Every window of a chain of digit groups is tried, the longest first, so that a card number that
// follows another number ("Order 99871 4111 ..."), is followed by one (the expiry date: "... 1111 12/27") or lies inside a longer run of
// groups is found (PF-09); one regular expression takes the longest match from the left, and when the check refuses that match the card
// that starts inside it is never looked at. A window of numbers that passes both checks by chance is redacted as well (about one window
// in two hundred): a marker for a number is the cheaper error.
func redactCards(s string, n *int) string {
	var out strings.Builder
	last := 0 // s[:last] is in out
	for i := 0; i < len(s); {
		if !digit(s[i]) {
			i++
			continue
		}
		groups, next := digitChain(s, i)
		for a := 0; a < len(groups); {
			b, found := cardWindow(s, groups, a)
			if !found {
				a++
				continue
			}
			out.WriteString(s[last:groups[a].start])
			out.WriteString(marker("card"))
			last = groups[b].end
			*n++
			a = b + 1
		}
		i = next
	}
	if last == 0 {
		return s
	}
	out.WriteString(s[last:])
	return out.String()
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
	return redactWrappedTails(redactSplitKeys(s, &n), &n), n
}

// keyPart reports whether w is made of the characters of a key and is not empty.
func keyPart(w string) bool {
	return w != "" && strings.IndexFunc(w, func(c rune) bool {
		return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.ContainsRune("+/_=-.", c))
	}) < 0
}

// wholeToken returns the class of w when all of it is one secret: a prefix rule, a hex run or, for a wrapped paragraph, a long entropic run.
func wholeToken(w string, wrapped bool) (string, bool) {
	var n int
	out := w
	for _, r := range redactRules {
		out = r.apply(out, &n)
	}
	if class, ok := strings.CutPrefix(out, redactionPrefix); ok && n > 0 && strings.HasSuffix(class, "]") && !strings.ContainsAny(class[:len(class)-1], "[]") {
		return class[:len(class)-1], true
	}
	switch {
	case longHex.FindString(w) == w:
		return "hex", true
	case wrapped && longBase64.FindString(w) == w && base64Replace(w) != w:
		return "token", true
	}
	return "", false
}

// redactSplitKeys redacts a key that a line break cut before its first part was long enough for its rule (independent red team RT-03):
// the last word of a line plus the next lines that hold one key-shaped word each (three at most) is read as one word. A run without a
// prefix needs a first line of 48 or more columns, a wrapped paragraph. It runs before redactWrappedTails, so a longer wrap is continued
// by that rule.
func redactSplitKeys(s string, n *int) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	lines := strings.Split(s, "\n")
	changed := false
	for i := 0; i+1 < len(lines); i++ {
		head := strings.TrimRight(lines[i], " \t\r")
		cut := strings.LastIndexAny(head, " \t") + 1
		if len(head)-cut < 2 || !keyPart(head[cut:]) {
			continue
		}
		joined := head[cut:]
		for j := i + 1; j < len(lines) && j <= i+3; j++ {
			tail := strings.TrimSpace(lines[j])
			if !keyPart(tail) {
				break
			}
			if joined += tail; len(joined) < 19 {
				continue
			}
			if class, ok := wholeToken(joined, len(head) >= 48); ok {
				lines[i] = head[:cut] + marker(class)
				for k := i + 1; k <= j; k++ {
					lines[k] = lines[k][:len(lines[k])-len(strings.TrimLeft(lines[k], " \t"))] + marker(class)
				}
				*n += j - i + 1
				changed, i = true, j
				break
			}
		}
	}
	if !changed {
		return s
	}
	return strings.Join(lines, "\n")
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
