-- Purpose: additive V3 generic storage; retain legacy preview tables for recovery.
-- Copy is transactional; any unequal destination aborts, never overwrites customer data.
-- Existing datasets/IDs/JSONB/versions/creators/timestamps preserved exactly.
-- +goose Up
CREATE TABLE runtime.record_sets (LIKE runtime.preview_datasets INCLUDING ALL);
SELECT runtime.install_tenant_rls('runtime.record_sets'::regclass);
REVOKE DELETE,UPDATE ON runtime.record_sets FROM framework_app;
GRANT UPDATE(write_version) ON runtime.record_sets TO framework_app;
CREATE TABLE runtime.records (LIKE runtime.preview_records INCLUDING ALL);
ALTER TABLE runtime.records ADD FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash);
ALTER TABLE runtime.records ADD schema_version text NOT NULL DEFAULT '1.0.0';
ALTER TABLE runtime.records ADD owner_user_id uuid NOT NULL DEFAULT iam.authenticated_actor();
ALTER TABLE runtime.records ADD owner_team_id uuid;
ALTER TABLE runtime.records ADD updated_by uuid NOT NULL DEFAULT iam.authenticated_actor();
SELECT runtime.install_tenant_rls('runtime.records'::regclass);
REVOKE INSERT,UPDATE ON runtime.records FROM framework_app;
GRANT INSERT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data) ON runtime.records TO framework_app;
GRANT UPDATE(custom_data,row_version,updated_at,updated_by) ON runtime.records TO framework_app;
-- Controlled migration policy exists only for the migration role.
CREATE POLICY migration_copy_sets ON runtime.record_sets TO framework_migrator USING(true) WITH CHECK(true);
CREATE POLICY migration_copy_records ON runtime.records TO framework_migrator USING(true) WITH CHECK(true);
CREATE POLICY migration_source_sets ON runtime.preview_datasets FOR SELECT TO framework_migrator USING(true);
CREATE POLICY migration_source_records ON runtime.preview_records FOR SELECT TO framework_migrator USING(true);
INSERT INTO runtime.record_sets SELECT * FROM runtime.preview_datasets;
INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,row_version,custom_data,created_by,created_at,updated_at,owner_user_id,updated_by)
 SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,row_version,custom_data,created_by,created_at,updated_at,created_by,created_by FROM runtime.preview_records;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime.preview_records l FULL JOIN runtime.records n USING(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id)
 WHERE l.id IS NULL OR n.id IS NULL OR ROW(l.row_version,l.custom_data,l.created_by,l.created_at,l.updated_at) IS DISTINCT FROM ROW(n.row_version,n.custom_data,n.created_by,n.created_at,n.updated_at)) THEN
 RAISE EXCEPTION 'V3 migration read equivalence failed'; END IF;
END $$;
-- +goose StatementEnd
DROP POLICY migration_source_sets ON runtime.preview_datasets;
DROP POLICY migration_source_records ON runtime.preview_records;
CREATE TABLE runtime.record_shares (
 tenant_id uuid NOT NULL, app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,entity_id text NOT NULL,id uuid NOT NULL,
 user_id uuid NOT NULL,can_write boolean NOT NULL DEFAULT false,
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,user_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE
);
SELECT runtime.install_tenant_rls('runtime.record_shares'::regclass);
REVOKE INSERT,UPDATE,DELETE ON runtime.record_shares FROM framework_app;
CREATE TABLE runtime.record_links (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,id uuid NOT NULL,field_id text NOT NULL,target_entity text NOT NULL,target_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE,
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,target_entity,target_id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id)
);
SELECT runtime.install_tenant_rls('runtime.record_links'::regclass);
CREATE TABLE runtime.unique_projections (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,entity_id text NOT NULL,field_id text NOT NULL,value_key text NOT NULL,id uuid NOT NULL,
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_key),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE
);
SELECT runtime.install_tenant_rls('runtime.unique_projections'::regclass);
REVOKE INSERT,UPDATE,DELETE ON runtime.unique_projections FROM framework_app;
REVOKE INSERT,UPDATE,DELETE ON runtime.preview_records,runtime.preview_datasets FROM framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'V3 data recovery requires a reviewed forward fix'; END $$;
-- +goose StatementEnd
