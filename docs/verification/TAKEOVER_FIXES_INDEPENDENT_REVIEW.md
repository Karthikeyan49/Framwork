# Independent review of the fixes made at the takeover of 3 November 2026

Reviewer worktree: a detached checkout of the shared object store, branch `review/takeover-fixes`, from `0155f7b` (branch
`integration/m9r-m14`; the code is identical to `d6ed47b`, the last code commit). Scope: the seventeen items of the brief: the nineteen commits
`3be63ec 1434798 e3c954c 5836a31 a4379a8 55ee30e 61eb1b4 e555654 6673055 4937324 02af0e5 2f27b01 d6ed47b 55bc0b3 7db6350 2fd4628 a3806f3 f3d3fe8 35ac1ab`,
the documents that describe them, and TS-01 as it is described. Written by a reviewer that did not write the fixes and changed no product code, test or ADR
of the tree: every probe lived in this worktree (`internal/**/zz_review_*_test.go` and a scratch package `internal/zzoldredact`, a copy of the redactor
of `11a4ee4`) and was deleted before this file was committed. Result words: PASS / PASS WITH FINDINGS / FAIL / NOT RUN. Every "held" names the attack that
was executed. Every secret-shaped input was generated at run time from a label (the tests' own helpers `alnum`, `luhnNumber`, `groups`); this file
names shapes, never a value. Nothing was run with `-tags=integration`, against Docker, a database or Redis, or on the network; `/home/user/integ` was not
touched (only its objects were read through the shared object store).

## Verdict

**PASS WITH FINDINGS. No HIGH finding. Five MEDIUM (TF-01 to TF-05) and seven LOW (TF-06 to TF-12).** The fixes do what their commit messages say for the inputs
they name, and their negative controls are real (checked below), but several of them leave a hole next to the one they close (PF-01, AO-02, TS-04, LE-02, RT-01) and
one of them (the `password_arg` rule) makes the redactor destroy ordinary sentences that it kept before. The documents call AO-02, TS-04 and LE-02 "fixed" and say the
verification gate and a reviewer cover changes that they do not cover.

| # | item | verdict | one line |
|---|---|---|---|
| 1 | PF-09 card scan (`3be63ec`, `1434798`) | PASS WITH FINDINGS | same output as the definition in 400,000 random texts, linear, boundaries right; a chance window can swallow the head of a real card that the old rule redacted whole (TF-06); the test cannot see it (TF-12) |
| 2 | RT-01 to RT-05, RT-08 (`e3c954c`, `5836a31`) | PASS WITH FINDINGS | linear and idempotent (300,000 fuzz texts), secrets of the named forms covered; but `password_arg` destroys prose (TF-01), the RT-05 exemption un-redacts passphrase-shaped credentials (TF-04), and near forms stay (TF-09) |
| 3 | verifier-v4 (`a4379a8`) | PASS WITH FINDINGS | exact money text, deterministic, budget never reached by an honest plan; a window gated on two other fields passes (TF-03) |
| 4 | PF-03 (`55ee30e`) | PASS | |
| 5 | AO-02 (`61eb1b4`, `e555654`) | PASS WITH FINDINGS | ordering right, built-in roles unchanged, list and veto carry no body; the apply and confirm answers still carry the proposal body and the whole requirement set (TF-02) |
| 6 | TS-02 (`6673055`) | PASS | every tenant-facing compile is on `catalogFor`; no other path found that tells a private tool; one note (TF-12) |
| 7 | TS-04 (`4937324`) | PASS WITH FINDINGS | raw invalid bytes refused (negative control fails without the check); a lone surrogate escape and the AI provider adapter still substitute U+FFFD (TF-07) |
| 8 | LE-02 (`02af0e5`) | PASS WITH FINDINGS | the Redis library is silenced and the test is a real detector; the OpenTelemetry exporters still print raw environment values to stderr (TF-08) |
| 9 | ER-04 (`2f27b01`) | PASS | no retry after anything that could have committed; `RetryWithin` unchanged; notes in TF-11 |
| 10 | ER-03 (`d6ed47b`) | PASS | request contexts are not cancelled by SIGTERM, `Shutdown` waits; not run with a real process; notes in TF-11 |
| 11 | RB-01 (`55bc0b3`) | PASS | |
| 12 | IS-03 (`7db6350`) | PASS | |
| 13 | PF-11 (`2fd4628`) | PASS | the early exit cannot refuse what the old code accepted; refusal cost is flat in the draft size |
| 14 | PF-10 (`a3806f3`) | PASS | one wrong sentence in a comment (TF-10) |
| 15 | IS-01, IS-02 (`f3d3fe8`, `35ac1ab`) | PASS for what runs offline | `go mod verify`, `go mod tidy -diff` clean, CI follows `go.mod`; the SHA-256 and URL and the advisories NOT RUN (no network) |
| 16 | documents | PASS WITH FINDINGS | all 22 hashes exist and the ancestry claims are true; evidence is claimed that does not exist and counts disagree (TF-05, TF-10) |
| 17 | TS-01 as described | PASS | the description matches the code (`signals.go:131`, `00086` `tool_reconcile`, `freeze_sandbox`); two notes (TF-05 c, TF-11) |

## Findings

Ranked by severity. File and line are of `0155f7b`.

### TF-01 (MEDIUM) the `password_arg` rule destroys ordinary sentences that the table of `11a4ee4` kept

`internal/ai/redact.go:70` (the rule), added by `5836a31` for RT-08. Four of its alternatives need no command or SQL context: `\bidentified\s+by\s+` + any word of 4 or more
characters, `\bmachine\b[^\n]*?\bpassword\s+` + any word of 4 or more characters (the `.netrc` form), `\bzip\b[^\n]*?\s(?:-p|-a|--password)\s*`, and
`\bpassword\s+["'][^"'\n]{4,200}["']`. The match replaces the words as well as the value. The "keep" lists of `TestRedactionOfPasswordsGivenAsArguments`
(`internal/ai/redact_shapes_test.go:514-527`) name no sentence with these words, and I find none in the red team's report, whose 389-input corpus verified its patch (the report's
only `identified by` and `machine` are the secret forms, `docs/verification/REDACTION_REDTEAM.md:45, 156, 727, 992-994`).

Executed (probe `TestZZProseFalsePositives`, `redactText` against the copy of the table of `11a4ee4`): of 60 sentences that I wrote around the keywords, 18 differ;
17 of them are new destructions, the 18th is the intended RT-05 improvement. Output, the table of `11a4ee4` kept every input as written:

| input | output now |
|---|---|
| `Each customer is identified by email address.` | `Each customer is [REDACTED:password_arg] address.` |
| `A shipment is uniquely identified by tracking number.` | `A shipment is uniquely [REDACTED:password_arg] number.` |
| `Orders identified by number 4471 must be shipped first.` | `Orders [REDACTED:password_arg] 4471 must be shipped first.` |
| `Customer type: individual, identified by passport` | `Customer type: individual, [REDACTED:password_arg]` |
| `Define the machine entity with a password field.` | `Define the [REDACTED:password_arg]` |
| `The machine learning model validates the password strength of each signup.` | `The [REDACTED:password_arg] of each signup.` |
| `Show a password "hint" field on the sign-up form.` | `Show a [REDACTED:password_arg] field on the sign-up form.` |
| `Use zip codes with -plus4 extension when available.` | `Use [REDACTED:password_arg] extension when available.` |

The product's own repair instruction (`internal/ai/template.go:33`, "the proposal identified by previous_proposal_hash") is changed by the new table too (corpus run, below).
What the model is sent (probe `TestZZRequestSegment`, `BuildContext` on the shipped fixture): for the request `Make customers identified by email address and give them a
password field.` the request segment is `{"request":"Make customers [REDACTED:password_arg] address and give them a password field."}`. A draft whose entity label is
`Customer, identified by email address` shows `[REDACTED:password_arg]` in the definition body of the context, and an `update` that repeats what the model was shown is
refused by `ApplyProposal` (`internal/ai/patch.go:124`): `INVALID_DEFINITION ... the definition contains a redaction placeholder; text the model was not shown cannot
be written back` (probe `TestZZIdentifiedByLabelBlocksUpdates`). A definition with such a label can therefore not be updated by the AI builder until a person edits it.

Impact: a wrong result and a regression in a plausible path (a builder is asked for "identified by", "machine" and "password" all the time); the request the model
reads loses its meaning, and the proposals for the affected definitions are refused. No secret is exposed by it.
Fix direction: keep the command and SQL forms but give them their context: `identified by` only after `CREATE|ALTER (USER|ROLE)` or before a quote, `machine` only at the
start of a line (`.netrc`), `zip` only with `-P`, `password` + quote only after `SQL`/`WITH`/`LOGIN`; add the sentences above to a near-miss list in
`redact_shapes_test.go`; run the old and the new table over a corpus of ordinary requests before shipping a rule that takes words out.

### TF-02 (MEDIUM) AO-02 is closed for the three routes it names; the apply and confirm answers still hand out the proposal body

`internal/platform/access/ai_decide.go:22` (`aiApplyView{Proposal ...}`) and `:58` (the 200 of `aiApply`), `:62` and `:89` (`aiConfirmView{Proposal, Requirements ...}`);
`internal/ai/service_decide.go:191` and `:352` (`st.Get`, not `st.Summary`); `internal/ai/store.go:415` (`Get` selects `p.body,p.derived,p.diagnostics`) and `:346-348` (the
fields are in the JSON). `61eb1b4` composes `metadata.read` into the two creation routes and the read of one proposal (`ai.go:276-284`) and changes only the veto to `Summary`.
Proved by reading, not executed (the integration stack was not to be run): a stored role that holds `ai.read`, `ai.apply` and `metadata.write` but not `metadata.read`
(the role `ai_applier_meta` of `TestM14APIPolicyMatrix`, actor "applier with metadata.write", `tests/integration/m14_api_test.go:137`, `get=false`: the matrix itself
asserts that its `GET /proposals/{id}` is 403) lists the proposals (`ai.read`), learns the expected revision from the list and the sensitive aspects from the 409
`acknowledge_mismatch`, applies, and receives in the 200 `{"proposal": {operations with the full definitions}, ...}`. The matrix asserts only the status of that apply. The same
holds for `confirm`, whose answer also carries `Requirements.Body`, the whole stored requirement set, to a holder of `requirements.decide` without `requirements.read`.
The reply to the apply can be called an echo of what the caller just wrote; for an `update` it is the whole new definition, i.e. most of the old one that the role may not read.
Impact: the same boundary as AO-02 (`metadata.read`), through a role that can write the draft; the ledger and the plan say "fixed".
Fix direction: answer apply and confirm with `Store.Summary` and the revision (the caller can read the result where it may), or compose `metadata.read` and
`requirements.read` into them; add the body assertion to the matrix for the two roles; say it in ADR104 item 10.

### TF-03 (MEDIUM) verifier-v4 finds a window gated on one other field and passes a window gated on two

`internal/verification/verifier.go:291` (`variant[m.field] = m.raw`: a mined variant sets exactly one field beyond the requirement's), `minedValues` (`:466`ff), `literals.go`.
PF-01 was "a window gated on a field the requirement does not pin passes". Executed (probe `TestZZWindowGatedOnTwoFields`, `orderDocument` of the verifier tests plus a second
optional string field `field.order.region`, requirement with nothing pinned, the window `amount > 626 and amount < 749` as a first transition of `state.start` to `state.complete`):

```
window 626..749 for orders gated on 1 optional field(s):  outcome FAIL,  3 approval mismatches, 116 results      (note == gold)
window 626..749 for orders gated on 2 optional field(s):  outcome PASS,  0 approval mismatches, 155 results      (note == gold and region == EU)
```

The oracle says approval is required for 700 in both; the human cases of the control spec (501 and 750 with note vip) do not touch the window. ADR102 amendment 14 names as unchanged
limits "a window written with arithmetic, and a world the requirement pins away"; PF-06 names a field type the miner does not read. Neither covers two ordinary string
fields. Impact: a candidate author makes a bypass PASS by adding a second gate field; the verifier is "a mechanical floor", but amendment 14 and the ledger present the
gated window as closed. Fix direction: mine the combinations of the mined fields inside the budget (it uses 2,440 of 50,000 at 31 amounts, so there is room), or BLOCK a plan whose
amount-reading route is gated on two or more unpinned optional fields; at least list it as a residual next to PF-06.

### TF-04 (MEDIUM) the RT-05 exemption un-redacts passphrase-shaped credentials that the old table redacted

`internal/ai/redact.go:89` (`proseSpecification`) and `:109`. RT-05 (LOW) made "The token is 64-character hex." survive by exempting any value that matches
`^(?:[A-Za-z0-9]+-)+[a-z]{3,}$`: any alphanumeric word, a hyphen, a lower-case word of 3 letters or more. That is also the shape of a passphrase. Executed (probe
`TestZZProseSpecificationHidesPasswords`, values built at run time as a nine-character alphanumeric with a digit and a capital, a hyphen and a lower-case word):

| sentence | table of `11a4ee4` | table now |
|---|---|---|
| `The password is <value>.` | redacted | **kept** |
| `the admin password is <value> and it rotates` | redacted | **kept** |
| `The token is <value>.` | redacted | **kept** |
| `define('DB_PASSWORD', '<value>');` (comma form of RT-01) | kept (no such form) | **kept** |
| `..., 'secret', '<value>'` | kept | **kept** |

The exemption applies to the `is|was|are` connector and to the `name', 'value` comma form, so the claim of ADR104 item 8 ("`define('DB_PASSWORD', '...')` ... redacted") holds only for
values that do not have this shape. The commit message shows only `Summer-2026` as still redacted. Impact: a regression of a security filter for a plausible credential
shape, made to fix a LOW. Fix direction: replace the open pattern by a closed list of specification words (`character`, `bit`, `byte`, `digit`, `hex`, `signed`, `compliant`,
`encoded`, `hashed`, `based` ...) or require a leading number or a known algorithm name; test it with credential-shaped negatives.

### TF-05 (MEDIUM) the documents claim evidence that the repository does not contain, and say that nobody has seen fixes that a reviewer re-verified

a. `docs/migration/LINE_INTEGRATION_PLAN.md:146` gives as evidence of IS-01 to IS-03 "the toolchain downloads and verifies, `make check`, the gate below". The only gate below is the one on
`585499d` (`:112`), which predates Go 1.27.3, x/net 0.62.0 and every fix of this review. `f3d3fe8` says "The full gate on this tree is recorded in docs/migration/LINE_INTEGRATION_PLAN.md";
at `0155f7b` the plan names `d6ed47b` only as the ER-03 fix (`:143`) and records no "Gate on" later than `585499d` (`:112`). The integrator's gate was still running when this review was written, so the sentence
is transitional, but an owner who reads the head today is told that the toolchain and dependency change was gated.
b. "No independent reviewer has seen any change of this section" (`LINE_INTEGRATION_PLAN.md:154`) and "no reviewer has seen the fixes" (`M9R_FINDING_STATUS.md:261`), both written on 3 November, are contradicted for `a4379a8` and `55ee30e` by
`docs/verification/POSTFIX_INDEPENDENT_REVIEW.md:173-183` ("Fixes re-verified"), which ran the repros of PF-01, PF-02 and PF-03 against `55ee30e` and reported them fixed (and found the survivor PF-06). The header of ADR102
amendment 14 ("NOT independently reviewed") was true when it was written on 29 October and is stale now.
c. ADR101's amendment (`docs/adr/101-m11-sandbox-v2.md:144`) says the milestone agent "reproduced on the database" that an instance can become terminal "`CANCELLED` by a signal ... or
`TIMED_OUT` by its deadline" with an `UNKNOWN` ledger; ADR100 amendment I2 (`100-m10-tool-registry-v2.md:258`) says the deadline path "was not executed". The code supports the claim by reading
(`execution.go:101-107`, `:431-450`: `Termination != ""` skips the outstanding check), but "reproduced" holds for `cancel` only.
d. The ledger row of the post-fix review (`M9R_FINDING_STATUS.md:261`) counts "2 MEDIUM, 9 LOW"; the report lists PF-01, PF-09 and PF-11 as MEDIUM and the other eight as LOW (3 and 8), and the row itself calls PF-11 MEDIUM.
e. "fixed" in the ledger and the plan stands for AO-02, TS-04 and LE-02 without the residuals of TF-02, TF-07 and TF-08, and for PF-01 without TF-03; the residual lists of ADR104 Amendment C and ADR102 amendment 14 omit them.
Impact: an owner decides on the state of the gate, the review and the fixes from sentences that do not match the tree. Fix direction: record the gate on the final tree (or replace "the gate below" by "not yet run"), correct the four statements, and add the residuals.

### TF-06 (LOW) the card scan can leave twelve digits of a real card where the old rule redacted all of it

`internal/ai/redact.go:168-206` (`cardWindow`), `:209-235` (`redactCards`). The commit says the scan keeps "the same spans" as the old rule and adds the windows it hid. It tries the windows
that start inside a failed match first, and a window that passes both checks by chance is redacted and consumed. When a non-card number of 16 digits in four groups is directly followed by a card in four groups (the
old rule's tiling then ends exactly before the card and finds it), a chance window that starts in the first number and ends in the first group of the card is redacted first and the other three groups stay.
Executed (probes `TestZZRegressionPrefixNumberThenCard`, `TestZZOldSpansAreSpansOfTheScan`, `TestZZCardLeakEndToEnd`): 19,478 texts of a random 16-digit number in four groups then a card in four
groups: the old rule redacted the whole card in 19,452; the scan left digits of the card visible in 1,462 (7.5 %), at worst 12 of 16 digits. End to end through `redactText`, digits masked:

```
old: Reference dddd dddd dddd dddd [REDACTED:card] was charged.
new: Reference dddd [REDACTED:card] dddd dddd dddd was charged.      (12 digits of the card visible)
```

On 300,000 random texts the real old regular expression redacted 91,386 spans that pass its check: 91,254 are spans of the scan, 132 overlap another window of the scan, none is missing.
Impact: narrow (it needs a long number before the card and a chance window) and strictly better than the old rule elsewhere, which missed the card in every other tiling (PF-09);
but it is a regression of the stated "same spans", and 12 digits exceed the first six and last four that card-data rules allow to show. Fix direction: redact the union of the digit groups of every valid window of a chain (no digit of any valid
window stays), at no extra cost; assert "no digit of a valid window remains" in the property test.

### TF-07 (LOW) the TS-04 fix refuses invalid bytes only: a lone surrogate escape still becomes U+FFFD, and the AI provider adapter still substitutes

`internal/tools/connector.go:188-190` and `internal/tools/catalog.go:209` call `utf8.Valid` on the raw bytes. A JSON escape is plain ASCII, so `{"currency":"\ud800"}` passes and `decodeScalar`
(`catalog.go:528`) returns U+FFFD. Executed (probe `TestZZLoneSurrogateEscapes`): `\ud800`, `\udc00` and the escape of U+FFFD itself all decode to the string U+FFFD (bytes `ef bf bd`) and are accepted as a SUCCESS value, `\ud800A` to U+FFFD followed by `A`, and a member name with
`\ud800` is accepted as a different name; two different byte strings still collapse into one value, which is what TS-04 said it removed. The same holds for a catalog file. Outside `internal/tools`,
`internal/ai/provider_http.go:183-232` (`parseEnvelope`) says a JSON string payload "is used byte for byte" and decodes it with `json.Unmarshal`: probe `TestZZEnvelopeSubstitution`:
`{"payload":"...ab\xffcd..."}` is accepted with `err=nil` and the payload holds `ab`, U+FFFD (bytes `ef bf bd`) and `cd`; the model label of the envelope is substituted the same way. Impact: LOW (the tool answer and
the model answer are stored with a replacement character instead of refused); the ledger and ADR100 amendment I2 say "invalid UTF-8 ... is refused". Fix direction: refuse a decoded string that holds U+FFFD
the input did not spell, or scan `\uD800-\uDFFF` escapes for pairs before decoding; apply the same check in `parseEnvelope`; add the escape and the `\xff` payload to the tests.

### TF-08 (LOW) after `Start` the OpenTelemetry exporters still write raw environment values to standard error

`internal/observability/observability.go:50-54`: `Start` installs the error handler (`otel.SetErrorHandler`) and the Redis logger, but the exporters report a malformed `OTEL_EXPORTER_OTLP_*` variable through
the library's own logger (`internal/global`, `global.Error`, a `stdr` logger on `os.Stderr`), with the value (`otlptracehttp@v1.47.0/internal/envconfig/envconfig.go:98,166,181`, `otlpmetrichttp@v1.47.0/.../envconfig.go`).
Executed (probe `TestZZOtelEnvValueReachesStderr`, a child process that calls `Start(..., "http://127.0.0.1:1", logger)` after the fix; the value is a hash of a label, masked here):

```
OTEL_EXPORTER_OTLP_HEADERS=x-api-key=<SECRET>%zz       -> "msg"="escape header value" "error"="invalid URL escape \"%zz\"" "value"="<SECRET>%zz"
OTEL_EXPORTER_OTLP_ENDPOINT=http://svc:<SECRET>@collector:badport -> "msg"="parse url" "error"="parse \"http://svc:<SECRET>@collector:badport\": invalid port ..." "input"="http://svc:<SECRET>@collector:badport"
OTEL_EXPORTER_OTLP_HEADERS=<SECRET>                    -> "msg"="parse headers" "error"="missing '=" "input"="<SECRET>"
```

`scripts/runtime-env.mjs:21` passes `OTEL_EXPORTER_OTLP_ENDPOINT` to the backends. Impact: LOW (operator-supplied, only when malformed, at start-up, not reachable by a caller); but AGENTS.md says "no secrets, tokens, DSNs, raw driver errors ... in logs" and
LE-02 is marked fixed for "the third-party library". Fix direction: `otel.SetLogger(logr.Discard())` (or a sink that drops the values) next to the error handler, with a child-process test like the Redis one.
The other third-party writers I looked for held (below): goose's `Provider` logs only when verbose, go-redis's logger is atomic and every line goes through it, pgx and go-oidc write nothing.

### TF-09 (LOW) forms next to the ones RT-01, RT-03 and RT-08 close still reach the provider

Not claimed by the commits, but the same family. Executed (probes `TestZZNearClaimMisses`, `TestZZWrappedKeyVariants`; values of 12 alphanumerics with a digit, built at run time); the value survives in full for:
the PHP and Ruby hash rocket (`'password' => '<value>'`, `:password => "<value>"`), the Go short declaration (`dbPassword := "<value>"`), `password == "<value>"`, escaped quotes (`password=\"<value>\"`),
a quoted JSON value of 7 characters (`{"password": "<value>"}`; the `:` form needs 8 characters and a digit or symbol, `redact.go:101-104`, which neither the commits nor ADR104 item 8 state) or of 14 letters,
`curl -u<user>:<value>` (no space), `gpg --passphrase <value>`, `az login -u <user> -p <value>`, `ldapsearch -w <value>`, a hard-wrapped key whose continuation line carries a quote prefix (`> `), a hard-wrapped key
over five lines (the rule joins three; documented), and the continuation followed by prose (documented). They were redacted: the same forms with `=`, `: ` and 8 or more characters, YAML blocks, `export`, `set`, `$env:`,
connection strings (`Password=`, `Pwd=`), query strings, CRLF and tab-indented wraps, wraps over three and four lines.
Fix direction: list these as residuals in ADR104 item 8 (RT-06, RT-07 and RT-09 are listed, these are not) or add the hash rocket and `:=` to the separator.

### TF-10 (LOW) smaller contradictions between a sentence and the tree

(a) ADR104 item 12 says it "extends A8 and item 3"; the repetition of the result exchange is item 4 (M14R-03), item 3 is the usage cap (`104-...md:617-640`).
(b) The comment of `redactCards` (`redact.go:208`) says a chance window is redacted "about one window in two hundred"; measured over 2,000,000 random windows per length: 1 in 100 (13 digits), 374 (14), 214 (15), **38 (16)**, 173 (17), 174 (18), 63 (19), about 1 in 98 on average; the commit message and ADR104 item 9 say "once in 40 windows of 16 digits".
(c) `internal/platform/app/operator_env_test.go:48` says the six names are pinned "as the AGENTS.md rule states them" and the failure message tells the reader to update AGENTS.md; AGENTS.md names none of the six (it says "Keep operator credentials out of runtime subprocesses", line 33).
(d) ADR104 item 8 lists "`the admin password is ...`" among the forms RT-08 added; the `is|was|are` connector exists at `11a4ee4`.
(e) the handoff (`docs/migration/AGENT_HANDOFFS.md:335`) says "Run `make setup` or install the toolchain"; `make setup` runs `scripts/bootstrap.sh`, which skips the download when `scripts/go.sh version` already succeeds (`:5`), so an existing `.local/toolchains/go` of 1.27.2 is kept and the build relies on the toolchain switch of `go.mod` (see TF-11).

### TF-11 (LOW) behaviour notes on ER-04, ER-03, IS-01, IS-03, TS-01

* ER-04: with a full queue every try fails at once, so the six tries end after 100 + 200 + 400 + 800 + 1500 ms = 3.0 s of the 6 s budget and the 8 s context; a burst that lasts longer loses the answer exactly as before (a bare 503, no audit); the
  pauses have no jitter (all waiting answers retry together); `aiResultPause` (`ai.go:586`) shifts by `attempt-1` and would go negative at 60 attempts (6 today). Documented: "not tested at the burst size". The statement "waits for a full admission queue" holds for about 3 s.
* ER-03: `SHUTDOWN_TIMEOUT` is parsed by `config.Load` (`config.go:139-144`), which the API, the worker and the dispatcher call, so a plain `SHUTDOWN_TIMEOUT=30` (a common convention) now stops all three from starting
  (the message does not echo the value); only the API uses it. By reading: the 80 s covers a request that was in flight at SIGTERM (70 s from its admission); one that still waits in the admission queue and starts its provider phase after it can need more and is then cut by `Close`.
* IS-03: the chmod follows the append (`m6r-env.mjs:20`, `m8-env.mjs:21`), so the credentials are in a file with the old mode for the time between the two calls, and it follows a symlink; `chmod` before the append is the smaller exposure.
* IS-01: see TF-10 (e). A checkout with an old local toolchain is repaired by the `GOTOOLCHAIN=auto` switch (verified by the checksum database), not by the pinned checksum; with `GOTOOLCHAIN=local` the build fails with "go.mod requires go >= 1.27.3".
* TS-01 (amendment I2): the reproduction is "a throwaway test, not kept"; no artefact lets the next owner re-run it (a kept failing test behind a skip or a tag would). `workflows.cancel` is open to every non-viewer role (`internal/policy/policy.go:112`), which ADR101 says and ADR100 does not: one cancel of a waiting instance strands the row and blocks the freeze of the sandbox for good.
* TS-02: `catalogFor` has no unit test (the proof is in the integration tests, which were not run here).

### TF-12 (LOW) test weaknesses

* `TestCardScanAgreesWithTheDefinition` (`redact_cards_test.go:167-215`) asks only whether the text holds a card window and whether one remains in the output (`windowOracle(out)`, `:202`); it cannot see TF-06 (the 12 digits that stay are not a window).
  Its oracle shares only `cardIssuer` and `luhn` with the code, as the commit says (checked), and my reference that returns the redacted text agrees with the scan on 400,000 texts, so the scan is right by its definition; the definition is what TF-06 is about.
* `TestCardNumbersBesideOtherNumbers`: the case "inside a run of short groups" has `keeps: ""` (`:61`), and `strings.Contains(out, "")` is always true, so the neighbours of that card are not asserted.
* `TestBuildContextRefusalCostsNoMoreThanAContextThatFits` is a ratio test with a threshold of 3 (`context_test.go:371`); under the load of this host I measured 1.7 to 2.0 (refused 181, 204, 185 ms against fits 93, 122, 92 ms), the claim is 1.1 on a quiet host; a heavier host can flake it.
* The TS-04 tests have the five raw-byte cases but not an escape (`\ud800`); the matrix of `TestM14APIPolicyMatrix` checks the status of apply and confirm, not their bodies (TF-02).

## Attacks that held

Each line is an executed probe unless it says "read".

1. **Card scan against the definition** (`TestZZCardScanEqualsReferenceOutputs`): a reference that scans bytes left to right, takes at each digit after a non-word byte the longest end of 13 to 19 digits with single separators and a non-word byte after, and shares only `cardIssuer` and `luhn`
   with the code: 400,000 random texts (132,436 hold a window) of cards in four spellings, random digit groups, `é`, no-break space, `٣`, tab, `\r\n`, `_`, `[` `]`, chains at the very start and end of the text: 0 differences in the output and in the count.
2. **Card scan, boundaries and offsets**: byte offsets are of `s` throughout; `\b` is the ASCII word class in the old regular expression, the reference and `wordByte` alike; a non-ASCII neighbour is a boundary; no window spans a two-space gap or an underscore. Held in the run above.
3. **Card scan is linear** (`TestZZLinearTiming`, `TestZZCardScanManyHits`): single digits, two-digit groups, four-digit groups, hyphen chains up to 4 MB double in time with their size (single digits: 49, 85, 167, 351 ms at 200, 400, 800, 1,600 KB); 200,000 cards in one chain (4 MB, 200,000 replacements) in 124 ms.
4. **Negative controls of PF-09**: `singleMatchCard` behind the rule: 11 of the 12 cases and the property test fail (`TestZZNegativeControlOldRule`), as the commit says. The new redaction tests against the `redact.go` of `11a4ee4`: six of seven fail (`TestCardNumbersBesideOtherNumbers`, `...QuotedKeysAndCompoundNames`, `...AKeyWrappedAfterAFewCharacters`, `...VendorTokenFormats`, `...PasswordsGivenAsArguments`, `...KeepsSentencesAboutCredentialFormats`); the seventh is the M14R-08 list that the old table already covered.
5. **Cost of the redactor on adversarial input** (`TestZZRedactorCost`): 16 shapes (prose, single-spaced digits, `password=` repeated, key-shaped lines and long lines, wrapped paragraph lines, one long word run, "the password is" repeated, `machine` repeated, `password "` and `token="` openers, a long value after a name, many names then one long value, JSON-like) at 32, 64, 128 and 256 KiB: every shape doubles with its size, 0.5 to 4.0 microseconds a byte, worst 1.06 s for 256 KiB of `token="` openers on a loaded host; no shape is super-linear (the `redactSplitKeys` joins are bounded by three lines).
6. **Idempotence and determinism** (`TestZZIdempotencyFuzz`): 300,000 random texts built from the fragments of the changed rules (names, separators, quotes, commands, newlines, card spellings, markers, look-alike characters, generated secrets): `redactText(redactText(x)) == redactText(x)` and two runs agree; 0 failures. (100,000 in a first run.)
7. **Corpus of the repository** (`TestZZCorpusNewRedactions`, `TestZZCorpusWholeFiles`): 940 files, 199,689 lines, 9,789,353 bytes through the table of `11a4ee4` and the table now: 99 lines differ: lines of the redactor's own sources, tests and reports (they quote secret-shaped text), code lines such as `const ownerToken = provision(...)` and `var operatorCredentials = []string{`
   (the intended RT-02 reach: a name that ends a longer word), and the repair template (TF-01); whole files: 26 differ, and one line differs by its neighbours (the new table keeps `token = uuid(t)` that the old one redacted).
8. **Wrapped keys** (`TestZZWrappedKeyVariants`): LF, CRLF, a tab-indented tail, trailing blanks, three and four lines: redacted; five lines and a quote prefix are TF-09; the tail followed by prose is the documented residual.
9. **verifier-v4**: `moneyText` over 400,000 random amounts (0 to 18 decimals, 0, 2, 3, 4 minor units, negative): exact round trip, the shape the data layer accepts, no trailing zero beyond the currency's own, 0 bad (`TestZZMoneyTextProperty`); work budget (`TestZZWorkBudget`): honest plan 98 of 50,000 steps, 31 amounts plus a mined field 2,440, so the budget does not block an honest plan (the budget is shared by all requirements of one `Verify`; I could not quickly build an input with several threshold requirements, so that case is by arithmetic only: 2,440 steps for the largest plan I built, about twenty such requirements before the bound); the literal amounts, the mined values and the probes are sorted, so the report is deterministic;
   the shipped tests `TestAWindowGatedOnAnotherFieldIsFound`, `TestWindowsBetweenTheMinorUnitsAreFound`, `TestMoneyText...`, `TestLiteralProbes...`, `TestProbeSetIsPinnedToTheVerifierVersion`, `TestAPlanWithTooManyAmountsIsBlocked` pass; a candidate staged under `verifier-v3` reads stale (`verification.VerifierVersion` is part of `promotion/dependencies.go:134` and `bundle2.go:635`).
10. **PF-03**: `TestTokenCheckBeforeAdmission` passes (10 cases); `VerifyRemaining` fails only where `Verify` fails (`identity.go:109-117`), so the new 401 `unauthenticated` is the answer the front door gives to the same token; the check runs after the strict body is read and before `Admit` (the reservation) and the provider.
11. **AO-02, order and roles**: the composed `metadata.read` is the first check of `API.ai` after the 503 for an unconfigured builder, for the two creations and the read of one proposal, before `aiBody`, the token check, `Admit` (reserve) and the provider; its resource is `{Type: "metadata", AppID: app}` like the metadata routes; the four built-in roles hold `metadata.read` for every `metadata` resource (`policy.go:133-136`) and legacy roles go through the native decision, so no built-in role changed; the list selects `summaryColumns` only; the veto answers `Summary`; the refused read is audited as `ai.read.rejected` through the generic rejection path (`access.go:403-425`).
12. **TS-02**: every non-test call of `compiler.Compile` was listed: `metadata.go:112`, `preview.go:82`, `releases.go:425` and `ai.go:362` use `catalogFor(access)`; `datasets.go:80` compiles with the platform view and answers only 400 or 409 for every failure (a tool that does not exist and a hash that does not match are one answer), so it tells nothing; the AI context's tool list is `Summaries(tenant)`, which filters by `AllowsTenant`; `staleness` and `definitionsOf` look up pins the tenant's own program holds; `toolEffect` reports pins of the tenant's own IR; `catalog()` returns a copy, so setting `Tenant` shares nothing; a tenant on the list, `"*"`, and no tenant (tooling) compile as before and the IR bytes equal the platform compile's; the restricted answer equals the unregistered one by `reflect.DeepEqual`; `TestToolRestrictedToOtherTenantsIsUnknownToTheCompilingTenant` and `TestTenantIsNotPartOfTheCatalogHash` pass. No compile cache is shared between tenants (the recompute memo is keyed by the stored bytes and the base program).
13. **TS-04 negative control**: with both `utf8.Valid` checks neutralised, `TestResponseBodyLimitsAndValidation` returns `SUCCESS` with `n-` followed by U+FFFD for four sequences and `TestLoadCatalogRefusesInvalidUTF8` fails, as the commit says; restored.
14. **LE-02**: the `SetLogger` call neutralised: `TestRedisClientLogsOnlyThroughSlog` fails with the library's line (`redis: ... failed to dial after 5 attempts: dial tcp 127.0.0.1:1: connect: connection refused`), so the child-process test is a real detector; `redis.SetLogger` stores into an atomic pointer in v9.23.0 (`internal/log.go:46-78`), `redisLogger` keeps its state in an `atomic.Int64` with a compare-and-swap and returns on a nil logger; every library line goes through `internal.Logger` (none uses `log` or `os.Stderr` directly); goose's `Provider` logs only with `WithVerbose` (`cmd/migrate` does not set it); pgx and go-oidc write nothing; the four CLIs print fixed messages (`postgres.Open` hides the DSN).
15. **ER-04**: `ErrAdmissionFull` is returned only by `Admission.Acquire`, before the transaction begins; the only place it can follow an effect is the second transaction that audits a refusal, after the first was rolled back, so a repeated exchange cannot store or charge twice; an unknown COMMIT outcome is not a `pgconn.PgError` and is not retried; `RetryWithin` is `RetryPaced` with `ConflictPause`, the same 5 to 25 ms and the same order of checks; the shipped tests pass (`TestRetryPacedRidesOutAFullAdmissionQueue` against a real `Admission`).
16. **ER-03**: `http.Server` has no `BaseContext`, so a request context is not derived from the signal context and SIGTERM does not cancel a running creation; `Shutdown` returns when the running requests have finished (an idle API stops at once), `Close` only follows a timeout; only the API reads `ShutdownTimeout` (`app.go:313`), the worker and the dispatcher wait on their own loops; `TestShutdownTimeoutFollowsTheAIBuilder` passes; the launcher test passes.
17. **RB-01** (`TestZZProbeEdgeCases`, `-race`): probes that `panic(nil)`, call `runtime.Goexit`, panic with an error that holds a secret-shaped text and one healthy probe, 20 rounds of `GET /readyz`: always 503 with the healthy probe `ok`, no panic reaches the process, goroutines 2 before and 2 after, the panic text is not in the log; each goroutine sends exactly once into a channel of capacity `len(probes)`, so no send blocks and none panics twice; `TestReadinessProbePanicFailsOnlyThatProbe` passes with `-race`.
18. **IS-03**: `node --test scripts/runtime-env.test.mjs`: 15 of 15 pass, including the IS-03 case (the test checks that credentials were appended, so it cannot pass on a no-op) and the ER-03 pass-through; the chmod is unconditional, so a `.env` with no additions is tightened too.
19. **PF-11**: bodies are re-encoded by `redactedJSON` (compact, HTML characters already escaped), so `json.Marshal` of the `RawMessage` embeds exactly `len(body)` bytes, and the application segment, hence the bundle, is at least the sum of the bodies; the early exit cannot refuse what the old code accepted (`TestZZRefusalCostByDraftSize`: a draft stops fitting at 70 definitions; refusals of 140, 280, 560 and 1,120 definitions cost 225, 202, 173 and 179 ms, flat; a context that fits costs 88 ms). Negative control: with the early exit removed the shipped test fails (refused 475 ms against 91 ms, 5.2 times; the commit says 4.5); restored. One difference outside any real input (by reading): an undecodable definition later in the list used to return its encode error and now returns `ErrContextTooLarge` first.
20. **Build**: `GOPROXY=off GOFLAGS=-mod=mod gt mod verify` says "all modules verified"; `GOPROXY=off gt mod tidy -diff` prints nothing (rc 0): `go.mod` and `go.sum` are tidy and hold the `h1:` and `/go.mod` lines of x/net 0.62.0, x/sync 0.24.0, x/sys 0.49.0 and x/text 0.43.0 and no line of the old versions; CI reads `go-version-file: go.mod`; `gofmt -l` over `internal cmd tests` prints nothing; the strings `1.27.2` that remain are the README (acknowledged), ADR013 and ADR021, the zoneinfo notes of `internal/runtime/rules` and historical reports; no code, script or workflow depends on them.
21. **Documents**: the 22 hashes the new text names (`02af0e5 11a4ee4 2b25e7c 2f27b01 2fd4628 35ac1ab 3be63ec 491edc6 55bc0b3 55ee30e 5836a31 585499d 61eb1b4 7db6350 a3806f3 a4379a8 d6ed47b e3c954c e555654 f3d3fe8 6673055 4937324`) are commits; the tips of the four lane branches and of `origin/remediation/m9r-foundation` (`bf70752 311c63e fc52dba 60da6f2 e0fb83d`, the last of 20 October) and `main` (`9da3f5f`) are ancestors of `0155f7b`; 16 report branches were merged; the thirteen reports say "no HIGH"; every test the new text names exists (`TestZZEdgeProcesses` is named as not in the repository); `README.md`, `AGENTS.md` and `CLAUDE.md` are unchanged, as the handoff says.

## NOT RUN

* Everything with `-tags=integration`, Docker, PostgreSQL or Redis (the brief forbids it): `TestM14APIPolicyMatrix`, `TestM14APIAskingAndReadingNeedMetadataRead`, `TestM13ToolPinTenantListIsPartOfThePinAndOfTheBuildCheck`, `TestM10Catalog...`, the audit-row assertions of AO-02, the end-to-end claims of ER-04, the database reproduction of TS-01 and its deadline path, migrations. TF-02 is therefore proved by reading, not executed; the negative controls "without the composition the matrix fails" and "the preview answers 200 with the other tenant's IR" are taken from the commit messages.
* The SHA-256 and the URL in `scripts/bootstrap.sh:11-12` against the Go release metadata, and the advisory identifiers (GO-2026-6809 and the seven of x/net), `govulncheck`: no network. The cached toolchain module `golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64` exists and `go mod verify` passes, which does not verify the tarball checksum.
* A real `cmd/api` process under SIGTERM with a running paid phase (ER-03); a proxy or an orchestrator; the burst of the edge rehearsal (more than 132 database requests at one moment).
* `make check`, `make test*`, the browsers, the fuzz targets, `go vet` of all packages, `-race` over more than one small package (two runs, `internal/platform/httpserver`), the performance gates.
* The TIMED_OUT path of TS-01 (read only).
* A negative control of every shipped test: done for PF-09, RT-01 to RT-08 (as a group), PF-11, TS-04 and LE-02; not for TS-02, ER-04, RB-01, PF-03, ER-03, IS-03.

## Commands

Run from the root of the review checkout. `gt` is `/home/user/bin/gt`. The probes are in the appendix; they were files named `zz_review_*_test.go` in the package named, deleted afterwards.

```
git checkout --detach 0155f7b
git log --format='%h %ad %s' --date=short 9da3f5f..0155f7b --first-parent
git diff --stat fb5a972 0155f7b
git diff --stat 11a4ee4 0155f7b -- . ':(exclude)docs'
git show <hash>                      # each of the 19 commits; also: git show 11a4ee4:internal/ai/redact.go, 11a4ee4:internal/ai/redact_fold.go
git diff fb5a972 0155f7b -- docs/adr/100-m10-tool-registry-v2.md           # and 104, docs/migration/M9R_FINDING_STATUS.md, AGENT_HANDOFFS.md, M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md, docs/adr/101-m11-sandbox-v2.md
git cat-file --batch-check < hashes.txt                                    # the hash-like tokens of the added documentation lines
git merge-base --is-ancestor <tip> 0155f7b                                 # bf70752 311c63e fc52dba 60da6f2 e0fb83d 9da3f5f
git for-each-ref --format='%(refname) %(objectname:short) %(committerdate:short)'
gt version
gt test -count=1 -run '<regex>' -v ./internal/ai                           # regexes: the probes of Appendix A to C and the shipped tests below
gt test -count=1 -run 'Redact|Card|Redaction|Fold|FixedPoint|Idempot' ./internal/ai                       # with the probes present: ok, 160 s
gt test -count=1 -run 'TestBuildContextRefusalCostsNoMoreThanAContextThatFits' -v ./internal/ai          # three times
gt test -count=1 -run 'TestTokenCheckBeforeAdmission' -v ./internal/platform/access
gt test -count=1 -run 'aiResult|AIResult|Retriable|Pause' -v ./internal/platform/access
gt test -count=1 -run 'Retry|Pause|Result' -v ./internal/platform/postgres
gt test -count=1 -run 'Shutdown' -v ./internal/config
gt test -count=1 -v -run 'Tenant' ./internal/compiler
gt test -count=1 -run 'TestAWindowGatedOnAnotherFieldIsFound|TestWindowsBetweenTheMinorUnitsAreFound|TestMoneyText|TestLiteralProbes|TestProbeSetIsPinned|TestAPlanWithTooManyAmountsIsBlocked' -v ./internal/verification
gt test -count=1 -race -run 'TestReadinessProbePanicFailsOnlyThatProbe' -v ./internal/platform/httpserver
/tmp/node24/node-v24.22.0-linux-x64/bin/node --test scripts/runtime-env.test.mjs
GOPROXY=off GOFLAGS=-mod=mod gt mod verify
GOPROXY=off gt mod tidy -diff
/home/user/m14-int/.local/gopath/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.3.linux-amd64/bin/gofmt -l internal cmd tests
gt env GOMODCACHE GOFLAGS GOTOOLCHAIN      # the module cache was read, never written by hand
# read-only searches (ripgrep) over the checkout, and over the module cache for go-redis v9.23.0, goose v3.28.0 and the OpenTelemetry modules:
#   os.Stderr | log.Print | log.Fatal | global.Error | global.Warn | internal.Logger | SetLogger | stdLogger | utf8 | ErrorLog | 1.27.2 | catalogFor | compiler.Compile( | ToolCatalog | Lookup( | Current(
# an aborted search for a local go1.27.3 archive: find / -xdev -name 'go1.27.3*' ...   (stopped after 120 s, nothing read from it; the module cache listing replaced it)

# probes (the package of each is named in the appendix)
gt test -count=1 -run 'TestZZCardScanEqualsReferenceOutputs|TestZZOldSpansAreSpansOfTheScan|TestZZRegressionPrefixNumberThenCard|TestZZCardLeakEndToEnd|TestZZLinearTiming|TestZZCardScanManyHits|TestZZChanceRates|TestZZNegativeControlOldRule' -v ./internal/ai
ZZ_BIAS=0 gt test -count=1 -run 'TestZZRegressionPrefixNumberThenCard' -v ./internal/ai ; ZZ_BIAS=1 ...              # uniform and biased prefixes
gt test -count=1 -run 'TestZZRedactorCost|TestZZProseFalsePositives|TestZZRequestSegment|TestZZIdentifiedByLabelBlocksUpdates|TestZZProseSpecificationHidesPasswords' -v ./internal/ai
gt test -count=1 -run 'TestZZNearClaimMisses|TestZZWrappedKeyVariants|TestZZEnvelopeSubstitution|TestZZRefusalCostByDraftSize' -v ./internal/ai
gt test -count=1 -run 'TestZZCorpusNewRedactions|TestZZCorpusWholeFiles' -v ./internal/ai
ZZ_N=300000 gt test -count=1 -run 'TestZZIdempotencyFuzz' -v ./internal/ai
gt test -count=1 -run 'TestZZWorkBudget|TestZZWindowGatedOnTwoFields|TestZZMoneyTextProperty' -v ./internal/verification
gt test -count=1 -run 'TestZZLoneSurrogateEscapes' -v ./internal/tools
gt test -count=1 -run 'TestZZOtelEnvValueReachesStderr' -v ./internal/observability
gt test -count=1 -race -run 'TestZZProbeEdgeCases' -v ./internal/platform/httpserver

# scratch package of the old table (deleted): 
mkdir internal/zzoldredact; git show 11a4ee4:internal/ai/redact.go > internal/zzoldredact/redact.go; git show 11a4ee4:internal/ai/redact_fold.go > internal/zzoldredact/redact_fold.go
sed -i 's/^package ai$/package zzoldredact/' internal/zzoldredact/*.go          # delete the metadata import and holdsRedaction; add: func RedactText(s string)(string,int){return redactText(s)}

# negative controls (each reverted with git checkout -- <file>)
sed -i '189s/!utf8.Valid(raw)/false \&\& !utf8.Valid(raw)/' internal/tools/connector.go; sed -i '209s/!utf8.Valid(raw)/(false \&\& !utf8.Valid(raw))/' internal/tools/catalog.go
sed -i 's/\tredis.SetLogger(&redisLogger{logger: logger})/\t_ = redis.SetLogger/' internal/observability/observability.go
edit internal/ai/context.go: replace the `bodies += len(body); bodies > MaxContextBytes` block by `_ = bodies`
git show 11a4ee4:internal/ai/redact.go > internal/ai/redact.go          # then the seven redaction tests

# at the end
rm -f internal/*/zz_review_*_test.go internal/platform/httpserver/zz_review_*_test.go; rm -rf internal/zzoldredact      # the probes and the scratch package
git diff --stat                                                               # empty: no tracked file differs from 0155f7b
git checkout -b review/takeover-fixes
git add docs/verification/TAKEOVER_FIXES_INDEPENDENT_REVIEW.md
git commit                                                                    # this file only
```

## Appendix: probe sources

Condensed; helpers (`alnum`, `luhnNumber`, `groups`, `material`, `fixtureDoc`, `ctxInput`, `build`, `segment`, `survives`, `cardRule`, `singleMatchCard`, `bulkDraft`) are the tests' own.

**A. TF-06, package `ai`**

```go
func TestZZRegressionPrefixNumberThenCard(t *testing.T) {
	rnd := rand.New(rand.NewSource(42))
	card := luhnNumber("4", "zz-regress-card", 16)
	cg := strings.Fields(groups(card, " "))
	visible := func(out string) (c int) {
		for _, g := range cg { if strings.Contains(out, g) { c += len(g) } }
		return
	}
	total, oldWhole, newLeak := 0, 0, 0
	for k := 0; k < 20000; k++ {
		var gs []string
		for g := 0; g < 4; g++ { gs = append(gs, fmt.Sprintf("%04d", rnd.Intn(10000))) }
		text := "Account " + strings.Join(gs, " ") + " " + strings.Join(cg, " ") + " end"
		var o, n int
		oldOut, newOut := singleMatchCard.apply(text, &o), cardRule(t).apply(text, &n)
		total++
		if o > 0 && visible(oldOut) == 0 { oldWhole++; if visible(newOut) > 0 { newLeak++ } }
	}
	t.Logf("%d texts: old redacted the whole card in %d, the scan left digits visible in %d", total, oldWhole, newLeak)
}
// observed: 19478 texts: old redacted the whole card in 19452, the scan left digits visible in 1462 (worst 12 digits)
```

**B. TF-01, package `ai`**: `out, _ := redactText(s)` for the sentences of the table; for the old table the scratch package `zzoldredact.RedactText(s)`; the label case:
`doc := fixtureDoc(t, exWorkflow)`; set the label of the first `entity` definition to `Customer, identified by email address`; `c := build(t, ctxInput(t, doc, "Rename the entity label to Client."))`; read
`segment(t, c, SegmentApplication).Text` (it holds `[REDACTED:password_arg]`); unmarshal the definition of that id and the hash from it; `ApplyProposal(doc, PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{{Op: "update", Target: id, ExpectedHash: hash, Definition: &shown}}})`
returns the conflict `INVALID_DEFINITION`.

**C. TF-04, package `ai`**

```go
w := alnum("zz-spec-a", 9)                      // a digit and a capital by construction
value := w + "-" + lowerLetters(alnum("zz-spec-a-w", 8)) + "ing"
for _, tpl := range []string{"The password is %s.", "the admin password is %s and it rotates", "The token is %s.", "define('DB_PASSWORD', '%s');"} {
	text := strings.Replace(tpl, "%s", value, 1)
	o, _ := zzoldredact.RedactText(text); n, _ := redactText(text)
	t.Log(strings.Contains(o, value), strings.Contains(n, value))   // false true, false true, false true, true true
}
```

**D. TF-03, package `verification`**

```go
doc := orderDocument(orderOptions{})
region := metadata.Definition{ID: "field.order.region", SchemaVersion: doc.SchemaVersion, Namespace: doc.Namespace, Label: "field.order.region", Kind: "field",
	Field: &metadata.Field{EntityRef: "entity.order", Type: "string", Required: false}}
doc.Definitions = append(doc.Definitions, region)
// add "field.order.region" to the Fields of the entity definition
amount := metadata.Expression{Op: "field", FieldRef: "field.order.amount"}
lit := func(a string) metadata.Expression { return metadata.Expression{Op: "typed_literal", ValueType: "money", Value: json.RawMessage(`{"amount":"` + a + `","currency":"USD"}`)} }
str := func(f, v string) metadata.Expression { return metadata.Expression{Op: "eq", Args: []metadata.Expression{{Op: "field", FieldRef: f}, {Op: "literal", Value: json.RawMessage(`"` + v + `"`)}}} }
args := []metadata.Expression{{Op: "gt", Args: []metadata.Expression{amount, lit("626")}}, {Op: "lt", Args: []metadata.Expression{amount, lit("749")}}, str("field.order.note", "gold")}
// gates == 2: args = append(args, str("field.order.region", "EU"))
// prepend metadata.Transition{To: "state.complete", Condition: &metadata.Expression{Op: "and", Args: args}} to the transitions of "state.start"
open := requirementSet(thresholdRequirement("po-approval", nil))
r := mustVerify(t, input(compileDocument(t, doc, compiler.PlatformCatalog()), open, controlSpec(open)))
// observed: one gate FAIL (3 mismatches, 116 results); two gates PASS (0 mismatches, 155 results)
```

**E. TF-07, package `tools` and `ai`**

```go
fields, ok := parseFlatObject([]byte(`{"currency":"\ud800"}`))      // ok == true
v, err := decodeScalar(Field{Type: "string", MaxLength: 64}, fields["currency"])   // the string U+FFFD, nil
env, err := parseEnvelope([]byte("{\"payload\":\"{\\\"label\\\":\\\"ab\xffcd\\\"}\"}"))   // err == nil, the payload holds ab, U+FFFD, cd
```

**F. TF-08, package `observability`**: a helper test that returns at once unless `ZZ_OBSERVABILITY_HELPER` is set and otherwise calls `Start(ctx, "zz", "test", "http://127.0.0.1:1", slog.New(slog.NewJSONHandler(os.Stdout, nil)))` and `Shutdown`;
the parent runs `exec.Command(os.Args[0], "-test.run=^TestZZOtelHelperProcess$")` with `OTEL_EXPORTER_OTLP_HEADERS` or `OTEL_EXPORTER_OTLP_ENDPOINT` set to a value built from `hex(sha256("zz-otel-secret")[:8])` and
checks whether that value is in the child's standard error.

**G. TF-02** has no probe: the lines are `ai_decide.go:22,58,62,89`, `service_decide.go:191,352`, `store.go:346-348,411-416`.
