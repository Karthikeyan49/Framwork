# ADR023: Versioned workflow plans and PostgreSQL durability

Status: Accepted for M9 development/test runtime; production activation remains M13.

M8 supplies reliable event delivery, but workflow source graphs are not executable
plans. M9 must survive process failure without changing the business definition
or repeating committed local activities.

Introduce opt-in metadata/IR1.2 and Workflow DSL2. The compiler produces closed,
bounded acyclic plans with typed Rule V2 conditions. Legacy1.0/1.1 IR bytes and
hashes remain unchanged. Restore verifies persisted plans and their agreement
with source; it never invokes the current compiler. Each instance stores its exact
IR, workflow plan, captured record inputs and version/hash pins. Release changes
affect new instances only.

The framework owns the state model. An explicit durability port uses PostgreSQL
as the initial adapter: tenant RLS, PG-clock deadlines, fenced leases, immutable
history, idempotent starts/signals/activities, and atomic local effect, history,
audit and lifecycle outbox writes. A worker-only SECURITY DEFINER claim function
returns one routing reference, never business payload. The existing single-job
live delegated identity boundary still governs execution. The dispatcher keeps
its M8 role and cannot read workflow payloads. No Redis workflow state or network
operation occurs inside a runtime transaction.

M9 registers a trusted no-effect local activity and an explicit transactional
activity port. M10 will register external tools. Unknown external outcomes must
wait for reconciliation, never automatic retry. A local transaction rollback is
the only basis for safe local retries. Cancellation/timeout fence later work;
compensation is explicitly registered and recorded in reverse completion order,
and failures require manual recovery. It does not undo arbitrary external actions.

Approvals bind the live authorized approver, instance version and current state;
self-approval is denied. All operations recheck policy and SQL DataScope using
captured field IDs without replacing stored input values. A lost COMMIT response
is resolved by persisted receipts/versions, not by repeating an effect.

Public operations remain isolated preview APIs. Workflow lifecycle events use a
separate immutable outbox contract; M8's fixed record receipt consumer is retained.
External subscribers, tool activities and production activation are later work.
Temporal was evaluated: it supplies stronger long-running operational tooling,
but adds a service before this bounded local foundation has a production workload.
The durability port permits a later adapter without changing business metadata.

Recovery uses reviewed resume/reconciliation commands with expected versions;
inputs, pins, activity receipts and history are never cleared. Migrations are
additive and forward-only. Back up PostgreSQL before upgrade; roll back binaries
only with workflow execution disabled, preserving new immutable history.

The preview adapter has a lifetime1000-entry history budget. This hard limit
blocks further recovery/signals when exhausted; it is an explicit operational
limit requiring a reviewed forward fix, not permission to erase evidence.
