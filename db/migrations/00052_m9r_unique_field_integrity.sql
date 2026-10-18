-- Governed integrity claims. Source records and earlier migrations are retained.
-- +goose Up
ALTER TABLE control.metadata_versions DROP CONSTRAINT metadata_versions_schema_version_check;
ALTER TABLE control.metadata_versions ADD CONSTRAINT metadata_versions_schema_version_check CHECK(schema_version IN('1.0.0','1.1.0','1.2.0','1.3.0','1.4.0'));
CREATE TABLE runtime.unique_constraints (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,schema_hash text NOT NULL CHECK(schema_hash ~ '^[0-9a-f]{64}$'),
 field_type text NOT NULL CHECK(field_type IN('string','integer','decimal','money','bool','date','datetime','enum','reference')),
 currency text NOT NULL,required boolean NOT NULL,enum_values jsonb NOT NULL,reference_to text NOT NULL,
 generation bigint NOT NULL DEFAULT 1 CHECK(generation BETWEEN 1 AND 1000000000),
 state text NOT NULL DEFAULT 'BUILDING' CHECK(state IN('BUILDING','VERIFYING','READY','ABORTED')),
 checkpoint bigint NOT NULL DEFAULT 1 CHECK(checkpoint>0),after_id uuid,verification_after uuid,
 scanned_rows bigint NOT NULL DEFAULT 0 CHECK(scanned_rows>=0),verified_rows bigint NOT NULL DEFAULT 0 CHECK(verified_rows>=0),
 source_revision bigint NOT NULL,source_ir_hash text NOT NULL,created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(field_id <> 'field.' || substring(entity_id FROM 8) || '.id'),
 CHECK((field_type='money' AND currency ~ '^[A-Z]{3}$' OR field_type<>'money' AND currency='') AND
 (field_type IN('integer','decimal','money') AND enum_values='[]'::jsonb AND reference_to='' OR runtime.scalar_options_safe(field_type,enum_values,reference_to))),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash),
 FOREIGN KEY(tenant_id,app_id,source_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision)
);
CREATE TABLE runtime.unique_claims (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,generation bigint NOT NULL,id uuid NOT NULL,
 value_key text CHECK(octet_length(value_key)<=32768),value_hash text COLLATE "C" CHECK(value_hash ~ '^[0-9a-f]{64}$'),source_version bigint NOT NULL CHECK(source_version>0),
 CHECK((value_key IS NULL)=(value_hash IS NULL)),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,generation,id),
 CONSTRAINT unique_claim_value UNIQUE(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,generation,value_hash),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) REFERENCES runtime.unique_constraints(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE
);
CREATE TABLE runtime.unique_constraint_history (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,generation bigint NOT NULL,snapshot jsonb NOT NULL,actor_id uuid NOT NULL,occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,generation),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) REFERENCES runtime.unique_constraints(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id)
);
SELECT runtime.install_tenant_rls('runtime.unique_constraints');
SELECT runtime.install_tenant_rls('runtime.unique_claims');
SELECT runtime.install_tenant_rls('runtime.unique_constraint_history');
REVOKE ALL ON runtime.unique_constraints,runtime.unique_claims,runtime.unique_constraint_history FROM framework_app;
GRANT SELECT ON runtime.unique_constraints,runtime.unique_constraint_history TO framework_app;
CREATE POLICY worker_no_generic ON runtime.unique_constraints AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE POLICY worker_no_generic ON runtime.unique_claims AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE POLICY worker_no_generic ON runtime.unique_constraint_history AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER retain_unique_constraints BEFORE DELETE OR TRUNCATE ON runtime.unique_constraints FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER immutable_unique_history BEFORE UPDATE OR DELETE OR TRUNCATE ON runtime.unique_constraint_history FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_unique_claims BEFORE TRUNCATE ON runtime.unique_claims FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TABLE control.unique_write_context (
 backend integer NOT NULL,slot integer NOT NULL CHECK(slot BETWEEN 0 AND 31),transaction_id xid8 NOT NULL,
 actor_id uuid NOT NULL,tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,id uuid NOT NULL,field_id text NOT NULL,generation bigint NOT NULL,value_hash text NOT NULL,
 PRIMARY KEY(backend,slot)
);
ALTER TABLE control.unique_write_context OWNER TO framework_schema_owner;
ALTER TABLE control.unique_write_context ENABLE ROW LEVEL SECURITY;
ALTER TABLE control.unique_write_context FORCE ROW LEVEL SECURITY;
CREATE POLICY schema_owner_access ON control.unique_write_context TO framework_schema_owner USING(true) WITH CHECK(true);

-- +goose StatementBegin
CREATE FUNCTION runtime.unique_key(raw jsonb,typ text,cur text,req boolean,choices jsonb) RETURNS text
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp SET TimeZone='UTC' SET DateStyle='ISO, YMD' AS $$
DECLARE n numeric;s record;value jsonb;
BEGIN
 IF raw IS NULL OR raw='null'::jsonb THEN
  IF req THEN RAISE EXCEPTION 'required unique value missing' USING ERRCODE='22023';END IF;
  RETURN NULL;
 END IF;
 IF typ IN('integer','decimal','money') THEN
  n:=runtime.numeric_value(raw,typ,cur,req);value:=to_jsonb(trim_scale(n)::text);
 ELSE
  SELECT * INTO s FROM runtime.scalar_native_value(raw,typ,req,choices);
  CASE typ
   WHEN 'string' THEN value:=raw; -- Preserve text beyond the ordering-index inline budget.
   WHEN 'enum','reference' THEN value:=to_jsonb(s.value_text);
   WHEN 'date' THEN value:=to_jsonb(s.value_date-date '0001-01-01');
   WHEN 'datetime' THEN value:=to_jsonb(trim_scale(extract(epoch FROM s.value_instant)*1000000)::text);
   WHEN 'bool' THEN value:=to_jsonb(s.value_boolean);
   ELSE RAISE EXCEPTION 'unsupported unique type' USING ERRCODE='22023';
  END CASE;
 END IF;
 RETURN jsonb_build_array('unique-v1',typ,cur,value)::text;
END $$;
CREATE FUNCTION runtime.unique_authorized(t uuid,a text,e text,f text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE r record;doc jsonb;action text;fid text;
BEGIN
 IF session_user<>'framework_app' OR NOT coalesce(iam.tenant_matches_scope(t),false) THEN RETURN false;END IF;
 SELECT m.role,h.active,v.bootstrap,v.body,v.body_hash INTO r
 FROM iam.tenants tenant JOIN iam.memberships m ON m.tenant_id=tenant.id
 JOIN iam.users u ON u.id=m.user_id JOIN control.role_heads h ON h.tenant_id=m.tenant_id AND h.name=m.role
 JOIN control.role_versions v ON v.tenant_id=h.tenant_id AND v.name=h.name AND v.version=h.version
 WHERE tenant.id=t AND tenant.active AND u.active AND m.active AND m.user_id=iam.authenticated_actor()
 FOR SHARE OF tenant,m,u,h;
 IF NOT FOUND OR NOT r.active OR r.body_hash IS DISTINCT FROM encode(sha256(r.body),'hex') THEN RETURN false;END IF;
 IF r.bootstrap THEN RETURN r.role IN('owner','admin','member');END IF;
 doc:=convert_from(r.body,'UTF8')::jsonb;
 IF doc->>'schema_version' IS DISTINCT FROM 'tenant-capabilities-v1' OR doc->>'name' IS DISTINCT FROM r.role THEN RETURN false;END IF;
 FOREACH action IN ARRAY ARRAY['data.entity.read','data.entity.write','data.row.read','data.row.write','data.field.read','data.field.write'] LOOP
  fid:=CASE WHEN action LIKE 'data.field.%' THEN f ELSE '' END;
  IF control.policy_grant(doc,action,a,e,fid)<>1 THEN RETURN false;END IF;
  IF action LIKE 'data.entity.%' OR action LIKE 'data.field.%' THEN
   IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(doc->'grants') g WHERE g->>'effect'='ALLOW' AND g->'actions' ? action
    AND g->'resource'->>'app_id' IN('*',a) AND g->'resource'->>'entity_id' IN('*',e) AND g->'resource'->>'field_id' IN('*',fid)
    AND g->'scope'='{"kind":"ALL"}'::jsonb) THEN RETURN false;END IF;
  END IF;
 END LOOP;
 RETURN true;
END $$;
CREATE FUNCTION runtime.authorize_unique_write(t uuid,a text,v bigint,h text,e text,rid uuid,payload jsonb) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.unique_constraints%ROWTYPE;k text;slot integer:=0;
BEGIN
 IF session_user<>'framework_app' OR NOT coalesce(iam.tenant_matches_scope(t),false) OR rid IS NULL OR jsonb_typeof(payload) IS DISTINCT FROM 'object'
 OR octet_length(payload::text)>65536 OR (SELECT count(*) FROM jsonb_object_keys(payload))>32 THEN RAISE EXCEPTION 'unique write denied' USING ERRCODE='42501';END IF;
 DELETE FROM control.unique_write_context WHERE backend=pg_backend_pid();
 FOR idx IN SELECT x.* FROM runtime.unique_constraints x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.state='READY' AND payload ? x.field_id ORDER BY x.field_id FOR SHARE LOOP
  k:=runtime.unique_key(payload->idx.field_id,idx.field_type,idx.currency,idx.required,idx.enum_values);
  IF k IS NULL THEN CONTINUE;END IF;
  IF NOT runtime.unique_authorized(t,a,e,idx.field_id) THEN RAISE EXCEPTION 'unique write denied' USING ERRCODE='42501';END IF;
  INSERT INTO control.unique_write_context VALUES(pg_backend_pid(),slot,pg_current_xact_id(),iam.authenticated_actor(),t,a,v,h,e,rid,idx.field_id,idx.generation,encode(sha256(convert_to(k,'UTF8')),'hex'));
  slot:=slot+1;
 END LOOP;
END $$;
CREATE FUNCTION runtime.clear_unique_write() RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' THEN RAISE EXCEPTION 'unique write denied' USING ERRCODE='42501';END IF;
 DELETE FROM control.unique_write_context WHERE backend=pg_backend_pid();
END $$;
CREATE OR REPLACE FUNCTION runtime.check_index_budget(t uuid,a text,v bigint,h text,e text) RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF (SELECT count(*) FROM (SELECT entity_id FROM runtime.numeric_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h UNION ALL SELECT entity_id FROM runtime.scalar_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h UNION ALL SELECT entity_id FROM runtime.search_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h UNION ALL SELECT entity_id FROM runtime.unique_constraints WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND state<>'ABORTED') x)>=32
 OR (SELECT count(*) FROM (SELECT entity_id FROM runtime.numeric_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e UNION ALL SELECT entity_id FROM runtime.scalar_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e UNION ALL SELECT entity_id FROM runtime.search_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e UNION ALL SELECT entity_id FROM runtime.unique_constraints WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e AND state<>'ABORTED') x)>=8
 THEN RAISE EXCEPTION 'index budget' USING ERRCODE='22023';END IF;
END $$;
CREATE FUNCTION runtime.register_unique_constraint(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.unique_constraints%ROWTYPE;typ text;cur text;req boolean;choices jsonb;target text;gen bigint:=1;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF NOT runtime.unique_authorized(t,a,e,f) THEN RAISE EXCEPTION 'unique manager denied' USING ERRCODE='42501';END IF;
 IF expected IS NULL OR expected<0 OR expected>=1000000000 OR f='field.' || substring(e FROM 8) || '.id' THEN RAISE EXCEPTION 'invalid unique request' USING ERRCODE='22023';END IF;
 SELECT item->'field' INTO definition FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') item
 WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND m.schema_version='1.4.0' AND item->>'kind'='field' AND item->>'id'=f AND item->'field'->>'entity_ref'=e AND item->'field'->'unique'='true'::jsonb
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
CREATE FUNCTION runtime.build_unique_constraint(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,gen bigint,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.unique_constraints%ROWTYPE;rec record;k text;kh text;last_id uuid;scanned bigint:=0;after_cursor uuid;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF NOT runtime.unique_authorized(t,a,e,f) THEN RAISE EXCEPTION 'unique manager denied' USING ERRCODE='42501';END IF;
 SELECT x.* INTO idx FROM runtime.unique_constraints x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema OR gen IS DISTINCT FROM idx.generation OR idx.state='ABORTED' OR expected IS NULL OR expected<1 THEN RAISE EXCEPTION 'unique witness changed' USING ERRCODE='55000';END IF;
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 after_cursor:=CASE WHEN idx.state='BUILDING' THEN idx.after_id ELSE idx.verification_after END;
 FOR rec IN WITH batch AS MATERIALIZED (
  SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e AND (after_cursor IS NULL OR r.id>after_cursor) ORDER BY r.id LIMIT 1000 FOR SHARE
 ) SELECT r.*,p.source_version projection_version,p.value_key projection_key,p.value_hash projection_hash FROM batch r LEFT JOIN runtime.unique_claims p
 ON idx.state='VERIFYING' AND p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.generation=gen AND p.id=r.id ORDER BY r.id LOOP
  k:=runtime.unique_key(rec.custom_data->f,idx.field_type,idx.currency,idx.required,idx.enum_values);kh:=encode(sha256(convert_to(k,'UTF8')),'hex');
  IF idx.state='BUILDING' THEN
   INSERT INTO runtime.unique_claims VALUES(t,a,v,h,e,f,gen,rec.id,k,kh,rec.row_version)
   ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,generation,id) DO UPDATE SET value_key=excluded.value_key,value_hash=excluded.value_hash,source_version=excluded.source_version;
  ELSIF rec.projection_version IS DISTINCT FROM rec.row_version OR rec.projection_key IS DISTINCT FROM k OR rec.projection_hash IS DISTINCT FROM kh THEN RAISE EXCEPTION 'unique coverage mismatch' USING ERRCODE='55000';END IF;
  last_id:=rec.id;scanned:=scanned+1;
 END LOOP;
 UPDATE runtime.unique_constraints x SET after_id=CASE WHEN idx.state='BUILDING' THEN coalesce(last_id,idx.after_id) ELSE idx.after_id END,
 verification_after=CASE WHEN idx.state='VERIFYING' THEN coalesce(last_id,idx.verification_after) ELSE idx.verification_after END,
 scanned_rows=x.scanned_rows+CASE WHEN idx.state='BUILDING' THEN scanned ELSE 0 END,verified_rows=x.verified_rows+CASE WHEN idx.state='VERIFYING' THEN scanned ELSE 0 END,
 checkpoint=x.checkpoint+1,state=CASE WHEN scanned=1000 THEN idx.state WHEN idx.state='BUILDING' THEN 'VERIFYING' ELSE 'READY' END
 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
END $$;
CREATE FUNCTION runtime.abort_unique_constraint(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,gen bigint,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.unique_constraints%ROWTYPE;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF NOT runtime.unique_authorized(t,a,e,f) THEN RAISE EXCEPTION 'unique manager denied' USING ERRCODE='42501';END IF;
 PERFORM 1 FROM runtime.record_sets x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h FOR UPDATE;
 SELECT x.* INTO idx FROM runtime.unique_constraints x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema OR gen IS DISTINCT FROM idx.generation OR expected IS NULL THEN RAISE EXCEPTION 'unique generation changed' USING ERRCODE='55000';END IF;
 IF idx.state='ABORTED' AND expected=idx.checkpoint-1 THEN RETURN;END IF;
 IF idx.state NOT IN('BUILDING','VERIFYING') OR expected<>idx.checkpoint THEN RAISE EXCEPTION 'unique abort conflict' USING ERRCODE='55000';END IF;
 INSERT INTO runtime.unique_constraint_history VALUES(t,a,v,h,e,f,gen,to_jsonb(idx),iam.authenticated_actor(),clock_timestamp());
 UPDATE runtime.unique_constraints x SET state='ABORTED',checkpoint=x.checkpoint+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
END $$;
CREATE FUNCTION runtime.maintain_unique_claims() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.unique_constraints%ROWTYPE;k text;old_key text;kh text;declared jsonb;fid text;
BEGIN
 -- Active bindings may declare integrity before a physical registry exists.
 SELECT convert_from(b.ir_body,'UTF8')::jsonb->'application' INTO declared FROM runtime.dataset_generations g JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id
 JOIN runtime.dataset_bindings b ON b.tenant_id=d.tenant_id AND b.app_id=d.app_id AND b.dataset_id=d.id AND b.version=d.active_binding
 WHERE g.tenant_id=NEW.tenant_id AND g.app_id=NEW.app_id AND g.anchor_revision=NEW.metadata_revision AND g.anchor_hash=NEW.ir_hash;
 IF declared IS NULL THEN SELECT m.document INTO declared FROM control.metadata_versions m WHERE m.tenant_id=NEW.tenant_id AND m.app_id=NEW.app_id AND m.revision=NEW.metadata_revision;END IF;
 FOR fid IN SELECT x->>'id' FROM jsonb_array_elements(declared->'definitions') x WHERE x->>'kind'='field' AND x->'field'->>'entity_ref'=NEW.entity_id AND x->'field'->'unique'='true'::jsonb LOOP
  IF NOT EXISTS(SELECT 1 FROM runtime.unique_constraints u WHERE u.tenant_id=NEW.tenant_id AND u.app_id=NEW.app_id AND u.metadata_revision=NEW.metadata_revision AND u.ir_hash=NEW.ir_hash AND u.entity_id=NEW.entity_id AND u.field_id=fid AND u.state='READY') THEN RAISE EXCEPTION 'unique integrity not ready' USING ERRCODE='55000';END IF;
 END LOOP;
 FOR idx IN SELECT x.* FROM runtime.unique_constraints x WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.metadata_revision=NEW.metadata_revision AND x.ir_hash=NEW.ir_hash AND x.entity_id=NEW.entity_id ORDER BY x.field_id FOR KEY SHARE LOOP
  IF idx.state='ABORTED' THEN
   IF session_user<>'framework_app' AND NOT pg_has_role(session_user,'framework_migrator','MEMBER') THEN RAISE EXCEPTION 'unique write denied' USING ERRCODE='42501';END IF;
   CONTINUE;
  END IF;
  IF idx.state<>'READY' THEN RAISE EXCEPTION 'unique integrity not ready' USING ERRCODE='55000';END IF;
  k:=runtime.unique_key(NEW.custom_data->idx.field_id,idx.field_type,idx.currency,idx.required,idx.enum_values);kh:=encode(sha256(convert_to(k,'UTF8')),'hex');
  old_key:=NULL;
  IF TG_OP='UPDATE' THEN old_key:=runtime.unique_key(OLD.custom_data->idx.field_id,idx.field_type,idx.currency,idx.required,idx.enum_values);END IF;
  IF k IS NOT NULL AND (TG_OP='INSERT' OR old_key IS DISTINCT FROM k) THEN
   IF NOT EXISTS(SELECT 1 FROM control.unique_write_context p WHERE p.backend=pg_backend_pid() AND p.transaction_id=pg_current_xact_id() AND p.actor_id=iam.authenticated_actor() AND p.tenant_id=NEW.tenant_id AND p.app_id=NEW.app_id AND p.metadata_revision=NEW.metadata_revision AND p.ir_hash=NEW.ir_hash AND p.entity_id=NEW.entity_id AND p.id=NEW.id AND p.field_id=idx.field_id AND p.generation=idx.generation AND p.value_hash=kh) THEN RAISE EXCEPTION 'unique write denied' USING ERRCODE='42501';END IF;
  END IF;
  DELETE FROM runtime.unique_claims p WHERE p.tenant_id=NEW.tenant_id AND p.app_id=NEW.app_id AND p.metadata_revision=NEW.metadata_revision AND p.ir_hash=NEW.ir_hash AND p.entity_id=NEW.entity_id AND p.field_id=idx.field_id AND p.generation=idx.generation AND p.id=NEW.id;
  INSERT INTO runtime.unique_claims VALUES(NEW.tenant_id,NEW.app_id,NEW.metadata_revision,NEW.ir_hash,NEW.entity_id,idx.field_id,idx.generation,NEW.id,k,kh,NEW.row_version);
 END LOOP;
 RETURN NEW;
END $$;
CREATE FUNCTION runtime.fence_unique_evolution() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE g runtime.dataset_generations%ROWTYPE;base jsonb;
BEGIN
 IF TG_OP='UPDATE' AND NEW.state<>'ACTIVATED' THEN RETURN NEW;END IF;
 SELECT x.* INTO g FROM runtime.dataset_generations x WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.dataset_id=NEW.dataset_id AND x.generation=NEW.base_generation;
 PERFORM 1 FROM runtime.record_sets r WHERE r.tenant_id=g.tenant_id AND r.app_id=g.app_id AND r.metadata_revision=g.anchor_revision AND r.ir_hash=g.anchor_hash FOR UPDATE;
 SELECT convert_from(b.ir_body,'UTF8')::jsonb INTO base FROM runtime.dataset_bindings b WHERE b.tenant_id=NEW.tenant_id AND b.app_id=NEW.app_id AND b.dataset_id=NEW.dataset_id AND b.version=NEW.base_binding;
 IF EXISTS(SELECT 1 FROM runtime.unique_constraints x WHERE x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.metadata_revision=g.anchor_revision AND x.ir_hash=g.anchor_hash AND x.state<>'ABORTED')
 OR jsonb_path_exists(base,'$.entities[*].fields[*] ? (@.unique == true)') OR jsonb_path_exists(convert_from(NEW.target_ir,'UTF8')::jsonb,'$.entities[*].fields[*] ? (@.unique == true)')
 THEN RAISE EXCEPTION 'constraint-preserving evolution unavailable' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions','runtime.scalar_indexes','runtime.scalar_projections','runtime.search_indexes','runtime.search_projections','runtime.unique_constraints','runtime.unique_claims','runtime.unique_constraint_history','control.unique_write_context') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='runtime' AND p.proname IN('unique_key','unique_authorized','authorize_unique_write','clear_unique_write','register_unique_constraint','build_unique_constraint','abort_unique_constraint','maintain_unique_claims','fence_unique_evolution') LOOP
  EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
  EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION runtime.authorize_unique_write(uuid,text,bigint,text,text,uuid,jsonb),runtime.clear_unique_write(),runtime.register_unique_constraint(uuid,text,bigint,text,text,text,text,bigint,text,bigint),runtime.build_unique_constraint(uuid,text,bigint,text,text,text,text,bigint,text,bigint,bigint),runtime.abort_unique_constraint(uuid,text,bigint,text,text,text,text,bigint,text,bigint,bigint) TO framework_app;
CREATE TRIGGER record_unique_claims AFTER INSERT OR UPDATE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.maintain_unique_claims();
CREATE TRIGGER dataset_unique_evolution BEFORE INSERT OR UPDATE ON runtime.dataset_evolutions FOR EACH ROW EXECUTE FUNCTION runtime.fence_unique_evolution();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain unique constraints, source records and evidence; reviewed forward recovery only';END $$;
-- +goose StatementEnd
