# API, Command and Query Pattern

Pipeline:
authenticate → derive tenant → rate limit → policy → validate → command/query → transaction → audit/trace → response.

Writes are explicit typed commands.

Dynamic behavior means metadata selects an approved command/query plan; it does not mean a generic unrestricted `execute anything` endpoint.

Retryable external write APIs use idempotency keys.
