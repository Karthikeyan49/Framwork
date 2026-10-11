# Workflow Reliability

## Failure mode
Crashes, retries or long timers lose or duplicate business operations.

## Solution
Separate your business workflow DSL from infrastructure durability.

Business Workflow Definition
→ Workflow Runtime Port
→ durable implementation.

Capabilities:
- durable history/state
- timers
- retry/backoff
- cancellation
- timeout
- DLQ/manual recovery
- idempotent activities

Evaluate Temporal for production durability instead of recreating all failure semantics yourself.

## Verification
Crash before action, crash after effect/before ack, duplicate message, timeout, cancellation, retry exhaustion.
