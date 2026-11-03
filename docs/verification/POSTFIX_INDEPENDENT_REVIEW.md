# Post-fix independent review (changes after 0e13abe, redaction corpus excepted)

Reviewer worktree: `/home/user/review-postfix`, branch `review/integration-postfix`, from the pinned commit
`11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`). Scope: the eight sections of brief 60.
Result words: PASS / FAIL / NOT RUN only. Every "held" names the attack that was executed.

## Progress

| # | scope | state |
|---|---|---|
| 1 | 62cc527 expiring-token admission check | attacked (integration probes, unit edge probes); one LOW |
| 2 | 3c27378 verifier-v3 | attacked (unit probes, two mutations); PF-01 MEDIUM, PF-02 LOW |
| 3 | bca9d98 operator credentials refused at start | attacked: real binaries, launcher, tree comparison; no finding |
| 4 | db9c2fa F-05 residual test | mutated 00092 and `activate_release`: PF-07 (LOW) |
| 5 | b88a465 version-gate scan | attacked: scratch migrations, mutations of every enumerated place; PF-04, PF-05 (LOW) |
| 6 | 294e597 migrate status | not started |
| 7 | 80a582f body-budget benchmarks | run; PF-08 (LOW, comment) |
| 8 | regression subset | not started |
| 0 | redactor code defects (panics, complexity, latency at the largest input) | not started |

## Verdict

Interim: PASS WITH FINDINGS (sections 3-8 still open).

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
four: `ok`. Also blind in `AuthoringVersionsNamed` (probe over 48 spellings): `~ '^1\.(4|5)\.0$'`, `string_to_array('x 1.4.0',' ')`, `';'`- and `'|'`-separated lists,
`SIMILAR TO`, `'1.' || '4.0'`. The comment in `version_family.go` lists `LIKE '1_4%'`, `'^1[.]4[.]'` and `SIMILAR TO` as the known blind spots and says "an honest author writes one of
the spellings above"; a spaced or multi-line array literal is valid PostgreSQL and is what a formatter produces. The live-catalog scan
(`tests/integration/m9r_catalog_version_gate_test.go`) reads the same text with the same function, so it is blind to the same spellings. No real place in
`db/migrations` is blind today (every `1.N` token in the migrations is on the scan's list: 00005, 00009, 00017, 00018/28/37 (event envelope, unrelated), 00020, 00045, 00052, 00080, 00090).
Direction: read only the string literals of the body (single, escape and dollar quoted) and match `(^|[^0-9.])1\.([0-9]+)` inside them.

### PF-05 (LOW) test gap: the web renderer's list of versions that may carry `unique` is not tested for 1.5.0

`apps/web/src/preview/contracts.ts:146`: `(ir.ir_version !== "1.4.0" && ir.ir_version !== "1.5.0")`. Removing the `1.5.0` member (the IR-01 class of defect, in TypeScript) leaves
`vitest run src/preview` at 26 of 26 passed; the test at `Workspace.test.tsx:491` decodes a 1.5.0 IR without a unique field. (The full suite had one timing failure in
`CommandRecovery.test.tsx`, 5,061 ms, that I did not attribute; the confirming re-run was cut off.) The same mutation of line 100 (the accepted `ir_version` chain) is caught. The SQL, Go and
`internal/ai/schema_text.go` lists are all caught: removing `1.5.0` from the `00080` CHECK, from the function in `00090`, from `metadataSchemaFiles`, from `IntegritySchema`
and from `WorkflowSchema` each fails a named test (`TestMetadataVersionsCheckListsEveryKnownVersion`, `TestSQLFunctionsNameWholeSchemaVersionFamilies`, `TestAdversarialCorpus` and two more in `internal/ai`,
`TestIRRestoreKeepsTheExactCompilerIdentityOfEveryIntegrityCapableContract`, `TestWorkflowSchemaVersions`).

### PF-06 (LOW, a variant of PF-01 that survives `verifier-v4`) a window gated on a null-safe condition over a field of a type the miner does not read passes

`internal/verification/verifier.go` `minedValues` collects the values of fields of kind `string`, `enum`, `bool` and `integer` only. Against the tree of `55ee30e` (exported to a
scratch directory; PF-01 and PF-02 repros all FAIL there as they should, see "Fixes re-verified"), a window `amount > 626 AND amount < 749 AND <gate>` with the requirement not pinning the
gate field still PASSES (64 results, 0 findings) when the gate is `eq(field, literal)` or `not(is_null(field))` over a `date`, `datetime`, `decimal` or `money` (USD) field. The same gate over a
string, enum or bool field fails with a `lit-...` finding that names the mined value. A gate that is not null-safe (`gt`, `lt` over decimal, money, datetime, integer) makes the probe unevaluable and the
verdict is `BLOCKED INCOMPLETE_CASE_INPUT` (fail closed; the author must pin the field). Repro: `zz_review_pf2b_test.go` `TestZZGateTypes` (not committed; shape as in PF-01 with the added
field `field.order.g` of the type under test). ADR102 amendment 14 does not list these types among the residuals. Direction: mine a value for the other literal-bearing kinds
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
| 0: redactor, latency | 26 adversarial shapes (one token, key per line, wrapped tails, assignments, `a_` chains, cards, digit pairs, PEM prefixes, cookie pairs, credentials in URLs, Cyrillic and fullwidth look-alikes, invalid UTF-8, hex runs, glued card+key chains) at 16 KiB, 64 KiB, 256 KiB and 1 MiB through `redactText` | held with a cost: linear, worst 49 ms at 16 KiB (the 4,000-rune request limit), 187 ms at 64 KiB, 0.99 s at 256 KiB and 3.65 s at 1 MiB (shape: one key then 150,000 one-word lines; the output is 2.7 times the input), measured on a host with load average 11; see PF-09 |

## Gate subset

None run yet.

## NOT RUN

Sections 3-8 and the redactor code review are in progress.
