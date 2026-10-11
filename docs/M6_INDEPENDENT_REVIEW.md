# M6 independent adversarial review

- Date: 2026-10-11
- Scope: M6 — Dynamic UI for isolated development/test previews
- Reviewer: independent Codex review agent under `AGENTS.md` and
  `docs/agents/82_TWO_AGENT_PROTOCOL.md`
- Result: **PASS** for the reviewed source and independently executed checks below.
  No unresolved BLOCKER/HIGH/MEDIUM finding remains.

The user authorized M4–M6 together. This review covers M6, including its small
compiler-component and datetime-validation compatibility changes. M2–M5 are the
baseline. The reviewer made no implementation or repository test edits. Independent
test overlays and harnesses lived under `/tmp`; this report is the reviewer-owned
repository change. PASS does not imply that these changes have been committed or
pushed, that production activation exists, or that later milestones are implemented.

## Architecture and implementation

Reviewed the versioned descriptor decoder and component registry, workspace renderer,
preview API client, access-console composition, CSS, frontend tests, Playwright
configuration/fixtures, browser installer, Make targets, CI and ADR-019 against the
UI/runtime/security specifications and milestone roadmap.

The renderer obtains compiled IR from an authenticated protected endpoint and checks
application/revision, supported compiler/IR/storage versions, stable references,
field/component compatibility and entity/tree budgets. Unknown components and versions
fail closed. Metadata labels and stored values render as React text; no arbitrary
HTML, scripts, imports, metadata URLs or expression evaluation are introduced. The
real browser scenario displays a literal script-shaped value and finds no script
element in the rendered application.

The access console resolves membership server-side before mounting a tenant-keyed
workspace. Tokens remain in memory. Preview requests use Authorization headers,
`credentials: omit`, explicit validated paths and bounded cancellation/timeouts.
Application/revision changes clear and abort old requests; tenant/page changes unmount
old state. Completion handlers check cancellation before installing results. The
frontend does not substitute visibility hints for authorization: the browser scenario
checks both disabled viewer controls and an actual server-side 403 for a forged viewer
write.

Forms preserve decimal/money strings, require exact integer bounds, exclude immutable
core aliases and send expected record versions. Datetimes preserve RFC3339 text and
nanoseconds; strict calendar/UTC validation remains in Go. Reference choices query
the same protected dataset and use the typed record envelope ID even when metadata
exposes no ID alias. Record details always expose that envelope ID. A conflicting or
uncertain mutation requires a successful reload before more mutations; there is no
automatic mutation retry. Production preview routes remain absent. No M7 execution,
workflow/tool side effect, sandbox lifecycle or production promotion is added.

## Findings resolved

1. **MEDIUM — datetime coercion changed customer values.** Executing the original
   `formValues` function showed that JavaScript Date conversion truncated
   `.123456789Z` to milliseconds, repaired February 30 into March, and accepted a
   timezone-less timestamp. The function now preserves the exact text and requires
   explicit RFC3339 timezone syntax. Go also rejects excess fractional digits and
   invalid timezone syntax. The final independent harness verifies unchanged
   nanoseconds and invalid calendar text, plus rejection of a missing timezone;
   repository Go/frontend tests cover authoritative validation.
2. **MEDIUM — uncertain writes could be repeated immediately.** An independent
   rendered-workspace test simulated a transport failure after a potentially
   committed CREATE. The UI displayed the raw transport message and reenabled
   CREATE. Its safety assertion initially **FAILed**. The renderer now replaces
   transport/5xx details with an explicit uncertain-outcome message and disables
   mutations/inputs until successful reload; conflicts also require reload. The
   same independent test now **PASSes**. A repository regression verifies that a
   failed reload keeps mutations disabled and a successful reload restores them.
3. **MEDIUM — compiler/renderer contracts diverged.** Independently compiled
   source accepted missing/incompatible status-badge fields and nested forms that
   the decoder rejected, so valid compiled applications could fail preview or form
   semantics. Compiler checks now reject those definitions with diagnostics, and
   decoder checks agree. Conversely, compiler-valid entities without an exposed
   core ID alias must remain usable: the decoder permits an optional validated
   alias, and reference choices use `record.id`. The final independent Go overlay
   asserts all four compilation outcomes, and the actual decoder accepts the
   optional-core IR. Empty entities now reject; scoped datalist identifiers avoid
   collisions between repeated reference controls.
4. **MEDIUM — browser fixture failures could expose credentials.** An owned
   Chromium probe using only a synthetic marker demonstrated that Playwright's
   failed `fill` on a disabled password control includes the value in its error
   message. Disabling screenshots/traces does not redact that call log. Credential
   entry now catches and replaces errors without forwarding messages or causes.
   Authenticated Playwright API fixture requests receive the same treatment because
   their failure logs can contain Authorization headers. Both final real-browser
   synthetic-marker regressions verify safe errors without credential/header values.

All fixes were inspected and reverified. No real credentials were printed, and no
existing customer data was modified. Browser integration creates unique operator-
provisioned development fixtures; their synthetic metadata/data/audit evidence remains
isolated in the shared local database.

## Independently executed checks

Commands ran from `/workspace/Framwork` against the final implementation. Temporary
harnesses exercise actual source through Vitest, Go overlays or TypeScript
transpilation; they do not replace implementation. They were removed after review.

| Command | Result | Evidence |
| --- | --- | --- |
| `make check` | PASS | All 84 supplied-document checksums, Go format/static checks, module verification, TypeScript lint and typecheck. |
| `./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/runtime/data ./internal/policy ./internal/platform/httpserver ./tests/security` | PASS | Compiler, typed values, policy, HTTP and security regressions; no race reported. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^Test(M4\|M5)' ./tests/integration` | PASS | Actual normal-role protected compiler/runtime API regressions; 4.588s initially and 4.705s after the final application-scoped policy hardening below. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run test` | PASS | 23 tests across five files; 2.02s. Includes stale descriptor suppression, read-only hints, safe errors, typed form payloads, CAS, exact datetime and uncertain-write/reload recovery. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web run build` | PASS | TypeScript and production Vite build; 31 modules transformed. |
| `make test-e2e` | PASS | Final three real Chromium tests, 4.7s: real Go/PostgreSQL CRUD, stale-version reload, deletion confirmation, 21-row pagination, viewer denial, disconnect/revision isolation, literal script text, 390px mobile no-overflow assertion and both credential/header-error redaction regressions. |
| `npm --cache /workspace/Framwork/.local/cache/npm --prefix apps/web exec -- vitest run --config /tmp/m6-review.config.mts` | FAIL initially; PASS after fix | Independent uncertain-write test; final 1.17s. Its initial failure is disclosed above. |
| `./scripts/go.sh test -race -count=1 -overlay /tmp/m6-review-go-overlay.json -run '^TestIndependentM6RendererCompatibility$' -v ./internal/compiler` | PASS | Final assertions accept optional-core source and reject missing/money status badges and nested forms; 1.156s. Initial observation probes demonstrated acceptance of those invalid components before the fix. |
| `node /tmp/m6-review-values.cjs` | PASS | Actual form/decoder functions: exact nanoseconds, no calendar repair, missing-timezone rejection, compiler-produced optional-core descriptor acceptance and empty-entity rejection. |
| `git diff --check` | PASS | Working tracked diff has no whitespace errors. |

An earlier independent `make test-e2e` also passed the initial single CRUD scenario
(4.1s), before the final mobile/redaction/recovery refinements. The final three-test
run supersedes that browser evidence. Playwright-owned API/Vite processes exited
after both runs; ports were released to the implementer. The reviewer did not stop
the shared infrastructure or another agent's services.

No final executed check failed. The review does not claim a fresh hosted GitHub
Actions run, a clean-machine browser installation, full application integration
coverage, full accessibility certification or production performance testing. Those
are distinct from the local checks actually executed here.

## Final application-policy composition recheck

The final composition change also supplies validated AppID to outer metadata
read/write/compile decisions, alongside the already scoped preview decisions. The
same validated ID is recorded in `State.MetadataApp` before deciding, so an
application-scoped denial is auditable even when no document is loaded. Inspection
confirmed that this check precedes registry reads/appends and compilation and retains
the authenticated tenant/user subject, obligation enforcement and buffered
audit-before-response transaction. The new real normal-role
`TestM4ApplicationScopedCompilePolicy` rejects compilation with 403 for a policy
denying an application-scoped request. The reviewer independently reran the complete
`^Test(M4|M5)` race integration subset and `make check` after this change; both pass.
The implementer's initial fixture compilation typo was corrected before those
independent runs; it is not claimed as a passing run.

## Practical limits and next boundary

This is a built-in versioned preview renderer with a 32-field projection ceiling,
20-row pages, bounded component trees and the M5 1,000-candidate visibility-proof
limit. Reference pickers show 20 choices plus explicit UUID entry; larger searchable
relationships and field masking require future capabilities. Unknown/new compiler
versions reject until supported. Browser tests disable snapshots/traces/video and
store generated outputs under ignored `.local`; build artifacts, dependencies,
tokens and keys should not be committed.

Semantic labels, keyboard buttons, status/alert messages, table headers and the
390px overflow check provide a baseline, not an accessibility audit. The renderer
does not run rule/workflow expressions or governed actions. An aborted write may
have committed server-side; users must reload and inspect before repeating it.
Separate requests do not promise a frozen dataset. M7 and later work require a new
authorized milestone and their own independent verification.
