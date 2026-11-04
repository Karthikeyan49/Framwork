# M10 independent adversarial review — Tool Registry V2 (ledger, protocol, authority)

## Progress

- Day 1 20:20 UTC: branch `review/m10-final` created at `fb52ad3`; contract read (AGENTS.md, ADR036).
- Day 1 ~20:50 UTC: static review of migrations 45/46/47, `tool_step.go`, `execution.go`, `signals.go`,
  `executor.go`, catalog and policy. Stopped by an API session limit.
- Day 2 05:30 UTC: resumed. Static candidates C1–C9 recorded below; executing probes against the MAIN stack next.

## Scope & commit

- Reviewed tree: `fb52ad3` ("docs(m12): record full integration result and decider wording"),
  head of the M12 branch which contains M10 and M11.
- Scope per review brief: ADR036; AGENTS.md; `internal/tools` (protocol side); `internal/runtime/workflow`;
  migrations 00045 and 00047 (and the replaced worker policy decision); compiler tool pins; metadata 1.3.0;
  `internal/policy` (tools.invoke); `internal/platform/access/workflows.go`; `internal/platform/app/app.go`;
  `internal/config/config.go`; `tests/integration/m10_*.go`.
- Database: MAIN stack only, every database command through the queue wrapper.

## Method

(in progress)

## Commands run

(in progress)

## Findings

### M10R-01 (HIGH) — a transient lookup failure turns an UNKNOWN write into a blind resend

- Where: `internal/runtime/workflow/tool_step.go:89-96` (`decideTool`, FAILED branch returns `KindSend` for any
  retryable failure) and `db/migrations/00046_m11_sandbox.sql:169` (`control.tool_begin` admits SEND from
  `state='FAILED' AND retryable` regardless of an earlier UNKNOWN attempt). The ledger keeps one `state`, so the
  fact "an earlier send has an unknown outcome" is overwritten by the lookup's `FAILED/rate_limited` outcome.
- Executed: `TestZZReviewM10C1LookupTransientFailureResendsWrite` (temporary probe, MAIN stack, disposable DB):
  LOOKUP_BY_REFERENCE tool `tool.notify_lookup`; provider records the effect then drops the connection on the
  first POST; answers the first lookup GET with 429. Result: `status=SUCCEEDED posts=2 lookups=1` and attempt
  history `[SEND/UNKNOWN_REQUIRES_RECONCILIATION/transport, LOOKUP/FAILED/rate_limited, SEND/SUCCESS/ok]`.
  The fake provider happens to dedupe by key (effects=1); a LOOKUP_BY_REFERENCE provider by definition does not
  (otherwise NATIVE_KEY would be declared), so the second POST is a second real-world effect.
- Same branch also resends NATIVE_KEY/DEDUPE_WINDOW tools without re-checking the replay window (the window test
  exists only in the UNKNOWN branch, `tool_step.go:114`), and the local rate limiter / `not_sent` lookup outcomes
  (`executor.go:204-205`, `:338`) reach it the same way.
- Impact: violates ADR036 #6 ("provably absent → resend allowed, otherwise UNKNOWN") and the AGENTS.md rule
  "reconcile unknown commit/external outcomes before repeating effects"; duplicate external writes.
- Fix: make "unknown outcome outstanding" durable (e.g. ledger column or derive from attempts: any attempt with
  outcome UNKNOWN not followed by SUCCESS or a LOOKUP `not_found`); in `decideTool` and in `tool_begin` refuse
  SEND while it holds, except via LOOKUP-proved absence, in-window keyed resend, or operator NOT_APPLIED. A failed
  lookup must leave the invocation UNKNOWN (retry the lookup, never the send).

### M10R-02 (HIGH) — a compromised worker skips the approval gate inside its own lease; the database accepts it

- Where: `control.tool_begin(..., needs_approval boolean)` (`00046_m11_sandbox.sql:125,148`) runs the approval
  evidence and record-freshness checks only when the caller passes `true`; the worker supplies it from its own
  catalog copy (`tool_step.go:230`). The compiled plan does not pin the approval requirement (plan states carry
  only `tool_ref`, `version`, `inputs`, `tool_hash`), so the database cannot know it. In addition
  `framework_worker` holds `UPDATE(state_id,status,...)` on its own leased instance (`00020_m9_workflow_runtime.sql:38`)
  with no transition check, so it can jump past the approval state.
- Executed: `TestZZReviewM10C4WorkerSkipsApprovalInsideOwnLease` — raw SQL as `framework_worker` inside the exact
  lease of a freshly started `native` instance (approval REQUIRED, never approved):
  `UPDATE runtime.workflow_instances SET state_id='state.tool',status='READY'` → `rows=1`;
  `tool_begin(...,'SEND',true)` → `TL001` (the check exists); `tool_begin(...,'SEND',false)` → `attempt=1`;
  `tool_finish('state.tool',1,'SUCCESS','ok',false,200,false,'{"notification_id":"forged"}')` → `SUCCEEDED`.
  The normal worker then wrote that body as the step receipt and the instance ended `SUCCEEDED` with
  `approvals=0`, `provider_requests=0`, receipt `{"notification_id":"forged"}`. The success path also never compares
  the ledger's `request_hash` with the step's request hash (`loadToolLedger` does not read it), so the forged
  `repeat('b',64)` witness was accepted.
- Impact: ADR036 #7 ("A REQUIRED tool sends only if the database finds an API-written approval signal ... and the
  bound record's current version still equals the approved snapshot") is enforced by the worker, not the database;
  forged approval-required tool evidence and a forged step result are accepted. This exceeds the ADR036
  consequence residual ("can still lie about a provider response"). The database cannot stop a compromised worker
  from calling the provider itself (it holds the network path and secrets), so the impact is on authority evidence,
  workflow integrity and auditability, which is what the ADR claims the database guarantees.
- Fix: pin the approval requirement (and delivery capability) into the compiled plan state so `tool_begin` derives
  it from `plan_body` and ignores the caller; replace the instance column `UPDATE` grant with a definer port that
  validates transitions against the pinned plan (at least: never enter a state guarded by an `approved` transition
  without the matching history/signal); compare `request_hash` on the success/receipt path.

### M10R-03 (MEDIUM) — retry policy is bypassed once an instance is in RETRY_WAIT; UNKNOWN writes never reach RECONCILE

- Where: `execution.go:126-170` keeps `status='RETRY_WAIT'` through the dispatch transaction (no state save) and a
  tool outcome never passes through `recordFailure`; `decideTool` computes `due := x.Status == "RETRY_WAIT"`
  (`tool_step.go:86`) and `exhausted` from `x.Attempt`, which therefore never grows. Only the ledger budget
  (`attempts>=20`, `tool_begin`) stops it, by raising 55000 instead of settling the instance.
- Executed: `TestZZReviewM10C2RetryWaitBypassesMaxAttempts` (READ tool, provider always 429, `max_attempts=3`):
  after 15.5 s `status=RETRY_WAIT attempt=1 requests=10 ledger=FAILED/rate_limited attempts=10` (contract: DEAD
  after 3). `TestZZReviewM10C2bUnknownResendsUnboundedNeverReconcile` (NATIVE_KEY write, provider always 500):
  `status=RETRY_WAIT attempt=1 posts=10 ledger=UNKNOWN/server_error attempts=10` after 15 s (contract: keyed
  resend "within the replay window and attempt budget", then RECONCILE). Configured backoff is ignored between
  these sends; only lease-margin `ErrLease` returns pace them.
- Impact: provider hammering (each 429 is answered by another send), `max_attempts`/backoff not honoured, and once
  the 20-attempt ledger budget is hit the instance livelocks in RETRY_WAIT (every claim fails with 55000) instead of
  DEAD or RECONCILE, so an unknown write is never surfaced for reconciliation.
- Fix: route every non-success tool outcome through the failure path (increment `attempt`, back off, honour
  `max_attempts`; UNKNOWN at budget → RECONCILE); treat "ledger budget exhausted" as a settled failure, not an error.

### M10R-04 (MEDIUM) — a rejected lookup turns an UNKNOWN write into a permanent failure; the effect becomes unreconcilable

- Where: `classifyResponse` maps a lookup 4xx (≠404/429) to `FAILED rejected` (`executor.go:381-382`, no
  `kind == KindLookup` case); `tool_finish` stores it as invocation `FAILED`; `decideTool` → `Permanent()` → DEAD.
  `control.tool_reconcile` and the `reconcile` signal require status RECONCILE (`00045:241`, `signals.go:118`).
- Executed: `TestZZReviewM10C3bLookupRejectedStrandsUnknownWrite` — first POST creates the effect and drops the
  connection; lookups answer 400. Result: `status=DEAD ledger=FAILED/rejected effects=1` attempts
  `[SEND/UNKNOWN_REQUIRES_RECONCILIATION/transport, LOOKUP/FAILED/rejected]`; owner `reconcile` → 409; owner
  `recover` → DEAD again (FAILED non-retryable). The write that happened at the provider can never be recorded.
- Related paths (static, same consequence): catalog definition removed/changed or tenant allowlist edited while
  the ledger is SENDING/UNKNOWN (`tool_step.go:180-184` fails Permanent before reading the ledger; the SENDING
  attempt is never abandoned; `Definition.Hash` covers `tenants`, so adding any tenant to an allowlist changes the
  pin for every tenant); stale approval (TL002) on a keyed resend after UNKNOWN; abandon at the last attempt
  (`execution.go:418`, `max_attempts=1` is authorable) goes DEAD (recoverable via `recover`, unlike the cases above).
- Impact: ADR036 #6 "otherwise UNKNOWN" and "NONE ... parks in RECONCILE" are not upheld; unknown external effects
  are lost into DEAD with no operator path to record APPLIED/NOT_APPLIED.
- Fix: a lookup that is not 2xx-valid or 404 must leave the invocation UNKNOWN; any terminal failure while an
  unknown outcome is outstanding must park in RECONCILE; read the ledger before the catalog fail-closed check.

### M10R-05 (MEDIUM) — "provably absent" lookup runs while the original request may still be in flight

- Where: `decideTool` returns `KindLookup` immediately after an UNKNOWN send (`tool_step.go:109-111`), and
  `Worker.Step` loops in the same lease (`execution.go:52-61`); there is no settle delay before trusting a 404.
- Executed: `TestZZReviewM10C5LookupWhileOriginalRequestInFlight` — provider commits the first POST at the end of
  1.8 s processing (tool timeout 1 s). Lookup GET arrived 1.278 s after the first POST, got 404, and the write was
  resent: attempts `[SEND/UNKNOWN_REQUIRES_RECONCILIATION/timeout, LOOKUP/FAILED/not_found, SEND/SUCCESS/ok]`, 2 POSTs.
- Impact: for providers whose write commits after the client timeout (the common cause of a timeout), the lookup
  "proof of absence" is a race and the write is duplicated. ADR036 #6 states the lookup proves absence.
- Fix: require a declared provider settle interval (catalog field) before a lookup may prove absence, schedule the
  lookup via RETRY_WAIT after it, and keep the invocation UNKNOWN on early 404s.

Static candidates (each is re-labelled with its executed evidence below once the probe has run):

- C1 (HIGH candidate) — after an UNKNOWN send, a later attempt that ends `FAILED` + retryable (lookup 429,
  local rate limit, `not_sent`) moves the ledger to `FAILED/retryable`; `decideTool`'s FAILED branch
  (`internal/runtime/workflow/tool_step.go:89-96`) then returns `KindSend` and `control.tool_begin`
  (`db/migrations/00046_m11_sandbox.sql:169`) accepts SEND from `FAILED AND retryable`. A LOOKUP_BY_REFERENCE
  write is resent without proof of absence; NATIVE_KEY/DEDUPE_WINDOW resends skip the replay-window check.
- C2 (MEDIUM candidate) — tool outcomes recorded while the instance is `RETRY_WAIT` never pass through
  `recordFailure`; `due := x.Status == "RETRY_WAIT"` stays true, so each FAILED-retryable/UNKNOWN outcome is
  resent immediately (no backoff, `max_attempts` ignored) until `tool_begin` raises "attempt budget exhausted"
  (55000) at 20 attempts, after which the instance never reaches DEAD or RECONCILE.
- C3 (MEDIUM candidate) — UNKNOWN external outcomes end in DEAD instead of RECONCILE (unreconcilable because
  `control.tool_reconcile` requires status RECONCILE): abandon at the last attempt (`execution.go:415-424`),
  lookup 4xx classified FAILED permanent (`executor.go:381`), catalog/allowlist change while SENDING/UNKNOWN
  (`tool_step.go:180-184` fails before inspecting the ledger; `Definition.Hash` includes `tenants`), stale
  approval on a resend (TL002).
- C4 (MEDIUM candidate) — the database approval/freshness check runs only when the worker passes
  `needs_approval=true` (8th argument of `control.tool_begin`), and `framework_worker` may
  `UPDATE runtime.workflow_instances(state_id,status)` on its own leased job (00020:38) with no transition check.
- C5 (MEDIUM candidate) — the lookup after a timeout runs immediately in the same lease (`decideTool` returns
  `KindLookup` without backoff); a provider still processing the original request answers 404 and the write
  is resent ("provably absent" is not proven while the original request may be in flight).
- C6 (LOW candidate) — replay-window test uses decision time (`now.Sub(firstSent) >= window`) with no margin
  for commit + send timeout; a resend decided just inside the window can reach the provider after it.
- C7 (LOW candidate) — `control.tool_reconcile` accepts any active non-viewer member (SQL) while ADR036 #6 and
  Go policy restrict `workflows.reconcile` to owner/admin (custom grants excepted); defense-in-depth gap.
- C8 (LOW candidate) — reconcile `APPLIED` output is canonicalized but not validated against the tool's
  declared typed output (`signals.go:134-139`).
- C9 (LOW candidate) — `control.tool_finish` does not consult the live SQL policy decision although ADR036 #4
  says every worker port is bound to it (recording an outcome after revocation is arguably correct; wording).

## Verified (attacks that held)

(in progress)

## Not covered

(in progress)
