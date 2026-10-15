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
   reference; the catalog forbids equal production/sandbox hosts and secrets.
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
   catalog and policy hashes are computed by the server, not request fields.
   Evidence is an input to M12; it contains no PASS, risk or approval claim.
6. **Contracts.** `sandbox.Context` and bundle 1.0.0 bytes are unchanged
   (stored bundles must still restore). New fields live in the versioned
   evidence type that a later bundle version references (M12).
7. **API and policy.** `POST .../preview/{app}/sandboxes` (witness of the exact
   compiled IR, workspace UUID, ceiling), `GET .../sandboxes/{id}`,
   `POST .../sandboxes/{id}/freeze`, `GET .../sandboxes/{id}/evidence`. Actions:
   `sandbox.read` (all roles) and `sandbox.manage` (owner/admin, delegable, never
   a role named `viewer`). Sandbox data and workflows use the existing dataset
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
