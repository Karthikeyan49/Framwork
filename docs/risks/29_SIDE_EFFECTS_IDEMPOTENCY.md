# Side Effects, Idempotency and Compensation

## Side-effect classes
READ_ONLY, INTERNAL_WRITE, EXTERNAL_REVERSIBLE, EXTERNAL_IRREVERSIBLE, FINANCIAL, DESTRUCTIVE.

## Idempotency
Every effectful activity uses a logical key:
tenant + workflow instance + step + business operation.

Persist a tool ledger:
key, request hash, state, external reference, response hash, timestamps.

## Compensation
Define explicit compensation only when genuinely possible.
Do not call email/payment/legal filing "rollbackable" after it happened.

High-risk irreversible actions require stronger approval.

## Verification
Replay identical request; crash after external success; retry; ensure one business effect.
