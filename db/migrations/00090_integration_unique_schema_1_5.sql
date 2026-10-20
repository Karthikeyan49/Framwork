-- Integration fix (remediation review B-05, independent review IR-01). Authoring schema 1.5.0 is a superset of 1.4.0: it
-- carries field.unique and adds the tool activity, and the compiler, metadata.IntegritySchema and the renderer accept it.
-- runtime.register_unique_constraint was the one SQL function that named a single authoring version (00052 selected the field
-- with m.schema_version='1.4.0'), so under a 1.5.0 document no claim could be registered and every write to the entity was
-- refused (55000 unique integrity not ready). It is replaced here from its only definition, byte for byte except that predicate.
-- Owner, ACL and every guard are unchanged; the migration fails if the owner or the ACL moved, or if any other function of the
-- four runtime schemas still gates on the single literal 1.4.0.
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
 IF (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='runtime' AND p.proname='register_unique_constraint')<>1 THEN
  RAISE EXCEPTION 'runtime.register_unique_constraint must exist exactly once before it is replaced';END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.register_unique_constraint(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.unique_constraints%ROWTYPE;typ text;cur text;req boolean;choices jsonb;target text;gen bigint:=1;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF NOT runtime.unique_authorized(t,a,e,f) THEN RAISE EXCEPTION 'unique manager denied' USING ERRCODE='42501';END IF;
 IF expected IS NULL OR expected<0 OR expected>=1000000000 OR f='field.' || substring(e FROM 8) || '.id' THEN RAISE EXCEPTION 'invalid unique request' USING ERRCODE='22023';END IF;
 SELECT item->'field' INTO definition FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') item
 WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND m.schema_version IN('1.4.0','1.5.0') AND item->>'kind'='field' AND item->>'id'=f AND item->'field'->>'entity_ref'=e AND item->'field'->'unique'='true'::jsonb
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.document->'definitions') ent WHERE ent->>'id'=e AND ent->>'kind'='entity' AND ent->'entity'->'fields' ? f);
 typ:=definition->>'type';cur:=coalesce(definition->>'currency','');req:=(definition->>'required')::boolean;target:=coalesce(definition->>'reference_to','');
 SELECT coalesce(jsonb_agg(x ORDER BY x COLLATE "C"),'[]'::jsonb) INTO choices FROM jsonb_array_elements_text(coalesce(definition->'enum_values','[]'::jsonb)) x;
 IF typ IS NULL OR req IS NULL OR schema IS NULL OR schema !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'unsupported unique field' USING ERRCODE='22023';END IF;
 INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES(t,a,v,h) ON CONFLICT DO NOTHING;
 PERFORM 1 FROM runtime.record_sets x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h FOR UPDATE;
 SELECT x.* INTO idx FROM runtime.unique_constraints x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF FOUND THEN
  IF ROW(idx.schema_hash,idx.field_type,idx.currency,idx.required,idx.enum_values,idx.reference_to) IS DISTINCT FROM ROW(schema,typ,cur,req,choices,target) THEN RAISE EXCEPTION 'unique witness mismatch' USING ERRCODE='55000';END IF;
  IF expected=idx.generation-1 THEN RETURN;END IF;
  IF expected<>idx.generation OR idx.generation>=1000000000 THEN RAISE EXCEPTION 'unique generation changed' USING ERRCODE='55000';END IF;
  IF idx.state<>'ABORTED' THEN RETURN;END IF;
  gen:=idx.generation+1;
 ELSIF expected<>0 THEN RAISE EXCEPTION 'unique generation changed' USING ERRCODE='55000';END IF;
 PERFORM runtime.check_index_budget(t,a,v,h,e);
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
 INSERT INTO runtime.unique_constraints(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,currency,required,enum_values,reference_to,generation,source_revision,source_ir_hash)
 VALUES(t,a,v,h,e,f,schema,typ,cur,req,choices,target,gen,sv,sh)
 ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) DO UPDATE SET generation=excluded.generation,state='BUILDING',checkpoint=1,after_id=NULL,verification_after=NULL,scanned_rows=0,verified_rows=0,source_revision=sv,source_ir_hash=sh,created_by=iam.authenticated_actor(),created_at=clock_timestamp();
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f oid:='runtime.register_unique_constraint(uuid,text,bigint,text,text,text,text,bigint,text,bigint)'::regprocedure;
 owner_name name;bad text;
BEGIN
 SELECT pg_get_userbyid(proowner) INTO owner_name FROM pg_proc WHERE oid=f;
 IF owner_name<>'framework_schema_owner' THEN RAISE EXCEPTION 'register_unique_constraint changed owner';END IF;
 IF has_function_privilege('framework_worker',f,'EXECUTE') OR has_function_privilege('framework_dispatcher',f,'EXECUTE') OR NOT has_function_privilege('framework_app',f,'EXECUTE')
  OR EXISTS(SELECT 1 FROM pg_proc p,LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE p.oid=f AND a.grantee=0) THEN
  RAISE EXCEPTION 'register_unique_constraint ACL drifted';END IF;
 SELECT string_agg(n.nspname||'.'||p.proname,', ') INTO bad FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname IN('control','runtime','iam','audit') AND p.prokind='f' AND pg_get_functiondef(p.oid) ~ 'schema_version[[:space:]]*=[[:space:]]*''1\.4\.0''';
 IF bad IS NOT NULL THEN RAISE EXCEPTION 'functions still gate on the single schema version 1.4.0: %',bad;END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain unique claims; forward recovery only';END $$;
-- +goose StatementEnd
