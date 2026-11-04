# ADR036: Versioned tenant workflow approval roles

- Status: accepted; R5e independently verified with full/build/browser PASS. See
  [implementation evidence](../migration/M9R_R5E_IMPLEMENTATION_REPORT.md).
- Date: 15 October 2026 (IST).
- Scope: metadata/compiler, workflow plans/signals and policy composition.

## Context

ADR032 introduced tenant-defined capability roles, but schema/IR 1.2 and Workflow
DSL 2.0 accept only owner/admin/member/viewer as approval roles. Changing those
contracts in place would silently broaden historical stored plans. ADR023 and
V3 require immutable in-flight artifacts and live authorization at signal time.

## Decision

Add explicit metadata/IR/compiler 1.3 and Workflow DSL 2.1. The new workflow
approval-role identifiers use the existing tenant-role grammar
`^[a-z][a-z0-9_]{0,63}$`, with 1–100 unique, canonically sorted names per approval
state. Rules remain DSL 2.0. Schema/IR 1.0–1.2 and Workflow DSL 2.0 keep their
existing bytes, hashes and role restrictions. Restore dispatches by persisted
versions, validates source/plan agreement and never invokes Compile. A pre-change
compiled 1.2 golden fixture and hash guard compatibility. The semantic React
renderer admits 1.3; its data/page contract is unchanged.

Compilation validates syntax, not tenant catalog existence. A role reference
does not create a role or grant permission. Approval requires the approver's
current active tenant membership, an active tenant role head, the exact role
name in the pinned approval state, live workflow action capability and all
existing record/field DataScopes. An unknown/inactive/unassigned role cannot
approve. Owners/admins receive no override of the pinned list. Role names are
pinned business references; capability definitions remain live and revocable.
Self-approval, state/version witnesses and deadlines remain enforced.

Add a small mandatory approval-authority port to workflow Store, composed using
policy.RoleStore. No token or HTTP role claims enter this port. Forward migration 45
admits metadata 1.3 and adds `control.lock_workflow_approver(uuid,uuid,text)`, executable
only by framework_app and owned by the NOLOGIN schema owner with fixed search
path. It verifies session identity/tenant and the selected membership role, and
takes SHARE locks on the tenant, user, membership and active role head. Role
administration already writes the tenant fence. Concurrent committed changes
therefore cause a stale REPEATABLE READ transaction to abort; an admitted signal
holds authority stable through commit. Lock/serialization failures retain the
existing bounded conflict response; there is no blind retry. Workers/dispatchers
cannot call this port or signal approvals. Readiness checks its definer/grants.

The approval check runs before signal receipt reconciliation as well as before
new transitions. Replaying an old idempotency key cannot bypass current role
revocation. Denied mutations roll back and retain bounded denial evidence;
successful signal/history/audit/outbox/receipt writes remain one transaction.

## Consequences and limits

Custom roles such as finance_manager can approve through existing preview APIs
after owner-authorized publication/assignment and explicit capabilities. This
does not add production activation, approval UI, a role editor, field masking,
partial writes, current-document approval semantics, new workflow activities,
identity providers or M10. Captured workflow inputs remain snapshots. The app
credential remains a trusted server credential; this helper is not a substitute
for the worker's separate exact-job SQL boundary.

## Verification and recovery

Run deterministic compile/restore/golden and schema-version negatives; populated
API/SQL membership/head/capability/DataScope, spoofed-claim, cross-tenant,
self/stale/idempotency/audit-fault and concurrent-revocation cases. Independently
review a stable commit, then run full checks/tests/build and browser regressions.

Migration 45 is additive in accepted versions/functions, forward-only and does
not rewrite metadata, pins or records. Quiesce authoring/workflows for rollout.
Before a binary rollback disable workflow execution and 1.3 authoring: the old
binary cannot read 1.3 artifacts. Preserve all artifacts and evidence; forward-fix
instead of downgrading/recompiling pending plans. Revoke the new port's EXECUTE
to suspend approval signals if necessary; never restore broad IAM grants.
