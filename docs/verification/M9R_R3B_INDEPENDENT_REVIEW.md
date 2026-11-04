# M9R R3b independent review

Reviewed stable commit `58c58ae630b368bfd185e297e928331acbe06dc9`, against parent `03fbfbd`, ADR030 and the remediation plan. This review covers governed numeric projection indexing, not full R2, R3c/R3d, production acceptance or general query performance.

**Final bounded R3b verdict at5fdb7f1: PASS; all findings from this review are closed.** Full R2/R3c/R3d and production acceptance remain outside this verdict. At initial checkpoint58c58ae the repository regression gate was PARTIAL: a reproduced MEDIUM workflow denial-provenance defect, fresh-migration fixture regression and implementer timing-dependent expired-lease failure were retained. Their original observations and subsequent executed closures appear below.

## Independence and safety

Implementation and applied migrations31–34 remained frozen during review. The reviewer changed no implementation, repository tests, credentials or services. The only repository addition is this report. Temporary Go overlays and their output are under `/tmp/r3b-review`; they are review harnesses, not application features. Tests used the repository's verified-target disposable database helper and cleanup, normal application/worker roles and owned fixture records. No shared Redis restart or concurrent implementer workload occurred during the isolated performance run.

Inspection covered migrations31–34, `internal/runtime/data/index.go`, numeric query selection and readiness, management routes and policy, worker grant checks, numeric integration tests, and the changed performance fixture. Source review is not represented as an independently executed credential exploit.

## Executed evidence

Commands were run from `/workspace/Framwork`. Each integration invocation used `-count=1`; the ordinary performance gate was run once, after correctness tests stopped.

| Exact command | Result | Evidence |
|---|---|---|
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-review/overlay.json -run '^TestIndependentR3B\|^TestM9RNumericIndex' -v ./tests/integration` | PASS,25.975s | Two independently authored populated/fault roots and all eight repository numeric roots. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM1FreshDatabaseMigrations$\|^TestM9ExpiredLeaseOnEarlySaveAndConcurrentSignals$\|^TestM9RejectedMutationsRollbackBeforeHTTPResponse$' -v ./tests/integration` | FAIL,7.854s | Fresh-migration fixture and workflow denial classification failed. Expired-lease root passed5.67s in this isolated run. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-review/baseline-overlay.json -run '^TestM1FreshDatabaseMigrations$\|^TestM9RejectedMutationsRollbackBeforeHTTPResponse$' -v ./tests/integration` | FAIL,2.440s | Owned-empty TRUNCATE CASCADE correction passed0.89s; workflow assertion remained failed. Initial diagnostic used nonexistent `allow` column and therefore did not establish the action; corrected below. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-review/baseline-overlay.json -run '^TestM9RejectedMutationsRollbackBeforeHTTPResponse$' -v ./tests/integration` | FAIL,1.376s | Corrected `allowed` column diagnostic executed successfully: actual denial action `preview.read.rejected`, resource `preview`; expected workflow denial absent. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-review/overlay.json -run '^TestIndependentR3BCheckpoint' -v ./tests/integration` | PASS,3.166s | Strengthened OWNER/CONDITION/NONE assertions, including positive visible controls and explicit absence of foreign-owner/conditional rows. |
| `make test-performance > /tmp/r3b-review/performance-results.txt 2>&1` | PASS,32.771s | Isolated ordinary-build reference workload described below. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-review/overlay.json -run '^TestIndependentR3BIndexedJoin' -v ./tests/integration` | PASS,2.170s | Indexed/fallback LEFT JOIN equivalence, equal-value cursor continuation, live NONE field scope with an existing cursor, and rejection of a cursor reused for a different projection. |

Independent full repository suite, browser, write-load gate, remote CI and production deployment: **NOT RUN** in this review. The implementer reported full integration FAIL220.100s with three failures, check/unit/frontend/build PASS, write workload PASS61.302s and browser five tests PASS7.3s. These are attributed implementer results, not independent executions.

## Numeric correctness and security

Independent probes established:

- Registration creates BUILDING state; copy and verification checkpoints remain bounded. Replayed checkpoints observe progress without duplicating advancement.
- An inserted record whose UUID lies behind both copy and verification cursors remains present after READY. An update during VERIFYING retains exact18-digit fractional money. Complete ASC/DESC pages, nulls, ties and cursors match the fallback over the same records.
- Deliberately corrupting a copied projection in the private fixture causes409 during verification, leaves its durable checkpoint unchanged, and recovers after the fixture projection is repaired. Audit failure rolls registration/backfill facts back in repository regressions.
- Actual waiting concurrent UPDATE and DELETE cause an older repeatable-read builder to abort40001; it cannot overwrite a newer projection or resurrect a deleted record. A pre-registration repeatable-read writer is fenced.
- Integer/decimal/money checks, source/schema witness rejection, direct unauthorized management SQL, worker column-grant drift and restrictive RLS passed. Disabling the projection trigger makes readiness fail; restoring it recovers readiness.
- Compatible adopted dataset bindings preserve numeric index identity. READY index joins use exact tenant/app/physical revision/hash/entity/field/record keys and row-version equality. SQL entity/field/row scopes, filters and cursor predicates remain before LIMIT. LEFT JOIN target scopes remain in the target join predicates.

Migration32's registry `FOR NO KEY UPDATE` avoids the previously observed source-row/projection-FK lock cycle. Migration34 verifies one bounded materialized source batch against projections instead of a separately planned lookup per row. Registration serializes quota admission under the physical anchor's exclusive lock, and increments its epoch before committing the new index. Application credentials lack projection/index mutation grants; workers lack generic reads and new-table grant drift fails closed.

## Isolated performance observation

The fixture independently asserted the existing reference tier: PostgreSQL18.6, four CPUs,8GiB memory, swap disabled, image `sha256:29754c7520f4b9654cf8ff98e19ac73b88e255c3a53f6236ebe9d52249bb6df3`. Dataset/client/sample sizes and thresholds were unchanged. Ten populated tenants include a100,000-record hot namespace.

| Measurement | Independent observation |
|---|---:|
| Unindexed SQL key p95,100 samples |0.444ms|
| Unindexed SQL numeric p95,100 samples |84.485ms|
| Unindexed SQL selective p95,100 samples |49.234ms|
| Copy and verify100,000 records |203 checkpoints;10.122s|
| Indexed HTTP,10 clients/100 samples |p50=48.382ms;p95=96.713ms;p99=116.989ms|
| HTTP throughput/errors |171.33requests/s;0errors|

Expected IDs, values, currency, ordering and cursor were asserted. The generic runtime plan assertion found the101-row numeric index page and bounded source-record fetches. Statistics were refreshed by the fixture after index completion; the build/verification itself ran before this refresh. This observation passes the existing750ms HTTP p95 gate for this configured numeric workload. It does not certify all fields, filters, joins, statistics states, hosts or production workloads. Earlier current-host read-gate failures remain historical evidence.

## Remaining regression findings at58c58ae

### MEDIUM: rejected workflow operation carries preview denial classification

`internal/platform/access/workflows.go:245` returns a typed lifecycle rejection without workflow evidence. The parent fresh-denial branch in `internal/platform/access/access.go:216` selects `workflows.rejected` only when evidence.Resource is `workflow`; the inherited parent resource remains `preview`.

The permanent `TestM9RejectedMutationsRollbackBeforeHTTPResponse` and independent diagnostic both reproduced HTTP400, zero workflow bindings/history/outbox/jobs, zero `workflows.rejected` evidence, and one denial labeled `preview.read.rejected`/`preview`. Atomic business rollback works; the provenance classification is incorrect. This code predates the numeric diff but prevents declaring the full regression gate passed.

Recommended correction: retain bounded trusted workflow resource/action context when mapping typed lifecycle failures, preserving actual denied policy evidence where supplied. Re-run the permanent assertion without weakening its expected action.

### LOW: fresh-migration TRUNCATE fixture no longer reaches its intended trigger

`tests/integration/identity_policy_test.go:717` truncates only metadata heads/versions. Migration31 adds a numeric-index FK referencing metadata versions, so PostgreSQL can refuse the statement before its expected immutability trigger executes. The original root failed; a temporary overlay using CASCADE solely in its uniquely owned empty disposable database passed the full fresh-migration root and the unchanged immutable-error assertion.

Recommended correction: update the owned empty fixture's FK closure/CASCADE statement. Never apply this probe to shared/customer records. This is a fixture regression, not an observed metadata mutation bypass.

### Expired-lease classification: timing-dependent regression retained

The implementer's full run observed42501 from audit RLS in `TestM9ExpiredLeaseOnEarlySaveAndConcurrentSignals`; the reviewer independently ran the exact root once and it passed5.67s. Both observations stand. R1 authority expires during authorizer/source/audit work; a late audit can fail closed before the older expected `workflow.ErrLease` path. No permission broadening is justified by the test failure.

A proposed typed normalization must occur only after rollback, use a fresh exact-ticket authority recheck, and classify lease loss only when that recheck proves the ticket invalid. Other42501/P0001 errors must retain their real failure. This correction and deterministic invalid-ticket regression were **NOT RUN** at this checkpoint.

## Limits and handoff

Numeric indexing is explicit owner/admin management, with eight fields/entity and32/physical namespace; JSONB remains primary. It does not implement metadata uniqueness, aggregate acceleration, text/date/search plans, schema evolution/new generations, general adoption completion, production authorization or service isolation. Operator corruption after a verified prefix is outside the application-maintained index invariant; operator recovery remains a reviewed forward action.

The root's pre-fix deadlock, full verification timeout, per-row lookup timeout, unapplied migration33 syntax failure and fixture mistakes remain in its implementation report. This reviewer did not execute those historical failures and does not relabel them as independent findings.

All independent workloads stopped and CPU/database ownership was returned to the implementer after the final join probe. **R3b numeric checks PASS; overall regression acceptance remains PARTIAL pending the findings above.**

## Correction review at0d08d0a

Independently inspected correction checkpoint `0d08d0a1ce5376a84a286f1f0f9b48211766d65c` after the implementer released testing. Numeric code, SQL planner and migrations31–34 are unchanged; the earlier isolated performance observation is retained without repeating the workload.

Executed:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-correction-review/overlay.json -run '^TestIndependentR3BRecheckCancellation|^TestM1FreshDatabaseMigrations$|^TestM9ExpiredLeaseOnEarlySaveAndConcurrentSignals$|^TestM9RejectedMutationsRollbackBeforeHTTPResponse$|^TestM9RWorkerSecurityErrorClassification$' -v ./tests/integration > /tmp/r3b-correction-review/results-0d08d0a.txt 2>&1
```

Combined result: **FAIL,21.506s**, solely the independently authored cancellation control described below. The four repository roots independently passed: fresh migrations0.95s; expired-lease/concurrent signals6.30s; rejected-mutation/denial evidence7.26s; worker security-error classification5.28s. Classification subtests executed real42501 and P0001 with a valid ticket and actual protected audit access after expiry. Existing zero-effect assertions and exact `workflows.rejected` expectations were retained.

The MEDIUM workflow provenance finding and LOW migration fixture finding are **CLOSED** at0d08d0a. Workflow evidence now retains bounded trusted workflow resource/ID; independent policy denial evidence remains preserved. The CASCADE probe remains confined to a uniquely owned empty disposable database. Native policy version `m9r-native-v2` identifies new index-management decisions without rewriting historical evidence.

### New LOW: cancellation is incorrectly classified as lease expiry

`internal/runtime/workflow/execution.go` rechecks42501/P0001 after rollback through `jobs.WithJob`; however, `jobs.WithJob` returns `ErrJob` for `CheckRole` query failures, including a cancelled caller context. That result does not establish persisted ticket revocation.

The independent control began with a valid claimed lease, executed an actual forbidden `SELECT custom_data FROM runtime.records` returning42501, and cancelled the caller context before revalidation. Worker Step returned `workflow.ErrLease` instead of the original permission error: **FAIL1.64s**. This is error/provenance masking, not an authority expansion or observed business mutation. Valid-ticket, uncancelled42501/P0001 controls passed. The failed assertion occurred before its subsequent business-count assertion; this particular probe therefore does not independently claim that count assertion ran.

Recommended correction: a narrow post-rollback authority-validation contract must preserve cancellation/query/role-drift validation errors and map lease loss only from a successfully evaluated exact-ticket false result. It must not replay business effects or broaden RLS. The implementer plans this correction; its execution is **NOT RUN** in this supplement.

The implementer separately reported resumed full `make check test build` PASS, integration254.958s and migration status1–34. These are attributed results. Independent numeric acceptance remains PASS; the cancellation-classification LOW remains open. All reviewer tests stopped and CPU/database/source ownership was returned before the implementer began its next correction.

## Final closure at5fdb7f1

Independently reviewed stable `5fdb7f1e78e547d3b6e5fcd5340fd41110ced437` after testing was released. `jobs.TicketAuthority` now returns `(bool,error)` and retains cancellation/database/catalog/unsafe-principal/grant failures as errors. It checks the exact persisted ticket through the protected SQL binding function. Worker Step maps lease loss only when this validation completes successfully and returns false; it otherwise retains the original42501/P0001. The fresh transaction follows rollback and runs no business callback. Successful binding retains existing begun/private-context bookkeeping, rather than claiming a completely side-effect-free SQL operation. Numeric query code and migrations remain unchanged.

Executed:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-correction-review/overlay.json -run '^TestIndependentR3BRecheckCancellation|^TestM1FreshDatabaseMigrations$|^TestM9ExpiredLeaseOnEarlySaveAndConcurrentSignals$|^TestM9RejectedMutationsRollbackBeforeHTTPResponse$|^TestM9RWorkerSecurityErrorClassification$' -v ./tests/integration > /tmp/r3b-correction-review/results-5fdb7f1.txt 2>&1
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/r3b-correction-review/overlay.json -run '^TestIndependentR3BTicketAuthority' -v ./tests/integration > /tmp/r3b-correction-review/authority-controls-5fdb7f1.txt 2>&1
```

Both commands **PASS**:22.835s and2.416s respectively. The former independent failure now passes1.39s and reaches its zero-business-effects assertion. All four permanent roots pass, including the added real42501-then-cancellation regression, valid uncancelled42501/P0001 controls, expired protected-audit access, exact workflow denial evidence and fresh migration immutability probe.

The separately authored direct-authority control established successful true for a valid ticket, successful false after persisted expiry, and errors for a cancelled context, absent database, closed pool, application-role database and unsafe worker column-grant drift. Grant drift was applied/revoked only in the disposable fixture; its business-count assertion passed. No authority was broadened to obtain these outcomes.

**Cancellation-classification LOW: CLOSED. Workflow provenance MEDIUM and migration fixture LOW remain CLOSED. No unresolved finding from this R3b review remains.** The original failures remain above. The ordinary numeric performance PASS was not repeated because no query/migration change followed it. The implementer's prior complete check/test/build PASS254.958s and final check/unit/build PASS are attributed evidence; an independent full suite at5fdb7f1, remote CI, main merge and production deployment remain **NOT RUN**. All reviewer processes have stopped and CPU/database ownership was returned to the implementer.
