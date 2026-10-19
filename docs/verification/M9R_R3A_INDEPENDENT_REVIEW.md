# M9R R3a independent review

Date: 2026-10-13. Reviewer: independent Codex review agent.

**Current verdict: PARTIAL for R3a at `42e1f26`.** Focused correctness and security
regressions pass, with no confirmed unresolved BLOCKER, HIGH or MEDIUM code-safety
finding. The independently executed current-host HTTP performance gate fails:
p95=899.709ms exceeds the unchanged750ms target. That acceptance gap remains open.
The earlier PASS at `376eb9c` is retained below as historical AMD-host evidence,
not a guarantee on the resumed Intel host. This is not acceptance of R2, R3b–R3d,
R4, production capacity or the complete M9R plan.

## Scope and findings

Reviewed migration27, the retained identity/tenant/worker predicates, installer,
ADR025 and correctness/performance fixtures at858b9b0,304e17e and376eb9c. RLS source
has not changed since ec9ab04; the later commits strengthen evidence. Followed
AGENTS.md and the two-agent protocol. Implementation remained read-only; the sole
repository edit by this review is this report. All test mutations were owned
fixtures; no shared service restart or Redis reset was performed.

The app policy still requires row tenant equality to trusted current tenant and
valid live actor/membership/tenant state. Moving those expressions into
uncorrelated subqueries makes membership a statement InitPlan, rather than
removing authorization or caching it across requests. Missing context remains
denied. Repeatable-read retains its existing snapshot semantics. Ownership,
grants, keys and forced RLS remain unchanged. The migration-only installer applies
the same policy to future tables and keeps runtime execution revoked.

Worker capability authority was not converted into app membership authority.
Its exact audience/job/lease checks, volatile expiry checks and restrictive
no-generic-data policies remain in force. Team hierarchy behavior is preserved.
This equivalence relies on trusted Go setting context before each statement;
customer metadata cannot execute SQL or mutate that context mid-statement.

Review identified and closed four evidence weaknesses before final acceptance:

- A foreign-tenant zero-row test initially used an empty tenant. It now populates
  and proves legitimate access to B before attempting forged access from A.
- The revocation test initially omitted actor_id, so missing identity explained
  denial. It now authenticates/binds the actor and asserts valid actor/tenant
  context before checking revoked membership.
- The EXPLAIN assertion initially accepted any InitPlan. VERBOSE output now
  identifies `iam.tenant_matches_scope(iam.current_tenant_id())` specifically and
  requires Actual Loops=1.
- HTTP samples initially discarded bodies, and key SQL sorted the `id::text`
  output alias. Samples now verify100 expected IDs, amounts, currency, names,
  order and a continuation cursor. Final SQL qualifies `r.id`, preserving UUID
  ordering and the actual physical-key path. Earlier text-sort measurements are
  not presented as the final key-path evidence.

These were source/evidence findings, not independently reproduced tenant escapes.

## Independently executed correctness checks

| Exact command | Result |
|---|---|
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(StatementMembershipRemainsFailClosed\|ReadinessWithGenericPreparedPlans\|RepeatableReadCannotRestoreReplacedLease\|GenericGrantDriftFailsReadinessAndStillDeniesRawData\|PopulatedTenantsAndAuthenticatedAudience)$\|^TestM5\|^TestM6R(SQLDataScope\|Nullable)' -v ./tests/integration` | PASS13.783s at304e17e;13 roots actually executed, including seven M5 roots, owner/team/hierarchy/share/condition DataScope and five worker/context/generic-plan roots |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m9r-r3a-review/overlay.json -run '^TestIndependentR3A' -v ./tests/integration` | PASS2.032s at304e17e; independent disposable-database probe |

The independent overlay, `/tmp/m9r-r3a-review/probe_test.go`, reused one connection
and generic prepared count query while switching valid A/B actors repeatedly
within one transaction, with both tenants populated. Each statement returned only
its selected tenant. It also established a repeatable-read snapshot, revoked
membership in another connection, confirmed the existing snapshot retained its
documented view, and confirmed a new transaction with a valid authenticated actor
denied the revoked membership. Unbound pooled reuse returned zero rows. The
overlay is a temporary session artifact, not repository implementation or a
claim that the old weak fixtures failed when executed.

## Independently executed performance gate

Exact command, executed alone after correctness tests stopped:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -run '^TestM9RMembershipPerformanceFloor$' -v ./tests/integration
```

- At304e17e: PASS32.485s. Key text-sort p95=58.869ms, numeric96.551ms and
  selective61.732ms. HTTP p95=579.738ms,
  p99=694.711ms,20.87 requests/s,0errors. This run exposed the ordering-alias issue;
  its key result is superseded below.
- At376eb9c with qualified UUID ordering: **PASS27.622s**. This is the accepted
  historical AMD-host evidence; the current-host supplement below supersedes
  its acceptance status for the resumed environment.

| Final workload | Samples | p50 ms | p95 ms | p99 ms | Throughput/s | Gate |
|---|---:|---:|---:|---:|---:|---|
| UUID key SQL page |100|0.434|0.510|0.522|278.49|PASS ≤100ms p95|
| Numeric SQL sort |100|82.021|95.441|113.052|11.29|PASS ≤500ms p95|
| Selective SQL filter/sort |100|48.742|60.730|74.558|18.04|PASS ≤500ms p95|
| Numeric HTTP,10 concurrent clients |100|477.148|658.923|706.252|20.50|PASS ≤750ms p95|

All final samples had zero errors. Each workload had three warmups. SQL timings
measure statement fetch; their throughput includes surrounding transactions.
HTTP uses the ordinary Go build and the real handler/auth/policy/audit path in an
httptest server; its timing also includes decoding and checking the returned page.

The gate verified the connected cluster against the constrained Docker instance,
not a caller-supplied tier label: PostgreSQL18.6,4 CPU quota,8GiB memory, swap
disabled, image `sha256:29754c7520f4b9654cf8ff98e19ac73b88e255c3a53f6236ebe9d52249bb6df3`.
Settings were shared_buffers128MB and work_mem4MB. Ten tenants contained190k
records:100k hot and10k in each other tenant, average hot row761bytes, with mixed
ownership/team/share facts. Statement/lock budgets stayed2000/500ms.

Read-only host inspection after the run showed AMD EPYC9V74,5 CPUs visible,
ambient cgroup quota4 CPUs and32GiB memory. The Go HTTP fixture runs on that host,
outside the independently limited PostgreSQL container. No production topology,
ingress/TLS or dedicated API-resource tier is inferred from this result.

Final VERBOSE EXPLAIN showed the membership-specific Result InitPlan with
Actual Loops=1, scanning100k authorized rows and filtering90k other-tenant rows.
The numeric plan still uses a sequential scan/top-N sort; this does not establish
governed custom-field indexes or large-scale query guarantees.

The old policy timed out with SQLSTATE57014 in the first independent run, but
completed1949.209ms in the final run. Therefore a categorical claim that it
always times out is unsupported; workload/cache/hardware variation is explicit.
The implementer's earlier race-build HTTP failure and mistaken timeout assertion
were reported to this reviewer but were not independently executed here. They
must remain distinct from the ordinary-build gates above.

## Limits and handoff

No unresolved code-safety blocker was found for this isolated optimization.
It does not fix stable dataset identity, dataset-wide writer serialization,
custom role/policy administration, verifier authority, retention or workflow
limits. R2/R4 and later authority/operations gates remain required. R3b–R3d
indexes/projections, uniqueness and aggregate expansion are not implemented by27.

NOT RUN by this reviewer: the complete repository/remote-CI suite at376eb9c,
browser/live smoke, race-instrumented performance, cold-cache guarantees,
long-duration/open-loop load, larger/complex metadata distributions, production
deployment or an OS-isolation test. A100-request warm closed-loop workload is a
bounded reference floor, not production certification. The historical benchmark's
owned database was cleaned up, and no reviewer process/service remained running
after that review.

## Resumed-host review at42e1f26

The host changed to Intel Xeon Platinum8573C after environment resumption. This
review inspected the stable query change in `internal/runtime/data/query.go:373`
and its fixtures before executing tests. Source and tests remained unchanged
during both independent runs. No R2 code or data adoption was involved.

For custom non-core sorts outside the worker adapter, a MATERIALIZED CTE selects
only ID and typed sort value before the wide record fetch. Tenant/app/revision/
IR/entity namespace, entity and used-field DataScope, user filter and cursor
predicates all remain inside that selection, before LIMIT. The outer fetch joins
the selected IDs with the same namespace. Both stages use the same statement
snapshot and forced RLS. Joined-target entity/field scopes remain in the LEFT
JOIN ON predicate; the namespace-inclusive unique record key prevents join
multiplication. Core-ID and worker paths are unchanged. Review found no
authorization, pagination or join regression in this change.

### Independently executed current-source commands

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9R(CustomSortPagePreservesTiesNullsScopesAndJoins|StatementMembershipRemainsFailClosed|ReadinessWithGenericPreparedPlans|RepeatableReadCannotRestoreReplacedLease|GenericGrantDriftFailsReadinessAndStillDeniesRawData|PopulatedTenantsAndAuthenticatedAudience)$|^TestM5|^TestM6RSQLDataScope|^TestIndependentM6RNullable' -v ./tests/integration
```

**PASS29.326s:**15 test roots actually executed: seven M5 roots; nullable-condition
denial; owner/team/hierarchy/share/condition DataScope; custom-sort ASC/DESC ties,
NULLs, continuation pages, declared joins, hidden sort-field denial and
conditional row selection before LIMIT; statement membership/revocation;
populated tenant/audience isolation; stale repeatable-read lease fencing;
forbidden grant drift; and API/worker/dispatcher generic-plan readiness.

After that process exited, with the implementer keeping CPU/database tests idle:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,performance -count=1 -run '^TestM9RMembershipPerformanceFloor$' -v ./tests/integration
```

**FAIL40.142s** (test body40.12s), exit1. HTTP p95 exceeded its target. No latency
threshold or statement/lock budget was increased, and no second run was selected
to replace this failure.

| Current Intel-host workload | Samples | p50 ms | p95 ms | p99 ms | Throughput/s | Result |
|---|---:|---:|---:|---:|---:|---|
| UUID key SQL page |100|0.471|2.778|4.487|104.95|PASS ≤100ms p95|
| Numeric SQL sort |100|96.247|215.011|314.237|7.96|PASS ≤500ms p95|
| Selective SQL filter/sort |100|64.860|137.407|147.154|11.69|PASS ≤500ms p95|
| Numeric HTTP,10 concurrent clients |100|447.373|899.709|995.374|17.00|FAIL >750ms p95|

Every workload returned zero errors. The benchmark validated expected response
IDs/order/amount/currency/name and cursor; successful HTTP status alone was not
the correctness criterion. The connected PostgreSQL18.6 cluster/container was
verified at4 CPU quota,8GiB memory, swap disabled, the same pinned image listed
above, shared_buffers128MB, work_mem4MB,190k records across ten tenants with100k
hot records averaging761bytes, and three warmups. Read-only `lscpu` plus
`cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/memory.max` after the gate confirmed
Intel Xeon Platinum8573C,5 visible CPUs, host quota400000/100000 and32GiB memory.
`git rev-parse HEAD` confirmed42e1f260de974bfb9f8c8588a119841f5d780b77; the
reviewed query and performance-test files had no working-tree diff.

The actual runtime generic-plan diagnostic independently confirmed
selected_page width48,101 selected rows and101 `records_pkey` index searches
with the exact tenant/app/revision/IR/entity/ID key. Membership-specific InitPlans
each had Actual Loops=1. Thus the narrow-fetch strategy is present, but its
presence does not establish the HTTP target. The original-policy comparison
returned SQLSTATE57014 in this run; the earlier1949.209ms completion above still
prevents a categorical claim that it always times out.

### Failures and variability retained

The implementer reported the following Intel-host executions; this reviewer did
not independently execute those runs and does not substitute them for the
independent failure above:

- Before narrow fetch: ordinary HTTP p95=1485.555ms, FAIL; profiled HTTP
  p95=858.323ms, FAIL, with Go CPU2.53s over32.54s elapsed.
- First narrow-fetch run: HTTP p95=901.114ms, FAIL.
- Later runtime-plan-instrumented ordinary run: HTTP p95=658.981ms,
  p99=725.121ms,21.80 requests/s,0errors, PASS30.538s.

These results, the historical AMD PASS, and the independent Intel FAIL show
variation between runs and hosts. This review did not establish how much came
from hardware, cache state or scheduling. A passing sample does not cancel a
failing gate. Current-host R3a performance acceptance remains incomplete until
the unchanged bounded target is met with representative, retained evidence.
Do not loosen RLS/DataScope, timeout budgets or the threshold to relabel this
run. Broader R2–R4 gates remain incomplete independently of this performance
result.

NOT RUN in this supplement: complete repository/CI/browser/live suites, the
earlier temporary overlay (not present after resumption), new raw-worker exploit
campaigns, cold-cache/open-loop/long-duration load, production or process
isolation. Existing raw-worker regressions named above were executed; this is
not a new full R1 acceptance. Both independent processes finished and the owned
fixture cleanup ran. No reviewer service remains running, and CPU/database test
ownership was released to the implementer.
