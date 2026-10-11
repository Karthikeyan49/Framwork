# Dynamic Query Performance

## Failure mode
Generic metadata creates large joins, scans or N+1 queries.

## Solution
Use a typed Query AST; never raw tenant SQL.

Controls:
- max relationship depth
- max selected fields
- mandatory pagination
- query timeout
- row/scan budget
- aggregation limits
- query fingerprints
- slow-query telemetry
- metadata-version-aware cache
- compiler query-risk diagnostics

JSONB custom fields use GIN/expression indexes only where query patterns justify them. Promote heavily used custom fields to indexed typed storage.

## Verification
Load tests, p95/p99, EXPLAIN review, worst-case fanout cases.
