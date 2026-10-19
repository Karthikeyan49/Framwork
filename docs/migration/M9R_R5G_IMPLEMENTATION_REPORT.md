# R5g — Governed role administration and local R5 completion

19 October 2026 UTC. Starting source `8cf4de2` on
`remediation/m9r-foundation`, initially clean and remotely pushed. Main remains
`9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7`. No main merge, M10, production
activation, customer-data deletion or historical migration change.

Current verification checkpoint: independent bounded PASS at product fix
`5c05d0b`, independent regressions/report `ba21eb0`. Final root verification
passes 275 integration groups without failures/skips,
126 frontend tests, browser suites 11+7 and unchanged representative read/write
capacity gates. Local R5 is complete within the user-selected scope; external
identity deployment remains deferred.

## Authorized scope

The user requested completion of the remaining R5 work and explicitly selected:
"No external provider yet; complete the local R5 work and deployment preparation."
This delivers the missing role-definition/member-assignment UI over working
R5a–f contracts and an operator deployment handoff. No provider, public domain or
production secret was invented. Real external IdP/MFA/gateway/TLS/log verification
remains DEFERRED/NOT RUN by that decision. Local conformance is not production SSO
certification. R6/R7 and the wider remediation/release gate remain open.

## Implementation and preservation

ADR060 was committed before code (`6a1d7b2`). Backend contract checkpoint
`99db506` adds tenant-authorized bounded role-summary discovery (100 + keyset
sentinel), a separate role authoring contract route and exact member point reads
beyond the existing 100-member directory. Explicit C ordering and strict query
parsing preserve continuation witnesses. Existing roles named catalog/options/
operations/teams/members/roles remain point-readable. No global role/member
catalog or claim-based authorization was introduced.

The authoring contract exposes current capability support/reservations, scope
requirements and V1 compiler budgets. It never grants the caller authority.
Assignment acknowledgements now add exact user/requested-role/expected-role
witnesses beside the retained `assigned` boolean. SQL still enforces live owner
policy, active target roles, last-owner protection and label CAS. Business state
and required audit continue to commit atomically.

Frontend checkpoint `c36a0e8` provides a separate role administration panel with
pagination, exact name inspection/creation, typed ALLOW/DENY grants, explicit
resource selectors, recursive DataScope conditions and activation/deactivation.
No grant is selected implicitly. New roles start inactive. Owner is read-only;
native bootstrap bodies have explicit warnings and separate replacement consent.
Configured empty grants deny rather than inherit native authority. Invalid,
duplicate-key, unsafe/lossy numeric, oversized or excessive scope drafts cannot
silently publish a previous value. Exact money/decimals use quoted strings.

Publications bind observed head version, fresh operation UUID, active state,
normalized definition and its stored-definition SHA-256. The existing HTTP
command decoder re-encodes strict maps with Go encoding/json, rather than
preserving JavaScript insertion order. A shared Go/TypeScript fixture fixes that
profile, including UTF-8 versus UTF-16 key ordering, integer-looking keys,
HTML/line-separator escaping and exact decimal strings. No historical role bytes,
hashes, compiler versions or pinned releases are rewritten.

Mutation intents are recorded before dispatch in the actor-owned memory journal
above workspace/routes/accounts. It retains only IDs, public conflict witnesses,
versions and hashes, never tokens, definitions or condition values. Unknown
publications reconcile using the original immutable operation receipt even after
the role head advances. Receipt recovery recomputes the normalized definition
hash; matching labels alone cannot erase an uncertain outcome. Missing/denied
receipts prove no rollback. New intent after a fresh authorized state/absence
requires explicit human review and a new CAS witness. No automatic POST retry.

Member assignments inspect exact tenant-scoped member and desired active role
state. They preserve the existing expected-role-label CAS and its away-and-back
ABA limitation; no invented membership revision or universal exactly-once claim.
Unknown outcomes block that member until fresh authorized point state is
explicitly reviewed. The UI warns about self-demotion/owner assignment and
refreshes live workspace access after a known successful self-assignment. A
subsequent denied read does not turn a saved assignment into a failed mutation.
Connection/selection/unmount aborts fence late reads, hashing and dispatched
changes; an old completion cannot clear a newer preparation or another actor's
journal.

## Deployment preparation

The [identity deployment runbook](../operations/R5_IDENTITY_DEPLOYMENT_RUNBOOK.md)
and [blank evidence template](../operations/R5_IDENTITY_DEPLOYMENT_EVIDENCE_TEMPLATE.md)
cover the supported public PKCE/token profile, pinned public keys, explicit HTTPS
endpoints and same-origin callback, secret bindings, dedicated Redis ACL,
query-log canary, outage/replay/rotation rehearsals, live revocation and provider
MFA assurance. Every real-deployment witness starts NOT RUN. The local issuer/
provider fixture is explicitly test-only. No new runtime dependency or gateway
product was introduced to simulate deployment completion.

## Independent findings and corrections

The [independent review](../verification/M9R_R5G_INDEPENDENT_REVIEW.md) preserves
all initial failures and exact executed probes. Review checkpoint `b39598a`
reproduced MEDIUM R5G-IR-01 (changed receipt definition with unchanged hash labels
cleared recovery) and LOW R5G-IR-02 (old asynchronous preparation left a new
connection disabled). Product fix `5c05d0b` recomputes receipt hashes and scopes
preparation state/guards by client plus unique operation key. Lifetime abort
releases only its pre-dispatch busy marker; an actual mutation owns a separate
marker and recorded intent. The first fix typecheck found a missing React ref
initializer; corrected before committing.

Independent closure `ba21eb0`: all 126 frontend tests, real role browser cases
2/2, lint/typecheck pass. Stronger probes publish on a new connection before the
old digest resolves, preserve newer busy state across the old completion and
retain receipt recovery when hashing finishes after unmount. Eight populated
backend groups passed during the original window, including actual dropped
COMMIT, immutable receipt recovery and simultaneous owner-demotion protection.
No unresolved finding remained in this bounded review. This does not replace
the final root gates or the deferred external deployment evidence.

## Commands executed and results

Ignored root logs use `.local/reports/r5g-*`; reviewer logs and report are linked
above. Commands describe this instance, not remote CI or a deployed provider.

| Exact command | Result | Evidence / limit |
| --- | --- | --- |
| `./scripts/go.sh fmt ./internal/policy ./internal/platform/access ./internal/tenant` | PASS | Intentional formatting during backend implementation. |
| `./scripts/go.sh test ./internal/policy ./internal/platform/access ./internal/tenant` | PASS | Initial selected units. |
| `./scripts/go.sh fmt ./internal/policy ./internal/platform/access ./tests/integration` | PASS | Backend fixture formatting. |
| `./scripts/go.sh test -race ./internal/policy ./internal/tenant` | PASS | New contract/compiler agreement and retained tenant units. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestM9RRoleAdministration' -v ./tests/integration` | PASS | Initial 3 groups 5.212s; expanded 4 groups 6.672s; independent expanded evidence in review. |
| `npm --prefix apps/web run lint` | FAIL initial; PASS corrected | Initial synchronous effect resets/control-regex annotation, then unused test import. No rule disabled globally; corrected lifecycle structure and fixtures. Final independent lint passes. |
| `npm --prefix apps/web run typecheck` | FAIL initial; PASS corrected | Test literal types, then fix ref initializer; corrected. No compiler rule weakened. |
| `npm --prefix apps/web test -- --run src/administration/roleContracts.test.ts` | FAIL initial | Prototype-safe object assertion used a matcher that hides `__proto__`; corrected to inspect own descriptor. |
| `npm --prefix apps/web test -- --run src/administration/roleContracts.test.ts src/administration/RolesPanel.test.tsx` | FAIL initial; PASS corrected | Initial combined 16 PASS/1 fixture assertion FAIL; final root selected 19/19 PASS, 2.77s. Includes reload/second-publication regression. |
| `make build` | FAIL initial; PASS corrected | Initial TypeScript fixture types; corrected all eight Go binaries + production web build. |
| `make test-e2e` preliminary | FAIL | Existing 9 PASS/new 2 FAIL: scope selector fixture, actual wire-hash/receipt defect. Preserved in browser-1 log. |
| `PLAYWRIGHT_BROWSERS_PATH=$PWD/.local/cache/ms-playwright npm --prefix apps/web run test:e2e -- roles.spec.ts` | FAIL earlier; PASS corrected | Second run 2 FAIL wire hashes; third 1 PASS/1 FAIL actual reload lifetime; fourth 2/2 PASS, 5.3s. Independent final 2/2 PASS, 5.0s after review fixes. No assertions/budgets weakened. |
| `./scripts/go.sh fmt ./internal/platform/access` | PASS | Shared fixture unit formatting. |
| `./scripts/go.sh test -race ./internal/platform/access` | PASS | Actual Go HTTP canonical wire fixture, 1.039s. |
| `./scripts/go.sh run /tmp/r5g-wire-fixture.go contracts/examples/role-editor-wire-v1.json` | PASS | Standard-library-only scratch helper generated the public test vector's expected Go HTTP hash; helper not a runtime dependency or committed artifact. |
| `npm --prefix apps/web test -- --run src/administration/RolesPanel.review.test.tsx src/administration/RolesPanel.test.tsx src/administration/roleContracts.test.ts src/AccessConsole.roles.review.test.tsx` | PASS with selection caveat | 24/24 across 3 matching files; the last filename did not exist. Actual AccessConsole.review 2 tests run independently and in the full gate, not claimed by this command. |
| `make setup infra-up migrate migration-status` | PASS | Locked dependency setup; PostgreSQL/Redis healthy, collector started; retained local credentials/keys preserved, all 54 applied, no new migration. |
| `make reference-up` | PASS | Accepted ADR058 reference-v2 profile selected after infra-up; capacity helpers verify the actual connected settings. |
| `GOFLAGS=-v M9R_LEGACY_API_BINARY=/workspace/Framwork/.local/reports/r3c-pre49-api M9R_LEGACY_API_SHA256=03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19 make check test build` | PASS | 275 integration groups in 569.621s with zero failures/skips; 126 frontend tests; Go race/unit/security and script tests, format/vet/module/source integrity, lint/typecheck, all 8 Go binaries/web build. Retained artifact hash was verified before use. |
| `make test-e2e` final | PASS | All 11 current application browser cases, 21.6s, on the final built API. |
| `make test-signin-e2e` | PASS | 7/7 real local browser/provider conformance cases, 8.2s; external provider remains deferred. |
| `make test-performance` | PASS | 65.148s; 100k hot records, ten populated tenants, ten HTTP clients; HTTP p95 112.045ms, p99 121.362ms, zero errors against unchanged 750ms. SQL key/numeric/selective p95 0.620/199.277/117.277ms. Original-policy 2s timeout is an expected negative control. |
| `make test-write-performance` | PASS | 61.520s; 32 clients, 9011 successful distinct-record writes, p95 243.551ms, p99 266.539ms, 149.74/s, zero visible errors against unchanged 500ms. |
| `make docs-verify`, `git diff --check` final | PASS | Frozen source checksums and final documentation whitespace. |
| Real external IdP/client, MFA, production HTTPS gateway/log canary, TLS/key rotation and remote GitHub Actions | NOT RUN / DEFERRED | User selected local work and preparation; no real provider/domain/deployment configured. |

Capacity used the actual connected PostgreSQL 18.6 reference-v2 tier: pinned image,
4 CPUs / 8 GiB/no swap, shared_buffers 2 GiB/effective_cache_size 6 GiB/work_mem 4 MiB/
random_page_cost 1.1, with API and PostgreSQL sharing the cloud parent's four CPUs.
No query/lock/transaction/client/latency budget was relaxed; no forced vacuum/GC
or artificial quiet period was used. Read SQL has three warmups and 100 samples;
HTTP has 100 samples. These two representative unchanged gates cover this UI/read-
contract slice, not a newly certified general query or role-control-plane workload.
The six other reference targets passed in the prior R3d checkpoint and were NOT
RERUN here because no data query/constraint/aggregate execution code changed.
No throughput-neutrality claim follows from comparing separate runs.

## Files, data and dependencies

Backend: `internal/policy/{definition,store}.go`, contract unit;
`internal/platform/access/{access,roles}.go`, wire fixture unit;
`internal/tenant/tenant.go`; populated integration role-administration tests plus
independent backend probes. Frontend: AccessConsole, RoleEditor/RolesPanel,
roleContracts/API client, shared Recovery/useChange and bounded layout; unit,
component, independent console/lifetime and real browser tests. Contract fixture:
`contracts/examples/role-editor-wire-v1.json`. Documentation: ADR060, this report,
independent review, deployment runbook/template, README/agent contract/plan/ledger.

No new package dependency, migration or business table. All 54 applied migrations
remain additive/frozen; no archive/data deletion, metadata recompilation, record
copy, counter reset or privilege widening. No customer DDL or production runtime
path is introduced by role editing. Source kits stay checksum-verifiable.

## Security, operating limits and recovery

Live server policy and SQL remain final. UI contract discovery is not authorization.
Bootstrap permissions remain deliberately broad until an owner explicitly
replaces them. Root owner cannot be edited; SQL still prevents losing the last
active owner. Defining a workflow approver role does not itself grant workflow
signals; R5e live authority checks remain required.

The editor intentionally refuses unsafe numerical representations and duplicate
actions; it is not a raw arbitrary JSON editor or an SSO directory synchronizer.
Very large HTTP definitions also share the existing 64 KiB command-envelope limit,
which can reject a definition below its standalone compiler ceiling. NUL values
are rejected by the server strict decoder even if a local condition draft is
otherwise syntactically valid. Such requests fail closed with HTTP 400, never publish
implicitly; neither caveat is a new privilege boundary or full authoring-parity
claim. Existing native/immutable role APIs remain available under policy.

Membership CAS does not detect away-and-back role changes; role definitions can
change while assignment is inspected because authority is live. The actor-owned
journal is memory-only. Preserve public operation IDs/hashes before reload/close
or full-page provider navigation. No policy bodies or tokens are persisted.
Do not erase receipts/audit evidence or replay an unknown POST to recover.

Rollback removes this UI/additive read surface while preserving memberships,
role heads/versions, operation receipts, data and audit. Restore a configured role
through a new reviewed version with current CAS; never edit history or downgrade
to code ignoring live role heads. If self-demotion removes access, an authorized
owner or controlled operator investigates without bypassing policy.

R7's earlier connection/worker timing failures and explicit injected driver/tracer
panic recovery failure remain unresolved; no reliability fix is claimed here.
R6 independent business acceptance/promotion authority, production active-release
wiring and wider product/query/operations work remain separate. No remote CI,
production SSO/MFA, refresh session, provider logout, persistent browser recovery,
automatic enrollment, SAML/SCIM or general API rate-limiting claim.

## Delivery and final status

Final runtime/test source is `ba21eb0` (product fix `5c05d0b` plus independent
regressions); subsequent changes finalize documentation only. The delivery
response records the exact pushed documentation commit and direct remote-head
equality, avoiding a self-referential commit hash in this file.
Commit source/tests/docs only; ignored `.env`, private
keys, local logs, database volumes, dependencies, `bin`, `dist` and Playwright
artifacts stay out of Git. Push only `remediation/m9r-foundation`; main unchanged.

R5g STATUS: PASS — independent review and final local gates.
R5 local completion STATUS: PASS — governed roles, policy administration, sign-in
conformance and deployment preparation within the user-selected local scope.
External identity deployment: DEFERRED at user request, NOT RUN.
M9R STATUS: PARTIAL. M10 remains paused.
