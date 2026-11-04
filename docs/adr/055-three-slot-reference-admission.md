# ADR055 — Measure three admitted API transactions on the shared CPU tier

19 October 2026 (Asia/Kolkata). Accepted as an implementation/measurement
experiment after independent read-only review, before code. Capacity is NOT
accepted until executed gates pass. Supersedes ADR054's default4 choice only.

Default4 passes the unmapped indexed aggregate floor but the real100k migrated
canonical workload fails: current integer SQL/HTTP p95 353.140/887.348ms,
decimal371.763/1134.864, money553.856/1267.286, filtered698.319/1653.025,
OWNER539.429/1277.209 and SHARE620.084/1799.529. Requests return correct values
with zero errors through measured cases. The expanded test eventually exceeds
its12m total suite timeout during retained filtered warmup, so later cases are
NOT RUN; this is not a passing migration/capacity gate. Preserve the failure.
No autovacuum backend was present at the sampled observation; do not invent a
causal claim about background maintenance.

Change actual Config and API fallback default to3 while preserving strict
operator range1..10, FIFO128, pool10 and every query/lock/transaction/queue/latency
limit. The parent has four CPUs shared by API and Docker. Leaving CPU headroom
is a scheduling hypothesis to measure, not a performance or production claim.
Keep ten offered HTTP clients and all waiting time in the unchanged workload.
No hidden benchmark override, driver/global setting, RLS or SQL semantics change.

Update default documentation/tests, inspect canonical/current-share plans and
measure current and retained branches with differentiated values. Independently
review executable correctness and admission; run all retained read/write/unique
gates and browsers. If this fails, retain the result and investigate plans rather
than declaring the experiment accepted. Any suite-timeout increase must cover
added fixture/rehearsal work only; individual budgets cannot increase.

Recovery uses an explicitly measured operator setting or disables SQL_V1 pending
a forward fix. Existing data/releases/receipts and authorization are preserved.
