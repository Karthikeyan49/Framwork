# M13 Approval and Promotion (control plane) — implementation report

Date: 16–17 October 2026 (UTC). Source: `claude/m13-approval-promotion` (based on the M12 branch
`claude/m12-candidate-builder`). Decisions, amendments and recovery are in
[ADR039](../adr/039-m13-approval-and-promotion.md). **M13 is PARTIAL against the roadmap line
"Approval + Promotion":** the approval, rejection, activation and rollback *authority* is built and
tested; there is **no production data plane** (no production datasets, no execution of storage
migrations, no release-pinned production routes — ADR039 §10). An activated release is an auditable
pointer and nothing serves it. This is a development/test preview capability, not production ready.

## What changed

Before M13, approval and activation were Go checks over tables the runtime role could write
directly: `framework_app` could INSERT approvals and INSERT/UPDATE the active-release pointer, and
the only database rules were two RLS policies. A buggy or compromised API runtime could approve
anything and point the pointer at any approved hash, stale or never activated.

- **Migration 49** (additive, forward-only): `framework_app` loses INSERT/UPDATE on
  `control.release_approvals` and `control.active_releases` (table and column level). New
  append-only `control.release_rejections` and `control.release_history`. SECURITY DEFINER ports
  `approve_release`, `reject_release`, `activate_release`, `rollback_release` (owner
  `framework_schema_owner`, `search_path=pg_catalog,pg_temp`, EXECUTE only for `framework_app`):
  database-floor separation of duties by risk level (HIGH excludes requester and acceptance author,
  CRITICAL also the sandbox freezer and needs two distinct approvers), approvers re-checked as
  active non-viewer members at activation, rejection permanence, compare-and-set activation
  with a row lock, rollback only to a previously activated and not rejected release with a
  class gate on the current release's `Risk.Rollback`. Guards: the pointer moves one generation
  at a time and only with its history row (deferred constraint trigger), history is a contiguous
  chain, approval/rejection/history rows can be written only from inside a port, and
  `control.worker_grants_safe()` now also trips on REFERENCES/TRIGGER grants and lists the release
  tables.
- **Go**: policy actions `releases.read|approve|activate|rollback` (policy `m13-native-v10`),
  audit fields, the promotion store (`ApproveRelease`, `RejectRelease`, `ActivateRelease`,
  `RollbackRelease`, reads, typed errors) and pure helpers (quorum floors, status derivation,
  rollback-plan rule). The M6R direct-write `Store.Approve/Activate/Rollback` were removed.
- **API** (preview-gated, exact patterns, strict bodies): `POST …/candidates/{c}/approve|reject|activate`,
  `POST …/releases/rollback`, `GET …/releases`, `GET …/releases/active`; candidate views gained
  status, approvals, quorum and rejections. Every decision loads the candidate through
  `Store.LoadCandidate` (Restore plus verification against the candidate's own base release), evaluates
  staleness in the same transaction and never compiles source ("no recompile after approval", enforced by
  a unit test).
- **Findings fixed on the way** (see the M12 report): the Restore/diff forgery (M12-F1), gateway
  catalog findings F1–F4, case-folded duplicate JSON keys.

## Commands executed and results

Executed on the merged tree (M10 review fixes, M11/M12 review fixes, migrations 49–53) on 2026-10-19,
one process at a time on an otherwise idle 4-core host, integration groups against the isolated M13 stack
(reset from this tree first; every test also builds its own disposable database from the tree's migrations).

| Command | Result |
|---|---|
| `go build ./...`, `go vet ./...`, `go vet -tags=integration ./tests/integration`, `gofmt -l .`, `node scripts/verify-docs.mjs` | PASS |
| Unit tests `-race -count=1` by group: promotion, risk, intent, verification, metadata; compiler, tools; runtime/...; policy, platform/..., audit, db, contracts, cmd; erp, events, identity, observability, sandbox, tenant, config, tests/security | PASS (every package) |
| `TestM13` group (ports, API, adversarial, integrity, review regressions `TestM13R*`), `-race` | PASS, 172 s |
| `TestM12` group, `-race` | PASS (after the F9 cost fixture was sized down under the race detector) |
| `TestM11` group (13 tests, includes `TestM11R*`), `-race` | PASS, 156 s |
| `TestM10` group (22 tests, includes `TestM10R*`), `-race` | PASS, 480 s |
| `TestM9` group, `-race` | PASS, 497 s |
| `TestM8` group without `TestM8RedisACLStreamLossAndPendingRecovery`, `-race` | PASS, 46 s |
| Full integration suite, `make check`, `make test-unit`, `make build`, `make test-e2e` | run on the M14 tree, which contains this branch: see "Final gate" below |

Defects found by running the merged tree (none by the branches alone) and fixed here:
- Migration 52 derives the worker grant guard for control, runtime, iam and audit and refuses every relation it does
  not list. The M8 and M9 test fixtures created effect-probe tables in `runtime` with a worker grant, so
  `worker_grants_safe()` was false in every disposable database and every worker-claiming test (M8–M11, M9R) failed
  with "worker job denied". The probe tables now live in their own schemas (`m8_fixture`, `m9_fixture`).
- The F9 large-candidate fixture exceeded the 3 s envelope while being built under `-race`; under the race detector
  it uses a smaller candidate (the non-race run keeps the full size).

## Independent review

Independent adversarial review of M13 (a separate agent, 2026-10-19): **PASS WITH FINDINGS**, 0 HIGH, 2 MEDIUM,
4 LOW, 8 INFO (`docs/verification/M13_INDEPENDENT_REVIEW.md`, fix re-verification in
`docs/verification/FIX_REVERIFICATION.md`). The database authority floor held against every direct attack.

| Finding | Disposition | Where |
|---|---|---|
| M13R-01 MEDIUM an acknowledged rejection did not stop an activation that started earlier | FIXED: per-hash fence row written by both ports, the older snapshot aborts with 40001 | migration 53, ADR039 amendment 12, `TestM13R01*` |
| M13R-02 MEDIUM rollback class gate read only the current release | FIXED: gate covers every release the pointer crosses | migration 53, `promote.go`, ADR039 amendment 12, `TestM13R02*` |
| M13R-03 LOW demoted approver still counts toward quorum | FIXED in the Go layer after the first fix round: only approvers whose current role grants `releases.approve` count at activation and in the derived status; the database floor is unchanged | `promote.go` (`holdingApprovers`), `policy/holds.go`, `promotion/approver_roles.go`, ADR039 amendment 13, `TestM13R03*` |
| M13R-04 LOW U+017F / U+212A alias a key | FIXED: request keys must be ASCII snake_case | `promote.go`, `TestM13R04*` |
| M13R-05 LOW invisible marks in notes | FIXED in Go, port and table constraint (U+200C/U+200D stay legal) | migration 53, `TestM13R05*` |
| M13R-06 LOW runtime components never compared with the binary | FIXED by the M12 API fixes (`RUNTIME_CHANGED` staleness) | `TestM13R06*` |
| FR-01 LOW a worker can set status/termination/failure of its leased instance | DOCUMENTED (ADR024 residual) | ADR039 amendment 13 |
| FR-02 LOW an exemption band between the verifier probes passed | FIXED: ratio probes, verifier version `verifier-v2` | ADR038 amendment 9, `TestExemptionBandBetweenBoundaryProbesFails` |

These fixes are NOT independently reviewed. No separate reviewer has re-verified them.

## Re-run recipe

```
export PATH=/tmp/node24/node-v24.22.0-linux-x64/bin:$PATH
/home/user/bin/dbrun13 /home/user/m13-env/reset.sh          # from the worktree root; applies migrations 1–53
for g in TestM13 TestM12 TestM11 TestM10 TestM9; do
  /home/user/bin/dbrun13 node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout 40m -run "$g" ./tests/integration
done
/home/user/bin/dbrun13 node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -timeout 40m -run TestM8 -skip TestM8RedisACLStreamLossAndPendingRecovery ./tests/integration
```

## Final gate

Run on 2026-10-19 on `claude/m14-ai-builder` (code identical to commit `8a8d34d`; this branch is an ancestor of it), against
the isolated M14 database stack reset from that tree. The branch itself was proven by the groups above.

| Command | Result |
|---|---|
| `make check` (docs checksums, format, vet, `mod verify`, web lint, typecheck) | PASS |
| `make test-unit` (Go unit tests `-race` over every package, `node --test scripts/*.test.mjs`, web `npm test`) | PASS (5 script tests, 29 web tests; `internal/ai` takes about 212 s under `-race`) |
| Full `go test -race -tags=integration ./tests/integration`, skipping `TestM8RedisACLStreamLossAndPendingRecovery` | 1714 s; exactly two failures, `TestM1DevelopmentIssuer` and `TestAPIWithoutCollectorAndDependencyOutages`: the worktree had no local development identity keys (`.local/identity`, ignored by git). Both PASS (7.6 s) once the keys were provided; no other test failed |
| `make build` | PASS |
| `make test-e2e` (5 browser tests) | PASS, 21.6 s |
| `TestM8RedisACLStreamLossAndPendingRecovery` | NOT RUN: it restarts the shared Redis and fails on isolated stacks by design |

## Follow-up: M13R-03 closed in the Go layer (2026-10-19)

Only approvers whose current role grants `releases.approve` count at activation and in the derived status (ADR039
amendment 13). Written failing-first: with `promote.go` reverted, `TestM13R03DemotedApproverNoLongerCounts`,
`...ReassignedCustomApproverNoLongerCounts` and `...CriticalQuorumNeedsBothApproversToHold` answered 200 on the approval
of a demoted member, and `...DeactivatedApproverNoLongerCounts` showed `APPROVED` for a candidate the port then refused.
Two existing tests changed their expectation, not their purpose: the activation after a demotion now answers
`approval_quorum_not_met` (Go refuses before the port, `TestM13APICriticalQuorumOnATransformRelease`), and the
custom-role matrix test activates through the loader-configured handler that approved (`TestM13APIRoleMatrixCustomRoleTenantAndAppScope`;
a handler that knows only the built-in roles correctly does not count a custom-role approver). Run on the isolated M13
stack reset from the changed tree, one process at a time. This change post-dates the "Final gate" below; the full
integration suite, `make check`, `make test-unit`, `make build` and `make test-e2e` were **not** re-run for it.

| Command | Result |
|---|---|
| `gofmt -l`, `node scripts/verify-docs.mjs`, `go vet -tags=integration ./tests/integration` | PASS |
| `go test -race ./internal/policy/... ./internal/promotion/... ./internal/platform/access/... ./internal/tenant/...` (new: `TestNativeHolds`, `TestDefinedHoldsFollowsTheGrants`) | PASS (promotion 37.9 s) |
| `TestM13R03` group, `-race` (5 tests) | PASS, 16 s |
| Mutation: `Holds` ignores an explicit DENY | caught by `TestDefinedHoldsFollowsTheGrants` and `TestM13R03HoldsMatchesDecide`; restored |
| `TestM13`, `TestM12` groups, `-race` | PASS, 256 s |

## Threat model and limits

- **Identity is bound by the API process** (session settings after token verification). A fully
  compromised API process that knows another member's issuer and subject can bind it and approve and
  activate as that member (executed: `TestM13AdversarialCompromisedAPIBindsAnotherMembersIdentity`).
  Credential-bound approvals are a later slice.
- **Forged candidate bodies**: the database cannot recompute the verifier, diff or policy hash; the raw
  ports accept a self-consistent forged body; `LoadCandidate` refuses it before any API decision.
- **Availability envelope**: restore plus verification plus staleness runs inside the 3 s runtime
  transaction; with `-race` on a loaded host several parallel approvals of one candidate time out
  with 503 and write nothing.
- **Activation of a non-EQUAL storage class** is allowed (HIGH/CRITICAL approval applies) but nothing
  migrates data; a production data plane must refuse to serve a release whose storage is not provisioned.
- A HIGH candidate needs three distinct people, CRITICAL two more approvers; tenants must add members.
- Tool-catalog staleness at approve/activate is covered by unit tests only (the integration order app has no tool pins).
- Not built (ADR039 §10): production data plane, approval expiry, configurable approver roles (R5e),
  volume measurement, a separate verifier credential, UI, release lifecycle events.
