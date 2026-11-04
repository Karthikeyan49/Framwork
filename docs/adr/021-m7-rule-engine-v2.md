# ADR-021: Versioned pure Rule Engine V2

Status: Accepted for M7 development. Date: 2026-10-12 (Asia/Kolkata).

M7 follows independently verified M6R. It introduces executable typed expressions;
M8 delivery, workflows, tools, sandbox/promotion orchestration and ERP posting are
outside this milestone.

Schema 1.0.0 and its published contract stay unchanged. Opt-in schema 1.1.0 requires
rule `dsl_version: 2.0.0`; application compilation emits IR/compiler 1.1.0 with typed
rule plans. Legacy compilation retains IR/compiler 1.0.0 and identical golden bytes.
The runtime explicitly supports both IR versions. Legacy expressions were checked
but not executed in M4–M6R; they are not silently executed under DSL2. Adopting DSL2
requires an explicit new source revision and reviewed release. New preview revisions
use distinct IR identities/datasets; original records and released artifacts persist.

The rules module owns a small closed AST and immutable canonical Program. Compiler
composition resolves exact entity fields and generates typed plans. Stored Restore
validates annotations, references, source binding, calendars, timezone hash and
budgets without compiling source or replacing compiler provenance. No expression
can obtain the clock, data, policy, secrets, filesystem, network or tools. Evaluation
accepts explicit typed values plus cancellation; results are values/decisions only.

Integer, decimal and Money use bounded signed integer counts of 10^-18 units with
magnitude <=10^18. Add/subtract are exact; multiplication/division round half to even
at 18 fractional places. Zero division and overflow fail. Money currencies must
match for add/subtract/comparison; multiplication/division require a scalar. `round`
uses explicit places 0–18; `money_round` explicitly rounds to reviewed currency minor
units. There is no implicit financial posting rounding or FX conversion. Decimal/
integer result JSON uses exact strings; money uses amount/currency objects.

Dates are Gregorian civil dates, years 1–9999. Instants are explicit RFC3339 with
<=9 fractional digits, normalized to UTC. Date arithmetic operates on civil days;
instant arithmetic operates on elapsed seconds. `date_in_zone` takes a literal IANA
zone. Calendar operations take a literal `calendar.id@version`, embedded snapshot,
holidays, working days/hours and fiscal start. Business hours use [open,close); adding
business days excludes the start date and zero preserves it. Fiscal year identifies
its starting year. DST folds are unambiguous because inputs are UTC instants; no
ambiguous/nonexistent local-wall-time-to-instant conversion operator is offered.

Go's host/environment timezone lookup is unsuitable for pure deterministic rules.
Vendor the managed Go 1.27.2 zoneinfo archive, retaining its license and SHA256
`b2d18a7c8fa8142097a48c99609fb3c92db5ee98bc740294e57eab8ae9f94779`.
Read it only from embedded memory via LoadLocationFromTZData. Every DSL2 plan binds
this hash. A future timezone update needs an explicit supported contract/version
and preservation of old snapshots; it must not replace bytes under this contract.

Limits: 256 AST nodes, depth 16, 32 fields, 8 calendars, 512 holidays per calendar,
64 homogeneous scalar list items, 16 boolean operands, 4096 string bytes, 256 KiB
plan/result, 4 MiB total application IR, +/-3660 date/business-day delta, +/-3660 days of elapsed seconds and
50,000 shared evaluation work units. Missing optional fields become typed null;
missing required accessed fields fail. Null equality/is_null are defined; other
null operands fail. AND/OR/IF/COALESCE are lazy. Invalid supplied inputs fail even
if a branch would not use them. Field validation order is stable.

Development/test preview can evaluate a stored record using its compiled plan.
The request supplies only record_id; source, entity and field inputs come from the
trusted preview Program and SQL scope. It enforces rule permission plus existing
entity/row/field permissions, SQL DataScope, RLS and audit-before-response. Literal
rules still require an authorized record and a core/first-field anchor projection.
No automatic write, workflow start, promotion or side effect follows a result.

Migration 00017 widens only the platform metadata-version allowlist and derives the
column from the inserted envelope in an invoker trigger. Runtime column privileges,
RLS and immutable history remain enforced. Quiesce authoring writes for the brief
constraint/trigger lock. Recovery is forward-only; no existing row is rewritten.

Verify full old suites, independent adversarial review, arithmetic properties,
actual fuzz campaigns, stored-plan tampering, timezone environment independence,
DST/range/budget failures and scoped/audited real-PostgreSQL preview execution.
