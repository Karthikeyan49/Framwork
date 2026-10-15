# R5c UI independent adversarial review

Date: 15 October 2026 IST. Implementation reviewed: `c993362` on
`remediation/m9r-foundation`, based on `aa1fa2a`.
Status: **FAIL — fixes and independent re-verification required**.
Full R5/M9R remain PARTIAL; no M10, main merge or production activation.

This review was performed by a separate agent after the implementation was
committed. The implementing agent froze source edits and test workloads.
Only independent tests and this report were added during review. No runtime
source, migration, dependency, production data or permissions were changed.

## Findings

### HIGH R5C-UI-01 — Valid scoped delegates cannot load records or ownership headers

A real custom role was published and assigned through the governed role APIs.
It has `data.entity.read/write` with OWNER scope, `data.row.read/write/assign`,
field read ALL with an explicit denial on name, and no field-write, sharing,
team or membership-read authority. Its ordinary visible query returns 503,
preventing the new UI from reaching the selected record. The browser first
stopped at Open record after 60 seconds; adding a direct HTTP query check
confirmed the actual 503 in 1.6 seconds. This is an implementation failure,
not a successful denial, and not evidence that the transfer works.

`TestIndependentR5cAssignOnlyHeaderQueries` isolates the failure with populated
records and live role definitions. Five of eight projection/role cases pass:
read-only and ALL-write controls both pass core-only and masked projections;
OWNER-write with field-write passes the masked projection. Three cases fail:
OWNER-write without field-write fails both projections, and OWNER-write with
field-write fails the core-only projection used by the ownership panel.

Source inspection of `visibleJSON` identifies an eager entity-write scope bind
that is never used if no writable non-core projection consumes that expression.
PostgreSQL cannot resolve such missing parameter positions. The test reports
only HTTP status, not raw driver errors or credentials.

A second reproduction, `TestIndependentR5cOpaqueAllowedActionDeniedScope`, uses
an opaque Go policy whose action permits the field but whose DataScope returns
ErrScope. The read should mask the value; it instead returns 503. `fieldPredicate`
binds the opaque action's ID array before discovering the scope denial, then
returns a constant FALSE without consuming the parameter. Both paths need
parameter-safe construction, without broadening action/scope authority or
fetching hidden values. Preserve the positive role controls and strict input,
worker confinement, opaque snapshot and row-budget restrictions.

### MEDIUM R5C-UI-02 — Truncated share review can dismiss an unobservable target

A pending unknown unshare contains a target recipient absent from the returned
truncated share window. After the read, Use reviewed record state is enabled,
even though that recipient may remain outside the window and its current state
has not been inspected. The UI's causal-outcome caveat does not make a missing
recipient in a truncated list evidence of absence.

The independent component test fails on the enabled review button. A positive
control where the pending recipient is present in the truncated window passes.
Require either an observable target state or an explicit separately designed
operator-resolution path; failed, incomplete or unrelated reads must not silently
release this resource's gate. An unlisted target in a complete share view can
be reviewed as absent. Ownership-only reads must remain insufficient for share
review.

## Executed commands and evidence

| Command | Result |
| --- | --- |
| `npm --prefix apps/web test -- --run src/administration/Administration.review.test.tsx` | FAIL: 7/8 pass, 1 fails on truncated target review; 2.79 seconds total |
| `npm --prefix apps/web run typecheck` | PASS with independent tests |
| `npm --prefix apps/web run lint` | PASS with independent tests |
| `npm --prefix apps/web run test:e2e -- administration.review.spec.ts` initial | FAIL: Open record never appears; 60-second browser timeout |
| Same browser command with direct HTTP precheck | FAIL: query expected 200, actual 503; 1.6-second scenario, 6.46-second runner |
| `./scripts/go.sh fmt ./tests/integration` | PASS, new independent Go fixture formatted |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5cAssignOnlyHeaderQueries$' ./tests/integration` | FAIL: 3/8 query subcases fail 503; 3.843 seconds package |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5c' ./tests/integration` | FAIL: same 3 subcases plus opaque denied-scope projection; 4.668 seconds package |

The seven passing component probes cover exact membership and ownership witness
mismatches without retries, serialized record reads, abort after dispatch retaining
an unknown intent, a late exact success releasing its own intent after unmount,
and malformed duplicate-member reads preserving the gate and hiding edit controls.
These are protocol/UI probes with synthetic transport, not database-effect claims.
The Go role matrix and browser setup use real PostgreSQL/HTTP state. Browser
fixtures create independent test tenants and retain server audit and records.

The real delegated ownership transfer, live loss of OWNER access, and ownership
panel at a 390-pixel viewport are **NOT VERIFIED**: the earlier backend 503
prevents those assertions. The browser test retains these assertions for the
post-fix rerun. No claim of responsive administration coverage is made yet.

The full `make check test build` and full browser suite are the implementing
agent's final gate after fixes. They were **NOT RUN by this reviewer**. Existing
root results are described in the implementation report, not reclassified here.

## Boundaries and handoff

Source review covered actor filtering and payload-free recovery storage,
component abort/unmount behavior, exact conflict witnesses, 4xx versus unknown
classification, independent ownership/share reads, record/business invalidation,
client path checks and UI composition. It does not certify all frontend states,
production activation, proxy behavior, browser closure recovery or timing
noninterference. The in-memory journal and membership ABA limitations remain
explicit ADR038/035 constraints.

The implementing agent must fix both findings, commit stable source, and request
independent re-review. Do not mark R5c UI complete until the negative fixtures,
real delegated browser case, final checks/build/browser suite and reports pass.
No production source fix was made by the reviewer. The earlier unconfirmed
workflow database connection failure remains R7 work.
