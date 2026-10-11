# Model Gateway

Centralizes provider adapters, structured output, redaction, budget and telemetry.

Request includes:
task type, privacy class, tenant policy, context bundle, output schema, budget.

Response includes:
structured payload, provider/model, usage, latency, trace metadata.

Business modules never store or call provider API formats directly.
