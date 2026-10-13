# M9R R1 worker authority remediation

Status: implementation verified by focused tests; full regression and independent
review are pending. R2–R4 remain pending. This report does not declare all external
review findings resolved or authorize M10/production exposure.

## Checkpoint and changes

The validation/plan checkpoint `7db648b` was pushed to
`remediation/m9r-foundation` before implementation, as requested. R0 reconciles
architecture authority in the supersession map without changing source kits.
The working implementation and customer data are retained.

ADR024 introduces operator-assigned PostgreSQL LOGIN audiences. Dispatcher-only
issuance and a tenant-bound worker mailbox replace worker global claims. Exact
job kind/tenant/actor/release/generation/token/expiry bind to a private backend/
transaction context. GUCs cannot create that authority. Instance→job locking
fences stale REPEATABLE READ snapshots and preserves signal lock order.

Workers lose generic record DML/SELECT and unused metadata/release access. A
narrow source function masks undeclared JSONB fields and selects only the bound
record. IAM user/membership/tenant/team and workflow/event RLS enforce that
context. Go policy/DataScope checks remain around the source port.

Event pointer1.1 routes to a principal-specific Redis stream, with separate
receiver ACLs. Current exact published attempts authorize payload consumption;
old attempts resolve terminal status only. Receipt-only consumers continue to
work. The former generic consumer mutation path is intentionally denied and its
transaction rolls back, including inbox/audit/local effects.

Workflow offers and committed pickup are separate from execution retry budgets.
An offline worker does not exhaust retries through dispatcher polling. One pickup
per offer prevents concurrent workers from taking the same lease. Killed/lost
pickup recovery waits for its persisted lease and preserves attempt evidence.

`worker-admin` assigns/revokes principals and appends operator audit atomically.
Its read-only check uses the worker credential. Local development reports an
unassigned worker as paused while API/frontend/dispatcher start. Provisioning is
explicit; startup never picks a tenant or grants authority silently. No new major
dependencies were introduced.

## Database changes and recovery

Forward migrations22–25 add assignment/context storage, audiences and pickup
facts; replace narrow functions/policies; revoke broad/unused grants. Applied
migrations remain unchanged; refinements are new forward migrations. No records,
IR/plan/input pins, history, inbox, envelopes or audit facts are deleted.

Quiesce old workers/dispatchers, migrate, provision/assign principals and receiver
ACLs, then start the new binaries. An already-running local Redis needs ACL reload
or an owned restart. Old unbound leases cannot execute and are reissued after
expiry. On failure stop workers and forward-fix; never restore broad grants or
erase evidence automatically. See [the runbook](M9R_WORKER_RUNBOOK.md).

## Verification executed

| Exact command | Result / evidence |
|---|---|
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorker' -v ./tests/integration` before implementation | EXPECTED FAIL: worker self-issued a global lease and forged context read/updated/deleted an unrelated record |
| Same command after the capability boundary | PASS: raw SQL privilege denials, populated runnable job and unrelated record |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8\|^TestM9' -v ./tests/integration` | PASS after fixes:157.459s, event/workflow/credential regressions and fault cases |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RPopulated' -v ./tests/integration` | PASS: two populated tenants, separate LOGIN principals, SET ROLE audience confinement, source reads and secondary-principal execution |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM8CrashRecovery\|^TestM9ROffline' -v ./tests/integration` | PASS:17.536s, real process death/publication recovery and25 unpicked offers without exhausting execution budget |
| `make check test-unit` | PASS: format/vet/dependency verification, lint/typecheck,84 V2/43 V3 document checks, Go tests,4 Node tests,28 frontend tests |
| `node scripts/m8-env.mjs`; `make infra-up` | PASS: retained credentials, scoped local ACL file, healthy PostgreSQL/Redis/Collector |
| `docker compose --env-file .env -f infra/compose/compose.yaml restart redis` | PASS: apply local receiver ACL cutover; persistent PostgreSQL data untouched |
| `make migrate` | PASS: forward local application; disposable fixtures also migrate and verify repeat no-op |
| `git diff --check` | PASS |
| `make check test-unit build`; `make test-integration` | RUNNING at this checkpoint; results must be recorded before completion |
| Independent stable-code review; browser suite/live smoke | NOT RUN at this checkpoint |

During implementation, SQL dollar-quote syntax, same-statement audience lookup,
readiness schema visibility, and old protocol assertions failed. They were fixed
and rerun. In particular, old publish attempts no longer execute after rollover;
the new attempt executes once and the old pointer resolves the committed receipt.
Crash recovery tests retain actual process kills and5s PostgreSQL expiry, rather
than replacing them with passing mocks.

New raw-credential tests cover wrong tenant/job/kind/release/generation/token,
expired leases, private-context/assignment denial, masking, failed rebind, pooled
transaction lifetime, membership/assignment revocation, principal inheritance,
replaced leases under REPEATABLE READ and removal of generic business mutation.
Event tests exercise cross-audience DB denial and receiver-key ACL denial.

## Limits and remaining gates

Operators/DBA and the trusted app producer remain privileged. A malicious worker
may fabricate permitted progress within its own job; its receipt/SUCCEEDED state
is not independently verified business acceptance or external tool authority.
The5s lease checks and held row fence prevent lease rollover during a transaction;
the trusted wrapper bounds transactions to3s. Revocation follows the current
transaction snapshot, rather than retroactively undoing committed/ongoing work.

Built-in role policy still lacks product ownership/sharing/custom ABAC: R5.
Compiler/verifier admission is not established by a matching hash: R6. Event
outage budgets, retention, workflow cycles, approval snapshots and production
operations remain R7. Stable dataset identity, query cost and concurrent record
writes are R2–R4 and are not changed by this checkpoint.

Source changes, migrations, tests, ADR/runbook and reports belong in Git. `.env`,
private keys, role/receiver passwords, `.local`, binaries, dependencies, frontend
builds and other generated artifacts remain ignored.
