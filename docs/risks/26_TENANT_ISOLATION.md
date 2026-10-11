# Tenant Isolation

## Trusted context
Tenant is derived server-side from authenticated identity and membership.

Never trust a browser-supplied tenant ID by itself.

## PostgreSQL RLS
Every tenant table contains `tenant_id`.
RLS is enabled as database backstop.

The normal app role:
- is not superuser
- has no BYPASSRLS
- is not table owner where avoidable

Use transaction-scoped context so connection pooling cannot leak tenant state.

## Defense layers
API tenant resolver → policy → repository tenant guard → PostgreSQL RLS.

## Verification
Cross-tenant SELECT/INSERT/UPDATE/DELETE, joins, aggregate leakage, forged headers/body, cache collision, queue context, file paths.
