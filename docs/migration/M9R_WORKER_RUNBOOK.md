# M9R worker cutover and recovery

The verified scope remains development/test preview execution. Production
activation, custom ABAC and external tools have separate gates in the remediation
plan. ADR024 defines the credential/process threat model and its limits.

## Cutover

1. Stop owned old workers and dispatchers. Retain records, job pins, receipts,
   attempts, inbox and history. Apply migrations22–26 with the operator binding.
   Migration22 removes broad worker grants; old binaries must remain stopped.
2. Provision independent PostgreSQL LOGIN identities with managed random passwords,
   INHERIT, NOSUPERUSER, NOBYPASSRLS, NOCREATEROLE, NOCREATEDB and NOREPLICATION.
   Grant only `framework_worker` membership. Do not grant app, dispatcher, owner,
   operator, audit-writer or built-in privileged roles. Role names must match
   `[a-z][a-z0-9_]{0,62}` and exclude reserved service/operator names. The existing
   local login is one audience. The database assignment constraint also rejects
   `worker`, `dispatcher`, `operator` and the named non-worker framework roles.
3. Use `bin/worker-admin --principal ROLE --tenant UUID --operator CHANGE_REF`.
   One principal maps to one tenant and this initial adapter retains one
   assignment per tenant, including inactive assignments. Assignment and bounded operator audit commit together.
   Assignment is not inferred from messages, GUCs or the first queued tenant.
4. Configure the worker's DB credential and unique Redis receiver credential.
   Receiver keys: only `framework:events:preview:v1:ROLE`; commands: ping,
   xgroup|create, xreadgroup, xautoclaim, xack. No publish, administration or other
   audience keys. Username is `event_ROLE`, except the retained local mapping
   `framework_worker` → `event_worker`. Dispatcher may only ping/xadd on the
   audience stream prefix and holds its separate routing DB credential.

   Reserve `operator`, `dispatcher` and `worker` as DB audience names to prevent
   collisions with retained broker account names. For deployed hostile-process
   isolation, use distinct service identities/containers, no shared readable
   credential files and no other-service process/environment inspection. Local
   `make dev` runs as one host user and is a trusted development convenience; its
   environment filtering does not establish that OS/secret isolation.
5. Local `make infra-up` writes the ACL file; a running Redis needs ACL reload or
   an owned service restart to apply changed ACL contents. Never pass operator
   secrets into runtime processes. `worker-admin --check` uses only the worker
   binding. Start new dispatcher/worker binaries and check their readiness.

Workflow leases are PostgreSQL mailboxes. Only dispatcher issuance changes lease
generation/token; a worker picks up one offer for its authenticated audience.
Pickup commits before the business transaction; concurrent polls cannot consume
the same offer twice. A lost pickup response or killed worker waits for the5s
lease before reissue. Offline audiences that never pick up an offer do not consume
the execution failure counter. Every issue/expiry remains in routing evidence;
operations retention remains R7 work. Picked-up crash retries retain the existing
bounded failure/recovery semantics.

Event pointers1.1 include audience routing and go to that audience's Redis stream.
PostgreSQL validates persisted audience/attempt facts. Older published attempts
can resolve terminal status for ACK; they cannot create a new payload capability.
Unbound pre-cutover leases cannot authorize workers and are reissued after expiry.
Old shared-stream messages remain broker references and are not authoritative.
No immutable envelope/inbox fact is rewritten to facilitate retries.

## Revocation, rollback and limitations

Use `worker-admin --disable` to deny future bindings. Terminate a compromised
principal's sessions and rotate its credentials through the DBA when immediate
revocation is required. An already-running bounded transaction uses its MVCC
snapshot: revocation is not an instantaneous rollback of that transaction.

Password rotation on the same LOGIN identity preserves its assignment. Replacing
the principal name requires a reviewed operator transaction: stop old workers,
disable/remove its retained assignment, assign the new role and append operator
audit. An inactive row still occupies the tenant uniqueness key. Keep old routing
attempts/inbox/history and old-stream references; existing leases reissue to the
new audience after expiry. The CLI does not silently replace another principal.

Failed binding removes context within its transaction. SAVEPOINT rollback can
restore a previously valid context, which still cannot switch job/resources or
extend expiry. The Go wrapper aborts the whole transaction after failed binding.

On failure stop workers, retain durable evidence and forward-fix. The migrations
reject Down; restoring broad grants is not an automatic rollback. Metadata/record
storage is unaffected by this cutover. Backup restore requires the matching new
binary/role/assignment configuration and preserves stored IR without recompiling.

Workers cannot read generic records or mutate business tables. The current
`local.noop` operates only within its instance and declared source read. Legacy
test-only generic consumer mutation is now rejected atomically. Future activities
require independently authorized narrow ports; a worker-written receipt or
SUCCEEDED state is not sufficient evidence for high-risk external effects.

The read port masks undeclared JSONB fields, retains source ownership/team/version
facts and the Go policy/DataScope checks. Built-in role policy remains the shipped
policy; arbitrary malicious-process ABAC is R5 work. Hash consistency is not
authenticated compiler/verifier admission; candidate authority remains R6.

The five-attempt event outage budget, DAG workflows and approval snapshots remain
R7 limitations. Audience confinement does not claim these are fixed.
