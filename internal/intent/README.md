# intent

Business intent and independent acceptance.

- `intent.go` (v1, bundle 1.0.0): `Contract`, `Acceptance`, `Hash`. Untouched;
  stored 1.0.0 bundles recompute `Contract.Hash()`.
- `requirements.go` (v2): `RequirementSet` (`requirements-v2`), typed per category
  (`approval_threshold`, `approval_gate`, `tool_effect`, `data_migration`; any
  other well-formed category is recorded but can never satisfy a mandatory
  requirement). `ParseRequirementSet` is strict: no duplicate, unknown,
  differently cased or missing keys, no trailing data, sorted role lists and
  canonical values; it returns the canonical bytes and their SHA-256. The
  operator-owned, hashed `requirement-policy-v1` sets floors (`FloorFor`,
  `CheckFloors`): a set cannot downgrade a floor-mandatory category.
- `acceptance.go` (v2): `AcceptanceSpec` (`acceptance-v2`) binds only
  `requirements_hash`; its closed schema has no member for an IR, source,
  compiler or artifact hash (structural independence). `ValidateAcceptance` is the
  authoring gate: hash binding, mandatory coverage and oracle agreement.
- `oracle.go`: `OracleThreshold` is a pure exact (`big.Rat`) function of the
  requirement and an input amount; it never reads an IR. `DeriveProbes` adds,
  for every threshold requirement, one minor currency unit below/at/above the
  threshold, zero, a negative amount and a wrong currency. Humans can add cases
  but cannot remove probes, and a case that contradicts the oracle is rejected
  (`ORACLE_DISAGREES`).

Money values in cases and contexts use `{"amount":"500.00","currency":"USD"}`, the
same shape the rule engine accepts. Requirement and case order is part of the
canonical form (it is the authored order); reports sort their output.
