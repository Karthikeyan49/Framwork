# Legacy Data Import

Pipeline:
source → staging → parse → mapping → validation → reconciliation → commit → report.

Staging preserves source ID/raw payload/error state.

Reconcile:
row counts, key financial totals where applicable, duplicates, missing references, invalid enums, orphan relations.

Imports are resumable and versioned.
