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

Follow-up integration adds role isolation and readiness probes. Browser UI,
real browser provider fixture, independent executable review and final full
checks/tests/build remain pending. These are not implied by the backend checks.

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
