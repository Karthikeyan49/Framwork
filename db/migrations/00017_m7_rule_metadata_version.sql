-- Purpose: Opt-in metadata 1.1 for compiled DSL2. No business record changes.
-- Compatibility: Retain immutable 1.0 sources and RLS/column grants. Deploy first.
-- Lock risk: brief metadata table constraint/trigger lock; quiesce authoring writes.
-- Recovery: transactional failure or reviewed forward fix; never downgrade history.
-- +goose Up
ALTER TABLE control.metadata_versions DROP CONSTRAINT metadata_versions_schema_version_check;
ALTER TABLE control.metadata_versions ADD CONSTRAINT metadata_versions_schema_version_check
 CHECK(schema_version IN ('1.0.0','1.1.0'));
-- Version is server-derived from the validated envelope, never an extra runtime grant.
-- +goose StatementBegin
CREATE FUNCTION control.metadata_envelope_version() RETURNS trigger
 LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 NEW.schema_version := NEW.document->>'schema_version';
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION control.metadata_envelope_version() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION control.metadata_envelope_version() FROM PUBLIC;
CREATE TRIGGER metadata_envelope_version BEFORE INSERT ON control.metadata_versions
 FOR EACH ROW EXECUTE FUNCTION control.metadata_envelope_version();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Rule metadata history requires a reviewed forward fix'; END $$;
-- +goose StatementEnd
