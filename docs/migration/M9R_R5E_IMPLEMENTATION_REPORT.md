# R5e — Configurable workflow approval-role contracts

Date: 15 October 2026 (IST). Status: implementation under verification.
Full R5/M9R remain PARTIAL; M10 is paused.

## Checkpoint and scope

The checkout began clean on `remediation/m9r-foundation` at `da42d57`.
The remote remediation head was an ancestor of
`claude/dazzling-goldberg-69wbkn`; the branch fast-forwarded and was pushed to
`e6305dfad4ea7ebfe9fd5a4a7de8994e2b81cfda`. Main was not merged or changed.
R5c record/team administration and migrations41–44 were preserved.

R5e adds opt-in metadata/IR/compiler1.3 and Workflow DSL2.1. Approval role names
can reference tenant-defined roles, including finance_manager. The previous
schema files and compiler outputs remain unchanged. The committed pre-change
workflow1.2 IR fixture has SHA256
`ea566c0483e6b67ecf2d9b94a9e99c75b397d1c4f16ca6c929ed5c8052c32f8b`.
Legacy restore retains original bytes/compiler provenance without compilation.

Approvals/rejections require a live active membership and tenant role head,
the exact role in the pinned state, a distinct approver, current capabilities
and record/field scopes, plus the existing state/version/deadline witnesses.
Token role and tenant claims do not select authority. Existing signal keys also
recheck current authority; receipt replay does not bypass revocation.

Migration45 expands the metadata version CHECK and adds the app-only
`control.lock_workflow_approver(uuid,uuid,text)` SECURITY DEFINER port. It binds
the session's tenant/actor/role and holds SHARE locks on tenant, user, membership
and head through signal commit. Current role-manager writes use the same tenant
fence. Stale REPEATABLE READ approvals fail instead of using revoked authority.
No IAM DML or new worker/dispatcher grants were added. Success remains atomic
with history, signal receipt, audit and lifecycle outbox. Denials roll back and
use existing fresh evidence handling.

## Changed areas

- ADR036 records the version matrix, live authority, concurrency and recovery.
- `contracts/jsonschema/application-v1.3.schema.json` and metadata validator/models.
- Compiler version dispatch, stored rule/workflow validation and golden tests.
- Workflow plan reader, approval port/signal checks and database readiness.
- Policy approval adapter and platform API/worker composition.
- React preview compatibility decoder/test for the unchanged1.3 page/data model.
- Integration fixtures/tests and linked status/contract documentation.

No dependencies, business activities or production routes were introduced.

## Commands and results

Logs remain ignored under `.local/reports/r5e-*`; no credentials are included.

| Command actually executed | Result |
| --- | --- |
| `git fetch origin` | PASS fetch; the following ancestor command initially failed because the configured fetch refspec omitted the two branches. Explicit branch fetch below corrected remote-tracking discovery; no history changed during that failure. |
| `git fetch origin refs/heads/remediation/m9r-foundation:refs/remotes/origin/remediation/m9r-foundation refs/heads/claude/dazzling-goldberg-69wbkn:refs/remotes/origin/claude/dazzling-goldberg-69wbkn` | PASS |
| `git merge-base --is-ancestor origin/remediation/m9r-foundation origin/claude/dazzling-goldberg-69wbkn` | PASS after explicit fetch |
| `git merge --ff-only origin/claude/dazzling-goldberg-69wbkn` | PASS |
| `git push origin remediation/m9r-foundation` | PASS checkpoint e6305df |
| `make setup` | PASS; existing Node24.19.0/Docker daemon reused |
| `make infra-up` | PASS PostgreSQL/Redis healthy and Collector running |
| `make migrate migration-status` before source changes | PASS migrations41–44; all1–44 applied |
| `make check test` on unchanged e6305df | PASS; integration485.256s, frontend29/29 |
| `./scripts/go.sh run .local/r5e-legacy.go` | PASS captured pre-change immutable golden/hash; helper is ignored |
| `make migrate migration-status` after migration45 | PASS;45 applied |
| `./scripts/go.sh test ./internal/compiler ./internal/metadata ./internal/policy ./internal/runtime/workflow` | PASS |
| `./scripts/go.sh test -race ./internal/compiler ./internal/metadata ./internal/policy ./internal/runtime/workflow` | PASS |
| `make check` | PASS |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web test` | PASS29/29 |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval|^TestM9DurableSnapshotApprovalAndImmutablePins$' -v ./tests/integration` | FAIL initial new rejection assertion expected synchronous terminal status; legacy and other new tests passed |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval' -v ./tests/integration` | FAIL first rerun: rejection fixture omitted the existing compensation effect; concurrency fixture re-resolved identity inside an already bound callback, clearing tenant context. Fixed fixture setup/counting; no production check was weakened. Final rerun passed (all four new tests,44.942s). |
| `make check test build` | NOT RUN on final source yet |
| `make test-e2e` | NOT RUN on final source yet |

The final focused rerun passed all four new tests (44.942s), including both
concurrent revocation orders. Independent and full verification remain pending.
Earlier failed runs remain recorded.

## Threat model and remaining gaps

The app remains a trusted authenticated server; the new helper is not a complete
policy engine or an authorization boundary for a compromised app credential.
Raw worker/dispatcher credentials have no EXECUTE grant. Existing worker
exact-ticket/live-policy confinement is preserved. Independent adversarial
review is required before this slice is accepted.

Compilation checks role syntax without querying a tenant catalog. Unknown role
references do not provision membership or capabilities, and cannot approve.
Role names are immutable workflow requirements; current capability definitions
are deliberately revocable. Business conditions/approval still use captured
inputs, not a current-document approval model. Production lifecycle, R5d field
masking/partial writes, R5c administration UI, identity UX/skew, remaining
R3 indexes/uniqueness/aggregates, R6 and R7 remain open. This is not a complete
product policy or production-readiness claim.

## Recovery

Apply migration45 before the binary, quiescing metadata authoring/workflows for
the brief constraint/fence cutover. No source, record or stored pin is rewritten
or deleted. Suspend approvals by revoking EXECUTE on the new port. A binary
rollback requires disabling workflows and1.3 authoring because the previous
binary cannot read1.3; keep pending artifacts/history and forward-fix. Do not
recompile existing instances, edit pushed migrations or restore broad grants.
