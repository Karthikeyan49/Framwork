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
concurrency" below is the follow-up assignment that replaced Part B.

## Progress

| Time (UTC, 2026-10-21) | Step | State |
|---|---|---|
| 19:53 | worktree verified at `be6ec7c`; Docker stacks ready; required reading done | done |
| 20:00 | baseline: stack reset + `TestM14SlowRequestBodiesDoNotHoldDatabaseSlots`, `TestM9RUniqueLifecycleIsTheSameUnderEveryIntegrityCapableSchema`, `TestMigrationLineageRefusesTheMilestoneLinesOldNumbering`, `TestIntegrationTenantRoleApproverGatesAToolSend` (`-race`) | PASS, 34.5 s |
| 20:03-20:10 | A1 body pre-read: raw-socket probes against `httpserver.New` on the pinned tree and on a scratch copy with the pre-read disabled; memory measurement; mutation check of the slow-body test | done, F-01, F-02 |
| 20:10-20:18 | A2 live catalog scan, 89 -> 90 attribute diff, unique duplicate/abort lifecycle under 1.4.0 and 1.5.0, scanner spellings, negative control; A3 restore matrix; A4 real launcher with fake binaries; A5 real `migrate` binary against seven database shapes and a fresh database; A6 tools harness under `-race`, mutation of the tenant-approver test | done, F-03, F-04 |
| 20:20-20:40 | Part B reading started (ports `00089`, `internal/ai` service/gateway/store, `access/ai*.go`); no Part B attack executed before the stop | stopped (API rate limit, host restart) |
| 2026-10-22 11:31 | resumed with a changed scope: Part B dropped (done by another reviewer), report finished for A1-A6, then M13 release-pointer concurrency | in progress |

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
* The fixes were re-verified on `be6ec7c` only; nothing of the coordinator's later fix commits on `integration/m9r-m14`.
* The full integration suite, `make check`, `make test-unit`, browser, fuzz and outage gates were not rerun (targeted runs only:
  the four fix regression tests, `internal/tools` `-race -count=3`, `internal/metadata` and `internal/compiler` probe units).
* F-01 was measured in-process with one subject from one peer; a multi-peer run up to the global limit (about 12,000 held
  requests) was not executed (it would exhaust the shared host), the 13.7 GiB figure is arithmetic on the measured 1.14 MiB.
* HTTP/2 (not served on the plain listener) and a real proxy in front of the API were not exercised.
* A2: the 1.5.0 unique lifecycle was run on the dataset-binding route of the suite's fixture; a 1.5.0 document that also
  declares tool activities together with a unique field was not authored.
* A6: `TestM12APIDetailReadsOfALargeCandidateConverge` (the `m12apiRetrying` change) was not run.

## Probe sources

The probes were temporary files (`tests/integration/zz_review_a1_test.go`, `zz_review_a2_test.go`, `zz_review_a5_test.go`,
`internal/metadata/zz_review_scan_test.go`, `internal/compiler/zz_review_restore_test.go`), deleted before every commit; the
snippets above are their essential parts. Scratch mutants: a copy of `be6ec7c` with `preRead := false && ...` (A1) and one with
`|| (false && !slices.Contains(permitted, roles[0]))` in `internal/policy/approval.go` (A6), never on the branch.
