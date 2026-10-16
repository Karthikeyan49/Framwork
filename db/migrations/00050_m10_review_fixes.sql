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

-- M10R-01: "an unknown outcome is outstanding" is durable ledger state. NULL
-- marks a row written before this migration (no backfill under forced RLS);
-- its value is derived conservatively from the retained attempts and then
-- materialized by the next port that touches the row.
ALTER TABLE runtime.tool_invocations ADD COLUMN unknown_outstanding boolean;
-- +goose StatementBegin
CREATE FUNCTION control.tool_outstanding(t uuid,i uuid,step text) RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT coalesce(v.unknown_outstanding,v.state IN('UNKNOWN','SENDING') OR v.state NOT IN('SUCCEEDED','APPLIED','NOT_APPLIED') AND EXISTS(SELECT 1 FROM runtime.tool_attempts a WHERE a.tenant_id=v.tenant_id AND a.instance_id=v.instance_id AND a.step_key=v.step_key AND (a.outcome='UNKNOWN_REQUIRES_RECONCILIATION' OR a.finished_at IS NULL)))
 FROM runtime.tool_invocations v WHERE v.tenant_id=t AND v.instance_id=i AND v.step_key=step
$$;
-- +goose StatementEnd
-- +goose StatementBegin
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
  IF inv.attempts>=20 THEN RAISE EXCEPTION 'tool attempt budget exhausted' USING ERRCODE='55000';END IF;
  n:=inv.attempts+1;
  UPDATE runtime.tool_invocations v SET state='SENDING',attempts=n,unknown_outstanding=outstanding,updated_at=clock_timestamp() WHERE v.tenant_id=l.tenant_id AND v.instance_id=l.job_id AND v.step_key=step;
 END IF;
 INSERT INTO runtime.tool_attempts(tenant_id,instance_id,step_key,attempt,kind,mode,lease_generation) VALUES(l.tenant_id,l.job_id,step,n,k,m,l.generation);
 RETURN QUERY SELECT n,tool_key,l.remaining_ms;
END $$;
-- +goose StatementEnd

-- M10R-01/M10R-04: the invocation state describes the write, not the last
-- attempt. Any non-success attempt while an unknown outcome is outstanding
-- (a failed/rejected/rate-limited lookup, a 429 on a keyed resend) leaves the
-- invocation UNKNOWN; only SUCCESS, a lookup that proved absence or an
-- operator reconciliation settles it. Attempt rows keep their own outcome.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.tool_finish(step text,n integer,outcome_in text,class_in text,retry_in boolean,status_in integer,simulated_in boolean,body bytea) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
#variable_conflict use_column
DECLARE l record;att runtime.tool_attempts%ROWTYPE;inv runtime.tool_invocations%ROWTYPE;next_state text;outstanding boolean;
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
 outstanding:=coalesce(inv.unknown_outstanding,true);
 IF outcome_in='SUCCESS' THEN next_state:='SUCCEEDED';outstanding:=false;
 ELSIF att.kind='LOOKUP' AND class_in='not_found' THEN next_state:='NOT_APPLIED';outstanding:=false;
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
 IF inv.state NOT IN('UNKNOWN','SENDING','FAILED') THEN RAISE EXCEPTION 'tool invocation already settled' USING ERRCODE='55000';END IF;
 UPDATE runtime.tool_attempts a SET finished_at=clock_timestamp(),outcome='UNKNOWN_REQUIRES_RECONCILIATION',outcome_class='reconciled' WHERE a.tenant_id=t AND a.instance_id=i AND a.step_key=step AND a.finished_at IS NULL;
 UPDATE runtime.tool_invocations v SET state=CASE WHEN applied THEN 'APPLIED' ELSE 'NOT_APPLIED' END,outcome_class='reconciled',retryable=false,unknown_outstanding=false,updated_at=clock_timestamp()
  WHERE v.tenant_id=t AND v.instance_id=i AND v.step_key=step;
 RETURN true;
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
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE (n.nspname='control' AND p.proname IN('tool_approval_gated','tool_request_hash','tool_outstanding','tool_begin','tool_finish','tool_abandon','tool_reconcile')) OR (n.nspname='runtime' AND p.proname IN('guard_worker_transition','guard_tool_receipt')) LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.tool_begin(text,text,text,text,text,text,text,boolean),control.tool_finish(text,integer,text,text,boolean,integer,boolean,bytea),control.tool_abandon(text) TO framework_worker;
GRANT EXECUTE ON FUNCTION control.tool_reconcile(uuid,uuid,text,boolean) TO framework_app;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain tool ledger evidence; forward recovery only';END $$;
-- +goose StatementEnd
