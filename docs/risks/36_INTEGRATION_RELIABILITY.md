# Integration Reliability

Every adapter defines:
auth, mapping, timeout, retry classification, idempotency capability, rate limit, circuit breaker, webhook verification, sandbox mode and normalized errors.

Persist webhook inbox IDs/body hashes before processing for dedupe/replay.

Metadata stores secret references, not plaintext credentials.

## Verification
Provider outage, duplicate webhook, out-of-order webhook, rate-limit response, expired credentials, schema change fixture.
