# Performance Budgets

Measure separately:
normal CRUD p50/p95/p99; dynamic query p95/p99; compiler latency; workflow enqueue/action latency; queue lag; tool latency; DB CPU/IO/connections; cache hit; memory/goroutines.

AI authoring latency is not allowed to block ordinary ERP transaction paths.

Set numeric SLOs after representative baseline/load testing rather than inventing them.
