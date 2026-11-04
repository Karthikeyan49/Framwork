# R5f — Provider sign-in and bounded clock tolerance

Date: 15 October 2026 UTC. Status: PASS for this bounded R5f slice.
Starting source: db39e34, branch remediation/m9r-foundation. Overall M9R
remains PARTIAL; no M10, main merge or production activation.

## Scope and implementation

ADR039 was written before code and independently reviewed as a design. The
existing RS256 bearer contract and live database authority remain intact.
The optional adapter uses public-client OAuth2 authorization code + S256 PKCE,
pinned ID/API signatures, nonce/state, browser binding and one-time Redis flows.
It checks issuer, both audiences, authorized party, matching subject, time bounds
and optional at_hash. No provider claim provisions a user, role or membership.
AUTH_CLOCK_SKEW defaults to zero, accepts whole seconds through 120s, and never
extends the one-hour signed token lifetime. Legacy NewVerifier keeps zero skew.

Endpoints are operator configured, HTTPS in production, loopback HTTP only in
development/test. No discovery or token-controlled key URL exists. Explicit
exchange auth style avoids automatic retry; the verified-TLS client refuses
redirects, has a five-second timeout and bounds JSON replies to 32 KiB. Exchange
uncertainty consumes the flow and requires a fresh sign-in, never blind replay.
Gateway POSTs enforce the exact configured browser origin, bounded JSON and
single cookies. Redis atomically saves bounded five-minute flows and consumes
with GETDEL; admission is 20 starts/remote/minute and 120 globally/minute.
Forwarded IP headers are not trusted. These are login-flow quotas, not R7's
still-pending general API abuse protection.

A dedicated framework_signin Redis ACL and API-only SIGNIN_REDIS_URL keep login
proofs separate from cache/events/worker credentials. Readiness checks operations
and confinement without consuming admission or touching an actual flow. Local
make infra-up adds only this user to an existing Redis; other users are retained.
Worker, dispatcher and frontend child environments do not receive this binding.

The React console uses provider-first sign-in with a separately labelled
development token option. Production's API descriptor hides manual entry and
states when sign-in is unconfigured. Bootstrap scrubs callback parameters before
React; HTML/dev-server referrer policy protects subsequent requests. A shared
completion promise prevents StrictMode/remount exchange duplication. Verified
connection state lives above SPA routes. An observed authenticated 401 clears
only its exact connection, including preview/administration transports; an old
failure cannot disconnect a newer account. Tokens never enter storage, URLs,
route state, journals or logs. Disconnect/reload clears memory.

A real browser conformance fixture implements local code/PKCE/ID/API token
exchange, signed malformed grants and live database revocation. It is explicitly
test-only, loopback-only and uses ignored owner-readable development keys.
It must never be deployed as an identity provider. Existing manual/API and
operation-recovery browser fixtures now follow persistent SPA connection state.

## Source and dependencies

Backend: internal/identity/{identity, login}.go and tests;
internal/config/{config, login}.go and tests; internal/platform/cache/login.go;
internal/platform/signin and its independent tests; internal/platform/app/app.go;
tests/integration/m9r_oidc_test.go. No module-internal dependency was introduced.

Frontend: apps/web/src/auth (client, session provider, sign-in/callback and tests),
App/main/access-console wiring, preview/admin authenticated transports, callback
HTML/referrer configuration, existing regression-fixture updates and isolated
OIDC Playwright project/provider. Tooling: API-only runtime filtering, additive
sign-in ACL helper, environment example, Make/CI browser command and README.
Decisions/evidence: ADR039, AGENTS.md, this report, independent review, plan and ledger.

Major Go dependencies are managed in go.mod/go.sum:

- github.com/coreos/go-oidc/v3 v3.21.0: established ID-token claim verification
  with the existing pinned signature port. Only its temporal checks are replaced
  with the framework's mandatory shared bounded policy, avoiding implicit 5m leeway.
- golang.org/x/oauth2 v0.37.0: standard authorization-code/S256 PKCE exchange,
  explicit public-client auth and bounded injected HTTP transport.
- github.com/go-jose/go-jose/v4 v4.1.4: transitive JOSE support for the OIDC library.

No frontend dependency was added. No database schema/migration, business record transformation,
implicit enrollment, existing credential replacement or destructive cleanup.

## Independent finding and correction

Review checkpoint c027809, backend 7112cd0. Reviewer added executable probes to
stable code while implementation and competing test workloads paused.
R5F-IR-01 (MEDIUM availability) reproduced an older successful completion deleting
a newer tab's binding cookie. A real HTTPS gateway, shared jar and blocked
exchange failed deterministically in three repetitions. No authorization/tenant
bypass was demonstrated.

Regression checkpoint 1320a40; correction 01aeb34 leaves the shared cookie
unchanged on completion. Atomic flow consumption already rejects replay; next
start or five-minute expiry replaces the binding. Independent verification
strengthened the test to finish B successfully and replay A with its original
cookie (401, no additional exchange). Three runs passed, alongside signed-token,
HTTP, live Redis/PostgreSQL and rebuilt-browser cases. Review evidence checkpoint
4b337a4 records bounded PASS with no unresolved finding. See
[the independent review](../verification/M9R_R5F_INDEPENDENT_REVIEW.md).

## Commands actually executed and results

Ignored logs are under .local/reports/r5f-*. Commands below describe local results;
remote GitHub Actions and an external production provider are not implied.

| Exact command | Result | Evidence |
| --- | --- | --- |
| `./scripts/go.sh test -race ./internal/identity ./internal/config` | PASS | Initial baseline, before implementation. |
| `npm --prefix apps/web test` | PASS baseline; FAIL first adapted fixtures; PASS corrected | Baseline 86/86; first adapted run 84/86 (two stale App response mocks); corrected final focused development run 94/94, 7.98s. |
| `./scripts/go.sh test -race ./internal/identity ./internal/config ./internal/platform/signin ./tests/security` | PASS | Clock/provider and security regressions. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9ROIDC' ./tests/integration` | FAIL initial; PASS after dedicated Redis binding | Initial 1.266s failed because cache ACL correctly denied login EVAL/keyspace. Cache was not widened. New binding passed 2.330s, then readiness/isolation follow-up2.119s; independent final 1.861s. |
| `node scripts/m8-env.mjs` | PASS | Retained local credentials preserved; generated separate random ignored sign-in credential. |
| `docker compose --env-file .env -f infra/compose/compose.yaml up -d --wait --wait-timeout 30 redis` | PASS | Redis healthy after the initial local additive ACL was loaded. |
| `node scripts/with-env.mjs node scripts/signin-roles.mjs` | PASS | Positive live additive activation; future infra-up handles existing Redis without restart. |
| `node --test scripts/runtime-env.test.mjs` | PASS | Seven cases; API-only filtering and fail-closed local helper. |
| `./scripts/go.sh build -o bin/api ./cmd/api` | PASS | API build, repeated independently before corrected browser run. |
| `npm --prefix apps/web run typecheck` | PASS | TypeScript verification. |
| `npm --prefix apps/web run lint` | PASS | ESLint with zero warnings. |
| `make build` | PASS | All eight Go binaries and web production bundle before final gate. |
| `make test-e2e` | PASS preliminary | Existing nine browser cases, 18.6s before final transport additions; final repeat is recorded below. |
| `make test-signin-e2e` | FAIL earlier fixtures; PASS corrected | First: 2 failed, 1 interrupted, 4 not run (fixture accessible-label timeouts); second: 7 failed (fixture CSP blocked callback redirect); third: 6 passed, 1 failed (fixture used inactive instead of CLI disabled). Corrected root 7/7, 8.7s; independent post-fix 7/7, 8.8s. No application validation was weakened. |
| `./scripts/go.sh test -race -count=3 -run '^TestIndependentOlderCompletionPreservesNewerBrowserBinding$' ./internal/platform/signin` | FAIL pre-fix; PASS fixed | Independent deterministic pre-fix three repetitions; root fixed 1.449s and strengthened independent 1.309s pass. |
| `./scripts/go.sh test -race -count=1 ./internal/identity ./internal/config ./internal/platform/signin ./tests/security` | PASS | Root fixed identity 2.106s/config 1.038s/signin 1.224s/security 1.041s and independent repeat. |
| `GOFLAGS=-v make check test build` | PASS | Source 4b337a4: 179 integration tests in 605.921s, frontend 94/94, Go race/unit/security, format/vet/module verification, lint/typecheck and all eight backend/web builds. |
| `make infra-up migrate migration-status` | PASS | PostgreSQL/Redis/collector healthy; retained local credentials/keys preserved; live additive sign-in ACL; zero new migrations, all 46 applied. |
| `make test-e2e` | PASS final | Existing browser regressions 9/9, 18.2s, after the full gate. |
| `make test-signin-e2e` | PASS final | Real local provider/browser cases 7/7, 8.8s, on corrected built API. |
| `make test-performance` | PASS | 44.367s; 100k hot records/ten populated tenants; HTTP ten-client p95 154.218ms, p99 185.260ms, 107.15 requests/s, zero errors; unchanged 750ms target. SQL key p95 0.466ms, numeric 114.053ms, selective 76.600ms. |
| `make docs-verify` | PASS | 84 retained-source and 43 V3 checksums. |
| `git diff --check` | PASS | No whitespace errors. |
| External IdP interoperability/MFA, real TLS outage, production proxy and GitHub Actions | NOT RUN | No external provider/deployment selected or certified. |

## Security, operating limits and recovery

The browser/provider/API trust boundary is covered by populated identity,
membership and tenant negatives, malformed origins/cookies/bodies, nonce/state,
PKCE, pinned JOSE/header/claim/time failures, flow configuration drift/TTL,
concurrent atomic consume, quotas, ACL confinement and safe-error/log assertions.
The independent review states exact executed cases and untested fault classes.

Deployment still requires an actual compatible provider/client, verified HTTPS
same-origin gateway, callback routing and query redaction at every incoming
access-log layer, production secret management, pinned-key rotation and clock
synchronization. Browser scrubbing cannot erase an incoming proxy request log.
No production SSO/MFA claim, provider logout, refresh session, browser-restart
persistence, automatic enrollment, SAML/SCIM or role-definition UI is delivered.
Public-client PKCE is the explicit supported profile; other IdP client profiles
need a reviewed adapter/configuration change. Local fixture success is not
external-provider certification. No external tools or universal exactly-once claim.

Full-page provider navigation unloads the existing memory-only payload-free
recovery journal. The UI preserves its before-unload warning and shows a provider
navigation warning when pending IDs exist. Retain pending IDs and reconcile
unknown operations before another write; this is not durable browser recovery.

Rollback disables all OIDC fields and returns to the existing bearer contract,
without changing users, memberships, metadata, records or audit/outbox evidence.
Expire outstanding Redis flows naturally; never broaden ACLs or erase unrelated
keys/evidence. Provider sign-in availability depends on Redis/IdP; valid existing
bearer requests retain their live database checks without an IdP round-trip.

Existing R7 connection and worker-policy 2s timeout findings remain open despite
passing reruns. Remaining R3b types, R3c uniqueness, R3d aggregation, R6 authority
and R7 operations are not fixed by R5f. Production active-release runtime remains
later M12/M13. Overall R5/M9R remains PARTIAL and M10 remains paused.

The final query gate used verified PostgreSQL 18.6, 4 CPUs / 8 GiB, swap disabled,
shared_buffers=128MB/work_mem=4MB, 100k hot records plus nine 10k-record tenants,
average record width 761 bytes and three warmups followed by 100 samples per
reported operation. The original-policy 2s timeout is an expected negative
control. Passing this configured numeric gate is not certification of general
unindexed queries, other types, login flood capacity or wider worker throughput.

## Git delivery and final status

All source/config/tests/docs belong on remediation/m9r-foundation only. Ignored
.env, private keys, local logs, node_modules, bin/dist and runtime data do not
belong in Git. No main merge or deployment. Verified runtime/test source: 4b337a4.
The commit containing this finalized report
also records ledger/README/agent-rule updates. The final delivery response records
the pushed commit and direct remote-head equality; no force push is permitted.

R5f STATUS: PASS (bounded adapter, independent and full local verification).
M9R STATUS: PARTIAL. M10 remains paused.
