-- M12 database-layer review fixes (ADR102 Amendments; docs/verification/M12_DB_INDEPENDENT_REVIEW.md
-- F2, F3, F4, F5). Additive and forward-only; 83, 84 and 85 are preserved. Replaced in place, each from
-- its LATEST definition, with every guard of that definition kept:
--   control.append_requirements, control.author_acceptance, control.stage_candidate (00083)
--   control.worker_grants_safe (00084)
-- New: control.candidate_body_keys, control.strict_candidate_json, control.keys_within,
--   control.worker_guard_granted, control.worker_guard_denied.
-- F1 (framework_app approving/activating directly) was closed by 00084 and F6 (worker-asserted approval flag)
-- by 00085; they get no change here.
--
-- F2  The ports store bytes that Go later re-reads. The jsonb view keeps the last of two equal keys and is
--     case-sensitive, Go's decoder is case-insensitive and rejects numbers such as 0e0. The ports now refuse a
--     body that is not one canonical-looking object: no leading or trailing whitespace, no duplicate member at
--     any depth (the member tokens of the text equal those of its jsonb), no exponent or leading-zero number,
--     only the keys of the Go types in the objects the ports read, and the scalar types they read. The Go
--     recomputation (Restore, VerifyWithBase) stays the control for everything the database cannot recompute.
-- F3  Staging bytes that are already staged (same tenant, app, hash and requester) is a replay and returns
--     false, whatever happened to the base since: the row is immutable and staleness is derived on read.
-- F4  The documents a bundle embeds (source, requirement set, acceptance spec) must equal the stored rows as
--     JSON values; the hashes were already bound, the documents were not.
-- F5  worker_grants_safe is derived, not listed: any privilege the worker effectively holds on any relation of
--     control, runtime, iam and audit (table and column level, REFERENCES and TRIGGER included), and CREATE on
--     those schemas, that worker_guard_granted() does not list makes it false. A relation a later migration adds
--     is therefore refused by default.
-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION control.candidate_body_keys(kind text) RETURNS text[] LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT CASE kind
 WHEN 'bundle' THEN ARRAY['Acceptance','AppID','BaseGeneration','BaseRelease','Dependencies','Diff','Evolution','HighRisk','IR','RequesterID','Requirements','Risk','RuntimeContract','Sandbox','Source','Storage','TenantID','Verification','Version']
 WHEN 'source' THEN ARRAY['Hash','JSON','Revision','SchemaVersion']
 WHEN 'ir' THEN ARRAY['CompilerVersion','Hash','JSON','Version']
 WHEN 'storage' THEN ARRAY['base_schema_hash','class','evolution_plan_hash','first_release','reasons','target_schema_hash']
 WHEN 'risk' THEN ARRAY['Facts','FactsHash','Level','PolicyHash','PolicyVersion','Reasons','Rollback','Separation']
 WHEN 'requirements' THEN ARRAY['Hash','JSON','PolicyVersion','Version']
 WHEN 'acceptance' THEN ARRAY['AuthorID','Hash','ID','JSON','RequirementsHash']
 WHEN 'sandbox' THEN ARRAY['DatasetID','Evidence','EvidenceHash','FrozenBy']
 WHEN 'verification' THEN ARRAY['Outcome','Report','ReportHash','VerifierVersion','VocabularyVersion']
 WHEN 'requirement_set' THEN ARRAY['app_id','requirements','schema_version']
 WHEN 'acceptance_spec' THEN ARRAY['cases','requirements_hash','schema_version']
 END
$$;
-- True when obj is an object whose keys are all in the key set of kind.
CREATE FUNCTION control.keys_within(obj jsonb,kind text) RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT obj IS NOT NULL AND jsonb_typeof(obj)='object' AND NOT EXISTS(SELECT 1 FROM jsonb_object_keys(obj) k WHERE k<>ALL(control.candidate_body_keys(kind)))
$$;
-- The text is one object without surrounding whitespace, with no duplicate member at any depth and no
-- number spelled with an exponent or a leading zero (outside strings). A duplicate member is dropped by
-- the jsonb view and always removes its key, so the string tokens of the text and of its jsonb differ;
-- strings are scanned with an unrolled pattern, so the check is linear.
CREATE FUNCTION control.strict_candidate_json(txt text,doc jsonb) RETURNS boolean LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT left(txt,1)='{' AND right(txt,1)='}'
  AND regexp_count(txt,'"[^"\\]*(?:\\.[^"\\]*)*"')=regexp_count(doc::text,'"[^"\\]*(?:\\.[^"\\]*)*"')
  AND regexp_replace(txt,'"[^"\\]*(?:\\.[^"\\]*)*"','""','g') !~ '(^|[^0-9.eE+-])-?0[0-9]|[0-9][eE]'
$$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.append_requirements(p_app text,p_expected bigint,p_body bytea) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();head bigint;doc jsonb;txt text;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant requirement decider required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_expected IS NULL OR p_expected<0 OR p_expected>=1000000 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 262144 THEN
  RAISE EXCEPTION 'invalid requirement set' USING ERRCODE='22023';END IF;
 BEGIN txt:=convert_from(p_body,'UTF8');doc:=txt::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid requirement set' USING ERRCODE='22023';END;
 IF jsonb_typeof(doc)<>'object' OR doc->>'schema_version' IS DISTINCT FROM 'requirements-v2' OR doc->>'app_id' IS DISTINCT FROM p_app OR jsonb_typeof(doc->'requirements') IS DISTINCT FROM 'array'
  OR NOT control.strict_candidate_json(txt,doc) OR NOT control.keys_within(doc,'requirement_set') THEN
  RAISE EXCEPTION 'invalid requirement set' USING ERRCODE='22023';END IF;
 SELECT coalesce(max(r.version),0) INTO head FROM control.requirement_sets r WHERE r.tenant_id=t AND r.app_id=p_app;
 IF head<>p_expected THEN RAISE EXCEPTION 'requirement set head changed' USING ERRCODE='55000';END IF;
 INSERT INTO control.requirement_sets(tenant_id,app_id,version,body,body_hash) VALUES(t,p_app,p_expected+1,p_body,encode(sha256(p_body),'hex'));
 RETURN p_expected+1;
END $$;
CREATE OR REPLACE FUNCTION control.author_acceptance(p_app text,p_requirements_hash text,p_body bytea) RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();doc jsonb;txt text;h text;found_id uuid;new_id uuid;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant acceptance author required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_requirements_hash IS NULL OR p_requirements_hash !~ '^[0-9a-f]{64}$' OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN
  RAISE EXCEPTION 'invalid acceptance spec' USING ERRCODE='22023';END IF;
 BEGIN txt:=convert_from(p_body,'UTF8');doc:=txt::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid acceptance spec' USING ERRCODE='22023';END;
 IF jsonb_typeof(doc)<>'object' OR doc->>'schema_version' IS DISTINCT FROM 'acceptance-v2' OR doc->>'requirements_hash' IS DISTINCT FROM p_requirements_hash OR jsonb_typeof(doc->'cases') IS DISTINCT FROM 'array'
  OR NOT control.strict_candidate_json(txt,doc) OR NOT control.keys_within(doc,'acceptance_spec') THEN
  RAISE EXCEPTION 'invalid acceptance spec' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM control.requirement_sets r WHERE r.tenant_id=t AND r.app_id=p_app AND r.body_hash=p_requirements_hash) THEN
  RAISE EXCEPTION 'requirement set required' USING ERRCODE='55000';END IF;
 h:=encode(sha256(p_body),'hex');
 SELECT a.id INTO found_id FROM control.acceptance_specs a WHERE a.tenant_id=t AND a.app_id=p_app AND a.body_hash=h AND a.author_id=actor;
 IF FOUND THEN RETURN found_id;END IF;
 INSERT INTO control.acceptance_specs(tenant_id,app_id,requirements_hash,body,body_hash) VALUES(t,p_app,p_requirements_hash,p_body,h) RETURNING id INTO new_id;
 RETURN new_id;
END $$;
CREATE OR REPLACE FUNCTION control.stage_candidate(p_app text,p_hash text,p_body bytea) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();b jsonb;txt text;lvl text;rev bigint;src record;rs record;acc record;ev record;evdoc jsonb;n bigint;
 base text;base_gen bigint:=0;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release builder required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_hash IS NULL OR p_body IS NULL OR octet_length(p_body)>8388608 OR p_hash IS DISTINCT FROM encode(sha256(p_body),'hex') THEN
  RAISE EXCEPTION 'invalid candidate' USING ERRCODE='22023';END IF;
 -- M12R F3: bytes that are already staged by this requester are a replay, whatever happened to the base
 -- since. The row is immutable evidence and staleness is derived on read, never decided at write.
 IF EXISTS(SELECT 1 FROM control.release_candidates c WHERE c.tenant_id=t AND c.app_id=p_app AND c.artifact_hash=p_hash AND c.requester_id=actor) THEN RETURN false;END IF;
 BEGIN txt:=convert_from(p_body,'UTF8');b:=txt::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid candidate' USING ERRCODE='22023';END;
 -- M12R F2: one object, no duplicate member, no exponent number, only the keys of the Go types in the
 -- objects this port reads, and the scalar types it reads (the jsonb view hides all of these).
 IF jsonb_typeof(b)<>'object' OR NOT control.strict_candidate_json(txt,b) OR NOT control.keys_within(b,'bundle')
  OR NOT control.keys_within(b->'Source','source') OR NOT control.keys_within(b->'IR','ir') OR NOT control.keys_within(b->'Storage','storage')
  OR NOT control.keys_within(b->'Risk','risk') OR NOT control.keys_within(b->'Requirements','requirements') OR NOT control.keys_within(b->'Acceptance','acceptance')
  OR NOT control.keys_within(b->'Sandbox','sandbox') OR NOT control.keys_within(b->'Verification','verification')
  OR jsonb_typeof(b->'Source'->'Revision') IS DISTINCT FROM 'number' OR jsonb_typeof(b->'Requirements'->'Version') IS DISTINCT FROM 'number'
  OR jsonb_typeof(b->'BaseGeneration') IS DISTINCT FROM 'number' OR jsonb_typeof(b->'Risk'->'PolicyVersion') IS DISTINCT FROM 'string'
  OR coalesce(b->'Risk'->>'PolicyVersion','') !~ '^[A-Za-z0-9._-]{1,80}$' OR jsonb_typeof(b->'Storage'->'class') IS DISTINCT FROM 'string'
  OR jsonb_typeof(b->'Risk'->'Level') IS DISTINCT FROM 'string' THEN
  RAISE EXCEPTION 'invalid candidate' USING ERRCODE='22023';END IF;
 IF jsonb_typeof(b)<>'object' OR b->>'Version' IS DISTINCT FROM '2.0.0' OR b->>'TenantID' IS DISTINCT FROM t::text OR b->>'AppID' IS DISTINCT FROM p_app
  OR b->>'RequesterID' IS DISTINCT FROM actor::text OR jsonb_typeof(b->'HighRisk') IS DISTINCT FROM 'boolean' OR jsonb_typeof(b->'BaseRelease') IS DISTINCT FROM 'string' THEN
  RAISE EXCEPTION 'candidate identity is not the authenticated request' USING ERRCODE='22023';END IF;
 lvl:=b->'Risk'->>'Level';
 IF lvl IS NULL OR lvl NOT IN('LOW','MEDIUM','HIGH','CRITICAL') OR (b->>'HighRisk') IS DISTINCT FROM (lvl IN('HIGH','CRITICAL'))::text
  OR b->'Verification'->>'Outcome' IS DISTINCT FROM 'PASS' OR length(coalesce(b->'Risk'->>'PolicyVersion','')) NOT BETWEEN 1 AND 80 THEN
  RAISE EXCEPTION 'candidate risk or verification claim is inconsistent' USING ERRCODE='22023';END IF;
 IF coalesce(b->'Source'->>'Revision','') !~ '^[1-9][0-9]{0,9}$' OR coalesce(b->'Requirements'->>'Version','') !~ '^[1-9][0-9]{0,6}$'
  OR coalesce(b->'Acceptance'->>'ID','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(b->'Sandbox'->>'DatasetID','') !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  OR coalesce(b->'IR'->>'Hash','') !~ '^[0-9a-f]{64}$' OR coalesce(b->>'BaseGeneration','') !~ '^[0-9]{1,18}$' THEN
  RAISE EXCEPTION 'invalid candidate bindings' USING ERRCODE='22023';END IF;
 rev:=(b->'Source'->>'Revision')::bigint;
 SELECT v.created_by,v.source_hash,v.document INTO src FROM control.metadata_versions v WHERE v.tenant_id=t AND v.app_id=p_app AND v.revision=rev;
 IF NOT FOUND OR src.source_hash IS DISTINCT FROM b->'Source'->>'Hash' THEN RAISE EXCEPTION 'stored source revision required' USING ERRCODE='55000';END IF;
 SELECT r.body_hash,r.decided_by,r.body INTO rs FROM control.requirement_sets r WHERE r.tenant_id=t AND r.app_id=p_app AND r.version=(b->'Requirements'->>'Version')::bigint;
 IF NOT FOUND OR rs.body_hash IS DISTINCT FROM b->'Requirements'->>'Hash' THEN RAISE EXCEPTION 'stored requirement set required' USING ERRCODE='55000';END IF;
 SELECT a.body_hash,a.author_id,a.requirements_hash,a.body INTO acc FROM control.acceptance_specs a WHERE a.tenant_id=t AND a.app_id=p_app AND a.id=(b->'Acceptance'->>'ID')::uuid;
 IF NOT FOUND OR acc.body_hash IS DISTINCT FROM b->'Acceptance'->>'Hash' OR acc.requirements_hash IS DISTINCT FROM rs.body_hash
  OR b->'Acceptance'->>'RequirementsHash' IS DISTINCT FROM rs.body_hash OR acc.author_id::text IS DISTINCT FROM b->'Acceptance'->>'AuthorID' THEN
  RAISE EXCEPTION 'stored acceptance spec required' USING ERRCODE='55000';END IF;
 -- M12R F4: the documents the bundle embeds are the stored rows (compared as JSON values), not only
 -- their hashes: a body cannot carry one document under another document's hash.
 IF b->'Source'->'JSON' IS DISTINCT FROM src.document OR b->'Requirements'->'JSON' IS DISTINCT FROM convert_from(rs.body,'UTF8')::jsonb
  OR b->'Acceptance'->'JSON' IS DISTINCT FROM convert_from(acc.body,'UTF8')::jsonb THEN
  RAISE EXCEPTION 'embedded document is not the stored row' USING ERRCODE='55000';END IF;
 -- Separation of duties at build time (ADR102 section 5), from DB-stamped identities.
 IF acc.author_id=actor OR acc.author_id=src.created_by THEN
  RAISE EXCEPTION 'acceptance must be authored independently of the requester and the source author' USING ERRCODE='42501';END IF;
 SELECT e.body,e.body_hash,e.frozen_by INTO ev FROM runtime.sandbox_evidence e JOIN runtime.sandboxes s ON s.tenant_id=e.tenant_id AND s.app_id=e.app_id AND s.dataset_id=e.dataset_id
  WHERE e.tenant_id=t AND e.app_id=p_app AND e.dataset_id=(b->'Sandbox'->>'DatasetID')::uuid AND s.state='FROZEN';
 IF NOT FOUND OR ev.body_hash IS DISTINCT FROM b->'Sandbox'->>'EvidenceHash' THEN RAISE EXCEPTION 'frozen sandbox evidence required' USING ERRCODE='55000';END IF;
 evdoc:=convert_from(ev.body,'UTF8')::jsonb;
 IF evdoc->>'ir_hash' IS DISTINCT FROM b->'IR'->>'Hash' OR evdoc->>'metadata_revision' IS DISTINCT FROM rev::text OR evdoc->>'data_mode' IS DISTINCT FROM 'SYNTHETIC_SEPARATE'
  OR b->'Sandbox'->>'Evidence' IS DISTINCT FROM convert_from(ev.body,'UTF8') OR b->'Sandbox'->>'FrozenBy' IS DISTINCT FROM ev.frozen_by::text THEN RAISE EXCEPTION 'sandbox evidence does not match the candidate' USING ERRCODE='55000';END IF;
 base:=b->>'BaseRelease';
 IF base='' THEN
  IF coalesce(b->>'BaseGeneration','0')<>'0' OR EXISTS(SELECT 1 FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app) THEN
   RAISE EXCEPTION 'candidate base is no longer current' USING ERRCODE='55000';END IF;
 ELSE
  base_gen:=(b->>'BaseGeneration')::bigint;
  IF base !~ '^[0-9a-f]{64}$' OR NOT EXISTS(SELECT 1 FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=base AND a.generation=base_gen) THEN
   RAISE EXCEPTION 'candidate base is no longer current' USING ERRCODE='55000';END IF;
 END IF;
 INSERT INTO control.release_bundles(tenant_id,app_id,artifact_hash,body) VALUES(t,p_app,p_hash,p_body) ON CONFLICT DO NOTHING;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n=0 THEN RETURN false;END IF;
 INSERT INTO control.release_candidates(tenant_id,app_id,artifact_hash,requester_id,base_release,base_generation,risk_level,high_risk,risk_policy_version,
  requirements_version,requirements_hash,acceptance_id,acceptance_hash,acceptance_author,sandbox_dataset_id,sandbox_evidence_hash,sandbox_frozen_by,source_revision,source_author,ir_hash)
 VALUES(t,p_app,p_hash,actor,base,base_gen,lvl,lvl IN('HIGH','CRITICAL'),b->'Risk'->>'PolicyVersion',(b->'Requirements'->>'Version')::bigint,rs.body_hash,
  (b->'Acceptance'->>'ID')::uuid,acc.body_hash,acc.author_id,(b->'Sandbox'->>'DatasetID')::uuid,ev.body_hash,ev.frozen_by,rev,src.created_by,b->'IR'->>'Hash');
 RETURN true;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
-- The direct grants of framework_worker on the relations of control, runtime, iam and audit, exactly as
-- the migrations through 00086 gave them (rel, privilege, column; NULL = table level). A test compares
-- this list with the live catalog in both directions.
CREATE FUNCTION control.worker_guard_granted() RETURNS TABLE(rel text,priv text,col text) LANGUAGE sql IMMUTABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT * FROM (VALUES
  ('audit.entries','INSERT','action'),
  ('audit.entries','INSERT','actor_id'),
  ('audit.entries','INSERT','allowed'),
  ('audit.entries','INSERT','decision_id'),
  ('audit.entries','INSERT','policy_version'),
  ('audit.entries','INSERT','request_id'),
  ('audit.entries','INSERT','resource'),
  ('audit.entries','INSERT','state'),
  ('audit.entries','INSERT','tenant_id'),
  ('audit.entries','INSERT','trace_id'),
  ('control.job_routes','SELECT',NULL),
  ('iam.memberships','SELECT',NULL),
  ('iam.team_memberships','SELECT',NULL),
  ('iam.teams','SELECT',NULL),
  ('iam.tenants','SELECT',NULL),
  ('iam.users','SELECT',NULL),
  ('runtime.event_envelopes','SELECT',NULL),
  ('runtime.event_inbox','INSERT','body_hash'),
  ('runtime.event_inbox','INSERT','consumer_id'),
  ('runtime.event_inbox','INSERT','event_id'),
  ('runtime.event_inbox','INSERT','tenant_id'),
  ('runtime.event_inbox','SELECT',NULL),
  ('runtime.tool_attempts','SELECT',NULL),
  ('runtime.tool_invocations','SELECT',NULL),
  ('runtime.workflow_bindings','SELECT',NULL),
  ('runtime.workflow_history','INSERT','event'),
  ('runtime.workflow_history','INSERT','instance_id'),
  ('runtime.workflow_history','INSERT','result_hash'),
  ('runtime.workflow_history','INSERT','sequence'),
  ('runtime.workflow_history','INSERT','state_id'),
  ('runtime.workflow_history','INSERT','status'),
  ('runtime.workflow_history','INSERT','step_key'),
  ('runtime.workflow_history','INSERT','tenant_id'),
  ('runtime.workflow_history','SELECT',NULL),
  ('runtime.workflow_instances','SELECT',NULL),
  ('runtime.workflow_instances','UPDATE','attempt'),
  ('runtime.workflow_instances','UPDATE','due_at'),
  ('runtime.workflow_instances','UPDATE','entered_at'),
  ('runtime.workflow_instances','UPDATE','failure'),
  ('runtime.workflow_instances','UPDATE','state_id'),
  ('runtime.workflow_instances','UPDATE','status'),
  ('runtime.workflow_instances','UPDATE','termination'),
  ('runtime.workflow_instances','UPDATE','version'),
  ('runtime.workflow_outbox','INSERT','body'),
  ('runtime.workflow_outbox','INSERT','body_hash'),
  ('runtime.workflow_outbox','INSERT','instance_id'),
  ('runtime.workflow_outbox','INSERT','sequence'),
  ('runtime.workflow_outbox','INSERT','tenant_id'),
  ('runtime.workflow_outbox','SELECT',NULL),
  ('runtime.workflow_receipts','INSERT','activity_ref'),
  ('runtime.workflow_receipts','INSERT','compensation_ref'),
  ('runtime.workflow_receipts','INSERT','instance_id'),
  ('runtime.workflow_receipts','INSERT','output'),
  ('runtime.workflow_receipts','INSERT','output_hash'),
  ('runtime.workflow_receipts','INSERT','request_hash'),
  ('runtime.workflow_receipts','INSERT','sequence'),
  ('runtime.workflow_receipts','INSERT','step_key'),
  ('runtime.workflow_receipts','INSERT','tenant_id'),
  ('runtime.workflow_receipts','SELECT',NULL),
  ('runtime.workflow_signals','SELECT',NULL)
 ) g(rel,priv,col)
$$;
-- Every other relation of the four schemas: the worker must hold nothing on it (derived, so a relation added
-- by a later migration is denied by default).
CREATE FUNCTION control.worker_guard_denied() RETURNS text[] LANGUAGE sql STABLE SET search_path=pg_catalog,pg_temp AS $$
 SELECT coalesce(array_agg(n.nspname||'.'||c.relname ORDER BY n.nspname||'.'||c.relname),ARRAY[]::text[]) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname IN('control','runtime','iam','audit') AND c.relkind IN('r','p','v','m','f')
  AND NOT EXISTS(SELECT 1 FROM control.worker_guard_granted() g WHERE g.rel=n.nspname||'.'||c.relname)
$$;
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','DELETE','TRUNCATE','REFERENCES','TRIGGER']) p
   WHERE n.nspname IN('control','runtime','iam','audit') AND c.relkind IN('r','p','v','m','f') AND has_table_privilege(session_user,c.oid,p)
   AND NOT EXISTS(SELECT 1 FROM control.worker_guard_granted() g WHERE g.rel=n.nspname||'.'||c.relname AND g.priv=p AND g.col IS NULL))
 AND NOT EXISTS(SELECT 1 FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN unnest(ARRAY['SELECT','INSERT','UPDATE','REFERENCES']) p
   WHERE a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL AND n.nspname IN('control','runtime','iam','audit') AND c.relkind IN('r','p','v','m','f')
   AND has_column_privilege(session_user,c.oid,a.attnum,p) AND NOT has_table_privilege(session_user,c.oid,p)
   AND NOT EXISTS(SELECT 1 FROM control.worker_guard_granted() g WHERE g.rel=n.nspname||'.'||c.relname AND g.priv=p AND g.col=a.attname))
 AND NOT EXISTS(SELECT 1 FROM pg_namespace n WHERE n.nspname IN('control','runtime','iam','audit') AND has_schema_privilege(session_user,n.oid,'CREATE'))
$$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control'
  AND p.proname IN('candidate_body_keys','keys_within','strict_candidate_json','worker_guard_granted','worker_guard_denied','worker_grants_safe','append_requirements','author_acceptance','stage_candidate') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.append_requirements(text,bigint,bytea),control.author_acceptance(text,text,bytea),control.stage_candidate(text,text,bytea) TO framework_app;
GRANT EXECUTE ON FUNCTION control.worker_grants_safe() TO framework_worker;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain release evidence; forward recovery only';END $$;
-- +goose StatementEnd
