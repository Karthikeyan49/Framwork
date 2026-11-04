-- Intrinsic IDs live outside custom_data. Preserve 47 and all existing evidence.
-- +goose Up
-- Validation deliberately stops installation if incompatible witnesses exist.
-- Do not delete registrations or customer records to make this migration pass.
ALTER TABLE runtime.scalar_indexes ADD CONSTRAINT scalar_indexes_custom_field
 CHECK(field_id <> 'field.' || substring(entity_id FROM 8) || '.id') NOT VALID;
ALTER TABLE runtime.scalar_indexes VALIDATE CONSTRAINT scalar_indexes_custom_field;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.register_scalar_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.scalar_indexes%ROWTYPE;typ text;req boolean;coll text;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF f='field.' || substring(e FROM 8) || '.id' THEN
  RAISE EXCEPTION 'unsupported intrinsic index field' USING ERRCODE='22023';
 END IF;
 SELECT item->'field' INTO definition FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') item
 WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND item->>'kind'='field' AND item->>'id'=f AND item->'field'->>'entity_ref'=e
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.document->'definitions') ent WHERE ent->>'id'=e AND ent->>'kind'='entity' AND ent->'entity'->'fields' ? f);
 typ:=definition->>'type';req:=(definition->>'required')::boolean;
 IF typ IS NULL OR typ NOT IN('string','date') OR req IS NULL OR schema IS NULL OR schema !~ '^[0-9a-f]{64}$'
 THEN RAISE EXCEPTION 'unsupported index field' USING ERRCODE='22023';END IF;
 IF typ='string' THEN
  SELECT pg_database_collation_actual_version(oid) INTO coll FROM pg_database WHERE datname=current_database();
  IF NOT runtime.scalar_collation_safe(coll) THEN RAISE EXCEPTION 'collation repair required' USING ERRCODE='55000';END IF;
 END IF;
 INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES(t,a,v,h) ON CONFLICT DO NOTHING;
 PERFORM 1 FROM runtime.record_sets x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h FOR UPDATE;
 SELECT x.* INTO idx FROM runtime.scalar_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
 IF FOUND THEN
  IF ROW(idx.schema_hash,idx.field_type,idx.required,idx.collation_version) IS DISTINCT FROM ROW(schema,typ,req,coll)
  THEN RAISE EXCEPTION 'index witness mismatch' USING ERRCODE='55000';END IF;
  RETURN;
 END IF;
 PERFORM runtime.check_index_budget(t,a,v,h,e);
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
 INSERT INTO runtime.scalar_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,required,collation_version,source_revision,source_ir_hash)
 VALUES(t,a,v,h,e,f,schema,typ,req,coll,sv,sh);
END $$;
-- +goose StatementEnd
-- CREATE OR REPLACE preserves the reviewed NOLOGIN owner and existing ACLs.

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain intrinsic-ID guard and records; reviewed forward recovery only';END $$;
-- +goose StatementEnd
