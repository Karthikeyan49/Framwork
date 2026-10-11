# Metadata Schema

External authoring format uses JSON/YAML validated with JSON Schema 2020-12, then decoded into typed Go domain models.

Stable references use IDs such as:
`entity.vendor`, `field.vendor.name`, `workflow.purchase_approval`.

Never reference by display label.

Version envelope includes:
schema_version, app_id, namespace, base_version, definitions.

Unknown structural keys are rejected unless inside an explicitly versioned extension namespace.
