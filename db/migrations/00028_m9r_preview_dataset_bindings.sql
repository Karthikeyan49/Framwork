-- R2b additive preview identity/bindings. No record copy, overwrite or deletion.
-- Equal storage schemas only; incompatible generations remain a later gate.
-- +goose Up
CREATE TABLE runtime.datasets (
 tenant_id uuid NOT NULL,app_id text NOT NULL,id uuid NOT NULL,workspace_id uuid NOT NULL,
 environment text NOT NULL DEFAULT 'preview' CHECK(environment='preview'),
 anchor_revision bigint NOT NULL,anchor_hash text NOT NULL,
 schema_hash text NOT NULL CHECK(schema_hash ~ '^[0-9a-f]{64}$'),
 schema_generation bigint NOT NULL DEFAULT 1 CHECK(schema_generation=1),
 active_binding bigint NOT NULL DEFAULT 1 CHECK(active_binding>0),
 epoch bigint NOT NULL DEFAULT 1 CHECK(epoch>0),
 state text NOT NULL CHECK(state IN('ADOPTING','OPEN')),
 verified_entity text NOT NULL DEFAULT '',verified_id uuid,
 verified_rows bigint NOT NULL DEFAULT 0 CHECK(verified_rows>=0),
 manifest_hash text NOT NULL DEFAULT repeat('0',64) CHECK(manifest_hash ~ '^[0-9a-f]{64}$'),
 created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,id),UNIQUE(tenant_id,app_id,workspace_id),
 UNIQUE(tenant_id,app_id,anchor_revision,anchor_hash),
 FOREIGN KEY(tenant_id,app_id,anchor_revision,anchor_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash)
);
SELECT runtime.install_tenant_rls('runtime.datasets');
REVOKE ALL ON runtime.datasets FROM framework_app;
GRANT SELECT ON runtime.datasets TO framework_app;
GRANT INSERT(tenant_id,app_id,id,workspace_id,anchor_revision,anchor_hash,schema_hash,state) ON runtime.datasets TO framework_app;
GRANT UPDATE(active_binding,epoch,state,verified_entity,verified_id,verified_rows,manifest_hash) ON runtime.datasets TO framework_app;
CREATE TABLE runtime.dataset_bindings (
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,version bigint NOT NULL CHECK(version>0),
 metadata_revision bigint NOT NULL,ir_hash text NOT NULL CHECK(ir_hash ~ '^[0-9a-f]{64}$'),
 ir_body bytea NOT NULL CHECK(octet_length(ir_body) BETWEEN 1 AND 1048576 AND ir_hash=encode(sha256(ir_body),'hex')),
 schema_hash text NOT NULL CHECK(schema_hash ~ '^[0-9a-f]{64}$'),
 created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,dataset_id,version),
 FOREIGN KEY(tenant_id,app_id,dataset_id) REFERENCES runtime.datasets(tenant_id,app_id,id),
 FOREIGN KEY(tenant_id,app_id,metadata_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision)
);
SELECT runtime.install_tenant_rls('runtime.dataset_bindings');
REVOKE ALL ON runtime.dataset_bindings FROM framework_app;
GRANT SELECT ON runtime.dataset_bindings TO framework_app;
GRANT INSERT(tenant_id,app_id,dataset_id,version,metadata_revision,ir_hash,ir_body,schema_hash) ON runtime.dataset_bindings TO framework_app;
CREATE POLICY worker_no_generic ON runtime.datasets AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE POLICY worker_no_generic ON runtime.dataset_bindings AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER immutable_dataset_binding BEFORE UPDATE OR DELETE ON runtime.dataset_bindings FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
ALTER TABLE runtime.datasets ADD FOREIGN KEY(tenant_id,app_id,id,active_binding)
 REFERENCES runtime.dataset_bindings(tenant_id,app_id,dataset_id,version) DEFERRABLE INITIALLY DEFERRED;
-- Immutable identity and monotonically fenced pointer/checkpoints even when an
-- application is old. Runtime cannot replace anchors or undo adoption.
-- +goose StatementBegin
CREATE FUNCTION runtime.check_dataset_update() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF ROW(NEW.tenant_id,NEW.app_id,NEW.id,NEW.workspace_id,NEW.environment,NEW.anchor_revision,NEW.anchor_hash,NEW.schema_hash,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.tenant_id,OLD.app_id,OLD.id,OLD.workspace_id,OLD.environment,OLD.anchor_revision,OLD.anchor_hash,OLD.schema_hash,OLD.created_by,OLD.created_at)
 OR NEW.epoch<>OLD.epoch+1 OR NEW.active_binding<OLD.active_binding OR NEW.verified_rows<OLD.verified_rows
 OR (OLD.state='OPEN' AND NEW.state<>'OPEN') THEN RAISE EXCEPTION 'dataset identity/checkpoint is immutable' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.check_dataset_update() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.check_dataset_update() FROM PUBLIC;
CREATE TRIGGER dataset_update_guard BEFORE UPDATE ON runtime.datasets FOR EACH ROW EXECUTE FUNCTION runtime.check_dataset_update();
-- +goose StatementBegin
CREATE FUNCTION runtime.fence_dataset_record_write() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE rec runtime.records%ROWTYPE;d runtime.datasets%ROWTYPE;binding runtime.dataset_bindings%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN rec:=OLD;ELSE rec:=NEW;END IF;
 IF pg_has_role(session_user,'framework_migrator','MEMBER') THEN RETURN rec;END IF;
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(rec.tenant_id) THEN RAISE EXCEPTION 'trusted dataset writer required' USING ERRCODE='42501';END IF;
 -- Adoption increments this row while holding FOR UPDATE. A stale RR writer
 -- cannot miss a new mapping and continue through its older snapshot.
 PERFORM 1 FROM runtime.record_sets s WHERE s.tenant_id=rec.tenant_id AND s.app_id=rec.app_id
 AND s.metadata_revision=rec.metadata_revision AND s.ir_hash=rec.ir_hash FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'dataset anchor required' USING ERRCODE='55000';END IF;
 SELECT x.* INTO d FROM runtime.datasets x WHERE x.tenant_id=rec.tenant_id AND x.app_id=rec.app_id
 AND x.anchor_revision=rec.metadata_revision AND x.anchor_hash=rec.ir_hash FOR SHARE;
 IF NOT FOUND THEN RETURN rec;END IF;
 SELECT b.* INTO binding FROM runtime.dataset_bindings b WHERE b.tenant_id=d.tenant_id AND b.app_id=d.app_id AND b.dataset_id=d.id AND b.version=d.active_binding;
 IF d.state<>'OPEN' OR d.id::text IS DISTINCT FROM current_setting('app.dataset_id',true)
 OR d.active_binding::text IS DISTINCT FROM current_setting('app.dataset_binding',true)
 OR binding.ir_hash IS DISTINCT FROM current_setting('app.execution_ir_hash',true)
 THEN RAISE EXCEPTION 'dataset binding is stale or sealed' USING ERRCODE='55000';END IF;
 RETURN rec;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.fence_dataset_record_write() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.fence_dataset_record_write() FROM PUBLIC;
CREATE TRIGGER dataset_record_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.fence_dataset_record_write();
ALTER TABLE runtime.outbox_intents ADD data_revision bigint;
ALTER TABLE runtime.outbox_intents ADD data_hash text;
ALTER TABLE runtime.outbox_intents ADD CONSTRAINT event_data_anchor_pair CHECK((data_revision IS NULL AND data_hash IS NULL) OR (data_revision IS NOT NULL AND data_hash IS NOT NULL AND data_revision>0 AND data_hash ~ '^[0-9a-f]{64}$'));
ALTER TABLE runtime.outbox_intents ADD FOREIGN KEY(tenant_id,app_id,data_revision,data_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash);
GRANT INSERT(data_revision,data_hash) ON runtime.outbox_intents TO framework_app;
-- Keep envelope bytes/version and execution release semantics. Only the causal
-- lane uses the retained physical origin, preserving pre-adoption sequencing.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.freeze_event() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE k text;s bigint;b bytea;h text;BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(NEW.tenant_id) OR NEW.actor_id IS DISTINCT FROM iam.authenticated_actor()
 THEN RAISE EXCEPTION 'trusted event producer required';END IF;
 IF NEW.data_revision IS NOT NULL AND NOT EXISTS(SELECT 1 FROM runtime.datasets d JOIN runtime.dataset_bindings binding
 ON binding.tenant_id=d.tenant_id AND binding.app_id=d.app_id AND binding.dataset_id=d.id AND binding.version=d.active_binding
 WHERE d.tenant_id=NEW.tenant_id AND d.app_id=NEW.app_id AND d.anchor_revision=NEW.data_revision AND d.anchor_hash=NEW.data_hash
 AND d.state='OPEN' AND d.id::text=current_setting('app.dataset_id',true) AND d.active_binding::text=current_setting('app.dataset_binding',true)
 AND binding.metadata_revision=NEW.metadata_revision AND binding.ir_hash=NEW.ir_hash) THEN RAISE EXCEPTION 'event dataset binding required';END IF;
 k:=jsonb_build_array('m8-v1',NEW.app_id,coalesce(NEW.data_revision,NEW.metadata_revision),coalesce(NEW.data_hash,NEW.ir_hash),NEW.entity_id,NEW.record_id)::text;
 INSERT INTO runtime.event_sequences(tenant_id,aggregate_key,sequence) VALUES(NEW.tenant_id,k,1)
 ON CONFLICT(tenant_id,aggregate_key) DO UPDATE SET sequence=event_sequences.sequence+1 RETURNING sequence INTO s;
 b:=convert_to(jsonb_build_object('schema_version','1.0.0','epoch','m8-v1','mode','preview','topic','record.changes.v1',
 'id',NEW.id,'tenant_id',NEW.tenant_id,'actor_id',NEW.actor_id,'app_id',NEW.app_id,'metadata_revision',NEW.metadata_revision,
 'ir_hash',NEW.ir_hash,'aggregate_type',NEW.entity_id,'aggregate_id',NEW.record_id,'aggregate_sequence',s,
 'event_type',NEW.event_type,'occurred_at',to_char(NEW.occurred_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
 'request_id',NEW.request_id,'traceparent',NEW.traceparent,'payload',jsonb_build_object('record_id',NEW.record_id,'record_version',NEW.record_version))::text,'UTF8');
 h:=encode(sha256(b),'hex');
 INSERT INTO runtime.event_envelopes(tenant_id,id,body,body_hash) VALUES(NEW.tenant_id,NEW.id,b,h);
 INSERT INTO control.job_routes(tenant_id,id,actor_id,release_hash) VALUES(NEW.tenant_id,NEW.id,NEW.actor_id,NEW.ir_hash);
 INSERT INTO control.event_deliveries(tenant_id,id,body_hash,aggregate_key,sequence,state) VALUES(NEW.tenant_id,NEW.id,h,k,s,'PENDING');
 RETURN NEW;END $$;
-- +goose StatementEnd
-- Extend the existing worker drift guard to the new private binding catalog.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname||'.'||c.relname IN('runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings')
 AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain adopted data and immutable bindings; reviewed forward recovery only';END $$;
-- +goose StatementEnd
