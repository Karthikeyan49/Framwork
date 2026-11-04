-- Additive generation/evolution storage. Original records/anchors are retained.
-- No customer backfill runs in DDL; verification/copy use bounded API batches.
-- +goose Up
ALTER TABLE runtime.datasets DROP CONSTRAINT datasets_schema_generation_check;
ALTER TABLE runtime.datasets ADD CONSTRAINT datasets_schema_generation_check CHECK(schema_generation>0);
ALTER TABLE runtime.datasets DROP CONSTRAINT datasets_state_check;
ALTER TABLE runtime.datasets ADD CONSTRAINT datasets_state_check CHECK(state IN('ADOPTING','OPEN','FROZEN'));
ALTER TABLE runtime.datasets ADD canonical_access boolean NOT NULL DEFAULT false;
ALTER TABLE runtime.datasets ADD fact_epoch bigint NOT NULL DEFAULT 1 CHECK(fact_epoch>0);
CREATE TABLE runtime.dataset_generations(
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,generation bigint NOT NULL CHECK(generation>0),
 anchor_revision bigint NOT NULL,anchor_hash text NOT NULL,origin_schema_hash text NOT NULL CHECK(origin_schema_hash ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,app_id,dataset_id,generation),UNIQUE(tenant_id,app_id,anchor_revision,anchor_hash),
 FOREIGN KEY(tenant_id,app_id,dataset_id) REFERENCES runtime.datasets(tenant_id,app_id,id),
 FOREIGN KEY(tenant_id,app_id,anchor_revision,anchor_hash) REFERENCES runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash)
);
SELECT runtime.install_tenant_rls('runtime.dataset_generations');
REVOKE INSERT,UPDATE,DELETE ON runtime.dataset_generations FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.dataset_generations AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER immutable_generation BEFORE UPDATE OR DELETE OR TRUNCATE ON runtime.dataset_generations FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
INSERT INTO runtime.dataset_generations SELECT tenant_id,app_id,id,1,anchor_revision,anchor_hash,schema_hash FROM runtime.datasets;
ALTER TABLE runtime.dataset_bindings ADD generation bigint NOT NULL DEFAULT 1;
ALTER TABLE runtime.dataset_bindings ADD FOREIGN KEY(tenant_id,app_id,dataset_id,generation) REFERENCES runtime.dataset_generations(tenant_id,app_id,dataset_id,generation);
GRANT INSERT(generation) ON runtime.dataset_bindings TO framework_app;
CREATE TABLE runtime.record_identity(
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,entity_id text NOT NULL,id uuid NOT NULL,
 owner_user_id uuid NOT NULL,owner_team_id uuid,tombstone boolean NOT NULL DEFAULT false,
 PRIMARY KEY(tenant_id,app_id,dataset_id,entity_id,id),
 FOREIGN KEY(tenant_id,app_id,dataset_id) REFERENCES runtime.datasets(tenant_id,app_id,id),
 FOREIGN KEY(tenant_id,owner_team_id) REFERENCES iam.teams(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.record_identity');
REVOKE INSERT,UPDATE,DELETE ON runtime.record_identity FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.record_identity AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER retain_record_identity BEFORE DELETE OR TRUNCATE ON runtime.record_identity FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TABLE runtime.identity_shares(
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,entity_id text NOT NULL,id uuid NOT NULL,user_id uuid NOT NULL,can_write boolean NOT NULL,
 PRIMARY KEY(tenant_id,app_id,dataset_id,entity_id,id,user_id),
 FOREIGN KEY(tenant_id,app_id,dataset_id,entity_id,id) REFERENCES runtime.record_identity(tenant_id,app_id,dataset_id,entity_id,id)
);
SELECT runtime.install_tenant_rls('runtime.identity_shares');
REVOKE INSERT,UPDATE,DELETE ON runtime.identity_shares FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.identity_shares AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE runtime.dataset_evolutions(
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,operation_id uuid NOT NULL,
 base_binding bigint NOT NULL,base_generation bigint NOT NULL,target_generation bigint NOT NULL,
 target_revision bigint NOT NULL,target_ir_hash text NOT NULL,target_ir bytea NOT NULL,
 plan_hash text NOT NULL,plan_body bytea NOT NULL,
 state text NOT NULL DEFAULT 'COPYING' CHECK(state IN('COPYING','LINKING','VERIFYING','READY','ACTIVATED','ABORTED')),
 checkpoint bigint NOT NULL DEFAULT 1 CHECK(checkpoint>0),after_entity text NOT NULL DEFAULT '',after_id uuid,
 copied_rows bigint NOT NULL DEFAULT 0,verified_rows bigint NOT NULL DEFAULT 0,
 source_manifest text NOT NULL DEFAULT repeat('0',64),target_manifest text NOT NULL DEFAULT repeat('0',64),
 target_schema_hash text NOT NULL CHECK(target_schema_hash ~ '^[0-9a-f]{64}$'),
 verification_epoch bigint NOT NULL DEFAULT 0,freeze_epoch bigint NOT NULL,
 created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,dataset_id,operation_id),
 FOREIGN KEY(tenant_id,app_id,dataset_id,base_binding) REFERENCES runtime.dataset_bindings(tenant_id,app_id,dataset_id,version),
 FOREIGN KEY(tenant_id,app_id,dataset_id,base_generation) REFERENCES runtime.dataset_generations(tenant_id,app_id,dataset_id,generation),
 FOREIGN KEY(tenant_id,app_id,dataset_id,target_generation) REFERENCES runtime.dataset_generations(tenant_id,app_id,dataset_id,generation),
 FOREIGN KEY(tenant_id,app_id,target_revision) REFERENCES control.metadata_versions(tenant_id,app_id,revision),
 CHECK(target_generation>base_generation AND base_binding>0 AND checkpoint>0 AND copied_rows>=0 AND verified_rows>=0),
 CHECK(octet_length(target_ir) BETWEEN 1 AND 1048576 AND target_ir_hash=encode(sha256(target_ir),'hex')),
 CHECK(octet_length(plan_body) BETWEEN 1 AND 2097152 AND plan_hash=encode(sha256(plan_body),'hex'))
);
SELECT runtime.install_tenant_rls('runtime.dataset_evolutions');
REVOKE INSERT,UPDATE,DELETE ON runtime.dataset_evolutions FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.dataset_evolutions AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE UNIQUE INDEX single_dataset_evolution ON runtime.dataset_evolutions(tenant_id,app_id,dataset_id) WHERE state NOT IN('ACTIVATED','ABORTED');
CREATE TRIGGER retain_evolution BEFORE DELETE OR TRUNCATE ON runtime.dataset_evolutions FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- The initial adapter remains the live single source until a verified cutover
-- enables canonical access. There is no fallback thereafter on missing identity.
CREATE VIEW runtime.read_records WITH(security_invoker=true) AS
 SELECT r.tenant_id,r.app_id,r.metadata_revision,r.ir_hash,r.entity_id,r.id,r.row_version,r.custom_data,r.created_by,r.created_at,r.updated_at,r.schema_version,
 CASE WHEN d.canonical_access THEN c.owner_user_id ELSE r.owner_user_id END owner_user_id,
 CASE WHEN d.canonical_access THEN c.owner_team_id ELSE r.owner_team_id END owner_team_id,r.updated_by
 FROM runtime.records r LEFT JOIN runtime.dataset_generations g ON g.tenant_id=r.tenant_id AND g.app_id=r.app_id AND g.anchor_revision=r.metadata_revision AND g.anchor_hash=r.ir_hash
 LEFT JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id
 LEFT JOIN runtime.record_identity c ON c.tenant_id=g.tenant_id AND c.app_id=g.app_id AND c.dataset_id=g.dataset_id AND c.entity_id=r.entity_id AND c.id=r.id
 WHERE NOT coalesce(d.canonical_access,false) OR (c.id IS NOT NULL AND NOT c.tombstone);
GRANT SELECT ON runtime.read_records TO framework_app;
CREATE VIEW runtime.read_shares WITH(security_invoker=true) AS
 SELECT s.* FROM runtime.record_shares s LEFT JOIN runtime.dataset_generations g ON g.tenant_id=s.tenant_id AND g.app_id=s.app_id AND g.anchor_revision=s.metadata_revision AND g.anchor_hash=s.ir_hash
 LEFT JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id WHERE NOT coalesce(d.canonical_access,false)
 UNION ALL SELECT g.tenant_id,g.app_id,g.anchor_revision,g.anchor_hash,s.entity_id,s.id,s.user_id,s.can_write
 FROM runtime.dataset_generations g JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id AND d.canonical_access
 JOIN runtime.identity_shares s ON s.tenant_id=g.tenant_id AND s.app_id=g.app_id AND s.dataset_id=g.dataset_id
 JOIN runtime.record_identity c ON c.tenant_id=s.tenant_id AND c.app_id=s.app_id AND c.dataset_id=s.dataset_id AND c.entity_id=s.entity_id AND c.id=s.id AND NOT c.tombstone;
GRANT SELECT ON runtime.read_shares TO framework_app;
ALTER VIEW runtime.read_records OWNER TO framework_schema_owner;
ALTER VIEW runtime.read_shares OWNER TO framework_schema_owner;
-- +goose StatementBegin
CREATE FUNCTION runtime.initial_dataset_generation() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 INSERT INTO runtime.dataset_generations VALUES(NEW.tenant_id,NEW.app_id,NEW.id,1,NEW.anchor_revision,NEW.anchor_hash,NEW.schema_hash);
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION runtime.check_dataset_update() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
 IF ROW(NEW.tenant_id,NEW.app_id,NEW.id,NEW.workspace_id,NEW.environment,NEW.anchor_revision,NEW.anchor_hash,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.tenant_id,OLD.app_id,OLD.id,OLD.workspace_id,OLD.environment,OLD.anchor_revision,OLD.anchor_hash,OLD.created_by,OLD.created_at)
 OR NEW.epoch<>OLD.epoch+1 OR NEW.active_binding<OLD.active_binding OR NEW.verified_rows<OLD.verified_rows OR NEW.schema_generation<OLD.schema_generation
 OR NEW.fact_epoch<OLD.fact_epoch OR (OLD.canonical_access AND NOT NEW.canonical_access)
 OR (NEW.schema_generation=OLD.schema_generation AND NEW.schema_hash<>OLD.schema_hash)
 OR (OLD.state='OPEN' AND NEW.state NOT IN('OPEN','FROZEN'))
 THEN RAISE EXCEPTION 'dataset identity/checkpoint is immutable' USING ERRCODE='55000';END IF;
 IF NEW.schema_generation<>OLD.schema_generation AND NOT EXISTS(SELECT 1 FROM runtime.dataset_evolutions o
 WHERE o.tenant_id=OLD.tenant_id AND o.app_id=OLD.app_id AND o.dataset_id=OLD.id AND o.state='READY'
 AND o.base_generation=OLD.schema_generation AND o.base_binding=OLD.active_binding AND o.target_generation=NEW.schema_generation
 AND NEW.schema_hash=o.target_schema_hash AND NEW.active_binding=OLD.active_binding+1 AND NEW.state='OPEN' AND NEW.canonical_access)
 THEN RAISE EXCEPTION 'verified evolution required' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION runtime.fence_dataset_record_write() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE rec runtime.records%ROWTYPE;d record;b runtime.dataset_bindings%ROWTYPE;
BEGIN
 IF TG_OP='DELETE' THEN rec:=OLD;ELSE rec:=NEW;END IF;
 IF pg_has_role(session_user,'framework_migrator','MEMBER') THEN RETURN rec;END IF;
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(rec.tenant_id) THEN RAISE EXCEPTION 'trusted dataset writer required' USING ERRCODE='42501';END IF;
 PERFORM 1 FROM runtime.record_sets s WHERE s.tenant_id=rec.tenant_id AND s.app_id=rec.app_id AND s.metadata_revision=rec.metadata_revision AND s.ir_hash=rec.ir_hash FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'dataset anchor required' USING ERRCODE='55000';END IF;
 SELECT x.*,g.generation AS mapped_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id
 WHERE g.tenant_id=rec.tenant_id AND g.app_id=rec.app_id AND g.anchor_revision=rec.metadata_revision AND g.anchor_hash=rec.ir_hash FOR SHARE OF x;
 IF NOT FOUND THEN RETURN rec;END IF;
 SELECT x.* INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=d.tenant_id AND x.app_id=d.app_id AND x.dataset_id=d.id AND x.version=d.active_binding;
 IF TG_OP='INSERT' AND d.state='FROZEN' AND EXISTS(SELECT 1 FROM control.evolution_context c JOIN runtime.dataset_evolutions o
 ON o.tenant_id=c.tenant_id AND o.app_id=c.app_id AND o.dataset_id=c.dataset_id AND o.operation_id=c.operation_id
 WHERE c.backend=pg_backend_pid() AND c.transaction_id=pg_current_xact_id() AND c.tenant_id=rec.tenant_id AND c.app_id=rec.app_id
 AND c.dataset_id=d.id AND c.entity_id=rec.entity_id AND c.record_id=rec.id AND o.state='COPYING' AND o.target_generation=d.mapped_generation)
 THEN RETURN NEW;END IF;
 IF d.state<>'OPEN' OR d.mapped_generation<>d.schema_generation OR d.id::text IS DISTINCT FROM current_setting('app.dataset_id',true)
 OR d.active_binding::text IS DISTINCT FROM current_setting('app.dataset_binding',true) OR b.ir_hash IS DISTINCT FROM current_setting('app.execution_ir_hash',true)
 THEN RAISE EXCEPTION 'dataset binding is stale or sealed' USING ERRCODE='55000';END IF;
 IF d.canonical_access AND TG_OP<>'DELETE' THEN
 SELECT c.owner_user_id,c.owner_team_id INTO NEW.owner_user_id,NEW.owner_team_id FROM runtime.record_identity c
 WHERE c.tenant_id=d.tenant_id AND c.app_id=d.app_id AND c.dataset_id=d.id AND c.entity_id=rec.entity_id AND c.id=rec.id AND NOT c.tombstone;
 IF NOT FOUND AND TG_OP='UPDATE' THEN RAISE EXCEPTION 'live record identity required' USING ERRCODE='55000';END IF;
 -- INSERT obtains a new identity from the trusted actor, never a stale copy.
 IF NOT FOUND THEN NEW.owner_user_id:=iam.authenticated_actor();NEW.owner_team_id:=NULL;END IF;
 END IF;
 IF TG_OP<>'DELETE' THEN RETURN NEW;END IF;
 RETURN OLD;
END $$;
CREATE FUNCTION runtime.mirror_record_identity() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE rec runtime.records%ROWTYPE;d record;
BEGIN
 IF TG_OP='DELETE' THEN rec:=OLD;ELSE rec:=NEW;END IF;
 SELECT x.*,g.generation AS mapped_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id
 WHERE g.tenant_id=rec.tenant_id AND g.app_id=rec.app_id AND g.anchor_revision=rec.metadata_revision AND g.anchor_hash=rec.ir_hash FOR SHARE OF x;
 IF NOT FOUND OR d.mapped_generation<>d.schema_generation THEN RETURN rec;END IF;
 IF d.state='FROZEN' AND (TG_OP='DELETE' OR (TG_OP='UPDATE' AND ROW(NEW.owner_user_id,NEW.owner_team_id) IS DISTINCT FROM ROW(OLD.owner_user_id,OLD.owner_team_id))) THEN
 UPDATE runtime.datasets SET fact_epoch=fact_epoch+1,epoch=epoch+1 WHERE tenant_id=d.tenant_id AND app_id=d.app_id AND id=d.id;
 END IF;
 IF NOT d.canonical_access AND NOT EXISTS(SELECT 1 FROM runtime.record_identity c WHERE c.tenant_id=d.tenant_id AND c.app_id=d.app_id AND c.dataset_id=d.id AND c.entity_id=rec.entity_id AND c.id=rec.id) THEN RETURN rec;END IF;
 IF TG_OP='DELETE' THEN
 UPDATE runtime.record_identity c SET tombstone=true WHERE c.tenant_id=d.tenant_id AND c.app_id=d.app_id AND c.dataset_id=d.id AND c.entity_id=rec.entity_id AND c.id=rec.id;
 ELSE
 IF TG_OP='INSERT' AND EXISTS(SELECT 1 FROM runtime.record_identity c WHERE c.tenant_id=d.tenant_id AND c.app_id=d.app_id AND c.dataset_id=d.id AND c.entity_id=rec.entity_id AND c.id=rec.id)
 THEN RAISE EXCEPTION 'record identity already existed' USING ERRCODE='23505';END IF;
 INSERT INTO runtime.record_identity VALUES(d.tenant_id,d.app_id,d.id,rec.entity_id,rec.id,rec.owner_user_id,rec.owner_team_id,false)
 ON CONFLICT(tenant_id,app_id,dataset_id,entity_id,id) DO UPDATE SET owner_user_id=excluded.owner_user_id,owner_team_id=excluded.owner_team_id;
 END IF;
 RETURN rec;
END $$;
CREATE FUNCTION runtime.mirror_identity_share() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE rec runtime.record_shares%ROWTYPE;d record;
BEGIN
 IF TG_OP='DELETE' THEN rec:=OLD;ELSE rec:=NEW;END IF;
 SELECT x.*,g.generation AS mapped_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id
 WHERE g.tenant_id=rec.tenant_id AND g.app_id=rec.app_id AND g.anchor_revision=rec.metadata_revision AND g.anchor_hash=rec.ir_hash FOR SHARE OF x;
 IF NOT FOUND OR d.mapped_generation<>d.schema_generation THEN RETURN rec;END IF;
 IF d.state='FROZEN' THEN UPDATE runtime.datasets SET fact_epoch=fact_epoch+1,epoch=epoch+1 WHERE tenant_id=d.tenant_id AND app_id=d.app_id AND id=d.id;END IF;
 IF NOT EXISTS(SELECT 1 FROM runtime.record_identity c WHERE c.tenant_id=d.tenant_id AND c.app_id=d.app_id AND c.dataset_id=d.id AND c.entity_id=rec.entity_id AND c.id=rec.id) THEN RETURN rec;END IF;
 IF TG_OP='DELETE' THEN
 DELETE FROM runtime.identity_shares s WHERE s.tenant_id=d.tenant_id AND s.app_id=d.app_id AND s.dataset_id=d.id AND s.entity_id=rec.entity_id AND s.id=rec.id AND s.user_id=rec.user_id;
 ELSE
 INSERT INTO runtime.identity_shares VALUES(d.tenant_id,d.app_id,d.id,rec.entity_id,rec.id,rec.user_id,rec.can_write)
 ON CONFLICT(tenant_id,app_id,dataset_id,entity_id,id,user_id) DO UPDATE SET can_write=excluded.can_write;
 END IF;
 RETURN rec;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.initial_dataset_generation() OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.mirror_record_identity() OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.mirror_identity_share() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.initial_dataset_generation(),runtime.mirror_record_identity(),runtime.mirror_identity_share() FROM PUBLIC;
CREATE TRIGGER dataset_initial_generation AFTER INSERT ON runtime.datasets FOR EACH ROW EXECUTE FUNCTION runtime.initial_dataset_generation();
CREATE TRIGGER record_identity_mirror AFTER INSERT OR UPDATE OR DELETE ON runtime.records FOR EACH ROW EXECUTE FUNCTION runtime.mirror_record_identity();
CREATE TRIGGER share_identity_mirror AFTER INSERT OR UPDATE OR DELETE ON runtime.record_shares FOR EACH ROW EXECUTE FUNCTION runtime.mirror_identity_share();
-- Preserve legacy preview snapshots while applying the same current identity.
CREATE VIEW runtime.read_preview_records WITH(security_invoker=true) AS
 SELECT p.*,CASE WHEN d.canonical_access THEN c.owner_user_id ELSE p.created_by END owner_user_id,
 CASE WHEN d.canonical_access THEN c.owner_team_id ELSE NULL::uuid END owner_team_id
 FROM runtime.preview_records p LEFT JOIN runtime.dataset_generations g ON g.tenant_id=p.tenant_id AND g.app_id=p.app_id AND g.anchor_revision=p.metadata_revision AND g.anchor_hash=p.ir_hash
 LEFT JOIN runtime.datasets d ON d.tenant_id=g.tenant_id AND d.app_id=g.app_id AND d.id=g.dataset_id
 LEFT JOIN runtime.record_identity c ON c.tenant_id=g.tenant_id AND c.app_id=g.app_id AND c.dataset_id=g.dataset_id AND c.entity_id=p.entity_id AND c.id=p.id
 WHERE NOT coalesce(d.canonical_access,false) OR (c.id IS NOT NULL AND NOT c.tombstone);
ALTER VIEW runtime.read_preview_records OWNER TO framework_schema_owner;
GRANT SELECT ON runtime.read_preview_records TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain source generations and identity; forward recovery only'; END $$;
-- +goose StatementEnd
