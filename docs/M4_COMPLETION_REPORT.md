# M4 completion report

Date: 2026-10-11. Scope: Compiler V1 only. User has authorized M4–M6 in sequence.

Implemented immutable typed Program/IR, stable symbol resolution and field ownership,
field/UI/expression type checks, bounded relationship planning, workflow graph/branch/
role/capability checks, tool permission/side-effect safety, canonical source/catalog/
schema/compiler hashing, structured diagnostics and generated test requirements.
A policy-protected read-only compile API emits IR or422 without activating sources.
ADR-017 documents reserved core ID mapping, fixed preview storage, no production base
adapter, no source execution and explicit unsupported capabilities.

Important files: internal/compiler/{compiler,expression,workflow,compiler_test}.go,
testdata/vendor.ir.json, public API/policy composition, contacts example, ADR-017.
No new dependency or database migration. Original buildkit is preserved.

Actually executed: make fmt; ./scripts/go.sh test -race ./internal/compiler;
./scripts/go.sh test -race ./internal/compiler ./internal/policy;
./scripts/go.sh test -race ./internal/compiler ./internal/policy ./internal/platform/access;
node scripts/with-env.mjs ./scripts/go.sh test -race -tags=integration -count=1 -v ./tests/integration -run TestM4;
make check; compiler fuzz smoke (exact command/results below).

Golden IR/reordered-input determinism, catalog hash sensitivity, copied-output safety,
reference/core/expression/component/graph/effect negatives and exact overlapping entity
ownership pass. Real API test covers authentication, foreign tenant denial, viewer
compile, stable hash, mandatory audit, invalid graph422 and unchanged draft state.
No production activation or migration rehearsal is claimed. Canonicalization preserves
ordered component trees/expressions/branches; set definitions and graph states sort.
The golden was generated once as an expected artifact and is checked normally, never
rewritten by tests. No compiler diagnostics indicate successful executable IR.

Schema upgrades, released production schema/base diffing, custom capability providers,
full tool adapters, complex fanout and list literals remain unavailable and reject
rather than bypass validation. Generated-test plans are requirements, not executed
business scenarios. Aggregate performance/production readiness remains later work.

Independent review is recorded in M4_INDEPENDENT_REVIEW.md before M5 acceptance.
All M2–M6 changes will be committed/pushed together after final user-authorized work.

Verification results: make check PASS; focused compiler/policy race tests PASS;
real TestM4 compile API PASS; compiler fuzz command
`./scripts/go.sh test ./internal/compiler -run '^$' -fuzz FuzzCompileNeverPanics -fuzztime=5s -parallel=2`
PASS with47,500 executions/6.031s. Independent review found three MEDIUM issues:
actual-source enum/approval set order affected hashing, conditional states lacked a
total fallback, and capped payload-schema diagnostics used map traversal order.
Fixed all three, added permanent set/fallback/capped-diagnostic regressions and
reran compiler race and real API tests. Review verifies these fixes separately.
Production activation, provider/tool execution, released-schema migrations and
load/DR tests NOT RUN and unavailable at this milestone.

M4 STATUS: PASS. Independent review finalized PASS in M4_INDEPENDENT_REVIEW.md;
all three findings fixed and verified. No later milestone was included in M4 scope.
