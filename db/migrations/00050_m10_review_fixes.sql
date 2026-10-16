-- M10 independent-review fixes (ADR036 Amendments). Additive and forward-only.
-- Migrations 45-48 are preserved; changed ports are replaced in place with the
-- same signatures, owner and ACLs. 49 belongs to another branch (gap intended).
-- +goose Up

-- M10R-02: the database derives the approval requirement from the pinned plan.
-- DSL 2.2.0 tool states pin tool_contract.approval; a stored DSL 2.1.0 plan has
-- no contract and is treated as approval-required whenever the tool state is
-- structurally gated by an approval state (the compiler guaranteed that gate
-- for every REQUIRED tool, so honest legacy executions are unaffected).
-- +goose StatementBegin
CREATE FUNCTION control.tool_approval_gated(plan jsonb,target text) RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 WITH RECURSIVE reach(id) AS (
  SELECT plan->>'initial_state'
  UNION
  SELECT t->>'to' FROM reach r
   JOIN LATERAL (SELECT s FROM jsonb_array_elements(plan->'states') s WHERE s->>'id'=r.id) st ON true
   CROSS JOIN LATERAL jsonb_array_elements(st.s->'transitions') t
   WHERE NOT (st.s->>'type'='approval' AND t->>'on'='approved')
 )
 SELECT NOT EXISTS(SELECT 1 FROM reach WHERE id=target)
$$;
-- The step request identity is derived from the pinned binding, exactly as the
-- runtime computes it; a worker cannot pin a forged request witness.
CREATE FUNCTION control.tool_request_hash(i uuid,plan_hash text,inputs bytea,step text,ref text,ver text,h text) RETURNS text LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT encode(sha256(convert_to('{"Instance":"'||i::text||'","Plan":"'||plan_hash||'","Step":"'||step||'","Activity":"'||ref||'@'||ver||'#'||h||'","Input":"'||encode(sha256(inputs),'hex')||'","Output":"'||encode(sha256(''::bytea),'hex')||'"}','UTF8')),'hex')
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.tool_begin(step text,ref text,ver text,h text,rh text,m text,k text,needs_approval boolean)
RETURNS TABLE(attempt integer,idempotency_key text,remaining_ms integer)
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;sb record;x runtime.workflow_instances%ROWTYPE;b runtime.workflow_bindings%ROWTYPE;plan jsonb;st jsonb;inv runtime.tool_invocations%ROWTYPE;n integer;current_version bigint;tool_key text;need boolean;
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
 SELECT s.tool_ceiling,s.state INTO sb FROM runtime.sandboxes s WHERE s.tenant_id=b.tenant_id AND s.app_id=b.app_id AND s.dataset_id=b.dataset_id;
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

-- M10R-02: the worker's column UPDATE grant on its leased instance is bound to
-- the pinned plan. A worker-made state change must follow an `always`
-- transition of a start/activity/timer state (approval states have only
-- approved/rejected transitions, which only the API signal path takes), and
-- leaving an activity requires its receipt. Runs as the invoking worker, so it
-- sees only its exact job under the existing row policies (fail closed).
-- +goose StatementBegin
CREATE FUNCTION runtime.guard_worker_transition() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE st jsonb;
BEGIN
 IF session_user<>'framework_worker' OR NEW.state_id IS NOT DISTINCT FROM OLD.state_id THEN RETURN NEW;END IF;
 SELECT s INTO st FROM runtime.workflow_bindings b,jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE b.tenant_id=OLD.tenant_id AND b.id=OLD.id AND s->>'id'=OLD.state_id;
 IF st IS NULL OR st->>'type' NOT IN('start','activity','timer') OR OLD.termination<>'' OR NEW.termination<>'' OR NEW.status<>'READY'
  OR NOT EXISTS(SELECT 1 FROM jsonb_array_elements(st->'transitions') t WHERE t->>'on'='always' AND t->>'to'=NEW.state_id)
  OR st->>'type'='activity' AND NOT EXISTS(SELECT 1 FROM runtime.workflow_receipts r WHERE r.tenant_id=OLD.tenant_id AND r.instance_id=OLD.id AND r.step_key=OLD.state_id) THEN
  RAISE EXCEPTION 'workflow transition is not permitted by the pinned plan' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
-- A worker-written receipt for a tool step must equal the settled ledger: the
-- exact pinned activity, the ledger request identity and its response bytes.
CREATE FUNCTION runtime.guard_tool_receipt() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE st jsonb;inv record;
BEGIN
 IF session_user<>'framework_worker' THEN RETURN NEW;END IF;
 SELECT s INTO st FROM runtime.workflow_bindings b,jsonb_array_elements(convert_from(b.plan_body,'UTF8')::jsonb->'states') s WHERE b.tenant_id=NEW.tenant_id AND b.id=NEW.instance_id AND s->>'id'=NEW.step_key;
 IF st IS NULL OR st->>'type'<>'activity' OR NOT (st->'activity' ? 'tool_ref') THEN RETURN NEW;END IF;
 SELECT v.state,v.request_hash,v.response_hash INTO inv FROM runtime.tool_invocations v WHERE v.tenant_id=NEW.tenant_id AND v.instance_id=NEW.instance_id AND v.step_key=NEW.step_key;
 IF NOT FOUND OR inv.state<>'SUCCEEDED' OR inv.request_hash IS DISTINCT FROM NEW.request_hash OR inv.response_hash IS DISTINCT FROM NEW.output_hash
  OR NEW.activity_ref IS DISTINCT FROM (st->'activity'->>'tool_ref')||'@'||(st->'activity'->>'version')||'#'||(st->>'tool_hash') THEN
  RAISE EXCEPTION 'tool receipt requires matching settled ledger evidence' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER guard_worker_transition BEFORE UPDATE ON runtime.workflow_instances FOR EACH ROW EXECUTE FUNCTION runtime.guard_worker_transition();
CREATE TRIGGER guard_tool_receipt BEFORE INSERT ON runtime.workflow_receipts FOR EACH ROW EXECUTE FUNCTION runtime.guard_tool_receipt();

-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE (n.nspname='control' AND p.proname IN('tool_approval_gated','tool_request_hash','tool_begin')) OR (n.nspname='runtime' AND p.proname IN('guard_worker_transition','guard_tool_receipt')) LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.tool_begin(text,text,text,text,text,text,text,boolean) TO framework_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain tool ledger evidence; forward recovery only';END $$;
-- +goose StatementEnd
