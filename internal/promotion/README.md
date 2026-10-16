# Release contracts and candidate builder — M6R, M12

Two bundle formats share one hash-bound restore path:

- **1.0.0 (M6R)** — caller-declared fields. Still restorable (stored artifacts and
  `cmd/release-check`), but tenants can no longer stage it.
- **2.0.0 (M12, [ADR038](../../docs/adr/038-m12-candidate-release-builder.md))** — built
  only by `BuildCandidate` from server-derived inputs: the stored source revision,
  the trusted compiler's IR, a stored requirement set and acceptance spec, a frozen
  sandbox evidence, and the active base. `Restore` recomputes everything that does
  not depend on the base — risk facts and assessment from the stored diff,
  requirement coverage, the verifier report, dependency hashes — and rejects any
  difference (about fifty tamper mutations are tested). The diff, storage class and
  evolution plan depend on the base release, so `Restore` checks them only
  structurally; `Candidate.VerifyWithBase` (called by `Store.LoadCandidate` with the
  candidate's own base release) recomputes them. Decide on a candidate only after
  loading it through `LoadCandidate`. `Rejection` carries the verifier report or assessment for FAIL/BLOCKED
  or risk-blocked builds; nothing is stored for them.

`dependencies.go` derives tool pins, the whole-tenant policy bundle hash and the
runtime components hash; `staleness.go` evaluates a candidate against live state
and is never persisted. `release_store.go` reads/writes requirement sets,
acceptance specs and candidates only through SECURITY DEFINER ports that stamp
the authenticated actor; `Store.Stage` calls `control.stage_candidate`, which
binds the body to the stored rows, enforces build-time separation of duties and
writes bundle plus index together. The DB cannot recompute the verifier, so
`LoadCandidate` recomputation (Restore plus the base verification) is the
backstop; M13 must load through it at approval and activation.

Store methods belong inside `WithRuntime` and append audit before commit.
Candidate routes exist only where preview exists (development/test). Approval,
activation, rollback and production volume measurement are M13. A declared PASS in
a payload is not a capability: only the deterministic verifier mints one.
