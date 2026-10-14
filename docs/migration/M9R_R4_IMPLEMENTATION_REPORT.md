# M9R R4 local command remediation progress

Report date: 14 October 2026 (IST); captures 13–14 October 2026 (UTC).

**R4 STATUS: PARTIAL — the write performance gate failed; independent review is pending.**
The local preview command boundary is implemented and root correctness/fault/
browser checks passed. This does not accept the full foundation, production,
metadata uniqueness, full schema evolution or the failed R3 HTTP floor.

## Changes and boundary

Ordinary writes no longer increment the single physical dataset counter. Shared
binding/anchor gates preserve adoption fencing; row CAS and tenant-inclusive
reference FKs protect concurrent writes. Old JSON reference scans remain for
retained legacy records. Full replacement edits to one record version conflict.
No records, counters or customer data were deleted.

API-local commands buffer their bounded body/response and retry at most three
times within four seconds after proven transaction aborts only. Each attempt
verifies the token and resolves live identity/membership/policy. PostgreSQL
serialization/deadlock COMMIT rejections are distinguished from lost/transport
outcomes. Unknown COMMIT never retries automatically. FK/lock conflicts roll
back the whole transaction and audit a defined rejection in a fresh live scope.

Additive migration 30 creates immutable, tenant-and-actor-scoped command receipts.
The exact request fingerprint binds operation/values/expected version, entity,
execution release and physical dataset/binding. Key mismatch is 409. Same-key
replay returns historical ID/version with empty values, no new business event or
workflow trigger. Mutation, receipt, business audit and outbox commit atomically.
Receipt PK collision after an RR wait rolls everything back before fresh retry.

`GET /v1/tenants/{tenant}/preview/{app}/commands/{command_key}` returns the requesting
actor's COMMITTED facts or UNKNOWN under live policy plus actor-restrictive forced
RLS. UNKNOWN does not establish abort. Receipts survive record deletion, cannot be
updated/deleted/truncated, and contain no customer value snapshot. Worker generic
access remains denied and drift checks cover receipts. Business audit includes
the command UUID; replay/request evidence is distinct from a business fact.

React supplies a UUID per intent and retains it before dispatch in an actor-scoped
journal above the workspace component. In-flight workspace selection is disabled;
disconnect clears credentials while retaining recovery identities for that actor
within the same console/tab. Other accounts cannot see those keys. Explicit
receipt lookup plus a fresh read releases a committed write gate;
UNKNOWN and record reload alone keep it blocked. The browser fault test lets the
actual server commit, discards the HTTP response and reconciles without another
write. Recovery survives same-tab account/workspace switches; browser reloads and
restarts still require keeping the key beforehand. Legacy keyless callers remain compatible but
cannot claim durable command reconciliation. The supplied UI always sends keys.

Decision: [ADR027](../adr/027-m9r-local-command-concurrency-and-reconciliation.md).
No new dependencies or runtime privileges are introduced.

## Commands actually executed

| Command | Result |
|---|---|
| `./scripts/go.sh fmt ./internal/platform/access ./internal/platform/postgres ./internal/runtime/data ./internal/policy` | PASS |
| `./scripts/go.sh test -race -count=1 ./internal/platform/access ./internal/platform/postgres ./internal/runtime/data ./internal/policy` | PASS; access initially had no unit tests; retry classification tests added afterward |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(Command\|IndependentWriters\|KnownSerialization\|UnknownCommand\|ReceiptMutation)' -v ./tests/integration` | Initial FAIL 6.572 s: new count helper guessed `audit.events`; corrected to existing `audit.entries`. PASS 7.728 s after correction |
| `npm --prefix apps/web run typecheck && npm --prefix apps/web test -- --reporter=dot` | Typecheck PASS; initial 3/28 test FAIL: old body/error/reload expectations contradicted new key/reconciliation behavior. Updated meaningful assertions verify UNKNOWN blocks and COMMITTED recovery; all 28 PASS 5.06 s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(Command\|IndependentWriters\|KnownSerialization\|UnknownCommand\|ReceiptMutation\|ConcurrentSameKey\|ActualDeadlock\|ConcurrentReference)' -v ./tests/integration` | PASS eight roots 24.950 s; real overlapping transactions, eight same-key clients, real server deadlock, 12 parent/reference races, known serialization abort, post-COMMIT reply loss, audit rollback and owned receipt isolation |
| `make check test-unit build` | PASS: Go format/vet/modules/race units/import boundaries/backend builds; source checksums 84+43; Node 5/frontend 28 tests; lint/typecheck/frontend build |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM5\|^TestM6R\|^TestM9R(Command\|IndependentWriters\|KnownSerialization\|UnknownCommand\|ReceiptMutation\|ConcurrentSameKey\|ActualDeadlock\|ConcurrentReference\|Dataset\|GenericGrantDrift\|ExactCapability\|ReadinessWithGeneric)' -v ./tests/integration` | PASS 93.890 s; retained CRUD/query/CAS/DataScope/release contracts, new command faults, all ten dataset roots including stale RR adoption, and exact worker/role-drift/generic-plan readiness |
| `git diff --check && ./scripts/go.sh vet ./internal/platform/access ./internal/platform/postgres ./internal/runtime/data && make migrate && ./scripts/go.sh build -o bin/api ./cmd/api` | PASS: additive migration 30 applied locally; disposable fixtures also apply/repeat the complete migration chain |
| `make test-e2e` | Initial FAIL: 4/5 PASS 15.2 s; new recovery test completed UI reconciliation then called unsupported GET on the redacting test helper. Added redacted GET. Final five browser tests PASS 13.3 s |
| `npm --prefix apps/web run lint && make test-e2e` | PASS lint and five browser tests including actual committed response loss/one record/one write, existing CRUD/stale versions/read-only policy/redaction/mobile checks |
| `node scripts/verify-docs.mjs` | PASS: 84 V2 and 43 V3 source checksums unchanged |
| `make test-write-performance` | FAIL 62.688 s: 32 clients ran for 60.260 s, with one HTTP 503. The test failed before reporting latency percentiles or final committed-fact counts. No performance acceptance is claimed. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RFinalReadRejectionRollsBackEveryCommandFact$\|^TestM9R(Command\|IndependentWriters\|KnownSerialization\|UnknownCommand\|ReceiptMutation\|ConcurrentSameKey\|ActualDeadlock\|ConcurrentReference)' -v ./tests/integration` | PASS 16.493 s: nine roots, including real restrictive SELECT-policy final-read failure under normal app credentials; keyless and receipted commands leave zero records/receipts/business audit/outbox and one fresh denial |
| `npm --prefix apps/web run typecheck` and `npm --prefix apps/web test -- --reporter=dot` after the recovery fix | PASS typecheck and 29 tests in 4.67 s; new disconnect/account/workspace/UNKNOWN/COMMITTED test retains one intent and never dispatches a second write |
| `npm --prefix apps/web run lint` after the recovery fix | PASS |
| `make test-write-performance` with bounded safe diagnostics after the rollback/recovery fixes | FAIL 61.838 s: 32 clients, 60.268 s, 6,471 successful writes, zero errors, p50 234.133 / p95 701.506 / p99 1,041.293 ms; 107.37 writes/s. Exact row-version, receipt, business-audit and outbox counts passed. Pool waited 6,461 times for 1,316.015 cumulative seconds; no audit error category. The 500 ms target remains failed |

## Performance and acceptance

`make test-write-performance` runs an ordinary Go build on the verified ADR025
PostgreSQL container (4 CPU, 8 GiB, swap disabled). Thirty-two HTTP clients update
distinct records for at least 60 seconds; each request has a stable command UUID
and expected row version. It checks exact returned ID/version and final version
sum plus receipt/business-audit/outbox counts. Target remains p95 ≤500 ms, with
no visible failure. The executed run failed with one HTTP 503. Its cause is not
yet established; latency percentiles and final fact-count assertions were not
reached. Do not interpret the passing focused correctness tests as a passed
capacity gate or raise the target to hide this failure. A subsequent diagnostic
run completed without errors and preserved all fact counts, but p95 was 701.506 ms.
Large cumulative pool waiting was observed; it does not establish the original
503's cause. Capacity remediation and independent re-verification remain open.

## Independent findings and corrections

Independent review reproduced a HIGH rollback invariant failure: a simulated
final no-row read after normal-role INSERT returned HTTP 404 while committing a
record without its receipt/outbox. It is a late-read fault reproduction, not a
demonstrated Native-policy attacker exploit. Every trusted local-command 4xx now
forces rollback before fresh authorized denial evidence, including typed errors.
The permanent regression uses an actual restrictive SELECT policy rather than a
production executor injection seam. Denied field/row decisions are retained for
the fresh audit; rejection suffixes are not duplicated.

A MEDIUM frontend probe demonstrated same-login workspace navigation losing an
in-flight intent. The actor-scoped pre-dispatch journal and inflight navigation
guard address that unmount boundary; credentials are still cleared on disconnect.
The [independent report](../verification/M9R_R4_INDEPENDENT_REVIEW.md) retains
the pre-fix observations. Independent acceptance of the corrections is pending.

The review has not yet accepted this stable boundary. Metadata uniqueness/index
projection work remains R3b/R3c; no customer unique-field capability is inferred
from this change. R1 forbids generic worker writes, so the worker/user regression
verifies confinement rather than inventing a broad writer port. Full schema
evolution, production activation and R5–R7 product/verification/operations remain
open. A focused test selection is not the final complete foundation suite.

## Recovery and publication

Quiesce incompatible old writer protocols before rollout. Retain the anchor epoch,
all immutable bindings and receipts. Do not run a destructive Down migration,
delete receipts or restore worker data grants. Confirm COMMITTED through the owned
receipt before treating an unknown result as successful; UNKNOWN needs continued
lookup/operator investigation and does not permit blind resubmission. Apply a
reviewed forward fix if reconciliation/receipt guards are damaged.

Source, migration, tests and reports belong in Git; credentials, local fixtures,
logs, binaries, node_modules and generated builds stay ignored. The user requested
publication of the current work before the remaining diagnosis/review. This
checkpoint is explicitly partial and unaccepted; publication does not establish
completion. Main remains unchanged; M10 remains paused.
