# Post-fix independent review (changes after 0e13abe, redaction corpus excepted)

Reviewer worktree: `/home/user/review-postfix`, branch `review/integration-postfix`, from the pinned commit
`11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`). Scope: the eight sections of brief 60.
Result words: PASS / FAIL / NOT RUN only. Every "held" names the attack that was executed.

## Progress

Updated 3 November 2026 after the interruption (the host rebooted; the stacks were restarted). The heavy lock was contended by four other agents, so the integration runs below were queued for more than 20 minutes.

| # | scope | state |
|---|---|---|
| 1 | 62cc527 expiring-token admission check | done; PF-03 (LOW, fixed in 55ee30e and re-verified); no other finding |
| 2 | 3c27378 verifier-v3 | done; PF-01 (MEDIUM) and PF-02 (LOW), both fixed in a4379a8 and re-verified; PF-06 (LOW) survives the fix |
| 3 | bca9d98 operator credentials refused at start | done; PASS; PF-10 (LOW, test gap) |
| 4 | db9c2fa F-05 residual test | done; PF-07 (LOW) |
| 5 | b88a465 version-gate scan | done; PF-04 and PF-05 (LOW) |
| 6 | 294e597 migrate status | done; PASS (populated disposable database, 15 scenarios) |
| 7 | 80a582f body-budget benchmarks | done; PASS; PF-08 (LOW, comment) |
| 8 | regression subset | `make check` PASS; unit packages `-race` PASS; integration `TestF05\|TestM14R\|TestM13R` PASS; `TestM14API\|Adv\|Ports` queued (see Gate subset) |
| 0 | redactor code defects | done; PF-09 (MEDIUM, card rule) and PF-11 (MEDIUM, cost of a refused creation with a large draft) |

## Verdict

Interim: PASS WITH FINDINGS. No HIGH finding. One MEDIUM against the pinned tree that is fixed on the integration branch (PF-01) and one MEDIUM that is not (PF-09, the card rule).

## Findings

### PF-01 (MEDIUM) `verifier-v3` literal probes are not repeated for the mined values of other fields: a window gated on a second field passes

`internal/verification/verifier.go` `minedProbe` (the five ids `below`, `at`, `above`, `plus1e6`, `minus1e6` are the only probes repeated with the values
that other conditions compare against) and `literals.go` (ids `lit-below-`, `lit-at-`, `lit-above-`, `lit-mid-`). The fixed probes get the mined variants
(review F4/F5: "an exemption such as note != vip is invisible in the all-null world"); the new literal probes do not. So an exemption window
that is written with literals AND gated on a field the requirement does not pin is not found, although the same window without the gate is.

Repro (unit, no database; probe file `zz_review_pf2_test.go` is not committed, the shape is in the report): `orderDocument(orderOptions{})`, then add to
`state.start` a first transition `-> state.complete` with the condition `amount > 626 USD AND amount < 749 USD AND note == "gold"`; requirement
`GT 500.00 USD`, `note` not pinned in the requirement context; control acceptance spec of `verifier_test.go`.
Observed: `Verify` returns `PASS`, 58 results, no finding. The same window without the `note` condition: `FAIL`
(`lit-above-626.01`, `lit-below-748.99`, `lit-mid-687.50`). Same result for 501..502 and 600..900 gated on `note == "gold"`. With the requirement pinning
`note = "regular"` the gated window passes by design (documented: "a world the requirement pins away").
Direction, verified by a temporary mutation: let `minedProbe` also select ids containing `/probe/lit-`; the gated windows then fail with
`.../lit-above-626.01/field.order.note="gold"` and the unpinned and pinned cases behave as before. Not stated as a residual in ADR102 amendment 13
(it names arithmetic, "another field" and a pinned world).

### PF-02 (LOW) literal probes live on the minor-unit grid; the runtime accepts 18 decimals, so a window narrower than one minor unit passes

`internal/verification/literals.go:93-105`: every probe amount is `FloatString(minor)` (line 101), so the midpoint of two neighbours one minor unit apart rounds onto a
probe that is already `taken`, and edges with more decimals than the exponent round. The data layer accepts money with up to 18 decimals whatever the
currency (`internal/runtime/data/values.go:16-34`, `internal/erp/types.go:29`, and the rules tests use `500.000000000000000001`).
Repro (unit): requirement `GT 500.00 USD`; plans `gte 500.01`, `gt 500.001`, a window `(500.001, 500.009)`, a window `(626, 626.01)`, KWD `(500.001, 500.002)`,
JPY `(50001, 50002)`: each `PASS`, 0 findings. An order of 500.005 USD then skips the approval that the oracle requires.
The claim in ADR102 amendment 13, "a probe at the midpoint of every two neighbours", does not hold for neighbours one unit apart. Part of this
(`gte 500.01` for `GT 500.00`) also passed `verifier-v2` (fixed probes are on the grid too). Direction: add probes at the literal and the threshold
plus and minus 10^-18 (the scale the data layer accepts), or refuse literals finer than the currency exponent.

### PF-03 (LOW) the expiring-token check is fail-open when the second verification fails

`internal/platform/access/ai.go:450-451`: `if _, remaining, err := expiring.VerifyRemaining(...); err != nil || remaining >= run { return nil }`. A token
that passed `Verify` in `serve` (`access.go:351`; the pre-verification is `access.go:254`) and expires before the check (the time between them is the identity/tenant/policy round trips of
`perform`, milliseconds) is admitted, goes to the provider and is lost at the result exchange: the M14R-03 residual, narrowed to tokens that have
milliseconds left. The comment says this is intended ("a token that does not verify is left to the exchange"). Direction: answer `401 unauthenticated` when
the check's own verification fails. Not exploitable beyond the caller's own tenant budget.

### PF-04 (LOW) the version-gate scan is still blind to valid spellings of a partial gate

`internal/metadata/version_family.go:24` (`sqlVersionToken = ['"$^{,]1\\?\.([0-9]+)`) needs one of five characters directly before the `1`. A function that gates
on `1.4.0` only passes `TestSQLFunctionsNameWholeSchemaVersionFamilies` when it is written as (scratch migration `00091_zz_review_scratch.sql` with
`CREATE OR REPLACE FUNCTION runtime.zz_review_gate(doc jsonb) ... SELECT doc->>'schema_version' <spelling>`; control `= ANY('{1.4.0}'::text[])` fails the test as it should):
`= ANY('{ 1.4.0 }'::text[])` (a space inside the braces), a multi-line array literal (`'{` newline `1.4.0` newline `}'`), `~ '^(1\.4\.0)$'` (a group), `LIKE '%1.4.0'`. All
four: `ok`. Also blind in `AuthoringVersionsNamed` (a probe over 49 spellings): `~ '^1\.(4|5)\.0$'`, `string_to_array('x 1.4.0',' ')`, `';'`- and `'|'`-separated lists,
`SIMILAR TO`, `'1.' || '4.0'`. The comment in `version_family.go` lists `LIKE '1_4%'`, `'^1[.]4[.]'` and `SIMILAR TO` as the known blind spots and says "an honest author writes one of
the spellings above"; a spaced or multi-line array literal is valid PostgreSQL and is what a formatter produces. The live-catalog scan
(`tests/integration/m9r_catalog_version_gate_test.go`) reads the same text with the same function, so it is blind to the same spellings. No real place in
`db/migrations` is blind today (every `1.N` token in the migrations is on the scan's list: 00005, 00009, 00017, 00018/28/37 (event envelope, unrelated), 00020, 00045, 00052, 00080, 00090).
Direction: read only the string literals of the body (single, escape and dollar quoted) and match `(^|[^0-9.])1\.([0-9]+)` inside them.

### PF-05 (LOW) test gap: the web renderer's list of versions that may carry `unique` is not tested for 1.5.0

`apps/web/src/preview/contracts.ts:146`: `(ir.ir_version !== "1.4.0" && ir.ir_version !== "1.5.0")`. Removing the `1.5.0` member (the IR-01 class of defect, in TypeScript) leaves
`vitest run src/preview` at 26 of 26 passed; the test at `Workspace.test.tsx:491` decodes a 1.5.0 IR without a unique field. (The full suite had one timing failure in
`CommandRecovery.test.tsx`, 5,061 ms, a 5 s limit on a loaded host; the test passes alone.) The same mutation of line 100 (the accepted `ir_version` chain) is caught. The SQL, Go and
`internal/ai/schema_text.go` lists are all caught: removing `1.5.0` from the `00080` CHECK, from the function in `00090`, from `metadataSchemaFiles`, from `IntegritySchema`
and from `WorkflowSchema` each fails a named test (`TestMetadataVersionsCheckListsEveryKnownVersion`, `TestSQLFunctionsNameWholeSchemaVersionFamilies`, `TestAdversarialCorpus` and two more in `internal/ai`,
`TestIRRestoreKeepsTheExactCompilerIdentityOfEveryIntegrityCapableContract`, `TestWorkflowSchemaVersions`).

### PF-06 (LOW, a variant of PF-01 that survives `verifier-v4`) a window gated on a null-safe condition over a field of a type the miner does not read passes

`internal/verification/verifier.go` `minedValues` collects the values of fields of kind `string`, `enum`, `bool` and `integer` only. Against the tree of `55ee30e` (exported to a
scratch directory; PF-01 and PF-02 repros all FAIL there as they should, see "Fixes re-verified"), a window `amount > 626 AND amount < 749 AND <gate>` with the requirement not pinning the
gate field still PASSES (64 results, 0 findings) when the gate is `eq(field, literal)` or `not(is_null(field))` over a `date`, `datetime`, `decimal` or `money` (USD) field. The same gate over a
string, enum or bool field fails with a `lit-...` finding that names the mined value. A gate that is not null-safe (`gt`, `lt` over decimal, money, datetime, integer) makes the probe unevaluable and the
verdict is `BLOCKED INCOMPLETE_CASE_INPUT` (fail closed; the author must pin the field). Repro: `zz_review_pf2b_test.go` `TestZZGateTypes` (not committed; shape as in PF-01 with the added
field `field.order.g` of the type under test). ADR102 amendment 14 does not list these types among the residuals. A second survivor of the same family: `gt(money_round(amount), 500)` over a threshold of `GT 500.00` PASSES on `verifier-v3` and `verifier-v4` (the boundary is 500.005, there is no literal there, so no probe falls in 500.00..500.005; `gt(round(amount, 0), 500)` is found by the fixed `above` probe). Direction: mine a value for the other literal-bearing kinds
(a literal of the field's own kind taken from the conditions, plus a value that matches none) or block a plan whose threshold exemption depends on such a field.

### PF-07 (LOW) `TestF05ApprovalOvertakenByAFirstActivationCannotTakeEffect` passes against a weakened migration 00092 and against a weakened `activate_release`

`tests/integration/m13r_approve_fence_test.go:178-200`. Mutation 1: in `db/migrations/00092_m13_approve_release_fence.sql` the `FOR SHARE` of the pointer read and the
`INSERT INTO control.release_fences ...` statement are neutralised (kept as comments so that the migration's own self-check, which looks for those two strings, still passes): the three tests
that the fence is for (`TestF05ApprovalOvertakenByACommittedRejection`, `...ByAnActivationOfAnotherCandidate`, `...ByARollback`) and `TestF05ApprovalsStillRunTogether` FAIL, and
`TestF05ApprovalOvertakenByAFirstActivationCannotTakeEffect` PASSES (log: `err=<nil>, 1 approval(s) recorded`). Mutation 2: in `00088_m13_review_fixes.sql` the stale-base test of
`activate_release` (`has_cur AND (cur.artifact_hash<>p_expected_base OR ...)`) is replaced by `FALSE`: the same test still PASSES (the pointer compare-and-set refuses the activation with 55000
on its own). So the test is evidence that the system refuses to activate the stale candidate, through at least two controls, and not evidence for 00092: with no pointer row the fence cannot
abort the approval (nothing to lock, an unchanged fence row), the approval commits, and the test only logs that. A third mutation (both controls) was NOT RUN. Direction: assert the approval's outcome
(recorded, with the candidate reading stale) and name the control, or document the test as a system property.

### PF-08 (LOW) the parallel body-budget benchmark does not measure many identities

`internal/platform/access/bodybudget_test.go` `BenchmarkBodyBudgetHoldAddReleaseParallel`: the comment says "Many requests of many identities at once" but every goroutine books under the same
`"issuer\x00subject-xxxxxxxx"`, so it measures one hot identity on the one lock. The numbers in `LINE_INTEGRATION_PLAN.md` are of the right order (see the table): the cost of the budget is a lock
and a counter, below one microsecond per request on a loaded host.

### PF-09 (MEDIUM) the card rule loses a valid card number that follows another group of digits: a match the check refuses is consumed

`internal/ai/redact.go:36-44` (`redactRule.apply`), `:62` (the card rule `\b\d(?:[ -]?\d){12,18}\b` with the `cardNumber` check) and `settled` (`:304`). `ReplaceAllStringFunc` takes the leftmost
match; when `ok` refuses it (no issuer prefix or no check digit) the text is returned as written and the search resumes AFTER that match, so a card number that starts inside the refused span is
never tried, and `settled` does not run again because nothing was replaced (`n == 0`). Repro (`redactText`, a Luhn-valid test number built from `4111` groups; no other secret in the text):
`"Order 99871 4111111111111111 12/27"`, `"id 123 4111 1111 1111 1111"`, `"12 4111 1111 1111 1111"`, `"ref 20261029 4111 1111 1111 1111"`, `"x-99871-4111-1111-1111-1111"` all come back unchanged, `n = 0`;
the same card alone, after a word, after a pipe, or after a number that fills the whole 19-digit window (`"1234567890123456 4111 1111 1111 1111"`) is redacted. This is a pasted order line or table
row, the accident the redactor exists for, and the documented rule is "13 to 19 digits, alone or in groups" (ADR104 Amendment C item 6). Direction: for the rules that carry a check, try every start
position of the span (re-run the match from the character after the refused start) or scan the digit groups of the text directly.

### PF-10 (LOW) test gap: the list of operator credentials is not pinned by name

`internal/platform/app/operator_env_test.go:32` loops over `operatorCredentials` itself, so a name removed from the list is no longer tested. Mutation: `REDIS_PASSWORD` and
`EVENT_OPERATOR_REDIS_URL` removed from `operator_env.go:12-13`: `go test ./internal/platform/app` stays green. Only `MIGRATION_DATABASE_URL`, `MIGRATOR_PASSWORD` and
`POSTGRES_PASSWORD` are pinned (`operator_env_test.go:41`). The other two tests and the position test fail for their mutations (call removed from `Main`: `TestMainRefusesOperatorCredentialsBeforeRunning`; values logged:
`TestOperatorCredentialRefusalNamesAndNeverShows`). Direction: assert the literal list of six.

### PF-11 (MEDIUM) a refused AI creation with a large draft costs seconds of CPU inside the admission slot and the transaction, and nothing limits how often it can be repeated

`internal/platform/access/ai.go:408-420` (`aiPropose` runs `svc.Admit`, `aiPlatform` and `svc.Prepare` inside the request transaction, which holds one of the `API_DB_CONCURRENCY` slots, default 4) and `internal/ai/context.go:160-180`
(`BuildContext` redacts every string of every editable definition BEFORE the 64 KiB bundle limit is checked). A draft may be up to 256 KiB and 500 definitions
(`metadata.MaxBytes`, schema `maxItems`); the redactor costs about 3.5 us per byte on adversarial text (PF-09's measurement), so the refusal `422 ai_context_too_large` is reached after the whole draft was redacted.
The refusal reserves nothing and charges nothing, so neither the per-tenant in-flight limit (`ai_busy`) nor the daily budget ever fires, and the traffic gate allows 300 requests per second per peer (`traffic.DefaultLimits`).
Measured (no mutation; `m14Setup` handler, `-race` binary, host load average ~30, so absolute times are inflated several times; the ratio is the finding): a member of tenant A with a registered
draft of 251,162 bytes (490 definitions, valid) sends `POST .../ai/patch-proposals` with a 4,000-character request: HTTP 422 after **9.57 s** (without `-race`, `BuildContext` alone takes 1.0 s on the same host: `TestZZBuildContextLatency`, `Prepare`
adds parse 65 ms and compile 8 ms). A second member's `GET /v1/tenants/{tenant}/access`: p50 15 ms, p95 35 ms. While the first member loops 6 such requests: the second member's read p50 **4.58 s**,
p95 10.3 s, max 12.5 s (30 of 30 answered 200); the looping member saw 32 x 422, 14 x 409 `ai_conflict` and 29 x 503 `service_unavailable`. Repro: `zz_review_pf0_test.go` (not committed): register the draft
with 49 entities of 9 enum fields each (label of 200 characters, two enum values of 58 characters), then `e.propose(e.alice, strings.Repeat("password=hunter2xx; ", 200))` in 6 goroutines and time `GET /access` by another member.
Direction: refuse before doing the work (the stored document size is known: a draft whose definitions cannot fit 64 KiB after clipping can be refused from its size, or the redaction can stop as soon as the running size passes the cap), and
count refused creations against the member (a 429 after a few context refusals), or move `Prepare` out of the transaction.

## Attacks that held

| claim | executed attack | result |
|---|---|---|
| 1a: nothing reserved, charged or sent; 401 `token_expiring` with `WWW-Authenticate`; audited rejection with closed codes | integration probe through `m14Setup` handler (traffic gate and 4-slot admission as in `app.go`), 5 s left against a run of 40 s, both routes; read the audit rows as `to_jsonb(a)::text` | held: 401 `token_expiring`, `WWW-Authenticate: Bearer realm="framework"`, 0 provider calls, usage 0/0, 0 proposals, 0 PENDING, 2 `ai.propose.rejected` rows whose `state` is `{"metadata_app": ...}` only; neither token substrings nor the request canary appear in the response or the rows |
| 1b: production wiring | read `app.go:233,270` (`Auth: auth` is the `*identity.Verifier`), `grep -rn "Authenticator\|Verify("` over the tree: one implementation, no wrapper; both creation routes refused | held |
| 1c: order and oracle | same request with a 5 s and a 600 s token for: viewer (no `ai.propose`), a member of tenant B only, an unprovisioned subject, malformed bodies (no key, unknown key, `expected_revision` 0), AI unconfigured, forged signature, garbage, empty bearer, two `Authorization` headers, lowercase `bearer`, double space, trailing space, empty bearer, no scheme, `text/plain`, 40 KiB body | held: identical answers for every pair (403, 403, 401, 400, 400, 400, 503, 401...); header shapes never reach the check (401 `unauthenticated` from `perform`); only an authorised caller with a short token sees `token_expiring`, and it comes before `409 draft_changed` for that caller |
| 1c: retry loop | 8 concurrent creations x 3 rounds of one tenant (contested usage row) through a wrapper that reports a token as long on its first check and short on every later check | held: 24 requests, 6 stored, 18 refused `token_expiring` on a retry; 0 PENDING; `control.ai_usage` requests 6 and tokens 35,826 equal the proposals' own sums; 6 `ai.proposal.created` and 18 `ai.propose.rejected` rows |
| 1d: arithmetic | identity unit probe with a fixed clock: exp = now+70, +69, iat+3600, iat in the future by the skew, `MaxInt64`, 0, missing, negative, within and beyond the skew, a string, a float with fraction | held: `Verify` and `VerifyRemaining` agree on every input; remaining is `exp - now` to the nanosecond (69.75 s for exp=now+70 at x.25 s); no saturation is reachable (`validTimes` bounds exp by iat+3600 and iat by now+skew+...); a lapsed token inside the skew reports -1.25 s and is refused |
| 1e: cost | `BenchmarkZZVerify` / `VerifyRemaining`, 2000 iterations, no race | 98.5 us and 73.4 us per call: an extra ~75 us per try (3 verifications per creation instead of 2); not an amplification |
| 2: remove `probes = append(probes, more...)` | temporary mutation, `go test ./internal/verification` | held: `TestExemptionWindowsBetweenTheFixedProbesFail`, `TestProbeSetIsPinnedToTheVerifierVersion`, `TestAPlanWithTooManyAmountsIsBlocked` fail |
| 2: bound | plans with 30, 31, 32, 33 extra amounts (+ the threshold 500) | held: 31 and 32 distinct are probed (155 and 159 results, 238 and 309 ms), 33 and 34 are `BLOCKED UNSUPPORTED_BY_VERIFIER` (1 result, 22 ms); 45 spellings (`n`, `n.0`, `n.00`) collapse to 16 distinct |
| 2: other currencies | JPY window 62600..74900 and 62600.5..74900.5, KWD 626..749 | held: found by `lit-` probes |
| 2: hostile literals | `1e999999999`, `0x1p100000000`, `1/0`, `1e5`, `+5`, `.5`, `5.`, Arabic digits, `1_000`, `00500`, 19 decimals, 65 digits in a money literal | held: the metadata validator refuses them before the compiler (only `-0` compiles, and it is zero) |
| 2: downgrade | `Recompute` of (a) a v2-style PASS report (window probes removed, outcome re-settled) labelled `verifier-v3`, (b) the same labelled `verifier-v2`, (c) an honest plan's report labelled `verifier-v2` | held: `ErrReportMismatch` each time; the version is not the only evidence, the bytes are recomputed |
| 3: the daemon refuses | built `bin` copies of `cmd/api`, `cmd/worker`, `cmd/dispatcher`; ran each under `env -i` with one operator variable at a time (a random value generated at run time), all six together, empty, one space, lower case | held: exit 1 and `"variables":[<name>]` for each of the 18 combinations; the random value and a `db.internal` host never appear in the output (0 matching lines); an empty value passes the check and fails later in `Run` ("service failed"), one space is refused, a lower-case name is not matched (nothing reads it); a clean environment reaches `Run` |
| 3: the list is complete | compared with every credential-like name in `.env.example`, `infra/compose/*.yaml`, `scripts/*.mjs`, `Makefile`, `.github`, AGENTS.md: the operator-level ones are exactly `MIGRATION_DATABASE_URL`, `MIGRATOR_PASSWORD`, `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `EVENT_OPERATOR_REDIS_URL`, `EVENT_OPERATOR_PASSWORD`; `REDISCLI_AUTH` exists only in the environment of one `docker compose exec` (`signin-roles.mjs`); `runtimeEnv()` computed over the real `.env`: no backend kind receives any of the six, the API receives `DATABASE_URL`, `REDIS_URL`, `SIGNIN_REDIS_URL`, the worker `DATABASE_URL`, `REDIS_URL`, `WORKER_DATABASE_URL`, the dispatcher `DATABASE_URL`, `DISPATCHER_DATABASE_URL`, `REDIS_URL` | held (names only were printed) |
| 3: entry points | `grep app.Main` : `cmd/api`, `cmd/worker`, `cmd/dispatcher` only; `migrate`, `iam`, `worker-admin`, `event-replay`, `release-check` hold operator credentials by design and do not call `Main`; `Run` is exported and bypasses the check but is called only by tests | held |
| 3: launcher | `node --test scripts/*.test.mjs` (13 of 13); `node scripts/with-env.mjs <dir>/pf-bin/api` (an unrecognised spelling) with the real `.env`; `node scripts/with-env.mjs <dir>/bin/api` (recognised); `runtimeKindOf` over `sh -c`, `bash -c`, `env`, `nice`, `time`, `go run .`, `./API` | held: the unrecognised spelling exits 1 naming exactly the six variables; the recognised one starts and answers `/healthz` 200 and `/readyz` 200; `go run .` and `./API` are not recognised (fail closed at the daemon) |
| 5: SQL places that list 1.5.0 | removed `1.5.0` from the `00080` CHECK and from the function of `00090` (scratch), ran `go test ./internal/metadata` | held: `TestMetadataVersionsCheckListsEveryKnownVersion` and `TestSQLFunctionsNameWholeSchemaVersionFamilies` fail |
| 5: array literal and list spellings of b88a465 | scratch migration with `= ANY('{1.4.0}'::text[])` | held: reported (gap 1.5.0) |
| 7: benchmarks | `nice gt test -run '^$' -bench BodyBudget -benchtime 1s -count 3 -benchmem ./internal/platform/access`, load average 11.7 on 4 cores (other agents running) | held: hold+add+release 102-110 ns/op, 0 B, 0 allocs (document: 59 ns idle); parallel 121-125 ns (document: 188 ns idle, four goroutines on one identity); 64 KiB booked read 2.16-2.51 us with 80 B/op and 2 allocs/op against 1.36-1.60 us plain (document: 1.35 against 0.95 us, "two allocations of 80 bytes"): the same order of magnitude and the same allocations, about 1.7x slower here because of the load; the extra cost per request stays below 1 us |
| 0: redactor, odd inputs | `redactText` over `""`, `" "`, newlines only, NUL, `\xff`, a lone surrogate, `sk-`, a bare `[REDACTED:`, a marker at the end of a line followed by tail-shaped lines with CRLF and trailing blanks | held: no panic, each result is a fixed point (`TestZZRedactOddInputs`) |
| 0: redactor, latency | 26 adversarial shapes (one token, key per line, wrapped tails, assignments, `a_` chains, cards, digit pairs, PEM prefixes, cookie pairs, credentials in URLs, Cyrillic and fullwidth look-alikes, invalid UTF-8, hex runs, glued card+key chains) at 16 KiB, 64 KiB, 256 KiB and 1 MiB through `redactText` | held with a cost: linear, worst 49 ms at 16 KiB (the 4,000-rune request limit), 187 ms at 64 KiB, 0.99 s at 256 KiB and 3.65 s at 1 MiB (shape: one key then 150,000 one-word lines; the output is 2.7 times the input), measured on a host with load average 11; the cost inside the admission slot is PF-11 |
| 6: `migrate status` against a populated disposable database | built `cmd/migrate`, created a database `zz_pf6_<random>` owned by the migrator (verified `current_database()` and the effective pgx target), ran the binary through `exec` for: empty, migrated (70 files), two versions deleted from `goose_db_version`, the last file pending, a row recorded `is_applied=false`, a version without a file (99999), a version recorded twice, version 80 removed (foreign numbering), a wrong password, a malformed port, a nonsense `sslmode`, an unreachable port, an empty URL, `down`, no argument | held: empty: status exit 0 with 70 pending, `up` applied 70 in 0.81 s, repeat applied 0; file 50 pending below 92: status marks it `(refused by up: numbered below the highest applied version 92)` and exits 1, `up` exits 1 naming version 50; file 92 pending above 91: status exit 0, plain `pending`; `is_applied=false`, a DB-only version and a duplicate row: status and `up` both exit 0 and agree (goose's `Status` ignores `is_applied`; `cmd/migrate` never runs `down`); foreign numbering: status and `up` exit 1 with the lineage message; every error line is generic ("migration database unavailable", "invalid PostgreSQL configuration", "MIGRATION_DATABASE_URL must be a valid PostgreSQL URL", usage) and neither the real password, the wrong-password marker, a `postgres://` string nor `127.0.0.1` appears in any output |
| 6: the tests fail without the fix | `TestStatusMarksThePendingMigrationsThatUpRefuses` with the marking disabled (`false` in place of the comparison) | held: fails (`0 refused`); the mutation `<` to `<=` is an equivalent mutant (a pending file cannot equal the highest applied version) |
| 3: the tests fail without the fix | `TestMainRefusesOperatorCredentialsBeforeRunning` with the call removed from `Main`; `TestOperatorCredentialRefusalNamesAndNeverShows` with the values logged | held: both fail; the list itself is not pinned (PF-10) |

## Fixes re-verified (a4379a8 verifier-v4, 55ee30e token check; read-only, not on the pin)

The tree of `55ee30e` was exported (`git archive`) to a scratch directory and the repros of PF-01, PF-02 and PF-03 were run there with the pinned toolchain.

| finding | repro against 55ee30e | result |
|---|---|---|
| PF-01 | window 626..749, 501..502 and 600..900 gated on `note == "gold"`, note not pinned | FAIL with `lit-above-626.01/field.order.note="gold"`, `lit-below-748.99/...` (found); pinned note: PASS by design; ungated window FAIL; honest plan PASS |
| PF-02 | `gte 500.01`, `gt 500.001`, `gte 500.005`, windows `(500.001, 500.009)`, `(626.001, 626.009)`, `(626, 626.01)`, `(626.005, 749.005)`, KWD `(500.001, 500.002)`, JPY `(50001, 50002)` | all FAIL with `lit-mid-500.005`, `lit-mid-500.0025`, `lit-mid-626.005`, `lit-mid-50001.5`, `lit-mid-500.0015`; JPY and KWD windows between the minor units are found |
| PF-03 | `TestTokenCheckBeforeAdmission` (10 cases) and a read of `ai.go` | PASS: `VerifyRemaining` failing now answers 401 `unauthenticated`; exactly the run is served; one second short is `token_expiring`; the header shapes are still left to the front door. Not re-run through the integration harness |
| unchanged | bound (31 and 32 distinct probed: 275-283 results; 33 and 34 `BLOCKED UNSUPPORTED_BY_VERIFIER`, 17 ms), forged relabelled report (`ErrReportMismatch` for a v2-style PASS labelled `verifier-v4`, one labelled `verifier-v2`, an honest plan labelled `verifier-v2`) | held. Cost at the bound with mined values: 670 and 928 results in 1.3 and 1.6 s on a host with load average 34 (verifier-v3: 0.24 and 0.31 s) |
| variants | a gate of another type: `bool`, `enum`, `string` FAIL (found); `integer`, `decimal`, `money`, `datetime` with `gt`: `BLOCKED INCOMPLETE_CASE_INPUT` (fail closed); `date`, `decimal`, `money`, `datetime` with `eq` or `not(is_null(...))`: PASS | PF-06 survives; `gt(money_round(amount), 500)` PASS (boundary 500.005, sub-minor, no literal there); an honest `amount * 2 > 1000` is BLOCKED (the 10^18 probe overflows), so the documented arithmetic residual is fail-closed for factors above 1 |

## Gate subset

Pinned tree `11a4ee4`, no product file mutated (every mutation was reverted with `git checkout --` before these runs; the probe files were outside the packages run).

| command | result |
|---|---|
| `hv nice -n 10 ./scripts/go.sh test -race -count=1 ./internal/ai/...` | PASS: `internal/ai` 311.8 s, `internal/ai/corpus` 5.6 s |
| `hv nice -n 10 ./scripts/go.sh test -race -count=1 ./internal/verification/... ./internal/identity/... ./internal/metadata/... ./internal/platform/... ./cmd/migrate/...` | PASS, 10 packages ok (`verification` 17.5 s, `identity` 4.2 s, `metadata` 8.1 s, `platform/access` 3.5 s, `platform/app` 1.3 s, `httpserver` 1.3 s, `postgres` 1.4 s, `signin` 1.4 s, `traffic` 2.9 s, `cmd/migrate` 1.0 s); two packages have no test files |
| `hv nice -n 10 make check` (docs-verify, gofmt, `go vet` of all packages and of the integration packages, `go mod verify`, `eslint --max-warnings 0`, `tsc --noEmit`) | PASS, rc 0 (the 935 s of wall time in the log are the wait for the lock) |
| `node --test scripts/*.test.mjs` | PASS, 13 of 13 |
| `vitest run src/preview` (unmutated) and `vitest run src/CommandRecovery.test.tsx` (unmutated) | PASS (26 of 26; 2 of 2 in 6.7 s). The full suite was run once, under the PF-05 mutation: 125 of 126 passed, the failure was `CommandRecovery.test.tsx` at 5,061 ms (its 5 s limit) on a host with load average ~30; it passes alone. The full frontend suite was not run unmutated (NOT RUN) |
| `it-run m14` (stack rebuilt from this tree's 70 migrations) `TestZZReviewPF6\|TestZZReviewPF0\|TestF05\|TestM14R\|TestM13R` | PASS (`ok` 350.1 s Go time, rc 0): 28 regression tests (`TestF05*` 6, `TestM13R*` 15, `TestM14R*` 7, including `TestM14R03ATokenThatEndsBeforeTheRunCanFinishIsRefusedBeforeAnythingIsSpent` 3.3 s and `TestF05ApprovalOvertakenByAFirstActivationCannotTakeEffect`) and my two probes; 0 failures. The F-05 residual test logs `err=<nil>, 1 approval(s) recorded` on the unmutated tree, as it did under both mutations |
| `it-run m14` (RESET=0) `TestM14API\|TestM14Adv\|TestM14Ports\|TestM14Pre\|TestM14Slow\|TestZZReviewPF2` | pending (queued behind run A) |

## NOT RUN

Sections 3-8 and the redactor code review are in progress.
