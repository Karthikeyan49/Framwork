-- M11 independent-review fixes (ADR037 Amendments; docs/verification/M11_INDEPENDENT_REVIEW.md
-- R01-R06). Additive and forward-only; 46, 47, 50 are preserved and the changed functions are
-- replaced in place with the same signatures, owner and ACLs, each from its LATEST definition:
--   runtime.register_sandbox, runtime.freeze_sandbox, runtime.guard_sandbox_dataset_update (all 00046)
--   control.tool_begin, control.tool_finish, control.tool_abandon, control.tool_reconcile (all 00050).
-- Every guard the older definitions had is kept; the additions are marked M11R-0n.
--
-- R01  tool_finish/abandon/reconcile refuse a frozen sandbox, and freeze_sandbox refuses while a
--      send is in flight (SENDING), an unknown outcome is outstanding (UNKNOWN) or a workflow is
--      parked in RECONCILE: the ledger the evidence hashes can no longer change after frozen_at.
-- R02  tool_begin holds the sandbox row FOR SHARE; freeze_sandbox holds it FOR UPDATE and runs READ
--      COMMITTED (it refuses any other isolation level), so a begin and a freeze serialise and the
--      freeze always sees the begin it waited for. A REPEATABLE READ freeze would not.
-- R03  fence triggers: no INSERT/UPDATE/DELETE on the workflow instance, history, receipt, signal
--      and tool-ledger rows of a frozen sandbox, whoever the caller is (the API, a worker, a signal).
-- R04  fence triggers on record_links, dataset_bindings and dataset_generations; the dataset row of a
--      frozen sandbox accepts no update at all (this also fences the epoch bumps of share/owner
--      mirrors, so record shares of a frozen sandbox are refused at the SQL layer).
-- R05  register_sandbox and freeze_sandbox require an owner/admin membership (ADR037 #5).
-- R06  the evidence manifests are ordered bytewise (COLLATE "C"); new bodies say so in
--      "manifest_order". Stored v1 bodies are immutable and keep the order they were hashed in;
--      nothing recomputes a manifest (M12 binds the stored body hash).
-- +goose Up

-- +goose StatementBegin
-- True when the workflow instance belongs to a dataset whose sandbox is FROZEN.
CREATE FUNCTION runtime.instance_in_frozen_sandbox(t uuid,i uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM runtime.workflow_bindings b JOIN runtime.sandboxes s ON s.tenant_id=b.tenant_id AND s.app_id=b.app_id AND s.dataset_id=b.dataset_id
  WHERE b.tenant_id=t AND b.id=i AND s.state='FROZEN')
$$;
-- True when the dataset is a FROZEN sandbox.
CREATE FUNCTION runtime.dataset_is_frozen_sandbox(t uuid,app text,ds uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT EXISTS(SELECT 1 FROM runtime.sandboxes s WHERE s.tenant_id=t AND s.app_id=app AND s.dataset_id=ds AND s.state='FROZEN')
$$;
-- The workflow rows and the tool ledger of a frozen sandbox are evidence: nothing writes them again.
CREATE FUNCTION runtime.fence_frozen_instance() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid;i uuid;
BEGIN
 IF TG_TABLE_NAME='workflow_instances' THEN
  IF TG_OP='DELETE' THEN t:=OLD.tenant_id;i:=OLD.id;ELSE t:=NEW.tenant_id;i:=NEW.id;END IF;
 ELSE
  IF TG_OP='DELETE' THEN t:=OLD.tenant_id;i:=OLD.instance_id;ELSE t:=NEW.tenant_id;i:=NEW.instance_id;END IF;
 END IF;
 IF runtime.instance_in_frozen_sandbox(t,i) THEN RAISE EXCEPTION 'frozen sandbox is read-only' USING ERRCODE='55000';END IF;
 IF TG_OP='DELETE' THEN RETURN OLD;END IF;
 RETURN NEW;
END $$;
-- Links, bindings and generations are keyed by dataset or by the records' anchor.
CREATE FUNCTION runtime.fence_frozen_dataset_rows() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid;app text;ds uuid;
BEGIN
 IF TG_OP='DELETE' THEN t:=OLD.tenant_id;app:=OLD.app_id;ELSE t:=NEW.tenant_id;app:=NEW.app_id;END IF;
 IF TG_TABLE_NAME='record_links' THEN
  IF TG_OP='DELETE' THEN
   SELECT g.dataset_id INTO ds FROM runtime.dataset_generations g WHERE g.tenant_id=t AND g.app_id=app AND g.anchor_revision=OLD.metadata_revision AND g.anchor_hash=OLD.ir_hash;
  ELSE
   SELECT g.dataset_id INTO ds FROM runtime.dataset_generations g WHERE g.tenant_id=t AND g.app_id=app AND g.anchor_revision=NEW.metadata_revision AND g.anchor_hash=NEW.ir_hash;
  END IF;
 ELSE
  IF TG_OP='DELETE' THEN ds:=OLD.dataset_id;ELSE ds:=NEW.dataset_id;END IF;
 END IF;
 IF ds IS NOT NULL AND runtime.dataset_is_frozen_sandbox(t,app,ds) THEN RAISE EXCEPTION 'frozen sandbox is read-only' USING ERRCODE='55000';END IF;
 IF TG_OP='DELETE' THEN RETURN OLD;END IF;
 RETURN NEW;
END $$;
-- M11R-04: the dataset row of a frozen sandbox accepts no update (manifest, binding pointer,
-- epochs, verification columns). Leaving FROZEN keeps its own message.
CREATE OR REPLACE FUNCTION runtime.guard_sandbox_dataset_update() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF OLD.state='FROZEN' AND EXISTS(SELECT 1 FROM runtime.sandboxes s WHERE s.tenant_id=OLD.tenant_id AND s.app_id=OLD.app_id AND s.dataset_id=OLD.id AND s.state='FROZEN') THEN
  IF NEW.state<>'FROZEN' THEN RAISE EXCEPTION 'frozen sandbox datasets stay frozen' USING ERRCODE='55000';END IF;
  RAISE EXCEPTION 'frozen sandbox is read-only' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.workflow_instances FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_instance();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.workflow_history FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_instance();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.workflow_receipts FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_instance();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.workflow_signals FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_instance();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.tool_invocations FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_instance();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.tool_attempts FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_instance();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.record_links FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_dataset_rows();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.dataset_bindings FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_dataset_rows();
CREATE TRIGGER sandbox_frozen_fence BEFORE INSERT OR UPDATE OR DELETE ON runtime.dataset_generations FOR EACH ROW EXECUTE FUNCTION runtime.fence_frozen_dataset_rows();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION runtime.register_sandbox(t uuid,app text,dataset uuid,ceiling text) RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t)
 OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role IN('owner','admin')) THEN
  RAISE EXCEPTION 'tenant sandbox administrator required' USING ERRCODE='42501';END IF;
 IF ceiling IS NULL OR ceiling NOT IN('BLOCK','SIMULATE','READ_ONLY','REROUTE','TEST_ACCOUNT') THEN RAISE EXCEPTION 'invalid sandbox tool ceiling' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM runtime.datasets d WHERE d.tenant_id=t AND d.app_id=app AND d.id=dataset AND d.environment='sandbox' AND d.state='OPEN') THEN
  RAISE EXCEPTION 'open sandbox dataset required' USING ERRCODE='55000';END IF;
 INSERT INTO runtime.sandboxes(tenant_id,app_id,dataset_id,data_mode,tool_ceiling) VALUES(t,app,dataset,'SYNTHETIC_SEPARATE',ceiling);
END $$;
CREATE OR REPLACE FUNCTION runtime.freeze_sandbox(t uuid,app text,dataset uuid,catalog_hash text,policy_hash text) RETURNS text LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE sb runtime.sandboxes%ROWTYPE;d runtime.datasets%ROWTYPE;b runtime.dataset_bindings%ROWTYPE;g runtime.dataset_generations%ROWTYPE;
 records_n bigint;records_hash text;ledger_n bigint;ledger_hash text;body bytea;h text;
BEGIN
 IF session_user<>'framework_app' OR t IS NULL OR t IS DISTINCT FROM iam.current_tenant_id() OR NOT iam.tenant_matches_scope(t)
 OR NOT EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role IN('owner','admin')) THEN
  RAISE EXCEPTION 'tenant sandbox administrator required' USING ERRCODE='42501';END IF;
 -- M11R-02: the freeze must read the ledger and the records AFTER it holds the locks below, in a
 -- fresh snapshot per statement. A REPEATABLE READ caller would aggregate from a snapshot that
 -- predates a begin/write it just waited for, so it is refused (the API runs this port READ COMMITTED).
 IF current_setting('transaction_isolation')<>'read committed' THEN RAISE EXCEPTION 'sandbox freeze requires read committed isolation' USING ERRCODE='55000';END IF;
 IF catalog_hash !~ '^[0-9a-f]{64}$' OR policy_hash !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'invalid freeze witness' USING ERRCODE='22023';END IF;
 SELECT * INTO sb FROM runtime.sandboxes s WHERE s.tenant_id=t AND s.app_id=app AND s.dataset_id=dataset FOR UPDATE;
 IF NOT FOUND OR sb.state<>'OPEN' THEN RAISE EXCEPTION 'open sandbox required' USING ERRCODE='55000';END IF;
 SELECT * INTO d FROM runtime.datasets x WHERE x.tenant_id=t AND x.app_id=app AND x.id=dataset FOR UPDATE;
 IF d.state<>'OPEN' OR d.environment<>'sandbox' THEN RAISE EXCEPTION 'open sandbox dataset required' USING ERRCODE='55000';END IF;
 -- M11R-01/02: quiescence. tool_begin holds the sandbox row FOR SHARE until it commits, so after the
 -- FOR UPDATE above every begin is either committed (and visible to the next statements: READ
 -- COMMITTED) or refused. Lock every ledger row of the dataset against concurrent settlement, then
 -- refuse while an attempt is in flight or an unknown outcome is parked for an operator: the
 -- evidence would describe a ledger that is still changing. The operator settles it (reconcile) and
 -- freezes again. Workflows that are merely waiting (no send outstanding) do not block the freeze;
 -- they stay parked, because every later write to them is fenced.
 PERFORM 1 FROM runtime.tool_invocations v JOIN runtime.workflow_bindings w ON w.tenant_id=v.tenant_id AND w.id=v.instance_id
  WHERE w.tenant_id=t AND w.app_id=app AND w.dataset_id=dataset FOR SHARE OF v;
 IF EXISTS(SELECT 1 FROM runtime.tool_invocations v JOIN runtime.workflow_bindings w ON w.tenant_id=v.tenant_id AND w.id=v.instance_id
   WHERE w.tenant_id=t AND w.app_id=app AND w.dataset_id=dataset AND v.state IN('SENDING','UNKNOWN'))
  OR EXISTS(SELECT 1 FROM runtime.workflow_instances i JOIN runtime.workflow_bindings w ON w.tenant_id=i.tenant_id AND w.id=i.id
   WHERE w.tenant_id=t AND w.app_id=app AND w.dataset_id=dataset AND i.status='RECONCILE') THEN
  RAISE EXCEPTION 'sandbox has outstanding tool work; settle it before freezing' USING ERRCODE='55000';END IF;
 SELECT * INTO b FROM runtime.dataset_bindings x WHERE x.tenant_id=t AND x.app_id=app AND x.dataset_id=dataset AND x.version=d.active_binding;
 SELECT * INTO g FROM runtime.dataset_generations x WHERE x.tenant_id=t AND x.app_id=app AND x.dataset_id=dataset AND x.generation=d.schema_generation;
 SELECT count(*),coalesce(encode(sha256(convert_to(string_agg(r.entity_id||':'||r.id::text||':'||r.row_version::text||':'||encode(sha256(convert_to(r.custom_data::text,'UTF8')),'hex'),E'\n' ORDER BY r.entity_id COLLATE "C",r.id),'UTF8')),'hex'),repeat('0',64))
  INTO records_n,records_hash FROM runtime.records r WHERE r.tenant_id=t AND r.app_id=app AND r.metadata_revision=g.anchor_revision AND r.ir_hash=g.anchor_hash;
 SELECT count(*),coalesce(encode(sha256(convert_to(string_agg(v.instance_id::text||':'||v.step_key||':'||v.tool_hash||':'||v.mode||':'||v.state||':'||v.attempts::text||':'||v.request_hash,E'\n' ORDER BY v.instance_id,v.step_key COLLATE "C"),'UTF8')),'hex'),repeat('0',64))
  INTO ledger_n,ledger_hash FROM runtime.tool_invocations v JOIN runtime.workflow_bindings w ON w.tenant_id=v.tenant_id AND w.id=v.instance_id WHERE w.tenant_id=t AND w.app_id=app AND w.dataset_id=dataset;
 IF records_n>10000 OR ledger_n>10000 THEN RAISE EXCEPTION 'sandbox evidence budget exceeded' USING ERRCODE='22023';END IF;
 body:=convert_to(jsonb_build_object('schema_version','sandbox-evidence-v1','app_id',app,'dataset_id',dataset,'workspace_id',d.workspace_id,
  'data_mode',sb.data_mode,'tool_ceiling',sb.tool_ceiling,'binding_version',b.version,'metadata_revision',b.metadata_revision,'ir_hash',b.ir_hash,
  'schema_hash',b.schema_hash,'schema_generation',d.schema_generation,'anchor_revision',g.anchor_revision,'anchor_hash',g.anchor_hash,
  'records',jsonb_build_object('count',records_n,'hash',records_hash),'tool_ledger',jsonb_build_object('count',ledger_n,'hash',ledger_hash),
  'tool_catalog_hash',catalog_hash,'policy_hash',policy_hash,'manifest_order','bytewise')::text,'UTF8');
 h:=encode(sha256(body),'hex');
 INSERT INTO runtime.sandbox_evidence(tenant_id,app_id,dataset_id,body,body_hash) VALUES(t,app,dataset,body,h);
 UPDATE runtime.sandboxes s SET state='FROZEN' WHERE s.tenant_id=t AND s.app_id=app AND s.dataset_id=dataset;
 UPDATE runtime.datasets x SET state='FROZEN',epoch=epoch+1,fact_epoch=fact_epoch+1 WHERE x.tenant_id=t AND x.app_id=app AND x.id=dataset;
 RETURN h;
END $$;
CREATE OR REPLACE FUNCTION control.tool_begin(step text,ref text,ver text,h text,rh text,m text,k text,needs_approval boolean)
RETURNS TABLE(attempt integer,idempotency_key text,remaining_ms integer)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;sb record;x runtime.workflow_instances%ROWTYPE;b runtime.workflow_bindings%ROWTYPE;plan jsonb;st jsonb;c jsonb;inv runtime.tool_invocations%ROWTYPE;n integer;current_version bigint;tool_key text;need boolean;outstanding boolean;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND OR NOT coalesce((SELECT d.allowed FROM control.worker_policy_decision() d),false) THEN RAISE EXCEPTION 'exact tool lease and live capability required' USING ERRCODE='42501';END IF;
 -- needs_approval is kept for signature compatibility and ignored (M10R-02).
 IF step IS NULL OR ref IS NULL OR ver IS NULL OR h !~ '^[0-9a-f]{64}$' OR rh !~ '^[0-9a-f]{64}$' OR m IS NULL OR m NOT IN('REROUTE','TEST_ACCOUNT','READ_ONLY','SIMULATE','BLOCK') OR k IS NULL OR k NOT IN('SEND','LOOKUP') OR needs_approval IS NULL THEN
  RAISE EXCEPTION 'invalid tool attempt witness' USING ERRCODE='22023';END IF;
 SELECT * INTO x FROM runtime.workflow_instances w WHERE w.tenant_id=l.tenant_id AND w.id=l.job_id FOR UPDATE;
 IF NOT FOUND OR x.state_id<>step OR x.status NOT IN('READY','RETRY_WAIT') OR x.termination<>'' THEN RAISE EXCEPTION 'tool step is not current' USING ERRCODE='55000';END IF;
 SELECT * INTO b FROM runtime.workflow_bindings w WHERE w.tenant_id=l.tenant_id AND w.id=l.job_id;
 -- M11: a frozen sandbox is immutable evidence, and the effective mode may never
 -- exceed the sandbox's server-owned ceiling. Preview datasets keep M10 rules.
 -- M11R-02: the share lock serialises this begin with freeze_sandbox (which holds the
 -- sandbox row FOR UPDATE and then sees every committed invocation): a begin that
 -- committed first is seen and refuses the freeze, a freeze that committed first is seen here.
 SELECT s.tool_ceiling,s.state INTO sb FROM runtime.sandboxes s WHERE s.tenant_id=b.tenant_id AND s.app_id=b.app_id AND s.dataset_id=b.dataset_id FOR SHARE;
 IF FOUND THEN
  IF sb.state<>'OPEN' THEN RAISE EXCEPTION 'sandbox is frozen' USING ERRCODE='55000';END IF;
  IF control.tool_mode_rank(m)>control.tool_mode_rank(sb.tool_ceiling) THEN RAISE EXCEPTION 'tool mode exceeds the sandbox ceiling' USING ERRCODE='42501';END IF;
 END IF;
 plan:=convert_from(b.plan_body,'UTF8')::jsonb;
 SELECT s INTO st FROM jsonb_array_elements(plan->'states') s WHERE s->>'id'=step;
 IF st IS NULL OR st->>'type'<>'activity' OR st->'activity'->>'tool_ref' IS DISTINCT FROM ref OR st->'activity'->>'version' IS DISTINCT FROM ver OR st->>'tool_hash' IS DISTINCT FROM h THEN
  RAISE EXCEPTION 'tool attempt does not match the pinned plan' USING ERRCODE='22023';END IF;
 need:=CASE WHEN st ? 'tool_contract' THEN st->'tool_contract'->>'approval' IS DISTINCT FROM 'NONE' ELSE control.tool_approval_gated(plan,step) END;
 IF need THEN
  -- Approval evidence is API-written: a signal row joined to its history entry,
  -- by an actor other than the starter. Workers cannot insert signals.
  IF NOT EXISTS(SELECT 1 FROM runtime.workflow_signals s JOIN runtime.workflow_history hs ON hs.tenant_id=s.tenant_id AND hs.instance_id=s.instance_id AND hs.sequence=s.sequence
   WHERE s.tenant_id=l.tenant_id AND s.instance_id=l.job_id AND hs.event='approve' AND hs.actor_id=s.actor_id AND s.actor_id<>b.actor_id) THEN
   RAISE EXCEPTION 'tool approval evidence required' USING ERRCODE='TL001';END IF;
  -- The approval covers the start snapshot; a changed record invalidates it.
  SELECT r.row_version INTO current_version FROM runtime.records r WHERE r.tenant_id=b.tenant_id AND r.app_id=b.app_id
   AND r.metadata_revision=coalesce(b.data_revision,b.metadata_revision) AND r.ir_hash=coalesce(b.data_hash,b.ir_hash)
   AND r.entity_id=plan->>'entity_id' AND r.id=b.record_id;
  IF current_version IS DISTINCT FROM b.record_version THEN RAISE EXCEPTION 'approved snapshot is stale' USING ERRCODE='TL002';END IF;
 END IF;
 IF rh IS DISTINCT FROM control.tool_request_hash(l.job_id,b.plan_hash,b.inputs,step,ref,ver,h) THEN
  RAISE EXCEPTION 'tool request witness does not match the pinned binding' USING ERRCODE='22023';END IF;
 tool_key:=control.tool_key(l.tenant_id,l.job_id,step,h);
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step FOR UPDATE;
 IF NOT FOUND THEN
  IF k<>'SEND' THEN RAISE EXCEPTION 'lookup requires a prior send' USING ERRCODE='55000';END IF;
  INSERT INTO runtime.tool_invocations(tenant_id,instance_id,step_key,tool_ref,tool_version,tool_hash,idempotency_key,request_hash,mode,state,attempts,unknown_outstanding)
   VALUES(l.tenant_id,l.job_id,step,ref,ver,h,tool_key,rh,m,'SENDING',1,false);
  n:=1;
 ELSE
  IF ROW(inv.tool_ref,inv.tool_version,inv.tool_hash,inv.request_hash,inv.mode) IS DISTINCT FROM ROW(ref,ver,h,rh,m) THEN RAISE EXCEPTION 'tool attempt witness changed' USING ERRCODE='55000';END IF;
  -- M10R-01: while an unknown outcome is outstanding only these may follow:
  -- a lookup (LOOKUP_BY_REFERENCE; also any stored legacy plan, a lookup is a
  -- read), an in-window keyed resend whose deadline also ends inside the
  -- replay window, a safe read retry, or (after a lookup proved absence or an
  -- operator NOT_APPLIED, which clear it) a fresh send. A legacy plan has no
  -- pinned capability, so it never resends after an unknown outcome.
  outstanding:=control.tool_outstanding(l.tenant_id,l.job_id,step);
  c:=st->'tool_contract';
  IF inv.state='SENDING' OR inv.state IN('SUCCEEDED','APPLIED') THEN RAISE EXCEPTION 'tool attempt not permitted from this state' USING ERRCODE='55000';END IF;
  IF k='LOOKUP' THEN
   IF NOT outstanding OR c IS NOT NULL AND c->>'idempotency_mode' IS DISTINCT FROM 'LOOKUP_BY_REFERENCE' THEN RAISE EXCEPTION 'tool lookup not permitted from this state' USING ERRCODE='55000';END IF;
  ELSIF inv.state='NOT_APPLIED' AND NOT outstanding THEN NULL;
  ELSIF NOT outstanding THEN
   IF NOT (inv.state='FAILED' AND inv.retryable) THEN RAISE EXCEPTION 'tool attempt not permitted from this state' USING ERRCODE='55000';END IF;
  ELSIF NOT (c IS NOT NULL AND c->>'retry_mode'='SAFE_RETRY' AND (
    c->>'idempotency_mode' IN('NATIVE_KEY','DEDUPE_WINDOW') AND clock_timestamp()+((c->>'timeout_ms')::bigint)*interval '1 millisecond'<inv.first_sent_at+((c->>'replay_window_ms')::bigint)*interval '1 millisecond'
    OR c->>'idempotency_mode'='NONE' AND c->>'side_effect' IN('NONE','READ'))) THEN
   RAISE EXCEPTION 'unknown tool outcome outstanding; reconcile before resending' USING ERRCODE='TL003';
  END IF;
  -- M10R-03: a distinct code, so the runtime settles the step (DEAD, or
  -- RECONCILE while unknown) instead of retrying a database refusal forever.
  IF inv.attempts>=20 THEN RAISE EXCEPTION 'tool attempt budget exhausted' USING ERRCODE='TL004';END IF;
  n:=inv.attempts+1;
  UPDATE runtime.tool_invocations v SET state='SENDING',attempts=n,unknown_outstanding=outstanding,updated_at=clock_timestamp() WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 END IF;
 INSERT INTO runtime.tool_attempts(tenant_id,instance_id,step_key,attempt,kind,mode,lease_generation) VALUES(l.tenant_id,l.job_id,step,n,k,m,l.generation);
 RETURN QUERY SELECT n,tool_key,l.remaining_ms;
END $$;
CREATE OR REPLACE FUNCTION control.tool_finish(step text,n integer,outcome_in text,class_in text,retry_in boolean,status_in integer,simulated_in boolean,body bytea) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;att runtime.tool_attempts%ROWTYPE;inv runtime.tool_invocations%ROWTYPE;next_state text;outstanding boolean;c jsonb;proved boolean:=false;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND THEN RAISE EXCEPTION 'exact tool lease required' USING ERRCODE='42501';END IF;
 IF outcome_in NOT IN('SUCCESS','FAILED','UNKNOWN_REQUIRES_RECONCILIATION') OR class_in IS NULL OR class_in IN('','lost','reconciled') OR retry_in IS NULL OR simulated_in IS NULL
  OR status_in IS NULL OR status_in NOT BETWEEN 0 AND 599 OR (outcome_in='SUCCESS')<>(body IS NOT NULL) OR octet_length(body)>16384 OR (outcome_in='SUCCESS')<>(class_in='ok') THEN
  RAISE EXCEPTION 'invalid tool outcome' USING ERRCODE='22023';END IF;
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step FOR UPDATE;
 SELECT * INTO att FROM runtime.tool_attempts a WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=n FOR UPDATE;
 IF runtime.instance_in_frozen_sandbox(l.tenant_id,l.job_id) THEN RAISE EXCEPTION 'sandbox is frozen' USING ERRCODE='55000';END IF;
 IF inv.state IS DISTINCT FROM 'SENDING' OR inv.attempts<>n OR att.finished_at IS NOT NULL OR att.lease_generation IS DISTINCT FROM l.generation THEN
  RAISE EXCEPTION 'stale tool outcome rejected' USING ERRCODE='55000';END IF;
 IF class_in='not_found' AND (att.kind<>'LOOKUP' OR outcome_in<>'FAILED') THEN RAISE EXCEPTION 'invalid lookup outcome' USING ERRCODE='22023';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome=outcome_in,outcome_class=class_in,http_status=status_in WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=n;
 outstanding:=coalesce(inv.unknown_outstanding,true);
 IF outcome_in='SUCCESS' THEN next_state:='SUCCEEDED';outstanding:=false;
 ELSIF att.kind='LOOKUP' AND class_in='not_found' THEN
  -- M10R-05: a 404 proves absence only for a pinned contract that declares a
  -- provider settle interval, and only when this lookup started after the
  -- latest send's deadline plus that interval. Otherwise (early lookup,
  -- undeclared interval, stored legacy plan) the write stays UNKNOWN.
  SELECT s->'tool_contract' INTO c FROM runtime.workflow_bindings b,jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE b.tenant_id=l.tenant_id AND b.id=l.job_id AND s->>'id'=step;
  IF c IS NOT NULL AND c->>'idempotency_mode'='LOOKUP_BY_REFERENCE' AND (c->>'settle_ms')::bigint>0 THEN
   SELECT att.started_at>=max(a.started_at)+((c->>'timeout_ms')::bigint+(c->>'settle_ms')::bigint)*interval '1 millisecond' INTO proved
    FROM runtime.tool_attempts a WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.kind='SEND';
  END IF;
  IF coalesce(proved,false) THEN next_state:='NOT_APPLIED';outstanding:=false;ELSE next_state:='UNKNOWN';outstanding:=true;END IF;
 ELSIF att.kind='LOOKUP' OR outcome_in<>'FAILED' OR outstanding THEN next_state:='UNKNOWN';outstanding:=true;
 ELSE next_state:='FAILED';
 END IF;
 UPDATE runtime.tool_invocations v SET state=next_state,outcome_class=class_in,retryable=retry_in AND next_state='FAILED',http_status=status_in,simulated=simulated_in,unknown_outstanding=outstanding,
  response=body,response_hash=CASE WHEN body IS NULL THEN NULL ELSE encode(sha256(body),'hex') END,updated_at=clock_timestamp()
  WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 RETURN next_state;
END $$;
CREATE OR REPLACE FUNCTION control.tool_abandon(step text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;inv runtime.tool_invocations%ROWTYPE;att runtime.tool_attempts%ROWTYPE;
BEGIN
 SELECT * INTO l FROM control.tool_lease();
 IF NOT FOUND OR NOT coalesce((SELECT d.allowed FROM control.worker_policy_decision() d),false) THEN RAISE EXCEPTION 'exact tool lease and live capability required' USING ERRCODE='42501';END IF;
 SELECT * INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step FOR UPDATE;
 IF NOT FOUND OR inv.state<>'SENDING' THEN RETURN false;END IF;
 IF runtime.instance_in_frozen_sandbox(l.tenant_id,l.job_id) THEN RAISE EXCEPTION 'sandbox is frozen' USING ERRCODE='55000';END IF;
 SELECT * INTO att FROM runtime.tool_attempts a WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=inv.attempts FOR UPDATE;
 IF att.finished_at IS NOT NULL OR NOT (att.lease_generation<l.generation OR att.started_at<clock_timestamp()-interval '3 seconds') THEN RAISE EXCEPTION 'tool attempt may still be in flight' USING ERRCODE='55000';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome='UNKNOWN_REQUIRES_RECONCILIATION',outcome_class='lost' WHERE a.tenant_id=l.tenant_id AND a.instance_id=l.job_id AND a.step_key=step AND a.attempt=inv.attempts;
 UPDATE runtime.tool_invocations v SET state='UNKNOWN',outcome_class='lost',retryable=false,unknown_outstanding=true,updated_at=clock_timestamp() WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 RETURN true;
END $$;
CREATE OR REPLACE FUNCTION control.tool_reconcile(t uuid,i uuid,step text,applied boolean) RETURNS boolean
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
 IF runtime.instance_in_frozen_sandbox(t,i) THEN RAISE EXCEPTION 'sandbox is frozen' USING ERRCODE='55000';END IF;
 IF inv.state NOT IN('UNKNOWN','SENDING','FAILED') THEN RAISE EXCEPTION 'tool invocation already settled' USING ERRCODE='55000';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome='UNKNOWN_REQUIRES_RECONCILIATION',outcome_class='reconciled' WHERE a.tenant_id=t AND a.instance_id=i AND a.step_key=step AND a.finished_at IS NULL;
 UPDATE runtime.tool_invocations v SET state=CASE WHEN applied THEN 'APPLIED' ELSE 'NOT_APPLIED' END,outcome_class='reconciled',retryable=false,unknown_outstanding=false,updated_at=clock_timestamp()
  WHERE v.tenant_id=t AND v.instance_id=i AND v.step_key=step;
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE (n.nspname='runtime' AND p.proname IN('instance_in_frozen_sandbox','dataset_is_frozen_sandbox','fence_frozen_instance','fence_frozen_dataset_rows','guard_sandbox_dataset_update','register_sandbox','freeze_sandbox')) OR (n.nspname='control' AND p.proname IN('tool_begin','tool_finish','tool_abandon','tool_reconcile')) LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION runtime.register_sandbox(uuid,text,uuid,text),runtime.freeze_sandbox(uuid,text,uuid,text,text) TO framework_app;
GRANT EXECUTE ON FUNCTION control.tool_begin(text,text,text,text,text,text,text,boolean),control.tool_finish(text,integer,text,text,boolean,integer,boolean,bytea),control.tool_abandon(text) TO framework_worker;
GRANT EXECUTE ON FUNCTION control.tool_reconcile(uuid,uuid,text,boolean) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain sandbox evidence; forward recovery only';END $$;
-- +goose StatementEnd
