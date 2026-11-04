# R5f independent adversarial review

Date: 15 October 2026 IST. Implementation reviewed: `c027809`, backend
checkpoint `7112cd0`, relative to `db39e34`. Independent reviewer operated on
stable source while the implementing agent paused edits and test workloads.
Only regression tests and this report were added by the reviewer. No production
fix was applied during this review.

## Initial verdict

**PARTIAL — one MEDIUM finding requires correction and re-verification.**
The executed negatives did not demonstrate an authentication or tenant-policy
bypass. This is a bounded adapter review, not certification of an external IdP,
production SSO deployment, MFA, SAML, SCIM or the whole M9R program.

## R5F-IR-01 — successful older completion deletes a newer binding

Severity: MEDIUM. Category: concurrent browser-flow availability.
Location: `internal/platform/signin/signin.go`, successful `complete` response.

The response unconditionally emits deletion of the host-only login cookie.
That response can arrive after another tab has started a newer login and
installed a different binding cookie. Cookie deletion has no comparison against
the browser's current value. The valid older response consequently erases the
newer binding, and the newer callback cannot complete without restarting login.
The old flow's server-side single-use protection still holds; this finding does
not grant an unauthorized identity or membership.

Executed reproduction uses a real HTTPS gateway, shared HTTP cookie jar and
controlled HTTP token endpoint. Start A, dispatch completion A and block its
exchange, start B (observe its distinct binding), release a valid signed pair
for A, and observe A's HTTP 200 remove B's cookie. The independent regression
`TestIndependentOlderCompletionPreservesNewerBrowserBinding` fails with a static
assertion message; no credential or response body is printed.

Minimal correction: successful completion need not delete the cookie. Its Redis
flow was already atomically consumed, so the remaining cookie cannot replay it.
Allow the next explicit start or five-minute browser expiry to replace/remove
the binding. Preserve duplicate-cookie rejection, per-flow consumption, nonce,
PKCE and state checks. Re-run the reproduction, replay tests and browser suite.

## Executed validation

Logs are ignored files under `.local/reports/r5f-review-*`. Test fixtures contain
synthetic identities/tokens; assertions intentionally avoid raw token, code,
binding, redirect or provider-response dumps.

| Exact command | Result | Evidence |
| --- | --- | --- |
| `./scripts/go.sh test -race ./internal/identity ./internal/config ./internal/platform/signin ./tests/security` | PASS | Existing identity/config/security checks; signin initially had no tests. |
| `./scripts/go.sh test -race -count=1 ./internal/identity` | PASS | Independent signed-pair/header/clock/configuration tests, 2.081s. |
| `./scripts/go.sh test -race -count=1 ./internal/identity ./internal/config ./internal/platform/signin ./tests/security` | PASS | Independent negatives added before concurrency regression, identity 1.842s, signin 1.073s. |
| `./scripts/go.sh test -race -count=1 -run '^TestIndependentOlderCompletionPreservesNewerBrowserBinding$' ./internal/platform/signin` | FAIL | Deterministic pre-fix cookie race, 0.064s. |
| `./scripts/go.sh test -race -count=3 -run '^TestIndependentOlderCompletionPreservesNewerBrowserBinding$' ./internal/platform/signin` | FAIL | Repeated pre-fix reproduction; see repeat log. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9ROIDC' ./tests/integration` | PASS | Live PostgreSQL identity/revocation/policy, Redis isolation/admission/TTL/atomic take, 2.017s. |
| `make test-signin-e2e` | PASS | Seven actual browser/provider scenarios, 9.8s; valid/skew, nonce/future/error rejection, no implicit enrollment and live-revocation clearing. |
| `npm --prefix apps/web test -- --run src/auth/Session.test.tsx src/auth/client.test.ts` | PASS | Eight frontend tests, 1.78s; StrictMode, cancellation, stale 401, memory-only tokens, callback rejection. |
| `node --test scripts/runtime-env.test.mjs` | PASS | Seven child-credential/provisioning/ACL activation checks. |
| `git diff --check` | PASS | No whitespace errors at initial handoff. |
| `make check test build` | NOT RUN by reviewer | Full final regression belongs to implementing agent after correction. |
| `make test-e2e` | NOT RUN by reviewer | Existing general-browser regression belongs to final implementation verification. |

Independent additions explicitly reject unsupported token types, unknown key ID,
critical JOSE headers, wrong algorithm, fractional/missing expiry, audience-list
overflow, empty subject, out-of-budget issued/not-before/expiry, signed lifetime
over one hour, malformed/media-type provider responses, configuration drift and
expired/future flows. A valid ID token expiring 29 seconds ago remains accepted
with an explicitly configured 30-second tolerance; its exact boundary rejects.
Every consumed completion is attempted again to prove no repeated exchange.
Gateway probes reject missing/foreign/combined/duplicate Origins, query
parameters, unknown/trailing/oversized/array bodies and missing/invalid/
duplicate cookies before reaching the flow store.

## Remaining boundaries

Provider timeout/availability behavior is source-reviewed and existing bounded
transport/error cases are executed; this review did not inject a real external
TLS outage, certify a production reverse proxy, conduct provider interoperability
testing or load-test login admission. Deployment callback-query log redaction
remains an operator requirement. Existing R7 worker/connection findings remain
open. No M10, production activation or main merge is authorized by this report.

## Correction and independent re-verification

Stable correction reviewed: `01aeb34`, following regression checkpoint
`1320a40`. The implementer removed the successful response's cookie deletion
and documented the concurrent-tab rationale in ADR039. No other authentication
or cookie-validation behavior was relaxed. The reviewer again held exclusive
test access while the implementer paused edits and workloads.

The independent HTTPS/cookie-jar regression was strengthened to complete B
after A, and to replay A using its **original** binding in a separate cookie-free
transport. The retained original binding returns 401 with no extra token
exchange; B completes with 200 and exactly the second exchange. This checks
both the recovery of the newer flow and preservation of single-use authority.

| Exact command after correction | Result | Evidence |
| --- | --- | --- |
| `./scripts/go.sh test -race -count=3 -run '^TestIndependentOlderCompletionPreservesNewerBrowserBinding$' ./internal/platform/signin` | PASS | Three repetitions including successful B and spent-A replay, 1.309s. |
| `./scripts/go.sh test -race -count=1 ./internal/identity ./internal/config ./internal/platform/signin ./tests/security` | PASS | Identity 2.011s; config 1.020s; signin 1.250s; security 1.031s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9ROIDC' ./tests/integration` | PASS | Live Redis/PostgreSQL regressions, 1.861s. |
| `./scripts/go.sh build -o bin/api ./cmd/api` | PASS | Browser exercised the rebuilt corrected API. |
| `make test-signin-e2e` | PASS | Seven real browser/provider scenarios on rebuilt corrected API, 8.8s. |

**R5F-IR-01: CLOSED by correction and executed independent regression.**
**Final independent verdict: PASS for this bounded R5f adapter slice.** No
unresolved finding remains in this review. Full repository checks/build/tests
and the general browser suite remain the implementer's responsibility before
marking the implementation complete. This does not close overall M9R or certify
an external provider/production deployment.
