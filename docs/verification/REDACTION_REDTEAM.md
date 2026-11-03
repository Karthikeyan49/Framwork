# Independent red team of the prompt redactor (`internal/ai/redact*.go`)

Branch `review/redaction-redteam`, pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`). Scope: `internal/ai/redact.go`, `redact_fold.go`,
every field that reaches a prompt (`context.go`, `repair.go`, `template.go`) and every path that carries author text to a model, a stored row, an audit row, a log
or a response. Written by an agent that did not write the redactor and changed no product code, test or ADR. Probes and mutations lived only in the worktree and were
reverted or deleted before each commit. Every secret-shaped input was generated at run time; this file names shapes (class, prefix, length, alphabet), never a value.
Verdicts are PASS / FAIL / NOT RUN only.

## Progress (kept current)

| state | detail |
|---|---|
| done | brief and code read; Part A matrix (about 3,000 cells x 6 samples); RT-01 and RT-02 minimised, patched, verified; Part C integration probes on the `fix` stack; false-positive corpus (389 inputs); timing at 4 KB to 1 MiB; 5 `holdsRedaction` bypass variants |
| interrupted | the run was cut by the account limit once and resumed; nothing was lost (the report and the branch are the record) |
| next | wrapped-key finding (RT-03) and its patch; mutations (90 planned); 5-minute fuzz under `hv`; Part C path table; Part B tables |

## Findings

| id | severity | one line |
|---|---|---|
| RT-01 | MEDIUM | a quoted key defeats the `name = value` rule: `"password": "..."`, `'password': '...'`, `os.environ["DB_PASSWORD"] = "..."`, `define('DB_PASSWORD', '...')` survive in full |
| RT-02 | MEDIUM | compound names without `_` or `-` are not names: `PGPASSWORD=...`, `dbPassword=...`, `apiSecret=...`, `postgresPassword: ...`, `SECRET_KEY_BASE=...` survive in full |

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

