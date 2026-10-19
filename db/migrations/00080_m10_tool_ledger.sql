-- M10 governed tool execution (ADR100). Additive and forward-only. The worker
-- changes the tool ledger only through ports bound to its exact authenticated
-- lease; attempts are immutable once finished; no bodies, headers or secrets.
-- +goose Up
ALTER TABLE control.metadata_versions DROP CONSTRAINT metadata_versions_schema_version_check;
ALTER TABLE control.metadata_versions ADD CONSTRAINT metadata_versions_schema_version_check
 CHECK(schema_version IN('1.0.0','1.1.0','1.2.0','1.3.0'));

CREATE TABLE runtime.tool_invocations(
 tenant_id uuid NOT NULL,instance_id uuid NOT NULL,step_key text NOT NULL CHECK(length(step_key) BETWEEN 1 AND 200),
 tool_ref text NOT NULL CHECK(length(tool_ref)<=160 AND tool_ref ~ '^tool\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$'),
 tool_version text NOT NULL CHECK(tool_version ~ '^[1-9][0-9]{0,3}\.[0-9]{1,4}\.[0-9]{1,4}$'),
 tool_hash text NOT NULL CHECK(tool_hash ~ '^[0-9a-f]{64}$'),
 idempotency_key text NOT NULL CHECK(idempotency_key ~ '^[0-9a-f]{64}$'),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 mode text NOT NULL CHECK(mode IN('REROUTE','TEST_ACCOUNT','READ_ONLY','SIMULATE','BLOCK')),
 state text NOT NULL CHECK(state IN('SENDING','SUCCEEDED','FAILED','UNKNOWN','NOT_APPLIED','APPLIED')),
 attempts integer NOT NULL CHECK(attempts BETWEEN 1 AND 20),
 first_sent_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 outcome_class text NOT NULL DEFAULT '',retryable boolean NOT NULL DEFAULT false,
 http_status integer NOT NULL DEFAULT 0 CHECK(http_status BETWEEN 0 AND 599),simulated boolean NOT NULL DEFAULT false,
 response bytea CHECK(response IS NULL OR octet_length(response)<=16384),
 response_hash text CHECK((response IS NULL AND response_hash IS NULL) OR response_hash=encode(sha256(response),'hex')),
 CHECK((state='SUCCEEDED')=(response IS NOT NULL)),
 PRIMARY KEY(tenant_id,instance_id,step_key),FOREIGN KEY(tenant_id,instance_id) REFERENCES runtime.workflow_instances(tenant_id,id)
);
SELECT runtime.install_tenant_rls('runtime.tool_invocations');
REVOKE INSERT,UPDATE,DELETE ON runtime.tool_invocations FROM framework_app;
CREATE TABLE runtime.tool_attempts(
 tenant_id uuid NOT NULL,instance_id uuid NOT NULL,step_key text NOT NULL,attempt integer NOT NULL CHECK(attempt BETWEEN 1 AND 20),
 kind text NOT NULL CHECK(kind IN('SEND','LOOKUP')),mode text NOT NULL,lease_generation bigint NOT NULL CHECK(lease_generation>0),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp(),finished_at timestamptz,
 outcome text NOT NULL DEFAULT '' CHECK(outcome IN('','SUCCESS','FAILED','UNKNOWN_REQUIRES_RECONCILIATION')),
 outcome_class text NOT NULL DEFAULT '' CHECK(outcome_class IN('','ok','not_sent','blocked','rejected','rate_limited','not_found','timeout','server_error','transport','invalid_response','lost','reconciled')),
 http_status integer NOT NULL DEFAULT 0 CHECK(http_status BETWEEN 0 AND 599),
 CHECK((finished_at IS NULL)=(outcome='')),
 PRIMARY KEY(tenant_id,instance_id,step_key,attempt),FOREIGN KEY(tenant_id,instance_id,step_key) REFERENCES runtime.tool_invocations(tenant_id,instance_id,step_key)
);
SELECT runtime.install_tenant_rls('runtime.tool_attempts');
REVOKE INSERT,UPDATE,DELETE ON runtime.tool_attempts FROM framework_app;
-- +goose StatementBegin
CREATE FUNCTION runtime.retain_tool_evidence() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF TG_OP<>'UPDATE' THEN RAISE EXCEPTION 'tool evidence is retained' USING ERRCODE='55000';END IF;
 IF TG_TABLE_NAME='tool_attempts' AND (OLD.finished_at IS NOT NULL OR ROW(NEW.tenant_id,NEW.instance_id,NEW.step_key,NEW.attempt,NEW.kind,NEW.mode,NEW.lease_generation,NEW.started_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.instance_id,OLD.step_key,OLD.attempt,OLD.kind,OLD.mode,OLD.lease_generation,OLD.started_at)) THEN
  RAISE EXCEPTION 'finished tool attempts are immutable' USING ERRCODE='55000';END IF;
 IF TG_TABLE_NAME='tool_invocations' AND (ROW(NEW.tenant_id,NEW.instance_id,NEW.step_key,NEW.tool_ref,NEW.tool_version,NEW.tool_hash,NEW.idempotency_key,NEW.request_hash,NEW.mode,NEW.first_sent_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.instance_id,OLD.step_key,OLD.tool_ref,OLD.tool_version,OLD.tool_hash,OLD.idempotency_key,OLD.request_hash,OLD.mode,OLD.first_sent_at)
  OR NEW.attempts<OLD.attempts OR OLD.state IN('SUCCEEDED','APPLIED')) THEN
  RAISE EXCEPTION 'tool invocation identity is immutable' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION runtime.retain_tool_evidence() OWNER TO framework_schema_owner;
REVOKE ALL ON FUNCTION runtime.retain_tool_evidence() FROM PUBLIC;
CREATE TRIGGER retain_tool_invocation BEFORE UPDATE OR DELETE ON runtime.tool_invocations FOR EACH ROW EXECUTE FUNCTION runtime.retain_tool_evidence();
CREATE TRIGGER retain_tool_attempt BEFORE UPDATE OR DELETE ON runtime.tool_attempts FOR EACH ROW EXECUTE FUNCTION runtime.retain_tool_evidence();
CREATE TRIGGER retain_tool_invocation_truncate BEFORE TRUNCATE ON runtime.tool_invocations FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_tool_attempt_truncate BEFORE TRUNCATE ON runtime.tool_attempts FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- Worker reads only its exact live job's ledger under the live SQL decision.
CREATE POLICY workflow_worker ON runtime.tool_invocations FOR SELECT TO framework_worker USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND instance_id::text=current_setting('app.job_id',true));
CREATE POLICY workflow_worker ON runtime.tool_attempts FOR SELECT TO framework_worker USING(iam.worker_identity_scope(tenant_id) AND (SELECT coalesce(allowed,false) FROM control.worker_policy_decision()) AND instance_id::text=current_setting('app.job_id',true));
GRANT SELECT ON runtime.tool_invocations,runtime.tool_attempts TO framework_worker;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_policy_decision() RETURNS TABLE(allowed boolean,action text,resource text,policy_version text,policy_revision bigint,app_id text,metadata_revision bigint,ir_hash text,workflow_id text,workflow_version text,plan_hash text,entity_id text,field_id text,record_id uuid,request_id text)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_variable
DECLARE a record;role record;b runtime.workflow_bindings%ROWTYPE;o runtime.outbox_intents%ROWTYPE;rec runtime.records%ROWTYPE;d record;c runtime.record_identity%ROWTYPE;doc jsonb;entity jsonb;fields jsonb;fid text;teams uuid[];invalid boolean;shared boolean:=false;record_json jsonb;
BEGIN
 SELECT x.* INTO a FROM control.worker_authority() x;IF NOT FOUND THEN RETURN;END IF;
 SELECT h.active,h.version,v.bootstrap,v.body,v.body_hash,m.role INTO role FROM iam.memberships m JOIN control.role_heads h ON h.tenant_id=m.tenant_id AND h.name=m.role JOIN control.role_versions v ON v.tenant_id=h.tenant_id AND v.name=h.name AND v.version=h.version WHERE m.tenant_id=a.tenant_id AND m.user_id=a.actor_id AND m.active;
 allowed:=false;action:='policy.unavailable';resource:='workflow';field_id:='';
 IF FOUND THEN policy_revision:=role.version;policy_version:=CASE WHEN role.bootstrap THEN 'tenant-role-b4:' ELSE 'tenant-role-v1:' END||role.body_hash;ELSE policy_revision:=0;policy_version:='sql-policy-unavailable-v1';END IF;
 IF a.kind='event' THEN
  SELECT x.* INTO o FROM runtime.outbox_intents x WHERE x.tenant_id=a.tenant_id AND x.id=a.job_id;IF NOT FOUND THEN RETURN;END IF;
  app_id:=o.app_id;metadata_revision:=o.metadata_revision;ir_hash:=o.ir_hash;entity_id:=o.entity_id;record_id:=o.record_id;request_id:=o.request_id;resource:='event';workflow_id:='';workflow_version:='';plan_hash:='';
 ELSE
  SELECT x.* INTO b FROM runtime.workflow_bindings x WHERE x.tenant_id=a.tenant_id AND x.id=a.job_id;IF NOT FOUND THEN RETURN;END IF;
  app_id:=b.app_id;metadata_revision:=b.metadata_revision;ir_hash:=b.ir_hash;workflow_id:=b.workflow_id;workflow_version:=b.workflow_version;plan_hash:=b.plan_hash;record_id:=b.record_id;request_id:=b.request_id;entity_id:=convert_from(b.plan_body,'UTF8')::jsonb->>'entity_id';
 END IF;
 IF role.version IS NULL OR NOT role.active THEN RETURN NEXT;RETURN;END IF;
 IF role.bootstrap THEN
  allowed:=role.role IN('owner','admin','member');action:=CASE a.kind WHEN 'event' THEN 'events.consume' ELSE 'workflows.execute' END;
  IF a.kind='workflow' AND NOT EXISTS(SELECT 1 FROM runtime.worker_lease_source_record()) THEN allowed:=false;action:='workflows.data_scope';END IF;
  RETURN NEXT;RETURN;
 END IF;
 doc:=convert_from(role.body,'UTF8')::jsonb;
 IF doc->>'schema_version' IS DISTINCT FROM 'tenant-capabilities-v1' OR doc->>'name' IS DISTINCT FROM role.role OR octet_length(role.body)>65536 THEN RETURN NEXT;RETURN;END IF;
 action:=CASE a.kind WHEN 'event' THEN 'events.consume' ELSE 'workflows.execute' END;
 IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 IF a.kind='event' THEN allowed:=true;RETURN NEXT;RETURN;END IF;
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE s->>'type'='activity') THEN
  action:='workflows.activity';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 END IF;
 -- M10: a plan with any registered tool activity also requires tools.invoke.
 IF EXISTS(SELECT 1 FROM jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE s->>'type'='activity' AND s->'activity' ? 'tool_ref') THEN
  action:='tools.invoke';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 END IF;
 resource:='entity';action:='data.entity.read';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 resource:='row';action:='data.row.read';IF control.policy_grant(doc,action,app_id,entity_id,'')<>1 THEN RETURN NEXT;RETURN;END IF;
 fields:=convert_from(b.inputs,'UTF8')::jsonb->'fields';
 IF jsonb_typeof(fields) IS DISTINCT FROM 'array' OR jsonb_array_length(fields)>128 THEN RAISE EXCEPTION 'input field budget';END IF;
 FOR fid IN SELECT jsonb_array_elements_text(fields) LOOP
  action:='data.field.read';resource:='field';field_id:=fid;
  IF control.policy_grant(doc,action,app_id,entity_id,fid)<>1 THEN RETURN NEXT;RETURN;END IF;
 END LOOP;field_id:='';
 action:='workflows.data_scope';resource:='entity';
 SELECT x.* INTO rec FROM runtime.records x WHERE x.tenant_id=b.tenant_id AND x.app_id=b.app_id AND x.metadata_revision=coalesce(b.data_revision,b.metadata_revision) AND x.ir_hash=coalesce(b.data_hash,b.ir_hash) AND x.entity_id=entity_id AND x.id=b.record_id;
 IF NOT FOUND THEN RETURN NEXT;RETURN;END IF;
 SELECT x.id,x.canonical_access,g.generation=x.schema_generation AS current_generation INTO d FROM runtime.dataset_generations g JOIN runtime.datasets x ON x.tenant_id=g.tenant_id AND x.app_id=g.app_id AND x.id=g.dataset_id WHERE g.tenant_id=rec.tenant_id AND g.app_id=rec.app_id AND g.anchor_revision=rec.metadata_revision AND g.anchor_hash=rec.ir_hash;
 IF FOUND AND d.canonical_access THEN
  SELECT x.* INTO c FROM runtime.record_identity x WHERE x.tenant_id=rec.tenant_id AND x.app_id=rec.app_id AND x.dataset_id=d.id AND x.entity_id=rec.entity_id AND x.id=rec.id AND NOT x.tombstone;
  IF NOT FOUND THEN RETURN NEXT;RETURN;END IF;rec.owner_user_id:=c.owner_user_id;rec.owner_team_id:=c.owner_team_id;
  SELECT EXISTS(SELECT 1 FROM runtime.identity_shares s WHERE s.tenant_id=c.tenant_id AND s.app_id=c.app_id AND s.dataset_id=c.dataset_id AND s.entity_id=c.entity_id AND s.id=c.id AND s.user_id=a.actor_id) INTO shared;
 ELSE SELECT EXISTS(SELECT 1 FROM runtime.record_shares s WHERE s.tenant_id=rec.tenant_id AND s.app_id=rec.app_id AND s.metadata_revision=rec.metadata_revision AND s.ir_hash=rec.ir_hash AND s.entity_id=rec.entity_id AND s.id=rec.id AND s.user_id=a.actor_id) INTO shared;END IF;
 record_json:=to_jsonb(rec)||jsonb_build_object('_policy_current_generation',coalesce(d.current_generation,true));
 WITH RECURSIVE permitted(id,path,depth,cycle) AS (
 SELECT team_id,ARRAY[team_id],0,false FROM iam.team_memberships WHERE tenant_id=a.tenant_id AND user_id=a.actor_id AND active
 UNION ALL SELECT t.id,p.path||t.id,p.depth+1,t.id=ANY(p.path) FROM iam.teams t JOIN permitted p ON t.parent_id=p.id WHERE t.tenant_id=a.tenant_id AND p.depth<9 AND NOT p.cycle
 ),bounded AS (SELECT DISTINCT id,bool_or(depth>8 OR cycle) OVER() bad FROM permitted ORDER BY id LIMIT 65)
 SELECT array_agg(id),coalesce(bool_or(bad),false) INTO teams,invalid FROM bounded;
 IF invalid OR cardinality(teams)>64 THEN RETURN NEXT;RETURN;END IF;
 SELECT x INTO entity FROM jsonb_array_elements(convert_from(b.ir_body,'UTF8')::jsonb->'entities') x WHERE x->>'id'=entity_id;
 IF entity IS NULL OR NOT control.policy_data_matches(doc,'data.entity.read',app_id,entity_id,'',record_json,a.actor_id,teams,shared,entity) THEN RETURN NEXT;RETURN;END IF;
 FOR fid IN SELECT jsonb_array_elements_text(fields) LOOP
  resource:='field';field_id:=fid;
  IF NOT control.policy_data_matches(doc,'data.field.read',app_id,entity_id,fid,record_json,a.actor_id,teams,shared,entity) THEN RETURN NEXT;RETURN;END IF;
 END LOOP;
 allowed:=true;field_id:='';action:='workflows.execute';resource:='workflow';RETURN NEXT;
EXCEPTION WHEN invalid_text_representation OR data_exception OR raise_exception THEN
 allowed:=false;action:='policy.unavailable';RETURN NEXT;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION control.tool_key(t uuid,i uuid,step text,h text) RETURNS text LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT encode(sha256(convert_to('tool-key-v1/'||t::text||'/'||i::text||'/'||step||'/'||h,'UTF8')),'hex')
$$;
-- Exact lease context for this transaction; GUCs only narrow it.
CREATE FUNCTION control.tool_lease() RETURNS TABLE(tenant_id uuid,job_id uuid,actor_id uuid,generation bigint,remaining_ms integer)
LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT a.tenant_id,a.job_id,a.actor_id,c.generation,greatest(0,floor(extract(epoch FROM j.lease_until-clock_timestamp())*1000)-1500)::integer
 FROM control.worker_authority() a JOIN control.worker_context c ON c.backend=pg_backend_pid() AND c.transaction_id=pg_current_xact_id() AND c.tenant_id=a.tenant_id AND c.job_id=a.job_id
 JOIN control.workflow_jobs j ON j.tenant_id=a.tenant_id AND j.id=a.job_id AND j.generation=c.generation
 WHERE a.kind='workflow'
$$;
CREATE FUNCTION control.tool_begin(step text,ref text,ver text,h text,rh text,m text,k text,needs_approval boolean)
RETURNS TABLE(attempt integer,idempotency_key text,remaining_ms integer)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;x runtime.workflow_instances%ROWTYPE;b runtime.workflow_bindings%ROWTYPE;st jsonb;inv runtime.tool_invocations%ROWTYPE;n integer;current_version bigint;tool_key text;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND OR NOT coalesce((SELECT d.allowed FROM control.worker_policy_decision() d),false) THEN RAISE EXCEPTION 'exact tool lease and live capability required' USING ERRCODE='42501';END IF;
 IF step IS NULL OR ref IS NULL OR ver IS NULL OR h !~ '^[0-9a-f]{64}$' OR rh !~ '^[0-9a-f]{64}$' OR m IS NULL OR m NOT IN('REROUTE','TEST_ACCOUNT','READ_ONLY','SIMULATE','BLOCK') OR k IS NULL OR k NOT IN('SEND','LOOKUP') OR needs_approval IS NULL THEN
  RAISE EXCEPTION 'invalid tool attempt witness' USING ERRCODE='22023';END IF;
 SELECT * INTO x FROM runtime.workflow_instances w WHERE w.tenant_id=l.tenant_id AND w.id=l.job_id FOR UPDATE;
 IF NOT FOUND OR x.state_id<>step OR x.status NOT IN('READY','RETRY_WAIT') OR x.termination<>'' THEN RAISE EXCEPTION 'tool step is not current' USING ERRCODE='55000';END IF;
 SELECT * INTO b FROM runtime.workflow_bindings w WHERE w.tenant_id=l.tenant_id AND w.id=l.job_id;
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
-- The outcome write is fenced by the exact lease generation that began the
-- attempt; a delayed result from a lost lease can never overwrite evidence.
CREATE FUNCTION control.tool_finish(step text,n integer,outcome_in text,class_in text,retry_in boolean,status_in integer,simulated_in boolean,body bytea) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;att runtime.tool_attempts%ROWTYPE;inv runtime.tool_invocations%ROWTYPE;next_state text;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND THEN RAISE EXCEPTION 'exact tool lease required' USING ERRCODE='42501';END IF;
 IF outcome_in NOT IN('SUCCESS','FAILED','UNKNOWN_REQUIRES_RECONCILIATION') OR class_in IS NULL OR class_in IN('','lost','reconciled') OR retry_in IS NULL OR simulated_in IS NULL
  OR status_in IS NULL OR status_in NOT BETWEEN 0 AND 599 OR (outcome_in='SUCCESS')<>(body IS NOT NULL) OR octet_length(body)>16384 OR (outcome_in='SUCCESS')<>(class_in='ok') THEN
  RAISE EXCEPTION 'invalid tool outcome' USING ERRCODE='22023';END IF;
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step FOR UPDATE;
 SELECT * INTO att FROM runtime.tool_attempts a WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=n FOR UPDATE;
 IF inv.state IS DISTINCT FROM 'SENDING' OR inv.attempts<>n OR att.finished_at IS NOT NULL OR att.lease_generation IS DISTINCT FROM l.generation THEN
  RAISE EXCEPTION 'stale tool outcome rejected' USING ERRCODE='55000';END IF;
 IF class_in='not_found' AND (att.kind<>'LOOKUP' OR outcome_in<>'FAILED') THEN RAISE EXCEPTION 'invalid lookup outcome' USING ERRCODE='22023';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome=outcome_in,outcome_class=class_in,http_status=status_in WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=n;
 next_state:=CASE WHEN outcome_in='SUCCESS' THEN 'SUCCEEDED' WHEN class_in='not_found' THEN 'NOT_APPLIED' WHEN outcome_in='FAILED' THEN 'FAILED' ELSE 'UNKNOWN' END;
 UPDATE runtime.tool_invocations v SET state=next_state,outcome_class=class_in,retryable=retry_in AND outcome_in='FAILED',http_status=status_in,simulated=simulated_in,
  response=body,response_hash=CASE WHEN body IS NULL THEN NULL ELSE encode(sha256(body),'hex') END,updated_at=clock_timestamp()
  WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 RETURN next_state;
END $$;
-- A send whose outcome was never recorded (crash, lost lease, failed write) is
-- UNKNOWN. Only an earlier lease or an attempt older than any send deadline.
CREATE FUNCTION control.tool_abandon(step text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;inv runtime.tool_invocations%ROWTYPE;att runtime.tool_attempts%ROWTYPE;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND OR NOT coalesce((SELECT d.allowed FROM control.worker_policy_decision() d),false) THEN RAISE EXCEPTION 'exact tool lease and live capability required' USING ERRCODE='42501';END IF;
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step FOR UPDATE;
 IF NOT FOUND OR inv.state<>'SENDING' THEN RETURN false;END IF;
 SELECT * INTO att FROM runtime.tool_attempts a WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=inv.attempts FOR UPDATE;
 IF att.finished_at IS NOT NULL OR NOT (att.lease_generation<l.generation OR att.started_at<clock_timestamp()-interval '3 seconds') THEN RAISE EXCEPTION 'tool attempt may still be in flight' USING ERRCODE='55000';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome='UNKNOWN_REQUIRES_RECONCILIATION',outcome_class='lost' WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=inv.attempts;
 UPDATE runtime.tool_invocations v SET state='UNKNOWN',outcome_class='lost',retryable=false,updated_at=clock_timestamp() WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 RETURN true;
END $$;
-- Owner/admin reconciliation (authorized by the API) closes the ledger. APPLIED
-- is final; NOT_APPLIED permits a fresh send. Evidence is never erased.
CREATE FUNCTION control.tool_reconcile(t uuid,i uuid,step text,applied boolean) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE inv runtime.tool_invocations%ROWTYPE;
BEGIN
 IF session_user<>'framework_app' OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t) OR applied IS NULL
  OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role<>'viewer') THEN
  RAISE EXCEPTION 'tenant reconciler required' USING ERRCODE='42501';END IF;
 IF NOT EXISTS(SELECT 1 FROM runtime.workflow_instances w WHERE w.tenant_id=t AND w.id=i AND w.status='RECONCILE' AND w.state_id=step) THEN RETURN false;END IF;
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=t AND v.instance_id=i AND v.step_key=step FOR UPDATE;
 IF NOT FOUND THEN RETURN false;END IF;
 IF inv.state NOT IN('UNKNOWN','SENDING','FAILED') THEN RAISE EXCEPTION 'tool invocation already settled' USING ERRCODE='55000';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome='UNKNOWN_REQUIRES_RECONCILIATION',outcome_class='reconciled' WHERE a.tenant_id=t AND a.instance_id=i AND a.step_key=step AND a.finished_at IS NULL;
 UPDATE runtime.tool_invocations v SET state=CASE WHEN applied THEN 'APPLIED' ELSE 'NOT_APPLIED' END,outcome_class='reconciled',retryable=false,updated_at=clock_timestamp()
  WHERE v.tenant_id=t AND v.instance_id=i AND v.step_key=step;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('tool_key','tool_lease','tool_begin','tool_finish','tool_abandon','tool_reconcile','worker_policy_decision') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.worker_policy_decision() TO framework_worker;
GRANT EXECUTE ON FUNCTION control.tool_begin(text,text,text,text,text,text,text,boolean),control.tool_finish(text,integer,text,text,boolean,integer,boolean,bytea),control.tool_abandon(text) TO framework_worker;
GRANT EXECUTE ON FUNCTION control.tool_reconcile(uuid,uuid,text,boolean) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain tool ledger evidence; forward recovery only';END $$;
-- +goose StatementEnd
