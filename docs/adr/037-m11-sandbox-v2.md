# ADR037: M11 Sandbox V2 — server-owned synthetic sandboxes

Status: accepted for implementation on `claude/m10-m11-tools-sandbox`.
Date: 2026-10-15. Scope: M11 (sandbox identity, data isolation, tool-mode
ceiling, freeze evidence). M12 owns running independent acceptance; M13 owns
approval and activation. M11 never produces a PASS.

## Authorization

Same 2026-10-15 user authorization as ADR036 (M10 and M11 together). M11 reuses
M10's governed gateway; it adds no production data path and no production
credentials.

## Context

V3 18 requires explicit data and capability isolation: "same runtime
implementation, safer context", with data modes (separate synthetic tables,
redacted snapshot, ephemeral dedicated DB) and tool modes (block, simulate,
reroute, read-only, dedicated test account). `internal/sandbox.Context` and
`promotion.Bundle.Sandbox` are caller-declared contracts: the ID, mode and the
"credentials mounted" flag are claims. ADR026 reserved `datasets.environment`
and said M11 must open it; `runtime.datasets` allowed only `preview`.

## Decision

1. **Sandbox = an EMPTY synthetic dataset with a server-owned identity.**
   `runtime.datasets.environment` now admits `sandbox`. A sandbox dataset is
   created only open and empty (BEFORE INSERT guard: state OPEN, no records at
   its independent anchor), never by adopting a preview or legacy namespace, and
   must be registered in `runtime.sandboxes` in the same transaction (deferred
   constraint trigger). `runtime.sandboxes` stores data mode, tool ceiling and
   state; its identity is immutable and rows cannot be deleted. Same runtime, same
   tables, same DataScope/RLS, command receipts, audit and outbox as preview: no
   second code path to drift.
2. **Data modes.** Only `SYNTHETIC_SEPARATE` is admitted (CHECK). Redacted
   snapshots and dedicated databases fail closed until redaction rules (open
   question 102), R5d masking, production data (M13) and operator provisioning
   exist. Seeding uses the governed command route (policy, DataScope, audit,
   outbox, idempotency keys); there is no bulk loader and no production read.
3. **Tool ceiling, enforced twice.** A sandbox declares one ceiling of
   BLOCK < SIMULATE < READ_ONLY < REROUTE < TEST_ACCOUNT. The effective mode is
   `tools.EffectiveMode(definition, ceiling)`: the most restrictive combination
   the tool can actually honor; anything it cannot honor (for example SIMULATE
   without a declared fixture, READ_ONLY on a WRITE tool) is BLOCK. The runtime
   derives environment and ceiling from the leased job through
   `control.worker_environment()` (never from requests, metadata, GUCs or AI), and
   `control.tool_begin` independently rejects a mode above the sandbox ceiling or
   any attempt on a frozen sandbox. LIVE does not exist in M10/M11.
4. **Credentials.** The ceiling can only reduce a tool's declared sandbox mode.
   Sandbox modes use only the tool's sandbox endpoint and sandbox secret
   reference; the catalog forbids a sandbox endpoint on any production host of the catalog
   (on any port) and a sandbox credential equal to any production credential, and
   only TEST_ACCOUNT carries a credential (REROUTE and READ_ONLY send none, also when a
   ceiling narrowed a TEST_ACCOUNT tool).
   The gateway in a sandbox never resolves the production reference (tested with
   a recording resolver).
5. **Freeze and evidence.** `runtime.freeze_sandbox` (owner/admin, `sandbox.manage`)
   atomically records one immutable `runtime.sandbox_evidence` body bound to:
   app, dataset, workspace, active binding and stored IR hash, schema hash and
   generation, anchor, data mode and tool ceiling, a manifest hash of every
   record (id, version, value hash), a manifest hash of the tool ledger, the
   server's tool-catalog hash and a hash of the deciding policy version. It then
   marks the sandbox and dataset FROZEN: record writes, workflow starts and tool
   sends are refused, evolution is impossible, and FROZEN cannot be left. The
   catalog and policy hashes are computed by the server, not request fields (the
   definer port itself takes them as arguments from its API-role caller).
   Evidence is an input to M12; it contains no PASS, risk or approval claim.
6. **Contracts.** `sandbox.Context` and bundle 1.0.0 bytes are unchanged
   (stored bundles must still restore). New fields live in the versioned
   evidence type that a later bundle version references (M12).
7. **API and policy.** `POST .../preview/{app}/sandboxes` (witness of the exact
   compiled IR, workspace UUID, ceiling), `GET .../sandboxes/{id}`,
   `POST .../sandboxes/{id}/freeze`, `GET .../sandboxes/{id}/evidence`. Actions:
   `sandbox.read` (all roles) and `sandbox.manage` (owner/admin only; not delegable to
   custom roles, as `policy.supportedRoleAction` rejects any other role). Sandbox data and workflows use the existing dataset
   binding routes. Routes exist only where preview exists (development/test).
8. **Out of scope.** UI sandbox badge, redacted snapshot/PII scan, ephemeral
   databases, patch/rebase, running acceptance, promotion, production exposure.

## Consequences

Sandbox isolation is enforced by PostgreSQL constraints and ports, not caller
flags. The effective tool mode is deterministic from pinned facts. A frozen
sandbox is reproducible evidence but also permanently read-only; a new sandbox is
needed for further experiments. Sandbox environment is not visible to M8 event
envelopes (they keep mode `preview`); M10 tools run only as workflow activities,
so no event subscriber can send from a sandbox.

## Verification and recovery

Integration tests cover EMPTY-only creation, isolation from preview records,
viewer/cross-tenant denial, replay and ceiling-conflict, direct-DML denial,
freeze/evidence immutability, frozen write refusal, ceiling enforcement against a
forged worker mode, and sandbox tool modes against a fake provider. Migration 46
is additive and forward-only. Recover by revoking `EXECUTE` on the sandbox ports
(creation/freezing stops; evidence stays) and forward-fixing; never delete
evidence or reopen a frozen sandbox.

## Amendments

Date: 2026-10-19. Trigger: `docs/verification/M11_INDEPENDENT_REVIEW.md` R01-R07. Enforced by
migration 00051 (additive, forward-only; 46 and 50 unchanged) and `tests/integration/m11r_review_fixes_test.go`
(`TestM11R*`), which failed on the reviewed code. Not independently reviewed.

1. **A frozen sandbox is read-only in the database (R01, R03, R04; amends #5 and Consequences).**
   `sandbox_frozen_fence` triggers refuse (SQLSTATE 55000, `frozen sandbox is read-only`) every
   INSERT/UPDATE/DELETE on the workflow instance, history, receipt, signal and tool-ledger rows of
   a frozen sandbox's workflows and on `record_links`, `dataset_bindings` and `dataset_generations` of its
   dataset; the dataset row accepts no update at all (which also stops the epoch bumps of record
   share/owner mirrors, so record shares of a frozen sandbox are refused at the SQL layer);
   `tool_finish`, `tool_abandon` and `tool_reconcile` refuse it explicitly. The API answers 409
   `sandbox_frozen` to a workflow signal. Workflows that were in flight when the sandbox was frozen
   stay parked: their worker steps fail against the fence (they have no state change to make).
2. **Freeze is quiescent and serialised with tool begins (R01, R02; amends #3 and #5).**
   `freeze_sandbox` holds the sandbox row `FOR UPDATE`; `tool_begin` holds it `FOR SHARE`. The freeze
   locks the ledger rows of the dataset, then refuses (55000, `sandbox has outstanding tool work`) while an
   invocation is `SENDING` or `UNKNOWN` or a workflow is in `RECONCILE`: the operator settles the
   outcome (reconcile, or wait for the attempt) and freezes again. A stuck unknown outcome therefore blocks
   the freeze until it is settled; that is intended. Workflows that merely wait (approval, timer, retry
   with a settled invocation) do not block it, because every later write to them is fenced (this deviates
   from the first design, which also waited for every non-terminal workflow: the existing test of
   freezing a sandbox with an in-flight workflow is part of the contract). **The port runs READ
   COMMITTED and refuses any other isolation level**: under REPEATABLE READ a freeze that waits for a
   concurrent begin would aggregate from a snapshot older than that begin and freeze evidence that omits
   it. The API opens the freeze route's transaction at READ COMMITTED (`postgres.WithRuntimeIsolation`);
   every other route keeps REPEATABLE READ.
3. **Owner/admin in the ports (R05; amends #5).** `register_sandbox` and `freeze_sandbox` require an
   active owner or admin membership, as the evolution ports do. **Residual:** the tool-catalog hash and
   the policy hash are arguments of the definer port; a compromised API credential of an owner/admin can
   still supply other values. They are computed by the server on the API path; the database cannot
   recompute them (credential-bound identity of the API process, as for the other API-supplied hashes).
4. **Bytewise manifests (R06; amends #5).** The record and ledger manifests are ordered `COLLATE "C"`
   and new evidence bodies carry `"manifest_order":"bytewise"`. `schema_version` stays
   `sandbox-evidence-v1` because M12 (`promotion.checkEvidence`) and the stored bundles check that value.
   Stored bodies are immutable and keep the order they were hashed in (no `manifest_order` field = the
   database default collation of that time); nothing recomputes a manifest (M12 binds the stored body hash),
   so no body needs migrating.
5. **R07** was fixed by gateway F4 (see ADR036 amendment of 2026-10-19); the ceiling now removes the
   test-account credential, which `TestNarrowedTestAccountToolNeverResolvesItsSandboxCredential` pins.
