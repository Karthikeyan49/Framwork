-- Controlled shadow writer. Tenant APIs provide typed intent, never SQL/flags.
-- +goose Up
CREATE TABLE control.evolution_context(backend integer PRIMARY KEY,transaction_id xid8 NOT NULL,tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,operation_id uuid NOT NULL,entity_id text NOT NULL,record_id uuid NOT NULL);
ALTER TABLE control.evolution_context OWNER TO framework_schema_owner;
ALTER TABLE control.evolution_context ENABLE ROW LEVEL SECURITY;
ALTER TABLE control.evolution_context FORCE ROW LEVEL SECURITY;
CREATE POLICY schema_owner_access ON control.evolution_context TO framework_schema_owner USING(true) WITH CHECK(true);
-- +goose StatementBegin
CREATE FUNCTION runtime.guard_evolution() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF ROW(NEW.tenant_id,NEW.app_id,NEW.dataset_id,NEW.operation_id,NEW.base_binding,NEW.base_generation,NEW.target_generation,NEW.target_revision,NEW.target_ir_hash,NEW.target_ir,NEW.plan_hash,NEW.plan_body,NEW.target_schema_hash,NEW.freeze_epoch,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.tenant_id,OLD.app_id,OLD.dataset_id,OLD.operation_id,OLD.base_binding,OLD.base_generation,OLD.target_generation,OLD.target_revision,OLD.target_ir_hash,OLD.target_ir,OLD.plan_hash,OLD.plan_body,OLD.target_schema_hash,OLD.freeze_epoch,OLD.created_by,OLD.created_at)
 OR NEW.checkpoint<>OLD.checkpoint+1 OR NEW.copied_rows<OLD.copied_rows OR OLD.state IN('ACTIVATED','ABORTED')
 OR NOT ((OLD.state='COPYING' AND NEW.state IN('COPYING','LINKING','ABORTED')) OR (OLD.state='LINKING' AND NEW.state IN('LINKING','VERIFYING','ABORTED')) OR (OLD.state='VERIFYING' AND NEW.state IN('VERIFYING','READY','ABORTED')) OR (OLD.state='READY' AND NEW.state IN('VERIFYING','ACTIVATED','ABORTED')))
 THEN RAISE EXCEPTION 'immutable evolution witness/checkpoint required' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION runtime.evolution_manager(t uuid,a text,ds uuid) RETURNS runtime.datasets LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;
BEGIN
 IF session_user<>'framework_app' OR NOT iam.tenant_matches_scope(t) OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role IN('owner','admin'))
 THEN RAISE EXCEPTION 'trusted evolution manager required' USING ERRCODE='42501';END IF;
 SELECT x.* INTO d FROM runtime.datasets x WHERE x.tenant_id=t AND x.app_id=a AND x.id=ds FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'dataset missing' USING ERRCODE='55000';END IF;
 RETURN d;
END $$;
CREATE FUNCTION runtime.start_evolution(t uuid,a text,ds uuid,op uuid,expected bigint,revision bigint,ir bytea,ih text,plan bytea,ph text,schema text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;b runtime.dataset_bindings%ROWTYPE;prior runtime.dataset_evolutions%ROWTYPE;g bigint;h text;artifact jsonb;
BEGIN
 d:=runtime.evolution_manager(t,a,ds);
 SELECT x.* INTO prior FROM runtime.dataset_evolutions x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.operation_id=op;
 IF FOUND THEN
 IF prior.base_binding<>expected OR prior.target_revision<>revision OR prior.target_ir_hash<>ih OR prior.plan_hash<>ph THEN RAISE EXCEPTION 'operation key conflict' USING ERRCODE='55000';END IF;
 RETURN;END IF;
 IF d.state<>'OPEN' OR d.active_binding<>expected THEN RAISE EXCEPTION 'base binding changed' USING ERRCODE='55000';END IF;
 SELECT x.* INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.version=expected;
 artifact:=convert_from(plan,'UTF8')::jsonb;
 IF encode(sha256(plan),'hex')<>ph OR encode(sha256(ir),'hex')<>ih OR artifact->>'base_ir_hash' IS DISTINCT FROM b.ir_hash
 OR artifact->>'target_ir_hash' IS DISTINCT FROM ih OR artifact->>'base_schema_hash' IS DISTINCT FROM b.schema_hash OR artifact->>'schema_hash' IS DISTINCT FROM schema
 THEN RAISE EXCEPTION 'evolution witness mismatch' USING ERRCODE='55000';END IF;
 g:=(SELECT coalesce(max(x.generation),0)+1 FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds);
 h:=encode(sha256(convert_to(jsonb_build_array('dataset-generation-v1',ds,g,op,schema)::text,'UTF8')),'hex');
 UPDATE runtime.datasets SET state='FROZEN',epoch=epoch+1 WHERE tenant_id=t AND app_id=a AND id=ds;
 INSERT INTO runtime.record_sets(tenant_id,app_id,metadata_revision,ir_hash) VALUES(t,a,1,h);
 INSERT INTO runtime.dataset_generations VALUES(t,a,ds,g,1,h,schema);
 INSERT INTO runtime.dataset_evolutions(tenant_id,app_id,dataset_id,operation_id,base_binding,base_generation,target_generation,target_revision,target_ir_hash,target_ir,plan_hash,plan_body,target_schema_hash,freeze_epoch)
 VALUES(t,a,ds,op,expected,d.schema_generation,g,revision,ih,ir,ph,plan,schema,d.epoch+1);
END $$;
CREATE FUNCTION runtime.copy_evolution_record(t uuid,a text,ds uuid,op uuid,e text,rid uuid,data jsonb) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;o runtime.dataset_evolutions%ROWTYPE;src runtime.records%ROWTYPE;dst runtime.records%ROWTYPE;sg runtime.dataset_generations%ROWTYPE;tg runtime.dataset_generations%ROWTYPE;shares bigint;
BEGIN
 d:=runtime.evolution_manager(t,a,ds);
 SELECT x.* INTO o FROM runtime.dataset_evolutions x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.operation_id=op;
 IF NOT FOUND THEN RAISE EXCEPTION 'evolution operation missing' USING ERRCODE='55000';END IF;
 IF d.state<>'FROZEN' OR d.active_binding<>o.base_binding OR d.schema_generation<>o.base_generation OR o.state<>'COPYING' OR jsonb_typeof(data)<>'object' OR octet_length(data::text)>65536
 THEN RAISE EXCEPTION 'sealed copy required' USING ERRCODE='55000';END IF;
 SELECT x.* INTO sg FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.generation=o.base_generation;
 SELECT x.* INTO tg FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.generation=o.target_generation;
 SELECT x.* INTO src FROM runtime.records x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=sg.anchor_revision AND x.ir_hash=sg.anchor_hash AND x.entity_id=e AND x.id=rid FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'copy source missing' USING ERRCODE='55000';END IF;
 -- Existing canonical access remains authoritative across later conversions.
 IF NOT d.canonical_access THEN
 INSERT INTO runtime.record_identity VALUES(t,a,ds,e,rid,src.owner_user_id,src.owner_team_id,false) ON CONFLICT DO NOTHING;
 SELECT count(*) INTO shares FROM runtime.record_shares s WHERE s.tenant_id=t AND s.app_id=a AND s.metadata_revision=sg.anchor_revision AND s.ir_hash=sg.anchor_hash AND s.entity_id=e AND s.id=rid;
 IF shares>256 THEN RAISE EXCEPTION 'share budget' USING ERRCODE='22023';END IF;
 INSERT INTO runtime.identity_shares SELECT t,a,ds,e,rid,s.user_id,s.can_write FROM runtime.record_shares s WHERE s.tenant_id=t AND s.app_id=a AND s.metadata_revision=sg.anchor_revision AND s.ir_hash=sg.anchor_hash AND s.entity_id=e AND s.id=rid
 ON CONFLICT(tenant_id,app_id,dataset_id,entity_id,id,user_id) DO NOTHING;
 END IF;
 SELECT x.* INTO dst FROM runtime.records x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=tg.anchor_revision AND x.ir_hash=tg.anchor_hash AND x.entity_id=e AND x.id=rid;
 IF FOUND THEN
 IF ROW(dst.row_version,dst.custom_data,dst.created_by,dst.created_at,dst.updated_at,dst.schema_version,dst.updated_by)
 IS DISTINCT FROM ROW(src.row_version,data,src.created_by,src.created_at,src.updated_at,src.schema_version,src.updated_by)
 THEN RAISE EXCEPTION 'unequal shadow record' USING ERRCODE='55000';END IF;
 RETURN;END IF;
 INSERT INTO control.evolution_context VALUES(pg_backend_pid(),pg_current_xact_id(),t,a,ds,op,e,rid)
 ON CONFLICT(backend) DO UPDATE SET transaction_id=excluded.transaction_id,tenant_id=excluded.tenant_id,app_id=excluded.app_id,dataset_id=excluded.dataset_id,operation_id=excluded.operation_id,entity_id=excluded.entity_id,record_id=excluded.record_id;
 INSERT INTO runtime.records(tenant_id,app_id,metadata_revision,ir_hash,entity_id,id,row_version,custom_data,created_by,created_at,updated_at,schema_version,owner_user_id,owner_team_id,updated_by)
 VALUES(t,a,tg.anchor_revision,tg.anchor_hash,e,rid,src.row_version,data,src.created_by,src.created_at,src.updated_at,src.schema_version,src.owner_user_id,src.owner_team_id,src.updated_by);
 DELETE FROM control.evolution_context WHERE backend=pg_backend_pid();
END $$;
CREATE FUNCTION runtime.link_evolution_record(t uuid,a text,ds uuid,op uuid,e text,rid uuid,f text,target text,target_id uuid) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;o runtime.dataset_evolutions%ROWTYPE;g runtime.dataset_generations%ROWTYPE;actual runtime.record_links%ROWTYPE;
BEGIN
 d:=runtime.evolution_manager(t,a,ds);
 SELECT x.* INTO o FROM runtime.dataset_evolutions x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.operation_id=op;
 IF NOT FOUND THEN RAISE EXCEPTION 'evolution operation missing' USING ERRCODE='55000';END IF;
 IF d.state<>'FROZEN' OR d.active_binding<>o.base_binding OR o.state<>'LINKING' THEN RAISE EXCEPTION 'sealed link required' USING ERRCODE='55000';END IF;
 SELECT x.* INTO g FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.generation=o.target_generation;
 IF NOT EXISTS(SELECT 1 FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=g.anchor_revision AND r.ir_hash=g.anchor_hash AND r.entity_id=e AND r.id=rid AND r.custom_data->>f=target_id::text)
 THEN RAISE EXCEPTION 'link source mismatch' USING ERRCODE='55000';END IF;
 INSERT INTO runtime.record_links VALUES(t,a,g.anchor_revision,g.anchor_hash,e,rid,f,target,target_id) ON CONFLICT DO NOTHING;
 SELECT x.* INTO actual FROM runtime.record_links x WHERE x.tenant_id=t AND x.app_id=a AND x.metadata_revision=g.anchor_revision AND x.ir_hash=g.anchor_hash AND x.entity_id=e AND x.id=rid AND x.field_id=f;
 IF actual.target_entity<>target OR actual.target_id<>target_id THEN RAISE EXCEPTION 'unequal shadow link' USING ERRCODE='55000';END IF;
END $$;
CREATE FUNCTION runtime.checkpoint_evolution(t uuid,a text,ds uuid,op uuid,expected bigint,phase text,entity text,rid uuid,count_delta bigint,source_hash text,target_hash text,done boolean) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;o runtime.dataset_evolutions%ROWTYPE;sg runtime.dataset_generations%ROWTYPE;tg runtime.dataset_generations%ROWTYPE;n bigint;dest bigint;
BEGIN
 d:=runtime.evolution_manager(t,a,ds);
 SELECT x.* INTO o FROM runtime.dataset_evolutions x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.operation_id=op FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'evolution operation missing' USING ERRCODE='55000';END IF;
 IF o.checkpoint<>expected OR o.state<>phase OR d.state<>'FROZEN' OR d.active_binding<>o.base_binding OR d.schema_generation<>o.base_generation
 THEN RAISE EXCEPTION 'evolution checkpoint changed' USING ERRCODE='55000';END IF;
 IF count_delta<0 OR count_delta>100 OR source_hash !~ '^[0-9a-f]{64}$' OR target_hash !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'checkpoint budget' USING ERRCODE='22023';END IF;
 IF phase='VERIFYING' AND o.verification_epoch<>d.fact_epoch THEN RAISE EXCEPTION 'live access changed; restart verification' USING ERRCODE='55000';END IF;
 IF done AND phase IN('COPYING','VERIFYING') THEN
 SELECT x.* INTO sg FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.generation=o.base_generation;
 SELECT x.* INTO tg FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.generation=o.target_generation;
 SELECT count(*) INTO n FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=sg.anchor_revision AND r.ir_hash=sg.anchor_hash;
 SELECT count(*) INTO dest FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=a AND r.metadata_revision=tg.anchor_revision AND r.ir_hash=tg.anchor_hash;
 IF n<>dest OR n<>o.copied_rows+(CASE WHEN phase='COPYING' THEN count_delta ELSE 0 END)
 OR (phase='VERIFYING' AND (n<>o.verified_rows+count_delta OR source_hash<>target_hash))
 OR EXISTS(SELECT 1 FROM runtime.records r LEFT JOIN runtime.record_identity c ON c.tenant_id=t AND c.app_id=a AND c.dataset_id=ds AND c.entity_id=r.entity_id AND c.id=r.id WHERE r.tenant_id=t AND r.app_id=a AND ((r.metadata_revision=sg.anchor_revision AND r.ir_hash=sg.anchor_hash) OR (r.metadata_revision=tg.anchor_revision AND r.ir_hash=tg.anchor_hash)) AND (c.id IS NULL OR c.tombstone))
 THEN RAISE EXCEPTION 'complete identity and shadow coverage required' USING ERRCODE='55000';END IF;
 END IF;
 UPDATE runtime.dataset_evolutions x SET checkpoint=x.checkpoint+1,after_entity=CASE WHEN done THEN '' ELSE entity END,after_id=CASE WHEN done THEN NULL ELSE rid END,
 copied_rows=x.copied_rows+CASE WHEN phase='COPYING' THEN count_delta ELSE 0 END,
 verified_rows=x.verified_rows+CASE WHEN phase='VERIFYING' THEN count_delta ELSE 0 END,
 source_manifest=source_hash,target_manifest=target_hash,
 verification_epoch=CASE WHEN phase='LINKING' AND done THEN d.fact_epoch ELSE x.verification_epoch END,
 state=CASE WHEN NOT done THEN phase WHEN phase='COPYING' THEN 'LINKING' WHEN phase='LINKING' THEN 'VERIFYING' WHEN phase='VERIFYING' THEN 'READY' ELSE phase END
 WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.operation_id=op;
 IF NOT FOUND THEN RAISE EXCEPTION 'evolution operation missing' USING ERRCODE='55000';END IF;
 IF phase='COPYING' AND done THEN
 UPDATE runtime.datasets SET canonical_access=true,epoch=epoch+1 WHERE tenant_id=t AND app_id=a AND id=ds;
 END IF;
END $$;
CREATE FUNCTION runtime.finish_evolution(t uuid,a text,ds uuid,op uuid,expected bigint,action text) RETURNS void
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE d runtime.datasets%ROWTYPE;o runtime.dataset_evolutions%ROWTYPE;b runtime.dataset_bindings%ROWTYPE;
BEGIN
 d:=runtime.evolution_manager(t,a,ds);
 SELECT x.* INTO o FROM runtime.dataset_evolutions x WHERE x.tenant_id=t AND x.app_id=a AND x.dataset_id=ds AND x.operation_id=op FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'evolution operation missing' USING ERRCODE='55000';END IF;
 IF (action='ACTIVATE' AND o.state='ACTIVATED') OR (action='ABORT' AND o.state='ABORTED') THEN RETURN;END IF;
 IF o.checkpoint<>expected OR d.state<>'FROZEN' OR d.active_binding<>o.base_binding OR d.schema_generation<>o.base_generation THEN RAISE EXCEPTION 'activation base changed' USING ERRCODE='55000';END IF;
 IF action='ACTIVATE' THEN
 IF o.state<>'READY' OR o.verification_epoch<>d.fact_epoch OR o.verified_rows<>o.copied_rows THEN RAISE EXCEPTION 'verified shadow required' USING ERRCODE='55000';END IF;
 INSERT INTO runtime.dataset_bindings(tenant_id,app_id,dataset_id,version,metadata_revision,ir_hash,ir_body,schema_hash,generation)
 VALUES(t,a,ds,d.active_binding+1,o.target_revision,o.target_ir_hash,o.target_ir,o.target_schema_hash,o.target_generation);
 UPDATE runtime.datasets SET schema_generation=o.target_generation,schema_hash=o.target_schema_hash,active_binding=active_binding+1,state='OPEN',epoch=epoch+1 WHERE tenant_id=t AND app_id=a AND id=ds;
 UPDATE runtime.dataset_evolutions SET state='ACTIVATED',checkpoint=checkpoint+1 WHERE tenant_id=t AND app_id=a AND dataset_id=ds AND operation_id=op;
 ELSIF action='ABORT' THEN
 IF o.state='ACTIVATED' THEN RAISE EXCEPTION 'post-cutover recovery needs reverse plan' USING ERRCODE='55000';END IF;
 UPDATE runtime.dataset_evolutions SET state='ABORTED',checkpoint=checkpoint+1 WHERE tenant_id=t AND app_id=a AND dataset_id=ds AND operation_id=op;
 UPDATE runtime.datasets SET state='OPEN',epoch=epoch+1 WHERE tenant_id=t AND app_id=a AND id=ds;
 ELSIF action='REVERIFY' AND o.state IN('VERIFYING','READY') THEN
 UPDATE runtime.dataset_evolutions SET state='VERIFYING',checkpoint=checkpoint+1,after_entity='',after_id=NULL,verified_rows=0,source_manifest=repeat('0',64),target_manifest=repeat('0',64),verification_epoch=d.fact_epoch WHERE tenant_id=t AND app_id=a AND dataset_id=ds AND operation_id=op;
 ELSE RAISE EXCEPTION 'unsupported evolution action' USING ERRCODE='22023';END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.guard_evolution() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.guard_evolution() FROM PUBLIC;
CREATE TRIGGER evolution_guard BEFORE UPDATE ON runtime.dataset_evolutions FOR EACH ROW EXECUTE FUNCTION runtime.guard_evolution();
ALTER FUNCTION runtime.evolution_manager(uuid,text,uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.start_evolution(uuid,text,uuid,uuid,bigint,bigint,bytea,text,bytea,text,text) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.copy_evolution_record(uuid,text,uuid,uuid,text,uuid,jsonb) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.link_evolution_record(uuid,text,uuid,uuid,text,uuid,text,text,uuid) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.checkpoint_evolution(uuid,text,uuid,uuid,bigint,text,text,uuid,bigint,text,text,boolean) OWNER TO framework_schema_owner;
ALTER FUNCTION runtime.finish_evolution(uuid,text,uuid,uuid,bigint,text) OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.evolution_manager(uuid,text,uuid),runtime.start_evolution(uuid,text,uuid,uuid,bigint,bigint,bytea,text,bytea,text,text),runtime.copy_evolution_record(uuid,text,uuid,uuid,text,uuid,jsonb),runtime.link_evolution_record(uuid,text,uuid,uuid,text,uuid,text,text,uuid),runtime.checkpoint_evolution(uuid,text,uuid,uuid,bigint,text,text,uuid,bigint,text,text,boolean),runtime.finish_evolution(uuid,text,uuid,uuid,bigint,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION runtime.start_evolution(uuid,text,uuid,uuid,bigint,bigint,bytea,text,bytea,text,text),runtime.copy_evolution_record(uuid,text,uuid,uuid,text,uuid,jsonb),runtime.link_evolution_record(uuid,text,uuid,uuid,text,uuid,text,text,uuid),runtime.checkpoint_evolution(uuid,text,uuid,uuid,bigint,text,text,uuid,bigint,text,text,boolean),runtime.finish_evolution(uuid,text,uuid,uuid,bigint,text) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain shadow and source evidence; forward recovery only'; END $$;
-- +goose StatementEnd
