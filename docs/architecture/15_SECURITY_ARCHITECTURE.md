# Security Architecture

## Defense in depth
Authentication → trusted tenant context → policy enforcement → domain checks → PostgreSQL RLS → tool/workflow policy → audit.

## PostgreSQL
Normal application role:
- is not superuser
- does not have `BYPASSRLS`
- preferably does not own tenant tables
- uses RLS on tenant-scoped business tables
- uses transaction-scoped tenant context to avoid pooled-connection leakage

## AI
Prompt, retrieved documents, emails, external responses and LLM output are all untrusted input.

AI receives only an allowlisted tool catalog. Tool execution independently checks authorization and capability policy.
