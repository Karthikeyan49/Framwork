# verification

Deterministic independent-acceptance verifier (`verifier-v1`, ADR038 section 4).

`Verify(Input)` is pure: no clock, randomness, network, database or file, no
goroutine, and it never calls `compiler.Compile` (a test enforces this). It takes a
restored `compiler.Program`, a parsed `intent.RequirementSet` and
`intent.AcceptanceSpec`, the storage evolution class, the changed surfaces, the
operator tool catalog and the sandbox evidence hash, and returns a canonical
`Report` bound to all of them (`Report.Hash` = SHA-256 of `Report.JSON`).
`Recompute` re-runs it and requires byte equality.

What it checks, per MANDATORY and RESOLVED requirement:

- `approval_threshold`: the derived boundary probes (one minor unit below/at/above,
  zero, negative, wrong currency) and the human cases are run through the stored
  workflow plan with `workflow.Program.Next` and the rule engine, exactly as the
  runtime would, and compared with the pure oracle (`intent.OracleThreshold`).
  Observed approval roles must equal the required roles.
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
  It does not mine decimal, money, date or datetime fields, and a bypass that
  lives only in a world the requirement pins away (for example `note = "vip"` when
  the requirement pins `note = "regular"`) is outside what the requirement
  promises; the independent acceptance author is expected to add that case.
- The report is replayable evidence derived from stored bytes, not an attestation
  by an independent credential. Approval and activation (M13) must recompute it.
- Observed approver roles must EQUAL the required set (a deliberate tightening of
  a superset reading): an approval open to more roles than the requirement names
  fails.
