# AI Provider Lock-In, Cost and Latency

Build a Model Gateway exposing task capabilities rather than provider-specific APIs.

Task classes:
metadata authoring, explanation, summarization, planning, optional embeddings.

Controls:
- tenant/request token budgets
- model routing
- context pruning
- static prompt reuse
- async authoring
- streaming UI
- no LLM call in ordinary deterministic CRUD/workflow logic

Maintain golden evaluations for validity, repair count, latency, cost and user acceptance.

## Verification
Swap model provider on the evaluation suite without changing metadata/compiler/runtime modules.
