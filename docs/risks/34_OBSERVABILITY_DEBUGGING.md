# Debugging and Observability

Instrument request → policy → metadata version → compiler → query → workflow → tool → integration.

Required correlation:
request/trace ID, tenant, actor/service, metadata version, sandbox ID, workflow instance, tool call, compiler version.

Use OpenTelemetry for vendor-neutral traces, metrics and logs.

Support a redacted incident bundle containing relevant trace, release version, diagnostics, workflow history and tool ledger.

## Verification
Pick a failed end-to-end transaction and prove an operator can reconstruct what happened from telemetry.
