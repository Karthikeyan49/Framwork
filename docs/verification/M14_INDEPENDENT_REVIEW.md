# M14 independent adversarial review (AI builder, as merged into `integration/m9r-m14`)

Reviewer: independent agent; it built nothing in either line and changed no product code. Pinned subject commit
`552fa9efe2092a592e39974ee23ec1ecbc057729` (head of `origin/integration/m9r-m14`). Review branch `review/integration-final`.
Subject: ADR104 with Amendments A and B (`docs/adr/104-m14-ai-builder-and-intent-capture.md`), migration `00089`,
`internal/ai/*`, `internal/platform/access/{ai,ai_decide,access}.go`, `internal/audit/audit.go`, policy `m14-native-v11`,
and the three items that are new since the gate: the 60-second provider phase, M13R-03 `holdingApprovers`, and the fixes of
migrations `00086`/`00087`/`00088` and `verifier-v2`.
Rules of truth: PASS / FAIL / NOT RUN. Every "held" names the attack that was executed. A scripted provider stands in for every
model: nothing here is evidence about a real model, a real vendor or real latency.

STATUS at `552fa9e` (the pinned commit): COMPLETE for the attacks listed (what was not covered: last section). Overall: **FAIL** - 4 MEDIUM, 4 LOW, no HIGH.
No model has authority in this build and every decision stayed with a human in every attack; the defects are in the money,
the audit hygiene and the concurrency of the AI route and in one M13 fence that the M13R-01 fix left open.

STATUS at `be6ec7cae1849bf535bad1811b21bf93067598b5` (re-verified 2026-10-22, the head after the builders' fixes): all eight findings are **OPEN** and reproduce (M14R-01 to M14R-04 MEDIUM, M14R-05 to M14R-08 LOW); no file of `internal/ai`, `internal/verification`, `internal/intent`, `internal/promotion`, `internal/audit` or the migrations `00081` to `00089` differs from the pinned commit, and the IR-02 fix removes one of the five triggers of M14R-03. Overall: **FAIL** - 4 MEDIUM, 4 LOW, no HIGH, as at the pinned commit.
The review of the pinned commit is kept as written; the re-verification is the section "Re-verification at `be6ec7c`" before "Not covered".

## Verdict by claim (at the pinned commit)

| Claim (task list, ADR104) | Verdict | Attack that decided it |
|---|---|---|
| The model has no identity and no authority | PASS | prompt scan (H1); forged requiredness/status/provenance never READY (H5); decisions exactly once (H4); patch fuzz, two runs (H6) |
| Prompts are metadata only | PASS | H1: the whole serialized provider request searched for tenant id, user ids, bearer tokens; request text stored nowhere |
| Redaction | FAIL (LOW, M14R-08) | 28 secret shapes: 10 survive; the ADR already says "mechanical filter" |
| Model calls never inside a transaction or holding a connection | PASS | H2: 7 to 8 concurrent provider phases (17 requests sent), 0 non-idle `framework_app` backends, honest `GET` in 9 to 25 ms |
| Apply / confirm: CAS, acknowledgement, revalidation | PASS | H4, H5, and the builders' `TestM14API*Apply*`/`Confirm*` in the full suite |
| Port hygiene (`ai_begin`, `ai_record_attempt`, `ai_finish`, `ai_decide`, three tables) | PASS | catalog query (H7) |
| Tenant isolation of the AI routes | PASS | H14: tenant-A proposal addressed through tenant B's path and by a user who is not a member of tenant A |
| Policy `m14-native-v11` | PASS | 45-route POST matrix (five AI routes), `Holds` vs route decision for 7 role shapes (H3) |
| Audit carries hashes and closed codes only | PASS for content, FAIL for robustness (MEDIUM, M14R-01) | 0 hits of the request text in `audit.entries` and `ai_proposals.body`; but the closed-code check turns a provider label into a 503 |
| HTTP provider SSRF guard | PASS (the dial path against a canary server and a stub resolver; no real network) | 58 address forms through `tools.DeniedAddress`; follow-up H16: 15 resolver answer shapes and 32 endpoint-spelling/answer combinations refused with 0 requests to the canary; `provider_http.go` read |
| New: 60-second provider phase (`reply.nextWithin`, `extendWriteDeadline`, `ai.APIWallClockCeiling`) | PASS | H2, H8: 58 s phase through `httpserver.New`, no leak to the next request, only proposal creation sets it |
| New: M13R-03 `holdingApprovers` / `RoleStore.Holds` / `NativeHolds` | PASS | H3 |
| New: fixes 00086 / 00087 / 00088 / `verifier-v2` | 00088 FAIL (MEDIUM, M14R-04); 00087 guard PASS; 00086 builders' regression tests only (own variants NOT RUN); `verifier-v2` FAIL (LOW, M14R-05) | rollback race; worker guard evaluated as a worker session and its 60-row allowlist checked for stale entries; narrow exemption band |
| Budget (daily tokens, in-flight, lease) | FAIL (MEDIUM, M14R-02, M14R-03) | cancelled calls uncharged; lost results charged the whole reservation |

## Progress (UTC)

| Time | Step | State |
|---|---|---|
| 2026-10-20 14:30-15:30 | static read of ADR104, `internal/ai`, `access/ai*.go`, `00089`; first probes: token expiry, provider behaviours, concurrent phases, model label, cancellation, role holds, rollback race | done |
| 2026-10-22 07:18 | resumed after an environment pause: Docker daemon restarted, stack healthy, database at migration 89 | done |
| 07:20-07:55 | probes: prompt content, 58-second phase, orphan sweep, concurrency of results, decision races, verifier band, patch fuzz, AI catalog | done |
| 07:41-08:12 | the full integration suite was run a second time on the resumed host, clean tree (see the integration review, gate table) | PASS (433 PASS, 5 SKIP, 0 FAIL) |
| 08:26-08:31 | every probe of the appendix re-run in one batch against the unchanged tree, with the sources printed in the appendix | every probe vets clean; the probes that show a defect fail as listed under Reproduction, the others pass; the working tree was clean afterwards |
| 2026-10-22 11:44-12:00 | follow-up on the items first marked NOT RUN, after another environment pause (Docker daemon restarted again): requirement-proposal parser fuzz, HTTP provider dial path, R7a burst, frozen-sandbox records, candidate-body differential | H15, H16 below; the other results are in the integration review |
| 16:40-17:03 and later | re-verification at `be6ec7c`: the probes of the open findings and of the held claims against the new head; the gate steps are in the integration review | see "Re-verification at `be6ec7c`" |

Method. Probes are untracked `zz_review_*_test.go` files that were never committed (only the two reports are); the sources are in the appendix.
Each ran against a disposable database that the fixture creates from this tree's migrations on my own Docker stack (project `framework-m0`,
started by me; nothing pre-existing was touched). One heavy process at a time, `nice -n 10`. Run command:

    node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -timeout 30m -v -run '<regex>' ./tests/integration

## Findings

Severity: HIGH exploitable violation of a security or integrity invariant; MEDIUM realistic integrity/availability defect or breach of a
documented contract; LOW hardening, documentation or test gap.

Reproduction (the probe sources are in the appendix; a probe fails when the defect is present):

| Finding | Probe | What it asserts / shows |
|---|---|---|
| M14R-01 | `TestReviewAIModelLabelOutsideTheAuditCharset` | HTTP 201 expected; 503 and a PENDING proposal observed |
| M14R-02 | `TestReviewAICancelledCallsAreNotCharged` | at most `completed+1` provider calls expected; 20 observed against 5 |
| M14R-03 | `TestReviewAIStaggeredAdmissionsFinishTogether`, `TestReviewAIConcurrentResultsAreNotLost`, `TestReviewAITokenExpiresDuringProviderPhase`, `TestReviewAIOrphanIsSweptAndChargedByTheReservation` | 201 expected for every proposal; 3 or 4 of 6 lost per round; the orphan's charge is logged |
| M14R-04 | `TestReviewRollbackOvertakesConcurrentRejection` | the pointer must not move onto a rejected hash; it does |
| M14R-05 | `TestReviewNarrowExemptionBandBetweenProbes` | every band that skips approval must FAIL; three narrow bands PASS |
| M14R-06 | `TestReviewAIProviderBehaviours` (case "reports negative and enormous usage") | logs `used_tokens=100005674`; no assertion |
| M14R-07 | `TestReviewAIProviderBehaviours` (case "hangs without honouring the context") | an answer within the wall clock expected; none after 12 s |

### M14R-01 MEDIUM - a provider-reported model label outside `[A-Za-z0-9._:/@+-]` turns a finished provider call into HTTP 503 and an orphaned PENDING proposal

Status at `be6ec7c`: **OPEN**, reproduces (4 of 4 labels).

Contract: ADR104 A10 ("State fields ... validated in `internal/audit`"), A8 (the result transaction records the outcome), and the premise that
provider output is untrusted data that must not be able to break the platform.

Evidence: `internal/ai/repair.go:194` stores the provider's `Model` after `clip` (control characters and length only, `internal/ai/context.go:64`);
the production HTTP adapter takes the label from the `model` member of the provider's JSON answer and only falls back to the operator's label when it is empty
(`internal/ai/provider_http.go:307-310`). `internal/platform/access/ai.go:479` copies `trace[0].Provider`, `.Model`,
`.TemplateVersion` raw into `evidence.State`. `internal/audit/audit.go:216-245` (`validAIEvidence`) refuses any label byte outside
`[A-Za-z0-9._:/@+-]`, `Audit.Append` fails, the whole result transaction rolls back and `aiExchange` answers 503 (`ai.go:551-553`).
The stored attempt is sanitized by `apiLabel` (`internal/ai/service.go:349`); the audit path is not.

Executed (`TestReviewAIModelLabelOutsideTheAuditCharset`, Appendix A): a valid, READY-able scripted answer whose model label is `alpha 4 turbo`,
`Beta-3.1 (70B)`, `модель-1` or `model=fast`. All four: HTTP 503 `service_unavailable`; provider calls 1; proposal left PENDING; `ai_invocations`
rows 0; the next honest request (in-flight limit 1 in this probe) is refused 429 `ai_busy` (the slot is held until the lease). Control: every other probe uses the labels `m` and
`scripted-model-1` and gets 201.

Impact: whoever controls the `model` field of a provider answer (the endpoint, or a proxy or alias in front of it; labels with a space or a
parenthesis are common for self-hosted servers and deployment aliases) makes every proposal fail after the provider call was made. Each failure holds
its in-flight slot for the lease (80 s at the default wall clock) and is charged its whole reservation when swept (M14R-03). Nothing is exposed;
fail-closed. The same flaw applies to the provider name (`clip(resp.Provider, 64)`).

Fix direction: run the three labels through `apiLabel` in `aiRun` before they reach the audit entry (or in the adapters); add the four labels to a
test.

### M14R-02 MEDIUM - a provider call that the client cancels is never charged: the daily token budget can be bypassed at the operator's expense

Status at `be6ec7c`: **OPEN**, reproduces (20 provider calls, 0 tokens charged).

Contract: ADR104 A5 "Daily budget is per tenant per UTC day over reserved + used tokens ... the gateway substitutes the pessimistic estimate when none is
given, the port never lowers it"; a crashed request is "charged pessimistically when swept" (A12); a call that times out is charged its input estimate
(`internal/ai/repair.go:177-181`: "The provider may have processed the request before it ran out of time").

Evidence: `repair.go:171-175` (and `:138-141` before the call): when the request context is cancelled the loop returns `RunFailed` before the attempt and
its usage are appended (`:183-184`). The result transaction then records `FAILED`/`INTERNAL` with zero usage and releases the reservation.

Executed (`TestReviewAICancelledCallsAreNotCharged`): daily budget sized for 5 completed requests (40,000 tokens, one attempt, one slot). 20 cycles: start
a proposal, wait until the provider call has begun (400 ms), cancel the request context. Result: **provider calls 20**, `ai_usage` requests 20,
`used_tokens` **0**, 20 proposals `FAILED/INTERNAL`, 0 invocation rows. Control, same configuration, 20 completed requests: 5 accepted (201), 15 refused,
`used_tokens` 32,370.

Impact: any member who holds `ai.propose` (owner and admin by default) can drive provider calls that the tenant's budget never sees; the only brakes are
the per-tenant in-flight cap (default 2, ceiling 8 in the service) and the request-rate gate. Whether a vendor bills an aborted call was not tested (no
real vendor); most bill at least the input.

Fix direction: treat cancellation like a timeout in `loop` - append the attempt with the input estimate (and, to stay pessimistic, the reserved output)
before returning; add a test that N cancelled cycles consume the budget.

### M14R-03 MEDIUM - the result exchange is one un-retried transaction: any failure in it discards a paid provider answer, leaves the proposal PENDING, and the sweep then charges the whole reservation

Status at `be6ec7c`: **OPEN**; of the five triggers, the admission queue emptied by slow bodies is gone with the IR-02 fix, the other four reproduce.

Contract: ADR104 A8 (three exchanges), A12 (residuals: "a crashed request keeps its in-flight slot until the lease lapses"). The ADR maps serialization
failures to "409 `ai_conflict` (no automatic retry)" (line 470) without saying that in the result exchange this discards a paid answer.

Evidence: `access/ai.go:464-499` (`aiRun`: one `Execute`, then one `aiExchange` in a detached 8 s context) and `:504-555` (`aiExchange`: no retry). Every
failure of the exchange rolls the `ai_finish` back, so the row stays PENDING until `expires_at` (wall clock + 20 s). The tenant's single
`control.ai_usage` row is updated by `ai_begin` (`00089:160-169`) and by `ai_finish` (`00089:227`) inside REPEATABLE READ transactions
(`postgres.WithRuntime`); the second concurrent writer fails 40001, mapped to 409 `ai_conflict` (`ai.go:233-234`).

Executed, five triggers, all by `TestReview...` in Appendix A (the first, third, fourth and fifth lose a paid answer; the second is the same hot row hit at
admission, where nothing is lost):

* **Concurrent results** (`TestReviewAIStaggeredAdmissionsFinishTogether`): six admissions 250 ms apart (no two admissions overlap), provider answers
  arriving within the same few milliseconds. First run: in 3 of 3 rounds exactly **3 of 6** were answered 409 `ai_conflict` and their proposals remained PENDING
  (`READY, PENDING, PENDING, READY, PENDING, READY`; `used_tokens` 17,382, only the three completed ones). Final batch: **4, 3 and 4 of 6** lost
  (`used_tokens` 11,588 / 17,382 / 11,588). The Postgres log of the stack during the first probes shows 11 `could not serialize access due to concurrent update`
  aborts of `control.ai_finish` (context `UPDATE control.ai_usage SET used_tokens=...`) and 16 of `control.ai_begin` (context `INSERT INTO control.ai_usage ...
  ON CONFLICT DO NOTHING`).
* **Simultaneous admissions** (`TestReviewAIConcurrentResultsAreNotLost`): eight requests released at the same instant, three rounds. First run: 5, 6 and 6 of 8
  answered 409 `ai_conflict`; final batch: 6, 7 and 6 of 8. Most refusals happen at admission (nothing stored, nothing charged; an honest retry works), but a
  result is lost as well: once in the first run (round 3), twice in the final batch (rounds 2 and 3: `READY, PENDING` and `READY, PENDING, READY`). The 17-request
  probe of H2 lost one result in its final run too (`READY` x 7, `PENDING` x 1).
* **Bearer token that expires during the provider phase** (`TestReviewAITokenExpiresDuringProviderPhase`): 3 s of validity left, 5 s phase: HTTP 401
  after the provider answered; provider calls 1; proposal PENDING; invocation rows 0; the next honest request 429 `ai_busy`. A 15-minute token and
  a 60 s phase make this about 6.7% of full-length requests (60/900).
* **Admission queue** (IR-02 in the integration review): with four slow-body connections an honest proposal returned 503 after 9.54 s and stayed PENDING.
* **Model label** (M14R-01).

Cost of one orphan (`TestReviewAIOrphanIsSweptAndChargedByTheReservation`): after the lease, the next admission sweeps the PENDING row to `EXPIRED`
and charges its reservation: `used_tokens` 67,264 = the orphan's **67,244** + the new request's 20, i.e. 34% of the default daily budget (200,000) per
lost answer; three lost answers exhaust the tenant's day. Until the sweep the row holds an in-flight slot.

Impact: honest members of one tenant lose paid results at normal concurrency, lose them whenever their token happens to expire mid-phase, and pay a
whole reservation for each. No data is exposed and nothing is applied; the user sees a 401/409/503 and must ask again.

Fix direction: retry the result transaction on 40001/40P01 inside the detached context (the ports are idempotent for a PENDING row; a finished row
answers `not_pending`); make usage accounting insert-only (one ledger row per proposal, summed by the budget check) so concurrent finishes do not
collide on one row; charge an orphan its input estimate instead of the full reservation when no attempt was recorded; list token expiry and conflict
beside the crash in A12.

### M14R-04 MEDIUM - `rollback_release` does not write the M13R-01 fence: a rollback whose transaction began before a committed rejection moves the pointer onto the rejected hash

Status at `be6ec7c`: **OPEN**, reproduces (the same item as IR-06 of the integration review).

Contract: migration `00088` header: "M13R-01 an acknowledged rejection did not stop an activation whose REPEATABLE READ transaction began earlier:
`reject_release` and `activate_release` now both write one `control.release_fences` row per hash". `rollback_release` checks `release_rejections` for its
target ("a rejected release is no longer a valid place to return to") but does not write the fence.

Evidence: `db/migrations/00088_m13_review_fixes.sql:51` (`reject_release`) and `:70` (`activate_release`) write `control.release_fences`;
`:99-128` (`rollback_release`) reads `release_rejections` at `:115` and updates only `active_releases` (`:122`) and `release_history` (`:125`). Under
REPEATABLE READ a rejection that commits after the rollback transaction's snapshot is invisible to the check, and there is no write-write conflict.

Executed (`TestReviewRollbackOvertakesConcurrentRejection`, Appendix A): c1 activated, c2 activated (pointer c2@2). A rollback transaction opens as the
release operator and takes its snapshot; the other approver rejects c1 through the port and commits (no error); the rollback then calls
`control.rollback_release(app, c1, c2, 2, '', false)` and **succeeds**: `err=<nil> pointer=2ffe0ddc61f1@3 rejections of c1=1` (the hash differs per run; the first run printed `833e2d172551@3`). The builders' own
`TestM13R01*` (activation path) pass in the full suite; this is the same class on the third writer of the pointer.

Impact: a veto (`RISK_UNACCEPTABLE`) that was acknowledged to the rejecting approver does not stop a concurrent rollback onto the vetoed release; the
window is the length of the rollback request. Same class and grade as M13R-01.

Fix direction: in `rollback_release`, before the rejection check, `INSERT INTO control.release_fences ... ON CONFLICT ... DO UPDATE SET writes=f.writes+1`
for `(t, p_app, p_target)` exactly as `activate_release` does; extend `TestM13R01*` to the rollback path.

### M14R-05 LOW - `verifier-v2`: an exemption band that avoids the fixed probe amounts passes (documented as a "mechanical floor", reproduced)

Status at `be6ec7c`: **OPEN**, reproduces.

Contract: `internal/intent/oracle.go:169-172` ("Probes are a mechanical floor, not a proof: human cases must cover special regions"), ADR102 line 289,
`internal/verification/README.md:37`. The FR-02 fix (`verifier-v2`) added nine ratios of the threshold.

Executed (`TestReviewNarrowExemptionBandBetweenProbes`, Appendix A, unit test, no database): the builders' fixture with a threshold of 500 and orders
strictly between 600 and 900 skipping approval -> FAIL (control). The same fixture with the band moved to 626..749, 501..502 or 1500..1999 ->
**PASS, 0 findings**. A band 3001..4999 -> FAIL (it contains the 7x probe, 3500).

Impact: a release whose approval rule has a gap narrower than the probe spacing carries a PASS report. M14 makes this reachable by a model-proposed
patch (the human still acknowledges the derived sensitive aspects and M13 approvers still decide).

Fix direction: also probe every money literal the walk compares against, each +-1 minor unit and the midpoints between consecutive literals, as the
verifier already does for string, enum, boolean and integer literals (`verifier.go:439-514`); keep the fixed set.

### M14R-06 LOW - provider-reported usage is charged as reported, up to 100,000,000 tokens: one answer can lock a tenant out for the UTC day

Status at `be6ec7c`: **OPEN**, reproduces.

Executed (`TestReviewAIProviderBehaviours`, case "reports negative and enormous usage"): a provider reporting input `-2^62` and output `2^62`: HTTP 429
(`FAILED/BUDGET`) and the tenant's `used_tokens` is **100,005,674** after one request. A 3 MiB answer is charged 792,106 (the pessimistic estimate) and
also ends `FAILED/BUDGET`. ADR104 A5: "Usage is taken from the provider's report".

Fix direction: cap the per-attempt reported usage at the attempt's reservation share (input is bounded by the context size, output by
`max_output_tokens`).

### M14R-07 LOW - a provider adapter that ignores its context holds the request, its goroutine and (until the lease) its slot beyond the wall clock

Status at `be6ec7c`: **OPEN**, reproduces.

Evidence: `internal/ai/repair.go:102-109` (`call` is synchronous, no watchdog). Executed (`TestReviewAIProviderBehaviours`, case "hangs without honouring
the context"): wall clock 2 s, no HTTP answer 12 s later. The built-in HTTP adapter honours the context (net/http), so only a custom `ai.Provider` can.

Fix direction: run `Complete` in a goroutine and `select` on `ctx.Done()`; abandon and count a straggler.

### M14R-08 LOW - redaction is a mechanical filter: 10 of 28 secret shapes survive (ADR104 Amendment B.4 says so)

Status at `be6ec7c`: **OPEN**, reproduces.

Executed (`internal/ai` unit probe, source not reproduced here because it consists of secret-shaped literals): the redacted shapes include PEM blocks, URL
credentials, JWTs, DSNs, most vendor key prefixes, AWS secrets, `authorization:` headers, `password=`/`password:` assignments, one-line private keys and
zero-width-split keys. The survivors: an Azure SAS query (`sig=`), an Azure storage connection string (`AccountKey=`), `Basic <base64>` without a header name,
a password in prose ("the admin password is ..."), a Hugging Face style `hf_` token, a Stripe-style `whsec_` secret, a `Cookie:` header with a session value,
spaced digit groups (card-like), a key whose prefix uses a Cyrillic look-alike letter, and a key split over a line break (the tail survives).

Impact: free text a human types (request, labels, descriptions) may carry these into a prompt. Documented residual, so LOW.

Fix direction: extend the table; apply NFKC/confusable normalisation before matching; consider refusing a request that still contains a secret-shaped token.

## Attacks that held (each executed)

* **H1 - the prompt carries metadata only** (`TestReviewAIPromptCarriesNoIdentity`). A real route call; the serialized provider request (22,708 bytes, seven
  segments: `instructions` TRUSTED; `request`, `application`, `release_diff`, `tools`, `roles`, `requirements` UNTRUSTED) searched for the tenant id, the owner,
  admin and viewer user ids and the owner and viewer bearer tokens: **none present**. The request text appears in no `audit.entries.state` row and no
  `ai_proposals.body` row (0 hits). The first run of this probe failed because I had put the tenant id and a token into the request text myself; it was
  corrected before any claim was made.
* **H2 - no connection is held during the provider phase** (`TestReviewAIConcurrentProviderPhasesFreeTheDatabase`): 17 requests with 6 s provider phases;
  while they ran, `pg_stat_activity` showed **0** non-idle `framework_app` backends and an honest `GET /access` took 9 ms (14 and 25 ms in earlier runs); the
  service admitted 7 or 8 (Go cap `MaxInFlightPerTenant` = 8, `internal/ai/limits.go:13`) and refused the rest 429 `ai_busy`.
* **H3 - M13R-03 `Holds` equals the decision** (`TestReviewHoldsVsDecideExtended`): seven role shapes - an `ALLOW releases.approve` selector on an entity, one on a field,
  `ALLOW` for this application plus a `DENY` for every application, a plain `ALLOW` for this application, `ai.apply` + `releases.read` only, an inactive role head,
  and a role of the same name in the other tenant that grants approve - each compared with the real `POST .../candidates/{hash}/approve` answer for the
  same member: `Holds` agreed in all seven (only the plain allow holds: 201; the other six 403).
* **H4 - a decision happens once and is linearised with human edits** (`TestReviewAIDecisionHappensOnce`, `TestReviewAIApplyVersusHumanEdit`): six rounds
  of two humans applying and a third rejecting the same proposal at the same instant: exactly one decision each round (final batch: five `REJECTED`, one
  `APPLIED`; the first run: six `REJECTED`), no 5xx, the draft head moved by one only when the status ended `APPLIED`, one decision audit row per proposal. Five rounds of an AI apply racing a human metadata append for the same next
  revision: exactly one winner each round (the human append, 201; the apply 409 `draft_changed` or `ai_conflict`), the head advanced by one, the stored
  document is the human's (label unchanged), no mixture of the two.
* **H5 - requirement status and provenance never come from the model** (`TestReviewAIConfirmIgnoresWhatTheModelSaysAboutItsOwnStatus`): a scripted answer
  that names `requiredness: OPTIONAL`, `requiredness_basis: OWNER_DECISION`, `status: RESOLVED`, `provenance: HUMAN` ends `FAILED/SCHEMA_ERROR` after 3
  attempts, never READY. An honest proposal confirmed by two humans at the same instant: one 200, one 409 `requirements_changed`, requirement-set version
  0 -> 1; the stored requirement carries `MANDATORY`, `POLICY_FLOOR`, `RESOLVED`, `AI_PROPOSED_HUMAN_CONFIRMED`, and `decided_by` is one of the two confirming
  humans (the winner).
* **H6 - parser and apply invariants under fuzzing** (`FuzzReviewPatchProposal`, two runs of 90 s: 841,972 and 490,161 executions, corpus seeded with every definition of a draft
  that contains a tool): never a panic; an accepted proposal has a canonical form that re-parses to the same bytes; every operation targets an allowlisted
  kind and carries exactly one payload of that kind; applied to a draft with a tool definition, every definition outside the allowlist is byte-identical
  afterwards and the document envelope (schema version, app id, namespace) is unchanged.
* **H7 - port and table hygiene** (read-only catalog query on the migrated database, version 89): `control.ai_begin`, `ai_record_attempt`, `ai_finish`,
  `ai_decide` are SECURITY DEFINER, owned by `framework_schema_owner`, `search_path=pg_catalog, pg_temp`, executable by `framework_app` only (not the worker,
  not the dispatcher, not PUBLIC). `ai_proposals`, `ai_invocations`, `ai_usage`: RLS enabled and forced, owner `framework_schema_owner`, `framework_app` holds
  SELECT only (no INSERT/UPDATE/DELETE), worker and dispatcher hold nothing. `control.worker_grants_safe()` is true in a real `framework_worker` session
  (it evaluates `session_user`; the migrator session correctly gets false).
* **H8 - the 60-second provider phase** (`TestReviewAIFiftyEightSecondProviderPhase` and the standalone Go server experiment of the integration review): through
  `httpserver.New` (10 s read and write deadlines), a 58 s scripted answer returns HTTP 201 after 58.1 s with the proposal READY, and the next request on the same
  client succeeds (200). `reply.nextWithin` is set in one place only (`access/ai.go:401`, proposal creation); the extension equals the service's effective
  wall clock plus 10 s, which `Limits.Effective()` clamps to `APIWallClockCeiling` (`internal/ai/service.go:78-80`); a handler's `SetWriteDeadline(+30 s)` does not
  leak to the next request on a keep-alive connection, and `ReadTimeout` does not cancel a running handler.
* **H9 - provider misbehaviour is contained** (`TestReviewAIProviderBehaviours`): a panic whose message is secret-shaped -> HTTP 503 after about 50 ms,
  `FAILED/PROVIDER_ERROR`, `used_tokens` 0, the text in neither the response (191 bytes) nor any audit row; 3 MiB of text -> `FAILED/BUDGET`, charged 792,106;
  invalid UTF-8 -> `FAILED/PARSE_ERROR`; a 5,000-level nested array -> `FAILED/PARSE_ERROR`, no stack overflow.
* **H10 - denials of every AI POST route are audited** (`TestReviewRouteMatrix`, five AI POST routes among 45): anonymous 401, viewer 403 and member 403 on
  `patch-proposals`, `requirement-proposals`, `apply`, `confirm`, `reject`; denial audit rows under `ai.propose` (4) and `ai.apply` (6) among 90 denial rows for 74 denied calls.
* **H11 - SSRF address forms** (`TestReviewDeniedAddressForms`): 58 forms (IPv4-mapped, IPv4-compatible, NAT64, 6to4, Teredo, ISATAP, metadata addresses,
  CGNAT, documentation ranges) are all denied by `tools.DeniedAddress`, which the provider's guarded dialer uses; public controls are allowed.
* **H12 - the worker guard is derived and holds** (a `psql` session as the migrator and as the worker, read-only): see H7.
* **H13 - nothing logs the request or the model's text** (`grep` for `slog.`, `log.Print`, `fmt.Print`, `os.Stderr` in `internal/ai/*.go`, `access/ai.go` and
  `access/ai_decide.go`: no match; the HTTP access log records only request id, trace id, method, status and duration (`httpserver.go:232`) and the panic
  recoverer only the request id (`:194`); the secret-shaped provider panic of H9 reached neither the response nor an audit row).

* **H14 - tenant isolation of the AI routes** (`TestReviewAICrossTenantProposalAccess`): bob is an admin of tenant A and the owner of tenant B. With his tenant-B
  authority, addressing a tenant-A proposal through tenant B's path: read 404, apply 404, confirm 404, reject 404, list 200 with no proposal of tenant A (the id appears
  in no answer). A user who is a member of tenant B only, on tenant A's path: read 403, apply 403, propose 403. Afterwards the tenant-A proposal is still READY, the
  draft head still 1, one proposal row.

* **H15 - the requirement-proposal parser under fuzzing** (follow-up, `FuzzReviewRequirementProposal`, 120 s, 2,019,114 executions, seeded with a valid
  proposal of each of the four known categories, an unknown category and an open-question-only proposal): never a panic; every accepted proposal has a canonical
  form that is a fixed point of parse and canonicalise (same bytes, same hash); every draft names a category the verifier understands and has a unique id; and
  `Confirm`'s own construction - requiredness and basis from `RequirementPolicy`, provenance `AI_PROPOSED_HUMAN_CONFIRMED`, each of the three statuses a human can
  give - yields a requirement set that validates and re-parses strictly with the draft's semantics unchanged. A READY proposal is therefore never unconfirmable for a
  reason the parser could have seen (the id and category patterns of the proposal and of the set are identical, `schema.go:40-41` against `requirements.go:97-98`).
* **H16 - the HTTP provider's dial path** (follow-up, `TestReviewProviderEndpointForms`, `TestReviewProviderResolverAnswerShapes`,
  `TestReviewProviderFilterBypassSpellingsAreRefusedAtDialTime`, `TestReviewProviderHostSpelling`; a loopback TLS canary server and the builders' stub resolver, no real
  network): of 41 endpoint forms built through `NewProvider`, 28 are refused at construction (private, loopback, metadata and mapped literals, `localhost`, `.local`,
  `.internal`, userinfo, non-https, bad ports, whitespace and backslash tricks, query, fragment, full-width digits); 9 pass the construction filter (non-canonical numbers
  such as `0x7f.0.0.1`, `2130706433`, `0177.0.0.1`, `127.1`; trailing-dot names such as `localhost.`; a punycode name; upper-case and trailing-dot spellings), and
  each of those ends in a resolution failure in 0 to 4 ms here (no DNS in this environment). With a resolver that answers them with loopback, `::1`, the metadata
  address or a private address (8 spellings x 4 answers = 32 combinations, including `.local.` and `.localhost.`) every call is refused by the network policy and the
  canary receives no request. Fifteen further resolver answers (scope identifiers, IPv4-mapped metadata addresses, NAT64 and 6to4 forms, reserved and documentation
  ranges, multicast) are refused likewise. Upper-case and trailing-dot host spellings of a legitimate endpoint work (a first guess that the dial guard's host comparison
  would break them was wrong). Observation (INFO): `validateEndpoint` (`config.go:231-243`) does not strip a trailing dot, so `.local.`, `.localhost.` and `.internal.` names
  pass the construction filter; the dial-time check is the real guard and it held. The builders' redirect, same-origin redirect, proxy-environment and rebinding tests
  (`provider_http_test.go`) passed in the `internal/ai` unit run of session 2.

## Re-verification at `be6ec7c` (2026-10-22)

Same commit, worktree and method as the section "Re-verification at `be6ec7c`" of the integration review (`docs/verification/INTEGRATION_INDEPENDENT_REVIEW.md`), which also lists
what changed after the pinned commit. For this half: `internal/ai`, `internal/verification`, `internal/intent`, `internal/promotion`, `internal/audit` and the migrations
`00081` to `00089` do not differ from `552fa9e` at all (`git diff --name-only 552fa9e be6ec7c -- <path>` prints nothing for each). The one change on the path of an AI request is the
front door, `internal/platform/access/access.go` (the body pre-read of IR-02, `:293-316`), which is the fix for the slow-body stall that cost an honest proposal its result. No commit after the pinned
one changes the code that any of the eight findings below is about (the IR-02 fix removes one trigger of M14R-03, see the end of this section); every probe that shows a defect was run again and still shows it.

| Finding | At `552fa9e` | At `be6ec7c` | Probe executed at `be6ec7c`, result |
|---|---|---|---|
| M14R-01 MEDIUM | open | **OPEN** | `TestReviewAIModelLabelOutsideTheAuditCharset`: FAIL for 4 of 4 labels (503 on a finished provider call, the proposal stays `PENDING`, 0 tokens charged) |
| M14R-02 MEDIUM | open | **OPEN** | `TestReviewAICancelledCallsAreNotCharged`: FAIL, 20 provider calls with 0 tokens charged against a budget that allows 5 |
| M14R-03 MEDIUM | open | **OPEN** | `TestReviewAIStaggeredAdmissionsFinishTogether` FAIL in 3 of 3 rounds (3, 4 and 3 of 6 results lost, `PENDING`); `TestReviewAIConcurrentResultsAreNotLost` FAIL in 3 of 3 rounds (6 of 8 answered 409 `ai_conflict` in each, one stored result lost in round 3); `TestReviewAITokenExpiresDuringProviderPhase` logs the third trigger (HTTP 401 after the provider answered, proposal `PENDING`) and `TestReviewAIOrphanIsSweptAndChargedByTheReservation` its price (the next admission after the lease charges the orphan's reservation, 67,244 tokens) |
| M14R-04 MEDIUM (= IR-06) | open | **OPEN** | `TestReviewRollbackOvertakesConcurrentRejection`: FAIL, the rollback onto the rejected hash succeeds |
| M14R-05 LOW | open | **OPEN** | `TestReviewNarrowExemptionBandBetweenProbes`: FAIL, the defect is present: the bands 626..749, 501..502 and 1500..1999 skip approval and the verifier answers PASS with 0 findings; the control bands 600..900 (3 findings) and 3001..4999 (1 finding) are caught |
| M14R-06 LOW | open | **OPEN** | `TestReviewAIProviderBehaviours`, case "reports negative and enormous usage": 429 `BUDGET`, `used_tokens=100005674` charged |
| M14R-07 LOW | open | **OPEN** | the same test, case "hangs without honouring the context": FAIL, no answer 12 s after a 2 s wall clock |
| M14R-08 LOW | open | **OPEN** | `TestReviewRedactionSurvivors`: FAIL, the defect is present: 10 of 28 secret shapes survive (an Azure SAS `sig=`, an Azure storage connection string, `Basic <base64>`, a password in prose, `hf_`, `whsec_`, a `Cookie:` header, spaced digit groups, a key with a Cyrillic look-alike prefix, a key split over a line break) |

The claims that held at the pinned commit were attacked again at `be6ec7c` with the same probes and held (PASS): prompt content (`TestReviewAIPromptCarriesNoIdentity`), the 58-second provider phase
through `httpserver.New` (`TestReviewAIFiftyEightSecondProviderPhase`), concurrent provider phases leave the database free (`TestReviewAIConcurrentProviderPhasesFreeTheDatabase`), decisions happen
once (`TestReviewAIDecisionHappensOnce`), apply against a human edit (`TestReviewAIApplyVersusHumanEdit`), a proposal's own status claims never confirm it
(`TestReviewAIConfirmIgnoresWhatTheModelSaysAboutItsOwnStatus`), tenant isolation (`TestReviewAICrossTenantProposalAccess`), and `Holds` against the approve route for 7 role shapes
(`TestReviewHoldsVsDecideExtended`: 1 allowed, 6 denied, `Holds` equal to the route decision in all 7). The unit probes of the HTTP provider dial path (`TestReviewProviderEndpointForms`, `...ResolverAnswerShapes`, `...HostSpelling`, `...FilterBypassSpellingsAreRefusedAtDialTime`) and of the 58 address forms (`TestReviewDeniedAddressForms`) pass, and the two fuzz probes (`FuzzReviewPatchProposal` about 156,000 executions, `FuzzReviewRequirementProposal` about 736,000, 60 s each) found no input.

What the IR-02 fix changes for this half: of the five triggers listed under M14R-03 the fourth, an admission queue emptied by four slow-body connections (an honest proposal that returned 503 after
9.54 s and stayed `PENDING`), is gone: `TestReviewSlowBodyHoldsAdmissionSlots` (the unchanged probe) now ends with both proposals `READY`: the honest proposal behind four trickling bodies answers 201 in 1.53 s (baseline 1.55 s), where it ended `PENDING` after a 503 at 9.54 s. The other four (concurrent results, simultaneous admissions, a token that expires during the provider phase, a model label) are unchanged.
The first two do not need a slow body at all: they come from the single `control.ai_usage` row that concurrent `ai_begin` and `ai_finish` update under REPEATABLE READ (40001, answered 409
`ai_conflict`) and from a result exchange that is not retried (`access/ai.go:504-555`).

## Not covered

* A real provider, a real vendor, billing of aborted calls, model quality and latency (no model exists in this environment).
* The HTTP provider was exercised with a loopback canary server and a stub resolver (H16); a dial over a real network, real DNS rebinding and a redirect from a real
  server were **NOT RUN** (the builders' redirect and rebinding tests passed in the unit run).
* A new prompt-injection corpus: only the builders' 412-item corpus (part of the `internal/ai` unit run) and my patch and requirement-proposal fuzz.
* More than one API process (the Go in-flight cap is per process; the database cap is per tenant), HTTP/2, a proxy that cuts the connection before 70 s.
* Per-member fairness: one member can occupy every in-flight slot of the tenant (by design per tenant); not executed.
* `00086` variants beyond the builders' regression tests: the records of a frozen sandbox and the `strict_candidate_json` differential of `00087` were run in the follow-up (integration
  review, A.9 and A.10: PASS); index or unique-claim mutation of a frozen sandbox dataset: **NOT RUN**.
* `API_ENV=production` refusal of AI configuration: read in `internal/config/config.go`, not executed.
* Performance of the AI routes under the reference profile.

## Appendix A - probe sources (Phase 2)

The probes need the helpers of the repository at the pinned commit (`m14Setup`, `m13Setup`, `labelPatch`, `fixtureDoc`, ...); copy each block to the named file, then run
`node scripts/with-env.mjs ./scripts/go.sh test -tags=integration -count=1 -timeout 30m -v -run '^<TestName>$' ./tests/integration`
(unit probes: `./scripts/go.sh test -count=1 -v -run '^<TestName>$' ./internal/verification`; the fuzz probe: `./scripts/go.sh test -run '^$' -fuzz '^FuzzReviewPatchProposal$' -fuzztime=90s ./internal/ai`). They were never committed; delete them after use. The redaction probe is not reproduced: it consists of secret-shaped literals. Blocks A.1 to A.7 compile and run unchanged against `be6ec7c` as well (that is how the re-verification ran them).

### A.1 M14R-01 to M14R-03, M14R-06, M14R-07 and H1, H2, H4, H5, H8, H9, H14 - the AI route probes

File: `tests/integration/zz_review_m14_test.go`

```go
//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

// Review probes for M14 (not product code).

type reviewHangProvider struct{ release chan struct{} }

func (h *reviewHangProvider) Complete(ctx context.Context, req ai.ProviderRequest) (ai.ProviderResponse, error) {
	<-h.release // ignores the context on purpose: an adapter that does not honour it
	return ai.ProviderResponse{}, errors.New("released")
}

type reviewPanicProvider struct{}

func (reviewPanicProvider) Complete(context.Context, ai.ProviderRequest) (ai.ProviderResponse, error) {
	panic("provider exploded key=REVIEWSECRET0123456789")
}

type reviewBytesProvider struct {
	payload []byte
	usage   ai.Usage
}

func (b reviewBytesProvider) Complete(context.Context, ai.ProviderRequest) (ai.ProviderResponse, error) {
	return ai.ProviderResponse{Payload: b.payload, Provider: "bytes", Model: "m", Usage: b.usage}, nil
}

func (e *m14Env) usageRow() (requests, used int64) {
	e.t.Helper()
	_ = e.f.operator.QueryRow(`SELECT coalesce(sum(requests),0),coalesce(sum(used_tokens),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&requests, &used)
	return
}

func (e *m14Env) statuses() string {
	e.t.Helper()
	rows, err := e.f.operator.Query(`SELECT status||'/'||coalesce(failure_code,'') FROM control.ai_proposals WHERE tenant_id=$1 AND app_id=$2 ORDER BY created_at`, e.f.tenantA, e.app)
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

// The bearer token of a request is verified again in the result transaction. A token that expires while the
// provider phase runs leaves the provider called and billed and the proposal PENDING.
func TestReviewAITokenExpiresDuringProviderPhase(t *testing.T) {
	e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Late"), Delay: 5 * time.Second, Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: "scripted-model-1"})
	short := e.f.token(t, e.f.aliceSubject, map[string]any{"exp": time.Now().Unix() + 3})
	w := e.propose(short, "Rename the order entity.")
	req, used := e.usageRow()
	t.Logf("token with 3 s left, provider phase 5 s: HTTP %d %s", w.Code, strings.TrimSpace(w.Body.String()))
	t.Logf("provider calls=%d, proposals=%s, usage requests=%d used_tokens=%d, invocation rows=%d", e.provider.Calls(), e.statuses(), req, used, countRows(t, e, "control.ai_invocations"))
	// The slot: with one allowed in flight, an honest request right after the failure.
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Next"), Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}, Model: "m"})
	next := e.propose(e.alice, "Rename again.")
	t.Logf("honest request right after: HTTP %d %s", next.Code, m14Code(next))
}

func countRows(t *testing.T, e *m14Env, table string) int {
	t.Helper()
	var n int
	_ = e.f.operator.QueryRow(`SELECT count(*) FROM `+table+` WHERE tenant_id=$1`, e.f.tenantA).Scan(&n)
	return n
}

func TestReviewAIProviderBehaviours(t *testing.T) {
	cases := []struct {
		name     string
		provider ai.Provider
		wall     time.Duration
	}{
		{"panics with a secret-shaped message", reviewPanicProvider{}, 0},
		{"returns 3 MiB of text", reviewBytesProvider{payload: bytes.Repeat([]byte("a"), 3<<20), usage: ai.Usage{InputTokens: 1, OutputTokens: 1}}, 0},
		{"returns invalid UTF-8", reviewBytesProvider{payload: append([]byte(`{"version":"ai-patch-proposal-v1","operations":[],"assumptions":["`), []byte{0xff, 0xfe, 0xc0, 0x80, '"', ']', '}'}...)}, 0},
		{"returns a deeply nested object", reviewBytesProvider{payload: []byte(strings.Repeat("[", 5000) + strings.Repeat("]", 5000))}, 0},
		{"reports negative and enormous usage", reviewBytesProvider{payload: []byte(`{}`), usage: ai.Usage{InputTokens: -1 << 62, OutputTokens: 1 << 62}}, 0},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			e := m14Setup(t, m14Options{gateway: ai.NewGateway(c.provider, ai.Options{})})
			e.draft("500")
			started := time.Now()
			w := e.propose(e.alice, "Rename the order entity.")
			req, used := e.usageRow()
			body := w.Body.String()
			t.Logf("HTTP %d after %v; proposals=%s; usage requests=%d used_tokens=%d; body %d bytes", w.Code, time.Since(started).Round(time.Millisecond), e.statuses(), req, used, len(body))
			if strings.Contains(body, "REVIEWSECRET") {
				t.Errorf("the provider's panic text reached the response body")
			}
			var audit int
			_ = e.f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE tenant_id=$1 AND state::text LIKE '%REVIEWSECRET%'`, e.f.tenantA).Scan(&audit)
			if audit != 0 {
				t.Errorf("the provider's panic text reached %d audit rows", audit)
			}
		})
	}
	t.Run("hangs without honouring the context (wall clock 2 s)", func(t *testing.T) {
		hang := &reviewHangProvider{release: make(chan struct{})}
		e := m14Setup(t, m14Options{gateway: ai.NewGateway(hang, ai.Options{}), limits: ai.ServiceLimits{WallClock: 2 * time.Second}})
		e.draft("500")
		done := make(chan int, 1)
		started := time.Now()
		go func() { w := e.propose(e.alice, "Rename the order entity."); done <- w.Code }()
		select {
		case code := <-done:
			t.Logf("answered HTTP %d after %v", code, time.Since(started).Round(time.Millisecond))
		case <-time.After(12 * time.Second):
			t.Logf("NO ANSWER 12 s after a 2 s wall clock: the request goroutine is blocked in the provider; proposals=%s", e.statuses())
			close(hang.release)
			<-done
			t.Errorf("a provider that ignores its context holds the request past the wall clock")
		}
	})
}

// Sixteen provider phases at once must not hold the database: an honest request is served while they run.
func TestReviewAIConcurrentProviderPhasesFreeTheDatabase(t *testing.T) {
	const n = 16
	e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: n, DailyTokens: 50_000_000}})
	doc := e.draft("500")
	for i := 0; i < n+1; i++ {
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Slow"), Delay: 6 * time.Second, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
	}
	var wg sync.WaitGroup
	codes := make([]int, n+1)
	names := make([]string, n+1)
	for i := 0; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := e.propose(e.alice, "Rename the order entity.")
			codes[i], names[i] = w.Code, m14Code(w)
		}(i)
		time.Sleep(30 * time.Millisecond)
	}
	time.Sleep(2 * time.Second)
	var active int
	_ = e.f.operator.QueryRow(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND usename IN ('framework_app') AND state<>'idle'`).Scan(&active)
	started := time.Now()
	honest := m14Do(e.h, "GET", "/v1/tenants/"+e.f.tenantA+"/access", e.viewer, nil)
	honestTook := time.Since(started)
	t.Logf("while %d provider phases run: active app backends=%d, honest GET HTTP %d in %v", n, active, honest.Code, honestTook.Round(time.Millisecond))
	wg.Wait()
	count := map[int]int{}
	for i, c := range codes {
		count[c]++
		if c != 201 {
			t.Logf("request %d -> %d %s", i, c, names[i])
		}
	}
	t.Logf("status codes of %d concurrent proposals (limit %d in flight): %v; proposals=%s", n+1, n, count, e.statuses())
	if honest.Code != http.StatusOK || honestTook > 3*time.Second {
		t.Errorf("an honest request was delayed or refused while provider phases ran")
	}
}

// The audit row of the result exchange carries the provider-reported model label as the gateway saw it, while the
// trace table stores it after apiLabel. internal/audit accepts only [A-Za-z0-9._:/@+-]. A label outside that set
// (spaces, parentheses, non-ASCII) is something a provider chooses, not the tenant.
func TestReviewAIModelLabelOutsideTheAuditCharset(t *testing.T) {
	for _, label := range []string{"alpha 4 turbo", "Beta-3.1 (70B)", "модель-1", "model=fast"} {
		label := label
		t.Run(label, func(t *testing.T) {
			e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
			doc := e.draft("500")
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Renamed"), Usage: ai.Usage{InputTokens: 1200, OutputTokens: 300}, Model: label})
			w := e.propose(e.alice, "Rename the order entity.")
			req, used := e.usageRow()
			t.Logf("model %q: HTTP %d %s", label, w.Code, strings.TrimSpace(w.Body.String()))
			t.Logf("provider calls=%d proposals=%s invocation rows=%d usage requests=%d used_tokens=%d", e.provider.Calls(), e.statuses(), countRows(t, e, "control.ai_invocations"), req, used)
			e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Again"), Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}, Model: "m"})
			next := e.propose(e.alice, "Rename again.")
			t.Logf("the next honest request: HTTP %d %s", next.Code, m14Code(next))
			if w.Code != 201 {
				t.Errorf("a provider-chosen model label %q turns a finished provider call into HTTP %d", label, w.Code)
			}
		})
	}
}

// A request whose client goes away after the provider call started: the gateway returns before it records the
// attempt, so the call is neither traced nor charged. Repeated, that is provider spend outside the daily budget.
func TestReviewAICancelledCallsAreNotCharged(t *testing.T) {
	const cycles = 20
	e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1, DailyTokens: 40000, MaxAttempts: 1, MaxOutputTokens: 1000}})
	doc := e.draft("500")
	for i := 0; i < cycles+4; i++ {
		e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Slow"), Delay: 3 * time.Second, Usage: ai.Usage{InputTokens: 3000, OutputTokens: 800}, Model: "m"})
	}
	for i := 0; i < cycles; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest("POST", e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Rename the order entity."}`)).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+e.alice)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() { e.h.ServeHTTP(w, req); close(done) }()
		time.Sleep(400 * time.Millisecond) // the provider call has started
		cancel()
		<-done
	}
	reqs, used := e.usageRow()
	calls := e.provider.Calls()
	t.Logf("%d cancelled cycles: provider calls=%d, usage requests=%d used_tokens=%d, proposals=%s, invocation rows=%d", cycles, calls, reqs, used, e.statuses(), countRows(t, e, "control.ai_invocations"))
	// Control: how many completed requests does the same budget allow?
	e2 := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1, DailyTokens: 40000, MaxAttempts: 1, MaxOutputTokens: 1000}})
	doc2 := e2.draft("500")
	completed, refused := 0, 0
	for i := 0; i < cycles; i++ {
		e2.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc2, "entity.order", "Fast"), Usage: ai.Usage{InputTokens: 3000, OutputTokens: 800}, Model: "m"})
		switch w := e2.propose(e2.alice, "Rename the order entity."); w.Code {
		case 201:
			completed++
		default:
			refused++
			_ = m14Code(w)
		}
	}
	_, used2 := e2.usageRow()
	t.Logf("control, same budget, completed requests: %d accepted, %d refused, used_tokens=%d", completed, refused, used2)
	if calls > completed+1 {
		t.Errorf("%d provider calls were made while the budget, spent honestly, allows %d: cancelled calls are not charged", calls, completed)
	}
}

// What a provider request carries: metadata only. Every segment of every request of a real route is searched for
// the tenant id, the actor ids, the bearer tokens and an operator DSN.
func TestReviewAIPromptCarriesNoIdentity(t *testing.T) {
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Orders"), Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}, Model: "m"})
	w := e.propose(e.alice, "Rename the order entity to Orders.")
	if w.Code != 201 {
		t.Fatalf("proposal HTTP %d %s", w.Code, w.Body.String())
	}
	reqs := e.provider.Requests()
	raw, _ := json.Marshal(reqs)
	text := string(raw)
	for name, secret := range map[string]string{"tenant id": e.f.tenantA, "owner id": e.f.alice, "admin id": e.f.bob, "viewer id": e.f.viewer, "owner bearer token": e.alice, "viewer bearer token": e.viewer} {
		if secret != "" && strings.Contains(text, secret) {
			t.Errorf("the %s reached a provider request", name)
		}
	}
	t.Logf("%d provider request(s), %d bytes; segments: %s", len(reqs), len(raw), segmentNames(reqs))
	// The request text the human typed is stored nowhere but as a hash.
	var hits int
	_ = e.f.operator.QueryRow(`SELECT (SELECT count(*) FROM audit.entries WHERE state::text LIKE '%Rename the order entity to Orders%')+(SELECT count(*) FROM control.ai_proposals WHERE convert_from(coalesce(body,'\x'::bytea),'UTF8') LIKE '%Rename the order entity to Orders%')`).Scan(&hits)
	if hits != 0 {
		t.Errorf("the request text was found in %d stored rows", hits)
	}
}

func segmentNames(reqs []ai.ProviderRequest) string {
	var out []string
	for _, r := range reqs {
		for _, s := range r.Segments {
			out = append(out, s.Name+"/"+string(s.Trust))
		}
	}
	return strings.Join(out, ",")
}

// The full 60 s contract: an answer after 58 s through the production http.Server settings, then the same
// keep-alive connection serves a normal request.
func TestReviewAIFiftyEightSecondProviderPhase(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 60 s")
	}
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Late"), Delay: 58 * time.Second, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := httpserver.New(ln.Addr().String(), e.h)
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	client := &http.Client{Timeout: 120 * time.Second}
	started := time.Now()
	req, _ := http.NewRequest("POST", "http://"+ln.Addr().String()+e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Rename the order entity."}`))
	req.Header.Set("Authorization", "Bearer "+e.alice)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("58 s provider phase failed after %v: %v", time.Since(started).Round(time.Millisecond), err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("58 s provider phase -> HTTP %d after %v (%d bytes); proposals=%s", resp.StatusCode, time.Since(started).Round(100*time.Millisecond), len(b), e.statuses())
	next, _ := http.NewRequest("GET", "http://"+ln.Addr().String()+"/v1/tenants/"+e.f.tenantA+"/access", nil)
	next.Header.Set("Authorization", "Bearer "+e.viewer)
	r2, err := client.Do(next)
	if err != nil {
		t.Fatalf("the next request on the same client failed: %v", err)
	}
	r2.Body.Close()
	if resp.StatusCode != 201 || r2.StatusCode != 200 {
		t.Errorf("HTTP %d then %d", resp.StatusCode, r2.StatusCode)
	}
}

// A request that failed to record its outcome leaves a PENDING row; it is swept and charged pessimistically by the next admission
// after its lease (wall clock + 20 s).
func TestReviewAIOrphanIsSweptAndChargedByTheReservation(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 30 s")
	}
	e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1, WallClock: 2 * time.Second}})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Lost"), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 1000, OutputTokens: 200}, Model: "m"})
	short := e.f.token(t, e.f.aliceSubject, map[string]any{"exp": time.Now().Unix() + 1})
	w := e.propose(short, "Rename the order entity.")
	_, used0 := e.usageRow()
	t.Logf("expired token mid-phase: HTTP %d; proposals=%s used_tokens=%d", w.Code, e.statuses(), used0)
	time.Sleep(23 * time.Second)
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Next"), Usage: ai.Usage{InputTokens: 10, OutputTokens: 10}, Model: "m"})
	next := e.propose(e.alice, "Rename again.")
	_, used1 := e.usageRow()
	t.Logf("after the lease: next request HTTP %d; proposals=%s used_tokens=%d (the orphan's reservation is charged: %d)", next.Code, e.statuses(), used1, used1-used0-20)
}

// Review probe: concurrent result exchanges of one tenant. Eight proposals finish within milliseconds of each other
// (equal provider delay, the in-flight cap of the Go service is 8). Each finish transaction writes the tenant's usage
// row under REPEATABLE READ. A transaction that loses the write-write race is answered 409 ai_conflict and its
// proposal stays PENDING while the provider's answer (already paid) is discarded.
func TestReviewAIConcurrentResultsAreNotLost(t *testing.T) {
	for round := 1; round <= 3; round++ {
		round := round
		t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
			const n = 8
			e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: n, DailyTokens: 50_000_000}})
			doc := e.draft("500")
			for i := 0; i < n; i++ {
				e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", fmt.Sprintf("Label %d", i)), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
			}
			var wg sync.WaitGroup
			codes := make([]string, n)
			start := make(chan struct{})
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					w := e.propose(e.alice, "Rename the order entity.")
					codes[i] = fmt.Sprintf("%d %s", w.Code, m14Code(w))
				}(i)
			}
			close(start)
			wg.Wait()
			var used int64
			var requests int
			_ = e.f.operator.QueryRow(`SELECT coalesce(sum(used_tokens),0), coalesce(sum(requests),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&used, &requests)
			t.Logf("round %d: answers=%v proposals=%s usage: requests=%d used_tokens=%d", round, codes, e.statuses(), requests, used)
			lost := 0
			for _, c := range codes {
				if !strings.HasPrefix(c, "201") {
					lost++
				}
			}
			if lost > 0 {
				t.Errorf("%d of %d concurrent proposals did not return their stored result (a paid provider answer was discarded)", lost, n)
			}
		})
	}
}

// Review probe: separate the two steps. Admissions are staggered (250 ms apart, so no two admission transactions
// overlap) and the scripted provider delays are chosen so that all answers arrive within the same few milliseconds.
// Only the result exchanges then run concurrently.
func TestReviewAIStaggeredAdmissionsFinishTogether(t *testing.T) {
	for round := 1; round <= 3; round++ {
		round := round
		t.Run(fmt.Sprintf("round_%d", round), func(t *testing.T) {
			const n = 6
			const spacing = 250 * time.Millisecond
			e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: n, DailyTokens: 50_000_000}})
			doc := e.draft("500")
			for i := 0; i < n; i++ {
				e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", fmt.Sprintf("Label %d", i)), Delay: 3*time.Second - time.Duration(i)*spacing, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "m"})
			}
			var wg sync.WaitGroup
			codes := make([]string, n)
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					w := e.propose(e.alice, "Rename the order entity.")
					codes[i] = fmt.Sprintf("%d %s", w.Code, m14Code(w))
				}(i)
				time.Sleep(spacing)
			}
			wg.Wait()
			var used int64
			var requests int
			_ = e.f.operator.QueryRow(`SELECT coalesce(sum(used_tokens),0), coalesce(sum(requests),0) FROM control.ai_usage WHERE tenant_id=$1`, e.f.tenantA).Scan(&used, &requests)
			t.Logf("round %d: answers=%v proposals=%s usage: requests=%d used_tokens=%d", round, codes, e.statuses(), requests, used)
			lost := 0
			for _, c := range codes {
				if !strings.HasPrefix(c, "201") {
					lost++
				}
			}
			if lost > 0 {
				t.Errorf("%d of %d staggered proposals did not return their stored result", lost, n)
			}
		})
	}
}

// ---------------------------------------------------------------- decisions

type reviewDecision struct {
	who  string
	code int
	name string
}

// race runs the calls at the same instant and returns their answers.
func reviewRace(calls map[string]func() *httptest.ResponseRecorder) []reviewDecision {
	var wg sync.WaitGroup
	start := make(chan struct{})
	out := make(chan reviewDecision, len(calls))
	for who, call := range calls {
		who, call := who, call
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := call()
			out <- reviewDecision{who, w.Code, m14Code(w)}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	var all []reviewDecision
	for d := range out {
		all = append(all, d)
	}
	return all
}

// A proposal is decided once: two humans apply it and a third rejects it at the same instant.
func TestReviewAIDecisionHappensOnce(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	for round := 1; round <= 6; round++ {
		before, _ := e.heads()
		p := e.readyPatch(before, "entity.order", fmt.Sprintf("Round %d", round))
		answers := reviewRace(map[string]func() *httptest.ResponseRecorder{
			"apply-alice":  func() *httptest.ResponseRecorder { return e.apply(e.alice, p.ID, before, nil) },
			"apply-bob":    func() *httptest.ResponseRecorder { return e.apply(e.bob, p.ID, before, nil) },
			"reject-carol": func() *httptest.ResponseRecorder { return e.reject(e.carol.token, p.ID, "OTHER") },
		})
		ok := 0
		for _, a := range answers {
			if a.code/100 == 2 {
				ok++
			}
			if a.code >= 500 {
				t.Errorf("round %d: %s answered %d %s", round, a.who, a.code, a.name)
			}
		}
		final := e.proposal(e.ai("GET", "/proposals/"+p.ID, e.alice, nil), 200)
		after, _ := e.heads()
		applied, discarded := 0, 0
		for _, st := range e.auditStates("ai.proposal.applied") {
			if st["ai_proposal_id"] == p.ID {
				applied++
			}
		}
		for _, st := range e.auditStates("ai.proposal.discarded") {
			if st["ai_proposal_id"] == p.ID {
				discarded++
			}
		}
		t.Logf("round %d: answers=%v final=%s head %d->%d audit applied=%d discarded=%d", round, answers, final.Status, before, after, applied, discarded)
		wantHead := before
		if final.Status == ai.ProposalApplied {
			wantHead = before + 1
		}
		if ok != 1 || after != wantHead || applied+discarded != 1 {
			t.Errorf("round %d: %d successful decisions, head %d->%d, %d decision audit rows (status %s)", round, ok, before, after, applied+discarded, final.Status)
		}
	}
}

// An AI apply and a human edit race for the same next revision: exactly one wins, and the stored revision is
// one of the two documents, never a mixture.
func TestReviewAIApplyVersusHumanEdit(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	for round := 1; round <= 5; round++ {
		before, _ := e.heads()
		label := fmt.Sprintf("Race %d", round)
		p := e.readyPatch(before, "entity.order", label)
		human, _ := json.Marshal(orderDocument(e.app, fmt.Sprintf("%d", 600+round)))
		answers := reviewRace(map[string]func() *httptest.ResponseRecorder{
			"apply-alice": func() *httptest.ResponseRecorder { return e.apply(e.alice, p.ID, before, nil) },
			"human-bob": func() *httptest.ResponseRecorder {
				return m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.bob, before, human)
			},
		})
		after, _ := e.heads()
		doc := e.storedDoc(after)
		entity := m14Def(t, doc, "entity.order")
		t.Logf("round %d: answers=%v head %d->%d label=%q", round, answers, before, after, entity.Label)
		wins := 0
		for _, a := range answers {
			if a.code/100 == 2 {
				wins++
			}
			if a.code >= 500 {
				t.Errorf("round %d: %s answered %d %s", round, a.who, a.code, a.name)
			}
		}
		if wins != 1 || after != before+1 {
			t.Errorf("round %d: %d winners, head %d->%d", round, wins, before, after)
		}
	}
}

// Requirements: the model cannot name requiredness, status or provenance; a confirmed requirement carries the policy
// floor and the AI-proposed-human-confirmed provenance whatever the proposal said; two confirmations at once decide once.
func TestReviewAIConfirmIgnoresWhatTheModelSaysAboutItsOwnStatus(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	sem, _ := json.Marshal(intent.ApprovalThreshold{WorkflowID: "workflow.order.approve", EntityID: "entity.order", FieldID: "field.order.amount", Currency: "USD", Op: "GT", Amount: "500", ApproverRoles: []string{"admin"}})
	forged := fmt.Sprintf(`{"version":%q,"requirements":[{"id":"r-forged","category":"approval_threshold","statement":"s","semantics":%s,"requiredness":"OPTIONAL","requiredness_basis":"OWNER_DECISION","status":"RESOLVED","provenance":"HUMAN"}],"open_questions":[],"proposed_tests":[],"assumptions":[]}`, ai.RequirementProposalVersion, sem)
	for i := 0; i < 4; i++ {
		e.provider.Push(step([]byte(forged)))
	}
	w := e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": "Orders above 500 USD need admin approval."})
	var got ai.Proposal
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	t.Logf("forged requiredness/status/provenance keys: HTTP %d status=%s failure=%s attempts=%d", w.Code, got.Status, got.FailureCode, got.Attempts)
	if got.Status == ai.ProposalReady {
		t.Errorf("a requirement proposal that names requiredness, status and provenance became READY")
	}
	// The honest shape: the human confirms; the stored requirement carries the floor and the provenance.
	e.provider.Push(e.reqAnswer("r-honest"))
	p := e.proposal(e.ai("POST", "/requirement-proposals", e.alice, map[string]any{"request": "Orders above 500 USD need admin approval."}), 201)
	_, version := e.heads()
	answers := reviewRace(map[string]func() *httptest.ResponseRecorder{
		"confirm-alice": func() *httptest.ResponseRecorder {
			return e.confirm(e.alice, p.ID, version, map[string]string{"r-honest": "RESOLVE"})
		},
		"confirm-bob": func() *httptest.ResponseRecorder {
			return e.confirm(e.bob, p.ID, version, map[string]string{"r-honest": "RESOLVE"})
		},
	})
	_, after := e.heads()
	t.Logf("two confirmations at once: %v requirement-set version %d -> %d", answers, version, after)
	wins := 0
	for _, a := range answers {
		if a.code/100 == 2 {
			wins++
		}
	}
	if wins != 1 || after != version+1 {
		t.Errorf("%d winners, version %d -> %d", wins, version, after)
	}
	var body []byte
	var decidedBy string
	if err := e.f.operator.QueryRow(`SELECT body, decided_by::text FROM control.requirement_sets WHERE tenant_id=$1 AND app_id=$2 ORDER BY version DESC LIMIT 1`, e.f.tenantA, e.app).Scan(&body, &decidedBy); err != nil {
		t.Fatal(err)
	}
	set, _, _, err := intent.ParseRequirementSet(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range set.Requirements {
		t.Logf("stored requirement %s: requiredness=%s basis=%s status=%s provenance=%s decided_by_alice=%v", r.ID, r.Requiredness, r.RequirednessBasis, r.Status, r.Provenance, decidedBy == e.f.alice || decidedBy == e.f.bob)
		if r.ID == "r-honest" && (r.Requiredness != intent.Mandatory || r.RequirednessBasis != intent.BasisPolicyFloor || r.Provenance != intent.ProvenanceAIConfirmed || r.Status != intent.StatusResolved) {
			t.Errorf("stored requirement %+v", r)
		}
	}
}

// Tenant isolation of the AI routes: bob is an admin of tenant A and the owner of tenant B. With his tenant-B
// authority he addresses a tenant-A proposal through tenant B's path. Nothing may be read, decided or listed.
func TestReviewAICrossTenantProposalAccess(t *testing.T) {
	e := m14Setup(t, m14Options{})
	e.draft("500")
	p := e.readyPatch(1, "entity.order", "Mine")
	base := "/v1/tenants/" + e.f.tenantB + "/preview/" + e.app + "/ai"
	for _, c := range []struct {
		name, method, suffix string
		body                 any
	}{
		{"read", "GET", "/proposals/" + p.ID, nil},
		{"list", "GET", "/proposals", nil},
		{"apply", "POST", "/proposals/" + p.ID + "/apply", map[string]any{"expected_revision": 1, "acknowledge": []string{}}},
		{"confirm", "POST", "/proposals/" + p.ID + "/confirm", map[string]any{"expected_requirements_version": 0, "decisions": []map[string]string{}}},
		{"reject", "POST", "/proposals/" + p.ID + "/reject", map[string]any{"reason": "OTHER"}},
	} {
		w := m14Do(e.h, c.method, base+c.suffix, e.bob, c.body)
		t.Logf("tenant B path, %-7s -> HTTP %d %s", c.name, w.Code, m14Code(w))
		if strings.Contains(w.Body.String(), p.ID) {
			t.Errorf("%s: the tenant-A proposal id appears in a tenant-B answer", c.name)
		}
		if c.method == "POST" && w.Code/100 == 2 {
			t.Errorf("%s: a tenant-A proposal was decided through tenant B", c.name)
		}
	}
	// The same id with the tenant-A path and a user who is not a member of tenant A at all.
	outsider := e.addMemberIn(e.f.tenantB, "outsider-ai", "owner")
	for _, c := range []struct {
		name, method, suffix string
		body                 any
	}{
		{"read", "GET", "/proposals/" + p.ID, nil},
		{"apply", "POST", "/proposals/" + p.ID + "/apply", map[string]any{"expected_revision": 1, "acknowledge": []string{}}},
		{"propose", "POST", "/patch-proposals", map[string]any{"request": "Rename the order entity."}},
	} {
		w := m14Do(e.h, c.method, e.root+"/ai"+c.suffix, outsider.token, c.body)
		t.Logf("tenant A path, tenant-B-only user, %-7s -> HTTP %d %s", c.name, w.Code, m14Code(w))
		if w.Code/100 == 2 {
			t.Errorf("%s: a user who is not a member of tenant A got HTTP %d", c.name, w.Code)
		}
	}
	final := e.proposal(e.ai("GET", "/proposals/"+p.ID, e.alice, nil), 200)
	head, _ := e.heads()
	t.Logf("tenant-A proposal afterwards: %s, draft head %d, proposals %d", final.Status, head, e.proposalRows())
	if final.Status != ai.ProposalReady || head != 1 {
		t.Errorf("the proposal or the draft changed: %s head %d", final.Status, head)
	}
}
```

### A.2 M14R-04 - rollback overtakes a concurrent rejection

File: `tests/integration/zz_review_rollback_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

// Review probe (not product code). M13R-01 closed "an acknowledged rejection does not stop an activation whose
// transaction began earlier" by making reject_release and activate_release write the same control.release_fences row
// (migration 00088). rollback_release checks release_rejections for its target too ("a rejected release is no longer a
// valid place to return to") but writes no fence row, so under REPEATABLE READ a rejection that commits after the
// rollback transaction's snapshot is invisible to it and no write-write conflict occurs.
func (e *m13Env) reviewHold(subject string, ready chan<- struct{}, proceed <-chan struct{}, query string, args ...any) error {
	return postgres.WithRuntime(context.Background(), e.f.db, func(ctx context.Context, q postgres.Executor) error {
		if err := e.bindDirect(ctx, q, subject, e.f.tenantA); err != nil {
			return err
		}
		ready <- struct{}{}
		<-proceed
		var dst any
		return q.QueryRowContext(ctx, query, args...).Scan(&dst)
	})
}

func TestReviewRollbackOvertakesConcurrentRejection(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	c1, _ := e.build("500", nil, e.carol)
	m13Code(t, "approve c1", e.httpApprove(e.dave.token, c1), 201)
	m13Code(t, "activate c1", e.httpActivate(e.erin.token, c1, "", 0), 200)
	c2, _ := e.build("500", m13Relabel("Second release"), e.carol)
	m13Code(t, "approve c2", e.httpApprove(e.dave.token, c2), 201)
	m13Code(t, "activate c2", e.httpActivate(e.erin.token, c2, c1, 1), 200)
	if hash, gen, _ := e.activePointer(); hash != c2 || gen != 2 {
		t.Fatalf("setup: pointer %s@%d", hash, gen)
	}
	// Control, no race: the rollback target that is already rejected is refused.
	// Race: the rollback transaction exists (snapshot taken), then the rejection commits, then the rollback calls the port.
	ready, proceed, done := make(chan struct{}, 1), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- e.reviewHold(e.erin.subject, ready, proceed, `SELECT control.rollback_release($1,$2,$3,$4,$5,$6)`, e.app, c1, c2, int64(2), "", false)
	}()
	<-ready
	if _, err := e.rejectPort(e.f.bobSubject, c1, "RISK_UNACCEPTABLE", "veto"); err != nil {
		t.Fatal(err)
	}
	close(proceed)
	rerr := <-done
	hash, gen, _ := e.activePointer()
	t.Logf("rollback after a committed rejection of its target: err=%v pointer=%s@%d rejections of c1=%d", rerr, short(hash), gen, len(e.rejectionsOf(c1)))
	if rerr != nil && strings.Contains(rerr.Error(), "plan") {
		t.Skipf("the rollback needed an acknowledged plan; repeat the race with a plan: %v", rerr)
	}
	if rerr == nil && hash == c1 {
		t.Errorf("the release pointer moved onto a hash whose rejection had committed before the rollback committed")
	}
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
```

### A.3 H3 - `RoleStore.Holds` against the approve route

File: `tests/integration/zz_review_holds_test.go`

```go
//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// Review probe (not product code): policy.RoleStore.Holds (what activation asks about an approver who is not the
// caller) against the answer of the real approve route for the same member, over role shapes the builders' test
// (TestM13R03HoldsMatchesDecide) does not use: selectors on an entity or a field, DENY on every application with an ALLOW on this one,
// an action list without the action, an inactive role head, and the same role name in two tenants.
func TestReviewHoldsVsDecideExtended(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	e.useAPI(func(a *access.API) { a.PolicyLoader = (policy.RoleStore{}).Load })
	publish := func(tenantID, token, name string, active bool, grants ...map[string]any) {
		t.Helper()
		def := map[string]any{"schema_version": "tenant-capabilities-v1", "name": name, "grants": grants}
		w := m5Call(e.h, "/v1/tenants/"+tenantID+"/roles", token, map[string]any{"operation_id": uuid(t), "expected_version": 0, "active": active, "definition": def})
		if w.Code/100 != 2 {
			t.Fatal("publish role", name, w.Code, w.Body.String())
		}
	}
	sel := func(app, entity, field string) map[string]string {
		return map[string]string{"app_id": app, "entity_id": entity, "field_id": field}
	}
	allow := func(actions []string, r map[string]string) map[string]any {
		return map[string]any{"effect": "ALLOW", "actions": actions, "resource": r}
	}
	deny := func(actions []string, r map[string]string) map[string]any {
		return map[string]any{"effect": "DENY", "actions": actions, "resource": r}
	}
	approve := []string{"releases.approve"}
	publish(e.f.tenantA, e.alice, "entity_scoped", true, allow(approve, sel(e.app, "entity.order", "*")))
	publish(e.f.tenantA, e.alice, "field_scoped", true, allow(approve, sel(e.app, "*", "field.order.amount")))
	publish(e.f.tenantA, e.alice, "deny_all_allow_here", true, allow(approve, sel(e.app, "*", "*")), deny(approve, sel("*", "*", "*")))
	publish(e.f.tenantA, e.alice, "allow_here", true, allow(approve, sel(e.app, "*", "*")))
	publish(e.f.tenantA, e.alice, "ai_apply_only", true, allow([]string{"ai.apply", "releases.read"}, sel("*", "*", "*")))
	publish(e.f.tenantA, e.alice, "inactive_allow", false, allow(approve, sel("*", "*", "*")))
	publish(e.f.tenantA, e.alice, "same_name", true, allow([]string{"releases.read"}, sel("*", "*", "*")))
	publish(e.f.tenantB, e.outsider, "same_name", true, allow(approve, sel("*", "*", "*")))
	root := "/v1/tenants/" + e.f.tenantA
	roles := []struct {
		role string
		who  m13Actor
		want bool
	}{
		{"entity_scoped", e.member("m_entity", "viewer"), false},
		{"field_scoped", e.member("m_field", "viewer"), false},
		{"deny_all_allow_here", e.member("m_denyall", "viewer"), false},
		{"allow_here", e.member("m_allow", "viewer"), true},
		{"ai_apply_only", e.member("m_ai", "viewer"), false},
		{"inactive_allow", e.member("m_inactive", "viewer"), false},
		{"same_name", e.member("m_same", "viewer"), false},
	}
	for _, r := range roles {
		if r.role != "inactive_allow" { // an inactive head cannot be assigned: the member stays a viewer
			r5Assign(t, e.h, root, e.alice, r.who.id, r.role, 200)
		}
	}
	c1, _ := e.build("500", nil, e.carol)
	resource := policy.Resource{TenantID: e.f.tenantA, Type: "preview", AppID: e.app}
	for _, r := range roles {
		w := e.httpApprove(r.who.token, c1)
		viaRoute := w.Code == 201 || w.Code == 200
		var viaHolds bool
		if err := e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, a tenant.Access) (err error) {
			viaHolds, err = (policy.RoleStore{}).Holds(ctx, q, a, r.who.id, r.role, "releases.approve", resource)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		t.Logf("%-20s approve route HTTP %d (allowed=%v), Holds=%v, expected %v", r.role, w.Code, viaRoute, viaHolds, r.want)
		if viaRoute != viaHolds || viaRoute != r.want {
			t.Errorf("%s: route %v, Holds %v, expected %v", r.role, viaRoute, viaHolds, r.want)
		}
	}
}
```

### A.4 M14R-05 - narrow exemption band against `verifier-v2` (unit test)

File: `internal/verification/zz_review_verifier_test.go`

```go
package verification

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Review probe (not product code): the builder's exemption band is 600 to 900 over a 500 threshold. The
// derived probes are fixed amounts (threshold +-1 unit, 0, negative, decades, 10x, 1000x and nine ratios of the
// threshold). A narrower band that lands between two probes is not seen.
func TestReviewNarrowExemptionBandBetweenProbes(t *testing.T) {
	set := requirementSet(thresholdRequirement("po-approval", regular))
	spec := controlSpec(set)
	variant := func(low, high string) compiler.Program {
		raw, err := json.Marshal(orderDocument(orderOptions{exemptBand: true}))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Count(text, `"amount":"600"`) != 1 || strings.Count(text, `"amount":"900"`) != 1 {
			t.Fatal("the fixture spells the band differently than this probe expects")
		}
		text = strings.Replace(strings.Replace(text, `"amount":"600"`, `"amount":"`+low+`"`, 1), `"amount":"900"`, `"amount":"`+high+`"`, 1)
		var doc metadata.Document
		if err = json.Unmarshal([]byte(text), &doc); err != nil {
			t.Fatal(err)
		}
		return compileDocument(t, doc, compiler.PlatformCatalog())
	}
	// Desired behaviour: every band that lets orders skip the approval must FAIL the verification.
	for _, c := range [][2]string{
		{"600", "900"},   // the builders' own band: control, caught by the ratio probes
		{"3001", "4999"}, // contains the 7x probe (3500): caught
		{"626", "749"},   // between the 1.25x and 1.5x probes
		{"501", "502"},   // between "above" (500.01) and the 0.75x..1.25x ratios
		{"1500", "1999"}, // between the 2x and 3x probes
	} {
		r := mustVerify(t, input(variant(c[0], c[1]), set, spec))
		t.Logf("orders strictly between %s and %s skip the approval -> verifier outcome %s findings=%d", c[0], c[1], r.Outcome, len(r.Findings))
		if r.Outcome != Fail {
			t.Errorf("band %s..%s: the verifier answered %s for a release that lets those orders skip approval", c[0], c[1], r.Outcome)
		}
	}
}
```

### A.5 H6 - patch proposal parser and apply invariants (fuzz)

File: `internal/ai/zz_review_fuzz_test.go`

```go
package ai

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// Review probe (not product code). Invariants that must hold for ANY byte string a model could send:
//   - parsing never panics;
//   - an accepted proposal re-parses from its own canonical form to the same canonical form;
//   - every operation targets an allowlisted kind and carries a definition of exactly that kind;
//   - applied to a draft that holds a tool definition, the patched document keeps every definition of a kind
//     outside the allowlist byte for byte, and the document envelope (schema_version, app_id, namespace) is unchanged;
//   - every definition of the patched document has exactly one payload and it matches its kind.
func payloadOf(d metadata.Definition) (string, int) {
	n, kind := 0, ""
	if d.Entity != nil {
		n, kind = n+1, "entity"
	}
	if d.Field != nil {
		n, kind = n+1, "field"
	}
	if d.Page != nil {
		n, kind = n+1, "page"
	}
	if d.Relationship != nil {
		n, kind = n+1, "relationship"
	}
	if d.Rule != nil {
		n, kind = n+1, "rule"
	}
	if d.Workflow != nil {
		n, kind = n+1, "workflow"
	}
	if d.Tool != nil {
		n, kind = n+1, "tool"
	}
	return kind, n
}

func FuzzReviewPatchProposal(f *testing.F) {
	doc := fixtureDoc(f, exVendor) // schema 1.0.0 draft that includes a tool definition
	empty := `{"version":"ai-patch-proposal-v1","operations":[],"assumptions":[],"unresolved_requirements":[],"proposed_tests":[],"risk_flags":[]}`
	f.Add([]byte(empty))
	for _, d := range doc.Definitions {
		d := d
		raw, err := json.Marshal(PatchProposal{Version: PatchProposalVersion, Assumptions: []string{}, UnresolvedRequirements: nil, RiskFlags: nil,
			Operations: []PatchOperation{{Op: "update", Target: d.ID, ExpectedHash: metadata.DefinitionHash(d), Definition: &d, Rationale: "r"}}})
		if err == nil {
			f.Add(raw)
		}
		del, err := json.Marshal(PatchProposal{Version: PatchProposalVersion, Operations: []PatchOperation{{Op: "delete", Target: d.ID, ExpectedHash: metadata.DefinitionHash(d), Rationale: "r"}}})
		if err == nil {
			f.Add(del)
		}
	}
	before := map[string][]byte{}
	for _, d := range doc.Definitions {
		raw, _ := json.Marshal(d)
		before[d.ID] = raw
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := ParsePatchProposal(raw)
		if err != nil {
			return
		}
		canon, _, err := CanonicalPatch(p)
		if err != nil {
			t.Fatalf("an accepted proposal has no canonical form: %v", err)
		}
		again, err := ParsePatchProposal(canon)
		if err != nil {
			t.Fatalf("canonical form of an accepted proposal does not re-parse: %v", err)
		}
		canon2, _, err := CanonicalPatch(again)
		if err != nil || !bytes.Equal(canon, canon2) {
			t.Fatal("canonical form is not a fixed point")
		}
		for _, op := range p.Operations {
			if !kindAllowed(targetKind(op.Target)) {
				// The parser may accept it (the allowlist is enforced at apply), but apply must refuse.
				if _, conflicts, aerr := ApplyProposal(doc, p); aerr == nil && len(conflicts) == 0 {
					t.Fatalf("a proposal naming target %q (kind outside the allowlist) applied cleanly", op.Target)
				}
			}
		}
		patched, conflicts, err := ApplyProposal(doc, p)
		if err != nil || len(conflicts) > 0 {
			return
		}
		if patched.SchemaVersion != doc.SchemaVersion || patched.AppID != doc.AppID || patched.Namespace != doc.Namespace {
			t.Fatal("the document envelope changed")
		}
		seen := map[string]bool{}
		for _, d := range patched.Definitions {
			kind, n := payloadOf(d)
			if n != 1 || kind != d.Kind || d.Kind != targetKind(d.ID) {
				t.Fatalf("definition %q: kind %q, payload %q x%d", d.ID, d.Kind, kind, n)
			}
			seen[d.ID] = true
			if !kindAllowed(d.Kind) {
				if old, ok := before[d.ID]; !ok {
					t.Fatalf("a definition of kind %q appeared", d.Kind)
				} else if now, _ := json.Marshal(d); !bytes.Equal(old, now) {
					t.Fatalf("a definition of kind %q (%s) changed", d.Kind, d.ID)
				}
			}
		}
		for id, old := range before {
			if !seen[id] && !kindAllowed(strings.SplitN(id, ".", 2)[0]) {
				t.Fatalf("a definition of a forbidden kind was deleted: %s (%d bytes)", id, len(old))
			}
		}
	})
}
```

### A.6 H15 - requirement proposal parser and confirm-time invariants (fuzz)

File: `internal/ai/zz_review_reqfuzz_test.go`

```go
package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/intent"
)

// Review probe (not product code). Invariants for ANY byte string a model could send as a requirement proposal:
//   - parsing never panics;
//   - an accepted proposal has a canonical form, and parsing the canonical bytes reproduces it exactly, with the same hash;
//   - every draft names a category the verifier understands, ids are unique;
//   - Confirm's own construction (requiredness and basis from RequirementPolicy, provenance AI_PROPOSED_HUMAN_CONFIRMED, the status of
//     any decision a human can give) yields a requirement set that validates and strictly re-parses, so a READY proposal is never
//     unconfirmable for a reason the parser could have seen;
//   - the semantics of a draft are not changed again by the set's own canonicalisation.
func FuzzReviewRequirementProposal(f *testing.F) {
	body := func(requirements string) []byte {
		return []byte(fmt.Sprintf(`{"version":%q,"requirements":%s,"open_questions":[],"proposed_tests":[],"assumptions":[]}`, RequirementProposalVersion, requirements))
	}
	draft := func(id, category, semantics string) string {
		return fmt.Sprintf(`{"id":%q,"category":%q,"statement":"statement","semantics":%s}`, id, category, semantics)
	}
	f.Add(body(`[` + draft("r1", "approval_threshold", `{"workflow_id":"workflow.order.approve","entity_id":"entity.order","field_id":"field.order.amount","currency":"USD","op":"GT","amount":"500","approver_roles":["admin"]}`) + `]`))
	f.Add(body(`[` + draft("r2", "approval_gate", `{"workflow_id":"workflow.order.approve","approver_roles":["admin","owner"]}`) + `]`))
	f.Add(body(`[` + draft("r3", "tool_effect", `{"tool_ref":"tool.vendor.create","max_side_effect":"WRITE","requires_approval":true,"max_sandbox_mode":"BLOCK"}`) + `]`))
	f.Add(body(`[` + draft("r4", "data_migration", `{"max_class":"COMPATIBLE"}`) + `]`))
	f.Add(body(`[` + draft("r5", "approval_threshold", `{"workflow_id":"workflow.order.approve","entity_id":"entity.order","field_id":"field.order.amount","currency":"JPY","op":"GTE","amount":"1000","approver_roles":["admin"],"context":{"field.order.note":"regular"}}`) + `,` +
		draft("r6", "approval_gate", `{"workflow_id":"workflow.order.approve","approver_roles":["admin"]}`) + `]`))
	f.Add([]byte(fmt.Sprintf(`{"version":%q,"requirements":[],"open_questions":[{"id":"q1","question":"Which currencies?","category":"approval_threshold"}],"proposed_tests":[],"assumptions":["a"]}`, RequirementProposalVersion)))
	f.Add(body(`[` + draft("r7", "custom_category", `{"x":1}`) + `]`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		p, err := ParseRequirementProposal(raw)
		if err != nil {
			return
		}
		canon, hash, err := CanonicalRequirementProposal(p)
		if err != nil {
			t.Fatalf("an accepted proposal has no canonical form: %v", err)
		}
		again, err := ParseRequirementProposal(canon)
		if err != nil {
			t.Fatalf("the canonical form of an accepted proposal does not re-parse: %v", err)
		}
		canon2, hash2, err := CanonicalRequirementProposal(again)
		if err != nil || !bytes.Equal(canon, canon2) || hash != hash2 {
			t.Fatalf("canonical form is not a fixed point:\n%s\n%s", canon, canon2)
		}
		seen := map[string]bool{}
		for _, d := range p.Requirements {
			if seen[d.ID] {
				t.Fatalf("duplicate id %q accepted", d.ID)
			}
			seen[d.ID] = true
			if !(intent.RequirementV2{Category: d.Category}).Known() {
				t.Fatalf("a draft of the category %q the verifier does not understand was accepted", d.Category)
			}
		}
		for _, status := range []string{intent.StatusResolved, intent.StatusOpenQuestion, intent.StatusRejected} {
			set := intent.RequirementSet{SchemaVersion: intent.RequirementSetVersion, AppID: "app.orders", Requirements: []intent.RequirementV2{}}
			for _, d := range p.Requirements {
				requiredness, basis := RequirementPolicy(d.Category)
				set.Requirements = append(set.Requirements, intent.RequirementV2{ID: d.ID, Category: d.Category, Requiredness: requiredness, RequirednessBasis: basis,
					Status: status, Provenance: intent.ProvenanceAIConfirmed, Semantics: d.Semantics})
			}
			c, err := set.Canonical()
			if err != nil {
				t.Fatalf("an accepted proposal cannot be confirmed with every draft %s: %v\n%s", status, err, canon)
			}
			parsed, _, _, err := intent.ParseRequirementSet(c)
			if err != nil {
				t.Fatalf("the set Confirm would store does not re-parse strictly (%s): %v\n%s", status, err, c)
			}
			if len(parsed.Requirements) != len(p.Requirements) {
				t.Fatalf("requirement count changed: %d -> %d", len(p.Requirements), len(parsed.Requirements))
			}
			for i, r := range parsed.Requirements {
				if !bytes.Equal(r.Semantics, p.Requirements[i].Semantics) {
					t.Fatalf("semantics of %s changed between proposal and set:\n%s\n%s", r.ID, p.Requirements[i].Semantics, r.Semantics)
				}
			}
		}
		_ = json.RawMessage(nil)
	})
}
```

### A.7 H16 - the HTTP provider dial path (endpoint forms, resolver answers, host spellings)

File: `internal/ai/zz_review_dial_test.go`

```go
package ai

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// Review probe (not product code): the HTTP provider's endpoint forms and resolver answers beyond the builders' list. Every form is
// built through NewProvider, the only entry configuration reaches, and called once with a short deadline. A form that is accepted at
// construction and then fails with a transport error (not a policy refusal or a resolution failure) would mean that a connection to
// the named address was attempted.
func TestReviewProviderEndpointForms(t *testing.T) {
	forms := []string{
		"https://127.0.0.1/v1", "https://[::1]/v1", "https://0x7f.0.0.1/v1", "https://2130706433/v1", "https://0177.0.0.1/v1", "https://127.1/v1",
		"https://[::ffff:127.0.0.1]/v1", "https://[::ffff:7f00:1]/v1", "https://169.254.169.254/latest/meta-data", "https://[fd00:ec2::254]/v1",
		"https://10.0.0.1/v1", "https://192.168.0.1:8443/v1", "https://100.100.100.200/v1", "https://[fe80::1%25eth0]/v1", "https://0.0.0.0/v1",
		"https://localhost/v1", "https://LOCALHOST/v1", "https://localhost./v1", "https://metadata.google.internal/v1", "https://metadata.google.internal./v1",
		"https://printer.local/v1", "https://a.localhost/v1",
		"https://user@example.com/v1", "https://user:pw@example.com/v1", "http://example.com/v1", "ftp://example.com/v1", "https://example.com:0/v1",
		"https://example.com:70000/v1", "https://example.com:-1/v1", "https:///v1", "https://", "https://exa mple.com/v1", "https://example.com/v1\n",
		"https://example.com@127.0.0.1/v1", "https://127.0.0.1#@example.com/v1", "https://127.0.0.1\\@example.com/v1", "https://example.com/v1?x=1", "https://example.com/v1#f",
		"https://１２７.0.0.1/v1", "https://xn--127-0-0-1.example/v1",
		"https://API.Example.com/v1", "https://api.example.com./v1",
	}
	for _, endpoint := range forms {
		cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: endpoint, Model: "m"}
		p, err := NewProvider(cfg)
		if err != nil {
			t.Logf("%-52q refused at construction", endpoint)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		started := time.Now()
		_, err = p.Complete(ctx, sampleRequest())
		cancel()
		verdict := "other"
		switch {
		case err == nil:
			verdict = "SUCCEEDED"
		case errors.Is(err, ErrProviderRefused):
			verdict = "refused by the network policy"
		case errors.Is(err, ErrProviderUnavailable) && strings.Contains(err.Error(), "transport"):
			verdict = "transport failure (name did not resolve, or a connection was attempted)"
		case errors.Is(err, ErrProviderTimeout):
			verdict = "timeout (a connection may have been attempted)"
		}
		t.Logf("%-52q accepted at construction; call -> %s (%v, %v)", endpoint, verdict, err, time.Since(started).Round(time.Millisecond))
	}
}

// Resolver answers with scope identifiers and odd shapes for a public-looking name.
func TestReviewProviderResolverAnswerShapes(t *testing.T) {
	canary := newTestServer(t, reply(answer("{}", "m", Usage{})))
	port := strings.TrimPrefix(canary.URL, "https://127.0.0.1")
	for _, answers := range [][]string{
		{"fe80::1%eth0"}, {"::1%lo"}, {"2001:db8::1%eth0"}, {"::ffff:169.254.169.254"}, {"::ffff:a9fe:a9fe"}, {"198.18.0.1"}, {"192.0.0.1"}, {"203.0.113.9"},
		{"240.0.0.1"}, {"2001::1"}, {"2002:7f00:1::1"}, {"64:ff9b::7f00:1"}, {"64:ff9b:1::1"}, {"100::1"}, {"ff02::1"},
	} {
		resolver := &stubResolver{next: func(int) []netip.Addr { return addrs(answers...) }}
		cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: "https://api.example.com" + port + "/v1", Model: "m"}
		p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: canary.pool, resolver: resolver})
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.Complete(context.Background(), sampleRequest())
		t.Logf("%-28v -> %v", answers, err)
		if err == nil || !errors.Is(err, ErrProviderRefused) {
			t.Errorf("%v: expected a policy refusal, got %v", answers, err)
		}
		if canary.hits.Load() != 0 {
			t.Fatalf("%v: the canary server received a request", answers)
		}
	}
}

// Host spelling: the dial guard compares the address http.Transport hands over with the configured host:port. A host written with
// upper-case letters or a trailing dot is still a valid endpoint for the operator; it must either work or be refused at construction.
func TestReviewProviderHostSpelling(t *testing.T) {
	canary := newTestServer(t, reply(answer("{}", "m", Usage{})))
	port := strings.TrimPrefix(canary.URL, "https://127.0.0.1")
	for _, host := range []string{"api.example.com", "API.Example.com", "api.example.com.", "Api.Example.Com."} {
		resolver := &stubResolver{next: func(int) []netip.Addr { return addrs("127.0.0.1") }}
		cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: "https://" + host + port + "/v1", Model: "m"}
		p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: canary.pool, resolver: resolver, allowLoopback: true})
		if err != nil {
			t.Logf("%-22s refused at construction: %v", host, err)
			continue
		}
		before := canary.hits.Load()
		_, err = p.Complete(context.Background(), sampleRequest())
		t.Logf("%-22s call -> err=%v, server hits +%d, resolver calls %d", host, err, canary.hits.Load()-before, resolver.calls)
		if err != nil {
			t.Errorf("%s: a valid endpoint spelling cannot be called: %v", host, err)
		}
	}
}

// The endpoint spellings that pass the construction filter (non-canonical numbers, trailing dots) when a resolver answers them with
// loopback, link-local or metadata addresses, as the system resolver would for "2130706433" or "localhost.".
func TestReviewProviderFilterBypassSpellingsAreRefusedAtDialTime(t *testing.T) {
	canary := newTestServer(t, reply(answer("{}", "m", Usage{})))
	port := strings.TrimPrefix(canary.URL, "https://127.0.0.1")
	for _, host := range []string{"0x7f.0.0.1", "2130706433", "0177.0.0.1", "127.1", "localhost.", "metadata.google.internal.", "printer.local.", "a.localhost."} {
		for _, answer := range []string{"127.0.0.1", "::1", "169.254.169.254", "10.0.0.7"} {
			resolver := &stubResolver{next: func(int) []netip.Addr { return addrs(answer) }}
			cfg := ProviderConfig{Version: ProviderConfigVersion, Kind: KindHTTPSJSON, Endpoint: "https://" + host + port + "/v1", Model: "m"}
			p, err := newHTTPJSONProvider(cfg, httpOptions{rootCAs: canary.pool, resolver: resolver})
			if err != nil {
				t.Logf("%-28s refused at construction", host)
				break
			}
			_, err = p.Complete(context.Background(), sampleRequest())
			if err == nil || !errors.Is(err, ErrProviderRefused) {
				t.Errorf("%s answered %s: %v", host, answer, err)
			}
			if canary.hits.Load() != 0 {
				t.Fatalf("%s answered %s: the canary server received a request", host, answer)
			}
		}
	}
}
```
