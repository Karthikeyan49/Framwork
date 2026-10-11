# Technology Stack

## V1
### Frontend
TypeScript + React.

### Backend
Go. Pin a supported patch release and upgrade regularly.

### Database
PostgreSQL 18.x supported patch line. Use RLS for tenant-data backstop. Use JSONB selectively for flexible fields.

### Redis
Cache, rate limiting, short-lived coordination and optional Redis Streams. Never source of truth.

### Object storage
S3-compatible.

### Observability
OpenTelemetry instrumentation/collector.

### AI
Provider-independent gateway; structured-output contracts.

## Later only if justified
- Rust: WASM host or measured CPU hotspot.
- Python: forecasting/optimization/specialized ML.
