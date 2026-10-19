# Tool Delivery Semantics

Each tool declares:
- side_effect_class
- idempotency_mode
- retry_mode
- reconciliation_mode
- compensation capability

`idempotency_mode`:
- NATIVE_KEY
- LOOKUP_BY_REFERENCE
- DEDUPE_WINDOW
- NONE

Possible outcomes include:
SUCCESS, FAILED, UNKNOWN_REQUIRES_RECONCILIATION.

Do not promise universal exactly-once external effects.
