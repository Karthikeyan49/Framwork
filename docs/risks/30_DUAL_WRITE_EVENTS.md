# Dual Writes and Events

## Problem
DB update succeeds but event publish fails, or inverse.

## Solution: transactional outbox
In one PostgreSQL transaction:
1. business write
2. outbox row
3. commit

Dispatcher publishes committed rows.
Consumers assume at-least-once delivery and deduplicate.

Event envelope:
event_id, tenant_id, aggregate type/id, type, schema version, sequence, trace ID, metadata version, payload, timestamp.

## Verification
Kill process between each stage; verify no lost committed business event and no duplicate business effect.
