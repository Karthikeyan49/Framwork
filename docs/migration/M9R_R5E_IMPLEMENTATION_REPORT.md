# R5e — Configurable workflow approval-role contracts

Date: 15 October 2026 (IST). **R5e STATUS: PASS**, independently reviewed including the audit
follow-up, with final full/build/browser checks executed. Full R5/M9R remain PARTIAL;
M10 is paused.

## Checkpoint and implementation

The checkout began clean on `remediation/m9r-foundation` at `da42d57`.
The remote remediation head was an ancestor of
`claude/dazzling-goldberg-69wbkn`; the branch fast-forwarded and was pushed to
`e6305dfad4ea7ebfe9fd5a4a7de8994e2b81cfda`. Main was not merged or changed.
R5c record/team administration and migrations 41–44 were preserved.

R5e adds opt-in metadata/IR/compiler 1.3 and Workflow DSL 2.1. Approval role names
can reference tenant-defined roles, including `finance_manager`. The previous
schema files and compiler outputs remain unchanged. The committed pre-change
workflow 1.2 IR fixture has SHA256
`ea566c0483e6b67ecf2d9b94a9e99c75b397d1c4f16ca6c929ed5c8052c32f8b`.
Legacy restore retains original bytes/compiler provenance without compilation.

| Metadata schema | IR / compiler | Workflow reader |
| --- | --- | --- |
| 1.0 | 1.0 | Legacy authoring only; no executable workflow plan |
| 1.1 | 1.1 | Legacy authoring only; executable Rule DSL 2 |
| 1.2 | 1.2 | DSL 2.0, original four-role contract |
| 1.3 | 1.3 | DSL 2.1, tenant role identifiers; Rule DSL remains 2.0 |

Approvals/rejections require a live active membership and tenant role head,
the exact role in the pinned state, a distinct approver, current capabilities
and record/field scopes, plus existing state/version/deadline witnesses. Token
role and tenant claims do not select authority. Existing signal keys also
recheck current authority; receipt replay does not bypass revocation.

Migration 45 expands the metadata version CHECK and adds the app-only
`control.lock_workflow_approver(uuid,uuid,text)` SECURITY DEFINER port. It binds
the session's tenant/actor/role and holds SHARE locks on tenant, user, membership
and head through signal commit. Role-manager writes use the same tenant fence.
Stale REPEATABLE READ approvals fail instead of using revoked authority.
No IAM DML or new worker/dispatcher grants were added. Success remains atomic
with history, signal receipt, audit and lifecycle outbox. Denials roll back and
use fresh bounded evidence, including immutable workflow/record correlation.

The initial implementation is `44a3979`; `07276da` adds independent regression
tests/review and a 15-minute aggregate integration-runner deadline. The reviewer
inspected that harness change: query 2s, lock 500ms, lease and performance budgets
remain unchanged. Audit-context fix `d4364a3` and independent follow-up assertions
at `f1e2e74` preserve those boundaries. All four code/review commits are pushed
on the designated branch. The final documentation commit records completed
verification and updates the README, ADR, plan and finding ledger.

## Changed areas and dependencies

- ADR036: version matrix, live authority, concurrency and recovery.
- Versioned JSON Schema, metadata validator/models and immutable legacy fixture.
- Compiler version dispatch, stored rule/workflow validation and golden tests.
- Workflow plan reader, approval authority/signal checks and readiness.
- Policy approval adapter and platform API/worker composition.
- React preview decoder/test for the unchanged 1.3 page/data model.
- Integration fixtures, independent regressions and linked status documentation.

No dependencies, business activities, production routes or customer DDL paths
were introduced. Migration 45 neither rewrites nor deletes records, source,
pins, receipts or history. No previously published migration was edited.

## Commands and observed results

Logs remain ignored under `.local/reports/r5e-*`. Exact independent commands and
additional test witnesses are in the [review report](../verification/M9R_R5E_INDEPENDENT_REVIEW.md).

| Command actually executed | Result |
| --- | --- |
| `git fetch origin` | PASS fetch; the first subsequent ancestor check failed because the configured fetch refspec omitted the two branches. Explicit fetch below corrected discovery without a history change. |
| `git fetch origin refs/heads/remediation/m9r-foundation:refs/remotes/origin/remediation/m9r-foundation refs/heads/claude/dazzling-goldberg-69wbkn:refs/remotes/origin/claude/dazzling-goldberg-69wbkn` | PASS |
| `git merge-base --is-ancestor origin/remediation/m9r-foundation origin/claude/dazzling-goldberg-69wbkn` | PASS after explicit fetch |
| `git merge --ff-only origin/claude/dazzling-goldberg-69wbkn` | PASS |
| `git push origin remediation/m9r-foundation` | PASS checkpoints e6305df, 07276da and f1e2e74; remote equality verified |
| `make setup` | PASS; existing Node 24.19.0/Docker daemon reused |
| `make infra-up` | PASS PostgreSQL/Redis healthy and Collector running |
| `make migrate migration-status` before source changes | PASS migrations 41–44; all 1–44 applied |
| `make check test` on unchanged e6305df | PASS; integration 485.256s, frontend 29/29 |
| `./scripts/go.sh run .local/r5e-legacy.go` | PASS captured pre-change golden/hash; helper is ignored |
| `make migrate migration-status` after migration 45 | PASS; 45 applied |
| `make migrate migration-status` repeated after review | PASS; zero migrations applied; all 1–45 recorded |
| `./scripts/go.sh test ./internal/compiler ./internal/metadata ./internal/policy ./internal/runtime/workflow` | PASS |
| `./scripts/go.sh test -race ./internal/compiler ./internal/metadata ./internal/policy ./internal/runtime/workflow` | PASS |
| `make check` | PASS |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web test` | PASS 29/29 |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval|^TestM9DurableSnapshotApprovalAndImmutablePins$' -v ./tests/integration` | FAIL initial new rejection assertion expected synchronous terminal status; legacy and other new tests passed |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval' -v ./tests/integration` | FAIL first rerun: rejection fixture omitted the existing compensation effect; concurrency fixture re-resolved identity inside a bound callback, clearing tenant context. Fixed fixtures; no production check weakened. Final root rerun PASS four tests, 44.942s. |
| `make check test build` on 07276da | PASS; integration 646.388s, Go race tests/checks, frontend 29/29, eight binaries and frontend build |
| `make test-e2e` on 07276da | PASS 5/5, 9.5s |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApprovalCustomRolesAndImmutablePins$' -v ./tests/integration` after audit fix | PASS 21.244s |
| `make check test build` on final f1e2e74 source/tests | PASS; integration 629.247s, Go race tests/checks, frontend 29/29, eight binaries and frontend build |
| `make test-e2e` on final f1e2e74 source/tests | PASS 5/5, 8.7s |

## Independent review and closed follow-up

The initial independent review passed eight approval tests (89.756s), including
field/action/head replay revocation, distinct-state/actor/key confusion and five
stale-authority cases. The separate legacy 1.2 regression passed (17.739s).
Independent unit race and format/docs checks passed. Its first run had a
reviewer-only asynchronous-state assertion error, corrected and rerun. Earlier
failed runs are retained rather than relabeled as successful verification.

After the first full/browser PASS, root inspection identified a LOW audit
correlation gap: role-list/self-denial evidence omitted instance/plan/IR/version
context. `d4364a3` enriches the same denied entry through the existing evidence
helper, then keeps `Allow=false`. The exact denied-admin assertion checks
instance, plan/IR hashes, sequence and record. Authorization, immutable
artifacts, migrations and transaction boundaries are unchanged.

Independent re-review passed three tests (47.401s), adding exact self/role-list
denial assertions across distinct states while preserving durable witnesses.
The LOW gap is CLOSED. Final full checks/tests/build passed on f1e2e74
(integration 629.247s), followed by browser 5/5 PASS (8.7s).

## Threat model and remaining gaps

The app remains a trusted authenticated server; this helper is not a complete
policy engine or an authorization boundary for a compromised app credential.
Raw worker/dispatcher credentials have no EXECUTE grant, and the session guard
continues denying even after an injected worker grant. Existing worker
exact-ticket/live-policy confinement is preserved. Review did not attack
pre-existing own-leased-state DML authority or certify production readiness.

Compilation checks role syntax without querying a tenant catalog. Unknown role
references never provision membership or capabilities and cannot approve.
Role names are immutable requirements; capability definitions remain revocable.
Conditions/approval still use captured inputs. Current-document approval,
production lifecycle, R5d field masking/partial writes, R5c administration UI,
identity UX/skew, remaining R3 indexes/uniqueness/aggregates, R6 and R7 remain
open. No performance-tagged benchmark, fuzz campaign, production rollout/DR
rehearsal or hosted CI result is claimed by this slice.

## Recovery and Git delivery

Apply migration 45 before the binary, quiescing metadata authoring/workflows for
the brief constraint/fence cutover. Suspend approvals by revoking EXECUTE on the
new port. Binary rollback requires disabling workflows and 1.3 authoring because
the previous binary cannot read 1.3. Preserve pending artifacts/history and
forward-fix; never recompile instances, edit pushed migrations or restore broad
grants. A new 1.3 source/IR is a new artifact, not an in-place edit.

For existing preview data use the explicit R2 dataset adoption/binding procedure
after schema verification; plain revision namespaces never migrate data
automatically. A populated 1.2-to1.3 adoption operation was not executed here.

Code, schema contracts, migration, fixture, tests, ADR and reports belong in Git.
`.env`, `.local`, dependencies, binaries and frontend build outputs stay ignored.
Publish target is `remediation/m9r-foundation`; no main merge or production
deployment occurred. Final local/remote HEAD equality and clean working-tree
verification are recorded in the completion response; generated artifacts are
excluded from the committed project files.
