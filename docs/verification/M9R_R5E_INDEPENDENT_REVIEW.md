# M9R R5e independent adversarial review

Date: 15 October 2026 (IST). Reviewed stable implementation `44a3979`, parent
`e6305df`, on `remediation/m9r-foundation`. Separate reviewer context; the root
held production source and competing test workloads frozen during this review.
Reviewer changes are two regression-test files and this report only. No
production code, migration, main branch, commit or push was changed by reviewer.

Status: PASS for the bounded R5e review below, including closure of the later
root-observed LOW audit-correlation gap at `d4364a3`. Final full checks/build/
browser validation of that fix remains the root's next step. Full R5/M9R remain
PARTIAL; M10 is paused.

## Scope and threat model

Read AGENTS/CLAUDE, README, V3 source contracts and supersession map, ADR023,
ADR032–036, the two-agent protocol and R5e implementation report. Reviewed the
`e6305df..44a3979` changes in schemas, compiler/restore, policy composition,
workflow plans/signals/readiness, frontend version admission and migration45.
The required cloud-environment-onboarding:setup skill guided reuse of the owned
local environment; no environment configuration or dependencies were changed.

Attacks use valid signed tokens with forged role/tenant claims, live role-body
and head changes, membership/identity revocation, stale REPEATABLE READ
snapshots, state/actor/key confusion and raw runtime SQL. All fixture mutations
use owned disposable databases through `m9SetupDocument`/`m8Database`. Those
helpers check development/test mode, local pgx effective driver targets and
connected database/session identity before fixture writes. No customer data,
secret, DSN or raw driver error is included here.

The app credential remains a trusted server credential. The new helper checks
membership/head and holds revocation locks; the Go caller separately checks
action capability and record/field DataScope. This helper alone is not a full
authorization policy for a compromised app process.

Worker/dispatcher conclusions are narrow: neither has EXECUTE on the new port;
an injected worker EXECUTE grant is detected by readiness and the helper's
session-user guard still returns false. The worker Approver adapter returns
false by source inspection. This review does not establish resistance to a
compromised executor mutating its own leased workflow state through existing
state-DML authority. R5e does not redesign that authority, add external effects,
or authorize M10.

## Findings

At the initial `44a3979` review, no BLOCKER, HIGH, MEDIUM or LOW implementation
finding was established by the reviewer. The first regression run exposed a reviewer fixture error:
`Signal` enters the next state as READY; the worker then records
WAITING_APPROVAL. The distinct-state test incorrectly required synchronous
WAITING_APPROVAL. Only the test was corrected to assert READY and explicitly
drive that worker transition. This was not an implementation fix.

New independent integration regressions exercise:

- A captured `field.contact.status` read DENY blocks a custom approver with
  an otherwise valid role/action. A previously successful key also fails after
  field-read, approval-capability or active-head revocation. Restoring authority
  reconciles the same receipt without repeating the transition.
- Successive approval states pin different custom roles. A valid approver for
  the first state cannot approve the second; forged owner/finance claims cannot
  select authority. Old-state receipt replay leaves the current state alone;
  fresh old-state requests and same-key/different-actor/state requests conflict.
- Committed membership deactivation, user deactivation, role reassignment,
  field-read revocation and action revocation each abort an already established
  stale REPEATABLE READ approval. Each denial leaves version, history, outbox,
  receipt and synthetic local-effect witnesses unchanged. The same pending
  instance succeeds once authority is restored.
- The SQL port binds actor to issuer/subject, uses the NOLOGIN schema owner,
  fixed search path and expected app-only ACL, rejects actor-GUC substitution,
  detects worker grant drift and detects an app EXECUTE revocation. Readiness
  passes again after restoring the original grants in the disposable database.

New compiler regressions reject rehashed IR downgrade/future-version, workflow
DSL downgrade/future-version, missing/extra plan and source/plan role mismatch.
They verify legacy-only dispatch preserves the exact 1.2 bytes and compiler
provenance, rejects 1.3, and accept 1/100 unique new role references while
rejecting 0/101/duplicate lists. Arbitrary syntactically valid role names compile
without provisioning tenant authority, as specified.

The independently rerun implementation regressions cover self-approval,
unlisted live admin, cross-tenant selection with populated tenant roles,
misleading signed claims, immutable in-flight pins after later authoring,
membership/head/action/OWNER-scope revocation, audit rollback and both orders of
role-head revocation/approval admission. Existing receipt reconciliation checks
run before a new transition only after live action/DataScope/role authority.
The existing legacy1.2 durable-workflow regression separately passed with live
approval, original input snapshot/pins, duplicate/collision protection and
immutable binding/history assertions.

## Commands and observed results

| Command actually executed | Result |
| --- | --- |
| `./scripts/go.sh test -tags=integration -run '^$' ./tests/integration` | PASS compile only, 0.014s; no behavioral test claim. |
| `gofmt -w internal/compiler/approval_roles_review_test.go tests/integration/m9r_workflow_approval_review_test.go` | Initial shell command unavailable; corrected to the pinned toolchain command below. |
| `"$(./scripts/go.sh env GOROOT)/bin/gofmt" -w internal/compiler/approval_roles_review_test.go tests/integration/m9r_workflow_approval_review_test.go` | PASS. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval' -v ./tests/integration` — initial reviewer run | FAIL, 83.360s: seven tests passed; distinct-state reviewer fixture failed on the lifecycle assertion described above. All five stale-authority subcases passed. |
| `./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/metadata ./internal/policy ./internal/runtime/workflow` | PASS: compiler18.188s, metadata2.380s, policy1.045s, workflow1.046s. Includes the two new compiler review tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval' -v ./tests/integration` — corrected final reviewer run | PASS, eight tests,89.756s; all five stale-authority subcases passed. The four independent reviewer tests took42.86s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9DurableSnapshotApprovalAndImmutablePins$' -v ./tests/integration` | PASS, one legacy1.2 workflow regression,17.739s (test body16.68s). |
| `git diff --exit-code e6305df 44a3979 -- contracts/jsonschema/application-v1.schema.json contracts/jsonschema/application-v1.1.schema.json contracts/jsonschema/application-v1.2.schema.json` | PASS: all three legacy schema files unchanged. |
| `git show e6305df:contracts/jsonschema/application-v1.2.schema.json \| sha256sum` and `sha256sum contracts/jsonschema/application-v1.2.schema.json internal/compiler/testdata/workflow-v1.2.ir.json` | PASS: old/current1.2 schema SHA256 `edb22fd9f616635eb66dd1d88e67bfa72982919bac9f10c990d0def522653d1d`; golden SHA256 `ea566c0483e6b67ecf2d9b94a9e99c75b397d1c4f16ca6c929ed5c8052c32f8b`. |
| `./scripts/check-format.sh && make docs-verify && git diff --check` | PASS;84 architecture source checksums and43 V3 source checksums. |

## Limits and recovery

The focused run is a small synthetic approval workload, not capacity evidence.
The four reviewer integration fixtures added42.86s to the final focused run;
the complete approval subset took89.756s. No query, lease or lock budget changed.
The full package may need a longer aggregate runner deadline as tests grow;
that does not relax per-operation performance gates.

Reviewer did not run full `make check test build`, browser e2e, performance-tagged
benchmarks, fuzz campaigns, populated production rollout/backup/restore drills,
or external tools. Disposable fixture creation applies migrations1–45 and
checks repeated migration application, but does not certify production cutover
locking or a populated1.2-to1.3 data-adoption operation. New1.3 IR naturally has
a different hash: revision preview namespaces do not implicitly migrate data;
the explicit existing dataset binding/adoption contract still applies. Legacy
pins remain readable with their original bytes.

Migration45 and earlier migrations remain untouched. The verified grant-revoke
probe confirms that suspending this approval port makes app readiness fail;
recover through reviewed grants and a forward fix, preserving artifacts,
history and receipts. Binary rollback still requires disabling workflows and
1.3 authoring because the previous binary cannot read1.3. No test deletes
immutable workflow evidence to retry.

## Aggregate test-runner follow-up

Inspected `git diff -- Makefile` after the review: the root added one explanatory
comment and `-timeout=15m` only to `go test` in `test-integration`. This sets the
aggregate Go test-process deadline; it changes no product runtime, query, lock,
lease or performance budget, and leaves performance-target commands unchanged.
Acceptable harness change. No reviewer tests were run and no source/migration
was edited while the root's final full verification ran. Its outcome is not
claimed by this review note.

## Denied-approval audit correlation follow-up

The root subsequently identified a LOW observability gap at final audit
inspection: role-list and self-approval rejection entries retained the actor,
tenant, policy decision and role, but could lose the workflow instance, pinned
IR/plan, version and record correlation when fresh denial evidence replaced
route evidence. The initial review did not assert those fields for this denial
branch and did not independently reproduce the pre-fix failure. Authorization
denial and transaction rollback were already enforced.

Reviewed stable fix `d4364a3` (parent `07276da`): one
`workflowEvidence(entry, x.Instance, "rejected", "")` call enriches the same
denied entry before the existing `Allow=false` and live-role assignment. The
helper copies the loaded instance's immutable pin and current sequence; it
does not change authority, state transitions, SQL, migrations or transaction
semantics. The root's test now requires exactly one denied-admin entry with
the exact instance, plan hash, IR hash, sequence and record. Root reported its
targeted regression PASS21.244s; this is root-supplied evidence.

Independent re-verification adds exact denial-evidence assertions to the
reviewer regressions for an owner self-approval denial and two distinct-state
custom-role denials. Each assertion binds the live actor/role, instance,
workflow/version, plan/IR hashes, sequence, app/revision and captured record/
record-version, requiring exactly one matching denial. Existing durable
version/history/outbox/receipt/effect witnesses must remain unchanged.

Independent command actually executed:
`node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM9RWorkflowApproval(CustomRolesAndImmutablePins|ReviewFieldCapabilityAndReceiptRevocation|ReviewDistinctStatesAndActors)$' -v ./tests/integration`
PASS, three tests,47.401s: field/receipt13.17s, distinct-state16.75s and custom
role/pins16.43s. This covers the
exact denied-admin receipt replay plus independent self/role-list correlation,
field/action/head revocation, state/key/actor negatives and immutable witnesses.
`./scripts/check-format.sh && make docs-verify && git diff --check` also passed
after these assertion/report additions. LOW correlation gap CLOSED within
this executed scope; no authorization or migration fix was required.

Full verification before this small fix was reported by root as
`make check test build` PASS (integration646.388s) and browser5/5 PASS; those
results are neither reviewer executions nor final verification of `d4364a3`.
The root will rerun its final full gates after this re-review.
