-- M13 approval and promotion control plane (ADR039). Additive and forward-only.
-- Approval, rejection, activation and rollback are written only through definer
-- ports that derive every authority-bearing value from DB-stamped rows: the
-- authenticated actor and tenant, live memberships, the M12 candidate index and
-- the active pointer. The runtime role loses its M6R write privileges on
-- release_approvals and active_releases (rows written by M6R are untouched) and
-- release history and rejections are append-only. Nothing here executes a
-- storage migration or serves a release: the pointer is desired state (section 10).
-- +goose Up
-- Rejections are permanent and bound to the exact artifact hash (section 3).
CREATE TABLE control.release_rejections(
 tenant_id uuid NOT NULL,app_id text NOT NULL,artifact_hash text NOT NULL CHECK(artifact_hash ~ '^[0-9a-f]{64}$'),
 actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 reason text NOT NULL CHECK(reason IN('REQUIREMENTS_NOT_MET','RISK_UNACCEPTABLE','EVIDENCE_INSUFFICIENT','SUPERSEDED','WITHDRAWN','OTHER')),
 note text NOT NULL DEFAULT '' CHECK(char_length(note)<=280 AND note !~ '[\u0001-\u001f\u007f-\u009f\u2028\u2029\u202a-\u202e\u2066-\u2069]'),
 rejected_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,app_id,artifact_hash,actor_id),
 FOREIGN KEY(tenant_id,app_id,artifact_hash) REFERENCES control.release_bundles(tenant_id,app_id,artifact_hash)
);
SELECT runtime.install_tenant_rls('control.release_rejections');
REVOKE INSERT,UPDATE,DELETE ON control.release_rejections FROM framework_app;
CREATE POLICY worker_no_generic ON control.release_rejections AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
-- One row per pointer move. Each row describes the release that became active
-- (risk level and storage class as recorded when it was activated).
CREATE TABLE control.release_history(
 tenant_id uuid NOT NULL,app_id text NOT NULL,seq bigint NOT NULL CHECK(seq>=1),
 kind text NOT NULL CHECK(kind IN('ACTIVATE','ROLLBACK')),
 artifact_hash text NOT NULL CHECK(artifact_hash ~ '^[0-9a-f]{64}$'),
 previous_hash text NOT NULL CHECK(previous_hash='' OR previous_hash ~ '^[0-9a-f]{64}$'),
 generation bigint NOT NULL CHECK(generation>=1),
 actor_id uuid NOT NULL DEFAULT iam.authenticated_actor(),
 at timestamptz NOT NULL DEFAULT clock_timestamp(),
 risk_level text NOT NULL CHECK(risk_level IN('LOW','MEDIUM','HIGH','CRITICAL')),
 storage_class text NOT NULL CHECK(storage_class IN('EQUAL','ADDITIVE','TRANSFORM','BREAKING')),
 plan_hash text CHECK(plan_hash ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(tenant_id,app_id,seq),
 UNIQUE(tenant_id,app_id,generation),
 CHECK(artifact_hash<>previous_hash),
 CHECK(kind='ROLLBACK' OR plan_hash IS NULL),
 CHECK(kind='ACTIVATE' OR previous_hash<>''),
 FOREIGN KEY(tenant_id,app_id,artifact_hash) REFERENCES control.release_bundles(tenant_id,app_id,artifact_hash)
);
SELECT runtime.install_tenant_rls('control.release_history');
REVOKE INSERT,UPDATE,DELETE ON control.release_history FROM framework_app;
CREATE POLICY worker_no_generic ON control.release_history AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE INDEX release_history_target ON control.release_history(tenant_id,app_id,artifact_hash,seq);
CREATE TRIGGER retain_release_rejections BEFORE UPDATE OR DELETE OR TRUNCATE ON control.release_rejections FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
CREATE TRIGGER retain_release_history BEFORE UPDATE OR DELETE OR TRUNCATE ON control.release_history FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- The M6R write paths end here: no direct runtime write privilege (table or
-- column level) and no permissive write policy remains. approval_read and
-- active_read stay; the existing schema_owner_access policies (USING true, from
-- install_tenant_rls in migration 11) are what the definer ports use.
DROP POLICY approval_insert ON control.release_approvals;
DROP POLICY active_write ON control.active_releases;
REVOKE INSERT(tenant_id,app_id,artifact_hash) ON control.release_approvals FROM framework_app;
REVOKE UPDATE(artifact_hash,generation) ON control.active_releases FROM framework_app;
REVOKE INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER ON control.release_approvals,control.active_releases FROM framework_app;
CREATE POLICY worker_no_generic ON control.release_approvals AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE POLICY worker_no_generic ON control.active_releases AS RESTRICTIVE TO framework_worker USING(false) WITH CHECK(false);
CREATE TRIGGER retain_active_release BEFORE DELETE OR TRUNCATE ON control.active_releases FOR EACH STATEMENT EXECUTE FUNCTION control.immutable_artifact();
-- +goose StatementBegin
-- History is a contiguous chain per application (seq and generation step by
-- one, previous_hash is the previous entry). Constraints catch races; this
-- catches a hand-written row that would leave a gap.
CREATE FUNCTION control.release_history_append() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
DECLARE prev record;
BEGIN
 SELECT h.seq,h.generation,h.artifact_hash INTO prev FROM control.release_history h WHERE h.tenant_id=NEW.tenant_id AND h.app_id=NEW.app_id ORDER BY h.seq DESC LIMIT 1;
 IF (FOUND AND (NEW.seq<>prev.seq+1 OR NEW.generation<>prev.generation+1 OR NEW.previous_hash<>prev.artifact_hash)) OR (NOT FOUND AND NEW.seq<>1) THEN
  RAISE EXCEPTION 'release history must extend its previous entry' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
-- The pointer moves one generation at a time and never changes identity.
CREATE FUNCTION control.release_pointer_step() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.app_id IS DISTINCT FROM OLD.app_id OR NEW.generation IS DISTINCT FROM OLD.generation+1 OR NEW.artifact_hash IS NOT DISTINCT FROM OLD.artifact_hash THEN
  RAISE EXCEPTION 'the release pointer moves one generation at a time' USING ERRCODE='55000';END IF;
 RETURN NEW;
END $$;
-- Every pointer state written after this migration is recorded in history by
-- the same transaction (checked at commit), so a hand-written pointer move by an
-- operator fails instead of silently diverging from the audit trail.
CREATE FUNCTION control.release_pointer_recorded() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM control.release_history h WHERE h.tenant_id=NEW.tenant_id AND h.app_id=NEW.app_id AND h.generation=NEW.generation AND h.artifact_hash=NEW.artifact_hash) THEN
  RAISE EXCEPTION 'the release pointer moves only with its release history entry' USING ERRCODE='55000';END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER release_history_append BEFORE INSERT ON control.release_history FOR EACH ROW EXECUTE FUNCTION control.release_history_append();
CREATE TRIGGER release_pointer_step BEFORE UPDATE ON control.active_releases FOR EACH ROW EXECUTE FUNCTION control.release_pointer_step();
CREATE CONSTRAINT TRIGGER release_pointer_recorded AFTER INSERT OR UPDATE ON control.active_releases DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION control.release_pointer_recorded();
-- +goose StatementBegin
-- Distinct approvals that satisfy the database floor NOW: the approver is still
-- an active user and an active non-viewer member, and is not excluded by the
-- candidate's DB-stamped parties (HIGH: requester, acceptance author;
-- CRITICAL: also the sandbox freezer). Internal: never granted to the runtime.
CREATE FUNCTION control.release_approval_count(t uuid,p_app text,p_hash text) RETURNS integer LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT count(DISTINCT a.actor_id)::integer FROM control.release_approvals a
  JOIN control.release_candidates c ON c.tenant_id=a.tenant_id AND c.app_id=a.app_id AND c.artifact_hash=a.artifact_hash
  JOIN iam.memberships m ON m.tenant_id=a.tenant_id AND m.user_id=a.actor_id AND m.active AND m.role<>'viewer'
  JOIN iam.users u ON u.id=a.actor_id AND u.active
 WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_hash
  AND (c.risk_level IN('LOW','MEDIUM') OR (a.actor_id<>c.requester_id AND a.actor_id<>c.acceptance_author AND (c.risk_level='HIGH' OR a.actor_id<>c.sandbox_frozen_by)))
$$;
-- section 3: approve a staged 2.0.0 candidate. Returns the eligible approval count.
CREATE FUNCTION control.approve_release(p_app text,p_hash text) RETURNS integer
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();c record;cur record;has_cur boolean;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release approver required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' THEN RAISE EXCEPTION 'invalid approval request' USING ERRCODE='22023';END IF;
 SELECT r.requester_id,r.acceptance_author,r.sandbox_frozen_by,r.risk_level,r.base_release,r.base_generation INTO c
  FROM control.release_candidates r WHERE r.tenant_id=t AND r.app_id=p_app AND r.artifact_hash=p_hash;
 IF NOT FOUND THEN RAISE EXCEPTION 'release candidate not found' USING ERRCODE='P0002';END IF;
 IF EXISTS(SELECT 1 FROM control.release_rejections x WHERE x.tenant_id=t AND x.app_id=p_app AND x.artifact_hash=p_hash) THEN
  RAISE EXCEPTION 'release candidate was rejected' USING ERRCODE='55000';END IF;
 SELECT a.artifact_hash,a.generation INTO cur FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app;
 has_cur:=FOUND;
 IF (has_cur AND (cur.artifact_hash<>c.base_release OR cur.generation<>c.base_generation)) OR (NOT has_cur AND (c.base_release<>'' OR c.base_generation<>0)) THEN
  RAISE EXCEPTION 'candidate is stale' USING ERRCODE='55000';END IF;
 IF c.risk_level IN('HIGH','CRITICAL') AND actor=c.requester_id THEN
  RAISE EXCEPTION 'separation of duties: the requester cannot approve this candidate' USING ERRCODE='42501',DETAIL='requester';END IF;
 IF c.risk_level IN('HIGH','CRITICAL') AND actor=c.acceptance_author THEN
  RAISE EXCEPTION 'separation of duties: the acceptance author cannot approve this candidate' USING ERRCODE='42501',DETAIL='acceptance_author';END IF;
 IF c.risk_level='CRITICAL' AND actor=c.sandbox_frozen_by THEN
  RAISE EXCEPTION 'separation of duties: the sandbox freezer cannot approve this candidate' USING ERRCODE='42501',DETAIL='sandbox_freezer';END IF;
 INSERT INTO control.release_approvals(tenant_id,app_id,artifact_hash,actor_id) VALUES(t,p_app,p_hash,actor) ON CONFLICT DO NOTHING;
 RETURN control.release_approval_count(t,p_app,p_hash);
END $$;
-- section 3: a veto is always safe, so there is no separation exclusion. True if newly
-- recorded, false if this actor already rejected the hash.
CREATE FUNCTION control.reject_release(p_app text,p_hash text,p_reason text,p_note text) RETURNS boolean
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();v_note text:=coalesce(p_note,'');n bigint;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release approver required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' OR p_reason IS NULL
  OR p_reason NOT IN('REQUIREMENTS_NOT_MET','RISK_UNACCEPTABLE','EVIDENCE_INSUFFICIENT','SUPERSEDED','WITHDRAWN','OTHER')
  OR char_length(v_note)>280 OR v_note ~ '[\u0001-\u001f\u007f-\u009f\u2028\u2029\u202a-\u202e\u2066-\u2069]' THEN
  RAISE EXCEPTION 'invalid rejection request' USING ERRCODE='22023';END IF;
 IF NOT EXISTS(SELECT 1 FROM control.release_candidates r WHERE r.tenant_id=t AND r.app_id=p_app AND r.artifact_hash=p_hash) THEN
  RAISE EXCEPTION 'release candidate not found' USING ERRCODE='P0002';END IF;
 INSERT INTO control.release_rejections(tenant_id,app_id,artifact_hash,actor_id,reason,note) VALUES(t,p_app,p_hash,actor,p_reason,v_note) ON CONFLICT DO NOTHING;
 GET DIAGNOSTICS n=ROW_COUNT;
 RETURN n=1;
END $$;
-- section 4: compare-and-set activation. The pointer row lock serialises activation
-- and rollback; two first activations race on the pointer primary key.
CREATE FUNCTION control.activate_release(p_app text,p_hash text,p_expected_base text,p_expected_generation bigint) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();c record;cur record;has_cur boolean;need integer;raw bytea;doc jsonb;cls text;n bigint;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release activator required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_hash IS NULL OR p_hash !~ '^[0-9a-f]{64}$' OR p_expected_base IS NULL OR (p_expected_base<>'' AND p_expected_base !~ '^[0-9a-f]{64}$')
  OR p_expected_generation IS NULL OR p_expected_generation<0 OR p_expected_generation>=9223372036854775807 THEN
  RAISE EXCEPTION 'invalid activation request' USING ERRCODE='22023';END IF;
 SELECT a.artifact_hash,a.generation INTO cur FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app FOR UPDATE;
 has_cur:=FOUND;
 SELECT r.risk_level,r.base_release,r.base_generation INTO c FROM control.release_candidates r WHERE r.tenant_id=t AND r.app_id=p_app AND r.artifact_hash=p_hash;
 IF NOT FOUND THEN RAISE EXCEPTION 'release candidate not found' USING ERRCODE='P0002';END IF;
 IF EXISTS(SELECT 1 FROM control.release_rejections x WHERE x.tenant_id=t AND x.app_id=p_app AND x.artifact_hash=p_hash) THEN
  RAISE EXCEPTION 'release candidate was rejected' USING ERRCODE='55000';END IF;
 IF p_expected_base<>c.base_release OR p_expected_generation<>c.base_generation THEN
  RAISE EXCEPTION 'expected base is not the candidate base' USING ERRCODE='55000';END IF;
 IF (has_cur AND (cur.artifact_hash<>p_expected_base OR cur.generation<>p_expected_generation)) OR (NOT has_cur AND (p_expected_base<>'' OR p_expected_generation<>0)) THEN
  RAISE EXCEPTION 'candidate is stale' USING ERRCODE='55000';END IF;
 -- Hold the approvers' memberships until commit, so a concurrent demotion
 -- either waits or fails this transaction; then count under that lock.
 PERFORM 1 FROM iam.memberships m WHERE m.tenant_id=t AND m.user_id IN(SELECT a.actor_id FROM control.release_approvals a WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_hash) FOR SHARE OF m;
 need:=CASE WHEN c.risk_level='CRITICAL' THEN 2 ELSE 1 END;
 IF control.release_approval_count(t,p_app,p_hash)<need THEN
  RAISE EXCEPTION 'approval quorum not met' USING ERRCODE='55000';END IF;
 SELECT b.body INTO raw FROM control.release_bundles b WHERE b.tenant_id=t AND b.app_id=p_app AND b.artifact_hash=p_hash;
 BEGIN doc:=convert_from(raw,'UTF8')::jsonb;EXCEPTION WHEN others THEN doc:=NULL;END;
 IF jsonb_typeof(doc)='object' AND jsonb_typeof(doc->'Storage')='object' AND jsonb_typeof(doc->'Storage'->'class')='string' THEN cls:=doc->'Storage'->>'class';END IF;
 IF cls IS NULL OR cls NOT IN('EQUAL','ADDITIVE','TRANSFORM','BREAKING') THEN
  RAISE EXCEPTION 'candidate storage class is unreadable' USING ERRCODE='55000';END IF;
 IF has_cur THEN
  UPDATE control.active_releases a SET artifact_hash=p_hash,generation=a.generation+1 WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_expected_base AND a.generation=p_expected_generation;
 ELSE
  INSERT INTO control.active_releases(tenant_id,app_id,artifact_hash,generation) VALUES(t,p_app,p_hash,1) ON CONFLICT DO NOTHING;
 END IF;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'the active release changed concurrently' USING ERRCODE='55000';END IF;
 INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,actor_id,risk_level,storage_class)
  SELECT t,p_app,coalesce(max(h.seq),0)+1,'ACTIVATE',p_hash,p_expected_base,p_expected_generation+1,actor,c.risk_level,cls FROM control.release_history h WHERE h.tenant_id=t AND h.app_id=p_app;
 RETURN p_expected_generation+1;
END $$;
-- section 5: roll the pointer back to a release that was activated before. The class
-- gate reads Risk.Rollback of the CURRENT release; anything but exactly
-- POINTER_SAFE in a 2.0.0 body (1.0.0 bodies, missing, non-string or unreadable
-- values) requires an acknowledged plan with a 64-hex plan hash.
CREATE FUNCTION control.rollback_release(p_app text,p_target text,p_expected_current text,p_expected_generation bigint,p_plan_hash text,p_plan_ack boolean) RETURNS bigint
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
DECLARE t uuid:=iam.current_tenant_id();actor uuid:=iam.authenticated_actor();plan text:=nullif(p_plan_hash,'');cur record;tgt record;raw bytea;doc jsonb;cls text;n bigint;
BEGIN
 IF NOT control.release_actor_ok(t) THEN RAISE EXCEPTION 'tenant release operator required' USING ERRCODE='42501';END IF;
 IF p_app IS NULL OR p_app='' OR p_target IS NULL OR p_target !~ '^[0-9a-f]{64}$' OR p_expected_current IS NULL OR p_expected_current !~ '^[0-9a-f]{64}$'
  OR p_expected_generation IS NULL OR p_expected_generation<1 OR p_expected_generation>=9223372036854775807 OR (plan IS NOT NULL AND plan !~ '^[0-9a-f]{64}$') THEN
  RAISE EXCEPTION 'invalid rollback request' USING ERRCODE='22023';END IF;
 SELECT a.artifact_hash,a.generation INTO cur FROM control.active_releases a WHERE a.tenant_id=t AND a.app_id=p_app FOR UPDATE;
 IF NOT FOUND OR cur.artifact_hash<>p_expected_current OR cur.generation<>p_expected_generation THEN
  RAISE EXCEPTION 'the active release changed' USING ERRCODE='55000';END IF;
 IF p_target=cur.artifact_hash THEN RAISE EXCEPTION 'the rollback target is the active release' USING ERRCODE='55000';END IF;
 SELECT h.risk_level,h.storage_class INTO tgt FROM control.release_history h WHERE h.tenant_id=t AND h.app_id=p_app AND h.artifact_hash=p_target ORDER BY h.seq DESC LIMIT 1;
 IF NOT FOUND THEN RAISE EXCEPTION 'the rollback target was never activated' USING ERRCODE='55000';END IF;
 SELECT b.body INTO raw FROM control.release_bundles b WHERE b.tenant_id=t AND b.app_id=p_app AND b.artifact_hash=cur.artifact_hash;
 BEGIN doc:=convert_from(raw,'UTF8')::jsonb;EXCEPTION WHEN others THEN doc:=NULL;END;
 IF jsonb_typeof(doc)='object' AND jsonb_typeof(doc->'Version')='string' AND doc->>'Version'='2.0.0' AND jsonb_typeof(doc->'Risk')='object' AND jsonb_typeof(doc->'Risk'->'Rollback')='string' THEN
  cls:=doc->'Risk'->>'Rollback';END IF;
 IF cls IS DISTINCT FROM 'POINTER_SAFE' AND (p_plan_ack IS NOT TRUE OR plan IS NULL) THEN
  RAISE EXCEPTION 'rolling back this release requires an acknowledged plan' USING ERRCODE='55000';END IF;
 UPDATE control.active_releases a SET artifact_hash=p_target,generation=a.generation+1 WHERE a.tenant_id=t AND a.app_id=p_app AND a.artifact_hash=p_expected_current AND a.generation=p_expected_generation;
 GET DIAGNOSTICS n=ROW_COUNT;
 IF n<>1 THEN RAISE EXCEPTION 'the active release changed concurrently' USING ERRCODE='55000';END IF;
 INSERT INTO control.release_history(tenant_id,app_id,seq,kind,artifact_hash,previous_hash,generation,actor_id,risk_level,storage_class,plan_hash)
  SELECT t,p_app,coalesce(max(h.seq),0)+1,'ROLLBACK',p_target,p_expected_current,p_expected_generation+1,actor,tgt.risk_level,tgt.storage_class,plan FROM control.release_history h WHERE h.tenant_id=t AND h.app_id=p_app;
 RETURN p_expected_generation+1;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
DO $$ DECLARE f regprocedure;BEGIN
 FOR f IN SELECT p.oid::regprocedure FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='control'
  AND p.proname IN('release_history_append','release_pointer_step','release_pointer_recorded','release_approval_count','approve_release','reject_release','activate_release','rollback_release') LOOP
 EXECUTE format('ALTER FUNCTION %s OWNER TO framework_schema_owner',f);
 EXECUTE format('REVOKE ALL ON FUNCTION %s FROM PUBLIC,framework_app,framework_worker,framework_dispatcher',f);
 END LOOP;
END $$;
-- +goose StatementEnd
GRANT EXECUTE ON FUNCTION control.approve_release(text,text),control.reject_release(text,text,text,text),control.activate_release(text,text,text,bigint),control.rollback_release(text,text,text,bigint,text,boolean) TO framework_app;
-- Extend the worker drift guard to every release authority table.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control.worker_grants_safe() RETURNS boolean LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,pg_temp AS $$
 SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname||'.'||c.relname IN('iam.roles','runtime.records','runtime.record_sets','runtime.record_links','runtime.unique_projections','runtime.outbox_intents','control.metadata_heads','control.metadata_versions','control.release_bundles','control.worker_context','control.worker_principals','runtime.datasets','runtime.dataset_bindings','runtime.command_receipts','runtime.numeric_indexes','runtime.numeric_projections','runtime.dataset_generations','runtime.record_identity','runtime.identity_shares','runtime.dataset_evolutions','runtime.event_data_sources','control.evolution_context','runtime.read_records','runtime.read_shares','runtime.read_preview_records','runtime.record_shares','control.role_heads','control.role_versions','runtime.sandboxes','runtime.sandbox_evidence','control.requirement_sets','control.acceptance_specs','control.release_candidates','control.release_approvals','control.active_releases','control.release_rejections','control.release_history') AND (pg_catalog.has_table_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE') OR pg_catalog.has_any_column_privilege(session_user,c.oid,'SELECT,INSERT,UPDATE')))
$$;
-- +goose StatementEnd
-- Fail the migration if anything (PUBLIC, a default privilege, a leftover column
-- grant or policy) still lets a runtime role write release authority directly,
-- or if the definer owner lost the policy its ports rely on.
-- +goose StatementBegin
DO $$ DECLARE r record;BEGIN
 FOR r IN SELECT c.oid::regclass AS tab,g.role FROM pg_catalog.pg_class c CROSS JOIN unnest(ARRAY['public','framework_app','framework_worker','framework_dispatcher']) g(role)
  WHERE c.oid IN('control.release_approvals'::regclass,'control.active_releases'::regclass,'control.release_rejections'::regclass,'control.release_history'::regclass) LOOP
  IF pg_catalog.has_table_privilege(r.role,r.tab,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER') OR pg_catalog.has_any_column_privilege(r.role,r.tab,'INSERT,UPDATE,REFERENCES')
   OR (r.role<>'framework_app' AND (pg_catalog.has_table_privilege(r.role,r.tab,'SELECT') OR pg_catalog.has_any_column_privilege(r.role,r.tab,'SELECT'))) THEN
   RAISE EXCEPTION 'release authority table % keeps a direct privilege for %',r.tab,r.role;END IF;
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_catalog.pg_policy p WHERE p.polrelid IN('control.release_approvals'::regclass,'control.active_releases'::regclass,'control.release_rejections'::regclass,'control.release_history'::regclass)
   AND p.polpermissive AND p.polcmd<>'r' AND p.polname<>'tenant_scope' AND p.polname<>'schema_owner_access') THEN
  RAISE EXCEPTION 'a permissive release authority write policy remains';END IF;
 IF (SELECT count(*) FROM pg_catalog.pg_policy p WHERE p.polname='schema_owner_access' AND p.polcmd='*' AND p.polroles=ARRAY['framework_schema_owner'::regrole]::oid[]
   AND p.polrelid IN('control.release_approvals'::regclass,'control.active_releases'::regclass,'control.release_rejections'::regclass,'control.release_history'::regclass,'control.release_candidates'::regclass,'control.release_bundles'::regclass))<>6 THEN
  RAISE EXCEPTION 'release ports need the schema owner policies';END IF;
END $$;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'retain release approvals, rejections and history; forward recovery only';END $$;
-- +goose StatementEnd
