# Rule DSL

Deterministic pure expressions only.

Types:
string, bool, integer/decimal, money, date/time, enum, reference, list.

Operators:
eq/neq/gt/gte/lt/lte/in/not_in/contains/is_null/and/or/not.

Functions start as a small allowlist.

Rules return values/decisions only. Network/DB/tool side effects belong to workflow/tool layer.
