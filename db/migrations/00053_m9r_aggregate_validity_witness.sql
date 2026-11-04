-- Nullable, versioned witness; existing projections/data are not rewritten.
-- +goose Up
ALTER TABLE runtime.numeric_projections ADD COLUMN aggregate_valid_v1 boolean;
-- +goose StatementBegin
CREATE FUNCTION runtime.certify_aggregate_projection_v1() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.numeric_indexes%ROWTYPE;raw jsonb;version bigint;n numeric;
BEGIN
 -- No registry SHARE lock: writers already own the source row, while builders
 -- acquire registry then source. Existing immutable registry facts suffice.
 SELECT x.* INTO idx FROM runtime.numeric_indexes x
 WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.metadata_revision=NEW.metadata_revision
 AND x.ir_hash=NEW.ir_hash AND x.entity_id=NEW.entity_id AND x.field_id=NEW.field_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'aggregate index witness missing' USING ERRCODE='42501';END IF;
 SELECT r.custom_data->NEW.field_id,r.row_version INTO raw,version FROM runtime.records r
 WHERE r.tenant_id=NEW.tenant_id AND r.app_id=NEW.app_id AND r.metadata_revision=NEW.metadata_revision
 AND r.ir_hash=NEW.ir_hash AND r.entity_id=NEW.entity_id AND r.id=NEW.id;
 IF NOT FOUND OR version IS DISTINCT FROM NEW.source_version THEN
  RAISE EXCEPTION 'aggregate source witness mismatch' USING ERRCODE='42501';
 END IF;
 BEGIN
  n:=runtime.numeric_value(raw,idx.field_type,idx.currency,idx.required);
 EXCEPTION WHEN invalid_parameter_value THEN
  NEW.aggregate_valid_v1:=false;RETURN NEW;
 END;
 NEW.aggregate_valid_v1:=n IS NOT DISTINCT FROM NEW.value_number;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.certify_aggregate_projection_v1() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.certify_aggregate_projection_v1() FROM PUBLIC,framework_app,framework_worker,framework_dispatcher;
CREATE TRIGGER certify_aggregate_projection_v1 BEFORE INSERT OR UPDATE ON runtime.numeric_projections
FOR EACH ROW EXECUTE FUNCTION runtime.certify_aggregate_projection_v1();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain records/projections; reviewed forward recovery only';END $$;
-- +goose StatementEnd
