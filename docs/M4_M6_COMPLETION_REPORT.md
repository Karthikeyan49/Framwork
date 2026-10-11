# M4–M6 delivery report

Date: 2026-10-11. Repository: Karthikeyan49/Framwork, main.
User authorized completing M4, M5 and M6 in one stretch and pushing all work.
This delivery includes the previously uncommitted M2/M3 foundation; it does not
start M7 or approve/promote any production metadata.

## 1. Summary

M4 provides typed deterministic compiler IR, references/types/capability checks,
structured diagnostics, canonical golden/hash tests and an audited compile API.
M5 executes compiler-minted plans through bounded parameterized queries and typed
CRUD, with tenant/version isolation, central policy, atomic audit, exact values,
CAS, reference integrity, keyset pages, one declared join and bounded aggregates.
M6 renders compiled IR through a versioned React registry into forms, typed fields,
tables, details and composed layouts, with safe text, loading/error/recovery states,
read-only hints, scoped cancellation and real browser verification.

Independent review is recorded separately in M4_INDEPENDENT_REVIEW.md,
M5_INDEPENDENT_REVIEW.md and M6_INDEPENDENT_REVIEW.md. Findings were fixed and
reverified, rather than treated as passing code. Milestone-specific reports preserve
initial failures, corrected expectations and exact focused commands.

## 2. Important repository structure

```text
cmd/{api,worker,migrate,iam}/
internal/{identity,tenant,policy,audit,metadata,compiler}/
internal/platform/{access,app,postgres,httpserver,cache}/
internal/runtime/data/{model,values,store,query,visibility}.go
apps/web/src/{api,preview}/
apps/web/e2e/{preview.spec,credentials}.ts
apps/web/playwright.config.ts
contracts/jsonschema/application-v1.schema.json
contracts/examples/{contacts,vendor}-draft-v1.json
internal/compiler/testdata/vendor.ir.json
db/migrations/00001–00008
Makefile, scripts/{go,bootstrap,browser-install,smoke}.*
.github/workflows/ci.yml
AGENTS.md, CLAUDE.md, README.md
docs/adr/015–019, docs/M2*–M6*
```

Existing module placeholders remain; no business rule/workflow/tool engine,
microservice, Rust/Python runtime, Kubernetes or customer-specific codebase added.

## 3–4. Commands and results

These are actual commands, not proposed checks. Focused/reviewer commands and initial
failed runs are in each milestone report. Final delivery checks are updated below.

| Command | Result/evidence |
| --- | --- |
| `make setup` | PASS; frozen dependency installation repeated successfully |
| `make infra-up migrate migration-status` | PASS; PostgreSQL18.6, Redis8.2.3 and Collector0.148.0 healthy |
| `make fmt check test build` | PASS after review fixes: format/vet/integrity, Go race/unit/security,23frontend tests, real integrations11.326s, four binaries and frontend build |
| `make check build` | PASS after final browser/redaction hardening |
| `make test-e2e` | PASS;3actual Chromium tests4.7s, CRUD/mobile/conflict/reload/paging/viewer denial and credential/header failure redaction |
| `node scripts/with-env.mjs node scripts/smoke.mjs` | PASS; health,7readiness contracts, proxy/auth/CORS, W3C correlation, collector traces/metrics and graceful shutdown |
| `make migration-status` | PASS;00001–00008 applied; fresh/repeatable migrations independently exercised |
| `./scripts/go.sh vet -tags=integration ./tests/integration` | PASS |
| `npm --cache .local/cache/npm --prefix apps/web audit --omit=dev --audit-level=high` | PASS;0 vulnerabilities reported |
| `./scripts/go.sh test ./internal/compiler -run '^$' -fuzz FuzzCompileNeverPanics -fuzztime=5s -parallel=2` | PASS;11,793executions/6.027s on final compiler |
| `./scripts/go.sh test ./internal/runtime/data -run '^$' -fuzz FuzzTypedFilterNeverEmitsCallerSQL -fuzztime=5s -parallel=2` | PASS;47,107executions/6.030s |
| `git diff --check` | PASS |
| `git ls-remote origin refs/heads/main` | PASS; remote base7d60ad5 before delivery |
| `git fetch origin` | PASS |
| `PLAYWRIGHT_BROWSERS_PATH=/workspace/Framwork/.local/cache/ms-playwright npm --cache .local/cache/npm --prefix apps/web exec -- playwright install chromium` | FAIL; official CDN denied403 by cloud egress policy; actual tests use installed system Chromium154 |

Earlier failures: M5 fixture compilation/disabled-route status expectation, owner-field
negative regression, M6 accessible label selectors, and a temporary regex-edit compile
failure were corrected and rerun. The final app-scoped compiler test initially failed
compilation due an audit-state field-name typo; corrected to existing MetadataApp.
A report-writing helper had a syntax error before writing any file; corrected without
changing product code. No failed verification is represented as passing.

NOT RUN: production promotion/schema rehearsal, later business acceptance scenarios,
load/DR certification, full accessibility certification and M7–M18 suites. These are
not implemented scope. Managed Chromium download remains failed, not implicitly passed.

## 5. Architecture decisions

ADR017: private immutable compiler Program and canonical V1 IR; fixed preview schema,
explicit unsupported capabilities and no released-base/migration approval claim.
ADR018: forced-RLS hybrid preview records, exact typed values, CAS/dataset coordination,
bounded SQL AST and unfiltered candidate visibility proof before customer predicates.
ADR019: versioned approved React renderer, safe typed UI, cancellation, uncertain-write
recovery and browser credential-log redaction. Earlier ADR015/016 ship with M2/M3.

## 6. Dependencies introduced

M4/M5 add no production dependencies. M6 adds pinned @playwright/test1.64.0 (and its
managed transitive packages) for actual browser tests; React/TypeScript remain existing
production/frontend dependencies. Earlier uncommitted M3 introduces
santhosh-tekuri/jsonschema/v6@6.0.2 for embedded JSON Schema validation and
go.yaml.in/yaml/v3@3.0.5 for restricted YAML authoring. Go manifests/checksums and npm
lockfile are committed; modules verify. Prettier3.6.2 was a pinned one-time formatting
tool, not a project dependency. No generated services or runtime language added.

## 7. Security observations

Tenant identity/membership is resolved server-side and backed by forced RLS. Policy
receives trusted app/entity/field/record/owner attributes; unknown obligations deny.
All protected responses follow mandatory audit/commit. Writes cannot succeed behind
a denied response-read policy. Preflight checks all source candidates and relevant
join targets before filter/sort/cursor execution, preventing a permission inference
oracle. App-scoped decisions also protect metadata compilation. No raw SQL, scripts,
HTML, secrets, source execution, customer privilege grants or production activation.
Unknown write outcomes/5xx and409 disable further UI mutations until successful reload.
Browser failure paths redact tokens and Authorization header values.

## 8. Known limits and recovery

Development/test preview only; production routes absent. Dataset key includes trusted
tenant/app/source revision/IR hash; new revisions start separate preview records.
Page100, projection/UI32, filter depth3/nodes32, IN32, one many_to_one join, bounded
aggregate and1,000 unfiltered candidates;3-second runtime deadline may reject requests
below these ceilings. This is not a throughput claim. Whole-query policy denial,
not field masking or partial-row pagination. References offer20choices+UUID entry.

Expected-version updates/deletes fail stale; concurrent dataset writes may return503
serialization and require reload. Unknown commit outcome requires inspection, not blind
retry. Migrations are forward-only. Dataset/IR selections remain stable; no destructive
volume reset. Date/year/timezone/fraction bounds match supported server/SQL domains.
Later indexes, trusted policy predicates, outbox/events, pure rule execution, durable
workflows, tool effects, true sandbox lifecycle, promotion and AI remain later work.

## 9. Verification against requested milestones

| Criterion | Evidence/status |
| --- | --- |
| M4 reference/type validation | PASS; negative corpus, exact field ownership and renderer compatibility |
| M4 canonical IR/deterministic hashes | PASS; golden, reorder/set/capped-diagnostic stability, catalog sensitivity, immutable-copy checks |
| M4 diagnostics and no activation | PASS; real authorized compile200/422, audit and unchanged draft state |
| M5 typed Query AST and parameterized SQL | PASS; real literal-injection tests, allowlists/bounds/cursor pins and fuzzing |
| M5 CRUD/custom fields and exact values | PASS; normal-role real creates/updates/deletes, decimal/money/date/reference negatives |
| M5 pagination/concurrency | PASS; terminal/keyset pages, stale versions, concurrent updates and reference/delete races |
| M5 tenant/version/policy/audit safety | PASS; isolated datasets, field/owner/app/row denials, filter-oracle prevention, audit rollback and fresh RLS migrations |
| M6 dynamic forms/table/detail/layout registry | PASS; compiled IR produces reusable approved components, version/compatibility negatives |
| M6 loading/errors/recovery/accessibility baseline | PASS; labels/semantic controls, empty/loading/error states, explicit conflict/unknown-outcome reload and mobile overflow check |
| M6 real end-to-end and permission negatives | PASS; browser with actual API/PG, owner CRUD and viewer API write403 |
| Managed manifests/CI/developer instructions | PASS; pinned dependencies, build/check/test/e2e commands and preserved84source checksums |
| Independent review | PASS; all three reviews finalized, including app-scoped compile policy |
| Stop before M7 | PASS; no rule executor or later milestone implemented |

## 10. Git and reusable environment

All accumulated M2–M6 source, forward migrations, schemas/examples/golden, managed
manifests, tests, CI, ADRs and reports are intended for this push. Generated bin/dist,
node_modules, .local, browser artifacts/fuzz caches, .env and identity keys remain
ignored and must not be committed. The canonical IR golden is intentional test source.
The previous remote main base is7d60ad50087a624beb53626025c5c409a58cdba6.
Commit/push and final clean status are recorded after completion, without force-push.

Cloud setup install_script/start_skill were saved as a draft with tested setup/build/
browser/start/check instructions; no credentials or network/repository membership
changes were saved. Publication is separate: review/save the changes in environment
settings and publish to reuse the updated environment. Current-instance verification
is complete; publication/new-task restoration is not claimed.

M4 STATUS: PASS
M5 STATUS: PASS
M6 STATUS: PASS. Frozen setup,23frontend tests and full real integrations15.583s
passed again after final changes; independent M6 app-scope recheck4.705s PASS.

Recommended next milestone: M7 — Rule Engine. Wait for a new user task.

Final setup command `make setup env browser-install` PASS: locked241-package npm
install, verified Go modules, preserved .env/identity keys, system Chromium selected.
Then `make check test build`, `make migration-status`, and the production npm audit
passed on the final implementation. Final source/API normal-role review remains PASS.
