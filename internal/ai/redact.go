package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math"
	"regexp"
	"slices"
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

// passwordValue is the value of a password argument: quoted (with spaces inside, and with a backslash before a quote as MySQL writes it), or one word of four or more
// characters that is not an option.
const passwordValue = `(?:'(?:[^'\\\n]|\\.){4,200}'|'[^'\n]{4,200}'|"(?:[^"\\\n]|\\.){4,200}"|"[^"\n]{4,200}"|["']?[^\s"';-][^\s"';]{3,}["']?)`

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

	// A .netrc entry with its login: "machine HOST login USER password SECRET" (on one line or several).
	passwordOfANetrcEntry = regexp.MustCompile(`(?i)\bmachine\s+[^\s"']+\s+login\s+[^\s"']+\s+password\s+` + passwordValue)

	// The .netrc forms that a sentence can imitate need a value that looks like a credential (a digit or a symbol, or a quoted one): "default login USER password
	// SECRET" at the start of a line, and a host with a dot and no login. "Each machine web01.example.com password rotates every 30 days" and "Default login admin
	// password rotation is monthly." are sentences (independent review of the follow-up, FU-02).
	passwordOfALooseNetrcEntry = regexp.MustCompile(`(?i)(?:(?m:^)[ \t]*default\s+login\s+[^\s"']+\s+password\s+` +
		`|\bmachine\s+[^\s"'/]+\.[^\s"'/]+\s+password\s+)` + passwordValue)
)

var (
	// The first words of a statement that creates or alters a user or a role (MariaDB also CREATE OR REPLACE USER), or grants rights (MySQL grants a password with
	// IDENTIFIED BY).
	sqlUserHead = regexp.MustCompile(`(?i)\b(?:(?:create(?:\s+or\s+replace)?|alter)\s+(?:user|role|group|login)\b|grant\b)`)
	// The target of a GRANT: ... ON ... TO. Only what follows it can name a password.
	sqlGrantTarget = regexp.MustCompile(`(?is)^grant\b.*?\son\s.*?\sto\s`)
	// IDENTIFIED [WITH plugin] BY value.
	sqlIdentifiedBy = regexp.MustCompile(`(?i)\bidentified\s+(?:with\s+\w+\s+)?by\s+` + passwordValue)
	// The PASSWORD literal of CREATE|ALTER USER|ROLE (PostgreSQL, Redshift, user mappings): quoted, an escape string or dollar quoted.
	sqlPasswordLiteral = regexp.MustCompile(`(?i)\bpassword\s+(?:e?'[^'\n]{4,200}'|"[^"\n]{4,200}"|\$\$[^$\n]{4,200}\$\$)`)
)

// bareSecretOrQuoted decides on a match whose last word is the value: a quoted literal is a password, a bare word is one only when it has a digit or a symbol, as the
// connector of assignment does ("identified by email address" is a way of naming things, "identified by Welcome1" is not).
func bareSecretOrQuoted(match string) bool {
	if last := match[len(match)-1]; last == '\'' || last == '"' {
		return true
	}
	return hasDigitOrSymbol(match[strings.LastIndexAny(match, " \t\r\n")+1:])
}

// hasDigitOrSymbol reports a value that has a digit or a symbol, which a word of a sentence has not.
func hasDigitOrSymbol(v string) bool { return strings.ContainsAny(v, "0123456789@!#$%^&*+/=") }

// sqlStatementEnd finds where the statement whose first words end at s[from] ends: at the first ";" that is not inside a quoted literal (single, double or back
// quotes, a dollar-quoted string), or at the end of s. A backslash inside a quote escapes the next character (MySQL). A literal is read to its closing quote, so a
// clause is never cut, whatever its length and whatever it contains. It reports whether a ";" ended the statement.
func sqlStatementEnd(s string, from int) (int, bool) {
	var quote byte
	for i := from; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == 0:
			switch {
			case c == ';':
				return i, true
			case c == '\'' || c == '"' || c == '`':
				quote = c
			case c == '$' && i+1 < len(s) && s[i+1] == '$':
				quote, i = '$', i+1
			}
		case quote == '$':
			if c == '$' && i+1 < len(s) && s[i+1] == '$' {
				quote, i = 0, i+1
			}
		case c == '\\' && i+1 < len(s):
			i++
		case c == quote:
			quote = 0
		}
	}
	return len(s), false
}

// sqlLiteral reports whether the match of a clause ends with a quoted literal, and its content.
func sqlLiteral(match string) (bool, string) {
	switch {
	case strings.HasSuffix(match, "$$"):
		if i := strings.Index(match, "$$"); i >= 0 && i+2 <= len(match)-2 {
			return true, match[i+2 : len(match)-2]
		}
	case strings.HasSuffix(match, "'"), strings.HasSuffix(match, `"`):
		if i := strings.IndexAny(match, `'"`); i >= 0 && i+1 <= len(match)-1 {
			return true, match[i+1 : len(match)-1]
		}
	}
	return false, ""
}

// clauseEnds reports that the clause that ends at span[to] is the last thing of the statement (end is the length of the statement without its trailing blanks), or is
// followed by the closing quote of a string that holds the statement: psql -c "ALTER USER app PASSWORD 'x'", a JSON value, a line of code, a code span of a document. A
// word of a sentence is followed by another word, a full stop or a line end, none of which closes anything. The first form of the statement scan did not look at what follows
// and so let such a password through when it had no digit and no symbol, which the form before it redacted.
func clauseEnds(span string, to, end int) bool {
	if to >= end {
		return true
	}
	rest := strings.TrimLeft(span[to:], " \t")
	return rest != "" && strings.IndexByte("\"'`", rest[0]) >= 0
}

// sqlCredential decides whether the clause of a statement names a password. ends: the clause is the last thing of the statement or is followed by the closing quote of the
// string that holds it (clauseEnds); terminated: a ";" ended the statement. A quoted literal is one when the statement is SQL by its ending (a ";", or the literal closes it)
// or the literal has a digit or a symbol: in "Create user form with password 'confirm' field" the word is a label. A bare word is one when it has a digit or a symbol, or
// when it ends a statement that a ";" ended (Oracle writes passwords bare: "CREATE USER scott IDENTIFIED BY tiger;").
func sqlCredential(match string, ends, terminated bool) bool {
	if quoted, content := sqlLiteral(match); quoted {
		return ends || terminated || hasDigitOrSymbol(content)
	}
	return hasDigitOrSymbol(match[strings.LastIndexAny(match, " \t\r\n")+1:]) || ends && terminated
}

// redactStatementPasswords redacts the password of a statement that creates, alters or grants a user or a role (CREATE USER ... IDENTIFIED BY '...', ALTER ROLE ... WITH
// CONNECTION LIMIT 5 PASSWORD '...', CREATE OR REPLACE USER, MySQL's GRANT ... TO ... IDENTIFIED BY ..., several users in one statement, a statement of any length). The
// statement runs from its first words to the first ";" that is outside a quoted literal; the words of the statement stay and only the clause with the secret is replaced.
// The same words in a request ("each customer is identified by email address") are not in such a statement, and a quoted word in a sentence is not a password unless it
// has a digit or a symbol (independent review of the takeover fixes TF-01, and of the follow-up, FU-01: the first form of this scan read SQL through closed lists and a
// bound of 1,000 bytes, and let passwords through that the rule before it redacted).
func redactStatementPasswords(s string, n *int) string {
	heads := sqlUserHead.FindAllStringIndex(s, -1)
	if len(heads) == 0 {
		return s
	}
	var b strings.Builder
	last, pos := 0, 0
	for _, h := range heads {
		if h[0] < pos {
			continue // inside the statement of an earlier head: the scan of that statement reads every clause of it
		}
		end, terminated := sqlStatementEnd(s, h[1])
		pos = end
		span := s[h[0]:end]
		if out := redactStatement(span, terminated, n); out != span {
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

// redactStatement replaces the clauses of one statement that name a password. A statement can hold several heads ("grant ... create role ..." without a ";" between them):
// an IDENTIFIED BY clause counts after the first CREATE or ALTER words or after the target of a GRANT, a PASSWORD literal after the first CREATE or ALTER words.
func redactStatement(span string, terminated bool, n *int) string {
	userFrom, grantFrom, grants := -1, -1, 0
	for _, h := range sqlUserHead.FindAllStringIndex(span, -1) {
		switch {
		case strings.EqualFold(span[h[0]:min(h[0]+5, len(span))], "grant"):
			// Only the first GRANT is looked at. Finding the target reads the rest of the statement, and a later GRANT has one only where the first has (its text is a part of the
			// first one's), so a look at each GRANT of a statement of many would cost the square of its length (a probe of 48,000 bytes of "grant " took 11 s).
			if grants++; grants == 1 {
				if loc := sqlGrantTarget.FindStringIndex(span[h[0]:]); loc != nil {
					grantFrom = h[0] + loc[1]
				}
			}
		case userFrom < 0:
			userFrom = h[0]
		}
	}
	end := len(strings.TrimRight(span, " \t\r\n")) // the statement without its trailing blanks
	var edits [][2]int
	for _, loc := range sqlIdentifiedBy.FindAllStringIndex(span, -1) {
		if userFrom >= 0 && loc[0] >= userFrom || grantFrom >= 0 && loc[0] >= grantFrom {
			if sqlCredential(span[loc[0]:loc[1]], clauseEnds(span, loc[1], end), terminated) {
				edits = append(edits, [2]int{loc[0], loc[1]})
			}
		}
	}
	for _, loc := range sqlPasswordLiteral.FindAllStringIndex(span, -1) {
		if userFrom >= 0 && loc[0] >= userFrom && sqlCredential(span[loc[0]:loc[1]], clauseEnds(span, loc[1], end), terminated) {
			edits = append(edits, [2]int{loc[0], loc[1]})
		}
	}
	if len(edits) == 0 {
		return span
	}
	slices.SortFunc(edits, func(a, b [2]int) int { return a[0] - b[0] })
	var out strings.Builder
	at := 0
	for _, e := range edits {
		if e[0] < at {
			continue // inside the clause before it ("IDENTIFIED BY PASSWORD 'hash'")
		}
		out.WriteString(span[at:e[0]])
		out.WriteString(marker("password_arg"))
		at = e[1]
		*n++
	}
	out.WriteString(span[at:])
	return out.String()
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
	{"password_arg", passwordOfALooseNetrcEntry, bareSecretOrQuoted, nil},
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
	`[0-9]{1,4}-(?:bits?|bytes?|chars?|characters?|digits?|letters?|symbols?|words?|hex|hexadecimal|alphanumeric|seconds?|minutes?|hours?|days?|weeks?|months?|years?|times?|uses?|or-more|or-fewer)` +
	`|(?:sha|aes|rsa|md|hmac|des|ecdsa|eddsa|rs|hs|es|ps|bcrypt|scrypt|argon|pbkdf|blake|base|utf|ascii|iso|rfc|fips|pkcs|oauth|oidc|saml|jwt|jws|jwe|totp|hotp|2fa|mfa|sso|tls|ssl|ldap|ntlm|x509|ed25519|ed448|curve25519)` +
	`-?[0-9.]{0,7}(?:-(?:[0-9]{1,5}|sha-?[0-9]{1,3}|based|compliant|signed|encoded|verified|hashed|encrypted|protected|secured|generated|safe|style|formatted|only|bearer))?` +
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
		// A comparison, a short declaration and a hash rocket (password == "...", dbPassword := "...", 'password' => '...') are redacted for a literal that has a
		// digit or a symbol only: a bare word on their right is a name or an expression ("password == confirmPassword", "password => password.length"), and a
		// word in quotes is a state or a label ("if password == "admin" then", "Warn when token == "expired"": independent review of the follow-up, FU-02).
		if (strings.Contains(sep, "==") || strings.Contains(sep, ":=") || strings.Contains(sep, "=>")) && (parts[3][0] != '"' && parts[3][0] != '\'' || !hasDigitOrSymbol(value)) {
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
// issuer prefix and a valid check digit. Every window of a chain of digit groups is tried, so that a card number that follows another
// number ("Order 99871 4111 ..."), is followed by one (the expiry date: "... 1111 12/27") or lies inside a longer run of groups is found
// (PF-09); one regular expression takes the longest match from the left, and when the check refuses that match the card that starts inside
// it is never looked at. The windows that overlap are one span, so no digit of any valid window stays: a window of numbers that passes both
// checks by chance (about one in a hundred, one in forty of 16 digits) is redacted as well, and when it overlaps a real card the card goes
// with it (independent review TF-06: taking the first window found left the other three groups of a card visible). A marker for a number
// is the cheaper error. Windows that only touch are two spans.
func redactCards(s string, n *int) string {
	var out strings.Builder
	last := 0 // s[:last] is in out
	for i := 0; i < len(s); {
		if !digit(s[i]) {
			i++
			continue
		}
		groups, next := digitChain(s, i)
		flush := func(first, end int) { // the span of groups[first..end]
			out.WriteString(s[last:groups[first].start])
			out.WriteString(marker("card"))
			last = groups[end].end
			*n++
		}
		first, end := -1, -1
		for a := range groups {
			b, found := cardWindow(s, groups, a)
			switch {
			case !found:
			case first >= 0 && a <= end:
				end = max(end, b)
			default:
				if first >= 0 {
					flush(first, end)
				}
				first, end = a, b
			}
		}
		if first >= 0 {
			flush(first, end)
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
