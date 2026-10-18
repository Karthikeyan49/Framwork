-- Fixed platform fields only, never customer-selected DDL. Local transaction
-- builds retain data; production online scheduling/lock/disk review is separate.
-- +goose Up
CREATE INDEX records_aggregate_headers ON runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id)
INCLUDE(row_version,owner_user_id,owner_team_id);
CREATE INDEX numeric_aggregate_values ON runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id)
INCLUDE(value_number,source_version,aggregate_valid_v1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain aggregate index evidence; reviewed forward recovery only';END $$;
-- +goose StatementEnd
