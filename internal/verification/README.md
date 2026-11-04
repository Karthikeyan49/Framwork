# verification

Deterministic independent-acceptance verifier (`verifier-v4`, ADR102 section 4 and amendments 9, 13 and 14).

`Verify(Input)` is pure: no clock, randomness, network, database or file, no
goroutine, and it never calls `compiler.Compile` (a test enforces this). It takes a
restored `compiler.Program`, a parsed `intent.RequirementSet` and
`intent.AcceptanceSpec`, the storage evolution class, the changed surfaces, the
operator tool catalog and the sandbox evidence hash, and returns a canonical
`Report` bound to all of them (`Report.Hash` = SHA-256 of `Report.JSON`).
`Recompute` re-runs it and requires byte equality.

What it checks, per MANDATORY and RESOLVED requirement:

- `approval_threshold`: the derived boundary probes (one minor unit below/at/above,
  zero, negative, wrong currency, decades, multiples and ratios of the threshold), the
  probes around every money amount the plan's own conditions compare the threshold field
  with (one unit below, at, one above, and the exact midpoints between neighbours and the
  threshold, with as many decimals as the data layer accepts; `literals.go`; repeated, like
  the boundary probes, for the values that the plan's other conditions compare the optional
  fields with) and the human cases are run through the stored
  workflow plan with `workflow.Program.Next` and the rule engine, exactly as the
  runtime would, and compared with the pure oracle (`intent.OracleThreshold`). The plan
  decides what is probed, never what is expected. A plan that compares the field with
  more than 32 distinct amounts is `BLOCKED`. Observed approval roles must equal the
  required roles.
- `approval_gate`: every path to completion must pass an approval state with
  exactly the required roles (memoised walk, linear in states + transitions).
- `tool_effect`: each pinned use resolves in the catalog for the tenant and stays
  within the side-effect, approval and sandbox-mode bounds.
- `data_migration`: the storage class stays within the bound.

Outcomes: `BLOCKED` (cannot be verified: `LEGACY_IR`, `NO_EXECUTABLE_PLAN`,
`UNSUPPORTED_BY_VERIFIER`, `OMITTED_REQUIREMENT`, `OPEN_MANDATORY`,
`FLOOR_VIOLATION`, `INCOMPLETE_CASE_INPUT`, `BUDGET`) beats `FAIL` (the release
differs: `APPROVAL_MISMATCH`, `GATE_BYPASS`, `GATE_ROLES_MISMATCH`,
`TOOL_*`, `MIGRATION_EXCEEDED`) beats `PASS`. Non-mandatory requirements are
reported `SKIPPED`. `CheckCompleteness` makes changed financial rules, approval
gates, tool activities and non-equal storage require a RESOLVED matching
requirement, otherwise `OMITTED_REQUIREMENT`.

Honest limits:

- Examples, not proofs. The mechanical floor is the derived boundary probes plus, for
  every string, enum, boolean and integer field the requirement does not pin,
  repeated probes with the values the route compares against and one value that
  matches none (at most 8 fields and 6 values per field; worlds that fail or cannot
  be evaluated are skipped). It catches exemptions and extra branches on unpinned
  fields and caps on huge orders, and an approval whose rejection still completes.
  It does not mine decimal, money, date or datetime fields other than the threshold
  field, whose amounts are probed as above; a window written with arithmetic
  (`amount * 2 > 1252`) instead of with the amounts of its edges is not found; and a
  bypass that lives only in a world the requirement pins away (for example
  `note = "vip"` when the requirement pins `note = "regular"`) is outside what the
  requirement promises; the independent acceptance author is expected to add that case.
- A window gated on ONE other unpinned optional field is found (a mined variant sets exactly one field beyond the requirement's); a window gated on TWO such
  fields (`note = "gold" and region = "EU"`) is not (independent review TF-03: FAIL with one gate, PASS with two). Mining the combinations inside the work
  budget, or BLOCKing a plan whose amount-reading route is gated on two or more such fields, are the two directions; neither is done (ADR102 amendment 14).
- The report is replayable evidence derived from stored bytes, not an attestation
  by an independent credential. Approval and activation (M13) must recompute it.
- Observed approver roles must EQUAL the required set (a deliberate tightening of
  a superset reading): an approval open to more roles than the requirement names
  fails.
