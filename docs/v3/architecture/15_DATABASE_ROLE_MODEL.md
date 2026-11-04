# Database Role Model

- `schema_owner`: NOLOGIN table/schema owner
- `db_migrator`: controlled DDL
- `api_runtime`: tenant-scoped, RLS applies
- `job_dispatcher`: sees routing/job metadata across tenants, not arbitrary business payload
- `worker_runtime`: executes one tenant job under tenant context, RLS applies
- `audit_writer`: restricted append capability if separated
- `maintenance`: controlled temporary backfills

Runtime roles never own tenant tables and never have BYPASSRLS.
