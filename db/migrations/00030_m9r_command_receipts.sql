-- R4: immutable command reconciliation. No customer value snapshots or cleanup.
-- +goose Up
CREATE TABLE runtime.command_receipts (
 tenant_id uuid NOT NULL,actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),app_id text NOT NULL,command_key uuid NOT NULL,
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 entity_id text NOT NULL,operation text NOT NULL CHECK(operation IN('create','update','delete')),
 record_id uuid NOT NULL,record_version bigint NOT NULL CHECK(record_version>0),
 metadata_revision bigint NOT NULL,ir_hash text NOT NULL CHECK(ir_hash ~ '^[0-9a-f]{64}$'),
 data_revision bigint NOT NULL,data_hash text NOT NULL,dataset_id uuid,binding_version bigint,
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,actor_id,app_id,command_key),
 FOREIGN KEY(tenant_id,app_id,data_revision,data_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash),
 FOREIGN KEY(tenant_id,app_id,metadata_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision),
 FOREIGN KEY(tenant_id,app_id,dataset_id,binding_version) REFERENCES runtime.dataset_bindings(tenant_id,app_id,dataset_id,version),
 CHECK((dataset_id IS NULL)=(binding_version IS NULL) AND (binding_version IS NULL OR binding_version>0))
);
SELECT runtime.install_tenant_rls('runtime.command_receipts');
REVOKE ALL ON runtime.command_receipts FROM framework_app;
GRANT SELECT ON runtime.command_receipts TO framework_app;
GRANT INSERT(tenant_id,actor_id,app_id,command_key,request_hash,entity_id,operation,record_id,record_version,metadata_revision,ir_hash,data_revision,data_hash,dataset_id,binding_version) ON runtime.command_receipts TO framework_app;
CREATE POLICY receipt_actor ON runtime.command_receipts AS RESTRICTIVE TO framework_app USING(actor_id=(SELECT iam.authenticated_actor())) WITH CHECK(actor_id=(SELECT iam.authenticated_actor()));
CREATE POLICY worker_no_generic ON runtime.command_receipts AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER immutable_command_receipt BEFORE UPDATE OR DELETE ON runtime.command_receipts FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER immutable_command_receipt_truncate BEFORE TRUNCATE ON runtime.command_receipts FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- Add the private receipt catalog to worker grant-drift admission checks.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname||'.'||c.relname IN('runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts')
 AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain command receipts; reviewed forward recovery only';END $$;
-- +goose StatementEnd
