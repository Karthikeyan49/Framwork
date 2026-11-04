# Frontend and browser sign-in security audit

Pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9` (code head `585499d`), branch
`audit/frontend-security`. Independent and report-only: no product code, test, ADR or other
document was changed. Probe files named `zz_review_*` lived only in the auditor's worktree and were
removed or reverted before every commit. Scope: `apps/web/src`, `apps/web` build configuration and
lockfile, the server side of OIDC sign-in (`internal/identity/login.go`, `internal/platform/signin`,
`internal/platform/cache/login.go`), the headers sent by the Go server and the dev server, and ADR039.
Not rediscovered: `M9R_R5C_UI_INDEPENDENT_REVIEW.md` (R5C-UI-01..03) and `M9R_R5F_INDEPENDENT_REVIEW.md`
(R5F-IR-01).

## Verdict

**PASS WITH FINDINGS** for this scope: no HIGH and no MEDIUM finding. Six LOW findings (FS-01..FS-06): three
hardening or documentation gaps (FS-01, FS-02, FS-06), one that quantifies a residual the documents already state
(FS-03), one that contradicts an ADR sentence without a practical leak (FS-04), and one spoofing class (FS-05). Every attack listed under "Attacks that held"
was executed; what could not be executed is listed under "NOT RUN".

## Progress

| Step | State |
| --- | --- |
| Static review of every file in `apps/web/src`, `vite.config.ts`, `index.html`, lockfile, `signin.go`, `login.go`, `cache/login.go`, `httpserver.go`, config validation, ADR039/ADR061, runbook | done |
| Server-side sign-in attack probe (real `signin.API` behind real `httpserver.Handler`, fake IdP with PKCE) and three mutation checks | done |
| Frontend lint, typecheck, unit tests, production build, dev-server header capture, hostile-data jsdom probe, mutation check, `npm audit` | done (one `hv` job, 06:54:30 to 06:55:41, after an 18 minute wait for the lock) |
| Second short `hv` job (07:08:24 to 07:09:32, after a 10 minute wait): probe detail lines, `.env` fallback check, dev-server header count, `npm audit` including dev dependencies | done; the two lint-guard runs of that job failed on a probe-command error (see "Second job") |

## Executed commands

| Command | Result |
| --- | --- |
| `hv nice -n 10` job: `npm run lint` (`eslint . --max-warnings 0`) | PASS, rc 0 |
| same job: `npm run typecheck` (`tsc --noEmit`) | PASS, rc 0 |
| same job: `npm test` (`vitest run`) | PASS, 19 files, 126 tests, 26.0 s |
| same job: `npm run build -- --outDir <scratch> --emptyOutDir` | PASS, 45 modules, `index.html` 452 B, JS 351,729 B, CSS 5,354 B |
| same job: `npm audit --omit=dev --audit-level=high` (the CI step, `.github/workflows/ci.yml:41-42`) | PASS, "found 0 vulnerabilities" (registry reachable through the proxy on 2026-11-03) |
| second job: `npm audit` (all dependencies) | FAIL as a gate would see it: 13 advisories (2 high, 11 moderate), all in development tooling, see FS-06 |
| `gt test -count=1 ./internal/platform/signin ./internal/identity` | PASS |
| `gt test -count=1 ./internal/platform/httpserver ./internal/config` | PASS |
| `gt test -count=1 -run TestZZ -v ./internal/platform/signin` (auditor probe, not committed) | PASS, 4 tests |
| Mutation of `signin.go:47` (`origin()` always true) with that probe | FAIL as it must: `origin "" accepted on /v1/auth/login/start (status 200)` |
| Mutation of `login.go:171` (flow key ignores the browser binding) | FAIL as it must: `login CSRF accepted` |
| Mutation of `login.go:244` (ID-token nonce not compared) | FAIL as it must: `mode nonce_wrong accepted`, `mode nonce_missing accepted` |
| Mutation of `Workspace.tsx` (page label via `dangerouslySetInnerHTML`) with the jsdom probe | FAIL as it must: `<svg onload="window.__pwned=4">` found in the DOM |

All mutations were reverted with `git checkout --`; `git status --short` showed only the report.

## Findings

Severity scale of the brief. No finding is HIGH or MEDIUM.

### FS-01 LOW: no browser-security header contract (CSP, framing, nosniff on the SPA, HSTS, Permissions-Policy, COOP) in the repository or the deployment runbook

* The repository never serves the SPA in production (no `FileServer` or `embed` in `cmd` or `internal`;
  `README.md:173-175` and the runbook say a gateway does). Headers actually set: the Go middleware sets only
  `X-Content-Type-Options: nosniff` and `Cache-Control: no-store` (`internal/platform/httpserver/httpserver.go:146-147`),
  plus `Referrer-Policy: no-referrer` on the completion reply (`signin.go:130`); the dev server sets only
  `Referrer-Policy` (`apps/web/vite.config.ts:17`); `apps/web/index.html` has a `referrer` meta and nothing else.
  The runbook "Gateway contract" (`docs/operations/R5_IDENTITY_DEPLOYMENT_RUNBOOK.md:52-60`) demands HTTPS, the same
  origin, log redaction and `Referrer-Policy: no-referrer`; the evidence template
  (`R5_IDENTITY_DEPLOYMENT_EVIDENCE_TEMPLATE.md:15`) checks "cookie security, no-referrer, callback canary". A search of
  `docs` for content-security, frame-ancestors, X-Frame, HSTS, Strict-Transport and Permissions-Policy finds nothing
  outside the test IdP fixture's own CSP (`apps/web/e2e/oidc-provider.ts:41`).
* Executed. Dev server (`npx vite --port 48391`, `curl -D -`): `GET /` returns `Vary: Origin`, `Content-Type`,
  `Cache-Control: no-cache`, `Etag`, `Referrer-Policy: no-referrer` and none of CSP, `X-Frame-Options`,
  `X-Content-Type-Options`, `Permissions-Policy`, `Strict-Transport-Security`. API completion reply (probe): `CSP=""`,
  `XFO=""`. The production build's `dist/index.html` contains exactly one `<script type="module" crossorigin src=...>`
  and one stylesheet link, no inline script and no inline style, so a strict CSP is deployable.
* Impact: the SPA keeps the bearer token in memory and performs role, ownership and team administration. No XSS sink
  exists today (see "Attacks that held"), so a CSP is defence in depth; without `frame-ancestors` or
  `X-Frame-Options` the Sign in and administration buttons can be framed (practical only if the IdP permits framing).
* Fix: add to the runbook and to the evidence template a required header set for the SPA host, for example
  `Content-Security-Policy: default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'`
  (candidate; not browser-tested here), `X-Content-Type-Options: nosniff`, `Strict-Transport-Security`, a
  `Permissions-Policy` that denies the unused features, `Cross-Origin-Opener-Policy: same-origin`; and add
  `Content-Security-Policy: default-src 'none'; frame-ancestors 'none'` to the API middleware (JSON only).

### FS-02 LOW: the lint configuration does not forbid the sinks the code base avoids

* `apps/web/eslint.config.js:6-9` is `js.configs.recommended` + `typescript-eslint` recommended + the `react-hooks`
  preset; `eslint-plugin-react` is not installed. No rule bans `dangerouslySetInnerHTML`, `javascript:` URLs,
  `eval`, string `setTimeout`, `new Function`, `localStorage`/`sessionStorage`, `document.write` or `window.open`.
  "No HTML injection sink, no web storage" is therefore a property of today's code, not of the gate.
* Executed (first job): a probe component containing `localStorage.setItem('token', ...)`, `eval`,
  `setTimeout('alert(1)', 0)`, `window.location.href = 'javascript:' + url`, `window.open`, `document.write`,
  `dangerouslySetInnerHTML` and `<a href="javascript:alert(1)" target="_blank">` was linted with the repository
  configuration: `2 problems (1 error, 1 warning)`, both from the React Compiler rules of the `react-hooks` preset
  (`react-hooks/immutability` on the `window.location.href` assignment, `react-hooks/unsupported-syntax` on one
  other construct), which apply only inside components and hooks and are not security rules. Nothing was reported for the
  remaining constructs. A second run for a plain module failed because of a probe-command error (see "Second job"); for
  plain modules the claim therefore rests on reading the configuration, which contains none of those rules.
* Fix (core ESLint, no new dependency): `no-eval`, `no-implied-eval`, `no-new-func`, `no-script-url`,
  `no-restricted-globals`/`no-restricted-properties` for the storage APIs and `document.write`, and
  `no-restricted-syntax` with `JSXAttribute[name.name='dangerouslySetInnerHTML']`.

### FS-03 LOW (quantifies a documented residual): behind the documented same-origin gateway the sign-in quota has one per-remote bucket

* `internal/platform/signin/signin.go:71` keys the admission quota on `net.SplitHostPort(r.RemoteAddr)` and
  `internal/platform/cache/login.go:24` refuses at `p>=20` (per remote) or `g>=120` (global) in a 60 s window.
  The runbook (`R5_IDENTITY_DEPLOYMENT_RUNBOOK.md:52-58`) puts every browser behind one gateway address, so the API
  sees one remote for all users. ADR061 (`docs/adr/061-m9r-pre-database-request-admission.md:66-67`) states that
  behind a proxy all clients may share the socket key, and the runbook (lines 71-74) says the sign-in quotas "do not
  certify a production abuse envelope"; what is not stated is that sign-in capacity is 20 starts per minute for the
  whole deployment and that any anonymous caller (forged `Origin`, no cookie, no token) can spend it.
* Executed (`TestZZQuotaKey`, not committed): 4 starts from one TCP peer with four different `X-Forwarded-For`
  values reach the store with 1 distinct per-remote key; an anonymous client with only `Origin` and
  `Content-Type: application/json` receives 200 and takes a slot. The Redis script was not executed (NOT RUN, no
  database stack); the limits are read from the quoted line.
* The SPA reduces every start failure, including `429` with `Retry-After: 60`, to "Sign-in could not start"
  (`apps/web/src/auth/SignIn.tsx:18-19`), so users get no wait guidance and retry.
* Fix: state the consequence in the runbook, or key the per-remote bucket on a client address asserted by a gateway
  the API trusts only from a configured proxy address; show a busy state in the SPA.

### FS-04 LOW: `vite.config.ts` loads the whole repository `.env` into the web process

* `apps/web/vite.config.ts:10` is `{ ...loadEnv(mode, root, ''), ...process.env }` with `envDir: root`. ADR039
  (`docs/adr/039-m9r-oidc-browser-sign-in.md:37-39`, "Runtime child filtering excludes this credential from workers,
  dispatchers and the web process") and `scripts/runtime-env.mjs:26` (the web child receives only
  `VITE_API_BASE_URL` and `APP_ENV` besides the shared list; tested in `scripts/runtime-env.test.mjs:10`) isolate the
  web process, but the config re-reads every variable of the root `.env` (database, operator, event and sign-in Redis
  credentials; names in `.env.example`) into the dev-server process. Only `HTTP_ADDR` is used.
* Executed with synthetic values: a temporary `.env` holding `MIGRATION_DATABASE_URL=synthetic-<hex>` and
  `SIGNIN_PASSWORD=synthetic-<hex>`; `loadEnv('development', dir, '')` returns both, while
  `loadEnv('development', dir, ['HTTP_ADDR','VITE_'])` returns neither (`HTTP_ADDR` and `VITE_API_BASE_URL` still load).
* Impact: no leak today (the object is local to the config function, Vite exposes only `VITE_*` to the client, and
  the production bundle contains no `postgres://`, `redis://` or `rediss://` string); it contradicts the isolation the
  ADR states and puts the values in the memory of the process that runs third-party development tooling. Development
  only.
* Fix: `loadEnv(mode, root, ['HTTP_ADDR', 'VITE_'])`.

### FS-05 LOW: names and labels are rendered without neutralising bidirectional or zero-width characters

* `apps/web/src/administration/contracts.ts:13` rejects only `\p{Cc}` in team names; the SQL function rejects only
  `[[:cntrl:]]` (`db/migrations/00043_m9r_team_admin.sql:39-40`) and team names are not unique
  (`db/migrations/00010_v3_roles_and_teams.sql:5-9`); application labels are 1..200 characters of anything
  (`contracts/jsonschema/application-v1.5.schema.json:234-238`); record strings are only length and UTF-8 checked
  (`internal/runtime/data/values.go:52`). The parent-team `<select>` in `TeamsPanel.tsx` lists names without IDs.
  The project rejects these character classes elsewhere (`internal/promotion/decisions.go:186-190`,
  `internal/ai/service.go:425`), so the gap is an inconsistency.
* Executed: glibc `iswcntrl` in `C.UTF-8` is true for U+0007, U+009F and U+2028 but false for U+200B, U+200E,
  U+202A..U+202E, U+2066, U+2069 and U+FEFF, so the SQL class does not stop them (the database's actual locale was not
  queried: NOT RUN, no database stack). The jsdom probe renders team names containing U+202E, U+2066, U+200B, U+FEFF
  and U+2060, record strings, labels and enum values containing the same, and 150,000-character labels: all pass the
  decoders and all render as text.
* Impact: no injection, but two teams or labels can look identical or reversed to an administrator choosing a parent
  team or reading a table; hierarchy changes affect record access (the panel says so). Layout of long strings was not
  rendered (jsdom has no layout): NOT RUN.
* Fix: apply the promotion-note rule to team names, show the team ID beside the name in selects, isolate user text
  with `unicode-bidi: isolate`.

### FS-06 LOW: the CI dependency gate omits development dependencies; the full audit reports 13 advisories (2 high)

* `.github/workflows/ci.yml:41-42` runs `npm audit --omit=dev --audit-level=high`, which passes (executed: "found 0
  vulnerabilities"). A full `npm audit` of the locked tree (executed twice on 2026-11-03, text and JSON) reports 13
  advisories, 2 high and 11 moderate, all in development tooling:
  `undici` 8.11.2 (high, range 8.0.0 to 8.12.1, 8 advisories, among them GHSA-4w7w-5xrc-9h26 cache poisoning and
  cross-user disclosure via cross-origin redirects and GHSA-2m5v-pch6-74hx TLS certificate validation bypass via dropped
  `tls` options), pulled in by `jsdom` (the unit-test environment); `zod` 4.6.5 (high, below 4.6.6, GHSA-mrhx-c7xc-m3w5
  memory exhaustion), pulled in by `eslint-plugin-react-hooks`; `minimatch` 10.2.6 (moderate, below 11.0.1, ReDoS through
  glob-part amplification), pulled in by `eslint`, `@eslint/config-array` and `@typescript-eslint/typescript-estree`,
  which marks `eslint`, `@eslint/js` and the typescript-eslint packages as affected (the other 9 moderate entries).
* Impact: development and test tooling that processes trusted repository inputs (the tests mock `fetch`; lint reads the
  repository's own sources); nothing reaches the production bundle (0 advisories with `--omit=dev`). It is also the
  toolchain that serves `127.0.0.1:5173` for the browser suites. No exploit was attempted and jsdom's use of undici
  was not traced, so reachability of the undici TLS and redirect advisories is not established. These advisories come
  from the registry's database, not from the auditor's knowledge.
* Fix: add the full audit (at least `--audit-level=high`) to CI, then move `undici` to 8.12.2 or later (jsdom update or
  an `overrides` entry), `zod` to 4.6.6 or later and `minimatch` to 11.0.1 or later (eslint and typescript-eslint
  update).

## Attacks that held

### Sign-in server

Probe `internal/platform/signin/zz_review_attack_test.go` (not committed): a TLS gateway running the real
`httpserver.Handler` (CORS, body budget) with the real `signin.API` and `identity.BrowserLogin`, an in-memory flow
store with atomic take, and a fake IdP that enforces S256 PKCE, the exact `redirect_uri`, `client_id`,
`grant_type` and single-use codes. Command: `gt test -count=1 -run TestZZ -v ./internal/platform/signin` (no `-race`).

| Attack | Executed input | Observed |
| --- | --- | --- |
| Positive control | start, issue a code against the real challenge, complete | 200 with a token; the exchange used the correct verifier, so PKCE is wired. The binding cookie value is not in the body and differs from `state` |
| Replay of a consumed flow | same cookie, state and code again | 401, 0 extra exchanges |
| Login CSRF (attacker callback delivered to the victim) | attacker `code` + `state` with the victim's cookie | 401, 0 exchanges |
| Authorization-code injection | attacker code with the victim's own state and cookie | 503: the IdP rejected it on PKCE; no token |
| Completion without the binding cookie | correct code and state, empty jar | 401 |
| Session fixation | the client cannot choose the cookie; start is Origin-gated, so a cross-site start answers 403 before `Set-Cookie` | no cookie planted (Origin row below) |
| Origin forgery on start and complete | `null`, trailing slash, upper-case, `http://`, suffix host, foreign host, `:443`, missing, `*` | all 403, flow store untouched (saves and takes unchanged). A leading-space variant is not an attack: Go trims header whitespace on the wire |
| Redirect URI and forwarding-header influence | `X-Forwarded-Host`, `X-Forwarded-Proto`, `Forwarded`, `X-Original-URL`, `X-Forwarded-For` on start | `redirect_uri` stays the configured value |
| Parameter injection into the token request | `code` = `x&redirect_uri=...&code_verifier=AAAA&grant_type=password` | the IdP saw the literal string as `code`, the configured `redirect_uri`, `grant_type=authorization_code` and a 43-character verifier; 503 |
| Token forgery at the IdP, 15 modes | wrong and missing nonce, ID `alg:none`, ID `HS256` keyed by the public modulus, access `alg:none`, access `HS256`, ID and access swapped, wrong access audience, expired access, unknown `kid`, `sub` mismatch, wrong ID audience, ID token returned as access token, 307 from the token endpoint, 70 KiB reply | every mode 401 or 503, no token in any reply |
| Parallel completion of one flow | 40 goroutines, same cookie, state and code | exactly 1 success, 1 exchange |
| Entropy | 60 flows | 240 distinct 43-character values for state, nonce, challenge and binding |
| CORS | preflight from the app origin, a foreign origin and `null` | 204 with `Access-Control-Allow-Origin` equal to the origin, no `Access-Control-Allow-Credentials`, `Vary: Origin`; 403 for the others; no wildcard (`config.go:192-199` rejects `*`) |
| Cookie attributes | start in the dev profile with an https redirect and in the production profile | `__Host-framework_login=<43 chars>; Path=/; Max-Age=300; HttpOnly; Secure; SameSite=Lax`, no `Domain` |
| Completion response | headers | `Cache-Control: no-store`, `Pragma: no-cache`, `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, JSON, no `Set-Cookie` |
| Error and redirect pages that echo input | `//evil.example/x`, `/a/../b`, `/v1/auth//provider`, encoded and CRLF paths | mux cleaning answers 307 with a relative `Location` (`/evil.example/x`) and an HTML-escaped body; other paths get the static JSON 404. No open redirect, no header injection |
| Redirect URI and endpoint configuration | `identity.ValidateProvider` matrix of 23 cases (`gt test -run TestZZValidateProviderMatrix`, probe not committed): trailing slash, query, fragment, userinfo, encoded path, double slash, backslash host, http in production, http non-loopback in development, `localhost.evil.example` and `127.0.0.1.evil.example` look-alikes, `javascript:` authorization endpoint, authorization query, userinfo in the token URL, `offline_access`, quote and space in a scope, no `openid`, CRLF in the client ID | all rejected as expected; the valid case and the two loopback development cases accepted. An operator cannot configure an open redirect or an unbound callback |
| Return-to / open redirect | `grep -rniE "return_to\|returnTo\|redirect_to\|next=" cmd internal apps/web/src` outside tests | no such parameter exists; the SPA always navigates to the constant `/access` (`SignIn.tsx:39`) |
| Business-route CSRF | `access.go:250-254` | business routes authenticate only `Authorization: Bearer`; cookies are read only by `login/complete`; SPA fetches use `credentials: 'omit'` except the two sign-in POSTs (`same-origin`) and `GET /readyz` |
| Logout | design | client-side memory clearing only (`Session.tsx:34-37`); the signed token stays valid up to its one-hour lifetime and no IdP logout exists. The runbook and ADR039 state this; not a finding |

### Frontend source and build

| Attack | Executed input | Observed |
| --- | --- | --- |
| XSS sinks, static | `grep -rnE "dangerouslySetInnerHTML\|innerHTML\|outerHTML\|insertAdjacentHTML\|document\.write\|eval\(\|new Function\|srcdoc\|createContextualFragment\|DOMParser\|<iframe\|<script\|window\.open\|postMessage\|localStorage\|sessionStorage\|document\.cookie\|indexedDB\|style=\{" apps/web/src` (non-test) | no hit. `href=` appears once, the constant `#main`; `window.location.assign` once (`SignIn.tsx:18`) on a validated URL; no `Blob`, download, worker, storage, `console` or `debugger` |
| Hostile data through the real components | vitest + jsdom probe `src/zz_review_hostile.test.tsx` (not committed), 6 tests PASS: descriptor labels, enum values and component labels containing `<img onerror>`, `<script>`, `javascript:`, `<svg/onload>`, a style-breaking string, bidi and zero-width characters and 150,000-character labels; records with the same in string, money amount and currency values and with keys `__proto__`, `constructor`, `toString`, `hasOwnProperty`; hostile session user and tenant names, roles, decision fields and team names; all clicked through (open record, tabs, relation picker, teams panel) | no `img/script/iframe/object/embed/svg/math/link/meta/base/style/a/frame/form[action]/[onerror]/[onload]/[style]` element created by data, no `on*` attribute, `window.__pwned` undefined, `Object.prototype.polluted` undefined (asserted) |
| Probe has teeth | the same probe with one label rendered through `dangerouslySetInnerHTML` | FAIL: `<svg onload="window.__pwned=4">` found |
| Error pages that echo input | a 400 reply whose body and `X-Request-ID` contain `<img onerror>` and `<script>` | the UI shows only the fixed message (`Workspace.tsx:21-35`); no body text is rendered |
| Prototype pollution | no merge or deep-set helper exists (`grep "Object.assign\|merge\|lodash\|deepSet"` outside tests: none); decoders fed `{"__proto__":...}` and `{"constructor":{"prototype":...}}` in record values, result pages and role condition values, and `parseConditionValue` fed the same | `Object.prototype` untouched (asserted); `JSON.parse` creates own properties only |
| DOM clobbering | no data-driven HTML exists, so no element with an attacker `id` or `name`; the only DOM lookup is `document.getElementById('root')` at start (`main.tsx:11`), before any data. The `id` and `name` attributes present in the rendered tree were only `input-0.0.0` to `input-0.0.4` and `choices-input-0.0.4`, all index-derived | none possible through data |
| `window.location.assign` target | 17 hostile authorization URLs through `loginClient.start`: `javascript:`, tab-split `java\tscript:`, mixed-case, `data:`, `file:`, `blob:`, `vbscript:`, protocol-relative, `http://` non-loopback, userinfo, fragment, duplicated `state`, foreign `redirect_uri`, `code_challenge_method=plain`, short nonce, wrong scope, no parameters | all rejected (asserted); https and http-loopback positive controls accepted |
| Callback returns | 9 hostile callbacks: extra `redirect_uri`, `return_to`, `next`, 4,097-character code, fragment token, CRLF in code, 42-character state, 2,049-character `iss`, duplicated `state` | all `{kind:"error"}` (asserted); `replaceState` was called 9 times, every time with `/auth/callback`. `/auth/callback/` (trailing slash) returns `undefined` and is not scrubbed because it is not the registered redirect URI and carries no IdP secret |
| Token and session storage | grep of non-test source and of the production bundle | 0 occurrences of `localStorage`, `sessionStorage`, `document.cookie`; the token lives only in React state (`Session.tsx`) |
| Source maps | `find dist -name '*.map'` and `grep -r sourceMappingURL dist` | 0 and 0 |
| Third-party scripts and leaks | bundle string and URL census | external names are only XML namespaces (`www.w3.org`), documentation links in React and React Router error strings (`react.dev`, `reactrouter.com`) and `http://localhost` (URL parsing); no `postgres://`, `redis://`, `rediss://`, `framework_login`, `http://127.0.0.1:8080`. `innerHTML` (5), `dangerouslySetInnerHTML` (13) and `postMessage` (1) come from react-dom and react-router vendor code, not from application source |
| Dev server | `npx vite --port 48391` + `curl` | foreign `Origin` gets 200 without `Access-Control-Allow-Origin`; foreign `Host: evil.example` gets 403; `/@fs/etc/passwd` 403; `/@fs/.../go.mod?raw` (outside the root) 403; `/.env`, `/.env?raw`, `/@fs/.../.env`, `/@fs/.../.env?raw` and `/.env.example` return 200 with a body whose md5 equals the SPA index (the SPA fallback, not the file), `/@fs/.../.env.example` 403; none of the five security headers is present on `/`; WebSocket upgrade with a foreign Origin and a bad token, and with the same Origin and no token, both 400 |
| Dependencies | lockfile census | 264 resolved packages all from `registry.npmjs.org` with integrity; no git or URL dependency; the only lifecycle script is the optional macOS `fsevents`; direct versions pinned exactly; `scripts/bootstrap.sh:19` installs with `npm ci --ignore-scripts`; production dependencies are react 19.3.0, react-dom 19.3.0, react-router-dom 7.18.4 (+ `cookie` 1.1.1, `set-cookie-parser` 2.7.2, `scheduler` 0.28.0); `npm audit --omit=dev` found 0 vulnerabilities |

### Dependency review from the auditor's knowledge

Every locked version (react 19.3, react-router 7.18, vite 8.3, vitest 5, jsdom 30, eslint 10, TypeScript 6,
Playwright 1.64) is newer than what the auditor knows, so the knowledge-based part of this review is "no known
problem", not "verified clean"; the registry audit is the stronger evidence (0 advisories for production
dependencies, 13 for development tooling, FS-06). The
historic issue classes of this stack are all fixed in versions below the locked ones: Vite dev-server file read and
`server.fs` bypasses and cross-origin WebSocket access (6.x, 7.x), the esbuild dev-server CORS issue (esbuild is not in
the lockfile; Vite 8 uses rolldown), the Vitest API-server RCE (3.0.4), React Router header-spoofing and redirect
issues (framework mode, 7.9 and earlier; this SPA uses only declarative `BrowserRouter`), `tough-cookie` prototype
pollution (4.1.2), `cookie` (0.7), Playwright browser download without TLS verification (1.55). The CI audit
(`ci.yml:41-42`) uses `--omit=dev`, so the development toolchain that serves `127.0.0.1:5173` for the browser suites
is outside it, which is how FS-06 stays invisible to the gate.

## Second job

Ran 07:08:24 to 07:09:32 under `hv`. Results:

* Hostile-data probe, details written to a file because Vitest 5 prints nothing for passing tests: 6 passed. Lines:
  `dangerous-elements: [] on*-attributes: []`; the rendered tree held 97 nodes and 905,447 bytes of HTML (the 150,000
  character labels) and contained the literal hostile strings as text; `error-text: "Check the field values or query
  limits and try again."`; teams console `dangerous-elements: []`; `own __proto__ on row.values: true polluted: undefined`;
  17 of 17 authorization URLs `REJECTED`; 9 of 9 callbacks `error`. (A `root intact: null` line is meaningless: the probe
  renders into a test container, not `index.html`.)
* Dev server: the `.env` body comparison and the header count are in the table above (0 of 5 security headers).
* Lint-guard runs for a component and a plain module: both exited 2 with "The unix formatter is no longer part of core
  ESLint" because the probe command asked for `-f unix`, which ESLint 10 dropped. That is an error of the probe command,
  not a repository result. It was not repeated (the lock queue took 10 minutes); NOT RUN.
* Full `npm audit`: FS-06.

## Observations (not findings)

* `GET /readyz` is unauthenticated and lists the internal dependency names and their state; the Service status page
  consumes it. Restrict it at the gateway if that is not wanted.
* The binding cookie is `SameSite=Lax` (ADR039 chose it); it is only needed by a same-origin `fetch` POST, so `Strict`
  would also work. Hardening only.
* `APP_ENV` defaults to `development` (`internal/config/config.go:57`) and the runbook table requires `production`;
  a deployment that forgets it gets `manual_token: true`, the preview routes and the looser transport rules at once.

## NOT RUN

* `make test-e2e` and `make test-signin-e2e` (Playwright browsers, Go API, PostgreSQL, Redis and the fixture IdP);
  no database stack in this task. The in-process equivalents above were executed instead.
* Redis-backed admission and flow store (the Lua script and ACL were read, not executed) and the SQL name
  validation (`[[:cntrl:]]`) against the real database locale.
* ESLint behaviour for the sink constructs in a plain module (the probe command used a formatter ESLint 10 removed; the
  component run of the first job stands).
* Layout of very long labels and names, and any real browser rendering (jsdom has no layout); the candidate CSP in
  FS-01 was not tried in a browser.
* A real external IdP, a TLS-terminating gateway or any production header set (none exists in the repository).
* DNS rebinding against the API (it ignores `Host`; sign-in is Origin-gated and business routes are bearer-gated, so no
  consequence was constructed).
