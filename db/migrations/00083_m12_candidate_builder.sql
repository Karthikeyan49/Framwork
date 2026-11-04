-- M12 candidate release builder (ADR102). Additive and forward-only.
-- Requirement sets, acceptance specs, the candidate index and release bundles
-- are written only through definer ports that bind every authority-bearing
-- value to DB-stamped rows (requirement decider, acceptance author, frozen
-- sandbox evidence, source author, active base). A tenant connection can no
-- longer INSERT a hash-consistent bundle body directly. Nothing here approves
-- or activates a release (M13) and no existing bundle row is changed.
-- +goose Up
CREATE TABLE control.requirement_sets(
 tenant_id uuid NOT NULL,app_id text NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 1000000),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 2 AND 262144),
 body_hash text NOT NULL CHECK(body_hash=encode(sha256(body),'hex')),
 decided_by uuid NOT NULL DEFAULT iam.authenticated_actor(),decided_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,version),
 UNIQUE(tenant_id,app_id,body_hash,version),
 FOREIGN KEY(tenant_id,app_id) REFERENCES control.metadata_heads(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.requirement_sets');
REVOKE INSERT,UPDATE,DELETE ON control.requirement_sets FROM framework_app;
CREATE POLICY worker_no_generic ON control.requirement_sets AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TABLE control.acceptance_specs(
 tenant_id uuid NOT NULL,app_id text NOT NULL,id uuid NOT NULL DEFAULT gen_random_uuid(),
 requirements_hash text NOT NULL CHECK(requirements_hash ~ '^[0-9a-f]{64}$'),
 body bytea NOT NULL CHECK(octet_length(body) BETWEEN 2 AND 1048576),
 body_hash text NOT NULL CHECK(body_hash=encode(sha256(body),'hex')),
 author_id uuid NOT NULL DEFAULT iam.authenticated_actor(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,id),
 UNIQUE(tenant_id,app_id,body_hash,author_id),
 FOREIGN KEY(tenant_id,app_id) REFERENCES control.metadata_heads(tenant_id,id)
);
SELECT runtime.install_tenant_rls('control.acceptance_specs');
REVOKE INSERT,UPDATE,DELETE ON control.acceptance_specs FROM framework_app;
CREATE POLICY worker_no_generic ON control.acceptance_specs AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
-- Derived, immutable index of each staged candidate. Every column is copied by
-- the stage port from the verified body and the DB rows it was bound to.
CREATE TABLE control.release_candidates(
 tenant_id uuid NOT NULL,app_id text NOT NULL,artifact_hash text NOT NULL,
 requester_id uuid NOT NULL,base_release text NOT NULL,base_generation bigint NOT NULL CHECK(base_generation>=0),
 risk_level text NOT NULL CHECK(risk_level IN('LOW','MEDIUM','HIGH','CRITICAL')),high_risk boolean NOT NULL,
 risk_policy_version text NOT NULL CHECK(length(risk_policy_version) BETWEEN 1 AND 80),
 requirements_version bigint NOT NULL CHECK(requirements_version>=1),requirements_hash text NOT NULL CHECK(requirements_hash ~ '^[0-9a-f]{64}$'),
 acceptance_id uuid NOT NULL,acceptance_hash text NOT NULL CHECK(acceptance_hash ~ '^[0-9a-f]{64}$'),acceptance_author uuid NOT NULL,
 sandbox_dataset_id uuid NOT NULL,sandbox_evidence_hash text NOT NULL CHECK(sandbox_evidence_hash ~ '^[0-9a-f]{64}$'),sandbox_frozen_by uuid NOT NULL,
 source_revision bigint NOT NULL CHECK(source_revision>=1),source_author uuid NOT NULL,ir_hash text NOT NULL CHECK(ir_hash ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,artifact_hash),
 CHECK(high_risk=(risk_level IN('HIGH','CRITICAL'))),
 FOREIGN KEY(tenant_id,app_id,artifact_hash) REFERENCES control.release_bundles(tenant_id,app_id,artifact_hash)
);
SELECT runtime.install_tenant_rls('control.release_candidates');
REVOKE INSERT,UPDATE,DELETE ON control.release_candidates FROM framework_app;
CREATE POLICY worker_no_generic ON control.release_candidates AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER retain_requirement_sets BEFORE UPDATE OR DELETE ON control.requirement_sets FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_acceptance_specs BEFORE UPDATE OR DELETE ON control.acceptance_specs FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_release_candidates BEFORE UPDATE OR DELETE ON control.release_candidates FOR EACH ROW EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_requirement_sets_truncate BEFORE TRUNCATE ON control.requirement_sets FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_acceptance_specs_truncate BEFORE TRUNCATE ON control.acceptance_specs FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_release_candidates_truncate BEFORE TRUNCATE ON control.release_candidates FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- Release evidence is never truncated, even by an operator mistake.
CREATE TRIGGER immutable_bundle_truncate BEFORE TRUNCATE ON control.release_bundles FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER immutable_approval_truncate BEFORE TRUNCATE ON control.release_approvals FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- Tenants stage only through control.stage_candidate (bundle 2.0.0).
REVOKE INSERT(tenant_id,app_id,artifact_hash,body) ON control.release_bundles FROM framework_app;
REVOKE INSERT ON control.release_bundles FROM framework_app;
-- +goose StatementBegin
CREATE FUNCTION control.release_actor_ok(t uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT session_user='framework_app' AND t IS NOT NULL AND t IS NOT DISTINCT FROM iam.current_tenant_id() AND iam.tenant_matches_scope(t)
  AND EXISTS(SELECT 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id=iam.authenticated_actor() AND m.active AND m.role<>'viewer')
$$;
-- Append-only requirement set versions with a compare-and-set head.
CREATE FUNCTION control.append_requirements(p_app text,p_expected bigint,p_body bytea) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();head bigint;doc jsonb;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant requirement decider required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_expected IS NULL OR p_expected<0 OR p_expected>=1000000 OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 262144 THEN
  RAISE EXCEPTION 'invalid requirement set' USING ERRCODE='22023';END IF;
 BEGIN doc:=convert_from(p_body,'UTF8')::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid requirement set' USING ERRCODE='22023';END;
 IF jsonb_typeof(doc)<>'object' OR doc->>'schema_version' IS DISTINCT FROM 'requirements-v2' OR doc->>'app_id' IS DISTINCT FROM p_app OR jsonb_typeof(doc->'requirements') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'invalid requirement set' USING ERRCODE='22023';END IF;
 SELECT coalesce(max(r.version),0) INTO head FROM control.requirement_sets r WHERE r.tenant_id=t AND r.app_id=p_app;
 IF head<>p_expected THEN RAISE EXCEPTION 'requirement set head changed' USING ERRCODE='55000';END IF;
 INSERT INTO control.requirement_sets(tenant_id,app_id,version,body,body_hash) VALUES(t,p_app,p_expected+1,p_body,encode(sha256(p_body),'hex'));
 RETURN p_expected+1;
END $$;
-- Acceptance specs are immutable and bind only the requirement-set hash. The
-- same author re-posting identical content gets the original row back, so a
-- requester cannot claim another person's spec by resubmitting it.
CREATE FUNCTION control.author_acceptance(p_app text,p_requirements_hash text,p_body bytea) RETURNS uuid
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();doc jsonb;h text;found_id uuid;new_id uuid;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant acceptance author required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_requirements_hash IS NULL OR p_requirements_hash !~ '^[0-9a-f]{64}$' OR p_body IS NULL OR octet_length(p_body) NOT BETWEEN 2 AND 1048576 THEN
  RAISE EXCEPTION 'invalid acceptance spec' USING ERRCODE='22023';END IF;
 BEGIN doc:=convert_from(p_body,'UTF8')::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid acceptance spec' USING ERRCODE='22023';END;
 IF jsonb_typeof(doc)<>'object' OR doc->>'schema_version' IS DISTINCT FROM 'acceptance-v2' OR doc->>'requirements_hash' IS DISTINCT FROM p_requirements_hash OR jsonb_typeof(doc->'cases') IS DISTINCT FROM 'array' THEN
  RAISE EXCEPTION 'invalid acceptance spec' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM control.requirement_sets r WHERE r.tenant_id=t AND r.app_id=p_app AND r.body_hash=p_requirements_hash) THEN
  RAISE EXCEPTION 'requirement set required' USING ERRCODE='55000';END IF;
 h:=encode(sha256(p_body),'hex');
 SELECT a.id INTO found_id FROM control.acceptance_specs a WHERE a.tenant_id=t AND a.app_id=p_app AND a.body_hash=h AND a.author_id=actor;
 IF FOUND THEN RETURN found_id;END IF;
 INSERT INTO control.acceptance_specs(tenant_id,app_id,requirements_hash,body,body_hash) VALUES(t,p_app,p_requirements_hash,p_body,h) RETURNING id INTO new_id;
 RETURN new_id;
END $$;
-- The only way a release bundle is written. It cannot recompute the verifier, so
-- it binds the body to everything the database does know: tenant, app, requester
-- (the authenticated actor), risk/HighRisk consistency, the exact requirement
-- set, acceptance spec (and its independent DB author), frozen sandbox evidence,
-- the stored source revision and the current active base. promotion.Restore
-- recomputes risk and verification from the stored bytes (ADR102 section 6).
CREATE FUNCTION control.stage_candidate(p_app text,p_hash text,p_body bytea) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();b jsonb;lvl text;rev bigint;src record;rs record;acc record;ev record;evdoc jsonb;n bigint;
 base text;base_gen bigint:=0;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release builder required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_hash IS NULL OR p_body IS NULL OR octet_length(p_body)>8388608 OR p_hash IS DISTINCT FROM encode(sha256(p_body),'hex') THEN
  RAISE EXCEPTION 'invalid candidate' USING ERRCODE='22023';END IF;
 BEGIN b:=convert_from(p_body,'UTF8')::jsonb;EXCEPTION WHEN others THEN RAISE EXCEPTION 'invalid candidate' USING ERRCODE='22023';END;
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
 SELECT v.created_by,v.source_hash INTO src FROM control.metadata_versions v WHERE v.tenant_id=t AND v.app_id=p_app AND v.revision=rev;
 IF NOT FOUND OR src.source_hash IS DISTINCT FROM b->'Source'->>'Hash' THEN RAISE EXCEPTION 'stored source revision required' USING ERRCODE='55000';END IF;
 SELECT r.body_hash,r.decided_by INTO rs FROM control.requirement_sets r WHERE r.tenant_id=t AND r.app_id=p_app AND r.version=(b->'Requirements'->>'Version')::bigint;
 IF NOT FOUND OR rs.body_hash IS DISTINCT FROM b->'Requirements'->>'Hash' THEN RAISE EXCEPTION 'stored requirement set required' USING ERRCODE='55000';END IF;
 SELECT a.body_hash,a.author_id,a.requirements_hash INTO acc FROM control.acceptance_specs a WHERE a.tenant_id=t AND a.app_id=p_app AND a.id=(b->'Acceptance'->>'ID')::uuid;
 IF NOT FOUND OR acc.body_hash IS DISTINCT FROM b->'Acceptance'->>'Hash' OR acc.requirements_hash IS DISTINCT FROM rs.body_hash
  OR b->'Acceptance'->>'RequirementsHash' IS DISTINCT FROM rs.body_hash OR acc.author_id::text IS DISTINCT FROM b->'Acceptance'->>'AuthorID' THEN
  RAISE EXCEPTION 'stored acceptance spec required' USING ERRCODE='55000';END IF;
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
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control' AND p.proname IN('release_actor_ok','append_requirements','author_acceptance','stage_candidate') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.append_requirements(text,bigint,bytea),control.author_acceptance(text,text,bytea),control.stage_candidate(text,text,bytea) TO framework_app;
-- Extend the existing worker drift guard to the new release catalogs.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions','runtime.sandboxes','runtime.sandbox_evidence','control.requirement_sets','control.acceptance_specs','control.release_candidates') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain release evidence; forward recovery only';END $$;
-- +goose StatementEnd
