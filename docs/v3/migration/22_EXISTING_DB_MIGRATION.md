# Existing Database Migration

If M5 created per-entity/shared typed tables for dynamic entities, use strangler migration:

1. mark current adapter LEGACY
2. define RecordStore interface
3. wrap legacy storage in adapter
4. implement V3 generic dynamic-record adapter
5. route new dynamic entities to V3
6. migrate old dynamic data with checkpointed jobs
7. compare reads/counts/checksums
8. switch storage binding per entity
9. retain legacy tables during compatibility window
10. remove only in a later cleanup release

Typed tables that are genuine Domain Pack storage remain typed.
