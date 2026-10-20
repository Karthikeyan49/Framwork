# Merged-tree re-baseline — remediation slice A

20 October 2026 (Asia/Kolkata). Product tree under test:
`552fa9efe2092a592e39974ee23ec1ecbc057729`.

**Slice A execution: COMPLETE. Combined gate: FAIL. M9R STATUS: PARTIAL.**
This document covers Step 0 and slice A of the user's integration instruction.
It neither accepts M10–M14 for production nor completes the remaining R7 work.
Independent review is NOT RUN, as explicitly deferred by the user while agents
are unavailable until 22 October, 21:00 UTC.

## Git checkpoint and scope

The clean checkout started on `remediation/m9r-foundation` at
`b00da36ca246bf52dcbf26b939ab627792a3abb3`. The configured origin fetch refspec
tracks only main, so a normal fetch did not create the integration tracking ref.
The explicit fetch below supplied it. The ancestor check and fast-forward both
succeeded; there was no reset, rebase, conflict resolution or history rewrite.
The authorized fast-forward has been pushed to `remediation/m9r-foundation`.

```sh
git fetch origin
git fetch origin refs/heads/integration/m9r-m14:refs/remotes/origin/integration/m9r-m14
git merge-base --is-ancestor HEAD origin/integration/m9r-m14
git checkout remediation/m9r-foundation
git merge --ff-only --quiet origin/integration/m9r-m14
git push origin remediation/m9r-foundation
git ls-remote origin refs/heads/remediation/m9r-foundation refs/heads/integration/m9r-m14 refs/heads/main
```

After that push both integration and remediation pointed to `552fa9e`; main
remained `9da3f5f40fcdbed53ebe5af694cf0a14c600cbe7`. No PR was created.
No application code, contracts, migration, dependency, shared integration file,
AGENTS.md or CLAUDE.md is changed by slice A. The required 23-file merge review
is slice B, and the plan/ledger reconciliation and proposed agent instructions
are slice C; neither is claimed here.

## Isolated environment and setup evidence

All new database work uses Compose project `framework-merged-20261020` with
its own `framework-merged-20261020_pgdata` volume and loopback ports PostgreSQL
15432, Redis 16379, OTLP 14318 and collector health 14133. The existing
`framework-m0` project, its data and credentials are retained. No shared or
customer database is migrated, reset or deleted.

Ignored harness and detailed command logs are in
`.local/merged-rebaseline-20261020/`. Credentials were generated randomly in a
0600 environment file. The unmodified `scripts/m8-env.mjs` was executed with
that isolated directory as its working directory; it generated separate cache,
sender, receiver, operator and `framework_signin` ACL identities. Only their
local port bindings were changed to 16379. No credentials are included here.

The harness `run.mjs` injects that environment before running a command, prevents
fallback to unrelated default-stack bindings, and exports
`COMPOSE_PROJECT_NAME=framework-merged-20261020`. Every gate below runs through
this prefix from `/workspace/Framwork`:

```sh
node .local/merged-rebaseline-20261020/run.mjs <command and arguments>
```

The Redis stream-loss test's existing hard-coded Compose restart resolves to
this owned Redis, as checked by comparing its Compose container ID with the
isolated container's ID before running the suite. No test is omitted for that
restart. No root `.env` or ACL replacement is needed.

Performance probes hard-code `framework-m0-postgres-1`. An ignored PATH wrapper
maps only their two exact operations (resource-limit inspect and read-only
cluster-identity query) to `framework-merged-20261020-postgres-1`. All other
Docker operations are forwarded unchanged. The tests still compare the actual
connected PostgreSQL system identifier and verify the actual image, CPU/memory/
swap limits and planner settings. This is a container-name substitution, not a
bypass of the reference checks. The wrapper's real Docker path is
`/usr/local/bin/docker` on this host.

Provisioning used the merged Compose files and an isolated overlay:

```sh
node .local/merged-rebaseline-20261020/run.mjs docker compose --env-file .local/merged-rebaseline-20261020/.env -f infra/compose/compose.yaml -f infra/compose/reference-performance.yaml -f .local/merged-rebaseline-20261020/compose.yaml up -d --wait --wait-timeout 90
node .local/merged-rebaseline-20261020/run.mjs node .local/merged-rebaseline-20261020/roles.mjs
node .local/merged-rebaseline-20261020/run.mjs node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate up
node .local/merged-rebaseline-20261020/run.mjs node scripts/with-env.mjs ./scripts/go.sh run ./cmd/migrate status
```

Initial setup failures are retained rather than called passes:

- First wrapper launch failed because `/usr/bin/docker` does not exist here;
  corrected only the ignored harness to `/usr/local/bin/docker`.
- Restored public `infra/compose/postgres-init.sh` had mode 0700. PostgreSQL's
  entrypoint could not read it, restarted, and became healthy without creating
  app/migrator roles. The first role bootstrap and migration up/status failed.
  Executed the unchanged initialization script via stdin only on the newly
  created isolated container, then reran role bootstrap and migrations. A 0644
  isolated copy is mounted by the overlay for subsequent fresh provisioning.
  The original public file, private credentials and existing database were not
  changed. The collector likewise uses a readable copy of its public config.
- A read-only role-inspection shell command had a quoting error; it changed
  nothing. Correctly quoted subsequent role/probe commands were used.

Corrected migration up reports **67 applied**, covering 00001–00057 and
00080–00089; status confirms the final migration. A ledger count confirms 67.
`control.worker_grants_safe()` evaluated with session authorization
`framework_worker` returns true. An earlier probe as PostgreSQL superuser
returned false, as expected for a session-user privilege guard; it was not a
worker-role failure.

Tools: Node 24.19.0, Go 1.27.2, Docker Compose v2.40.3. Reference profile:
PostgreSQL 18.6, 4 CPU, 8 GiB memory, 8 GiB memory-plus-swap (swap disabled),
shared_buffers 2GB, effective_cache_size 6GB, work_mem 4MB,
random_page_cost 1.1. API and Docker share the cloud parent's four-CPU quota.
The existing default stack remains running on the same host, so absolute
latencies are observations of this host, not isolated hardware certification.

The retained legacy API is present and SHA-256 verified:
`03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19`.
The harness sets `M9R_LEGACY_API_BINARY` to that retained pre-extension artifact
and sets its verified hash; the current build never replaces it.

## Executed gates

All commands use the isolated harness prefix above. Wall time includes
compilation/startup; the integration row also shows Go's test execution time.

| Command | Result | Evidence |
|---|---|---|
| `make setup` | PASS | 4.925s; modules verified; 241 Node packages installed from lockfile |
| `make browser-install` | PASS | 0.070s; existing browser installation available |
| `make check` | PASS | 14.326s; source-kit checksums, format, vet, modules, ESLint and TypeScript |
| `make test-unit` | PASS | 161.323s; 27 Go race-test packages (7 cached), 9 script tests, 126 frontend tests in 19 files |
| `make build` | PASS | 12.789s; backend commands and frontend built |
| `make test-integration GOFLAGS=-v` | PASS | 1376.854s wall / 1366.644s Go; 439 top-level groups; zero failures/skips, including nested tests |
| `make test-e2e` | PASS | 22.675s wall; 11 browser tests in 22.0s |
| `make test-signin-e2e` | PASS | 9.243s wall; 7 local sign-in tests in 8.5s |
| Pinned API traffic diagnostic below | PASS | 17.799s wall / 6.920s Go; 40 requests: 20 HTTP 401, 20 HTTP 429, 20 denial audits, zero rejected-request audits |
| `make test-rules-fuzz` | PASS | 44.584s wall; 3 targets, 10s fuzz time each |
| `make test-events-fuzz` | PASS | 54.889s wall; 10s fuzz time plus cold compilation |
| `make test-workflows-fuzz` | PASS | 14.533s wall; 10s fuzz time |
| JSON/YAML fuzz commands below | PASS | 11.962s / 11.549s wall; 10s each |
| Compiler/query fuzz commands below | PASS | 13.656s / 13.677s wall; 10s each |

Actual Redis stream-loss/restart: PASS 3.15s. Both OIDC integration groups PASS
(1.41s and 0.02s). Retained-binary compatibility checks executed without their
opt-in skips. The 96-receipt/63-node worker-policy query took 1.198391s and its
EXPLAIN completed under the unchanged 2s statement budget. This passing run does
not diagnose or close the historical timeout/bad-connection findings.

All nine repository fuzz functions were discovered and exercised: RuleCompile,
RuleRestoreAndInputs, FixedDecimalMoney, Pointer, WorkflowRestore,
JSONInputIsBounded, YAMLInputIsBounded, CompileNeverPanics and
TypedFilterNeverEmitsCallerSQL (each has the `Fuzz` prefix). Make recipes cover
five; the four direct commands close the coverage omission. These bounded fuzz
runs are not exhaustive correctness claims.

Exact additional commands, each after the isolated harness prefix:

```sh
env R7A_REVIEW_API_BINARY=/workspace/Framwork/bin/api R7A_REVIEW_API_SHA256=a3928eff45dcfc285fd5d4365d6cc3469371c9822a51ef04164851069363c80e node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration,trafficreview -timeout=3m -count=1 -run '^TestM9RTrafficReviewPinnedBinaryInvalidBurst$' -v ./tests/integration
env GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzJSONInputIsBounded$' -fuzztime=10s -parallel=2 ./internal/metadata
env GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzYAMLInputIsBounded$' -fuzztime=10s -parallel=2 ./internal/metadata
env GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzCompileNeverPanics$' -fuzztime=10s -parallel=2 ./internal/compiler
env GOMAXPROCS=2 ./scripts/go.sh test -run '^$' -fuzz '^FuzzTypedFilterNeverEmitsCallerSQL$' -fuzztime=10s -parallel=2 ./internal/runtime/data
```

### Outage, reference capacity and driver diagnostic

All commands in this section also use the isolated harness prefix. The reference
target deliberately skips only its `env` prerequisite to preserve the default
stack's files. Its Compose override selects the owned project and volume:

```sh
make reference-up -o env 'COMPOSE=docker compose --env-file .local/merged-rebaseline-20261020/.env -f infra/compose/compose.yaml -f .local/merged-rebaseline-20261020/compose.yaml'
```

**PASS**, 5.151s. This recreates only the isolated PostgreSQL container, preserving
its own volume. Every capacity target subsequently verifies the actual connected
reference profile and cluster identity. No other acceptance workload ran in
parallel, no forced vacuum/GC or scan-method changes were introduced, and no
individual acceptance budget was relaxed.

| Command | Result | Wall seconds | Current evidence / prior recorded comparison |
|---|---|---:|---|
| `make test-event-outage` | PASS | 620.252 | Go 609.024s; actual absence 600.001 monotonic / 600.092 PostgreSQL seconds; recovery total 606.251s versus R7b 606.446s |
| `make test-performance` | PASS | 77.978 | 10-client HTTP p95 124.505ms / p99 144.944ms, 100 samples, zero errors; R7b p95 158.903ms |
| `make test-scalar-performance` | PASS | 86.399 | 12 cases, worst HTTP p95 385.771ms; R7a 186.845ms |
| `make test-choice-performance` | PASS | 92.253 | 12 cases, worst HTTP p95 271.953ms; R7a 178.285ms |
| `make test-native-performance` | PASS | 77.230 | 12 cases, worst HTTP p95 244.004ms; R7a 165.110ms |
| `make test-search-performance` | PASS | 43.826 | 8 cases, worst HTTP p95 184.812ms; R7a 112.226ms |
| `make test-write-performance` | PASS | 63.405 | 32 clients / 60s, 7,968 writes, p95 301.897ms / p99 331.019ms, 132.36/s, zero visible errors/lost updates; R7b 6,527 writes / p95 349.656ms |
| `make test-unique-performance` | PASS | 182.161 | Fresh keys p95 331.310ms, expected conflicts 479.989ms; 32 clients / 60s: 6,463 writes, p95 350.588ms / p99 410.996ms, 107.20/s; R7a 75.606ms / 72.443ms and 10,428 writes / p95 214.344ms |
| `make test-aggregate-performance` | **FAIL** | 661.245 | All 24 cases completed; worst SQL p95 309.775ms (limit 500), worst HTTP p95 822.729ms (limit 750); R7a 186.878ms / 505.454ms |
| Driver diagnostic command below | **FAIL** | 12.869 | Go 6.857s; child did not complete rollback/pool recovery within the supervised deadline; historical IR-R3d-01 remains open |

Read/search p95 limits remain 750ms; ordinary/unique writes 500ms; aggregate SQL
500ms and HTTP 750ms. Non-aggregate targets have zero unexpected request errors.
Expected unique conflicts are checked responses, not unexpected errors. The
unique-conflict p95 has only **20.011ms** of headroom: passing this bounded
sample is not evidence of spare production capacity.

The outage retained SENT/1 and broker PENDING/16 at 600s, with successor attempts
and receipts zero. After recovery it verified exactly three of each completed
delivery, inbox fact, local effect, audit and immutable intent, with 19 attempts
retained and two unconfirmed redeliveries. These are local consistency checks,
not a universal external exactly-once guarantee.

Aggregate details: 8 unmapped, 8 mapped-current and 8 mapped-retained cases
completed, each with 100 measured requests. Exact result oracles matched; there
were no unexpected request errors or SQL budget failures. The mapped fixture
copied and verified 100,000 records and checked source/target manifests before
activation. The three **latency acceptance failures** are:

| Branch | Case | HTTP p95 ms | HTTP p99 ms | Limit ms |
|---|---|---:|---:|---:|
| Mapped current | filtered_money | 755.156 | 789.144 | 750 |
| Mapped retained reader | sum_money | 822.729 | 884.181 | 750 |
| Mapped retained reader | filtered_money | 755.607 | 795.439 | 750 |

The unmapped test passes (108.83s); mapped/retained test fails (551.05s); Go
package reports FAIL 659.903s and Make exits 2. The worst breach is 72.729ms.
This failure is not relabelled as a pass because returned results were correct.
No rerun was used to replace the failing evidence and no performance fix is
claimed in this slice.

The separately executed diagnostic was:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -tags=integration,driverpanic -timeout=1m -count=1 -run '^TestIndependentR3dPanicDoesNotIssueRestorationSQLAndPoolRecovers$' -v ./tests/integration
```

It fails with the bounded diagnostic message that injected driver panic did not
complete rollback and pool recovery within the supervised deadline. This
reproduces the pre-existing driver/tracer limitation; the default connector has
no custom tracer. Passing ordinary callback-panic checks does not close it.

**Combined requested gate: FAIL.** All requested commands ran, but aggregate
latency and the driver diagnostic do not pass. Independent review is **NOT RUN**
by the user's explicit scheduling instruction. Production deployment/data-plane
verification, a real model provider, external IdP/MFA/HTTPS gateway, controlled
pre-merge A/B measurements, forced-generic-plan diagnostics and DR restore drills
are **NOT RUN** in this slice. They are not implied by the passing baseline tests.


## Historical comparison and attribution

The last R7b report records 304 integration groups in 884.394s, browsers 11/7
in 26.9s/10.4s, actual outage 609.388s, numeric HTTP p95 158.903ms and ordinary
write p95 349.656ms with 6,527 writes. Its other six capacity targets were NOT
rerun. The prior R7a all-eight run records numeric 75.786ms; scalar worst
186.845ms; choice 178.285ms; native 165.110ms; search 112.226ms; ordinary writes
13,334/p95 165.909ms; unique writes 10,428/p95 214.344ms; aggregate worst HTTP
505.454ms. See the R7a/R7b implementation reports for the full envelopes.

The integration plan's historical milestone-agent run was not a single green
full-suite run: its table records 436 passes, two OIDC failures and an excluded
Redis restart test, followed by passing OIDC reruns after ACL repair. It did not
exercise the legacy artifact, all-eight performance targets or driver panic.
Those omissions are why the current full re-baseline is necessary.

Differences between these runs alone cannot establish that merging caused a
performance regression: host load, fresh cluster contents and fixture execution
history differ. No pre-merge same-host controlled A/B is performed in this
slice. Observed changes and acceptance-budget failures are stated separately
from causal claims.

The aggregate latency breach is a newly observed failing gate relative to the
recorded R7a all-eight PASS. R7b did not rerun aggregates or the other five
non-numeric/non-ordinary-write targets, so these records cannot establish whether
the deterioration began before the merge. No merge-caused product regression is
causally demonstrated, and performance neutrality is not claimed. The historical
driver diagnostic remains reproducibly failing. Full integration elapsed time
is greater than R7b, but the workload also increased from 304 to 439 groups; that
is not a like-for-like performance comparison.

## Additional observations

No new ADR, dependency or product behavior is introduced. Historical adversarial
test cases were executed in the full suite, but this is not a new independent
review of the merged implementation.

Future migration numbering needs coordination: `cmd/migrate/main.go` creates a
Goose provider without out-of-order allowance, and the database is already at 89.
The reserved remediation numbers 58–79 would become new lower-numbered
migrations. Goose v3.28.0 defaults to rejecting that condition. This is static
evidence, not an executed future-upgrade failure. See the handoff; resolve it
before a new remediation migration, without renumbering pushed files or
silently enabling unrestricted out-of-order execution. Fresh and upgraded
installations would also apply a future 58 on opposite sides of 80–89, so shared
SQL redefinitions need an equivalence test; an allowance switch alone is not
a complete design.

## Recovery, ownership and next slices

No product migration or code fix is introduced here. There is nothing to roll
back in existing data. Keep the isolated stack and logs for diagnosis; do not
copy its migration ledger onto any existing database. Stop only owned services
when necessary. The separate MAIN database described in the integration plan
has colliding old milestone migration numbers and must not receive this
sequence without a separately approved recovery plan.

M10–M14 remain a development/test preview. Their production data plane, separate
verifier credential, real model provider and independent acceptance remain with
the milestone agent. Local R5 deployment preparation does not certify an
external IdP, MFA or production gateway. M9R/R7 remain incomplete.

Slice A stops after reporting the measured gate. B (merge inspection), C (docs),
then user-confirmed R7 slice ordering remain pending. No M15+ implementation,
main merge, integration-branch push or deployment is authorized by this slice.

Files created by A: this report and `docs/migration/AGENT_HANDOFFS.md`. SHARED
files edited by A: **none**. No new migration, ADR or dependency. Build outputs,
Node dependencies, private environment/ACL/key files and `.local` logs/harness
remain ignored and must not be committed. Final preservation checks confirmed
unchanged default-stack container IDs/start times/restart counts, unchanged root
environment/ACL bytes and unchanged retained legacy-binary hash. Final `make docs-verify` PASSes all
84 + 43 source-kit checksums; `git diff --cached --check` and `git diff --quiet`
PASS. The final default-database ledger still has 67 applied migrations and the
worker-session privilege guard still returns true. Only the two named Markdown
files are staged; generated password values were checked absent from them.

The product-source fast-forward is pushed. The documentation checkpoint is
committed and pushed separately on the same designated branch after reporting
these failures; its hash is given in the completion message. No main/integration
push, PR, history rewrite or branch deletion is performed.

## Harness source for review

These files remain ignored operational artifacts. The following copies disclose
the executed isolation behavior without disclosing environment values. Reuse
requires the retained local artifacts or an equivalent newly provisioned stack;
fresh cloud restoration is NOT RUN and is not certified by saving a draft.

run.mjs:

```javascript
import fs from 'node:fs';import {parseEnv} from 'node:util';import {spawn} from 'node:child_process';
const root='/workspace/Framwork',dir='/workspace/Framwork/.local/merged-rebaseline-20261020',env={...process.env};
// Suppress fallback to unrelated default-stack bindings in scripts/with-env.mjs.
for(const key of Object.keys(parseEnv(fs.readFileSync(root+'/.env','utf8'))))env[key]='';
Object.assign(env,parseEnv(fs.readFileSync(dir+'/.env','utf8')));
env.PATH=dir+'/bin:'+process.env.PATH;
env.M9R_LEGACY_API_BINARY=root+'/.local/reports/r3c-pre49-api';env.M9R_LEGACY_API_SHA256='03811f7cc694f33757aa4b005179f69f4ad6f94c525c6592a4e7cdf3e2537a19';
const [c,...args]=process.argv.slice(2);if(!c)throw Error('Command required');const child=spawn(c,args,{cwd:root,env,stdio:'inherit'});child.on('error',()=>{console.error('Command launch failed');process.exitCode=1});for(const s of ['SIGINT','SIGTERM'])process.on(s,()=>child.kill(s));child.on('exit',(c)=>{process.exitCode=c??1});
```

bin/docker:

```javascript
#!/usr/bin/env node
import {spawnSync} from 'node:child_process';import fs from 'node:fs';
const a=process.argv.slice(2), target='framework-merged-20261020-postgres-1';
if(a[0]==='inspect'&&a.length===4&&a[1]==='--format'&&a[3]==='framework-m0-postgres-1'){a[3]=target;fs.appendFileSync('/workspace/Framwork/.local/merged-rebaseline-20261020/docker-reference-remap.log','inspect '+target+'\n');}
else if(a[0]==='exec'&&a[1]==='framework-m0-postgres-1'&&a.slice(2).join(' ')==='psql -U postgres -d postgres -Atc SELECT system_identifier::text FROM pg_catalog.pg_control_system()'){a[1]=target;fs.appendFileSync('/workspace/Framwork/.local/merged-rebaseline-20261020/docker-reference-remap.log','cluster_identity '+target+'\n');}
const r=spawnSync('/usr/local/bin/docker',a,{env:process.env,stdio:'inherit'});process.exit(r.status??1);
```

compose.yaml:

```yaml
services:
  postgres:
    ports: !override ["127.0.0.1:15432:5432"]
    volumes: !override ["pgdata:/var/lib/postgresql", "/workspace/Framwork/.local/merged-rebaseline-20261020/postgres-init.sh:/docker-entrypoint-initdb.d/01-roles.sh:ro"]
  redis:
    ports: !override ["127.0.0.1:16379:6379"]
    volumes: !override ["/workspace/Framwork/.local/merged-rebaseline-20261020/.local/m8/users.acl:/run/framework/users.acl:ro"]
  otel-collector:
    ports: !override ["127.0.0.1:14318:4318", "127.0.0.1:14133:13133"]
    volumes: !override ["/workspace/Framwork/.local/merged-rebaseline-20261020/collector.yaml:/etc/otelcol/config.yaml:ro"]
```
