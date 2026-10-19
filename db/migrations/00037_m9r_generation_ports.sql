-- Generation-aware trusted ports; generic worker grants remain absent.
-- +goose Up
ALTER TABLE runtime.workflow_bindings ADD dataset_id uuid;
ALTER TABLE runtime.workflow_bindings ADD binding_version bigint;
ALTER TABLE runtime.workflow_bindings ADD data_generation bigint;
ALTER TABLE runtime.workflow_bindings ADD data_revision bigint;
ALTER TABLE runtime.workflow_bindings ADD data_hash text;
ALTER TABLE runtime.workflow_bindings ADD CHECK((dataset_id IS NULL AND binding_version IS NULL AND data_generation IS NULL AND data_revision IS NULL AND data_hash IS NULL) OR (dataset_id IS NOT NULL AND binding_version IS NOT NULL AND data_generation IS NOT NULL AND data_revision IS NOT NULL AND data_hash IS NOT NULL AND binding_version>0 AND data_generation>0 AND data_revision>0 AND data_hash ~ '^[0-9a-f]{64}$'));
ALTER TABLE runtime.workflow_bindings ADD FOREIGN KEY(tenant_id,app_id,dataset_id,binding_version) REFERENCES runtime.dataset_bindings(tenant_id,app_id,dataset_id,version);
ALTER TABLE runtime.workflow_bindings ADD FOREIGN KEY(tenant_id,app_id,dataset_id,data_generation) REFERENCES runtime.dataset_generations(tenant_id,app_id,dataset_id,generation);
GRANT INSERT(dataset_id,binding_version,data_generation,data_revision,data_hash) ON runtime.workflow_bindings TO framework_app;
CREATE TABLE runtime.event_data_sources(
 tenant_id uuid NOT NULL,id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,generation bigint NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,id) REFERENCES runtime.outbox_intents(tenant_id,id),
 FOREIGN KEY(tenant_id,app_id,dataset_id,generation) REFERENCES runtime.dataset_generations(tenant_id,app_id,dataset_id,generation)
);
SELECT runtime.install_tenant_rls('runtime.event_data_sources');
REVOKE INSERT,UPDATE,DELETE ON runtime.event_data_sources FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.event_data_sources AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER immutable_event_source BEFORE UPDATE OR DELETE OR TRUNCATE ON runtime.event_data_sources FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
CREATE FUNCTION runtime.check_workflow_data_pin() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d record;b runtime.dataset_bindings%ROWTYPE;
BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(NEW.tenant_id) THEN RAISE EXCEPTION 'trusted workflow producer required' USING ERRCODE='42501';END IF;
 SELECT x.*,g.generation AS mapped_generation,g.anchor_revision AS mapped_revision,g.anchor_hash AS mapped_hash INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id
 WHERE g.tenant_id=NEW.tenant_id AND g.app_id=NEW.app_id AND g.anchor_revision=coalesce(NEW.data_revision,NEW.metadata_revision) AND g.anchor_hash=coalesce(NEW.data_hash,NEW.ir_hash) FOR SHARE OF x;
 IF NOT FOUND THEN
  IF NEW.dataset_id IS NOT NULL THEN RAISE EXCEPTION 'workflow data mapping missing' USING ERRCODE='55000';END IF;
  RETURN NEW;
 END IF;
 SELECT x.* INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=d.tenant_id AND x.app_id=d.app_id AND x.dataset_id=d.id AND x.version=d.active_binding;
 IF d.state<>'OPEN' OR NEW.dataset_id IS DISTINCT FROM d.id OR NEW.binding_version IS DISTINCT FROM d.active_binding OR NEW.data_generation IS DISTINCT FROM d.schema_generation OR NEW.data_generation IS DISTINCT FROM d.mapped_generation
 OR NEW.metadata_revision<>b.metadata_revision OR NEW.ir_hash<>b.ir_hash OR NEW.ir_body<>b.ir_body
 OR d.id::text IS DISTINCT FROM current_setting('app.dataset_id',true) OR d.active_binding::text IS DISTINCT FROM current_setting('app.dataset_binding',true)
 THEN RAISE EXCEPTION 'active immutable workflow data pin required' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION runtime.check_numeric_manager(t uuid,a text,v bigint,h text,sv bigint,sh text,schema text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d record;b runtime.dataset_bindings%ROWTYPE;
BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(t) OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role IN('owner','admin')) THEN RAISE EXCEPTION 'index manager required' USING ERRCODE='42501';END IF;
 SELECT x.*,g.generation AS mapped_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id WHERE g.tenant_id=t AND g.app_id=a AND g.anchor_revision=v AND g.anchor_hash=h FOR SHARE OF x;
 IF FOUND THEN
  SELECT x.* INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=d.id AND x.version=d.active_binding;
  IF d.state<>'OPEN' OR d.mapped_generation<>d.schema_generation OR d.id::text IS DISTINCT FROM current_setting('app.dataset_id',true) OR d.active_binding::text IS DISTINCT FROM current_setting('app.dataset_binding',true) OR b.ir_hash IS DISTINCT FROM sh OR b.metadata_revision<>sv OR b.schema_hash IS DISTINCT FROM schema THEN RAISE EXCEPTION 'active index binding required' USING ERRCODE='55000';END IF;
 ELSE
  IF v<>sv OR h IS DISTINCT FROM sh THEN RAISE EXCEPTION 'exact source anchor required' USING ERRCODE='55000';END IF;
 END IF;
END $$;
CREATE OR REPLACE FUNCTION runtime.freeze_event() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE k text;s bigint;b bytea;h text;d record;origin_revision bigint;origin_hash text;
BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(NEW.tenant_id) OR NEW.actor_id IS DISTINCT FROM iam.authenticated_actor() THEN RAISE EXCEPTION 'trusted event producer required';END IF;
 origin_revision:=NEW.metadata_revision;origin_hash:=NEW.ir_hash;
 IF NEW.data_revision IS NOT NULL THEN
  SELECT x.*,g.generation AS mapped_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id
  JOIN runtime.dataset_bindings binding ON binding.tenant_id=x.tenant_id AND binding.app_id=x.app_id AND binding.dataset_id=x.id AND binding.version=x.active_binding
  WHERE g.tenant_id=NEW.tenant_id AND g.app_id=NEW.app_id AND g.anchor_revision=NEW.data_revision AND g.anchor_hash=NEW.data_hash AND g.generation=x.schema_generation AND binding.generation=g.generation
  AND x.state='OPEN' AND x.id::text=current_setting('app.dataset_id',true) AND x.active_binding::text=current_setting('app.dataset_binding',true) AND binding.metadata_revision=NEW.metadata_revision AND binding.ir_hash=NEW.ir_hash;
  IF NOT FOUND THEN RAISE EXCEPTION 'event dataset binding required';END IF;
  origin_revision:=d.anchor_revision;origin_hash:=d.anchor_hash;
  INSERT INTO runtime.event_data_sources VALUES(NEW.tenant_id,NEW.id,NEW.app_id,d.id,d.mapped_generation);
 END IF;
 k:=jsonb_build_array('m8-v1',NEW.app_id,origin_revision,origin_hash,NEW.entity_id,NEW.record_id)::text;
 INSERT INTO runtime.event_sequences(tenant_id,aggregate_key,sequence) VALUES(NEW.tenant_id,k,1) ON CONFLICT(tenant_id,aggregate_key) DO UPDATE SET sequence=event_sequences.sequence+1 RETURNING sequence INTO s;
 b:=convert_to(jsonb_build_object('schema_version','1.0.0','epoch','m8-v1','mode','preview','topic','record.changes.v1','id',NEW.id,'tenant_id',NEW.tenant_id,'actor_id',NEW.actor_id,'app_id',NEW.app_id,'metadata_revision',NEW.metadata_revision,'ir_hash',NEW.ir_hash,'aggregate_type',NEW.entity_id,'aggregate_id',NEW.record_id,'aggregate_sequence',s,'event_type',NEW.event_type,'occurred_at',to_char(NEW.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),'request_id',NEW.request_id,'traceparent',NEW.traceparent,'payload',jsonb_build_object('record_id',NEW.record_id,'record_version',NEW.record_version))::text,'UTF8');
 h:=encode(sha256(b),'hex');
 INSERT INTO runtime.event_envelopes(tenant_id,id,body,body_hash) VALUES(NEW.tenant_id,NEW.id,b,h);
 INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES(NEW.tenant_id,NEW.id,NEW.actor_id,NEW.ir_hash);
 INSERT INTO control.event_deliveries(tenant_id,id,body_hash,aggregate_key,sequence,state) VALUES(NEW.tenant_id,NEW.id,h,k,s,'PENDING');
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION runtime.worker_source_record() RETURNS SETOF runtime.records LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE b runtime.workflow_bindings%ROWTYPE;rec runtime.records%ROWTYPE;d record;c runtime.record_identity%ROWTYPE;
BEGIN
 SELECT w.* INTO b FROM control.worker_authority() a JOIN runtime.workflow_bindings w ON w.tenant_id=a.tenant_id AND w.id=a.job_id WHERE a.kind='workflow';IF NOT FOUND THEN RETURN;END IF;
 SELECT r.* INTO rec FROM runtime.records r WHERE r.tenant_id=b.tenant_id AND r.app_id=b.app_id AND r.metadata_revision=coalesce(b.data_revision,b.metadata_revision) AND r.ir_hash=coalesce(b.data_hash,b.ir_hash) AND r.entity_id=(convert_from(b.plan_body,'UTF8')::jsonb->>'entity_id') AND r.id=b.record_id;
 IF NOT FOUND THEN RETURN;END IF;
 SELECT x.id,x.canonical_access INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id WHERE g.tenant_id=rec.tenant_id AND g.app_id=rec.app_id AND g.anchor_revision=rec.metadata_revision AND g.anchor_hash=rec.ir_hash;
 IF FOUND AND d.canonical_access THEN
  SELECT x.* INTO c FROM runtime.record_identity x WHERE x.tenant_id=rec.tenant_id AND x.app_id=rec.app_id AND x.dataset_id=d.id AND x.entity_id=rec.entity_id AND x.id=rec.id AND NOT x.tombstone;
  IF NOT FOUND THEN RETURN;END IF;rec.owner_user_id:=c.owner_user_id;rec.owner_team_id:=c.owner_team_id;
 END IF;
 SELECT coalesce(jsonb_object_agg(f.key,f.value),'{}'::jsonb) INTO rec.custom_data FROM jsonb_each(rec.custom_data) f WHERE f.key IN(SELECT jsonb_array_elements_text(convert_from(b.inputs,'UTF8')::jsonb->'fields'));
 RETURN NEXT rec;
END $$;
CREATE FUNCTION runtime.worker_source_shares() RETURNS SETOF runtime.record_shares LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT s.* FROM runtime.read_shares s JOIN runtime.worker_source_record() r ON r.tenant_id=s.tenant_id AND r.app_id=s.app_id AND r.metadata_revision=s.metadata_revision AND r.ir_hash=s.ir_hash AND r.entity_id=s.entity_id AND r.id=s.id
 JOIN control.worker_authority() a ON a.tenant_id=s.tenant_id AND a.actor_id=s.user_id WHERE a.kind='workflow'
$$;
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
ALTER FUNCTION runtime.check_workflow_data_pin() OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.worker_source_shares() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.check_workflow_data_pin(),runtime.worker_source_shares() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.worker_source_shares() TO framework_worker;
REVOKE ALL ON runtime.record_shares FROM framework_worker;
CREATE TRIGGER workflow_data_pin BEFORE INSERT ON runtime.workflow_bindings FOR EACH ROW EXECUTE FUNCTION runtime.check_workflow_data_pin();
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain workflow pins and causal origins; forward recovery only';END $$;
-- +goose StatementEnd
