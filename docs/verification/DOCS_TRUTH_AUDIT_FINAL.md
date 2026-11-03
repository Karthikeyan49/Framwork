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
| 1 | `LINE_INTEGRATION_PLAN.md`, `M9R_FINDING_STATUS.md`, `AGENT_HANDOFFS.md` (entries of 28 and 29 October) | DONE (DT-01 to DT-20) |
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

## Authority and open decisions

The three owner decisions are not taken and no document says so: numbering rule "The owner has not decided" (plan:120, ledger:234, handoffs:235-238, :313); contract proposal "PROPOSED ONLY - NOT APPLIED" (proposal:3), `AGENTS.md` / `CLAUDE.md` hashes equal the proposal baseline (`d449d4d6...`, `a947da39...`) and are unchanged since `b00da36`; rebuild of `MAIN`: DT-20. No document says that either file was changed, the proposal applied or the rule ratified (grep of all `.md`). `db/migrations/numbering_test.go` encodes the proposal and says the owner has not decided; it fails on 00058, a duplicate and a gap (executed with temporary files, removed).

## AGENTS.md and CLAUDE.md (report only, no edit proposed)

Stale against the tree: `AGENTS.md:3-6` and `CLAUDE.md:6-7,13` ("Do not begin M10 automatically" / "Do not continue M10 ...": M10-M14, ADR100-104 and migrations 00080-00092 exist); `AGENTS.md:50-125` ends at ADR063 (no ADR100-104); `:76-78` names schema/IR/compiler 1.4 only (`IntegritySchema` admits 1.4.0 and 1.5.0, `models.go:23`); `:115` "API/workflow stay RR" (the sandbox freeze route runs READ COMMITTED, `access.go:225-228,344-345`); `:122-123` "Only `local.noop` is registered" is FALSE when `TOOL_CATALOG_FILE` is set (`app.go:62-78`). Still true and now enforced: `:32-33` "Keep operator credentials out of runtime subprocesses" (`app.Main` refuses the six variables, IR-07; executed with the real `cmd/api` binary and synthetic values: exit 1, six names, no value). The make targets it names exist. No readiness, M15+, production data plane or real-provider claim in either file.

## Provenance of the imported reviewer reports

Each in-tree report is byte-identical to the last commit of the reviewer's branch (0 differing lines): `INTEGRATION_INDEPENDENT_REVIEW.md` and `M14_INDEPENDENT_REVIEW.md` = `origin/review/integration-final` `cb9e91a` (pins `552fa9e`, `be6ec7c`, `0e13abe` in the headers); `M14_AND_FIXES_INDEPENDENT_REVIEW.md` = `origin/review/m14-and-fixes` `1b68dae` (pin `be6ec7c`, round 3 at `507c12c`); `UPGRADE_AND_ORDERING_REHEARSAL.md` = `origin/audit/upgrade-rehearsal` `6f0805e` (`be6ec7c`, `43d417e`, `507c12c`); `DOCS_TRUTH_AUDIT.md` = `origin/audit/docs-truth` `e6beca0` (`be6ec7c`). The integrator's commits `079b8f3` and `fee7563` touched two of them but the result equals the branch tip: later reviewer text was imported, nothing was edited inside. The integrator's framing (plan:18-23, ledger:36-39, handoffs:295-297) matches the report headers (453 PASS / 5 SKIP / 0 FAIL, IR-01 to IR-06 and M14R-01, -02, -04, -06, -07 FIXED, M14R-03 PARTLY FIXED, PASS at `0e13abe`, IR-07 and IR-08 new) except DT-01, DT-06, DT-08.

## Executed here (commands and results)

`gt test -count=1` of the named tests (23 test functions in 28 packages, 0 FAIL); `TestRedaction*` with `REDACT_PROPERTY_RUNS=300000`: PASS 50 s; `FuzzRedactText` 30 s: 83,875 executions, PASS; the review shapes against the pre-fix redactor (overlay of `0e13abe` `redact.go`): 24 of 26 cases fail, the two that pass are the long-key storage string and `AWS_SECRET_ACCESS_KEY` (document: "nine survived, the tenth only with a short key": TRUE); IR-08 scan against the pre-fix `version_family.go`: exactly four new spellings fail (TRUE); IR-07: removing the call from `Main` (temporary, reverted) fails `TestMainRefusesOperatorCredentialsBeforeRunning` (TRUE; the guard is syntactic: `if false && ...` passes it); real `cmd/api` binary: refusal executed; verifier-v3 tests PASS (32 amounts probed, 33 blocked, the 8 listed extra probes of a 626-749 window); `TestMigrationNumbersFormOneIncreasingSequence` with 00058 / 00092 twice / 00094: FAIL, with 00093: PASS; `git apply --check` of the proposal patch on scratch copies: exit 0; `make docs-verify`: PASS; 461 integration tests listed at the pin (459 at `507c12c` + 2), 10 fuzz functions (`FuzzRedactText` has no make target and is not in the gate).

## NOT RUN / UNVERIFIABLE

Everything that needs PostgreSQL, Redis, Docker or a browser: the integration suite; the `507c12c` gate (no committed log); `TestM14R03ATokenThatEndsBefore...` and `TestF05ApprovalOvertakenByAFirstActivation...` (the `585499d` gate log shows only `ok tests/integration 2058.622s`, no -v); reference timings (8.55 s, 1.6 s, 50-75 ms, 18.6 s); `migrate status` against a real database; the 209,433-execution fuzz run and the 25 and 8 mutation counts (one-off runs, no log); the "reported 28 October" delivery date of the third pass (the report is dated 22 October).

## Agreement with the previous audit

Hold (checked at the pin): its DT-01, 02, 03, 04, 05, 08, 09, 10, 14, 15, 16, 17 (`cmd/migrate/main.go:47`), 18 (ADR104 A1 "Struck by Amendment B.7"), 19, 20, 21, 22, 23 (ADR103:370), 25, 26 (`numbering_test.go:10` cites a file that now exists). Not corrected: DT-06 and DT-07 (`AGENTS.md`, `CLAUDE.md`, by instruction), DT-24 (README: DT-02 here), DT-13 in the handoffs (DT-13 here), DT-12 (DT-24 here), FS-09 (DT-25 here), DT-11 partly (DT-06 here).
