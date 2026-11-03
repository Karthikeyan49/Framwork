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
| next | mutation table (90 mutations running), 5-minute fuzz under `hv`, the smaller Part A findings, appendix, verdict |

## Findings

| id | severity | one line |
|---|---|---|
| RT-01 | MEDIUM | a quoted key defeats the `name = value` rule: `"password": "..."`, `'password': '...'`, `os.environ["DB_PASSWORD"] = "..."`, `define('DB_PASSWORD', '...')` survive in full |
| RT-02 | MEDIUM | compound names without `_` or `-` are not names: `PGPASSWORD=...`, `dbPassword=...`, `apiSecret=...`, `postgresPassword: ...`, `SECRET_KEY_BASE=...` survive in full |
| RT-03 | MEDIUM | a key hard-wrapped so that its first line is shorter than the minimum of its rule leaves both halves: 44 of 69 wrapped keys keep 12 or more consecutive characters at each of the widths 60, 64, 72, 76 |

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
| 4 | namespace, app id, schema version | `application` | `redactString(clip())` (context.go:179-180) | code read; mutations M72-M73 |
| 5 | release-diff ids, kinds, aspects | `release_diff` | context.go:191-193 | code read; mutation M77 |
| 6 | tool catalog summary | `tools` | `cleanTool` (context.go:283-296); operator-authored, no endpoint or credential field exists in `ToolSummary` | code read; mutation M74 |
| 7 | role names | `roles` | context.go:219 | code read; mutation M75 |
| 8 | requirement ids, categories, statuses | `requirements` | context.go:239; statements and semantics are not sent at all | code read; mutation M76 |
| 9 | repair problems | `diagnostics` segment of attempts 2 and 3 | `capDiagnostics` (repair.go:295-307): `redactString(clip())` on path, code, message | PASS: a model answer with an unknown key shaped like a token produced `{"path":"","code":"unknown_field","message":"a key is not part of the closed schema"}`, the key never reaches the next prompt; mutations M78-M80 |
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
