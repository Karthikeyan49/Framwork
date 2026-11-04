-- Governed fixed search projections. Preserve source records, all old indexes and IR.
-- +goose Up
CREATE TABLE runtime.search_indexes (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,schema_hash text NOT NULL CHECK(schema_hash ~ '^[0-9a-f]{64}$'),
 field_type text NOT NULL DEFAULT 'string' CHECK(field_type='string'),required boolean NOT NULL,parser_hash text NOT NULL CHECK(parser_hash ~ '^[0-9a-f]{64}$'),
 state text NOT NULL DEFAULT 'BUILDING' CHECK(state IN('BUILDING','VERIFYING','READY')),
 checkpoint bigint NOT NULL DEFAULT 1 CHECK(checkpoint>0),after_id uuid,verification_after uuid,
 scanned_rows bigint NOT NULL DEFAULT 0 CHECK(scanned_rows>=0),verified_rows bigint NOT NULL DEFAULT 0 CHECK(verified_rows>=0),
 source_revision bigint NOT NULL,source_ir_hash text NOT NULL,created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(field_id <> 'field.' || substring(entity_id FROM 8) || '.id'),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash),
 FOREIGN KEY(tenant_id,app_id,source_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision)
);
SELECT runtime.install_tenant_rls('runtime.search_indexes');
REVOKE ALL ON runtime.search_indexes FROM framework_app;
GRANT SELECT ON runtime.search_indexes TO framework_app;
CREATE POLICY worker_no_generic ON runtime.search_indexes AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE runtime.search_projections (
 tenant_id uuid NOT NULL,app_id text NOT NULL,metadata_revision bigint NOT NULL,ir_hash text NOT NULL,
 entity_id text NOT NULL,field_id text NOT NULL,id uuid NOT NULL,value_search tsvector NOT NULL,source_version bigint NOT NULL CHECK(source_version>0),
 PRIMARY KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id) REFERENCES runtime.search_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) REFERENCES runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id) ON DELETE CASCADE
);
SELECT runtime.install_tenant_rls('runtime.search_projections');
REVOKE ALL ON runtime.search_projections FROM framework_app;
GRANT SELECT ON runtime.search_projections TO framework_app;
CREATE POLICY worker_no_generic ON runtime.search_projections AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE INDEX search_terms ON runtime.search_projections USING gin(value_search);
CREATE TRIGGER retain_search_indexes BEFORE DELETE OR TRUNCATE ON runtime.search_indexes FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_search_projections BEFORE TRUNCATE ON runtime.search_projections FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
CREATE FUNCTION runtime.search_context() RETURNS tsquery LANGUAGE plpgsql STABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE s text:=current_setting('app.search_text',true);q tsquery;
BEGIN
 IF s IS NULL OR octet_length(s)<1 OR octet_length(s)>256 THEN RETURN ''::tsquery;END IF;
 q:=plainto_tsquery('pg_catalog.simple'::regconfig,s);
 IF numnode(q)<1 OR numnode(q)>31 THEN RETURN ''::tsquery;END IF;
 RETURN q;
END $$;
CREATE FUNCTION runtime.search_parser_hash() RETURNS text LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('contract','simple-v1','server',current_setting('server_version_num'),
 'encoding',d.encoding,'provider',d.datlocprovider,'locale',d.datlocale,'ctype',d.datctype,'collate',d.datcollate,
 'reported',d.datcollversion,'actual',pg_database_collation_actual_version(d.oid),
 'config',c.oid,'parser',c.cfgparser,'parser_start',p.prsstart,'parser_token',p.prstoken,'parser_end',p.prsend,'parser_types',p.prslextype,
 'maps',(SELECT jsonb_agg(jsonb_build_array(m.maptokentype,m.mapseqno,m.mapdict,x.dicttemplate,x.dictinitoption,t.tmplinit,t.tmpllexize) ORDER BY m.maptokentype,m.mapseqno)
 FROM pg_ts_config_map m JOIN pg_ts_dict x ON x.oid=m.mapdict JOIN pg_ts_template t ON t.oid=x.dicttemplate WHERE m.mapcfg=c.oid))::text,'UTF8')),'hex')
 FROM pg_database d CROSS JOIN pg_ts_config c JOIN pg_ts_parser p ON p.oid=c.cfgparser
 WHERE d.datname=current_database() AND c.oid='pg_catalog.simple'::regconfig AND c.cfgparser=(SELECT oid FROM pg_ts_parser WHERE prsnamespace='pg_catalog'::regnamespace AND prsname='default')
 AND d.datcollversion IS NOT DISTINCT FROM pg_database_collation_actual_version(d.oid)
 AND EXISTS(SELECT 1 FROM pg_ts_config_map m WHERE m.mapcfg=c.oid)
 AND NOT EXISTS(SELECT 1 FROM pg_ts_config_map m JOIN pg_ts_dict x ON x.oid=m.mapdict WHERE m.mapcfg=c.oid AND (x.oid<>'pg_catalog.simple'::regdictionary OR x.dictinitoption IS NOT NULL OR x.dicttemplate<>(SELECT oid FROM pg_ts_template WHERE tmplnamespace='pg_catalog'::regnamespace AND tmplname='simple')))
$$;
CREATE FUNCTION runtime.search_value(raw jsonb,req boolean) RETURNS tsvector LANGUAGE plpgsql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
DECLARE s text;
BEGIN
 IF req IS NULL THEN RAISE EXCEPTION 'invalid search contract' USING ERRCODE='22023';END IF;
 IF raw IS NULL OR raw='null'::jsonb THEN
  IF req THEN RAISE EXCEPTION 'required search value missing' USING ERRCODE='22023';END IF;
  RETURN ''::tsvector;
 END IF;
 IF jsonb_typeof(raw)<>'string' THEN RAISE EXCEPTION 'invalid search value' USING ERRCODE='22023';END IF;
 s:=raw#>>'{}';
 IF octet_length(s)>4096 OR (req AND s='') THEN RAISE EXCEPTION 'search value budget' USING ERRCODE='22023';END IF;
 RETURN to_tsvector('pg_catalog.simple'::regconfig,s);
END $$;
CREATE OR REPLACE FUNCTION runtime.check_index_budget(t uuid,a text,v bigint,h text,e text) RETURNS void
LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF (SELECT count(*) FROM (SELECT entity_id FROM runtime.numeric_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h UNION ALL SELECT entity_id FROM runtime.scalar_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h UNION ALL SELECT entity_id FROM runtime.search_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h) x)>=32
 OR (SELECT count(*) FROM (SELECT entity_id FROM runtime.numeric_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e UNION ALL SELECT entity_id FROM runtime.scalar_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e UNION ALL SELECT entity_id FROM runtime.search_indexes WHERE tenant_id=t AND app_id=a AND metadata_revision=v AND ir_hash=h AND entity_id=e) x)>=8
 THEN RAISE EXCEPTION 'index budget' USING ERRCODE='22023';END IF;
END $$;
CREATE OR REPLACE FUNCTION runtime.register_search_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE definition jsonb;idx runtime.search_indexes%ROWTYPE;typ text;req boolean;parser text;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 IF f='field.' || substring(e FROM 8) || '.id' THEN RAISE EXCEPTION 'unsupported intrinsic index field' USING ERRCODE='22023';END IF;
 SELECT item->'field' INTO definition FROM control.metadata_versions m CROSS JOIN LATERAL jsonb_array_elements(m.document->'definitions') item
 WHERE m.tenant_id=t AND m.app_id=a AND m.revision=sv AND item->>'kind'='field' AND item->>'id'=f AND item->'field'->>'entity_ref'=e
 AND EXISTS(SELECT 1 FROM jsonb_array_elements(m.document->'definitions') ent WHERE ent->>'id'=e AND ent->>'kind'='entity' AND ent->'entity'->'fields' ? f);
 typ:=definition->>'type';req:=(definition->>'required')::boolean;
 IF typ IS DISTINCT FROM 'string' OR req IS NULL OR schema IS NULL OR schema !~ '^[0-9a-f]{64}$' OR definition ? 'enum_values' OR definition ? 'reference_to'
 THEN RAISE EXCEPTION 'unsupported search field' USING ERRCODE='22023';END IF;
 parser:=runtime.search_parser_hash();IF parser IS NULL THEN RAISE EXCEPTION 'unsupported search parser' USING ERRCODE='55000';END IF;
 INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES(t,a,v,h) ON CONFLICT DO NOTHING;
 PERFORM 1 FROM runtime.record_sets x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h FOR UPDATE;
 SELECT x.* INTO idx FROM runtime.search_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
 IF FOUND THEN
  IF ROW(idx.schema_hash,idx.field_type,idx.required,idx.parser_hash) IS DISTINCT FROM ROW(schema,typ,req,parser)
  THEN RAISE EXCEPTION 'index witness mismatch' USING ERRCODE='55000';END IF;
  RETURN;
 END IF;
 PERFORM runtime.check_index_budget(t,a,v,h,e);
 UPDATE runtime.record_sets x SET write_version=x.write_version+1 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h;
 INSERT INTO runtime.search_indexes(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,schema_hash,field_type,required,parser_hash,source_revision,source_ir_hash)
 VALUES(t,a,v,h,e,f,schema,typ,req,parser,sv,sh);
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION runtime.maintain_search_projection() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.search_indexes%ROWTYPE;val tsvector;parser text;
BEGIN
 FOR idx IN SELECT x.* FROM runtime.search_indexes x WHERE x.tenant_id=NEW.tenant_id AND x.app_id=NEW.app_id AND x.metadata_revision=NEW.metadata_revision AND x.ir_hash=NEW.ir_hash AND x.entity_id=NEW.entity_id LOOP
  IF parser IS NULL THEN parser:=runtime.search_parser_hash();END IF;
  IF idx.parser_hash IS DISTINCT FROM parser THEN RAISE EXCEPTION 'search parser witness changed' USING ERRCODE='55000';END IF;
  val:=runtime.search_value(NEW.custom_data->idx.field_id,idx.required);
  INSERT INTO runtime.search_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_search,source_version)
  VALUES(NEW.tenant_id,NEW.app_id,NEW.metadata_revision,NEW.ir_hash,NEW.entity_id,idx.field_id,NEW.id,val,NEW.row_version)
  ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET value_search=excluded.value_search,source_version=excluded.source_version;
 END LOOP;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION runtime.build_search_index(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE idx runtime.search_indexes%ROWTYPE;rec record;val tsvector;last_id uuid;scanned bigint:=0;after_cursor uuid;
BEGIN
 PERFORM runtime.check_numeric_manager(t,a,v,h,sv,sh,schema);
 SELECT x.* INTO idx FROM runtime.search_indexes x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f FOR NO KEY UPDATE;
 IF NOT FOUND OR idx.schema_hash IS DISTINCT FROM schema OR idx.parser_hash IS DISTINCT FROM runtime.search_parser_hash() THEN RAISE EXCEPTION 'index witness absent or stale' USING ERRCODE='55000';END IF;
 IF idx.checkpoint<>expected OR idx.state='READY' THEN RETURN;END IF;
 after_cursor:=CASE WHEN idx.state='BUILDING' THEN idx.after_id ELSE idx.verification_after END;
 FOR rec IN WITH batch AS MATERIALIZED (
  SELECT r.* FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=v AND r.ir_hash=h AND r.entity_id=e
  AND (after_cursor IS NULL OR r.id>after_cursor) ORDER BY r.id LIMIT 1000 FOR SHARE
 ) SELECT r.*,p.source_version projection_version,p.value_search projection_search FROM batch r LEFT JOIN runtime.search_projections p
 ON idx.state='VERIFYING' AND p.tenant_id=t AND p.app_id=a AND p.metadata_revision=v AND p.ir_hash=h AND p.entity_id=e AND p.field_id=f AND p.id=r.id ORDER BY r.id LOOP
  val:=runtime.search_value(rec.custom_data->f,idx.required);
  IF idx.state='BUILDING' THEN
   INSERT INTO runtime.search_projections(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id,value_search,source_version)
   VALUES(t,a,v,h,e,f,rec.id,val,rec.row_version)
   ON CONFLICT(tenant_id,app_id,metadata_revision,ir_hash,entity_id,field_id,id) DO UPDATE SET value_search=excluded.value_search,source_version=excluded.source_version
   WHERE search_projections.source_version<=excluded.source_version;
  ELSE
   IF rec.projection_version IS NULL OR rec.projection_version<>rec.row_version OR rec.projection_search IS DISTINCT FROM val THEN RAISE EXCEPTION 'index coverage mismatch' USING ERRCODE='55000';END IF;
  END IF;
  last_id:=rec.id;scanned:=scanned+1;
 END LOOP;
 UPDATE runtime.search_indexes x SET after_id=CASE WHEN idx.state='BUILDING' THEN coalesce(last_id,idx.after_id) ELSE idx.after_id END,
 verification_after=CASE WHEN idx.state='VERIFYING' THEN coalesce(last_id,idx.verification_after) ELSE idx.verification_after END,
 scanned_rows=x.scanned_rows+CASE WHEN idx.state='BUILDING' THEN scanned ELSE 0 END,verified_rows=x.verified_rows+CASE WHEN idx.state='VERIFYING' THEN scanned ELSE 0 END,
 checkpoint=x.checkpoint+1,state=CASE WHEN scanned=1000 THEN idx.state WHEN idx.state='BUILDING' THEN 'VERIFYING' ELSE 'READY' END
 WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=v AND x.ir_hash=h AND x.entity_id=e AND x.field_id=f;
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions','runtime.scalar_indexes','runtime.scalar_projections','runtime.search_indexes','runtime.search_projections') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
ALTER FUNCTION runtime.search_context() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.search_context() FROM PUBLIC;
ALTER FUNCTION runtime.search_parser_hash() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.search_parser_hash() FROM PUBLIC;
ALTER FUNCTION runtime.search_value(jsonb,boolean) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.search_value(jsonb,boolean) FROM PUBLIC;
ALTER FUNCTION runtime.register_search_index(uuid,text,bigint,text,text,text,text,bigint,text) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.register_search_index(uuid,text,bigint,text,text,text,text,bigint,text) FROM PUBLIC;
ALTER FUNCTION runtime.maintain_search_projection() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.maintain_search_projection() FROM PUBLIC;
ALTER FUNCTION runtime.build_search_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.build_search_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.search_context(),runtime.search_parser_hash(),runtime.register_search_index(uuid,text,bigint,text,text,text,text,bigint,text),runtime.build_search_index(uuid,text,bigint,text,text,text,text,bigint,text,bigint) TO framework_app;
CREATE POLICY search_match ON runtime.search_projections AS RESTRICTIVE TO framework_app USING(value_search @@ (SELECT runtime.search_context()));
CREATE TRIGGER record_search_projection AFTER INSERT OR UPDATE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.maintain_search_projection();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain search witnesses and source records; reviewed forward recovery only';END $$;
-- +goose StatementEnd
