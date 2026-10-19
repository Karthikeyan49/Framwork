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
| Eleven mutation checks of the guards (`docs/verification/M14_ADVERSARIAL_PLAN.md`) | each weakening was caught by at least one test; the tree was clean afterwards |
| Full integration suite, `make test`, `make test-e2e`, `make build` on the final tree | see "Final gate" |

## Threat model and limits

- **No real model.** Context quality, prompt quality, repair success rate, cost and latency are unmeasured. The
  corpus and the provider-swap comparison prove the gateway's substitutability and its handling of hostile bytes, not
  model behaviour.
- **The provider phase is clamped to 6 s** (the HTTP server's 10 s deadlines and the 3 s transaction cap): a real
  provider that answers more slowly cannot be used until an asynchronous slice exists (non-goal).
- **Closed schemas and the compiler protect structure, not meaning.** A model can propose valid, business-wrong
  logic; the control for that is M12 (verifier and independent acceptance) and M13 (approval), which M14 does not
  weaken. An AI-applied draft takes the whole path (`TestM14AdversarialAIAppliedDraftStillPassesTheReleasePath`).
- **Metadata leaves the platform** (labels, rule and workflow structure) to the configured provider; record data,
  secrets, tenant ids and tool endpoints never do. Redaction is a mechanical filter.
- **Identity is the API process's** (ADR039 residual): a compromised API can open, finish and decide proposals as any
  member whose identity it can bind; it gains no authority it did not have.
- Not exercised: daily budget rollover, several API processes, a per-item budget override over HTTP.
- Not built: asynchronous or streaming generation, embeddings, a UI, real vendor adapters, AI workflow activities.

## Independent review

NOT RUN. All evidence above is builder-side (`docs/verification/M14_ADVERSARIAL_PLAN.md` says so).
