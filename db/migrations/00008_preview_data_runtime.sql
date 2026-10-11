-- Purpose: M5 isolated development preview data; no production activation.
-- SAFE additive empty tables; no customer-generated DDL/backfill. Deploy before M5.
-- Lock risk: empty table setup only; writes serialize per tenant/app/version/hash.
-- Recovery: atomic failed-migration rollback; applied changes require forward fix.
-- +goose Up
CREATE TABLE runtime.preview_datasets (
 tenant_id uuid NOT NULL,
 app_id text NOT NULL CHECK(length(app_id)<=160),
 metadata_revision bigint NOT NULL CHECK(metadata_revision BETWEEN 1 AND 1000000000),
 ir_hash text NOT NULL CHECK(ir_hash ~ '^[0-9a-f]{64}$'),
 write_version bigint NOT NULL DEFAULT 1 CHECK(write_version>0),
 mode text NOT NULL DEFAULT 'preview' CHECK(mode='preview'),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash)
);
SELECT runtime.install_tenant_rls('runtime.preview_datasets'::regclass);
REVOKE DELETE,UPDATE ON runtime.preview_datasets FROM framework_app;
GRANT UPDATE(write_version) ON runtime.preview_datasets TO framework_app;
CREATE TABLE runtime.preview_records (
 tenant_id uuid NOT NULL,
 app_id text NOT NULL,
 metadata_revision bigint NOT NULL,
 ir_hash text NOT NULL,
 entity_id text NOT NULL CHECK(length(entity_id)<=160),
 id uuid NOT NULL,
 row_version bigint NOT NULL DEFAULT 1 CHECK(row_version BETWEEN 1 AND 9007199254740991),
 custom_data jsonb NOT NULL CHECK(jsonb_typeof(custom_data)='object' AND octet_length(custom_data::text)<=100000),
 created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash) REFERENCES runtime.preview_datasets(tenant_id,app_id,metadata_revision,ir_hash)
);
SELECT runtime.install_tenant_rls('runtime.preview_records'::regclass);
REVOKE INSERT,UPDATE ON runtime.preview_records FROM framework_app;
GRANT INSERT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,custom_data) ON runtime.preview_records TO framework_app;
GRANT UPDATE(custom_data,row_version,updated_at) ON runtime.preview_records TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'preview data requires reviewed forward recovery'; END $$;
-- +goose StatementEnd
