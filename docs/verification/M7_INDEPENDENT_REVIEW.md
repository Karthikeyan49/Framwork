# M7 Rule Engine V2 independent review

Verdict: **PASS for M7, with no unresolved BLOCKER, HIGH or confirmed MEDIUM finding.**
This does not authorize M8 or certify the complete production release lifecycle.

The reviewer read AGENTS.md, the V3 M7 prompt/roadmap, retained Rule DSL and module
contracts, ERP foundational types, M6R audit and ADR-021. Review scope includes the
pure evaluator, compiler/metadata versions, stored-plan admission, migration 00017,
scoped preview composition, frontend version compatibility and CI fuzz entry point.
Implementation and repository tests were not edited by the reviewer. Temporary
synthetic harnesses/overlays were used; the implementer separately adopted exercised
regressions. This document is the reviewer's exclusive repository deliverable.

## Findings, corrections and evidence

### MEDIUM — invalid instant offsets accepted: closed

The original `rules/value.go` instant pattern allowed arbitrary two-digit timezone
hours/minutes. Go's RFC3339 parser accepts values outside the RFC offset ranges.
Independent `.local/m7-review/main.go` demonstrated that typed literals
`2026-01-01T00:00:00+24:00` and `2026-01-01T00:00:00+00:60` compiled and evaluated,
silently normalizing to the previous day. This disagreed with the existing data
runtime's stricter timestamp validation.

The pattern now bounds time and offset hours to 00–23, minutes/seconds to 00–59 and
fractions to 1–9 digits. Permanent literal/input negatives were added. The same
independent harness now rejects both invalid offsets and preserves valid UTC input.
The rule race suite passes.

### MEDIUM — selected-record rule permission context omitted: closed

The first preview implementation authorized `rules.evaluate` before selecting the
record and supplied no RecordID/OwnerID. A real normal-role integration policy denied
evaluation of one specific record while allowing ordinary data reads and SQL scope.
The endpoint returned 200 with a result; the independent expected-403 regression
failed. This was a rule-action ABAC enforcement gap, not a bypass of SQL DataScope.

The initial coarse authorization remains, and a second enforced decision now uses
the actual scoped record ID and owner before evaluation. Final decision provenance
is audited before response. Query now selects actual ownership separately from the
creator; the retained LegacyAdapter explicitly maps legacy creator ownership.

Independent RecordID-specific and owner-specific negatives now return 403 without
result. The owner case assigns a uniquely owned fixture record from Alice to Bob,
with legitimate tenant membership and authenticated operator transaction, to verify
actual owner rather than creator. The implementer's permanent integration regression
also verifies the authorized-owner positive case. Both were independently executed.

### LOW — compiler trace provenance: closed by inspection

The metadata compile route initially always traced compiler 1.0.0, including opt-in
IR 1.1.0. It now uses the resulting Program's compiler version on success and selects
RuleCompilerVersion for a failed opt-in compilation. This small correction was
source-reviewed; a separate Collector attribute assertion was not executed.

### Additional compatibility and budget corrections

The implementer found that the frontend admitted IR 1.1.0 but returned a normalized
descriptor labeled 1.0.0. The decoder now preserves the admitted IR version while
retaining closed supported-version/component checks. The correction was inspected
and all 28 frontend tests were independently executed successfully.

Compile now enforces the same 4 MiB serialized IR bound as Restore. A synthetic
134,111-byte schema-valid source with 32 fields and repeated constant rule plans
independently exercised expansion beyond that bound: it returns no valid Program.
The initially generic serialization diagnostic was a LOW usability finding. The
final correction returns explicit `ir_budget` with the 4 MiB limit and documents
that bound in ADR-021. The independent harness and newly adopted permanent budget
regression were executed against this final correction successfully.

## Architectural conclusions

- Legacy metadata/IR/compiler 1.0.0 remain explicit and supported. Golden IR bytes
  are unchanged. New executable DSL2 plans require opt-in source schema 1.1.0 and
  compile to IR/compiler 1.1.0. Legacy rules are not silently executed under DSL2.
- Program state is serialized privately and copied for public access. Stored-plan
  restoration validates the typed tree, field ownership/type/options, authored
  expression binding, calendar snapshots, versions, timezone hash and budgets
  without invoking the whole authoring compiler or rewriting historical provenance.
  Tampering/compatibility negatives in rules, compiler and promotion race suites pass.
- Integer/Decimal/Money use exact signed fixed-point units at 18 fractional places,
  bounded magnitude and explicit half-even multiplication/division/rounding. Currency
  mismatch, unsupported currency, zero division and overflow fail. An independent
  rational oracle checked 2,000 signed Money operations; no floating-point result
  conversion is involved.
- Civil dates and UTC instants are distinct. Explicit IANA conversion, calendar
  ID/version/snapshot, holidays, weekdays/hours and fiscal starts are pinned. DSL2
  embeds timezone bytes and validates their hash, avoiding ambient ZONEINFO/files.
  The archive SHA256 independently matches
  `b2d18a7c8fa8142097a48c99609fb3c92db5ee98bc740294e57eab8ae9f94779`.
  Environment-independence, DST fold/gap, date-range and fuel tests pass. No ambiguous
  local-wall-time-to-instant conversion is offered.
- Missing optional fields become typed null; accessed missing required fields fail.
  Null equality/is_null are explicit; other null operands fail. Lazy boolean/branch/
  coalesce behavior and deterministic validation order are tested. Unknown functions,
  raw SQL, clock, network and host/tool capabilities have no expression entry point.
- AST/depth/field/list/calendar/string/output limits and shared work fuel bound
  execution. Context is used for explicit cancellation. Results are values/decisions;
  evaluation neither mutates business records nor emits outbox/network side effects.
- Development/test evaluation accepts only a record ID, derives the compiled rule
  and fields server-side, applies central rule/entity/row/field authorization, SQL
  scope, RLS and mandatory transactional audit. Production preview remains disabled.
  Actual PostgreSQL negatives cover denied action/field/scope, ownership, tenant,
  absent authentication, unknown record, inline expression injection and audit failure.
- Migration 00017 widens metadata version admission and derives the version column
  from the envelope. No business records or existing immutable sources are rewritten;
  privilege/RLS/immutability conventions remain. Deployment requires this forward
  migration and the existing operator compatibility gate, not a historical recompile.
- The Make fuzz target and CI step run all three bounded rule campaigns. No new Go
  or npm runtime dependency is needed for the engine; the timezone archive/license
  are deliberate versioned source artifacts.

## Independently executed checks

| Exact command | Result |
| --- | --- |
| `./scripts/go.sh run ./.local/m7-review` | Initial harness reproduced invalid-offset acceptance. Final harness rejects offsets, preserves valid UTC and rejects expanded IR beyond 4 MiB. |
| `./scripts/go.sh test -race -count=1 ./internal/runtime/rules ./internal/compiler ./internal/metadata ./internal/promotion` | PASS after instant correction. |
| `./scripts/go.sh test -race -count=1 ./internal/runtime/rules ./internal/compiler ./internal/metadata ./internal/promotion ./internal/runtime/data ./internal/policy` | PASS final six-package rerun; includes adopted Money oracle and stored-plan/legacy compatibility negatives. |
| `./scripts/go.sh test -race -count=1 -run '^TestM7OversizedApplicationIRHasBudgetDiagnostic$' -v ./internal/compiler` | PASS after final explicit budget diagnostic correction. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m7-review-integration-overlay.json -run '^TestIndependentM7RecordRulePermission$' -v ./tests/integration` | FAIL before correction: actual HTTP 200/result instead of 403. PASS after correction. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -overlay /tmp/m7-review-integration-overlay.json -run '^TestIndependentM7' -v ./tests/integration` | PASS corrected record and actual-owner negatives, 1.805s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^(TestM7\|TestIndependentM7)' -v ./tests/integration` | PASS actual repository tests, 3.368s: real threshold/calendar execution, no mutation/events, audit, isolation and record/owner authorization. |
| `./scripts/go.sh test -race -count=1 -overlay /tmp/m7-review-property-overlay.json -run '^TestIndependentM7MoneyRationalOracle$' -v ./internal/runtime/rules` | PASS 2,000 signed operations against rational oracle, 3.734s. Later adopted into repository and included in final race rerun. |
| `./scripts/go.sh test -run '^$' -fuzz '^FuzzRuleRestoreAndInputs$' -fuzztime=6s ./internal/runtime/rules` | PASS 9,878 executions, 7.044s. |
| `./scripts/go.sh test -run '^$' -fuzz '^FuzzRuleCompile$' -fuzztime=5s ./internal/runtime/rules` | PASS 90,696 executions, 5.293s. |
| `./scripts/go.sh test -run '^$' -fuzz '^FuzzFixedDecimalMoney$' -fuzztime=5s ./internal/runtime/rules` | PASS 18,367 executions, 6.011s. |
| `npm --prefix apps/web test` | PASS 28 tests in 5 files, 4.55s. |
| `sha256sum internal/runtime/rules/tzdata/zoneinfo.zip` | PASS expected pinned archive checksum. |
| `git diff -- contracts/jsonschema/application-v1.schema.json internal/compiler/testdata/vendor.ir.json` | No changes to legacy schema/golden files. Golden compatibility was also executed by compiler tests. |
| `node scripts/verify-docs.mjs` | PASS 84 retained V2 and 43 V3 checksums. |
| `git diff --check` | PASS. |

An initial copied integration overlay failed to compile on the implementer's then
unfinished unused fixture variable; only the temporary copy was corrected before
the actual permission reproduction. The first owner-only custom-policy probe also
matched an empty blocked RecordID at coarse authorization and was vacuous. Its
predicate was corrected to require a nonempty blocked ID, then the owner probe was
rerun successfully; the original RecordID failure/closure remains valid. Neither
harness issue is hidden or treated as independent safety evidence.

## Remaining scope and verification limits

No unresolved BLOCKER/HIGH/MEDIUM defect was reproduced in the reviewed M7 scope;
the confirmed LOW trace/budget findings are also closed. Short fuzz campaigns
provide exercised adversarial evidence, not exhaustive mathematical or
resource/performance proof.

This reviewer did not run the full repository CI/build/browser/live-smoke sequence
for M7, perform a real production migration/deployment or independently inspect
every database preservation checkpoint again. Remote CI execution and real production
certification are NOT RUN, not inferred from local Make/CI configuration. Broad
local results must remain attributed to the implementer. Targeted real PostgreSQL
tests and retained legacy golden checks were run independently. No API/Vite server
was started or left running by this review.

The operator compatibility gate, exact release approval and stored rule restoration
are foundation contracts. Public production rule/promotion orchestration, business
acceptance execution, workflow/tool delivery, inbox/dispatch and ERP posting remain
later milestones. Existing records are not automatically reinterpreted under a new
rule/metadata revision. The embedded timezone snapshot must be preserved under this
DSL contract; future timezone/toolchain semantic changes require explicit version
review and compatibility evidence.

**M7 REVIEW STATUS: PASS. Stop after M7; M8 requires a new authorized task.**
