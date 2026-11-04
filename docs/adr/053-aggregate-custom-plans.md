# ADR053 — Keep certified aggregate execution on custom plans

18 October 2026. Status: accepted after independent read-only design review,
before source. R3d remains incomplete until executable review and all gates pass.

The unchanged 100k-row workload still fails. A direct probe of the same filtered
money statement and bindings with JIT off takes 182.565ms with a custom plan;
its generic plan hits SQLSTATE57014 after 2.012s. OWNER custom149.433ms and
generic153.273ms both pass, so this is not evidence that every tail has one cause.
Preserve the failed workload and diagnostic results in the implementation report.

Only the certified interactive SQL_V1 kernel captures the effective `jit` and
`plan_cache_mode`, then sets transaction-local `jit=off` and
`plan_cache_mode=force_custom_plan`. Restore BOTH prior values before returning
from every normal/non-SQL-aborted path, including invalid-input and budget errors.
Restoration failure suppresses an otherwise successful result. SQL-aborted or
cancelled operations return an error; WithRuntime rollback resets LOCAL state.
No global/persistent settings, pgx driver change, worker change, scan-method
forcing, timeout increase, RLS change or metadata configuration option is added.

Cross prior JIT on/off with all three plan modes, exercise repeated callback
queries, actual cancellation/restoration faults, result suppression and pool reuse.
SQL and complete HTTP latency include ordinary setting reads/restoration, policy,
FIFO queue and commit. Ten offered clients, 100 samples, 100k records and all
existing budgets remain. The cloud parent supplies FOUR CPUs shared by API and
Docker; the PostgreSQL four-CPU limit is not four dedicated CPUs.

Admission four (or a separately reported three-slot experiment) remains an
explicit experimental composition until wired reproducibly into the application
and verified against every retained read/write/uniqueness/browser gate. Custom
planning does not certify unmeasured mapped generations or arbitrary workloads.
Recovery disables opt-in SQL_V1 and forward-fixes; preserve data and legacy reads.
