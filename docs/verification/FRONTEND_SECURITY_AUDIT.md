# Frontend and browser sign-in security audit

Pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`), branch
`audit/frontend-security`. Independent, report-only: no product code, test, ADR or other
document was changed. Probe files named `zz_review_*` lived only in the auditor's worktree and
were not committed. Scope: `apps/web/src`, `apps/web` build configuration and lockfile, the
server side of OIDC sign-in (`internal/identity/login.go`, `internal/platform/signin`,
`internal/platform/cache/login.go`), the headers sent by the Go server and the dev server, and
ADR039. Not rediscovered: `M9R_R5C_UI_INDEPENDENT_REVIEW.md` (R5C-UI-01..03) and
`M9R_R5F_INDEPENDENT_REVIEW.md` (R5F-IR-01).

## Progress

| Step | State |
| --- | --- |
| Static review of every file in `apps/web/src`, `vite.config.ts`, `index.html`, lockfile, `signin.go`, `login.go`, `cache/login.go`, `httpserver.go`, config validation | done |
| Server-side sign-in attack probe (real `signin.API` behind real `httpserver.Handler`, fake IdP with PKCE) | done, PASS (see "Attacks that held") |
| Frontend lint, typecheck, unit tests, production build, dev-server headers, hostile-data jsdom probe, lint-guard probe, `npm audit` | one combined `hv` job queued at 06:36 (log in the auditor's scratchpad); not started at the time of this commit |

## Findings

Severity scale of the brief. No HIGH and no MEDIUM finding was confirmed in scope. FS-01, FS-02 and FS-05 carry
an "evidence pending" mark until the queued frontend run completes; they are stated now so that they are not lost.

### FS-03 LOW (quantifies a documented residual): behind the documented same-origin gateway the sign-in quota has one per-remote bucket

* `internal/platform/signin/signin.go:71` keys the admission quota on `net.SplitHostPort(r.RemoteAddr)` and
  `internal/platform/cache/login.go:24` refuses at `p>=20` (per remote) or `g>=120` (global) within a 60 s window.
  `docs/operations/R5_IDENTITY_DEPLOYMENT_RUNBOOK.md:52-58` puts every browser behind one gateway address, so the API
  sees one remote for all users. The runbook (lines 71-74) says the quotas "do not certify a production abuse envelope"
  but not that sign-in capacity is 20 starts per minute for the whole deployment, nor that any anonymous caller
  (forged `Origin` header, no cookie, no token) can spend it.
* Repro (executed, `zz_review_attack_test.go` `TestZZQuotaKey`, not committed): 4 starts from one TCP peer with four
  different `X-Forwarded-For` values reach the store with 1 distinct per-remote key; an anonymous client with only
  `Origin` and `Content-Type: application/json` receives 200 and takes a slot. The Redis script itself was not
  executed (NOT RUN, no database stack); the limit is read from the quoted line.
* The SPA reduces every start failure, including `429` with `Retry-After: 60`, to "Sign-in could not start"
  (`apps/web/src/auth/SignIn.tsx:19-20`), so users get no wait guidance and retry.
* Fix: document the single-bucket consequence, or key the per-remote bucket on a client address asserted by a
  gateway that the API trusts only from a configured proxy address; show a busy state in the SPA.

### FS-04 LOW: `vite.config.ts` loads the whole repository `.env` into the web process

* `apps/web/vite.config.ts:10` is `{ ...loadEnv(mode, root, ''), ...process.env }` with `envDir: root`. ADR039
  (`docs/adr/039-m9r-oidc-browser-sign-in.md:37-39`, "Runtime child filtering excludes this credential from workers,
  dispatchers and the web process") and `scripts/runtime-env.mjs:26` (web child receives only `VITE_API_BASE_URL`
  and `APP_ENV` besides the shared list; tested in `scripts/runtime-env.test.mjs:10`) isolate the web process, but
  the config re-reads every variable of the root `.env` (database, operator, event and sign-in Redis credentials,
  see the names in `.env.example`) into the dev-server process. Only `HTTP_ADDR` is used.
* Repro (executed, synthetic values): a temporary `.env` holding `MIGRATION_DATABASE_URL=synthetic-<hex>` and
  `SIGNIN_PASSWORD=synthetic-<hex>`; `loadEnv('development', dir, '')` returns both, while
  `loadEnv('development', dir, ['HTTP_ADDR','VITE_'])` returns neither (`HTTP_ADDR` and `VITE_API_BASE_URL` still load).
* Impact: no leak today (the object is local to the config function and Vite exposes only `VITE_*` to the client);
  it contradicts the isolation the ADR states and puts the values in the memory of the process that runs third-party
  dev tooling. Development only.
* Fix: `loadEnv(mode, root, ['HTTP_ADDR', 'VITE_'])`.

### FS-01 LOW: no browser-security header contract (CSP, framing, HSTS, Permissions-Policy, COOP) in the repository or in the deployment runbook

* The repository never serves the SPA in production (no `FileServer`/`embed` in `cmd` or `internal`;
  `README.md:173-176` and the runbook say a gateway does). Headers actually set: the Go middleware sets only
  `X-Content-Type-Options: nosniff` and `Cache-Control: no-store` (`internal/platform/httpserver/httpserver.go:146-147`),
  plus `Referrer-Policy: no-referrer` on the completion reply (`signin.go:130`); the dev server sets only
  `Referrer-Policy` (`apps/web/vite.config.ts:17`); `apps/web/index.html` has a `referrer` meta and nothing else.
  The runbook "Gateway contract" (`docs/operations/R5_IDENTITY_DEPLOYMENT_RUNBOOK.md:52-60`) demands HTTPS, the same
  origin, log redaction and `Referrer-Policy: no-referrer`; the evidence template (`R5_IDENTITY_DEPLOYMENT_EVIDENCE_TEMPLATE.md:15`)
  checks "cookie security, no-referrer, callback canary". A search of `docs` for content-security, frame-ancestors,
  X-Frame, HSTS, Strict-Transport and Permissions-Policy finds nothing outside the test IdP fixture's own CSP
  (`apps/web/e2e/oidc-provider.ts:41`).
* Executed: the completion response of the real handler carries `CSP=""` and `XFO=""` (probe line "complete headers").
  Dev-server capture: see "Queued run" below.
* Impact: the SPA keeps the bearer token in memory and performs role, ownership and team administration. No XSS sink
  exists today (see "Attacks that held"), so a CSP is defence in depth; the missing `frame-ancestors`/`X-Frame-Options`
  leaves the Sign in and administration buttons frameable (practical only if the IdP allows framing); no HSTS or
  `Permissions-Policy` is part of the contract anywhere.
* Fix: add to the runbook and to the evidence template a required header set for the SPA host, for example
  `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'`
  (candidate; not browser-tested here, and the built `index.html` must be checked for inline scripts), `X-Content-Type-Options: nosniff`,
  `Strict-Transport-Security`, `Permissions-Policy` with the unused features denied, `Cross-Origin-Opener-Policy: same-origin`;
  and add `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'` to the API middleware (JSON only).

### FS-02 LOW: the lint configuration does not forbid the sinks the code base avoids (evidence pending)

* `apps/web/eslint.config.js:6-9` is `js.configs.recommended` + `typescript-eslint` recommended + the `react-hooks` rules.
  None of `no-eval`, `no-implied-eval`, `no-new-func`, `no-script-url`, a ban on `localStorage`/`sessionStorage`/`document.write`/`window.open`,
  or a `dangerouslySetInnerHTML` rule is in force; `eslint-plugin-react` is not installed. "No HTML injection sink,
  no web storage" is therefore a property of today's code, not of the gate.
* Repro (queued, see below): a file with `dangerouslySetInnerHTML`, `eval`, `setTimeout('...')`, `location.href = 'javascript:...'`,
  `window.open`, `document.write`, `localStorage.setItem('token', ...)` and `<a href="javascript:..." target="_blank">`
  linted with the repository configuration.
* Fix (core ESLint, no new dependency): `no-eval`, `no-implied-eval`, `no-new-func`, `no-script-url`,
  `no-restricted-globals`/`no-restricted-properties` for the storage and `document.write` APIs, and
  `no-restricted-syntax` with `JSXAttribute[name.name='dangerouslySetInnerHTML']`.

### FS-05 LOW: names and labels are rendered without neutralising bidirectional or zero-width characters (evidence pending)

* `apps/web/src/administration/contracts.ts:13` rejects only `\p{Cc}` in team names; the SQL function rejects only
  `[[:cntrl:]]` (`db/migrations/00043_m9r_team_admin.sql:39-40`) and team names are not unique
  (`db/migrations/00010_v3_roles_and_teams.sql:6-9`); application labels are 1..200 characters of anything
  (`contracts/jsonschema/application-v1.5.schema.json`, `label`); record strings are only length and UTF-8 checked
  (`internal/runtime/data/values.go:52`). The parent-team `<select>` in `TeamsPanel.tsx` lists names without IDs.
  The project rejects these character classes elsewhere (`internal/promotion/decisions.go:186-190`,
  `internal/ai/service.go:425`), so the gap is an inconsistency.
* Impact: all of it renders as text (no injection), but two teams or labels can look identical or reversed to an
  administrator choosing a parent team or reading a table; hierarchy changes affect record access (the panel says so).
* Repro: jsdom probe with `U+202E`, `U+2066`, `U+200B`, `U+FEFF` in team names and record values (queued, see below);
  SQL acceptance not run (no database stack). Fix: apply the promotion-note rule to team names, show the team ID next to
  the name in selects, isolate user text with `unicode-bidi: isolate`.


## Attacks that held

Each line names the attack that was executed. Probe: `internal/platform/signin/zz_review_attack_test.go`
(not committed): a TLS gateway running the real `httpserver.Handler` (CORS and body budget) with the
real `signin.API` and `identity.BrowserLogin`, an in-memory flow store with atomic take, and a fake
IdP that enforces S256 PKCE, the exact `redirect_uri`, `client_id`, `grant_type` and single-use codes.
Command: `/home/user/bin/gt test -count=1 -run TestZZ -v ./internal/platform/signin` (no `-race`).

| Attack | Executed input | Observed |
| --- | --- | --- |
| Positive control | start, issue code against the real challenge, complete | 200 with a token; the exchange used the correct verifier, so PKCE is wired. Binding cookie value is not in the body and differs from `state` |
| Replay of a consumed flow | same cookie, state and code a second time | 401, 0 extra exchanges |
| Login CSRF (attacker callback delivered to the victim) | attacker `code`+`state` with the victim's cookie | 401, 0 exchanges |
| Authorization-code injection | attacker code with the victim's own state and cookie | 503, the IdP rejected it on PKCE; no token |
| Completion without the binding cookie | correct code and state, empty jar | 401 |
| Origin forgery on start and complete | `null`, trailing slash, upper-case, `http://`, suffix host, foreign host, `:443`, missing, `*` | all 403, flow store untouched (saves and takes unchanged) |
| Redirect URI / forwarding header influence | `X-Forwarded-Host`, `X-Forwarded-Proto`, `Forwarded`, `X-Original-URL`, `X-Forwarded-For` on start | `redirect_uri` stays the configured value |
| Parameter injection into the token request | `code` = `x&redirect_uri=...&code_verifier=AAAA&grant_type=password` | provider saw the literal string as `code`, configured `redirect_uri`, `grant_type=authorization_code`, 43-char verifier; 503 |
| ID/access token forgery at the IdP (15 modes) | wrong and missing nonce, ID `alg:none`, ID `HS256` keyed by the public modulus, access `alg:none`, access `HS256`, ID and access swapped, wrong access audience, expired access, unknown `kid`, `sub` mismatch, wrong ID audience, ID token returned as access token, 307 redirect from the token endpoint, 70 KiB reply | every mode 401 or 503, no token in the reply |
| Parallel completion of one flow | 40 goroutines, same cookie/state/code | exactly 1 success, 1 exchange |
| Entropy | 60 flows | 240 distinct 43-character values for state, nonce, challenge and binding |
| CORS | preflight from the app origin, a foreign origin and `null` | 204 with `Access-Control-Allow-Origin` equal to the origin and no `Access-Control-Allow-Credentials`; 403 for the others; no wildcard |
| Cookie attributes | start in dev profile with https redirect and in production profile | `__Host-framework_login=<43 chars>; Path=/; Max-Age=300; HttpOnly; Secure; SameSite=Lax` |
| Completion response | headers | `Cache-Control: no-store`, `Pragma: no-cache`, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, JSON, no `Set-Cookie` |
| Error and redirect pages that echo input | `//evil.example/x`, `/a/../b`, `/v1/auth//provider`, encoded and CRLF paths | mux cleaning answers 307 with a relative `Location` (`/evil.example/x`), HTML-escaped body; other paths get the static JSON 404. No open redirect, no header injection |
| Unit suites | `gt test -count=1 ./internal/platform/signin ./internal/identity` | both `ok` |

## NOT RUN

* Everything that needs the shared heavy lock (frontend lint, typecheck, unit tests, production
  build, jsdom hostile-data probe, dev-server header capture) was queued with `hv` and had not
  started when this progress commit was made.
* Redis-backed admission and flow store (no database stack in this task).
* `npm audit` (no network); dependency review is from the auditor's knowledge only and the locked
  versions are newer than that knowledge.
