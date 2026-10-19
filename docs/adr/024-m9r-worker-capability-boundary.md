# ADR024: Authenticated worker audiences and confined job capabilities

Status: accepted for M9R implementation; verification pending.

The M9R baseline reproduced a worker credential obtaining a globally routed job,
then using caller-set identity GUCs to read and mutate unrelated tenant records.
Separating a claim function without constraining downstream authority would not
repair that boundary. A shared worker/receiver credential cannot authenticate
distinct workers by a caller-supplied worker ID or Redis consumer name.

## Decision

Use PostgreSQL LOGIN identities as authenticated worker audiences. Operators
assign each principal to one tenant; a principal must not acquire a second tenant
through a caller-supplied context. Principals inherit only the constrained worker
privilege group. The existing `framework_worker` login may represent one local
principal; it is not an implicitly authorized global worker. Additional principals
are provisioned explicitly, with managed unique credentials. Use session_user's
role identity for audience checks even if a caller attempts SET ROLE.

Only the dispatcher may claim global workflow/event routing work and issue a
lease. It has no business payload privileges. Issued capabilities bind the
persisted audience, tenant, job kind/ID, actor, release, source resource, operation,
generation, unguessable token and PostgreSQL expiry. Fields derive from immutable
captured workflow inputs, not a caller-supplied field list. Assignment revocation
and live membership are checked at execution.

The database binds a validated capability to a private authorization row keyed by
server-derived backend PID and full transaction ID. Workers cannot read or mutate
that table directly. Unverified tenant/job/actor GUCs cannot create authority;
identity and RLS helpers consult the protected context and current lease. Failed
binding clears previous authority; another transaction or reused connection
cannot reuse an old row. Rebinding an active transaction to another job denies.

Clearing is transactional. An arbitrary SQL caller can ROLLBACK TO a SAVEPOINT
to restore the previous valid capability; it remains confined to its original
job/resources and expiry. The trusted wrapper aborts the entire transaction on
failed binding. No claim of irreversible revocation through SQL rebind is made.

Lease fencing must survive REPEATABLE READ, rather than re-reading an old MVCC
generation. Workflow binding locks the instance before the job, revalidates the
lease under lock and holds the fence through commit, matching API signal/save
lock order. Stale snapshots abort. Event binding uses its delivery row fence.
Assignment/membership revocation takes effect within the bounded transaction
model; document and test the precise boundary rather than promising instantaneous
revocation of already-running transactions.

## Least privilege

Workflow workers operate only on their exact instance bindings/state/receipts/
history/lifecycle outbox. The present `local.noop` needs no general business DML;
remove those grants. No direct business-record SELECT is required: a capability
read port returns only the bound source record, declared fields and necessary
owner/team/version facts. It does not return the rest of custom_data. Event
receipt capabilities grant no business-record reads.

Restrict IAM self/membership/team facts to the bound delegated actor and tenant,
including `iam.users`, which the old restrictive policies omitted. Remove unused
metadata/record-set/link/projection privileges. Keep audit append provenance and
role checks. Unknown roles, missing assignments, absent/expired capabilities and
cross-audience attempts deny at the database boundary.

Keep Go policy/DataScope checks around the confined read port. Current Native
policy is built-in role based; arbitrary future ABAC is not safely authorized by
a predicate supplied by a compromised worker. R5 must provide trusted persisted
policy/issuer authority before claiming stronger malicious-process ABAC protection.

Broker messages carry routing capabilities, never business payloads. Workflow
durability remains PostgreSQL-owned. Delivery must target the assigned audience;
receiving another audience's reference cannot authorize database access. Operator
and dispatcher credentials must never enter worker subprocess environments.

M8 duplicate/dead pointers still need a narrow terminal-status resolution path
so they can be ACKed without payload authority. Old published attempts must not
become new payload capabilities. M9 history/audit/outbox writes precede scheduling,
because scheduling invalidates the current lease. Failure recording cannot reuse
an expired/replaced capability.

## Threat model and limits

Protect against stolen worker database credentials, arbitrary SQL/GUCs and a
compromised worker process holding its own audience/receiver credentials. A
principal explicitly assigned a tenant has that declared job-processing scope.
Operator/migrator, database superuser and trusted producer remain privileged.

Process protection requires separate service identity/container boundaries,
no readable operator/other-audience credential files and no inspection of other
service process environments. The same-user local `make dev` launcher does not
provide that isolation: its host `.env` contains several credentials. Tests verify
database authorization using isolated audience credentials, not hostile-process
isolation of that development host. Production deployment must establish and
verify the service-secret/OS boundary before applying the process claim.

Resource confinement does not attest honest execution by a malicious worker.
Direct permitted instance/history/receipt changes may be fabricated within its
own capability. Human approval signals remain an independent actor boundary.
Future high-risk tool authorization must not trust a worker-written SUCCEEDED
state or receipt alone. Admission of compiler/verification evidence remains the
M12/M13 authority work; matching artifact hashes alone do not prove provenance.

## Rollout and evidence

1. Add assignment/capability storage and narrow ports without deleting customer
   records, pins, receipts or history. Quiesce workers for credential/lease cutover.
2. Provision/assign worker audiences and broker delivery; existing unbound roles
   fail closed. Preserve pending work and invalidate/reissue old unbound leases.
3. Enable the new dispatcher/worker protocol and verify readiness/role grants.
4. On failure disable workers and forward-fix; never automatically restore broad
   grants or delete evidence to recover. Old binaries must not run this protocol.

Independent review additionally requires entity-inclusive share keys, explicit
temporary-catalog lookup hardening and denial despite restored generic grants.
Event-only readiness checks the same worker grants. Secondary receiver bindings
and workflow enablement are verified through configuration and process composition.

Required tests include populated two-tenant jobs, password-only self-claim denial,
forged context denial, wrong audience/tenant/app/release/record/field/operation,
expired/replayed generation/token, private-context tampering, pool reuse, failed
rebind, privilege drift, source-field confidentiality and no generic DML.

Also execute legitimate HTTP-started workflow progression, scoped event delivery,
membership/assignment revocation, simultaneous worker/approval/cancel operations,
publish-before-finish, duplicate/dead old messages, lost broker state, lease rollover
and unknown commit outcomes. Record the failing baseline and fixed results at
stable commits before claiming R1 complete.
