# ADR038: M12 Candidate Release Builder — server-built, recomputable candidates

Status: accepted for implementation on `claude/m12-candidate-builder`.
Date: 2026-10-15. Scope: M12 (candidate bundle assembly, server-derived risk,
independent acceptance verification, staleness). M13 owns approval, activation
and rollback. M14 owns AI intent capture. No production exposure.

## Authorization

On 2026-10-15 the user asked to "complete the other milestone too" after
authorizing M10 and M11 (ADR036/037). M12 is the next roadmap milestone
(`docs/v3/roadmap/50_RESTRUCTURED_MILESTONES.md`), so this is read as M12. No
other milestone, approval service, activation, or production path is authorized.
The foundation gate caveats of ADR036 still apply (R3b–R3d, R5d/R5e, R7 open).

## Context

M6R delivered a hash-bound bundle contract (`internal/promotion`, bundle 1.0.0)
and an operator compatibility gate, but nothing builds a candidate: every
authority-bearing field (`Verification.Outcome == "PASS"`, `HighRisk`,
`Acceptance.Provenance`, `AuthorActorID`, dependency hashes, sandbox context) is
caller-declared, `framework_app` can INSERT any hash-consistent body into
`control.release_bundles`, there is no diff, no risk classifier, no executed
acceptance and no staleness evaluation. Research reports behind this ADR cover
the requirement/promotion inventory, the verification authority model and the
risk/diff/staleness design; their citations are in the M12 implementation report.

## Decision

### 1. Candidate = bundle 2.0.0, assembled only on the server

`internal/promotion` gains bundle **2.0.0** as new types with version dispatch in
`Restore`. Bundle 1.0.0 bytes, hashes and restore behavior are unchanged
(stored bundles and `cmd/release-check` keep working); 1.0.0 stops being
stageable by tenants. A candidate is built from a strict request
`{revision, requirement_set_version, acceptance_spec_id, sandbox_id,
evolution_intent?, expected_base?}`. Unknown fields are rejected; requests naming
`outcome`, `high_risk`, `requester`, `provenance`, `verification`, `dependencies`,
`risk`, `sandbox` or `author` fail with 400. Everything else is derived inside one
REPEATABLE READ transaction:

1. source and builder identity from `control.metadata_versions` (DB-stamped);
2. the IR from the **trusted compiler** with the operator tool catalog (never a
   caller-supplied IR), required to be `Compile`-minted;
3. the **frozen** sandbox evidence for `sandbox_id` (ADR037), required to match
   the compiled `metadata_revision`, `ir_hash`, target storage schema hash and
   `SYNTHETIC_SEPARATE` data mode;
4. the base release from `control.active_releases` (callers may only assert
   `expected_base`; mismatch is 409);
5. dependencies, evolution plan, diff, capabilities, risk, completeness and the
   verification report, all pure functions of the above;
6. the bundle, staged through a definer port in the same transaction as its audit.

JSON keys `HighRisk` and `RequesterID` stay top-level with their meaning because
migration 11's `approval_insert` policy reads them; `HighRisk` is derived
(`risk level >= HIGH`) and `RequesterID` equals the authenticated actor.

### 2. Dependencies are derived, not declared

- **Policy bundle hash**: sha256 over policy engine/definition versions, the
  action catalog hash and every tenant role head's `(name, version, body_hash,
  active)` sorted by name (`policy.RoleStore.Snapshot`). Whole-tenant on purpose
  (fail closed); it replaces the per-actor `policy_hash` in sandbox evidence for
  staleness (that value stays as audit data).
- **Relevant tool hash**: sha256 of the canonical JSON array of the sorted
  `{ref, version, hash}` pins found in the IR's workflow tool states. Fresh iff
  every pin resolves with `tools.Catalog.Lookup` and `AllowsTenant`. Unrelated
  catalog additions never stale a candidate.
- **Schema snapshot**: `IR.SchemaSnapshot` layout id, the base release's storage
  schema hash (empty for a first release), the target storage schema hash
  and the evolution plan hash.
- **`runtime_contract_version`**: constant `runtime-contract-v1`, with a recorded
  components hash over `StorageContractVersion`, `EvolutionVersion`, supported IR
  versions, workflow and rule DSL versions, `tools.CatalogVersion`, and the diff,
  risk-policy, verifier, vocabulary, requirement-policy, requirement-set and
  acceptance-spec versions (no event envelope version).

### 3. Server-derived risk (`release-risk-v1`)

A pure, versioned, hashed policy (`internal/risk`) maps facts to
LOW < MEDIUM < HIGH < CRITICAL plus a BLOCKED verdict. Facts come only from the
restored base program, the server-compiled target, the evolution plan, the tool
catalog and a server-measured record count — never a request. Rules: page-only
and label-only changes LOW; entity/relationship/rule/workflow changes MEDIUM;
financial rules, approval-gate changes, WRITE tools (and anything inheriting
them) HIGH; WRITE tools without idempotency CRITICAL; storage class × volume
matrix (EQUAL LOW; ADDITIVE LOW→HIGH by volume; TRANSFORM MEDIUM→CRITICAL;
BREAKING blocked); unknown kinds HIGH; unclassified capability blocked; first
release floors at MEDIUM. Monotone join (max); no downgrade rules. Volume buckets
NONE/SMALL (≤10k)/LARGE (≤100k)/UNVERIFIED are measured by a tenant-scoped
definer function independent of the caller's DataScope; unknown volume is
UNVERIFIED. The policy table is data hashed into the bundle; `Restore` re-runs
the **named** policy version over the stored facts and requires byte equality;
a newer policy raising the level makes the candidate stale (`RISK_ESCALATED`).
Separation: LOW/MEDIUM none; HIGH excludes requester and acceptance author as
approver; CRITICAL needs two distinct approvals excluding requester, acceptance
author and sandbox freezer. Enforcement is M13; M12 records it.

### 4. Requirements and independent acceptance

- **Requirement sets** (`requirements-v2`) are typed, versioned, CAS-headed
  artifacts authored by an actor holding `requirements.decide`, stamped with the
  DB actor. Categories verifiable in M12: `approval_threshold` (money
  comparison on an entity field → approval by named roles), `approval_gate`
  (every start→complete path passes an approval state with named roles),
  `tool_effect` (a pinned tool's catalog side effect, approval and sandbox mode
  stay within bounds) and `data_migration` (evolution class stays within a
  bound). Any other category cannot be MANDATORY-satisfied: the candidate is
  BLOCKED (`UNSUPPORTED_BY_VERIFIER`). Requiredness floors come from the
  versioned code-defined `requirement-policy-v1` (operator-owned, hashed): a set
  cannot mark a floor-mandatory category optional, and an open or rejected
  mandatory requirement blocks.
- **Completeness is diff-driven**: every changed financial rule, approval
  gate, tool activity or non-EQUAL storage class must be covered by a RESOLVED
  requirement of the matching category, otherwise `OMITTED_REQUIREMENT` blocks.
- **Acceptance specs** (`acceptance-v2`) are immutable, DB-stamped authored
  artifacts that bind only `requirements_hash` (they may not reference IR,
  source, compiler or artifact hashes), created before any candidate that uses
  them. Cases are typed, closed vocabulary, no code: expected outcomes
  `NO_APPROVAL`, `APPROVAL_REQUIRED(roles)`, `INPUT_REJECTED`, plus derived
  probes. The server derives boundary probes mechanically from each
  requirement (threshold minus/at/plus one minor currency unit from
  `erp.Money`, zero, negative, wrong currency) from an **oracle that never reads
  the IR**; human cases may add but never remove probes and are checked against
  the oracle at authoring time.
- **Verifier**: a deterministic pure function over the exact stored IR bytes,
  using `compiler.Restore` programs, `rules.Program.Evaluate` and
  `workflow.Program.Next` (no clock, network, database or compile). Output is a
  canonical report bound to requirement-set hash, acceptance hash, IR hash,
  sandbox evidence hash, verifier version and vocabulary version. Outcome is
  PASS, FAIL (case differs) or BLOCKED (cannot be verified: legacy IR,
  non-built-in approver role, unsupported trigger, unsupported category).
  Only PASS is stored; FAIL/BLOCKED return the report (HTTP 422) and are audited as
  `candidates.build.rejected` with the report hash (Amendment 8).
- **Honest limit**: this is deterministic, replayable evidence, not an
  attestation by an independent credential. `Restore` recomputes it, and M13 must
  recompute it at approval and activation. A separate verifier principal
  (credential-level independence) is the recommended M13/M15 hardening; the
  bundle already carries the verifier version and evidence hash it would bind.

### 5. Separation of duties at build time (DB-enforced in the stage port)

`RequesterID` = authenticated actor; acceptance author ≠ requester; acceptance
author ≠ author of the compiled source revision; requirement decider may equal
neither author nor requester only where the tenant has more than one eligible
actor (no waiver mechanism is built); the database requires an active non-viewer
member and the API policy requires the `candidates.build` capability. Author attestation is by authenticated
actor, not proof of human authorship; documented residual.

### 6. Staging authority

Migration revokes `framework_app`'s direct INSERT on `control.release_bundles`
and adds TRUNCATE guards on bundles/approvals. `control.stage_candidate` is a
SECURITY DEFINER port (owner `framework_schema_owner`,
`search_path=pg_catalog,pg_temp`, execute only `framework_app`) that checks
tenant scope, active non-viewer actor, content hash, version 2.0.0, app id equals
the body's top-level `AppID` equals the route app (IR/app agreement is checked by
`Restore`), `RequesterID` equals the actor, the requirement set /
acceptance spec / sandbox evidence rows exist with matching hashes and DB
authors, the source revision exists, and the base is the current active release
or absent. `control.release_candidates` records derived columns for cheap list
and staleness queries. Because the DB cannot recompute the verifier, a
compromised API process can still stage a self-consistent forged body; `Restore`
recomputation at approval is the backstop (see §4 limit).

### 7. Staleness is derived, never stored

`Staleness(bound, current)` returns sorted reasons: `BASE_CHANGED`,
`BASE_GENERATION_ADVANCED`, `SCHEMA_CHANGED`, `POLICY_CHANGED`, `TOOL_CHANGED`,
`TOOL_REMOVED`, `TOOL_TENANT_DENIED`, `RISK_ESCALATED`, `EVIDENCE_INVALID`,
`UNSUPPORTED_RUNTIME`. The detail route returns `stale` and `stale_reasons` (the list route does not);
M13 re-evaluates inside its
approval and activation transactions. Source head moving is not stale; a stale
candidate is rebuilt to a new hash and never revived.

### 8. Patch/rebase hardening (pure)

`ApplicationPatch.Rebase` derives change class server-side (PRESENTATION only
for `page` on all three sides), applies a true three-way matrix (convergent
changes merge), emits typed conflict categories, and never auto-applies
non-page definitions. The merged document must be appended as a new metadata
revision and recompiled/re-sandboxed/re-accepted; rebase never preserves
approvals. Field-level merges and AI patch endpoints are deferred (M14/M16).

### 9. API and policy

Preview-gated (development/test) routes, exact-pattern matched:
`POST|GET .../requirements`, `POST|GET .../acceptance-specs`,
`POST .../candidates`, `GET .../candidates[/{hash}]` (derived staleness).
Actions: `requirements.read|decide`, `acceptance.read|author`,
`candidates.read|build`. Reads for all roles; writes owner/admin by default,
delegable, never a role named `viewer`. Policy version bump.

### 10. Non-goals

Approval, activation, rollback, executing migrations, a production dataset or
runtime, AI capture (M14), Level-2 sandbox end-to-end acceptance, redacted
snapshots, custom approval roles (R5e), outbox events for release lifecycle,
field-level rebase.

## Amendments recorded at implementation (2026-10-15)

These supersede the matching sentences above where they differ; the design
intent is unchanged.

1. **Volume source (§3).** No volume definer function was built. Until M13 can
   measure a production dataset, `promotion.DefaultVolume` supplies the bucket:
   a first release has no data (`NONE`); every later release is `UNVERIFIED`,
   which the risk policy rates at the top of its band. The volume value is part
   of the hashed risk facts, so `Restore` still recomputes it identically.
2. **One strict restore path (§1, §6), completed against the base (corrected after
   independent review M12-F1).** `promotion.Restore` dispatches on the bundle
   version. 2.0.0 bodies are decoded with unknown fields rejected and everything
   that does not depend on the base release is recomputed (facts and risk from the
   stored diff, requirements, acceptance, verifier report, dependency hashes). The
   diff, the storage class and the evolution plan DO depend on the base, so
   `Restore` only checks them structurally: a body whose diff was shrunk and whose
   facts, risk and report were recomputed from the forged diff is self-consistent
   and restores (reproduced by the reviewer and by
   `TestRestoreAloneAcceptsACoordinatedDiffForgeryButVerifyWithBaseRefusesIt`).
   `Store.LoadCandidate` — the only way the API loads a candidate to show or decide
   on it — therefore resolves the candidate's OWN base release (`BaseRelease`, never
   the current pointer) and runs `Candidate.VerifyWithBase`, which recomputes the
   diff, storage class and evolution plan; a failure is a 409
   `candidate_failed_verification`. `control.stage_candidate` still cannot see this
   (it binds only database-known facts), which
   `TestM12CoordinatedForgeryIsRefusedWhenLoaded` demonstrates end to end. Any new
   code that decides on a candidate must load it through `LoadCandidate`. 1.0.0
   bodies stay readable so stored artifacts and `cmd/release-check` keep working,
   but `Store.Stage` refuses them and `control.stage_candidate` accepts only
   version `2.0.0`.
3. **Approver roles compare by set equality (§4).** The verifier requires the
   workflow's approval roles to equal the requirement's roles (canonical sorted
   set), not merely overlap: a route that also admits another role is
   `APPROVAL_MISMATCH`/`GATE_ROLES_MISMATCH`.
4. **Separation of duties as shipped (§5).** The stage port enforces from
   database-stamped identities: requester = authenticated actor; acceptance
   author ≠ requester (R1); acceptance author ≠ author of the compiled source
   revision (R2); frozen sandbox evidence exists, matches the IR/revision/data
   mode and records its freezer (R3, recorded and bound; sandbox freezing is
   `sandbox.manage`, owner/admin only). The requirement decider is recorded
   on the immutable `control.requirement_sets` row the candidate names (by
   version and hash) and the sandbox freezer is bound in the bundle and the
   candidate index; neither is compared to the requester at build. Approval-time exclusion of the requester, acceptance author and
   freezer is M13. No waiver mechanism exists, so a single-actor tenant cannot
   build a candidate.
5. **Changed-surface rule (§4).** `NonEqualStorage` completeness excludes a
   first release (the whole schema is new and has no migration risk); financial
   rules, approval gates and tool activity still require covering requirements.
6. **Role grants (§9).** `requirements.decide`, `acceptance.author` and
   `candidates.build` are owner/admin in the native policy; stored custom roles
   may include them (never `viewer`). `sandbox.manage` is owner/admin only and
   cannot be delegated (`policy.supportedRoleAction`); ADR037 wording is
   corrected accordingly. Native policy version is `m12-native-v9`.
7. **Staging row bindings (§6).** `stage_candidate` additionally binds the
   frozen sandbox evidence text and freezer, the requirement set row hash, the
   acceptance row (author, requirements hash) and the active base
   (`artifact_hash` and generation), and then writes the bundle and its derived
   `control.release_candidates` row in one function call (two INSERTs, one transaction). `control.append_requirements`
   (compare-and-set head) and `control.author_acceptance` (idempotent for the
   same author and content) stamp the decider and author from the database
   actor. Direct INSERT on `control.release_bundles` is revoked at table and
   column level for `framework_app`; TRUNCATE guards cover the new tables,
   bundles and approvals; `control.worker_grants_safe()` includes the new tables.
8. **Audit (§9).** Successful stages record `release.candidate_staged`;
   an idempotent replay records `release.candidate_replayed`; verifier or
   risk rejections (422) and database refusals record `candidates.build.rejected`
   with the report/assessment hashes in the audit state. Requirement decisions
   record `requirements.decide` with the version and hash.
9. **Verifier strengthened after independent review (§4).** The review of the
   deterministic logic (`docs/verification/M12_VERIFIER_INDEPENDENT_REVIEW.md`,
   F2–F5, F7) showed that probes stopping one tick above the threshold, an
   all-null world for unpinned optional fields, and a walk that stopped at the
   first approval state let wrong applications pass. As shipped now:
   (a) an approval whose `rejected` edge can still reach `complete` is observed as
   `NO_APPROVAL(REJECTION_COMPLETES)` for thresholds and as a bypass for gates;
   (b) the derived probes add one decade each way from 1e3 to 1e18 and 10x/1000x
   multiples of the threshold, so an upper cap fails;
   (c) for every string, enum, boolean and integer field the requirement does not
   pin, the below/at/above and 1e6 probes are repeated with the values the route's
   own conditions compare against plus one value matching none of them (at most 8
   fields and 6 values per field); a mined world that cannot be evaluated or that
   ends in `failed` is skipped, never blocking and never a pass; (d) a plan-only
   workflow change around a pinned tool inherits the tool's level (F7).
   Still true: this is a bounded mechanical floor, not a proof. Decimal, money,
   date and datetime fields other than the threshold are not mined, two-field
   interactions beyond one extra value per run are not explored, and a bypass that
   needs a pinned value is outside what the requirement promises.

## Consequences

Candidates become reproducible server-built artifacts whose risk, dependencies
and verification are recomputable from the bytes. Single-person tenants cannot
build candidates until a waiver design exists. Non-EQUAL second releases rate
HIGH/CRITICAL until a production volume source exists (M13). Bundle format 2.0.0
adds ~tens of KiB; the 8 MiB cap holds because the base IR is referenced by hash.

## Verification and recovery

Unit: determinism and hash stability, tamper matrix per bundle field, oracle
tables across 0/2/3-minor-unit currencies, mutation fixtures (GT vs GTE, wrong
role, constant-true rule, bypass transition, wrong currency, unsupported role),
risk golden matrix and exhaustive-kind reflection, staleness per reason,
three-way merge property test. Integration: full source→sandbox→freeze→
requirements→acceptance→build flow; forged PASS/risk/provenance 400; direct
INSERT into `release_bundles` denied (pre-fix hand-built PASS body succeeds, then
fails); separation violations; stale per dependency; Restore recomputation
detects edited risk/report; business-wrong fixtures FAIL. Migrations are
additive/forward-only; recover by revoking EXECUTE on `stage_candidate` (building
stops, bundles remain) and forward-fixing; never delete bundles or evidence.
