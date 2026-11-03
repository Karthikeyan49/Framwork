# Documentation truth audit, final state

Independent audit of the written documentation of `integration/m9r-m14` at the pin `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`, last code change `62cc527`;
later commits are documents only). Branch `audit/docs-truth-final`. The auditor wrote or fixed none of the audited statements and changed no document, code or test: this file
is the only file written. Question asked of every statement: does the tree, the git history, a committed log or a command run here prove it? Classes: TRUE, FALSE, STALE (true
when written, not now), OVERCLAIM (stronger than its evidence), UNVERIFIABLE (no durable evidence and not reproducible here), NOT RUN. Results: PASS / FAIL / NOT RUN.
No database stack was used: a claim that needs one and has no committed log is UNVERIFIABLE or NOT RUN. Severity: MEDIUM = a reader would decide wrongly about security,
integrity, readiness, authority or what was verified; LOW = wording, number, date or pointer.

## Progress

| Step | Item | State |
|---|---|---|
| 0 | Briefs, `AGENTS.md`, `CLAUDE.md`, previous audit `DOCS_TRUTH_AUDIT.md` read | DONE |
| 1 | `LINE_INTEGRATION_PLAN.md`, `M9R_FINDING_STATUS.md`, `AGENT_HANDOFFS.md` (entries of 28 and 29 October) | DONE (DT-01, DT-04 to DT-20, DT-24 to DT-26) |
| 2 | ADR102 amendment 13, ADR103 amendments 12-16, ADR104 B.4 / Amendment C / residuals, `README.md`, `internal/verification/README.md`, `internal/ai/README.md`, M12 appendix, `scripts/runtime-env.mjs` | DONE (DT-02, DT-17, DT-21) |
| 3 | Other documents changed since `be6ec7c`; provenance of the imported reviewer reports | DONE (DT-03, DT-06, DT-10, DT-22, DT-23; provenance section) |
| 4 | `AGENTS.md`, `CLAUDE.md` (report only) | DONE (section below) |
| 5 | Mechanical checks (hashes, paths, links, anchors, make targets, test names, identifiers, env names, versions, migrations) | DONE |
| 6 | `hv nice -n 10 make docs-verify` | PASS: 84 architecture + 43 V3 source checksums (the target checks the two source kits only: no link, none of the audited files) |

## Result in numbers

Mechanical checks: 29 distinct commit hashes in the plan, 68 in the ledger, 15 in the handoffs, 0 missing, 0 not ancestors of the pin (the two other 7-hex tokens in the proposal
are blob ids, reproduced by applying its patch to scratch copies); 0 broken relative links or anchors in 19 scope files; 103 test, fuzz and benchmark names in backticks, 95 exist and
8 are `-run` prefixes (`TestM8` ... `TestM14`, `TestM13R03`); 0 make targets named in a document are missing from the Makefile (36 targets); 70 migration files (`00001`-`00057`,
`00080`-`00092`), next free number `00093`. Counts of classified statements per document and class: "Result in numbers (computed)" at the end.

## Findings

Quote the sentence, replace it with the text given. `:n` is the line at the pin.

### MEDIUM

**DT-01** `docs/migration/LINE_INTEGRATION_PLAN.md:23` and `:53`. "The last five rows of the table below are what was done after `0e13abe`; **none of it is re-verified by a reviewer.**" /
"Everything in the last five rows of the table is the builder's own work and has no reviewer's verdict".
Evidence: the work after `0e13abe` is in rows 38-43 (M14R-08, M14R-05, IR-07, IR-08, `migrate status`, M14R-03 residual): six rows, not five, and five older rows follow them (F-03, F-04, D2,
IR-04, combination test, lines 44-48). "The last five rows" are therefore rows that were reviewed (plan:61: the third pass attacked F-03 and F-04), and the rows without a verdict carry no marker in
the plan (only the ledger rows say "Not re-verified by a reviewer"). The F-05 row (:36) also gained a post-review sentence (`db9c2fa`).
Replace :23 with "The rows M14R-08, M14R-05, IR-07, IR-08, `migrate status` and the M14R-03 residual (the expiring token) of the table below are what was done after `0e13abe`, and the F-05 row gained the executed residual of 28 October; **none of it is re-verified by a reviewer.**"
Replace :53 with "Everything in those six rows and in the executed F-05 residual is the builder's own work and has no reviewer's verdict;".

**DT-02** `README.md:14`, `:650-651`, `:658`, `:763` (the previous audit's DT-24, not corrected: the only change to README since `be6ec7c` is the operator-credential paragraph at :241-245).
":14 Follow the remediation plan before M10." ":650-651 Workflow foundations now exist. Production promotion, external tools, AI and full scale/security certification remain later work. M10 stays paused."
":658 There are no public production activation endpoints or AI execution capabilities." ":763 Only `local.noop` is registered; external tools are M10."
Evidence: `internal/platform/app/app.go:62-78` loads the operator tool catalog when `TOOL_CATALOG_FILE` is set; `app.go:270` sets `PreviewEnabled` in development/test, which serves the M13 release routes
(`access/releases.go:182`) and the M14 AI routes (`access/ai.go:270`); `config.go:121-127` refuses `AI_PROVIDER_FILE` in production; migrations `00080`-`00092`; ADR100-104. README:659-660 itself names
"M10 tools, M11 sandbox, M12 candidate builder and M13 approval/promotion services".
Replace :14 with "M10-M14 exist as a development/test preview (see the [finding ledger](docs/migration/M9R_FINDING_STATUS.md)); follow the remediation plan for the remaining foundation work."
Replace :650-651 with "Workflow foundations exist. M10-M14 (operator-catalog tools, sandboxes, candidate builder, approval/promotion control plane, AI proposals) are development/test preview; the production data plane, production activation, a real model provider and full scale/security certification remain later work."
Replace :658 with "Release approval/activation routes (M13) and AI proposal routes (M14) exist only in development/test (`PreviewEnabled`; `AI_PROVIDER_FILE` is refused in production); there is no production activation endpoint and no production AI execution."
Replace :763 with "`local.noop` and, when `TOOL_CATALOG_FILE` is set, the operator-catalog tools of ADR100 are registered."

**DT-03** `docs/migration/V2_V3_SUPERSESSION_MAP.md:42`. "... M12 deterministic acceptance verifier (`verifier-v2`) ... an independent review exists as a report on branch `review/integration-final`, verdict FAIL with four MEDIUM and four LOW findings that the [finding ledger](M9R_FINDING_STATUS.md) tracks".
Evidence: `internal/verification/report.go:27` is `verifier-v3`; the M14 review is in the tree (`docs/verification/M14_INDEPENDENT_REVIEW.md`): FAIL at `552fa9e` and `be6ec7c`, PASS at `0e13abe` ("no HIGH and no MEDIUM open", header line 18). The row was last edited before the third pass and contradicts plan:20-23 and ledger:36-37.
Replace `verifier-v2` with `verifier-v3` and the review clause with "an independent review is in the tree ([M14_INDEPENDENT_REVIEW.md](../verification/M14_INDEPENDENT_REVIEW.md)): FAIL at `552fa9e` (four MEDIUM, four LOW), PASS at `0e13abe` (no HIGH, no MEDIUM open); the fixes made after `0e13abe` have no reviewer's verdict and the [finding ledger](M9R_FINDING_STATUS.md) tracks the rest".

### LOW: verification-status words and numbers in the central documents

**DT-04** `LINE_INTEGRATION_PLAN.md:95` and `:113` "29 Go packages" is FALSE for both gates; `:112` calls `585499d` "the code head".
Evidence: `gate-294e597-unit.log` and `gate-585499d-unit.log` each have 30 `ok` lines, `go list` finds 30 packages with tests at the pin; `internal/platform/app` got its first test file in `bca9d98` (IR-07), so 29 is right only for `507c12c` (:70). `585499d` changes documents only; the tree of the last code change is `62cc527`.
Replace "29 Go packages" with "30 Go packages" at :95 and :113, and ":112 Gate on the code head `585499d`" with "Gate on `585499d` (code identical to `62cc527`, the last code change)".

**DT-05** `LINE_INTEGRATION_PLAN.md:102-103` "The tree that is pushed differs from it by documents and by four benchmarks of the body budget in `bodybudget_test.go`" is STALE.
Evidence: `git diff --stat 294e597 11a4ee4 -- ':!*.md'` lists `identity.go`, `identity_test.go`, `access/ai.go`, `bodybudget_test.go`, `m13r_approve_fence_test.go`, `m14r_ai_route_test.go` (`62cc527`, `db9c2fa`, `80a582f`).
Replace "The tree that is pushed differs from it" with "The tree that was pushed when this gate was recorded (`80a582f`) differed from it" and append "The code changes after it (`db9c2fa`, `62cc527`) are covered by the gate on `585499d` below."

**DT-06** `LINE_INTEGRATION_PLAN.md:177` "have run since (both verdict FAIL, see the follow-up above)", `M9R_FINDING_STATUS.md:22-24` "both have run since and both ended FAIL", `M14_IMPLEMENTATION_REPORT.md:11-14` ("verdict FAIL" of the first review only).
Evidence: FAIL is the verdict at `552fa9e`; the same reports end with PASS at `0e13abe` (`INTEGRATION_INDEPENDENT_REVIEW.md:15`, `M14_INDEPENDENT_REVIEW.md:18`), as plan:20-23 and ledger:36-37 say.
Replace with "(verdict FAIL at `552fa9e`, PASS at `0e13abe`, see the follow-up above)" / "both have run since: FAIL at `552fa9e`, PASS at `0e13abe`, see the milestone-agent bullet below" / append to the M14 banner "A third pass at `0e13abe` gave PASS (no HIGH, no MEDIUM open); the fixes after it have no reviewer's verdict."

**DT-07** `M9R_FINDING_STATUS.md:253` ends "`migrate status` still lists a below-maximum file as plain pending (LOW)": STALE, contradicts :250 and `294e597` (status marks it "refused by up" and exits 1; `TestStatusMarksThePendingMigrationsThatUpRefuses` passes).
Replace with "`migrate status` listed a below-maximum file as plain pending when this was rehearsed; `294e597` marks it and exits 1 (row IR-07, IR-08 above)."

**DT-08** `M9R_FINDING_STATUS.md:244` last column "The review is final for `552fa9e`; it does not judge the fixes." and `:252` "the reviewer did not re-run them (see the plan)" are STALE.
Evidence: the same report contains the re-verification at `be6ec7c` and the third pass at `0e13abe` (IR-01 to IR-06 verified FIXED); the third pass attacked F-03 and F-04 and found IR-08 and IR-07 (plan:61).
Replace :244 with "The review judged `552fa9e`; its sections at `be6ec7c` and `0e13abe` (same report) verify IR-01 to IR-06 fixed; the second review re-verified IR-02/F-01/F-02 and D1/D2". Replace the tail of :252 with "F-03 and F-04 were fixed after the round that judged them; the third pass attacked both and found IR-08 and IR-07, fixed after it and not re-verified".

**DT-09** `M9R_FINDING_STATUS.md:213` "independent re-review of the fix is pending", `:240` "independent re-review pending", `:216` "Latest fixes and M14/merge independent review remain unverified".
Evidence: B-02 was re-run by the second review (A3: 288 relabelled artifacts, 1 restores) and the merge review (A.7); B-05 = IR-01 is verified FIXED at `be6ec7c` and `0e13abe`; the M14/merge reviews exist (PASS at `0e13abe`). What is pending is the remediation lane's re-verification.
Replace the first two with "re-verified by the independent reviews (second review A3, merge review A.7 / IR-01); the remediation agent's re-verification is pending" and the third with "Fixes after `0e13abe` have no reviewer's verdict; the M14/merge reviews passed at `0e13abe`".

**DT-10** `verifier-v2` described as current: `M9R_FINDING_STATUS.md:216` ("executes acceptance-v2/verifier-v2"), `M9R_REMEDIATION_PLAN.md:484` ("`verifier-v2` over pinned plans"), `docs/adr/103-m13-approval-and-promotion.md:346` ("the verifier version is part of the components (`verifier-v2`)").
Evidence: `VerifierVersion = "verifier-v3"` (`report.go:27`); ADR102:405 and `M12_DESIGN_APPENDIX.md:148` already say so. Replace `verifier-v2` with `verifier-v3` in the first two; append to ADR103:346 "(now `verifier-v3`, ADR102 amendment 13)".

**DT-11** "harmless" for the F-05 residual: `LINE_INTEGRATION_PLAN.md:50` ("executed on 28 October: harmless"), `M9R_FINDING_STATUS.md:40`, `AGENT_HANDOFFS.md:323` ("recorded and harmless"): OVERCLAIM.
Evidence: `TestF05ApprovalOvertakenByAFirstActivationCannotTakeEffect` asserts that the other candidate's activation is refused 55000, the pointer stays at generation 1 and the first release has one approval; the recorded approval is only logged (`m13r_approve_fence_test.go:182-205`). ADR103:370 already corrected the earlier "harmless" of amendment 12 because a recorded approval is shown and counted.
Replace "harmless" with "cannot move the pointer (the approval is recorded, the activation of that candidate is refused as stale)".

**DT-12** The commits after which the reference gates were not run again omit `62cc527` (it changed `access/ai.go`, the AI creation route): `LINE_INTEGRATION_PLAN.md:51-52` and `M9R_FINDING_STATUS.md:43-44` list "`f997efe`, `c76968f`, `b427e8b`, `a2c5c52`, `8528fde`". Append ", `62cc527`" in both.

**DT-13** `AGENT_HANDOFFS.md:219-220`, `:278`, `:305` ("this test will tell you which SQL bodies still name the older versions", "If you add authoring 1.6.0, both tell you ...", "it names the objects that still name only older versions"): OVERCLAIM (the previous audit's DT-13, corrected in the plan at :27, not in the handoffs).
Evidence: `internal/metadata/version_family.go:26` and `:31` hard-code `knownSchemaVersions()` and `schemaFamilies()` (`WorkflowSchema`, `IntegritySchema`, `models.go:23`); a 1.6.0 constant is invisible to the scan until both are extended.
Replace with "If you add authoring 1.6.0, add it to `knownSchemaVersions()` and to the family functions in `internal/metadata`; the scan then names the objects that still name only older versions (a guard, not a proof)."

**DT-14** `AGENT_HANDOFFS.md:293` "(code head `294e597`; later commits are documents)" is STALE inside a dated entry (`62cc527`, `db9c2fa` changed code and tests): append "; see the entry of 29 October for the later code change". The older "next free number is `00091`" (:218, :236) beside `00093` (:281, :310, :325): append "(superseded: `00093` since 22 October)".

**DT-15** Headings: `LINE_INTEGRATION_PLAN.md:3` "20 to 22 October 2026" and `M9R_FINDING_STATUS.md:29` "(22 October ...)" cover text and gates of 28 and 29 October. Replace with "20 to 29 October 2026" and "(22 to 29 October ...)".

**DT-16** `LINE_INTEGRATION_PLAN.md:32` "`TestM14PreReadBufferingIsBounded` (real server): 60 connections of one token sending 400 KiB each: 7 held, 53 refused 429" stands beside "(2 MiB per identity, 32 MiB in all)", but the test uses `access.NewBodyBudget(8<<20, 3<<20)` (`m14_api_bodybudget_test.go:154`; the default share of 2 MiB holds 5). Insert "against a test budget of 3 MiB per identity and 8 MiB in all" after "400 KiB each".

**DT-17** "near-miss lists for each rule" (`LINE_INTEGRATION_PLAN.md:38`) and "near-miss lists for every rule" (`docs/adr/104-m14-ai-builder-and-intent-capture.md:673`): OVERCLAIM. `TestRedactionLeavesTextNearTheNewRulesAlone` has no case for the `hf_`, `api_org_`, `whsec_`, `xapp-` prefixes or the `AccountKey` / `SharedAccessKey` names.
Replace with "near-miss lists for the Basic, prose-password, cookie, `sig=`, card-number and variable-name rules and the wrapped-key rule".

**DT-18** `LINE_INTEGRATION_PLAN.md:50` "the expiring-token part is closed by the row below": the row is above (:43). Replace "below" with "above".

**DT-19** `LINE_INTEGRATION_PLAN.md:108-110` (benchmarks): "two allocations of 80 bytes per request": `-benchmem` shows 80 B/op in 2 allocs/op (80 bytes in all). The time figures (59 ns, 188 ns, 1.35 us, 0.95 us, "idle host") are UNVERIFIABLE here: on the loaded host (load 4.5) three runs gave 84-92 ns, 131-197 ns, 2.0-2.4 us and 1.10-1.16 us. Replace with "two allocations, 80 bytes in all".

**DT-20** Owner decision on `MAIN`. `LINE_INTEGRATION_PLAN.md:297` "The development database `MAIN` has to be rebuilt, not migrated." is unqualified, while :185-186 says it "is preserved" and that no rebuild is authorized. Append "; whether and when to rebuild it is the owner's decision and is not taken".

### LOW: other documents

**DT-21** `internal/ai/README.md` file table lists `redact.go` but not `redact_fold.go` (added by `9196f26`). Replace the row "`context.go`, `redact.go`" with "`context.go`, `redact.go`, `redact_fold.go`" and append "; look-alike folding".

**DT-22** `docs/migration/M9R_REMEDIATION_PLAN.md:43-52` "A separate reviewer's interim report on the merge is in this tree (...); that reviewer's final reports, the M14 review and a re-verification of the integration fixes exist as reports on the branches `review/integration-final` and `review/m14-and-fixes`": STALE, all are in the tree and the merge report ends with PASS at `0e13abe`. Replace with "A separate reviewer's reports are in this tree: the merge review ([INTEGRATION_INDEPENDENT_REVIEW.md](../verification/INTEGRATION_INDEPENDENT_REVIEW.md), pinned `552fa9e`, re-run at `be6ec7c` and `0e13abe`: PASS at `0e13abe`), the M14 review and the re-verification of the integration fixes."

**DT-23** `docs/migration/M9R_AGENT_CONTRACT_UPDATE_PROPOSAL.md:157-162` (inside the "exact" patch): "Current user scheduling override: no independent agents until 22 October 2026, 21:00 UTC; do not spawn or wait meanwhile. Record independent review NOT RUN" would be written into `AGENTS.md`; it expired on 22 October and three review passes exist. The patch otherwise holds: baseline hashes still match, `git apply --check` exits 0, resulting blob ids `6866689` / `1ed343d` reproduce. Add to the note at the top "the scheduling override in the patch (lines 157-162 of this file) has expired and must be dropped or rewritten before an apply".

**DT-24** `M9R_FINDING_STATUS.md:34-35` "... are fixed on `integration/m9r-m14` with regression tests that fail without the change" is stronger than its rows (the previous audit's DT-12, reworded, not removed): B-04 is "wording only", IR-04 is `make check`. Replace "with regression tests that fail without the change (rows below)" with "with the tests, negative controls and exceptions named in the rows below".

**DT-25** `M9R_FINDING_STATUS.md:16-18` "B-01/B-02/B-05 confirmed OPEN, B-03 ... remain" (bullet describing report B of 20 October) reads as current status (previous audit FS-09, not corrected). Append "(at slice B; B-01, B-02, B-04, B-05 are fixed since, see the milestone-agent bullet below)".

**DT-26** `LINE_INTEGRATION_PLAN.md:119-120` "which tested every sentence of the first proposal against a real goose, a populated 57 and 89 and the real `migrate` binary" and `M9R_FINDING_STATUS.md:253` "every sentence of the proposed numbering rule was run against a real goose and the real `migrate` binary": OVERCLAIM.
Evidence: the rehearsal's own verdict table marks its item 4 (ADR reservations, documents have no order constraint) NOT RUN, "no ADR tooling was exercised" (`UPGRADE_AND_ORDERING_REHEARSAL.md:306`, progress table :23), and for the 58-79 sentence "a database still at 57 was not tested" (:301).
Replace "every sentence of the first proposal" with "the numbering sentences of the first proposal (not its ADR reservations)" and "every sentence of the proposed numbering rule" with "every numbering sentence of the proposed rule (its ADR reservations were not exercised)".

## Cross-document pairs (same fact, two statements)

| Fact | Statement A | Statement B | Finding |
|---|---|---|---|
| Verdict of the merge / M14 review | plan:20-23, ledger:36-37: PASS at `0e13abe` | plan:177, ledger:22-24, supersession map:42, M14 banner:11: FAIL | DT-06, DT-03 |
| Third-pass status of F-03 / F-04 | plan:61: attacked, found incomplete | ledger:252: the reviewer did not re-run them | DT-08 |
| `migrate status` | ledger:250: marks the file, exits 1 | ledger:253: still plain pending | DT-07 |
| Does the merge review judge the fixes | plan:20-23: IR-01 to IR-06 verified FIXED | ledger:244: "it does not judge the fixes" | DT-08 |
| Unique / restore fixes re-verified | reports: second review A3, merge review A.7, IR-01 | ledger:213, :240: "independent re-review pending" | DT-09 |
| Verifier version | `report.go:27`, ADR102:405, appendix:148: `verifier-v3` | ledger:216, remediation plan:484, ADR103:346, supersession map:42: `verifier-v2` | DT-10, DT-03 |
| F-05 residual | ADR103:370 retracts "harmless"; ADR103:380 "inert at the pointer" | plan:50, ledger:40, handoffs:323: "harmless" | DT-11 |
| Reliability of the version-gate scan | plan:27, :41: "a guard, not a proof" | handoffs:219-220, :278, :305: "will tell you" | DT-13 |
| Next free migration number | handoffs:281, :310, :325: `00093` (tree: 70 files, highest `00092`) | handoffs:218, :236: `00091` | DT-14 |
| `MAIN` database | plan:185-186: preserved, no rebuild authorized | plan:297: "has to be rebuilt" | DT-20 |
| Go packages with tests | logs, `go list`: 30 | plan:95, :113: 29 (plan:70: 29, true for `507c12c`) | DT-04 |
| Rows done after `0e13abe` | plan:38-43: six rows | plan:23, :53: "the last five rows" | DT-01 |
| M10-M14 | README:659-660: M10 tools ... M13 services | README:14, :651, :658, :763: paused / only `local.noop` | DT-02 |
| Rehearsal coverage | rehearsal:23, :306: rule 4 NOT RUN | plan:119-120, ledger:253: "every sentence" | DT-26 |

## Authority and open decisions

The three owner decisions are not taken and no document says so: numbering rule "The owner has not decided" (plan:120, ledger:234, handoffs:235-238, :313); contract proposal "PROPOSED ONLY - NOT APPLIED" (proposal:3), `AGENTS.md` / `CLAUDE.md` hashes equal the proposal baseline (`d449d4d6...`, `a947da39...`) and are unchanged since `b00da36`; rebuild of `MAIN`: DT-20. No document says that either file was changed, the proposal applied or the rule ratified (grep of all `.md`). `db/migrations/numbering_test.go` encodes the proposal and says the owner has not decided; it fails on 00058, a duplicate and a gap (executed with temporary files, removed).

## AGENTS.md and CLAUDE.md (report only, no edit proposed)

Stale against the tree: `AGENTS.md:3-6` and `CLAUDE.md:6-7,13` ("Do not begin M10 automatically" / "Do not continue M10 ...": M10-M14, ADR100-104 and migrations 00080-00092 exist); `AGENTS.md:50-125` ends at ADR063 (no ADR100-104); `:76-78` names schema/IR/compiler 1.4 only (`IntegritySchema` admits 1.4.0 and 1.5.0, `models.go:23`); `:115` "API/workflow stay RR" (the sandbox freeze route runs READ COMMITTED, `access.go:225-228,344-345`); `:122-123` "Only `local.noop` is registered" is FALSE when `TOOL_CATALOG_FILE` is set (`app.go:62-78`). Still true and now enforced: `:32-33` "Keep operator credentials out of runtime subprocesses" (`app.Main` refuses the six variables, IR-07; executed with the real `cmd/api` binary and synthetic values: exit 1, six names, no value). The make targets it names exist. No readiness, M15+, production data plane or real-provider claim in either file.

## Provenance of the imported reviewer reports

Each in-tree report is byte-identical to the last commit of the reviewer's branch (0 differing lines): `INTEGRATION_INDEPENDENT_REVIEW.md` and `M14_INDEPENDENT_REVIEW.md` = `origin/review/integration-final` `cb9e91a` (pins `552fa9e`, `be6ec7c`, `0e13abe` in the headers); `M14_AND_FIXES_INDEPENDENT_REVIEW.md` = `origin/review/m14-and-fixes` `1b68dae` (pin `be6ec7c`, round 3 at `507c12c`); `UPGRADE_AND_ORDERING_REHEARSAL.md` = `origin/audit/upgrade-rehearsal` `6f0805e` (`be6ec7c`, `43d417e`, `507c12c`); `DOCS_TRUTH_AUDIT.md` = `origin/audit/docs-truth` `e6beca0` (`be6ec7c`). The integrator's commits `079b8f3` and `fee7563` touched two of them but the result equals the branch tip: later reviewer text was imported, nothing was edited inside. The integrator's framing (plan:18-23, ledger:36-39, handoffs:295-297) matches the report headers (453 PASS / 5 SKIP / 0 FAIL, IR-01 to IR-06 and M14R-01, -02, -04, -06, -07 FIXED, M14R-03 PARTLY FIXED, PASS at `0e13abe`, IR-07 and IR-08 new) except DT-01, DT-06, DT-08.

## Executed here (commands and results)

`gt test -count=1` of the named tests (23 test functions in 28 packages, 0 FAIL); `TestRedaction*` with `REDACT_PROPERTY_RUNS=300000`: PASS 50 s; `FuzzRedactText` 30 s: 83,875 executions, PASS; the review shapes against the pre-fix redactor (overlay of `0e13abe` `redact.go`): 24 of 26 cases fail, the two that pass are the long-key storage string and `AWS_SECRET_ACCESS_KEY` (document: "nine survived, the tenth only with a short key": TRUE); IR-08 scan against the pre-fix `version_family.go`: exactly four new spellings fail (TRUE); IR-07: removing the call from `Main` (temporary, reverted) fails `TestMainRefusesOperatorCredentialsBeforeRunning` (TRUE; the guard is syntactic: `if false && ...` passes it); real `cmd/api` binary: refusal executed; verifier-v3 tests PASS (32 amounts probed, 33 blocked, the 8 listed extra probes of a 626-749 window); `TestMigrationNumbersFormOneIncreasingSequence` with 00058 / 00092 twice / 00094: FAIL, with 00093: PASS; `git apply --check` of the proposal patch on scratch copies: exit 0; `make docs-verify`: PASS; 461 integration tests listed at the pin (459 at `507c12c` + 2), 10 fuzz functions (`FuzzRedactText` has no make target and is not in the gate); `make -n` of the 18 targets the README and AGENTS.md name: all parse.

## NOT RUN / UNVERIFIABLE

Everything that needs PostgreSQL, Redis, Docker or a browser: the integration suite; the `507c12c` gate (no committed log); `TestM14R03ATokenThatEndsBefore...` and `TestF05ApprovalOvertakenByAFirstActivation...` (the `585499d` gate log shows only `ok tests/integration 2058.622s`, no -v); reference timings (8.55 s, 1.6 s, 50-75 ms, 18.6 s); `migrate status` against a real database; the 209,433-execution fuzz run and the 25 and 8 mutation counts (one-off runs, no log); the "reported 28 October" delivery date of the third pass (the report is dated 22 October).

## Agreement with the previous audit

Hold (checked at the pin): its DT-01, 02, 03, 04, 05, 08, 09, 10, 14, 15, 16, 17 (`cmd/migrate/main.go:47`), 18 (ADR104 A1 "Struck by Amendment B.7"), 19, 20, 21, 22, 23 (ADR103:370), 25, 26 (`numbering_test.go:10` cites a file that now exists). Not corrected: DT-06 and DT-07 (`AGENTS.md`, `CLAUDE.md`, by instruction), DT-24 (README: DT-02 here), DT-13 in the handoffs (DT-13 here), DT-12 (DT-24 here), FS-09 (DT-25 here), DT-11 partly (DT-06 here).

## Result in numbers (computed)

170 checked statements (rows of the ledger; a row may stand for several numbers or hashes of one sentence).

| Area | Statements | TRUE | FALSE | STALE | OVERCLAIM | UNVERIFIABLE | NOT RUN |
|---|---:|---:|---:|---:|---:|---:|---:|
| LINE_INTEGRATION_PLAN.md | 80 | 53 | 3 | 5 | 6 | 12 | 1 |
| M9R_FINDING_STATUS.md | 26 | 14 | 0 | 9 | 3 | 0 | 0 |
| AGENT_HANDOFFS.md | 13 | 8 | 0 | 2 | 3 | 0 | 0 |
| ADR102/103/104 | 9 | 6 | 0 | 1 | 1 | 1 | 0 |
| README.md | 9 | 5 | 1 | 3 | 0 | 0 | 0 |
| package READMEs, M12 appendix, runtime-env.mjs | 5 | 4 | 0 | 1 | 0 | 0 | 0 |
| other changed documents | 9 | 4 | 0 | 5 | 0 | 0 | 0 |
| imported reviewer reports | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| AGENTS.md, CLAUDE.md | 8 | 3 | 1 | 4 | 0 | 0 | 0 |
| mechanical checks | 8 | 8 | 0 | 0 | 0 | 0 | 0 |
| **Total** | **170** | **108** | **5** | **30** | **13** | **13** | **1** |

Defects: 26 (DT-01 to DT-26): MEDIUM 3 (DT-01, DT-02, DT-03), LOW 23. No HIGH: every duration, PASS and count of the gates on `294e597` and `585499d` matches the committed logs except the package count (DT-04); the other wrong figure is the row count of DT-01.

## Ledger

| # | Where | Statement | Evidence | Class |
|---|---|---|---|---|
| 1 | plan:5-10 | reports pinned 552fa9e (IR, M14R: FAIL), be6ec7c (F-01..08), D1-D4, DT-01..26 | report headers; DT table | TRUE |
| 2 | plan:13-14 | second review re-verified F-01, F-02, IR-06, M14R-01/-02/-07, D1, D2; found F-05..F-08 | M14_AND_FIXES header, rounds 1-3 | TRUE |
| 3 | plan:15-16 | aggregate capacity D5 and driver panic D1 untouched | ledger:266-269; remediation plan D1/D5 | TRUE |
| 4 | plan:18-23 | third pass at 0e13abe: gate PASS, 453 PASS / 5 SKIP / 0 FAIL, IR-01..06 and M14R-01,-02,-04,-06,-07 FIXED, M14R-03 PARTLY, PASS | INTEGRATION report:15, M14 report:18 | TRUE |
| 5 | plan:18 | third pass (reported 28 October) | reports dated 22 October; delivery date not in the tree | UNVERIFIABLE |
| 6 | plan:22 | IR-07, IR-08 new LOW; M14R-05, M14R-08 left open | reports:359-360, M14:18 | TRUE |
| 7 | plan:23,:53 | the last five rows of the table are what was done after 0e13abe | rows 38-43 are six; five older rows follow (DT-01) | FALSE |
| 8 | plan:27 | B-05/IR-01: 00090, IN ('1.4.0','1.5.0'), scan test, lifecycle test | 00090 exists; TestFamilyGaps/TestSQLFunctions pass; test name exists | TRUE |
| 9 | plan:28 | B-02: restore.go pins the 1.5.0 pair; five forgeries and a control | A3 matrix 1 of 288 restores | TRUE |
| 10 | plan:29 | B-01: allowlists, real launcher, fake go, 13 script tests | gate log: 13 pass; test names exist | TRUE |
| 11 | plan:30 | B-04 wording only | dfec052 diff | TRUE |
| 12 | plan:31 | IR-05: guard; test calls the guard; real migrate on eight shapes | A5: seven shapes and a fresh database | TRUE |
| 13 | plan:32 | IR-02 steps f997efe, c76968f, b427e8b | git subjects | TRUE |
| 14 | plan:32 | 1.14 MiB per connection, +1,141 MiB at 1,000 connections | F-01 table: +1141.1 MiB | TRUE |
| 15 | plan:32 | 2 MiB per identity, 32 MiB in all | bodybudget.go:33-35 | TRUE |
| 16 | plan:32 | TestM14PreReadBufferingIsBounded: 7 held, 53 x 429, +4.0 MiB | test budget is 3 MiB / 8 MiB, not the 2 MiB default (DT-16); figures need a DB | OVERCLAIM |
| 17 | plan:32 | honest read 8.55 s without step 1, 201 in 1.6 s with it | real-server test; needs a DB | UNVERIFIABLE |
| 18 | plan:33 | IR-03: set-up retry, test server aborts; 3 of 3, 11 of 150, 0 of 600 | tests exist; counts from commits only | UNVERIFIABLE |
| 19 | plan:34 | D1: CompensationScanSQL, uncorrelated NOT IN, 9d2928d | execution.go; reviewer 1.22-1.35 s to 32-36 ms | TRUE |
| 20 | plan:34 | 96 receipts 1.5-1.7 s before, 50-75 ms after | needs a DB (reviewer's host differs) | UNVERIFIABLE |
| 21 | plan:35 | IR-06: 00091, bfa47c4, TestM13R01RollbackOvertakenByACommittedRejection | 00091 exists; 1-line diff to 0e13abe is a comment | TRUE |
| 22 | plan:36 | F-05: 00092, 37d9b14, FOR SHARE, trigger, backfill; round-3 re-verification | 00092; round 3 table (S3-S5 40001, 25 rounds) | TRUE |
| 23 | plan:36 | F-05 residual executed 28 October (recorded, refused as stale, pointer stays) | db9c2fa; test asserts refusal and pointer, logs the approval | TRUE |
| 24 | plan:37 | admission 5 x in 4 s, result exchange 6 x in 6 s, about 16 abandoned calls, cap 2x+1024 | ai.go:63-64,558-559; repair.go:77-78,152 | TRUE |
| 25 | plan:37 | 4 of 4 labels 503; 16 calls 0 tokens; 3 of 6 lost; 100,005,674; 29,780 vs 41,598; 18 of 18 | M14 and re-verification reports; 2*5674+1024 + 2*8192+1024 = 29,780 | TRUE |
| 26 | plan:38 | M14R-08 ten surviving classes | M14 report:335 | TRUE |
| 27 | plan:38 | 14 cases for the review's classes, 12 own probes | counted in redact_shapes_test.go | TRUE |
| 28 | plan:38 | nine survived the unchanged table, the tenth only with a short key | pre-fix redactor via overlay: 24 of 26 fail, long-key string and AWS key pass | TRUE |
| 29 | plan:38 | 300,000 random strings idempotent | REDACT_PROPERTY_RUNS=300000: PASS 50 s | TRUE |
| 30 | plan:38 | FuzzRedactText 209,433 executions | one-off 90 s run; own 30 s run 83,875 executions PASS | UNVERIFIABLE |
| 31 | plan:38 | 25 mutations each failed by the test that names it | no list in the tree | UNVERIFIABLE |
| 32 | plan:38 | near-miss lists for each rule | no case for hf_, api_org_, whsec_, xapp-, AccountKey (DT-17) | OVERCLAIM |
| 33 | plan:39 | M14R-05 windows; verifier-v3; more than 32 amounts BLOCK; digest pinned | literals.go; 4 tests PASS; 32 probed, 33 blocked | TRUE |
| 34 | plan:39 | 8 mutations each failed | no list in the tree | UNVERIFIABLE |
| 35 | plan:40 | IR-07: six variables, API/worker/dispatcher exit 1, names never values | operator_env.go; real cmd/api run: exit 1, 6 names, no value | TRUE |
| 36 | plan:40 | 12 of 16 fringe spellings | INTEGRATION report A.16 | TRUE |
| 37 | plan:40 | syntax-tree test fails when the call is removed | removed in place, test FAIL, reverted | TRUE |
| 38 | plan:41 | IR-08: 10 of 21 missed; four spellings fail without the change | report A.17; pre-fix version_family.go: exactly four fail | TRUE |
| 39 | plan:42 | migrate status marks the file and exits 1 | main.go:62-97; TestStatusMarks... PASS | TRUE |
| 40 | plan:42 | checked on an isolated migrated database | needs a DB | UNVERIFIABLE |
| 41 | plan:43 | expiring token: VerifyRemaining, 401 token_expiring, 5/25/36 s vs 40 s, 70 s early | identity.go; ai.go:410-453; test source | TRUE |
| 42 | plan:43 | about 6.7% of requests with a 15-minute token | 60/900 | TRUE |
| 43 | plan:43 | TestM14R03ATokenThatEnds... passes; fails without the check | integration test; needs a DB (gate log: ok, no -v) | NOT RUN |
| 44 | plan:44 | F-03: 7 of 16 spellings missed, 43d417e | re-verification report:97-99 | TRUE |
| 45 | plan:45 | F-04: 18 spellings, 14 near-misses, 8 forms | 953be65; runtime-env.test.mjs | TRUE |
| 46 | plan:46 | D2: b05494a, numbering_test.go | tests PASS | TRUE |
| 47 | plan:47 | IR-04: make check vets integration,performance tags | Makefile; gate check log | TRUE |
| 48 | plan:48 | combination test PASS 18.6 s | test exists; time needs a DB | UNVERIFIABLE |
| 49 | plan:50 | closed by the row below | the row is above (:43) | FALSE |
| 50 | plan:50 | F-05 residual: harmless | ADR103:370 retracted 'harmless' (DT-11) | OVERCLAIM |
| 51 | plan:51-52 | request path changed after f997efe, c76968f, b427e8b, a2c5c52, 8528fde | 62cc527 changed access/ai.go too (DT-12) | STALE |
| 52 | plan:56-57 | third round found F-05..F-08 fixed, no new defect; bandwidth residual reproduced | re-verification round 3 | TRUE |
| 53 | plan:57-58 | 0e13abe differs from 507c12c by documents and one comment in 00091 | git diff --stat | TRUE |
| 54 | plan:59-61 | 168 rounds; 4,000 receipt sets; 38 times faster | INTEGRATION report:355,395,451 | TRUE |
| 55 | plan:64-75 | gate on 507c12c: 32 s, 221 s, 33 s, 1976.7 s (459/458), 36.3/8.9 s, 80 s, 609.5 s | no committed log; 461 listed now = 459 + 2 | UNVERIFIABLE |
| 56 | plan:77-79 | not run and not claimed (reference gates, driverpanic, ACL test, final review) | consistent with ledger | TRUE |
| 57 | plan:81-87 | 429 too_many_uploads / 503 service_busy, Retry-After, 400 invalid_request, 1 MiB | access.go:290-324 | TRUE |
| 58 | plan:89-100 | gate on 294e597: 32, 276, 16 s; 1838.8 s / 1858 s; 37.1/8.9 s; 128 s; 610 s; recovery counts | gate-294e597 logs | TRUE |
| 59 | plan:95,:113 | 29 Go packages | 30 ok lines in both unit logs; go list 30 (DT-04) | FALSE |
| 60 | plan:98 | e2e start bin/api through the launcher | playwright configs use with-env.mjs | TRUE |
| 61 | plan:102-103 | the tree that is pushed differs from 294e597 by documents and four benchmarks | 62cc527, db9c2fa also changed code/tests (DT-05) | STALE |
| 62 | plan:108-110 | benchmarks 59 ns, 188 ns, 1.35 us, 0.95 us, two allocations of 80 bytes | host loaded (84-92, 131-197 ns, 2.0-2.4, 1.10-1.16 us); 80 B in 2 allocs | UNVERIFIABLE |
| 63 | plan:112-115 | gate on 585499d: 34, 263, 34 s; 2058.6 s / 2067 s; 39.6/9.5 s; 88 s; 609 s; counts | gate-585499d logs | TRUE |
| 64 | plan:112 | code head 585499d | 585499d is documents only; last code change 62cc527 (DT-04) | OVERCLAIM |
| 65 | plan:115 | commits after 585499d are documents only | git diff 585499d 11a4ee4 = the plan | TRUE |
| 66 | plan:117-137 | numbering rule: 00001-00057 then 00080 up, no gaps, 58-79 unused; test fails on gap, duplicate, 58-79 | executed with temporary files: 00058, 00092 twice, 00094 FAIL; 00093 PASS | TRUE |
| 67 | plan:120 | the owner has not decided | no document says ratified | TRUE |
| 68 | plan:122-124 | migrate up answers 'migration refused: detected N missing (out-of-order)...' | main.go:99,109 | TRUE |
| 69 | plan:126-128 | MAIN holds version 60 and migrate refuses it | lineage guard exists; no DB here | UNVERIFIABLE |
| 70 | plan:133-136 | rules 3 and 4 (replace from latest definition; UpTo, no hard-coded Up count) | post-checks in 00090-00092; grep of tests | TRUE |
| 71 | plan:119-120 | rehearsal tested every sentence of the first proposal | rehearsal marks its rule 4 NOT RUN (DT-26) | OVERCLAIM |
| 72 | plan:139-153 | B-03 restriction and cutover rule | prior audit; restore.go | TRUE |
| 73 | plan:160 | main remains 9da3f5f | git rev-parse origin/main | TRUE |
| 74 | plan:177 | reviews have run since (both verdict FAIL) | PASS at 0e13abe (DT-06) | STALE |
| 75 | plan:180-186 | reservations 58-79/064-099, 80-99/100-119; no rebuild authorized; MAIN preserved | consistent | TRUE |
| 76 | plan:297-298 | MAIN has to be rebuilt, not migrated | unqualified beside :185-186 (DT-20) | OVERCLAIM |
| 77 | plan:202-203 | 116/264 and 192/253 commits/files | prior audit | TRUE |
| 78 | plan:209-218 | number map: 80-89, ADR100-104, 1.5.0, example file, m14-native-v11 | code constants; git show -M 8366090 | TRUE |
| 79 | plan:213 | 00090 and up stay free | flagged historical at :181 | STALE |
| 80 | plan:3 | heading '20 to 22 October 2026' | text and gates of 28-29 October (DT-15) | STALE |
| 81 | ledger:5-8 | M9R PARTIAL; combined FAIL; 552fa9e; a1d8b00 pushed; main 9da3f5f | refs checked | TRUE |
| 82 | ledger:16-18 | B-01/B-02/B-05 confirmed OPEN (report B) | fixed since (DT-25) | STALE |
| 83 | ledger:22-24 | both have run since and both ended FAIL | PASS at 0e13abe (DT-06) | STALE |
| 84 | ledger:29-44 | follow-up bullet: statuses, hashes, third pass PASS, 5 fixes not re-verified | plan, reports, git | TRUE |
| 85 | ledger:34-35 | fixed with regression tests that fail without the change | B-04, IR-04 have none (DT-24) | OVERCLAIM |
| 86 | ledger:43-44 | request path changed after five hashes | 62cc527 missing (DT-12) | STALE |
| 87 | ledger:40 | F-05 residual executed 28 October: harmless | DT-11 | OVERCLAIM |
| 88 | ledger:213 | independent re-review of the fix is pending | re-verified by reviewers (DT-09) | STALE |
| 89 | ledger:216 | executes acceptance-v2/verifier-v2; M14/merge review remain unverified | verifier-v3; reviews passed (DT-09, DT-10) | STALE |
| 90 | ledger:240 | B-02 independent re-review pending | A3 and A.7 (DT-09) | STALE |
| 91 | ledger:239 | B-01 fixed; 13 script tests; eight spellings | gate log | TRUE |
| 92 | ledger:243 | B-05 fixed 46d8a2a; remediation re-verification not done | 46d8a2a; no remediation report | TRUE |
| 93 | ledger:244 | IR rows: hashes, 8.55 s / 1.6 s; the review does not judge the fixes | same report judges them at be6ec7c, 0e13abe (DT-08) | STALE |
| 94 | ledger:245 | IR-06 = M14R-04 fixed bfa47c4, 00091 | git; 00091 | TRUE |
| 95 | ledger:246 | F-05 fixed 37d9b14, 00092; re-verified round 3 | report round 3 | TRUE |
| 96 | ledger:247 | M14R-01..07 rows, hashes, numbers | ai.go, repair.go, reports | TRUE |
| 97 | ledger:248 | M14R-08 fixed 9196f26: 14/12/300,000/209,433/25 | tests counted; run; fuzz and mutations U | TRUE |
| 98 | ledger:249 | M14R-05 fixed 3c27378, verifier-v3, stale v2 | report.go; bundle2.go:635; dependencies.go:134 | TRUE |
| 99 | ledger:250 | IR-07 bca9d98, IR-08 b88a465, migrate status 294e597 | executed | TRUE |
| 100 | ledger:251 | M14R-03 residual fixed 62cc527 | code; integration test not run here | TRUE |
| 101 | ledger:252 | F-03 and F-04: the reviewer did not re-run them | third pass attacked both (DT-08) | STALE |
| 102 | ledger:253 | migrate status still lists a below-maximum file as plain pending | 294e597 (DT-07) | STALE |
| 103 | ledger:253 | rehearsal: every sentence of the numbering rule run; 70 migrations | rule 4 NOT RUN (DT-26); 70 true | OVERCLAIM |
| 104 | ledger:254 | audit: 178 statements, 26 defects (0/9/17) | previous audit table | TRUE |
| 105 | ledger:234 | migrations 1-57 and 80 up to 00092 apply fresh; owner has not decided | 70 files; rehearsal | TRUE |
| 106 | ledger:266-269 | combined gate FAIL on the remediation line; B-01/02/05 fixed, await re-verification | consistent | TRUE |
| 107 | handoffs:203-242 | 20 October entry: hashes 46d8a2a, dfec052, f997efe, 6c4cee6, 1fb0110; gate on f6398bf | git subjects; dated entry | TRUE |
| 108 | handoffs:218,:236 | next free number is 00091 / sequence from 00091 | 00093 since 22 October (DT-14) | STALE |
| 109 | handoffs:219-220 | this test will tell you which SQL bodies still name the older versions | knownSchemaVersions hard-coded (DT-13) | OVERCLAIM |
| 110 | handoffs:263 | code head 507c12c; later commits documents and one comment in 00091 | git diff | TRUE |
| 111 | handoffs:272-282 | body budget 2 MiB / 32 MiB, 429/503; RetryWithin; CompensationScanSQL; 00091/00092; next free 00093 | code; migrations | TRUE |
| 112 | handoffs:278,:305 | If you add authoring 1.6.0 it names the objects | DT-13 | OVERCLAIM |
| 113 | handoffs:293 | code head 294e597; later commits are documents | 62cc527, db9c2fa (DT-14) | STALE |
| 114 | handoffs:295-297 | third pass: 453 PASS / 5 SKIP / 0 FAIL, IR/M14R statuses, PASS | report headers | TRUE |
| 115 | handoffs:301-303 | app.Main refuses six variables; every in-repository start is unaffected | dev.mjs, smoke.mjs, playwright, tests use curated env | TRUE |
| 116 | handoffs:306-307 | VerifierVersion is verifier-v3; v2 candidates read stale, bundle fails closed | report.go; bundle2.go:635 | TRUE |
| 117 | handoffs:310,:325 | next free number is 00093 | numbering test accepts 00093 | TRUE |
| 118 | handoffs:319-322 | VerifyRemaining; Verify unchanged; 401 token_expiring | identity.go diff | TRUE |
| 119 | handoffs:323 | F-05 residual recorded and harmless | DT-11 | OVERCLAIM |
| 120 | 102:389-413 | amendment 13: literals.go, ids lit-*, MaxLiteralAmounts 32, UNSUPPORTED_BY_VERIFIER, 4 tests, 8 probes | code and tests | TRUE |
| 121 | 102:405 | candidates under verifier-v2 read stale; bundles fail closed | dependencies.go:134; bundle2.go:635 | TRUE |
| 122 | 103:370-380 | amendment 16 and executed residual (55000, generation 1) | 00092; db9c2fa | TRUE |
| 123 | 103:346 | verifier version is part of the components (verifier-v2) | now verifier-v3 (DT-10) | STALE |
| 124 | 104:592 | B.4 pointer to Amendment C item 6 | exists | TRUE |
| 125 | 104:659-675 | item 6: 26 cases (14+12), 300,000, FuzzRedactText 209,433 / 90 s, 25 mutations, 1 MB in 0.2-1.7 s | counted/executed; runs U | UNVERIFIABLE |
| 126 | 104:673 | near-miss lists for every rule | DT-17 | OVERCLAIM |
| 127 | 104:677-690 | item 7: 401 token_expiring, 10 s margin, 70 s early, no UI | ai.go; web app has no AI call | TRUE |
| 128 | 104:692-702 | residuals after Amendment C incl. 6.7% closed by item 7 | consistent with plan | TRUE |
| 129 | README.md:241-245 | backends refuse six operator variables, exit 1 | executed with the real binary | TRUE |
| 130 | README.md:136-139 | Go 1.27.2, Node 24.19.0, ports 5432 6379 4318 13133 8080 5173 | go.mod, .nvmrc, compose.yaml, vite.config.ts | TRUE |
| 131 | README.md:180-189 | make check/test/build/release-check, docs-verify | Makefile | TRUE |
| 132 | README.md:193-205 | operational routes | grep in code | TRUE |
| 133 | README.md:14 | Follow the remediation plan before M10 | M10-M14 exist (DT-02) | STALE |
| 134 | README.md:650-651 | M10 stays paused; external tools, AI later work | DT-02 | STALE |
| 135 | README.md:658 | no public production activation endpoints or AI execution capabilities | dev/test routes exist (DT-02) | STALE |
| 136 | README.md:763 | Only local.noop is registered; external tools are M10 | app.go:62-78 (DT-02) | FALSE |
| 137 | README.md:659-660 | M10 tools, M11 sandbox, M12 builder, M13 services | ADRs | TRUE |
| 138 | verification/README:1-55 | verifier-v3; decades, multiples, ratios; 32 amounts BLOCKED; limits | oracle.go DeriveProbes; verifier.go | TRUE |
| 139 | ai/README | 412 corpus items | 6 JSON files total 412 | TRUE |
| 140 | ai/README | file table complete | redact_fold.go missing (DT-21) | STALE |
| 141 | M12 appendix:146-148 | verifier-v2 section superseded by verifier-v3 | ADR102 amendment 13 | TRUE |
| 142 | runtime-env.mjs:3-9 | list is a convenience; backends refuse operator credentials (operator_env.go) | file exists; executed | TRUE |
| 143 | remediation plan:43-52 | interim report in tree; final reports on branches | all in the tree (DT-22) | STALE |
| 144 | remediation plan:484 | verifier-v2 over pinned plans | DT-10 | STALE |
| 145 | remediation plan (diff) | B-01..B-05 fixed on integration; 9d2928d | git | TRUE |
| 146 | proposal:3-24 | PROPOSED ONLY, baseline SHA-256, blob ids 6866689/1ed343d | sha256sum; git apply --check; hash-object | TRUE |
| 147 | proposal:157-162 | scheduling override until 22 October 21:00 UTC | expired (DT-23) | STALE |
| 148 | supersession map:42 | verifier-v2; M14 review FAIL on branch | DT-03 | STALE |
| 149 | M14 report:11-14 | first independent review FAIL, fixes in Amendment C | accurate, incomplete (DT-06) | STALE |
| 150 | M13 report:84 | FIXED for reject and activate; rollback 00091, approve 00092 | ADR103 15, 16 | TRUE |
| 151 | M10/M11 report:20 | DSL 2.1.0 as first built; 2.2.0 now | plan.go:30 | TRUE |
| 152 | 5 reports | byte-identical to reviewer branch tips (0 differing lines) | git show origin/<branch>:file vs HEAD | TRUE |
| 153 | reports | each names the commit it was written against | headers: 552fa9e, be6ec7c, 0e13abe, 507c12c, 43d417e | TRUE |
| 154 | 079b8f3, fee7563 | integrator commits touching two reports | result equals branch tip: import, not edit | TRUE |
| 155 | AGENTS.md:3-6; CLAUDE.md:6-7,13 | do not begin/continue M10 | M10-M14 in the tree | STALE |
| 156 | AGENTS.md:50-125 | module contracts end at ADR063 | no ADR100-104 | STALE |
| 157 | AGENTS.md:76-78 | ADR044 names 1.4 only | IntegritySchema admits 1.5.0 | STALE |
| 158 | AGENTS.md:115 | API/workflow stay RR | freeze route RC (access.go:225-228,344) | STALE |
| 159 | AGENTS.md:122-123 | Only local.noop is registered | app.go:62-78 | FALSE |
| 160 | AGENTS.md:32-33 | keep operator credentials out of runtime subprocesses | enforced by app.Main | TRUE |
| 161 | AGENTS.md:129-132 | make setup/infra-up/migrate/check/test/build/browser-install/test-e2e | Makefile | TRUE |
| 162 | AGENTS.md, CLAUDE.md | unchanged since b00da36; no document claims they changed | git log; grep | TRUE |
| 163 | all scope files | commit hashes exist and are ancestors of the pin | git cat-file / merge-base: 156 tokens in 12 files, 2 are blob ids | TRUE |
| 164 | 19 scope files | relative links and anchors resolve | links.py: 0 problems | TRUE |
| 165 | scope files | backticked repository paths exist | 4 hits: old name in a rename table, a branch name, a range, build output | TRUE |
| 166 | scope files | make targets named exist | 36 targets; 34 prose hits | TRUE |
| 167 | scope files | test and fuzz names exist | 95 of 103; 8 are -run prefixes | TRUE |
| 168 | scope files | code identifiers and env names exist | 9 identifiers, 2 env names outside source: fixtures/vocabulary | TRUE |
| 169 | versions | 1.3.0 / 1.4.0 / 1.5.0; DSL 2.0.0 / 2.1.0 / 2.2.0; verifier-v3; m14-native-v11; ai-provider-v1 | models.go, compiler.go, plan.go, report.go, policy.go, contract.go | TRUE |
| 170 | make docs-verify | 84 + 43 checksums | hv nice -n 10 make docs-verify: PASS | TRUE |
