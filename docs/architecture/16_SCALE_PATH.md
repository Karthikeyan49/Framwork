# Scale Path

1. Single Go backend process + PostgreSQL + Redis + object storage.
2. Separate API and worker entrypoints from the same codebase.
3. Partition workers by workload/tenant.
4. Extract only measured independent scaling/failure domains.
5. Offer dedicated DB/workers for high-isolation enterprise tenants.

Split only when independent scaling, failure isolation, stable team ownership, release cadence or profiling justifies it.
