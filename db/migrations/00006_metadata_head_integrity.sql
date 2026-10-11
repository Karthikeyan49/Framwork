-- Purpose: prevent an authoring head from pointing at absent immutable history.
-- Compatibility: SAFE additive; no backfill; existing M3 heads are transactional.
-- Validation: missing-version head updates fail commit; normal append still atomic.
-- Recovery: atomic rollback if historical corruption exists; inspect and forward-fix.
-- +goose Up
ALTER TABLE control.metadata_heads ADD CONSTRAINT metadata_head_revision
 FOREIGN KEY(tenant_id,id,revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision)
 DEFERRABLE INITIALLY DEFERRED;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'metadata integrity requires a reviewed forward fix'; END $$;
-- +goose StatementEnd
