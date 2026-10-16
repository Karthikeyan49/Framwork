-- Fixed, typed projections; retain all old records, indexes and conversion ports.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION runtime.scalar_options_safe(typ text,choices jsonb,target text) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE item jsonb;prior text;s text;
BEGIN
 IF typ IS NULL OR choices IS NULL OR target IS NULL OR jsonb_typeof(choices)<>'array' THEN RETURN false;END IF;
 IF typ='enum' THEN
  IF target<>'' OR jsonb_array_length(choices)<1 OR jsonb_array_length(choices)>100 THEN RETURN false;END IF;
  FOR item IN SELECT value FROM jsonb_array_elements(choices) LOOP
   IF jsonb_typeof(item)<>'string' THEN RETURN false;END IF;
   s:=item#>>'{}';
   IF char_length(s)<1 OR char_length(s)>128 OR octet_length(s)>512 OR (prior IS NOT NULL AND prior COLLATE "C">=s COLLATE "C") THEN RETURN false;END IF;
   prior:=s;
  END LOOP;
  RETURN true;
 ELSIF typ='reference' THEN
  RETURN choices='[]'::jsonb AND char_length(target)<=160 AND target ~ '^entity\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$';
 ELSE
  RETURN typ IN('string','date') AND choices='[]'::jsonb AND target='';
 END IF;
END $$;
CREATE FUNCTION runtime.scalar_project_value(raw jsonb,typ text,req boolean,choices jsonb)
RETURNS TABLE(value_text text,value_date date,overflow boolean)
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE s text;
BEGIN
 IF req IS NULL OR typ IS NULL OR choices IS NULL THEN RAISE EXCEPTION 'invalid scalar contract' USING ERRCODE='22023';END IF;
 IF typ IN('string','date') THEN
  IF choices<>'[]'::jsonb THEN RAISE EXCEPTION 'invalid scalar options' USING ERRCODE='22023';END IF;
  RETURN QUERY SELECT * FROM runtime.scalar_value(raw,typ,req);RETURN;
 END IF;
 IF (typ='enum' AND NOT runtime.scalar_options_safe(typ,choices,'')) OR (typ='reference' AND choices<>'[]'::jsonb) OR typ NOT IN('enum','reference')
 THEN RAISE EXCEPTION 'invalid choice contract' USING ERRCODE='22023';END IF;
 overflow:=false;
 IF raw IS NULL OR raw='null'::jsonb THEN
  IF req THEN RAISE EXCEPTION 'required indexed value missing' USING ERRCODE='22023';END IF;
  RETURN NEXT;RETURN;
 END IF;
 IF jsonb_typeof(raw)<>'string' THEN RAISE EXCEPTION 'invalid choice value' USING ERRCODE='22023';END IF;
 s:=raw#>>'{}';
 IF (typ='enum' AND NOT choices ? s) OR (typ='reference' AND s !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
 THEN RAISE EXCEPTION 'invalid choice value' USING ERRCODE='22023';END IF;
 value_text:=s;RETURN NEXT;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.scalar_options_safe(text,jsonb,text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.scalar_project_value(jsonb,text,boolean,jsonb) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.scalar_options_safe(text,jsonb,text),runtime.scalar_project_value(jsonb,text,boolean,jsonb) FROM PUBLIC;

ALTER TABLE runtime.scalar_indexes ADD COLUMN enum_values jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE runtime.scalar_indexes ADD COLUMN reference_to text NOT NULL DEFAULT '';
ALTER TABLE runtime.scalar_indexes DROP CONSTRAINT scalar_indexes_field_type_check;
ALTER TABLE runtime.scalar_indexes ADD CONSTRAINT scalar_indexes_field_type_check CHECK(field_type IN('string','date','enum','reference'));
ALTER TABLE runtime.scalar_indexes ADD CONSTRAINT scalar_indexes_options CHECK(runtime.scalar_options_safe(field_type,enum_values,reference_to)) NOT VALID;
ALTER TABLE runtime.scalar_indexes VALIDATE CONSTRAINT scalar_indexes_options;
ALTER TABLE runtime.scalar_projections DROP CONSTRAINT scalar_projections_field_type_check;
ALTER TABLE runtime.scalar_projections ADD CONSTRAINT scalar_projections_field_type_check CHECK(field_type IN('string','date','enum','reference'));
ALTER TABLE runtime.scalar_projections DROP CONSTRAINT scalar_projections_check;
ALTER TABLE runtime.scalar_projections ADD CONSTRAINT scalar_projections_check CHECK(
 (field_type='string' AND value_date IS NULL AND (value_text IS NULL OR octet_length(value_text)<=512) AND (NOT overflow OR value_text IS NULL))
 OR (field_type='date' AND value_text IS NULL AND NOT overflow)
 OR (field_type IN('enum','reference') AND value_date IS NULL AND NOT overflow AND (value_text IS NULL OR octet_length(value_text)<=512)));
CREATE INDEX scalar_enum_asc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_text ASC NULLS LAST,id ASC) WHERE field_type='enum';
CREATE INDEX scalar_enum_desc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_text DESC NULLS LAST,id ASC) WHERE field_type='enum';
CREATE INDEX scalar_reference_asc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_text ASC NULLS LAST,id ASC) WHERE field_type='reference';
CREATE INDEX scalar_reference_desc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_text DESC NULLS LAST,id ASC) WHERE field_type='reference';

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.register_scalar_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.scalar_indexes%ROWTYPE;typ text;req boolean;coll text;choices jsonb;target text;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF f='field.' || substring(e FROM 8) || '.id' THEN RAISE EXCEPTION 'unsupported intrinsic index field' USING ERRCODE='22023';END IF;
 SELECT item->'field' INTO definition FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') item
 WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND item->>'kind'='field' AND item->>'id'=f AND item->'field'->>'entity_ref'=e
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.document->'definitions') ent WHERE ent->>'id'=e AND ent->>'kind'='entity' AND ent->'entity'->'fields' ? f);
 typ:=definition->>'type';req:=(definition->>'required')::boolean;
 choices:=coalesce(definition->'enum_values','[]'::jsonb);target:=coalesce(definition->>'reference_to','');
 -- Validate the raw shape before sorting; text conversion must not admit numbers.
 IF jsonb_typeof(choices) IS DISTINCT FROM 'array' OR EXISTS(SELECT 1 FROM jsonb_array_elements(choices) x WHERE jsonb_typeof(x)<>'string')
 THEN RAISE EXCEPTION 'invalid scalar options' USING ERRCODE='22023';END IF;
 SELECT coalesce(jsonb_agg(x ORDER BY x COLLATE "C"),'[]'::jsonb) INTO choices FROM jsonb_array_elements_text(choices) x;
 IF NOT runtime.scalar_options_safe(typ,choices,target) OR req IS NULL OR schema IS NULL OR schema !~ '^[0-9a-f]{64}$'
 THEN RAISE EXCEPTION 'unsupported index field' USING ERRCODE='22023';END IF;
 IF typ='reference' AND NOT EXISTS(SELECT 1 FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') ent WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND ent->>'id'=target AND ent->>'kind'='entity')
 THEN RAISE EXCEPTION 'invalid reference target' USING ERRCODE='22023';END IF;
 IF typ<>'date' THEN
  SELECT pg_database_collation_actual_version(oid) INTO coll FROM pg_database WHERE datname=current_database();
  IF NOT runtime.scalar_collation_safe(coll) THEN RAISE EXCEPTION 'collation repair required' USING ERRCODE='55000';END IF;
 END IF;
 INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES(t,a,v,h) ON CONFLICT DO NOTHING;
 PERFORM 1 FROM runtime.record_sets x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h FOR UPDATE;
 SELECT x.* INTO idx FROM runtime.scalar_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
 IF FOUND THEN
  IF ROW(idx.schema_hash,idx.field_type,idx.required,idx.collation_version,idx.enum_values,idx.reference_to) IS DISTINCT FROM ROW(schema,typ,req,coll,choices,target)
  THEN RAISE EXCEPTION 'index witness mismatch' USING ERRCODE='55000';END IF;
  RETURN;
 END IF;
 PERFORM runtime.check_index_budget(t,a,v,h,e);
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
 INSERT INTO runtime.scalar_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,required,collation_version,source_revision,source_ir_hash,enum_values,reference_to)
 VALUES(t,a,v,h,e,f,schema,typ,req,coll,sv,sh,choices,target);
END $$;
-- +goose StatementEnd

-- Retain prior ownership/ACLs and bounded copy/verification/locking.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.maintain_scalar_projection() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.scalar_indexes%ROWTYPE;val record;
BEGIN
 FOR idx IN SELECT x.* FROM runtime.scalar_indexes x WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.metadata_revision=NEW.metadata_revision AND x.ir_hash=NEW.ir_hash AND x.entity_id=NEW.entity_id LOOP
  SELECT * INTO val FROM runtime.scalar_project_value(NEW.custom_data->idx.field_id,idx.field_type,idx.required,idx.enum_values);
  INSERT INTO runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,field_type,value_text,value_date,overflow,source_version)
  VALUES(NEW.tenant_id,NEW.app_id,NEW.metadata_revision,NEW.ir_hash,NEW.entity_id,idx.field_id,NEW.id,idx.field_type,val.value_text,val.value_date,val.overflow,NEW.row_version)
  ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET field_type=excluded.field_type,value_text=excluded.value_text,value_date=excluded.value_date,overflow=excluded.overflow,source_version=excluded.source_version;
 END LOOP;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION runtime.build_scalar_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.scalar_indexes%ROWTYPE;rec record;val record;last_id uuid;scanned bigint:=0;after_cursor uuid;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT x.* INTO idx FROM runtime.scalar_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema OR (idx.field_type<>'date' AND NOT runtime.scalar_collation_safe(idx.collation_version)) THEN RAISE EXCEPTION 'index witness absent or stale' USING ERRCODE='55000';END IF;
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 after_cursor:=CASE WHEN idx.state='BUILDING' THEN idx.after_id ELSE idx.verification_after END;
 FOR rec IN WITH batch AS MATERIALIZED (
  SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
  AND (after_cursor IS NULL OR r.id>after_cursor) ORDER BY r.id LIMIT 1000 FOR SHARE
 ) SELECT r.*,p.source_version projection_version,p.field_type projection_type,p.value_text projection_text,p.value_date projection_date,p.overflow projection_overflow FROM batch r LEFT JOIN runtime.scalar_projections p
 ON idx.state='VERIFYING' AND p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.id=r.id ORDER BY r.id LOOP
  SELECT * INTO val FROM runtime.scalar_project_value(rec.custom_data->f,idx.field_type,idx.required,idx.enum_values);
  IF idx.state='BUILDING' THEN
   INSERT INTO runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,field_type,value_text,value_date,overflow,source_version)
   VALUES(t,a,v,h,e,f,rec.id,idx.field_type,val.value_text,val.value_date,val.overflow,rec.row_version)
   ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET field_type=excluded.field_type,value_text=excluded.value_text,value_date=excluded.value_date,overflow=excluded.overflow,source_version=excluded.source_version
   WHERE scalar_projections.source_version<=excluded.source_version;
  ELSE
   IF rec.projection_version IS NULL OR rec.projection_version<>rec.row_version OR rec.projection_type<>idx.field_type OR rec.projection_text IS DISTINCT FROM val.value_text OR rec.projection_date IS DISTINCT FROM val.value_date OR rec.projection_overflow IS DISTINCT FROM val.overflow THEN RAISE EXCEPTION 'index coverage mismatch' USING ERRCODE='55000';END IF;
  END IF;
  last_id:=rec.id;scanned:=scanned+1;
 END LOOP;
 UPDATE runtime.scalar_indexes x SET after_id=CASE WHEN idx.state='BUILDING' THEN coalesce(last_id,idx.after_id) ELSE idx.after_id END,
 verification_after=CASE WHEN idx.state='VERIFYING' THEN coalesce(last_id,idx.verification_after) ELSE idx.verification_after END,
 scanned_rows=x.scanned_rows+CASE WHEN idx.state='BUILDING' THEN scanned ELSE 0 END,verified_rows=x.verified_rows+CASE WHEN idx.state='VERIFYING' THEN scanned ELSE 0 END,
 checkpoint=x.checkpoint+1,state=CASE WHEN scanned=1000 THEN idx.state WHEN idx.state='BUILDING' THEN 'VERIFYING' ELSE 'READY' END
 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain typed projection witnesses and records; reviewed forward recovery only';END $$;
-- +goose StatementEnd
