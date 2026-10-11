-- Purpose: M3 tenant-owned authoring heads and immutable validated source revisions.
-- Compatibility: SAFE additive, no business-data tables/backfill; deploy before M3 API.
-- Lock risk: new empty tables only. Validation: normal-role RLS/version/atomicity tests.
-- Recovery: atomic rollback on application failure; forward fixes; never erase history.
-- +goose Up
CREATE TABLE control.metadata_heads (
 tenant_id uuid NOT NULL,
 id text NOT NULL CHECK (id ~ '^app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$' AND length(id)<=160),
 namespace text NOT NULL CHECK (length(namespace) BETWEEN 1 AND 100),
 revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 1000000000),
 PRIMARY KEY(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.metadata_heads'::regclass);
REVOKE DELETE,UPDATE ON control.metadata_heads FROM framework_app;
GRANT UPDATE(revision) ON control.metadata_heads TO framework_app;
CREATE TABLE control.metadata_versions (
 tenant_id uuid NOT NULL,
 app_id text NOT NULL,
 revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 1000000000),
 schema_version text NOT NULL DEFAULT '1.0.0' CHECK (schema_version='1.0.0'),
 document jsonb NOT NULL CHECK (jsonb_typeof(document)='object' AND octet_length(document::text)<=300000
   AND document->>'schema_version'=schema_version AND document->>'app_id'=app_id),
 source_hash text NOT NULL CHECK (source_hash ~ '^[0-9a-f]{64}$'),
 created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,revision),
 FOREIGN KEY(tenant_id,app_id) REFERENCES control.metadata_heads(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.metadata_versions'::regclass);
REVOKE INSERT,UPDATE,DELETE ON control.metadata_versions FROM framework_app;
GRANT INSERT(tenant_id,app_id,revision,document,source_hash) ON control.metadata_versions TO framework_app;
-- Forensic operator read access is explicit; app still cannot read foreign scopes.
CREATE POLICY operator_metadata_read ON control.metadata_versions FOR SELECT TO framework_migrator USING(true);
-- +goose StatementBegin
CREATE FUNCTION control.reject_metadata_mutation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN RAISE EXCEPTION 'metadata source revisions are immutable'; END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control.reject_metadata_mutation() FROM PUBLIC;
CREATE TRIGGER metadata_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON control.metadata_versions
 FOR EACH STATEMENT EXECUTE FUNCTION control.reject_metadata_mutation();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'metadata history requires a reviewed forward fix'; END $$;
-- +goose StatementEnd
