# ADR025: Evaluate tenant membership once per statement

Status: accepted for implementation, 2026-10-13.

The reviewed runtime policy checks membership for each scanned record. At 100k
records, a custom numeric sort exceeds the existing 2 s statement budget even
though the same physical scan without that repeated work takes tens of ms.

Keep forced RLS, tenant predicates and live IAM membership checks. In app-role
tenant policies, compare the row tenant to an uncorrelated `current_tenant_id()`
subquery and validate `tenant_matches_scope(current_tenant_id())` in another
uncorrelated subquery. PostgreSQL InitPlans evaluate those predicates once per
statement. STABLE by itself does not guarantee that behavior. This is not a
cache across transactions. Missing identity/tenant and revoked membership still
deny. REPEATABLE READ retains its existing snapshot revocation semantics.

Change only the app tenant policies installed by the platform helper and the
equivalent team predicates. Worker capability checks remain resource/lease
checks, without substituting app membership for a worker grant. Update the
installer for future tables. Preserve owners, grants, keys and FORCE RLS.

## Reference workload chosen before testing

Use the pinned local PostgreSQL 18 image limited to 4 CPUs and 8 GiB. Retain the
existing 2000 ms statement and 500 ms lock timeouts. Record actual image digest,
memory/cache settings and host CPU model. Use ten populated tenants, 100k records
in the hot dataset and 10k in each other tenant, with representative JSONB text,
money, status, owner/team/share data. Warm three times, then measure at least 100
queries per SQL case and 100 HTTP requests across ten clients. Report p50, p95,
p99, errors and throughput. Warm performance does not establish cold-start,
larger-scale or production capacity.

Latency acceptance uses the ordinary Go build used by the service. Race builds
remain correctness checks; race instrumentation overhead is reported separately
and is not a production latency measurement. The initial race-instrumented run
passed SQL gates but failed the HTTP gate (p95 838.674 ms); this failure remains
in the evidence. The same workload/resource limits and unchanged targets must
be tested with the service's ordinary build, rather than raising a budget.

The plan's unchanged gates are SQL p95 ≤100 ms for a key page and ≤500 ms for
numeric sort/selective filter; HTTP p95 ≤750 ms. Capture EXPLAIN ANALYZE/BUFFERS
and require membership InitPlan `Actual Loops=1`. Execute missing/foreign/stale
context, pooled reuse, DataScope and membership revocation negatives. The
previous report's exact hardware numbers are not the new reference baseline.

R3a can precede R2 because it changes neither data identity nor release layout.
R2 continuity and R4 concurrency remain required. Governed projection indexes,
uniqueness and aggregate expansion are separate R3b–R3d gates, not claims of this
change. No timeout increase or disabling RLS is an implementation remedy.

## Recovery

Use an additive migration. Failed migration rolls back atomically; deployed
recovery uses a reviewed forward policy change with tenant/worker denial tests.
No data copy or destructive operation is involved. Reverting this optimization
must preserve the same authorization and acknowledge the former performance
ceiling.
