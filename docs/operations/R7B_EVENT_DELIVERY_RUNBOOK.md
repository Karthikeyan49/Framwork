# R7b preview event outage operation

Authority: [ADR062](../adr/062-m9r-event-outage-budgets.md) and its independent
review correction [ADR063](../adr/063-m9r-live-event-authority.md), database policy
`event-outage-v3`. Event envelopes remain1.0 and routing pointers remain1.1;
stored IR and workflow plans are unchanged. This is development/test delivery,
not a production deployment or an external-action guarantee.

## Timing and observation

- Publication lease:5 seconds. Finishing after expiry or with a stale
  generation/token fails; a successful send without a confirmed finish remains
  uncertain and may be republished with a new attempt.
- Successful finish:15-minute receipt window. Only the assigned tenant worker
  can resolve that exact event/hash/token/generation during the window. Live
  membership, assignment, policy and RLS still apply. Trusted Go consumer
  transactions remain3 seconds, SQL2 seconds and lock waits500ms; this is not a
  new processing-lease protocol or raw-credential process-liveness guarantee.
  Event authority requires READ COMMITTED at binding and every later use. Raw
  RR/SERIALIZABLE and imported snapshots fail closed. Revocation is visible on
  subsequent statements; API and workflow isolation remain REPEATABLE READ.
- Known publish failure:500ms exponential retry, capped at60 seconds. Expired
  publication leases also wait this backoff after expiry. Maximum20 attempts;
  immediate known failures cannot exhaust the budget before about783.5 seconds.
- A missing consumer does not cause republication during the first receipt
  window. After timeout, a new fenced attempt may publish the same immutable
  event. Repeated receipt timeouts can occupy roughly5 hours plus dispatch
  delays before exhaustion. These are bounds on the local policy, not an
  end-to-end delivery SLA.

Observe delivery state, attempt count, generation, lease_until, next_at,
last_error and retained attempt outcomes through a controlled operator session.
Correlate existing publish/consume traces without logging credentials, payloads
or raw driver errors. Repeated receipt timeouts, exhausted budgets and aggregate
backlogs require operator attention; alerting/retention automation is separate.

Tradeoff: after Redis stream loss a successful SENT event can wait up to the
remaining15-minute receipt window before republication. Increasing consumer
outage tolerance does not provide fast broker-loss repair. Detecting broker
loss and safely advancing an attempt earlier needs a separately reviewed port;
do not erase inboxes, forge receipts or rewrite due times operationally.

## Forward migration

1. Identify and quiesce owned API event emitters, consumers and dispatchers.
   Drain their database transactions. Do not kill unrelated
   services or flush Redis. Preserve event envelopes, attempts, inbox, replay
   history and broker references.
2. Apply `make migrate`, then inspect `make migration-status`. Migration00055
   changes functions and the attempt-count upper bound only; migration00056
   adds event isolation enforcement and semantic retry readiness. Neither resets
   existing states, tokens, attempts, deadlines or data.
   Migration00057 takes NOWAIT exclusive locks on worker context and the two
   event tables before installing direct restrictive READ COMMITTED RLS. A live
   old reader/binder refuses cutover with SQLSTATE55P03; the failed migration
   installs no partial policies. Drain only known owned work and retry. Never
   infer DDL rollback from a cancelled client response; check migration state.
3. Verify new runtime event readiness before starting owned dispatchers/workers.
   New binaries deny a missing or mismatched policy contract. Older compatible
   callers keep the same SQL signatures; in-progress issued deadlines remain
   unchanged until completion or a later fenced attempt. Pre-correction RR event
   consumers must be upgraded: they fail closed after migration00056. The policy
   probe returns no contract for incorrect actual helper delays; runtime roles
   still cannot execute the private helper. This is drift detection, not proof
   against an administrator able to replace both helper and probe.
   Function-only replacement at56 cannot revoke all cached reads in an already
   open RR backend; do not claim an online cutover or run before the barrier
   at57 succeeds. New readiness requires both exact restrictive policies.
4. Observe completion and ordering in the intended local tenant. DEAD and
   LEGACY_PAUSED rows remain paused; they do not resume just because code was
   updated. Workflow leases and external tools are unaffected.

On failure stop owned delivery and forward-fix. Migration down intentionally
fails. Reverting to a five-attempt constraint can reject retained rows above
five and shorten their recovery budget; it is not an automatic rollback.

## Replay and ordering

A dead predecessor blocks its aggregate's successors; unrelated aggregates
continue independently. Existing `bin/event-replay` is still a controlled
migrator operation requiring exact tenant/event/expected-generation and a
reviewed operator reference/reason. It preserves the immutable body, inbox and
attempt history. It is not a least-privilege public recovery API; that R7 design
remains open. Never reset generations or delete receipts to force execution.
Completing a replayed event cannot undo external effects or missing data.

## Executable rehearsal

From the prepared local checkout:

```sh
make test-event-outage
```

The explicit test owns a disposable database, worker role, unique Redis stream
and sender/receiver ACLs. It stops consuming for at least600 real monotonic and
PostgreSQL seconds while dispatch continues, and revokes XADD only on its owned
sender. It then restores the sender and drains committed receipts/effects/audit,
checking hashes, ordering, deduplication and foreign-worker denial. No shared
sender ACL, Redis service, customer stream, policy duration or timestamp is
changed to manufacture the result. Owned fixtures are cleaned up on normal
completion. The15-minute total test deadline does not change SQL/transaction
budgets.

The ordinary integration suite includes a30-second version plus accelerated
SQL boundary and populated forward-migration tests. Those boundary tests are
not substitutes for the actual600-second rehearsal. See the implementation and
independent review reports for executed results and any failures.
