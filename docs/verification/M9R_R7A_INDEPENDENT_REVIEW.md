# M9R R7a independent adversarial review

19 October 2026 UTC. Stable reviewed checkpoint **`8f9805e`**, compared with the
completed local R5 checkpoint `4f01dfd`. ADR061 was committed before product code
at `be7b01a`. Checkout clean at entry; implementation agent paused source edits,
builds and shared database workloads during this review. Reviewer changes are
only independent tests and this report. No schema, dependency, authorization,
provider, main merge, production activation or M10 change.

Result: **bounded independent PASS; no BLOCKER/HIGH/MEDIUM/LOW product finding
identified**. The original missing-admission behavior is independently reproduced
against the checksum-pinned old binary, and the same raw acceptance test passes
against the fixed binary. This does not close full R7 or certify production abuse
capacity. Final root regression/browser/reference-capacity gates remain required.

## Scope and threat model

Read AGENTS/CLAUDE, ADR061, the R7 plan, full implementation diff, relevant API
transaction/retry, configuration, HTTP wrapper and runtime-environment contracts.
Attempted peer-port/IP-format/forwarding-header bypass, signed identity tuple
confusion, claim/token/peer rotation, parallel quota overdraft, active-entry
eviction, expired-slot storage growth, clock regression/fractional refill,
request/subject metric cardinality, observer panic, signature/body/database work
after rejection, queued expiry/cancellation, stale membership, audit failure and
known-abort retry changes.

Tests use actual runtime-role PostgreSQL and populated disposable fixtures.
Effective driver/database/role targets are verified before targeted fixture
queries/changes. Authenticated authority remains live database membership/policy;
the gate grants no capability and retains no authorization result. Invalid JWTs
are crypto/claim failures; signed-valid unprovisioned or revoked users still use
live database resolution under the general budgets. No negative membership cache
or unverified identity-key selection is inferred.

## Actual pinned before/after API reproduction

Added explicit `integration && trafficreview` diagnostic
`TestM9RTrafficReviewPinnedBinaryInvalidBurst`. It requires a supplied artifact
and SHA-256, fails if either is absent/mismatched, and has no default SKIP. It uses
separate `R7A_REVIEW_API_*` variables and never changes the existing
`M9R_LEGACY_API_BINARY`/SHA settings or artifact.

Each invocation starts its own ephemeral loopback API against its owned
disposable database. The child receives an explicit API-only environment:
runtime database/cache, public verifier JWKS, exact test issuer/audience and
disabled events/workflows. No operator/worker/dispatcher bindings, private
signing key or credential-bearing arguments reach the child. Startup requires
actual `/readyz`200; shutdown waits for that owned process only. Raw API output
is discarded. The probe issues 40 malformed-bearer GETs to `/v1/session`, rotates
valid untrusted forwarding headers, checks unique server request IDs and safe
429 envelopes/Retry-After/no-store, then counts exact matching audit request IDs.

| Artifact | Identity | Actual result |
| --- | --- | --- |
| `.local/reports/r7a-before-api` | SHA-256 `ee24c2fb16c7fa6c4b0b9d5588d2a0c5f39168587ecd6609916e57aa989fdaa8`; buildinfo Go1.27.2, revision `ba21eb0`, modified=true. This was the retained R5 product source with documentation edits before R7a | **Raw acceptance FAIL**: 40×401, zero429, 40 matching `authentication.denied` rows, zero rejected-request audit rows; request loop79ms, test6.105s |
| `bin/api`, built during this review | SHA-256 `c3f57d1563b5de0b02848f1a0f01430399c7e349d7f140ba8a9ae26f8019e4f3`; Go1.27.2, revision `8f9805e`, modified=true from reviewer test files. Product source unchanged during review | **PASS**: 20×401, 20×429, 20 matching denial audit rows, **zero audit rows for every429 request ID**; request loop50ms, test7.119s |

Exact successful reproduction commands, from the checkout root:

```sh
R7A_REVIEW_API_BINARY=$PWD/.local/reports/r7a-before-api R7A_REVIEW_API_SHA256=ee24c2fb16c7fa6c4b0b9d5588d2a0c5f39168587ecd6609916e57aa989fdaa8 node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration,trafficreview -timeout=2m -count=1 -run '^TestM9RTrafficReviewPinnedBinaryInvalidBurst$' -v ./tests/integration
./scripts/go.sh build -o bin/api ./cmd/api
R7A_REVIEW_API_BINARY=$PWD/bin/api R7A_REVIEW_API_SHA256=c3f57d1563b5de0b02848f1a0f01430399c7e349d7f140ba8a9ae26f8019e4f3 node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration,trafficreview -timeout=2m -count=1 -run '^TestM9RTrafficReviewPinnedBinaryInvalidBurst$' -v ./tests/integration
```

Build occurred before executing the two final binary probes; the ordering above
groups commands for reproduction. Rebuilding after later commits changes the
fixed artifact hash: explicitly review/pin that new artifact rather than changing
an expected checksum to conceal an integrity failure.

Ignored local evidence: `.local/reports/r7a-independent-before-rerun.log` and
`r7a-independent-after-rerun.log`. The initial before/after attempts both failed
from a reviewer fixture bug: character arithmetic in rotated forwarded headers
eventually produced a control byte that the real HTTP client rejected. The
fixture now generates valid decimal IPv4 strings. Initial failures remain in
`r7a-independent-before.log` and `r7a-independent-after.log`; they were not the
raw admission acceptance result and are not counted as product findings.

## Executed populated attacks and faults

The selected integration command executed **8 groups with no skips/failures**:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=3m -count=1 -run '^TestM9RTraffic' -v ./tests/integration
```

Final result: **PASS8.343s**, including three independent new groups:

- Inject an actual PostgreSQL40001 at command audit time. One logical request
  consumes the single configured request allowance, but its known-aborted whole
  command retry rechecks JWT validity: exactly three authenticator calls
  (precheck and two transaction attempts), one business effect, one receipt and
  one outbox fact. A second exhausted POST uses a body that panics on read;
  HTTP429 occurs before crypto/body/retry and facts remain exactly one each.
- Hold the database admission lease, queue a signed-valid request, cancel its
  context, and observe503, no transaction audit, only the initial verification,
  removed queue slot and no leaked lease. The retained actual expiry test also
  waits until the signed token really expires before releasing its database slot:
  second verification denies401.
- Run100 simultaneous malformed bearer requests from one peer. Exactly20 enter
  denial audit and80 return429, with exactly20 audit calls. No concurrent burst
  overdraft or generic503 occurred.

The five retained groups independently rerun in the same command prove peer and
global gates precede signature/body work; exhausted invalid/subject gates do not
touch even a closed database pool, while a new-peer negative control reaches503;
token/role/tenant/peer/header rotations do not evade signed-subject limits; live
membership revocation denies and admitted audit failure remains503. Their first
unchanged five-group run also passed6.262s before adding reviewer probes.

Unit/config/wrapper checks:

```sh
./scripts/go.sh test -race -count=1 ./internal/platform/traffic ./internal/platform/httpserver ./internal/config
node --test scripts/runtime-env.test.mjs
make docs-verify
git diff --check
```

All **PASS**. Final Go package timings: traffic1.446s, httpserver1.183s,
config1.017s. Traffic has eight original and two independent new test groups.
Node9/9 passed without skips. Documentation verifies84 retained architecture
and43 V3 checksums. The wrapper executes1000 actual-stack429 responses and1000
parallel log-record calls, proving bounded summaries, exact window totals and
redaction of sensitive host/query/header strings. Safe request ID, no-store and
exposed Retry-After assertions pass.

New metric tests execute **all seven actual gate rejection reasons**, including
populated8192 peer/subject caps. Every rejection measures exactly one and has
only two attributes: the closed reason and fixed policy version. No request,
subject, address, token, path or tenant value becomes an attribute. Four complete
8192-subject expiry/reuse cycles retain bounded maps/slot/free arrays; repeated
active rejections cannot reset token debt or obtain fractional refill. Original
tests also execute monotonic-clock regression, IPv4-mapped/port/unknown-address
normalization, tuple-length ambiguity, peer/global/invalid separation, active
entry preservation,1000-way burst contention, observer-panic containment and
invalid/disable-like configuration denial.

## Limits and recovery

This is per-process admission for protected access API routes. It is not a
cluster quota, durable forensic system, DDoS protection, provider login quota or
health/readiness protection. Unknown routes and middleware-level rejects have
their existing separate behavior. Proxy clients may share one actual socket-IP
bucket; forwarding headers cannot grant a separate peer. API replicas/restarts
change the available aggregate budget. No production abuse envelope is certified
by the40-request probe or test timings.

Rejection log summaries emit on a429 after the10-second interval; there is no
background flush when traffic stops. Suppressed tail counts may never appear in
a later log summary. Closed counters record every gate rejection in process;
export loss/restart remain possible and are not durable audit retention. Existing
immutable audit/receipt history is preserved. No telemetry-provider blocking or
arbitrary injected database-driver panic recovery guarantee was tested here.

No extra retries are introduced for429, failed/unknown writes or external effects.
Clients retain existing explicit retry/reconciliation contracts. Rollback removes
the admission layer only after assessing restored abuse exposure; restart is not
an audit/inbox/receipt reset. No migration or existing business record was altered.
Disposable test cleanup is unrelated to production evidence retention.

Full `make check test build`, all11 application plus7 sign-in browser cases and
all8 unchanged reference capacity targets are **NOT RUN by this reviewer** and
remain the implementation agent's final gates because common HTTP admission
changed. Local R5 stays complete within its documented limits; external
IdP/domain/MFA/gateway verification remains deferred. R6, driver-panic recovery,
worker/connection timing, outage/replay, retention/DR and other R7 work remain
open. No main merge, production deployment, M10 or broader M9R completion follows.
