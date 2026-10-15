# Transactional Business Write + Audit + Outbox

For business mutations:

```text
BEGIN
business state change
audit evidence
outbox event
COMMIT
```

This couples committed business state to audit/event intent.

External tool lifecycle:
tool intent ledger → remote request → success / failure / unknown → reconciliation if required.
