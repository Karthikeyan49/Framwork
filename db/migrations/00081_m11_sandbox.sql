-- M11 sandbox identity and enforcement (ADR101). Additive and forward-only.
-- A sandbox is an EMPTY synthetic dataset whose environment, tool ceiling and
-- freeze evidence are server-owned. Nothing here touches preview datasets,
-- records or production credentials; tool sends stay governed by migration 80.
-- +goose Up
ALTER TABLE runtime.datasets DROP CONSTRAINT datasets_environment_check;
ALTER TABLE runtime.datasets ADD CONSTRAINT datasets_environment_check CHECK(environment IN('preview','sandbox'));
GRANT INSERT(environment) ON runtime.datasets TO framework_app;
CREATE TABLE runtime.sandboxes(
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,
 data_mode text NOT NULL CHECK(data_mode='SYNTHETIC_SEPARATE'),
 tool_ceiling text NOT NULL CHECK(tool_ceiling IN('BLOCK','SIMULATE','READ_ONLY','REROUTE','TEST_ACCOUNT')),
 state text NOT NULL DEFAULT 'OPEN' CHECK(state IN('OPEN','FROZEN')),
 created_by uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,dataset_id),FOREIGN KEY(tenant_id,app_id,dataset_id) REFERENCES runtime.datasets(tenant_id,app_id,id)
);
SELECT runtime.install_tenant_rls('runtime.sandboxes');
REVOKE INSERT,UPDATE,DELETE ON runtime.sandboxes FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.sandboxes AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE runtime.sandbox_evidence(
 tenant_id uuid NOT NULL,app_id text NOT NULL,dataset_id uuid NOT NULL,
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 2 AND 16384),body_hash text NOT NULL CHECK(body_hash=encode(sha256(body),'hex')),
 frozen_by uuid NOT NULL DEFAULT iam.authenticated_actor(),frozen_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,dataset_id),FOREIGN KEY(tenant_id,app_id,dataset_id) REFERENCES runtime.sandboxes(tenant_id,app_id,dataset_id)
);
SELECT runtime.install_tenant_rls('runtime.sandbox_evidence');
REVOKE INSERT,UPDATE,DELETE ON runtime.sandbox_evidence FROM framework_app;
CREATE POLICY worker_no_generic ON runtime.sandbox_evidence AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER retain_sandbox_evidence BEFORE UPDATE OR DELETE ON runtime.sandbox_evidence FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_sandbox_evidence_truncate BEFORE TRUNCATE ON runtime.sandbox_evidence FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_sandbox_truncate BEFORE TRUNCATE ON runtime.sandboxes FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
CREATE FUNCTION control.tool_mode_rank(m text) RETURNS integer LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT CASE m WHEN 'BLOCK' THEN 0 WHEN 'SIMULATE' THEN 1 WHEN 'READ_ONLY' THEN 2 WHEN 'REROUTE' THEN 3 WHEN 'TEST_ACCOUNT' THEN 4 ELSE 99 END
$$;
-- A sandbox dataset can only ever be created empty and open, with no records.
CREATE FUNCTION runtime.guard_sandbox_dataset() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NEW.environment='sandbox' THEN
  IF NEW.state<>'OPEN' OR EXISTS(SELECT 1 FROM runtime.records r WHERE r.tenant_id=NEW.tenant_id AND r.app_id=NEW.app_id AND r.metadata_revision=NEW.anchor_revision AND r.ir_hash=NEW.anchor_hash)
  OR EXISTS(SELECT 1 FROM runtime.preview_records p WHERE p.tenant_id=NEW.tenant_id AND p.app_id=NEW.app_id AND p.metadata_revision=NEW.anchor_revision AND p.ir_hash=NEW.anchor_hash) THEN
   RAISE EXCEPTION 'a sandbox dataset must be created empty and open' USING ERRCODE='55000';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION runtime.require_sandbox_row() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NEW.environment='sandbox' AND NOT EXISTS(SELECT 1 FROM runtime.sandboxes s WHERE s.tenant_id=NEW.tenant_id AND s.app_id=NEW.app_id AND s.dataset_id=NEW.id) THEN
  RAISE EXCEPTION 'a sandbox dataset requires its registered sandbox' USING ERRCODE='55000';END IF;
 RETURN NULL;
END $$;
-- Frozen sandboxes are evidence: no leaving FROZEN and no evolution afterwards.
CREATE FUNCTION runtime.guard_sandbox_state() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF TG_OP='DELETE' OR ROW(NEW.tenant_id,NEW.app_id,NEW.dataset_id,NEW.data_mode,NEW.tool_ceiling,NEW.created_by,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.app_id,OLD.dataset_id,OLD.data_mode,OLD.tool_ceiling,OLD.created_by,OLD.created_at)
 OR OLD.state='FROZEN' THEN RAISE EXCEPTION 'sandbox identity and frozen evidence are immutable' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION runtime.guard_sandbox_dataset_update() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF OLD.state='FROZEN' AND NEW.state<>'FROZEN' AND EXISTS(SELECT 1 FROM runtime.sandboxes s WHERE s.tenant_id=OLD.tenant_id AND s.app_id=OLD.app_id AND s.dataset_id=OLD.id AND s.state='FROZEN') THEN
  RAISE EXCEPTION 'frozen sandbox datasets stay frozen' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER sandbox_dataset_guard BEFORE INSERT ON runtime.datasets FOR EACH ROW EXECUTE FUNCTION runtime.guard_sandbox_dataset();
CREATE CONSTRAINT TRIGGER sandbox_row_required AFTER INSERT ON runtime.datasets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION runtime.require_sandbox_row();
CREATE TRIGGER sandbox_state_guard BEFORE UPDATE OR DELETE ON runtime.sandboxes FOR EACH ROW EXECUTE FUNCTION runtime.guard_sandbox_state();
CREATE TRIGGER sandbox_dataset_update_guard BEFORE UPDATE ON runtime.datasets FOR EACH ROW EXECUTE FUNCTION runtime.guard_sandbox_dataset_update();
-- +goose StatementBegin
CREATE FUNCTION runtime.register_sandbox(t uuid,app text,dataset uuid,ceiling text) RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t)
 OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role<>'viewer') THEN
  RAISE EXCEPTION 'tenant sandbox administrator required' USING ERRCODE='42501';END IF;
 IF ceiling IS NULL OR ceiling NOT IN('BLOCK','SIMULATE','READ_ONLY','REROUTE','TEST_ACCOUNT') THEN RAISE EXCEPTION 'invalid sandbox tool ceiling' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM runtime.datasets d WHERE d.tenant_id=t AND d.app_id=app AND d.id=dataset AND d.environment='sandbox' AND d.state='OPEN') THEN
  RAISE EXCEPTION 'open sandbox dataset required' USING ERRCODE='55000';END IF;
 INSERT INTO runtime.sandboxes(tenant_id,app_id,dataset_id,data_mode,tool_ceiling) VALUES(t,app,dataset,'SYNTHETIC_SEPARATE',ceiling);
END $$;
CREATE FUNCTION runtime.freeze_sandbox(t uuid,app text,dataset uuid,catalog_hash text,policy_hash text) RETURNS text LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE sb runtime.sandboxes%ROWTYPE;d runtime.datasets%ROWTYPE;b runtime.dataset_bindings%ROWTYPE;g runtime.dataset_generations%ROWTYPE;
 records_n bigint;records_hash text;ledger_n bigint;ledger_hash text;body bytea;h text;
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t)
 OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role<>'viewer') THEN
  RAISE EXCEPTION 'tenant sandbox administrator required' USING ERRCODE='42501';END IF;
 IF catalog_hash !~ '^[0-9a-f]{64}$' OR policy_hash !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'invalid freeze witness' USING ERRCODE='22023';END IF;
 SELECT * INTO sb FROM runtime.sandboxes s WHERE s.tenant_id=t AND s.app_id=app AND s.dataset_id=dataset FOR UPDATE;
 IF NOT FOUND OR sb.state<>'OPEN' THEN RAISE EXCEPTION 'open sandbox required' USING ERRCODE='55000';END IF;
 SELECT * INTO d FROM runtime.datasets x WHERE x.tenant_id=t AND x.app_id=app AND x.id=dataset FOR UPDATE;
 IF d.state<>'OPEN' OR d.environment<>'sandbox' THEN RAISE EXCEPTION 'open sandbox dataset required' USING ERRCODE='55000';END IF;
 SELECT * INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=t AND x.app_id=app AND x.dataset_id=dataset AND x.version=d.active_binding;
 SELECT * INTO g FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=app AND x.dataset_id=dataset AND x.generation=d.schema_generation;
 SELECT count(*),coalesce(encode(sha256(convert_to(string_agg(r.entity_id||':'||r.id::text||':'||r.row_version::text||':'||encode(sha256(convert_to(r.custom_data::text,'UTF8')),'hex'),E'\n' ORDER BY r.entity_id,r.id),'UTF8')),'hex'),repeat('0',64))
  INTO records_n,records_hash FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=app AND r.metadata_revision=g.anchor_revision AND r.ir_hash=g.anchor_hash;
 SELECT count(*),coalesce(encode(sha256(convert_to(string_agg(v.instance_id::text||':'||v.step_key||':'||v.tool_hash||':'||v.mode||':'||v.state||':'||v.attempts::text||':'||v.request_hash,E'\n' ORDER BY v.instance_id,v.step_key),'UTF8')),'hex'),repeat('0',64))
  INTO ledger_n,ledger_hash FROM runtime.tool_invocations v JOIN runtime.workflow_bindings w ON w.tenant_id=v.tenant_id AND w.id=v.instance_id WHERE w.tenant_id=t AND w.app_id=app AND w.dataset_id=dataset;
 IF records_n>10000 OR ledger_n>10000 THEN RAISE EXCEPTION 'sandbox evidence budget exceeded' USING ERRCODE='22023';END IF;
 body:=convert_to(jsonb_build_object('schema_version','sandbox-evidence-v1','app_id',app,'dataset_id',dataset,'workspace_id',d.workspace_id,
  'data_mode',sb.data_mode,'tool_ceiling',sb.tool_ceiling,'binding_version',b.version,'metadata_revision',b.metadata_revision,'ir_hash',b.ir_hash,
  'schema_hash',b.schema_hash,'schema_generation',d.schema_generation,'anchor_revision',g.anchor_revision,'anchor_hash',g.anchor_hash,
  'records',jsonb_build_object('count',records_n,'hash',records_hash),'tool_ledger',jsonb_build_object('count',ledger_n,'hash',ledger_hash),
  'tool_catalog_hash',catalog_hash,'policy_hash',policy_hash)::text,'UTF8');
 h:=encode(sha256(body),'hex');
 INSERT INTO runtime.sandbox_evidence(tenant_id,app_id,dataset_id,body,body_hash) VALUES(t,app,dataset,body,h);
 UPDATE runtime.sandboxes s SET state='FROZEN' WHERE s.tenant_id=t AND s.app_id=app AND s.dataset_id=dataset;
 UPDATE runtime.datasets x SET state='FROZEN',epoch=epoch+1,fact_epoch=fact_epoch+1 WHERE x.tenant_id=t AND x.app_id=app AND x.id=dataset;
 RETURN h;
END $$;
-- Worker-visible environment of its exact leased job; it exposes no dataset rows.
CREATE FUNCTION control.worker_environment() RETURNS TABLE(environment text,tool_ceiling text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NOT coalesce((SELECT d.allowed FROM control.worker_policy_decision() d),false) THEN RETURN;END IF;
 RETURN QUERY SELECT coalesce(d.environment,'preview'),s.tool_ceiling
  FROM control.worker_authority() a JOIN runtime.workflow_bindings b ON b.tenant_id=a.tenant_id AND b.id=a.job_id
  LEFT JOIN runtime.datasets d ON d.tenant_id=b.tenant_id AND d.app_id=b.app_id AND d.id=b.dataset_id
  LEFT JOIN runtime.sandboxes s ON s.tenant_id=d.tenant_id AND s.app_id=d.app_id AND s.dataset_id=d.id
  WHERE a.kind='workflow';
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.tool_begin(step text,ref text,ver text,h text,rh text,m text,k text,needs_approval boolean)
RETURNS TABLE(attempt integer,idempotency_key text,remaining_ms integer)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;sb record;x runtime.workflow_instances%ROWTYPE;b runtime.workflow_bindings%ROWTYPE;st jsonb;inv runtime.tool_invocations%ROWTYPE;n integer;current_version bigint;tool_key text;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND OR NOT coalesce((SELECT d.allowed FROM control.worker_policy_decision() d),false) THEN RAISE EXCEPTION 'exact tool lease and live capability required' USING ERRCODE='42501';END IF;
 IF step IS NULL OR ref IS NULL OR ver IS NULL OR h !~ '^[0-9a-f]{64}$' OR rh !~ '^[0-9a-f]{64}$' OR m IS NULL OR m NOT IN('REROUTE','TEST_ACCOUNT','READ_ONLY','SIMULATE','BLOCK') OR k IS NULL OR k NOT IN('SEND','LOOKUP') OR needs_approval IS NULL THEN
  RAISE EXCEPTION 'invalid tool attempt witness' USING ERRCODE='22023';END IF;
 SELECT * INTO x FROM runtime.workflow_instances w WHERE w.tenant_id=l.tenant_id AND w.id=l.job_id FOR UPDATE;
 IF NOT FOUND OR x.state_id<>step OR x.status NOT IN('READY','RETRY_WAIT') OR x.termination<>'' THEN RAISE EXCEPTION 'tool step is not current' USING ERRCODE='55000';END IF;
 SELECT * INTO b FROM runtime.workflow_bindings w WHERE w.tenant_id=l.tenant_id AND w.id=l.job_id;
 -- M11: a frozen sandbox is immutable evidence, and the effective mode may never
 -- exceed the sandbox's server-owned ceiling. Preview datasets keep M10 rules.
 SELECT s.tool_ceiling,s.state INTO sb FROM runtime.sandboxes s WHERE s.tenant_id=b.tenant_id AND s.app_id=b.app_id AND s.dataset_id=b.dataset_id;
 IF FOUND THEN
  IF sb.state<>'OPEN' THEN RAISE EXCEPTION 'sandbox is frozen' USING ERRCODE='55000';END IF;
  IF control.tool_mode_rank(m)>control.tool_mode_rank(sb.tool_ceiling) THEN RAISE EXCEPTION 'tool mode exceeds the sandbox ceiling' USING ERRCODE='42501';END IF;
 END IF;
 SELECT s INTO st FROM jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE s->>'id'=step;
 IF st IS NULL OR st->>'type'<>'activity' OR st->'activity'->>'tool_ref' IS DISTINCT FROM ref OR st->'activity'->>'version' IS DISTINCT FROM ver OR st->>'tool_hash' IS DISTINCT FROM h THEN
  RAISE EXCEPTION 'tool attempt does not match the pinned plan' USING ERRCODE='22023';END IF;
 IF needs_approval THEN
  -- Approval evidence is API-written: a signal row joined to its history entry,
  -- by an actor other than the starter. Workers cannot insert signals.
  IF NOT EXISTS(SELECT 1 FROM runtime.workflow_signals s JOIN runtime.workflow_history hs ON hs.tenant_id=s.tenant_id AND hs.instance_id=s.instance_id AND hs.sequence=s.sequence
   WHERE s.tenant_id=l.tenant_id AND s.instance_id=l.job_id AND hs.event='approve' AND hs.actor_id=s.actor_id AND s.actor_id<>b.actor_id) THEN
   RAISE EXCEPTION 'tool approval evidence required' USING ERRCODE='TL001';END IF;
  -- The approval covers the start snapshot; a changed record invalidates it.
  SELECT r.row_version INTO current_version FROM runtime.records r WHERE r.tenant_id=b.tenant_id AND r.app_id=b.app_id
   AND r.metadata_revision=coalesce(b.data_revision,b.metadata_revision) AND r.ir_hash=coalesce(b.data_hash,b.ir_hash)
   AND r.entity_id=convert_from(b.plan_body,'UTF8')::jsonb->>'entity_id' AND r.id=b.record_id;
  IF current_version IS DISTINCT FROM b.record_version THEN RAISE EXCEPTION 'approved snapshot is stale' USING ERRCODE='TL002';END IF;
 END IF;
 tool_key:=control.tool_key(l.tenant_id,l.job_id,step,h);
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step FOR UPDATE;
 IF NOT FOUND THEN
  IF k<>'SEND' THEN RAISE EXCEPTION 'lookup requires a prior send' USING ERRCODE='55000';END IF;
  INSERT INTO runtime.tool_invocations(tenant_id,instance_id,step_key,tool_ref,tool_version,tool_hash,idempotency_key,request_hash,mode,state,attempts)
   VALUES(l.tenant_id,l.job_id,step,ref,ver,h,tool_key,rh,m,'SENDING',1);
  n:=1;
 ELSE
  IF ROW(inv.tool_ref,inv.tool_version,inv.tool_hash,inv.request_hash,inv.mode) IS DISTINCT FROM ROW(ref,ver,h,rh,m) THEN RAISE EXCEPTION 'tool attempt witness changed' USING ERRCODE='55000';END IF;
  IF NOT (inv.state='UNKNOWN' OR k='SEND' AND (inv.state='NOT_APPLIED' OR inv.state='FAILED' AND inv.retryable)) THEN RAISE EXCEPTION 'tool attempt not permitted from this state' USING ERRCODE='55000';END IF;
  IF inv.attempts>=20 THEN RAISE EXCEPTION 'tool attempt budget exhausted' USING ERRCODE='55000';END IF;
  n:=inv.attempts+1;
  UPDATE runtime.tool_invocations v SET state='SENDING',attempts=n,updated_at=clock_timestamp() WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 END IF;
 INSERT INTO runtime.tool_attempts(tenant_id,instance_id,step_key,attempt,kind,mode,lease_generation) VALUES(l.tenant_id,l.job_id,step,n,k,m,l.generation);
 RETURN QUERY SELECT n,tool_key,l.remaining_ms;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE (n.nspname='runtime' AND p.proname IN('guard_sandbox_dataset','require_sandbox_row','guard_sandbox_state','guard_sandbox_dataset_update','register_sandbox','freeze_sandbox')) OR (n.nspname='control' AND p.proname IN('tool_mode_rank','worker_environment','tool_begin')) LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION runtime.register_sandbox(uuid,text,uuid,text),runtime.freeze_sandbox(uuid,text,uuid,text,text) TO framework_app;
GRANT EXECUTE ON FUNCTION control.tool_begin(text,text,text,text,text,text,text,boolean),control.worker_environment() TO framework_worker;
-- Extend the existing worker drift guard to the sandbox catalogs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions','runtime.sandboxes','runtime.sandbox_evidence') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain sandbox evidence; forward recovery only';END $$;
-- +goose StatementEnd
