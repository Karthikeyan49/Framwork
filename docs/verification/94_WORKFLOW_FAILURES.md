# Workflow Failure Tests

Inject:
worker crash before action; crash after side effect before ack; external timeout; duplicate message; out-of-order event; DB failure; cancellation; timer; retry exhaustion.

Verify no duplicate business effect, recoverable state, clear history and working manual/DLQ path.
