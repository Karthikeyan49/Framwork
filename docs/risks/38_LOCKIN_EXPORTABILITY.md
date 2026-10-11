# Customer Lock-In and Exportability

Define a Portable Application Bundle containing:
canonical metadata, schemas, workflows, policy references, tool contracts, data dictionary, migration history, release manifest and optional business-data export.

Document JSON/YAML formats and versioning.

Goal: the framework runtime is proprietary/product-specific if desired, but customer definitions and data are not opaque or trapped.

## Verification
Export a tenant app, import into clean test environment, compare canonical IR and business record counts.
