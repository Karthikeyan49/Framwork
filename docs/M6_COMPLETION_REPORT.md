# M6 completion evidence

The versioned React registry renders compiled draft IR into composed sections/toggle
tabs, forms, typed inputs/select/reference picker/status badge, paginated tables and
record details. CRUD carries row versions; conflicts require explicit reload and
delete requires confirmation. Unknown components/versions/field mismatches fail closed.
Metadata renders as text. Tokens remain in memory; verified tenant selection gates
mounting, request cancellation suppresses stale completions, read-only UI hints never
replace central server policy. Production preview routes remain absent.

Important structure:

- internal/compiler: M4 private immutable Program, typed canonical IR, diagnostics.
- internal/runtime/data: M5 parameterized plans, CRUD/CAS, visibility proof and values.
- internal/platform/access: authenticated compile and development preview composition.
- contracts/examples: draft contacts and vendor fixtures; no customer-specific code.
- db/migrations/00004–00008: accumulated M2–M5 forward migrations, all forced RLS.
- apps/web/src/preview: versioned contracts/decoder, composable renderer/workspace.
- apps/web/src/api/preview.ts: bounded authenticated typed preview client.
- apps/web/e2e + playwright.config.ts: real browser/API/PostgreSQL scenario.
- scripts/browser-install.mjs, Makefile and CI: reusable browser/test commands.
- docs/adr/017–019: compiler, preview data and dynamic UI decisions.

Commands actually executed:

| Command | Result |
| --- | --- |
| `npm --cache .local/cache/npm --prefix apps/web install --save-dev --save-exact @playwright/test@1.64.0` | PASS |
| `PLAYWRIGHT_BROWSERS_PATH=/workspace/Framwork/.local/cache/ms-playwright npm --cache .local/cache/npm --prefix apps/web exec -- playwright install chromium` | FAIL;403 network domain policy; no TLS/signature bypass |
| `chromium --version` | PASS; installed Chromium154.0.8037.57 |
| `make browser-install build` | PASS; system browser selected, four Go binaries and frontend built |
| `npm --cache .local/cache/npm --prefix apps/web test` | Initial FAIL19pass/1fail: exact money label selector; corrected accessible labels, then PASS20tests/2.17s |
| `npm --cache .local/cache/npm --prefix apps/web run lint` | PASS |
| `npm --cache .local/cache/npm --prefix apps/web run typecheck` | PASS |
| `make test-e2e` | Initial FAIL: exact required-name label selector timed out; accessible label fix then PASS1realbrowser scenario3.2s/4.4s |

The initial test failures were exposed and corrected. The browser download remains
blocked by the current cloud egress list; existing system Chromium supplies actual
browser evidence. CI uses supported managed Chromium installation. No claim of that
blocked download succeeding is made. No production runtime dependency is added;
Playwright1.64.0 is a pinned development dependency solely for E2E browser verification.

Browser acceptance exercises real owner connection/live membership, compiled descriptor,
metadata-generated forms/table/detail, exact money/date creation, HTML text safety,
update/version2, concurrent API update/stale409, explicit reload, confirmed delete,
21-record paging20+1, viewer UI read-only and forged API write403, disconnect and draft
switch clearing the previous page. Unit tests cover compatibility fail-closed, typed
payload/value preservation, malicious text, denied write redaction, stale descriptor
completion and selected row version/no automatic retries.

Known limits: built-in renderer32 fields/entity, reference choices first20+explicitUUID,
no custom React imports, file/chart/KPI or tool-action widgets; no field masking,
full accessibility certification or performance claim. Pure rules/workflows/tools/
outbox/AI/production sandbox/promotion remain later milestones. M6 does not execute
compiler generated-test requirements. M7 is not started. E2E uses unique isolated
operator-provisioned development fixtures; retained data/keys are preserved.

Security: script/HTML/raw SQL/metadata URLs are never evaluated. Unknown version/
component/reference fails validation; typed API is authoritative. Requests time out/
abort; aborted commands may already have committed and require reload before repeating.
Browser traces/video/screenshots are disabled. Secrets, dependencies, browser outputs,
.local, bin and frontend dist are ignored and must not be committed.

Final cumulative verification and independent M6 review PASS; Git delivery recorded
in M4_M6_COMPLETION_REPORT.md.

Review-driven corrections: preserve exact timestamp text/nanoseconds for Go validation;
require explicit timezone, reject excessive fractional precision/invalid offsets on
server; make optional UUID aliases compatible; align compiler badge/nested-form
validation; block mutations after unknown network/5xx outcome or409 until successful
reload, including form-only pages; redact credential-entry and API fixture failure
logs. Empty entities reject and datalist IDs are unique. Latest frontend tests PASS23.
A temporary timestamp-regex edit caused `make fmt` and focused Go compilation to FAIL;
the edit was corrected and focused race/full checks/tests/build passed again.

Additional exact commands executed:

| Command | Result |
| --- | --- |
| `make fmt check test build` | PASS; Go races,23frontend tests, real integrations11.326s, four binaries/frontend |
| `make check build` | PASS after final browser hardening |
| `make test-e2e` | PASS;3tests/4.7s including mobile and credential/header redaction |
| `make migration-status` | PASS;00001–00008 applied |
| `npm --cache .local/cache/npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS;0 vulnerabilities |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS; seven readiness contracts, health/proxy/auth/CORS, traces/metrics, graceful shutdown |
| `./scripts/go.sh test ./internal/compiler -run '^$' -fuzz FuzzCompileNeverPanics -fuzztime=5s -parallel=2` | PASS;11,793executions/6.027s |
| `./scripts/go.sh test ./internal/runtime/data -run '^$' -fuzz FuzzTypedFilterNeverEmitsCallerSQL -fuzztime=5s -parallel=2` | PASS;47,107executions/6.030s |
| `git diff --check` | PASS |
| `npm --cache .local/cache/npm exec --yes --package=prettier@3.6.2 -- prettier --write apps/web/src/preview/Workspace.tsx apps/web/src/preview/contracts.ts apps/web/src/preview/Workspace.test.tsx apps/web/src/api/preview.ts apps/web/e2e/preview.spec.ts apps/web/playwright.config.ts` | PASS; pinned formatting tool, no project dependency added |

Go1.27.2 and Node24.19.0 remain pinned; Chromium154 is the tested system browser.
Final cumulative evidence is recorded in M4_M6_COMPLETION_REPORT.md.

M6 STATUS: PASS. Independent review finalized with no unresolved findings; locked
setup and final fullchecks/tests/build passed. M7 is not started.
