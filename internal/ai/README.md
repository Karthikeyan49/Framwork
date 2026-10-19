# ai

Model gateway, semantic context builder and proposal pipeline (ADR040, M14).

A model holds no authority and has no identity. Everything it returns is parsed into a closed, typed,
hashed proposal that an authenticated human applies, confirms or rejects. The model is a `Provider`
behind a port; nothing outside this package builds a prompt or imports an adapter (architecture rules
ARCH-01..10 in `architecture_test.go`).

| File | Responsibility |
|---|---|
| `contract.go`, `limits.go`, `errors.go` | public types, ceilings, typed errors |
| `schema.go`, `schema_text.go`, `canonical.go` | closed output schemas, strict decoding, canonical hashes |
| `patch.go`, `kinds.go` | apply with per-target hash checks, the kind allowlist, server-derived sensitive aspects |
| `context.go`, `redact.go` | metadata-only, deterministic, capped, redacted context; untrusted-segment wrapping |
| `template.go` | versioned prompt templates (system text, repair lead-in) |
| `gateway.go`, `repair.go` | the bounded repair loop, budgets, usage, per-attempt trace |
| `provider_scripted.go`, `provider_http.go`, `config.go` | deterministic scripted provider; HTTPS-JSON provider with a guarded dialer; `ai-provider-v1` loading |
| `eval.go` | evaluation harness used for the provider-swap comparison |
| `store.go`, `service.go`, `service_decide.go` | proposals, traces and budgets through the definer ports of migration 00060; admission, provider phase, human decisions |
| `corpus/`, `testdata/corpus/` | the adversarial corpus (412 items) and its interpreter |

The HTTP surface is `internal/platform/access/ai.go`. Status: preview. No real model was called; the provider
phase may take up to 60 s (`APIWallClockCeiling`): the route extends the write deadline of its own connection for it
(ADR040 A5, Amendment B.9); the longest phase executed is a scripted 11 s answer behind the production server
timeouts. Real provider latency, cost and quality are unmeasured.
