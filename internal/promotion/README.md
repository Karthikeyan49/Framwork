# Release contracts, candidate builder and promotion — M6R, M12, M13

Two bundle formats share one hash-bound restore path:

- **1.0.0 (M6R)** — caller-declared fields. Still restorable (stored artifacts and
  `cmd/release-check`), but tenants can no longer stage it, and it has no
  candidate row, so it can no longer be approved or activated.
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
backstop.

## Approval and promotion (M13, [ADR039](../../docs/adr/039-m13-approval-and-promotion.md))

`decisions.go` wraps the four migration-49 ports. Each is SECURITY DEFINER,
owned by `framework_schema_owner`, executable only by `framework_app`, and
derives tenant, actor, membership, risk level, parties, base and generation from
database-stamped rows; the wrappers never pass an actor, a level or a quorum.

| Store method | Port | Database floor (ADR039 §3–§5) |
|---|---|---|
| `ApproveRelease` → approvals | `control.approve_release` | active non-viewer member; 2.0.0 candidate row exists; not rejected; bound base still current; HIGH excludes requester and acceptance author, CRITICAL also the sandbox freezer; idempotent per actor |
| `RejectRelease` → recorded | `control.reject_release` | member; closed reason; note ≤ 280 characters without control characters; permanent for the exact hash; no separation exclusion |
| `ActivateRelease` → generation | `control.activate_release` | pointer row locked; expected base and generation equal the candidate's bound base and the pointer; quorum (CRITICAL 2, else 1) of approvers who still pass the floor and are still members; compare-and-set write plus an `ACTIVATE` history row |
| `RollbackRelease` → generation | `control.rollback_release` | pointer equals the expected current at the expected generation; target appears in history; unless the current release is `POINTER_SAFE`, an acknowledged plan with a hash; `ROLLBACK` history row |

Reads: `Approvals`, `Rejections`, `History` (newest first, at most 50) and
`WasActivated`. Pure rules: `RequiredApprovals`, `FloorRoles`, `FloorExcluded`,
`FloorExclusion` (names the excluded role, never another actor's id),
`CountEligible`, `DeriveStatus` (REJECTED > ACTIVE > SUPERSEDED > APPROVED >
STAGED), `RollbackClassNeedsPlan` and `RollbackPlan.Acknowledged`.

The wrappers reject malformed arguments before SQL (`ErrInvalid`, which wraps
`ErrGate`; `ErrRecoveryRequired` for a plan that needs recovery) and map port
SQLSTATEs to `ErrNotFound` (P0002), `ErrConflict` (55000, 40001, 40P01, 55P03,
23505, 23503), `ErrForbidden` (42501) and `ErrInvalid` (22023). A `*PortError`
keeps the `*pgconn.PgError` reachable with `errors.As`; its `Error()` never
contains driver text.

**Who may call them:** only an API handler that has (1) an ALLOW from
`policy.Decide` for `releases.approve` (approve and reject), `releases.activate`
or `releases.rollback`, (2) loaded the candidate only through `LoadCandidate`
(`Restore` plus `VerifyWithBase` against the candidate's own base release; never
a body taken from the request or read around it) and (3) found `Staleness` empty
and, for activation,
the bundle's own `Separation` satisfied by `risk.CheckSeparation`, all in the
same REPEATABLE READ transaction that then appends the audit entry. The
wrappers do not audit. The database cannot recompute the verifier, the policy
bundle hash or tool pins; `LoadCandidate` and `Staleness` in Go remain the only
backstop against a self-consistent forged body staged by a compromised API
(ADR039 residual).

The M6R write paths (`Store.Approve/Activate/Rollback`, the pure `Approve` /
`ValidateActivation` and the `Approval` token) are removed: the runtime role no
longer holds INSERT/UPDATE on `control.release_approvals` or
`control.active_releases`. `Gate` remains only for `Store.Stage`; `Store.Active`,
`ActiveBase`, `ExecutionPin` and `CheckActiveCompatibility` are unchanged.

## Database ports and the Go recomputation

`control.stage_candidate`, `append_requirements` and `author_acceptance` (migration 00052) accept only a body that is one
object, has no duplicate member at any depth, spells no number with an exponent or leading zero, uses the key sets of the Go
types (`control.candidate_body_keys`, compared with the struct tags by `TestM12RBodyKeyListsMatchTheGoStructs`; add a field to a
2.0.0 struct and the key list must change in a new migration) and embeds the stored source, requirement set and acceptance
spec. An exact replay by the same requester returns `false` whatever the base did since. The database still cannot recompute
risk, verification, the diff or the IR: `Restore` and `VerifyWithBase` (called by `Store.LoadCandidate`) remain the control.

## Not built

The production data plane (ADR039 §10): nothing serves an activated release;
activation records desired state and the storage class but executes no storage
migration; no production datasets, routes, consumers or tool execution; volume
stays `UNVERIFIED`; no approval expiry, configurable approver roles, lifecycle
outbox events or UI. Store methods belong inside `WithRuntime`; candidate and
release routes exist only where preview exists (development/test). A declared
PASS in a payload is not a capability: only the deterministic verifier mints one.
