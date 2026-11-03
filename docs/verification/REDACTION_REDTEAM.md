# Independent red team of the prompt redactor (`internal/ai/redact*.go`)

Branch `review/redaction-redteam`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`). Scope: `internal/ai/redact.go`, `redact_fold.go`,
every field that reaches a prompt (`context.go`, `repair.go`, `template.go`) and every path that carries author text to a model, a stored row, an audit row, a log
or a response. Written by an agent that did not write the redactor and changed no product code, test or ADR. Probes and mutations lived only in the worktree and were
reverted or deleted before each commit. Every secret-shaped input was generated at run time; this file names shapes (class, prefix, length, alphabet), never a value.
Verdicts are PASS / FAIL / NOT RUN only.

## Progress (kept current)

| state | detail |
|---|---|
| done | brief and code read; Part A matrix (about 3,000 cells x 6 samples); RT-01, RT-02 and RT-03 minimised, patched, verified; Part C integration probes on the `fix` stack; false-positive corpus (389 inputs); timing at 4 KB to 1 MiB; 5 `holdsRedaction` bypass variants |
| interrupted | the run was cut by the account limit once and resumed; nothing was lost (the report and the branch are the record) |
| done (later) | Part A matrix section, Part B false-positive and timing tables, Part C path table with two integration probes |
| done (later) | findings RT-04 to RT-09, appendices A and B (generators, repro tests, four verified patches) |
| done (final) | 90 mutations, 5-minute fuzz of the pinned code under `hv` (402,055 executions, PASS), obfuscation tier, verdict |


## Verdict

**FAIL for the claim that the redactor catches the accidental pastes of common credentials: 5 MEDIUM (RT-01, RT-02, RT-03, RT-04, RT-08) and 4 LOW (RT-05, RT-06, RT-07, RT-09) findings, no HIGH.
Each MEDIUM has a repro generated at run time and a patch of at most 80 diff lines that was verified in a scratch copy of the pinned tree (shipped tests, false-positive corpus of 389 inputs, Part A matrix).**
**PASS for Part C: no path carries author text to a provider, a stored row, an audit row, a log line or a response without the redactor, except the model's own answer, which the code and the corpus
state is not redacted.** Timing: PASS (no accepted input over 0.4 s, all growth linear). The 25 mutation checks of the ADR are not re-run; my own 90 are in the mutation section.

What held (executed attacks): the 60 prefix-rule classes in 24 neutral carriers (8,640 samples, 0 failures); the fold against fullwidth, circled, mathematical and Cyrillic look-alikes and soft-hyphen/joiner
insertions (shipped cases re-run, plus 4 own look-alike carriers inside RT corpus forms); `holdsRedaction` against `[`/`:` escapes in typed strings and raw messages; tail rule against prose and
the typed-marker variants; 389 realistic inputs with no change in the requirement, role, code, markdown, non-English and ticket categories; 42 adversarial inputs per size from 4 KB to 1 MiB; 100,000-string and
9,000-deep JSON trees; invalid UTF-8, NUL, lone surrogates; determinism and idempotence under fuzz (27,776 and 35,690 executions, below).

Not covered: see the last section.
## Findings

| id | severity | one line |
|---|---|---|
| RT-01 | MEDIUM | a quoted key defeats the `name = value` rule: `"password": "..."`, `'password': '...'`, `os.environ["DB_PASSWORD"] = "..."`, `define('DB_PASSWORD', '...')` survive in full |
| RT-02 | MEDIUM | compound names without `_` or `-` are not names: `PGPASSWORD=...`, `dbPassword=...`, `apiSecret=...`, `postgresPassword: ...`, `SECRET_KEY_BASE=...` survive in full |
| RT-03 | MEDIUM | a key hard-wrapped so that its first line is shorter than the minimum of its rule leaves both halves: 44 of 69 wrapped keys keep 12 or more consecutive characters at each of the widths 60, 64, 72, 76 |
| RT-04 | MEDIUM | twelve well-known vendor token formats have no rule and survive in every carrier: `GOCSPX-`, `dckr_pat_`, `shpat_`/`shpss_`, `hvs.`, `glrt-`/`gldt-`, `sq0atp-`, `NRAK-`, Telegram and Discord bot tokens, Azure AD client secrets |
| RT-05 | LOW | the "name is value" prose rule destroys sentences about token and password formats (`The token is 64-character hex.`): 7 of 47 connector sentences, 1.8 % of the corpus |
| RT-06 | LOW | a string that is entirely identifier-shaped skips the generic rules, so a 40+ character lower-case token typed alone (a request, a label, an enum value, a field id) reaches the provider |
| RT-07 | LOW | hex runs of 32 to 128 characters survive when glued to `0x`, `_` or a letter (`0x` + 64 hex, `key_` + 32 hex): the `\b` fails and hex entropy never reaches 4.0 |
| RT-08 | MEDIUM | command-line, SQL and `.netrc` forms whose name is not followed by `=` or `:` survive: `curl -u user:SECRET`, `mysql -pSECRET`, `sshpass -p`, `docker login -p`, `PASSWORD 'SECRET'`, `IDENTIFIED BY`: 17 of 17 forms |
| RT-09 | LOW | delimiters inside the secret: a raw `#`, `?`, `/`, `\`, quote or space in a URL password defeats the DSN rule (27 of 99 cases); `& ; , ' "` cut an unquoted `name=value` value and leave its tail |

### RT-01 MEDIUM - a quoted key defeats the assignment rule (`internal/ai/redact.go:69`, the `assignment` pattern)

The pattern needs the separator (`:` or `=`, or `is|was|are`) directly after the name, so the closing quote of a key breaks the match. The shapes below are the most
common way a credential is pasted (an API response, a config file, a code sample): the secret stays whole when it is shorter than 40 characters and has no known prefix
(the hex, prefix and base64 rules never see it).

Repro: `TestRTReproQuotedKeysAndCompoundNames` (Appendix A; the secrets are drawn at run time: 12 and 32 alphanumerics). On the pinned code 10 of 10 quoted forms fail:
`{"user":"bob","password": "<12>"}`, `{"client_secret":"<32>"}`, `{"apiKey": "<32>"}`, `{"token":"<32>"}`, `{'password': '<12>'}`, `os.environ["DB_PASSWORD"] = "<12>"`,
`config['password'] = '<12>'`, `"password" = "<12>"` (TOML), and the comma forms `define('DB_PASSWORD', '<12>');`, `props.setProperty("password", "<12>");`.
End to end (integration probe on the `fix` stack, Part C): `{"client_secret":"<32 alnum>"}` typed into a request, and `{"password": "<12 alnum>"}` inside a field label,
reached the provider request verbatim (segments `request` and `application`).

Patch (verified in a scratch copy, reverted): the optional quote characters and the `name', 'value` comma move into the separator group, so the group numbers and
`assignmentReplace` do not change. The comma form falls into the strict "prose" branch of `assignmentReplace` (value of 6 or more characters with a digit or a symbol).
Combined with RT-02 it is one line (see the diff after RT-02).

### RT-02 MEDIUM - compound variable names are not names (`internal/ai/redact.go:69`, the `\b(?:[a-z0-9]+[_-])*` prefix)

The prefix group accepts words joined by `_` or `-` (`DB_PASSWORD`), but a name glued to another word has no word boundary in front of it, so `PGPASSWORD`,
`dbPassword`, `apiSecret`, `jwtSecret`, `postgresPassword`, `adminPassword` are not recognised, and `SECRET_KEY_BASE` fails the closing `\b`. `PGPASSWORD=... psql` is
probably the single most pasted terminal line of this kind.

Repro: same test, 7 of 7 compound forms fail on the pinned code (`PGPASSWORD=<12> psql -h db -U app -d prod`, `dbPassword=<12>`, `apiSecret=<32>`, `jwtSecret: <32>`,
`--set auth.postgresPassword=<12>`, `adminPassword: <12>` in a YAML block, `SECRET_KEY_BASE=<32>`).

### Patch for RT-01 and RT-02 (one line of `redact.go`, 13-line diff, verified then reverted)

```diff
-var assignment = regexp.MustCompile(`(?i)\b(?:[a-z0-9]+[_-])*(api[_-]?key|apikey|secret(?:[_-]?(?:access[_-]?)?key)?|client[_-]?secret|...|credentials?)\b(\s*[:=]\s*|\s+(?:is|was|are)\s+)(...)`)
+var assignment = regexp.MustCompile(`(?i)\b[a-z0-9_-]*(api[_-]?key|apikey|secret(?:[_-]?(?:access[_-]?)?key)?(?:[_-]base)?|client[_-]?secret|...|credentials?)\b(["'\x60\]]*\s*[:=]\s*|["']\s*,\s*|\s+(?:is|was|are)\s+)(...)`)
```

(`...` stands for the unchanged alternatives and the unchanged value group; the diff is `/home/user/...` free of secrets and is reproduced in full in Appendix B.)

Verification of the patch on the pinned tree, scratch copy:

| check | result |
|---|---|
| `TestRTReproQuotedKeysAndCompoundNames` | FAIL before (17 of 17 cases leak), PASS after; the 15-item keep list (`MAX_TOKENS=4096`, `{"password": "required"}`, `{"password_policy": {...}}`, `["password", "username", "email"]`, ...) is unchanged before and after |
| shipped tests, `gt test -count=1 -run Redact ./internal/ai` | PASS after the patch |
| false-positive corpus (Part B, 311 + 78 inputs) | identical set of changed inputs before and after (40 + 9), no new false positive |
| Part A matrix (class x carrier, 6 samples a cell) | 44 cells improve (253 samples), 0 cells get worse; bad outcomes 5,657 -> 5,404 |
| time at 1 MiB (worst accepted string is 16 KB) | still linear; `assign_*` inputs +10 to 30 % (for example 1.9 s -> 2.1 s), no input over 2.8 s |


### RT-03 MEDIUM - a wrapped key whose first line is too short for any rule leaves both halves (`internal/ai/redact.go:181-247`, `redactWrappedTails`)

The tail rule continues a key only after a redacted marker at the end of a line. When the line break falls early in the key, the first line holds fewer characters than
the minimum of its rule (`ghp_` + 20, `sk-` + 16, `AKIA` + 16 exactly, `sk_live_` + 10, an unprefixed 40-character run), so nothing is redacted, there is no marker, and the
tail alone on its line is not touched either. This is not the documented residual ("the tail shares its line with prose", ADR104 Amendment C item 6): here the tail is alone on
its line and the line is followed by nothing but a newline. Hard-wrapped text is what a paste from an e-mail, a PDF, a chat export or a log viewer gives.

Repro: `TestRTReproWrappedKeyHeadTooShort` (Appendix A; secrets drawn at run time). The first line is a 60, 64, 72 or 76 column line of prose that ends with the first
`head` characters of the key (2 to 30 in steps of 2), the rest of the key stands alone on the next line, then `\nthanks`. Five classes: `ghp_` + 36 alphanumerics,
`sk-` + 48 alphanumerics, `AKIA` + 16, a 40-character base64 secret key, `sk_live_` + 24. Result on the pinned code: **44 of 69 wrapped keys keep 12 or more consecutive secret
characters at every width** (first case: the `ghp_` token cut after 2 characters keeps all 36 characters of its body). The one-line cases of the Part A matrix confirm it
(`Here is the key ghp_<6c>` newline `<30c>`: 20 of 20 samples survive).

Patch (73 added lines, 79-line diff, verified then reverted; Appendix B): `redactSplitKeys` reads the last word of a line together with the lone key-shaped words on
the next lines (at most three) as one word and redacts the lines when that word is wholly one secret of the prefix rules, a 32+ hex run or, only when the first line is
48 or more columns (a wrapped paragraph), a 40+ entropic run. It runs before the tail rule, so a three-way wrap is continued by the existing rule.

| check | result |
|---|---|
| `TestRTReproWrappedKeyHeadTooShort` | FAIL before (44 of 69 at each width), PASS after (0 of 69) |
| `gt test -count=1 ./internal/ai` (whole package) with RT-01/02 and RT-03 applied | PASS (30.8 s) |
| false-positive corpus, 311 + 78 inputs | identical before and after, no new false positive |
| Part A matrix | 0 cells worse |
| time | worst 1 MiB input of many key-shaped lines 6.2 s (linear, 4 x size is about 4.5 x time); 16 KB, the largest string the route accepts, 117 ms |
| residual of the patch | a head glued to prose (`usedAKIA...`), a tail followed by prose, and a break inside the prefix rule's own token class stay as documented |

### RT-04 MEDIUM - vendor token formats that no rule names (`internal/ai/redact.go:50`, the `api_key` alternation; the generic rules start at 40 characters)

Twelve formats, each in all four carriers tried (bare, in a sentence, as a JSON value, as `VALUE=...`): `GOCSPX-` + 28 (Google OAuth client secret), `dckr_pat_` + 27 (Docker Hub),
`shpat_` or `shpss_` + 32 hex (Shopify; the `_` also defeats the hex rule), `hvs.` + 24 (Vault), `glrt-` and `gldt-` + 20 (GitLab runner and deploy tokens; only `glpat-` has a rule),
`sq0atp-` + 22 (Square), `NRAK-` + 27 (New Relic), `<10 digits>:A` + 34 (Telegram bot), `M` + 23 `.` 6 `.` 27 (Discord bot; the dots split the runs, PARTIAL), `xxx8Q~` + 34 (Azure AD secret;
`~` splits the run). Part A matrix: 20 classes with 89 to 100 % bad samples. Repro: `TestRTReproVendorFormats` (Appendix A), 12 of 12 formats fail on the pinned code.

Patch (11-line diff, one alternation extended; scratch copy, then discarded): add the prefixes to `api_key`: `GOCSPX-[A-Za-z0-9_-]{24,}`, `dckr_pat_[A-Za-z0-9_-]{20,}`, `shp(?:at|ca|pa|ss)_[A-Fa-f0-9]{32}`,
`hvs\.[A-Za-z0-9_-]{20,}`, `gl(?:rt|dt|ptt|ft|imt|oas|soat|cbt)-[A-Za-z0-9_-]{16,}`, `sq0(?:atp|csp)-[A-Za-z0-9_-]{22,}`, `NRAK-[A-Z0-9]{27}`, `\d{8,10}:A[A-Za-z0-9_-]{34}`,
`[MNO][A-Za-z0-9_-]{23,25}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,38}`, `[A-Za-z0-9_.-]{3}\dQ~[A-Za-z0-9_.~-]{31,34}` (Appendix B). Verified: repro PASS, `-run Redact` PASS, false-positive corpus
identical (389 inputs), Part A matrix **279 cells improve, 0 get worse, bad samples 5,657 -> 3,985**.

### RT-05 LOW - the connector rule destroys sentences about token and password formats

See Part B.1. `redact.go:92-97`. Seven sentences of the form "The token|secret|password|credentials is|was|are <word with a digit>" lose their subject. No patch proposed (the choice of what to
keep is a product trade-off, see Part B.1).

### RT-06 LOW - an identifier-shaped string is never run through the generic rules (`redact.go:391-433`, `identifierLike`, `redactStructural`)

`redactString` and `redactTree` treat any string matching `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$` as structure and apply only the prefix table and the hex rule. The comment gives the reason
(a long id must not break its references) but the exemption also covers free text: a request, a label, an enum value (128 characters), a rule literal that consists of one lower-case token.
Executed on the `fix` stack: a request that is a 48-character lower-case alphanumeric token, and a field id of 47 such characters, reached the provider verbatim (segments `request` and
`application`), and the id was also stored in the proposal body the model copied it into. The same token with one space before it is redacted (`token` class). Bare, the classes `nuget`
(`oy2` + 43), `rubygems_` + 48 hex, a 52-character Azure DevOps PAT (5 of 6) and `dop_v1_` + 64 hex were not redacted when typed alone. A fix needs the callers to say which fields are references
(the context builder knows: `ID`, `Kind`, the ref fields); not proposed here.

### RT-07 LOW - hex glued to a word character (`redact.go:72` `longHex`, `redact.go:279` `entropy(m) < 4.0`)

Probe: hex runs of 32, 40, 64 and 128 characters with 19 left neighbours and 8 right neighbours (608 cases): **288 are not redacted**. A run survives when the character before it is `_`, `x`
(so `0x` + 64 hex, a wallet private key) or a letter after `f`, or when the character after it is `_` or a letter after `f`; digits and `a`-`f` simply extend the run. Examples: `key_` + 32 hex,
`sk_` + 64 hex, `token` + 40 hex, a hex run followed by `_backup`. `\b` is a boundary only between a word and a non-word character, `_` is a
word character, and `longBase64` cannot save it because the entropy of a hex string is at most 4.0 bits and `entropy(m) < 4.0` rejects almost every such run. Rare as a credential format
(wallet keys, `prefix_hex` tokens with no rule); no patch.

### RT-08 MEDIUM - the name is not followed by `=` or `:` (`redact.go:69`, the separator group)

`curl -u admin:SECRET`, `curl --user "admin:SECRET"`, `mysql -uroot -pSECRET`, `mysqldump ... -pSECRET`, `sshpass -p 'SECRET'` and `sshpass -p SECRET`, `docker login -u bob -p SECRET` and
`--password SECRET`, `CREATE USER bob WITH PASSWORD 'SECRET'`, `ALTER ROLE app WITH LOGIN PASSWORD 'SECRET'`, `... IDENTIFIED BY 'SECRET'` and bare, a `.netrc` `machine ... password SECRET`,
`redis-cli -a SECRET`, `mongo -p SECRET`, `wget --http-password SECRET`, `zip -P SECRET`: the secret (12 alphanumerics, no symbol) survives in **17 of 17** forms on the pinned code. These
are the terminal and SQL pastes the brief lists; the matrix shows the same for 33 of 41 command-line templates and 8 of 10 SQL templates. Repro: `TestRTReproCommandAndSQLForms` (Appendix A).

Patch (12-line diff; scratch copy, then discarded): one more entry in `redactRules`, class `password_arg`, that matches the command words with their password option and the SQL
`PASSWORD '...'` / `IDENTIFIED BY ...` forms (Appendix B). The match replaces the option as well as the value (`[REDACTED:password_arg] https://...`). Verified: repro 17 of 17 PASS,
`-run Redact` PASS, false-positive corpus identical, Part A matrix 22 cells improve, 0 get worse.

### RT-09 LOW - delimiters inside the secret

- URL password: a raw `#`, `?`, `/`, `\`, `'`, `"`, `<`, `>` or space in the userinfo ends the `url_credentials` pattern before the `@`, so `postgres://app:<6>#<6>@db...` is not redacted at all (the user name and
  both halves of the password stay). 9 of the 33 characters tried, in 3 schemes: 27 of 99 cases. `@` itself is fine (the pattern takes the last `@`).
- Unquoted `name=value`: the value stops at `,` `;` `&` `'` `"` or a space, so `DB_PASSWORD=<6>&<6>` leaves the tail after the delimiter (here 6 characters; a long generated password
  leaves more); the colon form (`password: <6>&<6>`, value must be 8+ characters) leaves the whole value.
No patch proposed.
## Part A - secret shapes that survive (results matrix)

Corpus: 3020 class/carrier/form cells, 6 samples each (18120 redactions attempted), 298 credential and paste classes in 12 groups, run on the pinned code. Each sample is drawn at run time from the alphabet and length of its class. REDACTED = no run of 12 or more consecutive secret characters left; PARTIAL = such a run left; SURVIVES = the whole secret left. 28 carriers per single-token class (bare, sentence, after a label, JSON value and JSON key, YAML, `.env`, CRLF paste, indented, double/single quotes, backticks, markdown fence, URL path/query/fragment, trailing dot, parentheses, between two other secrets, hard wrap at 60/64/72/76 columns, followed by a camelCase line or a hex-looking word); structured classes use their own forms (a PEM block, a header, a command line, a SQL statement, a file).

| group | cells | REDACTED | PARTIAL | SURVIVES |
|---|---|---|---|---|
| payment card numbers (12 issuers x 9 forms) | 109 | 594 | 0 | 60 |
| cloud (AWS, GCP, Azure, DigitalOcean, Cloudflare, Heroku) | 432 | 1909 | 264 | 419 |
| command lines | 41 | 64 | 0 | 182 |
| connection strings and DSNs | 20 | 94 | 0 | 26 |
| files and code (env, netrc, npmrc, kubeconfig, JSON, YAML, XML, logs, prose) | 75 | 210 | 0 | 240 |
| JWT, hex, base64, random strings | 336 | 1100 | 219 | 697 |
| headers | 23 | 132 | 0 | 6 |
| payments and messaging | 588 | 2712 | 384 | 432 |
| query strings | 14 | 53 | 0 | 31 |
| AI and SaaS keys | 756 | 3057 | 552 | 927 |
| source hosts and registries | 616 | 2526 | 300 | 870 |
| SQL | 10 | 12 | 0 | 48 |

Every class with a prefix rule is REDACTED in all 24 neutral carriers (bare, sentence, label, JSON, YAML, env, CRLF, quotes, fences, URLs, punctuation, between secrets, followed by camelCase or hex words): 0 failures out of 8,640 samples of 60 such classes. The failures sit in the hard-wrap carriers (RT-03 and the documented residual) and in the classes below.

### Classes with failures outside the wrap carriers (counts over all non-wrap carriers/forms of the class)

| class (group) | REDACTED | PARTIAL+SURVIVES | what the redactor sees |
|---|---|---|---|
| passphrase_words (generic) | 0 | 144 | four plain words (documented residual) |
| random_24_alnum (generic) | 12 | 132 | 24 alphanumerics |
| discord_bot_token (pay) | 12 | 132 | 24.6.27 characters: `.` splits the runs (PARTIAL: 24 and 27 left) |
| atlassian_legacy_24 (saas) | 12 | 132 | 24 alphanumerics, no rule |
| intercom_pagerduty_20 (saas) | 12 | 132 | 20 alphanumerics, no rule |
| gitlab_deploy_gldt (scm) | 12 | 132 | `gldt-` + 20 |
| gitlab_runner_glrt (scm) | 12 | 132 | `glrt-` + 20: only `glpat-` is a rule |
| vault_hvs (scm) | 12 | 132 | `hvs.` + 24: `.` splits |
| vault_legacy_s (scm) | 12 | 132 | `s.` + 24 |
| azure_ad_client_secret (cloud) | 14 | 130 | `xxx8Q~` + 34 of `[A-Za-z0-9_.~-]`: `~` splits the run, no prefix rule |
| gcp_oauth_client_secret (cloud) | 18 | 126 | `GOCSPX-` + 28: no rule, under 40 |
| heroku_api_key_uuid (cloud) | 18 | 126 | UUID, no rule (by design) |
| hex_0x_eth_privkey (generic) | 18 | 126 | `0x` + 64 hex: `\b` fails after `x`, hex entropy under 4.0 (RT-07) |
| random_32_alnum (generic) | 18 | 126 | 32 alphanumerics |
| random_39_alnum (generic) | 18 | 126 | 39 alphanumerics (the generic rule starts at 40) |
| square_oauth_sq0atp (pay) | 18 | 126 | `sq0atp-` + 22 |
| telegram_bot_token (pay) | 18 | 126 | `<10 digits>:AA` + 33: `:` splits, under 40 |
| twilio_api_key_sid_SK (pay) | 18 | 126 | `SK` + 32 hex: glued prefix |
| mistral_32alnum (saas) | 18 | 126 | 32 alphanumerics, no rule |
| newrelic_NRAK (saas) | 18 | 126 | `NRAK-` + 27 |
| shopify_shpat (saas) | 18 | 126 | `shpat_` + 32 hex: `_` blocks `\b`, 38 chars |
| shopify_shpss (saas) | 18 | 126 | `shpss_` + 32 hex |
| docker_hub_pat (scm) | 18 | 126 | `dckr_pat_` + 27: no rule, under 40 |
| npm_legacy_uuid (scm) | 18 | 126 | UUID, no rule (by design) |
| newrelic_license (saas) | 63 | 81 | 36 hex + `NRAL`: entropy |
| card_maestro (card) | 30 | 24 | Maestro 6759... is not in the issuer table |
| card_maestro_5018 (card) | 30 | 24 | Maestro 5018... is not in the issuer table |
| digitalocean_pat (cloud) | 124 | 20 | `dop_v1_` + 64 hex: `\b` fails after `_`, entropy about 4.0 |
| card_mastercard (card) | 42 | 12 | Mastercard as a JSON number (`{"card": 5...}`): digits glued to `:`? see form list |
| 7z_p (cmd) | 0 | 6 |  |
| az_login_p (cmd) | 0 | 6 |  |
| curl_json_password (cmd) | 0 | 6 |  |
| curl_u (cmd) | 0 | 6 |  |
| curl_u_quoted (cmd) | 0 | 6 |  |
| curl_user (cmd) | 0 | 6 |  |
| docker_login_p (cmd) | 0 | 6 |  |
| docker_login_password_space (cmd) | 0 | 6 |  |
| echo_pipe_docker_login (cmd) | 0 | 6 |  |
| gpg_passphrase (cmd) | 0 | 6 |  |
| helm_set (cmd) | 0 | 6 |  |
| htpasswd_b (cmd) | 0 | 6 |  |
| ldapsearch_w (cmd) | 0 | 6 |  |
| mongo_p (cmd) | 0 | 6 |  |
| mysql_pSECRET (cmd) | 0 | 6 |  |
| mysql_p_quoted (cmd) | 0 | 6 |  |
| mysqldump_p (cmd) | 0 | 6 |  |
| net_use (cmd) | 0 | 6 |  |
| npm_config_authtoken (cmd) | 0 | 6 |  |
| openssl_pass (cmd) | 0 | 6 |  |
| pgpassword_env (cmd) | 0 | 6 |  |
| powershell_securestring (cmd) | 0 | 6 |  |
| redis_cli_a (cmd) | 0 | 6 |  |
| smbclient_pct (cmd) | 0 | 6 |  |
| sqlcmd_P (cmd) | 0 | 6 |  |
| sshpass_p (cmd) | 0 | 6 |  |
| sshpass_p_quoted (cmd) | 0 | 6 |  |
| wget_http_password_space (cmd) | 0 | 6 |  |
| wpa_passphrase (cmd) | 0 | 6 |  |
| zip_P (cmd) | 0 | 6 |  |
| postgres_url_pw_with_hash (conn) | 0 | 6 |  |
| access_key_space (file) | 0 | 6 |  |
| api_key_space_colon (file) | 0 | 6 |  |
| csv_password_col (file) | 0 | 6 |  |
| docker_config_auth (file) | 0 | 6 |  |
| env_file_apiSecret_camel (file) | 0 | 6 |  |
| env_file_camel_dbPassword (file) | 0 | 6 |  |
| env_file_camel_jwtSecret (file) | 0 | 6 |  |
| env_file_encryption_key (file) | 0 | 6 |  |
| env_file_pw (file) | 0 | 6 |  |
| env_file_secret_key_base (file) | 0 | 6 |  |
| env_file_sendgrid_pass (file) | 0 | 6 |  |
| html_input_password (file) | 0 | 6 |  |
| java_setproperty (file) | 0 | 6 |  |
| json_apiKey_camel (file) | 0 | 6 |  |
| json_client_secret (file) | 0 | 6 |  |
| json_package_npm_token (file) | 0 | 6 |  |
| json_password (file) | 0 | 6 |  |
| json_password_pretty (file) | 0 | 6 |  |
| json_secret (file) | 0 | 6 |  |
| json_token (file) | 0 | 6 |  |
| key_colon (file) | 0 | 6 |  |
| log_line_with_password_quoted (file) | 0 | 6 |  |
| login_slash (file) | 0 | 6 |  |
| md_table_password (file) | 0 | 6 |  |
| netrc (file) | 0 | 6 |  |
| npmrc_authToken_uuid (file) | 0 | 6 |  |
| npmrc_auth_b64 (file) | 0 | 6 |  |
| ocr_text_spaced (file) | 0 | 6 |  |
| pass_colon (file) | 0 | 6 |  |
| pgpass (file) | 0 | 6 |  |
| private_key_space_colon (file) | 0 | 6 |  |
| pw_colon (file) | 0 | 6 |  |
| python_dict (file) | 0 | 6 |  |
| python_environ (file) | 0 | 6 |  |
| secret_key_space_colon (file) | 0 | 6 |  |
| shell_history (file) | 0 | 6 |  |
| slack_message_paste (file) | 0 | 6 |  |
| xml_add_key_value (file) | 0 | 6 |  |
| xml_password_element (file) | 0 | 6 |  |
| bare_bearer_short12 (header) | 0 | 6 |  |
| auth_query (query) | 0 | 6 |  |
| code_oauth (query) | 0 | 6 |  |
| key_query_alnum32 (query) | 0 | 6 |  |
| pass_query (query) | 0 | 6 |  |
| pw_query (query) | 0 | 6 |  |
| nuget (scm) | 138 | 6 | `oy2` + 43 base32: identifier-shaped when bare (RT-06) |
| rubygems (scm) | 138 | 6 | `rubygems_` + 48 hex: identifier-shaped when bare, entropy about 4.0 |
| alter_role_password (sql) | 0 | 6 |  |
| create_user_identified_by (sql) | 0 | 6 |  |
| create_user_with_password (sql) | 0 | 6 |  |
| grant_identified_by (sql) | 0 | 6 |  |
| insert_users_hash_plain (sql) | 0 | 6 |  |
| oracle_connect (sql) | 0 | 6 |  |
| oracle_identified_by_bare (sql) | 0 | 6 |  |
| set_password_function (sql) | 0 | 6 |  |
| azure_devops_pat (cloud) | 139 | 5 | 52 base32 chars: identifier-shaped when bare (RT-06) |
| random_lowercase_48 (generic) | 140 | 4 |  |
| ldap_bind (conn) | 3 | 3 |  |
| redis_empty_user (conn) | 3 | 3 |  |
| amqp_url (conn) | 4 | 2 |  |
| mysql_url (conn) | 4 | 2 |  |
| postgres_url (conn) | 4 | 2 |  |
| postgres_url_human_pw (conn) | 4 | 2 |  |
| smtps_url (conn) | 4 | 2 |  |
| creds_colon_pair (file) | 4 | 2 |  |
| psql_url (cmd) | 5 | 1 |  |
| wget_password_eq (cmd) | 5 | 1 |  |
| ftp_url (conn) | 5 | 1 |  |
| jdbc_query_password (conn) | 5 | 1 |  |
| odbc_pwd (conn) | 5 | 1 |  |
| rediss_default (conn) | 5 | 1 |  |
| compose_env_list (file) | 5 | 1 |  |
| env_file_named (file) | 5 | 1 |  |
| ini_password (file) | 5 | 1 |  |
| yaml_password_colon (file) | 5 | 1 |  |
| password_query (query) | 5 | 1 |  |

### Second tier: obfuscation and encodings (outside the stated threat model; LOW or INFO)

Each transform was applied to three keys (`ghp_` + 36, `sk-` + 48, `AKIA` + 16) inside "Use ... for the job." (`marker` = a redaction marker appeared; `unchanged` = the sentence came back byte for byte).

| transform | ghp_ | sk- | AKIA |
|---|---|---|---|
| plain (control) | marker | marker | marker |
| `%67hp_` / `%61KIA` (first character URL-encoded) | marker | marker | **unchanged** |
| `&#103;hp_` / `&#65;KIA` (HTML entity) | **unchanged** | marker | **unchanged** |
| `\u0067hp_` / `\u0041KIA` written as literal text | marker | marker | **unchanged** |
| base64 of the whole key | marker | marker | **unchanged** (28 characters) |
| reversed key | marker | marker | **unchanged** |
| characters separated by spaces, or by hyphens | **unchanged** | **unchanged** | **unchanged** |
| braille blank (U+2800) every 6 characters | **unchanged** | **unchanged** | **unchanged** |
| combining acute (U+0301) after every 6th character | **unchanged** | **unchanged** | **unchanged** |
| URL-encoded whole key, hex of the key, upper case, Armenian o, zero-width space, ZWNJ, LRM, CGJ, Hangul filler, soft hyphen, RLO, Cyrillic and Greek look-alikes, fullwidth | marker | marker | marker |

INFO/LOW: the short fixed-length formats (AKIA) and any key split by spaces, hyphens, U+2800 or combining marks come back unchanged; everything the fold table covers is found. The combining-mark
and U+2800 survivals are the documented "table is short on purpose" residuals; the encodings are outside the threat model. Not measured: whether a marker in the "marker" rows left a run of 12 characters.

## Part C - is the redactor applied everywhere

Method: read `context.go`, `repair.go`, `service.go`, `store.go`, `gateway.go`, `provider_http.go`, `internal/platform/access/ai.go`, `ai_decide.go`, then proved it on the `fix`
stack with two integration probes (`it-run fix`, logs `rt-partc-1/3`; the probe builds a draft whose entity label, enum value, page-component label, rule string literal, field
label and field id carry run-time secrets, sends a request with six more, scripts a model answer that echoes three, applies it, and asks again). The provider's requests
(`e.provider.Requests()`), the three `control.ai_*` tables, every audit row of the tenant, the captured log stream (DEBUG) and all responses were searched for each secret.

**Verdict: no path skips the redactor for text that reaches a provider (no HIGH).** The only unredacted sink is the model's own answer, which the code states is not redacted.

| # | author text | sink | protected by | evidence (executed) |
|---|---|---|---|---|
| 1 | `ProposeInput.Request` | provider segment `request` | `validRequestText` (service.go:156), `validateRequest` + `sanitizeText` (context.go:120), `redactString(request)` (context.go:255) | PASS for card in groups, AKIA id, JWT, Slack webhook secret, `password=`: absent; `{"client_secret":"<32>"}` present (RT-01) |
| 2 | draft definition id, kind, label | `application.definitions[]` | `redactString(clip())` (context.go:163) | PASS: ghp token in a label absent; a 47-character lower-case token used as a field id present (RT-06) |
| 3 | whole definition body: enum values, component labels, rule literals, calendars | `application.definitions[].definition` | `redactRaw` -> `redactTree` (context.go:169, redact.go:395); JSON object keys are skipped by design (the only author-chosen keys are `Activity.Inputs`, pattern `^[a-z][a-z0-9_]{0,63}$`) | PASS: AWS secret key as an enum value, DSN password in a component label, `sk-` key in a rule literal: absent; `{"password": "<12>"}` in a field label present (RT-01) |
| 4 | namespace, app id, schema version | `application` | `redactString(clip())` (context.go:179-180) | code read; mutation M72 (label) killed, M73 (app id) **survives** the shipped tests |
| 5 | release-diff ids, kinds, aspects | `release_diff` | context.go:191-193 | code read; mutation M77 **survives** |
| 6 | tool catalog summary | `tools` | `cleanTool` (context.go:283-296); operator-authored, no endpoint or credential field exists in `ToolSummary` | code read; mutation M74 **survives** |
| 7 | role names | `roles` | context.go:219 | code read; mutation M75 killed |
| 8 | requirement ids, categories, statuses | `requirements` | context.go:239; statements and semantics are not sent at all | code read; mutation M76 **survives** |
| 9 | repair problems | `diagnostics` segment of attempts 2 and 3 | `capDiagnostics` (repair.go:295-307): `redactString(clip())` on path, code, message | PASS: a model answer with an unknown key shaped like a token produced `{"path":"","code":"unknown_field","message":"a key is not part of the closed schema"}`, the key never reaches the next prompt; mutations M78-M80 (remove the redaction of path or message, remove the cap) **survive** the whole shipped package |
| 10 | output schema, templates | `output_schema`, instructions | static embedded contract files and constants (`schema_text.go:90-`, `template.go`) | code read: no author text |
| 11 | the model's answer | `ai_proposals.body`, create and GET responses | **not redacted** (documented: `internal/ai/corpus/check.go:183`, "ADR104 redacts the context, not the output") | PASS as documented: the three echoed shapes (ghp, AKIA, sk-) are stored and returned verbatim; after apply they sit in the draft and are redacted again in the next prompt (request 2: absent) |
| 12 | stored rows | `ai_proposals`, `ai_invocations`, `ai_usage` | the request text is never stored (SHA-256 only, service.go:276); diagnostics are `capDiagnostics` output; provider and model labels go through `apiLabel` (alphabet `[A-Za-z0-9._:/@+-]`, 64 and 128 bytes, repair.go:249) | PASS: no secret in `ai_invocations`, `ai_usage`, `diagnostics`; `body` holds model output only (row 11) |
| 13 | audit rows | `audit.entries` | `aiEvidence` copies ids, hashes, codes, counts, provider, model, template (ai.go:143) | PASS: 0 of 15 secrets in the `ai.*` rows (7 KB) and in all 9 KB of the tenant's audit rows |
| 14 | log lines | httpserver logger | the AI routes log nothing of bodies; the provider adapter returns fixed text (`provider_http.go`, header comment) | PASS: DEBUG stream of the whole probe (1.7 KB) holds no secret |
| 15 | error bodies and responses | `aiErrorBody`, `GET /proposals*` | fixed messages and codes (ai.go:90-253); the diagnostics of an apply refusal (`DiagnosticsOf`, service.go:446) are not run through the redactor but carry validator and compiler vocabulary and JSON pointers made of validated ids | code read; responses of the probe searched: only the model echo (row 11) |
| 16 | metric labels | OpenTelemetry | grep of every `attribute.` and `metric.` use: ids, hashes, versions, reasons; no AI-specific metric exists | NOT RUN beyond the grep (no metrics endpoint in the test handler) |

`holdsRedaction` (patch.go:124, redact.go:460), unit probe `TestRTHoldsRedaction` through `ParsePatchProposal` and `ApplyProposal`:

| model writes into a definition | result |
|---|---|
| `[REDACTED:secret]` in a label; in a rule string literal | refused (`INVALID_DEFINITION`) |
| the same with `[` for the bracket, or `:` for the colon, in a typed string and in a `json.RawMessage` literal | refused: HELD (the strict parser re-encodes the raw message before the check) |
| `[redacted:secret]`, `[REDACTED :secret]`, fullwidth brackets, `[Redacted:token]` | accepted; none is the placeholder the redactor writes (INFO) |
| a zero-width character inside the prefix | refused earlier by the strict parser ("invisible character") |
| a placeholder in a requirement proposal's `statement`, a question, an assumption, a patch `rationale` | accepted and stored (the guard covers `op.Definition` only). A requirement statement then holds text the human sees before confirming; INFO |

Other observations (not findings): the generic rules never see JSON numbers (`redactTree` only visits strings), so a 16-digit card number written as a JSON integer in a
`typed_literal` is not redacted (schema 1.5 allows integers up to 10^18) - LOW, rare.

## Part B - what the redactor destroys, and how it behaves

### B.1 False positives (389 realistic inputs, 12 categories, sentences and pastes of the product's domain)

Set 1: 311 inputs (requirements 30, roles 20, security vocabulary 51, identifiers 28, hashes and ids 24, numbers 25, URLs and paths 25, code 25, markdown 10, multi-line 10,
non-English 25, assignment prose 30, tail rule 8). Set 2: 78 (connector prose 47, pasted tickets, e-mails and specs 31). The tail-rule category holds real keys on purpose and is
excluded from the rate. Result: **40 of 381 inputs changed (10.5 %), 39 if the real card number is not counted; none of the 30 requirements, 20 role sentences, 25 code samples,
10 markdown blocks, 25 non-English sentences (Tamil, Hindi, Chinese, Japanese, Korean, Arabic, Hebrew, Russian, Greek, Thai, Vietnamese, accents, emoji) and 31 pasted tickets
changed**; one input differs only because carriage returns are removed.

| rule (marker class) | inputs changed | typical input | harm |
|---|---|---|---|
| `token` (40+ character run of `[A-Za-z0-9+/_-]` with a digit and entropy 4.0) | 13 | kebab-case slugs, paths and URL paths of 40+ characters containing a digit (`src/modules/orders/components/OrderApprovalThresholdForm2`, `how-to-configure-the-approval-workflow-for-orders-over-500-usd`), a long upper-case constant, a 75-character test name, a base64 data URI | degrading (the id or link is replaced, the sentence stays) |
| `hex` (32+ hex characters) | 9 | commit ids, digests, ETags, trace ids, checksums | degrading, by design |
| `secret` (assignment rule) | 14 | six `name=value` placeholders (`token=null`, `secret = TODO`, `password=null`, `Password: ********`, `pwd=/home/user/project`, `client_secret: rotate_quarterly`), `Secret: vault://prod/db/main`, and **seven sentences of the form "The token\|secret\|password\|credentials is\|was\|are <word with a digit>"** | six harmless or degrading; the seven sentences are destructive (below) |
| `authorization` | 2 | `Authorization: required for all write endpoints`, `Authorization: mandatory for all endpoints` | degrading (the requirement word is removed) |
| `card` | 1 | `Card 4111 1111 1111 1111 must never be stored.` | harmless (a card number) |
| `url_credentials` | 1 | `ssh://git@git.example.com:22/erp/approvals.git` (a user name, no password) | degrading |

The tail rule and the "is" connector, which the brief names as the most likely to remove ordinary text:

- Held (executed): 50 of the 51 security-vocabulary sentences (only `Authorization: required ...` changed) and 38 of the 47 connector sentences are returned byte for byte,
  among them `the token is valid for 30 minutes`, `The password is required.`, `The key is rotated daily by the platform.`, `The token is JWT-based and stateless.`, `The secret is rotated
  every 90 days.`, `Passwords are case sensitive.`, `The credentials are stored in the vault.`, `The password is hashed before it is stored.`, `The token was refreshed at 12:30 UTC.`
- **RT-05 LOW - the connector rule destroys sentences about token and password formats** (`redact.go:92-97`, the prose branch of `assignmentReplace`: a value of 6+ characters with
  a digit). Seven of the 47 connector sentences change, each loses its subject: `The token is 64-character hex.` -> `The [REDACTED:secret] hex.`; `The secret is 256-bit AES.` ->
  `The [REDACTED:secret] AES.`; `The token is HS256-signed.` -> `The [REDACTED:secret].`; `The token is OAuth2-compliant.`; `The password is SHA-256 hashed with a salt.` ->
  `The [REDACTED:secret] hashed with a salt.`; `The credentials are SHA-256 hashed.`; `The password was 2FA-verified.` These are normal non-functional requirements. 7 of 381 inputs
  (1.8 %), so under the 2 % bar for MEDIUM. A patch is not proposed here; the shape to exclude is a value whose last hyphen-separated part is a plain lower-case word of 3+ letters
  (`64-character`, `HS256-signed`) or `LETTERS-DIGITS` of at most four letters (`SHA-256`, `AES-256`), at the price of missing `The password is Summer-2026` style prose.
- Tail rule (8 inputs, each with a real key or a typed marker at the end of a line): after `Key sk-<48>` three following lines that are single camelCase words (`OrderItem`,
  `CustomerAccount`, `InvoiceLine`) all became `[REDACTED:api_key]`; single capitalised words (`Payments`, `Shipments`, `PurchaseOrders`) and prose lines stop the chain and
  survive. A marker typed by the user (`A user typed [REDACTED:token]` at the end of a line) starts the chain exactly like a real one: the three camelCase lines after it
  became markers. Both need a key or a typed marker at the end of the previous line, so the damage is limited to that text. INFO.

### B.2 Timing

`redactString` on inputs built against each rule (4 KB, 16 KB, 64 KiB, 256 KiB, 1 MiB; ms; 42 inputs, abridged to the slowest of each family):

| input | 4 KB | 16 KB | 64 KiB | 256 KiB | 1 MiB |
|---|---|---|---|---|---|
| `tail_lines` (a key, then 100,000 short camelCase lines) | 15 | 72 | 290 | 916 | 3,015 |
| `url_credentials_many` | 12 | 53 | 196 | 708 | 3,021 |
| `assign_many_values` (`password=abcd` x n) | 23 | 43 | 205 | 570 | 2,305 |
| `assign_is_connector` | 7 | 34 | 127 | 500 | 2,076 |
| `card_digit_space` (`1 1 1 ...`) | 5 | 26 | 113 | 616 | 1,985 |
| `assign_colon`, `basic_many`, `fold_cyrillic_prefix` | 5-7 | 25-29 | 130-135 | 458-538 | 1,846-1,885 |
| `b64_one_run` (one 1 MiB run, entropy over it) | 2 | 17 | 61 | 175 | 807 |
| `fold_combining` (1 MiB of combining marks), `emoji_4byte`, `invalid_utf8`, `pem_*` | 0-5 | 2-19 | 1-69 | 4-216 | 13-892 |

Growth is linear (four times the size takes 3 to 4.5 times as long); no input is quadratic. The largest single string the routes accept is the request (4,000 runes, at most
16 KB, body cap 32 KiB) or a rule literal (4,096 characters): at most about 75 ms on the pinned code (117 ms with the RT-03 patch). The whole draft is redacted string by string, so the cost follows the document size
(`metadata.MaxBytes` 256 KiB): the slowest accepted draft I built (179 fields with 100 enum values each, every value a match) took **387 ms** in `BuildContext` (then refused as too
large; the size check comes after the redaction); 100,000 matching 8-byte strings take 1.96 s (about 20 microseconds each). `redactTree` over 100,000 strings took 7.0 s, which no
accepted document can reach (256 KiB holds under 30,000 strings); depth 9,000 took 0.9 ms. Invalid UTF-8, NUL, lone surrogates (decoded to U+FFFD by `encoding/json`), 4-byte runes and
long combining sequences: no panic. **No input takes over 2 s on an accepted size: PASS.** With the RT-01/02 patch the `assign_*` inputs are 10 to 30 % slower at 1 MiB (still linear).

### B.3 Properties and fuzz

My own native fuzz target `FuzzRTRedact` (seeded with 105 class tokens and their forms, 170 paste templates and the 389 false-positive inputs; asserts per input: two calls give the same output; applying the redactor
to its own output changes nothing; no change without a counted replacement; the output is valid UTF-8; the output is at most 4 x input + 64 bytes):

| run | code | executions | result |
|---|---|---|---|
| 5 min under `hv` (`-fuzztime 300s -parallel 2`) | pinned | 402,055, 122 new interesting inputs | **PASS**, no failure |
| 25 s, no lock (under the 30 s rule) | pinned | 27,776 | PASS |
| 40 s, no lock (over the 30 s rule: my mistake) | all four patches | 35,690 | PASS |

The `hv` run waited about 40 minutes for the lock. Measured expansion on the 42 adversarial families at 16 KB: worst **2.71 x** (a key followed by 7-byte camelCase lines, each replaced by an 18-byte marker), then 1.93 x
(`http://u:p@h/ ` x n); the 64 KiB context cap is applied after redaction. Shipped properties re-run on the pinned code: `TestRedactionIsIdempotentOnMixedFragments`, `TestRedactionSettlesToAFixedPoint`,
`TestRedactionFoldsLookalikesOnlyToExposeASecret` and the seed corpus of `FuzzRedactText` PASS. Strings without a secret come back byte for byte in every script tried (25 sentences in 17 scripts, emoji sequences,
accented Latin, plus the fuzz assertion "no change without a count"). A marker typed by a user acts like a real one for the tail rule (B.1).

## Mutation table (my own 90 mutations; test adequacy)

Method: my own driver (`mutate.py`, not the integrator's) applies one exact-string mutation to a product file of the worktree, runs `gt test -count=1 -run Redact ./internal/ai` (the whole package, about 30 s, for the 15 mutations of `context.go`, `repair.go`, `patch.go` and `holdsRedaction`; the sanitizer mutations M51-M53 with `-run Redact|Sanitize`), records KILLED (a shipped test fails) or SURVIVED, and reverts with `git checkout`. Mutants that did not compile were corrected and re-run. `git status` was clean after the last mutation.

Result: **53 killed, 37 survived (2 of them equivalent), 0 skipped.** Killed: M03, M04, M05, M07, M09, M12, M14, M15, M16, M17, M18, M23, M24, M25, M26, M27, M28, M29, M30, M31, M34, M39, M41, M42, M43, M44, M46, M47, M48, M49, M51, M52, M54, M55, M56, M57, M59, M62, M63, M64, M67, M69, M70, M71, M72, M75, M81, M82, M83, M86, M87, M88, M90.

| survivor | mutation | why the shipped tests do not catch it |
|---|---|---|
| M01 | pem rule loses DOTALL | masked for long bodies by the base64 rule; a short body or the header line leaks - gap |
| M02 | jwt segments need 50 chars | masked by the base64 rule on the 43-character signature; the other two segments would leak - gap |
| M06 | api_key loses github_pat_ | masked by the base64 rule (the token is 82 characters) - gap |
| M08 | api_key loses SG. | masked on the 43-character part only; the first part would leak - gap |
| M10 | bearer needs 32 chars | the shipped bearer token is longer than 32 - no near-miss at 16-31 - gap |
| M11 | authorization needs 80 chars | masked by the bearer rule for `Authorization: Bearer`; `Token`/`Basic` forms would leak - gap |
| M13 | basic decode check drops the colon test | the colon test is the only thing that keeps `Basic information` out of basic_auth - false-positive guard untested |
| M19 | card: visa 13 digits not accepted | no 13-digit Visa case - gap |
| M20 | card: 19-digit window shortened to 16 | no 17-19 digit card case - gap |
| M21 | assignment loses passphrase | no `passphrase=` case - gap |
| M22 | assignment loses credentials | no `credentials=` case - gap |
| M32 | longBase64 needs 48 | every shipped base64 case is 56 or more characters - no 40-47 case - gap |
| M33 | entropy floor removed | no 40+ character low-entropy near-miss - the entropy floor is untested |
| M35 | digit requirement removed | no 40+ character all-letter near-miss |
| M36 | letter requirement removed | no 40+ character all-digit near-miss |
| M37 | tail: marker class jwt dropped | tail after a jwt marker untested |
| M38 | tail: marker class hex dropped | tail after a hex marker untested |
| M40 | tail: credentialShaped mixed-case branch only | tail word without digit but with mixed case untested |
| M45 | settled repeats once | only one counterexample needs a second pass; two extra passes are not pinned |
| M50 | identifierLike accepts every lower-case token | identifier-shaped near-miss with spaces untested |
| M53 | sanitizeText: invalid UTF-8 kept | input that is only invalid UTF-8 (no other dirty rune) untested; json.Marshal repairs it later, low impact |
| M58 | fold: only Cf dropped (no default ignorable, no variation selector) | default-ignorable and variation-selector characters inside a key untested |
| M60 | fold: mathematical digits dropped | mathematical digits untested |
| M61 | fold: circled capitals dropped | circled capitals untested |
| M65 | fold: Cyrillic a removed | Cyrillic a untested |
| M66 | fold: Greek omicron removed | Greek omicron untested |
| M68 | holdsRedaction ignores marshal errors | equivalent: json.Marshal of a Definition cannot fail |
| M73 | context: app id not redacted | **no test pins the redaction of `app_id`** |
| M74 | context: tool refs not redacted | **no test pins the redaction of tool refs** |
| M76 | context: requirement ids not redacted | **no test pins the redaction of requirement ids** |
| M77 | context: diff ids not redacted | **no test pins the redaction of release-diff ids** |
| M78 | repair: diagnostic message not redacted | **no test pins the redaction of a diagnostic message in the repair loop** |
| M79 | repair: diagnostic path not redacted | **no test pins the redaction of a diagnostic path** |
| M80 | repair: feedback diagnostics not capped | **the repair loop can feed uncapped, unredacted diagnostics back and no test notices** |
| M84 | clip: no sanitize | equivalent: `redactString` sanitizes again |
| M85 | url_credentials needs a password colon | URL userinfo without a password colon untested |
| M89 | slack token class shrunk | only `xoxb-` tested of the Slack classes |

Caveat: the 28 survivors of the `-run Redact` scope were not re-run against the whole package except M51-M53 (M53 still survives); other tests (for example `TestBuildContext*`, which killed M34, M82, M86-M88) may catch some of them. The seven mutations of `context.go` and `repair.go` (M73, M74, M76-M80) were run against the whole package and survive: that is the finding for the integrator - the redaction of those fields is untested, although Part C shows it is applied.


## What I did not cover

- The author's 300,000-run property (`REDACT_PROPERTY_RUNS`) and the integrator's 25-mutation driver were not run; my 90 mutations and 402,055 fuzz executions replace them.
- Part C was executed through the scripted provider only (no real model, no real HTTP adapter); the HTTP adapter's request body is `json.Marshal(ProviderRequest)` of the same segments (read, not executed against a server).
- The requirement `confirm` route was exercised once and answered 400 (my request shape); what a confirmed requirement set stores of a model statement was therefore not shown by execution (the proposal body was).
- Metric labels: read (grep), not executed.
- The remaining matrix classes outside the 105 + 170 + 12 x 9 forms (gRPC metadata, GraphQL variables, SAML assertions, kubeconfig exec plugins, Terraform state, Jupyter outputs, OCR text with line noise) and the card number as a JSON integer.
- Patches 3 and 4 widen the table (regex additions) and were checked against 389 false-positive inputs only; a 1,000-sentence corpus of ERP prose would be the next check. Patch 2 (split keys) was checked on one hard-wrapped family; a key broken inside a table cell or a YAML block scalar was not tried.
- A 5-minute fuzz of the patched code under `hv` (only 40 s without the lock).
- The Unicode confusables beyond the table, precomposed accents and other scripts, a password that is a plain word, a key broken in the middle of a paragraph (tail shares its line with prose): documented residuals, quantified only for the wrap (RT-03) and the obfuscation tier.
## Appendix A - generators, corpus builder and repro tests (Go; no secret literal: values are drawn at run time)

All code is package `ai` test code (`zz_review_*_test.go`), deleted from the worktree before every commit. The full tables are about 105 classes and 170 templates in the same shape; the 28 carriers of Part A are listed in its text.

### A.1 helpers and class generators
```go
// RED TEAM PROBE (temporary, never committed): generators of secret-shaped inputs. Every value is drawn at run time from the alphabet
// and length of its credential class; the source holds prefixes only.


const (
	aAlnum    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	aLower    = "abcdefghijklmnopqrstuvwxyz0123456789"
	aUpperNum = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	aHex      = "0123456789abcdef"
	aHexU     = "0123456789ABCDEF"
	aB64      = aAlnum + "+/"
	aB64u     = aAlnum + "-_"
	aB32      = "abcdefghijklmnopqrstuvwxyz234567"
	aDigits   = "0123456789"
	aPwd      = aAlnum + "!#$%&*+-=?@^_"
	aAzure    = aAlnum + "_.~-"
)

func rs(r *rand.Rand, alphabet string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.Intn(len(alphabet))]
	}
	return string(b)
}

// rsMixed is rs but guarantees at least one digit and one upper and one lower letter when the alphabet allows (real tokens have them; a
// test of the shape must not depend on luck).
func rsMixed(r *rand.Rand, alphabet string, n int) string {
	for {
		s := rs(r, alphabet, n)
		d, u, l := false, false, false
		for _, c := range s {
			switch {
			case c >= '0' && c <= '9':
				d = true
			case c >= 'A' && c <= 'Z':
				u = true
			case c >= 'a' && c <= 'z':
				l = true
			}
		}
		if (d || !strings.ContainsAny(alphabet, aDigits)) && (u || !strings.ContainsAny(alphabet, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")) && (l || !strings.ContainsAny(alphabet, "abcdefghijklmnopqrstuvwxyz")) {
			return s
		}
	}
}

func rtLuhn(r *rand.Rand, prefix string, length int) string {
	digits := []byte(prefix)
	for len(digits) < length-1 {
		digits = append(digits, byte('0'+r.Intn(10)))
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

func rtUUID(r *rand.Rand) string {
	return rs(r, aHex, 8) + "-" + rs(r, aHex, 4) + "-" + rs(r, aHex, 4) + "-" + rs(r, aHex, 4) + "-" + rs(r, aHex, 12)
}

func rtWrap64(s string, n int) string {
	var parts []string
	for len(s) > n {
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return strings.Join(append(parts, s), "\n")
}

// rtSample is one generated credential: token is what the author pastes, core is the part that must not survive, forms are class-specific
// texts (a PEM block, a header, a command line); when forms is empty the generic carriers are applied to token.
type rtSample struct {
	token string
	core  string
	forms []rtForm
}

type rtForm struct{ name, text string }

type rtClass struct {
	group string
	name  string
	gen   func(r *rand.Rand) rtSample
	note  string // what the redactor is expected to do, for the report
}

func tok(prefix string, core string) rtSample { return rtSample{token: prefix + core, core: core} }

func pemBlock(label string, bodyBytes int, r *rand.Rand) (string, string) {
	body := rs(r, aB64, bodyBytes)
	return "-----" + "BEGIN " + label + "-----\n" + rtWrap64(body, 64) + "\n-----" + "END " + label + "-----", body
}

var rtClasses = []rtClass{ // subset; the same shape for all classes
	{"cloud", "aws_access_key_id", func(r *rand.Rand) rtSample { return tok("AKIA", rs(r, aUpperNum, 16)) }, ""},
	{"cloud", "aws_secret_access_key", func(r *rand.Rand) rtSample { return tok("", rsMixed(r, aB64, 40)) }, ""},
	{"cloud", "gcp_api_key", func(r *rand.Rand) rtSample { return tok("AIza", rs(r, aB64u, 35)) }, ""},
	{"cloud", "digitalocean_pat", func(r *rand.Rand) rtSample { return tok("dop_v1_", rs(r, aHex, 64)) }, ""},
	{"scm", "github_pat_classic", func(r *rand.Rand) rtSample { return tok("ghp_", rsMixed(r, aAlnum, 36)) }, ""},
	{"scm", "github_fine_grained", func(r *rand.Rand) rtSample { return tok("github_pat_", rs(r, aAlnum, 22)+"_"+rs(r, aAlnum, 59)) }, ""},
	{"scm", "npm_token", func(r *rand.Rand) rtSample { return tok("npm_", rs(r, aAlnum, 36)) }, ""},
	{"pay", "stripe_sk_live", func(r *rand.Rand) rtSample { return tok("sk_live_", rs(r, aAlnum, 24)) }, ""},
	{"saas", "openai_sk", func(r *rand.Rand) rtSample { return tok("sk-", rs(r, aAlnum, 48)) }, ""},
	{"saas", "anthropic_sk_ant", func(r *rand.Rand) rtSample { return tok("sk-ant-api03-", rs(r, aB64u, 93)+"AA") }, ""},
	{"saas", "huggingface_hf", func(r *rand.Rand) rtSample { return tok("hf_", rs(r, aAlnum, 34)) }, ""},
	{"saas", "shopify_shpat", func(r *rand.Rand) rtSample { return tok("shpat_", rs(r, aHex, 32)) }, ""},
	{"generic", "hex_64_sha256", func(r *rand.Rand) rtSample { return tok("", rs(r, aHex, 64)) }, ""},
	{"generic", "random_32_alnum", func(r *rand.Rand) rtSample { return tok("", rsMixed(r, aAlnum, 32)) }, ""},
}
```

### A.2 paste templates (`{S}` is the run-time secret) and outcome classification
```go
// RED TEAM PROBE (temporary, never committed): templates of the accidental pastes (connection strings, headers, command lines, SQL, files).
// {S} is replaced by a secret generated at run time; nothing here is a secret.


type rtTpl struct {
	group string
	name  string
	kind  string // generator of {S}
	tpl   string
}

// rtSecretFor draws the secret of one template.
func rtSecretFor(r *rand.Rand, kind string) string {
	words := []string{"Winter", "Summer", "Dragon", "Monkey", "Purple", "Tiger", "Orange", "Falcon", "Shadow", "Rocket"}
	switch kind {
	case "pwd": // generated, symbols included
		return rsMixed(r, aAlnum+"!#$%&*+-=?@^_", 12)
	case "pwdh": // human: Word + digits + symbol
		return words[r.Intn(len(words))] + rs(r, aDigits, 4) + string("!#$%&*"[r.Intn(6)])
	case "pwdnosym": // human: Word + digits, no symbol
		return words[r.Intn(len(words))] + rs(r, aDigits, 4)
	case "pwd8": // short random
		return rsMixed(r, aAlnum, 8)
	case "alnum16":
		return rsMixed(r, aAlnum, 16)
	case "alnum20":
		return rsMixed(r, aAlnum, 20)
	case "alnum32":
		return rsMixed(r, aAlnum, 32)
	case "alnum36":
		return rsMixed(r, aAlnum, 36)
	case "hex32":
		return rs(r, aHex, 32)
	case "hex40":
		return rs(r, aHex, 40)
	case "b64_44":
		return rs(r, aB64, 43) + "="
	case "b64_88":
		return rs(r, aB64, 86) + "=="
	case "uuid":
		return rtUUID(r)
	case "basic": // base64 of user:password
		return base64.StdEncoding.EncodeToString([]byte("svc-user:" + rsMixed(r, aAlnum+"!#$%", 14)))
	case "basicshort":
		return base64.StdEncoding.EncodeToString([]byte("bob:" + rs(r, aLower, 6)))
	case "jwt":
		return "eyJhbGciOiJIUzI1NiJ9" + ".eyJ" + rs(r, aB64u, 50) + "." + rs(r, aB64u, 43)
	case "ghp":
		return "ghp_" + rsMixed(r, aAlnum, 36)
	case "awssec":
		return rsMixed(r, aB64, 40)
	case "digits9":
		return rs(r, aDigits, 9)
	}
	return rs(r, aAlnum, 16)
}

var rtTemplates = []rtTpl{ // subset
	{"conn", "postgres_url", "pwd", `postgresql://app:{S}@db.internal:5432/prod?sslmode=require`},
	{"conn", "mongodb_srv", "pwd", `mongodb+srv://admin:{S}@cluster0.abcde.mongodb.net/test?retryWrites=true&w=majority`},
	{"conn", "sentry_dsn_style", "hex32", `https://{S}@o123456.ingest.sentry.io/7654321`},
	{"header", "authorization_basic", "basic", `Authorization: Basic {S}`},
	{"header", "x_api_key", "alnum32", `X-Api-Key: {S}`},
	{"header", "cookie_session", "alnum32", `Cookie: _ga=GA1.2.123; session={S}; theme=dark`},
	{"query", "access_token_query", "alnum32", `https://api.example.com/v1/me?access_token={S}`},
	{"cmd", "curl_u", "pwd", `curl -u admin:{S} https://api.example.com/v1/items`},
	{"cmd", "mysql_pSECRET", "pwd", `mysql -uroot -p{S} -h db shop`},
	{"cmd", "pgpassword_env", "pwd", `PGPASSWORD={S} psql -h db -U app -d prod`},
	{"cmd", "docker_login_p", "pwd", `docker login -u bob -p {S} registry.example.com`},
	{"sql", "create_user_with_password", "pwd", `CREATE USER bob WITH PASSWORD '{S}';`},
	{"file", "env_file_named", "pwd", "APP_ENV=prod\nDB_HOST=db.internal\nDB_PASSWORD={S}\nLOG_LEVEL=info\n"},
	{"file", "env_file_camel_dbPassword", "pwd", "dbHost=db.internal\ndbPassword={S}\n"},
	{"file", "netrc", "pwd", "machine api.example.com login bob password {S}\n"},
	{"file", "kubeconfig_token_opaque", "alnum36", "users:\n- name: admin\n  user:\n    token: {S}\n"},
	{"file", "xml_password_element", "pwd", "<server><id>nexus</id><username>deploy</username><password>{S}</password></server>"},
	{"file", "yaml_password_colon", "pwd", "database:\n  host: db\n  password: {S}\n"},
	{"file", "json_password", "pwd", `{"user":"bob","password":"{S}"}`},
	{"file", "python_environ", "pwd", `os.environ["DB_PASSWORD"] = "{S}"`},
}

// rtLCS is the length and end index (in out) of the longest common substring of core and out.
func rtLCS(core, out string) (int, int) {
	best, end := 0, 0
	prev := make([]int, len(out)+1)
	cur := make([]int, len(out)+1)
	for i := 1; i <= len(core); i++ {
		for j := 1; j <= len(out); j++ {
			if core[i-1] == out[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best, end = cur[j], j
				}
			} else {
				cur[j] = 0
			}
		}
		prev, cur = cur, prev
	}
	return best, end
}

// rtOutcome classifies out against the secret core: SURVIVES (all of it), PARTIAL (12 or more consecutive characters), REDACTED.
func rtOutcome(out, core string) (string, int) {
	if core == "" {
		return "n/a", 0
	}
	if strings.Contains(out, core) {
		return "SURVIVES", len(core)
	}
	best, _ := rtLCS(core, out)
	min := 12
	if len(core) < min {
		min = len(core)
	}
	if best >= min {
		if len(core) < 12 {
			return "SURVIVES", best
		}
		return "PARTIAL", best
	}
	return "REDACTED", best
}

// rtElide hides the surviving parts of the secret in out, so that a report can quote the observed output.
func rtElide(out, core string) string {
	for i := 0; i < 6; i++ {
		best, end := rtLCS(core, out)
		if best < 6 {
			break
		}
		out = out[:end-best] + fmt.Sprintf("<%dc>", best) + out[end:]
	}
	return out
}
```
### A.3 repro tests of RT-01 to RT-04 and RT-08
```go
// RED TEAM REPRO (temporary; the appendix of the report carries it): RT-01 quoted keys, RT-02 compound names, RT-03 wrapped keys.
// Every secret is drawn at run time; the file holds no secret-shaped literal.


func rtSecretsFor(r *rand.Rand) (pw, long string) {
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	pick := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alnum[r.Intn(len(alnum))]
		}
		return string(b)
	}
	return "Xq7" + pick(9), "Zk4" + pick(29)
}

// cases: {name, template with %P (12 characters) or %L (32 characters)}
var rtReproQuoted = [][2]string{
	{"json password", `{"user":"bob","password": "%P"}`},
	{"json client_secret", `{"client_secret":"%L"}`},
	{"json apiKey", `{"apiKey": "%L"}`},
	{"json token", `{"token":"%L"}`},
	{"python dict", `config = {'host': 'db', 'password': '%P'}`},
	{"python environ", `os.environ["DB_PASSWORD"] = "%P"`},
	{"python index", `config['password'] = '%P'`},
	{"php define", `define('DB_PASSWORD', '%P');`},
	{"java setProperty", `props.setProperty("password", "%P");`},
	{"toml quoted key", `"password" = "%P"`},
}

var rtReproCompound = [][2]string{
	{"PGPASSWORD env", `PGPASSWORD=%P psql -h db -U app -d prod`},
	{"camel dbPassword", `dbPassword=%P`},
	{"camel apiSecret", `apiSecret=%L`},
	{"camel jwtSecret", `jwtSecret: %L`},
	{"helm postgresPassword", `--set auth.postgresPassword=%P`},
	{"yaml adminPassword", "admin:\n  adminPassword: %P"},
	{"SECRET_KEY_BASE", `SECRET_KEY_BASE=%L`},
}

// must stay as written
var rtReproKeep = []string{
	`MAX_TOKENS=4096`, `max_tokens=4096`, `token_limit: 2000`, `{"role":"viewer","can_edit":false}`, `{"password": "required"}`, `{"password": ""}`,
	`{"password_policy": {"min_length": 12}}`, `The password is required.`, `props.setProperty("password", "required");`, `{"secret": false, "token": null}`,
	`"token_type": "bearer"`, `db.password_min_length = 12`, `keyword=value`, `{"tokens": 5, "keys": 3}`, `["password", "username", "email"]`,
}

func rtFill(tpl string, pw, long string) string {
	return strings.NewReplacer("%P", pw, "%L", long).Replace(tpl)
}

func TestRTReproQuotedKeysAndCompoundNames(t *testing.T) {
	r := rand.New(rand.NewSource(20261103))
	for _, group := range []struct {
		id    string
		cases [][2]string
	}{{"RT-01 quoted key", rtReproQuoted}, {"RT-02 compound name", rtReproCompound}} {
		for _, c := range group.cases {
			pw, long := rtSecretsFor(r)
			text := rtFill(c[1], pw, long)
			out := redactString(text)
			secret := pw
			if strings.Contains(c[1], "%L") {
				secret = long
			}
			if strings.Contains(out, secret) {
				t.Errorf("%s / %s: the secret survives: %q", group.id, c[0], strings.ReplaceAll(out, secret, "<"+group.id+" secret>"))
			}
		}
	}
	for _, keep := range rtReproKeep {
		if out := redactString(keep); out != keep {
			t.Errorf("false positive: %q -> %q", keep, out)
		}
	}
}

// RED TEAM REPRO RT-03 (temporary; Appendix A of the report carries it): a key hard-wrapped at a column whose first line is too short for any rule.


// maxRun is the length of the longest run of secret characters that out still holds.
func maxRun(secret, out string) int {
	best := 0
	for i := range secret {
		for j := i + best + 1; j <= len(secret) && strings.Contains(out, secret[i:j]); j++ {
			best = j - i
		}
	}
	return best
}

func TestRTReproWrappedKeyHeadTooShort(t *testing.T) {
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(20261103))
	pick := func(alphabet string, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = alphabet[r.Intn(len(alphabet))]
		}
		return string(b)
	}
	classes := []struct {
		name, prefix, alphabet string
		n                      int
	}{{"github token (ghp_ + 36)", "ghp_", alnum, 36}, {"openai style (sk- + 48)", "sk-", alnum, 48}, {"aws key id (AKIA + 16)", "AKIA", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", 16},
		{"aws secret key (40 base64)", "", alnum + "+/", 40}, {"stripe live (sk_live_ + 24)", "sk_live_", alnum, 24}}
	for _, width := range []int{60, 64, 72, 76} {
		leaks, cases := 0, 0
		first := ""
		for _, c := range classes {
			for head := 2; head <= 30; head += 2 { // characters of the key that stay on the first line
				body := pick(c.alphabet, c.n)
				key := c.prefix + body
				if head >= len(key) {
					continue
				}
				padding := strings.Repeat("x", 0) + "Please keep this credential for the deployment job, it is the one used by the"
				line1 := strings.TrimRight(padding[:width-head-1], " ") + strings.Repeat(" ", 1)
				for len(line1) < width-head {
					line1 += " "
				}
				text := line1 + key[:head] + "\n" + key[head:] + "\nthanks"
				out := redactString(text)
				cases++
				if run := maxRun(body, out); run >= 12 {
					leaks++
					if first == "" {
						first = fmt.Sprintf("%s wrap %d head %d: %d consecutive secret characters remain", c.name, width, head, run)
					}
				}
			}
		}
		if leaks > 0 {
			t.Errorf("wrap at %d columns: %d of %d wrapped keys keep 12 or more consecutive characters (first: %s)", width, leaks, cases, first)
		}
	}
}

// RED TEAM REPRO RT-04 (temporary; Appendix A carries it): well-known vendor token formats that no rule names. Secrets drawn at run time.


func TestRTReproVendorFormats(t *testing.T) {
	const (
		alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
		url   = alnum + "-_"
		hex   = "0123456789abcdef"
		up    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
		az    = alnum + "_.~-"
		dig   = "0123456789"
	)
	r := rand.New(rand.NewSource(20261103))
	pick := func(a string, n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = a[r.Intn(len(a))]
		}
		return string(b)
	}
	formats := []struct{ name, prefix, body string }{
		{"Google OAuth client secret (GOCSPX- + 28)", "GOCSPX-", pick(url, 28)},
		{"Docker Hub access token (dckr_pat_ + 27)", "dckr_pat_", pick(url, 27)},
		{"Shopify access token (shpat_ + 32 hex)", "shpat_", pick(hex, 32)},
		{"Shopify shared secret (shpss_ + 32 hex)", "shpss_", pick(hex, 32)},
		{"HashiCorp Vault token (hvs. + 24)", "hvs.", pick(url, 24)},
		{"GitLab runner token (glrt- + 20)", "glrt-", pick(url, 20)},
		{"GitLab deploy token (gldt- + 20)", "gldt-", pick(url, 20)},
		{"Square OAuth token (sq0atp- + 22)", "sq0atp-", pick(url, 22)},
		{"New Relic user key (NRAK- + 27)", "NRAK-", pick(up, 27)},
		{"Telegram bot token (10 digits : A + 34)", pick(dig, 10) + ":", "A" + pick(url, 34)},
		{"Discord bot token (M + 23 . 6 . 27)", "", "M" + pick(alnum, 23) + "." + pick(url, 6) + "." + pick(url, 27)},
		{"Azure AD client secret (3 chars 8Q~ + 34)", pick(alnum, 3) + "8Q~", pick(az, 34)},
	}
	for _, f := range formats {
		for _, carrier := range []string{"%s", "Please use %s for the integration.", "{\"value\":\"%s\"}", "VALUE=%s"} {
			text := strings.Replace(carrier, "%s", f.prefix+f.body, 1)
			if out := redactString(text); strings.Contains(out, f.body[len(f.body)/2:]) {
				t.Errorf("%s survives in %q -> %q", f.name, strings.Replace(carrier, "%s", f.prefix+"<body>", 1), strings.Replace(out, f.body, "<body>", 1))
				break
			}
		}
	}
}

// RED TEAM REPRO RT-08 (temporary; Appendix A carries it): command-line, SQL and file forms whose name is not followed by "=" or ":".


func TestRTReproCommandAndSQLForms(t *testing.T) {
	const alnum = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(20261103))
	forms := []struct{ name, tpl string }{
		{"curl -u", `curl -u admin:%S https://api.example.com/v1/items`},
		{"curl --user quoted", `curl --user "admin:%S" https://api.example.com/`},
		{"mysql -pSECRET", `mysql -uroot -p%S -h db shop`},
		{"mysqldump -pSECRET", `mysqldump -h db -u root -p%S shop > shop.sql`},
		{"sshpass -p quoted", `sshpass -p '%S' ssh deploy@10.0.0.5`},
		{"sshpass -p", `sshpass -p %S ssh deploy@10.0.0.5`},
		{"docker login -p", `docker login -u bob -p %S registry.example.com`},
		{"docker login --password", `docker login --username bob --password %S`},
		{"CREATE USER ... PASSWORD", `CREATE USER bob WITH PASSWORD '%S';`},
		{"ALTER ROLE ... PASSWORD", `ALTER ROLE app WITH LOGIN PASSWORD '%S';`},
		{"IDENTIFIED BY quoted", `CREATE USER 'bob'@'%%' IDENTIFIED BY '%S';`},
		{"IDENTIFIED BY bare", `ALTER USER scott IDENTIFIED BY %S;`},
		{".netrc", "machine api.example.com login bob password %S\n"},
		{"redis-cli -a", `redis-cli -h cache -a %S ping`},
		{"mongo -p", `mongo --host db -u admin -p %S --authenticationDatabase admin`},
		{"wget --http-password", `wget --http-user bob --http-password %S https://example.com/f`},
		{"zip -P", `zip -P %S secure.zip docs/*`},
	}
	for _, f := range forms {
		b := make([]byte, 12)
		for i := range b {
			b[i] = alnum[r.Intn(len(alnum))]
		}
		secret := "Xq7" + string(b[:9])
		text := strings.ReplaceAll(f.tpl, "%S", secret)
		text = strings.ReplaceAll(text, "%%", "%")
		if out := redactString(text); strings.Contains(out, secret) {
			t.Errorf("%s: the secret survives: %q", f.name, strings.ReplaceAll(out, secret, "<secret>"))
		}
	}
}
```

## Appendix B - proposed patches (each verified in a scratch copy of the pinned tree, then discarded; the worktree product files are unchanged)

The scratch copies applied them to `internal/ai/redact.go` of `11a4ee4`. Applied together (RT-01/02, RT-03, RT-04, RT-08) in one scratch copy: the whole shipped `gt test -count=1 ./internal/ai` package PASSED (92.8 s on a loaded host) and the four repro tests PASSED. A 40-second fuzz of the four patches together (35,690 executions; determinism, idempotence, output at most 4 x input + 64 bytes, no change without a count) found no failure; that run did not go through `hv` although it lasted over 30 s (my mistake, noted in the final message).

### RT-01 and RT-02 (13 lines)
```diff
diff --git a/internal/ai/redact.go b/internal/ai/redact.go
index 3472a18..933cc6e 100644
--- a/internal/ai/redact.go
+++ b/internal/ai/redact.go
@@ -66,7 +66,7 @@ var redactRules = []redactRule{
 // STRIPE_SECRET_KEY). The "=" form is redacted for any value of four or more characters; the ":" form (ordinary prose such as
 // "Password: required") only when the value looks like a credential; "name is value" (the admin password is ...) only when the
 // value has a digit or a symbol and so is not a word like "required".
-var assignment = regexp.MustCompile(`(?i)\b(?:[a-z0-9]+[_-])*(api[_-]?key|apikey|secret(?:[_-]?(?:access[_-]?)?key)?|client[_-]?secret|access[_-]?token|auth[_-]?token|refresh[_-]?token|token|password|passwd|pwd|passphrase|private[_-]?key|account[_-]?key|(?:shared[_-]?)?access[_-]?key|credentials?)\b(\s*[:=]\s*|\s+(?:is|was|are)\s+)("[^"\n]{1,200}"|'[^'\n]{1,200}'|[^\s,;&"']{1,200})`)
+var assignment = regexp.MustCompile(`(?i)\b[a-z0-9_-]*(api[_-]?key|apikey|secret(?:[_-]?(?:access[_-]?)?key)?(?:[_-]base)?|client[_-]?secret|access[_-]?token|auth[_-]?token|refresh[_-]?token|token|password|passwd|pwd|passphrase|private[_-]?key|account[_-]?key|(?:shared[_-]?)?access[_-]?key|credentials?)\b(["'\x60\]]*\s*[:=]\s*|["']\s*,\s*|\s+(?:is|was|are)\s+)("[^"\n]{1,200}"|'[^'\n]{1,200}'|[^\s,;&"']{1,200})`)
 
 var (
 	longHex    = regexp.MustCompile(`\b[0-9A-Fa-f]{32,}\b`)
```

### RT-03 (79 lines)
```diff
diff --git a/internal/ai/redact.go b/internal/ai/redact.go
index 3472a18..c539a12 100644
--- a/internal/ai/redact.go
+++ b/internal/ai/redact.go
@@ -333,7 +333,73 @@ func redactPlain(s string) (string, int) {
 		}
 		return out
 	})
-	return redactWrappedTails(s, &n), n
+	return redactWrappedTails(redactSplitKeys(s, &n), &n), n
+}
+
+// keyPart reports whether w is made of the characters of a key and is not empty.
+func keyPart(w string) bool {
+	return w != "" && strings.IndexFunc(w, func(c rune) bool {
+		return !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.ContainsRune("+/_=-.", c))
+	}) < 0
+}
+
+// wholeToken returns the class of w when all of it is one secret: a prefix rule, a hex run or, for a wrapped paragraph, a long entropic run.
+func wholeToken(w string, wrapped bool) (string, bool) {
+	var n int
+	out := w
+	for _, r := range redactRules {
+		out = r.apply(out, &n)
+	}
+	if class, ok := strings.CutPrefix(out, redactionPrefix); ok && n > 0 && strings.HasSuffix(class, "]") && !strings.ContainsAny(class[:len(class)-1], "[]") {
+		return class[:len(class)-1], true
+	}
+	switch {
+	case longHex.FindString(w) == w:
+		return "hex", true
+	case wrapped && longBase64.FindString(w) == w && base64Replace(w) != w:
+		return "token", true
+	}
+	return "", false
+}
+
+// redactSplitKeys redacts a key that a line break cut before its first part was long enough for its rule: the last word of a line plus
+// the next lines that hold one key-shaped word each (three at most) is read as one word. A run without a prefix needs a first line of 48+ columns.
+func redactSplitKeys(s string, n *int) string {
+	if !strings.Contains(s, "\n") {
+		return s
+	}
+	lines := strings.Split(s, "\n")
+	changed := false
+	for i := 0; i+1 < len(lines); i++ {
+		head := strings.TrimRight(lines[i], " \t\r")
+		cut := strings.LastIndexAny(head, " \t") + 1
+		if len(head)-cut < 2 || !keyPart(head[cut:]) {
+			continue
+		}
+		joined := head[cut:]
+		for j := i + 1; j < len(lines) && j <= i+3; j++ {
+			tail := strings.TrimSpace(lines[j])
+			if !keyPart(tail) {
+				break
+			}
+			if joined += tail; len(joined) < 19 {
+				continue
+			}
+			if class, ok := wholeToken(joined, len(head) >= 48); ok {
+				lines[i] = head[:cut] + marker(class)
+				for k := i + 1; k <= j; k++ {
+					lines[k] = lines[k][:len(lines[k])-len(strings.TrimLeft(lines[k], " \t"))] + marker(class)
+				}
+				*n += j - i + 1
+				changed, i = true, j
+				break
+			}
+		}
+	}
+	if !changed {
+		return s
+	}
+	return strings.Join(lines, "\n")
 }
 
 // redactStructural is the part of the redactor that also applies to identifier-shaped
```

### RT-04 (11 lines)
```diff
--- a/internal/ai/redact.go
+++ b/internal/ai/redact.go	2026-11-03 06:33:17.125450665 +0000
@@ -47,7 +47,7 @@
 	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]+-----.*?(?:-----END [A-Z0-9 ]+-----|$)`), nil},
 	{"url_credentials", regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.\-]*://[^\s/?#@"'<>\\]*@[^\s"'<>\\]+`), nil},
 	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`), nil},
-	{"api_key", regexp.MustCompile(`\b(?:(?:sk|rk|pk)-[A-Za-z0-9_-]{16,}|(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{10,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|xapp-[0-9]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{30,}|ya29\.[0-9A-Za-z_-]{20,}|glpat-[A-Za-z0-9_-]{16,}|npm_[A-Za-z0-9]{30,}|SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}|hf_[A-Za-z0-9]{30,}|api_org_[A-Za-z0-9]{30,}|whsec_[A-Za-z0-9]{16,})`), nil},
+	{"api_key", regexp.MustCompile(`\b(?:(?:sk|rk|pk)-[A-Za-z0-9_-]{16,}|(?:sk|rk|pk)_(?:live|test)_[A-Za-z0-9]{10,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|xox[abprs]-[A-Za-z0-9-]{10,}|xapp-[0-9]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_-]{30,}|ya29\.[0-9A-Za-z_-]{20,}|glpat-[A-Za-z0-9_-]{16,}|npm_[A-Za-z0-9]{30,}|SG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}|hf_[A-Za-z0-9]{30,}|api_org_[A-Za-z0-9]{30,}|whsec_[A-Za-z0-9]{16,}|GOCSPX-[A-Za-z0-9_-]{24,}|dckr_pat_[A-Za-z0-9_-]{20,}|shp(?:at|ca|pa|ss)_[A-Fa-f0-9]{32}|hvs\.[A-Za-z0-9_-]{20,}|gl(?:rt|dt|ptt|ft|imt|oas|soat|cbt)-[A-Za-z0-9_-]{16,}|sq0(?:atp|csp)-[A-Za-z0-9_-]{22,}|NRAK-[A-Z0-9]{27}|\d{8,10}:A[A-Za-z0-9_-]{34}|[MNO][A-Za-z0-9_-]{23,25}\.[A-Za-z0-9_-]{6}\.[A-Za-z0-9_-]{27,38}|[A-Za-z0-9_.-]{3}\dQ~[A-Za-z0-9_.~-]{31,34})`), nil},
 	{"aws_key_id", regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|ANPA|ANVA|AIPA|ABIA|ACCA)[0-9A-Z]{16}\b`), nil},
 	{"bearer", regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`), nil},
 	{"authorization", regexp.MustCompile(`(?i)\bauthorization\s*[:=]\s*(?:bearer\s+|basic\s+|token\s+)?[^\s,;"']{8,}`), nil},
```

### RT-08 (12 lines)
```diff
--- a/internal/ai/redact.go
+++ b/internal/ai/redact.go	2026-11-03 06:37:15.921941559 +0000
@@ -60,6 +60,9 @@
 	{"cookie", regexp.MustCompile(`(?i)\b(?:set-)?cookie[ \t]*:[^\r\n]*?[A-Za-z0-9_.%-]+=[^\s;]{8,}[^\r\n]*`), nil},
 	// A payment card number: 13 to 19 digits, alone or in groups, with a known issuer prefix and a valid check digit.
 	{"card", regexp.MustCompile(`\b\d(?:[ -]?\d){12,18}\b`), cardNumber},
+	// A password given as an argument or as a quoted literal after the word: curl -u user:SECRET, mysql -pSECRET, sshpass -p SECRET, docker login -p SECRET,
+	// IDENTIFIED BY SECRET, a .netrc "password SECRET", SQL PASSWORD 'SECRET'.
+	{"password_arg", regexp.MustCompile(`(?i)(?:(?:\bcurl\b[^\n]*?\s(?:-u|--user)\s+["']?[^\s:"']+:|\b(?:sshpass|redis-cli|mongo(?:sh)?|docker\s+login|zip)\b[^\n]*?\s(?:-p|-a|--password)\s*|\bwget\b[^\n]*?--(?:http-|ftp-)?password[\s=]+|\bmysql(?:dump|admin)?\b[^\n]*?\s-p|\bidentified\s+by\s+|\bmachine\b[^\n]*?\bpassword\s+)["']?[^\s"';]{4,}["']?|\bpassword\s+["'][^"'\n]{4,200}["'])`), nil},
 }
 
 // assignment finds "name = value" pairs with a secret-like name, also behind other words of a variable name (DB_PASSWORD,
```
