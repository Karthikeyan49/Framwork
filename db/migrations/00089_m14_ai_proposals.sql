-- M14 AI proposals, traceability and budgets (ADR104 amendments A1-A4). Additive and
-- forward-only. A model holds no authority and has no identity: proposals, their
-- per-attempt trace and the per-tenant daily usage are written ONLY through definer
-- ports that derive tenant and actor from the leased session settings, exactly like the
-- M12/M13 ports. The runtime role has no DML on these tables; the model call itself
-- happens outside any transaction (begin -> provider -> record + finish). Raw prompts and
-- raw model output are never columns here: hashes, the validated canonical body and
-- server-derived facts only.
-- Replaced functions: none (the worker drift guard of migration 87 is derived and needs no extension).
-- +goose Up
CREATE TABLE control.ai_usage(
 tenant_id uuid NOT NULL,usage_day date NOT NULL,
 requests bigint NOT NULL DEFAULT 0 CHECK(requests>=0),
 used_tokens bigint NOT NULL DEFAULT 0 CHECK(used_tokens>=0),
 PRIMARY KEY(tenant_id,usage_day)
);
SELECT runtime.install_tenant_rls('control.ai_usage');
REVOKE INSERT,UPDATE,DELETE ON control.ai_usage FROM framework_app;
CREATE POLICY worker_no_generic ON control.ai_usage AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE control.ai_proposals(
 tenant_id uuid NOT NULL,app_id text NOT NULL,id uuid NOT NULL DEFAULT gen_random_uuid(),
 kind text NOT NULL CHECK(kind IN('patch','requirements')),
 requester_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 request_hash text NOT NULL CHECK(request_hash ~ '^[0-9a-f]{64}$'),
 source_revision bigint NOT NULL CHECK(source_revision BETWEEN 1 AND 1000000000),
 draft_hash text NOT NULL CHECK(draft_hash ~ '^[0-9a-f]{64}$'),
 usage_day date NOT NULL,reserved_tokens bigint NOT NULL CHECK(reserved_tokens BETWEEN 1 AND 10000000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),expires_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN('PENDING','READY','NEEDS_HUMAN','FAILED','EXPIRED','APPLIED','CONFIRMED','REJECTED')),
 finished_at timestamptz,
 context_hash text CHECK(context_hash ~ '^[0-9a-f]{64}$'),
 proposal_hash text CHECK(proposal_hash ~ '^[0-9a-f]{64}$'),
 body bytea CHECK(body IS NULL OR octet_length(body) BETWEEN 2 AND 262144),
 derived jsonb CHECK(derived IS NULL OR jsonb_typeof(derived)='object'),
 diagnostics jsonb CHECK(diagnostics IS NULL OR (jsonb_typeof(diagnostics)='array' AND jsonb_array_length(diagnostics)<=40)),
 diagnostics_hash text CHECK(diagnostics_hash ~ '^[0-9a-f]{64}$'),
 failure_code text CHECK(failure_code IN('PARSE_ERROR','SCHEMA_ERROR','FORBIDDEN_TARGET','CONFLICT','COMPILE_ERROR','PROVIDER_ERROR','TIMEOUT','BUDGET','EXPIRED','NOT_CONFIGURED','INTERNAL')),
 attempts smallint CHECK(attempts BETWEEN 0 AND 16),
 input_tokens bigint CHECK(input_tokens BETWEEN 0 AND 100000000),output_tokens bigint CHECK(output_tokens BETWEEN 0 AND 100000000),
 decided_by uuid,decided_at timestamptz,
 reject_reason text CHECK(reject_reason IN('NOT_WANTED','INCORRECT','UNSAFE','SUPERSEDED','OTHER')),
 result_revision bigint CHECK(result_revision BETWEEN 1 AND 1000000000),
 result_hash text CHECK(result_hash ~ '^[0-9a-f]{64}$'),
 decision_hash text CHECK(decision_hash ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,app_id,id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,app_id) REFERENCES control.metadata_heads(tenant_id,id),
 FOREIGN KEY(tenant_id,usage_day) REFERENCES control.ai_usage(tenant_id,usage_day),
 CONSTRAINT ai_proposal_hash_matches CHECK(proposal_hash IS NOT DISTINCT FROM CASE WHEN body IS NULL THEN NULL ELSE encode(sha256(body),'hex') END),
 CONSTRAINT ai_proposal_shape CHECK(
  (status='PENDING' AND finished_at IS NULL AND body IS NULL AND derived IS NULL AND failure_code IS NULL AND decided_by IS NULL AND decided_at IS NULL AND reject_reason IS NULL AND result_revision IS NULL AND result_hash IS NULL AND decision_hash IS NULL)
  OR (status='READY' AND finished_at IS NOT NULL AND body IS NOT NULL AND derived IS NOT NULL AND failure_code IS NULL AND decided_by IS NULL AND decided_at IS NULL AND reject_reason IS NULL AND result_revision IS NULL AND result_hash IS NULL AND decision_hash IS NULL)
  OR (status='NEEDS_HUMAN' AND finished_at IS NOT NULL AND failure_code IS NULL AND decided_by IS NULL AND decided_at IS NULL AND reject_reason IS NULL AND result_revision IS NULL AND result_hash IS NULL AND decision_hash IS NULL)
  OR (status='FAILED' AND finished_at IS NOT NULL AND body IS NULL AND derived IS NULL AND failure_code IS NOT NULL AND decided_by IS NULL AND decided_at IS NULL AND reject_reason IS NULL AND result_revision IS NULL AND result_hash IS NULL AND decision_hash IS NULL)
  OR (status='EXPIRED' AND finished_at IS NOT NULL AND body IS NULL AND derived IS NULL AND failure_code='EXPIRED' AND decided_by IS NULL AND decided_at IS NULL AND reject_reason IS NULL AND result_revision IS NULL AND result_hash IS NULL AND decision_hash IS NULL)
  OR (status='APPLIED' AND kind='patch' AND finished_at IS NOT NULL AND body IS NOT NULL AND derived IS NOT NULL AND failure_code IS NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND reject_reason IS NULL AND result_revision IS NOT NULL AND result_hash IS NOT NULL AND decision_hash IS NOT NULL)
  OR (status='CONFIRMED' AND kind='requirements' AND finished_at IS NOT NULL AND body IS NOT NULL AND derived IS NOT NULL AND failure_code IS NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND reject_reason IS NULL AND result_revision IS NOT NULL AND result_hash IS NOT NULL AND decision_hash IS NOT NULL)
  OR (status='REJECTED' AND finished_at IS NOT NULL AND body IS NOT NULL AND derived IS NOT NULL AND failure_code IS NULL AND decided_by IS NOT NULL AND decided_at IS NOT NULL AND reject_reason IS NOT NULL AND result_revision IS NULL AND result_hash IS NULL AND decision_hash IS NOT NULL))
);
SELECT runtime.install_tenant_rls('control.ai_proposals');
REVOKE INSERT,UPDATE,DELETE ON control.ai_proposals FROM framework_app;
CREATE POLICY worker_no_generic ON control.ai_proposals AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
-- A revision or requirement-set version is the result of at most one proposal.
CREATE UNIQUE INDEX ai_proposals_result ON control.ai_proposals(tenant_id,app_id,kind,result_revision) WHERE result_revision IS NOT NULL;
CREATE INDEX ai_proposals_pending ON control.ai_proposals(tenant_id,expires_at) WHERE status='PENDING';
CREATE INDEX ai_proposals_list ON control.ai_proposals(tenant_id,app_id,created_at DESC,id);
CREATE TABLE control.ai_invocations(
 tenant_id uuid NOT NULL,app_id text NOT NULL,proposal_id uuid NOT NULL,attempt smallint NOT NULL CHECK(attempt BETWEEN 1 AND 16),
 provider text NOT NULL CHECK(provider ~ '^[A-Za-z0-9._:/@+-]{1,64}$'),
 model text NOT NULL CHECK(model ~ '^[A-Za-z0-9._:/@+-]{0,128}$'),
 template_version text NOT NULL CHECK(template_version ~ '^[A-Za-z0-9._-]{1,40}$'),
 context_hash text NOT NULL CHECK(context_hash='' OR context_hash ~ '^[0-9a-f]{64}$'),
 request_hash text NOT NULL CHECK(request_hash='' OR request_hash ~ '^[0-9a-f]{64}$'),
 output_hash text NOT NULL CHECK(output_hash='' OR output_hash ~ '^[0-9a-f]{64}$'),
 input_tokens bigint NOT NULL CHECK(input_tokens BETWEEN 0 AND 100000000),output_tokens bigint NOT NULL CHECK(output_tokens BETWEEN 0 AND 100000000),
 latency_ms integer NOT NULL CHECK(latency_ms BETWEEN 0 AND 3600000),
 outcome text NOT NULL CHECK(outcome IN('OK','PARSE_ERROR','SCHEMA_ERROR','FORBIDDEN_TARGET','CONFLICT','COMPILE_ERROR','PROVIDER_ERROR','TIMEOUT','BUDGET')),
 diagnostics_hash text NOT NULL CHECK(diagnostics_hash='' OR diagnostics_hash ~ '^[0-9a-f]{64}$'),
 compiler_hash text NOT NULL CHECK(compiler_hash ~ '^[0-9a-f]{64}$'),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,proposal_id,attempt),
 FOREIGN KEY(tenant_id,app_id,proposal_id) REFERENCES control.ai_proposals(tenant_id,app_id,id)
);
SELECT runtime.install_tenant_rls('control.ai_invocations');
REVOKE INSERT,UPDATE,DELETE ON control.ai_invocations FROM framework_app;
CREATE POLICY worker_no_generic ON control.ai_invocations AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
-- Proposals and traces are never deleted or truncated, even by an operator mistake;
-- traces and usage are never edited back.
CREATE TRIGGER retain_ai_proposals BEFORE DELETE OR TRUNCATE ON control.ai_proposals FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_ai_invocations BEFORE UPDATE OR DELETE OR TRUNCATE ON control.ai_invocations FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_ai_usage BEFORE DELETE OR TRUNCATE ON control.ai_usage FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
-- The AI tables are written only by the ports: a schema-owner definer called from a
-- runtime session. An operator session cannot add or edit a row by hand.
CREATE FUNCTION control.ai_port_write() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF session_user<>'framework_app' OR current_user<>'framework_schema_owner' THEN
  RAISE EXCEPTION 'AI records are written only through the AI ports' USING ERRCODE='42501';END IF;
 RETURN NEW;
END $$;
-- Status moves only along the ADR104 A4 machine and nothing already set is edited.
CREATE FUNCTION control.ai_proposal_step() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF ROW(NEW.tenant_id,NEW.app_id,NEW.id,NEW.kind,NEW.requester_id,NEW.request_hash,NEW.source_revision,NEW.draft_hash,NEW.usage_day,NEW.reserved_tokens,NEW.created_at,NEW.expires_at)
  IS DISTINCT FROM ROW(OLD.tenant_id,OLD.app_id,OLD.id,OLD.kind,OLD.requester_id,OLD.request_hash,OLD.source_revision,OLD.draft_hash,OLD.usage_day,OLD.reserved_tokens,OLD.created_at,OLD.expires_at) THEN
  RAISE EXCEPTION 'AI proposal identity is immutable' USING ERRCODE='55000';END IF;
 IF OLD.status='PENDING' THEN
  IF NEW.status NOT IN('READY','NEEDS_HUMAN','FAILED','EXPIRED') THEN
   RAISE EXCEPTION 'a pending AI proposal finishes as READY, NEEDS_HUMAN, FAILED or EXPIRED' USING ERRCODE='55000';END IF;
  IF ROW(NEW.decided_by,NEW.decided_at,NEW.reject_reason,NEW.result_revision,NEW.result_hash,NEW.decision_hash)
   IS DISTINCT FROM ROW(NULL::uuid,NULL::timestamptz,NULL::text,NULL::bigint,NULL::text,NULL::text) THEN
   RAISE EXCEPTION 'a pending AI proposal has no decision' USING ERRCODE='55000';END IF;
 ELSIF OLD.status='READY' THEN
  IF NEW.status NOT IN('APPLIED','CONFIRMED','REJECTED') THEN
   RAISE EXCEPTION 'a ready AI proposal is applied, confirmed or rejected' USING ERRCODE='55000';END IF;
  IF ROW(NEW.finished_at,NEW.context_hash,NEW.proposal_hash,NEW.body,NEW.derived,NEW.diagnostics,NEW.diagnostics_hash,NEW.failure_code,NEW.attempts,NEW.input_tokens,NEW.output_tokens)
   IS DISTINCT FROM ROW(OLD.finished_at,OLD.context_hash,OLD.proposal_hash,OLD.body,OLD.derived,OLD.diagnostics,OLD.diagnostics_hash,OLD.failure_code,OLD.attempts,OLD.input_tokens,OLD.output_tokens) THEN
   RAISE EXCEPTION 'the stored AI proposal is immutable' USING ERRCODE='55000';END IF;
 ELSE
  RAISE EXCEPTION 'the AI proposal is in a terminal status' USING ERRCODE='55000';
 END IF;
 RETURN NEW;
END $$;
-- Usage counters only grow.
CREATE FUNCTION control.ai_usage_step() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NEW.tenant_id<>OLD.tenant_id OR NEW.usage_day<>OLD.usage_day OR NEW.requests<OLD.requests OR NEW.used_tokens<OLD.used_tokens THEN
  RAISE EXCEPTION 'AI usage only grows' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER ai_port_write BEFORE INSERT OR UPDATE ON control.ai_proposals FOR EACH ROW EXECUTE FUNCTION control.ai_port_write();
CREATE TRIGGER ai_port_write BEFORE INSERT OR UPDATE ON control.ai_invocations FOR EACH ROW EXECUTE FUNCTION control.ai_port_write();
CREATE TRIGGER ai_port_write BEFORE INSERT OR UPDATE ON control.ai_usage FOR EACH ROW EXECUTE FUNCTION control.ai_port_write();
CREATE TRIGGER ai_proposal_step BEFORE UPDATE ON control.ai_proposals FOR EACH ROW EXECUTE FUNCTION control.ai_proposal_step();
CREATE TRIGGER ai_usage_step BEFORE UPDATE ON control.ai_usage FOR EACH ROW EXECUTE FUNCTION control.ai_usage_step();
-- +goose StatementBegin
-- Admission. Authority (active non-viewer member), the draft head the caller read, the
-- per-tenant in-flight limit and the daily token budget are all decided here, under one
-- tenant-wide advisory lock, so concurrent requests cannot both pass a limit. The budget
-- and in-flight numbers come from the API's configuration but cannot exceed the fixed
-- ceilings below. Stale PENDING rows are expired (and charged pessimistically).
CREATE FUNCTION control.ai_begin(p_app text,p_kind text,p_request_hash text,p_source_revision bigint,p_draft_hash text,p_daily_tokens bigint,p_reserve_tokens bigint,p_inflight integer,p_lease_seconds integer) RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();today date:=(clock_timestamp() AT TIME ZONE 'UTC')::date;head bigint;used bigint;reserved bigint;inflight bigint;pid uuid;x record;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant AI requester required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR length(p_app)>160 OR p_app !~ '^app\.[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$' OR p_kind IS NULL OR p_kind NOT IN('patch','requirements')
  OR p_request_hash IS NULL OR p_request_hash !~ '^[0-9a-f]{64}$' OR p_draft_hash IS NULL OR p_draft_hash !~ '^[0-9a-f]{64}$'
  OR p_source_revision IS NULL OR p_source_revision NOT BETWEEN 1 AND 1000000000
  OR p_daily_tokens IS NULL OR p_daily_tokens NOT BETWEEN 1 AND 100000000 OR p_reserve_tokens IS NULL OR p_reserve_tokens NOT BETWEEN 1 AND 10000000
  OR p_inflight IS NULL OR p_inflight NOT BETWEEN 1 AND 16 OR p_lease_seconds IS NULL OR p_lease_seconds NOT BETWEEN 1 AND 600 THEN
  RAISE EXCEPTION 'invalid AI request' USING ERRCODE='22023';END IF;
 SELECT h.revision INTO head FROM control.metadata_heads h WHERE h.tenant_id=t AND h.id=p_app;
 IF NOT FOUND THEN RAISE EXCEPTION 'application draft not found' USING ERRCODE='P0002';END IF;
 IF head<>p_source_revision OR NOT EXISTS(SELECT 1 FROM control.metadata_versions v WHERE v.tenant_id=t AND v.app_id=p_app AND v.revision=head AND v.source_hash=p_draft_hash) THEN
  RAISE EXCEPTION 'the application draft changed' USING ERRCODE='55000',DETAIL='draft_changed';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('framework.ai:'||t::text,0));
 FOR x IN SELECT p.app_id,p.id,p.usage_day,p.reserved_tokens FROM control.ai_proposals p WHERE p.tenant_id=t AND p.status='PENDING' AND p.expires_at<=clock_timestamp() ORDER BY p.created_at LIMIT 100 LOOP
  UPDATE control.ai_proposals p SET status='EXPIRED',finished_at=clock_timestamp(),failure_code='EXPIRED' WHERE p.tenant_id=t AND p.app_id=x.app_id AND p.id=x.id AND p.status='PENDING';
  UPDATE control.ai_usage s SET used_tokens=s.used_tokens+x.reserved_tokens WHERE s.tenant_id=t AND s.usage_day=x.usage_day;
 END LOOP;
 INSERT INTO control.ai_usage(tenant_id,usage_day) VALUES(t,today) ON CONFLICT DO NOTHING;
 SELECT s.used_tokens INTO used FROM control.ai_usage s WHERE s.tenant_id=t AND s.usage_day=today;
 SELECT count(*),coalesce(sum(p.reserved_tokens),0) INTO inflight,reserved FROM control.ai_proposals p WHERE p.tenant_id=t AND p.status='PENDING' AND p.expires_at>clock_timestamp();
 IF inflight>=p_inflight THEN RAISE EXCEPTION 'too many AI requests are in flight' USING ERRCODE='53400',DETAIL='in_flight';END IF;
 IF used+reserved+p_reserve_tokens>p_daily_tokens THEN RAISE EXCEPTION 'the daily AI token budget is exhausted' USING ERRCODE='53400',DETAIL='daily_budget';END IF;
 INSERT INTO control.ai_proposals(tenant_id,app_id,kind,request_hash,source_revision,draft_hash,usage_day,reserved_tokens,expires_at)
  VALUES(t,p_app,p_kind,p_request_hash,p_source_revision,p_draft_hash,today,p_reserve_tokens,clock_timestamp()+make_interval(secs=>p_lease_seconds)) RETURNING id INTO pid;
 UPDATE control.ai_usage s SET requests=s.requests+1 WHERE s.tenant_id=t AND s.usage_day=today;
 RETURN pid;
END $$;
-- One trace row per provider attempt: hashes, labels, counts and a closed outcome. The
-- proposal must be the caller's, PENDING and unexpired.
CREATE FUNCTION control.ai_record_attempt(p_app text,p_proposal uuid,p_attempt integer,p_provider text,p_model text,p_template text,p_context_hash text,p_request_hash text,p_output_hash text,p_input_tokens bigint,p_output_tokens bigint,p_latency_ms integer,p_outcome text,p_diagnostics_hash text,p_compiler_hash text) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();r record;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant AI requester required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_proposal IS NULL OR p_attempt IS NULL OR p_attempt NOT BETWEEN 1 AND 16
  OR p_provider IS NULL OR p_provider !~ '^[A-Za-z0-9._:/@+-]{1,64}$' OR p_model IS NULL OR p_model !~ '^[A-Za-z0-9._:/@+-]{0,128}$' OR p_template IS NULL OR p_template !~ '^[A-Za-z0-9._-]{1,40}$'
  OR p_context_hash IS NULL OR (p_context_hash<>'' AND p_context_hash !~ '^[0-9a-f]{64}$') OR p_request_hash IS NULL OR (p_request_hash<>'' AND p_request_hash !~ '^[0-9a-f]{64}$')
  OR p_output_hash IS NULL OR (p_output_hash<>'' AND p_output_hash !~ '^[0-9a-f]{64}$') OR p_diagnostics_hash IS NULL OR (p_diagnostics_hash<>'' AND p_diagnostics_hash !~ '^[0-9a-f]{64}$')
  OR p_compiler_hash IS NULL OR p_compiler_hash !~ '^[0-9a-f]{64}$'
  OR p_input_tokens IS NULL OR p_input_tokens NOT BETWEEN 0 AND 100000000 OR p_output_tokens IS NULL OR p_output_tokens NOT BETWEEN 0 AND 100000000
  OR p_latency_ms IS NULL OR p_latency_ms NOT BETWEEN 0 AND 3600000
  OR p_outcome IS NULL OR p_outcome NOT IN('OK','PARSE_ERROR','SCHEMA_ERROR','FORBIDDEN_TARGET','CONFLICT','COMPILE_ERROR','PROVIDER_ERROR','TIMEOUT','BUDGET') THEN
  RAISE EXCEPTION 'invalid AI attempt' USING ERRCODE='22023';END IF;
 SELECT p.requester_id,p.status,p.expires_at INTO r FROM control.ai_proposals p WHERE p.tenant_id=t AND p.app_id=p_app AND p.id=p_proposal FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'AI proposal not found' USING ERRCODE='P0002';END IF;
 IF r.requester_id<>actor THEN RAISE EXCEPTION 'only the requester records attempts' USING ERRCODE='42501';END IF;
 IF r.status<>'PENDING' THEN RAISE EXCEPTION 'the AI proposal is not pending' USING ERRCODE='55000',DETAIL='not_pending';END IF;
 IF r.expires_at<=clock_timestamp() THEN RAISE EXCEPTION 'the AI proposal expired' USING ERRCODE='55000',DETAIL='expired';END IF;
 INSERT INTO control.ai_invocations(tenant_id,app_id,proposal_id,attempt,provider,model,template_version,context_hash,request_hash,output_hash,input_tokens,output_tokens,latency_ms,outcome,diagnostics_hash,compiler_hash)
  VALUES(t,p_app,p_proposal,p_attempt,p_provider,p_model,p_template,p_context_hash,p_request_hash,p_output_hash,p_input_tokens,p_output_tokens,p_latency_ms,p_outcome,p_diagnostics_hash,p_compiler_hash);
END $$;
-- Compare-and-set PENDING -> READY | NEEDS_HUMAN | FAILED. The stored body must hash to
-- the stated proposal hash and carry the schema version of the proposal kind. The
-- actual usage is always charged; a row already past its lease becomes EXPIRED and the
-- function RETURNS 'EXPIRED' (rather than raising) so that charge commits.
CREATE FUNCTION control.ai_finish(p_app text,p_proposal uuid,p_status text,p_body bytea,p_proposal_hash text,p_derived jsonb,p_diagnostics jsonb,p_diagnostics_hash text,p_failure_code text,p_context_hash text,p_attempts integer,p_input_tokens bigint,p_output_tokens bigint) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();r record;doc jsonb;want_version text;want_list jsonb;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant AI requester required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_proposal IS NULL OR p_status IS NULL OR p_status NOT IN('READY','NEEDS_HUMAN','FAILED')
  OR p_attempts IS NULL OR p_attempts NOT BETWEEN 0 AND 16 OR p_input_tokens IS NULL OR p_input_tokens NOT BETWEEN 0 AND 100000000 OR p_output_tokens IS NULL OR p_output_tokens NOT BETWEEN 0 AND 100000000
  OR (p_context_hash IS NOT NULL AND p_context_hash !~ '^[0-9a-f]{64}$') OR (p_diagnostics_hash IS NOT NULL AND p_diagnostics_hash !~ '^[0-9a-f]{64}$')
  OR (p_diagnostics IS NOT NULL AND (jsonb_typeof(p_diagnostics)<>'array' OR jsonb_array_length(p_diagnostics)>40)) OR (p_derived IS NOT NULL AND jsonb_typeof(p_derived)<>'object')
  OR (p_failure_code IS NOT NULL AND p_failure_code NOT IN('PARSE_ERROR','SCHEMA_ERROR','FORBIDDEN_TARGET','CONFLICT','COMPILE_ERROR','PROVIDER_ERROR','TIMEOUT','BUDGET','NOT_CONFIGURED','INTERNAL'))
  OR (p_body IS NOT NULL AND (octet_length(p_body) NOT BETWEEN 2 AND 262144 OR p_proposal_hash IS DISTINCT FROM encode(sha256(p_body),'hex'))) OR (p_body IS NULL AND p_proposal_hash IS NOT NULL) THEN
  RAISE EXCEPTION 'invalid AI result' USING ERRCODE='22023';END IF;
 IF (p_status='READY' AND (p_body IS NULL OR p_derived IS NULL OR p_failure_code IS NOT NULL))
  OR (p_status='NEEDS_HUMAN' AND p_failure_code IS NOT NULL) OR (p_status='FAILED' AND (p_body IS NOT NULL OR p_derived IS NOT NULL OR p_failure_code IS NULL)) THEN
  RAISE EXCEPTION 'AI result does not fit its status' USING ERRCODE='22023';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended('framework.ai:'||t::text,0));
 SELECT p.kind,p.requester_id,p.status,p.expires_at,p.usage_day INTO r FROM control.ai_proposals p WHERE p.tenant_id=t AND p.app_id=p_app AND p.id=p_proposal FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'AI proposal not found' USING ERRCODE='P0002';END IF;
 IF r.requester_id<>actor THEN RAISE EXCEPTION 'only the requester finishes a proposal' USING ERRCODE='42501';END IF;
 IF r.status<>'PENDING' THEN RAISE EXCEPTION 'the AI proposal is not pending' USING ERRCODE='55000',DETAIL='not_pending';END IF;
 IF p_body IS NOT NULL THEN
  BEGIN doc:=convert_from(p_body,'UTF8')::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid AI result' USING ERRCODE='22023';END;
  want_version:=CASE r.kind WHEN 'patch' THEN 'ai-patch-proposal-v1' ELSE 'ai-requirement-proposal-v1' END;
  want_list:=CASE r.kind WHEN 'patch' THEN doc->'operations' ELSE doc->'requirements' END;
  IF jsonb_typeof(doc)<>'object' OR doc->>'version' IS DISTINCT FROM want_version OR jsonb_typeof(want_list) IS DISTINCT FROM 'array' THEN
   RAISE EXCEPTION 'AI result is not a proposal of this kind' USING ERRCODE='22023';END IF;
 END IF;
 UPDATE control.ai_usage s SET used_tokens=s.used_tokens+p_input_tokens+p_output_tokens WHERE s.tenant_id=t AND s.usage_day=r.usage_day;
 IF r.expires_at<=clock_timestamp() THEN
  UPDATE control.ai_proposals p SET status='EXPIRED',finished_at=clock_timestamp(),failure_code='EXPIRED',attempts=p_attempts,input_tokens=p_input_tokens,output_tokens=p_output_tokens WHERE p.tenant_id=t AND p.app_id=p_app AND p.id=p_proposal;
  RETURN 'EXPIRED';
 END IF;
 UPDATE control.ai_proposals p SET status=p_status,finished_at=clock_timestamp(),context_hash=p_context_hash,proposal_hash=p_proposal_hash,body=p_body,derived=p_derived,diagnostics=p_diagnostics,diagnostics_hash=p_diagnostics_hash,
  failure_code=p_failure_code,attempts=p_attempts,input_tokens=p_input_tokens,output_tokens=p_output_tokens WHERE p.tenant_id=t AND p.app_id=p_app AND p.id=p_proposal;
 RETURN p_status;
END $$;
-- The human decision, once: READY -> APPLIED (patch) | CONFIRMED (requirements) | REJECTED.
-- APPLY and CONFIRM name the result the caller just wrote in this transaction (the
-- appended metadata revision, or requirement-set version) and the port checks that it is
-- the current head, carries the stated hash and was written by this very human. The
-- decider need not be the requester (ADR104 A3).
CREATE FUNCTION control.ai_decide(p_app text,p_proposal uuid,p_decision text,p_expected_revision bigint,p_result_revision bigint,p_result_hash text,p_reject_reason text,p_decision_hash text) RETURNS text
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();r record;head bigint;ver record;st text;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant AI decider required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_proposal IS NULL OR p_decision IS NULL OR p_decision NOT IN('APPLY','CONFIRM','REJECT') OR p_decision_hash IS NULL OR p_decision_hash !~ '^[0-9a-f]{64}$'
  OR (p_decision IN('APPLY','CONFIRM') AND (p_expected_revision IS NULL OR p_result_revision IS NULL OR p_result_hash IS NULL OR p_result_hash !~ '^[0-9a-f]{64}$' OR p_reject_reason IS NOT NULL
    OR p_expected_revision NOT BETWEEN (CASE p_decision WHEN 'APPLY' THEN 1 ELSE 0 END) AND 999999999 OR p_result_revision<>p_expected_revision+1))
  OR (p_decision='REJECT' AND (p_expected_revision IS NOT NULL OR p_result_revision IS NOT NULL OR p_result_hash IS NOT NULL OR p_reject_reason IS NULL OR p_reject_reason NOT IN('NOT_WANTED','INCORRECT','UNSAFE','SUPERSEDED','OTHER'))) THEN
  RAISE EXCEPTION 'invalid AI decision' USING ERRCODE='22023';END IF;
 SELECT p.kind,p.status INTO r FROM control.ai_proposals p WHERE p.tenant_id=t AND p.app_id=p_app AND p.id=p_proposal FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'AI proposal not found' USING ERRCODE='P0002';END IF;
 IF r.status<>'READY' THEN
  RAISE EXCEPTION 'the AI proposal is not ready' USING ERRCODE='55000',DETAIL=CASE WHEN r.status IN('APPLIED','CONFIRMED','REJECTED') THEN 'decided' ELSE 'not_ready' END;END IF;
 IF p_decision='APPLY' THEN
  IF r.kind<>'patch' THEN RAISE EXCEPTION 'only a patch proposal is applied' USING ERRCODE='22023';END IF;
  SELECT h.revision INTO head FROM control.metadata_heads h WHERE h.tenant_id=t AND h.id=p_app;
  IF NOT FOUND OR head<>p_result_revision THEN RAISE EXCEPTION 'the application draft changed' USING ERRCODE='55000',DETAIL='draft_changed';END IF;
  SELECT v.source_hash,v.created_by INTO ver FROM control.metadata_versions v WHERE v.tenant_id=t AND v.app_id=p_app AND v.revision=p_result_revision;
  IF NOT FOUND OR ver.source_hash<>p_result_hash OR ver.created_by<>actor THEN RAISE EXCEPTION 'the applied revision is not this decision''s write' USING ERRCODE='22023';END IF;
 ELSIF p_decision='CONFIRM' THEN
  IF r.kind<>'requirements' THEN RAISE EXCEPTION 'only a requirement proposal is confirmed' USING ERRCODE='22023';END IF;
  SELECT coalesce(max(s.version),0) INTO head FROM control.requirement_sets s WHERE s.tenant_id=t AND s.app_id=p_app;
  IF head<>p_result_revision THEN RAISE EXCEPTION 'the requirement set changed' USING ERRCODE='55000',DETAIL='draft_changed';END IF;
  SELECT s.body_hash,s.decided_by INTO ver FROM control.requirement_sets s WHERE s.tenant_id=t AND s.app_id=p_app AND s.version=p_result_revision;
  IF NOT FOUND OR ver.body_hash<>p_result_hash OR ver.decided_by<>actor THEN RAISE EXCEPTION 'the confirmed set is not this decision''s write' USING ERRCODE='22023';END IF;
 ELSIF r.kind NOT IN('patch','requirements') THEN
  RAISE EXCEPTION 'unknown proposal kind' USING ERRCODE='22023';
 END IF;
 st:=CASE p_decision WHEN 'APPLY' THEN 'APPLIED' WHEN 'CONFIRM' THEN 'CONFIRMED' ELSE 'REJECTED' END;
 UPDATE control.ai_proposals p SET status=st,decided_by=actor,decided_at=clock_timestamp(),reject_reason=p_reject_reason,result_revision=p_result_revision,result_hash=p_result_hash,decision_hash=p_decision_hash
  WHERE p.tenant_id=t AND p.app_id=p_app AND p.id=p_proposal;
 RETURN st;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control'
  AND p.proname IN('ai_port_write','ai_proposal_step','ai_usage_step','ai_begin','ai_record_attempt','ai_finish','ai_decide') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.ai_begin(text,text,text,bigint,text,bigint,bigint,integer,integer),
 control.ai_record_attempt(text,uuid,integer,text,text,text,text,text,text,bigint,bigint,integer,text,text,text),
 control.ai_finish(text,uuid,text,bytea,text,jsonb,jsonb,text,text,text,integer,bigint,bigint),
 control.ai_decide(text,uuid,text,bigint,bigint,text,text,text) TO framework_app;
-- The worker drift guard is derived since migration 87 (control.worker_grants_safe refuses every relation of control,
-- runtime, iam and audit that it does not list): the AI tables grant the worker nothing, so it needs no extension.
-- Fail the migration if any runtime role (or PUBLIC) can still write the AI tables
-- directly, if a worker can read them, if a permissive write policy remains, or if the
-- ports lost the owner policy they rely on or got a wrong owner, search_path or grant.
-- +goose StatementBegin
DO $$ DECLARE r record;f regprocedure;BEGIN
 FOR r IN SELECT c.oid::regclass AS tab,g.role FROM pg_catalog.pg_class c CROSS JOIN unnest(ARRAY['public','framework_app','framework_worker','framework_dispatcher']) g(role)
  WHERE c.oid IN('control.ai_proposals'::regclass,'control.ai_invocations'::regclass,'control.ai_usage'::regclass) LOOP
  IF pg_catalog.has_table_privilege(r.role,r.tab,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR pg_catalog.has_any_column_privilege(r.role,r.tab,'INSERT,UPDATE,REFERENCES')
   OR (r.role<>'framework_app' AND (pg_catalog.has_table_privilege(r.role,r.tab,'SELECT') OR pg_catalog.has_any_column_privilege(r.role,r.tab,'SELECT'))) THEN
   RAISE EXCEPTION 'AI table % keeps a direct privilege for %',r.tab,r.role;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_catalog.pg_policy p WHERE p.polrelid IN('control.ai_proposals'::regclass,'control.ai_invocations'::regclass,'control.ai_usage'::regclass)
   AND p.polpermissive AND p.polcmd<>'r' AND p.polname<>'tenant_scope' AND p.polname<>'schema_owner_access') THEN
  RAISE EXCEPTION 'a permissive AI write policy remains';END IF;
 IF (SELECT count(*) FROM pg_catalog.pg_policy p WHERE p.polname='schema_owner_access' AND p.polcmd='*' AND p.polroles=ARRAY['framework_schema_owner'::regrole]::oid[]
   AND p.polrelid IN('control.ai_proposals'::regclass,'control.ai_invocations'::regclass,'control.ai_usage'::regclass))<>3 THEN
  RAISE EXCEPTION 'the AI ports need the schema owner policies';END IF;
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('ai_begin','ai_record_attempt','ai_finish','ai_decide') LOOP
  IF NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.oid=f::oid AND p.prosecdef AND p.proowner='framework_schema_owner'::regrole AND cardinality(p.proconfig)=1 AND p.proconfig[1] ~ '^search_path=pg_catalog, ?pg_temp$')
   OR NOT pg_catalog.has_function_privilege('framework_app',f,'EXECUTE')
   OR pg_catalog.has_function_privilege('public',f,'EXECUTE') OR pg_catalog.has_function_privilege('framework_worker',f,'EXECUTE') OR pg_catalog.has_function_privilege('framework_dispatcher',f,'EXECUTE') THEN
   RAISE EXCEPTION 'AI port % has a wrong owner, search_path or grant',f;END IF;
 END LOOP;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain AI proposals and traces; forward recovery only';END $$;
-- +goose StatementEnd
