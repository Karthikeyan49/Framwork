# R5f — Provider sign-in and bounded clock tolerance

Date: 15 October 2026 IST. Status: implementation checkpoint, not accepted.
Starting source `db39e34`, branch `remediation/m9r-foundation`. M9R PARTIAL;
no M10, main merge or production activation.

ADR039 was written before code and independently reviewed as a design. The
existing RS256 bearer contract and live database authority remain intact.
Provider sign-in uses standard OAuth2/OIDC libraries, pinned public keys,
PKCE/nonce/browser binding and one-time Redis flows. Signed lifetime remains
one hour; optional clock tolerance defaults to zero and is capped at 120s.
No implicit user/role provisioning, refresh storage, SAML or SCIM is added.

## Executed evidence at backend checkpoint

- Baseline `./scripts/go.sh test -race ./internal/identity ./internal/config`:
  PASS; baseline `npm --prefix apps/web test`: PASS 86/86 (6.11s).
- `./scripts/go.sh test -race ./internal/identity ./internal/config ./internal/platform/signin ./tests/security`:
  PASS after new clock/provider/unit cases. HTTP composition gains integration
  tests; the signin package itself has no separate unit test files yet.
- First `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestM9ROIDC' ./tests/integration`:
  FAIL (1.266s), both new tests. The existing cache credential correctly rejects
  login keyspace/EVAL access. It was not widened. Introduced dedicated API-only
  `SIGNIN_REDIS_URL` / `framework_signin` role and exact login keyspace commands.
- `node scripts/m8-env.mjs`: PASS, retained credentials preserved, new random
  sign-in binding added to ignored local configuration. Redis was restarted
  with the additive ACL; `docker compose --env-file .env -f infra/compose/compose.yaml up -d --wait --wait-timeout 30 redis`: PASS healthy.
- Same focused integration command after dedicated binding: PASS (2.330s).
  Cookie/state/PKCE, one-time completion, forged role/tenant claims, disabled
  database identity, quotas, flow/counter TTL and atomic Take are exercised.
- `node --test scripts/runtime-env.test.mjs`: PASS 6/6. Only API subprocesses
  receive provider configuration/sign-in credentials; other process kinds do not.

Follow-up focused integration including role isolation/readiness: PASS 2.119s.
Node credential filtering/ACL activation tests: PASS 7/7. Frontend unit tests:
PASS 94/94, typecheck/lint and `make build`: PASS. Existing `make test-e2e`:
PASS 9/9 before the final connection-transport additions; repeat required.

`make test-signin-e2e` has now passed 7/7 (8.7s). Prior attempts are retained:
the first stopped after fixture label lookup timeouts, the second failed 7/7
because the test provider's form-action CSP blocked its cross-origin callback
redirect, and the third passed 6/7 but used unsupported `inactive` instead of
`disabled` in the operator fixture. Fixed the fixture label, allowed only the
exact loopback callback origin in its CSP, and used the existing CLI enum.
These were test-fixture corrections, not weakened application validation.

The browser has provider-first sign-in, early callback scrubbing, a completion
promise shared across StrictMode/remount, connection state above SPA routes,
and exact-connection 401 clearing in preview/administration transports. Manual
tokens are a separately labelled development option; disabled production sign-in
is explicit. Tokens never enter web storage or operation journals. Independent
executable review reproduced R5F-IR-01: a successful older exchange deleted the
cookie for a newer tab's flow (MEDIUM availability issue; no authority bypass).
The deterministic real HTTPS/shared-jar regression failed in three repetitions.
Correction leaves the cookie unchanged on completion: atomic Take already spent
the old flow, and only a new start or five-minute expiry replaces its binding.
Independent re-verification and final full checks/tests/build remain pending.

## Boundaries

No SQL migration or existing business data change. Redis holds ephemeral login
proofs, not persistent users, memberships or ERP facts. Worker/event/cache
credentials must not read it; operator test credentials remain confined to test
setup/inspection. Sign-in readiness probes use short-lived reserved keys and do
not consume admission quotas. Errors and telemetry contain no token/provider
response/code/nonce/cookie or DSN.

Production use requires an externally configured and validated provider, HTTPS
same-origin gateway, client registration and callback-query log redaction. A
local provider fixture will prove the adapter, not external SSO/MFA deployment.
Clock synchronization, operator key rotation and live database revocation remain
required. Full-page provider navigation loses the memory-only recovery journal;
retain its warning and make that limit explicit. Keep R7's earlier connection
and worker statement-timeout findings open; no runtime budgets are relaxed.
