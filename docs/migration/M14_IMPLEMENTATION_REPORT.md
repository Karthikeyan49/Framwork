# M14 AI Builder and Intent Capture — implementation report

Date: 19 October 2026 (UTC). Source: `claude/m14-ai-builder` (built on the M13 lineage). Decisions, amendments and
recovery are in [ADR040](../adr/040-m14-ai-builder-and-intent-capture.md). **M14 is a development/test preview
capability. No real model was called, no provider endpoint or credential was available, and the independent review
of M14 has not been run.** It is not production ready.

## What changed

A model holds no authority and has no identity. Its output is parsed into a closed, typed, hashed proposal that an
authenticated human applies, confirms or rejects through an ordinary endpoint.

- **`internal/ai`** (the module that was reserved since M0): a provider port with a deterministic scripted provider
  and one generic HTTPS-JSON provider (guarded dialer, no redirects, bounded bodies, credential read per call); a
  metadata-only, deterministic, capped and redacted context bundle with `UNTRUSTED` segments; versioned templates;
  closed output schemas with strict decoding; `ApplyProposal` (kind allowlist, per-target expected-hash check,
  server-derived sensitive aspects from `compiler.ComputeDiff`); a bounded compile-and-repair loop with budgets and a
  per-attempt trace; `Evaluate` for the provider-swap comparison; `ai-provider-v1` configuration (`AI_PROVIDER_FILE`).
- **Migration 00060**: `control.ai_proposals`, `ai_invocations`, `ai_usage` (forced RLS, no DML for the runtime role)
  and the definer ports `ai_begin`, `ai_record_attempt`, `ai_finish`, `ai_decide` (authority from the leased session,
  budget and in-flight limit, expiry sweep, status compare-and-set, hash-bound stored body). Raw prompts and model
  output are never stored. The worker drift guard is not replaced (ADR040 Amendment B.7).
- **API** (`internal/platform/access/ai.go`, preview-gated, strict bodies): create patch and requirement proposals,
  list/get, `apply` (re-validated against the current draft, server-derived acknowledgement, appended as a new metadata
  revision by the authenticated human), `confirm` (requirements with provenance `AI_PROPOSED_HUMAN_CONFIRMED`),
  `reject`. Policy `m14-native-v11` (`ai.read`, `ai.propose`, `ai.apply`). Unset provider → every AI route answers 503
  and nothing else changes. Audit carries hashes and closed codes only.

## Commands executed and results

On the merged tree (M13 lineage with migrations 49–53 and the M14 line with migration 60), 2026-10-19, one process at a
time, integration tests against the isolated M14 stack reset from this tree.

| Command | Result |
|---|---|
| `go build ./...`, `go vet ./...`, `go vet -tags=integration ./tests/integration`, `gofmt -l`, `node scripts/verify-docs.mjs` | PASS |
| `go test -race ./internal/ai/... ./internal/policy ./internal/platform/... ./internal/audit ./internal/tools` | PASS (`internal/ai` takes about 200 s under `-race`, 24 s without) |
| `TestM14` integration group (API, ports, adversarial; 23 tests), `-race` | PASS, 92 s |
| `TestM12`, `TestM13` groups on the M14 stack, `-race` | PASS, 251 s |
| Twelve mutation checks of the guards (`docs/verification/M14_ADVERSARIAL_PLAN.md`; the twelfth removes the write-deadline extension) | each weakening was caught by at least one test; the tree was clean afterwards |
| Full integration suite, `make check`, `make test-unit`, `make build`, `make test-e2e` on the final tree | see "Final gate" below |

## Final gate

Run on 2026-10-19 on `claude/m14-ai-builder` (code identical to commit `8a8d34d`; later commits change documentation only),
against the isolated M14 database stack reset from that tree.

| Command | Result |
|---|---|
| `make check` (docs checksums, format, vet, `mod verify`, web lint, typecheck) | PASS |
| `make test-unit` (Go unit tests `-race` over every package, `node --test scripts/*.test.mjs`, web `npm test`) | PASS (5 script tests, 29 web tests; `internal/ai` takes about 212 s under `-race`) |
| Full `go test -race -tags=integration ./tests/integration`, skipping `TestM8RedisACLStreamLossAndPendingRecovery` | 1714 s; exactly two failures, `TestM1DevelopmentIssuer` and `TestAPIWithoutCollectorAndDependencyOutages`: the worktree had no local development identity keys (`.local/identity`, ignored by git). Both PASS (7.6 s) once the keys were provided; no other test failed |
| `make build` | PASS |
| `make test-e2e` (5 browser tests) | PASS, 21.6 s |
| `TestM8RedisACLStreamLossAndPendingRecovery` | NOT RUN: it restarts the shared Redis and fails on isolated stacks by design |

## Follow-up: provider-phase wall clock lifted from 6 s to 60 s (2026-10-19)

The synchronous provider phase was clamped to 6 s because `httpserver.New` ends a response at 10 s. It now follows
the contract (`ai.APIWallClockCeiling` = `DefaultWallClock`, 60 s) and the route extends the write deadline of its own
connection (`reply.nextWithin`, `http.NewResponseController`; ADR040 A5 and Amendment B.9). Run on the isolated M14
stack reset from the changed tree, one process at a time. This change was gated again on the merged tree (section "Final gate, re-run" below).

| Command | Result |
|---|---|
| `gofmt -l` on tracked Go files, `node scripts/verify-docs.mjs`, `go vet ./internal/ai/... ./internal/platform/...`, `go vet -tags=integration ./tests/integration` | PASS |
| `go test -race ./internal/platform/access ./internal/platform/httpserver` (new: `TestExtendWriteDeadline*`) | PASS, 3.3 s and 1.1 s |
| `go test -race ./internal/ai/...` (new: `TestEffectiveWallClock*`) | PASS, 193 s |
| `TestM14APIProviderPhaseOutlivesTheServerWriteTimeout`, `-race` (11 s scripted answer behind a real `httpserver.New` server) | PASS, 13.2 s |
| Same test with `nextWithin` removed from `access/ai.go` | FAIL as intended: EOF after 11.05 s; the file was restored |
| `TestM14APIClientGoneDuringProviderPhaseStillRecordsTheOutcome`, `-race` | PASS, 3.8 s: the proposal ended `FAILED`/`INTERNAL`, one provider call, the slot was free again |
| `TestM14` group (25 tests), `-race` | PASS, 109 s |
| `TestM12`, `TestM13` groups, `-race` | PASS, 251 s |
| After merging the M13R-03 fix (ADR039 amendment 13): `go vet ./...`, `go vet -tags=integration ./tests/integration`, `gofmt -l`, `node scripts/verify-docs.mjs`, `go test -race` of `internal/policy`, `internal/promotion`, `internal/platform/...`, `internal/tenant` | PASS |
| After the same merge, M14 stack reset from the merged tree: `TestM14`, `TestM13`, `TestM12` groups together, `-race` | PASS, 381 s |

## Final gate, re-run after the 60 s change and the M13R-03 merge

Run on 2026-10-19 on `claude/m14-ai-builder` at `3987baa` (code identical to `c05d769`; later commits change documentation
only), against the isolated M14 database stack reset from that tree. The development identity keys were linked into the
worktree, so the two environmental failures of the first run did not recur.

| Command | Result |
|---|---|
| `make check` (docs checksums, format, vet, `mod verify`, web lint, typecheck) | PASS, 28 s |
| `make test-unit` (Go `-race` over every package, script tests, web tests) | PASS, 208 s |
| Full `go test -race -tags=integration ./tests/integration`, skipping `TestM8RedisACLStreamLossAndPendingRecovery` | PASS, 1653.7 s, no failure |
| `make build` | PASS, 18 s |
| `make test-e2e` (5 browser tests) | PASS, 25.3 s |
| `TestM8RedisACLStreamLossAndPendingRecovery` | NOT RUN (restarts the shared Redis; fails on isolated stacks by design) |

## Gap-analysis follow-ups closed on this line (2026-10-19)

`docs/verification/FOUNDATION_GAP_ANALYSIS.md` left three M10 to M13 follow-ups at PARTIAL. They are closed here; the
audit document itself is not edited (its refresh belongs to the remediation line).

| Item | What changed | Tests |
|---|---|---|
| N-13 policy actions `tools.invoke`, `sandbox.read`, `sandbox.manage` had no test that names them | the built-in matrix, the resource shapes, tenant and anonymous refusals and the custom-role rules are pinned: `tools.invoke` for any role but viewer, `sandbox.read` for every role, `sandbox.manage` for no custom role | `TestToolInvokeCapability`, `TestSandboxCapabilities`, `TestToolAndSandboxGrantsInCustomRoles`; mutation checks (viewer may invoke, member may manage the sandbox) fail them |
| N-11 `Delivery.MayRetry` had no production caller and its test pinned a rule that production does not use | removed; the retry rules live in `decideTool` over the durable ledger, which has its own table test; the package comment and README no longer say that no tool executes | `TestDeliveryDeclarationsAreValidatedForTheirRisk`, `TestDecideToolRoutesFreshOutcomesAndRefusesBlindResends` |
| N-06 `releaseGate` answered true unconditionally | the gate carries the request's own `candidates.build` decision, obtained at the point of use, and opens only for it, for the same tenant and actor and for the stage action | `TestReleaseGateOpensOnlyForTheRequestsOwnDecision` and every candidate-building integration test |

Still open: N-03 / N-22 (a worker inside its lease can set status, termination and failure of its instance; ADR024
residual, a trigger would have to encode the plan's terminal states), N-07 (the migration 48 comment cannot change;
ADR039 amendment 6 states it), N-12 (a separate verifier credential) and N-15 (`runtime.unique_projections` has no writer;
the remediation line replaces it). Run on the M14 stack reset from the changed tree: `go vet ./internal/...`,
`go vet -tags=integration ./tests/integration`, `go test -race` of `internal/policy`, `internal/tools`,
`internal/platform/...`, `internal/runtime/workflow` PASS; `TestM10` to `TestM14` groups, `-race`, PASS in 1056 s. The full
suite, `make test-unit`, `make build` and `make test-e2e` were not re-run for this commit.

## Items the reviews listed as "not covered", executed here (2026-10-19)

| Review item | Executed as | Result |
|---|---|---|
| M12 API and M13 reviews: tool-pin staleness through the handlers (reviewed by reading only) | `TestM13ToolPin*` (details in the M13 report) | `TOOL_CHANGED` and `TOOL_REMOVED` at view, approve and activate; fresh again when the exact definition returns |
| M11 review: freeze racing a record write (reasoned statically) | `TestM11RFreezeRacesRecordWrites`: 12 concurrent writers through the governed command route, the sandbox frozen after 0, 20, 50, 120 and 300 ms | in every round the record count of the evidence, the rows of the dataset and the acknowledged writes are equal (0, 10, 11, 10 and 10 records); the writes that lost the race were refused with 409; 15.4 s with `-race` |

The freeze test does not mutate the freeze's locking (applied migrations are immutable), so its sensitivity rests on the
equality it asserts: a write acknowledged after the freeze snapshot, or a counted row nobody acknowledged, fails it. Only
these tests were run for this addition.

## Threat model and limits

- **No real model.** Context quality, prompt quality, repair success rate, cost and latency are unmeasured. The
  corpus and the provider-swap comparison prove the gateway's substitutability and its handling of hostile bytes, not
  model behaviour.
- **The provider phase may take up to 60 s** (the contract's wall clock). It holds no transaction and no database
  connection; the route moves the write deadline of its own connection to wall clock + 10 s (ADR040 A5, Amendment
  B.9). It holds an HTTP connection for that time and the API has no process-wide limit on such requests (the
  per-tenant in-flight limit applies); a proxy in front of the API may cut the exchange earlier, and a provider that
  needs more than 60 s needs the asynchronous slice (non-goal). Executed: an 11 s answer behind the production server
  timeouts; not executed: a 60 s phase and a real proxy.
- **Closed schemas and the compiler protect structure, not meaning.** A model can propose valid, business-wrong
  logic; the control for that is M12 (verifier and independent acceptance) and M13 (approval), which M14 does not
  weaken. An AI-applied draft takes the whole path (`TestM14AdversarialAIAppliedDraftStillPassesTheReleasePath`).
- **Metadata leaves the platform** (labels, rule and workflow structure) to the configured provider; record data,
  secrets, tenant ids and tool endpoints never do. Redaction is a mechanical filter.
- **Identity is the API process's** (ADR039 residual): a compromised API can open, finish and decide proposals as any
  member whose identity it can bind; it gains no authority it did not have.
- **This line is not integrated with the remediation line.** Both add migrations `00045` to `00053` and ADRs `036` to
  `040` with different content, 21 files conflict and the metadata contract `1.3.0` is defined twice; the merged tree
  does not start. `docs/migration/LINE_INTEGRATION_PLAN.md` records the executed trial merge and the options.
- Not exercised: daily budget rollover, several API processes, a per-item budget override over HTTP.
- Not built: asynchronous or streaming generation, embeddings, a UI, real vendor adapters, AI workflow activities.

## Independent review

NOT RUN. All evidence above is builder-side (`docs/verification/M14_ADVERSARIAL_PLAN.md` says so).
