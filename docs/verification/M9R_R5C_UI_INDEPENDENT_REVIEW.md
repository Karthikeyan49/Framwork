# R5c UI independent adversarial review

Date: 15 October 2026 IST. Implementation reviewed: `c993362` on
`remediation/m9r-foundation`, based on `aa1fa2a`.
Final status: **PASS on stable source c0df7c2 within the bounded preview contract**.
The historical FAIL checkpoints and all three corrected findings remain below.
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

## Re-review of f81d207

The implementing agent committed both original corrections and froze the source
again. Independent execution confirms R5C-UI-01 and R5C-UI-02 are closed within
the exercised cases: all eight role/projection combinations and the opaque
scope-denial read pass; the missing target in a truncated window is blocked.
Additional complete-list absence, absent unknown-share, and malformed-target
controls pass. No scope, snapshot proof or row-budget relaxation was observed.

### HIGH R5C-UI-03 — Masked assign-only delegation is rejected by full-record authorization

The real browser now reaches the selected masked record and ownership panel.
Its first transfer without `preview.write` correctly returns 403 and leaves the
owner/version unchanged. The reviewer corrected the browser fixture to publish
a new live role version explicitly granting `preview.write`; no server authority
was weakened. The resulting transfer still returns 403 despite the required
preview, entity OWNER read/write and row read/write/assign capabilities.

`authorizeAccess` calls `current`, which computes read DataScope for every field
and fetches/decodes the complete business JSONB. It then calls
`authorizeExistingWrite`, which requires write scope for every business field.
A delegate denied name read and lacking field-write grants therefore cannot
perform ownership administration. This conflicts with ADR034/038's intended
entity/record administrative boundary and the assign-only masked UI scenario.
The earlier mocked UI test did not prove this end-to-end server behavior.

`TestIndependentR5cMaskedDelegateAccessChanges` isolates seven live role/record
cases. Five controls pass: missing preview.write, missing row.assign, a record
outside OWNER read scope, NONE entity write scope, and a fully field-authorized
assigner. The masked assign-only case and a paired case containing a malformed
hidden business field fail with 403 instead of 200. Missing row.share never
permits a share listing or share mutation; denied/successful operations retain
exact stored header witnesses. The malformed value exists only in a newly
created test record and was installed with a transaction-local trusted fixture
identity, never in customer data. It ensures the eventual administrative path
need not decode an unreadable business value.

Fix this using a typed-header read under the exact entity read/write DataScopes,
plus trusted owner row guards and the existing assign/share capability, SQL port,
CAS, recipient/binding fences and transaction/audit behavior. Do not grant hidden
field access or all field writes to make the browser pass. Preserve denial for
non-owners, NONE scope, missing administrative capabilities and revoked roles.

| Re-review command | Result |
| --- | --- |
| `npm --prefix apps/web test -- --run src/administration/Administration.review.test.tsx` | PASS 12/12; 3.59 seconds |
| `npm --prefix apps/web run typecheck`; `npm --prefix apps/web run lint` | PASS |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5c' ./tests/integration` before adding the new administration matrix | PASS 6.165 seconds; original findings' Go probes closed |
| `npm --prefix apps/web run test:e2e -- administration.review.spec.ts` with explicit live preview.write grant after denial control | FAIL: transfer still denied 403; 8.6-second scenario. Query, masked record, core ownership header and 390-pixel ownership-panel assertions pass before this failure |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5cMaskedDelegateAccessChanges$' ./tests/integration` first isolated matrix | FAIL: masked assign-only 403; 2.928 seconds |
| Same Go command while strengthening malformed-field fixture | FAIL: masked assign-only 403, then fixture setup rejected an unscoped UPDATE with trusted actor required; 3.209 seconds. Fixture corrected using transaction-local trusted identity |
| Same Go command on final seven-case matrix | FAIL: masked assign-only and malformed hidden-value cases 403; five controls pass; 3.772 seconds |

The ownership panel's 390-pixel overflow assertion is now executed and passes.
The actual transfer, loss of OWNER access, narrow team and sharing panel checks
remain NOT VERIFIED because R5C-UI-03 stops the browser earlier. Root's full-suite
run also exposed the initial missing-preview.write fixture: its failure is
retained in the implementation report. At that checkpoint the independent verdict was
**FAIL pending R5C-UI-03 correction and re-verification**. No production source
was edited by the reviewer; no workloads remain running at handoff.

## Final independent re-review of c0df7c2

Verdict: **PASS within the bounded R5c preview administration contract**.
All three review findings are corrected and independently re-executed. This
is not overall M9R completion or production certification. The source remained
stable during review; the reviewer added only tests and this report.

R5C-UI-03 now uses typed ID/version/owner/team headers under the live entity
read/write SQL scopes, followed by the trusted-owner row/action guards. No
business JSON is selected or decoded by this administrative preflight. The
existing port, expected version, binding fence and transaction boundaries remain.
The seven-case mutation matrix now passes, including hidden-name delegation,
malformed hidden-value independence, non-owner and NONE-scope rejection,
missing-preview.write/row.assign denials, and broad-field positive control.
Successful replies are also checked for absence of business-value keys.

An additional live-role test grants row.share while explicitly denying all
business field read/write access. Reading the authorized share facts, granting
and revoking a read share succeed. Ownership transfer without row.assign fails.
After the role is deactivated, share-list and mutation requests fail and the
stored owner/version/share facts remain unchanged. These are real transaction
and stored-header checks rather than transport mocks.

The independently updated browser fixture keeps the initial missing
preview.write denial and confirms the stored owner/version remain unchanged.
It then publishes a role version explicitly granting that capability. The
field-restricted delegate successfully transfers ownership without row.share,
teams.read, memberships.read or business field-write grants. The reply reports
saved version 3, the following OWNER read becomes unavailable, and a direct
query confirms the old owner sees no records. The privileged read confirms the
new owner and exact version. Hidden name never appears in the delegate's DOM.

At a 390 by 844 viewport, the selected ownership panel, a newly created team's
administration panel and the sharing panel each pass a document-width assertion.
This is bounded narrow-viewport coverage, not a claim about all browsers,
translations, long names, hierarchy widths or accessibility states.

| Final independent command | Result |
| --- | --- |
| `./scripts/go.sh fmt ./tests/integration` | PASS, independent additions formatted |
| Go integration command shown below | PASS seven top-level tests; 25.903 seconds. Includes the eight-case query and seven-case access matrices, opaque denied-scope masking, share/revocation test, legacy/canonical/SQL-port regressions |
| `npm --prefix apps/web test -- --run src/administration/Administration.review.test.tsx` | PASS 12/12; 3.37 seconds |
| `npm --prefix apps/web run typecheck` | PASS |
| `npm --prefix apps/web run lint` | PASS |
| `npm --prefix apps/web run test:e2e -- administration.review.spec.ts` | PASS 1/1; 4.4-second scenario, 7.2-second runner |

Exact final Go command:

```sh
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -timeout=5m -count=1 -run '^TestIndependentR5c|^TestM9RRecordAccess' ./tests/integration
```

All earlier failures remain above, including review fixture mistakes and the
original pre-fix behavior. No production source was changed by the reviewer,
and no test workload remains running at handoff. Root must complete its full
`make check test build` and browser gate against these stable source changes
before the milestone completion report. Full R5/M9R remain PARTIAL; identity,
remaining R3, R6, R7 and the unconfirmed workflow connection failure remain open.
