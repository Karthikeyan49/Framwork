# ADR039 — OIDC browser sign-in and bounded clock tolerance

Status: accepted for implementation after independent read-only design review,
15 October 2026 IST. Executable review remains required.
Scope: R5f, provider-independent browser login; M9R remains PARTIAL. No M10,
main merge, production activation, SAML, SCIM or provider-specific deployment.

## Decision

Preserve ADR014's RS256 access-token/pinned-public-JWKS contract and trusted
`(issuer,subject)` identity. Introduce configurable `AUTH_CLOCK_SKEW`, default
zero and bounded to whole seconds between zero and two minutes. Apply it to
expiry, issued-at and not-before boundaries, without extending the one-hour
signed lifetime limit. No token tenant/role/name claim provisions authority.

Use established OAuth2/OIDC Go libraries behind identity-owned ports. Configure
one provider explicitly: authorization endpoint, token endpoint, client ID,
redirect URI and bounded scopes including `openid`, alongside the existing
issuer/audience/public keys. No token-controlled discovery/JWKS URLs, dynamic
provider selection, automatic user enrollment or client secret in the browser.
Require HTTPS in production; development/test HTTP endpoints are loopback only.
Operator-owned endpoint configuration is not taken from any request. Incomplete
configuration fails startup; disabled configuration adds no provider dependency.

The browser uses the same origin API gateway (the existing `/api` proxy is the
local example). The API exposes a public provider descriptor, origin-checked
POST start and POST complete. Start generates random state, nonce, S256 PKCE
verifier and a separate browser binding. Redis stores a short-lived flow under
a hash combining random state and an independent random HttpOnly
SameSite=Lax cookie (a mismatched older callback cannot consume a newer flow); production cookies are Secure
and host-only. No flow secret enters browser storage. TTL is five minutes.
The existing cache credential is confined to `framework:cache:*`; do not widen
it. Use a separate API-only `SIGNIN_REDIS_URL` / `framework_signin` ACL identity
confined to `framework:signin:v1:*` with only ping/get/set/ttl/eval/getdel. Runtime
child filtering excludes this credential from workers, dispatchers and the web
process. A short-lived namespace probe checks flow operations without consuming
login admission quotas; an outside-namespace read must be denied.
Bound admission to new flow storage, with short-lived per-remote-address and
global quotas; do not trust forwarding headers. These protect this login flow,
not the still-pending R7 general API admission work.

Completion requires the exact configured browser origin, binding cookie and
state. Atomically consume the flow before exchange; bind its stored parameters
to the provider configuration. Never retry exchange after an unknown outcome.
Reject duplicate binding cookies and callback parameters. Use production
`__Host-` cookie naming, Path=/ and no Domain.
Successful completion must not set or delete this shared cookie: an older
in-flight exchange cannot observe a newer tab's binding. Atomic flow consumption
prevents replay; the binding is replaced by the next start or expires after
five minutes. This closes independently reproduced R5F-IR-01 without weakening
state/browser binding.
The redirect has a fixed
`/auth/callback` path and no query/fragment. Use a bounded verified-TLS HTTP
client with explicit public-client AuthStyleInParams (no auto-probe retry),
refuse redirects, limit the response,
and send the exact redirect URI and verifier. The provider receives no framework
credentials. Restart sign-in after an expired/consumed/lost flow; do not infer
rollback or re-use a code.

The OIDC library's default temporal leeway does not define framework policy:
disable only its time check and immediately apply the mandatory shared bounded
exp/iat/nbf validation. Signature/issuer/client-audience checks stay enabled.
Validate the ID token's pinned signature, issuer, client audience/authorized
party, nonce and time bounds. If at_hash exists, verify it. Validate the API
access token through the existing verifier with the configured API audience and
require the same issuer/subject as the ID token. ID tokens never authorize API
requests. Ignore refresh tokens; do not request offline access or implement
refresh sessions. Return only the verified access token in a no-store response;
all business requests retain Authorization bearer headers and live database
identity/membership/policy checks. Login cookies never authenticate business APIs.

The bootstrap captures and removes code/state/error parameters from browser
history before React effects run; the HTML declares no-referrer before assets.
Deployment access logs must redact callback queries (authorization codes are
short-lived but sensitive). The callback uses one shared completion promise
across StrictMode effect replay; cleanup must not re-exchange or abandon its
sole successful reply. Never put access/ID/refresh tokens in URLs, route state,
web storage, recovery journals, telemetry or errors. Retain a verified connection
in a React provider above SPA routes; explicit disconnect/observed 401 clears it.
A stale request can clear only the connection instance that sent it.
The existing token console remains a separately labelled development/API option,
with provider sign-in primary when configured. Missing provider configuration
must be explicit; never claim production SSO when the adapter is disabled.

A full provider redirect unloads the SPA. ADR038's payload-free recovery journal
remains memory-only, so preserve its before-unload warning and explain that
pending resource IDs must be kept before leaving. Do not silently persist
business values or tokens to make redirect recovery appear durable.

## Verification and remaining boundaries

Test clock boundaries/invalid configuration; populated active/revoked database
identities; state/cookie/nonce/issuer/audience/subject/at_hash mismatch; single-use
and parallel completion; PKCE; malformed/oversized/redirecting provider replies;
Redis outage/admission; safe errors and no secret telemetry. Exercise an actual
HTTP provider fixture and browser redirect/code exchange, then existing frontend,
backend, integration and browser regressions. Obtain independent adversarial
review of stable code and resolve findings before claiming this bounded slice.

A local fixture proves the adapter, not an external provider integration, MFA,
logout/revocation at the IdP, enterprise enrollment, SAML or SCIM. Key rotation
remains operator-managed/restart-based. Reauthentication replaces refresh-token
storage. Provider-side outages fail login; existing valid bearer requests do
not depend on a live provider. Redis outage fails new sign-in closed.

Rollback disables provider configuration and keeps the existing bearer contract,
users, memberships, metadata, records, audit and recovery evidence. No database
migration or implicit privilege change is required. Keep the separately recorded
R7 connection and worker-capacity failures open; do not relax their budgets.
