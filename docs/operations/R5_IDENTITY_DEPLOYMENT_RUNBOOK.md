# R5 identity deployment preparation

19 October 2026. The user selected **local R5 completion and deployment
preparation**, with no external provider yet. This is an operator handoff, not
a deployment authorization or evidence that production SSO/MFA works. Keep the
application on the remediation branch; no main merge, production activation or
M10 is implied. ADR039 and ADR060 define the supported profile.

## Before choosing a provider

The supported adapter is a public OAuth2/OIDC client using authorization code and
S256 PKCE. The provider must issue an RS256 ID token for the browser client and an
RS256 API access token with `typ=at+jwt`, the configured API audience, exact issuer,
matching subject, issued-at and expiration, and a signed lifetime at most one
hour. ID-token nonce, audience/authorized-party and optional at_hash are checked.
A default opaque token, HS256 token or confidential-client-only product does not
meet this contract. No runtime discovery or token-controlled key URL is supported.
Provider role/team/tenant claims never provision or grant authority.

Have the identity operator record the selected provider, public domain, client ID,
API audience and supported claims. Provision existing users by the exact stable
issuer/subject with the operator IAM port, then grant tenant memberships through
the governed role APIs/UI. Do not paste tokens, codes, cookies, secrets or DSNs
into chat, Git, tickets or this evidence record. Use deployment secret settings.

## Configuration handoff

| Binding | Required deployment value / check |
| --- | --- |
| `APP_ENV` | `production`; dev/test descriptors and local issuer are not production settings |
| `AUTH_ISSUER` | Exact provider HTTPS issuer |
| `AUTH_AUDIENCE` | Registered API audience, distinct from the browser client if required by the provider |
| `AUTH_JWKS_FILE` | Mounted operator-managed public RS256 key set; verified provenance and read-only permissions |
| `AUTH_CLOCK_SKEW` | Whole seconds, 0s–120s; default 0s; synchronize host clocks rather than widening signed lifetime |
| `OIDC_CLIENT_ID` | Registered public client, no client secret |
| `OIDC_AUTHORIZATION_URL`, `OIDC_TOKEN_URL` | Explicit approved HTTPS endpoints, no redirecting token endpoint |
| `OIDC_REDIRECT_URI` | Exact registered `https://<application-domain>/auth/callback`, no wildcard |
| `OIDC_SCOPES` | `openid` by default; no offline access |
| `CORS_ORIGINS` | Exact HTTPS application origin matching the callback; no wildcard |
| `VITE_API_BASE_URL` | Build with `/api` for the same-origin gateway; never embed secrets in Vite variables |
| `SIGNIN_REDIS_URL` | API-only `rediss` binding for user `framework_signin`, DB0, verified trust, password in secret settings |
| PostgreSQL, cache, telemetry bindings | Existing production config validation still applies: verify-full PostgreSQL, TLS Redis and approved telemetry; distinct runtime/operator/worker bindings |

Provision only `~framework:signin:v1:* -@all +ping +get +set +ttl +eval +getdel`
for the dedicated sign-in Redis identity. Do not grant ACL administration, other
keyspaces, cache/event credentials or worker access. Existing readiness checks
verify both permitted operations and confinement. The local `signin-roles.mjs`
helper intentionally accepts loopback development Compose only and must not be
used to provision production. No credentials from `.env` or local key files
belong in the deployment.

## Gateway contract

Serve the built SPA and `/api/*` on the **same HTTPS origin**. Strip only `/api`
when proxying to the API, preserve the original Host/origin semantics, and route
`/auth/callback` to the SPA. Keep the backend listener private to that gateway.
Maintain `Referrer-Policy: no-referrer`; do not cache auth endpoint responses.
The browser binding is a Secure, host-only, HttpOnly, SameSite=Lax cookie in
production. Do not broaden it to a parent domain or rewrite its path/security
attributes. Enforce HTTPS for the whole browser journey.

Remove **query strings**, authorization headers, cookies and response/request
bodies from every callback/access-log layer: CDN, WAF, load balancer, reverse
proxy, application/APM, error sampling and log exports. For example, an Nginx
log format may use `$uri` rather than `$request`, `$request_uri` or `$args`;
that example is not a shipped/tested gateway configuration. Audit the actual
chosen gateway rather than assuming browser history scrubbing protects its logs.
Use a synthetic non-secret callback canary to check all retained/exported logs;
never use a real authorization code as the canary.

General authenticated/unauthenticated API rate limiting remains R7. The existing
login quotas (20 starts/remote/minute, 120 globally/minute) do not certify a
production abuse envelope. Do not trust arbitrary forwarded-IP headers to bypass
those quotas. Set any trusted gateway/network limits explicitly and test them.

## Reproducible local preparation

From the existing checkout, with Node24, the pinned Go toolchain and Docker:

```sh
make setup
make infra-up
make migrate
make migration-status
make check
make test
make build
make browser-install
make test-e2e
make test-signin-e2e
```

The last target uses a test-only, loopback-only provider fixture, never a
production IdP. Backend production-config rejection, signed-token/time/nonce
checks, HTTP gateway faults, live Redis confinement, live membership revocation
and browser connection/recovery checks run in these suites. The R5g report records
the exact local executions. Fixture success proves local behavior under these
cases only. No external-provider availability, tenant directory import, MFA,
production proxy, TLS chain or production secret-store claim follows.

Retained-binary integration coverage additionally requires the checksum-pinned
artifact and `M9R_LEGACY_API_BINARY` / `M9R_LEGACY_API_SHA256` bindings described
in the R3b/R3c reports. Verify actual provenance/hash before use. Without that
artifact those optional compatibility cases SKIP; do not call them verified by
the plain command above. Never reset the checkout to manufacture an old binary.

## External verification checklist — deferred until provider/domain exist

Create a copy of [the evidence template](R5_IDENTITY_DEPLOYMENT_EVIDENCE_TEMPLATE.md)
outside the retained architecture kits. The identity/release operator records
actual results, source/build identity, observer and sanitized evidence references.
Execute on a staging deployment with production-like trust and controls first:

1. Confirm gateway SPA/callback and API routing, exact CORS origin, certificate
   chain/hostname, private backend network, secure cookie and no-referrer header.
   Check every log/export layer using the non-secret canary. Failure blocks use.
2. Register the public PKCE client and API profile. Verify successful sign-in for
   a provisioned tenant user, signed issuer/audience/subject mapping and live DB
   memberships. Verify an unprovisioned subject and forged provider role claims
   cannot enroll or authorize. Preserve sanitized outcomes, never token contents.
3. Test wrong audience/issuer/key/nonce, expired/replayed callback, mismatched
   browser cookie and exchanged-code replay. A consumed/uncertain exchange needs
   a fresh explicit sign-in; no blind exchange retry. Test overlapping tabs.
4. Revoke membership/deactivate the assigned role while the bearer is still
   signed-valid; tenant access must deny. An authenticated 401 must clear only
   that exact browser connection, including administration/preview transports.
5. Exercise verified-TLS Redis loss/ACL denial and IdP timeout/outage. New sign-in
   must fail closed; a previously valid bearer still uses live PostgreSQL policy.
   Restore infrastructure, then begin a new flow. Do not widen ACLs to recover.
6. Rehearse provider key rotation: install an overlap public JWKS through the
   trusted operator path, restart APIs, verify old/new keys during overlap,
   remove retired keys and restart again after the agreed token window. Record
   rollback and expected old-token rejection. No automatic key fetch is implied.
7. Verify provider-side MFA policy and recovery against the organization's
   assurance requirements. This adapter does not independently enforce `acr` /
   `amr`, MFA enrollment or provider logout; any required application assurance
   claim enforcement needs a separately reviewed contract/change before rollout.
8. Verify clock synchronization and the chosen bounded skew. Recheck Redis
   credential isolation from workers/dispatcher/frontend and least-privilege DB
   roles. Complete R7 operational gates and R6 promotion authority separately.

Every unexecuted step stays NOT RUN. A local PASS may not replace these witnesses.
Provider logout, refresh-token sessions, persistent browser recovery, SAML/SCIM,
automatic enrollment and account recovery are separate features, not hidden R5
completion claims.

## Recovery / rollback

Preserve role versions, publication receipts, membership/audit history and all
business records. Reconcile unknown changes by publication operation receipt or
fresh authorized membership reads; no automatic administrative POST replay.
A configured definition is restored through a new CAS version, never by mutating
history. Membership CAS is the role label and does not detect away-and-back ABA.
Retain pending IDs before reload or full-page provider navigation: the journal
is memory-only. If the user loses access, a different authorized owner or the
controlled operator path must investigate; do not bypass live policy.

To disable optional provider sign-in, remove the five OIDC bindings together and
restart with the remaining valid bearer configuration. Production manual-token
UI remains hidden. Leave outstanding flows to expire naturally; do not delete
unrelated Redis evidence or reuse worker credentials. No rollback undoes an
external action or arbitrary data migration. Any production change/activation
requires its own explicit deployment authorization and release evidence.
