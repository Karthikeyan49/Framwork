# Data Architecture

## Logical planes
**Control plane:** metadata, versions, compiler artifacts, policies, tools, releases.
**Business plane:** tenant records, workflow instances, integration state, attachments.

They may share one PostgreSQL cluster in V1 but use clear schemas/roles.

## Hybrid business data
1. typed core columns
2. JSONB `custom_data` for long-tail fields
3. indexes/generated/promoted columns for hot custom fields
4. materialized/reporting projections when justified

## Query path
Metadata → validated Query AST → compiler-resolved physical fields → parameterized SQL → application policy + PostgreSQL RLS.

Never execute raw SQL supplied by metadata or LLM output.
