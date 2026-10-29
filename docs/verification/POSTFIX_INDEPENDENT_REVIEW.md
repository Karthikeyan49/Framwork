# Post-fix independent review (changes after 0e13abe, redaction corpus excepted)

Reviewer worktree: `/home/user/review-postfix`, branch `review/integration-postfix`, from the pinned commit
`11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`). Scope: the eight sections of brief 60.
Result words: PASS / FAIL / NOT RUN only. Every "held" names the attack that was executed.

## Progress

| # | scope | state |
|---|---|---|
| 1 | 62cc527 expiring-token admission check | attacked (integration probes, unit edge probes); one LOW |
| 2 | 3c27378 verifier-v3 | attacked (unit probes, two mutations); PF-01 MEDIUM, PF-02 LOW |
| 3 | bca9d98 operator credentials refused at start | not started |
| 4 | db9c2fa F-05 residual test | not started |
| 5 | b88a465 version-gate scan | not started |
| 6 | 294e597 migrate status | not started |
| 7 | 80a582f body-budget benchmarks | not started |
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

## Gate subset

None run yet.

## NOT RUN

Sections 3-8 and the redactor code review are in progress.
