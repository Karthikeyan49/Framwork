-- Fixed platform projections; no tenant DDL, source transformation or cleanup.
-- +goose Up
CREATE TABLE runtime.scalar_indexes (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,schema_hash text NOT NULL CHECK(schema_hash ~ '^[0-9a-f]{64}$'),
 field_type text NOT NULL CHECK(field_type IN('string','date')),required boolean NOT NULL,collation_version text,
 state text NOT NULL DEFAULT 'BUILDING' CHECK(state IN('BUILDING','VERIFYING','READY')),
 checkpoint bigint NOT NULL DEFAULT 1 CHECK(checkpoint>0),after_id uuid,verification_after uuid,
 scanned_rows bigint NOT NULL DEFAULT 0 CHECK(scanned_rows>=0),verified_rows bigint NOT NULL DEFAULT 0 CHECK(verified_rows>=0),
 source_revision bigint NOT NULL,source_ir_hash text NOT NULL,created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash),
 FOREIGN KEY(tenant_id,app_id,source_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision)
);
SELECT runtime.install_tenant_rls('runtime.scalar_indexes');
REVOKE ALL ON runtime.scalar_indexes FROM framework_app;
GRANT SELECT ON runtime.scalar_indexes TO framework_app;
CREATE POLICY worker_no_generic ON runtime.scalar_indexes AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE runtime.scalar_projections (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,id uuid NOT NULL,field_type text NOT NULL CHECK(field_type IN('string','date')),
 value_text text,value_date date,overflow boolean NOT NULL,source_version bigint NOT NULL CHECK(source_version>0),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) REFERENCES runtime.scalar_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE,
 CHECK((field_type='string' AND value_date IS NULL AND (value_text IS NULL OR octet_length(value_text)<=512) AND (NOT overflow OR value_text IS NULL))
 OR (field_type='date' AND value_text IS NULL AND NOT overflow))
);
SELECT runtime.install_tenant_rls('runtime.scalar_projections');
REVOKE ALL ON runtime.scalar_projections FROM framework_app;
GRANT SELECT ON runtime.scalar_projections TO framework_app;
CREATE POLICY worker_no_generic ON runtime.scalar_projections AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE INDEX scalar_text_asc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_text ASC NULLS LAST,id ASC) WHERE field_type='string' AND NOT overflow;
CREATE INDEX scalar_text_desc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_text DESC NULLS LAST,id ASC) WHERE field_type='string' AND NOT overflow;
CREATE INDEX scalar_date_asc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_date ASC NULLS LAST,id ASC) WHERE field_type='date';
CREATE INDEX scalar_date_desc ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_date DESC NULLS LAST,id ASC) WHERE field_type='date';
CREATE INDEX scalar_text_overflow ON runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) WHERE overflow;
CREATE TRIGGER retain_scalar_indexes BEFORE DELETE OR TRUNCATE ON runtime.scalar_indexes FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_scalar_projections BEFORE TRUNCATE ON runtime.scalar_projections FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
CREATE FUNCTION runtime.scalar_collation_safe(expected text) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT datcollversion IS NOT DISTINCT FROM pg_database_collation_actual_version(oid)
 AND expected IS NOT DISTINCT FROM pg_database_collation_actual_version(oid)
 FROM pg_database WHERE datname=current_database()
$$;
CREATE FUNCTION runtime.scalar_value(raw jsonb,typ text,req boolean) RETURNS TABLE(value_text text,value_date date,overflow boolean)
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE s text;y integer;
BEGIN
 IF typ NOT IN('string','date') OR typ IS NULL OR req IS NULL THEN RAISE EXCEPTION 'unsupported scalar type' USING ERRCODE='22023';END IF;
 overflow:=false;
 IF raw IS NULL OR raw='null'::jsonb THEN
  IF req THEN RAISE EXCEPTION 'required indexed value missing' USING ERRCODE='22023';END IF;
  RETURN NEXT;RETURN;
 END IF;
 IF jsonb_typeof(raw)<>'string' THEN RAISE EXCEPTION 'invalid scalar value' USING ERRCODE='22023';END IF;
 s:=raw#>>'{}';
 IF typ='string' THEN
  IF octet_length(s)>4096 OR (req AND s='') THEN RAISE EXCEPTION 'indexed text budget' USING ERRCODE='22023';END IF;
  overflow:=octet_length(s)>512;
  IF NOT overflow THEN value_text:=s;END IF;
 ELSE
  IF s !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN RAISE EXCEPTION 'invalid civil date' USING ERRCODE='22023';END IF;
  y:=substring(s,1,4)::integer;
  IF y<1 OR y>9999 THEN RAISE EXCEPTION 'civil date range' USING ERRCODE='22023';END IF;
  BEGIN value_date:=make_date(y,substring(s,6,2)::integer,substring(s,9,2)::integer);
  EXCEPTION WHEN datetime_field_overflow OR invalid_datetime_format THEN RAISE EXCEPTION 'invalid civil date' USING ERRCODE='22023';END;
 END IF;
 RETURN NEXT;
END $$;
CREATE FUNCTION runtime.check_index_budget(t uuid,a text,v bigint,h text,e text) RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF (SELECT count(*) FROM (SELECT entity_id FROM runtime.numeric_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h UNION ALL SELECT entity_id FROM runtime.scalar_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h) x)>=32
 OR (SELECT count(*) FROM (SELECT entity_id FROM runtime.numeric_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e UNION ALL SELECT entity_id FROM runtime.scalar_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e) x)>=8
 THEN RAISE EXCEPTION 'index budget' USING ERRCODE='22023';END IF;
END $$;
CREATE FUNCTION runtime.register_scalar_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.scalar_indexes%ROWTYPE;typ text;req boolean;coll text;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
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
CREATE FUNCTION runtime.maintain_scalar_projection() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.scalar_indexes%ROWTYPE;val record;
BEGIN
 FOR idx IN SELECT x.* FROM runtime.scalar_indexes x WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.metadata_revision=NEW.metadata_revision AND x.ir_hash=NEW.ir_hash AND x.entity_id=NEW.entity_id LOOP
  SELECT * INTO val FROM runtime.scalar_value(NEW.custom_data->idx.field_id,idx.field_type,idx.required);
  INSERT INTO runtime.scalar_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,field_type,value_text,value_date,overflow,source_version)
  VALUES(NEW.tenant_id,NEW.app_id,NEW.metadata_revision,NEW.ir_hash,NEW.entity_id,idx.field_id,NEW.id,idx.field_type,val.value_text,val.value_date,val.overflow,NEW.row_version)
  ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET field_type=excluded.field_type,value_text=excluded.value_text,value_date=excluded.value_date,overflow=excluded.overflow,source_version=excluded.source_version;
 END LOOP;
 RETURN NEW;
END $$;
CREATE FUNCTION runtime.build_scalar_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.scalar_indexes%ROWTYPE;rec record;val record;last_id uuid;scanned bigint:=0;after_cursor uuid;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT x.* INTO idx FROM runtime.scalar_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema OR (idx.field_type='string' AND NOT runtime.scalar_collation_safe(idx.collation_version)) THEN RAISE EXCEPTION 'index witness absent or stale' USING ERRCODE='55000';END IF;
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 after_cursor:=CASE WHEN idx.state='BUILDING' THEN idx.after_id ELSE idx.verification_after END;
 FOR rec IN WITH batch AS MATERIALIZED (
  SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
  AND (after_cursor IS NULL OR r.id>after_cursor) ORDER BY r.id LIMIT 1000 FOR SHARE
 ) SELECT r.*,p.source_version projection_version,p.field_type projection_type,p.value_text projection_text,p.value_date projection_date,p.overflow projection_overflow FROM batch r LEFT JOIN runtime.scalar_projections p
 ON idx.state='VERIFYING' AND p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.id=r.id ORDER BY r.id LOOP
  SELECT * INTO val FROM runtime.scalar_value(rec.custom_data->f,idx.field_type,idx.required);
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

-- Share quotas with compatible numeric registration; preserve prior grants.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.register_numeric_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.numeric_indexes%ROWTYPE;typ text;cur text;req boolean;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT item->'field' INTO definition FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') item
 WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND item->>'kind'='field' AND item->>'id'=f AND item->'field'->>'entity_ref'=e
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.document->'definitions') ent WHERE ent->>'id'=e AND ent->>'kind'='entity' AND ent->'entity'->'fields' ? f);
 typ:=definition->>'type';cur:=coalesce(definition->>'currency','');req:=(definition->>'required')::boolean;
 IF typ IS NULL OR typ NOT IN('integer','decimal','money') OR req IS NULL OR schema IS NULL OR schema !~ '^[0-9a-f]{64}$'
 THEN RAISE EXCEPTION 'unsupported index field' USING ERRCODE='22023';END IF;
 -- Exclusive from the start: never upgrade two competing shared locks.
 INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES(t,a,v,h) ON CONFLICT DO NOTHING;
 PERFORM 1 FROM runtime.record_sets x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h FOR UPDATE;
 SELECT x.* INTO idx FROM runtime.numeric_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
 IF FOUND THEN
  IF ROW(idx.schema_hash,idx.field_type,idx.currency,idx.required) IS DISTINCT FROM ROW(schema,typ,cur,req)
  THEN RAISE EXCEPTION 'index witness mismatch' USING ERRCODE='55000';END IF;
  RETURN;
 END IF;
 PERFORM runtime.check_index_budget(t,a,v,h,e);
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
 INSERT INTO runtime.numeric_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,currency,required,source_revision,source_ir_hash)
 VALUES(t,a,v,h,e,f,schema,typ,cur,req,sv,sh);
END $$;
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions','runtime.scalar_indexes','runtime.scalar_projections') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
ALTER FUNCTION runtime.scalar_collation_safe(text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.scalar_value(jsonb,text,boolean) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.check_index_budget(uuid,text,bigint,text,text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.register_scalar_index(uuid,text,bigint,text,text,text,text,bigint,text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.maintain_scalar_projection() OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.build_scalar_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.scalar_collation_safe(text),runtime.scalar_value(jsonb,text,boolean),runtime.check_index_budget(uuid,text,bigint,text,text),runtime.register_scalar_index(uuid,text,bigint,text,text,text,text,bigint,text),runtime.maintain_scalar_projection(),runtime.build_scalar_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.scalar_collation_safe(text),runtime.register_scalar_index(uuid,text,bigint,text,text,text,text,bigint,text),runtime.build_scalar_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) TO framework_app;
CREATE TRIGGER record_scalar_projection AFTER INSERT OR UPDATE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.maintain_scalar_projection();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain scalar witnesses and records; reviewed forward recovery only';END $$;
-- +goose StatementEnd
