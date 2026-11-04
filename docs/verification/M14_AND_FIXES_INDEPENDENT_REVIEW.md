# Independent review of the 2026-10-20/21 integration fixes and first independent review of M14

Reviewer: independent agent; did not build, fix or merge any of the reviewed code. No product code was changed.
Pinned commit: `be6ec7cae1849bf535bad1811b21bf93067598b5` (head of `origin/integration/m9r-m14` at start).
Review branch: `review/m14-and-fixes`. Stack: isolated M14 Compose stack only (`/home/user/bin/dbrunm14`); every integration
probe below runs on a disposable database created by the suite's own `m8Database` fixture.
Rules of truth: PASS / FAIL / NOT RUN. Every "held" names the attack that was executed.
Numbers: this report uses the merged numbers (migrations `00080`-`00090`, ADR100-104, tool schema/IR/compiler `1.5.0`);
older reports are read through the map in `docs/migration/LINE_INTEGRATION_PLAN.md`.
Host: 4 cores shared with two other agents (load average recorded next to timing results).

STATUS: Part A (the integration fixes) COMPLETE: 2 FAIL-grade findings of the fixes (F-01 MEDIUM, F-02 LOW) and 2 LOW gaps
(F-03, F-04); every other attack listed below held. Part B (first independent review of M14) was NOT RUN by this reviewer: the
coordinator reassigned it after a host restart and it was completed by another reviewer
(`origin/review/integration-final:docs/verification/M14_INDEPENDENT_REVIEW.md`, M14R-01..04). Section "M13 release-pointer
concurrency" below is the follow-up assignment that replaced Part B: 12 pairs executed, the rollback race of IR-06 reproduced (FAIL)
and one more LOW (F-05: approvals recorded on rejected or superseded candidates); 8 pairs PASS. Re-verification of the coordinator's
fixes (last section): F-01, F-02 and IR-06 PASS; the body budget opens a new MEDIUM (F-06, a cross-tenant write lock held by eight
unprovisioned identities with header-only requests); F-05 is still open. Round 2 (`a2c5c52`, `8528fde`, `9d2928d`, `b05494a`): M14R-01, -02,
-07, the concurrent-result part of M14R-03, D1 and D2 fixed; M14R-06 partly (F-08 LOW); label reduction loses evidence (F-07 LOW).
Round 3 (`507c12c`): F-05, F-06, F-07 and F-08 fixed; no new defect; the stated bandwidth-bound residual of the body budget reproduced.

## Progress

| Time (UTC, 2026-10-21) | Step | State |
|---|---|---|
| 19:53 | worktree verified at `be6ec7c`; Docker stacks ready; required reading done | done |
| 20:00 | baseline: stack reset + `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots`, `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema`, `TestMigrationLineageRefusesTheMilestoneLinesOldNumbering`, `TestIntegrationTenantRoleApproverGatesAToolSend` (`-race`) | PASS, 34.5 s |
| 20:03-20:10 | A1 body pre-read: raw-socket probes against `httpserver.New` on the pinned tree and on a scratch copy with the pre-read disabled; memory measurement; mutation check of the slow-body test | done, F-01, F-02 |
| 20:10-20:18 | A2 live catalog scan, 89 -> 90 attribute diff, unique duplicate/abort lifecycle under 1.4.0 and 1.5.0, scanner spellings, negative control; A3 restore matrix; A4 real launcher with fake binaries; A5 real `migrate` binary against seven database shapes and a fresh database; A6 tools harness under `-race`, mutation of the tenant-approver test | done, F-03, F-04 |
| 20:20-20:40 | Part B reading started (ports `00089`, `internal/ai` service/gateway/store, `access/ai*.go`); no Part B attack executed before the stop | stopped (API rate limit, host restart) |
| 2026-10-22 11:31 | resumed with a changed scope: Part B dropped (done by another reviewer), report finished for A1-A6, then M13 release-pointer concurrency | done |
| 11:33-11:38 | release-pointer races: 12 pairs executed with held REPEATABLE READ transactions | S2 FAIL (= IR-06), S3/S4/S5 FAIL (F-05 LOW), 8 PASS |
| 11:40 | `TestM12APIDetailReadsOfALargeCandidateConverge` once (A6 item) | PASS, 10.8 s, load 2.5 |
| 11:42-11:50 | re-verification of `c76968f` (F-01/F-02) and `bfa47c4` (IR-06) in a scratch copy of `bfa47c4`; mutation and negative controls of the builder's new tests | F-01 PASS, F-02 PASS, IR-06 PASS; new F-06 MEDIUM; F-05 still open |
| 12:07-12:16 | round 2: `9d2928d`, `b05494a`, `a2c5c52`, `8528fde` on a scratch copy of `2ca08d8` with my own probes | M14R-01/-02/-07, M14R-03 results, D1, D2 fixed; M14R-06 partly (F-08); F-07 new LOW |
| 17:14-17:18 | round 3: `b427e8b`, `37d9b14`, `dae9a1b` (head `507c12c`) on the `fix` stack | F-05, F-06, F-07, F-08 fixed; F-06 bandwidth residual confirmed as stated; no new defect |

## Findings

Severity: HIGH exploitable violation of a security or integrity invariant; MEDIUM realistic integrity/availability defect or
documented-contract breach; LOW hardening, documentation or test gap.

### F-01 MEDIUM - the IR-02 pre-read lets any valid token make the API buffer 1.14 MiB per connection, ignoring the route caps

`serve` (`internal/platform/access/access.go:293-302`) reads every verified-bearer POST body up to the front door's 1 MiB cap
**before** identity resolution, membership and policy, and before the handler applies its own cap (AI create 32 KiB, apply 128 KiB,
confirm 64 KiB, reject 4 KiB, metadata/datasets 260 KiB, commands 64 KiB). A caller whose token merely verifies (a subject that is
not provisioned at all, or a `viewer`) is read in full and held for up to the 10 s read timeout.

Executed (`TestZZReviewA1Memory`, real `httpserver.New` server in the test process, one tenant-owner token, each connection sends the
headers of a 1 MiB body and 1 MiB - 1 bytes, then stalls; heap measured after `runtime.GC()`; no `-race`):

| tree | connections | heap in use before -> while held | per connection | answers |
|---|---|---|---|---|
| pinned `be6ec7c` | 300 | 10.8 -> 352.0 MiB | 1.14 MiB | none (all held), no 429 |
| pinned `be6ec7c` | 1000 | 10.6 -> 1151.7 MiB (Sys 1186.9 MiB) | 1.14 MiB | none (all held), no 429 |
| scratch copy, pre-read disabled (= pre-fix path) | 300 | 10.0 -> 10.2 MiB | 0.00 MiB | 413 `body_too_large` at once (the 32 KiB handler cap) |

The traffic gate admitted all 1000 requests of one subject from one peer (burst 600 + 300/s). Within one 10 s read timeout the default
limits admit about 3,600 requests per peer (about 4 GiB) and about 12,000 in total (global burst 2,000 + 1,000/s, about 13.7 GiB): enough to
exhaust a 16 GB host and take the API down for every tenant. `TestZZReviewA1WhoIsRead` confirms that the subject needs no membership:
with `Content-Length: 900000` and one byte sent, an unprovisioned-but-signed subject and a `viewer` wait 10.03 s on the AI and metadata
routes (read), where the pre-fix copy answers 401 / 403 in 0.02-0.03 s. With `Expect: 100-continue` the pinned tree now sends
`100 Continue` to a `viewer` and reads its body before the 403; the pre-fix copy answers 403 without asking for the body.

The plan's residual ("at most 1 MiB per in-flight verified POST ... bounded in number only by the traffic gate") understates this: the
measured cost is 1.14 MiB, the caller needs no tenant membership, and the previous bound on the AI routes was the handler cap
(32 KiB create, 4 KiB reject: an amplification of 32x to 256x), not 1 MiB. The DB-slot defect IR-02 is fixed; it was traded for a memory
exhaustion that a single token can trigger with default settings.

Repro (inside the M14 fixture, `e := m14Setup(t, m14Options{}); e.draft("500")`, real server from `httpserver.New`):

```go
head := fmt.Sprintf("POST %s/ai/patch-proposals HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 1048576\r\n\r\n", e.root, e.alice)
body := bytes.Repeat([]byte("a"), 1<<20-1) // one shared buffer on the client side
for i := 0; i < 1000; i++ { c, _ := net.Dial("tcp", addr); c.Write([]byte(head)); c.Write(body) } // keep c open
time.Sleep(1500 * time.Millisecond); runtime.GC(); runtime.ReadMemStats(&m) // HeapInuse grows by ~1.14 MiB per connection
```

Fix direction (for the builder): pre-read with the cap of the matched route (the handlers' constants), not the front-door cap, and/or a
process-wide budget of buffered body bytes (per subject and global) that answers 429/503 instead of buffering.

### F-02 LOW - "an anonymous caller is never read" is false on four routes

The new comment in `serve` and the plan state that only a verified bearer is read, "so an anonymous caller cannot make the server buffer
anything". The release idempotent POSTs (`retryRelease`: `candidates`, `acceptance-specs`, up to 1 MiB) and the two `commands` routes
(`localCommand`, up to 64 KiB) read the body before authentication (`access.go:265-286`, unchanged by `f997efe`). Executed
(`TestZZReviewA1WhoIsRead`, `Content-Length: 900000`, one byte sent): anonymous and invalid-bearer requests to `candidates`,
`acceptance-specs` and `.../commands` wait 10.03 s and the connection closes without an answer; the AI and metadata routes answer 401 in
0.02 s. Identical on the pre-fix copy, so this is not a regression of the fix, but the invariant the fix relies on does not hold. Bounded by
the invalid-bearer budget (20 per peer, 50 global burst, 10/s), so about 150 MiB at most.

### F-03 LOW - the version-gate scan is narrower than "the next superset cannot repeat it"

`internal/metadata/sql_version_gate_test.go` reads only dollar-quoted function bodies and only single-quoted `'1.N.0'` literals;
`00090`'s own post-check matches only `schema_version = '1.4.0'`. Executed (`familyGaps` fed 16 spellings of a single-version gate):
reported for `=`, one-member `IN`, `IS NOT DISTINCT FROM`, `->>` path equality, `E'...'`, `CASE ... WHEN`, `= ANY(ARRAY[...])`; **not reported**
for `'1.4.'||'0'`, `$v$1.4.0$v$`, `@> '{"schema_version":"1.4.0"}'`, a jsonpath `== "1.4.0"`, `LIKE '1.4%'`, `~ '^1\.4\.'` and `'1.4'`; it
also flags the harmless `>= '1.4.0'`. Policies, trigger `WHEN` clauses, other CHECKs, defaults, views and index predicates are not scanned at
all. No defect today: a live catalog scan of a migrated head database (150 functions, 1,090 constraints, 225 policies, 102 triggers, 165
defaults, 3 views, 19 expression/partial indexes; `TestZZReviewA2CatalogVersionGates`) finds only the fixed function, the complete CHECK,
the event envelope literal `'1.0.0'` in `runtime.freeze_event` and two column defaults `'1.0.0'` (overwritten from the document / a record
format version), none of which gates on a subset of the authoring family. The negative control holds: with `00090` removed in a scratch copy
`TestSQLFunctionsNameWholeSchemaVersionFamilies` FAILS on `runtime.register_unique_constraint`.

### F-04 LOW - launcher kind detection fails open, and two evidence statements overstate their tests

* `scripts/with-env.mjs` picks the allowlist only for the exact argument forms `./cmd/<kind>`, `bin/<kind>` and `*/bin/<kind>`; anything else
  gets the parent's whole environment. Executed with the real launcher, a synthetic `.env` and fake binaries:
  `bin/api`, `bin/worker`, `bin/dispatcher`, `./bin/worker`, an absolute `.../bin/worker`, `go run ./cmd/api` and `go run ./cmd/worker` receive
  exactly the allowlists (API: `AI_PROVIDER_FILE`, `TOOL_CATALOG_FILE`; worker: the four `TOOL_*`; dispatcher: none; no `MIGRATION_DATABASE_URL`
  anywhere). `go run ./cmd/worker/` (trailing slash) and `go run github.com/Karthikeyan49/Framwork/cmd/worker` receive **everything**,
  including `MIGRATION_DATABASE_URL`, `POSTGRES_PASSWORD`, `MIGRATOR_PASSWORD` and the operator Redis URL (AGENTS.md: "Keep operator
  credentials out of runtime subprocesses"). The Makefile targets use the exact forms, so the documented launchers are safe; pre-existing, not
  introduced by `dfec052`.
* The plan says the B-01 launcher test starts "a fake `bin/api` and `bin/worker`": `scripts/runtime-env.test.mjs` starts only `bin/api`.
  The plan cites `tests/integration/m_migration_lineage_test.go` as "executed against the real `migrate` command": the test calls
  `postgres.CheckMigrationLineage` only (A5 below runs the real binary).

## Attacks that held

### A1 body pre-read (`f997efe`, `f6398bf`)

* **Mutation check of `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots`**: in a scratch copy with `preRead := false && ...` the test FAILS
  ("honest read behind 4 slow bodies: status 200 after 8.56 s"); on the pinned tree it PASSES (proposal 201 READY in 1.62 s). The test is not
  vacuous. Load average 2.72 during the mutant run.
* **Old/new differential over sizes and framings**: 9 POST routes (AI create, AI apply, metadata, datasets, approve, candidates, sandboxes,
  unique build, query) x bodies of 0, 2, 1 MiB - 1, 1 MiB, 1 MiB + 1, 3 MiB x `Content-Length` and chunked: all 108 status/code pairs are
  identical between the pinned tree and the copy without the pre-read (`TestZZReviewA1Sizes`). The cap error is replayed faithfully.
* **Keep-alive reuse after a read error**: a malformed chunk, a chunk shorter than announced and a 2 MiB body (cap hit), each followed by a
  pipelined GET on the same connection: exactly one answer and the connection is closed (no second request parsed). Valid chunked bodies and a
  body on a bodiless route are followed by the pipelined answer (reuse works). Same on both trees except the read-error code (below).
* **Audit/code difference of a failed read** (documented by the plan): the pinned tree answers a broken chunked body on the AI route with 400
  `invalid_request` before the transaction; the pre-fix copy answered 400 `invalid_ai_request` inside it. No authorization decision is skipped
  that would let a caller learn anything (the 400 comes before any decision). Not graded.
* **Anonymous / invalid bearer on pre-read routes**: AI and metadata POSTs answer 401 in 0.02 s without reading (both trees).
* **Content-Length 0, chunked empty body, protocol-level forms**: size 0 with `Content-Length: 0` and with an empty chunked body give the
  handlers' own typed 400 on all nine routes on both trees.
* Note for readers of timing probes: with an announced body below 256 KiB, net/http itself drains the rest of the body before it writes any
  answer (`maxPostHandlerReadBytes`), so a stalled small body gets no answer on either tree, but no database slot is held while net/http
  drains (the slot is released before the response is written). The who-is-read probe therefore announces 900,000 bytes.

### A2 migration `00090` and the version-gate scan (`46d8a2a`)

* **Function attributes 89 -> 90** (`TestZZReviewA2RegisterUniqueBeforeAfter`, disposable database at 89, then `UpTo(90)`): owner
  `framework_schema_owner`, `SECURITY DEFINER`, `search_path=pg_catalog, pg_temp`, ACL `{framework_schema_owner=X, framework_app=X}`, volatility,
  parallel safety, leakproof, return type and the EXECUTE matrix of app/worker/dispatcher/migrator/schema owner/sign-in/PUBLIC are identical;
  only the body digest changes. A text diff of the two bodies differs only in `CREATE OR REPLACE` and the `IN('1.4.0','1.5.0')` predicate.
* **Unique lifecycle with populated entities**: the builder's `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema` PASSES (60 rows);
  my variation `TestZZReviewA2UniqueDuplicateAbortUnder15` (40 duplicate rows) gives the same sequence under 1.4.0 and 1.5.0: register BUILDING/1,
  duplicate build 409, abort ABORTED, re-register BUILDING generation 2, 0 leaked claims, 1 history row, claim source schema = the document's.
* **No unique under older contracts**: authoring a 1.3.0 or 1.2.0 document with `unique: true` is refused (400 `invalid_metadata`).
* **Live catalog scan** (see F-03): no other SQL object gates on part of the authoring family.

### A3 restore pairing (`dfec052`)

`TestZZReviewRestoreMatrix` (unit, `internal/compiler`): the compiled 1.5.0 tool artifact relabelled over 6 IR versions x 8 compiler labels x
6 application schema versions (document and every definition relabelled, freshly hashed): **1 of 288 restores**, the original triple. A tool-free
integrity artifact restores only as (1.4.0, 1.4.0, 1.4.0) or (1.5.0, 1.5.0, 1.5.0), which is the documented superset. A single definition
relabelled 1.4.0 or 1.3.0 inside a 1.5.0 artifact, a changed namespace and a migration class are refused. Plan DSL: 2.2.0 with contracts and the
documented restore-only legacy shape (2.1.0 without contracts) restore; 2.0.0, 2.1.0 with contracts, 2.2.0 without contracts and 2.3.0 are
refused. A forged `catalog_hash` restores (only checked non-empty; it feeds only the diff's `IRUpgrade` flag), and the send path re-derives the
contract from the live catalog and fails closed on disagreement (`tool_step.go:243-247`). Read, not executed: `MatchesDefinition`,
`promotion` staleness (`SupportedIR`), `dependencies.runtimeComponents` (DSL 2.0.0/2.1.0/2.2.0), `contracts.ts` (IR list and `unique` only for
1.4.0/1.5.0), `internal/ai/schema_text.go` (1.3.0/1.4.0/1.5.0 schema files): consistent with the restore checks.

### A4 runtime environment (`dfec052`)

Every variable that `config.Load` (including `loadTraffic` and the OIDC block) and the app wiring read for the API, worker and dispatcher is on
the matching allowlist, `TOOL_SECRETS_DIR`/`TOOL_CA_FILE`/
`TOOL_ALLOW_INTERNAL` reach only the worker, `AI_PROVIDER_FILE` only the API, the dispatcher and the web server get none of them and no
process gets `MIGRATION_DATABASE_URL` (executed with the real launcher, see F-04 for the fail-open forms).

### A5 lineage guard (`dfec052`)

`TestZZReviewA5Lineage` / `TestZZReviewA5FreshDatabase` run the real `cmd/migrate` binary built from the pinned tree (`status`, then `up`):

| database | guard | `status` / `up` | version after |
|---|---|---|---|
| fresh (no version table, no objects) | pass | 0 / 0, 68 applied | 90 |
| remediation numbering at 57 | pass | 0 / 0, 11 applied | 90 |
| merged at 89 / at 90 | pass | 0 / 0, 1 / 0 applied | 90 |
| shared prefix at 44 | pass | 0 / 0, 24 applied | 90 |
| old milestone migrations (from `60da6f2`) at old 45, old 53, old 45-53 + 60 (the MAIN shape) | refused, typed error | 1 / 1, nothing applied | 45 / 53 / 60 |

No false positive and no false negative among these shapes (migrations 1-44 are byte-identical on both lines, checked).

### A6 test-side changes (`1fb0110`, `6c4cee6`, `bbf2465`)

* **Mutation check of `TestIntegrationTenantRoleApproverGatesAToolSend`**: in a scratch copy where `policy.RoleStore.LockWorkflowApprover` no
  longer requires the actor's role to be in the plan's approval roles, the test FAILS at the first refusal (the admin without
  `finance_manager` gets 200). Not vacuous. On the pinned tree it PASSES (18.6 s). Observation: that list check exists only in Go; the SQL port
  `control.lock_workflow_approver` receives only the actor's own role and fences its revocation.
* **`wait` panicking `http.ErrAbortHandler`**: all three callers are handlers of the test server; the client has already decided its outcome
  when its context ends. `go test -race -count=3 ./internal/tools` PASS (10.3 s) and the three tests that use `wait` `-count=20` PASS (18.8 s,
  load 2.7-3.3).
* **`m12apiRetrying`** (read): wraps only the set-up of the two large releases, retries only 429/503 up to six times, and the measured readers
  use the unwrapped handler. It can hide a set-up flake, not a claim.
* **`m10Options.approvers` / `customRoles`** (read): `nil` keeps `admin, owner`; `customRoles` only adds `RoleStore.Load` for the new test.

## Not covered

* **Part B, the M14 review**: not executed by this reviewer (reassigned, see STATUS). From reading only, before the stop: the gateway
  returns a cancelled provider call without an attempt row and without any usage (`internal/ai/repair.go:171-175`), while a timeout is
  charged its input estimate and a crash its whole reservation; the other reviewer executed this as M14R-02. Not claimed here.
* Part A judged `be6ec7c`; the coordinator's later local commits up to `2ca08d8` were re-verified in the two re-verification sections
  (scratch copies of `bfa47c4` and `2ca08d8`, nothing checked out or pushed by me). Not executed there: the token that expires during the
  provider phase (a documented residual of `8528fde`), a known abort raised outside `Admit` (read only: now 409 instead of 503), a retry budget
  that runs out in the middle of a try (read only: `RetryWithin` hands the attempt the budget context, so such a try is cancelled and the
  exchange answers 503 without the 409 audit; per-tenant contention of at most eight results does not reach 6 s), and `make check` /
  `make test-unit` / the full suite on the new head.
* The full integration suite, `make check`, `make test-unit`, browser, fuzz and outage gates were not rerun (targeted runs only:
  the four fix regression tests, `internal/tools` `-race -count=3`, `internal/metadata` and `internal/compiler` probe units).
* F-01 was measured in-process with one subject from one peer; a multi-peer run up to the global limit (about 12,000 held
  requests) was not executed (it would exhaust the shared host), the 13.7 GiB figure is arithmetic on the measured 1.14 MiB.
* HTTP/2 (not served on the plain listener) and a real proxy in front of the API were not exercised.
* A2: the 1.5.0 unique lifecycle was run on the dataset-binding route of the suite's fixture; a 1.5.0 document that also
  declares tool activities together with a unique field was not authored.
* A6: `TestM12APIDetailReadsOfALargeCandidateConverge` ran once on `be6ec7c` (PASS, 10.8 s); no repetition under load.

## M13 release-pointer concurrency (follow-up assignment, 2026-10-22, pinned tree `be6ec7c`)

Question: does any pair among `approve_release`, `reject_release`, `activate_release` and `rollback_release` (latest definitions:
`approve_release` from `00084`, the other three from `00088`) let a REPEATABLE READ transaction whose snapshot predates a committed
conflicting change still move the pointer, or record an approval onto a rejected or superseded hash?

Method (template: Appendix A.2 of `M14_INDEPENDENT_REVIEW.md`): each "T1" is a real runtime transaction (`postgres.WithRuntime`, REPEATABLE
READ) bound as the acting member (`bindDirect`), which takes its snapshot (`SELECT pg_current_snapshot()`), waits, and only then calls its
port and commits; "T2" is the conflicting port call, committed in between. Setup per test: c1 activated (generation 1), c2 activated over
it (generation 2), candidates c3/c4/c5 built on base c2@2 (`TestZZReviewRaceAgainstRejection`, `...AgainstActivation`,
`...AgainstRollback`, `...RejectVsRollbackAndFirstActivation`; reset stack, `-race`, 14.7 s + 8.0 s, load average 1.0-2.6).

| # | T1 (older snapshot) | T2 (committed in between) | T1 result | pointer / evidence after | verdict |
|---|---|---|---|---|---|
| S1 | `activate(c4, c2@2)` | `reject(c4)` | 40001 | c2@2 unchanged | PASS (M13R-01 fence) |
| S2 | `rollback(target c1, c2@2)` | `reject(c1)` | **ok, generation 3** | **pointer on the rejected c1** | **FAIL** (= M14R-04 / IR-06, reproduced) |
| S3 | `approve(c3)` | `reject(c3)` | **ok, count 1** | **approval of dave recorded on the rejected c3** (1 approval, 1 rejection) | **FAIL** (F-05, LOW) |
| S4 | `approve(c5, base c2@2)` | `activate(c4)` moves the pointer to c4@3 | **ok, count 1** | **approval recorded on the superseded c5** | **FAIL** (F-05, LOW) |
| S5 | `approve(c5, base c2@2)` | `rollback(c1)` moves the pointer to c1@3 | **ok, count 1** | **approval recorded on the superseded c5** | **FAIL** (F-05, LOW) |
| S6 | `activate(c3, c2@2)` | `activate(c4, c2@2)` | 40001 | c4@3 | PASS |
| S7 | `activate(c3, c2@2)` | `rollback(c1)` | 40001 | c1@3 | PASS |
| S8 | `rollback(target c1, c2@2)` | `activate(c4)` | 40001 | c4@3 | PASS |
| S9 | `rollback(target c1, c2@2)` | `rollback(c1)` by another operator | 40001 | c1@3 | PASS |
| S10 | `reject(c4)` | `activate(c4)` | 40001 (the veto aborts and can be repeated) | c4@3, no rejection | PASS (no pointer onto a rejected hash) |
| S12 | `reject(c1)` | `rollback(c1)` | ok | c1@3, rejected after it became active | PASS (equals the serial order rollback -> reject; a veto after a release ran is allowed) |
| S13 | first `activate(a, '', 0)` | first `activate(b, '', 0)` | 40001 | b@1 | PASS |
| - | `approve(c?)` vs a committed `approve` / `reject` vs `reject` | - | not raced: no shared invariant (both rows are per actor) | | NOT RUN |

The generation / expected-base compare-and-set holds in every executed pair: `active_releases ... FOR UPDATE` under REPEATABLE READ raises
40001 when the row changed after the snapshot (S6-S9, S13). A stale approval is inert at the pointer: after S4 the activation of c5
(`expected c2@2`) is refused with 409 through the API, and S1 shows the fence refusing an activation of a hash rejected concurrently.

### F-05 LOW - `approve_release` has neither the fence nor a pointer lock: an approval whose transaction began earlier is recorded on a rejected or superseded candidate

`approve_release` (still the `00084:120-143` definition; `00088` did not replace it) reads `release_rejections` and the active pointer from its
snapshot and inserts the approval without touching `control.release_fences` or locking `active_releases`. S3: a rejection acknowledged to its
author (bob) is followed by dave's approval of the same hash; S4/S5: an approval is recorded for a candidate whose base the pointer had already
left. The approvals cannot move the pointer (activation is fenced and compare-and-set; stale candidates never become fresh again because
generations only grow), but the port's own refusals ("release candidate was rejected", "candidate is stale") are overtaken, and the
evidence that `promote.go` shows and counts (`Store.Approvals`, `ApproverRoles`) then contains an approval made after a veto. Same class as
M13R-01 and IR-06, without a pointer consequence.

Repro (essential part; helper `zzHold` = the A.2 template with a second statement `SELECT pg_current_snapshot()` before the wait):

```go
e, c1, c2 := zzTwoReleases(t)                          // c1@1 -> c2@2
c3, _ := e.build("500", m13Relabel("Third release"), e.carol)
approve := e.zzHold("approve(c3)", e.dave.subject, `SELECT control.approve_release($1,$2)`, e.app, c3) // snapshot taken
e.rejectPort(e.f.bobSubject, c3, "RISK_UNACCEPTABLE", "veto")                                       // commits
t.Log(approve.finish(), len(e.approvalsOf(c3)), len(e.rejectionsOf(c3)))                            // "ok 1", 1, 1
```

Fix direction: write the same `control.release_fences` row in `approve_release` before the rejection check (as `activate_release` does), and
read the pointer with `FOR SHARE` (REPEATABLE READ then raises 40001 when it moved); extend `TestM13R01*` to approvals.

## Re-verification of the coordinator's fixes (2026-10-22, local commits on `integration/m9r-m14`)

Tree: a scratch copy of `bfa47c4` (contains `c76968f` for F-01/F-02, `b05494a` and `bfa47c4` for IR-06/M14R-04; read from the shared
object store, nothing checked out); disposable databases of the M14 stack; no `-race` for the memory probes, `-race` for the races.

### F-01 / F-02 (`c76968f`, body budget: 4 MiB per verified identity, 32 MiB total, booked before the first byte)

| probe | result | verdict |
|---|---|---|
| anonymous and invalid bearer, `Content-Length: 900000`, one byte sent, 9 POST route families (candidates, acceptance-specs, commands, query, AI, metadata, datasets, approve, roles) | 401 `unauthenticated` in 0.19-0.41 s on all 18 combinations (on `be6ec7c`: 10.03 s for candidates, acceptance-specs and commands) | F-02 **PASS** |
| mutation: the read condition without `preError == nil`, the builder's `TestM14PreReadBufferingIsBounded` | its sub-test "an anonymous or invalid caller is never read" FAILS on all ten combinations (8.0 s each) | the regression test is not vacuous |
| one identity, 1000 connections, 1 MiB - 1 sent each | 4 held, 904 x 429 `too_many_uploads`, 92 x 429 `rate_limited`; heap +5.5 MiB (`be6ec7c`: +1141.1 MiB); another identity is served | F-01 **PASS** |
| 40 signed identities, 1000 connections | 32 held, 778 x 503 `service_busy`, 190 x 429; heap +38.0 MiB | bounded |
| 40 identities, 400 chunked connections | 31 held (chunked books the 1 MiB cap), 362 x 503, 7 x 429; heap +35.2 MiB | bounded |
| 200 abnormal endings (client closes mid-body, malformed chunk, 2 MiB cap hit, `Expect: 100-continue` then abort), then 8 concurrent full 1 MiB posts of the same identity | 4 admitted (the AI route answers 413 after reading), 4 x 429: the share is whole again, no leaked booking | **PASS** |
| the builder's tests on the fix tree: `internal/platform/access` budget/replay units (`-race`), `TestM14PreReadBufferingIsBounded` (4 sub-tests) and the IR-02 regression `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots` | PASS (1.3 s; 15.6 s and 5.3 s, `-race`, load 1.4-2.2) | the slot fix keeps working with the budget |
| unprovisioned identity and `viewer`, 9 routes, `Content-Length: 900000` each | four bookings fit the 4 MiB share (held until the read timeout), the fifth and later are 429 at once | as designed |

### F-06 MEDIUM - the total body budget is a lock that eight identities without membership hold with header-only requests

The budget books the **declared** `Content-Length` before any byte arrives (`reservation`, `bodybudget.go`), and refuses every booking that
does not fit the 32 MiB total with 503 `service_busy`. Eight signed identities that were never provisioned (any account of the issuer, no
tenant membership) each open four connections that declare a 1 MiB body and send no body byte at all: 32 x 1 MiB fills the total exactly.
Executed (`TestZZReviewFixTotalBudgetHeaderOnly`, real `httpserver.New` server, process-default budget):

| moment | tenant owner's 2-byte `POST .../metadata/{app}/versions` |
|---|---|
| before | 400 `invalid_metadata` (the validator: served) |
| 0.3 s into the attack (32 header-only connections, heap +0.7 MiB) | **503 `service_busy`** |
| 9.3 s (first wave about to hit the read timeout) | **503 `service_busy`** |
| 9.7 s (the attacker reconnects; 64 connections) | **503 `service_busy`** |
| after the attacker left | 400 `invalid_metadata` |

Every body-carrying request of every tenant (record commands, metadata, AI, approvals, activation, rollback, role and team changes) is
refused while the lock is held; the attacker needs about 3.2 connections per second and almost no bandwidth to keep it. Before the fix the
same callers could exhaust memory (F-01); before `f997efe` one tenant owner could hold the four database slots (IR-02). The fix's own test
"the total bounds identities that stay within their own share" asserts this refusal as intended behaviour; the commit's claim "one identity
cannot starve the others beyond its share" is true, but eight identities starve everyone. GET requests are unaffected.

Repro (inside the M14 fixture; process-default budget):

```go
for i := 0; i < 8; i++ { tok := e.f.token(t, uuid(t), nil)                       // signed, never provisioned
  for j := 0; j < 4; j++ { c, _ := net.Dial("tcp", addr)
    c.Write([]byte("POST " + e.root + "/ai/patch-proposals HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer " + tok +
      "\r\nContent-Type: application/json\r\nContent-Length: 1048576\r\n\r\n")) } }  // no body byte follows
// any identity's POST with a body now gets 503 service_busy until the attackers' read timeouts (10 s); they reconnect
```

Fix direction: book bytes as they are read (for example in 64 KiB steps inside the replay reader), not the declared length, so that only
memory actually held counts; and/or keep part of the total for identities that resolve to an active membership, or size the total far above
`perSubject` times a plausible number of hostile identities.

### IR-06 / M14R-04 (`bfa47c4`, migration `00091`: `rollback_release` writes the fence)

* The race table above re-run on `bfa47c4`: **S2 now 40001**, pointer stays c2@2 (PASS); S12 (a rejection older than a committed rollback onto
  the same hash) now also aborts with 40001, which is consistent with S10; S1, S6-S10, S13 unchanged (40001); **S3, S4, S5 unchanged (F-05
  still open: `approve_release` was not part of the fix)**.
* Negative control: the builder's new `TestM13R01RollbackOvertakenByACommittedRejection` copied into a tree without `00091` FAILS ("the pointer
  moved onto a hash whose rejection had committed"); on `bfa47c4` all 18 `TestM13R*` PASS (63.2 s, `-race`).
* Read: `00091` replaces the `00088` body with one added `INSERT ... release_fences ... (p_target)` before the rejection check; the post-check
  keeps owner and ACL and requires the fence statement in the definition.

## Re-verification round 2 (2026-10-22, `9d2928d`, `b05494a`, `a2c5c52`, `8528fde`, `2ca08d8`)

Tree: a scratch copy of `2ca08d8` (the local head); my own probes (`zz_review_m14fix_test.go`, `zz_review_d2_test.go`), not the
coordinator's regression tests; `-race`; disposable databases; load average 0.6-4.2 (other agents active).

| item | verdict | executed evidence |
|---|---|---|
| M14R-01 provider labels (`a2c5c52`) | **fixed** | 8 answers with model labels `alpha 4 turbo`, `Beta-3.1 (70B)`, `модель-1`, `model=fast`, `модель`, 300 x `x`, `a<U+202E>b<NUL>c`, `ok-model:1`: all 201 READY, one attempt row and audit rows `admitted, created` each; stored and audited labels `alpha4turbo`, `Beta-3.170B`, `-1`, `modelfast`, empty, 128 x `x`, `abc`, `ok-model:1` |
| M14R-02 canceled calls (`a2c5c52`) | **fixed** | client cancels 600 ms into a 2 s provider call, five cycles: each proposal `FAILED/INTERNAL` with one `TIMEOUT` attempt row and 13,866 tokens charged (input estimate + whole reserved output); `used_tokens` 69,330 = the sum; the sixth admission is refused 429 `ai_budget_exhausted` (daily budget 120,000). A wall-clock timeout is charged the input estimate only (5,674) |
| M14R-06 reported usage (`a2c5c52`) | **partly fixed** (F-08 LOW) | an answer reporting 100,000,000 + 100,000,000 tokens is charged 57,512 and the next request is admitted (201) |
| M14R-07 adapter ignoring its context (`a2c5c52`) | **fixed**, bound refuses everyone while tripped | adapter that never returns, wall clock 1 s: each request ends after 1.3-1.5 s (`503 ai_unavailable`); after 19 abandoned calls (soft bound, 3 batches of 8 started below 16) an honest request is refused at once (503, adapter not reached); after the stuck calls return, the next request is 201 READY. The gateway is per process, so the refusal applies to every tenant; the production HTTP adapter honours its context (`NewRequestWithContext`, `DialContext`), so only a misbehaving adapter trips it |
| M14R-03 concurrent results (`8528fde`) | **fixed** | 3 rounds of 6 staggered admissions whose answers arrive together: 18 of 18 READY (the other review measured 3 of 6 lost per round before); 16 rolled-back transactions in the disposable database during the rounds versus 0 for six sequential proposals (so tries were repeated); `used_tokens` 140,940 = the sum of the 24 proposals' tokens; every proposal has exactly one `ai.proposal.admitted` and one `ai.proposal.created` audit row and as many invocation rows as attempts: **no double charge, no double store, no missing or duplicate audit** |
| M14R-03 simultaneous creations (`8528fde`) | residual (as the commit says) | 3 rounds of 8 at the same instant: 6, 7 and 7 READY, the rest 409 `ai_conflict` after 0.3 s (all five tries spent within about 100 ms of jittered pauses); each refusal has one `ai.propose.rejected` audit row and no `admitted` row; nothing stored or charged for them |
| M14R-03 token expiring during the provider phase | not fixed (the commit's residual) | NOT RUN by me |
| other status codes under the 5x admission retry | **unchanged** | 409 `draft_changed` 0.028 s, 400 `invalid_ai_request` 0.014 s, 429 `ai_busy` 0.026 s, 404 `not_found` 0.018 s, 403 `forbidden` (viewer) 0.013 s, 429 `ai_budget_exhausted` 0.063 s: same codes, no retry delay. Read, not executed: a known abort raised outside `Admit` (policy loading, audit append) was 503 before and is now 409 `ai_conflict` after the tries |
| D1 compensation scan (`9d2928d`) | **fixed** | `TestM9RSQLWorkerStatementPolicyCostAndRowLeaseExpiry` 3.56 s PASS and the three `TestM9*Compensat*` tests PASS on the head; `NOT IN` over `step_key` (NOT NULL, `00020`) is equivalent to the replaced `NOT EXISTS` |
| D2 migrate messages (`b05494a`) | **fixed**, no secret | real binaries with probe migrations against a database at 91: an extra `00058` -> rc 1 "migration refused: detected 1 missing (out-of-order) migration lower than database version (91): version 58; ..."; a duplicate `00091` -> rc 1 naming both files (status and up); a failing `00092` -> rc 1 "migration 92 failed (no connection details logged)"; no output matched a DSN/password/host pattern; `cmd/migrate` and `db/migrations` unit tests PASS |
| F-01 / F-02 / IR-06 | fixed (round 1) | see above; F-06 (new) and F-05 still open |

### F-07 LOW - label reduction merges and drops characters, so the trace can name a different model

`apiLabel` deletes every character outside `[A-Za-z0-9._:/@+-]` instead of replacing it: `Beta-3.1 (70B)` is stored and audited as
`Beta-3.170B` (reads as version 3.170B), `модель-1` as `-1`, `модель` as an empty label. The label is the trace's evidence of which model
produced a proposal; distinct labels can collapse into one. Fix direction: replace each run of refused characters with one `_` and keep a
hash of the raw label.

### F-08 LOW - the usage cap is four times, not twice, what a request could cost, and it can exceed the proposal's reservation

`chargeUsage` caps the input and the output separately at `2*(in+outTokens)+1024`, so one attempt can be charged about `4*(in+out)+2048`:
measured 57,512 tokens for an answer whose cancel-equivalent charge is 13,866 and whose whole three-attempt reservation is 41,598. One hostile
answer costs 29% of the default daily budget (four lock the tenant out for the UTC day), and because `ai_finish` charges more than was
reserved, `used_tokens` can pass the daily budget the admission checked. The commit says "a figure above twice what the request could cost ...
is capped".

## Re-verification round 3 (2026-10-22, `b427e8b`, `37d9b14`, `dae9a1b`, head `507c12c`)

Tree: a scratch copy of `507c12c` on the idle `fix` stack (`dbrunfix`, reset once from the tree: migrations through `00092`), every run
`nice -n 19`, one at a time, without `-race` (the coordinator's gate ran on the M14 stack; load average 3.6-7.1). Own probes only.

| item | verdict | executed evidence |
|---|---|---|
| F-06 (`b427e8b`, bytes booked as they arrive; 2 MiB per identity, 32 MiB total) | **fixed** | header-only attack, 1 MiB announced, nothing sent: 8 identities x 4 and 20 identities x 4 hold 0 bytes (heap +0.4 / +1.0 MiB), the owner's POST is served (400 `invalid_metadata`, the validator) during and after; the same with `Expect: 100-continue` (the server sends `100 Continue` to all 80, nothing arrives, nothing is booked) |
| F-06 residual (stated in the commit) | confirmed, not a new defect | 20 signed identities x 2 connections that really send 1 MiB - 1 and stall: 36 held, 4 refused 503; heap +36.8 MiB; the owner's 64 KiB POST gets 503 `service_busy` until they leave, then 400 again. With 16 x 2 the leftover 32 bytes of the total still admit a 2-byte POST. Bandwidth-bound (about 32 MiB per read timeout) and needs at least 16 accounts |
| booking under abuse | **holds** | one identity, 300 connections x 400 KiB sent (one byte short): 6 held, 294 x 429, heap +3.2 MiB, another identity served; chunked 1 MiB bodies from 20 identities: 33 held, 7 x 503, heap +36.8 MiB; a 20 KiB body written one byte per write: answered normally in 0.07 s |
| release on abnormal endings | **holds** | 5 x client gone after 700 KiB, 5 x malformed chunk after data, 5 x cap hit at 1 MiB, 2 x server read timeout after half a body; afterwards two full 1 MiB bodies of the same identity are both read (413 from the AI cap) and a third tiny POST still fits: nothing leaked |
| refusal in the middle of a body | **safe** | the identity's share full, then bodies of 2, 100 and 300 KiB that contain a complete request line, followed by a pipelined GET on the same connection: one answer (429) and the connection is closed; the embedded request line is never parsed and the pipelined request is not served (no reuse, no smuggling) |
| F-05 (`37d9b14`, migration `00092`) | **fixed** | S3 approve vs committed reject: 40001, no approval recorded; S4 approve vs committed activation and S5 approve vs committed rollback: 40001, no approval; S1, S2, S6-S10, S12, S13 unchanged (40001) |
| a veto aborted by an approval | **no** | S14: a rejection whose snapshot predates a committed approval of the same hash succeeds (approvals 1, rejections 1): approvals leave an existing fence row unchanged |
| approvers that conflict | **no** | S15: two approvers of one hash with overlapping transactions: both recorded (2 approvals) |
| lock-order deadlock | **none found** | 25 rounds of 4 concurrent random port calls (approve, reject, activate, rollback) on 4 candidates and the pointer: no 40P01; outcomes approve 1 ok / 7 x 40001 / 13 refused, reject 19 ok / 2 x 40001 (a concurrent activation of the same hash, the S10 class), activate 7 x 40001 / 20 refused, rollback 22 ok / 9 x 40001. Read: no path takes a fence row before the pointer, so no cycle |
| trigger and backfill hygiene | **holds** | `control.release_candidate_fence()`: owner `framework_schema_owner`, `SECURITY DEFINER`, `search_path=pg_catalog, pg_temp`, ACL owner only; app, worker, dispatcher and PUBLIC can neither insert into `release_candidates` nor write `release_fences`; a direct call as `framework_app` is refused 42501; the worker guard evaluated as `framework_worker` is true. Backfill: a database at 91 with two candidates has 0 fences, after `00092` 2 and none missing; a candidate staged afterwards gets its fence from the trigger |
| F-07 (`dae9a1b`, labels) | **fixed** (residual noted) | `alpha_4_turbo`, `Beta-3.1_70B`, `model_fast`, `a_b_c`, `ok-model:1`, 128 x `x`, all 201 READY with the same label stored and audited. Residual: a leading run of non-ASCII letters is still dropped (`модель-1` -> `-1`, `модель` -> empty), so two non-Latin labels with the same ASCII tail still collapse; no raw-label hash is kept |
| F-08 (`dae9a1b`, usage cap) | **fixed** | the answer reporting 100,000,000 + 100,000,000 tokens is now charged 29,780 (input 2 x 5,674 + 1,024 = 12,372; output 2 x 8,192 + 1,024 = 17,408), below the proposal's reservation of 41,598; the next request is admitted (201) |

## Probe sources

The probes were temporary files (`tests/integration/zz_review_a1_test.go`, `zz_review_a2_test.go`, `zz_review_a5_test.go`,
`internal/metadata/zz_review_scan_test.go`, `internal/compiler/zz_review_restore_test.go`, `tests/integration/zz_review_release_race_test.go`, `zz_review_fix01_test.go`, `zz_review_m14fix_test.go`, `zz_review_d2_test.go`, `zz_review_r3_budget_test.go`, `zz_review_r3_fence_test.go`), deleted before every commit; the
snippets above are their essential parts. Scratch mutants: a copy of `be6ec7c` with `preRead := false && ...` (A1) and one with
`|| (false && !slices.Contains(permitted, roles[0]))` in `internal/policy/approval.go` (A6), never on the branch.
