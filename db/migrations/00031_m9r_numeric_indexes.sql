-- Governed numeric indexes: additive platform DDL, never tenant-selected DDL.
-- JSONB remains primary. Retain registry/checkpoints; forward recovery only.
-- +goose Up
CREATE TABLE runtime.numeric_indexes (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,schema_hash text NOT NULL CHECK(schema_hash ~ '^[0-9a-f]{64}$'),
 field_type text NOT NULL CHECK(field_type IN('integer','decimal','money')),currency text NOT NULL,required boolean NOT NULL,
 state text NOT NULL DEFAULT 'BUILDING' CHECK(state IN('BUILDING','READY')),
 checkpoint bigint NOT NULL DEFAULT 1 CHECK(checkpoint>0),after_id uuid,scanned_rows bigint NOT NULL DEFAULT 0,
 source_revision bigint NOT NULL,source_ir_hash text NOT NULL,created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash),
 FOREIGN KEY(tenant_id,app_id,source_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision),
 CHECK((field_type='money' AND currency ~ '^[A-Z]{3}$') OR (field_type<>'money' AND currency=''))
);
SELECT runtime.install_tenant_rls('runtime.numeric_indexes');
REVOKE ALL ON runtime.numeric_indexes FROM framework_app;
GRANT SELECT ON runtime.numeric_indexes TO framework_app;
CREATE POLICY worker_no_generic ON runtime.numeric_indexes AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE runtime.numeric_projections (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,id uuid NOT NULL,value_number numeric,source_version bigint NOT NULL CHECK(source_version>0),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) REFERENCES runtime.numeric_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE
);
SELECT runtime.install_tenant_rls('runtime.numeric_projections');
REVOKE ALL ON runtime.numeric_projections FROM framework_app;
GRANT SELECT ON runtime.numeric_projections TO framework_app;
CREATE POLICY worker_no_generic ON runtime.numeric_projections AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE INDEX numeric_projection_asc ON runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_number ASC NULLS LAST,id ASC);
CREATE INDEX numeric_projection_desc ON runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,value_number DESC NULLS LAST,id ASC);
CREATE TRIGGER retain_numeric_indexes BEFORE DELETE OR TRUNCATE ON runtime.numeric_indexes FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_numeric_projections BEFORE TRUNCATE ON runtime.numeric_projections FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- No caller can supply an expression, cast, currency or privilege declaration.
-- Source fields and active binding are checked independently in the database.
-- +goose StatementBegin
CREATE FUNCTION runtime.check_numeric_manager(t uuid,a text,v bigint,h text,sv bigint,sh text,schema text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;b runtime.dataset_bindings%ROWTYPE;
BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(t) OR NOT EXISTS(
 SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role IN('owner','admin'))
 THEN RAISE EXCEPTION 'index manager required' USING ERRCODE='42501';END IF;
 SELECT x.* INTO d FROM runtime.datasets x WHERE x.tenant_id=t AND x.app_id=a AND x.anchor_revision=v AND x.anchor_hash=h FOR SHARE;
 IF FOUND THEN
  SELECT x.* INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=d.id AND x.version=d.active_binding;
  IF d.state<>'OPEN' OR d.id::text IS DISTINCT FROM current_setting('app.dataset_id',true)
  OR d.active_binding::text IS DISTINCT FROM current_setting('app.dataset_binding',true)
  OR b.ir_hash IS DISTINCT FROM sh OR b.metadata_revision<>sv OR b.schema_hash IS DISTINCT FROM schema
  THEN RAISE EXCEPTION 'active index binding required' USING ERRCODE='55000';END IF;
 ELSE
  IF v<>sv OR h IS DISTINCT FROM sh THEN RAISE EXCEPTION 'exact source anchor required' USING ERRCODE='55000';END IF;
 END IF;
END $$;
CREATE FUNCTION runtime.numeric_value(raw jsonb,typ text,cur text,req boolean) RETURNS numeric
LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog AS $$
DECLARE s text;n numeric;
BEGIN
 IF raw IS NULL OR raw='null'::jsonb THEN
  IF req THEN RAISE EXCEPTION 'required indexed value missing' USING ERRCODE='22023';END IF;
  RETURN NULL;
 END IF;
 IF typ='integer' THEN
  IF jsonb_typeof(raw)<>'number' THEN RAISE EXCEPTION 'invalid indexed integer' USING ERRCODE='22023';END IF;
  n:=raw::text::numeric;
  IF n<>trunc(n) OR abs(n)>9007199254740991 THEN RAISE EXCEPTION 'indexed integer budget' USING ERRCODE='22023';END IF;
  RETURN n;
 ELSIF typ='money' THEN
  IF jsonb_typeof(raw)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(raw))<>2
  OR raw->>'currency' IS DISTINCT FROM cur OR jsonb_typeof(raw->'amount') IS DISTINCT FROM 'string'
  THEN RAISE EXCEPTION 'invalid indexed money' USING ERRCODE='22023';END IF;
  s:=raw->>'amount';
 ELSIF typ='decimal' AND jsonb_typeof(raw)='string' THEN s:=raw#>>'{}';
 ELSE RAISE EXCEPTION 'invalid indexed type' USING ERRCODE='22023';END IF;
 IF length(s)>64 OR s !~ '^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$' THEN RAISE EXCEPTION 'indexed decimal budget' USING ERRCODE='22023';END IF;
 n:=s::numeric;
 IF abs(n)>1000000000000000000 THEN RAISE EXCEPTION 'indexed decimal range' USING ERRCODE='22023';END IF;
 RETURN n;
END $$;
CREATE FUNCTION runtime.register_numeric_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text) RETURNS void
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
 IF (SELECT count(*) FROM runtime.numeric_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h)>=32
 OR (SELECT count(*) FROM runtime.numeric_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e)>=8
 THEN RAISE EXCEPTION 'index budget' USING ERRCODE='22023';END IF;
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
 INSERT INTO runtime.numeric_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,currency,required,source_revision,source_ir_hash)
 VALUES(t,a,v,h,e,f,schema,typ,cur,req,sv,sh);
END $$;
CREATE FUNCTION runtime.maintain_numeric_projection() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.numeric_indexes%ROWTYPE;n numeric;
BEGIN
 FOR idx IN SELECT x.* FROM runtime.numeric_indexes x WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.metadata_revision=NEW.metadata_revision AND x.ir_hash=NEW.ir_hash AND x.entity_id=NEW.entity_id LOOP
  n:=runtime.numeric_value(NEW.custom_data->idx.field_id,idx.field_type,idx.currency,idx.required);
  INSERT INTO runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_number,source_version)
  VALUES(NEW.tenant_id,NEW.app_id,NEW.metadata_revision,NEW.ir_hash,NEW.entity_id,idx.field_id,NEW.id,n,NEW.row_version)
  ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET value_number=excluded.value_number,source_version=excluded.source_version;
 END LOOP;
 RETURN NEW;
END $$;
CREATE FUNCTION runtime.build_numeric_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.numeric_indexes%ROWTYPE;rec runtime.records%ROWTYPE;n numeric;last_id uuid;scanned bigint:=0;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT x.* INTO idx FROM runtime.numeric_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema THEN RAISE EXCEPTION 'index witness absent' USING ERRCODE='55000';END IF;
 -- A retried checkpoint after a lost response observes progress, never repeats
 -- an unbounded implicit build or silently skips a requested checkpoint.
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 FOR rec IN SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
 AND (idx.after_id IS NULL OR r.id>idx.after_id) ORDER BY r.id LIMIT 1000 FOR SHARE LOOP
  n:=runtime.numeric_value(rec.custom_data->f,idx.field_type,idx.currency,idx.required);
  INSERT INTO runtime.numeric_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_number,source_version)
  VALUES(t,a,v,h,e,f,rec.id,n,rec.row_version)
  ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET value_number=excluded.value_number,source_version=excluded.source_version
  WHERE numeric_projections.source_version<=excluded.source_version;
  last_id:=rec.id;scanned:=scanned+1;
 END LOOP;
 IF scanned<1000 THEN
  -- Trigger maintenance protects already-scanned rows and inserts behind the
  -- cursor. Final coverage/value/version verification fails closed. The normal
  -- statement budget bounds this scan; a timeout never makes an index READY.
  IF EXISTS(SELECT 1 FROM runtime.records r LEFT JOIN runtime.numeric_projections p
   ON p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.id=r.id
   WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
   AND (p.id IS NULL OR p.source_version<>r.row_version OR p.value_number IS DISTINCT FROM runtime.numeric_value(r.custom_data->f,idx.field_type,idx.currency,idx.required)))
  THEN RAISE EXCEPTION 'index coverage mismatch' USING ERRCODE='55000';END IF;
 END IF;
 UPDATE runtime.numeric_indexes x SET after_id=coalesce(last_id,idx.after_id),scanned_rows=x.scanned_rows+scanned,checkpoint=x.checkpoint+1,state=CASE WHEN scanned<1000 THEN 'READY' ELSE 'BUILDING' END
 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.check_numeric_manager(uuid,text,bigint,text,bigint,text,text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.numeric_value(jsonb,text,text,boolean) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.register_numeric_index(uuid,text,bigint,text,text,text,text,bigint,text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.maintain_numeric_projection() OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.build_numeric_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.check_numeric_manager(uuid,text,bigint,text,bigint,text,text),runtime.numeric_value(jsonb,text,text,boolean),runtime.register_numeric_index(uuid,text,bigint,text,text,text,text,bigint,text),runtime.maintain_numeric_projection(),runtime.build_numeric_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.register_numeric_index(uuid,text,bigint,text,text,text,text,bigint,text),runtime.build_numeric_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) TO framework_app;
CREATE TRIGGER record_numeric_projection AFTER INSERT OR UPDATE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.maintain_numeric_projection();
-- Extend the authenticated worker's negative catalog; no generic grants.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname||'.'||c.relname IN('runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections')
 AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain index witnesses and records; reviewed forward recovery only';END $$;
-- +goose StatementEnd
