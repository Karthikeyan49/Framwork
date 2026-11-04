# Independent review of the follow-up to the review of the takeover fixes

Reviewer worktree: a detached checkout of the shared object store at `f33a0b5` (branch `integration/m9r-m14`), then the branch `review/takeover-followup`. Range under review:
`ad80853..f33a0b5`, 15 commits: `f485724 c54dd61 8451090 eb4b51b aa3ee70 b9ac1ee 040ad44 e5afb07 949da13 0b1407e f69dcad 83738a2 d98eb3d 364c7a4 f33a0b5` (the code commits
are `aa3ee70 83738a2 b9ac1ee 040ad44 949da13 e5afb07 0b1407e d98eb3d f485724 c54dd61`; the rest are documents and the merge of the previous review). The previous review
([TAKEOVER_FIXES_INDEPENDENT_REVIEW.md](TAKEOVER_FIXES_INDEPENDENT_REVIEW.md), pinned at `0155f7b`, TF-01 to TF-12) was read first. Written by a reviewer that did not write
the code or the documents it judges and changed no product code, test or ADR of the tree: every probe lived in this worktree (`internal/**/zz_review_*_test.go`, and two scratch
packages, `internal/zzold` = `11a4ee4:internal/ai/redact.go` + `redact_fold.go` and `internal/zzprev` = the same files at `0155f7b`, each with `package` renamed and a
`RedactText` wrapper) and was deleted before this file was committed; `git status` showed this file alone. Result words: PASS / PASS WITH FINDINGS / FAIL / NOT RUN. Every
"held" names the attack that was executed. Every secret-shaped input was generated at run time from a label (the tests' own helpers `alnum`, `luhnNumber`, `groups`, `material`,
`fullwidth`, `hexOf`); this file names shapes, never a value, and writes no escape sequence of a surrogate literally. Nothing of any `.env` was printed or committed (the copy that
the integration helper needs was deleted with the probes). `/home/user/integ`, the other worktrees, `main` and the development database `MAIN` were not touched; nothing was pushed.

Use of the database: only through `RESET=0 /home/user/bin/it-run m14 <this worktree> <log> '<regex>'` on the isolated stack: once with the two names that the brief allows
(`TestM14APIPolicyMatrix|TestM14APIAskingAndReadingNeedMetadataRead`: PASS), once with the same two names against the parent versions of two production files (FAIL as intended), and
four more runs with the first name only, one per member of the TF-02 negative control. The TS-01 test was compiled and run without its variable (it skips before any database access).
Network: read-only GETs of `go.dev/dl/?mode=json`, `vuln.go.dev/ID/GO-2026-6809.json` and `vuln.go.dev/index/modules.json` (section E). The host was restarted after the probes had
run, and the backups of the probe sources survived it; the key probes were run again after it (section "Reproduced after the reboot").

## Verdict

**PASS WITH FINDINGS. No HIGH finding. One MEDIUM (FU-01) and eight LOW (FU-02 to FU-09).** Every finding of the previous review that was fixed is fixed for what it named: the ten
sentences of TF-01 and the product's own repair template are kept; passphrase-shaped values (TF-04) are redacted in all eleven connectors tried; the card scan equals an
independent reference on 1.5 million random texts and no digit of a window stays (TF-06); the apply, confirm and reject answers hold no body, proven on the database with four
independent negative controls (TF-02); the surrogate scanner agrees with `encoding/json` on 400,000 random texts and each of its four call sites has a test that fails without it
(TF-07); the OpenTelemetry exporters no longer print `OTEL_EXPORTER_OTLP_*` values (TF-08). The follow-up repeats the pattern of the last round once: the statement scan that
replaced the broad `password_arg` rule reads SQL through closed lists and a 1,000-byte window, so passwords in forms that the table of `0155f7b` redacted (PostgreSQL role DDL with
`CONNECTION LIMIT`, `IN ROLE` or `VALID UNTIL` before `PASSWORD`, MariaDB `CREATE OR REPLACE USER`, a statement longer than about 1 KB, a `;` inside a quoted PostgreSQL password)
reach the model in full, and the residual list of ADR104 item 15 a does not say so (FU-01). The narrower rules also still destroy some ordinary text, two classes of it new with
this range (FU-02).

| # | item | result | one line |
|---|---|---|---|
| A.1 | ordinary text, diffed against `11a4ee4` | PASS WITH FINDINGS | my corpus of 264 texts (and a third list of 30 that no table changes): changed by 11a4ee4 10, by 0155f7b 97, now 48, of which 43 were kept by 11a4ee4 (7 of the 215 realistic ones); 32,843 lines of the repository's own prose: 2 new, both code; 12 texts are new against both older tables and 5 more against 0155f7b (FU-02) |
| A.2 | forms still redacted, near forms | PASS WITH FINDINGS | 126 forms: 20 that 0155f7b leaked are redacted now, 5 that it redacted leak now, plus a bulk statement, a `;` in a quoted password, a clause cut by the 1,000 bytes (FU-01); every output was valid UTF-8 at the span cut |
| A.3 | the closed `proseSpecification` | PASS | 18 families of credential-shaped values x 400 x 11 connectors: redacted except values that are spelled like a list entry or end in `!`; the red team's seven sentences kept, nine more format sentences destroyed than at 0155f7b (FU-02) |
| A.4 | card union | PASS WITH FINDINGS | exact against my own reference on 1.5 million random texts, linear to 1 MiB; a chain of short groups loses about one group in ten, a counter run most of it (FU-04) |
| A.5 | cross-cutting | PASS | 300,000 texts: 0 not a fixed point, 0 non-deterministic, 0 invalid UTF-8, 0 panics; worst cost 4.9 us/byte (5.2 in a second run), linear; at most 126 bytes allocated per input byte |
| B | TF-02 | PASS WITH FINDINGS | both named integration tests pass; four negative controls on the database each fail; what remains is ids and one model token (FU-06) |
| C | TF-07 | PASS WITH FINDINGS | scanner == encoding/json on 400,000 texts, linear to 4 MiB, legitimate answers pass; the author-facing decoders, the AI request body and the sign-in body still substitute (FU-05) |
| D | TF-08 | PASS WITH FINDINGS | of 33 malformed `OTEL_*` settings 30 print nothing and 31 do not print the value, none of the OTLP ones leak; the sampler, the span-limit variables and the resource attributes still write to standard error (FU-03) |
| E | small items and documents | PASS WITH FINDINGS | Jittered, clamp, chmod, `rm -rf`, TS-01 gate, RC-04 hold; the SHA-256 and the advisories are verified online; seven statements of the documents are not backed (FU-07) |
| F | negative controls | PASS WITH FINDINGS | 22 controls run; the tests detect what they claim except the chmod order, the finite RC-04 list, one case of the catalog test and `OfAStatementKeepsItsName` (FU-08); the TS-01 test cannot pass under one fix it names (FU-09) |

What each commit claims, and what held:

| commit | claim | result |
|---|---|---|
| `aa3ee70` | three password rules with their context, closed format list, `:=` `=>` `==` | the review's sentences kept, 5 of 6 new tests fail on the old rule; FU-01, FU-02 |
| `83738a2` | a `.netrc` default entry begins its line | the sentence of its message is kept; a line that begins with "Default login USER password WORD" is destroyed (FU-02) |
| `b9ac1ee` | the card scan redacts the union of overlapping windows | exact; both TF-06 tests fail on the first-window loop; FU-04 |
| `040ad44` | apply and confirm answers hold no body | holds on the database; FU-06 |
| `949da13` | refuse a lone surrogate escape (tools, catalog, envelope, payload) | holds; FU-05 |
| `e5afb07` | the OpenTelemetry libraries cannot write raw `OTEL_EXPORTER_OTLP_*` values | holds for that family; FU-03 |
| `0b1407e`, `d98eb3d` | jitter, chmod before append, clean unpack, gated TS-01 test, `Jittered` guard | hold; FU-08, FU-09 |
| `f485724`, `c54dd61` | RC-04 test, clamp of `aiResultPause` | hold; FU-08 |
| documents | hashes, counts, test names | 29 of 29 hashes are commits; FU-07 |

## Findings

Ranked by severity. File and line are of `f33a0b5`.

### FU-01 (MEDIUM) the statement scan reads SQL through closed lists and a 1,000-byte window: passwords that the table of `0155f7b` redacted reach the model

`internal/ai/redact.go:79` (`sqlUserHead`: `create|alter user|role|group|login` and `grant`; no `OR REPLACE`), `:86-87` (`sqlRolePassword`: only the words `with`, `login`, `nologin`,
`(un)encrypted`, `(no)superuser|createdb|createrole|createuser|inherit|replication|bypassrls` and a name without a blank may precede `PASSWORD`), `:75` and `:111-118` (a statement is
its head plus at most 1,000 bytes), `:115` (the first `;` ends it, quoted or not).

Executed (probes `TestZZCorpusForms`, `TestZZRolePasswordOptions`, `TestZZSemicolonInQuotes`, `TestZZManyUsersLongStatement`, `TestZZSpanBoundary`, `TestZZIdentifiedByCases`; the
password is 12 alphanumerics made at run time, "leak" means that its 12 characters are in the output; the three table columns are `11a4ee4` / `0155f7b` / now):

| input shape | 11a4ee4 | 0155f7b | now |
|---|---|---|---|
| `CREATE ROLE app WITH LOGIN CONNECTION LIMIT 5 PASSWORD '<S>';` (the same with `CREATE USER`, `ALTER ROLE`, `ALTER USER`, `CREATE GROUP`, and with `IN ROLE readers`, `VALID UNTIL 'infinity'`, `ROLE a`, `ADMIN b`, `SYSID 100` before `PASSWORD`: 35 of 100 statements) | leak | ok | **leak** |
| `CREATE ROLE "app user" WITH LOGIN PASSWORD '<S>';` | leak | ok | **leak** |
| `CREATE OR REPLACE USER bob IDENTIFIED BY '<S>';` (MariaDB) | leak | ok | **leak** |
| `CREATE ROLE app LOGIN PASSWORD 'ab;<S>';` | leak | ok | **leak** |
| one `CREATE USER` of 60 users, `'userNN'@'%' IDENTIFIED BY '<S>'` each, 2,591 bytes: secrets visible | 60 | 0 | **34** (users 26 to 59; the span ended at about byte 1,000) |
| `CREATE USER bob` + N blanks + `IDENTIFIED BY '<S>';` (`TestZZCutClassification`) | leak | ok | whole up to N = 969; **partly for N = 970 to 976** (the marker is written and a suffix of the secret, 1 to 7 of its 12 characters, stays next to it: `[REDACTED:password_arg]` + tail); **the whole secret from N = 977** (the cut fragment is taken only if it holds a digit or a symbol; the bare form: 970, 971 to 977, 978) |
| `CREATE USER bob IDENTIFIED BY <letters only>;` and `ALTER USER bob IDENTIFIED BY <letters only>;` (three words tried) | leak | ok | leak (a documented residual of item 15 a; Oracle writes passwords bare) |
| `CREATE USER bob IDENTIFIED BY 'ab;<S>';`, the same with `ALTER USER` and `GRANT ... IDENTIFIED BY` | leak | leak | leak |

Also open in all three tables (not regressions): `SET PASSWORD FOR 'u'@'h' = PASSWORD('<S>')` and `= '<S>'`, the old password after `REPLACE` (MySQL and Oracle), PostgreSQL `PASSWORD $$<S>$$` and
`E'<S>'`, a `curl` whose `-u USER:<S>` is on the line after a backslash, `--proxy-user`, `unzip -P`, `7z -p`, `sqlcmd -P`, `htpasswd -b`, `net use`, `openssl -passin pass:` and `SSHPASS=`
with `sshpass -e`. What held (see "Attacks that held"): spaces, case, CRLF, tabs, single, double and back quotes, several users in one short statement, `IDENTIFIED WITH plugin BY`,
`AS '<40 hex>'` (the hex rule takes the hash), `ENCRYPTED PASSWORD`, `ALTER USER ... WITH PASSWORD`, `GRANT ... IDENTIFIED BY`, the flag variants of curl, wget, sshpass, docker login,
mongo and mongosh, az login, gpg, ldapsearch and redis-cli; `--password-stdin` is kept.

Impact: pasted DDL that the table of `0155f7b` redacted stays in the request that is sent to the provider, in the shapes above. The practical odds differ (PostgreSQL's usual order,
`LOGIN PASSWORD ... CONNECTION LIMIT`, is covered; `CONNECTION LIMIT` or `VALID UNTIL` before the password, a MariaDB script, a bulk statement and a generated password that contains `;`
are less common but ordinary), and the ADR (item 15 a, "the quoted `PASSWORD` of a role that only the option words of `CREATE ROLE` precede", "ends at `;` or after 1,000 bytes") states
the closed list and the bound but not that the list is a subset of PostgreSQL's options, that `OR REPLACE` is missing, or what the bound and the `;` do to a secret. It is the kind of
defect that TF-04 was last round: a credential filter narrowed to save ordinary text, in a form that was redacted before. No secret was exposed by the probes.
Fix direction: inside a span that starts with `CREATE|ALTER (OR REPLACE)? USER|ROLE|GROUP|LOGIN`, accept any tokens up to the end of the statement between the name and `PASSWORD`
(`[^;]*?`), end the statement at a `;` that is outside quotes, read to the end of the statement (or to a bound of tens of kilobytes) instead of 1,000 bytes, and never cut a clause:
extend the span to the end of the clause that starts inside it. Add each option word to the near-miss list and the 60-user statement to `redact_prose_test.go`.

### FU-02 (LOW) the narrower rules still destroy ordinary text in contexts of their own, and two classes of it are new against both older tables

Executed (probes `TestZZCorpusKeep`, `TestZZListOldChanged`, `TestZZNewRegressionsOfTheFollowUp`, `TestZZDocsCorpus`, `TestZZProseSpecificationSentences`, `TestZZEndToEndLabels`,
`TestZZTF01Sentences`). My corpus has two lists written by me, not taken from the tests: list 1, 215 realistic requests, labels and descriptions that use `identified by`, `machine`,
password fields and flows, token, secret and key words, SSO, `zip`, `redis-cli`, `mysql`, `mongo`, `docker login`, `curl`, grants and `create user|role` in prose; list 2, 49 near misses
written around the new rules.

| list | texts | changed by 11a4ee4 | changed by 0155f7b | changed now | destroyed now and kept by 11a4ee4 |
|---|---|---|---|---|---|
| 1 (realistic) | 215 | 2 (the RT-05 sentences) | 70 | 7 | 7 |
| 2 (near misses) | 49 | 8 | 27 | 41 | 36 |
| 3 (`passwd`, `pwd`, login forms, token expiry, hints, `netrc`, plain `zip` / `redis-cli` / `docker login` / `mongo`; `TestZZListThree`, run once, after the reboot) | 30 | 0 | 0 | 0 | 0 |
| the prose of 324 documents of the repository (not the redactor's own reports), line by line | 32,843 lines | 857 | 859 | 859 | 2 (both code lines) |

The ten sentences of TF-01 and the repair template of `internal/ai/template.go` ("the proposal identified by previous_proposal_hash") are kept: **TF-01 is fixed as filed** (66 of the 70
texts of list 1 that 0155f7b destroyed are kept now). The seven of list 1 that remain: three of the shape `machine <host with dots> password <word>` (`redact.go:71`, the `.netrc` entry
without a login: "Each machine web01.corp.example.com password rotates every 30 days"), one `Create user roles with password 'hint' column.` (the role rule of the statement scan), and
three lines that begin with `Default login <user> password <word>` ("Default login admin password rotation is monthly.", `redact.go:70`, `(?m:^)[ \t]*default`). Through
`BuildContext` such a label or request shows `[REDACTED:password_arg]` in the request and the application segment (`TestZZEndToEndLabels`: 5 of 10 labels, those of the remaining
classes; the other five, which are TF-01 shapes, are kept), and a definition that holds the placeholder cannot be updated with the text the model
was shown (`holdsRedaction`, `patch.go`; shown by the previous review, not executed again here).
Destroyed now and **kept by 0155f7b** (17 texts of the two lists, `TestZZNewRegressionsOfTheFollowUp`). Twelve of them were kept by `11a4ee4` as well, so they are new against both
older tables: 7 lines that begin with `Default login ...` (the follow-up `83738a2` fixed the sentence of its message and left the line start) and 5 comparisons or assignments with a
quoted literal, the operators that `aa3ee70` added (`if password == "admin" then ...`, `Warn when token == "expired"`, `credentials == "missing"`, `password := "required"`,
`password => "hidden"`). Five are format sentences outside the closed list that 11a4ee4 destroyed too (`x509-style`, `8-or-more characters`, `1-time`, `oauth2-bearer style`,
`ed25519-signed`): of 78 format sentences I tried 11a4ee4 changed 45, 0155f7b 5 and the table now 13, nine of the 13 having been kept by 0155f7b (the four that were not: `HMAC-SHA256`
twice, `TLS1.3-protected`, `v4.public`); the red team's seven are kept. The rest of list 2 is the documented ambiguity of a command or statement in prose (`curl -u username:password`,
`sshpass -p option`, `mongo -pretty`, `identified by 'email'` after `create user`).
Impact: LOW. The words of a request are replaced by a marker in contexts that an application builder writes now and then; no secret is exposed. Fix direction: for the `.netrc`
rules require the value to be credential-shaped (a digit or a symbol) as the assignment rule does, or the line to hold nothing else; for the literal operators require the right-hand
side to look like a credential; add these sentences to `redact_prose_test.go`.

### FU-03 (LOW) TF-08 is closed for the OTLP variables and open for three other families of malformed `OTEL_*` settings

`internal/observability/observability.go:31` (the logger is installed first, correct), `:51` (`trace.NewTracerProvider`) and `:56` (`otel.SetErrorHandler`, after it).
Executed (probe `TestZZOtelMatrix`: 33 malformed settings, each in a child process that calls `Start(..., "http://127.0.0.1:1", logger)`; the value is a hash of a label): 30 leave
standard error empty and 31 do not print the value, among them `OTEL_EXPORTER_OTLP_HEADERS`, `_ENDPOINT` and the traces and metrics variants of both, the certificate and key paths,
the compression, timeout and protocol settings, `OTEL_BSP_*`, `OTEL_METRIC_EXPORT_*`; the settings that the libraries reject through their logger (headers, endpoints, certificate
paths, timeouts, the metric interval, the temporality) each produce exactly one fixed warning on standard output. Two print the raw value and a third prints the malformed entry:

```
OTEL_TRACES_SAMPLER=<value>              stderr: "<date> unsupported sampler: <value>"
OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT=<value>  stderr: "<date> WARN invalid limit environment variable error=\"strconv.Atoi: parsing \\\"<value>\\\"...\" key=... value=<value>"
OTEL_RESOURCE_ATTRIBUTES=<entry>,broken  stderr: three lines "partial resource: missing value: [broken]" (the entry without a value)
```

Causes, by reading the modules: the SDK reports the sampler and the resource through `otel.Handle` (`sdk@v1.47.0/trace/provider.go:536-538`, `resource/resource.go:269,291`,
`env.go:86`); until `SetErrorHandler` runs the default delegator does `log.Print(err)` (`otel@v1.47.0/internal/errorhandler/errorhandler.go:36-42`), and `Start` installs it after the
provider is built. The six span-limit variables are read by `go.opentelemetry.io/auto/sdk@v1.2.1/limit.go:13` (`var maxSpan = newSpanLimits()`, a package initializer) and a
malformed one is written by `slog.Warn` on the default logger (`:85-90`) before `main` runs, so nothing in `Start` can reach it; every binary that imports `go.opentelemetry.io/otel`
has it. Impact: LOW (operator-supplied, start-up only, not reachable by a caller, and none of these variables normally holds a credential); but the sentence of the handoff "the
OpenTelemetry libraries log through slog with one fixed warning" is true for the OTLP family only. Fix direction: call `otel.SetErrorHandler` (and `slog.SetDefault(logger)`) at the
top of `Start`, next to `SetLogger`; say in ADR104 item 15 f that the init-time variables are the launcher's to validate (`scripts/runtime-env.mjs`). The sink itself holds: 64
goroutines x 2,000 calls under `-race` gave one warning and no race.

### FU-04 (LOW) the card union loses far more of a numeric list than "can lose a number"

`internal/ai/redact.go:329-368`, ADR104 item 9. The scan is exact (A.4) and the rule is as documented; its effect on a chain of short groups, measured (probe `TestZZCardChains`):
uniform random chains of N four-digit groups separated by single spaces lose 4.0% of the groups at N = 5 and **9.7 to 9.8% at N = 50 to 1,000** (worst chain: 152 of 1,000 groups in 38
markers); counters lose most of their groups because a window that starts with 4 or 5 passes the Luhn check far more often than by chance (`4000 4001 ... 4399`: 284 of 400 groups,
71%, in 64 markers; `5100 ...`: 70%; 3,000 counters from 1000: 20%); a crafted chain of 400 groups (2,021 bytes) in which every window from the fifth group on is valid collapses into two markers (the first card and the other 396 groups).
The ADR says "a long list of short numbers separated by single spaces or hyphens can lose a number to the marker (accepted: the cheaper error for a safety net)". The stated chance rates
hold within sampling error (mine: 1 in 39 windows of 16 digits at 2 million samples, exact 1 in 38.7; 1 in 390 of 14, exact 1 in 385; 1 in 100 on average). Impact: LOW (no secret; a
pasted table of numbers loses about a tenth of its cells). Fix direction: say so in the ADR, or merge only windows that overlap by at least one group and cap the merged span.

### FU-05 (LOW) the scanner is applied where the brief of TF-07 named; the other decoders of author text still substitute U+FFFD silently

Executed: `metadata.DecodeJSON` accepts a lone surrogate escape and returns U+FFFD in a value and in a key (`TestZZMetadataDecoderSubstitutes`; it refuses invalid bytes);
the AI request body goes through it (`internal/platform/access/ai.go:110`, `aiBody`): `{"request":"Rename the order page<escape of U+D800> to Orders."}` is accepted and the request is
stored, hashed and sent to the provider with U+FFFD in it (`TestZZAIBodyWithALoneSurrogateEscape`; a raw invalid byte is refused). ADR104 item 15 e names the metadata decoder for
"a document an author sends" and not the request text. Every JSON decode site of `internal/tools`, `internal/ai`, `internal/metadata`, `internal/runtime/**` and
`internal/platform/**`, by source (the sites are read from a `grep` of `json.Unmarshal|NewDecoder|Decode(|DecodeJSON`; the column "how" says whether the row was executed or read):

| source | site | raw invalid bytes | lone surrogate escape | how |
|---|---|---|---|---|
| provider answer | `ai/provider_http.go:189` `parseEnvelope` | refused (now) | refused (now) | executed (tests, controls) |
| model payload | `ai/schema.go:77` `strictTree`, then `metadata.DecodeJSONBounded` | refused | refused (now) | executed |
| tool answer | `tools/connector.go:191` `parseFlatObject`, then `decodeScalar` (`catalog.go:528`) | refused | refused (now) | executed |
| catalog file | `tools/catalog.go:209` `LoadCatalog` | refused | refused (now) | executed |
| author document, metadata, preview, promote, dataset admission | `metadata/validation.go:219-227` via `platform/access/metadata.go:56`, `preview.go:42`, `promote.go:108`, `datasets.go:44` | refused | **substitutes** | executed for `DecodeJSON`, read for the routes |
| AI request and decision bodies | `platform/access/ai.go:97-131` `aiBody` | refused | **substitutes** | executed |
| AI provider configuration (operator) | `ai/config.go:80,97` | refused | substitutes | read |
| records and datasets (end users of an application) | `runtime/data/store.go:85`, `query.go:94,442`, `evolution.go:62,121`, `dataset.go:383`, `shadow.go:137`, `values.go:169`, `visibility.go:167`; `runtime/workflow/store.go:270,297` | refused | substitutes | read |
| tool inputs of a workflow | `tools.DecodeInput` (`catalog.go:524`, `connector.go:139`, `runtime/workflow/tool_step.go:178`) | already decoded by `DecodeJSON` | substitutes | read |
| sign-in body (`code`, `state`, `issuer`) | `platform/signin/signin.go:91` `readJSON` | **substitutes** (no check) | substitutes | read |
| rows the platform wrote (plans, IR, stored drafts, Redis flow, shares) | `workflow/plan.go`, `rules/compile.go`, `workflow/store.go:139`, `ai/service.go:265`, `service_decide.go:125`, `cache/login.go:64`, `runtime/data/dataset.go:414`, `shadow.go:191`, `query.go:429` | n/a | n/a (already passed an author-facing decoder) | read |
| test and scripted sources | `ai/provider_scripted.go:158`, `ai/corpus/*.go` | n/a | n/a | read |

Legitimate answers are not broken (probe `TestZZLoneSurrogateDifferential` and the tests of the commits): a valid pair, the escape of U+FFFD, multi-byte text and the Unicode tag
characters of the attack corpus (written as pairs in `internal/ai/testdata/corpus/*.json`) pass; `go test ./internal/ai/...` and `./internal/tools` pass. Impact: LOW, as in the ADR
(a replacement character instead of a refusal, no bypass: the substituted value cannot equal a different legitimate value that is checked). Fix direction: apply
`tools.HasLoneSurrogateEscape` in `metadata.decodeJSON` (one place, next to its `utf8.Valid`) so that the author-facing, AI, data and workflow sites inherit it.

### FU-06 (LOW) TF-02 holds; the members that remain are ids, fixed texts and one model token

Executed: the two named integration tests pass; the members of every body that a role with `ai.read` + `ai.apply` + `metadata.write` (or `requirements.decide`) and without
`metadata.read` / `requirements.read` can receive were enumerated by marshalling the view types with every field set and by running `aiReply` over every refusal (probes
`TestZZBodiesOfTheDecisionRoutes`, `TestZZErrorBodiesOfTheDecisionRoutes`). Apply 200: `proposal.{app_id attempts context_hash created_at decided_at decided_by diagnostics_hash
draft_hash expires_at failure_code finished_at id kind proposal_hash reject_reason request_hash requester_id result_hash result_revision source_revision status usage.{input_tokens
output_tokens}}`, `revision.{app_id created_at created_by revision schema_version source_hash status}`, `sensitive[]`. Confirm 200: the same `proposal`, `requirements.{app_id version hash
decided_by decided_at}`, `recorded[].{id category requiredness requiredness_basis status provenance}`. Reject 200: `proposal`. List 200: `proposals[]` with the same `proposal`
members (`summaryColumns` only, `store.go:436`); the two creations answer 403 to these roles (no `ai.propose`; with it they would compose `metadata.read`, `ai.go`, the first switch of
`API.ai`), `GET /proposals/{id}` answers 403 because it composes `metadata.read` (the matrix asserts `get=false` for both roles), apply is 403 for the confirm role and confirm for the apply
role. Errors: `{code message request_id}` plus, for the 409
`proposal_conflict`, `conflicts[].{target category reason}`; the 422 `proposal_invalid`, `diagnostics[].{path code message}`; the 409 `acknowledge_mismatch`, `sensitive[]`; every other
refusal a fixed code and message. The three optional members `proposal`, `derived` and `diagnostics` of `ai.Proposal` are filled only by `Store.Get` (`store.go:366-422`, `scanProposal(..., withBody)`); `Summary` (`:424`)
leaves them out and the database test shows no `operations`, `document`, `semantics` or `statement` in the answers. Audit evidence (`aiEvidence`, `metadataEvidence`) holds hashes,
codes, counts and the revision, no text. What can still be learned without `metadata.read`: definition ids and fixed validator texts (`conflicts[].target`, `diagnostics[].path`,
`sensitive[]` as `<definition id>:<aspect>`, which the caller must echo by design), the hashes of the draft and of the request (a guess oracle), and `recorded[].category`, a token of up
to 64 characters (`categoryPattern`, `schema.go:42`) that the **model** chose and the caller did not send, which ADR104 item 15 d calls "the decisions the caller made". Impact: LOW; the
boundary of AO-02 (definitions, labels, expressions, requirement statements) holds. Fix direction: state the members in item 10; drop `category` from `Recorded` or say it is the
model's.

### FU-07 (LOW) statements of the new and changed documents that the tree does not back

1. "35 requests and commands that stay as written" (ADR104 item 15 a; plan, TF-01 row): the list has **37** entries (`redact_prose_test.go:27-65`; 35 at `aa3ee70`, two added by
   `83738a2`). "24 forms" is right (`:79-102`).
2. ADR104 item 15 h says that `scripts/bootstrap.sh` "says in the message what a SHUTDOWN_TIMEOUT is"; the message is in `internal/config/config.go:141`.
3. ADR104 item 15 a: "the worst shape is `create user x identified by '...'` repeated": not reproduced; of 50 shapes the named one cost 3.4 to 3.7 us/byte in my first run and 3.0 to
   3.5 in the second, `mysql -p<4 characters>` repeated 4.2 to 4.9 and 3.5 to 4.0, `sshpass -p` 3.5 to 4.5 and 3.7 to 4.1, the look-alike prefix shapes 4.5 and 4.5 to 5.2; the claim
   of linear time holds (A.5).
4. ADR104 item 15 d: `Recorded` as "ids, categories and states of the decisions the caller made": FU-06.
5. The handoff: "the OpenTelemetry libraries log through slog with one fixed warning": FU-03.
6. The ledger row of the review: "every fix has a test that fails without it": none detects the chmod order of `0b1407e`, the `rm -rf` of `bootstrap.sh`, or the text of the `SHUTDOWN_TIMEOUT`
   message (FU-08).
7. The residual lists of ADR104 item 15 a omit the PostgreSQL option words, `OR REPLACE`, a `;` inside a quoted SQL password and the clause cut by the bound (FU-01).
Not checkable here and so not claimed: every figure of the gate record (21 s, 400 s, 2,120.4 s, 11 and 7 browser tests, 64 s, 609.1 s), "the 300,000-text idempotence run was a separate
one-off" (the shipped test runs 6,000 by default; I ran 300,000 of my own, A.5), and the failure message that the plan quotes for the TS-01 reproduction (FU-09, NOT RUN).
What the tree does back is listed in "Attacks that held" (hashes, test names, counts, `git diff 364c7a4 f33a0b5 -- . ':!docs'`, the toolchain checksum, the advisories).

### FU-08 (LOW) tests that do not detect everything they are credited with

Each is a result of a negative control (section F).
1. The chmod before the append of `0b1407e` (`scripts/m6r-env.mjs`, `m8-env.mjs`): with both scripts at `ad80853` (chmod after the append) `node --test scripts/runtime-env.test.mjs`
   still passes 15 of 15; the IS-03 test checks the final mode only.
2. `TestRedactionOfAStatementKeepsItsName` passes against the rule it replaced; it guards the new design, not TF-01.
3. RC-04 (`ai_routes_test.go`): dropping `reject` from `aiPOST` and allowing a trailing slash are detected; adding the alternative `proposals-x` is not, because the test compares the
   registered patterns and 17 hand-picked neighbours, not "exactly".
4. `catalog_test.go`, "a half in the member name": refused by `strictKeys` (a non-ASCII key) with the new check removed; only the two value cases detect it.
5. No test covers the `rm -rf .local/toolchains/go` of `bootstrap.sh` (it runs only when `scripts/go.sh version` fails, so an old toolchain that runs is not removed, as the handoff
   now says) or the text of the new `SHUTDOWN_TIMEOUT` message.

### FU-09 (LOW) the TS-01 reproduction cannot pass under one of the two fixes that its comment names

`tests/integration/m10_ts01_test.go:26-67`. Without the variable it skips (executed: `SKIP ... (0.00s)`, line 28; it compiles with `-tags=integration` and `go vet`). Its comment (line
60) says that "the fix may settle the row at the cancel, or refuse the cancel: both leave nothing stranded", and the code returns early only when nothing is outstanding after the cancel
took (lines 46-47 fail the test when `Termination` is not `CANCELLED`). A fix that
refuses the cancel answers 409: `m.signal` calls `m9Decode`, which does `t.Fatal(r.Code, ...)` for any status other than 200 (`m9_workflow_test.go:139-143`), so the test fails under that
fix although the invariant holds. By reading; the test itself was not run (it needs the database and a name outside the two that the brief allows, and it fails today by design).
Fix direction: send the cancel with `m5Call` and accept 409 as a pass.

## Attacks that held

Each line is an executed probe unless it says "read".

1. **TF-01 sentences** (`TestZZTF01Sentences`): the ten sentences (the eight of the review's table, the request of its `BuildContext` case, the repair template) are returned as written, `n = 0`.
2. **Statement forms** (`TestZZCorpusForms`): of 126 forms (MySQL, MariaDB, Oracle, PostgreSQL, SQL Server, 40 command variants, `.netrc` in 11 spellings) the secret is gone in 102 and in
   all spellings of space, case, CRLF, tab, quote, several users in one statement of up to 3 users, `IDENTIFIED WITH plugin BY`, `ENCRYPTED PASSWORD`, `ALTER USER ... WITH PASSWORD`,
   `GRANT ... IDENTIFIED BY`; 20 forms that `0155f7b` leaked are redacted now (`curl -u` glued, `--user=`, `az login -p` and `--password`, `gpg --passphrase` in four spellings,
   `ldapsearch -w` in four, `redis-cli --pass`, a `.netrc` on several lines, a default entry, a quote or a blank inside a quoted password).
3. **UTF-8 at the span cut** (`TestZZSpanBoundary`): a clause placed 960 to 1,010 bytes after the head with fillers of 1, 1, 2, 3 and 4 bytes per character: 255 outputs, all valid UTF-8, no
   panic; `end` backs up to a rune start (`redact.go:118`). 59 tiny, truncated and invalid-byte inputs (`TestZZEdgeInputsDoNotPanic`): no panic, valid output for valid input, fixed points.
4. **Look-alike spellings** (`TestZZLookalikeStatementWords`): the statement words in fullwidth, with Cyrillic look-alikes, with a joiner inside `IDENTIFIED` and with no-break spaces:
   the secret is gone in all four (the fold pass of `redactText`).
5. **Closed `proseSpecification`** (`TestZZProseSpecificationValues`): 18 families of credential-shaped values (word-year, word-word-digit, alphanumeric-word, leet, number-word-number,
   hex-word, ...) x 400 values x 11 connectors (`is`, `was`, `are`, "the admin password is", `token is`, `secret is`, three comma forms, `=`, `:`): redacted in all but four families:
   `Word-Word!` (a trailing `!` is read as the end of the sentence, in all three tables), a closed-list prefix followed by a word (letters only: the rule needs a digit or a symbol, in
   all three tables), a closed-list prefix followed by a year (`Sso-2026`: kept by design, "a value spelled like one of them stays kept"), and `15-days` (intended); four values of the
   family number-word (a one-digit number, a hyphen and `tiger`, 7 characters) stay only in the `:` form, whose rule needs 8 characters and has nothing to do with the exemption. The red
   team's seven sentences are kept.
6. **Card scan against my reference** (`TestZZCardsAgainstMyReference`, `TestZZIssuerTable`): a reference of my own (every start after a non-word byte, every end after a digit with a
   non-word byte after it, 13 to 19 digits with single optional separators, union of overlapping windows, touching windows apart) that shares only `cardIssuer` and `luhn`; `cardIssuer` is
   cross-checked against a table of my own over all 4-digit prefixes and lengths 12 to 20 (0 differences). 1,500,000 random texts (cards in four spellings, random chains, `e-acute`,
   no-break space, Arabic digit, CRLF, markers, chains at the ends): scan and reference agree in output and in count in all; 838,990 hold a window, 66,279 merge overlapping windows,
   at most 10 windows in a text, 1,193,830 markers. Linear (`TestZZCardLinear`): 64 KiB to 1 MiB on nine shapes, 0.03 to 0.27 us/byte (single digits and hyphen chains are the
   dearest, 14 ms and 13 ms for 64 KiB, 224 ms and 271 ms for 1 MiB); the crafted all-valid chain and four-digit groups cost 0.03 to 0.05.
7. **Fixed point, determinism, no invalid UTF-8, no panic** (`TestZZFuzzFixedPoint`): 60,000 texts (seed 99) and 240,000 (seed 4242) of 1 to 14 fragments of the changed rules (statement
   words, flags, quotes, `;`, blanks of 0 to 1,100, multi-byte and look-alike characters, markers, generated secrets, cards), each through `redactText` twice, once more for determinism
   and through `redactString`: 0 not a fixed point, 0 non-deterministic, 0 invalid UTF-8 from valid input, 0 panics.
8. **Cost** (`TestZZRedactorCost`, `TestZZRuleCost`): 50 shapes at 64, 128 and 256 KiB (`curl`, `curl -u`, `docker login`, `mysql -p`, `zip -P`, `sshpass -p`, `wget --password`, `gpg`,
   `ldapsearch`, `redis-cli`, `az login`, `.netrc` forms, `create user ... identified by` many times and as one long statement, `grant`, `identified by`, `password '`, quotes, `==`, `:=`,
   `=>`, digit chains, look-alike and multi-byte prefixes, counted-repetition openers): worst 4.91 us/byte (`mysql -pXXXX` repeated, 128 KiB) in the first run and 5.18 (the look-alike
   prefix before `create user`, 128 KiB) in the second, which ran beside a fuzz job; the per-byte cost at 256 KiB is 0.6 to 1.43 times that at 64 KiB for every shape (linear is
   1.0); the changed rules alone peak at 0.40 to 0.52 (`passwordOfACommand`), 0.12 to 0.13 (`.netrc`), 0.20 to 0.24 (statement scan), 0.09 to 0.16 (`sqlUserHead`), 0.75 to 0.88
   (`assignment`) and 0.03 to 0.04 (cards, for the nineteen units of that probe) us/byte; at most 125 to 126 bytes allocated per input byte (the look-alike shape, which runs the table
   twice).
9. **Surrogate scanner against the decoder** (`TestZZLoneSurrogateDifferential`): 400,000 random texts built from 43 pieces (lone and paired escapes in both cases, the escape of U+FFFD and
   the character itself, escaped backslashes before `u`, malformed escapes, multi-byte text) wrapped in 13 structures (a string, a member value, a member name, arrays, nesting, duplicate
   keys, two documents, truncated, text outside a string): 146,503 are well-formed JSON, 95,142 of them make the decoder substitute (counted with `Decoder.Token` over keys and values, so
   duplicates count); the scanner agrees in all, no panic. Linear: 256 KiB to 4 MiB on seven shapes, about 1 to 2 ns/byte (7 in one cold first measurement of the first run).
10. **Each surrogate site detects** (negative controls, F): the tool answer, the catalog, the envelope's escape check, the envelope's UTF-8 check and `strictTree` each have a test that
    fails when only that check is removed; a stubbed scanner fails four tests.
11. **TF-02 on the database**: see B and F: the two named tests pass; the parent `ai_decide.go` and `service_decide.go` fail the matrix at `holds "operations"`; the four single-member
    mutations (revision document, proposal body of the apply answer, proposal body of the confirm answer, requirement set body) fail it at `document`, `operations`, `semantics`,
    `semantics`.
12. **Sink and rate limit**: `TestOtelWarningIsFixedAndLimitedInRate` passes; 64 goroutines x 2,000 calls under `-race` (`TestZZSinkConcurrency`): one warning, no race; `Enabled` is true for
    levels 0 and 1 only; a derived sink is the same sink.
13. **`go.mod`**: `GOPROXY=off GOFLAGS=-mod=mod go mod tidy -diff` prints nothing (rc 0); `go mod verify`: "all modules verified"; `go-logr/logr` is imported directly by
    `internal/observability`, so the move from `// indirect` is the tidy result.
14. **Jitter** (read, and the tests of the commit pass): `Jittered(d)` returns `d - d/4 + rand(0..d/2)`, from 0.75 d to 1.25 d; the base pauses 100, 200, 400, 800 and 1,500 ms add up to
    3.0 s and to at most 3.75 s with the spread, against `aiResultBudget` of 6 s, which `RetryPaced` enforces as a context deadline in the timer wait (`retry.go:80-97`); `aiResultPause`
    clamps the shift to 0..4 (`ai.go:587-592`), so attempts below 1 or above 38 neither panic nor overflow (the test tries -3, 0, 1, 5, 6, 37, 38, 64 and 1<<20); `Jittered(0)` and
    negative waits return 0.
15. **Scripts**: both chmod calls precede the append; the files were read earlier in the same script (`existsSync` / `readFileSync` throw when `.env` is absent), so the call cannot meet a
    missing file; `node --test scripts/runtime-env.test.mjs`: 15 of 15 pass. `bootstrap.sh` (read): `root` is the repository root and the script `cd`s there, the path is the literal
    `.local/toolchains/go`, and the `rm -rf` comes after the checksum check and before the unpack, so a failed download keeps the old toolchain.
16. **Toolchain and advisories, online**: the SHA-256 in `scripts/bootstrap.sh:11` (`65efbc80...379b40`) equals the value that `go.dev/dl/?mode=json` publishes for
    `go1.27.3.linux-amd64.tar.gz` (the archive itself was not downloaded); `GO-2026-6809` ("Distrust compromised intermediate certificates in crypto/x509") is fixed in 1.27.3 and 1.26.10;
    the advisories of `golang.org/x/net` fixed after 0.59.0 are exactly seven (`GO-2026-6603 6610 6611 6612 6617` at 0.60.0, `6860 6866` at 0.62.0).
17. **TS-01 gate**: `go test -tags=integration -run TestTS01 ./tests/integration` without the variable: `SKIP` in 0.00 s, with the message of the gate.
18. **Documents**: the 29 hash-like tokens of the added lines are commits (`git cat-file --batch-check`); the test names the new text uses exist; "(the commits after `364c7a4` are documents
    only)" holds: `git diff --stat 364c7a4 f33a0b5 -- . ':!docs'` prints nothing, the one changed file is `docs/migration/LINE_INTEGRATION_PLAN.md` (19 insertions, 1 deletion); the ledger
    counts for the post-fix review (3 MEDIUM, 8 LOW) match `POSTFIX_INDEPENDENT_REVIEW.md`; "nothing in this repository reads" the apply and confirm answers holds for `apps/`, `cmd/` and
    `internal/` outside `platform/access/ai*`; `workflows.cancel` is open to every role but the viewer (`policy.go:112`); "10,000 random texts" (6,000 + 4,000 in
    `TestCardScanAgreesWithTheDefinition`) and "20,000 strings" (`TestLoneSurrogateScanAgreesWithTheDecoder`) are right; TF-03 is stated OPEN and is: the range changes only the
    README of `internal/verification` (no Go file), so the behaviour that the previous review executed is unchanged.
19. **Build and suites**: `gofmt -l` over the changed packages and `tests` prints nothing (probes excluded); `go vet` of the seven changed packages and of `./tests/integration`
    (`-tags=integration`) prints nothing; `go test ./internal/tools ./internal/observability ./internal/platform/postgres ./internal/platform/access ./internal/config
    ./internal/platform/app ./internal/metadata` and `go test ./internal/ai/...` pass.
20. **Other writers**: no library of the list wrote outside slog in the probes except as FU-03: `pgx` (the 28 matches in the module are examples and the README), `go-oidc` (none) and
    `goose` (`cmd/migrate` uses `NewProvider` without `WithVerbose`; the global `log` calls are in the unused package logger) write nothing; `go-redis` goes through `redis.SetLogger`
    (installed by `Start`, unchanged by this range; `TestRedisClientLogsOnlyThroughSlog` passes in the suite); `http.Server` has no `ErrorLog`
    (`httpserver.go:272`), so the standard library would write panics that escape `recoverer` and accept errors to the standard logger; two anonymous GETs, one with `;` in the query,
    produced no line (`TestZZServerErrorLogReachesTheStandardLogger`).

21. **Handoff on the toolchain** (`AGENT_HANDOFFS.md`): with the go1.27.2 toolchain of the module cache, `GOTOOLCHAIN=local go list ./internal/config` in this worktree prints `go: go.mod
    requires go >= 1.27.3 (running go 1.27.2; GOTOOLCHAIN=local)`, and with `GOTOOLCHAIN=auto` the same binary reports `go version go1.27.3 linux/amd64` (it switched to the cached
    1.27.3; `GOPROXY=off`, so the download and the checksum-database check are not exercised), as the handoff says; and `scripts/go.sh` uses an existing `.local/toolchains/go` whatever its
    release (read), so `make setup` does not replace an old one unless it is removed first.

## Section F: negative controls

Each control restored the file afterwards (`git checkout -- <file>`) and `git status` showed no change of a tracked file before the next one.

| test or change | control | result |
|---|---|---|
| `redact_prose_test.go` (6 tests), `aa3ee70` | `redact.go` of `eb4b51b` (the parent) | 5 FAIL (`KeepsOrdinaryText...`, `OfPasswordsGivenAsArguments...`, `OfPassphraseShapedValues...`, `KeepsMoreSentencesAboutCredentialFormats`, `OfAssignmentsWrittenWithOtherOperators`); **`OfAStatementKeepsItsName` PASSES** (FU-08.2) |
| the two sentences of `83738a2` | `redact.go` of `aa3ee70` | `KeepsOrdinaryText...` FAIL |
| `redact_cards_test.go`, `b9ac1ee` | `redact.go` of `aa3ee70` (first-window loop) | `TestCardScanAgreesWithTheDefinition` FAIL, `TestACardAfterANumberOfFourGroupsIsRedactedWhole` FAIL (a group of the card stays); `TestCardNumbersBesideOtherNumbers` passes (it is PF-09's) |
| `HasLoneSurrogateEscape`, `jsontext_test.go` | the function returns false at once | 4 tests FAIL (`TestHasLoneSurrogateEscape`, `TestLoneSurrogateScanAgreesWithTheDecoder`, `TestLoadCatalogRefusesInvalidUTF8`, `TestResponseBodyLimitsAndValidation`) |
| `connector.go` check | `|| HasLoneSurrogateEscape(raw)` removed | `TestResponseBodyLimitsAndValidation` FAIL (five escape cases succeed with U+FFFD) |
| `catalog.go` check | removed | `TestLoadCatalogRefusesInvalidUTF8` FAIL (two value cases; the member-name case is refused by `strictKeys`, FU-08.4) |
| `provider_http.go` escape check | removed | `TestParseEnvelopeRefusesTextThatIsNotTextInsteadOfSubstituting` FAIL (4 cases) |
| `provider_http.go` UTF-8 check | disabled | the same test FAIL (4 cases) |
| `schema.go` `strictTree` check | disabled | `TestAPayloadWithALoneSurrogateEscapeIsMalformed` FAIL (3 cases) |
| `otellog_test.go` | `otel.SetLogger` call removed | `TestOtelLibrariesLogOnlyThroughSlog/after_Start...` FAIL ("reaches the process output"); the unit test of the sink passes (it does not test the call) |
| RC-04 test | `reject` dropped from `aiPOST` | FAIL |
| RC-04 test | trailing slash allowed | FAIL |
| RC-04 test | extra alternative `proposals-x` | **PASSES** (FU-08.3) |
| `ai_retry_test.go` | jitter removed from `aiResultPause` | FAIL ("1 distinct values: no spread") |
| `ai_retry_test.go` | clamp removed | FAIL (panic, negative shift) |
| `retry_test.go` | `Jittered` of `d98eb3d^` (no guard) | FAIL (`Jittered(-1ns) = -1ns`, then a panic of `crypto/rand`) |
| `retry_test.go` | `Jittered` returns `d` | FAIL ("covered only 800ms to 800ms") |
| `TestM14APIPolicyMatrix`, TF-02, database | `ai_decide.go` and `service_decide.go` of `ad80853` | FAIL at `m14_api_test.go:203` (`holds "operations"`); the build of the stack and the run took 29 s |
| the same, one member each | the revision document kept / `Summary` -> `Get` in `Apply` / `Summary` -> `Get` in `Confirm` / the requirement body added to the header | FAIL at `:203` `document`, `:203` `operations`, `:230` `semantics`, `:230` `semantics` |
| IS-03 test, the order of `0b1407e` | both scripts at `ad80853` | 15 of 15 **PASS** (FU-08.1) |
| `m10_ts01_test.go` | run without the variable | SKIP, 0.00 s; the failing run is NOT RUN (FU-09) |
| `redact_shapes_test.go` fragments, `operator_env_test.go` | not controlled | the first is a property test (it passes against the old table too, as a property of both); the second changes a comment and a message |

## Reproduced after the reboot

The host was restarted after the first runs. The probe sources, the scratch packages and the helper scripts survived; the worktree was clean and detached at `f33a0b5`, with no probe in
it. The probes were restored from the backups and run again; the numbers match those above to the digit unless the line says otherwise.

| part | rerun | result |
|---|---|---|
| A.1 | `TestZZCorpusKeep`, `TestZZListOldChanged`, `TestZZNewRegressionsOfTheFollowUp`, `TestZZCorpusForms`, `TestZZDocsCorpus`, `TestZZProseSpecificationSentences`, `TestZZTF01Sentences`, `TestZZEndToEndLabels` | 264 texts: 145 / 10 / 97 / 66 / 43; list 1 2 / 70 / 7, list 2 8 / 27 / 41; 17 texts kept at 0155f7b; 126 forms, 24 leak, 5 new; 324 files, 32,843 lines, 857 / 859 / 859 / 2; 78 format sentences 45 / 5 / 13; the ten TF-01 sentences kept |
| A.2 | `TestZZSpanBoundary`, `TestZZSemicolonInQuotes`, `TestZZManyUsersLongStatement`, `TestZZRolePasswordOptions`, `TestZZEdgeInputsDoNotPanic`, `TestZZLookalikeStatementWords`, `TestZZIdentifiedByCases` | N = 977 (a secret counted as redacted when not whole), and `TestZZCutClassification` (run once afterwards) gives the classes above; the straddling clause leaves its tail; 34 of 60 secrets visible; 35 role statements leak; the `;` case of PostgreSQL leaks; 59 edge inputs, no panic; the four look-alike spellings redacted |
| A.3 | `TestZZProseSpecificationValues` | PASS (the table of counts is the same by construction: same seed) |
| A.4 | `TestZZIssuerTable`, `TestZZCardsAgainstMyReference` (1,500,000), `TestZZCardChains`, `TestZZCardLinear`, `TestZZChanceRates` | 0 differences; 838,990 / 66,279 / 10 / 1,193,830; the chain figures identical; linear |
| A.5 | `TestZZFuzzFixedPoint` (60,000, seed 99), `TestZZRedactorCost`, `TestZZRuleCost` | 54,536 changed, 0 / 0 / 0 / 0 in 166 s; worst cost 5.18 us/byte, 126.1 bytes allocated per input byte |
| B | `RESET=0 it-run m14 <worktree> review-followup-m14-rerun 'TestM14APIPolicyMatrix\|TestM14APIAskingAndReadingNeedMetadataRead' -v` | `--- PASS: TestM14APIPolicyMatrix (6.00s)`, `--- PASS: TestM14APIAskingAndReadingNeedMetadataRead (2.87s)`, `ok ... 10.023s`, rc 0 (the first run was not `-v`; this one shows that both tests ran); the member probes of the bodies unchanged |
| C | `TestZZLoneSurrogateDifferential`, `TestZZLoneSurrogateLinear`, `TestZZMetadataDecoderSubstitutes`, `TestZZAIBodyWithALoneSurrogateEscape` | 400,000 / 146,503 / 95,142 / 0 mismatches / 0 panics; linear; `DecodeJSON` and `aiBody` substitute |
| D | `TestZZOtelMatrix`, `TestZZSinkConcurrency` with `-race`, the shipped sink tests | 33 settings, 2 print the value, the same three families write to standard error; one warning from 128,000 calls, no race |
| E | `node --test scripts/runtime-env.test.mjs`, `go mod tidy -diff`, `go mod verify`, `go test -skip TestZZ` over `./internal/ai/...` `./internal/tools` `./internal/observability` `./internal/platform/{postgres,access,httpserver,app}` `./internal/config` `./internal/metadata`, `go test -tags=integration -run TestTS01` | 15 of 15; rc 0; "all modules verified"; every package `ok`; TS-01 `SKIP` |

Not run again after the reboot: the 22 negative controls (their results do not depend on the host; each file was restored and `git status` was checked after each), the 240,000-text
fuzz run, the online lookups of section E (data of the two sites, read once), `go vet` and `gofmt`.

## NOT RUN

* The reproduction of TS-01 and its quoted failure message (database; a name outside the two allowed; it fails by design).
* `make check`, `make test-unit`, `-race` over `./internal/ai` or the other large packages (only `-race` on `./internal/observability` and the integration helper's own `-race` build),
  the browser suites, the fuzz targets of the plan, the full integration suite, the ten-minute outage test, the performance gates: the brief excludes them, and every figure of the gate
  record in the plan (21 s, 400 s, 2,120.4 s, 11 and 7 browser tests, 64 s, 609.1 s) is unverified.
* A real `cmd/api` process under SIGTERM (ER-03), a real run of `scripts/bootstrap.sh` (it downloads and deletes), the toolchain archive itself (only its published checksum was compared),
  the download of go1.27.3 and its check against the checksum database by an older `go` (item 21 of "Attacks that held" ran the switch from the module cache with `GOPROXY=off`).
* The integrator's one-off run of 300,000 idempotence texts (I ran my own).
* Roles at the database level beyond the two named tests: the enumeration of the members of the bodies (FU-06) is by reading and by a probe on the view types and `aiReply`, not by
  requests to every route with every stored role.
* A scan of every other `OTEL_*` variable that the SDK reads (33 were tried), of `http.Server` log triggers other than the two GETs, and of `goose`'s global logger under a failing
  migration.

## Commands

Run from the root of the review worktree. `gt` is `/home/user/bin/gt`, `hv` is `/home/user/bin/hv`.

```
git checkout --detach f33a0b5 ; git rev-parse --short HEAD ; git checkout -b review/takeover-followup
git log --oneline ad80853..f33a0b5 ; git diff --stat ad80853 f33a0b5 ; git diff --stat 364c7a4 f33a0b5 -- . ':!docs'
git show <hash> ; git show 11a4ee4:internal/ai/redact.go ; git diff ad80853 f33a0b5 -- <file>          # every code commit and document of the range
git cat-file --batch-check < hashes.txt                                                                  # the 29 tokens of the added documentation lines
mkdir internal/zzold internal/zzprev
git show 11a4ee4:internal/ai/redact.go > internal/zzold/redact.go ; git show 11a4ee4:internal/ai/redact_fold.go > internal/zzold/redact_fold.go   # same for 0155f7b -> zzprev
sed -i 's/^package ai$/package zzold/' internal/zzold/*.go ; printf 'package zzold\nfunc RedactText(s string)(string,int){return redactText(s)}\nfunc RedactString(s string) string {return redactString(s)}\n' > internal/zzold/export.go
cp /home/user/integ/.env .env       # never printed, ignored by git, deleted at the end
gt test -count=1 -run '<probe regexes of the appendix>' -v ./internal/ai            # also ./internal/tools ./internal/metadata ./internal/observability ./internal/platform/access ./internal/platform/httpserver
ZZ_N=1500000 gt test -count=1 -run TestZZCardsAgainstMyReference -v ./internal/ai
ZZ_N=240000 ZZ_SEED=4242 gt test -count=1 -run TestZZFuzzFixedPoint -v -timeout 60m ./internal/ai          # and ZZ_N=60000 (default seed 99)
gt test -count=1 -skip 'TestZZ' ./internal/tools ./internal/observability ./internal/platform/postgres ./internal/platform/access ./internal/config ./internal/platform/app ./internal/metadata
gt test -count=1 -skip 'TestZZ' -timeout 40m ./internal/ai/...
hv nice -n 10 gt test -count=1 -race -run 'TestZZSinkConcurrency|TestOtelWarningIsFixedAndLimitedInRate|TestOtelLibrariesLogOnlyThroughSlog' -v ./internal/observability
hv nice -n 10 gt test -tags=integration -count=1 -run 'TestTS01' -v ./tests/integration
gt vet ./internal/ai ./internal/tools ./internal/observability ./internal/platform/access ./internal/platform/postgres ./internal/config ./internal/platform/app ; gt vet -tags=integration ./tests/integration
<toolchain gofmt> -l internal/ai internal/tools internal/observability internal/platform internal/config tests
GOPROXY=off GOFLAGS=-mod=mod gt mod tidy -diff ; GOPROXY=off GOFLAGS=-mod=mod gt mod verify
/tmp/node24/node-v24.22.0-linux-x64/bin/node --test scripts/runtime-env.test.mjs
RESET=0 it-run m14 <worktree> review-followup-m14 'TestM14APIPolicyMatrix|TestM14APIAskingAndReadingNeedMetadataRead'      # PASS, rc 0, 8.771 s of Go time; after the reboot with -v: 10.023 s
curl -sS 'https://go.dev/dl/?mode=json&include=all' ; curl -sS https://vuln.go.dev/ID/GO-2026-6809.json ; curl -sS https://vuln.go.dev/index/modules.json    # read with python3 -I
# negative controls (each reverted with git checkout -- <file>): git show <rev>:<path> > <path>, or sed -i on one expression, then the test command above;
# the TF-02 ones with RESET=0 it-run m14 <worktree> <log> 'TestM14APIPolicyMatrix' after replacing ai_decide.go / service_decide.go
# at the end: rm -f internal/*/zz_review_*_test.go internal/platform/*/zz_review_*_test.go ; rm -rf internal/zzold internal/zzprev ; rm -f .env ; git status --short   # empty
git add docs/verification/TAKEOVER_FOLLOWUP_INDEPENDENT_REVIEW.md ; git commit                                                                                       # this file only
```

## Appendix: probe sources (condensed)

Helpers (`alnum`, `luhnNumber`, `groups`, `material`, `fullwidth`, `hexOf`, `fixtureDoc`, `ctxInput`, `build`, `segment`, `cardIssuer`, `luhn`, `redactText`) are the tree's own; the
old tables are called as `zzold.RedactText` and `zzprev.RedactText`. No probe contains a secret-shaped literal: `{S}` is `alnum(label, 12)` and `{H}` is `hexOf(label, 40)`.

**A.1 corpus harness** (`zz_review_corpus_test.go`, `zz_review_list_test.go`, `zz_review_list3_test.go`): `zzKeep` (215 strings), `zzKeep2` (49) and a third list of 30 are Go slices of sentences; for each `in`:
`old,_ := zzold.RedactText(in); prev,_ := zzprev.RedactText(in); now,_ := redactText(in)`; count `old != in`, `prev != in`, `now != in`, and print `now != in && old == in`. The 43 texts
destroyed now and kept by 11a4ee4 are listed after this appendix. `zzForms` is a slice of `{name, text}` with `{S}` and `{H}` placeholders replaced by `alnum("zzrev/<i>", 12)` and
`hexOf("zzrev/hash/<i>", 40)`; the verdict per table is `strings.Contains(out, S)`.

**A.2 statement scan** (`zz_review_scan_test.go`):

```go
for pad := 0; pad < 1200; pad++ { // where does the protection end
    out, _ := redactText("CREATE USER bob" + strings.Repeat(" ", pad) + "IDENTIFIED BY '" + S + "';")
    if !strings.Contains(out, S) { last = pad }                                    // last == 977 for that secret (not whole); TestZZCutClassification, with another secret: 969 / 970-976 / 977 and up
}
for _, filler := range []string{" ", "/", e-acute, cjk, emoji} {                    // multi-byte filler before the clause
    for pad := 960; pad <= 1010; pad++ { /* build "CREATE USER bob " + filler*... + " IDENTIFIED BY '"+S+"';" */ ; if !utf8.ValidString(out) { t.Fatal() } }
}
for opts in {"", "WITH", "WITH LOGIN", ..., "WITH CONNECTION LIMIT 5", "IN ROLE readers", "VALID UNTIL 'infinity'", "ROLE a", "ADMIN b", "SYSID 100", ...} {  // 20 option variants
    for verb in {"CREATE ROLE app", "CREATE USER app", "ALTER ROLE app", "ALTER USER app", "CREATE GROUP app"} {
        text := verb + " " + opts + " PASSWORD '" + S + "';" ; if leaks(redactText(text)) { log }  } }                // 35 of 100 leak
// 60 users: "CREATE USER 'user%02d'@'%%' IDENTIFIED BY '<S_i>'" joined by ", " and a final ";"  -> secrets visible: old 60, prev 0, now 34
```

**A.3** (`zz_review_prose_test.go`): 18 generators (`cap1(w)+"-"+year`, `w+"-"+w+digit`, `alnum(.,9)+"-"+w`, `"P4ss-"+w`, `number+"-"+w+"-"+number`, `hex+"-"+w`, closed-list prefix + word or year, `N+"-days"`,
`Word-Word!`, ...) over 20 words; 11 connectors `"The password is %s."`, `"The password was %s"`, `"The credentials are %s"`, `"the admin password is %s and it rotates"`, `"The token is %s."`,
`"The secret is %s"`, `"define('DB_PASSWORD', '%s');"`, `"define(\"API_TOKEN\", \"%s\");"`, `"config.set('secret', '%s')"`, `"password=%s"`, `"password: %s"`; count the texts that still contain the value, per table.

**A.4 reference** (`zz_review_cards_test.go`):

```go
func zzWindows(s string) []zzWin {
    for i := 0; i < len(s); i++ {
        if !isDigit(s[i]) || i > 0 && isWord(s[i-1]) { continue }
        digits := []int{int(s[i]-'0')}; j := i + 1
        for {
            if n := len(digits); n >= 13 && n <= 19 && (j == len(s) || !isWord(s[j])) && cardIssuer(digits) && luhn(digits) { out = append(out, zzWin{i, j}) }
            if len(digits) == 19 { break }
            switch { case j < len(s) && isDigit(s[j]): digits = append(digits, int(s[j]-'0')); j++
                     case j+1 < len(s) && (s[j] == ' ' || s[j] == '-') && isDigit(s[j+1]): digits = append(digits, int(s[j+1]-'0')); j += 2
                     default: j = -1 }
            if j < 0 { break } } } }
// union: sort by from; a window with from < previous.to extends it, otherwise it opens a new span; output = text with spans replaced by "[REDACTED:card]", count = spans
```

The generator draws 1 to 12 items per text: a Luhn number of a random issuer and length written in one of four spellings (flat, groups of 4, random groups of 1 to 6, groups of 3 to 6, with a
space, a hyphen or none), 1 to 14 random digits, a chain of 2 to 10 four-digit groups (half of them starting with 4), a card followed by a separator and digits, or one of 27 separators and
characters (blank, hyphen, two blanks, newline, CRLF, tab, `/`, `.`, `a`, `_`, e-acute, CJK, emoji, Arabic digit, no-break space, a marker, `12/27`, ...). `TestZZCardChains` builds chains
`fmt.Sprintf("%04d", rnd.Intn(10000))` joined by blanks and counts `len(strings.Fields(text)) - len(strings.Fields(redacted with markers as one blank))`.

**A.5** (`zz_review_fuzz_test.go`, `zz_review_cost_test.go`): the fuzz draws 1 to 14 of 50 fragment generators (see item 7 above) and checks
`out,_ := redactText(t); again,m := redactText(out); again == out && m == 0; redactText(t) == out; utf8.ValidString(out) if utf8.ValidString(t); redactString(redactString(t)) == redactString(t)`;
the cost probe builds `size` bytes by repeating a unit (`"curl "`, `"curl -u "`, `"create user x identified by 'yyyy' "`, ...) or head + units + tail, times `redactText` after `runtime.GC()`
and reads `TotalAlloc` before and after.

**B** (`zz_review_bodies_test.go`): `zzKeys(v)` marshals `v` and returns the sorted JSON paths; it is called on `aiApplyView`, `aiConfirmView` and the reject body with every field set, and
on `rr.response.body` for the `requestRejection` that `aiReply(req, err)` returns for 12 typed refusals (`ai.Refusal` with `Conflicts`, `Diagnostics`, `Sensitive`, the sentinels, a
`PortError`, a raw error). `aiBody(r, aiCreateBody, &dest, []string{"request"}, "expected_revision")` is called with a body that holds the escape of U+D800 (built as `"\\" + "u" + "d800"`).

**C** (`internal/tools/zz_review_json_test.go`):

```go
func zzTruth(raw []byte) (substituted, wellFormed bool) {
    if !json.Valid(raw) { return false, false }
    dec := json.NewDecoder(bytes.NewReader(raw)); decoded := 0
    for { tok, err := dec.Token(); if err == io.EOF { break }; if s, ok := tok.(string); ok { decoded += strings.Count(s, "\xef\xbf\xbd") } }   // keys and duplicates included
    spelled := 0 /* my own scan of the strings: count the escape of U+FFFD (4 hex digits, any case) and the raw bytes EF BF BD, skipping every other escape */
    return decoded > spelled, true }
// 400,000 texts: pieces joined (43 pieces) inside 13 wrappers; for well-formed texts require HasLoneSurrogateEscape(raw) == truth
```

**D** (`zz_review_otel_test.go`): for each of 33 `{variable, shape}`: `secret := otelSecret("zz/matrix/<i>")`, `runOtelHelper(t, "start", variable+"="+shape(secret))` (the shipped child-process helper
that calls `Start(ctx, "otel-log-regression", "test", "http://127.0.0.1:1", slog.New(slog.NewJSONHandler(os.Stdout, nil)))`), then `strings.Contains(stdout+stderr, secret)` and whether
standard error is empty. `TestZZSinkConcurrency`: 64 goroutines x 2,000 mixed `Error`, `Info(1)`, `WithValues().WithName()` and `Enabled` calls on one `otelLogSink`, counting the warnings in a
mutex-guarded buffer.

**F mutations** (each `git checkout -- <file>` afterwards): `sed -i 's/if !utf8.Valid(raw) || HasLoneSurrogateEscape(raw) {/if !utf8.Valid(raw) {/' internal/tools/connector.go`;
`sed -i 's/!utf8.Valid(raw) || HasLoneSurrogateEscape(raw) || !strictKeys(raw)/!utf8.Valid(raw) || !strictKeys(raw)/' internal/tools/catalog.go`; in `provider_http.go` the same edit of the
envelope condition twice, once without the scan and once as `(false && !utf8.Valid(raw)) || tools.HasLoneSurrogateEscape(raw)`; in `schema.go` `case false && tools.HasLoneSurrogateEscape(raw):`;
`return false` as the first statement of `HasLoneSurrogateEscape`; in `observability.go` the `otel.SetLogger(...)` line replaced by `_ = logr.New(...)`; in `ai.go` `aiPOST` without `|reject`,
with `/?` before the end anchor, with the extra alternative `proposals-x`, `aiResultPause` without `postgres.Jittered(...)` and with `<<(attempt-1)`; in `retry.go` the file of `d98eb3d^` and
`return d + 0*time.Duration(n.Int64()), true`; for TF-02 `git show ad80853:internal/platform/access/ai_decide.go > ...` and `.../service_decide.go`, or one of `header.Document = nil` -> `_ = header`,
`stored, err := st.Summary(` -> `st.Get(`, `final, err := st.Summary(` -> `st.Get(`, and a field `Body any` with the tag `requirements` added to `aiRequirementsHeader`.

**The 43 texts of list 1 and list 2 that the table now destroys and `11a4ee4` kept** (output of `TestZZCorpusKeep`; the first seven are list 1):
`Show the machine host.internal password rotation date on the asset page.` / `Each machine web01.corp.example.com password rotates every 30 days.` / `Alert when the machine
db01.example.com password expires.` / `Create user roles with password 'hint' column.` / `Default login admin password rotation is monthly.` / `Default login admin password reset after first
use.` / `Requirements:` newline `Default login user password reset after first use.` / then list 2: `Create login form password "hint" text under the field.` / `Create login page with password
'forgot' link at the bottom.` / `Create login page with password "reveal" toggle.` / `Create user form with password 'confirm' field.` / `Create user account with password "reset" button for
admins.` / `Create login screen with password "show/hide" icon.` / `Alter user profile with password 'rules' page.` / `Alter role page with password "policy" tab.` / `Create group chat with
password 'optional' setting.` / `Create role form password "strength" indicator.` / `Create user records identified by 4471 and name.` / `Create user accounts identified by 'email'.` /
`Create user profiles identified by 'username' and tenant.` / `Grant the editor role on the blog to authors identified by 'email'.` / `Grant discounts on orders to customers identified by
'loyalty id'.` / `Default login user password policy: 12 characters minimum.` / `Default login admin password expires after 90 days.` / `Default login guest password optional.` / `default
login admin password reset` / `Show the machine api.example.com login bob password rotation date.` / `The machine srv.local password policy says 14 characters.` / `Add machine
host.example.com password reset to the runbook.` / `Each machine inventory.local password expiry is shown in red.` / `Call the API with curl -u username:password for testing.` /
`Document curl --user USER:PASSWORD usage for the integration.` / `The mysql -u app -pretty output format is not needed.` / `Use mongo -pretty to print the documents.` / `Run mongosh -pretty
if you want formatted output.` / `docker login -password-stdin is not supported, use --password-stdin.` / `Show how the sshpass -p option works in the training deck.` / `The wget
--password option is deprecated here.` / `if password == "admin" then show a warning banner.` / `Warn when token == "expired" in the response.` / `Reject orders where credentials ==
"missing".` / `Set password := "required" for new users.` / `The password => "hidden" mapping in the form.`
