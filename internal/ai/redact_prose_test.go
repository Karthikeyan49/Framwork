package ai

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TF-01, TF-04 and TF-09 (independent review of the fixes of 3 November): the rule that redacts passwords given as arguments took words out of
// ordinary requests ("each customer is identified by email address"), the exemption for sentences about credential formats kept
// passphrase-shaped credentials, and the neighbours of the closed forms stayed open. These are the tests of the corrected rules. Every
// secret-shaped value is made at run time from a label.

// lowerWord is n lower-case letters derived from label.
func lowerWord(label string, n int) string {
	b := make([]byte, n)
	for i, c := range material(label, n) {
		b[i] = 'a' + c%26
	}
	return string(b)
}

// Requests, labels and descriptions that use the words of the command and SQL rules and hand over nothing: the rule that takes words
// out of them is wrong, whatever it protects.
func TestRedactionKeepsOrdinaryTextThatUsesTheWordsOfTheCommandRules(t *testing.T) {
	for _, keep := range []string{
		// "identified by" in a model of data
		"Each customer is identified by email address.",
		"A shipment is uniquely identified by tracking number.",
		"Orders identified by number 4471 must be shipped first.",
		"Customer type: individual, identified by passport",
		"Customer, identified by email address",
		"Make customers identified by email address and give them a password field.",
		"Each product is identified by SKU and barcode.",
		"Create a user entity identified by username.",
		"Create user accounts identified by email address.",
		"Alter the role entity, identified by name, to add a description.",
		"Grant access to every employee identified by badge number.",
		"Grant a discount to customers identified by loyalty tier.",
		"Invoices are identified by invoice number 2026-001.",
		"the proposal identified by previous_proposal_hash",
		// "machine" and "password"
		"Define the machine entity with a password field.",
		"The machine learning model validates the password strength of each signup.",
		"Each machine has a login and a password field for the operator console.",
		"Add a machine login password reset flow.",
		"Machine learning password strength checks run at sign-up.",
		"The default login admin password rotation is monthly.",
		"Reset the default login password every 90 days.",
		// the sentences of the second review (FU-02) that the first form of the rules destroyed
		"Create user roles with password 'hint' column.",
		"Create login form password \"hint\" text under the field.",
		"Create login page with password 'forgot' link at the bottom.",
		"Create user form with password 'confirm' field.",
		"Create user account with password \"reset\" button for admins.",
		"Alter user profile with password 'rules' page.",
		"Alter role page with password \"policy\" tab.",
		"Create group chat with password 'optional' setting.",
		"Create role form password \"strength\" indicator.",
		"Create user accounts identified by 'email'.",
		"Create user profiles identified by 'username' and tenant.",
		"Grant the editor role on the blog to authors identified by 'email'.",
		"Grant discounts on orders to customers identified by 'loyalty id'.",
		"Each machine web01.corp.example.com password rotates every 30 days.",
		"Alert when the machine db01.example.com password expires.",
		"Show the machine host.internal password rotation date on the asset page.",
		"The machine srv.local password policy says 14 characters.",
		"Add machine host.example.com password reset to the runbook.",
		"Each machine inventory.local password expiry is shown in red.",
		"Default login admin password rotation is monthly.",
		"Default login admin password reset after first use.",
		"Requirements:\nDefault login user password reset after first use.",
		"Default login user password policy: 12 characters minimum.",
		"Default login admin password expires after 90 days.",
		"Default login guest password optional.",
		"default login admin password reset",
		"if password == \"admin\" then show a warning banner.",
		"Warn when token == \"expired\" in the response.",
		"Reject orders where credentials == \"missing\".",
		"Set password := \"required\" for new users.",
		"The password => \"hidden\" mapping in the form.",
		"Show a password \"hint\" field on the sign-up form.",
		"Label the password 'Passcode' on the form.",
		"Rename the password \"Pass phrase\" column.",
		"Create user roles with a password \"hint\" field.",
		"The password \"strength\" meter is green.",
		// commands and tools that have the same flags with another meaning
		"Use zip codes with -plus4 extension when available.",
		"zip -r backup.zip data/",
		"redis-cli -h cache -p 6379 ping",
		"mysql -h db -P 3306 orders",
		"mysql -h db -P3306 orders",
		"docker login -u bob --password-stdin registry.example.com",
		"sshpass -P assword ssh deploy@host",
		"curl --user-agent \"Mozilla/5.0\" https://api.example.com/v1/orders",
		"mongo --host db --port 27017",
		"gpg --list-keys --with-colons",
		"ldapsearch -x -H ldap://ldap.example.com -b dc=example",
	} {
		if out, n := redactText(keep); out != keep || n != 0 {
			t.Errorf("%q was changed to %q", keep, out)
		}
	}
}

// The command and SQL forms are still redacted where their context is there, in the spellings the review and the red team used and
// the ones next to them.
func TestRedactionOfPasswordsGivenAsArgumentsNeedsTheirContext(t *testing.T) {
	s := alnum("ctx/secret", 12)
	words := lowerWord("ctx/word", 9) + "ing"
	for name, text := range map[string]string{
		"create user identified by, quoted":      "CREATE USER 'bob'@'%' IDENTIFIED BY '" + s + "';",
		"create user identified with ... by":     "CREATE USER bob IDENTIFIED WITH mysql_native_password BY '" + s + "';",
		"create user on a second line":           "CREATE USER 'bob'@'%'\n  IDENTIFIED BY '" + s + "';",
		"create user, a quoted passphrase":       "CREATE USER bob IDENTIFIED BY '" + words + " " + words + "';",
		"create user, the second of two users":   "CREATE USER 'a'@'h' IDENTIFIED BY 'x1y2z3', 'b'@'h' IDENTIFIED BY '" + s + "';",
		"alter user, bare, with digits":          "ALTER USER bob IDENTIFIED BY " + s + ";",
		"alter user if exists":                   "ALTER USER IF EXISTS bob IDENTIFIED BY '" + s + "'",
		"grant, bare":                            "GRANT ALL ON db.* TO bob IDENTIFIED BY " + s + ";",
		"create role with password":              "CREATE ROLE app WITH LOGIN PASSWORD '" + s + "' VALID UNTIL 'infinity';",
		"create user, option words, then pass":   "CREATE USER adminuser CREATEUSER PASSWORD '" + s + "';",
		"alter role, encrypted password":         "ALTER ROLE app WITH ENCRYPTED PASSWORD \"" + s + "\";",
		"netrc, on one line":                     "machine api.example.com login bob password " + s,
		"netrc, on three lines":                  "machine api.example.com\n  login bob\n  password " + s + "\n",
		"netrc, default entry":                   "default login anonymous password " + s,
		"netrc, without a login":                 "machine api.example.com password " + s,
		"curl -u, glued":                         "curl -v -ubob:" + s + " https://api.example.com/v1/orders",
		"sshpass, a quoted password with spaces": "sshpass -p '" + words + " " + words + "' ssh deploy@host",
		"redis-cli --pass":                       "redis-cli -h cache --pass " + s + " ping",
		"mongosh --password=":                    "mongosh mongodb://db/orders --username app --password=" + s,
		"az login":                               "az login -u bob@example.com -p " + s,
		"az login --password":                    "az login --service-principal --password " + s,
		"gpg --passphrase":                       "gpg --batch --passphrase " + s + " --decrypt backup.gpg",
		"ldapsearch -w":                          "ldapsearch -x -D cn=admin,dc=example -w " + s + " -b dc=example",
		"zip --password":                         "zip --password " + s + " backup.zip data/",
	} {
		if out, n := redactText(text); n == 0 || strings.Contains(out, s) || strings.Contains(out, words) {
			t.Errorf("%s: the password survives: %s", name, strings.ReplaceAll(strings.ReplaceAll(out, s, "<SECRET>"), words, "<WORDS>"))
		}
	}
}

// What an SQL statement says around the password stays: the rule takes the secret and the words that introduce it, not the statement.
func TestRedactionOfAStatementKeepsItsName(t *testing.T) {
	s := alnum("ctx/statement", 12)
	out, n := redactText("CREATE USER reporting_ro IDENTIFIED BY '" + s + "'; GRANT SELECT ON orders TO reporting_ro;")
	if n == 0 || strings.Contains(out, s) {
		t.Fatalf("the password survives: %q", out)
	}
	for _, kept := range []string{"CREATE USER reporting_ro", "GRANT SELECT ON orders TO reporting_ro;"} {
		if !strings.Contains(out, kept) {
			t.Errorf("%q was taken out: %q", kept, out)
		}
	}
}

// TF-04: the exemption for a sentence about a credential format is a closed list. A passphrase has the shape the first version let
// through (a word or number, a hyphen, a plain word), and it is redacted in every connector, as are the old table's other examples.
func TestRedactionOfPassphraseShapedValuesInProse(t *testing.T) {
	values := map[string]string{
		"alphanumeric, hyphen, long word":  alnum("tf04/a", 9) + "-" + lowerWord("tf04/a-w", 8) + "ing",
		"alphanumeric, hyphen, short word": alnum("tf04/b", 9) + "-" + lowerWord("tf04/b-w", 5),
		"word, hyphen, year":               strings.ToUpper(lowerWord("tf04/c", 1)) + lowerWord("tf04/c-w", 3) + "-2026",
		"word, hyphen, number":             strings.ToUpper(lowerWord("tf04/d", 1)) + lowerWord("tf04/d-w", 3) + "-" + alnum("tf04/d-n", 8)[:3] + "7",
		"two words and a digit":            lowerWord("tf04/e", 6) + "-" + lowerWord("tf04/e-w", 6) + "7",
		"capital, hyphen, a word":          strings.ToUpper(lowerWord("tf04/f", 1)) + "7" + lowerWord("tf04/f-w", 4) + "-" + lowerWord("tf04/f-x", 6),
	}
	for name, value := range values {
		for _, tpl := range []string{
			"The password is %s.",
			"the admin password is %s and it rotates",
			"The token is %s.",
			"The secret was %s",
			"The credentials are %s",
			"define('DB_PASSWORD', '%s');",
			"define('API_TOKEN', \"%s\");",
		} {
			text := strings.Replace(tpl, "%s", value, 1)
			if out, n := redactText(text); n == 0 || strings.Contains(out, value) {
				t.Errorf("%s: %q keeps the credential: %q", name, tpl, strings.ReplaceAll(out, value, "<VALUE>"))
			}
		}
	}
}

// The sentences about formats that the exemption exists for, and the neighbours of the closed list.
func TestRedactionKeepsMoreSentencesAboutCredentialFormats(t *testing.T) {
	for _, keep := range []string{
		"The token is 32-hex.", "The token is 128-bit.", "The password is 12-char minimum.", "The password is 8-character minimum.",
		"The key is RSA-2048.", "The key is AES-256 encrypted.", "The secret is SHA256 hashed.", "The token is HS256-signed.", "The token is RS256-signed.",
		"The secret is Base-64 encoded.", "The token is RFC-7519 compliant.", "The password is bcrypt-hashed.", "The token is OAuth2-compliant.",
		"The password was 2FA-verified.", "The token is JWT-based and stateless.", "The credentials are SHA-256 hashed.",
		"The token is 15-minute and rotates.", "The password is 90-day and rotates.",
		"The token is x509-style.", "The password is 8-or-more characters.", "The code is 1-time.", "The token is oauth2-bearer style.", "The token is ed25519-signed.",
		"The token is HMAC-SHA256 signed.", "The token is TLS1.3-protected.",
	} {
		if out, n := redactText(keep); out != keep || n != 0 {
			t.Errorf("%q was changed to %q", keep, out)
		}
	}
}

// TF-09: the forms next to the closed ones that the review found open.
func TestRedactionOfAssignmentsWrittenWithOtherOperators(t *testing.T) {
	s := alnum("tf09/op", 12)
	for name, text := range map[string]string{
		"hash rocket, quoted key":  "'password' => '" + s + "',",
		"hash rocket, symbol key":  ":password => \"" + s + "\"",
		"hash rocket, secret":      "'client_secret' => '" + s + "'",
		"go short declaration":     "dbPassword := \"" + s + "\"",
		"go short declaration, tk": "apiToken := '" + s + "'",
		"comparison":               "if password == \"" + s + "\" {",
	} {
		if out, n := redactText(text); n == 0 || strings.Contains(out, s) {
			t.Errorf("%s: the value survives: %s", name, strings.ReplaceAll(out, s, "<SECRET>"))
		}
	}
	// A bare word after these operators is a name or an expression, not a literal; the old operators keep their rule.
	for _, keep := range []string{
		"if password == confirmPassword {",
		"password => password.length > 8",
		"token => token.trim()",
		"x := total",
		"password := promptForPassword()",
		"password == \"\"",
	} {
		if out, n := redactText(keep); out != keep || n != 0 {
			t.Errorf("%q was changed to %q", keep, out)
		}
	}
}

// FU-01 (independent review of the follow-up): the statement scan read SQL through a closed list of option words and a bound of 1,000 bytes, and let through the
// passwords of statements that the rule before it redacted. Whatever stands between the name and the word, the statement is read to its end, and the words of the
// statement stay.
func TestRedactionOfRoleStatementsWhateverStandsBeforeThePassword(t *testing.T) {
	verbs := []string{"CREATE ROLE app", "CREATE USER app", "ALTER ROLE app", "ALTER USER app", "CREATE GROUP app", "CREATE OR REPLACE USER app", `CREATE ROLE "app user"`,
		"create or replace user app", "Alter Role app"}
	options := []string{"", "WITH", "WITH LOGIN", "WITH CONNECTION LIMIT 5", "IN ROLE readers", "VALID UNTIL 'infinity'", "ROLE a", "ADMIN b", "SYSID 100",
		"WITH SUPERUSER CREATEDB NOCREATEROLE", "ENCRYPTED", "LOGIN INHERIT", "WITH REPLICATION BYPASSRLS CONNECTION LIMIT -1 IN GROUP g"}
	count := 0
	for i, verb := range verbs {
		for j, opts := range options {
			s := alnum(fmt.Sprintf("fu01/role/%d/%d", i, j), 12)
			for _, literal := range []string{"'" + s + "'", "E'" + s + "'", "$$" + s + "$$", `"` + s + `"`} {
				for _, tail := range []string{";", "", " ;", " LOGIN;", "\n"} {
					text := strings.TrimSpace(verb+" "+opts) + " PASSWORD " + literal + tail
					out, n := redactText(text)
					count++
					if n == 0 || strings.Contains(out, s) || !strings.HasPrefix(out, strings.Fields(verb)[0]) {
						t.Errorf("%q: the password survives or the statement is damaged: %s", strings.ReplaceAll(text, s, "<S>"), strings.ReplaceAll(out, s, "<S>"))
					}
					if again, m := redactText(out); again != out || m != 0 {
						t.Errorf("%q: not a fixed point: %q", strings.ReplaceAll(text, s, "<S>"), strings.ReplaceAll(again, s, "<S>"))
					}
				}
			}
		}
	}
	if count < 1000 {
		t.Fatalf("only %d statements tried", count)
	}
}

func TestRedactionOfAStatementOfManyUsersAndOfAnyLength(t *testing.T) {
	for _, users := range []int{3, 60, 700} {
		var b strings.Builder
		b.WriteString("CREATE USER ")
		secrets := make([]string, users)
		for i := range secrets {
			secrets[i] = alnum(fmt.Sprintf("fu01/bulk/%d/%d", users, i), 12)
			if i > 0 {
				b.WriteString(",\n  ")
			}
			fmt.Fprintf(&b, "'user%03d'@'%%' IDENTIFIED BY '%s'", i, secrets[i])
		}
		b.WriteString(";\nGRANT SELECT ON orders TO 'user000'@'%';")
		text := b.String()
		out, n := redactText(text)
		for i, s := range secrets {
			if strings.Contains(out, s) {
				t.Fatalf("%d users (%d bytes): the secret of user %d survives", users, len(text), i)
			}
		}
		if n != users {
			t.Errorf("%d users: %d replacements", users, n)
		}
		for _, kept := range []string{"CREATE USER 'user000'@'%' ", "'user" + fmt.Sprintf("%03d", users-1) + "'@'%' [REDACTED:password_arg];", "GRANT SELECT ON orders TO 'user000'@'%';"} {
			if !strings.Contains(out, kept) {
				t.Errorf("%d users: %q was taken out", users, kept)
			}
		}
	}
}

func TestRedactionOfASecretThatHoldsASemicolonOrAQuote(t *testing.T) {
	s := alnum("fu01/semicolon", 12)
	for name, text := range map[string]string{
		"postgres, semicolon":      "CREATE ROLE app LOGIN PASSWORD 'ab;" + s + "';",
		"postgres, dollar quoted":  "CREATE ROLE app LOGIN PASSWORD $$ab;" + s + "$$;",
		"mysql, semicolon":         "CREATE USER bob IDENTIFIED BY 'ab;" + s + "';",
		"mysql, alter":             "ALTER USER bob IDENTIFIED BY 'x;y;" + s + "' PASSWORD EXPIRE;",
		"mysql, escaped quote":     `CREATE USER bob IDENTIFIED BY 'ab\'; ` + s + `';`,
		"grant, semicolon":         "GRANT ALL ON db.* TO bob IDENTIFIED BY 'ab;" + s + "';",
		"two statements, the 2nd":  "CREATE ROLE a LOGIN PASSWORD 'zz1234';\nCREATE ROLE b LOGIN PASSWORD 'ab;" + s + "';",
		"a statement after prose":  "Run this one: CREATE USER bob IDENTIFIED BY 'ab;" + s + "'; then restart.",
		"nothing closes the quote": "CREATE USER bob IDENTIFIED BY '" + s,
	} {
		if out, n := redactText(text); n == 0 || strings.Contains(out, s) {
			t.Errorf("%s: the password survives: %s", name, strings.ReplaceAll(out, s, "<S>"))
		}
	}
}

// A bare word is a password when it has a digit or a symbol, or when it ends a statement that a ";" ended (Oracle writes passwords bare); in a sentence it is a word.
func TestRedactionOfABarePasswordNeedsTheEndOfAStatement(t *testing.T) {
	word := lowerWord("fu01/bare", 7)
	for name, text := range map[string]string{
		"create user":       "CREATE USER scott IDENTIFIED BY " + word + ";",
		"alter user":        "ALTER USER scott IDENTIFIED BY " + word + " ;",
		"grant":             "GRANT ALL ON db.* TO scott IDENTIFIED BY " + word + ";",
		"with a digit":      "CREATE USER scott IDENTIFIED BY " + word + "7 and more",
		"mariadb":           "CREATE OR REPLACE USER scott IDENTIFIED BY " + word + ";",
		"after a statement": "CREATE USER a IDENTIFIED BY 'xyz123';\nALTER USER scott IDENTIFIED BY " + word + ";",
	} {
		if out, n := redactText(text); n == 0 || strings.Contains(out, word) {
			t.Errorf("%s: the password survives: %s", name, strings.ReplaceAll(out, word, "<WORD>"))
		}
	}
	for _, keep := range []string{
		"Create user accounts identified by email address.",
		"Create user accounts identified by " + word + " and more words.",
		"CREATE USER scott IDENTIFIED BY RANDOM PASSWORD;",
	} {
		if out, n := redactText(keep); out != keep || n != 0 {
			t.Errorf("%q was changed to %q", keep, out)
		}
	}
}

// Each clause of a statement is a password only where it can be one: IDENTIFIED BY after CREATE or ALTER USER or after the target of a GRANT, a PASSWORD literal after
// CREATE or ALTER USER or ROLE. The same words before that place, or in a sentence that has no such statement, are words (a mutation that counted them from the first
// word of the statement survived the first version of these tests).
func TestRedactionCountsAClauseOnlyWhereItIsAPassword(t *testing.T) {
	s := alnum("fu01/clauses", 12) + "7"
	for name, text := range map[string]string{
		"identified by after the target of a grant": "Grant read on orders to analysts identified by '" + s + "' for the quarter.",
		"identified by in the next statement":       "Grant read on orders to analysts; create user bob identified by '" + s + "' for the quarter.",
		"password after create user":                "Create user bob with password '" + s + "' for the quarter.",
		"password after alter role":                 "Alter role bob with connection limit 5 password '" + s + "' for the quarter.",
	} {
		if out, n := redactText(text); n == 0 || strings.Contains(out, s) {
			t.Errorf("%s: the password survives: %s", name, strings.ReplaceAll(out, s, "<S>"))
		}
	}
	// A statement that has both clauses, the PASSWORD literal before the IDENTIFIED BY clause: the edits are made in the order of the text.
	s2 := alnum("fu01/clauses/2", 12) + "8"
	mixed := "CREATE USER a WITH PASSWORD '" + s + "', b IDENTIFIED BY '" + s2 + "';"
	if out, n := redactText(mixed); n != 2 || strings.Contains(out, s) || strings.Contains(out, s2) {
		t.Errorf("a statement that mixes the two clauses: %d replacements: %s", n, strings.NewReplacer(s, "<S>", s2, "<S2>").Replace(out))
	}
	// Two heads in one statement, with no ";" between them: the scan of the first reads the clauses of the second, and the second is not scanned again.
	twoHeads := "GRANT SELECT ON t TO a IDENTIFIED BY '" + s + "' CREATE ROLE b LOGIN PASSWORD '" + s2 + "'"
	if out, n := redactText(twoHeads); n != 2 || strings.Contains(out, s) || strings.Contains(out, s2) {
		t.Errorf("two heads in one statement: %d replacements: %s", n, strings.NewReplacer(s, "<S>", s2, "<S2>").Replace(out))
	}
	for _, keep := range []string{
		"Grant managers identified by 'approver1' on orders the right to approve and to reject.",
		"Grant read access on orders to analysts, then set the field password 'label1' to bold.",
		"Grant the role to users identified by 'guest1' on the orders page.",
		"Alter the screen so that users identified by 'guest1' see less.",
		"Create the users page so that a user identified by 'guest1' sees it.",
	} {
		if out, n := redactText(keep); out != keep || n != 0 {
			t.Errorf("%q was changed to %q", keep, out)
		}
	}
}

// A quoted password of letters only (no digit, no symbol) is a password when the statement is SQL by its ending: a ";" ended it, or the literal closes it. In a sentence
// it is a label ("Create user form with password 'confirm' field").
func TestRedactionOfAQuotedPasswordOfLettersNeedsTheEndOfAStatement(t *testing.T) {
	word := lowerWord("fu01/letters", 9)
	for name, text := range map[string]string{
		"a clause after it, a semicolon ends the statement": "CREATE USER bob IDENTIFIED BY '" + word + "' PASSWORD EXPIRE;",
		"postgres, more options after it":                   "ALTER ROLE bob WITH PASSWORD '" + word + "' VALID UNTIL 'infinity';",
		"the literal closes the statement":                  "CREATE ROLE bob LOGIN PASSWORD '" + word + "'",
		"the literal closes it, blanks after":               "CREATE ROLE bob LOGIN PASSWORD '" + word + "'  \n",
		// A string that holds the statement closes after the literal: the statement is SQL, whatever the end of the text is (the form before the follow-up to FU-01 redacted these).
		"a shell string":                  `psql -c "ALTER USER postgres PASSWORD '` + word + `'"`,
		"a shell string, mysql":           `mysql -e "CREATE USER 'app'@'%' IDENTIFIED BY '` + word + `'" && restart`,
		"a shell string in single quotes": `mysql -e 'CREATE USER app IDENTIFIED BY "` + word + `"' && restart`,
		"a value of JSON":                 `{"query": "CREATE ROLE app LOGIN PASSWORD '` + word + `'", "wait": true}`,
		"a list of arguments":             `command: ["psql", "-c", "ALTER USER app PASSWORD '` + word + `'"]`,
		"a call of code":                  `cur.execute("ALTER ROLE app PASSWORD '` + word + `'")`,
		"a code span of a document":       "Run `CREATE USER app IDENTIFIED BY '" + word + "'` first, then restart.",
	} {
		if out, n := redactText(text); n == 0 || strings.Contains(out, word) {
			t.Errorf("%s: the password survives: %s", name, strings.ReplaceAll(out, word, "<WORD>"))
		}
	}
	for _, keep := range []string{
		"Create user form with password '" + word + "' field",
		"Create user bob with password '" + word + "' for the quarter.",
	} {
		if out, n := redactText(keep); out != keep || n != 0 {
			t.Errorf("%q was changed to %q", keep, out)
		}
	}
}

// One look at a statement redacts all of its clauses, in the order of the text, whichever kind comes first (the table is applied again until nothing is left, but a single
// scan must not depend on that).
func TestRedactStatementMakesItsEditsInTheOrderOfTheText(t *testing.T) {
	s, s2 := alnum("fu01/order", 12)+"7", alnum("fu01/order/2", 12)+"8"
	for name, span := range map[string]string{
		"password first":      "CREATE USER a WITH PASSWORD '" + s + "', b IDENTIFIED BY '" + s2 + "'",
		"identified by first": "CREATE USER a IDENTIFIED BY '" + s + "', b WITH PASSWORD '" + s2 + "'",
	} {
		var n int
		if out := redactStatement(span, true, &n); n != 2 || strings.Contains(out, s) || strings.Contains(out, s2) {
			t.Errorf("%s: %d replacements: %s", name, n, strings.NewReplacer(s, "<S>", s2, "<S2>").Replace(out))
		}
	}
}

// The statement scan costs time in proportion to the text, whatever the statement is made of. A probe of "grant " repeated 8,000 times (48,000 bytes) took 11 s while the scan
// looked at the target of every GRANT of a statement, and the longer shapes took more than 20 s: 200 times the cost of ordinary prose of the same size, where the linear scan
// costs about twice as much. The limit is 50 times the cost of prose measured in the same run, so it holds with the race detector on (the unit gate runs with it) and on a
// busy host, and is far from both.
func TestStatementScanIsLinear(t *testing.T) {
	timeOf := func(text string) time.Duration {
		start := time.Now()
		redactText(text)
		return time.Since(start)
	}
	baseline := max(timeOf(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 1100)), 20*time.Millisecond)
	for _, c := range []struct {
		name string
		text string
	}{
		{"grant", strings.Repeat("grant ", 8000)},
		{"grant a on b", strings.Repeat("grant a on b ", 4000)},
		{"heads of every kind", strings.Repeat("create role a grant on b create user c grant ", 1500)},
		{"many short statements", strings.Repeat("create user a; ", 4000)},
	} {
		if took := timeOf(c.text); took > 50*baseline {
			t.Fatalf("%s: %d bytes took %s, prose of 49,500 bytes took %s", c.name, len(c.text), took, baseline)
		}
	}
}

func TestSQLStatementEnd(t *testing.T) {
	for _, c := range []struct {
		text       string
		from       int
		end        int
		terminated bool
	}{
		{"abc;def", 0, 3, true},
		{"abc", 0, 3, false},
		{"a 'x;y' b; c", 0, 9, true},
		{`a "x;y" b; c`, 0, 9, true},
		{"a `x;y` b; c", 0, 9, true},
		{"a $$x;y$$ b; c", 0, 11, true},
		{`a 'x\'; y' b; c`, 0, 12, true},
		{"a 'x''y;z' b; c", 0, 12, true},
		{"a 'unclosed; b", 0, 14, false},
		{"; leading", 0, 0, true},
		{"abc;def", 4, 7, false},
		{"", 0, 0, false},
	} {
		if end, terminated := sqlStatementEnd(c.text, c.from); end != c.end || terminated != c.terminated {
			t.Errorf("sqlStatementEnd(%q, %d) = %d, %v; want %d, %v", c.text, c.from, end, terminated, c.end, c.terminated)
		}
	}
}
