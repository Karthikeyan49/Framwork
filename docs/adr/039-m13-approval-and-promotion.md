# ADR039: M13 Approval and Promotion — database-enforced release authority

Status: accepted for implementation on `claude/m13-approval-promotion`.
Date: 2026-10-15. Scope: M13 **control plane** — approval, rejection, activation
and rollback of the server-built candidates of ADR038, with an append-only
activation history. M13 does **not** deliver a production data plane (see §10);
the milestone is therefore reported PARTIAL against the roadmap line
"M13 Approval + Promotion".

## Authorization

On 2026-10-15 the user asked to continue the interrupted work and to "finish the
next milestone too" after M12. M13 is the next roadmap milestone
(`docs/v3/roadmap/50_RESTRUCTURED_MILESTONES.md`). Foundation items R3b–R3d,
R5d, R5e, R6 and R7 are still open (ADR036); that is why the production data
plane is excluded (§10).

## Context — what exists and what is wrong with it (verified, not assumed)

M6R built Go-side `Approve`/`ValidateActivation`/`Rollback` and direct table
writes. Verified against the migrated database:

- `framework_app` holds `INSERT` on `control.release_approvals` and
  `control.active_releases` and `UPDATE(artifact_hash, generation)` on
  `control.active_releases`.
- The only database rules are two RLS policies: `approval_insert` (a high-risk
  body cannot be approved by its `RequesterID`) and `active_write` (some approval
  row exists for the new hash). Nothing checks risk-based quorum, acceptance
  author or sandbox freezer exclusion, staleness, base/generation, rejection,
  that a rollback target was ever active, or that an approver is still a member.
- All other checks (`Approve`, `ValidateActivation`, `RollbackPlan`) run in the
  API process, so a buggy or compromised API runtime could approve anything and
  point the release pointer at any approved hash, including a stale or
  never-activated one.
- `Store.Approve/Activate/Rollback` take caller-built `Candidate` values and a
  `Gate` that the composition root can set to `true`.

ADR038 deliberately left authority enforcement to M13: separation of duties,
quorum, staleness re-evaluation inside the approval and activation transactions,
and recomputation of the candidate from its stored bytes.

## Decision

### 1. Layering

1. **Policy (Go):** the API runs `policy.Decide` for a new action before any work.
2. **Recompute (Go):** every approve/reject/activate/rollback request loads the
   candidate with `promotion.Store.LoadCandidate` (which `Restore`s: risk,
   verification, diff, dependency hashes are recomputed from the stored bytes and
   must match) and evaluates `promotion.Staleness` **in the same REPEATABLE READ
   transaction**. A candidate that fails `Restore` or is stale is refused before
   the port is called. **No handler compiles source or IR** ("no recompile after
   approval"): only stored bytes, the operator tool catalog and the live policy
   role snapshot are read.
3. **Database ports (SQL):** new SECURITY DEFINER ports (owner
   `framework_schema_owner`, `search_path=pg_catalog,pg_temp`, `EXECUTE` only for
   `framework_app`) enforce everything the database can know from DB-stamped rows,
   so a compromised API still cannot exceed the floors in §3–§5. What the database
   cannot recompute (verifier report, policy bundle hash, tool pins) stays a Go
   recomputation backstop, exactly as in ADR038 §6; this is a documented residual.
4. **Tables:** `framework_app` loses direct `INSERT`/`UPDATE` on
   `control.release_approvals` and `control.active_releases`; all release
   authority tables are append-only (or CAS pointer) and written only by ports.

### 2. Data model (migration 49, additive, forward-only)

- `control.release_rejections(tenant_id, app_id, artifact_hash, actor_id uuid
  DEFAULT iam.authenticated_actor(), reason text, note text, rejected_at)` —
  PK `(tenant_id, app_id, artifact_hash, actor_id)`; FK to `release_bundles`;
  `reason IN ('REQUIREMENTS_NOT_MET','RISK_UNACCEPTABLE','EVIDENCE_INSUFFICIENT',
  'SUPERSEDED','WITHDRAWN','OTHER')`; `note` ≤ 280 chars, no control characters.
  Forced tenant RLS; immutable (UPDATE/DELETE/TRUNCATE trigger); no `framework_app`
  DML.
- `control.release_history(tenant_id, app_id, seq bigint, kind text
  ('ACTIVATE'|'ROLLBACK'), artifact_hash, previous_hash text (empty for the first
  activation), generation bigint, actor_id uuid DEFAULT iam.authenticated_actor(),
  at timestamptz DEFAULT clock_timestamp(), risk_level text, storage_class text,
  plan_hash text)` — PK `(tenant_id, app_id, seq)`, UNIQUE `(tenant_id, app_id,
  generation)`, FK to `release_bundles`, `seq` = previous max + 1 under the row
  lock of §4. Forced RLS; immutable; no `framework_app` DML.
- `control.release_approvals` (existing, PK `(tenant, app, hash, actor)`) and
  `control.active_releases` (existing pointer + generation) stay; their
  `framework_app` write privileges (table and column level) and the permissive
  `approval_insert`/`active_write` policies are removed. Rows created by M6R are
  untouched. TRUNCATE guards and `control.worker_grants_safe()` cover the new
  tables, and workers get no access.
- Existing activations have no history row: they can be rolled back **from**, but
  are not valid rollback **targets** until they appear in history.

### 3. Approval and rejection (`control.approve_release`, `control.reject_release`)

`control.approve_release(p_app text, p_hash text) RETURNS integer` — returns the
number of distinct eligible approvals recorded for the hash after the call.
Checks, all from DB-stamped data (`iam.current_tenant_id()`,
`iam.authenticated_actor()`, `control.release_candidates`, `iam.memberships`):

1. `control.release_actor_ok(tenant)` — active non-`viewer` member of the scope.
2. The candidate row exists for `(tenant, app, hash)`; it is a 2.0.0 candidate
   (M6R 1.0.0 rows have no candidate row and can no longer be approved).
3. No rejection row exists for the hash (SQLSTATE 55000).
4. The candidate's base is still current: `release_candidates.base_release` /
   `base_generation` equal the active pointer (or both empty/0 with no pointer).
   Otherwise 55000 `candidate is stale`.
5. Database floor separation, by `release_candidates.risk_level`:
   `LOW`/`MEDIUM` none; `HIGH` the actor ∉ {`requester_id`, `acceptance_author`};
   `CRITICAL` the actor ∉ {`requester_id`, `acceptance_author`,
   `sandbox_frozen_by`}. Violation: 42501. (Go applies the possibly stricter
   `Separation` of the restored bundle with `risk.CheckSeparation`.)
6. Insert the approval (idempotent: a repeat by the same actor returns the same
   count; a unique violation is handled, not surfaced).

`control.reject_release(p_app, p_hash, p_reason, p_note) RETURNS boolean` — true if
newly recorded, false if the same actor already rejected. Same steps 1–2; no
separation exclusion (a veto is always safe, so the requester may withdraw).
Rejection is bound to the exact hash and permanent: approve and activate refuse
a rejected hash; an identical rebuild has the same hash and stays rejected, so a
new attempt must change a requirement, acceptance, source or sandbox input.

### 4. Activation (`control.activate_release`)

`control.activate_release(p_app text, p_hash text, p_expected_base text,
p_expected_generation bigint) RETURNS bigint` — returns the new generation.
Takes `SELECT … FOR UPDATE` on the app's `active_releases` row when present
(serialises with rollback), then checks:

1. `release_actor_ok`; candidate row exists; not rejected.
2. `p_expected_base` and `p_expected_generation` equal both the candidate's bound
   base (`base_release`, `base_generation`) **and** the current pointer (`''`/0
   with no pointer for a first release). Any mismatch: 55000. A candidate that is
   already active is therefore refused as stale (409), not re-applied.
3. Quorum from the database's own floors: required approvals = 2 for `CRITICAL`,
   else 1; counted approvals are distinct actors that (re)satisfy the §3 floor
   exclusions and are **still** active non-`viewer` members at activation time.
   Fewer: 55000 `approval quorum not met`.
4. CAS write: `INSERT … ON CONFLICT DO NOTHING` for a first release, else
   `UPDATE … WHERE artifact_hash = base AND generation = expected` with
   `generation = generation + 1`; exactly one row must change (else 55000).
5. Append the `ACTIVATE` history row (`previous_hash`, `generation`, actor,
   `risk_level`, `storage_class` copied from the candidate row) and return the
   new generation.

Go additionally requires the restored bundle's own `Separation` to hold
(`risk.CheckSeparation(sep, parties, approvers)`), `Staleness` to be empty, and
the candidate not to be a 1.0.0 bundle. Activation of a candidate whose
`storage_class` is not `EQUAL` (and not a first release) is allowed, because
risk already rates it HIGH/CRITICAL (volume UNVERIFIED) and requires independent
approval, but **M13 does not execute storage migrations**; the history row
records the class and the pointer is *desired state*. The future production data
plane MUST refuse to serve a release whose storage generation is not provisioned
and bound (§10).

### 5. Rollback (`control.rollback_release`)

`control.rollback_release(p_app text, p_target text, p_expected_current text,
p_expected_generation bigint, p_plan_hash text, p_plan_ack boolean) RETURNS bigint`:

1. `release_actor_ok`; the pointer equals `p_expected_current` at
   `p_expected_generation` (row locked `FOR UPDATE`); target ≠ current.
2. The target appears in `control.release_history` as a previously activated
   release (an approved-but-never-activated artifact is **not** a valid target,
   closing the M6R gap that allowed the pointer to jump to any approved hash).
3. Class gate: read `Risk.Rollback` from the stored body of the **current**
   release. If it is `POINTER_SAFE` no plan is needed. Otherwise (`DATA_NOT_REVERSIBLE`,
   `EXTERNAL_EFFECTS`, a 1.0.0 body or anything unreadable) `p_plan_ack` must be
   true and `p_plan_hash` a 64-hex hash. Go computes `p_plan_ack` as
   `storage_compatible && external_effects_reviewed && !recovery_required`; a
   plan that requires recovery is refused in Go and the port alike.
4. Move the pointer (`generation + 1`), append the `ROLLBACK` history row with the
   plan hash, return the new generation.

Rollback never re-runs staleness (an older release is by definition built against
an older world); candidates built on the replaced base turn stale
(`BASE_CHANGED`) and must be rebuilt. An execution pin retains its release
through rollback (M6R `ExecutionPin`, unchanged).

### 6. Policy actions and defaults

`releases.read` (all roles), `releases.approve` (approve and reject),
`releases.activate`, `releases.rollback` — the last three owner/admin by default;
stored custom roles may include them, never a role named `viewer`
(`supportedRoleAction`). Native policy version becomes `m13-native-v10`;
retained evidence keeps its original version. Resource type is `preview` with the
route's `app` (same as ADR038 actions). Separate actions let a tenant split
approving from activating.

### 7. API (preview-gated, exact-pattern routes, strict bodies)

| Route | Action | Body |
|---|---|---|
| `POST …/preview/{app}/candidates/{candidate}/approve` | `releases.approve` | `{"artifact_hash"}` must equal the path hash |
| `POST …/candidates/{candidate}/reject` | `releases.approve` | `{"artifact_hash","reason","note"}` |
| `POST …/candidates/{candidate}/activate` | `releases.activate` | `{"artifact_hash","expected_base","expected_generation"}` |
| `POST …/preview/{app}/releases/rollback` | `releases.rollback` | `{"target","expected_current","expected_generation","plan":{"storage_compatible","external_effects_reviewed","recovery_required","plan_hash"}}` |
| `GET …/preview/{app}/releases` | `releases.read` | history, newest first, ≤ 50 |
| `GET …/preview/{app}/releases/active` | `releases.read` | active candidate view + generation |

`GET …/candidates/{candidate}` (ADR038) gains `status`
(`STAGED|APPROVED|REJECTED|ACTIVE|SUPERSEDED`, derived), `approvals`
(actor id + time), `approvals_required`, `rejections` and `active`.
Authority-bearing fields (actor, risk, parties, quorum, generation) are never
accepted from a request. Error mapping extends `releaseReply`: SQLSTATE `42501`
→ 403 (`separation_of_duties` names the excluded **role**, never another actor's
id), `55000`/`40001`/`23505`/`23503` → 409, `22023` → 400, `P0002` → 404;
a stale candidate → 409 `candidate_stale` with sorted reasons; a candidate that
fails `Restore` → 409 `candidate_failed_verification` (details audited, not
returned). Every rejection rolls back and is audited. Success actions:
`release.approved`, `release.rejected`, `release.activated`, `release.rolled_back`,
with artifact hash, risk level, generation and counts in the audit state (no
free-text note in audit).

### 8. Concurrency and idempotency

All handlers run in the API's REPEATABLE READ transaction. A serialization or
deadlock failure is a 409 with no automatic retry; the caller re-reads. Approve
and reject are idempotent per actor. Activation and rollback are compare-and-set
on `(hash, generation)`; of two racing activations exactly one succeeds. Approval
racing activation is safe because activation re-counts approvals under the lock.

### 9. Removal of the M6R write paths

`promotion.Store.Approve/Activate/Rollback` (direct DML with a caller `Gate`) and the
M6R pure `Approve`/`ValidateActivation`/`Approval` token are removed: the runtime
role no longer has the privileges they need. `Gate` and `Store.Stage` (ADR038) stay
unchanged, as do `Candidate`, `Restore`, `ExecutionPin`, `Store.Active` /
`ActiveBase` (they read the pointer) and `CheckActiveCompatibility`.
`cmd/release-check` keeps rejecting unsupported active IR.

### 10. Non-goals — the production data plane is NOT part of this ADR

Explicitly out of scope and unimplemented: production datasets and an execution
environment `production`; executing storage migrations / evolution plans at
activation; release-pinned production routes for data, rules and workflows;
production event consumers and tool execution (config still rejects
`WORKFLOWS_ENABLED`/`EVENTS_ENABLED` in production); volume measurement
(candidates keep `UNVERIFIED` volume); a separate verifier credential; approval
expiry; configurable approver roles (R5e); outbox events for release lifecycle;
UI. These depend on the open foundation items and on independent review of
M10–M13, so they are a later, separately authorised slice ("M13c"). Until it
exists an activated release is an auditable pointer and nothing serves it.

## Amendments recorded at implementation (2026-10-16)

These supersede the matching sentences above where they differ.

1. **Candidates are loaded only through `Store.LoadCandidate` (§1 step 2).** `Restore`
   cannot recompute the diff, storage class or evolution plan (they depend on the base
   release); the independent review finding M12-F1 showed a self-consistent forged body
   restores. `LoadCandidate` therefore also runs `Candidate.VerifyWithBase` against the
   candidate's own base release, and every M13 handler loads through it. A failure is a 409
   `candidate_failed_verification`; a 1.0.0 bundle (no candidate row) is a 404.
2. **Extra database guards (migration 49, §2).** Beyond the ADR: `control.active_releases`
   refuses DELETE and TRUNCATE; an UPDATE must change the hash and raise the generation by
   exactly one; a deferred constraint trigger refuses, at commit, any pointer INSERT/UPDATE
   without the matching `release_history` row; `release_history` must extend its previous
   entry (seq, generation and previous hash step by one); INSERTs into
   `release_approvals`, `release_rejections` and `release_history` are refused unless they
   come from a `framework_app` session running inside a port with the authenticated actor.
   A fixture can therefore no longer write a pointer or an approval as the operator.
3. **Storage class source (§4).** `release_candidates` has no storage class column, so
   activation reads the lower-case `Storage.class` key of the stored body and refuses
   anything unreadable. A rollback history row copies risk level and storage class from the
   target's latest history row.
4. **Separation signalling.** A separation violation is `42501` with DETAIL exactly
   `requester`, `acceptance_author` or `sandbox_freezer`; the API maps that to 403
   `separation_of_duties` and names the excluded role, never an actor id. Racing
   activations under REPEATABLE READ may lose with `40001` (also 409).
5. **Worker drift guard.** `control.worker_grants_safe()` now tests
   `SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER` on the listed tables (the M12 DB
   review found REFERENCES/TRIGGER grants went unnoticed) and lists the four release
   authority tables.
6. **Ports read the jsonb view.** `stage_candidate` (migration 48) and the activation and
   rollback ports read the stored body through `jsonb`, where the last of duplicate keys
   wins. The database does not enforce canonical JSON; `Restore` (canonical re-encoding) is
   the backstop and runs before any decision reaches a port. A compromised API process can
   still stage and drive a self-consistent forged body through the raw ports: the adversarial
   suite records exactly which of its attacks the database stops and which only Go stops.
7. **API details (§7).** Statuses: approve 201 (200 for the same actor again), reject 201
   (200 when already rejected), activate and rollback 200 with a release view. Additional 409
   codes from Go pre-checks: `candidate_rejected`, `approval_quorum_not_met`, `base_changed`,
   `release_changed`, `no_active_release`, `rollback_target_is_current`,
   `rollback_target_not_released`, `rollback_plan_required`, `recovery_required`; 400:
   `hash_mismatch`. A supplied rollback plan must be fully acknowledged even for a
   POINTER_SAFE release. Body keys must be spelled exactly in lower case. Idempotent replays
   are audited as `release.approval_replayed` / `release.rejection_replayed`; refusals as
   `releases.<action>.rejected`.
8. **Go store.** The rejection type is `promotion.ReleaseRejection` (`Rejection` is the M12
   build-rejection error). Store methods return typed errors (`ErrConflict`, `ErrForbidden`,
   `ErrInvalid`, `ErrNotFound`, `ErrRecoveryRequired`, wrapping `*PortError` so the SQLSTATE
   stays reachable) and do not write audit; the handler's evidence entry does, in the same
   transaction.
9. **Rollback targets (§5).** A release that was rejected after it ran is not a rollback
   target: the port refuses it (`55000`) and the route answers 409
   `rollback_target_rejected` (found by the adversarial suite).
10. **Decoder hardening found on the way.** `metadata.DecodeJSON` now rejects keys that differ
   only by Unicode case folding (they previously decoded to the last spelling).
11. **Bounded recomputation (after the M12 API review, F9; ADR038 Amendment 10).** Approve,
   reject, activate and rollback load the candidate through the same memoised
   `LoadCandidate`. A cold recomputation beyond the per-tenant or per-process bound is refused
   before any port call with 429 / 503 `release_busy` and `Retry-After`, audited with the closed
   refusal code `RECOMPUTE_BUSY_TENANT|PROCESS`; it decides nothing, so the routes stay fail
   closed (`TestM12APIColdRecomputationIsBoundedTypedAndAudited`). Staleness, approvals and the
   pointer are read again on every request and never remembered.

12. **Independent review fixes (2026-10-19; `docs/verification/M13_INDEPENDENT_REVIEW.md`, migration 53).**
   - *M13R-01, a veto is not overtaken (§3, §4, §8).* `reject_release` and `activate_release` both write
     one `control.release_fences` row per hash (an upsert that bumps a counter). Under REPEATABLE READ the
     transaction with the older snapshot therefore aborts with `40001` (mapped as the other concurrency
     refusals) instead of committing next to a rejection it could not see; a lock-only fence does not
     have this effect. Different hashes use different rows and never conflict. `approve_release` does not
     write the row: an approval recorded beside a committed rejection is harmless because activation reads
     the rejection again. A rejection that loses to a committed activation fails with `40001` and succeeds
     on retry (a release can be vetoed after it ran). Tests: `TestM13R01*`.
   - *M13R-02, the rollback class gate covers the span (§5).* The gate reads the `Risk.Rollback` class of the
     current release and of every history entry newer than the target's latest entry
     (`control.release_rollback_class`); anything but exactly `POINTER_SAFE` among them needs the acknowledged
     plan with its hash. The handler assesses the same span (at most `MaxRollbackSpan` = 32 releases; a longer
     span counts as unreadable and needs a plan); the port is the authority. Tests: `TestM13R02*`.
   - *M13R-04, keys are plain ASCII (§7).* `encoding/json` matches field names under Unicode folding (U+017F
     folds to "s", U+212A to "k"), so "every key spelled exactly" was false for those. The strict reader now
     refuses any object key that is not ASCII `[a-z0-9_]` before decoding.
   - *M13R-05, invisible marks (§3).* Rejection notes may not contain U+061C, U+200B, U+200E, U+200F or
     U+FEFF (Go, the port and the table constraint; older rows are not re-validated). The zero width joiners
     U+200C and U+200D stay legal because emoji sequences and several scripts need them.
13. **M13R-03 is fixed in the Go layer; residuals of the same review.** *M13R-03 (fixed, 2026-10-19):* the
   database floor still counts every approver who is an active non-viewer member, and the port cannot evaluate
   the policy. The handler now narrows the recorded approvers, after the separation bars, to those whose
   CURRENT role grants `releases.approve` on the application (`promotion.Store.ApproverRoles` reads the
   roles in the request's transaction; `policy.RoleStore.Holds` answers from the stored role head, an explicit
   DENY winning, and `policy.NativeHolds` for deployments without a role loader). The narrowed list decides
   the quorum at activation and the derived status of an undecided candidate, so the view and the refusal
   agree; the recorded approvals are never changed, which means that restoring the role restores the
   approval. A rejection is not narrowed: a veto stays a veto. The database floor remains the last check
   (the narrowed set is a subset of what the port counts, apart from the `iam.users.active` condition only
   the port reads). The read and the port share the request's REPEATABLE READ snapshot, so a role change
   that commits after the snapshot is first seen by the next request, as for every other policy decision.
   A change of a role's definition already made every candidate stale (`POLICY_CHANGED`); the new check
   covers the reassignment of a member to another role, which changes no role head. Tests:
   `TestM13R03*` (built-in demotion through the role route, deactivation, reassignment between custom roles,
   a CRITICAL quorum with one of two approvers demoted, and a comparison of `Holds` with the real approve
   route over eight roles), `TestNativeHolds`, `TestDefinedHoldsFollowsTheGrants`. `Holds` repeats the grant
   rules of `Defined.Decide` for a member who is not the caller; the comparison test is what keeps the two
   equal. *M13R-06:* a candidate recorded under other runtime components now reads stale
   (`RUNTIME_CHANGED`, M12 API fixes) and the verifier version is part of the components (`verifier-v2`).
   *FR-01 (re-verification):* a worker can set status, termination and failure of its own leased instance
   without a transition; this is the ADR024 residual of a compromised worker inside its exact lease.
   *Informational items I-1..I-8* of the review stand as written there.

14. **Tool-pin staleness is executed through the handlers (2026-10-19).** A candidate whose IR pins an operator tool reads
   stale with `TOOL_CHANGED` when the definition changes and `TOOL_REMOVED` when it disappears, in the view, at approve and
   at activate, and is fresh again when the exact definition returns (`TestM13ToolPin*`). The tenant list belongs to the
   definition hash, so narrowing it is `TOOL_CHANGED`; a build that pins a tool the tenant may not use is refused at
   build (`BLOCK.tool_unresolved`), which leaves `TOOL_TENANT_DENIED` as defence in depth that no API path reaches.

## Consequences

Approval and promotion authority no longer rests on Go code alone: the database now
refuses what a buggy or partly compromised API runtime can do **under its own
identity** — self-approving a HIGH/CRITICAL candidate it requested, skipping quorum,
activating over a moved base, reviving a rejected candidate, moving the pointer
without history, or rolling back to an artifact that was never active (or was
rejected).

Residuals, stated plainly:

- **Identity is bound by the API process.** The ports read the actor and tenant from
  session settings the API sets after verifying a token (AGENTS.md: settings are not
  credentials). A fully compromised API process that knows another member's issuer
  and subject can bind that identity and approve and activate "as" that member, so it
  CAN satisfy separation of duties and CRITICAL quorum by impersonation
  (`TestM13AdversarialCompromisedAPIBindsAnotherMembersIdentity`). Closing this needs
  an approval credential the API cannot mint (a later slice).
- **Forged bodies.** A compromised API can still stage a self-consistent forged body
  (the database cannot recompute the verifier, diff or policy hash); `Store.LoadCandidate`
  (Restore plus base verification) is the only backstop, and it runs in Go before every
  decision (`TestM13AdversarialCompromisedAPIForgedCandidate` records which attacks the
  database stops and which only Go stops).
- **Availability envelope.** Every decision request restores and recomputes the
  candidate inside the 3 s runtime transaction; on a loaded 4-core host with the race
  detector, one read takes about 0.7 s and several parallel approvals of one candidate
  time out with 503 (fail closed, nothing written). A cache or a lighter recompute is a
  later optimisation; it was not measured without the race detector.
- **Independent actors are required** (a HIGH candidate needs requester, acceptance
  author and approver to be three distinct people, CRITICAL needs two approvers beyond
  those), so small tenants must add members.
- Activation without storage execution records intent only (§10).

## Verification and recovery

Unit: quorum/exclusion derivation, status derivation, plan acknowledgement,
policy catalog and defaults. Integration (isolated database): every port against
doctored arguments from each role (viewer, other tenant, worker, requester,
acceptance author, freezer, demoted approver); quorum counting and idempotency;
rejection permanence; activation CAS under two concurrent callers; rollback target
and class gating; direct DML, TRUNCATE and immutability denial for every table and
role; `worker_grants_safe`; SECURITY DEFINER hygiene (owner, `search_path`,
`PUBLIC` execute); API flow with forged fields, role matrix, stale-at-approve and
stale-at-activate (policy, tool, base), and a self-consistent forged candidate
staged directly through `stage_candidate` that `Restore` must refuse at approval.
Migration 49 is additive and forward-only. Recovery: revoke `EXECUTE` on the four
ports (authority stops, history and approvals stay) and forward-fix; never delete
approvals, rejections or history, and never edit an applied migration.
