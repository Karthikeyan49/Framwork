# ADR061 — Bounded admission before authentication and database work

19 October 2026 (Asia/Kolkata). Recorded before implementation. Scope: R7a,
independent foundation work pulled forward from R7 while R6's M11–M14 authority
integration remains open. No M10, production promotion, external provider, main
merge, schema/data change or dependency addition. Existing R5 checkpoint4f01dfd
is clean and remotely verified before this task.

## Problem and authority

API_DB_CONCURRENCY is a fair concurrent database gate, not a rate limit. Today an
unauthenticated request can enter it, open a transaction and append one immutable
`authentication.denied` audit row. Login quotas protect only the PKCE flow.
Bound abuse before expensive signature/database/audit work without caching tenant
membership, authorization, role versions or bearer validity.

## Admission profile

One API instance owns bounded peer and signed-subject token buckets and global
request/invalid-authentication budgets. Initial trusted profile: global1000/s,
burst2000; peer and verified issuer/subject300/s, burst600; invalid-authentication
peer1/s burst20 and global10/s burst50. Trusted operator configuration may tune
the general rate/burst within explicit bounds; invalid-authentication bounds,
key-space caps and cleanup stay fixed in this version. Limits are enabled by
default, including zero-valued programmatic API configuration; no disable flag.

Peer keys use the actual socket RemoteAddr, canonicalized IP (unmapping IPv4)
and port removal. Ignore Forwarded/X-Forwarded-For and bearer tenant/role claims.
Invalid/unknown peers share one fail-closed key. Signed subjects use a length-
delimited issuer/subject SHA-256, never bearer bytes. Peers/subjects each cap at
8192 tracked entries; idle keys expire after two minutes, longer than maximum
refill time. Bounded incremental cleanup never evicts active entries to admit
new keys. When full, new keys fail closed. No request-owned goroutines, timers,
unbounded queues or per-request map scans. No request payload/credential storage.

Global and peer checks precede cryptographic verification and body processing.
Verify a bearer before choosing the signed-subject/invalid budget. Admitted
signature failures retain existing denied audit behavior. Excess failures return
429 before PostgreSQL, audit and repository work. Here invalid means malformed
or signature/claim/time-invalid bearer authentication. Signed-valid but
unprovisioned/revoked users still require live database resolution and use the
general peer/subject/global budgets; there is no cached negative membership.
Valid tokens are reverified at
transaction execution (and each known-abort retry), preserving expiration checks
after database queue waits. No authenticated membership or policy result cache.
All admitted protected reads/writes retain live authorization and atomic audit.

429 uses the standard safe error envelope, server request ID, no-store and an
integer Retry-After. It never implies a write committed and must not cause any
automatic administrative/command POST retry. CORS exposes Retry-After. Keep
health/readiness and provider login on their documented separate contracts;
readiness needs network/operator protection and login retains its own quotas.

## Evidence and operating boundary

Record all rate rejections in low-cardinality OTel counters by closed reason,
without IPs, subjects, tenant claims, URLs, headers or payloads. Aggregate HTTP429
access logs to at most one summary per10 seconds per handler, including first
occurrence; preserve normal per-request logs for admitted requests. Suppressed
429s must not turn into another immutable audit/log flood. OTel queue/export
loss and process restart do not constitute durable forensic retention. This
slice does not redesign existing business/authorization audit retention.

This is process-local admission, not a distributed/global cluster quota,
authentication proof, policy decision, DDoS shield or production abuse capacity
certification. Behind a proxy all clients may share its socket key: do not trust
client-supplied forwarding headers to escape it. A real gateway needs separately
verified edge quotas, trusted-network design and deployment sizing. Multiple
API replicas multiply these local budgets. No Redis widening or new secret.

## Acceptance and recovery

Use deterministic monotonic-clock tests for refill/Retry-After, global/peer/
subject separation, clock regression, canonical addresses, bounded cardinality,
expiry, parallel admission, invalid tokens and observer failure containment.
Execute populated real-PG tests showing rejected requests perform no signature
verification (peer/global) or transaction/audit (all exhausted gates), rotate
untrusted forwarding headers/tokens without bypass, preserve first denied audit,
recheck valid-token expiry after queuing and preserve live revocation/known-abort
semantics. Test the actual HTTP stack's redacted/bounded429 log and response.
Independently review stable code and fix findings before final full checks/build,
application/sign-in browsers and unchanged reference read/write gates.

No existing migration or data is modified. Rollback removes this admission layer
only after operator assessment of the restored abuse exposure. Restart clears
local buckets; it is not an outage/retry/receipt reset mechanism. Preserve all
business/audit/command/inbox evidence. R6, driver-panic/connection/worker timing,
outbox outage budgets, audit retention/DR and other R7 work remain open.
