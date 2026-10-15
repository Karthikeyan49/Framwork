# M9 Workflow Runtime V2 acceptance

This checklist is verified; evidence is in M9_IMPLEMENTATION_REPORT.md and M9_INDEPENDENT_REVIEW.md. Scope: M9 over retained M0–M8/M6R;
development/test execution only. M10 tools and M13 production activation are excluded.

- [x] Opt-in executable DSL2 plans, deterministic typed conditions and versioned IR.
- [x] Legacy bytes/hashes preserved; stored restore checks source/plan agreement.
- [x] Immutable instance pins and captured inputs; no recompile or implicit upgrade.
- [x] Durable state, ordered append-only history, timers and PG-time deadlines.
- [x] Fenced concurrent work, bounded retry/backoff, timeout and dead-letter handling.
- [x] Idempotent starts/signals/activities with request-hash conflict checks.
- [x] Local effects, receipts, history, audit and lifecycle outbox commit together.
- [x] Live policy, source-record/field DataScope and single-job worker RLS.
- [x] Approval role checks, no self-approval, stale/duplicate signal protection.
- [x] Cancellation, explicit compensation and reviewed recovery/reconciliation.
- [x] Unknown outcomes never blindly retry; unknown commit resolves durable state.
- [x] Actual crash/restart, concurrency, database failure and adversarial verification.
- [x] Migration/grant/readiness checks, regressions, no data loss and independent review.

All available local checks passed after disclosed failures were fixed.

Lifetime history is capped at1000 entries; reaching it blocks further signals and
requires a reviewed forward fix. Only trusted local.noop is registered. Production
activation, external tools, lifecycle-event subscriber delivery, remote CI and
fresh cloud restoration were not executed. These limits do not certify production
rollout or universal exactly-once external effects.

M9 STATUS: PASS within the documented preview scope. Stop before M10.
