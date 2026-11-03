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
| Frontend lint, typecheck, unit tests, production build, hostile-data jsdom probe | queued behind the shared heavy lock; see "NOT RUN" until filled |

## Findings

(filled below as they are confirmed)

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
