-- Final barrier after column privileges are revoked in 00014. Wait for any pre-cutover legacy transactions,
-- reconcile late INSERTs, and refuse divergent late UPDATEs. Legacy data remains.
-- Operators must quiesce old API/worker writers before the V3 migration chain.
-- +goose Up
LOCK TABLE runtime.preview_datasets,runtime.preview_records IN ACCESS EXCLUSIVE MODE;
INSERT INTO runtime.record_sets SELECT * FROM runtime.preview_datasets ON CONFLICT DO NOTHING;
INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,row_version,custom_data,created_by,created_at,updated_at,owner_user_id,updated_by)
 SELECT tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,row_version,custom_data,created_by,created_at,updated_at,created_by,created_by FROM runtime.preview_records ON CONFLICT DO NOTHING;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM runtime.preview_records l LEFT JOIN runtime.records n USING(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id)
 WHERE n.id IS NULL OR l.row_version>n.row_version OR (l.row_version=n.row_version AND ROW(l.custom_data,l.created_by,l.created_at,l.updated_at) IS DISTINCT FROM ROW(n.custom_data,n.created_by,n.created_at,n.updated_at))) THEN
 RAISE EXCEPTION 'legacy cutover divergence requires reviewed reconciliation; no records overwritten';END IF;
END $$;
-- +goose StatementEnd
REVOKE INSERT,UPDATE,DELETE ON runtime.preview_records,runtime.preview_datasets FROM framework_app,framework_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'preserve both stores; controlled recovery only';END $$;
-- +goose StatementEnd
