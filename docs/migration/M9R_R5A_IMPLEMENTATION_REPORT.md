# M9R R5a — Versioned tenant capability authority

Date: 14–15 October 2026 (IST). Bounded R5a status: PASS for trusted Go
composition, API management and SQL-scoped API execution. Full R5/M9R: PARTIAL.
Independent review confirmed a HIGH raw-worker capability-body enforcement gap;
R5b must close that database boundary. No M10 or production deployment follows.

Baseline `45335e3`; implemented source commits `1ae1f27` and `caa9d84` on
`remediation/m9r-foundation`. Compatible records and immutable evidence remain.
No archives, source kits or customer data were deleted. No new dependency added.

## Implemented behavior

Migration 38 creates immutable tenant role versions, advancing heads and exact
publication-operation witnesses. Four retained roles have explicit bootstrap
adapters; configured roles have no implicit grant. Live membership chooses the
role. Owner-only publication compares the head, and assignment compares the
expected role label and preserves the last active owner. An actual tenant-row
write fences repeatable-read owner write skew. Management and audit commit together.

The closed compiler validates actions, selectors, typed DataScope, scope budgets,
strict JSON and unsupported reserved grants. DENY wins. Entity/field scopes reach
SQL before API rows enter Go. Teams come from live membership; definitions cannot
supply trusted team IDs. Loaded policy engines bind tenant and actor, and bare
compiler results cannot authorize. Decisions identify semantics and definition hash.

API requests load current authority. Workflow/event composition loads only the
exact delegated actor/tenant definition through a no-argument port. Global role
name discovery is denied; runtime admission detects table/column grant drift.
Readiness checks ownership, RLS, functions and immutability. Workflow denials roll
back business work, then append bounded evidence in a fresh exact-ticket transaction.
They cannot repeat effects. Event revocation is checked before a new local effect.

Files include migration 38, policy definition/store/readiness and tests, access
role routes, composition, tenant role syntax, audit provenance, workflow denial
handling and integration regressions. The mobile UI stopped displaying a long
diagnostic policy hash that overflowed its shell. [ADR032](../adr/032-m9r-versioned-tenant-capabilities.md)
records the decisions; [independent review](../verification/M9R_R5A_INDEPENDENT_REVIEW.md)
contains executed probes and reproducibility witnesses.

## API and recovery

- GET `/v1/tenants/{tenant}/roles/{role}`: current tenant definition.
- POST `/v1/tenants/{tenant}/roles`: operation_id, expected_version, active,
  definition. Definition contains schema_version `tenant-capabilities-v1`, name
  and grants; each grant has effect, actions, resource and supported typed scope.
- GET `/v1/tenants/{tenant}/roles/operations/{operation}`: immutable original
  publication result, even after the head advances.
- POST `/v1/tenants/{tenant}/members/{member}/role`: role and expected_role.
  Reconcile an uncertain assignment through owner-authorized membership listing.
  This compares labels, not a membership revision/ABA witness.

`make migrate` applies migration 38. It is forward-only. Recover configurable
roles by publishing another reviewed version with head CAS; preserve history.
Keep an active owner. Do not roll back to a binary that ignores configured heads:
its old built-in switch could broaden restricted member/admin authority. Stop
services and deploy a compatible forward fix instead. No destructive cleanup.

## Commands actually executed

PASS unless stated otherwise; tests used owned disposable databases. Earlier
failures remain below, even where subsequent corrected runs pass.

| Exact command / run | Result |
|---|---|
| `./scripts/go.sh test ./internal/policy ./internal/tenant ./internal/platform/access ./internal/platform/app ./internal/events ./tests/security` | PASS |
| `./scripts/go.sh test -race ./internal/policy ./internal/tenant ./internal/events ./internal/runtime/workflow ./internal/platform/postgres ./tests/security` | PASS |
| Focused six-root command below | PASS, 19.104s |
| `make migrate migration-status` | PASS; all migrations 1–38 applied |
| `make check test build` — first | FAIL; integration 585.274s: fixed 100ms lifecycle cancellation interrupted readiness; build not reached |
| Lifecycle command below — original | FAIL, three runs reproduced the premature cancellation |
| Lifecycle command below — after startup synchronization | PASS, three runs, 7.093s |
| `make check test build` — rerun at 1ae1f27 | PASS; full integration 586.315s, Go race/static, source checksums, Node/frontend, backend/frontend builds |
| `make test-performance` — root | PASS, 71.628s; 100k rows/10 clients: p95 232.157ms, p99 388.247ms, zero errors, 750ms target |
| `make test-write-performance` — root | PASS, 62.812s; 32 clients/60s: 7,920 writes, p95 345.929ms, p99 429.043ms, zero errors, 500ms target |
| `make test-e2e` — first | FAIL; two mobile overflow cases failed, three passed |
| Final nine-root command below at caa9d84 | PASS, 31.120s |
| `make check test-unit build` at caa9d84 | PASS; static/Go/Node 5/frontend 29/checksums/builds |
| `make test-e2e` at caa9d84 | PASS; all five browser cases, 9.6s |
| Independent probes, package tests, lifecycle and both workloads | PASS for the bounded slice; confirmed full-R5 HIGH detailed below |
| Production deployment; record administration/masking UI; custom workflow approval roles | NOT RUN / not implemented |

Six-root command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RCapabilit|^TestM9RConcurrentOwner|^TestM9RWorkerCapability|^TestM9REventConsumer|^TestM9RRoleNameGrant' -v ./tests/integration
```

Lifecycle command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=3 -run '^TestWorkerLifecycle$' -v ./tests/integration
```

Final nine-root command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RCapabilit|^TestM9RConcurrentOwner|^TestM9RWorkerCapability|^TestM9REventConsumer|^TestM9RRoleNameGrant|^TestM9RStoredRole|^TestM9RReservedCapabilit|^TestWorkerLifecycle' -v ./tests/integration
```

Other initial focused FAILs were: an invalid aggregate projection fixture
(corrected with an authorized control); rollback of denial audit (fixed with a
fresh exact-ticket append); and a worker field-denial fixture targeting balance
while the workflow declares status (corrected to status). Subsequent executed
regressions passed without weakening the denied-access assertions.

Independent review at caa9d84 reproduced the old unbound-engine bug with the
prefixed source, and proved a lock-only owner fence lets both owners demote
under repeatable read. The current tenant/actor binding and changed-row fence
passed. Its final workload runs passed: query p95 215.675ms/zero errors;
9,177 concurrent writes, p95 260.225ms/zero errors. Exact independent commands,
probe source, failed fixture run and hashes are in the linked review.

## Remaining HIGH and scope limits

Raw worker credentials can still bind their exact live lease and read the
pinned record's declared status field after a configured field DENY, empty TEAM
scope or inactive role head. Trusted Worker.Step denies and records current
policy provenance, but SQL worker_authority does not yet consult these bodies.
Generic record-table access, other jobs and other tenants remain denied; this
is a capability revocation gap within the leased resource, not a reproduced
cross-tenant escape. R5b must enforce live authorization in private SQL ports.

API scopes and trusted worker policy tests do not prove raw credential safety.
Privileged DBA/schema compromise is outside the runtime-role threat model.
Transactions use their authorized snapshot, not retroactive cancellation of
already committed effects. Bootstrap roles remain broad until replaced.

Record owner/team/share administration, masking/partial updates, configurable
workflow approval-role metadata, membership revision CAS, JWT skew/provider UX,
R3b remaining indexes, uniqueness/aggregation, R6 acceptance authority and R7
operations remain open in the finding ledger. No production, AI or external ERP
side effect is enabled. M10 remains paused.

## Git and next action

Commit reviewed source and reports on the remediation branch; no main merge or
production deployment. Ignore .env, dependencies, binaries, builds and local
logs. Preserve the original source-kit checksums. The next action is the R5b
raw-SQL capability fence plus adversarial revocation and full regression checks.

R5a STATUS: PASS (bounded trusted-Go slice)

R5 / M9R STATUS: PARTIAL
