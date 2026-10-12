# Rule Engine V2 — M7

Compile typed Definition through the application compiler. Runtime receives an
immutable canonical Program restored from its persisted typed plan. Evaluate takes
explicit raw-JSON field inputs and cancellation; it has no clock, data provider,
authorization, network, filesystem, secrets, SQL or tool port. Results are values,
not authority to execute actions. See ADR021 and the M7 verification reports.

Supported types: bool, string, integer, decimal, Money/currency, civil date, UTC
instant, enum, entity reference and homogeneous scalar lists. Missing optional
fields are typed null; required accessed fields must exist. Supplied invalid inputs
always fail. is_null/eq/neq define null behavior; other null operands fail. AND/OR,
IF and COALESCE short-circuit. Input validation and canonical output are stable.

Operators: eq/neq/gt/gte/lt/lte/in/not_in/contains/is_null/and/or/not, if/coalesce,
list, concat/lower/upper/trim, add/sub/mul/div/round/money_round, date_add_days,
date_diff_days, datetime_add_seconds, date_in_zone, business_add_days,
is_business_day, business_is_open and fiscal_year. Functions have closed signatures;
there are no arbitrary function names, SQL, loops, regex expressions or callbacks.

All numeric arithmetic uses signed integer 10^-18 units, magnitude <=10^18.
Multiply/divide round half to even at18 places; add/subtract are exact. Division by
zero/overflow fail. Money add/subtract/comparison require identical currency;
multiply/divide require a scalar. round takes explicit places0–18. money_round uses
reviewed currency minor units; posting rounding and FX conversion are later work.
Numeric result JSON uses strings; Money uses amount/currency objects.

Dates are Gregorian years1–9999. Instants use strict RFC3339 (fraction<=9 digits)
and normalize to UTC. Civil days differ from elapsed seconds. A literal IANA zone
is required for date_in_zone; business functions require literal calendarID@version
bound to a compiled snapshot. Hours are [open,close). Business-day addition excludes
the start day; zero leaves it unchanged. fiscal_year returns its starting year.
UTC instants make DST folds explicit; no local-wall-time-to-instant operator exists.

Limits: depth16/nodes256, fields32, calendars8/holidays512, enums100, scalar list64,
boolean arguments16, string4096 bytes, plan/result256KiB, delta+/-3660 civil/business
days or the same span of elapsed seconds, and shared evaluation work50000. These
fail explicitly and honor cancellation. One work unit is one node or traversed
calendar day; sorted holiday lookup bounds calendar membership checks.
The application compiler additionally caps total IR at4MiB and returns ir_budget;
large repeated field/calendar snapshots may require splitting application scope.

The embedded tzdata/zoneinfo.zip is an intentional runtime data dependency from the
managed Go1.27.2 distribution, with its retained BSD license. SHA256 is
b2d18a7c8fa8142097a48c99609fb3c92db5ee98bc740294e57eab8ae9f94779. Plans pin this hash;
lookup uses only embedded memory, so host TZ/ZONEINFO/zoneinfo do not affect rules.
Future data updates need explicit version compatibility and old snapshot retention.

Compiler/schema1.1 opt-in creates a new revision/IR identity. Existing releases are
never recompiled; legacy1.0 lacks executable DSL2 plans. Development/test preview
evaluates only an actual scoped record selected by UUID, with live action, entity,
row, field and record-specific rule permission plus mandatory audit. No writes or
event delivery occurs during expression evaluation.
