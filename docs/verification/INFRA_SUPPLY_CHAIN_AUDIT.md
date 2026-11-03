# Infrastructure, CI, bootstrap and supply-chain audit (IS-01 ...)

Pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9`, branch `audit/infra-supply-chain`.
Scope: `.github/**`, `Makefile`, `scripts/**`, `infra/**`, `go.mod`/`go.sum`, `apps/web/package.json` and lockfile, `.env.example`,
`.gitignore`, Dockerfiles (none exist: `git ls-files` finds no Dockerfile). No database stack was used. Static analysis first;
the only network use was read-only (go.dev, vuln.go.dev, registry.npmjs.org, `git ls-remote` of three public Action repositories).
Report-only: no product file was changed. Probe files live in the session scratch directory, outside the worktree.

## Progress (kept current)

| time (UTC) | state |
|---|---|
| 06:50 | static review of every in-scope file done; history scan of all refs done; first findings written (this commit) |
| next | govulncheck result (build was queued behind the heavy-run lock), npm audit of dev dependencies, report polish |

## Verdict

**PASS WITH FINDINGS** for the infrastructure scope. No HIGH finding. No secret, key, `.env` or credential-bearing DSN exists
anywhere in the history of the refs of this clone (first scan: 88 refs, 455 non-merge commits, 206,578 added lines; re-scanned at the end, see row 14). The bootstrap download,
the Action pins, the npm lockfile and the Go module sums all verify. One MEDIUM (the pinned Go toolchain is one security patch
behind) and nine LOW findings.

## Findings

### IS-01 MEDIUM - pinned Go toolchain 1.27.2 is behind a published crypto/x509 security fix (1.27.3, 2026-10-19)

- Pins: `go.mod:3` (`go 1.27.2`), `scripts/go.sh:9` (accepts only `go1.27.2 `), `scripts/bootstrap.sh:6,10,11` (downloads
  `go1.27.2.linux-amd64.tar.gz`, checks one SHA-256), `.github/workflows/ci.yml:17-19` (`go-version-file: go.mod`, `cache: false`).
- Evidence (executed, read-only network): `https://go.dev/doc/devel/release` states "go1.27.3 (released 2026-10-19) includes
  security fixes to the crypto/x509 package". The Go vulnerability database record `GO-2026-6809` (alias `CVE-2026-111943`,
  "Distrust compromised intermediate certificates in crypto/x509") lists stdlib fixed in `1.27.3`, affected symbol
  `crypto/x509.Certificate.Verify`. The date of the audit is 2026-11-03: the fix was 15 days old and no gate, test or document
  mentions it.
- Reachability (static): every outbound TLS client of the product verifies peers through `Certificate.Verify`:
  `internal/identity/login.go:148` (OIDC token endpoint), `internal/ai/provider_http.go:91-101` (AI provider),
  `internal/tools/executor.go:77-87` (tools), plus go-oidc's discovery/JWKS fetch and `verify-full` PostgreSQL / `rediss` Redis
  (`internal/config/config.go:186,298,312`). The record is a denylist of known-compromised intermediates, so exploitability is
  narrow (a chain through such an intermediate that the platform trust store still accepts). The severity is MEDIUM because the
  fix is a one-line version bump in four places and nothing in the repository can detect the next one (see IS-06).
- Repro: `curl -s https://vuln.go.dev/ID/GO-2026-6809.json` (fields `affected[].ranges` and `ecosystem_specific.imports`), and
  `curl -s 'https://go.dev/dl/?mode=json&include=all'` (newest stable `go1.27.3`).
- Fix direction: bump `go.mod`, `scripts/go.sh`, `scripts/bootstrap.sh` (URL and checksum from go.dev), re-run the gates.

### IS-02 LOW - `golang.org/x/net v0.59.0` carries seven published advisories (fixed in 0.60.0 and 0.62.0); reachability not proven either way

- `go.mod:44`. Method: the 43 modules linked into `./cmd/...` (`go list -deps`) were matched against every
  `vuln.go.dev` record of the 15 of them that appear in the index (75 records fetched, semver ranges evaluated). Only x/net matches:
  `GO-2026-6603`, `6610`, `6611`, `6612`, `6617` (x/net/http2, fixed 0.60.0) and `GO-2026-6860`, `6866` (x/net/html, fixed 0.62.0).
- `golang.org/x/net/http2`, `hpack`, `trace`, `idna` are linked into `api`, `worker`, `dispatcher` (not `migrate`) only through
  `google.golang.org/grpc/internal/transport`; `x/net/html` is not linked. No repository package imports x/net directly
  (`go.mod` lists it as indirect), and the API server uses the standard library's HTTP/2, which `1.27.2` already patched.
  I therefore expect the five http2 advisories to be "linked, not called", but this is an inference: see the govulncheck row
  under "Not run" for whether the symbol-level tool could be run.
- Repro: `/home/user/bin/gt list -deps -f '{{with .Module}}{{.Path}}@{{.Version}}{{end}}' ./cmd/api | grep x/net` and the
  record ids above at `https://vuln.go.dev/ID/<id>.json`.

### IS-03 LOW - pre-existing `.env` keeps its mode while new credentials are appended to it

- `scripts/m8-env.mjs:20` and `scripts/m6r-env.mjs:19` call `fs.appendFileSync('.env', ..., {mode:0o600})`. The `mode` option applies only
  when the call creates the file. `scripts/env.mjs:25` creates `.env` with `flag:'wx', mode:0o600` (held), and `scripts/m8-env.mjs:24`
  chmods the ACL file, but nothing checks or repairs the mode of a `.env` that already exists, although
  `scripts/dev-identity.mjs:22` refuses a signing key with group/other bits. `.env.example:1` tells the developer "Copy to .env or run make env".
- Executed probe (scratch directory, run-time generated 64-hex Redis password, no real `.env` touched): `.env` created with mode 0644, then
  `node scripts/m8-env.mjs` and `node scripts/m6r-env.mjs`. Result: `.env` stays `644` after both scripts, now holding the generated
  event-sender, event-receiver, event-operator (Redis `+@all`), sign-in, worker and dispatcher credentials; `.local/m8/users.acl` is `600`.
  `scripts/with-env.mjs` and `scripts/dev.mjs` read the file without a mode check.
- Fix direction: in the two append scripts `fs.chmodSync('.env', 0o600)` (or refuse when `mode & 0o077`), as `dev-identity.mjs` does.

### IS-04 LOW - the Redis container runs as root (the `command` override skips the image's privilege drop)

- `infra/compose/compose.yaml:25-28` replaces the image command with `/bin/sh -ec '...; exec redis-server /tmp/redis.conf'`. The official
  image's entrypoint drops to the `redis` user only when its first argument is `redis-server`; here it is `/bin/sh`. No `user:` is set.
- Observed (host process listing, no `docker` command used): the three running `redis-server 0.0.0.0:6379` processes (the `bind 0.0.0.0` of
  `compose.yaml:28`) have uid 0, while the `postgres` workers run as uid 999 and `/otelcol-contrib` as uid 10001.
- Why it matters here: the same Redis holds `event_operator` (`~* &* +@all`, `scripts/m8-env.mjs:23`) and `framework_signin` (`+eval`).
  An operator-credential holder or a Lua sandbox escape lands as container root rather than an unprivileged user. Scope is loopback-only
  development Compose, hence LOW.
- Note for the fix: the design currently depends on root. `compose.yaml:29` bind-mounts a 0600 file owned by the host user and the shell
  `cp`s it as root; dropping to uid 999 needs the copy done before the drop (or a readable mode / `user:` mapping).

### IS-05 LOW - Compose hardening: images by mutable tag, no capability / privilege / resource limits, passwords stay in container environments

- `infra/compose/compose.yaml:4` `postgres:18.6`, `:22` `redis:8.2.3`, `:38` `otel/opentelemetry-collector-contrib:0.148.0`: tags, not digests.
  `docs/adr/025-m9r-membership-evaluation-and-performance-floor.md:25` asks the performance run to "Record actual image digest", but the
  Compose file cannot pin one.
- `docker compose config` (rendered with dummy values) shows no `cap_drop`, `security_opt: no-new-privileges`, `read_only`, `mem_limit`, `cpus` or
  `pids_limit` for the default profile; only `reference-performance.yaml` sets `cpus: 4` and `mem_limit: 8g`, and for Postgres only.
- `compose.yaml:8-10` passes `MIGRATOR_PASSWORD` and `APP_DB_PASSWORD` into the Postgres container environment for its whole life although only
  first-time init needs them (`postgres-init.sh`); `compose.yaml:24` does the same with `REDIS_PASSWORD` for the healthcheck. Visible to anyone
  who can run `docker inspect` on the host. `restart: unless-stopped` brings the stack back after a reboot.
- The contrib collector image carries many receivers/exporters the single `otlp` -> `debug` pipeline (`infra/otel/collector.yaml:20-30`) never uses.

### IS-06 LOW - no automated vulnerability or update gate in CI

- `.github/workflows/ci.yml` has no `govulncheck` step and no Dependabot/Renovate configuration exists (`.github` holds only `workflows/ci.yml`).
  `npm audit --omit=dev --audit-level=high` (`ci.yml:42`) runs last, after about two hours of work, only for production dependencies (7 packages),
  and is skipped whenever an earlier step fails. SHA-pinned Actions (held, see table) never update without automation.
- Concrete effect: IS-01 and IS-02 were visible on public advisory feeds for two weeks and nothing in the gates could have noticed.
- Cosmetic: `ci.yml:16,20` comment the pins `# v5` and `# v4`; the SHAs are the exact tags `v5.5.0` and `v4.4.0` (verified, see table).

### IS-07 LOW - Node version pin drift between `.nvmrc`/CI and the bootstrap check

- `.nvmrc:1` and `ci.yml:22` pin `24.19.0`; `scripts/bootstrap.sh:18` accepts any Node 24 (`... = 24`) and `apps/web/package.json:6-8` allows
  `>=24 <25`. The audit host runs 24.22.0, so the documented gates and CI are not the same runtime. `make setup` also downloads no Node (only
  Go is fetched), so Node is trusted from the host.
- Related: `apps/web/playwright.config.ts:17` and `playwright.signin.config.ts:5` use `/usr/bin/chromium` whenever it exists, and
  `scripts/browser-install.mjs:6` skips the Playwright browser download in that case, so browser results depend on an unpinned system
  browser. The CI step `ci.yml:31-35` installs the pinned Playwright Chromium, which is unused if the runner image also has `/usr/bin/chromium`
  (not verified: NOT RUN, no runner access).

### IS-08 LOW - `make build` is not reproducible across checkouts and produces dynamically linked binaries

- `Makefile:69-79` builds with default flags. Executed: `gt build -o a ./cmd/release-check` twice from the same worktree gives identical
  SHA-256 (same path), but `go version -m` shows no `-trimpath`, `CGO_ENABLED=1`, and `vcs.revision`/`vcs.time` embedded; the binary contains the
  worktree path 53 times and the module-cache path 1079 times, so a second checkout path or GOPATH changes every artifact hash.
  `file`/`ldd`: "dynamically linked ... libc.so.6".
- `internal/erp/types.go:9` embeds tzdata "on minimal production images", but a glibc-linked binary does not run on `scratch`/distroless-static.
- Fix direction: `-trimpath`, `CGO_ENABLED=0` (if no cgo package is required) and `-buildvcs=false` or a fixed `SOURCE_DATE_EPOCH` policy.

### IS-09 LOW - no project licence and no third-party notices; Apache-2.0 / BSD / MPL obligations are not materialised

- `git ls-files` finds no `LICENSE`, `NOTICE`, `COPYING` or third-party list except `internal/runtime/rules/tzdata/LICENSE`. From my
  knowledge of the upstream projects (not machine-checked): direct Go dependencies are Apache-2.0 (go-oidc, OpenTelemetry, grpc,
  jsonschema, go.yaml.in/yaml), MIT (pgx, goose), BSD-2 (go-redis), BSD-3 (x/oauth2, protobuf, uuid). Binary redistribution of any of them needs
  their licence texts, and Apache-2.0 additionally their NOTICE text. Frontend (read from `apps/web/package-lock.json` `license` fields):
  the 7 production packages are MIT; the shipped bundle's obligations are the MIT notices of React, React DOM and React Router. Dev-only:
  `lightningcss*` MPL-2.0 (12 entries; file-level copyleft, build tool, not shipped), `caniuse-lite` CC-BY-4.0, `mdn-data` CC0-1.0.
- `AGENTS.md:45` says "Never commit ... binaries"; the one committed binary is `internal/runtime/rules/tzdata/zoneinfo.zip`, documented as an
  intentional exception with its hash (`internal/runtime/rules/README.md:43`), and byte-identical to Go 1.27.2's `lib/time/zoneinfo.zip`
  (SHA-256 prefix `b2d18a7c8fa81420` on both): held.

### IS-10 LOW - Redis cache user and the Postgres role-drift check are broader than they need to be

- `scripts/m8-env.mjs:23`: `user default ... ~framework:cache:* -@all +ping +get +set +del +expire +ttl`. The only code that connects with this
  credential is `internal/platform/cache/cache.go:26-32` (`Ping`, `Close`). Five of six granted commands are unused. They are confined to the
  `framework:cache:*` prefix, so the exposure is small.
- `infra/compose/v3-role-bootstrap.sql:10` rejects a runtime role that is a member of a named deny-list (the project roles,
  `pg_read_all_data`, `pg_write_all_data`, and any role with a dangerous attribute). A membership of `pg_execute_server_program`,
  `pg_read_server_files`, `pg_write_server_files` or `pg_signal_backend` is not named and passes. An allow-list (the runtime role has no
  `pg_auth_members` rows at all) would close it. This is drift detection against an operator action, not a remote vector. Static reading only;
  no database was available (NOT RUN).

## What held (executed attack or inspection)

| # | Check | Result | Executed attack / evidence |
|---|---|---|---|
| 1 | Go toolchain download is pinned, hashed, TLS-only | PASS | `scripts/bootstrap.sh:10` `--proto '=https' --tlsv1.2 --fail --location`; executed against a local HTTPS server that answers 302 to a plain-HTTP URL: with these flags curl exits 1 (`Protocol "http" not supported or disabled`) and the HTTP listener is never hit, while the same request without `--proto` follows it; `:11` `sha256sum -c` before `tar`; compared with `go.dev/dl/?mode=json`: the pinned SHA-256 equals the published one for `go1.27.2.linux-amd64.tar.gz` (stable). Written to `.local/toolchains` (repo-local, `mktemp` 0600, trap removes), run as the invoking user, no sudo. (Version currency: IS-01.) |
| 2 | npm install path | PASS | `bootstrap.sh:19` `npm ci --ignore-scripts`; lockfile v3, 264 entries, all `https://registry.npmjs.org`, all have `integrity`; no git/file/http dependency, no link; only `fsevents@2.3.3` has an install script (macOS optional, skipped by `--ignore-scripts`); `npm ci --dry-run --offline` of a scratch copy exits 0 (package.json and lock in sync); no `.npmrc` anywhere; dependencies pinned to exact versions (no `^`/`~`). |
| 3 | npm advisories | PASS (production) | `npm audit --omit=dev --audit-level=high` on the committed lock: "found 0 vulnerabilities". Full audit incl. dev: see Not run. |
| 4 | Go module integrity | PASS | `gt mod verify` (GOPROXY=off): "all modules verified"; `go list -m all` 168 modules (15 direct, 43 linked); no `replace`, `exclude`, `retract`, `toolchain` directive; no retracted or deprecated module; pseudo-versions in `go.mod` are only `pgservicefile` and the two `genproto` modules (standard for pgx and grpc). `go mod tidy -diff` could not be evaluated offline (NOT RUN, needs test-only modules from the network). |
| 5 | GitHub Actions pins | PASS | `git ls-remote` of the three upstream repositories: `11bd7190...` = `actions/checkout` tag `v4.2.2`; `d35c59ab...` = `actions/setup-go` tag `v5.5.0`; `49933ea5...` = `actions/setup-node` tag `v4.4.0`. All three `uses:` are full SHAs. `runs-on: ubuntu-24.04` (not `latest`). |
| 6 | CI permissions, triggers, secrets | PASS | `ci.yml:5-6` `permissions: contents: read` at workflow level; triggers `push` and `pull_request` only (no `pull_request_target`, no `workflow_run`); no `secrets.` or `${{ github.event.* }}` reaches a `run:` (only `github.workspace` in `env:` and `github.ref` in the concurrency group); no `upload-artifact`, no `actions/cache` and `setup-go` has `cache: false`, so there is no artifact to hold `.env` and no cache to poison. The `.env` the job uses is generated per run by `make env` (random). |
| 7 | Compose port exposure | PASS | `docker compose --env-file <dummy> -f infra/compose/compose.yaml config`: all five published ports have `host_ip: 127.0.0.1` (5432, 6379, 4318, 13133). In-container `0.0.0.0` binds (`compose.yaml:28`, `collector.yaml:4,8`) are reachable only through those loopback publishes. Overlay `reference-performance.yaml` also rendered: adds limits only. |
| 8 | Compose defaults and passwords | PASS | every password is `${VAR:?run make env first}` (`compose.yaml:8-10,24`): `config` without the variable fails; no default password exists in any in-scope file. |
| 9 | Healthchecks | PASS | present on postgres (`compose.yaml:15-19`) and redis (`:31-35`); `infra-up` uses `up -d --wait --wait-timeout 90`. The collector has none in Compose; CI polls port 13133 (`ci.yml:38-39`). |
| 10 | Role bootstrap, passwords from env only | PASS | `infra/compose/postgres-init.sh:5-7` uses `psql --set` + `:'var'` literal quoting; roles `NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`; `REVOKE ALL ... FROM PUBLIC`; `v3-role-bootstrap.sql` creates `NOLOGIN` roles, raises on superuser/bypassrls/createdb/createrole/replication drift and on login for the owner/audit/maintenance roles; `scripts/m6r-roles.mjs:8` accepts only `^[a-f0-9]{64}$`, sends SQL on stdin (never argv), and `:13` scrubs the passwords from its log (written 0600). No application path uses `POSTGRES_PASSWORD`: `rg` finds it only in `infra/compose/compose.yaml`, `scripts/env.mjs`, the refusal list `internal/platform/app/operator_env.go` and tests. |
| 11 | No operator credential reaches a backend | PASS | `scripts/runtime-env.mjs:20-29` allow-lists per kind; `internal/platform/app/operator_env.go:11-14` refuses `MIGRATION_DATABASE_URL`, `MIGRATOR_PASSWORD`, `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `EVENT_OPERATOR_*` at start (IR-07 was in scope of other reviewers; here only read). |
| 12 | Redis ACL vs code needs (events, sign-in) | PASS | `internal/events/redis.go:68,76,92,104,124` use `XADD`, `XGROUP CREATE ... MKSTREAM`, `XAUTOCLAIM`, `XREADGROUP`, `XACK`, `PING`: exactly the dispatcher (`+ping +xadd`, prefix keys) and worker (`+ping +xgroup|create +xreadgroup +xautoclaim +xack`, one stream key) grants in `m8-env.mjs:23`. `internal/platform/cache/login.go:20-30,57,83` use `EVAL` with `GET/SET/TTL` inside, `GETDEL`: exactly the `framework_signin` grant (`+ping +get +set +ttl +eval +getdel`, `~framework:signin:v1:*`), the same string as `scripts/signin-roles.mjs:14`. `ReadyLoginFlows` (`login.go:77-80`) fails closed if the sign-in user can `GET framework:cache:*` or `ACL CAT`. Passwords are stored as `#<sha256>`; none in argv (`signin-roles.mjs` pipes the hash on stdin and passes the operator password through `REDISCLI_AUTH`). Over-grant of `default`: IS-10. |
| 13 | `.env` generation | PASS (fresh) | `scripts/env.mjs:8,25`: `crypto.randomBytes(32)` hex per secret, `wx`, mode 0600, refuses to overwrite; `m8-env.mjs:17-18` rejects reused or duplicate credentials. `.gitignore` ignores `.env`, `.env.*` (not `.env.example`), `.local/`, `bin/`, `node_modules/`. Pre-existing file: IS-03. |
| 14 | Committed secrets, all refs | PASS | `git log --all --no-merges -p -U0` over the refs of this clone (88 at the first scan: 56 branches, 28 remote-tracking, 4 tags; the set grows while other agents commit): 455 commits, 206,578 added lines, scanned by my own pattern classes (PEM private-key headers, `ghp_`/`github_pat_`, `AKIA`/`ASIA`, `xox*`, `AIza`, Stripe live keys, `sk-`/`sk-ant-`, JWTs, DSNs with a password, `Bearer`, `_authToken`, password/secret/api-key literals, 64-hex assigned to key-like names). No `.env`, `*.pem`, `*.key`, `*.p12`, `.npmrc`, `.netrc`, ACL file or database dump was ever added (942 distinct paths ever added listed and filtered by name and extension; only `zoneinfo.zip` is binary). 76 pattern hits, all synthetic: `internal/ai/redact_test.go:10-30,79,103`, `internal/ai/context_test.go:221-228`, `internal/ai/corpus/corpus.go:424-435` (redactor corpus; documented example keys such as the vendor-published `...EXAMPLE` AWS pair, sequential-alphabet GitHub token, 27-character PEM header stub), `internal/ai/provider_http_test.go:325`, `internal/tools/fixtures_test.go:8` (test key, `_test.go`), `internal/platform/app/operator_env_test.go:50`, `internal/config/*_test.go`, `scripts/runtime-env.test.mjs`, `apps/web/src/auth/*.test.*`, plus template lines `scripts/env.mjs:18-19`, `scripts/m6r-env.mjs:17`, `scripts/m8-env.mjs:14`, `infra/compose/postgres-init.sh:5-7` (variable interpolation, not values). No hit has a real-token shape (every literal is low-variety, a documented example, or contains `example`/`test`/`fake` context). Values were never printed; only file, line, class, length and alphabet. |
| 15 | Git objects | PASS | largest blobs: `zoneinfo.zip` 408 KB and the AI corpus JSON 357 KB; no archive, dump or key store. |
| 16 | TLS verification never disabled in tooling | PASS | `rg` for `GOFLAGS`, `GONOSUMDB`, `GOINSECURE`, `GOPROXY`, `NODE_TLS_REJECT_UNAUTHORIZED`, `--insecure`, `curl -k`, `strict-ssl` in `scripts`, `Makefile`, `.github`, `infra`, web configs: no match. `InsecureSkipVerify: true` appears nowhere in non-test Go (only read in production validators `config.go:96,186,312,320`, `login.go:21`). No `curl | sh`, no `sudo`, no `chmod 777`. Production config demands HTTPS issuer, `sslmode=verify-full`, `rediss` with verified TLS, HTTPS CORS and telemetry (`config.go:96,141,171-187,201,211,296-320`). |
| 17 | Dev servers bind loopback | PASS | `vite.config.ts:16` `host: '127.0.0.1'`, `package.json` `vite --host 127.0.0.1`, `e2e/oidc-provider.ts:67` `listen(8090, "127.0.0.1")`, default `HTTP_ADDR=127.0.0.1:8080`. |
| 18 | Script injection surface | PASS | `scripts/with-env.mjs:16` uses `spawn` without a shell and parses dotenv as data (`:10`); `smoke.mjs`/`dev.mjs` spawn fixed binaries; Makefile has no user-interpolated shell besides `RULE_FUZZ_TIME ?= 10s`. |
| 19 | Docs checksum gate | PASS (read) | `scripts/verify-docs.mjs` hashes 84 + 43 documents against `SHA256SUMS.md`; not re-run (docs-only, outside this scope). |
| 20 | Redis image carries the Lua fix | PASS (knowledge) | `redis:8.2.3` is above 8.2.2, the release that fixed the October 2025 Lua use-after-free (CVE-2025-49844, "RediShell"), relevant because `framework_signin` holds `+eval`. From my knowledge of the advisory; not machine-checked. |

## Not run

| Item | Reason |
|---|---|
| govulncheck (symbol-level) | `go install golang.org/x/vuln/cmd/govulncheck@latest` was queued behind the shared heavy-run lock; see the final line of this section for its outcome. The module-level matching in IS-02 and the std record in IS-01 replace it only partly. |
| `go mod tidy -diff` | needs modules (testify, sqlite, ...) that are not in the module cache; `GOPROXY=off` fails ("module lookup disabled"). |
| Full `npm audit` incl. dev dependencies | first attempt hit my 90 s timeout through the proxy; CI audits production only (IS-06). |
| `docker inspect` / image digests / running as which user (other than the process listing above) | `docker` other than `compose config` is forbidden for this task. |
| Actions behaviour on a GitHub runner (fork PR token scope, `/usr/bin/chromium` presence, setup-go download integrity) | no runner access; statements about them are from reading `ci.yml` only. |
| PostgreSQL role bootstrap executed against a database | the brief gives no database stack; static reading only (IS-10). Database function and grant correctness belongs to the SQL-catalog audit. |
| Remote-only refs | only the 51 refs present in this clone were scanned; `refs/pull/*` and branches not fetched are not covered. |
| Licence texts of transitive Go modules | from knowledge only (IS-09). |
