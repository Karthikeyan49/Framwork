package ai

import (
	"strings"
	"testing"
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
