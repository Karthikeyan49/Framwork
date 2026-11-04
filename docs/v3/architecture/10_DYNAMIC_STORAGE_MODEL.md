# Dynamic Storage Model

Tenant-created entities do not create/alter shared business tables.

Suggested generic record store:

```text
runtime.records
tenant_id
entity_id
record_id
schema_version
owner_user_id
owner_team_id
data JSONB
row_version
created_at
updated_at
created_by
updated_by
```

Supporting:
`record_links`, `record_shares`, and optional typed projection indexes for text/number/date/reference/uniqueness.

This is not pure EAV: JSONB is the primary dynamic record state; projection tables are performance/index structures.

Invariant-heavy ERP modules remain typed Domain Pack tables.
