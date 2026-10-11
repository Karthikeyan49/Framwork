# Tool Registry

Each versioned tool declares:
ID, input/output schemas, required permissions, side-effect class, sandbox mode, approval policy, timeout, retry policy, audit policy, tenant scope.

Gateway:
resolve version → authorize → validate → inject secret reference → idempotency → execute → normalize → trace/audit.

Agents never receive direct external credentials.
