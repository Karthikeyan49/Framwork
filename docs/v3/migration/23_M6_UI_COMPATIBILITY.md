# M6 Dynamic UI Compatibility

The frontend must consume semantic contracts, not physical storage.

Use:
entity_id, field_id, query_id, action_id, component definition.

Do not expose table names, column names or SQL details to page metadata.

The same renderer should work for dynamic records, typed Domain Packs and reporting projections.
