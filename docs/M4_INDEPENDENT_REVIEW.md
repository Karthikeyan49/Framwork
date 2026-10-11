# M4 independent adversarial review

- Date: 2026-10-11
- Scope: M4 — Compiler V1
- Reviewer: independent Codex review agent under `AGENTS.md` and
  `docs/agents/82_TWO_AGENT_PROTOCOL.md`
- Result: **PASS** for the implementation and independently executed checks below.
  No unresolved BLOCKER/HIGH finding remains.

The user authorized M4–M6 in one stretch. This review covers M4 only and does not
approve an unreviewed runtime or UI. M2/M3 are the existing baseline. The reviewer
made no implementation or repository test edits; this report is the reviewer-owned
repository change. Temporary test overlays were written under `/tmp`.

## Scope and boundaries

Reviewed the compiler, private immutable Program, typed IR, catalog/schema/version
identity, symbol and exact field ownership checks, expressions, relationships,
components, workflow graph, tool declarations, diagnostics and protected compile
API against the roadmap, canonical IR/compiler/rule/workflow/tool specifications,
compiler verification guidance and ADR-017.

Only validated M3 Source can produce a Program. Failure produces diagnostics and
no trusted Program. Returned JSON, IR and entity plans are copies. Hashes include
normalized typed source, compiler/IR versions, trusted catalog contents and the
fixed preview schema snapshot; compilation depends on no clock, AI or database.
Set collections normalize while UI children, expression arguments and workflow
branch order retain their meaning.

The reserved exact identifier field maps explicitly to the platform record UUID.
Relationships require owned reference fields, a core identifier target and bounded
many-to-one cardinality. Cross-entity expressions, unavailable capabilities,
effectful tools without safe declarations and non-total/cyclic workflow graphs
fail closed. The HTTP operation resolves live membership and central policy,
retrieves the immutable source revision, and commits audit before returning IR.
It leaves the source status `draft` and does not activate anything.

## Findings resolved during review

1. **MEDIUM — incomplete canonical set normalization.** Reversing only an enum
   value set or approval-role set changed the IR hash. Derived entity field plans
   sorted enum values, but the embedded application retained the original order.
   Both embedded enum values and approval roles now sort. Independent overlays
   confirm both reorderings preserve the hash; the repository regression is
   `TestCanonicalSetsAndTotalWorkflowBranches`.
2. **MEDIUM — guarded workflow dead end.** An approval state whose sole outgoing
   transition had literal condition `false` produced a valid Program. Reachability
   alone did not establish a completion path. Each nonterminal state now requires
   exactly one final unconditional fallback. The same independent probe now
   returns `missing_fallback` and no Program; the repository regression covers it.
3. **MEDIUM — unstable capped diagnostics.** An invalid tool payload with 50
   properties, mixed nesting depths and four missing required names per object
   produced ten distinct diagnostic sets over 100 compiles of identical inputs.
   Map traversal happened before truncation at 100. Payload properties now traverse
   in sorted order with escaped property names in paths. The same independent
   100-compile probe produces exactly one diagnostic set after the fix. The
   repository `TestCappedDiagnosticsAreDeterministic` fixture now asserts it reaches
   100 diagnostics; its first version emitted fewer than 100 and was strengthened
   after review. The final independent compiler race run includes this correction.

No remaining security or architecture blocker was identified in the reviewed M4
scope. These observations do not constitute a general proof of correctness.

## Independently executed checks

All commands ran from `/workspace/Framwork`; services and existing credentials were
preserved. The final focused race and HTTP suites ran after the three fixes.

| Command | Result | Evidence |
| --- | --- | --- |
| `make check` | PASS | 84 source-document checksums, format, Go vet, module verification, frontend lint and typecheck; repeated after fixes. |
| `./scripts/go.sh test -race -count=1 ./internal/compiler ./internal/policy ./internal/platform/httpserver ./tests/security` | PASS | Final compiler, policy, HTTP boundary and architecture/security unit checks. |
| `./scripts/go.sh test -race -count=1 ./internal/compiler` | PASS | Repeated after the repository capped regression was strengthened; package result 1.560s. |
| `node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -run '^TestM4' ./tests/integration` | PASS | Real normal-role PostgreSQL/API authentication and tenant denial, viewer compile permission, stable IR/hash, audit evidence, draft preservation and invalid workflow rejection. Final package result 1.334s. |
| `./scripts/go.sh test -fuzz=FuzzCompileNeverPanics -fuzztime=5s -parallel=2 ./internal/compiler` | PASS | Final run: 17,828 executions, 6.199s. Earlier exploratory run also passed. |
| `./scripts/go.sh test -overlay=/tmp/m4-review-overlay.json -count=1 -run '^TestIndependentM4NormalizationProbe$' -v ./internal/compiler` | PASS, observation probe | Reproduced the initial normalization/branch findings; the probe logged behavior without asserting the unsafe behavior was correct. |
| `./scripts/go.sh test -overlay=/tmp/m4-review-overlay.json -count=1 -run '^TestIndependentM4DiagnosticOrderProbe$' -v ./internal/compiler` | PASS, observation probe | Reproduced the initial nondeterministic capped diagnostics. |
| `./scripts/go.sh test -overlay=/tmp/m4-review-overlay.json -count=1 -run '^TestIndependentM4' -v ./internal/compiler` | PASS | Final independent probes confirmed all three fixes, including 100 identical invalid compiles. |
| `git diff --check` | PASS | No whitespace errors in the reviewed working diff. |

An additional `git diff --check` attempt encountered a transient execution-server
transport disconnect before launching. The earlier completed check passed; this
infrastructure failure was not a source check failure.

The review did not independently rerun the complete application build, frontend
tests or all prior-milestone integration tests. Implementer evidence remains
separate from this table. No source check failed during this review; the observation
probes exposed real defects despite returning zero, as explained above.

## Limits and subsequent work

This is a bounded V1 preview compiler. Only schema 1.0.0 and the fixed preview
snapshot are supported. Nonnull production bases, list predicates without a
supported literal representation, executable tool capabilities and unsupported
file/action/chart components fail closed. There is no production release adapter,
customer DDL planner, workflow execution, rule evaluation, sandbox or promotion.
Migration class `NONE` describes the fixed preview snapshot, not production safety.
Generated test entries are requirements, not evidence of executed generated tests.

M5 and M6 must consume Program through public contracts and maintain the isolated
preview boundary in ADR-017. Production use remains gated by later sandbox,
verification, approval and promotion milestones.
