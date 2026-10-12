# Tool delivery contracts — M6R

Delivery models native keys, reference lookup, dedupe windows and non-idempotent
providers. Retry/reconciliation rules keep timeout and crash outcomes unknown until
resolved; successful effects are not replayed. These are admission/decision contracts,
not external connectors or exactly-once guarantees. M10 implements governed execution;
M17 adds connectors. See ADR-020 and delivery_test.go.
