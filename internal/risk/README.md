# risk

Pure, versioned release-risk policy (ADR038 section 3, design appendix D).

`Evaluate(Facts)` maps server-derived facts to a level (LOW < MEDIUM < HIGH <
CRITICAL), a separate BLOCKED verdict, a separation-of-duties requirement and a
rollback class. The package imports only the standard library: it reads no
clock, randomness, database, tool catalog or compiler state. Facts are reduced
from trusted artifacts by the caller (`internal/promotion`); a request never
supplies one.

- The policy `release-risk-v1` is data (`Spec`): a rule table, a storage x volume
  matrix, volume thresholds, per-level separation and rollback rules.
  `Policy.Hash` is sha256 over its canonical document, so any edited row changes
  the hash. Published versions are never edited; change a row by adding
  `release-risk-v2`. The golden test pins the v1 hash and sample assessments.
- Level is the monotone maximum over fired rules. There are no downgrade rules.
  A blocking rule sets `Blocked` and never lowers the level. `NewPolicy` rejects
  specs whose matrix or separation could weaken as a fact is added.
- Invalid facts (unknown enum values, duplicates, more than `MaxEntries` list
  items) are errors, never a default level. `Normalize`/`Canonical` give one byte
  representation per fact set; `CheckCanonical` rejects stored facts that are not
  canonical.
- `Registry.Verify(assessment, facts)` re-runs the policy the assessment names and
  requires byte equality. An unknown policy version fails closed. The built-in
  registry has no mutators.
- `CheckSeparation` is the pure check the approval service (M13) applies:
  distinct approvers, none equal to an excluded party (case-insensitive).
- `VolumeBucket`: unknown, negative or above `LargeMax` counts are UNVERIFIED,
  the top bucket, so uncertainty never rates lower than a measured volume.
