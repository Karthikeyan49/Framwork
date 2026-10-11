# PostgreSQL row-level security

M2 uses migration 00004 and `postgres.WithRuntime` (ADR-015). Every normal runtime
transaction resets pooled context, checks the leased connection's effective role,
and owns bounded commit/rollback. Identity and tenant ports set transaction-local
verified issuer/subject, actor, and live membership-backed tenant scope. No scope,
malformed scope, inactive identity/membership/tenant, or tenant setting alone grants
business-row access. Go policy enforcement remains mandatory before repository calls.

Future tenant-table Goose migrations must create typed core + JSONB tables and
call the migration-only helper in the same transaction, for example:

```sql
-- Example only; no production business table is created in M2.
CREATE TABLE runtime.example (
    tenant_id uuid NOT NULL,
    id uuid NOT NULL,
    name text NOT NULL,
    custom jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.example'::regclass);
```

The helper requires a migration-owned runtime/control table, nonnull UUID tenant
scope, tenant-inclusive primary/unique keys and same-tenant foreign keys. It rejects
existing policies, global exclusion constraints and direct partition children. It
installs ENABLE + FORCE RLS, both USING and WITH CHECK, and only CRUD grants to
framework_app. Do not grant TRUNCATE, REFERENCES, TRIGGER, schema CREATE, table
ownership, privileged role membership or runtime access to partition children.
Operators have no automatic row-access policy on future business tables: migrations
that need backfills require explicit reviewed privileges and a data recovery plan.

Integration tests create uniquely named disposable tables to exercise this actual
helper with framework_app; production business schemas remain M5 work. Migration
00004 hardens M1 IAM policies too. Bounded read-only IAM SECURITY DEFINER functions
use fixed paths and explicit owner read policies to avoid recursive RLS; no
superuser/BYPASSRLS role is used by runtime or these functions. Custom settings do
not protect against a stolen database password plus arbitrary SQL access.

M3 migrations 00005–00007 install this boundary on metadata heads/versions, then
restrict runtime writes to head revision updates and immutable source appends.
Tenant-inclusive deferred head/version integrity, envelope checks, creator/time
defaults and mutation triggers are additional safeguards (ADR-016). Neither RLS
nor a schema-valid metadata source grants approval or production activation.
