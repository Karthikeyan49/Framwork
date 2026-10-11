# ADR-018: Bounded typed data runtime in isolated development previews

- Status: accepted
- Date: 2026-10-11
- Scope: M5

Runtime consumes only immutable compiler.Program through private Scope, pinned to
source revision and IR content hash. API resolves source via metadata public ports,
compiles against the platform catalog, verifies tenant identity/membership and central
preview policy. Endpoints exist only in development/test; production does not register
them. No activation, customer DDL, SQL/shell payload or external effect is possible.
This implements data mechanics while leaving M11 sandbox lifecycle/redaction/tool
routing and M12 approved production release activation to their documented milestones.

Migration00008 creates tenant/app/revision/IR-hash scoped datasets and records with
ENABLE/FORCE RLS via the existing installer. Typed core UUID/actor/time/row-version
columns surround bounded custom_data JSONB, avoiding EAV. Core identifier aliases
are immutable; ordinary values match compiled field types. Decimal/money values are
exact bounded strings (money also carries fixed currency); integers stay within
JavaScript safe range. Date/time canonicalize; references require same-dataset target
existence. Future hot custom fields require measured index/promotion migrations.

All commands update a dataset write_version row before reference checks or mutation.
This serializes preview writes; stale repeatable-read snapshots fail40001 rather than
letting a concurrent reference/delete create dangling data. UPDATE/DELETE require
expected row_version; conflict409 or concurrent serialization503 requires reload,
never a blind retry. This dataset lock intentionally favors safety over throughput.

Query AST supports projection32, one sort key plus UUID tie-break, typed filters
(max depth3/nodes32, IN32), page1–100, bounded keyset cursor, a single declared
many_to_one reference join, and count/sum over at most the requested100-row budget.
Over-budget aggregates fail; they never silently aggregate a partial dataset.
Identifiers/types/SQL operators come from trusted plans/allowlists; field keys and
all values use bind parameters. Cursor fingerprint pins query and dataset; cursors
are positions, not authorization. No offset scans, arbitrary SQL/functions/tables,
uncontrolled recursive relationships or unbounded aggregate exists. Query spans carry
only IDs/fingerprints, not field values or SQL parameters; M2 imposes query deadlines.

Central policy is enforced at preview/entity/field/row/aggregate boundaries, including
filter/sort/join fields and reference targets. Every decision is audited in the same
transaction. Rows are buffered and SQL rows closed before row-policy audit writes
on the single connection. Write response/read and affected-field checks occur before
mutation. Viewer is read-only; owner/admin/member may manipulate preview data. ABAC
adapters receive trusted owner/entity/field/record attributes; unknown obligations deny.
No metadata grants roles. Whole-list denial on denied rows is safe; no field masking
or partial-row policy pagination claim is made.

Business mutations and mandatory audit share the M2 transaction. No event publisher
is added before M10 outbox. This is isolated preview data, not a claim of production
business events/side-effect correctness. Applied SQL is not rewritten; recovery uses
forward fixes, stable source/IR selection and retained data. Dataset counter increments
on some rejected reference commands are internal coordination, not business revision
changes. Unknown commit outcome requires inspection. No default seed/customer data.

Verification uses normal-role real CRUD, revision/tenant isolation, field/row policy,
read-denial-before-write, outer audit rollback, exact values, injection literals,
cursor/sort bounds, aggregates, declared joins and concurrent record/reference deletion.
Fresh migration tests include00008. No throughput/backup/DR claim; production registry
approval/promotion and rate/aggregate-tenant quotas remain later work.

Independent review found that authorizing only filtered results permits a filter
oracle (empty result versus denied matching row). Queries now prove visibility over
all unfiltered source candidates in the same repeatable-read snapshot, including all
used fields and joined targets. Any denied candidate rejects the entire query before
customer predicates run. The hard ceiling is1,000 candidates, plus authorized
lookahead; predicates/cursors cannot reduce this proof budget. Larger datasets require
a future trusted policy-predicate adapter. Per-field audit and the3-second transaction
deadline may reject queries below that ceiling; no throughput claim is made. Aggregate
sum requires its field in the actual projection; budgets follow authorization. Dates
and canonical UTC timestamps require years1–9999, matching the SQL/browser domain.
