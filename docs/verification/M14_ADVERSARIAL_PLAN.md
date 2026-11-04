# M14 adversarial plan and builder-side evidence

Status: **builder-side evidence, not an independent review.** The attacks below were written against the
contract of ADR040 by the same lineage that built it. An independent review of M14 has not been run. Nothing
here is evidence about any real model: no real model, provider endpoint or credential was available.

Executed 2026-10-19 on the merged tree (M13 lineage + M14 line), integration tests against the isolated M14
stack, `-race`, one process at a time.

## Attacks and results

| ID | Attack | ADR040 | Evidence | Result |
|---|---|---|---|---|
| A-01 | The 412-item corpus (direct injection 36, indirect injection 30, exfiltration 28, hostile output 275, resource 27, golden 16) through strict parsing, `ApplyProposal`, `BuildContext` and the gateway with the scripted provider: nothing hostile reaches a returned document without passing the allowlist, the per-target hash check and the compile | 4–7, 13 | `TestAdversarialCorpus` | PASS |
| A-02 | 62 corpus items through the real routes (two skipped: a per-item budget override has no HTTP form, or the registry refused the draft) | 9, 12, 13 | `TestM14AdversarialCorpusEndToEnd` | PASS |
| A-03 | Properties of every run: determinism of hashes, budgets never exceeded, no secret or canary text in context, concurrency under `-race` | 4, 7, 11 | `TestPropertiesOfEveryRun`, `TestRunsAreDeterministic`, `TestCorpusUnderConcurrency` | PASS |
| A-04 | Provider swap: the same 145 items through the scripted provider and through the HTTPS-JSON provider against a local TLS server give identical validation outcomes | 14 | `TestProviderSwapEvaluation` | PASS (substitutability of the gateway only; says nothing about model quality) |
| A-05 | Architecture: only `internal/platform/access/ai*.go` and one wiring file import `internal/ai`; deterministic packages never do; no vendor SDK; no HTTP client outside the provider file; no `database/sql` outside the store; nothing prints or logs (ARCH-01..10) with negative tests that feed a violating tree | 2 | `internal/ai/architecture_test.go` | PASS |
| A-06 | An AI-applied draft still has to pass sandbox, candidate build and verification: a sensitive change without independent acceptance still fails | 9 | `TestM14AdversarialAIAppliedDraftStillPassesTheReleasePath` | PASS |
| A-07 | Routes: policy matrix (owner, admin, member, viewer, custom roles, outsider, anonymous), strict bodies on every route, 503 when unconfigured before anything happens, acknowledgement of the server-derived sensitive list, apply against the current draft, state machine (double apply, apply after reject), requirement capture and human confirmation, NEEDS_HUMAN and FAILED as stored results, one transient retry, budgets and in-flight limit, context cap, audit without prompt or model text, tenant and application isolation, no model call on ordinary paths | 9–12 | `TestM14API*` (17 tests) | PASS |
| A-08 | Ports: caller authority, malformed arguments, admission under concurrency, expiry, finish/decide state machine, append-only tables, RLS, grants, the derived worker guard | 8 | `TestM14Ports*` (5 tests) | PASS |
| A-09 | HTTP provider: redirect (including to the same origin), request and response caps, timeout, private-range dial refusal, credential not retained | 3 | `internal/ai/provider_http_test.go` | PASS |
| A-10 | Deadlines: an 11 s scripted answer behind a real `http.Server` built by `httpserver.New` (10 s read and write timeouts) still gives a stored READY proposal; a client that gives up during the provider phase leaves one `FAILED`/`INTERNAL` proposal, one provider call and a free in-flight slot | 5, 8 | `TestM14APIProviderPhaseOutlivesTheServerWriteTimeout`, `TestM14APIClientGoneDuringProviderPhaseStillRecordsTheOutcome`, `TestExtendWriteDeadline*`, `TestEffectiveWallClock*` | PASS (13.2 s and 3.8 s) |

## Mutation checks

Each guard was weakened in the source, the tests that cover it were run, and the file was restored (the tree was
clean afterwards). A guard whose weakening no test notices is not a control.

| Mutation | Tests that failed |
|---|---|
| Kind allowlist admits `tool` (`kinds.go`) | 10 (`TestAllowlistCoversEveryDefinitionKind`, `TestApplyProposalRefusesEveryNonAllowlistedKind`, `TestAdversarialCorpus`, …) |
| Duplicate keys accepted by the strict decoder (`schema.go`) | 4 (`TestParsePatchProposalRejects`, `TestAdversarialCorpus`, …) |
| Redaction is the identity (`redact.go`) | 5 (`TestRedactTextTable`, `TestBuildContextRedactsSecretShapedTokens`, …) |
| Attempts ceiling 3 → 30 (`contract.go`) | 5 (`TestRunPatchBudgets`, `TestLoadProviderConfigLimits`, …) |
| Apply without the server-derived acknowledgement (`service_decide.go`) | 2 (`TestM14APIApplyRequiresTheServerDerivedAcknowledgement`, `TestM14AdversarialCorpusEndToEnd`) |
| Apply without comparing the draft revision (`service_decide.go`) | 1 (`TestM14APIApplyRevalidatesAgainstTheCurrentDraft`) |
| Decide port ignores the READY state (migration 00060) | 2 (`TestM14PortsCallerAuthority`, `TestM14PortsFinishAndDecideStateMachine`) |
| Daily token budget not enforced (migration 00060) | 2 (`TestM14APIAdmissionBudgetsAndLimits`, `TestM14PortsAdmission`) |
| Viewer may propose and apply (`policy.go`) | 4 (`TestM14APIPolicyMatrix`, three policy unit tests) |
| Unknown body fields accepted (`access/ai.go`) | 1 (`TestM14APIBodiesAreStrict`) |
| A deterministic package imports `internal/ai` (`internal/observability`) | 2 (`TestArchitectureAIBoundary` ARCH-01, `TestArchitectureAIImporters`); for packages that `internal/ai` itself depends on the Go compiler already rejects the import as a cycle |
| The route does not extend the write deadline for the provider phase (`access/ai.go`, `nextWithin` removed) | 1 (`TestM14APIProviderPhaseOutlivesTheServerWriteTimeout`: the request ends in EOF after 11.05 s) |

## Not executed

- Any real model, any real provider endpoint, any vendor credential. Prompt quality, repair success rate,
  latency and cost are unmeasured.
- A provider phase of 60 s (the longest the API allows; the longest executed is 11 s), a real proxy or load balancer in front of the API, and many concurrent long provider phases (ADR040 A5, A12).
- Daily budget rollover (the clock is not injectable end to end); several API processes sharing one database.
- A per-item budget override over HTTP; load or timing tests of the provider phase; browser or frontend.
- An independent review of M14 and independent re-verification of this evidence.
