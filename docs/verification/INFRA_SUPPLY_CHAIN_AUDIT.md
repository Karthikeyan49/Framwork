# Infrastructure, CI, bootstrap and supply-chain audit (IS-01 ...)

Pinned commit `11a4ee40a12e0301472b275203ac10d8347eaad9`, branch `audit/infra-supply-chain`.
Scope: `.github/**`, `Makefile`, `scripts/**`, `infra/**`, `go.mod`/`go.sum`, `apps/web/package.json` and lockfile, `.env.example`,
`.gitignore`, Dockerfiles (none exist: `git ls-files` finds no Dockerfile). No database stack was used. Static analysis first;
the only network use was read-only (go.dev, vuln.go.dev, registry.npmjs.org, `git ls-remote` of three public Action repositories).
Report-only: no product file was changed. Probe files live in the session scratch directory, outside the worktree.

## Progress (kept current)

| time (UTC) | state |
|---|---|
| 06:41 | static review of every in-scope file, history scan, Go/npm advisory matching done; IS-01..IS-10 pushed (`c342c59`) |
| 06:45 | IS-11 (`.gitignore`) and IS-12 (CI trigger) added after reading the live Actions runs; lockfile-injection check; pushed (`ae5ae9f`) |
| 06:56 | executed Redis ACL matrix (36 commands), linked-binary reachability of the x/net advisories, full `npm audit`, history re-scan; IS-02/IS-06 revised |
| pending | `govulncheck` itself is still queued behind another agent's run of the shared heavy-run lock (started 06:36); NOT RUN unless it finishes before the final message |

## Verdict

**PASS WITH FINDINGS** for the infrastructure scope. No HIGH finding. No secret, key, `.env` or credential-bearing DSN exists
anywhere in the history of the refs of this clone (first scan: 88 refs, 455 non-merge commits, 206,578 added lines; re-scanned at the end, see row 14). The bootstrap download,
the Action pins, the npm lockfile and the Go module sums all verify. One MEDIUM (the pinned Go toolchain is one security patch
behind) and eleven LOW findings. The refs scanned grew from 88 to 89 while I worked (other agents commit to the same repository).

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

### IS-02 LOW - `golang.org/x/net v0.59.0` carries seven published advisories (fixed in 0.60.0 and 0.62.0); executed analysis says linked, not called

- `go.mod:44`. Method: the 43 modules linked into `./cmd/...` (`go list -deps`) were matched against every `vuln.go.dev` record of the 15 of them
  that appear in the index (75 records fetched, semver ranges evaluated). Only x/net matches: `GO-2026-6603`, `6610`, `6611`, `6612`, `6617`
  (x/net/http2, fixed 0.60.0) and `GO-2026-6860`, `6866` (x/net/html, fixed 0.62.0). Every other linked module (pgx, go-redis, grpc, protobuf,
  go-jose, OpenTelemetry, oauth2, go-oidc, goose) has no record that covers its pinned version.
- Reachability, executed: `gt build -o a ./cmd/api` then `go tool nm`: the binary holds 226 `x/net/http2` symbols, but none of the
  `Transport`, `ClientConn`, `Server`, `ConfigureServer`, `ConfigureTransport` entry points; of each record's listed symbols the linked binary contains
  0 (6610, 6860, 6866) or about 30 of 80-100 (6603, 6611, 6612, 6617; all `Framer`/frame/`hpack` helpers). `x/net/html` is not linked at all.
  `grpc.NewClient`, `grpc.Dial`, `grpc.NewServer` are absent from the binary and no repository package imports grpc (it arrives through the
  OpenTelemetry exporter configuration packages and generated OTLP stubs), so no code path of the product can open an HTTP/2 gRPC connection.
  The API's own HTTP/2 is the standard library's, which `1.27.2` already patched. Verdict: linked, not called. This is a method substitute for
  `govulncheck` (Not run), not a proof.
- Repro: `/home/user/bin/gt list -deps -f '{{with .Module}}{{.Path}}@{{.Version}}{{end}}' ./cmd/api | grep x/net`,
  `/home/user/bin/gt build -o /tmp/a ./cmd/api && /home/user/bin/gt tool nm /tmp/a | grep -c ' [Tt] golang.org/x/net/http2'`, and the record ids above at
  `https://vuln.go.dev/ID/<id>.json`. Fix direction: `go get golang.org/x/net@v0.62.0` (or later) with the Go bump of IS-01.

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
- Executed full audit (production and development, committed lock): 13 advisories, 0 critical, 2 high, 11 moderate, all in development tooling:
  `undici 8.11.2` (range 8.0.0 - 8.12.1; via `jsdom`; EventSource parser memory DoS and a subresource-integrity bypass), `zod 4.6.5` (< 4.6.6; via
  `eslint-plugin-react-hooks`; memory exhaustion on oversized collections), and `minimatch 10.2.6` (GHSA-g3hv-75wg-m249) with the nine ESLint /
  typescript-eslint packages that depend on it. The two highs have `fixAvailable: true`. None ships in the production bundle (production audit: 0), so
  the effect is on developer and CI machines only; the CI gate `--omit=dev` cannot show them.
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
  credential is `internal/platform/cache/cache.go:26-32` (`Ping`, `Close`). Five of six granted commands are unused (executed on a throwaway Redis: the `default` user can `SET framework:cache:k` and is
  denied everything else, see row 12). They are confined to the `framework:cache:*` prefix, so the exposure is small.
- `infra/compose/v3-role-bootstrap.sql:10` rejects a runtime role that is a member of a named deny-list (the project roles,
  `pg_read_all_data`, `pg_write_all_data`, and any role with a dangerous attribute). A membership of `pg_execute_server_program`,
  `pg_read_server_files`, `pg_write_server_files` or `pg_signal_backend` is not named and passes. An allow-list (the runtime role has no
  `pg_auth_members` rows at all) would close it. This is drift detection against an operator action, not a remote vector. Static reading only;
  no database was available (NOT RUN).

### IS-11 LOW - `.gitignore` protects `.env*` and `.local/` only; key, certificate and secret-directory names elsewhere are not ignored

- `.gitignore:1-9`. Executed `git check-ignore` on credential-shaped names: `.env.local`, `.env.production`, `.local/m8/users.acl` are ignored;
  `private.pem`, `key.pem`, `tls.key`, `ca.crt`, `.npmrc`, `id_rsa`, `users.acl`, `jwks.json`, `secrets/crm_live`, `tool-secrets/crm_live` are
  NOT ignored. The product reads such files from operator-chosen paths (`AUTH_JWKS_FILE`, `TOOL_SECRETS_DIR`, `TOOL_CA_FILE`;
  `internal/tools/secrets.go:78` enforces 0600 but nothing stops a `git add -A`), and `AGENTS.md:45` forbids committing private keys.
- There is no pre-commit hook and no secret-scanning step in CI; the only barrier is the host's push protection. The history itself is clean (row 14).

### IS-12 LOW - CI runs the whole 36-minute stack on every push to every branch, and a later push cancels the earlier commit's run

- `.github/workflows/ci.yml:2-4` (`push:` with no `branches`/`paths`, plus `pull_request:`), `:7-9` (`cancel-in-progress: true` per ref).
- Observed through read-only `gh run list`: my first report push produced run `41591477368` ("Framework checks", `in_progress`) within a
  minute, next to five more simultaneous runs from other audit/review branches; of the last 30 completed runs 19 are `cancelled`, 9 `success`,
  2 `failure` (the most recent failures are two pushes to `review/integration-final`, step `make test`); the integration branch runs last 28.5-36.3 min each. A documentation-only push costs the same as a code push,
  and a code commit followed within minutes by a documentation commit is cancelled and never gets its own verdict
  (e.g. `80a582f` on `integration/m9r-m14`: cancelled after 4.1 min). A pull request update also runs twice (the `push` run and the
  `pull_request` run use different concurrency groups).
- Positive evidence for the pinned commit: run `40834565648` on `11a4ee4` is `success` (36.1 min); the previous code-head runs `db9c2fa` and `0e13abe` are `success`.
- Fix direction: `branches: [main, integration/**]` for `push`, `paths-ignore: ['docs/**', '**/*.md']` or a cheap docs-only job, and a
  separate group for pull requests; never `cancel-in-progress` on the integration branch.

## What held (executed attack or inspection)

| # | Check | Result | Executed attack / evidence |
|---|---|---|---|
| 1 | Go toolchain download is pinned, hashed, TLS-only | PASS | `scripts/bootstrap.sh:10` `--proto '=https' --tlsv1.2 --fail --location`; executed against a local HTTPS server that answers 302 to a plain-HTTP URL: with these flags curl exits 1 (`Protocol "http" not supported or disabled`) and the HTTP listener is never hit, while the same request without `--proto` follows it; `:11` `sha256sum -c` before `tar`; compared with `go.dev/dl/?mode=json`: the pinned SHA-256 equals the published one for `go1.27.2.linux-amd64.tar.gz` (stable). Written to `.local/toolchains` (repo-local, `mktemp` 0600, trap removes), run as the invoking user, no sudo. (Version currency: IS-01.) |
| 2 | npm install path | PASS | `bootstrap.sh:19` `npm ci --ignore-scripts`; lockfile v3, 264 entries, all `https://registry.npmjs.org`, all have `integrity`; no git/file/http dependency, no link; only `fsevents@2.3.3` has an install script (macOS optional, skipped by `--ignore-scripts`); `npm ci --dry-run --offline` of a scratch copy exits 0 (package.json and lock in sync); no `.npmrc` anywhere; dependencies pinned to exact versions (no `^`/`~`). |
| 3 | npm advisories | PASS (production), see IS-06 (development) | `npm audit --omit=dev --audit-level=high` on a scratch copy of the committed lock: "found 0 vulnerabilities". Full `npm audit --json`: 13 advisories, all development tooling (IS-06). |
| 4 | Go module integrity | PASS | `gt mod verify` (GOPROXY=off): "all modules verified"; `go list -m all` 168 modules (15 direct, 43 linked); no `replace`, `exclude`, `retract`, `toolchain` directive; no retracted or deprecated module; pseudo-versions in `go.mod` are only `pgservicefile` and the two `genproto` modules (standard for pgx and grpc). `go mod tidy -diff` could not be evaluated offline (NOT RUN, needs test-only modules from the network). |
| 5 | GitHub Actions pins | PASS | `git ls-remote` of the three upstream repositories: `11bd7190...` = `actions/checkout` tag `v4.2.2`; `d35c59ab...` = `actions/setup-go` tag `v5.5.0`; `49933ea5...` = `actions/setup-node` tag `v4.4.0`. All three `uses:` are full SHAs. `runs-on: ubuntu-24.04` (not `latest`). |
| 6 | CI permissions, triggers, secrets | PASS | `ci.yml:5-6` `permissions: contents: read` at workflow level; triggers `push` and `pull_request` only (no `pull_request_target`, no `workflow_run`); no `secrets.` or `${{ github.event.* }}` reaches a `run:` (only `github.workspace` in `env:` and `github.ref` in the concurrency group); no `upload-artifact`, no `actions/cache` and `setup-go` has `cache: false`, so there is no artifact to hold `.env` and no cache to poison. The `.env` the job uses is generated per run by `make env` (random). |
| 7 | Compose port exposure | PASS | `docker compose --env-file <dummy> -f infra/compose/compose.yaml config`: all five published ports have `host_ip: 127.0.0.1` (5432, 6379, 4318, 13133). In-container `0.0.0.0` binds (`compose.yaml:28`, `collector.yaml:4,8`) are reachable only through those loopback publishes. Overlay `reference-performance.yaml` also rendered: adds limits only. |
| 8 | Compose defaults and passwords | PASS | every password is `${VAR:?run make env first}` (`compose.yaml:8-10,24`): `config` without the variable fails; no default password exists in any in-scope file. |
| 9 | Healthchecks | PASS | present on postgres (`compose.yaml:15-19`) and redis (`:31-35`); `infra-up` uses `up -d --wait --wait-timeout 90`. The collector has none in Compose; CI polls port 13133 (`ci.yml:38-39`). |
| 10 | Role bootstrap, passwords from env only | PASS | `infra/compose/postgres-init.sh:5-7` uses `psql --set` + `:'var'` literal quoting; roles `NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`; `REVOKE ALL ... FROM PUBLIC`; `v3-role-bootstrap.sql` creates `NOLOGIN` roles, raises on superuser/bypassrls/createdb/createrole/replication drift and on login for the owner/audit/maintenance roles; `scripts/m6r-roles.mjs:8` accepts only `^[a-f0-9]{64}$`, sends SQL on stdin (never argv), and `:13` scrubs the passwords from its log (written 0600). No application path uses `POSTGRES_PASSWORD`: `rg` finds it only in `infra/compose/compose.yaml`, `scripts/env.mjs`, the refusal list `internal/platform/app/operator_env.go` and tests. |
| 11 | No operator credential reaches a backend | PASS | `scripts/runtime-env.mjs:20-29` allow-lists per kind; `internal/platform/app/operator_env.go:11-14` refuses `MIGRATION_DATABASE_URL`, `MIGRATOR_PASSWORD`, `POSTGRES_PASSWORD`, `REDIS_PASSWORD`, `EVENT_OPERATOR_*` at start (IR-07 was in scope of other reviewers; here only read). |
| 12 | Redis ACL vs code needs (events, sign-in) | PASS | `internal/events/redis.go:68,76,92,104,124` use `XADD`, `XGROUP CREATE ... MKSTREAM`, `XAUTOCLAIM`, `XREADGROUP`, `XACK`, `PING`: exactly the dispatcher (`+ping +xadd`, prefix keys) and worker (`+ping +xgroup|create +xreadgroup +xautoclaim +xack`, one stream key) grants in `m8-env.mjs:23`. `internal/platform/cache/login.go:20-30,57,83` use `EVAL` with `GET/SET/TTL` inside, `GETDEL`: exactly the `framework_signin` grant (`+ping +get +set +ttl +eval +getdel`, `~framework:signin:v1:*`), the same string as `scripts/signin-roles.mjs:14`. `ReadyLoginFlows` (`login.go:77-80`) fails closed if the sign-in user can `GET framework:cache:*` or `ACL CAT`. Passwords are stored as `#<sha256>`; none in argv (`signin-roles.mjs` pipes the hash on stdin and passes the operator password through `REDISCLI_AUTH`). Over-grant of `default`: IS-10. **Executed**: the ACL file written by the repo's own `scripts/m8-env.mjs` (random run-time passwords, scratch directory) was loaded into a throwaway `redis-server` (host binary 7.0.15, 127.0.0.1:16399, stopped afterwards; the Compose image 8.2.3 was not run) and 36 commands were issued per user with `redis-cli --user`: 36 of 36 as expected. Allowed: `default` PING and SET/GET under `framework:cache:`; `framework_signin` the saveLogin-shaped EVAL and GETDEL on `framework:signin:v1:*`; `event_dispatcher` `XADD ... MAXLEN ~ 10000`; `event_worker` `XGROUP CREATE ... MKSTREAM`, `XREADGROUP`, `XAUTOCLAIM`, `XACK` on its own stream. Denied (NOPERM, or Redis's script-level "user executing the script can't access/run"): `default` outside its prefix, EVAL, KEYS, XADD, ACL LIST, CONFIG, FLUSHALL; `framework_signin` GET on `framework:cache:*`, `ACL CAT`, SCRIPT/FUNCTION LOAD, XADD, and from inside Lua a cache-key read, FLUSHALL and CONFIG; `event_dispatcher` XREADGROUP, XLEN, XDEL, GET, writes outside the prefix; `event_worker` XADD, any other stream, XGROUP DESTROY/SETID, XDEL, XINFO. The operator is a full administrator by design. |
| 13 | `.env` generation | PASS (fresh) | `scripts/env.mjs:8,25`: `crypto.randomBytes(32)` hex per secret, `wx`, mode 0600, refuses to overwrite; `m8-env.mjs:17-18` rejects reused or duplicate credentials. `.gitignore` ignores `.env`, `.env.*` (not `.env.example`), `.local/`, `bin/`, `node_modules/`. Pre-existing file: IS-03. |
| 14 | Committed secrets, all refs | PASS | `git log --all --no-merges -p -U0` over the refs of this clone (88 at the first scan, 89 at the re-scan: about 56 branches, 28 remote-tracking, 4 tags; the set grows while other agents commit): re-scan 06:50 = 534 commits of which 484 non-merge, 209,234 added lines; first scan 455 non-merge, 206,578 lines, scanned by my own pattern classes (PEM private-key headers, `ghp_`/`github_pat_`, `AKIA`/`ASIA`, `xox*`, `AIza`, Stripe live keys, `sk-`/`sk-ant-`, JWTs, DSNs with a password, `Bearer`, `_authToken`, password/secret/api-key literals, 64-hex assigned to key-like names). No `.env`, `*.pem`, `*.key`, `*.p12`, `.npmrc`, `.netrc`, ACL file or database dump was ever added (942 distinct paths ever added listed and filtered by name and extension; only `zoneinfo.zip` is binary). 79 pattern hits at the re-scan (76 at the first), all synthetic or placeholder: `internal/ai/redact_test.go:10-30,79,103`, `internal/ai/context_test.go:221-228`, `internal/ai/corpus/corpus.go:424-435` (redactor corpus; documented example keys such as the vendor-published `...EXAMPLE` AWS pair, sequential-alphabet GitHub token, 27-character PEM header stub), `internal/ai/provider_http_test.go:325`, `internal/tools/fixtures_test.go:8` (test key, `_test.go`), `internal/platform/app/operator_env_test.go:50`, `internal/config/*_test.go`, `scripts/runtime-env.test.mjs`, `apps/web/src/auth/*.test.*`, plus three DSN-shaped lines in another agent's in-progress report on `review/redaction-redteam` (`docs/verification/REDACTION_REDTEAM.md:151,602,603`; 3-character passwords such as `pwd`), `docs/verification/GATEWAY_INDEPENDENT_REVIEW.md:117` (a 26-character placeholder after `Bearer`), plus template lines `scripts/env.mjs:18-19`, `scripts/m6r-env.mjs:17`, `scripts/m8-env.mjs:14`, `infra/compose/postgres-init.sh:5-7` (variable interpolation, not values). No hit has a real-token shape (every literal is low-variety, a documented example, or contains `example`/`test`/`fake` context). Values were never printed; only file, line, class, length and alphabet. |
| 15 | Git objects | PASS | largest blobs: `zoneinfo.zip` 408 KB and the AI corpus JSON 357 KB; no archive, dump or key store. |
| 16 | TLS verification never disabled in tooling | PASS | `rg` for `GOFLAGS`, `GONOSUMDB`, `GOINSECURE`, `GOPROXY`, `NODE_TLS_REJECT_UNAUTHORIZED`, `--insecure`, `curl -k`, `strict-ssl` in `scripts`, `Makefile`, `.github`, `infra`, web configs: no match. `InsecureSkipVerify: true` appears nowhere in non-test Go (only read in production validators `config.go:96,186,312,320`, `login.go:21`). No `curl | sh`, no `sudo`, no `chmod 777`. Production config demands HTTPS issuer, `sslmode=verify-full`, `rediss` with verified TLS, HTTPS CORS and telemetry (`config.go:96,141,171-187,201,211,296-320`). |
| 17 | Dev servers bind loopback | PASS | `vite.config.ts:16` `host: '127.0.0.1'`, `package.json` `vite --host 127.0.0.1`, `e2e/oidc-provider.ts:67` `listen(8090, "127.0.0.1")`, default `HTTP_ADDR=127.0.0.1:8080`. |
| 18 | Script injection surface | PASS | `scripts/with-env.mjs:16` uses `spawn` without a shell and parses dotenv as data (`:10`); `smoke.mjs`/`dev.mjs` spawn fixed binaries; Makefile has no user-interpolated shell besides `RULE_FUZZ_TIME ?= 10s`. |
| 19 | Docs checksum gate | PASS (read) | `scripts/verify-docs.mjs` hashes 84 + 43 documents against `SHA256SUMS.md`; not re-run (docs-only, outside this scope). |
| 20 | Redis image carries the Lua fix | PASS (knowledge) | `redis:8.2.3` is above 8.2.2, the release that fixed the October 2025 Lua use-after-free (CVE-2025-49844, "RediShell"), relevant because `framework_signin` holds `+eval`. From my knowledge of the advisory; not machine-checked. |
| 21 | npm lockfile injection | PASS | for all 264 lock entries the `resolved` URL equals `https://registry.npmjs.org/<name>/-/<basename>-<version>.tgz` (0 mismatches), every `integrity` is `sha512`, root specs equal `package.json` (0 drift), no semver range in `package.json`. |
| 22 | CI observed at the pinned commit; workflow history | PASS | `gh run list --branch integration/m9r-m14` (read-only): `11a4ee4` success 36.1 min, `db9c2fa` success, `0e13abe` success. `git log --all -p -- .github` (9 commits): `ci.yml` is the only workflow file on any ref; no `pull_request_target`, `workflow_run`, `self-hosted`, `secrets.`, `id-token`, `write-all`, `actions/cache`, `upload-artifact` or unpinned `uses:` was ever committed. |

## Not run

| Item | Reason |
|---|---|
| govulncheck (symbol-level) | `go install golang.org/x/vuln/cmd/govulncheck@latest` under `hv` has waited for the shared heavy-run lock since 06:36 (another agent's run holds it); never started. Substitutes (executed): the module/version matching against `vuln.go.dev` and the linked-symbol analysis in IS-02, and the standard-library record in IS-01. Partial only: no call-graph analysis of the repository's own code. |
| `go mod tidy -diff` | needs modules (testify, sqlite, ...) that are not in the module cache; `GOPROXY=off` fails ("module lookup disabled"). |
| `docker inspect` / image digests / running as which user (other than the process listing above) | `docker` other than `compose config` is forbidden for this task. |
| Actions behaviour on a GitHub runner (fork PR token scope, `/usr/bin/chromium` presence, setup-go download integrity) | no runner access; statements about them are from reading `ci.yml` only. |
| PostgreSQL role bootstrap executed against a database | the brief gives no database stack; static reading only (IS-10). Database function and grant correctness belongs to the SQL-catalog audit. |
| Remote-only refs | only the 51 refs present in this clone were scanned; `refs/pull/*` and branches not fetched are not covered. |
| Licence texts of transitive Go modules | from knowledge only (IS-09). |
