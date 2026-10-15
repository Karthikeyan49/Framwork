# M9R R1 independent review

Date: 2026-10-13. Reviewer: independent Codex review agent.

**Verdict: PASS for the R1 database credential boundary at `7afc940`.** No
unresolved BLOCKER, HIGH or confirmed MEDIUM finding remains in the reviewed
scope. This does not accept the overall M9R plan, production deployment, hostile
process isolation, arbitrary ABAC, compiler/verifier authority or external tools.
R2–R4 and the later R5–R7 gates remain separate work.

## Scope and method

Reviewed the R0/R1 changes from `c6102c2`, the fixture checkpoint `2212d7e`, the
review correction `ddef21e` and final readiness correction `7afc940`. Sources
included migrations22–26, ADR024, the worker runbook/report, jobs, source adapter,
event audience/receipt handling, role checks, configuration and process
composition. AGENTS.md and the two-agent protocol governed this review.

Implementation stayed read-only. Independent test overlays were stored under
`/tmp/m9r-review-attacks/` and `/tmp/m9r-review-config/`; they did not replace
tracked files. Database cases used repository helpers that create uniquely named
disposable databases, validate their effective/connected fixture bindings and
clean up afterward. The secondary-service case provisioned and removed its own
principal/Redis receiver and stream. The reviewer did not restart shared services,
reset the shared Redis stream or alter customer records. The sole repository edit
by this review is this report.

The earlier validation report's original HIGH worker exploit was the remediation
target. This reviewer did not independently execute that historical exploit at
the old baseline during this turn. The reviewer did execute raw-credential attacks
and populated negative tests at the fixed checkpoints.

## Findings and closure

| Finding | Independent evidence | Resolution |
|---|---|---|
| Original HIGH: global worker claim/GUC authority allowed unrelated business access | Fixed-checkpoint raw self-claim requires SQLSTATE42501; generic SELECT/UPDATE/DELETE, forged selectors and two populated audiences denied | Dispatcher issuance, authenticated principal assignment, private backend/full-transaction context, exact lease fence and narrow source port; PASS |
| MEDIUM: same record UUID in another entity exposed its share row | At2212d7e, the overlay saw source share1 and unrelated-entity share1, expected1/0 | Migration26 adds bound entity ID; independent overlay and repository regression PASS |
| MEDIUM: valid secondary receiver configuration rejected | Independent config overlay failed with the dedicated sender/receiver error, although EventBinding supported that principal | Bounded receiver names and exact service binding; config overlay and actual secondary app.Run startup/execution/shutdown PASS |
| MEDIUM: dispatcher lost explicit workflow-disable flag | Direct Node assertion found undefined instead of `false` | Runtime environment now forwards the flag; assertion and five repository Node tests PASS |
| MEDIUM: API readiness failed with generic prepared plans | At ddef21e, a forced generic plan produced SQLSTATE42501, permission denied for worker_principal; events.Ready failed at call0 | Worker-only calls moved into the Go worker branch, without granting API their execution privilege; eight forced-generic API calls and all three role regressions PASS at7afc940 |
| Proposed correction would lose descendant-team resolution | Source inspection found direct-membership filtering conflicted with tenant.Store.Teams' descendant contract; this moving-code concern was not executed before removal | Optional narrowing removed; independent parent/child fixture requires both team IDs and PASS |
| Conditional temporary-catalog concern and generic-grant drift | No TEMP exploit claimed under shipped CONNECT-only worker grants | Migration26 sets pg_temp explicitly last, qualifies catalogs, adds restrictive no-generic policies and shared readiness checks; repository restored-grant raw SELECT/UPDATE denial PASS |

Two threat-model qualifications were also corrected. Same-user local development
can read its host credential files; environment filtering alone is not hostile
process isolation. ADR024/runbook/report now require service/secret isolation and
mark that deployment verification unrun. SQL SAVEPOINT rollback can restore an
earlier valid protected context; the independent probe confirmed it still selects
only the original masked source and expires. It does not grant another job or
extend the lease. The trusted wrapper aborts the whole transaction on bind failure.

## Commands actually executed

Results below are independent reviewer executions. Earlier implementer full-suite
results are not substituted for independent execution. Overlays remain temporary
session artifacts; corresponding repository regressions are named above.

| Exact command | Checkpoint and result |
|---|---|
| `./scripts/go.sh test -race -count=1 ./internal/platform/jobs ./internal/runtime/data ./internal/runtime/workflow ./internal/events ./internal/config ./internal/platform/app` | PASS at c6102c2 and ddef21e; jobs/app report no unit test files, rather than tested behavior |
| `./scripts/go.sh test -race -count=1 -overlay /tmp/m9r-review-config/overlay.json -run '^TestIndependentSecondaryWorkerConfig$' -v ./internal/config` | FAIL before correction; PASS1.019s at ddef21e |
| `node --test scripts/runtime-env.test.mjs` | PASS5 tests at ddef21e |
| `./scripts/go.sh test -tags=integration -overlay /tmp/m9r-review-attacks/overlay.json -run '^$' ./tests/integration` | Compile-only PASS before DB execution; zero tests, not behavioral verification |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m9r-review-attacks/overlay.json -run '^TestIndependentR1' -v ./tests/integration` | FAIL7.827s at2212d7e solely on share entity collision; masking/raw privilege/selector/SAVEPOINT/live-expiry case PASS. After correction, three roots including descendant teams PASS10.493s at ddef21e |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(Worker\|Exact\|Populated\|Expired\|Offline\|Repeatable)' -v ./tests/integration` | PASS11.860s at2212d7e; seven roots executed |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R\|^TestM8(ConcurrentClaimsConsumersAndWorkerIsolation\|LeaseFencingBoundedRetriesDeadLetterAndReplay\|AckAfterCommitAndDurableQuarantine)$\|^TestM9(UnknownCommitRetainsReceiptsAndNeverRepeatsLocalEffect\|ActualProcessKillBeforeAndAfterLocalCommit)$' -v ./tests/integration` | PASS72.065s at ddef21e;20 roots, including all R1 cases, real secondary service, grant drift, claim concurrency, fenced retry/DLQ/replay, ACK/quarantine, actual SIGKILL before/after commit and dropped successful COMMIT response. The prefix also selected record-trigger/readiness/rejected-mutation/retry regressions; these executed and passed |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m9r-review-attacks/overlay.json -run '^TestIndependentR1APIReadinessDoesNotNeedWorkerFunctionPrivileges$' -v ./tests/integration` | FAIL0.887s initially and FAIL0.928s with minimal42501 diagnosis at ddef21e; PASS2.046s at7afc940. The retained minimal old CASE still fails42501 as expected; actual events.Ready succeeds eight times |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(ReadinessWithGenericPreparedPlans\|SecondaryWorkerServiceStartsWithBoundAudience\|GenericGrantDriftFailsReadinessAndStillDeniesRawData)$' -v ./tests/integration` | PASS9.849s at7afc940; app/worker/dispatcher generic plans, wrong-role denial, actual secondary execution/shutdown and restored-grant confinement |
| `./scripts/go.sh test -race -count=1 ./internal/events` | PASS1.018s at7afc940 |

The dispatcher flag was independently tested before and after correction using
this exact inline command (FAIL before, PASS after):

```sh
node --input-type=module - <<'JS'
import assert from 'node:assert/strict'
import {runtimeEnv} from './scripts/runtime-env.mjs'
assert.equal(runtimeEnv({WORKFLOWS_ENABLED:'false'},'dispatcher').WORKFLOWS_ENABLED,'false','dispatcher must preserve explicit disable flag')
JS
```

## Acceptance and limits

Executed evidence establishes populated cross-audience denial, wrong exact ticket
fields, source/field confinement, denied generic business mutation, transaction
lifetime and expiry, stale REPEATABLE READ fencing, separate issuance/pickup,
offline-offer recovery, known/unknown commit handling, and legitimate secondary
worker lifecycle. Shared schema grants and rollout preserve records, pinned
artifacts, receipts and immutable evidence; no business-data migration was added.

The initial adapter retains one principal assignment per tenant, including
inactive assignments. Assigned principals process their tenant's issued jobs.
Tenant-scoped team hierarchy metadata remains available for descendant resolution;
this is not arbitrary ABAC or per-team metadata privacy certification. Membership
and assignment revocation follow the stated transaction snapshot/lease model,
with session termination required for immediate hostile-session cutoff.

Malicious progress/receipt fabrication within an authorized job remains possible
and explicitly documented. It cannot serve as independent human acceptance or
future high-risk tool authority. Custom policy issuer authority is R5;
compiler/verifier provenance is R6; outage budgets, retention, workflow cycles,
approval snapshot semantics and production operations are R7. Stable dataset
identity, query costs and write contention remain R2–R4.

NOT RUN by this reviewer: whole-repository Make/CI suite after7afc940, browser/live
smoke, production deployment, remote CI, actual hostile-process secret/OS isolation,
or a TEMP-enabled exploit. The implementer owns the remaining repository/operational
checks; this report does not claim them passed. R3a design was outside this review.
