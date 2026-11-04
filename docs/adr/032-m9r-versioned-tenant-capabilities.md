# ADR032: Versioned tenant capability definitions

Status: R5a independently PASS for trusted Go composition; full R5 PARTIAL.

Follow-up: [ADR033](033-m9r-worker-sql-capability-enforcement.md) applies live SQL
worker authority and forward40 typed/statement fixes. The original R5a review
gap below is closed within R5b's documented scope; full product R5 stays open.

R5a uses immutable, tenant-scoped role definitions and a CAS head. Trusted
identity and live membership select the role; token claims and HTTP capability
payloads cannot grant authority. Definitions contain a closed action catalog,
app/entity/field selectors and typed DataScope. Missing/inactive definitions,
unknown actions and invalid scopes deny. Explicit DENY wins over ALLOW. Teams
come from live server-side membership; owner/share predicates bind the verified
actor. There is no SQL or executable code in a definition.

Scopes are accepted only on entity/field data grants, which SQL intersects
before reading/writing rows. Row/aggregate and non-data grants are action gates;
scope-bearing grants there are rejected. Conditional DENY is unsupported and
rejected. Each capability has at most eight scoped grants and 63 combined nodes.
Existing SQL/projection/whole-command budgets also apply; definition admission
does not prove compatibility with every IR. Conditions resolve against the
executing typed IR. Strict JSON rejects duplicate keys, case aliases, trailing
data, unknown fields and excessive complexity. Returned scopes are deep copies.

Existing owner/admin/member/viewer behavior is retained by a recorded database
bootstrap adapter, avoiding an undocumented permission migration. Public
contracts cannot claim that adapter. New roles have no implicit capabilities.
Owners may publish replacements for configurable built-ins; the root owner role
is protected. Legacy tenant-wide roles remain explicitly broad. Decisions bind
both engine semantics and definition hash: `tenant-role-v1:<hash>` for configured
roles, `tenant-role-b4:<hash>` for native adapter version four. Old audit is retained.

Publication versions, operation keys and audit commit together. Reconcile lost
publication responses through the immutable operation key: this returns the
original version even when the head has advanced. Assignment compares an explicit
expected current role. Reconcile a lost assignment response through owner-authorized
membership listing before another assignment. This compares role labels, not a
membership revision/ABA witness. Only active owners may publish definitions or
assign existing active members. Members and roles must belong to the trusted
tenant; assignment must not remove its last active owner.

Management writes the tenant row before locking/checking the active owner
membership. Under repeatable read a competing owner change must abort, rather
than use a stale owner count. Immutable version and advancing-head triggers
retain history and reject mismatched active/version witnesses. Tenant-inclusive
membership FKs require the selected role to exist in that tenant.

Composition loads authority in each API/worker transaction; no global cache or
unknown-role fallback exists. Workers receive only the routed actor's definition
through a no-argument port, together with the exact actor and tenant IDs, and
verify both. Existing lease/record/field confinement remains. The global IAM
identifier catalog is operator-only. API/worker transaction admission rejects
renewed SELECT privileges, including column grants. Readiness checks owner/RLS,
immutability, function ownership/search path and grants.

Failed workflow mutations roll back before bounded denial evidence is appended
in a fresh exact-ticket transaction. Denial audit cannot replay an activity or
state transition. An audit failure remains an error; an unavailable or canceled
admission is not inferred to mean lease expiry. Inactive role and field/SQL-scope
denials preserve definition provenance. Event consumption loads live authority
before a new local effect; completed receipt reconciliation does not repeat effects.

Record owner/team/share administration must take the exclusive dataset gate
before records and mutate current logical identity. Validate active target
membership and tenant-inclusive teams, preserve tombstones and audit atomically;
never restore stale physical grants across retained generations. This is a
requirement for the next slice, not implemented administration in R5a.

Deliver independently reviewed slices. Custom workflow approval-role references,
record/team/share administration, field masking/partial updates, identity UX/skew
and frontend administration remain separate acceptance items. Existing workflow
plans still restrict approval roles. R5a does not close full R5, enable production,
implement business ERP modules or begin M10.

Independent review at caa9d84 confirmed that raw worker SQL retains access to
the exact pinned record and declared fields after role-body/active-head denial.
Trusted-Go checks do not close this boundary. R5b must enforce live capability
authority in private database ports before full R5 can pass. See the
[R5a review](../verification/M9R_R5A_INDEPENDENT_REVIEW.md).
