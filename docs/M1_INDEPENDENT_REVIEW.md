# M1 independent adversarial review

- Date: 2026-10-11
- Scope: M1 — Tenant / Identity / Policy; no M2 implementation
- Reviewer: independent Codex review agent under `AGENTS.md` and
  `docs/agents/82_TWO_AGENT_PROTOCOL.md`
- Result: **PASS** for the reviewed implementation and checks below.

No unresolved BLOCKER/HIGH finding remains in the reviewed implementation. This
review is verification evidence; it does not replace human milestone approval.
The reviewer made no implementation edits. This report is the only reviewer-owned
repository change.

## Reviewed boundaries

Inspected identity verification and pinned public keys, trusted actor/tenant
types, live membership resolution, native policy and HTTP enforcement, audit,
operator provisioning, runtime database privileges, migrations 00002–00003,
authentication configuration, local issuer tooling, frontend access/client
behavior, architecture instructions and ADR-014. Existing M0 infrastructure was
reviewed separately; this report concerns the additive M1 implementation.

Protected session and tenant endpoints now use the central policy port. Session
policy permits only the authenticated user's own resource; tenant decisions
require the trusted user and access actor to match. Unknown actions, resources,
roles and obligations deny. Authorization, membership reads and audit share the
request transaction, and audit commits before protected payloads are returned.
JWT roles/tenant claims and browser headers/body fields do not grant membership.

## Findings and corrections verified

| Severity | Finding | Final correction and evidence |
| --- | --- | --- |
| HIGH | Checking only superuser/BYPASSRLS/create-role/create-database flags accepted the migration role as a runtime connection, despite its IAM/audit ownership and schema DDL rights. An independent read-only live probe reproduced this. | Runtime privilege checks now inspect protected schema/table ownership, schema CREATE, IAM write and audit mutation privileges. Protected endpoints independently check the role. Real PostgreSQL tests reject both migration-role readiness and direct protected HTTP use with that role. |
| MEDIUM | A first non-owner membership could bootstrap an ownerless tenant. | Tenant provisioning requires an active first owner, serializes changes on the tenant row and prevents removing the last active owner. Real concurrent owner-removal tests retain exactly one owner. |
| MEDIUM | Operator audit lacked enough state/provenance to reconstruct a role/status change. | Typed bounded audit state records role, user/membership status and operator reference. Additive migration 00003 records database writer provenance; column privileges prevent the app from overriding writer/time defaults. Real database/CLI tests validate committed evidence and forbidden app overrides. |
| MEDIUM | A failed COMMIT was described as guaranteeing no committed change. | Operator errors distinguish failed audit append from an uncertain commit outcome and instruct operators to inspect state/evidence before retrying. |
| Boundary | Application composition queried identity-owned storage directly. | Schema readiness is now exposed by the identity store's narrow public contract. Static boundary checks pass. |
| Boundary | A public JWKS configuration could contain private RSA components. | Verifier construction rejects private key components; a valid RSA public set augmented with private material is rejected by regression tests. |
| Boundary | Session data originally relied on identity ownership/RLS without a central policy decision. | Session now calls policy, enforces obligations, records decision/version and commits audit before returning user/workspace data. The final independent integration rerun verifies explicit session denial and unknown-obligation denial along with normal access. |
| MEDIUM | Readiness could report ready with IAM users present but required audit migration columns absent. | Readiness now composes module-owned identity, tenant and audit probes. Audit checks required INSERT-column privileges, state/writer columns and forced RLS without reading evidence or writing events. The final independent integration rerun verifies readiness after fresh migrations and rejection after dropping `state` only in the uniquely owned disposable database. |

Opaque principal/user/access contracts prevent consumers from constructing trusted
identities from browser strings. Runtime IAM access remains read-only, while the
explicit operator CLI uses separate migration credentials and commits state and
evidence atomically. Local signing keys are preserved with private permissions;
local token issuance refuses production and external issuer impersonation.

## Independently executed checks

All commands below were executed in `/workspace/Framwork`; all completed with exit
code 0. Race tests and integration tests were rerun after the session policy fix
and the later module-readiness correction.

| Exact command | Result | Evidence |
| --- | --- | --- |
| `make check` | PASS | 84 preserved source-document checksums, Go format/vet, Go module verification, frontend lint and TypeScript checking. |
| `./scripts/go.sh test -race -count=1 ./internal/identity ./internal/tenant ./internal/policy ./internal/config ./internal/audit ./internal/platform/access ./tests/security` | PASS | Identity/claims/key negatives, tenant/policy/config tests and module-boundary checks. Audit/access have no direct unit test files; their behavior is exercised by real integration tests. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM1' ./tests/integration` | PASS | Real app/operator roles; authentication, tenant attacks, revocations, audit failure/obligations, RLS/pool context, unsafe-role rejection, provisioning recovery, simultaneous owner changes, actual operator/local-issuer commands and fresh migrations 00001–00003 with repeatability/readiness/drift rejection/cleanup. Final post-readiness run completed in 2.340 seconds. |
| `./scripts/go.sh test -race -count=1 ./internal/identity ./internal/tenant ./internal/audit ./internal/platform/app ./tests/security` | PASS | Post-readiness-correction regression and compilation. Audit/app have no direct unit test files; integration exercises their readiness contracts. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web test` | PASS | 4 test files, 14 tests, including the access console and client behavior. |
| `node --check scripts/dev-identity.mjs` | PASS | Local issuer script syntax. |
| `node --check scripts/smoke.mjs` | PASS | Updated smoke script syntax; inspection confirms all five readiness dependencies are asserted. |

The temporary read-only pre-fix role reproduction was executed as
`node scripts/with-env.mjs ./scripts/go.sh run .local/review/m1-role.go` and then
removed. Its unsafe acceptance was the finding above, not a passing security
check; the persistent integration regression now verifies rejection.

The implementer separately reports a successful complete
`make fmt check test build migration-status` and live smoke. Those full builds,
full M0 integration suite, live smoke and hosted GitHub Actions were **not
independently run as part of this M1 review** and are not claimed as reviewer
results here. No failed independent final check is hidden.

## Limits and follow-up

- Pinned RS256 `at+jwt` verification is an API token contract. Provider redirect
  login/PKCE, MFA, refresh/session cookies, account recovery, automatic key
  discovery/hot rotation and per-token session revocation are future work. Key
  rotation currently requires staged public-file updates and API restarts.
- Membership/user status is read from PostgreSQL for each request. An in-flight
  repeatable-read request can finish under its prior authorization snapshot;
  subsequent requests see committed revocation.
- Fixed M1 roles/actions and limited ownership ABAC are foundational, not the
  later metadata-driven permission/compiler/business runtime. Narrow IAM RLS
  supports M1; reusable business-data access and RLS remain M2.
- Audit is append-only against application and ordinary operator DML. Privileged
  database DDL administrators can alter storage/protection. External immutable
  archival and cryptographic proof of a human operator are not implemented.
- Operator reference is declared attribution; database writer identity records
  the actual database role. Production deployment must maintain the separate
  runtime/operator credential contracts and control operator access.
- Workspace/member lists are capped at 100 without pagination. The frontend
  access console holds bearer tokens only in memory; it is not a production
  identity-provider login flow.

**M1 REVIEW STATUS: PASS. Stop before M2 pending a separate authorized task.**
